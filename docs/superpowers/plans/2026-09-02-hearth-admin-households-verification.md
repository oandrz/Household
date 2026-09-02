# Hearth — admin households and metrics, browser verification

Walked 2026-09-02 against the running dev stack (`hearth-api-1`, `hearth-web-1`,
`hearth-postgres-1`, `hearth-mailpit-1`) at <http://localhost:5173>, on branch
`admin-households`. Browser tool: Claude in Chrome failed to connect after two
attempts ("Browser extension is not connected"); fell back to the Playwright
MCP tools (`mcp__plugin_playwright_playwright__*`) for the whole walk, as the
brief allows.

**Result: 15 of 15 criteria pass. Caveats on criteria 7 and 12.**

This is a recording pass only: nothing in the product was changed to make a
criterion pass. Where the walk found a defect that no criterion named
directly (criterion 7's caveat, and the items under "Beyond the criteria"),
it is written down for a separate, reviewed fix — not fixed here.

---

## The criteria

### 1. A signed-in limited member sees the not-found page at `/admin/households`

**Done:** Signed in as Jamie (`jamie@example.test`, role `limited`,
capability `money`) via the magic-link flow (Sign in → "Email me a one-time
sign-in link" → link from Mailpit). Confirmed her sidebar carries no Admin
link. Navigated directly to `http://localhost:5173/admin/households`.

**Seen:** The URL settled on `/admin/households?q=&limit=50` but the page
rendered only `Page not found.` — no admin chrome, no data. The network
panel showed the page's own guard call, `GET /api/v1/admin/flags`, answering
**404**, indistinguishable from an unrouted path (`requirePlatformAdmin`'s
own 404, per `middleware_admin.go`).

**Result:** PASS.

**Caveats:** None.

---

### 2. Re-auth prompt, then the four tiles

**Done:** Signed in as `andreas@hearth.family` (platform admin) with
`walkpassword2026`. Clicked the "Admin" sidebar link, which led to
`/admin/flags` and showed "Confirm your password / Re-enter your password to
open the admin surface." Entered `walkpassword2026` and submitted. Then
navigated to `/admin/households`.

**Seen:** The password prompt accepted the same product password and opened
the operator shell. `/admin/households` rendered four tiles: Households (2),
Active, 7 days (1), Sign-ups, 30 days (0 requested / 0 completed), Invites
pending (1).

**Result:** PASS.

**Caveats:** None.

---

### 3. Each tile equals the matching `psql` count, in the same minute

**Done:** Immediately after criterion 2's page load, ran four queries against
the running Postgres: `SELECT COUNT(*) FROM households`; the active-household
query copied verbatim from `admin_directory.sql` (`CountActiveHouseholdsSince`,
using the 7-day window `usecase/admin_directory.go` actually passes —
`directoryActiveWindow = 7 * 24 * time.Hour`); the signups-30d query; the
pending-invites query. All four ran within the same minute as the page load
(13:37:21 UTC vs. page load 13:37:13 UTC).

**Seen:**

| Tile | Screen | `psql` |
|---|---|---|
| Households | 2 | 2 |
| Active, 7 days | 1 | 1 |
| Sign-ups, 30 days | 0 requested / 0 completed | `0\|0` |
| Invites pending | 1 | 1 |

The signups tile's zero case renders as plain `0 requested` / `0 completed` —
two independent counts, not a ratio — so there is no `NaN%`/`—` concern; the
frontend never computes a percentage from these two numbers
(`AdminHouseholdsPage.tsx`'s `MetricTiles`).

Separately: the invites table holds **two** unaccepted rows
(`kris@example.test`, `partner@example.test`), but `partner@example.test`
expired 2026-08-24 (`now()` at the time of the walk was 2026-09-02), so the
brief's tile query (`accepted_at IS NULL AND expires_at > now()`) correctly
returns 1, not 2. `ListPendingInvitesForAdmin` in `admin_directory.sql` filters
on the identical `expires_at > $2` condition, so the drill-in's "Pending
invites" list agrees with the tile — see criterion 11's "before" state, which
lists only Kris.

**Result:** PASS.

**Caveats:** None.

---

### 4. The active nav link is visibly distinct, by computed style

**Done:** With `/admin/households` open, ran
`getComputedStyle(a).color` and `.fontWeight` on both `nav[aria-label="Operator"] a` elements (Flags, Households) via `browser_evaluate`, rather than
trusting `aria-current` alone.

**Seen:**

| Link | `aria-current` | `color` | `font-weight` |
|---|---|---|---|
| Flags (inactive) | `null` | `oklab(0.999994 … / 0.6)` (≈ white at 60% opacity) | 500 |
| Households (active) | `page` | `rgb(255, 255, 255)` (solid white) | 600 |

Both the computed `color` (opacity 0.6 vs. fully opaque) and `font-weight`
(500 vs. 600) differ between the two states — the active link is not relying
on `aria-current` alone.

**Result:** PASS.

**Caveats:** None.

---

### 5. Searching a member's email finds the household; searching an invitee's email finds nothing

**Done:** Submitted the search form (not keystroke-by-keystroke) with
`jamie@example.test`, then with `kris@`.

**Seen:** `jamie@example.test` → "Andreas & Christine" listed, with
`matched Jamie · jamie@example.test` under the name, "Showing 1 of 1". `kris@`
→ `Nothing matches 'kris@'.` — Kris is an invitee (not yet accepted), so she
is invisible to the member-search join, exactly as `SearchHouseholds`'s
`LEFT JOIN LATERAL` over `memberships`/`users` predicts.

**Result:** PASS.

**Caveats:** None.

---

### 6. A wrong-case search still finds the household, with no "matched" line

**Done:** Submitted the search form with `OENTORO` (the household's
`family_name` is stored as `Oentoro`).

**Seen:** "Andreas & Christine" listed, "Showing 1 of 1", **no** `matched …`
paragraph under the name — the household itself matched
(`h.family_name ILIKE sqlc.arg(pattern)`), not a member, so `household_matched`
is true and no per-member match line is shown.

**Result:** PASS.

**Caveats:** None.

---

### 7. A nonsense search shows the empty state; Clear restores the list

**Done:** Submitted the search form with `nonsense`. Read the resulting
paragraph and its "Clear" control. Then, to isolate exactly which control was
exercised, inspected the DOM: there are **two** separate "Clear" buttons
rendered whenever `q !== ""` — one inline in the search form next to the
"Search" button (`AdminHouseholdsPage`'s own conditional), and one inside the
"Nothing matches" paragraph itself (`HouseholdsTable`'s `onClear`). Clicked
the one inside the "Nothing matches" paragraph, since that is the one the
criterion's wording points at ("Clear restores the list", read right where
the empty state is shown).

**Seen:** The paragraph read exactly `Nothing matches 'nonsense'.` with a
"Clear" button beside it. Clicking it navigated back to `?q=&limit=50` and
the list of both households reappeared ("Showing 2 of 2") — the list genuinely
was restored. But `document.getElementById('household-search').value` was
still `"nonsense"` afterward: the search box kept showing the stale query
while the table underneath it showed the unfiltered list, a real (if minor)
inconsistency for whoever is reading the screen.

Clicking the **other** Clear button — the one in the search form itself, next
to "Search" — does not have this problem: it resets both the URL and the
input's value to `""`. The difference is in `AdminHouseholdsPage.tsx`: the
form's own Clear handler calls `setDraft("")` before `onSearch("")`; the
table's `onClear` prop is just `() => onSearch("")`, and `draft`'s
`useState(q)` only seeds from `q` on mount, so it never re-syncs when the URL
changes are driven from the table's own Clear.

**Result:** PASS on the criterion as written (the list is restored) — with a
caveat.

**Caveats:** The "Nothing matches" paragraph's own Clear button restores the
list but leaves the search box showing the query that just returned zero
results. An operator who trusts the input box over the table would believe a
search is still active when it is not. Worth a small fix (have that `onClear`
also reset `draft`, or lift `draft` to sync with `q` via `useEffect`) but not
fixed here per this task's scope.

---

### 8. One search is one request and one audit row, with the right `detail`

**Done:** Recorded the audit-log row count (`SELECT COUNT(*) FROM
admin_audit_log`) immediately before submitting one fresh search —
`jamie` — using `form_input`-equivalent (`browser_fill_form`, filling the
whole string at once, then one click on Search; no per-keystroke typing that
could fire a debounced request). Read the network log filtered to
`/api/v1/admin/households` immediately after, and the audit table's last row.

**Seen:** The network log's final matching entry was exactly one request,
`GET /api/v1/admin/households?q=jamie&limit=50 => 200`. The audit-log count
went from 12 to **13** — exactly one new row. Its `detail` column read
`{"query": "q=jamie&limit=50"}` — byte-for-byte the format
`adminHouseholdsPath`/`URLSearchParams.toString()` builds. No StrictMode
double-fetch was observed (React Query's in-flight-promise dedup for a
query key evidently absorbs the double-mount without a second network
call).

**Result:** PASS.

**Caveats:** None.

---

### 9. Searching a Telegram-only member finds the household; the drill-in shows "Telegram," no email

**Done:** Submitted the search form with `ethan` (Ethan is the seed's
Telegram-only limited member: `users.email` NULL, a `telegram_accounts` row
with `chat_id 424242`, substituting for the brief's generic "kid" per this
task's setup notes). Clicked into the resulting household's drill-in.

**Seen:** Search: "Andreas & Christine" listed with `matched Ethan` — no
`· <email>` suffix, since `household.match.memberEmail` is null for him.
Drill-in members table, Ethan's row: Channel cell reads `Telegram`; no email
appears anywhere in that row.

**Result:** PASS.

**Caveats:** None.

---

### 10. The drill-in's members table shows roles, capabilities, last active; Ethan shows "never"

**Done:** Read the full members table on the Oentoro drill-in
(`/admin/households/96096965-af91-4a39-b299-320f5324f464`), before Kris's
invite was accepted.

**Seen:**

| Name | Channel | Role | Capabilities | Last active |
|---|---|---|---|---|
| Andreas | andreas@hearth.family | Owner | calendar chores money marriage | 3 min ago |
| Kayla | — | Limited | calendar chores | never |
| Ethan | Telegram | Limited | calendar | **never** |
| Christine | christine@hearth.family | Owner | calendar chores money marriage | 16 days ago |
| Jamie | jamie@example.test | Limited | money | 5 min ago |

Ethan — the Telegram-only member substituting for "Kid" — reads exactly
`never`, since no session has ever existed for him
(`ListMembersForAdmin`'s `MAX(COALESCE(s.last_seen_at, s.created_at))`
correctly returns null with zero matching rows, and `relativeTimeLabel(null,
now)` renders `"never"`). Kayla, also never signed in, shows the same. Roles
render as `Owner`/`Limited` (`memberBadgeLabel`), capabilities as a
space-joined list.

**Result:** PASS.

**Caveats:** None.

---

### 11. The pending invite is listed with inviter and expiry; accepting it removes it and raises the member count

**Done ("before"):** On the same drill-in load as criterion 10, read the
"Pending invites" section. Recorded the household header's member count.
Then signed out of the admin session (product-level sign-out, via the
"Sign out" button — no second browser profile was needed once signed out),
navigated straight to Kris's accept URL
(`http://localhost:5173/invite/Ult1D6k43SMVE4gAoqPHzNab9tF3wr5AR-W_oHmwdLM`),
filled a password, and submitted "Accept & join household". Signed back in
as `andreas@hearth.family`, re-authenticated into `/admin`, and reloaded the
same drill-in.

**Seen ("before"):** `Family Oentoro · created Aug 17, 2026 · SGD · 5
members`. Pending invites: one entry — **Kris**, `kris@example.test`,
`Limited`, `invited by Andreas`, `expires Sep 9, 2026`. (The other
unaccepted-but-expired invite, `partner@example.test`, correctly does not
appear here — see criterion 3's note; `ListPendingInvitesForAdmin` filters
`expires_at > $2` exactly like the tile query.)

Accepting Kris's invite: `psql` confirmed `invites.accepted_at` was set
(`2026-09-02 13:42:06…`) and a new membership row for Kris (`role: limited`)
existed immediately after.

**Seen ("after"):** `… · 6 members` (5 → 6, +1 exactly). Kris now appears in
the Members table (`kris@example.test`, Limited, `calendar`, `just now`).
"Pending invites" now reads **`None pending.`**.

**Result:** PASS.

**Caveats:** None. (This is an irreversible change to the dev database — see
"State changed on the dev box" below.)

---

### 12. Three wrong passwords lock the household's sign-in; the drill-in's lockout callout shows a time, and `unlock-household` clears it on reload

**Done last, as instructed.** First pass (induction only): on `/sign-in`,
submitted `andreas@hearth.family` with two wrong passwords through the
browser UI and saw the inline error step down (`Two tries left…` after
attempt 1); confirmed via curl what the third attempt's exact response
carries (`INVALID_CREDENTIALS` with `attemptsRemaining: 1` after attempt 2,
then `423 HOUSEHOLD_LOCKED` with an ISO `lockedUntil` after attempt 3) —
`login_attempts`/`HOUSEHOLD_LOCKED` is server-side state keyed on the
household, not something the sign-in form's own local error state proves by
itself.

Re-read what "the lockout callout" and "makes it disappear on reload"
actually describe: `AdminHouseholdPage.tsx` renders a `lockout` block sourced
from `AdminDirectoryDeps{LoginAttempts}` whose copy is `lockoutLabel()` —
the *only* copy in this codebase that names an absolute time
(`"Sign-in is locked until <time> (in <n> min)."`), and whose second line
cites the exact command the criterion tells you to run next
(`adminctl unlock-household --email <owner>`). That is the admin surface
this task built to make an operator's lockout visible without touching
`psql` — the sign-in screen's own inline error (checked in criterion 6's
predecessor logic, not one of the fifteen) is local component state, gone on
any reload regardless of whether the household is still locked, so testing
"disappears on reload" against it would be vacuous by construction.

Redid the lockout itself via three `curl -X POST /api/v1/auth/sign-in`
calls with wrong passwords, so the browser's live admin session (already
re-authenticated into `/admin`) was never touched. Loaded the Oentoro
drill-in while locked. Ran
`docker compose exec -T api go run ./cmd/adminctl unlock-household --email andreas@hearth.family`
(the working form for this environment's `docker-compose.yml`, which has no
separate `admin` service — the brief's `docker compose run --rm admin
/app/adminctl …` does not apply here). Reloaded the drill-in. Confirmed the
underlying lock was genuinely cleared, not just hidden from this one screen,
with a real `curl` sign-in using the correct password.

**Seen:** curl attempt 1 → `401 INVALID_CREDENTIALS`,
`"attemptsRemaining": 2`. Attempt 2 → `401 INVALID_CREDENTIALS`,
`"attemptsRemaining": 1`. Attempt 3 → `423 HOUSEHOLD_LOCKED`,
`"lockedUntil": "2026-09-02T14:03:12.271344Z"`. With the browser's admin
session untouched, `/admin/households/96096965-…` (no re-auth prompt —
the grant was still live) rendered a `role="status"` callout:
`Sign-in is locked until 10:03 PM (in 15 min).` / `Clear it early with
adminctl unlock-household --email <owner>.` — matching `lockedUntil`
converted to local time. `adminctl unlock-household` printed
`Household unlocked.` Reloading the same drill-in: the `status` block was
gone entirely — no lockout callout, straight to the Members table. A final
`curl` sign-in with `walkpassword2026` returned `200`, confirming the
underlying lock (not just this screen's view of it) was cleared.

**Result:** PASS.

**Caveats:** The first pass of this criterion (browser-only, three wrong
passwords through the sign-in form, then a reload of `/sign-in` itself)
tested the wrong surface — the sign-in screen's inline error is local
component state that clears on any reload regardless of lock status, so it
could not have shown a FAIL even if the real, server-side lockout had never
been cleared. That pass is retained above only for the induction evidence
(the attempt-countdown copy); the PASS verdict rests on the redo against
`AdminHouseholdPage.tsx`'s own lockout callout, which is the thing this task
actually built and the thing capable of failing.

---

### 13. A member's first request sets `last_seen_at`; a second request a minute later leaves it unchanged

**Done:** As part of Jamie's sign-in for criterion 1 (before any admin
session existed in the browser, to avoid the shared session cookie being
overwritten mid-test), read `sessions.last_seen_at` for her session
(`WHERE user_id = '1c17ba89-b948-456a-af69-b4238f31bc0d' ORDER BY created_at
DESC LIMIT 1`) immediately after her first authenticated page load. Waited
75 seconds (via a `Monitor` timer, not a raw sleep). Reloaded the same tab
(still Jamie's session — no other identity had signed in yet in that browser
context) and re-read the same row. Also read `sessionTouchInterval` in
`api/internal/adapter/http/middleware_session.go`: **1 hour** — so a gap of
roughly a minute is well inside the no-op window by design, not a coincidence
of timing.

**Seen:** First read (13:34:39 UTC, ~5s after sign-in):
`last_seen_at = 2026-09-02 13:34:34.853967+00`. Second read (13:36:30 UTC,
~112s after the first request, ~70s after the wait began): **same**
`last_seen_at = 2026-09-02 13:34:34.853967+00` — unchanged, consistent with
the 1-hour touch throttle.

**Result:** PASS.

**Caveats:** None.

---

### 14. At 320px, rows collapse to two lines and neither page scrolls horizontally

**Done:** Resized the browser viewport to exactly 320×760 (confirmed via
`window.innerWidth === 320`). Checked
`document.documentElement.scrollWidth <= window.innerWidth` and took a
full-page screenshot on both the households list and the Oentoro drill-in.
Resized back to 1440×900 afterward.

**Seen:** List page: `scrollWidth 320 <= innerWidth 320` — true, no
overflow. Drill-in: `scrollWidth 305 <= innerWidth 320` — true. Screenshots
confirm the visual collapse the code comments describe: list rows show
`Name · Family` on one line and `N members · <last active>` on the next; the
drill-in's member rows show `Name · Channel` on one line and
`Role  capabilities · <last active>` on the next (Andreas's second line wraps
onto a third visual line purely from text length — `Owner  calendar chores
money marriage · 3 min ago` — but this is line-wrapping within the
two-logical-line structure, not a layout break, and it never pushes the page
wider than the viewport).

**Result:** PASS.

**Caveats:** None.

---

### 15. A well-formed-but-missing UUID and a malformed non-UUID both show not-found

**Done:** Navigated to
`/admin/households/00000000-0000-0000-0000-000000000000` and to
`/admin/households/not-a-uuid` while signed in as the admin.

**Seen:** Both rendered `Page not found.` inside the operator chrome (header
with Flags/Households nav still visible — this is `NotFoundScreen` rendered
as the route's content, not the whole-app 404 criterion 1 saw). Network
panel: `GET /api/v1/admin/households/00000000-0000-0000-0000-000000000000
=> 404` and `GET /api/v1/admin/households/not-a-uuid => 404` — the API
answers both identically, exactly as `AdminHouseholdPage.tsx`'s own comment
says it should ("a malformed id, which the API answers identically").

**Result:** PASS.

**Caveats:** None.

---

## Beyond the criteria

Walking this as a first-time operator, not just against the checklist:

**A. The "Nothing matches" Clear button leaves the search box stale (see
criterion 7).** Restated here because it is the one real, confirmed defect
the walk found in code this task actually shipped. An operator who searched,
got zero results, clicked "Clear," and then looked only at the search box
(not the table) would believe their search term was still active. Cheap fix:
sync the two Clear paths, or have `AdminHouseholdsPage`'s `draft` state
re-seed from the `q` prop when it changes underneath the component.

**B. No way to tell an operator "an invite existed and expired" from this
screen.** `partner@example.test`'s invite (expired 2026-08-24) is invisible
on both the metrics tile and the drill-in's "Pending invites" list — by
design, since neither query considers it pending. That is the right answer
for "is it pending," but it means an operator troubleshooting "did we ever
invite partner@example.test" has no way to find out from `/admin/households`
at all; they would need direct database access. Not a defect against any of
the fifteen criteria (all of which are about *pending* invites specifically),
but worth naming as a real gap in the screen's usefulness for support
work.

**C. The empty household (Tan, 0 members, no sessions) renders cleanly.**
Worth recording as a positive, not a defect: the members table shows just its
header row with no body rows (no crash, no "undefined" text), and "Pending
invites" correctly reads `None pending.` The zero-member, zero-session,
zero-invite case was clearly designed for, not just accidentally
non-crashing.

**D. The household lockout callout expresses its cooldown as a duration
("locked for 15 minutes"), not a clock time.** This is unrelated to anything
this task built (`SignInScreen.tsx` predates it), but it is a visible
inconsistency: the *admin* re-auth lockout screen (a different, older
subsystem — see the `2026-09-02-hearth-admin-surface-verification.md` walk)
shows an absolute unlock time ("locked until 3:45 PM"), while this
*household* sign-in lockout shows only "15 minutes." Neither is wrong on its
own, but a support engineer moving between the two screens might expect the
same convention on both. Flagged for awareness only; out of this task's
scope to fix.

---

## State changed on the dev box

- **Kris (`kris@example.test`) is now a permanent member** of "Andreas &
  Christine" (accepted via criterion 11's real accept flow, with a
  Playwright-set password). This is not reversible from the admin screen and
  was not undone after the walk — the household now genuinely has 6 members,
  and the invite row's `accepted_at` is set.
- **`admin_audit_log` grew from 0 to 33 rows** over the course of the walk —
  expected and left in place; the table is append-only with no delete path
  in the product, matching the equivalent note in the admin-surface
  verification.
- **Jamie's session** (created during criterion 1/13) and **Andreas's admin
  re-auth grants** (several, one per fresh sign-in during the walk) remain in
  `sessions` / whatever backs the admin grant; both expire naturally.
- **`login_attempts` holds 18 rows**, including the deliberate failures from
  both passes of criterion 12 (three in-browser, three more via `curl` for
  the redo); the household itself is unlocked (confirmed twice: the reload
  of the admin drill-in showing no callout, and a real `curl` sign-in
  returning 200).
- `platform_admins` already held `andreas@hearth.family` (granted before this
  task's walk began, per the environment setup) — unchanged.
- No flags, currencies, or other households were modified. The browser
  viewport was left at 1440×900 (not the 320px used for criterion 14).
