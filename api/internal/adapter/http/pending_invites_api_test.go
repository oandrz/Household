package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Pending invites: docs/superpowers/specs/2026-09-19-hearth-partner-invite-lobby-design.md,
// milestone 1.

type pendingInviteBody struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Role         string    `json:"role"`
	Capabilities []string  `json:"capabilities"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

func (env *testEnv) mustInviteOwner(t *testing.T, session, csrf *http.Cookie, name, email string) {
	t.Helper()
	rec := env.authed(t, http.MethodPost, "/api/v1/household/members/invite", map[string]any{
		"name": name, "email": email, "role": "owner",
		"capabilities": []string{"calendar", "chores", "money", "marriage"},
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite %s: %d %s", email, rec.Code, rec.Body.String())
	}
}

func (env *testEnv) pendingInvites(t *testing.T, session *http.Cookie) []pendingInviteBody {
	t.Helper()
	rec := env.authedGet(t, "/api/v1/household/invites", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("list pending invites: %d %s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) == "null" {
		t.Fatal("an empty pending list must be [], never null")
	}
	var body []pendingInviteBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode pending invites: %v %s", err, rec.Body.String())
	}
	return body
}

func findPendingInvite(invites []pendingInviteBody, email string) (pendingInviteBody, bool) {
	for _, invite := range invites {
		if invite.Email == email {
			return invite, true
		}
	}
	return pendingInviteBody{}, false
}

func TestAnOwnerSeesAPendingInviteAndWithdrawsIt(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	env.mustInviteOwner(t, session, csrf, "Jane", "jane@example.com")

	invite, ok := findPendingInvite(env.pendingInvites(t, session), "jane@example.com")
	if !ok {
		t.Fatal("the invite just sent is not in the pending list")
	}
	if invite.Name != "Jane" || invite.Role != "owner" || !invite.ExpiresAt.After(time.Now()) {
		t.Fatalf("pending invite = %+v", invite)
	}

	rec := env.authed(t, http.MethodDelete, "/api/v1/household/invites/"+invite.ID, nil, session, csrf)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("withdraw: %d %q", rec.Code, rec.Body.String())
	}
	if _, ok := findPendingInvite(env.pendingInvites(t, session), "jane@example.com"); ok {
		t.Fatal("a withdrawn invite is still listed")
	}
	// A second withdraw is a 404: the row is gone, not merely stamped.
	if rec := env.authed(t, http.MethodDelete, "/api/v1/household/invites/"+invite.ID, nil, session, csrf); rec.Code != http.StatusNotFound {
		t.Fatalf("second withdraw: %d %s", rec.Code, rec.Body.String())
	}
}

// An invitee's address is personal data, so the list is owner-only -- the
// same rule that lets only an owner see members' addresses.
func TestThePendingInviteListIsOwnerOnly(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.limitedEmail, env.limitedPassword)

	if rec := env.authedGet(t, "/api/v1/household/invites", session); rec.Code != http.StatusForbidden {
		t.Fatalf("a limited member listing invites: %d %s", rec.Code, rec.Body.String())
	}
}

// Reading shows no secret, so a token may list. Withdrawing changes who may
// join, so a token may not (spec decision 12, the reason behind ADR 7 rule 2).
func TestATokenCanListPendingInvitesButNotWithdrawThem(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.mustInviteOwner(t, session, csrf, "Jane", "jane@example.com")
	tok := env.mustCreateToken(t, session, csrf, "agent")

	if rec := env.bearer(t, http.MethodGet, "/api/v1/household/invites", nil, tok.Token); rec.Code != http.StatusOK {
		t.Fatalf("a token listing invites: %d %s", rec.Code, rec.Body.String())
	}

	invite, ok := findPendingInvite(env.pendingInvites(t, session), "jane@example.com")
	if !ok {
		t.Fatal("setup: the invite is not pending")
	}
	rec := env.bearer(t, http.MethodDelete, "/api/v1/household/invites/"+invite.ID, nil, tok.Token)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "SESSION_REQUIRED") {
		t.Fatalf("a token withdrawing an invite: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := findPendingInvite(env.pendingInvites(t, session), "jane@example.com"); !ok {
		t.Fatal("a refused withdraw still deleted the invite")
	}
}

func TestWithdrawingAnUnknownInviteIs404(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	for _, id := range []string{"00000000-0000-0000-0000-000000000000", "not-a-uuid"} {
		if rec := env.authed(t, http.MethodDelete, "/api/v1/household/invites/"+id, nil, session, csrf); rec.Code != http.StatusNotFound {
			t.Fatalf("withdraw %q: %d %s", id, rec.Code, rec.Body.String())
		}
	}
}
