# Bills — verification walkthrough

Run 2026-08-10 (host clock 06:14–06:50 +08; the API container is UTC, where
it was 22:14–22:50 on 2026-08-09 — **the same instant**, just a different
calendar-day label on each side, since +08 is eight hours ahead of UTC and
the walk ran entirely before the UTC day rolled over at 08:00 +08. This
means the browser's own `new Date()` — which every date input defaults to
and which `BillModal`'s/`MarkPaidModal`'s own `today()` reads from — named
**2026-08-10** throughout, one calendar day ahead of the server's own
`deps.Clock.Now().UTC()`, which named **2026-08-09** until the walk had
already finished with every date-sensitive criterion. Both sides agree on
the *month* (August 2026) for the whole walk, which is what every
month-scoped figure below depends on; every day-level date used in a
criterion was typed explicitly rather than taken from a default, for exactly
this reason, and is stated as typed, not as "today"). Driven in a real
Chromium (Claude in Chrome) at a resized 1440×1000 window against
`http://localhost:5173`, from a wiped database:

```
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
make down
docker volume rm hearth_hearth-pgdata
make up          # 00008_bills.sql confirmed in hearth-migrate-1's own log
make seed
```

Both Docker engines were checked before anything else, per `docs/HANDOVER.md`
§2: `docker ps` on colima showed the five live `hearth-*` containers; Docker
Desktop was not running at all (`docker --context desktop-linux ps` refused
to connect), so no phantom port was chased.

Criteria are Task 18 of `.superpowers/sdd/2026-08-09-hearth-bills/task-18-brief.md`,
verbatim. They were **not** walked in strict numeric order — criterion 11
("All caught up") only holds while every live bill is either paid-up-to-date
or not yet created, and criteria 6/7/9/10/13 all deliberately create overdue
or no-rate bills that would make "all caught up" permanently false once they
exist. Criterion 11 was walked immediately after criteria 2/3/5 (one bill,
freshly paid, nothing else on the household yet), before any overdue or
no-rate bill was created for the later criteria. The departure is recorded
here rather than silently reordering the table below, which still lists
every criterion by its own number.

**Result: 15 of 15 pass — after one real defect the walk itself found at
criterion 14, fixed, mutation-checked, re-walked live mid-walk, and then
swept for the class rather than closed on the one instance — the identical
gap turned out to be sitting in two more Money pages that are not Bills'
own.**

The defect is the reason this walk exists. `GET /bills` is money-AND-owner
gated exactly like `GET /goals`, `GET /budgets/{month}` and `GET
/transactions` (`router.go`'s own comment covers the whole group in one
sentence, not Bills alone), so a limited member holding `money` reaches
`/money/bills` — the route guard and the sidebar link both check only the
capability, never the role — and the request answers 403. `GoalsPage.tsx`
already distinguishes that routine, expected 403 from a genuine failure,
with its own `goals-owner-only` explanation. `BillsPage.tsx`, built
explicitly citing `GoalsPage.tsx` as the pattern to follow throughout its
own header comment, had never been given the equivalent branch: every
failure, the ordinary "you're not the owner" 403 included, answered with the
same red `bills-load-error` alert a genuine server outage would produce.
Fixing that one instance, and then re-reading `router.go`'s own comment
naming the whole group, was what prompted checking `BudgetPage.tsx` and
`TransactionsPage.tsx` against the same guard rather than treating Bills'
own fix as the end of the task — both had the identical gap, neither of them
a Bills file, neither one covered by so much as an absence test. See "The
defect, and the fix" below and `docs/LEARNING.md` pattern 1 (**fixing an
instance rarely fixes the class** — this is that pattern's twelfth recorded
instance, not pattern 2's, which is about a test that cannot fail; there was
no test here to fail in the first place).

---

## Setup, done through the product first

`make seed` on a wiped volume creates the household, its members, spaces and
notification preferences — and, confirmed again here, **zero** accounts,
categories, bills (`select count(*) from accounts, categories, bills` → all
0). Every precondition below was created through the running app, not
inserted:

- **Three accounts** (Finances page): **OCBC Everyday** (SGD, cash, opening
  S$5,000.00 as of 2026-01-01), **Jakarta Savings** (IDR, cash, opening
  Rp5,000,000), **US Brokerage** (USD, cash, opening $1,000.00 — Finances
  itself immediately read "1 account not included: no exchange rate for
  USD," the identical no-rate exclusion Accounts' own walk pinned).
- **Thirteen categories already existed** the first time the Budget page's
  own "Edit budget" modal was opened, with real UUIDs already assigned
  (`select id, name from categories` confirms it), before any category had
  been created through this walk — a pre-existing default-category bootstrap
  this walk did not go looking for the source of, since it is not a Bills
  criterion and every category used below (Utilities, Subscriptions) was
  simply picked from the list already offered.
- **An August 2026 budget**, one line (Utilities, cap S$500.00) — needed
  because `BudgetByPerson` and the populated `BudgetStatCards` row both only
  mount when `data.budget !== null` (`BudgetPage.tsx`'s own gate); without
  this, criteria 3 and 4 would have had no screen to read their figures from
  at all, the same gap Goals' own walk hit at its criterion 8.
- **A limited member, Jamie**, invited via `adminctl create-invite
  --role=limited --capabilities=money --inviter-email=andreas@hearth.family`,
  invite accepted in the browser (password set there, not typed into any
  admin tool), for criterion 14.

---

## The walk's own arithmetic, tracked as it went

Eight bills were created over the course of the walk, each existing to carry
a specific criterion or product question — named here once so the table
below can refer to them by name without re-deriving what each one is for.

| bill | account | cadence | created due | why |
|---|---|---|---|---|
| **Internet** | OCBC Everyday (SGD) | monthly | 2026-08-20 | c2, c3, c4, c5, c11 |
| **Cleaning service** | OCBC Everyday (SGD) | monthly | 2026-07-25 (overdue, manual) | c10, PQ1 |
| **Phone** | OCBC Everyday (SGD) | monthly | 2026-08-03 (overdue, autopay) | c6, c8, c9, c10 |
| **Rent** | OCBC Everyday (SGD) | monthly | 2026-01-31 (overdue) | c7 |
| **Netflix** | OCBC Everyday (SGD) | monthly | 2026-09-15, subscription | c12, PQ2 |
| **Jakarta apartment** | Jakarta Savings (IDR) | monthly | 2026-08-25 | c13 (converts) |
| **US streaming** | US Brokerage (USD) | monthly | 2026-08-28, subscription | c13 (no rate), PQ3 |
| **Car loan** | OCBC Everyday (SGD) | monthly | 2026-01-01 | c15 |

**Internet's own figures across the walk it carries**, S$65.00 throughout as
the bill's stored amount, though `MarkPaid`'s own amount is a per-payment
override (used deliberately for payment 2, below):

| after | dueThisMonth | paidSoFar | next due |
|---|---|---|---|
| c2 create | S$65.00 | S$0.00 | Aug 20 |
| c5 archive → restore | S$65.00 | S$0.00 | Aug 20 (unchanged by archive/restore) |
| c3 pay #1 (S$65.00, payer Andreas) | S$65.00 | **S$65.00** | Sep 20 |
| c11 (no other bill exists yet) | — | — | **"All caught up"** |
| c4 payer cleared, pay #2 (**S$70.00**, unattributed) | S$65.00 | S$65.00 | Oct 20 |

`dueThisMonthMinor` stays S$65.00 through both payments because it sums
*due-this-month* bills and payments (Aug 20's occurrence), and neither
payment's `due_on` after the second payment is in August any more — the
second payment settled the Sep 20 occurrence, not a second August one.
`paidSoFarMinor` on the Bills page (bucketed by `due_on`) also stays at
S$65.00 after payment 2, for the identical reason — see product question 1.

---

## Criterion by criterion

| # | Criterion | Result |
|---|---|---|
| 1 | A household with no bills sees the first-run state, not an empty table | **PASS** — before any account or bill existed, `/money/bills` rendered `data-testid="bills-empty-state"` with `bills-create-first` present, and `bills-due-soon`/`bills-stat-due-this-month` **absent** — a household with zero bills never mounts a table, empty or otherwise. Body text: "No bills yet / Add the household's fixed costs — rent, insurance, subscriptions — and Hearth will track what's due and what's already been paid. / Add your first bill." |
| 2 | `+ Add bill` creates a monthly bill; it appears under Due soon with its category, autopay badge and account | **PASS** — Internet (S$65.00, monthly, due 20 Aug, category Utilities, autopay on, pay-from OCBC Everyday, payer Andreas). `bills-due-soon` read exactly: `"AUG\n20\nInternet\nUtilities · autopay · OCBC Everyday\nAutopay\nS$65.00\nMark paid\nArchive"`. Stat cards: Due this month S$65.00, Paid so far S$0.00 · Nothing paid yet, Next due Aug 20 · Internet. Screenshot `02` |
| 3 | Mark paid writes a real expense — open the Transactions page and see the row, with the bill's name as its description — and Budget's `Spent` for the month moves by exactly that amount | **PASS** — Marked Internet paid (S$65.00). Transactions page: one row, "TODAY · AUG 10", description **"Internet"**, "OCBC Everyday · Andreas · Utilities", **−S$65.00**, "Spent this month S$65.00". Budget (August): Spent so far **S$0.00 → S$65.00**, exactly the payment — Budgeted S$500.00, Remaining S$435.00, Utilities S$65.00/S$500.00. Screenshot `04` |
| 4 | Spending by person shows the payment under the bill's payer; with the payer cleared, it shows under **Unattributed**, and the rows still sum to `Spent` | **PASS** — Payment 1 (payer Andreas, S$65.00) read under "Andreas" on Budget's Spending by person. Internet's payer was then cleared (Edit bill → Paid by → Unassigned) and marked paid a second time (S$70.00, deliberately a different amount from the bill's own stored figure, to prove the two payments are independent). Spending by person then read **Andreas S$65.00 / Unattributed S$70.00**, and Spent so far read **S$135.00** — 65 + 70 exactly. Screenshot `06` |
| 5 | **Archive a live bill from its own row**, find it under "Show archived", restore it, and confirm it is back in Due soon. Goals shipped this feature with no way in; walk it, do not infer it | **PASS** — clicked `aria-label="Archive Internet"` on the live row itself (not a separate archived-view control — unlike Goals' own defect, Bills' Archive button ships on every live row from the same commit that built the list, per `docs/FEATURE_TRACKER.md`'s own note). With the household's only bill archived, `bills-empty-state` rendered (no live bills) **and** — the sibling-not-nested case `BillsPage.tsx`'s own tests pin — toggling "Show archived" surfaced `bills-archived-section` reading `"Internet(archived)\nUtilities · autopay · OCBC Everyday\nAutopay\nS$65.00\nRestore"` at the same time, so the way back out was never lost. Restore returned it to `bills-due-soon` unchanged (Aug 20, S$65.00), `bills-empty-state` gone. Screenshot `03` |
| 6 | `next_due` advanced by one month **from the due date**, not from the date you clicked — pay one deliberately late and check | **PASS** — Phone, due 2026-08-03 (created overdue, since browser-local "today" was Aug 10), paid on 2026-08-10 (7 days late by the browser's own clock, 6 by the server's UTC one — late either way). `next_due` read **Sep 3**, confirmed by direct query: `select next_due from bills where name='Phone'` → `2026-09-03`. Had it advanced from the click instead, it would read 2026-09-10. |
| 7 | A bill due on the 31st, paid, lands on 28 February and then on **31 March**, not 28 March. This is the anchor-day behaviour; it needs two payments to be visible | **PASS** — Rent, `due_anchor_day` fixed at 31 by `Create` (never touched by either payment, confirmed by query throughout). Due 2026-01-31 → paid → **`next_due = 2026-02-28`** (2026 is not a leap year, so clamped from 31) → paid again → **`next_due = 2026-03-31`** (unclamped, back to the real anchor day since March has 31 days) — not 2026-03-28, which is what a clamp-of-the-clamp would have produced. `select due_anchor_day, next_due from bills where name='Rent'` → `31 \| 2026-03-31`. Screenshot `09` |
| 8 | Undo removes the payment, removes the transaction from the ledger, and rewinds the due date | **PASS** — Undid Phone's Aug 20 payment (its second, most recent one — see criterion 9's own setup). `select * from bill_payments` lost that row; `select * from transactions` lost its linked expense row entirely (confirmed the specific `transaction_id` was gone, not merely orphaned); `next_due` rewound **Sep 20 → Aug 20** exactly, the due date the payment had settled. Bills page: "Paid so far" dropped S$155.00 → S$110.00, Phone moved back into Due soon. Screenshot `08` |
| 9 | Undo on an older payment is refused, naming the one that can be undone | **PASS** — Phone was paid twice: once for its Aug 3 occurrence, then (after editing `next_due` to Aug 20 so a second August-bucketed payment would exist to test against) once for Aug 20. Clicking Undo on the **Aug 3** row (the older payment, still visible since both due dates fell in August) and confirming produced, in place on that same row: **"Only the most recent payment, due 2026-08-20, can be undone."** — naming the newer payment, not the one that was clicked. The Aug 3 payment stayed untouched (`bill_payments` still held both rows after the refusal). |
| 10 | An overdue autopay bill and an overdue manual bill read differently | **PASS** — Cleaning service (manual, no autopay): **"Overdue since 25 Jul"**. Phone (autopay): **"Should have gone out on 3 Aug — confirm it did"**. Both rendered together, `Overdue` pill on each, matching the task-12 brief's own two example strings verbatim in shape. Screenshot `07` |
| 11 | All-caught-up state appears once every bill due this month is paid, and names the next one | **PASS, walked immediately after criteria 2/3/5** (see the reordering note above — no overdue or no-rate bill existed yet). With only Internet on the household, paid once: `bills-all-caught-up` read **"All caught up — everything due in August is paid. Next bill: Internet, 20 Sep."**, gated correctly on `dueThisMonthMinor (S$65.00) === paidSoFarMinor (S$65.00)`, `excludedNoRate === 0`, and the earliest bill (Internet itself) not overdue. Screenshot `05` |
| 12 | A subscription-flagged bill appears in the panel; the monthly and annual figures agree (`× 12`) | **PASS** — Netflix, S$15.99/mo, "Counts as a subscription" ticked. Subscriptions panel: heading **"Subscriptions · S$15.99/mo"**, row **"Netflix S$15.99"**, footer **"S$191.88/year"**. Arithmetic: 15.99 × 12 = 191.88 exactly (integer minor units: 1,599 × 12 = 19,188 minor = S$191.88, no rounding involved). Screenshot `10` |
| 13 | A bill on a second-currency account converts into primary in the stat cards, and a currency with no rate is **excluded and counted on screen** | **PASS, both halves** — Jakarta apartment (IDR, Jakarta Savings, Rp1,241,000, due 25 Aug): "Due this month" moved **S$155.00 → S$255.00**, +S$100.00 exactly (Rp1,241,000 ÷ 12,410 = S$100.00 exact, the design's own static rate, no rounding). US streaming (USD, US Brokerage, $9.99, due 28 Aug, also ticked as a subscription): "Due this month" **stayed at S$255.00** — not counted — and `bills-excluded-no-rate` read **"1 bill is not counted: no exchange rate."** Screenshot `11` |
| 14 | A limited member holding `money` is refused the Bills page — reads included — **and then loads Overview as that same member** and sees the limited-member panel, not a page with a heading and nothing under it. Bills gives Overview a fourth failing query; this is the criterion that proves it did not blank the page (`docs/LEARNING.md` pattern 2) | **FAIL on the first walk (Bills' own page, not Overview) — PASS after the fix, re-walked live, then swept for the class.** `GET /api/v1/bills` as Jamie (limited, holds `money`, not owner) answered **403 `FORBIDDEN` "Only an owner may do that."`** both before and after the fix — the refusal itself was never in question. Before the fix, `/money/bills` rendered the same `bills-load-error` red alert ("Couldn't load your bills.") a genuine outage would produce. After the fix: `bills-owner-only` — **"Bills / Owner only / Bills is visible to the household owner. Ask them if you'd like to see where things stand."** Overview, both before and after (this half never needed a fix — that is the criterion's own literal claim, and it held throughout), rendered `overview-limited-heading` — "Money / Amounts are hidden for your account. The accounts shared with you are in Finances. / Go to Finances" — 223 characters of real content, not a blank page. **The sweep prompted by this fix then found the identical gap in `/money/budget` and `/money/transactions`, neither of them a Bills page** — both fixed and re-walked as Jamie the same way: `budget-owner-only` reading "Budget / Owner only / Budget is visible to the household owner...", `transactions-owner-only` reading the equivalent for Transactions. See "The defect, and the fix" below. Screenshots `13`, `14`, `16`, `17` |
| 15 | Overview's Next bill card shows the earliest unpaid bill and moves without a reload after `+ Add → Bill` | **PASS** — Card read **"Next bill / S$1,200.00 / Rent · Overdue"** (Rent, due 2026-03-31, the earliest `next_due` across every live bill at that point). A `window.__noReloadSentinel` value was set before opening `+ Add → Bill`; it survived the whole round trip (a full page reload would have wiped it). Created **Car loan** (S$350.00, due **2026-01-01** — earlier than Rent's Mar 31), and the card updated, with no navigation and no reload, to **"S$350.00 / Car loan · Overdue"**. Viewed inline, not saved to disk (see the Screenshots section below) — the numbers above are the DOM/sentinel reads taken at the time, which is the evidence this walk's own instructions call for |

---

## The three product questions

**1. Paying an overdue prior-month bill surfaces nowhere in "Paid this
month."** Walked directly: Cleaning service (due 2026-07-25) was marked paid
on 2026-08-10. The payment succeeded with no error — `select * from
transactions where description='Cleaning service'` shows a real S$45.00
expense dated 2026-08-10 — but on the Bills page itself, the screen the
household had just used to make the payment: **`bills-paid-this-month` did
not gain a row for it**, and **"Paid so far" stayed at exactly S$110.00**,
unmoved by a payment that had just, genuinely, happened. What a household
sees: they click Mark paid on an overdue bill from last month, the modal
closes with no error, the bill itself visibly moves (its badge changes from
"Overdue" to a new August due date, since Jul 25 + 1 month = Aug 25) — but
the two figures that exist specifically to answer "did I pay this" this
month give no acknowledgment at all. The money is real and correctly
recorded (it also fed Budget's August Spent, confirmed separately for
Rent's own two backdated payments, S$2,400.00 total, both landing in
August's ledger because `Budget.Spent` buckets by the transaction's own
`occurred_on`, never by the bill's `due_on`); it is only invisible on the
one screen — Bills' own "Paid this month" — whose entire job is bucketing by
`due_on`. A household paying down several months of arrears in one sitting
would see every payment succeed and almost none of them "count" on the
screen they are using to track that they paid.

**2. A no-payer bill paying into a month with no budget is invisible on
Spending by person.** Walked directly: Netflix (no payer set) was marked
paid with `paidOn = 2026-09-15`, landing the transaction in September.
September has no budget row (`select * from budgets` — only August exists).
Navigating Budget to September rendered the **empty state** — "No budget
set for September yet" — and `BudgetByPerson` never mounted at all;
"Spending by person" as a panel does not exist on that screen in that state.
`select description, occurred_on, amount_minor, paid_by_membership_id from
transactions where description='Netflix'` confirms the S$15.99 expense is
genuinely on the ledger, dated September, payer-less — real money, invisible
specifically because the month it landed in has no budget, independent of
whether a bill was involved at all. This is `BudgetByPerson`'s documented
gate (`BudgetPage.tsx`'s own `data.budget !== null` branch) doing exactly
what it was built to do; the walk's job was to show what a household
actually experiences from it, not to flag it as a defect.

**3. A subscription in a currency with no FX rate renders its row while
contributing to neither the monthly nor the annual total, explained only by
a page-level footnote.** Confirmed directly by criterion 13's own US
streaming bill, which is deliberately both a no-rate currency **and** ticked
as a subscription: the Subscriptions panel rendered its row — **"US
streaming $9.99"** — in its own currency, exactly as every other row does,
while the panel's own heading and annual line stayed at **S$15.99/mo** and
**S$191.88/year** — Netflix alone, unmoved by US streaming's presence. The
only place a household learns why is the page-level footnote beneath the
whole two-column layout, **"1 bill is not counted: no exchange rate,"**
which does not name *which* bill, does not say *which* total(s) it is
missing from, and sits well below the Subscriptions panel rather than inside
it (`BillsPage.tsx`'s own comment explains this placement is deliberate: the
count is deduplicated across three totals — due this month, paid so far, and
subscriptions — so nesting it inside any one card would misattribute the
other two). A household glancing only at the Subscriptions card would see a
row that looks charged and counted, sitting directly beneath a total that
does not include it, with the explanation one scroll away and speaking of
"bills," plural and unspecific, rather than naming the one in front of them.

---

## The defect, and the fix

**Criterion 14 read differently on Bills' own page than it should have.**
`GET /bills` is money-AND-owner gated, identically to `GET /goals`, `GET
/budgets/{month}` and `GET /transactions` (`router.go`'s own comment on the
whole `txn` group: "there is no reading of it for a limited member that
would not read as broken"). A limited member holding `money` reaches
`/money/bills` at all — `moneyGuardRoute` and the sidebar link both check
only the capability, never the role — so the 403 is not hypothetical, it is
the ordinary, designed-for outcome of a real member state. `GoalsPage.tsx`
already has a branch for exactly this: `goals.error instanceof ApiError &&
goals.error.status === 403` renders `goals-owner-only`, a calm, correctly-
worded explanation, distinct from `goals-load-error`'s red alert for a
genuine failure. `BillsPage.tsx`'s own header comment cites `GoalsPage.tsx`
as its pattern throughout — for the empty state, for Archive/Restore, for
the whole page's shape — but this one branch had not been carried over:
`bills.error` went straight to `bills-load-error` regardless of status, so a
limited member who followed nothing more alarming than the sidebar's own
Bills link landed on **"Couldn't load your bills."** in `role="alert"` red
text, indistinguishable from what a database outage would produce.

No test covered either shape of `bills.error` before this walk — not even
an absence test to have been fooled by, the way the interim Overview's own
three tests were (`docs/LEARNING.md` pattern 2, a genuinely different shape
from this one: there, a test existed and could not fail; here, no test
existed at all). The gap was simply never written.

**Fixing that one instance is what surfaced the other two.** Re-reading
`router.go`'s own comment — which names the whole money-AND-owner group,
transactions/categories/budgets/goals/bills, in one sentence rather than
singling out goals or bills — prompted checking every other page built
against the same guard rather than treating Bills' own fix as the end of
the task (`docs/LEARNING.md` pattern 1: fixing an instance rarely fixes the
class, now its twelfth recorded instance). Both siblings had the identical
gap:

- **`BudgetPage.tsx`** collapsed `budget.error || !budget.data` into one
  branch, so a 403 and the ordinary post-TanStack-Query type-narrowing guard
  answered with the same generic `BUDGET_COPY.loadError` alert — one line
  worse than Bills', since splitting the two conditions apart was itself
  part of the fix.
- **`TransactionsPage.tsx`** had `transactionsQuery.isError` render a bare
  inline `"Couldn't load your transactions."` with no owner-only branch and
  no test in either direction, the identical shape of gap as Bills' own.

`FinancesPage.tsx` (`GET /accounts`) was checked and needs no equivalent
fix: that route requires only `money`, never `owner` (`router.go`'s own
comment: "Accounts... reads need money; writes need money and owner"), so
any error a member who already passed the `/money` capability gate sees
there is a genuine failure, not this routine 403 — there is no owner-only
state to distinguish. `BudgetHistoryModal.tsx` and `GoalContributionsPanel.tsx`
read money-AND-owner routes of their own (`GET /budgets/history`, `GET
/goals/{id}/contributions`) but are reachable only from inside a page that
has already loaded successfully as an owner, so a non-owner can never open
either — no equivalent gap to close there.

**The fix** mirrors `GoalsPage.tsx`'s branch exactly, in all three files:

- `web/src/features/money/BillsPage.tsx` — imports `ApiError`, and
  `bills.error` now branches on `bills.error instanceof ApiError &&
  bills.error.status === 403`, rendering a `data-testid="bills-owner-only"`
  section (page title, "Owner only," and the explanation) before falling
  through to the existing `bills-load-error` for anything else.
  `web/src/features/money/billCopy.ts` gains `ownerOnlyHeading`/`ownerOnlyBody`.
- `web/src/features/money/BudgetPage.tsx` — the collapsed guard is split:
  `budget.error` now branches on status first (`data-testid="budget-owner-only"`
  or the existing `data-testid="budget-load-error"`), with a separate
  `if (!budget.data) return null;` guarding the type only, the identical
  shape `GoalsPage.tsx`'s own post-error `!goals.data` guard already uses.
  `web/src/features/money/budgetCopy.ts` gains the same two keys.
- `web/src/features/money/TransactionsPage.tsx` — `transactionsQuery.isError`
  gains the identical branch (`data-testid="transactions-owner-only"` /
  `data-testid="transactions-load-error"`). The new copy lives in
  `transactionCopy.ts`'s `TRANSACTIONS_COPY` (`ownerOnlyHeading`/
  `ownerOnlyBody`), matching `billCopy.ts`/`budgetCopy.ts` rather than the
  page's own pre-existing inline `"Couldn't load your transactions."`
  string — caught on a final diff pass, since writing the new copy inline
  would have quietly started a second convention for exactly the two
  strings this sweep added, in a file whose own header comment states the
  reason a copy module exists at all (keeping components free of exports
  eslint's `react-refresh/only-export-components` rule has to reason
  about). The pre-existing inline load-error string is untouched — moving
  it too would be a second, unrelated cleanup this sweep does not need.
- Six new tests total, two per page, each the same shape as
  `GoalsPage.test.tsx`'s own pair: a 403 renders the owner-only explanation
  with both expected phrases and **not** the generic alert; a 500 renders
  the generic alert and **not** the owner-only explanation.

**Mutation-checked, independently, in all three files.** Each page's own
`if (false && status === 403)` sent that page's new "renders the owner-only
explanation" test red — `BillsPage.test.tsx`: `Unable to find an element by:
[data-testid="bills-owner-only"]`; `BudgetPage.test.tsx`: the identical
message for `budget-owner-only`; `TransactionsPage.test.tsx`: the identical
message for `transactions-owner-only` — restoring the real condition turned
each back green (15/15, 37/37, 18/18 in their own files; 460/460 across the
whole frontend suite, up from 454 before this walk's six additions).

**Re-walked live**, not merely re-tested: `docker restart hearth-web-1`
(host edits to `web/src/**` do not reliably reach the dev server's
chokidar-backed hot reload — the exact issue `docs/HANDOVER.md` §2 names),
then Jamie's session reloaded `/money/bills`, `/money/budget` and
`/money/transactions` in turn and read each page's new copy exactly as
written, before-and-after screenshots for Bills (`13`) and Overview (`14`,
confirming the second half of the criterion was never broken), then the two
sibling pages after their own fix (`16`, `17`).

`docs/SYSTEM_DESIGN.md` was deliberately **not** touched: no route, table,
port or guard changed on any of the three pages — only a JSX branch and its
copy in each, the identical reasoning Goals' own walk gave for leaving that
document alone after its own archive-button fix.

---

## The state the walk ends in

Stated exactly, since several criteria depend on reading it precisely.

- **Eight bills**, all live, none archived by the end (Internet was archived
  and restored mid-walk at criterion 5; nothing else was archived).
  Internet: S$65.00/mo, next due 2026-10-20, unattributed. Cleaning service:
  S$45.00/mo, next due 2026-08-25 (settled its one overdue occurrence).
  Phone: S$45.00/mo, next due 2026-08-20 (its second payment was undone at
  criterion 8, rewinding it here). Rent: S$1,200.00/mo, next due
  2026-03-31, still overdue and unpaid past its second clamped occurrence.
  Netflix: S$15.99/mo, next due 2026-10-15, subscription. Jakarta
  apartment: Rp1,241,000/mo, next due 2026-08-25, unpaid. US streaming:
  $9.99/mo, next due 2026-08-28, unpaid, subscription, no FX rate. Car
  loan: S$350.00/mo, next due 2026-01-01, overdue, unpaid.
- **Seven payments** in `bill_payments`: Rent × 2 (Jan 31, Feb 28), Cleaning
  service × 1 (Jul 25), Phone × 1 (Aug 3 — the Aug 20 one was undone),
  Internet × 2 (Aug 20, Sep 20), Netflix × 1 (Sep 15).
- **Seven transactions** on the ledger, matching the seven payments above
  one-for-one (`transaction_id` on every surviving `bill_payments` row
  resolves).
- **Bills page (August), live**: Due this month **S$300.00** (Rent's own
  Mar-31 overdue occurrence does not count toward August the way an
  August-dated bill would — this total sums currently-due-in-August bills
  and payments only), Paid so far **S$110.00**, Next due **Overdue · Rent**.
  `excludedNoRate` = 1 (US streaming).
- **Budget (August)**: Spent so far **S$2,625.00** (every transaction dated
  in August, bill-driven or not, sums here regardless of `due_on`) against a
  S$500.00 cap — 525% used, Remaining **−S$2,125.00**. Spending by person:
  Andreas S$110.00, Unattributed S$2,515.00.
- **Budget (September)**: no budget row. Netflix's S$15.99 payment is on
  the ledger, dated there, and invisible on this screen (product question 2).

---

## Screenshots: 15 files, 15 distinct hashes

`shasum -a 256` over `docs/superpowers/plans/2026-08-09-hearth-bills-screenshots/`
returns **15 distinct hashes across 15 files** — no accidental duplicate
this walk (files `16` and `17` are the sibling fix's own before/after,
`BudgetPage.tsx` and `TransactionsPage.tsx` re-walked as Jamie after their
fix). The numbering runs `02`–`14`, `16`–`17`: two criteria's own screens —
**1** (the first-run empty state) and **15** (Overview's Next bill card
moving without a reload) — were viewed inline during the walk but not saved
to disk before the state moved on to the next criterion. Both criteria's
numbers in the table above (criterion 1: `bills-empty-state` present,
`bills-due-soon`/every stat card absent; criterion 15: the card's own text
and the `window.__noReloadSentinel` surviving the round trip) are asserted
directly from the DOM/sentinel read taken at the time, which is the
evidence this walk's own instructions call for — screenshots are the
record, not the evidence.

---

## Observations outside this walk's scope

Recorded rather than fixed, because it is not a Bills criterion and would be
a scope change of its own — the same standard Goals' own walk held its three
observations to.

- **The undo-refusal message prints an ISO date where every other date on
  the page reads "20 Aug."** Criterion 9's own text —
  `"Only the most recent payment, due 2026-08-20, can be undone."` — comes
  from `writeUndoPaymentError` (`bill_handlers.go`), which formats
  `BillPaymentNotLatestError.MostRecentDueOn` with `occurredOnLayout`
  (`"2006-01-02"`, the wire format used for every date-only field in a
  request or response body) and `BillsPage.tsx`'s own `apiErrorMessage`
  passes the server's string through **verbatim** — the same convention its
  own comment gives for why the client does no further composition on any
  of `MarkPaid`'s refusals. Every other date on this same screen — the row
  badges, the stat cards, the All-caught-up panel's "Next bill: X, 20 Sep."
  — goes through `billCopy.ts`'s own `dayMonthLabel`/`monthDayLabel`
  helpers instead. A household reading this one sentence sees a date format
  that appears nowhere else in the product. Not fixed here because the fix
  is server-side (either reformat at the handler, which would make this the
  one error message with bespoke date formatting rather than the wire
  format every other error shares, or accept the ISO form as intentional
  wire-format consistency and note it) — a real product decision, not a
  three-line mirror of an existing pattern the way this walk's own fix was.

---

## The gate

```
./scripts/arch-lint.sh
architecture lint passed
cd web && npx tsc --noEmit
cd web && npm run lint
cd api && go vet ./...

cd api && go test ./... -count=1 -timeout=5m
ok  	github.com/andreasoentoro/hearth/api/cmd/adminctl                    0.488s
ok  	github.com/andreasoentoro/hearth/api/cmd/api                         2.340s
?   	github.com/andreasoentoro/hearth/api/internal/adapter/clock          [no test files]
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/crypto         1.007s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/fx             1.467s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/http           93.088s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/mail           1.856s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/postgres       112.930s
?   	github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen  [no test files]
ok  	github.com/andreasoentoro/hearth/api/internal/config                 3.076s
ok  	github.com/andreasoentoro/hearth/api/internal/domain                 3.463s
?   	github.com/andreasoentoro/hearth/api/internal/testsupport            [no test files]
ok  	github.com/andreasoentoro/hearth/api/internal/usecase                4.857s

cd web && npx vitest run
 Test Files  44 passed (44)
      Tests  460 passed (460)
```

Ten Go packages `ok`, zero `FAIL` or `panic:` lines anywhere in the captured
output, 460 frontend tests across 44 files — including the six new tests
this walk's own fix (and its sweep across `BudgetPage.tsx`/
`TransactionsPage.tsx`) is pinned by, two per file — arch lint / `tsc` /
eslint / `go vet` all clean, overall exit **0**. This is the run against the
final tree, after the sweep, not the interim 456 the fix-only version of
this file reported. The "Not implemented: Window's scrollTo()" lines are
jsdom's own routine warning, present on every run of this suite, not a
failure.
