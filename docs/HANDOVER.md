# Hearth — handover

Written 2026-07-27, after slices 0 and 1 shipped, and updated the same day once
self-serve sign-up's code (not yet its browser walk — §1) was done too. This
is the document to read before picking the work back up, whether that is you
in three months or someone new.

---

## 1. Where things stand

Two of six slices are complete, reviewed and verified end to end in a browser.
A third piece of work, self-serve sign-up, is also complete and reviewed —
every task's code was reviewed clean, including fix rounds — but has **not**
had its own browser walk yet. Say that plainly rather than letting "verified
end to end" quietly absorb it: `make lint && make test` passing is not the
same claim as a human clicking through it.

| Slice | Contents | State |
|---|---|---|
| 0 — Skeleton | Clean-architecture layout, Docker, Compose, Make, migrations, health endpoints | **Done** |
| 1 — Household & identity | Sign-in, magic link, invite acceptance, lockout, members, roles, capabilities, spaces, Settings | **Done** |
| — Self-serve sign-up | Sign-up, household provisioning, an ISO 4217 currency allowlist and list endpoint, `adminctl prune`, a per-IP rate limiter | Code-complete; browser walk **pending** |
| 2 — Money | Accounts, Transactions, Budget, Goals, Bills | Not started |
| 3 — Marriage | Retros, Vision, Agreements | Not started |
| 4 — Family | Calendar | Not started |
| 5 — Overview | Read-only aggregation across 2–4 | Not started |

Self-serve sign-up carries no slice number on purpose: it was specified and
built between slices 1 and 2, ahead of Money (see "What to do next" below for
why), and the rest of the roadmap keeps its original numbers so every existing
reference to "slice 2 (Money)" elsewhere still points at the right thing.

The definition of done for slices 0 and 1 was walked in a real browser on a
wiped database: **10 of 10 criteria pass**. The record, including the three that
failed on the first walk and what was done about them, is in
`docs/superpowers/plans/2026-07-26-hearth-identity-verification.md`.

Self-serve sign-up's own definition of done is a 15-criterion walk, written
down in `docs/superpowers/plans/2026-07-27-hearth-self-serve-signup.md`
(Task 32) — a stranger creating a household, the endpoint's silence holding
under six rapid sign-ups, the per-IP limit eventually engaging, `adminctl
unlock-household --email` resolving the right household, and `adminctl prune`
refusing a window under seven days among them. It has not been run. Start it
from `make down && make up` (or an explicit `make migrate`), not a bare
`make up` — see §5's Makefile item.

Two screens the design marks "· not built" are deliberately absent: the **kids
view** and **custom space pages**. That is the design's own scoping, not an
omission.

---

## 2. Running it

```bash
make dev      # everything, logs tailed — http://localhost:5173
make seed     # the design's household; prints Christine's invite URL
```

`make seed` gives you Andreas signed-in-able with a known development password,
a pending invite for Christine, Kayla (12) and Ethan (8) as credential-less
members, the three builtin spaces, and notification preferences.

Bare `make` lists every target. The ones you will actually use:

| | |
|---|---|
| `make dev` / `make down` | start and stop |
| `make dev-local` | API and web as native processes for a debugger; infra stays in Docker |
| `make seed` | the household above; refuses to run outside `APP_ENV=development` **and** against a non-local database |
| `make reset-password EMAIL=…` | prompts on stdin, never a flag; also revokes that user's sessions |
| `make unlock-household` | clears a lockout without waiting 15 minutes |
| `make migrate-new NAME=…` | runs through the pinned dev image, not a host binary |
| `make lint` | arch lint, frontend typecheck, eslint, `go vet` |
| `make test` | Go suite (needs Docker) plus 117 frontend tests |

**Docker is colima on the original machine.** The Go suite uses testcontainers
and needs:

```bash
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
```

Mailpit catches all outbound mail at `http://localhost:8025`. Magic links and
invites land there in development.

---

## 3. The shape of the code

```
api/
  cmd/api/            wiring only — read config, open the pool, build the router, serve
  cmd/adminctl/       seed, reset-password, unlock-household, create-invite, prune
  internal/domain/    rules. Imports the standard library and nothing else.
  internal/usecase/   services + every port interface (ports.go)
  internal/adapter/   http, postgres, crypto, mail, clock, fx
  migrations/         goose
web/src/
  api/client.ts       apiFetch — the only way the app talks to the server
  components/         generic primitives only (Modal)
  features/           auth, shell, settings, placeholder
  routes/router.tsx   the route tree
```

**Dependencies point inward, and `make lint-arch` enforces it mechanically** —
including in test files. `internal/domain` may import stdlib only;
`internal/usecase` may add `internal/domain`. Everything else lives in an
adapter. The lint runs a real build first, because `go list` alone tolerates
breakage the compiler rejects.

`internal/usecase/ports.go` is the contract. Read it before writing a service or
a repository; it carries doc comments that are load-bearing, not decorative
(the `""` ⇄ SQL NULL convention, the transactional-accept warning).

---

## 4. What to do next

**The build order changed once already: self-serve sign-up shipped ahead of
slice 2.** The original four-slice order below (Money, then Marriage, then
Family, then Overview) was dependency-driven and none of that changed. Sign-up
is a separate piece of work that cut the line for a different, recorded
reason: its own spec's decision 1 ships it before the platform admin console
that will manage it (a deferred, separate spec) so it earns real usage first
— and a household has to be able to exist before there is anything for that
console to administer.

**Build slice 2 (Money) next.** It is still the largest, still the design's
centre of gravity, and everything above it in the dependency order — now
including self-serve sign-up — is done. Slice 5 (Overview) must still be last
— it only aggregates, so building it early means stubbing everything it reads.

Each slice gets its own spec → plan → implementation cycle, the same way these
did. The originating spec for slices 0–1 is
`docs/superpowers/specs/2026-07-26-hearth-foundation-design.md`; self-serve
sign-up's own is
`docs/superpowers/specs/2026-07-27-hearth-self-serve-signup-design.md`. The
completed plans beside them are worth skimming for house style before writing
a fourth.

### Before slice 2's first task

Three things the last review flagged as "must not be forgotten", all cheap now
and expensive later — **none of them closed by self-serve sign-up**:

1. **`requireCapability` middleware exists and no route uses it.** The spec
   promises the server enforces capabilities independently of the UI. That
   promise is currently vacuous — there is no capability-gated route.
   Self-serve sign-up added four new routes and all four are public; it did
   not add the first capability-gated one, so the deadline moved but did not
   close. Slice 2 adds the first one (`money`). Wire it, and extend the
   route-walk test matrices in `api/internal/adapter/http/api_test.go` to
   cover it.
2. **The derived figures are the real business logic and the spec does not
   define them.** The design shows `66% used`, `S$137/day left`, `on pace to
   save S$1,780`, `4 of 4 on track`, net worth from assets minus liabilities,
   and unspent budget rolling into a nominated goal at month end. Pin every
   formula in slice 2's spec. If you do not, each implementer will invent one.
3. **Money is `int64` minor units plus an ISO 4217 code, everywhere.**
   `domain.Money` already refuses to mix currencies and refuses to wrap on
   overflow. `usecase.Rate` is a fraction, not a scaled decimal, because IDR to
   SGD cannot be represented otherwise. Do not add a `float64` to a monetary
   path.

### The seams slice 2 will use

- **Bank sync is a port with no real implementation.** SGFinDex is restricted to
  licensed institutions, so `BankSyncProvider` exists with manual and CSV
  adapters behind it. The 3-step Link-account modal from the design is built
  against that port; the SGFinDex branch is shown unavailable.
- **The sidebar renders from `me.spaces`**, filtered and ordered by the server.
  Slice 2 adds pages under the existing Money space; it does not touch the
  sidebar.
- **`components/Modal`** is the shared primitive. Roughly fifteen modals across
  slices 2–4 build on it. It reaches genuine `:modal` state — do not
  reintroduce a declarative `open` attribute.
- **`stubFetchRoutes`** matches on method and URL and throws on an unregistered
  request. Use it for every frontend test; a stub that ignores the URL has
  silently passed broken code twice in this project.

---

## 5. Remaining items

`docs/superpowers/plans/2026-07-26-hearth-follow-ups.md` is the full list, with
the reasoning for each. The headlines:

### Decisions, not defects — do not "fix" these without a conversation

- **The lockout is household-wide, and that leaks which addresses share a
  household.** Someone who knows one member's address can confirm a second by
  watching the attempts countdown. Accepted deliberately: the design's copy says
  the household locks. Revisit only if the lock's scope changes.
- **The lock is uncapped**, so repeated guessing can hold a household locked
  indefinitely. Accepted: magic link is deliberately ungated and is the way back
  in.

Both are documented in the code at the point a future editor would change them.

### Worth doing when convenient

Three items previously listed here are now done, closed by self-serve sign-up:
`preAuthPathPrefixes`/`publicRoutePrefixes` became one `web/src/routes/publicRoutes.ts`
backed by a router-walk test; non-ASCII display names now get a correct avatar
initial (`initialOf` slices the first rune and case-folds through
`cases.Upper`, and `avatar_initial` widened to `text` for the rare expansion
case); and backend currency validation now checks membership in a real ISO
4217 allowlist (`domain.ParseCurrency`) rather than format only, so `ZZZ` is
refused.

- `requireCapability` unused (see above — this one has a deadline).
- `apiFetch` has no timeout or abort, so a request that never settles leaves its
  control disabled indefinitely.
- `CurrencyPanel` and `NotificationsPanel` are correct but unprotected: neither
  has a test that would catch a regression to the non-awaited invalidation.
- **`make up` can silently skip a migration.** `api` declares `depends_on:
  migrate` with `service_completed_successfully`, but Compose only
  re-evaluates that condition when it recreates `api` — so a stack left
  running across a newly added migration keeps its already-succeeded
  `migrate` container and never reruns it. `make dev-local` already runs
  `make migrate` explicitly before starting anything; `make up` and a bare
  `docker compose up` do not. Found while grounding this slice's own docs
  update, not by a test — `make down && make up` (which forces recreation) or
  an explicit `make migrate` sidesteps it for now.

### Before this is deployed anywhere real

- **TLS termination in front is mandatory.** Cookies are `Secure` outside
  development while nginx listens on plain `:80`. Without TLS the browser never
  returns the session cookie and everything 401s. The API logs a warning at
  startup; `.env.example` and `web/nginx.conf` say so.
- **SMTP now takes TLS policy and credentials from configuration**, defaulting
  to mandatory TLS outside development. Set `SMTP_USERNAME` / `SMTP_PASSWORD`
  for a hosted relay. If mail cannot leave the box, magic link — the only way
  back into a locked household — fails silently by design.
- The two production images have no wiring between them. `web/nginx.conf`
  hard-codes `proxy_pass http://api:8080`, so `hearth-web` cannot start alone.
- **The sign-up per-IP rate limiter's fix protects the production image only.**
  `web/nginx.conf` now sets `X-Real-IP` to `$remote_addr` and suppresses
  `True-Client-IP` on every API-proxying location, so a client can no longer
  spoof `chi`'s `middleware.RealIP` resolution through those headers there.
  But `docker-compose.yml` has no nginx service at all — in development, Vite
  proxies `/api` straight to `api:8080` with no header rewriting — so the
  per-IP limiter stays fully spoofable in development, and the pending browser
  walk (§1) cannot exercise the fix either way. The global daily mail ceiling
  (200/day, counted from `signups`) is what actually bounds the damage in the
  meantime.

---

## 6. Conventions worth keeping

These were each learned from a defect that shipped past a green test suite.

- **Fix the class, not the instance.** Four times in this project a defect was
  fixed at one site while its siblings kept the bug — a PATCH corrected in two
  of three endpoints, an error oracle closed at the mailer and left two lines
  away, a non-awaited invalidation fixed in one panel with two untouched. When
  you fix something, grep for its shape.
- **Verify UI behaviour in a real browser.** jsdom's `<dialog>` is a stub, so
  five passing tests hid a modal that threw on every open in production. If a
  behaviour depends on the platform, a simulated DOM cannot tell you it works.
- **A test that cannot fail is not protecting anything.** Mutate it: break the
  code deliberately, confirm the test goes red, restore. This caught a sidebar
  ordering test whose fixture agreed with the wrong answer, and a guard test
  that asserted a transient state.
- **Ask what a caller can measure, not just what it is told.** Two enumeration
  oracles here were built from timing and from which deadline moved, not from
  error codes.
- **Pin tool versions.** Floating `@latest` broke this build twice.
- **Every 2xx except 204 carries a JSON body.** `apiFetch` throws on an ok
  response it cannot parse.

---

## 7. If you are handing this to an agent

The completed plans are the format that worked:
`docs/superpowers/plans/2026-07-26-hearth-skeleton.md`, `…-identity.md`, and
`2026-07-27-hearth-self-serve-signup.md`. All three were executed task-by-task
with a fresh implementer per task, an independent review after each, and fix
rounds until clean. Every task found a real defect in the plan — budget for
that rather than treating it as failure.

The most valuable review instruction was not "check this task" but **"what
sibling of this defect exists elsewhere?"**
