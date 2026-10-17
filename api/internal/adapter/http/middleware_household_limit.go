package httpadapter

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// HouseholdLimits are the two per-household request limits. The bucket
// bounds how many requests a household may send over time; the in-flight
// cap bounds how many of them run at once, which is what decides how many
// database connections one household can hold. ADR 14 says why both.
type HouseholdLimits struct {
	// PerSecond is how fast the bucket refills; Burst is how many requests
	// it holds.
	PerSecond float64
	Burst     int
	// InFlight is how many of one household's requests run at once.
	// MaxWaiting more may wait for a slot, each for at most MaxWait, so a
	// page that fires many calls together runs in small batches instead of
	// being refused.
	InFlight   int
	MaxWaiting int
	MaxWait    time.Duration
}

// ProductionHouseholdLimits are the numbers main.go runs with.
//
//   - Burst 60: the home screen fires 11 calls at once, so 60 covers about
//     five quick reloads in a row.
//   - PerSecond 10: the heaviest read on capped data (the account list at
//     100,000 transactions) costs about 20 ms of database time, so one
//     household's steady stream of them stays near a fifth of the box.
//   - InFlight 4: the request pool has postgres.RequestPoolMaxConns (10)
//     connections, and one household holds at most 4 of them.
//   - MaxWaiting 16 and MaxWait 5 s: long enough for any page's calls to
//     run in turn, short enough to sit well inside RequestDeadline.
//
// TestProductionHouseholdLimitsFitWhatTheyProtect ties these to the
// numbers they must fit around.
var ProductionHouseholdLimits = HouseholdLimits{
	PerSecond: 10, Burst: 60, InFlight: 4, MaxWaiting: 16, MaxWait: 5 * time.Second,
}

// householdSweepEvery is how often households with nothing to remember are
// dropped, which bounds memory to the households active in the last minute.
const householdSweepEvery = time.Minute

// HouseholdLimiter enforces HouseholdLimits per household.
//
// IT IS IN-MEMORY AND THEREFORE PER-PROCESS, like ipRateLimiter: a restart
// resets every household, and a second API container would double each
// household's limits. Move to a shared counter before running two.
type HouseholdLimiter struct {
	limits HouseholdLimits
	now    func() time.Time

	mu         sync.Mutex
	households map[string]*householdBudget
	lastSweep  time.Time
}

type householdBudget struct {
	tokens     float64
	refilledAt time.Time
	// slots holds one entry per running request. Sending takes a slot,
	// receiving gives it back; a channel makes "wait for a free slot, or
	// give up" a plain select.
	slots    chan struct{}
	waiting  int
	loggedAt time.Time
}

// NewHouseholdLimiter does not call now itself, for the same reason
// newIPRateLimiter does not: routers built with a deliberately incomplete
// Deps must not panic at construction.
func NewHouseholdLimiter(limits HouseholdLimits, now func() time.Time) *HouseholdLimiter {
	return &HouseholdLimiter{limits: limits, now: now, households: map[string]*householdBudget{}}
}

// admit spends one token and takes a running slot for householdID, waiting
// in line for a slot if all are busy. On success it returns release, which
// the caller must call when the request finishes (calling it twice is
// harmless). Otherwise release is nil and retryAfter says how long to wait.
//
// The token is spent before waiting: a request refused by the line was
// still sent, and counting it keeps a household that floods the line from
// also keeping its whole allowance.
func (l *HouseholdLimiter) admit(ctx context.Context, householdID string) (release func(), retryAfter time.Duration) {
	l.mu.Lock()
	now := l.now()
	l.sweep(now)
	b := l.budget(householdID, now)
	l.refill(b, now)
	if b.tokens < 1 {
		l.mu.Unlock()
		return nil, time.Duration((1 - b.tokens) / l.limits.PerSecond * float64(time.Second))
	}
	b.tokens--

	select {
	case b.slots <- struct{}{}:
		l.mu.Unlock()
		return releaseOnce(b), 0
	default:
	}
	if b.waiting >= l.limits.MaxWaiting {
		l.mu.Unlock()
		return nil, time.Second
	}
	// While waiting > 0 the sweep keeps b, so the wait below needs no lock.
	b.waiting++
	l.mu.Unlock()

	timer := time.NewTimer(l.limits.MaxWait)
	defer timer.Stop()
	admitted := false
	select {
	case b.slots <- struct{}{}:
		admitted = true
	case <-timer.C:
	case <-ctx.Done():
	}

	l.mu.Lock()
	b.waiting--
	l.mu.Unlock()
	if !admitted {
		return nil, time.Second
	}
	return releaseOnce(b), 0
}

func releaseOnce(b *householdBudget) func() {
	var once sync.Once
	return func() { once.Do(func() { <-b.slots }) }
}

// budget returns householdID's state, creating it with a full bucket. Call
// with l.mu held.
func (l *HouseholdLimiter) budget(householdID string, now time.Time) *householdBudget {
	b, ok := l.households[householdID]
	if !ok {
		b = &householdBudget{
			tokens:     float64(l.limits.Burst),
			refilledAt: now,
			slots:      make(chan struct{}, l.limits.InFlight),
		}
		l.households[householdID] = b
	}
	return b
}

// refill adds the tokens earned since the last refill. A clock that stepped
// backwards earns nothing, and refilling restarts from the new time so the
// household is not frozen until the clock catches up. Call with l.mu held.
func (l *HouseholdLimiter) refill(b *householdBudget, now time.Time) {
	elapsed := now.Sub(b.refilledAt)
	b.refilledAt = now
	if elapsed <= 0 {
		return
	}
	b.tokens = math.Min(float64(l.limits.Burst), b.tokens+elapsed.Seconds()*l.limits.PerSecond)
}

// sweep drops households with a full bucket and nothing running or waiting:
// a dropped household comes back with a full bucket, which is exactly what
// it had. One with a request running is kept, or it would get fresh slots.
// Call with l.mu held.
func (l *HouseholdLimiter) sweep(now time.Time) {
	if since := now.Sub(l.lastSweep); since >= 0 && since < householdSweepEvery {
		return
	}
	l.lastSweep = now
	full := time.Duration(float64(l.limits.Burst) / l.limits.PerSecond * float64(time.Second))
	for id, b := range l.households {
		if b.waiting == 0 && len(b.slots) == 0 && now.Sub(b.refilledAt) >= full {
			delete(l.households, id)
		}
	}
}

// refusalWorthLogging reports whether to log this refusal: at most once a
// minute per household, so a flood cannot fill the log.
func (l *HouseholdLimiter) refusalWorthLogging(householdID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.budget(householdID, now)
	if !b.loggedAt.IsZero() && now.Sub(b.loggedAt) < time.Minute {
		return false
	}
	b.loggedAt = now
	return true
}

// retryAfterSeconds is the Retry-After value for a wait: whole seconds,
// rounded up, never 0 (0 would invite an immediate retry).
func retryAfterSeconds(d time.Duration) int {
	return max(1, int(math.Ceil(d.Seconds())))
}

const householdRateLimitedMessage = "This household is sending too many requests. Wait a few seconds and try again."

// limitHousehold applies the household limits. It sits immediately after
// requireSession in every signed-in group: after it, so a caller with no
// session gets 401 and never learns a route exists from a 429; before every
// other guard, because a request that will be refused still cost
// requireSession's reads.
//
// A nil limiter fails closed. Routers built for tests that are not about
// limits pass limits that never bind instead.
func limitHousehold(l *HouseholdLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := RequestScope(r)
			if l == nil || !ok {
				logAndWriteInternal(w, r, errors.New("household limit: no limiter in Deps, or not placed after requireSession"))
				return
			}
			release, retryAfter := l.admit(r.Context(), scope.HouseholdID)
			if release == nil {
				if l.refusalWorthLogging(scope.HouseholdID) {
					slog.Warn("household request limit reached",
						"household_id", scope.HouseholdID, "request_id", middleware.GetReqID(r.Context()))
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(retryAfter)))
				WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", householdRateLimitedMessage, nil)
				return
			}
			defer release()
			next.ServeHTTP(w, r)
		})
	}
}
