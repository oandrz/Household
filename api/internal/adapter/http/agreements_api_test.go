package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// zeroProposalID is a well-formed uuid belonging to nobody. Other files in
// this package spell the same value into a local `zeroUUID`; three tests here
// share it, so it is named once at the top instead.
const zeroProposalID = "00000000-0000-0000-0000-000000000000"

// agreementRoutes is the one list every route-shaped test in this file walks,
// so a route added later is walked by all of them without being added twice.
//
// It holds ONE route in this task and grows to seven in Task 8, when the six
// writes are actually routed. Listing an unrouted path here early does not
// merely relax the matrix -- chi answers an unmatched path from r.NotFound
// (router.go:114) BEFORE any group middleware runs, so an unrouted write 404s
// on every leg, including "no session", and the matrix below could not pass at
// this task's commit.
func agreementRoutes() []struct{ method, path string } {
	return []struct{ method, path string }{
		{http.MethodGet, "/api/v1/marriage/agreements"},
	}
}

// The decode targets below mirror agreement_handlers.go's DTOs field for
// field. They are test-side copies rather than the handlers' own unexported
// structs because this package is httpadapter_test, outside the package under
// test -- the shape billResponseBody and transactionsListBody already use in
// this directory.
type agreementLineBody struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Body   string `json:"body"`
}

type agreementSectionBody struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Count      int                 `json:"count"`
	Visible    bool                `json:"visible"`
	Agreements []agreementLineBody `json:"agreements"`
}

type agreementProposalBody struct {
	ID                string   `json:"id"`
	Kind              string   `json:"kind"`
	Status            string   `json:"status"`
	SectionID         string   `json:"sectionId"`
	SectionName       string   `json:"sectionName"`
	TargetAgreementID string   `json:"targetAgreementId"`
	Body              string   `json:"body"`
	PreviousBody      string   `json:"previousBody"`
	Note              string   `json:"note"`
	ParkNote          string   `json:"parkNote"`
	ProposedByName    string   `json:"proposedByName"`
	AwaitingNames     []string `json:"awaitingNames"`
	TargetChanged     bool     `json:"targetChanged"`
	CanAgree          bool     `json:"canAgree"`
	CanWithdraw       bool     `json:"canWithdraw"`
}

type agreementHistoryBody struct {
	Version       int      `json:"version"`
	ProposalID    string   `json:"proposalId"`
	Kind          string   `json:"kind"`
	Body          string   `json:"body"`
	PreviousBody  string   `json:"previousBody"`
	SignedByNames []string `json:"signedByNames"`
}

type agreementOwnerBody struct {
	MembershipID string `json:"membershipId"`
	Name         string `json:"name"`
}

// UpdatedAt is *string, not *time.Time: this test only ever asks whether it is
// null, and the raw-bytes test below is what pins the literal null itself.
type agreementsDocumentBody struct {
	Locked    bool                    `json:"locked"`
	Owners    []agreementOwnerBody    `json:"owners"`
	Version   int                     `json:"version"`
	UpdatedAt *string                 `json:"updatedAt"`
	Sections  []agreementSectionBody  `json:"sections"`
	Proposals []agreementProposalBody `json:"proposals"`
	History   []agreementHistoryBody  `json:"history"`
}

type agreementsReadBody struct {
	Agreements agreementsDocumentBody `json:"agreements"`
}

// mustReadAgreements is setup, not an assertion: a read that did not answer
// 200 with a parseable body fails here rather than as a confusing empty
// struct in whatever asserts on the document next. Decoding it is also what
// proves the 2xx carries JSON at all -- apiFetch throws on an ok response it
// cannot parse.
func mustReadAgreements(t *testing.T, env *testEnv, session *http.Cookie) agreementsDocumentBody {
	t.Helper()
	rec := env.authedGet(t, "/api/v1/marriage/agreements", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET agreements: status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}
	var out agreementsReadBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode agreements: %v (body = %s)", err, rec.Body.String())
	}
	return out.Agreements
}

// TestAgreementRoutesRequireMarriageAndOwner is
// TestMarriageRoutesRequireMarriageAndOwner's shape (marriage_api_test.go)
// applied to this feature: every agreements route against no session, a
// limited member, and an owner.
//
// The owner leg asserts only that NO GUARD refused. On newTestEnv's one-owner
// household the read answers 200 and, once Task 8 routes them, the writes
// answer 409, 404 or 400 -- each write's real status is its own test in Task
// 8, and pinning it twice would make this matrix fail whenever a body shape
// changed.
func TestAgreementRoutesRequireMarriageAndOwner(t *testing.T) {
	env := newTestEnv(t)
	for _, route := range agreementRoutes() {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			if rec := env.do(route.method, route.path, nil); rec.Code != http.StatusUnauthorized {
				t.Fatalf("no session = %d, want 401 (body = %s)", rec.Code, rec.Body.String())
			}
			session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)
			if rec := requestRouteAs(t, env, route.method, route.path, session, csrf); rec.Code != http.StatusForbidden {
				t.Fatalf("limited member = %d, want 403 (body = %s)", rec.Code, rec.Body.String())
			}
			session, csrf = env.signIn(t, env.ownerEmail, env.ownerPassword)
			rec := requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Fatalf("owner = %d, want any non-guard status (body = %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// requireCapability and requireOwner both answer 403 FORBIDDEN, so the matrix
// above cannot say which one refused the limited member. Only a caller HOLDING
// marriage without being an owner can see requireOwner, and three independent
// layers refuse to build that membership (domain.NewMembership, MemberService,
// and 00002_identity.sql's limited_members_have_no_marriage CHECK). So it is
// doctored through the same membershipDouble seam
// TestMarriageRouteRejectsALimitedMemberHoldingMarriage uses
// (marriage_api_test.go:155), swapped in for this one request only.
//
// HouseholdID on the doctored membership must match env.householdID:
// requireSession cross-checks it against the session row and answers 401 on a
// mismatch, which would look like the guard was never reached rather than
// like it refused.
func TestAgreementsRouteRejectsALimitedMemberHoldingMarriage(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.limitedEmail, env.limitedPassword)

	router := env.routerWithMemberships(membershipDouble{
		MembershipRepository: env.deps.Memberships,
		membership: domain.Membership{
			HouseholdID:  env.householdID,
			UserID:       "irrelevant-scope-userid-comes-from-the-real-session",
			Role:         domain.RoleLimited,
			Capabilities: domain.Capabilities{domain.CapMarriage},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/marriage/agreements", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")
}

// TestLockedAgreementsReadCarriesLiteralEmptyArrays reads the RAW WIRE BYTES,
// because Go decodes null and [] into the same nil slice -- only the bytes
// prove the frontend's Zod schemas get []. Vision's
// TestGetVisionForANeverSetYearCarriesLiteralEmptyArrays
// (vision_api_test.go:118) is this test's shape.
//
// newTestEnv's household has one owner, so this is decision 2's state: locked,
// nothing written yet. 200 rather than 403 because what it lacks is a second
// owner, not permission, and the empty state IS the page (decision 3 -- the
// read is never gated on locked).
func TestLockedAgreementsReadCarriesLiteralEmptyArrays(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/marriage/agreements", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	for _, want := range []string{
		`"locked":true`, `"version":1`, `"updatedAt":null`,
		`"sections":[]`, `"proposals":[]`, `"history":[]`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("wire body does not literally carry %s -- got %s", want, raw)
		}
	}

	// The owners list travels on every response and its LENGTH is the owner
	// count -- no second count travels beside it, so a screen that says
	// "one owner" is reading these rows.
	doc := mustReadAgreements(t, env, session)
	if len(doc.Owners) != 1 || doc.Owners[0].Name != "Andreas" {
		t.Fatalf("owners = %+v, want exactly the single owner Andreas", doc.Owners)
	}
}
