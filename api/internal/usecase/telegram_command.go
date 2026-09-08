package usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// Telegram commands: the household's chat as a second way to write and read
// money, beside the browser and hearthctl. This file is the "what is valid"
// half. The "who is asking" half -- chat → user → membership, then owner and
// the money capability -- lives at the channel's edge in
// adapter/telegram/commands.go, exactly where requireSession/requireOwner
// live for HTTP (ADR 8). Nothing here takes an actor; it takes a household
// and a membership the edge already vouched for.

// TelegramCallerService resolves a chat to the membership behind it. It
// decides nothing: the adapter's guard reads Role and Capabilities off the
// result, the way requireOwner reads them off Scope.
type TelegramCallerService struct {
	Accounts    TelegramAccountRepository
	Memberships MembershipRepository
}

// Resolve reports domain.ErrNotFound for a chat bound to no user, or a user
// with no membership -- the two are one case to the bot ("link your account
// from the app"), and telling them apart would let a chat probe which
// accounts exist.
func (s *TelegramCallerService) Resolve(ctx context.Context, chatID int64) (domain.Membership, error) {
	userID, err := s.Accounts.ByChatID(ctx, chatID)
	if err != nil {
		return domain.Membership{}, err
	}
	return s.Memberships.ByUser(ctx, userID)
}

type TelegramCommandDeps struct {
	Accounts     *AccountService
	Categories   *CategoryService
	Transactions *TransactionService
	Clock        Clock
	// Nudges is nil when the daily digest is not configured; /nudges then
	// answers that there is nothing to turn off.
	Nudges NudgeRepository
}

// ErrNudgesUnavailable is /nudges on an install with no daily digest.
var ErrNudgesUnavailable = errors.New("daily digest is not configured on this install")

type TelegramCommandService struct{ d TelegramCommandDeps }

func NewTelegramCommandService(d TelegramCommandDeps) *TelegramCommandService {
	return &TelegramCommandService{d: d}
}

// TelegramSpend is one /spend or /income as parsed by the adapter: text
// fields, not ids, because a person types names. UpdateID is Telegram's own
// id for the message; it becomes the idempotency key, so an update Telegram
// redelivers after a restart is a replay rather than a second row.
type TelegramSpend struct {
	HouseholdID  string
	MembershipID string
	UpdateID     int64
	Kind         domain.TransactionKind
	AmountText   string
	Description  string
	AccountName  string // "" means "the household's only cash account"
	CategoryName string // "" means no category
}

type TelegramSpendResult struct {
	Transaction domain.Transaction
	AccountName string
	MinorUnits  int
	Replayed    bool
}

// ResolutionError is a refusal the bot can explain: what was looked for and
// what exists. Candidates is what the person could have typed.
type ResolutionError struct {
	What       string // "account" | "category"
	Typed      string
	Candidates []string
	Ambiguous  bool
}

func (e *ResolutionError) Error() string {
	if e.Ambiguous {
		return fmt.Sprintf("%s %q matches more than one", e.What, e.Typed)
	}
	return fmt.Sprintf("unknown %s %q", e.What, e.Typed)
}

// TelegramUpdateKeyPrefix is the idempotency key namespace for chat
// commands; the key is this plus the update id.
const TelegramUpdateKeyPrefix = "telegram-update-"

func (s *TelegramCommandService) LogSpend(ctx context.Context, in TelegramSpend) (TelegramSpendResult, error) {
	account, err := s.resolveAccount(ctx, in.HouseholdID, in.AccountName)
	if err != nil {
		return TelegramSpendResult{}, err
	}
	units := domain.MinorUnitsFor(account.OpeningBalance.Currency)
	amount, err := domain.ParseAmount(in.AmountText, units)
	if err != nil {
		return TelegramSpendResult{}, err
	}
	categoryID := ""
	if in.CategoryName != "" {
		categoryID, err = s.resolveCategory(ctx, in.HouseholdID, in.CategoryName, in.Kind)
		if err != nil {
			return TelegramSpendResult{}, err
		}
	}
	nt := NewTransaction{
		IdempotencyKey:     fmt.Sprintf("%s%d", TelegramUpdateKeyPrefix, in.UpdateID),
		HouseholdID:        in.HouseholdID,
		Kind:               string(in.Kind),
		OccurredOn:         s.d.Clock.Now().UTC().Truncate(24 * time.Hour),
		Description:        in.Description,
		CategoryID:         categoryID,
		PaidByMembershipID: in.MembershipID,
		AmountMinor:        amount,
	}
	switch in.Kind {
	case domain.TransactionExpense:
		nt.FromAccountID = account.ID
	case domain.TransactionIncome:
		nt.ToAccountID = account.ID
	default:
		// Transfers need two accounts and are a form, not a one-liner.
		return TelegramSpendResult{}, domain.ErrTransactionAccountsInvalid
	}
	created, replayed, err := s.d.Transactions.CreateOrReplay(ctx, nt)
	if err != nil {
		return TelegramSpendResult{}, err
	}
	return TelegramSpendResult{Transaction: created, AccountName: account.Nickname, MinorUnits: units, Replayed: replayed}, nil
}

// Balances is /balance: every live account with its current balance.
func (s *TelegramCommandService) Balances(ctx context.Context, householdID string) ([]AccountView, error) {
	return s.d.Accounts.List(ctx, householdID, false)
}

// SetNudges is /nudges on|off. ErrNudgesUnavailable when no digest is
// configured, so the reply can say so rather than pretend to save a choice.
func (s *TelegramCommandService) SetNudges(ctx context.Context, chatID int64, enabled bool) error {
	if s.d.Nudges == nil {
		return ErrNudgesUnavailable
	}
	return s.d.Nudges.SetEnabled(ctx, chatID, enabled)
}

// Recent is /recent: the newest n transactions across every month.
func (s *TelegramCommandService) Recent(ctx context.Context, householdID string, n int) ([]TransactionView, error) {
	views, err := s.d.Transactions.List(ctx, householdID, TransactionFilter{Limit: n})
	if err != nil {
		return nil, err
	}
	if len(views) > n {
		views = views[:n]
	}
	return views, nil
}

// Names is what an IntentParser may choose from: live account nicknames
// and unarchived category names, both kinds. Names rather than ids so the
// parser echoes something the person recognises; resolution to an id is
// LogSpend's job, by the same rule /spend uses.
func (s *TelegramCommandService) Names(ctx context.Context, householdID string) (accounts, categories []string, err error) {
	views, err := s.d.Accounts.List(ctx, householdID, false)
	if err != nil {
		return nil, nil, err
	}
	for _, v := range views {
		accounts = append(accounts, v.Account.Nickname)
	}
	cats, err := s.d.Categories.List(ctx, householdID)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range cats {
		if c.ArchivedAt == nil {
			categories = append(categories, c.Name)
		}
	}
	sort.Strings(accounts)
	sort.Strings(categories)
	return accounts, categories, nil
}

// resolveAccount is the same rule hearthctl's import uses -- an exact,
// case-insensitive name, unambiguous -- plus one convenience a chat needs:
// no name means the household's only cash account, if there is exactly
// one. Anything else is refused with the list, never guessed.
func (s *TelegramCommandService) resolveAccount(ctx context.Context, householdID, name string) (domain.Account, error) {
	views, err := s.d.Accounts.List(ctx, householdID, false)
	if err != nil {
		return domain.Account{}, err
	}
	names := make([]string, 0, len(views))
	for _, v := range views {
		names = append(names, v.Account.Nickname)
	}
	sort.Strings(names)
	if name == "" {
		var cash []domain.Account
		for _, v := range views {
			if v.Account.Type == domain.AccountCash {
				cash = append(cash, v.Account)
			}
		}
		if len(cash) == 1 {
			return cash[0], nil
		}
		return domain.Account{}, &ResolutionError{What: "account", Candidates: names, Ambiguous: len(cash) > 1}
	}
	var matches []domain.Account
	for _, v := range views {
		if strings.EqualFold(strings.TrimSpace(v.Account.Nickname), strings.TrimSpace(name)) {
			matches = append(matches, v.Account)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return domain.Account{}, &ResolutionError{What: "account", Typed: name, Candidates: names}
	default:
		return domain.Account{}, &ResolutionError{What: "account", Typed: name, Candidates: names, Ambiguous: true}
	}
}

func (s *TelegramCommandService) resolveCategory(ctx context.Context, householdID, name string, kind domain.TransactionKind) (string, error) {
	cats, err := s.d.Categories.List(ctx, householdID)
	if err != nil {
		return "", err
	}
	var names []string
	var matches []domain.Category
	for _, c := range cats {
		if c.ArchivedAt != nil || string(c.Kind) != string(kind) {
			continue
		}
		names = append(names, c.Name)
		if strings.EqualFold(strings.TrimSpace(c.Name), strings.TrimSpace(name)) {
			matches = append(matches, c)
		}
	}
	sort.Strings(names)
	switch len(matches) {
	case 1:
		return matches[0].ID, nil
	case 0:
		return "", &ResolutionError{What: "category", Typed: name, Candidates: names}
	default:
		return "", &ResolutionError{What: "category", Typed: name, Candidates: names, Ambiguous: true}
	}
}

// IsResolutionError is the adapter's errors.As shortcut.
func IsResolutionError(err error) (*ResolutionError, bool) {
	var re *ResolutionError
	if errors.As(err, &re) {
		return re, true
	}
	return nil, false
}
