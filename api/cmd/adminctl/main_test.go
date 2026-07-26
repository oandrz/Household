package main

import (
	"strings"
	"testing"
	"time"
)

func TestRequireLocalDatabase(t *testing.T) {
	cases := []struct {
		name        string
		databaseURL string
		wantErr     bool
	}{
		{"localhost", "postgres://hearth:hearth@localhost:5432/hearth?sslmode=disable", false},
		{"loopback IPv4", "postgres://hearth:hearth@127.0.0.1:5432/hearth?sslmode=disable", false},
		{"loopback IPv6", "postgres://hearth:hearth@[::1]:5432/hearth?sslmode=disable", false},
		{"the compose service name", "postgres://hearth:hearth@postgres:5432/hearth?sslmode=disable", false},
		{"an arbitrary remote host", "postgres://hearth:hearth@db.example.com:5432/hearth?sslmode=disable", true},
		{"a managed database host", "postgres://user:pass@my-prod-db.abcdef.us-east-1.rds.amazonaws.com:5432/hearth", true},
		{"unparsable URL", "://not-a-url", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := requireLocalDatabase(tc.databaseURL)
			if tc.wantErr && err == nil {
				t.Fatalf("requireLocalDatabase(%q) = nil, want an error", tc.databaseURL)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("requireLocalDatabase(%q) = %v, want nil", tc.databaseURL, err)
			}
		})
	}
}

// TestRunRefusesToSeedARemoteDatabaseBeforeConnecting proves both of seed's
// guards run before postgres.Open ever attempts to reach the database, not
// after: DATABASE_URL here points at a non-routable address, so if the
// guard ran after Open (or not at all), this would hang for Open's 5-second
// ping timeout, or longer, waiting on a connection that can never succeed.
// Returning well under that proves run refused before ever dialing out.
func TestRunRefusesToSeedARemoteDatabaseBeforeConnecting(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_URL", "postgres://hearth:hearth@10.255.255.1:5432/hearth?sslmode=disable")
	t.Setenv("SESSION_SECRET", "development-session-secret-not-for-production")
	t.Setenv("SMTP_ADDR", "localhost:1025")
	t.Setenv("SMTP_FROM", "Hearth <noreply@hearth.localhost>")
	t.Setenv("APP_BASE_URL", "http://localhost:5173")
	t.Setenv("ARGON2_TIME", "1")
	t.Setenv("ARGON2_MEMORY_KIB", "8192")
	t.Setenv("ARGON2_THREADS", "1")

	start := time.Now()
	err := run([]string{"seed"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected run to refuse, got nil error")
	}
	if !strings.Contains(err.Error(), "not a recognised local address") {
		t.Fatalf("err = %v, want a local-database refusal", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("run took %v to refuse; want it to refuse before ever attempting to connect "+
			"(postgres.Open's own ping timeout is 5s)", elapsed)
	}
}

// TestRunRefusesToSeedOutsideDevelopmentBeforeConnecting is the same proof
// for the environment guard: APP_ENV=production must refuse before Open is
// ever called, even though DATABASE_URL here is otherwise a recognised local
// host and would pass requireLocalDatabase on its own.
func TestRunRefusesToSeedOutsideDevelopmentBeforeConnecting(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://hearth:hearth@10.255.255.1:5432/hearth?sslmode=disable")
	t.Setenv("SESSION_SECRET", "development-session-secret-not-for-production")
	t.Setenv("SMTP_ADDR", "localhost:1025")
	t.Setenv("SMTP_FROM", "Hearth <noreply@hearth.localhost>")
	t.Setenv("APP_BASE_URL", "http://localhost:5173")
	t.Setenv("ARGON2_TIME", "1")
	t.Setenv("ARGON2_MEMORY_KIB", "8192")
	t.Setenv("ARGON2_THREADS", "1")

	start := time.Now()
	err := run([]string{"seed"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected run to refuse, got nil error")
	}
	if !strings.Contains(err.Error(), "refusing to seed outside development") {
		t.Fatalf("err = %v, want the environment refusal", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("run took %v to refuse; want it to refuse before ever attempting to connect", elapsed)
	}
}
