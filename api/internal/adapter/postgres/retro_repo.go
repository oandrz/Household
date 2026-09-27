package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// RetroRepo holds only *sqlcgen.Queries, not the pool -- unlike GoalRepo it
// has no method that needs to begin its own transaction: every write here is
// exactly one statement (CategoryRepo is the closer model for that reason).
type RetroRepo struct {
	q *sqlcgen.Queries
}

func NewRetroRepo(db *DB) *RetroRepo {
	return &RetroRepo{q: sqlcgen.New(db.Pool())}
}

func (r *RetroRepo) Create(ctx context.Context, householdID string, month time.Time) (usecase.RetroRecord, error) {
	row, err := r.q.CreateRetro(ctx, sqlcgen.CreateRetroParams{
		HouseholdID: uuid(householdID),
		Month:       dateOnly(month),
	})
	if err != nil {
		return usecase.RetroRecord{}, translate(err, "create retro")
	}
	return toRetroRecord(row.ID, row.Month, row.Mood, row.WentWell, row.WasHard, row.Notes, row.CompletedAt, row.Version)
}

func (r *RetroRepo) ByMonth(ctx context.Context, householdID string, month time.Time) (usecase.RetroRecord, error) {
	row, err := r.q.GetRetroByMonth(ctx, sqlcgen.GetRetroByMonthParams{
		HouseholdID: uuid(householdID),
		Month:       dateOnly(month),
	})
	if err != nil {
		return usecase.RetroRecord{}, translate(err, "get retro by month")
	}
	return toRetroRecord(row.ID, row.Month, row.Mood, row.WentWell, row.WasHard, row.Notes, row.CompletedAt, row.Version)
}

func (r *RetroRepo) List(ctx context.Context, householdID string) ([]usecase.RetroSummary, error) {
	rows, err := r.q.ListRetros(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list retros")
	}
	out := make([]usecase.RetroSummary, 0, len(rows))
	for _, row := range rows {
		rec, err := toRetroRecord(row.ID, row.Month, row.Mood, row.WentWell, row.WasHard, row.Notes, row.CompletedAt, row.Version)
		if err != nil {
			return nil, err
		}
		// Quote is left zero-valued: RetroSummary's doc comment says
		// RetroService.List always overwrites it with
		// domain.FirstSentence(Retro.Notes), so setting it here is wasted work.
		out = append(out, usecase.RetroSummary{
			Retro:           rec,
			ActionCount:     int(row.ActionCount),
			OpenActionCount: int(row.OpenActionCount),
		})
	}
	return out, nil
}

// Update resolves the two ways a zero-row UPDATE can happen: the retro was
// deleted, or the other partner saved first and moved the version. ByMonth
// is the one cheap read that tells them apart -- a deleted retro must read
// back as ErrNotFound, never as "reload and try again."
func (r *RetroRepo) Update(ctx context.Context, u usecase.RetroUpdate) (usecase.RetroRecord, error) {
	mood, err := moodParam(u.Mood)
	if err != nil {
		return usecase.RetroRecord{}, err
	}
	version, ok := versionParam(u.Version)
	if !ok {
		// A version outside int32's range can never legitimately be the stored
		// one -- the column is a Postgres `integer`, so every real value
		// already fits. Refusing here, before sending it to Postgres, stops
		// int32(u.Version) from silently wrapping into a small number that
		// happens to match a real stale version -- the same answer, given
		// honestly instead of by accidental truncation.
		return usecase.RetroRecord{}, domain.ErrRetroChanged
	}
	row, err := r.q.UpdateRetro(ctx, sqlcgen.UpdateRetroParams{
		HouseholdID: uuid(u.HouseholdID),
		ID:          uuid(u.RetroID),
		Mood:        mood,
		WentWell:    u.WentWell,
		WasHard:     u.WasHard,
		Notes:       u.Notes,
		Version:     version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, missing := r.ByMonth(ctx, u.HouseholdID, u.Month)
		switch {
		case errors.Is(missing, domain.ErrNotFound):
			return usecase.RetroRecord{}, domain.ErrNotFound
		case missing != nil:
			// The recheck failed for a reason that has nothing to do with
			// concurrency -- e.g. retroMood refusing a stored value outside
			// 1..5. Reporting that as "your partner saved first" would hide
			// a genuine data fault behind a conflict message nobody would
			// think to look at twice. missing is already translate()d by
			// ByMonth, so it's returned as-is, not re-wrapped.
			return usecase.RetroRecord{}, missing
		default:
			return usecase.RetroRecord{}, domain.ErrRetroChanged
		}
	}
	if err != nil {
		return usecase.RetroRecord{}, translate(err, "update retro")
	}
	return toRetroRecord(row.ID, row.Month, row.Mood, row.WentWell, row.WasHard, row.Notes, row.CompletedAt, row.Version)
}

func (r *RetroRepo) Complete(ctx context.Context, householdID, retroID string, at time.Time) (usecase.RetroRecord, error) {
	row, err := r.q.CompleteRetro(ctx, sqlcgen.CompleteRetroParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(retroID),
		CompletedAt: timestamptz(at),
	})
	if err != nil {
		return usecase.RetroRecord{}, translate(err, "complete retro")
	}
	return toRetroRecord(row.ID, row.Month, row.Mood, row.WentWell, row.WasHard, row.Notes, row.CompletedAt, row.Version)
}

// DeleteDraft treats a zero-row DELETE as domain.ErrNotFound, never
// success -- SetBillNextDue shipped the opposite way round and committed
// two of three writes on a zero-row match (docs/LEARNING.md). This check
// exists in Go as well as in the SQL's completed_at IS NULL clause: the
// WHERE clause stops the wrong row being deleted, this check stops the
// caller being told it worked.
func (r *RetroRepo) DeleteDraft(ctx context.Context, householdID, retroID string) error {
	n, err := r.q.DeleteDraftRetro(ctx, sqlcgen.DeleteDraftRetroParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(retroID),
	})
	if err != nil {
		return translate(err, "delete draft retro")
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// versionParam converts the port's int Version into the wire int32,
// reporting ok=false rather than truncating when the value doesn't fit.
// u.Version arrives from a request body via Save with no bounds check
// before here (Save leaves the version comparison entirely to this
// repository), and int32(u.Version) alone would silently wrap --
// int32(4294967297) == 1, so a client sending that value would match a
// real draft at version 1 and overwrite whatever a partner had just saved.
// This is the request-derived counterpart to moodParam below: CLAUDE.md's
// "fail closed on values you did not construct" applies to both, and
// goal_repo.go's RowLimit cast (clampContributionLimit, before int32()) is
// the same precedent for guarding a cast rather than trusting it.
func versionParam(v int) (int32, bool) {
	if v < 0 || v > math.MaxInt32 {
		return 0, false
	}
	return int32(v), true
}

// moodParam converts the port's *int into the wire *int16, validating a
// present value through domain.ParseMood -- CLAUDE.md's "fail closed on
// values you did not construct": even though Save already validates
// u.Mood, this repository's contract doesn't assume that, and the
// retros.mood CHECK (BETWEEN 1 AND 5) backstops it regardless.
func moodParam(mood *int) (*int16, error) {
	if mood == nil {
		return nil, nil
	}
	m, err := domain.ParseMood(*mood)
	if err != nil {
		return nil, fmt.Errorf("postgres: retro mood: %w", err)
	}
	v := int16(m)
	return &v, nil
}

// retroMood is moodParam's read-side counterpart: a stored mood is
// re-validated through domain.ParseMood rather than cast directly (the
// same fail-closed-on-a-database-column rule ParseMood's doc names) -- a
// row this package didn't just write must not be trusted uninspected.
func retroMood(mood *int16) (*int, error) {
	if mood == nil {
		return nil, nil
	}
	m, err := domain.ParseMood(int(*mood))
	if err != nil {
		return nil, fmt.Errorf("postgres: retro mood: %w", err)
	}
	v := int(m)
	return &v, nil
}

// toRetroRecord converts one retros row's columns into a usecase.RetroRecord.
// It takes plain fields, not a generated row struct, because CreateRetro,
// GetRetroByMonth, ListRetros, UpdateRetro and CompleteRetro each return
// their own distinctly-named sqlc row type despite selecting the identical
// column list (toGoal's comment gives the same reason for the same shape).
func toRetroRecord(id pgtype.UUID, month pgtype.Date, mood *int16, wentWell, wasHard, notes string,
	completedAt pgtype.Timestamptz, version int32) (usecase.RetroRecord, error) {
	m, err := retroMood(mood)
	if err != nil {
		return usecase.RetroRecord{}, err
	}
	return usecase.RetroRecord{
		ID:          uuidToString(id),
		Month:       dateToTime(month),
		Mood:        m,
		WentWell:    wentWell,
		WasHard:     wasHard,
		Notes:       notes,
		CompletedAt: timePtrOf(completedAt),
		Version:     int(version),
	}, nil
}
