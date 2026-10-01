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
// EARLIER of {previous month, current month} that has no retro row yet and is
// not before the month the household was created in.
//
// A couple doing July's retro on 2 August means July, not August, and August
// stays available afterwards. With both months already covered there is
// nothing to start, and the page opens what exists instead.
//
// The creation month is a floor: a household did not exist the month before
// it signed up, so it is never asked to look back on it. A household created
// on 1 October is offered October. Once October has a retro it is offered
// nothing until November, never September.
//
// today and createdOn are both the household's calendar days (TodayIn), so
// "previous", "current" and the creation month are the household's months and
// not the server's. They are parameters, never time.Now() reached for in
// here: every other date rule in this codebase takes its clock from the
// caller, which is what makes them testable without freezing time globally.
func StartableMonth(today, createdOn time.Time, currentExists, previousExists bool) (time.Time, bool) {
	current := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	previous := current.AddDate(0, -1, 0)
	floor := time.Date(createdOn.Year(), createdOn.Month(), 1, 0, 0, 0, 0, time.UTC)

	switch {
	case !previousExists && !previous.Before(floor):
		return previous, true
	case !currentExists && !current.Before(floor):
		return current, true
	default:
		return time.Time{}, false
	}
}

// firstSentenceMax is the fallback budget for notes that never terminate a
// sentence, counted with the trailing ellipsis included: 60 characters fits
// the design's history row beside the mood and the action count without
// wrapping (docs/superpowers/specs/2026-08-16-hearth-retros-design.md, the
// "First sentence" row).
const firstSentenceMax = 60

// FirstSentence is the quoted line in a history row: the design renders
// `June 2026 · Mood 4/5 · 3 actions · "best month this year"`, and June's
// notes open with exactly that sentence. Derived rather than a second field
// nobody would fill twice.
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
	// Cut on a rune boundary, not a byte position: a note can hold any
	// language, and slicing bytes would split a multi-byte character in
	// half -- the same mistake initialOf (adapter/postgres) exists to avoid,
	// where ToUpper(name[:1]) produced mojibake for a non-ASCII name.
	runes := []rune(trimmed)
	return string(runes[:firstSentenceMax-1]) + "…"
}
