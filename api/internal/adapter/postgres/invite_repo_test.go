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

	h, err := households.Create(ctx, "Andreas & Christine", "Oentoro")
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
