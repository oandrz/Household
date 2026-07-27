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

func TestSignupRepoRoundTrip(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.NewSignupRepo(db)
	ctx := context.Background()

	expires := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	hash := []byte("round-trip-hash-aaaaaaaaaaaaaaaa")
	if err := repo.Create(ctx, "Round@Trip.test", hash, expires); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.ByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	// citext preserves the stored casing; the lookup is what is
	// case-insensitive. Assert the address survives as typed.
	if got.Email != "Round@Trip.test" {
		t.Fatalf("Email = %q, want the address as given", got.Email)
	}
	if got.ConsumedAt != nil {
		t.Fatalf("ConsumedAt = %v, want nil on a fresh signup", got.ConsumedAt)
	}
	if !got.ExpiresAt.Equal(expires) {
		t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, expires)
	}
}

// TestSignupRepoCreateConsumedWritesAnInertCounterRow exercises
// CreateConsumedSignup for real, against a real Postgres instance -- nothing
// else in this package or in usecase's tests ever executes that SQL, since
// signupDouble only simulates it. It confirms the fix-round property the
// query exists for: the row is written already consumed, so it counts toward
// CountForEmailSince (what fixes the rate-limit finding) but can never
// provision anything.
func TestSignupRepoCreateConsumedWritesAnInertCounterRow(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.NewSignupRepo(db)
	ctx := context.Background()

	expires := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	hash := []byte("consumed-hash-eeeeeeeeeeeeeeeeee")
	if err := repo.CreateConsumed(ctx, "taken@example.test", hash, expires); err != nil {
		t.Fatalf("CreateConsumed: %v", err)
	}

	got, err := repo.ByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	if got.ConsumedAt == nil {
		t.Fatal("ConsumedAt is nil, want it stamped at insert time")
	}
	if got.Email != "taken@example.test" {
		t.Fatalf("Email = %q, want taken@example.test", got.Email)
	}

	count, err := repo.CountForEmailSince(ctx, "taken@example.test", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CountForEmailSince: %v", err)
	}
	if count != 1 {
		t.Fatalf("CountForEmailSince = %d, want 1 -- this is the entire reason CreateConsumed exists", count)
	}

	blueprint, err := usecase.NewSignupBlueprint("Someone Else's Household", "Stranger", "SGD")
	if err != nil {
		t.Fatalf("NewSignupBlueprint: %v", err)
	}
	if _, err := repo.Provision(ctx, got.ID, "hashed-password", blueprint); !errors.Is(err, domain.ErrTokenExpired) {
		t.Fatalf("error = %v, want domain.ErrTokenExpired -- a pre-consumed row must never provision a household", err)
	}
}

func TestSignupRepoByTokenHashReportsNotFound(t *testing.T) {
	repo := postgres.NewSignupRepo(openTestDB(t))
	_, err := repo.ByTokenHash(context.Background(), []byte("no-such-hash-bbbbbbbbbbbbbbbbbbb"))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v, want domain.ErrNotFound", err)
	}
}

func TestSignupRepoCountForEmailSinceDoesNotJoinThroughUsers(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.NewSignupRepo(db)
	ctx := context.Background()

	// The address has no account at all. The count must still be 2: a limit
	// that could only be reached by a registered address would itself tell a
	// caller which addresses are registered.
	for i, hash := range [][]byte{
		[]byte("count-one-cccccccccccccccccccccc"),
		[]byte("count-two-dddddddddddddddddddddd"),
	} {
		if err := repo.Create(ctx, "stranger@example.test", hash, time.Now().Add(time.Hour)); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	got, err := repo.CountForEmailSince(ctx, "stranger@example.test", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CountForEmailSince: %v", err)
	}
	if got != 2 {
		t.Fatalf("CountForEmailSince = %d, want 2", got)
	}
}

func TestSignupRepoProvisionCreatesTheWholeHousehold(t *testing.T) {
	db := openTestDB(t)
	pool := db.Pool()
	repo := postgres.NewSignupRepo(db)
	ctx := context.Background()

	hash := []byte("provision-hash-eeeeeeeeeeeeeeeee")
	if err := repo.Create(ctx, "founder@example.test", hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	signup, err := repo.ByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}

	blueprint, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "SGD")
	if err != nil {
		t.Fatalf("NewSignupBlueprint: %v", err)
	}

	got, err := repo.Provision(ctx, signup.ID, "hashed-password", blueprint)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if got.UserID == "" || got.HouseholdID == "" || got.MembershipID == "" {
		t.Fatalf("Provision returned %+v, want every id populated", got)
	}

	t.Run("the user carries the verified address, not one passed in", func(t *testing.T) {
		var email, initial string
		if err := pool.QueryRow(ctx,
			`SELECT email, avatar_initial FROM users WHERE id = $1`, got.UserID).Scan(&email, &initial); err != nil {
			t.Fatalf("query user: %v", err)
		}
		if email != "founder@example.test" {
			t.Fatalf("email = %q, want the address from the signup row", email)
		}
		if initial != "A" {
			t.Fatalf("avatar_initial = %q, want A", initial)
		}
	})

	t.Run("the owner holds every capability", func(t *testing.T) {
		var role string
		var caps []string
		if err := pool.QueryRow(ctx,
			`SELECT role, capabilities FROM memberships WHERE id = $1`, got.MembershipID).Scan(&role, &caps); err != nil {
			t.Fatalf("query membership: %v", err)
		}
		if role != "owner" {
			t.Fatalf("role = %q, want owner", role)
		}
		if len(caps) != len(domain.AllCapabilities()) {
			t.Fatalf("capabilities = %v, want all %d", caps, len(domain.AllCapabilities()))
		}
	})

	t.Run("all three builtin spaces exist, in position order", func(t *testing.T) {
		rows, err := pool.Query(ctx,
			`SELECT key FROM spaces WHERE household_id = $1 ORDER BY position`, got.HouseholdID)
		if err != nil {
			t.Fatalf("query spaces: %v", err)
		}
		defer rows.Close()
		var keys []string
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				t.Fatalf("scan: %v", err)
			}
			keys = append(keys, key)
		}
		want := []string{"money", "marriage", "family"}
		if len(keys) != len(want) {
			t.Fatalf("keys = %v, want %v", keys, want)
		}
		for i := range want {
			if keys[i] != want[i] {
				t.Fatalf("keys = %v, want %v", keys, want)
			}
		}
	})

	t.Run("notification preferences are on", func(t *testing.T) {
		var bills, overspend, retro, digest bool
		if err := pool.QueryRow(ctx,
			`SELECT bill_reminders, overspend_alerts, retro_reminder, weekly_digest
			 FROM notification_preferences WHERE household_id = $1`, got.HouseholdID).
			Scan(&bills, &overspend, &retro, &digest); err != nil {
			t.Fatalf("query preferences: %v", err)
		}
		if !bills || !overspend || !retro || !digest {
			t.Fatal("want every notification flag true")
		}
	})

	t.Run("secondary currency mirrors primary and the toggle is off", func(t *testing.T) {
		var primary, secondary string
		var show bool
		if err := pool.QueryRow(ctx,
			`SELECT primary_currency, secondary_currency, show_secondary_currency
			 FROM households WHERE id = $1`, got.HouseholdID).Scan(&primary, &secondary, &show); err != nil {
			t.Fatalf("query household: %v", err)
		}
		if primary != "SGD" || secondary != "SGD" || show {
			t.Fatalf("got primary=%q secondary=%q show=%v, want SGD/SGD/false", primary, secondary, show)
		}
	})

	t.Run("the signup is consumed, so it cannot be used twice", func(t *testing.T) {
		_, err := repo.Provision(ctx, signup.ID, "hashed-password", blueprint)
		if !errors.Is(err, domain.ErrTokenExpired) {
			t.Fatalf("second Provision error = %v, want domain.ErrTokenExpired", err)
		}
	})
}

// This is the test the whole transaction exists for. A partial provision leaves
// a users row occupying users.email's unique index with no membership under it,
// which makes that address permanently unable to sign up again -- no retry
// could ever create a second user with it.
//
// The failure is forced with a real constraint rather than a mock: an owner
// holding fewer than every capability violates the memberships
// owners_hold_all_capabilities CHECK, which fires *after* the household and the
// user have already been inserted in the same transaction. That is exactly the
// mid-transaction position a partial write would occupy.
func TestSignupRepoProvisionIsAllOrNothing(t *testing.T) {
	db := openTestDB(t)
	pool := db.Pool()
	repo := postgres.NewSignupRepo(db)
	ctx := context.Background()

	hash := []byte("atomic-hash-fffffffffffffffffff")
	if err := repo.Create(ctx, "atomic@example.test", hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	signup, err := repo.ByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}

	blueprint, err := usecase.NewSignupBlueprint("Doomed household", "Ade", "SGD")
	if err != nil {
		t.Fatalf("NewSignupBlueprint: %v", err)
	}
	// An owner with only one capability. domain.NewMembership would refuse this
	// too, but Provision is handed a blueprint directly, so the database CHECK
	// is the gate being exercised here.
	blueprint.OwnerCapabilities = domain.Capabilities{domain.CapCalendar}

	if _, err := repo.Provision(ctx, signup.ID, "hashed-password", blueprint); err == nil {
		t.Fatal("Provision succeeded with an under-capable owner; the CHECK constraint did not fire")
	}

	t.Run("no household survived", func(t *testing.T) {
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM households WHERE name = $1`, "Doomed household").Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 0 {
			t.Fatalf("found %d households, want 0", count)
		}
	})

	t.Run("no user survived, so the address is not blocked", func(t *testing.T) {
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM users WHERE email = $1`, "atomic@example.test").Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 0 {
			t.Fatalf("found %d users, want 0 -- a surviving row permanently blocks this address", count)
		}
	})

	t.Run("the signup is still unconsumed, so a retry is possible", func(t *testing.T) {
		again, err := repo.ByTokenHash(ctx, hash)
		if err != nil {
			t.Fatalf("ByTokenHash: %v", err)
		}
		if again.ConsumedAt != nil {
			t.Fatalf("ConsumedAt = %v, want nil -- a consumed signup after a failed provision is unrecoverable",
				again.ConsumedAt)
		}
	})
}

func TestSignupRepoProvisionRefusesAnExpiredSignup(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.NewSignupRepo(db)
	ctx := context.Background()

	hash := []byte("expired-hash-gggggggggggggggggg")
	if err := repo.Create(ctx, "late@example.test", hash, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	signup, err := repo.ByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	blueprint, err := usecase.NewSignupBlueprint("Too late", "Ade", "SGD")
	if err != nil {
		t.Fatalf("NewSignupBlueprint: %v", err)
	}
	if _, err := repo.Provision(ctx, signup.ID, "hashed-password", blueprint); !errors.Is(err, domain.ErrTokenExpired) {
		t.Fatalf("error = %v, want domain.ErrTokenExpired", err)
	}
}

func TestSignupRepoPruneLeavesLiveRows(t *testing.T) {
	db := openTestDB(t)
	pool := db.Pool()
	repo := postgres.NewSignupRepo(db)
	ctx := context.Background()

	// One live, one expired. Both created long enough ago to be inside the
	// prune cutoff, so only liveness decides.
	if err := repo.Create(ctx, "live@example.test", []byte("live-hash-hhhhhhhhhhhhhhhhhhhhh"),
		time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("Create live: %v", err)
	}
	if err := repo.Create(ctx, "dead@example.test", []byte("dead-hash-iiiiiiiiiiiiiiiiiiiii"),
		time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("Create expired: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE signups SET created_at = now() - interval '40 days'`); err != nil {
		t.Fatalf("age the rows: %v", err)
	}

	deleted, err := repo.Prune(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("Prune deleted %d, want 1 -- only the expired row", deleted)
	}
	if _, err := repo.ByTokenHash(ctx, []byte("live-hash-hhhhhhhhhhhhhhhhhhhhh")); err != nil {
		t.Fatalf("the live signup was pruned: %v", err)
	}
}

// The reason PruneLoginAttempts exists: ClearFailures is scoped
// WHERE household_id = $1, which never matches the NULL rows an
// unknown-address attempt records.
func TestLoginAttemptRepoPruneReachesNullHouseholdRows(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.NewLoginAttemptRepo(db)
	ctx := context.Background()

	old := time.Now().Add(-40 * 24 * time.Hour)
	if err := repo.Record(ctx, nil, nil, "stranger@example.test", false, old); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// ClearFailures cannot touch it -- there is no household to scope by.
	deleted, err := repo.Prune(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("Prune deleted %d, want 1", deleted)
	}
}

func TestLoginAttemptRepoPruneKeepsRecentRows(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.NewLoginAttemptRepo(db)
	ctx := context.Background()

	// Inside the lockout window. Pruning this would clear a live lockout.
	if err := repo.Record(ctx, nil, nil, "recent@example.test", false, time.Now()); err != nil {
		t.Fatalf("Record: %v", err)
	}
	deleted, err := repo.Prune(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("Prune deleted %d recent rows, want 0", deleted)
	}
}
