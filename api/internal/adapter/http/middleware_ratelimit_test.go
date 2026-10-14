package httpadapter

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestIPRateLimiterCountsPerIPAndResets(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	l := newIPRateLimiter(2, time.Minute, clock)

	if !l.allow("1.2.3.4") {
		t.Fatal("the first request must be allowed")
	}
	if !l.allow("1.2.3.4") {
		t.Fatal("the second request must be allowed")
	}
	if l.allow("1.2.3.4") {
		t.Fatal("the third request must be refused")
	}
	if !l.allow("5.6.7.8") {
		t.Fatal("a different IP has its own budget")
	}

	now = now.Add(time.Minute)
	if !l.allow("1.2.3.4") {
		t.Fatal("the window must reset")
	}
}

// Counting ports separately would make the limiter count nothing: a client's
// repeat requests arrive on different ephemeral ports.
func TestClientIPStripsThePort(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "203.0.113.7:54321"
	if got := clientIP(r); got != "203.0.113.7" {
		t.Fatalf("clientIP = %q, want 203.0.113.7", got)
	}
}

// TestSignUpRateLimitsCompose is the regression guard for a real defect:
// 10/hour per IP composed with a 200/day global ceiling meant a single IP,
// entirely within its own budget, could exhaust the global ceiling alone
// (10 * 24 = 240 > 200) -- after which every sign-up silently answered 202
// and mailed nothing for up to a day, for every address, with no one told.
// signUpRequestsPerIPPerHour must bind before usecase.SignupGlobalDailyLimit
// ever could, so this asserts the arithmetic directly rather than trusting
// two numbers in two files to stay in sync by coincidence.
//
// This lives in package httpadapter, not usecase: both constants are
// unexported in their own packages, so neither an external test package nor
// usecase itself (which cannot import httpadapter without a cycle) could
// reach both. signUpRequestsPerIPPerHour is already reachable here for
// free; SignupGlobalDailyLimit was exported from usecase specifically so
// this assertion has somewhere it can run -- see its own doc comment in
// usecase/signup.go.
func TestSignUpRateLimitsCompose(t *testing.T) {
	if got := signUpRequestsPerIPPerHour * 24; got >= usecase.SignupGlobalDailyLimit {
		t.Fatalf("signUpRequestsPerIPPerHour (%d) * 24 = %d, want < usecase.SignupGlobalDailyLimit (%d) -- "+
			"a single IP within its own hourly budget could exhaust the global daily ceiling alone",
			signUpRequestsPerIPPerHour, got, usecase.SignupGlobalDailyLimit)
	}
}

// TestHashRouteLimitsLeaveTheHasherRoomForEveryoneElse is the same kind of
// check as TestSignUpRateLimitsCompose (LEARNING pattern 11), for the
// password hasher. Three public routes run a derivation, each behind its own
// per-address limit, and the hasher behind them is shared by everyone: when
// its queue is full, every person signing in is told "busy".
//
// So one address, spending every allowance it has, must only be able to
// occupy a small share of the hasher. The sum is done against the worst
// case on purpose: the fewest slots any box gets, and a full second per
// derivation, several times what the default cost takes.
func TestHashRouteLimitsLeaveTheHasherRoomForEveryoneElse(t *testing.T) {
	const (
		pessimisticDerivation = time.Second
		largestShare          = 0.05
	)

	// Seconds of derivation one address may ask for, per second of clock.
	asked := 0.0
	for _, route := range []struct {
		limit  int
		window time.Duration
	}{
		{signInAttemptsPerIPPerWindow, signInLimitWindow},
		{signUpCompletionsPerIPPerWindow, passwordSetupLimitWindow},
		{inviteAcceptsPerIPPerWindow, passwordSetupLimitWindow},
	} {
		asked += float64(route.limit) * pessimisticDerivation.Seconds() / route.window.Seconds()
	}

	share := asked / float64(crypto.MinConcurrentDerivations)
	if share > largestShare {
		t.Fatalf("one address can occupy %.1f%% of the password hasher, want at most %.0f%% -- "+
			"the per-address limits on the hashing routes are too generous for a hasher of %d slots",
			share*100, largestShare*100, crypto.MinConcurrentDerivations)
	}
}

func TestRateLimitByIPAnswersTheStandardEnvelope(t *testing.T) {
	now := time.Now()
	l := newIPRateLimiter(1, time.Minute, func() time.Time { return now })
	h := rateLimitByIP(l)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i, wantCode := range []int{http.StatusOK, http.StatusTooManyRequests} {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "198.51.100.1:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != wantCode {
			t.Fatalf("request %d = %d, want %d", i, rec.Code, wantCode)
		}
	}
}
