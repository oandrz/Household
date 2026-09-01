package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestPlatformAdminRepoGrantsAndRevokes(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	users := postgres.NewUserRepo(db)
	admins := postgres.NewPlatformAdminRepo(db)

	user, err := users.Create(ctx, "operator@example.test", "", "Operator")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := admins.Get(ctx, user.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get before Grant = %v, want ErrNotFound", err)
	}

	if err := admins.Grant(ctx, user.ID, "the operator"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	// Granting twice must not fail: adminctl is run by a human who will run it
	// twice, and a crash there reads as "it didn't work".
	if err := admins.Grant(ctx, user.ID, "the operator again"); err != nil {
		t.Fatalf("Grant twice: %v", err)
	}

	got, err := admins.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("Get after Grant: %v", err)
	}
	if got.UserID != user.ID || got.Note != "the operator again" {
		t.Fatalf("Get = %+v, want the second note", got)
	}

	if err := admins.Revoke(ctx, user.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := admins.Get(ctx, user.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get after Revoke = %v, want ErrNotFound", err)
	}
}

func TestFeatureFlagRepoSeparatesGlobalFromHouseholdOverrides(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	flags := postgres.NewFeatureFlagRepo(db)

	household, err := households.Create(ctx, domain.Household{Name: "Test", FamilyName: "Test", PrimaryCurrency: "SGD"})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	actor, err := users.Create(ctx, "flags@example.test", "", "Flags")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := flags.SetGlobal(ctx, string(domain.FlagFamilyCalendar), true, actor.ID); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}
	if err := flags.SetHousehold(ctx, household.ID, string(domain.FlagFamilyCalendar), false, actor.ID); err != nil {
		t.Fatalf("SetHousehold: %v", err)
	}

	global, house, err := flags.OverridesFor(ctx, household.ID)
	if err != nil {
		t.Fatalf("OverridesFor: %v", err)
	}
	if global[string(domain.FlagFamilyCalendar)] != true {
		t.Fatalf("global = %v, want the flag true", global)
	}
	if house[string(domain.FlagFamilyCalendar)] != false {
		t.Fatalf("household = %v, want the flag false", house)
	}

	// SetGlobal twice is an update, not a second row: the key is the primary
	// key, and an INSERT without ON CONFLICT would fail on the second save.
	if err := flags.SetGlobal(ctx, string(domain.FlagFamilyCalendar), false, actor.ID); err != nil {
		t.Fatalf("SetGlobal twice: %v", err)
	}
	global, _, err = flags.OverridesFor(ctx, household.ID)
	if err != nil {
		t.Fatalf("OverridesFor again: %v", err)
	}
	if global[string(domain.FlagFamilyCalendar)] != false {
		t.Fatalf("global after update = %v, want false", global)
	}

	if err := flags.ClearHousehold(ctx, household.ID, string(domain.FlagFamilyCalendar)); err != nil {
		t.Fatalf("ClearHousehold: %v", err)
	}
	_, house, err = flags.OverridesFor(ctx, household.ID)
	if err != nil {
		t.Fatalf("OverridesFor after clear: %v", err)
	}
	if _, present := house[string(domain.FlagFamilyCalendar)]; present {
		t.Fatalf("household override survived ClearHousehold: %v", house)
	}
}

// TestExtendingASessionKeepsItsAdminGrant is the test the spec asks for by
// name. ExtendSession writes one column today; the day someone widens it to a
// whole-row update, every live admin grant would silently reset and nothing
// else in the suite would notice.
func TestExtendingASessionKeepsItsAdminGrant(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	users := postgres.NewUserRepo(db)
	households := postgres.NewHouseholdRepo(db)
	sessions := postgres.NewSessionRepo(db)

	household, err := households.Create(ctx, domain.Household{Name: "Test", FamilyName: "Test", PrimaryCurrency: "SGD"})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	user, err := users.Create(ctx, "session@example.test", "", "Session")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	tokenHash := []byte("a-32-byte-looking-token-hash----")
	if err := sessions.Create(ctx, tokenHash, user.ID, household.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}

	grant := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Millisecond)
	if err := sessions.GrantAdmin(ctx, tokenHash, &grant); err != nil {
		t.Fatalf("GrantAdmin: %v", err)
	}
	if err := sessions.Extend(ctx, tokenHash, time.Now().Add(48*time.Hour)); err != nil {
		t.Fatalf("Extend: %v", err)
	}

	record, err := sessions.ByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	if record.AdminGrantExpiresAt == nil {
		t.Fatal("extending the session cleared its admin grant")
	}
	if !record.AdminGrantExpiresAt.Equal(grant) {
		t.Fatalf("grant = %v, want %v", record.AdminGrantExpiresAt, grant)
	}

	if err := sessions.GrantAdmin(ctx, tokenHash, nil); err != nil {
		t.Fatalf("GrantAdmin(nil): %v", err)
	}
	record, err = sessions.ByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("ByTokenHash after clear: %v", err)
	}
	if record.AdminGrantExpiresAt != nil {
		t.Fatalf("grant after clear = %v, want nil", record.AdminGrantExpiresAt)
	}
}

func TestAdminAuditRepoRecordsAndReadsBack(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	users := postgres.NewUserRepo(db)
	audit := postgres.NewAdminAuditRepo(db)

	user, err := users.Create(ctx, "audited@example.test", "", "Audited")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := audit.Record(ctx, usecase.AdminAuditEntry{
		ActorUserID: user.ID,
		Action:      "PUT /api/v1/admin/flags/{key}",
		Target:      "family_calendar",
		Detail:      map[string]any{"enabled": true},
		IP:          "203.0.113.5",
		At:          time.Now(),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := audit.Recent(ctx, 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Recent returned %d entries, want 1", len(entries))
	}
	if entries[0].Target != "family_calendar" || entries[0].Detail["enabled"] != true {
		t.Fatalf("entry = %+v, want the target and detail written", entries[0])
	}
}

func TestAdminReauthAttemptsAreScopedToOneUser(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	users := postgres.NewUserRepo(db)
	attempts := postgres.NewAdminReauthAttemptRepo(db)

	a, err := users.Create(ctx, "a@example.test", "", "A")
	if err != nil {
		t.Fatalf("create user a: %v", err)
	}
	b, err := users.Create(ctx, "b@example.test", "", "B")
	if err != nil {
		t.Fatalf("create user b: %v", err)
	}

	now := time.Now()
	for i := 0; i < 3; i++ {
		if err := attempts.Record(ctx, a.ID, false, now); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if err := attempts.Record(ctx, a.ID, true, now); err != nil {
		t.Fatalf("record success: %v", err)
	}

	failures, err := attempts.FailuresSince(ctx, a.ID, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("FailuresSince: %v", err)
	}
	if len(failures) != 3 {
		t.Fatalf("FailuresSince(a) = %d, want 3 (successes are not failures)", len(failures))
	}

	otherFailures, err := attempts.FailuresSince(ctx, b.ID, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("FailuresSince(b): %v", err)
	}
	if len(otherFailures) != 0 {
		t.Fatalf("FailuresSince(b) = %d, want 0 -- one admin's failures must not lock another", len(otherFailures))
	}

	if err := attempts.ClearFailures(ctx, a.ID); err != nil {
		t.Fatalf("ClearFailures: %v", err)
	}
	failures, err = attempts.FailuresSince(ctx, a.ID, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("FailuresSince after clear: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("FailuresSince after ClearFailures = %d, want 0", len(failures))
	}
}
