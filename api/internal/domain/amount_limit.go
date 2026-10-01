package domain

import "fmt"

// MaxAmountMinor is the largest single amount anyone may type into Hearth, in
// minor units: 100 trillion, which is S$1 trillion or Rp 1 trillion. Every
// write path that accepts an amount from a person refuses more than this, in
// either direction, through CheckAmountWithinLimit.
//
// Why there is a ceiling at all: stored amounts are added up on every read
// (an account's balance, net worth, a goal's total), and the answer has to
// fit in an int64. With no ceiling, one expense of 9,223,372,036,854,775,807
// was accepted and every money page of that household then failed, including
// the page the row would have been deleted from.
//
// Why this number. It has to satisfy four things at once:
//
//   - Real figures stay valid. A house at Rp 10,000,000,000 is 1e12 minor
//     units, a hundredth of the ceiling.
//   - One amount at the ceiling survives currency conversion. The largest
//     rate in the FX table is SGD to IDR at 12,410, and 1e14 x 12,410 is
//     1.2e18, under the int64 limit of 9.2e18. A ceiling of 1e15 would not.
//     A rate above 92,233 breaks this, so check it when a rate source is added.
//   - A sum cannot overflow by accident. It takes more than 92,233 rows that
//     are each at the ceiling to overflow one sum.
//   - The browser can hold it exactly. JavaScript numbers are exact only up to
//     2^53 (about 9e15); past that the form would send a different figure
//     from the one typed.
//
// A per-amount ceiling is NOT a guarantee on its own, and don't describe it
// as one: nothing limits how many rows a household has, so a script that
// writes 92,234 rows at the ceiling into one account still overflows that
// account's balance. The ceiling turns "one typo" into "tens of thousands of
// deliberate absurd entries". If a sum does overflow, the read fails loudly
// (ErrAmountOverflow, or Postgres refusing the cast) rather than showing a
// clamped or partial total, because a wrong balance is worse than no balance.
//
// Accepted trade-off: in a currency worth very little per unit the ceiling is
// a smaller sum of real money. It is about US$1 million in Iranian rial (IRR)
// at the market rate, so a house there may not fit. Raising the ceiling for
// everyone to fit that case gives up the conversion headroom above.
//
// Don't raise this without redoing the arithmetic: the test beside this file
// pins the conversion, browser and row-count promises.
const MaxAmountMinor int64 = 100_000_000_000_000

// CheckAmountWithinLimit refuses an amount further from zero than
// MaxAmountMinor with ErrAmountTooLarge. Negative amounts get the same limit
// as positive ones: an opening balance or a goal withdrawal can be negative,
// and a huge negative figure breaks a sum just as a huge positive one does.
//
// It says nothing about sign or zero -- each field keeps its own rule for
// those (ErrTransactionAmountNotPositive, ErrNegativeAmount and the rest), so
// call this beside that check, not instead of it.
//
// Call it only on a figure a person supplied. A figure this code computed --
// a balance, a total, a rollover -- is a sum of many amounts and may
// legitimately be larger.
//
// One computed figure IS held to MaxAmountMinor, by its own rule: a holding's
// worth, quantity times unit price (CheckHoldingValueWithinLimit). It is a
// product of two typed figures rather than a sum of many rows, so a single
// typo can take it past an int64.
func CheckAmountWithinLimit(minor int64) error {
	// Two comparisons, not one on the absolute value: negating math.MinInt64
	// returns itself, so an "abs(minor) > max" check would let it through.
	if minor > MaxAmountMinor || minor < -MaxAmountMinor {
		return fmt.Errorf("%w: got %d, limit is %d", ErrAmountTooLarge, minor, MaxAmountMinor)
	}
	return nil
}
