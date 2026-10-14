package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type PlatformAdminRepo struct{ q *sqlcgen.Queries }

func NewPlatformAdminRepo(db *DB) *PlatformAdminRepo {
	return &PlatformAdminRepo{q: sqlcgen.New(db.Pool())}
}

func (r *PlatformAdminRepo) Get(ctx context.Context, userID string) (domain.PlatformAdmin, error) {
	row, err := r.q.GetPlatformAdmin(ctx, uuid(userID))
	if err != nil {
		return domain.PlatformAdmin{}, translate(err, "get platform admin")
	}
	return domain.PlatformAdmin{
		UserID:    uuidToString(row.UserID),
		Note:      row.Note,
		CreatedAt: timeOf(row.CreatedAt),
	}, nil
}

// Grant is an upsert rather than an insert: adminctl is run by a person who
// will run it twice, and failing the second time reads as "it did not work".
func (r *PlatformAdminRepo) Grant(ctx context.Context, userID, note string) error {
	return translate(r.q.GrantPlatformAdmin(ctx, sqlcgen.GrantPlatformAdminParams{
		UserID: uuid(userID),
		Note:   note,
	}), "grant platform admin")
}

func (r *PlatformAdminRepo) Revoke(ctx context.Context, userID string) error {
	return translate(r.q.RevokePlatformAdmin(ctx, uuid(userID)), "revoke platform admin")
}

func (r *PlatformAdminRepo) List(ctx context.Context) ([]usecase.PlatformAdminListing, error) {
	rows, err := r.q.ListPlatformAdmins(ctx)
	if err != nil {
		return nil, translate(err, "list platform admins")
	}
	out := make([]usecase.PlatformAdminListing, 0, len(rows))
	for _, row := range rows {
		listing := usecase.PlatformAdminListing{
			UserID:      uuidToString(row.UserID),
			DisplayName: row.DisplayName,
			Note:        row.Note,
			CreatedAt:   timeOf(row.CreatedAt),
		}
		// users.email is nullable -- a member created without credentials has
		// none. sqlc renders it as *string with emit_pointers_for_null_types.
		if row.Email != nil {
			listing.Email = *row.Email
		}
		out = append(out, listing)
	}
	return out, nil
}

type FeatureFlagRepo struct{ q *sqlcgen.Queries }

func NewFeatureFlagRepo(db *DB) *FeatureFlagRepo {
	return &FeatureFlagRepo{q: sqlcgen.New(db.Pool())}
}

// OverridesFor returns the two override layers separately rather than merged,
// because merging is domain.ResolveFlags' job and it needs to know which layer
// each value came from.
func (r *FeatureFlagRepo) OverridesFor(ctx context.Context, householdID string) (map[string]bool, map[string]bool, error) {
	global, err := r.GlobalOverrides(ctx)
	if err != nil {
		return nil, nil, err
	}
	rows, err := r.q.GetHouseholdFlagOverrides(ctx, uuid(householdID))
	if err != nil {
		return nil, nil, translate(err, "get household flag overrides")
	}
	household := make(map[string]bool, len(rows))
	for _, row := range rows {
		household[row.Key] = row.Enabled
	}
	return global, household, nil
}

func (r *FeatureFlagRepo) GlobalOverrides(ctx context.Context) (map[string]bool, error) {
	rows, err := r.q.GetGlobalFlagOverrides(ctx)
	if err != nil {
		return nil, translate(err, "get global flag overrides")
	}
	global := make(map[string]bool, len(rows))
	for _, row := range rows {
		global[row.Key] = row.Enabled
	}
	return global, nil
}

func (r *FeatureFlagRepo) AllHouseholdOverrides(ctx context.Context) ([]usecase.HouseholdFlagOverride, error) {
	rows, err := r.q.ListHouseholdFlagOverrides(ctx)
	if err != nil {
		return nil, translate(err, "list household flag overrides")
	}
	out := make([]usecase.HouseholdFlagOverride, 0, len(rows))
	for _, row := range rows {
		out = append(out, usecase.HouseholdFlagOverride{
			HouseholdID:   uuidToString(row.HouseholdID),
			HouseholdName: row.HouseholdName,
			Key:           row.Key,
			Enabled:       row.Enabled,
		})
	}
	return out, nil
}

func (r *FeatureFlagRepo) SetGlobal(ctx context.Context, key string, enabled bool, updatedBy string) error {
	return translate(r.q.SetGlobalFlag(ctx, sqlcgen.SetGlobalFlagParams{
		Key:       key,
		Enabled:   enabled,
		UpdatedBy: nullableUUID(&updatedBy),
	}), "set global flag")
}

func (r *FeatureFlagRepo) SetHousehold(ctx context.Context, householdID, key string, enabled bool, updatedBy string) error {
	return translate(r.q.SetHouseholdFlag(ctx, sqlcgen.SetHouseholdFlagParams{
		HouseholdID: uuid(householdID),
		Key:         key,
		Enabled:     enabled,
		UpdatedBy:   nullableUUID(&updatedBy),
	}), "set household flag")
}

func (r *FeatureFlagRepo) ClearHousehold(ctx context.Context, householdID, key string) error {
	return translate(r.q.ClearHouseholdFlag(ctx, sqlcgen.ClearHouseholdFlagParams{
		HouseholdID: uuid(householdID),
		Key:         key,
	}), "clear household flag")
}

// AdminAuditRepo writes the admin audit log and never reads it back -- see
// usecase.AdminAuditRepository's doc comment for why.
type AdminAuditRepo struct{ q *sqlcgen.Queries }

func NewAdminAuditRepo(db *DB) *AdminAuditRepo { return &AdminAuditRepo{q: sqlcgen.New(db.Pool())} }

func (r *AdminAuditRepo) Record(ctx context.Context, entry usecase.AdminAuditEntry) error {
	detail := entry.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return translate(err, "encode audit detail")
	}
	return translate(r.q.RecordAdminAudit(ctx, sqlcgen.RecordAdminAuditParams{
		ActorUserID: uuid(entry.ActorUserID),
		Action:      entry.Action,
		Target:      entry.Target,
		Detail:      encoded,
		Ip:          entry.IP,
		CreatedAt:   timestamptz(entry.At),
	}), "record admin audit")
}

type AdminReauthAttemptRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewAdminReauthAttemptRepo(db *DB) *AdminReauthAttemptRepo {
	return &AdminReauthAttemptRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

func (r *AdminReauthAttemptRepo) Record(ctx context.Context, userID string, succeeded bool, at time.Time) error {
	return translate(r.q.RecordAdminReauthAttempt(ctx, sqlcgen.RecordAdminReauthAttemptParams{
		UserID:    uuid(userID),
		Succeeded: succeeded,
		At:        timestamptz(at),
	}), "record admin reauth attempt")
}

// ReserveFailure takes this operator's ledger lock, writes the attempt,
// reads the ones before it, and commits -- the same shape, for the same
// reason, as LoginAttemptRepo.ReserveFailure.
func (r *AdminReauthAttemptRepo) ReserveFailure(ctx context.Context, userID string, at, since time.Time) (usecase.FailureReservation, error) {
	var reserved usecase.FailureReservation
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.LockAttemptLedger(ctx, "admin-reauth-attempts:"+userID); err != nil {
			return err
		}
		id, err := q.ReserveAdminReauthFailure(ctx, sqlcgen.ReserveAdminReauthFailureParams{
			UserID: uuid(userID),
			At:     timestamptz(at),
		})
		if err != nil {
			return err
		}
		earlier, err := q.ListEarlierAdminReauthFailures(ctx, sqlcgen.ListEarlierAdminReauthFailuresParams{
			UserID: uuid(userID),
			At:     timestamptz(since),
			ID:     id,
			Newest: usecase.MaxEarlierFailures,
		})
		if err != nil {
			return err
		}
		reserved = usecase.FailureReservation{ID: uuidToString(id), Earlier: toTimes(earlier)}
		return nil
	})
	if err != nil {
		return usecase.FailureReservation{}, translate(err, "reserve admin reauth failure")
	}
	return reserved, nil
}

func (r *AdminReauthAttemptRepo) Withdraw(ctx context.Context, attemptID string) error {
	return translate(r.q.WithdrawAdminReauthFailure(ctx, uuid(attemptID)), "withdraw admin reauth failure")
}

func (r *AdminReauthAttemptRepo) ClearFailures(ctx context.Context, userID string) error {
	return translate(r.q.ClearAdminReauthFailures(ctx, uuid(userID)), "clear admin reauth failures")
}
