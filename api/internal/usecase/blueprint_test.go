package usecase_test

import (
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestDefaultNotificationPreferencesAreAllOn(t *testing.T) {
	got := usecase.DefaultNotificationPreferences()
	if !got.BillReminders || !got.OverspendAlerts || !got.RetroReminder || !got.WeeklyDigest {
		t.Fatalf("got %+v, want every flag true", got)
	}
}

func TestBlueprintForSignupValidates(t *testing.T) {
	t.Run("normalises the currency and mirrors it into secondary", func(t *testing.T) {
		b, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "sgd")
		if err != nil {
			t.Fatalf("NewSignupBlueprint: %v", err)
		}
		if b.PrimaryCurrency != "SGD" {
			t.Fatalf("PrimaryCurrency = %q, want SGD", b.PrimaryCurrency)
		}
		// Equal to primary, not the column's IDR default: CurrencyPanel renders
		// its toggle label straight from the column, so a household that never
		// chose IDR must not find "Show IDR equivalents" in Settings.
		if b.SecondaryCurrency != "SGD" {
			t.Fatalf("SecondaryCurrency = %q, want SGD", b.SecondaryCurrency)
		}
		if b.ShowSecondaryCurrency {
			t.Fatal("ShowSecondaryCurrency = true, want false for a self-serve household")
		}
	})

	t.Run("family name mirrors the household name", func(t *testing.T) {
		b, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "SGD")
		if err != nil {
			t.Fatalf("NewSignupBlueprint: %v", err)
		}
		if b.FamilyName != "Ade & Kris" {
			t.Fatalf("FamilyName = %q, want the household name", b.FamilyName)
		}
	})

	t.Run("the owner holds every capability", func(t *testing.T) {
		b, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "SGD")
		if err != nil {
			t.Fatalf("NewSignupBlueprint: %v", err)
		}
		if b.OwnerRole != domain.RoleOwner {
			t.Fatalf("OwnerRole = %q, want owner", b.OwnerRole)
		}
		for _, want := range domain.AllCapabilities() {
			if !b.OwnerCapabilities.Has(want) {
				t.Fatalf("OwnerCapabilities missing %q -- the memberships CHECK would reject this row", want)
			}
		}
	})

	t.Run("a blank household name is refused", func(t *testing.T) {
		for _, name := range []string{"", "   ", "\t\n"} {
			if _, err := usecase.NewSignupBlueprint(name, "Ade", "SGD"); err != usecase.ErrHouseholdNameRequired {
				t.Fatalf("NewSignupBlueprint(%q) error = %v, want ErrHouseholdNameRequired", name, err)
			}
		}
	})

	t.Run("a blank display name is refused", func(t *testing.T) {
		if _, err := usecase.NewSignupBlueprint("Ade & Kris", "  ", "SGD"); err != usecase.ErrDisplayNameRequired {
			t.Fatalf("error = %v, want ErrDisplayNameRequired", err)
		}
	})

	t.Run("an unknown currency is refused", func(t *testing.T) {
		if _, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "ZZZ"); err == nil {
			t.Fatal("NewSignupBlueprint accepted ZZZ")
		}
	})

	t.Run("names are trimmed", func(t *testing.T) {
		b, err := usecase.NewSignupBlueprint("  Ade & Kris  ", "  Ade  ", "SGD")
		if err != nil {
			t.Fatalf("NewSignupBlueprint: %v", err)
		}
		if b.Name != "Ade & Kris" || b.OwnerDisplayName != "Ade" {
			t.Fatalf("got Name=%q OwnerDisplayName=%q, want both trimmed", b.Name, b.OwnerDisplayName)
		}
	})
}
