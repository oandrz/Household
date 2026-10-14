package usecase

import (
	"context"
	"log/slog"
	"time"
)

// failureWithdrawer is the one method withdrawReservation needs, so the
// sign-in ledger and the admin prompt's ledger can share it.
type failureWithdrawer interface {
	Withdraw(ctx context.Context, attemptID string) error
}

// withdrawTimeout bounds the cleanup below, which no request is waiting on.
const withdrawTimeout = 5 * time.Second

// withdrawReservation removes a reserved failure whose password check never
// ran. It keeps ctx's values but not its cancellation: the usual reason to
// be here is that the caller has gone, and their context ended with them.
//
// A failed withdrawal is logged, not returned. The caller is already
// answering "busy", and the stray row costs one strike that expires with
// the lockout window.
func withdrawReservation(ctx context.Context, ledger failureWithdrawer, attemptID string) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), withdrawTimeout)
	defer cancel()
	if err := ledger.Withdraw(cleanup, attemptID); err != nil {
		slog.ErrorContext(ctx, "withdraw reserved sign-in failure", "error", err)
	}
}
