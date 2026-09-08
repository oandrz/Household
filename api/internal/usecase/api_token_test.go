package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func apiTokenFixture(t *testing.T) (*usecase.APITokenService, *apiTokenDouble, *fixedClock) {
	t.Helper()
	clock := &fixedClock{now: time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC)}
	repo := newAPITokenDouble()
	repo.clock = clock
	svc := usecase.NewAPITokenService(usecase.APITokenDeps{Tokens: repo, Gen: &seqTokens{}, Clock: clock})
	return svc, repo, clock
}

func TestCreateTokenShowsTheRawSecretOnceAndStoresOnlyItsHash(t *testing.T) {
	svc, repo, clock := apiTokenFixture(t)
	created, err := svc.Create(context.Background(), usecase.NewAPITokenInput{UserID: "u1", HouseholdID: "h1", Name: "  laptop cron "})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Raw, domain.APITokenPrefix) {
		t.Fatalf("raw token %q must start with %q", created.Raw, domain.APITokenPrefix)
	}
	if created.Token.Name != "laptop cron" {
		t.Fatalf("name not trimmed: %q", created.Token.Name)
	}
	if want := clock.Now().Add(domain.DefaultAPITokenLifetime); !created.Token.ExpiresAt.Equal(want) {
		t.Fatalf("expiry %v, want the 90-day default %v", created.Token.ExpiresAt, want)
	}
	if created.Token.Prefix == "" || !strings.HasPrefix(strings.TrimPrefix(created.Raw, domain.APITokenPrefix), created.Token.Prefix) || len(created.Token.Prefix) > 8 {
		t.Fatalf("prefix %q should be the first (up to 8) characters of the secret", created.Token.Prefix)
	}
	for hash := range repo.rows {
		if hash == created.Raw {
			t.Fatalf("the raw token must never be stored")
		}
	}
	if _, err := repo.ByTokenHash(context.Background(), (&seqTokens{}).HashToken(created.Raw)); err != nil {
		t.Fatalf("the stored hash must be the hash of the whole raw string: %v", err)
	}
}

func TestCreateTokenRefusesABlankNameAndABadLifetime(t *testing.T) {
	svc, repo, _ := apiTokenFixture(t)
	if _, err := svc.Create(context.Background(), usecase.NewAPITokenInput{UserID: "u1", HouseholdID: "h1", Name: "   "}); !errors.Is(err, domain.ErrAPITokenNameInvalid) {
		t.Fatalf("blank name: %v", err)
	}
	if _, err := svc.Create(context.Background(), usecase.NewAPITokenInput{UserID: "u1", HouseholdID: "h1", Name: "x", Lifetime: 366 * 24 * time.Hour}); !errors.Is(err, domain.ErrAPITokenLifetimeInvalid) {
		t.Fatalf("366 days: %v", err)
	}
	if _, err := svc.Create(context.Background(), usecase.NewAPITokenInput{UserID: "u1", HouseholdID: "h1", Name: "x", Lifetime: -time.Hour}); !errors.Is(err, domain.ErrAPITokenLifetimeInvalid) {
		t.Fatalf("negative: %v", err)
	}
	if len(repo.rows) != 0 {
		t.Fatalf("a refused create must store nothing")
	}
}

func TestRevokeIsScopedToTheOwner(t *testing.T) {
	svc, repo, _ := apiTokenFixture(t)
	created, _ := svc.Create(context.Background(), usecase.NewAPITokenInput{UserID: "u1", HouseholdID: "h1", Name: "mine"})
	if err := svc.Revoke(context.Background(), "u2", created.Token.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another user revoking must be not-found, got %v", err)
	}
	if repo.liveCount("u1") != 1 {
		t.Fatalf("token was revoked by a stranger")
	}
	if err := svc.Revoke(context.Background(), "u1", created.Token.ID); err != nil {
		t.Fatal(err)
	}
	if repo.liveCount("u1") != 0 {
		t.Fatalf("owner's revoke did not take")
	}
}

func TestAnExpiredTokenIsUnfindable(t *testing.T) {
	svc, repo, clock := apiTokenFixture(t)
	created, _ := svc.Create(context.Background(), usecase.NewAPITokenInput{UserID: "u1", HouseholdID: "h1", Name: "short", Lifetime: time.Hour})
	hash := (&seqTokens{}).HashToken(created.Raw)
	if _, err := repo.ByTokenHash(context.Background(), hash); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Hour)
	if _, err := repo.ByTokenHash(context.Background(), hash); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expired token must be not-found, got %v", err)
	}
}

func TestRemovingAMemberRevokesTheirTokensToo(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.apiTokens.Create(ctx, []byte("h-ethan"), "ethanxxx", domain.APIToken{UserID: f.ethanID, HouseholdID: f.householdID, Name: "ethan", ExpiresAt: f.clock.Now().Add(time.Hour)})
	f.apiTokens.Create(ctx, []byte("h-andreas"), "andreasx", domain.APIToken{UserID: f.andreasID, HouseholdID: f.householdID, Name: "andreas", ExpiresAt: f.clock.Now().Add(time.Hour)})

	if err := f.memberSvc.Remove(ctx, f.householdID, "membership-ethan"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := f.apiTokens.liveCount(f.ethanID); got != 0 {
		t.Fatalf("live tokens for Ethan = %d, want 0", got)
	}
	if got := f.apiTokens.liveCount(f.andreasID); got != 1 {
		t.Fatalf("live tokens for Andreas = %d, want 1 (untouched)", got)
	}
}
