package domain

import "time"

// LockoutPolicy describes how repeated password failures suspend password
// sign-in. Magic-link sign-in is deliberately not covered: it is the recovery
// path, and gating it would let either member lock the household out with no
// way back in short of a terminal.
type LockoutPolicy struct {
	MaxAttempts int
	Window      time.Duration
	LockFor     time.Duration
}

func DefaultLockoutPolicy() LockoutPolicy {
	return LockoutPolicy{MaxAttempts: 3, Window: 15 * time.Minute, LockFor: 15 * time.Minute}
}

type LockState struct {
	Locked            bool
	Until             time.Time
	AttemptsRemaining int
}

// Evaluate reports the lock state given the household's failed password
// attempts. It is pure: callers supply both the failures and the current time.
func (p LockoutPolicy) Evaluate(failures []time.Time, now time.Time) LockState {
	cutoff := now.Add(-p.Window)

	recent := 0
	var latest time.Time
	for _, at := range failures {
		if at.Before(cutoff) {
			continue
		}
		recent++
		if at.After(latest) {
			latest = at
		}
	}

	if recent >= p.MaxAttempts {
		until := latest.Add(p.LockFor)
		if now.Before(until) {
			return LockState{Locked: true, Until: until, AttemptsRemaining: 0}
		}
		// A lock that has been served resets the allowance to full, by
		// design: once LockFor has elapsed the household earns a fresh set
		// of tries rather than being immediately re-evaluated against the
		// same recent failures. This is only coherent while LockFor >=
		// Window — with DefaultLockoutPolicy the two are equal, so a lock
		// can never expire while its triggering failures are still inside
		// the window. A policy with LockFor < Window would let the lock
		// expire before the failures age out, briefly granting a full
		// allowance with failures still counting.
		return LockState{AttemptsRemaining: p.MaxAttempts}
	}

	return LockState{AttemptsRemaining: p.MaxAttempts - recent}
}
