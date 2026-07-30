package usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// NewTransaction is the create input.
//
// It carries no currency field, deliberately. A transaction is denominated in
// its account's currency, so the service derives it -- a request that could
// name one would be a field a handler accepts and never persists, which is
// the shape four defects in this project have had. The same goes for the
// received amount: only its figure crosses the wire, and its currency comes
// from the destination account.
type NewTransaction struct {
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
//
// ClearReceivedAmount is a separate bool rather than a **int64 because the
// two states a caller needs -- "leave it" and "remove it" -- are otherwise
// indistinguishable from a nil pointer. It is how a transfer that stops
// crossing currencies loses the figure that no longer applies.
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
// AccountDeps. Households and FX are unused by validation -- they exist here
// because MonthSummary (Task 11) is a second method on this same service and
// needs both to convert one currency's spend into the household's primary.
type TransactionDeps struct {
	Transactions TransactionRepository
	Categories   CategoryLookup
	Accounts     AccountLookup
	Households   HouseholdRepository
	FX           FXRateProvider
	Clock        Clock
}

// TransactionService covers the ledger: the transactions themselves and the
// month summary computed from them (monthsummary.go).
//
// It takes no actor parameter, by the rule this codebase follows: services
// enforce what is *valid*, middleware enforces who is *asking*. Every
// transactions route is gated on the money capability and on owner in the
// router.
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
	t := domain.Transaction{
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
		return domain.Transaction{}, err
	}
	return s.d.Transactions.Create(ctx, t)
}

// Update merges the patch onto the stored transaction and validates the
// *result*, never the incoming fields. That ordering is the point: switching
// a kind to transfer and leaving a category alone are each legal in isolation
// and illegal together, so validating the patch would let the pair through.
// AccountService.Update is the same shape for the same reason.
func (s *TransactionService) Update(ctx context.Context, householdID, id string, patch TransactionUpdate) (domain.Transaction, error) {
	view, err := s.d.Transactions.Get(ctx, householdID, id)
	if err != nil {
		return domain.Transaction{}, err
	}
	t := view.Transaction
	// t.ReceivedAmount is a pointer copied from view, not a value -- left
	// alone, validate's currency-normalising write below would mutate
	// whatever the repository handed back, not just this function's local
	// copy. A service merging a patch must never write through to a value it
	// did not allocate itself.
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

// validate normalises and checks an assembled transaction in place. Shared by
// Create and Update so the two cannot drift -- the defect class this project
// has hit repeatedly is a rule fixed at one call site while its sibling keeps
// the bug.
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

	// The account combination the kind requires, mirroring the
	// accounts_match_kind constraint. One sentinel for every wrong shape: the
	// screen shows one message beside the account pickers, and separate errors
	// for "not yours" and "does not exist" would tell a caller which ids are
	// real elsewhere.
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
	// lives on AccountLookup because *postgres.AccountRepo already answers it
	// for account ownership, and a second port asking the same question of the
	// same table would be two answers waiting to disagree.
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

// validateReceivedAmount implements decision 3: required when a transfer
// crosses currencies, optional when it does not (a bank fee), and refused on
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

// accountLookupError turns "there is no such account in this household" into
// the same sentinel every other wrong-account case returns. A distinct error
// here would let a caller tell an id that exists in another household apart
// from one that does not exist at all.
func accountLookupError(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrTransactionAccountsInvalid
	}
	return err
}
