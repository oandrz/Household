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

// maxRequestBodyBytes bounds every JSON body this API accepts. It is one
// constant used everywhere, rather than a per-handler value, so the limit
// cannot silently drift from one route to another. 1 KiB is generous for
// every request shape in this file: the largest legitimate body (a member
// invite, or a full household update) is a few hundred bytes of field names
// and short strings.
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
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
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

	switch {
	case errors.Is(err, domain.ErrInvalidCredentials):
		WriteError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "That email or password is incorrect.", nil)
	case errors.Is(err, domain.ErrHouseholdLocked):
		WriteError(w, http.StatusLocked, "HOUSEHOLD_LOCKED",
			"This household is temporarily locked after too many failed sign-in attempts.", nil)
	case errors.Is(err, domain.ErrNotFound):
		WriteError(w, http.StatusNotFound, "NOT_FOUND", "That could not be found.", nil)
	case errors.Is(err, domain.ErrForbidden):
		WriteError(w, http.StatusForbidden, "FORBIDDEN", "You do not have permission to do that.", nil)
	case errors.Is(err, domain.ErrLastOwner):
		WriteError(w, http.StatusConflict, "LAST_OWNER", "A household must keep at least one owner.", nil)
	case errors.Is(err, domain.ErrLimitedCannotHoldMarriage),
		errors.Is(err, domain.ErrOwnerMustHoldAllCapabilities),
		errors.Is(err, domain.ErrUnknownCapability):
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_CAPABILITIES",
			"That capability set is not valid for this role.", nil)
	case errors.Is(err, domain.ErrUnknownRole):
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_ROLE", "That role is not recognised.", nil)
	case errors.Is(err, domain.ErrAmountOverflow):
		// Reaching the HTTP layer means a calculation is wrong, not that the
		// caller sent a bad request -- nothing on this API surface accepts a
		// caller-supplied amount that could overflow. Handled like the
		// default branch (logged, generic 500) with its own case only so the
		// log line names the specific cause.
		logAndWriteInternal(w, r, err)
	case errors.Is(err, domain.ErrInvalidMoney):
		// Unlike ErrAmountOverflow above, this is no longer only an internal-
		// arithmetic signal: HouseholdService.Update (Task 15) wraps a
		// caller-supplied currency code's domain.NewMoney failure in this
		// same sentinel (see normalizeCurrency in usecase/household.go), so
		// a typo in PATCH /household's primaryCurrency or secondaryCurrency
		// field reaches here too. That is an ordinary bad request, not a
		// calculation gone wrong, and must not 500.
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_CURRENCY",
			"That currency code is not valid.", nil)
	case errors.Is(err, domain.ErrInviteExpired):
		WriteError(w, http.StatusGone, "INVITE_EXPIRED", "This invite has expired.", nil)
	case errors.Is(err, domain.ErrInviteRequiresEmail):
		WriteError(w, http.StatusUnprocessableEntity, "INVITE_REQUIRES_EMAIL",
			"An invite requires an email address.", nil)
	case errors.Is(err, usecase.ErrPasswordTooShort):
		WriteError(w, http.StatusUnprocessableEntity, "PASSWORD_TOO_SHORT",
			"Password must be at least 12 characters.", nil)
	case errors.Is(err, usecase.ErrPasswordTooLong):
		WriteError(w, http.StatusUnprocessableEntity, "PASSWORD_TOO_LONG",
			"Password must be at most 256 characters.", nil)
	case errors.Is(err, domain.ErrInviteAlreadyAccepted):
		WriteError(w, http.StatusConflict, "INVITE_ALREADY_ACCEPTED", "This invite has already been accepted.", nil)
	case errors.Is(err, domain.ErrTokenExpired):
		WriteError(w, http.StatusGone, "TOKEN_EXPIRED", "This link has expired or has already been used.", nil)
	case errors.Is(err, domain.ErrRateLimited):
		WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests. Try again later.", nil)
	case errors.Is(err, usecase.ErrSpaceNameTaken):
		WriteError(w, http.StatusConflict, "SPACE_NAME_TAKEN", "A space with that name already exists.", nil)
	case errors.Is(err, usecase.ErrSpaceVisibilityNotSupported):
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_VISIBILITY", "That visibility is not supported yet.", nil)
	case errors.Is(err, usecase.ErrSpaceNameRequired):
		WriteError(w, http.StatusUnprocessableEntity, "SPACE_NAME_REQUIRED", "A space name is required.", nil)
	default:
		// domain.ErrAlreadyExists deliberately has no case above and falls
		// through to here: it is a general unique-violation sentinel (see its
		// doc comment in domain/errors.go), and blanket-mapping it to 409
		// would turn an unrelated collision -- e.g. a session token-hash
		// collision during sign-in -- into a caller-visible conflict instead
		// of the transparent retry it actually needs. Every service that
		// means a genuine conflict already translates ErrAlreadyExists into
		// its own sentinel (e.g. HouseholdService.CreateSpace ->
		// ErrSpaceNameTaken) before this function ever sees it; that
		// sentinel gets its own case above instead.
		logAndWriteInternal(w, r, err)
	}
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
