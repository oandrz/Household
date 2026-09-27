package domain

import (
	"fmt"
	"time"
)

// BlankReason says why a figure the report would normally show is absent --
// never "we computed zero": the truth is the figure can't be known, the same
// reason the net worth card blanks.
type BlankReason string

const (
	ReasonNoOpeningPrice BlankReason = "no_opening_price"
	ReasonNoClosingPrice BlankReason = "no_closing_price"
)

// ReturnComponent holds one figure in both currencies, kept separate rather
// than converted -- a US stock flat in USD while SGD strengthened still made
// the household poorer, which only the primary figure shows.
type ReturnComponent struct {
	Native  Money
	Primary Money
}

// PeriodReturn is what one holding earned over one period, split the three
// ways the PRD pins. Unrealised and Total are pointers: the two figures that
// can be unknowable, since each needs a market price at every end of the
// period the household was still holding through; Reason says which price was
// missing when they're nil. Realised, Income and Fees are never nil --
// computed from amounts that actually changed hands, so no missing valuation
// can take them away. Fees are POSITIVE and already subtracted inside Total,
// reported separately so a screen shows what was charged, not just its
// effect.
type PeriodReturn struct {
	Period     Period
	Unrealised *ReturnComponent
	Realised   ReturnComponent
	Income     ReturnComponent
	Fees       ReturnComponent
	Total      *ReturnComponent
	Reason     BlankReason

	// The day each end was measured at, so a screen can show how old the figure
	// is -- valuations quietly going stale is the PRD's top risk, and an
	// invisible age is how that's missed. Nil means no price was used at that
	// end, since nothing was held there -- nothing is worth nothing without
	// asking anybody, and a date there would claim a measurement nobody made.
	OpeningPriceAsOf *time.Time
	ClosingPriceAsOf *time.Time
}

// ReturnOver computes what this holding earned over one period, on a
// cost-carried shape: each end is (market value - cost of what is held), and
// unrealised is the difference between the two -- which makes the PRD's core
// rule, "buying more must never read as profit", true by construction, since a
// purchase adds equally to both sides. It also gets two mid-period rules for
// free: a holding bought inside the period held nothing at the start, so the
// figure is its gain since acquisition; one sold inside it holds nothing at
// the end, so the carried-in gain moves from unrealised to realised at that
// moment without double-counting. events, income and prices must be this
// HOLDING's rows, in any order -- another holding's rows would give a silently
// wrong answer, so callers group first (see usecase.HoldingService.Report).
func (h Holding) ReturnOver(
	period Period,
	events []HoldingEvent,
	income []HoldingIncome,
	prices []Valuation,
	primaryCurrency string,
) (PeriodReturn, error) {
	zeroNative := Money{Amount: 0, Currency: h.Currency}
	zeroPrimary := Money{Amount: 0, Currency: primaryCurrency}

	// Both ends are folded from the BEGINNING of the holding's life, not the
	// period boundary: average cost depends on every event before the window, so
	// starting there would price a sale off the wrong basis, silently, for
	// holdings bought earlier.
	open, err := h.Position(eventsBefore(events, period.Start()), primaryCurrency)
	if err != nil {
		return PeriodReturn{}, err
	}
	closing, err := h.Position(eventsThrough(events, period.End()), primaryCurrency)
	if err != nil {
		return PeriodReturn{}, err
	}

	realised, err := subtractComponent(
		ReturnComponent{Native: closing.Realised, Primary: closing.RealisedPrimary},
		ReturnComponent{Native: open.Realised, Primary: open.RealisedPrimary})
	if err != nil {
		return PeriodReturn{}, err
	}

	received, fees, err := sumIncome(income, period, zeroNative, zeroPrimary)
	if err != nil {
		return PeriodReturn{}, err
	}

	out := PeriodReturn{
		Period:   period,
		Realised: realised,
		Income:   received,
		Fees:     fees,
	}

	// The closing price is looked for first because it names the period the
	// owner is actually looking at: "no price recorded in Q2" is something
	// they can act on, where "no price in Q1" sends them one period back.
	closeValue, closedAt, ok, err := valueAtClose(closing.Held, prices, period, zeroNative, zeroPrimary)
	if err != nil {
		return PeriodReturn{}, err
	}
	if !ok {
		out.Reason = ReasonNoClosingPrice
		return out, nil
	}
	out.ClosingPriceAsOf = closedAt
	// A period opens at the preceding period's closing value, so the opening
	// price must come from that period -- a March price isn't what June was
	// worth, and using it would be the PRD's top-risk stale-data failure.
	previous, err := period.Previous()
	if err != nil {
		return PeriodReturn{}, err
	}
	openValue, openedAt, ok, err := valueAtClose(open.Held, prices, previous, zeroNative, zeroPrimary)
	if err != nil {
		return PeriodReturn{}, err
	}
	if !ok {
		out.Reason = ReasonNoOpeningPrice
		return out, nil
	}
	out.OpeningPriceAsOf = openedAt

	closeGain, err := subtractComponent(closeValue, ReturnComponent{Native: closing.Cost, Primary: closing.CostPrimary})
	if err != nil {
		return PeriodReturn{}, err
	}
	openGain, err := subtractComponent(openValue, ReturnComponent{Native: open.Cost, Primary: open.CostPrimary})
	if err != nil {
		return PeriodReturn{}, err
	}
	unrealised, err := subtractComponent(closeGain, openGain)
	if err != nil {
		return PeriodReturn{}, err
	}

	total, err := addComponent(unrealised, realised)
	if err != nil {
		return PeriodReturn{}, err
	}
	if total, err = addComponent(total, received); err != nil {
		return PeriodReturn{}, err
	}
	if total, err = subtractComponent(total, fees); err != nil {
		return PeriodReturn{}, err
	}

	out.Unrealised = &unrealised
	out.Total = &total
	return out, nil
}

// valueAtClose is what `held` was worth at the end of `window`, at the latest
// price recorded inside it. The second return is false when no usable price
// exists, blanking the figure rather than showing a zero -- except when
// nothing is held, since nothing is worth nothing, and demanding a price there
// would blank every mid-period purchase, the most common case. The third
// return is the price's date, so the caller can show how stale the figure is;
// nil when nothing was held.
func valueAtClose(held Quantity, prices []Valuation, window Period, zeroNative, zeroPrimary Money) (ReturnComponent, *time.Time, bool, error) {
	if held.Nano() == 0 {
		return ReturnComponent{Native: zeroNative, Primary: zeroPrimary}, nil, true, nil
	}
	price, ok := latestPriceIn(prices, window)
	if !ok {
		return ReturnComponent{}, nil, false, nil
	}
	native, err := price.MarketValue(held)
	if err != nil {
		return ReturnComponent{}, nil, false, err
	}
	primary, err := price.PrimaryMarketValue(held)
	if err != nil {
		return ReturnComponent{}, nil, false, err
	}
	asOf := price.AsOf
	return ReturnComponent{Native: native, Primary: primary}, &asOf, true, nil
}

// latestPriceIn is the newest valuation dated inside the window, never one
// dated after it -- a price recorded after a quarter closed is information
// that quarter didn't have, and letting it serve would mean typing today's
// price silently rewrites an answer someone has already read. Two prices can't
// share a day ((holding_id, as_of) is UNIQUE), so a strict comparison keeps
// the first of any pair that somehow does.
func latestPriceIn(prices []Valuation, window Period) (Valuation, bool) {
	var newest Valuation
	found := false
	for _, p := range prices {
		if !window.Contains(p.AsOf) {
			continue
		}
		if !found || startOfDayUTC(p.AsOf).After(startOfDayUTC(newest.AsOf)) {
			newest, found = p, true
		}
	}
	return newest, found
}

// sumIncome totals the period's payments and charges separately. Both come
// back positive; ReturnOver subtracts the fees.
func sumIncome(rows []HoldingIncome, period Period, zeroNative, zeroPrimary Money) (received, fees ReturnComponent, err error) {
	received = ReturnComponent{Native: zeroNative, Primary: zeroPrimary}
	fees = ReturnComponent{Native: zeroNative, Primary: zeroPrimary}

	for _, r := range rows {
		if !period.Contains(r.ReceivedOn) {
			continue
		}
		one := ReturnComponent{Native: r.Amount, Primary: r.InPrimary()}
		switch r.Kind {
		case IncomeReceived:
			if received, err = addComponent(received, one); err != nil {
				return received, fees, err
			}
		case IncomeFee:
			if fees, err = addComponent(fees, one); err != nil {
				return received, fees, err
			}
		default:
			// A kind that is neither reaches here only from a row this
			// package did not construct.
			return received, fees, fmt.Errorf("%w: %q", ErrUnknownIncomeKind, r.Kind)
		}
	}
	return received, fees, nil
}

// eventsBefore is everything that happened strictly before the day `start`.
func eventsBefore(events []HoldingEvent, start time.Time) []HoldingEvent {
	out := make([]HoldingEvent, 0, len(events))
	for _, e := range events {
		if startOfDayUTC(e.OccurredOn).Before(start) {
			out = append(out, e)
		}
	}
	return out
}

// eventsThrough is everything up to and INCLUDING the day `end`. Inclusive
// because a period's last day is a day the household was still trading on.
func eventsThrough(events []HoldingEvent, end time.Time) []HoldingEvent {
	out := make([]HoldingEvent, 0, len(events))
	for _, e := range events {
		if !startOfDayUTC(e.OccurredOn).After(end) {
			out = append(out, e)
		}
	}
	return out
}

func addComponent(a, b ReturnComponent) (ReturnComponent, error) {
	native, err := a.Native.Add(b.Native)
	if err != nil {
		return ReturnComponent{}, err
	}
	primary, err := a.Primary.Add(b.Primary)
	if err != nil {
		return ReturnComponent{}, err
	}
	return ReturnComponent{Native: native, Primary: primary}, nil
}

// subtractComponent goes through Add with a negated amount rather than a new
// Sub method, because Add is where the currency check and the overflow check
// already live and there should be one of each.
func subtractComponent(a, b ReturnComponent) (ReturnComponent, error) {
	return addComponent(a, ReturnComponent{
		Native:  Money{Amount: -b.Native.Amount, Currency: b.Native.Currency},
		Primary: Money{Amount: -b.Primary.Amount, Currency: b.Primary.Currency},
	})
}
