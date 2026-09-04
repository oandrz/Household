package postgres

import (
	"context"
	"fmt"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// UnavailableBrowse is what the database browse is wired with when
// OpenReadOnly failed for a reason that is NOT a misconfiguration: the
// database was not reachable, the pool could not be created, or the
// read-only privilege could not be checked at all. Those three are the
// arms of readonly_pool.go that do not carry ErrReadOnlyMisconfigured, so
// they do not refuse the boot -- see main.go's openBrowse for why taking the
// whole household product down over an operator panel would be wrong.
//
// It exists so that "the box could not open the browse" and "this install
// was never given a DATABASE_READONLY_URL" stay two different answers. Left
// nil instead, the service would be nil, the handlers' nil check would fire,
// and an operator restoring onto a fresh box would be told to set a variable
// that is already set -- sending them to edit .env when the real fix is the
// hearth_readonly role or the database itself.
//
// Every method answers usecase.ErrBrowseUnavailable, which the HTTP layer
// maps to 503 DB_BROWSE_UNAVAILABLE. That is the port's contract for "the
// store is there, I just could not reach it", so this is a Liskov-honest
// implementation rather than a stub that returns something a caller would
// have to special-case.
//
// There is deliberately no retry and no reconnection. The pool is opened
// once at boot; recovering from this state is a restart, which is also the
// moment the operator finds out whether they actually fixed anything.
//
// It carries the error that stopped the pool from opening so that every 503
// this produces can log *why*, not just *that*. The startup log records it
// once; an operator reading a request log an hour later, after rotation has
// taken that line, would otherwise have nothing. The zero value is valid and
// answers the bare sentinel.
type UnavailableBrowse struct{ cause error }

var _ usecase.DatabaseBrowser = UnavailableBrowse{}

// NewUnavailableBrowse wraps the OpenReadOnly failure that led here. It is
// never a misconfiguration -- those refuse the boot rather than reaching
// this constructor.
func NewUnavailableBrowse(cause error) UnavailableBrowse {
	return UnavailableBrowse{cause: cause}
}

// err is the one answer both methods give. It keeps ErrBrowseUnavailable as
// the sentinel a caller matches on -- errors.Is walks to it either way -- and
// adds the boot failure as context, the same shape browseErr uses for a live
// pool's failures.
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
