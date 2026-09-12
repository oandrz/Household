package domain

import (
	"fmt"
	"time"
)

// IncomeKind is which direction a cash movement that is not a trade went.
//
// A dividend, a coupon and a distribution are all IncomeReceived: they add to
// what the holding earned without changing what is held or what it cost. A
// custody, platform or storage charge is IncomeFee, and reduces it.
//
// Both are stored as POSITIVE amounts and the report subtracts fees when it
// sums a period. Storing a fee as negative money would be the only negative
// amount in this package, and one exception is how a rule stops being a rule.
//
// Brokerage on a trade is NOT a fee row: an acquisition records the whole
// amount that left the bank and a disposal the whole amount that arrived, so
// commission is already inside the cost basis and the proceeds. Only a charge
// that buys nothing needs a row of its own.
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

// HoldingIncome is one payment received from a holding, or one charge made
// against it, on a day.
//
// It is deliberately NOT a HoldingEventKind. Income changes neither the
// quantity held nor what that quantity cost, so folding it through the
// average-cost pool would make every disposal after a dividend realise the
// wrong number. It arrives with the period report instead, on its own footing
// -- which is exactly what holding.go's HoldingEventKind comment promised.
//
// PrimaryAmount carries the household's own currency when the holding is not
// already in it, under the same contract HoldingEvent.PrimaryAmount does: what
// the owner knows is the amount that reached their bank, not the rate behind
// it.
//
// ReceivedOn is the day the money moved, not the day it was typed. The report
// buckets by it.
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
	if i.Amount.Amount < 0 {
		return fmt.Errorf("%w: an income amount cannot be negative, got %d", ErrInvalidMoney, i.Amount.Amount)
	}
	// Zero is refused for the reason a zero-quantity event is: nothing changed
	// hands, so there is nothing to record, and a row of zero only dilutes the
	// count of what the household actually earned.
	if i.Amount.Amount == 0 {
		return ErrHoldingIncomeAmountNotPositive
	}
	return validatePrimaryAmount(i.PrimaryAmount, holdingCurrency, primaryCurrency)
}

// InPrimary is the amount in the household's own currency: the separately
// recorded one when the holding is in some other currency, the native amount
// when it is already in the household's. Same contract as
// HoldingEvent.inPrimary, exported because the report sums these outside the
// fold.
func (i HoldingIncome) InPrimary() Money {
	if i.PrimaryAmount != nil {
		return *i.PrimaryAmount
	}
	return i.Amount
}
