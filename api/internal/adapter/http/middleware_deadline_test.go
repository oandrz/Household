package httpadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// captureLog sends slog's default logger to a buffer for one test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not the error envelope: %q", rec.Body.String())
	}
	return body.Error.Code
}

func TestARequestPastItsDeadlineAnswers503Timeout(t *testing.T) {
	logs := captureLog(t)
	h := withRequestDeadline(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		logAndWriteInternal(w, r, fmt.Errorf("list accounts: %w", r.Context().Err()))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != "TIMEOUT" {
		t.Fatalf("got %d %s, want 503 TIMEOUT", rec.Code, rec.Body.String())
	}
	if strings.Contains(logs.String(), "unhandled error") || !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("a timed-out request must log at warn, not as an unhandled error; log:\n%s", logs)
	}
}

func TestACallerWhoLeftIsNotLoggedAsAnError(t *testing.T) {
	logs := captureLog(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	logAndWriteInternal(httptest.NewRecorder(), r, fmt.Errorf("hash password: %w", context.Canceled))

	if strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "level=INFO") {
		t.Fatalf("a caller who left must log at info; log:\n%s", logs)
	}
}

// The mapping follows the request's own context, not the error's shape: an
// unrelated timeout inside a live request is still a defect someone must see.
func TestATimeoutInsideALiveRequestIsStillA500(t *testing.T) {
	logs := captureLog(t)
	rec := httptest.NewRecorder()
	logAndWriteInternal(rec, httptest.NewRequest(http.MethodGet, "/", nil),
		fmt.Errorf("outbox: %w", context.DeadlineExceeded))
	if rec.Code != http.StatusInternalServerError || errorCode(t, rec) != "INTERNAL" {
		t.Fatalf("got %d, want 500 INTERNAL", rec.Code)
	}
	if !strings.Contains(logs.String(), "unhandled error") {
		t.Fatalf("want the unhandled-error log; log:\n%s", logs)
	}
}

func TestTheRequestDeadlineMiddlewareSetsRequestDeadline(t *testing.T) {
	var deadline time.Time
	var ok bool
	h := withRequestDeadline(RequestDeadline)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		deadline, ok = r.Context().Deadline()
	}))
	start := time.Now()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if got := deadline.Sub(start); !ok || got > RequestDeadline+time.Second || got < RequestDeadline-time.Second {
		t.Fatalf("deadline = %v from start (set %v), want about %v", got, ok, RequestDeadline)
	}
}
