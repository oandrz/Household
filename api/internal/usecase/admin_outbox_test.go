package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// fakeOutbox is the in-memory double every test here runs against. It
// records the limit it was asked for, which is how the clamping tests
// observe what the service decided.
type fakeOutbox struct {
	page       usecase.OutboxPage
	message    usecase.OutboxMessage
	err        error
	gotLimit   int
	gotMessage string
}

func (f *fakeOutbox) Recent(_ context.Context, limit int) (usecase.OutboxPage, error) {
	f.gotLimit = limit
	if f.err != nil {
		return usecase.OutboxPage{}, f.err
	}
	return f.page, nil
}

func (f *fakeOutbox) Message(_ context.Context, id string) (usecase.OutboxMessage, error) {
	f.gotMessage = id
	if f.err != nil {
		return usecase.OutboxMessage{}, f.err
	}
	return f.message, nil
}

func TestOutboxListClampsTheLimitAtBothEnds(t *testing.T) {
	for _, tt := range []struct {
		name string
		ask  int
		want int
	}{
		{"zero means the default", 0, usecase.OutboxDefaultLimit},
		{"negative means the default", -3, usecase.OutboxDefaultLimit},
		{"above the maximum is capped", usecase.OutboxMaxLimit + 1, usecase.OutboxMaxLimit},
		{"a usable number is honoured", 7, 7},
	} {
		t.Run(tt.name, func(t *testing.T) {
			outbox := &fakeOutbox{}
			svc := usecase.NewAdminOutboxService(outbox)
			if _, err := svc.List(context.Background(), tt.ask); err != nil {
				t.Fatalf("List: %v", err)
			}
			if outbox.gotLimit != tt.want {
				t.Fatalf("limit asked of the outbox = %d, want %d", outbox.gotLimit, tt.want)
			}
		})
	}
}

func TestOutboxListReportsTruncatedWhenTheOutboxHoldsMore(t *testing.T) {
	outbox := &fakeOutbox{page: usecase.OutboxPage{
		Messages: []usecase.OutboxMessage{{ID: "a"}, {ID: "b"}},
		Total:    9,
	}}
	svc := usecase.NewAdminOutboxService(outbox)

	listing, err := svc.List(context.Background(), 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !listing.Truncated {
		t.Fatal("Truncated = false, want true: 9 held, 2 returned")
	}
	if listing.Total != 9 {
		t.Fatalf("Total = %d, want 9", listing.Total)
	}
}

func TestOutboxListIsNotTruncatedWhenEverythingFits(t *testing.T) {
	outbox := &fakeOutbox{page: usecase.OutboxPage{
		Messages: []usecase.OutboxMessage{{ID: "a"}, {ID: "b"}},
		Total:    2,
	}}
	svc := usecase.NewAdminOutboxService(outbox)

	listing, err := svc.List(context.Background(), 50)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Truncated {
		t.Fatal("Truncated = true, want false: 2 held, 2 returned")
	}
}

// The view type is where the HTML body stops. This test does not prove that
// on its own -- nothing here would fail if OutboxMessageView grew an HTML
// field, and claiming otherwise would be exactly the false confidence
// docs/LEARNING.md pattern 2 is about. What it does prove is that the links
// are extracted from the body rather than taken from anything the outbox
// handed over ready made. The field's absence is held one layer out, by Task
// 5's exact-key assertion and Task 6's .strict() schema.
func TestOutboxMessageReturnsExtractedLinksAndTheTextBody(t *testing.T) {
	outbox := &fakeOutbox{message: usecase.OutboxMessage{
		ID:      "0OQ1sV2mB7hN4kR8xT3wZq",
		To:      "chris@example.com",
		Subject: "Your Hearth sign-in link",
		SentAt:  time.Date(2026, 9, 4, 9, 12, 33, 0, time.UTC),
		Text:    "Open https://oink.mywire.org/sign-in/magic?token=abc123 to sign in.",
		HTML:    `<a href="https://never.example/rendered">x</a>`,
	}}
	svc := usecase.NewAdminOutboxService(outbox)

	view, err := svc.Message(context.Background(), "0OQ1sV2mB7hN4kR8xT3wZq")
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if outbox.gotMessage != "0OQ1sV2mB7hN4kR8xT3wZq" {
		t.Fatalf("id asked of the outbox = %q", outbox.gotMessage)
	}
	if len(view.Links) != 1 || view.Links[0] != "https://oink.mywire.org/sign-in/magic?token=abc123" {
		t.Fatalf("Links = %#v", view.Links)
	}
	if view.Text != outbox.message.Text {
		t.Fatalf("Text = %q, want the body verbatim", view.Text)
	}
	if view.To != "chris@example.com" || view.Subject != "Your Hearth sign-in link" {
		t.Fatalf("view = %#v", view)
	}
}

// A text part exists on every Hearth message, so the HTML part must never be
// the source of the links a screen shows -- if it were, the rendered-HTML
// surface this design rejected would be one field away.
func TestOutboxMessageIgnoresTheHTMLPartWhenThereIsText(t *testing.T) {
	outbox := &fakeOutbox{message: usecase.OutboxMessage{
		Text: "Open https://text.example/1",
		HTML: `<a href="https://html.example/2">x</a>`,
	}}
	svc := usecase.NewAdminOutboxService(outbox)

	view, err := svc.Message(context.Background(), "id")
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if len(view.Links) != 1 || view.Links[0] != "https://text.example/1" {
		t.Fatalf("Links = %#v, want the text part's URL only", view.Links)
	}
}

// Both failures travel unchanged: the HTTP layer answers 502 for one and 404
// for the other, and a service that flattened them into a single error would
// make those two answers impossible to tell apart.
func TestOutboxPassesTheOutboxsOwnFailuresThrough(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"unavailable", usecase.ErrOutboxUnavailable},
		{"not found", domain.ErrNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := usecase.NewAdminOutboxService(&fakeOutbox{err: tt.err})

			if _, err := svc.List(context.Background(), 0); !errors.Is(err, tt.err) {
				t.Fatalf("List error = %v, want %v", err, tt.err)
			}
			if _, err := svc.Message(context.Background(), "id"); !errors.Is(err, tt.err) {
				t.Fatalf("Message error = %v, want %v", err, tt.err)
			}
		})
	}
}
