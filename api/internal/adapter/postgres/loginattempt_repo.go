package postgres

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
)

type LoginAttemptRepo struct{ q *sqlcgen.Queries }

func NewLoginAttemptRepo(db *DB) *LoginAttemptRepo {
	return &LoginAttemptRepo{q: sqlcgen.New(db.Pool())}
}

func (r *LoginAttemptRepo) Record(ctx context.Context, householdID, userID *string, email string, succeeded bool, at time.Time) error {
	return translate(r.q.RecordLoginAttempt(ctx, sqlcgen.RecordLoginAttemptParams{
		HouseholdID: nullableUUID(householdID),
		UserID:      nullableUUID(userID),
		Email:       email,
		Succeeded:   succeeded,
		At:          timestamptz(at),
	}), "record login attempt")
}

func (r *LoginAttemptRepo) FailuresSince(ctx context.Context, householdID string, since time.Time) ([]time.Time, error) {
	rows, err := r.q.ListRecentFailures(ctx, sqlcgen.ListRecentFailuresParams{
		HouseholdID: uuid(householdID),
		At:          timestamptz(since),
	})
	if err != nil {
		return nil, translate(err, "list recent failures")
	}
	return toTimes(rows), nil
}

func (r *LoginAttemptRepo) FailuresSinceForEmail(ctx context.Context, email string, since time.Time) ([]time.Time, error) {
	rows, err := r.q.ListRecentFailuresByEmail(ctx, sqlcgen.ListRecentFailuresByEmailParams{
		Email: email,
		At:    timestamptz(since),
	})
	if err != nil {
		return nil, translate(err, "list recent failures by email")
	}
	return toTimes(rows), nil
}

func (r *LoginAttemptRepo) ClearFailures(ctx context.Context, householdID string) error {
	return translate(r.q.ClearFailures(ctx, uuid(householdID)), "clear failures")
}

// Prune wraps PruneLoginAttempts. See LoginAttemptRepository.Prune's doc
// comment in ports.go for the NULL-household_id rows this reaches that
// ClearFailures cannot, and the caller's obligation to pass a cutoff well
// outside domain.LockoutPolicy.Window.
func (r *LoginAttemptRepo) Prune(ctx context.Context, before time.Time) (int64, error) {
	n, err := r.q.PruneLoginAttempts(ctx, timestamptz(before))
	if err != nil {
		return 0, translate(err, "prune login attempts")
	}
	return n, nil
}
