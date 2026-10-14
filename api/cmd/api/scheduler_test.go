package main

import (
	"runtime"
	"testing"
)

func TestAddSchedulerThreadsLeavesRoomBesideTheHasher(t *testing.T) {
	before := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(before) })

	const hashing = 4
	if got := addSchedulerThreads(hashing); got != before+hashing {
		t.Fatalf("addSchedulerThreads(%d) = %d, want %d", hashing, got, before+hashing)
	}
	if got := runtime.GOMAXPROCS(0); got != before+hashing {
		t.Fatalf("scheduler threads now = %d, want %d -- with the hasher flat out, "+
			"as many threads as there were before must still be free", got, before+hashing)
	}
}
