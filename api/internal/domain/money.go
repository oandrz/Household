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
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("%w: %s and %s", ErrCurrencyMismatch, m.Currency, other.Currency)
	}
	return Money{Amount: m.Amount + other.Amount, Currency: m.Currency}, nil
}

func (m Money) String() string {
	sign := ""
	amount := m.Amount
	if amount < 0 {
		sign, amount = "-", -amount
	}
	return fmt.Sprintf("%s%s %d.%02d", sign, m.Currency, amount/100, amount%100)
}
