# Agreements — verification walkthrough

**Result: 15 of 15 pass. One product defect was found, fixed and
re-verified, and one platform behaviour was found and correctly left
unfixed** — both below "Findings" rather than folded into a criterion's own
cell, because neither sits on the fifteen criteria's own literal path.

Two criteria (2 and 10) were reordered relative to the brief's own
numbering for a dependency reason named inline below; criterion 10 was
walked by two different paths (an interpreted propose-time one and a
literal sign-time one) because the brief's own wording anticipated either
could be the one the running code actually takes, and both are recorded
rather than only the one that happened to fire first.

**The defect was not found by any of the fifteen criteria themselves** — it
surfaced during a deeper, off-script investigation into a theoretical
concern this record's own first draft had already written up as
"investigated, not reproduced." A stronger reviewer, reading that draft,
pointed out that the reproduction attempt had ruled out only one *trigger*
(a real window-focus event) for a mechanism (a background refetch reaching
a still-open modal) this walk had already demonstrated *elsewhere* in its
own evidence — criterion 10's own propose-time sequence shows a refetch
landing while a modal stays open and its Send button staying correctly
disabled through it. A second, cheaper trigger for the identical mechanism
(an unrelated pending card's own Agree, clicked from inside the same tab)
reproduced the concern in one call. Full account, the fix, its test and its
sibling sweep are under "One defect found and fixed" below; one Chromium
platform behaviour, confirmed via an isolated zero-app-code repro to not be
Hearth's own code, stays under "Findings, not defects" as before.

Run 2026-09-06, entirely within a single host-clock day (Asia/Singapore,
UTC+8; the API container runs UTC, seven to eight hours behind, never
crossing a boundary that mattered for anything read below except criterion
6's own deliberate demonstration of exactly that gap). `colima status`
answered "colima is running" throughout ("using macOS Virtualization.Framework").
`docker ps` before this walk showed `hearth-web-1`/`hearth-api-1`/
`hearth-postgres-1`/`hearth-mailpit-1` already up from an earlier session on
the SAME engine (colima) — the `lsof -nP -iTCP:5173 -sTCP:LISTEN` line
answered with an `ssh` process (a port-forward on this remote-mounted
machine, not a second Docker engine racing colima for the port), so no
engine ambiguity applied this time, but the check was still made before
trusting `localhost`, per the standing instruction. `goose_db_version` read
**14** (`00014_agreements.sql`) before any criterion was walked.

**The seeded household was NOT fresh when this task started** — a prior
session's `make seed` had already left Christine as an accepted co-owner
(not a pending invite), which contradicts criterion 1's own fixture
("a freshly seeded household IS the locked state"). `make down` alone does
not drop the `hearth-pgdata` volume, so a stack recreated on top of old data
carries the old membership rows forward with it. Fixed by `docker volume rm
hearth_hearth-pgdata` between `make down` and `make up`, which is what
actually produced a genuine one-owner-plus-pending-invite household for
criterion 1. **This cost real time and will cost it again** — worth adding
to `docs/HANDOVER.md` alongside the two-Docker-engines and stale-Vite traps
this task's own brief already names, since "a freshly seeded household is
the locked state" is silently false unless the volume is dropped first.

**Browser: Playwright's own Chromium throughout**, driven via
`browser_run_code_unsafe` (raw Playwright API access) rather than
`tabs_context_mcp`/Claude in Chrome — chosen up front rather than after
three failed connection attempts, because this walk's own two-owner and
two-window criteria (2, 6, 8, 9, 10, 12) need genuinely separate cookie
jars, and Playwright's `browser.newContext()` gives that directly, one
call, no profile-killing dance. Three real, independently-authenticated
browser contexts were used across the walk: **Andreas** (owner, the primary
`page` this tool exposes), **Christine window 1** (a second context, used
for every "second browser profile" and "B" role below — signed in through
the real invite-accept flow at criterion 3, and twice more through the real
sign-in form after her session was revoked by her own role changes at
criterion 12 — see that criterion's entry), and **Christine window 2** (a
third context, opened only where the walk itself needed a second,
independent signature from Christine while window 1's own Propose modal had
to stay open and undisturbed — criterion 10's both variants; closed logic
lives entirely in that criterion's entry, no state from it survives
elsewhere). Object references (not `globalThis` variables, which do not
survive between tool calls in this harness — confirmed empirically before
relying on it) were reacquired each call via `page.context().browser()
.contexts()`, tagging each context's page with a `__owner` property once
created so later calls could find the right one without re-deriving it from
URL heuristics.

**`christine@hearth.family`'s password, not a magic link.** The brief
anticipated a NULL password requiring Mailpit — but the volume wipe above
(needed to get criterion 1's fixture right) meant this session's own
invite-accept flow set Christine's password itself
(`christine-dev-password`, typed into the real accept-invite form at
criterion 3), so the magic-link route the brief names never applied here.
Recorded because the brief's own instruction was followed in spirit (use
the product's own flows, never write to the database for a session) even
though the specific mechanism it named turned out to be stale for this
particular run.

Criteria are Task 18's own brief, `.superpowers/sdd/2026-09-05-hearth-agreements/task-18-brief.md`,
walked in the order below rather than strictly 1-through-15 — **criterion
3 (accept the invite) was walked before criterion 2 (cold-load a two-owner
household)**, because criterion 2 cannot be observed at all until a second
owner exists: a freshly-seeded household is criterion 1's own one-owner
fixture, and criterion 2's premise is explicitly "as a two-owner household."
The brief's numbering assumes an order that cannot literally be followed;
this is named here rather than silently reordered.

---

## Criterion by criterion

| # | Criterion | Result |
|---|---|---|
| 1 | `/marriage/agreements` on the freshly seeded household explains the two-owner rule, and "Invite your partner" lands on Settings with the invite modal open. `/settings?invite=maybe` lands on Settings, modal closed | **PASS** — on the genuinely fresh (volume-wiped) one-owner household, `/marriage/agreements` rendered `data-testid="agreements-locked-invite"` reading "🤝 / Agreements need at least two owners / Agreements are the promises your household lives by. Every one of them is agreed by all the owners before it takes effect. / This household has one owner." with an "Invite your partner" link whose `href` read `/settings?invite=true`. Clicking it navigated to `http://localhost:5173/settings?invite=true` with a real `dialog[open]` present, `h2` reading "Invite a family member" — the invite modal, genuinely open, not merely the Settings page. Screenshot `01-invite-your-partner-settings-modal.png`. Then a **bare navigation** to `http://localhost:5173/settings?invite=maybe` landed on Settings with `document.querySelector('dialog[open]')` reading `null` — closed, exactly as `settingsRoute`'s own `validateSearch` requires (`search.invite === true || search.invite === "true"` only; `"maybe"` matches neither). Screenshot `01-settings-invite-maybe-closed.png`. |
| 3 | Accept the invite in a second browser profile: the page is the unlocked empty state, not locked and not an error | **PASS, walked before criterion 2 — see the reordering note above.** A genuinely separate Playwright context (`browser.newContext()`) navigated to the invite URL `make seed` printed (`http://localhost:5173/invite/hearth-dev-invite-token`), read "Andreas invited you in. / You'll share Money, Marriage and Family. / Joining as co-owner — full access, equal say on every agreement." Filled Name "Christine" and a real password, clicked "Accept & join household" — landed on `/` signed in as a genuine second owner. `/marriage/agreements` in that same context then rendered `data-testid="agreements-empty"` ("Write your first agreements…"), with `agreements-locked-invite` and `agreements-load-error` both absent. Screenshots `03a-invite-landing.png`, `03-christine-unlocked-empty.png`. |
| 2 | Cold-load `/marriage/agreements` as a two-owner household with the document request held open: the locked explanation never appears, not even for one frame | **PASS, walked after criterion 3 — see the reordering note above.** `page.route('**/api/v1/marriage/agreements', …)` held the response for 3000ms before `route.continue()`. During the hold (polled at 400ms, five times, and again once explicitly with a screenshot at 600ms): `document.querySelector('[data-testid="agreements-locked-invite"]')` read `null` on every sample, and the page's main content read exactly `"Loading…"` (`AGREEMENT_COPY.loading`, gated by TanStack's own `isLoading = isPending && isFetching`, never a `data?.locked ?? true` guess). Screenshot `02-cold-load-held-loading.png`. After the hold released, the page settled on the real two-owner unlocked empty state (`locked: false, error: false, empty: true`) — the fetch that was held was answering honestly, not merely delayed past the assertion window. |
| 4 | Click Use starter set: the page does not look identical afterwards, and all four sections are offered in the propose modal's section picker | **PASS** — before: "Write your first agreements" empty state. After clicking `agreements-starter-set`: "Your sections are ready / Money, Conflict, Home & kids and Us are ready…" — a genuinely different heading and body, not the same screen with an invisible change (decision 8's own reason this sentence exists). Screenshots `04a-before-starter-set.png` / `04b-after-starter-set.png` (byte-distinct, confirmed by hash — see Step 4 below). Clicking "Add your first agreement" opened Propose with its section `<select>` offering exactly `["Money", "Conflict", "Home & kids", "Us"]`. Screenshot `04c-propose-section-picker.png`. |
| 5 | Propose as the first owner: the card names who it waits for, offers Withdraw, offers no Agree. Withdraw one and watch it go | **PASS, both halves — the withdraw half is deliberately the LAST action taken against this specific proposal, after criterion 6 reads the same card from the other side; see that entry.** Andreas proposed adding to Money ("No solo spend over $200 without checking in first."). The resulting card read "Pending change — needs Christine" with exactly one button, "Withdraw" — no Agree anywhere in the card's own button list. Screenshot `05b-proposal-card-andreas.png`. Later, after criterion 6 read the identical card from Christine's side, Andreas clicked Withdraw → the two-step confirm ("Withdraw this proposal? …") → "Withdraw it": the card count on Andreas' page went from 1 to 0. Screenshot `05c-after-withdraw.png` (byte-identical to `04b-after-starter-set.png` — see the hash-comparison note below; this is the correct outcome, not a missed screenshot). |
| 6 | The same card in the second profile offers Agree and Discuss, and no Withdraw | **PASS** — the identical proposal (same `data-testid`, `agreement-proposal-14040f14-…`) read on Christine's own browser context: "Pending change — needs Christine", buttons exactly `["Agree", "Discuss"]` — no Withdraw. Screenshot `06-proposal-card-christine.png`. |
| 7 | Agree it: the agreement appears numbered in its section, and the section count, the continuous 01..N numbering across both columns and the header's v{n} all move together, read in one page script | **PASS, all three read together** — a fresh add proposal (Money) was created and agreed from Christine's context. One `page.evaluate` before the click read `subtitle: "…changes need every owner to agree"` (no version clause yet — `updatedAt` was still `null`, the document's real first-ever accepted change) and an empty `sectionCounts` (every section had `count: 0`, hence invisible per decision 8 — nothing rendered, not a bug). The SAME script, after clicking Agree, read: `subtitle: "…· v2, updated 6 Sept"`, `sectionCounts: [{ name: "Money", count: "1 agreement", rows: ["01No solo spend over $200…"] }]`, `proposalsRemaining: 0` — the count, the row's own `01` number and the header's `v2` all landed in the same read, all consistent with each other. Screenshot `07-agreed-numbering.png`. |
| 8 | Discuss a proposal, open `/marriage/retros`, agree it from the To-discuss block. Both pages reflect it with no manual reload | **PASS** — Andreas proposed adding to Conflict; Christine clicked Discuss and typed a real park note ("Let's talk about which rooms count as neutral so we both know where to go.") before Park for next retro — the card then read "Parked for the next retro — needs Christine" with the note visible under a "To discuss" label (closing the six-things concern about an empty park note — see below). Christine then used a real **client-side `<Link>` click** (`page.getByRole('link', { name: 'Retros' }).click()`, never `page.goto`) to reach `/marriage/retros` without a reload: the To-discuss block rendered the identical parked item, full text and note intact. Clicking its own Agree button there resolved it — the block then disappeared from Retros (nothing left parked). A second client-side `<Link>` click back to Agreements (again no reload) showed zero pending proposals and the agreement now live, numbered `02`, in the Conflict section, alongside Money's `01` — continuous numbering held across the navigation that never reloaded either page. Screenshots `08a-discuss-note-typed.png`, `08b-retros-after-agree.png`, `08c-agreements-after-noreload.png`. |
| 9 | On a household that has never started a retro, `/marriage/retros` still renders the To-discuss block | **PASS, and genuinely live rather than only structurally placed** — this exact household had never started a retro at the moment criterion 8 was walked (`retros-empty-state` was present, `noRetrosYet: true`), so criterion 8's own navigation to Retros IS this criterion's live case, not a separate contrived one: `agreements-to-discuss` rendered alongside `retros-empty-state` — the two mutually exclusive Retro branches (empty-state vs. history+detail) sitting beside a THIRD, always-present block that belongs to neither. Screenshot `09-to-discuss-no-retros-yet.png`. This closes the walk brief's own first deferred concern with a real observation rather than a structural-only one. |
| 10 | Two browsers, one agreement, two edit proposals: B's send is refused with the conflict copy, nothing typed is lost, Send stays disabled after the refetch, and B's proposal is still listed | **PASS, walked as two separate real sequences — see below for why both, and which is literal.** The server's only conflict mechanism (`LockAgreementTarget`, confirmed by reading `agreement_write_repo.go` and its own test `TestCreateProposalRefusesAStaleTarget`) compares a proposal's `previousBody` against the agreement's **live, already-accepted** wording — there is no separate lock preventing two simultaneously-open proposals against one target (confirmed: no such constraint in `00014_agreements.sql`, and the sign-time path below creates exactly that state on purpose). So "B's send is refused" only happens if the live wording actually changed between B opening the modal and B sending, which requires someone completing a DIFFERENT edit in between — the brief's own escape clause ("if the refusal arrives at propose time rather than at sign time … record it as the interpreted path it is") anticipated exactly this ambiguity. **Sequence A — propose-time, interpreted:** Christine (window 1) opened Propose → Edit on agreement 01, saw "…$200…", typed her own new wording ("…$150…") and left the modal open, unsent. Andreas then proposed his own edit on the same agreement ("…$100…") and sent it; a third, independently-signed-in Christine context ("window 2") agreed it, landing Andreas' wording as the new live body. Christine's still-open window-1 modal (never touched in between) then clicked Send: refused, `AGREEMENT_CHANGED`, banner read verbatim "This agreement changed while you were writing, so nothing was saved. Close this and start again from the current wording."; the textarea still held Christine's own "…$150…" draft (nothing lost); Send stayed `disabled: true` even after `waitForTimeout(500)` let the triggered background refetch land (the `hadConflict` latch, confirmed to survive the refetch rather than merely appearing before it). Screenshot `10c-B-send-result.png`. In this sequence **B has no proposal of her own to still be listed — her send never created one** — so this half of the wording is not literally satisfiable on this path, which is why sequence B below exists. **Sequence B — sign-time, literal.** Fresh state: Andreas proposed a second edit on the same target (needs Christine); Christine, BEFORE agreeing his, proposed her OWN edit on the same target (needs Andreas) — both proposals coexisted simultaneously, confirmed on screen (`cardsAfter` listing both). Christine window 2 then agreed Andreas' proposal, landing his wording. Reading Christine's own still-pending proposal afterward (from both Andreas' and Christine's own views): **it was still listed**, now carrying `data-testid="proposal-stale-note"` reading (from Christine's own view, `canWithdraw: true`) "This no longer matches the agreement it was written against, so it can't be agreed. Withdraw it and propose the change again." and (from Andreas' view) "Ask Christine to withdraw it and propose it again against the current wording." — decision 14's two named copies, both actually rendered, not merely present in `agreementCopy.ts`. Its own Agree button (visible to Andreas, since he is the one it awaits) rendered but `disabled: true` via `proposal.targetChanged`. Screenshots `10d-signtime-B-still-listed-stale.png`, `10e-andreas-view-of-stale-B.png`. **Both sequences are real, both pass; the brief's own literal wording is satisfied only by the second, which is recorded as the one closer to the criterion's stated ending, with the first kept because it is the path the code most readily takes and is exactly what the brief's escape clause names.** One thing worth being precise about, since it matters for the defect below: **sequence A's own window (Christine "window 1") never actually received Andreas' edit as a live prop update before Send was clicked** — the agreeing happened in an entirely separate browser context ("window 2"), which holds its own independent `QueryClient` with no cross-tab sync (confirmed: no `BroadcastChannel` wiring in `main.tsx`) — so `bodyOf(targetId)` at send time was still reading window 1's own, never-refetched cache, which still correctly held the wording Christine had actually seen. The 409 this produced is real and correctly handled, but it does not, on its own, demonstrate that an open modal survives a refetch landing **while it waits** — only that a modal whose own tab never refetched at all sends the right thing regardless. That gap is exactly what "One defect found and fixed" below closes: a genuine same-tab refetch, landing before Send is clicked, is a different and materially worse case. |
| 11 | Propose a remove, agree it, open Version history, read the removed wording, click Restore: Propose opens seeded with that wording in add mode, and the picker offers the now-empty section | **PASS, and this is also the walk's own direct evidence for the New section→Propose and Version history→Restore→Propose handoffs (see "Two-dialog handoffs" below)** — Andreas proposed removing Conflict's agreement 02 ("When we disagree about the kids…"); Christine agreed it. The Conflict section then vanished from the document entirely (only "Money" rendered — an empty section is invisible, decision 8). Version history's newest entry read "v7 · current / 6 Sept 2026 / Removed from Conflict: "When we disagree about the kids, we step outside the room before raising our voices." / Agreed by Andreas and Christine", with its own Restore button. Clicking Restore: **the history modal closed and exactly one dialog remained open** (`document.querySelectorAll('dialog[open]').length === 1`, confirmed, not merely inferred), titled "Propose a change", mode "Add new" checked, section pre-selected **"Conflict"** (the now-empty section, genuinely offered by the picker), body textarea pre-filled with the removed wording verbatim, and the restore-only sentence "Restoring is an ordinary proposal — it takes effect once everyone agrees." present. Focus had moved INTO the new dialog (`document.activeElement` was inside it, an `INPUT`) — real `showModal()`/focus-trap behaviour exercised by a real browser, not jsdom's stub. Screenshots `11a-version-history.png`, `11b-restore-seeded-propose.png`. Sent and agreed afterward, restoring Conflict with its agreement renumbered `02` at its new position — continuous numbering by creation order (decision 11), not a resurrected old number. |
| 12 | With proposals open, remove the second owner in Settings: the page re-locks, the proposals are still listed and named as waiting, and no write control is offered | **PASS, via the interpreted path Settings' own UI actually offers — named plainly, since there is no delete-member control at all.** `MembersPanel.tsx` has no "Remove member" affordance anywhere (confirmed by reading the whole file) even though the backend route exists (`DELETE /household/members/{id}`); the only control that changes the household's *owner count* is the role toggle, which demotes an owner to Limited — the real, product-level way to bring a two-owner household down to one without deleting anyone. With one pending proposal open ("We check in about big purchases before, not after."), Christine's role switch was clicked on Andreas' own Settings page: `aria-checked` flipped `true → false`, label "Limited". `/marriage/agreements` then rendered `data-testid="agreements-frozen"` reading "This household is down to one owner, so nothing here can change. Everything you agreed is still here, and it unlocks again when a second owner joins. / 1 change is waiting for a second owner" — the proposal **still listed** (`proposalsListed: 1`) with **zero buttons on its card** (`proposalButtons: []`, confirmed empty — gone, not `disabled: true`), `+ Section` and `Propose a change` both entirely absent from the header, and `Version history` still present (a read, per decision 3). Screenshot `12-relocked-with-content.png`. Christine's role was then restored to Owner in the same session — this **revoked and re-issued her session both times** (`docs/superpowers/specs/2026-07-26-hearth-foundation-design.md:305`: a role change revokes the affected member's sessions), which the walk observed directly (her window-1 tab landed back on `/sign-in` on its next reload) and worked around by signing her back in through the real form, not a database write. The household then unlocked again, the same proposal intact and pending exactly as before. |
| 13 | Create a section named "Money" twice: the second says so under the field, the modal stays open, and the typed name survives | **PASS** — with "Money" already a live section, New section was opened and "Money" typed again: `data-testid="agreement-section-name-error"` read "You already have a section called that." directly under the field, the dialog remained open (`dialogStillOpen: true`), and the input still read `"Money"` — no reset, no reload triggered (`AGREEMENT_SECTION_NAME_TAKEN` deliberately does not refetch, per `useAgreements.ts`'s own comment). Screenshot `13-duplicate-section-name.png`. |
| 14 | Keyboard only: Tab reaches Agree, Discuss and Withdraw on a pending card and every control in the propose modal, each with a visible focus ring, and Enter activates | **PASS, with one finding named below rather than folded silently into the pass.** On Andreas' page, Tab from the header's "Propose a change" landed directly on the pending card's own "Withdraw" (`outlineWidth: "2px", outlineStyle: "solid"` — read via `getComputedStyle`, not inferred from a CSS class); Enter opened the two-step confirm, and Tab reached both "Cancel" and "Withdraw it" with the same visible ring; Enter on "Cancel" (reached via Shift+Tab back to it) closed the confirm without withdrawing. On Christine's page, the SAME card's own Tab sequence reached "Agree" then "Discuss", both with a visible ring; Enter on Discuss opened the park-note panel. Inside the Propose modal (Andreas), a full forward Tab sweep visited, in order, every real control — the three mode radios, "+ New section", the section `<select>`, both textareas, Cancel — each read with a visible ring (`2px solid`); the modal's own initial `showModal()`-driven focus (the first radio, before any Tab) correctly showed **no** ring, which is expected `:focus-visible` behaviour for a programmatic `.focus()` call, not a defect. The focus ring was independently re-checked at all **four phone widths** (320/360/375/414px) on the header's own "Propose a change" button, closing the walk brief's own fourth deferred concern with real per-width evidence rather than the "claimed… without artifacts" state it named. **Finding, not a defect** (see below): Tab from the last focusable control inside the Propose dialog (Cancel, or Send itself once enabled) lands on `document.body` for exactly one keypress before the very next Tab correctly returns to the dialog's first control — reproduced identically in a bare, zero-Hearth-code `<dialog>`+`showModal()` test page, so this is Chromium's own native modal-cycling behaviour, not application code; outside page content was independently confirmed to remain properly inert throughout (a direct `.focus()` call on the real "Propose a change" button behind the open dialog was refused by the browser). |
| 15 | The ladder 320/360/375/414/768/1024/1440 on the populated page and the history modal, comparing `scrollWidth` to `clientWidth` | **PASS, all seven widths, page and dialog both, plus the `lg` two-column grid boundary checked directly** — at every width, `document.documentElement.scrollWidth === document.documentElement.clientWidth` on the populated page (three sections with content, one empty section, the "New section"/"Propose"/"Version history" header). The same walk then opened Version history at each of the seven widths and read the dialog's own `scrollWidth - clientWidth` (never the document's, since a `<dialog>` paints in the top layer) — **0 at every width**, page and dialog alike. Closing the walk brief's own third deferred concern with a number rather than an assumption: at 768px the two section columns stacked (`col0Top: 408, col1Top: 524`, same `left`); at 1024px and 1440px (`lg` and above) they sat side by side (`col0Top === col1Top`, `col1Left > col0Left`) — read via `getBoundingClientRect()`, not eyeballed. Screenshots: `15-{320,360,375,414,768,1024,1440}px-page.png` and the matching `-modal.png` set, fourteen files, all present. |

---

**Score: 15 of 15 pass.** One product defect was found and fixed, though
not on any criterion's own literal path — see "One defect found and fixed"
below for the full account, the fix, its mutation-checked test and its
sibling sweep. `web/src/features/marriage/ProposeAgreementModal.tsx` and
`ProposeAgreementModal.test.tsx` are the only files under `api`/`web` this
task touches, committed separately before this record's own amendment;
`make lint && make test` (Step 8) was re-run after that fix, on the tree
carrying it, and both are green (see that section below).

---

## Two-dialog handoffs (the walk brief's third deferred concern)

Both directions were exercised with real evidence, not inferred from the
component tree:

- **New section → Propose**, the success path (criterion 13 only exercised
  the *failure* path — a duplicate name, which correctly does NOT hand off
  anywhere). Creating a genuinely new section ("Travel") closed the New
  section dialog and opened Propose in the same beat:
  `document.querySelectorAll('dialog[open]').length === 1` immediately
  after, titled "Propose a change", its section `<select>` already reading
  "Travel", and focus moved into the new dialog (`document.activeElement`
  an `INPUT`, `dialog.contains(...)` true). Screenshot
  `newsection-to-propose-handoff.png`.
- **Version history → Restore → Propose** — criterion 11's own entry above
  has the full account: one dialog open throughout the handoff, correct
  title, correct pre-fill, correct focus.

Neither handoff ever showed two `dialog[open]` elements at once, and focus
was inside the new dialog immediately in both cases — the class of defect
the brief named ("a modal that threw on every open in production while all
five of its tests passed") did not reproduce here, in either direction,
across three separate real exercises of it (13's failure path, 11's
restore, and the new-section success path above).

---

## The six things this walk was asked to cover directly

1. **The To-discuss block on a household that had never started a retro** —
   criterion 9's own entry: this was the household's actual state at the
   moment criterion 8 reached Retros, so the live case and the structural
   case are the same observation, not two separate ones.
2. **A `parkNote` rendering with real content** — criterion 8: a genuine,
   specific sentence ("Let's talk about which rooms count as neutral so we
   both know where to go.") was typed and rendered back, under the "To
   discuss" label, both on the Agreements card and in the Retros To-discuss
   block.
3. **Both two-dialog handoffs** — see the dedicated section above.
4. **The two-column grid at `lg`, and the focus ring at all four phone
   widths** — criterion 15 (grid) and criterion 14 (focus ring), both with
   `getBoundingClientRect()`/`getComputedStyle()` numbers, not screenshots
   alone.
5. **The locked-with-content state** — criterion 12: the document stayed
   fully rendered and read-only, the frozen proposal stayed listed and
   named as waiting, and no write control was offered (buttons array empty,
   not disabled).
6. **The date rendering** — see below, a product observation, not a fix.

### The date rendering (product observation, not a fix)

`agreementDateLabel`'s exact call
(`new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "short" })`)
was reproduced directly in page script against a constructed instant,
`2026-09-06T16:30:00.000Z`, with an explicit `timeZone` override standing in
for each partner's own browser: rendered as **"7 Sept"** for a
Singapore-based viewer (00:30 local — just past midnight) and **"6 Sept"**
for a Jakarta-based viewer (23:30 local — still the previous evening) of
the identical timestamp. This is not a hypothetical: Singapore is UTC+8 and
Jakarta (WIB) is UTC+7, so any change signed between 16:00 and 23:59 UTC —
eight hours out of every day — renders one calendar date to one partner and
the previous one to the other, on every date label this feature has (the
proposal card's timestamp, the header's "updated" clause, and every
Version-history entry). This matches `docs/FEATURE_TRACKER.md`'s own
existing note on the Agreements-by-section row and `docs/HANDOVER.md`'s
"Worth doing when convenient" list; recorded here with the concrete before/
after strings the instruction asked for, deliberately not fixed, per the
same instruction.

---

## One defect found and fixed

Not on any of the fifteen criteria's own literal path — surfaced during the
deeper, off-script investigation criterion 10's own entry points to, after
an initial pass at this same concern had wrongly concluded there was
nothing to reproduce. Recorded here rather than folded into a criterion's
cell, the same shape Bills' and the outbound-inspector's own walks used for
a defect their own criteria didn't directly name.

**What it was.** `ProposeAgreementModal.tsx`'s `handleSend` computed
`previousBody` by calling `bodyOf(targetId)` fresh, at send time —
`targets.find((agreement) => agreement.id === targetId)?.body ?? ""`,
against `targets`, itself derived every render from the `sections` PROP.
That prop updates on any background refetch this modal's own shared
`useAgreements()` query receives while the modal stays open (a state the
modal is explicitly designed to survive — `hadConflict`'s own comment:
"nothing typed is lost"). An `edit` or a `remove` does not just change an
agreement's `body`; it retires the row's id outright and inserts a new one
(decision 9, `applyAgreementChange`'s own `remove()` then `add()`). So the
very first background refetch reaching an open modal **after any edit
landed anywhere on the target it was pointed at** made `bodyOf(targetId)`
resolve to `""` for a `targetId` that no longer existed — and an empty
`previous_body` on an `edit` or a `remove` is a shape
`agreement_proposals_shape` (the database's own CHECK) refuses outright.
The household saw `422 AGREEMENT_PROPOSAL_SHAPE_INVALID`, surfaced as the
generic "Could not send that for agreement. Try again." — no hint that the
target had moved, and no path back to a working Send short of closing and
reopening the modal (something the error message never suggests).

**How it was found.** Criterion 10's own two sequences (above) both use a
completely separate browser context to land the competing edit, and
separate contexts hold separate `QueryClient`s with no cross-tab sync
(`main.tsx` wires no `BroadcastChannel`) — so neither sequence's own
"held-open modal" tab ever actually received a live prop update before
Send was clicked; each passed correctly, but for a narrower reason than
first assumed (see that entry's own closing paragraph). A first attempt to
test the narrower, same-tab case directly — reproducing a real
`refetchOnWindowFocus` trigger via `page.bringToFront()`, synthetic
`focus`/`visibilitychange` events and a same-context decoy tab — reported
"investigated, not reproduced," because Playwright's automation-controlled
pages never report `document.hasFocus() === false` in this harness (see
`docs/LEARNING.md`'s new walk section). That result was too narrow: it
ruled out one *trigger* for the refetch, not the underlying mechanism.
Clicking an unrelated pending proposal's own Agree button — still visible
in the DOM behind the open modal's backdrop, reached via a script rather
than a real click, since a real pointer cannot reach it while the dialog is
modal — reaches the identical `queryClient.invalidateQueries` call a real
window-focus refetch would also make, **inside the same tab holding the
open modal**, and reproduced the failure in one call: the target `<select>`
option visibly updated to the newly-landed wording while the modal stayed
open, and Send then failed with the generic 422 above, confirmed via a
`page.route` capture of the actual outgoing request body
(`previousBody: ""`) and a direct replay of that exact body against the
API (`422 AGREEMENT_PROPOSAL_SHAPE_INVALID`).

**The fix.** `previousBody` is now its own `useState`, snapshotted once
when the target is chosen (the initial state, and `chooseMode`/
`chooseTarget`) and read directly — never recomputed from `bodyOf(targetId)`
at send time. `body` and `targetId` were already handled this way; only
`previousBody` read live. Re-verified in the running app against the exact
scenario above (screenshot `10f-refetch-during-open-modal-fixed.png`): the
same same-tab refetch, landing while the modal stays open, now correctly
answers the real `409 AGREEMENT_CHANGED` conflict — the existing banner
("This agreement changed while you were writing, so nothing was saved.")
appears, Send stays disabled, and the typed draft survives — instead of the
confusing 422.

**The test.** A new case in `ProposeAgreementModal.test.tsx` renders the
modal behind a small harness component that holds `sections` in its own
`useState` and swaps it via a button click — a real parent re-render with a
new prop, not a fake `rerender` call — then asserts the captured request
carries the original `previousBody`. **Mutation-checked**: reverting the
fix (restoring the live `bodyOf(targetId)` call) failed the new test with
`previousBody: ""` where `"Each gets S$200/mo no-questions-asked"` was
expected — the exact failure this test exists to catch, watched red before
being restored to pass. `npx vitest run src/features/marriage/` — 21 files,
182 tests, all green with the fix in place.

**Sibling sweep.** Grepped every `*Modal*.tsx` in `web/src/features` for a
`.find((x) => x.id === …)` lookup called directly inside a submit handler
rather than only inside a state setter. `BillModal.tsx`, `BudgetModal.tsx`
and `TransactionModal.tsx` each have one, but every one of them looks up a
value to *display* (an account, a category), never one a server compares
for staleness — Money's own entities have no propose/sign concurrency
check at all. `VisionModal.tsx` and `RetroModal.tsx` were checked too, per
this project's own precedent (`VisionModal.tsx`, its header comment): both
solve the identical underlying worry — a draft that could resubmit
something stale — by closing the modal outright on conflict rather than
trying to keep a version-tracked draft alive across a refetch, a different
and equally valid answer to the same question. No second instance of this
exact shape (a value that must equal "what this session saw," recomputed
live from a prop that can move) was found anywhere else in this codebase.
Filed as `docs/LEARNING.md` pattern 18.

**Commit**: `web/src/features/marriage/ProposeAgreementModal.tsx` and its
test, committed separately, before this record's own amendment, naming the
criterion (10, by way of the concern its own entry names) and the sweep
above.

---

## Findings, not defects

One thing surfaced during this walk that is not on the criteria's own
literal path and is not application code — recorded rather than silently
passed over, the standard Vision's own walk set for this section.

1. **Chromium's native `<dialog>` focus cycling visits `document.body` for
   one keypress at the tail of a modal's tab order, before self-correcting
   on the very next Tab.** Found while walking criterion 14: Tab from the
   Propose modal's last focusable control (Cancel, with Send disabled; and,
   separately, Send itself once enabled) landed on `document.body`
   (`inside: false`, confirmed stable after a 500ms settle, not a
   measurement race) rather than wrapping to the dialog's first control.
   **Reproduced in an isolated, zero-Hearth-code test page** — a bare
   `<dialog>` with two `<button>`s and a plain `showModal()` call shows the
   identical one-tab detour through `body` — which settles this as a
   genuine Chromium platform behaviour, not a bug in `Modal.tsx` or in this
   feature's own propose modal. Two things keep this from being a real
   focus-trap escape: outside page content stayed properly inert throughout
   (a direct `.focus()` call on the real "Propose a change" button, sitting
   right behind the open dialog, was refused by the browser — confirmed,
   not assumed), and the very next Tab press after landing on `body`
   correctly returns focus inside the dialog, to its first control. Not
   fixed here: `Modal.tsx`'s own header comment explains, at length, why it
   deliberately relies on the platform's native focus trap rather than
   hand-rolled keydown logic — reintroducing that logic to paper over one
   self-correcting, non-escaping browser quirk shared by every modal in
   this codebase (not something specific to Agreements) would be exactly
   the regression that comment argues against. Worth a product conversation
   if it ever surfaces as a real complaint, not a walk-time code change.

---

## Step 4: screenshots compared by hash, not by eye

`shasum -a 256` over all 44 PNGs in
`docs/superpowers/plans/2026-09-05-hearth-agreements-screenshots/` (43 from
the walk itself, plus `10f-refetch-during-open-modal-fixed.png` from the
defect's own re-verification) found **43 distinct hashes and exactly one
repeated pair**: `04b-after-starter-set.png` and `05c-after-withdraw.png`
are byte-identical.
This is the correct outcome, not a missed change: at the moment
`05c-after-withdraw.png` was taken, Andreas had just withdrawn the only
proposal that had ever existed since the starter set was seeded, with
nothing agreed in between — so the document's actual state (four sections,
every one still at `count: 0`, zero proposals) was genuinely,
byte-for-byte the same state `04b` had captured moments earlier. A
withdrawn proposal is specified to leave the document exactly as it was
("nothing in the document changes" — `AGREEMENT_COPY.withdrawConfirmBody`,
and this file's own row above), so an identical screenshot here is direct,
positive evidence that promise held, not the "the change did not land"
failure mode this step exists to catch on a genuine before/after pair for
the same edit.

---

## Step 8: full suite and lint, after the walk

Run twice: once on the tree the walk started from (no code changed by the
fifteen criteria themselves — all green, 13 Go packages, 88 web files / 826
tests), and again after "One defect found and fixed" above landed its
change:

```
make lint   # arch-lint, tsc, eslint, go vet — all green
make test   # api: 13 packages ok; web: 88 files / 827 tests passed
```

827, not 826 — the one new regression test in `ProposeAgreementModal.test.tsx`.

Both green. `git status --short api web` reads empty.

---

## The state the walk ends in

Stated precisely, since this task is explicit that shared state touched
during a walk should be named and, where reversed, said so.

- **Membership**: Andreas and Christine are both owners (Christine's role
  was toggled Owner→Limited→Owner during criterion 12; the household ends
  in the SAME two-owner state it was in before that criterion, restored
  through the real Settings UI, not a database write). Christine's session
  was revoked twice by her own role changes (an intentional, documented
  security property, not a bug) and re-established twice through the real
  sign-in form with the password her own invite-accept set.
- **Sections**: `Money`, `Conflict`, `Home & kids`, `Us` (from the starter
  set), plus `Travel` (created live during the New-section→Propose handoff
  check) — `Home & kids` was never given an agreement during this walk and
  stays invisible in the document (decision 8), same as `Travel`.
- **Agreements**: three live, numbered continuously — `01` Money ("V-Fix-Test
  wording, landed by Andreas." — a placeholder sentence from the "One defect
  found and fixed" section's own re-verification pass, several edits deep
  into criterion 10's own race sequence and the deeper investigation after
  it; left as-is rather than restored, the same "state left as a test
  produced it" precedent Vision's own walk record sets, since nothing reads
  this wording besides the household itself), `02` Conflict ("When we
  disagree about the kids, we step outside the room before raising our
  voices." — restored via criterion 11's own Restore flow, so it carries a
  fresh id and a fresh `02`, not the original row's), `03` Us ("Date night
  stays on the calendar even during a busy week." — added during the
  off-script phone-width exploration, Step 5).
- **Proposals**: zero open. Every proposal this walk created was either
  withdrawn or agreed by the end of the criterion (or the investigation
  pass) that created it — none left dangling.
- **Version history**: eleven entries (`v12` current, four shown, "Show
  v2–v8 ↓" collapsed) — every edit this walk and its deeper investigation
  made, including the several extra edit/agree rounds "One defect found and
  fixed" needed to reproduce and then re-verify the fix, all real, none
  touched directly in the database.
- **No SQL was run against any Agreements table during this walk** — every
  state change above went through the real product's own routes, including
  the deliberately-engineered conflicts in criterion 10 and the defect's
  own reproduction, both of which needed three real signed-in sessions
  rather than any shortcut.
- `hearth_hearth-pgdata` was dropped once, at the very start (see the
  fresh-seed note above), and not touched again.

---

## Screenshots: 44 files, 43 distinct hashes

See "Step 4" above for the one repeated pair and why it is the expected
outcome rather than a miss.
