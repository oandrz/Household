package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/config"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// TestRunReturnsErrorWhenListenFails proves a listener bind failure (e.g.
// EADDRINUSE) propagates out of run() as a non-nil error, so main() reaches
// os.Exit(1) rather than exiting 0 having served nothing.
//
// DATABASE_URL must be a real, reachable Postgres: run() calls postgres.Open
// before constructing the listener, so a fake URL would fail there instead
// and this test would pass for the wrong reason. testsupport.StartPostgres
// gets run() past that and into ListenAndServe.
func TestRunReturnsErrorWhenListenFails(t *testing.T) {
	databaseURL := testsupport.StartPostgres(t)

	// Reserve a port by holding a listener open on it, so run()'s server
	// collides with it. run() binds the wildcard address (":<port>"), so
	// the reservation must too -- a loopback-only reservation does not
	// reliably collide with a wildcard bind on every platform.
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to reserve a port: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port

	t.Setenv("APP_ENV", "test")
	t.Setenv("PORT", strconv.Itoa(port))
	t.Setenv("DATABASE_URL", databaseURL)
	t.Setenv("SMTP_ADDR", "localhost:1025")
	t.Setenv("SMTP_FROM", "Hearth <noreply@hearth.localhost>")
	t.Setenv("APP_BASE_URL", "http://localhost:5173")

	runErr := run()
	if runErr == nil {
		t.Fatal("expected run() to return an error when the port is already bound")
	}
	// run() only wraps an error with "listen and serve" in the listener
	// branch; postgres.Open's own errors are worded "parse database url:",
	// "create pool:", "ping database:" -- so this assertion fails loudly if
	// a future change makes run() error out earlier than the bind.
	if !strings.Contains(runErr.Error(), "listen and serve") {
		t.Fatalf("expected a listen-and-serve error, got: %v", runErr)
	}
}

// TestLogStartupAddressesReportsBothAddresses pins what startup prints: the
// listener's own address and the SMTP server mail goes through. Don't drop
// the SMTP line: without it, a service pointed at the wrong mail host looks
// identical at startup to one pointed at the right one.
//
// It calls logStartupAddresses directly, not run(), because run() needs a
// real Postgres and a whole container is too much for checking two log lines.
func TestLogStartupAddressesReportsBothAddresses(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	logStartupAddresses(config.Config{
		AppEnv:      "development",
		SMTPAddr:    "mailpit:1025",
		SMTPTLSMode: "none",
	}, ":8080")

	logged := buf.String()
	for _, want := range []string{`addr=:8080`, `env=development`, `smtp_addr=mailpit:1025`, `tls_mode=none`} {
		if !strings.Contains(logged, want) {
			t.Errorf("startup log is missing %s\ngot: %s", want, logged)
		}
	}
}

// TestLogStartupAddressesNeverLogsSMTPCredentials guards the obvious mistake in
// the change above: printing the mail configuration is useful, printing the
// relay's password is a credential leak into every log sink downstream.
func TestLogStartupAddressesNeverLogsSMTPCredentials(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	logStartupAddresses(config.Config{
		AppEnv:       "production",
		SMTPAddr:     "smtp.example.com:587",
		SMTPUsername: "hearth-relay-user",
		SMTPPassword: "correct-horse-battery-staple",
		SMTPTLSMode:  "mandatory",
	}, ":8080")

	logged := buf.String()
	for _, secret := range []string{"hearth-relay-user", "correct-horse-battery-staple"} {
		if strings.Contains(logged, secret) {
			t.Errorf("startup log leaked an SMTP credential (%q)\ngot: %s", secret, logged)
		}
	}
}

// --- the database browse's four boot outcomes -----------------------------

// TestOpenBrowseAnswersEachKindOfDatabaseReadonlyURL walks every arm of
// openBrowse against a real Postgres: whether a DSN parses is pgxpool's
// judgement, and whether the role may write is a live privilege lookup.
//
// One container for the whole test, subtests inside -- StartPostgres has no
// reuse across calls. It also runs deploy/readonly-role.sql, so
// hearth_readonly here matches the role production actually has.
//
// The security-relevant assertion is "a writable DSN refuses the boot":
// serving a read-only browse through a connection that may INSERT is worse
// than serving nothing.
func TestOpenBrowseAnswersEachKindOfDatabaseReadonlyURL(t *testing.T) {
	adminURL := testsupport.StartPostgres(t)
	readonlyURL := testsupport.ReadOnlyURL(t, adminURL)
	ctx := context.Background()

	// Refusing the boot is a returned error, and NOT returning a service:
	// a caller that ignored the error must not find a usable browse waiting.
	t.Run("a writable DSN refuses the boot", func(t *testing.T) {
		svc, db, err := openBrowse(ctx, config.Config{DatabaseReadonlyURL: adminURL})
		if !errors.Is(err, postgres.ErrReadOnlyMisconfigured) {
			t.Fatalf("error = %v, want it to carry postgres.ErrReadOnlyMisconfigured", err)
		}
		if !strings.Contains(err.Error(), "not a read-only role") {
			t.Fatalf("error = %v, want it to say the role is not read-only", err)
		}
		if svc != nil || db != nil {
			t.Fatalf("svc = %v, db = %v; a refused boot must hand back neither", svc, db)
		}
	})

	t.Run("an unparseable DSN refuses the boot", func(t *testing.T) {
		svc, db, err := openBrowse(ctx, config.Config{DatabaseReadonlyURL: "::: not a dsn :::"})
		if !errors.Is(err, postgres.ErrReadOnlyMisconfigured) {
			t.Fatalf("error = %v, want it to carry postgres.ErrReadOnlyMisconfigured", err)
		}
		if svc != nil || db != nil {
			t.Fatalf("svc = %v, db = %v; a refused boot must hand back neither", svc, db)
		}
	})

	// The restore-day case: the variable is set but the database cannot be
	// reached yet. Port 1 is closed, so the connection is refused immediately
	// rather than waiting out OpenReadOnly's five-second ping timeout.
	t.Run("an unreachable DSN does not refuse the boot", func(t *testing.T) {
		var buf bytes.Buffer
		previous := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
		t.Cleanup(func() { slog.SetDefault(previous) })

		svc, db, err := openBrowse(ctx, config.Config{
			DatabaseReadonlyURL: "postgres://hearth_readonly:pw@127.0.0.1:1/hearth?sslmode=disable",
		})
		if err != nil {
			t.Fatalf("error = %v; an unreachable database must not take the whole product down", err)
		}
		if db != nil {
			t.Fatal("a ReadOnlyDB came back for a database that was never reached; " +
				"the startup log reads this as \"database browse enabled\"")
		}
		if svc == nil {
			t.Fatal("svc is nil, so the browse would answer DB_BROWSE_NOT_CONFIGURED and tell the " +
				"operator to set a variable that is already set")
		}

		// The service must answer "the store is there, I could not reach it"
		// on BOTH methods -- that is what makes the route's 503 say
		// DB_BROWSE_UNAVAILABLE.
		if _, err := svc.Tables(ctx); !errors.Is(err, usecase.ErrBrowseUnavailable) {
			t.Fatalf("Tables error = %v, want usecase.ErrBrowseUnavailable", err)
		}
		_, rowsErr := svc.Rows(ctx, "households", 10, 0)
		if !errors.Is(rowsErr, usecase.ErrBrowseUnavailable) {
			t.Fatalf("Rows error = %v, want usecase.ErrBrowseUnavailable", rowsErr)
		}
		// The boot failure travels with the stand-in, so the 503's log line
		// says why the pool was never opened, not just that it wasn't --
		// useful once log rotation has taken the startup line away.
		if !strings.Contains(rowsErr.Error(), "connection refused") {
			t.Fatalf("the stand-in dropped the boot failure: %v", rowsErr)
		}

		// The log line is the only thing that says which .env line caused
		// this: pgx builds its connect errors from `user=` and `database=`
		// alone and never mentions the variable.
		//
		// This covers openBrowse's OWN log line only. "database browse
		// enabled" lives in run(), not here, so asserting it is absent from
		// this buffer would be a check that can never fail --
		// TestRunLogsTheBrowseAsEnabledOnlyWhenItActuallyOpened checks that
		// predicate against run() itself.
		logged := buf.String()
		if !strings.Contains(logged, "DATABASE_READONLY_URL") {
			t.Fatalf("the failure log never names the variable to fix\ngot: %s", logged)
		}
	})

	t.Run("no DATABASE_READONLY_URL is not a failure", func(t *testing.T) {
		svc, db, err := openBrowse(ctx, config.Config{DatabaseReadonlyURL: ""})
		if err != nil {
			t.Fatalf("error = %v; an install that never asked for the browse must boot", err)
		}
		if svc != nil || db != nil {
			t.Fatalf("svc = %v, db = %v; both must be nil so the routes answer "+
				"DB_BROWSE_NOT_CONFIGURED and name the variable", svc, db)
		}
	})

	// The live case last, so the three failures above cannot pass by the
	// pool happening to be open from an earlier subtest.
	t.Run("the real read-only role opens a live browse", func(t *testing.T) {
		svc, db, err := openBrowse(ctx, config.Config{DatabaseReadonlyURL: readonlyURL})
		if err != nil {
			t.Fatalf("openBrowse: %v", err)
		}
		if svc == nil || db == nil {
			t.Fatalf("svc = %v, db = %v; a live browse must hand back both", svc, db)
		}
		t.Cleanup(db.Close)

		tables, err := svc.Tables(ctx)
		if err != nil {
			t.Fatalf("Tables: %v", err)
		}
		if len(tables) == 0 {
			t.Fatal("the live browse listed no tables at all")
		}
	})
}

// TestRunRefusesTheBootOnAMisconfiguredReadonlyURL is the half of the above
// that openBrowse cannot prove on its own: that run() propagates the refusal
// instead of logging it and carrying on.
//
// The port is held open for the whole test, the same trick
// TestRunReturnsErrorWhenListenFails uses -- here as the control, not the
// subject: if run() ever got past the browse it would fail at the bind with
// "listen and serve", so a misconfiguration that stopped refusing would
// produce a *different* error, not a silent pass.
func TestRunRefusesTheBootOnAMisconfiguredReadonlyURL(t *testing.T) {
	databaseURL := testsupport.StartPostgres(t)

	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to reserve a port: %v", err)
	}
	defer ln.Close()

	t.Setenv("APP_ENV", "test")
	t.Setenv("PORT", strconv.Itoa(ln.Addr().(*net.TCPAddr).Port))
	t.Setenv("DATABASE_URL", databaseURL)
	t.Setenv("SMTP_ADDR", "localhost:1025")
	t.Setenv("SMTP_FROM", "Hearth <noreply@hearth.localhost>")
	t.Setenv("APP_BASE_URL", "http://localhost:5173")
	// The read-write URL: a role that may INSERT into users.
	t.Setenv("DATABASE_READONLY_URL", databaseURL)

	runErr := run()
	if runErr == nil {
		t.Fatal("run() returned nil for a DATABASE_READONLY_URL that may write")
	}
	if !errors.Is(runErr, postgres.ErrReadOnlyMisconfigured) {
		t.Fatalf("run() error = %v, want it to carry postgres.ErrReadOnlyMisconfigured", runErr)
	}
	if strings.Contains(runErr.Error(), "listen and serve") {
		t.Fatalf("run() reached the listener before refusing: %v", runErr)
	}
}

// syncBuffer is a bytes.Buffer safe to write from more than one goroutine.
// run() logs from the listener goroutine (logStartupAddresses, "server
// stopped") as well as the main path, so a bare bytes.Buffer here would race
// -- invisible today only because the suite does not run under -race.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runWithReadonlyURL boots the whole service once against a port that is
// already taken, so ListenAndServe fails and run() returns instead of blocking
// until a signal. It returns everything run() logged along the way.
//
// The held port is the same control TestRunReturnsErrorWhenListenFails uses:
// reaching it means run() got past the browse wiring, so "listen and serve"
// in the error is proof the boot was not refused earlier for another reason.
func runWithReadonlyURL(t *testing.T, databaseURL, readonlyURL string) (string, error) {
	t.Helper()

	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to reserve a port: %v", err)
	}
	defer ln.Close()

	var buf syncBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	t.Setenv("APP_ENV", "test")
	t.Setenv("PORT", strconv.Itoa(ln.Addr().(*net.TCPAddr).Port))
	t.Setenv("DATABASE_URL", databaseURL)
	t.Setenv("SMTP_ADDR", "localhost:1025")
	t.Setenv("SMTP_FROM", "Hearth <noreply@hearth.localhost>")
	t.Setenv("APP_BASE_URL", "http://localhost:5173")
	t.Setenv("DATABASE_READONLY_URL", readonlyURL)

	// Two statements, not `return buf.String(), run()`: Go evaluates a return
	// list left to right, so that one-liner reads the buffer BEFORE run() has
	// written anything to it and every log assertion sees an empty string.
	runErr := run()
	return buf.String(), runErr
}

// TestRunLogsTheBrowseAsEnabledOnlyWhenItActuallyOpened pins the predicate on
// run()'s startup line, which openBrowse's own tests cannot reach: the line is
// in run(), and it reads readonlyDB != nil rather than adminBrowseSvc != nil.
//
// Those two disagree in exactly one case: an unreachable database still
// produces a service (so the routes answer DB_BROWSE_UNAVAILABLE rather than
// telling the operator to set a variable that is already set), so the
// obvious predicate, adminBrowseSvc != nil, would announce "database browse
// enabled" right under the error saying it could not be opened.
//
// Both directions are asserted, and the positive one is what stops the
// negative from being vacuous: "the log does not say enabled" would also pass
// if the line were deleted outright.
func TestRunLogsTheBrowseAsEnabledOnlyWhenItActuallyOpened(t *testing.T) {
	adminURL := testsupport.StartPostgres(t)
	readonlyURL := testsupport.ReadOnlyURL(t, adminURL)

	const enabled = "database browse enabled"

	t.Run("a live read-only pool is announced", func(t *testing.T) {
		logged, runErr := runWithReadonlyURL(t, adminURL, readonlyURL)
		if runErr == nil || !strings.Contains(runErr.Error(), "listen and serve") {
			t.Fatalf("run() error = %v, want the held-port failure", runErr)
		}
		if !strings.Contains(logged, enabled) {
			t.Fatalf("a browse that really opened was never announced as %q\ngot: %s", enabled, logged)
		}
	})

	t.Run("an unreachable pool is not announced", func(t *testing.T) {
		logged, runErr := runWithReadonlyURL(t, adminURL,
			"postgres://hearth_readonly:pw@127.0.0.1:1/hearth?sslmode=disable")
		if runErr == nil || !strings.Contains(runErr.Error(), "listen and serve") {
			t.Fatalf("run() error = %v; an unreachable browse must not refuse the boot", runErr)
		}
		if !strings.Contains(logged, "DATABASE_READONLY_URL") {
			t.Fatalf("the failure log never names the variable to fix\ngot: %s", logged)
		}
		if strings.Contains(logged, enabled) {
			t.Fatalf("run() announced %q for a browse it could not open\ngot: %s", enabled, logged)
		}
	})
}
