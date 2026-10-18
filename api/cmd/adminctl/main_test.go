package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/clock"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
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

// TestRunRefusesToSeedARemoteDatabaseBeforeConnecting: DATABASE_URL points at
// a non-routable address, so if a guard ran after postgres.Open (or not at
// all), this would hang for Open's ~5s ping timeout instead of returning
// immediately -- proving both guards run before Open ever dials out.
func TestRunRefusesToSeedARemoteDatabaseBeforeConnecting(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_URL", "postgres://hearth:hearth@10.255.255.1:5432/hearth?sslmode=disable")
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
// for the environment guard: DATABASE_URL is the same non-routable address,
// so a guard running after postgres.Open would hang. The message check pins
// that APP_ENV refused, not requireLocalDatabase.
func TestRunRefusesToSeedOutsideDevelopmentBeforeConnecting(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://hearth:hearth@10.255.255.1:5432/hearth?sslmode=disable")
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

// TestRunPruneRefusesAWindowUnderTheFloor: repositories are all nil here, so
// if the floor check did not run first, a Prune call reaching any of them
// would panic rather than merely delete the wrong rows -- proving the floor
// is enforced before any repository is touched. See usecase.RetentionFloor
// for why the floor is seven days.
func TestRunPruneRefusesAWindowUnderTheFloor(t *testing.T) {
	retention := usecase.NewRetentionService(usecase.RetentionDeps{Clock: clock.System{}})
	err := runPrune(context.Background(), retention, 3*24*time.Hour)
	if err == nil {
		t.Fatal("runPrune(3 days) = nil, want a refusal")
	}
	if !strings.Contains(err.Error(), "must be at least 7 days") {
		t.Fatalf("err = %v, want it to name the 7-day floor", err)
	}
}

// TestGrantPlatformAdminNeedsAnEmail: every one of these commands resolves a
// person by address, and a missing flag must say so rather than acting on
// whoever happens to be first in the table.
func TestGrantPlatformAdminNeedsAnEmail(t *testing.T) {
	// nil repositories are safe here precisely because the guard returns
	// before touching either one -- which is the behaviour under test.
	err := runGrantPlatformAdmin(context.Background(), nil, nil, "", "")
	if err == nil || !strings.Contains(err.Error(), "--email") {
		t.Fatalf("runGrantPlatformAdmin with no --email = %v, want an error naming --email", err)
	}
}

// TestRevokePlatformAdminNeedsAnEmail is grant's guard again: revoking with
// no address would revoke nothing identifiable, so it must refuse before
// ever calling ByEmail or Revoke -- both nil here for the same reason.
func TestRevokePlatformAdminNeedsAnEmail(t *testing.T) {
	err := runRevokePlatformAdmin(context.Background(), nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), "--email") {
		t.Fatalf("runRevokePlatformAdmin with no --email = %v, want an error naming --email", err)
	}
}

// TestUsageListsEveryAdminCommand keeps the help text honest: a command
// nobody can discover is a command nobody uses.
func TestUsageListsEveryAdminCommand(t *testing.T) {
	for _, want := range []string{
		"grant-platform-admin", "revoke-platform-admin",
		"list-platform-admins", "unlock-admin",
	} {
		if !strings.Contains(usage, want) {
			t.Fatalf("usage does not mention %q", want)
		}
	}
}

type resetUsers struct {
	usecase.UserRepository
	hash string
}

func (u *resetUsers) ByEmail(_ context.Context, email string) (usecase.StoredUser, error) {
	return usecase.StoredUser{User: domain.User{ID: "user-1", Email: email}}, nil
}
func (u *resetUsers) SetPasswordHash(_ context.Context, _ string, hash string) error {
	u.hash = hash
	return nil
}

type plainHasher struct{ usecase.PasswordHasher }

func (plainHasher) Hash(_ context.Context, p string) (string, error) { return "hashed:" + p, nil }

type recordingRevoker struct {
	calls []usecase.ChatRule
	err   error
}

func (r *recordingRevoker) RevokeAccess(_ context.Context, _ string, chat usecase.ChatRule) error {
	r.calls = append(r.calls, chat)
	return r.err
}

func TestResetPasswordCutsOffEverythingAndSaysWhat(t *testing.T) {
	users, access := &resetUsers{}, &recordingRevoker{}
	msg, err := resetPassword(context.Background(), users, plainHasher{}, access, "andreas@hearth.family", "a-new-password-1")
	if err != nil {
		t.Fatalf("resetPassword: %v", err)
	}
	if users.hash != "hashed:a-new-password-1" {
		t.Fatalf("hash = %q", users.hash)
	}
	if len(access.calls) != 1 || access.calls[0] != usecase.RevokeChat {
		t.Fatalf("revoke calls = %v, want one RevokeChat", access.calls)
	}
	want := "Password reset for andreas@hearth.family. Every session, API token and unused sign-in link has been revoked and their Telegram chat disconnected."
	if msg != want {
		t.Fatalf("message = %q, want %q", msg, want)
	}
}

func TestResetPasswordWhoseRevokeFailsSaysNothingElseWasRevoked(t *testing.T) {
	access := &recordingRevoker{err: errors.New("simulated")}
	_, err := resetPassword(context.Background(), &resetUsers{}, plainHasher{}, access, "andreas@hearth.family", "a-new-password-1")
	if err == nil || !strings.Contains(err.Error(), "password was reset, but nothing else was revoked; run reset-password again") {
		t.Fatalf("err = %v", err)
	}
}
