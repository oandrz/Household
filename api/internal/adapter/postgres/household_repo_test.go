package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestHouseholdRepoRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)

	// Values differ from the households table's own column defaults
	// (SGD/IDR/true) and from what Update sets below, so this proves Create
	// and Update each forward every field rather than coincidentally
	// matching a default or an already-set value. CreateHouseholdParams is a
	// keyed struct: a field dropped from the literal would silently
	// zero-value and hide behind the column default. FXRateMode is asserted
	// separately as "auto" because Create ignores it by design (see
	// HouseholdRepository.Create). The time zone is not UTC for the same
	// reason the currencies are not SGD and IDR.
	created, err := households.Create(ctx, domain.Household{
		Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "GBP", SecondaryCurrency: "JPY", ShowSecondaryCurrency: false,
		Timezone: "Pacific/Auckland",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create did not return an id — uuidToString must have failed")
	}
	if created.PrimaryCurrency != "GBP" || created.SecondaryCurrency != "JPY" ||
		created.ShowSecondaryCurrency || created.FXRateMode != "auto" ||
		created.Timezone != "Pacific/Auckland" {
		t.Fatalf("Create did not forward the caller's household: %+v", created)
	}
	// The database stamps the creation instant; the retro floor reads it.
	if created.CreatedAt.IsZero() {
		t.Fatal("Create did not return the household's creation instant")
	}

	// Change every field, including Name and SecondaryCurrency. Don't let
	// the query narrow back to a subset of columns -- this assertion is what
	// makes a future narrowing fail loudly, instead of returning a nil error
	// over a partial write.
	want := domain.Household{
		ID: created.ID, Name: "The Oentoro Household", FamilyName: "Tan",
		PrimaryCurrency: "USD", ShowSecondaryCurrency: true,
		SecondaryCurrency: "EUR", FXRateMode: "manual", Timezone: "America/Sao_Paulo",
	}
	// Update is handed a different creation instant on purpose. It must
	// ignore it: a household's creation is set once, by the database.
	attempted := want
	attempted.CreatedAt = created.CreatedAt.AddDate(-1, 0, 0)
	updated, err := households.Update(ctx, attempted)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("Update moved the creation instant to %v, want it left at %v", updated.CreatedAt, created.CreatedAt)
	}
	// CreatedAt is compared with Equal above and blanked here: two time.Time
	// values for one instant can differ under ==, which compares the
	// location pointer too.
	if withoutCreatedAt(updated) != want {
		t.Fatalf("Update returned %+v, want %+v", updated, want)
	}

	fetched, err := households.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !fetched.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("Get after Update has creation instant %v, want %v", fetched.CreatedAt, created.CreatedAt)
	}
	if withoutCreatedAt(fetched) != want {
		t.Fatalf("Get after Update = %+v, want %+v", fetched, want)
	}

	if _, err := households.Get(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}
}

// withoutCreatedAt is h with its creation instant cleared, so the rest of a
// household can be compared with ==.
func withoutCreatedAt(h domain.Household) domain.Household {
	h.CreatedAt = time.Time{}
	return h
}

// A household with no time zone cannot be given a "today": Go loads "" as UTC
// without an error, so the row would look like a stored choice and behave
// like the server's clock. The column refuses it, so a caller that forgets
// the field fails at the write instead of on every later request.
func TestHouseholdRepoRefusesAnEmptyTimezone(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)

	if _, err := households.Create(ctx, domain.Household{
		Name: "No zone", FamilyName: "No zone", PrimaryCurrency: "SGD", SecondaryCurrency: "SGD",
	}); err == nil {
		t.Fatal("Create accepted a household with an empty time zone")
	}

	created, err := households.Create(ctx, domain.Household{
		Name: "Zoned", FamilyName: "Zoned", PrimaryCurrency: "SGD", SecondaryCurrency: "SGD",
		Timezone: "Asia/Singapore",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	created.Timezone = ""
	if _, err := households.Update(ctx, created); err == nil {
		t.Fatal("Update accepted an empty time zone")
	}
	fetched, err := households.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.Timezone != "Asia/Singapore" {
		t.Fatalf("timezone = %q after a refused update, want Asia/Singapore", fetched.Timezone)
	}
}
