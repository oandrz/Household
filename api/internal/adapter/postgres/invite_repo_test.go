package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestInviteLifecycle(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	invites := postgres.NewInviteRepo(db)

	h, err := households.Create(ctx, domain.Household{
		Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	inviter, err := users.Create(ctx, "andreas@hearth.family", "hash", "Andreas")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	tokenHash := []byte("invitetokenhashinvitetokenhash01")
	expiry := time.Now().Add(72 * time.Hour)
	inviteID, err := invites.Create(ctx, h.ID, "kid@example.com", "Kid", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar}, tokenHash, inviter.ID, expiry)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if inviteID == "" {
		t.Fatal("Create did not return an id")
	}

	details, err := invites.ByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	if details.ID != inviteID || details.HouseholdID != h.ID || details.Email != "kid@example.com" ||
		details.Role != domain.RoleLimited || details.InviterName != "Andreas" || details.FamilyName != "Oentoro" {
		t.Fatalf("details = %+v", details)
	}
	if details.AcceptedAt != nil {
		t.Fatalf("AcceptedAt = %v, want nil before acceptance", details.AcceptedAt)
	}

	if err := invites.MarkAccepted(ctx, inviteID); err != nil {
		t.Fatalf("MarkAccepted: %v", err)
	}

	details, err = invites.ByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("ByTokenHash after accept: %v", err)
	}
	if details.AcceptedAt == nil {
		t.Fatal("AcceptedAt = nil, want non-nil after acceptance")
	}

	if err := invites.MarkAccepted(ctx, inviteID); !errors.Is(err, domain.ErrInviteAlreadyAccepted) {
		t.Fatalf("re-accepting an already-accepted invite: got %v, want domain.ErrInviteAlreadyAccepted", err)
	}
}

// TestInviteAcceptCreatesUserAndMembershipAtomically proves the happy path:
// one Accept call creates exactly one user, one membership tying that user
// to the household with the invite's role and capabilities, and stamps the
// invite's accepted_at -- all three, from one transaction.
func TestInviteAcceptCreatesUserAndMembershipAtomically(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	members := postgres.NewMembershipRepo(db)
	invites := postgres.NewInviteRepo(db)

	h, err := households.Create(ctx, domain.Household{
		Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	inviter, err := users.Create(ctx, "andreas@hearth.family", "hash", "Andreas")
	if err != nil {
		t.Fatalf("create inviter: %v", err)
	}

	tokenHash := []byte("accepthappytokenaccepthappytoke1")
	inviteID, err := invites.Create(ctx, h.ID, "kid@example.com", "Kid", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar}, tokenHash, inviter.ID, time.Now().Add(72*time.Hour))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	accepted, err := invites.Accept(ctx, inviteID, "kid@example.com", "$argon2id$kidhash", "Kid",
		h.ID, domain.RoleLimited, domain.Capabilities{domain.CapCalendar})
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if accepted.UserID == "" || accepted.MembershipID == "" || accepted.HouseholdID != h.ID {
		t.Fatalf("accepted = %+v", accepted)
	}

	if countUsersByEmail(t, db, "kid@example.com") != 1 {
		t.Fatal("Accept must create exactly one user")
	}

	membership, err := members.ByUser(ctx, accepted.UserID)
	if err != nil {
		t.Fatalf("ByUser: %v", err)
	}
	if membership.ID != accepted.MembershipID || membership.HouseholdID != h.ID ||
		membership.Role != domain.RoleLimited || len(membership.Capabilities) != 1 ||
		membership.Capabilities[0] != domain.CapCalendar {
		t.Fatalf("membership = %+v", membership)
	}

	details, err := invites.ByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	if details.AcceptedAt == nil {
		t.Fatal("AcceptedAt = nil, want non-nil after Accept")
	}
}

// TestInviteAcceptIsSingleUse simulates a second, concurrent-style acceptance
// of an invite that a first Accept call already consumed. It must fail with
// domain.ErrInviteAlreadyAccepted -- not a raw unique-constraint error from
// trying to create a second user at the same email -- and it must not leave
// a second user row behind.
func TestInviteAcceptIsSingleUse(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	invites := postgres.NewInviteRepo(db)

	h, err := households.Create(ctx, domain.Household{
		Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	inviter, err := users.Create(ctx, "andreas@hearth.family", "hash", "Andreas")
	if err != nil {
		t.Fatalf("create inviter: %v", err)
	}

	tokenHash := []byte("acceptsingleusetokenacceptsingl1")
	inviteID, err := invites.Create(ctx, h.ID, "kid@example.com", "Kid", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar}, tokenHash, inviter.ID, time.Now().Add(72*time.Hour))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := invites.Accept(ctx, inviteID, "kid@example.com", "$argon2id$kidhash", "Kid",
		h.ID, domain.RoleLimited, domain.Capabilities{domain.CapCalendar}); err != nil {
		t.Fatalf("first Accept: %v", err)
	}

	_, err = invites.Accept(ctx, inviteID, "kid@example.com", "$argon2id$kidhash", "Kid",
		h.ID, domain.RoleLimited, domain.Capabilities{domain.CapCalendar})
	if !errors.Is(err, domain.ErrInviteAlreadyAccepted) {
		t.Fatalf("second Accept: got %v, want domain.ErrInviteAlreadyAccepted", err)
	}

	if got := countUsersByEmail(t, db, "kid@example.com"); got != 1 {
		t.Fatalf("users with kid@example.com after two Accept calls = %d, want 1", got)
	}
}

// TestInviteAcceptRollsBackOnMembershipConstraintViolation is the test that
// proves the whole point of putting Accept in one transaction: a failure
// partway through -- forced here by a role/capability combination the
// owners_hold_all_capabilities check constraint rejects -- must leave no
// trace, not an orphaned user occupying the unique email index that a retry
// could never get past.
func TestInviteAcceptRollsBackOnMembershipConstraintViolation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	invites := postgres.NewInviteRepo(db)

	h, err := households.Create(ctx, domain.Household{
		Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	inviter, err := users.Create(ctx, "andreas@hearth.family", "hash", "Andreas")
	if err != nil {
		t.Fatalf("create inviter: %v", err)
	}

	tokenHash := []byte("acceptrollbacktokenacceptrollba1")
	inviteID, err := invites.Create(ctx, h.ID, "violator@example.com", "Violator", domain.RoleOwner,
		domain.Capabilities{domain.CapMoney}, tokenHash, inviter.ID, time.Now().Add(72*time.Hour))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// An owner must hold every capability; this set holds only one, so
	// CreateMembership's INSERT violates owners_hold_all_capabilities.
	_, err = invites.Accept(ctx, inviteID, "violator@example.com", "$argon2id$hash", "Violator",
		h.ID, domain.RoleOwner, domain.Capabilities{domain.CapMoney})
	if err == nil {
		t.Fatal("expected the database constraint to reject an owner with a partial capability set")
	}

	if got := countUsersByEmail(t, db, "violator@example.com"); got != 0 {
		t.Fatalf("users with violator@example.com after a rolled-back Accept = %d, want 0", got)
	}

	details, err := invites.ByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	if details.AcceptedAt != nil {
		t.Fatal("AcceptedAt must also be rolled back, not just the user insert")
	}
}

// TestLiveInviteForEmail proves LiveInviteForEmail's filter against a real
// Postgres query planner, not just the in-memory double: an unaccepted,
// unexpired invite is found; an accepted one and an expired one, for the
// same address, are not; and a live invite in a different household for the
// same address is not found either.
func TestLiveInviteForEmail(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	invites := postgres.NewInviteRepo(db)

	h, err := households.Create(ctx, domain.Household{
		Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	inviter, err := users.Create(ctx, "andreas@hearth.family", "hash", "Andreas")
	if err != nil {
		t.Fatalf("create inviter: %v", err)
	}

	if _, err := invites.LiveInviteForEmail(ctx, h.ID, "christine@hearth.family"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("before any invite exists: got %v, want domain.ErrNotFound", err)
	}

	liveHash := []byte("livetokenhashlivetokenhashlive01")
	liveID, err := invites.Create(ctx, h.ID, "christine@hearth.family", "Christine", domain.RoleOwner,
		domain.AllCapabilities(), liveHash, inviter.ID, time.Now().Add(72*time.Hour))
	if err != nil {
		t.Fatalf("Create (live): %v", err)
	}

	details, err := invites.LiveInviteForEmail(ctx, h.ID, "christine@hearth.family")
	if err != nil {
		t.Fatalf("LiveInviteForEmail: %v", err)
	}
	if details.ID != liveID || details.Role != domain.RoleOwner || details.Name != "Christine" ||
		details.InviterName != "Andreas" || details.FamilyName != "Oentoro" {
		t.Fatalf("details = %+v", details)
	}

	// A different household's invite for the same address must not surface.
	h2, err := households.Create(ctx, domain.Household{
		Name: "A Different Household", FamilyName: "Someone Else",
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
	})
	if err != nil {
		t.Fatalf("create second household: %v", err)
	}
	if _, err := invites.LiveInviteForEmail(ctx, h2.ID, "christine@hearth.family"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a different household's lookup: got %v, want domain.ErrNotFound", err)
	}

	if err := invites.MarkAccepted(ctx, liveID); err != nil {
		t.Fatalf("MarkAccepted: %v", err)
	}
	if _, err := invites.LiveInviteForEmail(ctx, h.ID, "christine@hearth.family"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("after acceptance: got %v, want domain.ErrNotFound", err)
	}

	expiredHash := []byte("expiredtokenhashexpiredtokenhas1")
	if _, err := invites.Create(ctx, h.ID, "kayla@example.com", "Kayla", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar}, expiredHash, inviter.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("Create (expired): %v", err)
	}
	if _, err := invites.LiveInviteForEmail(ctx, h.ID, "kayla@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("an expired invite: got %v, want domain.ErrNotFound", err)
	}
}

func countUsersByEmail(t *testing.T, db *postgres.DB, email string) int {
	t.Helper()
	var count int
	if err := db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM users WHERE email = $1`, email).Scan(&count); err != nil {
		t.Fatalf("count users by email: %v", err)
	}
	return count
}
