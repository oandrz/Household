package httpadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
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

// mustInviteOwner exercises the email channel, which this task hides behind
// FlagEmailInvites -- every caller of this helper is testing the pending-
// invite mechanics (list, withdraw, expiry), not the flag itself, so the
// flag is turned on here rather than in each of those tests.
func (env *testEnv) mustInviteOwner(t *testing.T, session, csrf *http.Cookie, name, email string) {
	t.Helper()
	if err := env.featureFlags.SetGlobal(context.Background(), string(domain.FlagEmailInvites), true, ""); err != nil {
		t.Fatalf("enable email invites: %v", err)
	}
	rec := env.authed(t, http.MethodPost, "/api/v1/household/members/invite", map[string]any{
		"name": name, "email": email, "role": "owner", "channel": "email",
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

// Withdrawing changes who may join the household, so it is an owner's call.
// The limited member here has everything else a withdraw needs -- a real
// cookie session, a valid CSRF token, the id of a real invite in their own
// household -- so requireOwner is the only thing left to refuse it.
func TestALimitedMemberCannotWithdrawAnInvite(t *testing.T) {
	env := newTestEnv(t)
	ownerSession, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.mustInviteOwner(t, ownerSession, ownerCSRF, "Jane", "jane@example.com")
	invite, ok := findPendingInvite(env.pendingInvites(t, ownerSession), "jane@example.com")
	if !ok {
		t.Fatal("setup: the invite is not pending")
	}

	limitedSession, limitedCSRF := env.signIn(t, env.limitedEmail, env.limitedPassword)
	rec := env.authed(t, http.MethodDelete, "/api/v1/household/invites/"+invite.ID, nil, limitedSession, limitedCSRF)
	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")

	if _, ok := findPendingInvite(env.pendingInvites(t, ownerSession), "jane@example.com"); !ok {
		t.Fatal("a refused withdraw still deleted the invite")
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

// An accepted invite is history, not a mistake: withdrawing it must refuse
// with 409, and the row must survive so nothing else has to notice it was
// asked to disappear.
func TestWithdrawingAnAcceptedInviteIs409(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	env.mustInviteOwner(t, session, csrf, "Jane", "jane@example.com")
	invite, ok := findPendingInvite(env.pendingInvites(t, session), "jane@example.com")
	if !ok {
		t.Fatal("setup: the invite is not pending")
	}

	// The test mailer drops the invite mail rather than delivering it, so
	// there is no token here to accept the invite through the public API --
	// accepting it can only be simulated by writing the row directly.
	tag, err := env.db.Pool().Exec(context.Background(),
		`UPDATE invites SET accepted_at = now() WHERE id = $1`, invite.ID)
	if err != nil {
		t.Fatalf("mark invite accepted: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("mark invite accepted touched %d rows, want 1", tag.RowsAffected())
	}

	rec := env.authed(t, http.MethodDelete, "/api/v1/household/invites/"+invite.ID, nil, session, csrf)
	if rec.Code != http.StatusConflict {
		t.Fatalf("withdraw an accepted invite: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec).Error.Code; got != "INVITE_ALREADY_ACCEPTED" {
		t.Fatalf("withdraw an accepted invite: error code = %q, want INVITE_ALREADY_ACCEPTED", got)
	}

	var count int
	if err := env.db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM invites WHERE id = $1`, invite.ID).Scan(&count); err != nil {
		t.Fatalf("count invite rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("a refused withdraw left %d rows for the accepted invite, want 1", count)
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

// A personal API token is a headless credential. It must not be able to
// change who can get into the household: minting a co-owner, demoting the
// other owner, or removing them are all browser-session actions (spec
// decision 12, the reason behind ADR 7 rule 2). Milestone 1 put this guard
// on withdraw only -- see TestATokenCanListPendingInvitesButNotWithdrawThem
// above; this is its sibling for the other three routes that manage
// household membership.
func TestATokenCannotChangeWhoIsInTheHousehold(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	tok := env.mustCreateToken(t, session, csrf, "agent")

	cases := []struct {
		name   string
		method string
		path   string
		body   map[string]any
	}{
		{"invite", http.MethodPost, "/api/v1/household/members/invite", map[string]any{
			"name": "Jane", "email": "jane@example.com", "role": "owner",
			"capabilities": []string{"calendar", "chores", "money", "marriage"},
		}},
		// The mint-a-co-owner attack itself: promote the seeded limited
		// member to owner with a full capability set, exactly what an
		// owner's own PATCH would need to succeed.
		{"update member", http.MethodPatch, "/api/v1/household/members/" + env.limitedMembership,
			map[string]any{"role": "owner", "capabilities": []string{"calendar", "chores", "money", "marriage"}}},
		{"remove member", http.MethodDelete, "/api/v1/household/members/" + env.limitedMembership, nil},
		// A fake id is enough: requireCookieSession sits before the handler
		// ever looks at the id, so a token is refused before it could learn
		// whether that invite exists at all.
		{"new link", http.MethodPost, "/api/v1/household/invites/00000000-0000-0000-0000-000000000000/link", nil},
		// Admit -- Let in -- is the same rule for the same reason: a leaked
		// API token must not be able to seat a new member either (spec
		// decision 12).
		{"admit", http.MethodPost, "/api/v1/household/invites/00000000-0000-0000-0000-000000000000/admit", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.bearer(t, tc.method, tc.path, tc.body, tok.Token)
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "SESSION_REQUIRED") {
				t.Fatalf("token reached %s %s: got %d %s, want 403 SESSION_REQUIRED", tc.method, tc.path, rec.Code, rec.Body.String())
			}
		})
	}

	// Every case above must be a true refusal, not a guard that answers
	// wrong but still lets the write through: nothing was invited, the
	// limited member is still there, and still limited.
	if _, ok := findPendingInvite(env.pendingInvites(t, session), "jane@example.com"); ok {
		t.Fatal("the invite case was refused with 403 but the invite was created anyway")
	}
	found := false
	for _, m := range env.getMembers(t, session) {
		if m.ID != env.limitedMembership {
			continue
		}
		found = true
		if m.Role != "limited" {
			t.Fatalf("the update-member case was refused with 403 but the role became %q anyway", m.Role)
		}
	}
	if !found {
		t.Fatal("the remove-member case was refused with 403 but the member is gone anyway")
	}
}

// --- Task 4: the email_invites flag, refused at the edge as well as the UI ---

// Email invites are hidden while mail cannot leave the box (ADR 3). The
// flag is not only a UI affordance: hearthctl and any crafted request reach
// the same route, and an invite nobody can deliver is worse than a refusal
// the owner can read (spec decision 10).
func TestEmailInviteIsRefusedWhileTheFlagIsOff(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/household/members/invite", map[string]any{
		"name": "Jane", "email": "jane@example.com", "role": "owner", "channel": "email",
		"capabilities": []string{"money", "calendar", "chores", "marriage"},
	}, session, csrf)
	assertErrorResponse(t, rec, http.StatusConflict, "EMAIL_INVITES_DISABLED")
}

// A channel this build does not define is refused before anything is
// written, and so is an absent one: the spec asks for a default that
// refuses rather than one that guesses.
func TestInviteChannelMustBeNamedExplicitly(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	cases := map[string]map[string]any{
		"absent": {
			"name": "Jane", "role": "owner",
			"capabilities": []string{"money", "calendar", "chores", "marriage"},
		},
		"empty": {
			"name": "Jane", "role": "owner", "channel": "",
			"capabilities": []string{"money", "calendar", "chores", "marriage"},
		},
		"unknown": {
			"name": "Jane", "role": "owner", "channel": "carrier_pigeon",
			"capabilities": []string{"money", "calendar", "chores", "marriage"},
		},
		"profile for an owner": {
			"name": "Jane", "role": "owner", "channel": "profile",
			"capabilities": []string{"money", "calendar", "chores", "marriage"},
		},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, "/api/v1/household/members/invite", body, session, csrf)
			if rec.Code == http.StatusCreated {
				t.Fatalf("channel %s was accepted; it must be refused", name)
			}
		})
	}
}

// The kid profile is unchanged by this milestone: a limited member with no
// sign-in, created directly, with no invite row and no link. It says
// "profile" out loud rather than being inferred from an absent field,
// because inference is how an invite goes somewhere nobody meant.
func TestAProfileOnlyMemberIsStillCreatedDirectly(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/household/members/invite", map[string]any{
		"name": "Kayla", "role": "limited", "channel": "profile",
		"capabilities": []string{"calendar", "chores"},
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201: %s", rec.Code, rec.Body.String())
	}
	// No invite row: the member exists already, so there is nothing pending.
	list := env.authedGet(t, "/api/v1/household/invites", session)
	if strings.Contains(list.Body.String(), "Kayla") {
		t.Fatal("a kid profile wrote an invite row")
	}
	// Counted, not merely found: the profile arm has to answer and return,
	// and an arm that fell through to the create call below the switch
	// would produce two Kaylas that a "contains" assertion would pass.
	members := env.authedGet(t, "/api/v1/household/members", session)
	if got := strings.Count(members.Body.String(), `"Kayla"`); got != 1 {
		t.Fatalf("the members list names Kayla %d times, want exactly 1", got)
	}
}

// With the flag on, the email path is exactly what it was before this
// milestone: the flag hides a channel, it does not change one.
func TestEmailInviteWorksWhenTheOperatorTurnsTheFlagOn(t *testing.T) {
	env := newTestEnv(t)
	if err := env.featureFlags.SetGlobal(context.Background(), string(domain.FlagEmailInvites), true, ""); err != nil {
		t.Fatalf("enable email invites: %v", err)
	}
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/household/members/invite", map[string]any{
		"name": "Jane", "email": "jane@example.com", "role": "owner", "channel": "email",
		"capabilities": []string{"money", "calendar", "chores", "marriage"},
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("email invite with the flag on: got %d, want 201", rec.Code)
	}
	// Counted, not merely present: before this assertion, the test checked
	// only rec.Code == 201, which a switch arm that fell through to a
	// second Create call (the shape TestEmailInviteWithNoAddressIsRefused's
	// mutation check pins the email arm against) would also have passed
	// while writing Jane's invite twice.
	pending := env.pendingInvites(t, session)
	count := 0
	for _, invite := range pending {
		if invite.Email == "jane@example.com" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("jane@example.com appears %d times in the pending list, want exactly 1", count)
	}
}

// --- Task 5: the telegram channel, and the email arm's empty-address loophole ---

// A caller that explicitly names the email channel but sends no address
// must be refused, not silently handed to Create -- whose own empty-email
// branch exists for the profile arm's kid case and would otherwise create a
// profile-only member with no invite row, no token and no mail for a
// request that asked for a real invite.
func TestEmailInviteWithNoAddressIsRefused(t *testing.T) {
	env := newTestEnv(t)
	if err := env.featureFlags.SetGlobal(context.Background(), string(domain.FlagEmailInvites), true, ""); err != nil {
		t.Fatalf("enable email invites: %v", err)
	}
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/household/members/invite", map[string]any{
		"name": "Jane", "email": "", "role": "limited", "channel": "email",
		"capabilities": []string{"calendar", "chores"},
	}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVITE_REQUIRES_EMAIL")

	// The loophole this closes wrote a profile-only member with the empty
	// email arm's shape (Create's role != limited guard means a "limited"
	// role is exactly the case that would otherwise have quietly
	// succeeded) -- so the strongest proof of the fix is that nobody named
	// Jane exists afterward.
	members := env.authedGet(t, "/api/v1/household/members", session)
	if strings.Contains(members.Body.String(), `"Jane"`) {
		t.Fatal("a refused email invite with no address created a member anyway")
	}
}

type inviteCreatedBody struct {
	ID        string    `json:"id"`
	Link      string    `json:"link"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// The owner picks Telegram and gets back a one-time t.me deep link -- the
// point of milestone 2's task 5. The list route shows the invite exists but
// never repeats the link: the raw token behind it is never stored, so
// there is nothing left to show a second time.
func TestCreatingATelegramInviteReturnsTheLinkOnce(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/household/members/invite", map[string]any{
		"name": "Christine", "role": "owner", "channel": "telegram",
		"capabilities": []string{"money", "calendar", "chores", "marriage"},
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var body inviteCreatedBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode invite response: %v %s", err, rec.Body.String())
	}
	if !strings.Contains(body.Link, "?start=inv_") {
		t.Fatalf("link %q carries no inv_ payload", body.Link)
	}
	// The exact 47-character payload -- "inv_" plus NewToken's 43
	// base64url characters -- is what keeps this under Telegram's
	// 64-character /start limit. This is the one test in the suite that
	// runs CreateTelegram over the real crypto.TokenGenerator rather than
	// a fixture double, so it is where that invariant is actually pinned.
	payload := body.Link[strings.Index(body.Link, "start=")+len("start="):]
	if len(payload) != 47 {
		t.Fatalf("payload is %d characters; Telegram's start limit is 64 and the spec budgets 47", len(payload))
	}
	if body.ID == "" {
		t.Fatal("a telegram invite response carried no id")
	}

	// The list route shows the invite but never the link again.
	list := env.authedGet(t, "/api/v1/household/invites", session)
	if strings.Contains(list.Body.String(), "t.me") {
		t.Fatal("the pending list leaked the deep link")
	}
	if !strings.Contains(list.Body.String(), `"channel":"telegram"`) {
		t.Fatalf("the pending list does not report the channel: %s", list.Body.String())
	}
}

// Telegram invites are hidden while the telegram_sign_in flag is off, the
// same edge-enforced rule TestEmailInviteIsRefusedWhileTheFlagIsOff pins for
// the email channel (spec decision 10).
func TestTelegramInviteIsRefusedWhileTheFlagIsOff(t *testing.T) {
	env := newTestEnv(t)
	if err := env.featureFlags.SetGlobal(context.Background(), string(domain.FlagTelegramSignIn), false, ""); err != nil {
		t.Fatalf("disable telegram sign-in: %v", err)
	}
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/household/members/invite", map[string]any{
		"name": "Christine", "role": "owner", "channel": "telegram",
		"capabilities": []string{"money", "calendar", "chores", "marriage"},
	}, session, csrf)
	assertErrorResponse(t, rec, http.StatusConflict, "TELEGRAM_INVITES_UNAVAILABLE")
}

// The service test (TestTheWebFormCannotAcceptATelegramInvite) proves the
// rule; this proves the route reports it the way a stranger's request would
// be reported -- 404, not the 410 an expired invite gets or a 500 -- so
// nothing about the response tells a caller holding a real Telegram token
// that it differs from one that was never issued (spec decision 7).
func TestPublicInviteRoutesTreatATelegramTokenAsUnknown(t *testing.T) {
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
	// Guard the slice below: a miss on Index would return -1, and
	// -1+len("start=inv_") still slices a plausible-looking (wrong) offset
	// rather than failing loudly -- so this test could 404 for the wrong
	// reason and still look like it proved the rule.
	if !strings.Contains(created.Link, "start=inv_") {
		t.Fatalf("link %q carries no inv_ payload", created.Link)
	}
	rawToken := created.Link[strings.Index(created.Link, "start=inv_")+len("start=inv_"):]

	preview := env.do(http.MethodGet, "/api/v1/invites/"+rawToken, nil)
	if preview.Code != http.StatusNotFound {
		t.Fatalf("preview: got %d, want 404: %s", preview.Code, preview.Body.String())
	}
	accept := env.do(http.MethodPost, "/api/v1/invites/"+rawToken+"/accept", map[string]any{
		"password": "a-long-enough-password", "displayName": "Christine",
	})
	if accept.Code != http.StatusNotFound {
		t.Fatalf("accept: got %d, want 404: %s", accept.Code, accept.Body.String())
	}
}

// --- Task 9: Admit -- Let in --------------------------------------------

// admitBody decodes POST .../admit's response.
type admitBody struct {
	Member struct {
		ID           string   `json:"id"`
		Name         string   `json:"name"`
		Role         string   `json:"role"`
		Capabilities []string `json:"capabilities"`
	} `json:"member"`
	SignInSent bool `json:"signInSent"`
}

// mustCreateAndKnockTelegramInvite creates a Telegram invite through the
// public route (exactly as an owner would) and records a knock on it
// through the usecase layer directly -- the bot side of a knock has no
// HTTP route of its own; env.deps.Invites is the same *usecase.InviteService
// the router itself was built from.
func (env *testEnv) mustCreateAndKnockTelegramInvite(t *testing.T, session, csrf *http.Cookie, chatID int64, username string) inviteCreatedBody {
	t.Helper()
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
	if _, err := env.deps.Invites.Knock(context.Background(), rawToken, chatID, username); err != nil {
		t.Fatalf("Knock: %v", err)
	}
	return created
}

// The whole point of the milestone: the owner clicks Let in on a knocked
// invite and a real member appears in the roster, with the sign-in link
// reported sent (noopInviteChats never fails).
func TestOwnerAdmitsAKnockedTelegramInvite(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	created := env.mustCreateAndKnockTelegramInvite(t, session, csrf, 4242, "christine_t")

	rec := env.authed(t, http.MethodPost, "/api/v1/household/invites/"+created.ID+"/admit", nil, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("admit: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body admitBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode admit response: %v %s", err, rec.Body.String())
	}
	if body.Member.Name != "Christine" || body.Member.Role != "owner" {
		t.Fatalf("member = %+v", body.Member)
	}
	if !body.SignInSent {
		t.Fatal("signInSent is false; the test env's chats double never fails a send")
	}

	found := false
	for _, m := range env.getMembers(t, session) {
		if m.ID == body.Member.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("admit answered 200 but the new member is not in the household")
	}

	// The invite is spent: a second Let in refuses.
	again := env.authed(t, http.MethodPost, "/api/v1/household/invites/"+created.ID+"/admit", nil, session, csrf)
	assertErrorResponse(t, again, http.StatusConflict, "INVITE_ALREADY_ACCEPTED")
}

// Nobody has tapped the link yet: there is no one to let in.
func TestAdmitRefusesAnUnknockedTelegramInvite(t *testing.T) {
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

	admit := env.authed(t, http.MethodPost, "/api/v1/household/invites/"+created.ID+"/admit", nil, session, csrf)
	assertErrorResponse(t, admit, http.StatusConflict, "INVITE_NOT_KNOCKED")
}

// The chat may have joined a different household between the knock and the
// click (spec decision 15) -- the re-check InviteRepo.Admit's own
// transaction performs, proved here through the real route rather than
// only against the repository directly.
func TestAdmitRefusesAChatAlreadyBoundToAnotherAccount(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	created := env.mustCreateAndKnockTelegramInvite(t, session, csrf, 4242, "christine_t")

	owner, ok := env.findMemberByRole(t, session, "owner")
	if !ok {
		t.Fatal("setup: no owner in the seeded household")
	}
	if err := postgres.NewTelegramAccountRepo(env.db).Create(context.Background(), usecase.TelegramBinding{
		UserID: owner.User.ID, ChatID: 4242, ChatUsername: "christine_t",
	}); err != nil {
		t.Fatalf("bind the chat elsewhere: %v", err)
	}

	admit := env.authed(t, http.MethodPost, "/api/v1/household/invites/"+created.ID+"/admit", nil, session, csrf)
	assertErrorResponse(t, admit, http.StatusConflict, "CHAT_ALREADY_BOUND")
}

// findMemberByRole is admit-test setup: it reads the household's own roster
// back through the ordinary list route rather than a direct database read,
// so the owner id it hands back is exactly what a real caller would see.
func (env *testEnv) findMemberByRole(t *testing.T, session *http.Cookie, role string) (memberListEntry, bool) {
	t.Helper()
	for _, m := range env.getMembers(t, session) {
		if m.Role == role {
			return m, true
		}
	}
	return memberListEntry{}, false
}

// A limited member is not an owner -- Admit sits behind requireOwner the
// same as every other route in this group.
func TestALimitedMemberCannotAdmit(t *testing.T) {
	env := newTestEnv(t)
	ownerSession, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	created := env.mustCreateAndKnockTelegramInvite(t, ownerSession, ownerCSRF, 4242, "christine_t")

	limitedSession, limitedCSRF := env.signIn(t, env.limitedEmail, env.limitedPassword)
	rec := env.authed(t, http.MethodPost, "/api/v1/household/invites/"+created.ID+"/admit", nil, limitedSession, limitedCSRF)
	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")
}

// The matching code is compared by eye and accepted by nothing. If this
// ever fails, someone has turned a display into a credential, and ADR 4's
// rejection of guessable one-time codes applies (spec decision 3). There is
// no existing convention in this package for pinning an absent request
// field, so this reads the handler's own source -- the crude fallback the
// task brief itself names, not the preferred tool.
func TestTheAdmitRequestHasNoCodeField(t *testing.T) {
	body, err := os.ReadFile("invite_lobby_handlers.go")
	if err != nil {
		t.Fatalf("read handler source: %v", err)
	}
	for _, forbidden := range []string{"Code string", `json:"code"`} {
		if bytes.Contains(body, []byte(forbidden)) {
			t.Fatalf("the admit handler declares %q; the matching code must never be accepted by an endpoint", forbidden)
		}
	}
}
