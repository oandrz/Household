package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
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

	household, err := households.Create(ctx, domain.Household{Timezone: "UTC", Name: "Test", FamilyName: "Test", PrimaryCurrency: "SGD"})
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

// TestExtendingASessionKeepsItsAdminGrant guards ExtendSession's one-column
// write: the day someone widens it to a whole-row update, every live admin
// grant would silently reset and nothing else in the suite would notice.
func TestExtendingASessionKeepsItsAdminGrant(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	users := postgres.NewUserRepo(db)
	households := postgres.NewHouseholdRepo(db)
	sessions := postgres.NewSessionRepo(db)

	household, err := households.Create(ctx, domain.Household{Timezone: "UTC", Name: "Test", FamilyName: "Test", PrimaryCurrency: "SGD"})
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

// TestTouchWritesOnlyLastSeenAt is Touch's half of the one-column rule
// TestExtendingASessionKeepsItsAdminGrant states for Extend and GrantAdmin:
// each of the three writes its own column and none can overwrite another's.
func TestTouchWritesOnlyLastSeenAt(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	users := postgres.NewUserRepo(db)
	households := postgres.NewHouseholdRepo(db)
	sessions := postgres.NewSessionRepo(db)

	household, err := households.Create(ctx, domain.Household{Timezone: "UTC", Name: "Test", FamilyName: "Test", PrimaryCurrency: "SGD"})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	user, err := users.Create(ctx, "touch@example.test", "", "Touch")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	tokenHash := []byte("a-32-byte-looking-token-hash-tch")
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)
	if err := sessions.Create(ctx, tokenHash, user.ID, household.ID, expiry); err != nil {
		t.Fatalf("create session: %v", err)
	}
	grant := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Millisecond)
	if err := sessions.GrantAdmin(ctx, tokenHash, &grant); err != nil {
		t.Fatalf("GrantAdmin: %v", err)
	}

	before, err := sessions.ByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("ByTokenHash before touch: %v", err)
	}
	if before.LastSeenAt != nil {
		t.Fatalf("LastSeenAt before any touch = %v, want nil", before.LastSeenAt)
	}

	seen := time.Now().UTC().Truncate(time.Millisecond)
	if err := sessions.Touch(ctx, tokenHash, seen); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	after, err := sessions.ByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("ByTokenHash after touch: %v", err)
	}
	if after.LastSeenAt == nil || !after.LastSeenAt.Equal(seen) {
		t.Fatalf("LastSeenAt = %v, want %v", after.LastSeenAt, seen)
	}
	if !after.ExpiresAt.Equal(expiry) {
		t.Fatalf("Touch moved expires_at: %v, want %v", after.ExpiresAt, expiry)
	}
	if after.AdminGrantExpiresAt == nil || !after.AdminGrantExpiresAt.Equal(grant) {
		t.Fatalf("Touch changed the admin grant: %v, want %v", after.AdminGrantExpiresAt, grant)
	}
}

// TestAdminAuditRepoRecordsTheRow reads admin_audit_log directly: the port is
// write-only (usecase.AdminAuditRepository's doc comment), so the table is the
// only place to check what Record wrote.
func TestAdminAuditRepoRecordsTheRow(t *testing.T) {
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

	var target, ip string
	var detail []byte
	err = db.Pool().QueryRow(ctx,
		`SELECT target, detail, ip::text FROM admin_audit_log WHERE actor_user_id::text = $1`, user.ID).
		Scan(&target, &detail, &ip)
	if err != nil {
		t.Fatalf("read the audit row back: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(detail, &decoded); err != nil {
		t.Fatalf("decode detail %q: %v", detail, err)
	}
	if target != "family_calendar" || decoded["enabled"] != true || ip != "203.0.113.5" {
		t.Fatalf("row = target %q, detail %v, ip %q; want the target, detail and ip written", target, decoded, ip)
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
	since := now.Add(-time.Hour)
	for i := 0; i < 3; i++ {
		if err := attempts.Record(ctx, a.ID, false, now); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if err := attempts.Record(ctx, a.ID, true, now); err != nil {
		t.Fatalf("record success: %v", err)
	}

	reserved, err := attempts.ReserveFailure(ctx, a.ID, now, since)
	if err != nil {
		t.Fatalf("ReserveFailure(a): %v", err)
	}
	if len(reserved.Earlier) != 3 {
		t.Fatalf("earlier failures for a = %d, want 3 (successes are not failures, and a reservation "+
			"does not count itself)", len(reserved.Earlier))
	}

	other, err := attempts.ReserveFailure(ctx, b.ID, now, since)
	if err != nil {
		t.Fatalf("ReserveFailure(b): %v", err)
	}
	if len(other.Earlier) != 0 {
		t.Fatalf("earlier failures for b = %d, want 0 -- one admin's failures must not lock another", len(other.Earlier))
	}

	// A withdrawn reservation is gone; one that was not withdrawn counts.
	if err := attempts.Withdraw(ctx, reserved.ID); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	again, err := attempts.ReserveFailure(ctx, a.ID, now, since)
	if err != nil {
		t.Fatalf("ReserveFailure(a) after Withdraw: %v", err)
	}
	if len(again.Earlier) != 3 {
		t.Fatalf("earlier failures for a after Withdraw = %d, want 3 again", len(again.Earlier))
	}

	if err := attempts.ClearFailures(ctx, a.ID); err != nil {
		t.Fatalf("ClearFailures: %v", err)
	}
	cleared, err := attempts.ReserveFailure(ctx, a.ID, now, since)
	if err != nil {
		t.Fatalf("ReserveFailure(a) after ClearFailures: %v", err)
	}
	if len(cleared.Earlier) != 0 {
		t.Fatalf("earlier failures for a after ClearFailures = %d, want 0", len(cleared.Earlier))
	}
}

// TestAdminReauthReservationsMadeTogetherEachGetAPlaceInLine is
// TestReservationsMadeTogetherEachSeeTheOnesBeforeThem for the admin
// prompt's ledger: simultaneous attempts by one operator see 0, 1, 2 ...
// earlier failures, one each, so only the first three can be "the first
// three".
func TestAdminReauthReservationsMadeTogetherEachGetAPlaceInLine(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	attempts := postgres.NewAdminReauthAttemptRepo(db)
	operator, err := postgres.NewUserRepo(db).Create(ctx, "operator@example.test", "", "Operator")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	const callers = 40
	now := time.Now()
	seen := make([]int, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			reserved, err := attempts.ReserveFailure(ctx, operator.ID, now, now.Add(-time.Hour))
			if err != nil {
				t.Errorf("ReserveFailure: %v", err)
				return
			}
			seen[i] = len(reserved.Earlier)
		}()
	}
	close(start)
	wg.Wait()
	if t.Failed() {
		return
	}

	sort.Ints(seen)
	for i, n := range seen {
		if n != min(i, usecase.MaxEarlierFailures) {
			t.Fatalf("simultaneous reservations did not each get their own place in line; "+
				"what each one saw, sorted: %v", seen)
		}
	}
}
