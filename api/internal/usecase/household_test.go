package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestSpacesForALimitedMemberWithOnlyCalendarReturnsExactlyFamily(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	m := domain.Membership{
		ID: "membership-kid", HouseholdID: f.householdID, UserID: "user-kid",
		Role: domain.RoleLimited, Capabilities: domain.Capabilities{domain.CapCalendar},
	}

	got, err := f.householdSvc.Spaces(ctx, f.householdID, m)
	if err != nil {
		t.Fatalf("Spaces: %v", err)
	}
	if len(got) != 1 || got[0].Key != "family" {
		t.Fatalf("spaces = %+v, want exactly the Family space", got)
	}
}

func TestCreateSpaceAssignsTheNextPositionAndIsNotBuiltin(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// The fixture seeds the three builtins at positions 1-3.
	got, err := f.householdSvc.CreateSpace(ctx, f.householdID, "Movie Night", domain.VisibilityEveryone)
	if err != nil {
		t.Fatalf("CreateSpace: %v", err)
	}
	if got.Position != 4 {
		t.Fatalf("Position = %d, want 4", got.Position)
	}
	if got.IsBuiltin {
		t.Fatal("IsBuiltin = true, want false")
	}
	if got.RequiredCapability != "" {
		t.Fatalf("RequiredCapability = %q, want empty", got.RequiredCapability)
	}
	if got.Key != "movie-night" {
		t.Fatalf("Key = %q, want %q", got.Key, "movie-night")
	}
	if got.ID == "" {
		t.Fatal("expected an assigned ID")
	}
}

func TestCreateSpaceAcceptsOnlyEveryoneAndParentsOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.householdSvc.CreateSpace(ctx, f.householdID, "Book Club", domain.VisibilityCustom); !errors.Is(err, usecase.ErrSpaceVisibilityNotSupported) {
		t.Fatalf("err = %v, want usecase.ErrSpaceVisibilityNotSupported", err)
	}
	spaces, err := f.spaces.List(ctx, f.householdID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, s := range spaces {
		if s.Key == "book-club" {
			t.Fatal("rejected custom space was written anyway")
		}
	}

	if _, err := f.householdSvc.CreateSpace(ctx, f.householdID, "Book Club", domain.VisibilityEveryone); err != nil {
		t.Fatalf("CreateSpace with VisibilityEveryone: %v", err)
	}
	if _, err := f.householdSvc.CreateSpace(ctx, f.householdID, "Vacation Planning", domain.VisibilityParentsOnly); err != nil {
		t.Fatalf("CreateSpace with VisibilityParentsOnly: %v", err)
	}
}

func TestCreateSpaceRejectsADuplicateNameWithinTheHousehold(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Colliding with the builtin Money space (case- and whitespace-insensitive,
	// since the key is derived by lowercasing and hyphenating): the check must
	// run against the full space list, not just custom, previously-created ones.
	if _, err := f.householdSvc.CreateSpace(ctx, f.householdID, " money ", domain.VisibilityEveryone); !errors.Is(err, usecase.ErrSpaceNameTaken) {
		t.Fatalf("err = %v, want usecase.ErrSpaceNameTaken (builtin collision)", err)
	}

	if _, err := f.householdSvc.CreateSpace(ctx, f.householdID, "Movie Night", domain.VisibilityEveryone); err != nil {
		t.Fatalf("first CreateSpace: %v", err)
	}
	if _, err := f.householdSvc.CreateSpace(ctx, f.householdID, "movie night", domain.VisibilityParentsOnly); !errors.Is(err, usecase.ErrSpaceNameTaken) {
		t.Fatalf("err = %v, want usecase.ErrSpaceNameTaken (custom collision)", err)
	}

	spaces, err := f.spaces.List(ctx, f.householdID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	count := 0
	for _, s := range spaces {
		if s.Key == "movie-night" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("movie-night spaces = %d, want exactly 1", count)
	}
}

func TestUpdateNormalisesThePrimaryCurrencyToUppercaseAndRejectsANonThreeLetterCurrency(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	current, err := f.householdSvc.Get(ctx, f.householdID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	lowercase := current
	lowercase.PrimaryCurrency = "usd"
	updated, err := f.householdSvc.Update(ctx, lowercase)
	if err != nil {
		t.Fatalf("Update with lowercase currency: %v", err)
	}
	if updated.PrimaryCurrency != "USD" {
		t.Fatalf("PrimaryCurrency = %q, want %q", updated.PrimaryCurrency, "USD")
	}
	fetched, err := f.householdSvc.Get(ctx, f.householdID)
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if fetched.PrimaryCurrency != "USD" {
		t.Fatalf("persisted PrimaryCurrency = %q, want %q", fetched.PrimaryCurrency, "USD")
	}

	tooShort := current
	tooShort.PrimaryCurrency = "US"
	if _, err := f.householdSvc.Update(ctx, tooShort); !errors.Is(err, domain.ErrInvalidMoney) {
		t.Fatalf("err = %v, want domain.ErrInvalidMoney (two letters)", err)
	}

	tooLong := current
	tooLong.PrimaryCurrency = "USDD"
	if _, err := f.householdSvc.Update(ctx, tooLong); !errors.Is(err, domain.ErrInvalidMoney) {
		t.Fatalf("err = %v, want domain.ErrInvalidMoney (four letters)", err)
	}

	// Neither rejected write must have clobbered the currency Update just set.
	fetched, err = f.householdSvc.Get(ctx, f.householdID)
	if err != nil {
		t.Fatalf("Get after rejected Update: %v", err)
	}
	if fetched.PrimaryCurrency != "USD" {
		t.Fatalf("PrimaryCurrency = %q, want %q unchanged after the rejected update", fetched.PrimaryCurrency, "USD")
	}
}

// TestUpdateNormalisesTheSecondaryCurrencyToUppercaseAndRejectsANonThreeLetterCurrency
// mirrors the primary-currency test above: SecondaryCurrency gets the
// identical normalisation and validation, not a pass-through, because it is
// persisted exactly like PrimaryCurrency and feeds the same
// FXRateProvider.Rate(from, to) lookup on the conversion path -- a malformed
// secondary code must fail here, at the edit, not later as a missing rate.
func TestUpdateNormalisesTheSecondaryCurrencyToUppercaseAndRejectsANonThreeLetterCurrency(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	current, err := f.householdSvc.Get(ctx, f.householdID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	lowercase := current
	lowercase.SecondaryCurrency = "eur"
	updated, err := f.householdSvc.Update(ctx, lowercase)
	if err != nil {
		t.Fatalf("Update with lowercase secondary currency: %v", err)
	}
	if updated.SecondaryCurrency != "EUR" {
		t.Fatalf("SecondaryCurrency = %q, want %q", updated.SecondaryCurrency, "EUR")
	}
	fetched, err := f.householdSvc.Get(ctx, f.householdID)
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if fetched.SecondaryCurrency != "EUR" {
		t.Fatalf("persisted SecondaryCurrency = %q, want %q", fetched.SecondaryCurrency, "EUR")
	}

	tooShort := current
	tooShort.SecondaryCurrency = "EU"
	if _, err := f.householdSvc.Update(ctx, tooShort); !errors.Is(err, domain.ErrInvalidMoney) {
		t.Fatalf("err = %v, want domain.ErrInvalidMoney (two letters)", err)
	}

	tooLong := current
	tooLong.SecondaryCurrency = "EURR"
	if _, err := f.householdSvc.Update(ctx, tooLong); !errors.Is(err, domain.ErrInvalidMoney) {
		t.Fatalf("err = %v, want domain.ErrInvalidMoney (four letters)", err)
	}

	// Neither rejected write must have clobbered the currency Update just set.
	fetched, err = f.householdSvc.Get(ctx, f.householdID)
	if err != nil {
		t.Fatalf("Get after rejected Update: %v", err)
	}
	if fetched.SecondaryCurrency != "EUR" {
		t.Fatalf("SecondaryCurrency = %q, want %q unchanged after the rejected update", fetched.SecondaryCurrency, "EUR")
	}
}

func TestUpdatePersistsEveryFieldItIsGiven(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	current, err := f.householdSvc.Get(ctx, f.householdID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	want := domain.Household{
		ID:                    current.ID,
		Name:                  "The Oentoro Household",
		FamilyName:            "Tan",
		PrimaryCurrency:       "USD",
		ShowSecondaryCurrency: !current.ShowSecondaryCurrency,
		SecondaryCurrency:     "EUR",
		FXRateMode:            "manual",
	}
	if want.Name == current.Name || want.FamilyName == current.FamilyName ||
		want.PrimaryCurrency == current.PrimaryCurrency || want.SecondaryCurrency == current.SecondaryCurrency ||
		want.FXRateMode == current.FXRateMode {
		t.Fatal("test setup bug: every field must differ from the seeded value to prove Update didn't drop it")
	}

	updated, err := f.householdSvc.Update(ctx, want)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated != want {
		t.Fatalf("Update returned %+v, want %+v", updated, want)
	}

	fetched, err := f.householdSvc.Get(ctx, f.householdID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched != want {
		t.Fatalf("Get after Update = %+v, want %+v", fetched, want)
	}
}

func TestUpdateNotificationsRoundTripsAllFourFlags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	current, err := f.householdSvc.Notifications(ctx, f.householdID)
	if err != nil {
		t.Fatalf("Notifications: %v", err)
	}
	if !current.BillReminders || !current.OverspendAlerts || !current.RetroReminder || !current.WeeklyDigest {
		t.Fatalf("seeded preferences = %+v, want all true", current)
	}

	want := usecase.NotificationPreferences{
		BillReminders: false, OverspendAlerts: true, RetroReminder: false, WeeklyDigest: true,
	}
	updated, err := f.householdSvc.UpdateNotifications(ctx, f.householdID, want)
	if err != nil {
		t.Fatalf("UpdateNotifications: %v", err)
	}
	if updated != want {
		t.Fatalf("UpdateNotifications returned %+v, want %+v", updated, want)
	}

	fetched, err := f.householdSvc.Notifications(ctx, f.householdID)
	if err != nil {
		t.Fatalf("Notifications after update: %v", err)
	}
	if fetched != want {
		t.Fatalf("Notifications after update = %+v, want %+v", fetched, want)
	}
}
