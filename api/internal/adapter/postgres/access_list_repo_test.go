package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// Household access list (docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md).
// These two listers are the only queries that read another member's
// credentials, so the household boundary is what they are tested for.

type accessFixture struct {
	db                 *postgres.DB
	householdID        string
	ownerID            string
	partnerID          string
	strangerID         string
	strangersHousehold string
}

func seedAccessFixture(t *testing.T) accessFixture {
	t.Helper()
	ctx := context.Background()
	db := openTestDB(t)
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	members := postgres.NewMembershipRepo(db)

	newHousehold := func(name string) domain.Household {
		h, err := households.Create(ctx, domain.Household{
			Name: name, FamilyName: "Test",
			PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
		})
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		return h
	}
	newOwner := func(h domain.Household, email, name string) string {
		u, err := users.Create(ctx, email, "", name)
		if err != nil {
			t.Fatalf("create user %s: %v", email, err)
		}
		if _, err := members.Create(ctx, domain.Membership{
			HouseholdID: h.ID, UserID: u.ID, Role: domain.RoleOwner,
			Capabilities: domain.AllCapabilities(),
		}); err != nil {
			t.Fatalf("create membership %s: %v", email, err)
		}
		return u.ID
	}

	ours := newHousehold("Ours")
	theirs := newHousehold("Theirs")
	return accessFixture{
		db:                 db,
		householdID:        ours.ID,
		ownerID:            newOwner(ours, "owner@example.com", "Owner"),
		partnerID:          newOwner(ours, "partner@example.com", "Partner"),
		strangerID:         newOwner(theirs, "stranger@example.com", "Stranger"),
		strangersHousehold: theirs.ID,
	}
}

func mustToken(t *testing.T, repo *postgres.APITokenRepo, userID, householdID, name string, expiresAt time.Time) domain.APIToken {
	t.Helper()
	tok, err := repo.Create(context.Background(), []byte("hash-"+name), "pfx-"+name, domain.APIToken{
		UserID: userID, HouseholdID: householdID, Name: name, ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("create token %s: %v", name, err)
	}
	return tok
}

func tokenNames(tokens []domain.APIToken) []string {
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		out = append(out, tok.Name)
	}
	return out
}

func TestListTokensForHouseholdShowsEveryMembersLiveTokensAndNoOtherHouseholds(t *testing.T) {
	f := seedAccessFixture(t)
	repo := postgres.NewAPITokenRepo(f.db)
	week := time.Now().Add(7 * 24 * time.Hour)

	mustToken(t, repo, f.ownerID, f.householdID, "owner-laptop", week)
	mustToken(t, repo, f.partnerID, f.householdID, "partner-script", week)
	mustToken(t, repo, f.strangerID, f.strangersHousehold, "stranger-token", week)

	got, err := repo.ListForHousehold(context.Background(), f.householdID)
	if err != nil {
		t.Fatalf("ListForHousehold() = %v", err)
	}
	names := tokenNames(got)
	if len(names) != 2 || names[0] != "partner-script" || names[1] != "owner-laptop" {
		t.Fatalf("ListForHousehold() names = %v, want [partner-script owner-laptop] (newest first, ours only)", names)
	}
}

func TestListTokensForHouseholdLeavesOutRevokedAndExpiredTokens(t *testing.T) {
	f := seedAccessFixture(t)
	ctx := context.Background()
	repo := postgres.NewAPITokenRepo(f.db)

	mustToken(t, repo, f.ownerID, f.householdID, "live", time.Now().Add(time.Hour))
	mustToken(t, repo, f.ownerID, f.householdID, "expired", time.Now().Add(-time.Minute))
	revoked := mustToken(t, repo, f.ownerID, f.householdID, "revoked", time.Now().Add(time.Hour))
	if err := repo.Revoke(ctx, f.ownerID, revoked.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	got, err := repo.ListForHousehold(ctx, f.householdID)
	if err != nil {
		t.Fatalf("ListForHousehold() = %v", err)
	}
	if names := tokenNames(got); len(names) != 1 || names[0] != "live" {
		t.Fatalf("ListForHousehold() names = %v, want [live]", names)
	}
}

func TestListTokensForHouseholdWithNoneIsAnEmptySliceNotAnError(t *testing.T) {
	f := seedAccessFixture(t)
	got, err := postgres.NewAPITokenRepo(f.db).ListForHousehold(context.Background(), f.householdID)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("ListForHousehold() = %#v, %v; want empty non-nil slice, nil", got, err)
	}
}

func TestListChatsForHouseholdShowsOnlyThisHouseholdsChats(t *testing.T) {
	f := seedAccessFixture(t)
	ctx := context.Background()
	repo := postgres.NewTelegramAccountRepo(f.db)

	for _, b := range []usecase.TelegramBinding{
		{UserID: f.ownerID, ChatID: 1001, ChatUsername: "owner_tg"},
		{UserID: f.partnerID, ChatID: 1002},
		{UserID: f.strangerID, ChatID: 1003, ChatUsername: "stranger_tg"},
	} {
		if err := repo.Create(ctx, b); err != nil {
			t.Fatalf("bind %s: %v", b.UserID, err)
		}
	}

	got, err := repo.ListForHousehold(ctx, f.householdID)
	if err != nil {
		t.Fatalf("ListForHousehold() = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListForHousehold() = %+v, want the owner's and the partner's chats only", got)
	}
	byUser := map[string]usecase.TelegramBinding{}
	for _, b := range got {
		byUser[b.UserID] = b
	}
	if byUser[f.ownerID].ChatUsername != "owner_tg" {
		t.Fatalf("owner's chat = %+v", byUser[f.ownerID])
	}
	if b, ok := byUser[f.partnerID]; !ok || b.ChatUsername != "" || b.LinkedAt.IsZero() {
		t.Fatalf("partner's chat = %+v, want present, no username, a linked-at time", b)
	}
	if _, ok := byUser[f.strangerID]; ok {
		t.Fatal("another household's chat is listed")
	}
}

func TestListChatsForHouseholdWithNoneIsAnEmptySliceNotAnError(t *testing.T) {
	f := seedAccessFixture(t)
	got, err := postgres.NewTelegramAccountRepo(f.db).ListForHousehold(context.Background(), f.householdID)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("ListForHousehold() = %#v, %v; want empty non-nil slice, nil", got, err)
	}
}
