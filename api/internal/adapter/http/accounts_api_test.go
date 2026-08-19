package httpadapter_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
	"testing"
	"time"
)

// --- Task 38: accounts, the first capability gate, and redaction ----------

// TestAccountsListRequiresTheMoneyCapability is the first capability gate in
// the product. Until this route existed, requireCapability was defined and
// unused, so the promise that the server enforces capabilities independently
// of the UI was vacuous.
func TestAccountsListRequiresTheMoneyCapability(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.limitedEmail, env.limitedPassword) // calendar + chores

	rec := env.authedGet(t, "/api/v1/accounts", session)
	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")
}

// TestAccountsWriteRequiresOwnership is the half the capability gate does not
// cover: a limited member who *does* hold money can read the screen and must
// not be able to change it. Kids look, parents manage.
func TestAccountsWriteRequiresOwnership(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/accounts", map[string]any{
		"nickname": "Sneaky", "type": "cash",
		"openingBalanceMinor": 100, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26",
	}, session, csrf)
	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")
}

// TestAccountsAreRedactedForALimitedMember asserts the amount fields are
// ABSENT, not zero. A zeroed balance still reads as a real one, and a zeroed
// net worth says "this family has nothing" -- a different and worse untruth
// than saying nothing.
//
// The redacted entry's key set is asserted exactly, not just that the amount
// keys happen to be missing: redactedAccounts builds the field nils onto the
// full accountDTO (account_handlers.go), which is a blacklist on the field
// axis even though the role check ten lines above it is a deliberate
// whitelist. A blacklist fails open -- add a new money-carrying field to
// accountDTO later and every limited member receives it, with nothing here
// going red, because the fields this test happened to name would still be
// absent. Asserting the whole key set instead forces exactly that addition to
// be a deliberate decision, at the one moment it matters. That is not
// hypothetical: "openingBalance" was added to accountDTO after this test was
// written, and this assertion is what caught it un-redacted. Naming the
// blacklisted fields in this comment would have needed updating too, so it
// deliberately does not.
func TestAccountsAreRedactedForALimitedMember(t *testing.T) {
	env := newTestEnv(t)
	ownerSession, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// One visible to limited members, one not.
	env.mustCreateAccount(t, ownerSession, ownerCSRF, map[string]any{
		"nickname": "OCBC Joint Savings", "type": "cash",
		"openingBalanceMinor": 4_690_000, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26", "visibleToLimitedMembers": true,
	})
	env.mustCreateAccount(t, ownerSession, ownerCSRF, map[string]any{
		"nickname": "DBS Everyday", "type": "cash",
		"openingBalanceMinor": 824_055, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26", "visibleToLimitedMembers": false,
	})

	session, _ := env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)
	rec := env.authedGet(t, "/api/v1/accounts", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, present := raw["summary"]; present {
		t.Error("summary is present for a limited member; it must be omitted entirely")
	}

	accounts, ok := raw["accounts"].([]any)
	if !ok || len(accounts) != 1 {
		t.Fatalf("accounts = %v, want exactly the one shared account", raw["accounts"])
	}
	entry := accounts[0].(map[string]any)
	if entry["nickname"] != "OCBC Joint Savings" {
		t.Errorf("nickname = %v, want the shared account", entry["nickname"])
	}

	// The exact set accountDTO produces once Balance and BalanceAsOf are
	// nilled out: both carry `omitempty`, so a nil pointer drops the key
	// entirely rather than serialising as null. ID/OwnerMembershipID/
	// OwnerName/ArchivedAt have no `omitempty` and stay present as null.
	wantKeys := []string{
		"id", "nickname", "type", "ownerMembershipId", "ownerName",
		"countTowardNetWorth", "visibleToLimitedMembers", "archivedAt",
	}
	if len(entry) != len(wantKeys) {
		t.Fatalf("redacted account has keys %v, want exactly %v", mapKeys(entry), wantKeys)
	}
	for _, k := range wantKeys {
		if _, present := entry[k]; !present {
			t.Errorf("redacted account is missing expected key %q (got %v)", k, mapKeys(entry))
		}
	}
}

// mapKeys is a decodeError-style test helper: it exists only to put a
// readable key list into a failure message, since Go maps do not stringify
// in a stable order on their own.
func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestOwnerSeesEveryAccountAndTheSummary is the control for the test above: a
// redaction test that passed because the endpoint returns nothing to anybody
// would be worthless.
func TestOwnerSeesEveryAccountAndTheSummary(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	env.mustCreateAccount(t, session, csrf, map[string]any{
		"nickname": "DBS Everyday", "type": "cash",
		"openingBalanceMinor": 824_055, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26",
	})
	env.mustCreateAccount(t, session, csrf, map[string]any{
		"nickname": "Car loan", "type": "loan",
		"openingBalanceMinor": 1_450_000, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26",
	})

	rec := env.authedGet(t, "/api/v1/accounts", session)
	var got struct {
		Accounts []struct {
			Nickname string `json:"nickname"`
			Balance  struct {
				AmountMinor int64  `json:"amountMinor"`
				Currency    string `json:"currency"`
			} `json:"balance"`
		} `json:"accounts"`
		Summary *struct {
			NetWorthMinor int64 `json:"netWorthMinor"`
			Computable    bool  `json:"computable"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(got.Accounts))
	}
	if got.Summary == nil {
		t.Fatal("summary is missing for an owner")
	}
	if !got.Summary.Computable || got.Summary.NetWorthMinor != -625_945 {
		t.Errorf("summary = %+v, want a computable net worth of -625945 (824055 - 1450000)", got.Summary)
	}
}

// TestAccountErrorCodesMatchTheSpecTable pins the wire contract for the design
// doc's own §6.3 table at the one level nothing had asserted it before: each
// of these five codes existed only as a string literal in errors.go and, for
// two of them, a second literal in account_handlers.go, with nothing
// confirming the two agreed or that either matched what the table promises.
//
// This is a contract test, not a regression test for a live breakage: today,
// a wrong code costs nothing, because AccountModal's error paragraph falls
// back to a generic message whenever apiErrorMessage doesn't recognise the
// code it was given. The cost arrives the day a caller starts keying off one
// of these strings specifically — a typo here would then fail silently,
// against a suite that stayed green.
func TestAccountErrorCodesMatchTheSpecTable(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	cases := []struct {
		name string
		body map[string]any
		code string
	}{
		{
			name: "a blank nickname",
			body: map[string]any{
				"nickname": "   ", "type": "cash",
				"openingBalanceMinor": 100, "openingBalanceCurrency": "SGD",
				"openingBalanceAsOf": "2026-07-26",
			},
			code: "NICKNAME_REQUIRED",
		},
		{
			name: "a type this API does not recognise",
			body: map[string]any{
				"nickname": "Mystery account", "type": "bitcoin_wallet",
				"openingBalanceMinor": 100, "openingBalanceCurrency": "SGD",
				"openingBalanceAsOf": "2026-07-26",
			},
			code: "INVALID_TYPE",
		},
		{
			name: "a loan entered as a negative balance",
			body: map[string]any{
				"nickname": "Car loan", "type": "loan",
				"openingBalanceMinor": -145_000, "openingBalanceCurrency": "SGD",
				"openingBalanceAsOf": "2026-07-26",
			},
			code: "INVALID_BALANCE",
		},
		{
			name: "an opening balance dated in the future",
			body: map[string]any{
				"nickname": "DBS Everyday", "type": "cash",
				"openingBalanceMinor": 100, "openingBalanceCurrency": "SGD",
				"openingBalanceAsOf": "2099-01-01",
			},
			code: "INVALID_AS_OF",
		},
		{
			name: "an owner who is not a member of this household",
			body: map[string]any{
				"nickname": "DBS Everyday", "type": "cash",
				"openingBalanceMinor": 100, "openingBalanceCurrency": "SGD",
				"openingBalanceAsOf": "2026-07-26",
				"ownerMembershipId":  "00000000-0000-0000-0000-000000000000",
			},
			code: "INVALID_OWNER",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, "/api/v1/accounts", tc.body, session, csrf)
			assertErrorResponse(t, rec, http.StatusUnprocessableEntity, tc.code)
		})
	}
}

// TestOwnerSeesTheTwelveMonthTrend pins the wire shape the Finances chart
// reads. The window is fixed by the clock, so the months are assertable.
func TestOwnerSeesTheTwelveMonthTrend(t *testing.T) {
	env := newTestEnvWithClock(t, &movableClock{now: time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)})
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	env.mustCreateAccount(t, session, csrf, map[string]any{
		"nickname": "DBS Everyday", "type": "cash",
		"openingBalanceMinor": 824_055, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-01",
	})

	rec := env.authedGet(t, "/api/v1/accounts", session)
	var got struct {
		Summary *struct {
			Trend *struct {
				Points []struct {
					Month         string `json:"month"`
					NetWorthMinor *int64 `json:"netWorthMinor"`
					Complete      bool   `json:"complete"`
				} `json:"points"`
				ChangeBasisPoints *int64 `json:"changeBasisPoints"`
			} `json:"trend"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Summary == nil || got.Summary.Trend == nil {
		t.Fatal("no trend for an owner with an account")
	}
	points := got.Summary.Trend.Points
	if len(points) != 12 {
		t.Fatalf("points = %d, want 12", len(points))
	}
	if points[0].Month != "2025-08" || points[11].Month != "2026-07" {
		t.Errorf("window = %s..%s, want 2025-08..2026-07", points[0].Month, points[11].Month)
	}
	// An account opened this month: every earlier month is a gap, and a gap
	// is a null, never a zero.
	if points[0].NetWorthMinor != nil {
		t.Errorf("2025-08 = %d, want null -- nothing was tracked then", *points[0].NetWorthMinor)
	}
	if points[11].NetWorthMinor == nil || *points[11].NetWorthMinor != 824_055 {
		t.Errorf("2026-07 = %v, want 824055", points[11].NetWorthMinor)
	}
	if got.Summary.Trend.ChangeBasisPoints != nil {
		t.Errorf("changeBasisPoints = %d, want absent -- June is unknown",
			*got.Summary.Trend.ChangeBasisPoints)
	}

	// netWorthMinor must be PRESENT and null on a gap month, not omitted:
	// the frontend needs the slot to keep the axis aligned.
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"netWorthMinor":null`)) {
		t.Error("a gap month omits netWorthMinor entirely; it must be sent as null")
	}

	// Absent, not null. A decoded *int64 is nil either way, so only the raw
	// bytes can tell "we have no honest percentage" from "the percentage is
	// null" -- and 0 is a real reading here, meaning unchanged, which is why
	// the field carries omitempty and the figure above does not.
	if bytes.Contains(rec.Body.Bytes(), []byte(`"changeBasisPoints"`)) {
		t.Errorf("changeBasisPoints is present in the body; it must be omitted entirely when suppressed: %s", rec.Body.String())
	}
}

// TestALimitedMemberGetsNoTrend needs no new guard to pass, and that is the
// point: the trend rides inside the summary, which is already withheld whole.
// The test exists so that a later refactor moving the trend to its own field
// or its own route cannot leak amounts without going red.
func TestALimitedMemberGetsNoTrend(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)

	rec := env.authedGet(t, "/api/v1/accounts", session)
	if bytes.Contains(rec.Body.Bytes(), []byte(`"trend"`)) {
		t.Errorf("a limited member's response carries a trend: %s", rec.Body.String())
	}
}
