package main

import "runtime"

// addSchedulerThreads raises the number of threads Go runs goroutines on by
// busy, the most goroutines the password hasher can have computing at once,
// and returns the new number.
//
// A derivation is pure computation for a tenth of a second or more. By
// default Go has one scheduler thread per CPU, so two derivations can hold
// every one of them, and a goroutine that needs only a moment -- to read a
// reply from Postgres, to answer "busy" -- waits its turn behind them.
// Measured on a two-CPU container under a steady stream of sign-ins: each
// database round trip took about 100 ms with Postgres itself idle, and one
// sign-in took many seconds.
//
// With the extra threads, sharing the CPUs is the operating system's job,
// and it runs a thread that has just woken ahead of one that has been
// computing. The CPUs are no faster and hashing gets no extra share; the
// short jobs just stop queueing behind the long ones.
//
// Don't remove this to "follow the default". The default assumes no
// goroutine computes for long without pausing, and the hasher does.
func addSchedulerThreads(busy int) int {
	n := runtime.GOMAXPROCS(0) + busy
	runtime.GOMAXPROCS(n)
	return n
}
