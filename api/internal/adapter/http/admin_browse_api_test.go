package httpadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// stubBrowser is the usecase.DatabaseBrowser a configured test env holds.
// Each test sets only the field it cares about.
//
// calls is not decoration. Two of the answers this file asserts on --
// NOT_FOUND for an unknown table, and INVALID_RANGE for a range that cannot
// be served -- are bodies the router already produces for a path that does
// not exist at all, so without a count of how many questions actually
// reached the browser, both tests pass against a build with no routes in it.
// Counting them is what makes "the route matched and the browse answered"
// distinguishable from "chi's NotFound answered", and what proves an
// unusable range is refused BEFORE any query runs.
type stubBrowser struct {
	tables []usecase.TableInfo
	page   usecase.RowPage
	err    error

	calls int
}

func (s *stubBrowser) Tables(context.Context) ([]usecase.TableInfo, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.tables, nil
}

// Rows echoes back the limit and offset it was handed, exactly as
// postgres.BrowseRepo does. That is what lets the clamp test assert on the
// body without this double knowing the cap exists: the service clamps, and
// whatever it decided arrives here and comes straight back out.
func (s *stubBrowser) Rows(_ context.Context, _ string, limit, offset int) (usecase.RowPage, error) {
	s.calls++
	if s.err != nil {
		return usecase.RowPage{}, s.err
	}
	page := s.page
	page.Limit = limit
	page.Offset = offset
	return page, nil
}

// sampleBrowse is a small database as the browse sees it: two tables, one
// redacted column, one row. The redacted column is deliberately present in
// both the column list and the row, because "you may not see this value" is
// a fact the screen has to receive and a DTO can silently drop.
func sampleBrowse() *stubBrowser {
	columns := []usecase.ColumnInfo{
		{Name: "id", DataType: "uuid"},
		{Name: "display_name", DataType: "text"},
		{Name: "password_hash", DataType: "text", Redacted: true},
	}
	return &stubBrowser{
		tables: []usecase.TableInfo{
			{Name: "households", RowCount: 3, Columns: columns},
			{Name: "accounts", RowCount: 12, Columns: columns},
		},
		page: usecase.RowPage{
			Columns: columns,
			Rows:    [][]string{{"5b2f1e30-0f0a-4c1f-9a44-2e7c0e5a1a01", "Andreas", domain.RedactedCell}},
			Total:   12,
		},
	}
}

// browseRouter builds a router sharing every one of env's dependencies except
// AdminBrowse, which is the browse service wrapped around the double passed
// in. It is the seam adminRouterWith already established, used here for a
// reason particular to this feature: env.router is the UNCONFIGURED install
// (Deps.AdminBrowse nil), which is the state the two 503 tests need, so a
// configured router has to be built beside it rather than replacing it.
//
// The grant lives in Postgres, not in the router, so a session granted
// through env.router opens this one too.
func (env *testEnv) browseRouter(browser usecase.DatabaseBrowser) http.Handler {
	d := env.deps
	d.AdminBrowse = usecase.NewAdminBrowseService(browser)
	return httpadapter.NewRouter(d)
}

// The key sets are asserted exactly, the same way the mail and households
// tests assert theirs: Task 8 mirrors these shapes in Zod, so a field added
// or renamed by accident must fail here rather than reach a frontend that
// parses it into nothing.
func TestAdminDatabaseTablesAnswersTheTableList(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)
	browser := sampleBrowse()
	router := env.browseRouter(browser)

	rec := get(t, router, "/api/v1/admin/db/tables", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	assertKeys(t, "top level", rec.Body.Bytes(), "tables")

	var body struct {
		Tables []json.RawMessage `json:"tables"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tables) != 2 {
		t.Fatalf("tables = %d, want 2", len(body.Tables))
	}
	assertKeys(t, "table", body.Tables[0], "name", "rowCount", "columns")

	var first struct {
		Name     string            `json:"name"`
		RowCount int64             `json:"rowCount"`
		Columns  []json.RawMessage `json:"columns"`
	}
	if err := json.Unmarshal(body.Tables[0], &first); err != nil {
		t.Fatalf("decode the first table: %v", err)
	}
	if first.Name != "households" {
		t.Fatalf("the first table is %q, want households", first.Name)
	}
	if first.RowCount != 3 {
		t.Fatalf("households rowCount = %d, want 3", first.RowCount)
	}
	if len(first.Columns) != 3 {
		t.Fatalf("households has %d columns, want 3", len(first.Columns))
	}
	assertKeys(t, "column", first.Columns[0], "name", "dataType", "redacted")

	var redacted struct {
		Name     string `json:"name"`
		DataType string `json:"dataType"`
		Redacted bool   `json:"redacted"`
	}
	if err := json.Unmarshal(first.Columns[2], &redacted); err != nil {
		t.Fatalf("decode the redacted column: %v", err)
	}
	if redacted.Name != "password_hash" || redacted.DataType != "text" {
		t.Fatalf("column = %+v, want password_hash/text", redacted)
	}
	if !redacted.Redacted {
		t.Fatal("password_hash arrived with redacted = false: the screen cannot tell a withheld " +
			"value from an absent one")
	}
}

func TestAdminDatabaseRowsPages(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)
	browser := sampleBrowse()
	router := env.browseRouter(browser)

	rec := get(t, router, "/api/v1/admin/db/tables/accounts?limit=10&offset=20", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	assertKeys(t, "top level", rec.Body.Bytes(), "table", "columns", "rows", "total", "limit", "offset")

	var body struct {
		Table   string            `json:"table"`
		Columns []json.RawMessage `json:"columns"`
		Rows    [][]string        `json:"rows"`
		Total   int64             `json:"total"`
		Limit   int               `json:"limit"`
		Offset  int               `json:"offset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Table != "accounts" {
		t.Fatalf("table = %q, want accounts -- the path segment is what names the page", body.Table)
	}
	if body.Limit != 10 || body.Offset != 20 {
		t.Fatalf("limit/offset = %d/%d, want 10/20", body.Limit, body.Offset)
	}
	if body.Total != 12 {
		t.Fatalf("total = %d, want 12 -- the whole table, not the page", body.Total)
	}
	if len(body.Rows) != 1 || len(body.Rows[0]) != 3 {
		t.Fatalf("rows = %#v, want one row of three cells", body.Rows)
	}
	if body.Rows[0][2] != domain.RedactedCell {
		t.Fatalf("the redacted cell arrived as %q, want %q", body.Rows[0][2], domain.RedactedCell)
	}
	assertKeys(t, "column", body.Columns[0], "name", "dataType", "redacted")
}

// Absent is not an error -- the service has a default, and the operator typed
// a URL rather than filling in a form. Present and unusable is refused, and
// refused before any query runs, which is what browser.calls asserts.
func TestAdminDatabaseRowsRefusesAnUnusableRange(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)
	browser := sampleBrowse()
	router := env.browseRouter(browser)

	for _, query := range []string{"?limit=abc", "?limit=0", "?limit=-1", "?offset=-1", "?offset=x"} {
		t.Run(query, func(t *testing.T) {
			rec := get(t, router, "/api/v1/admin/db/tables/accounts"+query, session)
			assertErrorResponse(t, rec, http.StatusBadRequest, "INVALID_RANGE")
			if browser.calls != 0 {
				t.Fatalf("the browse was asked %d questions for %q; an unusable range must be "+
					"refused before any query runs", browser.calls, query)
			}
		})
	}
}

// Over the cap is clamped, not refused: asking for more than the maximum is a
// reasonable request with a bounded answer. The double echoes back whatever
// limit reached it, so a body saying 100 is the service's clamp and nothing
// else.
func TestAdminDatabaseRowsClampsALargeLimit(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)
	router := env.browseRouter(sampleBrowse())

	rec := get(t, router, "/api/v1/admin/db/tables/accounts?limit=5000", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s -- a limit over the cap is clamped, not refused",
			rec.Code, rec.Body.String())
	}

	var body struct {
		Limit int `json:"limit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Limit != usecase.BrowseMaxLimit {
		t.Fatalf("limit = %d, want %d", body.Limit, usecase.BrowseMaxLimit)
	}
}

// The 404 for a table that is not there, which the adapter answers with
// domain.ErrNotFound.
//
// The calls assertion is what gives this test teeth. writeNotFound and this
// 404 deliberately share one body (router.go's own comment says why), so
// before the route existed the assertion on the envelope alone was already
// green against chi's NotFound. Counting the question proves the request
// reached the browse and came back, rather than never being routed at all.
func TestAdminDatabaseUnknownTableIs404(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)
	browser := &stubBrowser{err: domain.ErrNotFound}
	router := env.browseRouter(browser)

	rec := get(t, router, "/api/v1/admin/db/tables/no_such_table", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
	if browser.calls != 1 {
		t.Fatalf("the browse was asked %d questions, want 1 -- a 404 that never reached the "+
			"browse is chi's, not this route's", browser.calls)
	}
}

// 503 and not 404: everyone who reaches these handlers has already proved
// they are a platform admin with a live grant, so hiding the route from them
// would cost them the one fact that says what to fix. The message names the
// variable because the person reading it is the person who can set it.
//
// A subtest per route, not one loop body, so that a guard lost from one
// handler fails only that route's subtest -- the two handlers are guarded
// separately and this is where that shows.
func TestAdminDatabaseAnswers503WhenUnconfigured(t *testing.T) {
	env := newTestEnv(t) // no browse: Deps.AdminBrowse is nil
	session := grantedAdmin(t, env)

	for _, path := range []string{
		"/api/v1/admin/db/tables",
		"/api/v1/admin/db/tables/accounts",
	} {
		t.Run(path, func(t *testing.T) {
			rec := env.authedGet(t, path, session)
			body := assertErrorResponse(t, rec, http.StatusServiceUnavailable, "DB_BROWSE_NOT_CONFIGURED")
			if !strings.Contains(body.Error.Message, "DATABASE_READONLY_URL") {
				t.Fatalf("message = %q, want it to name DATABASE_READONLY_URL", body.Error.Message)
			}
		})
	}
}

// Configured and broken is a different event from not configured, and gets a
// different code: "no value set" sends the operator to .env, "the value is
// set and the connection is broken" sends them to the box and the
// hearth_readonly role. Collapsing the two would send them to the wrong one.
//
// The log assertion is the other half, and it is not decoration. The response
// body is deliberately generic, so the log line is the ONLY place the cause
// survives -- browse_repo.go's browseErr wraps the failing operation and the
// pg error into this sentinel precisely so this layer can record them. The
// stub's error carries an operation phrase for exactly that reason: asserting
// only that "something was logged" would still pass if the branch logged the
// bare sentinel and threw the cause away.
func TestAdminDatabaseAnswers503WhenTheBrowseIsBroken(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)
	broken := fmt.Errorf("%w: count rows: dial tcp 127.0.0.1:5432: connection refused",
		usecase.ErrBrowseUnavailable)
	router := env.browseRouter(&stubBrowser{err: broken})

	for _, path := range []string{
		"/api/v1/admin/db/tables",
		"/api/v1/admin/db/tables/accounts",
	} {
		t.Run(path, func(t *testing.T) {
			var buf bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })

			rec := get(t, router, path, session)
			assertErrorResponse(t, rec, http.StatusServiceUnavailable, "DB_BROWSE_UNAVAILABLE")

			logged := buf.String()
			if !strings.Contains(logged, "database browse unavailable") {
				t.Fatalf("nothing was logged for a 503 whose body carries no cause\ngot: %s", logged)
			}
			if !strings.Contains(logged, "count rows") {
				t.Fatalf("the log dropped the operation the adapter wrapped in for it\ngot: %s", logged)
			}
			if !strings.Contains(logged, "connection refused") {
				t.Fatalf("the log dropped the underlying cause\ngot: %s", logged)
			}
		})
	}
}

// The audit row is what makes reading a household's money an act with a
// record. auditAdmin writes before chi matches the route, so the table name
// is in the path and the offset is in the query string -- this asserts on the
// row, not on the middleware.
//
// The 200 is checked first and is load-bearing: a 404 leaves an audit row
// too, so without it this test would pass against a build with no route at
// all and the name would be a lie.
func TestReadingRowsLeavesAnAuditRowNamingTheTableAndOffset(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)
	router := env.browseRouter(sampleBrowse())

	rec := get(t, router, "/api/v1/admin/db/tables/accounts?limit=10&offset=20", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	entries, err := env.adminAudit.Recent(context.Background(), 1)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Recent(1) returned %d entries, want 1", len(entries))
	}
	if entries[0].Action != "GET /api/v1/admin/db/tables/accounts" {
		t.Fatalf("Action = %q", entries[0].Action)
	}
	if entries[0].Target != "/api/v1/admin/db/tables/accounts" {
		t.Fatalf("Target = %q, want the path naming the table that was read", entries[0].Target)
	}
	query, ok := entries[0].Detail["query"].(string)
	if !ok {
		t.Fatalf("Detail[query] = %v (%T), want the raw query string", entries[0].Detail["query"], entries[0].Detail["query"])
	}
	if !strings.Contains(query, "offset=20") {
		t.Fatalf("Detail[query] = %q, want it to record which page was read", query)
	}
}
