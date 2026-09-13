package postgres

import (
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// TestReadingAnUnknownEnumValueIsRefused pins the fail-closed rule for enum
// columns: a value no writer in this codebase could have stored is refused on
// read, not carried up as a domain value that a guard or a switch would then
// misread. No database is involved on purpose -- the CHECK constraints are
// exactly what stop a real table from producing these rows, so the conversion
// itself is the unit under test.
func TestReadingAnUnknownEnumValueIsRefused(t *testing.T) {
	cases := []struct {
		name string
		read func() error
		want error
	}{
		{"category kind", func() error { _, err := toCategory(sqlcgen.Category{Kind: "refund"}); return err }, domain.ErrUnknownCategoryKind},
		{"account type", func() error { _, err := toAccount(sqlcgen.Account{Type: "crypto_wallet"}); return err }, domain.ErrUnknownAccountType},
		{"transaction kind", func() error { _, err := toTransaction(sqlcgen.Transaction{Kind: "refund"}); return err }, domain.ErrUnknownTransactionKind},
		{"membership role", func() error { _, err := toRole("admin"); return err }, domain.ErrUnknownRole},
		{"membership capability", func() error { _, err := toCapabilities([]string{"money", "banking"}); return err }, domain.ErrUnknownCapability},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.read(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestReadingAKnownEnumValueSucceeds is the other half: parsing on read must
// not refuse the values the product actually writes.
func TestReadingAKnownEnumValueSucceeds(t *testing.T) {
	if c, err := toCategory(sqlcgen.Category{Kind: "expense"}); err != nil || string(c.Kind) != "expense" {
		t.Fatalf("toCategory(expense) = %q, %v", c.Kind, err)
	}
	if tx, err := toTransaction(sqlcgen.Transaction{Kind: "transfer"}); err != nil || string(tx.Kind) != "transfer" {
		t.Fatalf("toTransaction(transfer) = %q, %v", tx.Kind, err)
	}
	m, err := toMembership(sqlcgen.Membership{}.ID, sqlcgen.Membership{}.HouseholdID, sqlcgen.Membership{}.UserID,
		"limited", []string{"money", "chores"})
	if err != nil || string(m.Role) != "limited" || len(m.Capabilities) != 2 {
		t.Fatalf("toMembership(limited, money+chores) = %+v, %v", m, err)
	}
}
