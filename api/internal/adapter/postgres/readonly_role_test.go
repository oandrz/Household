package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
)

// The role is the guard that actually holds: a mistake in the adapter's SQL
// still cannot write, because Postgres refuses on the other side of the wire.
// Everything else this feature does is defence in depth on top of this, so
// this is the test that must never be allowed to go green for the wrong
// reason -- see the spec's decision 1.
func TestReadOnlyRoleCanReadAndCannotWrite(t *testing.T) {
	adminURL := testsupport.StartPostgres(t)
	ctx := context.Background()

	db, err := postgres.Open(ctx, testsupport.ReadOnlyURL(t, adminURL))
	if err != nil {
		t.Fatalf("Open as hearth_readonly: %v", err)
	}
	t.Cleanup(db.Close)

	t.Run("reads", func(t *testing.T) {
		var count int
		if err := db.Pool().QueryRow(ctx,
			`SELECT count(*) FROM information_schema.tables WHERE table_name = 'households'`,
		).Scan(&count); err != nil {
			t.Fatalf("select: %v", err)
		}
		if count != 1 {
			t.Fatalf("households not visible to hearth_readonly; the GRANT did not run")
		}
	})

	t.Run("cannot insert", func(t *testing.T) {
		_, err := db.Pool().Exec(ctx,
			`INSERT INTO households (id, name) VALUES (gen_random_uuid(), 'nope')`)
		if err == nil {
			t.Fatal("INSERT succeeded as hearth_readonly")
		}
		// Assert on WHY it failed. A NOT NULL violation on some column this
		// INSERT forgot would also be a non-nil error, and this subtest would
		// then pass whether or not the role can write -- which would also
		// make Step 6's mutation check meaningless.
		message := err.Error()
		if !strings.Contains(message, "permission denied") &&
			!strings.Contains(message, "read-only transaction") {
			t.Fatalf("INSERT failed for the wrong reason: %v", err)
		}
	})

	t.Run("cannot create a table", func(t *testing.T) {
		if _, err := db.Pool().Exec(ctx, `CREATE TABLE nope (id int)`); err == nil {
			t.Fatal("CREATE TABLE succeeded as hearth_readonly")
		}
	})

	// The role's own session default, independent of the GRANTs above. A
	// GRANT that was accidentally widened would still be caught here.
	t.Run("its session is read-only by default", func(t *testing.T) {
		var setting string
		if err := db.Pool().QueryRow(ctx, `SHOW default_transaction_read_only`).Scan(&setting); err != nil {
			t.Fatalf("show: %v", err)
		}
		if setting != "on" {
			t.Fatalf("default_transaction_read_only = %q, want \"on\"", setting)
		}
	})

	// Named so a failure says which of the two mechanisms is missing rather
	// than only that a write went through.
	t.Run("its statement timeout is set", func(t *testing.T) {
		var setting string
		if err := db.Pool().QueryRow(ctx, `SHOW statement_timeout`).Scan(&setting); err != nil {
			t.Fatalf("show: %v", err)
		}
		if !strings.HasPrefix(setting, "3") {
			t.Fatalf("statement_timeout = %q, want 3 seconds", setting)
		}
	})
}
