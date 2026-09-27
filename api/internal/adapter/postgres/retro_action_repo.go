package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// RetroActionRepo keeps the pool alongside the pool-backed *sqlcgen.Queries,
// like GoalRepo, because Add needs its own transaction: the action and its
// assignees are one write (see usecase.RetroActionRepository.Add), and a
// *sqlcgen.Queries built at construction time can't start one on its own.
type RetroActionRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewRetroActionRepo(db *DB) *RetroActionRepo {
	return &RetroActionRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

// Add writes the action and every assignee inside one pgx.BeginFunc
// transaction, the same shape GoalRepo.Create uses. AddRetroActionAssignee
// scopes its SELECT to household_id, so an id that isn't a membership at
// all, or belongs to a different household, matches zero rows; returning an
// error on that zero rolls the action insert back too, instead of silently
// leaving the action with a missing owner. AddRetroAction gives
// carried_from the same treatment -- see that query's comment.
//
// The loop deliberately does not dedupe in.AssigneeMembershipIDs before
// calling AddRetroActionAssignee, repeats included: picking the same person
// twice is a redundant selection, not a conflict, and the query's own ON
// CONFLICT DO UPDATE turns the repeat into a no-op instead of a 23505.
// Deduping here first would make that SQL clause unreachable and untested.
func (r *RetroActionRepo) Add(ctx context.Context, in usecase.RetroActionInput) (usecase.RetroActionRecord, error) {
	// CarriedFrom is the one id on this port that arrives unvalidated -- the
	// carry-over control posts it from a request body, unlike every other
	// id here, which this package's own prior reads produced. Left
	// unchecked, nullableUUID/uuid would turn a malformed value into SQL
	// NULL, which AddRetroAction's "$2 IS NULL OR ..." clause can't tell
	// apart from a genuinely absent carried_from -- so "" (not carried) must
	// keep working while a value that fails to parse must not silently
	// become "not carried". domain.ErrNotFound is the honest answer: an
	// action that can't be parsed isn't one this household has.
	if in.CarriedFrom != "" && !uuidLooksValid(in.CarriedFrom) {
		return usecase.RetroActionRecord{}, domain.ErrNotFound
	}

	var result usecase.RetroActionRecord
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		row, err := q.AddRetroAction(ctx, sqlcgen.AddRetroActionParams{
			Body:        in.Body,
			CarriedFrom: nullableUUID(optionalID(in.CarriedFrom)),
			ID:          uuid(in.RetroID),
			HouseholdID: uuid(in.HouseholdID),
		})
		if err != nil {
			return translate(err, "add retro action")
		}

		for _, membershipID := range in.AssigneeMembershipIDs {
			n, err := q.AddRetroActionAssignee(ctx, sqlcgen.AddRetroActionAssigneeParams{
				ActionID:    row.ID,
				ID:          uuid(membershipID),
				HouseholdID: uuid(in.HouseholdID),
			})
			if err != nil {
				return translate(err, "add retro action assignee")
			}
			if n == 0 {
				// Not a membership of this household -- the same
				// zero-row-is-never-success rule SetDone and Remove enforce
				// below, raised inside the transaction here so it rolls back
				// the action insert too.
				return domain.ErrNotFound
			}
		}

		// dedupeIDs here, not in the loop above: the loop must see every
		// input id, repeats included, so a repeat reaches
		// AddRetroActionAssignee and exercises its ON CONFLICT clause.
		// This only shapes what Add returns, to match what
		// ForRetro/OpenInMonth read back -- the table's PRIMARY KEY
		// (action_id, membership_id) already makes a duplicate row
		// impossible to store.
		result = toRetroActionRecord(row.ID, row.RetroID, row.Body, row.DoneAt, row.CarriedFrom, dedupeIDs(in.AssigneeMembershipIDs))
		return nil
	})
	if err != nil {
		return usecase.RetroActionRecord{}, err
	}
	return result, nil
}

// dedupeIDs keeps the first occurrence of each id and drops the rest,
// returning nil (not empty) for no input -- the same convention assigneeIDs
// below uses, so Add's record and ForRetro/OpenInMonth's never disagree.
func dedupeIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// ForRetro returns one retro's actions, insertion order, each carrying the
// assignees ListRetroActions already folded onto its row.
func (r *RetroActionRepo) ForRetro(ctx context.Context, householdID, retroID string) ([]usecase.RetroActionRecord, error) {
	rows, err := r.q.ListRetroActions(ctx, sqlcgen.ListRetroActionsParams{
		HouseholdID: uuid(householdID),
		RetroID:     uuid(retroID),
	})
	if err != nil {
		return nil, translate(err, "list retro actions")
	}
	out := make([]usecase.RetroActionRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, toRetroActionRecord(row.ID, row.RetroID, row.Body, row.DoneAt, row.CarriedFrom, assigneeIDs(row.AssigneeIds)))
	}
	return out, nil
}

// SetDone ticks or unticks one action. done=false leaves doneAt zero, which
// timestamptz below turns into SQL NULL -- the contract is to clear the
// stamp, never record a "not done" time.
func (r *RetroActionRepo) SetDone(ctx context.Context, householdID, actionID string, done bool, at time.Time) error {
	var doneAt pgtype.Timestamptz
	if done {
		doneAt = timestamptz(at)
	}
	n, err := r.q.SetRetroActionDone(ctx, sqlcgen.SetRetroActionDoneParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(actionID),
		DoneAt:      doneAt,
	})
	if err != nil {
		return translate(err, "set retro action done")
	}
	if n == 0 {
		// A zero-row UPDATE is never success -- the SetBillNextDue defect
		// this codebase already learned from (docs/LEARNING.md), and the
		// same check DeleteDraft and Remove both make.
		return domain.ErrNotFound
	}
	return nil
}

// Remove hard-deletes one action. Nothing references an action except a
// later action's carried_from, which is ON DELETE SET NULL
// (00009_retros.sql), so this can never orphan anything.
func (r *RetroActionRepo) Remove(ctx context.Context, householdID, actionID string) error {
	n, err := r.q.DeleteRetroAction(ctx, sqlcgen.DeleteRetroActionParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(actionID),
	})
	if err != nil {
		return translate(err, "delete retro action")
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// OpenInMonth returns that month's unticked actions. month is trusted to
// already be the first of the calendar month, midnight UTC (see
// RetroActionRepository's doc comment) -- this repository doesn't
// renormalise it.
func (r *RetroActionRepo) OpenInMonth(ctx context.Context, householdID string, month time.Time) ([]usecase.RetroActionRecord, error) {
	rows, err := r.q.ListOpenActionsInMonth(ctx, sqlcgen.ListOpenActionsInMonthParams{
		HouseholdID: uuid(householdID),
		Month:       dateOnly(month),
	})
	if err != nil {
		return nil, translate(err, "list open actions in month")
	}
	out := make([]usecase.RetroActionRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, toRetroActionRecord(row.ID, row.RetroID, row.Body, row.DoneAt, row.CarriedFrom, assigneeIDs(row.AssigneeIds)))
	}
	return out, nil
}

// assigneeIDs converts array_agg's wire-level []pgtype.UUID into the
// []string RetroActionRecord carries, returning nil (not a zero-length
// slice) for no assignees. COALESCE in ListRetroActions and
// ListOpenActionsInMonth already turns SQL NULL into '{}', so len(ids) == 0
// is the only case left to normalise.
func assigneeIDs(ids []pgtype.UUID) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = uuidToString(id)
	}
	return out
}

// toRetroActionRecord converts one retro_actions row's columns into a
// usecase.RetroActionRecord. carriedFrom uses optionalIDToString, not
// uuidToString, because the column is nullable -- the same "" <-> SQL NULL
// convention AccountRecord.OwnerMembershipID already follows.
func toRetroActionRecord(id, retroID pgtype.UUID, body string, doneAt pgtype.Timestamptz, carriedFrom pgtype.UUID, assignees []string) usecase.RetroActionRecord {
	return usecase.RetroActionRecord{
		ID:                    uuidToString(id),
		RetroID:               uuidToString(retroID),
		Body:                  body,
		DoneAt:                timePtrOf(doneAt),
		CarriedFrom:           optionalIDToString(carriedFrom),
		AssigneeMembershipIDs: assignees,
	}
}
