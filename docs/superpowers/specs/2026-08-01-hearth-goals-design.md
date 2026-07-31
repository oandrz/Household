# Hearth — Goals

Spec for slice 2's fourth feature: the Savings goals screen, its New/Edit goal
modal, the contributions that make a goal move, and the manual rollover that
finally gives Budget's unspent money somewhere to go. Written 2026-08-01 after
a brainstorming session; decisions below record what was chosen and why, in the
order they were made.

The design source is the Goals section of `design/Household Dashboard.dc.html`
(screen `is_goals`, modal `modalGoal`), plus the Overview card "Goals on track
· 4 of 4" and the Budget modal's "Roll unspent into savings" toggle.

Goals is taken before Bills for one reason: Budget shipped with a deferral
pointing at it. `docs/FEATURE_TRACKER.md` carries "Roll unspent into savings"
as ⬜ *(deferred whole to Goals — spec decision 1)*, and Budget's insight card
names money the household has not spent with nowhere to send it. That is a
promise outstanding inside shipped code. Bills has no such promise, and its own
dependent — the Family calendar — is a slice away.

**This is the M-sized Goals, deliberately.** The design shows automatic monthly
transfers ("S$2,050 auto-saved on the 1st of each month", "next transfer Aug
1"). Nothing in this project runs on a clock, and inventing this codebase's
first scheduler inside a feature that arrived with four undefined figures is
the wrong trade. Contributions are entered by a person; rollover happens when
somebody clicks. Automatic contribution is its own later spec, written once
real households have goals to point at.

## Decisions

1. **A goal's progress is its own contributions ledger, not an account
   balance and not a sum over transactions.** The design funds two different
   goals ("Bali family trip", "New family car") from one account, OCBC Joint —
   so one balance cannot serve as one goal's progress, and the account-link
   model is arithmetically dead on the design's own data. Deriving progress
   from tagged transactions was rejected as the larger, later feature: it
   changes the transactions schema and the ledger UI, forces a household to
   log a transfer before a goal can show any progress at all, and drags in the
   cross-currency transfer rules. A dedicated `goal_contributions` table leaves
   Transactions untouched and gives the Monthly contributions card and the
   rollover a place to write.

   **The accepted cost, stated plainly because someone will try to "fix" it: a
   contribution moves no real money.** A goal earmarks; it does not hold. Goal
   progress and account balances are independent figures, and nothing
   reconciles them. This is written into the migration and the domain file at
   the point an editor would reach for a `transactions` join.

2. **"On track" means still reachable at the planned rate.** A goal is on
   track when `remaining ÷ monthsLeft ≤ planned monthly`. This is
   Remaining-based, exactly as Budget's pace figures are (Budget spec decision
   2), so the card can always show the arithmetic behind its own pill and no
   figure is a forecast the screen cannot explain. Missed contributions raise
   the required monthly figure month by month until it crosses the planned
   one, so a lapse surfaces without a separate "you skipped a month" rule.

   The two rejected alternatives failed differently. *Contributed-to-date*
   (expected = starting + planned × months elapsed) reads "on track" forever
   for a household that front-loaded a goal whose date has since become
   unreachable. *Contributed-this-month* reads off-track on the 1st of every
   month for every goal, and never answers the question actually being asked.

3. **A target date is optional, and a dateless goal has no status.** The
   design's own Emergency fund shows "S$18,500 of S$30,000 · 6 months
   expenses" and no date at all. A dateless goal renders its progress, target
   and planned monthly, carries no On track / Behind pill, and is excluded from
   the "X of Y on track" count — whose copy states what it counted. Requiring a
   date would force a household saving toward "6 months expenses" to invent one
   and then be reported against fiction; a fallback status for dateless goals
   was rejected for putting two different meanings behind one word on one
   screen.

4. **Rollover is a button on a closed month, not a stored toggle.** A past
   month's Budget insight card offers "Move S$1,780 into a goal"; the household
   picks one, one contribution is written, and the budget row is stamped so a
   second attempt is refused. The design's toggle does not ship as a toggle:
   a setting labelled "Roll unspent into savings" that acts only when clicked
   reads as automatic, which is the dishonesty Budget spec decision 1 refused
   in the first place.

   **Closed months only.** Mid-month "unspent" is still moving; money moved out
   of a figure that later shrinks is a wrong number the household cannot undo.
   The accepted cost matches Budget decision 4's: a household that never
   revisits last month never rolls anything, and nothing happens silently at
   midnight on the 1st.

5. **A goal carries an explicit currency, defaulting to the household's
   primary.** This departs from `00006_budgets.sql`, which deliberately has no
   currency column, and the difference is the point: a budget is one month, and
   a primary-currency change restating one month's plan was an accepted cost. A
   goal accumulates for years, so the same silence would restate a multi-year
   total and every contribution behind it. `accounts` already stores currency
   per row for this exact reason, and its migration says so — a goal is the
   long-lived kind of row, not the monthly-plan kind. A contribution carries no
   currency of its own; it is in its goal's currency by construction, and only the
   cross-goal totals convert, through the same convert-then-add path
   `NetWorthSummary` uses, naming the missing-rate case rather than dropping it
   silently.

6. **The design's "Fund from" account select does not ship.** Under decision 1
   an account link moves no money and drives no figure, so it would be
   decoration on a screen whose whole job is to be believed. A household with
   four goals across two accounts loses the record of which pot holds what;
   that is the accepted cost, and it returns for free if contributions ever
   become real transfers. The card line "S$350/mo · from OCBC Joint" ships as
   "S$350/mo".

7. **The Monthly contributions card shows planned and actual side by side.**
   The design's own bar sums the planned amounts (800 + 500 + 400 + 350 =
   2,050), which is a commitment, not an achievement. With contributions
   entered by hand the two diverge constantly, and that divergence is the fact
   the household needs. Planned-only would show "S$2,050 total" to a household
   that contributed nothing all month; actual-only would empty the card on the
   1st of every month and leave the planned amounts — which drive decision 2 —
   nowhere but the individual cards.

8. **Starting balance is a contribution row, not a column on the goal.** The
   modal keeps its "Starting balance" field; saving writes a
   `goal_contributions` row dated the goal's creation with
   `source = 'starting_balance'`. Progress is then a single sum with nothing
   beside it. A column plus a ledger is structurally the Critical defect the
   Accounts branch shipped and had to fix — a field prefilled from a derived
   value and written back as the opening one, silently moving a household
   total (`docs/LEARNING.md` pattern 1).

9. **A goal archives; it is never deleted, and "achieved" is derived.**
   Contributions reference a goal, and a rolled-over budget month names one, so
   deletion would strand rows and blank a past month's record — the accounts
   precedent, for the accounts reason. Archive hides a goal behind a "Show
   archived" view with restore. "Achieved" is `contributed ≥ target`, computed
   on read: no stored flag, so it can never disagree with the ledger. An
   explicit "Mark achieved" control was rejected for allowing exactly that
   contradiction.

10. **Reads and writes are both gated `money` + owner**, the Budget and
    Transactions shape, for the reason Budget spec decision 8 gives: the page
    is nothing but figures, and a screen with every figure blank reads as
    broken rather than restricted. A limited member never sees the Goals link —
    `SPACE_PAGES` gating, as with Transactions.

11. **The rollover goal picker offers only goals in the household's primary
    currency.** Budget has no currency column and is implicitly primary
    (decision 5); a goal may be in another. Rather than convert inside a
    rollover and store a rate nobody can audit, non-primary goals are listed as
    unavailable with the reason stated — the same shape as the missing-rate
    copy on Finances. If every goal is non-primary the button is disabled and
    says why.

## Data model

Migration `00007_goals.sql`:

```sql
CREATE TABLE goals (
    id                    uuid PRIMARY KEY,
    household_id          uuid NOT NULL REFERENCES households (id),
    name                  text NOT NULL,
    target_amount_minor   bigint NOT NULL CHECK (target_amount_minor > 0),
    currency              char(3) NOT NULL,       -- decision 5; defaults to primary at creation
    target_month          date,                   -- first of the month; NULL = no date (decision 3)
    planned_monthly_minor bigint NOT NULL CHECK (planned_monthly_minor >= 0),
    archived_at           timestamptz,
    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL,
    UNIQUE (household_id, name)
);

CREATE TABLE goal_contributions (
    id           uuid PRIMARY KEY,
    goal_id      uuid NOT NULL REFERENCES goals (id),
    household_id uuid NOT NULL REFERENCES households (id),
    amount_minor bigint NOT NULL CHECK (amount_minor <> 0),
    occurred_on  date NOT NULL,
    note         text NOT NULL DEFAULT '',
    source       text NOT NULL,                   -- 'manual' | 'starting_balance' | 'budget_rollover'
    created_at   timestamptz NOT NULL
);

ALTER TABLE budgets
    ADD COLUMN rolled_over_at    timestamptz,
    ADD COLUMN rollover_goal_id  uuid REFERENCES goals (id);
```

- `target_month` uses the same first-of-month convention `budgets.month` and
  `MonthTotals` already take.
- `household_id` is carried on `goal_contributions` as well as on `goals` so
  every read is scoped by household without a join — the shape the other
  repositories already use.
- `amount_minor <> 0` rather than `> 0`: a mistyped contribution is corrected by
  deleting it, but a genuine correction downward (a goal the household raided)
  is a negative row. Zero is meaningless and refused.
- `source` is a string the code did not construct once it comes back from the
  column, so parsing it has a `default` that refuses (`domain.ParseGoal…`),
  the same fail-closed rule as `TransactionKind`.
- `rollover_goal_id` has no `ON DELETE` clause because goals are never deleted
  (decision 9). An archived goal keeps the reference readable.
- Goals are **not** deleted and have no `DELETE` endpoint. Contributions are
  hard-deleted, because nothing references a contribution — the same reasoning
  the Transactions spec applied to a transaction.

## The formulas, pinned

Let `contributed = SUM(goal_contributions.amount_minor)` for the goal, in the
goal's own currency, and `remaining = max(0, target − contributed)`.

**"Today" is `deps.Clock.Now()`, resolved in the handler and passed down as a
parameter — the Budget convention, inherited by name rather than restated.**
`internal/usecase/budget.go` says why in its own comment: no service here reads
`time.Now()` itself, because a service that does cannot be tested at a
boundary. Month arithmetic truncates in **UTC**, exactly as
`domain.budget.go` and `startOfMonth` already do. No new timezone concept is
introduced; `docs/HANDOVER.md` §6 records a `time.Truncate`-and-location
mistake that shipped at two call sites here, and inventing a household clock
would be a third.

These formulas are the contract; an implementer who needs a different one
changes this spec first.

| Figure | Formula |
|---|---|
| Progress % | `contributed ÷ target`, rounded to the nearest whole percent, capped at 100 for the ring. A negative `contributed` renders 0, never a reversed ring |
| Achieved | `contributed ≥ target`. Derived on read, never stored (decision 9) |
| `monthsLeft` | Whole calendar months from the current month to `target_month`, **inclusive of both ends**: Aug 2026 → Dec 2026 = 5; target month = current month = 1. Never zero, never negative — a past target never reaches the division |
| Required monthly | `remaining ÷ monthsLeft`, rounded **up** to the whole minor unit. Rounding up is deliberate: rounding down states a figure that does not actually reach the target |
| On track | `target_month` present, not archived, not achieved, and `required monthly ≤ planned_monthly` |
| Behind | Same inputs, `required monthly > planned_monthly`. A `target_month` before the current month and not achieved is **Behind** by definition, with no division performed |
| No status | `target_month IS NULL`, or the goal is archived |
| "X of Y on track" | Y counts unarchived, dated, unachieved goals; X counts those on track. Copy names what was excluded: "3 of 4 on track · 1 with no date". Y = 0 hides the figure rather than rendering "0 of 0" |
| Next goal (Overview) | Of the dated, unarchived, unachieved goals, the earliest `target_month`; ties broken by name. None → the line is omitted |
| Planned monthly total | `SUM(planned_monthly_minor)` over unarchived goals, each converted to primary currency. Goals with no available rate are excluded and the card states how many, the ledger's own copy pattern |
| Actual this month | `SUM(amount_minor)` over contributions with `occurred_on` in the current month, unarchived goals only, **excluding `source = 'starting_balance'`**, converted the same way and excluded-for-no-rate the same way. The exclusion is load-bearing: a household creating four goals with existing balances on day one would otherwise read "S$41,200 added in August" for money that never moved, destroying the planned-versus-actual divergence decision 7 exists to show |
| Suggested monthly (modal) | `remaining ÷ monthsLeft` at the values currently typed, recomputed live; blank while target or date is blank. A suggestion only — the household may type anything, including 0 |
| Unspent available to roll (Budget) | The closed month's `Remaining` (Budgeted − Spent) when positive. `≤ 0` → no rollover offered |

## API

All under `/api/v1`, all gated `money` capability **and** owner (decision 10),
CSRF on writes, the same middleware chain as budget. Every 2xx carries a JSON
body; a household with no goals is `200 {"goals": [], "summary": {…}}`, never
204 and never 404.

| Route | Behaviour |
|---|---|
| `GET /goals` | The whole screen in one response: each goal with its stored fields, `contributed`, progress percent, status (`on_track`/`behind`/`achieved`/`none`), and `requiredMonthlyMinor` where it exists; plus `summary` with the planned total, the actual-this-month total, the `X of Y` counts, the excluded-for-no-rate count, and the next dated goal. `?archived=true` returns archived goals instead |
| `POST /goals` | `{name, targetAmountMinor, currency, targetMonth?, plannedMonthlyMinor, startingBalanceMinor?}`. One transaction: the goal row, then the `starting_balance` contribution when the figure is non-zero (decision 8). Duplicate name against `UNIQUE (household_id, name)` → 409 naming the taken name; unknown currency → 422 via `domain.ParseCurrency`; target ≤ 0 → 422 |
| `PATCH /goals/{id}` | Any of name, target, target month (including clearing it to null), planned monthly. **Currency is not patchable** — it would restate every contribution behind it; the household archives the goal and creates a new one. `archivedAt` set or cleared archives or restores |
| `POST /goals/{id}/contributions` | `{amountMinor, occurredOn, note?}`, `source = 'manual'`. A contribution against an archived goal → 422 |
| `DELETE /goals/{id}/contributions/{cid}` | Hard delete. A mistyped figure needs a way back, and nothing references a contribution. `starting_balance` and `budget_rollover` rows are deletable too — refusing would leave a household stuck with a wrong number it can see. **Deleting a `budget_rollover` row clears that month's `rolled_over_at` and `rollover_goal_id` in the same transaction**, so the month becomes rollable again; leaving the stamp would produce money gone from the goal, a month claiming it rolled over, and a 409 on every retry — unrecoverable partial state, the shape `guarding-partial-writes` exists to catch |
| `POST /budgets/{month}/rollover` | `{goalId}`. Writes one contribution with `source = 'budget_rollover'` and a note naming the month, and stamps `rolled_over_at`/`rollover_goal_id`, in one transaction. Refuses: a month with **no `budgets` row at all** → 404, stated explicitly rather than left to `Budgeted = 0` catching it by arithmetic (Budget decision 4 allows a closed month with spend and no caps, so this state is reachable); an open (current or future) month → 422; an already-stamped month → 409; a goal whose currency is not the household's primary → 422 (decision 11); an archived goal → 422; `Remaining ≤ 0` → 422 |

Ports: `GoalRepository` is new and narrow — `List`, `Get`, `Create`,
`Update`, `AddContribution`, `DeleteContribution`, `ListContributions`,
`MonthContributionTotals`. `BudgetRepository` grows `MarkRolledOver` and
`ClearRolledOver`, each taking the month so the stamp and the contribution move
together and cannot half-happen in either direction. Status, `monthsLeft` and the required-monthly
arithmetic live in `internal/domain/goal.go` and are pure functions over
values — no repository, no clock beyond a `today` parameter.

`GoalService` holds no actor parameter, like every other service here:
authorisation is HTTP-layer only.

## Screens and states

New route `/money/goals`, a third link under the MONEY sidebar group — the
`SPACE_PAGES` entry lands in the same change as the route, which is the failure
mode `docs/FEATURE_TRACKER.md`'s Marriage note exists to prevent.

**Goals page** — `GoalsPage.tsx` stays a rendering shell; fetch orchestration
lives in `useGoals.ts` from day one (Budget spec decision 11's precedent, taken
rather than repeated advice).

1. **Empty state** — no goals: the design's framing plus one "Create your first
   goal" action opening the real modal. No templates; a goal has no equivalent
   of a category set to prefill, and a fake starter goal is a number nobody
   chose.
2. **Set state** — cards in a 2-up grid: ring with percent, name, status pill,
   "S$2,600 of S$4,000 · by Dec 2026", "S$350/mo". A dateless goal drops the
   date clause and the pill (decision 3). An achieved goal shows an Achieved
   pill and a full ring. Clicking a card opens the edit modal.
3. **Monthly contributions card** — the design's stacked bar of planned amounts
   with its legend, the planned total, and beside it the actual figure for this
   month. Where they differ the card says so in one sentence. "Next transfer
   Aug 1" does not ship; nothing schedules a transfer.
4. **Archived** — "Show archived" lists archived goals with their final
   figures and a Restore action, the Accounts pattern.
5. **No exchange rate** — a goal in a currency with no rate to primary still
   renders its own card in its own currency; only the two totals exclude it,
   and the card states the exclusion count.

**New / Edit goal modal** — `GoalModal.tsx` on the shared `Modal` primitive.
Fields: name, target amount with currency (currency editable only on create,
per the API note), target month (optional, a month picker with a "No target
date" choice), starting balance (create only — after creation the ledger owns
it), planned monthly. The design's "Auto-save each month" panel keeps its
suggested figure and its live recomputation, relabelled "Planned each month";
it promises nothing about automation. Money inputs reuse the transaction
modal's minor-unit handling — no floats anywhere in the path.

**Contributions** — "Add contribution" on each card opens a small form (amount,
date defaulting to today, optional note) and lists that goal's recent
contributions with a delete control behind an in-page confirmation, never
`window.confirm`, matching the ledger's own delete.

**Budget page** — a closed month with `Remaining > 0` and no stamp shows
"S$1,780 unspent in July · Move it into a goal", opening a goal picker
(primary-currency, unarchived goals only). After the move the card states where
the money went and the button is gone. The current month's insight card is
unchanged; no rollover language appears on it.

**Overview** — the "Goals on track" card becomes real: the `X of Y` figure and
the next dated goal beneath it. `+ Add → Savings goal` becomes live, with no
precondition — the account dependency disappeared with decision 6.

**Copy the design asserts and this feature does not ship:**

| Design | Ships as |
|---|---|
| "S$2,050 auto-saved on the 1st of each month" | "S$2,050 planned each month · S$1,200 added in August" |
| "next transfer Aug 1" | Dropped; the actual figure replaces it |
| "Auto-save each month" (modal) | "Planned each month" |
| "Unspent budget rolls into the Bali trip goal at month end" | "S$1,780 unspent in July · Move it into a goal" |
| "S$350/mo · from OCBC Joint" | "S$350/mo" (decision 6) |

## Error handling

- Adapters map missing rows to `domain.ErrNotFound`; no `pgx` type crosses the
  boundary.
- Goal creation with a starting balance is one transaction: a goal with a
  missing opening contribution is impossible. The rollover is one transaction:
  a stamped month with no contribution, or a contribution with no stamp, is
  impossible. This is the `guarding-partial-writes` rule applied to the two
  places here that write twice.
- `source` and any currency code arriving from a column or a request body is
  parsed through a `switch` with a refusing `default`.
- A contribution has no currency of its own — it is its goal's. If a request
  body carries one anyway it must equal the goal's, or the write is refused
  with 422 rather than silently ignored; a value the code did not construct is
  never dropped on the floor.
- Name collisions surface as 409 with the taken name composed into the message,
  inline in the modal, as `BudgetModal` already does for categories. A name held
  by an **archived** goal offers restore instead of a bare 409 — the same gotcha
  Budget's Task 13 review caught on `categories_household_id_name_key`.
- Frontend tests stub with `stubFetchRoutes`, which throws on an unregistered
  request.

## Testing

- **Domain**: the formula table becomes table-driven tests. Boundaries that
  get their own cases: `monthsLeft` when the target month *is* the current
  month, one month ahead, and in the past; required-monthly rounding up at a
  figure that divides exactly and one that does not; `on track` at exactly
  `required == planned`; progress with `contributed > target`; a negative
  contribution.
- **Designated mutations**, each broken deliberately and watched go red before
  being restored (`proving-tests-can-fail`): the inclusive `monthsLeft`
  boundary (drop the `+1`), the `≤` in the on-track rule (to `<`), the
  closed-month refusal in rollover, and the primary-currency filter on the
  picker. Each must be a mutation *only one guard* defends — the interim
  Overview's plan named a mutation two guards defended, so it could never go
  red (`docs/LEARNING.md` pattern 2).
- **Usecase**: goal service against in-memory doubles — summary counts with a
  dateless goal present, an achieved goal excluded from the denominator,
  no-rate exclusion counts, contribution add/delete moving progress, archive
  and restore.
- **HTTP**: route-walk matrix rows for every new route in a new
  `goals_api_test.go` (unauthenticated, wrong household, non-owner, missing
  `money` capability, missing CSRF on writes) — the per-feature split Budget's
  Task 1 established.
- **Postgres**: the unique name constraint, the household scoping of both
  tables, the two transactional writes rolling back whole on a forced failure,
  the rollover stamp preventing a second write, and the round trip that matters
  most — roll over, delete the rollover contribution, roll over again and
  succeed. A test that only asserts the 409 would pass against the
  stamp-left-behind bug.
- **Frontend**: the five page states, the modal's create and edit shapes, the
  live suggested-monthly recomputation, the contribution add/delete flow, the
  Budget rollover button's appear/disappear conditions. New guard tests
  mutation-checked.
- **Browser walk before done**, on a wiped database, covering an owner **and** a
  limited member holding `money`. The interim Overview's one real defect was a
  page that rendered nothing at all for exactly that member, invisible to every
  unit test because each asserted the *absence* of something and absence holds
  perfectly over a blank page (`docs/LEARNING.md` pattern 2).

## Out of scope

| Not here | Where it lives |
|---|---|
| Automatic monthly contributions, and any scheduler | Its own later spec, once households have goals to point at |
| Automatic month-end rollover | Same spec; decision 4 ships the manual move |
| Contributions as real transfers between accounts | Returns with a transactions-linked model, if ever; decision 1 names the cost |
| "Fund from" account on a goal | Dropped, decision 6 |
| Goal contribution reminders by email | The notification preferences exist; the job does not |
| Bills, and Overview's Next bill card | Money's fifth feature |

## Definition of done

`make lint && make test` green; the designated mutations above each watched
red and restored; a 15-criterion browser walk on a wiped database whose script
is dry-run against the state the walk itself creates (no criterion may assert a
counter the walk has already moved — `docs/LEARNING.md` pattern 13);
`docs/FEATURE_TRACKER.md` updated with the summary table **recounted by
symbol** — the three Goals rows, Budget's "Roll unspent into savings" row, and
Overview's "Goals on track" and "+ Add" rows, with "Savings goals with progress
and funding source" landing at 🟡 and naming the dropped funding source as its
gap; `docs/SYSTEM_DESIGN.md` gaining the two tables, the routes and their
guards, and the service; `docs/LEARNING.md` gaining whatever the work teaches.
