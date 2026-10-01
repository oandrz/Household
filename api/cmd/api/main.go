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
	// The zone database, compiled into the binary. Every authenticated
	// request loads the household's time zone by name; on an image with no
	// zoneinfo files that load fails and the request is a 500. Don't remove
	// this because the tests pass without it: a developer's machine has the
	// files, a minimal container may not.
	_ "time/tzdata"

	"github.com/andreasoentoro/hearth/api/internal/adapter/clock"
	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
	"github.com/andreasoentoro/hearth/api/internal/adapter/fx"
	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/adapter/mail"
	"github.com/andreasoentoro/hearth/api/internal/adapter/openrouter"
	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/adapter/telegram"
	"github.com/andreasoentoro/hearth/api/internal/config"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// telegram.StartHandler is declared by the adapter, not imported from
// usecase, so the compiler checks the two signatures agree only where both
// packages are visible -- today, the NewPoller call below. This assertion
// checks the same thing independently of that call site, so a signature
// drift still fails the build, by name, even if construction later moves
// behind a helper or a conditional.
var _ telegram.StartHandler = (*usecase.TelegramAuthService)(nil)

var _ usecase.MailOutbox = (*mail.MailpitOutbox)(nil)

var _ usecase.DatabaseBrowser = (*postgres.BrowseRepo)(nil)

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

	// Deps.Secure is !cfg.IsDevelopment(): outside development, session and
	// CSRF cookies are Secure, so a browser returns them only over HTTPS.
	// This process never terminates TLS, so that guarantee depends on a
	// reverse proxy or load balancer in front of it (web/nginx.conf,
	// .env.example) -- without one, every cookie is silently dropped and
	// every authenticated request 401s with no indication why. That can't be
	// fixed from here, so this warns loudly at the one moment an operator is
	// watching: startup.
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

	adminBrowseSvc, readonlyDB, err := openBrowse(ctx, cfg)
	if err != nil {
		return err
	}
	// Nil in all three of the outcomes that are not a live connection, so this
	// is also the predicate the "enabled" line below reads -- see openBrowse.
	if readonlyDB != nil {
		defer readonlyDB.Close()
	}

	// Repositories. Each is constructed once and shared by every service
	// (and, for Users/Memberships/Sessions, by the HTTP layer directly too)
	// that needs it: one implementation per port, backed by this one pool.
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
	accountRepo := postgres.NewAccountRepo(db)
	holdingRepo := postgres.NewHoldingRepo(db)
	holdingEventRepo := postgres.NewHoldingEventRepo(db)
	holdingValuationRepo := postgres.NewHoldingValuationRepo(db)
	categoryRepo := postgres.NewCategoryRepo(db)
	transactionRepo := postgres.NewTransactionRepo(db)
	budgetRepo := postgres.NewBudgetRepo(db)
	goalRepo := postgres.NewGoalRepo(db)
	billRepo := postgres.NewBillRepo(db)
	retroRepo := postgres.NewRetroRepo(db)
	retroActionRepo := postgres.NewRetroActionRepo(db)
	visionRepo := postgres.NewVisionRepo(db)
	agreementRepo := postgres.NewAgreementRepo(db)
	telegramLinks := postgres.NewTelegramLinkRepo(db)
	telegramAccounts := postgres.NewTelegramAccountRepo(db)
	nudgeRepo := postgres.NewNudgeRepo(db)
	platformAdminRepo := postgres.NewPlatformAdminRepo(db)
	featureFlagRepo := postgres.NewFeatureFlagRepo(db)
	adminAuditRepo := postgres.NewAdminAuditRepo(db)
	adminReauthRepo := postgres.NewAdminReauthAttemptRepo(db)

	hasher := crypto.NewArgon2Hasher(cfg.Argon2Time, cfg.Argon2MemoryKiB, cfg.Argon2Threads)
	tokens := crypto.NewTokenGenerator()
	sysClock := clock.System{}
	mailer := mail.NewSMTPMailer(cfg.SMTPAddr, cfg.SMTPFrom, cfg.AppBaseURL,
		cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPTLSMode)
	// One provider instance, shared by accounts and transactions: both only
	// ever read a rate, so there is no reason for each service to hold its
	// own.
	fxProvider := fx.NewStaticProvider()

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
		Invites:           invites,
		Users:             users,
		Sessions:          sessions,
		Mailer:            mailer,
		Hasher:            hasher,
		Tokens:            tokens,
		Clock:             sysClock,
		SessionTTL:        httpadapter.SessionTTL,
		BaseURL:           cfg.AppBaseURL,
		BotUsername:       cfg.TelegramBotUsername,
		TelegramInviteTTL: usecase.TelegramInviteTTL,
		Codes:             crypto.PairCodes{},
		Accounts:          telegramAccounts,
	})
	apiTokens := postgres.NewAPITokenRepo(db)
	memberSvc := usecase.NewMemberService(usecase.MemberDeps{
		Members:   memberships,
		Sessions:  sessions,
		APITokens: apiTokens,
	})
	apiTokenSvc := usecase.NewAPITokenService(usecase.APITokenDeps{Tokens: apiTokens, Gen: tokens, Clock: sysClock})
	accessSvc := usecase.NewAccessListService(usecase.AccessListDeps{
		Tokens: apiTokens, Chats: telegramAccounts, Members: memberships,
	})
	householdSvc := usecase.NewHouseholdService(usecase.HouseholdDeps{
		Holdings:      holdingRepo,
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
	// Nil unless a bot is configured: nil Deps.Telegram and Deps.TelegramLink
	// is what makes POST /auth/telegram/start and the five /auth/telegram
	// link/unlink routes answer 404, so "not configured" is expressed once,
	// here, rather than re-derived by every consumer.
	var telegramSvc *usecase.TelegramAuthService
	var telegramLinkSvc *usecase.TelegramLinkService
	var telegramPoller *telegram.Poller
	var telegramClient *telegram.Client
	if cfg.TelegramEnabled() {
		client := telegram.NewClient(cfg.TelegramBotToken)
		telegramClient = client
		telegramSvc = usecase.NewTelegramAuthService(usecase.TelegramAuthDeps{
			Links:      telegramLinks,
			Accounts:   telegramAccounts,
			MagicLinks: magicLinks,
			// The same signups repository SignupService holds: Telegram mints
			// no token type of its own -- it writes a row in the existing
			// table, on the existing 24-hour expiry, under the existing global
			// daily ceiling.
			Signups:     signups,
			Sender:      client,
			Tokens:      tokens,
			Clock:       sysClock,
			BaseURL:     cfg.AppBaseURL,
			BotUsername: cfg.TelegramBotUsername,
			Invites:     inviteSvc,
		})
		// Closes the two-way wiring InviteService.SetChats describes:
		// inviteSvc above was built with no Chats because telegramSvc could
		// not exist yet (it needs inviteSvc itself, as TelegramAuthDeps.Invites
		// above). Must run before the poller or the HTTP server starts serving
		// requests.
		inviteSvc.SetChats(telegramSvc)
		telegramPoller = telegram.NewPoller(client, telegramSvc)
		// Built from the same configuration as telegramSvc above: no bot
		// token means neither service exists, and every route either one
		// backs answers 404 identically.
		telegramLinkSvc = usecase.NewTelegramLinkService(usecase.TelegramLinkDeps{
			Links:       telegramLinks,
			Accounts:    telegramAccounts,
			Users:       users,
			Tokens:      tokens,
			Clock:       sysClock,
			BotUsername: cfg.TelegramBotUsername,
		})
	}

	accountSvc := usecase.NewAccountService(usecase.AccountDeps{
		Accounts:   accountRepo,
		Households: households,
		FX:         fxProvider,
		Clock:      sysClock,
		// Read only to refuse a type change on an account that still holds
		// investments -- see AccountDeps.Holdings.
		Holdings: holdingRepo,
	})
	holdingSvc := usecase.NewHoldingService(usecase.HoldingDeps{
		Holdings:   holdingRepo,
		Events:     holdingEventRepo,
		Valuations: holdingValuationRepo,
		Income:     postgres.NewHoldingIncomeRepo(db),
		Accounts:   accountRepo,
		Households: households,
	})

	categorySvc := usecase.NewCategoryService(categoryRepo)
	transactionSvc := usecase.NewTransactionService(usecase.TransactionDeps{
		Transactions: transactionRepo,
		// CategoryRepo also satisfies the narrower CategoryLookup that
		// TransactionService declares -- one repository, two ports, each
		// caller seeing only what it needs (interface segregation).
		Categories: categoryRepo,
		Accounts:   accountRepo,
		Households: households,
		FX:         fxProvider,
	})
	goalSvc := usecase.NewGoalService(usecase.GoalDeps{
		Goals:      goalRepo,
		Households: households,
		FX:         fxProvider,
	})
	budgetSvc := usecase.NewBudgetService(usecase.BudgetDeps{
		Budgets:      budgetRepo,
		Transactions: transactionRepo,
		Categories:   categoryRepo,
		Households:   households,
		Members:      memberships,
		FX:           fxProvider,
		// Goals is read only by RollOver to fetch the target goal before
		// writing a rollover contribution. Wire it anyway: a nil port
		// reachable from an already-wired service is a panic waiting to
		// happen.
		Goals: goalRepo,
	})
	billSvc := usecase.NewBillService(usecase.BillDeps{
		Bills:      billRepo,
		Households: households,
		FX:         fxProvider,
		// AccountRepo already satisfies the narrower AccountLookup BillService
		// declares, the same one TransactionDeps.Accounts is wired with above
		// -- one repository, two ports, each caller seeing only what it needs.
		Accounts: accountRepo,
		// The same CategoryLookup TransactionDeps is wired with: a paid bill
		// becomes a real expense, so its category is validated by the same
		// rule as a hand-entered one (see BillDeps).
		Categories: categoryRepo,
	})
	retroSvc := usecase.NewRetroService(retroRepo, retroActionRepo, households)
	// goalRepo doubles as the GoalProgressReader: Vision needs one
	// percentage from Goals and the narrow port is what keeps it from
	// depending on GoalRepository's whole surface.
	visionSvc := usecase.NewVisionService(visionRepo, goalRepo)
	agreementSvc := usecase.NewAgreementService(agreementRepo, memberships)
	adminSvc := usecase.NewAdminService(usecase.AdminDeps{
		Admins: platformAdminRepo,
		Flags:  featureFlagRepo,
		Audit:  adminAuditRepo,
		Clock:  sysClock,
	})
	// Policy is left zero on purpose: NewAdminReauthService fills in
	// domain.DefaultLockoutPolicy(), the same policy NewAuthService uses for
	// the household lock. Both locks share that policy but count into
	// separate ledgers (00012_admin.sql).
	adminReauthSvc := usecase.NewAdminReauthService(usecase.AdminReauthDeps{
		Users:    users,
		Attempts: adminReauthRepo,
		Hasher:   hasher,
		Clock:    sysClock,
	})
	// Policy is left zero here too: the drill-in's lockout line must be
	// computed by the identical policy sign-in applies, and both
	// constructors fill in domain.DefaultLockoutPolicy() when handed none.
	adminDirectorySvc := usecase.NewAdminDirectoryService(usecase.AdminDirectoryDeps{
		Directory:     postgres.NewAdminDirectoryRepo(db),
		LoginAttempts: loginAttempts,
		Clock:         sysClock,
	})
	// Nil unless MAILPIT_API_URL is set. httpadapter.Deps.AdminOutbox being
	// nil is what makes the two /admin/mail routes answer 503 and name the
	// variable; the routes themselves are registered either way.
	var adminOutboxSvc *usecase.AdminOutboxService
	if cfg.OutboxEnabled() {
		adminOutboxSvc = usecase.NewAdminOutboxService(mail.NewMailpitOutbox(cfg.MailpitAPIURL))
	}

	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.Port),
		Handler: httpadapter.NewRouter(httpadapter.Deps{
			Pinger:         db,
			Auth:           authSvc,
			Invites:        inviteSvc,
			Members:        memberSvc,
			Households:     householdSvc,
			Signups:        signupSvc,
			Accounts:       accountSvc,
			Transactions:   transactionSvc,
			Categories:     categorySvc,
			Budgets:        budgetSvc,
			Goals:          goalSvc,
			Holdings:       holdingSvc,
			Bills:          billSvc,
			Retros:         retroSvc,
			Visions:        visionSvc,
			Agreements:     agreementSvc,
			APITokens:      apiTokenSvc,
			APITokenRepo:   apiTokens,
			Telegram:       telegramSvc,
			TelegramLink:   telegramLinkSvc,
			Access:         accessSvc,
			Admin:          adminSvc,
			AdminReauth:    adminReauthSvc,
			AdminDirectory: adminDirectorySvc,
			AdminOutbox:    adminOutboxSvc,
			AdminBrowse:    adminBrowseSvc,
			Users:          users,
			Memberships:    memberships,
			Sessions:       sessions,
			Tokens:         tokens,
			Clock:          sysClock,
			Secure:         !cfg.IsDevelopment(),
			TrustedProxies: cfg.TrustedProxies,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		// ReadTimeout bounds the whole request, not just its headers --
		// ReadHeaderTimeout alone leaves a slow-body attack, or a request
		// that never finishes, free to hold the connection open indefinitely.
		ReadTimeout: 15 * time.Second,
		// MaxHeaderBytes is set explicitly rather than left to net/http's
		// unstated default: it is the request-size bound on the *server*,
		// alongside the JSON-body bound (httpadapter's maxRequestBodyBytes,
		// in errors.go) that lives in the handler layer.
		MaxHeaderBytes: 1 << 20,
	}

	// serveErr carries ListenAndServe's outcome back to the main path.
	// Buffered so the goroutine never blocks sending, and read only after
	// <-ctx.Done(): a listen failure sends before calling stop() (which is
	// what unblocks ctx.Done()), so the send always happens-before the read.
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

	// The poller is a bare goroutine beside the server's, cancelled by the same
	// signal context, and nothing waits on it at shutdown -- a deliberate
	// trade-off: ctx passes straight through to HandleStart, so a /start in
	// flight at SIGTERM is cancelled mid-flight. That's safe because
	// HandleStart holds no multi-statement transaction, so the worst durable
	// outcome is a spent nonce with no reply (the person presses Start again
	// -- the bot's refusal already says so) or an unused magic-link row that
	// expires in fifteen minutes. Draining instead would need a WaitGroup
	// that run() waits on, which only helps if the supervisor's kill timeout
	// outlasts the in-flight send; otherwise it trades a clean cancellation
	// for a write racing process death. Add the WaitGroup once this loop
	// writes more than one row per update.
	if telegramPoller != nil {
		// Chat commands ride the same poller. The Commander is the channel's
		// inbound guard (ADR 8): it resolves the chat to a membership and
		// refuses anyone who is not an owner with Money before any service is
		// called -- wired here, after the money services exist.
		commander := telegram.NewCommander(
			&usecase.TelegramCallerService{Accounts: telegramAccounts, Memberships: memberships},
			usecase.NewTelegramCommandService(usecase.TelegramCommandDeps{
				Accounts:     accountSvc,
				Categories:   categorySvc,
				Transactions: transactionSvc,
				Households:   households,
				Clock:        sysClock,
				Nudges:       nudgeRepoIfEnabled(cfg, nudgeRepo),
			}),
			telegramClient,
		)
		// Free text is read by Claude only when a key is configured, and
		// written only after the person confirms (commands.go). Without a
		// key the bot is commands-only and says so.
		if cfg.IntentParsingEnabled() {
			parser, err := openrouter.NewIntentParser(cfg.OpenRouterAPIKey, cfg.OpenRouterModel)
			if err != nil {
				slog.Error("openrouter configuration refused", "error", err)
				os.Exit(1)
			}
			commander.WithIntentParser(parser)
		}
		telegramPoller.WithCommands(commander)
		slog.Info("telegram sign-in and chat commands enabled",
			"bot_username", cfg.TelegramBotUsername, "free_text", cfg.IntentParsingEnabled(),
			"model", cfg.OpenRouterModel)
		go telegramPoller.Run(ctx)

		// The daily digest: a bare goroutine like the poller's, cancelled by
		// the same context and recovering the same way (no Recoverer over
		// it). It ticks every fifteen minutes and lets NudgeDue and the claim
		// ledger decide, so a restart at 09:01 still delivers at 09:15 and
		// every tick after the first is a no-op in the database.
		if cfg.NudgesEnabled() {
			nudges := usecase.NewNudgeService(usecase.NudgeDeps{
				Recipients: nudgeRepo, Bills: billSvc, Budgets: budgetSvc, Sender: telegramClient,
			})
			go runNudges(ctx, nudges, cfg.NudgesAt, cfg.NudgesLocation, nudgeRepo)
			slog.Info("daily digest enabled", "at", cfg.NudgesAt, "timezone", cfg.NudgesLocation.String())
		}
	}

	if cfg.OutboxEnabled() {
		slog.Info("outbound message inspector enabled", "mailpit_api_url", cfg.MailpitAPIURL)
	}

	// The predicate is readonlyDB != nil ("a live pool was opened"), not
	// cfg.BrowseEnabled() or adminBrowseSvc != nil: those three agree except
	// when configured-but-unreachable, where the service exists (so the
	// browse answers DB_BROWSE_UNAVAILABLE, not "you never set the variable")
	// but logging "enabled" here would contradict openBrowse's own error. No
	// arguments logged -- the value is a DSN carrying a password.
	if readonlyDB != nil {
		slog.Info("database browse enabled")
	}

	<-ctx.Done()

	// If the listener itself failed to start, there is nothing to shut down,
	// and the failure must propagate so the process exits non-zero. A
	// signal-driven shutdown leaves serveErr empty here, since ListenAndServe
	// only returns (with ErrServerClosed) once Shutdown is called below.
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

// openBrowse decides what the operator's database browse is wired with --
// run() only propagates the error and closes the pool. It is a separate,
// testable function because these outcomes are security-relevant: a browse
// served through a writable connection is worse than no browse at all.
//
// The four outcomes are deliberately not the same:
//
//   - Not configured. Both returns are nil, so Deps.AdminBrowse is nil and
//     the two /admin/db routes answer 503 naming DATABASE_READONLY_URL --
//     registered either way, so the route tree never changes with config.
//   - Misconfigured -- an unparseable DSN, or one that connects as a role
//     that may write. Refuses the boot: both are typos no retry fixes, and a
//     "read-only" browse over a writable connection is worse than none.
//   - Opened but unusable -- unreachable, the pool could not be created, or
//     the privilege check errored. Does NOT refuse the boot: this is the
//     case where someone is restoring the product from the paper key with
//     the role not created yet, and taking the whole household product down
//     over an operator panel would invert the promise the read-only role
//     exists to keep. Wired with postgres.UnavailableBrowse, so it answers
//     503 DB_BROWSE_UNAVAILABLE ("the box could not open this") rather than
//     the "you never set the variable" 503 a nil service gives, which would
//     send the operator to fix an .env line that is already correct.
//   - Live. The service is real and the *ReadOnlyDB comes back so run() can
//     close it.
//
// The returned *postgres.ReadOnlyDB is non-nil in exactly the last case, which
// is why it -- and not "the service is non-nil" -- is what the startup log's
// "database browse enabled" line is allowed to read.
func openBrowse(ctx context.Context, cfg config.Config) (*usecase.AdminBrowseService, *postgres.ReadOnlyDB, error) {
	if !cfg.BrowseEnabled() {
		return nil, nil, nil
	}

	readonlyDB, err := postgres.OpenReadOnly(ctx, cfg.DatabaseReadonlyURL)
	switch {
	case err == nil:
		return usecase.NewAdminBrowseService(postgres.NewBrowseRepo(readonlyDB)), readonlyDB, nil
	case errors.Is(err, postgres.ErrReadOnlyMisconfigured):
		return nil, nil, err
	default:
		// The variable is named in the message rather than left to the
		// wrapped error's text: pgx builds connect errors from `user=` and
		// `database=` alone, so without this the log line would never
		// mention the .env line that caused it. The DSN itself stays absent:
		// it carries a password, and credentials never go to a log (see
		// logStartupAddresses).
		slog.Error("DATABASE_READONLY_URL is set but the read-only database could not be opened; "+
			"the database browse will answer 503 and the rest of Hearth is unaffected", "error", err)
		// The failure travels with the stand-in, so every 503 it produces can
		// log why the pool was never opened -- not only that it was not.
		return usecase.NewAdminBrowseService(postgres.NewUnavailableBrowse(err)), nil, nil
	}
}

// logStartupAddresses reports the two addresses an operator needs at the one
// moment they are watching: this process's listen port, and the SMTP server
// it sends through. Mail is the recovery path (magic link is the only way
// back into a locked household) and the send is fire-and-forget, so nothing
// downstream ever surfaces a wrong SMTP target -- printing it here is what
// lets someone tell "pointed at the wrong host" from "the relay refused it",
// and in development it reminds you that mail lands in Mailpit rather than a
// real inbox.
//
// SMTPUsername and SMTPPassword are deliberately absent: credentials never go
// to a log. SMTPTLSMode explains a silent failure, since a relay reached
// under the wrong TLS policy fails the same way an unreachable one does.
func logStartupAddresses(cfg config.Config, listenAddr string) {
	slog.Info("listening", "addr", listenAddr, "env", cfg.AppEnv)
	slog.Info("sending mail", "smtp_addr", cfg.SMTPAddr, "tls_mode", cfg.SMTPTLSMode)
}

// nudgeRepoIfEnabled gives /nudges its repository only when the digest runs,
// so the command can say "not configured here" instead of saving a choice
// nothing reads. A typed nil would not do: the interface would be non-nil.
func nudgeRepoIfEnabled(cfg config.Config, repo *postgres.NudgeRepo) usecase.NudgeRepository {
	if !cfg.NudgesEnabled() {
		return nil
	}
	return repo
}

// runNudges is the digest's clock. Deliveries older than a month are pruned
// on the first tick of each run and then daily, beside the send.
func runNudges(ctx context.Context, svc *usecase.NudgeService, at string, loc *time.Location, repo *postgres.NudgeRepo) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		nudgeTick(ctx, svc, at, loc, repo)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// nudgeTick is one tick, recovering per tick the way the poller recovers per
// update: a panic costs one delivery attempt, not the digest until the next
// deploy.
func nudgeTick(ctx context.Context, svc *usecase.NudgeService, at string, loc *time.Location, repo *postgres.NudgeRepo) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("daily digest tick panicked", "panic", r)
		}
	}()
	day, due := usecase.NudgeDue(time.Now(), at, loc)
	if !due {
		return
	}
	svc.RunOnce(ctx, day)
	if n, err := repo.Prune(ctx, day.AddDate(0, -1, 0)); err != nil {
		slog.Error("nudge prune failed", "error", err)
	} else if n > 0 {
		slog.Info("nudge deliveries pruned", "rows", n)
	}
}
