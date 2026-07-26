package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
)

// TestFailuresSinceForEmailIsolatesByAddress proves the one method ports.go
// singles out as security-relevant: it must return only the failures for the
// requested email, only the ones inside the window, and only the
// unsuccessful ones -- the same three axes TestLoginAttemptsRespectTheWindow
// already covers for FailuresSince, but here scoped by address rather than
// by household, since this is the path sign-in uses for an email that
// matches no user (so a stranger's countdown looks identical to a member's).
func TestFailuresSinceForEmailIsolatesByAddress(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	attempts := postgres.NewLoginAttemptRepo(db)

	base := time.Now().UTC()

	// Two failures for the target address inside the window, one outside it.
	if err := attempts.Record(ctx, nil, nil, "stranger@example.com", false, base.Add(-40*time.Minute)); err != nil {
		t.Fatalf("Record (outside window): %v", err)
	}
	if err := attempts.Record(ctx, nil, nil, "stranger@example.com", false, base.Add(-5*time.Minute)); err != nil {
		t.Fatalf("Record (inside window): %v", err)
	}
	if err := attempts.Record(ctx, nil, nil, "stranger@example.com", false, base.Add(-1*time.Minute)); err != nil {
		t.Fatalf("Record (inside window): %v", err)
	}
	// A successful attempt for the same address, inside the window -- must
	// not be counted as a failure.
	if err := attempts.Record(ctx, nil, nil, "stranger@example.com", true, base.Add(-2*time.Minute)); err != nil {
		t.Fatalf("Record (succeeded): %v", err)
	}
	// Failures for a different address entirely, inside the window -- must
	// not leak into the target address's count.
	for _, offset := range []time.Duration{-3 * time.Minute, -2 * time.Minute} {
		if err := attempts.Record(ctx, nil, nil, "someone-else@example.com", false, base.Add(offset)); err != nil {
			t.Fatalf("Record (other address): %v", err)
		}
	}

	failures, err := attempts.FailuresSinceForEmail(ctx, "stranger@example.com", base.Add(-15*time.Minute))
	if err != nil {
		t.Fatalf("FailuresSinceForEmail: %v", err)
	}
	if len(failures) != 2 {
		t.Fatalf("len = %d, want 2 — the 40-minute-old failure is outside the window, the "+
			"successful attempt must not count, and the other address's failures must not leak in", len(failures))
	}
}
