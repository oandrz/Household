package domain

import "fmt"

// Money is an exact amount in an ISO 4217 currency, held in minor units.
// Floating point never appears in a monetary path.
type Money struct {
	Amount   int64
	Currency string
}

// NewMoney validates the currency through ParseCurrency, the single reference
// for what a valid code is. Don't narrow this to a structural check like
// "three uppercase letters" -- sign-up lets a stranger choose the code, and a
// well-formed string like "ZZZ" is not a real ISO 4217 currency.
func NewMoney(amount int64, currency string) (Money, error) {
	code, err := ParseCurrency(currency)
	if err != nil {
		return Money{}, err
	}
	return Money{Amount: amount, Currency: code}, nil
}

func (m Money) Add(other Money) (Money, error) {
	if m.Currency == "" || other.Currency == "" {
		return Money{}, fmt.Errorf("%w: a Money zero value has no currency", ErrMoneyWithoutCurrency)
	}
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("%w: %s and %s", ErrCurrencyMismatch, m.Currency, other.Currency)
	}
	sum := m.Amount + other.Amount
	// Overflow shows up as a sum whose sign disagrees with two operands that
	// agree with each other: two positives summing non-positive, or two
	// negatives summing non-negative. Refuse rather than wrap silently.
	if (m.Amount > 0 && other.Amount > 0 && sum <= 0) ||
		(m.Amount < 0 && other.Amount < 0 && sum >= 0) {
		return Money{}, fmt.Errorf("%w: %d + %d", ErrAmountOverflow, m.Amount, other.Amount)
	}
	return Money{Amount: sum, Currency: m.Currency}, nil
}

func (m Money) String() string {
	sign := ""
	// Computed without negating m.Amount: negating math.MinInt64 in two's
	// complement returns itself, corrupting the output. Instead, when
	// negative, negate (amount+1) -- always representable, since amount+1 is at
	// least MinInt64+1 -- and add 1 back in uint64, which has room for 2^63.
	var magnitude uint64
	if m.Amount < 0 {
		sign = "-"
		magnitude = uint64(-(m.Amount + 1)) + 1
	} else {
		magnitude = uint64(m.Amount)
	}
	return fmt.Sprintf("%s%s %d.%02d", sign, m.Currency, magnitude/100, magnitude%100)
}
