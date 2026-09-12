package domain

import (
	"fmt"
	"time"
)

// BlankReason says why a figure the report would normally show is absent. It
// is never "we computed zero" -- zero is a claim about the household's money,
// and the truth in these cases is that the figure cannot be known. The net
// worth card blanks the same way and for the same reason.
type BlankReason string

const (
	ReasonNoOpeningPrice BlankReason = "no_opening_price"
	ReasonNoClosingPrice BlankReason = "no_closing_price"
)

// ReturnComponent is one figure in both currencies at once: the holding's own,
// and the household's. They are separate numbers rather than one converted
// into the other -- a US stock flat in USD while SGD strengthened made the
// household poorer, and only the primary figure says so.
type ReturnComponent struct {
	Native  Money
	Primary Money
}

// PeriodReturn is what one holding earned over one period, split the three
// ways the PRD pins.
//
// Unrealised and Total are pointers because they are the two that can be
// unknowable: both need a market price at each end of the period that the
// household was still holding through. When they are nil, Reason says which
// price was missing.
//
// Realised, Income and Fees are never nil. They are computed from amounts that
// actually changed hands, so no price is involved and a missing valuation
// cannot take them away. A quarter where the owner sold at a profit and forgot
// to record a price is not an unknowable quarter.
//
// Fees are POSITIVE and already subtracted inside Total. They are reported
// separately so a screen can show what was charged rather than only its effect.
type PeriodReturn struct {
	Period     Period
	Unrealised *ReturnComponent
	Realised   ReturnComponent
	Income     ReturnComponent
	Fees       ReturnComponent
	Total      *ReturnComponent
	Reason     BlankReason

	// The day each end was measured at, so a screen can show how old the
	// figure is. The PRD's top product risk is valuations quietly going
	// stale, and an age nobody can see is how that goes unnoticed.
	//
	// Nil means no price was used at that end -- which happens when nothing
	// was held there, since nothing is worth nothing without asking anybody.
	// A date there would claim a measurement nobody made.
	OpeningPriceAsOf *time.Time
	ClosingPriceAsOf *time.Time
}

// ReturnOver computes what this holding earned over one period.
//
// The shape is cost-carried: each end of the period is measured as (market
// value - cost of what is held), and the period's unrealised figure is the
// difference between the two ends. That is what makes the PRD's most important
// rule -- "buying more must never read as profit" -- true by construction: a
// purchase adds the same amount to both sides, so it moves the figure by
// exactly zero. There is no net-contributions term to forget.
//
// It also gets the PRD's two mid-period rules for free. A holding bought
// inside the period held nothing at the start, so the opening term is zero and
// the figure is its gain since acquisition. A holding sold inside the period
// holds nothing at the end, so the gain it carried in is removed from
// unrealised at the same moment realised picks it up, and the two do not
// double-count.
//
// events, income and prices are this HOLDING's rows, in any order. Passing
// another holding's rows would produce a wrong answer quietly, so callers group
// first -- see usecase.HoldingService.Report.
func (h Holding) ReturnOver(
	period Period,
	events []HoldingEvent,
	income []HoldingIncome,
	prices []Valuation,
	primaryCurrency string,
) (PeriodReturn, error) {
	zeroNative := Money{Amount: 0, Currency: h.Currency}
	zeroPrimary := Money{Amount: 0, Currency: primaryCurrency}

	// Both ends are folded from the BEGINNING of the holding's life, not from
	// the period boundary. Average cost depends on every event before the
	// window, so a fold that started at the boundary would price a sale off
	// the wrong basis -- silently, and only for holdings bought earlier.
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
	// A period's opening value is the preceding period's closing value, so the
	// opening price has to have been recorded in that preceding period. A
	// March price is not what June was worth: computing from it would be the
	// stale-data failure the PRD names as its top product risk.
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

// valueAtClose is what `held` was worth at the end of `window`, using the
// latest price recorded INSIDE that window.
//
// The second return is false when no usable price exists, which is what blanks
// the figure rather than producing a zero. Holding nothing is the exception:
// nothing is worth nothing, provably, and demanding a price for it would blank
// every holding bought mid-period -- the most common case there is.
// The third return is the day of the price that was used, which the caller
// reports so the owner can see how old the figure is. It is nil when nothing
// was held and therefore no price was consulted.
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

// latestPriceIn is the newest valuation dated inside the window -- never one
// dated after it. A price recorded after a quarter closed is information that
// quarter did not have, and letting it serve would mean typing today's price
// silently rewrites an answer somebody has already read.
//
// Two prices cannot share a day: (holding_id, as_of) is UNIQUE and a second
// entry for a day is an update, not a second opinion. A strict comparison
// therefore keeps the first of any pair that somehow does.
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
