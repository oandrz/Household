package domain

import (
	"errors"
	"fmt"
	"time"
)

// CheckHoldingValueWithinLimit refuses a holding that would be worth more
// than MaxAmountMinor, with ErrHoldingValueTooLarge. "Worth" is a quantity
// times a unit price, and the check covers every pair a read can multiply:
// peak, the most the holding has ever held (Position.PeakHeld), against every
// recorded price, native and household-currency.
//
// Why it exists: the amount ceiling checks each figure a person types, and a
// quantity and a unit price can both pass it while their product does not fit
// in an int64. 100,000 units and a price of 1e14 minor units were both
// accepted, and every portfolio read of that household then failed. So every
// write that changes a holding's events or prices runs this check on the
// holding as it WOULD be, and nothing is stored if it refuses.
//
// Why the peak and not what is held now: the period report prices the
// quantity held at the end of each period, which may be long ago and far more
// than today's.
//
// Why every price and not the latest: each period of the report uses the
// latest price inside that period, so an old price is still multiplied.
//
// Why MaxAmountMinor and not "fits in an int64": the report goes on to add a
// market value to a cost, a realised gain and income. Held to the ceiling, a
// holding's worth has the same headroom in those sums, in currency
// conversion and in the browser as any amount a person typed.
//
// Accepted trade-off: this refuses some pairs no read ever multiplies. Ten
// million units bought and sold in January, then a price in June that those
// ten million could not carry, is refused although only June's smaller
// quantity is ever priced at it. Checking only the pairs the report really
// uses would mean repeating the report's choice of period and price here, and
// the two would then have to be kept in step.
//
// A holding already past the limit refuses every write except the repair:
// deleting the purchase that made it so, or re-entering that day's price
// lower.
func CheckHoldingValueWithinLimit(peak Quantity, prices []Valuation) error {
	for _, price := range prices {
		if err := checkWorthAtPrice(peak, price.UnitPrice, price.AsOf); err != nil {
			return err
		}
		if price.PrimaryUnitPrice != nil {
			if err := checkWorthAtPrice(peak, *price.PrimaryUnitPrice, price.AsOf); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkWorthAtPrice goes through Quantity.Value, the one multiply in this
// package, so this rule and the reads it protects cannot round differently.
func checkWorthAtPrice(peak Quantity, unitPrice Money, asOf time.Time) error {
	worth, err := peak.Value(unitPrice)
	if errors.Is(err, ErrAmountOverflow) {
		// A product too large for an int64 is the largest case of "too
		// large", and it is this request's mistake. Don't wrap err here:
		// ErrAmountOverflow answers 500, and a wrapped one would still match
		// it.
		return fmt.Errorf("%w: %s units at %d %s (price of %s) does not fit in an int64",
			ErrHoldingValueTooLarge, FormatQuantity(peak), unitPrice.Amount, unitPrice.Currency, asOf.Format(time.DateOnly))
	}
	if err != nil {
		return err
	}
	// One comparison is enough: a quantity and a unit price are never
	// negative (NewQuantity and Quantity.Value refuse both), so neither is
	// their product.
	if worth.Amount > MaxAmountMinor {
		return fmt.Errorf("%w: %s units at %d %s (price of %s) is %d, limit is %d",
			ErrHoldingValueTooLarge, FormatQuantity(peak), unitPrice.Amount, unitPrice.Currency,
			asOf.Format(time.DateOnly), worth.Amount, MaxAmountMinor)
	}
	return nil
}
