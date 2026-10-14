package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RetentionWindow is how long Hearth keeps rows that exist only to answer a
// short-lived question: a sign-in attempt, a link that was used or has
// lapsed, an invite nobody accepted. The API prunes past it once a day.
const RetentionWindow = 30 * 24 * time.Hour

// RetentionFloor is the shortest window Prune accepts. It is far outside
// domain.LockoutPolicy.Window (15 minutes), because deleting a login attempt
// still inside that window would clear a live lockout -- a cleanup that
// unlocks a household while it is being guessed at. Enforced, not
// documented, so no plausible-looking flag value can reach that state.
const RetentionFloor = 7 * 24 * time.Hour

// ErrRetentionWindowTooShort is Prune's refusal of a window under
// RetentionFloor. Nothing has been deleted when it is returned.
var ErrRetentionWindowTooShort = errors.New("retention window is under the floor")

// Pruner is the one method retention needs from each table it cleans. Each
// repository's own port says which of its rows Prune may delete.
type Pruner interface {
	Prune(ctx context.Context, before time.Time) (int64, error)
}

type RetentionDeps struct {
	Signups       Pruner
	LoginAttempts Pruner
	TelegramLinks Pruner
	MagicLinks    Pruner
	Invites       Pruner
	Clock         Clock
}

// RetentionService is the one definition of what Hearth deletes because it
// is old. The API runs it daily with RetentionWindow; `adminctl prune` runs
// it by hand with a window of the operator's choosing. admin_audit_log is
// not here and must never be: a log the operator can edit stops meaning
// anything.
type RetentionService struct{ d RetentionDeps }

func NewRetentionService(d RetentionDeps) *RetentionService { return &RetentionService{d: d} }

// PruneCounts is how many rows each table lost.
type PruneCounts struct {
	Signups       int64
	LoginAttempts int64
	TelegramLinks int64
	MagicLinks    int64
	Invites       int64
}

// Prune deletes, from every table it knows, the rows older than olderThan
// that its repository says may go. A window under RetentionFloor is refused
// before anything is touched.
//
// One table failing does not stop the others: each delete stands alone and
// the next run repeats it, so a partial prune is a smaller prune, never a
// broken one. The error names every table that failed.
func (s *RetentionService) Prune(ctx context.Context, olderThan time.Duration) (PruneCounts, error) {
	if olderThan < RetentionFloor {
		return PruneCounts{}, fmt.Errorf("%w: must be at least %d days, because pruning login attempts "+
			"inside the lockout window would clear a live lockout",
			ErrRetentionWindowTooShort, int(RetentionFloor.Hours()/24))
	}
	before := s.d.Clock.Now().Add(-olderThan)

	var counts PruneCounts
	var errs []error
	for _, table := range []struct {
		name  string
		rows  Pruner
		count *int64
	}{
		{"signups", s.d.Signups, &counts.Signups},
		{"login attempts", s.d.LoginAttempts, &counts.LoginAttempts},
		{"telegram link requests", s.d.TelegramLinks, &counts.TelegramLinks},
		{"magic links", s.d.MagicLinks, &counts.MagicLinks},
		{"invites", s.d.Invites, &counts.Invites},
	} {
		n, err := table.rows.Prune(ctx, before)
		if err != nil {
			errs = append(errs, fmt.Errorf("prune %s: %w", table.name, err))
			continue
		}
		*table.count = n
	}
	return counts, errors.Join(errs...)
}
