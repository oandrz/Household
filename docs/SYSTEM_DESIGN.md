# Hearth — system design

How the system is put together, how a request moves through it, and what the
data looks like. Written so an engineer new to the project can orient in one
sitting.

**Scope:** what exists today — slices 0 and 1. Money, Marriage, Family and
Overview are not built; see `docs/FEATURE_TRACKER.md`.

---

## 1 · Containers

What runs, and what talks to what.

```mermaid
graph TD
    Browser["Browser"]

    subgraph dev["Development (docker compose)"]
        Web["web — Vite dev server :5173<br/>proxies /api and health paths"]
        API["api — Go service :8080<br/>air hot reload"]
        PG[("postgres :5432")]
        Mail["mailpit :8025 / :1025"]
        Migrate["migrate — one-shot goose<br/>api waits for it to succeed"]
    end

    Browser -->|"same origin, cookies"| Web
    Web -->|"proxy /api/v1"| API
    API --> PG
    API -->|SMTP| Mail
    Migrate --> PG
```

The browser only ever talks to one origin. In development Vite proxies `/api` to
the Go service; in production nginx serves the built bundle and proxies the same
path. There is no CORS configuration anywhere, because there is never a
cross-origin request.

`api` declares `depends_on: migrate` with `service_completed_successfully`, so
the schema is always applied before the service starts — including when someone
runs `docker compose up` directly rather than `make up`.

**Production differs in two ways that matter:** the `web` container is nginx
serving static files, and TLS termination in front is mandatory — cookies are
`Secure` outside development, so without TLS the browser never returns the
session cookie.

---

## 2 · Backend layers

Clean architecture. Dependencies point inward only, and `make lint-arch`
enforces it mechanically — including in test files.

```mermaid
graph TD
    subgraph cmd["cmd/"]
        Main["cmd/api — wiring"]
        Admin["cmd/adminctl — seed, reset-password,<br/>unlock-household, create-invite"]
    end

    subgraph adapters["internal/adapter/ — implements the ports"]
        HTTP["http — chi router, middleware,<br/>handlers, error table"]
        PGA["postgres — repositories over sqlc"]
        Crypto["crypto — argon2id, tokens"]
        MailA["mail — SMTP"]
        Clock["clock"]
        FX["fx — static rates"]
    end

    subgraph usecase["internal/usecase/ — services + ports.go"]
        Auth["AuthService"]
        Invite["InviteService"]
        Member["MemberService"]
        House["HouseholdService"]
        Seed["Seed"]
    end

    subgraph domain["internal/domain/ — rules, stdlib only"]
        Rules["Money · Role · Capability<br/>Membership · Space · LockoutPolicy<br/>typed errors"]
    end

    Main --> HTTP
    Main --> PGA
    Main --> Crypto
    Main --> MailA
    Admin --> usecase
    HTTP --> usecase
    usecase --> domain
    PGA -.->|implements ports| usecase
    Crypto -.-> usecase
    MailA -.-> usecase
    Clock -.-> usecase
    FX -.-> usecase
```

Solid arrows are compile-time dependencies. Dotted arrows are adapters
satisfying an interface declared in `usecase/ports.go` — the dependency still
points inward, which is why every service is testable against in-memory doubles.

**Two rules that shape everything else:**

- No `pgx`, `chi` or other infrastructure type escapes the adapter layer. A
  missing row becomes `domain.ErrNotFound` at that boundary, never `pgx.ErrNoRows`
  further up.
- **No service takes an actor parameter.** Services enforce what is *valid*;
  middleware enforces who is *asking*. Authorisation exists in exactly one place.

---

## 3 · Ports and their adapters

`usecase/ports.go` is the contract between the layers.

| Port | Implemented by | Notes |
|---|---|---|
| `UserRepository` | `adapter/postgres` | Includes the transactional `CreateWithMembership` |
| `HouseholdRepository`, `MembershipRepository`, `SessionRepository`, `MagicLinkRepository`, `LoginAttemptRepository`, `InviteRepository`, `SpaceRepository`, `NotificationRepository` | `adapter/postgres` | Nine narrow repositories rather than one wide one |
| `PasswordHasher`, `TokenGenerator` | `adapter/crypto` | argon2id with cost from config; tokens are random, stored hashed |
| `Mailer` | `adapter/mail` | SMTP; TLS policy and credentials from config |
| `Clock` | `adapter/clock` | So lockout windows and expiry are deterministic in tests |
| `FXRateProvider` | `adapter/fx` | Static table today; a live provider drops in behind it |

`BankSyncProvider` is specified but has no consumer yet — it arrives with the
Money slice, with manual and CSV adapters. Automatic sync via SGFinDex is not
available to an app like this.

---

## 4 · Request pipeline

Every `/api/v1` request passes through the same chain. The order is the security
model.

```mermaid
graph TD
    Req["Request"] --> RID["RequestID · RealIP · Recoverer<br/>(recoverer writes the standard error envelope)"]
    RID --> Public{"Public route?"}

    Public -->|"sign-in, magic-link,<br/>magic-link/consume,<br/>invites/{token}"| Handler
    Public -->|no| Session["requireSession<br/>reads hearth_session cookie,<br/>re-reads membership from the DB,<br/>extends when under a day remains"]

    Session --> Safe{"GET or HEAD?"}
    Safe -->|yes| Handler
    Safe -->|no| CSRF["requireCSRF<br/>double-submit, constant-time compare"]

    CSRF --> Owner{"Household-wide<br/>mutation?"}
    Owner -->|yes| RequireOwner["requireOwner"]
    Owner -->|no| Handler
    RequireOwner --> Handler

    Handler["Handler — decode within a size limit,<br/>call the service"] --> Service["Service"]
    Service --> Domain["Domain rules"]
    Service --> Repo["Repository"]
    Repo --> DB[("Postgres")]
    Service --> MapErr["MapDomainError<br/>the only place a status is chosen"]
    MapErr --> Resp["{ error: { code, message, details } }"]
```

**The membership is re-read on every request**, never cached in the session row.
A capability change therefore takes effect on the caller's very next request;
session revocation is belt-and-braces rather than the enforcement mechanism.

### Route table

| Method | Path | Guards |
|---|---|---|
| POST | `/auth/sign-in` | none — this *is* the credential check |
| POST | `/auth/magic-link` | none — always 202 |
| POST | `/auth/magic-link/consume` | none — the token is the credential |
| GET | `/auth/me` | session |
| POST | `/auth/sign-out` | session · CSRF |
| GET | `/invites/{token}` | none — the token is the credential |
| POST | `/invites/{token}/accept` | none |
| GET | `/household`, `/household/members`, `/spaces`, `/notification-preferences` | session |
| PATCH | `/household`, `/notification-preferences` | session · CSRF · owner |
| POST | `/household/members/invite`, `/spaces` | session · CSRF · owner |
| PATCH · DELETE | `/household/members/{id}` | session · CSRF · owner |
| GET | `/healthz`, `/readyz` | none — outside `/api/v1` |

Two test matrices walk the live router and assert this: every non-public route
rejects an unauthenticated caller, and every owner-gated route rejects a limited
member. A route added without its guard fails a test rather than shipping.

---

## 5 · Key flows

### Sign in, with the lockout

```mermaid
sequenceDiagram
    participant B as Browser
    participant H as Handler
    participant A as AuthService
    participant R as Repositories

    B->>H: POST /auth/sign-in
    H->>A: SignIn(email, password)
    A->>R: ByEmail

    alt address unknown
        A->>R: record attempt (no household)
        A->>A: decoy verify — equalises timing
        A-->>H: SignInFailedError (per-address countdown)
    else known
        A->>R: failures in the window
        alt household locked
            A->>R: record attempt
            A->>A: decoy verify
            A-->>H: SignInFailedError (locked, until)
        else not locked
            A->>A: verify password
            alt wrong
                A->>R: record attempt
                A-->>H: SignInFailedError (attempts remaining)
            else correct
                A->>R: clear failures, create session
                A-->>H: session token
            end
        end
    end

    H-->>B: 200 + Set-Cookie, or 401/423 with the same shape
```

Three failures inside fifteen minutes lock the **household's password sign-in**
for fifteen minutes. Magic link is never gated by the lock — that is the
recovery path. The decoy verification exists so argon2's cost cannot distinguish
the branches.

### Magic link — deliberately silent

```mermaid
sequenceDiagram
    participant B as Browser
    participant A as AuthService
    participant R as Repositories
    participant M as Mailer

    B->>A: POST /auth/magic-link
    A->>R: count recent for this address
    A->>R: look the address up
    Note over A,R: both reads always run, in this order,<br/>so the branches cost the same
    alt known, under the limit
        A->>R: store token hash (15 min, single use)
        A-)M: send in a goroutine, own timeout
    end
    A-->>B: 202 {"status":"accepted"} — always
```

Nothing about the response reveals whether the address exists, including when
the rate limit is exhausted or the send fails. **The frontend is therefore the
only place a send failure can surface**, which is why the sent panel carries
retry copy.

### Invite acceptance — one transaction

```mermaid
sequenceDiagram
    participant B as Browser
    participant I as InviteService
    participant R as InviteRepo
    participant DB as Postgres

    B->>I: POST /invites/{token}/accept
    I->>R: preview — expired? already accepted?
    I->>I: build membership via domain rules
    I->>I: hash password
    I->>R: Accept(...)
    R->>DB: BEGIN
    R->>DB: mark invite accepted (guarded)
    R->>DB: create user
    R->>DB: create membership
    R->>DB: COMMIT
    I-->>B: 200 + session cookie
```

All three writes are one transaction. Split apart, a failure in the middle leaves
an orphaned user holding the unique email index and the invite becomes
permanently unacceptable. The invite is claimed *first*, so two concurrent
accepts serialise on that row and the loser gets a clean conflict.

### What the frontend loads

```mermaid
graph TD
    Boot["App start"] --> Me["GET /auth/me<br/>cached as ['me']"]
    Me --> Guard{"Authenticated?"}
    Guard -->|no| SignIn["/sign-in"]
    Guard -->|yes| Shell["AppShell"]
    Shell --> Sidebar["Sidebar renders me.spaces —<br/>already filtered and ordered by the server"]
    Shell --> Page["Route content"]
    Page --> Settings["Settings panels — their own queries"]
    Settings -->|"on mutation"| Invalidate["invalidate ['me'] and the panel's query,<br/>awaited so the guard spans the refetch"]
```

`/auth/me` returns the user, household, membership, capabilities and visible
spaces in one response, so the shell renders without a request waterfall. The
sidebar never filters client-side — duplicating that rule is how the two drift.

---

## 6 · Data model

```mermaid
erDiagram
    households ||--o{ memberships : has
    households ||--o{ invites : has
    households ||--o{ sessions : scopes
    households ||--o{ spaces : has
    households ||--o{ login_attempts : scopes
    households ||--|| notification_preferences : has
    users ||--o{ memberships : holds
    users ||--o{ sessions : owns
    users ||--o{ magic_links : owns
    users ||--o{ login_attempts : may_reference
    users ||--o{ invites : invited_by

    households {
        uuid id PK
        text name
        text family_name
        char primary_currency
        bool show_secondary_currency
        char secondary_currency
        text fx_rate_mode
    }
    users {
        uuid id PK
        citext email "nullable — children have none"
        text password_hash "nullable"
        text display_name
        text avatar_initial
    }
    memberships {
        uuid id PK
        uuid household_id FK
        uuid user_id FK
        text role "owner | limited"
        text_array capabilities "calendar chores money marriage"
    }
    invites {
        uuid id PK
        uuid household_id FK
        citext email
        text role
        bytea token_hash
        uuid invited_by FK
        timestamptz expires_at
        timestamptz accepted_at
    }
    sessions {
        uuid id PK
        bytea token_hash
        uuid user_id FK
        uuid household_id FK
        timestamptz expires_at
        timestamptz revoked_at
    }
    magic_links {
        uuid id PK
        uuid user_id FK
        bytea token_hash
        timestamptz expires_at
        timestamptz consumed_at
    }
    signups {
        uuid id PK
        citext email
        bytea token_hash
        timestamptz expires_at
        timestamptz consumed_at "nullable"
    }
    login_attempts {
        uuid id PK
        uuid household_id FK "nullable"
        uuid user_id FK "nullable"
        citext email
        bool succeeded
        timestamptz at
    }
    spaces {
        uuid id PK
        uuid household_id FK
        text key
        text name
        text visibility "everyone | parents_only | custom"
        int position
        text required_capability
    }
```

Notes that are not obvious from the shapes:

- **Only hashes are stored** — passwords, session tokens, magic-link tokens,
  invite tokens and sign-up tokens. A raw token exists in memory and in an
  email, never in a column.
- **Every household-scoped table carries `household_id`**, so scoping is
  structural. Repository methods take it as their first argument after the
  context. `magic_links` and `signups` are the exceptions: a magic link
  identifies a user, not a membership, so it scopes through `user_id`
  instead; a sign-up identifies a verified address with no user or
  household behind it yet, so it has neither.
- **`login_attempts` allows both foreign keys to be null**, so an attempt against
  an unknown address is recorded without revealing whether it exists.
- **`signups` has no `user_id`**, unlike `magic_links`. There is no user yet —
  only a verified address — which is also why the row carries no household
  name or display name: those are collected on the screen the mailed token
  leads to, after verification, so a stranger cannot submit a sign-up for
  someone else's address with a household of their own choosing. `email` is
  deliberately not unique; several live tokens for one address are fine,
  and the first one consumed wins.
- **Database constraints mirror the domain rules** rather than trusting the
  application: a limited member cannot hold `marriage`, an owner must hold all
  four capabilities, and capabilities must come from the known set.
- **Money is `int64` minor units plus an ISO 4217 code** everywhere. `float64`
  never appears in a monetary path.

---

## 7 · Frontend structure

```
web/src/
  api/client.ts        apiFetch — the only way the app talks to the server:
                       CSRF header, credentials, error envelope decoding,
                       401 handling
  components/          generic primitives only (Modal, on native <dialog>)
  features/
    auth/              sign-in, invite, magic-link screens and hooks
    shell/             AppShell, Sidebar, RequireAuth, RequireCapability
    settings/          members, spaces, currency, notifications
    placeholder/       named stand-ins for unbuilt areas
  routes/router.tsx    the route tree
```

**Route guards are presentation, not security.** The server enforces
independently; `RequireAuth` and `RequireCapability` exist so the UI does not
render something the user will be refused.

---

## 8 · Operational shape

| | |
|---|---|
| Migrations | goose, applied by a one-shot container before `api` starts |
| Generated SQL | sqlc, from `internal/adapter/postgres/queries/*.sql` — `make sqlc` |
| Sessions | opaque random token, hashed at rest, 30 days, extended on use, revocable |
| CSRF | double-submit cookie, compared in constant time, mutating methods only |
| Mail | Mailpit in development; TLS policy and credentials from config elsewhere |
| Seeding | `adminctl seed`, refused unless `APP_ENV=development` **and** the database host is local — both checked before the connection opens |
| Health | `/healthz` ignores the database; `/readyz` pings it |

---

## Keeping this document true

This describes the system as it is, not as it was planned. When a feature ships,
an interface changes, a table is added or a flow is reshaped, update the diagram
it affects in the same change — a diagram nobody trusts is worse than none.

Use the `maintaining-system-design` skill; it says what to check and how to keep
the diagrams honest.
