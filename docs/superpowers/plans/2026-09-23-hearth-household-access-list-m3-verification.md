# Household access list, milestone 3 — gate and mutation checks

**Run 2026-09-23**, on branch `partner-invite-lobby-m3`, against real Postgres
via testcontainers (colima, migration `00021` / goose version 21). This file
is the evidence for Task 8 of
`docs/superpowers/plans/2026-09-23-hearth-household-access-list-m3.md`. The
brief is
`.superpowers/sdd/2026-09-23-hearth-household-access-list-m3/task-8-brief.md`.
Task 9's browser walk comes after this file.

**Summary: `make lint && make test` green. One dead-code finding fixed on the
way (below, not a mutation). All four mutation checks went red for the
brief's own edit, verbatim — none needed a substitute. After reverting all
four, `make test` is green again and the working tree is clean.**

---

## 0. Full gate (Step 1)

`make lint` failed once, on the milestone's own new code, before the fix
below; green after it. `make test` was green on the first run.

### `lint-dead` (knip) flagged an unused export

```
cd web && npx --yes knip@5.88.1 --no-progress
Unused exported types (1)
NewApiToken  type  src/features/settings/useHouseholdAccess.ts:27:13
```

`NewApiToken` (`useHouseholdAccess.ts`) is the mutation input type for
`useCreateApiToken`. `NewApiTokenModal.tsx`'s submit handler called
`create.mutate({ name: ..., expiresInDays: days })` with an inline object
literal — TypeScript matched it structurally, so nothing in the codebase
ever *named* the type, which is what knip checks for, not what the compiler
checks for. Per the brief ("find the caller that should reach it rather
than deleting it"), and matching the house pattern already used for
`useInviteMember.ts`'s `RoleOption` / `InviteChannel` (imported into
`InviteMemberModal.tsx` and used to type local state, not just passed
structurally): `NewApiTokenModal.tsx` now imports `NewApiToken` and types
the payload explicitly before calling `mutate`.

```diff
-import { useCreateApiToken } from "./useHouseholdAccess";
+import { useCreateApiToken, type NewApiToken } from "./useHouseholdAccess";
...
-            create.mutate({ name: name.trim(), expiresInDays: days });
+            const input: NewApiToken = { name: name.trim(), expiresInDays: days };
+            create.mutate(input);
```

`make lint` re-run: green (`arch-lint`, `tsc`, `eslint`, `deadcode`,
`staticcheck`, `knip`, `go vet` all pass).

### `make test`

```
ok  	.../cmd/adminctl	0.540s
ok  	.../cmd/api	5.852s
ok  	.../cmd/hearthctl	0.316s
ok  	.../internal/adapter/crypto	1.291s
ok  	.../internal/adapter/fx	0.272s
ok  	.../internal/adapter/http	284.798s
ok  	.../internal/adapter/intent	0.762s
ok  	.../internal/adapter/mail	0.316s
ok  	.../internal/adapter/openrouter	0.770s
ok  	.../internal/adapter/postgres	292.952s
ok  	.../internal/adapter/telegram	0.389s
ok  	.../internal/config	0.297s
ok  	.../internal/domain	0.284s
ok  	.../internal/usecase	1.401s
...
 Test Files  108 passed (108)
      Tests  952 passed (952)
```

No `FAIL` line anywhere in the run. `hearthctl`'s route-table test (which
diffs its table against `router.go` both ways) passed, confirming the
`GET /household/access` row Task 1 added to
`api/cmd/hearthctl/routes.go` matches the router.

---

## 1. Mutation checks (Step 2)

Each mutation was applied, the named test run, the failing line copied
below, then the file restored with `git checkout -- <file>` (mutation 3 also
re-ran `make sqlc`). `git status --short` was empty after each revert. None
of the four turned `TestAnOwnerCannotRevokeAnotherMembersToken` red — it was
left running green throughout, confirming it guards the existing `DELETE`
route rather than any of this milestone's new code.

### Mutation 1 — the handler serves `ForHousehold` to a limited member

`api/internal/adapter/http/access_handlers.go`, `case domain.RoleLimited:`:

```diff
-			list, err = deps.Access.ForMember(r.Context(), scope.HouseholdID, scope.UserID, withChats)
+			list, err = deps.Access.ForHousehold(r.Context(), scope.HouseholdID, withChats)
```

`go test ./internal/adapter/http/ -run TestALimitedMemberSeesOnlyTheirOwnTokensAndChat -count=1`:

```
--- FAIL: TestALimitedMemberSeesOnlyTheirOwnTokensAndChat (1.93s)
    access_api_test.go:124: tokens = [{ID:2984d46e... MemberName:Ethan ...} {ID:9f6f06cd... MemberName:Andreas ...}], want only Ethan's own
```

Red for the right reason: the limited member ("Ethan") saw the owner's
("Andreas") token too. Reverted; `git status --short` empty.

### Mutation 2 — the access route drops its cookie-only guard

`api/internal/adapter/http/router.go`, the access route's group:

```diff
 			g.Group(func(a chi.Router) {
-				a.Use(requireCookieSession)
 				a.Get("/household/access", handleHouseholdAccess(deps))
 			})
```

`go test ./internal/adapter/http/ -run TestTheAccessListRefusesAnAPIToken -count=1`:

```
--- FAIL: TestTheAccessListRefusesAnAPIToken (1.47s)
    access_api_test.go:140: GET access with a token: 200 {"telegramEnabled":false,"tokens":[{"id":"ff5046a8-...","memberId":"085d9c1b-...","memberName":"Andreas","name":"claude", ...}],"chats":[]}
        , want 403 SESSION_REQUIRED
```

Red for the right reason: an API-token-authenticated request that should
have been refused with `403 SESSION_REQUIRED` (spec decision 5) instead got
a `200` with the caller's own token list. Reverted; `git status --short`
empty.

### Mutation 3 — `ListLiveAPITokensForHousehold` loses its household filter

`api/internal/adapter/postgres/queries/api_token.sql`:

```diff
 SELECT * FROM api_tokens
-WHERE household_id = $1 AND revoked_at IS NULL AND expires_at > now()
+WHERE $1::uuid IS NOT NULL AND revoked_at IS NULL AND expires_at > now()
 ORDER BY created_at DESC;
```

Unlike the equivalent mutations in the milestone-1 verification file, this
one **builds cleanly** — `$1` is still referenced, just not as the scope —
so `make sqlc` succeeds and the test runs as the brief wrote it.

`go test ./internal/adapter/postgres/ -run TestListTokensForHouseholdShowsEveryMembersLiveTokensAndNoOtherHouseholds -count=1`:

```
--- FAIL: TestListTokensForHouseholdShowsEveryMembersLiveTokensAndNoOtherHouseholds (1.46s)
    access_list_repo_test.go:104: ListForHousehold() names = [stranger-token partner-script owner-laptop], want [partner-script owner-laptop] (newest first, ours only)
```

Red for the right reason: `stranger-token`, which belongs to a second,
unrelated household, leaked into the result. Reverted with
`git checkout -- api/internal/adapter/postgres/queries/api_token.sql`, then
`make sqlc` run again; `git status --short api/` printed nothing, so no
stale generated code was left under `sqlcgen/`.

### Mutation 4 — `ApiTokenList` shows Revoke on every row

`web/src/features/settings/ApiTokenList.tsx`:

```diff
-        <TokenRow key={t.id} token={t} isMine={t.memberId === myUserId} />
+        <TokenRow key={t.id} token={t} isMine={true} />
```

`npx vitest run src/features/settings/ApiTokenList.test.tsx`:

```
FAIL  src/features/settings/ApiTokenList.test.tsx > ApiTokenList > offers Revoke on my own tokens only
AssertionError: expected [ <button …(2)></button>, …(1) ] to have a length of 1 but got 2
 ❯ src/features/settings/ApiTokenList.test.tsx:44:63
```

Red for the right reason: a second Revoke button appeared on the partner's
row, which the test's `toHaveLength(1)` assertion catches. Reverted;
`git status --short` empty.

### After all four reverts

```
git status --short   # empty
make test             # 108 test files, 952 tests, all passed; no FAIL line
```

---

## 2. What this does and does not prove

This file proves the four specific failure modes the brief named: a role
check that serves the wrong scope, a route that forgets its session guard, a
query that forgets its household filter, and a frontend that forgets whose
row it is rendering. It does not repeat the milestone's browser walk — that
is Task 9 — and it does not claim any coverage beyond the four checks and
the full `make lint && make test` run recorded above.

---

## 3. Browser walk (Task 9)

**Walked 2026-09-23, 01:31–01:45 UTC, in real Chromium** (the
chrome-devtools MCP) against the running Docker stack at
`http://localhost:5173`. **Result: 14 of 14 passed.** No product defect
found. Two environment problems were found and fixed on the way (below).

### Environment — proving the served build is this branch

- `lsof -i :5173` → held by Docker Desktop (`com.docke…`). `docker --context
  desktop-linux ps` showed the `hearth` compose project, working dir this
  repo; colima had nothing running. The shell's default context is colima,
  so every compose command needed `DOCKER_CONTEXT=desktop-linux`.
- **The dev database was one migration behind:** `docker compose run --rm
  migrate` applied `00021_invite_channels.sql` (goose version 21). Then
  `docker compose up -d --build api web`; api log: `telegram sign-in and
  chat commands enabled bot_username=HearthOinkDevBot`.
- **The web container served a Vite crash overlay, not the app:**
  `Failed to resolve import "qrcode-generator" from
  "src/features/settings/InviteLinkShare.tsx"`. The `hearth-node-modules`
  named volume predates that dependency; `docker restart hearth-web-1` re-ran
  `npm install` and the page loaded. `make test` could never see this — the
  suite does not use that volume.
- Branch proof: `GET /api/v1/household/access` unauthenticated → **401
  `UNAUTHENTICATED`**, while an unknown route (`/api/v1/zzz-nonexistent`) →
  **404 `NOT_FOUND`**, so the route exists; and Settings shows the **Access**
  card.
- `make seed`, `make hearthctl`. Sign-in for every session was by
  **one-time magic link** read from Mailpit, not by typing passwords.

**Three isolated sessions**, one browser, separate cookie jars
(chrome-devtools `isolatedContext`): **owner** = Andreas (owner, platform
admin), **partner** = Christine (owner — `/auth/me` in that context returned
`Christine`, role `owner`, proving isolation), **kid** = Jamie (limited,
money only; the seeded Kayla/Ethan have no email and cannot sign in). This
differs from the brief's Claude-in-Chrome + Playwright pairing: the
Claude-in-Chrome window returned screenshots that did not match its own
viewport, so it was dropped after criterion 1's first read.

### Criteria

| # | Result | What was actually seen |
|---|---|---|
| 1 | ✅ | Owner's Settings: `Access` h2, "Every way into your household besides a password.", `API TOKENS` h3 and `LINKED CHATS` h3 (Telegram is on in this stack). No other card mentions Telegram — the only occurrence is the `Connect Telegram` button inside Linked chats. |
| 2 | ✅ | New token → dialog "New API token", Name empty, Expires after = **90 days** (options 30 days / 90 days / 1 year), Create disabled until named. Named `walk-owner`, Create → "Your new token", "Copy it now. You won't see it again", secret `hearth_DM3P…EXg` (51 chars), buttons **Copy** and **Done**. |
| 3 | ✅ | Done, then New token: one open dialog, `:modal` true, Name `""`, select `90`. `document.documentElement.outerHTML.includes(<secret fragment>)` → **false**; no `hearth_…` string anywhere in `body.innerText`. |
| 4 | ✅ | Row: `walk-owner · Andreas` / `DM3Pkdd2… · Never used · Expires Dec 22, 2026` / **Revoke**. Sep 23 + 90 days = Dec 22. |
| 5 | ✅ | `HEARTH_TOKEN=<secret> hearthctl login --token` → "token accepted for andreas@hearth.family", exit 0; `hearthctl whoami` → Andreas, role owner, exit 0. After reload the row read `Last used Sep 23, 2026`. |
| 6 | ✅ | `curl -H "Authorization: Bearer <secret>" …/api/v1/household/access` → **HTTP 403** `{"error":{"code":"SESSION_REQUIRED",…}}`. |
| 7 | ✅ | Partner created `walk-partner` (secret shown once). Owner reload: row `walk-partner · Christine` / `6ebkhkpI… · Never used · Expires Dec 22, 2026` with **no buttons** in the row; the owner's own two rows each have Revoke. |
| 8 | ✅ | Partner clicked Revoke → the row changed in place to **Yes, revoke / Keep** (in-page, no `window.confirm`, no dialog). Yes, revoke → row gone from the partner's list. Owner reload → row gone. The revoked secret then got **401** on `/auth/me`. |
| 9 | ✅ | Owner Revoke → Yes, revoke → list shows only the old `test` row. `hearthctl whoami` → "the API answered 401 … Run: hearthctl login", **exit 2**. |
| 10 | ✅ | Owner invited "Walk Invitee" (Kid, Telegram link) → Access card: **"1 pending invite — in Members"** (a button, 44px tall). Scrolled to page bottom (Members h2 at −709px), clicked it → `scrollY` 110, Members h2 at **23px** from the top. After withdrawing the invite the pointer was gone. |
| 11 | ✅ | Kid's Access: "Your own ways in besides a password.", `API TOKENS` → "No API tokens." while the owner has a live `test` token; no invite pointer (the owner's invite was pending at the time); Linked chats shows only Connect. After the kid made its own token, only that one row showed. Network (xhr/fetch) on load: `auth/me, household/members, spaces, household, currencies, notification-preferences, household/access, auth/telegram` — **no `/household/invites`**, confirmed again from `performance.getEntriesByType('resource')` after a reload. |
| 12 | ✅ (scoped) | Dev bot `@HearthOinkDevBot` is configured. **Connect**: opened `https://t.me/HearthOinkDevBot?start=<code>` in a new tab; the card switched to "Open Telegram and press Start … We'll check automatically." **The link handshake itself was not exercised** — nothing could press Start in a Telegram client. To test the linked state, a `telegram_accounts` row for Andreas (`chat_username walk_fake_chat`, fake chat id, nudges off) was **inserted by SQL**. Owner reload: "Connected as @walk_fake_chat · Linked Sep 23, 2026 · **Disconnect**"; partner saw `@walk_fake_chat · Andreas` with no Disconnect. Owner clicked **Disconnect** (real click) → list refreshed at once to "Connect Telegram"; DB count for that user → 0; partner reload no longer lists it. |
| 13 | ✅ | `resize_page` stops at the window's 500px minimum, so a 375×812 mobile viewport was emulated. Owner and kid pages: `innerWidth 375`, `documentElement.scrollWidth 375`, `scrollBy(50,0)` left `scrollX 0`. The 59-char name `walk-kid-a-very-long-token-name-for-the-narrow-screen-check` renders `walk-kid-a-very-long-token-nam…` — its div is `overflow:hidden; text-overflow:ellipsis`, width 220, scrollWidth 450 (the two spans past the viewport edge are inside that clipped div). New token 82×44, Revoke 65×44, Connect Telegram 122×44; `elementFromPoint` at each centre hits the button. Tapping Revoke gave Yes, revoke 84×44 / Keep 53×44 in-row; tapping New token opened a `:modal` dialog 375 wide with no page overflow. |
| 14 | ✅ | No console errors or warnings in any of the three sessions. Owner: a final no-reload pass (create `walk-console` → Done → New token → Cancel → Revoke → Yes, revoke → Connect Telegram) then `list_console_messages` (error/warn/issue) → **none**. Partner and kid: only Vite/React DevTools info. api log for the window: no error lines. |

### Seen, not a failure of this milestone

- The kid's page raises a DevTools **issue** (not a console error):
  "Incorrect use of `<label for=FORM_ELEMENT>`" — `CurrencyPanel.tsx`'s
  `primary-currency` label points at a read-only text for limited members.
  Last touched 2026-09-19 (#26), not on this branch. Follow-up.
- Focus stays on the "1 pending invite" button after it scrolls to Members,
  rather than moving to the Members card. Not a criterion; worth a look.
- Telegram **Disconnect** and invite **Withdraw** act on one click with no
  confirm step, unlike token Revoke. Not a criterion; noted for consistency.

### State left in the dev database

All walk tokens revoked (`walk-owner`, `walk-partner`, `walk-console`, the
kid's long-named one), the invite withdrawn, the fake chat row removed by
Disconnect. **Changed and not reverted:** Christine's and Jamie's
`password_hash` were set to a copy of Andreas's seeded hash
(`hearth-dev-password`) before switching to magic links; `make
reset-password` sets them back to anything else.

## Follow-up fixes (2026-09-23)

The three items under "Seen, not a failure of this milestone" above, plus
the leftover shared password hash, closed out as a separate small change on
`partner-invite-lobby-m3` (commits `94f6b50`, `2751382`, `6a63600`, `6d400a4`
docs, `19e5f51` a review-fix on `2751382` — see Item 4 below — and `6d218f6`
a docs-only correction to a claim this doc's own `expect` note made without
testing it). Criterion 12 above is untouched — the fake `telegram_accounts`
row it used was already removed by that walk's own Disconnect click; this
follow-up does not re-walk Telegram, the controller does that separately
with the owner.

### Criterion 12, re-walked with a real Telegram client (2026-09-24)

The SQL-inserted row above proved the *linked* state; this proves the
*handshake* with a real person pressing Start. Owner (Andreas, signed in on
the Docker Desktop stack at http://localhost:5173, same branch) clicked
**Connect Telegram** in the Access panel's Linked chats group; it opened
`https://t.me/HearthOinkDevBot?start=<nonce>`. The product owner pressed
Start in their own Telegram client (`@Andreas_oen`).

- **The round trip is real.** `telegram_link_requests` row `7ebd624c…` was
  created 23:31:48 UTC and consumed 23:31:52 UTC with a chat id set — the
  dev bot received Start and the API redeemed the nonce four seconds later.
- **The answer was a refusal, and the right one.** `GET
  /api/v1/auth/telegram/link/7ebd624c…` →
  `{"status":"refused","chatUsername":"Andreas_oen","reason":"That Telegram
  chat is already connected to another Hearth account."}`. That chat is bound
  to the Telegram-only account created by the Telegram sign-up walk on
  2026-09-01; one chat belongs to one account (the UNIQUE on
  `telegram_accounts.chat_id`), so Confirm was correctly never offered.
- **"Link expired" on the second tap is the single-use nonce working.**
  Opening the t.me page made Telegram Desktop send Start at once, which spent
  the nonce; the owner's own later tap hit an already-consumed link.
- **The card still read "Open Telegram and press Start…"** while Chrome was in
  the background (`document.visibilityState` = `hidden`). That is
  `refetchIntervalInBackground: false` doing its job, not a stuck poll — the
  status endpoint already answered `refused`.

**Not exercised live:** the Confirm step and the "Connected as @…" state
reached through a real handshake. The owner chose not to free the chat by
deleting the 2026-09-01 test account's binding. Confirm stays covered by
`TelegramConnection.test.tsx`, and the linked state by the SQL-row walk
above. Verdict for criterion 12: ✅ real round trip, refusal path; Confirm
path by test only.

### Item 2 — dev database passwords

Christine's and Jamie's `password_hash` still matched Andreas's exactly
(copied during this walk and never reverted). Reset each independently with
the project's own tool: `docker compose exec api go run ./cmd/adminctl
reset-password --email=<email>`, driven through `expect` rather than `-T`
(see LEARNING.md's correction to this walk's "an agent cannot drive it"
note — `-T` disables the container pty that `term.ReadPassword` needs;
`expect` supplies one). Verified by `SELECT email, password_hash FROM
users WHERE email IN (...)`: all three hashes now distinct. New credentials
are in the engineering report, not here (`.superpowers/sdd/m3-leftovers-
report.md`) — this file is read by more people than need them.

### Item 3 — CurrencyPanel dangling label

`CurrencyPanel.tsx:93` fix: `<label htmlFor="primary-currency">` only when
`isOwner` (the branch that also renders the input); a plain `<span
className="text-ink">Primary currency</span>` otherwise. RED: a new
CurrencyPanel test asserted every `label[for]` in the rendered tree resolves
via `getElementById` — failed with `expected null not to be null` against
the unfixed component. GREEN after the fix; full CurrencyPanel.test.tsx
(7/7) and the whole frontend suite (958/958) still pass. Checked the rest of
the file and the settings feature directory (`grep -rn 'htmlFor='
web/src/features/settings/`) for the same shape — every other `htmlFor` is
inside a modal whose control always renders with its label, so no sibling
fix was needed. Browser: signed in as Jamie (limited member) with the new
password from item 2 — itself end-to-end proof that reset worked —
`document.querySelectorAll('label[for]')` on `/settings` returned `[]`.

### Item 4 — focus after the pending-invite pointer

`MembersPanel.tsx`'s `<h2>` inside `<section id="members">` now carries
`id="members-heading"` and `tabIndex={-1}`. `AccessPanel.tsx`'s pointer
button scrolls `#members` (the whole card, unchanged from before either
fix) into view and then, separately, calls `.focus({ preventScroll: true
})` on `#members-heading`.

The first commit (`2751382`) scrolled straight to `#members-heading` — one
element doing both jobs. A review pass before declaring the branch done
(`19e5f51`) caught that this silently moved the scroll landing position
from the card's own top edge (padding and border included) to the
heading's, a behaviour change nobody asked for, and made `preventScroll`
on the focus call meaningless — it only does anything when the scroll
target and the focus target are different elements, which they weren't.
Fixed by giving each job its own `getElementById` call, and strengthened
the test to assert `scrollIntoView`'s call target is `#members`, not the
heading (proven to fail against the single-target version first).

RED: a new AccessPanel test rendered the panel next to a stand-in
`<section id="members"><h2 id="members-heading" tabIndex={-1}>` (jsdom has
no `scrollIntoView`, stubbed with `vi.fn()`), focused the pointer, clicked
it, and asserted `document.activeElement` was the heading — failed (focus
stayed on the button) against the unfixed component. GREEN after the fix.
Browser (re-run after `19e5f51`): signed in as Andreas, created a real
pending invite ("Recheck", Telegram channel), clicked "1 pending invite —
in Members", and read both the scroll position and `document.activeElement`
back from the page —
`{"activeTag":"H2","activeId":"members-heading","sectionTop":0,"headingTop":23}`:
the `#members` card's top edge lands at the viewport top (`sectionTop: 0`,
matching this same walk's criterion 10, which recorded the heading landing
at 23px — the card's own top padding), while focus is on the heading
specifically (`activeId: "members-heading"`), not the card. Invite
withdrawn afterward.

### Item 5 — Telegram Disconnect confirm step

`TelegramConnection.tsx`: Disconnect now asks first, using the same
`useConfirmAction` pattern as `ApiTokenList`'s Revoke — "Disconnect this
chat? Reminders and bot commands stop working there." with "Yes,
disconnect" / "Keep" buttons, `disconnect.mutateAsync()` inside `confirm()`.
RED (component reverted with `git stash` to isolate the test change from the
fix): the updated "shows the connected chat and offers Disconnect" test and
a new "sends no DELETE ... when cancelled" test both failed against the old
one-click component, the first on `findByText("Disconnect this chat?
...")` timing out. GREEN after restoring the fix; full
TelegramConnection.test.tsx + AccessPanel.test.tsx (16/16) pass. Browser: no
real chat was connected for Andreas in this dev database (the criterion-12
fake row had already been cleaned up by that walk's own Disconnect click),
so a temporary `telegram_accounts` row was inserted by SQL for this
verification only, exactly as criterion 12 above did. Clicked Disconnect →
saw the confirm line and both buttons → clicked **Keep** (never "Yes,
disconnect" — the controller re-walks the real Telegram flow with the owner
separately) → confirmed by SQL that the row was untouched. The temporary row
was then deleted and the page reloaded to confirm the UI matched ("Connect
Telegram" again).

### Item 6 — Withdraw confirm (2026-09-24)

`PendingInvitesList.tsx` (the email row) and `PendingInviteCard.tsx` (the
Telegram card — `docs/LEARNING.md`'s 2026-09-23 M3-leftovers bullet named
this the sibling left open when Item 5 above shipped) fixed the same way:
Withdraw now asks first, using the same `useConfirmAction` pattern as
ApiTokenList's Revoke and TelegramConnection's Disconnect (Item 5) —
"Withdraw this invite? The link stops working." with "Yes, withdraw" /
"Keep" buttons, `withdraw.mutateAsync(id)` inside `confirm()`.
`PendingInvitesList.tsx` keys one `useConfirmAction()` instance by invite
id, since that one hook instance now serves every email row in the list;
`PendingInviteCard.tsx` mounts one card per invite, so it stays on the
default single-item key, same as `ApiTokenList`'s own per-row `TokenRow`.

RED (implementation reverted with `git stash push -- PendingInviteCard.tsx
PendingInvitesList.tsx`, test changes kept, the same isolation Item 5 used):
9 of 42 tests across the three touched files failed against the old
one-click components — the updated "withdraws an invite … after a confirm"
and "withdraws a knocked invite, after a confirm" tests (no "Yes, withdraw"
button to find), the double-click test rewritten to target "Yes, withdraw"
instead of the old Withdraw trigger, two new "sends no DELETE and returns to
Withdraw when the confirm is cancelled" (Keep) tests, a new "confirming one
invite's withdraw does not open the confirm pair on another" test, and
`MembersPanel.test.tsx`'s existing 409 test. The other 33 tests in the same
three files passed unchanged. GREEN after restoring the fix: `PendingInvitesList.test.tsx`
+ `PendingInviteCard.test.tsx` + `MembersPanel.test.tsx` (42/42) and the
whole frontend suite (961/961) pass; `npx tsc --noEmit -p .`, `npx eslint
src/features/settings/` and `make lint` (arch-lint, tsc, eslint, deadcode,
staticcheck, knip, `go vet`) all clean.

Mutation check: changed `PendingInviteRow`'s trigger button's `onClick` from
`onAsk` to `onConfirm` (bypassing the gate so a click goes straight to the
DELETE). 6 of 12 `PendingInvitesList.test.tsx` tests failed for the expected
reason — no "Yes, withdraw" button ever rendered, since the row skipped the
confirm step entirely. Reverted; suite green again.

Sibling grep (`docs/LEARNING.md`'s own "before you call something done"
checklist item 3): `grep -rn '\.mutate(\|\.mutateAsync('
web/src --include="*.tsx"` filtered to Delete/Remove/Revoke/Disconnect/
Withdraw/Discard-labelled buttons found three more hits —
`HoldingIncomePanel.tsx`, `HoldingLotsPanel.tsx`, `TransactionsPage.tsx` —
all three already gated (the first two through their own `useConfirmAction`
instance, the third through `TransactionModal`'s in-modal confirm; both are
named in `useConfirmAction.ts`'s own header comment as pre-existing
callers). No further fix needed.

Browser (Docker Desktop stack, signed in as `andreas@hearth.family`):
created a Telegram invite ("Withdraw Test Invite") via **+ Invite**, closed
the modal, and drove the row in Settings' own Pending invites list — not the
modal's copy of the same card. Clicked **Withdraw** → saw "Withdraw this
invite? The link stops working." with **Yes, withdraw** / **Keep** →
clicked **Keep** → invite still listed, Withdraw button back, no DELETE in
the request log. Clicked **Withdraw** again → **Yes, withdraw** → the
invite and the whole "Pending invites" heading disappeared, and the Access
panel's "1 pending invite — in Members" pointer button disappeared with it.
No console errors or warnings at any point. Repeated at `375x812x2,mobile,
touch` (this doc's own criterion-12 note on the resize-page minimum) with a
second invite ("Phone Width Invite"): the confirm line and both buttons
render full width below the row, both meet the 44px touch target, nothing
clips. Console checked again afterward, still clean.

Commit: `fix(web): ask before withdrawing an invite` (`55cea7f`).
