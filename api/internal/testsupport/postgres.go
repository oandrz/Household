// Package testsupport provides fixtures shared by tests. It is only ever
// imported from _test.go files.
package testsupport

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// StartPostgres boots a disposable Postgres, applies every migration, and
// returns its connection URL. The container is removed when the test ends.
func StartPostgres(t *testing.T) string {
	t.Helper()

	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("hearth"),
		tcpostgres.WithUsername("hearth"),
		tcpostgres.WithPassword("hearth"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate container: %v", err)
		}
	})

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	applyMigrations(t, url)
	createReadOnlyRole(t, url)
	return url
}

func applyMigrations(t *testing.T, url string) {
	t.Helper()

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open for migrations: %v", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.Up(db, migrationsDir(t)); err != nil {
		t.Fatalf("goose up: %v", err)
	}
}

// migrationsDir resolves api/migrations from this file's own location, so tests
// pass regardless of the working directory they run from.
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine the caller path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
}

// readOnlyPassword is the role's password in tests only. It is set here
// rather than in deploy/readonly-role.sql because psql's :'var' syntax is
// client-side and unusable from pgx -- the spec's decision 5.
const readOnlyPassword = "hearth-readonly"

// createReadOnlyRole runs deploy/readonly-role.sql against the freshly
// migrated database, so every test in this repository sees the same role
// production will have. A test that opened the browse against DATABASE_URL
// would prove nothing about the guard that matters.
func createReadOnlyRole(t *testing.T, url string) {
	t.Helper()

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect to create the read-only role: %v", err)
	}
	defer conn.Close(ctx)

	script, err := os.ReadFile(readOnlyRoleScript(t))
	if err != nil {
		t.Fatalf("read deploy/readonly-role.sql: %v", err)
	}
	// No arguments, so pgx uses the simple protocol and the whole file --
	// DO block included -- goes in one round trip.
	if _, err := conn.Exec(ctx, string(script)); err != nil {
		t.Fatalf("run deploy/readonly-role.sql: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`ALTER ROLE hearth_readonly PASSWORD '`+readOnlyPassword+`'`); err != nil {
		t.Fatalf("set the read-only role's password: %v", err)
	}
}

// readOnlyRoleScript resolves deploy/readonly-role.sql from this file's own
// location, the same way migrationsDir does, so tests pass regardless of the
// working directory they run from.
func readOnlyRoleScript(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine the caller path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "deploy", "readonly-role.sql")
}

// ReadOnlyURL is the same database as adminURL, reached as hearth_readonly.
// Use it for anything that exercises the database browse: opening the browse
// against the read-write URL would test the SQL and none of the guard.
func ReadOnlyURL(t *testing.T, adminURL string) string {
	t.Helper()
	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse the container url: %v", err)
	}
	parsed.User = url.UserPassword("hearth_readonly", readOnlyPassword)
	return parsed.String()
}
