package httpadapter

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// maxRequestBodyBytes bounds every JSON body this API accepts, except the
// two routes named on decodeJSONBodyLimit's own doc comment below, which
// carry a legitimately larger shape and call decodeJSONBodyLimit directly
// with their own constant instead. It is one constant used everywhere else,
// rather than a per-handler value, so the limit cannot silently drift from
// one route to another. 1 KiB is generous for every other request shape in
// this file: the largest legitimate body (a member invite, or a full
// household update) is a few hundred bytes of field names and short
// strings.
const maxRequestBodyBytes = 1024

// decodeJSONBody reads r.Body bounded to maxRequestBodyBytes and decodes it
// into dest, writing the response itself on any failure. A caller over the
// limit gets 413 PAYLOAD_TOO_LARGE through the standard error envelope
// (never net/http's own bare "http: request body too large" text); any
// other decode failure -- malformed JSON, a wrong-shaped field -- gets 400
// INVALID_BODY, exactly as every handler already answered before this
// helper existed.
//
// Without this, an unauthenticated POST (sign-in, magic-link, magic-link
// consume are all reachable pre-auth and pre-CSRF) carrying a
// multi-gigabyte body would be decoded into memory in full before the
// handler ever got a chance to reject it -- the worst place for an unbounded
// read to live, since none of the usual gates (a session, a CSRF token)
// have run yet.
//
// Usage: `var req someRequest; if !decodeJSONBody(w, r, &req) { return }`.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dest any) bool {
	return decodeJSONBodyLimit(w, r, dest, maxRequestBodyBytes)
}

// decodeJSONBodyLimit is decodeJSONBody's general form, for the routes whose
// legitimate body does not fit maxRequestBodyBytes' "a few hundred bytes"
// assumption:
//
//   - PUT /budgets/{month} always carries the household's entire category
//     list as budget lines (full-replace, never a patch -- see
//     BudgetRepository.Upsert's own doc comment), and that list's length is
//     not bounded by this codebase (Task 10 adds category creation with no
//     cap). budget_handlers.go's maxBudgetRequestBodyBytes is the caller
//     that needs this.
//   - PATCH /retros/{month} carries three free-text fields (went well / was
//     hard / notes) this feature deliberately never caps (RetroUpdate's own
//     doc comment in ports.go). retro_handlers.go's
//     maxRetroRequestBodyBytes is the caller that needs this.
//   - POST /marriage/agreements/proposals and
//     POST /marriage/agreements/proposals/{id}/park carry rune-capped free
//     text (body, previousBody, note, park note) at 500 runes each, and a
//     500-rune CJK field alone exceeds the 1 KiB default.
//     agreement_handlers.go's maxAgreementRequestBodyBytes is the caller that
//     needs this.
//
// Every other route keeps using the tighter default via decodeJSONBody
// above.
func decodeJSONBodyLimit(w http.ResponseWriter, r *http.Request, dest any, maxBytes int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			WriteError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE",
				"The request body is too large.", nil)
			return false
		}
		WriteError(w, http.StatusBadRequest, "INVALID_BODY", "The request body could not be parsed.", nil)
		return false
	}
	return true
}

// telegramChatTakenMessage and telegramAlreadyLinkedMessage are the two
// sentences a member can be given for the same underlying refusal, reached
// two different ways: POST .../confirm surfaces it as one of these 409s
// below, and GET .../link/{id} surfaces it earlier, before a confirm is even
// attempted, as a usecase.TelegramLinkReasonChatTaken /
// usecase.TelegramLinkReasonAlreadyLinked code on the status response
// (telegramLinkReasonMessage, telegram_handlers.go). The usecase layer may
// not import this package or hold user-facing copy (internal/usecase may
// depend only on the standard library and internal/domain), so it hands back
// a stable code and this package -- the only one that may -- turns it into
// words, once, for both call sites.
const (
	telegramChatTakenMessage     = "That Telegram chat is already connected to another Hearth account."
	telegramAlreadyLinkedMessage = "This account already has a Telegram chat. Disconnect it first."
)

// MapDomainError is the single table translating a domain or usecase
// sentinel into the one error envelope every failure response uses.
// Handlers never build an error response by hand; every failure path ends
// here.
//
// It takes *http.Request, not just http.ResponseWriter, so the default,
// unmapped-error branch can recover the chi request ID from the request's
// context via middleware.GetReqID: chi's middleware.RequestID only injects
// the ID into the context, it exposes no other way to read it, and there is
// no path from a bare http.ResponseWriter back to that context. This is a
// deliberate, narrow deviation from the signature the task brief sketches
// (MapDomainError(w, err)) -- see the task report.
//
// The table itself is domainErrorResponses, below; this function is the
// SignInFailedError special case and the loop that walks it.
func MapDomainError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}

	// SignInFailedError must be matched with errors.As, not by relying on
	// errors.Is walking its Unwrap chain: Unwrap alone would correctly pick
	// the right *sentinel* (ErrHouseholdLocked vs ErrInvalidCredentials), but
	// only errors.As recovers the concrete struct, which is what carries
	// AttemptsRemaining and LockedUntil for the response body. The 401 and
	// 423 cases below are intentionally distinct -- see the type's own doc
	// comment in usecase/auth.go -- and must never be collapsed into one
	// status.
	var signInErr *usecase.SignInFailedError
	if errors.As(err, &signInErr) {
		if signInErr.Locked {
			WriteError(w, http.StatusLocked, "HOUSEHOLD_LOCKED",
				"This household is temporarily locked after too many failed sign-in attempts.",
				map[string]any{"lockedUntil": signInErr.LockedUntil})
			return
		}
		WriteError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "That email or password is incorrect.",
			map[string]any{"attemptsRemaining": signInErr.AttemptsRemaining})
		return
	}

	for _, row := range domainErrorResponses {
		if !matchesAny(err, row.sentinels) {
			continue
		}
		if row.internal {
			logAndWriteInternal(w, r, err)
			return
		}
		if row.logAs != "" {
			slog.Error(row.logAs, "error", err, "request_id", middleware.GetReqID(r.Context()))
		}
		WriteError(w, row.status, row.code, row.message, nil)
		return
	}
	logAndWriteInternal(w, r, err)
}

// domainErrorResponse is one row of MapDomainError's table: the sentinels
// that select it and the error envelope it answers with.
type domainErrorResponse struct {
	sentinels []error
	status    int
	code      string
	message   string
	// logAs, when set, logs the error under this message before answering.
	// For a row whose response body is deliberately generic, that log line is
	// the only place the wrapped cause survives.
	logAs string
	// internal answers the generic, logged 500 instead of status, code and
	// message: the sentinel means a calculation went wrong, not that the
	// request was bad, and it has a row only so the log names the cause.
	internal bool
}

func matchesAny(err error, sentinels []error) bool {
	for _, sentinel := range sentinels {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

// domainErrorResponses is every sentinel the API answers with something more
// specific than a 500, IN ORDER. Rows are tried top to bottom and the first
// match wins, exactly as the switch this table replaced did, and the order is
// load-bearing: errors.Is walks wrapped errors, so one error can match more
// than one row. *domain.BillNotPayableError and *domain.BillPaymentNotLatestError
// both unwrap to domain.ErrForbidden, and a sentinel a service translated out
// of domain.ErrAlreadyExists must be answered by its own row, which is why
// ALREADY_EXISTS sits last. Add a new row where its meaning belongs, above
// any more general row it could also match.
//
// A sentinel with no row falls through to the logged 500. Several are absent
// on purpose, and the comments where their rows would sit say why.
var domainErrorResponses = []domainErrorResponse{
	{
		sentinels: []error{domain.ErrInvalidCredentials},
		status:    http.StatusUnauthorized,
		code:      "INVALID_CREDENTIALS",
		message:   "That email or password is incorrect.",
	},
	{
		// Deliberately its own code and its own message, never folded into
		// HOUSEHOLD_LOCKED below: the two locks are counted in separate
		// ledgers on purpose (see the admin_reauth_attempts comment in
		// 00012_admin.sql), and telling an operator their *household* is
		// locked when only the admin surface is would send them to reset a
		// password that is working fine.
		sentinels: []error{domain.ErrAdminLocked},
		status:    http.StatusLocked,
		code:      "ADMIN_LOCKED",
		message:   "Too many failed attempts. Try again in a few minutes.",
	},
	{
		sentinels: []error{domain.ErrHouseholdLocked},
		status:    http.StatusLocked,
		code:      "HOUSEHOLD_LOCKED",
		message:   "This household is temporarily locked after too many failed sign-in attempts.",
	},
	{
		sentinels: []error{domain.ErrNotFound},
		status:    http.StatusNotFound,
		code:      "NOT_FOUND",
		message:   "That could not be found.",
	},
	{
		sentinels: []error{domain.ErrForbidden},
		status:    http.StatusForbidden,
		code:      "FORBIDDEN",
		message:   "You do not have permission to do that.",
	},
	{
		sentinels: []error{domain.ErrLastOwner},
		status:    http.StatusConflict,
		code:      "LAST_OWNER",
		message:   "A household must keep at least one owner.",
	},
	{
		sentinels: []error{domain.ErrLimitedCannotHoldMarriage, domain.ErrOwnerMustHoldAllCapabilities, domain.ErrUnknownCapability},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_CAPABILITIES",
		message:   "That capability set is not valid for this role.",
	},
	{
		sentinels: []error{domain.ErrUnknownRole},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_ROLE",
		message:   "That role is not recognised.",
	},
	{
		sentinels: []error{domain.ErrUnknownFlag},
		status:    http.StatusUnprocessableEntity,
		code:      "UNKNOWN_FLAG",
		message:   "That feature flag does not exist in this build.",
	},
	{
		// Reaching the HTTP layer means a calculation is wrong, not that the
		// caller sent a bad request -- nothing on this API surface accepts a
		// caller-supplied amount that could overflow. Handled like the
		// default branch (logged, generic 500) with its own case only so the
		// log line names the specific cause.
		sentinels: []error{domain.ErrAmountOverflow},
		internal:  true,
	},
	{
		// Unlike ErrAmountOverflow above, this is no longer only an internal-
		// arithmetic signal: HouseholdService.Update (Task 15) wraps a
		// caller-supplied currency code's domain.NewMoney failure in this
		// same sentinel (see normalizeCurrency in usecase/household.go), so
		// a typo in PATCH /household's primaryCurrency or secondaryCurrency
		// field reaches here too. That is an ordinary bad request, not a
		// calculation gone wrong, and must not 500.
		sentinels: []error{domain.ErrInvalidMoney},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_CURRENCY",
		message:   "That currency code is not valid.",
	},
	{
		sentinels: []error{domain.ErrInviteExpired},
		status:    http.StatusGone,
		code:      "INVITE_EXPIRED",
		message:   "This invite has expired.",
	},
	{
		sentinels: []error{domain.ErrInviteRequiresEmail},
		status:    http.StatusUnprocessableEntity,
		code:      "INVITE_REQUIRES_EMAIL",
		message:   "An invite requires an email address.",
	},
	{
		sentinels: []error{usecase.ErrPasswordTooShort},
		status:    http.StatusUnprocessableEntity,
		code:      "PASSWORD_TOO_SHORT",
		message:   "Password must be at least 12 characters.",
	},
	{
		sentinels: []error{usecase.ErrPasswordTooLong},
		status:    http.StatusUnprocessableEntity,
		code:      "PASSWORD_TOO_LONG",
		message:   "Password must be at most 256 characters.",
	},
	{
		sentinels: []error{domain.ErrInviteAlreadyAccepted},
		status:    http.StatusConflict,
		code:      "INVITE_ALREADY_ACCEPTED",
		message:   "This invite has already been accepted.",
	},
	{
		sentinels: []error{domain.ErrTokenExpired},
		status:    http.StatusGone,
		code:      "TOKEN_EXPIRED",
		message:   "This link has expired or has already been used.",
	},
	{
		sentinels: []error{domain.ErrRateLimited},
		status:    http.StatusTooManyRequests,
		code:      "RATE_LIMITED",
		message:   "Too many requests. Try again later.",
	},
	{
		sentinels: []error{usecase.ErrSpaceNameTaken},
		status:    http.StatusConflict,
		code:      "SPACE_NAME_TAKEN",
		message:   "A space with that name already exists.",
	},
	{
		sentinels: []error{usecase.ErrSpaceVisibilityNotSupported},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_VISIBILITY",
		message:   "That visibility is not supported yet.",
	},
	{
		sentinels: []error{usecase.ErrSpaceNameRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "SPACE_NAME_REQUIRED",
		message:   "A space name is required.",
	},
	{
		sentinels: []error{usecase.ErrInvalidFXRateMode},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_FX_RATE_MODE",
		message:   "That FX rate mode is not valid.",
	},
	{
		// 502 rather than 500: the failure is upstream of this service, not
		// a bug in it. That is not always "go look at Mailpit, not here",
		// though -- a stray path segment in MAILPIT_API_URL surfaces as this
		// same sentinel (see the 404-on-list case in mailpit_outbox.go's
		// Recent), and the wrapped cause logged below is the only thing
		// that names the .env line to fix in that case. Deliberately
		// distinct from the 503 an unconfigured inspector answers -- one
		// means "set the variable", the other means "the container is
		// down", and collapsing them would send the operator to fix the
		// wrong thing.
		sentinels: []error{usecase.ErrOutboxUnavailable},
		logAs:     "mail outbox unavailable",
		status:    http.StatusBadGateway,
		code:      "MAIL_UPSTREAM_UNAVAILABLE",
		message:   "Mailpit is not answering. The messages are not lost — the reader is.",
	},
	{
		// 503 rather than 502: unlike the mail inspector, the failure is not
		// upstream of this service in another process -- it is this
		// install's own second connection, and the advice is "look at
		// DATABASE_READONLY_URL and the hearth_readonly role", not "look at
		// something else". Deliberately a different code from
		// DB_BROWSE_NOT_CONFIGURED: "no value set" and "the value is set and
		// the connection is broken" send the operator to different places.
		//
		// Logged for the same reason the outbox branch above is, and it
		// matters more here: the response body is deliberately generic, so
		// this line is the ONLY place the cause survives. browseErr in
		// browse_repo.go wraps both the failing operation and the pg error
		// into this sentinel (`%w: %s: %v`) precisely so this layer can
		// record them -- dropping the log would throw away the only thing
		// that distinguishes a dead connection from a statement timeout
		// from a revoked privilege.
		sentinels: []error{usecase.ErrBrowseUnavailable},
		logAs:     "database browse unavailable",
		status:    http.StatusServiceUnavailable,
		code:      "DB_BROWSE_UNAVAILABLE",
		message:   "The database browse cannot reach its read-only connection.",
	},
	{
		// Defence in depth: the handler already refuses a negative offset
		// with the same code, and this covers any other caller of the
		// service.
		sentinels: []error{usecase.ErrInvalidOffset},
		status:    http.StatusBadRequest,
		code:      "INVALID_RANGE",
		message:   "offset must not be negative.",
	},
	{
		sentinels: []error{usecase.ErrInviteeAlreadyRegistered},
		status:    http.StatusConflict,
		code:      "EMAIL_ALREADY_REGISTERED",
		message:   "An account with that email address already exists.",
	},
	{
		// Deliberately not folded into ALREADY_EXISTS, whose copy ("That
		// already exists.") tells the holder of a spent sign-up link nothing
		// useful, and whose own comment scopes it to a write race.
		sentinels: []error{usecase.ErrSignupAlreadyUsed},
		status:    http.StatusConflict,
		code:      "SIGNUP_ALREADY_USED",
		message:   "This link has already been used. Try signing in instead.",
	},
	{
		sentinels: []error{usecase.ErrHouseholdNameRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "HOUSEHOLD_NAME_REQUIRED",
		message:   "A household name is required.",
	},
	{
		sentinels: []error{usecase.ErrDisplayNameRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "DISPLAY_NAME_REQUIRED",
		message:   "Your name is required.",
	},
	{
		sentinels: []error{domain.ErrAccountNicknameRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "NICKNAME_REQUIRED",
		message:   "An account name is required.",
	},
	{
		sentinels: []error{domain.ErrUnknownAccountType},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_TYPE",
		message:   "That account type is not recognised.",
	},
	{
		sentinels: []error{domain.ErrLiabilityBalanceNegative},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_BALANCE",
		message:   "Enter what you owe as a positive amount — Hearth subtracts it for you.",
	},
	{
		sentinels: []error{domain.ErrOpeningBalanceInFuture},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_AS_OF",
		message:   "That date is in the future.",
	},
	{
		sentinels: []error{domain.ErrAccountOwnerNotInHousehold},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_OWNER",
		message:   "That person is not in this household.",
	},
	{
		sentinels: []error{domain.ErrAPITokenNameInvalid},
		status:    http.StatusUnprocessableEntity,
		code:      "TOKEN_NAME_INVALID",
		message:   "Give the token a name of up to 80 characters.",
	},
	{
		sentinels: []error{domain.ErrAPITokenLifetimeInvalid},
		status:    http.StatusUnprocessableEntity,
		code:      "TOKEN_LIFETIME_INVALID",
		message:   "expiresInDays must be between 1 and 365.",
	},
	{
		sentinels: []error{domain.ErrIdempotencyKeyInvalid},
		status:    http.StatusUnprocessableEntity,
		code:      "IDEMPOTENCY_KEY_INVALID",
		message:   "Idempotency-Key must be 1 to 128 printable ASCII characters with no spaces.",
	},
	{
		// Only reachable on the race the service comments on: the index
		// said the key exists, the lookup found nothing, so the row was
		// deleted in between. A 409 that says "retry" is honest; a 500 is
		// not.
		sentinels: []error{domain.ErrIdempotencyKeyInUse},
		status:    http.StatusConflict,
		code:      "IDEMPOTENCY_KEY_IN_USE",
		message:   "This Idempotency-Key was in use a moment ago and is now free. Retry the request.",
	},
	{
		sentinels: []error{domain.ErrIdempotencyKeyReused},
		status:    http.StatusConflict,
		code:      "IDEMPOTENCY_KEY_REUSED",
		message:   "This Idempotency-Key was already used for a different transaction. Use a new key.",
	},
	{
		sentinels: []error{domain.ErrTransactionDescriptionRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "DESCRIPTION_REQUIRED",
		message:   "Give this transaction a description.",
	},
	{
		sentinels: []error{domain.ErrUnknownTransactionKind},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_KIND",
		message:   "That is not a kind of transaction Hearth records.",
	},
	{
		sentinels: []error{domain.ErrTransactionAmountNotPositive},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_AMOUNT",
		message:   "Enter an amount greater than zero. Whether it adds or subtracts comes from the kind.",
	},
	// One message for every wrong-account shape, including an account in
	// another household: separate ones would tell a caller which ids are real
	// elsewhere.
	{
		sentinels: []error{domain.ErrTransactionAccountsInvalid},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_ACCOUNTS",
		message:   "Choose accounts that match this kind of transaction.",
	},
	{
		sentinels: []error{domain.ErrReceivedAmountRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "RECEIVED_AMOUNT_REQUIRED",
		message:   "These accounts are in different currencies. Enter what actually arrived.",
	},
	{
		sentinels: []error{domain.ErrReceivedAmountNotAllowed},
		status:    http.StatusUnprocessableEntity,
		code:      "RECEIVED_AMOUNT_NOT_ALLOWED",
		message:   "Only a transfer records an amount received.",
	},
	{
		sentinels: []error{domain.ErrCategoryKindMismatch},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_CATEGORY",
		message:   "That category does not belong to this kind of transaction.",
	},
	{
		sentinels: []error{domain.ErrUnknownCategoryKind},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_CATEGORY",
		message:   "That category does not belong to this kind of transaction.",
	},
	{
		sentinels: []error{domain.ErrCategoryNameTaken},
		status:    http.StatusConflict,
		code:      "CATEGORY_NAME_TAKEN",
		message:   "A category with that name already exists.",
	},
	{
		sentinels: []error{domain.ErrCategoryNameRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "CATEGORY_NAME_REQUIRED",
		message:   "A category name is required.",
	},
	{
		sentinels: []error{domain.ErrBudgetLineDuplicate},
		status:    http.StatusUnprocessableEntity,
		code:      "DUPLICATE_BUDGET_LINE",
		message:   "Each category can only appear once in a budget.",
	},
	{
		sentinels: []error{domain.ErrBudgetCapNegative},
		status:    http.StatusUnprocessableEntity,
		code:      "NEGATIVE_BUDGET_CAP",
		message:   "A budget cap cannot be negative.",
	},
	{
		sentinels: []error{domain.ErrBudgetIncomeNegative},
		status:    http.StatusUnprocessableEntity,
		code:      "NEGATIVE_BUDGET_INCOME",
		message:   "Expected income cannot be negative.",
	},
	{
		sentinels: []error{domain.ErrBudgetCategoryUnknown},
		status:    http.StatusUnprocessableEntity,
		code:      "UNKNOWN_BUDGET_CATEGORY",
		message:   "That category could not be found.",
	},
	// --- holdings ---------------------------------------------------------
	{
		// The plain case: a collision against a LIVE holding in the same
		// account. holding_handlers.go intercepts this same sentinel before it
		// reaches here when the colliding row is archived, and builds a richer
		// 409 carrying that holding's id so the modal can offer Restore rather
		// than a dead end -- the writeGoalNameConflict precedent.
		sentinels: []error{domain.ErrHoldingNameTaken},
		status:    http.StatusConflict,
		code:      "HOLDING_NAME_TAKEN",
		message:   "This account already has a holding with that name.",
	},
	{
		sentinels: []error{domain.ErrHoldingDateInFuture},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_DATE",
		message:   "That date is in the future.",
	},
	{
		sentinels: []error{domain.ErrHoldingNameRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "HOLDING_NAME_REQUIRED",
		message:   "Give this holding a name.",
	},
	{
		sentinels: []error{domain.ErrHoldingAccountNotInvestment},
		status:    http.StatusUnprocessableEntity,
		code:      "ACCOUNT_NOT_INVESTMENT",
		message:   "Holdings live in an investment account. Choose one, or change this account's type first.",
	},
	{
		sentinels: []error{domain.ErrAccountHasHoldings},
		status:    http.StatusUnprocessableEntity,
		code:      "ACCOUNT_HAS_HOLDINGS",
		message:   "This account holds investments, so its type cannot change. Archive or move them first.",
	},
	{
		sentinels: []error{domain.ErrUnknownPeriodKind},
		status:    http.StatusBadRequest,
		code:      "INVALID_PERIOD_KIND",
		message:   "Ask for a quarter, a half or a year.",
	},
	{
		// 400 rather than 422: this is a query parameter the caller chose, not
		// a value the household typed into a form.
		sentinels: []error{domain.ErrPeriodCountOutOfRange},
		status:    http.StatusBadRequest,
		code:      "INVALID_PERIOD_COUNT",
		message:   "That is more history than this report draws. Ask for between 1 and 12 periods.",
	},
	{
		sentinels: []error{domain.ErrUnknownIncomeKind},
		status:    http.StatusUnprocessableEntity,
		code:      "UNKNOWN_INCOME_KIND",
		message:   "Record this as income or as a fee.",
	},
	{
		sentinels: []error{domain.ErrHoldingIncomeAmountNotPositive},
		status:    http.StatusUnprocessableEntity,
		code:      "INCOME_AMOUNT_NOT_POSITIVE",
		message:   "Enter how much was paid. A fee is entered as a positive amount and comes off the total.",
	},
	{
		sentinels: []error{domain.ErrPrimaryCurrencyHeldByHoldings},
		status:    http.StatusUnprocessableEntity,
		code:      "PRIMARY_CURRENCY_HELD_BY_HOLDINGS",
		message:   "Your currency cannot change while you hold investments: every holding records what it cost in the currency you kept books in at the time, and nothing here can restate that.",
	},
	{
		sentinels: []error{domain.ErrHoldingArchived},
		status:    http.StatusUnprocessableEntity,
		code:      "HOLDING_ARCHIVED",
		message:   "This holding is archived. Restore it before recording anything against it.",
	},
	{
		// Covers both directions: recording a sale bigger than the position,
		// and deleting a purchase a later sale was costed against.
		sentinels: []error{domain.ErrHoldingOversold},
		status:    http.StatusUnprocessableEntity,
		code:      "HOLDING_OVERSOLD",
		message:   "That would sell more than this holding has ever held.",
	},
	{
		sentinels: []error{domain.ErrHoldingEventQuantityNotPositive},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_QUANTITY",
		message:   "Enter a quantity greater than zero.",
	},
	{
		sentinels: []error{domain.ErrInvalidQuantity, domain.ErrQuantityNegative},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_QUANTITY",
		message:   "Enter a quantity as a number, up to nine decimal places.",
	},
	{
		sentinels: []error{domain.ErrUnknownInstrumentKind},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_INSTRUMENT",
		message:   "That is not a kind of investment Hearth records.",
	},
	{
		sentinels: []error{domain.ErrUnknownHoldingEventKind},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_EVENT_KIND",
		message:   "An entry is either a purchase or a sale.",
	},
	{
		sentinels: []error{domain.ErrHoldingPrimaryAmountRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "PRIMARY_AMOUNT_REQUIRED",
		message:   "This holding is in another currency, so Hearth needs the amount in your own as well.",
	},
	{
		sentinels: []error{domain.ErrHoldingPrimaryAmountNotAllowed},
		status:    http.StatusUnprocessableEntity,
		code:      "PRIMARY_AMOUNT_NOT_ALLOWED",
		message:   "This holding is already in your own currency, so it needs only one amount.",
	},
	{
		// The plain case: a name collision against a LIVE goal, no restore
		// hint to offer. goal_handlers.go's writeGoalNameConflict intercepts
		// this same sentinel before it reaches here whenever the colliding
		// row turns out to be archived, and builds a richer 409 (the
		// archived goal's id in details, so the New/Edit modal can offer
		// Restore instead of a dead end) using this case's own message and
		// status as its fallback if that lookup itself fails.
		sentinels: []error{domain.ErrGoalNameTaken},
		status:    http.StatusConflict,
		code:      "GOAL_NAME_TAKEN",
		message:   "A goal with that name already exists.",
	},
	{
		sentinels: []error{domain.ErrGoalNameRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "GOAL_NAME_REQUIRED",
		message:   "A goal name is required.",
	},
	{
		sentinels: []error{domain.ErrGoalTargetNotPositive},
		status:    http.StatusUnprocessableEntity,
		code:      "GOAL_TARGET_NOT_POSITIVE",
		message:   "Enter a target greater than zero.",
	},
	{
		sentinels: []error{domain.ErrGoalPlannedMonthlyNegative},
		status:    http.StatusUnprocessableEntity,
		code:      "GOAL_PLANNED_MONTHLY_NEGATIVE",
		message:   "Planned monthly cannot be negative.",
	},
	{
		sentinels: []error{domain.ErrGoalCurrencyImmutable},
		status:    http.StatusUnprocessableEntity,
		code:      "GOAL_CURRENCY_IMMUTABLE",
		message:   "A goal's currency cannot be changed after it is created.",
	},
	{
		sentinels: []error{domain.ErrGoalArchived},
		status:    http.StatusUnprocessableEntity,
		code:      "GOAL_ARCHIVED",
		message:   "That goal is archived.",
	},
	{
		sentinels: []error{domain.ErrContributionAmountZero},
		status:    http.StatusUnprocessableEntity,
		code:      "CONTRIBUTION_AMOUNT_ZERO",
		message:   "Enter an amount other than zero.",
	},
	{
		sentinels: []error{domain.ErrRolloverMonthOpen},
		status:    http.StatusUnprocessableEntity,
		code:      "ROLLOVER_MONTH_OPEN",
		message:   "Only a closed month can be rolled over.",
	},
	{
		sentinels: []error{domain.ErrRolloverNothingUnspent},
		status:    http.StatusUnprocessableEntity,
		code:      "ROLLOVER_NOTHING_UNSPENT",
		message:   "That month has nothing unspent to roll over.",
	},
	{
		sentinels: []error{domain.ErrRolloverCurrencyMismatch},
		status:    http.StatusUnprocessableEntity,
		code:      "ROLLOVER_CURRENCY_MISMATCH",
		message:   "Only a goal in the household's primary currency can receive a rollover.",
	},
	{
		sentinels: []error{domain.ErrRolloverAlreadyDone},
		status:    http.StatusConflict,
		code:      "ROLLOVER_ALREADY_DONE",
		message:   "That month has already been rolled over.",
	},
	{
		sentinels: []error{domain.ErrUnknownCadence},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_CADENCE",
		message:   "That cadence is not recognised.",
	},
	{
		sentinels: []error{domain.ErrBillNameRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "BILL_NAME_REQUIRED",
		message:   "A bill name is required.",
	},
	{
		sentinels: []error{domain.ErrBillAmountNotPositive},
		status:    http.StatusUnprocessableEntity,
		code:      "BILL_AMOUNT_NOT_POSITIVE",
		message:   "Enter an amount greater than zero.",
	},
	{
		// The plain case: a name collision against a LIVE bill, no restore
		// hint to offer. bill_handlers.go's writeBillWriteError intercepts
		// this same sentinel before it reaches here whenever the colliding
		// row turns out to be archived, and builds a richer 409 (the
		// archived bill's id in details, so the New/Edit modal can offer
		// Restore instead of a dead end) using this case's own message and
		// status as its fallback if that lookup itself fails -- the
		// ErrGoalNameTaken precedent above, applied to bills.
		sentinels: []error{domain.ErrBillNameTaken},
		status:    http.StatusConflict,
		code:      "BILL_NAME_TAKEN",
		message:   "A bill with that name already exists.",
	},
	{
		// bill_handlers.go's handleUpdateBill intercepts this same sentinel
		// before it reaches here whenever the request carries a
		// payFromAccountId, building a 422 that names both currencies
		// (BillService.Update's own doc comment says that message is
		// deliberately the HTTP layer's job, not the service's). This case
		// is the fallback for whenever that lookup itself cannot complete.
		sentinels: []error{domain.ErrBillCurrencyImmutable},
		status:    http.StatusUnprocessableEntity,
		code:      "BILL_CURRENCY_IMMUTABLE",
		message:   "A bill's currency cannot be changed after it is created.",
	},
	// --- Retros (Task 8) --------------------------------------------------
	{
		sentinels: []error{domain.ErrRetroChanged},
		status:    http.StatusConflict,
		code:      "RETRO_CHANGED",
		message:   "Someone else saved this retro while you were editing it. Reload to see their changes.",
	},
	{
		// Deliberately its own code, not RETRO_EXISTS: the two conflicts have
		// different causes and want different copy. RETRO_EXISTS
		// (handleStartRetro's own case, ahead of MapDomainError -- see its
		// comment) is the race of two concurrent Create calls for the SAME
		// free month; this is the calm case of both candidate months already
		// being taken, which the Start-retro button should not even be able
		// to reach (it would not be rendered -- RetrosView.StartMonth would
		// already be nil), so it needs its own message rather than
		// "someone already started it," which would be actively wrong here.
		sentinels: []error{domain.ErrRetroNothingToStart},
		status:    http.StatusConflict,
		code:      "RETRO_NOTHING_TO_START",
		message:   "Both this month and last month already have a retro.",
	},
	{
		sentinels: []error{domain.ErrInvalidMood},
		status:    http.StatusBadRequest,
		code:      "INVALID_MOOD",
		message:   "That is not a mood we can record.",
	},
	{
		sentinels: []error{domain.ErrRetroActionBodyRequired},
		status:    http.StatusBadRequest,
		code:      "RETRO_ACTION_BODY_REQUIRED",
		message:   "Give this action some text before saving it.",
	},
	// --- Vision (Task 9) ----------------------------------------------------
	{
		sentinels: []error{domain.ErrVisionChanged},
		status:    http.StatusConflict,
		code:      "VISION_CHANGED",
		message:   "This vision changed while you were editing it. Reload and try again.",
	},
	{
		sentinels: []error{domain.ErrVisionGoalUnknown},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_GOAL_UNKNOWN",
		message:   "That savings goal is not one of this household's.",
	},
	{
		sentinels: []error{domain.ErrVisionThemeRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_THEME_REQUIRED",
		message:   "Give this year a theme.",
	},
	{
		sentinels: []error{domain.ErrVisionMeasureAmbiguous},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_MEASURE_INVALID",
		message:   "A measure is either a number you keep, or a savings goal — not both.",
	},
	{
		// Deliberately its own code, not VISION_MEASURE_INVALID: that
		// message tells a household they picked BOTH a number and a goal,
		// which is actively wrong here -- switching a measure to "A savings
		// goal" and saving without choosing one means they picked NEITHER.
		// Same reasoning RETRO_NOTHING_TO_START's own comment gives for not
		// reusing RETRO_EXISTS: two different causes want different copy.
		sentinels: []error{domain.ErrVisionMeasureGoalRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_MEASURE_GOAL_REQUIRED",
		message:   "Pick a savings goal for that measure, or switch it back to a number you keep.",
	},
	{
		sentinels: []error{domain.ErrVisionThemeTooLong},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_INVALID",
		message:   "A vision theme can be at most 120 characters.",
	},
	{
		sentinels: []error{domain.ErrVisionDescriptionTooLong},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_INVALID",
		message:   "A vision description can be at most 2,000 characters.",
	},
	{
		// Shared by both a vision's own year and a milestone's (Vision.Validate
		// returns this sentinel for either), so the message names neither
		// specifically.
		sentinels: []error{domain.ErrVisionYearOutOfRange},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_INVALID",
		message:   "Choose a year between 1900 and 2200.",
	},
	{
		sentinels: []error{domain.ErrVisionPillarNameRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_INVALID",
		message:   "Give this pillar a name.",
	},
	{
		sentinels: []error{domain.ErrVisionMeasureLabelRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_INVALID",
		message:   "Give this measure a label.",
	},
	{
		sentinels: []error{domain.ErrVisionMeasureTargetNotPositive},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_INVALID",
		message:   "A measure's target must be greater than zero.",
	},
	{
		sentinels: []error{domain.ErrVisionMeasureCurrentNegative},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_INVALID",
		message:   "A measure's current value cannot be negative.",
	},
	{
		sentinels: []error{domain.ErrVisionMilestoneTitleRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_INVALID",
		message:   "Give this milestone a title.",
	},
	{
		sentinels: []error{domain.ErrVisionTooManyPillars},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_INVALID",
		message:   "A vision can have at most 12 pillars.",
	},
	{
		sentinels: []error{domain.ErrVisionTooManyMeasures},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_INVALID",
		message:   "A pillar can have at most 8 measures.",
	},
	{
		sentinels: []error{domain.ErrVisionTooManyMilestones},
		status:    http.StatusUnprocessableEntity,
		code:      "VISION_INVALID",
		message:   "A vision can have at most 24 milestones.",
	},
	// --- Agreements ---------------------------------------------------------
	// domain.ErrUnknownAgreementProposalKind and
	// domain.ErrUnknownAgreementProposalStatus are deliberately absent from
	// this block. A bad kind in a request body is answered by
	// handleProposeAgreementChange's own ParseAgreementProposalKind call, with
	// 422 AGREEMENT_KIND_INVALID, exactly as parseVisionYear answers a bad year
	// (decision 21). So either sentinel reaching this function came from a
	// database column -- a row no migration allows and no writer here wrote --
	// and the logged 500 below is the right answer to an impossible row, not a
	// 4xx telling a household their request was wrong when it was not.
	{
		sentinels: []error{domain.ErrAgreementsNeedTwoOwners},
		status:    http.StatusConflict,
		code:      "AGREEMENTS_NEED_TWO_OWNERS",
		message:   "Agreements need at least two owners.",
	},
	{
		sentinels: []error{domain.ErrAgreementChanged},
		status:    http.StatusConflict,
		code:      "AGREEMENT_CHANGED",
		message:   "The agreement this was written against has changed, so nothing was signed.",
	},
	{
		sentinels: []error{domain.ErrAgreementNotOpen},
		status:    http.StatusConflict,
		code:      "AGREEMENT_PROPOSAL_RESOLVED",
		message:   "This change was already settled. Reload to see it.",
	},
	{
		sentinels: []error{domain.ErrAgreementSectionNameTaken},
		status:    http.StatusConflict,
		code:      "AGREEMENT_SECTION_NAME_TAKEN",
		message:   "You already have a section with that name.",
	},
	{
		sentinels: []error{domain.ErrAgreementSectionNameRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "AGREEMENT_SECTION_NAME_REQUIRED",
		message:   "Give this section a name.",
	},
	{
		sentinels: []error{domain.ErrAgreementSectionNameTooLong},
		status:    http.StatusUnprocessableEntity,
		code:      "AGREEMENT_SECTION_NAME_TOO_LONG",
		message:   "That section name is too long.",
	},
	{
		sentinels: []error{domain.ErrAgreementProposalShapeInvalid},
		status:    http.StatusUnprocessableEntity,
		code:      "AGREEMENT_PROPOSAL_SHAPE_INVALID",
		message:   "That is not a change we can propose.",
	},
	{
		sentinels: []error{domain.ErrAgreementEditUnchanged},
		status:    http.StatusUnprocessableEntity,
		code:      "AGREEMENT_EDIT_UNCHANGED",
		message:   "This wording is the same as the agreement it changes.",
	},
	{
		sentinels: []error{domain.ErrAgreementBodyRequired},
		status:    http.StatusUnprocessableEntity,
		code:      "AGREEMENT_BODY_REQUIRED",
		message:   "Write the agreement before proposing it.",
	},
	{
		sentinels: []error{domain.ErrAgreementBodyTooLong},
		status:    http.StatusUnprocessableEntity,
		code:      "AGREEMENT_BODY_TOO_LONG",
		message:   "That agreement is too long.",
	},
	// The two note caps get two codes rather than one shared "note too long",
	// because they are different fields on different screens: the proposal's
	// note is in the Propose modal, the park note in the card's Discuss
	// expander, and a 422 that cannot say which field is a 422 the screen
	// cannot place.
	{
		sentinels: []error{domain.ErrAgreementNoteTooLong},
		status:    http.StatusUnprocessableEntity,
		code:      "AGREEMENT_NOTE_TOO_LONG",
		message:   "That note is too long.",
	},
	{
		sentinels: []error{domain.ErrAgreementParkNoteTooLong},
		status:    http.StatusUnprocessableEntity,
		code:      "AGREEMENT_PARK_NOTE_TOO_LONG",
		message:   "That note is too long.",
	},
	// domain.ErrUnknownContributionSource has no case here, deliberately: it
	// means a goal_contributions row holds a source value this code never
	// wrote (ParseContributionSource's own doc comment), which is a real
	// internal failure -- a database column disagreeing with every writer
	// this codebase has -- not a client mistake. It falls through to the
	// generic, logged 500 below like ErrAmountOverflow does above, rather
	// than getting a 4xx case that would tell a caller their request was
	// wrong when it was not.
	{
		sentinels: []error{domain.ErrTelegramChatTaken},
		status:    http.StatusConflict,
		code:      "TELEGRAM_CHAT_TAKEN",
		message:   telegramChatTakenMessage,
	},
	{
		sentinels: []error{domain.ErrTelegramAlreadyLinked},
		status:    http.StatusConflict,
		code:      "TELEGRAM_ALREADY_LINKED",
		message:   telegramAlreadyLinkedMessage,
	},
	{
		sentinels: []error{domain.ErrTelegramLinkNotPending},
		status:    http.StatusConflict,
		code:      "TELEGRAM_LINK_NOT_PENDING",
		message:   "No Telegram chat has opened this link, or it expired. Start again.",
	},
	{
		sentinels: []error{domain.ErrTelegramUnlinkWouldLockOut},
		status:    http.StatusConflict,
		code:      "TELEGRAM_UNLINK_LOCKOUT",
		message:   "Add an email address to this account before disconnecting Telegram.",
	},
	{
		sentinels: []error{domain.ErrTelegramMintsRateLimited},
		status:    http.StatusTooManyRequests,
		code:      "TELEGRAM_LINK_RATE_LIMITED",
		message:   "Too many attempts. Try again in an hour.",
	},
	{
		// Every service that means a genuine, nameable conflict already
		// translates domain.ErrAlreadyExists into its own sentinel before
		// this function ever sees it (e.g. HouseholdService.CreateSpace ->
		// ErrSpaceNameTaken, InviteService.Create -> ErrInviteeAlreadyRegistered
		// above) -- both get their own, more specific case, and are matched
		// first because errors.Is walks in table order. This case is
		// the backstop for the race those specific translations cannot
		// close by themselves: two callers hitting the same unique
		// constraint at once, only one of which had a pre-check to lose.
		// The clearest example is InviteRepository.Accept -- two invites for
		// the same new address accepted concurrently both pass Create's
		// email-not-registered check, and only one of the two CreateUser
		// calls that follow can win the users.email unique index. Falling
		// through to the generic 500 default below would be wrong for that
		// race: it is a real, if rare, conflict a retry can't paper over,
		// not an internal bug, so 409 is the right answer whenever nothing
		// more specific already caught it.
		sentinels: []error{domain.ErrAlreadyExists},
		status:    http.StatusConflict,
		code:      "ALREADY_EXISTS",
		message:   "That already exists.",
	},
}

// logAndWriteInternal logs the real error -- which is never returned to the
// caller -- and answers 500 INTERNAL with the request ID, so a user can quote
// it when reporting the problem.
func logAndWriteInternal(w http.ResponseWriter, r *http.Request, err error) {
	reqID := middleware.GetReqID(r.Context())
	slog.Error("unhandled error", "error", err, "request_id", reqID)
	WriteError(w, http.StatusInternalServerError, "INTERNAL",
		"Something went wrong. Please try again, or quote this reference if it keeps happening.",
		map[string]any{"requestId": reqID})
}

// sessionRevocationWarning is the message a membership mutation's own
// success body carries when usecase.ErrSessionRevocationFailed comes back:
// the mutation already committed, so this never goes through
// MapDomainError's error-envelope shape at all. member_handlers.go checks
// for this sentinel with errors.Is before calling MapDomainError, and builds
// a 200 response with the mutation's normal body plus this warning instead --
// MapDomainError, given only the error, has no way to know what that body
// should contain.
const sessionRevocationWarning = "The change was saved, but we couldn't sign the member out of their other sessions. " +
	"They may still be able to use an old session until it expires."
