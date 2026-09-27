package domain

import "time"

// TokenState is what a single-use, expiring token can be. It exists so the
// three token flows in this codebase -- invites, magic links and sign-ups --
// share one answer to "is this still usable", rather than three.
type TokenState int

const (
	TokenLive TokenState = iota
	TokenConsumed
	TokenExpired
)

func (s TokenState) String() string {
	switch s {
	case TokenLive:
		return "live"
	case TokenConsumed:
		return "consumed"
	case TokenExpired:
		return "expired"
	default:
		return "unknown"
	}
}

// TokenLifecycle reports whether a token is still usable, and if not, why.
//
// Consumed is checked before expired: a used token must still report
// consumed, or the holder is sent chasing a replacement for an account
// they already have. Invites and sign-up both call this, so neither can
// get the order backwards.
//
// consumedAt is a pointer because "not consumed" is the absence of a
// timestamp, matching the nullable column it is read from.
func TokenLifecycle(now, expiresAt time.Time, consumedAt *time.Time) TokenState {
	if consumedAt != nil {
		return TokenConsumed
	}
	if !expiresAt.After(now) {
		return TokenExpired
	}
	return TokenLive
}
