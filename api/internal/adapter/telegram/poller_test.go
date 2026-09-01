package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

type handlerSpy struct {
	mu    sync.Mutex
	calls []StartCommand
	err   error
	panic bool
}

func (h *handlerSpy) HandleStart(_ context.Context, chatID int64, payload string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, StartCommand{ChatID: chatID, Payload: payload})
	if h.panic {
		panic("handler exploded")
	}
	return h.err
}

func (h *handlerSpy) seen() []StartCommand {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]StartCommand(nil), h.calls...)
}

// The mock delivers its two updates exactly once and then answers with an
// empty result, the same way TestPollerAdvancesTheOffsetPastIgnoredUpdates's
// mock does. A mock that redelivers the same updates on every request (as
// real Telegram never does once the offset has moved past them) would make
// the poller's tight retry loop dispatch far faster than a 5ms-granularity
// wait can ever catch at exactly one call -- that shape was tried and failed
// deterministically, not flakily, three runs in a row.
func TestPollerDispatchesStartCommands(t *testing.T) {
	var mu sync.Mutex
	delivered := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		first := !delivered
		delivered = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !first {
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[
			{"update_id":11,"message":{"text":"/start nonce-a","chat":{"id":501}}},
			{"update_id":12,"message":{"text":"chatter","chat":{"id":501}}}]}`))
	}))
	defer srv.Close()

	spy := &handlerSpy{}
	p := NewPoller(newClientWithBase("t", srv.URL), spy)
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx)
	defer cancel()

	waitFor(t, func() bool { return len(spy.seen()) == 1 })

	got := spy.seen()
	if len(got) != 1 {
		t.Fatalf("dispatched %d times, want 1 -- the non-/start update must be ignored", len(got))
	}
	if got[0].Payload != "nonce-a" || got[0].ChatID != 501 {
		t.Fatalf("dispatched %+v, want chat 501 payload nonce-a", got[0])
	}
}

// The offset must advance past every update returned, including the ones that
// were ignored -- otherwise Telegram redelivers the ignored update forever and
// the loop never makes progress.
func TestPollerAdvancesTheOffsetPastIgnoredUpdates(t *testing.T) {
	var mu sync.Mutex
	var offsets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Offset int64 `json:"offset"`
		}
		_ = decodeJSON(r, &body)
		mu.Lock()
		offsets = append(offsets, itoa(body.Offset))
		first := len(offsets) == 1
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if first {
			_, _ = w.Write([]byte(`{"ok":true,"result":[
				{"update_id":30,"message":{"text":"/help","chat":{"id":9}}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer srv.Close()

	p := NewPoller(newClientWithBase("t", srv.URL), &handlerSpy{})
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx)
	defer cancel()
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(offsets) >= 2 })

	mu.Lock()
	defer mu.Unlock()
	if offsets[1] != "31" {
		t.Fatalf("second offset = %s, want 31", offsets[1])
	}
}

// The poller is a bare goroutine; chi's recoverer does not cover it. A panic
// in the handler must not take the process down.
//
// The mock delivers once and then answers empty (see
// TestPollerDispatchesStartCommands for why): otherwise the panicking handler
// fires dozens of times before cancel takes effect, which is noisy and proves
// nothing beyond what one dispatch already proves.
func TestPollerSurvivesAPanickingHandler(t *testing.T) {
	var mu sync.Mutex
	delivered := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		first := !delivered
		delivered = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !first {
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[
			{"update_id":40,"message":{"text":"/start boom","chat":{"id":7}}}]}`))
	}))
	defer srv.Close()

	spy := &handlerSpy{panic: true}
	p := NewPoller(newClientWithBase("t", srv.URL), spy)
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx) // must not crash the test binary
	defer cancel()
	waitFor(t, func() bool { return len(spy.seen()) >= 1 })
}

func TestPollerBacksOffAndKeepsGoingAfterAnError(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			_, _ = w.Write([]byte(`{"ok":false,"description":"nope"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer srv.Close()

	p := NewPoller(newClientWithBase("t", srv.URL), &handlerSpy{})
	p.baseBackoff = time.Millisecond // keep the test fast
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx)
	defer cancel()
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return calls >= 2 })
}

func TestPollerStopsWhenTheContextIsCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer srv.Close()

	p := NewPoller(newClientWithBase("t", srv.URL), &handlerSpy{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of cancellation")
	}
}

// waitFor polls cond every 5ms until it is true, failing the test if 2s pass
// without it. The poller runs on its own goroutine, so tests must wait for
// its effects rather than asserting immediately after starting it.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}

// decodeJSON reads a request body into v. Tests use it to inspect the offset
// the poller sent, without duplicating json.NewDecoder boilerplate at every
// call site.
func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return errors.New("request has no body")
	}
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(v)
}

// itoa formats an int64 the same way strconv.Itoa formats an int, without the
// truncation risk of converting through int on a 32-bit platform.
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
