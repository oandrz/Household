package httpadapter

import (
	"net/http"
	"time"
)

type telegramStartResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
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
