package httpadapter_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
	"testing"
	"time"
)

// --- accounts, the first capability gate, and redaction --------------------

// TestAccountsListRequiresTheMoneyCapability proves the server enforces a
// capability on its own, independent of the UI: a limited member without
// money gets 403.
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
// ABSENT, not zero: a zeroed balance would read as real, a zeroed net worth
// as "this family has nothing" -- a worse untruth than saying nothing.
//
// The redacted entry's key set is asserted exactly, not just that the
// amount keys are missing: redactedAccounts nils fields onto the full
// accountDTO, a blacklist on the field axis even though the role check
// above it is a whitelist. A blacklist fails open, so asserting the whole
// key set forces a new money-carrying field to be added here deliberately.
// Don't weaken this to checking named keys: "openingBalance" was added to
// accountDTO un-redacted after this test was written, and only the
// full-set check caught it.
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

// TestAccountErrorCodesMatchTheSpecTable pins the wire contract for the
// design doc's §6.3 table: each of these five codes is only a string
// literal in errors.go (and, for two, a second one in
// account_handlers.go); this test is what confirms they agree with each
// other and with the table.
//
// This is a contract test, not a regression test for a live breakage: today
// a wrong code costs nothing, since AccountModal falls back to a generic
// message when apiErrorMessage doesn't recognise it. The cost arrives the
// day a caller keys off one of these strings -- a typo would then fail
// silently against a suite that stayed green.
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
// reads.
//
// The clock is anchored to real now, not an absolute date: session expiry is
// checked in SQL with Postgres `now()`, not by this clock, so a test pinned
// to an absolute instant stops authenticating exactly one SessionTTL after
// it's written -- this one did.
//
// The anchor is the 15th at midday because every month has a 15th. The 28th
// isn't safe in February, the 1st sits on a timezone boundary, and AddDate
// normalises an overflowing day into the next month rather than refusing --
// so an anchor on the 31st would silently assert the wrong window.
func TestOwnerSeesTheTwelveMonthTrend(t *testing.T) {
	now := time.Now().UTC()
	anchor := time.Date(now.Year(), now.Month(), 15, 12, 0, 0, 0, time.UTC)
	thisMonth := anchor.Format("2006-01")
	oldestMonth := anchor.AddDate(0, -11, 0).Format("2006-01")

	env := newTestEnvWithClock(t, &movableClock{now: anchor})
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	env.mustCreateAccount(t, session, csrf, map[string]any{
		"nickname": "DBS Everyday", "type": "cash",
		"openingBalanceMinor": 824_055, "openingBalanceCurrency": "SGD",
		// The first of the anchor's own month: usecase/account.go refuses an
		// opening balance dated more than a day ahead of Clock.Now(), and the
		// assertions below need the account tracked from this month and no
		// earlier.
		"openingBalanceAsOf": thisMonth + "-01",
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
	if points[0].Month != oldestMonth || points[11].Month != thisMonth {
		t.Errorf("window = %s..%s, want %s..%s",
			points[0].Month, points[11].Month, oldestMonth, thisMonth)
	}
	// An account opened this month: every earlier month is a gap, and a gap
	// is a null, never a zero.
	if points[0].NetWorthMinor != nil {
		t.Errorf("%s = %d, want null -- nothing was tracked then", oldestMonth, *points[0].NetWorthMinor)
	}
	if points[11].NetWorthMinor == nil || *points[11].NetWorthMinor != 824_055 {
		t.Errorf("%s = %v, want 824055", thisMonth, points[11].NetWorthMinor)
	}
	// The account was opened this month, so the newest bar has every counted
	// account and the oldest bar is missing the only one there is -- not
	// merely unknown, but explicitly incomplete.
	if !points[11].Complete {
		t.Errorf("%s complete = false, want true -- the account is tracked by this month", thisMonth)
	}
	if points[0].Complete {
		t.Errorf("%s complete = true, want false -- the account was not open yet", oldestMonth)
	}
	if got.Summary.Trend.ChangeBasisPoints != nil {
		t.Errorf("changeBasisPoints = %d, want absent -- the month before the window is unknown",
			*got.Summary.Trend.ChangeBasisPoints)
	}

	// netWorthMinor must be PRESENT and null on a gap month, not omitted:
	// the frontend needs the slot to keep the axis aligned.
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"netWorthMinor":null`)) {
		t.Error("a gap month omits netWorthMinor entirely; it must be sent as null")
	}

	// complete needs the same byte-level pin netWorthMinor gets, for a reason
	// specific to bool: a missing key and `false` decode to the same Go zero
	// value, so a decoded field can't tell "the wire said false" from "the
	// wire said nothing" -- only the raw bytes can. An accidental
	// `,omitempty` on the Complete tag would slip past every assertion above.
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"complete":false`)) {
		t.Error(`the oldest month's complete field is missing from the wire; want a literal "complete":false`)
	}

	// Absent, not null. A decoded *int64 is nil either way, so only the raw
	// bytes can tell "we have no honest percentage" from "the percentage is
	// null" -- and 0 is a real reading here, meaning unchanged, which is why
	// the field carries omitempty and the figure above does not.
	if bytes.Contains(rec.Body.Bytes(), []byte(`"changeBasisPoints"`)) {
		t.Errorf("changeBasisPoints is present in the body; it must be omitted entirely when suppressed: %s", rec.Body.String())
	}
}

// TestALimitedMemberGetsNoTrend needs no new guard to pass -- the trend
// rides inside the already-withheld summary. It exists so a future refactor
// moving the trend to its own field or route can't leak amounts silently.
func TestALimitedMemberGetsNoTrend(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)

	rec := env.authedGet(t, "/api/v1/accounts", session)
	if bytes.Contains(rec.Body.Bytes(), []byte(`"trend"`)) {
		t.Errorf("a limited member's response carries a trend: %s", rec.Body.String())
	}
}
