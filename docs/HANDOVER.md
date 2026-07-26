# Hearth — handover

Written 2026-07-27, after slices 0 and 1 shipped. This is the document to read
before picking the work back up, whether that is you in three months or someone
new.

---

## 1. Where things stand

Two of six slices are complete, reviewed and verified end to end in a browser.

| Slice | Contents | State |
|---|---|---|
| 0 — Skeleton | Clean-architecture layout, Docker, Compose, Make, migrations, health endpoints | **Done** |
| 1 — Household & identity | Sign-in, magic link, invite acceptance, lockout, members, roles, capabilities, spaces, Settings | **Done** |
| 2 — Money | Accounts, Transactions, Budget, Goals, Bills | Not started |
| 3 — Marriage | Retros, Vision, Agreements | Not started |
| 4 — Family | Calendar | Not started |
| 5 — Overview | Read-only aggregation across 2–4 | Not started |

The definition of done for slices 0 and 1 was walked in a real browser on a
wiped database: **10 of 10 criteria pass**. The record, including the three that
failed on the first walk and what was done about them, is in
`docs/superpowers/plans/2026-07-26-hearth-identity-verification.md`.

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
| `make test` | Go suite (needs Docker) plus 96 frontend tests |

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
  cmd/adminctl/       seed, reset-password, unlock-household, create-invite
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

**Build slice 2 (Money) next.** It is the largest, the design's centre of
gravity, and everything above it in the dependency order is done. Slice 5
(Overview) must be last — it only aggregates, so building it early means
stubbing everything it reads.

Each slice gets its own spec → plan → implementation cycle, the same way these
two did. The originating spec is
`docs/superpowers/specs/2026-07-26-hearth-foundation-design.md`; the two
completed plans beside it are worth skimming for house style before writing a
third.

### Before slice 2's first task

Three things the last review flagged as "must not be forgotten", all cheap now
and expensive later:

1. **`requireCapability` middleware exists and no route uses it.** The spec
   promises the server enforces capabilities independently of the UI. That
   promise is currently vacuous — there is no capability-gated route. Slice 2
   adds the first one (`money`). Wire it, and extend the route-walk test
   matrices in `api/internal/adapter/http/api_test.go` to cover it.
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

- `requireCapability` unused (see above — this one has a deadline).
- `preAuthPathPrefixes` and `publicRoutePrefixes` are two hand-maintained lists
  with nothing tying either to the route tree. A future public route that calls
  `useMe()` reintroduces a bug already fixed once.
- `apiFetch` has no timeout or abort, so a request that never settles leaves its
  control disabled indefinitely.
- Non-ASCII display names produce a replacement-character avatar initial,
  permanently — there is no profile-edit endpoint.
- `CurrencyPanel` and `NotificationsPanel` are correct but unprotected: neither
  has a test that would catch a regression to the non-awaited invalidation.
- Backend currency validation is format-only, so `ZZZ` is accepted.

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

The two completed plans are the format that worked:
`docs/superpowers/plans/2026-07-26-hearth-skeleton.md` and
`…-identity.md`. Both were executed task-by-task with a fresh implementer per
task, an independent review after each, and fix rounds until clean. Every task
found a real defect in the plan — budget for that rather than treating it as
failure.

The most valuable review instruction was not "check this task" but **"what
sibling of this defect exists elsewhere?"**
