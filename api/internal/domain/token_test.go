package domain

import (
	"testing"
	"time"
)

func TestTokenLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	t.Run("live when unconsumed and unexpired", func(t *testing.T) {
		if got := TokenLifecycle(now, future, nil); got != TokenLive {
			t.Fatalf("got %v, want TokenLive", got)
		}
	})

	t.Run("expired at exactly the expiry instant", func(t *testing.T) {
		// Not After(now) is the rule checkInviteLive already used: a token
		// whose expiry is exactly now is spent, not live.
		if got := TokenLifecycle(now, now, nil); got != TokenExpired {
			t.Fatalf("got %v, want TokenExpired", got)
		}
	})

	t.Run("consumed beats expired", func(t *testing.T) {
		// This ordering is the whole reason this function exists. An invite
		// that was accepted and has since passed its expiry must report
		// accepted -- telling the holder "expired, ask for another" would
		// send them chasing a second invite for an account they already have.
		if got := TokenLifecycle(now, past, &past); got != TokenConsumed {
			t.Fatalf("got %v, want TokenConsumed", got)
		}
	})

	t.Run("consumed while still inside its window", func(t *testing.T) {
		if got := TokenLifecycle(now, future, &past); got != TokenConsumed {
			t.Fatalf("got %v, want TokenConsumed", got)
		}
	})
}
