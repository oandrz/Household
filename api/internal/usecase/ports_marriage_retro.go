// This file holds the marriage slice's monthly retro ports. ports.go lists
// every ports file.

package usecase

import (
	"context"
	"time"
)

// RetroRecord is one stored retro. Mood is a pointer because "nobody has
// picked an emoji yet" is a real state and 0 is not a mood; CompletedAt is a
// pointer for the same reason -- nil IS the draft concept.
type RetroRecord struct {
	ID string
	// Month is always the first of the calendar month, midnight UTC -- the
	// same normalised convention budgets.month and
	// TransactionRepository.MonthTotals's own month parameter use. A
	// repository must store and return it that way; a caller comparing two
	// Month values (RetroService does, for the mood chart and the startable
	// month) may rely on that rather than re-normalising itself.
	Month       time.Time
	Mood        *int
	WentWell    string
	WasHard     string
	Notes       string
	CompletedAt *time.Time
	Version     int
}

// RetroSummary is one row of the history list: the stored retro plus the
// action counts the row displays. Quote is the exception to "what the
// repository can supply": RetroService.List always overwrites it with
// domain.FirstSentence(Retro.Notes) (per the spec's formulas table, "History
// row"), unconditionally, so a RetroRepository.List implementation has no
// reason to populate it -- whatever it puts there is discarded, not merged.
// This struct carries the field anyway so Tasks 4-8 have one name for the
// row rather than a repository type plus a service-only wrapper around it.
type RetroSummary struct {
	Retro RetroRecord
	// ActionCount is every action the retro has ever recorded, ticked or
	// not -- the History row's own "K actions" figure (spec's formulas
	// table: "K counts all of that retro's actions, ticked or not").
	ActionCount int
	// OpenActionCount is the subset of ActionCount still undone --
	// count(*) WHERE done_at IS NULL, the same predicate SetActionDone's
	// own done=false branch clears. Overview's "Next retro" card reads
	// THIS field, never ActionCount: a retro whose three actions are all
	// ticked has ActionCount 3 but OpenActionCount 0, and the card exists
	// to answer "is there anything still outstanding," not "how many
	// actions were ever written down." Ticking an action leaves this
	// number; it never rejoins it.
	OpenActionCount int
	Quote           string
}

// RetroActionInput is what Add receives. AssigneeMembershipIDs may be empty
// (an action nobody owns yet) or hold one or both owners; CarriedFrom is the
// id of last month's action when this one was carried, "" otherwise.
type RetroActionInput struct {
	HouseholdID           string
	RetroID               string
	Body                  string
	AssigneeMembershipIDs []string
	CarriedFrom           string
}

// RetroActionRecord is one action. DoneAt nil means open.
type RetroActionRecord struct {
	ID                    string
	RetroID               string
	Body                  string
	DoneAt                *time.Time
	CarriedFrom           string
	AssigneeMembershipIDs []string
}

// RetroUpdate is one save of the retro's own fields. Version is the version
// the editor loaded; the repository refuses the write when it no longer
// matches. Mood nil clears the mood, which a household can legitimately do.
type RetroUpdate struct {
	HouseholdID string
	RetroID     string
	// Month is the retro's own month, carried so the repository can tell a
	// retro that no longer exists (ErrNotFound) from one whose version moved
	// under the editor (ErrRetroChanged) after a zero-row UPDATE. The HTTP
	// layer reads {month} from the URL, but passes it through un-normalised
	// -- RetroService.Save is the caller that normalises it (with
	// startOfMonth) before ever setting this field, the same way it already
	// normalises before comparing in List and Month. Always the first of the
	// month, midnight UTC by the time it reaches here -- RetroRecord.Month's
	// own convention; the repository does not normalise it either.
	Month    time.Time
	Mood     *int
	WentWell string
	WasHard  string
	Notes    string
	Version  int
}

// RetroRepository stores one household's monthly retros. Every method is
// scoped by householdID and must filter on it in SQL: a retro that belongs to
// another household must be indistinguishable from one that does not exist.
type RetroRepository interface {
	// Create writes an empty draft for the month and returns it. A month that
	// already has a retro surfaces as domain.ErrAlreadyExists -- the UNIQUE
	// (household_id, month) constraint, translated, never a raw pgx error.
	// This is also what makes a double-clicked button harmless. month must
	// already be the first of the calendar month, midnight UTC -- the caller
	// (RetroService) normalises before calling; this method does not.
	Create(ctx context.Context, householdID string, month time.Time) (RetroRecord, error)
	// ByMonth reports domain.ErrNotFound when the month has no retro, which
	// the page reads as "not started" rather than as an error. month must
	// already be normalised the same way Create's own parameter is -- see
	// that method's comment.
	ByMonth(ctx context.Context, householdID string, month time.Time) (RetroRecord, error)
	// List returns every retro, newest month first, each carrying its own
	// action count AND open action count (RetroSummary's own doc comment
	// says which is which). Deliberately unbounded: a household writes
	// twelve rows a year, so a decade is 120 rows and one query, and the
	// design's "Show 2025 (7 more)" is a disclosure over data the page
	// already holds, not a second request. Do not add paging without a
	// household the flat list actually hurts.
	List(ctx context.Context, householdID string) ([]RetroSummary, error)
	// Update replaces mood and the three text columns, and bumps version, but
	// ONLY when the stored version equals u.Version. A mismatch returns
	// domain.ErrRetroChanged and writes nothing -- the other partner saved
	// while this one was typing, and merging the two would silently lose one
	// of them. The returned record carries the NEW version, so a caller never
	// has to guess what to send next. u.Month must already be normalised --
	// see its own doc comment on RetroUpdate.
	Update(ctx context.Context, u RetroUpdate) (RetroRecord, error)
	// Complete stamps completed_at with at. Idempotent: completing an already
	// finished retro leaves the original timestamp and is not an error, the
	// same shape GoalRepository.SetArchived takes.
	Complete(ctx context.Context, householdID, retroID string, at time.Time) (RetroRecord, error)
	// DeleteDraft removes a retro that has NOT been finished. The
	// completed_at IS NULL condition belongs in the WHERE clause, not in a
	// service if: a check-then-delete can race, and -- the reason that
	// matters here -- a zero-row match must report domain.ErrNotFound rather
	// than success. SetBillNextDue shipped the other way round and committed
	// two of three writes on a zero-row match (docs/LEARNING.md, database
	// catalogue).
	DeleteDraft(ctx context.Context, householdID, retroID string) error
}

// RetroActionRepository stores what a retro decided to do next month.
type RetroActionRepository interface {
	// Add writes the action AND its assignees inside one transaction: an
	// assignee that is not a membership of this household fails the whole
	// insert, so no orphan action survives a half-written assignment.
	Add(ctx context.Context, in RetroActionInput) (RetroActionRecord, error)
	// ForRetro returns a retro's actions in insertion order (created_at, id).
	// There is no position column to sort by -- see 00009_retros.sql for why.
	ForRetro(ctx context.Context, householdID, retroID string) ([]RetroActionRecord, error)
	// SetDone ticks or unticks. done=false clears done_at rather than
	// stamping a "not done" time. Reports domain.ErrNotFound on a zero-row
	// match, for the same reason DeleteDraft does.
	SetDone(ctx context.Context, householdID, actionID string, done bool, at time.Time) error
	// Remove hard-deletes an action. Nothing references an action except a
	// later action's carried_from, which is ON DELETE SET NULL, so removal
	// cannot orphan anything.
	Remove(ctx context.Context, householdID, actionID string) error
	// OpenInMonth returns that month's unticked actions -- the "Still open
	// from July" offer. The caller passes the immediately previous month
	// only: a household that skipped four months must not be handed an
	// unbounded backlog on the night it comes back (spec decision 4). month
	// must already be the first of the calendar month, midnight UTC --
	// RetroRecord.Month's own convention; the caller normalises, not this
	// method.
	OpenInMonth(ctx context.Context, householdID string, month time.Time) ([]RetroActionRecord, error)
}
