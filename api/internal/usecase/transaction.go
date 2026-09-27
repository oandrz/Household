package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// NewTransaction is the create input. It carries no currency field,
// deliberately: a transaction is denominated in its account's currency, so
// the service derives it -- a field a handler accepts and never persists is
// the shape four defects in this project have had. Same for the received
// amount: only its figure crosses the wire; its currency comes from the
// destination account.
type NewTransaction struct {
	// IdempotencyKey is optional. When set, a repeated create with the same
	// key and the same fields answers the stored row instead of writing a
	// second one -- see CreateOrReplay.
	IdempotencyKey      string
	HouseholdID         string
	Kind                string
	OccurredOn          time.Time
	Description         string
	CategoryID          string
	PaidByMembershipID  string
	FromAccountID       string
	ToAccountID         string
	AmountMinor         int64
	ReceivedAmountMinor *int64
}

// TransactionUpdate is a real patch: a nil pointer means "leave this alone".
// ClearReceivedAmount is a separate bool, not a **int64, because "leave it"
// and "remove it" are otherwise indistinguishable from a nil pointer -- it's
// how a transfer that stops crossing currencies loses the figure that no
// longer applies.
type TransactionUpdate struct {
	Kind                *string
	OccurredOn          *time.Time
	Description         *string
	CategoryID          *string
	PaidByMembershipID  *string
	FromAccountID       *string
	ToAccountID         *string
	AmountMinor         *int64
	ReceivedAmountMinor *int64
	ClearReceivedAmount bool
}

// TransactionDeps gathers every port TransactionService needs, mirroring
// AccountDeps. Households and FX are unused by validation -- they exist
// because MonthSummary, a second method on this same service, needs both to
// convert one currency's spend into the household's primary.
type TransactionDeps struct {
	Transactions TransactionRepository
	Categories   CategoryLookup
	Accounts     AccountLookup
	Households   HouseholdRepository
	FX           FXRateProvider
	Clock        Clock
}

// TransactionService covers the ledger: the transactions themselves and the
// month summary computed from them (monthsummary.go). It takes no actor
// parameter, by the rule this codebase follows: services enforce what is
// *valid*, the channel's inbound edge enforces who is *asking* (ADR 8) --
// every transactions route is gated on the money capability and on owner in
// the router.
type TransactionService struct {
	d TransactionDeps
}

func NewTransactionService(d TransactionDeps) *TransactionService {
	return &TransactionService{d: d}
}

func (s *TransactionService) List(ctx context.Context, householdID string, f TransactionFilter) ([]TransactionView, error) {
	return s.d.Transactions.List(ctx, householdID, f)
}

func (s *TransactionService) Get(ctx context.Context, householdID, id string) (TransactionView, error) {
	return s.d.Transactions.Get(ctx, householdID, id)
}

func (s *TransactionService) Delete(ctx context.Context, householdID, id string) error {
	return s.d.Transactions.Delete(ctx, householdID, id)
}

func (s *TransactionService) Create(ctx context.Context, in NewTransaction) (domain.Transaction, error) {
	created, _, err := s.CreateOrReplay(ctx, in)
	return created, err
}

// CreateOrReplay is Create with the idempotency contract made visible: true
// means the key was already used for this exact transaction and the stored
// row is handed back, not written again. Without a key it's Create.
//
// Insert first, look up second -- never "check, then insert": racing
// retries would both pass a check, but the unique index lets one insert win
// and turns the other into the replay path. A differing stored row for an
// existing key is refused (ErrIdempotencyKeyReused), not silently reported
// "done" for the wrong transaction.
func (s *TransactionService) CreateOrReplay(ctx context.Context, in NewTransaction) (domain.Transaction, bool, error) {
	if in.IdempotencyKey != "" {
		if err := domain.ValidateIdempotencyKey(in.IdempotencyKey); err != nil {
			return domain.Transaction{}, false, err
		}
	}
	t := domain.Transaction{
		IdempotencyKey:     in.IdempotencyKey,
		HouseholdID:        in.HouseholdID,
		Kind:               domain.TransactionKind(in.Kind),
		OccurredOn:         in.OccurredOn,
		Description:        in.Description,
		CategoryID:         in.CategoryID,
		PaidByMembershipID: in.PaidByMembershipID,
		FromAccountID:      in.FromAccountID,
		ToAccountID:        in.ToAccountID,
		Amount:             domain.Money{Amount: in.AmountMinor},
	}
	if in.ReceivedAmountMinor != nil {
		t.ReceivedAmount = &domain.Money{Amount: *in.ReceivedAmountMinor}
	}
	if err := s.validate(ctx, &t); err != nil {
		return domain.Transaction{}, false, err
	}
	created, err := s.d.Transactions.Create(ctx, t)
	if err == nil {
		return created, false, nil
	}
	if t.IdempotencyKey == "" || !errors.Is(err, domain.ErrIdempotencyKeyInUse) {
		return domain.Transaction{}, false, err
	}
	stored, lookupErr := s.d.Transactions.GetByIdempotencyKey(ctx, t.HouseholdID, t.IdempotencyKey)
	if lookupErr != nil {
		// The index said the key exists and the lookup says it does not:
		// the row was deleted between the two calls. Report the original
		// refusal rather than inventing an answer; the caller's retry will
		// simply create it.
		return domain.Transaction{}, false, fmt.Errorf("replay lookup after %w: %v", err, lookupErr)
	}
	if !t.SameCreate(stored) {
		return domain.Transaction{}, false, domain.ErrIdempotencyKeyReused
	}
	return stored, true, nil
}

// Update merges the patch onto the stored transaction and validates the
// *result*, never the incoming fields: switching a kind to transfer and
// leaving a category alone are each legal alone and illegal together, so
// validating the patch would let the pair through. AccountService.Update is
// the same shape for the same reason.
func (s *TransactionService) Update(ctx context.Context, householdID, id string, patch TransactionUpdate) (domain.Transaction, error) {
	view, err := s.d.Transactions.Get(ctx, householdID, id)
	if err != nil {
		return domain.Transaction{}, err
	}
	t := view.Transaction
	// t.ReceivedAmount is a pointer copied from view, not a value: left
	// alone, validate's currency-normalising write below would mutate the
	// repository's own copy, not just this function's. Never write through
	// to a value you didn't allocate.
	if t.ReceivedAmount != nil {
		received := *t.ReceivedAmount
		t.ReceivedAmount = &received
	}

	if patch.Kind != nil {
		t.Kind = domain.TransactionKind(*patch.Kind)
	}
	if patch.OccurredOn != nil {
		t.OccurredOn = *patch.OccurredOn
	}
	if patch.Description != nil {
		t.Description = *patch.Description
	}
	if patch.CategoryID != nil {
		t.CategoryID = *patch.CategoryID
	}
	if patch.PaidByMembershipID != nil {
		t.PaidByMembershipID = *patch.PaidByMembershipID
	}
	if patch.FromAccountID != nil {
		t.FromAccountID = *patch.FromAccountID
	}
	if patch.ToAccountID != nil {
		t.ToAccountID = *patch.ToAccountID
	}
	if patch.AmountMinor != nil {
		t.Amount.Amount = *patch.AmountMinor
	}
	// ClearReceivedAmount takes priority over ReceivedAmountMinor so a caller
	// can never accidentally undo a clear by sending both in one patch -- there
	// is no legitimate request that means both "remove it" and "set it".
	if patch.ClearReceivedAmount {
		t.ReceivedAmount = nil
	} else if patch.ReceivedAmountMinor != nil {
		t.ReceivedAmount = &domain.Money{Amount: *patch.ReceivedAmountMinor}
	}

	if err := s.validate(ctx, &t); err != nil {
		return domain.Transaction{}, err
	}
	return s.d.Transactions.Update(ctx, t)
}

// validate normalises and checks an assembled transaction in place, shared
// by Create and Update so a rule can't be fixed at one call site and missed
// in its sibling -- the defect class this project keeps hitting.
func (s *TransactionService) validate(ctx context.Context, t *domain.Transaction) error {
	kind, err := domain.ParseTransactionKind(string(t.Kind))
	if err != nil {
		return err
	}
	t.Kind = kind

	t.Description = strings.TrimSpace(t.Description)
	if t.Description == "" {
		return domain.ErrTransactionDescriptionRequired
	}

	if t.Amount.Amount <= 0 {
		return domain.ErrTransactionAmountNotPositive
	}

	// The account combination the kind requires mirrors accounts_match_kind.
	// One sentinel for every wrong shape: separate errors for "not yours" vs
	// "does not exist" would tell a caller which ids are real.
	switch t.Kind {
	case domain.TransactionExpense:
		if t.FromAccountID == "" || t.ToAccountID != "" {
			return domain.ErrTransactionAccountsInvalid
		}
	case domain.TransactionIncome:
		if t.ToAccountID == "" || t.FromAccountID != "" {
			return domain.ErrTransactionAccountsInvalid
		}
	case domain.TransactionTransfer:
		if t.FromAccountID == "" || t.ToAccountID == "" || t.FromAccountID == t.ToAccountID {
			return domain.ErrTransactionAccountsInvalid
		}
	default:
		// Unreachable while ParseTransactionKind above admits only these three.
		// A fourth kind added there without a rule here must refuse rather
		// than skip the account check it has no case for.
		return fmt.Errorf("transaction: no account rule for kind %q: %w", t.Kind, domain.ErrUnknownTransactionKind)
	}

	// The currencies come from the accounts, never from the request.
	var fromCurrency, toCurrency string
	if t.FromAccountID != "" {
		view, err := s.d.Accounts.Get(ctx, t.HouseholdID, t.FromAccountID)
		if err != nil {
			return accountLookupError(err)
		}
		fromCurrency = view.Balance.Currency
	}
	if t.ToAccountID != "" {
		view, err := s.d.Accounts.Get(ctx, t.HouseholdID, t.ToAccountID)
		if err != nil {
			return accountLookupError(err)
		}
		toCurrency = view.Balance.Currency
	}

	// The amount is denominated in the account money left, or arrived in.
	if fromCurrency != "" {
		t.Amount.Currency = fromCurrency
	} else {
		t.Amount.Currency = toCurrency
	}

	if err := s.validateReceivedAmount(t, fromCurrency, toCurrency); err != nil {
		return err
	}

	if err := s.validateCategory(ctx, t); err != nil {
		return err
	}

	// An account can only be paid for by someone in this household. The check
	// lives on AccountLookup, not a new port: *postgres.AccountRepo already
	// answers ownership, and a second port could disagree with it.
	if t.PaidByMembershipID != "" {
		ok, err := s.d.Accounts.MembershipBelongsToHousehold(ctx, t.HouseholdID, t.PaidByMembershipID)
		if err != nil {
			return err
		}
		if !ok {
			return domain.ErrTransactionAccountsInvalid
		}
	}
	return nil
}

// validateReceivedAmount requires a received amount when a transfer crosses
// currencies, allows one when it does not (a bank fee), and refuses one on
// anything that is not a transfer.
func (s *TransactionService) validateReceivedAmount(t *domain.Transaction, fromCurrency, toCurrency string) error {
	if t.Kind != domain.TransactionTransfer {
		if t.ReceivedAmount != nil {
			return domain.ErrReceivedAmountNotAllowed
		}
		return nil
	}
	if t.ReceivedAmount == nil {
		if fromCurrency != toCurrency {
			return domain.ErrReceivedAmountRequired
		}
		return nil
	}
	if t.ReceivedAmount.Amount <= 0 {
		return domain.ErrTransactionAmountNotPositive
	}
	t.ReceivedAmount.Currency = toCurrency
	return nil
}

// validateCategory keeps the ledger's promise that a category feeds Budget
// spend: a transfer is not spend, and an income is not Groceries.
func (s *TransactionService) validateCategory(ctx context.Context, t *domain.Transaction) error {
	if t.CategoryID == "" {
		return nil
	}
	if t.Kind == domain.TransactionTransfer {
		return domain.ErrCategoryKindMismatch
	}
	ok, err := s.d.Categories.BelongsToHousehold(ctx, t.HouseholdID, t.CategoryID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrCategoryKindMismatch
	}
	kind, err := s.d.Categories.Kind(ctx, t.HouseholdID, t.CategoryID)
	if err != nil {
		return err
	}
	want := domain.CategoryExpense
	if t.Kind == domain.TransactionIncome {
		want = domain.CategoryIncome
	}
	if kind != want {
		return domain.ErrCategoryKindMismatch
	}
	return nil
}

// accountLookupError turns "there is no such account in this household"
// into the shared wrong-account sentinel -- a distinct error would leak
// whether an id exists in another household or not at all.
func accountLookupError(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrTransactionAccountsInvalid
	}
	return err
}
