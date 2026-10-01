// This file holds the money slice's investment-holding ports. ports.go
// lists every ports file.

package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// HoldingRecord is a holding joined to the account it sits in, which is what
// every consumer of the holdings list actually wants -- the same shape and the
// same reason as AccountView and MemberView.
type HoldingRecord struct {
	Holding         domain.Holding
	AccountName     string
	AccountArchived bool
}

// HoldingRepository stores what a household owns inside its investment
// accounts. It is deliberately separate from AccountRepository and touches
// no balance: a holding is invisible to ListAccounts, to net worth, and to
// the twelve-month trend. Making an account's balance read from holdings is
// a later, separate decision about whether an investment account also
// carries uninvested cash.
type HoldingRepository interface {
	// List returns one household's holdings ordered by name, each joined to its
	// account's nickname and whether that account is archived -- an archived
	// account's holdings are still real money and stay listed, with the account
	// labelled. includeArchived is a UNION, not a filter swap: false returns
	// the live holdings, true returns live AND archived together, each carrying
	// its own ArchivedAt -- the AccountRepository.List / GoalRepository.List
	// contract. Don't implement it as "archived instead".
	List(ctx context.Context, householdID string, includeArchived bool) ([]HoldingRecord, error)
	// Get reports domain.ErrNotFound when no holding with this id exists in
	// this household -- including when it exists in another one, so nothing
	// leaks the existence of another household's rows.
	Get(ctx context.Context, householdID, holdingID string) (domain.Holding, error)
	// Create reports domain.ErrHoldingNameTaken on a name collision within
	// the same ACCOUNT, archived holdings included.
	Create(ctx context.Context, h domain.Holding) (domain.Holding, error)
	// Update changes name, instrument and unit only. Currency and AccountID
	// are not mutable: a holding's currency is what every one of its events
	// is denominated in, and moving a holding between accounts would move
	// money between accounts with no ledger row to say so.
	Update(ctx context.Context, h domain.Holding) (domain.Holding, error)
	// SetArchived archives (non-nil) or restores (nil). A holding is never
	// deleted: its events and valuations reference it, and a sold-out
	// position is still part of the year's realised profit.
	SetArchived(ctx context.Context, householdID, holdingID string, archivedAt *time.Time) (domain.Holding, error)
	// CountLiveForAccount is what stops an account's type being changed out
	// from under its holdings. AccountService patches Type freely, so without
	// this a cash account could end up holding 300g of gold.
	CountLiveForAccount(ctx context.Context, householdID, accountID string) (int64, error)
}

// HoldingCounter is what services OUTSIDE this feature need to know about
// holdings, and nothing more: whether an account still holds anything, and
// whether the household holds anything at all. Narrow on purpose, the same
// interface-segregation rule behind nine small repositories instead of one
// object with forty methods -- so the accounts and household services
// cannot grow a dependency on holdings they were never meant to have.
type HoldingCounter interface {
	CountLiveForAccount(ctx context.Context, householdID, accountID string) (int64, error)
	// CountForHousehold counts ARCHIVED holdings too. An archived holding
	// still has events, and those events still have to fold -- archiving is
	// how a household stops looking at something, not how it forgets it.
	CountForHousehold(ctx context.Context, householdID string) (int64, error)
}

// HoldingFold is the rule a write to one holding must pass before anything
// is stored. The service supplies it; the repository calls it once, while it
// holds the lock on the holding's row, and writes only if it returns nil. An
// error it returns goes back to the caller unchanged, with nothing written.
//
// Both arguments are the holding's rows as they WOULD be after this write,
// read inside the lock:
//
//   - events: every acquisition and disposal, in
//     HoldingEventRepository.ListByHolding's order. An insert has the new
//     event appended last; a delete has the removed event taken out; a price
//     write leaves them as stored.
//   - prices: every valuation, in no promised order. A price write has the
//     new price in the list and the row it replaces (the same calendar day)
//     taken out; an event write leaves them as stored.
//
// Why the repository reads the prices instead of the service passing them
// in: a service that read them first could miss a price committed between
// its read and the lock. A large purchase and a large price are each legal
// alone, so both writers would pass and both commit.
//
// A fold does no I/O and calls no repository: it runs while the transaction
// holds a row lock and a pool connection.
type HoldingFold func(events []domain.HoldingEvent, prices []domain.Valuation) error

// HoldingEventRepository stores the acquisitions and disposals a holding is
// made of. Income (a dividend) is deliberately not among them: it changes
// neither what is held nor what it cost, so folding it here would corrupt the
// average cost.
type HoldingEventRepository interface {
	// ListByHolding returns one holding's events ordered by (OccurredOn,
	// CreatedAt, ID). That order is a contract, not a preference -- an
	// implementation may not relax it.
	//
	// OccurredOn is a date, so two events can share one (buying and selling
	// the same morning is ordinary), and domain.Holding.Position sorts
	// stably, keeping whatever order it's handed for a tie. The tie is
	// therefore broken here, by recording order. Any other order silently
	// changes a household's realised gain: on identical same-day events,
	// buy-then-sell realises 750 where sell-then-buy realises 1000.
	ListByHolding(ctx context.Context, householdID, holdingID string) ([]domain.HoldingEvent, error)
	// ListByHousehold is the same contract across every holding, grouped by
	// holding, so a portfolio page folds every position without one query
	// per holding.
	ListByHousehold(ctx context.Context, householdID string) ([]domain.HoldingEvent, error)
	Insert(ctx context.Context, e domain.HoldingEvent) (domain.HoldingEvent, error)
	// InsertWithFold is Insert with the holding's rules held ACROSS the write;
	// a service must use it for anything the fold can refuse. The
	// implementation locks the holding, reads its events and prices in the
	// same transaction, calls fold with them (see HoldingFold for exactly
	// what it is handed), and inserts only if fold returns nil -- so a second
	// writer blocks and then folds the first one's result, not a stale copy.
	//
	// Reading, folding and writing as three separate calls is NOT
	// equivalent: two sales of 30 from a holding of 50 would each pass and
	// both commit, leaving events that can't be folded at all. This port
	// owns the transaction and the lock, never the rule.
	InsertWithFold(ctx context.Context, e domain.HoldingEvent, fold HoldingFold) (domain.HoldingEvent, error)
	// DeleteWithFold is the same guarantee in the other direction: removing a
	// purchase a later sale was costed against, or a sale that kept the
	// holding small, must not be able to race a concurrent write. It reports
	// domain.ErrNotFound, without calling fold, when the event is not this
	// household's and this holding's.
	DeleteWithFold(ctx context.Context, householdID, holdingID, eventID string, fold HoldingFold) error
	// Delete reports domain.ErrNotFound when the event is not this
	// household's AND this holding's, rather than silently succeeding. Both
	// halves matter: the household keeps two families apart, and the
	// holding keeps the URL honest -- a caller naming holding A must not
	// remove a row of holding B, whose fold would then never have been
	// checked.
	Delete(ctx context.Context, householdID, holdingID, eventID string) error
}

// HoldingValuationRepository stores what one unit of a holding was worth on a
// given day.
type HoldingValuationRepository interface {
	// ListByHolding returns one holding's valuations, newest first.
	ListByHolding(ctx context.Context, householdID, holdingID string) ([]domain.Valuation, error)
	// ListLatest returns at most one row per holding: the newest price each
	// has. A holding with NO valuation produces no row at all rather than a
	// zero one -- the caller reads an absent holding as "no price recorded",
	// which is what a screen must say instead of showing a figure of zero.
	ListLatest(ctx context.Context, householdID string) ([]domain.Valuation, error)
	// ListForHousehold returns every valuation the household has, not one
	// per holding. The period report needs the whole history: a quarter
	// opens at a price recorded in the quarter before it, and ListLatest
	// has already discarded that one.
	ListForHousehold(ctx context.Context, householdID string) ([]domain.Valuation, error)
	// UpsertWithFold writes one price per holding per day: a second write for
	// the same AsOf day replaces the first. Re-entering a day's price is a
	// correction, not a second opinion, and two rows for one day would leave
	// the report with no way to choose between them.
	//
	// It is the only way to store a price, and it holds the same lock as
	// HoldingEventRepository.InsertWithFold: the implementation locks the
	// holding, reads its events and prices in the same transaction, calls
	// fold (see HoldingFold), and writes only if fold returns nil. A price
	// write and an event write on one holding therefore never overlap.
	//
	// Don't add a plain Upsert beside it: a price stored without the fold is
	// how a holding came to be worth more than an int64 can hold, which
	// failed every portfolio read for the household. It reports
	// domain.ErrNotFound, without calling fold, when the holding is not this
	// household's.
	UpsertWithFold(ctx context.Context, v domain.Valuation, fold HoldingFold) (domain.Valuation, error)
	// Delete needs no fold: every rule a HoldingFold enforces can only be
	// broken by a price that is present, so removing one cannot break it.
	Delete(ctx context.Context, householdID, valuationID string) error
}

// HoldingIncomeRepository stores the dividends a holding paid and the
// charges made against it.
//
// Unlike HoldingEventRepository, there is no ordering contract here and no
// fold-inside-the-write: income never enters the average-cost pool, so no
// invariant spans two rows and nothing needs a lock. Any order is correct
// because summing a period is commutative.
type HoldingIncomeRepository interface {
	Insert(ctx context.Context, i domain.HoldingIncome) (domain.HoldingIncome, error)
	ListByHolding(ctx context.Context, householdID, holdingID string) ([]domain.HoldingIncome, error)
	ListByHousehold(ctx context.Context, householdID string) ([]domain.HoldingIncome, error)
	// Delete returns domain.ErrNotFound when the row is not this household's
	// and this holding's, never a silent success. See
	// HoldingEventRepository.Delete for why the holding is part of the scope
	// and not only the household.
	Delete(ctx context.Context, householdID, holdingID, incomeID string) error
}
