package telegram

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func msg(id int64, chat int64, text string) Update {
	u := Update{UpdateID: id, Message: &Message{Text: text}}
	u.Message.Chat.ID = chat
	return u
}

func TestParseCommandGrammar(t *testing.T) {
	cases := []struct {
		text string
		want Command
		ok   bool
	}{
		{"/spend 84.50 groceries", Command{Name: "spend", Amount: "84.50", Description: "groceries"}, true},
		{"/spend 12 coffee and cake #\"Dining out\" @\"DBS Savings\"", Command{Name: "spend", Amount: "12", Description: "coffee and cake", Category: "Dining out", Account: "DBS Savings"}, true},
		{"/income 6500 salary @OCBC", Command{Name: "income", Amount: "6500", Description: "salary", Account: "OCBC"}, true},
		{"/SPEND@HearthOinkBot 5 gum", Command{Name: "spend", Amount: "5", Description: "gum"}, true},
		{"/spend", Command{Name: "spend"}, true},
		{"/balance", Command{Name: "balance"}, true},
		{"/recent extra words", Command{Name: "recent"}, true},
		{"/help", Command{Name: "help"}, true},
		{"/start abc", Command{}, false},
		{"just words", Command{Name: "text", Description: "just words"}, true},
		{"/yes", Command{Name: "yes"}, true},
		{"/no thanks", Command{Name: "no"}, true},
		{"/unknown 5", Command{}, false},
		{"", Command{}, false},
	}
	for _, c := range cases {
		got, ok := ParseCommand(msg(7, 42, c.text))
		if ok != c.ok {
			t.Errorf("%q: ok=%v want %v", c.text, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		c.want.ChatID, c.want.UpdateID = 42, 7
		if got != c.want {
			t.Errorf("%q:\n got %+v\nwant %+v", c.text, got, c.want)
		}
	}
	if _, ok := ParseCommand(Update{UpdateID: 1}); ok {
		t.Fatal("an update with no message must not parse")
	}
}

// --- Commander doubles

type resolverStub struct {
	member domain.Membership
	err    error
}

func (r resolverStub) Resolve(context.Context, int64) (domain.Membership, error) {
	return r.member, r.err
}

type serviceSpy struct {
	nudges    []bool
	nudgesErr error
	spends    []usecase.TelegramSpend
	result    usecase.TelegramSpendResult
	err       error
}

func (s *serviceSpy) LogSpend(_ context.Context, in usecase.TelegramSpend) (usecase.TelegramSpendResult, error) {
	s.spends = append(s.spends, in)
	return s.result, s.err
}
func (s *serviceSpy) Balances(context.Context, string) ([]usecase.AccountView, error) {
	return []usecase.AccountView{{Account: domain.Account{Nickname: "DBS"}, Balance: domain.Money{Amount: 123456, Currency: "SGD"}}}, nil
}
func (s *serviceSpy) Recent(context.Context, string, int) ([]usecase.TransactionView, error) {
	return nil, nil
}
func (s *serviceSpy) Names(context.Context, string) ([]string, []string, error) {
	return []string{"DBS Savings"}, []string{"Groceries"}, nil
}

func (s *serviceSpy) SetNudges(_ context.Context, chatID int64, enabled bool) error {
	s.nudges = append(s.nudges, enabled)
	return s.nudgesErr
}

// parserStub answers every sentence with one intent, and records what it
// was asked so a test can see the names went along.
type parserStub struct {
	intent      usecase.Intent
	err         error
	asked       []usecase.ParseIntentInput
	hadDeadline bool
}

func (p *parserStub) ParseIntent(ctx context.Context, in usecase.ParseIntentInput) (usecase.Intent, error) {
	p.asked = append(p.asked, in)
	_, p.hadDeadline = ctx.Deadline()
	return p.intent, p.err
}

type senderSpy struct {
	mu   sync.Mutex
	sent []string
}

func (s *senderSpy) SendMessage(_ context.Context, _ int64, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, text)
	return nil
}

func owner() domain.Membership {
	return domain.Membership{ID: "m-1", HouseholdID: "h-1", Role: domain.RoleOwner, Capabilities: domain.Capabilities{domain.CapMoney}}
}

func spendCmd() Command {
	return Command{ChatID: 42, UpdateID: 9, Name: "spend", Amount: "84.50", Description: "groceries", Category: "Groceries"}
}

func TestAnUnlinkedChatIsRefusedBeforeAnyServiceCall(t *testing.T) {
	svc, sender := &serviceSpy{}, &senderSpy{}
	c := NewCommander(resolverStub{err: domain.ErrNotFound}, svc, sender)
	c.HandleCommand(context.Background(), spendCmd())
	if len(svc.spends) != 0 || len(sender.sent) != 1 || sender.sent[0] != replyNotLinked {
		t.Fatalf("spends=%d sent=%v", len(svc.spends), sender.sent)
	}
}

func TestALimitedMemberAndAnOwnerWithoutMoneyAreRefused(t *testing.T) {
	limited := domain.Membership{ID: "m-2", HouseholdID: "h-1", Role: domain.RoleLimited, Capabilities: domain.Capabilities{domain.CapMoney}}
	ownerNoMoney := domain.Membership{ID: "m-3", HouseholdID: "h-1", Role: domain.RoleOwner, Capabilities: domain.Capabilities{domain.CapCalendar}}
	for name, m := range map[string]domain.Membership{"limited with money": limited, "owner without money": ownerNoMoney} {
		svc, sender := &serviceSpy{}, &senderSpy{}
		NewCommander(resolverStub{member: m}, svc, sender).HandleCommand(context.Background(), Command{ChatID: 1, Name: "balance"})
		if len(sender.sent) != 1 || sender.sent[0] != replyNotOwner {
			t.Errorf("%s: sent %v", name, sender.sent)
		}
	}
}

func TestAnOwnerSpendReachesTheServiceWithTheUpdateIDAndGetsAReceipt(t *testing.T) {
	svc := &serviceSpy{result: usecase.TelegramSpendResult{
		Transaction: domain.Transaction{Description: "groceries", Amount: domain.Money{Amount: 8450, Currency: "SGD"}},
		AccountName: "DBS Savings", MinorUnits: 2,
	}}
	sender := &senderSpy{}
	NewCommander(resolverStub{member: owner()}, svc, sender).HandleCommand(context.Background(), spendCmd())
	if len(svc.spends) != 1 {
		t.Fatalf("service called %d times", len(svc.spends))
	}
	in := svc.spends[0]
	if in.HouseholdID != "h-1" || in.MembershipID != "m-1" || in.UpdateID != 9 || in.Kind != domain.TransactionExpense || in.AmountText != "84.50" || in.CategoryName != "Groceries" {
		t.Fatalf("service input %+v", in)
	}
	if len(sender.sent) != 1 || sender.sent[0] != "Logged -84.50 SGD — groceries (DBS Savings)" {
		t.Fatalf("receipt %v", sender.sent)
	}
}

func TestAReplayedSpendSaysAlreadyLogged(t *testing.T) {
	svc := &serviceSpy{result: usecase.TelegramSpendResult{
		Transaction: domain.Transaction{Description: "x", Amount: domain.Money{Amount: 100, Currency: "SGD"}}, AccountName: "A", MinorUnits: 2, Replayed: true,
	}}
	sender := &senderSpy{}
	NewCommander(resolverStub{member: owner()}, svc, sender).HandleCommand(context.Background(), spendCmd())
	if !strings.HasPrefix(sender.sent[0], "Already logged") {
		t.Fatalf("got %q", sender.sent[0])
	}
}

func TestRefusalsAreExplainedAndInternalErrorsAreNot(t *testing.T) {
	cases := map[error]string{
		&usecase.ResolutionError{What: "account", Typed: "DBX", Candidates: []string{"DBS", "OCBC"}}:    `No account called "DBX". One of: DBS, OCBC`,
		&usecase.ResolutionError{What: "account", Candidates: []string{"DBS", "OCBC"}, Ambiguous: true}: "Which account? Add @name — one of: DBS, OCBC",
		domain.ErrInvalidMoney:  "That amount could not be read",
		errors.New("pgx: boom"): "That could not be saved",
	}
	for err, want := range cases {
		svc, sender := &serviceSpy{err: err}, &senderSpy{}
		NewCommander(resolverStub{member: owner()}, svc, sender).HandleCommand(context.Background(), spendCmd())
		if len(sender.sent) != 1 || !strings.Contains(sender.sent[0], want) {
			t.Errorf("%v: sent %v, want containing %q", err, sender.sent, want)
		}
		if strings.Contains(sender.sent[0], "pgx") {
			t.Errorf("internal error text leaked to chat: %q", sender.sent[0])
		}
	}
}

func TestSpendWithoutAmountOrDescriptionGetsUsageNotAWrite(t *testing.T) {
	svc, sender := &serviceSpy{}, &senderSpy{}
	c := NewCommander(resolverStub{member: owner()}, svc, sender)
	c.HandleCommand(context.Background(), Command{ChatID: 1, Name: "spend", Amount: "5"})
	c.HandleCommand(context.Background(), Command{ChatID: 1, Name: "income"})
	if len(svc.spends) != 0 || len(sender.sent) != 2 || sender.sent[0] != replyUsageSpend || sender.sent[1] != replyUsageIncome {
		t.Fatalf("spends=%d sent=%v", len(svc.spends), sender.sent)
	}
}

func TestBalanceFormatsEachAccountInItsCurrency(t *testing.T) {
	svc, sender := &serviceSpy{}, &senderSpy{}
	NewCommander(resolverStub{member: owner()}, svc, sender).HandleCommand(context.Background(), Command{ChatID: 1, Name: "balance"})
	if sender.sent[0] != "DBS: SGD 1234.56" {
		t.Fatalf("got %q", sender.sent[0])
	}
}

func TestHelpNeedsNoLinkedAccount(t *testing.T) {
	sender := &senderSpy{}
	NewCommander(resolverStub{err: domain.ErrNotFound}, &serviceSpy{}, sender).HandleCommand(context.Background(), Command{ChatID: 1, Name: "help"})
	if sender.sent[0] != replyHelp {
		t.Fatalf("got %q", sender.sent[0])
	}
}

func TestFreeTextWithoutAParserIsCommandsOnly(t *testing.T) {
	svc, sender := &serviceSpy{}, &senderSpy{}
	NewCommander(resolverStub{member: owner()}, svc, sender).HandleCommand(context.Background(), Command{ChatID: 1, Name: "text", Description: "spent 5 on gum"})
	if len(sender.sent) != 1 || sender.sent[0] != replyNoParser || len(svc.spends) != 0 {
		t.Fatalf("sent %v spends %d", sender.sent, len(svc.spends))
	}
}

func TestFreeTextIsShownBackAndWrittenOnlyOnYes(t *testing.T) {
	svc := &serviceSpy{result: usecase.TelegramSpendResult{
		Transaction: domain.Transaction{Description: "groceries", Amount: domain.Money{Amount: 8450, Currency: "SGD"}}, AccountName: "DBS Savings", MinorUnits: 2,
	}}
	sender := &senderSpy{}
	parser := &parserStub{intent: usecase.Intent{Kind: "expense", Amount: "84.50", Description: "groceries", Account: "DBS Savings", Category: "Groceries"}}
	c := NewCommander(resolverStub{member: owner()}, svc, sender).WithIntentParser(parser)

	c.HandleCommand(context.Background(), Command{ChatID: 42, UpdateID: 500, Name: "text", Description: "spent 84.50 on groceries at DBS"})
	if len(svc.spends) != 0 {
		t.Fatalf("a parsed sentence must not be written before /yes")
	}
	if len(parser.asked) != 1 || parser.asked[0].Accounts[0] != "DBS Savings" || parser.asked[0].Categories[0] != "Groceries" {
		t.Fatalf("parser must receive the household's names: %+v", parser.asked)
	}
	if sender.sent[0] != "Log expense 84.50 — groceries #Groceries @DBS Savings? Reply /yes or /no." {
		t.Fatalf("summary %q", sender.sent[0])
	}

	c.HandleCommand(context.Background(), Command{ChatID: 42, UpdateID: 501, Name: "yes"})
	if len(svc.spends) != 1 {
		t.Fatalf("/yes must write exactly once, wrote %d", len(svc.spends))
	}
	in := svc.spends[0]
	if in.UpdateID != 500 || in.AmountText != "84.50" || in.AccountName != "DBS Savings" || in.CategoryName != "Groceries" || in.Kind != domain.TransactionExpense {
		t.Fatalf("written %+v; the key must be the sentence's update id, not the /yes", in)
	}
	if sender.sent[1] != "Logged -84.50 SGD — groceries (DBS Savings)" {
		t.Fatalf("receipt %q", sender.sent[1])
	}

	c.HandleCommand(context.Background(), Command{ChatID: 42, UpdateID: 502, Name: "yes"})
	if len(svc.spends) != 1 || sender.sent[2] != replyNothingPending {
		t.Fatalf("a second /yes must write nothing: spends=%d sent=%q", len(svc.spends), sender.sent[2])
	}
}

func TestNoDiscardsAndAnExpiredPendingIsNotWritten(t *testing.T) {
	svc, sender := &serviceSpy{}, &senderSpy{}
	parser := &parserStub{intent: usecase.Intent{Kind: "income", Amount: "10", Description: "refund"}}
	c := NewCommander(resolverStub{member: owner()}, svc, sender).WithIntentParser(parser)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	c.HandleCommand(context.Background(), Command{ChatID: 1, UpdateID: 1, Name: "text", Description: "got a 10 refund"})
	c.HandleCommand(context.Background(), Command{ChatID: 1, UpdateID: 2, Name: "no"})
	c.HandleCommand(context.Background(), Command{ChatID: 1, UpdateID: 3, Name: "yes"})
	if len(svc.spends) != 0 || sender.sent[1] != replyDiscarded || sender.sent[2] != replyNothingPending {
		t.Fatalf("spends=%d sent=%v", len(svc.spends), sender.sent)
	}

	c.HandleCommand(context.Background(), Command{ChatID: 1, UpdateID: 4, Name: "text", Description: "got a 10 refund"})
	now = now.Add(pendingTTL + time.Second)
	c.HandleCommand(context.Background(), Command{ChatID: 1, UpdateID: 5, Name: "yes"})
	if len(svc.spends) != 0 || sender.sent[4] != replyNothingPending {
		t.Fatalf("an expired intent must not be written: spends=%d sent=%q", len(svc.spends), sender.sent[4])
	}
}

func TestASentenceThatIsNotATransactionIsNotHeld(t *testing.T) {
	svc, sender := &serviceSpy{}, &senderSpy{}
	c := NewCommander(resolverStub{member: owner()}, svc, sender).WithIntentParser(&parserStub{intent: usecase.Intent{Kind: "none"}})
	c.HandleCommand(context.Background(), Command{ChatID: 1, UpdateID: 1, Name: "text", Description: "hello there"})
	c.HandleCommand(context.Background(), Command{ChatID: 1, UpdateID: 2, Name: "yes"})
	if sender.sent[0] != replyNotUnderstood || sender.sent[1] != replyNothingPending || len(svc.spends) != 0 {
		t.Fatalf("sent %v spends %d", sender.sent, len(svc.spends))
	}
}

// A sentence from a chat that may not write gets silence: no parser call
// (money), no reply (an outbound send a stranger could farm). A slash
// command from the same chat still gets its one-sentence refusal.
func TestAStrangerOrLimitedMembersSentenceIsIgnoredSilently(t *testing.T) {
	limited := domain.Membership{ID: "m-2", HouseholdID: "h-1", Role: domain.RoleLimited, Capabilities: domain.Capabilities{domain.CapMoney}}
	for name, r := range map[string]resolverStub{"stranger": {err: domain.ErrNotFound}, "limited": {member: limited}} {
		parser := &parserStub{intent: usecase.Intent{Kind: "expense", Amount: "5", Description: "x"}}
		sender := &senderSpy{}
		c := NewCommander(r, &serviceSpy{}, sender).WithIntentParser(parser)
		c.HandleCommand(context.Background(), Command{ChatID: 1, Name: "text", Description: "spent 5"})
		if len(parser.asked) != 0 || len(sender.sent) != 0 {
			t.Errorf("%s: asked=%d sent=%v, want nothing", name, len(parser.asked), sender.sent)
		}
		c.HandleCommand(context.Background(), Command{ChatID: 1, Name: "balance"})
		if len(sender.sent) != 1 {
			t.Errorf("%s: a slash command must still be answered, sent %v", name, sender.sent)
		}
	}
}

// The poller handles one update at a time, so a parser that never answers
// would hold every chat. The Commander, not the adapter, sets the ceiling.
func TestTheParserIsAlwaysCalledWithADeadline(t *testing.T) {
	parser := &parserStub{intent: usecase.Intent{Kind: "none"}}
	c := NewCommander(resolverStub{member: owner()}, &serviceSpy{}, &senderSpy{}).WithIntentParser(parser)
	c.HandleCommand(context.Background(), Command{ChatID: 1, Name: "text", Description: "hello"})
	if len(parser.asked) != 1 || !parser.hadDeadline {
		t.Fatalf("asked=%d deadline=%v, want one call with a deadline", len(parser.asked), parser.hadDeadline)
	}
}

func TestNudgesOnOffTogglesThisChatAndAnythingElseIsUsage(t *testing.T) {
	svc := &serviceSpy{}
	sender := &senderSpy{}
	c := NewCommander(resolverStub{member: owner()}, svc, sender)
	for _, arg := range []string{"off", "on", "maybe"} {
		m := &Message{Text: "/nudges " + arg}
		m.Chat.ID = 1
		cmd, ok := ParseCommand(Update{UpdateID: 1, Message: m})
		if !ok || cmd.Name != "nudges" {
			t.Fatalf("/nudges %s parsed as %+v ok=%v", arg, cmd, ok)
		}
		c.HandleCommand(context.Background(), cmd)
	}
	if len(svc.nudges) != 2 || svc.nudges[0] != false || svc.nudges[1] != true {
		t.Fatalf("toggles = %v, want [false true]", svc.nudges)
	}
	if len(sender.sent) != 3 || sender.sent[0] != replyNudgesOff || sender.sent[1] != replyNudgesOn || sender.sent[2] != replyUsageNudges {
		t.Fatalf("replies = %q", sender.sent)
	}
}

// The guard runs before the toggle: a limited member cannot turn on a digest
// that would carry money they may not see.
func TestALimitedMemberCannotToggleNudges(t *testing.T) {
	svc := &serviceSpy{}
	limited := domain.Membership{ID: "m-2", HouseholdID: "h-1", Role: domain.RoleLimited, Capabilities: domain.Capabilities{domain.CapMoney}}
	NewCommander(resolverStub{member: limited}, svc, &senderSpy{}).
		HandleCommand(context.Background(), Command{ChatID: 1, Name: "nudges", Description: "on"})
	if len(svc.nudges) != 0 {
		t.Fatal("a limited member reached SetNudges")
	}
}

// On an install with no digest configured, /nudges is a question with an
// answer, not a failure: the reply says so and nothing is logged as an error.
func TestNudgesOnAnInstallWithoutADigestSaysSo(t *testing.T) {
	svc := &serviceSpy{nudgesErr: usecase.ErrNudgesUnavailable}
	sender := &senderSpy{}
	NewCommander(resolverStub{member: owner()}, svc, sender).
		HandleCommand(context.Background(), Command{ChatID: 1, Name: "nudges", Description: "off"})
	if len(sender.sent) != 1 || sender.sent[0] != replyNudgesUnavailable {
		t.Fatalf("replies = %q, want the not-configured reply", sender.sent)
	}
}
