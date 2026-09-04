package usecase

import (
	"context"
	"errors"
)

// AdminBrowseService is the operator's read of the database itself. It is its
// own service rather than more methods on AdminService for the same reason
// AdminDirectoryService and AdminOutboxService are: that service is "who is a
// platform admin, feature flags, the audit log", and this one reads every
// table in the schema through a different connection entirely.
//
// It takes no actor parameter. The /admin guards in the HTTP layer are the
// only gate, as everywhere else in this product -- and here that is worth
// saying twice, because this is the one service in Hearth that can read every
// household's money.
//
// It is deliberately thin. Paging is all it decides; the SQL, the validation
// of a table name and the redaction of a column all belong to the
// implementation of DatabaseBrowser, which is the only layer that can enforce
// them where they actually hold.
type AdminBrowseService struct{ browser DatabaseBrowser }

const (
	// BrowseDefaultLimit is how many rows one page carries when the caller
	// names no limit.
	BrowseDefaultLimit = 50
	// BrowseMaxLimit is the most one page will carry. Its own constant
	// rather than the outbox's or the directory's: the three answer
	// different questions, and sharing one would make a change to any of
	// them move the others.
	BrowseMaxLimit = 100
)

// ErrInvalidOffset is a request the service cannot serve at all, as opposed
// to a limit outside the useful range, which it clamps. Asking for more rows
// than the cap is a reasonable question with a bounded answer; asking to
// start before the beginning is not a question.
var ErrInvalidOffset = errors.New("offset must not be negative")

func NewAdminBrowseService(browser DatabaseBrowser) *AdminBrowseService {
	return &AdminBrowseService{browser: browser}
}

// Tables lists every table the browse's own role can see, including the
// migration bookkeeping and the audit log. Nothing is hidden: an operator
// surface that lied about what is in the database would be worse than no
// surface.
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

// Rows returns one page of one table.
//
// The limit is clamped here rather than in the port, whose contract passes it
// straight through to SQL's LIMIT: a caller-supplied limit reaching that
// unbounded is exactly how one request ends up reading a whole table.
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
