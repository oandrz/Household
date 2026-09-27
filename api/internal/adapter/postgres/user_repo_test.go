package postgres_test

import (
	"context"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// TestUserRepoStoresAnEmptyEmailAsNull proves the "" <-> SQL NULL
// convention StoredUser's doc comment gives for Email and PasswordHash.
// users.email is citext UNIQUE and nullable, so storing "" instead of NULL
// for a credential-less child would collide with a second one on the
// unique index -- multiple NULLs never collide. This only passes if
// UserRepo.Create uses nullableText, not text, for Email.
func TestUserRepoStoresAnEmptyEmailAsNull(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	users := postgres.NewUserRepo(db)

	ethan, err := users.Create(ctx, "", "", "Ethan")
	if err != nil {
		t.Fatalf("create first credential-less child: %v", err)
	}
	if _, err := users.Create(ctx, "", "", "Mia"); err != nil {
		t.Fatalf("create second credential-less child: %v — email must be stored as NULL, not \"\"", err)
	}

	found, err := users.ByID(ctx, ethan.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if found.Email != "" {
		t.Fatalf("Email = %q, want \"\" — NULL must come back as the empty string", found.Email)
	}
	if found.PasswordHash != "" {
		t.Fatalf("PasswordHash = %q, want \"\"", found.PasswordHash)
	}

	if err := users.SetPasswordHash(ctx, ethan.ID, "$argon2id$new"); err != nil {
		t.Fatalf("SetPasswordHash: %v", err)
	}
	found, err = users.ByID(ctx, ethan.ID)
	if err != nil {
		t.Fatalf("ByID after SetPasswordHash: %v", err)
	}
	if found.PasswordHash != "$argon2id$new" {
		t.Fatalf("PasswordHash = %q, want $argon2id$new", found.PasswordHash)
	}

	if err := users.SetPasswordHash(ctx, ethan.ID, ""); err != nil {
		t.Fatalf("SetPasswordHash back to empty: %v", err)
	}
	found, err = users.ByID(ctx, ethan.ID)
	if err != nil {
		t.Fatalf("ByID after clearing password hash: %v", err)
	}
	if found.PasswordHash != "" {
		t.Fatalf("PasswordHash = %q, want \"\" after clearing — SetPasswordHash must also store NULL for \"\"", found.PasswordHash)
	}
}

// TestUserRepoCreateWithMembershipCreatesBothRowsAtomically is the happy
// path: one call creates exactly one user and one membership tying that
// user to the household with the given role and capabilities.
func TestUserRepoCreateWithMembershipCreatesBothRowsAtomically(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	members := postgres.NewMembershipRepo(db)

	h, err := households.Create(ctx, domain.Household{
		Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}

	m := domain.Membership{
		HouseholdID:  h.ID,
		Role:         domain.RoleLimited,
		Capabilities: domain.Capabilities{domain.CapChores},
	}

	user, membership, err := users.CreateWithMembership(ctx, "", "", "Baby", m)
	if err != nil {
		t.Fatalf("CreateWithMembership: %v", err)
	}
	if user.ID == "" || user.Email != "" || user.DisplayName != "Baby" {
		t.Fatalf("user = %+v", user)
	}
	if membership.ID == "" || membership.UserID != user.ID || membership.HouseholdID != h.ID ||
		membership.Role != domain.RoleLimited || len(membership.Capabilities) != 1 ||
		membership.Capabilities[0] != domain.CapChores {
		t.Fatalf("membership = %+v", membership)
	}

	found, err := users.ByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if found.Email != "" || found.PasswordHash != "" {
		t.Fatalf("found user = %+v, want empty email and password hash", found)
	}

	byUser, err := members.ByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("ByUser: %v", err)
	}
	if byUser.ID != membership.ID {
		t.Fatalf("membership.ID = %q, ByUser found %q", membership.ID, byUser.ID)
	}
}

// TestUserRepoCreateWithMembershipRollsBackOnMembershipConstraintViolation
// proves the point of CreateWithMembership's transaction (mirroring
// TestInviteAcceptRollsBackOnMembershipConstraintViolation in
// invite_repo_test.go): a failure partway through -- forced here by
// owners_hold_all_capabilities -- must leave no trace, not an orphaned user
// row with a NULL email and no membership (see CreateWithMembership's own
// doc comment for why that NULL email matters).
func TestUserRepoCreateWithMembershipRollsBackOnMembershipConstraintViolation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)

	h, err := households.Create(ctx, domain.Household{
		Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}

	// An owner must hold every capability; this set holds only one, so
	// CreateMembership's INSERT violates owners_hold_all_capabilities.
	m := domain.Membership{
		HouseholdID:  h.ID,
		Role:         domain.RoleOwner,
		Capabilities: domain.Capabilities{domain.CapMoney},
	}

	if _, _, err := users.CreateWithMembership(ctx, "", "", "Violator", m); err == nil {
		t.Fatal("expected the database constraint to reject an owner with a partial capability set")
	}

	if got := countUsersByDisplayName(t, db, "Violator"); got != 0 {
		t.Fatalf("users named Violator after a rolled-back CreateWithMembership = %d, want 0", got)
	}
}

// countUsersByDisplayName exists because CreateWithMembership's child case
// has a NULL email, which countUsersByEmail (invite_repo_test.go) cannot
// match against -- display name is the one caller-supplied identifier that
// survives a NULL email.
func countUsersByDisplayName(t *testing.T, db *postgres.DB, displayName string) int {
	t.Helper()
	var count int
	if err := db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM users WHERE display_name = $1`, displayName).Scan(&count); err != nil {
		t.Fatalf("count users by display name: %v", err)
	}
	return count
}
