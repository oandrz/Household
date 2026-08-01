# Goals Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Slice 2's fourth feature — the Savings goals page, its New/Edit goal modal, a contributions ledger, and the manual budget rollover that finally gives Budget's unspent money somewhere to go, per `docs/superpowers/specs/2026-08-01-hearth-goals-design.md`.

**Architecture:** Clean architecture as everywhere in this repo. `internal/domain/goal.go` holds the pinned status arithmetic as pure functions over values; `internal/usecase/goal.go` composes the screen through one new narrow port (`GoalRepository`) plus the household and FX ports it already needs; `internal/adapter/postgres` implements it with sqlc-generated queries; `internal/adapter/http` gates every route `money` + owner. A goal's progress is a sum over `goal_contributions` — never an account balance, never a sum over transactions. The React frontend renders one `GET /goals` response.

**Tech Stack:** Go 1.25 / chi / pgx / sqlc / goose / testcontainers; React + TypeScript + TanStack Router + TanStack Query + zod + Vitest.

## Global Constraints

Copied from the spec and `CLAUDE.md`; every task's requirements include these.

- Money is `int64` minor units + ISO 4217 code, everywhere. No `float64` in a monetary path, on either side of the stack.
- **A goal carries an explicit currency** (spec decision 5), unlike a budget. Contributions have no currency of their own — they are in their goal's currency by construction. Only cross-goal totals convert, and they convert per goal then add (`docs/LEARNING.md` pattern 12).
- **Nothing in this feature runs on a clock.** No scheduler, no cron, no month-end job, no automatic transfer. Every contribution is written because a person clicked. Copy must not imply otherwise — see the copy table below.
- Reads and writes are both gated `money` capability **and** owner (spec decision 10), the Budget/Transactions shape.
- Every 2xx except 204 carries a JSON body; `apiFetch` throws on an ok response it cannot parse. `DELETE` answers 204 with no body, the one exemption (`handleDeleteTransaction`'s own comment).
- Fail closed on values not constructed here: a `switch` over a wire or database value needs a refusing `default`.
- Adapters map missing rows to `domain.ErrNotFound`; no `pgx` type crosses out of `adapter/postgres`.
- `make lint-arch` applies to test files too: `domain` imports stdlib only; `usecase` adds `domain`.
- No service takes an actor parameter. `today` is always a parameter, never `time.Now()` inside a service (`BudgetDeps`' own doc comment says why).
- Frontend tests stub the network with `web/src/test/fetchStub.ts`'s `stubFetchRoutes`, which throws on an unregistered request.
- Commit messages: conventional prefixes (`feat:`, `test:`, `refactor:`, `docs:`).
- Run the Go suite from `api/`: `go test ./...` (needs a Docker socket; see `docs/HANDOVER.md` §2). Frontend: `cd web && npx vitest run`. Regenerate typed queries with `make sqlc`.
- **Out of scope, do not build:** automatic contributions or any scheduler, automatic month-end rollover, contributions as real account transfers, the design's "Fund from" account select (spec decision 6), goal reminder emails, Bills and Overview's Next bill card.

### The pinned formulas (spec, verbatim contract)

`contributed` = sum of a goal's contributions in the goal's own currency; `remaining = max(0, target − contributed)`. **"Today" is `deps.Clock.Now()`, resolved in the handler and passed down**; month arithmetic truncates in UTC, as `domain/budget.go` already does.

| Figure | Formula |
|---|---|
| Progress % | `contributed ÷ target` to the nearest whole percent, capped at 100 for the ring; negative `contributed` renders 0 |
| Achieved | `contributed ≥ target`, derived on read, never stored |
| `monthsLeft` | Whole calendar months from the current month to `target_month`, **inclusive of both ends**: Aug 2026 → Dec 2026 = 5; same month = 1; never ≤ 0 |
| Required monthly | `remaining ÷ monthsLeft`, rounded **up** to the whole minor unit |
| On track | dated, unarchived, unachieved, and `required ≤ planned monthly` |
| Behind | same inputs, `required > planned`. A target month before the current month and unachieved is Behind **with no division performed** |
| No status | `target_month IS NULL`, or archived |
| "X of Y on track" | Y counts unarchived, dated, unachieved goals; Y = 0 hides the figure |
| Next goal | earliest `target_month` among dated, unarchived, unachieved goals; ties by name |
| Planned monthly total | `SUM(planned_monthly)` over unarchived goals, converted to primary; no-rate goals excluded and counted |
| Actual this month | contributions with `occurred_on` in the current month, unarchived goals only, **excluding `source = 'starting_balance'`**, converted and excluded the same way |
| Unspent available to roll | a **closed** month's `Remaining` (Budgeted − Spent) when positive |

### The copy the design asserts and this feature does not ship

| Design | Ships as |
|---|---|
| "S$2,050 auto-saved on the 1st of each month" | "S$2,050 planned each month · S$1,200 added in August" |
| "next transfer Aug 1" | dropped; the actual figure replaces it |
| "Auto-save each month" (modal) | "Planned each month" |
| "Unspent budget rolls into the Bali trip goal at month end" | "S$1,780 unspent in July · Move it into a goal" |
| "S$350/mo · from OCBC Joint" | "S$350/mo" |

### Three deviations from the spec, deliberate and visible

The spec is approved; these three implement it more faithfully to house precedent than its own sketch did. Anyone reviewing a task against the spec will meet them, so they are stated once here rather than argued three times.

1. **Archive and restore are their own routes** (`POST /goals/{id}/archive`, `POST /goals/{id}/restore`), not `archivedAt` on `PATCH`. `router.go`'s own comment gives the reason for accounts and categories: if archiving were patchable, an ordinary rename that happened to include the field would archive as a side effect of saving a name. The spec's PATCH sketch predates that reading.
2. **`goal_contributions` carries `source_budget_month date`.** The spec requires that deleting a rollover contribution clears that month's stamp in the same transaction. Finding *which* month to clear from a note string would be guesswork; this column makes the lookup exact and lets a partial unique index refuse a second rollover of one month at the database level.
3. **The rollover contribution's `note` is stored empty.** The spec says the note names the month; the month is now a column, and user-facing copy belongs in the frontend (`goalCopy.ts`), not composed in a Go handler. The card renders "From July's unspent budget" from `source` + `sourceBudgetMonth`.

---

### Task 1: Migration `00007_goals.sql`

**Files:**
- Create: `api/migrations/00007_goals.sql`
- Test: `api/internal/adapter/postgres/schema_test.go` (extend)

**Interfaces:**
- Produces: tables `goals` and `goal_contributions`, and two new columns on `budgets`, exactly as below. Every later task's SQL relies on these names and constraints.

- [ ] **Step 1: Write the migration**

```sql
-- +goose Up

-- goals is one household's savings target. Unlike budgets (00006), a goal
-- carries an explicit currency: a budget is one month's plan and a
-- primary-currency change restating it was an accepted cost, while a goal
-- accumulates for years and the same silence would restate a multi-year total
-- and every contribution behind it. accounts stores currency per row for this
-- exact reason. Do not "fix" this by dropping the column.
--
-- target_month is NULL for a goal with no target date -- the design's own
-- Emergency fund ("6 months expenses", no date). A dateless goal shows
-- progress and carries no on-track status at all, because the status rule
-- divides by months left and there is no honest number to divide by.
CREATE TABLE goals (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id          uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name                  text        NOT NULL,
    target_amount_minor   bigint      NOT NULL CHECK (target_amount_minor > 0),
    currency              char(3)     NOT NULL,
    -- Always the first of the month, the same convention budgets.month and
    -- TransactionRepository.MonthTotals take.
    target_month          date,
    planned_monthly_minor bigint      NOT NULL CHECK (planned_monthly_minor >= 0),
    -- A goal is archived, never deleted: contributions reference it, and a
    -- rolled-over budget month names it. The accounts precedent, for the
    -- accounts reason.
    archived_at           timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    -- An archived goal still occupies its name, exactly as an archived
    -- category does. A collision with one offers restore rather than a bare
    -- 409 (see the HTTP task).
    UNIQUE (household_id, name)
);

-- goal_contributions is what a goal's progress is made of. A contribution
-- moves no real money: a goal earmarks, it does not hold, so goal progress and
-- account balances are independent figures and nothing reconciles them (spec
-- decision 1). Do not "fix" this by joining transactions -- that is the
-- larger, later feature the spec's own decision 1 rejected.
--
-- The row carries no currency: it is its goal's, by construction.
CREATE TABLE goal_contributions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    goal_id      uuid        NOT NULL REFERENCES goals(id),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    -- Non-zero rather than positive: a genuine correction downward (a goal the
    -- household raided) is a negative row, while zero is meaningless.
    amount_minor bigint      NOT NULL CHECK (amount_minor <> 0),
    occurred_on  date        NOT NULL,
    note         text        NOT NULL DEFAULT '',
    -- A new source needs a migration. That is deliberate: the Go parser fails
    -- closed on this column too, and both layers refusing an unknown value is
    -- the house pattern (transactions.kind carries the same CHECK).
    source       text        NOT NULL
                             CHECK (source IN ('manual', 'starting_balance', 'budget_rollover')),
    -- Set only on a budget rollover: which month's unspent money this is.
    -- Deleting the row clears that month's stamp on budgets, and finding the
    -- month from a note string would be guesswork.
    source_budget_month date,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT budget_month_is_a_rollover_thing CHECK (
        source = 'budget_rollover' OR source_budget_month IS NULL
    ),
    CONSTRAINT rollover_names_its_month CHECK (
        source <> 'budget_rollover' OR source_budget_month IS NOT NULL
    )
);

CREATE INDEX goal_contributions_goal_idx
    ON goal_contributions (goal_id);

-- The "actual this month" figure walks one household's contributions by date.
CREATE INDEX goal_contributions_household_date_idx
    ON goal_contributions (household_id, occurred_on);

-- Belt and braces beside the conditional UPDATE in RollOverToGoal: even a
-- future code path that forgets the stamp cannot write two rollovers for one
-- household-month.
CREATE UNIQUE INDEX goal_contributions_one_rollover_per_month
    ON goal_contributions (household_id, source_budget_month)
    WHERE source = 'budget_rollover';

-- A month can be rolled over exactly once, and the record survives the goal
-- being archived -- so no ON DELETE clause here: goals are never deleted.
ALTER TABLE budgets
    ADD COLUMN rolled_over_at   timestamptz,
    ADD COLUMN rollover_goal_id uuid REFERENCES goals(id);

ALTER TABLE budgets
    ADD CONSTRAINT rollover_stamp_is_whole CHECK (
        (rolled_over_at IS NULL AND rollover_goal_id IS NULL)
     OR (rolled_over_at IS NOT NULL AND rollover_goal_id IS NOT NULL)
    );

-- +goose Down
ALTER TABLE budgets DROP CONSTRAINT rollover_stamp_is_whole;
ALTER TABLE budgets DROP COLUMN rollover_goal_id;
ALTER TABLE budgets DROP COLUMN rolled_over_at;
DROP TABLE goal_contributions;
DROP TABLE goals;
```

- [ ] **Step 2: Extend the schema test**

`schema_test.go` already asserts each table's columns and constraints against a migrated database; follow its existing table-case shape exactly. Add cases for: both new tables' column sets; `UNIQUE (household_id, name)` on `goals`; the three CHECKs on `goal_contributions` (`amount_minor <> 0`, and the two `source_budget_month` pairings); the partial unique index; `budgets`' two new columns and the `rollover_stamp_is_whole` CHECK; the cascade shapes (`goals`→household CASCADE, `goal_contributions`→goal NO ACTION, `budgets.rollover_goal_id`→goal NO ACTION).

- [ ] **Step 3: Run and verify, including Down**

Run: `cd api && go test ./internal/adapter/postgres/ -run TestSchema -v`
Expected: PASS.

Then run the Down migration for real — no migration in this project has ever been rolled back by a test, which `docs/HANDOVER.md` §5 lists as a standing gap. With the dev stack up: apply, roll back, apply again through the migrate container. Expected: all three clean, and the second apply leaves the same schema the test just asserted.

- [ ] **Step 4: Commit**

```bash
git add api/migrations/00007_goals.sql api/internal/adapter/postgres/schema_test.go
git commit -m "feat: goals and goal_contributions tables, budget rollover stamp"
```

---

### Task 2: Domain — goal types and the pinned status arithmetic

**Files:**
- Create: `api/internal/domain/goal.go`
- Test: `api/internal/domain/goal_test.go`

**Interfaces:**
- Produces (Tasks 3, 6, 7 and 8 consume these exact names):

```go
// GoalStatus is what the card's pill says. "none" is a real state, not a
// missing one: a goal with no target date has nothing to be on track against.
type GoalStatus string

const (
    GoalOnTrack    GoalStatus = "on_track"
    GoalBehind     GoalStatus = "behind"
    GoalAchieved   GoalStatus = "achieved"
    GoalStatusNone GoalStatus = "none"
)

// ContributionSource says where a contribution came from. It arrives from a
// database column, so ParseContributionSource refuses anything else.
type ContributionSource string

const (
    ContributionManual          ContributionSource = "manual"
    ContributionStartingBalance ContributionSource = "starting_balance"
    ContributionBudgetRollover  ContributionSource = "budget_rollover"
)

func ParseContributionSource(s string) (ContributionSource, error)

type Goal struct {
    ID             string
    HouseholdID    string
    Name           string
    Target         Money
    TargetMonth    *time.Time // nil = no target date; else the first of a month
    PlannedMonthly Money
    ArchivedAt     *time.Time
}

func (g Goal) IsArchived() bool

type GoalContribution struct {
    ID                string
    GoalID            string
    HouseholdID       string
    Amount            Money
    OccurredOn        time.Time
    Note              string
    Source            ContributionSource
    SourceBudgetMonth *time.Time // set only when Source is ContributionBudgetRollover
}

// MonthsLeftInclusive counts whole calendar months from today's month to the
// target month, counting both ends: Aug -> Dec is 5, and a target in the
// current month is 1, because the household can still contribute this month.
// A target month already past returns 0, and callers must treat 0 as "behind",
// never as a divisor.
func MonthsLeftInclusive(targetMonth, today time.Time) int

// RequiredMonthlyMinor rounds UP. Rounding down states a figure that does not
// actually reach the target. ok is false when monthsLeft <= 0 — there is no
// honest number, and the caller must not divide.
func RequiredMonthlyMinor(remainingMinor int64, monthsLeft int) (int64, bool)

// GoalProgressPercent is contributed/target to the nearest whole percent,
// capped at 100 for the ring and floored at 0 so a net-negative goal never
// renders a reversed ring.
func GoalProgressPercent(contributedMinor, targetMinor int64) int

// GoalStatusFor is the spec's status table as one function. It never divides
// by a non-positive months-left.
func GoalStatusFor(g Goal, contributedMinor int64, today time.Time) GoalStatus

// GoalRemainingMinor is max(0, target - contributed).
func GoalRemainingMinor(contributedMinor, targetMinor int64) int64
```

- [ ] **Step 1: Write the failing tests**

```go
package domain

import (
	"errors"
	"testing"
	"time"
)

func goalDate(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestMonthsLeftInclusive(t *testing.T) {
	cases := []struct {
		name        string
		target      time.Time
		today       time.Time
		want        int
	}{
		{"four months ahead counts both ends", goalDate(2026, time.December, 1), goalDate(2026, time.August, 1), 5},
		{"the target month itself is one month", goalDate(2026, time.August, 1), goalDate(2026, time.August, 19), 1},
		{"next month is two", goalDate(2026, time.September, 1), goalDate(2026, time.August, 31), 2},
		{"across a year boundary", goalDate(2027, time.January, 1), goalDate(2026, time.November, 1), 3},
		{"a past target month is zero, never negative", goalDate(2026, time.July, 1), goalDate(2026, time.August, 1), 0},
		{"far in the past is still zero", goalDate(2024, time.March, 1), goalDate(2026, time.August, 1), 0},
		{"the day of the month never matters", goalDate(2026, time.December, 1), goalDate(2026, time.August, 31), 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MonthsLeftInclusive(tc.target, tc.today); got != tc.want {
				t.Fatalf("MonthsLeftInclusive = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRequiredMonthlyMinor(t *testing.T) {
	cases := []struct {
		name      string
		remaining int64
		months    int
		want      int64
		wantOK    bool
	}{
		{"exact division", 500000, 5, 100000, true},
		{"rounds up, because rounding down never reaches the target", 500001, 5, 100001, true},
		{"one month left needs the whole remainder", 140000, 1, 140000, true},
		{"nothing remaining needs nothing", 0, 5, 0, true},
		{"no months left has no honest figure", 500000, 0, 0, false},
		{"negative months are refused the same way", 500000, -3, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := RequiredMonthlyMinor(tc.remaining, tc.months)
			if ok != tc.wantOK || (ok && got != tc.want) {
				t.Fatalf("RequiredMonthlyMinor = %d,%v want %d,%v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestGoalProgressPercent(t *testing.T) {
	cases := []struct {
		name                     string
		contributed, target      int64
		want                     int
	}{
		{"the design's Bali trip", 260000, 400000, 65},
		{"rounds to nearest", 129000, 400000, 32}, // 32.25 -> 32
		{"half rounds up", 130000, 400000, 33},    // 32.5 -> 33
		{"over target caps at 100 for the ring", 500000, 400000, 100},
		{"net negative floors at 0, never a reversed ring", -5000, 400000, 0},
		{"zero target cannot happen but must not divide", 1000, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := GoalProgressPercent(tc.contributed, tc.target); got != tc.want {
				t.Fatalf("GoalProgressPercent = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestGoalStatusFor(t *testing.T) {
	sgd := func(a int64) Money { return Money{Amount: a, Currency: "SGD"} }
	dec2026 := goalDate(2026, time.December, 1)
	jul2026 := goalDate(2026, time.July, 1)
	today := goalDate(2026, time.August, 15)
	archived := goalDate(2026, time.August, 1)

	base := Goal{
		Name:           "Bali family trip",
		Target:         sgd(400000),
		TargetMonth:    &dec2026,
		PlannedMonthly: sgd(35000),
	}

	cases := []struct {
		name        string
		goal        Goal
		contributed int64
		want        GoalStatus
	}{
		// remaining 140000 over 5 months = 28000 required <= 35000 planned.
		{"reachable at the planned rate is on track", base, 260000, GoalOnTrack},
		// remaining 300000 over 5 months = 60000 required > 35000 planned.
		{"unreachable at the planned rate is behind", base, 100000, GoalBehind},
		{"required exactly equal to planned is still on track", func() Goal {
			g := base
			g.PlannedMonthly = sgd(28000)
			return g
		}(), 260000, GoalOnTrack},
		{"one minor unit short of the required figure is behind", func() Goal {
			g := base
			g.PlannedMonthly = sgd(27999)
			return g
		}(), 260000, GoalBehind},
		{"contributed past the target is achieved, whatever the date says", base, 400000, GoalAchieved},
		{"a past target date, unachieved, is behind without dividing", func() Goal {
			g := base
			g.TargetMonth = &jul2026
			return g
		}(), 100000, GoalBehind},
		{"a past target date that was met is still achieved", func() Goal {
			g := base
			g.TargetMonth = &jul2026
			return g
		}(), 400000, GoalAchieved},
		{"no target date means no status", func() Goal {
			g := base
			g.TargetMonth = nil
			return g
		}(), 100000, GoalStatusNone},
		{"an archived goal has no status", func() Goal {
			g := base
			g.ArchivedAt = &archived
			return g
		}(), 100000, GoalStatusNone},
		{"a planned monthly of zero cannot be on track while anything remains", func() Goal {
			g := base
			g.PlannedMonthly = sgd(0)
			return g
		}(), 260000, GoalBehind},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := GoalStatusFor(tc.goal, tc.contributed, today); got != tc.want {
				t.Fatalf("GoalStatusFor = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseContributionSourceRefusesAnythingElse(t *testing.T) {
	for _, ok := range []string{"manual", "starting_balance", "budget_rollover"} {
		if _, err := ParseContributionSource(ok); err != nil {
			t.Fatalf("ParseContributionSource(%q) = %v, want nil", ok, err)
		}
	}
	if _, err := ParseContributionSource("automatic"); !errors.Is(err, ErrUnknownContributionSource) {
		t.Fatalf("ParseContributionSource(\"automatic\") = %v, want ErrUnknownContributionSource", err)
	}
	if _, err := ParseContributionSource(""); !errors.Is(err, ErrUnknownContributionSource) {
		t.Fatalf("ParseContributionSource(\"\") = %v, want ErrUnknownContributionSource", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd api && go test ./internal/domain/ -run 'TestMonthsLeft|TestRequiredMonthly|TestGoalProgress|TestGoalStatusFor|TestParseContributionSource' -v`
Expected: FAIL — `undefined: MonthsLeftInclusive` and friends. (`ErrUnknownContributionSource` is added in Task 3's sentinel block; add it here in `errors.go` as part of this task instead if the compiler needs it first — it is one line and belongs with the parser it serves.)

- [ ] **Step 3: Implement**

```go
package domain

import (
	"fmt"
	"time"
)

// (types and constants exactly as in the Interfaces block above, each with the
// doc comment shown there — those comments are the contract, not decoration.)

func ParseContributionSource(s string) (ContributionSource, error) {
	switch ContributionSource(s) {
	case ContributionManual:
		return ContributionManual, nil
	case ContributionStartingBalance:
		return ContributionStartingBalance, nil
	case ContributionBudgetRollover:
		return ContributionBudgetRollover, nil
	default:
		// The default is the point: this value arrives from a database column
		// or a request body, so an unrecognised one is refused rather than
		// carried further, the same rule ParseTransactionKind follows.
		return "", fmt.Errorf("%w: %q", ErrUnknownContributionSource, s)
	}
}

func (g Goal) IsArchived() bool { return g.ArchivedAt != nil }

// MonthsLeftInclusive compares by calendar month, never by instant, so a
// target read at 00:00 and at 23:59 agree. Both ends count: a household can
// still contribute in the target month itself.
func MonthsLeftInclusive(targetMonth, today time.Time) int {
	target := time.Date(targetMonth.Year(), targetMonth.Month(), 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	months := (target.Year()-now.Year())*12 + int(target.Month()) - int(now.Month())
	if months < 0 {
		return 0
	}
	return months + 1
}

// RequiredMonthlyMinor rounds up by adding monthsLeft-1 before dividing, which
// is exact integer arithmetic — no float ever touches a monetary path here.
func RequiredMonthlyMinor(remainingMinor int64, monthsLeft int) (int64, bool) {
	if monthsLeft <= 0 {
		return 0, false
	}
	if remainingMinor <= 0 {
		return 0, true
	}
	m := int64(monthsLeft)
	return (remainingMinor + m - 1) / m, true
}

func GoalRemainingMinor(contributedMinor, targetMinor int64) int64 {
	if contributedMinor >= targetMinor {
		return 0
	}
	return targetMinor - contributedMinor
}

// GoalProgressPercent rounds half away from zero, the same rounding
// PercentUsed uses, and clamps to [0, 100] for the ring.
func GoalProgressPercent(contributedMinor, targetMinor int64) int {
	if targetMinor <= 0 || contributedMinor <= 0 {
		return 0
	}
	pct := int((contributedMinor*100 + targetMinor/2) / targetMinor)
	if pct > 100 {
		return 100
	}
	return pct
}

// GoalStatusFor is the spec's status table, in the order the table states it:
// achieved wins over everything (a goal met early is met), then the two states
// that have no honest arithmetic, then the comparison itself.
func GoalStatusFor(g Goal, contributedMinor int64, today time.Time) GoalStatus {
	if contributedMinor >= g.Target.Amount {
		return GoalAchieved
	}
	if g.IsArchived() || g.TargetMonth == nil {
		return GoalStatusNone
	}
	monthsLeft := MonthsLeftInclusive(*g.TargetMonth, today)
	required, ok := RequiredMonthlyMinor(GoalRemainingMinor(contributedMinor, g.Target.Amount), monthsLeft)
	if !ok {
		// The target month has passed and the goal was not met. Behind is the
		// honest answer, and no division happens to produce it.
		return GoalBehind
	}
	if required <= g.PlannedMonthly.Amount {
		return GoalOnTrack
	}
	return GoalBehind
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd api && go test ./internal/domain/ -v`
Expected: PASS.

- [ ] **Step 5: Mutation-check the two designated tests (proving-tests-can-fail)**

The spec designates these. Break, watch red, restore, watch green. Each must be defended by exactly one guard — if breaking it leaves everything green, the test is not protecting the rule and must be rewritten before moving on (`docs/LEARNING.md` pattern 2).

1. In `MonthsLeftInclusive`, change `return months + 1` to `return months`. Expected red: "the target month itself is one month" and "four months ahead counts both ends".
2. In `GoalStatusFor`, change `required <= g.PlannedMonthly.Amount` to `required < ...`. Expected red: "required exactly equal to planned is still on track" — and *only* that case, which is what proves the boundary case is doing the work.

- [ ] **Step 6: Commit**

```bash
git add api/internal/domain/goal.go api/internal/domain/goal_test.go api/internal/domain/errors.go
git commit -m "feat: goal domain types and the spec's pinned status arithmetic"
```

---

### Task 3: Ports and sentinels

**Files:**
- Modify: `api/internal/usecase/ports.go`, `api/internal/domain/errors.go`
- Modify: `api/internal/usecase/testdouble_test.go` (grow the in-memory doubles)

**Interfaces:**
- Produces (Tasks 4–9 implement or consume these exact signatures):

```go
// GoalRecord is one goal with the only derived figure the repository can
// supply: the sum of its contributions. Every other figure on the screen
// (percent, status, required monthly) is domain arithmetic the service does,
// not something SQL should be asked to know.
type GoalRecord struct {
    Goal             domain.Goal
    ContributedMinor int64
}

// GoalMonthTotal is one goal's contributions inside one calendar month.
type GoalMonthTotal struct {
    GoalID      string
    AmountMinor int64
}

type GoalRepository interface {
    // List returns one household's goals with their contributed totals, newest
    // target first then by name. Archived goals are included only when
    // includeArchived is true.
    List(ctx context.Context, householdID string, includeArchived bool) ([]GoalRecord, error)
    // Get reports domain.ErrNotFound when no goal with this id exists in this
    // household — including when one exists in a different household, which
    // must be indistinguishable from not existing at all.
    Get(ctx context.Context, householdID, goalID string) (GoalRecord, error)
    // Create writes the goal and, when startingBalanceMinor is non-zero, its
    // opening contribution (source starting_balance, dated createdOn) in ONE
    // transaction. A goal whose opening contribution is missing is not a state
    // this port can produce. A name colliding with UNIQUE (household_id, name)
    // — archived rows included — surfaces as domain.ErrGoalNameTaken.
    Create(ctx context.Context, g domain.Goal, startingBalanceMinor int64, createdOn time.Time) (domain.Goal, error)
    // Update replaces every mutable column: name, target, target month
    // (nil clears it), planned monthly. Currency is NOT mutable — see
    // GoalService.Update's own comment. Same collision contract as Create.
    Update(ctx context.Context, g domain.Goal) (domain.Goal, error)
    // SetArchived stamps or clears archived_at. Archiving is idempotent and
    // keeps every contribution and rollover reference intact; there is no
    // delete, the accounts precedent.
    SetArchived(ctx context.Context, householdID, goalID string, archived bool) (domain.Goal, error)
    // AddContribution writes one row. c.ID is ignored; the database assigns
    // it. c.Amount's currency must equal the goal's — the service checks, and
    // the column does not exist to hold a second answer.
    AddContribution(ctx context.Context, c domain.GoalContribution) (domain.GoalContribution, error)
    // DeleteContribution removes one row and, when that row is a
    // budget_rollover, clears its month's rolled_over_at and rollover_goal_id
    // on budgets IN THE SAME TRANSACTION. Leaving the stamp would strand the
    // household: money gone from the goal, a month claiming it rolled over,
    // and 409 on every retry. domain.ErrNotFound when there was nothing to
    // remove.
    DeleteContribution(ctx context.Context, householdID, goalID, contributionID string) error
    // ListContributions returns one goal's contributions, newest first, at
    // most limit rows.
    ListContributions(ctx context.Context, householdID, goalID string, limit int) ([]domain.GoalContribution, error)
    // MonthContributionTotals sums each unarchived goal's contributions inside
    // one calendar month, EXCLUDING source 'starting_balance'. The exclusion is
    // load-bearing and lives here so no caller can forget it: a household
    // creating four goals with existing balances would otherwise read
    // "S$41,200 added in August" for money that never moved.
    MonthContributionTotals(ctx context.Context, householdID string, month time.Time) ([]GoalMonthTotal, error)
}
```

And on `BudgetRepository`:

```go
    // RollOverToGoal writes a budget month's unspent money into a goal as one
    // contribution and stamps the month, in ONE transaction. The stamp is set
    // by a conditional UPDATE (... AND rolled_over_at IS NULL), so a second
    // concurrent call finds no row to update and gets
    // domain.ErrRolloverAlreadyDone rather than writing a second contribution.
    // domain.ErrNotFound when the month has no budget row at all — a state
    // Budget decision 4 makes reachable, since a closed month can have spend
    // and no caps.
    RollOverToGoal(ctx context.Context, in RollOverToGoalInput) (domain.GoalContribution, error)
}

// RollOverToGoalInput is what one rollover needs. Note is deliberately absent:
// the row's note stays empty and the frontend renders "From July's unspent
// budget" from source + sourceBudgetMonth, because user-facing copy does not
// belong in a Go handler.
type RollOverToGoalInput struct {
    HouseholdID string
    Month       time.Time // the budget month being rolled over
    GoalID      string
    Amount      domain.Money
    OccurredOn  time.Time
}
```

New sentinels in `api/internal/domain/errors.go`, beside the budget block:

```go
	// The goal sentinels. GoalService checks each before its repository call,
	// following the per-field convention above rather than a generic
	// validation error, so every 422 carries a field-specific code.
	ErrUnknownContributionSource  = errors.New("unknown contribution source")
	ErrGoalNameRequired           = errors.New("a goal name is required")
	ErrGoalNameTaken              = errors.New("goal name taken")
	ErrGoalTargetNotPositive      = errors.New("a goal's target must be positive")
	ErrGoalPlannedMonthlyNegative = errors.New("a goal's planned monthly amount cannot be negative")
	ErrGoalCurrencyImmutable      = errors.New("a goal's currency cannot be changed")
	ErrGoalArchived               = errors.New("that goal is archived")
	ErrContributionAmountZero     = errors.New("a contribution cannot be zero")

	// The rollover sentinels. Each is a refusal BudgetService.RollOver makes
	// before anything is written; ErrRolloverAlreadyDone can also arrive from
	// the repository's own conditional UPDATE losing a race.
	ErrRolloverMonthOpen         = errors.New("only a closed month can be rolled over")
	ErrRolloverAlreadyDone       = errors.New("that month has already been rolled over")
	ErrRolloverNothingUnspent    = errors.New("that month has nothing unspent to roll over")
	ErrRolloverCurrencyMismatch  = errors.New("only a goal in the household's primary currency can receive a rollover")
```

- [ ] **Step 1: Add the interfaces, the input struct and the sentinels** — exactly as above. `ports.go`'s doc comments are load-bearing; copy them verbatim rather than paraphrasing.

- [ ] **Step 2: Grow the in-memory doubles** in `testdouble_test.go`, following the file's existing map-backed shape. The goal double keeps `map[string]domain.Goal` plus `map[string][]domain.GoalContribution` keyed by goal id, and must reproduce four contracts the Postgres implementation has, or Task 6's tests prove nothing: name collisions (archived rows included) return `ErrGoalNameTaken`; `Create` with a non-zero starting balance writes the opening contribution; `DeleteContribution` on a rollover row clears the stamp the budget double holds; `MonthContributionTotals` excludes `starting_balance`.

- [ ] **Step 3: Verify** — `cd api && go build ./... && go test ./internal/usecase/`
Expected: PASS (nothing consumes the new methods yet). `make lint-arch` clean.

- [ ] **Step 4: Commit**

```bash
git add api/internal/usecase/ports.go api/internal/usecase/testdouble_test.go api/internal/domain/errors.go
git commit -m "feat: GoalRepository port, rollover contract and the goal sentinels"
```

---

### Task 4: Postgres — `GoalRepository` (everything but the rollover write)

**Files:**
- Create: `api/internal/adapter/postgres/queries/goal.sql`, `api/internal/adapter/postgres/goal_repo.go`
- Modify: generated `api/internal/adapter/postgres/sqlcgen/**` (via `make sqlc`, never by hand)
- Test: `api/internal/adapter/postgres/goal_repo_test.go`

**Interfaces:**
- Consumes: Task 1's tables, Task 3's port.
- Produces: `NewGoalRepo(db *DB) *GoalRepo` implementing **the whole `GoalRepository` port**, `DeleteContribution` included, so the package compiles and this task's tests stand on their own. `BudgetRepository.RollOverToGoal` is Task 5's. One branch here cannot be tested yet — `DeleteContribution`'s stamp-clearing path, because no rollover row can exist until Task 5 writes one. That is stated rather than skipped, and Task 5's round-trip test is what covers it; Task 4 is not "done" in the tracker sense until that test exists.

- [ ] **Step 1: Write the queries** in `queries/goal.sql`, sqlc-annotated like `queries/budget.sql`. Needed: `CreateGoal`, `InsertGoalContribution`, `ListGoalsWithTotals` (goals LEFT JOIN a `SUM(amount_minor)` per goal, `COALESCE`d to 0, filtered on `archived_at IS NULL` unless the include flag is set), `GetGoalWithTotal`, `UpdateGoal`, `SetGoalArchived`, `ListGoalContributions`, `MonthContributionTotals` (`WHERE occurred_on >= $month AND occurred_on < $month + interval '1 month' AND source <> 'starting_balance'`, joined to unarchived goals), `DeleteGoalContribution` and `ClearBudgetRollover` (both shown in full in Task 5's Step 3, which is where the invariant they serve is explained). Run `make sqlc` and commit the generated file alongside.

- [ ] **Step 2: Write the failing tests** (testcontainers, same harness as `budget_repo_test.go`; reuse its seeded-household helpers):

```go
func TestGoalCreateWritesTheOpeningContribution(t *testing.T)
// Create a goal with startingBalanceMinor 250000: Get returns
// ContributedMinor 250000 and ListContributions shows exactly one row, source
// starting_balance, dated createdOn.

func TestGoalCreateThatFailsWritesNothingAtAll(t *testing.T)
// The reachable half of the atomicity claim: Create with a name that already
// exists AND a non-zero starting balance. The goal insert fails, so assert
// both that the error is ErrGoalNameTaken and that no contribution row exists
// for that household at all — no orphan pointing at a goal that was never
// written. The other direction (a goal surviving a failed contribution insert)
// has no reachable failure to inject: the only way that insert fails is a
// CHECK violation on amount_minor = 0, which Create never sends because zero
// writes no row. It is guarded by construction — both statements run inside
// one pgx.BeginFunc — and by the schema test's CHECK case, and this comment
// exists so nobody later reads the gap as an oversight.

func TestGoalCreateWithZeroStartingBalanceWritesNoContribution(t *testing.T)
// startingBalanceMinor 0: the goal exists, ListContributions is empty,
// ContributedMinor is 0. Zero is not a contribution; the CHECK would refuse it.

func TestGoalCreateDuplicateNameIsErrGoalNameTaken(t *testing.T)
// Same name twice → errors.Is(err, domain.ErrGoalNameTaken). Then archive the
// first and try again → still ErrGoalNameTaken, because an archived goal
// still occupies its unique key (the categories gotcha, restated).

func TestGoalGetFromAnotherHouseholdIsErrNotFound(t *testing.T)
// A real goal id, the wrong household → domain.ErrNotFound, indistinguishable
// from an id that never existed.

func TestGoalUpdateClearsTargetMonth(t *testing.T)
// Update with TargetMonth nil on a goal that had one → the column is NULL and
// Get returns nil, not the zero time.

func TestGoalArchiveAndRestoreKeepContributions(t *testing.T)
// SetArchived true: List(false) omits it, List(true) includes it, its
// contributions are untouched and ContributedMinor is unchanged. Archiving
// twice does not move the original stamp forward — assert first stamp wins.
// SetArchived false clears it.

func TestGoalMonthContributionTotalsExcludesStartingBalance(t *testing.T)
// A goal created with a starting balance dated this month, plus one manual
// contribution this month and one last month. The total for this month is the
// manual one alone. This is the defect the spec calls out by name: a household
// creating goals with existing balances must not read them as money added.

func TestGoalMonthContributionTotalsExcludesArchivedGoals(t *testing.T)
// A contribution this month on an archived goal does not appear in the totals.

func TestGoalDeleteContributionRemovesExactlyThatRow(t *testing.T)
// Two manual contributions; delete one. The other survives, ContributedMinor
// drops by exactly the deleted amount, and deleting the same id twice reports
// domain.ErrNotFound the second time.

func TestGoalDeleteContributionOfAnotherGoalIsErrNotFound(t *testing.T)
// A real contribution id passed with a different goal's id (same household)
// → domain.ErrNotFound and the row is still there. The pair is checked, not
// just the contribution id.

func TestGoalDeleteContributionFromAnotherHouseholdIsErrNotFound(t *testing.T)
```

- [ ] **Step 3: Run to verify failure** — `cd api && go test ./internal/adapter/postgres/ -run TestGoal -v` → FAIL, `undefined: NewGoalRepo`.

- [ ] **Step 4: Implement** `goal_repo.go`, in the shape `budget_repo.go` uses: `GoalRepo{q *sqlcgen.Queries, pool *pgxpool.Pool}`, `NewGoalRepo(db *DB)`, `pgx.BeginFunc` for `Create` (goal insert, then the opening contribution when non-zero, both inside the one function) and for `DeleteContribution` (the delete, then `ClearBudgetRollover` when the deleted row's source is `budget_rollover` — the branch no test can reach until Task 5 exists, written now so the port is whole). Map SQLSTATE 23505 on `goals_household_id_name_key` to `domain.ErrGoalNameTaken` — check the constraint name, not just the code, so a future unique key cannot masquerade as a name collision. `pgx.ErrNoRows` → `domain.ErrNotFound`. Every `domain.Money` is built with `domain.NewMoney(minor, row.Currency)` so a goal never leaves the adapter without its own currency. `source` from the column goes through `domain.ParseContributionSource` — a value this code did not construct is refused here, not carried up.

- [ ] **Step 5: Run to verify pass** — same command. Expected: PASS.

- [ ] **Step 6: Mutation-check the exclusion**: delete `AND source <> 'starting_balance'` from `MonthContributionTotals`' SQL and re-run; `TestGoalMonthContributionTotalsExcludesStartingBalance` must go red. Restore, green.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/postgres/queries/goal.sql api/internal/adapter/postgres/goal_repo.go api/internal/adapter/postgres/goal_repo_test.go api/internal/adapter/postgres/sqlcgen/
git commit -m "feat: postgres GoalRepository with transactional goal creation"
```

---

### Task 5: Postgres — the rollover write, and the round trip that proves the stamp

**Files:**
- Modify: `api/internal/adapter/postgres/queries/budget.sql`, `api/internal/adapter/postgres/budget_repo.go`
- Test: `api/internal/adapter/postgres/budget_repo_test.go` (extend), `api/internal/adapter/postgres/goal_repo_test.go` (extend)

**Interfaces:**
- Consumes: Task 3's `RollOverToGoalInput`, and Task 4's `GoalRepo.DeleteContribution` (already written, its stamp-clearing branch not yet reachable).
- Produces: `BudgetRepo.RollOverToGoal`, and the tests that close Task 4's one uncovered branch. The write and the delete are one invariant read from two directions — the stamp and the contribution exist together or not at all — which is why the round trip is tested here rather than split.

- [ ] **Step 1: Write the failing tests**

```go
func TestRollOverToGoalWritesContributionAndStampTogether(t *testing.T)
// A budgeted, closed month and a goal. RollOverToGoal writes one contribution
// (source budget_rollover, sourceBudgetMonth = the month, note "") and the
// budgets row now carries rolled_over_at and rollover_goal_id.

func TestRollOverToGoalTwiceIsErrRolloverAlreadyDone(t *testing.T)
// A second call for the same month → errors.Is(err, domain.ErrRolloverAlreadyDone)
// AND no second contribution row exists.

func TestRollOverToGoalWithoutABudgetRowIsErrNotFound(t *testing.T)
// A month that has spend but no budgets row (Budget decision 4 makes this
// reachable) → domain.ErrNotFound, and no contribution is written.

func TestRollOverThenDeleteThenRollOverAgainSucceeds(t *testing.T)
// THE round trip, and the one that matters: roll over, assert stamped; delete
// the rollover contribution, assert the stamp is gone (rolled_over_at AND
// rollover_goal_id both NULL); roll over again, assert it succeeds and there
// is exactly one contribution. A test that asserted only the 409 would pass
// against the stamp-left-behind bug.

func TestDeleteManualContributionLeavesEveryStampAlone(t *testing.T)
// A month rolled into goal A; a manual contribution on goal A deleted. The
// stamp survives — deletion clears a stamp only for the row that set it.

func TestDeleteRolloverClearsOnlyItsOwnMonth(t *testing.T)
// Two closed months, each rolled into the same goal. Delete June's rollover:
// June's stamp is gone, July's is untouched. This is why the contribution
// carries source_budget_month — clearing by goal id alone would unstamp both.
```

- [ ] **Step 2: Run to verify failure** — `cd api && go test ./internal/adapter/postgres/ -run 'TestRollOver|TestDelete' -v` → FAIL.

- [ ] **Step 3: Implement.**

`BudgetRepo.RollOverToGoal`, inside `pgx.BeginFunc`:

```sql
-- name: StampBudgetRollover :one
UPDATE budgets
   SET rolled_over_at = now(), rollover_goal_id = $3, updated_at = now()
 WHERE household_id = $1 AND month = $2 AND rolled_over_at IS NULL
RETURNING id;
```

Zero rows is ambiguous by itself — the month may not exist, or may already be stamped — so distinguish with one follow-up `SELECT` inside the same transaction: no row at all → `domain.ErrNotFound`; a row with a stamp → `domain.ErrRolloverAlreadyDone`. Then insert the contribution with `source = 'budget_rollover'`, `source_budget_month = $month`, `note = ''`. The partial unique index from Task 1 is the second line of defence, and a 23505 on `goal_contributions_one_rollover_per_month` also maps to `ErrRolloverAlreadyDone` — a concurrent pair must not surface as a 500.

`GoalRepo.DeleteContribution` was written in Task 4; its two queries are shown here because this is the task where the invariant they serve is actually proven. Inside `pgx.BeginFunc`:

```sql
-- name: DeleteGoalContribution :one
DELETE FROM goal_contributions
 WHERE id = $1 AND goal_id = $2 AND household_id = $3
RETURNING source, source_budget_month;

-- name: ClearBudgetRollover :exec
UPDATE budgets
   SET rolled_over_at = NULL, rollover_goal_id = NULL, updated_at = now()
 WHERE household_id = $1 AND month = $2;
```

No rows deleted → `domain.ErrNotFound`. When the deleted row's source is `budget_rollover`, run `ClearBudgetRollover` for its `source_budget_month` in the same transaction. Both statements or neither — that is the whole point of the task.

- [ ] **Step 4: Run to verify pass.** Expected: PASS.

- [ ] **Step 5: Mutation-check the round trip**: make `DeleteContribution` skip `ClearBudgetRollover` (delete the branch). `TestRollOverThenDeleteThenRollOverAgainSucceeds` must go red on the second rollover; the plain delete tests must stay green — that contrast is what proves the round-trip test is the one carrying the invariant. Restore, green.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/postgres/
git commit -m "feat: transactional budget rollover and stamp-clearing contribution delete"
```

---

### Task 6: Usecase — `GoalService`

**Files:**
- Create: `api/internal/usecase/goal.go`
- Test: `api/internal/usecase/goal_test.go`

**Interfaces:**
- Consumes: `GoalRepository`, `HouseholdRepository.Get` (primary currency), `FXRateProvider` — through `GoalDeps`, mirroring `BudgetDeps`.
- Produces (Task 8's handlers consume these):

```go
// GoalView is one card: the stored goal plus every derived figure the screen
// shows. RequiredMonthly is present only for a dated, unachieved goal —
// RequiredMonthlyOK false means the card shows no "needs S$X/mo" line rather
// than a zero.
type GoalView struct {
    Goal              domain.Goal
    Contributed       domain.Money
    Percent           int
    Status            domain.GoalStatus
    RequiredMonthly   domain.Money
    RequiredMonthlyOK bool
}

// GoalsSummary is the page header and the Monthly contributions card.
// PlannedMonthlyTotal and ActualThisMonth are both in the household's primary
// currency; a goal whose currency has no rate to primary is excluded from both
// and counted in ExcludedNoRate, never silently dropped.
type GoalsSummary struct {
    Currency            string
    PlannedMonthlyTotal domain.Money
    ActualThisMonth     domain.Money
    OnTrackCount        int
    DatedCount          int
    NoDateCount         int
    ExcludedNoRate      int
    NextGoalID          string
    NextGoalName        string
    NextGoalMonth       *time.Time
}

type GoalsView struct {
    Goals   []GoalView
    Summary GoalsSummary
}

// NewGoal is what Create receives. Currency defaults to the household's
// primary at the HTTP layer, never here — the service refuses an empty one
// rather than guessing.
type NewGoal struct {
    HouseholdID          string
    Name                 string
    TargetMinor          int64
    Currency             string
    TargetMonth          *time.Time
    PlannedMonthlyMinor  int64
    StartingBalanceMinor int64
}

// GoalUpdate is a PATCH: a nil field is unchanged. ClearTargetMonth is how a
// dated goal loses its date, the same explicit-clear convention
// clearReceivedAmount uses on transactions — a nil pointer already means
// "unchanged", so it cannot also mean "clear".
type GoalUpdate struct {
    Name                *string
    TargetMinor         *int64
    TargetMonth         *time.Time
    ClearTargetMonth    bool
    PlannedMonthlyMinor *int64
}

type NewContribution struct {
    HouseholdID string
    GoalID      string
    AmountMinor int64
    OccurredOn  time.Time
    Note        string
}

type GoalDeps struct {
    Goals      GoalRepository
    Households HouseholdRepository
    FX         FXRateProvider
}

func NewGoalService(d GoalDeps) *GoalService

func (s *GoalService) List(ctx context.Context, householdID string, includeArchived bool, today time.Time) (GoalsView, error)
func (s *GoalService) Create(ctx context.Context, in NewGoal, createdOn time.Time) (domain.Goal, error)
func (s *GoalService) Update(ctx context.Context, householdID, goalID string, patch GoalUpdate) (domain.Goal, error)
func (s *GoalService) SetArchived(ctx context.Context, householdID, goalID string, archived bool) (domain.Goal, error)
func (s *GoalService) AddContribution(ctx context.Context, in NewContribution) (domain.GoalContribution, error)
func (s *GoalService) DeleteContribution(ctx context.Context, householdID, goalID, contributionID string) error
func (s *GoalService) Contributions(ctx context.Context, householdID, goalID string) ([]domain.GoalContribution, error)
```

- [ ] **Step 1: Write the failing tests** against the in-memory doubles:

```go
func TestGoalListComposesTheDesignsCards(t *testing.T)
// Four goals matching the design's own screen (Bali 2,600 of 4,000 by Dec
// 2026 at 350/mo; Emergency 18,500 of 30,000, no date, 500/mo; Education
// 41,200 of 120,000 by 2032 at 800/mo; Car 3,600 of 30,000 by 2029 at
// 400/mo), today mid-August 2026. Assert each card's Percent and Status, and
// that the dateless Emergency fund is GoalStatusNone.

func TestGoalListCountsOnlyDatedUnachievedGoals(t *testing.T)
// Of four goals, one dateless and one achieved: DatedCount is 2, NoDateCount
// is 1, OnTrackCount counts only among the 2. An achieved goal is in neither
// count — it is not a goal to be on track for.

func TestGoalListPlannedTotalConvertsThenAdds(t *testing.T)
// One SGD goal and one IDR goal with a rate: PlannedMonthlyTotal is the SGD
// figure plus the converted IDR one, computed per goal then added (the
// MonthSummary rule, LEARNING pattern 12). Remove the rate: that goal is
// excluded from BOTH totals and ExcludedNoRate is 1 — never a short total
// that looks correct.

func TestGoalListActualThisMonthExcludesStartingBalances(t *testing.T)
// The double's MonthContributionTotals excludes them (Task 3), and this test
// pins that the service does not re-add them from anywhere else.

func TestGoalListNextGoalIsTheEarliestDatedUnachievedOne(t *testing.T)
// Ties break by name; an achieved goal with an earlier date is skipped; all
// dateless → NextGoalID is "".

func TestGoalCreateValidates(t *testing.T)
// Empty/whitespace name → ErrGoalNameRequired; target 0 or negative →
// ErrGoalTargetNotPositive; planned monthly negative →
// ErrGoalPlannedMonthlyNegative; unknown currency → the ParseCurrency error;
// a target month is normalised to the first of its month before the repo sees
// it; a negative starting balance is allowed (a goal can start in deficit
// only if the household says so — assert it round-trips) while zero writes no
// contribution.

func TestGoalUpdateRefusesACurrencyChangeAndClearsADate(t *testing.T)
// There is no currency field on GoalUpdate at all; the test asserts the
// service rejects a currency mismatch if one is smuggled through NewGoal's
// path (ErrGoalCurrencyImmutable), and that ClearTargetMonth true with
// TargetMonth nil clears the date while both nil leaves it untouched.

func TestGoalAddContributionRefusesZeroAndArchivedGoals(t *testing.T)
// amountMinor 0 → ErrContributionAmountZero; a contribution against an
// archived goal → ErrGoalArchived; a valid one is written in the GOAL's
// currency, not the household's primary (assert with a goal whose currency
// differs from primary).
```

- [ ] **Step 2: Run, verify FAIL. Step 3: Implement.** `List` reads the household once for the primary currency, the goals once, and `MonthContributionTotals` once; converts each goal's planned monthly and each month total into primary with the FX provider, per goal then add; fills `Percent`, `Status`, `RequiredMonthly` from Task 2's functions. Validation lives in `Create`/`Update`/`AddContribution` and runs before any repository call. Collision and not-found errors from the repository pass through untranslated — assert `errors.Is` still holds through the service. **Step 4: Run, verify PASS.**

- [ ] **Step 5: Mutation-check the conversion test**: in `List`, add the raw minor units instead of the converted ones for the planned total. `TestGoalListPlannedTotalConvertsThenAdds` must go red. Restore, green.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/goal.go api/internal/usecase/goal_test.go
git commit -m "feat: GoalService — cards, summary counts and contribution writes"
```

---

### Task 7: Usecase — `BudgetService.RollOver`

**Files:**
- Modify: `api/internal/usecase/budget.go` (`BudgetDeps` grows `Goals GoalRepository`), `api/internal/usecase/ports.go` if a doc comment needs it
- Test: `api/internal/usecase/budget_test.go` (extend)

**Interfaces:**
- Consumes: `BudgetService.Month` (for `Remaining`), `GoalRepository.Get`, `BudgetRepository.RollOverToGoal`.
- Produces:

```go
// RollOver moves a CLOSED month's unspent budget into a goal, as one
// contribution, once. It is the manual half of the design's "Roll unspent into
// savings" toggle: nothing here runs on a clock, and the spec's decision 4
// explains why a stored toggle that acts only when clicked would be worse than
// this button.
//
// Every refusal happens before anything is written:
//   - a current or future month            -> domain.ErrRolloverMonthOpen
//   - a month with no budget row           -> domain.ErrNotFound
//   - Remaining <= 0                       -> domain.ErrRolloverNothingUnspent
//   - an archived goal                     -> domain.ErrGoalArchived
//   - a goal not in the primary currency   -> domain.ErrRolloverCurrencyMismatch
//   - a month already rolled over          -> domain.ErrRolloverAlreadyDone
func (s *BudgetService) RollOver(ctx context.Context, householdID string, month time.Time, goalID string, today time.Time) (domain.GoalContribution, error)
```

The currency refusal is spec decision 11: budgets carry no currency column and are implicitly primary, while a goal may be in another. Converting inside a rollover would store a rate nobody can audit, so a non-primary goal is refused with a reason instead.

- [ ] **Step 1: Write the failing tests**

```go
func TestBudgetRollOverWritesTheMonthsRemainingIntoTheGoal(t *testing.T)
// July budgeted 5,200.00, spent 3,420.00, today in August. RollOver writes one
// contribution of 1,780.00 dated today, source budget_rollover, its
// sourceBudgetMonth July.

func TestBudgetRollOverRefusesAnOpenMonth(t *testing.T)
// The current month → ErrRolloverMonthOpen, nothing written. A future month
// likewise. Mid-month "unspent" is still moving; money moved out of a figure
// that later shrinks is a wrong number the household cannot undo.

func TestBudgetRollOverRefusesAMonthWithNoBudgetRow(t *testing.T)
// A closed month with spend and no caps → domain.ErrNotFound, not a silent
// zero-remaining refusal.

func TestBudgetRollOverRefusesNothingUnspent(t *testing.T)
// Spent >= Budgeted → ErrRolloverNothingUnspent, nothing written.

func TestBudgetRollOverRefusesANonPrimaryCurrencyGoal(t *testing.T)
// Household primary SGD, goal in IDR → ErrRolloverCurrencyMismatch, even when
// a rate exists. The refusal is about auditability, not availability.

func TestBudgetRollOverRefusesAnArchivedGoal(t *testing.T)

func TestBudgetRollOverTwiceIsRefused(t *testing.T)
// The double stamps like the real repository; the second call →
// ErrRolloverAlreadyDone with exactly one contribution in existence.
```

- [ ] **Step 2: Run, verify FAIL. Step 3: Implement** — closed-month check first (compare month-start against today's month-start with the same UTC truncation `domain/budget.go` uses), then `Month` for `Remaining`, then the goal checks, then the one repository call. **Step 4: Run, verify PASS.**

- [ ] **Step 5: Mutation-check the closed-month refusal**: change the comparison to allow the current month. `TestBudgetRollOverRefusesAnOpenMonth` must go red. Restore, green.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/budget.go api/internal/usecase/budget_test.go
git commit -m "feat: BudgetService.RollOver — manual, closed-month, once"
```

---

### Task 8: HTTP — goal routes

**Files:**
- Create: `api/internal/adapter/http/goal_handlers.go`, `api/internal/adapter/http/goals_api_test.go`
- Modify: `api/internal/adapter/http/router.go` (inside the existing `txn` group — the one already stacking `requireCapability(domain.CapMoney)` + `requireOwner`; `Deps` gains `Goals *usecase.GoalService` beside `Budgets`), `api/internal/adapter/http/errors.go` (sentinel mappings), `api/cmd/api/main.go` (wire `NewGoalRepo`, `NewGoalService`, and `BudgetDeps.Goals`)

**Interfaces:**
- Consumes: Task 6's service methods.
- Produces: the wire contract the frontend (Task 10) parses.

```
GET    /api/v1/goals[?archived=true]          -> 200 goalsResponse
POST   /api/v1/goals                          -> 201 {"goal": goalDTO}          (CSRF)
PATCH  /api/v1/goals/{id}                     -> 200 {"goal": goalDTO}          (CSRF)
POST   /api/v1/goals/{id}/archive             -> 200 {"goal": goalDTO}          (CSRF)
POST   /api/v1/goals/{id}/restore             -> 200 {"goal": goalDTO}          (CSRF)
GET    /api/v1/goals/{id}/contributions       -> 200 {"contributions": [...]}
POST   /api/v1/goals/{id}/contributions       -> 201 {"contribution": {...}}    (CSRF)
DELETE /api/v1/goals/{id}/contributions/{cid} -> 204 no body                    (CSRF)
```

```jsonc
// goalsResponse
{
  "currency": "SGD",                      // the household's primary
  "goals": [{
    "id": "...", "name": "Bali family trip",
    "targetMinor": 400000, "currency": "SGD",
    "targetMonth": "2026-12",             // or null
    "plannedMonthlyMinor": 35000,
    "contributedMinor": 260000,
    "percent": 65,
    "status": "on_track",                 // on_track | behind | achieved | none
    "requiredMonthlyMinor": 28000,
    "requiredMonthlyOk": true,
    "archivedAt": null                    // RFC3339 when archived
  }],
  "summary": {
    "plannedMonthlyTotalMinor": 205000,
    "actualThisMonthMinor": 120000,
    "onTrackCount": 3, "datedCount": 3, "noDateCount": 1,
    "excludedNoRate": 0,
    "nextGoal": {"id": "...", "name": "Bali family trip", "targetMonth": "2026-12"}  // or null
  }
}

// one contribution
{"id": "...", "amountMinor": 50000, "occurredOn": "2026-08-14", "note": "",
 "source": "budget_rollover", "sourceBudgetMonth": "2026-07"}
```

Request bodies:

```jsonc
// POST /goals — currency omitted defaults to the household's primary, filled
// in by the handler, so the service never guesses.
{"name": "Japan 2027", "targetMinor": 1000000, "currency": "SGD",
 "targetMonth": "2027-12", "plannedMonthlyMinor": 55000, "startingBalanceMinor": 0}

// PATCH /goals/{id} — absent field = unchanged; clearTargetMonth is how a date
// is removed, the clearReceivedAmount convention.
{"name": "Japan 2028", "targetMonth": "2028-12", "clearTargetMonth": false}

// POST /goals/{id}/contributions — `currency` is optional and exists only to
// be checked: a contribution has no currency of its own, it is its goal's. A
// body that carries one which does not match the goal is refused with 422
// rather than silently ignored, because a value the code did not construct is
// never dropped on the floor (the spec's error-handling rule).
{"amountMinor": 50000, "occurredOn": "2026-08-14", "note": "August transfer",
 "currency": "SGD"}
```

- [ ] **Step 1: Write the failing tests** in `goals_api_test.go`, beside the other per-feature files.

Route-walk matrix rows for **all eight** routes, the shape `budget_api_test.go` uses: unauthenticated → 401; a member of another household → the not-found shape; a non-owner → 403; a member without `money` → 403; every write without CSRF → 403.

Then behaviour:
- `GET /goals` on a household with none is `200 {"goals": [], "summary": {...}}` — never 204, never 404.
- `POST /goals` without `currency` stores the household's primary; with `"currency": "ZZZ"` → 422 (`domain.ParseCurrency` refuses).
- A duplicate name → 409 `GOAL_NAME_TAKEN`; a name held by an **archived** goal → the same 409 with a body that names the archived goal's id, so the modal can offer restore instead of a dead end (the categories gotcha Budget's Task 13 review caught, applied here).
- `targetMonth: "2026-13"` → 400 `INVALID_MONTH`; `targetMonth: null` on create is accepted (a dateless goal).
- `PATCH` with `clearTargetMonth: true` clears it; a later `GET` shows `"targetMonth": null` and `"status": "none"`.
- `POST /goals/{id}/archive` then `GET /goals` omits it, `GET /goals?archived=true` includes it, `/restore` brings it back.
- `POST .../contributions` with `amountMinor: 0` → 422 `CONTRIBUTION_AMOUNT_ZERO`; against an archived goal → 422 `GOAL_ARCHIVED`; with `"currency": "IDR"` against an SGD goal → 422 `GOAL_CURRENCY_IMMUTABLE`, while a matching `currency` and an absent one both succeed.
- `DELETE .../contributions/{cid}` → 204 with no body; deleting it twice → the not-found shape.
- A contribution belonging to a different goal of the same household → the not-found shape (the id pair is checked, not just the contribution id).

- [ ] **Step 2: Run to verify FAIL. Step 3: Implement.**

Handlers are DTO mapping only — no arithmetic. `today` comes from `deps.Clock.Now()`. Month strings parse with `monthLayout` ("2006-01") through a helper shaped like `parseBudgetMonth`; dates parse as "2006-01-02". Errors go through `MapDomainError`, which gains:

| Sentinel | Status | Code |
|---|---|---|
| `ErrGoalNameTaken` | 409 | `GOAL_NAME_TAKEN` |
| `ErrGoalNameRequired` | 422 | `GOAL_NAME_REQUIRED` |
| `ErrGoalTargetNotPositive` | 422 | `GOAL_TARGET_NOT_POSITIVE` |
| `ErrGoalPlannedMonthlyNegative` | 422 | `GOAL_PLANNED_MONTHLY_NEGATIVE` |
| `ErrGoalCurrencyImmutable` | 422 | `GOAL_CURRENCY_IMMUTABLE` |
| `ErrGoalArchived` | 422 | `GOAL_ARCHIVED` |
| `ErrContributionAmountZero` | 422 | `CONTRIBUTION_AMOUNT_ZERO` |
| `ErrRolloverMonthOpen` | 422 | `ROLLOVER_MONTH_OPEN` |
| `ErrRolloverNothingUnspent` | 422 | `ROLLOVER_NOTHING_UNSPENT` |
| `ErrRolloverCurrencyMismatch` | 422 | `ROLLOVER_CURRENCY_MISMATCH` |
| `ErrRolloverAlreadyDone` | 409 | `ROLLOVER_ALREADY_DONE` |
| `ErrUnknownContributionSource` | 500 | (unmapped: it means a database column holds something this code never wrote — a real internal failure, not a client error) |

Routes, inside the existing `txn` group in `router.go`:

```go
txn.Get("/goals", handleListGoals(deps))
txn.Get("/goals/{id}/contributions", handleListGoalContributions(deps))

txn.Group(func(w chi.Router) {
    w.Use(requireCSRF)
    w.Post("/goals", handleCreateGoal(deps))
    w.Patch("/goals/{id}", handleUpdateGoal(deps))
    // Archive and restore are their own routes rather than a field on PATCH,
    // the same reasoning accounts and categories already carry above: if
    // archiving were patchable, an ordinary rename that happened to include it
    // would archive the goal as a side effect of saving a name.
    w.Post("/goals/{id}/archive", handleArchiveGoal(deps))
    w.Post("/goals/{id}/restore", handleRestoreGoal(deps))
    w.Post("/goals/{id}/contributions", handleAddGoalContribution(deps))
    w.Delete("/goals/{id}/contributions/{contributionId}", handleDeleteGoalContribution(deps))
})
```

- [ ] **Step 4: Run to verify PASS** — `cd api && go test ./internal/adapter/http/ -run TestGoal -v`, then the whole package.

- [ ] **Step 5: Commit**

```bash
git add api/internal/adapter/http/ api/cmd/api/main.go
git commit -m "feat: goal routes gated money+owner, with archive and restore of their own"
```

---

### Task 9: HTTP — the rollover route

**Files:**
- Modify: `api/internal/adapter/http/budget_handlers.go`, `api/internal/adapter/http/router.go`
- Test: `api/internal/adapter/http/budget_api_test.go` (extend — the rollover is Budget's screen, and its refusals are budget refusals)

**Interfaces:**
- Consumes: Task 7's `BudgetService.RollOver`.
- Produces:

```
POST /api/v1/budgets/{month}/rollover   {"goalId": "..."} -> 200 {"contribution": {...}}   (CSRF)
```

The response carries the written contribution so the frontend can show what moved without a second request. Registered in the same CSRF group as `PUT /budgets/{month}`.

**`budgetMonthResponse` gains two fields in this task**, because the Budget screen cannot know whether a month was already rolled over otherwise:

```jsonc
  "rolledOverAt": "2026-08-01T09:14:22Z",   // or null
  "rolloverGoalId": "..."                   // or null; the two move together
```

They come from the `budgets` row Task 1 added the columns to, carried through `domain.Budget` and `BudgetMonthView`. `budgetSchemas.ts` mirrors them in Task 15.

- [ ] **Step 1: Failing tests**: matrix rows for the route; a closed, budgeted month with unspent money → 200 and the contribution body; the current month → 422 `ROLLOVER_MONTH_OPEN`; a second call → 409 `ROLLOVER_ALREADY_DONE`; a month with no budget → 404; an IDR goal in an SGD household → 422 `ROLLOVER_CURRENCY_MISMATCH`; a malformed month → 400 `INVALID_MONTH`; one end-to-end assertion that `GET /goals` afterwards shows the goal's `contributedMinor` risen by exactly the month's `remainingMinor`; and, for the two new response fields, that `GET /budgets/{month}` reads `rolledOverAt: null` before and both fields populated after.

- [ ] **Step 2–4: Run FAIL → implement (thin handler, `decodeJSONBody`, `MapDomainError`) → run PASS.**

- [ ] **Step 5: Commit**

```bash
git add api/internal/adapter/http/
git commit -m "feat: POST /budgets/{month}/rollover, closed months only"
```

---

### Task 10: Frontend — schemas, hook, route, sidebar

**Files:**
- Create: `web/src/features/money/goalSchemas.ts`, `web/src/features/money/useGoals.ts`, `web/src/features/money/goalCopy.ts`
- Modify: `web/src/routes/router.tsx` (a `moneyGoalsRoute` sibling of `moneyBudgetRoute`, above the splat), `web/src/features/shell/Sidebar.tsx` (`SPACE_PAGES.money` gains `{ label: "Goals", to: "/money/goals" }` after Budget, the design's own order)
- Test: `web/src/features/money/useGoals.test.ts`, `web/src/routes/router.test.tsx` (extend), `web/src/features/shell/Sidebar.test.tsx` (extend)

**Interfaces:**
- Consumes: Task 8's wire contract.
- Produces:

```ts
// goalSchemas.ts — zod mirrors of goal_handlers.go's DTOs, following the
// backend's structs rather than the design doc, the budgetSchemas.ts rule.
export const goalSchema, goalsResponseSchema, goalContributionSchema,
             goalContributionsResponseSchema, goalResponseSchema
export type Goal, GoalsResponse, GoalContribution

// useGoals.ts — TanStack Query, the useBudget.ts shape.
export function goalsQueryKey(includeArchived: boolean)
export function goalContributionsQueryKey(goalId: string)
export function useGoals(options?: { includeArchived?: boolean; enabled?: boolean }): {
  data, loading, error,
  createGoal(body): Promise<Goal>,
  updateGoal(id, body): Promise<void>,
  archiveGoal(id): Promise<void>,
  restoreGoal(id): Promise<void>,
  addContribution(goalId, body): Promise<void>,
  deleteContribution(goalId, contributionId): Promise<void>,
}
export function useGoalContributions(goalId: string, enabled: boolean)
```

Every mutation's `onSuccess` **returns** its invalidation promise (the `useBudget.ts` convention — TanStack Query awaits the callback before a mutation settles, and settled is what `await addContribution(...)` waits on). Contribution writes invalidate both `goalsQueryKey(...)` variants and that goal's contributions key: a contribution changes the card's progress, the summary totals and the list at once.

- [ ] **Step 1: Failing tests**
- hook test with `stubFetchRoutes`: `GET /api/v1/goals` on mount; `createGoal` POSTs the exact body and returns the parsed goal; `addContribution` POSTs then re-GETs `/goals`; `deleteContribution` DELETEs and tolerates the 204 (no body to parse — the one status `apiFetch` does not parse).
- router test: "redirects a member without the money capability away from /money/goals", copied from the transactions/budget redirect test's shape — **and mutation-checked the same way that one was** (remove the guard, watch it go red).
- sidebar test: the money group renders four links in order (Finances, Transactions, Budget, Goals) and Goals' active state is computed via `useMatchRoute`, never `Link`'s `activeProps` — the cascade defect in `docs/LEARNING.md` pattern 3 must not come back.

- [ ] **Step 2–4: FAIL → implement → PASS.**

- [ ] **Step 5: Commit**

```bash
git add web/src/features/money/goalSchemas.ts web/src/features/money/useGoals.ts web/src/features/money/goalCopy.ts web/src/features/money/useGoals.test.ts web/src/routes/ web/src/features/shell/
git commit -m "feat: goals route, sidebar link, schemas and fetch hook"
```

---

### Task 11: Frontend — GoalsPage, cards, empty state, archived view

**Files:**
- Create: `web/src/features/money/GoalsPage.tsx`, `web/src/features/money/GoalCard.tsx`
- Modify: `web/src/features/money/goalCopy.ts`
- Test: `web/src/features/money/GoalsPage.test.tsx`

**Interfaces:**
- Consumes: `useGoals`; `formatMoney.ts` for every figure (minor units in, string out — the page never does money arithmetic).
- Produces: `GoalsPage` (the route component), which owns the "Show archived" toggle state and opens Task 12's modal and Task 13's contribution panel through local state.

The card renders: a progress ring at `percent`, the name, the status pill, "S$2,600 of S$4,000 · by Dec 2026" (the date clause omitted entirely when `targetMonth` is null), and "S$350/mo". **No "from OCBC Joint" line** — spec decision 6. When `requiredMonthlyOk` and the status is `behind`, the card also states what the goal actually needs per month, so "Behind" is never a verdict without its arithmetic.

- [ ] **Step 1: Failing tests** (all with `stubFetchRoutes` registering `GET /api/v1/goals`):
- four goals render four cards with their formatted figures and percent labels
- a `status: "none"` goal renders no pill and no date clause
- an `achieved` goal renders the achieved pill and a full ring
- a `behind` goal names its required monthly figure
- `goals: []` renders the empty state with one "Create your first goal" action and **no templates** (a goal has no category set to prefill; a fake starter goal is a number nobody chose)
- "Show archived" refetches with `?archived=true` and renders Restore per row
- `excludedNoRate > 0` renders the exclusion note with its count, the ledger's copy shape
- a 403 from `GET /goals` (an owner-gated read reached by a limited member holding `money`) renders the owner-only explanation, not the generic load error — branch on `ApiError.status` from `web/src/api/client.ts`. The interim Overview's one real defect was a page that rendered nothing for exactly that member; this is the assertion that would have caught it.
- **no automation copy anywhere**: assert the strings "auto-saved", "next transfer" and "Auto-save" are absent, pinned so a future copy-paste from the design cannot smuggle them back in (the same guard Budget's page test uses for "rolls into")

- [ ] **Step 2–4: FAIL → implement → PASS.** `GoalsPage` is composition only; copy lives in `goalCopy.ts`.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/money/
git commit -m "feat: Goals page — cards, empty state, archived view"
```

---

### Task 12: Frontend — the New/Edit goal modal

**Files:**
- Create: `web/src/features/money/GoalModal.tsx`
- Test: `web/src/features/money/GoalModal.test.tsx`

**Interfaces:**
- Consumes: the shared `components/Modal` primitive (genuine `:modal`; never a declarative `open` attribute — `docs/HANDOVER.md` §4), `useGoals`' `createGoal`/`updateGoal`, and `TransactionModal`'s money-input helpers (`toMinorUnits`, `minorUnitsToInputValue`, `describeAmountError` in `formatMoney.ts`) rather than a second implementation.
- Produces: `GoalModal({ mode, goal, currencies, primaryCurrency, onClose, onSaved })` where `mode` is `"create" | "edit"`.

Fields: name; target amount with a currency select (**create only** — editing is refused because it would restate every contribution behind it, and the modal says so where the disabled control is); target month with an explicit "No target date" choice; starting balance (**create only** — after creation the ledger owns it, spec decision 8); planned monthly. The design's "Auto-save each month" panel keeps its live suggestion — `remaining ÷ monthsLeft` at the values currently typed, blank while target or date is blank — relabelled **"Planned each month"**.

- [ ] **Step 1: Failing tests** with `stubFetchRoutes`:
- create posts exactly the typed values in minor units, including `startingBalanceMinor`
- the suggestion recomputes as the target and date change, and is absent while either is blank
- choosing "No target date" posts `targetMonth: null` and hides the suggestion panel entirely
- edit mode prefills from the goal, omits starting balance, and disables the currency select with its reason visible
- edit posts a PATCH; switching a dated goal to "No target date" sends `clearTargetMonth: true`
- a 409 `GOAL_NAME_TAKEN` keeps the modal open with the taken name in the message; when the body names an archived goal, the modal offers Restore instead of a dead end
- a zero or negative target shows the inline error and fires no request
- React controlled inputs are driven through the native setter in tests, not synthetic typing (`docs/HANDOVER.md` §2's note)

- [ ] **Step 2–4: FAIL → implement → PASS.**

- [ ] **Step 5: Mutation-check the clear-date test**: make the modal send `targetMonth: null` without `clearTargetMonth`. The edit test must go red (the backend treats absent-or-null as unchanged). Restore, green.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/money/GoalModal.tsx web/src/features/money/GoalModal.test.tsx
git commit -m "feat: new and edit goal modal with a live planned-monthly suggestion"
```

---

### Task 13: Frontend — contributions: add, list, delete

**Files:**
- Create: `web/src/features/money/GoalContributionsPanel.tsx`
- Modify: `web/src/features/money/GoalCard.tsx`, `web/src/features/money/goalCopy.ts`
- Test: `web/src/features/money/GoalContributionsPanel.test.tsx`

**Interfaces:**
- Consumes: `useGoals`' `addContribution`/`deleteContribution`, `useGoalContributions(goalId, enabled)` — fetched only while the panel is open, the way `useBudgetHistory.ts` gates its own query.
- Produces: `GoalContributionsPanel({ goal, onClose })`, opened from each card's "Add contribution" control.

The panel holds a small form (amount, date defaulting to today, optional note) and that goal's recent contributions, each with a delete control **behind an in-page confirmation, never `window.confirm`** — the ledger's own delete pattern. Each row is labelled by source: a manual row shows its note, a starting-balance row reads "Starting balance", and a rollover row reads "From July's unspent budget", composed in `goalCopy.ts` from `source` + `sourceBudgetMonth` (deviation 3 — the server stores no copy).

- [ ] **Step 1: Failing tests**: the form posts amount in minor units and the date as `YYYY-MM-DD`; a zero amount shows the inline error and fires nothing; adding refetches `/goals` so the card's progress moves in the same interaction; the three source labels render correctly, with the rollover label naming the month from `sourceBudgetMonth`; delete asks first, then DELETEs, then refetches; a delete that 404s (already gone in another tab) surfaces the error inline rather than silently succeeding.

- [ ] **Step 2–4: FAIL → implement → PASS.**

- [ ] **Step 5: Commit**

```bash
git add web/src/features/money/
git commit -m "feat: goal contributions panel — add, list by source, delete with confirmation"
```

---

### Task 14: Frontend — the Monthly contributions card

**Files:**
- Create: `web/src/features/money/MonthlyContributionsCard.tsx`
- Modify: `web/src/features/money/GoalsPage.tsx`, `web/src/features/money/goalCopy.ts`
- Test: `web/src/features/money/MonthlyContributionsCard.test.tsx`

**Interfaces:**
- Consumes: `GoalsResponse["summary"]` and the goals array (for the stacked bar's per-goal planned amounts).
- Produces: `MonthlyContributionsCard({ goals, summary })`.

The design's stacked bar and legend keep showing **planned** monthly amounts per unarchived goal; the card states the planned total and, beside it, `actualThisMonthMinor` — the two figures labelled apart, with one sentence when they differ (spec decision 7). "Next transfer Aug 1" does not ship.

- [ ] **Step 1: Failing tests**: the bar's segments are proportional to each goal's planned monthly and the legend names each goal with its own figure; the planned total and the actual figure both render, formatted, with distinct labels; when actual is 0 the card says so in words rather than hiding the figure; when actual exceeds planned the sentence says that, not the other way round; `excludedNoRate > 0` renders the exclusion note; an archived goal contributes nothing to bar, legend or totals.

- [ ] **Step 2–4: FAIL → implement → PASS.**

- [ ] **Step 5: Commit**

```bash
git add web/src/features/money/
git commit -m "feat: monthly contributions card — planned and actual, labelled apart"
```

---

### Task 15: Frontend — the Budget rollover

**Files:**
- Create: `web/src/features/money/BudgetRolloverCard.tsx`
- Modify: `web/src/features/money/BudgetPage.tsx`, `web/src/features/money/useBudget.ts` (a `rollOver` mutation), `web/src/features/money/budgetSchemas.ts` (the month response gains `rolledOverAt`/`rolloverGoalId`), `web/src/features/money/budgetCopy.ts`
- Test: `web/src/features/money/BudgetPage.test.tsx` (extend), `web/src/features/money/BudgetRolloverCard.test.tsx`

**Interfaces:**
- Consumes: `POST /budgets/{month}/rollover`, `GET /goals` (for the picker).
- Produces: `BudgetRolloverCard({ month, remainingMinor, currency, rolledOverTo, onRolledOver })`.

`budgetSchemas.ts`'s `budgetMonthResponseSchema` gains the two fields Task 9 added to the wire: `rolledOverAt: z.string().nullable()` and `rolloverGoalId: z.string().nullable()`. They are nullable, not optional — a Go pointer serialises to `null`, never an absent key, the same rule that file already states for `expectedIncomeMinor`.

Behaviour: on a **closed** month with `remainingMinor > 0` and no stamp, the insight area shows "S$1,780 unspent in July · Move it into a goal", opening a picker of unarchived goals **in the household's primary currency only**; a goal in another currency is listed as unavailable with the reason (spec decision 11), and when no goal qualifies the button is disabled and says why. After a successful move the card states where the money went and the button is gone. The current month shows none of this.

- [ ] **Step 1: Failing tests**: the card is absent on the current month, absent when `remainingMinor <= 0`, absent when the month carries a stamp (showing the destination sentence instead), and present otherwise; the picker excludes archived and non-primary-currency goals and explains the latter; a successful move POSTs `{goalId}` then refetches both the month and `/goals`; a 409 `ROLLOVER_ALREADY_DONE` (another tab won) shows the error inline and refetches rather than leaving a stale button; **the design's automatic sentence is still absent** — extend Budget's existing "rolls into" assertion rather than replacing it.

- [ ] **Step 2–4: FAIL → implement → PASS.**

- [ ] **Step 5: Commit**

```bash
git add web/src/features/money/
git commit -m "feat: move a closed month's unspent budget into a goal"
```

---

### Task 16: Frontend — Overview's goals card and the quick-add entry

**Files:**
- Create: `web/src/features/overview/GoalsCard.tsx`
- Modify: `web/src/features/overview/OverviewPage.tsx`, `web/src/features/overview/QuickAddMenu.tsx`, `web/src/features/overview/copy.ts`
- Test: `web/src/features/overview/OverviewPage.test.tsx` (extend), `web/src/features/overview/GoalsCard.test.tsx`

**Interfaces:**
- Consumes: `useGoals({ enabled: isOwner })` — the same `enabled` gate `OverviewPage` already applies to `useBudget`, and for the same reason: `GET /goals` is `requireCapability(money)` **and** `requireOwner`, so firing it for anyone else caches a failure under a key nobody meant to write.
- Produces: `GoalsCard({ goals })` rendering "3 of 4 on track" plus "next: Bali · Dec 2026", and a `QuickAddMenu` entry that opens `GoalModal`.

- [ ] **Step 1: Failing tests**: the card renders the counts and the next dated goal; `datedCount: 0` hides the figure rather than rendering "0 of 0"; the card names how many goals were excluded for having no date when any were; a limited member holding `money` sees neither the card nor the quick-add entry, and **still sees a page with content on it** (extend the existing limited-member test rather than adding a new one that asserts only absence — absence holds perfectly over a blank page, which is exactly how the interim Overview's defect survived every test); the quick-add "Savings goal" entry opens the modal and, on save, refetches `/goals`.

- [ ] **Step 2–4: FAIL → implement → PASS.**

- [ ] **Step 5: Commit**

```bash
git add web/src/features/overview/
git commit -m "feat: Overview goals-on-track card and the savings-goal quick add"
```

---

### Task 17: Docs — the three that must not go stale

**Files:**
- Modify: `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`, `docs/HANDOVER.md`

Use the `maintaining-system-design` skill for `SYSTEM_DESIGN.md`: the `goals`/`goal_contributions` pair and `budgets`' two new columns in the data section; the nine new routes with their `money`+owner guards in the route table; `GoalService` and the grown `BudgetService` in the component diagram; and prose under the diagram for the three non-obvious shapes — a contribution moves no real money, the rollover writes a contribution and a stamp in one transaction (and deleting the contribution clears the stamp), and a goal carries a currency where a budget does not.

`FEATURE_TRACKER.md`, Money section:
- "Savings goals with progress and funding source" → **🟡, with the gap named**: funding source deliberately dropped (spec decision 6); progress and the rest ship. A 🟡 with no named gap is worse than a ⬜.
- "Monthly contributions summary" → ✅ (planned and actual side by side).
- "New goal (modal)" → ✅ (create and edit; currency fixed after creation).
- Budget's "Roll unspent into savings" → **🟡**: the manual move ships, the design's automatic month-end toggle does not.
- Add a row for goal contributions (add/delete/list by source), which the design's mockup never draws — the design shows only the aggregate.
- Add a row for archive/restore of a goal, ⬜→✅, likewise undrawn.
- Overview's "Goals on track card" → ✅; "+ Add" quick-create → still 🟡, with its remaining entries (Bill, Calendar event, Marriage retro) named.
- **Recount the summary table by counting symbols**, first symbol per row cell, bare or with prose after it — never by adjusting the previous totals.

`LEARNING.md`: whatever the tasks actually taught; at minimum every defect a review or the walk finds, filed under an existing pattern where the shape matches rather than as a new one.

`HANDOVER.md`: slice 2 row → "Accounts, Transactions, Budget, Goals built; Bills not started"; §4's "what Goals must pin" section replaced by what it pinned and what Bills inherits; the automatic-contribution and automatic-rollover deferrals added to §5 as named follow-ups with their reasons.

- [ ] **Step 1: Update all four. Step 2: `make lint && make test` green. Step 3: Commit**

```bash
git add docs/
git commit -m "docs: record Goals in the system design, tracker and handover"
```

---

### Task 18: Walk the definition of done

**Files:**
- Create: `docs/superpowers/plans/2026-08-01-hearth-goals-verification.md`

Start from nothing (`make down && docker volume rm hearth_hearth-pgdata && make up && make seed`), real browser at `http://localhost:5173`. Before starting, check `docker ps` on **both** Docker engines — a stale Docker Desktop stack silently holding ports 5173/8080/8025 cost a previous walk an hour (`docs/HANDOVER.md` §2).

**Arithmetic dry-run note (`docs/LEARNING.md` pattern 13):** criteria 6–9 share one prepared month and one prepared goal. Each states the totals it expects *after* the walk's own earlier steps; write the expected numbers down before clicking, and no criterion may assert a counter an earlier criterion has already moved without saying so. Screenshots are the record, not the evidence — assert on numbers read from the DOM (`innerText`, `getBoundingClientRect()`), and compare screenshot hashes (`shasum -a 256`) rather than eyeballing before/after pairs.

1. Sign in as Andreas; the MONEY sidebar group shows Finances, Transactions, Budget, **Goals**, and Goals' link colours only on its own route.
2. `/money/goals` on the seeded household shows the empty state: one "Create your first goal" action, no templates, and no automation copy anywhere on the page.
3. Create "Bali family trip": target 4,000.00 SGD, date Dec 2026, starting balance 2,600.00, planned monthly 350.00. The modal's suggestion updates live as the target and date are typed. Save. The card reads 65%, "S$2,600.00 of S$4,000.00 · by Dec 2026", "S$350.00/mo", pill **On track** (2,600 contributed, 1,400 remaining over the months to Dec 2026 at today's real date — compute it by hand and record the arithmetic).
4. Create "Emergency fund" with **no target date**: the card shows progress and no pill, and the header count names it as excluded ("… · 1 with no date").
5. Create a goal whose planned monthly is far too small for its date: the card reads **Behind** and states the monthly figure it actually needs.
6. Add a 500.00 contribution to Bali: the ring, the "of" figure and the Monthly contributions card's **actual** figure all move by exactly 500.00, while the **planned** total does not move at all. The starting balance from criterion 3 is **not** in the actual figure — that is the defect this criterion exists to catch.
7. Delete that contribution from the panel (confirming in-page, never a browser dialog): every figure returns to criterion 3's values.
8. Set a budget for **last month** with caps that leave real unspent money (use the seeded transactions' own figures; read them from the Transactions page first). On the Budget screen, switch the picker back a month: the card offers "S$X unspent in <month> · Move it into a goal".
9. Move it into Bali: the goal's contributed figure rises by exactly X, the contributions panel shows the row labelled "From <month>'s unspent budget", and the Budget card now names the destination with no button left. Click through again in a second tab: refused, with the message, and **no second contribution**.
10. Delete that rollover contribution from the goal, return to Budget's same month: the button is back. Move it again: it succeeds, and there is exactly one rollover row. (The round trip, in the browser this time.)
11. Overview shows "Goals on track — N of M" with the next dated goal named, and "+ Add → Savings goal" opens the modal and saves.
12. Archive "Emergency fund": it leaves the list and the counts; "Show archived" lists it with Restore; its contributions survive; Restore returns it whole.
13. Create a goal in **IDR** while the household's primary is SGD: its own card renders in IDR; the planned and actual totals convert it at the live rate; then reach the no-rate state the accounts walk used and confirm the exclusion note appears with a count rather than a silently short total.
14. With the IDR goal present, open the rollover picker: the IDR goal is listed as unavailable with its reason, and only primary-currency goals can be chosen.
15. A limited member granted `money` through Settings: `/money/goals` shows the owner-only explanation, not a blank page and not a generic error; Overview still renders content for them. Then, by `curl` with a session cookie only: `POST /api/v1/goals` without CSRF → 403; `GET /api/v1/goals` as that member → 403; `POST /api/v1/budgets/2026-13/rollover` → 400 `INVALID_MONTH`.

- [ ] **Step 1: Run the walk, recording PASS/FAIL per criterion as you go, with the arithmetic written out where a criterion asserts a figure. Step 2: `make lint && make test`, paste the summary lines. Step 3: Fix anything found — each in its own commit, with a test that would have caught it, and re-walk that criterion. Step 4: Commit the record**

```bash
git add docs/superpowers/plans/2026-08-01-hearth-goals-verification.md
git commit -m "docs: record the Goals verification walkthrough"
```

---

## Definition of done for this plan

Every task's tests green under `make lint && make test`; the designated mutations in Tasks 2, 4, 5, 6, 7, 10 and 12 each seen red then green (and any that cannot go red rewritten, not excused — `docs/LEARNING.md` pattern 2); the Task 18 walk recorded with every criterion passing or its failure fixed, re-walked and learned from; and the four documents in Task 17 updated with the tracker's summary table recounted by symbol — before any of this is called finished.
