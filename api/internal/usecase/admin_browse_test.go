package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// browserStub records the limit and offset it was called with, so the tests
// below can assert on what the service asked the port for rather than on what
// it returned.
type browserStub struct {
	tables     []usecase.TableInfo
	page       usecase.RowPage
	err        error
	gotTable   string
	gotLimit   int
	gotOffset  int
	tablesCall int
}

func (b *browserStub) Tables(context.Context) ([]usecase.TableInfo, error) {
	b.tablesCall++
	return b.tables, b.err
}

func (b *browserStub) Rows(_ context.Context, table string, limit, offset int) (usecase.RowPage, error) {
	b.gotTable, b.gotLimit, b.gotOffset = table, limit, offset
	return b.page, b.err
}

// The clamp lives in the service and not in the port, whose contract passes
// the limit straight through to SQL's LIMIT: a caller-supplied limit reaching
// that unbounded is how one request ends up reading a whole table. Same
// reasoning as AdminService.RecentAudit.
func TestRowsClampsTheLimit(t *testing.T) {
	cases := []struct {
		name string
		give int
		want int
	}{
		{"absent becomes the default", 0, usecase.BrowseDefaultLimit},
		{"negative becomes the default", -3, usecase.BrowseDefaultLimit},
		{"a sensible limit is passed through", 10, 10},
		{"the maximum is passed through", usecase.BrowseMaxLimit, usecase.BrowseMaxLimit},
		{"more than the maximum is capped, not refused", 5000, usecase.BrowseMaxLimit},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := &browserStub{}
			svc := usecase.NewAdminBrowseService(stub)

			if _, err := svc.Rows(context.Background(), "accounts", c.give, 0); err != nil {
				t.Fatalf("Rows: %v", err)
			}
			if stub.gotLimit != c.want {
				t.Fatalf("port asked for limit %d, want %d", stub.gotLimit, c.want)
			}
		})
	}
}

// A negative offset is not a request the service can serve, and silently
// treating it as 0 would answer a different question from the one asked.
func TestRowsRefusesANegativeOffset(t *testing.T) {
	stub := &browserStub{}
	svc := usecase.NewAdminBrowseService(stub)

	if _, err := svc.Rows(context.Background(), "accounts", 50, -1); err == nil {
		t.Fatal("Rows accepted a negative offset")
	}
	if stub.gotTable != "" {
		t.Fatal("the port was called with a negative offset")
	}
}

// The service adds nothing to a not-found: the operator must be able to tell
// "no such table" from "the browse is broken", and only the first of those is
// their own typo.
func TestRowsPassesNotFoundThrough(t *testing.T) {
	stub := &browserStub{err: domain.ErrNotFound}
	svc := usecase.NewAdminBrowseService(stub)

	if _, err := svc.Rows(context.Background(), "nope", 50, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v, want domain.ErrNotFound", err)
	}
}

func TestTablesPassesUnavailabilityThrough(t *testing.T) {
	stub := &browserStub{err: usecase.ErrBrowseUnavailable}
	svc := usecase.NewAdminBrowseService(stub)

	if _, err := svc.Tables(context.Background()); !errors.Is(err, usecase.ErrBrowseUnavailable) {
		t.Fatalf("error = %v, want usecase.ErrBrowseUnavailable", err)
	}
}

// A nil slice marshals to JSON null, not [], and the frontend's list
// components break on null -- CLAUDE.md's "every 2xx except 204 carries a
// JSON body" rule, one layer up.
func TestTablesNeverReturnsNil(t *testing.T) {
	svc := usecase.NewAdminBrowseService(&browserStub{tables: nil})

	tables, err := svc.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	if tables == nil {
		t.Fatal("Tables returned a nil slice")
	}
}

// Same contract as TestTablesNeverReturnsNil, for the Rows path: a nil Rows
// or Columns slice marshals to JSON null, not [], and the frontend's table
// view breaks on null.
func TestRowsNeverReturnsNilRowsOrColumns(t *testing.T) {
	stub := &browserStub{page: usecase.RowPage{Rows: nil, Columns: nil}}
	svc := usecase.NewAdminBrowseService(stub)

	page, err := svc.Rows(context.Background(), "accounts", 50, 0)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if page.Rows == nil {
		t.Fatal("Rows returned a nil Rows slice")
	}
	if page.Columns == nil {
		t.Fatal("Rows returned a nil Columns slice")
	}
}
