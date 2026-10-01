package httpadapter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// These tests pin "today" as the household's calendar day (ADR 12). They
// share one clock shape: 23:00 UTC, the hour at which a UTC server and a
// Singapore household disagree about the date.

// eveningClock is a clock stopped at the next 23:00 UTC at or after real now,
// and the UTC calendar day that instant falls on.
//
// Forward of real now, never behind it: session expiry is checked by
// Postgres's own now(), so a clock pinned to the past signs in and is then
// refused (see movableClock). At 23:00 UTC it is 07:00 the NEXT day in
// Singapore, which is the whole point: utcDay is the server's date and
// utcDay+1 is the household's.
func eveningClock() (clk *movableClock, utcDay time.Time) {
	now := time.Now().UTC()
	anchor := time.Date(now.Year(), now.Month(), now.Day(), 23, 0, 0, 0, time.UTC)
	if anchor.Before(now) {
		anchor = anchor.AddDate(0, 0, 1)
	}
	return &movableClock{now: anchor}, time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, time.UTC)
}

// monthEndClock is eveningClock on the last day of a month: 23:00 UTC, when
// it is already the 1st of the NEXT month in Singapore. utcMonth is the
// server's month; the household's is the one after.
func monthEndClock() (clk *movableClock, utcMonth time.Time) {
	return monthEndClockAfter(0)
}

// monthEndClockAfter is monthEndClock moved a whole number of months further
// on. The retro test needs one: its control looks at the month BEFORE the
// server's, and that must not be a month before the test household existed.
func monthEndClockAfter(months int) (clk *movableClock, utcMonth time.Time) {
	now := time.Now().UTC()
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	anchor := firstOfMonth.AddDate(0, 1, -1).Add(23 * time.Hour)
	if anchor.Before(now) {
		firstOfMonth = firstOfMonth.AddDate(0, 1, 0)
	}
	firstOfMonth = firstOfMonth.AddDate(0, months, 0)
	anchor = firstOfMonth.AddDate(0, 1, -1).Add(23 * time.Hour)
	return &movableClock{now: anchor}, firstOfMonth
}

// setTimezone moves the household to another zone through the API, the way
// an owner does it in Settings.
func (env *testEnv) setTimezone(t *testing.T, session, csrf *http.Cookie, zone string) {
	t.Helper()
	rec := env.authed(t, http.MethodPatch, "/api/v1/household", map[string]any{"timezone": zone}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("set time zone to %s: status = %d, body = %s", zone, rec.Code, rec.Body.String())
	}
}

// The API refuses a zone it cannot load, so the only way one reaches the
// column is around the API: a hand-edited row, or zone data that changed
// under a stored name. Every date the household would then see is a guess.
// The request fails instead, for a session and for a token alike, because
// each has its own middleware and both must set the household's day.
func TestAHouseholdWhoseTimezoneCannotBeLoadedGetsA500NotAGuessedDate(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	token := env.mustCreateToken(t, session, csrf, "script").Token

	if rec := env.authedGet(t, "/api/v1/auth/me", session); rec.Code != http.StatusOK {
		t.Fatalf("before the zone is broken, a session GET is %d, want 200", rec.Code)
	}
	if rec := env.bearer(t, http.MethodGet, "/api/v1/auth/me", nil, token); rec.Code != http.StatusOK {
		t.Fatalf("before the zone is broken, a token GET is %d, want 200", rec.Code)
	}

	if _, err := env.db.Pool().Exec(context.Background(),
		`UPDATE households SET timezone = 'Mars/Olympus_Mons' WHERE id = $1`, env.householdID); err != nil {
		t.Fatalf("break the stored zone: %v", err)
	}

	assertErrorResponse(t, env.authedGet(t, "/api/v1/auth/me", session), http.StatusInternalServerError, "INTERNAL")
	assertErrorResponse(t, env.bearer(t, http.MethodGet, "/api/v1/auth/me", nil, token), http.StatusInternalServerError, "INTERNAL")
}

// QA ISSUE-002. At 07:00 in Singapore the holding forms default to today's
// date, and the server, still on yesterday in UTC, answered "That date is in
// the future." The date a household may record a fact on is its own today.
func TestAHoldingFactDatedOnTheHouseholdsTodayIsAccepted(t *testing.T) {
	clk, utcDay := eveningClock()
	env := newTestEnvWithClock(t, clk)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	account := newHoldingAccount(t, env, session, csrf, "Brokerage", "investment")
	holding := newHolding(t, env, session, csrf, account, "D05")
	base := "/api/v1/holdings/" + holding

	// One day ahead of the server's UTC date: today for a household in
	// Singapore, tomorrow for one on UTC.
	singaporeToday := utcDay.AddDate(0, 0, 1).Format("2006-01-02")
	singaporeTomorrow := utcDay.AddDate(0, 0, 2).Format("2006-01-02")

	// The three writes a holding takes, each of which carries a date.
	writes := []struct {
		name     string
		path     string
		body     func(date string) map[string]any
		accepted int
	}{
		{"a purchase", base + "/events", func(date string) map[string]any {
			return map[string]any{"kind": "acquisition", "quantity": "10", "amountMinor": 30_000, "occurredOn": date}
		}, http.StatusCreated},
		{"a price", base + "/valuations", func(date string) map[string]any {
			return map[string]any{"unitPriceMinor": 3_100, "asOf": date}
		}, http.StatusOK},
		{"a dividend", base + "/income", func(date string) map[string]any {
			return map[string]any{"kind": "income", "amountMinor": 4_500, "receivedOn": date}
		}, http.StatusCreated},
	}

	// The control, and the reason this test can fail: the household is still
	// on UTC here, so the very date accepted below is tomorrow and refused.
	// Without it, a server that accepted any date at all would pass.
	for _, w := range writes {
		t.Run(w.name+" dated the server's tomorrow is refused while the household is on UTC", func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, w.path, w.body(singaporeToday), session, csrf)
			assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_DATE")
		})
	}

	env.setTimezone(t, session, csrf, "Asia/Singapore")

	for _, w := range writes {
		t.Run(w.name+" dated the household's today is accepted once it is on Singapore", func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, w.path, w.body(singaporeToday), session, csrf)
			if rec.Code != w.accepted {
				t.Fatalf("status = %d, want %d (body = %s)", rec.Code, w.accepted, rec.Body.String())
			}
		})
		t.Run(w.name+" dated the household's tomorrow is still refused", func(t *testing.T) {
			rec := env.authed(t, http.MethodPost, w.path, w.body(singaporeTomorrow), session, csrf)
			assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_DATE")
		})
	}

	// A token has its own middleware. hearthctl must get the household's day
	// too, not the server's.
	t.Run("a token gets the household's today as well", func(t *testing.T) {
		token := env.mustCreateToken(t, session, csrf, "script").Token
		rec := env.bearer(t, http.MethodPost, base+"/valuations",
			map[string]any{"unitPriceMinor": 3_200, "asOf": singaporeToday}, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
		}
	})
}

// QA ISSUE-003. On the 1st of the month in Singapore the ledger opened on the
// server's month, the one just ended, while the Add transaction form dated
// the new row today. The row saved and was not on the screen. With no month
// in the request, the ledger opens on the household's month.
func TestTheLedgerOpensOnTheHouseholdsMonth(t *testing.T) {
	clk, utcMonth := monthEndClock()
	env := newTestEnvWithClock(t, clk)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.setTimezone(t, session, csrf, "Asia/Singapore")

	householdMonth := utcMonth.AddDate(0, 1, 0)
	householdToday := householdMonth.Format("2006-01-02") // the 1st, in Singapore

	category, _ := env.firstExpenseCategory(t, session)
	account := env.mustCreateAccountID(t, session, csrf)
	env.mustCreateExpense(t, session, csrf, householdToday, category, account, 1_250)

	t.Run("on Singapore the default month is the new one, and today's row is in it", func(t *testing.T) {
		got := env.listTransactions(t, session, "/api/v1/transactions")
		if want := householdMonth.Format("2006-01"); got.Summary.Month != want {
			t.Errorf("summary month = %s, want the household's month %s", got.Summary.Month, want)
		}
		if got.Summary.Count != 1 || len(got.Transactions) != 1 || got.Transactions[0].OccurredOn != householdToday {
			t.Errorf("ledger = %+v, want the one expense dated %s", got, householdToday)
		}
	})

	// The control: the same request with the household on UTC opens on the
	// month the server is still in, where that row is not.
	t.Run("on UTC the default month is still the old one", func(t *testing.T) {
		env.setTimezone(t, session, csrf, "UTC")
		got := env.listTransactions(t, session, "/api/v1/transactions")
		if want := utcMonth.Format("2006-01"); got.Summary.Month != want {
			t.Errorf("summary month = %s, want the server's month %s", got.Summary.Month, want)
		}
		if got.Summary.Count != 0 || len(got.Transactions) != 0 {
			t.Errorf("ledger = %+v, want an empty month", got)
		}
	})
}

// QA ISSUE-004. On the 1st of the month in Singapore a bill paid today was
// missing from "paid this month", and its Undo with it, and the summary read
// "Nothing due this month" above a bill due that very day. Both figures are
// scoped to a month, and the month is the household's.
func TestBillsThisMonthIsTheHouseholdsMonth(t *testing.T) {
	clk, utcMonth := monthEndClock()
	env := newTestEnvWithClock(t, clk)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.setTimezone(t, session, csrf, "Asia/Singapore")

	householdToday := utcMonth.AddDate(0, 1, 0).Format("2006-01-02") // the 1st, in Singapore
	account := env.mustCreateAccountID(t, session, csrf)
	bill := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Internet", "amountMinor": 45_000, "cadence": "monthly",
		"nextDue": householdToday, "payFromAccountId": account,
	}).Bill

	list := func(t *testing.T) billsListResponseBody {
		t.Helper()
		rec := env.authedGet(t, "/api/v1/bills", session)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /bills: status = %d, body = %s", rec.Code, rec.Body.String())
		}
		return decodeBillsList(t, rec)
	}

	t.Run("a bill due today counts as due this month and is not overdue", func(t *testing.T) {
		got := list(t)
		if got.Summary.DueThisMonthMinor != 45_000 {
			t.Errorf("dueThisMonthMinor = %d, want 45000", got.Summary.DueThisMonthMinor)
		}
		if len(got.Bills) != 1 || got.Bills[0].Overdue {
			t.Errorf("bills = %+v, want the one bill, due today and not overdue", got.Bills)
		}
	})

	payment := env.mustPayBill(t, session, csrf, bill.ID, map[string]any{"paidOn": householdToday}).Payment

	t.Run("a bill paid today is listed under paid this month, where its Undo is", func(t *testing.T) {
		got := list(t)
		if len(got.PaidThisMonth) != 1 || got.PaidThisMonth[0].ID != payment.ID {
			t.Fatalf("paidThisMonth = %+v, want the payment %s", got.PaidThisMonth, payment.ID)
		}
		if got.Summary.PaidSoFarMinor != 45_000 || got.Summary.DueThisMonthMinor != 45_000 {
			t.Errorf("summary = %+v, want 45000 paid of 45000 due", got.Summary)
		}
	})

	// The control: on UTC the server is still in the month before, where
	// nothing was due and nothing was paid.
	t.Run("on UTC the same household is still in the month before", func(t *testing.T) {
		env.setTimezone(t, session, csrf, "UTC")
		got := list(t)
		if len(got.PaidThisMonth) != 0 || got.Summary.PaidSoFarMinor != 0 || got.Summary.DueThisMonthMinor != 0 {
			t.Errorf("paidThisMonth = %+v, summary = %+v, want an empty month", got.PaidThisMonth, got.Summary)
		}
	})
}

// Archiving a bill takes an instant for the stamp and a day for the row it
// answers with. The handler must not hand the service one value for both.
func TestArchivingABillStampsTheInstantAndJudgesOverdueByTheHouseholdsDay(t *testing.T) {
	clk, utcDay := eveningClock()
	env := newTestEnvWithClock(t, clk)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.setTimezone(t, session, csrf, "Asia/Singapore")

	account := env.mustCreateAccountID(t, session, csrf)
	// Due on the server's today, which is the household's yesterday.
	bill := env.mustCreateBill(t, session, csrf, map[string]any{
		"name": "Old gym", "amountMinor": 8_000, "cadence": "monthly",
		"nextDue": utcDay.Format("2006-01-02"), "payFromAccountId": account,
	}).Bill

	rec := env.authed(t, http.MethodPost, "/api/v1/bills/"+bill.ID+"/archive", nil, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	got := decodeBill(t, rec).Bill
	if !got.Overdue {
		t.Error("overdue = false, want true: the bill was due yesterday by the household's calendar")
	}
	if got.ArchivedAt == nil {
		t.Fatal("archivedAt is null on an archived bill")
	}
	// Compared as instants: the wire carries an offset, and which offset the
	// database driver hands back is not what this test is about.
	stamped, err := time.Parse(time.RFC3339, *got.ArchivedAt)
	if err != nil {
		t.Fatalf("archivedAt %q is not RFC 3339: %v", *got.ArchivedAt, err)
	}
	if !stamped.Equal(clk.Now()) {
		t.Errorf("archivedAt = %s, want the instant %s", *got.ArchivedAt, clk.Now().Format(time.RFC3339))
	}
}

// QA ISSUE-005. On 1 October in Singapore the server, still on 30 September,
// offered "Start August retro". The month a retro starts on is the earlier of
// {last month, this month} with none yet, and both of those are the
// household's.
func TestStartRetroBeginsTheMonthJustEndedInTheHouseholdsZone(t *testing.T) {
	clk, utcMonth := monthEndClockAfter(1)
	env := newTestEnvWithClock(t, clk)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// The server is on the last day of utcMonth. The household in Singapore
	// is on the 1st of the month after, so utcMonth is the month it just
	// finished and the one to look back on.
	monthJustEnded := utcMonth.Format("2006-01")
	monthBefore := utcMonth.AddDate(0, -1, 0).Format("2006-01")

	startMonth := func(t *testing.T) string {
		t.Helper()
		rec := env.authedGet(t, "/api/v1/retros", session)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /retros: status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var body retrosListWithDataBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.StartMonth == nil {
			t.Fatal("startMonth is null for a household with no retros")
		}
		return *body.StartMonth
	}

	// The control: on UTC the month still running is "this month", so the
	// one offered is the month before it.
	t.Run("on UTC the month offered is the one before the server's", func(t *testing.T) {
		if got := startMonth(t); got != monthBefore {
			t.Errorf("startMonth = %s, want %s", got, monthBefore)
		}
	})

	env.setTimezone(t, session, csrf, "Asia/Singapore")

	t.Run("on Singapore the month offered is the one that just ended there", func(t *testing.T) {
		if got := startMonth(t); got != monthJustEnded {
			t.Errorf("startMonth = %s, want %s", got, monthJustEnded)
		}
	})

	t.Run("and Start creates that month", func(t *testing.T) {
		rec := env.authed(t, http.MethodPost, "/api/v1/retros", nil, session, csrf)
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST /retros: status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var created retroDetailBody
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if created.Retro.Month != monthJustEnded {
			t.Errorf("started %s, want %s", created.Retro.Month, monthJustEnded)
		}
	})
}
