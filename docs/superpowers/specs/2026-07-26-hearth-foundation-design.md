# Hearth — Foundation (Skeleton + Household & Identity)

**Date:** 2026-07-26
**Status:** Approved
**Scope:** Slice 0 (skeleton) + Slice 1 (household & identity)
**Source design:** `design/Household Dashboard.dc.html` (turns 5a and 6a)

## Context

The imported design document describes a household dashboard for two adults and
two children: money, marriage and family, grouped into sidebar "spaces". It
covers 13 screens and roughly 15 modals across five bounded contexts. That is
too large for one specification, so the product is split into slices that are
specified, planned and implemented one at a time.

### Slices

| # | Slice | Contents |
|---|-------|----------|
| 0 | Skeleton | Clean-architecture layout, Docker, Compose, Make, migrations, health endpoints |
| 1 | Household & identity | Sign-in, magic link, invite acceptance, lockout, members, roles, capabilities, spaces, Settings |
| 2 | Money | Accounts, Transactions, Budget, Goals, Bills |
| 3 | Marriage | Retros, Vision, Agreements |
| 4 | Family | Calendar |
| 5 | Overview | Read-only aggregation across slices 2–4 |

This document specifies slices 0 and 1 together. Slice 1 comes before every
feature slice because all data is household-scoped and capability-gated;
repository signatures cannot be written correctly without it. Overview comes
last because it only aggregates: building it first would mean stubbing
everything it reads.

### Out of scope

- **Kids view** and **custom space pages** — the design's own flow map marks both
  "· not built".
- **Billing, plans and quotas.** "Free plan" in the design renders as a static
  label.
- **Household signup and provisioning.** One household exists, created by the
  seed. Multi-tenancy is preserved in the schema, not in the UI.
- **Real bank synchronisation.** See "Bank sync" below.
- Every screen in slices 2–5. Their sidebar entries route to placeholder pages
  that name the slice which will ship them.

## Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Backend | Go | Stated requirement |
| Architecture | Clean architecture, dependencies pointing inward | Stated requirement |
| Frontend | React + TypeScript SPA against a JSON API | Chosen for UI flexibility and design fidelity over server-rendered HTML |
| Tenancy | Single household in the product, `household_id` in the schema | Personal use now; the product may be sold later, and retrofitting scoping would touch every table and query |
| Auth | HttpOnly session cookie, server-side sessions | Instant revocation, no token-refresh logic, no XSS-readable credentials. Scale is four users |
| Bank sync | `BankSyncProvider` port with manual and CSV adapters | SGFinDex access is restricted to participating financial institutions; real auto-sync is not buildable. The port keeps a future aggregator to a one-package change |
| Router | `chi` | Handlers stay `http.Handler`; framework types never leak into the interface layer |
| DB access | `sqlc` + `pgx` | Typed Go generated from real SQL; keeps queries visible and the repository boundary honest |
| Migrations | `goose` | Plain `.sql` up/down, single binary |
| Money | `int64` minor units + ISO currency code | SGD and IDR coexist; floating point is not acceptable for either |

## Architecture

Two deployables, one repository.

```
/
├── api/                        Go service
│   ├── cmd/
│   │   ├── api/main.go         wiring only
│   │   └── adminctl/           operational CLI (password reset, unlock)
│   ├── internal/
│   │   ├── domain/             entities, value objects, domain errors — no internal imports
│   │   ├── usecase/            application services + port interfaces
│   │   ├── adapter/http/       chi handlers, DTOs, middleware
│   │   ├── adapter/postgres/   sqlc queries + repository implementations
│   │   └── adapter/mail/       magic-link and invite sender
│   ├── migrations/             goose .sql files
│   └── sqlc.yaml
├── web/                        Vite + React + TypeScript
│   └── src/{routes,features,components,api,lib}
├── design/                     imported design document (reference only, never built)
├── docker-compose.yml
├── Makefile
└── docs/
```

The dependency rule is enforced mechanically, not by convention. `make lint-arch`
fails if `domain` imports any other internal package, or if `usecase` imports
`adapter`. It runs in CI.

Ports are declared in `usecase` and implemented in `adapter`. Slice 1 ports:

`UserRepository`, `HouseholdRepository`, `MembershipRepository`,
`InviteRepository`, `SessionRepository`, `MagicLinkRepository`,
`LoginAttemptRepository`, `Mailer`, `Clock`, `PasswordHasher`,
`TokenGenerator`, `FXRateProvider`.

`Clock` and `TokenGenerator` are ports because lockout windows, invite expiry and
token values must be deterministic under test.

The frontend talks only to `/api/v1`. In development Vite proxies that path to
the API container, so requests stay same-origin and the session cookie applies
without configuration. In production a single nginx serves the static bundle and
proxies `/api`. There is no CORS configuration anywhere.

The frontend is organised by feature — `features/auth`, `features/household`,
`features/settings` — each owning its components, hooks, zod schemas and query
keys. `components/` holds only genuinely generic primitives: `Button`, `Modal`,
`Field`.

## Domain model

```
User          id, email (nullable — children may have none),
              password_hash (nullable), display_name, avatar_initial, created_at

Household     id, name, family_name, primary_currency, show_secondary_currency,
              secondary_currency, fx_rate_mode, created_at

Membership    id, household_id, user_id, role, capabilities, joined_at

Invite        id, household_id, email, name, role, capabilities, token_hash,
              invited_by, expires_at, accepted_at

Session       id, token_hash, user_id, household_id, created_at, expires_at,
              revoked_at

MagicLink     id, user_id, token_hash, expires_at, consumed_at

LoginAttempt  id, household_id, user_id (nullable), succeeded, at

Space         id, household_id, key, name, visibility, position, is_builtin

NotificationPreference
              household_id, bill_reminders, overspend_alerts, retro_reminder,
              weekly_digest
```

`Membership.capabilities` is stored as a Postgres `text[]` with a check
constraint restricting values to the known set.

Every table except `User` carries `household_id`. Repository methods take a
household scope as their first argument; the HTTP middleware supplies it from the
session. No handler reads a household identifier from a request body.

### Roles and capabilities

Role is coarse: `owner` (a parent, full access) or `limited` (a child). The
`capabilities` set is the fine grain, drawn from
`{calendar, chores, money, marriage}`.

The design's Settings screen fixes the intent: both parents hold every
capability; Kayla (12) holds `calendar` and `chores`; Ethan (8) holds `calendar`
only.

Two rules are enforced in the domain constructor, not in handlers:

1. A `limited` member can never hold the `marriage` capability.
2. A household must retain at least one `owner`; removing or demoting the last
   one is rejected.

Children exist as members without credentials. Because the kids view is out of
scope, a `limited` member has no email address, no password and no invite, and
cannot sign in this slice. They appear in Settings and will later be selectable
as calendar participants. Attempting to invite a `limited` member with no email
succeeds and simply creates no `Invite` row.

### Spaces

Each household is seeded with three built-in spaces: Money (`parents_only`),
Marriage (`parents_only`), Family (`everyone`). `visibility` is one of
`everyone`, `parents_only`, `custom`. The sidebar renders from this table,
filtered by the caller's capabilities, which is what allows "+ New space" to
extend the navigation without a code change.

Custom space *pages* are out of scope; creating a space this slice produces the
sidebar entry and a landing placeholder only. The `template` field on space
creation (Kids, Home, Travel, Blank in the design) selects a suggested name and
visibility; it creates no pages.

### Lockout

Three failed sign-in attempts within a 15-minute window lock the **household**
for 15 minutes. Locking the household rather than the account follows the
design's copy: *"Two tries left before we lock the household for 15 minutes."*

`LoginAttempt` rows drive the check, which lives in a domain service taking a
`Clock`. It is unit-tested without sleeping. A successful sign-in clears the
counter for that household.

The sign-in response reports `attemptsRemaining` on failure and `lockedUntil`
when locked, because the design's error state displays both.

### Money and foreign exchange

`Money{amount int64, currency string}` is a value object in `domain`, always in
minor units. Arithmetic between differing currencies is a compile-time-visible
error path, never a silent conversion.

`FXRateProvider` is a port. Slice 1 ships `StaticRateProvider` seeded with
`S$1 = Rp 12,410`. The design labels the rate "auto", so a live provider can be
substituted without touching callers. Conversion is display-time only: balances
are stored in their native currency and the secondary figure is computed for
presentation.

## API surface

All routes are under `/api/v1`. JSON request and response bodies. The session
cookie carries identity; middleware resolves the household scope and injects it
into the request context.

```
POST   /auth/sign-in              {email, password}
                                  → 200 + Set-Cookie
                                  | 401 {code, attemptsRemaining}
                                  | 423 {code, lockedUntil}
POST   /auth/magic-link           {email} → 202 always (never reveals whether the address exists)
POST   /auth/magic-link/consume   {token} → 200 + Set-Cookie | 410 if expired or consumed
POST   /auth/sign-out             → 204, revokes the session row
GET    /auth/me                   → {user, household, membership, capabilities, spaces}

GET    /invites/:token            → {householdName, inviterName, role, capabilities}   (pre-auth)
POST   /invites/:token/accept     {password, displayName} → 200 + Set-Cookie

GET    /household                 → household settings
PATCH  /household                 {familyName, primaryCurrency, showSecondaryCurrency, fxRateMode}
GET    /household/members         → [{user, role, capabilities}]
POST   /household/members/invite  {name, email?, role, capabilities} → 201
PATCH  /household/members/:id     {role, capabilities}
DELETE /household/members/:id     → 204, also revokes that member's sessions

GET    /spaces                    → spaces visible to the caller, ordered
POST   /spaces                    {name, visibility, template} → 201

GET    /notification-preferences  → the four toggles from the Settings screen
PATCH  /notification-preferences

GET    /healthz                   → liveness, no database access
GET    /readyz                    → readiness, pings the database
```

`GET /auth/me` is deliberately broad: it returns everything the application shell
needs in a single request, so the sidebar renders without a request waterfall.
TanStack Query caches it under the key `['me']`, and every membership or space
mutation invalidates it.

### Magic link

A magic link is requested by email address and delivered by the `Mailer` port. In
development Mailpit captures it. The token is single-use, expires after 15
minutes, and only its hash is stored. Consuming it creates a session exactly as a
password sign-in does. The endpoint always answers `202` regardless of whether
the address exists, so it cannot be used to enumerate members.

### Errors

One envelope for every error response:

```json
{"error": {"code": "INVALID_CREDENTIALS", "message": "...", "details": {}}}
```

`code` is a stable machine-readable string the frontend switches on. `message` is
human-readable and safe to display. Domain errors map to codes in a single table
in `adapter/http/errors.go`; handlers never construct error responses by hand.

Unexpected errors are logged with a request identifier and returned as a generic
`500 INTERNAL`. The request identifier is included in the response so a user can
quote it.

### CSRF

Double-submit cookie. A non-HttpOnly `csrf_token` cookie is set at sign-in; the
API client echoes it in an `X-CSRF-Token` header on every mutating request, and
the server compares the two. A mismatch returns `403 CSRF_INVALID`.

Session cookies are `HttpOnly`, `SameSite=Lax`, and `Secure` outside development.

### Credentials and session lifetime

Passwords are hashed with argon2id behind the `PasswordHasher` port; parameters
live in configuration so they can be raised without a code change. Only hashes
are stored, for passwords, session tokens, magic links and invite tokens alike.

Sessions last 30 days, extended on use. Sign-out, member removal and capability
changes all revoke the affected sessions immediately, which is the property that
motivated server-side sessions in the first place.

## Frontend

### Screens built this slice

Sign in, invite acceptance, wrong-password state, magic-link-sent state,
application shell (sidebar and header), and Settings (members, spaces, currency,
notifications). The Settings screen's "Connected accounts" panel belongs to
slice 2 and is omitted here; the plan label is static text.

The three authentication states are one route driven by a state machine, not
three routes — matching the design's `authScreen` control. Every other sidebar
destination routes to a placeholder that names the slice which will ship it, so
the shell is real from the first day.

### Guards

`<RequireAuth>` reads the `['me']` query; on a `401` the API client clears the
cache and redirects to sign-in. Capability gating uses
`<RequireCapability cap="marriage">` plus sidebar filtering.

Client-side gating is presentation only. The server enforces every rule
independently, and the HTTP test suite asserts that each protected route rejects
callers lacking the capability.

### Design tokens

Tokens are extracted from the design document into `tailwind.config.ts` before
any screen is built: the palette (`#f0eee9` canvas, `#fafaf9` surface, `#1a6b52`
accent), the four families (Schibsted Grotesk, Newsreader, Karla, IBM Plex
Mono/Sans), radii and shadows. Later slices compose from tokens rather than
re-reading the design document.

The `<Modal>` primitive — backdrop dismissal, close control, focus trap, Escape
handling — is built here because slices 2–4 need roughly fifteen modals.

### Stack

Vite, React 19, TypeScript in strict mode, TanStack Router (typed routes,
deep-linkable), TanStack Query (server state and cache invalidation), Tailwind,
react-hook-form with zod. Zod schemas validate API responses as well as forms.

## Infrastructure

### Compose services

| Service | Purpose |
|---|---|
| `postgres` | 17-alpine, named volume, `pg_isready` healthcheck |
| `migrate` | one-shot goose run, exits after applying migrations |
| `api` | Go service, `air` for hot reload in development |
| `web` | Vite development server, proxies `/api` to `api:8080` |
| `mailpit` | captures magic-link and invite email; UI on port 8025 |

Migrations run as a one-shot service rather than in the API entrypoint, so a
failed migration is visible and does not produce a crash loop.

Both Dockerfiles are multi-stage. The API builds to a distroless static image
running as a non-root user. The web image builds the bundle with Node and serves
it from nginx, which also proxies `/api`. Compose targets the `dev` stage of
each; `make build` produces the final stages.

### Ports

| Port | Service |
|---|---|
| 5173 | web — the URL used during development |
| 8080 | api — proxied, direct access only for `curl` |
| 5432 | postgres |
| 8025 | Mailpit interface |

### Makefile

Self-documenting: bare `make` prints the target list, with `dev` first.

```
dev                              Postgres, Mailpit, API and web together — http://localhost:5173
dev-local                        API and web as native processes, infra in Docker
up down restart logs ps          Compose lifecycle (up is the detached variant of dev)
migrate migrate-down migrate-new goose
sqlc                             regenerate typed queries
seed                             the design's household: two parents, two children, three spaces
reset-password EMAIL=…           adminctl
unlock-household                 adminctl
test test-api test-web           test-api uses testcontainers
lint lint-arch                   golangci-lint plus the dependency-rule check
fmt                              gofmt and prettier
psql shell-api                   exec into running containers
build                            production images
```

`make dev` runs both services in the foreground with interleaved, prefixed logs;
Ctrl-C stops both. `make dev-local` runs the API and web natively for debugger
attachment, keeping only Postgres and Mailpit in Docker:

```make
dev-local:
	docker compose up -d postgres mailpit
	$(MAKE) migrate
	@trap 'kill 0' EXIT INT TERM; \
	 (cd api && air -c .air.toml 2>&1 | sed 's/^/[api] /') & \
	 (cd web && npm run dev 2>&1 | sed 's/^/[web] /') & \
	 wait
```

The `trap 'kill 0'` terminates the whole process group, so an interrupt never
leaves an orphaned Vite process holding port 5173.

`make seed` creates the exact household from the design — Andreas and Christine
as owners, Kayla (12) with calendar and chores, Ethan (8) with calendar, and the
three built-in spaces — so every later slice develops against a realistic
fixture.

Configuration is by environment variable, with a committed `.env.example` and a
git-ignored `.env` for Compose.

## Testing

Test-driven: tests are written before the code they cover.

- **Domain** — pure unit tests. Lockout windows, capability rules, the
  last-owner rule, `Money` arithmetic, foreign-exchange rounding.
- **Use case** — mocked ports. Every authentication path: correct password,
  incorrect password, third incorrect attempt, locked household, expired invite,
  already-accepted invite, expired magic link, consumed magic link.
- **Repository** — testcontainers Postgres, real SQL, real migrations.
- **HTTP** — `httptest` against the full router with a real database. Covers
  CSRF rejection, cookie attributes, and that every protected route returns
  `401` when unauthenticated and `403` when the caller lacks the capability.
- **Frontend** — Vitest and Testing Library for the authentication state machine
  and capability gating. No end-to-end tests this slice.

## Definition of done

On a clean checkout, `make dev` produces a working application at
`http://localhost:5173` where a user can:

1. Open an invite link and accept it by setting a password.
2. Sign in with email and password.
3. Be locked out for 15 minutes after three incorrect passwords, seeing the
   remaining attempts before that.
4. Request a magic link, retrieve it from Mailpit, and sign in with it.
5. See the real sidebar, filtered by their capabilities.
6. Edit household currency settings and notification preferences.
7. Invite a member and change a child's capabilities.
8. Sign out, which revokes the session.

`make test` and `make lint-arch` both pass.

## Next

Slice 2 (Money) is specified separately. Its spec must pin the derived figures
the design displays but does not define: budget percentage used, daily pace
remaining, projected saving, goal progress and on-track determination, net worth
from assets and liabilities, and the month-end roll of unspent budget into a
nominated goal.
