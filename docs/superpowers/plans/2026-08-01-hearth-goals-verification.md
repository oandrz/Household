# Goals — verification walkthrough

Run 2026-08-01 (host clock 18:41–19:20 +08; the API container is UTC, where it
was 10:41–11:20 on the **same** calendar date — the last two walks each had to
reconcile a month or day skew and this one does not: UTC would not have crossed
midnight until 08:00 +08 the following morning, so "this month" reads August
2026 on both sides throughout). Driven in a real Chromium at 1440×1000 through
Playwright, against `http://localhost:5173`, from a wiped database:

```
make down && docker volume rm hearth_hearth-pgdata && make up && make seed
```

Criteria are Task 18 of `.superpowers/sdd/2026-08-01-hearth-goals/task-18-brief.md`,
verbatim, walked in order.

**Result: 15 of 15 pass — after one real defect the walk itself found at
criterion 12, fixed, mutation-checked and re-walked live mid-walk
(`82453ff`).**

The defect is the reason this walk exists. Goals shipped archive and restore
end to end — column, repository, service, `POST /goals/{id}/archive`, a
`useGoals.archiveGoal` mutation with its own passing test, a "Show archived"
toggle, and a Restore button on every archived card — and **no screen ever
called `archiveGoal`**, so no household could ever put a goal behind the view
Restore exists to bring it back from. `FEATURE_TRACKER.md` already carried the
row at ✅. Criterion 12 said "Archive Emergency fund" and there was nothing to
click. `docs/LEARNING.md` pattern 15 carries it in full.

**Both Docker engines were checked before anything else**, per
`docs/HANDOVER.md` §2: colima held the live stack, and the Docker Desktop
stack's five `hearth-*` containers were all `Exited` and stayed that way for
the whole walk. No phantom port was chased this time.

---

## Three departures from the script, decided before the walk

1. **Criterion 8's "use the seeded transactions' own figures" has no seeded
   transactions to use.** `make seed` on a wiped volume creates the household,
   its members, spaces and notification preferences — and **zero** accounts,
   categories and transactions (`select count(*) from transactions` → 0).
   The criterion was met by creating the state through the product first, then
   reading the figures off the Transactions page exactly as the criterion
   says: one account (**OCBC Joint**, cash, S$10,000.00 as of 2026-06-01) and
   two July expenses (**Groceries S$420.00** on 2026-07-10, **Dining out
   S$130.00** on 2026-07-15). The Transactions page, filtered to July 2026,
   then read "2 in July 2026 · Spent this month S$550.00", and those are the
   figures the July budget was built against.
2. **Criterion 13's "the no-rate state the accounts walk used" is two states,
   and this walk took the first.** That walk reached no-rate twice: its
   criterion 9 held a **USD** account against an SGD primary ("no exchange
   rate for USD"), and its criterion 11 switched the household's primary to
   **EUR**, at which point nothing had a rate. The EUR route was rejected
   here on the evidence: with primary EUR *every* goal is excluded, both
   totals go to €0.00, and "a count rather than a **silently short** total"
   stops being a distinguishable claim — there is no surviving total for the
   short one to be short against. It would also have had to be undone before
   criterion 14, whose picker needs `data.currency === "SGD"` and at least one
   eligible SGD goal, and a failed switch-back would have broken that
   criterion with entirely the wrong copy. A **USD goal against the SGD
   primary** leaves the SGD totals meaningful, produces `excludedNoRate: 1`
   with a real discrepancy on screen to explain, needs no undo, and makes
   criterion 14 stronger by proving the picker lists *every* ineligible goal
   rather than only the one the criterion names.
3. **Criterion 15's limited member was granted `money` through Settings, not
   through `adminctl`.** The brief offers
   `adminctl create-invite --capabilities=money` as the shortcut; the
   criterion's own words are "a limited member granted `money` **through
   Settings**". Jamie was invited with `--role=limited` and **no**
   capabilities, accepted the invite in the browser, and Andreas then flipped
   the "Jamie Money access" toggle in Settings — after which the member row's
   own description changed from "Kid · no only" to "Kid · money only", which
   is the grant landing. (That first string is a copy defect, recorded under
   "Observations outside this walk's scope" below.)

---

## The walk's own arithmetic (`docs/LEARNING.md` pattern 13)

Written down **before** clicking, and checked against the DOM at each step.
Criteria 6–10 share one prepared month (July 2026) and one prepared goal
(Bali family trip), so every figure below states what the walk's own earlier
steps had already moved.

**Bali family trip — contributed, and the ring that renders it.** Target
S$4,000.00 (400000 minor) throughout.

| after | contributed | ring | why |
|---|---|---|---|
| c3 create | 260000 | **65%** | starting balance 2,600.00; `(260000·100 + 200000) / 400000 = 65.5 → 65` |
| c6 add 500.00 | 310000 | **78%** | `(310000·100 + 200000) / 400000 = 78.0` (77.5% rounded half-up) |
| c7 delete it | 260000 | **65%** | back to c3 exactly |
| c9 rollover +650.00 | 325000 | **81%** | `(325000·100 + 200000) / 400000 = 81.75 → 81` (81.25% rounded) |
| c10 delete rollover | 260000 | **65%** | |
| c10 move again | 325000 | **81%** | |
| c14 delete rollover | 260000 | **65%** | **the walk's final state** |

**The header subtitle, which four criteria assert after an earlier one moved
it.** `datedCount` counts unarchived, dated, unachieved goals.

| after | subtitle | note |
|---|---|---|
| c3 | `1 of 1 on track` | Bali only |
| c4 | `1 of 1 on track · 1 with no date` | **what criterion 4 asserts, and it was true when asserted** |
| c5 | `1 of 2 on track · 1 with no date` | c5's own Behind goal moved the denominator — c4's figure is not re-asserted after this point |
| c11 quick add | `1 of 2 on track · 2 with no date` | Home repairs, dateless |
| c12 archive | `1 of 2 on track · 1 with no date` | Emergency fund out of every count |
| c12 restore | `1 of 2 on track · 2 with no date` | whole again |
| c13 IDR goal | `1 of 2 on track · 3 with no date` | |
| c13 USD goal | `1 of 2 on track · 4 with no date` | **final** |

**Planned monthly total** (converted to the primary currency, live goals only).

| after | total | arithmetic |
|---|---|---|
| c3 | S$350.00 | 350 |
| c4 | S$550.00 | 350 + 200 |
| c5 | S$600.00 | 350 + 200 + 50 |
| c11 | S$625.00 | + 25 (Home repairs) |
| c12 archive | S$425.00 | − 200 (Emergency fund) |
| c12 restore | S$625.00 | + 200 |
| c13 IDR | S$725.00 | + Rp 1,241,000 = 124,100,000 IDR minor ÷ 12,410 = **10,000 SGD minor = S$100.00, exact** |
| c13 USD | S$725.00 | **unchanged** — the USD goal is excluded, not silently added |
| c15's control goal | S$735.00 | + S$10.00, see "the state the walk ends in" |
| that goal archived | S$725.00 | **final** |

**Actual this month** — the figure criterion 6 exists to police.

| after | total | arithmetic |
|---|---|---|
| c3–c5 | **S$0.00** | three starting balances totalling S$3,800.00, and none of them counted |
| c6 | S$500.00 | the manual contribution, alone |
| c7 | S$0.00 | |
| c9 / c10 | S$650.00 | the rollover, dated today |
| c13 | S$700.00 | + Rp 620,500 = 62,050,000 IDR minor ÷ 12,410 = **5,000 SGD minor = S$50.00, exact** |
| c14 | S$50.00 | rollover deleted; **final** |

Both IDR figures were chosen to divide exactly by the static rate's 12,410, so
no rounding enters the conversion and a discrepancy of one cent would have
been a real finding rather than a rounding artefact.

---

## Criterion by criterion

| # | Criterion | Result |
|---|---|---|
| 1 | MONEY sidebar shows Finances, Transactions, Budget, **Goals**; Goals colours only on its own route | **PASS** — read as numbers, not eyeballed. On `/money/goals` the Goals link computes `rgb(26, 107, 82)` while Finances, Transactions and Budget are all `rgb(28, 27, 24)`; on `/` and on `/money/budget` the Goals link is `rgb(28, 27, 24)` and the active one is the route's own. `/money` being a prefix of `/money/goals` is the trap here — Finances stays uncoloured on the Goals route, so the match is not a `startsWith` |
| 2 | The empty state: one action, no templates, no automation copy | **PASS** — `goals-empty-state` present, exactly one `goals-create-first` button, `goals-new` absent (the empty state owns the only entry). `document.body.innerText` lowercased contains none of `auto-saved`, `auto`, `next transfer`, `transfer`, `automatic`, `rolls into`, `1st of each month`, `template`, `starter`, `suggested`. Screenshot `01` |
| 3 | "Bali family trip", 4,000.00 SGD by Dec 2026, starting 2,600.00, planned 350.00; live suggestion; card reads 65% / On track | **PASS** — four suggestion readings captured, all distinct, each matching `RequiredMonthlyMinor` by hand: target typed, no date → **no panel at all**; +Dec 2026 → `~S$800.00/mo` (400000 ÷ 5); target → 5,000.00 → `~S$1,000.00/mo`; back to 4,000.00, date → Jun 2027 → `~S$363.64/mo` (⌈400000 ÷ 11⌉ = 36364); back to Dec 2026, starting 2,600.00 → `~S$280.00/mo` (⌈140000 ÷ 5⌉). Card: `65%`, `S$2,600.00 of S$4,000.00 · by Dec 2026`, `S$350.00/mo`, pill **On track**. **The arithmetic:** Aug → Dec 2026 inclusive is 5 months; remaining 400000 − 260000 = 140000; required ⌈140000/5⌉ = 28000 = S$280.00 ≤ planned S$350.00 → on track. Screenshots `02`, `03` |
| 4 | "Emergency fund" with no target date: progress, no pill, named in the header count | **PASS** — `12%` (⌊(120000·100 + 500000)/1000000⌋ = 12), no `goal-card-status` element at all on that card, no `· by …` clause, subtitle `1 of 1 on track · 1 with no date`. Screenshot `04` |
| 5 | A goal whose planned monthly is far too small reads **Behind** and states what it needs | **PASS** — "New car", S$22,000.00 by Jun 2027, planned S$50.00: pill **Behind**, and beneath it `Needs S$2,000.00/mo to catch up`. **The arithmetic:** Aug 2026 → Jun 2027 inclusive is 11 months; remaining 2,200,000 minor ÷ 11 = exactly 200,000 = S$2,000.00 > S$50.00. Subtitle moved to `1 of 2 on track · 1 with no date` — see the ledger above. Screenshot `05` |
| 6 | A 500.00 contribution moves the ring, the "of" figure and **actual** by exactly 500.00, leaves **planned** still, and the starting balance is not in actual | **PASS, and this is the criterion with teeth.** Ring 65% → **78%**; `S$2,600.00 of S$4,000.00` → `S$3,100.00 of S$4,000.00`; actual `S$0.00` → **`S$500.00`**; planned `S$600.00` → `S$600.00`, unmoved. The defect this exists to catch would have read **S$4,300.00** — the two starting balances (2,600.00 + 1,200.00) plus the contribution, all dated today. It read 500.00. The divergence line said `S$100.00 short of plan this month.` = 600 − 500. Screenshot `06` |
| 7 | Delete it from the panel, confirming in-page: every figure returns to criterion 3's | **PASS** — the confirm is in-page (`Delete this contribution? This can't be undone.` with Cancel / Yes, delete), and `window.confirm`/`alert`/`prompt` were **hooked before the click and recorded zero calls**. After: `65%`, `S$2,600.00 of S$4,000.00`, actual `S$0.00`, planned `S$600.00`, `Nothing logged yet this month.` — criterion 3's values exactly. Screenshot `07` |
| 8 | A budget for last month with real unspent money; the card offers "S$X unspent in <month> · Move it into a goal" | **PASS** — July 2026 budget: Groceries cap S$800.00, Dining out cap S$400.00. Budgeted **S$1,200.00**, Spent **S$550.00** (420 + 130, the figures read off the Transactions page first), Remaining **S$650.00**. The card reads `S$650.00 unspent in July · Move it into a goal`, with the CTA enabled and no disabled-reason. **X = S$650.00** for every criterion below. No `budget-rollover-excluded-note`, correctly — every July expense is in the primary currency. Screenshots `08`, `09` |
| 9 | Move it into Bali; the panel shows the rollover row; the Budget card names the destination with no button; a second tab is refused with no second contribution | **PASS** — Bali `S$2,600.00` → **`S$3,250.00`** (+650.00 exactly), ring 65% → **81%**; actual `S$0.00` → **`S$650.00`**, divergence `S$50.00 more than planned this month.` = 650 − 600. The panel shows exactly one row labelled **`From July's unspent budget`**, `SGD 650.00`. The Budget card became `S$650.00 moved into Bali family trip.` with **no** CTA and **no** confirm button. The second tab (same session, a real second tab opened before the first confirm and its picker left pre-selected) then clicked its own Confirm and was refused: `That month has already been rolled over.` in a `role="alert"`, while the card behind it had already swapped to the destination sentence — the survives-the-branch-swap behaviour `BudgetRolloverCard.tsx`'s own comment describes, seen for real. **No second contribution:** `select source, source_budget_month … from goal_contributions` returned exactly one `budget_rollover` row. Screenshots `10`, `11` |
| 10 | Delete the rollover from the goal, return to Budget: the button is back; move again; exactly one rollover row | **PASS**, the round trip done in the browser this time. Deleting it restored Bali to `65% / S$2,600.00` and actual to `S$0.00`, and `/money/budget` on July showed `S$650.00 unspent in July` with the CTA back — the delete had cleared `rolled_over_at` and `rollover_goal_id`. Moving again succeeded (`S$650.00 moved into Bali family trip.`), and the panel again held **exactly one** `From July's unspent budget` row. Screenshot `12` |
| 11 | Overview shows "Goals on track — N of M" naming the next dated goal; "+ Add → Savings goal" opens the modal and saves | **PASS** — the card read `Goals on track` / **`1 of 2`** / `next: Bali family trip · Dec 2026` / `1 with no date`. **The counts:** dated, unarchived, unachieved = Bali (on track) and New car (behind) → 1 of 2; next = the earliest `target_month`, Dec 2026 before Jun 2027. "+ Add" offered Transaction, Account, **Savings goal**; Savings goal opened a genuine `:modal` "Goal details" dialog, and saving "Home repairs" (S$1,000.00, no date, planned S$25.00) moved the card to `2 with no date` **without a reload**. Screenshots `13`, `14` |
| 12 | Archive "Emergency fund"; it leaves the list and every count; "Show archived" brings it back beside the live goals with Restore, counts unchanged, contributions intact; Restore returns it whole | **FAIL on the first walk — no Archive control existed anywhere. PASS after the fix (`82453ff`), re-walked live.** See "The defect" below. After the fix, **archived by pressing Enter on the Archive button, not by clicking it** (the check a unit test cannot make — see the fix): Emergency fund left the list (4 cards → 3), the subtitle went `1 of 2 on track · 2 with no date` → **`1 of 2 on track · 1 with no date`**, planned `S$625.00` → **`S$425.00`** (− 200), actual stayed **`S$650.00`** (its only row is a `starting_balance`, already excluded, and `MonthContributionTotals` joins on `archived_at IS NULL` so nothing else dropped), divergence moved `S$25.00 more` → **`S$225.00 more than planned`** = 650 − 425. **And no edit modal opened**, which is what the Enter press was for. "Show archived" then rendered **four** cards — the three live ones *and* Emergency fund beside them, marked `(archived)`, offering `Restore Emergency fund` and **not** Archive, still reading `12% · S$1,200.00 of S$10,000.00` (its contributions survived) — with every count and both totals **unchanged by the toggle** (`1 of 2 on track · 1 with no date`, S$425.00, S$650.00; "unchanged" here means unchanged by the toggle, not by the archive, which moved them one step earlier). Restore returned it whole: marker gone, Add contribution and Archive back, `12% · S$1,200.00`, subtitle `2 with no date`, planned `S$625.00`. Screenshots `15`, `16`, `17` |
| 13 | An IDR goal against an SGD primary: its card in IDR, both totals converting it at the live rate; then the no-rate state, with a count rather than a silently short total | **PASS** — "Jakarta trip", Rp 12,410,000 target, planned Rp 1,241,000, renders **in its own currency**: `Rp 620,500 of Rp 12,410,000`, `Rp 1,241,000/mo`, and the legend entry stays `Jakarta trip · Rp 1,241,000` while every other legend entry is in S$. Planned total S$625.00 → **S$725.00** and actual S$650.00 → **S$700.00**, both exact at the static 1 SGD = 12,410 IDR rate (see the arithmetic table). Then "US college fund" in **USD**, which the static provider has no rate for: the card renders `$0.00 of $10,000.00 · $200.00/mo`, the legend lists it, **the planned total stays S$725.00 — not S$925.00** — and the card states `1 goal is not counted: no exchange rate.` The discrepancy is on screen *and* named: six goals in the legend, five in the total, one counted as excluded. Screenshots `18`, `19` |
| 14 | Delete criterion 10's rollover first; then the picker lists the IDR goal as unavailable with its reason, only primary-currency goals are choosable, and **Cancel** | **PASS** — the delete first, exactly as the criterion's own note requires: criterion 10 left July stamped, so without it there would have been no CTA to open a picker with. After deleting, Bali returned to `65% / S$2,600.00`, actual fell to `S$50.00` (the IDR contribution alone), and July's card offered `S$650.00 unspent in July` again. The picker then held seven options: `Choose a goal…` plus **four selectable SGD goals** (New car, Bali family trip, Emergency fund, Home repairs) and **two `disabled` ones with their reason in the option text** — `Jakarta trip (IDR — only SGD goals can receive a rollover)` and `US college fund (USD — only SGD goals can receive a rollover)`. Neither is silently dropped and neither can be chosen. **Cancel** closed the picker with the offer still standing, and the database confirms the month is unrolled: `select month, rolled_over_at, rollover_goal_id from budgets` → `2026-07-01 | NULL | NULL`. Screenshot `20` |
| 15 | A limited member with `money`: the owner-only explanation on `/money/goals`, Overview still renders content; then two `curl` groups on **different** sessions | **PASS**, all five checks. **In the browser as Jamie:** `/money/goals` renders `goals-owner-only` — "Goals / Owner only / Goals is visible to the household owner. Ask them if you'd like to see where things stand." — with **no** `goals-load-error`, no `role="alert"`, and 108 characters of real content rather than a blank page. `/` renders the limited-member Money panel ("Amounts are hidden for your account. The accounts shared with you are in Finances." with a link there), not an empty heading. **Two `curl` sessions, two cookie jars.** As Jamie (limited, holds `money`, not owner), **with a valid CSRF header supplied so CSRF cannot be the cause**: `GET /api/v1/goals` → **403** `FORBIDDEN` "Only an owner may do that."; `POST /api/v1/goals` → **403** `FORBIDDEN`, same message. As Andreas (owner), session cookie only, no CSRF header, valid JSON body and `Content-Type`: `POST /api/v1/goals` → **403** `CSRF_INVALID` "The CSRF token is missing or does not match." — and a **control** request with the identical body *plus* the header returned **201**, which is what proves the 403 was the CSRF guard and not the body. With CSRF present, `POST /api/v1/budgets/2026-13/rollover` → **400** `INVALID_MONTH` "That month could not be read. Use YYYY-MM." Screenshot `21` |

---

## The defect, and the fix

**Criterion 12 had nothing to click.** `/money/goals` offered, per live card,
"Add contribution" and nothing else; the edit modal offered Cancel and Save;
the page offered "+ New goal" and "Show archived". Grepping the whole
frontend for `archiveGoal` outside `useGoals.ts` and its own test returned
**nothing**. Every one of the seven mutations `useGoals` returns had a real
caller except that one.

So the household could see "Show archived", could see a Restore button on any
archived goal — and could never archive one. The half of the feature that
gets a goal *into* that state did not exist above the hook.

Nothing was wrong at any layer below. `00007_goals.sql` has the column,
`GoalRepo.SetArchived` keeps the first stamp idempotently,
`GoalService.SetArchived` exists, `POST /goals/{id}/archive` is mounted and
guarded, `useGoals.archiveGoal` posts to it and invalidates, and
`useGoals.test.ts` has a test that passes. `docs/SYSTEM_DESIGN.md` already
documents the route. The gap was one JSX block.

**Why it got through** — three separate mechanisms, in the commit and in
`docs/LEARNING.md` pattern 15:

- **No task owned the control.** Task 11's brief describes the archived view
  as "each marked '(archived)' with a Restore action"; Task 12's field list is
  name, target, currency, date, starting balance, planned monthly. Neither
  mentions Archive, so no task's tests were wrong — the work item never
  existed. This is the **second** plan gap on this branch; `d1c7248` wired
  `GoalModal` into `GoalsPage` for exactly the same reason.
- **The hook test proved the capability, not the reach.** A test that mounts
  `useGoals` and calls `archiveGoal` is a true statement about the hook and
  says nothing about whether any component ever does.
- **The tracker row was ticked from the backend up.** "Archive and restore a
  goal ✅" was written while archive had no UI.

**The fix** (`82453ff`): `GoalCard` renders **Archive** on a live card,
mirroring `AccountRow`'s own either/or — Archive on live, Restore on archived,
never both — with `aria-label={`Archive ${goal.name}`}`, `text-danger`, and no
`window.confirm`, for the reason `AccountsPanel.tsx` already states beside its
own: archiving is reversible from the archived view, and a native modal blocks
the tooling every browser walk here uses. `GoalsPage`'s per-card `restoringIds`
became `pendingIds` covering both directions and `GoalCard`'s `restorePending`
prop became `pending`, the name `AccountRow` already uses for the same thing.

**The trap inside the fix, and what caught it.** `GoalCard`'s root is
`role="button"` with **both** `onClick` and `onKeyDown`. A nested button must
stop both, because a real browser fires a separate `click` after the Enter
keydown has already bubbled — stopping only the click leaves Enter archiving
the goal *and* opening the edit modal behind the card that is disappearing.
`fireEvent.click` never presses a key, so no unit test in this repo can see
it; this is `docs/HANDOVER.md` §1's `TransactionFilters` lesson in a second
form. The re-walk therefore archived Emergency fund **by pressing Enter on a
focused Archive button** and asserted `document.querySelector('dialog[open]')`
was null afterwards.

**Two mutation checks, both seen red before green:**

| mutation | test that went red |
|---|---|
| `{onArchive && (` → `{false && onArchive && (` | *a live card offers Archive, which POSTs /goals/{id}/archive and refetches* — "Unable to find an accessible element with the role 'button' and name 'Archive Bali family trip'" |
| dropping the `!archived &&` gate | *an archived card offers Restore and no Archive* |

Both tests query `within(card)`, not the page — an unscoped `getByRole` would
still have passed with the button rendered on the wrong card.

`docs/SYSTEM_DESIGN.md` was deliberately **not** touched: the route it already
documents was always there, no table, port, guard or flow changed, and editing
it on reflex would have implied a structural change that did not happen.

### The sibling sweep, run rather than assumed

Pattern 15's own prescription — "for each mutation a hook returns, grep the
source (excluding the hook and its own tests) for a caller" — was then run
across **every** hook in the frontend, not just `useGoals`, because this repo
has five recorded cases of fixing an instance and not the class
(`docs/LEARNING.md` pattern 1).

Twenty-two exported hooks (`useAccounts`, `useCreateAccount`,
`useUpdateAccount`, `useSetAccountArchived`, `useBudget`, `useBudgetHistory`,
`useTransactions`, `useCategories`, `useCreateTransaction`,
`useUpdateTransaction`, `useDeleteTransaction`, `useHouseholdMembers`, and the
ten in `useAuth.ts`) all have real callers. The two hooks that return several
functions were then opened up member by member: `useGoals`' six write functions
all have callers (`archiveGoal` now among them), and of `useBudget`'s nine
returned members, eight do.

**The ninth is a second orphan, and it is worth naming precisely because it is
*not* the same defect.** `useBudget`'s `reload` — `async () => { await
query.refetch(); }` — has no caller anywhere and no test either; the only
matches outside its own file are unrelated comments. That is dead code, not a
dead end: no household loses a capability, because nothing on the Budget screen
needs a manual refetch (every write invalidates). `archiveGoal` was an orphaned
**capability** with an affordance built to escape from it; `reload` is an
orphaned **convenience**. Left in place rather than deleted, because removing
an exported hook member is a scope change this walk has no mandate for — but
recorded here so the next person to touch `useBudget.ts` knows it costs
nothing to drop.

So: the sweep ran across every money and auth hook, and `archiveGoal` was the
only orphan that a household could feel.

---

## The state the walk ends in

Stated exactly, because criterion 14 was chosen to leave it stateable.

- **July 2026 budget: unrolled.** `rolled_over_at` and `rollover_goal_id` are
  both `NULL`, and the page still offers `S$650.00 unspent in July · Move it
  into a goal`.
- **Six live goals**, one archived. Live: New car (0%, Behind, S$22,000.00 by
  Jun 2027), Bali family trip (65%, On track, S$2,600.00 of S$4,000.00 by Dec
  2026), Emergency fund (12%, no date), Home repairs (0%, no date), Jakarta
  trip (5%, Rp 620,500 of Rp 12,410,000), US college fund (0%, $0.00 of
  $10,000.00). Archived: **CSRF probe** — created by criterion 15's *control*
  request, which had to succeed for that criterion's 403 to mean anything.
  It was archived through the product afterwards (a second, unscripted use of
  the new Archive button), which is why the planned total went S$725.00 →
  S$735.00 → S$725.00 and why the final figures below match the ledger.
- **Header** `1 of 2 on track · 4 with no date`; **planned monthly total**
  `S$725.00`; **actual this month** `S$50.00`; **divergence** `S$675.00 short
  of plan this month.`; **exclusion note** `1 goal is not counted: no exchange
  rate.`
- **Three contributions** in the database: two `starting_balance`
  (S$2,600.00 Bali, S$1,200.00 Emergency fund) and one `manual`
  (62,050,000 IDR minor, Jakarta trip).

---

## Screenshots: 21 files, 20 distinct hashes, and one deliberate pair

`shasum -a 256` over
`docs/superpowers/plans/2026-08-01-hearth-goals-screenshots/` returns **20
distinct hashes across 21 files**: exactly one pair collides, because
**`11-bali-rollover-row.jpeg` and `12-rollover-round-trip-one-row.jpeg` are
byte-identical** (`3106fae041926fbb…`).

That is the exact signature `docs/HANDOVER.md` §2 warns about, and here it is
**the criterion's own claim rather than a failure**: criterion 10 asserts that
deleting the rollover and moving it again returns the goal to precisely
criterion 9's state, with exactly one rollover row. Two identical images are
what that looks like. The intermediate state — the one that proves the delete
really happened rather than the click doing nothing — is not carried by an
image at all but by two reads taken between them: the contributions panel
holding only `Starting balance` with Bali back at `65% / S$2,600.00`, and
`/money/budget` on July showing the CTA restored. Every other pair of files
differs.

---

## Observations outside this walk's scope

Recorded rather than fixed, because none is a Goals criterion and each would
be a scope change:

- **The contributions panel prints bare currency codes where every other
  money surface prints a symbol.** Rows read `SGD 2,600.00` and `IDR 620,500`
  while the card two inches away reads `S$2,600.00` and `Rp 620,500`. This is
  `formatMoney`'s documented fallback doing exactly what it says — the panel
  takes no `currencies` prop, and `GoalsPage.tsx`'s own comment calls that
  deliberate ("nothing here that must wait on a second query to settle"). It
  is still an inconsistency a household sees, in the one place it is reading
  its own ledger. A `symbol` prop threaded from the page's existing
  `symbolFor` would close it.
- **A limited member with no capabilities is described as "Kid · no only".**
  Settings renders the capability list as "<role> · <capabilities> only", and
  with an empty list the sentence loses its noun. The invite-acceptance screen
  has the same shape: "Joining as Kid — no access only." Both are
  pre-existing and neither touches Money; the fix is a distinct empty-list
  branch in whatever composes that phrase.
- **Console noise, so a real error would stand out.** Across the whole walk
  the only console errors were network status lines: `401` on
  `/api/v1/auth/me` and friends whenever nobody was signed in (before sign-in
  and immediately after each of four sign-outs), the two **deliberate** `403`s
  from Jamie's `/accounts` and `/goals`, and one `404` for `/favicon.ico`.
  **No JavaScript exception, no React error, no failed render** at any point.

---

## The gate

`make lint && make test`, whole output captured to a file rather than
filtered — the interim-Overview walk lost its only evidence of a flaky package
by grepping too narrowly, and this walk's dev stack was up on the same colima
engine testcontainers uses.

```
./scripts/arch-lint.sh
architecture lint passed
cd web && npx tsc --noEmit
cd web && npm run lint
cd api && go vet ./...

cd api && go test ./... -count=1 -timeout=5m
ok  	github.com/andreasoentoro/hearth/api/cmd/adminctl                    0.461s
ok  	github.com/andreasoentoro/hearth/api/cmd/api                         1.989s
?   	github.com/andreasoentoro/hearth/api/internal/adapter/clock          [no test files]
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/crypto         1.351s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/fx             1.354s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/http          70.314s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/mail           2.320s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/postgres      99.477s
?   	github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen  [no test files]
ok  	github.com/andreasoentoro/hearth/api/internal/config                 3.132s
ok  	github.com/andreasoentoro/hearth/api/internal/domain                 3.499s
?   	github.com/andreasoentoro/hearth/api/internal/testsupport            [no test files]
ok  	github.com/andreasoentoro/hearth/api/internal/usecase                5.599s

cd web && npx vitest run
 Test Files  37 passed (37)
      Tests  384 passed (384)
```

Ten Go packages `ok`, **zero** `FAIL` or `panic:` lines anywhere in the
captured output, 384 frontend tests across 37 files, arch lint / `tsc` /
eslint / `go vet` all clean, overall exit **0**. The 384 includes the two
tests this walk added. No flake this run, unlike the interim-Overview walk's
one unexplained `postgres` failure — and `postgres` took 99s here with the dev
stack sharing the engine, which is the load that made that one plausible.
