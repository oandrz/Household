package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
)

func TestOpenAndPing(t *testing.T) {
	url := testsupport.StartPostgres(t)

	db, err := postgres.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(db.Close)

	if err := db.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestMigrationsCreatedTheSchema(t *testing.T) {
	url := testsupport.StartPostgres(t)

	db, err := postgres.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(db.Close)

	var count int
	err = db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_name = 'households'`).Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("households table not found; migrations did not run")
	}
}

func TestOpenRejectsAnUnreachableDatabase(t *testing.T) {
	if _, err := postgres.Open(context.Background(), "postgres://nobody@127.0.0.1:1/none"); err == nil {
		t.Fatal("expected an error for an unreachable database")
	}
}

// A request deadline reaches a running query through its context; the
// HTTP layer answers 503 TIMEOUT on that basis, so pgx must say why.
func TestAQueryCancelledByItsContextReportsDeadlineExceeded(t *testing.T) {
	db, err := postgres.Open(context.Background(), testsupport.StartPostgres(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(db.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = db.Pool().Exec(ctx, "SELECT pg_sleep(2)")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want one matching context.DeadlineExceeded", err)
	}
}

func TestTheRequestPoolStopsAQueryAtItsStatementTimeout(t *testing.T) {
	db, err := postgres.Open(context.Background(), testsupport.StartPostgres(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(db.Close)

	start := time.Now()
	_, err = db.Pool().Exec(context.Background(), "SELECT pg_sleep(30)")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "57014" {
		t.Fatalf("err = %v, want SQLSTATE 57014 (query_canceled by statement_timeout)", err)
	}
	if took := time.Since(start); took > postgres.RequestStatementTimeout+3*time.Second {
		t.Fatalf("query ran %v, want it stopped near %v", took, postgres.RequestStatementTimeout)
	}
}

func TestEachPoolCarriesItsOwnStatementTimeout(t *testing.T) {
	url := testsupport.StartPostgres(t)
	for want, open := range map[string]func(context.Context, string) (*postgres.DB, error){
		"5s":   postgres.Open,
		"1min": postgres.OpenJobs,
	} {
		db, err := open(context.Background(), url)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		var got string
		if err := db.Pool().QueryRow(context.Background(), "SHOW statement_timeout").Scan(&got); err != nil {
			t.Fatalf("SHOW: %v", err)
		}
		db.Close()
		if got != want {
			t.Errorf("statement_timeout = %q, want %q", got, want)
		}
	}
}
