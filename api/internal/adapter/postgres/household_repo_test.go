package postgres_test

import (
	"context"
	"errors"
	"testing"

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
	// HouseholdRepository.Create).
	created, err := households.Create(ctx, domain.Household{
		Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "GBP", SecondaryCurrency: "JPY", ShowSecondaryCurrency: false,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create did not return an id — uuidToString must have failed")
	}
	if created.PrimaryCurrency != "GBP" || created.SecondaryCurrency != "JPY" ||
		created.ShowSecondaryCurrency || created.FXRateMode != "auto" {
		t.Fatalf("Create did not forward the caller's household: %+v", created)
	}

	// Change every field, including Name and SecondaryCurrency. Don't let
	// the query narrow back to a subset of columns -- this assertion is what
	// makes a future narrowing fail loudly, instead of returning a nil error
	// over a partial write.
	want := domain.Household{
		ID: created.ID, Name: "The Oentoro Household", FamilyName: "Tan",
		PrimaryCurrency: "USD", ShowSecondaryCurrency: true,
		SecondaryCurrency: "EUR", FXRateMode: "manual",
	}
	updated, err := households.Update(ctx, want)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated != want {
		t.Fatalf("Update returned %+v, want %+v", updated, want)
	}

	fetched, err := households.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched != updated {
		t.Fatalf("Get after Update = %+v, want %+v", fetched, updated)
	}

	if _, err := households.Get(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}
}
