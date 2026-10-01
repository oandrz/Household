package usecase_test

import (
	"errors"
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
		b, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "sgd", "Asia/Singapore")
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

	t.Run("carries the time zone into the household it renders", func(t *testing.T) {
		b, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "SGD", "America/Sao_Paulo")
		if err != nil {
			t.Fatalf("NewSignupBlueprint: %v", err)
		}
		if b.Timezone != "America/Sao_Paulo" || b.Household().Timezone != "America/Sao_Paulo" {
			t.Fatalf("Timezone = %q, Household().Timezone = %q, want America/Sao_Paulo in both",
				b.Timezone, b.Household().Timezone)
		}
	})

	// Refused, not replaced with UTC: a household quietly on UTC is the
	// defect the stored zone exists to remove.
	t.Run("refuses a time zone it cannot load", func(t *testing.T) {
		for _, zone := range []string{"", "Local", "Mars/Olympus_Mons"} {
			if _, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "SGD", zone); !errors.Is(err, domain.ErrInvalidTimezone) {
				t.Fatalf("NewSignupBlueprint(zone %q) error = %v, want ErrInvalidTimezone", zone, err)
			}
		}
	})

	t.Run("family name mirrors the household name", func(t *testing.T) {
		b, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "SGD", "Asia/Singapore")
		if err != nil {
			t.Fatalf("NewSignupBlueprint: %v", err)
		}
		if b.FamilyName != "Ade & Kris" {
			t.Fatalf("FamilyName = %q, want the household name", b.FamilyName)
		}
	})

	t.Run("the owner holds every capability", func(t *testing.T) {
		b, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "SGD", "Asia/Singapore")
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
			if _, err := usecase.NewSignupBlueprint(name, "Ade", "SGD", "Asia/Singapore"); err != usecase.ErrHouseholdNameRequired {
				t.Fatalf("NewSignupBlueprint(%q) error = %v, want ErrHouseholdNameRequired", name, err)
			}
		}
	})

	t.Run("a blank display name is refused", func(t *testing.T) {
		if _, err := usecase.NewSignupBlueprint("Ade & Kris", "  ", "SGD", "Asia/Singapore"); err != usecase.ErrDisplayNameRequired {
			t.Fatalf("error = %v, want ErrDisplayNameRequired", err)
		}
	})

	t.Run("an unknown currency is refused", func(t *testing.T) {
		if _, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "ZZZ", "Asia/Singapore"); err == nil {
			t.Fatal("NewSignupBlueprint accepted ZZZ")
		}
	})

	t.Run("names are trimmed", func(t *testing.T) {
		b, err := usecase.NewSignupBlueprint("  Ade & Kris  ", "  Ade  ", "SGD", "Asia/Singapore")
		if err != nil {
			t.Fatalf("NewSignupBlueprint: %v", err)
		}
		if b.Name != "Ade & Kris" || b.OwnerDisplayName != "Ade" {
			t.Fatalf("got Name=%q OwnerDisplayName=%q, want both trimmed", b.Name, b.OwnerDisplayName)
		}
	})
}
