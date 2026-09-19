# Partner invite lobby, milestone 1 — verification

> **STATUS: WALKED, 2026-09-19. 15 of 15 criteria pass, one of them
> (criterion 7's "private window") met by the closest real equivalent, which
> is named below.** All four mutations went red for the right reason. Mutation
> 4 first exposed a test fixture that could not catch it; that was fixed in
> its own commit (`7fe48bc`) before the gate ran. Mutations 1 and 2 could not
> run as the brief wrote them (they do not build), so each ran as its closest
> building equivalent, recorded below. `make lint && make test` green on
> `7fe48bc`. No product defect was found. Four observations are recorded at
> the end, none of them a milestone-1 regression.

This file is the evidence for Task 6 of
`docs/superpowers/plans/2026-09-19-hearth-partner-invite-lobby-m1.md`. The
brief is `.superpowers/sdd/2026-09-19-hearth-partner-invite-lobby-m1/task-6-brief.md`.
The feature: an owner sees every invite the household has sent that nobody
has accepted and that has not expired, in Settings → Members, and can
withdraw one; and Overview's "Finish setting up" gains a fourth step,
"Invite your partner" (spec:
`docs/superpowers/specs/2026-09-19-hearth-partner-invite-lobby-design.md`).

---

## 1. Mutation checks

Each mutation was applied, the named test run, the failure read, and the file
restored with `git checkout -- <file>`. After mutations 1 and 2 were reverted,
`make sqlc` ran again and `git status --short api/` printed nothing, so no
change was left under `sqlcgen/`.

### Mutation 1 — `ListPendingInvites` loses its household scope

**The brief's literal edit does not build.** Deleting `household_id = $1 AND `
leaves `$2` with no `$1`, and `make sqlc` refuses the file:

```
internal/adapter/postgres/queries/identity.sql:167:1: could not determine data type of parameter $1
make: *** [sqlc] Error 1
```

A generator error never reaches the test, so it proves nothing about it. The
closest edit that removes the scope **and still builds** keeps `$1` but makes
it match every row:

```sql
WHERE (household_id = $1 OR TRUE) AND accepted_at IS NULL AND expires_at > $2
```

`go test ./internal/adapter/postgres/ -run '^TestListPendingInvites$'`:

```
--- FAIL: TestListPendingInvites (1.37s)
    invite_repo_test.go:379: pending = [{... Name:Christine Email:christine@hearth.family ...} {... Name:Stranger Email:stranger@example.com ...}], want exactly Christine's invite
```

Red for the right reason: the other household's "Stranger" invite leaked into
the list. Restored; green again (`--- PASS: TestListPendingInvites (1.29s)`).

### Mutation 2 — `DeleteUnacceptedInvite` loses its household scope

**The brief's literal edit does not build either.** Deleting
` AND household_id = $2` leaves one parameter, so sqlc generates
`DeleteUnacceptedInvite(ctx, id)` with no params struct, and the repository
stops compiling:

```
internal/adapter/postgres/invite_repo.go:223:52: undefined: sqlcgen.DeleteUnacceptedInviteParams
```

Same substitute as mutation 1:

```sql
WHERE id = $1 AND (household_id = $2 OR TRUE) AND accepted_at IS NULL
```

`go test ./internal/adapter/postgres/ -run '^TestDeleteInvite$'`:

```
--- FAIL: TestDeleteInvite (1.31s)
    invite_repo_test.go:422: delete through another household: got <nil>, want domain.ErrNotFound
```

Red for the right reason, on the assertion **one line before** the one the
brief quotes. Both sit in the same block and check the same property: the
test first asks "did the delete through another household answer not
found?" and stops there, so it never reaches "must survive a delete scoped to
another household". Restored; green again (`--- PASS: TestDeleteInvite (0.96s)`).

### Mutation 3 — the withdraw route loses `requireCookieSession`

Deleted `c.Use(requireCookieSession)` from `router.go` (line 287).
`go test ./internal/adapter/http/ -run '^TestATokenCanListPendingInvitesButNotWithdrawThem$'`:

```
--- FAIL: TestATokenCanListPendingInvitesButNotWithdrawThem (1.35s)
    pending_invites_api_test.go:116: a token withdrawing an invite: 204
```

Red for the right reason: without the guard a bearer token withdrew the
invite. Restored; green again (`--- PASS ... (1.48s)`).

### Mutation 4 — `partnerStep` counts the signed-in owner as the partner

Changed `>= 2` to `>= 1` in `web/src/features/overview/partnerStep.ts`.

**First run: only half the expected tests failed.** `partnerStep.test.ts` went
red, but **every `OverviewPage.test.tsx` test stayed green**, including
"shows a fresh household what is left to set up", which the brief says must
fail:

```
× is invited while an owner-role invite is pending
× ignores limited members and limited invites
AssertionError: expected 'joined' to be 'none'
Test Files  1 failed | 1 passed (2)
Tests  2 failed | 22 passed (24)
```

**Why: the Overview tests' default roster was `[]`.** Its comment called an
empty member list "a household still waiting for its partner". No real
household has zero members: the owner who created it is always one. With zero
owners, `0 >= 2` and `0 >= 1` are both false, so the page tests could not tell
the real rule from the mutated one. The Task 5 implementer's own mutation
(`>= 2` → `> 2`) went the other direction, which the explicit `TWO_OWNERS`
fixture does catch, so this gap was not visible then.

**Fixed in the open, in its own commit** (`7fe48bc test(overview): default the
roster to one owner, not an impossible empty household`): the default roster
is now `ONE_OWNER` (Sam, the signed-in owner), with a comment at the fixture
saying why it must never be `[]`. All 52 overview tests stay green on the real
code. Re-running the mutation:

```
× is invited while an owner-role invite is pending
× ignores limited members and limited invites
× shows a fresh household what is left to set up
× tells an owner whose partner is invited that the invite is on its way
 FAIL  OverviewPage.test.tsx > shows a fresh household what is left to set up
TestingLibraryElementError: Unable to find an element with the text: 1 of 4 done.
 FAIL  OverviewPage.test.tsx > tells an owner whose partner is invited that the invite is on its way
TestingLibraryElementError: Unable to find an element with the text: Invite sent — waiting for your partner.
AssertionError: expected 'joined' to be 'invited' // Object.is equality
AssertionError: expected 'joined' to be 'none' // Object.is equality
Test Files  2 failed (2)
Tests  4 failed | 20 passed (24)
```

Red for the right reason in both files: the lone owner is counted as the
partner, so the step reads as done ("1 of 4" never appears) and the "Invite
sent" line never shows. Restored; `npx vitest run src/features/overview/`
green again (`Tests 52 passed (52)`).

---

## 2. Full gate

`make lint && make test` on `7fe48bc` (the fixture fix included), colima's
Docker socket for testcontainers. Exit 0. Closing lines, verbatim:

```
./scripts/arch-lint.sh
architecture lint passed
cd web && npx tsc --noEmit
cd web && npm run lint
> eslint .
deadcode passed
staticcheck passed
cd web && npx --yes knip@5.88.1 --no-progress
cd api && go vet ./...
=== MAKE LINT EXIT 0 ===
cd api && go test ./... -count=1 -timeout=20m
ok  	github.com/andreasoentoro/hearth/api/cmd/adminctl	0.329s
ok  	github.com/andreasoentoro/hearth/api/cmd/api	5.339s
ok  	github.com/andreasoentoro/hearth/api/cmd/hearthctl	0.358s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/crypto	0.881s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/fx	0.276s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/http	243.433s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/intent	0.272s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/mail	0.803s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/openrouter	0.798s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/postgres	264.970s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/telegram	0.378s
ok  	github.com/andreasoentoro/hearth/api/internal/config	0.281s
ok  	github.com/andreasoentoro/hearth/api/internal/domain	0.275s
ok  	github.com/andreasoentoro/hearth/api/internal/usecase	1.412s
cd web && npx vitest run
 Test Files  103 passed (103)
      Tests  907 passed (907)
=== GATE EXIT 0 ===
```

The commits after `7fe48bc` change only documentation.

---

## 3. Browser walk

### Setup

- **Which engine serves 5173.** `lsof -nP -iTCP:5173 -sTCP:LISTEN` showed
  `ssh` (PID 49674), which is colima's port forwarder. Docker Desktop would
  show `com.docker.backend`. Docker Desktop's socket was not even up
  (`connect: no such file or directory`), so colima was the only engine.
- `make up` (not `make dev`), then **`docker restart hearth-api-1
  hearth-web-1` after every mutation had been reverted and after the last
  frontend edit** (`7fe48bc`, a test file). The restart matters because the
  api container bind-mounts `./api` and hot-reloads, and three of the four
  mutations touched files under it.
- **The running API had the new route.** `GET /api/v1/household/invites`
  answered `401 UNAUTHENTICATED` (route exists, needs a session), while an
  unknown path under `/api/v1/household/` answered `404`.
- `make seed` → "Andreas & Christine", Andreas signed in. That household
  already has **two** owners (Christine accepted in an earlier session), so
  criteria 9–13, which need one owner, ran on a **fresh household made by
  self-serve sign-up** ("Walk Test Household", owner Sam), in its own isolated
  browser context.
- **Tools.** Criteria 1–7 (invite half) used Playwright MCP. Its server
  dropped mid-walk ("Connection closed"), and criteria 7 (owner half) to 15
  used Chrome DevTools MCP. Every action was a real click or key press,
  except these: the token for criterion 15 was created with `fetch` from the
  owner's page, as the brief allows (`POST /api/v1/auth/tokens` from the
  browser session), and a few `evaluate_script` reads measured layout and
  focus.

### Criteria

| # | Result | Evidence |
|---|---|---|
| 1 | **Pass** | Settings → "+ Invite" → typed "Jane", chose **Parent** (the modal's word for the owner role), typed `jane@example.com`, clicked Send invite. Network: `POST /api/v1/household/members/invite` → `201`, then `GET /household/members` and `GET /household/invites` refetches. No reload: after the `POST` the network log holds only those API calls, and the URL stayed `/settings`. No `<dialog>` left in the DOM. The **Pending invites** heading and Jane's row appeared |
| 2 | **Pass** | Row text: `Owner · jane@example.com · Expires Sep 26`, which is 2026-09-19 plus seven days |
| 3 | **Pass** | Mailpit held "Andreas invited you to Hearth", to `jane@example.com`. Clicking its link opened `/invite/hFkY…` showing "Andreas invited you in." and "Joining as co-owner — full access, equal say on every agreement." (with a "Signed in as Andreas" banner, because that tab shared the owner's cookies) |
| 4 | **Pass** | Clicked Withdraw on Jane. Network: exactly one `DELETE /api/v1/household/invites/023a5c1f-f62c-433e-9232-67d8715b1cff` → `204`, then one `GET /household/invites`. The Pending invites section disappeared. `select count(*) from invites where email='jane@example.com'` → `0` |
| 5 | **Pass** | Reopened the same Mailpit link: "We couldn't find that invite. Check the link, or ask whoever invited you to send a new one." No preview. The console logged the API's `404` for `GET /api/v1/invites/<token>`, which is the intended answer for a deleted invite |
| 6 | **Pass** | Invited Jane again (submitted with Enter, so the keyboard path works too), then **double-clicked** Withdraw (Playwright `dblclick`). Network: exactly one `DELETE /api/v1/household/invites/0c1add8e-…` → `204`. No error alert, no leftover row |
| 7 | **Pass (interpreted)** | Invited "Kid", role Kid (limited), `kid@example.com`. Row: `Limited · kid@example.com · Expires Sep 26`. **"Private window" was met by a separate browser context** (Playwright `browser.newContext()`, Chromium's own mechanism for an incognito window). No MCP tool opens a real incognito window. That context had no Hearth cookies. From Mailpit in that context: the preview had **no** "Signed in as" banner and read "Joining as Kid — calendar & chores only". Typed a password with real key presses → Accept → landed on Overview as Kid. **Owner side** (Chrome DevTools, Andreas's default context, fresh navigation to `/settings`): Kid listed under Members as "Kid · calendar & chores only", and no Pending invites section |
| 8 | **Pass** | Kid signed in inside an isolated Chrome DevTools context (`isolatedContext=kid`), then Overview → Settings. Members panel: five members listed, no "+ Invite", **no Pending invites section, no alert**. Network across both pages: `auth/me`, `accounts` (`403`, see observation 3), `household/members`, `spaces`, `household`, `currencies`, `notification-preferences`, `auth/telegram`. **No request to `/api/v1/household/invites`** |
| 9 | **Pass** | Signed up `sam.walk@example.com` through `/sign-up` and Mailpit's set-up link (isolated context `fresh`). Overview: "Finish setting up", "1 of 4 done", four steps: ✓ Create your household · Add an account (Set up → `/money`) · Set a budget for September (Set up → `/money/budget`) · **Invite your partner (Set up → `/settings?invite=true`)** |
| 10 | **Pass** | Clicked that Set up: URL `/settings?invite=true`, `<dialog>` open **and** `matches(':modal')` true, heading "Invite a family member", focus in its Name field |
| 11 | **Pass** | Invited "Alex", **Parent**, `alex.walk@example.com` → row `Owner · alex.walk@example.com · Expires Sep 26`. Clicked Overview in the sidebar (client-side, no reload): the step read **"Invite sent — waiting for your partner"**, its link **See invite** → `/settings`. Clicked it: `/settings`, no `<dialog>`, Alex's row visible. Still "1 of 4 done" (sent is not joined) |
| 12 | **Pass** | Alex accepted from Mailpit in isolated context `alex` ("Sam invited you in.", "Joining as co-owner") and landed on Overview with "2 of 4 done". **Back as Sam**, a click on Overview (no reload) → "2 of 4 done", **✓ Invite your partner**. Sam then used the checklist's own links: added account "Walk Checking" S$1,000.00 (net worth S$0.00 → S$1,000.00; the account's Owner picker already listed Alex) → "3 of 4 done". Created September's budget with Groceries at S$800 → Overview: **no "Finish setting up" at all**, budget card "S$0.00 of S$800.00" |
| 13 | **Pass** | Before inviting Alex, with Sam the only owner, Agreements showed "Agreements need at least two owners" / "This household has one owner." and an **Invite your partner** link. Clicking it → `/settings?invite=true`, dialog open and `:modal`, focus inside it |
| 14 | **Pass** | A pending invite with a 79-character email. Emulated a 360×740 phone viewport: `innerWidth` 360, `document.documentElement.scrollWidth` **360** (no horizontal scroll). The row's detail line has `scrollWidth` 607 against `clientWidth` 214, with `text-overflow: ellipsis` and `white-space: nowrap`. A screenshot shows "Limited · maximilian.alexander.wolfgan…". Withdraw stays on screen (right edge 321 px, 44 px tall). No element's right edge passed the viewport |
| 15 | **Pass** | Created a token from the owner's browser session (`POST /api/v1/auth/tokens` → `201`, 1-day expiry). `curl -X DELETE -H "Authorization: Bearer <token>" http://localhost:5173/api/v1/household/invites/084e8e1f-…` → **`403`** `{"error":{"code":"SESSION_REQUIRED","message":"This action needs a signed-in browser session, not an API token."}}`. The same token's `GET /api/v1/household/invites` → `200`, still listing the invite, and the Settings page still showed it. The token was revoked afterwards (`204`) |

### Console

- **Owner (Andreas) and fresh-household (Sam) sessions:** no errors and no
  warnings apart from `favicon.ico` 404s.
- **Criterion 5:** the API's `404` for the withdrawn invite's token, which is
  the behaviour under test.
- **Kid's session:** `401` on `/auth/me` before signing in (the sign-in page's
  probe), `403` on `GET /accounts` (observation 3, older than this
  milestone), and `favicon.ico` 404s.
- **Alex's session:** `401` on `/auth/me` before accepting (the invite page's
  signed-out probe).

### Rows the walk created

Removed:

- Jane's two invites and Maximilian's invite (withdrawn in criteria 4, 6 and
  15; rows deleted).
- Kid's **membership** in "Andreas & Christine", removed through the product's
  own `DELETE /api/v1/household/members/{id}` (`204`). The seeded household is
  back to its four seeded members.

Still present, named here:

- **Kid's invite row** (`kid@example.com`, accepted). An accepted invite is
  history and cannot be withdrawn (`409`), by design.
- **The user `kid@example.com`** ("Kid"). It no longer has a membership, and
  there is no product route to delete a user.
- **"Walk Test Household"**, with its users **Sam** (`sam.walk@example.com`)
  and **Alex** (`alex.walk@example.com`), Alex's accepted invite, the account
  "Walk Checking", September's budget (Groceries S$800), its seeded
  categories and the sign-up row. There is no product route to delete a
  household.
- **API token `task6-walk-criterion15`**: revoked, the row remains.
- **Six Mailpit messages**, to `jane@example.com` (×2), `kid@example.com`,
  `maximilian…@a-very-long-household-domain.example.com`,
  `sam.walk@example.com` and `alex.walk@example.com`.

A later `make seed` is unaffected: it reports the seeded household as it
always has.

### Observations (none is a milestone-1 regression)

1. **"Invite your partner" opens the modal on Kid.** Both doors to
   `/settings?invite=true` (Agreements since 2026-09-07, Overview's checklist
   since this milestone) open the invite modal with **Role: Kid** selected. A
   first-time owner who types a name and an email and presses Send invites
   their partner as a limited member. The checklist then keeps saying "Set
   up", because only owner-role invites count. The spec does not say which
   role the modal should start on. Milestone 2 reshapes this modal, so its
   plan is the natural place to settle it (`MembersPanel.tsx` owns the
   modal; `router.tsx:355` validates `?invite=true`).
2. **With a long email on a phone, the expiry is clipped away.** The expiry
   follows the email on one truncated line, so at 360 px the row shows
   "Limited · maximilian.alexander.wolfgan…" and no date
   (`PendingInvitesList.tsx`, the `truncate` line). Criterion 14 passes as
   written, since nothing overflows. Whether the date should get its own line
   is a design question.
3. **Overview asks a member without Money for `GET /accounts` and gets
   `403`.** `useAccounts(false)` is ungated (`OverviewPage.tsx:55`, from
   `9719648`, 2026-08-01). It is older than this milestone and harmless (the
   page shows "You don't have access to Money in this household."), but it
   puts a red `403` in a limited member's console.
4. **An open Settings tab does not learn about an acceptance in another
   browser.** Sam's Settings tab kept showing Alex as pending, and Sam as
   the only member, until Sam moved to another page. That is expected in
   milestone 1, which has no polling. Milestone 2's planned 3-second poll
   runs only while a Telegram invite is waiting for a knock, so this case,
   an emailed invite accepted in another browser, is not addressed by the
   spec yet.

Also seen, older than this milestone and outside it: after Escape closes a
modal that `?invite=true` opened, the URL keeps `?invite=true` (so a reload
reopens the modal) and focus falls to `<body>`. `MembersPanel.tsx:171`
documents the seeding as deliberate.
