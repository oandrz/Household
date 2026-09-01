package postgres

import (
	"context"
	"encoding/json"
	"time"

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

func (r *AdminAuditRepo) Recent(ctx context.Context, limit int) ([]usecase.AdminAuditEntry, error) {
	rows, err := r.q.RecentAdminAudit(ctx, int32(limit))
	if err != nil {
		return nil, translate(err, "recent admin audit")
	}
	out := make([]usecase.AdminAuditEntry, 0, len(rows))
	for _, row := range rows {
		detail := map[string]any{}
		// A detail column this code cannot decode must not fail the whole
		// read: the log is for looking at after something went wrong, and
		// that is exactly when a malformed row is most likely.
		_ = json.Unmarshal(row.Detail, &detail)
		out = append(out, usecase.AdminAuditEntry{
			ActorUserID: uuidToString(row.ActorUserID),
			Action:      row.Action,
			Target:      row.Target,
			Detail:      detail,
			IP:          row.Ip,
			At:          timeOf(row.CreatedAt),
		})
	}
	return out, nil
}

type AdminReauthAttemptRepo struct{ q *sqlcgen.Queries }

func NewAdminReauthAttemptRepo(db *DB) *AdminReauthAttemptRepo {
	return &AdminReauthAttemptRepo{q: sqlcgen.New(db.Pool())}
}

func (r *AdminReauthAttemptRepo) Record(ctx context.Context, userID string, succeeded bool, at time.Time) error {
	return translate(r.q.RecordAdminReauthAttempt(ctx, sqlcgen.RecordAdminReauthAttemptParams{
		UserID:    uuid(userID),
		Succeeded: succeeded,
		At:        timestamptz(at),
	}), "record admin reauth attempt")
}

func (r *AdminReauthAttemptRepo) FailuresSince(ctx context.Context, userID string, since time.Time) ([]time.Time, error) {
	rows, err := r.q.AdminReauthFailuresSince(ctx, sqlcgen.AdminReauthFailuresSinceParams{
		UserID: uuid(userID),
		At:     timestamptz(since),
	})
	if err != nil {
		return nil, translate(err, "admin reauth failures since")
	}
	out := make([]time.Time, 0, len(rows))
	for _, at := range rows {
		out = append(out, timeOf(at))
	}
	return out, nil
}

func (r *AdminReauthAttemptRepo) ClearFailures(ctx context.Context, userID string) error {
	return translate(r.q.ClearAdminReauthFailures(ctx, uuid(userID)), "clear admin reauth failures")
}
