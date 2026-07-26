package postgres_test

import (
	"context"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
)

// TestUserRepoStoresAnEmptyEmailAsNull proves the "" <-> SQL NULL convention
// documented for PasswordHash also holds for Email, which ports.go does not
// spell out explicitly but users.email being citext UNIQUE and nullable
// requires: if Create stored "" instead of NULL for a credential-less child,
// a second such child would collide on the unique index. Multiple NULLs
// never collide, so this only passes if nullableText (not text) is used for
// Email in UserRepo.Create.
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
