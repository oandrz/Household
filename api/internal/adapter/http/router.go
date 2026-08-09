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
	Users        usecase.UserRepository
	Memberships  usecase.MembershipRepository
	Sessions     usecase.SessionRepository
	Tokens       usecase.TokenGenerator
	Clock        usecase.Clock
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

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, "NOT_FOUND", "That endpoint does not exist.", nil)
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
				su.Use(rateLimitByIP(newIPRateLimiter(signUpRequestsPerIPPerHour, time.Hour, now)))
				su.Post("/sign-up", handleSignUp(deps))
			})
			auth.Get("/sign-up/{token}", handleSignUpPreview(deps))
			auth.Post("/sign-up/{token}/complete", handleCompleteSignUp(deps))

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
					// POST /bills/{id}/pay and DELETE
					// /bills/{id}/payments/{paymentId} are Task 10's own
					// routes, not this task's -- MarkPaid and UndoPayment
					// already exist on BillService but are deliberately
					// unrouted here.
				})
			})
		})
	})

	return r
}
