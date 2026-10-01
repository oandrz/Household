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

### Calendar

**Household day**:
The calendar date it is right now for a household, worked out from the one
time zone the household keeps; every "today", "this month" and "this year" in
Hearth means this, for every member, wherever they are.
_Avoid_: server date, UTC today, local date, the browser's date

**Fact** and **plan**:
A fact is something that already happened and was recorded (a purchase, a
price, a payment, a transaction, an opening balance); it may not be dated
after the household day. A plan is something intended (a bill's next due
date, a goal's target month, a budget month); it may be.
_Avoid_: past entry, future entry
