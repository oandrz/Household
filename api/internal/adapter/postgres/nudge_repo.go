package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

var _ usecase.NudgeRepository = (*NudgeRepo)(nil)

type NudgeRepo struct{ q *sqlcgen.Queries }

func NewNudgeRepo(db *DB) *NudgeRepo { return &NudgeRepo{q: sqlcgen.New(db.Pool())} }

func (r *NudgeRepo) Recipients(ctx context.Context) ([]usecase.NudgeRecipient, error) {
	rows, err := r.q.ListNudgeRecipients(ctx)
	if err != nil {
		return nil, translate(err, "list nudge recipients")
	}
	out := make([]usecase.NudgeRecipient, 0, len(rows))
	for _, row := range rows {
		out = append(out, usecase.NudgeRecipient{
			ChatID:       row.ChatID,
			HouseholdID:  uuidToString(row.HouseholdID),
			MembershipID: uuidToString(row.MembershipID),
			Currency:     row.PrimaryCurrency,
		})
	}
	return out, nil
}

func (r *NudgeRepo) Claim(ctx context.Context, chatID int64, householdID string, day time.Time) (bool, error) {
	n, err := r.q.ClaimNudge(ctx, sqlcgen.ClaimNudgeParams{ChatID: chatID, HouseholdID: uuid(householdID), Day: date(day)})
	if err != nil {
		return false, translate(err, "claim nudge")
	}
	return n == 1, nil
}

func (r *NudgeRepo) Release(ctx context.Context, chatID int64, householdID string, day time.Time) error {
	return translate(r.q.ReleaseNudge(ctx, sqlcgen.ReleaseNudgeParams{ChatID: chatID, HouseholdID: uuid(householdID), Day: date(day)}), "release nudge")
}

func (r *NudgeRepo) SetEnabled(ctx context.Context, chatID int64, enabled bool) error {
	n, err := r.q.SetNudgesEnabled(ctx, sqlcgen.SetNudgesEnabledParams{ChatID: chatID, NudgesEnabled: enabled})
	if err != nil {
		return translate(err, "set nudges enabled")
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *NudgeRepo) Prune(ctx context.Context, before time.Time) (int64, error) {
	n, err := r.q.PruneNudgeDeliveries(ctx, date(before))
	if err != nil {
		return 0, translate(err, "prune nudge deliveries")
	}
	return n, nil
}

// date is the calendar day of t in t's own location: the caller has already
// decided which day "today" is, and the column stores that decision.
func date(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}
