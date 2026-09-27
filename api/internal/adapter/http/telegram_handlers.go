package httpadapter

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type telegramStartResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// telegramBindingResponse is what GET, POST .../confirm and DELETE
// /auth/telegram all answer with -- the binding, or its absence. DELETE
// answers Connected: false rather than 204: the panel re-renders from the
// body, and apiFetch throws on an ok response it cannot parse.
type telegramBindingResponse struct {
	Connected    bool       `json:"connected"`
	ChatUsername string     `json:"chatUsername,omitempty"`
	LinkedAt     *time.Time `json:"linkedAt,omitempty"`
}

type telegramLinkStartResponse struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// telegramLinkStatusResponse is what the panel polls GET
// /auth/telegram/link/{id} for. Reason is the refusal sentence, built by
// telegramLinkReasonMessage from the usecase's stable code -- the usecase
// layer holds no user-facing copy, so this is the one place it's written,
// matching errors.go's 409 mapping for the same refusal reached through
// confirm. Empty for every other status.
type telegramLinkStatusResponse struct {
	Status       string `json:"status"`
	ChatUsername string `json:"chatUsername,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// telegramLinkReasonMessage turns Status's stable refusal code into the
// sentence the panel shows, matching errors.go's 409 mapping for the same
// refusal via confirm. An unrecognised code fails closed and answers "":
// with Reason `omitempty` on the wire, the panel's own fallback renders
// instead of a raw code leaking to the screen.
func telegramLinkReasonMessage(code string) string {
	switch code {
	case usecase.TelegramLinkReasonChatTaken:
		return telegramChatTakenMessage
	case usecase.TelegramLinkReasonAlreadyLinked:
		return telegramAlreadyLinkedMessage
	default:
		return ""
	}
}

// handleTelegramStart mints the deep link that carries a sign-in request into
// Telegram. It reads no body and takes no identifier, so it needs no oracle
// defence -- nobody is claiming an identity yet. A nil Deps.Telegram means no
// bot is configured; the route answers 404, same as any unrouted path, so an
// install without Telegram gives away nothing about whether the feature
// exists.
func handleTelegramStart(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Telegram == nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "That endpoint does not exist.", nil)
			return
		}
		link, err := deps.Telegram.StartLink(r.Context())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, telegramStartResponse{URL: link.URL, ExpiresAt: link.ExpiresAt})
	}
}

// handleTelegramBinding answers this user's Telegram binding: connected
// (chat, linkedAt) or not. domain.ErrNotFound from Binding means "no chat
// bound", the ordinary case for most members reaching Settings, not an
// error, so it answers connected: false rather than MapDomainError's 404. A
// nil Deps.TelegramLink means no bot is configured: writeNotFound keeps that
// indistinguishable from a route that doesn't exist.
func handleTelegramBinding(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.TelegramLink == nil {
			writeNotFound(w)
			return
		}
		scope, _ := RequestScope(r)
		binding, err := deps.TelegramLink.Binding(r.Context(), scope.UserID)
		if errors.Is(err, domain.ErrNotFound) {
			WriteJSON(w, http.StatusOK, telegramBindingResponse{Connected: false})
			return
		}
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, telegramBindingResponse{
			Connected: true, ChatUsername: binding.ChatUsername, LinkedAt: &binding.LinkedAt,
		})
	}
}

// handleTelegramLinkStart mints a link nonce for this session's member and
// returns the deep link plus the row id the panel polls Status with.
func handleTelegramLinkStart(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.TelegramLink == nil {
			writeNotFound(w)
			return
		}
		scope, _ := RequestScope(r)
		start, err := deps.TelegramLink.Start(r.Context(), scope.UserID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, telegramLinkStartResponse{ID: start.ID, URL: start.URL, ExpiresAt: start.ExpiresAt})
	}
}

// handleTelegramLinkStatus answers where a link request has got to: waiting,
// pending (naming the redeeming chat), connected, refused (with a reason) or
// expired. {id} is the telegram_link_requests row id, not the nonce -- safe
// to expose because a row owned by someone else answers 404
// (domain.ErrNotFound), never 403, so a row id can't be probed for
// existence -- the rule every route in this API follows.
func handleTelegramLinkStatus(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.TelegramLink == nil {
			writeNotFound(w)
			return
		}
		scope, _ := RequestScope(r)
		status, err := deps.TelegramLink.Status(r.Context(), scope.UserID, chi.URLParam(r, "id"))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, telegramLinkStatusResponse{
			Status: status.Status, ChatUsername: status.ChatUsername,
			Reason: telegramLinkReasonMessage(status.Reason),
		})
	}
}

// handleTelegramLinkConfirm writes the binding for a pending link this
// session's member minted. The deciding click happens inside a session
// that is already authenticated, where a stolen deep link cannot reach --
// the security this feature rests on (ADR 10).
func handleTelegramLinkConfirm(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.TelegramLink == nil {
			writeNotFound(w)
			return
		}
		scope, _ := RequestScope(r)
		binding, err := deps.TelegramLink.Confirm(r.Context(), scope.UserID, chi.URLParam(r, "id"))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, telegramBindingResponse{
			Connected: true, ChatUsername: binding.ChatUsername, LinkedAt: &binding.LinkedAt,
		})
	}
}

// handleTelegramUnlink removes this session's Telegram binding. 200 with the
// resulting state, not 204: the panel re-renders from the body, and
// apiFetch throws on an ok response it cannot parse.
func handleTelegramUnlink(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.TelegramLink == nil {
			writeNotFound(w)
			return
		}
		scope, _ := RequestScope(r)
		if err := deps.TelegramLink.Unlink(r.Context(), scope.UserID); err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, telegramBindingResponse{Connected: false})
	}
}
