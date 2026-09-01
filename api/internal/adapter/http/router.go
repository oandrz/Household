package httpadapter

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// signUpRequestsPerIPPerHour bounds one client's sign-up requests. The
// per-address limit in SignupService is bypassed by varying the address; this is
// what actually bounds outbound mail. 5 is generous for a household setting
// itself up (one request, maybe a couple of retries) and far below what a loop
// needs to be useful.
//
// This limit must bind before usecase.SignupGlobalDailyLimit, the global
// daily mail ceiling, and the arithmetic has to be checked whenever either
// number changes: signUpRequestsPerIPPerHour * 24 is the most mail a single
// IP can ever cause in a day while staying entirely within its own budget. If
// that figure reaches the global ceiling, one address, never having been
// rate-limited itself, can silently exhaust the platform's entire daily mail
// budget -- and because the global ceiling's own failure mode is silent (every
// sign-up still answers 202 and mails nothing, for up to a day, with no
// caller-visible signal), nobody finds out from a complaint the way they would
// from this limit's very loud, very local 429. The asymmetry is the whole
// reason this limit exists: it must trip first, for one caller, long before
// the shared ceiling ever could. TestSignUpRateLimitsCompose
// (middleware_ratelimit_test.go) asserts the inequality so raising either
// number in isolation fails a test instead of silently reopening this.
const signUpRequestsPerIPPerHour = 5

// Deps carries everything the HTTP layer needs. Handlers receive their
// collaborators through this struct rather than reaching for globals.
//
// Users and Memberships are raw repository ports, not routed through a
// service, because no Task 12-15 service exposes what this layer needs from
// them directly: GET /auth/me assembles its bundle from the user's own
// profile (Users.ByID) and the session middleware resolves a caller's
// Membership (Memberships.ByUser) to populate Scope. Every other Deps field
// mirrors what the task brief names.
type Deps struct {
	Pinger       Pinger
	Auth         *usecase.AuthService
	Invites      *usecase.InviteService
	Members      *usecase.MemberService
	Households   *usecase.HouseholdService
	Signups      *usecase.SignupService
	Accounts     *usecase.AccountService
	Transactions *usecase.TransactionService
	Categories   *usecase.CategoryService
	Budgets      *usecase.BudgetService
	Goals        *usecase.GoalService
	Bills        *usecase.BillService
	Retros       *usecase.RetroService
	Visions      *usecase.VisionService
	// Telegram is nil when no bot is configured. The route checks for nil
	// rather than being conditionally registered, so the router's shape does
	// not change with configuration and every test builds the same tree.
	Telegram *usecase.TelegramAuthService
	// Admin and AdminReauth are the platform-operator surface's two
	// services. Unlike Telegram above they are never nil in a real
	// deployment: the /admin subtree is always routed, and
	// requirePlatformAdmin's 404 -- not conditional registration -- is what
	// hides it. Leaving Admin nil therefore does not just disable /admin: it
	// panics into recoverer's 500 on every auth flow too, since
	// buildMeResponse now reads it to answer the me bundle's flags and
	// admin bit -- sign-in, magic-link consumption and invite acceptance
	// all resolve through buildMeResponse before ever writing a cookie.
	Admin       *usecase.AdminService
	AdminReauth *usecase.AdminReauthService
	Users       usecase.UserRepository
	Memberships usecase.MembershipRepository
	Sessions    usecase.SessionRepository
	Tokens      usecase.TokenGenerator
	Clock       usecase.Clock
	// Secure controls the Secure flag on the session and CSRF cookies. It is
	// !cfg.IsDevelopment(): false only in development, so cookies still work
	// over plain http on localhost.
	Secure bool
}

func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	// recoverer, not middleware.Recoverer -- see its own doc comment in
	// middleware_recoverer.go for why chi's version (a bare, bodyless 500)
	// is wrong for this API.
	r.Use(recoverer)

	// writeNotFound, not an inline WriteError: requirePlatformAdmin answers
	// with the identical helper, so a hidden admin route and a genuinely
	// absent one cannot drift apart into two distinguishable bodies.
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeNotFound(w)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "That method is not allowed here.", nil)
	})

	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", handleReadyz(deps.Pinger))

	r.Route("/api/v1", func(api chi.Router) {
		api.Route("/auth", func(auth chi.Router) {
			// Public: no session exists yet.
			auth.Post("/sign-in", handleSignIn(deps))
			auth.Post("/magic-link", handleRequestMagicLink(deps))
			auth.Post("/magic-link/consume", handleConsumeMagicLink(deps))

			// Public: no session exists yet, and no CSRF cookie either.
			//
			// The per-IP limiter wraps only the request endpoint. The preview
			// and complete endpoints need a token that was mailed to a real
			// address, so they are not a path to unbounded mail; the request
			// endpoint is.
			auth.Group(func(su chi.Router) {
				// deps.Clock.Now, not a bound method value: forming the method
				// value would read deps.Clock immediately, while NewRouter is
				// still building the route tree -- and some callers (health_test.go's
				// Deps{Pinger: ...}) build a router with every other field left
				// zero, deliberately, because they only exercise /healthz. A closure
				// defers that read to request time, when every route's Deps is
				// actually complete.
				now := func() time.Time { return deps.Clock.Now() }
				// The limiter stays outermost: a closed sign-up route must
				// not become an unmetered way to make flag lookups.
				su.Use(rateLimitByIP(newIPRateLimiter(signUpRequestsPerIPPerHour, time.Hour, now)))
				su.Use(requireFeature(deps, domain.FlagSignupsOpen))
				su.Post("/sign-up", handleSignUp(deps))
			})
			// The two token routes stay behind the same flag as the request
			// route above: a half-finished sign-up must not be completable
			// after registration closes, or a token minted before the switch
			// stays redeemable indefinitely.
			auth.Group(func(tok chi.Router) {
				tok.Use(requireFeature(deps, domain.FlagSignupsOpen))
				tok.Get("/sign-up/{token}", handleSignUpPreview(deps))
				tok.Post("/sign-up/{token}/complete", handleCompleteSignUp(deps))
			})

			// Its own limiter instance, not the sign-up group's: a person who
			// has just signed up should not find Telegram sign-in already
			// spent, and vice versa.
			auth.Group(func(tg chi.Router) {
				now := func() time.Time { return deps.Clock.Now() }
				tg.Use(rateLimitByIP(newIPRateLimiter(telegramStartsPerIPPerHour, time.Hour, now)))
				tg.Use(requireFeature(deps, domain.FlagTelegramSignIn))
				tg.Post("/telegram/start", handleTelegramStart(deps))
			})

			auth.Group(func(g chi.Router) {
				g.Use(requireSession(deps))
				g.Get("/me", handleMe(deps))

				g.Group(func(m chi.Router) {
					m.Use(requireCSRF)
					m.Post("/sign-out", handleSignOut(deps))
				})
			})
		})

		// Public: reached before the caller has any session at all.
		api.Get("/invites/{token}", handleInvitePreview(deps))
		api.Post("/invites/{token}/accept", handleAcceptInvite(deps))

		// Public: the sign-up form reads this before any session exists, and
		// Settings reads it after one. One list, served rather than duplicated
		// in the frontend.
		api.Get("/currencies", handleListCurrencies())

		api.Group(func(g chi.Router) {
			g.Use(requireSession(deps))

			g.Get("/household", handleGetHousehold(deps))
			g.Get("/household/members", handleListMembers(deps))
			g.Get("/spaces", handleListSpaces(deps))
			g.Get("/notification-preferences", handleGetNotificationPreferences(deps))

			// The Family calendar's API stub, dark behind its flag. It answers
			// an empty list rather than 501: a flag-gated route must behave
			// like a real route once its flag is on, or the flag proves
			// nothing about the feature it guards.
			g.Group(func(f chi.Router) {
				f.Use(requireFeature(deps, domain.FlagFamilyCalendar))
				f.Get("/family/calendar", handleListCalendarEvents(deps))
			})

			g.Group(func(m chi.Router) {
				m.Use(requireCSRF)

				m.Group(func(o chi.Router) {
					o.Use(requireOwner)
					o.Patch("/household", handleUpdateHousehold(deps))
					o.Patch("/notification-preferences", handleUpdateNotificationPreferences(deps))
					o.Post("/household/members/invite", handleInviteMember(deps))
					o.Patch("/household/members/{id}", handleUpdateMember(deps))
					o.Delete("/household/members/{id}", handleRemoveMember(deps))
					o.Post("/spaces", handleCreateSpace(deps))
				})
			})

			// Accounts: the first capability-gated routes in the product. Reads
			// need money; writes need money and owner, stacked -- see
			// middleware_capability.go for why the redundancy is deliberate.
			g.Group(func(a chi.Router) {
				a.Use(requireCapability(domain.CapMoney))
				a.Get("/accounts", handleListAccounts(deps))

				a.Group(func(w chi.Router) {
					w.Use(requireCSRF)
					w.Use(requireOwner)
					w.Post("/accounts", handleCreateAccount(deps))
					w.Patch("/accounts/{id}", handleUpdateAccount(deps))
					// Archive and restore are their own routes rather than a
					// field on PATCH: if archiving were patchable, an ordinary
					// edit that happened to include it would archive the account
					// as a side effect of saving a nickname.
					w.Post("/accounts/{id}/archive", handleArchiveAccount(deps))
					w.Post("/accounts/{id}/restore", handleRestoreAccount(deps))
				})
			})

			// Transactions requires money AND owner for reads as well as
			// writes, which is deliberately unlike accounts above.
			//
			// A limited member's accounts view shows names with no amounts
			// (accounts decision 5). Applied to a ledger that is a table whose
			// every figure is blank, next to a "Spent this month" that has to
			// be absent rather than zero -- a page that reads as broken. So
			// for a limited member the money capability means "see which
			// accounts this household has" and nothing further. Do not
			// "simplify" this to match the accounts group.
			g.Group(func(txn chi.Router) {
				txn.Use(requireCapability(domain.CapMoney))
				// The capability guard above is stacked on requireOwner below
				// even though an owner without money is not a representable
				// state today (domain.ValidateMembershipChange refuses it).
				// This route must not lean on an invariant enforced in
				// another layer for another reason: if that invariant were
				// ever relaxed, this route would silently open with no
				// failing test to catch it.
				txn.Use(requireOwner)

				txn.Get("/transactions", handleListTransactions(deps))
				txn.Get("/categories", handleListCategories(deps))
				txn.Get("/budgets/{month}", handleGetBudgetMonth(deps))
				txn.Get("/budgets/history", handleBudgetHistory(deps))

				txn.Group(func(w chi.Router) {
					w.Use(requireCSRF)
					w.Post("/transactions", handleCreateTransaction(deps))
					w.Patch("/transactions/{id}", handleUpdateTransaction(deps))
					w.Delete("/transactions/{id}", handleDeleteTransaction(deps))

					// Archive and restore are their own routes rather than a
					// field on PATCH, the same reasoning as accounts above:
					// if archiving were patchable, an ordinary rename that
					// happened to include it would archive the category as a
					// side effect of saving a new name.
					w.Post("/categories", handleCreateCategory(deps))
					w.Patch("/categories/{id}", handleRenameCategory(deps))
					w.Post("/categories/{id}/archive", handleArchiveCategory(deps))
					w.Post("/categories/{id}/restore", handleRestoreCategory(deps))
				})

				// PUT /budgets/{month} gets its own CSRF group rather than
				// joining the one above -- the task brief that introduced
				// this route pinned this exact shape, and keeping it
				// distinct means budgets and transactions guards can each
				// change without touching the other's route list. POST
				// .../rollover joins it: both are budget-month writes behind
				// the same money+owner+CSRF stack, and there is no reason
				// for the two to diverge the way budgets and transactions
				// were kept apart above.
				txn.Group(func(w chi.Router) {
					w.Use(requireCSRF)
					w.Put("/budgets/{month}", handlePutBudgetMonth(deps))
					w.Post("/budgets/{month}/rollover", handleRolloverBudgetMonth(deps))
				})

				// Goals sit in the same money+owner group as transactions,
				// categories and budgets, for the same reason router.go's
				// comment above gives for those: a goal card with contributed
				// amounts and progress figures is as much "the household's
				// money" as a ledger row, so there is no reading of it for a
				// limited member that would not read as broken.
				txn.Get("/goals", handleListGoals(deps))
				txn.Get("/goals/{id}/contributions", handleListGoalContributions(deps))

				txn.Group(func(w chi.Router) {
					w.Use(requireCSRF)
					w.Post("/goals", handleCreateGoal(deps))
					w.Patch("/goals/{id}", handleUpdateGoal(deps))
					// Archive and restore are their own routes rather than a
					// field on PATCH, the same reasoning as accounts and
					// categories above: if archiving were patchable, an
					// ordinary rename that happened to include it would
					// archive the goal as a side effect of saving a name.
					w.Post("/goals/{id}/archive", handleArchiveGoal(deps))
					w.Post("/goals/{id}/restore", handleRestoreGoal(deps))
					w.Post("/goals/{id}/contributions", handleAddGoalContribution(deps))
					w.Delete("/goals/{id}/contributions/{contributionId}", handleDeleteGoalContribution(deps))
				})

				// Bills sit in the same money+owner group as transactions,
				// categories, budgets and goals, for the same reason this
				// file's own comment above gives for those: a bill's amount
				// and due date are as much "the household's money" as a
				// ledger row, so there is no reading of it for a limited
				// member that would not read as broken.
				txn.Get("/bills", handleListBills(deps))
				txn.Group(func(w chi.Router) {
					w.Use(requireCSRF)
					w.Post("/bills", handleCreateBill(deps))
					w.Patch("/bills/{id}", handleUpdateBill(deps))
					// Archive and restore are their own routes rather than a
					// field on PATCH, the same reasoning as accounts,
					// categories and goals above: if archiving were
					// patchable, an ordinary rename that happened to
					// include it would archive the bill as a side effect
					// of saving a name.
					w.Post("/bills/{id}/archive", handleArchiveBill(deps))
					w.Post("/bills/{id}/restore", handleRestoreBill(deps))
					// The two routes that move money: pay writes a payment,
					// an expense and the advanced due date in one
					// transaction (BillRepository.RecordPayment's own
					// comment); undo reverses all three
					// (BillRepository.UndoPayment's own comment).
					w.Post("/bills/{id}/pay", handleMarkBillPaid(deps))
					w.Delete("/bills/{id}/payments/{paymentId}", handleUndoBillPayment(deps))
				})
			})

			// Marriage is parents-only, and its capability is refused to
			// limited members in the domain (domain.ErrLimitedCannotHoldMarriage)
			// -- so requireOwner is redundant here TODAY. It is stacked anyway,
			// for the reason the money group's own comment above already gives
			// (the txn group's requireCapability/requireOwner pairing): a route
			// leaning on an invariant enforced in another layer for another
			// reason opens silently the day that invariant is relaxed, with no
			// failing test to catch it.
			g.Group(func(m chi.Router) {
				m.Use(requireCapability(domain.CapMarriage))
				m.Use(requireOwner)

				m.Get("/retros", handleListRetros(deps))
				m.Get("/retros/{month}", handleGetRetro(deps))
				m.Get("/marriage/vision", handleGetVision(deps))

				// Every write joins its own CSRF sub-group, the same shape
				// this file already uses for goals and bills above: the two
				// reads stay outside it because requireCSRF only ever
				// applies to a mutating request.
				m.Group(func(w chi.Router) {
					w.Use(requireCSRF)
					w.Post("/retros", handleStartRetro(deps))
					w.Patch("/retros/{month}", handleSaveRetro(deps))
					// Complete is its own route, not a field on PATCH: if
					// finishing were patchable, saving a typo could finish
					// the retro as a side effect of an ordinary save.
					w.Post("/retros/{month}/complete", handleCompleteRetro(deps))
					w.Delete("/retros/{month}", handleDiscardRetro(deps))
					w.Post("/retros/{month}/actions", handleAddRetroAction(deps))
					w.Patch("/retros/{month}/actions/{id}", handleSetRetroActionDone(deps))
					w.Delete("/retros/{month}/actions/{id}", handleRemoveRetroAction(deps))
					w.Put("/marriage/vision/{year}", handleSaveVision(deps))
				})
			})

			// The admin surface. requirePlatformAdmin answers 404 to everyone
			// else, so this whole subtree is invisible to a household member.
			// auditAdmin wraps it rather than each handler: a handler that
			// forgets to log is the failure mode.
			//
			// requireCSRF is stacked at the subtree root, but INNERMOST of the
			// three -- behind both admin guards. Each half of that is
			// deliberate and neither survives on its own:
			//
			// At the root, rather than around POST /session alone, every
			// mutating admin route is CSRF-checked by construction. Nesting
			// it around the one route that needs it today would leave a
			// mutating route added to the granted group tomorrow with no CSRF
			// guard at all and no test to notice. GET is exempt inside
			// requireCSRF itself, so the reads are unaffected.
			//
			// Innermost, rather than ahead of the guards, so that a
			// CSRF-rejected admin request still writes its audit row. A
			// cross-site forgery aimed at a real platform admin is exactly
			// the event admin_audit_log exists to make visible, and with the
			// CSRF check in front of auditAdmin it would be refused without
			// leaving a trace. It also keeps requirePlatformAdmin's 404 as
			// the first thing a signed-in non-admin meets, whatever they send
			// or omit.
			//
			// The cost is paid by a test, not by the product:
			// TestEveryMutatingRouteRequiresCSRF walks this subtree like any
			// other and needs its caller to get past requirePlatformAdmin
			// first, so it makes its owner a platform admin. See the comment
			// on that line -- it is load-bearing, not scaffolding.
			g.Route("/admin", func(adm chi.Router) {
				adm.Use(requirePlatformAdmin(deps))
				adm.Use(auditAdmin(deps))
				adm.Use(requireCSRF)

				// The one route that must be reachable without a grant --
				// it is how a grant is obtained.
				adm.Post("/session", handleAdminSession(deps))

				adm.Group(func(granted chi.Router) {
					granted.Use(requireAdminGrant(deps))
					granted.Get("/flags", handleListFlags(deps))

					// No requireCSRF here: it already sits at the /admin
					// subtree root, above requireAdminGrant, so every
					// mutating route in this group is covered by
					// construction -- see this file's own comment on the
					// /admin subtree for why it is deliberately outermost
					// of the three guards rather than nested around just
					// these three routes.
					granted.Put("/flags/{key}", handleSetGlobalFlag(deps))
					granted.Put("/flags/{key}/households/{householdID}", handleSetHouseholdFlag(deps))
					granted.Delete("/flags/{key}/households/{householdID}", handleClearHouseholdFlag(deps))
				})
			})
		})
	})

	return r
}
