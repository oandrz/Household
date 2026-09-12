package usecase_test

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// --- AccountLookup ----------------------------------------------------------

type accountLookupDouble struct {
	types map[string]domain.AccountType
}

func newAccountLookupDouble() *accountLookupDouble {
	return &accountLookupDouble{types: map[string]domain.AccountType{}}
}

func (d *accountLookupDouble) put(id string, t domain.AccountType) { d.types[id] = t }

// Get mirrors AccountRepo.Get: an account in another household -- or none at
// all -- is ErrNotFound, never a forbidden, so nothing leaks its existence.
func (d *accountLookupDouble) Get(_ context.Context, _, accountID string) (usecase.AccountView, error) {
	t, ok := d.types[accountID]
	if !ok {
		return usecase.AccountView{}, domain.ErrNotFound
	}
	return usecase.AccountView{Account: domain.Account{ID: accountID, Type: t}}, nil
}

func (d *accountLookupDouble) MembershipBelongsToHousehold(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}

// --- HoldingRepository ------------------------------------------------------

type holdingRepoDouble struct {
	rows map[string]domain.Holding
	n    int
}

func newHoldingRepoDouble() *holdingRepoDouble {
	return &holdingRepoDouble{rows: map[string]domain.Holding{}}
}

func (d *holdingRepoDouble) List(_ context.Context, householdID string, includeArchived bool) ([]usecase.HoldingRecord, error) {
	out := []usecase.HoldingRecord{}
	for _, h := range d.rows {
		if h.HouseholdID != householdID {
			continue
		}
		if h.IsArchived() && !includeArchived {
			continue
		}
		out = append(out, usecase.HoldingRecord{Holding: h, AccountName: "Brokerage"})
	}
	// The real query orders by (name, id); the double matches it so a test
	// that depends on order is testing the same thing in both places.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Holding.Name != out[j].Holding.Name {
			return out[i].Holding.Name < out[j].Holding.Name
		}
		return out[i].Holding.ID < out[j].Holding.ID
	})
	return out, nil
}

func (d *holdingRepoDouble) Get(_ context.Context, householdID, holdingID string) (domain.Holding, error) {
	h, ok := d.rows[holdingID]
	if !ok || h.HouseholdID != householdID {
		return domain.Holding{}, domain.ErrNotFound
	}
	return h, nil
}

func (d *holdingRepoDouble) Create(_ context.Context, h domain.Holding) (domain.Holding, error) {
	// The unique key is (account_id, name), archived rows included -- the
	// migration's own scoping, mirrored here so a service test sees the same
	// collision the database would raise.
	for _, existing := range d.rows {
		if existing.AccountID == h.AccountID && existing.Name == h.Name {
			return domain.Holding{}, domain.ErrHoldingNameTaken
		}
	}
	d.n++
	h.ID = "holding-" + strconv.Itoa(d.n)
	d.rows[h.ID] = h
	return h, nil
}

func (d *holdingRepoDouble) Update(_ context.Context, h domain.Holding) (domain.Holding, error) {
	existing, ok := d.rows[h.ID]
	if !ok || existing.HouseholdID != h.HouseholdID {
		return domain.Holding{}, domain.ErrNotFound
	}
	// Currency and AccountID are not mutable, matching UpdateHolding's own SQL.
	existing.Name, existing.Instrument, existing.Unit = h.Name, h.Instrument, h.Unit
	d.rows[h.ID] = existing
	return existing, nil
}

func (d *holdingRepoDouble) SetArchived(_ context.Context, householdID, holdingID string, archivedAt *time.Time) (domain.Holding, error) {
	h, ok := d.rows[holdingID]
	if !ok || h.HouseholdID != householdID {
		return domain.Holding{}, domain.ErrNotFound
	}
	h.ArchivedAt = archivedAt
	d.rows[holdingID] = h
	return h, nil
}

func (d *holdingRepoDouble) CountLiveForAccount(_ context.Context, householdID, accountID string) (int64, error) {
	var n int64
	for _, h := range d.rows {
		if h.HouseholdID == householdID && h.AccountID == accountID && !h.IsArchived() {
			n++
		}
	}
	return n, nil
}

// --- HoldingEventRepository -------------------------------------------------

type holdingEventRepoDouble struct {
	rows []domain.HoldingEvent
	n    int
}

func newHoldingEventRepoDouble() *holdingEventRepoDouble { return &holdingEventRepoDouble{} }

// ordered mirrors the repository's ORDER BY (occurred_on, created_at, id).
// Insertion order stands in for created_at, and the sort is STABLE so that
// same-day events keep it -- which is the whole contract the port's doc
// comment spells out. A double that returned map order instead would let a
// same-day ordering bug pass here and fail only against the real database.
func (d *holdingEventRepoDouble) ordered(match func(domain.HoldingEvent) bool) []domain.HoldingEvent {
	out := []domain.HoldingEvent{}
	for _, e := range d.rows {
		if match(e) {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].OccurredOn.Before(out[j].OccurredOn)
	})
	return out
}

func (d *holdingEventRepoDouble) ListByHolding(_ context.Context, householdID, holdingID string) ([]domain.HoldingEvent, error) {
	return d.ordered(func(e domain.HoldingEvent) bool {
		return e.HouseholdID == householdID && e.HoldingID == holdingID
	}), nil
}

func (d *holdingEventRepoDouble) ListByHousehold(_ context.Context, householdID string) ([]domain.HoldingEvent, error) {
	out := d.ordered(func(e domain.HoldingEvent) bool { return e.HouseholdID == householdID })
	sort.SliceStable(out, func(i, j int) bool { return out[i].HoldingID < out[j].HoldingID })
	return out, nil
}

func (d *holdingEventRepoDouble) Insert(_ context.Context, e domain.HoldingEvent) (domain.HoldingEvent, error) {
	d.n++
	e.ID = "event-" + strconv.Itoa(d.n)
	d.rows = append(d.rows, e)
	return e, nil
}

// InsertWithFold mirrors the real repository's contract: fold sees the events
// that WOULD exist, and the insert happens only if it accepts. A single
// goroutine cannot exercise the lock, so what this double pins is the
// ordering -- fold before write, and fold's refusal preventing the write.
// The lock itself is proved against a real Postgres in
// postgres/holding_repo_test.go's racing-disposals test.
func (d *holdingEventRepoDouble) InsertWithFold(
	ctx context.Context,
	e domain.HoldingEvent,
	fold func([]domain.HoldingEvent) error,
) (domain.HoldingEvent, error) {
	existing, err := d.ListByHolding(ctx, e.HouseholdID, e.HoldingID)
	if err != nil {
		return domain.HoldingEvent{}, err
	}
	if err := fold(append(existing, e)); err != nil {
		return domain.HoldingEvent{}, err
	}
	return d.Insert(ctx, e)
}

func (d *holdingEventRepoDouble) DeleteWithFold(
	ctx context.Context,
	householdID, holdingID, eventID string,
	fold func([]domain.HoldingEvent) error,
) error {
	existing, err := d.ListByHolding(ctx, householdID, holdingID)
	if err != nil {
		return err
	}
	remaining := make([]domain.HoldingEvent, 0, len(existing))
	found := false
	for _, e := range existing {
		if e.ID == eventID {
			found = true
			continue
		}
		remaining = append(remaining, e)
	}
	if !found {
		return domain.ErrNotFound
	}
	if err := fold(remaining); err != nil {
		return err
	}
	return d.Delete(ctx, householdID, eventID)
}

func (d *holdingEventRepoDouble) Delete(_ context.Context, householdID, eventID string) error {
	for i, e := range d.rows {
		if e.ID == eventID && e.HouseholdID == householdID {
			d.rows = append(d.rows[:i], d.rows[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

// --- HoldingValuationRepository ---------------------------------------------

type valuationRepoDouble struct {
	rows []domain.Valuation
	n    int
}

func newValuationRepoDouble() *valuationRepoDouble { return &valuationRepoDouble{} }

func (d *valuationRepoDouble) ListByHolding(_ context.Context, householdID, holdingID string) ([]domain.Valuation, error) {
	out := []domain.Valuation{}
	for _, v := range d.rows {
		if v.HouseholdID == householdID && v.HoldingID == holdingID {
			out = append(out, v)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[j].AsOf.Before(out[i].AsOf) })
	return out, nil
}

// ListLatest returns at most one row per holding and NO row for a holding
// nobody has priced -- the absence the portfolio view renders as "no price
// recorded" rather than as a figure of zero.
func (d *valuationRepoDouble) ListLatest(_ context.Context, householdID string) ([]domain.Valuation, error) {
	newest := map[string]domain.Valuation{}
	for _, v := range d.rows {
		if v.HouseholdID != householdID {
			continue
		}
		if seen, ok := newest[v.HoldingID]; !ok || seen.AsOf.Before(v.AsOf) {
			newest[v.HoldingID] = v
		}
	}
	out := []domain.Valuation{}
	for _, v := range newest {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].HoldingID < out[j].HoldingID })
	return out, nil
}

// Upsert mirrors the UNIQUE (holding_id, as_of) key: a second price for one
// day replaces the first rather than joining it.
func (d *valuationRepoDouble) Upsert(_ context.Context, v domain.Valuation) (domain.Valuation, error) {
	for i, existing := range d.rows {
		if existing.HoldingID == v.HoldingID && existing.AsOf.Equal(v.AsOf) {
			v.ID = existing.ID
			d.rows[i] = v
			return v, nil
		}
	}
	d.n++
	v.ID = "valuation-" + strconv.Itoa(d.n)
	d.rows = append(d.rows, v)
	return v, nil
}

func (d *valuationRepoDouble) Delete(_ context.Context, householdID, valuationID string) error {
	for i, v := range d.rows {
		if v.ID == valuationID && v.HouseholdID == householdID {
			d.rows = append(d.rows[:i], d.rows[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}
