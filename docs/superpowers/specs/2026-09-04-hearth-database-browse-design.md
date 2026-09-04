# Hearth — the read-only database browse

**Written 2026-09-04.** This expands §4 of
`docs/superpowers/specs/2026-09-01-hearth-admin-surface-design.md` the way
`2026-09-02-hearth-admin-households-design.md` expanded §6 and
`2026-09-04-hearth-outbound-inspector-design.md` expanded §5. **Where the two
differ, this one wins**, and §13 lists every difference so nobody has to diff
them by hand.

It is the fourth and last feature of the operator surface. It was always
last, for two reasons that still hold: it is the only one with an
infrastructure dependency outside this repository, and it has by far the
largest security surface. The other three exist in part so that the
re-authentication grant and the audit log had real use before this arrived.

---

## 1. What this is, and the pain it closes

Today, answering "why does this household's net worth look wrong" means SSH
to the box, `docker exec ... psql`, and typing SQL as the read-write
application role. That is the most privileged connection in the system,
opened by a human under time pressure, with no record that it happened.

The browse is the boring 90% of that, moved into the operator surface: pick
a table, read a page of rows. It runs as a role that **cannot write**, it
costs a re-authentication, and every page view leaves an `admin_audit_log`
row.

**It closes an operational pain. It does not add a product capability.** No
household member can see any of it, and nothing about the product's
behaviour changes when it is switched on.

It also closes one gap left by an earlier decision. The `/admin/audit`
screen was built and descoped on 2026-09-02, leaving `admin_audit_log`
written but readable only through `psql`. `admin_audit_log` is a table in
`public` like any other, so this browse reads it — not as a replacement for
the screen that was cut, but as the general tool that makes the specific one
unnecessary.

## 2. What it is not

- **Not a SQL console.** There is no place to type a query. See decision 16.
- **Not a writer.** No `UPDATE`, no `DELETE`, no `INSERT`, at any layer. The
  narrowing in [ADR 5](../../adr/0005-platform-admin-authorization.md) is
  "mutations stay on the CLI, reads move to the web", and this is the read
  half. `adminctl` over SSH remains the only way to change anything.
- **Not a query builder.** No filters, no sorting controls, no joins, no
  aggregate. Decision 16.
- **Not an export.** No CSV, no download. A page of rows on a screen.
- **Not a schema editor or a migration tool.** It reads
  `information_schema`; it never writes DDL.
- **Not a support tool for household members.** It is behind
  `requirePlatformAdmin`'s 404 like the rest of `/admin`.

---

## 3. Decisions

### Decision 1 — The Postgres role is the guard; everything else is depth

The four guards in the parent spec's §4.3 are deliberately ordered by how
much they are trusted, and that ordering is a real instruction to the
implementer: **only the first one holds if the code is wrong.**

A validated table name, a capped limit and a redaction rule are all
application logic, and application logic is what has bugs. A role granted
`SELECT` and nothing else is enforced by Postgres, on the other side of the
wire, and stays true through any refactor of this repository.

Everything below serves that ordering. Where a guard could be implemented
either in the role or in the adapter, it goes in the role, and the adapter
does it too.

### Decision 2 — `ALTER DEFAULT PRIVILEGES`, or next year's table is invisible

`GRANT SELECT ON ALL TABLES IN SCHEMA public TO hearth_readonly` grants on
the tables that exist **at the moment it runs**. Migration `00014` creates a
table the role cannot see, and `information_schema.tables` lists only what
the current role has privileges on — so the new table does not appear in the
browse, no error is raised anywhere, and the operator concludes the feature
is fine because a long list of tables came back and nobody counts a long
list.

So the role script also runs:

```sql
ALTER DEFAULT PRIVILEGES FOR ROLE hearth IN SCHEMA public
    GRANT SELECT ON TABLES TO hearth_readonly;
```

`FOR ROLE hearth` is load-bearing: default privileges attach to the role that
*creates* the object, and migrations run as `hearth`. Written without it, the
statement attaches to whoever ran the script, which on the box is also
`hearth` and in a testcontainer is also `hearth` — so it would appear to work
everywhere and fail the first time anyone runs a migration as a different
role. Say it explicitly rather than rely on the coincidence.

This is the same failure shape as decision 8's type rule: a thing added next
year, by someone who has never read this file, must land in the right state
without being told.

### Decision 3 — Read-only and the timeout live on the role, not in a transaction

The parent spec says `SET LOCAL statement_timeout = '3s'` on the browse
connection. `SET LOCAL` only applies inside an explicit transaction, and an
adapter that forgets to open one gets no timeout and no error. So both
settings are attached to the role itself:

```sql
ALTER ROLE hearth_readonly SET default_transaction_read_only = on;
ALTER ROLE hearth_readonly SET statement_timeout = '3s';
```

`default_transaction_read_only` is a second, independent refusal of writes
that does not depend on the `GRANT` being right, and it costs one line.

**The adapter sets the timeout too**, through the pool's connection
`RuntimeParams`, so a database whose role predates this decision — an
existing box provisioned from an older `PROVISION.md`, say — is still
bounded. Two mechanisms for one rule is correct here: they fail
independently.

### Decision 4 — The API refuses to boot if the read-only pool can write

Someone will eventually paste the read-write URL into `DATABASE_READONLY_URL`
to make a broken panel work, at 1 a.m., meaning to change it back. That is
not a hypothetical failure mode; it is the ordinary one.

So on startup, after opening the second pool, the API asserts:

```sql
SELECT has_table_privilege(current_user, 'users', 'INSERT');
```

`true` refuses the boot with a message naming the variable. This is what
turns guard 1 from a claim in a document into a property of the running
process, and it follows the precedent `config.go` already sets by refusing a
half-set Telegram pair rather than starting with the feature silently
mis-wired.

`users` is the probe table because it exists in migration `00002`, is never
dropped, and holds credentials — if the connection can write *there*, nothing
else about the configuration matters.

The same check also runs in the pool's `AfterConnect`, so it holds on every
connection the pool ever opens rather than only on the first. A boot-time-only
check is true about the process that started; this one stays true after a
reconnect, and after somebody grants the role something at 2 a.m.

**A database that is merely unreachable at boot is a different case and must
not refuse the boot** — see decision 12.

### Decision 5 — One idempotent SQL file, three consumers

The role is created by provisioning, not by a migration, because migrations
do not run as a superuser and creating roles is not schema. But "not a
migration" must not mean "typed by hand three different ways", so the
statements live in exactly one file, `deploy/readonly-role.sql`, consumed by:

| Consumer | How |
|---|---|
| The dev stack | A one-shot Compose service running `postgres:17-alpine` with the file mounted, after `migrate` completes (`condition: service_completed_successfully`). The `api` dev image carries `goose`, not `psql`, so it cannot be the one that runs this |
| The Go suite | `testsupport.StartPostgres`, immediately after `applyMigrations`, executing the same file through pgx |
| Production | One `psql` step in `deploy/PROVISION.md` |

`api` in turn declares `depends_on` the one-shot with
`condition: service_completed_successfully`, or `make dev` races it and the
boot-time self-check in decision 4 fails on a role that is about to exist.

**The file contains no password**, and that is not tidiness. `psql`'s
`:'variable'` interpolation is a client-side feature: a file using it is
executable by `psql` and by nothing else, and `testsupport` sending that line
to the server through pgx gets a syntax error. Each consumer therefore sets
the password itself, in one statement after running the file — the dev value
`hearth-readonly` written in `docker-compose.yml` beside the database
credentials already there, the test value inside `testsupport`, and the
generated production value in `deploy/PROVISION.md`, which is the only one
that is a secret.

It must be idempotent, because the dev service runs on every `make dev` and
the suite runs it per container. Postgres has no `CREATE ROLE IF NOT
EXISTS`, so the file guards the create in a `DO` block and lets the
`GRANT`s — which are naturally idempotent — run unconditionally.

**Not `docker-entrypoint-initdb.d`.** That directory runs only when the data
directory is initialised, and `hearth-pgdata` already exists on every machine
this product has ever run on. A developer would pull the branch, run `make
dev`, and get a panel that says it is not configured, with the fix being to
destroy their database.

### Decision 6 — Validation runs on the read-only pool, so it doubles as the privilege check

The requested table name is matched against `information_schema.tables`
(`table_schema = 'public'`, `table_type = 'BASE TABLE'`) **through the
read-only pool**, not the application pool.

`information_schema` is privilege-filtered per role. Asking the read-only
role therefore answers a stronger question than "does this table exist": it
answers "does this table exist *and* may this connection read it". A table
the `GRANT` missed — decision 2's failure — answers `domain.ErrNotFound`
rather than reaching a `SELECT` that fails with a raw Postgres permission
error the operator has to interpret.

Only after a match is the name quoted with `pgx.Identifier{name}.Sanitize()`.
The name from the URL is never concatenated into SQL, in either order of
those two steps.

### Decision 7 — Redaction happens inside the `SELECT` list

The obvious implementation reads every column and drops the redacted ones in
Go. This one never asks for them:

```sql
SELECT '«redacted»'::text, "email"::text, "created_at"::text FROM "users" ...
```

A redacted column contributes a constant to the select list. The bytes do not
travel over the wire, do not enter this process's memory, and cannot appear
in a heap dump, a panic value, a slow-query log or a future debug print of
the row slice.

The select list is built from the column list `information_schema.columns`
returned for the validated table — never from anything in the request — so
this is not a place where input reaches SQL either.

Casting to `text` in Postgres rather than formatting driver values in Go is
the second reason: the adapter then has no driver types to map, and
`RowPage.Rows [][]string` is what the query literally returns. `jsonb`,
`interval`, arrays and enums all render the way `psql` renders them, which is
the rendering the operator already knows.

### Decision 8 — Type first, name second, denylist third — and the denylist starts almost empty

A column is redacted when **any** of these holds:

1. its `data_type` is `bytea`,
2. its name ends in `_hash` or `_secret`, or contains `password`,
3. it appears in `domain`'s explicit denylist.

Rule 1 is the one that survives. Every token in this schema is stored as
`token_hash bytea` (`sessions`, `magic_links`, `invites`, `signups`,
`telegram_link_nonces`), so a secret column added by a migration in 2027 is
redacted before its author has heard of this file.

Rule 2 catches the one credential that is not `bytea`:
`users.password_hash` is `text`, because an Argon2 encoded hash is a
self-describing string. Rule 2 exists precisely because rule 1 is not
complete, and this column is the proof.

Rule 3 is the escape hatch for a column that is secret in a way neither its
type nor its name reveals. **It starts with no entries, deliberately**, and
each one added must carry a comment saying why the first two rules missed it.
A denylist pre-filled with guesses becomes the thing people trust, and then
the type rule stops being maintained.

Redaction is decided per column and reported to the UI as
`ColumnInfo.Redacted`, so the operator sees a column that was **withheld**,
never a column that looks empty. "There is no value here" and "you may not
see the value here" are different facts and the screen must not merge them.

### Decision 9 — `NULL` and the empty string must be distinguishable

`RowPage.Rows` is `[][]string`, so a `NULL` and an empty text column would
both arrive as `""` and a junior engineer reading the screen would conclude
they are the same thing. They are not, and in this schema the difference is
sometimes the bug being investigated (`users.password_hash` is `NULL` for a
member who has only ever used a magic link).

`NULL` renders as `«null»`, produced in SQL by `coalesce(col::text,
'«null»')`. The empty string renders as nothing at all. The guillemets are
the same marker `«redacted»` uses, and the screen explains both in one line
of legend rather than in a tooltip nobody opens.

A real value that happens to be the literal text `«null»` is
indistinguishable from a `NULL`. Accepted: no such value exists in this
schema, and the alternative — a per-cell null flag doubling the wire format —
buys nothing an operator would use.

### Decision 10 — Paging is ordered by the primary key, `ctid` when there is none

`LIMIT`/`OFFSET` without `ORDER BY` gives Postgres permission to return rows
in any order it likes, and it exercises that permission: page 2 can repeat a
row from page 1 and skip another entirely. Nothing errors. The operator reads
a table and simply does not see a row that is there.

So every row query orders by the table's primary-key columns, discovered from
`pg_index`/`pg_attribute` for the validated table, ascending, in key order.
A table with no primary key orders by `ctid` — an arbitrary but stable
ordering within a page-read, which is honest about being arbitrary.

This is not sorting as a feature. The operator cannot choose it, and it does
not appear as a control. It exists so that "page 2" means something.

### Decision 11 — Exact `count(*)`, not `reltuples`

`TableInfo.RowCount` and `RowPage.Total` are real counts. The estimate from
`pg_class.reltuples` is cheaper and is the right answer on a database where
`count(*)` is a sequential scan of hundreds of millions of rows. This
database has a few dozen small tables, a 3-second ceiling, and one
operator.

Taking the estimate today would mean labelling every figure "approx" on a
screen whose entire job is to show what is actually there. If a table ever
grows enough for the count to hit the timeout, the timeout is the signal to
revisit this, and the failure is loud.

### Decision 12 — Unconfigured, misconfigured and unavailable are three different answers

The parent spec says unset config means the browse is unavailable and the UI
says so. That must never degrade to an empty table list, which reads as
"this database has no tables".

| State | Answer | Code | What the screen says |
|---|---|---|---|
| `DATABASE_READONLY_URL` unset | `503` | `DB_BROWSE_NOT_CONFIGURED` | Not configured on this install, and names the variable |
| Set but unparseable | — | — | **The API does not start.** A typo in `.env` is provably wrong, and a `503` would hide it behind a screen nobody reads |
| Set, connects, and the write self-check passes when it should fail | — | — | **The API does not start.** Decision 4. This is the read-write URL in the read-only slot, and serving anything through it is worse than serving nothing |
| Set and parseable, but the database is unreachable at boot | `503` | `DB_BROWSE_UNAVAILABLE` | **The API starts.** The browse is unavailable and `slog.Error` names the variable |
| Table not in `information_schema` for this role | `404` | `NOT_FOUND` | This table does not exist, or is not readable by the browse role |
| `limit` or `offset` present but not an integer, or `offset` negative, or `limit` below 1 | `400` | `INVALID_RANGE` | Refused before any query runs |
| Query exceeded `statement_timeout`, or the read-only pool is down | `503` | `DB_BROWSE_UNAVAILABLE` | The reader is broken, not the data |

The unreachable-at-boot row is the one that matters most and is the least
obvious. The day it happens is the day someone is restoring this product onto
a fresh box from the paper key, with `DATABASE_READONLY_URL` already in
`.env` and the role not created yet. Refusing the boot there would take the
whole product down over an operator panel — the exact inversion of guard 1's
own promise that a half-provisioned box degrades to "you cannot use this",
never to something worse. A misconfiguration a human typed is refused; an
environment that is not ready yet is waited out.

Neither unavailability answers `404`. `requirePlatformAdmin` and
`requireFeature` both use `404` to hide a route's *existence*; everyone who
reaches this handler has already proved they are an admin with a live grant,
so hiding it from them costs them the one fact that says what to fix. This is
the shape `2026-09-04-hearth-outbound-inspector-design.md`'s decision 10
defined, and the parent spec's §5.2 anticipated the browse would copy. It
does.

### Decision 13 — `Deps.AdminBrowse` is nil when unconfigured, and the route is registered anyway

The same shape `Telegram` and `AdminOutbox` already have: the route tree does
not change with configuration, so every test builds the same tree, and the
handler's nil check is the one place the decision lives.

### Decision 14 — A second pool, sized for one operator

`postgres.Open` hardcodes `MaxConns = 10`, which is right for the
application's request traffic and wrong for a panel one person clicks. The
read-only pool is built by its own constructor with `MaxConns = 3`, so a
runaway browse cannot consume a share of the database's connection budget
that the product needs.

It is a separate constructor rather than a parameter on `Open`, because the
two pools differ in more than size: this one also carries the
`statement_timeout` runtime parameter from decision 3 and runs the self-check
from decision 4. A `Open(ctx, url, opts...)` that grew three options would
put the read-only decisions in the read-write path's file, where nobody
looking for them would find them.

### Decision 15 — No feature flag

`DATABASE_READONLY_URL` already is the switch, and it is the one that
matters: it is the presence of the credential. A feature flag would add a
second switch that can disagree with the first, and the disagreement would
read as a bug in the panel. Same reasoning as the mail inspector's decision
14.

### Decision 16 — No search, no filter, no free SQL, in this version or by default

Every one of these is a real request an operator will make, and each is
refused for its own reason:

- **A SQL box** turns the whole feature into "arbitrary read access to every
  household's data with a text field", where the only guard left is the role.
  Decision 1 says the role is the guard that holds — that is an argument for
  depth, not for removing everything else.
- **A `WHERE` builder** puts user input into SQL, which is precisely what
  decision 6 exists to prevent. It is buildable safely with parameters and a
  validated column list; it is not buildable *cheaply* safely, and no operator
  has needed it yet.
- **Sorting controls** would make decision 10's ordering look like a
  preference instead of the correctness property it is.

The escape hatch already exists and is documented: `psql` over SSH, for the
questions this screen cannot answer. The point of the browse is that the
common question no longer needs it.

### Decision 17 — The audit row needs no middleware change

The parent spec's §4.4 requires both routes to be audited "with the table
name and offset". `auditAdmin` writes `Target = r.URL.Path` and
`Detail.query = r.URL.RawQuery`, before chi has matched the route. The table
name is a path segment and the offset is a query parameter, so a request to
`/api/v1/admin/db/tables/transactions?limit=50&offset=100` already records
both, by construction, in the row the existing middleware writes.

This is worth stating because the obvious reading of §4.4 is "add fields to
the audit entry", and doing so would move an invariant out of middleware that
cannot forget into handlers that can.

### Decision 18 — An offset past the end is an empty page, not a 404

`offset=1000` on a 12-row table answers `200` with `Rows: []`, `Total: 12`.
It is a valid question with a boring answer. A `404` there would be
indistinguishable from the table not existing, which is decision 12's other
`404` and a genuinely different problem.

---

## 4. The layers

```
domain/dbbrowse.go          Redaction rules. Pure, stdlib only, no SQL.
usecase/ports.go            DatabaseBrowser — the port, verbatim from §4.2.
usecase/admin_browse.go     AdminBrowseService — clamps limit/offset, nothing else.
adapter/postgres/readonly_pool.go   The second pool, its params and its self-check.
adapter/postgres/browse_repo.go     The DatabaseBrowser implementation.
adapter/http/admin_browse_handlers.go   Two handlers in the granted group.
web/src/features/admin/AdminDatabasePage.tsx   One lazy page.
```

The redaction rules live in `domain` for the same reason `ExtractLinks` does:
they are a decision about the product, testable as a pure function against a
table of column names and types, and they must not be reachable only through
a database. `domain` may not import `pgx`, which is also why `ColumnInfo`
carries `DataType string` — the type as `information_schema` names it, mapped
in the adapter.

`AdminBrowseService` is thin on purpose. It defaults `limit` to 50 when absent
and **clamps a `limit` above 100 down to 100 rather than refusing it** —
asking for more than the cap is a reasonable request with a bounded answer,
where `limit=0` or `offset=-1` is not a request at all and gets decision
12's `400`. Then it calls the port. It
does not know about SQL and it takes no actor parameter, because
authorisation lives in the HTTP layer.

## 5. The read-only role

`deploy/readonly-role.sql`, run by the three consumers in decision 5:

```sql
-- Creates the SELECT-only role the admin database browse reads through.
-- Idempotent: every consumer in the spec's decision 5 runs it repeatedly.
-- Postgres has no CREATE ROLE IF NOT EXISTS, hence the DO block.
--
-- Run as a role that may create roles and that owns the tables (`hearth` in
-- every environment today).
--
-- It sets no password, deliberately: `psql`'s :'var' interpolation is a
-- client-side feature, and a file that uses it cannot be executed by the Go
-- test suite through pgx. Every consumer sets the password itself in one
-- statement afterwards -- see the spec's decision 5.

DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'hearth_readonly') THEN
        CREATE ROLE hearth_readonly LOGIN;
    END IF;
END
$$;

-- Read-only, twice: once by what is granted, once by what the role's own
-- session default allows. The two fail independently. See decision 3.
ALTER ROLE hearth_readonly SET default_transaction_read_only = on;
ALTER ROLE hearth_readonly SET statement_timeout = '3s';

GRANT CONNECT ON DATABASE hearth TO hearth_readonly;
GRANT USAGE ON SCHEMA public TO hearth_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO hearth_readonly;

-- Without this, a table created by a migration after today is invisible to
-- the browse and nothing reports it. See decision 2.
ALTER DEFAULT PRIVILEGES FOR ROLE hearth IN SCHEMA public
    GRANT SELECT ON TABLES TO hearth_readonly;
```

Nothing else is granted. No `USAGE` on sequences, no `EXECUTE` on functions,
no `CREATE` anywhere.

## 6. HTTP surface

Both routes sit inside the existing `/admin` granted group in `router.go`, so
`requirePlatformAdmin`, the re-auth grant check, `auditAdmin` and
`requireCSRF` all apply by construction. Neither handler checks who is
asking.

```
GET /api/v1/admin/db/tables
```

```json
{
  "tables": [
    {
      "name": "accounts",
      "rowCount": 14,
      "columns": [
        { "name": "id", "dataType": "uuid", "redacted": false },
        { "name": "household_id", "dataType": "uuid", "redacted": false }
      ]
    }
  ]
}
```

```
GET /api/v1/admin/db/tables/{table}?limit=50&offset=0
```

```json
{
  "table": "accounts",
  "columns": [
    { "name": "id", "dataType": "uuid", "redacted": false },
    { "name": "name", "dataType": "text", "redacted": false }
  ],
  "rows": [["0f2c…", "Joint current"], ["7a11…", "«null»"]],
  "total": 14,
  "limit": 50,
  "offset": 0
}
```

Every 2xx carries a body, per `CLAUDE.md`. `rows` is an array of arrays in
column order rather than an array of objects: a Postgres table may have two
columns whose names differ only in ways JSON keys do not preserve, and the
column list is already being sent.

## 7. Configuration

`DATABASE_READONLY_URL`, read in `internal/config/config.go` beside
`DATABASE_URL` and `MAILPIT_API_URL`.

- Empty means the feature is off and `Deps.AdminBrowse` is nil (decision 13).
- Set but unparseable **refuses the boot**, as `config.go` already refuses a
  half-set Telegram pair.
- Set and parseable but pointing at a connection that can write **refuses the
  boot** (decision 4).

`docker-compose.yml`'s `api` service gets the dev value written in, beside
`MAILPIT_API_URL` and for the same reason recorded in that file's comment: a
developer must not have to configure the feature to discover it exists.

`deploy/docker-compose.prod.yml`'s `api` already declares `env_file: [.env]`,
so **no compose change is needed in production** — only the variable in the
box's `.env`, which is added when the operator chooses to provision the role.

`docs/INFRASTRUCTURE.md` already names `DATABASE_READONLY_URL` as the
variable this unbuilt feature will need. This work makes that sentence true.

## 8. Frontend

`/admin/database`, a fourth link in `AdminShell`'s `OperatorNav`, computing
its own active state with `useMatchRoute` — never `activeProps`, for the
reason `docs/LEARNING.md`'s Frontend section records.

The page follows `AdminHouseholdsPage`: one query for the table list, a
second for the selected table's rows, `useAdminDatabase` beside
`useAdminOutbox`, Zod schemas beside `adminOutboxSchemas.ts`, and the
not-configured copy naming the variable rather than saying "unavailable".

Layout: table list on the left (name and row count), rows on the right, with
`Previous`/`Next` driving `offset`. **The two panes stack below the `md`
breakpoint** — a two-column layout cannot hold at 305px, and the walk checks
that width, so saying it here is cheaper than discovering it at criterion 15. The rows table scrolls inside its own
`overflow-x: auto` container — the body must never scroll sideways, per the
mobile-responsive work. A one-line legend explains `«redacted»` and `«null»`;
a redacted column's header is marked so the two facts are visible together.

**The fourth nav link is a known risk.** Adding the third pushed the operator
header 14px past a 305px viewport, fixed with `flex-wrap` on `AdminShell`.
The wrap should absorb a fourth, but the browser walk re-checks all four
widths rather than assuming it.

## 9. Testing

| Level | What it covers |
|---|---|
| `domain/dbbrowse_test.go` | The redaction predicate as a table test: `bytea`, `_hash`, `_secret`, `password` anywhere in the name, case, and a plain column that must **not** be redacted |
| `usecase/admin_browse_test.go` | Service against an in-memory `DatabaseBrowser` double: limit clamped at both ends and defaulted, negative offset refused, not-found passed through |
| `adapter/postgres/browse_repo_test.go` | Against a real container: an unknown table is `domain.ErrNotFound`; a table name carrying a quote or a semicolon is `ErrNotFound` and not an error from Postgres; paging is stable across two pages; `NULL` renders `«null»`; a `bytea` column renders `«redacted»` and its bytes appear nowhere in the result |
| `adapter/postgres/readonly_pool_test.go` | The self-check: a pool opened as `hearth` fails it, a pool opened as `hearth_readonly` passes; an `INSERT` through the read-only pool is refused by Postgres |
| `adapter/http/admin_browse_api_test.go` | Every row of decision 12's table; both routes answer `404 NOT_FOUND` to a non-admin and `401 ADMIN_REAUTH_REQUIRED` to an admin whose grant has expired; the audit row for a row read carries the table name and the offset. **No CSRF test** — both routes are `GET`, which `requireCSRF` exempts by design, and a test asserting otherwise would be green for the wrong reason |
| `web/.../AdminDatabasePage.test.tsx` | The not-configured copy, the redacted legend, paging controls, and that a redacted cell renders the marker rather than an empty cell |

At least one test per task is mutation-checked, per `CLAUDE.md`.

**The test that must exist and would be easy to omit**: a schema-driven
redaction test. It reads every column of every table from a migrated
container's `information_schema` and asserts that each one whose type is
`bytea`, or whose name matches rule 2, is reported `Redacted: true` by
`Tables()`. It is written against the live schema rather than a fixture list,
so migration `00014` adding `webhook_secret bytea` is covered by a test
written today. Decision 8 is the kind of rule that survives review and dies
in a later refactor unless a test holds it against the real schema.

Its sibling, equally easy to omit: an assertion that **no test in the suite
opens the browse against `DATABASE_URL`**. The `postgres` package tests must
build the read-only pool from the read-only role, or they prove nothing about
the guard that matters.

## 10. Documentation this work updates

Part of the work, not a tidy-up after it:

- `docs/SYSTEM_DESIGN.md` — a new port, a new adapter, a second connection
  pool, two routes and a screen. Use the `maintaining-system-design` skill;
  the second pool is a change to the component diagram, not only to a flow.
- `docs/FEATURE_TRACKER.md` — section 9's "Read-only database browse" row
  ⬜ → ✅ or 🟡 with the gap named, and the summary table recounted from the
  symbols rather than adjusted.
- `docs/LEARNING.md` — whatever the build and the walk teach. The
  `ALTER DEFAULT PRIVILEGES` trap belongs there whether or not anyone trips
  it, because the next person to grant a read-only role will write the
  incomplete version.
- **`docs/adr/0005-platform-admin-authorization.md` — amended, not
  superseded.** Its own "Revisit this when" names this feature as the moment
  the "mutations stay on the CLI, reads move to the web" narrowing is first
  tested against a real database read. It must record what held and what did
  not.
- `docs/INFRASTRUCTURE.md` — `DATABASE_READONLY_URL` and the role become
  real, with what breaks without them.
- `deploy/PROVISION.md` — the role step, on a box where `POSTGRES_USER` is
  `hearth`, so it is one `psql` invocation and one `.env` line.
- **`deploy/README.md`'s "Restoring", and `docs/INFRASTRUCTURE.md`'s "what
  breaks without it" — roles are cluster-level and `backup.sh` dumps one
  database with `--no-privileges`, so neither the role nor its grants are in
  any backup this product takes.** After restoring into a database, the role
  script is run again against it. It is idempotent, so "run it after every
  restore" is the whole instruction, and decision 2's default privileges make
  it right even when the restore recreates every table.
- `deploy/README.md` — "Break-glass" gains the browse. Every command in
  that section today is a mutation; reading a row on the box is not covered
  there at all, which is the gap this feature closes.
- `docs/ADMIN_SURFACE_HANDOVER.md` — §2 gains the feature, §3 loses it and
  becomes empty. The operator surface is then complete.

## 11. Rollout

**This ships dark.** The product owner's decision on 2026-09-04: build,
merge and deploy with `DATABASE_READONLY_URL` unset, so production shows the
not-configured panel until they run the two provisioning steps themselves.
Nothing in this work touches the live box, its database roles or its `.env`.

That is also why decision 12's not-configured copy names the variable: for
some period, the deployed product's own admin surface is the first reader of
that message, and it must be an instruction rather than an apology.

One branch, `admin-db-browse`, in dependency order: the role script, domain,
port, service, read-only pool, adapter, config, routes, frontend, docs, then
the browser walk. The plan turns this into tasks; it is not written here.

**Definition of done is `CLAUDE.md`'s**: `make lint && make test` green, at
least one mutation-checked test per task, tracker and learning log updated,
and — the bar the product owner set explicitly — **a fifteen-criterion
browser walk against the running stack**, recorded criterion by criterion,
before anyone calls it done.

Three criteria that walk badly from a script and must be written carefully:

- **The not-configured state** needs the API restarted with the variable
  unset. It is the state production will actually be in.
- **The write refusal** is not visible in the UI at all. It is walked at the
  database: `psql` as `hearth_readonly`, attempt an `INSERT`, record the
  refusal. A criterion that cannot be clicked is still a criterion.
- **Redaction** is walked against `sessions` or `magic_links`, whose
  `token_hash` must render `«redacted»` on screen while the network response
  in the browser's own devtools contains no hex.

## 12. Rejected options

| Option | Why not |
|---|---|
| Reuse the application pool with `SET TRANSACTION READ ONLY` | The guard becomes application logic. Decision 1. |
| A SQL text box | Arbitrary reads of every household with one guard left. Decision 16. |
| `docker-entrypoint-initdb.d` for the dev role | Runs only on a fresh volume; every existing machine would silently lack the role. Decision 5. |
| A migration that creates the role | Migrations do not run as a superuser, and a role is not schema. Parent spec §4.3. |
| `pg_class.reltuples` for row counts | An "approx" label on a screen whose job is to show what is there. Decision 11. |
| Filtering redacted columns in Go | The bytes still cross the wire and enter this process. Decision 7. |
| An `/admin/audit` screen instead | Built and descoped on 2026-09-02. This browse reads that table as one of fifteen. §1. |
| A feature flag | A second switch that can disagree with the credential. Decision 15. |
| Hiding `admin_audit_log` or `goose_db_version` from the list | An operator surface that lies about what is in the database. §1. |

## 13. Differences from admin-surface spec §4

| §4 says | This says | Why |
|---|---|---|
| `SET LOCAL statement_timeout = '3s'` on the browse connection | The timeout is a role setting **and** a pool runtime parameter | `SET LOCAL` is a no-op outside an explicit transaction, so an adapter bug silently removes the guard. Decision 3 |
| `GRANT SELECT` on `public` | Also `ALTER DEFAULT PRIVILEGES FOR ROLE hearth` | Without it, every table created after provisioning is invisible to the browse and nothing reports it. Decision 2 |
| Silent on proving the role is read-only | The API refuses to boot if the read-only pool can write | The variable will one day be filled with the read-write URL. Decision 4 |
| Silent on how the role is created outside production | One idempotent SQL file with three consumers, including `testsupport` | Otherwise the tests prove nothing about the role, and dev has no way to run the feature. Decision 5 |
| Redaction described as columns that "render `«redacted»`" | Redaction is emitted as a literal in the `SELECT` list | The bytes never leave Postgres, so they cannot leak through a log or a dump. Decision 7 |
| Silent on ordering | `ORDER BY` primary key, `ctid` when absent | `OFFSET` without `ORDER BY` silently repeats and skips rows. Decision 10 |
| Silent on `NULL` | `«null»`, distinct from the empty string | `[][]string` merges two different facts. Decision 9 |
| "Unavailable" as one state | Three: `503` unconfigured, refuse-to-boot misconfigured, `503` unavailable | Matches the shape the mail inspector defined and §5.2 predicted the browse would copy. Decision 12 |
| Both routes "audited with the table name and offset" | No audit change; the existing middleware already records both | The path and query string are what `auditAdmin` writes, before routing. Decision 17 |
| Silent on pool size | Its own constructor, `MaxConns = 3` | `Open`'s 10 is sized for request traffic, not for one operator. Decision 14 |
