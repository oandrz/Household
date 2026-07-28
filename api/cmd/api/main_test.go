package main

import (
	"bytes"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/config"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
)

// TestRunReturnsErrorWhenListenFails proves that a listener bind failure
// (e.g. EADDRINUSE) propagates out of run() as a non-nil error, so main()
// reaches os.Exit(1) instead of exiting 0 having served nothing.
//
// DATABASE_URL must point at a real, reachable Postgres: run() now calls
// postgres.Open before it ever constructs the listener, so a fake
// DATABASE_URL would make run() fail there instead, and this test would
// pass for the wrong reason -- a database error, not the listener-bind
// failure it exists to catch. testsupport.StartPostgres boots a disposable
// container so run() gets past postgres.Open and actually reaches
// ListenAndServe.
func TestRunReturnsErrorWhenListenFails(t *testing.T) {
	databaseURL := testsupport.StartPostgres(t)

	// Reserve a port by holding a listener open on it for the duration of
	// the test, so the server started inside run() collides with it. run()
	// binds to the wildcard address (":<port>"), so the reservation must
	// use the wildcard address too -- a loopback-only reservation does not
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
	// run() only wraps an error with this prefix in the listener-failure
	// branch (see main.go: fmt.Errorf("listen and serve: %w", err)).
	// postgres.Open's own error paths are worded "parse database url:",
	// "create pool:", and "ping database:", so this assertion fails loudly
	// if a future change makes run() error out earlier than the bind.
	if !strings.Contains(runErr.Error(), "listen and serve") {
		t.Fatalf("expected a listen-and-serve error, got: %v", runErr)
	}
}

// TestLogStartupAddressesReportsBothAddresses pins what startup prints: the
// listener's own address and the SMTP server mail goes through. The SMTP line
// is the reason this test exists -- it was previously absent, so a service
// pointed at the wrong mail host looked identical at startup to one pointed at
// the right one.
//
// It calls logStartupAddresses directly rather than run(), because run() needs
// a real Postgres and a whole Postgres container is not a reasonable price for
// checking two log lines.
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
