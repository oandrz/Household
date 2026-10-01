package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// These tests pin what usecase.HoldingFold promises a service: the fold is
// handed the holding's events AND prices as they would be after the write,
// read under the holding's lock, and nothing is written when it refuses.

// holdingFoldFixture is one empty SGD holding and the two repositories that
// write to it.
type holdingFoldFixture struct {
	householdID string
	holding     domain.Holding
	events      *postgres.HoldingEventRepo
	valuations  *postgres.HoldingValuationRepo
}

func newHoldingFoldFixture(t *testing.T) holdingFoldFixture {
	t.Helper()
	db := openTestDB(t)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")
	h, err := postgres.NewHoldingRepo(db).Create(context.Background(), newTestHolding(householdID, accountID, "D05"))
	if err != nil {
		t.Fatalf("Create holding: %v", err)
	}
	return holdingFoldFixture{
		householdID: householdID,
		holding:     h,
		events:      postgres.NewHoldingEventRepo(db),
		valuations:  postgres.NewHoldingValuationRepo(db),
	}
}

func (f holdingFoldFixture) price(minor int64, asOf time.Time) domain.Valuation {
	return domain.Valuation{
		HoldingID: f.holding.ID, HouseholdID: f.householdID, UnitPrice: moneyOf(minor), AsOf: asOf,
	}
}

func (f holdingFoldFixture) purchase(t *testing.T, units int64, on time.Time) domain.HoldingEvent {
	t.Helper()
	return domain.HoldingEvent{
		HoldingID: f.holding.ID, HouseholdID: f.householdID, Kind: domain.HoldingAcquisition,
		Quantity: testQuantity(t, units), Amount: moneyOf(5_000_000), OccurredOn: on,
	}
}

// unitPrices is the set of prices a fold was handed, without their order:
// the port promises none.
func unitPrices(prices []domain.Valuation) map[int64]bool {
	out := map[int64]bool{}
	for _, p := range prices {
		out[p.UnitPrice.Amount] = true
	}
	return out
}

// A large purchase and a large price, racing: each is legal alone (100,000
// units with no price; a price of 1e12 with nothing held) and together they
// make a holding worth 1e17, a thousand times the limit. Without one lock
// shared by BOTH repositories, each fold reads the holding before the other
// writer has committed, both pass, and the household is left with a holding
// its portfolio cannot add up.
func TestARacingLargePurchaseAndLargePriceCannotBothCommit(t *testing.T) {
	f := newHoldingFoldFixture(t)
	ctx := context.Background()

	// The service's rule, with a sleep in front. The sleep holds the
	// check-to-write window open long enough for the two goroutines to
	// genuinely overlap. Don't remove it: without it they serialise by luck
	// and this test passes with no lock at all (LEARNING pattern 19).
	rule := func(events []domain.HoldingEvent, prices []domain.Valuation) error {
		time.Sleep(300 * time.Millisecond)
		position, err := f.holding.Position(events, "SGD")
		if err != nil {
			return err
		}
		return domain.CheckHoldingValueWithinLimit(position.PeakHeld, prices)
	}
	buy := func() error {
		_, err := f.events.InsertWithFold(ctx, f.purchase(t, 100_000, july(2)), rule)
		return err
	}
	price := func() error {
		_, err := f.valuations.UpsertWithFold(ctx, f.price(1_000_000_000_000, july(5)), rule)
		return err
	}

	results := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(1)
	for _, write := range []func() error{buy, price} {
		go func() {
			start.Wait()
			results <- write()
		}()
	}
	start.Done()
	first, second := <-results, <-results

	refused := 0
	for _, err := range []error{first, second} {
		if errors.Is(err, domain.ErrHoldingValueTooLarge) {
			refused++
		} else if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if refused != 1 {
		t.Fatalf("%d of the 2 racing writes were refused, want exactly 1 (errors: %v, %v)", refused, first, second)
	}

	// And what was stored is exactly one of the two, still inside the limit.
	events, err := f.events.ListByHolding(ctx, f.householdID, f.holding.ID)
	if err != nil {
		t.Fatalf("ListByHolding events: %v", err)
	}
	prices, err := f.valuations.ListByHolding(ctx, f.householdID, f.holding.ID)
	if err != nil {
		t.Fatalf("ListByHolding prices: %v", err)
	}
	if len(events)+len(prices) != 1 {
		t.Fatalf("stored %d events and %d prices, want one row in total", len(events), len(prices))
	}
	position, err := f.holding.Position(events, "SGD")
	if err != nil {
		t.Fatalf("the holding no longer folds after the race: %v", err)
	}
	if err := domain.CheckHoldingValueWithinLimit(position.PeakHeld, prices); err != nil {
		t.Fatalf("the holding is past the limit after the race: %v", err)
	}
}

// Re-entering a day's price is a correction, so the fold must see the new
// price INSTEAD of that day's stored one. The correction here is timed 03:00
// in Singapore, which is still the 4th in UTC: the day is the one the value
// shows in its own zone, exactly as the stored key is built, and it must
// replace the 5th's row and leave the 4th's alone.
func TestAPriceCorrectionIsFoldedWithoutTheRowItReplaces(t *testing.T) {
	f := newHoldingFoldFixture(t)
	ctx := context.Background()
	for _, stored := range []domain.Valuation{f.price(80, july(4)), f.price(100, july(5))} {
		if _, err := f.valuations.UpsertWithFold(ctx, stored, acceptAnything); err != nil {
			t.Fatalf("UpsertWithFold: %v", err)
		}
	}

	singapore := time.FixedZone("SGT", 8*60*60)
	earlyOnTheFifth := time.Date(2026, 7, 5, 3, 0, 0, 0, singapore)
	var seen []domain.Valuation
	if _, err := f.valuations.UpsertWithFold(ctx, f.price(250, earlyOnTheFifth), func(_ []domain.HoldingEvent, prices []domain.Valuation) error {
		seen = prices
		return nil
	}); err != nil {
		t.Fatalf("UpsertWithFold correction: %v", err)
	}

	got := unitPrices(seen)
	if len(seen) != 2 || !got[80] || !got[250] {
		t.Fatalf("the fold saw prices %v, want the 4th's 80 and the corrected 250 only -- not the 100 being replaced", got)
	}

	stored, err := f.valuations.ListByHolding(ctx, f.householdID, f.holding.ID)
	if err != nil {
		t.Fatalf("ListByHolding: %v", err)
	}
	after := unitPrices(stored)
	if len(stored) != 2 || !after[80] || !after[250] {
		t.Fatalf("stored prices = %v, want 80 and 250: the fold must describe what the write then does", after)
	}
}

// A refusal must leave nothing behind, and must not disturb the price a
// correction was aimed at. The caller's error comes back unchanged.
func TestARefusedPriceIsNotStored(t *testing.T) {
	f := newHoldingFoldFixture(t)
	ctx := context.Background()
	refusal := errors.New("the fold refuses")
	refuse := func([]domain.HoldingEvent, []domain.Valuation) error { return refusal }

	if _, err := f.valuations.UpsertWithFold(ctx, f.price(250, july(5)), refuse); !errors.Is(err, refusal) {
		t.Fatalf("error = %v, want the fold's own error", err)
	}
	stored, err := f.valuations.ListByHolding(ctx, f.householdID, f.holding.ID)
	if err != nil {
		t.Fatalf("ListByHolding: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("a refused price was stored: %+v", stored)
	}

	if _, err := f.valuations.UpsertWithFold(ctx, f.price(100, july(5)), acceptAnything); err != nil {
		t.Fatalf("UpsertWithFold: %v", err)
	}
	if _, err := f.valuations.UpsertWithFold(ctx, f.price(250, july(5)), refuse); !errors.Is(err, refusal) {
		t.Fatalf("error = %v, want the fold's own error", err)
	}
	stored, err = f.valuations.ListByHolding(ctx, f.householdID, f.holding.ID)
	if err != nil {
		t.Fatalf("ListByHolding: %v", err)
	}
	if len(stored) != 1 || stored[0].UnitPrice.Amount != 100 {
		t.Fatalf("after a refused correction the prices are %+v, want the original 100 untouched", stored)
	}
}

// An event write changes no price, but its fold is handed every price all
// the same: a purchase is judged against them.
func TestAnEventWriteIsHandedTheHoldingsPrices(t *testing.T) {
	f := newHoldingFoldFixture(t)
	ctx := context.Background()
	for _, stored := range []domain.Valuation{f.price(80, july(4)), f.price(100, july(5))} {
		if _, err := f.valuations.UpsertWithFold(ctx, stored, acceptAnything); err != nil {
			t.Fatalf("UpsertWithFold: %v", err)
		}
	}

	var onInsert []domain.Valuation
	bought, err := f.events.InsertWithFold(ctx, f.purchase(t, 10, july(2)), func(_ []domain.HoldingEvent, prices []domain.Valuation) error {
		onInsert = prices
		return nil
	})
	if err != nil {
		t.Fatalf("InsertWithFold: %v", err)
	}
	if got := unitPrices(onInsert); len(onInsert) != 2 || !got[80] || !got[100] {
		t.Fatalf("InsertWithFold's fold saw prices %v, want 80 and 100", got)
	}

	var onDelete []domain.Valuation
	var remaining []domain.HoldingEvent
	if err := f.events.DeleteWithFold(ctx, f.householdID, f.holding.ID, bought.ID, func(events []domain.HoldingEvent, prices []domain.Valuation) error {
		remaining, onDelete = events, prices
		return nil
	}); err != nil {
		t.Fatalf("DeleteWithFold: %v", err)
	}
	if got := unitPrices(onDelete); len(onDelete) != 2 || !got[80] || !got[100] {
		t.Fatalf("DeleteWithFold's fold saw prices %v, want 80 and 100", got)
	}
	if len(remaining) != 0 {
		t.Fatalf("DeleteWithFold's fold saw %d events, want none: the deleted one is taken out", len(remaining))
	}
}

// A price for a holding in another household reads as absent, and the fold
// is never run on rows the caller does not own.
func TestAPriceForAnotherHouseholdsHoldingIsNotFound(t *testing.T) {
	f := newHoldingFoldFixture(t)
	ctx := context.Background()
	// Any id that is not this holding's household: the lock query matches on
	// both, so a wrong household finds no row to lock.
	const someoneElse = "00000000-0000-0000-0000-000000000000"

	price := f.price(100, july(5))
	price.HouseholdID = someoneElse
	called := false
	_, err := f.valuations.UpsertWithFold(ctx, price, func([]domain.HoldingEvent, []domain.Valuation) error {
		called = true
		return nil
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if called {
		t.Fatal("the fold ran for a holding the caller does not own")
	}
	stored, err := f.valuations.ListByHolding(ctx, f.householdID, f.holding.ID)
	if err != nil {
		t.Fatalf("ListByHolding: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("a price was stored through the wrong household: %+v", stored)
	}
}
