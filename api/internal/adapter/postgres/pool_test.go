package postgres_test

import (
	"context"
	"testing"

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
