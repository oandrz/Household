package crypto

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// These are internal tests (package crypto, not crypto_test) because the
// ceilings are only observable by swapping the derivation for one the test
// controls; from outside, a bounded hasher and an unbounded one return
// identical hashes.

// encodedForTest is a well-formed stored hash, so Verify reaches the
// derivation instead of refusing on a parse error before it.
func encodedForTest(t *testing.T, h *Argon2Hasher) string {
	t.Helper()
	encoded, err := h.Hash(context.Background(), "password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	return encoded
}

// heldDerivations swaps the derivation for one that parks until release is
// closed, and reports each arrival on entered. A test can then hold every
// slot for as long as it needs, with no sleep standing in for "long enough".
func heldDerivations(h *Argon2Hasher) (entered chan struct{}, release chan struct{}) {
	entered = make(chan struct{}, 1024)
	release = make(chan struct{})
	h.idKey = func(_, _ []byte, _, _ uint32, _ uint8, keyLen uint32) []byte {
		entered <- struct{}{}
		<-release
		return make([]byte, keyLen)
	}
	return entered, release
}

// waitFor fails the test instead of hanging it when an expected event never
// comes.
func waitFor(t *testing.T, events <-chan struct{}, n int, what string) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-events:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for %s (%d of %d)", what, i, n)
		}
	}
}

// answerOrFail runs call and fails the test if it has not answered within a
// few seconds. A caller that should have been refused but was made to wait
// would otherwise hang the test instead of failing it.
func answerOrFail(t *testing.T, what string, call func() error) error {
	t.Helper()
	answered := make(chan error, 1)
	go func() { answered <- call() }()
	select {
	case err := <-answered:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("%s was made to wait instead of being answered", what)
		return nil
	}
}

// waitUntilQueued blocks until n callers hold a place in line. The queue is
// the only thing that says a caller has stopped moving, so it is polled.
func waitUntilQueued(t *testing.T, h *Argon2Hasher, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(h.queue) < n {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d callers queued", len(h.queue), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestArgon2DerivationsAreBoundedInConcurrency pins the memory ceiling:
// however many requests arrive at once, no more than cap(slots) derivations
// -- each holding Argon2MemoryKiB of RAM -- run at the same moment. Hash and
// Verify are both driven, because either one skipping the bound would be
// the whole hole.
func TestArgon2DerivationsAreBoundedInConcurrency(t *testing.T) {
	h := NewArgon2Hasher(1, 8, 1)
	encoded := encodedForTest(t, h)
	slots := cap(h.slots)

	calls := map[string]func() error{
		"Hash": func() error {
			_, err := h.Hash(context.Background(), "password")
			return err
		},
		"Verify": func() error {
			_, err := h.Verify(context.Background(), "password", encoded)
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			var running, peak atomic.Int32
			started := make(chan struct{}, 1024)
			release := make(chan struct{})
			h.idKey = func(_, _ []byte, _, _ uint32, _ uint8, keyLen uint32) []byte {
				now := running.Add(1)
				for {
					old := peak.Load()
					if now <= old || peak.CompareAndSwap(old, now) {
						break
					}
				}
				started <- struct{}{}
				<-release
				running.Add(-1)
				return make([]byte, keyLen)
			}

			// As many callers as the hasher will take at all: every slot,
			// and every place in line.
			callers := slots + cap(h.queue)
			var wg sync.WaitGroup
			for i := 0; i < callers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := call(); err != nil {
						t.Errorf("%s: %v", name, err)
					}
				}()
			}

			// Every slot is held and everyone else is in line. Only now is
			// "at most cap(slots) at once" a claim the hasher had the chance
			// to break.
			waitFor(t, started, slots, "the slots to fill")
			waitUntilQueued(t, h, cap(h.queue))
			close(release)
			wg.Wait()

			if got := int(peak.Load()); got != slots {
				t.Fatalf("peak concurrent derivations = %d, want exactly %d", got, slots)
			}
		})
	}
}

// TestArgon2RefusesACallerWhenTheQueueIsFull pins the queue's ceiling: with
// every slot held and every place in line taken, the next caller is told
// "busy" at once instead of joining a line with no end.
func TestArgon2RefusesACallerWhenTheQueueIsFull(t *testing.T) {
	h := NewArgon2Hasher(1, 8, 1)
	encoded := encodedForTest(t, h)
	entered, release := heldDerivations(h)

	var wg sync.WaitGroup
	for i := 0; i < cap(h.slots)+cap(h.queue); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := h.Hash(context.Background(), "password"); err != nil {
				t.Errorf("a caller inside the ceiling was refused: %v", err)
			}
		}()
	}
	waitFor(t, entered, cap(h.slots), "the slots to fill")
	waitUntilQueued(t, h, cap(h.queue))
	if !h.Full() {
		t.Error("Full() = false with every slot and every place in line taken")
	}

	err := answerOrFail(t, "Hash past the ceiling", func() error {
		_, err := h.Hash(context.Background(), "password")
		return err
	})
	if !errors.Is(err, usecase.ErrHasherBusy) {
		t.Errorf("Hash past the ceiling: err = %v, want usecase.ErrHasherBusy", err)
	}

	var ok bool
	err = answerOrFail(t, "Verify past the ceiling", func() error {
		var err error
		ok, err = h.Verify(context.Background(), "password", encoded)
		return err
	})
	if !errors.Is(err, usecase.ErrHasherBusy) {
		t.Errorf("Verify past the ceiling: err = %v, want usecase.ErrHasherBusy", err)
	}
	if ok {
		t.Error("Verify past the ceiling answered true; a check that never ran must never pass")
	}

	close(release)
	wg.Wait()
	if h.Full() {
		t.Error("Full() = true after every caller finished")
	}
}

// TestArgon2WaiterLeavesWhenItsCallerDoes pins the other half of the queue:
// a request that goes away stops waiting, and gives its place in line back.
// A waiter that kept its place after leaving would let abandoned requests
// fill the line for good.
func TestArgon2WaiterLeavesWhenItsCallerDoes(t *testing.T) {
	h := NewArgon2Hasher(1, 8, 1)
	entered, release := heldDerivations(h)

	var holders sync.WaitGroup
	for i := 0; i < cap(h.slots); i++ {
		holders.Add(1)
		go func() {
			defer holders.Done()
			_, _ = h.Hash(context.Background(), "password")
		}()
	}
	waitFor(t, entered, cap(h.slots), "the slots to fill")
	// Busy slots alone are not "full": the next caller would wait, not be
	// refused.
	if h.Full() {
		t.Fatal("Full() = true with every slot taken but nobody in line")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := h.Hash(ctx, "password")
		done <- err
	}()
	waitUntilQueued(t, h, 1)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter kept waiting after its caller left")
	}
	if got := len(h.queue); got != 0 {
		t.Fatalf("places in line still held after the waiter left = %d, want 0", got)
	}

	close(release)
	holders.Wait()
	if got := len(h.slots); got != 0 {
		t.Fatalf("slots still held after every derivation finished = %d, want 0", got)
	}
}
