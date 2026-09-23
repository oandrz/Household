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
