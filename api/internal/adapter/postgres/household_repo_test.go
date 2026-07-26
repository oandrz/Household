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

	updated, err := households.Update(ctx, domain.Household{
		ID: created.ID, FamilyName: "Tan", PrimaryCurrency: "USD",
		ShowSecondaryCurrency: false, FXRateMode: "manual",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.FamilyName != "Tan" || updated.PrimaryCurrency != "USD" ||
		updated.ShowSecondaryCurrency || updated.FXRateMode != "manual" {
		t.Fatalf("update did not persist: %+v", updated)
	}
	// Name and SecondaryCurrency are not writable through Update; they must
	// come back unchanged rather than as the (empty) values on the argument.
	if updated.Name != "Andreas & Christine" || updated.SecondaryCurrency != "IDR" {
		t.Fatalf("Update must not touch Name or SecondaryCurrency: %+v", updated)
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
