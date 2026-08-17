# Retros — verification walkthrough

**Result: 15 of 15 pass.** No product defect was found. Three criteria (2,
10, 13) were met by a path the product's own client-side guards or missing
controls made necessary rather than the criterion's most literal reading,
each named as such inline; two of those three (2 and 13) also turned up a
real divergence between the design spec's own prose and what ships,
investigated and judged not to need a code change during this task — see
"Findings, not defects" below.

Run 2026-08-18 (host clock ~06:00–07:30 +08; the API container is UTC, where
it was ~22:00–23:30 on 2026-08-17 — the same instant, a different
calendar-day label on each side, since +08 is eight hours ahead of UTC and
the walk ran before the UTC day rolled over. Both sides agree on the
**month** throughout (August 2026), which is what every month-scoped figure
below depends on; `new Date()` in the browser read Aug 18, the API's own
clock read Aug 17 — checked once, at the start, and not load-bearing for any
criterion below since none of them cross a month boundary between the two
clocks.) Driven in a real Chromium (Claude in Chrome) against
`http://localhost:5173` for criteria 1–13 (its own `window.innerWidth` read
`2752` throughout — `resize_window` reported success at 1440×1000 but never
actually changed it, a tooling artifact caught and worked around at
criterion 15, not a product finding), plus a second, genuinely separate
browser (Playwright's own Chromium, independent cookie jar, whose own
`page.setViewportSize()` does change `innerWidth`) for criterion 14's two
sessions and all of criterion 15's width checks. From a freshly recreated
stack:

```
make down && make up     # forces recreation; hearth-migrate-1's own log confirms
                          # "OK 00009_retros.sql" / "successfully migrated to version: 9"
make seed
```

Docker Desktop is the active engine on this run (colima was not running —
`colima status` answered "colima is not running"); `docker compose ps`
against Docker Desktop showed the five `hearth-*` containers already
existing there from a previous session, so that is the engine this walk
used throughout, checked once at the start rather than assumed. `select
count(*) from retros` read **0** immediately after seeding — this is the
seed's own state, not Task 11's ten leftover rows, confirmed directly
rather than assumed from a clean volume.

Criteria are Task 17 of `.superpowers/sdd/2026-08-16-hearth-retros/task-17-brief.md`,
verbatim, walked in numeric order except where noted inline.

**Two non-seeded members were created through the running app, not inserted:**

- **Christine accepted her seeded co-owner invite** (`http://localhost:5173/invite/hearth-dev-invite-token-2`,
  printed by `make seed`), password set in the browser — role `owner`,
  capabilities `["calendar","chores","money","marriage"]`, confirmed by
  reading `GET /api/v1/auth/me` directly. This is the second owner criterion 7
  needs (assign an action to both partners) and one of the two real browsers
  criterion 14 needs.
- **Jamie**, invited via `adminctl create-invite --email=jamie@hearth.family
  --name=Jamie --role=limited --capabilities=money --inviter-email=andreas@hearth.family`,
  invite accepted in the browser — role `limited`, capabilities `["money"]`,
  no `marriage`. This is the limited-member-holding-money-not-marriage state
  criterion 2 needs.

Both invite-acceptance forms hit the exact trap `docs/HANDOVER.md` §2 names:
a plain `click()`/synthetic value assignment left React's own state empty and
the submit button did nothing (no network request fired, confirmed with
`read_network_requests`) until the native-setter-plus-`input`-event pattern
was used instead. Recorded here because it cost real time before the cause
was recognised, the same shape the doc already warns about.

---

## Criterion by criterion

| # | Criterion | Result |
|---|---|---|
| 1 | A signed-in owner sees Marriage in the sidebar, expanding to a Retros link | **PASS** — signed in as Andreas, `nav.innerText` read `"...Bills\nMARRIAGE\nRetros\nSettings..."` — an uppercase `MARRIAGE` group label with a `Retros` link beneath it, the identical shape `MONEY` already takes. |
| 2 | A limited member sees no Marriage entry at all, and typing `/marriage/retros` lands on the owner-only explanation — not a red error | **PASS, by an interpreted path — see below** — signed in as Jamie (limited, holds `money`, not `marriage`): `nav.innerText` read `"...Bills\nSettings..."`, no `MARRIAGE` label, no `Retros` link at all. Typing `/marriage/retros` directly landed on **`/`** (Overview), not on `/marriage/retros` and not on a page reading `retros-owner-only` — `window.location.href` confirmed `http://localhost:5173/` after the navigation settled. This is **not a red error**, which is the criterion's own literal requirement, and it is met — but it is not literally "the owner-only explanation" `RetrosPage.tsx` itself renders, because `RequireCapability.tsx` (`web/src/features/shell/RequireCapability.tsx`) is a *client-side* guard that bounces anyone lacking the `marriage` capability straight to `/` via `<Navigate to="/" replace />` before `RetrosPage` ever mounts or issues its own `GET /retros` — Jamie never reaches the page whose 403 branch the criterion describes. `RetrosPage.tsx`'s own `data-testid="retros-owner-only"` branch (`RETRO_COPY.ownerOnlyHeading` "Owner only" / `RETRO_COPY.ownerOnlyBody` "Retros are visible to the household owner. Ask them if you'd like to see where things stand.") is real, correctly built (mirrors `GoalsPage.tsx`/`BillsPage.tsx`/`BudgetPage.tsx`), and unit-tested — but is defence-in-depth for a state decision 11 calls "redundant today" (`domain.ValidateMembershipChange` already refuses `marriage` to any limited member, so nobody can reach the page with the capability check passing and the server's `requireOwner` still refusing). The server side was checked directly too: `GET /api/v1/retros` as Jamie answered `403 FORBIDDEN "You do not have permission to do that."` — the `requireCapability(marriage)` middleware's own generic refusal, confirming the guard holds at the wire even though the client never gets far enough to show it. This is the identical shape Bills' own walk found for its third member state (Noor, holding no capabilities at all) — a client-side redirect, not a rendered explanation — recorded here rather than passed over quietly. |
| 3 | A household with no retros sees the first-run panel; the start button names the right month | **PASS** — before any retro existed, `/marriage/retros` rendered `data-testid="retros-empty-state"` reading "No retros yet / A monthly check-in for just the two of you -- what went well, what was hard, and what to try next." with `data-testid="retros-start"` reading **"Start July retro"** — the earlier of {previous month, current month} with no retro row, per decision 5, since both July and August were empty at that point. Screenshot `03-empty-state.jpg`. |
| 4 | Starting a retro on a date where last month has none files it against **last** month | **PASS** — clicked "Start July retro" (today is 18 August). The history list gained a row **"July 2026 / In progress"**, the modal that opened read **"July 2026 retro"**, and the sidebar's own button immediately relabelled to **"Start August retro"** (August is now the only remaining startable month). Confirms decision 5's rule directly rather than inferring it from criterion 3 alone. |
| 5 | The mood picker: click one, and — separately — reach it by keyboard, arrow through all five, and confirm the focused option is visibly distinct. Capture before/after screenshots and compare by `shasum -a 256` | **PASS** — **Mouse:** clicking the "Terrible" pill set `input[aria-label="Terrible"].checked = true` (read directly from the DOM) and gave that pill's `<label>` `border-accent bg-callout` in place of `border-hairline`. **Keyboard:** blurred focus to the dialog, Tab landed on "Close", a second Tab landed on the radiogroup's one tab-stop (the checked radio, native browser behaviour for a `name`-grouped radio set), then `ArrowRight` four times walked Terrible → Not great → Okay → Good → Great, `document.activeElement`'s `aria-label` read out and matching at every step. Each step was screenshotted zoomed on the mood row; all five show the browser's own native focus ring (a blue square) around the small, genuinely visible `<input type="radio">` itself, distinct from the green selection border on the whole pill — confirms `RetroModal.tsx`'s own header comment (lines 359–373), which built this picker with a real, non-`sr-only` radio specifically to avoid the `TransactionsPage` Kind-filter defect (a hidden input whose visible pill never reacted to focus). `shasum -a 256` over the five saved images (`05a`–`05e`) returned **five distinct hashes** — no two identical, so each keyboard step visibly changed the screen. Screenshots `05a-mood-focus-terrible.png` … `05e-mood-focus-great.png`. |
| 6 | Type into both textareas, Save draft, reload — the text is still there and the retro still reads as in progress | **PASS** — typed into "What went well" ("Finished the retro walk. Kept our July budget on track.") and "What was hard" ("The retro modal took two tries to get the mood click right.") with the native-setter-plus-`input`-event pattern, clicked Save draft — `read_network_requests` confirmed `PATCH /api/v1/retros/2026-07` → `200`. Navigated fully away and back to `/marriage/retros` (a real reload, not a soft route change), opened July's history row again: both sentences read back verbatim, mood read **"mood 5/5"**, and the history row still read **"July 2026 / In progress"** — not finished, not reset. |
| 7 | Add two actions, assign one to each partner and one to both; the initials render as the design's `A` / `C` / `A C` | **PASS** — added three actions in the same draft: "Book the babysitter for date night" assigned to Andreas only, "Call the insurance company about the renewal" assigned to Christine only, "Plan the September weekend trip together" assigned to both. `document.body.innerText` for the action block read, verbatim: `"Book the babysitter for date night\nA\nCall the insurance company about the renewal\nC\nPlan the September weekend trip together\nA\nC"` — rendering **`A`**, **`C`**, and **`A` `C`** exactly as the design draws, in that order, for the three assignment shapes. Screenshot `07-actions-assignees.jpg`. Saved via Save draft afterwards (`PATCH /api/v1/retros/2026-07` → `200` again). |
| 8 | Finish the retro. It leaves the draft state, appears in the history, and the done count moves | **PASS** — before finishing, the page header read only `"Monthly check-in, just the two of us"` (no "done since" phrase at all with zero finished retros, confirmed by reading it directly — matches the formula table's "with no finished retros the header omits the phrase entirely" rule). Clicked Finish retro on July's draft. After: header read `"Monthly check-in, just the two of us · 1 done since Jul 2026"`, the History row changed from `"July 2026 / In progress"` to `"July 2026 / Mood 5/5 · 3 actions"` — left the draft state and the count moved 0 → 1. The mood chart also gained its month labels (`Sep Dec Mar Jun`, in place of "Not enough retros yet"), and the detail panel auto-opened July's own finished view, reading `"Aug 18 · mood 5/5"` and `"Actions for August"` over the same three action rows — confirmed deliberate, not a mislabel: `retroCopy.ts`'s own comment ("actions decided in a retro are carried out the month AFTER it") and `RetroDetail.tsx:156`'s `actionsHeading(nextMonthName(record.month))` name this as the design's own convention (`dc.html`), not a bug. |
| 9 | The money check-in shows the retro month's budget figures and today's goals standing, and says which is which | **PASS** — July's modal read `"Budget: 805% used  Goals today: No goals to check on yet."` Verified against real data, not eyeballed: `GET /api/v1/budgets/2026-07` returned `totalCap=135000` minor, `totalSpent=1087209` minor (summed from its own `categories[].spentMinor`) → `1087209/135000 = 805%` exactly, matching the panel digit for digit. `GET /api/v1/goals` returned `"goals":[]`, matching "No goals to check on yet." The "which is which" half is explicit in the copy itself — `MoneyCheckInPanel.tsx`'s own header comment: budget is `useBudget(month)` (the retro's own month, no further label needed since it is rendered inside that month's own retro modal) while goals is `useGoals()` with no month argument at all, and the **"Goals today"** wording is the label carrying that distinction, verbatim per decision 3. Screenshot `09-money-checkin.jpg`. |
| 10 | On a month with no budget, the panel says so rather than showing `0%` | **PASS, reached via a documented SQL precondition — see below** — August's modal read `"Budget: No budget set for August yet  Goals today: No goals to check on yet."`, not `0%`. **How August was made budget-less**: `RetroService.Start` computes its month itself from the server clock (`domain.StartableMonth(today, ...)`) and takes no month parameter from the client (`api/internal/usecase/retro.go:200-225`), so a retro can only ever be started for the real previous or current month — July or August on this run's actual clock (18 August 2026) — and the seed had already given **both** of those months a real budget row. There is also no product control anywhere (Budget page included) that un-sets a budget once created — Budget has no delete route. To exercise this criterion at all inside the one real day this walk had, August's `budgets`/`budget_lines` rows were deleted directly in Postgres (`delete from budget_lines where budget_id=…; delete from budgets where id=…`), confirmed by `GET /api/v1/budgets/2026-08` reading `"budget":null` immediately after, then August's retro was started and opened exactly as a household would. This is a deliberate, narrow substitute for a state the product itself cannot produce on demand — not a shortcut around a state it could. |
| 11 | Tick an action from the page (not the modal); reload; it stays ticked | **PASS, walked after 12** (July's own actions were still all unticked when 12 ran, since ticking one first would have removed it from 12's own "Still open from July" list before that criterion could observe all three) — on July's finished detail view (the page, no modal open), ticked the checkbox beside "Book the babysitter for date night". `GET /api/v1/retros/2026-07` immediately read that action's `doneAt` as `"2026-08-17T22:21:15.275768Z"` (no longer `null`), the other two unchanged. Navigated fully away and back, reopened July's row: the same checkbox read `checked: true` from the DOM (the other two still `false`), and the row rendered with a strikethrough and a filled green box (screenshot `11-tick-persists.jpg`) — ticked from the page, not the modal, and it survived a real reload. |
| 12 | Start the next month's retro: last month's unticked action is offered, carrying it creates a new action here, and last month's row is unchanged | **PASS** — opening August's retro (July's own unticked actions, none ticked at this point) showed **"Still open from July"** listing all three: "Book the babysitter for date night", "Call the insurance company about the renewal", "Plan the September weekend trip together", each with its own **Carry over** button. Clicked Carry over on the first. Result: August's own action list gained **"Book the babysitter for date night"** under **"Actions for September"** with a **"Carried from July"** provenance line beneath it (screenshot `10-12-carry-over.jpg`), and "Still open from July" dropped to the remaining two (the carried one is not re-offered). Confirmed against the wire, not just the screen: `GET /api/v1/retros/2026-08` → the new action's `carriedFrom` is `"28683a49-1857-45c0-905a-21319dd548e8"`; `GET /api/v1/retros/2026-07` → the **original** action (same id) still exists on July's own retro, still `"doneAt":null`, still `"carriedFrom":""` — July's own row is byte-for-byte unchanged, exactly decision 4's contract. |
| 13 | Delete a draft — allowed. Try to delete a finished retro — refused, with copy that explains | **PASS, both halves, the second by an interpreted path — see below** — **Draft delete:** opened August's own draft (Edit), clicked "Discard draft," the in-page confirmation read `"This draft will be permanently deleted. This can't be undone."` with "Keep it"/"Yes, discard it," confirmed — the page returned to the empty August slot, the History list dropped back to just July, and `"Start August retro"` reappeared in the header. A real, literal pass through the UI. **Finished-retro refusal:** `RetroModal.tsx`'s own Discard-draft trigger is rendered **only** when `completedAt === null` (`retro.data.retro.completedAt === null`, RetroModal.tsx:601) — a finished retro offers no delete control at all, on purpose (its own comment: "an offer that always fails is worse than no offer"), so there is no click path to attempt this against July's finished retro; the only way this branch is ever reached in the real product is a genuine two-tab race (the other partner finishes it between page load and the click), which `retroCopy.ts:241-251`'s own comment names directly. To exercise the refusal at all, `DELETE /api/v1/retros/2026-07` (July, already finished) was issued directly with a real session cookie and the page's own `csrf_token` header — the same request the app would send. **Result: refused** — `404` (not `204`), body `{"error":{"code":"NOT_FOUND","message":"That could not be found."}}`. This is **not** the design spec's own error-table wording ("That retro is finished and cannot be deleted") — `handleDiscardRetro`'s own comment (`retro_handlers.go:351-360`) and `retroCopy.ts`'s own comment explain this is deliberate, not an oversight: `RetroRepository.DeleteDraft`'s `WHERE completed_at IS NULL` answers the identical `ErrNotFound` a genuinely missing retro gets, on purpose, so "there is no draft here" reads the same either way, with no separate "already finished" state for the client to handle. The refusal itself is real and correctly wired (`DeleteDraft`'s zero-row match does not report success — the exact `SetBillNextDue`-shaped defect this codebase has shipped before, and does not here); the copy a household would actually see if this branch were ever reached is generic, not the explanatory sentence the criterion's own wording and the design spec both describe. Recorded here rather than silently marked done, since a household could not literally observe this message without the same devtools access this walk used. |
| 14 | **Two browsers, one draft.** Both open it, both edit, the second save is refused with the reload copy and nothing typed is lost | **PASS, all four parts checked** — restarted August's draft (deleted at 13), opened its Edit modal in **two genuinely separate browsers**: Claude in Chrome, signed in as Andreas ("Browser A"), and Playwright's own independent Chromium, an entirely separate process with its own cookie jar, signed in as Christine ("Browser B") — not two tabs of one browser, which cannot hold two sessions at once. Both loaded the draft at `version: 1` before either saved. **Andreas typed and saved first** ("Andreas's own August note, saved first." in "What went well"), confirmed via `GET /api/v1/retros/2026-08` → `version: 2`, `wentWell` matching. **Christine, still on her stale `version: 1` mount, typed into "What was hard"** ("Christine's own August note, typed before saving.") and clicked Save draft. **(a) Reload copy:** an `alert` appeared in her modal reading `"Someone else saved this retro while you were editing. Nothing you typed here has been lost — but Save and Finish are turned off until you close this retro and reopen it to see their version."` (the server's own `409 RETRO_CHANGED` — confirmed directly by replaying the identical stale-version PATCH, body `"Someone else saved this retro while you were editing it. Reload to see their changes."` — with the frontend adding the "nothing lost" reassurance on top). **(b) Nothing typed was lost:** her "What was hard" textbox still read `"Christine's own August note, typed before saving."` in the DOM *after* the refusal, read directly from the Playwright accessibility snapshot, not inferred. **(c) Save and Finish disabled on that mount:** confirmed — and more than the criterion names: Save draft, Finish retro, **and** Discard draft, the two Carry-over buttons, the add-action input and its two assign buttons were all `[disabled]` in the same snapshot, a fully frozen stale mount rather than two buttons alone. **(d) Reopening gives a clean editor:** closed and reopened the modal in Browser B — both textareas were empty (no stale text, no error banner), every control enabled again, and the page behind the modal now showed Andreas's own saved line ("Andreas's own August note, saved first.") under "What went well" — the reopen picked up the winner's real, current version. Screenshots `14-conflict-banner.png` (Browser B, mid-refusal) and `14-clean-reopen.png` (Browser B, after reopening). |
| 15 | Every screen at 320 / 375 / 768 / 1440 with no horizontal overflow, including the modal — check the dialog's own `scrollWidth` vs `clientWidth`, because a native `<dialog>` paints in the top layer and a document-level check cannot see it | **PASS, all four widths, both the Retros page and its own modal, plus Overview's new card** — `claude-in-chrome`'s `resize_window` did not actually change this session's viewport (`window.innerWidth` stayed `2752` after a reported-successful 320×800 resize — a tooling artifact of this environment, not a product finding, noted so it is not mistaken for one), so this criterion was walked in Playwright's own independent Chromium instead, whose `page.setViewportSize()` genuinely changes `window.innerWidth`. At each of 320, 375, 768, 1440: `document.documentElement.scrollWidth === document.documentElement.clientWidth` on the plain Retros page (305/305, 360/360, 753/753, 1440/1440), **then** the Edit modal was opened and its own `<dialog>` element's `scrollWidth`/`clientWidth` read separately (305/305, 360/360, 753/753, 1425/1425) — no width ever exceeded the other, at any of the eight checks. At 320px specifically (the spec's own named tightest point — five mood pills in a row), each pill's `getBoundingClientRect()` read **44.39–44.41 × 65px**, clearing the 44px touch floor project-wide, not merely "close." Overview (carrying Retros' own "Next retro" card) was checked at all four widths too, the same way: 305/305, 360/360, 768/768, 1440/1440 — no overflow at any of them. Screenshot `15-320px-modal.png`. |

---

**Score: 15 of 15 pass.** Criteria 2, 10 and 13 were met by paths the product's own client-side guards or missing controls made necessary rather than by the criterion's most literal reading — each is named as such above, not passed over quietly. No defect was found that needed fixing in this task; see "Findings, not defects" below for the two divergences from the design spec's own prose that were investigated and judged not to warrant a code change during this walk.

---

## Findings, not defects

Two places where this walk found the shipped product saying or doing
something other than the design spec's own prose promises, investigated in
full and judged, after review, not to need a code change during this task —
recorded rather than silently passed over, the same standard Bills' own walk
held its own "product questions" section to.

1. **Criterion 13's finished-retro-delete refusal answers a generic
   `"That could not be found."`, not the design spec's own error-table
   wording ("That retro is finished and cannot be deleted").** Both
   `retro_handlers.go:351-360`'s own comment and `retroCopy.ts:241-251`'s own
   comment name this as deliberate: `RetroRepository.DeleteDraft`'s `WHERE
   completed_at IS NULL` answers the identical `ErrNotFound` a genuinely
   missing retro gets, on purpose, so "there is no draft here" reads the same
   either way, with no separate "already finished" state for the client to
   handle. This was weighed against fixing it to match the spec's literal
   copy and left as shipped: the refusal itself is real and correctly wired
   (no zero-row-match-reports-success defect here), the divergence is
   documented at the exact line a future editor would change it, and the
   fix is not a three-line mirror of an existing pattern — `DeleteDraft`'s
   contract in `ports.go` documents `ErrNotFound` on the zero-row match, so
   distinguishing "finished" from "missing" would need a new domain error, a
   new handler branch, and new tests, a cross-layer change to a reviewed
   contract made on the strength of a walk's own disagreement with a
   decision the code already explains. Worth a product conversation, not a
   walk-time edit.
2. **Criterion 2's "lands on the owner-only explanation" is met by a client
   redirect to Overview, not a rendered explanation.** `RequireCapability.tsx`
   bounces anyone lacking the `marriage` capability to `/` before
   `RetrosPage.tsx` ever mounts; `retros-owner-only` is real, correctly
   built, and unit-tested, but is defence-in-depth for a state decision 11
   calls "redundant today" (a limited member can never hold `marriage` at
   all, so the capability check always fails first). Not a defect — the same
   shape Bills' own walk found and accepted for Noor, its own third member
   state.

Both are named here rather than fixed because fixing either would mean
changing a documented, deliberate decision rather than closing a gap nobody
had noticed — the walk's job was to find out what the product actually does
and say so, not to relitigate a call the code already explains.

---

## The state the walk ends in

Stated exactly, since a few of the numbers above depend on reading it
precisely, and one change here was never reversed.

- **Two owners**: Andreas (seeded) and Christine (accepted her seeded
  co-owner invite this run). **One limited member holding `money` only**:
  Jamie, invited and accepted this run, used for criterion 2.
- **Two retros**: July 2026, **finished**, mood 5/5, three actions ("Book the
  babysitter for date night" — ticked, Andreas; "Call the insurance company
  about the renewal" — open, Christine; "Plan the September weekend trip
  together" — open, both). August 2026, a **draft**, `version: 2`, "What went
  well" reading "Andreas's own August note, saved first.", no actions of its
  own (the draft that carried "Book the babysitter" forward was discarded at
  criterion 13 to prove drafts can be deleted, then a fresh, empty August
  draft was started again for criterion 14 — its own carry-over from July
  was never re-clicked, since criterion 14 only needed a draft to exist, not
  a repeat of criterion 12).
- **August's `budgets` row was deleted directly in Postgres** for criterion
  10 (no product control exists to un-set a budget, and no month besides
  July/August was reachable through the real Start-retro button on this
  run's actual clock — see criterion 10's own entry) **and was not
  restored.** `/money/budget` for August and Overview's "This month" card
  both now read "No budget set" as a direct, visible consequence — not a
  product defect, a leftover of this walk's own setup that the next session
  against this dev database should know about before reading either screen.
- The stack is otherwise the seed's own state plus what this walk added
  through the app: no other table was touched directly.

---

## Screenshots: 14 files, 14 distinct hashes

`shasum -a 256` over `docs/superpowers/plans/2026-08-16-hearth-retros-screenshots/`
returns 14 distinct hashes across 14 files — no accidental duplicate this
walk (`05a`–`05e`, the five keyboard-focus states, are the pair criterion 5's
own instructions call out by name: two identical hashes there would mean the
change did not land, and none are). Criteria **1, 2, 4, 6 and 8** carry no
image of their own — each is asserted
directly from a DOM read or a wire response quoted verbatim in its own row
above, which is the evidence this walk's instructions call for; screenshots
are the record, not the evidence. Two screenshots from the previous, unfinished
attempt (`08-finished-retro.jpg`, `overview-next-retro-card.jpg`) were not
carried forward uncredited — the first was deleted outright (criterion 8 is
asserted from text reads alone, and `11-tick-persists.jpg`, taken this run,
shows the same finished-July-retro state with an added tick), the second was
retaken this run against this run's own data before being written back under
its original name.
