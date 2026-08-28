# Vision & goals — verification walkthrough

**Result: 15 of 15 pass.** No product defect needed a code fix. One
criterion (8) was met by a deliberate, narrow substitute rather than its
most literal reading, named as such inline, for a reason that lives in a
*different* feature's own spec, not Vision's; two other things surfaced
during the walk and are recorded under "Findings, not defects" below rather
than treated as bugs, because both are documented, deliberate decisions
already sitting in the code.

**Addendum (2026-08-29, final whole-branch review):** criterion 7 was
re-walked against a non-zero percentage. The original run below is
preserved verbatim, since it is not wrong — only a weak witness: it
demonstrated a linked measure against a *zero*-contribution goal, and
`Current` is always 0 for a linked measure (`toVisionDTO`'s own shape), so a
regression swapping `Percent` for `Current` would still render `0%` and
pass unnoticed. The re-walk (same row, its own paragraph below) linked the
measure to a goal actually holding a balance and confirmed the Vision page
renders that non-zero figure, matching `/money/goals`. Still 15 of 15; this
addendum makes that criterion's own PASS a stronger one, not a different
one.

Run 2026-08-28/29 (host clock +08 crossed midnight mid-walk, reading
2026-08-29 04:54–05:16 for the bulk of the criteria; the API container is
UTC, reading 2026-08-28 20:54–21:16 for the same instants — the same
sixty-odd minutes, a different calendar-day label on each side, since +08
is eight hours ahead of UTC. Nothing in Vision is month- or day-scoped the
way Retros is, so unlike that walk's own preamble this is not load-bearing
for any criterion below — checked once, at the end, and stated here rather
than left to be misread from screenshot timestamps.) colima was the active
engine throughout (`colima status` answered "colima is running", containers
already up from an earlier session — `hearth-api-1`/`postgres`/`mailpit` "Up
2 hours", `hearth-web-1` restarted mid-walk, see criterion 3's own entry).
`goose_db_version` read `10` (`00010_vision.sql`, this feature's own
migration) before any criterion was walked.

**Claude in Chrome did not connect** — `tabs_context_mcp` answered "Browser
extension is not connected" on three consecutive attempts, the threshold the
task brief itself sets before falling back. Driven instead in Playwright's
own independent Chromium throughout, for every one of the fifteen criteria
including the two-window ones (11–12) and every width in 15 — the same
browser Retros' own walk used as its "second, genuinely separate browser"
for exactly the same class of check (`page.setViewportSize()` genuinely
changes `window.innerWidth`, confirmed again here: 305/305, 360/360,
768/768, 1440/1440 at criterion 15, no `resize_window` artifact this run
since Claude in Chrome was never reached).

**A stale dev server, on the very first criterion that opened the modal —
diagnosed, not treated as a product defect.** Opening Edit vision for the
first time, the "Pillars" heading and "+ Add pillar" button rendered as
genuine zero-width, zero-height elements —
`getBoundingClientRect()` on `[data-testid="vision-modal-add-pillar"]` read
`0×0`, and Playwright's own click timed out with "element is not visible."
`curl`ing the served module
(`http://localhost:5173/src/features/marriage/visionCopy.ts`) settled it in
one command: the entire `--- VisionModal (Task 12) ---` half of the file —
every string `VisionModal.tsx` needs beyond the page itself — was absent
from what Vite was serving, though `cat`ting the file on disk showed it in
full, current and correct. `docker compose restart web` fixed it in one
command; the served module then matched the file byte for byte, and every
criterion below was walked (or re-walked) against the restarted server.
This is the exact trap named in this task's own brief and recorded three
times already in `docs/LEARNING.md`'s "Tooling and infrastructure" section
— a fourth instance is added there, evidence under the existing pattern
rather than a new one, per this repo's own convention. Criteria 1 and 2
were first observed before the restart (both already true then — the
sidebar and the empty state render from `visionCopy.ts` fields the stale
serve happened not to be missing) and were re-observed after it; every
criterion from 3 onward was walked entirely against the restarted server.

From the state the household was already in (seeded, plus Christine's own
co-owner invite accepted and Jamie's limited-member invite accepted, both in
an earlier session — `make seed` was re-run at the start of this task and
reported both as already done): **Andreas** (owner, `calendar,chores,money,
marriage`) and **Christine** (owner, same capabilities) are the two parents;
**Jamie** (limited, `money` only, no `marriage`) is the household's one
limited member used for criterion 14, after this walk gave her a real
password (see that criterion's own entry — `adminctl reset-password` needs
a real controlling terminal and this environment has none, so `expect` drove
it). Two further limited members with no email on record (`calendar,chores`
and `calendar` alone) were already present from prior sessions and were not
touched. One savings goal, **Emergency fund** (target S$30,000.00, zero
contributions, reading 0% on `/money/goals`), existed at the start and was
used for criteria 7 and 8, ending the walk deleted (criterion 8's own SQL
substitute, see below) — `/money/goals` reads "No savings goals yet" as a
direct, visible consequence, not restored, the same kind of leftover
Retros' own record left its deleted August budget in.

Criteria are Task 15 of
`.superpowers/sdd/2026-08-28-hearth-vision/task-15-brief.md`, verbatim,
walked in numeric order except where noted inline.

---

## Criterion by criterion

| # | Criterion | Result |
|---|---|---|
| 1 | Signed in as an owner, the sidebar shows Vision & goals under Marriage, and it opens `/marriage/vision` | **PASS** — signed in as Andreas (`GET /api/v1/auth/me` confirmed `role: "owner"`, `capabilities` including `marriage`), the sidebar's own accessibility snapshot read a `MARRIAGE` group with `Retros` and `Vision & goals` links beneath it, `/url: /marriage/vision` on the latter. Clicking it landed on `http://localhost:5173/marriage/vision`. |
| 2 | A household with no vision for this year sees the empty state — an invitation to set the theme, not a grid of blank cards and not an error | **PASS** — before any vision existed, `/marriage/vision` rendered `data-testid="vision-empty-state"` reading "No vision set for 2026 / Set this year's theme, the pillars you're building it on and what you're both working toward." with `data-testid="vision-empty-cta"` reading "Set this year's vision". No error, no card grid. Screenshot `01-02-empty-state.png`. |
| 3 | Edit vision opens the modal, and its year select offers exactly last year, this year and next year — nothing else | **PASS, after diagnosing the stale-server issue above** — the modal's own `data-testid="vision-modal-year"` combobox listed exactly three `<option>`s: `2025`, `2026` (selected) and `2027`. Screenshot `03-modal-year-select.png`. |
| 4 | Saving a theme and description renders the hero, theme in quotes at full width, and no marriage-duration block | **PASS** — saved theme `Slow down together` and a description; the hero (`data-testid="vision-hero"`) rendered `"Slow down together"` (literal quote marks in the text node, not CSS) at full card width with the description beneath it, and no second column, no "Married · N years · date" block anywhere in the DOM or any screenshot at any width this walk took. Visible in every full-page screenshot from `04-07-09-vision-page-full.png` onward. |
| 5 | Adding a pillar with a description and two typed measures renders Pillar 1, its description and both measures with their N of M figures | **PASS** — added pillar "Us time" with a description and two typed measures. Rendered: `PILLAR 1` / `Us time` / the description, then `Date nights / month  2 of 2 ✓` and `Phone-free dinners / week  1 of 4`, both `data-testid="vision-measure-row"`. Screenshot `04-07-09-vision-page-full.png`. |
| 6 | A measure at its target shows the met marker; one below target does not | **PASS** — `Date nights / month`, saved current 2 / target 2, rendered `2 of 2 ✓` with the accent-coloured, semibold styling `MeasureRow`'s own `measure.met` branch applies. `Phone-free dinners / week`, saved current 1 / target 4, rendered `1 of 4` with no `✓` and the plain-weight styling. Both read directly from the DOM text, not eyeballed from a screenshot alone. |
| 7 | Adding a measure linked to a savings goal renders that goal's percentage, and it matches what /money/goals shows for the same goal | **PASS, re-verified against a non-zero percentage in the 2026-08-29 whole-branch-review addendum — see that paragraph below the original entry.** Original entry (2026-08-28/29, preserved verbatim): PASS — added a measure "Emergency fund" linked to the seeded `Emergency fund` goal (Track by → "A savings goal" → Goal → "Emergency fund"). Vision rendered `Emergency fund  0%`. `/money/goals` independently rendered `0% Emergency fund S$0.00 of S$30,000.00` for the same goal — the two figures match exactly, both derived from the same zero-contribution state (`domain.GoalProgressPercent(0, 3000000) = 0`). **This PASS is real but a weak witness**: `Current` is always 0 for a linked measure (`toVisionDTO`'s own shape, `api/internal/usecase/vision.go`), so a regression that swapped `Percent` for `Current` in that same function would still render `0%` here and this criterion would still read PASS — the zero-contribution figure cannot tell the correct field from the wrong one. **Re-walk (2026-08-29, final whole-branch review, non-zero)**: by this point the original seeded `Emergency fund` goal had been deleted (criterion 8, this walk's own second half) and its Vision measure reset to a typed `0 of 1` (Finding 2, "Findings, not defects" below), so a fresh goal was created rather than reusing either — `/money/goals` → "Create your first goal" → name `Emergency fund`, target `S$1,000.00`, starting balance `S$500.00`, no target date, planned monthly `S$0`. `/money/goals` rendered `50% Emergency fund S$500.00 of S$1,000.00` for it (screenshot `07-revisit-goals-50pct.png`). Opened Edit vision, switched the `Emergency fund` measure's Track by to "A savings goal", chose the new `Emergency fund` goal, and saved — `PUT /api/v1/marriage/vision/2026` answered `200` on the wire (`browser_network_requests`). `/marriage/vision` then rendered `Emergency fund  50%` (screenshot `07-revisit-vision-50pct.png`) — the same non-zero figure `/money/goals` shows for the same goal, and a figure `0%` could not have come from. This is the discriminating case: only a live read of `Percent` (not `Current`, which stays 0) produces `50%` here, so this re-walk is the evidence a `Percent`/`Current` swap in `toVisionDTO` would actually fail, closing the gap the original zero-contribution run left open. |
| 8 | Delete that goal from /money/goals. The delete must succeed — not error — and the Vision page must then show that measure's label with no number. This exercises the database constraint branch that would otherwise make deleting a goal fail inside the Goals feature | **PASS, by a substitute the task brief's own instructions anticipate — see below** — Goals has no delete affordance and no `DELETE` route at all, by a *different* feature's own deliberate decision (Goals' spec: "Goals are **not** deleted and have no `DELETE` endpoint"; `GoalRepository` has no `Delete` method; `router.go` wires no `DELETE /goals/{id}`). Before touching the goal at all, checked for a blocking reference (`select id, rollover_goal_id from budgets where rollover_goal_id is not null` → 0 rows, so no `NO ACTION` FK would block the delete). **First, through the real product**: clicked Archive on `/money/goals` — the goal left the live list, and reloading `/marriage/vision` still read `Emergency fund  0%`, unchanged — confirming decision 8's other half ("archiving is not deletion") through an actual affordance before touching SQL. **Then the delete itself**: `DELETE FROM goals WHERE name = 'Emergency fund'` against the running Postgres container — `DELETE 1`, no constraint violation of any kind, the CHECK's third branch (`measure_is_typed_or_linked`'s `goal_id IS NULL AND target_value IS NULL AND current_value IS NULL` arm) absorbing the referential `SET NULL` exactly as spec decision 8 describes. `select label, goal_id, current_value, target_value from vision_measures` immediately after read the `Emergency fund` row **still present**, `goal_id` now empty, both values still null — the row survived, only the link broke. Reloading `/marriage/vision` in the browser then read `Emergency fund  Goal removed` — the exact `measureFigureUnavailable` copy, no number anywhere near it. Screenshot `08-goal-removed.png`. |
| 9 | Adding three milestones renders them in order under Longer horizon, each with its year, title and note | **PASS** — added, in order, `2026 / Weekend in Bali / Just the two of us, no kids.`, `2027 / Renovate the kitchen / Tied to the house fund.`, `2028 / Ten years married / A trip somewhere we've never been.` All three rendered under `Longer horizon` in the same order, each `data-testid="vision-milestone-card"` carrying its year, title and note. Screenshot `04-07-09-vision-page-full.png`. |
| 10 | + Add milestone opens the same modal | **PASS** — closed the editor, then clicked the page's own dashed `data-testid="vision-add-milestone"` tile in the Longer horizon panel (distinct from the modal's own internal "+ Add milestone", used to add rows once already inside). It opened the identical `Edit our vision` dialog — same title, same privacy badge, same theme/description/pillars already populated — not a second, milestone-only form. Screenshot `10-add-milestone-opens-modal.png`. |
| 11 | Two windows, both signed in as the same owner, both with the modal open on the same year: the second save is refused with the reload message, and after reloading the first partner's pillars are still there | **PASS, all parts checked on the wire, not just the screen** — two genuinely separate Playwright tabs (`browser_tabs`, same browser process, same cookie jar — the literal "same owner" the criterion asks for, not two different people), both signed in as Andreas, both opened Edit vision on 2026 while the vision stood at `version: 2` (confirmed by direct SQL before either tab acted). **Tab A** edited the theme to `Slow down together (edited by A)` and saved first — `select version from visions` read `3` immediately after, confirming the write landed. **Tab B**, still holding its stale `version: 2` draft, edited the description and clicked Save: `browser_network_requests` showed `PUT /api/v1/marriage/vision/2026 => 409 Conflict` on the wire, and the DOM grew `data-testid="vision-conflict"` reading "Someone else saved this year's vision while you were editing. Nothing has been sent -- but reloading will discard the changes you made here and show their version instead." with Save vision now `disabled`. Screenshot `11-conflict-banner-tabB.png`. Clicking `data-testid="vision-conflict-reload"` closed Tab B's modal and re-rendered the page from the fresh document: all three of Tab A's pillars — `Us time` (with its three measures), `Family time` (one measure), `Growth` (no measures) — were present and unchanged, nothing lost. Screenshot `11-after-reload-pillars-intact.png`. One incidental effect of Tab A's own save is recorded under "Findings, not defects" below, not folded into this PASS since it is a separate, already-documented behaviour, not part of the conflict mechanism this criterion tests. |
| 12 | The same on a year that had no vision: both windows open the empty modal, and the second save is refused rather than overwriting | **PASS** — switched both tabs' own year select to **2027** (in the select per criterion 3, and confirmed to have no row yet). Both modals opened blank — `data-testid="vision-empty-state"` visible behind each, both holding `version: 0`. **Tab A** set theme `New chapter` and saved: `select year, version, theme from visions` read `2027 | 1 | New chapter` — the create succeeded, landing at version 1 exactly as spec decision 10 describes. **Tab B**, still on `version: 0`, set theme `Fresh start` and saved: `PUT /api/v1/marriage/vision/2027 => 409 Conflict` on the wire, the identical conflict banner rendered. Screenshot `12-conflict-banner-fresh-year.png`. `select year, version, theme from visions` afterward still read `2027 | 1 | New chapter` — Tab B's `Fresh start` never landed; the first-save race decision 10 names explicitly was refused, not silently overwritten. |
| 13 | The Overview shows the Vision 2026 card with one line per pillar (each its first measure) and the Vision check-in strip inside the Next-retro card. A household with no vision sees neither | **PASS, both halves, the negative half re-verified twice — see below** — after saving a vision with three pillars (`Us time` → two measures, `Family time` → one measure, `Growth` → none, added specifically to exercise both the `position`-order rule and the no-measures fallback the spec's formulas table names, not just render a card with one trivial line), `/` showed both surfaces: `data-testid="vision-card"` read, in order, `2026 theme / "Slow down together" / Date nights / month  2 of 2 / Family outings / month  1 of 2 / Growth` — pillar 3's own name standing in for its absent measure, exactly the formula's fallback — and the Next-retro card's own `data-testid="vision-checkin-strip"` read `Vision check-in: 2026 theme — "Slow down together"`. Screenshot `13-overview-with-vision.png`. The linked Emergency-fund measure sits at pillar 1's position 2 (not position 0), so its later deletion (criterion 8) should never touch either Overview surface — checked directly, not assumed from the position rule alone: reloading `/` well after that deletion (after criterion 11's own whole-document save had since landed too) still read `Date nights / month  2 of 2`, unchanged — surviving both the goal's deletion and a full re-save of the document. **The negative half** ("a household with no vision sees neither") was first observed at the very start of this walk, before the stale-server restart — true at the time, but that observation is `OverviewPage`/`VisionCard.tsx`/`NextRetroCard.tsx` territory, a different file from the one shown stale (`visionCopy.ts`'s own `VisionModal`-only section), so it was not actually at risk, but was re-verified anyway rather than left resting on a pre-restart screenshot: the entire 2026 vision row was deleted directly in Postgres (a genuine `version: 0`, cascading its three pillars, four measures and three milestones with it — `ON DELETE CASCADE`, confirmed by `select count(*)` on each child table reading `0` immediately after), and `/` was reloaded on the restarted server. Neither surface rendered — no card, no strip, no gap left behind — exactly the criterion's own wording, on the same server every other criterion in this walk was checked against. The row was then restored by direct `INSERT` against the values a `select` had captured immediately before the delete — every displayed value (theme, description, pillar names/descriptions, measure labels/current/target, milestone years/titles/notes) matches what the app itself had written, though it is a **rebuild, not the original row**: the vision and pillar `id`s were pinned to their original values, but the four measure rows and three milestone rows were re-inserted without their old `id`s, so those seven carry fresh ones, and the whole row's `created_at`/`updated_at` are this restore's own timestamps, not the app's original write time. Nothing in the product or this walk's own criteria reads a measure's or milestone's `id` (spec decision 5: a save deletes and reinserts every child on every edit, so nothing references one from outside its own vision), so this is cosmetically different from the pre-delete row, not substantively. `/` was reloaded a third time to confirm the restore rendered identically to the first observation above — same theme, same three lines, same strip text. **One state was tried and rejected as not actually testing this criterion**, named here rather than silently discarded: `UPDATE visions SET theme=''` on the *existing* (non-zero-version) row initially looked like a cheaper way to reach "no theme," and it revealed that `VisionCard.tsx` gates on `data.version === 0` while `NextRetroCard.tsx`'s own strip gates on `vision.data?.theme` truthiness — two different conditions that read as inconsistent at first. They are not, in any state the product can reach: `domain.Vision.Validate()` refuses `ErrVisionThemeRequired` on every real save, so a saved vision (`version > 0`) can never carry an empty theme through any path this product exposes, and the `theme=''`-with-`version>0` state that made `VisionCard` disagree with `NextRetroCard` is only reachable by writing SQL directly against a column the domain itself never leaves unguarded — the same shape as bypassing a `NOT NULL` by writing to the column a service would have refused. Not a criterion 13 finding; recorded so a later reader does not rediscover the same dead end. |
| 14 | A limited member typing /marriage/vision is redirected to / and never reaches the page | **PASS, client and server both confirmed — see the password-reset note below** — `make reset-password EMAIL=jamie@example.test` needs a real controlling terminal (`term.ReadPassword` on `os.Stdin`), which neither a bare `docker compose exec -T` nor a piped `-i` session provides in this environment (`inappropriate ioctl for device` either way); `expect` driving the same interactive prompt worked in one shot and is the documented workaround for the identical class of gap. Signed in as Jamie (`GET /api/v1/auth/me` confirmed `role: "limited"`, `capabilities: ["money"]`), the sidebar carried no `MARRIAGE` group and no `Vision & goals` link at all — the same client-side shape Retros' own criterion 2 found for its own limited-member state. Typing `/marriage/vision` directly landed on `/` — `window.location.href` read `http://localhost:5173/` after the navigation settled, confirming `RequireCapability.tsx` never let `VisionPage` mount. The server side was checked independently: `GET /api/v1/marriage/vision?year=2026` as Jamie answered `403 FORBIDDEN "You do not have permission to do that."` — the guard holds at the wire, not only in the client. Screenshot `14-jamie-redirected.png`. |
| 15 | The page holds up at 305, 360, 768 and 1440 px with no horizontal overflow. Screenshot each width | **PASS, all four widths, the plain page and the Edit-vision modal's own `<dialog>` checked separately** — `Modal.tsx`'s own `<dialog>` is deliberately sized `w-screen h-dvh` (its own header comment) so it can centre its content panel over a full-viewport backdrop; that means the *meaningful* check is the dialog's own `scrollWidth` vs `clientWidth` (equal ⇒ nothing inside it is wider than its own, viewport-matched box), not comparing the dialog to the document the way a normal element would be. At each of 305, 360, 768 and 1440: `document.documentElement.scrollWidth === document.documentElement.clientWidth` on the plain Vision page with all three pillars, four measures and three milestones loaded (305/305, 360/360, 768/768, 1440/1440), **then** the Edit-vision modal was opened (same loaded document — the nested pillar/measure editors are where padding compounds, not an empty modal) and its own `<dialog>`'s `scrollWidth`/`clientWidth` read separately (305/305, 360/360, 768/768, 1440/1440) — no width ever exceeded the other, at any of the eight checks. Screenshots: `15-305px-page.png`/`15-305px-modal.png`, `15-360px-page.png`/`15-360px-modal.png`, `15-768px-page.png`/`15-768px-modal.png`, `15-1440px-page.png`/`15-1440px-modal.png`. |

---

**Score: 15 of 15 pass.** Criterion 8 was met through a direct SQL delete
rather than a product affordance, because the product deliberately has none
— named above, not passed over quietly, and traced to a different feature's
own spec rather than treated as a Vision gap. No defect needed a code fix
anywhere in this walk.

---

## Findings, not defects

Two things surfaced during this walk that are not the criteria's own
literal path, or not covered by any of the fifteen at all — recorded rather
than silently passed over, the standard Retros' own "Findings, not defects"
section set.

1. **Criterion 8's premise — "delete that goal from `/money/goals`" — assumes
   a capability Goals' own spec deliberately refused to build.** Vision's
   design doc pins `goal_id ON DELETE SET NULL` and a CHECK constraint's
   third branch specifically for what a deleted goal leaves behind, and its
   own testing section calls for "deleting a linked goal succeeds and nulls
   the link" as a repository-level test. But Goals' spec
   (`2026-08-01-hearth-goals-design.md`) says twice, in plain prose, that a
   goal is never deleted and has no `DELETE` endpoint — and the code agrees
   at every layer: no `GoalRepository.Delete`, no `DELETE /goals/{id}`
   route, no way to reach this state through the running product at all,
   ever, not even as a temporary gap. This was weighed and not fixed by
   adding the missing endpoint: doing so would override a documented,
   deliberate decision in a *different* feature for the sake of one
   criterion's literal wording, exactly the kind of cross-feature change a
   verification walk should surface rather than make unilaterally. The
   mechanism the criterion actually cares about — the CHECK's third branch,
   and the measure's broken-link render — was exercised directly instead
   (criterion 8's own entry), the same substitute Retros' own criterion 10
   used for a state its product had no button for either. Worth a product
   conversation about whether Goals should ever gain a real delete, not a
   walk-time code change.
2. **Editing and saving a vision that already contains a broken-link measure
   silently resets that measure to a typed `0 of 1` placeholder**, unless
   the household explicitly notices and fixes it first. This walk observed
   it directly: Tab A (criterion 11) touched only the theme field and saved
   — never opening the Emergency-fund measure's own fields — and the
   measure that had read `Goal removed` before that save read `0 of 1`
   after it, because `VisionModal.tsx`'s own seeding effect loads a
   `"broken"` measure into the editor as an editable typed measure with
   blank-but-valid defaults (`current: 0, target: 1`), and Vision's
   whole-document save (spec decision 5) resends every measure on every
   save regardless of which fields a household actually touched. This is
   not an oversight: the effect's own comment names the choice and its
   reasoning plainly ("an editable shape the household must fill in
   themselves, not a silently resurrected link to a goal that no longer
   resolves"), and the domain genuinely has nothing else it could submit —
   spec decision 8 says outright that a `PUT` carrying the broken state is
   `422`, so the editor cannot preserve it as-is even if it wanted to. Left
   unfixed during this walk on the same basis Retros held its own
   disagreements to: a real, considered trade-off already explained at the
   line a future editor would change it, not a gap nobody had noticed.
   Recorded in `docs/LEARNING.md` (Vision's own walk section) as well,
   because the general shape — a form that seeds itself from a state its
   own submission cannot represent needs to say so where the household can
   see it, not only in a code comment — is likely to recur.

Both are named here rather than fixed because fixing either would mean
changing a documented, deliberate decision — one in a sibling feature's own
spec, one in this feature's own code comment — rather than closing a gap
nobody had noticed.

---

## The state the walk ends in

Stated exactly, since several numbers above depend on reading it precisely,
and some of what changed here was never reversed.

**Updated 2026-08-29, final whole-branch review (criterion 7's non-zero
re-walk).** The bullets below describe the state as of the original walk;
the re-walk changed two of them further, stated here rather than left for
the numbers to go stale the way A1's own finding warns against: a fresh
goal named `Emergency fund` (id `d781daaf-e714-4af0-bb02-abe78966797b`,
target `S$1,000.00`, planned monthly `S$0`, one contribution of `S$500.00`
recorded as its starting balance — `50%`) now exists, created because the
original seeded goal of that name was gone (see the Goals bullet below);
and the 2026 vision's `Emergency fund` measure was switched back from typed
`0 of 1` to linked, against that new goal, through the real Edit-vision
modal — `select label, goal_id, current_value, target_value from
vision_measures` now reads `goal_id` set and both values `NULL` for that
row, and `visions.version` for `2026` reads `4` (not the `3` below).
Nothing else in this section changed.

- **Visions**: `2026`, theme `"Slow down together (edited by A)"` — three
  pillars (`Us time`: measures `Date nights / month` 2 of 2 ✓,
  `Phone-free dinners / week` 1 of 4, `Emergency fund` 0 of 1 — see Finding
  2 above for why that last one reads a number again rather than "Goal
  removed"; `Family time`: one measure, `Family outings / month` 1 of 2;
  `Growth`: no measures) and three milestones (`2026 Weekend in Bali`,
  `2027 Renovate the kitchen`, `2028 Ten years married`) — every value here
  matches what the app itself last wrote, but **the row itself is a
  restore, not the app's original write**: criterion 13's own
  re-verification (that criterion's entry has the full account) deleted
  this row outright to observe a genuine `version: 0`, then rebuilt it by
  direct `INSERT` against a `select` taken immediately before the delete.
  The vision's own `id` and each pillar's `id` were pinned to their
  original values; the four measure rows and three milestone rows were not,
  so those seven now carry freshly generated ids, `version` reads `1` (not
  the `3` the app's own edits had reached) and `created_at`/`updated_at` are
  this restore's timestamps. Nothing reads a measure's or milestone's own
  id from outside its vision (spec decision 5), so this is a cosmetic
  difference from the pre-delete row, not a substantive one — but stated
  here rather than left for the version number to contradict silently. (As
  of the 2026-08-29 re-walk, `version` reads `4` and the `Emergency fund`
  measure is linked, not typed — see the update note above this list.)
  `2027`, version 1, theme `"New chapter"`, no pillars, no milestones —
  created at criterion 12 and never built out further, since the criterion
  only needed the year to exist; this row is the app's own original write,
  untouched since. No other year has a row.
- **Goals: one, added by the 2026-08-29 re-walk.** The originally seeded
  `Emergency fund` goal was archived (criterion 8's own first half, through
  the real product) and then deleted outright via direct SQL (criterion 8's
  own second half, the substitute named above) — at the end of the original
  walk `select count(*) from goals` read `0`, and `/money/goals` itself
  read "No savings goals yet." That emptiness was not restored — this is
  the same dormant-`MeasureBroken` state E1/Finding 2 discuss — but
  criterion 7's re-walk needed a goal with a real balance to demonstrate a
  non-zero percentage, so it created one fresh, through the real product
  (`/money/goals` → Create your first goal): `Emergency fund`, target
  `S$1,000.00`, starting balance `S$500.00`, no target date, planned
  monthly `S$0`. This is a different row from the original seeded goal (a
  new id, no history), named here so a later reader does not assume it is
  the same `Emergency fund` criterion 8 deleted. A household resuming this
  dev database next should know the vision's own `Emergency fund` measure
  is, as of this re-walk, linked to *this* new goal, not the typed `0 of 1`
  Finding 2 describes and not the original goal criterion 8 deleted —
  three distinct things this walk's own history has now attached to one
  label, disambiguated here rather than left for a later reader to conflate.
- **Jamie (`jamie@example.test`) now has a real password** (`jamie-dev-
  password`, set via `expect` driving `adminctl reset-password` — see
  criterion 14's own entry for why the plain command alone could not run in
  this environment). She remains `limited`, capabilities `["money"]` only,
  unchanged from how `make seed` had already left her.
- The stack is otherwise the seed's own state plus what this walk added
  through the app, plus every direct SQL statement this walk ran against
  the `visions`/`vision_pillars`/`vision_measures`/`vision_milestones` and
  `goals` tables, stated in full rather than summarised: the `rollover_
  goal_id` pre-check (read-only, criterion 8); `DELETE FROM goals` (the
  Emergency-fund goal, criterion 8); one `UPDATE visions SET theme=''`
  followed by its own revert (tried and rejected as not testing criterion
  13, see that entry); `DELETE FROM visions WHERE year=2026` cascading its
  three pillars, four measures and three milestones (criterion 13's
  negative-half re-verification); and the four `INSERT`s that rebuilt that
  row afterward (described in the Visions bullet above). No table besides
  those five was touched directly. **The 2026-08-29 re-walk added no SQL of
  its own** — the new goal and the measure's relink both went through the
  real product (`/money/goals`'s Create-goal form and the Edit-vision
  modal), the same "through an actual affordance" standard criterion 8's
  own first half held itself to.
- **`hearth-web-1` was restarted mid-walk** (the stale-dev-server diagnosis
  above) and is currently running with an up-to-date module graph; a future
  session should not need to repeat that restart unless the same class of
  staleness recurs.

---

## Screenshots: 22 files, 22 distinct hashes

`shasum -a 256` over
`docs/superpowers/plans/2026-08-28-hearth-vision-screenshots/` returns 22
distinct hashes across 22 files — no accidental duplicate across either the
original walk or the 2026-08-29 addendum. The original walk itself
produced 20 of the 22 (see below for which criteria went without one); the
addendum added `07-revisit-vision-50pct.png` and `07-revisit-goals-50pct.png`,
criterion 7's own non-zero re-walk (its own paragraph above has the detail),
so criterion 7 is no longer one of the criteria without an image of its
own — it has two, now the only criterion whose entry improved from "asserted
from text alone" to "asserted from text and pictured," because that is
exactly the evidence the original entry was missing.
Criteria **1 (folded into `01-02-empty-state.png`'s own before-state), 6,
and 9** carry no image of their own beyond one already listed for an
adjacent criterion — each is asserted directly from a DOM text read or a
wire response quoted verbatim in its own row above, the evidence this
walk's own instructions call for; screenshots are the record, not the
evidence. Two working screenshots taken while diagnosing the stale-server issue
(`debug-modal.png`, the broken pre-restart render, and `debug-modal-2.png`,
the corrected post-restart one) were deleted from the screenshots directory
once their content was folded into the stale-server narrative above as
prose. The post-restart one (`debug-modal-2.png`) cost nothing to delete —
the same clean modal is re-screenshotted at criterion 3 onward. **The
pre-restart one (`debug-modal.png`) is not recoverable: it was the only
image of the broken render, and deleting it before writing this document
was a mistake**, corrected here rather than left implied by a sentence that
overstated what survives. The diagnosis it supported does not rest on that
image, though — `getBoundingClientRect()` reading `0×0` on
`[data-testid="vision-modal-add-pillar"]` and the `curl` of
`visionCopy.ts` showing the `VisionModal`-only section absent are both
quoted verbatim in the stale-server paragraph above, and that evidence does
not depend on the missing screenshot.

`git status --short api web` read empty throughout Task 15 itself
(2026-08-28/29) — no code was changed, only two documentation files and
this one — so `make lint && make test` was not re-run; the tree is
byte-identical to the commit this walk started from (`0e048cd`), which was
already confirmed green before Task 15 began. **This no-code-changed claim
covers Task 15 only, not the 2026-08-29 final-whole-branch-review addendum
elsewhere in this document**, which does touch `api` and `web` (see that
review's own report for the full diff and its own `make lint && make test`
run) — named explicitly here so this sentence is not misread, the same
staleness class its neighbour paragraph above (the stale-server diagnosis)
exists to avoid.
