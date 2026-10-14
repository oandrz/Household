package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type scheduleFixture struct {
	now     time.Time
	prunes  int
	digests int
	failing bool
	s       *schedule
}

func newScheduleFixture(withDigest bool) *scheduleFixture {
	f := &scheduleFixture{now: time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)}
	f.s = &schedule{
		now: func() time.Time { return f.now },
		prune: func(context.Context) error {
			f.prunes++
			if f.failing {
				return errors.New("database unavailable")
			}
			return nil
		},
	}
	if withDigest {
		f.s.digest = func(context.Context) { f.digests++ }
	}
	return f
}

// TestTheScheduledPruneRunsWithTheDigestOff is the reason the schedule
// always starts: a box with no Telegram and no digest still prunes, on the
// first tick after start-up.
func TestTheScheduledPruneRunsWithTheDigestOff(t *testing.T) {
	f := newScheduleFixture(false)

	f.s.tick(context.Background())

	if f.prunes != 1 {
		t.Fatalf("prunes after the first tick = %d, want 1", f.prunes)
	}
}

func TestThePruneRunsOnceADay(t *testing.T) {
	f := newScheduleFixture(true)
	ctx := context.Background()

	f.s.tick(ctx)
	f.now = f.now.Add(pruneEvery - tickEvery)
	f.s.tick(ctx)
	if f.prunes != 1 {
		t.Fatalf("prunes within a day = %d, want 1", f.prunes)
	}
	f.now = f.now.Add(tickEvery)
	f.s.tick(ctx)
	if f.prunes != 2 {
		t.Fatalf("prunes after a day = %d, want 2", f.prunes)
	}
	if f.digests != 3 {
		t.Fatalf("digest checks = %d, want one per tick, 3", f.digests)
	}
}

// TestAFailedPruneIsTriedAgainOnTheNextTick: a prune that failed has not
// pruned anything, so it must not wait a day.
func TestAFailedPruneIsTriedAgainOnTheNextTick(t *testing.T) {
	f := newScheduleFixture(false)
	ctx := context.Background()
	f.failing = true

	f.s.tick(ctx)
	f.failing = false
	f.now = f.now.Add(tickEvery)
	f.s.tick(ctx)
	f.now = f.now.Add(tickEvery)
	f.s.tick(ctx)

	if f.prunes != 2 {
		t.Fatalf("prunes = %d, want 2: the failure, then the retry that succeeded, then none", f.prunes)
	}
}

// TestAPanickingPruneDoesNotStopTheDigest: one job's panic costs that job
// one tick, never the other job or the clock.
func TestAPanickingPruneDoesNotStopTheDigest(t *testing.T) {
	f := newScheduleFixture(true)
	f.s.prune = func(context.Context) error { panic("boom") }

	f.s.tick(context.Background())

	if f.digests != 1 {
		t.Fatalf("digest checks after a panicking prune = %d, want 1", f.digests)
	}
}
