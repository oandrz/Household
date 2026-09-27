package postgres_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// browseFixture holds both connections to the same container: read-write to
// seed rows, read-only under test. One container per Test func (subtests
// inside) -- StartPostgres boots a fresh container each call, no reuse.
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

// A name that is not a table -- a typo, a table in another schema, or an
// attempt to smuggle SQL through the URL -- must always come back as
// ErrNotFound, never as an error from Postgres: a Postgres error would mean
// the name reached a query.
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

// The bytes of a redacted column must not appear anywhere in the page, not
// hex-encoded or truncated -- so this checks the whole page, not one cell,
// which could still pass while the value leaked elsewhere. The whole-page
// check runs first for the same reason: it must go red when the SELECT
// list stops redacting, and a single-cell check would catch that first and
// hide the failure.
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

// NULL and the empty string are different facts and [][]string cannot
// carry the difference on its own.
//
// The NULL check uses users.email, not password_hash: password_hash ends
// in _hash, so it is redacted and renders «redacted» whether it is NULL or
// not. That is asserted too, and it is the stronger fact -- a redacted
// column must never leak "this member has no password" through the NULL
// marker. email is NULL for a Telegram-only account (UserRepo.Create
// writes NULL when given no address); display_name carries the empty
// string the NULL marker must not be confused with.
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

// OFFSET without ORDER BY lets Postgres return rows in any order: page 2
// can repeat a row from page 1 and skip another, with nothing raising an
// error and the operator not seeing a row that is there.
//
// The write between the pages is why this read-only test writes at all: on
// a table nobody else touches, an unordered LIMIT/OFFSET happens to return
// the same heap order every time, hiding the defect. A live operator pages
// while the product keeps writing, and a row rewritten between clicks moves
// to the end of the heap -- page 2 skips it and the last page repeats what
// page 1 already showed. That is the failure this ORDER BY prevents.
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

// The two orderings no other test reaches: every table here has a
// single-column primary key, so neither the multi-column case nor the
// ctid fallback runs anywhere else.
//
// Both are real: two tables today have a composite key (what
// array_position in orderBy exists for), and the first no-primary-key
// migration would otherwise hit ctid for the first time in production.
func TestRowsOrdersByWhateverKeyTheTableHas(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	t.Run("a composite primary key", func(t *testing.T) {
		// Seeded out of key order deliberately. household_feature_flags is
		// keyed (household_id, key), so the ordering array_position has to
		// produce is the keys alphabetically -- only an insert that
		// disagrees with it can tell that apart from doing nothing. Don't
		// seed in key order: a static heap reads back in insertion order
		// with or without the ORDER BY, so that would only prove the
		// int2vector cast is valid SQL, not that ordering works.
		seedFeatureFlags(t, f.admin, "money", "budgets", "chores")

		got := assertEveryRowIsPagedOnce(t, f.repo, "household_feature_flags", "key", 3)
		if want := []string{"budgets", "chores", "money"}; !slices.Equal(got, want) {
			t.Fatalf("keys came back in the order %v, want %v", got, want)
		}
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

// Nothing is hidden from the list: migration bookkeeping and the audit log
// are tables like any other. admin_audit_log matters twice over, since
// there is no /admin/audit screen, so this browse is its only UI.
//
// Counts are asserted by value, not merely read: rowCounts binds its
// answers back to table names POSITIONALLY, so only a value check (not a
// mere type check) can catch a shifted binding. Three distinct numbers are
// needed for that -- two tables filled to different depths, one untouched.
func TestTablesListsEverythingIncludingTheBookkeeping(t *testing.T) {
	f := newBrowseFixture(t)

	// One household holding three accounts, so households and accounts carry
	// counts that differ from each other and from the empty tables around them.
	seedAccounts(t, f.admin, 3)

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
	for _, want := range []struct {
		table string
		count int64
	}{
		{"accounts", 3},
		{"households", 1},
		{"sessions", 0},
	} {
		if got := byName[want.table]; got != want.count {
			t.Errorf("%s RowCount = %d, want %d (a count bound to the wrong table name?)",
				want.table, got, want.count)
		}
	}
}

// The test the design spec calls the one easy to omit
// (docs/superpowers/specs/2026-09-04-hearth-database-browse-design.md §9):
// a redaction sweep driven by the schema itself, not a hand-typed list.
// domain/dbbrowse_test.go is that typed list, and it can only ever be as
// complete as its author's memory -- it says nothing about a column a
// migration adds next year.
//
// The oracle is the CATALOGUE, read on the admin connection, deliberately
// not the same expression the code under test uses. It resolves each
// column's type through pg_type -- an array to its element, a domain to its
// base, repeatedly -- and asks whether pg_catalog.bytea appears anywhere in
// that chain. That matters twice:
//
//   - information_schema.data_type would be the obvious oracle and is the
//     wrong one: it reports a CATEGORY, not a name, for exactly the shapes
//     this test exists to catch (bytea[] reads "ARRAY", a domain over bytea
//     reads "USER-DEFINED"). An oracle built on it would certify the blind
//     spot instead of finding it.
//   - the chain resolves domains, which ColumnIsRedacted cannot -- it is
//     stdlib-only, and typbasetype is a catalogue read. No column here is a
//     domain over bytea today, so oracle and code agree and this test is
//     green; the day a migration adds one, this test goes red, which is why
//     ColumnIsRedacted's doc comment is allowed to describe that gap rather
//     than close it.
//
// The migrated schema alone can't carry this test: every real bytea column
// is also named *_hash, so the name rule alone would keep a schema-wide
// sweep green even with the type rule deleted. type_shapes supplies two
// bytea columns under names no name rule matches, making the type rule
// load-bearing.
func TestEveryColumnTheCatalogueCallsSecretIsRedacted(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	seedTypeShapes(t, f.admin, []byte{0xC0, 0xFF, 0xEE, 0x01}, []byte{0xC0, 0xFF, 0xEE, 0x02})

	reported := map[string]map[string]usecase.ColumnInfo{}
	tables, err := f.repo.Tables(ctx)
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	for _, tbl := range tables {
		byColumn := map[string]usecase.ColumnInfo{}
		for _, c := range tbl.Columns {
			byColumn[c.Name] = c
		}
		reported[tbl.Name] = byColumn
	}

	onlyTheTypeRuleCatchesIt := 0
	for _, col := range secretColumnsFromCatalogue(t, f.admin) {
		byColumn, ok := reported[col.table]
		if !ok {
			t.Errorf("%s is in the catalogue but not in the browse's table list", col.table)
			continue
		}
		got, ok := byColumn[col.column]
		if !ok {
			t.Errorf("%s.%s is in the catalogue but not in the browse's column list", col.table, col.column)
			continue
		}
		mustRedact := col.isBytea || col.nameSaysSecret
		if mustRedact && !got.Redacted {
			t.Errorf("%s.%s (%s) is not redacted, and it must be: bytea in its type chain = %v, name says secret = %v",
				col.table, col.column, col.resolved, col.isBytea, col.nameSaysSecret)
		}
		if col.isBytea && !col.nameSaysSecret {
			onlyTheTypeRuleCatchesIt++
		}
	}

	// Without this the sweep could pass by finding nothing to check: an
	// oracle whose SQL quietly matched no rows looks exactly like a clean
	// schema. These two columns are the ones only the type rule catches, so
	// deleting it would show up here.
	if onlyTheTypeRuleCatchesIt < 2 {
		t.Fatalf("the sweep found %d columns that only the type rule catches, want at least 2 "+
			"(type_shapes.raw and type_shapes.blobs) -- the oracle is matching nothing",
			onlyTheTypeRuleCatchesIt)
	}
	// A column that is neither bytea nor named like a secret must NOT be
	// withheld: over-redaction on this screen looks like data loss.
	if note := reported["type_shapes"]["note"]; note.Redacted {
		t.Error("type_shapes.note is redacted, and nothing about it is secret")
	}

	// The same two columns through the other path: Tables() reads columns
	// with allColumns, Rows() with columnsOf, and only checking both keeps
	// them from drifting. columnsOf is the one that decides what the SELECT
	// list withholds, so it is the one whose mistake would put real bytes on
	// a screen.
	page, err := f.repo.Rows(ctx, "type_shapes", 10, 0)
	if err != nil {
		t.Fatalf("Rows(type_shapes): %v", err)
	}
	whole := strings.ToLower(strings.Join(flatten(page.Rows), "|"))
	if strings.Contains(whole, "c0ffee") {
		t.Fatalf("the page carries the seeded bytes: %q", whole)
	}
	for _, column := range []string{"raw", "blobs"} {
		if cell := cellOf(t, page, column, 0); cell != domain.RedactedCell {
			t.Errorf("type_shapes.%s cell = %q, want %q", column, cell, domain.RedactedCell)
		}
	}
}

// Validating the table name through the read-only pool is supposed to
// answer a stronger question than "does this name exist": "and may this
// connection read it". This test proves that in code rather than in prose
// -- without it, TestRowsRefusesAnythingThatIsNotATable only proves that
// six names which exist nowhere are refused, true whatever pool the lookup
// runs on.
func TestATableThisRoleCannotReadIsNotFound(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	if _, err := f.admin.Pool().Exec(ctx,
		`CREATE TABLE secret_stuff (id bigint primary key)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	// ALTER DEFAULT PRIVILEGES will have granted SELECT on it already; revoke
	// it again so this table stands for one the grants never reached.
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

// ALTER DEFAULT PRIVILEGES, tested directly rather than trusted: a table
// created after the role script ran is the shape of every future
// migration, invisible here without it.
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

// seedSession inserts one session, with the household and user its two
// foreign keys require. Every insert here asserts it wrote a row: a seed
// helper that silently writes nothing turns a read of an empty table green
// for the wrong reason.
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

// seedTypeShapes creates the table the schema sweep needs and the migrations
// don't have: bytea under a column name no name rule matches, in both a
// plain and an array shape.
//
// raw is bytea with an innocent name -- every migration's bytea column is
// also named *_hash, so without it the name rule alone would carry the
// whole sweep. blobs is bytea[], the shape data_type can only report as
// "ARRAY". note is the control: nothing about it is secret, and it must
// come back visible.
func seedTypeShapes(t *testing.T, db *postgres.DB, raw, blob []byte) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Pool().Exec(ctx, `
		CREATE TABLE type_shapes (
			id     bigint PRIMARY KEY,
			raw    bytea,
			blobs  bytea[],
			note   text
		)`); err != nil {
		t.Fatalf("create type_shapes: %v", err)
	}
	tag, err := db.Pool().Exec(ctx,
		`INSERT INTO type_shapes (id, raw, blobs, note) VALUES (1, $1, ARRAY[$2::bytea], 'ordinary')`,
		raw, blob)
	if err != nil {
		t.Fatalf("fill type_shapes: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("fill type_shapes wrote %d rows, want 1", tag.RowsAffected())
	}
}

// catalogueColumn is one column as the catalogue itself describes it,
// independently of anything the browse believes about it.
type catalogueColumn struct {
	table, column string
	// resolved is the type chain the oracle walked, e.g. "bytea[] -> bytea",
	// so a failure says WHY the column should have been redacted rather than
	// only that it was not.
	resolved       string
	isBytea        bool
	nameSaysSecret bool
}

// secretColumnsFromCatalogue answers, for every column of every table in
// public, the two questions the redaction rule answers -- on the admin
// connection, so it is a statement about the database, not the pool under
// test.
//
// The recursive term is the whole point: a column's type may wrap the
// interesting one (an array wraps its element via typelem, a domain wraps
// its base via typbasetype, either can wrap the other), and asking whether
// pg_catalog.bytea appears anywhere in that chain is a different question
// from "what does data_type say" -- the one ColumnIsRedacted's type rule is
// meant to answer.
//
// typcategory = 'A' guards the array step because typelem is non-zero on
// several types that are not arrays at all (name, point, int2vector), and
// following it there would walk into nonsense.
func secretColumnsFromCatalogue(t *testing.T, db *postgres.DB) []catalogueColumn {
	t.Helper()
	rows, err := db.Pool().Query(context.Background(), `
		WITH RECURSIVE chain AS (
			SELECT c.relname AS table_name, a.attname AS column_name,
			       a.atttypid AS type_oid, 0 AS hops
			FROM pg_attribute a
			JOIN pg_class c ON c.oid = a.attrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public'
			  AND c.relkind IN ('r', 'p')
			  AND a.attnum > 0
			  AND NOT a.attisdropped
			UNION ALL
			SELECT ch.table_name, ch.column_name,
			       CASE WHEN t.typtype = 'd' THEN t.typbasetype ELSE t.typelem END,
			       ch.hops + 1
			FROM chain ch
			JOIN pg_type t ON t.oid = ch.type_oid
			WHERE ch.hops < 8
			  AND (t.typtype = 'd' OR (t.typcategory = 'A' AND t.typelem <> 0))
		)
		SELECT ch.table_name,
		       ch.column_name,
		       string_agg(format_type(ch.type_oid, NULL), ' -> ' ORDER BY ch.hops),
		       bool_or(t.typname = 'bytea' AND t.typnamespace = 'pg_catalog'::regnamespace),
		       lower(ch.column_name) LIKE '%\_hash'
		         OR lower(ch.column_name) LIKE '%\_secret'
		         OR lower(ch.column_name) LIKE '%password%'
		FROM chain ch
		JOIN pg_type t ON t.oid = ch.type_oid
		GROUP BY ch.table_name, ch.column_name
		ORDER BY ch.table_name, ch.column_name`)
	if err != nil {
		t.Fatalf("read the catalogue: %v", err)
	}
	defer rows.Close()

	var out []catalogueColumn
	for rows.Next() {
		var c catalogueColumn
		if err := rows.Scan(&c.table, &c.column, &c.resolved, &c.isBytea, &c.nameSaysSecret); err != nil {
			t.Fatalf("scan a catalogue column: %v", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the catalogue: %v", err)
	}
	// A sweep over nothing is not a sweep. The schema has thirty-odd tables;
	// a handful of rows here means the query stopped matching.
	if len(out) < 100 {
		t.Fatalf("the catalogue sweep found %d columns, which is far too few to be the whole schema", len(out))
	}
	return out
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

// assertEveryRowIsPagedOnce reads a table two rows at a time and fails
// unless each row shows up on exactly one page, identified by the named
// column. It returns the identifiers in page order, so a caller that cares
// about ordering (not just no repeats) can assert on it too.
func assertEveryRowIsPagedOnce(t *testing.T, repo *postgres.BrowseRepo, table, idColumn string, want int) []string {
	t.Helper()
	seen := map[string]bool{}
	var order []string
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
			order = append(order, id)
		}
	}
	if len(seen) != want {
		t.Fatalf("saw %d of %d %s rows across the pages", len(seen), want, table)
	}
	return order
}

// rewriteAccount updates one account in place, the way the product does
// while an operator is paging. What it writes doesn't matter -- that the
// row moves to the end of the heap does, since that is the order an
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

// displayType's label is the one user-visible behaviour no other test
// touches: neutering it to `return dataType` would leave every other test
// in this package green.
//
// Both branches come from the existing schema, with no extra seeding:
// information_schema reports a category, not a type name, for arrays and
// for citext, so without this an operator browsing users would read
// "USER-DEFINED" on the one screen whose whole job is showing the schema.
func TestColumnTypesAreShownAsTheSchemaSpellsThem(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	for _, want := range []struct{ table, column, dataType string }{
		{"users", "email", "citext"},              // information_schema: USER-DEFINED
		{"memberships", "capabilities", "text[]"}, // information_schema: ARRAY, udt _text
		{"users", "id", "uuid"},                   // already a type name, passed through
	} {
		page, err := f.repo.Rows(ctx, want.table, 1, 0)
		if err != nil {
			t.Fatalf("Rows(%s): %v", want.table, err)
		}
		got := ""
		for _, c := range page.Columns {
			if c.Name == want.column {
				got = c.DataType
			}
		}
		if got != want.dataType {
			t.Errorf("%s.%s DataType = %q, want %q", want.table, want.column, got, want.dataType)
		}
	}
}
