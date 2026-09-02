package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"sort"
	"testing"
)

// grantedAdmin signs the owner in as a platform admin with a fresh grant,
// the state every test in this file starts from.
func grantedAdmin(t *testing.T, env *testEnv) *http.Cookie {
	t.Helper()
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	rec := env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin session: %d %s", rec.Code, rec.Body.String())
	}
	return session
}

func sortedKeys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not an object: %s", raw)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func assertKeys(t *testing.T, what string, raw json.RawMessage, want ...string) {
	t.Helper()
	got := sortedKeys(t, raw)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("%s keys = %v, want exactly %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s keys = %v, want exactly %v", what, got, want)
		}
	}
}

type householdsBody struct {
	Metrics    json.RawMessage   `json:"metrics"`
	Households []json.RawMessage `json:"households"`
	Truncated  bool              `json:"truncated"`
}

type listingBody struct {
	Name        string          `json:"name"`
	MemberCount int             `json:"memberCount"`
	Match       json.RawMessage `json:"match"`
}

// The key sets are asserted exactly: the spec's "no money on either screen"
// is a property of the wire shape, and a field added to a DTO by accident
// must fail here rather than pass through.
func TestAdminHouseholdsListsTheSeededHouseholdWithExactlyTheSpecsKeys(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)

	rec := env.authedGet(t, "/api/v1/admin/households", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body householdsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertKeys(t, "top level", rec.Body.Bytes(), "metrics", "households", "truncated")
	assertKeys(t, "metrics", body.Metrics, "households", "activeHouseholds7d", "signups30d", "pendingInvites")
	if len(body.Households) != 1 {
		t.Fatalf("households = %d rows, want the one seeded household", len(body.Households))
	}
	assertKeys(t, "household row", body.Households[0],
		"id", "name", "familyName", "memberCount", "createdAt", "lastActiveAt", "primaryCurrency", "match")

	var row listingBody
	if err := json.Unmarshal(body.Households[0], &row); err != nil {
		t.Fatalf("decode row: %v", err)
	}
	if row.Name != "Andreas & Christine" || row.MemberCount != 3 {
		t.Fatalf("row = %+v, want the seeded household with its three members", row)
	}
	if string(row.Match) != "null" {
		t.Fatalf("an unsearched list named a match: %s", row.Match)
	}
	if body.Truncated {
		t.Fatal("one household reported truncated")
	}
}

func TestAdminHouseholdsSearchByMemberEmailNamesTheMatch(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)

	rec := env.authedGet(t, "/api/v1/admin/households?q=ethan%40", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body householdsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Households) != 1 {
		t.Fatalf("search by a member's email found %d households, want 1", len(body.Households))
	}
	var match struct {
		MemberName  string  `json:"memberName"`
		MemberEmail *string `json:"memberEmail"`
	}
	var row listingBody
	if err := json.Unmarshal(body.Households[0], &row); err != nil {
		t.Fatalf("decode row: %v", err)
	}
	if err := json.Unmarshal(row.Match, &match); err != nil || match.MemberName != "Ethan" {
		t.Fatalf("match = %s, want Ethan", row.Match)
	}

	rec = env.authedGet(t, "/api/v1/admin/households?q=nobody-here", session)
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Households) != 0 {
		t.Fatalf("a search that matches nothing returned %d rows", len(body.Households))
	}
	if string(body.Metrics) == "" {
		t.Fatal("a no-match search dropped the metrics")
	}
}

type householdPageBody struct {
	Household      json.RawMessage   `json:"household"`
	Members        []json.RawMessage `json:"members"`
	PendingInvites []json.RawMessage `json:"pendingInvites"`
	Lockout        json.RawMessage   `json:"lockout"`
}

func TestAdminHouseholdDrillInShowsMembersAndTheLockout(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)
	path := "/api/v1/admin/households/" + env.householdID

	rec := env.authedGet(t, path, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var page householdPageBody
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertKeys(t, "top level", rec.Body.Bytes(), "household", "members", "pendingInvites", "lockout")
	assertKeys(t, "household", page.Household, "id", "name", "familyName", "createdAt", "primaryCurrency")
	if len(page.Members) != 3 {
		t.Fatalf("members = %d, want 3", len(page.Members))
	}
	assertKeys(t, "member", page.Members[0],
		"userId", "name", "email", "channel", "role", "capabilities", "lastActiveAt")
	if string(page.Lockout) != "null" {
		t.Fatalf("an unlocked household reported lockout = %s", page.Lockout)
	}

	// Three wrong passwords lock the household's sign-in (the same policy
	// AuthService applies); the drill-in must now say so.
	for i := 0; i < 3; i++ {
		env.do(http.MethodPost, "/api/v1/auth/sign-in",
			map[string]string{"email": env.limitedEmail, "password": "wrong-password"})
	}
	rec = env.authedGet(t, path, session)
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(page.Lockout) == "null" {
		t.Fatal("three failed sign-ins did not surface as a lockout on the drill-in")
	}
	assertKeys(t, "lockout", page.Lockout, "lockedUntil")
}

func TestAdminHouseholdUnknownAndMalformedIDsAre404(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)

	rec := env.authedGet(t, "/api/v1/admin/households/00000000-0000-0000-0000-000000000000", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")

	rec = env.authedGet(t, "/api/v1/admin/households/not-a-uuid", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}
