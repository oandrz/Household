package postgres

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type NotificationRepo struct{ q *sqlcgen.Queries }

func NewNotificationRepo(db *DB) *NotificationRepo {
	return &NotificationRepo{q: sqlcgen.New(db.Pool())}
}

func (r *NotificationRepo) Get(ctx context.Context, householdID string) (usecase.NotificationPreferences, error) {
	row, err := r.q.GetNotificationPreferences(ctx, uuid(householdID))
	if err != nil {
		return usecase.NotificationPreferences{}, translate(err, "get notification preferences")
	}
	return toNotificationPreferences(row), nil
}

func (r *NotificationRepo) Upsert(ctx context.Context, householdID string, p usecase.NotificationPreferences) (usecase.NotificationPreferences, error) {
	row, err := r.q.UpsertNotificationPreferences(ctx, sqlcgen.UpsertNotificationPreferencesParams{
		HouseholdID:     uuid(householdID),
		BillReminders:   p.BillReminders,
		OverspendAlerts: p.OverspendAlerts,
		RetroReminder:   p.RetroReminder,
		WeeklyDigest:    p.WeeklyDigest,
	})
	if err != nil {
		return usecase.NotificationPreferences{}, translate(err, "upsert notification preferences")
	}
	return toNotificationPreferences(row), nil
}
