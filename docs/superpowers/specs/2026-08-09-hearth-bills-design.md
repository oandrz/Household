# Hearth — Bills

Spec for slice 2's fifth and last feature: the Bills & subscriptions screen,
its Add-bill modal, the payment that finally connects a household's fixed costs
to the ledger those costs are missing from, and Overview's "Next bill" card.
Written 2026-08-09 after a brainstorming session; the decisions below record
what was chosen and why, in the order they were made.

The design source is the Bills section of `design/Household Dashboard.dc.html`
(screen `is_bills`, modal `modalBill`), plus the Overview card "Next bill ·
S$142.30 · SP utilities · Jul 8" and the "+ Add" menu's own Bill entry
("Recurring or one-off").

Bills is last in Money because it is the only feature left there, not because
anything was waiting on it. What *is* waiting on it sits outside Money: the
Family calendar draws bill dates on its month grid ("11 events & 6 bill dates
in July", legend "Everyone / bills"), so slice 4 cannot be whole until this
table exists. Overview's fourth card is waiting too, and ships here.

**Bills arrived with every figure undefined, and unlike Accounts, Transactions,
Budget and Goals it also arrived with figures that contradict themselves.** The
mockup's `Due this month S$918.68` equals its own paid list (572.20) plus its
due-soon list (326.50) plus a bill dated **5 August** — a different month — and
its `Paid so far S$316.98` matches nothing at all, its own three paid rows
summing to 572.20. Nothing here can be reverse-engineered from the picture.
Every figure below is therefore pinned in this document first, and the
divergence from the mockup's arithmetic is deliberate, not a defect to be
"corrected" later.

## Decisions

1. **Marking a bill paid writes a real expense into the ledger.** The payment
   creates a `transactions` row: kind `expense`, from the bill's pay-from
   account, in the bill's category, for the amount actually paid. Budget's
   `Spent`, the daily pace figures, Spending by person and net worth all then
   include the household's fixed costs without anybody entering them twice.

   **Goals' "moves no real money" precedent does not transfer here, and the
   difference is the whole point.** A goal *earmarks* — nothing has left the
   household — so decision 1 of the Goals spec deliberately refused to
   reconcile a goal against any account. A bill is an actual payment to an
   actual company. Recording it as anything less than a transaction would leave
   the Bills screen's own "Paid so far" figure disagreeing with the budget on
   the next page over.

   The accepted cost, stated plainly because it is the failure mode this
   creates: a household that marks a bill paid *and* separately hand-enters the
   same expense in the ledger double-counts it. The Mark-paid modal says so at
   the point of clicking, and the ledger row it writes carries the bill's name
   as its description so the duplicate is recognisable.

   The rejected third option was linking an existing ledger row: the household
   would still enter the transaction first and then hunt for it, and a bill
   with no match would be stuck half-paid.

2. **A bill is a template with a payment history, not a set of generated
   occurrence rows.** `bills` holds the recurring facts — name, amount,
   cadence, next due date, category, pay-from account, autopay, subscription
   flag. Paying writes a `bill_payments` row and advances `next_due` by the
   cadence. "Due soon" is each unpaid bill's own `next_due`, sorted: the design
   shows one row per bill, so no future occurrence needs generating at all.

   Materialised occurrences were rejected for needing generation — either a
   scheduler, which this codebase does not have and has twice refused to
   invent (Budget decision 1, Goals decision 4), or generate-on-read, which
   writes rows during a `GET`. A last-paid stamp with no history table was
   rejected for the opposite reason: "was July's electricity paid?" becomes
   unanswerable in August, and there is no `transaction_id` to delete when a
   payment is undone.

   The accepted cost: a single future occurrence cannot be skipped or re-dated
   on its own without moving `next_due`.

3. **Autopay is a display flag and changes no behaviour.** Every bill —
   autopay and manual alike — is marked paid by a person, through one code
   path. The flag drives the row's `Autopay` badge, the header's `N of M on
   autopay` count, and the wording of the overdue state. Nothing pays itself,
   because nothing in this product runs on a clock.

   The design's toggle copy therefore changes. "Mark as automatically paid —
   otherwise you'll get a reminder" promises two things that do not happen, and
   the reminder half is dead for the same missing scheduler (the Settings
   toggle "Bill due reminders (3 days before)" has existed and done nothing
   since slice 1). The toggle reads: **"The bank pays this one — we'll still
   ask you to confirm it went out."**

   The accepted cost, and it is a real one: an autopay bill the bank has
   already taken sits unpaid on screen until somebody confirms it, and goes
   overdue there while the money is genuinely gone. The flag earns its place at
   exactly that moment — it tells the household whether an overdue row is an
   errand or a click, which is why the overdue copy differs by kind (decision
   9). Auto-stamping on read was rejected outright: it writes rows during a
   `GET`, it invents money the bank may not have taken (a failed GIRO would
   show as paid), and utilities vary month to month so the amount written
   would be a guess that moves net worth.

4. **A subscription is a bill with a flag the household set, not a bill the
   code inferred.** The Add-bill modal carries "Counts as a subscription"; the
   rollup sums exactly what was ticked. Deriving it from the category was
   rejected because categories are household-editable and shared with
   transactions and budgets — renaming "Streaming" would silently empty the
   panel, and hard-coding a category name in Go is the kind of unconstructed
   value the fail-closed rule exists to refuse. Deriving it from cadence and
   amount was rejected for needing a threshold nobody decided.

   Two riders settled with it. A non-monthly cadence normalises: the panel
   states a monthly figure, computed as set out in the formula table below.
   And **"last reviewed Mar 2026" does not ship** — the design
   draws no control anywhere that could set that date, so it would be a figure
   no household could ever make true.

5. **"Due this month" counts the whole month including what is already paid,
   so it and "Paid so far" read as one fraction.** The two cards say "S$316.98
   of S$918.68", which is how a household reads two figures side by side, and
   both are explainable from one list. The alternative — a still-to-pay figure
   that counts down — leaves the month's total obligation nowhere on the
   screen.

   The accepted cost: a bill due on the 31st inflates the card all month even
   though it is weeks away.

   Rider: the **Due soon list is not month-bounded**. It shows each unpaid
   bill's next due date, soonest first, crossing into next month exactly as the
   design's own Aug 5 row does. And `N of M on autopay` counts unarchived bills
   only.

   **Every unpaid bill appears somewhere, under one of two headings.** A
   rolling 30-day window alone would make a yearly insurance bill invisible for
   eleven months of the year while the header kept counting it — the page would
   say "7 of 9 bills on autopay" above a list of three. So the same list
   carries a second heading: **Due soon** for anything overdue or within 30
   days, **Later** for everything beyond it, one row component for both. The
   design draws only the first heading, because its own mockup happens to have
   nothing further out.

6. **A payment can be undone, and undoing reverses all three writes.** Undo
   deletes the `bill_payments` row, deletes the expense it generated, and
   rewinds `next_due` to that payment's `due_on`. Bills is a write path into
   the money that drives Budget and net worth, so a mis-click must not leave an
   expense the household never made.

   **Only the most recent payment of a bill can be undone.** Undoing July on a
   bill also paid in August would rewind `next_due` behind a period that is
   still paid, and the screen would show a due date for money already spent.

   The accepted cost: if the household has since edited that transaction in the
   ledger, the undo throws their edit away.

   Rider: `bill_payments.amount_minor` is a fact of its own and
   `transaction_id` is `ON DELETE SET NULL`, so deleting the expense from the
   Transactions page leaves the payment history standing rather than erasing
   it.

7. **A bill has no currency column; it is denominated in its pay-from
   account's currency by construction.** This is not a free choice.
   `TransactionService.Create` already forces an expense's currency to its
   from-account's (`internal/usecase/transaction.go:232`), so any independent
   currency on a bill would be overwritten the moment it wrote one. The bill's
   `pay_from_account_id` is therefore `NOT NULL`: it supplies both the account
   the expense leaves and the currency the amount is in.

   Consequence, and it is refused rather than absorbed: **re-pointing a bill at
   an account in a different currency is rejected**, naming both currencies.
   `amount_minor` means something different under the new account, and silently
   restating a stored minor-unit figure is exactly the Critical defect
   `AccountModal` shipped on the transactions branch (`docs/HANDOVER.md` §1).
   Moving a bill across currencies means archiving it and creating a new one.

8. **A bill carries an optional payer, and Spending by person grows an
   Unattributed row.** The design's modal draws no payer field, but
   `BudgetService.Month` skips any transaction whose `PaidByMembershipID` is
   empty when building `ByPerson` while still counting it in `Spent`
   (`internal/usecase/budget.go:252`) — so unattributed bill payments would
   quietly stop that shipped card summing to the month's spend. A household
   holds this fact anyway: Andreas pays the utilities, month after month, so it
   is set once on the bill rather than chosen twelve times.

   Attributing to whoever clicks Mark paid was rejected as a worse lie than a
   blank: it records who acted, and writes are owner-gated, so in a two-owner
   household most bills would land on whoever happens to do the confirming.

   Rider that ships either way: **`ByPerson` gains an explicit Unattributed
   row** so its rows always sum to `Spent`. That closes a hole which already
   exists today — any hand-entered transaction saved without a payer vanishes
   from the card the same way, with nothing on screen saying so.

9. **The screen enumerates its states, and the design's "All bills covered"
   panel is re-purposed rather than shipped as drawn.** "Nothing needs manual
   payment this month" is false under decision 3: every bill needs a click now,
   so "needs manual payment" no longer separates anything. The panel fires when
   every bill due this month is paid and says so.

   The design draws no overdue state at all. That gap is closed here, with the
   five states listed under "Screens and states" below, the same way the
   Transactions ledger enumerates its five rather than discovering one in
   production.

10. **Overview's "Next bill" card and the "+ Add → Bill" entry ship in this
    slice.** Goals set the precedent: it shipped its own page and Overview's
    goals-on-track card together, because the card is composition over the hook
    the page already has. The card's figure — the earliest unpaid due date and
    its amount — is one the Bills header already computes. Overview goes from
    three of the design's eight cards to four.

    Family's calendar bill dates do **not** ship here. The calendar has no
    route, no table and no spec; building slice 4's foundation inside slice 2's
    last feature would be a much larger change than either deserves.

## Data model

Migration `00008_bills.sql`. Two new tables, no changes to existing ones.

```sql
CREATE TABLE bills (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id          uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name                  text        NOT NULL,
    amount_minor          bigint      NOT NULL CHECK (amount_minor > 0),
    -- No currency column, deliberately (decision 7). A bill is denominated in
    -- its pay-from account's currency, because TransactionService already
    -- forces exactly that on every expense it writes. Do not "fix" this by
    -- adding one: the value would be overwritten the moment a payment wrote a
    -- transaction, and the two would disagree in the meantime.
    cadence               text        NOT NULL
                                      CHECK (cadence IN ('one_off','monthly','quarterly','yearly')),
    -- NULL only for a settled one-off: the bill was paid and has no next date.
    -- A settled one-off is not auto-archived -- that would hide a record the
    -- household may still want to see.
    next_due              date,
    category_id           uuid        REFERENCES categories(id) ON DELETE SET NULL,
    -- NOT NULL because it supplies the currency as well as the account the
    -- expense leaves. No ON DELETE clause: accounts are never deleted, only
    -- archived, the same reason goals carry none.
    pay_from_account_id   uuid        NOT NULL REFERENCES accounts(id),
    -- Optional. Its absence is why BudgetByPerson grows an Unattributed row
    -- (decision 8) rather than silently dropping the spend.
    paid_by_membership_id uuid        REFERENCES memberships(id) ON DELETE SET NULL,
    -- Display only. Nothing in this product pays a bill by itself.
    autopay               boolean     NOT NULL DEFAULT false,
    is_subscription       boolean     NOT NULL DEFAULT false,
    -- A bill is archived, never deleted: bill_payments references it. The
    -- accounts/categories/goals precedent, for the same reason.
    archived_at           timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    -- An archived bill still occupies its name, exactly as an archived goal or
    -- category does. A collision with one offers restore rather than a bare
    -- 409.
    UNIQUE (household_id, name)
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
    -- page must not erase the household's record that the bill was paid.
    transaction_id uuid        REFERENCES transactions(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    -- Belt and braces beside the service's own check: a double-clicked Mark
    -- paid cannot write two payments for one occurrence.
    UNIQUE (bill_id, due_on)
);

-- The month figures walk one household's payments by the date they were due.
CREATE INDEX bill_payments_household_due_idx ON bill_payments (household_id, due_on);
```

**Four cadences, no weekly.** Nothing in a household's fixed costs recurs
weekly, and an unused `CHECK` value is an unused `switch` arm forever. A fifth
cadence needs a migration, deliberately: the Go parser fails closed on this
column too, and both layers refusing an unknown value is the house pattern
(`transactions.kind` and `goal_contributions.source` both carry it).

**`next_due` advances from `due_on`, never from today.** A bill paid three days
late would otherwise drift three days every month, and a year later sit a month
out. `domain.NextDue(cadence, dueOn)` owns the arithmetic and is a pure
function over values.

**The month-end case is pinned because Go gets it wrong by default.**
`time.Time.AddDate(0, 1, 0)` on 31 January returns **3 March**, not 28
February. `NextDue` advances by calendar month and clamps to the target month's
last day: 31 Jan → 28 Feb (29 in a leap year) → 31 Mar. `docs/HANDOVER.md` §6
records a `time.Truncate`-and-location mistake that shipped at two call sites
in this project; this is the same family, and it is the plan's designated
mutation.

**Household scoping gets the `GoalRepository` treatment.** `bill_payments` has
no database constraint tying its `household_id` to its bill's, so a row could
in principle carry a `household_id` that disagrees with the bill it names.
Every repository method that reads or writes a payment filters on
`household_id` **and** `bill_id` together — never on payment id alone, or a
payment leaks across households. This goes in the port's doc comment, where the
next implementer will read it.

## The formulas, pinned

**"Today" is `deps.Clock.Now()`, resolved in the handler and passed down as a
parameter** — the Budget and Goals convention, inherited by name rather than
restated. No service here reads `time.Now()` itself, because a service that
does cannot be tested at a boundary. Month arithmetic truncates in **UTC**,
exactly as `domain/budget.go` and `startOfMonth` already do. No household
clock, no new timezone concept.

Let `month` be the current calendar month in UTC.

These formulas are the contract; an implementer who needs a different one
changes this spec first.

| Figure | Formula |
|---|---|
| Due this month | `SUM(bill_payments.amount_minor)` where `due_on` falls in `month`, **plus** `SUM(bills.amount_minor)` over unarchived bills whose `next_due` falls in `month`. Both halves are required — see the warning below |
| Paid so far | `SUM(bill_payments.amount_minor)` where `due_on` falls in `month` |
| Next due | The earliest non-NULL `next_due` over unarchived bills, with that bill's name and amount. **Overdue** when it is before today. None → the card and the stat are omitted, never rendered as a zero |
| `N of M on autopay` | `M` counts unarchived bills; `N` counts those with `autopay`. `M = 0` hides the line rather than rendering "0 of 0" |
| Due soon (list) | Unarchived bills with a non-NULL `next_due` **within the next 30 days inclusive, or already past**, ordered by `next_due` ascending, ties by name. Overdue rows sort first by virtue of their earlier date |
| Later (list) | Every remaining unarchived bill with a non-NULL `next_due`, same ordering. Due soon and Later together account for every unpaid bill, so nothing the header counts is missing from the page |
| Paid this month (list) | `bill_payments` with `due_on` in `month`, newest `paid_on` first, ties by bill name |
| Subscriptions per year | Over unarchived bills with `is_subscription`: `monthly × 12`, `quarterly × 4`, `yearly × 1`. `one_off` is excluded — a one-off is not a subscription by definition |
| Subscriptions per month | The annual figure ÷ 12, floored |
| Next bill (Overview) | The same figure as *Next due*, rendered as the design's "S$142.30 · SP utilities · Jul 8". None → the card renders its own empty line, never a zero |

**Integer-first, one division.** Every normalisation multiplies up to an annual
total and divides exactly once at the end. No `float64` appears in any monetary
path — the 50/30/20 budget template shipped a `incomeMinor * 0.3` that drifted
a minor unit on a real income figure and was caught only in review
(`docs/LEARNING.md`, Domain and money catalogue). Checked against the mockup:
70.90/mo → 850.80/yr → ÷12 → 70.90.

**"Due this month" cannot be computed from `bills` alone, and the naive query
passes review.** A monthly bill paid on 8 July has `next_due = 8 August`, so
`SELECT ... WHERE next_due IN month` misses every bill already paid — which is
the entire "Paid so far" half of the pair this decision defines. The union of
`bill_payments.due_on` and unpaid `bills.next_due` is the figure. This warning
belongs in the port doc comment, not only here.

**Cross-currency sums convert into the household's primary currency before
adding**, through the same convert-then-add path `NetWorthSummary` uses. A bill
whose account currency has no available rate is **excluded, and the screen
names how many were excluded** — the `BudgetRolloverCard` precedent (commit
`8a1114b`), not a silent drop.

**The mockup's own figures are not reproducible under these formulas, and that
is deliberate.** `Due this month S$918.68` includes a bill dated 5 August, and
`Paid so far S$316.98` contradicts its own list, which sums to 572.20. Nobody
should "fix" this arithmetic back toward the picture.

## API

All under `/api/v1`, all gated on the `money` capability **and** owner — reads
included, the Transactions/Budget/Goals shape, because a bills screen with
every figure blank reads as broken rather than restricted. CSRF on writes, the
same middleware chain as budget and goals. Every 2xx except 204 carries a JSON
body; a household with no bills is `200 {"bills": [], "summary": {…}}`, never
204 and never 404.

| Route | Behaviour |
|---|---|
| `GET /bills` | The whole screen in one response: `summary` (due-this-month, paid-so-far, next-due with its bill name and overdue flag, the autopay counts, the excluded-for-no-rate count, the subscription monthly and annual totals), `dueSoon`, `later`, `paidThisMonth` and `subscriptions`. `?archived=true` returns archived bills instead, the `AccountRepository.List` contract |
| `POST /bills` | `{name, amountMinor, cadence, nextDue, categoryId?, payFromAccountId, paidByMembershipId?, autopay, isSubscription}`. Duplicate name against `UNIQUE (household_id, name)` → 409 naming the taken name, and offering restore when the holder is archived; unknown cadence → 422; archived pay-from account → 422 naming it; `amountMinor ≤ 0` → 422 |
| `PATCH /bills/{id}` | Any of name, amount, cadence, next due, category, payer, autopay, subscription flag. **Re-pointing `payFromAccountId` at an account in a different currency → 422 naming both currencies** (decision 7). Archiving is deliberately **not** a patchable field — see the archive routes below |
| `POST /bills/{id}/pay` | `{amountMinor, paidOn}` — the amount defaults to the bill's but is editable, because utilities vary. **One database transaction writes three rows:** the `bill_payments` row (`due_on` = the bill's current `next_due`), the `transactions` expense, and the advanced `next_due`. Refuses: an occurrence already paid → 409; an archived bill → 422; an archived pay-from account → 422; a settled one-off with `next_due IS NULL` → 422 |
| `DELETE /bills/{id}/payments/{paymentID}` | Undo. **One database transaction reverses all three**: deletes the payment, deletes its transaction when the link still points at one, and rewinds `next_due` to the payment's `due_on`. Anything but the bill's most recent payment → 409, naming which payment is undoable |
| `POST /bills/{id}/archive` · `/restore` | Stamps or clears `archived_at`. **Their own routes rather than a field on PATCH**, the reasoning `router.go` already records for accounts, categories and goals: if archiving were patchable, an ordinary rename that happened to include the field would archive the bill as a side effect of saving a name. Payment history is untouched either way |

Ports: `BillRepository` is new and narrow — `List`, `Get`, `Create`, `Update`,
`SetArchived`, `RecordPayment`, `UndoPayment`, `ListPayments`,
`MonthPaymentTotals`. `RecordPayment` and `UndoPayment` each own their whole
transaction; a partly-applied payment is not a state this port can produce,
which is the shape `guarding-partial-writes` exists to catch.

`BillService` reads the pay-from account's currency through the existing
`AccountLookup` port rather than declaring a second one, and builds the expense
with that currency — the same rule `TransactionService.Create` applies at
`transaction.go:232`, asserted by a test rather than assumed. Cadence
arithmetic and the overdue predicate live in `internal/domain/bill.go` as pure
functions over values, with `today` a parameter.

`BillService` holds no actor parameter, like every other service here:
authorisation is HTTP-layer only.

## Screens and states

New route `/money/bills`, replacing the `/money/$` placeholder — **the last
placeholder left in Money**. The `SPACE_PAGES` entry lands in the same change
as the route, which is the failure mode `docs/FEATURE_TRACKER.md`'s Marriage
note exists to prevent, and the route sits under `RequireCapability
cap="money"`.

`useBills.ts` owns every fetch and mutation. This is Budget decision 11
applied again on purpose: `BudgetPage.tsx` never grew the debt
`TransactionsPage.tsx` carries (over 500 lines doing fetch orchestration,
pagination, body translation and row rendering together), because its hook
existed from day one. `BillsPage.tsx` does the same.

Components: `BillStatCards`, `BillList` (Due soon, Later and paid-this-month
all share one row component), `SubscriptionsCard`, `BillModal` (add and edit,
one modal, the `TransactionModal` pattern), `MarkPaidModal`, plus
`billSchemas.ts` and `billCopy.ts`.

The five states the page must render, enumerated here so none is discovered in
production:

1. **No bills at all.** First run: the design's own "+ Add bill" as the primary
   action, no stat cards, no empty lists with headings above them.
2. **Bills exist, none due this month.** Stat cards render zeros with their own
   copy; the Due soon heading explains rather than sitting blank, and the bills
   themselves are visible under Later.
3. **Ordinary.** Due soon, Later and paid-this-month, populated as the data
   falls.
4. **All caught up.** Every bill due this month is paid: "All caught up —
   everything due in August is paid. Next bill: school fees, 15 Sep." An empty
   "Due soon" heading with nothing under it reads as a loading bug rather than
   an achievement, which is the failure the interim Overview's blank
   limited-member page already produced once.
5. **Overdue.** A bill whose `next_due` has passed unpaid sorts to the top of
   Due soon with its own marker, and the Next-due stat names it as overdue
   rather than printing a past date as though it were upcoming. The copy
   differs by kind, which is the one moment autopay earns its place: an autopay
   row reads **"Should have gone out on 24 Jul — confirm it did"**, a manual
   row reads **"Overdue since 24 Jul"**.

Mark paid opens a small modal: amount prefilled from the bill and editable,
date prefilled with today, pay-from account shown read-only, and the sentence
naming what it will do — write an expense — so the double-entry cost of
decision 1 is stated where somebody could incur it. Undo sits behind an in-page
confirmation, never `window.confirm`.

Overview gains `NextBillCard.tsx` and the "+ Add → Bill" entry, taking its
quick-create menu to four of six. `BudgetByPerson` gains its Unattributed row.

## Error handling

Fail closed on every value that arrives from a database column or a request. A
`switch` over `cadence` carries a `default` that refuses, beside the DB
`CHECK` — both layers, the house pattern.

`domain.ErrBillNameTaken` mirrors `ErrGoalNameTaken`. A missing row becomes
`domain.ErrNotFound` at the adapter boundary, never `pgx.ErrNoRows` further up.
A bill that exists in a different household is indistinguishable from one that
does not exist.

Each refusal names its reason rather than returning a bare code:

| Case | Response |
|---|---|
| Occurrence already paid | 409; `UNIQUE (bill_id, due_on)` is the backstop behind the service's own check |
| Undoing a payment that is not the most recent | 409, naming which payment is undoable |
| Paying from an archived account | 422, naming the account |
| Re-pointing a bill at a different-currency account | 422, naming both currencies |
| Name colliding with an archived bill | Offers restore, not a bare 409 — the categories and goals rule |
| Paying a settled one-off | 422 |

The frontend's `apiFetch` throws on an ok response it cannot parse as JSON, so
every 2xx except 204 carries a body — including archive and restore.

## Testing

- **Designated mutation: `domain.NextDue`'s month-end clamp.** A monthly bill
  due 31 January advances to 28 February (29 in a leap year). Remove the clamp
  and Go's `AddDate` returns 3 March — the test must go red, and be seen to.
- **The union figure.** A bill paid this month still counts toward "Due this
  month". The naive `WHERE next_due IN month` query passes code review and
  returns a wrong number, so this test is what stands between the two.
- **Undo refuses a non-latest payment**, and the message names the one it would
  accept.
- **Atomicity, both directions.** A forced failure on the third write of
  `RecordPayment` leaves no payment row and no expense; a forced failure inside
  `UndoPayment` leaves the payment, its transaction and `next_due` all intact.
- **Currency agreement.** The expense a bill payment writes carries the same
  currency `TransactionService.Create` would have set for that account.
- **`ByPerson` rows plus Unattributed sum exactly to `Spent`.**
- **Household scoping.** A payment id from another household is not found, not
  forbidden — the `GoalRepository` contract.
- Route-walk matrix in a new `bills_api_test.go`, following the `api_test.go`
  split Budget's Task 1 made.
- Every frontend test uses `stubFetchRoutes`; a stub that ignores the URL has
  silently passed broken code twice in this project.

**`00008` is the first migration since the `make up` skip was written down.**
Compose only re-evaluates `depends_on: migrate` when it recreates `api`, so a
stack left running across a new migration keeps its already-succeeded `migrate`
container and never reruns it. `make down && make up`, or an explicit `make
migrate`, before anything is walked.

## Out of scope

Said out loud so nobody builds it by accident or reports it as missing:

- **"Bill due reminders (3 days before)" stays dead.** The Settings toggle has
  existed since slice 1 and sends nothing. Sending it needs this codebase's
  first scheduler — refused by Budget decision 1 and Goals decision 4. It is
  the same later spec as automatic goal contributions and automatic month-end
  rollover: those three are one missing piece, not three gaps.
- **Autopay pays nothing** (decision 3).
- **Calendar bill dates** — slice 4, Family's own spec (decision 10).
- **No per-occurrence skip or re-date** — decision 2's accepted cost.
- **No CSV export.** Not drawn for Bills, and `apiFetch`'s JSON-only contract
  is untouched by this work.
- **No cadence beyond the four**, and none configurable without a migration.

## Definition of done

`make lint && make test` green, at least one mutation-checked test (the
`NextDue` clamp above), and the three documents updated **in the same change**:

- `docs/FEATURE_TRACKER.md` — Bills' four rows ⬜ → ✅, Overview's "Next bill"
  card and the "+ Add" menu row, plus new rows for what the design never draws:
  undo a payment, archive and restore a bill, and the Unattributed row on
  Spending by person. Recount the summary table by counting symbols, not by
  adjusting the stated totals.
- `docs/SYSTEM_DESIGN.md` §4 and §5, through the `maintaining-system-design`
  skill — new routes and their guards, two new tables, a new port, and a
  request flow that writes into `transactions` from outside Transactions.
- `docs/LEARNING.md` — whatever this work teaches, and `docs/HANDOVER.md` for
  where the next person picks up.

**And a 15-criterion browser walk on a real database before any "done" claim**,
covering three member states rather than only the seeded owner. The Goals walk
found a feature every layer of which existed and no screen ever called; the
interim Overview walk found a limited member looking at a blank page that every
unit test agreed with.
