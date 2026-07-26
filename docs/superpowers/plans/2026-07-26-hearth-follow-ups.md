# Hearth — known follow-ups

Recorded during the skeleton and identity plans. Each was reviewed, judged
non-blocking, and deliberately deferred or parked with a ruling. This file
exists so those judgements survive the scratch directory they were written in.

## Parked with a ruling from the product owner

These are decisions, not defects. The code and comments match them.

- **The lockout is household-wide, and that leaks which addresses share a household.** Someone who knows one member's address can confirm a second belongs to the same household by watching the attempts countdown. Accepted: the design's own copy says the household locks, and the screen shows the locked state to whoever is typing. Revisit only if the lock's scope changes.
- **The lock is uncapped, so repeated guessing can hold a household locked indefinitely.** Accepted: an attacker who keeps guessing should not get a countdown that runs out while they work. Magic link is deliberately ungated and is the escape hatch.

## Deferred, by area

### Skeleton plan

- **Task 1** — router.go MethodNotAllowed branch has no test
- **Task 1** — cmd/api EADDRINUSE test hangs rather than fails if wildcard-collision semantics ever differ
- **Task 1** — graceful-shutdown exit-0 path covered only by a manual smoke run
- **Task 2** — go.mod has an orphaned require block (cespare/xxhash, moby) left from the Go 1.24 hand-edit; cosmetic
- **Task 2** — if a future change drops stop() in main.go's goroutine, the bind-failure test hangs to the 10-minute default rather than failing fast; no committed -timeout anywhere yet
- **Task 3** — the four self-check cases assert exit status only, never message text
- **Task 4** — hearth-node-modules volume survives make down, so npm install can leave stale packages once web/package.json exists
- **Task 4** — a go.mod dependency added while the stack runs is not picked up by air alone; the module cache lives in the image, not the bind mount
- **Task 4** — web dev service bypasses any Dockerfile, so web/Dockerfile is only exercised by make build
- **Task 4** — no pin-drift monitoring for air/goose/mailpit
- **Task 4** — a failing migration blocking api is inferred from Compose semantics, never empirically tested
- **Task 5** — 5 high-severity npm audit findings in eslint's dev-only chain, needs a breaking eslint 10 bump
- **Task 5** — bare npm install in web/ needs --legacy-peer-deps for @hookform/resolvers; npm ci is clean

### Identity plan

- **Task 6** — errors.go carries a "Added in the Task 6 fix round" comment that will read oddly as history
- **Task 6** — Capabilities.Strings() loop variable named cap shadows the builtin
- **Task 7** — TestTheLockExpiresAfterFifteenMinutes is mechanically identical to the older-than-window test; the real branch is reachable only via the new exact-boundary test
- **Task 7** — the fix commit is typed refactor: though it also adds a test and a comment
- **Task 9** — login_attempts permits a user_id with a null household_id
- **Task 9** — no expires_at > created_at constraint on sessions, magic_links, invites
- **Task 9** — no cross-table household_id agreement enforced
- **Task 9** — NextSpacePosition is read-then-write with no uniqueness on (household_id, position), and ListSpaces has no tiebreaker
- **Task 11** — SessionRepository.Extend and RevokeAllForUser untested
- **Task 11** — :exec methods treat an unknown id as a silent no-op rather than ErrNotFound
- **Task 12** — N concurrent guesses all read the pre-attempt count, giving one burst-sized overrun per lock cycle
- **Task 12** — if Hasher.Hash ever fails, the fallback decoy is not argon2-shaped so Verify rejects it early and the timing gap reopens — only in a state where hash generation is already broken
- **Task 13** — send goroutine has no handle, so nothing drains it at shutdown
- **Task 13** — the report's claim that a failed Create never burns rate-limit budget holds for the double but not necessarily against real Postgres, where a timeout can commit server-side while the client sees an error
- **Task 14** — error message strings duplicate the length constants
- **Task 15** — translate's new 23505 branch drops the op name and constraint name that the default branch preserves
- **Task 15** — CreateSpace stores Name untrimmed while Key is trimmed; spaceKey only replaces ASCII spaces
- **Task 16** — Recoverer writes a bare 500, bypassing the error envelope
- **Task 16** — all three route walks blanket-skip /api/v1/invites/*, so a future mutating route there is auto-exempt
- **Task 16** — no Content-Type validation on public POSTs; RealIP trusts X-Forwarded-For
- **Task 16** — fxRateMode has no validation, so an invalid value reaches the DB CHECK and returns 500 — same class as the ErrInvalidMoney finding
- **Task 17** — if Christine's membership were removed after acceptance, a reseed would mint an invite that could never be accepted — the credentialed mirror of Finding 3, pre-existing
- **Task 17** — the orphan query has no supporting index; maxInviteLadderRungs and LIMIT 1 without ORDER BY are pragmatic bounds
- **Task 18** — modeRef syncs in useEffect rather than useLayoutEffect, leaving a narrow stale-ref window unobservable in jsdom
- **Task 18** — "Forgot?" has no pending guard, unlike the main magic-link button
- **Task 19** — /money/$ and /marriage/$ splat routes have no automated coverage
- **Task 19** — the accept endpoint still has no server-side guard against overwriting a live session; mitigated by a UI warning, not eliminated
- **Task 20** — CurrencyPanel and NotificationsPanel are correct but unprotected — no gap-probing test in either
- **Task 20** — apiFetch has no timeout or AbortController anywhere, so a refetch that never settles leaves a row disabled indefinitely — pre-existing, app-wide
- **Task 21** — backend currency validation is format-only, so ZZZ is accepted — pre-existing domain.NewMoney behaviour

## Not yet owned by any task

Surfaced by the final whole-branch review as requirements each task could
reasonably have assumed another covered. The urgent ones were fixed; these
remain open.

- `requireCapability` middleware exists but no route uses it, so the spec's promise that the server enforces capabilities independently is currently vacuous. It must not stay that way when the first capability-gated route lands.
- `preAuthPathPrefixes` and `publicRoutePrefixes` are two hand-maintained lists with nothing tying either to the actual route tree. A future public route that calls `useMe()` reintroduces a bug already fixed once.
- `apiFetch` has no timeout or abort, so a request that never settles leaves its control disabled indefinitely.
- Non-ASCII display names produce a replacement-character avatar initial, permanently — there is no profile-edit endpoint.

## Reversed decisions worth knowing about

- Task 15 recorded that `domain.ErrAlreadyExists` must never be blanket-mapped to 409, because a session token-hash collision would surface as a conflict rather than being retried. The final fix wave mapped it to 409 anyway, as a backstop against a 500 on invite acceptance. The reversal was reviewed and accepted; the residual is a 32-byte token collision.

