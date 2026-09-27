# Hearth

A shared dashboard for one household: its money, its bills and goals, and the
agreements and plans the people in it make together.

## Language

### Money

**Primary currency**:
The one currency a household reads its totals in; every figure that adds
amounts from more than one currency is shown in it.
_Avoid_: base currency, home currency, default currency

**Rate**:
How many units of one currency one unit of another is worth, held as an exact
fraction so that both directions of a pair (SGD→IDR and IDR→SGD) are exact.
_Avoid_: exchange rate as a decimal, FX multiplier

**No rate**:
The state of an amount whose currency has no rate to the primary currency; the
amount is left out of the total and listed as excluded, never counted as zero.
_Avoid_: unconvertible, missing FX, zero-rated
