package domain

import "fmt"

// Money is an exact amount in an ISO 4217 currency, held in minor units.
// Floating point never appears in a monetary path.
type Money struct {
	Amount   int64
	Currency string
}

func NewMoney(amount int64, currency string) (Money, error) {
	if len(currency) != 3 {
		return Money{}, fmt.Errorf("currency must be three letters, got %q", currency)
	}
	for _, r := range currency {
		if r < 'A' || r > 'Z' {
			return Money{}, fmt.Errorf("currency must be uppercase, got %q", currency)
		}
	}
	return Money{Amount: amount, Currency: currency}, nil
}

func (m Money) Add(other Money) (Money, error) {
	if m.Currency == "" || other.Currency == "" {
		return Money{}, fmt.Errorf("%w: a Money zero value has no currency", ErrInvalidMoney)
	}
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("%w: %s and %s", ErrCurrencyMismatch, m.Currency, other.Currency)
	}
	sum := m.Amount + other.Amount
	// Overflow in signed addition shows up as a sum whose sign disagrees
	// with two operands that agree with each other: two positives summing
	// to a non-positive result, or two negatives summing to a non-negative
	// one. Refuse rather than silently wrapping.
	if (m.Amount > 0 && other.Amount > 0 && sum <= 0) ||
		(m.Amount < 0 && other.Amount < 0 && sum >= 0) {
		return Money{}, fmt.Errorf("%w: %d + %d", ErrAmountOverflow, m.Amount, other.Amount)
	}
	return Money{Amount: sum, Currency: m.Currency}, nil
}

func (m Money) String() string {
	sign := ""
	// The magnitude is computed without ever negating m.Amount: negating
	// math.MinInt64 in two's complement returns itself, which would leave
	// amount negative and corrupt the output. Instead, when amount is
	// negative, negate (amount+1) -- which is always representable in an
	// int64 because amount can be at most MinInt64 -- and add 1 back in
	// uint64, which has room for the one extra step that int64 does not.
	var magnitude uint64
	if m.Amount < 0 {
		sign = "-"
		magnitude = uint64(-(m.Amount + 1)) + 1
	} else {
		magnitude = uint64(m.Amount)
	}
	return fmt.Sprintf("%s%s %d.%02d", sign, m.Currency, magnitude/100, magnitude%100)
}
