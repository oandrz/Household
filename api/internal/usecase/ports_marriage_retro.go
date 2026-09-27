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
	// same convention budgets.month and TransactionRepository.MonthTotals's
	// month param use. A repository must store and return it that way, so a
	// caller may compare two Month values directly without renormalising.
	Month       time.Time
	Mood        *int
	WentWell    string
	WasHard     string
	Notes       string
	CompletedAt *time.Time
	Version     int
}

// RetroSummary is one row of the history list: the stored retro plus its
// action counts. RetroService.List always overwrites Quote with
// domain.FirstSentence(Retro.Notes), discarding whatever a
// RetroRepository.List implementation put there. The field exists so
// callers have one row type instead of a wrapper around the repository
// struct.
type RetroSummary struct {
	Retro RetroRecord
	// ActionCount is every action the retro has ever recorded, ticked or
	// not -- the History row's "K actions" figure.
	ActionCount int
	// OpenActionCount is the subset of ActionCount not yet done -- count(*)
	// WHERE done_at IS NULL, the same predicate SetDone's done=false branch
	// clears. Overview's "Next retro" card reads this field, never
	// ActionCount: it answers "anything still outstanding," not "how many
	// actions were ever recorded."
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
	// retro that no longer exists (domain.ErrNotFound) from one whose
	// version moved under the editor (domain.ErrRetroChanged) after a
	// zero-row UPDATE. Must already be normalised to the first of the
	// month, midnight UTC -- RetroService.Save does this before setting the
	// field; the repository does not normalise it.
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
	// already has a retro reports domain.ErrAlreadyExists -- the UNIQUE
	// (household_id, month) constraint, translated, never a raw pgx error,
	// which also makes a double-clicked button harmless. month must already
	// be normalised to the first of the calendar month, midnight UTC; the
	// caller (RetroService) does that, not this method.
	Create(ctx context.Context, householdID string, month time.Time) (RetroRecord, error)
	// ByMonth reports domain.ErrNotFound when the month has no retro, which
	// the page reads as "not started," not an error. month must already be
	// normalised the same way Create's is.
	ByMonth(ctx context.Context, householdID string, month time.Time) (RetroRecord, error)
	// List returns every retro, newest month first, with its action count
	// and open action count (see RetroSummary). Deliberately unbounded -- a
	// household writes twelve rows a year, so a decade is 120 rows in one
	// query, and the design's "Show 2025 (7 more)" is client-side
	// disclosure, not a second request. Don't add paging without a
	// household the flat list actually hurts.
	List(ctx context.Context, householdID string) ([]RetroSummary, error)
	// Update replaces mood and the three text columns and bumps version, but
	// only when the stored version equals u.Version -- a mismatch returns
	// domain.ErrRetroChanged and writes nothing, since the other partner
	// saved first and merging would silently lose their edit. The returned
	// record carries the new version, so a caller never has to guess what to
	// send next. u.Month must already be normalised, per RetroUpdate.Month.
	Update(ctx context.Context, u RetroUpdate) (RetroRecord, error)
	// Complete stamps completed_at with at. Idempotent: completing an already
	// finished retro leaves the original timestamp and is not an error, the
	// same shape GoalRepository.SetArchived takes.
	Complete(ctx context.Context, householdID, retroID string, at time.Time) (RetroRecord, error)
	// DeleteDraft removes a retro that has NOT been finished. completed_at
	// IS NULL belongs in the WHERE clause, not a service-level
	// check-then-delete, which can race; a zero-row match must report
	// domain.ErrNotFound, never a silent success. Don't repeat
	// SetBillNextDue's mistake of committing partial writes on a zero-row
	// match (see docs/LEARNING.md).
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
	// from July" offer. The caller passes only the immediately previous
	// month: a household that skipped four months must not be handed an
	// unbounded backlog on the night it comes back. month must already be
	// normalised to the first of the calendar month, midnight UTC; the
	// caller normalises, not this method.
	OpenInMonth(ctx context.Context, householdID string, month time.Time) ([]RetroActionRecord, error)
}
