# Interim Overview (M2) — verification walkthrough

Run 2026-08-01 (host clock 2026-08-01 06:24 +08; the API container is UTC,
where it was still 2026-07-31 22:24 — that skew is the subject of criterion 12
and is worth reading before anything else), in a real Chrome via browser
automation, against http://localhost:5173. Criteria derive from Task 6 of
`2026-07-31-hearth-interim-overview.md`, adjusted for the two scope decisions
recorded below.

**Result: 14 of 14 pass — after one real defect the walk itself found at
criterion 9 was fixed, mutation-checked and re-verified live mid-walk. A
second defect, the sibling of that one, was found by review afterwards and is
recorded below rather than folded into a criterion, because no criterion here
would have caught it.**

The defect is the reason this walk exists. A limited member holding the money
capability saw a page containing the word "Overview" and nothing else. All
eleven unit tests for the page passed against it, before and after, because
every test covering that member asserted the *absence* of something — no
budget card, no checklist, no "+ Add" — and absence held perfectly while the
page was blank. Nothing asserted that anything was present. Screenshots
`04-limited-member-with-money.jpg` shows the fixed state.

**Two departures from the plan's own script, decided before the walk:**

1. **The checklist has three steps, not four.** The plan's fourth step,
   "Invite your partner", was to tick on `members.length > 1`. Reading
   `InviteService.Create` first showed an emailed invite writes only to the
   `invites` table, while `GET /household/members` reads `memberships` joined
   to `users` — so a pending invite is not a row there, and no endpoint
   exposes one. The step could therefore only tick once the partner
   *accepted*, leaving an owner who had just sent an invite looking at an
   unticked "Invite your partner" whose link leads to a Settings page showing
   no trace of it. The product owner chose to drop the step rather than ship
   that, on the precedent of Budget spec decision 1 (the dormant "Roll
   unspent into savings" toggle was cut, not shipped inert). Criteria 2, 6
   and 7 below read "of 3" for that reason. Exposing pending invites is
   filed as follow-up work.
2. **`PlaceholderPage.tsx` was not deleted.** The plan said Overview was its
   last user. It is not: `/money/$` still renders it, because Goals and Bills
   have no page yet. Deleting it would have broken that route.

| # | Criterion | Result |
|---|---|---|
| 1 | A fresh household lands on `/` and sees a real page, not "Arriving in slice 5." | PASS — heading "Overview", net worth S$0.00, "No budget set yet" |
| 2 | Checklist reads **1 of 3 done** with "Create your household" ticked | PASS — screenshot `01` |
| 3 | The budget step names the *current* month, read at render time | PASS — "Set a budget for August" on 1 August local, with the API container still in July (see criterion 12). A hardcoded or UTC-derived label would have read "July" |
| 4 | "+ Add" offers only what exists; Transaction disabled before any account, with its reason beside it | PASS — Transaction `disabled`, "Add an account first" shown, Account enabled. No Bill, Savings goal, Calendar event or Marriage retro entry present. Screenshot `02` |
| 5 | Adding an account from "+ Add" advances the checklist and the net worth card without a reload | PASS — S$2,500.00 account created; checklist went to **2 of 3** with "✓ Add an account"; net worth card showed S$2,500.00. Modal reached genuine `:modal` state (`dialog.matches(':modal')` true), not the jsdom stub |
| 6 | Transaction becomes enabled once an account exists, and its reason disappears | PASS |
| 7 | A transaction created from "+ Add" reaches the ledger — the menu's mutation must invalidate, not merely close | PASS — S$42.50 "Groceries from Overview" appears at `/money/transactions`; net worth moved S$2,500.00 → S$2,457.50, exactly the expense |
| 8 | The checklist's "Set up" link reaches `/money/budget`; setting a budget completes the list, which then disappears | PASS — link `href="/money/budget"`; after saving the family-of-four template the checklist was **gone** and the budget card read "1% used · S$42.50 of S$3,790.00". Screenshot `03` |
| 9 | A limited member **with** money: no figure, no budget card, no checklist, no "+ Add", nothing rendered as an error | **PASS after the defect found here was fixed.** The four absence clauses all held on the first walk — and the page was empty apart from its heading. Fixed by rendering a third shape: a "Money" panel reading "Amounts are hidden for your account. The accounts shared with you are in Finances." with a link there. Re-verified live: panel present, no net worth card, no budget card, no checklist, no "+ Add", no `role="alert"`. Screenshot `04` |
| 10 | A limited member **without** money sees the single no-access panel and nothing else | PASS — `GET /accounts` answered 403; page reads "You don't have access to Money in this household." with no error state. Screenshot `05` |
| 11 | The established household shows no checklist, both cards populated, and its net worth **equals** the figure on `/money` | PASS — seeded owner Andreas: Overview −S$5,860.00, `/money` −S$5,860.00. Two cards disagreeing about one number is the defect this criterion exists to catch; they agree |
| 12 | Overview and Budget agree about which month "this month" is | PASS, and this is the criterion the environment made interesting. Host local time was 1 August; the API container runs UTC, where it was still 31 July. `currentMonth()` reads the local calendar, so Overview asked `GET /budgets/2026-08` and `/money/budget` rendered "August 2026" — the two screens agreed because Task 2 made them share one function. `GET /budgets/{month}` takes its month from the URL, so no server-side "now" is involved and there is no mismatch to reconcile |
| 13 | No console errors across the walk | PASS — no errors or exceptions captured |
| 14 | Screenshots record distinct states | PASS — five screenshots, five distinct SHA-256 hashes. Two byte-identical before/after images are the failure `docs/LEARNING.md` already records from the finance-fixes branch; hashes were compared rather than eyeballed |

## Found after the walk, by review

**The fixed defect had a sibling one JSX block away, and the walk could not
have caught it.** Gating the new limited-member panel on `accounts.isSuccess`
was deliberate. The setup checklist directly beneath it had no equivalent
gate, so while `/accounts` and `/budgets/{month}` were in flight both
`hasAccount` and `hasBudget` read `false` — indistinguishable from a household
that has neither — and an **established** household saw "Finish setting up ·
1 of 3 done" on every cold load of `/` until its figures arrived, telling it
to create the account and budget it already owned.

Every step of the walk above navigated and then waited, so it never observed
the window. Reproduced by injecting a 1.5s delay into `fetch` for those two
URLs and re-mounting the page: before the fix the checklist appeared and then
vanished; after it, a `MutationObserver` watching the whole body across the
delay recorded **zero** appearances, with the final render showing both cards
and no checklist.

The obvious test for it is also a trap, and is recorded in `docs/LEARNING.md`
for that reason: asserting on the *first* render passes vacuously, because
`me` has not resolved then either and nothing owner-only renders at all. The
test that pins it holds open the window after `me` lands and before the money
queries do. Both new gates are pinned — `budget.data` by that test,
`accounts.isSuccess` by the type checker, which reports
`TS18048: 'accounts.data' is possibly 'undefined'` the moment it is removed.

**One untested link closed at the same time.** The checklist's "Add an
account" step points at `/money`, and nothing covered it: the suite's href
assertion checked the budget *card's* link, and the walk reached the account
form through "+ Add" rather than through the step. Both steps' hrefs are now
asserted, mutation-checked by pointing the account step at `/money/budget`.

## One flaky gate run, unexplained

`make test` failed once on `internal/adapter/postgres` after the sibling fix
above — a fix that touched only frontend files, so it cannot be causal. The
package did not fail again in four subsequent runs (three forced
`go test -count=1` on the package, one more full `make test`), and the final
gate is green: 10 Go packages, 293 frontend tests, lint clean.

Two things are worth knowing rather than pretending it did not happen. The
failing run took 104s against a ~90s baseline, and the dev stack was up on
Docker Desktop throughout the walk while the Go suite runs against colima —
two engines competing for the same machine, which is the environment
`docs/LEARNING.md` already has an entry about, in a new form. This package
also contains the concurrency test that same document records as only being
meaningful with a warmed pool and a start barrier, which is the shape most
likely to flake under load. **Not diagnosed, and not claimed as diagnosed** —
the detail was lost because the run's output was filtered too narrowly to
capture the `--- FAIL` line. If it recurs, capture the whole package output
before rerunning; a rerun that passes destroys the only evidence.

## Observations outside M2's scope

**The ledger header disagrees with the calendar at a month boundary.** With
the host on 1 August and the API container on UTC 31 July, `/money/transactions`
headed itself "0 in July 2026" while holding an August-dated transaction. That
count comes from the server's own clock, not from anything M2 touched, and it
is the same UTC-versus-local class `docs/LEARNING.md` already carries three
instances of — this is a fourth, on the server side, and it is genuinely
user-visible: a Singapore household on the first of the month sees last
month's name over this month's ledger. Recorded, not fixed: it belongs to
Transactions, and fixing it means deciding whose calendar the server should
use, which is a product decision.

**One guard is correct but not test-pinned.** The limited-member panel is
gated on `accounts.isSuccess && !accounts.data.summary` rather than on the
absence of `summary` alone, because `summary` is equally undefined while the
request is in flight — the looser condition would flash the panel at an owner
on every visit. Mutating the gate to the looser form leaves all tests green.
A test for it would have to assert a transient loading state, which is
exactly the shape `docs/LEARNING.md` warns produced a worthless guard test
before. The gate stays; its lack of cover is recorded here rather than
papered over.

## Mutations checked

Every new test was broken on purpose and watched go red:

| Test | Mutation | Observed failure |
|---|---|---|
| `currentMonth` reads the local calendar | `new Date().toISOString().slice(0, 7)` | `expected '2026-07' to be '2026-08'` |
| never asks for a budget for a limited member | `enabled: true` | `expected true to be false` |
| drops the checklist once finished | delete `if (done === steps.length) return null;` | `expected <h2 …></h2> to be null` |
| no Transaction before an account exists | `canAddTransaction = true` | test red |
| does not fetch a modal's data until opened | mount `TransactionModal` unconditionally | `expected true to be false` |
| explains the missing figures to a limited member | disable the panel's render | `Unable to find an element with the text: /amounts are hidden/i` |

The plan's own designated mutation for the limited-member budget card —
removing the `isOwner` render guard — **did not go red**, and that is worth
recording. Two guards defend that behaviour (`enabled: isOwner` on the hook
and `isOwner &&` on the render), so removing either one alone changes
nothing observable. The test was retargeted at what actually does the work:
it now asserts, through the stub's own `capture` hook, that the budget
endpoint is never requested at all. That version dies when the `enabled`
gate goes.
