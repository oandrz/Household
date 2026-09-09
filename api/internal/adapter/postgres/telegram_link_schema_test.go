package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
)

func TestTelegramLinkRequestCarriesItsUserThroughRedemption(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewTelegramLinkRepo(db)
	user, err := postgres.NewUserRepo(db).Create(ctx, "link-1@hearth.family", "", "Andreas")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := repo.Create(ctx, user.ID, []byte("hash-1"), time.Now().Add(10*time.Minute)); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	got, err := repo.Consume(ctx, []byte("hash-1"), 8801, "andreas")
	if err != nil {
		t.Fatalf("Consume() = %v, want nil", err)
	}
	if got.UserID != user.ID {
		t.Fatalf("UserID = %q, want %q", got.UserID, user.ID)
	}

	row, err := repo.ByID(ctx, got.ID)
	if err != nil {
		t.Fatalf("ByID() = %v, want nil", err)
	}
	// Consumed, carrying a user and a chat, is the pending state the browser
	// polls for -- there is no status column, so this is the whole of it.
	if !row.Consumed || row.ChatID != 8801 || row.ChatUsername != "andreas" {
		t.Fatalf("row = %+v, want consumed by chat 8801 (andreas)", row)
	}
}

func TestSignInNonceCarriesNoUser(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewTelegramLinkRepo(db)

	if err := repo.Create(ctx, "", []byte("hash-2"), time.Now().Add(10*time.Minute)); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	got, err := repo.Consume(ctx, []byte("hash-2"), 8802, "")
	if err != nil {
		t.Fatalf("Consume() = %v, want nil", err)
	}
	// "" and not the zero UUID: HandleStart branches on this being empty.
	if got.UserID != "" {
		t.Fatalf("UserID = %q, want empty", got.UserID)
	}
}
