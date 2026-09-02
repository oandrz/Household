# Hearth — platform admin surface

Written 2026-09-01.

The operator — one person, the vendor — needs a place inside the product to
turn features on and off, look at the data without an SSH tunnel, hand someone
a sign-in link, and see how many households exist. Today all four need a
terminal and a key to the box.

This spec covers **the admin foundation and its first tenants**. Database
*management* beyond reading — editing rows, running SQL — is deliberately not
here. It gets its own spec and its own ADR if it is ever wanted.

---

## 1. What this is not

Two mistakes are easy to make here, and both are cheaper to name than to undo.

**This is not a household owner's panel.** `domain.Role` (`owner`, `limited`)
and `domain.Capability` (`calendar`, `chores`, `money`, `marriage`) are scoped
to one household and answer "what may this member do inside their own home".
Platform admin is a different axis entirely: it answers "who runs this
install". The two never merge. No `IsAdmin` field appears on `Membership`, and
no admin check is ever expressed as a role or a capability.

**Feature flags are not a second capability system.** The boundary, which
belongs in a comment in `featureflag.go`:

> A **capability** answers *who may use this*. A **flag** answers *whether this
> install has it at all*. A route may carry both. Neither substitutes for the
> other.

`docs/FEATURE_TRACKER.md` is the justification: 23 of 111 rows are ⬜ or 🟡,
including four notification rows that are built end to end and deliver nothing.
Flags let that work ship dark and be turned on for one household first, instead
of waiting for a release where everything is finished at once.

---

## 2. Authorization

### 2.1 Who is an admin

New table, one row per admin:

```sql
CREATE TABLE platform_admins (
    user_id    uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    note       text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
```

A platform admin is an ordinary user who also has a row here. They still belong
to their own household and use the product normally — which matters, because
`requireSession` resolves a membership and answers 401 without one, so an admin
with no household could not sign in at all.

`internal/domain/admin.go` carries the type and nothing else:

```go
// PlatformAdmin is an operator of this install. It is deliberately empty of
// permissions: there are no admin levels, and adding one later means adding a
// field here rather than reinterpreting an existing one.
type PlatformAdmin struct {
    UserID    string
    Note      string
    CreatedAt time.Time
}
```

**Admins are created only by `adminctl`**, never over HTTP:

```
adminctl grant-platform-admin --email=<address> [--note=<why>]
adminctl revoke-platform-admin --email=<address>
adminctl list-platform-admins
adminctl unlock-admin --email=<address>          # clears admin_reauth_attempts (§2.2)
```

There is no "invite an admin" endpoint and no self-promotion path. An admin
surface that can mint its own admins turns one stolen session into permanent
access; a CLI on the box means the box is the boundary.

### 2.2 The re-auth grant

Session TTL is 30 days (`httpadapter.SessionTTL`). That is right for a
household member and far too long for a surface that can read every
household's finances. Entering `/admin` therefore requires the password again.

```
POST /api/v1/admin/session   { "password": "..." }  → 204
```

Verified with the existing `usecase.PasswordHasher`. On success the session row
gets `admin_grant_expires_at = now + 30 minutes`.

```sql
ALTER TABLE sessions ADD COLUMN admin_grant_expires_at timestamptz;
```

`usecase.SessionRecord` gains an `AdminGrantExpiresAt *time.Time`, and
`SessionRepository` gains one method:

```go
// GrantAdmin stamps a session's admin re-auth grant. A nil expiry clears it.
GrantAdmin(ctx context.Context, tokenHash []byte, expiresAt *time.Time) error
```

Session extension does not clobber the grant: `ExtendSession` is
`UPDATE sessions SET expires_at = $2 WHERE token_hash = $1` — one column, not a
whole-row rewrite. Worth a test anyway, because the day someone widens that
statement the grant would silently reset and nothing else would complain.

The grant lives on the session row, not in a second cookie. Sign-out already
revokes the session, so the grant dies with it for free, and there is no second
cookie whose `Secure`/`SameSite`/`HttpOnly` flags could be got wrong
independently of the first.

The grant is **not** extended by activity, unlike the session itself. Thirty
minutes is thirty minutes; a long admin session is re-authenticated rather than
renewed silently.

**Failed re-auth attempts get their own ledger, not `login_attempts`.** The
product's lockout is *household*-scoped: `domain.DefaultLockoutPolicy` locks
password sign-in for every member after three failures in fifteen minutes.
Feeding admin mistypes into that ledger would let the operator lock their whole
household out of the ordinary product as a side effect of fumbling a password
on a screen nobody else can see. So:

```sql
CREATE TABLE admin_reauth_attempts (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    succeeded boolean     NOT NULL,
    at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX admin_reauth_attempts_user_at_idx ON admin_reauth_attempts (user_id, at DESC);
```

The *policy* is reused rather than reinvented: the same
`domain.LockoutPolicy.Evaluate` runs over this ledger, keyed on `user_id`. A
locked admin surface therefore never touches household sign-in, and household
sign-in never locks the admin surface. `adminctl unlock-household` gains a
sibling, `adminctl unlock-admin --email=`, so the operator is not locked out of
their own box with no way back.

This is not a table the audit log can stand in for. The log is an inert record;
an enforcement path that read it would make deleting or truncating the log a
way to reset a lockout.

### 2.3 The middleware chain

Every admin route runs:

```
requireSession → requirePlatformAdmin → auditAdmin → [requireAdminGrant] → [requireCSRF]
```

- **`requirePlatformAdmin(deps)`** — looks up `platform_admins` by
  `scope.UserID`. Absent → **404 NOT_FOUND**, with the same body the router's
  own `NotFound` handler writes. Not 403: a 403 confirms that `/admin` exists
  and that the caller found the right path. To anyone who is not an admin, the
  admin surface is indistinguishable from a typo.
- **`auditAdmin(deps)`** — writes one `admin_audit_log` row per request,
  including reads. It wraps the whole subtree rather than being called by each
  handler, because a handler that forgets is the failure mode and middleware
  cannot forget.
- **`requireAdminGrant(deps)`** — 401 with code `ADMIN_REAUTH_REQUIRED` when
  `admin_grant_expires_at` is null or past. Applied to everything except
  `POST /admin/session` itself. The distinct code is what lets the UI show a
  password prompt instead of bouncing the operator to sign-in.
- **`requireCSRF`** — unchanged, on mutating methods only.

### 2.4 The audit log

```sql
CREATE TABLE admin_audit_log (
    id            uuid PRIMARY KEY,
    actor_user_id uuid        NOT NULL REFERENCES users(id),
    action        text        NOT NULL,  -- "GET /admin/db/tables/transactions"
    target        text        NOT NULL DEFAULT '',
    detail        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    ip            text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX admin_audit_log_created_at_idx ON admin_audit_log (created_at DESC);
```

Append-only. **No delete route exists anywhere in the product**, and `adminctl
prune` does not touch this table — the point of a log the operator cannot edit
is that it still means something after the operator makes a mistake.

`detail` never contains a secret: no passwords, no tokens, no message bodies,
no row values from the DB browse. It records *what was looked at*, not what was
seen.

The log is readable in the admin UI at `/admin/audit`, newest first, and
reading it is itself audited.

> **Amended 2026-09-02.** The `/admin/audit` screen was built to this
> section and §7, walked in a browser, and then descoped by the product
> owner as not needed; it was removed rather than merged. The log is read
> through `psql`. Everything else in this section stands, including that
> any future reader of the log is itself audited by construction.

---

## 3. Feature flags

### 3.1 The registry is code

`internal/domain/featureflag.go`, following `ParseRole` and
`ParseCapabilities` exactly:

```go
type Flag string

const (
    FlagSignupsOpen         Flag = "signups_open"
    FlagTelegramSignIn      Flag = "telegram_sign_in"
    FlagNotificationDelivery Flag = "notification_delivery"
    FlagFamilyCalendar      Flag = "family_calendar"
)

// FlagDefinition is one flag as the product knows it. Default is what a fresh
// install does with no override rows at all.
type FlagDefinition struct {
    Flag        Flag
    Description string
    Default     bool
}

func AllFlags() []FlagDefinition
func ParseFlag(s string) (Flag, error) // unknown → ErrUnknownFlag
```

The launch set is small and each entry is real:

| Flag | Default | What it gates |
|---|---|---|
| `signups_open` | `true` | `POST /auth/sign-up` and the public sign-up screen. Closing registration without a redeploy. |
| `telegram_sign_in` | `true` | The Telegram routes, today gated only by a nil check on config (ADR 4). |
| `notification_delivery` | `false` | The four 🟡 notification rows, once something sends them. Off until delivery exists. |
| `family_calendar` | `false` | An unbuilt page. It exists at launch to prove dark-shipping works before it is needed in anger. |

Adding a flag is one `const` and one line in `AllFlags()`. Nothing else.

### 3.2 Overrides are data

```sql
CREATE TABLE feature_flags (
    key        text PRIMARY KEY,
    enabled    boolean     NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by uuid        REFERENCES users(id)
);

CREATE TABLE household_feature_flags (
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    key          text        NOT NULL,
    enabled      boolean     NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    updated_by   uuid        REFERENCES users(id),
    PRIMARY KEY (household_id, key)
);
```

A row exists **only where someone overrode something**. There is no row per
flag per household, and no seeding: an install with no rows behaves exactly as
`AllFlags()` says.

`key` has no foreign key to a registry table, because the registry is
compile-time. The consequence is deliberate and must be handled rather than
prevented: a key can outlive the `const` that named it, after a flag is deleted
from the code. Resolution's `switch` skips keys `ParseFlag` refuses, so an
orphaned row can never turn anything on. The admin UI lists such rows under
"orphaned — safe to delete" so they are visible rather than mysterious.

### 3.3 Resolution is a pure function

```go
// ResolveFlags answers, for one household, what every known flag is set to.
// Precedence: a household override beats a global override, which beats the
// compile-time default. Keys ParseFlag refuses are ignored — an override row
// naming a flag this build does not have must never enable anything.
func ResolveFlags(defs []FlagDefinition, global, household map[Flag]bool) FlagSet

type FlagSet map[Flag]bool

// Enabled answers false for a flag this build does not define, so a typo in a
// caller fails closed rather than opening a route.
func (f FlagSet) Enabled(flag Flag) bool
```

No database, no context, no clock. The precedence bugs live here, and here they
are testable in microseconds.

### 3.4 Where flags are read and enforced

`requireSession` resolves the caller's `FlagSet` and stores it on `Scope`
alongside `Membership`. One extra query per authenticated request, both scopes
in a single statement:

```sql
SELECT key, enabled, NULL::uuid AS household_id FROM feature_flags
UNION ALL
SELECT key, enabled, household_id FROM household_feature_flags WHERE household_id = $1
```

**Not cached, on purpose.** One box, a handful of households, and a stale cache
after an admin toggles something is a worse defect than one small indexed
query. A cache is added when a measurement asks for one, with an invalidation
path designed at that point — not speculatively now.

Enforcement is a middleware that stacks beside `requireCapability`:

```go
func requireFeature(flag domain.Flag) func(http.Handler) http.Handler
```

A disabled flag answers **404 NOT_FOUND**, not 403. On this install the feature
does not exist, and 403 would confirm a route that is meant to be invisible.

Public routes with no session — sign-up, Telegram start — cannot read a
household's overrides, because there is no household yet. `requireFeature`
handles them itself: with no `Scope` on the request it resolves the **global**
set only. Household overrides are meaningless before authentication and must
never be silently treated as "on".

That fallback is why enforcement is one middleware rather than a middleware
plus a helper that public handlers remember to call. A hand-rolled check as a
handler's first statement is the shape that gets forgotten on the next public
route added, and forgetting it fails *open*.

Turning a flag off **never deletes data**. It hides routes and UI; the rows
stay exactly where they were and reappear when it is turned back on.

### 3.5 The client

`GET /auth/me` gains two fields:

```json
{
  "isPlatformAdmin": false,
  "features": { "signups_open": true, "telegram_sign_in": true,
                "notification_delivery": false, "family_calendar": false }
}
```

**Every known key is always present**, so the frontend never has to decide what
a missing key means. `useFeature("family_calendar")` reads from the same
`useMe` cache the app already has; the sidebar, the router guards and the
individual screens all consult it. The server guard is what enforces; the
client's job is only to avoid showing a door that opens onto a 404.

### 3.6 The admin screen

`/admin/flags` lists every `FlagDefinition` with its description, its
compile-time default, its global state, and how many households override it.
Toggling global state is `PUT /api/v1/admin/flags/{key}`; a household override
is `PUT /api/v1/admin/flags/{key}/households/{householdID}` and
`DELETE` removes the override rather than setting it false — "no opinion" and
"explicitly off" are different states and the UI shows all three.

---

## 4. Read-only database browse

### 4.1 Why read-only, in writing

`api/cmd/adminctl/main.go`'s own doc comment says adminctl exists for operator
actions "that have no business behind an authenticated HTTP endpoint". That
position is not overturned here, it is narrowed: **mutations stay in adminctl
over SSH; reads move to the web behind re-authentication and an audit log.**
ADR 0005 records this, with the reasoning and the cost.

The cost is real and stated plainly: an admin session that has been
re-authenticated within the last 30 minutes can read every household's
financial data. That is why §2.2 exists, why every page view is audited, and
why writes are not on the table.

### 4.2 The port

Everything the HTTP layer sees is already strings, so no database type crosses
out of the adapter and `make lint-arch` stays green:

```go
// DatabaseBrowser is a read-only, structural view of the database, for the
// operator's admin surface. Every value it returns is already rendered as
// text: no driver type, no `any`, and nothing a caller could accidentally
// write through.
type DatabaseBrowser interface {
    Tables(ctx context.Context) ([]TableInfo, error)
    Rows(ctx context.Context, table string, limit, offset int) (RowPage, error)
}

type TableInfo struct {
    Name     string
    RowCount int64
    Columns  []ColumnInfo
}

type ColumnInfo struct {
    Name     string
    DataType string
    Redacted bool // true when this column's values are never rendered
}

type RowPage struct {
    Columns []string
    Rows    [][]string
    Total   int64
    Limit   int
    Offset  int
}
```

### 4.3 Four guards, in order of how much they are trusted

1. **A separate Postgres role.** `hearth_readonly`, granted `SELECT` on
   `public` and nothing else, reached through its own pool built from
   `DATABASE_READONLY_URL`. This is the guard that actually holds: a mistake in
   the adapter's SQL still cannot write. The role is created during
   provisioning (`deploy/PROVISION.md`), not by a migration — migrations do not
   run as a superuser.

   **When `DATABASE_READONLY_URL` is unset the browse is unavailable** and the
   admin UI says so in as many words. There is no fallback to the read-write
   pool. A half-provisioned box degrades to "you cannot use this", never to
   "you are using it through the wrong connection".

2. **Table names are never interpolated from input.** The requested name is
   matched against `information_schema.tables` for schema `public` at call
   time; no match is `domain.ErrNotFound`. Only then is it quoted with
   `pgx.Identifier{name}.Sanitize()`.

3. **Bounded work.** `SET LOCAL statement_timeout = '3s'` on the browse
   connection, `limit` capped at 100 and defaulted to 50, `offset` non-negative.
   A misclick cannot pull a million rows or pin a CPU.

4. **Redaction, fail-closed.** A column is redacted when *any* of these holds,
   and redacted columns render `«redacted»`:
   - its type is `bytea` (every token and hash in this schema is `bytea`),
   - its name ends in `_hash` or `_secret`, or contains `password`,
   - it appears in an explicit denylist in `internal/domain`.

   The type rule is what makes this survive: a secret column added in a
   migration next year is redacted before anyone remembers this file exists.
   `ColumnInfo.Redacted` is returned to the UI so the operator can see that a
   column was withheld rather than empty.

### 4.4 Routes

```
GET /api/v1/admin/db/tables                          → []TableInfo
GET /api/v1/admin/db/tables/{table}?limit=&offset=   → RowPage
```

Both audited with the table name and offset. Reads are the entire risk surface
here, so reads are what the log is about.

---

## 5. Outbound message inspector

### 5.1 Why it proxies Mailpit

Every token in this schema is stored as a hash (`token_hash bytea`), so a
magic-link or invite URL cannot be reconstructed from the database. Storing raw
links to make this panel possible would create a new store of live credentials
to solve a convenience problem — the wrong trade by a wide margin.

Mailpit already holds the real messages and already runs in production
(`deploy/docker-compose.prod.yml`), bound to loopback. The inspector reads
Mailpit's HTTP API over the Compose network.

This removes a specific, documented production pain: `deploy/README.md`
describes handing someone an invite by opening an SSH tunnel to Mailpit and
copying the link out by hand.

### 5.2 The port

```go
// MailOutbox reads messages the product has sent. It exists so the operator
// can hand someone a link that mail cannot deliver (see ADR 3). The only
// implementation today reads Mailpit; when mail leaves the box this port gets
// a second one instead of a rewrite.
type MailOutbox interface {
    Recent(ctx context.Context, limit int) ([]OutboxMessage, error)
    Message(ctx context.Context, id string) (OutboxMessage, error)
}

type OutboxMessage struct {
    ID        string
    To        string
    Subject   string
    SentAt    time.Time
    Body      string // populated by Message, empty in Recent
}
```

Adapter: `internal/adapter/mail/mailpit_outbox.go`, configured by
`MAILPIT_API_URL` (`http://mailpit:8025`). Unset → the panel is unavailable and
says so, the same fail-closed shape as the DB browse.

### 5.3 Handling live links

Message bodies contain working magic-link, invite and sign-up URLs. Therefore:

- The list at `/admin/mail` shows recipient, subject and time **only**.
- Opening one message is a separate request, and its own audit row naming the
  recipient and message ID. Seeing a link is always a deliberate act with a
  record.
- The audit `detail` never contains the body or the URL.

---

## 6. Households and metrics

**Expanded in `2026-09-02-hearth-admin-households-design.md`, which wins where
the two differ.** That spec was written when this item came up for build; it
keeps everything below and adds the things a sketch this size could not settle
— the search predicate, the four counters exactly, `sessions.last_seen_at` and
why "last activity" is not `created_at`, the drill-in's lockout line, and the
decision that this item needs no configuration of its own (see §9).

`/admin/households`, reading tables that already exist. No analytics table is
added; a counter that can drift from the rows it counts is worse than a query.

- **List** — name, family name, member count, created date, last activity
  (the most recent session issued to any member), primary currency.
- **Counters** — households, members, sign-ups requested vs completed over the
  last 30 days, invites still pending.
- **Drill-in** — one household's members, their roles and capabilities, and
  whether each has ever signed in. **Not their money.**

That last line is a boundary, not an omission. Financial data is reachable
through the DB browse, which costs a deliberate second step and leaves a second
audit row. Casually browsing a customer's finances should not be one click away
from a support question.

---

## 7. The frontend

A `/admin` branch in the existing TanStack route tree, loaded with
`React.lazy` so no household member ever downloads it:

```
/admin                       AdminGate → AdminShell
  /admin/flags               feature flags
  /admin/db                  table list
  /admin/db/$table           rows
  /admin/mail                outbound messages
  /admin/households          households and metrics
  /admin/audit               the audit log — descoped 2026-09-02, see §2.4
```

- **`AdminGate`** renders the re-auth password prompt when the API answers
  `ADMIN_REAUTH_REQUIRED`, and renders the app's normal not-found page on a 404
  — so a non-admin who types `/admin` sees exactly what they would see typing
  any other nonsense URL.
- **`AdminShell`** is visually distinct from the household app (a different
  header treatment and an explicit "operator" label). Knowing which surface you
  are on should not require reading the URL.
- The sidebar's link to `/admin` renders only when `me.isPlatformAdmin` is
  true.

---

## 8. Testing

**Domain, no database:**
- `ResolveFlags` precedence: household beats global beats default; an unknown
  key is ignored; `Enabled` on an undefined flag is false.
- `ParseFlag` refuses an unknown string.
- The redaction predicate: `bytea`, `_hash`, `_secret`, `password`, denylist.

**HTTP (`admin_api_test.go`), the shape the existing `*_api_test.go` files use:**
- A signed-in non-admin gets 404 on every admin route.
- An admin with no grant gets 401 `ADMIN_REAUTH_REQUIRED`.
- A grant older than 30 minutes gets the same.
- A wrong password does not mint a grant, and lands in
  `admin_reauth_attempts`.
- Three wrong re-auth passwords lock the admin surface **and leave household
  password sign-in working** — asserted in one test, because that separation is
  the whole reason the second ledger exists.
- Three wrong household sign-ins lock household sign-in and leave an existing
  admin grant usable.
- Sign-out invalidates the grant with the session.
- Every admin route writes exactly one audit row, reads included.
- A route behind a disabled flag answers 404 while the flag is off and 200
  after it is turned on for that household only.
- A **public** route behind a disabled flag (`signups_open` on
  `POST /auth/sign-up`) answers 404 with no session at all, and ignores a
  household override that would have enabled it.
- Extending a session leaves `admin_grant_expires_at` untouched.

**Postgres, against testcontainers:**
- The read-only pool **fails** on an `INSERT`. This proves the role rather than
  the intention, and is the single most important test in the spec.
- An unknown table name is `ErrNotFound` rather than a SQL error.
- A `bytea` column comes back redacted.

**Frontend:**
- The admin chunk is absent from the main bundle graph.
- `useFeature` hides a nav item and its route together.
- `AdminGate` shows the password prompt on `ADMIN_REAUTH_REQUIRED` and the
  not-found page on 404.

At least one new test is mutation-checked per the `proving-tests-can-fail`
skill, and the whole surface is walked in a real browser before it is called
done — including signing in as a non-admin and confirming `/admin` is
indistinguishable from a typo.

---

## 9. Rollout

1. Migration `00012_admin.sql` — `platform_admins`, `feature_flags`,
   `household_feature_flags`, `admin_audit_log`, `admin_reauth_attempts`, and
   the `sessions.admin_grant_expires_at` column.
2. `adminctl grant-platform-admin` / `revoke-platform-admin` /
   `list-platform-admins`, and `unlock-admin`.
3. Admin shell, re-auth, audit middleware, `/admin/flags`. **This is a
   shippable slice on its own.**
4. `hearth_readonly` role and `DATABASE_READONLY_URL` in `deploy/PROVISION.md`;
   then the DB browse.
5. `MAILPIT_API_URL`; then the outbound inspector.
6. Households and metrics.

Each of 4 and 5 sits behind its own configuration and is unavailable without
it, so a box provisioned halfway degrades to "this panel is off", never to
something open. **6 needs none and is always available to a granted admin** —
corrected on 2026-09-02 when it was built: the database browse needs a
Postgres role and the inspector needs a Mailpit URL, but households and
metrics reads tables this schema already has, so there is nothing to provision
and therefore nothing to gate on
(`2026-09-02-hearth-admin-households-design.md`, decision 11).

---

## 10. Documentation owed

- **ADR 0005 — platform admin authorization and read-only web database
  access.** Records the new authorization axis, the re-auth grant, and the
  narrowing of adminctl's "no business behind an authenticated HTTP endpoint"
  position: mutations stay on the CLI, reads move to the web behind re-auth and
  an audit log.
- `docs/FEATURE_TRACKER.md` — new rows through the `hearth-product-driver`
  skill, with the summary **recounted** rather than guessed.
- `docs/SYSTEM_DESIGN.md` — through `maintaining-system-design`: a new
  authorization axis, four new tables, a new middleware chain, two new ports.
- `docs/INFRASTRUCTURE.md` — `DATABASE_READONLY_URL` and `MAILPIT_API_URL`,
  what each costs (nothing) and what breaks without it (one panel each).
- `docs/LEARNING.md` — on completion.

---

## 11. Deliberately out of scope

- **Writing to the database from the web.** Its own spec, its own ADR, if ever.
- **Impersonation.** Viewing a household as its members see it is a consent and
  privacy design of its own, and it would make the audit log load-bearing in a
  way nothing here does.
- **An ops/health panel.** `/healthz`, `/readyz`, backup age. Useful, not asked
  for, and cheap to add later on this foundation.
- **Admin levels.** One flat set of operators. `PlatformAdmin` is shaped so a
  level can be added as a field rather than by reinterpreting an existing one.
