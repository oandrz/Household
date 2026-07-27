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

	// PrimaryCurrency, SecondaryCurrency and ShowSecondaryCurrency deliberately
	// differ from the households table's own column defaults (SGD/IDR/true):
	// Create now takes a fully-populated domain.Household rather than
	// (name, familyName), and CreateHouseholdParams is a keyed sqlc struct --
	// if a field were dropped from the params literal in household_repo.go,
	// Go would silently zero-value it and the column default would mask the
	// bug. Asserting values that differ from the defaults is what makes this
	// test actually prove Create forwards the caller's values, not just that
	// it forwards *some* values coincidentally equal to what the old
	// hardcoded literals already were. FXRateMode is asserted separately as
	// "auto" because Create ignores it by design (see
	// HouseholdRepository.Create's doc comment) -- the column default is the
	// only value the CHECK constraint makes safe to assume at creation time.
	//
	// These values also deliberately differ from what Update sets below, so
	// that step still proves Update writes every field rather than finding
	// it already at the target value.
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

	// Change every field, including Name and SecondaryCurrency -- the two
	// UpdateHousehold used to silently drop. This is the assertion that makes
	// a future narrowing of the query fail loudly instead of returning a nil
	// error over a partial write.
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
