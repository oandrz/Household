package domain_test

import (
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestNewMoneyRejectsAnUnknownCurrency(t *testing.T) {
	if _, err := domain.NewMoney(100, "sgd"); err == nil {
		t.Fatal("expected lowercase currency to be rejected")
	}
	if _, err := domain.NewMoney(100, "SG"); err == nil {
		t.Fatal("expected a two-letter currency to be rejected")
	}
}

func TestAddRefusesToMixCurrencies(t *testing.T) {
	sgd, _ := domain.NewMoney(1000, "SGD")
	idr, _ := domain.NewMoney(1000, "IDR")

	if _, err := sgd.Add(idr); !errors.Is(err, domain.ErrCurrencyMismatch) {
		t.Fatalf("err = %v, want ErrCurrencyMismatch", err)
	}
}

func TestAddSumsMinorUnits(t *testing.T) {
	a, _ := domain.NewMoney(824055, "SGD")
	b, _ := domain.NewMoney(100, "SGD")

	sum, err := a.Add(b)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if sum.Amount != 824155 {
		t.Fatalf("Amount = %d, want 824155", sum.Amount)
	}
}

func TestStringFormatsMinorUnits(t *testing.T) {
	m, _ := domain.NewMoney(824055, "SGD")
	if got := m.String(); got != "SGD 8240.55" {
		t.Fatalf("String() = %q, want %q", got, "SGD 8240.55")
	}
}
