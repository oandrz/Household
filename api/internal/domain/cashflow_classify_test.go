package domain

import (
	"errors"
	"testing"
)

func TestClassifyCashflow(t *testing.T) {
	cases := []struct {
		name    string
		kind    TransactionKind
		toType  AccountType
		cadence Cadence
		want    CashflowRole
	}{
		{"income", TransactionIncome, "", "", CashflowIn},
		{"plain expense", TransactionExpense, "", "", CashflowOut},
		{"monthly bill payment", TransactionExpense, "", CadenceMonthly, CashflowOut},
		{"one-off bill payment", TransactionExpense, "", CadenceOneOff, CashflowOut},
		{"quarterly bill payment", TransactionExpense, "", CadenceQuarterly, CashflowSpreadOut},
		{"yearly bill payment", TransactionExpense, "", CadenceYearly, CashflowSpreadOut},
		{"transfer into a loan", TransactionTransfer, AccountLoan, "", CashflowLoanOut},
		{"transfer into a credit card", TransactionTransfer, AccountCreditCard, "", CashflowIgnore},
		{"transfer into cash", TransactionTransfer, AccountCash, "", CashflowIgnore},
		{"transfer into an investment", TransactionTransfer, AccountInvestment, "", CashflowIgnore},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ClassifyCashflow(c.kind, c.toType, c.cadence)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestClassifyCashflowRefusesUnknownInput(t *testing.T) {
	cases := []struct {
		name    string
		kind    TransactionKind
		toType  AccountType
		cadence Cadence
	}{
		{"unknown kind", "refund", "", ""},
		{"transfer with no destination type", TransactionTransfer, "", ""},
		{"transfer to an unknown type", TransactionTransfer, "boat", ""},
		{"expense with an unknown cadence", TransactionExpense, "", "weekly"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ClassifyCashflow(c.kind, c.toType, c.cadence)
			if !errors.Is(err, ErrUnknownCashflowInput) {
				t.Fatalf("got %v, want ErrUnknownCashflowInput", err)
			}
		})
	}
}

func TestSpreadForward(t *testing.T) {
	sum := func(parts []int64) (total int64) {
		for _, p := range parts {
			total += p
		}
		return total
	}

	t.Run("an even split gives equal parts", func(t *testing.T) {
		parts := SpreadForward(210000, 12)
		if len(parts) != 12 {
			t.Fatalf("got %d parts, want 12", len(parts))
		}
		for i, p := range parts {
			if p != 17500 {
				t.Errorf("part %d = %d, want 17500", i, p)
			}
		}
	})

	t.Run("the remainder goes to the first month", func(t *testing.T) {
		parts := SpreadForward(100, 12)
		if parts[0] != 12 {
			t.Errorf("first part = %d, want 12", parts[0])
		}
		for i, p := range parts[1:] {
			if p != 8 {
				t.Errorf("part %d = %d, want 8", i+1, p)
			}
		}
		if sum(parts) != 100 {
			t.Errorf("parts sum to %d, want 100", sum(parts))
		}
	})

	t.Run("a quarter splits into three", func(t *testing.T) {
		parts := SpreadForward(100, 3)
		want := []int64{34, 33, 33}
		for i := range want {
			if parts[i] != want[i] {
				t.Fatalf("got %v, want %v", parts, want)
			}
		}
	})

	t.Run("a refund spreads too", func(t *testing.T) {
		for i, p := range SpreadForward(-1200, 12) {
			if p != -100 {
				t.Errorf("part %d = %d, want -100", i, p)
			}
		}
	})

	t.Run("a negative remainder still goes to the first month", func(t *testing.T) {
		parts := SpreadForward(-100, 3)
		want := []int64{-34, -33, -33}
		for i := range want {
			if parts[i] != want[i] {
				t.Fatalf("got %v, want %v", parts, want)
			}
		}
	})
}

func TestSpreadMonths(t *testing.T) {
	cases := map[Cadence]int{
		CadenceYearly:    12,
		CadenceQuarterly: 3,
		CadenceMonthly:   1,
		CadenceOneOff:    1,
	}
	for cadence, want := range cases {
		if got := SpreadMonths(cadence); got != want {
			t.Errorf("SpreadMonths(%q) = %d, want %d", cadence, got, want)
		}
	}
}
