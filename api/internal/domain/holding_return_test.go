package domain_test

import (
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// What a holding earned over one period, split the three ways the PRD pins:
// the change in market value of what is still held, the gain on what was sold,
// and the cash it paid out along the way.
//
// The arithmetic is cost-carried -- each end of the period is measured as
// (market value - cost of what is held) -- which is what makes "buying more
// must never read as profit" true by construction rather than by a correcting
// term. Most of the tests below are about the two ends.

// d is a day in 2026. The report's tests span months, so `on` (March only) is
// no use here.
func d(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 0, 0, 0, 0, time.UTC)
}

func quarter(t *testing.T, index int) domain.Period {
	t.Helper()
	return mustPeriod(t, domain.PeriodQuarter, 2026, index)
}

func buyOn(t *testing.T, when time.Time, qty, costMinor int64) domain.HoldingEvent {
	t.Helper()
	return domain.HoldingEvent{
		Kind: domain.HoldingAcquisition, Quantity: units(t, qty),
		Amount: sgdAmount(t, costMinor), OccurredOn: when,
	}
}

func sellOn(t *testing.T, when time.Time, qty, proceedsMinor int64) domain.HoldingEvent {
	t.Helper()
	return domain.HoldingEvent{
		Kind: domain.HoldingDisposal, Quantity: units(t, qty),
		Amount: sgdAmount(t, proceedsMinor), OccurredOn: when,
	}
}

func priceOn(t *testing.T, when time.Time, unitPriceMinor int64) domain.Valuation {
	t.Helper()
	return domain.Valuation{UnitPrice: sgdAmount(t, unitPriceMinor), AsOf: when}
}

func incomeOn(t *testing.T, kind domain.IncomeKind, when time.Time, minor int64) domain.HoldingIncome {
	t.Helper()
	return domain.HoldingIncome{Kind: kind, Amount: sgdAmount(t, minor), ReceivedOn: when}
}

func mustReturn(t *testing.T, h domain.Holding, p domain.Period, events []domain.HoldingEvent,
	income []domain.HoldingIncome, prices []domain.Valuation, primary string) domain.PeriodReturn {
	t.Helper()
	r, err := h.ReturnOver(p, events, income, prices, primary)
	if err != nil {
		t.Fatalf("ReturnOver(%s): %v", p.Label(), err)
	}
	return r
}

// The plain case: held right through, priced at both ends. S$1,000.00 of stock
// worth S$1,200.00 three months later earned S$200.00 and nothing was sold.
func TestAHoldingHeldThroughAPeriodEarnsTheChangeInItsMarketValue(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 2),
		[]domain.HoldingEvent{buyOn(t, d(time.January, 5), 10, 100000)},
		nil,
		[]domain.Valuation{
			priceOn(t, d(time.March, 31), 10000), // opens Q2 at S$1,000.00
			priceOn(t, d(time.June, 30), 12000),  // closes it at S$1,200.00
		},
		"SGD")

	if r.Unrealised == nil {
		t.Fatalf("unrealised should be computable, blanked with %q", r.Reason)
	}
	if r.Unrealised.Native.Amount != 20000 {
		t.Errorf("unrealised = %d, want 20000", r.Unrealised.Native.Amount)
	}
	if r.Realised.Native.Amount != 0 {
		t.Errorf("realised = %d, want 0", r.Realised.Native.Amount)
	}
	if r.Total == nil || r.Total.Native.Amount != 20000 {
		t.Errorf("total = %v, want 20000", r.Total)
	}
}

// The most common case there is, and the one a naive blanking rule breaks: a
// holding bought inside the period has no opening price because it did not
// exist yet. It starts from its cost on the acquisition date, per the PRD.
func TestAHoldingBoughtInsideThePeriodStartsFromItsCost(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 2),
		[]domain.HoldingEvent{buyOn(t, d(time.May, 5), 10, 100000)},
		nil,
		[]domain.Valuation{priceOn(t, d(time.June, 30), 12000)},
		"SGD")

	if r.Unrealised == nil {
		t.Fatalf("a holding bought mid-period needs no opening price, blanked with %q", r.Reason)
	}
	if r.Unrealised.Native.Amount != 20000 {
		t.Errorf("unrealised = %d, want 20000 (S$1,200.00 worth against S$1,000.00 paid)", r.Unrealised.Native.Amount)
	}
}

// Sold clean inside the period: nothing is held at the end, so no closing
// price is needed and the unrealised gain the holding carried into the period
// is handed over to realised rather than double-counted.
func TestAHoldingSoldInsideThePeriodNeedsNoClosingPrice(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 2),
		[]domain.HoldingEvent{
			buyOn(t, d(time.January, 5), 10, 100000),
			sellOn(t, d(time.May, 20), 10, 130000),
		},
		nil,
		[]domain.Valuation{priceOn(t, d(time.March, 31), 11000)}, // S$1,100.00 at the open
		"SGD")

	if r.Unrealised == nil {
		t.Fatalf("nothing held at the close needs no price, blanked with %q", r.Reason)
	}
	// The S$100.00 it was already up at the start stops being unrealised...
	if r.Unrealised.Native.Amount != -10000 {
		t.Errorf("unrealised = %d, want -10000", r.Unrealised.Native.Amount)
	}
	// ...and the whole S$300.00 gain against cost becomes realised...
	if r.Realised.Native.Amount != 30000 {
		t.Errorf("realised = %d, want 30000", r.Realised.Native.Amount)
	}
	// ...leaving the S$200.00 the period actually earned.
	if r.Total == nil || r.Total.Native.Amount != 20000 {
		t.Errorf("total = %v, want 20000", r.Total)
	}
}

// The single most important rule in the feature. Buying at the going rate adds
// the same amount to what is held and to what it cost, so it moves the figure
// by exactly nothing.
func TestBuyingMoreInsideThePeriodIsNotProfit(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 2),
		[]domain.HoldingEvent{
			buyOn(t, d(time.January, 5), 10, 100000),
			buyOn(t, d(time.May, 5), 10, 100000), // same price, twice the position
		},
		nil,
		[]domain.Valuation{
			priceOn(t, d(time.March, 31), 10000),
			priceOn(t, d(time.June, 30), 10000), // the market did nothing
		},
		"SGD")

	if r.Unrealised == nil || r.Unrealised.Native.Amount != 0 {
		t.Fatalf("unrealised = %v, want 0 -- doubling a position is not a profit", r.Unrealised)
	}
	if r.Total == nil || r.Total.Native.Amount != 0 {
		t.Errorf("total = %v, want 0", r.Total)
	}
}

// A missing price blanks the components that need one and NOTHING ELSE. A
// quarter where the owner sold at a profit and forgot to type a price is not
// an unknowable quarter.
func TestAMissingClosingPriceBlanksUnrealisedButNotRealised(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 2),
		[]domain.HoldingEvent{
			buyOn(t, d(time.January, 5), 10, 100000),
			sellOn(t, d(time.May, 20), 4, 60000), // cost 40000, realised 20000
		},
		[]domain.HoldingIncome{incomeOn(t, domain.IncomeReceived, d(time.April, 3), 500)},
		[]domain.Valuation{priceOn(t, d(time.March, 31), 10000)}, // opens, never closes
		"SGD")

	if r.Unrealised != nil {
		t.Errorf("unrealised = %v, want blank", r.Unrealised)
	}
	if r.Reason != domain.ReasonNoClosingPrice {
		t.Errorf("reason = %q, want %q", r.Reason, domain.ReasonNoClosingPrice)
	}
	if r.Total != nil {
		t.Errorf("total = %v, want blank when a component is", r.Total)
	}
	if r.Realised.Native.Amount != 20000 {
		t.Errorf("realised = %d, want 20000 -- no price is involved in it", r.Realised.Native.Amount)
	}
	if r.Income.Native.Amount != 500 {
		t.Errorf("income = %d, want 500", r.Income.Native.Amount)
	}
}

func TestAMissingOpeningPriceBlanksUnrealisedWithItsOwnReason(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 2),
		[]domain.HoldingEvent{buyOn(t, d(time.January, 5), 10, 100000)},
		nil,
		[]domain.Valuation{priceOn(t, d(time.June, 30), 12000)}, // closes, never opened
		"SGD")

	if r.Unrealised != nil {
		t.Errorf("unrealised = %v, want blank", r.Unrealised)
	}
	if r.Reason != domain.ReasonNoOpeningPrice {
		t.Errorf("reason = %q, want %q", r.Reason, domain.ReasonNoOpeningPrice)
	}
}

// Rule one of the boundary decision: a price recorded after the period closed
// is information the period did not have. Letting it serve would mean typing
// today's price silently rewrites a quarter that has already been read.
func TestAPriceRecordedAfterThePeriodDoesNotCloseIt(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 2),
		[]domain.HoldingEvent{buyOn(t, d(time.January, 5), 10, 100000)},
		nil,
		[]domain.Valuation{
			priceOn(t, d(time.March, 31), 10000),
			priceOn(t, d(time.July, 2), 15000), // two days too late
		},
		"SGD")

	if r.Unrealised != nil {
		t.Fatalf("unrealised = %v, want blank -- the July price belongs to Q3", r.Unrealised)
	}
	if r.Reason != domain.ReasonNoClosingPrice {
		t.Errorf("reason = %q, want %q", r.Reason, domain.ReasonNoClosingPrice)
	}
}

// Rule two: a period's opening value is the preceding period's closing value,
// so the opening price has to have been recorded in that preceding period. A
// March price is not what June was worth, and computing from it would be the
// stale-data failure the PRD names as its top product risk.
func TestAPriceFromTwoPeriodsBackIsTooStaleToOpenAPeriod(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 3),
		[]domain.HoldingEvent{buyOn(t, d(time.January, 5), 10, 100000)},
		nil,
		[]domain.Valuation{
			priceOn(t, d(time.March, 31), 10000),     // Q1 -- too old to open Q3
			priceOn(t, d(time.September, 30), 12000), // closes Q3 fine
		},
		"SGD")

	if r.Unrealised != nil {
		t.Fatalf("unrealised = %v, want blank", r.Unrealised)
	}
	if r.Reason != domain.ReasonNoOpeningPrice {
		t.Errorf("reason = %q, want %q", r.Reason, domain.ReasonNoOpeningPrice)
	}
}

func TestOnlyTheIncomeAndFeesInsideThePeriodCount(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 2),
		[]domain.HoldingEvent{buyOn(t, d(time.January, 5), 10, 100000)},
		[]domain.HoldingIncome{
			incomeOn(t, domain.IncomeReceived, d(time.February, 1), 900), // Q1, not ours
			incomeOn(t, domain.IncomeReceived, d(time.May, 1), 500),
			incomeOn(t, domain.IncomeFee, d(time.June, 1), 100),
			incomeOn(t, domain.IncomeReceived, d(time.July, 1), 700), // Q3, not ours
		},
		[]domain.Valuation{
			priceOn(t, d(time.March, 31), 10000),
			priceOn(t, d(time.June, 30), 10000),
		},
		"SGD")

	if r.Income.Native.Amount != 500 {
		t.Errorf("income = %d, want 500", r.Income.Native.Amount)
	}
	if r.Fees.Native.Amount != 100 {
		t.Errorf("fees = %d, want 100", r.Fees.Native.Amount)
	}
	// Fees are stored positive and subtracted here, so the period earned S$4.00.
	if r.Total == nil || r.Total.Native.Amount != 400 {
		t.Errorf("total = %v, want 400", r.Total)
	}
}

// Average cost depends on every event before the period, not only those inside
// it. A fold that started at the period boundary would price this sale off the
// April purchase alone and realise nothing.
func TestTheCostBasisComesFromBeforeThePeriodNotFromInsideIt(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 2),
		[]domain.HoldingEvent{
			buyOn(t, d(time.January, 5), 10, 100000), // 10 at S$100.00
			buyOn(t, d(time.April, 5), 10, 300000),   // 10 at S$300.00; blended S$200.00
			sellOn(t, d(time.May, 20), 5, 150000),    // 5 out at S$200.00 cost
		},
		nil,
		[]domain.Valuation{
			priceOn(t, d(time.March, 31), 10000),
			priceOn(t, d(time.June, 30), 30000),
		},
		"SGD")

	// Cost out is 5/20 of S$4,000.00 = S$1,000.00 against S$1,500.00 of
	// proceeds. A window starting in April would use 5/10 of S$3,000.00 and
	// realise nothing at all.
	if r.Realised.Native.Amount != 50000 {
		t.Errorf("realised = %d, want 50000", r.Realised.Native.Amount)
	}
}

// The PRD's own example, and the reason the household's currency is carried
// rather than converted: flat in USD, poorer in SGD. Anyone reading only the
// native figure would conclude this holding did nothing.
func TestAHoldingFlatInItsOwnCurrencyCanStillLoseTheHouseholdMoney(t *testing.T) {
	primaryOpen := money(t, 13500, "SGD")
	primaryClose := money(t, 12000, "SGD")
	primaryCost := money(t, 135000, "SGD")

	h := usdHolding()
	r := mustReturn(t, h, quarter(t, 2),
		[]domain.HoldingEvent{{
			Kind: domain.HoldingAcquisition, Quantity: units(t, 10),
			Amount: money(t, 100000, "USD"), PrimaryAmount: &primaryCost,
			OccurredOn: d(time.January, 5),
		}},
		nil,
		[]domain.Valuation{
			{UnitPrice: money(t, 10000, "USD"), PrimaryUnitPrice: &primaryOpen, AsOf: d(time.March, 31)},
			{UnitPrice: money(t, 10000, "USD"), PrimaryUnitPrice: &primaryClose, AsOf: d(time.June, 30)},
		},
		"SGD")

	if r.Unrealised == nil {
		t.Fatalf("blanked with %q", r.Reason)
	}
	if r.Unrealised.Native.Amount != 0 || r.Unrealised.Native.Currency != "USD" {
		t.Errorf("native unrealised = %v, want 0 USD -- the price did not move", r.Unrealised.Native)
	}
	if r.Unrealised.Primary.Amount != -15000 || r.Unrealised.Primary.Currency != "SGD" {
		t.Errorf("primary unrealised = %v, want -15000 SGD", r.Unrealised.Primary)
	}
}

// A holding that has never been bought reports zeroes rather than blanks: it
// is held through the period at a quantity of nothing, and nothing is knowable
// about it without a price precisely because there is nothing to price.
func TestAHoldingWithNoEventsReportsZeroesNotBlanks(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 2), nil, nil, nil, "SGD")

	if r.Unrealised == nil {
		t.Fatalf("nothing held at either end needs no price, blanked with %q", r.Reason)
	}
	if r.Unrealised.Native.Amount != 0 || r.Realised.Native.Amount != 0 {
		t.Errorf("unrealised %v, realised %v, want both zero", r.Unrealised, r.Realised)
	}
	if r.Total == nil || r.Total.Native.Amount != 0 {
		t.Errorf("total = %v, want 0", r.Total)
	}
}

func TestTheReturnCarriesThePeriodItIsFor(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 3), nil, nil, nil, "SGD")
	if r.Period.Label() != "Q3 2026" {
		t.Errorf("period = %q, want Q3 2026", r.Period.Label())
	}
}

// --- the boundaries themselves ----------------------------------------------
//
// The four tests below exist because a mutation survived without them: the
// first and last days of a period, two prices inside one window, and a period
// whose previous one is in another year. Every one of those is a place an
// off-by-one lives, and this repository has recorded six of them.

// An event on the period's FIRST day belongs to the period, not to the one
// before it. Folded the other way, this holding would look like it was already
// held at the open and would demand a price for a quarter it did not exist in.
func TestAnEventOnTheFirstDayOfThePeriodIsInsideIt(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 2),
		[]domain.HoldingEvent{buyOn(t, d(time.April, 1), 10, 100000)},
		nil,
		[]domain.Valuation{priceOn(t, d(time.June, 30), 12000)},
		"SGD")

	if r.Unrealised == nil {
		t.Fatalf("bought on 1 April, so nothing was held at the open: blanked with %q", r.Reason)
	}
	if r.Unrealised.Native.Amount != 20000 {
		t.Errorf("unrealised = %d, want 20000", r.Unrealised.Native.Amount)
	}
}

// An event on the period's LAST day belongs to the period. A quarter's final
// day is a day the household was still trading on, and dropping it would move
// that sale into the next quarter -- where its basis no longer exists.
func TestAnEventOnTheLastDayOfThePeriodIsInsideIt(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 2),
		[]domain.HoldingEvent{
			buyOn(t, d(time.January, 5), 10, 100000),
			sellOn(t, d(time.June, 30), 10, 130000), // the last day of Q2
		},
		nil,
		[]domain.Valuation{priceOn(t, d(time.March, 31), 10000)},
		"SGD")

	if r.Realised.Native.Amount != 30000 {
		t.Errorf("realised = %d, want 30000 -- the sale is inside Q2", r.Realised.Native.Amount)
	}
	if r.Unrealised == nil {
		t.Fatalf("nothing is held after that sale, so no closing price is needed: blanked with %q", r.Reason)
	}
	if r.Total == nil || r.Total.Native.Amount != 30000 {
		t.Errorf("total = %v, want 30000", r.Total)
	}
}

// When a window holds several prices it is the LAST one that closes it. Both
// ends are tested at once here: an earlier price sits inside each window, and
// picking either of them changes the answer.
func TestTheNEWESTPriceInEachWindowIsTheOneThatCounts(t *testing.T) {
	r := mustReturn(t, sgdHolding(), quarter(t, 2),
		[]domain.HoldingEvent{buyOn(t, d(time.January, 5), 10, 100000)},
		nil,
		[]domain.Valuation{
			priceOn(t, d(time.January, 10), 5000), // Q1, superseded
			priceOn(t, d(time.March, 31), 10000),  // Q1, opens Q2 at S$1,000.00
			priceOn(t, d(time.April, 15), 11000),  // Q2, superseded
			priceOn(t, d(time.June, 30), 12000),   // Q2, closes it at S$1,200.00
		},
		"SGD")

	if r.Unrealised == nil {
		t.Fatalf("blanked with %q", r.Reason)
	}
	if r.Unrealised.Native.Amount != 20000 {
		t.Errorf("unrealised = %d, want 20000 -- the newest price in each window", r.Unrealised.Native.Amount)
	}
}

// The first quarter of a year opens where the fourth quarter of the PREVIOUS
// one closed. A Previous() that decremented the index without the year would
// look for the opening price in Q4 of this year, which has not happened yet.
func TestTheFirstQuarterOfAYearOpensWhereTheLastOneClosed(t *testing.T) {
	december := time.Date(2025, time.December, 1, 0, 0, 0, 0, time.UTC)
	newYearsEve := time.Date(2025, time.December, 31, 0, 0, 0, 0, time.UTC)

	r := mustReturn(t, sgdHolding(), quarter(t, 1),
		[]domain.HoldingEvent{buyOn(t, december, 10, 100000)},
		nil,
		[]domain.Valuation{
			priceOn(t, newYearsEve, 10000),       // Q4 2025 closes, so Q1 2026 opens
			priceOn(t, d(time.March, 31), 12000), // and Q1 2026 closes
		},
		"SGD")

	if r.Unrealised == nil {
		t.Fatalf("Q1 2026 opens at the Q4 2025 price: blanked with %q", r.Reason)
	}
	if r.Unrealised.Native.Amount != 20000 {
		t.Errorf("unrealised = %d, want 20000", r.Unrealised.Native.Amount)
	}
}
