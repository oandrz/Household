package domain

import (
	"fmt"
	"time"
)

// IncomeKind is which direction a non-trade cash movement went. A dividend,
// coupon or distribution is IncomeReceived, adding to what the holding earned
// without touching what's held or what it cost; a custody, platform or storage
// charge is IncomeFee, and reduces it. Both are stored POSITIVE and the report
// subtracts fees when it sums a period: a negative fee would be the only
// negative amount in this package, and one exception is how a rule stops
// being a rule. Brokerage is NOT a fee row: an acquisition records the whole
// amount that left the bank, a disposal the whole amount that arrived, so
// commission is already inside the cost basis and proceeds; only a charge
// that buys nothing gets its own row.
type IncomeKind string

const (
	IncomeReceived IncomeKind = "income"
	IncomeFee      IncomeKind = "fee"
)

// ParseIncomeKind refuses anything it does not recognise, the same way every
// other parser in this package does -- this value arrives from a database
// column or a request body.
func ParseIncomeKind(s string) (IncomeKind, error) {
	switch IncomeKind(s) {
	case IncomeReceived:
		return IncomeReceived, nil
	case IncomeFee:
		return IncomeFee, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownIncomeKind, s)
	}
}

// HoldingIncome is one payment received from a holding, or one charge against
// it, on a day. It's deliberately NOT a HoldingEventKind: income changes
// neither the quantity held nor its cost, so folding it through the
// average-cost pool would make every disposal after a dividend realise the
// wrong number -- it arrives with the period report instead. PrimaryAmount
// carries the household's currency under the same contract as
// HoldingEvent.PrimaryAmount: the owner knows the amount that reached their
// bank, not the rate. ReceivedOn is the day the money moved, not typed; the
// report buckets by it.
type HoldingIncome struct {
	ID            string
	HoldingID     string
	HouseholdID   string
	Kind          IncomeKind
	Amount        Money
	PrimaryAmount *Money
	ReceivedOn    time.Time
	Note          string
}

// Validate checks the row against the currency of the holding it belongs to
// and the household's primary currency, through the same
// validatePrimaryAmount every other row in this feature uses.
func (i HoldingIncome) Validate(holdingCurrency, primaryCurrency string) error {
	if _, err := ParseIncomeKind(string(i.Kind)); err != nil {
		return err
	}
	if i.Amount.Currency != holdingCurrency {
		return fmt.Errorf("%w: income is %s, holding is %s", ErrCurrencyMismatch, i.Amount.Currency, holdingCurrency)
	}
	// Zero and negative share one error. Zero means nothing changed hands, the
	// reason a zero-quantity event is refused too; negative means a fee was
	// entered wrong -- fees are positive and subtracted at summary time, which
	// this error's message says, better than a generic "cannot be negative".
	if i.Amount.Amount <= 0 {
		return fmt.Errorf("%w: got %d", ErrHoldingIncomeAmountNotPositive, i.Amount.Amount)
	}
	return validatePrimaryAmount(i.PrimaryAmount, holdingCurrency, primaryCurrency)
}

// InPrimary is the amount in the household's currency: the separately
// recorded one when the holding is in another currency, the native amount
// otherwise. Same contract as HoldingEvent.inPrimary, exported for the report
// to sum outside the fold.
func (i HoldingIncome) InPrimary() Money {
	if i.PrimaryAmount != nil {
		return *i.PrimaryAmount
	}
	return i.Amount
}
