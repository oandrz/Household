package httpadapter

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// The operator's database browse: the table list and one page of one table.
// Both are reads inside the /admin granted group, so requirePlatformAdmin,
// auditAdmin, requireCSRF and requireAdminGrant apply by construction --
// nothing here checks who is asking.
//
// This is the one surface in Hearth that can read a household's money, which
// is why it costs a re-authentication and why every request through it is an
// audit row. The table name is a path segment and the offset is a query
// parameter, so auditAdmin -- which runs before chi matches the route --
// already records both without this file doing anything.

type browseColumnDTO struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Redacted bool   `json:"redacted"`
}

type browseTableDTO struct {
	Name     string            `json:"name"`
	RowCount int64             `json:"rowCount"`
	Columns  []browseColumnDTO `json:"columns"`
}

type browseTablesResponse struct {
	Tables []browseTableDTO `json:"tables"`
}

type browseRowsResponse struct {
	Table   string            `json:"table"`
	Columns []browseColumnDTO `json:"columns"`
	Rows    [][]string        `json:"rows"`
	Total   int64             `json:"total"`
	Limit   int               `json:"limit"`
	Offset  int               `json:"offset"`
}

// writeBrowseUnconfigured is the answer when DATABASE_READONLY_URL is unset.
// 503 and not 404 on purpose, and the message names the variable because the
// person reading it is the person who can set it.
func writeBrowseUnconfigured(w http.ResponseWriter) {
	WriteError(w, http.StatusServiceUnavailable, "DB_BROWSE_NOT_CONFIGURED",
		"The database browse is not configured on this install. Set DATABASE_READONLY_URL and restart the API.", nil)
}

func browseColumns(columns []usecase.ColumnInfo) []browseColumnDTO {
	out := make([]browseColumnDTO, 0, len(columns))
	for _, c := range columns {
		out = append(out, browseColumnDTO{Name: c.Name, DataType: c.DataType, Redacted: c.Redacted})
	}
	return out
}

// parseBrowseRange reads limit and offset from the query string.
//
// Absent is not an error -- the service has a default, and the operator typed
// a URL rather than filling in a form. Present and unusable is refused before
// any query runs: limit=0 and offset=-1 are not questions the browse can
// answer, and silently reading them as "the default" would answer a different
// question from the one asked. A limit above the cap is neither -- it is
// clamped by the service, not refused here.
func parseBrowseRange(q url.Values) (limit, offset int, ok bool) {
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return 0, 0, false
		}
		limit = n
	}
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, 0, false
		}
		offset = n
	}
	return limit, offset, true
}

func handleAdminDatabaseTables(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.AdminBrowse == nil {
			writeBrowseUnconfigured(w)
			return
		}
		tables, err := deps.AdminBrowse.Tables(r.Context())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		body := browseTablesResponse{Tables: make([]browseTableDTO, 0, len(tables))}
		for _, t := range tables {
			body.Tables = append(body.Tables, browseTableDTO{
				Name: t.Name, RowCount: t.RowCount, Columns: browseColumns(t.Columns),
			})
		}
		WriteJSON(w, http.StatusOK, body)
	}
}

func handleAdminDatabaseRows(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.AdminBrowse == nil {
			writeBrowseUnconfigured(w)
			return
		}
		limit, offset, ok := parseBrowseRange(r.URL.Query())
		if !ok {
			WriteError(w, http.StatusBadRequest, "INVALID_RANGE",
				"limit must be a positive whole number and offset must not be negative.", nil)
			return
		}

		// The table name is not validated here, and that is deliberate:
		// the only honest check is "does the browse's own role see a table
		// with this name", which lives in the adapter and answers
		// domain.ErrNotFound. A regexp here would be a second, weaker rule
		// that could drift from the real one.
		table := chi.URLParam(r, "table")

		page, err := deps.AdminBrowse.Rows(r.Context(), table, limit, offset)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, browseRowsResponse{
			Table:   table,
			Columns: browseColumns(page.Columns),
			Rows:    page.Rows,
			Total:   page.Total,
			Limit:   page.Limit,
			Offset:  page.Offset,
		})
	}
}
