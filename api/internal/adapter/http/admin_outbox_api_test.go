package httpadapter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// stubOutbox is the MailOutbox a configured test env holds. Each test sets
// only the field it cares about.
type stubOutbox struct {
	page    usecase.OutboxPage
	message usecase.OutboxMessage
	err     error
}

func (s *stubOutbox) Recent(context.Context, int) (usecase.OutboxPage, error) {
	if s.err != nil {
		return usecase.OutboxPage{}, s.err
	}
	return s.page, nil
}

func (s *stubOutbox) Message(context.Context, string) (usecase.OutboxMessage, error) {
	if s.err != nil {
		return usecase.OutboxMessage{}, s.err
	}
	return s.message, nil
}

func sampleOutbox() *stubOutbox {
	sent := time.Date(2026, 9, 4, 9, 12, 33, 0, time.UTC)
	return &stubOutbox{
		page: usecase.OutboxPage{
			Total: 9,
			Messages: []usecase.OutboxMessage{{
				ID: "0OQ1sV2mB7hN4kR8xT3wZq", To: "chris@example.com",
				Subject: "Your Hearth sign-in link", SentAt: sent,
			}},
		},
		message: usecase.OutboxMessage{
			ID: "0OQ1sV2mB7hN4kR8xT3wZq", To: "chris@example.com",
			Subject: "Your Hearth sign-in link", SentAt: sent,
			Text: "Open https://oink.mywire.org/sign-in/magic?token=abc123 to sign in.",
			HTML: `<a href="https://never.example/rendered">x</a>`,
		},
	}
}

// The key sets are asserted exactly, the same way the households tests
// assert that no money reaches that screen: here the property is that no
// body text reaches the list, and a field added to the DTO by accident must
// fail here rather than pass through.
func TestAdminMailListsMessagesWithExactlyTheSpecsKeys(t *testing.T) {
	env := newTestEnvWithOutbox(t, sampleOutbox())
	session := grantedAdmin(t, env)

	rec := env.authedGet(t, "/api/v1/admin/mail", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	assertKeys(t, "top level", rec.Body.Bytes(), "messages", "total", "truncated")

	var body struct {
		Messages  []json.RawMessage `json:"messages"`
		Total     int               `json:"total"`
		Truncated bool              `json:"truncated"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(body.Messages))
	}
	assertKeys(t, "message", body.Messages[0], "id", "to", "subject", "sentAt")
	if body.Total != 9 || !body.Truncated {
		t.Fatalf("total = %d, truncated = %v", body.Total, body.Truncated)
	}
}

func TestAdminMailMessageReturnsLinksAndTextButNeverHTML(t *testing.T) {
	env := newTestEnvWithOutbox(t, sampleOutbox())
	session := grantedAdmin(t, env)

	rec := env.authedGet(t, "/api/v1/admin/mail/0OQ1sV2mB7hN4kR8xT3wZq", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	assertKeys(t, "message", rec.Body.Bytes(), "id", "to", "subject", "sentAt", "links", "text")

	var body struct {
		Links []string `json:"links"`
		Text  string   `json:"text"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Links) != 1 || body.Links[0] != "https://oink.mywire.org/sign-in/magic?token=abc123" {
		t.Fatalf("links = %#v", body.Links)
	}
	if body.Text == "" {
		t.Fatal("text is empty")
	}
}

// Unconfigured is not the same event as unreachable, and neither is a 404:
// everyone who reaches these handlers has already proved they are a platform
// admin with a live grant.
func TestAdminMailSaysWhenItIsNotConfigured(t *testing.T) {
	env := newTestEnv(t) // no outbox: Deps.AdminOutbox is nil
	session := grantedAdmin(t, env)

	for _, path := range []string{"/api/v1/admin/mail", "/api/v1/admin/mail/0OQ1sV2mB7hN4kR8xT3wZq"} {
		rec := env.authedGet(t, path, session)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, want 503", path, rec.Code)
		}
		if code := errorCode(t, rec.Body.Bytes()); code != "MAIL_INSPECTOR_NOT_CONFIGURED" {
			t.Fatalf("%s code = %q", path, code)
		}
	}
}

func TestAdminMailSaysWhenTheOutboxCannotBeRead(t *testing.T) {
	env := newTestEnvWithOutbox(t, &stubOutbox{err: usecase.ErrOutboxUnavailable})
	session := grantedAdmin(t, env)

	rec := env.authedGet(t, "/api/v1/admin/mail", session)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if code := errorCode(t, rec.Body.Bytes()); code != "MAIL_UPSTREAM_UNAVAILABLE" {
		t.Fatalf("code = %q", code)
	}
}

func TestAdminMailMessageIsANotFoundWhenMailpitHasDroppedIt(t *testing.T) {
	env := newTestEnvWithOutbox(t, &stubOutbox{err: domain.ErrNotFound})
	session := grantedAdmin(t, env)

	rec := env.authedGet(t, "/api/v1/admin/mail/0OQ1sV2mB7hN4kR8xT3wZq", session)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if code := errorCode(t, rec.Body.Bytes()); code != "NOT_FOUND" {
		t.Fatalf("code = %q, want NOT_FOUND", code)
	}
}

// Fail closed on a value we did not construct. Mailpit ids are exactly 22
// characters of [0-9A-Za-z]; "latest" is Mailpit's own magic id for "the most
// recent message", so a typo must never reach the upstream request.
func TestAdminMailMessageRefusesAnIDThatIsNotMailpitShaped(t *testing.T) {
	env := newTestEnvWithOutbox(t, sampleOutbox())
	session := grantedAdmin(t, env)

	for _, id := range []string{
		"latest",
		"0OQ1sV2mB7hN4kR8xT3wZ",   // 21
		"0OQ1sV2mB7hN4kR8xT3wZqq", // 23
		"0OQ1sV2mB7hN4kR8xT3w-q",  // a character outside the alphabet
	} {
		rec := env.authedGet(t, "/api/v1/admin/mail/"+id, session)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("id %q status = %d, want 400", id, rec.Code)
		}
		if code := errorCode(t, rec.Body.Bytes()); code != "INVALID_ID" {
			t.Fatalf("id %q code = %q", id, code)
		}
	}
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	return envelope.Error.Code
}
