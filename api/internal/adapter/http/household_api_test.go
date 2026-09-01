package httpadapter_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// TestLimitedMemberCannotUpdateMembers covers behaviour 10. It has the
// limited member try to promote their own membership to owner -- the
// realistic case the requireOwner guard exists to stop -- rather than
// editing someone else's, but either target must be rejected identically:
// requireOwner checks the caller's own role, not whose membership is named
// in the URL.
func TestLimitedMemberCannotUpdateMembers(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)

	rec := env.authed(t, http.MethodPatch, "/api/v1/household/members/"+env.limitedMembership,
		map[string]any{"role": "owner", "capabilities": domain.AllCapabilities().Strings()}, session, csrf)
	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")
}

// TestLimitedMemberCannotCreateSpace covers behaviour 11.
func TestLimitedMemberCannotCreateSpace(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/spaces",
		map[string]any{"name": "Movie Night", "visibility": "everyone"}, session, csrf)
	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")
}

// TestOwnerOnlyRoutesRejectALimitedMember is the sibling of
// TestEveryProtectedRouteRejectsAnUnauthenticatedCaller: it walks the live
// router and, for every mutating route (method not GET/HEAD/OPTIONS), signs
// in as a *limited* member and attaches a genuinely valid session and CSRF
// token -- so the only thing that could still reject the request is
// requireOwner -- then asserts 403 FORBIDDEN unless the route is on the
// short, commented allowlist below.
//
// An earlier version of this test was a hand-maintained table of "routes I
// believe are owner-gated," justified by a claim that chi.Walk can't be used
// here because Go function values aren't comparable, so there was no
// reliable way to check whether a route's middleware chain included
// requireOwner specifically. That claim was correct but beside the point:
// this test was never supposed to introspect the middleware chain. It
// observes behaviour, exactly as the unauthenticated matrix does for
// requireSession -- a route wired without requireOwner now simply succeeds
// when it should have been forbidden, and fails this test on that basis, no
// reflection required. A route added to the owner-gated set without
// actually being wired behind requireOwner in router.go fails this test
// rather than shipping unnoticed.
//
// TestLimitedMemberCannotUpdateMembers and TestLimitedMemberCannotCreateSpace
// above still individually pin the task's original eleven enumerated
// behaviours verbatim; this walk is the exhaustive superset the
// coordinator's route audit asked for.
//
// Signed in as env.moneyLimitedEmail, not env.limitedEmail: the plain limited
// fixture holds only calendar and chores, so every accounts write route below
// would refuse it at requireCapability before the request ever reached
// requireOwner, and this walk would pass without ever exercising the guard
// it is named after. env.moneyLimitedEmail also holds money and is still
// limited, so every assertion this walk already made keeps holding.
func TestOwnerOnlyRoutesRejectALimitedMember(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)

	// Every entry is a mutating route that is correctly NOT owner-gated,
	// with the reason it's exempt recorded alongside it.
	allowlist := map[string]bool{
		// Public, pre-auth: reached before any session exists at all, so
		// there is no caller identity yet to check ownership against.
		"POST /api/v1/auth/sign-in":                  true,
		"POST /api/v1/auth/magic-link":               true,
		"POST /api/v1/auth/magic-link/consume":       true,
		"POST /api/v1/auth/sign-up":                  true,
		"POST /api/v1/auth/sign-up/{token}/complete": true,
		// Same reasoning: no identifier, no session required. It answers 404
		// rather than 403 when Telegram is not configured -- the same 404
		// any unrouted path gets -- regardless of who is signed in.
		"POST /api/v1/auth/telegram/start": true,
		// Any signed-in member, owner or not, may end their own session --
		// ownership has nothing to do with signing yourself out.
		"POST /api/v1/auth/sign-out": true,
	}

	routes, ok := env.router.(chi.Routes)
	if !ok {
		t.Fatal("router does not implement chi.Routes")
	}

	replacer := strings.NewReplacer("{id}", "00000000-0000-0000-0000-000000000000")

	checked := 0
	adminChecked := 0
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		switch method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return nil
		}
		// The admin subtree is gated on an axis orthogonal to household
		// ownership -- a platform_admins row, not domain.RoleOwner -- and its
		// refusal is deliberately a 404, so requiring 403 FORBIDDEN of it
		// would be asserting the opposite of what the design wants (see
		// requirePlatformAdmin's doc comment for why a 403 there would
		// confirm both that the surface exists and that the caller found the
		// right path).
		//
		// It is asserted rather than allowlisted past. A skip would waste the
		// one walk that already has a non-owner signed in and every admin
		// route enumerated; this branch turns the same walk into a structural
		// second check of the 404, covering every mutating admin route added
		// later for free, without a named entry anyone could forget.
		if strings.HasPrefix(route, "/api/v1/admin") {
			path := replacer.Replace(route)
			req := httptest.NewRequest(method, path, bytes.NewReader([]byte("{}")))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(session)
			req.AddCookie(csrf)
			req.Header.Set("X-CSRF-Token", csrf.Value)
			rec := httptest.NewRecorder()
			env.router.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s: status = %d, want 404 -- the admin surface must look "+
					"like a typo to a household member (body = %s)", method, route, rec.Code, rec.Body.String())
			} else if body := decodeError(t, rec); body.Error.Code != "NOT_FOUND" {
				t.Errorf("%s %s: error code = %q, want NOT_FOUND", method, route, body.Error.Code)
			}
			adminChecked++
			return nil
		}
		if invitePreAuthRoutes[method+" "+route] {
			// Public, pre-auth, and mutating (POST .../accept) -- exempt for
			// the identical reason the /auth/* entries above are. See
			// invitePreAuthRoutes' doc comment for why this is a named
			// two-route allowlist rather than a "/api/v1/invites/" prefix
			// skip.
			return nil
		}
		if allowlist[method+" "+route] {
			return nil
		}

		path := replacer.Replace(route)
		req := httptest.NewRequest(method, path, bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(session)
		req.AddCookie(csrf)
		req.Header.Set("X-CSRF-Token", csrf.Value)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403 (body = %s)", method, route, rec.Code, rec.Body.String())
		} else {
			body := decodeError(t, rec)
			if body.Error.Code != "FORBIDDEN" {
				t.Errorf("%s %s: error code = %q, want FORBIDDEN", method, route, body.Error.Code)
			}
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	t.Logf("checked %d owner-gated candidate routes, %d admin routes", checked, adminChecked)
	// The admin floor guards the branch above the same way the owner floor
	// below guards the walk: if the subtree stopped being enumerated, the
	// branch would assert nothing and pass.
	//
	// 4, not the pre-Task-8 1: POST /admin/session plus the three mutating
	// flag routes Task 8 adds (PUT .../flags/{key}, PUT and DELETE
	// .../flags/{key}/households/{householdID}). Left at 1 it would still
	// pass with all three of those silently dropped from the walk -- the
	// exact vacuous pass this floor exists to catch. Raise it again as more
	// admin routes are added, exactly as the owner floor below has been
	// raised.
	if adminChecked < 4 {
		t.Fatalf("checked %d admin routes, want at least 4 -- "+
			"the walk is no longer reaching the admin subtree", adminChecked)
	}
	// 10, not the pre-accounts 6: the four accounts write routes are mutating
	// and owner-gated too, and a floor left at the old count would still pass
	// if all four vanished from the walk -- exactly the vacuous pass this
	// guard exists to catch.
	if checked < 10 {
		t.Fatalf("checked %d routes, want at least 10 -- "+
			"the walk may not be enumerating routes correctly", checked)
	}
}

// memberListEntry mirrors member_handlers.go's memberViewDTO for decoding
// GET /household/members responses in the tests below.
type memberListEntry struct {
	ID   string `json:"id"`
	User struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		DisplayName   string `json:"displayName"`
		AvatarInitial string `json:"avatarInitial"`
	} `json:"user"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

func (env *testEnv) getMembers(t *testing.T, session *http.Cookie) []memberListEntry {
	t.Helper()
	rec := env.do(http.MethodGet, "/api/v1/household/members", nil, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /household/members: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var members []memberListEntry
	if err := json.NewDecoder(rec.Body).Decode(&members); err != nil {
		t.Fatalf("decode member list: %v (body = %s)", err, rec.Body.String())
	}
	return members
}

// TestMemberListRevealsEmailsToAnOwner covers the coordinator's ruling on
// GET /household/members: an owner caller sees the full roster with every
// member's real email address populated.
func TestMemberListRevealsEmailsToAnOwner(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	members := env.getMembers(t, session)
	if len(members) != 3 {
		t.Fatalf("members = %d, want 3 (owner + the two limited members)", len(members))
	}
	for _, m := range members {
		if m.User.Email == "" {
			t.Fatalf("member %q (%s) has an empty email, want it populated for an owner caller",
				m.User.DisplayName, m.Role)
		}
	}
}

// TestMemberListWithholdsEmailsFromALimitedMember is
// TestMemberListRevealsEmailsToAnOwner's sibling: a limited caller sees the
// identical roster -- same member count, names, roles and capabilities --
// with every email emptied rather than the list filtered down to fewer
// rows. The member-count assertion is what would catch a future change that
// filtered rows instead of redacting the one field that needs it: a row
// filter would still make this test's email assertions pass while quietly
// hiding other members entirely.
func TestMemberListWithholdsEmailsFromALimitedMember(t *testing.T) {
	env := newTestEnv(t)

	ownerSession, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	asOwner := env.getMembers(t, ownerSession)

	limitedSession, _ := env.signIn(t, env.limitedEmail, env.limitedPassword)
	asLimited := env.getMembers(t, limitedSession)

	if len(asLimited) != len(asOwner) {
		t.Fatalf("members seen by limited caller = %d, want %d (same as an owner) -- "+
			"emails must be redacted per-field, not filtered by row", len(asLimited), len(asOwner))
	}

	byID := make(map[string]memberListEntry, len(asOwner))
	for _, m := range asOwner {
		byID[m.ID] = m
	}

	for _, m := range asLimited {
		if m.User.Email != "" {
			t.Fatalf("member %q (%s) has email %q, want it withheld from a limited caller",
				m.User.DisplayName, m.Role, m.User.Email)
		}

		owner, ok := byID[m.ID]
		if !ok {
			t.Fatalf("member %+v (id %q) is not among the members an owner sees -- "+
				"the roster itself must be identical, only the email field should differ", m, m.ID)
		}
		if m.User.DisplayName != owner.User.DisplayName ||
			m.User.AvatarInitial != owner.User.AvatarInitial ||
			m.Role != owner.Role ||
			!slicesEqual(m.Capabilities, owner.Capabilities) {
			t.Fatalf("member %q differs beyond email: limited view = %+v, owner view = %+v", m.ID, m, owner)
		}
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type householdResponse struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	FamilyName            string `json:"familyName"`
	PrimaryCurrency       string `json:"primaryCurrency"`
	ShowSecondaryCurrency bool   `json:"showSecondaryCurrency"`
	SecondaryCurrency     string `json:"secondaryCurrency"`
	FXRateMode            string `json:"fxRateMode"`
}

func (env *testEnv) getHousehold(t *testing.T, session *http.Cookie) householdResponse {
	t.Helper()
	rec := env.do(http.MethodGet, "/api/v1/household", nil, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /household: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var h householdResponse
	if err := json.NewDecoder(rec.Body).Decode(&h); err != nil {
		t.Fatalf("decode household: %v (body = %s)", err, rec.Body.String())
	}
	return h
}

// TestUpdateHouseholdIsARealPatch pins the fix for Finding 1: PATCH
// /household previously assigned every field unconditionally from plain
// value fields, so an omitted field and an explicit zero value were
// indistinguishable -- sending the API spec's own documented body (which
// omits secondaryCurrency) blanked it to "", and HouseholdService.Update's
// currency validation then failed with a 500. Pointer fields fix this: an
// absent field must leave the current value untouched, and a bad currency
// must report 422, never 500.
func TestUpdateHouseholdIsARealPatch(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	t.Run("every field present", func(t *testing.T) {
		rec := env.authed(t, http.MethodPatch, "/api/v1/household", map[string]any{
			"name": "The Oentoros", "familyName": "Oentoro", "primaryCurrency": "SGD",
			"showSecondaryCurrency": false, "secondaryCurrency": "IDR", "fxRateMode": "manual",
		}, session, csrf)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var got householdResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
		}
		if got.Name != "The Oentoros" || got.FamilyName != "Oentoro" || got.PrimaryCurrency != "SGD" ||
			got.ShowSecondaryCurrency || got.SecondaryCurrency != "IDR" || got.FXRateMode != "manual" {
			t.Fatalf("household = %+v, want every field updated to what was sent", got)
		}
	})

	t.Run("a single field present leaves the rest unchanged", func(t *testing.T) {
		before := env.getHousehold(t, session)

		rec := env.authed(t, http.MethodPatch, "/api/v1/household",
			// The spec's own documented PATCH body: only familyName,
			// primaryCurrency, showSecondaryCurrency and fxRateMode --
			// secondaryCurrency (and name) are deliberately omitted here,
			// which is exactly the shape that used to 500.
			map[string]any{"familyName": "Oentoro-Wattimena"}, session, csrf)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var got householdResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
		}
		if got.FamilyName != "Oentoro-Wattimena" {
			t.Fatalf("familyName = %q, want the new value", got.FamilyName)
		}
		if got.Name != before.Name ||
			got.PrimaryCurrency != before.PrimaryCurrency ||
			got.ShowSecondaryCurrency != before.ShowSecondaryCurrency ||
			got.SecondaryCurrency != before.SecondaryCurrency ||
			got.FXRateMode != before.FXRateMode {
			t.Fatalf("PATCHing only familyName changed other fields: before = %+v, after = %+v", before, got)
		}
	})

	t.Run("an invalid currency reports 422, not 500", func(t *testing.T) {
		rec := env.authed(t, http.MethodPatch, "/api/v1/household",
			map[string]any{"primaryCurrency": "nope"}, session, csrf)
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_CURRENCY")
	})

	// The surviving sibling of the ErrInvalidMoney fix above: fxRateMode
	// reaches the database's CHECK (fx_rate_mode IN ('auto', 'manual'))
	// constraint completely unvalidated on this path, so a caller-supplied
	// value outside that pair used to reach the constraint first and 500.
	t.Run("an invalid fxRateMode reports 422, not 500", func(t *testing.T) {
		rec := env.authed(t, http.MethodPatch, "/api/v1/household",
			map[string]any{"fxRateMode": "weekly"}, session, csrf)
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_FX_RATE_MODE")
	})
}

type notificationPreferencesResponse struct {
	BillReminders   bool `json:"billReminders"`
	OverspendAlerts bool `json:"overspendAlerts"`
	RetroReminder   bool `json:"retroReminder"`
	WeeklyDigest    bool `json:"weeklyDigest"`
}

// TestUpdateNotificationPreferencesIsARealPatch is
// TestUpdateHouseholdIsARealPatch's sibling for
// PATCH /notification-preferences, which had the identical bug: a plain
// bool field cannot distinguish "the caller didn't mention this toggle"
// from "the caller wants it off," so an omitted field was silently set to
// false.
func TestUpdateNotificationPreferencesIsARealPatch(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// Establish a known starting point: every toggle on.
	rec := env.authed(t, http.MethodPatch, "/api/v1/notification-preferences", map[string]any{
		"billReminders": true, "overspendAlerts": true, "retroReminder": true, "weeklyDigest": true,
	}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup PATCH status = %d, body = %s", rec.Code, rec.Body.String())
	}

	t.Run("a single field present leaves the rest unchanged", func(t *testing.T) {
		rec := env.authed(t, http.MethodPatch, "/api/v1/notification-preferences",
			map[string]any{"weeklyDigest": false}, session, csrf)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var got notificationPreferencesResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
		}
		if got.WeeklyDigest {
			t.Fatal("weeklyDigest = true, want false after the PATCH")
		}
		if !got.BillReminders || !got.OverspendAlerts || !got.RetroReminder {
			t.Fatalf("PATCHing only weeklyDigest changed other toggles: %+v", got)
		}
	})

	t.Run("every field present", func(t *testing.T) {
		rec := env.authed(t, http.MethodPatch, "/api/v1/notification-preferences", map[string]any{
			"billReminders": false, "overspendAlerts": false, "retroReminder": false, "weeklyDigest": false,
		}, session, csrf)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var got notificationPreferencesResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
		}
		if got.BillReminders || got.OverspendAlerts || got.RetroReminder || got.WeeklyDigest {
			t.Fatalf("preferences = %+v, want every toggle false", got)
		}
	})
}

// TestCreateSpaceWithABlankNameReturns422 pins the fix for Finding 4's other
// sentinel: usecase.ErrSpaceNameRequired had no MapDomainError case at all
// and fell through to a bare 500 for what is an entirely ordinary bad
// request -- a blank space name.
func TestCreateSpaceWithABlankNameReturns422(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/spaces",
		map[string]any{"name": "   ", "visibility": "everyone"}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "SPACE_NAME_REQUIRED")
}

// TestInviteMemberRejectsAnAddressThatAlreadyHasAUsersRow pins the fix for
// the invite-to-an-existing-member 500: InviteRepo.Accept unconditionally
// calls CreateUser and never reuses an existing row, so an owner inviting an
// address that already belongs to a member (a mistype, or a re-invite) used
// to get 201 with the mail sent, and the recipient would then 500 forever at
// acceptance. This must be rejected at creation, where the owner who typed
// the address can see it and act on it.
func TestInviteMemberRejectsAnAddressThatAlreadyHasAUsersRow(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/household/members/invite", map[string]any{
		"name": "Ethan Again", "email": env.limitedEmail, "role": "limited",
		"capabilities": []string{"calendar"},
	}, session, csrf)
	assertErrorResponse(t, rec, http.StatusConflict, "EMAIL_ALREADY_REGISTERED")
}

// TestSignInForARemovedMemberReturns401NotA404 pins the fix for the other
// symptom sharing the invite-500's root cause: removing a member deletes
// only its memberships row, not the users row underneath it, so the address
// still resolves through Users.ByEmail. SignIn used to call Members.ByUser
// next, get domain.ErrNotFound back, and propagate it bare -- MapDomainError
// turns that into 404, a status no other sign-in failure produces and a
// stranger's guess never gets, which itself discloses that the address once
// belonged to someone. It must fail exactly like any other sign-in failure:
// 401 INVALID_CREDENTIALS.
func TestSignInForARemovedMemberReturns401NotA404(t *testing.T) {
	env := newTestEnv(t)
	ownerSession, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodDelete, "/api/v1/household/members/"+env.limitedMembership,
		nil, ownerSession, ownerCSRF)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove member: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = env.do(http.MethodPost, "/api/v1/auth/sign-in", map[string]string{
		"email": env.limitedEmail, "password": env.limitedPassword,
	})
	assertErrorResponse(t, rec, http.StatusUnauthorized, "INVALID_CREDENTIALS")
}

// --- Task 20 fix round: PATCH /household/members/:id was the last PATCH
// still behaving like a PUT ----------------------------------------------

// TestUpdateMemberIsARealPatch is TestUpdateHouseholdIsARealPatch's and
// TestUpdateNotificationPreferencesIsARealPatch's sibling for
// PATCH /household/members/:id, which had the identical bug for longer:
// plain (non-pointer) Role/Capabilities fields meant a caller had to send
// both together, or the omitted one decoded to its zero value and 422'd as
// an unknown role or an invalid capability set. Unlike the other two, this
// endpoint's role and capabilities also interact through domain rules that
// only make sense evaluated together (an owner must hold every capability;
// a limited member may never hold "marriage"), so beyond "absent means
// unchanged" this also pins that a role-only change is validated against
// the membership's *existing* capabilities, not a zero-valued stand-in for
// them.
//
// The seeded limited member (env.limitedMembership, capabilities
// {calendar, chores}) is used throughout: it is the one membership in
// newTestEnv whose existing capabilities are a strict subset of
// domain.AllCapabilities(), which is exactly what makes the third subtest
// below possible.
func TestUpdateMemberIsARealPatch(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	t.Run("a role-only patch leaves capabilities intact", func(t *testing.T) {
		// Sent role equals the membership's current role -- a legitimate
		// "role-only" body (only the "role" key is present at all) that
		// exercises the exact regression a pointer-less fix would miss: if
		// an absent capabilities field decoded to an empty slice instead of
		// the current value, this would still succeed (an empty set is
		// valid for "limited") while silently wiping Ethan's capabilities.
		rec := env.authed(t, http.MethodPatch, "/api/v1/household/members/"+env.limitedMembership,
			map[string]any{"role": "limited"}, session, csrf)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var got updateMemberResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
		}
		if got.Role != "limited" {
			t.Fatalf("role = %q, want limited", got.Role)
		}
		if !slicesEqual(got.Capabilities, []string{"calendar", "chores"}) {
			t.Fatalf("capabilities = %v, want [calendar chores] unchanged by a role-only patch", got.Capabilities)
		}

		members := env.getMembers(t, session)
		persisted := mustFindMember(t, members, env.limitedMembership)
		if !slicesEqual(persisted.Capabilities, []string{"calendar", "chores"}) {
			t.Fatalf("persisted capabilities = %v, want [calendar chores] unchanged", persisted.Capabilities)
		}
	})

	t.Run("a capabilities-only patch leaves the role intact", func(t *testing.T) {
		// This is the exact shape that used to 422 INVALID_ROLE: only
		// "capabilities" is sent, no "role" key at all.
		rec := env.authed(t, http.MethodPatch, "/api/v1/household/members/"+env.limitedMembership,
			map[string]any{"capabilities": []string{"calendar", "chores", "money"}}, session, csrf)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var got updateMemberResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v (body = %s)", err, rec.Body.String())
		}
		if got.Role != "limited" {
			t.Fatalf("role = %q, want limited (unchanged by a capabilities-only patch)", got.Role)
		}
		if !slicesEqual(got.Capabilities, []string{"calendar", "chores", "money"}) {
			t.Fatalf("capabilities = %v, want [calendar chores money]", got.Capabilities)
		}

		members := env.getMembers(t, session)
		persisted := mustFindMember(t, members, env.limitedMembership)
		if persisted.Role != "limited" {
			t.Fatalf("persisted role = %q, want limited unchanged", persisted.Role)
		}
	})

	t.Run("a role-only promotion to owner with a partial capability set still 422s", func(t *testing.T) {
		// At this point env.limitedMembership holds {calendar, chores,
		// money} (the previous subtest's result) -- still missing
		// "marriage", so promoting it to owner without also sending every
		// capability must be validated against those *existing*
		// capabilities and rejected, not validated against an empty or
		// full stand-in value that would let it through incorrectly.
		rec := env.authed(t, http.MethodPatch, "/api/v1/household/members/"+env.limitedMembership,
			map[string]any{"role": "owner"}, session, csrf)
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_CAPABILITIES")

		// And the rejection must not have partially applied: still limited,
		// still exactly the capabilities from before this subtest ran.
		members := env.getMembers(t, session)
		persisted := mustFindMember(t, members, env.limitedMembership)
		if persisted.Role != "limited" {
			t.Fatalf("persisted role = %q, want limited -- a rejected update must not apply", persisted.Role)
		}
		if !slicesEqual(persisted.Capabilities, []string{"calendar", "chores", "money"}) {
			t.Fatalf("persisted capabilities = %v, want [calendar chores money] unchanged by the rejected PATCH",
				persisted.Capabilities)
		}
	})
}

type updateMemberResponse struct {
	ID           string   `json:"id"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

func mustFindMember(t *testing.T, members []memberListEntry, id string) memberListEntry {
	t.Helper()
	for _, m := range members {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("no member with id %q in %+v", id, members)
	return memberListEntry{}
}
