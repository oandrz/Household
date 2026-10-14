package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type LoginAttemptRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewLoginAttemptRepo(db *DB) *LoginAttemptRepo {
	return &LoginAttemptRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
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

// ReserveFailure takes the ledger's lock for this household or address,
// writes the attempt, reads the ones before it, and commits. The lock is
// what puts simultaneous attempts in one order; see
// usecase.LoginAttemptRepository.ReserveFailure. It is held for these two
// statements only -- never across the password check that follows, which
// would tie a database connection to a hash that takes a thousand times
// longer.
func (r *LoginAttemptRepo) ReserveFailure(ctx context.Context, householdID, userID *string, email string, at, since time.Time) (usecase.FailureReservation, error) {
	// login_attempts.email is citext, so two spellings of one address count
	// together; the lock has to treat them as one address too.
	ledger := "login-attempts:address:" + strings.ToLower(email)
	if householdID != nil {
		ledger = "login-attempts:household:" + *householdID
	}

	var reserved usecase.FailureReservation
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.LockAttemptLedger(ctx, ledger); err != nil {
			return err
		}
		id, err := q.ReserveLoginFailure(ctx, sqlcgen.ReserveLoginFailureParams{
			HouseholdID: nullableUUID(householdID),
			UserID:      nullableUUID(userID),
			Email:       email,
			At:          timestamptz(at),
		})
		if err != nil {
			return err
		}

		var earlier []pgtype.Timestamptz
		if householdID != nil {
			earlier, err = q.ListEarlierFailures(ctx, sqlcgen.ListEarlierFailuresParams{
				HouseholdID: uuid(*householdID),
				At:          timestamptz(since),
				ID:          id,
			})
		} else {
			earlier, err = q.ListEarlierFailuresByEmail(ctx, sqlcgen.ListEarlierFailuresByEmailParams{
				Email: email,
				At:    timestamptz(since),
				ID:    id,
			})
		}
		if err != nil {
			return err
		}
		reserved = usecase.FailureReservation{ID: uuidToString(id), Earlier: toTimes(earlier)}
		return nil
	})
	if err != nil {
		return usecase.FailureReservation{}, translate(err, "reserve login failure")
	}
	return reserved, nil
}

func (r *LoginAttemptRepo) Withdraw(ctx context.Context, attemptID string) error {
	return translate(r.q.WithdrawLoginFailure(ctx, uuid(attemptID)), "withdraw login failure")
}

func (r *LoginAttemptRepo) ClearFailures(ctx context.Context, householdID string) error {
	return translate(r.q.ClearFailures(ctx, uuid(householdID)), "clear failures")
}

// Prune wraps PruneLoginAttempts. See usecase.LoginAttemptRepository.Prune's
// doc comment for the NULL-household_id rows this reaches that
// ClearFailures cannot, and the caller's obligation to pass a cutoff well
// outside domain.LockoutPolicy.Window.
func (r *LoginAttemptRepo) Prune(ctx context.Context, before time.Time) (int64, error) {
	n, err := r.q.PruneLoginAttempts(ctx, timestamptz(before))
	if err != nil {
		return 0, translate(err, "prune login attempts")
	}
	return n, nil
}
