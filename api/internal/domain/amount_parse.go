package domain

import (
	"fmt"
	"strings"
)

// ParseAmount turns what a person typed -- "84.50", "1,250", "12", "0.5" --
// into minor units for a currency with the given number of decimal places.
// It is the one place a decimal string enters the money model, and it does
// it with integer string arithmetic: no float64 ever touches an amount
// (CLAUDE.md's money rule), so "0.1" in a 2-place currency is exactly 10,
// never 9.999… rounded.
//
// It refuses rather than rounds: more decimal places than the currency has
// is an error, because "84.505" means the person and the product disagree
// about what the currency can represent, and silently dropping the 5 would
// hide that. A sign is refused too; the kind of transaction carries it.
func ParseAmount(text string, minorUnits int) (int64, error) {
	s := strings.ReplaceAll(strings.TrimSpace(text), ",", "")
	if s == "" || minorUnits < 0 || minorUnits > 6 {
		return 0, fmt.Errorf("%w: %q", ErrInvalidMoney, text)
	}
	whole, frac, hasPoint := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	if hasPoint && frac == "" {
		return 0, fmt.Errorf("%w: %q", ErrInvalidMoney, text)
	}
	if len(frac) > minorUnits {
		return 0, fmt.Errorf("%w: %q has more decimal places than the currency allows (%d)", ErrInvalidMoney, text, minorUnits)
	}
	frac += strings.Repeat("0", minorUnits-len(frac))
	digits := whole + frac
	if len(digits) > 18 {
		return 0, fmt.Errorf("%w: %q is too large", ErrInvalidMoney, text)
	}
	var n int64
	for _, ch := range digits {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("%w: %q", ErrInvalidMoney, text)
		}
		n = n*10 + int64(ch-'0')
	}
	if n <= 0 {
		return 0, fmt.Errorf("%w: %q must be greater than zero", ErrInvalidMoney, text)
	}
	return n, nil
}

// MinorUnitsFor is the decimal-place count of an active currency, or 2 for
// a code the table does not know -- the table is the same allowlist
// ParseCurrency enforces, so an unknown code cannot reach a stored account
// and the fallback only ever serves a display path.
func MinorUnitsFor(code string) int {
	if c, ok := byCode[strings.ToUpper(code)]; ok {
		return c.MinorUnits
	}
	return 2
}

// FormatAmount is ParseAmount's inverse for replies: minor units to a
// decimal string with exactly the currency's places. Money.String hard-codes
// two places and the wire never formats at all, so this exists for the one
// channel that speaks to a person in text.
func FormatAmount(minor int64, minorUnits int) string {
	neg := minor < 0
	if neg {
		minor = -minor
	}
	s := fmt.Sprintf("%d", minor)
	if minorUnits > 0 {
		for len(s) <= minorUnits {
			s = "0" + s
		}
		s = s[:len(s)-minorUnits] + "." + s[len(s)-minorUnits:]
	}
	if neg {
		s = "-" + s
	}
	return s
}
