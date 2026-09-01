package telegram

import (
	"context"
	"log/slog"
	"time"
)

// StartHandler is what the poller hands a parsed /start to. It is declared
// here, in the adapter, rather than imported from usecase, so this package
// depends on a shape rather than on a concrete service.
type StartHandler interface {
	HandleStart(ctx context.Context, chatID int64, payload string) error
}

const (
	// pollTimeout is Telegram's server-side long-poll wait. Long, because a
	// short one is just a busy loop against someone else's API.
	pollTimeout = 50 * time.Second
	// maxBackoff caps the retry delay after an error. Telegram being down for
	// an hour must not become an hour-long sleep that outlives the outage.
	maxBackoff = 60 * time.Second
)

// Poller long-polls Telegram for updates and dispatches /start commands.
//
// Exactly one process may run this. Telegram hands each update to a single
// getUpdates caller, so a second replica would silently steal updates and the
// symptom would be "sign-in works about half the time". True on one box today;
// this comment is here because the constraint is invisible until it bites.
//
// The offset is held in memory. After a restart Telegram redelivers updates it
// was never acknowledged for, so a /start can be processed twice. That is safe
// because the nonce was already consumed: the second pass takes the
// already-consumed branch and the bot says the link expired. Recorded rather
// than left to luck.
type Poller struct {
	client      *Client
	handler     StartHandler
	offset      int64
	baseBackoff time.Duration
}

func NewPoller(c *Client, h StartHandler) *Poller {
	return &Poller{client: c, handler: h, baseBackoff: time.Second}
}

// Run blocks until ctx is cancelled. It never returns on error: the API must
// keep serving HTTP while Telegram is unreachable, so a failure backs off and
// tries again rather than ending the loop.
func (p *Poller) Run(ctx context.Context) {
	backoff := p.baseBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		updates, err := p.client.GetUpdates(ctx, p.offset, pollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("telegram getUpdates failed", "error", err, "retry_in", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = p.baseBackoff

		for _, u := range updates {
			// Advance past every update, including ignored ones. Leaving the
			// offset behind an update we chose not to act on makes Telegram
			// redeliver it forever and the loop never progresses.
			if u.UpdateID >= p.offset {
				p.offset = u.UpdateID + 1
			}
			start, ok := ParseStart(u)
			if !ok {
				continue
			}
			p.dispatch(ctx, start)
		}
	}
}

// dispatch recovers, because this runs on a bare goroutine that chi's
// middleware.Recoverer does not cover: an unrecovered panic here would take
// down the whole process and every unrelated in-flight request, not just this
// one update. Same reasoning as sendMagicLinkAsync's recover.
func (p *Poller) dispatch(ctx context.Context, start StartCommand) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("telegram start handler panicked", "panic", r)
		}
	}()
	if err := p.handler.HandleStart(ctx, start.ChatID, start.Payload); err != nil {
		slog.Error("telegram start handler failed", "error", err)
	}
}
