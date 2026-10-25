// This file holds the money slice's cashflow ports. ports.go lists every
// ports file.

package usecase

import (
	"context"
	"time"
)

// CashflowConfirmation is one "looks right, count it" mark on a finished
// month. Month is the first day of that month. ConfirmedByName is the display
// name of the member who confirmed it, or "" when that member has since left.
type CashflowConfirmation struct {
	Month           time.Time
	ConfirmedByName string
	ConfirmedAt     time.Time
}

// CashflowConfirmationRepository stores the months a household has confirmed.
// Every method is scoped to one household.
type CashflowConfirmationRepository interface {
	// List returns every confirmed month, oldest first.
	List(ctx context.Context, householdID string) ([]CashflowConfirmation, error)
	// Confirm is idempotent: an existing row comes back unchanged, so the
	// first person to confirm is the one kept. The caller passes a month that
	// is already the first of a month; the database refuses anything else.
	Confirm(ctx context.Context, householdID string, month time.Time, membershipID string, at time.Time) (CashflowConfirmation, error)
	// Unconfirm treats a missing row as success, not as ErrNotFound.
	Unconfirm(ctx context.Context, householdID string, month time.Time) error
}
