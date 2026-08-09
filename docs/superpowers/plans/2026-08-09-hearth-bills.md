# Bills Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Slice 2's fifth and last feature — the Bills & subscriptions page, its Add-bill modal, a payment that writes a real expense into the ledger, undo, and Overview's "Next bill" card, per `docs/superpowers/specs/2026-08-09-hearth-bills-design.md`.

**Architecture:** Clean architecture as everywhere in this repo. `internal/domain/bill.go` holds the cadence arithmetic as pure functions over values — `NextDue` and its month-end clamp are the load-bearing part. `internal/usecase/bill.go` composes the screen through one new narrow port (`BillRepository`) plus the household, FX and account-lookup ports it already needs. `internal/adapter/postgres` implements it with sqlc-generated queries, and owns the two multi-row writes (`RecordPayment`, `UndoPayment`) as real database transactions. `internal/adapter/http` gates every route `money` + owner, reads included. The React frontend renders one `GET /bills` response through one hook.

**Tech Stack:** Go 1.25 / chi / pgx / sqlc / goose / testcontainers; React + TypeScript + TanStack Router + TanStack Query + zod + Vitest.

## Global Constraints

Copied from the spec and `CLAUDE.md`; every task's requirements include these.

- Money is `int64` minor units + ISO 4217 code, everywhere. No `float64` in a monetary path, on either side of the stack. Normalising a cadence multiplies up to an annual figure and divides **exactly once**, at the end.
- **A bill has no currency of its own.** It is denominated in its pay-from account's currency, because `TransactionService.Create` already forces that on every expense (`api/internal/usecase/transaction.go:232`). Only cross-bill totals convert, and they convert per bill then add (`docs/LEARNING.md` pattern 12).
- **Nothing in this feature runs on a clock.** No scheduler, no cron, no due-date job, no automatic payment. Every payment is written because a person clicked. `autopay` changes no behaviour at all — it drives a badge, a count, and the wording of the overdue state. Copy must not imply otherwise; see the copy table below.
- Reads and writes are both gated `money` capability **and** owner, the Transactions/Budget/Goals shape.
- Every 2xx except 204 carries a JSON body; `apiFetch` throws on an ok response it cannot parse. `DELETE` answers 204 with no body, the one exemption (`handleDeleteTransaction`'s own comment).
- Fail closed on values not constructed here: a `switch` over a wire or database value needs a refusing `default`.
- Adapters map missing rows to `domain.ErrNotFound`; no `pgx` type crosses out of `adapter/postgres`.
- `make lint-arch` applies to test files too: `domain` imports stdlib only; `usecase` adds `domain`.
- No service takes an actor parameter. `today` is always a parameter, never `time.Now()` inside a service.
- Frontend tests stub the network with `web/src/test/fetchStub.ts`'s `stubFetchRoutes`, which throws on an unregistered request.
- Commit messages: conventional prefixes (`feat:`, `test:`, `refactor:`, `docs:`).
- Run the Go suite from `api/`: `go test ./...` (needs a Docker socket; see `docs/HANDOVER.md` §2). Frontend: `cd web && npx vitest run`. Regenerate typed queries with `make sqlc`.
- **`00008` is the first migration since the `make up` skip was recorded.** Compose only re-evaluates `depends_on: migrate` when it recreates `api`, so a stack left running keeps its already-succeeded `migrate` container. Run `make down && make up`, or an explicit `make migrate`, before testing anything by hand.
- **Out of scope, do not build:** any scheduler, bill due reminder emails (the Settings toggle stays dead), autopay that pays anything, calendar bill dates, per-occurrence skip or re-date, CSV export, a fifth cadence.

### The pinned formulas (spec, verbatim contract)

**"Today" is `deps.Clock.Now()`, resolved in the handler and passed down**; month arithmetic truncates in UTC, as `domain/budget.go` already does.

| Figure | Formula |
|---|---|
| Due this month | `SUM(bill_payments.amount_minor)` where `due_on` in month **plus** `SUM(bills.amount_minor)` over unarchived bills whose `next_due` is in month |
| Paid so far | `SUM(bill_payments.amount_minor)` where `due_on` in month |
| Next due | earliest non-NULL `next_due` over unarchived bills, with that bill's name and amount; **overdue** when before today; none → omitted, never a zero |
| `N of M on autopay` | `M` = unarchived bills, `N` = those with `autopay`; `M = 0` hides the line |
| Due soon (list) | unarchived, non-NULL `next_due`, **within 30 days inclusive or already past**, by `next_due` ascending, ties by name |
| Later (list) | every remaining unarchived bill, same ordering — those due beyond 30 days, **and settled one-offs** (`NextDue == nil`, `Settled` true), which render "Settled" where a date would go. Due soon and Later together account for every unarchived bill, so nothing the header counts is missing from the page |
| Paid this month (list) | `bill_payments` with `due_on` in month, newest `paid_on` first, ties by bill name |
| Subscriptions per year | over unarchived `is_subscription` bills: `monthly × 12`, `quarterly × 4`, `yearly × 1`; `one_off` excluded |
| Subscriptions per month | the annual figure ÷ 12, floored |
| Next bill (Overview) | the same figure as *Next due* |

**The mockup's own figures are not reproducible under these formulas and that is deliberate.** `Due this month S$918.68` includes a bill dated 5 August; `Paid so far S$316.98` contradicts its own list, which sums to 572.20. Do not "correct" the arithmetic back toward the picture.

### The copy the design asserts and this feature does not ship

| Design | Ships as |
|---|---|
| "Mark as automatically paid — otherwise you'll get a reminder" (modal toggle) | "The bank pays this one — we'll still ask you to confirm it went out" |
| "All bills covered · Nothing needs manual payment this month" | "All caught up — everything due in August is paid. Next bill: school fees, 15 Sep." |
| "S$850.80/year · last reviewed Mar 2026" | "S$850.80/year" — no review date ships; nothing can set one |
| (no overdue state drawn) | autopay: "Should have gone out on 24 Jul — confirm it did"; manual: "Overdue since 24 Jul" |

### Two deviations from the spec, deliberate and visible

The spec is approved; these two implement it more faithfully than its own sketch did. Anyone reviewing a task against the spec will meet them, so they are stated once here rather than argued twice.

1. **`bills` carries `due_anchor_day smallint`, which the spec's schema does not.** Without it, clamping is one-way and permanent: 31 Jan clamps to 28 Feb, and the following month advances from *28* to 28 Mar, so a bill due on the 31st silently becomes a bill due on the 28th forever after its first February. The anchor is the day the household actually chose; each advance clamps the anchor to the destination month instead of clamping the clamped value. 31 Jan → 28 Feb → **31 Mar**. The column is set from `next_due` at create, and reset whenever `next_due` is patched — an explicit edit is the household choosing a new anchor.

2. **`PATCH /bills/{id}` carries no `archivedAt` field.** Archive and restore are their own routes, the reasoning `router.go:250` already records for accounts, categories and goals: if archiving were patchable, an ordinary rename that happened to include the field would archive the bill as a side effect of saving a name. The spec's API table already says this; it is restated here because the spec's `PATCH` row and its archive row sit far apart.

---

### Task 1: Migration `00008_bills.sql`

**Files:**
- Create: `api/migrations/00008_bills.sql`
- Test: `api/internal/adapter/postgres/schema_test.go` (extend)

**Interfaces:**
- Produces: tables `bills` and `bill_payments` exactly as below. Every later task's SQL relies on these names, constraints and the `due_anchor_day` column.

- [ ] **Step 1: Write the migration**

```sql
-- +goose Up

-- bills is one household's recurring fixed costs. Unlike goals (00007), a bill
-- carries no currency column: it is denominated in its pay-from account's
-- currency, because TransactionService.Create already forces an expense's
-- currency to its from-account's (usecase/transaction.go:232). A currency
-- here would be overwritten the moment a payment wrote its transaction, and
-- the two would disagree in the meantime. Do not "fix" this by adding one.
CREATE TABLE bills (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id          uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name                  text        NOT NULL,
    amount_minor          bigint      NOT NULL CHECK (amount_minor > 0),
    cadence               text        NOT NULL
                                      CHECK (cadence IN ('one_off','monthly','quarterly','yearly')),
    -- NULL only for a settled one-off: paid, and with no next date. A settled
    -- one-off is NOT auto-archived -- that would hide a record the household
    -- may still want to see.
    next_due              date,
    -- The day of the month the household actually chose, kept apart from
    -- next_due because clamping is lossy. 31 Jan clamps to 28 Feb; advancing
    -- from 28 would give 28 Mar and the bill would have silently moved off the
    -- 31st forever. Each advance clamps THIS value to the destination month,
    -- so 31 Jan -> 28 Feb -> 31 Mar. Set from next_due at create, reset when
    -- next_due is patched.
    due_anchor_day        smallint    NOT NULL CHECK (due_anchor_day BETWEEN 1 AND 31),
    category_id           uuid        REFERENCES categories(id) ON DELETE SET NULL,
    -- NOT NULL: it supplies the currency as well as the account the expense
    -- leaves. No ON DELETE clause -- accounts are never deleted, only
    -- archived, the same reason goals carries none.
    pay_from_account_id   uuid        NOT NULL REFERENCES accounts(id),
    -- Optional. Its absence is why BudgetByPerson grows an Unattributed row
    -- (spec decision 8) rather than silently dropping the spend, which is what
    -- usecase/budget.go:252 does today for any transaction with no payer.
    paid_by_membership_id uuid        REFERENCES memberships(id) ON DELETE SET NULL,
    -- Display only. Nothing in this product pays a bill by itself: there is no
    -- scheduler anywhere in this codebase, and Budget decision 1 and Goals
    -- decision 4 both refused to invent one. This flag drives a badge, a
    -- count, and the wording of the overdue state.
    autopay               boolean     NOT NULL DEFAULT false,
    -- Set by the household, never inferred from the category: categories are
    -- editable and shared with transactions and budgets, so renaming one would
    -- silently empty the subscriptions panel.
    is_subscription       boolean     NOT NULL DEFAULT false,
    -- A bill is archived, never deleted: bill_payments references it. The
    -- accounts/categories/goals precedent, for the same reason.
    archived_at           timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    -- An archived bill still occupies its name, exactly as an archived goal or
    -- category does. A collision with one offers restore rather than a bare
    -- 409 (see the HTTP task).
    UNIQUE (household_id, name),
    -- A settled one-off is the only bill without a next date. Anything else
    -- with a NULL next_due is a bug that would vanish from every list.
    CONSTRAINT only_a_one_off_has_no_next_due CHECK (
        next_due IS NOT NULL OR cadence = 'one_off'
    )
);

CREATE INDEX bills_household_idx ON bills (household_id) WHERE archived_at IS NULL;

-- bill_payments is the record of what was actually paid, and the only place a
-- past occurrence exists at all: bills carries one date forward, never a
-- history. amount_minor is what was paid, which may differ from the bill's own
-- amount -- utilities vary, and the Mark-paid modal lets the figure be edited.
CREATE TABLE bill_payments (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    bill_id        uuid        NOT NULL REFERENCES bills(id),
    household_id   uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    -- Which occurrence this settles -- the bill's next_due at the moment of
    -- paying, NOT the date it was paid on. Every month-scoped figure keys off
    -- this column, so a bill paid late still counts against the month it was
    -- due in.
    due_on         date        NOT NULL,
    paid_on        date        NOT NULL,
    amount_minor   bigint      NOT NULL CHECK (amount_minor > 0),
    -- SET NULL rather than CASCADE: deleting the expense from the Transactions
    -- page must not erase the household's record that the bill was paid. The
    -- payment row's own amount_minor is what survives.
    transaction_id uuid        REFERENCES transactions(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    -- Belt and braces beside BillService's own check: a double-clicked Mark
    -- paid cannot write two payments for one occurrence.
    UNIQUE (bill_id, due_on)
);

-- The month figures walk one household's payments by the date they were DUE,
-- not the date they were paid.
CREATE INDEX bill_payments_household_due_idx ON bill_payments (household_id, due_on);

-- +goose Down
DROP TABLE bill_payments;
DROP TABLE bills;
```

- [ ] **Step 2: Extend the schema test**

Add to `api/internal/adapter/postgres/schema_test.go`, in the same style as the existing table assertions:

```go
func TestBillsSchema(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	t.Run("only a one-off may have a NULL next_due", func(t *testing.T) {
		h, acct := seedHouseholdAndAccount(t, ctx, db)
		_, err := db.Pool().Exec(ctx, `
			INSERT INTO bills (household_id, name, amount_minor, cadence, next_due,
			                   due_anchor_day, pay_from_account_id)
			VALUES ($1, 'Broken', 1000, 'monthly', NULL, 1, $2)`, h, acct)
		if err == nil {
			t.Fatal("expected only_a_one_off_has_no_next_due to refuse a monthly bill with no next date")
		}
	})

	t.Run("one occurrence can be paid only once", func(t *testing.T) {
		h, acct := seedHouseholdAndAccount(t, ctx, db)
		bill := insertBill(t, ctx, db, h, acct, "SP utilities", "monthly", "2026-08-08")
		pay := func() error {
			_, err := db.Pool().Exec(ctx, `
				INSERT INTO bill_payments (bill_id, household_id, due_on, paid_on, amount_minor)
				VALUES ($1, $2, '2026-08-08', '2026-08-08', 14230)`, bill, h)
			return err
		}
		if err := pay(); err != nil {
			t.Fatalf("first payment: %v", err)
		}
		if err := pay(); err == nil {
			t.Fatal("expected UNIQUE (bill_id, due_on) to refuse a second payment of one occurrence")
		}
	})
}
```

Write `seedHouseholdAndAccount` and `insertBill` beside the existing helpers in that file if no equivalent exists; reuse the existing ones if they do.

- [ ] **Step 3: Run the migration and the test**

Run: `cd api && go test ./internal/adapter/postgres/ -run TestBillsSchema -v`
Expected: PASS. If the migration itself is malformed, testcontainers fails at goose with the SQL error quoted.

- [ ] **Step 4: Commit**

```bash
git add api/migrations/00008_bills.sql api/internal/adapter/postgres/schema_test.go
git commit -m "feat(bills): bills and bill_payments tables"
```

---

### Task 2: Domain — cadence, the due-date arithmetic, and the month-end clamp

**Files:**
- Create: `api/internal/domain/bill.go`
- Create: `api/internal/domain/bill_test.go`
- Modify: `api/internal/domain/errors.go`

**Interfaces:**
- Produces: `domain.Cadence` with `ParseCadence`, `MonthsPerPeriod`, `PeriodsPerYear`; `domain.Bill`; `domain.BillPayment`; `domain.NextDue(c Cadence, from time.Time, anchorDay int) (time.Time, bool)`; `domain.IsOverdue(nextDue, today time.Time) bool`; `domain.AnnualEquivalentMinor(c Cadence, amountMinor int64) (int64, bool)`. Tasks 4–8 and 10 all call these by these names.

- [ ] **Step 1: Write the failing tests**

Create `api/internal/domain/bill_test.go`:

```go
package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseCadenceRefusesAnythingElse(t *testing.T) {
	for _, ok := range []string{"one_off", "monthly", "quarterly", "yearly"} {
		if _, err := domain.ParseCadence(ok); err != nil {
			t.Fatalf("ParseCadence(%q): %v", ok, err)
		}
	}
	// The default arm is the point: this value arrives from a database column
	// and from a request body.
	if _, err := domain.ParseCadence("weekly"); !errors.Is(err, domain.ErrUnknownCadence) {
		t.Fatalf("ParseCadence(\"weekly\") = %v, want ErrUnknownCadence", err)
	}
}

// The clamp is the whole reason NextDue exists. Go's own AddDate(0,1,0) on
// 31 January returns 3 March.
func TestNextDueClampsToTheLastDayOfAShortMonth(t *testing.T) {
	cases := []struct {
		name      string
		cadence   domain.Cadence
		from      string
		anchorDay int
		want      string
	}{
		{"31 Jan monthly lands on 28 Feb", domain.CadenceMonthly, "2026-01-31", 31, "2026-02-28"},
		{"29 Feb exists in a leap year", domain.CadenceMonthly, "2028-01-31", 31, "2028-02-29"},
		// The anchor is what makes this one right. Advancing from the clamped
		// 28 Feb would give 28 March and the bill would have moved off the
		// 31st permanently.
		{"28 Feb advances back to 31 Mar, not 28 Mar", domain.CadenceMonthly, "2026-02-28", 31, "2026-03-31"},
		{"an ordinary month is untouched", domain.CadenceMonthly, "2026-08-08", 8, "2026-09-08"},
		{"quarterly crosses three months", domain.CadenceQuarterly, "2026-11-30", 30, "2027-02-28"},
		{"yearly crosses a year", domain.CadenceYearly, "2026-03-15", 15, "2027-03-15"},
		{"29 Feb yearly clamps to 28 Feb", domain.CadenceYearly, "2028-02-29", 29, "2029-02-28"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := domain.NextDue(c.cadence, day(c.from), c.anchorDay)
			if !ok {
				t.Fatal("NextDue reported no next date for a recurring cadence")
			}
			if !got.Equal(day(c.want)) {
				t.Fatalf("NextDue = %s, want %s", got.Format("2006-01-02"), c.want)
			}
		})
	}
}

func TestNextDueHasNoNextDateForAOneOff(t *testing.T) {
	if _, ok := domain.NextDue(domain.CadenceOneOff, day("2026-08-08"), 8); ok {
		t.Fatal("a one-off has no next due date")
	}
}

// It advances from the date passed in, never from today: a bill paid three
// days late must not shift its due date three days every month.
func TestNextDueAdvancesFromTheDueDateNotThePaidDate(t *testing.T) {
	got, _ := domain.NextDue(domain.CadenceMonthly, day("2026-08-08"), 8)
	if !got.Equal(day("2026-09-08")) {
		t.Fatalf("NextDue = %s, want 2026-09-08", got.Format("2006-01-02"))
	}
}

func TestIsOverdue(t *testing.T) {
	today := day("2026-08-09")
	if !domain.IsOverdue(day("2026-08-08"), today) {
		t.Fatal("yesterday is overdue")
	}
	// Due today is not overdue: the household has the whole day to pay it.
	if domain.IsOverdue(today, today) {
		t.Fatal("a bill due today is not overdue")
	}
	if domain.IsOverdue(day("2026-08-10"), today) {
		t.Fatal("tomorrow is not overdue")
	}
}

// Integer-first: multiply up to a year, divide exactly once at the end. The
// 50/30/20 budget template shipped a float multiply that drifted a minor unit
// on a real figure (docs/LEARNING.md, Domain and money catalogue).
func TestAnnualEquivalentMultipliesAndNeverDivides(t *testing.T) {
	cases := []struct {
		cadence domain.Cadence
		minor   int64
		want    int64
		ok      bool
	}{
		{domain.CadenceMonthly, 1998, 23976, true},
		{domain.CadenceQuarterly, 5000, 20000, true},
		{domain.CadenceYearly, 12000, 12000, true},
		{domain.CadenceOneOff, 9999, 0, false},
	}
	for _, c := range cases {
		got, ok := domain.AnnualEquivalentMinor(c.cadence, c.minor)
		if ok != c.ok || got != c.want {
			t.Fatalf("AnnualEquivalentMinor(%s, %d) = (%d, %v), want (%d, %v)",
				c.cadence, c.minor, got, ok, c.want, c.ok)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd api && go test ./internal/domain/ -run 'TestParseCadence|TestNextDue|TestIsOverdue|TestAnnualEquivalent' -v`
Expected: FAIL — `undefined: domain.ParseCadence` and the rest.

- [ ] **Step 3: Add the sentinel error**

In `api/internal/domain/errors.go`, in the money/bills group:

```go
	// Bills. ErrUnknownCadence is returned for a cadence this code did not
	// construct -- it arrives from a database column and from a request body,
	// so both layers refuse it, the same rule ParseTransactionKind and
	// ParseContributionSource already follow.
	ErrUnknownCadence      = errors.New("unknown bill cadence")
	ErrBillNameRequired    = errors.New("a bill name is required")
	ErrBillAmountNotPositive = errors.New("a bill amount must be positive")
	// ErrBillNameTaken is UNIQUE (household_id, name) on bills, translated the
	// same way ErrCategoryNameTaken and ErrGoalNameTaken are. An archived bill
	// still holds its name, so the HTTP layer offers restore rather than a
	// bare 409.
	ErrBillNameTaken = errors.New("bill name taken")
```

- [ ] **Step 4: Write the implementation**

Create `api/internal/domain/bill.go`:

```go
package domain

import (
	"fmt"
	"time"
)

// Cadence is how often a bill repeats. It arrives from a database column and
// from a request body, so ParseCadence refuses anything else. A fifth cadence
// needs a migration as well as a case here -- both layers refusing an unknown
// value is the house pattern (transactions.kind, goal_contributions.source).
type Cadence string

const (
	CadenceOneOff    Cadence = "one_off"
	CadenceMonthly   Cadence = "monthly"
	CadenceQuarterly Cadence = "quarterly"
	CadenceYearly    Cadence = "yearly"
)

func ParseCadence(s string) (Cadence, error) {
	switch Cadence(s) {
	case CadenceOneOff:
		return CadenceOneOff, nil
	case CadenceMonthly:
		return CadenceMonthly, nil
	case CadenceQuarterly:
		return CadenceQuarterly, nil
	case CadenceYearly:
		return CadenceYearly, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownCadence, s)
	}
}

// MonthsPerPeriod is how many calendar months one period spans. A one-off has
// no period at all and returns 0, which is why NextDue refuses it rather than
// advancing by nothing and returning the same date forever.
func (c Cadence) MonthsPerPeriod() int {
	switch c {
	case CadenceMonthly:
		return 1
	case CadenceQuarterly:
		return 3
	case CadenceYearly:
		return 12
	default:
		return 0
	}
}

// PeriodsPerYear is how many times a cadence recurs in a year. Used only for
// the subscriptions rollup, which multiplies rather than divides.
func (c Cadence) PeriodsPerYear() int {
	switch c {
	case CadenceMonthly:
		return 12
	case CadenceQuarterly:
		return 4
	case CadenceYearly:
		return 1
	default:
		return 0
	}
}

type Bill struct {
	ID                 string
	HouseholdID        string
	Name               string
	Amount             Money
	Cadence            Cadence
	NextDue            *time.Time // nil only for a settled one-off
	DueAnchorDay       int
	CategoryID         string // "" when uncategorised, the ports.go NULL convention
	PayFromAccountID   string
	PaidByMembershipID string // "" when unattributed
	Autopay            bool
	IsSubscription     bool
	ArchivedAt         *time.Time
}

func (b Bill) IsArchived() bool { return b.ArchivedAt != nil }

type BillPayment struct {
	ID            string
	BillID        string
	HouseholdID   string
	DueOn         time.Time
	PaidOn        time.Time
	Amount        Money
	TransactionID string // "" once the ledger row has been deleted
}

// NextDue advances a due date by one period of the cadence, clamping to the
// last day of the destination month.
//
// The clamp is why this function exists. Go's time.Time.AddDate(0, 1, 0) on
// 31 January returns 3 March -- it normalises "31 February" forward instead of
// refusing it -- so a bill due on the 31st would walk off the end of every
// short month. 31 Jan -> 28 Feb (29 in a leap year) -> 31 Mar is what a
// household means.
//
// anchorDay, not from.Day(), is what gets clamped. Clamping the clamped value
// is one-way: 31 Jan lands on 28 Feb, and advancing from 28 would give 28
// March, so a bill due on the 31st would silently become a bill due on the
// 28th forever after its first February.
//
// It advances from `from`, never from today: a bill paid three days late must
// not shift its due date three days every month.
//
// ok is false for a one-off, which has no next date at all.
func NextDue(c Cadence, from time.Time, anchorDay int) (time.Time, bool) {
	months := c.MonthsPerPeriod()
	if months == 0 {
		return time.Time{}, false
	}
	// Move to the first of the destination month, so the day never
	// participates in the month arithmetic and cannot overflow it.
	first := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, months, 0)
	day := anchorDay
	if last := lastDayOf(first.Year(), first.Month()); day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, time.UTC), true
}

// lastDayOf is the zeroth day of the following month, which time.Date
// normalises backwards to the previous month's last day. This is the one place
// that normalisation is exactly what is wanted.
func lastDayOf(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// IsOverdue reports whether an unpaid due date has passed. A bill due today is
// not overdue: the household has the whole day to pay it.
func IsOverdue(nextDue, today time.Time) bool {
	return startOfDay(nextDue).Before(startOfDay(today))
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// AnnualEquivalentMinor is what a bill costs in a year at its cadence. It only
// ever multiplies: the subscriptions panel divides exactly once, at the very
// end, when it turns the annual total into a monthly one. ok is false for a
// one-off, which is not a recurring cost and is excluded from the rollup.
func AnnualEquivalentMinor(c Cadence, amountMinor int64) (int64, bool) {
	periods := c.PeriodsPerYear()
	if periods == 0 {
		return 0, false
	}
	return amountMinor * int64(periods), true
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd api && go test ./internal/domain/ -run 'TestParseCadence|TestNextDue|TestIsOverdue|TestAnnualEquivalent' -v`
Expected: PASS, all cases.

- [ ] **Step 6: Mutation-check the clamp — this is the plan's designated mutation**

Break it on purpose. In `NextDue`, replace the clamped body with Go's own arithmetic:

```go
	return from.AddDate(0, months, 0), true
```

Run: `cd api && go test ./internal/domain/ -run TestNextDueClamps -v`
Expected: **FAIL**, with `NextDue = 2026-03-03, want 2026-02-28`.

Now restore the real implementation and run it again. Expected: PASS. A test that has not been seen red is not protecting anything — five tests in this project passed against deliberately broken code before this rule existed (`docs/LEARNING.md`).

- [ ] **Step 7: Commit**

```bash
git add api/internal/domain/bill.go api/internal/domain/bill_test.go api/internal/domain/errors.go
git commit -m "feat(bills): cadence arithmetic with the month-end clamp"
```

---

### Task 3: Ports and records

**Files:**
- Modify: `api/internal/usecase/ports.go`

**Interfaces:**
- Produces: `usecase.BillRecord`, `usecase.BillPaymentRecord`, `usecase.NewBillRow`, `usecase.PaymentWrite`, and the `usecase.BillRepository` interface. Task 4 and Task 5 implement it; Tasks 6 and 7 consume it. `BillPatch` is **not** here — it is a usecase-layer type Task 6 declares in `internal/usecase/bill.go`, exactly as `GoalUpdate` lives in `usecase/goal.go`.

- [ ] **Step 1: Add the records and the port**

Append to `api/internal/usecase/ports.go`, after the goal section:

```go
// BillRecord is a bill joined to the names the screen displays -- its category
// and its pay-from account's nickname. Same shape and same reason as
// AccountView and TransactionView above: every consumer of the list wants the
// names, and re-reading them per row is a query per row.
//
// Bill.Amount carries the PAY-FROM ACCOUNT's currency, populated by the
// repository from the same account join that supplies AccountName. A bill has
// no currency column of its own (00008_bills.sql says why), and
// TransactionService.Create already forces an expense's currency to its
// from-account's (usecase/transaction.go:232) -- so the account is the single
// source, and there is deliberately no second Currency field here that could
// disagree with it.
type BillRecord struct {
	Bill         domain.Bill
	CategoryName string
	AccountName  string
}

// BillPaymentRecord is one settled occurrence joined to its bill's name and
// autopay flag, which is what the "Paid this month" list renders ("Singtel
// fibre · Internet · autopay · DBS").
//
// ListPayments populates both joined fields. RecordPayment populates BillName
// only and leaves Autopay false: its caller has just read the whole bill and
// already holds the flag, so joining it back would be a second read of
// something the service is looking at.
type BillPaymentRecord struct {
	Payment  domain.BillPayment
	BillName string
	Autopay  bool
}

// NewBillRow is Create's input. DueAnchorDay is derived by the service from
// NextDue, never supplied by a caller: an anchor that disagreed with the first
// due date would drift on the very first advance. The service derives it the
// same way on any update that moves NextDue, so the one-line calendar
// computation lives in one layer rather than two.
type NewBillRow struct {
	HouseholdID        string
	Name               string
	AmountMinor        int64
	Cadence            domain.Cadence
	NextDue            time.Time
	DueAnchorDay       int
	CategoryID         string
	PayFromAccountID   string
	PaidByMembershipID string
	Autopay            bool
	IsSubscription     bool
}

// PaymentWrite is everything RecordPayment needs to write all three rows. The
// service assembles it; the repository does not look anything up.
//
// Currency is the pay-from account's, resolved by the service through
// AccountLookup. Description is the bill's name, so the ledger row is
// recognisable as the bill's own -- which is what makes a household's
// accidental duplicate entry visible rather than invisible.
type PaymentWrite struct {
	HouseholdID        string
	BillID             string
	DueOn              time.Time
	PaidOn             time.Time
	AmountMinor        int64
	Currency           string
	Description        string
	CategoryID         string
	PayFromAccountID   string
	PaidByMembershipID string
	// NextDue is what bills.next_due becomes, already computed by
	// domain.NextDue. nil settles a one-off.
	NextDue *time.Time
}

// BillRepository is one household's bills and their payment history.
//
// Two contracts here are load-bearing and neither is enforced by the database:
//
//   - bill_payments has no constraint tying its household_id to its bill's, so
//     a row could in principle carry a household_id that disagrees with the
//     bill it names. Every method that reads or writes a payment must filter
//     by household_id AND bill_id together, never by payment id alone, or a
//     payment leaks across households. This is the GoalRepository contract,
//     for the same reason.
//
//   - MonthTotals cannot be computed from bills alone. A monthly bill paid on
//     8 July has next_due = 8 August, so a query filtering bills.next_due into
//     the month misses every bill already paid -- which is the entire "paid so
//     far" half of the figure. The implementation must union bill_payments by
//     due_on with unpaid bills by next_due. The naive query passes review and
//     returns a wrong number.
//
//     The two halves filter archived bills differently, on purpose. The unpaid
//     half excludes an archived bill: a bill nobody intends to pay again is
//     not an obligation. The paid half includes it: the money left the
//     household, and archiving a bill afterwards must not retroactively empty
//     the month it was paid in. A reviewer meeting this asymmetry cold will
//     read it as a bug, which is why it is written here.
type BillRepository interface {
	// List returns one household's bills with their category and account
	// names. includeArchived is a UNION, not a filter swap: false returns the
	// live bills, true returns the live ones AND the archived ones together,
	// each carrying its own ArchivedAt. That is the AccountRepository.List and
	// GoalRepository.List contract; do not implement it as "archived instead".
	List(ctx context.Context, householdID string, includeArchived bool) ([]BillRecord, error)
	// Get reports domain.ErrNotFound when no bill with this id exists in this
	// household -- including when one exists in a different household, which
	// must be indistinguishable from not existing at all.
	Get(ctx context.Context, householdID, billID string) (BillRecord, error)
	// Create writes one row. A name colliding with UNIQUE (household_id, name)
	// -- archived rows included -- surfaces as domain.ErrBillNameTaken.
	Create(ctx context.Context, in NewBillRow) (BillRecord, error)
	// Update replaces every mutable column, DueAnchorDay included. BillService
	// is what turns a partial PATCH into a complete domain.Bill; this port
	// never merges -- the AccountRepository.Update and GoalRepository.Update
	// contract, for the same reason: conditional SQL in the adapter is a
	// second place for the update rules to live. Same collision contract as
	// Create.
	Update(ctx context.Context, b domain.Bill) (BillRecord, error)
	// SetArchived stamps archived_at with at, or clears it when archived is
	// false, and returns the bill as it now stands. It returns the record
	// rather than a bare error because every 2xx except 204 carries a JSON
	// body in this product, so an error-only signature would force the archive
	// handler into a second Get purely to build its response. `at` is supplied
	// by the caller rather than read with time.Now() inside the adapter, the
	// AccountRepository.SetArchived convention.
	SetArchived(ctx context.Context, householdID, billID string, archived bool, at time.Time) (BillRecord, error)
	// RecordPayment writes the bill_payments row, the expense transaction and
	// the advanced next_due in ONE database transaction. A bill left advanced
	// with no payment, or a payment with no expense, is not a state this port
	// can produce. An occurrence already paid surfaces as
	// domain.ErrAlreadyExists, from UNIQUE (bill_id, due_on).
	RecordPayment(ctx context.Context, in PaymentWrite) (BillPaymentRecord, error)
	// UndoPayment deletes the payment, deletes its transaction when the link
	// still points at one, and rewinds next_due to the payment's due_on -- in
	// ONE database transaction, all three or none.
	//
	// It refuses any payment that is not the bill's most recent, with
	// domain.ErrForbidden: undoing an older one would rewind next_due behind a
	// period that is still paid, and the screen would show a due date for
	// money already spent.
	UndoPayment(ctx context.Context, householdID, billID, paymentID string) error
	// ListPayments returns one household's payments whose due_on falls in the
	// month containing `month`, newest paid_on first, ties by bill name.
	ListPayments(ctx context.Context, householdID string, month time.Time) ([]BillPaymentRecord, error)
	// MonthTotals returns the two figures the stat cards pair: paidMinor is
	// the sum of payments due in the month, and dueMinor is that plus every
	// unarchived bill still due in it. See this interface's own header comment
	// for why the second cannot come from bills alone.
	//
	// Both are per-currency, keyed by the pay-from account's currency, because
	// a household can hold accounts in more than one. The service converts and
	// adds; the repository never does money arithmetic across currencies.
	MonthTotals(ctx context.Context, householdID string, month time.Time) (dueMinor, paidMinor map[string]int64, err error)
}
```

- [ ] **Step 2: Verify it compiles and the arch lint holds**

Run: `cd api && go build ./... && cd .. && make lint-arch`
Expected: both clean. `ports.go` imports only stdlib and `internal/domain`.

- [ ] **Step 3: Commit**

```bash
git add api/internal/usecase/ports.go
git commit -m "feat(bills): BillRepository port and its records"
```

---

### Task 4: Postgres — the read side of `BillRepository`

**Files:**
- Create: `api/internal/adapter/postgres/queries/bill.sql`
- Create: `api/internal/adapter/postgres/bill_repo.go`
- Create: `api/internal/adapter/postgres/bill_repo_test.go`
- Modify: generated `api/internal/adapter/postgres/sqlcgen/*` (via `make sqlc`)

**Interfaces:**
- Consumes: `usecase.BillRepository` from Task 3, `domain.Bill` from Task 2.
- Produces: `postgres.NewBillRepo(db *DB) *BillRepo` implementing `List`, `Get`, `Create`, `Update`, `SetArchived`, `ListPayments`, `MonthTotals`. Task 5 adds the two transactional writes to the same type. `cmd/api/main.go` wires it in Task 9.

- [ ] **Step 1: Write the queries**

Create `api/internal/adapter/postgres/queries/bill.sql`. The two that matter are `ListBills` (joins for the display names) and `BillMonthTotals` (the union):

```sql
-- name: ListBills :many
SELECT b.id, b.household_id, b.name, b.amount_minor, b.cadence, b.next_due,
       b.due_anchor_day, b.category_id, b.pay_from_account_id,
       b.paid_by_membership_id, b.autopay, b.is_subscription, b.archived_at,
       COALESCE(c.name, '')  AS category_name,
       a.nickname            AS account_name,
       a.opening_balance_currency AS currency
FROM bills b
JOIN accounts a ON a.id = b.pay_from_account_id
LEFT JOIN categories c ON c.id = b.category_id
WHERE b.household_id = $1
  AND (sqlc.arg(include_archived)::boolean OR b.archived_at IS NULL)
ORDER BY b.next_due NULLS LAST, b.name;

-- name: BillMonthDueTotals :many
-- The union the port's doc comment demands. A bill paid this month has already
-- advanced past it, so the two halves come from different tables and neither
-- alone is the figure.
--
-- No archived_at filter here, unlike BillMonthUnpaidTotals below, and that
-- asymmetry is deliberate: this money already left the household, so archiving
-- the bill afterwards must not retroactively empty the month it was paid in.
SELECT a.opening_balance_currency AS currency, SUM(p.amount_minor)::bigint AS minor
FROM bill_payments p
JOIN bills b    ON b.id = p.bill_id
JOIN accounts a ON a.id = b.pay_from_account_id
WHERE p.household_id = $1
  AND p.due_on >= sqlc.arg(month_start)::date
  AND p.due_on <  sqlc.arg(next_month)::date
GROUP BY a.opening_balance_currency;

-- name: BillMonthUnpaidTotals :many
SELECT a.opening_balance_currency AS currency, SUM(b.amount_minor)::bigint AS minor
FROM bills b
JOIN accounts a ON a.id = b.pay_from_account_id
WHERE b.household_id = $1
  AND b.archived_at IS NULL
  AND b.next_due >= sqlc.arg(month_start)::date
  AND b.next_due <  sqlc.arg(next_month)::date
GROUP BY a.opening_balance_currency;

-- name: ListBillPaymentsForMonth :many
SELECT p.id, p.bill_id, p.household_id, p.due_on, p.paid_on, p.amount_minor,
       p.transaction_id, b.name AS bill_name, b.autopay, a.opening_balance_currency AS currency
FROM bill_payments p
JOIN bills b    ON b.id = p.bill_id
JOIN accounts a ON a.id = b.pay_from_account_id
WHERE p.household_id = $1
  AND p.due_on >= sqlc.arg(month_start)::date
  AND p.due_on <  sqlc.arg(next_month)::date
ORDER BY p.paid_on DESC, b.name;
```

Write `GetBill`, `CreateBill`, `UpdateBill` and `SetBillArchived` in the same file, following `queries/goal.sql`'s shapes exactly. `GetBill` filters on `household_id` and `id` together. **`UpdateBill` is an unconditional full-row `SET` — every mutable column including `due_anchor_day`, no `COALESCE` and no dynamic SQL.** `BillService` merges a partial PATCH into a complete `domain.Bill` before this port ever sees it (`ports.go`'s `Update` comment, and the same rule `AccountRepository.Update` and `GoalRepository.Update` already state), so the anchor arrives already derived and the adapter never computes a calendar day.

Every query that returns a bill joins `accounts` and selects `a.opening_balance_currency` (the column is named for the balance it was introduced with; it is the account's currency, full stop — there is no bare `currency` column on `accounts`), because `Bill.Amount` carries the pay-from account's currency — there is no currency column on `bills`, and no second `Currency` field on `BillRecord` to fill instead.

- [ ] **Step 2: Regenerate**

Run: `make sqlc`
Expected: new methods on `sqlcgen.Queries`. If sqlc errors on a query, the message quotes the offending SQL.

- [ ] **Step 3: Write the failing repository tests**

Create `api/internal/adapter/postgres/bill_repo_test.go`. The two that carry the contract:

```go
// The naive "bills whose next_due is in this month" query returns a wrong
// number here, because paying the bill already advanced it into next month.
func TestMonthTotalsCountsABillAlreadyPaidThisMonth(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db) // SGD

	// Due 8 Aug, S$142.30, monthly.
	bill := createBill(t, ctx, repo, h, acct, "SP utilities", domain.CadenceMonthly, day("2026-08-08"), 14230)
	// A second bill due 24 Aug, unpaid.
	createBill(t, ctx, repo, h, acct, "Income tax", domain.CadenceMonthly, day("2026-08-24"), 23000)

	next := day("2026-09-08")
	if _, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-08-08"), PaidOn: day("2026-08-08"),
		AmountMinor: 14230, Currency: "SGD", Description: "SP utilities",
		PayFromAccountID: acct, NextDue: &next,
	}); err != nil {
		t.Fatalf("RecordPayment: %v", err)
	}

	due, paid, err := repo.MonthTotals(ctx, h, day("2026-08-01"))
	if err != nil {
		t.Fatalf("MonthTotals: %v", err)
	}
	if paid["SGD"] != 14230 {
		t.Fatalf("paid = %d, want 14230", paid["SGD"])
	}
	// 14230 already paid + 23000 still due. A query over bills.next_due alone
	// would return 23000 and look right.
	if due["SGD"] != 37230 {
		t.Fatalf("due = %d, want 37230 (the paid bill still counts)", due["SGD"])
	}
}

func TestGetHidesABillFromAnotherHousehold(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo := postgres.NewBillRepo(db)
	mine, myAcct := seedHouseholdAndAccount(t, ctx, db)
	theirs, theirAcct := seedHouseholdAndAccount(t, ctx, db)
	other := createBill(t, ctx, repo, theirs, theirAcct, "Theirs", domain.CadenceMonthly, day("2026-08-08"), 1000)

	_, err := repo.Get(ctx, mine, other.Bill.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get across households = %v, want ErrNotFound (not forbidden, not a row)", err)
	}
	_ = myAcct
}
```

Also write: `List` returns live-and-archived as a union when `includeArchived` is true; `Create` surfaces `domain.ErrBillNameTaken` on a duplicate, archived rows included; `Update` with `NextDue` set also moves `due_anchor_day`.

- [ ] **Step 4: Run them to verify they fail**

Run: `cd api && go test ./internal/adapter/postgres/ -run TestMonthTotals -v`
Expected: FAIL — `undefined: postgres.NewBillRepo`.

- [ ] **Step 5: Write `bill_repo.go`**

Follow `goal_repo.go` exactly: a `BillRepo` holding both `*sqlcgen.Queries` and the `*pgxpool.Pool` (Task 5's two writes each begin their own transaction), `translate(err, "…")` on every error, and a `toBillRecord` converter that builds `domain.Money` from `amount_minor` plus the joined account currency, and maps SQL NULL to `""` for `CategoryID` and `PaidByMembershipID` per `ports.go`'s convention.

`MonthTotals` calls both generated queries and merges them:

```go
func (r *BillRepo) MonthTotals(ctx context.Context, householdID string, month time.Time) (map[string]int64, map[string]int64, error) {
	start, next := monthBounds(month) // the same helper budget_repo.go uses
	paidRows, err := r.q.BillMonthDueTotals(ctx, sqlcgen.BillMonthDueTotalsParams{
		HouseholdID: uuid(householdID), MonthStart: date(start), NextMonth: date(next),
	})
	if err != nil {
		return nil, nil, translate(err, "bill month paid totals")
	}
	unpaidRows, err := r.q.BillMonthUnpaidTotals(ctx, sqlcgen.BillMonthUnpaidTotalsParams{
		HouseholdID: uuid(householdID), MonthStart: date(start), NextMonth: date(next),
	})
	if err != nil {
		return nil, nil, translate(err, "bill month unpaid totals")
	}
	paid := map[string]int64{}
	for _, row := range paidRows {
		paid[row.Currency] = row.Minor
	}
	// due is paid PLUS still-unpaid: the whole month's obligation, which is
	// what makes the two stat cards read as one fraction (spec decision 5).
	due := map[string]int64{}
	for cur, minor := range paid {
		due[cur] = minor
	}
	for _, row := range unpaidRows {
		due[row.Currency] += row.Minor
	}
	return due, paid, nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd api && go test ./internal/adapter/postgres/ -run TestBill -v && cd api && go test ./internal/adapter/postgres/ -run TestMonthTotals -v`
Expected: PASS. (Task 5 supplies `RecordPayment`; write it as a stub returning `nil, errors.New("not implemented")` only if Step 3's test needs it to compile, and delete the stub in Task 5.)

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/postgres/queries/bill.sql api/internal/adapter/postgres/bill_repo.go api/internal/adapter/postgres/bill_repo_test.go api/internal/adapter/postgres/sqlcgen
git commit -m "feat(bills): postgres reads, including the month-totals union"
```

---

### Task 5: Postgres — `RecordPayment` and `UndoPayment`, the two transactional writes

**Files:**
- Modify: `api/internal/adapter/postgres/bill_repo.go`
- Modify: `api/internal/adapter/postgres/queries/bill.sql`
- Modify: `api/internal/adapter/postgres/bill_repo_test.go`

**Interfaces:**
- Produces: `RecordPayment` and `UndoPayment` on `*BillRepo`, honouring the all-or-nothing and most-recent-only contracts in `ports.go`.

- [ ] **Step 1: Write the failing tests**

```go
// All three writes or none. Guarding-partial-writes exists because four
// defects in this project returned success for work that had only partly
// happened.
func TestRecordPaymentIsAtomic(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	bill := createBill(t, ctx, repo, h, acct, "SP utilities", domain.CadenceMonthly, day("2026-08-08"), 14230)

	// A category id that does not exist fails the transactions FK, which is
	// the second of the three writes.
	next := day("2026-09-08")
	_, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-08-08"), PaidOn: day("2026-08-08"),
		AmountMinor: 14230, Currency: "SGD", Description: "SP utilities",
		CategoryID: "00000000-0000-0000-0000-000000000001",
		PayFromAccountID: acct, NextDue: &next,
	})
	if err == nil {
		t.Fatal("expected the bad category to fail the write")
	}

	after, err := repo.Get(ctx, h, bill.Bill.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !after.Bill.NextDue.Equal(day("2026-08-08")) {
		t.Fatalf("next_due = %s, want it unmoved at 2026-08-08", after.Bill.NextDue.Format("2006-01-02"))
	}
	payments, err := repo.ListPayments(ctx, h, day("2026-08-01"))
	if err != nil {
		t.Fatalf("ListPayments: %v", err)
	}
	if len(payments) != 0 {
		t.Fatalf("got %d payments, want none -- a failed write left one behind", len(payments))
	}
}

func TestUndoRefusesAnythingButTheMostRecentPayment(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	bill := createBill(t, ctx, repo, h, acct, "Netflix", domain.CadenceMonthly, day("2026-07-05"), 1998)

	aug := day("2026-08-05")
	july, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-07-05"), PaidOn: day("2026-07-05"),
		AmountMinor: 1998, Currency: "SGD", Description: "Netflix",
		PayFromAccountID: acct, NextDue: &aug,
	})
	if err != nil {
		t.Fatalf("july payment: %v", err)
	}
	sep := day("2026-09-05")
	if _, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-08-05"), PaidOn: day("2026-08-05"),
		AmountMinor: 1998, Currency: "SGD", Description: "Netflix",
		PayFromAccountID: acct, NextDue: &sep,
	}); err != nil {
		t.Fatalf("august payment: %v", err)
	}

	// Undoing July would rewind next_due to 5 July -- behind August, which is
	// still paid -- and the screen would show a due date for money spent.
	err = repo.UndoPayment(ctx, h, bill.Bill.ID, july.Payment.ID)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("UndoPayment(older) = %v, want ErrForbidden", err)
	}
}

func TestUndoReversesAllThreeWrites(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	bill := createBill(t, ctx, repo, h, acct, "SP utilities", domain.CadenceMonthly, day("2026-08-08"), 14230)

	next := day("2026-09-08")
	pay, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-08-08"), PaidOn: day("2026-08-08"),
		AmountMinor: 14230, Currency: "SGD", Description: "SP utilities",
		PayFromAccountID: acct, NextDue: &next,
	})
	if err != nil {
		t.Fatalf("RecordPayment: %v", err)
	}
	if err := repo.UndoPayment(ctx, h, bill.Bill.ID, pay.Payment.ID); err != nil {
		t.Fatalf("UndoPayment: %v", err)
	}

	// 1. The payment row is gone.
	payments, err := repo.ListPayments(ctx, h, day("2026-08-01"))
	if err != nil {
		t.Fatalf("ListPayments: %v", err)
	}
	if len(payments) != 0 {
		t.Fatalf("got %d payments after undo, want 0", len(payments))
	}
	// 2. The expense is gone from the ledger. Counted in SQL rather than
	//    through a repository, so this asserts the row's absence and not some
	//    other layer's filtering of it.
	var txns int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM transactions WHERE household_id = $1`, h).Scan(&txns); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if txns != 0 {
		t.Fatalf("got %d transactions after undo, want 0 -- the expense survived", txns)
	}
	// 3. The due date is back where it was.
	after, err := repo.Get(ctx, h, bill.Bill.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !after.Bill.NextDue.Equal(day("2026-08-08")) {
		t.Fatalf("next_due = %s, want it rewound to 2026-08-08", after.Bill.NextDue.Format("2006-01-02"))
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd api && go test ./internal/adapter/postgres/ -run 'TestRecordPaymentIsAtomic|TestUndo' -v`
Expected: FAIL — not implemented.

- [ ] **Step 3: Implement both, each in one `pgx.Tx`**

```go
func (r *BillRepo) RecordPayment(ctx context.Context, in usecase.PaymentWrite) (usecase.BillPaymentRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return usecase.BillPaymentRecord{}, translate(err, "begin record payment")
	}
	defer tx.Rollback(ctx)
	q := r.q.WithTx(tx)

	// 1. The expense. Its currency is the account's, resolved by the service
	//    through AccountLookup -- the same rule TransactionService.Create
	//    applies at usecase/transaction.go:232.
	txn, err := q.CreateTransaction(ctx, sqlcgen.CreateTransactionParams{ /* kind expense, from_account, no to_account */ })
	if err != nil {
		return usecase.BillPaymentRecord{}, translate(err, "create bill expense")
	}
	// 2. The payment row. UNIQUE (bill_id, due_on) is what refuses a second
	//    payment of one occurrence; translate() maps it to ErrAlreadyExists.
	pay, err := q.CreateBillPayment(ctx, sqlcgen.CreateBillPaymentParams{ /* …, TransactionID: txn.ID */ })
	if err != nil {
		return usecase.BillPaymentRecord{}, translate(err, "create bill payment")
	}
	// 3. The advance. A bill left advanced with no payment is exactly the
	//    partial state this transaction exists to make impossible.
	if err := q.SetBillNextDue(ctx, sqlcgen.SetBillNextDueParams{ /* … */ }); err != nil {
		return usecase.BillPaymentRecord{}, translate(err, "advance bill next due")
	}
	if err := tx.Commit(ctx); err != nil {
		return usecase.BillPaymentRecord{}, translate(err, "commit record payment")
	}
	return toBillPaymentRecord(pay, in.Description, in.Currency)
}
```

`UndoPayment` opens its own transaction and, inside it: reads the payment filtered by `household_id` **and** `bill_id` (never by payment id alone — the port's contract); reads the bill's most recent `due_on` and returns `domain.ErrForbidden` if this payment is not it; deletes the payment; deletes the transaction when `transaction_id` is non-NULL; and sets `next_due` back to the payment's `due_on`.

**`SetBillNextDue` must never write `due_anchor_day`, on either path.** Only create and an explicit `PATCH` of `next_due` set the anchor, because only those are the household choosing a day. Advancing and rewinding are both mechanical. Writing it here would destroy the anchor through undo, which is the exact failure the column exists to prevent — with anchor 31: due 31 Jan → pay → 28 Feb → pay → 31 Mar → **undo** → 28 Feb, and if undo reset the anchor to 28 the next advance lands on 28 March and the bill has lost its 31st permanently.

- [ ] **Step 4: Add the anchor-survives-undo test**

This is the sibling of Task 2's designated mutation, and nothing else in the plan covers it. The clamp test proves the arithmetic; this proves the anchor is not quietly overwritten by the one write that has no business touching it.

```go
func TestUndoDoesNotDestroyTheDueAnchorDay(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo := postgres.NewBillRepo(db)
	h, acct := seedHouseholdAndAccount(t, ctx, db)
	// Due on the 31st: the only anchor that can be lost.
	bill := createBill(t, ctx, repo, h, acct, "Rent", domain.CadenceMonthly, day("2026-01-31"), 250000)

	feb := day("2026-02-28")
	if _, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-01-31"), PaidOn: day("2026-01-31"),
		AmountMinor: 250000, Currency: "SGD", Description: "Rent",
		PayFromAccountID: acct, NextDue: &feb,
	}); err != nil {
		t.Fatalf("january payment: %v", err)
	}
	mar := day("2026-03-31")
	febPay, err := repo.RecordPayment(ctx, usecase.PaymentWrite{
		HouseholdID: h, BillID: bill.Bill.ID, DueOn: day("2026-02-28"), PaidOn: day("2026-02-28"),
		AmountMinor: 250000, Currency: "SGD", Description: "Rent",
		PayFromAccountID: acct, NextDue: &mar,
	})
	if err != nil {
		t.Fatalf("february payment: %v", err)
	}

	if err := repo.UndoPayment(ctx, h, bill.Bill.ID, febPay.Payment.ID); err != nil {
		t.Fatalf("UndoPayment: %v", err)
	}

	after, err := repo.Get(ctx, h, bill.Bill.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !after.Bill.NextDue.Equal(day("2026-02-28")) {
		t.Fatalf("next_due = %s, want it rewound to 2026-02-28", after.Bill.NextDue.Format("2006-01-02"))
	}
	// The whole point: undo rewound the date but must not have taken the
	// anchor with it. An anchor of 28 here means the bill has silently lost
	// its 31st, and the next advance would land on 28 March.
	if after.Bill.DueAnchorDay != 31 {
		t.Fatalf("due_anchor_day = %d, want 31 -- undo overwrote the anchor", after.Bill.DueAnchorDay)
	}
}
```

- [ ] **Step 5: Run to verify they pass**

Run: `cd api && go test ./internal/adapter/postgres/ -run 'TestRecordPayment|TestUndo' -v`
Expected: PASS, the anchor test included.

- [ ] **Step 6: Mutation-check the atomicity test**

Remove the `defer tx.Rollback(ctx)` and commit after each statement instead. Run `TestRecordPaymentIsAtomic`. Expected: **FAIL** — a payment row survives a failed write. Restore.

Then mutate the anchor: make `SetBillNextDue` also write `due_anchor_day = EXTRACT(day FROM $next_due)`. Run `TestUndoDoesNotDestroyTheDueAnchorDay`. Expected: **FAIL** — `due_anchor_day = 28, want 31`. Restore.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/postgres/
git commit -m "feat(bills): atomic payment and undo"
```

---

### Task 6: Usecase — `BillService`, the screen composition

**Files:**
- Create: `api/internal/usecase/bill.go`
- Create: `api/internal/usecase/bill_test.go`
- Modify: `api/internal/usecase/testdouble_test.go`

**Interfaces:**
- Consumes: `BillRepository`, `HouseholdRepository`, `FXRateProvider`, `AccountLookup`.
- Produces: `usecase.BillDeps`, `usecase.BillService`, `NewBillService(BillDeps) *BillService`, the view types `BillView`, `BillPaymentView`, `BillsSummary`, `BillsView`, and **`usecase.BillPatch`** — declared here in `bill.go`, not in `ports.go`, exactly as `GoalUpdate` is. `List(ctx, householdID string, includeArchived bool, today time.Time) (BillsView, error)`, `Create(ctx, NewBill) (BillView, error)`, `Update(ctx, householdID, billID string, BillPatch) (BillView, error)`, `SetArchived(ctx, householdID, billID string, archived bool, at time.Time) (BillView, error)`. Task 9 calls all four.

**`Update` is where the merge happens, and it is the reason `BillPatch` lives at this layer.** The service `Get`s the bill, applies each non-nil field of the patch onto it, re-derives `DueAnchorDay` from `NextDue` whenever the patch moved it, and hands `BillRepository.Update` a complete `domain.Bill`. The port never merges — the rule `AccountRepository.Update`, `TransactionRepository.Update` and `GoalRepository.Update` each state in their own doc comments.

```go
// BillPatch is a PATCH: a nil field is unchanged. There is deliberately no
// ArchivedAt field -- archive and restore are their own routes, so an ordinary
// rename cannot archive a bill as a side effect (router.go's own comment for
// accounts, categories and goals).
//
// ClearCategory and ClearPayer are how a set field is unset, the same explicit
// -clear convention clearReceivedAmount uses on transactions: a nil pointer
// already means "unchanged", so it cannot also mean "clear".
type BillPatch struct {
	Name               *string
	AmountMinor        *int64
	Cadence            *domain.Cadence
	NextDue            *time.Time
	CategoryID         *string
	ClearCategory      bool
	PayFromAccountID   *string
	PaidByMembershipID *string
	ClearPayer         bool
	Autopay            *bool
	IsSubscription     *bool
}
```

- [ ] **Step 1: Write the view types and the failing tests**

The views mirror `GoalView`/`GoalsSummary`:

```go
// BillView is one row on the screen: the stored bill plus the derived figures.
// Amount is in the BILL's own currency -- the pay-from account's -- not the
// household's primary: a row for an IDR account renders in IDR. Only the
// summary totals below convert, for the reason GoalView's own comment gives.
type BillView struct {
	Bill         domain.Bill
	CategoryName string
	AccountName  string
	Overdue      bool
	// DueSoon is true when the bill belongs above the "Later" heading: overdue,
	// or due within 30 days inclusive. Computed here rather than in the
	// frontend so the rule lives in exactly one place.
	DueSoon bool
}

// BillsSummary is the page header and the three stat cards. DueThisMonth,
// PaidSoFar, SubscriptionsMonthly and SubscriptionsAnnual are all in the
// household's primary currency; a bill whose account currency has no rate to
// primary is excluded from every one of them and counted in ExcludedNoRate,
// never silently dropped (the BudgetRolloverCard precedent, 8a1114b).
type BillsSummary struct {
	Currency            string
	DueThisMonth        domain.Money
	PaidSoFar           domain.Money
	NextDueBillID       string
	NextDueBillName     string
	NextDueOn           *time.Time
	NextDueAmount       domain.Money
	NextDueOverdue      bool
	NextDueAutopay      bool
	AutopayCount        int
	BillCount           int
	SubscriptionsMonthly domain.Money
	SubscriptionsAnnual  domain.Money
	ExcludedNoRate      int
}
```

Tests to write in `bill_test.go`, each against in-memory doubles:

```go
// Every unpaid bill is on the page under one heading or the other. A 30-day
// filter alone would leave a yearly insurance bill invisible for eleven months
// while the header kept counting it (spec decision 5's rider).
func TestListSplitsDueSoonFromLaterAndKeepsBoth(t *testing.T) {
	today := day("2026-08-09")
	svc := newBillServiceWith(t,
		bill("Income tax", "2026-08-24", 23000),
		bill("Car insurance", "2026-11-01", 96500),
	)
	view, err := svc.List(context.Background(), "h1", false, today)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(view.Bills) != 2 {
		t.Fatalf("got %d bills, want both on the page", len(view.Bills))
	}
	byName := map[string]usecase.BillView{}
	for _, b := range view.Bills {
		byName[b.Bill.Name] = b
	}
	if !byName["Income tax"].DueSoon {
		t.Error("a bill 15 days out belongs under Due soon")
	}
	if byName["Car insurance"].DueSoon {
		t.Error("a bill 84 days out belongs under Later")
	}
}

func TestListMarksAnOverdueBillOverdueAndSortsItFirst(t *testing.T) {
	today := day("2026-08-09")
	svc := newBillServiceWith(t,
		bill("Netflix", "2026-08-20", 1998),
		bill("Income tax", "2026-07-24", 23000), // overdue
	)
	view, err := svc.List(context.Background(), "h1", false, today)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if view.Bills[0].Bill.Name != "Income tax" {
		t.Fatalf("first row = %q, want the overdue bill", view.Bills[0].Bill.Name)
	}
	if !view.Bills[0].Overdue {
		t.Error("a due date in the past is overdue")
	}
	// Overdue is also Due soon: it belongs above the Later heading, not below.
	if !view.Bills[0].DueSoon {
		t.Error("an overdue bill belongs under Due soon")
	}
	if view.Bills[1].Overdue {
		t.Error("a future due date is not overdue")
	}
}

// Integer-first: multiply every bill up to a year, add, then divide once.
func TestSubscriptionsRollupMultipliesThenDividesOnce(t *testing.T) {
	svc := newBillServiceWith(t,
		subscription("Netflix", domain.CadenceMonthly, 1998),
		subscription("YouTube Premium", domain.CadenceMonthly, 1798),
		subscription("Spotify family", domain.CadenceMonthly, 1698),
		subscription("Disney+", domain.CadenceMonthly, 1198),
		subscription("iCloud 200GB", domain.CadenceMonthly, 398),
		// Not ticked as a subscription: it must not reach either figure.
		bill("SP utilities", "2026-08-08", 14230),
		// A one-off is excluded even when ticked -- it is not a recurring cost.
		oneOffSubscription("Domain renewal", 3500),
	)
	view, err := svc.List(context.Background(), "h1", false, day("2026-08-09"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// 70.90/mo is the design's own figure; 70.90 * 12 = 850.80 is its own
	// annual line, and the two agree because one is derived from the other.
	if got := view.Summary.SubscriptionsAnnual.Amount; got != 85080 {
		t.Fatalf("annual = %d, want 85080", got)
	}
	if got := view.Summary.SubscriptionsMonthly.Amount; got != 7090 {
		t.Fatalf("monthly = %d, want 7090", got)
	}
}

func TestSubscriptionsRollupNormalisesANonMonthlyCadence(t *testing.T) {
	svc := newBillServiceWith(t,
		subscription("Insurance", domain.CadenceQuarterly, 30000),
		subscription("Domain", domain.CadenceYearly, 1200),
	)
	view, err := svc.List(context.Background(), "h1", false, day("2026-08-09"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// 30000*4 + 1200*1 = 121200 a year; 121200/12 = 10100 a month.
	if got := view.Summary.SubscriptionsAnnual.Amount; got != 121200 {
		t.Fatalf("annual = %d, want 121200", got)
	}
	if got := view.Summary.SubscriptionsMonthly.Amount; got != 10100 {
		t.Fatalf("monthly = %d, want 10100", got)
	}
}

func TestSummaryExcludesABillWithNoRateAndCountsIt(t *testing.T) {
	// Household primary SGD; one bill on an IDR account the FX double has no
	// rate for.
	svc := newBillServiceWithFX(t, noRateFX{},
		bill("SP utilities", "2026-08-08", 14230),          // SGD
		billOn("Arisan", "IDR", "2026-08-15", 50_000_000),  // no rate
	)
	view, err := svc.List(context.Background(), "h1", false, day("2026-08-09"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := view.Summary.DueThisMonth.Amount; got != 14230 {
		t.Fatalf("due = %d, want 14230 -- the no-rate bill must not be summed", got)
	}
	// Excluded, never silently dropped: the screen says how many.
	if view.Summary.ExcludedNoRate != 1 {
		t.Fatalf("ExcludedNoRate = %d, want 1", view.Summary.ExcludedNoRate)
	}
}

func TestNextDueIsOmittedWhenThereIsNone(t *testing.T) {
	svc := newBillServiceWith(t)
	view, err := svc.List(context.Background(), "h1", false, day("2026-08-09"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// nil, not a zero Money: the card renders nothing rather than "S$0.00".
	if view.Summary.NextDueOn != nil {
		t.Fatalf("NextDueOn = %v, want nil for a household with no bills", view.Summary.NextDueOn)
	}
}

func TestAutopayCountsOnlyUnarchivedBills(t *testing.T) {
	svc := newBillServiceWith(t,
		autopayBill("Income tax", "2026-08-24", 23000),
		archived(autopayBill("Old gym", "2026-08-02", 8000)),
		bill("School fees", "2026-08-15", 38000),
	)
	view, err := svc.List(context.Background(), "h1", false, day("2026-08-09"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if view.Summary.AutopayCount != 1 || view.Summary.BillCount != 2 {
		t.Fatalf("got %d of %d on autopay, want 1 of 2 -- the archived bill counts in neither",
			view.Summary.AutopayCount, view.Summary.BillCount)
	}
}
```

Write the small fixture helpers (`newBillServiceWith`, `newBillServiceWithFX`, `bill`, `billOn`, `autopayBill`, `subscription`, `oneOffSubscription`, `archived`, `noRateFX`) beside the tests, in the style `budget_test.go` already uses for its own fixtures. Each returns a `usecase.BillRecord` with a sensible SGD default so a test names only the thing it is about.

- [ ] **Step 2: Run them to verify they fail**

Run: `cd api && go test ./internal/usecase/ -run TestList -v`
Expected: FAIL — `undefined: usecase.NewBillService`.

- [ ] **Step 3: Add the in-memory double**

In `testdouble_test.go`, add a `fakeBillRepo` beside the existing fakes: a slice of `BillRecord`, a slice of `BillPaymentRecord`, and an `err` field so a test can force a failure. `UndoPayment` enforces the most-recent rule in the fake too — a double that is more permissive than the real port lets a service bug through.

- [ ] **Step 4: Implement `BillService`**

`List` does, in order: read the household for its primary currency; `repo.List`; `repo.MonthTotals`; `repo.ListPayments`; then compose. Every cross-currency sum converts per bill through `deps.FX` and adds with `domain.Money.Add`, counting a missing rate rather than dropping it. `DueSoon` is `overdue || nextDue.Sub(today) <= 30*24*time.Hour`, computed on day boundaries.

`Create` derives `DueAnchorDay` from `NextDue.Day()` — never from a caller — trims and refuses an empty name (`ErrBillNameRequired`), refuses a non-positive amount (`ErrBillAmountNotPositive`), and refuses an archived pay-from account by asking `AccountLookup.Get`.

`Update` `Get`s the bill, applies each non-nil field of the `BillPatch` onto it, and hands `BillRepository.Update` a **complete** `domain.Bill` — the port never merges. It re-derives `DueAnchorDay` from the new `NextDue` whenever the patch moved it: an explicit edit is the household choosing a new anchor, and leaving the old one would make the next advance jump to a day they did not pick. It refuses a `PayFromAccountID` whose account currency differs from the bill's current one, with a new sentinel `domain.ErrBillCurrencyImmutable` — add it to `errors.go` in this task. The message names both currencies at the HTTP layer, not here.

Write a test for the anchor on update: patching a bill's `NextDue` to the 15th makes a later advance land on the 15th, not on the day the bill previously carried. Nothing else covers that path — Task 5's own anchor test covers the *mechanical* rewind, which must NOT touch the anchor, and this is the opposite case.

- [ ] **Step 5: Run to verify they pass**

Run: `cd api && go test ./internal/usecase/ -run 'TestList|TestSubscriptions|TestSummary|TestNextDue|TestAutopay' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/bill.go api/internal/usecase/bill_test.go api/internal/usecase/testdouble_test.go api/internal/domain/errors.go
git commit -m "feat(bills): BillService composes the screen"
```

---

### Task 7: Usecase — `MarkPaid` and `UndoPayment`

**Files:**
- Modify: `api/internal/usecase/bill.go`
- Modify: `api/internal/usecase/bill_test.go`

**Interfaces:**
- Produces: `(*BillService).MarkPaid(ctx, in MarkPayment) (BillPaymentView, error)` and `(*BillService).UndoPayment(ctx, householdID, billID, paymentID string) error`, where `MarkPayment{HouseholdID, BillID string; AmountMinor int64; PaidOn time.Time}`. Task 10 calls both.

- [ ] **Step 1: Write the failing tests**

```go
// The expense a bill payment writes must carry the ACCOUNT's currency -- the
// same rule TransactionService.Create applies at transaction.go:232. If these
// two ever disagree, a household's ledger row and its bill say different
// things about the same money.
func TestMarkPaidWritesTheExpenseInTheAccountsCurrency(t *testing.T) {
	repo := &fakeBillRepo{}
	svc := newBillService(t, repo, withAccount("acct-idr", "IDR"))
	repo.add(billOn("Arisan", "IDR", "2026-08-15", 50_000_000))

	if _, err := svc.MarkPaid(context.Background(), usecase.MarkPayment{
		HouseholdID: "h1", BillID: "bill-1", AmountMinor: 50_000_000, PaidOn: day("2026-08-15"),
	}); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	if got := repo.lastWrite.Currency; got != "IDR" {
		t.Fatalf("expense currency = %q, want the account's IDR", got)
	}
	// And the description is the bill's own name, so an accidental duplicate
	// hand-entered in the ledger is recognisable rather than invisible.
	if got := repo.lastWrite.Description; got != "Arisan" {
		t.Fatalf("description = %q, want the bill's name", got)
	}
}

func TestMarkPaidAdvancesNextDueByTheCadenceFromTheDueDate(t *testing.T) {
	repo := &fakeBillRepo{}
	svc := newBillService(t, repo)
	repo.add(bill("SP utilities", "2026-08-08", 14230)) // monthly, anchor 8

	// Paid three days late.
	if _, err := svc.MarkPaid(context.Background(), usecase.MarkPayment{
		HouseholdID: "h1", BillID: "bill-1", AmountMinor: 14230, PaidOn: day("2026-08-11"),
	}); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	// 2026-09-08, NOT 2026-09-11: paying late must not move the bill's day,
	// or a year of late payments walks it a month off.
	if got := repo.lastWrite.NextDue; got == nil || !got.Equal(day("2026-09-08")) {
		t.Fatalf("next due = %v, want 2026-09-08", got)
	}
	// The occurrence settled is the one that was due, not the day it was paid.
	if !repo.lastWrite.DueOn.Equal(day("2026-08-08")) {
		t.Fatalf("due_on = %s, want 2026-08-08", repo.lastWrite.DueOn.Format("2006-01-02"))
	}
}

func TestMarkPaidSettlesAOneOffWithNoNextDate(t *testing.T) {
	repo := &fakeBillRepo{}
	svc := newBillService(t, repo)
	repo.add(oneOff("Renew passport", "2026-08-20", 7000))

	if _, err := svc.MarkPaid(context.Background(), usecase.MarkPayment{
		HouseholdID: "h1", BillID: "bill-1", AmountMinor: 7000, PaidOn: day("2026-08-20"),
	}); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	if repo.lastWrite.NextDue != nil {
		t.Fatalf("next due = %v, want nil -- a one-off has no next occurrence", repo.lastWrite.NextDue)
	}
}

func TestMarkPaidRefusesAnArchivedBill(t *testing.T) {
	repo := &fakeBillRepo{}
	svc := newBillService(t, repo)
	repo.add(archived(bill("Old gym", "2026-08-02", 8000)))

	_, err := svc.MarkPaid(context.Background(), usecase.MarkPayment{
		HouseholdID: "h1", BillID: "bill-1", AmountMinor: 8000, PaidOn: day("2026-08-09"),
	})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("MarkPaid(archived bill) = %v, want ErrForbidden", err)
	}
}

func TestMarkPaidRefusesAnArchivedPayFromAccount(t *testing.T) {
	repo := &fakeBillRepo{}
	svc := newBillService(t, repo, withArchivedAccount("acct-1", "SGD"))
	repo.add(bill("SP utilities", "2026-08-08", 14230))

	_, err := svc.MarkPaid(context.Background(), usecase.MarkPayment{
		HouseholdID: "h1", BillID: "bill-1", AmountMinor: 14230, PaidOn: day("2026-08-09"),
	})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("MarkPaid(archived account) = %v, want ErrForbidden", err)
	}
}

func TestMarkPaidRefusesASettledOneOff(t *testing.T) {
	repo := &fakeBillRepo{}
	svc := newBillService(t, repo)
	settled := oneOff("Renew passport", "2026-08-20", 7000)
	settled.Bill.NextDue = nil // already paid; there is no occurrence left
	repo.add(settled)

	_, err := svc.MarkPaid(context.Background(), usecase.MarkPayment{
		HouseholdID: "h1", BillID: "bill-1", AmountMinor: 7000, PaidOn: day("2026-08-25"),
	})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("MarkPaid(settled one-off) = %v, want ErrForbidden", err)
	}
}

func TestUndoPaymentPassesThroughTheMostRecentOnlyRefusal(t *testing.T) {
	// The rule lives in the repository, which owns the transaction that reads
	// the latest due_on and rewinds next_due together. This asserts the
	// service does not swallow or reinterpret the refusal on the way out.
	repo := &fakeBillRepo{undoErr: domain.ErrForbidden}
	svc := newBillService(t, repo)
	if err := svc.UndoPayment(context.Background(), "h1", "bill-1", "pay-old"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("UndoPayment = %v, want ErrForbidden passed through", err)
	}
}
```

`fakeBillRepo` records the last `PaymentWrite` it received in `lastWrite`, which is what every assertion above reads: the service's whole job here is assembling that value correctly.

- [ ] **Step 2: Run to verify they fail**

Run: `cd api && go test ./internal/usecase/ -run TestMarkPaid -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// MarkPaid writes the payment, the expense and the advanced due date, through
// BillRepository.RecordPayment's single transaction.
//
// The amount is the caller's, not the bill's: utilities vary, so the modal
// prefills the bill's figure and lets it be edited. The bill's own
// amount_minor is unchanged by paying -- a one-off high month is not a new
// standing amount.
func (s *BillService) MarkPaid(ctx context.Context, in MarkPayment) (BillPaymentView, error) {
	rec, err := s.deps.Bills.Get(ctx, in.HouseholdID, in.BillID)
	if err != nil {
		return BillPaymentView{}, err
	}
	if rec.Bill.IsArchived() {
		return BillPaymentView{}, domain.ErrForbidden
	}
	if rec.Bill.NextDue == nil {
		// A settled one-off has no occurrence left to pay.
		return BillPaymentView{}, domain.ErrForbidden
	}
	acct, err := s.deps.Accounts.Get(ctx, in.HouseholdID, rec.Bill.PayFromAccountID)
	if err != nil {
		return BillPaymentView{}, err
	}
	if acct.Account.IsArchived() {
		return BillPaymentView{}, domain.ErrForbidden
	}
	// The expense's currency is the account's, never the bill's stored figure
	// reinterpreted -- transaction.go:232 is the rule this mirrors.
	currency := acct.Balance.Currency

	var next *time.Time
	// Advance from the DUE date, never from PaidOn: a bill paid three days
	// late must not shift three days every month.
	if n, ok := domain.NextDue(rec.Bill.Cadence, *rec.Bill.NextDue, rec.Bill.DueAnchorDay); ok {
		next = &n
	}

	pay, err := s.deps.Bills.RecordPayment(ctx, PaymentWrite{
		HouseholdID:        in.HouseholdID,
		BillID:             in.BillID,
		DueOn:              *rec.Bill.NextDue,
		PaidOn:             in.PaidOn,
		AmountMinor:        in.AmountMinor,
		Currency:           currency,
		Description:        rec.Bill.Name,
		CategoryID:         rec.Bill.CategoryID,
		PayFromAccountID:   rec.Bill.PayFromAccountID,
		PaidByMembershipID: rec.Bill.PaidByMembershipID,
		NextDue:            next,
	})
	if err != nil {
		return BillPaymentView{}, err
	}
	return toBillPaymentView(pay), nil
}
```

`UndoPayment` is a straight delegation to the repository, which owns both the transaction and the most-recent-only refusal.

- [ ] **Step 4: Run to verify they pass**

Run: `cd api && go test ./internal/usecase/ -run 'TestMarkPaid|TestUndoPayment' -v`
Expected: PASS.

- [ ] **Step 5: Mutation-check the late-payment test**

Change `domain.NextDue(rec.Bill.Cadence, *rec.Bill.NextDue, …)` to advance from `in.PaidOn`. Run `TestMarkPaidAdvancesNextDueByTheCadenceFromTheDueDate`. Expected: **FAIL** — `2026-09-11, want 2026-09-08`. Restore.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/bill.go api/internal/usecase/bill_test.go
git commit -m "feat(bills): mark paid writes a real expense"
```

---

### Task 8: Spending by person grows an Unattributed row

**Files:**
- Modify: `api/internal/usecase/budget.go:252`
- Modify: `api/internal/usecase/budget_test.go`
- Modify: `api/internal/adapter/http/budget_handlers.go`
- Modify: `web/src/features/money/BudgetByPerson.tsx`
- Modify: `web/src/features/money/budgetSchemas.ts`
- Modify: `web/src/features/money/BudgetPage.test.tsx`

**Interfaces:**
- Produces: a `BudgetPersonView` whose `MembershipID` is `""` and whose `Name` is empty for the unattributed bucket; the DTO carries it as `{"membershipId": "", "name": ""}` and the frontend renders the label. Copy belongs in the frontend, not composed in Go.

This is one behaviour end to end, so it is one task: a reviewer cannot sensibly accept the backend half and reject the frontend half.

- [ ] **Step 1: Write the failing test**

```go
// Before Bills, a transaction with no payer was counted in Spent and dropped
// from ByPerson, so the card's rows quietly summed to less than the month's
// spend with nothing on screen saying so. Bills makes that the common case:
// a bill with no payer pays every month.
func TestByPersonRowsSumToSpent(t *testing.T) {
	svc := newBudgetServiceWith(t,
		expense("Groceries", 12000, paidBy("m-andreas")),
		expense("SP utilities", 14230, paidBy("")), // a bill payment, no payer
	)
	view, err := svc.Month(context.Background(), "h1", day("2026-08-01"), day("2026-08-09"))
	if err != nil {
		t.Fatalf("Month: %v", err)
	}
	if len(view.ByPerson) != 2 {
		t.Fatalf("got %d rows, want 2 -- unattributed spend needs a row of its own", len(view.ByPerson))
	}
	// The unattributed bucket sorts last, whenever its first transaction
	// happened to appear.
	last := view.ByPerson[len(view.ByPerson)-1]
	if last.MembershipID != "" {
		t.Fatalf("last row membership = %q, want the empty unattributed key", last.MembershipID)
	}
	var total int64
	for _, p := range view.ByPerson {
		total += p.Spent.Amount
	}
	// The whole point: the card's rows must account for every cent of Spent,
	// or it quietly disagrees with the figure above it.
	if total != view.Spent.Amount {
		t.Fatalf("rows sum to %d but Spent is %d", total, view.Spent.Amount)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd api && go test ./internal/usecase/ -run TestByPersonRowsSumToSpent -v`
Expected: FAIL — one row, and the sum is short.

- [ ] **Step 3: Implement**

In `budget.go`, replace the `if t.PaidByMembershipID != ""` guard with an unconditional accumulate keyed on the possibly-empty id, and let `memberNames` return `""` for that key. Order it last in `personOrder` so it renders at the bottom regardless of when the first unattributed transaction appeared.

- [ ] **Step 4: Run to verify it passes, then mutation-check**

Run the test: PASS. Then restore the `!= ""` guard and re-run: **FAIL**. Restore the fix.

- [ ] **Step 5: Frontend**

`budgetSchemas.ts` needs no change (`membershipId` is already `z.string()`). In `BudgetByPerson.tsx`, render a row whose `membershipId` is `""` with the label **"Unattributed"** and a one-line explanation beneath the card: *"Spending with nobody recorded as the payer — bills without a "Paid by", and transactions saved without one."* Add a test in `BudgetPage.test.tsx` asserting the label appears for an empty `membershipId` and not otherwise.

- [ ] **Step 6: Run both suites**

Run: `cd api && go test ./internal/usecase/ -run TestByPerson -v && cd ../web && npx vitest run src/features/money/BudgetPage.test.tsx`
Expected: PASS both.

- [ ] **Step 7: Commit**

```bash
git add api/internal/usecase/budget.go api/internal/usecase/budget_test.go api/internal/adapter/http/budget_handlers.go web/src/features/money/
git commit -m "fix(budget): spending by person accounts for unattributed spend"
```

---

### Task 9: HTTP — the bill routes

**Files:**
- Create: `api/internal/adapter/http/bill_handlers.go`
- Create: `api/internal/adapter/http/bills_api_test.go`
- Modify: `api/internal/adapter/http/router.go`
- Modify: `api/internal/adapter/http/deps.go`
- Modify: `api/cmd/api/main.go`

**Interfaces:**
- Produces: `GET /api/v1/bills`, `POST /api/v1/bills`, `PATCH /api/v1/bills/{id}`, `POST /api/v1/bills/{id}/archive`, `POST /api/v1/bills/{id}/restore`, and the DTOs `billDTO`, `billsSummaryDTO`, `billPaymentDTO`, `billsResponse`. Task 11's zod schemas mirror these field names exactly.

- [ ] **Step 1: Write the route-walk matrix first**

Create `bills_api_test.go` following `budget_api_test.go`'s shape. Cover, for each of the five routes: an owner with `money` succeeds; an owner without `money` is 403; a **limited member holding `money` is 403 on reads as well as writes** (the Transactions/Budget/Goals shape — a bills screen with every figure blank reads as broken); no session is 401; a write with no CSRF token is 403.

- [ ] **Step 2: Run to verify it fails**

Run: `cd api && go test ./internal/adapter/http/ -run TestBills -v`
Expected: FAIL — the routes 404.

- [ ] **Step 3: Write the handlers and DTOs**

Field names, which Task 11 mirrors exactly:

```go
type billDTO struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	AmountMinor        int64   `json:"amountMinor"`
	Currency           string  `json:"currency"`
	Cadence            string  `json:"cadence"`
	NextDue            *string `json:"nextDue"`
	CategoryID         string  `json:"categoryId"`
	CategoryName       string  `json:"categoryName"`
	PayFromAccountID   string  `json:"payFromAccountId"`
	AccountName        string  `json:"accountName"`
	PaidByMembershipID string  `json:"paidByMembershipId"`
	Autopay            bool    `json:"autopay"`
	IsSubscription     bool    `json:"isSubscription"`
	Overdue            bool    `json:"overdue"`
	DueSoon            bool    `json:"dueSoon"`
	Settled            bool    `json:"settled"`
	ArchivedAt         *string `json:"archivedAt"`
}
```

`billsResponse` carries `bills`, `paidThisMonth` and `summary`. The frontend splits `bills` into Due soon and Later on the server-computed `dueSoon` flag rather than recomputing 30 days in TypeScript.

**`Settled` must be serialised, and Task 12 must render it.** A settled one-off — paid, with no next date — has `NextDue == nil`, so it belongs to neither Due soon nor Later by their own definitions (both require a non-null `next_due`), and it is deliberately not auto-archived, because that would hide a record the household may still want. But it is still counted in `BillCount` and `N of M on autopay`. Drop the field and a bill is counted in the header while appearing nowhere on the page — the same defect the 30-day window had before this plan grew its Later heading, and the same shape as a feature no screen can reach. Task 7's own review found this gap in these very sketches; it is closed here.

Error mapping: `ErrBillNameTaken` → 409 whose body names the taken name and whether the holder is archived (so the modal can offer restore); `ErrBillCurrencyImmutable` → 422 naming both currencies; `ErrUnknownCadence`, `ErrBillNameRequired`, `ErrBillAmountNotPositive` → 422; `ErrForbidden` → 422 with the reason; `ErrAccountOwnerNotInHousehold` → 422 (already mapped by the shared switch in `internal/adapter/http/errors.go`; confirm rather than re-add); `ErrNotFound` → 404. Every 2xx carries a body, archive and restore included.

- [ ] **Step 4: Wire the routes**

In `router.go`, inside the money group, mirroring the goals block exactly:

```go
			txn.Get("/bills", handleListBills(deps))
			txn.Group(func(w chi.Router) {
				w.Use(requireCSRF)
				w.Post("/bills", handleCreateBill(deps))
				w.Patch("/bills/{id}", handleUpdateBill(deps))
				// Archive and restore are their own routes rather than a field
				// on PATCH, the same reasoning as accounts, categories and
				// goals above: if archiving were patchable, an ordinary rename
				// that happened to include it would archive the bill as a side
				// effect of saving a name.
				w.Post("/bills/{id}/archive", handleArchiveBill(deps))
				w.Post("/bills/{id}/restore", handleRestoreBill(deps))
			})
```

Add `Bills *usecase.BillService` to `deps.go` and construct it in `main.go`.

- [ ] **Step 5: Run to verify it passes**

Run: `cd api && go test ./internal/adapter/http/ -run TestBills -v`
Expected: PASS, every row of the matrix.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/http/ api/cmd/api/main.go
git commit -m "feat(bills): bill routes gated money+owner"
```

---

### Task 10: HTTP — pay and undo

**Files:**
- Modify: `api/internal/adapter/http/bill_handlers.go`
- Modify: `api/internal/adapter/http/bills_api_test.go`
- Modify: `api/internal/adapter/http/router.go`

**Interfaces:**
- Produces: `POST /api/v1/bills/{id}/pay` answering `{"payment": …, "bill": …}`, and `DELETE /api/v1/bills/{id}/payments/{paymentId}` answering 204.

- [ ] **Step 1: Extend the matrix and add the behaviour tests**

Add the two routes to the guard matrix, plus: paying an occurrence twice is 409; undoing an older payment is 409 naming the one that can be undone; paying a bill in another household is 404.

- [ ] **Step 2: Run to verify they fail; implement; run to verify they pass**

Run: `cd api && go test ./internal/adapter/http/ -run TestBills -v`

`POST /pay` takes `{amountMinor, paidOn}`; the amount defaults to the bill's when absent. `DELETE` answers 204 with no body — the one exemption to the JSON-body rule, matching `handleDeleteTransaction`.

- [ ] **Step 3: Commit**

```bash
git add api/internal/adapter/http/
git commit -m "feat(bills): pay and undo routes"
```

---

### Task 11: Frontend — schemas, hook, route, sidebar

**Files:**
- Create: `web/src/features/money/billSchemas.ts`
- Create: `web/src/features/money/useBills.ts`
- Create: `web/src/features/money/useBills.test.ts`
- Create: `web/src/features/money/BillsPage.tsx` (shell only; Task 12 builds it out)
- Modify: `web/src/routes/router.tsx`
- Modify: `web/src/features/shell/Sidebar.tsx`
- Delete: `web/src/features/placeholder/…` usage for `/money/$` — see below

**Interfaces:**
- Produces: `billsQueryKey()`, `useBills(includeArchived)`, `useCreateBill`, `useUpdateBill`, `useMarkPaid`, `useUndoPayment`, `useArchiveBill`, `useRestoreBill`, and the types `Bill`, `BillPayment`, `BillsSummary`, `BillsResponse`. Tasks 12–16 consume these names.

- [ ] **Step 1: Write the schemas**

Mirror `bill_handlers.go`'s DTOs exactly, following `goalSchemas.ts`'s conventions: `nullable()` for Go pointer fields with no `omitempty` (they always serialise, `null` when unset), plain fields for the rest. `cadenceSchema = z.enum(["one_off","monthly","quarterly","yearly"])`.

- [ ] **Step 2: Write the hook, with all fetch orchestration inside it**

This is Budget decision 11 applied again on purpose. `BudgetPage.tsx` never grew the debt `TransactionsPage.tsx` carries — over 500 lines doing fetch orchestration, pagination, body translation and row rendering together — because `useBudget.ts` existed from day one. `BillsPage.tsx` gets the same treatment: no `apiFetch` call appears in a component in this feature.

- [ ] **Step 3: Replace the `/money/$` placeholder with the real route**

In `router.tsx`: add `moneyBillsRoute` as a sibling of `moneyGoalsRoute` under `moneyGuardRoute`, and **delete `moneySplatRoute`** — Bills was its last remaining reason to exist, and the header comment at line 19 says so. Update the route-map comment at the top of the file in the same edit. Add `{ label: "Bills", to: "/money/bills" }` to `SPACE_PAGES.money` in `Sidebar.tsx`, in the same change as the route: a route without its sidebar entry, or the reverse, is the failure mode `docs/FEATURE_TRACKER.md`'s Marriage note exists to prevent.

- [ ] **Step 4: Update `router.test.tsx`**

The existing test asserting `/money/anything` renders the placeholder must go, replaced by one asserting `/money/bills` renders BillsPage and that a member without `money` is redirected away from it — the shape `router.test.tsx` already uses for `/money/budget`.

- [ ] **Step 5: Run**

Run: `cd web && npx vitest run src/features/money/useBills.test.ts src/routes/router.test.tsx && npx tsc --noEmit`
Expected: PASS, no type errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/money/ web/src/routes/ web/src/features/shell/Sidebar.tsx
git commit -m "feat(bills): route, sidebar link, schemas and fetch hook"
```

---

### Task 12: Frontend — BillsPage, its lists and its five states

**Files:**
- Modify: `web/src/features/money/BillsPage.tsx`
- Create: `web/src/features/money/BillStatCards.tsx`
- Create: `web/src/features/money/BillRow.tsx`
- Create: `web/src/features/money/billCopy.ts`
- Create: `web/src/features/money/BillsPage.test.tsx`

**Interfaces:**
- Consumes: `useBills`, `Bill`, `BillsSummary` from Task 11.
- Produces: `BillRow` (one row, used by Due soon, Later and Paid this month alike) and `billCopy.ts` holding every user-facing string.

- [ ] **Step 1: Write the failing tests, one per state**

```ts
// The five states, each asserted by its own test, so none is discovered in
// production. This is what the Transactions ledger's own five-state coverage
// bought, and what the interim Overview's blank limited-member page cost when
// a state went unenumerated.
it("first run: offers Add bill and shows no empty list headings", …)
it("bills exist but none is due this month: the stat cards explain rather than showing bare zeros", …)
it("ordinary: splits Due soon from Later on the server's dueSoon flag", …)
it("all caught up: names the next bill and the month that is settled", …)
it("overdue: sorts first, and an autopay bill's copy differs from a manual one's", …)
it("a settled one-off appears under Later with 'Settled' where a date would go", …)
```

**That last state is not optional.** A settled one-off — a one-off bill that has been paid — has no next due date, so it belongs to neither Due soon nor Later by their own definitions, and it is deliberately not auto-archived. It is still counted in the header's `N of M on autopay` and in `BillCount`. Render it under Later on the server-sent `settled` flag, showing "Settled" in place of the date, or it is a bill counted in a figure and visible nowhere — the same defect the 30-day window had before this plan grew its Later heading.

The overdue test asserts both strings: autopay → `Should have gone out on 24 Jul — confirm it did`; manual → `Overdue since 24 Jul`.

**Archive and restore both need a screen, and this is the task that gives them one.** Goals shipped archiving with every layer beneath it built — column, repository, service, route, a mutation with a passing test — and **no screen that called it**, so "Show archived" and every Restore button led out of a state no household could enter. The Task 18 browser walk found it at criterion 12 (`82453ff`, `docs/LEARNING.md` pattern 15). Do not repeat it: three more tests here, and they are not optional.

```ts
it("every live bill row carries an Archive control", …)
it("Show archived lists archived bills, each with Restore", …)
it("an archived bill is in neither Due soon nor Later, and counts in no stat card", …)
```

`BillRow` renders Archive **or** Restore, never both — the either/or `AccountRow` and `GoalCard` already use. A row describes what a household can do, not what the stack can serve.

- [ ] **Step 2: Run to verify they fail; implement; run to verify they pass**

Run: `cd web && npx vitest run src/features/money/BillsPage.test.tsx`

Every string lives in `billCopy.ts`, never inline in JSX — the `goalCopy.ts`/`budgetCopy.ts` convention, which is what let those two be swept for tone in one file.

- [ ] **Step 3: Commit**

```bash
git add web/src/features/money/
git commit -m "feat(bills): the Bills page and its five states"
```

---

### Task 13: Frontend — the Add/Edit bill modal

**Files:**
- Create: `web/src/features/money/BillModal.tsx`
- Create: `web/src/features/money/BillModal.test.tsx`
- Modify: `web/src/features/money/BillsPage.tsx`

**Interfaces:**
- Consumes: `useCreateBill`, `useUpdateBill`, `useAccounts`, and the categories hook `TransactionModal.tsx` already uses.
- Produces: `BillModal`, mounted at three entry points — the empty state, a row's edit affordance, and the header's `+ Add bill`.

- [ ] **Step 1: Write the failing tests**

- Opens blank for a new bill and POSTs on save.
- Opens populated from an existing bill and PATCHes.
- **Is actually mounted at all three entry points.** Goals shipped a modal no screen mounted, and a whole task's review missed it (`d1c7248`); assert each entry point opens it.
- The autopay toggle's copy is the rewritten one, not the design's: `The bank pays this one — we'll still ask you to confirm it went out`.
- A 409 on a name held by an **archived** bill offers restore rather than showing a bare error — the `BudgetModal.tsx` pattern.
- Choosing a pay-from account in a different currency from the bill's current one shows the server's 422 inline, naming both currencies, and keeps the modal open.

- [ ] **Step 2: Run to verify they fail; implement; run to verify they pass**

Run: `cd web && npx vitest run src/features/money/BillModal.test.tsx`

Fields, in the design's order: Bill name, Amount, Repeats, Next due, Category, Pay from, Paid by, On autopay, Counts as a subscription.

- [ ] **Step 3: Commit**

```bash
git add web/src/features/money/
git commit -m "feat(bills): add and edit a bill"
```

---

### Task 14: Frontend — Mark paid, and undo

**Files:**
- Create: `web/src/features/money/MarkPaidModal.tsx`
- Create: `web/src/features/money/MarkPaidModal.test.tsx`
- Modify: `web/src/features/money/BillRow.tsx`

**Interfaces:**
- Consumes: `useMarkPaid`, `useUndoPayment`.

- [ ] **Step 1: Write the failing tests**

- Prefills the bill's amount and today's date, and posts an edited amount unchanged by the prefill.
- Names what it will do — writes an expense — so the double-entry cost of spec decision 1 is stated where a household could incur it.
- Undo sits behind an **in-page confirmation, never `window.confirm`** (the house rule; `GoalContributionsPanel.tsx` is the pattern).
- Undo on an older payment surfaces the server's 409 inline, naming the payment that can be undone.

- [ ] **Step 2: Run to verify they fail; implement; run to verify they pass**

Run: `cd web && npx vitest run src/features/money/MarkPaidModal.test.tsx`

- [ ] **Step 3: Commit**

```bash
git add web/src/features/money/
git commit -m "feat(bills): mark paid and undo"
```

---

### Task 15: Frontend — the subscriptions panel

**Files:**
- Create: `web/src/features/money/SubscriptionsCard.tsx`
- Create: `web/src/features/money/SubscriptionsCard.test.tsx`
- Modify: `web/src/features/money/BillsPage.tsx`

- [ ] **Step 1: Write the failing tests**

- Lists the ticked bills with the monthly total in the heading and the annual figure beneath.
- A quarterly or yearly subscription renders its own amount and cadence, and the panel says the totals are monthly equivalents — a household seeing `S$120` beside `Insurance` in a monthly total needs to know which number is which.
- **No "last reviewed" line is rendered.** Nothing can set that date; the design draws no control for it.
- Empty state when nothing is ticked, explaining how a bill becomes a subscription.

- [ ] **Step 2: Run to verify they fail; implement; run to verify they pass**

Run: `cd web && npx vitest run src/features/money/SubscriptionsCard.test.tsx`

- [ ] **Step 3: Commit**

```bash
git add web/src/features/money/
git commit -m "feat(bills): subscriptions rollup"
```

---

### Task 16: Frontend — Overview's Next bill card and the quick-add entry

**Files:**
- Create: `web/src/features/overview/NextBillCard.tsx`
- Create: `web/src/features/overview/NextBillCard.test.tsx`
- Modify: `web/src/features/overview/OverviewPage.tsx`
- Modify: `web/src/features/overview/AddMenu.tsx` (or whichever file holds the "+ Add" menu — check before editing)

**Interfaces:**
- Consumes: `useBills` and `billsQueryKey` — **the same hook and the same cache entry the Bills page itself uses**, which is what `GoalsCard` does and why the card moves without a reload after a write.

- [ ] **Step 1: Read `OverviewPage.tsx`'s member-state guard before writing anything**

`GET /bills` is gated `money` **and** owner, so `useBills` **403s for a limited member** — Overview is about to acquire a fourth failing query, and the limited-member panel is currently gated on `accounts.isSuccess`. Read that guard first; do not add a query to this page without knowing what decides whether the panel renders.

This is not hypothetical. The interim Overview's one real defect was exactly this shape: a limited member holding `money` saw a page containing the word "Overview" and nothing else. Every unit test passed against it, before and after, because each test covering that member asserted the **absence** of something — and absence holds perfectly over a blank page (`docs/LEARNING.md` pattern 2).

- [ ] **Step 2: Write the failing tests**

- Renders the design's line: `S$142.30` over `SP utilities · Jul 8`.
- An overdue next bill says so rather than printing a past date as though it were upcoming.
- A household with no bills renders the card's own empty line, never a zero.
- `+ Add → Bill` opens `BillModal`, saves, and moves the card with no reload.
- The entry is **disabled with its reason until an account exists** — a bill needs a pay-from account, the same precondition `+ Add → Transaction` already carries.
- **A limited member still gets the limited-member panel**, with the bills query failing alongside the others. Assert on what is *present* — the panel's own text — not only on what is missing, or the test agrees with a blank page.
- **`NextBillCard` renders nothing on a 403**, not an error region and not a blank card with a heading above it.

- [ ] **Step 3: Run to verify they fail; implement; run to verify they pass**

Run: `cd web && npx vitest run src/features/overview/`

- [ ] **Step 4: Commit**

```bash
git add web/src/features/overview/
git commit -m "feat(overview): next bill card and the bill quick add"
```

---

### Task 17: Docs — the three that must not go stale

**Files:**
- Modify: `docs/FEATURE_TRACKER.md`
- Modify: `docs/SYSTEM_DESIGN.md`
- Modify: `docs/LEARNING.md`
- Modify: `docs/HANDOVER.md`

- [ ] **Step 1: `docs/SYSTEM_DESIGN.md`, via the `maintaining-system-design` skill**

Invoke the skill; do not hand-edit around it. What changed structurally: seven new routes and their guards (§4), two new tables (§5's data diagram), a new port, and — the one worth prose rather than a box — **a request flow that writes into `transactions` from outside Transactions**. Say why that is allowed here and what keeps it honest: the currency comes from `AccountLookup`, the same rule `TransactionService.Create` applies, and a test asserts the two agree.

- [ ] **Step 2: `docs/FEATURE_TRACKER.md`**

Move Bills' four rows ⬜ → ✅. Move Overview's "Next bill card" ⬜ → ✅ and update the "+ Add" row (four of six entries live now). Add new rows for what the design never draws: **undo a payment**, **archive and restore a bill**, and **the Unattributed row on Spending by person** — each with a sentence saying why it has no mockup, the way the Accounts and Goals sections already do.

**The Navigation shell section changes too, and it is easy to miss.** Its "Placeholder pages for unbuilt areas" row names `/money/$` as one of the two left; Task 11 deleted that route, so `/` is the only one remaining. Fix the row's prose, not just its symbol.

**Recount the summary table by counting symbols**, not by adjusting the stated totals. Count the first symbol in each row's own cell, whether the cell is a bare `| ✅ |` or has prose after it. This file records that estimating those numbers produced two errors that cancelled.

- [ ] **Step 3: `docs/LEARNING.md`**

One entry per thing this work taught. At minimum: Go's `AddDate` normalising 31 January to 3 March, and why the anchor day is a column rather than a derived value — the second bug is the one that hides behind the first, because clamping alone looks correct until the month after.

- [ ] **Step 4: `docs/HANDOVER.md`**

Slice 2 is complete. Say what the next person picks up: Marriage, and that Bills has now closed Family's dependency.

- [ ] **Step 5: Commit**

```bash
git add docs/
git commit -m "docs: record Bills in the system design, tracker and handover"
```

---

### Task 18: Walk the definition of done in a real browser

**Files:**
- Create: `docs/superpowers/plans/2026-08-09-hearth-bills-verification.md`
- Create: `docs/superpowers/plans/2026-08-09-hearth-bills-screenshots/`

Tests passing is not the claim. `CLAUDE.md` requires this, the product owner asked for it explicitly, and every walk in this project so far has found something the suite agreed with.

- [ ] **Step 1: Start clean**

```bash
make down && make up   # 00008 is a new migration; see Global Constraints
make seed
```

- [ ] **Step 2: Run all fifteen criteria, recording each**

1. A household with no bills sees the first-run state, not an empty table.
2. `+ Add bill` creates a monthly bill; it appears under Due soon with its category, autopay badge and account.
3. Mark paid writes a real expense — **open the Transactions page and see the row**, with the bill's name as its description — and Budget's `Spent` for the month moves by exactly that amount.
4. Spending by person shows the payment under the bill's payer; with the payer cleared, it shows under **Unattributed**, and the rows still sum to `Spent`.
5. **Archive a live bill from its own row**, find it under "Show archived", restore it, and confirm it is back in Due soon. Goals shipped this feature with no way in; walk it, do not infer it.
6. `next_due` advanced by one month **from the due date**, not from the date you clicked — pay one deliberately late and check.
7. A bill due on the 31st, paid, lands on 28 February and then on **31 March**, not 28 March. This is the anchor-day behaviour; it needs two payments to be visible.
8. Undo removes the payment, removes the transaction from the ledger, and rewinds the due date.
9. Undo on an older payment is refused, naming the one that can be undone.
10. An overdue autopay bill and an overdue manual bill read differently.
11. All-caught-up state appears once every bill due this month is paid, and names the next one.
12. A subscription-flagged bill appears in the panel; the monthly and annual figures agree (`× 12`).
13. A bill on a second-currency account converts into primary in the stat cards, and a currency with no rate is **excluded and counted on screen**.
14. A limited member holding `money` is refused the Bills page — reads included — **and then loads Overview as that same member** and sees the limited-member panel, not a page with a heading and nothing under it. Bills gives Overview a fourth failing query; this is the criterion that proves it did not blank the page (`docs/LEARNING.md` pattern 2).
15. Overview's Next bill card shows the earliest unpaid bill and moves without a reload after `+ Add → Bill`.

**Assert on numbers, not screenshots** — `getBoundingClientRect()` or `innerText` read in page script. Screenshots are the record, not the evidence, and two byte-identical before/after files mean the change did not land (`shasum -a 256` them).

- [ ] **Step 3: Fix whatever the walk finds, mid-walk, with its own test**

Every walk in this project has found at least one thing. Budget found two, Goals found one that made a shipped feature unreachable. Fix it, pin it with a test, and record both in the verification file.

- [ ] **Step 4: Commit the record**

```bash
git add docs/superpowers/plans/2026-08-09-hearth-bills-verification.md docs/superpowers/plans/2026-08-09-hearth-bills-screenshots/
git commit -m "docs: record the Bills verification walkthrough"
```

---

## Definition of done for this plan

- `make lint && make test` green.
- The `NextDue` clamp mutation seen red and restored (Task 2, Step 6), plus the two supporting mutations in Tasks 5, 7 and 8.
- `docs/FEATURE_TRACKER.md`, `docs/SYSTEM_DESIGN.md`, `docs/LEARNING.md` and `docs/HANDOVER.md` all updated in Task 17, not afterwards.
- The 15-criterion browser walk recorded with its result, including anything it found.
- Slice 2 (Money) complete: Accounts, Transactions, Budget, Goals, Bills.
