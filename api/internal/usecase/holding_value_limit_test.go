package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// A holding's worth is quantity times unit price, and both can be inside the
// amount ceiling while their product is not. Every test here is about WHEN
// the service refuses: before anything is stored. A price that was stored and
// then failed its own response left a household whose portfolio could not be
// read at all, including by the page the price would have been corrected on.

// oneHundredThousand units at oneTrillionMajor each is 1e19 minor units, which
// does not fit in an int64. Each figure alone is accepted.
const (
	oneHundredThousand = 100_000
	oneTrillionMajor   = domain.MaxAmountMinor // 1e14 minor: the largest price a person may type
)

func (f *holdingFixture) recordPrice(holdingID string, when time.Time, unitPriceMinor int64) error {
	_, err := f.svc.RecordValuation(context.Background(), domain.Valuation{
		HouseholdID: f.householdID, HoldingID: holdingID,
		UnitPrice: domain.Money{Amount: unitPriceMinor, Currency: "SGD"}, AsOf: when,
	}, reportToday)
	return err
}

func (f *holdingFixture) recordEvent(t *testing.T, holdingID string, kind domain.HoldingEventKind, when time.Time, units, minor int64) (domain.HoldingEvent, error) {
	t.Helper()
	return f.svc.RecordEvent(context.Background(), domain.HoldingEvent{
		HouseholdID: f.householdID, HoldingID: holdingID, Kind: kind,
		Quantity: qty(t, units), Amount: money(t, minor, "SGD"), OccurredOn: when,
	}, reportToday)
}

// mustStillRead is the other half of every refusal: the screens that add the
// holding up have to load afterwards.
func (f *holdingFixture) mustStillRead(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.svc.Portfolio(ctx, f.householdID, true); err != nil {
		t.Errorf("Portfolio after the refusal: %v", err)
	}
	if _, err := f.svc.Report(ctx, f.householdID, domain.PeriodQuarter, 6, reportToday); err != nil {
		t.Errorf("Report after the refusal: %v", err)
	}
}

func TestAPriceThatMakesTheHoldingWorthTooMuchIsRefusedAndNotStored(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")
	f.buy(t, h.ID, holdingDay(2), oneHundredThousand, 5_000_000)

	err := f.recordPrice(h.ID, holdingDay(5), oneTrillionMajor)
	if !errors.Is(err, domain.ErrHoldingValueTooLarge) {
		t.Fatalf("error = %v, want ErrHoldingValueTooLarge", err)
	}
	if len(f.valuations.rows) != 0 {
		t.Fatalf("the refused price was stored: %+v", f.valuations.rows)
	}
	f.mustStillRead(t)
}

// The other order. With nothing held, any price is fine: nothing times a
// price is nothing. The purchase is then the write that has to be refused.
func TestAPurchaseThatMakesTheHoldingWorthTooMuchIsRefusedAndNotStored(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")
	if err := f.recordPrice(h.ID, holdingDay(5), oneTrillionMajor); err != nil {
		t.Fatalf("a price on a holding that holds nothing: %v", err)
	}

	_, err := f.recordEvent(t, h.ID, domain.HoldingAcquisition, holdingDay(2), oneHundredThousand, 5_000_000)
	if !errors.Is(err, domain.ErrHoldingValueTooLarge) {
		t.Fatalf("error = %v, want ErrHoldingValueTooLarge", err)
	}
	if len(f.events.rows) != 0 {
		t.Fatalf("the refused purchase was stored: %+v", f.events.rows)
	}
	f.mustStillRead(t)

	// One unit at that price is exactly the limit, so the holding is still
	// usable: the refusal was about the quantity, not the holding.
	if _, err := f.recordEvent(t, h.ID, domain.HoldingAcquisition, holdingDay(2), 1, 5_000_000); err != nil {
		t.Fatalf("one unit at the largest price: %v", err)
	}
}

// Deleting a sale puts its units back into every later moment of the
// history. Here the holding peaked at 60,000 units; without the sale it would
// have reached 110,000, and 110,000 at the recorded price is past the limit.
func TestDeletingASaleThatWouldRestoreTooLargeAQuantityIsRefused(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")
	f.buy(t, h.ID, holdingDay(1), 60_000, 600_000)
	sale, err := f.recordEvent(t, h.ID, domain.HoldingDisposal, holdingDay(2), 50_000, 500_000)
	if err != nil {
		t.Fatalf("sell: %v", err)
	}
	f.buy(t, h.ID, holdingDay(3), 50_000, 500_000)
	// 60,000 units at 1e9 is 6e13, inside the limit of 1e14.
	f.price(t, h.ID, holdingDay(4), 1_000_000_000)

	err = f.svc.DeleteEvent(context.Background(), f.householdID, h.ID, sale.ID)
	if !errors.Is(err, domain.ErrHoldingValueTooLarge) {
		t.Fatalf("error = %v, want ErrHoldingValueTooLarge", err)
	}
	if len(f.events.rows) != 3 {
		t.Fatalf("the holding has %d events, want all 3: the sale must still be there", len(f.events.rows))
	}
	f.mustStillRead(t)
}

// The period report prices the quantity held at each period's end, so a
// price is checked against the most the holding EVER held. One unit is held
// today and the price is dated before anything was bought; it is still
// refused, because 100,000 units were held in between.
func TestAPriceIsJudgedAgainstTheMostEverHeldWhateverItsDate(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")
	f.buy(t, h.ID, holdingDay(5), oneHundredThousand, 5_000_000)
	f.sell(t, h.ID, holdingDay(6), oneHundredThousand-1, 5_000_000)

	// 1e10 a unit: the 1 unit held today would be worth 1e10, but 100,000
	// units were once held, and they would be worth 1e15.
	err := f.recordPrice(h.ID, holdingDay(1), 10_000_000_000)
	if !errors.Is(err, domain.ErrHoldingValueTooLarge) {
		t.Fatalf("error = %v, want ErrHoldingValueTooLarge", err)
	}
	if len(f.valuations.rows) != 0 {
		t.Fatalf("the refused price was stored: %+v", f.valuations.rows)
	}

	// 1e9 a unit puts the peak at exactly the limit, which is allowed.
	if err := f.recordPrice(h.ID, holdingDay(1), 1_000_000_000); err != nil {
		t.Fatalf("a price that keeps the peak at the limit: %v", err)
	}
}

// A foreign holding records each price twice, and the household-currency one
// is multiplied by the same quantity. Here the native price is small and only
// the household-currency price is too large.
func TestTheHouseholdCurrencyPriceOfAForeignHoldingIsHeldToTheLimitToo(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "VOO", "USD")
	ctx := context.Background()
	primary := money(t, 500_000, "SGD")
	if _, err := f.svc.RecordEvent(ctx, domain.HoldingEvent{
		HouseholdID: f.householdID, HoldingID: h.ID, Kind: domain.HoldingAcquisition,
		Quantity: qty(t, oneHundredThousand), Amount: money(t, 400_000, "USD"), PrimaryAmount: &primary,
		OccurredOn: holdingDay(2),
	}, reportToday); err != nil {
		t.Fatalf("buy: %v", err)
	}

	tooMuch := money(t, oneTrillionMajor, "SGD")
	_, err := f.svc.RecordValuation(ctx, domain.Valuation{
		HouseholdID: f.householdID, HoldingID: h.ID,
		UnitPrice: money(t, 100, "USD"), PrimaryUnitPrice: &tooMuch, AsOf: holdingDay(5),
	}, reportToday)
	if !errors.Is(err, domain.ErrHoldingValueTooLarge) {
		t.Fatalf("error = %v, want ErrHoldingValueTooLarge", err)
	}
	if len(f.valuations.rows) != 0 {
		t.Fatalf("the refused price was stored: %+v", f.valuations.rows)
	}
}

// brokenHolding is a holding already past the limit: 100,000 units and a
// stored price of 1e14 on the 10th. The service can no longer create one, so
// the price is put straight into the double, the way a row written before
// the rule existed sits in the table.
func brokenHolding(t *testing.T) (*holdingFixture, domain.Holding, domain.HoldingEvent) {
	t.Helper()
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")
	purchase, err := f.recordEvent(t, h.ID, domain.HoldingAcquisition, holdingDay(2), oneHundredThousand, 5_000_000)
	if err != nil {
		t.Fatalf("buy: %v", err)
	}
	f.valuations.rows = append(f.valuations.rows, domain.Valuation{
		ID: "stored-before-the-rule", HouseholdID: f.householdID, HoldingID: h.ID,
		UnitPrice: money(t, oneTrillionMajor, "SGD"), AsOf: holdingDay(10),
	})
	return f, h, purchase
}

// Re-entering a day's price is how a wrong price is corrected, so the
// correction must be judged WITHOUT the price it replaces. If the old row
// stayed in the list, the only way out of a broken holding would be closed.
func TestCorrectingADaysPriceDownwardSucceedsOnAHoldingPastTheLimit(t *testing.T) {
	f, h, _ := brokenHolding(t)

	// Anything that leaves the bad price in place is refused, even a small
	// price for another day.
	if err := f.recordPrice(h.ID, holdingDay(11), 100); !errors.Is(err, domain.ErrHoldingValueTooLarge) {
		t.Fatalf("a price for another day: error = %v, want ErrHoldingValueTooLarge", err)
	}

	// The correction carries a time of day; the stored row is at midnight.
	// They are the same calendar day and the same row.
	afternoonOfTheTenth := holdingDay(10).Add(15 * time.Hour)
	if err := f.recordPrice(h.ID, afternoonOfTheTenth, 1_000); err != nil {
		t.Fatalf("correcting the 10th's price downward: %v", err)
	}
	if len(f.valuations.rows) != 1 || f.valuations.rows[0].UnitPrice.Amount != 1_000 {
		t.Fatalf("prices = %+v, want the one corrected row at 1000", f.valuations.rows)
	}
	f.mustStillRead(t)
}

// The other repair: delete the purchase that made the holding too valuable.
func TestDeletingThePurchaseRepairsAHoldingPastTheLimit(t *testing.T) {
	f, h, purchase := brokenHolding(t)

	if err := f.svc.DeleteEvent(context.Background(), f.householdID, h.ID, purchase.ID); err != nil {
		t.Fatalf("deleting the purchase: %v", err)
	}
	if len(f.events.rows) != 0 {
		t.Fatalf("events = %+v, want none", f.events.rows)
	}
	f.mustStillRead(t)
}
