package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestUserRepoRoundTrip(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.NewUserRepo(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, "andreas@hearth.family", "$argon2id$fake", "Andreas")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.AvatarInitial != "A" {
		t.Fatalf("AvatarInitial = %q, want A — it is derived from the display name", created.AvatarInitial)
	}

	found, err := repo.ByEmail(ctx, "ANDREAS@HEARTH.FAMILY")
	if err != nil {
		t.Fatalf("ByEmail is case-insensitive because the column is citext: %v", err)
	}
	if found.ID != created.ID {
		t.Fatal("ByEmail returned a different user")
	}

	if _, err := repo.ByEmail(ctx, "nobody@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}
}

// TestFindOrphanedChild proves the query behind seed.go's duplicate-child
// guard against a real Postgres query planner: it finds a credential-less
// user with no membership row at all, but not one that still has a
// membership, and not one that has an email or a password even if its
// display name matches.
func TestFindOrphanedChild(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	members := postgres.NewMembershipRepo(db)

	if _, err := users.FindOrphanedChild(ctx, "Kayla"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("before any user exists: got %v, want domain.ErrNotFound", err)
	}

	h, err := households.Create(ctx, "Andreas & Christine", "Oentoro")
	if err != nil {
		t.Fatalf("create household: %v", err)
	}

	// A credential-less user with a membership is not an orphan.
	memberedKayla, err := users.Create(ctx, "", "", "Kayla")
	if err != nil {
		t.Fatalf("create membered kayla: %v", err)
	}
	if _, err := members.Create(ctx, domain.Membership{
		HouseholdID: h.ID, UserID: memberedKayla.ID, Role: domain.RoleLimited,
		Capabilities: domain.Capabilities{domain.CapCalendar},
	}); err != nil {
		t.Fatalf("create membership: %v", err)
	}
	if _, err := users.FindOrphanedChild(ctx, "Kayla"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a membered Kayla must not be reported as an orphan: got %v", err)
	}

	// A user with an email, even with no membership, is not this kind of
	// orphan -- FindOrphanedChild only concerns credential-less members.
	if _, err := users.Create(ctx, "kayla2@example.com", "", "Kayla"); err != nil {
		t.Fatalf("create emailed kayla: %v", err)
	}
	if _, err := users.FindOrphanedChild(ctx, "Kayla"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("an emailed Kayla must not be reported as an orphan: got %v", err)
	}

	// The real orphan: credential-less, no membership.
	orphan, err := users.Create(ctx, "", "", "Kayla")
	if err != nil {
		t.Fatalf("create orphaned kayla: %v", err)
	}
	found, err := users.FindOrphanedChild(ctx, "Kayla")
	if err != nil {
		t.Fatalf("FindOrphanedChild: %v", err)
	}
	if found.ID != orphan.ID {
		t.Fatalf("found = %+v, want the orphaned user %+v", found, orphan)
	}
}

func TestMembershipRepoRejectsAnInvalidCapabilitySet(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	members := postgres.NewMembershipRepo(db)

	h, err := households.Create(ctx, "Andreas & Christine", "Oentoro")
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	u, err := users.Create(ctx, "", "", "Ethan")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// The domain constructor is the first gate; the database check constraint is
	// the second. This asserts the second, by bypassing the first.
	_, err = members.Create(ctx, domain.Membership{
		HouseholdID: h.ID, UserID: u.ID, Role: domain.RoleLimited,
		Capabilities: domain.Capabilities{domain.CapMarriage},
	})
	if err == nil {
		t.Fatal("expected the database constraint to reject marriage for a limited member")
	}
	// A constraint violation must not be mistranslated as "not found": that
	// would report data that is present and invalid as if it were absent.
	// translate special-cases only pgx.ErrNoRows today; this guards against a
	// regression that widened the mapping.
	if errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("constraint violation must not translate to domain.ErrNotFound, got %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	h, _ := postgres.NewHouseholdRepo(db).Create(ctx, "H", "H")
	u, _ := postgres.NewUserRepo(db).Create(ctx, "a@b.c", "hash", "Andreas")
	sessions := postgres.NewSessionRepo(db)

	hash := []byte("0123456789abcdef0123456789abcdef")
	expiry := time.Now().Add(30 * 24 * time.Hour)

	if err := sessions.Create(ctx, hash, u.ID, h.ID, expiry); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rec, err := sessions.ByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	if rec.UserID != u.ID || rec.HouseholdID != h.ID {
		t.Fatalf("record = %+v", rec)
	}

	if err := sessions.RevokeByToken(ctx, hash); err != nil {
		t.Fatalf("RevokeByToken: %v", err)
	}
	if _, err := sessions.ByTokenHash(ctx, hash); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a revoked session must not resolve, got err = %v", err)
	}
}

func TestLoginAttemptsRespectTheWindow(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	h, _ := postgres.NewHouseholdRepo(db).Create(ctx, "H", "H")
	attempts := postgres.NewLoginAttemptRepo(db)

	base := time.Now().UTC()
	for _, offset := range []time.Duration{-40 * time.Minute, -5 * time.Minute, -1 * time.Minute} {
		if err := attempts.Record(ctx, &h.ID, nil, "a@b.c", false, base.Add(offset)); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	failures, err := attempts.FailuresSince(ctx, h.ID, base.Add(-15*time.Minute))
	if err != nil {
		t.Fatalf("FailuresSince: %v", err)
	}
	if len(failures) != 2 {
		t.Fatalf("len = %d, want 2 — the 40-minute-old failure is outside the window", len(failures))
	}

	if err := attempts.ClearFailures(ctx, h.ID); err != nil {
		t.Fatalf("ClearFailures: %v", err)
	}
	failures, _ = attempts.FailuresSince(ctx, h.ID, base.Add(-time.Hour))
	if len(failures) != 0 {
		t.Fatalf("len = %d, want 0 after clearing", len(failures))
	}
}

func TestSpaceRepoListsInPositionOrder(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	h, _ := postgres.NewHouseholdRepo(db).Create(ctx, "H", "H")
	spaces := postgres.NewSpaceRepo(db)

	for _, s := range domain.BuiltinSpaces(h.ID) {
		if _, err := spaces.Create(ctx, s); err != nil {
			t.Fatalf("Create %s: %v", s.Key, err)
		}
	}

	listed, err := spaces.List(ctx, h.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 3 || listed[0].Key != "money" || listed[2].Key != "family" {
		t.Fatalf("listed = %+v", listed)
	}

	next, err := spaces.NextPosition(ctx, h.ID)
	if err != nil {
		t.Fatalf("NextPosition: %v", err)
	}
	if next != 4 {
		t.Fatalf("NextPosition = %d, want 4", next)
	}
}

// TestSpaceRepoRejectsADuplicateKeyWithErrAlreadyExists proves the database's
// own UNIQUE (household_id, key) constraint -- the backstop for the race
// usecase.HouseholdService.CreateSpace's list-then-compare pre-check cannot
// close on its own -- surfaces as domain.ErrAlreadyExists rather than an
// opaque wrapped driver error. translate's pgconn.PgError/23505 case is what
// this exercises.
func TestSpaceRepoRejectsADuplicateKeyWithErrAlreadyExists(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	h, _ := postgres.NewHouseholdRepo(db).Create(ctx, "H", "H")
	spaces := postgres.NewSpaceRepo(db)

	first := domain.Space{HouseholdID: h.ID, Key: "movie-night", Name: "Movie Night",
		Visibility: domain.VisibilityEveryone, Position: 1}
	if _, err := spaces.Create(ctx, first); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	second := domain.Space{HouseholdID: h.ID, Key: "movie-night", Name: "Movie Night (again)",
		Visibility: domain.VisibilityEveryone, Position: 2}
	if _, err := spaces.Create(ctx, second); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("err = %v, want domain.ErrAlreadyExists", err)
	}
}
