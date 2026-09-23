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
