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
// accounts. It is deliberately separate from AccountRepository and touches no
// balance: a holding is invisible to ListAccounts, to net worth and to the
// twelve-month trend, which is milestone 1's whole boundary. Making an
// account's balance read from holdings is a later, separate decision about
// whether an investment account also carries uninvested cash.
type HoldingRepository interface {
	// List returns one household's holdings ordered by name, each joined to
	// its account's nickname and whether that account is archived -- an
	// archived account's holdings are still real money and still listed, with
	// the account labelled. includeArchived is a UNION, not a filter swap:
	// false returns the live holdings, true returns live AND archived
	// together, each carrying its own ArchivedAt. The AccountRepository.List
	// and GoalRepository.List contract; do not implement it as "archived
	// instead".
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
// whether the household holds anything at all. A narrow port rather than the
// whole HoldingRepository, by the same interface-segregation rule that gives
// the usecase ports nine small repositories instead of one object with forty
// methods -- and so that the accounts and household services cannot grow a
// dependency on holdings they were never meant to have.
type HoldingCounter interface {
	CountLiveForAccount(ctx context.Context, householdID, accountID string) (int64, error)
	// CountForHousehold counts ARCHIVED holdings too. An archived holding
	// still has events, and those events still have to fold -- archiving is
	// how a household stops looking at something, not how it forgets it.
	CountForHousehold(ctx context.Context, householdID string) (int64, error)
}

// HoldingEventRepository stores the acquisitions and disposals a holding is
// made of. Income (a dividend) is deliberately not among them: it changes
// neither what is held nor what it cost, so folding it here would corrupt the
// average cost.
type HoldingEventRepository interface {
	// ListByHolding returns one holding's events ordered by
	// (OccurredOn, CreatedAt, ID). THAT ORDER IS A CONTRACT, NOT A
	// PREFERENCE, and an implementation may not relax it.
	//
	// OccurredOn is a date, so two events can share one -- buying and selling
	// the same morning is ordinary -- and domain.Holding.Position sorts
	// STABLY, which means it keeps whatever order it is handed for a tie. The
	// tie is therefore broken here, by the order the events were actually
	// recorded in. Return them in any other order and a household's realised
	// gain changes silently: on identical same-day events, buy-then-sell
	// realises 750 where sell-then-buy realises 1000.
	ListByHolding(ctx context.Context, householdID, holdingID string) ([]domain.HoldingEvent, error)
	// ListByHousehold is the same contract across every holding, grouped by
	// holding, so a portfolio page folds every position without one query
	// per holding.
	ListByHousehold(ctx context.Context, householdID string) ([]domain.HoldingEvent, error)
	Insert(ctx context.Context, e domain.HoldingEvent) (domain.HoldingEvent, error)
	// InsertWithFold is Insert with the holding's invariant held ACROSS the
	// write, and it is what a service must use for anything the fold can
	// refuse. The implementation locks the holding, lists its events in the
	// same transaction, calls fold with them, and inserts only if fold returns
	// nil -- so a second writer blocks and then folds the first one's result
	// rather than a stale copy.
	//
	// Reading, folding and writing as three separate calls is NOT equivalent:
	// two sales of 30 from a holding of 50 would each pass and both commit,
	// leaving events that cannot be folded at all. fold is the caller's own
	// rule (domain.Holding.Position); this port owns the transaction and the
	// lock, never the rule.
	InsertWithFold(ctx context.Context, e domain.HoldingEvent, fold func([]domain.HoldingEvent) error) (domain.HoldingEvent, error)
	// DeleteWithFold is the same guarantee in the other direction: removing a
	// purchase a later sale was costed against must not be able to race a
	// concurrent write. fold receives the events that WOULD remain.
	DeleteWithFold(ctx context.Context, householdID, holdingID, eventID string, fold func([]domain.HoldingEvent) error) error
	// Delete reports domain.ErrNotFound when the event is not this
	// household's AND this holding's, rather than silently succeeding. Both
	// halves of that scope are load-bearing: the household keeps two families
	// apart, and the holding keeps the URL honest -- a caller naming holding A
	// must not be able to remove a row of holding B, whose fold would then
	// never have been checked.
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
	// Upsert writes one price per holding per day: a second write for the
	// same AsOf replaces the first. Re-entering a day's price is a
	// correction, not a second opinion, and two rows for one day would leave
	// the report with no way to choose between them.
	// ListForHousehold returns EVERY valuation the household has, not one per
	// holding. The period report needs the whole history: a quarter opens at
	// a price recorded in the quarter before it, and ListLatest has already
	// discarded that one.
	ListForHousehold(ctx context.Context, householdID string) ([]domain.Valuation, error)
	// Upsert writes one price per holding per day: a second write for the
	// same AsOf replaces the first. Re-entering a day's price is a
	// correction, not a second opinion, and two rows for one day would leave
	// the report with no way to choose between them.
	Upsert(ctx context.Context, v domain.Valuation) (domain.Valuation, error)
	Delete(ctx context.Context, householdID, valuationID string) error
}

// HoldingIncomeRepository stores the dividends a holding paid and the charges
// made against it.
//
// Unlike HoldingEventRepository there is no ordering contract here and no
// fold-inside-the-write: income never enters the average-cost pool, so no
// invariant spans two rows, no order changes the answer, and nothing needs a
// lock. Any order is correct because summing a period is commutative.
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
