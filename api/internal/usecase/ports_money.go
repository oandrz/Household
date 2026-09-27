// This file holds the money slice's core ports: exchange rates, accounts,
// transactions, categories and budgets. ports.go lists every ports file.

package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// FXRateProvider looks up the rate between two currencies. The design labels
// the rate "auto"; a live provider replaces the static one without any caller
// changing.
//
// For a pair it has no rate for, it returns an error wrapping domain.ErrNoRate,
// and callers rely on that: it is the only failure a screen may answer by
// leaving an amount out of a total. Any other error means the lookup itself
// failed (a provider outage, a cancelled request), and the caller fails the
// request. Callers do not use this directly for arithmetic; they build a
// Converter (converter.go) per request.
type FXRateProvider interface {
	Rate(ctx context.Context, from, to string) (domain.Rate, error)
}

// AccountView is an account joined to its owner's display name, which is what
// every consumer of the accounts list actually wants -- the same shape and the
// same reason as MemberView.
//
// Balance is the account's current balance: its opening balance plus every
// transaction dated on or after Account.OpeningBalanceAsOf, summed by the
// repository. It is denominated in the account's own currency, because every
// transaction on an account is; nothing here converts.
//
// It is a separate field from Account.OpeningBalance, and the two are
// different numbers as soon as an account has a transaction on it. Balance
// answers "what does this hold now"; Account.OpeningBalance answers "what did
// someone assert it held on Account.OpeningBalanceAsOf", and is the only one
// of the two a caller may ever write back. Reading Balance and storing it as
// an opening balance moves the household's net worth by every transaction
// since -- a real defect this project shipped in the account edit form, see
// docs/LEARNING.md.
//
// OwnerName is "" for a shared account, following the same "" <-> SQL NULL
// convention as domain.Account.OwnerMembershipID.
type AccountView struct {
	Account   domain.Account
	OwnerName string
	Balance   domain.Money
}

// AccountMonthMovement is one account's net movement across one calendar
// month, in that account's own currency. It is the twelve-month net worth
// chart's only new input.
//
// Delta is signed: money leaving the account is negative, money arriving is
// positive, and a month with no movement produces no row rather than a zero
// one -- a caller reads an absent month as "nothing moved", which is what an
// absent row means.
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
	// since onward, counting only transactions dated on or after that
	// account's own opening_balance_as_of -- the same filter
	// AccountView.Balance is computed with. The two must stay the same: the
	// trend walks backwards from Balance by subtracting these, so a filter
	// that differs by one row makes the older bars wrong and plausible at the
	// same time.
	//
	// There is no upper bound on the transaction date, deliberately. Balance
	// has none either, so a future-dated transaction is already inside the
	// figure the walk anchors on and must be inside these rows too.
	//
	// Archived accounts are included; the caller decides what counts, exactly
	// as it does for Balance.
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

// AccountLookup is what TransactionService needs of accounts: the currency an
// account is denominated in, whether it belongs to this household, and
// whether a membership does too, for the paid-by check. Get returns
// domain.ErrNotFound for an account in another household, the same as
// AccountRepository.Get above -- "that account is not yours" must be
// indistinguishable from "there is no such account" here as well.
//
// *postgres.AccountRepo already satisfies this: both methods exist on it
// already, for AccountRepository above.
type AccountLookup interface {
	Get(ctx context.Context, householdID, accountID string) (AccountView, error)
	MembershipBelongsToHousehold(ctx context.Context, householdID, membershipID string) (bool, error)
}

// TransactionView is a transaction joined to the names the ledger displays --
// its category, who paid, and each account's nickname. Same shape and same
// reason as MemberView and AccountView: every consumer of the list wants
// the names, and re-reading them per row is a query per row.
//
// The two Before...Opening fields answer whether this transaction predates the
// opening-balance date of the account on that side, and so does not move that
// account's balance. Each is nil when there is no account on that side.
//
// It is two fields rather than one because a transfer has two accounts with
// two different opening dates: it can predate one and not the other, moving
// one balance and leaving the other alone. A single flag would mark such a row
// with a note that is half true. The server answers this rather than the
// frontend recomputing it, so the rule lives in exactly one place.
type TransactionView struct {
	Transaction     domain.Transaction
	CategoryName    string
	PaidByName      string
	FromAccountName string
	ToAccountName   string

	BeforeFromAccountOpening *bool
	BeforeToAccountOpening   *bool
}

// TransactionFilter is the design's five filters plus paging. An empty field
// means no filtering on it, following the same "" <-> unset convention the
// rest of this file uses.
//
// AccountID matches a transaction on *either* side. A filter that only matched
// from_account_id would hide money arriving in the account someone selected,
// which is half of what they were looking for.
//
// Paging is keyset, not offset: CursorDate and CursorID are the last row of
// the previous page, and the query asks for rows ordered after that pair.
// Offset paging shifts every later row by one when a transaction is added
// mid-scroll, so a page boundary silently repeats or skips a transaction.
type TransactionFilter struct {
	Kind               string
	AccountID          string
	CategoryID         string
	PaidByMembershipID string
	// Month is any instant inside the calendar month to list. Zero means every
	// month -- but note the HTTP adapter never sends zero by default: an absent
	// `month` parameter defaults to the current month, and only an explicit
	// `month=all` produces zero here (see parseTransactionFilter).
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
	//
	// It is idempotent and safe to run concurrently: one INSERT ... ON
	// CONFLICT DO NOTHING against UNIQUE (household_id, name), never a
	// read-then-write, which would race two simultaneous first requests into
	// two starter sets.
	//
	// An archived category still occupies its unique key. An implementation
	// must count it as already seeded -- never treat "no live categories" as
	// "has none" -- so a household that cleared its whole list is not
	// silently re-seeded over; the unique key is the backstop of last resort,
	// for any path that reaches the insert without going through that count
	// at all.
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
	// List returns one household's transactions, newest first, matching every
	// filter that is set. It returns at most f.Limit+1 rows so the caller can
	// tell whether another page exists without a second query.
	//
	// f.Limit <= 0 is treated as 50, and any f.Limit above 200 is clamped down
	// to it -- both are the implementation's own constants, not configurable,
	// so a caller (the GET /transactions handler) that passes through an
	// unvalidated request-provided limit must know it can get back at most 201
	// rows, not limit+1, and that "no limit sent" does not mean "no cap
	// applied."
	List(ctx context.Context, householdID string, f TransactionFilter) ([]TransactionView, error)
	// Get reports domain.ErrNotFound when no transaction with this id exists
	// in this household -- including when one exists in a different household,
	// which must be indistinguishable from not existing at all.
	Get(ctx context.Context, householdID, transactionID string) (TransactionView, error)
	// Create writes the "" <-> SQL NULL convention for every optional id:
	// category, payer, and whichever account side the kind leaves empty --
	// and for t.IdempotencyKey, which is stored NULL when "". t.ID is
	// ignored; the database assigns it. A non-empty key this household has
	// already stored reports domain.ErrIdempotencyKeyInUse and writes
	// nothing; the service decides whether that is a replay.
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
	// MonthTotals returns every transaction in one calendar month, which the
	// service converts and sums.
	//
	// It returns rows rather than a SQL SUM deliberately, and the bound is one
	// household's transactions in one month -- the design's own busiest
	// example is 247. A SQL SUM would be correct only for a household whose
	// transactions are all in its primary currency; having two code paths
	// whose answers could disagree is the trade this refuses. The FX provider
	// lives in this layer, so the conversion cannot move down here anyway.
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
	// domain.ErrNotFound when the month has no budget row at all — a state
	// Budget decision 4 makes reachable, since a closed month can have spend
	// and no caps.
	RollOverToGoal(ctx context.Context, in RollOverToGoalInput) (domain.GoalContribution, error)
}
