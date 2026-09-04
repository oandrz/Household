package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// MailpitOutbox reads the messages Mailpit has caught. It is the only
// implementation of usecase.MailOutbox, and it is a plain JSON client on
// purpose: everything interesting about a message -- which of its strings
// are links -- is domain.ExtractLinks' job, one layer in.
//
// Two Mailpit endpoints are used and no others:
//
//	GET /api/v1/messages?limit=N   the list, newest first
//	GET /api/v1/message/{id}       one message, both body parts
//
// GET /api/v1/message/{id}/link-check is deliberately NOT used, and a test
// asserts that no other path is ever requested. It issues a real HTTP request
// to every URL it finds in order to report each one's status, and every URL
// in a Hearth email is a live single-use token on a public host.
//
// Reading a message marks it read in Mailpit's own store. That is a write,
// from a panel described as read-only, and it is accepted: the flag is not
// product state and nothing in Hearth reads it. Avoiding it would mean
// fetching the raw source and parsing MIME here.
type MailpitOutbox struct {
	base string
	http *http.Client
}

// NewMailpitOutbox points at Mailpit's HTTP API -- http://mailpit:8025 in
// both Compose stacks.
//
// The timeout is short because Mailpit is a container on the same host: a
// slow answer means something is wrong, not that something is far away, and
// an operator is better served by a prompt 502 than by a page that hangs.
func NewMailpitOutbox(baseURL string) *MailpitOutbox {
	return &MailpitOutbox{
		base: strings.TrimRight(baseURL, "/"),
		http: &http.Client{Timeout: 5 * time.Second},
	}
}

// mailpitAddress mirrors net/mail.Address as Mailpit serialises it.
type mailpitAddress struct {
	Name    string `json:"Name"`
	Address string `json:"Address"`
}

type mailpitSummary struct {
	ID      string           `json:"ID"`
	To      []mailpitAddress `json:"To"`
	Subject string           `json:"Subject"`
	// Created is when Mailpit received the message. The list response has no
	// Date field at all, which is why SentAt comes from a different source on
	// each route -- see Message below.
	Created time.Time `json:"Created"`
}

type mailpitList struct {
	Total    int              `json:"total"`
	Messages []mailpitSummary `json:"messages"`
}

type mailpitMessage struct {
	ID      string           `json:"ID"`
	To      []mailpitAddress `json:"To"`
	Subject string           `json:"Subject"`
	// Date is the message's own header, falling back to the received time.
	// The detail response has no Created field, so this is the only sent-at
	// Mailpit offers here. It can differ from the list's Created by the
	// length of the SMTP hop, which is to a container on the same host and
	// which Hearth's own client stamps at send -- so in this install the two
	// agree, and a one-second difference between the two screens is the hop
	// rather than a bug.
	Date time.Time `json:"Date"`
	Text string    `json:"Text"`
	HTML string    `json:"HTML"`
}

// firstRecipient fails closed. Hearth addresses every message to exactly one
// person (adapter/mail/smtp.go), so an empty list means the assumption this
// mapping rests on has changed -- better a 502 the operator can report than a
// blank cell nobody notices.
func firstRecipient(addresses []mailpitAddress) (string, error) {
	if len(addresses) == 0 || addresses[0].Address == "" {
		return "", fmt.Errorf("%w: a message with no recipient", usecase.ErrOutboxUnavailable)
	}
	return addresses[0].Address, nil
}

// errUpstreamNotFound is get's own signal that the upstream answered 404. It
// is not domain.ErrNotFound: what a 404 MEANS depends on which route asked.
// On the message route it is "Mailpit no longer holds that message"; on the
// list route there is nothing to not-find, and a 404 there means the base URL
// is wrong -- Mailpit supports a configured webroot, so a stray path segment
// in MAILPIT_API_URL produces exactly that. Each caller translates it.
var errUpstreamNotFound = errors.New("mailpit answered 404")

// get performs one upstream request and decodes its body. A 404 becomes
// errUpstreamNotFound for the caller to interpret; every other failure --
// transport, status, body -- becomes usecase.ErrOutboxUnavailable, because
// from a caller's point of view they are the same event: the outbox is there
// and could not be read.
func (m *MailpitOutbox) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.base+path, nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %v", usecase.ErrOutboxUnavailable, err)
	}

	resp, err := m.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", usecase.ErrOutboxUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return errUpstreamNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%w: mailpit answered %d", usecase.ErrOutboxUnavailable, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: decode response: %v", usecase.ErrOutboxUnavailable, err)
	}
	return nil
}

func (m *MailpitOutbox) Recent(ctx context.Context, limit int) (usecase.OutboxPage, error) {
	var list mailpitList
	if err := m.get(ctx, "/api/v1/messages?limit="+strconv.Itoa(limit), &list); err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			// See errUpstreamNotFound: on the list this is a wrong base URL,
			// not a missing message, and it must never reach the screen as an
			// empty list.
			return usecase.OutboxPage{}, fmt.Errorf("%w: mailpit answered 404 for the message list; check MAILPIT_API_URL", usecase.ErrOutboxUnavailable)
		}
		return usecase.OutboxPage{}, err
	}

	page := usecase.OutboxPage{
		Messages: make([]usecase.OutboxMessage, 0, len(list.Messages)),
		Total:    list.Total,
	}
	for _, summary := range list.Messages {
		to, err := firstRecipient(summary.To)
		if err != nil {
			return usecase.OutboxPage{}, err
		}
		// Text and HTML stay zero here, deliberately: Mailpit's summary
		// carries a Snippet of up to 250 characters of body, which for a
		// Hearth email is long enough to contain the whole link.
		page.Messages = append(page.Messages, usecase.OutboxMessage{
			ID:      summary.ID,
			To:      to,
			Subject: summary.Subject,
			SentAt:  summary.Created,
		})
	}
	return page, nil
}

func (m *MailpitOutbox) Message(ctx context.Context, id string) (usecase.OutboxMessage, error) {
	var message mailpitMessage
	path := "/api/v1/message/" + url.PathEscape(id)
	if err := m.get(ctx, path, &message); err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			// Here a 404 is the ordinary case the screen has copy for:
			// Mailpit's store has no volume, so a message can simply be gone.
			return usecase.OutboxMessage{}, domain.ErrNotFound
		}
		return usecase.OutboxMessage{}, err
	}

	to, err := firstRecipient(message.To)
	if err != nil {
		return usecase.OutboxMessage{}, err
	}
	return usecase.OutboxMessage{
		ID:      message.ID,
		To:      to,
		Subject: message.Subject,
		SentAt:  message.Date,
		Text:    message.Text,
		HTML:    message.HTML,
	}, nil
}
