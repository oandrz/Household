package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestMagicLinkLifecycle(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	users := postgres.NewUserRepo(db)
	links := postgres.NewMagicLinkRepo(db)

	u, err := users.Create(ctx, "andreas@hearth.family", "", "Andreas")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	hash := []byte("magiclinkhashmagiclinkhash012345")
	expiry := time.Now().Add(15 * time.Minute)
	if err := links.Create(ctx, u.ID, hash, expiry); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// CountRecentMagicLinks joins on u.email = $1; if the user's email had
	// been stored as NULL rather than the real address, this would silently
	// come back 0 instead of erroring.
	count, err := links.CountSince(ctx, "andreas@hearth.family", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CountSince: %v", err)
	}
	if count != 1 {
		t.Fatalf("CountSince = %d, want 1", count)
	}

	userID, err := links.Consume(ctx, hash)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if userID != u.ID {
		t.Fatalf("Consume returned %q, want %q", userID, u.ID)
	}

	if _, err := links.Consume(ctx, hash); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("re-consuming a used token: got %v, want domain.ErrNotFound", err)
	}
}
