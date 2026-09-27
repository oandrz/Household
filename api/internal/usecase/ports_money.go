// This file holds the money slice's core ports: exchange rates, accounts,
// transactions, categories and budgets. ports.go lists every ports file.

package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// FXRateProvider looks up the rate between two currencies. The design labels
// the rate "auto"; a live provider replaces the static one with no caller
// change.
//
// It returns an error wrapping domain.ErrNoRate for a pair it has no rate
// for -- the only failure a screen may answer by leaving an amount out of a
// total. Any other error means the lookup itself failed (outage, cancelled
// request), and the caller fails the whole request. Callers never use this
// directly for arithmetic; they build a Converter (converter.go) per
// request.
type FXRateProvider interface {
	Rate(ctx context.Context, from, to string) (domain.Rate, error)
}

// AccountView is an account joined to its owner's display name -- the same
// shape and reason as MemberView. Balance is the opening balance plus every
// transaction dated on or after Account.OpeningBalanceAsOf, summed by the
// repository, in the account's own currency because every transaction on an
// account is; nothing here converts.
//
// Balance and Account.OpeningBalance are different numbers once an account has
// a transaction: Balance answers "what does this hold now," OpeningBalance
// answers "what did someone assert it held on OpeningBalanceAsOf," and
// OpeningBalance is the only one of the two a caller may ever write back.
// Don't write Balance back as an opening balance -- it moves the household's
// net worth by every transaction since, a real defect this project shipped in
// the account edit form (docs/LEARNING.md).
//
// OwnerName is "" for a shared account, the same "" <-> SQL NULL convention as
// domain.Account.OwnerMembershipID.
type AccountView struct {
	Account   domain.Account
	OwnerName string
	Balance   domain.Money
}

// AccountMonthMovement is one account's net movement across one calendar
// month, in that account's own currency -- the twelve-month net worth
// chart's only new input.
//
// Delta is signed: leaving is negative, arriving is positive. A month with
// no movement produces no row, not a zero one; a caller reads an absent
// month as "nothing moved."
//
// Month is the first of the month at midnight, so two values for the same
// month compare equal.
type AccountMonthMovement struct {
	AccountID string
	Month     time.Time
	Delta     domain.Money
}

type AccountRepository interface {
	// List returns one household's accounts, ordered oldest first. Archived
	// accounts are included only when includeArchived is true, and never
	// contribute to any total regardless.
	List(ctx context.Context, householdID string, includeArchived bool) ([]AccountView, error)
	// MonthlyMovements returns every account's per-month net movement from
	// since onward, filtered by transactions dated on or after that account's
	// own opening_balance_as_of -- the same filter AccountView.Balance uses,
	// and the two must stay the same, since the trend walks backwards from
	// Balance by subtracting these rows, so a one-row difference makes an older
	// bar wrong and plausible at once. There is deliberately no upper bound on
	// the transaction date either, matching Balance: a future-dated transaction
	// already counts in that figure, so it must count here too. Archived
	// accounts are included; the caller decides what counts, as it does for
	// Balance.
	MonthlyMovements(ctx context.Context, householdID string, since time.Time) ([]AccountMonthMovement, error)
	// Get reports domain.ErrNotFound when no account with this id exists in
	// this household -- including when one exists in a different household,
	// which must be indistinguishable from not existing at all.
	Get(ctx context.Context, householdID, accountID string) (AccountView, error)
	// Create writes a.OwnerMembershipID following the "" <-> SQL NULL
	// convention: "" stores NULL, meaning shared. a.ID and a.ArchivedAt are
	// ignored -- the database assigns the first and a new account is never
	// born archived.
	Create(ctx context.Context, a domain.Account) (domain.Account, error)
	// Update replaces every mutable column. AccountService is what turns a
	// partial PATCH into a complete Account; this port never merges.
	Update(ctx context.Context, a domain.Account) (domain.Account, error)
	// SetArchived stamps archived_at with at, or clears it when archived is
	// false. Accounts are never deleted: transactions will reference these
	// rows, and destroying an account would take its history with it.
	SetArchived(ctx context.Context, householdID, accountID string, archived bool, at time.Time) (domain.Account, error)
	// MembershipBelongsToHousehold answers whether a membership is in this
	// household, so an account can never be assigned to a member of another
	// one. It lives here rather than on MembershipRepository because that port
	// is already consumed by sign-in and does not need widening for this.
	MembershipBelongsToHousehold(ctx context.Context, householdID, membershipID string) (bool, error)
}

// AccountLookup is what TransactionService needs of accounts: the currency
// an account is denominated in, whether it belongs to this household, and
// whether a membership does too, for the paid-by check. Get returns
// domain.ErrNotFound for an account in another household, same as
// AccountRepository.Get -- "not yours" must be indistinguishable from "no
// such account."
//
// *postgres.AccountRepo already satisfies this.
type AccountLookup interface {
	Get(ctx context.Context, householdID, accountID string) (AccountView, error)
	MembershipBelongsToHousehold(ctx context.Context, householdID, membershipID string) (bool, error)
}

// TransactionView is a transaction joined to the names the ledger displays --
// its category, who paid, and each account's nickname. Same shape and reason
// as MemberView and AccountView: every consumer wants the names, and
// re-reading them per row would be a query per row.
//
// BeforeFromAccountOpening and BeforeToAccountOpening say whether the
// transaction predates that side's opening-balance date, so it doesn't move
// that account's balance; nil when there's no account on that side. Two
// fields, not one: a transfer's two accounts can have different opening dates,
// so a transaction can predate one and not the other, and one flag would be
// half true. The server computes this once rather than the frontend
// recomputing it, so the rule lives in one place.
type TransactionView struct {
	Transaction     domain.Transaction
	CategoryName    string
	PaidByName      string
	FromAccountName string
	ToAccountName   string

	BeforeFromAccountOpening *bool
	BeforeToAccountOpening   *bool
}

// TransactionFilter is the design's five filters plus paging. An empty
// field means no filtering on it, the same "" <-> unset convention this
// file uses elsewhere.
//
// AccountID matches a transaction on *either* side -- matching only
// from_account_id would hide money arriving in the selected account, half
// of what the caller is looking for.
//
// Paging is keyset, not offset: CursorDate and CursorID are the previous
// page's last row, and the query asks for rows ordered after that pair.
// Offset paging shifts every later row when a transaction is added
// mid-scroll, silently repeating or skipping one across a page boundary.
type TransactionFilter struct {
	Kind               string
	AccountID          string
	CategoryID         string
	PaidByMembershipID string
	// Month is any instant inside the calendar month to list. Zero means
	// every month, but the HTTP adapter never sends zero by default: an
	// absent `month` defaults to the current month, and only `month=all`
	// produces zero here (see parseTransactionFilter).
	Month time.Time

	CursorDate time.Time
	CursorID   string
	Limit      int
}

type CategoryRepository interface {
	// List returns one household's categories in sort_order. Archived
	// categories are included only when includeArchived is true.
	List(ctx context.Context, householdID string, includeArchived bool) ([]domain.Category, error)
	// EnsureSeeded creates the starter set for a household that has none.
	// Idempotent and safe to run concurrently: one INSERT ... ON CONFLICT DO
	// NOTHING against UNIQUE (household_id, name), never a read-then-write,
	// which would race two simultaneous first requests into two starter sets.
	// An archived category still occupies its unique key, so an implementation
	// must count it as already seeded -- never treat "no live categories" as
	// "has none," or a household that archived its whole list gets silently
	// re-seeded. The unique key is the backstop of last resort for any path
	// that reaches the insert directly.
	EnsureSeeded(ctx context.Context, householdID string, starter []domain.Category) error
	// Create adds one category at the end of the household's sort order.
	// A name colliding with UNIQUE (household_id, name) — archived rows
	// included — surfaces as domain.ErrCategoryNameTaken.
	Create(ctx context.Context, c domain.Category) (domain.Category, error)
	// Rename changes the name only, same collision contract as Create.
	// domain.ErrNotFound when the id is not this household's.
	Rename(ctx context.Context, householdID, categoryID, name string) (domain.Category, error)
	// SetArchived stamps or clears archived_at. Archiving is idempotent,
	// keeps every transaction and budget line referencing the row, and is
	// the only removal that exists — there is no delete.
	SetArchived(ctx context.Context, householdID, categoryID string, archived bool) (domain.Category, error)
}

// CategoryLookup is what TransactionService needs of categories: whether an
// id is one of this household's, and what kind it is. Narrow on purpose -- it
// does not need List or EnsureSeeded, and a port that hands it those is a
// port that invites a service to seed as a side effect of validation.
type CategoryLookup interface {
	BelongsToHousehold(ctx context.Context, householdID, categoryID string) (bool, error)
	Kind(ctx context.Context, householdID, categoryID string) (domain.CategoryKind, error)
}

type TransactionRepository interface {
	// List returns one household's transactions, newest first, matching
	// every set filter. It returns at most f.Limit+1 rows so the caller can
	// tell whether another page exists without a second query.
	//
	// f.Limit <= 0 is treated as 50, and anything above 200 is clamped to
	// it -- both are fixed constants, not configurable. A caller (the GET
	// /transactions handler) that passes through an unvalidated limit gets
	// back at most 201 rows, not limit+1, and "no limit sent" does not mean
	// "no cap applied."
	List(ctx context.Context, householdID string, f TransactionFilter) ([]TransactionView, error)
	// Get reports domain.ErrNotFound when no transaction with this id exists
	// in this household -- including when one exists in a different household,
	// which must be indistinguishable from not existing at all.
	Get(ctx context.Context, householdID, transactionID string) (TransactionView, error)
	// Create writes the "" <-> SQL NULL convention for every optional id --
	// category, payer, whichever account side the kind leaves empty -- and
	// for t.IdempotencyKey, stored NULL when "". t.ID is ignored; the
	// database assigns it. A non-empty key already stored for this
	// household reports domain.ErrIdempotencyKeyInUse and writes nothing;
	// the service decides whether that's a replay.
	Create(ctx context.Context, t domain.Transaction) (domain.Transaction, error)
	// GetByIdempotencyKey is the replay lookup after Create reported
	// ErrIdempotencyKeyInUse. Household-scoped: another household's row
	// under the same key is domain.ErrNotFound, indistinguishable from no
	// row at all.
	GetByIdempotencyKey(ctx context.Context, householdID, key string) (domain.Transaction, error)
	// Update replaces every mutable column. TransactionService is what turns a
	// partial PATCH into a complete Transaction; this port never merges.
	Update(ctx context.Context, t domain.Transaction) (domain.Transaction, error)
	// Delete removes the row, and reports domain.ErrNotFound when there was
	// none to remove. Nothing references a transaction, so nothing is
	// orphaned -- which is why this differs from accounts, where SetArchived
	// exists and no delete does.
	Delete(ctx context.Context, householdID, transactionID string) error
	// MonthTotals returns every transaction in one calendar month; the
	// service converts and sums them.
	//
	// Rows, not a SQL SUM, deliberately: the design's busiest example is
	// 247 rows for one household-month, and a SUM would only be correct if
	// every transaction were in the household's primary currency -- two
	// code paths that could disagree is the trade this refuses. The FX
	// provider lives in this layer anyway, so the conversion can't move
	// down here.
	MonthTotals(ctx context.Context, householdID string, month time.Time) ([]TransactionView, error)
}

// RollOverToGoalInput is what one rollover needs. Note is deliberately absent:
// the row's note stays empty and the frontend renders "From July's unspent
// budget" from source + sourceBudgetMonth, because user-facing copy does not
// belong in a Go handler.
type RollOverToGoalInput struct {
	HouseholdID string
	Month       time.Time // the budget month being rolled over
	GoalID      string
	Amount      domain.Money
	OccurredOn  time.Time
}

type BudgetRepository interface {
	// Get returns one household-month's budget. domain.ErrNotFound means the
	// month has never been budgeted — callers translate that to the empty
	// state, not an error. month is any instant in the month.
	Get(ctx context.Context, householdID string, month time.Time) (domain.Budget, error)
	// Upsert replaces the month's budget wholesale in one transaction:
	// parent row upserted on (household_id, month), lines deleted and
	// rewritten. Full-replace, never merge — the modal always holds the
	// entire budget, and replace makes removed rows unambiguous. b.ID and
	// line IDs are ignored; the database assigns them.
	Upsert(ctx context.Context, b domain.Budget) (domain.Budget, error)
	// History returns the budgets for the closed months in [from, month),
	// plus the viewed month if budgeted — newest first, months without a
	// budget row simply absent, never zero-filled.
	History(ctx context.Context, householdID string, month time.Time, months int) ([]domain.Budget, error)
	// RollOverToGoal writes a budget month's unspent money into a goal as one
	// contribution and stamps the month, in ONE transaction. The stamp is set
	// by a conditional UPDATE (... AND rolled_over_at IS NULL), so a second
	// concurrent call finds no row to update and gets
	// domain.ErrRolloverAlreadyDone rather than writing a second contribution.
	// domain.ErrNotFound when the month has no budget row at all — reachable
	// since a closed month can have spend and no caps.
	RollOverToGoal(ctx context.Context, in RollOverToGoalInput) (domain.GoalContribution, error)
}
