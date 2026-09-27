package postgres

import (
	"context"
	"fmt"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// UnavailableBrowse is what the browse is wired with when OpenReadOnly
// fails for a reason that is NOT a misconfiguration: the database was
// unreachable, the pool couldn't be created, or the read-only privilege
// check itself failed. Those arms of readonly_pool.go don't carry
// ErrReadOnlyMisconfigured, so they don't refuse the boot (main.go's
// openBrowse explains why).
//
// It keeps "the box could not open" and "this install was never
// configured" as two different answers -- nil instead would fire the
// handlers' nil check and send an operator restoring a fresh box to set a
// variable that is already set, instead of to the real fix (the
// hearth_readonly role or the database itself).
//
// Every method answers usecase.ErrBrowseUnavailable (mapped to 503
// DB_BROWSE_UNAVAILABLE): a Liskov-honest implementation, not a stub a
// caller has to special-case.
//
// There is no retry or reconnection -- the pool opens once at boot, and
// recovering is a restart, the same moment the operator learns whether the
// fix worked. It carries the cause so every 503 can log *why*, not just
// *that* -- the startup log's copy may be rotated away by the time someone
// reads a request log. The zero value is valid and answers the bare
// sentinel.
type UnavailableBrowse struct{ cause error }

var _ usecase.DatabaseBrowser = UnavailableBrowse{}

// NewUnavailableBrowse wraps the OpenReadOnly failure that led here. It is
// never a misconfiguration -- those refuse the boot rather than reaching
// this constructor.
func NewUnavailableBrowse(cause error) UnavailableBrowse {
	return UnavailableBrowse{cause: cause}
}

// err is the one answer both methods give: ErrBrowseUnavailable stays the
// sentinel a caller matches with errors.Is, with the boot failure added as
// context -- the same shape browseErr uses for a live pool's failures.
func (u UnavailableBrowse) err() error {
	if u.cause == nil {
		return usecase.ErrBrowseUnavailable
	}
	return fmt.Errorf("%w: the read-only pool was never opened at startup: %v",
		usecase.ErrBrowseUnavailable, u.cause)
}

func (u UnavailableBrowse) Tables(context.Context) ([]usecase.TableInfo, error) {
	return nil, u.err()
}

func (u UnavailableBrowse) Rows(context.Context, string, int, int) (usecase.RowPage, error) {
	return usecase.RowPage{}, u.err()
}
