// Command api is the Hearth HTTP service. It does wiring and nothing else:
// every decision it makes is which implementation to construct.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/config"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           httpadapter.NewRouter(httpadapter.Deps{Pinger: db}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// serveErr carries the outcome of ListenAndServe back to the main path.
	// It is buffered so the goroutine never blocks sending to it, and it is
	// only ever read after <-ctx.Done() returns, so there is no data race:
	// a listen failure sends before calling stop() (which is what unblocks
	// ctx.Done() below), so the send always happens-before the read.
	serveErr := make(chan error, 1)

	go func() {
		slog.Info("listening", "addr", srv.Addr, "env", cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped", "error", err)
			serveErr <- err
			stop()
			return
		}
		serveErr <- nil
	}()

	<-ctx.Done()

	// If the listener itself failed to start, there is nothing to shut down
	// and the failure must propagate so the process exits non-zero. A
	// signal-driven shutdown leaves serveErr empty at this point, since
	// ListenAndServe only returns (with ErrServerClosed) once Shutdown is
	// called below.
	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("listen and serve: %w", err)
		}
	default:
	}

	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
