package httpadapter

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeNow is a settable clock for the bucket. The wait line uses real
// timers, so tests that exercise waiting use a short MaxWait instead.
type fakeNow struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeNow() *fakeNow { return &fakeNow{t: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)} }

func (f *fakeNow) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeNow) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}

// smallLimits shows the mechanism at a size a test can count. The
// production numbers are checked separately, against the things they must
// fit around.
func smallLimits() HouseholdLimits {
	return HouseholdLimits{PerSecond: 10, Burst: 3, InFlight: 2, MaxWaiting: 1, MaxWait: 2 * time.Second}
}

func mustAdmit(t *testing.T, l *HouseholdLimiter, id string) func() {
	t.Helper()
	release, wait := l.admit(context.Background(), id)
	if release == nil {
		t.Fatalf("household %s was refused (retry after %v), want admitted", id, wait)
	}
	return release
}

func waitingIn(l *HouseholdLimiter, id string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b, ok := l.households[id]; ok {
		return b.waiting
	}
	return 0
}

func runningIn(l *HouseholdLimiter, id string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b, ok := l.households[id]; ok {
		return len(b.slots)
	}
	return 0
}

// eventually polls cond for up to a second: the waiter goroutine needs a
// moment to join the line, and a fixed sleep would be either flaky or slow.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTheBucketHoldsItsBurstAndRefillsAtItsRate(t *testing.T) {
	clk := newFakeNow()
	l := NewHouseholdLimiter(smallLimits(), clk.now)
	for range 3 {
		mustAdmit(t, l, "h1")()
	}
	release, wait := l.admit(context.Background(), "h1")
	if release != nil {
		t.Fatal("a fourth request with the bucket empty must be refused")
	}
	if wait != 100*time.Millisecond {
		t.Fatalf("retry after = %v, want 100ms (one token at 10/s)", wait)
	}

	clk.advance(100 * time.Millisecond)
	mustAdmit(t, l, "h1")()

	clk.advance(time.Hour)
	for range 3 {
		mustAdmit(t, l, "h1")()
	}
	if release, _ := l.admit(context.Background(), "h1"); release != nil {
		t.Fatal("an hour idle must refill to the burst size, not beyond it")
	}
}

func TestEachHouseholdHasItsOwnBucket(t *testing.T) {
	l := NewHouseholdLimiter(smallLimits(), newFakeNow().now)
	for range 3 {
		mustAdmit(t, l, "h1")()
	}
	mustAdmit(t, l, "h2")()
}

func TestARequestOverTheInFlightCapWaitsForASlot(t *testing.T) {
	l := NewHouseholdLimiter(smallLimits(), newFakeNow().now)
	first := mustAdmit(t, l, "h1")
	second := mustAdmit(t, l, "h1")

	admitted := make(chan func(), 1)
	go func() {
		release, _ := l.admit(context.Background(), "h1")
		admitted <- release
	}()
	eventually(t, "the third request to join the line", func() bool { return waitingIn(l, "h1") == 1 })

	first()
	select {
	case release := <-admitted:
		if release == nil {
			t.Fatal("the waiter must be admitted once a slot frees, not refused")
		}
		release()
	case <-time.After(time.Second):
		t.Fatal("the waiter was not admitted after a slot freed")
	}
	second()
}

func TestALineLongerThanMaxWaitingIsRefusedAtOnce(t *testing.T) {
	// A bucket big enough that the line, not the bucket, is what refuses.
	limits := smallLimits()
	limits.Burst = 10
	l := NewHouseholdLimiter(limits, newFakeNow().now)
	first := mustAdmit(t, l, "h1")
	second := mustAdmit(t, l, "h1")
	waiter := make(chan func(), 1)
	go func() {
		release, _ := l.admit(context.Background(), "h1") // fills the line of one
		waiter <- release
	}()
	eventually(t, "the line to fill", func() bool { return waitingIn(l, "h1") == 1 })

	start := time.Now()
	release, wait := l.admit(context.Background(), "h1")
	if release != nil {
		t.Fatal("a request that would make the line too long must be refused")
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("refusal took %v, want immediate: it must not wait in a full line", took)
	}
	if wait != time.Second {
		t.Fatalf("retry after = %v, want 1s", wait)
	}

	first()
	if release := <-waiter; release != nil {
		release()
	}
	second()
}

func TestAWaiterIsRefusedAfterMaxWait(t *testing.T) {
	limits := smallLimits()
	limits.MaxWait = 50 * time.Millisecond
	l := NewHouseholdLimiter(limits, newFakeNow().now)
	defer mustAdmit(t, l, "h1")()
	defer mustAdmit(t, l, "h1")()

	start := time.Now()
	release, _ := l.admit(context.Background(), "h1")
	if release != nil {
		t.Fatal("no slot freed, so the waiter must be refused")
	}
	if took := time.Since(start); took < 50*time.Millisecond {
		t.Fatalf("refused after %v, want at least MaxWait (50ms)", took)
	}
	if n := waitingIn(l, "h1"); n != 0 {
		t.Fatalf("waiting = %d after the waiter gave up, want 0", n)
	}
}

func TestAWaiterWhoseCallerLeftLeavesTheLine(t *testing.T) {
	l := NewHouseholdLimiter(smallLimits(), newFakeNow().now)
	first := mustAdmit(t, l, "h1")
	second := mustAdmit(t, l, "h1")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan func(), 1)
	go func() {
		release, _ := l.admit(ctx, "h1")
		done <- release
	}()
	eventually(t, "the waiter to join the line", func() bool { return waitingIn(l, "h1") == 1 })
	cancel()
	select {
	case release := <-done:
		if release != nil {
			t.Fatal("a waiter whose caller left must not be admitted")
		}
	case <-time.After(time.Second):
		t.Fatal("a waiter whose caller left kept waiting for a slot")
	}
	if n := waitingIn(l, "h1"); n != 0 {
		t.Fatalf("waiting = %d, want 0", n)
	}
	first()
	second()
	if n := runningIn(l, "h1"); n != 0 {
		t.Fatalf("running = %d after both released, want 0: a slot leaked", n)
	}
}

func TestReleasingTwiceFreesOneSlot(t *testing.T) {
	l := NewHouseholdLimiter(smallLimits(), newFakeNow().now)
	held := mustAdmit(t, l, "h1")
	twice := mustAdmit(t, l, "h1")
	twice()
	twice()
	if n := runningIn(l, "h1"); n != 1 {
		t.Fatalf("running = %d, want 1: a second release must not free someone else's slot", n)
	}
	held()
}

func TestIdleHouseholdsAreDroppedButBusyOnesKept(t *testing.T) {
	clk := newFakeNow()
	l := NewHouseholdLimiter(smallLimits(), clk.now)
	mustAdmit(t, l, "idle")()
	busy := mustAdmit(t, l, "busy")
	defer busy()

	clk.advance(2 * householdSweepEvery)
	mustAdmit(t, l, "someone-else")() // any admit may sweep

	l.mu.Lock()
	_, idleKept := l.households["idle"]
	_, busyKept := l.households["busy"]
	l.mu.Unlock()
	if idleKept {
		t.Error("an idle household with a full bucket must be dropped")
	}
	if !busyKept {
		t.Error("a household with a request still running must be kept, or it would get fresh slots")
	}
}

func TestAClockSteppingBackAddsNoTokensAndDoesNotFreeze(t *testing.T) {
	clk := newFakeNow()
	l := NewHouseholdLimiter(smallLimits(), clk.now)
	for range 3 {
		mustAdmit(t, l, "h1")()
	}
	clk.advance(-time.Hour)
	if release, _ := l.admit(context.Background(), "h1"); release != nil {
		t.Fatal("a clock stepping back must not add tokens")
	}
	clk.advance(100 * time.Millisecond)
	mustAdmit(t, l, "h1")() // refill resumes from the new time, not an hour later
}

func TestRetryAfterSecondsRoundsUpToAtLeastOne(t *testing.T) {
	for d, want := range map[time.Duration]int{
		0: 1, 100 * time.Millisecond: 1, time.Second: 1, 1500 * time.Millisecond: 2,
	} {
		if got := retryAfterSeconds(d); got != want {
			t.Errorf("retryAfterSeconds(%v) = %d, want %d", d, got, want)
		}
	}
}

func TestARefusalIsLoggedAtMostOnceAMinutePerHousehold(t *testing.T) {
	clk := newFakeNow()
	l := NewHouseholdLimiter(smallLimits(), clk.now)
	if !l.refusalWorthLogging("h1") {
		t.Fatal("the first refusal must be logged")
	}
	if l.refusalWorthLogging("h1") {
		t.Fatal("a second refusal within the minute must not be logged")
	}
	if !l.refusalWorthLogging("h2") {
		t.Fatal("another household's first refusal must be logged")
	}
	clk.advance(time.Minute)
	if !l.refusalWorthLogging("h1") {
		t.Fatal("a refusal a minute later must be logged again")
	}
}

// Requests that truly overlap: every goroutine waits at the barrier, so the
// in-flight cap is tested under real contention, not a queue that happens
// to run one at a time.
func TestConcurrentRequestsNeverExceedTheInFlightCap(t *testing.T) {
	limits := HouseholdLimits{PerSecond: 1000, Burst: 1000, InFlight: 4, MaxWaiting: 100, MaxWait: 5 * time.Second}
	l := NewHouseholdLimiter(limits, newFakeNow().now)

	var running, most atomic.Int32
	barrier := make(chan struct{})
	var wg sync.WaitGroup
	for range 40 {
		wg.Go(func() {
			<-barrier
			release, _ := l.admit(context.Background(), "h1")
			if release == nil {
				t.Error("with a long line and a long wait, every request must eventually run")
				return
			}
			n := running.Add(1)
			for {
				m := most.Load()
				if n <= m || most.CompareAndSwap(m, n) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			running.Add(-1)
			release()
		})
	}
	close(barrier)
	wg.Wait()
	if got := most.Load(); got > 4 || got < 2 {
		t.Fatalf("most running at once = %d, want between 2 and 4 (the cap, and proof they overlapped)", got)
	}
}
