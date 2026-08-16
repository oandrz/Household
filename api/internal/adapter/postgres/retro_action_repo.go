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
// like GoalRepo, because Add needs to begin its own transaction: the action
// and its assignees are one write (usecase.RetroActionRepository.Add's own
// doc comment), and a *sqlcgen.Queries built once at construction time
// cannot start a transaction on its own.
type RetroActionRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewRetroActionRepo(db *DB) *RetroActionRepo {
	return &RetroActionRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

// Add writes the action and every assignee inside one pgx.BeginFunc
// transaction, the same shape GoalRepo.Create uses. AddRetroActionAssignee
// is itself scoped to this household -- its SELECT requires the membership
// to belong to household_id, so an id that is not a membership at all, or
// is a membership of a different household, matches zero rows. Seeing that
// zero here and returning an error is what rolls the action insert back
// too: without it, AddRetroActionAssignee's own zero-row failure would be
// silently ignored and the action would survive with a missing owner.
func (r *RetroActionRepo) Add(ctx context.Context, in usecase.RetroActionInput) (usecase.RetroActionRecord, error) {
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
				// Not a membership of this household -- the same zero-row-
				// is-never-success rule SetDone and Remove enforce below,
				// just raised from inside the transaction instead of after
				// it, so returning it here rolls back the action insert
				// above along with it.
				return domain.ErrNotFound
			}
		}

		// len(...) == 0 -> nil, matching assigneeIDs' own normalisation
		// below: without this, a caller passing AssigneeMembershipIDs: []string{}
		// (as opposed to leaving it nil) would get that exact empty-but-
		// non-nil slice echoed back from Add, while ForRetro and OpenInMonth
		// would both report the identical action as nil -- one shape from
		// the write path, a different one from every read path for the same
		// row.
		assignees := in.AssigneeMembershipIDs
		if len(assignees) == 0 {
			assignees = nil
		}
		result = toRetroActionRecord(row.ID, row.RetroID, row.Body, row.DoneAt, row.CarriedFrom, assignees)
		return nil
	})
	if err != nil {
		return usecase.RetroActionRecord{}, err
	}
	return result, nil
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

// SetDone ticks or unticks one action. done=false leaves doneAt at its zero
// value, which timestamptz's caller-side counterpart below turns into SQL
// NULL -- SetDone's own contract (usecase/ports.go): clear the stamp, never
// record a "not done" time.
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

// OpenInMonth returns that month's unticked actions -- month is trusted to
// already be the first of the calendar month, midnight UTC, per this
// method's own doc comment on RetroActionRepository; the repository does not
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
// []string RetroActionRecord carries, returning nil (rather than a
// zero-length, non-nil slice) for an action with no assignees -- the COALESCE
// in ListRetroActions/ListOpenActionsInMonth already turned SQL's own NULL
// into '{}' before this ever sees it, so len(ids) == 0 is the only case left
// to normalise.
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
// usecase.RetroActionRecord. carriedFrom uses optionalIDToString rather than
// uuidToString directly because the column is nullable (carried_from is set
// only when this action was carried over from last month) -- the same ""
// <-> SQL NULL convention AccountRecord.OwnerMembershipID already follows.
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
