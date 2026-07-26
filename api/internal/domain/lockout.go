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
		return LockState{AttemptsRemaining: p.MaxAttempts}
	}

	return LockState{AttemptsRemaining: p.MaxAttempts - recent}
}
