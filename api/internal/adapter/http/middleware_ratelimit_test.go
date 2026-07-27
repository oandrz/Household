package httpadapter

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestIPRateLimiterCountsPerIPAndResets(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	l := newIPRateLimiter(2, time.Minute, clock)

	if !l.allow("1.2.3.4") || !l.allow("1.2.3.4") {
		t.Fatal("the first two requests must be allowed")
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

// TestSignUpRateLimitsCompose is the regression guard for the whole-branch
// review defect: 10/hour per IP composed with a 200/day global ceiling meant
// a single IP, entirely within its own budget, could exhaust the global
// ceiling alone (10 * 24 = 240 > 200) -- after which every sign-up silently
// answered 202 and mailed nothing for up to a day, for every address, with no
// one told. signUpRequestsPerIPPerHour must bind before
// usecase.SignupGlobalDailyLimit ever could, so this asserts the arithmetic
// directly rather than trusting two numbers in two files to stay in sync by
// coincidence.
//
// This lives here, in package httpadapter, not in package usecase: the two
// constants are unexported in their own packages, and Go's visibility rules
// mean an unexported identifier is invisible outside its defining package
// regardless of import direction -- an external test package for either one
// (httpadapter_test or usecase_test) could not reach the other's unexported
// constant, and usecase cannot import httpadapter without an import cycle
// (httpadapter already imports usecase). signUpRequestsPerIPPerHour is
// already reachable here for free, this file being internal to package
// httpadapter; SignupGlobalDailyLimit was exported from usecase specifically
// so this assertion has somewhere it can actually run, rather than being
// skipped -- see its own doc comment in usecase/signup.go.
func TestSignUpRateLimitsCompose(t *testing.T) {
	if got := signUpRequestsPerIPPerHour * 24; got >= usecase.SignupGlobalDailyLimit {
		t.Fatalf("signUpRequestsPerIPPerHour (%d) * 24 = %d, want < usecase.SignupGlobalDailyLimit (%d) -- "+
			"a single IP within its own hourly budget could exhaust the global daily ceiling alone",
			signUpRequestsPerIPPerHour, got, usecase.SignupGlobalDailyLimit)
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
