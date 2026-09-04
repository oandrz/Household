package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
)

// Someone will eventually paste the read-write URL into DATABASE_READONLY_URL
// to make a broken panel work, at 1 a.m., meaning to change it back. That is
// not a hypothetical failure mode; it is the ordinary one. This is the test
// that turns the spec's guard 1 from a claim in a document into a property of
// the running process.
func TestOpenReadOnlyRefusesAConnectionThatCanWrite(t *testing.T) {
	adminURL := testsupport.StartPostgres(t)

	db, err := postgres.OpenReadOnly(context.Background(), adminURL)
	if err == nil {
		db.Close()
		t.Fatal("OpenReadOnly accepted the read-write URL")
	}
	// The message is read by an operator at 3 a.m., so it must name the
	// variable rather than describe a privilege check.
	if !strings.Contains(err.Error(), "DATABASE_READONLY_URL") {
		t.Fatalf("error does not name the variable: %v", err)
	}
	// main.go decides whether to refuse the boot by matching this sentinel.
	// It survives out through Ping only because assertCannotWrite and
	// OpenReadOnly each wrap it with %w -- pgxpool v5.10.0 and puddle v2.2.2
	// pass an AfterConnect error straight through unwrapped at every hop
	// (the pool's Constructor, puddle's acquire, and Pool.Ping all just
	// `return err`), so nothing outside this file has to preserve the
	// chain. If this assertion fails, one of this file's two %w wraps broke,
	// and main.go would degrade instead of refusing -- exactly backwards.
	if !errors.Is(err, postgres.ErrReadOnlyMisconfigured) {
		t.Fatalf("error does not carry ErrReadOnlyMisconfigured: %v", err)
	}
}

func TestOpenReadOnlyAcceptsTheReadOnlyRole(t *testing.T) {
	adminURL := testsupport.StartPostgres(t)

	db, err := postgres.OpenReadOnly(context.Background(), testsupport.ReadOnlyURL(t, adminURL))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(db.Close)

	var one int
	if err := db.Pool().QueryRow(context.Background(), `SELECT 1`).Scan(&one); err != nil {
		t.Fatalf("select through the read-only pool: %v", err)
	}
}

// The pool carries the timeout itself as well as the role carrying it, so a
// database whose role predates this decision -- an existing box provisioned
// from an older PROVISION.md -- is still bounded.
func TestTheReadOnlyPoolBoundsItsOwnStatements(t *testing.T) {
	adminURL := testsupport.StartPostgres(t)

	db, err := postgres.OpenReadOnly(context.Background(), testsupport.ReadOnlyURL(t, adminURL))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(db.Close)

	var setting string
	if err := db.Pool().QueryRow(context.Background(), `SHOW statement_timeout`).Scan(&setting); err != nil {
		t.Fatalf("show: %v", err)
	}
	if setting == "0" {
		t.Fatal("statement_timeout is unset on the read-only pool")
	}
}

func TestOpenReadOnlyRefusesAnUnparseableURL(t *testing.T) {
	// "host=x port=notanumber", not something vaguely wrong-looking: pgx
	// accepts a bare string as a keyword/value DSN, so a value like "not a
	// dsn at all" might parse into keys rather than fail, and the test would
	// pass or fail depending on pgx's parser rather than on this code. A
	// non-numeric port is refused by ParseConfig for certain.
	_, err := postgres.OpenReadOnly(context.Background(), "host=x port=notanumber dbname=hearth")
	if err == nil {
		t.Fatal("OpenReadOnly accepted a URL that is not a DSN")
	}
	if !errors.Is(err, postgres.ErrReadOnlyMisconfigured) {
		t.Fatalf("error does not carry ErrReadOnlyMisconfigured: %v", err)
	}
}

// The other half of the same decision, and the one that keeps a restore from
// taking the whole product down: a database that is simply not there yet is
// NOT a misconfiguration, so main.go degrades instead of refusing the boot.
func TestOpenReadOnlyTreatsAnUnreachableDatabaseAsMerelyUnavailable(t *testing.T) {
	// A DSN that parses and cannot connect: nothing listens on this port.
	_, err := postgres.OpenReadOnly(context.Background(),
		"postgres://hearth_readonly:pw@127.0.0.1:1/hearth?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("OpenReadOnly succeeded against a port nothing listens on")
	}
	if errors.Is(err, postgres.ErrReadOnlyMisconfigured) {
		t.Fatalf("an unreachable database was reported as a misconfiguration: %v", err)
	}
}
