package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// AdminOutboxService is the operator's read of the mail this install has
// sent. It is its own service rather than three more methods on AdminService
// for the same reason AdminDirectoryService is: that service is "who is a
// platform admin, feature flags, the audit log", and this one reads a store
// outside the database entirely.
//
// It takes no actor parameter. The /admin guards in the HTTP layer are the
// only gate, as everywhere else in this product.
type AdminOutboxService struct{ outbox MailOutbox }

const (
	// OutboxDefaultLimit is how many messages the list returns when the
	// caller names no limit or an unusable one.
	OutboxDefaultLimit = 50
	// OutboxMaxLimit is the most the list will return. Its own constant
	// rather than the directory's: the two answer different questions, and
	// sharing one would make a change to either move the other.
	OutboxMaxLimit = 200
)

func NewAdminOutboxService(outbox MailOutbox) *AdminOutboxService {
	return &AdminOutboxService{outbox: outbox}
}

// OutboxListing is the list screen's whole answer.
type OutboxListing struct {
	Messages []OutboxMessage
	// Total is what the outbox holds; Truncated says it holds more than
	// this listing carries.
	Total     int
	Truncated bool
}

// OutboxMessageView is one message as the operator sees it: the links pulled
// out of whichever body part had them, and the plain text for context.
//
// There is deliberately no HTML field. Nothing above this service has a use
// for the HTML part, and the surface that would render it is the one this
// design rejected -- see the spec's decision 1. Adding the field back is the
// first step of building that surface by accident.
type OutboxMessageView struct {
	ID      string
	To      string
	Subject string
	SentAt  time.Time
	Text    string
	Links   []string
}

// List returns the newest messages the outbox holds, with the limit clamped.
// An unusable limit becomes the default rather than an error: the operator
// typed a URL, not a form.
func (s *AdminOutboxService) List(ctx context.Context, limit int) (OutboxListing, error) {
	if limit <= 0 {
		limit = OutboxDefaultLimit
	}
	if limit > OutboxMaxLimit {
		limit = OutboxMaxLimit
	}

	page, err := s.outbox.Recent(ctx, limit)
	if err != nil {
		return OutboxListing{}, err
	}
	return OutboxListing{
		Messages:  page.Messages,
		Total:     page.Total,
		Truncated: page.Total > len(page.Messages),
	}, nil
}

// Message returns one message with its links extracted. This is the only
// place domain.ExtractLinks is called, and the only place the HTML part is
// read -- it goes no further.
func (s *AdminOutboxService) Message(ctx context.Context, id string) (OutboxMessageView, error) {
	message, err := s.outbox.Message(ctx, id)
	if err != nil {
		return OutboxMessageView{}, err
	}
	return OutboxMessageView{
		ID:      message.ID,
		To:      message.To,
		Subject: message.Subject,
		SentAt:  message.SentAt,
		Text:    message.Text,
		Links:   domain.ExtractLinks(message.Text, message.HTML),
	}, nil
}
