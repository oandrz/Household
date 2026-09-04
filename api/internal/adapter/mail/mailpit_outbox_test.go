package mail_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/mail"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// recordingMailpit is an httptest server that remembers every path it was
// asked for. The paths matter as much as the answers: see
// TestMailpitOutboxNeverCallsLinkCheck.
type recordingMailpit struct {
	mu     sync.Mutex
	paths  []string
	server *httptest.Server
}

func newRecordingMailpit(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *recordingMailpit {
	t.Helper()
	rec := &recordingMailpit{}
	rec.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		// EscapedPath, not Path: Go's server has already decoded Path by the
		// time a handler sees it, so a request for ".../..%2Fmessages"
		// arrives as ".../../messages" there and the escaping test below
		// would fail against correct code. EscapedPath is what actually went
		// over the wire.
		rec.paths = append(rec.paths, r.URL.EscapedPath())
		rec.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(rec.server.Close)
	return rec
}

func (m *recordingMailpit) requestedPaths() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.paths...)
}

// The JSON below is Mailpit v1.30.5's own shape, field for field.
const listJSON = `{
  "total": 9,
  "unread": 0,
  "messages_count": 9,
  "start": 0,
  "messages": [
    {"ID": "0OQ1sV2mB7hN4kR8xT3wZq",
     "To": [{"Name": "", "Address": "chris@example.com"}],
     "Subject": "Your Hearth sign-in link",
     "Created": "2026-09-04T09:12:33.123456Z",
     "Snippet": "Here is your sign-in link: https://oink.mywire.org/sign-in/magic?token=abc123"}
  ]
}`

const messageJSON = `{
  "ID": "0OQ1sV2mB7hN4kR8xT3wZq",
  "To": [{"Name": "", "Address": "chris@example.com"}],
  "Subject": "Your Hearth sign-in link",
  "Date": "2026-09-04T09:12:30Z",
  "Text": "Open https://oink.mywire.org/sign-in/magic?token=abc123 to sign in.",
  "HTML": ""
}`

func TestMailpitOutboxRecentMapsTheListResponse(t *testing.T) {
	server := newRecordingMailpit(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "50" {
			t.Errorf("limit query = %q, want 50", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(listJSON))
	})
	outbox := mail.NewMailpitOutbox(server.server.URL)

	page, err := outbox.Recent(context.Background(), 50)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if page.Total != 9 {
		t.Fatalf("Total = %d, want 9", page.Total)
	}
	if len(page.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(page.Messages))
	}
	got := page.Messages[0]
	if got.ID != "0OQ1sV2mB7hN4kR8xT3wZq" || got.To != "chris@example.com" {
		t.Fatalf("message = %#v", got)
	}
	if got.SentAt.UTC().Format("2006-01-02T15:04:05Z") != "2026-09-04T09:12:33Z" {
		t.Fatalf("SentAt = %v, want Mailpit's Created", got.SentAt)
	}
	// A list must never carry a body, whatever Mailpit offered: the snippet
	// above contains a whole working link.
	if got.Text != "" || got.HTML != "" {
		t.Fatalf("a listed message carried a body: %#v", got)
	}
	if paths := server.requestedPaths(); len(paths) != 1 || paths[0] != "/api/v1/messages" {
		t.Fatalf("requested %v", paths)
	}
}

func TestMailpitOutboxMessageMapsTheDetailResponse(t *testing.T) {
	server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(messageJSON))
	})
	outbox := mail.NewMailpitOutbox(server.server.URL)

	message, err := outbox.Message(context.Background(), "0OQ1sV2mB7hN4kR8xT3wZq")
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if message.Text == "" {
		t.Fatal("Text is empty, want the body")
	}
	if message.To != "chris@example.com" {
		t.Fatalf("To = %q", message.To)
	}
	if paths := server.requestedPaths(); len(paths) != 1 ||
		paths[0] != "/api/v1/message/0OQ1sV2mB7hN4kR8xT3wZq" {
		t.Fatalf("requested %v", paths)
	}
}

// Decision 2 of the spec, held by a test rather than by a comment. Mailpit's
// link-check endpoint issues a real HTTP request to every URL in the body,
// and every URL in a Hearth email is a live single-use token on a public
// host. This is the kind of rule that survives review and dies in a later
// refactor unless something fails when it is broken.
func TestMailpitOutboxNeverCallsLinkCheck(t *testing.T) {
	server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(messageJSON))
	})
	outbox := mail.NewMailpitOutbox(server.server.URL)

	if _, err := outbox.Message(context.Background(), "0OQ1sV2mB7hN4kR8xT3wZq"); err != nil {
		t.Fatalf("Message: %v", err)
	}
	for _, path := range server.requestedPaths() {
		if path != "/api/v1/message/0OQ1sV2mB7hN4kR8xT3wZq" {
			t.Fatalf("requested an unexpected upstream path %q", path)
		}
	}
}

func TestMailpitOutboxMapsAMissingMessageToNotFound(t *testing.T) {
	server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	outbox := mail.NewMailpitOutbox(server.server.URL)

	_, err := outbox.Message(context.Background(), "0OQ1sV2mB7hN4kR8xT3wZq")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v, want domain.ErrNotFound", err)
	}
}

func TestMailpitOutboxMapsEveryOtherFailureToUnavailable(t *testing.T) {
	t.Run("a non-2xx that is not a 404", func(t *testing.T) {
		server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		outbox := mail.NewMailpitOutbox(server.server.URL)
		if _, err := outbox.Recent(context.Background(), 10); !errors.Is(err, usecase.ErrOutboxUnavailable) {
			t.Fatalf("error = %v, want ErrOutboxUnavailable", err)
		}
	})

	t.Run("a body that is not JSON", func(t *testing.T) {
		server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>not json</html>"))
		})
		outbox := mail.NewMailpitOutbox(server.server.URL)
		if _, err := outbox.Recent(context.Background(), 10); !errors.Is(err, usecase.ErrOutboxUnavailable) {
			t.Fatalf("error = %v, want ErrOutboxUnavailable", err)
		}
	})

	t.Run("a message with no recipient at all", func(t *testing.T) {
		server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"ID":"0OQ1sV2mB7hN4kR8xT3wZq","To":[],"Subject":"x","Date":"2026-09-04T09:12:30Z","Text":"y","HTML":""}`))
		})
		outbox := mail.NewMailpitOutbox(server.server.URL)
		if _, err := outbox.Message(context.Background(), "0OQ1sV2mB7hN4kR8xT3wZq"); !errors.Is(err, usecase.ErrOutboxUnavailable) {
			t.Fatalf("error = %v, want ErrOutboxUnavailable", err)
		}
	})

	// A list has nothing to not-find. Mailpit supports a configured webroot,
	// so a MAILPIT_API_URL carrying a stray path segment makes
	// /api/v1/messages answer 404 -- and mapping that to ErrNotFound would
	// reach the screen as an empty list under a line that says messages do
	// not last, which is both wrong and convincing.
	t.Run("a 404 on the list is unavailable, not not-found", func(t *testing.T) {
		server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		outbox := mail.NewMailpitOutbox(server.server.URL)
		if _, err := outbox.Recent(context.Background(), 10); !errors.Is(err, usecase.ErrOutboxUnavailable) {
			t.Fatalf("error = %v, want ErrOutboxUnavailable", err)
		}
	})

	t.Run("nothing listening", func(t *testing.T) {
		// A port nothing is bound to: the transport itself fails.
		outbox := mail.NewMailpitOutbox("http://127.0.0.1:1")
		if _, err := outbox.Recent(context.Background(), 10); !errors.Is(err, usecase.ErrOutboxUnavailable) {
			t.Fatalf("error = %v, want ErrOutboxUnavailable", err)
		}
	})
}

// The id reaches the adapter already validated by the handler, but the
// adapter escapes it anyway: the check and the URL construction live in
// different files, and only one of them is in front of someone making a
// change.
func TestMailpitOutboxEscapesTheMessageID(t *testing.T) {
	server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	outbox := mail.NewMailpitOutbox(server.server.URL)

	_, _ = outbox.Message(context.Background(), "../messages")

	paths := server.requestedPaths()
	if len(paths) != 1 || paths[0] != "/api/v1/message/..%2Fmessages" {
		t.Fatalf("requested %v, want the escaped segment", paths)
	}
}
