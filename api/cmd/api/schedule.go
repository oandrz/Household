package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// tickEvery is how often the schedule wakes. Each job decides for itself
// whether its time has come, so a restart delays a job by at most this.
const tickEvery = 15 * time.Minute

// pruneEvery is how often the retention prune runs once it has succeeded.
const pruneEvery = 24 * time.Hour

// schedule is the API's one clock (ADR 9). It always runs: the retention
// prune needs it whether or not Telegram is configured, and a prune that
// only ran when the digest was on would silently never run on a box without
// one. The digest rides the same clock when it is configured.
type schedule struct {
	now func() time.Time
	// prune runs on the first tick and then every pruneEvery. A failed run
	// is tried again on the next tick.
	prune func(ctx context.Context) error
	// digest runs on every tick when set; it decides for itself whether
	// today's send is due. Nil when the digest is off.
	digest func(ctx context.Context)

	lastPrune time.Time
}

func (s *schedule) run(ctx context.Context) {
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for {
		s.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// tick runs each job that is due. A panic in one job costs that job one
// tick, never the clock or the other job.
func (s *schedule) tick(ctx context.Context) {
	recovering("retention prune", func() { s.pruneIfDue(ctx) })
	if s.digest != nil {
		recovering("daily digest", func() { s.digest(ctx) })
	}
}

func (s *schedule) pruneIfDue(ctx context.Context) {
	now := s.now()
	if !s.lastPrune.IsZero() && now.Sub(s.lastPrune) < pruneEvery {
		return
	}
	if err := s.prune(ctx); err != nil {
		slog.Error("retention prune failed", "error", err)
		return
	}
	s.lastPrune = now
}

func recovering(job string, run func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("scheduled job panicked", "job", job, "panic", r)
		}
	}()
	run()
}

// pruneRetention is the schedule's prune job: RetentionService with the
// product's window, and a log line saying what went.
func pruneRetention(retention *usecase.RetentionService) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		counts, err := retention.Prune(ctx, usecase.RetentionWindow)
		slog.Info("retention prune",
			"signups", counts.Signups, "login_attempts", counts.LoginAttempts,
			"telegram_link_requests", counts.TelegramLinks, "magic_links", counts.MagicLinks,
			"expired_invites", counts.Invites)
		return err
	}
}
