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

	"github.com/andreasoentoro/hearth/api/internal/adapter/clock"
	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/adapter/mail"
	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/config"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
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

	// Repositories. Each is constructed once and shared by every service (and,
	// for Users/Memberships/Sessions, by the HTTP layer directly too) that
	// needs it -- there is exactly one implementation of each port in
	// production, backed by this one connection pool.
	users := postgres.NewUserRepo(db)
	households := postgres.NewHouseholdRepo(db)
	memberships := postgres.NewMembershipRepo(db)
	sessions := postgres.NewSessionRepo(db)
	magicLinks := postgres.NewMagicLinkRepo(db)
	loginAttempts := postgres.NewLoginAttemptRepo(db)
	invites := postgres.NewInviteRepo(db)
	spaces := postgres.NewSpaceRepo(db)
	notifications := postgres.NewNotificationRepo(db)

	hasher := crypto.NewArgon2Hasher(cfg.Argon2Time, cfg.Argon2MemoryKiB, cfg.Argon2Threads)
	tokens := crypto.NewTokenGenerator()
	sysClock := clock.System{}
	mailer := mail.NewSMTPMailer(cfg.SMTPAddr, cfg.SMTPFrom, cfg.AppBaseURL)

	authSvc := usecase.NewAuthService(usecase.AuthDeps{
		Users:      users,
		Members:    memberships,
		Sessions:   sessions,
		Attempts:   loginAttempts,
		MagicLinks: magicLinks,
		Mailer:     mailer,
		Hasher:     hasher,
		Tokens:     tokens,
		Clock:      sysClock,
		SessionTTL: httpadapter.SessionTTL,
		BaseURL:    cfg.AppBaseURL,
	})
	inviteSvc := usecase.NewInviteService(usecase.InviteDeps{
		Invites:    invites,
		Users:      users,
		Sessions:   sessions,
		Mailer:     mailer,
		Hasher:     hasher,
		Tokens:     tokens,
		Clock:      sysClock,
		SessionTTL: httpadapter.SessionTTL,
		BaseURL:    cfg.AppBaseURL,
	})
	memberSvc := usecase.NewMemberService(usecase.MemberDeps{
		Members:  memberships,
		Sessions: sessions,
	})
	householdSvc := usecase.NewHouseholdService(usecase.HouseholdDeps{
		Households:    households,
		Spaces:        spaces,
		Notifications: notifications,
	})

	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.Port),
		Handler: httpadapter.NewRouter(httpadapter.Deps{
			Pinger:      db,
			Auth:        authSvc,
			Invites:     inviteSvc,
			Members:     memberSvc,
			Households:  householdSvc,
			Users:       users,
			Memberships: memberships,
			Sessions:    sessions,
			Tokens:      tokens,
			Clock:       sysClock,
			Secure:      !cfg.IsDevelopment(),
		}),
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
