# 5. Platform admin — a separate axis, granted only from the box

**Status:** Accepted — 2026-09-02. **Amended 2026-09-04** — see the amendment
below, which records what decision 4's narrowing looked like when the
read-only database browse finally tested it. Code-complete and reviewed
across eleven tasks, `make lint && make test` green, and **walked against the
dev stack the same day** — 15 of 15 criteria passed
(`docs/superpowers/plans/2026-09-02-hearth-admin-surface-verification.md`).
**The branch is not merged and nothing here is deployed.** The walk ran
against `localhost:5173`, not `oink.mywire.org`.

## Context

Today the only way to change what this install can do, or to look at its data
without going through the product's own screens, is a terminal and a key to
the box: `adminctl` for support operations, `psql` for everything else.
That is fine for one operator and painful the moment a second flag needs
flipping, a stranger needs their sign-up unstuck, or the operator is on a
phone with no shell in reach.

This slice builds the foundation and its first tenant — a re-authenticated
`/admin` surface, feature flags with a global default and a per-household
override, and an append-only audit log of who looked at what. It deliberately
does **not** build the read-only database browse or the outbound-mail
inspector the design spec also describes (its §§4–5) — those get their own
review once this foundation has run for a while. Nothing below should be read
as covering them; where they matter, this ADR says so explicitly rather than
by omission.

Two things were easy to get wrong here, and both are cheaper to name than to
undo.

## Decision

### 1. Platform admin is an axis orthogonal to household role and capability

`domain.Role` (`owner`, `limited`) and `domain.Capability` (`calendar`,
`chores`, `money`, `marriage`) answer **"what may this member do inside their
own household."** Platform admin answers a different question entirely:
**"who runs this install."** The two never merge.

`internal/domain/admin.go` carries a `PlatformAdmin` type with nothing on it
but an identity and a note:

```go
type PlatformAdmin struct {
    UserID    string
    Note      string
    CreatedAt time.Time
}
```

No `IsAdmin` field exists on `Membership`, and no admin check anywhere in the
codebase is ever expressed as a role or a capability. **Merging the two was
rejected** for a reason sharper than tidiness: every existing authorization
decision in this product — `requireCapability`, `requireOwner`,
`domain.ValidateMembershipChange` — is written on the assumption that a role
or capability is scoped to *one household*. An `IsAdmin` capability would be
the one flag in that whole system that, if it ever leaked past a household
boundary (a stale cache, a copy-pasted membership fixture, a scope built from
the wrong row), would grant install-wide reach rather than a mistake confined
to one family's data. Keeping platform admin as its own table with its own
lookup (`requirePlatformAdmin`, §2.3 below) means every place that decision is
made is a place built and reviewed for exactly that question, never a
side-effect of a household-scoped check being evaluated somewhere it
shouldn't be.

A platform admin is still an ordinary member of their own household and uses
the product normally — this matters mechanically as well as conceptually,
because `requireSession` resolves a membership and answers 401 without one; an
admin with no household of their own could not sign in at all.

### 2. Admins are created only by `adminctl`, never over HTTP

```
adminctl grant-platform-admin --email=<address> [--note=<why>]
adminctl revoke-platform-admin --email=<address>
adminctl list-platform-admins
adminctl unlock-admin --email=<address>
```

There is no "invite an admin" endpoint, no self-promotion path, and no way to
reach `PlatformAdminRepository.Grant` except from a command run on the box.
**Verified by grep, not assumed:** `.Grant(` appears at exactly one production
call site in the whole repository — `runGrantPlatformAdmin` in
`api/cmd/adminctl/main.go` — plus the repository's own test and one HTTP test
fixture, neither of which is a path a caller over the network can reach.

The reasoning is in `runGrantPlatformAdmin`'s own doc comment, and it is
worth repeating here because it is the load-bearing sentence of this whole
ADR:

> There is deliberately no HTTP route for this and no self-promotion path
> anywhere in the product: an admin surface that could mint its own admins
> would turn one stolen session into permanent access to every household's
> data. Keeping creation here, on a command a stranger cannot reach without a
> shell on this box, means the box itself is the boundary — not a password,
> not a session, not a role check that a bug could get wrong.

An admin surface that can create admins is a privilege-escalation engine with
extra steps: steal one admin's session, mint a second admin, and the theft
outlives the stolen cookie's own expiry. Requiring shell access on the box for
every admin grant and revoke means the thing an attacker has to compromise is
the same thing that was always the trust boundary for this install — the box
itself — rather than a new, web-reachable one this feature would otherwise
introduce.

### 3. The re-auth grant: thirty minutes, stamped on the session row

The household session cookie lives thirty days (`httpadapter.SessionTTL`) —
right for a member doing the ordinary product, and far too long for a surface
that can read every household's finances. Entering `/admin` therefore costs
the password again:

```
POST /api/v1/admin/session   { "password": "..." }  → 204
```

Verified with the existing `usecase.PasswordHasher`. On success, the session
row gets a stamped expiry:

```sql
ALTER TABLE sessions ADD COLUMN admin_grant_expires_at timestamptz;
```

**On the session row, not a second cookie**, for a reason that is really
about what happens on sign-out: revoking the session already happens today,
and a grant that lives inside the row it revokes dies with it for free. A
second cookie would need its own `Secure`/`SameSite`/`HttpOnly` flags gotten
right independently of the first, and a second place sign-out would have to
remember to clear.

**Not extended by activity, unlike the session itself.** A long admin session
is re-authenticated, not silently renewed — thirty minutes of continuous use
still asks again at the thirty-minute mark. `requireAdminGrant` answers `401
ADMIN_REAUTH_REQUIRED` rather than the ordinary `UNAUTHENTICATED` specifically
so the frontend can tell "your household session died" from "the admin
surface wants your password again" and show a prompt instead of bouncing the
operator out to sign-in.

### 4. This narrows, rather than overturns, `adminctl`'s own written position

`api/cmd/adminctl/main.go`'s package comment states plainly what the CLI is
for:

> Command adminctl is Hearth's operational CLI: the seed that gives a clean
> checkout its bootstrap household, and the handful of support operations
> (resetting a password, unlocking a locked-out household, inviting a new
> member) that are genuinely operator actions and have no business behind an
> authenticated HTTP endpoint.

This slice does not overturn that position. It narrows it, in exactly two
places, and leaves it standing everywhere else:

- **Who gets to be an operator stays on the CLI, permanently.** Granting and
  revoking platform admin — the one action that decides who can reach
  `/admin` at all — has no HTTP route and never will under this design (§2
  above). This is the position `adminctl`'s comment states, applied to the
  highest-stakes mutation this feature has.
- **A narrow, audited, reversible product-configuration change moves to the
  web, behind re-authentication and a permanent log.** Toggling a feature
  flag (§3 of the design spec) is a mutation, but it is not a database write
  in the sense `adminctl`'s comment is protecting against — it flips a row in
  a table built for exactly this, every toggle is logged, and turning a flag
  back off restores the prior behaviour exactly. That is the shape the
  spec's own §4 sets out for the future read-only database browse too:
  **mutations against the database's actual rows stay in `adminctl` over
  SSH; reads move to the web behind re-authentication and an audit log.**
  This branch does not build that browse — it builds the pattern's first,
  narrowest instance, and inherits its reasoning.

Nothing here pretends the tension is absent. Moving *any* operator action off
a terminal a stranger cannot reach and onto a web endpoint a stolen session
can reach is exactly the cost `adminctl`'s comment was written to avoid. What
bounds it, and what this ADR is on record as accepting: every such route sits
behind a password re-entered within the last thirty minutes, and every
request against it — reads included — leaves a permanent row in
`admin_audit_log` that nothing in the product can delete.

### 5. The separate re-auth ledger

Failed re-authentication attempts get their own table:

```sql
CREATE TABLE admin_reauth_attempts (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    succeeded boolean     NOT NULL,
    at        timestamptz NOT NULL DEFAULT now()
);
```

This is deliberate, not incidental, and it is close enough to a real defect
that it earns its own entry in `docs/LEARNING.md` as well as this one.
`login_attempts`, the ledger `AuthService.SignIn` already writes to, locks
password sign-in **per household** — three failures inside fifteen minutes
locks every member of that household out, by design, because the recovery
path (a magic link) does not depend on the password at all. Feeding admin
re-auth mistypes into that same table would mean an operator fumbling their
*own* password, on a screen nobody else in their household can even see,
locks their entire family out of the ordinary product as a side effect.

The `AdminReauthService` struct's own doc comment states the reasoning
(`api/internal/usecase/admin_reauth.go`) — not `Verify`'s own comment
further down, which is shorter and about something else:

> Its failures are counted in their own ledger, never in `login_attempts`.
> That table's lockout is household-scoped, so an operator's mistypes there
> would lock their whole household out of the ordinary product — a bad
> outcome caused by a screen nobody else can even see.

The *policy* is reused even though the ledger is not: the same
`domain.LockoutPolicy.Evaluate` runs over `admin_reauth_attempts`, keyed on
`user_id` rather than `household_id`. A locked admin surface therefore never
touches household sign-in, and household sign-in never locks the admin
surface — two independent failure domains sharing one policy shape.

**This is not a theoretical distinction; the walk proved it holds in the real
system.** Criterion 6 of the browser verification: three wrong admin
passwords answered `423 ADMIN_LOCKED`, and in the same browser session,
household password sign-in continued to answer `200`. The separation the
table exists to guarantee is not merely coded — it was exercised end to end
against the running dev stack.

## Consequences

**The cost is real, and it is the same cost §4 of the design spec names for
the (not-yet-built) database browse.** An admin session re-authenticated
within the last thirty minutes can read every household's flag state and
toggle it, globally or for one household. That is why every such request is
audited, why the grant expires on a fixed clock rather than sliding with
activity, and why creating the admin in the first place needed a shell on the
box. Audit is an accountability mechanism, not a preventive one — it does not
stop a misuse, it makes one impossible to hide afterward.

**The accepted limits, stated rather than discovered by someone else later:**

- **An unauthenticated caller gets `401`, not `404`, on `/api/v1/admin/*`.**
  `requirePlatformAdmin` must run after `requireSession` — there is no
  membership or admin status to check without one — so a stranger with no
  credentials at all can already tell the admin subtree apart from a
  genuinely unrouted path, by the status code alone.
  `requirePlatformAdmin`'s own doc comment records this as an accepted limit,
  not an oversight: `TestEveryProtectedRouteRejectsAnUnauthenticatedCaller`
  requires exactly that 401 of every protected route in this codebase, and
  carving out an exception for `/admin` alone would be a bigger structural
  change than the leak justifies — especially since `GET /auth/me` puts
  `isPlatformAdmin` on every response anyway, so the surface's *existence* was
  never the secret. What the 404-to-a-signed-in-non-admin guard protects is
  narrower and still real: a household member poking at the API from inside a
  live session learns nothing.
- **A locked admin surface stays locked under continued guessing, and the
  lock is not visible until someone tries.** Every failed attempt —
  including one made while already locked — extends the lock, the same
  choice `AuthService.SignIn` makes on its own locked branch. There is no
  in-product early exit; `adminctl unlock-admin --email=` on the box is the
  only way back in before it expires on its own. The browser walk surfaced a
  sharp edge of this that the design did not call out in advance: on reload
  while locked, the admin surface answers with the ordinary
  `ADMIN_REAUTH_REQUIRED` password prompt, not a lockout message — the lock
  is discoverable only by *submitting* the form, and a submission made while
  still locked is itself a recorded failure that pushes the expiry further
  out. This is accepted for the same reason the rest of this section is: the
  alternative (answering differently to a locked caller before they submit
  anything) would let an unauthenticated prober distinguish "locked" from
  "not yet tried," which is a smaller leak solved by creating a larger one.

## Amendment — 2026-09-04: what the database browse proved

**Amended, not superseded.** Everything above stands. The "Revisit this when"
list below named the read-only database browse as the moment decision 4's
narrowing — *mutations against the database's actual rows stay in `adminctl`
over SSH; reads move to the web behind re-authentication and an audit log* —
would first be tested against a real database read rather than a flag toggle.
It has been built (branch `admin-db-browse`, spec
`docs/superpowers/specs/2026-09-04-hearth-database-browse-design.md`). This
is what held and what did not.

**The status of the work, stated plainly, because this ADR's own header sets
the bar:** code-complete and reviewed, `make lint && make test` green, and
**its fifteen-criterion browser walk ran on 2026-09-04 and passed 15 of 15**
(`docs/superpowers/plans/2026-09-04-hearth-database-browse-verification.md`)
— so it now meets the same bar as the walk this ADR's Status line cites for
the first slice, and what follows is a claim about a browse that was driven
in a browser.

The walk mattered to *this* ADR's central claim more than to most, because
the narrowing below rests on a guard that is Postgres's rather than this
codebase's, and a guard is a claim until someone attacks it. It was attacked:
the write refusal was run as `hearth_readonly` against the live role, and
**both of its guards were proven to hold independently** — a plain `INSERT`
is refused by `default_transaction_read_only` (`cannot execute INSERT in a
read-only transaction`), and `BEGIN; SET TRANSACTION READ WRITE`, the one
statement that switches that guard off, then meets the `GRANT`
(`permission denied for table households`). `UPDATE`, `DELETE`, `DROP TABLE`
and `CREATE TABLE` were all refused too, and nothing was written.

**What the walk did not change, and must not be read as changing:**
production ships with `DATABASE_READONLY_URL` unset by the product owner's
decision on 2026-09-04. The narrowing is verified; it is not yet exercised on
the live box, and will not be until an operator runs `deploy/PROVISION.md`
section 10.

### 1. The narrowing held, and the guard turned out not to be this codebase's

No write reaches the web. The browse has two routes and both are `GET`s; the
port it depends on, `usecase.DatabaseBrowser`, has two methods and neither
can mutate anything; and every statement the adapter issues is a `SELECT` —
over the household tables, over `information_schema`, and (for the ordering
key) over `pg_index`, `pg_attribute` and `to_regclass`. The catalogue it
reads from is wider than `information_schema` alone; what matters is that all
of it is reads. But the load-bearing part is not any of that, and this is the sentence worth
carrying forward: **the guard is Postgres's, not ours.** The browse reads
through its own connection pool as `hearth_readonly`, a role granted `SELECT`
on `public` and nothing else, with `default_transaction_read_only` set on the
role itself. A write attempted through that pool — by a bug, a bad refactor,
or someone who did not read this file — is refused by the database before
anything in this repository has an opinion about it.

That is a stronger position than decision 4 was written from. Decision 4
bounded the risk with *process* (re-authentication, an audit row). Here the
process is still there, and underneath it is a mechanism that does not depend
on anyone's care. The API even refuses to start if the read-only pool turns
out to be able to write — checked on every connection the pool opens, not
once at boot — because serving a "read-only" browse through a writable
connection is worse than serving no browse at all.

**One line in Consequences above needs qualifying because of this.** It says
audit "is an accountability mechanism, not a preventive one — it does not
stop a misuse, it makes one impossible to hide afterward". That is exactly
right for the feature flags it was written about, where the only thing
standing between an admin session and a changed row is the operator's own
judgement. It is not the whole story for the browse: here the *primary*
guard is preventive, and the audit log is the second layer rather than the
only one. Reading is still only accountable-after-the-fact — see §3 below —
but writing is genuinely prevented.

### 2. What this ADR did not anticipate: a role is not schema, and not backup

The thing decision 4 got wrong was not the direction of the narrowing. It
was assuming that "reads move to the web" is a change to this codebase.

`hearth_readonly` is a **cluster-level** object, and that is what puts it
outside the migration path. Not "because migrations are only for tables" —
goose runs arbitrary SQL, and this repository's own
`api/migrations/00002_identity.sql` opens with `CREATE EXTENSION IF NOT
EXISTS citext;`, which is not a table either. The real reason is privilege:
`CREATE ROLE` requires the `CREATEROLE` attribute, and the role migrations
run as need not hold it — making every migration able to mint roles, so that
one of them can, is a larger grant than the feature is worth. So it is
created by `deploy/readonly-role.sql`, run during provisioning, by hand, by a
person with a shell on the box. And because roles live in the cluster rather than in
a database, `deploy/backup.sh` — one `pg_dump` of one database, with
`--no-privileges` — captures neither the role nor its grants. **A restore
that looks complete leaves the browse broken**, and the failure surfaces days
later, on the one day somebody needs it. The instruction is to re-run the
script after every restore; it is idempotent, which is what makes that a
whole instruction rather than a checklist (`deploy/README.md`).

**This makes the browse the admin surface's first genuinely infrastructural
dependency.** Every earlier feature in this surface was code and tables:
`platform_admins`, `feature_flags`, `admin_audit_log`, `sessions.last_seen_at`,
and — for the mail inspector — a container that was already running. All of
those arrive with a deploy. This one does not. Turning it on in production
is a provisioning step (`deploy/PROVISION.md` §10), not a release, and the
product owner's decision on 2026-09-04 was to ship it **dark**: merged and
deployed with `DATABASE_READONLY_URL` unset, so the deployed panel says it is
not configured and names the variable until someone chooses otherwise.

Nothing in this ADR predicted that, and the reason it matters beyond this one
feature is the shape: the admin surface can now grow a dependency that a
deploy does not satisfy and a backup does not preserve. The next such
dependency should be recognised as one on the day it is proposed.

### 3. The accepted cost, restated in its sharpest form

Consequences above says an admin session re-authenticated within the last
thirty minutes "can read every household's flag state and toggle it". That
was the true statement when it was written. This is the true statement now:

> **A re-authenticated admin session can read every household's finances —
> every account, balance, transaction and goal — one page at a time, with an
> `admin_audit_log` row per page.**

Not one household's, not on request, not with anyone's consent. That is not a
new decision; it is the cost §4 of the design spec named and this ADR agreed
to in advance. It is stated here in those words because the earlier phrasing
is about a feature-flag screen, and someone reading only that would
underestimate by an order of magnitude what a stolen admin session is now
worth.

What bounds it is unchanged and is worth re-reading as a set rather than as
five separate mechanisms: the admin must have been granted from a shell on
the box (§2); the grant costs the password again within the last thirty
minutes and does not slide with activity (§3); every page view — reads
included — writes an audit row before the handler runs, carrying the table in
`Target` and the offset in `detail.query` (which is why paging lives in the
URL); columns holding secrets are never selected at all, so `password_hash`
and every `bytea` render `«redacted»` and their bytes never leave Postgres;
and there is no SQL box, no filter, no join and no export — a page of rows on
a screen, and `psql` over SSH for anything more. **The bound that is most
easily lost is the last one.** Each of the missing features is individually
reasonable to ask for, and the first one added turns a bounded read into an
arbitrary one.

## Revisit this when

- **Admin levels are wanted.** `PlatformAdmin` carries no permission field on
  purpose — one flat set of operators today — specifically so a level, if it
  is ever needed, is a field added here rather than a reinterpretation of
  something that already means something else.
- ~~**The read-only database browse (design spec §4) is built.**~~ **Done —
  2026-09-04.** This was the moment the "mutations stay on the CLI, reads
  move to the web" narrowing in decision 4 above was tested against an actual
  database read rather than a flag toggle. The amendment above is that
  record; the bullet is kept rather than deleted so the trigger and its
  outcome sit together.
- **A second operator exists.** Nothing here assumes exactly one; `adminctl
  list-platform-admins` and the audit log's `actor_user_id` already support
  more than one, but the product has only ever been run by one, and that is
  worth re-examining the day it isn't.

## See also

- `docs/superpowers/specs/2026-09-01-hearth-admin-surface-design.md` — the
  design this was built from, including the database browse and mail
  inspector this ADR narrows the position for but does not build.
- `docs/superpowers/specs/2026-09-04-hearth-database-browse-design.md` — the
  browse's own design, which the 2026-09-04 amendment above records the
  outcome of. Its decisions 1–4 are where "the guard is Postgres's" is
  argued in full.
- `docs/INFRASTRUCTURE.md` — `DATABASE_READONLY_URL`, the `hearth_readonly`
  password, and what breaks without either; `deploy/PROVISION.md` §10 is how
  the role is created.
- `docs/superpowers/plans/2026-09-02-hearth-admin-surface-verification.md` —
  the 15-criterion browser walk, including the criterion 6 result this ADR
  cites for the ledger separation.
- `docs/SYSTEM_DESIGN.md` §3, §4 and §6 — the ports, the `/admin` middleware
  chain, and the five new tables.
- `docs/FEATURE_TRACKER.md` — what shipped in this slice and what did not
  (the flags screen's own gaps, found on the walk).
- `docs/LEARNING.md` — the near-miss this ADR's decision 5 avoided, and what
  would have caught it sooner if it hadn't been.
