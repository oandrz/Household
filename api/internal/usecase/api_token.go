package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

type APITokenDeps struct {
	Tokens APITokenRepository
	Gen    TokenGenerator
	Clock  Clock
}

// APITokenService creates, lists and revokes a member's own tokens. It has
// no say in what a token may do: a token authenticates exactly as the
// member's session would, and the HTTP layer's guards decide the rest
// (docs/superpowers/specs/2026-09-08-hearth-api-tokens-design.md).
type APITokenService struct{ d APITokenDeps }

func NewAPITokenService(d APITokenDeps) *APITokenService { return &APITokenService{d: d} }

// NewAPITokenInput is what a create needs. Lifetime zero means the default;
// the caller does not have to know what that is.
type NewAPITokenInput struct {
	UserID      string
	HouseholdID string
	Name        string
	Lifetime    time.Duration
}

// CreatedAPIToken is the one and only place the raw secret exists outside
// the caller's hands. Raw is never stored and never logged.
type CreatedAPIToken struct {
	Token domain.APIToken
	Raw   string
}

// prefixLength is how much of the raw secret a listing shows. Eight
// characters of 43 identify a token to its owner and give an attacker
// nothing useful.
const prefixLength = 8

func (s *APITokenService) Create(ctx context.Context, in NewAPITokenInput) (CreatedAPIToken, error) {
	name, err := domain.ValidateAPITokenName(in.Name)
	if err != nil {
		return CreatedAPIToken{}, err
	}
	lifetime := in.Lifetime
	if lifetime == 0 {
		lifetime = domain.DefaultAPITokenLifetime
	}
	if err := domain.ValidateAPITokenLifetime(lifetime); err != nil {
		return CreatedAPIToken{}, err
	}
	secret, _, err := s.d.Gen.NewToken()
	if err != nil {
		return CreatedAPIToken{}, err
	}
	raw := domain.APITokenPrefix + secret
	// The hash covers the whole raw string, prefix included, so what the
	// middleware hashes off the wire is exactly what was stored.
	hash := s.d.Gen.HashToken(raw)
	now := s.d.Clock.Now()
	created, err := s.d.Tokens.Create(ctx, hash, secret[:min(prefixLength, len(secret))], domain.APIToken{
		UserID:      in.UserID,
		HouseholdID: in.HouseholdID,
		Name:        name,
		ExpiresAt:   now.Add(lifetime),
	})
	if err != nil {
		return CreatedAPIToken{}, err
	}
	return CreatedAPIToken{Token: created, Raw: raw}, nil
}

func (s *APITokenService) List(ctx context.Context, userID string) ([]domain.APIToken, error) {
	return s.d.Tokens.ListForUser(ctx, userID)
}

// Revoke is user-scoped by the repository's contract: a member can only
// ever revoke their own.
func (s *APITokenService) Revoke(ctx context.Context, userID, tokenID string) error {
	return s.d.Tokens.Revoke(ctx, userID, tokenID)
}
