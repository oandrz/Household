package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// A holding is what the household owns some of; its events are the acquisitions
// and disposals that got it there. Position folds those events into the three
// figures every screen in this feature needs: how much is still held, what it
// cost, and what has already been realised by selling.
//
// The fold is where this feature's arithmetic is easy to get wrong, so most of
// what follows is about the fold rather than the structs.

func on(day int) time.Time {
	return time.Date(2026, 3, day, 0, 0, 0, 0, time.UTC)
}

func sgdAmount(t *testing.T, minor int64) domain.Money {
	t.Helper()
	m, err := domain.NewMoney(minor, "SGD")
	if err != nil {
		t.Fatalf("NewMoney(%d): %v", minor, err)
	}
	return m
}

// units builds a Quantity from a whole number of units, which is all these
// tests need -- the fractional cases live in quantity_test.go.
func units(t *testing.T, n int64) domain.Quantity {
	t.Helper()
	q, err := domain.NewQuantity(n * domain.QuantityScale)
	if err != nil {
		t.Fatalf("NewQuantity(%d units): %v", n, err)
	}
	return q
}

func buy(t *testing.T, day int, qty, costMinor int64) domain.HoldingEvent {
	t.Helper()
	return domain.HoldingEvent{
		Kind:       domain.HoldingAcquisition,
		Quantity:   units(t, qty),
		Amount:     sgdAmount(t, costMinor),
		OccurredOn: on(day),
	}
}

func sell(t *testing.T, day int, qty, proceedsMinor int64) domain.HoldingEvent {
	t.Helper()
	return domain.HoldingEvent{
		Kind:       domain.HoldingDisposal,
		Quantity:   units(t, qty),
		Amount:     sgdAmount(t, proceedsMinor),
		OccurredOn: on(day),
	}
}

func sgdHolding() domain.Holding {
	return domain.Holding{
		Name:       "Test holding",
		Instrument: domain.InstrumentStock,
		Unit:       "share",
		Currency:   "SGD",
	}
}

// --- the fail-closed parsers -------------------------------------------------

func TestParseInstrumentKindAcceptsEveryKindItShips(t *testing.T) {
	for _, s := range []string{"stock", "gold", "other"} {
		if _, err := domain.ParseInstrumentKind(s); err != nil {
			t.Fatalf("ParseInstrumentKind(%q): %v", s, err)
		}
	}
}

func TestParseInstrumentKindRefusesAnythingElse(t *testing.T) {
	// The value arrives from a database column or a request body, so an
	// unrecognised one is refused rather than carried further -- the same rule
	// ParseContributionSource and ParseTransactionKind follow.
	for _, s := range []string{"", "crypto", "Stock", "bond"} {
		if _, err := domain.ParseInstrumentKind(s); !errors.Is(err, domain.ErrUnknownInstrumentKind) {
			t.Fatalf("ParseInstrumentKind(%q) error = %v, want ErrUnknownInstrumentKind", s, err)
		}
	}
}

func TestParseHoldingEventKindRefusesAnythingElse(t *testing.T) {
	if _, err := domain.ParseHoldingEventKind("acquisition"); err != nil {
		t.Fatalf("acquisition: %v", err)
	}
	if _, err := domain.ParseHoldingEventKind("disposal"); err != nil {
		t.Fatalf("disposal: %v", err)
	}
	for _, s := range []string{"", "buy", "sell", "dividend"} {
		if _, err := domain.ParseHoldingEventKind(s); !errors.Is(err, domain.ErrUnknownHoldingEventKind) {
			t.Fatalf("ParseHoldingEventKind(%q) error = %v, want ErrUnknownHoldingEventKind", s, err)
		}
	}
}

// --- the cross-currency rule, mirroring Transaction.ReceivedAmount -----------

func TestEventRequiresAPrimaryAmountWhenTheHoldingIsNotInPrimaryCurrency(t *testing.T) {
	e := buy(t, 1, 10, 1000)
	e.Amount, _ = domain.NewMoney(1000, "USD")

	if err := e.Validate("USD", "SGD"); !errors.Is(err, domain.ErrHoldingPrimaryAmountRequired) {
		t.Fatalf("Validate error = %v, want ErrHoldingPrimaryAmountRequired", err)
	}
}

func TestEventRefusesAPrimaryAmountWhenItWouldBeTheSameNumber(t *testing.T) {
	// Storing it anyway invites the two figures to disagree later, with nothing
	// to say which one is the truth. The sibling rule to
	// ErrReceivedAmountNotAllowed on a same-currency transfer.
	e := buy(t, 1, 10, 1000)
	p := sgdAmount(t, 1000)
	e.PrimaryAmount = &p

	if err := e.Validate("SGD", "SGD"); !errors.Is(err, domain.ErrHoldingPrimaryAmountNotAllowed) {
		t.Fatalf("Validate error = %v, want ErrHoldingPrimaryAmountNotAllowed", err)
	}
}

func TestEventAcceptsTheTwoShapesThatAreActuallyValid(t *testing.T) {
	same := buy(t, 1, 10, 1000)
	if err := same.Validate("SGD", "SGD"); err != nil {
		t.Fatalf("same-currency event with no primary amount: %v", err)
	}

	cross := buy(t, 1, 10, 1000)
	cross.Amount, _ = domain.NewMoney(1000, "USD")
	p := sgdAmount(t, 1350)
	cross.PrimaryAmount = &p
	if err := cross.Validate("USD", "SGD"); err != nil {
		t.Fatalf("cross-currency event with a primary amount: %v", err)
	}
}

func TestEventRefusesAnAmountInTheWrongCurrency(t *testing.T) {
	e := buy(t, 1, 10, 1000) // SGD
	if err := e.Validate("USD", "SGD"); !errors.Is(err, domain.ErrCurrencyMismatch) {
		t.Fatalf("Validate error = %v, want ErrCurrencyMismatch", err)
	}
}

func TestEventRefusesAZeroQuantity(t *testing.T) {
	// Zero is a legal Quantity -- a holding sold to nothing -- but not a legal
	// event: nothing changed hands, so there is nothing to record.
	e := buy(t, 1, 0, 1000)
	if err := e.Validate("SGD", "SGD"); !errors.Is(err, domain.ErrHoldingEventQuantityNotPositive) {
		t.Fatalf("Validate error = %v, want ErrHoldingEventQuantityNotPositive", err)
	}
}

// --- Money.Prorate, the sibling of Quantity.Value ---------------------------

func TestProrateSplitsAPoolByQuantity(t *testing.T) {
	pool := sgdAmount(t, 3000)

	got, err := pool.Prorate(units(t, 5), units(t, 20))
	if err != nil {
		t.Fatalf("Prorate: %v", err)
	}
	if got.Amount != 750 {
		t.Fatalf("Amount = %d, want 750", got.Amount)
	}
}

func TestProrateRefusesAnEmptyWhole(t *testing.T) {
	// Dividing a cost pool by a zero holding has no answer, and returning zero
	// would silently report that a disposal cost nothing -- which reads as pure
	// profit.
	pool := sgdAmount(t, 3000)
	if _, err := pool.Prorate(units(t, 1), units(t, 0)); !errors.Is(err, domain.ErrProrateWholeNotPositive) {
		t.Fatalf("error = %v, want ErrProrateWholeNotPositive", err)
	}
}

// A cost pool is never negative: events refuse a negative amount, and a
// disposal's cost is capped at the pool it comes out of. Prorate therefore
// refuses a negative amount outright rather than carrying sign-handling code
// that nothing exercises -- untested two's-complement care on a monetary path
// is exactly what "no cleverness in security-sensitive code" is about.
func TestProrateRefusesANegativeAmount(t *testing.T) {
	pool := sgdAmount(t, -100)
	if _, err := pool.Prorate(units(t, 1), units(t, 2)); !errors.Is(err, domain.ErrInvalidMoney) {
		t.Fatalf("error = %v, want ErrInvalidMoney", err)
	}
}

// Prorate's own refusal is deliberately NOT ErrHoldingOversold. Both guards
// fire on the same shape, but this one means "that is not a proportion" while
// the fold's means "this household does not own that much" -- and while they
// shared an error, removing either one left the other still producing it, so
// neither guard was actually pinned. Found by mutation testing.
func TestProrateRefusesAPartLargerThanTheWhole(t *testing.T) {
	pool := sgdAmount(t, 3000)
	if _, err := pool.Prorate(units(t, 21), units(t, 20)); !errors.Is(err, domain.ErrProratePartExceedsWhole) {
		t.Fatalf("error = %v, want ErrProratePartExceedsWhole", err)
	}
}

// Prorate multiplies a money amount by a nano quantity, so it hits the same
// 128-bit intermediate Quantity.Value does, and must not overflow on figures a
// real IDR portfolio produces. Rp 1,000,000,000 (1e11 minor) across 100,000
// units gives an intermediate of 1e11 * 1e14 = 1e25, far past int64 and past
// uint64 too.
func TestProrateDoesNotOverflowOnALargeIDRPool(t *testing.T) {
	pool, err := domain.NewMoney(100_000_000_000, "IDR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}

	got, err := pool.Prorate(units(t, 25_000), units(t, 100_000))
	if err != nil {
		t.Fatalf("Prorate returned %v -- a real IDR pool must divide, not overflow", err)
	}
	if got.Amount != 25_000_000_000 {
		t.Fatalf("Amount = %d, want 25000000000", got.Amount)
	}
}

// --- the fold ---------------------------------------------------------------

func TestPositionOfASingleAcquisitionIsItsQuantityAndItsCost(t *testing.T) {
	p, err := sgdHolding().Position([]domain.HoldingEvent{buy(t, 1, 10, 1000)}, "SGD")
	if err != nil {
		t.Fatalf("Position: %v", err)
	}
	if p.Held.Nano() != 10*domain.QuantityScale {
		t.Fatalf("Held = %d, want 10 units", p.Held.Nano())
	}
	if p.Cost.Amount != 1000 {
		t.Fatalf("Cost = %d, want 1000", p.Cost.Amount)
	}
	if p.Realised.Amount != 0 {
		t.Fatalf("Realised = %d, want 0", p.Realised.Amount)
	}
}

// The asymmetry that makes average cost correct: buying at a new price moves
// the average, selling never does. A disposal takes its own cost out of the
// pool in exact proportion, so what is left is still valued at the same average
// as before the sale.
func TestADisposalLeavesTheAverageCostAloneAndAnAcquisitionMovesIt(t *testing.T) {
	h := sgdHolding()

	// 10 @ 100 then 10 @ 200 -> 20 units costing 3000, an average of 150.
	afterBuys, err := h.Position([]domain.HoldingEvent{buy(t, 1, 10, 1000), buy(t, 2, 10, 2000)}, "SGD")
	if err != nil {
		t.Fatalf("Position: %v", err)
	}
	if afterBuys.Cost.Amount != 3000 || afterBuys.Held.Nano() != 20*domain.QuantityScale {
		t.Fatalf("after buys: cost %d held %d, want 3000 and 20 units", afterBuys.Cost.Amount, afterBuys.Held.Nano())
	}

	// Selling 5 must remove exactly 5 * 150 = 750 of cost, leaving 15 units at
	// 2250 -- still an average of 150. The sale price is deliberately nothing
	// like 150, to prove it does not leak into the average.
	afterSale, err := h.Position([]domain.HoldingEvent{
		buy(t, 1, 10, 1000), buy(t, 2, 10, 2000), sell(t, 3, 5, 9999),
	}, "SGD")
	if err != nil {
		t.Fatalf("Position: %v", err)
	}
	if afterSale.Held.Nano() != 15*domain.QuantityScale {
		t.Fatalf("Held = %d, want 15 units", afterSale.Held.Nano())
	}
	if afterSale.Cost.Amount != 2250 {
		t.Fatalf("Cost = %d, want 2250 -- a disposal must not move the average", afterSale.Cost.Amount)
	}

	// A further buy at 300 does move it: 2250 + 3000 = 5250 over 25 units.
	afterBuyAgain, err := h.Position([]domain.HoldingEvent{
		buy(t, 1, 10, 1000), buy(t, 2, 10, 2000), sell(t, 3, 5, 9999), buy(t, 4, 10, 3000),
	}, "SGD")
	if err != nil {
		t.Fatalf("Position: %v", err)
	}
	if afterBuyAgain.Cost.Amount != 5250 {
		t.Fatalf("Cost = %d, want 5250 -- an acquisition must move the average", afterBuyAgain.Cost.Amount)
	}
}

// Realised gain is proceeds minus the cost of what was sold, valued at the
// average *at the time of that sale* -- not the final average. The second sale
// below is priced off a pool a later purchase had already moved, so a fold that
// used one average for everything would get it wrong.
func TestRealisedGainUsesTheAverageAtTheTimeOfEachSale(t *testing.T) {
	p, err := sgdHolding().Position([]domain.HoldingEvent{
		buy(t, 1, 10, 1000), // avg 100
		buy(t, 2, 10, 2000), // avg 150 over 20
		sell(t, 3, 5, 1500), // cost 750, realised 750; 15 left at 2250
		buy(t, 4, 10, 3000), // avg 210 over 25
		sell(t, 5, 5, 2000), // cost 1050, realised 950; total 1700
	}, "SGD")
	if err != nil {
		t.Fatalf("Position: %v", err)
	}
	if p.Realised.Amount != 1700 {
		t.Fatalf("Realised = %d, want 1700", p.Realised.Amount)
	}
	if p.Held.Nano() != 20*domain.QuantityScale {
		t.Fatalf("Held = %d, want 20 units", p.Held.Nano())
	}
	if p.Cost.Amount != 4200 {
		t.Fatalf("Cost = %d, want 4200", p.Cost.Amount)
	}
}

// The fold must order events by date itself. A repository returning rows in
// insertion order, or a caller appending a backdated correction, would
// otherwise produce a different answer for the same holding -- and the
// difference is silent.
func TestPositionFoldsInDateOrderNotSliceOrder(t *testing.T) {
	inOrder := []domain.HoldingEvent{
		buy(t, 1, 10, 1000), buy(t, 2, 10, 2000), sell(t, 3, 5, 1500),
	}
	shuffled := []domain.HoldingEvent{
		sell(t, 3, 5, 1500), buy(t, 2, 10, 2000), buy(t, 1, 10, 1000),
	}

	want, err := sgdHolding().Position(inOrder, "SGD")
	if err != nil {
		t.Fatalf("Position(inOrder): %v", err)
	}
	got, err := sgdHolding().Position(shuffled, "SGD")
	if err != nil {
		t.Fatalf("Position(shuffled): %v", err)
	}

	if got != want {
		t.Fatalf("shuffled = %+v, in order = %+v -- the fold must sort by date", got, want)
	}
}

// Selling more than is held is refused rather than producing a negative
// holding. NewQuantity already refuses a negative, so the fold must run its
// running total through it rather than subtracting raw int64s.
func TestPositionRefusesToSellMoreThanIsHeld(t *testing.T) {
	_, err := sgdHolding().Position([]domain.HoldingEvent{
		buy(t, 1, 10, 1000), sell(t, 2, 11, 5000),
	}, "SGD")
	if !errors.Is(err, domain.ErrHoldingOversold) {
		t.Fatalf("Position error = %v, want ErrHoldingOversold", err)
	}
}

func TestPositionOfAHoldingWithNoEventsIsZeroInItsOwnCurrency(t *testing.T) {
	h := sgdHolding()
	h.Currency = "IDR"

	p, err := h.Position(nil, "SGD")
	if err != nil {
		t.Fatalf("Position: %v", err)
	}
	if p.Held.Nano() != 0 || p.Cost.Amount != 0 || p.Realised.Amount != 0 {
		t.Fatalf("got %+v, want all zero", p)
	}
	// A zero-value Money has no currency, and Money.Add refuses one -- so a
	// fresh holding's figures have to carry the holding's currency from the
	// start or the first Add against them fails.
	if p.Cost.Currency != "IDR" || p.Realised.Currency != "IDR" {
		t.Fatalf("currencies = %q/%q, want IDR", p.Cost.Currency, p.Realised.Currency)
	}
}

// Two events on the same day are a real case -- occurred_on is a date, so
// buying and selling the same morning share one -- and the order between them
// changes the answer: buying first dilutes the average the sale is costed
// against. Buy-then-sell realises 750 here; sell-then-buy realises 1000.
//
// The fold sorts stably, so a tie keeps the caller's order. That makes the
// REPOSITORY's ordering load-bearing, which is why HoldingEventRepository's
// contract fixes it at (occurred_on, created_at, id) -- the order the events
// were actually recorded in. This test pins the domain half of that bargain:
// if the sort ever stops being stable, the repository's guarantee stops
// meaning anything and this fails.
func TestPositionKeepsSliceOrderForEventsOnTheSameDay(t *testing.T) {
	base := buy(t, 1, 10, 1000)
	buyThenSell := []domain.HoldingEvent{base, buy(t, 1, 10, 2000), sell(t, 1, 5, 1500)}
	sellThenBuy := []domain.HoldingEvent{base, sell(t, 1, 5, 1500), buy(t, 1, 10, 2000)}

	first, err := sgdHolding().Position(buyThenSell, "SGD")
	if err != nil {
		t.Fatalf("Position(buyThenSell): %v", err)
	}
	second, err := sgdHolding().Position(sellThenBuy, "SGD")
	if err != nil {
		t.Fatalf("Position(sellThenBuy): %v", err)
	}

	if first.Realised.Amount != 750 {
		t.Fatalf("buy-then-sell realised = %d, want 750", first.Realised.Amount)
	}
	if second.Realised.Amount != 1000 {
		t.Fatalf("sell-then-buy realised = %d, want 1000 -- a stable sort must keep slice order on a tie", second.Realised.Amount)
	}
}

// The test above pins the two answers but not the mechanism. Pinning the
// stability itself needs care: Go's sort.Slice leaves an all-equal slice
// completely untouched (pdqsort spots the already-sorted run), so a dozen
// events on one day cannot tell a stable sort from an unstable one. Ties
// inside a slice that genuinely needs sorting are a different matter -- there
// sort.Slice does reorder equal keys, from about a dozen elements up.
//
// So this case mixes two days. The disposal sits second among the day-one
// events, and nine more day-one acquisitions follow it, with day-two
// acquisitions scattered through the slice so the sort has real work to do.
// In recorded order the sale is costed against ten units at an average of 100:
// 500 of cost leaves and 1000 is realised. Let it drift past the later
// day-one buys and it is costed against a much larger pool, and the realised
// figure moves -- same events, same days, different answer.
func TestPositionKeepsSameDayOrderWhenTheSortHasRealWorkToDo(t *testing.T) {
	dayOne := []domain.HoldingEvent{buy(t, 1, 10, 1000), sell(t, 1, 5, 1500)}
	for i := 0; i < 9; i++ {
		dayOne = append(dayOne, buy(t, 1, 1, 200))
	}
	dayTwo := make([]domain.HoldingEvent, 0, 6)
	for i := 0; i < 6; i++ {
		dayTwo = append(dayTwo, buy(t, 2, 1, 300))
	}

	// Scatter the later day through the slice, keeping each day's own order.
	// Built by walking a fixed length rather than draining two cursors, so the
	// loop cannot fail to terminate.
	events := make([]domain.HoldingEvent, 0, len(dayOne)+len(dayTwo))
	i, j := 0, 0
	for len(events) < cap(events) {
		takeDayTwo := (len(events)%3 == 2 && j < len(dayTwo)) || i == len(dayOne)
		if takeDayTwo {
			events = append(events, dayTwo[j])
			j++
			continue
		}
		events = append(events, dayOne[i])
		i++
	}

	p, err := sgdHolding().Position(events, "SGD")
	if err != nil {
		t.Fatalf("Position: %v", err)
	}
	if p.Realised.Amount != 1000 {
		t.Fatalf("Realised = %d, want 1000 -- same-day events must stay in the order they were recorded", p.Realised.Amount)
	}
	if p.Held.Nano() != 20*domain.QuantityScale {
		t.Fatalf("Held = %d, want 20 units", p.Held.Nano())
	}
	if p.Cost.Amount != 4100 {
		t.Fatalf("Cost = %d, want 4100", p.Cost.Amount)
	}
}

// --- valuations --------------------------------------------------------------

func valuation(t *testing.T, day int, unitPriceMinor int64) domain.Valuation {
	t.Helper()
	return domain.Valuation{UnitPrice: sgdAmount(t, unitPriceMinor), AsOf: on(day)}
}

func TestMarketValueIsTheHeldQuantityAtTheValuationPrice(t *testing.T) {
	v := valuation(t, 1, 250)

	got, err := v.MarketValue(units(t, 20))
	if err != nil {
		t.Fatalf("MarketValue: %v", err)
	}
	if got.Amount != 5000 {
		t.Fatalf("Amount = %d, want 5000", got.Amount)
	}
	if got.Currency != "SGD" {
		t.Fatalf("Currency = %q, want SGD", got.Currency)
	}
}

// A valuation carries the same cross-currency rule its holding's events do,
// because it is the same question: a figure in USD needs its primary-currency
// twin, and one already in primary must not carry a second. The rule lives in
// one place so the two cannot drift.
func TestValuationCarriesTheSameCrossCurrencyRuleAsAnEvent(t *testing.T) {
	crossNoPrimary := domain.Valuation{AsOf: on(1)}
	crossNoPrimary.UnitPrice, _ = domain.NewMoney(250, "USD")
	if err := crossNoPrimary.Validate("USD", "SGD"); !errors.Is(err, domain.ErrHoldingPrimaryAmountRequired) {
		t.Fatalf("cross-currency with no primary: error = %v, want ErrHoldingPrimaryAmountRequired", err)
	}

	samePlusPrimary := valuation(t, 1, 250)
	p := sgdAmount(t, 250)
	samePlusPrimary.PrimaryUnitPrice = &p
	if err := samePlusPrimary.Validate("SGD", "SGD"); !errors.Is(err, domain.ErrHoldingPrimaryAmountNotAllowed) {
		t.Fatalf("same-currency with a primary: error = %v, want ErrHoldingPrimaryAmountNotAllowed", err)
	}

	if err := valuation(t, 1, 250).Validate("SGD", "SGD"); err != nil {
		t.Fatalf("same-currency, no primary: %v", err)
	}
}

func TestValuationRefusesANegativeUnitPrice(t *testing.T) {
	if err := valuation(t, 1, -1).Validate("SGD", "SGD"); !errors.Is(err, domain.ErrInvalidMoney) {
		t.Fatalf("error = %v, want ErrInvalidMoney", err)
	}
}

func TestValuationRefusesAPriceInTheWrongCurrency(t *testing.T) {
	if err := valuation(t, 1, 250).Validate("USD", "SGD"); !errors.Is(err, domain.ErrCurrencyMismatch) {
		t.Fatalf("error = %v, want ErrCurrencyMismatch", err)
	}
}

// --- the primary-currency pool ----------------------------------------------
//
// A holding in a currency the household does not keep its books in carries two
// cost pools, folded side by side: the native one, and the household's own.
// The second is not derivable from the first afterwards, which is the whole
// reason it is folded rather than converted -- see the tests below.

func usdHolding() domain.Holding {
	return domain.Holding{
		Name:       "US stock",
		Instrument: domain.InstrumentStock,
		Unit:       "share",
		Currency:   "USD",
	}
}

func money(t *testing.T, minor int64, currency string) domain.Money {
	t.Helper()
	m, err := domain.NewMoney(minor, currency)
	if err != nil {
		t.Fatalf("NewMoney(%d, %s): %v", minor, currency, err)
	}
	return m
}

// buyUSD is an acquisition of a USD holding by a household whose books are in
// SGD: the amount that left the brokerage in USD, and the amount that left the
// bank in SGD, both recorded because the owner knows both.
func buyUSD(t *testing.T, day int, qty, usdMinor, sgdMinor int64) domain.HoldingEvent {
	t.Helper()
	primary := money(t, sgdMinor, "SGD")
	return domain.HoldingEvent{
		Kind:          domain.HoldingAcquisition,
		Quantity:      units(t, qty),
		Amount:        money(t, usdMinor, "USD"),
		PrimaryAmount: &primary,
		OccurredOn:    on(day),
	}
}

func sellUSD(t *testing.T, day int, qty, usdMinor, sgdMinor int64) domain.HoldingEvent {
	t.Helper()
	primary := money(t, sgdMinor, "SGD")
	return domain.HoldingEvent{
		Kind:          domain.HoldingDisposal,
		Quantity:      units(t, qty),
		Amount:        money(t, usdMinor, "USD"),
		PrimaryAmount: &primary,
		OccurredOn:    on(day),
	}
}

// The test the whole decision rests on. Two lots bought at the same USD price
// but different exchange rates blend to an SGD cost per unit that is neither
// rate: S$135.00 and S$130.00 per unit average to S$132.50. No single rate
// applied to the USD realised figure produces the SGD one, which is why the
// primary pool is folded rather than converted after the fact.
func TestRealisedInPrimaryCurrencyUsesTheBlendedRateNotTheLatestOne(t *testing.T) {
	h := usdHolding()
	events := []domain.HoldingEvent{
		buyUSD(t, 1, 10, 100000, 135000), // US$1,000.00 cost S$1,350.00
		buyUSD(t, 2, 10, 100000, 130000), // US$1,000.00 cost S$1,300.00
		sellUSD(t, 3, 10, 120000, 160000),
	}

	p, err := h.Position(events, "SGD")
	if err != nil {
		t.Fatalf("Position: %v", err)
	}

	// Native: cost pool US$2,000.00, half of it leaves, realised is
	// US$1,200.00 - US$1,000.00.
	if p.Realised.Amount != 20000 || p.Realised.Currency != "USD" {
		t.Errorf("Realised = %d %s, want 20000 USD", p.Realised.Amount, p.Realised.Currency)
	}
	// Primary: cost pool S$2,650.00, half of it -- S$1,325.00, ten units at
	// the blended S$132.50 -- leaves against S$1,600.00 of proceeds.
	if p.RealisedPrimary.Amount != 27500 || p.RealisedPrimary.Currency != "SGD" {
		t.Errorf("RealisedPrimary = %d %s, want 27500 SGD", p.RealisedPrimary.Amount, p.RealisedPrimary.Currency)
	}
	// What is still held cost the other half of the blended pool, which is
	// what keeps the average per unit unchanged by the sale.
	if p.CostPrimary.Amount != 132500 || p.CostPrimary.Currency != "SGD" {
		t.Errorf("CostPrimary = %d %s, want 132500 SGD", p.CostPrimary.Amount, p.CostPrimary.Currency)
	}
	if p.Cost.Amount != 100000 || p.Cost.Currency != "USD" {
		t.Errorf("Cost = %d %s, want 100000 USD", p.Cost.Amount, p.Cost.Currency)
	}
}

// A holding already in the household's currency carries no second amount --
// validatePrimaryAmount refuses one, because two figures for the same number
// can disagree later with nothing to say which is true. The fold fills the
// primary pool from the native one instead, so every caller reads the primary
// figures without asking which case it is in.
func TestAHoldingAlreadyInThePrimaryCurrencyFillsBothPoolsFromOneAmount(t *testing.T) {
	h := sgdHolding()
	p, err := h.Position([]domain.HoldingEvent{
		buy(t, 1, 10, 100000),
		sell(t, 2, 4, 50000),
	}, "SGD")
	if err != nil {
		t.Fatalf("Position: %v", err)
	}

	if p.CostPrimary != p.Cost {
		t.Errorf("CostPrimary = %v, want it to equal Cost %v", p.CostPrimary, p.Cost)
	}
	if p.RealisedPrimary != p.Realised {
		t.Errorf("RealisedPrimary = %v, want it to equal Realised %v", p.RealisedPrimary, p.Realised)
	}
}

// An empty holding's primary pool is zero in the HOUSEHOLD's currency, not in
// the holding's. A zero carrying the wrong currency code fails at the first
// Add rather than at the point it was built, three calls away from the cause.
func TestAnEmptyPositionsPrimaryPoolIsInTheHouseholdsCurrency(t *testing.T) {
	p, err := usdHolding().Position(nil, "SGD")
	if err != nil {
		t.Fatalf("Position: %v", err)
	}
	if p.CostPrimary.Currency != "SGD" || p.CostPrimary.Amount != 0 {
		t.Errorf("CostPrimary = %d %s, want 0 SGD", p.CostPrimary.Amount, p.CostPrimary.Currency)
	}
	if p.RealisedPrimary.Currency != "SGD" || p.RealisedPrimary.Amount != 0 {
		t.Errorf("RealisedPrimary = %d %s, want 0 SGD", p.RealisedPrimary.Amount, p.RealisedPrimary.Currency)
	}
	if p.Cost.Currency != "USD" {
		t.Errorf("Cost currency = %s, want USD", p.Cost.Currency)
	}
}

// The fold reads rows it did not construct. A primary amount in a third
// currency is refused rather than added into the household's pool -- the
// fail-closed rule CLAUDE.md states for any value arriving from a database
// column.
func TestTheFoldRefusesAPrimaryAmountInSomeOtherCurrency(t *testing.T) {
	wrong := money(t, 999, "IDR")
	events := []domain.HoldingEvent{{
		Kind:          domain.HoldingAcquisition,
		Quantity:      units(t, 1),
		Amount:        money(t, 100, "USD"),
		PrimaryAmount: &wrong,
		OccurredOn:    on(1),
	}}
	if _, err := usdHolding().Position(events, "SGD"); err == nil {
		t.Fatal("a primary amount in IDR must not fold into an SGD pool")
	}
}
