package httpadapter

import (
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// The operator's outbound mail: a list and one message. Both are reads inside
// the /admin granted group, so requirePlatformAdmin, auditAdmin, requireCSRF
// and requireAdminGrant apply by construction -- nothing here checks who is
// asking. Every timestamp leaves as RFC 3339 in UTC.
//
// The list carries no body text of any kind, and the detail carries the plain
// text and the links pulled out of it, never the HTML part. A rendered email
// is not what this screen is for; see the spec's decision 1.

// mailpitIDPattern is Mailpit v1.30.5's id shape exactly: 22 characters from
// a 62-character alphanumeric alphabet (internal/shortuuid). Refusing
// anything else before the upstream request is made is this route's "fail
// closed on values you did not construct" -- with two specific teeth, since
// Mailpit reads the literal id "latest" as "the most recent message", and an
// id containing a slash would aim the request at a different endpoint
// entirely.
var mailpitIDPattern = regexp.MustCompile(`^[0-9A-Za-z]{22}$`)

type outboxMessageSummaryDTO struct {
	ID      string    `json:"id"`
	To      string    `json:"to"`
	Subject string    `json:"subject"`
	SentAt  time.Time `json:"sentAt"`
}

type outboxListResponse struct {
	Messages  []outboxMessageSummaryDTO `json:"messages"`
	Total     int                       `json:"total"`
	Truncated bool                      `json:"truncated"`
}

type outboxMessageResponse struct {
	ID      string    `json:"id"`
	To      string    `json:"to"`
	Subject string    `json:"subject"`
	SentAt  time.Time `json:"sentAt"`
	Links   []string  `json:"links"`
	Text    string    `json:"text"`
}

// writeOutboxUnconfigured is the answer when MAILPIT_API_URL is unset. It is
// 503 and not 404 on purpose: a 404 here would hide the route from the one
// person allowed to use it, and the message names the variable because the
// person reading it is the person who can set it.
func writeOutboxUnconfigured(w http.ResponseWriter) {
	WriteError(w, http.StatusServiceUnavailable, "MAIL_INSPECTOR_NOT_CONFIGURED",
		"The message inspector is not configured on this install. Set MAILPIT_API_URL and restart the API.", nil)
}

// handleAdminMail is the list. A limit that fails to parse is 0, which the
// service turns into its default -- the operator typed a URL, not a form.
func handleAdminMail(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.AdminOutbox == nil {
			writeOutboxUnconfigured(w)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		listing, err := deps.AdminOutbox.List(r.Context(), limit)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		body := outboxListResponse{
			Messages:  make([]outboxMessageSummaryDTO, 0, len(listing.Messages)),
			Total:     listing.Total,
			Truncated: listing.Truncated,
		}
		for _, m := range listing.Messages {
			body.Messages = append(body.Messages, outboxMessageSummaryDTO{
				ID: m.ID, To: m.To, Subject: m.Subject, SentAt: m.SentAt.UTC(),
			})
		}
		WriteJSON(w, http.StatusOK, body)
	}
}

// handleAdminMailMessage is the deliberate second click: opening one message
// is its own request and its own audit row, which is what makes seeing a live
// link an act with a record rather than the default state of a screen.
func handleAdminMailMessage(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.AdminOutbox == nil {
			writeOutboxUnconfigured(w)
			return
		}
		id := chi.URLParam(r, "messageID")
		if !mailpitIDPattern.MatchString(id) {
			WriteError(w, http.StatusBadRequest, "INVALID_ID", "That is not a message id.", nil)
			return
		}
		view, err := deps.AdminOutbox.Message(r.Context(), id)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, outboxMessageResponse{
			ID: view.ID, To: view.To, Subject: view.Subject,
			SentAt: view.SentAt.UTC(), Links: view.Links, Text: view.Text,
		})
	}
}
