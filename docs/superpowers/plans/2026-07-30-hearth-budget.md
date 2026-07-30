# Budget Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Slice 2's third feature — the Budget page, Edit-budget modal, Budget history, and full category management, per `docs/superpowers/specs/2026-07-30-hearth-budget-design.md`.

**Architecture:** Clean architecture as everywhere in this repo: `domain` holds the pinned formulas as pure functions, `usecase` composes them through two ports (`BudgetRepository` new, `CategoryRepository` grown), `adapter/postgres` implements them, `adapter/http` gates them `money`+owner, and the React frontend renders one `GET /budgets/{month}` response. Budgets are per-month parent+lines rows; the whole month saves in one `PUT`.

**Tech Stack:** Go 1.25 / chi / pgx / goose / testcontainers; React + TypeScript + TanStack Router + Vitest.

## Global Constraints

Copied from the spec and `CLAUDE.md`; every task's requirements include these.

- Money is `int64` minor units + ISO 4217 code, everywhere. No `float64` in a monetary path.
- Caps and expected income are in the household's **primary currency** and carry **no currency column** (spec decision 9).
- Reads and writes both gated `money` capability **and** owner (spec decision 8).
- Every 2xx except 204 carries a JSON body; `apiFetch` throws otherwise.
- Fail closed on values not constructed here: `switch` over wire/DB values needs a refusing `default`.
- Adapters map missing rows to `domain.ErrNotFound`; no `pgx` type crosses out of `adapter/postgres`.
- `make lint-arch` applies to test files too: `domain` imports stdlib only; `usecase` adds `domain`.
- Frontend tests stub the network with `web/src/test/fetchStub.ts`'s `stubFetchRoutes`, which throws on unregistered requests.
- The rollover toggle, "4 of 4 on track", Overview's budget card, Export CSV and over-spend alert emails are **out of scope** (spec's Out-of-scope table). Do not build them.
- Commit messages: conventional prefixes (`feat:`, `test:`, `refactor:`, `docs:`), as `git log` shows.
- Run the Go suite from `api/`: `go test ./...` (needs a Docker socket; see `docs/HANDOVER.md` §2). Frontend: `cd web && npx vitest run`.

### The pinned formulas (spec, verbatim contract)

All in primary currency; "the month" is the viewed calendar month.

| Figure | Formula |
|---|---|
| Spent | Expense-kind only, converted per transaction then summed (`MonthSummary`'s exact rule); no-rate transactions excluded and counted |
| Budgeted | Sum of caps |
| Remaining | Budgeted − Spent, may be negative, never clamped |
| Percent used | Spent ÷ Budgeted to nearest whole percent; hidden when Budgeted = 0 |
| Days left | (last day of month − today) + 1; past month → 0; future month → days in that month |
| Daily pace | Remaining ÷ days left, floored; only when Remaining > 0 and month is current |
| On pace to save | = Remaining; only when Remaining > 0 and month is current |
| Per-category over | category spent > cap |
| Left to allocate | expected income − Budgeted; only when income set |
| History result | closed months: Spent − Budgeted signed; current month "so far" |
| Months under budget | of closed months **with a budget row**: those with Spent ≤ Budgeted |

---

### Task 1: Split `api_test.go` by feature area

**Files:**
- Modify: `api/internal/adapter/http/api_test.go` (2036 lines → shared harness only)
- Create: `api/internal/adapter/http/auth_api_test.go`, `household_api_test.go`, `accounts_api_test.go`, `transactions_api_test.go`

**Interfaces:**
- Consumes: the existing test harness (`newTestServer`-style helpers, route-walk matrix runner) already in `api_test.go`.
- Produces: the same helpers, exported within the package from `api_test.go`; feature blocks moved verbatim into the four new files. Task 9 and 10 add `budget_api_test.go` beside them.

This is spec decision 11 and the handover's own pending item, done **before** Budget adds a fifth block. Pure movement — no test's name, body or assertion changes.

- [ ] **Step 1: Confirm the suite is green before touching anything**

Run: `cd api && go test ./internal/adapter/http/`
Expected: PASS. Record the test count from `-v | grep -c '^=== RUN'` — the count after the split must be identical.

- [ ] **Step 2: Move each feature area's tests into its own file**

Keep in `api_test.go`: package clause, shared harness types, helpers, and the route-walk matrix runner every block uses. Move into the new files: the auth tests (sign-in, magic link, lockout, sign-up), household/member/space/settings tests, accounts tests, transactions+categories tests. Each new file starts with only `package httpadapter` and the imports its tests need (run `goimports` rather than hand-pruning).

- [ ] **Step 3: Verify nothing changed**

Run: `cd api && go test ./internal/adapter/http/ -v | grep -c '^=== RUN'`
Expected: PASS with the identical count from Step 1. Also run `make lint`.

- [ ] **Step 4: Commit**

```bash
git add api/internal/adapter/http/
git commit -m "refactor: split api_test.go by feature area before Budget adds a fifth block"
```

---

### Task 2: Migration `00006_budgets.sql`

**Files:**
- Create: `api/migrations/00006_budgets.sql`
- Test: `api/internal/adapter/postgres/schema_test.go` (extend)

**Interfaces:**
- Produces: tables `budgets` and `budget_lines` exactly as below; every later task's SQL relies on these names and constraints.

- [ ] **Step 1: Write the migration**

```sql
-- +goose Up

-- budgets is one household-month's plan. The row's existence IS "a budget is
-- set for this month" (spec decision 7): the empty state is one lookup, and a
-- month whose caps were all removed (a parent with zero lines) stays
-- distinguishable from a month never budgeted.
--
-- month is always the first of the month, the same convention
-- TransactionRepository.MonthTotals takes. Caps and expected income are in
-- the household's primary currency and deliberately carry no currency column:
-- a cap is a plan, not a transaction (spec decision 9). Changing the
-- household's primary currency changes what these numbers mean -- the same
-- accepted trade-off the accounts currency-change screen documents. Do not
-- "fix" this by adding a currency column.
CREATE TABLE budgets (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id          uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    month                 date        NOT NULL,
    -- NULL means the household chose not to say, and hides the income and
    -- left-to-allocate cards. It never defaults to zero: zero is a claim.
    expected_income_minor bigint,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, month)
);

-- budget_lines is one category's cap inside one month's budget. ON DELETE
-- CASCADE from budgets: replacing a month's budget deletes and rewrites its
-- lines inside one transaction (BudgetRepository.Upsert). No cascade from
-- categories -- a category referenced by a line archives, never deletes,
-- the same reasoning accounts use.
CREATE TABLE budget_lines (
    id          uuid   PRIMARY KEY DEFAULT gen_random_uuid(),
    budget_id   uuid   NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
    category_id uuid   NOT NULL REFERENCES categories(id),
    cap_minor   bigint NOT NULL CHECK (cap_minor >= 0),
    UNIQUE (budget_id, category_id)
);

-- +goose Down
DROP TABLE budget_lines;
DROP TABLE budgets;
```

- [ ] **Step 2: Extend the schema test**

`schema_test.go` already asserts each table's columns and constraints against a migrated database. Add cases for both new tables: column set, `UNIQUE (household_id, month)`, `UNIQUE (budget_id, category_id)`, the `cap_minor >= 0` check, and the two cascades (`budgets`→household CASCADE, `budget_lines`→budget CASCADE, `budget_lines`→category NO ACTION). Follow the file's existing table-case shape exactly.

- [ ] **Step 3: Run and verify, including Down**

Run: `cd api && go test ./internal/adapter/postgres/ -run TestSchema -v`
Expected: PASS. Then verify the Down migration actually runs (the handover notes no test has ever run one): with the dev stack up, `docker compose exec api go run github.com/pressly/goose/v3/cmd/goose@v3.27.3 -dir migrations postgres "$DATABASE_URL" down && ... up` — or apply/rollback/apply through the migrate container. Expected: both directions clean.

- [ ] **Step 4: Commit**

```bash
git add api/migrations/00006_budgets.sql api/internal/adapter/postgres/schema_test.go
git commit -m "feat: budgets and budget_lines tables"
```

---

### Task 3: Domain — the pinned formulas as pure functions

**Files:**
- Create: `api/internal/domain/budget.go`
- Test: `api/internal/domain/budget_test.go`

**Interfaces:**
- Produces (Task 8 consumes these exact names):

```go
type BudgetLine struct {
    CategoryID string
    Cap        Money
}

type Budget struct {
    ID             string
    HouseholdID    string
    Month          time.Time // first of month
    ExpectedIncome *Money    // nil = not provided
    Lines          []BudgetLine
}

// DaysLeftInMonth: (last day − today) + 1 for the current month (today
// counts — you can still spend today); 0 for a past month; the whole month's
// length for a future month. month is any instant in the month; today is a
// date.
func DaysLeftInMonth(month, today time.Time) int

// PercentUsed: spent/budgeted to nearest whole percent. ok=false when
// budgetedMinor == 0 — the screen hides the figure, it never shows NaN.
func PercentUsed(spentMinor, budgetedMinor int64) (pct int, ok bool)

// DailyPace: remaining/daysLeft floored to a whole minor unit. ok=false when
// remainingMinor <= 0 or daysLeft <= 0 — hidden, not zero.
func DailyPace(remainingMinor int64, daysLeft int) (paceMinor int64, ok bool)
```

Remaining and Budgeted are plain sums the service does with `Money.Add`; they need no domain function. `Budget` carries `Money` so a cap can never exist without its currency being the primary one the service constructed it with.

- [ ] **Step 1: Write the failing tests**

```go
package domain

import (
	"testing"
	"time"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// The spec's table, row by row. Each case name quotes the rule it pins.
func TestDaysLeftInMonth(t *testing.T) {
	cases := []struct {
		name  string
		month time.Time
		today time.Time
		want  int
	}{
		{"first of a 31-day month: the whole month", date(2026, time.July, 1), date(2026, time.July, 1), 31},
		{"mid-month: today still counts", date(2026, time.July, 1), date(2026, time.July, 19), 13},
		{"last day: one day left", date(2026, time.July, 1), date(2026, time.July, 31), 1},
		{"past month: zero", date(2026, time.June, 1), date(2026, time.July, 19), 0},
		{"future month: its whole length", date(2026, time.September, 1), date(2026, time.July, 19), 30},
		{"February in a non-leap year", date(2026, time.February, 1), date(2026, time.February, 1), 28},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DaysLeftInMonth(tc.month, tc.today); got != tc.want {
				t.Fatalf("DaysLeftInMonth = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPercentUsed(t *testing.T) {
	cases := []struct {
		name             string
		spent, budgeted  int64
		wantPct          int
		wantOK           bool
	}{
		{"the design's own figures round to 66", 342000, 520000, 66, true},
		{"zero budgeted hides the figure, never NaN", 342000, 0, 0, false},
		{"over 100 stays literal", 600000, 520000, 115, true},
		{"rounds to nearest, not down", 335000, 520000, 64, true}, // 64.42 -> 64
		{"half rounds up", 130000, 520000, 25, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pct, ok := PercentUsed(tc.spent, tc.budgeted)
			if ok != tc.wantOK || (ok && pct != tc.wantPct) {
				t.Fatalf("PercentUsed = %d,%v want %d,%v", pct, ok, tc.wantPct, tc.wantOK)
			}
		})
	}
}

func TestDailyPace(t *testing.T) {
	cases := []struct {
		name      string
		remaining int64
		daysLeft  int
		wantPace  int64
		wantOK    bool
	}{
		{"the design's own figures floor to 136 whole units", 178000, 13, 13692, true}, // 178000/13 = 13692.3 minor
		{"exact division", 130000, 13, 10000, true},
		{"nothing remaining hides the card", 0, 13, 0, false},
		{"overspent hides the card", -5000, 13, 0, false},
		{"past month (zero days) hides the card", 178000, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pace, ok := DailyPace(tc.remaining, tc.daysLeft)
			if ok != tc.wantOK || (ok && pace != tc.wantPace) {
				t.Fatalf("DailyPace = %d,%v want %d,%v", pace, ok, tc.wantPace, tc.wantOK)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd api && go test ./internal/domain/ -run 'TestDaysLeft|TestPercentUsed|TestDailyPace' -v`
Expected: FAIL — `undefined: DaysLeftInMonth` etc.

- [ ] **Step 3: Implement**

```go
package domain

import "time"

// (Budget and BudgetLine types as in the Interfaces block above, each with a
// doc comment saying what it is; ExpectedIncome's comment states that nil
// means "not provided" and hides the income cards — it never defaults to 0.)

// DaysLeftInMonth is the spec's pinned rule: today counts, because the
// household can still spend today. A past month has no days left; a future
// month has all of them. Comparison is by calendar month, not by instant, so
// "today at 23:59" and "today at 00:00" agree.
func DaysLeftInMonth(month, today time.Time) int {
	mStart := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	tStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	daysIn := mStart.AddDate(0, 1, -1).Day()
	switch {
	case mStart.Before(tStart):
		return 0
	case mStart.After(tStart):
		return daysIn
	default:
		return daysIn - today.Day() + 1
	}
}

// PercentUsed rounds to the nearest whole percent, half away from zero — the
// same rounding Money already uses. ok=false when nothing is budgeted; the
// caller hides the figure rather than showing NaN or infinity.
func PercentUsed(spentMinor, budgetedMinor int64) (int, bool) {
	if budgetedMinor == 0 {
		return 0, false
	}
	return int((spentMinor*100 + budgetedMinor/2) / budgetedMinor), true
}

// DailyPace floors: telling a household it can spend S$137/day when the true
// figure is 136.92 overshoots by month end; flooring never does.
func DailyPace(remainingMinor int64, daysLeft int) (int64, bool) {
	if remainingMinor <= 0 || daysLeft <= 0 {
		return 0, false
	}
	return remainingMinor / int64(daysLeft), true
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd api && go test ./internal/domain/ -run 'TestDaysLeft|TestPercentUsed|TestDailyPace' -v`
Expected: PASS.

- [ ] **Step 5: Mutation-check the two designated tests (proving-tests-can-fail)**

The spec designates these two likeliest drifts. Break, watch red, restore, watch green:
1. In `DaysLeftInMonth`, change `- today.Day() + 1` to `- today.Day()` (the off-by-one). Expected: "today still counts" and "last day" cases fail.
2. In `PercentUsed`, drop the `+ budgetedMinor/2` rounding term. Expected: "rounds to nearest" case fails.

- [ ] **Step 6: Commit**

```bash
git add api/internal/domain/budget.go api/internal/domain/budget_test.go
git commit -m "feat: budget domain types and the spec's pinned pace formulas"
```

---

### Task 4: Ports — `BudgetRepository`, `CategoryRepository` writes

**Files:**
- Modify: `api/internal/usecase/ports.go`
- Modify: `api/internal/usecase/testdouble_test.go` (grow the in-memory doubles)

**Interfaces:**
- Produces (Tasks 5–8 implement/consume these exact signatures):

```go
type BudgetRepository interface {
	// Get returns one household-month's budget. domain.ErrNotFound means the
	// month has never been budgeted — callers translate that to the empty
	// state, not an error. month is any instant in the month.
	Get(ctx context.Context, householdID string, month time.Time) (domain.Budget, error)
	// Upsert replaces the month's budget wholesale in one transaction:
	// parent row upserted on (household_id, month), lines deleted and
	// rewritten. Full-replace, never merge — the modal always holds the
	// entire budget, and replace makes removed rows unambiguous. b.ID and
	// line IDs are ignored; the database assigns them.
	Upsert(ctx context.Context, b domain.Budget) (domain.Budget, error)
	// History returns the budgets for the closed months in [from, month),
	// plus the viewed month if budgeted — newest first, months without a
	// budget row simply absent, never zero-filled.
	History(ctx context.Context, householdID string, month time.Time, months int) ([]domain.Budget, error)
}
```

And on `CategoryRepository`:

```go
	// Create adds one category at the end of the household's sort order.
	// A name colliding with UNIQUE (household_id, name) — archived rows
	// included — surfaces as domain.ErrCategoryNameTaken.
	Create(ctx context.Context, c domain.Category) (domain.Category, error)
	// Rename changes the name only, same collision contract as Create.
	// domain.ErrNotFound when the id is not this household's.
	Rename(ctx context.Context, householdID, categoryID, name string) (domain.Category, error)
	// SetArchived stamps or clears archived_at. Archiving is idempotent,
	// keeps every transaction and budget line referencing the row, and is
	// the only removal that exists — there is no delete.
	SetArchived(ctx context.Context, householdID, categoryID string, archived bool) (domain.Category, error)
```

- New sentinel in `api/internal/domain/errors.go`: `ErrCategoryNameTaken = errors.New("category name taken")`, beside the existing sentinels.

- [ ] **Step 1: Add the interfaces and doc comments** — exactly as above; `ports.go`'s doc comments are load-bearing, copy them verbatim.

- [ ] **Step 2: Grow the in-memory doubles** in `testdouble_test.go` following the file's existing map-backed shape (`map[string]domain.Budget` keyed household+month; category doubles gain the three writes with the same collision semantics). Compile-only here; behaviour is exercised by Task 7–8 tests.

- [ ] **Step 3: Verify** — `cd api && go build ./... && go test ./internal/usecase/`
Expected: PASS (nothing consumes the new methods yet). `make lint-arch` clean.

- [ ] **Step 4: Commit**

```bash
git add api/internal/usecase/ports.go api/internal/usecase/testdouble_test.go api/internal/domain/errors.go
git commit -m "feat: BudgetRepository port and category write contracts"
```

---

### Task 5: Postgres `BudgetRepository`

**Files:**
- Create: `api/internal/adapter/postgres/budget_repo.go`
- Test: `api/internal/adapter/postgres/budget_repo_test.go`

**Interfaces:**
- Consumes: Task 2's tables, Task 4's port.
- Produces: `NewBudgetRepo(pool)` returning the implementation `cmd/api/main.go` wires in Task 9.

- [ ] **Step 1: Write the failing tests** (testcontainers, same harness as `transaction_repo_test.go` — reuse its seeded-household helpers):

```go
// Test names are the behaviours; bodies follow the repo test house style.
func TestBudgetUpsertCreatesThenReplaces(t *testing.T)
// Upsert month with lines {groceries:80000, dining:45000}; Get returns both.
// Upsert again with only {groceries:90000}: Get returns exactly one line —
// the dining line is GONE (full-replace), groceries shows the new cap.

func TestBudgetGetUnbudgetedMonthIsErrNotFound(t *testing.T)
// Get a month never budgeted → errors.Is(err, domain.ErrNotFound).

func TestBudgetUpsertIsOneTransaction(t *testing.T)
// Upsert with a second line whose category_id belongs to ANOTHER household
// (FK-valid uuid, wrong household — repo validates ownership in the same
// statement set): expect an error AND Get shows the month unchanged from
// before the call. A partial write — parent updated, lines half-replaced —
// is the exact shape guarding-partial-writes exists for.

func TestBudgetHistorySkipsUnbudgetedMonths(t *testing.T)
// Budget May and July, not June. History(July, months=6) returns July and
// May only — no zero-filled June row.

func TestBudgetExpectedIncomeNullRoundTrips(t *testing.T)
// Upsert with ExpectedIncome nil → Get returns nil, not a zero Money.
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/adapter/postgres/ -run TestBudget -v` → FAIL, `undefined: NewBudgetRepo`.

- [ ] **Step 3: Implement.** One file, pgx like its siblings. Upsert inside `pgx.BeginFunc`: (1) verify every line's category belongs to the household with one `SELECT count(*) ... WHERE id = ANY($1) AND household_id = $2` and refuse on mismatch; (2) `INSERT INTO budgets ... ON CONFLICT (household_id, month) DO UPDATE SET expected_income_minor = EXCLUDED.expected_income_minor, updated_at = now() RETURNING id`; (3) `DELETE FROM budget_lines WHERE budget_id = $1`; (4) batch insert the new lines. Money columns ↔ `domain.Money` conversion uses the household's primary currency, read inside the same transaction, so a `Budget` never leaves the adapter with a currency the service didn't ask for. `pgx.ErrNoRows` → `domain.ErrNotFound` at this boundary, as everywhere.

- [ ] **Step 4: Run to verify pass** — same command. Expected: PASS.

- [ ] **Step 5: Mutation-check the full-replace test**: change the implementation's `DELETE FROM budget_lines` to a no-op; `TestBudgetUpsertCreatesThenReplaces` must fail on the surviving dining line. Restore, green.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/postgres/budget_repo.go api/internal/adapter/postgres/budget_repo_test.go
git commit -m "feat: postgres BudgetRepository with transactional full-replace upsert"
```

---

### Task 6: Postgres category writes

**Files:**
- Modify: `api/internal/adapter/postgres/category_repo.go`
- Test: `api/internal/adapter/postgres/category_repo_test.go` (extend)

**Interfaces:**
- Consumes: Task 4's three method contracts.

- [ ] **Step 1: Write the failing tests**

```go
func TestCategoryCreateAppendsToSortOrder(t *testing.T)
// Create "Helper's salary" in a seeded household → row exists, kind expense,
// sort_order = max(existing)+1, List returns it last.

func TestCategoryCreateDuplicateNameIsErrCategoryNameTaken(t *testing.T)
// Create "Groceries" (a starter name) → errors.Is(err, domain.ErrCategoryNameTaken).
// Also against an ARCHIVED "Groceries": archived rows keep their unique key.

func TestCategoryRenameKeepsEverythingElse(t *testing.T)
// Rename groceries → "Food"; kind, sort_order, archived_at untouched.
// Rename to an existing sibling's name → ErrCategoryNameTaken.
// Rename an id from another household → domain.ErrNotFound.

func TestCategoryArchiveAndRestore(t *testing.T)
// SetArchived true stamps archived_at; List(includeArchived=false) omits it,
// List(includeArchived=true) has it; a transaction referencing it keeps its
// category_id; SetArchived false clears the stamp. Archiving twice is not an
// error and does not move the original stamp forward — assert first stamp wins.
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/adapter/postgres/ -run TestCategory -v` → FAIL, undefined methods.

- [ ] **Step 3: Implement.** `Create`: `INSERT ... sort_order = (SELECT COALESCE(MAX(sort_order),0)+1 FROM categories WHERE household_id=$1)`; map the `UNIQUE (household_id, name)` violation (pgx `SQLSTATE 23505` on that constraint name) to `domain.ErrCategoryNameTaken` — check the constraint name, not just the code, so a future unique key doesn't masquerade. `Rename`: `UPDATE ... WHERE id=$1 AND household_id=$2 RETURNING ...`, no rows → `ErrNotFound`, same 23505 mapping. `SetArchived`: `archived_at = CASE WHEN $3 THEN COALESCE(archived_at, now()) ELSE NULL END` — the COALESCE is the "first stamp wins" rule.

- [ ] **Step 4: Run to verify pass.** Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/internal/adapter/postgres/category_repo.go api/internal/adapter/postgres/category_repo_test.go
git commit -m "feat: category create, rename, archive and restore in postgres"
```

---

### Task 7: Usecase — category writes on `CategoryService`

**Files:**
- Modify: `api/internal/usecase/category.go`
- Test: `api/internal/usecase/category_test.go` (extend)

**Interfaces:**
- Produces (Task 10's handlers call these):

```go
func (s *CategoryService) Create(ctx context.Context, householdID, name string) (domain.Category, error)
func (s *CategoryService) Rename(ctx context.Context, householdID, categoryID, name string) (domain.Category, error)
func (s *CategoryService) Archive(ctx context.Context, householdID, categoryID string) (domain.Category, error)
func (s *CategoryService) Restore(ctx context.Context, householdID, categoryID string) (domain.Category, error)
```

- [ ] **Step 1: Failing tests** against the in-memory double: `Create` trims whitespace and refuses an empty name (`domain.ErrValidation` shape the service already uses); new categories are always `CategoryExpense` kind (budget caps envelope spending; an income category has no cap — state this in a comment); `Rename` refuses empty the same way; `Archive`/`Restore` pass through. Collision and not-found arrive from the repo and pass through untranslated — assert `errors.Is` still holds through the service.

- [ ] **Step 2: Run, verify FAIL. Step 3: Implement (thin orchestration, validation only — no seeding side effects; seeding stays where it is). Step 4: Run, verify PASS.**

- [ ] **Step 5: Commit**

```bash
git add api/internal/usecase/category.go api/internal/usecase/category_test.go
git commit -m "feat: category create, rename, archive on CategoryService"
```

---

### Task 8: Usecase — `BudgetService`

**Files:**
- Create: `api/internal/usecase/budget.go`
- Test: `api/internal/usecase/budget_test.go`

**Interfaces:**
- Consumes: `BudgetRepository`, `TransactionRepository.MonthTotals`, `CategoryRepository.List`, `HouseholdRepository.Get`, `MembershipRepository` (the existing listing the member handlers use for names), the FX provider — all through `Deps`, mirroring `TransactionService`'s construction.
- Produces (Task 9's handlers consume):

```go
type BudgetCategoryView struct {
	CategoryID   string
	CategoryName string
	Archived     bool
	Cap          domain.Money
	Spent        domain.Money
	Over         bool
}

type BudgetPersonView struct {
	MembershipID string
	Name         string
	Spent        domain.Money
}

type BudgetMonthView struct {
	Currency       string
	Month          time.Time
	Budget         *domain.Budget // nil = never set (empty state)
	Categories     []BudgetCategoryView
	Budgeted       domain.Money
	Spent          domain.Money   // MonthSummary's exact rule
	Remaining      int64          // minor units; may be negative
	PercentUsed    int
	PercentOK      bool
	DaysLeft       int
	DailyPace      int64
	DailyPaceOK    bool
	ByPerson       []BudgetPersonView
	ExcludedNoRate []ExcludedTransaction
	OverCount      int
}

type BudgetHistoryMonth struct {
	Month    time.Time
	Budgeted domain.Money
	Spent    domain.Money
	Closed   bool // false only for the viewed (current) month
}

func (s *BudgetService) Month(ctx context.Context, householdID string, month, today time.Time) (BudgetMonthView, error)
func (s *BudgetService) Save(ctx context.Context, householdID string, month time.Time, expectedIncomeMinor *int64, lines []BudgetLineInput) (domain.Budget, error)
func (s *BudgetService) History(ctx context.Context, householdID string, month, today time.Time, months int) ([]BudgetHistoryMonth, error)

type BudgetLineInput struct {
	CategoryID string
	CapMinor   int64
}
```

`today` is a parameter, never `time.Now()` inside the service — the clock port is how every service here gets time, and it is what makes the days-left tests deterministic.

- [ ] **Step 1: Failing tests** against in-memory doubles. The behaviours, each its own test:

```go
func TestBudgetMonthComposesTheDesignsFigures(t *testing.T)
// Seed caps groceries 800.00 / dining 450.00; expenses groceries 640.00,
// dining 465.00 (all primary currency). Assert: Budgeted 1250.00, Spent
// 1105.00, Remaining 14500 minor, PercentUsed 88 (ok), dining Over true with
// OverCount 1, groceries Over false.

func TestBudgetMonthSpentReusesTheMonthSummaryRule(t *testing.T)
// Add an income transaction and a transfer to the month: neither moves
// Spent. Add an expense in a currency with no rate: excluded from Spent AND
// from its category's figure, present in ExcludedNoRate. This is the spec's
// "reused exactly" claim, tested rather than assumed.

func TestBudgetMonthUnbudgetedStillReportsSpend(t *testing.T)
// No budget row: Budget nil, Categories still lists categories with spend
// (cap zero Money, Over false), Spent real. The screen shows what was spent
// before caps exist.

func TestBudgetMonthArchivedCategoryWithCapStillRenders(t *testing.T)
// Cap on a category, then archive it: its BudgetCategoryView stays, Archived
// true. History stays true (spec decision 5).

func TestBudgetMonthGroupsSpendByPerson(t *testing.T)
// Expenses paid by two memberships plus one with PaidByMembershipID "":
// two named rows with converted totals; the unattributed spend gets no row.

func TestBudgetSaveValidates(t *testing.T)
// Duplicate CategoryID in lines → ErrValidation before the repo sees it;
// negative cap → ErrValidation; unknown category → the repo double's error
// passes through; nil expected income round-trips as nil.

func TestBudgetHistoryMarksOnlyTheCurrentMonthOpen(t *testing.T)
// Budgets in May, June, July (today mid-July): July row Closed false,
// others true; unbudgeted April absent entirely.
```

- [ ] **Step 2: Run, verify FAIL. Step 3: Implement.** `Month` reads household (primary currency), budget (ErrNotFound → nil, not error), categories (includeArchived=true — archived caps must render), `MonthTotals` once; converts each expense with the same per-transaction convert-then-add order `MonthSummary` uses (`docs/LEARNING.md` pattern 12); groups by category id and by `PaidByMembershipID`; fills the domain functions from Task 3. Membership names come from the same repository listing the member handlers use. `Save` validates then delegates to `Upsert`, constructing each cap as `domain.NewMoney(capMinor, primary)` so a bad currency cannot exist. **Step 4: Run, verify PASS.**

- [ ] **Step 5: Mutation-check the reuse test**: in the service's spent loop, delete the `Kind != TransactionExpense` guard — `TestBudgetMonthSpentReusesTheMonthSummaryRule` must go red on the income transaction. Restore, green.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/budget.go api/internal/usecase/budget_test.go
git commit -m "feat: BudgetService month view, save and history"
```

---

### Task 9: HTTP — budget routes

**Files:**
- Create: `api/internal/adapter/http/budget_handlers.go`
- Modify: `api/internal/adapter/http/router.go` (inside the existing `txn` group — the one already stacking `requireCapability(domain.CapMoney)` + `requireOwner`), `cmd/api/main.go` (wire `NewBudgetRepo`, `BudgetService`)
- Test: `api/internal/adapter/http/budget_api_test.go` (new, beside Task 1's split files)

**Interfaces:**
- Consumes: Task 8's service methods.
- Produces: the wire contract the frontend (Task 11) parses:

```
GET /api/v1/budgets/2026-07        -> 200 budgetMonthResponse
PUT /api/v1/budgets/2026-07        -> 200 {"budget": {...}}   (CSRF)
GET /api/v1/budgets/history?months=6 -> 200 {"months":[...]}
```

```jsonc
// budgetMonthResponse — null budget is the empty state, spent figures real:
{
  "currency": "SGD", "month": "2026-07",
  "budget": {                       // or null
    "expectedIncomeMinor": 910000,  // or null
    "lines": [{"categoryId": "...", "capMinor": 80000}]
  },
  "categories": [{"categoryId": "...", "name": "Groceries", "archived": false,
                  "capMinor": 80000, "spentMinor": 64000, "over": false}],
  "budgetedMinor": 520000, "spentMinor": 342000, "remainingMinor": 178000,
  "percentUsed": 66, "percentOk": true,
  "daysLeft": 13, "dailyPaceMinor": 13692, "dailyPaceOk": true,
  "byPerson": [{"membershipId": "...", "name": "Paula", "spentMinor": 161000}],
  "excludedNoRate": 2, "overCount": 1
}
```

- [ ] **Step 1: Failing tests.** Route-walk matrix rows for all three routes (the transactions matrix shape: unauthenticated 401, other-household 404-shaped isolation, non-owner 403, member-without-money 403, PUT without CSRF 403). Then behaviour: GET of an unbudgeted month is `200` with `"budget": null` and real spent figures (test seeds an expense, no budget); PUT with a duplicate categoryId 422s and a follow-up GET shows nothing written; PUT round-trips (save then GET returns the lines); malformed month segment `2026-7` → 400; `months` param clamped to [1, 24]; PUT with `expectedIncomeMinor: null` keeps the cards-hidden state.

- [ ] **Step 2: Run, verify FAIL. Step 3: Implement.** Month parsing: `time.Parse("2006-01", chi.URLParam(r, "month"))`, else `WriteError(w, 400, "INVALID_MONTH", ...)`. `today` from `deps.Clock.Now()`. DTO mapping only — no arithmetic in handlers. Errors through `MapDomainError`; add `ErrCategoryNameTaken` → 409 `CATEGORY_NAME_TAKEN` to its table while here (Task 10 asserts it). Routes:

```go
txn.Get("/budgets/{month}", handleGetBudgetMonth(deps))
txn.Get("/budgets/history", handleBudgetHistory(deps))
txn.Group(func(w chi.Router) {
    w.Use(requireCSRF)
    w.Put("/budgets/{month}", handlePutBudgetMonth(deps))
})
```

(`/budgets/history` registered in the same group; chi routes the literal segment `history` and the `{month}` pattern without ambiguity — the GET test for both proves it.)

- [ ] **Step 4: Run, verify PASS** — `go test ./internal/adapter/http/ -run TestBudget -v`, then the whole package.

- [ ] **Step 5: Commit**

```bash
git add api/internal/adapter/http/ cmd/api/main.go
git commit -m "feat: budget month, save and history routes gated money+owner"
```

---

### Task 10: HTTP — category write routes

**Files:**
- Modify: `api/internal/adapter/http/category_handlers.go`, `router.go`
- Test: `api/internal/adapter/http/budget_api_test.go` (extend — categories are Budget's screen)

**Interfaces:**
- Consumes: Task 7's service methods.
- Produces:

```
POST  /api/v1/categories               {"name": "Helper's salary"} -> 201 {"category": {...}}
PATCH /api/v1/categories/{id}          {"name": "Food"}            -> 200 {"category": {...}}
POST  /api/v1/categories/{id}/archive                              -> 200 {"category": {...}}
POST  /api/v1/categories/{id}/restore                              -> 200 {"category": {...}}
```

All inside the same `money`+owner group, CSRF on all four (writes). `categoryDTO` gains `"archived": bool` — the existing list handler keeps omitting archived rows unless `?includeArchived=true`, which the modal passes.

- [ ] **Step 1: Failing tests**: matrix rows for the four routes; duplicate name → 409 `CATEGORY_NAME_TAKEN` with the modal-facing message; archive→list omits/includes correctly; restore undoes; rename of another household's id → the not-found shape the matrix pins.

- [ ] **Step 2–4: Run FAIL → implement (thin handlers, decodeJSONBody, MapDomainError) → run PASS.**

- [ ] **Step 5: Commit**

```bash
git add api/internal/adapter/http/
git commit -m "feat: category create, rename, archive and restore routes"
```

---

### Task 11: Frontend — schemas, hook, route, sidebar

**Files:**
- Create: `web/src/features/money/budgetSchemas.ts`, `web/src/features/money/useBudget.ts`
- Modify: `web/src/routes/router.tsx` (budget route as a sibling of `moneyTransactionsRoute`), `web/src/features/shell/Sidebar.tsx` (`SPACE_PAGES.money` gains `{ label: "Budget", to: "/money/budget" }`)
- Test: `web/src/features/money/useBudget.test.ts`, `web/src/routes/router.test.tsx` (extend), `web/src/features/shell/Sidebar.test.tsx` (extend)

**Interfaces:**
- Consumes: Task 9/10's wire contract.
- Produces: `budgetSchemas.ts` zod parsers (`budgetMonthResponseSchema`, `budgetHistoryResponseSchema`, `categorySchema`) matching Task 9's JSON exactly, and `useBudget(month: string)` returning `{ data, loading, error, reload, save, createCategory, renameCategory, archiveCategory, restoreCategory }` — fetch orchestration lives here, not in the page (spec decision 11). `save(body)` PUTs and reloads; the category calls hit their routes then reload.

- [ ] **Step 1: Failing tests**: hook test with `stubFetchRoutes` — GET on mount for the given month, `save` PUTs the exact body then re-GETs; router test "redirects a member without the money capability away from /money/budget" (copy the transactions redirect test's shape — and mutation-check it the same way that one was); sidebar test: money group renders three links, Budget's active state is route-driven via `useMatchRoute` like its siblings (the activeProps cascade defect, LEARNING pattern 3, must not come back).

- [ ] **Step 2–4: FAIL → implement → PASS.** Route nests under `moneyGuardRoute`, sibling of `moneyTransactionsRoute`, above the splat.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/money/budgetSchemas.ts web/src/features/money/useBudget.ts web/src/features/money/useBudget.test.ts web/src/routes/ web/src/features/shell/
git commit -m "feat: budget route, sidebar link and fetch hook"
```

---

### Task 12: Frontend — BudgetPage set state

**Files:**
- Create: `web/src/features/money/BudgetPage.tsx`, `web/src/features/money/BudgetStatCards.tsx`, `web/src/features/money/BudgetCategoryGrid.tsx`, `web/src/features/money/BudgetByPerson.tsx`, `web/src/features/money/budgetCopy.ts`
- Test: `web/src/features/money/BudgetPage.test.tsx`

**Interfaces:**
- Consumes: `useBudget`; `formatMoney.ts` for every figure (minor units in, string out — the page never does float math).
- Produces: `BudgetPage` (the route component), month state lifted here (`YYYY-MM` string), `onMonthChange` passed to the picker; opens the Task 14 modal via local state.

- [ ] **Step 1: Failing tests** (all with `stubFetchRoutes` registering `GET /api/v1/budgets/2026-07`):

- renders the four stat cards from the response's minor units (Budgeted S$5,200.00 etc. — assert via testids, exact formatted strings)
- an over category shows the over state and copy; the insight card says "On pace to save" only when `dailyPaceOk`
- percent hidden when `percentOk` false; pace card hidden when `dailyPaceOk` false
- past month (stub a month where `daysLeft: 0`): pace and on-pace absent, "so far" language dropped
- `excludedNoRate > 0` renders the ledger's exclusion copy shape with the count
- archived category line renders with an archived marker
- by-person rows render names and formatted totals
- **no rollover sentence anywhere** — assert the string "rolls into" is absent (spec decision 1, pinned so a future copy-paste from the design can't smuggle it in)

- [ ] **Step 2–4: FAIL → implement → PASS.** `BudgetPage` is composition only: header + month picker + states; each card component takes parsed props. Copy lives in `budgetCopy.ts` like `transactionCopy.ts`.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/money/
git commit -m "feat: Budget page set state — stat cards, category grid, by-person"
```

---

### Task 13: Frontend — empty state and templates

**Files:**
- Create: `web/src/features/money/budgetTemplates.ts`
- Modify: `web/src/features/money/BudgetPage.tsx`
- Test: `web/src/features/money/BudgetPage.test.tsx` (extend)

**Interfaces:**
- Produces: `budgetTemplates.ts` exporting `familyOfFourTemplate(categories: Category[]): TemplatePrefill`, `fiftyThirtyTwentyTemplate(categories: Category[], incomeMinor: number): TemplatePrefill`, where `TemplatePrefill = { expectedIncomeMinor: number | null; lines: { categoryId: string; capMinor: number }[]; missing: string[] }`. Templates map by **category name** onto the household's real category list (they never invent ids); a template name with no matching live category lands in `missing` and the modal shows it as a suggested add. Family-of-four caps are the design's own Budget screen numbers in primary-currency minor units (Groceries 80000, Dining out 45000, Kids & school 60000, Insurance 42000, Utilities 32000, Transport 30000, Petrol 25000, Household 25000, Giving 20000, Fun & hobbies 20000). 50/30/20 splits income: 50% across the needs set (Groceries, Utilities, Transport, Insurance, Kids & school, Household, Petrol, Health — proportional to the family-of-four weights), 30% across wants (Dining out, Fun & hobbies, Giving — same rule), 20% left unallocated; each line rounds down to a whole minor unit so the split never exceeds income.

- [ ] **Step 1: Failing tests**: empty state renders when `budget: null` (design copy, "Create your first budget", template cards); "Import last month" card absent when the previous month's GET (stubbed) has `budget: null`, present when it has one; template functions are pure — unit-test the mapping, the `missing` behaviour, and that 50/30/20's line sum ≤ income with 20% headroom.

- [ ] **Step 2–4: FAIL → implement → PASS.** Template clicks open the modal prefilled (Task 14's props); nothing PUTs until Save. The 50/30/20 card is the one template that cannot prefill caps without income: it opens the modal with the income field focused and a one-line prompt ("Enter your expected income and we'll split it 50/30/20"), and computes its lines the moment income is entered — a test pins that no lines exist while income is blank and that they appear, summing ≤ income, once it is typed.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/money/
git commit -m "feat: budget empty state and prefill-only templates"
```

---

### Task 14: Frontend — Edit-budget modal

**Files:**
- Create: `web/src/features/money/BudgetModal.tsx`
- Test: `web/src/features/money/BudgetModal.test.tsx`

**Interfaces:**
- Consumes: shared `components/Modal` (genuine `:modal`; no declarative `open` attribute — HANDOVER §4), `useBudget`'s `save` + category calls, `TransactionModal`'s money-input approach (minor units, no floats — reuse its input component/helpers rather than writing a second one).
- Produces: `BudgetModal({ month, initial, categories, onClose, onSaved })` where `initial` is either the month's current budget or a `TemplatePrefill`.

Behaviour (from the spec, all tested):
- Three cards: expected income editable and blankable; Allocated live-sums the rows; Left to allocate = income − allocated, the pair hidden when income blank, negative shown as negative.
- One row per line: name is an editable input (rename queued, applied on save), cap input, ✕ removes the row (cap only), archive control queues archive.
- "+ Add a category": dropdown of active categories without a cap this month, plus "New category…" inline name → queues a create.
- Save runs queued category creates/renames/archives **first** (their own endpoints), then one `PUT` with the full line set. Any failure keeps the modal open, shows the error inline (409 name collision shows the taken name), and does **not** fire the remaining calls.
- Cancel discards everything queued.

- [ ] **Step 1: Failing tests** with `stubFetchRoutes`: save order (create → PUT — assert via the stub's recorded call order); 409 on create keeps modal open, no PUT fired; ✕ row removal reflected in the PUT body (removed category absent); blank income → `expectedIncomeMinor: null` in the body and hidden cards; add-category dropdown excludes already-capped and archived categories; rename lands as PATCH before PUT.

- [ ] **Step 2–4: FAIL → implement → PASS.**

- [ ] **Step 5: Mutation-check the save-order test**: swap the implementation to PUT-first; the order test must fail. Restore.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/money/BudgetModal.tsx web/src/features/money/BudgetModal.test.tsx
git commit -m "feat: edit-budget modal with queued category writes and one PUT"
```

---

### Task 15: Frontend — history modal and month picker

**Files:**
- Create: `web/src/features/money/BudgetHistoryModal.tsx`
- Modify: `web/src/features/money/BudgetPage.tsx` (picker wiring)
- Test: `web/src/features/money/BudgetHistoryModal.test.tsx`, `BudgetPage.test.tsx` (extend)

**Interfaces:**
- Consumes: `GET /budgets/history?months=6` via `useBudget`; the page's lifted month state.
- Produces: `BudgetHistoryModal({ months, onPickMonth, onClose })` — clicking a month row calls `onPickMonth("2026-05")` which closes the modal and switches the page's picker (the design's "full breakdown" is the page itself).

- [ ] **Step 1: Failing tests**: three summary cards computed from the response (avg spend and avg saved over **closed** months only; months under budget "5 of 6" counts closed budgeted months); current month row marked "so far"; unbudgeted months simply absent (stub a gap, assert no zero row); row click calls `onPickMonth` and the page then GETs the picked month; picker ‹ › moves one month and the URL/GET follows; **no Export CSV control** (deferred — assert absent, same reason as Task 12's rollover assertion).

- [ ] **Step 2–4: FAIL → implement → PASS.**

- [ ] **Step 5: Commit**

```bash
git add web/src/features/money/
git commit -m "feat: budget history modal and month picker"
```

---

### Task 16: Docs — the three that must not go stale

**Files:**
- Modify: `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`, `docs/HANDOVER.md`

Use the `maintaining-system-design` skill for `SYSTEM_DESIGN.md`: the `budgets`/`budget_lines` pair in the data section, the seven new routes in the route table with their `money`+owner guards, `BudgetService` and the grown `CategoryService` in the component diagram, and prose under the diagram for the two non-obvious shapes (full-replace PUT; caps carrying no currency column).

`FEATURE_TRACKER.md`: Budget's five rows updated — "Envelope per category with pace" ✅, "Empty state with templates" ✅, "Spending by person" ✅, "Edit budget (modal)" ✅, "Budget history (modal)" 🟡 *(Export CSV deferred — apiFetch's JSON-only contract, transactions decision 7)*. Add rows the design draws inside Budget that this slice resolves: "Edit categories (rename, add, archive)" ✅ under Categories; "Roll unspent into savings" ⬜ *(deferred whole to Goals — spec decision 1)*. Recount the summary table by counting symbols, not adjusting.

`LEARNING.md`: whatever the tasks actually taught; at minimum, if any review or walk finds a defect, its pattern entry.

`HANDOVER.md`: slice 2 row now "Accounts, Transactions, Budget built; Goals, Bills not started"; "what to settle before Goals" inherits the two figures still unpinned ("4 of 4 on track", rollover mechanics).

- [ ] **Step 1: Update all four. Step 2: `make lint && make test` green. Step 3: Commit**

```bash
git add docs/
git commit -m "docs: record Budget in the system design, tracker and handover"
```

---

### Task 17: Walk the definition of done

**Files:**
- Create: `docs/superpowers/plans/2026-07-30-hearth-budget-verification.md`

Start from nothing (`make down && docker volume rm hearth_hearth-pgdata && make up && make seed`), real browser at `http://localhost:5173`. **Arithmetic dry-run note (LEARNING pattern 13):** criteria 5–7 below share one prepared month; each states the totals it expects *after* the walk's own earlier steps, and no criterion asserts a counter an earlier criterion has moved without saying so.

1. Sign in as Andreas; the MONEY sidebar group shows Finances, Transactions, **Budget**; Budget's link colours only on its own route.
2. `/money/budget` on the seeded (unbudgeted) household shows the empty state: design copy, "Create your first budget", the two seed templates — and **no** "Import last month" card (June has no budget; the dead template never renders).
3. "Create your first budget" opens the modal blank: income card empty, Allocated S$0.00, Left-to-allocate hidden while income is blank.
4. Family-of-four template prefills the modal with the design's ten caps mapped onto the household's real categories; nothing is saved until Save (close without saving → still the empty state).
5. Set a real budget: income 9,100.00, caps Groceries 800.00 and Dining out 450.00 only (drop the template's other rows with ✕). Save. The page shows Budgeted S$1,250.00, Spent as whatever the seeded July transactions put in those two categories — read the Transactions page's own figures first and write the expected numbers down *before* saving.
6. Log a new S$60.00 Dining out expense dated today from the Transactions page; Budget's Dining figure and Spent both move by exactly 60.00; if that pushes Dining past 450.00, the over state and OverCount sentence appear.
7. Percent, Remaining, Daily pace agree with the pinned formulas computed by hand from criterion 5+6's own totals and today's real date (write the arithmetic in the record).
8. Edit the budget: rename Groceries to "Food" inline, add a new category "Arisan" with a 100.00 cap, archive "Petrol". Save. The grid shows Food and Arisan; the transaction modal's dropdown now lacks Petrol; old Petrol transactions keep their label in the ledger.
9. Attempt a duplicate: add-category "New category…" with name "Food" → inline 409 message, modal stays open, nothing half-saved (reload: previous state intact).
10. Month picker ‹ to June: empty state (June unbudgeted), header says June, no fake zeros. › back to July: budget intact. Forward to August: caps absent, "Import last month" card present (July now has a budget).
11. August's "Import last month" prefills July's caps; save; History modal now lists August "so far" and July closed with its signed result; June absent, not zero.
12. History row click on July closes the modal and switches the page to July.
13. An IDR expense with FX auto rate counts in Spent converted; then switch FX handling to a state with no rate for a currency (per the accounts walk's method) and confirm the exclusion line appears with a count rather than a silently short total.
14. A limited member granted `money` via Settings: `/money/budget` refuses (reads are owner-gated); the sidebar never offered the link.
15. `PUT` without CSRF (curl, session cookie only) → 403; `GET /api/v1/budgets/2026-7` → 400 INVALID_MONTH.

- [ ] **Step 1: Run the walk, recording PASS/FAIL per criterion as you go. Step 2: `make lint && make test`, paste summary lines. Step 3: Fix anything found (its own commit(s), with tests). Step 4: Commit the record**

```bash
git add docs/superpowers/plans/2026-07-30-hearth-budget-verification.md
git commit -m "docs: record the Budget verification walkthrough"
```

---

## Definition of done for this plan

Every task's tests green under `make lint && make test`; the designated mutations in Tasks 3, 5, 8, 11 and 14 each seen red then green; the Task 17 walk recorded with every criterion passing or its failure fixed and re-walked; the three documents updated in Task 16 — before any of this is called finished.
