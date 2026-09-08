package domain

import (
	"strings"
	"time"
)

// APIToken is a member's own long-lived credential for scripts and agents.
// It names a user and a household like a session does, so a request it
// authenticates is scoped exactly as one from that member's browser.
//
// The raw secret never appears here: it is shown once at creation and only
// its hash is stored (see usecase.APITokenRepository).
type APIToken struct {
	ID          string
	UserID      string
	HouseholdID string
	Name        string
	// Prefix is the first few characters of the raw token, kept so a list
	// can tell tokens apart without holding the secret.
	Prefix     string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// APITokenPrefix opens every raw token, so a leaked one is recognisable in
// a log or a secret scanner as Hearth's.
const APITokenPrefix = "hearth_"

// Token lifetimes: a credential that lasts forever is one nobody rotates.
// 90 days is long enough for a laptop cron to be forgotten about and short
// enough that it eventually stops working; a year is the ceiling.
const (
	DefaultAPITokenLifetime = 90 * 24 * time.Hour
	MaxAPITokenLifetime     = 365 * 24 * time.Hour
	MaxAPITokenNameLength   = 80
)

// ValidateAPITokenName trims and refuses an empty or overlong name. The
// name is what a person sees in `token list`, so "" would be a row that
// cannot be told from its neighbours.
func ValidateAPITokenName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > MaxAPITokenNameLength {
		return "", ErrAPITokenNameInvalid
	}
	return name, nil
}

// ValidateAPITokenLifetime refuses anything outside (0, MaxAPITokenLifetime].
// Zero means "the default" to callers, and they substitute it before asking
// here; this function never guesses.
func ValidateAPITokenLifetime(d time.Duration) error {
	if d <= 0 || d > MaxAPITokenLifetime {
		return ErrAPITokenLifetimeInvalid
	}
	return nil
}
