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

	// Deps.Secure below (and the session/CSRF cookies it governs) is
	// !cfg.IsDevelopment(): Secure outside development, which means a
	// browser will only ever return either cookie over HTTPS. This process
	// itself never terminates TLS -- it just listens on cfg.Port -- so
	// outside development that guarantee depends entirely on a reverse
	// proxy or load balancer terminating TLS in front of it (see
	// web/nginx.conf and .env.example). Deployed with nothing doing that,
	// every cookie this service sets is silently dropped by the browser and
	// every authenticated request 401s with no indication why. This can't be
	// fixed from here -- it's a deployment-topology requirement, not a code
	// path -- so the best this can do is make it loud at the one moment an
	// operator is watching: startup.
	if !cfg.IsDevelopment() {
		slog.Warn("APP_ENV is not development: session and CSRF cookies are Secure and will only be " +
			"returned by a browser over HTTPS. TLS termination in front of this service (a reverse proxy " +
			"or load balancer) is mandatory -- see .env.example and web/nginx.conf. Without it, every " +
			"cookie this service sets is silently dropped and every authenticated request will 401.")
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
	signups := postgres.NewSignupRepo(db)

	hasher := crypto.NewArgon2Hasher(cfg.Argon2Time, cfg.Argon2MemoryKiB, cfg.Argon2Threads)
	tokens := crypto.NewTokenGenerator()
	sysClock := clock.System{}
	mailer := mail.NewSMTPMailer(cfg.SMTPAddr, cfg.SMTPFrom, cfg.AppBaseURL,
		cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPTLSMode)

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
	signupSvc := usecase.NewSignupService(usecase.SignupDeps{
		Signups:    signups,
		Users:      users,
		Sessions:   sessions,
		Mailer:     mailer,
		Hasher:     hasher,
		Tokens:     tokens,
		Clock:      sysClock,
		SessionTTL: httpadapter.SessionTTL,
		BaseURL:    cfg.AppBaseURL,
	})

	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.Port),
		Handler: httpadapter.NewRouter(httpadapter.Deps{
			Pinger:      db,
			Auth:        authSvc,
			Invites:     inviteSvc,
			Members:     memberSvc,
			Households:  householdSvc,
			Signups:     signupSvc,
			Users:       users,
			Memberships: memberships,
			Sessions:    sessions,
			Tokens:      tokens,
			Clock:       sysClock,
			Secure:      !cfg.IsDevelopment(),
		}),
		ReadHeaderTimeout: 10 * time.Second,
		// ReadTimeout bounds the whole request, not just its headers:
		// ReadHeaderTimeout alone leaves a slow-body attack (or a request
		// that simply never finishes sending) free to hold a connection open
		// indefinitely once the headers have arrived.
		ReadTimeout: 15 * time.Second,
		// MaxHeaderBytes is set explicitly rather than left to net/http's
		// unstated default -- this is the request-size bound that lives on
		// the *server*, alongside the JSON-body bound
		// (httpadapter's unexported maxRequestBodyBytes, in errors.go) that
		// lives in the handler layer.
		MaxHeaderBytes: 1 << 20,
	}

	// serveErr carries the outcome of ListenAndServe back to the main path.
	// It is buffered so the goroutine never blocks sending to it, and it is
	// only ever read after <-ctx.Done() returns, so there is no data race:
	// a listen failure sends before calling stop() (which is what unblocks
	// ctx.Done() below), so the send always happens-before the read.
	serveErr := make(chan error, 1)

	go func() {
		logStartupAddresses(cfg, srv.Addr)
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

// logStartupAddresses reports the two addresses an operator needs at the one
// moment they are watching: the port this process listens on, and the SMTP
// server it sends through. Mail is on the recovery path -- magic link is the
// only way back into a locked household -- and the send is fire-and-forget
// (see usecase/auth.go's sendMagicLinkAsync), so nothing downstream ever
// surfaces a wrong SMTP target. Printing it here is what lets someone tell
// "pointed at the wrong host" from "the relay refused it", and in development
// it is the reminder that mail lands in Mailpit rather than a real inbox.
//
// SMTPUsername and SMTPPassword are deliberately absent: credentials never go
// to a log, and SMTPTLSMode is the field that actually explains a silent
// failure, because a relay reached under the wrong TLS policy fails the same
// way an unreachable one does.
func logStartupAddresses(cfg config.Config, listenAddr string) {
	slog.Info("listening", "addr", listenAddr, "env", cfg.AppEnv)
	slog.Info("sending mail", "smtp_addr", cfg.SMTPAddr, "tls_mode", cfg.SMTPTLSMode)
}
