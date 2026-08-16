package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Mood is how the month felt, 1 (worst) to 5 (best) -- the design's five
// emoji. It is a distinct type rather than an int so a caller cannot pass a
// count or an index where a mood belongs.
type Mood int

// ParseMood refuses anything outside 1..5. It fails closed because a mood
// arrives from two places we did not construct: a request body and a database
// column (CLAUDE.md, "Fail closed on values you did not construct").
func ParseMood(n int) (Mood, error) {
	if n < 1 || n > 5 {
		return 0, fmt.Errorf("%w: %d", ErrInvalidMood, n)
	}
	return Mood(n), nil
}

// StartableMonth answers which month the "Start retro" button begins: the
// EARLIER of {previous month, current month} that has no retro row yet.
//
// A couple doing July's retro on 2 August means July, not August -- the
// design's own example retro is dated Jun 28, near the edge of its month --
// and August stays available afterwards. When both months already have a
// retro there is nothing to start, and the page opens what exists instead.
//
// today is a parameter, never time.Now() reached for in here: every other
// date rule in this codebase takes its clock from the caller, which is what
// makes them testable without freezing time globally.
func StartableMonth(today time.Time, currentExists, previousExists bool) (time.Time, bool) {
	current := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	previous := current.AddDate(0, -1, 0)

	switch {
	case !previousExists:
		return previous, true
	case !currentExists:
		return current, true
	default:
		return time.Time{}, false
	}
}

// firstSentenceMax is the fallback budget for notes that never terminate a
// sentence, counted with the trailing ellipsis included. The design doc
// (docs/superpowers/specs/2026-08-16-hearth-retros-design.md:278) specifies
// "the first 60 characters with an ellipsis", but this task's own test data
// (retro_test.go's long-note case) only passes with 62 here -- a two-
// character gap between the plan's spec and its own test fixture, flagged in
// task-2-report.md for the controller to reconcile rather than resolved
// unilaterally by editing the verbatim test.
const firstSentenceMax = 62

// FirstSentence is the quoted line in a history row: the design renders
// `June 2026 · Mood 4/5 · 3 actions · "best month this year"`, and June's
// notes open with exactly that sentence. Derived rather than a second field
// nobody would fill twice (spec decision 7).
func FirstSentence(notes string) string {
	trimmed := strings.TrimSpace(notes)
	if trimmed == "" {
		return ""
	}
	if i := strings.IndexAny(trimmed, ".!?"); i >= 0 {
		return trimmed[:i+1]
	}
	if utf8.RuneCountInString(trimmed) <= firstSentenceMax {
		return trimmed
	}
	// Cut on a rune boundary: a note can hold any language, and slicing by
	// byte position would split a multi-byte character in half. This is the
	// same class of mistake initialOf (internal/adapter/postgres) already
	// exists to avoid, for the same reason: ToUpper(name[:1]) sliced bytes,
	// not runes, and produced mojibake for any non-ASCII display name.
	runes := []rune(trimmed)
	return string(runes[:firstSentenceMax-1]) + "…"
}
