package httpadapter

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
