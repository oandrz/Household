package httpadapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// The invite lobby's owner side: POST /household/invites/{id}/link, which
// is both "get a new link" and "Not them" (task-8-brief.md). Knocking is
// simulated by calling env.deps.Invites.Knock directly -- the same way a
// real knock arrives, through the Telegram bot, is out of this package's
// reach, and the postgres and usecase suites already prove Knock and
// ReplaceToken themselves.

// TestANewInviteLinkReplacesTheOldOneAndTellsTheKnockedChat is the route's
// happy path: a fresh link comes back, the old token stops knocking, and
// the chat that knocked is told (via noopInviteChats -- this test only
// proves the route reaches NewLink and gets a live link back; which chat
// was told what is usecase/invite_test.go's job).
func TestANewInviteLinkReplacesTheOldOneAndTellsTheKnockedChat(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/household/members/invite", map[string]any{
		"name": "Christine", "role": "owner", "channel": "telegram",
		"capabilities": []string{"money", "calendar", "chores", "marriage"},
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create telegram invite: got %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created inviteCreatedBody
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode invite response: %v %s", err, rec.Body.String())
	}
	if !strings.Contains(created.Link, "start=inv_") {
		t.Fatalf("link %q carries no inv_ payload", created.Link)
	}
	rawToken := created.Link[strings.Index(created.Link, "start=inv_")+len("start=inv_"):]

	if _, err := env.deps.Invites.Knock(context.Background(), rawToken, 4242, "jane_t"); err != nil {
		t.Fatalf("Knock: %v", err)
	}

	linkRec := env.authed(t, http.MethodPost, "/api/v1/household/invites/"+created.ID+"/link", nil, session, csrf)
	if linkRec.Code != http.StatusOK {
		t.Fatalf("new link: got %d, want 200: %s", linkRec.Code, linkRec.Body.String())
	}
	var newLink inviteCreatedBody
	if err := json.Unmarshal(linkRec.Body.Bytes(), &newLink); err != nil {
		t.Fatalf("decode new link response: %v %s", err, linkRec.Body.String())
	}
	if newLink.Link == created.Link {
		t.Fatal("the new link is the old link")
	}
	if !strings.Contains(newLink.Link, "?start=inv_") {
		t.Fatalf("new link %q carries no inv_ payload", newLink.Link)
	}

	// The old token is dead -- the same domain.ErrNotFound answer an
	// unknown token gets, so a chat holding a leaked old link learns
	// nothing about why it stopped working.
	if _, err := env.deps.Invites.Knock(context.Background(), rawToken, 5555, "someone"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the old token still knocks: %v", err)
	}
}

// An email invite has no link to replace -- refused with its own message,
// not silently converted (spec decision 9).
func TestNewLinkOnAnEmailInviteIs409(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.mustInviteOwner(t, session, csrf, "Jane", "jane@example.com")
	invite, ok := findPendingInvite(env.pendingInvites(t, session), "jane@example.com")
	if !ok {
		t.Fatal("setup: the invite is not pending")
	}

	rec := env.authed(t, http.MethodPost, "/api/v1/household/invites/"+invite.ID+"/link", nil, session, csrf)
	assertErrorResponse(t, rec, http.StatusConflict, "INVITE_NOT_TELEGRAM")
}

// An id from another household is a 404, never a 403 or a 409 that would
// confirm the invite exists elsewhere (docs/LEARNING.md pattern 24).
func TestNewLinkForAnUnknownInviteIs404(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/household/invites/00000000-0000-0000-0000-000000000000/link", nil, session, csrf)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// Getting a new link changes who can get into the household exactly as
// much as creating or withdrawing one does, so it needs the same three
// guards. A limited member is refused by requireOwner.
func TestALimitedMemberCannotGetANewInviteLink(t *testing.T) {
	env := newTestEnv(t)
	ownerSession, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	rec := env.authed(t, http.MethodPost, "/api/v1/household/members/invite", map[string]any{
		"name": "Christine", "role": "owner", "channel": "telegram",
		"capabilities": []string{"money", "calendar", "chores", "marriage"},
	}, ownerSession, ownerCSRF)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create telegram invite: got %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created inviteCreatedBody
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode invite response: %v %s", err, rec.Body.String())
	}

	limitedSession, limitedCSRF := env.signIn(t, env.limitedEmail, env.limitedPassword)
	forbidden := env.authed(t, http.MethodPost, "/api/v1/household/invites/"+created.ID+"/link", nil, limitedSession, limitedCSRF)
	assertErrorResponse(t, forbidden, http.StatusForbidden, "FORBIDDEN")
}
