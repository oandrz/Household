package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestTelegramLinkConsumeIsSingleUse(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewTelegramLinkRepo(openTestDB(t))
	hash := []byte("nonce-hash-one")

	if err := repo.Create(ctx, hash, time.Now().Add(10*time.Minute)); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	if err := repo.Consume(ctx, hash, 4242); err != nil {
		t.Fatalf("first Consume() = %v, want nil", err)
	}
	if err := repo.Consume(ctx, hash, 4242); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second Consume() = %v, want domain.ErrNotFound", err)
	}
}

func TestTelegramLinkConsumeRefusesAnExpiredNonce(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewTelegramLinkRepo(openTestDB(t))
	hash := []byte("nonce-hash-expired")

	if err := repo.Create(ctx, hash, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	if err := repo.Consume(ctx, hash, 4242); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Consume() on an expired nonce = %v, want domain.ErrNotFound", err)
	}
}

// The count is what the per-chat rate limit reads, and it must see only rows
// this chat actually redeemed -- not every nonce ever minted.
func TestTelegramLinkCountsOnlyThisChatsRedemptions(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewTelegramLinkRepo(openTestDB(t))
	since := time.Now().Add(-time.Hour)

	for _, hash := range [][]byte{[]byte("c1-a"), []byte("c1-b")} {
		if err := repo.Create(ctx, hash, time.Now().Add(10*time.Minute)); err != nil {
			t.Fatalf("Create() = %v", err)
		}
		if err := repo.Consume(ctx, hash, 1111); err != nil {
			t.Fatalf("Consume() = %v", err)
		}
	}
	if err := repo.Create(ctx, []byte("c2-a"), time.Now().Add(10*time.Minute)); err != nil {
		t.Fatalf("Create() = %v", err)
	}
	if err := repo.Consume(ctx, []byte("c2-a"), 2222); err != nil {
		t.Fatalf("Consume() = %v", err)
	}

	got, err := repo.CountLinksSince(ctx, 1111, since)
	if err != nil {
		t.Fatalf("CountLinksSince() = %v, want nil", err)
	}
	if got != 2 {
		t.Fatalf("CountLinksSince(chat 1111) = %d, want 2", got)
	}
}

// TestTelegramLinkRepoPruneLeavesLiveRows mirrors
// TestSignupRepoPruneLeavesLiveRows exactly, for the same table shape: one
// row consumed (and therefore prunable once old), one row left live (never
// prunable no matter how old, because it might still be redeemed). Both are
// aged past the cutoff by the same direct UPDATE the signups test uses, so
// only consumed-ness decides the outcome, not age alone.
func TestTelegramLinkRepoPruneLeavesLiveRows(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	pool := db.Pool()
	repo := postgres.NewTelegramLinkRepo(db)

	liveHash := []byte("prune-live-hash")
	consumedHash := []byte("prune-consumed-hash")

	if err := repo.Create(ctx, liveHash, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("Create live: %v", err)
	}
	if err := repo.Create(ctx, consumedHash, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("Create consumed: %v", err)
	}
	if err := repo.Consume(ctx, consumedHash, 7777); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE telegram_link_requests SET created_at = now() - interval '40 days'`); err != nil {
		t.Fatalf("age the rows: %v", err)
	}

	deleted, err := repo.Prune(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("Prune deleted %d, want 1 -- only the consumed row", deleted)
	}

	// Assert directly against the table rather than through Consume: a
	// pruned row and an already-consumed-but-still-present row would both
	// answer domain.ErrNotFound from Consume, which would prove nothing
	// about whether the DELETE actually ran.
	var liveCount, consumedCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM telegram_link_requests WHERE nonce_hash = $1`, liveHash).Scan(&liveCount); err != nil {
		t.Fatalf("query live row: %v", err)
	}
	if liveCount != 1 {
		t.Fatalf("live row count = %d, want 1 -- the live row must survive Prune", liveCount)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM telegram_link_requests WHERE nonce_hash = $1`, consumedHash).Scan(&consumedCount); err != nil {
		t.Fatalf("query consumed row: %v", err)
	}
	if consumedCount != 0 {
		t.Fatalf("consumed row count = %d, want 0 -- Prune should have deleted it", consumedCount)
	}
}

func TestTelegramAccountByChatIDIsNotFoundWhenUnbound(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewTelegramAccountRepo(openTestDB(t))

	if _, err := repo.ByChatID(ctx, 999999); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ByChatID() on an unbound chat = %v, want domain.ErrNotFound", err)
	}
}

// TestTelegramAccountByChatIDResolvesTheBoundUser exercises the success path:
// TelegramAccountRepository deliberately has no Create (the binding is written
// inside SignupRepository.Provision's transaction, per its port doc comment),
// so the binding row is inserted directly, the way telegram_schema_test.go
// exercises the schema's own constraints.
func TestTelegramAccountByChatIDResolvesTheBoundUser(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	user, err := postgres.NewUserRepo(db).Create(ctx, "", "", "Kayla")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO telegram_accounts (user_id, chat_id) VALUES ($1, $2)`, user.ID, 5555); err != nil {
		t.Fatalf("insert telegram account: %v", err)
	}

	got, err := postgres.NewTelegramAccountRepo(db).ByChatID(ctx, 5555)
	if err != nil {
		t.Fatalf("ByChatID() = %v, want nil", err)
	}
	if got != user.ID {
		t.Fatalf("ByChatID() = %q, want %q", got, user.ID)
	}
}
