package httpadapter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// zeroProposalID is a well-formed uuid belonging to nobody. Other files in
// this package spell the same value into a local `zeroUUID`; three tests here
// share it, so it is named once at the top instead.
const zeroProposalID = "00000000-0000-0000-0000-000000000000"

// agreementRoutes is the one list every route-shaped test in this file walks,
// so a route added later is walked by all of them without being added twice.
// The GET is first and stays first: TestAgreementWriteRoutesRequireCSRF drops
// it with [1:], requireCSRF exempting reads entirely.
func agreementRoutes() []struct{ method, path string } {
	p := proposalPath(zeroProposalID)
	return []struct{ method, path string }{
		{http.MethodGet, "/api/v1/marriage/agreements"},
		{http.MethodPost, "/api/v1/marriage/agreements/sections"},
		{http.MethodPost, "/api/v1/marriage/agreements/starter-set"},
		{http.MethodPost, agreementProposalsPath},
		{http.MethodPost, p + "/agree"},
		{http.MethodPost, p + "/park"},
		{http.MethodPost, p + "/withdraw"},
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

// TestAgreementProposeRouteRejectsALimitedMemberHoldingMarriage extends the
// doctored-membership pattern above to a WRITE route. Propose is the write
// picked to isolate requireOwner for, because it is the one write with
// nothing else beneath the HTTP layer to catch a missing guard: Sign's own
// SQL only inserts a signature `WHERE ... m.role = 'owner'`
// (queries/agreements.sql, SignAgreementProposal) and Withdraw's SQL refuses
// unless the caller IS the proposer or the proposer has left ownership
// (WithdrawAgreementProposal) -- but InsertAgreementProposal performs no
// role check at all. If requireOwner were ever dropped from this group, or
// the six writes moved to a sibling group that forgot it, Propose is the one
// call a limited member could reach and have fully succeed: an arbitrary
// proposal landed in the household's agreements document, nothing left to
// refuse it.
//
// The request carries a real CSRF cookie and a matching X-CSRF-Token header,
// so of the three guards this route sits behind (requireCapability,
// requireOwner, requireCSRF), only requireOwner is left able to answer here
// -- the doctored membership holds CapMarriage, so capability passes, and
// CSRF is satisfied on purpose. A bare status/code check on this one request
// is therefore a real isolation of requireOwner, not a guess about which of
// three guards fired.
func TestAgreementProposeRouteRejectsALimitedMemberHoldingMarriage(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)

	router := env.routerWithMemberships(membershipDouble{
		MembershipRepository: env.deps.Memberships,
		membership: domain.Membership{
			HouseholdID:  env.householdID,
			UserID:       "irrelevant-scope-userid-comes-from-the-real-session",
			Role:         domain.RoleLimited,
			Capabilities: domain.Capabilities{domain.CapMarriage},
		},
	})

	req := httptest.NewRequest(http.MethodPost, agreementProposalsPath, nil)
	req.AddCookie(session)
	req.AddCookie(csrf)
	req.Header.Set("X-CSRF-Token", csrf.Value)
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

const agreementProposalsPath = "/api/v1/marriage/agreements/proposals"

func proposalPath(id string) string { return agreementProposalsPath + "/" + id }

// addSecondOwner seeds the partner through the repositories -- api_test.go's
// own owner-seeding shape (api_test.go:420-433) -- and signs them in for real
// cookies, because every test below needs two sessions rather than two
// membership rows.
//
// It is per-file and deliberately NOT part of newTestEnv: other files assert
// on that household having exactly one owner, and moving this into the
// constructor would break them in a way that looks unrelated to this feature.
//
// The cheap argon2 parameters are the ones newTestEnv itself uses -- this is
// still the real hasher, only its cost is turned down, so sign-in exercises
// Verify exactly as it does in production.
func addSecondOwner(t *testing.T, env *testEnv) (session, csrf *http.Cookie) {
	t.Helper()
	hash, err := crypto.NewArgon2Hasher(1, 8*1024, 1).Hash("hunter2hunter2")
	if err != nil {
		t.Fatalf("hash second owner password: %v", err)
	}
	user, err := env.users.Create(context.Background(), "christine@hearth.family", hash, "Christine")
	if err != nil {
		t.Fatalf("create second owner user: %v", err)
	}
	if _, err := env.deps.Memberships.Create(context.Background(), domain.Membership{
		HouseholdID:  env.householdID,
		UserID:       user.ID,
		Role:         domain.RoleOwner,
		Capabilities: domain.AllCapabilities(),
	}); err != nil {
		t.Fatalf("create second owner membership: %v", err)
	}
	return env.signIn(t, "christine@hearth.family", "hunter2hunter2")
}

type agreementSectionWriteBody struct {
	Section    agreementSectionBody   `json:"section"`
	Agreements agreementsDocumentBody `json:"agreements"`
}

type agreementProposalWriteBody struct {
	Proposal   agreementProposalBody  `json:"proposal"`
	Agreements agreementsDocumentBody `json:"agreements"`
}

// mustCreateAgreementSection and mustProposalWrite are setup, not assertions:
// a write that did not land fails here rather than as a confusing failure in
// whatever reads its id next. Decoding every response is also what proves each
// 2xx carries parseable JSON -- apiFetch throws on an ok response it cannot
// parse, so an unparseable 200 is a broken screen, not a passing test.
func mustCreateAgreementSection(t *testing.T, env *testEnv, name string, session, csrf *http.Cookie) agreementSectionBody {
	t.Helper()
	rec := env.authed(t, http.MethodPost, "/api/v1/marriage/agreements/sections",
		map[string]any{"name": name}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create section %q: status = %d, want 201 (body = %s)", name, rec.Code, rec.Body.String())
	}
	var out agreementSectionWriteBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode section write: %v (body = %s)", err, rec.Body.String())
	}
	return out.Section
}

func mustProposalWrite(t *testing.T, env *testEnv, path string, body any,
	session, csrf *http.Cookie, want int) agreementProposalWriteBody {
	t.Helper()
	rec := env.authed(t, http.MethodPost, path, body, session, csrf)
	if rec.Code != want {
		t.Fatalf("POST %s: status = %d, want %d (body = %s)", path, rec.Code, want, rec.Body.String())
	}
	var out agreementProposalWriteBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v (body = %s)", path, err, rec.Body.String())
	}
	return out
}

// findAgreement locates one live agreement by its wording, across sections,
// because the numbering runs continuously across them and no test should
// depend on which section index it landed in.
func findAgreement(t *testing.T, doc agreementsDocumentBody, body string) agreementLineBody {
	t.Helper()
	for _, s := range doc.Sections {
		for _, a := range s.Agreements {
			if a.Body == body {
				return a
			}
		}
	}
	t.Fatalf("no live agreement reads %q -- sections = %+v", body, doc.Sections)
	return agreementLineBody{}
}

func findProposal(doc agreementsDocumentBody, id string) (agreementProposalBody, bool) {
	for _, p := range doc.Proposals {
		if p.ID == id {
			return p, true
		}
	}
	return agreementProposalBody{}, false
}

// TestAgreementWriteRoutesRequireCSRF sends all six writes twice -- no header,
// then a wrong one -- and asserts the CODE, not the status. requireOwner sits
// ahead of requireCSRF in the same group and answers the identical 403, so a
// bare status check stays green with the requireCSRF line deleted, which is
// the one thing this test exists to catch.
func TestAgreementWriteRoutesRequireCSRF(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	for _, route := range agreementRoutes()[1:] { // [1:] drops the GET: requireCSRF exempts reads
		for _, token := range []string{"", "definitely-the-wrong-value"} {
			t.Run(route.method+" "+route.path+" token="+token, func(t *testing.T) {
				req := httptest.NewRequest(route.method, route.path, nil)
				req.AddCookie(session)
				req.AddCookie(csrf)
				if token != "" {
					req.Header.Set("X-CSRF-Token", token)
				}
				rec := httptest.NewRecorder()
				env.router.ServeHTTP(rec, req)
				assertErrorResponse(t, rec, http.StatusForbidden, "CSRF_INVALID")
			})
		}
	}
}

// TestAgreementWritesOnATwoOwnerHousehold walks one proposal's whole life on
// the wire. Withdraw is exercised in BOTH directions, because a comparison
// with its sides swapped passes every one-sided refusal test.
func TestAgreementWritesOnATwoOwnerHousehold(t *testing.T) {
	env := newTestEnv(t)
	a, aCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	b, bCSRF := addSecondOwner(t, env)

	// A section is a label, not a promise: creating one is immediate and
	// unsigned, and it stays invisible until an agreed agreement sits in it
	// (decision 8).
	section := mustCreateAgreementSection(t, env, "Money", a, aCSRF)
	if section.Count != 0 || section.Visible {
		t.Fatalf("new section = %+v, want count 0 and visible false", section)
	}
	propose := map[string]any{
		"kind": "add", "sectionId": section.ID,
		"body": "We talk about anything over $200.", "note": "",
	}

	first := mustProposalWrite(t, env, agreementProposalsPath, propose, a, aCSRF, http.StatusCreated)
	if first.Proposal.Status != "pending" {
		t.Fatalf("status = %q, want pending", first.Proposal.Status)
	}
	// The proposer signed implicitly, in the same transaction (decision 5), so
	// only the other owner is awaited -- "needs Christine", never "needs both".
	if len(first.Proposal.AwaitingNames) != 1 || first.Proposal.AwaitingNames[0] != "Christine" {
		t.Fatalf("awaitingNames = %v, want [Christine]", first.Proposal.AwaitingNames)
	}

	// 404 before 403 (decision 22): an unknown id is not somebody else's
	// proposal, and the handler has to read the row before it can know whose
	// it is.
	rec := env.authed(t, http.MethodPost, proposalPath(zeroProposalID)+"/withdraw", nil, a, aCSRF)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
	// B did not propose it and A is still an owner: 403, ahead of any 409.
	rec = env.authed(t, http.MethodPost, proposalPath(first.Proposal.ID)+"/withdraw", nil, b, bCSRF)
	assertErrorResponse(t, rec, http.StatusForbidden, "AGREEMENT_NOT_PROPOSER")
	// And the proposer's own withdraw goes through -- the other direction.
	withdrawn := mustProposalWrite(t, env, proposalPath(first.Proposal.ID)+"/withdraw", nil, a, aCSRF, http.StatusOK)
	if withdrawn.Proposal.Status != "withdrawn" {
		t.Fatalf("status = %q, want withdrawn", withdrawn.Proposal.Status)
	}
	if len(withdrawn.Agreements.Proposals) != 0 {
		t.Fatalf("proposals = %+v, want none open after the withdrawal", withdrawn.Agreements.Proposals)
	}

	second := mustProposalWrite(t, env, agreementProposalsPath, propose, a, aCSRF, http.StatusCreated)
	// Discuss parks it: still open, still answerable (decision 7).
	parked := mustProposalWrite(t, env, proposalPath(second.Proposal.ID)+"/park",
		map[string]any{"note": "next retro"}, b, bCSRF, http.StatusOK)
	if parked.Proposal.Status != "parked" || parked.Proposal.ParkNote != "next retro" {
		t.Fatalf("park = %+v, want parked carrying the note", parked.Proposal)
	}

	agreed := mustProposalWrite(t, env, proposalPath(second.Proposal.ID)+"/agree", nil, b, bCSRF, http.StatusOK)
	if agreed.Proposal.Status != "accepted" {
		t.Fatalf("status = %q, want accepted -- the second owner completes the set", agreed.Proposal.Status)
	}
	// The version, the history and the numbering all move together, off the
	// one document the write returned.
	if agreed.Agreements.Version != 2 || len(agreed.Agreements.History) != 1 {
		t.Fatalf("version = %d, history = %d, want 2 and 1",
			agreed.Agreements.Version, len(agreed.Agreements.History))
	}
	landed := findAgreement(t, agreed.Agreements, "We talk about anything over $200.")
	if landed.Number != 1 {
		t.Fatalf("number = %d, want 1 -- the first agreement in the document", landed.Number)
	}

	// Agreeing again is refused, not a no-op success: accepted means the
	// household already got what this click is asking for, so "reload, this
	// was settled" is the honest answer.
	rec = env.authed(t, http.MethodPost, proposalPath(second.Proposal.ID)+"/agree", nil, b, bCSRF)
	assertErrorResponse(t, rec, http.StatusConflict, "AGREEMENT_PROPOSAL_RESOLVED")
}

// TestStarterSetSeedsFourSectionsAndIsIdempotent covers the one write that
// answers the bare document envelope rather than a row plus a document, and
// the one that may create nothing -- which is why it is 200 and not 201, and
// why a second click is a no-op rather than a 409 (decision 17).
func TestStarterSetSeedsFourSectionsAndIsIdempotent(t *testing.T) {
	env := newTestEnv(t)
	a, aCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	addSecondOwner(t, env) // the writes are locked below two owners

	for attempt := 1; attempt <= 2; attempt++ {
		rec := env.authed(t, http.MethodPost, "/api/v1/marriage/agreements/starter-set", nil, a, aCSRF)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: status = %d, want 200 (body = %s)", attempt, rec.Code, rec.Body.String())
		}
		var out agreementsReadBody
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("attempt %d: decode starter-set: %v (body = %s)", attempt, err, rec.Body.String())
		}
		if len(out.Agreements.Sections) != 4 {
			t.Fatalf("attempt %d: %d sections, want 4 -- %+v",
				attempt, len(out.Agreements.Sections), out.Agreements.Sections)
		}
		// Sections only, never agreements (decision 17): "everything on this
		// page is here because you both agreed" has no bulk-signed exception.
		for _, s := range out.Agreements.Sections {
			if s.Count != 0 || s.Visible {
				t.Fatalf("attempt %d: section %q has count %d and visible %v, want 0 and false",
					attempt, s.Name, s.Count, s.Visible)
			}
		}
		if out.Agreements.Version != 1 {
			t.Fatalf("attempt %d: version = %d, want 1 -- a section is not an accepted change",
				attempt, out.Agreements.Version)
		}
	}
}

// TestAStaleAgreeIsRefusedAndTheProposalSurvives is the spec's "stale agree
// through two real sessions". It cannot be built from one session: two edits
// have to exist against the same wording before either lands, which is the
// race the whole feature exists to refuse (decision 13).
func TestAStaleAgreeIsRefusedAndTheProposalSurvives(t *testing.T) {
	env := newTestEnv(t)
	a, aCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	b, bCSRF := addSecondOwner(t, env)

	const original = "We talk about anything over $200."
	section := mustCreateAgreementSection(t, env, "Money", a, aCSRF)
	added := mustProposalWrite(t, env, agreementProposalsPath, map[string]any{
		"kind": "add", "sectionId": section.ID, "body": original, "note": "",
	}, a, aCSRF, http.StatusCreated)
	landed := mustProposalWrite(t, env, proposalPath(added.Proposal.ID)+"/agree", nil, b, bCSRF, http.StatusOK)
	target := findAgreement(t, landed.Agreements, original)

	// Two edits against the SAME wording, both proposed before either is
	// agreed, so both pass their propose-time target check.
	edit := func(body string) agreementProposalWriteBody {
		return mustProposalWrite(t, env, agreementProposalsPath, map[string]any{
			"kind": "edit", "targetAgreementId": target.ID,
			"previousBody": original, "body": body, "note": "",
		}, a, aCSRF, http.StatusCreated)
	}
	firstEdit := edit("We talk about anything over $300.")
	secondEdit := edit("We talk about anything over $500.")

	// B agrees the first. An accepted edit stamps the target removed and
	// inserts the new wording, so the second edit's previous_body now names
	// something that is gone.
	appliedFirst := mustProposalWrite(t, env, proposalPath(firstEdit.Proposal.ID)+"/agree", nil, b, bCSRF, http.StatusOK)
	if appliedFirst.Agreements.Version != 3 || len(appliedFirst.Agreements.History) != 2 {
		t.Fatalf("version = %d, history = %d, want 3 and 2",
			appliedFirst.Agreements.Version, len(appliedFirst.Agreements.History))
	}

	// 409 AGREEMENT_CHANGED, not 404: the proposal WAS found, only its target
	// moved. Merging the two changes would produce a document neither owner
	// agreed to.
	rec := env.authed(t, http.MethodPost, proposalPath(secondEdit.Proposal.ID)+"/agree", nil, b, bCSRF)
	assertErrorResponse(t, rec, http.StatusConflict, "AGREEMENT_CHANGED")

	// Nothing was written and nothing was hidden: the refused proposal is
	// still listed, still open, and targetChanged now says so before the next
	// click (decision 14) -- the read-side echo of the check that just fired.
	doc := mustReadAgreements(t, env, b)
	stale, ok := findProposal(doc, secondEdit.Proposal.ID)
	if !ok {
		t.Fatalf("the refused proposal is no longer listed -- proposals = %+v", doc.Proposals)
	}
	if stale.Status != "pending" || !stale.TargetChanged {
		t.Fatalf("stale proposal = %+v, want status pending with targetChanged true", stale)
	}
	if doc.Version != 3 {
		t.Fatalf("version = %d, want 3 -- the refused agree must not have moved the document", doc.Version)
	}
}

// TestProposeRefusesAnUnknownKindAndAnOversizedBody covers both refusals the
// handler answers without the service: the kind, parsed at the boundary
// (decision 21), and decodeJSONBodyLimit's 8 KiB ceiling. Neither reaches
// requireTwoOwners, which is why a one-owner env is the right fixture -- a 409
// here would mean the order of the two checks had silently swapped.
func TestProposeRefusesAnUnknownKindAndAnOversizedBody(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, agreementProposalsPath,
		map[string]any{"kind": "delete", "body": "x"}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "AGREEMENT_KIND_INVALID")

	rec = env.authed(t, http.MethodPost, agreementProposalsPath,
		map[string]any{"kind": "add", "body": strings.Repeat("x", 9*1024)}, session, csrf)
	assertErrorResponse(t, rec, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE")
}
