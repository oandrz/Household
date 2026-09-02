package httpadapter_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// lastSeen reads the session row behind a cookie, through the same
// repository the router uses -- not through any route, since the property
// under test is what a route does to the row.
func lastSeen(t *testing.T, env *testEnv, session *http.Cookie) *time.Time {
	t.Helper()
	record, err := env.deps.Sessions.ByTokenHash(context.Background(), env.deps.Tokens.HashToken(session.Value))
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	return record.LastSeenAt
}

// Anchored on real now, for the reason movableClock's own comment gives:
// GetLiveSession's WHERE clause uses Postgres's now(), so a clock pinned to
// a past date signs in against wall time and expires one SessionTTL later.
func touchClock() *movableClock {
	return &movableClock{now: time.Now().UTC().Truncate(time.Second)}
}

func TestAFreshSessionIsTouchedOnItsFirstRequest(t *testing.T) {
	clk := touchClock()
	env := newTestEnvWithClock(t, clk)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	if got := lastSeen(t, env, session); got != nil {
		t.Fatalf("sign-in alone set last_seen_at = %v; only an authenticated request should", got)
	}

	rec := env.authedGet(t, "/api/v1/auth/me", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /me: %d %s", rec.Code, rec.Body.String())
	}
	got := lastSeen(t, env, session)
	if got == nil || !got.Equal(clk.now) {
		t.Fatalf("last_seen_at after first request = %v, want %v", got, clk.now)
	}
}

func TestASessionTouchedTenMinutesAgoIsNotTouchedAgain(t *testing.T) {
	clk := touchClock()
	env := newTestEnvWithClock(t, clk)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authedGet(t, "/api/v1/auth/me", session)
	first := lastSeen(t, env, session)

	clk.Advance(10 * time.Minute)
	env.authedGet(t, "/api/v1/auth/me", session)

	second := lastSeen(t, env, session)
	if second == nil || !second.Equal(*first) {
		t.Fatalf("a request ten minutes after a touch moved last_seen_at from %v to %v; the throttle is an hour", first, second)
	}
}

func TestASessionTouchedOverAnHourAgoIsTouchedAgain(t *testing.T) {
	clk := touchClock()
	env := newTestEnvWithClock(t, clk)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authedGet(t, "/api/v1/auth/me", session)

	clk.Advance(61 * time.Minute)
	env.authedGet(t, "/api/v1/auth/me", session)

	got := lastSeen(t, env, session)
	if got == nil || !got.Equal(clk.now) {
		t.Fatalf("last_seen_at after an hour = %v, want %v", got, clk.now)
	}
}

// touchFailingSessions is the real repository with Touch broken, the same
// swap-one-port seam routerWithMemberships uses.
type touchFailingSessions struct{ usecase.SessionRepository }

var errTouchFailed = errors.New("touch failed")

func (touchFailingSessions) Touch(context.Context, []byte, time.Time) error { return errTouchFailed }

func TestATouchFailureDoesNotFailTheRequest(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	d := env.deps
	d.Sessions = touchFailingSessions{env.deps.Sessions}
	router := httpadapter.NewRouter(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("a failed touch answered %d; a usage timestamp must never fail a request", rec.Code)
	}
}
