// The one place minor units become a string. One helper rather than
// per-component formatting, because four components formatting independently
// will disagree about thousands separators -- and this project has a rule
// about fixing the class rather than the instance.
//
// The backend sends minor units plus an ISO 4217 code and never a formatted
// string: domain.Money.String() hard-codes two decimals and puts the code in
// front, which is right for a log line and wrong for a screen.

// NO_DECIMAL_CURRENCIES are the codes this app renders whole even though the
// backend's allowlist treats them as two-minor-unit currencies. IDR is here
// because the design draws Rp 85,400,000 and nobody quotes rupiah cents. This
// affects display only -- the stored value keeps every minor unit.
// Exported because AccountModal's toMinorUnits reads the same set: a currency
// this file renders whole must be parsed whole too, or "85400000" typed into
// the form and "Rp 85,400,000" shown back would disagree by two decimal places.
export const NO_DECIMAL_CURRENCIES = new Set(["IDR", "VND"]);

// U+2212 MINUS SIGN, not a hyphen: it aligns with digits at the same width,
// which a hyphen does not, and every negative figure in this app is in a
// column of numbers.
const MINUS = "−";

export function formatMoney(
  amountMinor: number,
  currency: string,
  symbol?: string,
): string {
  const whole = NO_DECIMAL_CURRENCIES.has(currency);
  const digits = whole ? 0 : 2;
  const magnitude = Math.abs(amountMinor) / 100;

  const formatted = new Intl.NumberFormat("en-SG", {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(magnitude);

  // A glyph symbol butts against the digits (S$8,240.55); a symbol spelled
  // with letters reads as one token if it does the same (Rp85,400,000 could
  // be misread as a currency code fused to a house number), so it gets the
  // same space a bare code needs (BRL 1,000.00). Ending-in-a-letter is what
  // tells the two apart -- "S$" ends in the glyph, "Rp" ends in a letter.
  const prefix = symbol
    ? /[A-Za-z]$/.test(symbol)
      ? `${symbol} `
      : symbol
    : `${currency} `;
  const sign = amountMinor < 0 ? MINUS : "";
  return `${sign}${prefix}${formatted}`;
}

// Minor units are ALWAYS hundredths, for every currency this app accepts:
// domain.ParseSelectableCurrency admits only two-minor-unit codes, so IDR is
// stored in hundredths of a rupiah exactly as SGD is stored in cents. The
// no-decimal treatment above is a display and input convention -- how many
// digits a person may type -- and never a change to the scale. Conflating the
// two would store Rp 85,400,000 as 85400000 and render it back as Rp 854,000.
const MINOR_UNITS_PER_MAJOR = 100;

// toMinorUnits turns what someone typed ("8240.55") into what the API stores
// (824055). It lives here, beside the formatter and NO_DECIMAL_CURRENCIES,
// because one module has to own the scale -- a parser and a formatter that
// disagree produce a figure that changes every time it round-trips.
//
// Splitting on the decimal point rather than multiplying a float by 100 is the
// frontend half of the rule that no floating-point value enters a monetary
// path. Returns null for anything that is not a number, which the caller shows
// as a field error rather than posting a NaN.
export function toMinorUnits(input: string, currency: string): number | null {
  const trimmed = input.trim().replace(/,/g, "");
  if (!/^-?\d+(\.\d+)?$/.test(trimmed)) return null;

  const negative = trimmed.startsWith("-");
  const [whole, fraction = ""] = trimmed.replace("-", "").split(".");
  const allowedDecimals = NO_DECIMAL_CURRENCIES.has(currency) ? 0 : 2;

  // More precision than the field offers is a typo, not a rounding problem.
  // Refusing is honest; silently truncating "8240.555" to 8240.55 would change
  // a figure the person is looking at.
  if (fraction.length > allowedDecimals) return null;

  const cents = fraction.padEnd(2, "0");
  const minor = Number(whole) * MINOR_UNITS_PER_MAJOR + Number(cents);
  return negative ? -minor : minor;
}

// The inverse of toMinorUnits, needed to prefill an editable field from a
// stored figure -- AccountModal's Balance and TransactionModal's Amount /
// Amount received all do this on open. Kept here, beside its inverse and
// NO_DECIMAL_CURRENCIES, rather than duplicated per component: two components
// each re-deriving "how many decimal places does this currency show" from
// their own copy of that rule are two chances for the rule to drift, which is
// exactly the failure toMinorUnits's own module comment already warns about.
// Not the same function as formatMoney (which adds thousands separators, a
// currency symbol and a typographic minus sign, none of which belong in an
// editable input) -- but it agrees with both on the one fact that matters:
// minor units are always hundredths, and NO_DECIMAL_CURRENCIES is a display
// convention, never a change to that scale.
export function minorUnitsToInputValue(amountMinor: number, currency: string): string {
  const negative = amountMinor < 0;
  const magnitude = Math.abs(amountMinor);
  const cents = magnitude % 100;
  // Subtracting the exact remainder before dividing keeps this an exact
  // integer division -- (magnitude - cents) is always a multiple of 100 -- so
  // no floating-point rounding enters a figure the person is about to see and
  // edit.
  const whole = (magnitude - cents) / 100;
  const decimals = NO_DECIMAL_CURRENCIES.has(currency) ? 0 : 2;
  const value = decimals === 0 ? String(whole) : `${whole}.${String(cents).padStart(2, "0")}`;
  return negative ? `-${value}` : value;
}

// Chooses the message for a monetary field toMinorUnits refused: "not a
// number" when that is what actually went wrong, or the currency-specific
// message when the figure is a real number but has more decimals than the
// currency allows -- typically a currency switching to a no-decimal one (IDR,
// VND) without the amount itself being touched. Originally lived only inside
// AccountModal's Balance validation, whose own comment explains why the
// currency-specific message matters: restating the figure back ("Enter an
// amount, like 8240.55") describes exactly what is already in the field
// rather than what is wrong with it. Moved here once TransactionModal needed
// the same distinction for Amount and Amount received -- two fields making
// this decision independently is the same "components disagree about 52.30"
// risk toMinorUnits's own comment warns about, just for the error message
// instead of the parsed value.
export function describeAmountError(input: string, currency: string, example: string): string {
  const hasADecimalPoint = /^-?\d+\.\d+$/.test(input.trim().replace(/,/g, ""));
  if (NO_DECIMAL_CURRENCIES.has(currency) && hasADecimalPoint) {
    return `${currency} doesn't use cents. Remove the decimal point.`;
  }
  return `Enter an amount, like ${example}.`;
}
