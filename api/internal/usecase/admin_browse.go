package usecase

import (
	"context"
	"errors"
)

// AdminBrowseService is the operator's read of the database itself -- its
// own service (like AdminDirectoryService, AdminOutboxService) because
// AdminService covers admin/flags/audit, not raw tables. No actor
// parameter: the /admin guards are the only gate -- worth repeating since
// this is the one service that can read every household's money. It stays
// thin: only paging is decided here; SQL, table validation and column
// redaction belong to DatabaseBrowser alone.
type AdminBrowseService struct{ browser DatabaseBrowser }

const (
	// BrowseDefaultLimit is how many rows one page carries when the caller
	// names no limit.
	BrowseDefaultLimit = 50
	// BrowseMaxLimit is the most one page will carry -- its own constant,
	// not the outbox's or the directory's, so changing one never moves the
	// others.
	BrowseMaxLimit = 100
)

// ErrInvalidOffset is a request the service cannot serve at all -- unlike
// an out-of-range limit, which it clamps, a negative offset isn't a
// question with a bounded answer.
var ErrInvalidOffset = errors.New("offset must not be negative")

func NewAdminBrowseService(browser DatabaseBrowser) *AdminBrowseService {
	return &AdminBrowseService{browser: browser}
}

// Tables lists every table the browse role can see, migration bookkeeping
// and the audit log included. Nothing hidden -- a surface that lied about
// the database would be worse than none.
func (s *AdminBrowseService) Tables(ctx context.Context) ([]TableInfo, error) {
	tables, err := s.browser.Tables(ctx)
	if err != nil {
		return nil, err
	}
	if tables == nil {
		tables = []TableInfo{}
	}
	return tables, nil
}

// Rows returns one page of one table. The limit is clamped here, not in
// the port (whose contract passes it straight to SQL's LIMIT) -- unbounded,
// a caller-supplied limit is exactly how one request reads a whole table.
func (s *AdminBrowseService) Rows(ctx context.Context, table string, limit, offset int) (RowPage, error) {
	if offset < 0 {
		return RowPage{}, ErrInvalidOffset
	}
	switch {
	case limit <= 0:
		limit = BrowseDefaultLimit
	case limit > BrowseMaxLimit:
		limit = BrowseMaxLimit
	}

	page, err := s.browser.Rows(ctx, table, limit, offset)
	if err != nil {
		return RowPage{}, err
	}
	if page.Rows == nil {
		page.Rows = [][]string{}
	}
	if page.Columns == nil {
		page.Columns = []ColumnInfo{}
	}
	return page, nil
}
