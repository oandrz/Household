package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestNotificationPreferencesRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)
	notifications := postgres.NewNotificationRepo(db)

	h, err := households.Create(ctx, domain.Household{
		Name: "H", FamilyName: "H",
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}

	if _, err := notifications.Get(ctx, h.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get on a household with no preferences row: got %v, want domain.ErrNotFound", err)
	}

	// An alternating pattern, not all-true or all-false, so a cross-wired
	// field assignment would fail this assertion.
	want := usecase.NotificationPreferences{
		BillReminders: true, OverspendAlerts: false, RetroReminder: true, WeeklyDigest: false,
	}
	upserted, err := notifications.Upsert(ctx, h.ID, want)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if upserted != want {
		t.Fatalf("Upsert returned %+v, want %+v", upserted, want)
	}

	got, err := notifications.Get(ctx, h.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != want {
		t.Fatalf("Get = %+v, want %+v", got, want)
	}

	// Upsert again with the opposite pattern to prove ON CONFLICT DO UPDATE
	// actually overwrites the row rather than leaving the first insert in place.
	want2 := usecase.NotificationPreferences{
		BillReminders: false, OverspendAlerts: true, RetroReminder: false, WeeklyDigest: true,
	}
	if _, err := notifications.Upsert(ctx, h.ID, want2); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	got, err = notifications.Get(ctx, h.ID)
	if err != nil {
		t.Fatalf("Get after second upsert: %v", err)
	}
	if got != want2 {
		t.Fatalf("Get after second upsert = %+v, want %+v", got, want2)
	}
}
