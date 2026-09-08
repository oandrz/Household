package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// Chat commands. This file is the inbound edge for Telegram the way
// router.go plus its middleware is for HTTP: it parses what arrived,
// decides who is asking and whether they may, and only then calls a
// service. ADR 8 records why the guard lives here and not in usecase.

// Command is one recognised chat command, already split into its parts.
type Command struct {
	ChatID   int64
	UpdateID int64
	// Name is one of spend, income, balance, recent, help, yes, no, or
	// text -- the last being a plain sentence, which only means something
	// when an IntentParser is configured (stage 5b).
	Name string
	// For spend and income:
	Amount      string
	Description string
	Account     string // from @name, "" if none
	Category    string // from #name, "" if none
}

// ParseCommand recognises the grammar
//
//	/spend <amount> <description…> [#category] [@account]
//	/income <amount> <description…> [#category] [@account]
//	/balance
//	/recent
//	/help
//
// Tokens beginning with # and @ may appear anywhere after the amount and
// may contain spaces when written as #"dining out" or @"DBS Savings"; the
// rest of the words are the description. Anything else -- /start, plain
// text, an unknown slash -- returns false, and the caller decides whether
// plain text means anything (it does not, in this stage). Refuse what you
// did not construct: an unknown command is ignored, never guessed at.
func ParseCommand(u Update) (Command, bool) {
	if u.Message == nil {
		return Command{}, false
	}
	text := strings.TrimSpace(u.Message.Text)
	cmd := Command{ChatID: u.Message.Chat.ID, UpdateID: u.UpdateID}
	if text == "" {
		return Command{}, false
	}
	if !strings.HasPrefix(text, "/") {
		// A plain sentence. The Commander decides whether it means anything
		// (only with a parser configured); the poller just hands it over.
		cmd.Name, cmd.Description = "text", text
		return cmd, true
	}
	word, rest, _ := strings.Cut(text, " ")
	// Telegram appends @botname to commands in groups: "/spend@HearthBot".
	word, _, _ = strings.Cut(strings.ToLower(word), "@")
	switch word {
	case "/balance", "/recent", "/help", "/yes", "/no":
		cmd.Name = strings.TrimPrefix(word, "/")
		return cmd, true
	case "/spend", "/income":
		cmd.Name = strings.TrimPrefix(word, "/")
	default:
		return Command{}, false
	}
	tokens := splitQuoted(strings.TrimSpace(rest))
	if len(tokens) == 0 {
		return cmd, true // the handler answers with usage
	}
	cmd.Amount = tokens[0]
	var desc []string
	for _, tok := range tokens[1:] {
		switch {
		case strings.HasPrefix(tok, "#") && len(tok) > 1:
			cmd.Category = tok[1:]
		case strings.HasPrefix(tok, "@") && len(tok) > 1:
			cmd.Account = tok[1:]
		default:
			desc = append(desc, tok)
		}
	}
	cmd.Description = strings.Join(desc, " ")
	return cmd, true
}

// splitQuoted splits on spaces but keeps "quoted words" together, so
// #"dining out" is one token. A quote left open runs to the end.
func splitQuoted(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// CallerResolver and CommandService are the two shapes the edge needs from
// usecase, declared here so this package depends on shapes, not services
// (the same reason StartHandler is declared in poller.go).
type CallerResolver interface {
	Resolve(ctx context.Context, chatID int64) (domain.Membership, error)
}

type CommandService interface {
	LogSpend(ctx context.Context, in usecase.TelegramSpend) (usecase.TelegramSpendResult, error)
	Balances(ctx context.Context, householdID string) ([]usecase.AccountView, error)
	Recent(ctx context.Context, householdID string, n int) ([]usecase.TransactionView, error)
	// Names is what the parser may pick from: live account nicknames and
	// the household's unarchived category names.
	Names(ctx context.Context, householdID string) (accounts, categories []string, err error)
}

type Sender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

// Commander is the guard and the reply. It is what the poller hands a parsed
// Command to.
type Commander struct {
	resolver CallerResolver
	svc      CommandService
	sender   Sender
	// parser is nil unless an API key is configured; then free text is read
	// into an intent, shown back, and written only on /yes.
	parser usecase.IntentParser
	now    func() time.Time

	mu      sync.Mutex
	pending map[int64]pendingIntent // by chat; the one thing awaiting /yes
}

// pendingIntent is a parsed sentence waiting for its /yes. It keeps the
// update id of the sentence, not of the /yes, so the eventual write's
// idempotency key is the message that carried the intent.
type pendingIntent struct {
	spend   usecase.TelegramSpend
	summary string
	expires time.Time
}

// pendingTTL is how long a "Log this? /yes" waits. Short on purpose: a /yes
// twenty minutes later is more likely a reply to something else.
const pendingTTL = 5 * time.Minute

// parseTimeout caps one call to the parser. The poller handles updates one
// at a time on its own goroutine, so a provider that stalls (a free-tier
// queue, a slow model) would otherwise hold every chat's next message for
// as long as the provider liked. Whichever adapter is wired, this is the
// one ceiling; the adapters' own client timeouts are a second line.
const parseTimeout = 30 * time.Second

func NewCommander(r CallerResolver, s CommandService, sender Sender) *Commander {
	return &Commander{resolver: r, svc: s, sender: sender, now: time.Now, pending: map[int64]pendingIntent{}}
}

// WithIntentParser turns free text on. Returns the commander for chaining.
func (c *Commander) WithIntentParser(p usecase.IntentParser) *Commander {
	c.parser = p
	return c
}

const (
	replyNotLinked      = "This chat is not linked to a Hearth account. Sign in from the app with Telegram first."
	replyNotOwner       = "Only a household owner with Money can log spending here."
	replyHelp           = "Commands:\n/spend <amount> <what> [#category] [@account]\n/income <amount> <what> [#category] [@account]\n/balance\n/recent\n\nExamples:\n/spend 84.50 groceries #Groceries @\"DBS Savings\"\n/income 6500 salary\n\nAmounts are in the account's currency. Quote names with spaces."
	replyUsageSpend     = "Usage: /spend <amount> <what> [#category] [@account] — e.g. /spend 12.50 coffee #\"Dining out\""
	replyUsageIncome    = "Usage: /income <amount> <what> [#category] [@account] — e.g. /income 6500 salary @\"DBS Savings\""
	replyNoParser       = "I only understand commands here. Send /help to see them."
	replyNotUnderstood  = "I could not read that as an expense or income. Try /spend <amount> <what>, or /help."
	replyNothingPending = "Nothing to confirm. Tell me what you spent, or use /spend."
	replyDiscarded      = "Discarded."
	recentCount         = 5
)

// HandleCommand is the whole edge: resolve the chat, refuse what a limited
// member or a stranger may not do, run the command, reply. Every reply is a
// plain sentence; a refusal never says which of "unbound" or "unknown user"
// it was.
func (c *Commander) HandleCommand(ctx context.Context, cmd Command) error {
	if cmd.Name == "help" {
		return c.sender.SendMessage(ctx, cmd.ChatID, replyHelp)
	}
	member, err := c.resolver.Resolve(ctx, cmd.ChatID)
	if err != nil {
		// A plain sentence from an unlinked chat gets silence, not a reply:
		// before free text existed such a message was ignored, and every
		// reply is an outbound send against the same Telegram budget
		// sign-in links use. A slash command still gets the sentence back.
		if cmd.Name == "text" {
			return nil
		}
		return c.sender.SendMessage(ctx, cmd.ChatID, replyNotLinked)
	}
	// The same stack the money routes carry: the capability and the role,
	// both, for the same reason router.go stacks them -- neither may lean on
	// an invariant enforced elsewhere.
	if member.Role != domain.RoleOwner || !member.Capabilities.Has(domain.CapMoney) {
		if cmd.Name == "text" {
			return nil
		}
		return c.sender.SendMessage(ctx, cmd.ChatID, replyNotOwner)
	}

	switch cmd.Name {
	case "spend", "income":
		return c.logSpend(ctx, member, cmd)
	case "balance":
		return c.balance(ctx, member, cmd)
	case "recent":
		return c.recent(ctx, member, cmd)
	case "text":
		return c.freeText(ctx, member, cmd)
	case "yes":
		return c.confirm(ctx, cmd)
	case "no":
		c.takePending(cmd.ChatID)
		return c.sender.SendMessage(ctx, cmd.ChatID, replyDiscarded)
	default:
		// Fail closed: ParseCommand only builds the names above, but a
		// switch over a value it did not construct itself still refuses.
		return c.sender.SendMessage(ctx, cmd.ChatID, replyHelp)
	}
}

func (c *Commander) logSpend(ctx context.Context, member domain.Membership, cmd Command) error {
	kind := domain.TransactionExpense
	usage := replyUsageSpend
	if cmd.Name == "income" {
		kind = domain.TransactionIncome
		usage = replyUsageIncome
	}
	if cmd.Amount == "" || cmd.Description == "" {
		return c.sender.SendMessage(ctx, cmd.ChatID, usage)
	}
	res, err := c.svc.LogSpend(ctx, usecase.TelegramSpend{
		HouseholdID:  member.HouseholdID,
		MembershipID: member.ID,
		UpdateID:     cmd.UpdateID,
		Kind:         kind,
		AmountText:   cmd.Amount,
		Description:  cmd.Description,
		AccountName:  cmd.Account,
		CategoryName: cmd.Category,
	})
	if err != nil {
		return c.sender.SendMessage(ctx, cmd.ChatID, explain(err))
	}
	return c.sender.SendMessage(ctx, cmd.ChatID, receipt(res, kind))
}

// freeText is stage 5b: a sentence, read by the parser into the same
// fields /spend takes, shown back, and held until /yes. The guard above
// has already run, so a stranger's message never reaches the API.
func (c *Commander) freeText(ctx context.Context, member domain.Membership, cmd Command) error {
	if c.parser == nil {
		return c.sender.SendMessage(ctx, cmd.ChatID, replyNoParser)
	}
	accounts, categories, err := c.svc.Names(ctx, member.HouseholdID)
	if err != nil {
		slog.Error("telegram names failed", "error", err)
		return c.sender.SendMessage(ctx, cmd.ChatID, "Could not read the accounts right now.")
	}
	parseCtx, cancel := context.WithTimeout(ctx, parseTimeout)
	defer cancel()
	intent, err := c.parser.ParseIntent(parseCtx, usecase.ParseIntentInput{Text: cmd.Description, Accounts: accounts, Categories: categories})
	if err != nil {
		slog.Error("telegram intent parse failed", "error", err)
		return c.sender.SendMessage(ctx, cmd.ChatID, "I could not read that right now. Use /spend <amount> <what> instead.")
	}
	var kind domain.TransactionKind
	switch intent.Kind {
	case "expense":
		kind = domain.TransactionExpense
	case "income":
		kind = domain.TransactionIncome
	default:
		return c.sender.SendMessage(ctx, cmd.ChatID, replyNotUnderstood)
	}
	if intent.Amount == "" || intent.Description == "" {
		return c.sender.SendMessage(ctx, cmd.ChatID, replyNotUnderstood)
	}
	spend := usecase.TelegramSpend{
		HouseholdID: member.HouseholdID, MembershipID: member.ID, UpdateID: cmd.UpdateID,
		Kind: kind, AmountText: intent.Amount, Description: intent.Description,
		AccountName: intent.Account, CategoryName: intent.Category,
	}
	summary := fmt.Sprintf("%s %s — %s", intent.Kind, intent.Amount, intent.Description)
	if intent.Category != "" {
		summary += " #" + intent.Category
	}
	if intent.Account != "" {
		summary += " @" + intent.Account
	}
	c.mu.Lock()
	c.pending[cmd.ChatID] = pendingIntent{spend: spend, summary: summary, expires: c.now().Add(pendingTTL)}
	c.mu.Unlock()
	return c.sender.SendMessage(ctx, cmd.ChatID, "Log "+summary+"? Reply /yes or /no.")
}

// confirm writes the pending intent. The /yes itself carries no fields: a
// person cannot confirm something the bot did not just show them.
func (c *Commander) confirm(ctx context.Context, cmd Command) error {
	p, ok := c.takePending(cmd.ChatID)
	if !ok {
		return c.sender.SendMessage(ctx, cmd.ChatID, replyNothingPending)
	}
	res, err := c.svc.LogSpend(ctx, p.spend)
	if err != nil {
		return c.sender.SendMessage(ctx, cmd.ChatID, explain(err))
	}
	return c.sender.SendMessage(ctx, cmd.ChatID, receipt(res, p.spend.Kind))
}

// takePending removes and returns the chat's pending intent if it has not
// expired. Expired ones are dropped on the way out, so the map never grows
// past one live entry per chat that ever spoke.
func (c *Commander) takePending(chatID int64) (pendingIntent, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.pending[chatID]
	delete(c.pending, chatID)
	if !ok || !c.now().Before(p.expires) {
		return pendingIntent{}, false
	}
	return p, true
}

func receipt(res usecase.TelegramSpendResult, kind domain.TransactionKind) string {
	verb := "Logged"
	if res.Replayed {
		verb = "Already logged"
	}
	sign := "-"
	if kind == domain.TransactionIncome {
		sign = "+"
	}
	amount := domain.FormatAmount(res.Transaction.Amount.Amount, res.MinorUnits)
	return fmt.Sprintf("%s %s%s %s — %s (%s)", verb, sign, amount, res.Transaction.Amount.Currency, res.Transaction.Description, res.AccountName)
}

func (c *Commander) balance(ctx context.Context, member domain.Membership, cmd Command) error {
	views, err := c.svc.Balances(ctx, member.HouseholdID)
	if err != nil {
		slog.Error("telegram /balance failed", "error", err)
		return c.sender.SendMessage(ctx, cmd.ChatID, "Could not read the accounts right now.")
	}
	if len(views) == 0 {
		return c.sender.SendMessage(ctx, cmd.ChatID, "No accounts yet. Add one in the app.")
	}
	var b strings.Builder
	for _, v := range views {
		units := domain.MinorUnitsFor(v.Balance.Currency)
		fmt.Fprintf(&b, "%s: %s %s\n", v.Account.Nickname, v.Balance.Currency, domain.FormatAmount(v.Balance.Amount, units))
	}
	return c.sender.SendMessage(ctx, cmd.ChatID, strings.TrimRight(b.String(), "\n"))
}

func (c *Commander) recent(ctx context.Context, member domain.Membership, cmd Command) error {
	views, err := c.svc.Recent(ctx, member.HouseholdID, recentCount)
	if err != nil {
		slog.Error("telegram /recent failed", "error", err)
		return c.sender.SendMessage(ctx, cmd.ChatID, "Could not read the ledger right now.")
	}
	if len(views) == 0 {
		return c.sender.SendMessage(ctx, cmd.ChatID, "No transactions yet.")
	}
	var b strings.Builder
	for _, v := range views {
		t := v.Transaction
		sign := "-"
		account := v.FromAccountName
		if t.Kind == domain.TransactionIncome {
			sign = "+"
			account = v.ToAccountName
		}
		fmt.Fprintf(&b, "%s %s%s %s %s (%s)\n", t.OccurredOn.Format("Jan 2"), sign,
			domain.FormatAmount(t.Amount.Amount, domain.MinorUnitsFor(t.Amount.Currency)), t.Amount.Currency, t.Description, account)
	}
	return c.sender.SendMessage(ctx, cmd.ChatID, strings.TrimRight(b.String(), "\n"))
}

// explain turns a service refusal into one sentence a person can act on.
// Anything it does not recognise is logged and answered generically: the
// chat must not become a channel that prints internal errors.
func explain(err error) string {
	if re, ok := usecase.IsResolutionError(err); ok {
		list := strings.Join(re.Candidates, ", ")
		switch {
		case re.Typed == "" && re.What == "account":
			if re.Ambiguous {
				return "Which account? Add @name — one of: " + list
			}
			return "No cash account to use. Add @name — one of: " + list
		case re.Ambiguous:
			return fmt.Sprintf("%q matches more than one %s. One of: %s", re.Typed, re.What, list)
		default:
			return fmt.Sprintf("No %s called %q. One of: %s", re.What, re.Typed, list)
		}
	}
	if errorsIs(err, domain.ErrInvalidMoney) {
		return "That amount could not be read. Use the account's currency, e.g. 84.50 (no more decimals than the currency has)."
	}
	if errorsIs(err, domain.ErrCategoryKindMismatch) {
		return "That category is for the other kind of transaction."
	}
	slog.Error("telegram command failed", "error", err)
	return "That could not be saved. Try again, or use the app."
}

func errorsIs(err, target error) bool { return errors.Is(err, target) }
