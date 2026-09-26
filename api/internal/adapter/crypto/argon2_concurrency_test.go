package crypto

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestArgon2DerivationsAreBoundedInConcurrency pins the memory ceiling that
// keeps sign-in from being a way to exhaust the box: however many requests
// arrive at once, no more than cap(slots) derivations -- each holding
// Argon2MemoryKiB of RAM -- may be running at the same moment. The rest wait.
//
// This is an internal test (package crypto, not crypto_test) because the
// ceiling is only observable by swapping the derivation for one that records
// how many copies of itself are running; from outside, a bounded hasher and an
// unbounded one return identical hashes.
func TestArgon2DerivationsAreBoundedInConcurrency(t *testing.T) {
	h := NewArgon2Hasher(1, 8, 1)
	limit := cap(h.slots)
	if limit < 1 {
		t.Fatalf("cap(slots) = %d, want at least 1", limit)
	}

	var running, peak atomic.Int32
	h.idKey = func(password, salt []byte, t, m uint32, p uint8, keyLen uint32) []byte {
		now := running.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		// Long enough that, without the bound, every goroutine below would
		// be inside this function at once.
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		return make([]byte, keyLen)
	}

	var wg sync.WaitGroup
	for i := 0; i < limit*4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := h.Hash("password"); err != nil {
				t.Errorf("Hash: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := int(peak.Load()); got > limit {
		t.Fatalf("peak concurrent derivations = %d, want at most %d", got, limit)
	}
}
