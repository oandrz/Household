package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// HouseholdUsage counts what a household has stored, for the caps in
// domain/limits.go. Services call it inside their create methods, so every
// channel (browser, hearthctl, API tokens, the Telegram bot) meets the same
// cap.
//
// Count answers every kind in domain.CountedLimitKinds and returns an error
// for any other kind, never a zero: a zero would let the write through.
// Named things count archived rows too. Pending invites are those not yet
// accepted and not expired; active API tokens are those neither revoked nor
// expired.
//
// TransactionsInMonth counts transactions dated in the calendar month that
// month falls in.
type HouseholdUsage interface {
	Count(ctx context.Context, householdID string, kind domain.LimitKind) (int, error)
	TransactionsInMonth(ctx context.Context, householdID string, month time.Time) (int, error)
}
