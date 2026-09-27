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
		// The comparison is !expiresAt.After(now): equal counts as expired.
		if got := TokenLifecycle(now, now, nil); got != TokenExpired {
			t.Fatalf("got %v, want TokenExpired", got)
		}
	})

	t.Run("consumed beats expired", func(t *testing.T) {
		// expiresAt and consumedAt are both "past" on purpose: this pins that
		// consumed outranks expired.
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
