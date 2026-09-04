package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// browseFixture holds both connections to the same container: the read-write
// one only ever used to put rows there, and the read-only one under test.
// One container per Test func, subtests inside -- StartPostgres boots a fresh
// container on every call and there is no reuse.
type browseFixture struct {
	admin *postgres.DB
	repo  *postgres.BrowseRepo
}

func newBrowseFixture(t *testing.T) browseFixture {
	t.Helper()
	ctx := context.Background()
	adminURL := testsupport.StartPostgres(t)

	admin, err := postgres.Open(ctx, adminURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(admin.Close)

	readonly, err := postgres.OpenReadOnly(ctx, testsupport.ReadOnlyURL(t, adminURL))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(readonly.Close)

	return browseFixture{admin: admin, repo: postgres.NewBrowseRepo(readonly)}
}

// A name that is not a table must be answered the same way whether it is a
// typo, a table in another schema, or an attempt to smuggle SQL through the
// URL. The interesting half is the second: these strings must come back as
// ErrNotFound, never as an error from Postgres, because an error from
// Postgres would mean the name reached a query.
func TestRowsRefusesAnythingThatIsNotATable(t *testing.T) {
	f := newBrowseFixture(t)

	for _, name := range []string{
		"no_such_table",
		"pg_catalog.pg_authid",
		`users"; drop table users; --`,
		"users; select 1",
		"'users'",
		"",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.repo.Rows(context.Background(), name, 10, 0)
			if !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("Rows(%q) error = %v, want domain.ErrNotFound", name, err)
			}
		})
	}
}

// The bytes of a redacted column must not be in the answer at all -- not
// hex-encoded, not truncated, not anywhere. This asserts on the whole page
// rather than on the one cell, because "the cell says «redacted»" would still
// pass if the value were also being carried somewhere else.
//
// The whole-page assertion runs first on purpose: it is the one that has to
// go red when the SELECT list stops replacing a redacted column, and an
// equality check on the single cell would fire before it and hide it.
func TestRowsRedactsSecretsAndNeverCarriesTheirBytes(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	// A session row carries token_hash bytea. Anything recognisable in it is
	// what must not appear.
	seedSession(t, f.admin, []byte{0xDE, 0xAD, 0xBE, 0xEF})

	page, err := f.repo.Rows(ctx, "sessions", 10, 0)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}

	whole := strings.ToLower(strings.Join(flatten(page.Rows), "|"))
	for _, forbidden := range []string{"deadbeef", `\xdeadbeef`} {
		if strings.Contains(whole, forbidden) {
			t.Fatalf("the page carries the redacted bytes (%q)", forbidden)
		}
	}

	var redactedColumn = -1
	for i, c := range page.Columns {
		if c.Name == "token_hash" {
			redactedColumn = i
			if !c.Redacted {
				t.Fatal("token_hash is not reported as redacted")
			}
		}
	}
	if redactedColumn < 0 {
		t.Fatal("no token_hash column in the page")
	}
	if page.Rows[0][redactedColumn] != domain.RedactedCell {
		t.Fatalf("token_hash cell = %q, want %q", page.Rows[0][redactedColumn], domain.RedactedCell)
	}
}

// NULL and the empty string are different facts and [][]string cannot carry
// the difference on its own.
//
// The column is users.email rather than users.password_hash, which is what
// this task's brief named: password_hash ends in _hash, so
// domain.ColumnIsRedacted is true for it and it renders «redacted» whether
// its value is NULL or not. That is asserted below too, because it is the
// stronger fact -- a redacted column must not leak "this member has no
// password at all" through the NULL marker. email is NULL for a
// Telegram-only account (UserRepo.Create writes NULL when it is given no
// address) and display_name carries the empty string the NULL marker must
// not be confused with.
func TestRowsDistinguishesNullFromEmpty(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	seedUserWithoutAPassword(t, f.admin)

	page, err := f.repo.Rows(ctx, "users", 10, 0)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if cell := cellOf(t, page, "email", 0); cell != domain.NullCell {
		t.Fatalf("a NULL rendered as %q, want %q", cell, domain.NullCell)
	}
	if cell := cellOf(t, page, "display_name", 0); cell != "" {
		t.Fatalf("an empty string rendered as %q, want an empty string", cell)
	}
	if cell := cellOf(t, page, "password_hash", 0); cell != domain.RedactedCell {
		t.Fatalf("a NULL in a redacted column rendered as %q, want %q", cell, domain.RedactedCell)
	}
}

// OFFSET without ORDER BY lets Postgres return rows in any order it likes,
// and it exercises that permission: page 2 can repeat a row from page 1 and
// skip another entirely, with nothing raising an error. The operator simply
// does not see a row that is there.
//
// The write between the pages is the point, and it is why this read-only
// test writes at all. On a table that is only ever read, one connection at a
// time, an unordered LIMIT/OFFSET happens to return the same heap order to
// every page and the defect stays invisible at any row count. An operator
// pages a live database over seconds in which the product keeps writing, and
// a row rewritten between two clicks moves to the end of the heap: page 2
// then skips a row it never showed and the last page hands back one page 1
// already did. That is the failure this ORDER BY exists to prevent.
func TestRowsPagesWithoutRepeatingOrSkipping(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	seedAccounts(t, f.admin, 5)

	seen := map[string]bool{}
	for offset := 0; offset < 6; offset += 2 {
		page, err := f.repo.Rows(ctx, "accounts", 2, offset)
		if err != nil {
			t.Fatalf("Rows(offset=%d): %v", offset, err)
		}
		if page.Total != 5 {
			t.Fatalf("Total = %d, want 5", page.Total)
		}
		for _, row := range page.Rows {
			id := cellByName(t, page, row, "id")
			if seen[id] {
				t.Fatalf("row %s appeared on two pages", id)
			}
			seen[id] = true
			if offset == 0 {
				rewriteAccount(t, f.admin, id)
			}
		}
	}
	if len(seen) != 5 {
		t.Fatalf("saw %d of 5 rows across the pages", len(seen))
	}
}

// The two orderings the test above does not reach. accounts has a
// single-column primary key, and every other table these tests read does
// too, so neither the multi-column ordering nor the ctid fallback runs
// anywhere else.
//
// Both are real. Two tables in this schema are keyed on a pair of columns
// today -- putting those two back in the order the index declares them is the
// whole job of the array_position expression in orderBy -- and the first
// migration that creates a table without a primary key would otherwise reach
// the ctid branch in production having never run it once.
func TestRowsOrdersByWhateverKeyTheTableHas(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	t.Run("a composite primary key", func(t *testing.T) {
		seedFeatureFlags(t, f.admin, "budgets", "chores", "money")
		assertEveryRowIsPagedOnce(t, f.repo, "household_feature_flags", "key", 3)
	})

	t.Run("no primary key at all", func(t *testing.T) {
		if _, err := f.admin.Pool().Exec(ctx,
			`CREATE TABLE unkeyed (n integer NOT NULL)`); err != nil {
			t.Fatalf("create the unkeyed table: %v", err)
		}
		tag, err := f.admin.Pool().Exec(ctx,
			`INSERT INTO unkeyed (n) SELECT i FROM generate_series(1, 5) AS i`)
		if err != nil {
			t.Fatalf("fill the unkeyed table: %v", err)
		}
		if tag.RowsAffected() != 5 {
			t.Fatalf("fill the unkeyed table wrote %d rows, want 5", tag.RowsAffected())
		}
		assertEveryRowIsPagedOnce(t, f.repo, "unkeyed", "n", 5)
	})
}

// A valid question with a boring answer. A 404 here would be
// indistinguishable from the table not existing, which is a different
// problem.
func TestRowsAnswersAnEmptyPagePastTheEnd(t *testing.T) {
	f := newBrowseFixture(t)

	seedAccounts(t, f.admin, 3)

	page, err := f.repo.Rows(context.Background(), "accounts", 10, 1000)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(page.Rows) != 0 {
		t.Fatalf("got %d rows past the end, want 0", len(page.Rows))
	}
	if page.Total != 3 {
		t.Fatalf("Total = %d, want 3", page.Total)
	}
}

// Nothing is hidden from the list: the migration bookkeeping and the audit
// log are tables like any other. admin_audit_log matters twice over, because
// descoping the /admin/audit screen left this browse as its only UI.
func TestTablesListsEverythingIncludingTheBookkeeping(t *testing.T) {
	f := newBrowseFixture(t)

	tables, err := f.repo.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	byName := map[string]int64{}
	for _, tbl := range tables {
		byName[tbl.Name] = tbl.RowCount
	}
	for _, want := range []string{"households", "users", "admin_audit_log", "goose_db_version"} {
		if _, ok := byName[want]; !ok {
			t.Fatalf("%s is missing from the table list", want)
		}
	}
}

// Decision 6, tested rather than asserted in prose. Validating the table name
// through the read-only pool is supposed to answer a stronger question than
// "does this name exist" -- it answers "and may this connection read it".
// Without this test, TestRowsRefusesAnythingThatIsNotATable proves only that
// six names which exist nowhere are refused, which is trivially true whatever
// pool the lookup runs on.
func TestATableThisRoleCannotReadIsNotFound(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	if _, err := f.admin.Pool().Exec(ctx,
		`CREATE TABLE secret_stuff (id bigint primary key)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	// ALTER DEFAULT PRIVILEGES will have granted SELECT on it already, which
	// is decision 2 working; take it away again to make this table stand for
	// one the grants never reached.
	if _, err := f.admin.Pool().Exec(ctx,
		`REVOKE SELECT ON secret_stuff FROM hearth_readonly`); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if _, err := f.repo.Rows(ctx, "secret_stuff", 10, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Rows on an unreadable table: error = %v, want domain.ErrNotFound", err)
	}

	tables, err := f.repo.Tables(ctx)
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	for _, tbl := range tables {
		if tbl.Name == "secret_stuff" {
			t.Fatal("a table this role cannot read is listed by Tables")
		}
	}
}

// Decision 2, tested directly rather than trusted. A table created after the
// role script ran is the shape of every future migration, and without ALTER
// DEFAULT PRIVILEGES it is invisible here with nothing reporting it.
func TestTablesSeesATableCreatedAfterTheRole(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	if _, err := f.admin.Pool().Exec(ctx,
		`CREATE TABLE later_migration (id bigint primary key, note text)`); err != nil {
		t.Fatalf("create the later table: %v", err)
	}

	tables, err := f.repo.Tables(ctx)
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	for _, tbl := range tables {
		if tbl.Name == "later_migration" {
			return
		}
	}
	t.Fatal("a table created after the role is invisible to the browse: ALTER DEFAULT PRIVILEGES is missing")
}

// seedSession inserts one session, with the household and the user its two
// foreign keys require. Every insert here asserts that it wrote a row: a seed
// helper that silently writes nothing turns a test that reads an empty table
// green for the wrong reason.
func seedSession(t *testing.T, db *postgres.DB, tokenHash []byte) {
	t.Helper()
	ctx := context.Background()
	householdID := insertTestHousehold(t, db)

	var userID string
	// QueryRow with RETURNING is its own assertion: no row inserted means no
	// row to scan, and Scan says so.
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO users (email, display_name, avatar_initial)
		 VALUES ('browse@example.test', 'Browse', 'B')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert the session's user: %v", err)
	}

	tag, err := db.Pool().Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, household_id, expires_at)
		 VALUES ($1, $2, $3, now() + interval '30 days')`, tokenHash, userID, householdID)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("insert session wrote %d rows, want 1", tag.RowsAffected())
	}
}

// seedUserWithoutAPassword inserts the one user TestRowsDistinguishesNullFromEmpty
// reads: email NULL, password_hash NULL and display_name the empty string.
// avatar_initial is char(1) NOT NULL, so it carries a real letter.
func seedUserWithoutAPassword(t *testing.T, db *postgres.DB) {
	t.Helper()
	tag, err := db.Pool().Exec(context.Background(),
		`INSERT INTO users (email, password_hash, display_name, avatar_initial)
		 VALUES (NULL, NULL, '', 'B')`)
	if err != nil {
		t.Fatalf("insert the user: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("insert the user wrote %d rows, want 1", tag.RowsAffected())
	}
}

// seedAccounts inserts n accounts under one household, filling every NOT NULL
// column 00004_accounts.sql declares.
func seedAccounts(t *testing.T, db *postgres.DB, n int) {
	t.Helper()
	householdID := insertTestHousehold(t, db)

	tag, err := db.Pool().Exec(context.Background(),
		`INSERT INTO accounts (household_id, nickname, type, opening_balance_minor,
		                       opening_balance_currency, opening_balance_as_of)
		 SELECT $1, 'Account ' || i, 'cash', i * 100, 'SGD', current_date
		 FROM generate_series(1, $2) AS i`, householdID, n)
	if err != nil {
		t.Fatalf("insert %d accounts: %v", n, err)
	}
	if tag.RowsAffected() != int64(n) {
		t.Fatalf("insert accounts wrote %d rows, want %d", tag.RowsAffected(), n)
	}
}

// seedFeatureFlags inserts one flag per key under one household.
// household_feature_flags is keyed on (household_id, key), which is what
// makes it the composite-key case.
func seedFeatureFlags(t *testing.T, db *postgres.DB, keys ...string) {
	t.Helper()
	householdID := insertTestHousehold(t, db)

	tag, err := db.Pool().Exec(context.Background(),
		`INSERT INTO household_feature_flags (household_id, key, enabled)
		 SELECT $1, k, true FROM unnest($2::text[]) AS k`, householdID, keys)
	if err != nil {
		t.Fatalf("insert %d feature flags: %v", len(keys), err)
	}
	if tag.RowsAffected() != int64(len(keys)) {
		t.Fatalf("insert feature flags wrote %d rows, want %d", tag.RowsAffected(), len(keys))
	}
}

// assertEveryRowIsPagedOnce reads a table two rows at a time and fails unless
// each row shows up on exactly one page, identified by the named column.
func assertEveryRowIsPagedOnce(t *testing.T, repo *postgres.BrowseRepo, table, idColumn string, want int) {
	t.Helper()
	seen := map[string]bool{}
	for offset := 0; offset <= want; offset += 2 {
		page, err := repo.Rows(context.Background(), table, 2, offset)
		if err != nil {
			t.Fatalf("Rows(%s, offset=%d): %v", table, offset, err)
		}
		for _, row := range page.Rows {
			id := cellByName(t, page, row, idColumn)
			if seen[id] {
				t.Fatalf("%s row %s appeared on two pages", table, id)
			}
			seen[id] = true
		}
	}
	if len(seen) != want {
		t.Fatalf("saw %d of %d %s rows across the pages", len(seen), want, table)
	}
}

// rewriteAccount updates one account in place, the way the product does while
// an operator is paging. What it writes does not matter; that the row's live
// version moves to the end of the heap does, because that is the row order an
// unordered LIMIT/OFFSET reads.
func rewriteAccount(t *testing.T, db *postgres.DB, id string) {
	t.Helper()
	tag, err := db.Pool().Exec(context.Background(),
		`UPDATE accounts SET nickname = nickname || ' (renamed)' WHERE id = $1`, id)
	if err != nil {
		t.Fatalf("rewrite account %s: %v", id, err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("rewrite account %s changed %d rows, want 1", id, tag.RowsAffected())
	}
}

// flatten is every cell of a page in one slice, for assertions about the page
// as a whole rather than about one cell.
func flatten(rows [][]string) []string {
	var cells []string
	for _, row := range rows {
		cells = append(cells, row...)
	}
	return cells
}

// cellOf is the cell in the named column of the row at index row.
func cellOf(t *testing.T, page usecase.RowPage, column string, row int) string {
	t.Helper()
	if row >= len(page.Rows) {
		t.Fatalf("the page has %d rows, so there is no row %d to read %q from", len(page.Rows), row, column)
	}
	return cellByName(t, page, page.Rows[row], column)
}

// cellByName is the cell of one already-held row, looked up through the
// page's column list -- RowPage.Rows is column-ordered text and carries no
// names of its own.
func cellByName(t *testing.T, page usecase.RowPage, row []string, column string) string {
	t.Helper()
	for i, c := range page.Columns {
		if c.Name != column {
			continue
		}
		if i >= len(row) {
			t.Fatalf("column %q is at index %d but the row has %d cells", column, i, len(row))
		}
		return row[i]
	}
	t.Fatalf("no %q column in the page", column)
	return ""
}
