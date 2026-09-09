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

func TestTelegramLinkRequestCarriesItsUserThroughRedemption(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewTelegramLinkRepo(db)
	user, err := postgres.NewUserRepo(db).Create(ctx, "link-1@hearth.family", "", "Andreas")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	id, err := repo.Create(ctx, user.ID, []byte("hash-1"), time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	if id == "" {
		t.Fatal("Create() returned an empty id")
	}
	got, err := repo.Consume(ctx, []byte("hash-1"), 8801, "andreas")
	if err != nil {
		t.Fatalf("Consume() = %v, want nil", err)
	}
	if got.UserID != user.ID {
		t.Fatalf("UserID = %q, want %q", got.UserID, user.ID)
	}
	if got.ID != id {
		t.Fatalf("Consume() ID = %q, want the id Create() returned (%q)", got.ID, id)
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

	if _, err := repo.Create(ctx, "", []byte("hash-2"), time.Now().Add(10*time.Minute)); err != nil {
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

func TestBindingRefusesASecondChatForTheSameUserAndASecondUserForTheSameChat(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewTelegramAccountRepo(db)
	users := postgres.NewUserRepo(db)
	alice, err := users.Create(ctx, "alice@hearth.family", "", "Alice")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := users.Create(ctx, "bob@hearth.family", "", "Bob")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	first := usecase.TelegramBinding{UserID: alice.ID, ChatID: 9001, ChatUsername: "alice"}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	// One chat per user: the same person cannot accumulate phones, or a
	// disconnect would miss one.
	second := usecase.TelegramBinding{UserID: alice.ID, ChatID: 9002}
	if err := repo.Create(ctx, second); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("second chat for the same user = %v, want domain.ErrAlreadyExists", err)
	}
	// One user per chat: two accounts on one chat would make a sign-in
	// ambiguous.
	stolen := usecase.TelegramBinding{UserID: bob.ID, ChatID: 9001}
	if err := repo.Create(ctx, stolen); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("second user for the same chat = %v, want domain.ErrAlreadyExists", err)
	}

	got, err := repo.ByUserID(ctx, alice.ID)
	if err != nil {
		t.Fatalf("ByUserID() = %v, want nil", err)
	}
	if got.ChatID != 9001 || got.ChatUsername != "alice" || got.LinkedAt.IsZero() {
		t.Fatalf("binding = %+v, want chat 9001 (alice) with a linked_at", got)
	}

	if err := repo.Delete(ctx, alice.ID); err != nil {
		t.Fatalf("Delete() = %v, want nil", err)
	}
	if _, err := repo.ByUserID(ctx, alice.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ByUserID after Delete = %v, want domain.ErrNotFound", err)
	}
	// The chat is free again: a disconnect that left the chat claimed would
	// make relinking the same phone impossible.
	if err := repo.Create(ctx, usecase.TelegramBinding{UserID: bob.ID, ChatID: 9001}); err != nil {
		t.Fatalf("rebinding a freed chat = %v, want nil", err)
	}
}
