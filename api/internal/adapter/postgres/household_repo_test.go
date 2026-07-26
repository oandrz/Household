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

	created, err := households.Create(ctx, "Andreas & Christine", "Oentoro")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create did not return an id — uuidToString must have failed")
	}
	if created.PrimaryCurrency != "SGD" || created.SecondaryCurrency != "IDR" ||
		!created.ShowSecondaryCurrency || created.FXRateMode != "auto" {
		t.Fatalf("defaults not applied: %+v", created)
	}

	// Change every field, including Name and SecondaryCurrency -- the two
	// UpdateHousehold used to silently drop. This is the assertion that makes
	// a future narrowing of the query fail loudly instead of returning a nil
	// error over a partial write.
	want := domain.Household{
		ID: created.ID, Name: "The Oentoro Household", FamilyName: "Tan",
		PrimaryCurrency: "USD", ShowSecondaryCurrency: false,
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
