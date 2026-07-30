# Hearth — Budget

Spec for slice 2's third feature: the Budget screen, its Edit-budget modal,
Budget history, and the category management the tracker has been carrying as
Budget's debt since Transactions shipped. Written 2026-07-30 after a
brainstorming session; decisions below record what was chosen and why, in the
order they were made.

The design source is the Budget section of `design/Household Dashboard.dc.html`
(screen `is_budget`, modals `modalBudget` and `modalBudgetHist`, and the
`budgetEmpty` state). Accounts and Transactions are built and walked; Budget
builds directly on the Transactions ledger — an envelope per category is a sum
over the month's transactions by category, which
`TransactionRepository.MonthTotals` already returns.

## Decisions

1. **Rollover is deferred to Goals, whole.** The design's "Roll unspent into
   savings" toggle names a savings goal and moves money at month end; Goals is
   not built. The toggle does not ship — not stored-but-dormant, not stubbed —
   because a control that looks real and does nothing violates this project's
   own honesty rules. The insight card says what is unspent; the rollover
   sentence arrives with Goals. The tracker carries the row as ⬜ with this
   reason.

2. **The pace figures are Remaining-based, not projections.** In the design's
   own numbers both derive from Remaining (5,200 − 3,420 = 1,780; 1,780 ÷ 13
   days ≈ 137). A run-rate projection contradicts the mockup's figures with
   the mockup's own data (3,420 over 18 days projects past the budget, which
   would make "on pace to save S$1,780" read "over by S$690"). Remaining-based
   figures are always mutually consistent and never surprise the household
   with a forecast the screen cannot explain. Formulas pinned in full below.

3. **"Monthly income" is a manually typed expected-income field, optional.**
   Stored on the month's budget row, prefilled from the previous month when
   importing. Summing the month's income transactions instead reads S$0 on
   the 1st — exactly when the household sits down to plan — and sends "Left
   to allocate" negative. Left blank, the income and left-to-allocate cards
   hide and only Allocated shows; nothing invents a number.

4. **Budgets are per-month rows with explicit copy-forward, never automatic.**
   Each month's caps are their own rows. A new month starts unset; the empty
   state offers "Import last month" (one click, prefills the modal) or a
   template. History stays exact — a month shows the caps it actually had —
   and no silent state change happens at midnight on the 1st. The cost is
   accepted: a household that never clicks sees the empty state each new
   month.

5. **Category management ships here, in the Edit-budget modal, in full:**
   add, rename, archive. ✕ on a row removes that category's **cap** from this
   month's budget and never touches the category itself; archive is its own
   control on the row. Archiving hides a category from new-cap and
   transaction dropdowns but keeps every old transaction labelled and every
   existing budget line intact — the accounts archive precedent applied to a
   different table, as the handover predicted. This closes the tracker's
   known category-editing gap in the one screen the design gives it.

6. **Templates prefill the modal; they never write directly.** Family-of-four
   opens the modal with its category set and round starter caps in the
   household's primary currency (the design's "· SGD" tag is seed-data
   storytelling, not a currency rule). 50/30/20 asks for expected income in
   the modal first, then splits it 50% needs / 30% wants across mapped
   starter categories, leaving 20% unallocated as savings headroom. The
   household reviews, adjusts, saves. One-click instant budgets were rejected
   because they write caps nobody looked at — and 50/30/20 with no income on
   file has nothing to split anyway. "Import last month" is the same
   mechanic: fetch June, prefill, save July.

7. **Storage is a parent-and-lines pair** (approach A of three considered):
   `budgets` (one row per household-month, holds expected income) plus
   `budget_lines` (one row per budget-category, holds the cap). Existence of
   the parent row *is* "a budget is set for this month", so the empty state
   is one lookup and "all caps removed" (a parent with zero lines) stays
   distinguishable from "never set". Lines-only and caps-on-categories were
   rejected: the first leaves expected income homeless and fuzzes the empty
   state, the second contradicts decision 4 and needs an audit log for
   history.

8. **Reads and writes are both gated `money` + owner**, the Transactions
   shape, for the same reason: the page is nothing but figures, and a screen
   with every figure blank reads as broken rather than restricted.

9. **Caps and expected income live in the primary currency and carry no
   currency column.** A cap is a plan, not a transaction. Changing the
   household's primary currency changes what the numbers mean — the same
   accepted trade-off the currency-change screen already documents for
   accounts, restated in the spec so nobody adds a currency column to fix a
   non-defect.

10. **Export CSV in the history modal is deferred**, for the transactions
    spec's decision-7 reason verbatim: `apiFetch` throws on an ok response it
    cannot parse as JSON, so CSV needs its own non-JSON response path with
    its own guard and test. The tracker row says so.

11. **`api_test.go` splits by feature area before Budget's routes land.** The
    handover flags the file at 2036 lines and says split before the next
    feature adds a fifth block; this spec adopts that as a task rather than
    leaving it advice. Likewise `TransactionsPage.tsx`'s shape warning:
    `BudgetPage.tsx` keeps fetch orchestration in a hook from day one instead
    of copying the 500-line page and splitting later.

## Data model

Migration `00006_budgets.sql`:

```sql
CREATE TABLE budgets (
    id                    uuid PRIMARY KEY,
    household_id          uuid NOT NULL REFERENCES households (id),
    month                 date NOT NULL,          -- always the first of the month
    expected_income_minor bigint,                 -- NULL = not provided
    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL,
    UNIQUE (household_id, month)
);

CREATE TABLE budget_lines (
    id          uuid PRIMARY KEY,
    budget_id   uuid NOT NULL REFERENCES budgets (id) ON DELETE CASCADE,
    category_id uuid NOT NULL REFERENCES categories (id),
    cap_minor   bigint NOT NULL CHECK (cap_minor >= 0),
    UNIQUE (budget_id, category_id)
);
```

- `month` uses the same first-of-month convention `MonthTotals` already takes.
- Deleting a cap deletes its line. Archiving a category leaves its lines
  untouched: an archived category with July spend still renders in July
  (history stays true); it stops being offered for new caps.
- No `currency` column, per decision 9.

## The formulas, pinned

All figures are in the household's primary currency. "The month" is the
calendar month being viewed. These formulas are the contract; an implementer
who needs a different one changes this spec first.

| Figure | Formula |
|---|---|
| Spent (total and per category) | The Transactions "Spent this month" rule, reused exactly: expense-kind transactions only, each converted to primary currency; a transaction with no available exchange rate is **excluded** and the screen states how many were excluded, the ledger's own copy pattern |
| Budgeted | Sum of the month's caps |
| Remaining | Budgeted − Spent. May be negative; negative renders as over, never clamps |
| "66% used" | Spent ÷ Budgeted, rounded to the nearest whole percent. Budgeted = 0 → the figure is hidden, never `NaN` or `∞` |
| Days left | (last day of month − today) + 1 — today counts, you can still spend today. Past month → 0; future month → days in that month |
| "S$137/day left" | Remaining ÷ days left, floored to a whole unit. Hidden when Remaining ≤ 0 or the viewed month is not the current one |
| "On pace to save S$…" | = Remaining. Shown only while Remaining > 0 **and** the viewed month is current |
| Per-category over | Category spent > cap |
| Left to allocate | Expected income − Budgeted. Rendered only when expected income is set; may be negative (over-allocated), shown as such |
| History "Result" | Per closed month with a budget: Spent − Budgeted, signed (−S$420 under, +S$210 over). The current month shows "so far" instead |
| "Months under budget" | Of the closed months in range that have a budget row: those with Spent ≤ Budgeted |
| Spending by person | The month's expenses grouped by `PaidByMembershipID`, per-member converted totals. No synthetic "Kids (shared)" grouping — the design's grouping was seed-data storytelling; members render individually |

## API

All under `/api/v1`, all gated `money` capability **and** owner (decision 8),
CSRF on writes, same middleware chain as transactions. Month path segment is
`YYYY-MM`; malformed → 400. Every 2xx carries a JSON body.

| Route | Behaviour |
|---|---|
| `GET /budgets/{month}` | The whole screen in one response: `budget` (null when unset; else expected income and lines with caps), per-category spent, totals (budgeted, spent, remaining, percent used, days left, daily pace), spending by person, excluded-for-no-rate count. No budget row is still `200` with `"budget": null` plus the spent figures — the screen shows what was spent even before caps exist, and the empty state needs the month, not a 404 |
| `PUT /budgets/{month}` | Upsert the whole month in one transaction: expected income plus the full line set `[{categoryId, capMinor}]`. Full replace, not patch — the modal always holds the entire budget, and replace makes removed rows unambiguous. Duplicate category in the payload → 422 before any write; unknown or foreign category id → 422; negative cap → 422 |
| `GET /budgets/history?months=6` | Current month plus up to N closed months: per month budgeted, spent, result. Months without a budget row are skipped, not zero-filled |
| `POST /categories` | Create a category (name). Duplicate name against `UNIQUE (household_id, name)` → 409, surfaced inline |
| `PATCH /categories/{id}` | Rename, same duplicate guard |
| `POST /categories/{id}/archive` | Archive: hidden from dropdowns, transactions and budget lines keep it. No DELETE exists |
| `POST /categories/{id}/restore` | Undo archive |

Copy-last-month and the two templates are **not** endpoints. Import = `GET`
previous month + prefill + `PUT` this month; templates prefill client-side
from a fixed frontend table. The server stays one honest upsert with nothing
to keep consistent in two places.

Ports: `BudgetRepository` (Get, Upsert, History) is new and narrow.
`CategoryRepository` grows Create, Rename, Archive, Restore.
`TransactionRepository.MonthTotals` already returns what per-category and
per-person grouping need; the budget service groups, no new transaction
query.

## Screens and states

New route `/money/budget`, third link under the MONEY sidebar group (add to
`SPACE_PAGES`; the grouped sidebar already renders n links).

**Budget page** — `BudgetPage.tsx` stays a rendering shell; fetch
orchestration lives in a hook (decision 11).

1. **Empty state** (no budget row for the viewed month): the design's copy,
   "Create your first budget" opening a blank modal, template cards.
   "Import last month" renders only when the previous month has a budget — a
   dead template is never shown.
2. **Set state**: four stat cards (Budgeted, Spent so far, Remaining, Daily
   pace), the categories grid with per-cap bars and the over state, Spending
   by person, and the insight card — "On pace to save S$X" plus an
   over-category sentence derived from the over count. No rollover sentence
   (decision 1).
3. **Month picker** ‹ › with the month label. Past month: pace and on-pace
   cards hidden, "so far" language dropped. Future month: allowed, caps with
   zero spend.
4. **Spend without budget**: the month has transactions but no caps — empty
   state, but the header still names the month; no fake zeros.
5. **Excluded transactions**: the no-rate exclusion line, same copy shape as
   the ledger's.
6. **Archived-category caps**: the line renders with name and spend, marked
   archived, excluded from the new-cap dropdown.

**Edit budget modal** — `BudgetModal.tsx` on the shared `Modal` primitive.
Three cards: expected income (editable, blank allowed), Allocated, Left to
allocate (the last two hidden as a pair when income is blank). One row per
line: editable name (rename applied on save), cap input in minor units
(reusing the transaction modal's money-input handling — no floats), ✕ to
drop the cap, an archive control. "+ Add a category": dropdown of active
categories without a cap this month, plus "New category…" with an inline
name field. Save issues the single `PUT`; category creates/renames/archives
issue their own calls on save, before the `PUT`, and a failure keeps the
modal open with the error inline.

**History modal**: three summary cards (avg monthly spend, avg saved per
month, months under budget), up to six month rows with bars, current month
marked "so far". Clicking a month row closes the modal and switches the
page's month picker to it — the design's "full breakdown" is the page
itself, not a new screen. Export CSV deferred (decision 10).

## Error handling

- Adapters map missing rows to `domain.ErrNotFound`; no `pgx` type crosses
  the boundary.
- The `PUT` upsert is one transaction — a partial line write is impossible,
  and validation (duplicate category, unknown id, negative cap) happens
  before the first write.
- Category name collisions surface as 409 with a named error code, inline in
  the modal — never a silent overwrite.
- Fail closed on wire values: any category id or enum-ish string the code
  did not construct is refused in a `default` branch.
- Frontend tests stub with `stubFetchRoutes`, which throws on unregistered
  requests.

## Testing

- **Domain**: the formula table becomes table-driven tests. Boundaries:
  days-left on the 1st, on the last day, past and future months; percent
  with zero budgeted; negative remaining; daily-pace flooring. Designated
  mutations for the two likeliest drifts: the days-left `+1` and the
  spent rule's expense-only filter.
- **Usecase**: budget service against in-memory doubles — per-category and
  per-person grouping, archived-line retention, no-rate exclusion count,
  history skipping unbudgeted months.
- **HTTP**: route-walk matrix rows for every new route (unauthenticated,
  wrong household, non-owner, missing `money` capability, missing CSRF on
  writes) — added to the post-split test files, the split itself being an
  early task (decision 11).
- **Postgres**: upsert-replace semantics (a line absent from the payload is
  gone after `PUT`), both unique constraints, cascade on budget delete.
- **Frontend**: the six page states, modal add/rename/drop/archive flows,
  template prefills (including 50/30/20's income prompt), month-picker hide
  rules. New guard tests mutation-checked.

## Out of scope

| Not here | Where it lives |
|---|---|
| Rollover of unspent budget into a goal | Goals spec (decision 1) |
| "4 of 4 on track" | Goals |
| Overview's "July budget 66% used" card | Slice 5 (Overview); this feature ships the API it will read |
| Export CSV from history | Deferred (decision 10), tracker row |
| Budget over-spend alert emails | Notification preference exists; the alert job is its own later piece |
| Inline category editing in the ledger | Stays deferred as the Transactions spec left it |

## Definition of done

`make lint && make test` green; new guard tests mutation-checked; a
15-criterion browser walk on a wiped database, whose script is dry-run
against the state the walk itself creates (LEARNING pattern 13's
walk-arithmetic rule — no criterion may assert a counter the walk has
already moved); `docs/FEATURE_TRACKER.md` Budget rows updated with the
summary recounted; `docs/SYSTEM_DESIGN.md` gains the table pair, routes and
service; `docs/LEARNING.md` gains whatever the work teaches.
