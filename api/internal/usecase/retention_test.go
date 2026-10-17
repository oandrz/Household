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

// recordingPruner remembers the cutoff it was handed and answers a fixed
// count or error.
type recordingPruner struct {
	before time.Time
	called bool
	rows   int64
	err    error
}

func (p *recordingPruner) Prune(_ context.Context, before time.Time) (int64, error) {
	p.called, p.before = true, before
	return p.rows, p.err
}

type retentionFixture struct {
	signups, attempts, links, magic, invites, tokens, sessions *recordingPruner
	clock                                                      *fixedClock
	svc                                                        *usecase.RetentionService
}

func newRetentionFixture() *retentionFixture {
	f := &retentionFixture{
		signups: &recordingPruner{rows: 1}, attempts: &recordingPruner{rows: 2},
		links: &recordingPruner{rows: 3}, magic: &recordingPruner{rows: 4}, invites: &recordingPruner{rows: 5},
		tokens: &recordingPruner{rows: 6}, sessions: &recordingPruner{rows: 7},
		clock: &fixedClock{now: time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)},
	}
	f.svc = usecase.NewRetentionService(usecase.RetentionDeps{
		Signups: f.signups, LoginAttempts: f.attempts, TelegramLinks: f.links,
		MagicLinks: f.magic, Invites: f.invites, APITokens: f.tokens, Sessions: f.sessions, Clock: f.clock,
	})
	return f
}

func (f *retentionFixture) all() map[string]*recordingPruner {
	return map[string]*recordingPruner{"signups": f.signups, "login attempts": f.attempts,
		"telegram link requests": f.links, "magic links": f.magic, "invites": f.invites,
		"api tokens": f.tokens, "sessions": f.sessions}
}

func TestPruneCleansEveryTableWithTheSameCutoff(t *testing.T) {
	f := newRetentionFixture()

	counts, err := f.svc.Prune(context.Background(), usecase.RetentionWindow)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	want := f.clock.now.Add(-usecase.RetentionWindow)
	for name, p := range f.all() {
		if !p.called || !p.before.Equal(want) {
			t.Errorf("%s: called %v with cutoff %v, want %v", name, p.called, p.before, want)
		}
	}
	if counts != (usecase.PruneCounts{Signups: 1, LoginAttempts: 2, TelegramLinks: 3, MagicLinks: 4, Invites: 5,
		APITokens: 6, Sessions: 7}) {
		t.Fatalf("counts = %+v, want each table's own count in its own field", counts)
	}
}

// TestPruneRefusesAWindowInsideTheFloor: a window short enough to reach into
// the lockout window would clear a live lockout, so it is refused before any
// table is touched.
func TestPruneRefusesAWindowInsideTheFloor(t *testing.T) {
	f := newRetentionFixture()

	_, err := f.svc.Prune(context.Background(), usecase.RetentionFloor-time.Hour)
	if !errors.Is(err, usecase.ErrRetentionWindowTooShort) {
		t.Fatalf("err = %v, want ErrRetentionWindowTooShort", err)
	}
	for name, p := range f.all() {
		if p.called {
			t.Errorf("%s was pruned despite the refusal", name)
		}
	}
}

// TestOneFailingTableDoesNotStopTheOthers: each delete stands alone, so one
// broken table must not leave the rest growing.
func TestOneFailingTableDoesNotStopTheOthers(t *testing.T) {
	f := newRetentionFixture()
	f.attempts.err = errors.New("connection reset")

	counts, err := f.svc.Prune(context.Background(), usecase.RetentionWindow)
	if err == nil || !strings.Contains(err.Error(), "login attempts") {
		t.Fatalf("err = %v, want it to name the table that failed", err)
	}
	for name, p := range f.all() {
		if !p.called {
			t.Errorf("%s was skipped after another table failed", name)
		}
	}
	if counts.Invites != 5 || counts.LoginAttempts != 0 {
		t.Fatalf("counts = %+v, want the others' counts and none for the failed table", counts)
	}
}

// TestTheLockoutPolicyFitsTheEarlierFailuresCap: repositories return at most
// MaxEarlierFailures earlier failures. A policy that locks after more than
// that would never see its own limit reached.
func TestTheLockoutPolicyFitsTheEarlierFailuresCap(t *testing.T) {
	if got := domain.DefaultLockoutPolicy().MaxAttempts; got > usecase.MaxEarlierFailures {
		t.Fatalf("DefaultLockoutPolicy().MaxAttempts = %d, above MaxEarlierFailures = %d -- the lock could never engage",
			got, usecase.MaxEarlierFailures)
	}
}
