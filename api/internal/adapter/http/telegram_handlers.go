package httpadapter

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

type telegramStartResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// telegramBindingResponse is what GET, POST .../confirm and DELETE
// /auth/telegram all answer with -- the account's Telegram binding, or the
// lack of one. DELETE answers this with Connected: false rather than 204:
// the panel re-renders from the body, and apiFetch throws on an ok response
// it cannot parse.
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
// /auth/telegram/link/{id} for. Reason is the sentence the panel shows for a
// refused link. Empty for every other status, and never populated from a
// database error -- the service chooses it (see errors.go's mapping).
type telegramLinkStatusResponse struct {
	Status       string `json:"status"`
	ChatUsername string `json:"chatUsername,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// handleTelegramStart mints the deep link that carries a sign-in request into
// Telegram. It reads no body and takes no identifier: the person is not
// claiming to be anyone yet, which is why this route needs no oracle defence.
//
// A nil Deps.Telegram means no bot is configured, and the route answers 404 --
// the same answer any unrouted path gets, so an install without Telegram gives
// away nothing about whether the feature exists.
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
// (chat, linkedAt) or not. domain.ErrNotFound from Binding means "this
// member has no chat bound" -- the ordinary case for most members reaching
// Settings, not an error -- so it is answered as connected: false, not
// mapped through MapDomainError's 404.
//
// A nil Deps.TelegramLink means no bot is configured, and the route answers
// 404 -- the same answer any unrouted path gets, so an install without
// Telegram gives away nothing about whether the feature exists.
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
// to hand to the browser because a row belonging to someone else answers
// domain.ErrNotFound (404), not 403, the same rule every other route in this
// API follows so a row id cannot be tested for existence.
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
			Status: status.Status, ChatUsername: status.ChatUsername, Reason: status.Reason,
		})
	}
}

// handleTelegramLinkConfirm writes the binding for a pending link this
// session's member minted, the moment spec decision 1 calls the security of
// this feature: the deciding click happens inside a session that is already
// authenticated, where a stolen deep link cannot reach.
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
