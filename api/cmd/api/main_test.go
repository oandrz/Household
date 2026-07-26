package main

import (
	"net"
	"strconv"
	"testing"
)

// TestRunReturnsErrorWhenListenFails proves that a listener bind failure
// (e.g. EADDRINUSE) propagates out of run() as a non-nil error, so main()
// reaches os.Exit(1) instead of exiting 0 having served nothing.
func TestRunReturnsErrorWhenListenFails(t *testing.T) {
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
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789abcdef")

	if err := run(); err == nil {
		t.Fatal("expected run() to return an error when the port is already bound")
	}
}
