# Hearth — feature tracker

Every feature in `design/Household Dashboard.dc.html`, and whether it exists
yet — plus a handful of rows the design does not draw, where a design feature
needed them to exist (see "Where things stand" below).

**Legend**

| | |
|---|---|
| ✅ | Built and verified |
| 🟡 | Partly built — the gap is named |
| ⬜ | Not started |
| 🚫 | Out of scope — marked "· not built" by the design itself, or descoped by the product owner; the row says which |

**Where things stand:** 95 of 120 features built or partly built.

> **Recounted 2026-09-02**, when the four unbuilt platform-administration
> features were given rows (section 9). The count of *built* work does not
> move — 77 ✅ and 17 🟡, unchanged — but the denominator does, because four
> features that existed only as prose are now on the map: 116 → 120, ⬜ 20 →
> 24. A number going up because the map got honest is the healthy direction.
>
> **The summary table itself was not changed with that recount** — it still
> read Platform administration 4/1/0/0 and Total ⬜ 20, summing to 116 under a
> headline that said 120. Corrected 2026-09-02 (same day) while answering
> "what's next": the row now reads 4/1/4/0 and the total ⬜ 24, from a fresh
> count of the symbols in §9, not a delta.
>
> **The audit screen, 2026-09-02, same day — built, walked, then descoped.**
> Section 9's `/admin/audit` row moves ⬜ → 🚫. The screen was built end to
> end on branch `admin-audit-screen`, every suite green, and walked in
> Chrome by the product owner and the agent together; the owner then
> decided the feature is not needed and asked for it to be removed rather
> than merged. The code is gone; the log stays readable through `psql`.
> 🚫 rather than a deleted row, because a feature that was specified,
> prioritised and then cut is a decision the map should still show — and
> the first 🚫 that is the owner's call rather than the design's, hence the
> legend's wording above. Recounting §9 gives 4/1/3/1; totals
> **77/17/23/3 = 120**, Built + Partial **94**, the same headline as before
> the day started.
>
> **Households and metrics, 2026-09-02, later the same day — built and
> walked.** Section 9's "Households and metrics" row moved ⬜ → ✅: an
> operator's household list with four counters and an explicit search over
> households *and* members, and a read-only drill-in behind it (members,
> channel, pending invites, the household's sign-in lockout). It has its own
> spec, `docs/superpowers/specs/2026-09-02-hearth-admin-households-design.md`,
> which expands the admin-surface spec's §6 and wins where the two differ. The
> one schema change is `sessions.last_seen_at`, stamped by `requireSession` at
> most once an hour, so "active in the last 7 days" means *used* rather than
> *signed in*. **The row's browser walk ran the same day, Task 11 of the same
> plan** (`docs/superpowers/plans/2026-09-02-hearth-admin-households-verification.md`):
> 15 of 15 criteria pass, with two caveats — criterion 7's caveat was a real
> defect (the "Nothing matches" message's own Clear button restored the list
> but left the search box showing the stale query), fixed in the same commit
> that recorded the walk; criterion 12 was confirmed against the drill-in's
> own lockout callout through the API, with the browser's admin session kept
> alive throughout, rather than against the sign-in screen's own local error
> state. This row now carries the "walked in a browser" bar every other ✅ in
> this file carries.
>
> **The recount was a count, not an addition.** Section 9's nine rows were
> counted by the first status symbol in each row's own cell —
> `awk '/^## 9/,/^## Suggested/'` over this file, one `grep -c` per symbol —
> giving **5 ✅ / 1 🟡 / 2 ⬜ / 1 🚫**, which is what the summary table's
> Platform administration row now reads. The Total row was then re-summed from
> the nine section rows as they stand rather than adjusted by delta:
> **78/17/22/3 = 120**, Built + Partial **95**. Only section 9 changed, so no
> other section was recounted. Reworded in the same section, without moving a
> symbol: the flags screen's first named gap used to rest on "there is no
> household list anywhere in the product". There is one now — what is missing
> is the control, not the list.

> **In production since 2026-08-15**, at <https://oink.mywire.org>. **No count
> below changes** — deployment is not a design feature, and this file's totals
> are a measure of the product against `design/Household Dashboard.dc.html`, not
> of the install. `docs/SYSTEM_DESIGN.md` §8 and `docs/HANDOVER.md` §1 carry the
> deployment's own state.
>
> **One caveat does change how some ✅ rows behave in production, without
> changing whether they are built.** Under
> [ADR 3](adr/0003-mail-stays-on-the-box.md) no mail leaves the box, so every
> feature whose flow runs through an email — sign-up, **inviting a member**,
> magic-link recovery, notification preferences — works exactly as built but
> reaches its recipient only if the owner opens Mailpit over an SSH tunnel and
> passes the link along by hand. Inviting a third person is therefore not
> self-service on the live install today. The rows stay ✅ because the code is
> built and verified; the constraint is the host's DNS, and the ADR's exit
> condition is the day someone who is not Andreas or Christine needs to receive
> an email.
>
> **Amended 2026-09-01, when Telegram sign-in was built.** Two of the four
> flows named above now have a second delivery channel that needs no DNS, no
> domain and no relay: **sign-up and magic-link recovery** can reach a stranger
> over a Telegram bot, once one is configured. **Inviting a member still
> cannot** — invites deliberately stay on email in this slice (see the ⬜ row
> in section 1), so "inviting a third person is not self-service" remains true
> word for word, and notification preferences still send nothing at all.
> The caveat above is narrowed, not lifted, and it is narrowed only where a bot
> is configured — the live install has none today. See
> [ADR 4](adr/0004-telegram-as-a-second-delivery-channel.md) and ADR 3's own
> amended exit condition.

**The notifications correction, 2026-08-16 — four rows were ✅ for something no
household can actually receive.** All four `Notifications — …` rows in Household
settings move ✅ → 🟡, each with its gap named. The preferences are real end to
end — column, repository, `GET`/`PATCH`, the Settings panel, tests — but
`usecase.Mailer` has exactly three methods (magic link, invite, sign-up), no
caller anywhere reads `notification_preferences` in order to send anything, and
nothing in this codebase runs on a clock: the only cron that exists is the box's
nightly backup. The design's own copy promises delivery rather than a switch —
"Bill due reminders (3 days before)". This is the same shape as the Goals
archive row (see `docs/LEARNING.md` pattern 15): every layer built, and the thing
the row is named after still impossible for a household to experience. Found
while answering "what's next" for Marriage, whose own retro reminder is one of
the four. **Nothing was built or unbuilt here — only the record corrected.**
Recounting Household settings by this file's own rule (the first symbol in each
row's own cell) gives 11/8/2 where it read 15/4/2, taking the totals from
60/13/28/2 = 103 to 56/17/28/2 = 103 — no row added or removed, four moved from
Built to Partial. "73 of 103 built or partly built" is unchanged, which is the
point: a 🟡 and a ✅ count the same there, and that is exactly how four rows
stayed wrong without moving any headline number.

**The Bills update, recorded before the ones below it — Money's fifth and
last feature.** Every number below is a fresh count of the ✅/🟡/⬜/🚫 symbols
in the tables — the first symbol in each row's own cell, whichever comes
first whether the cell is bare or carries prose after it — never the
previous totals adjusted in place; this file records that adjusting by delta
instead of recounting has produced wrong numbers before (see the Budget
update below). Four rows move ⬜ → ✅ in the Bills table: the due-soon/paid
timeline, autopay status, the subscriptions summary and Add bill (modal).
Two new rows the design's own mockup never draws — **Undo a payment** and
**Archive and restore a bill** — are added at ✅, the same shape Accounts'
and Goals' own no-mockup rows already take. One more new row lands in
Budget: **Unattributed row on Spending by person**, also ✅, the gap Bills'
Task 8 closed before Bills itself existed to reach it. One row moves in
Overview: **Next bill card** reaches ✅, reading `useBills` the same way
`/money/bills` itself does; the **"+ Add" quick-create menu**'s own cell is
rewritten in place (still 🟡) to say four of six entries are now live
(Transaction, Account, Savings goal, Bill) rather than three. The
**Navigation shell**'s "Placeholder pages for unbuilt areas" row keeps its
✅ but its prose is corrected: it had named `/money/$` as one of two routes
still using the placeholder component, but Bills' own route replaced that
splat outright (commit `946630e`) rather than joining beside it, and `/`
had already stopped using the placeholder back when the interim Overview
shipped — so the row now says, correctly, that none are left rather than
naming two that were already down to zero. Recounting by this file's own
rule takes the totals from 52/12/33/2 = 99 to 60/12/28/2 = 102 — three rows
added (two Bills, one Budget), five moved from Not started to Built (four
in Bills, one in Overview).

**The Goals update, recorded before the ones below it.** Slice 2's fourth
feature shipped: savings targets whose progress is a contributions ledger,
not an account balance; the New/Edit goal modal; the contributions panel;
the Monthly contributions card; and the Budget page's own manual rollover
into a goal. As with every update recorded here, every number is a fresh
count of the ✅/🟡/⬜/🚫 symbols in the tables below — the first symbol in
each row's own cell — never the previous totals adjusted in place. Four
rows move and two are added in Money: "Savings goals with progress and
funding source" moves ⬜ → 🟡, its one named gap being the design's
funding-source account (dropped deliberately, spec decision 6); "Monthly
contributions summary" and "New goal (modal)" both move ⬜ → ✅; two new
rows the design's own mockup never draws — "Goal contributions — add,
delete, list by source" and "Archive and restore a goal" — are added at ✅;
and Budget's "Roll unspent into savings" moves ⬜ → 🟡, since the manual
move now ships even though the design's automatic month-end toggle does
not. One more row moves in Overview: "Goals on track card" reaches ✅,
reading the real `X of Y on track` figure. Recounting by this file's own
rule takes the totals from 47/10/38/2 = 97 to 52/12/33/2 = 99 — two rows
added (both Money), five moved from Not started to Partial or Built.

**The interim Overview (M2) update.** `/` stopped being a placeholder. Section
4 gains two rows the design does not draw — a setup checklist and the
limited-member "amounts are hidden" panel, both ✅ — and three of its existing
rows move ⬜ → 🟡 with their gaps named: the net worth card, the budget card
and the "+ Add" menu. The remaining five Overview cards stay ⬜; they need
Bills, Goals, Marriage and Family. Recounting by this file's own rule takes
the totals from 45/7/41/2 = 95 to 47/10/38/2 = 97 — two rows added, three
moved from Not started to Partial.

**The Budget update, recorded before it.** That earlier update
did three things to the tables below, and every number here is a fresh count
of the ✅/🟡/⬜/🚫 symbols in the tables — the first symbol in each row's own
cell, counted whether that cell holds a bare symbol or a symbol with prose
crammed in after it — not the previous total adjusted in place: it corrects
"Budget history (modal)" from the ✅ the Task 15 commit that built it had
already marked in this file back to 🟡, since that row's own gap — Export
CSV — was never actually closed; it adds one new row, "Roll unspent into
savings" (⬜, deferred whole to Goals); and counting by that rule surfaced a
third thing, present before this task touched anything: the Transactions
table's **"Full ledger with filters"** row, whose cell reads "✅ — a
transaction dated…", counts as ✅ same as every other built row, but its
cell holds a dash-note rather than a clean `| ✅ |`. Before this update the
stated summary table had Money's Built at 13 and Not started at 15; a fresh
count of the actual tables (every ✅/🟡/⬜/🚫 symbol, whether its cell is bare
or has prose after it) gives Built 14 and Not started 14 for that same
pre-edit state — the stated numbers were wrong in both directions by
exactly one, landing on the same row total by two errors that happened to
cancel rather than one correct count. Recounting by that rule — the first
symbol in the cell, not "does the cell contain nothing but the symbol" — is
what this update fixes, alongside its own two changes. Money
now has three features fully built —
Accounts (a household records what it owns and owes by hand and sees a net
worth built from it), the Transactions ledger (logging, editing and deleting
expenses, income and transfers, with filters and the five screen states the
design and spec both call for), and Budget (create from scratch, from a
template, or by importing last month's; edit an existing month end to end;
and review the last six months' spend-vs-budget in the History modal, save
for its own Export CSV) — plus the recent-transactions strip on Finances,
deferred by the accounts spec for having no data and now built with the
ledger to read from. Nothing else in Money is started yet, and nothing in
Marriage or Family has been started. Overview is no longer among them: see
the M2 note above.
Five of the rows below have no mockup of their own — the provisioning
transaction behind self-serve sign-up, the currency list endpoint, and
`adminctl prune` — because the design's own "Create household" screen (the
`authScreen` state, and the sign-in footer's "No household yet? Create one")
is what makes each of them necessary, and none has anywhere else to live in
this tracker. Accounts adds three more design-less rows, for a different
reason each: archive and restore, which the design never draws anywhere in
Money (see the Finances table below); and two ⬜ rows — custom account types
and a warning before a primary-currency change strands every account — for
work the accounts spec named and deliberately deferred rather than built.
Transactions adds two more under **Categories**: the list and its
first-use seeding (needed before Budget existed, for the transaction modal's
own dropdown), and — added by the Budget update — renaming, creating and
archiving a category, which the design's spec folds into the Edit-budget
modal rather than a dedicated screen the dc.html mockup never drew. Goals
adds two more of its own, under **Goals** below: contributions — add,
delete, list by source — and archive/restore, neither of which the design's
mockup draws beyond the cards and the contributions summary themselves.

**The mobile-responsive update, recorded before it.** Every existing screen —
not a new one — now reflows down to a 320px phone, on the product owner's
explicit "same UI, layout and structure" constraint. One row is added to
Navigation shell, the section `AppShell`, `NavDrawer`, `MobileTopBar` and
`Sidebar` all live under: **Mobile-responsive layout**, at 🟡 rather than ✅.
The round's own width-matrix walk (Task 10) found one real overflow —
`BudgetModal`'s category rows, invisible to a document-level check because a
native `<dialog>` sits outside normal document flow — and fixed it in the
same task; it also found, and deliberately kept, five touch targets under the
44px floor and one structural gap, all named in the row's own cell rather
than glossed over — a 🟡 with no explanation is worse than a ⬜. As with every
update recorded here, the new total is a fresh
count of the ✅/🟡/⬜/🚫 symbols in the tables below — the first symbol in each
row's own cell — not the previous totals adjusted in place: recounting all
eight sections leaves every column unchanged except Navigation shell's
Partial, which moves from 0 to 1 for the new row (7/0/1/0 → 7/1/1/0, nine rows
now, not eight), taking the stated totals from 60/12/28/2 = 102 to
60/13/28/2 = 103 — one row added, no row moved between states.

**The retro open-action-count fix.** Overview's "Next retro card with
carried-over actions" moves ⬜ → ✅ (Task 15 had shipped it at 🟡 in its
own report, but this file's row had never been updated to reflect that --
it still read ⬜). The gap was real either way: the card read
`actionCount`, the retro's total, so a retro whose every action was
already ticked still claimed outstanding work on the home page.
`GET /retros` now carries `openActionCount` alongside `actionCount` on
every row -- a correlated `done_at IS NULL` subquery through
`RetroSummary`, `retroSummaryDTO` and the zod schema -- and the card reads
that instead; `RetroHistoryList`'s own history row is untouched, since the
spec's formulas table is explicit that its own "K actions" figure counts
the total, ticked or not. No other row changes. Recounting by this file's
own rule (the first symbol in each row's own cell) takes Overview from
4/3/3/0 to 5/3/2/0 and the totals from 58/17/26/2 = 103 to
59/17/25/2 = 103 — no row added or removed, one row moved from Not started
to Built.

This also caught the top-line "Where things stand" figure sitting wrong
before this edit touched anything: it read "73 of 103", but the table it
is supposed to summarise (Built + Partial) already gave 58 + 17 = 75, not
73 -- two rows' worth of drift the Bills and Goals updates left behind
without correcting the headline they were adjusting by delta rather than
recounting (the same failure mode this file names for itself twice above).
With this update's own +1 Built, the honest figure is 59 + 17 = **76**, not
the 74 a delta off the stale 73 would have given -- corrected directly
rather than compounding the error a third time.

**The Retros reconciliation, 2026-08-17 — Marriage and Overview brought into
one true state, after two tasks nudged rows piecemeal.** Tasks 11 and 15 each
ticked what they had just shipped without touching the rows a sibling task
had already shipped and left unticked, so the file was internally consistent
but not true: the single retro view and the start-retro modal sat at ⬜
though both shipped in Tasks 12 and 13, and two features the design's mockup
never draws — carrying an action forward, deleting a draft — had no row at
all. Fixed in one pass rather than another nudge. **Two rows move ⬜ → ✅**:
"Single retro view — went well, was hard, actions, notes" (the actions list
is this row's own name, so it carries no separate row) and "Start retro
(modal) with mood, money check-in and actions," the latter noting the
design's "45 min" duration is drawn and not built (spec decision 8). **Two
rows are added, the shape Accounts' archive/restore and Goals' contributions
rows already take**: "Carry an unfinished action into the next retro" (✅ —
`RetroModal`'s carry-over control posts a real `addAction` for every action
the server offers under "Still open from July") and "Delete a draft retro,"
which lands at 🟡 rather than the ✅ the round set out to write. The backend
is real end to end — the migration, `RetroRepository.DeleteDraft` (`WHERE
completed_at IS NULL`, mutation-checked in Task 5), `DELETE /retros/{month}`
and `useRetro.discardDraft` (with its own passing test) — but **no
component anywhere calls it**: `grep -rl discardDraft web/src/features/marriage/`
returns only `useRetro.ts` and its own test file. That is the same "built and
tested at every layer, reachable from none of them" shape `docs/LEARNING.md`
pattern 15 already carries twice, found here by running the pattern's own
mechanical check (grep every hook mutation for a caller outside the hook and
its own tests) against `useRetro.ts` before writing the row rather than
trusting the round's own plan, which asked for ✅. The same check on
`removeAction` — the port's `DELETE /retros/{month}/actions/{id}`, no mockup
ever draws it and no task's brief ever assigned it a caller either — turns
up the identical shape with no tracker row to correct, since none was ever
promised; recorded in `docs/LEARNING.md` rather than here. Overview's "Next
retro card with carried-over actions" was already ✅ and already correct — no
change. Recounting by this file's own rule (the first symbol in each row's
own cell, across all eight section tables, never the previous totals
adjusted by delta) leaves every area unchanged except Marriage, which moves
from 2/0/11/0 (13 rows) to 5/1/9/0 (15 rows) — **two** existing rows move
Not started → Built (the single retro view, the start-retro modal; Not
started drops from 11 to 9, exactly those two, not three), and **two** new
rows are added: one direct to Built (carry an unfinished action forward)
and one direct to Partial (delete a draft retro, still gapped at this
point) — the Partial column's own +1 is a new row landing there, not a
second state transition out of Not started. Totals move from 59/17/25/2 =
103 to **62/18/23/2 = 105**. The "Where things stand" headline (Built +
Partial) moves from 76 of 103 to **80 of 105**.

**Delete a draft retro, 2026-08-17 — the row this same reconciliation had
just named 🟡 moves to ✅.** The gap was real when written: `useRetro.ts`
exposed `discardDraft`, backed by a real migration, a mutation-checked
`RetroRepository.DeleteDraft` (`WHERE completed_at IS NULL`), `DELETE
/retros/{month}` and the hook's own passing test, but no component called
any of it — `docs/LEARNING.md` pattern 15's fourth instance in this one
feature. `RetroModal.tsx` now offers a "Discard draft" control, shown only
while `completedAt` is null (a finished retro offers nothing, since the
server refuses one and an offer that always fails is worse than no offer),
confirmed in the page rather than with `window.confirm`
(`TransactionModal.tsx`'s own delete flow is the precedent this mirrors),
and `RetrosPage.tsx` reacts to a successful discard on both counts a
household would notice: the draft leaves the history list and the Start
button returns. A real browser walk against this exact flow found one more
thing the round's own plan had not named — closing the modal alone left the
page's `selectedMonth` still pointed at the just-deleted retro, so
`RetroDetail.tsx` rendered "Couldn't load this retro." the instant a delete
that had actually succeeded finished; fixed in the same round with a new
`onDiscarded` callback, not deferred. `removeAction`
(`DELETE /retros/{month}/actions/{id}`) has the identical shape — a real
endpoint, no caller — and stays deliberately unbuilt: no mockup draws a
delete control on an action and no task's brief ever named one, so building
it now would be invention, not the spec's own ask; `docs/LEARNING.md`
already records this as a decision, not an oversight. Recounting by this
file's own rule leaves every area unchanged except Marriage, which moves
from 5/1/9/0 to **6/0/9/0** (still 15 rows, one moved from Partial to
Built), taking the stated totals from 62/18/23/2 = 105 to
**63/17/23/2 = 105** — row count unchanged, Built + Partial still **80 of
105** (a Partial becoming Built moves neither side of that sum).

**The net worth trend, 2026-08-19 — a wrong constraint corrected, a real gap
found and closed the same day.** The 12-month trend (Money's own last gap) is
code-complete and walked in a real browser: the twelve-month series, the
newest-bar-equals-headline guarantee, the change badge, the day-one and
sparse-history states, and the archived-account exclusion all behave as
specced. The row's own prose used to claim the trend needed a
balance-snapshot table; it never did (see the rewritten paragraph above). The
browser walk also found one live gap the plan's own test suite could not have
caught by construction: the change badge's text wrapped mid-phrase on
Overview at mainstream mobile widths (360px, 320px), stranding "month" alone
in the danger colour — Task 7 had flagged this exact risk as unverified and
deferred it to the browser pass, and the browser pass is what found it real.
First recorded 🟡 with the gap named rather than the ✅ the round's own plan
asked for, because CLAUDE.md's tracker rule outranks a plan once a real gap
is confirmed. A same-day fix round closed it: the badge now renders as its
own line beneath the figure whenever the caller supplies a changeNote
(Overview), matching the design's own Overview tile, instead of inline beside
the 30px figure; Finances' bare `▼ 6.0%` (no changeNote) is untouched and
still renders inline, as the design's Finances screen draws it. Confirmed
clean at 360px and 320px in a real browser and pinned by a structural test
(`NetWorthCard.test.tsx`, five cases) that asserts the badge's own DOM
arrangement — parent element and tag name, not merely its words — so a
regression back to always-inline cannot pass silently. The row moves to ✅.
Recounting by this file's own rule (the first symbol in each row's own cell)
found exactly one symbol changed at the time: "Net worth with 12-month
trend" moved 🟡 → ✅; "Net worth card" was left 🟡 for a reason this file
believed was real (no assets/liabilities split on Overview) and stayed 🟡.
Money moved from 24/4/7/0 to **25/3/7/0** (24 rows unchanged, one Partial
becomes Built), taking the stated totals from 63/17/23/2 = 105 to
**64/16/23/2 = 105** — Built + Partial still **80 of 105** (a Partial
becoming Built moves neither side of that sum).

**Second correction, same day — the "Net worth card" gap this paragraph
just left standing was never real either.** A review round checked the
claim above against the design itself rather than trusting the sentence
this file had just rewritten, and found it false: the Overview net worth
tile (`design/Household Dashboard.dc.html:305`) draws three stacked
lines — label, figure, change — in every iteration of the file, and
nothing else. The only "Assets & liabilities" block the design contains
anywhere is a separate sibling card, drawn only inside the Finances
screen's own `is_finances` block, which this product already builds as
its own row in this same table. There is no version of the
Overview tile, on either screen, that was ever meant to carry a
breakdown — a fact the row's own next clause had already half-admitted
("was never meant to… stays Finances-only by design") without anyone
following that sentence to its conclusion. **This is the same habit
pattern 16 was written for, caught happening a second time while writing
up the first**: a claim about a design mockup, restated without anyone
opening the mockup to check it, in the very paragraph that exists to
correct an unchecked claim about a query. See pattern 16's own entry in
`docs/LEARNING.md` for the fuller lesson. "Net worth card" moves 🟡 → ✅.
Recounting again: Overview (home) moves from 5/3/2/0 to **6/2/2/0** (10
rows unchanged, one more Partial becomes Built), taking the stated
totals from 64/16/23/2 = 105 to **65/15/23/2 = 105** — Built + Partial
still **80 of 105**.

**The Vision reconnaissance, 2026-08-28 — one row added, nothing built.**
Answering "what's next" meant reading the design's own Vision screen
(`design/Household Dashboard.dc.html:590-615`) against this table's four Vision
rows, and the screen draws a fifth thing no row covers: the marriage-duration
block beside the theme hero — "Married · 14 years · Feb 14, 2012". It is not
part of the yearly theme (a theme is set every January; a wedding date is set
once), and nothing in this product holds it — no column on `households`, no
field in Settings, no mention anywhere in `api/internal`. Added as ⬜ rather
than folded silently into "Vision — yearly theme", because a row is the only
way the next implementer finds out the date has to be captured somewhere
before that block can render at all. The same read also confirmed what the
four existing Vision rows already say — no `vision` symbol exists in
`api/internal` or `web/src`, so nothing here is built and nothing moves state.
Recounting by this file's own rule (the first symbol in each row's own cell)
leaves every area unchanged except Marriage, which moves from 6/0/9/0 (15
rows) to **6/0/10/0** (16 rows) — one row added, no row moved between states —
taking the stated totals from 65/15/23/2 = 105 to **65/15/24/2 = 106**. The
"Where things stand" headline (Built + Partial) is unchanged at **80**, now of
**106** rather than 105: a new Not-started row moves the denominator only.

**The Vision page ships, 2026-08-29 — three rows move.** Vision spec's task
11 built `VisionPage.tsx`, `PillarCard.tsx` and `MilestoneGrid.tsx` (the
route, the sidebar entry and the `/marriage` index redirect landing in the
same change, `110ab0a`'s own condition, restated in section 6 above): the
theme hero, the pillar grid with its measures, and the longer-horizon
milestone panel all read real data off `useVision(year)` (Vision spec's task
10). Three of the five Vision rows the reconnaissance above found move from
Not-started to Built — yearly theme, pillars with measures, longer-horizon
milestones. The other two stay ⬜: the marriage-duration block is still
deliberately unbuilt (Vision spec decision 2), and Edit vision (modal) is
still a placeholder `onEdit` — Vision spec's task 12 replaces it wholesale
next, which is also why the "+ Add milestone" tile counts as shipped here
rather than held back: it is visible and wired to the same handler the
header's own Edit vision button is, matching the design's own two entry
points into one editor (`dc.html` draws `onClick="{{ openVision }}"` twice —
the header button and the milestone tile; the empty state's own call to
action, added later, is Hearth's own third, not the design's) — even though
neither does anything yet. Recounting by
this file's own rule leaves every area unchanged except Marriage, which
moves from 6/0/10/0 (16 rows) to **9/0/7/0** (16 rows) — three rows move from
Not-started to Built, none added or removed — taking the stated totals from
65/15/24/2 = 106 to **68/15/21/2 = 106**. The "Where things stand" headline
(Built + Partial) moves from 80 to **83**, still of **106**.

**The Edit-vision modal ships, 2026-08-29 — one row moves.** Vision spec's
task 12 built `VisionModal.tsx` and replaced `VisionPage.tsx`'s own `onEdit`
— a no-op placeholder since task 11, whose own comment said as much — with
the real editor task 11 deliberately left for it. It is the whole document
in one form: theme, a year select offering only the previous, current and
next calendar year (a household setting January's theme in December needs
next year; one writing up a year nobody recorded needs last year), the
description, every pillar's name, description and measures, and every
milestone, saved together in a single `PUT`. Two fields the design's own
modal never drew at all (spec decision 7) are new here rather than carried
over from the mockup — a pillar's own description and a measure editor per
pillar — because the page already rendered both (task 11) with nowhere to
edit either. All three of `onEdit`'s call sites now open it, including the
empty state's own call to action, which a review finding on task 11
deliberately left unproven until this task made the handler do something
observable. Edit vision (modal) moves from Not-started to Built; the
marriage-duration block stays the one deliberately-unbuilt row in this
section (decision 2, unchanged by this task). Recounting by this file's own
rule leaves every area unchanged except Marriage, which moves from 9/0/7/0
(16 rows) to **10/0/6/0** (16 rows) — one row moves from Not-started to
Built, none added or removed — taking the stated totals from 68/15/21/2 = 106
to **69/15/20/2 = 106**. The "Where things stand" headline (Built + Partial)
moves from 83 to **84**, still of **106**.

**Vision's two Overview surfaces ship, 2026-08-29 — one row added, one row
moves.** Vision spec's task 13 built `VisionCard.tsx` and the check-in strip
inside `NextRetroCard.tsx` — the last two pieces of Vision & goals living
outside `/marriage/vision` itself. The Overview (home) section's own row for
"Vision 2026 card" never existed even though that section's intro paragraph
had counted the card among the design's seven since the Vision
reconnaissance (2026-08-28) — added now as ✅ directly, the same
find-and-build-in-one-motion this file has taken before, rather than landing
as a bare ⬜ first. "Vision check-in strip" moves from Not-started to Built.
Both surfaces read `useVision(currentVisionYear())` independently (one call
in each component, not one lifted to `OverviewPage.tsx` and passed down) —
`useVision` takes no `enabled` option, so gating the request for a member
without `marriage` has to be OverviewPage choosing not to mount the
component at all, the same shape `NextRetroCard.tsx`'s own header comment
already gives for `useRetros`; the two calls share one TanStack Query cache
entry, so mounting both costs one network request, not two — proved in
`OverviewPage.test.tsx`, not merely assumed from how the library is
documented to behave. Recounting by this file's own rule leaves every area
unchanged except Overview (home), which moves from 6/2/2/0 (10 rows) to
**8/2/1/0** (11 rows) — one row added as Built, one row moves from
Not-started to Built — taking the stated totals from 69/15/20/2 = 106 to
**71/15/19/2 = 107**. The "Where things stand" headline (Built + Partial)
moves from 84 to **86**, now of **107**.

**Vision's doc pass, 2026-08-29 — no row moves; three things corrected or
added in prose, one thing checked and left alone.** Task 14 verified rather
than assumed what Tasks 9–13 had already written here. First, the claim it
was told to fix — that this file and `docs/SYSTEM_DESIGN.md` currently say
the Edit-vision modal has "both" entry points — does not hold *today*: every
row and every narrative paragraph above already names **three** (the
header's Edit vision button, the "+ Add milestone" tile, and the empty
state's own call to action). It did hold once: Vision spec's task 11
(`8f2ee81`) shipped this file with "`VisionPage.tsx`'s `onEdit` is a no-op
placeholder ... both entry points, the header's Edit vision button and
MilestoneGrid's '+ Add milestone' tile, already call it" — true at that
moment, since the modal and its empty-state CTA did not exist yet — and
task 12 (`8814e70`) correctly rewrote it to "All three of `onEdit`'s call
sites" the instant the modal shipped a third. So the git history does
contain "both entry points" text, but as an accurate description of a
transitional state that was correctly superseded, not a persisting error —
a claim worth checking against the actual commits rather than a single
`grep` on the working tree, which is what the first pass of this note got
wrong. Separately, the one sentence elsewhere that reads ambiguously at a
glance — "matching the design's own two entry points," in the 2026-08-29
Vision-page paragraph above — is correct as written for a different reason:
`design/Household Dashboard.dc.html` really does draw
`onClick="{{ openVision }}"` exactly twice (the header button and the
milestone tile, not the empty state, which the design does not draw);
Hearth's own third is an addition, not a miscount. Clarified in place so it
cannot be misread as an undercount of what was built. Second, **the "Vision
— yearly theme" row now says plainly that per-year history is real and
reachable**, not merely stored: the schema keeps one vision per
`(household_id, year)` forever, and the modal's year select is a real,
working affordance onto it, not a drawn-but-inert field — narrowed to the
previous, current and next year by the UI, not by the API. Third, **all
four Vision rows in this section read ✅ ahead of Vision's own browser
walk** — Vision spec's task 15, not yet run as of this pass. This file's own
legend defines ✅ as "Built **and verified**," and the Retros rows just
above only earned a plain ✅ once Retros' task 17 walk passed 15 of 15
(recorded in its own paragraph above); Vision's four rows were marked ✅ by
tasks 11–13 as each shipped, with no equivalent walk behind them yet. Left
at ✅ rather than downgraded, on this task's own instruction — the code is
built, reviewed and covered by `make test`, exactly the same standing Bills
and Goals held between their own last code task and their walk — but stated
here so nobody reads "the Marriage rows are ✅" as "the walk has run." The
same gap applies to the two Overview rows in section 4. Fourth, checked and
left alone: **the `/marriage` index redirect and the year select's own
reach into a past year were both considered for their own row and neither
got one.** The redirect is unreachable-by-design plumbing — the identical
shape `moneyIndexRoute` already has, and that route has never carried a row
of its own either, only the prose above and in section 6 — and the year
select is not "something the design never drew" the way Accounts'
archive/restore or Goals' contributions were: `modalVision`'s own mockup
draws a Year field, `▾` and all, so widening its own row's prose (done
above) is the honest-sized change, not a new line item for a control the
design already put in the picture. **No row moved, none was added or
removed, and recounting by this file's own rule confirms the table below is
still exactly 71/15/19/2 = 107** — unchanged from the state Task 13's own
paragraph above left it in.

**Vision's fifteen-criterion browser walk (Task 15) has now run and
passed, 15 of 15** (2026-08-29), the same bar Retros' Task 17 and every
Money feature were held to before their rows could read plain ✅ —
confirming the six Vision rows in this section and the two Overview rows
(`Vision 2026 card`, `Vision check-in strip`) that had been marked ✅ ahead
of this walk, on the product owner's own instruction, per the doc pass
above. Recorded in full, criterion by criterion, in
`docs/superpowers/plans/2026-08-28-hearth-vision-verification.md`, including
one criterion (8, deleting a goal a measure links to) met by a deliberate,
narrow substitute rather than its most literal path: Goals' own spec
(`2026-08-01-hearth-goals-design.md`) says plainly that a goal is never
deleted and has no `DELETE` endpoint — so "delete that goal from
`/money/goals`" was exercised with a direct SQL `DELETE` against the same
row a household can only ever archive through the product, the identical
shape Retros' own criterion 10 used for a state its product had no button
for. **No product defect needed a code fix, and no row here moves or
downgrades to 🟡.** One thing surfaced during the walk that the final
whole-branch review (2026-08-29) later re-classified: editing and saving a
vision that already contains a measure whose linked goal was deleted resets
that measure to a typed `0 of 1` placeholder unless the household
explicitly fixes it first. First recorded here as a documented, deliberate
trade-off; the review's own verdict is the opposite — **a latent defect,
not an accepted trade-off** — dormant only because Goals has no `DELETE`
route to reach `MeasureBroken` through the running product at all (this
walk needed a raw SQL `DELETE` to produce it), not because the state is
actually fine to ship. Still no code fix here and no row moves, because the
fix belongs to whoever builds Goals' delete, not to this merge: a third
seeded state in `VisionModal.tsx` that blocks Save until the household
picks a goal or types a number, not a relaxed domain. Recorded in full in
`docs/LEARNING.md` under Vision's own walk section, and noted at the
seeding effect in `VisionModal.tsx` itself. Recounting by this file's own
rule confirms the table below is still exactly 71/15/19/2 = 107 — a browser
walk confirms rows already marked Built; it does not add or move one on its
own.

**The Telegram sign-in update, 2026-09-01 — three rows added, none moved, and
two of the three are deliberately NOT ✅.** Telegram sign-in adds a second
delivery channel for the sign-in and sign-up links Hearth already mints
([ADR 4](adr/0004-telegram-as-a-second-delivery-channel.md)). Three rows join Entry and
authentication: **Telegram sign-up (create a household from a chat)** and
**Telegram sign-in (a returning member)**, both **🟡**, and **Telegram
invites**, **⬜**, which this slice deliberately does not build. The two 🟡
rows are the point of this entry. Both features are code-complete, reviewed
across nine tasks, and `make lint && make test` green — and **not one criterion
of a browser walk has been exercised**, because creating a bot needs a Telegram
account and a BotFather token nobody has made yet. This file's own "What
'built' means" bar requires the feature to work *and* be verified, and every
Money feature, Retros and Vision each earned ✅ only after a fifteen-criterion
walk. Marking these ✅ on a green suite would be claiming a bar the last five
features were held to and these two were not, and this file records elsewhere
(`docs/LEARNING.md` pattern 15, and the notifications correction above) exactly
what it costs when a row claims something no household can yet experience.
They move to ✅ the day the twelve-criterion walk in
`docs/superpowers/plans/2026-09-01-telegram-sign-in-verification.md` is run and
passes — it is written out step by step, including the four traps that produce
a false failure. Recounting by this file's own rule — the first symbol in each row's own
cell — takes Entry and authentication from 10/1/0/0 to **10/3/1/0** and the
totals from 71/15/19/2 = 107 to **71/17/20/2 = 110**: three rows added, none
moved. "88 of 110 built or partly built" is up from 86 of 107 because a 🟡 and
a ✅ count the same there — which is precisely why the gap has to be named in
the cell rather than trusted to the headline.

**Amended again, 2026-09-01, from the whole-branch fix wave.** The review that
gated this branch found the gap named in the Telegram sign-up row above was
worse than the row said: a Telegram-only account has no password an
operator can reset and no address a magic link can reach, so a fourth row —
**attach an email address to a Telegram-only account**, **⬜** — joins Entry
and authentication to put that gap on the map rather than in a reviewer's
notes. One row added, none moved: 71/17/20/2 = 110 becomes **71/17/21/2 =
111**.

**The Telegram sign-in walk, 2026-09-01 — two rows move, none added or
removed.** `docs/superpowers/plans/2026-09-01-telegram-sign-in-verification.md`'s
twelve numbered criteria and its unnumbered checks all passed against a real
BotFather bot, `HearthOinkBot`, so **Telegram sign-up** and **Telegram
sign-in** move 🟡 → ✅ in section 1 above, dated 2026-09-01. Recounting by
this file's own rule — the first symbol in each row's own cell, not delta
arithmetic — takes Entry and authentication from 10/3/2/0 to **12/1/2/0**
and the totals from 71/17/21/2 = 111 to **73/15/21/2 = 111**: two rows
moved, none added, none removed. **Attach an email address to a
Telegram-only account** stays ⬜ — a browser walk of the two channel rows
above does not close it; it still needs a settings-page flow, per its own
row. "88 of 111 built or partly built" is unchanged, because a 🟡 and a ✅
count the same there — precisely why the gap had to be named in the cell
rather than trusted to the headline.

**The admin surface, 2026-09-02 — a new section added, one existing row
moved, and this file's denominator grows for a reason none of the updates
above needed.** `docs/superpowers/specs/2026-09-01-hearth-admin-surface-design.md`
shipped code-complete, reviewed and walked 15 of 15 criteria against the dev
stack
(`docs/superpowers/plans/2026-09-02-hearth-admin-surface-verification.md`):
a re-authenticated `/admin` surface, feature flags with a global default and
a per-household override, an append-only audit log, and four new `adminctl`
commands. A new **§9 · Platform administration** holds five rows for it —
four ✅, one 🟡 (the flags screen itself, missing a control to create the
household override the whole model exists to support, and a near-invisible
toggle; see [ADR 5](adr/0005-platform-admin-authorization.md)). Section 7's
**Shared month calendar with per-person filters** moves ⬜ → 🟡: the branch
gave it an API route and a flag as a deliberate dark-shipping exercise, with
no events and no UI behind either. Recounting by this file's own rule — the
first symbol in each row's own cell, not delta arithmetic — takes the totals
from 73/15/21/2 = 111 to **77/17/20/2 = 116**: five rows added, one moved,
none removed. Unlike every earlier update in this table, the five new rows
are not measured against `design/Household Dashboard.dc.html` at all — §9's
own header says why they are counted anyway. "88 of 111 built or partly
built" becomes **"94 of 116"**; the gap this file exists to name for the
flags screen sits in its own cell above, not folded into that number.

| Area | Built | Partial | Not started | Out of scope |
|---|---|---|---|---|
| Entry & authentication | 12 | 1 | 2 | 0 |
| Navigation shell | 7 | 1 | 1 | 0 |
| Household settings | 11 | 8 | 2 | 0 |
| Overview (home) | 8 | 2 | 1 | 0 |
| Money | 25 | 3 | 7 | 0 |
| Marriage | 10 | 0 | 6 | 0 |
| Family | 0 | 1 | 1 | 1 |
| Household extras | 0 | 0 | 0 | 1 |
| Platform administration | 5 | 1 | 2 | 1 |
| **Total** | **78** | **17** | **22** | **3** |

---

## What "built" means

A row only becomes ✅ when the feature works **and** the code behind it meets the
standards below. A feature that works but nobody can safely change is not
finished — it is a debt with a tick next to it.

**Clean architecture.** Dependencies point inward. `internal/domain` holds the
rules and imports the standard library only; `internal/usecase` orchestrates
them through ports it declares itself; adapters implement those ports. Any
database, HTTP or third-party type stops at the adapter boundary. `make
lint-arch` enforces this mechanically, including in test files.

**SOLID, as it actually applies here.**

- **Single responsibility** — one file, one job. `auth.go` does sign-in;
  `invite.go` does invites. When a file starts needing "and" to describe it,
  split it.
- **Open/closed** — extend by adding an adapter, not by editing a service.
  `FXRateProvider` exists precisely so a real rate source can arrive without
  touching the code that uses it. `BankSyncProvider` does not exist:
  Accounts, Money's first feature, ships manual entry only, which needs no
  port at all — a port with one implementation and no second caller is the
  wrong shape. One arrives when CSV import gives it a second implementation
  to abstract over.
- **Liskov** — an adapter must honour its port's whole contract, including
  errors. A missing row surfaces as `domain.ErrNotFound`, never `pgx.ErrNoRows`.
  A caller must not need to know which implementation it has.
- **Interface segregation** — narrow ports for what a caller needs. Nine small
  repositories, not one object with forty methods.
- **Dependency inversion** — services depend on interfaces they declare, and
  `main.go` decides the implementations. That is the whole reason the services
  are testable against in-memory doubles.

**Readable by a junior engineer on their first week.** This is a real
requirement, not a nicety:

- Names say what a thing is. Comments say **why** — never what the line already
  says.
- Small, focused files beat clever ones. If understanding a function needs three
  other files open, the seam is wrong.
- Exported things carry their contract in a doc comment. `usecase/ports.go` is
  the model: the `""` ⇄ SQL NULL convention and the transactional-accept warning
  live where the next implementer will actually read them.
- Every non-obvious decision is written down at the point someone would try to
  change it. Where a trade-off was accepted, the comment says so and why.
- Tests read as documentation. A test name states the behaviour; the body shows
  it.
- No cleverness in security-sensitive code. Obvious and boring wins.

**And the practical gate:** `make lint && make test` green, at least one new test
mutation-checked, and `docs/LEARNING.md` updated with anything the work taught.
The full checklist is at the end of `docs/LEARNING.md`.

---

## 1 · Entry and authentication

| Feature | State | Notes |
|---|---|---|
| Sign in with email and password | ✅ | No household details shown before authentication, per the design |
| Wrong-password state with attempts remaining | ✅ | "Two tries left", "One try left" — the design's copy verbatim |
| Household lockout after three failures | ✅ | Locks the household for 15 minutes, as the design's copy states |
| Magic link — request | ✅ | Always answers the same way whether or not the address exists |
| Magic link — sent panel | ✅ | Carries retry copy; the send is fire-and-forget so nothing else prompts it |
| Magic link — consume and sign in | ✅ | Works while the household is locked; that is the recovery path |
| Create household (self-serve sign-up) | ✅ | `POST /auth/sign-up` answers `202 {"status":"accepted"}` identically for a fresh, already-registered or rate-limited address — the same enumeration-safe contract as magic link. The design's single create card is split across two screens (email only, then the rest) so the person who clicks the mailed link supplies their own details, never a stranger's |
| Household provisioning | ✅ | One transaction creates the household, the owner, their membership, the three builtin spaces and notification preferences, with the sign-up token consumed first so two completions of one link can never both succeed |
| Invite acceptance | ✅ | Shows inviter, household and role; warns if you are already signed in as someone else. A limited member invited with all three capability toggles off read "Joining as Kid — no access only." here, and "Kid · no only" in the Settings members list, until 2026-09-01: `limitedAccessPhrase` returned the fragment "no" and each of its two callers completed it differently. It now returns the whole clause (`limitedAccessClause`, "calendar & chores only" / "no extra access"), covered by `features/auth/copy.test.ts` |
| Sign out | ✅ | From the sidebar footer, returns to sign-in |
| "Forgot?" password recovery | 🟡 | Present, and triggers a magic link. There is no separate password-reset flow — recovery is `make reset-password` from the command line |
| Telegram sign-up — a stranger creates a household from a chat | ✅ | Code-complete and reviewed across nine tasks: `POST /auth/telegram/start` mints a 10-minute nonce, the bot answers `/start` with a 24-hour sign-up link, and `SignupRepository.Provision` binds the chat inside the transaction that already creates the household, owner, membership, spaces and preferences — so an owner with **no email address at all** is a first-class user, the second kind after a child. `make lint && make test` green. **Walked against a real BotFather bot (`HearthOinkBot`) and passed, 2026-09-01**: a stranger's `/start` produced a sign-up link, the create-household screen showed the Telegram sentence with no email box anywhere on it, provisioning landed the new owner signed in with `email` NULL and a bound `telegram_accounts` row, and an `INSERT` naming both an email and a chat id was refused by the database's own `signups_have_exactly_one_channel` CHECK. Full record: `docs/superpowers/plans/2026-09-01-telegram-sign-in-verification.md`. Off unless configured — an install with no bot answers `404` and the sign-in screen hides the control |
| Telegram sign-in — a returning member | ✅ | The bot resolves the chat to its bound user and sends the **existing** magic link, 15 minutes, single use — no new token type and no new session-issuing code anywhere. Three limits stack: 20/hour per IP on the start route (its own bucket, not sign-up's), 3 links/hour per chat, and the shared global daily sign-up ceiling. An unknown, expired, consumed, rate-limited or ceiling-blocked `/start` all get one identical refusal, word for word, so none of them can be told apart by probing. **Walked against the same real bot and passed, 2026-09-01**: a bound chat's `/start` sent a sign-in link rather than a second sign-up link — the discriminating result, since an unbound chat would have minted a second signup and could have created a second household, and the household count held at 6 throughout — tapping it signed the same user in, and the spent link and the fourth `/start` within the per-chat window were both refused with the identical wording. Full record: `docs/superpowers/plans/2026-09-01-telegram-sign-in-verification.md` |
| Telegram invites — a shareable `t.me/…?start=inv_<token>` link | ⬜ | **Deliberately not built in this slice**, not an oversight. Invites still go to an email address and are still relayed from Mailpit by hand on the live install, which is one person's inconvenience on a two-person install rather than a blocker. It is the natural follow-up: the delivery channel exists now, so this is a payload change to the same `/start` parsing, not new infrastructure. Recorded here so the gap is on the map — see `docs/SYSTEM_DESIGN.md` §5 and [ADR 4](adr/0004-telegram-as-a-second-delivery-channel.md)'s Out of scope |
| Attach an email address to a Telegram-only account | ⬜ | **Not built — found in the branch's whole-branch review, 2026-09-01.** A Telegram sign-up leaves `users.email` NULL. `GetUserByEmail` is `WHERE email = $1` and NULL never matches a parameter, so the password collected at sign-up cannot sign anyone in, `POST /auth/magic-link` has no address to send to, and `adminctl reset-password --email=` cannot address the account — Telegram becomes the only door into that household with no operator recovery path today. `SignUpCompleteScreen.tsx` now says this to the person signing up. The fix is a settings-page flow that lets a signed-in Telegram user add and verify an email, giving `GetUserByEmail` something to match; until then the only recovery an operator has is `make psql` by hand (see `docs/LEARNING.md`) |

## 2 · Navigation shell

| Feature | State | Notes |
|---|---|---|
| Sidebar grouped into spaces | ✅ | Which spaces appear, and in what order — rendered from the server's own filtered, ordered list, never re-sorted or re-filtered client-side. Grouping a space's own pages under its label is the row below |
| Sidebar's design 5a form — a space with several built pages renders as an uppercase group label plus one link per page | ✅ | `SPACE_PAGES` map in `Sidebar.tsx`; a space with one built page renders the identical group-label-plus-link shape, not a bare single link (the earlier single-link branch was deleted as unreachable once every remaining builtin space had two or more pages — task 10 exercised that generality for the first time when Marriage's own one-page entry (Retros) came back needing no new rendering code). Money took the grouped form once Transactions shipped a second page; Family still renders nothing at all, since the sidebar shows a builtin space only when `SPACE_PAGES` names at least one built page for it and Family's own entry has not come back since `110ab0a` (see section 7 below). A *custom* space from "+ New space" still renders as a single link — the rule keys off `isBuiltin`, not off the absence of a pages entry. Active state is computed per link via `useMatchRoute`, not `Link`'s `activeProps` — `activeProps` merges its class onto the base class rather than replacing it, which shipped an active link with both an ink and an accent color class present at once and the accent never winning the cascade (`docs/LEARNING.md` pattern 3) |
| Space visibility per member | ✅ | Money is capability-gated, Marriage is parents-only, Family is for everyone |
| Household footer with members and plan | ✅ | "Free plan" is static text, as specified |
| Modal primitive | ✅ | Native `<dialog>`; backdrop dismissal, Escape, focus trap. Slices 2–4 build on it |
| Placeholder pages for unbuilt areas | ✅ | None are left. `/` stopped using it when the interim Overview shipped (M2, before Bills); `/money/$` — the last route still pointing at it — was replaced outright by `/money/bills` when Bills shipped (commit `946630e`), not merely joined by a sibling. The component itself is unreferenced today, kept rather than deleted because Family will want it again the moment it grows a route with no page behind it yet (Marriage no longer needs it — task 10 replaced its placeholder with the real `RetrosPage`, not a stand-in). Marriage's and Family's own placeholders were both deleted with their routes in `110ab0a` — a placeholder is honest as the *inside* of a space a household already has, and dishonest as a whole navigation destination offering a page that does not exist |
| `⌘K` command palette | ⬜ | Shown in the sidebar header; no behaviour behind it |
| "+ New space" | ✅ | See Household settings below |
| Mobile-responsive layout | 🟡 | Every existing screen reflows down to 320px, same UI and structure, no redesign: `AppShell`'s sidebar becomes an off-canvas `NavDrawer` (reached through `MobileTopBar`'s hamburger) below `lg` (1024px), restoring the original two-column grid at `lg` and above via `lg:contents`; auth cards, page gutters (`PageContainer`) and modal field pairs (`FieldPair`) reflow at `sm`; full-height boxes (`Modal`, `NavDrawer`, every auth screen) use `dvh` rather than `vh` so iOS Safari's collapsing toolbar cannot hide content under it; interactive controls carry a 44px touch-target floor. Walked at 320/375/414/768/1024/1440 across every authenticated screen, sign-in, sign-up and one modal from each family (`docs/superpowers/plans/2026-08-15-mobile-responsive.md`, Task 10's own width matrix); the walk found one real failure — `BudgetModal`'s category rows overflowed their own dialog box at every width (a native `<dialog>` paints in the top layer, so `document.documentElement`'s check cannot see it; the dialog's own `scrollWidth` vs `clientWidth` can) — and it was fixed in the same task (`min-w-0` on the row's flexible name field, the same unshrinkable-flex-item class already on record for `<select>` and `BudgetStatCards`), not merely logged. Five gaps stayed under the 44px floor, each measured and deliberately left rather than missed: `MembersPanel`'s owner/limited role toggle (24.5px — raising it outgrows the 34px avatar beside it); the 16 `ToggleSwitch` instances across Settings and Money (23px — several sit 6–14px apart, and a larger target would make adjacent switches overlap, worse for hit accuracy than a small one); `BillRow`'s Mark paid/Archive/Restore/Undo controls (16.5px — raising them grows the stacked mobile cluster from ~89px to ~144px against a ~40px left block, and fixing it properly needs a row restructure the spec forbids); `BudgetRolloverCard`'s "Move into goal" link (genuinely inline mid-sentence, so `min-height` does not apply); and `BudgetPage`'s `‹`/`›` month arrows (raised on height only, `h-11` with no `w-11`, because the full square wraps "Edit budget" onto two lines in that row's real state). One structural gap, separate from the floor: `AppShell` leaves `<main>` `inert` if the drawer is open when the viewport is resized past `lg` — hamburger and backdrop are both `lg:hidden` by then, but `<nav>` is a sibling of `<main>`, never inert, and renders normally at `lg` via `lg:contents`, so Escape or any sidebar navigation both clear it; reachable by tablet rotation. Two pre-existing `md:` breakpoints (a third, un-migrated size step) survive in `BudgetStatCards.tsx` and `FinancesPage.tsx`, left alone rather than folded into the two-breakpoint (`sm`/`lg`) convention the rest of the round follows. **Two escapes found and fixed on 2026-09-01, after the round:** the sign-up completion screen (`/sign-up/$token`), built later and never in the round's width matrix, held its auth card at the full desktop 428px inside a 375px phone and scrolled the page sideways — its currency `<select>`'s longest option is that card's wrapper's min-content width, and a grid item's `min-width: auto` floors the track it sits in; fixed with `min-w-0` on all seven auth-card wrappers (the same wrapper is copy-pasted across six files) plus `break-words` where an address or a household name is echoed back, which `min-w-0` alone does not cover. And `BudgetPage`'s month-chip/History/Edit-budget row was 9px over the viewport at 320px and flex-shrunk into two-line labels at 375px; it now wraps. And every icon that was a Unicode character became an inline SVG (`components/icons.tsx`): `⏻` on sign out rendered as an empty button on the product owner's Samsung Fold 7, which has no font covering U+23FB — `☰` and the five `✕` controls were the same bet on the same device and went with it. Re-walked at 320/375/884/1440. The row stays 🟡: the five 44px gaps and the `inert`-on-resize gap above are unchanged |

## 3 · Household settings

| Feature | State | Notes |
|---|---|---|
| Members list with roles | ✅ | Owner and limited, with the design's own role labels |
| Member access switches | ✅ | Calendar, Chores, Money, Marriage. Marriage is never offered to a child |
| "Off for kids by default" on Money | ✅ | A child can be granted Money access; the design's toggle is real |
| Email addresses hidden from non-owners | ✅ | Owners see them; a limited member sees the list without addresses |
| Last-owner protection | ✅ | Removing or demoting the last owner is refused inline |
| Invite a family member (modal) | ✅ | Name, role, optional email, access switches |
| Remove a member | ⬜ | No control in the design either; the backend supports it |
| Spaces list with audiences | ✅ | |
| New space (modal) | 🟡 | Everyone and Parents only work. **Custom is shown disabled** — per-space membership is not built, and the design marks custom space pages "not built" too |
| Space templates — Kids, Home, Travel, Blank | 🟡 | Offered; they set a suggested name and visibility. They create no pages, because custom space pages are out of scope |
| Currency list (`GET /api/v1/currencies`) | ✅ | One server-served ISO 4217 list — only two-minor-unit codes are offered, since `Money.String()` hard-codes two decimal places. The frontend stopped keeping its own `CURRENCY_SYMBOLS` table; this backs both this panel and the sign-up form's currency select |
| Currency and region — primary currency | ✅ | |
| Currency and region — show second currency | ✅ | The toggle only — whether the second currency is shown. What it *is* is the row below |
| Currency and region — choose the second currency | 🟡 | No control exists to pick *what* the second currency is. A household that enables the toggle cannot choose what to compare against. Self-serve sign-up sets a household's second currency equal to its primary and leaves the toggle off, which makes this gap reachable by any stranger who signs up, not only Andreas & Christine |
| Currency and region — FX rate | 🟡 | The mode is stored and editable, but the rate itself is a fixed table. A live provider drops in behind the existing port |
| Notifications — bill due reminders | 🟡 | The preference is stored, served and editable end to end; **nothing sends the reminder.** `usecase.Mailer` has three methods — magic link, invite, sign-up — and no caller reads `notification_preferences` to mail anything. Nothing in this codebase runs on a clock either: the only cron anywhere is the box's nightly backup. The design's own copy promises delivery, not a switch — "Bill due reminders (3 days before)" |
| Notifications — overspend alerts | 🟡 | Same gap as the row above: stored and editable, never sent |
| Notifications — monthly retro reminder | 🟡 | Same gap as the two rows above: stored and editable, never sent. Retros itself is built now (section 6), so this row has something real to remind a household of — the gap is only that nothing in this codebase runs on a clock |
| Notifications — weekly family digest | 🟡 | Same gap: stored and editable, never sent |
| Retention pruning (`adminctl prune`) | ✅ | No UI — `cmd/adminctl prune --older-than=<days>` deletes consumed/expired `signups`, stale `login_attempts` and consumed/expired `telegram_link_requests` past the cutoff. Refuses anything under a seven-day floor so it can never reach inside `domain.LockoutPolicy.Window` and clear a lockout that is still live |
| Connected accounts | ⬜ | Belongs with Money. Note that automatic bank sync is not available to an app like this — see Money below |

## 4 · Overview (home)

`/` is a real page as of the interim Overview (M2), and now carries six of
the design's **seven** cards (counted directly off `design/Household
Dashboard.dc.html`'s own Overview screen — the money row of four, the
Marriage "Next retro" card, "This week" and "Vision 2026"; the header's own
"+ Add" button is not a card) — three from Money (net worth, budget, goals),
one more from Bills (next bill) and two from Marriage (next retro, Vision
2026) — plus a setup checklist and a limited-member panel the design does
not draw. The Vision check-in strip (design's own line inside the Next retro
card, not a card of its own, so it does not add to the seven) is built too.
The one remaining card ("This week" agenda) needs Family, which does not
exist yet — so this section is no longer mostly ⬜, and the page grows into
the designed Overview rather than being replaced.

| Feature | State |
|---|---|
| Net worth card | ✅ — the figure, the not-computable case, and the month-to-date change badge, by reusing Finances' own card. The design's own Overview tile (`design/Household Dashboard.dc.html:305`) draws exactly three stacked lines — label, figure, change — and nothing else, in every iteration of the file; the only "Assets & liabilities" block anywhere in the design is a separate sibling card on the Finances page, which this product already builds as its own row below. There is no version of the Overview tile that was ever meant to carry a breakdown, so there is no gap left to name here |
| July budget card — percentage used | 🟡 — percentage used plus the two figures behind it, and a "Set a budget" link when the household has never budgeted. No sparkline. Owner-only: `GET /budgets/{month}` is `requireCapability(money)` **and** `requireOwner` |
| Next bill card | ✅ — the next-due bill's name, amount and due date (or the overdue/autopay state in its place), reading `useBills`, the same hook and cache entry `/money/bills` itself uses |
| Goals on track card | ✅ — the real `X of Y on track` figure and the next dated goal beneath it, reading `useGoals`, the same hook and cache entry `/money/goals` itself uses. A household whose every live goal is achieved reads "All goals reached": an achieved goal is counted in neither `datedCount` nor `noDateCount` and is never `nextGoal`, so all three clauses were null at once and the card painted its heading over blank space (`docs/LEARNING.md`, the zero-render pattern) |
| Next retro card with carried-over actions | ✅ — shows the current month's retro (draft or finished), or the startable month as a prompt, plus its OPEN action count beneath. `GET /retros` carries both `actionCount` (the total) and `openActionCount` per row; the card reads the latter, the design's own "carried-over actions" figure. Shipped 🟡 first (Task 15, reading the total instead, which overstated outstanding work on a fully-ticked retro) — closed end to end (SQL subquery through the zod schema), leaving `RetroHistoryList`'s own "K actions, ticked or not" row reading the total it is supposed to |
| Vision 2026 card | ✅ *(`VisionCard.tsx`, Vision spec's task 13 — a row this table never carried until now, though the section's own intro paragraph had already counted it among the seven cards since the Vision reconnaissance. Renders one line per pillar, in `position` order, each showing that pillar's FIRST measure with its live figures, not the design's own three flat commitment lines ("1 weekend away per quarter" etc.) — a third shape the design never says how to store (spec decision 3). A pillar with no measures falls back to its own name; a measure with `hasFigure: false` shows its label and no number. Omitted entirely — not an empty quotation — for a year with no vision yet (`version: 0`); Overview's setup checklist is the surface that names what is missing, not this card)* |
| Vision check-in strip | ✅ *(inside `NextRetroCard.tsx`, Vision spec's task 13 — "Vision check-in: 2026 theme — "…"", gated on the theme being non-empty, which a version-0 year always sends as `""`, so the strip and the card agree about when there is nothing to show without a second, separate check)* |
| "This week" agenda | ⬜ |
| "+ Add" quick-create menu | 🟡 — four of six entries now live: Transaction, Account, Savings goal and Bill. Transaction and Bill are both disabled with their reason until an account exists — a bill needs a pay-from account the same way an expense needs a from-account; Savings goal has no such precondition (Goals decision 6 — there is no funding-source account to require). Calendar event and Marriage retro still join it in the change that builds each |
| Setup checklist (no mockup — see below) | ✅ |
| Limited-member "amounts are hidden" panel (no mockup — see below) | ✅ |

The "+ Add" menu offers Transaction, Account, Bill, Savings goal, Calendar
event and Marriage retro — four of six are live (see the row above). Neither
remaining entry is still blocked on a missing space the way both once were:
`/marriage/retros` exists now, the same way `/money/bills` existed before its
own menu entry was wired in, so a Marriage retro entry is buildable today
without waiting on anything else. Calendar event is the one still waiting —
on Family.

Two rows above have no mockup of their own. The **setup checklist** is three
steps derived from data the page already fetched (create your household, add an
account, set a budget for the current month); it disappears at three of three,
so an established household is not shown a permanent chore list, and it never
renders for a limited member, who can do none of it. It has three steps rather
than the four an onboarding flow would suggest: an emailed invite writes only
to the `invites` table while `GET /household/members` reads memberships, so an
"invite your partner" step could only tick once the partner *accepted* — the
step joins the list in the change that exposes pending invites. The
**limited-member panel** exists because Overview is the only page every member
reaches: a limited member holding `money` gets no summary, no budget card and
no checklist, and without it saw a page with nothing on it at all (found in the
browser walk, `2026-07-31-hearth-interim-overview-verification.md` criterion 9).

## 5 · Money

All five features are built — Budget's own history modal is whole except for
Export CSV, and Goals is whole except for the funding-source account the
design draws and decision 6 deliberately drops. Bills is the last of the
five to ship: recurring bills on a one-off/monthly/quarterly/yearly cadence,
marked paid by writing a real expense transaction so Budget, Spending by
person and net worth all move, archive and restore, undo, and a
subscriptions rollup. This is still the largest area, and every task on it
is code-complete, reviewed clean, and walked — Task 18's own 15-criterion
browser walk ran 2026-08-10 and passed 15 of 15, so "built" here means the
code *and* a walk confirming it. All five Money features are now walked.

**Finances**

| Feature | State |
|---|---|
| Net worth with 12-month trend | ✅ — the twelve-month series, the newest-bar-equals-headline guarantee and the month-to-date change badge, walked in a real browser (Task 8, 2026-08-19). A fix round the same day closed the one gap the walk found: Overview's change badge (`▼ 6.0% this month`) wrapped mid-phrase at 360px and 320px because it rendered inline beside the 30px figure; it now renders as its own line underneath whenever the caller passes a changeNote (Overview), matching the design's own Overview tile, while Finances' bare `▼ 6.0%` (no changeNote) stays inline as it always did. Confirmed clean at both widths and pinned by a structural test (`NetWorthCard.test.tsx`) so it cannot silently regress to inline |
| Assets and liabilities breakdown | ✅ |
| Accounts by owner, with SGD/IDR split | ✅ |
| Recent transactions strip | ✅ |
| Link account — step 1, choose source | ⬜ |
| Link account — step 2, authorise | ⬜ |
| Link account — step 3, details and ownership | ⬜ |
| Manual account entry | ✅ |
| Archive and restore | ✅ |
| Custom account types | ⬜ |
| Warning in Settings before a primary-currency change strands every account | ⬜ |

**The 12-month trend shipped 2026-08-19, and the note that used to sit here
was wrong.** It used to say the trend "needs balance snapshots: a second
table, and a separate decision about when a snapshot gets written." It never
did, and nobody had checked that claim against the code: balances in this
product have always been derived from the transactions ledger, the same way
`ListAccounts` computes today's balance on every read
(`api/internal/adapter/postgres/queries/account.sql`). The trend is the same
idea walked back twelve months — `ListAccountMonthlyMovements` sums each
account's transactions by calendar month, and
`AccountService.trend` (`api/internal/usecase/networth_trend.go`) subtracts
each month's delta from today's live balance to get that month's figure.
Every bar is recomputed on every `GET /accounts`; nothing is written or
scheduled. The trade-off this buys — history is not frozen, so an account's
past bars move if you edit an old transaction, and every month is converted
at today's FX rate rather than the rate that held at the time — is spec
decision 1 and decision 2 of
`docs/superpowers/sdd/2026-08-19-net-worth-trend/`, not a gap. The browser
walk that shipped this also found and closed a real, if narrow, defect: the
change badge wrapped mid-phrase on Overview at mainstream mobile widths —
see the row above for what broke and how it was fixed. `docs/LEARNING.md`
records the lesson behind the wrong constraint: a document that states an
implementation constraint without citing the code that imposes it is a claim
the next reader will believe rather than check.

**Archive and restore is not drawn anywhere in the design.** There is no
remove control on the design's own account form or accounts list. An account
is never deleted — archiving stamps a timestamp, and a "Show archived" view
lists archived accounts with a restore action, so a mistake is recoverable.

**Custom account types and the primary-currency-change warning are deferred,
not missing by accident.** Five fixed types (`cash`, `investment`, `property`,
`loan`, `credit_card`) cover the design's own breakdown bars; a household
that wants something more specific, like "Gold savings" or "Arisan", names it
in the free-text nickname on a `cash` account instead. Custom types would also
need to be seeded at household creation, which reaches into the self-serve
sign-up provisioning transaction for a feature nobody has asked for yet — the
wrong trade today. The currency-change warning belongs to the Settings
screen: an owner changing the household's primary currency can strand every
account with no exchange rate to the new one, and the design has no copy or
control for warning them first. The state it would prevent is visible and
reversible without the warning (the screen already says when no account can
be converted, and changing the currency back restores the figure), which is
why this is deferred rather than treated as a defect.

**Link account — steps 1 and 2 of the design's chooser will not be built as
drawn.** SGFinDex access to real bank data is restricted to licensed
financial institutions (see the bank-sync note below), so the design's
"Connect a Singapore bank" card can never turn on. A chooser with one
permanently dead branch teaches nothing, so `+ Add account` opens step 3's
own form directly — built and counted as *Manual account entry* above, not as
a separate "step 3" row, since it is the same shipped modal under a different
name.

**Automatic bank sync is not buildable here.** SGFinDex access is restricted to
licensed financial institutions. The design's Singpass flow will be shown
unavailable; accounts arrive by manual entry or file import, behind a port that
a real aggregator could later fill.

**Transactions**

| Feature | State |
|---|---|
| Full ledger with filters | ✅ — a transaction dated on its account's opening date now moves the balance and net worth: the opening balance is the figure at the *start* of its day, so the balance sum filters `occurred_on >= opening_balance_as_of` and the ledger's before-marker flips to strictly-before (`<`) to match. Was 🟡 from the Transactions merge until the finance-fixes round closed it; see `docs/superpowers/specs/2026-07-30-hearth-finance-fixes-design.md` decision 1 and `docs/LEARNING.md` pattern 13 |
| Inline category editing | ⬜ |
| Add transaction (modal) | ✅ |
| Export CSV | ⬜ |

**Full ledger with filters and Add transaction share one modal, add and edit
alike** — the same `TransactionModal` Task 15 built, opened blank for a new
row (its own test: blank fields, POSTs on save) and opened populated for an
existing one clicked from the ledger. That click-to-edit path is also the
only caller `PATCH /transactions/{id}` has, and its own Delete control
(behind an in-page confirmation, never `window.confirm`) is the only caller
`DELETE /transactions/{id}` has. The ledger itself covers all five screen
states the spec's section 7.1 names: first run, filters matching nothing
(deliberately different copy from first run, so a household that filtered to
nothing doesn't think its ledger was wiped), transactions excluded from the
month's spend for want of an exchange rate, a row dated before its account's
opening balance (naming the account, since a transfer can predate one side
and not the other), and a disabled Add button when the household has no
accounts yet to attach a transaction to. "Load older transactions" appends a
second page held in local state, separate from the reactive first page —
editing or deleting a row that lives on an appended page patches or removes
it there directly, so a correction made on an older page is not left showing
its stale, pre-edit value.

**The month the header names and the month the ledger lists are the same
month, and always were meant to be.** `handleListTransactions` says so in its
own doc comment — the ledger and the two figures above it "are one screen and
must describe the same month" — but `parseTransactionFilter` defaulted only the
summary's month and left `filter.Month` zero, which means every month, so the
screen read "0 in August 2026" above ten July rows. The default now applies to
both halves. `month=all` is the one deliberate way out and widens the **list**
only: the summary still answers for one calendar month by construction, so the
screen drops its count and names the month beside the spend figure rather than
printing either over rows they do not describe. The Month control opens on the
month the response named (it opened blank, which Chrome draws as a row of
dashes reading like a broken control), and an empty current month gets its own
state — naming the month, offering "Show every month" — instead of the
first-run panel, which only the widened view can honestly show.

**Inline category editing and Export CSV are the two pieces of the design's
Transactions screen still unbuilt, for different reasons.** Changing a
transaction's category today means opening the same edit modal used for
everything else, not clicking the category text directly in the ledger row —
a second control for the one field the modal already edits is more surface
for the same outcome, so it is deferred rather than missing by accident.
Export CSV is deferred for a structural reason (the transactions spec's
decision 7): `apiFetch` is the only way the frontend talks to the server and
throws on an ok response it cannot parse as JSON, so a CSV download needs its
own non-JSON response path out of the frontend, with its own guard and its
own test. Building the file client-side from what the ledger has already
fetched was rejected for the same decision: with keyset pagination, that would
silently omit every row past the first page and produce a file that looks
right but isn't.

**Categories**

| Feature | State |
|---|---|
| Category list, seeded on first use | ✅ |
| Rename, create and archive a category | ✅ |

**The list, the seeding, and editing are all built.** The starter set of
thirteen categories (`domain.StarterCategories()`) is created the first time
a household's own list is read — by `GET /api/v1/categories` or the
transaction modal's dropdown — not at household creation, so provisioning a
new household stays untouched by a feature it does not need. Renaming,
creating and archiving a category happens inline in the Edit-budget modal
(`BudgetModal.tsx`, Task 14) rather than a separate "Edit categories" screen —
the design's own spec (not the original dc.html mockup) folds those controls
into the same modal a household is already capping categories in, which is
why this feature adds a row the design's mockup itself has no dedicated
screen for.

**Budget**

| Feature | State |
|---|---|
| Envelope per category with pace | ✅ |
| Empty state with Family-of-four, 50/30/20 and import templates | ✅ |
| Spending by person | ✅ |
| Unattributed row on Spending by person (no mockup — see below) | ✅ |
| Edit budget (modal) | ✅ |
| Budget history (modal) | 🟡 *(Export CSV deferred — `apiFetch`'s JSON-only contract, transactions decision 7)* |
| Roll unspent into savings | 🟡 *(the manual move ships; the design's automatic month-end toggle does not)* |

**A household can create, edit and review a budget end to end now.** The
four stat cards (Budgeted, Spent, Remaining, Daily pace), the per-category
caps grid with the over state, and Spending by person all render live from
`GET /budgets/{month}` — `BudgetPage.tsx` and its own card components, backed
by `useBudget`. The empty state (`budget: null`) renders the design's real
"No budget set for &lt;Month&gt; yet" panel, both templates and the
conditional "Import last month" card, with every template's caps computed for
real (`budgetTemplates.ts`: exact name mapping onto the household's live
categories, `missing` for anything unmatched, the 50/30/20 proportional split
with its 20%-headroom flooring, computed live as income is typed). Clicking a
template, or "Create your first budget", opens the real Edit-budget modal
(`BudgetModal.tsx`) rather than a stub: three cards (expected income,
Allocated, Left to allocate — the last two hidden as a pair while income is
blank), one editable row per capped category (rename, cap, ✕ to drop the cap,
archive), and "+ Add a category" (an existing uncapped category, or a brand
new one). Save runs every queued category create/rename/archive first, in
order, through their own endpoints, then issues one `PUT` with the full line
set; a failure anywhere in that sequence keeps the modal open with the error
inline and never fires the remaining calls, including the `PUT`. A 409 name
collision names the taken name (the server's own message doesn't carry it,
so the modal composes it from the name it just attempted); a name that
belongs to an *archived* category offers restore instead of silently 409ing
on `categories_household_id_name_key` (the gotcha Task 13's review flagged).
Task 15 closed a real gap Task 14 left behind: there was no way to *open* the
Edit-budget modal for a month that already had one, only from the empty
state's templates — a household could create a budget but never change it
again through the UI. The header's own "Edit budget" button
(`openEditBudget` in `BudgetPage.tsx`) now normalises the month's existing
`budget.expectedIncomeMinor`/`lines` into the same prefill shape a template
produces, exactly as `BudgetModal.tsx`'s own header comment anticipated this
task would. Task 15 also shipped Budget history: a "History" button next to
the ‹ › picker opens `BudgetHistoryModal.tsx`, fetching `GET
/budgets/history?months=6` only while open (`useBudgetHistory.ts`, gated the
same way `useBudget.ts`'s own prevMonth query is). Three summary cards (avg
monthly spend, avg saved/month, months under budget) are computed over
**closed, budgeted** months only — the current month (still "so far") and any
closed month with every cap removed are excluded from all three, not
zero-filled into the average. Clicking a row switches the page's own month
state and closes the modal — the design's "full breakdown" is the Budget
screen itself, not a second view inside the modal. **Export CSV, drawn in the
design's mockup, is deferred and never implemented, not merely hidden** —
`apiFetch` is the only way the frontend talks to the server and throws on an
ok response it cannot parse as JSON, so a CSV download needs its own
non-JSON response path, its own guard and its own test (the transactions
spec's decision 7, restated for Budget by decision 10 of the Budget spec) —
which is why the row above is 🟡 rather than ✅ even though every other part
of History works end to end. **"Roll unspent into savings" ships as a manual button, not the design's
automatic toggle.** A closed month with `Remaining > 0` and no stamp shows
`BudgetRolloverCard.tsx`'s own offer — "S$1,780 unspent in July · Move it
into a goal" — opening a picker of the household's primary-currency,
unarchived goals; after the move the card names where the money went and the
button is gone (Goals spec decision 4). This is deliberately not the
design's toggle: a setting labelled "Roll unspent into savings" that acts
only when clicked would read as automatic, the exact dishonesty Budget spec
decision 1 already refused for this same feature — nothing here runs on a
clock. **The gap this row's 🟡 names is that automatic month-end rollover
specifically does not exist**, not the manual move, which is complete
end-to-end including its own owner-ruled edge case: `Remaining` excludes any
expense with no available exchange rate, so on a month with exclusions the
true unspent figure can be lower than what the button moves. The card names
the excluded count next to the button whenever it is non-zero (commit
`8a1114b`) rather than blocking the move — see `docs/SYSTEM_DESIGN.md` §5's
Goals flow for the ruling in full.

**The Unattributed row has no mockup of its own — the design's Spending by
person chart only ever draws named members.** It exists because a
transaction can be payer-less, and until Bills shipped that was rare enough
that the grouping (`usecase/budget.go:252`) silently dropped it rather than
showing a row for it. A bill's own `paid_by_membership_id` is optional the
same way, and autopay makes a payer-less expense the common case rather than
the exception — a household paying several bills by autopay would otherwise
watch its budget's by-person total quietly under-count the very spend the
screen exists to show. Bills' own Task 8 built the row before Bills itself
shipped, since the gap was reachable by hand-entering a payer-less
transaction even before any bill could create one.

**Goals**

| Feature | State |
|---|---|
| Savings goals with progress and funding source | 🟡 *(no funding-source account — dropped deliberately, spec decision 6)* |
| Monthly contributions summary | ✅ |
| New goal (modal) | ✅ |
| Goal contributions — add, delete, list by source | ✅ |
| Archive and restore a goal | ✅ |

**A household can set a goal, track it and move money into it end to end
now.** A goal carries a name, a target amount and currency, an optional
target date, and a planned-monthly figure; progress is a `goal_contributions`
ledger, not an account balance — a contribution moves no real money, and
nothing reconciles a goal's progress against any account
(`docs/SYSTEM_DESIGN.md` §5). The New/Edit goal modal (`GoalModal.tsx`,
Task 12) creates and edits a goal, with a live suggested-monthly figure
recomputed from whatever is currently typed; currency is settable only at
creation; the modal is wired into the Goals page at three entry points
(empty state, a card, "+ New goal"), closing a plan gap the Task 12 review
found where no task had actually mounted it. The contributions panel
(`GoalContributionsPanel.tsx`, Task 13) adds a manual contribution and lists
a goal's history by source (manual, starting balance, budget rollover), with
delete behind an in-page confirmation, never `window.confirm`. Archive hides
a goal behind a "Show archived" view with restore, the Accounts pattern; a
goal is never deleted, because contributions reference it and a rolled-over
budget month may name it.

**"Archive and restore a goal" was ticked ✅ before it was true, and this row
is the correction.** Every layer under archiving existed — column, repository,
service, `POST /goals/{id}/archive`, a `useGoals.archiveGoal` mutation with a
passing test — and **no screen ever called it**, so "Show archived" and every
card's Restore button led out of a state no household could enter. The Task 18
browser walk found the dead end at criterion 12 and fixed it with an Archive
button on every live card (`GoalCard.tsx`), mirroring `AccountRow`'s own
either/or. The row is ✅ now because a household can actually do it; the lesson
that a row describes what a household can do rather than what the stack can
serve is `docs/LEARNING.md` pattern 15.

**"Savings goals with progress and funding source" is 🟡 for one named
reason: the design's "Fund from" account select does not ship.** Under the
decision that a contribution moves no real money, an account link would
drive no figure and decorate a screen whose whole job is to be believed
(spec decision 6). A household with several goals across two accounts loses
the record of which pot funds which goal — the accepted cost, returned for
free only if contributions ever become real transfers. Everything else the
row promises — progress, target, status — is built.

**Two rows here have no mockup of their own**, the same shape Accounts' own
archive/restore and Transactions' own category rows take: the design's
Goals screen draws cards and a contributions summary, never the add/delete
controls or the archive view that make either one real, so both are counted
as their own rows rather than folded silently into the cards above them.

**Bills**

| Feature | State |
|---|---|
| Due-soon and paid-this-month timeline | ✅ |
| Autopay status | ✅ |
| Subscriptions summary | ✅ |
| Add bill (modal) | ✅ |
| Undo a payment (no mockup — see below) | ✅ |
| Archive and restore a bill (no mockup — see below) | ✅ |

**A household can add a bill, see it come due, mark it paid and undo that if
it was a mistake — sixteen tasks deep, code-complete and reviewed clean, and
now walked in a browser too: Task 18's 15-criterion walk (2026-08-10) passed
15 of 15, after one real defect found at criterion 14, fixed, and swept for
the class rather than stopped at the one instance — `GET /bills` is
money-AND-owner-gated exactly like `GET /goals`, but `BillsPage.tsx` answered
every failure, a routine "you're not the owner" 403 included, with the same
red `bills-load-error` alert a genuine server error gets, where
`GoalsPage.tsx`'s own copy already distinguishes the two. Fixing it and
re-reading `router.go`'s own comment — which names the whole money-AND-owner
group in one sentence, not Bills alone — found the identical gap already
sitting in `BudgetPage.tsx` and `TransactionsPage.tsx`, neither of them a
Bills file. All three fixed to mirror `GoalsPage.tsx`'s `goals-owner-only`
branch exactly (`bills-owner-only`/`budget-owner-only`/
`transactions-owner-only`, each with its own copy), pinned by six tests in
`GoalsPage.test.tsx`'s own pair-shape (two per page) and mutation-checked
independently. See
`docs/superpowers/plans/2026-08-09-hearth-bills-verification.md` and
`docs/LEARNING.md` pattern 1.** The Bills page
(`/money/bills`) lists
live bills split into Due soon and Later (both server-computed, so the rule
lives in one place), a Paid this month list, and an Archived view; the
Add/Edit bill modal sets name, amount, cadence, due date, pay-from account,
optional category and payer, autopay and "counts as a subscription"; Mark
paid writes a real expense transaction into the ledger — the currency comes
from the pay-from account, the identical rule `TransactionService.Create`
applies, and a test pins the two agreeing — and its undo reverses the
expense, the payment record and the advanced due date together, refusing to
undo anything but a bill's most recent payment. The Subscriptions panel
totals what autopay-and-subscription bills cost monthly and annually,
converting like every other cross-currency sum and naming what could not
convert rather than dropping it. A bill due on the 31st survives every short
month: `next_due` clamps to the destination month's real length off a
stored anchor day, not the date it last advanced from, which is what stops a
one-way clamp walking the bill off the 31st for good (`docs/SYSTEM_DESIGN.md`
§5).

**Two rows here have no mockup of their own**, the same shape Accounts' and
Goals' own archive/restore rows take. **Undo a payment** is Bills'
equivalent of deleting a transaction or a goal contribution — the design
draws no such control because it draws no history of past payments at all,
only the current due date. **Archive and restore a bill** is the identical
"never delete, stamp instead" pattern every other Money entity already
uses; unlike Goals' own archive control, which shipped with every layer
built and no screen that could reach it (`docs/LEARNING.md` pattern 15),
Bills' Archive button is wired onto every live row from the same commit
that built the list, so there is no equivalent dead end to find here.

**Every derived figure the design shows anywhere in Money is now pinned and
built.** Net worth from assets minus liabilities (Accounts); `66% used`,
`S$137/day left` and `on pace to save S$1,780` — all Remaining-based rather
than a run-rate projection — in the formula table of
`docs/superpowers/specs/2026-07-30-hearth-budget-design.md` (decision 2);
`X of Y on track` plus the manual move of unspent budget into a nominated
goal, in the formula table of
`docs/superpowers/specs/2026-08-01-hearth-goals-design.md`; and Bills' own
three — the due-soon/later split, autopay's badge and copy, and the
subscriptions monthly/annual totals — in its own spec's formula table.
Nothing in Money is left where an implementer would be inventing a figure
with no decision recorded first.

## 6 · Marriage

Parents-only throughout. Reachable again as of task 10: `/marriage/retros`
exists, guarded by `requireCapability(marriage)` stacked on `requireOwner`
(spec decision 11 — redundant with `domain.ValidateMembershipChange` today,
kept so the route does not lean on an invariant enforced only one layer
down), with its own `SPACE_PAGES` entry in `Sidebar.tsx` and a real page
(`RetrosPage.tsx`) behind it — the route, the sidebar entry and the page all
landed in the same change, `110ab0a`'s own condition for adding any of the
three back (splitting them across tasks would leave a route with no way to
reach it, or a sidebar link to a route that 404s).

**Retros is now whole, its own last two rows shipped in Tasks 12–14.**
`RetroDetail.tsx` (Task 12) is the selected month's full detail — went well
and was hard as the design's own one-bullet-per-line cards, notes, and the
actions list with each row's real, keyboard-focusable checkbox (not an
`sr-only` stand-in — `docs/LEARNING.md` pattern 3 is the reason given in the
component's own header comment) and per-assignee initial circles. The tick
writes through `useRetro`'s `setActionDone`, which sends `{ done }` alone and
never the retro's own `version`, precisely so one partner ticking an action
any time in the month can never collide with the other's open editor
(decision 6). `RetroModal.tsx` (Task 13) is the Start/Edit modal — mood,
the two textareas, `MoneyCheckInPanel`'s live budget-and-goals read (decision
3, stored nowhere), and a "+ Add an action" composer plus a "Still open from
July" carry-over offer (Task 14; the composer closed a plan gap — see
`docs/LEARNING.md` pattern 15's third instance) — reachable through the
`retro-detail-mount` `RetrosPage.tsx` names. Each task drove its own change
in a real browser before calling it done — Task 12's own Tab-only keyboard
walk over the action row's real checkbox, Task 13's walk, which is what
*found* the last-write-wins conflict defect `docs/LEARNING.md` pattern 3
records, and its reviewer independently clearing the mood picker's focus
ring — and **the feature's own fifteen-criterion browser walk (Task 17) has
now run and passed, 15 of 15** (2026-08-18), the same bar every Money
feature was held to before its rows could read plain ✅ — confirming the
rows above, three of them (criteria 2, 10 and 13) by an interpreted path
named in the record rather than the criterion's most literal reading, the
same way Bills' walk did for Bills, recorded in
`docs/superpowers/plans/2026-08-16-hearth-retros-verification.md`.

**No product defect needed a code fix. Two divergences from the design
spec's own prose came out of it, both judged, after review, not to need
one.** **A finished retro's delete is refused with a generic `404`,
`"That could not be found."`, not the design spec's own named copy**
("That retro is finished and cannot be deleted") — the refusal itself is
real and correctly wired (`RetroRepository.DeleteDraft`'s `WHERE
completed_at IS NULL` answers the identical `ErrNotFound` a genuinely
missing retro gets, on purpose, so "there is no draft here" reads the same
either way — `retro_handlers.go:351-360`'s own comment explains why). And
**a limited member who types `/marriage/retros` never actually reaches
`RetrosPage.tsx`'s own owner-only branch** — `RequireCapability.tsx`
redirects them to `/` before the page ever mounts, and since a limited
member can never hold `marriage` at all — `limited_members_have_no_marriage`,
a database `CHECK` constraint, `docs/LEARNING.md`'s Task 7 entry — anyone
who *does* pass that guard is already an owner, so the branch is
unreachable through the app today. Both stay shipped as built: the branch
is defence in depth, not dead code (`router.go:292-299`'s own comment on
the group), and the delete refusal's own contract (`ports.go`) would need a
new domain error to carry the distinction the spec's copy wants — a
cross-layer change on the strength of a walk's own disagreement with a
decision the code already explains, not a three-line fix. The same shape
Bills' own walk accepted for its own third member state, Noor.

**`110ab0a` deleted `/marriage` and `/marriage/$` along with the placeholder
page they rendered**, because a navigation row whose only content was the
sentence "Arriving in slice 3" reads as a broken product rather than an
honest one — worth keeping on record here since it explains why Marriage had
no route at all between that commit and task 10, not because the feature
regressed. One known minor gap task 10 left rather than fixing speculatively:
`marriageGuardRoute` had one child (`retros`) and no index route, so a caller
who typed bare `/marriage` by hand matched a real route (unlike before, when
it 404'd) and saw the sidebar shell with a blank content area instead of a
page. **The Vision spec's task 11 closed that gap** the moment Marriage grew
a second page to redirect to: `marriageIndexRoute` (path `"/"`, `beforeLoad`
throwing `redirect({ to: "/marriage/retros" })`) now sends bare `/marriage`
to Retros, the same "first page wins" choice `moneyIndexRoute` already made
for Money.

| Feature | State |
|---|---|
| Retro history with mood | ✅ |
| Mood chart over 12 months | ✅ |
| Single retro view — went well, was hard, actions, notes | ✅ |
| Start retro (modal) with mood, money check-in and actions | ✅ *(the design's own "45 min" duration is drawn, not built — spec decision 8)* |
| Carry an unfinished action into the next retro (no mockup — see below) | ✅ |
| Delete a draft retro (no mockup — see below) | ✅ |
| Vision — yearly theme | ✅ *(`VisionPage.tsx`'s theme hero, Vision spec's task 11 — the year label, the theme in literal quotes, and its description; an empty description renders nothing, not an empty block. **Per-year history is real, not merely stored**: `visions` keeps one row per `(household_id, year)` forever (spec decision 4), and a household genuinely can reach a past year — the modal's own year select (`vision-modal-year`) is the only affordance that changes which year this page renders, and it writes back to `VisionPage`'s own `year` state, so the same mounted `useVision(year)` call reloads on the SAME page rather than the modal alone. The select offers only the previous, current and next calendar year, anchored on today, so 2025 or 2027 is reachable through the UI today but 2019 is not — the server accepts any year in `MinVisionYear`–`MaxVisionYear`, so that narrowing is a UI choice, not an API limit, and a page-level year picker of its own would be a small, real addition rather than a gap in what already exists)* |
| Vision — marriage duration beside the theme ("Married · 14 years · Feb 14, 2012") | ⬜ *(drawn, deliberately not built — Vision spec decision 2. Nothing in this product stores a wedding date, no feature would read one, and the only derived figure is today minus the date; building it costs a column, a modal field the design itself never draws, a null state, a visibility decision and a leap-day edge, for no behaviour. The theme hero renders full width instead. Same treatment as the design's drawn-but-unbuilt "45 min" retro duration, Retros decision 8)* |
| Vision — pillars with measures | ✅ *(`PillarCard.tsx`, Vision spec's task 11 — numbered "Pillar 1", "Pillar 2"…, name, description and every measure. A measure with `hasFigure: false` (a linked goal deleted, a link that failed to resolve, or an unrecognised kind) renders its label and no number at all, never "0 of 0" or "0%" — the same "blank the figure and say why" rule Accounts applies when a primary-currency change leaves net worth uncomputable)* |
| Vision — longer-horizon milestones | ✅ *(`MilestoneGrid.tsx`, Vision spec's task 11 — one card per milestone, in order, with year, title and note; an empty note renders nothing. The dashed "+ Add milestone" tile opens the Edit-vision modal (Vision spec's task 12), the same as the header's own Edit vision button)* |
| Edit vision (modal) | ✅ *(`VisionModal.tsx`, Vision spec's task 12 — the whole-document editor: theme, a year select offering only the previous/current/next calendar year, description, every pillar's name, description and measures, and every milestone, saved together in one `PUT`. Adds the two fields the design's own modal never drew at all (spec decision 7) — a pillar's own description and a measure editor per pillar (a label, then either a typed current/target pair or a linked-goal picker, never both; switching modes clears the other's inputs rather than leaving a hidden stale value that would still submit). All three of `onEdit`'s call sites open it — the header's Edit vision button, the "+ Add milestone" tile, and the empty state's own call to action, the one every household with no vision yet sees first. A stale `version` (409) latches a one-way conflict banner decided from the response's own error code (RetroModal.tsx's precedent, for the same staleness reason); its only action reloads the year and discards the local draft outright, rather than trying to resume editing in place)* |
| Agreements by section | ⬜ |
| Agreements empty state with starter sets | ⬜ |
| Propose a change — add, edit, remove (modal) | ⬜ |
| New agreement section (modal) | ⬜ |
| Version history (modal) | ⬜ |

**The first two rows landed in task 11**, both reading off the one `GET
/retros` fetch `RetrosPage.tsx` already makes — neither adds a request of its
own. `RetroHistoryList.tsx` groups by year, current year expanded, older
years collapsed behind the design's own "Show 2025 (7 more)" disclosure; each
row renders only the clauses its retro actually has, mood, action count and
quote each independently omitted rather than shown as "0" or empty quotes
(task 10's own stand-in got the action-count guard wrong — `docs/LEARNING.md`
pattern 2 has that entry). `MoodChart.tsx` is inline SVG, no charting
dependency: a month with no finished retro, or a finished one with no mood,
breaks the line rather than drawing a zero, verified in a real browser
against three simultaneous gap sources at once (a month with no retro row at
all, a finished retro with `mood: null`, and the current month's own
in-progress draft).

**Two rows here have no mockup of their own**, the same shape Accounts'
archive/restore and Goals' contributions rows already take. **Carry an
unfinished action into the next retro** is `RetroModal`'s "Still open from
July" offer (decision 4): clicking one of the previous month's unticked
actions posts a fresh `addAction` with `carried_from` pointing at the
original, July's own row untouched and still unticked; only the immediately
previous month is ever offered, and the label "Carried from {month}" is true
for every path the product can produce, since the offer only ever lists
`OpenInMonth`'s own server-side answer. **Delete a draft retro** was, as of
this same round's own first pass, the one row on this page that was not
simply true — `docs/LEARNING.md` pattern 15's shape a fourth time in the
same feature, every layer below the screen real and the screen never asking
for it — and moved to ✅ in the round's own fix pass: `RetroModal.tsx` now
offers a "Discard draft" control, shown only for a draft (`completedAt ===
null`; a finished retro offers nothing, since the server refuses one and
an offer that always fails is worse than no offer), confirmed in the page
rather than with `window.confirm`. `removeAction`
(`DELETE /retros/{month}/actions/{id}`) has the identical shape and stays
deliberately unbuilt — no mockup draws a delete control on an action and no
task's brief ever named one, so this is a decision, recorded in
`docs/LEARNING.md`, not an oversight left for the next reader to rediscover.

Agreements are the unusual one: every change goes through **propose → both
sign**, and history is preserved so a removed agreement can still be seen and
restored. That is append-only and versioned, not ordinary CRUD.

## 7 · Family

| Feature | State | Notes |
|---|---|---|
| Shared month calendar with per-person filters | 🟡 | **The admin-surface branch (2026-09-02) gave this an API stub and a flag, nothing else.** `GET /api/v1/family/calendar` exists, gated behind `domain.FlagFamilyCalendar` (default off) — turning the flag on answers `200 {"events":[]}` rather than 404, walked directly against the running API. It exists to prove dark-shipping works before the feature is needed in anger, per the flags spec's own words: nothing writes an event, and no page or route reads the endpoint from the browser. Still needs Bills' own dependency satisfied (bill dates on the grid) plus the real page and the write side |
| New event (modal) | ⬜ | |
| Kids view | 🚫 | The design marks it "· not built" |

**Family has no routes any more, and no sidebar entry**, for the same reason
Marriage does not (`110ab0a`): `/family/calendar` and its "Arriving in slice
4" placeholder were deleted together. Whoever builds the calendar adds the
route back and the `SPACE_PAGES` entry in the same change. One difference
from Marriage: Family carries **no** required capability — `domain.BuiltinSpaces`
makes it unconditional — so its route sits directly under the shell with no
`RequireCapability` wrapper, exactly as it did before. Settings still lists
Family as a space for everyone. The new API stub above has no route mounted
in the frontend at all — reaching it today needs a raw request against the
running API, not a click anywhere in the product.

## 8 · Household extras

| Feature | State | Notes |
|---|---|---|
| Custom space page — landing and "Add page" | 🚫 | The design marks it "· not built". Creating a space today adds the sidebar entry only |

## 9 · Platform administration

**Not in `design/Household Dashboard.dc.html` at all**, and never will be — this
is the operator's own surface for running the install, not a household
feature. Counted here anyway, the same way Bills' "Undo a payment" and Goals'
"Archive and restore a goal" are counted without a mockup behind them: a row
that exists and works is on the map whether or not the design ever drew it.
See [ADR 5](adr/0005-platform-admin-authorization.md) for why the surface is
shaped the way it is, and
`docs/superpowers/plans/2026-09-02-hearth-admin-surface-verification.md` for
the browser walk the ✅ and 🟡 rows of that first slice cite. **One ✅ below
cites a different walk**: "Households and metrics", built later on
2026-09-02, names
`docs/superpowers/plans/2026-09-02-hearth-admin-households-verification.md`
in its own cell as the file its walk was recorded in — 15 of 15 criteria pass,
with two caveats (see that row).

**The two ⬜ rows below, the audit screen the owner descoped on 2026-09-02,
and households and metrics, built the same day, were all deliberately out of
scope for the slice that built the rest; the two that remain are now the
product's next work** (see "Suggested order").
They were described in prose here rather than given rows until 2026-09-02, on
the reasoning that unbuilt-and-unplanned work is not on the map. That
reasoning expired the moment they were prioritised: a feature nobody has a row
for is a feature the tracker cannot be asked about. Each cites the section of
`docs/superpowers/specs/2026-09-01-hearth-admin-surface-design.md` that already
specifies it in full — none of these needs a fresh design, only a plan.

| Feature | State | Notes |
|---|---|---|
| Platform admin identity and re-authentication | ✅ | `adminctl grant-platform-admin` / `revoke-platform-admin` are the only way a `platform_admins` row is created or removed — verified by grep, not assumed: `PlatformAdminRepository.Grant` has exactly one call site outside test code, `runGrantPlatformAdmin` in `api/cmd/adminctl/main.go`. There is no HTTP route and no self-promotion path. Entering `/admin` costs the password again regardless of session age; a correct re-entry stamps `sessions.admin_grant_expires_at` thirty minutes out and is cleared by sign-out for free — not extended by activity, unlike the session itself. Failed attempts count against their own ledger, `admin_reauth_attempts`, never the household-scoped `login_attempts`: three wrong admin passwords lock the admin surface with `423` while the same browser's household sign-in keeps answering `200`, walked directly (criterion 6). **Two rough edges found on the walk, neither a criterion failure:** the wrong-password screen shows sign-in's own copy, "That email or password is incorrect.", on a screen with no email field; and a locked surface still shows the ordinary password prompt on reload rather than a lockout message — the lock is discoverable only by submitting, and a submission made while still locked extends it (ADR 5's accepted limits) |
| Admin audit log | ✅ | Every request that reaches `auditAdmin` writes one `admin_audit_log` row before the handler runs, reads included — middleware, not a per-handler call, so a handler that forgets is not a failure mode that can happen. `auditAdmin` sits behind `requirePlatformAdmin` in the chain, so a non-admin's or an unauthenticated request never reaches it and is never logged; the log is complete for the surface it can see, not for every request the path `/admin/*` receives. Append-only: no delete route anywhere in the product, and `adminctl prune` does not touch it. Walked directly (criterion 14): the row count grew by exactly one for a single page view |
| Feature flags — registry, resolution and enforcement | ✅ | Four flags at launch (`signups_open`, `telegram_sign_in`, `notification_delivery`, `family_calendar`); a household override beats a global override beats the compile-time default, resolved fresh on every authenticated request rather than cached. `requireFeature` answers `404`, not `403`, for a disabled route — on pre-auth paths too, where it resolves the global set only, so a household override can never apply before a household is known. Walked directly (criteria 9–12): turning `family_calendar` on globally opened `GET /api/v1/family/calendar`; a household override closed it for that household alone; deleting the override reopened it, proving "no opinion" differs from "explicitly off"; turning `signups_open` off answered `404` on both the public sign-up form and its token-completion route together, and restored both when turned back on |
| Admin flags screen (`/admin/flags`) | 🟡 | Lists every flag with its description, compile-time default and current global state, and toggles the global value. **Two gaps found on the browser walk; the final fix wave (2026-09-02) closed the accessible-state half of the second, the rest remain open:** there is still no control to *create* a household override — the per-household state the flags model exists to support (a global default with a per-household exception) is reachable only by a hand-written `PUT`, never by a click. **The household list a picker needs now exists (`/admin/households`, 2026-09-02); the control itself does not** — the product owner chose to build support lookup and metrics first (households spec §1), so what was "there is no list to pick from" is now only "nobody has wired the picker to the list". The screen's only interactive control — a segmented On/Off toggle — is still 12px muted-grey text on a transparent ground, right-aligned roughly 1900px from its own label, and did not register on a first read of the screen at all, confirmed only through the accessibility tree; placement, not contrast, is the problem (a contrast check on the same text measured roughly 5.4:1, passing WCAG AA), and placement is out of scope for this wave. What the wave did fix: the "Default" segment (no override at all) used to carry its current-ness in background colour alone, with neither it nor a screen reader having any way to say which of the three states — Default, On, Off — was current, since it is a status span rather than a button and so never carried `aria-pressed` the way On and Off do. It now carries `aria-current="true"` whenever it is the flag's current state, so the three states are distinguishable to assistive tech even though the toggle is still hard to find. A green suite proved the underlying toggle worked; it could not have shown that nobody could find it |
| `adminctl` — `grant-platform-admin`, `revoke-platform-admin`, `list-platform-admins`, `unlock-admin` | ✅ | Four new commands, no UI — the same shape `Retention pruning (adminctl prune)` already has in Household settings, above. `unlock-admin` clears `admin_reauth_attempts` for one user, the admin surface's equivalent of a magic link; walked directly (criterion 7) |
| Admin audit screen (`/admin/audit`) | 🚫 | **Descoped by the product owner on 2026-09-02 — not the design's marking, the owner's decision.** It was built first: `GET /admin/audit?limit=N` in the granted group, `RecentAdminAudit` joined to `users` so rows named their actor, an `AdminAuditPage` with limit-only "Show more" to the service's 500 cap, and a Flags · Audit nav in `AdminShell`; every suite green, three mutation checks red, and a browser walk that found and fixed one defect (the active nav link was indistinguishable — see `docs/LEARNING.md`, Frontend). The owner then judged the screen unnecessary and asked for it to be removed rather than merged. The code is gone from the tree; the log is read through `psql` as before, and `AdminService.RecentAudit` stays in place for the tests that use it. The one thing kept is unrelated to the screen: `useAdminFlags` no longer refetches on window focus, because every refetch of an audited route is itself an audit row. A patch of the removed work was saved outside the repository at the time, but the honest statement is that reinstating it means rebuilding from the spec (§2.4, §7) |
| Read-only database browse | ⬜ | Design spec §4. **The piece originally asked for.** A separate `SELECT`-only Postgres role (`hearth_readonly`) reached through its own pool from `DATABASE_READONLY_URL`, so a mistake in the adapter's SQL still cannot write; table names validated against `information_schema` rather than interpolated; `statement_timeout` and a page cap; and redaction that fails closed by column *type* (`bytea`) as well as by name, so a secret column added next year is redacted before anyone remembers the rule exists. Unset config means the panel is unavailable — never a silent fallback to the read-write pool. **The only one of the four with an infrastructure dependency:** the role is created during provisioning, not by a migration, so `deploy/PROVISION.md` changes before the feature can run anywhere real |
| Outbound message inspector | ⬜ | Design spec §5. Proxies Mailpit's HTTP API rather than storing links: every token in this schema is stored hashed, and inventing a raw-link store to solve a convenience problem is the wrong trade. **Closes a live operational pain rather than adding a capability** — under [ADR 3](adr/0003-mail-stays-on-the-box.md) no mail leaves the box, so handing someone an invite today means opening an SSH tunnel to Mailpit and copying the link out by hand (`deploy/README.md`). Needs `MAILPIT_API_URL`; unset means the panel is unavailable. The message bodies contain working magic-link and invite URLs, so opening one is its own audit row |
| Households and metrics | ✅ | Design spec §6, expanded in `docs/superpowers/specs/2026-09-02-hearth-admin-households-design.md`. Four tiles, explicit search over households and members (Telegram-only members by name, since they have no email the operator could know), most-recently-active ordering from a throttled `sessions.last_seen_at`, and a read-only drill-in with members, channel, pending invites and the household's sign-in lockout. No money on either screen, asserted by exact key sets rather than by reading the handler — financial data stays behind the database browse, so reading a customer's finances costs a deliberate second step and a second audit row. Reads tables that already exist; no analytics table, because a counter that can drift from the rows it counts is worse than a query. **Walked 2026-09-02, Task 11 of the plan — 15 of 15 criteria pass, with two caveats** (`docs/superpowers/plans/2026-09-02-hearth-admin-households-verification.md`): criterion 7's caveat was a real defect — the "Nothing matches" message's own Clear button restored the list but left the search box showing the stale query — fixed in the same commit that recorded the walk; criterion 12 was confirmed against the drill-in's own lockout callout through the API, with the browser's admin session kept alive throughout, rather than against the sign-in screen's own local error state. **Named gap, not a criterion failure:** an expired, unaccepted invite is invisible on this screen by the spec's own "pending only" rule (both the metrics tile and the drill-in's "Pending invites" list filter on `expires_at > now()`) — an operator troubleshooting a stale invite still needs `psql` |

---

## Suggested order

**Reprioritised 2026-09-02 by the product owner: the four remaining
platform-administration features come before any further household work** —
two, now: the audit screen was built, walked and then descoped the same day
(its row in section 9 says why), and households and metrics was built later
that same day.
That is a preference, not a dependency, and it is worth naming as one — see
"what this costs" below. The order *within* each group is still dependency.

**First — finish the operator surface (section 9).** Both remaining items are
specified in full in
`docs/superpowers/specs/2026-09-01-hearth-admin-surface-design.md`; neither
needs a fresh design, only a plan. **The next one is item 3, the outbound
message inspector.**

1. ~~**Admin audit screen**~~ — **removed from the roadmap 2026-09-02.**
   Built and walked that day, then descoped by the product owner as not
   needed; the log stays readable through `psql`. Not deferred — cut. The
   numbering below is kept so earlier references to "item 2" still point
   at the same thing.
2. ~~**Households and metrics**~~ (§6) — **built 2026-09-02**, on branch
   `admin-households` and to its own spec,
   `docs/superpowers/specs/2026-09-02-hearth-admin-households-design.md`,
   which expands §6 of the admin-surface spec. Its browser walk ran the same
   day, Task 11 of the plan — 15 of 15 criteria pass, with two caveats;
   section 9's row carries them. It read tables that already existed, as
   predicted, plus one new column
   (`sessions.last_seen_at`). The flags screen's named 🟡 gap is *closer*
   rather than closed: the household list a per-household override picker
   needs now exists, the picker does not. Numbering kept, as for item 1.
3. **Outbound message inspector** (§5) — needs `MAILPIT_API_URL` and nothing
   else. Closes a *live* operational pain rather than adding a capability:
   under ADR 3 handing someone an invite means an SSH tunnel to Mailpit today.
4. **Read-only database browse** (§4) — last of the four despite being the one
   originally asked for, because it is the only one with an infrastructure
   dependency (`hearth_readonly` and `DATABASE_READONLY_URL` are provisioned,
   not migrated, so `deploy/PROVISION.md` changes first) and much the largest
   security surface. It is also the piece the other three de-risk: every one of
   them exercises the re-auth grant and the audit log under real use before the
   surface that can read every household's finances arrives.

**Then — the household product**, dependencies as before:

5. **Money's remaining 7 ⬜** — 25 of 35 rows are ✅; what is left is the tail,
   not the slice
6. **Marriage's remaining 6 ⬜** — 10 of 16 are ✅; Agreements is the
   interesting problem, and it is append-only and versioned, not CRUD
7. **Family** — the only genuinely untouched area. Calendar needs Bills for the
   bill dates on the month grid, and Bills is ✅
8. **Overview** — 8 of 11 already ✅ because it grew alongside Money; what
   remains only aggregates the areas above

**What this costs.** Sections 1–8 are the household product — what a customer
buys. Section 9 is the surface the operator uses to run the install; no
household member can see any of it. Prioritising it means the thing being sold
stands still while the thing that runs it improves. The case for doing it
anyway is real and it is why the reprioritisation was made: the install is
live, sign-up is self-serve and ungated, and three of these four exist to make
a real install operable by someone who is not sitting at its database. Family
is the household work most visibly delayed by this choice — it is the one area
with nothing built at all.

Each area gets its own spec → plan → implementation cycle. See `docs/HANDOVER.md`
for what to settle before the first task of the next one.

Each area gets its own spec → plan → implementation cycle. See `docs/HANDOVER.md`
for what to settle before the first task of the next one.
