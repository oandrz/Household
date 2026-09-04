# Read-only database browse — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the platform operator a screen that lists the database's tables
and pages through one table's rows, read through a Postgres role that cannot
write, with every page view audited — so the ordinary "why does this
household's figure look wrong" question stops needing `psql` as the
application's read-write user over SSH.

**Architecture:** A `DatabaseBrowser` port whose every value is already a
string, so no driver type crosses out of the adapter. One adapter backed by a
**second** `pgxpool` built from `DATABASE_READONLY_URL`, which refuses to
serve through a connection that can write. Redaction as a pure `domain`
predicate, applied by emitting a literal into the `SELECT` list so secret
bytes never leave Postgres. A thin `AdminBrowseService` that clamps paging and
nothing else. Two read routes inside the existing `/admin` granted group, so
all four admin guards apply by construction. One lazily-loaded React page
following `AdminMailPage`. The `hearth_readonly` role itself is provisioning,
not a migration: one idempotent SQL file with three consumers.

**Tech Stack:** Go as pinned in `api/go.mod`, chi, pgx v5 (`pgxpool`,
`pgx.Identifier`), Postgres 17, React 19, TanStack Query and Router, Zod,
Vitest. No new Go or npm dependency: `pgx` and `pgxpool` are already direct
dependencies, and everything this feature needs is in them.

**Spec:** `docs/superpowers/specs/2026-09-04-hearth-database-browse-design.md`
— read it before Task 1. This plan argues from it and does not repeat its
reasoning. Where a step says "decision N", that is a section of the spec.

**Branch:** `admin-db-browse`, already created, already carrying the spec.

## Global Constraints

Copied from `CLAUDE.md` and the spec. Every task's requirements implicitly
include this section.

- **Clean architecture, enforced by `make lint-arch` including test files.**
  `internal/domain` imports the standard library only. `internal/usecase` may
  add `internal/domain`. Everything else lives under `internal/adapter/**` or
  `cmd/**`. Adapters *may* import `usecase` — that is how they implement its
  ports.
- **No database type crosses out of the adapter layer.** No `pgx` or `pgxpool`
  type appears outside `internal/adapter/postgres`. A missing row becomes
  `domain.ErrNotFound` at that boundary, never `pgx.ErrNoRows` further up.
- **Authorisation exists only in the HTTP layer.** No service takes an actor
  parameter. Both new routes go inside the existing `granted` group; neither
  handler checks who is asking.
- **Every 2xx except 204 carries a JSON body.** `apiFetch` throws on an ok
  response it cannot parse.
- **Fail closed on values you did not construct.** The table name is matched
  against `information_schema` before it is quoted; `limit` and `offset` are
  validated before any query runs; a column whose type or name is unknown to
  the redaction rules is still checked against all three of them.
- **No `float64` in a monetary path.** Money columns are rendered by Postgres
  as text (`::text`) and never parsed in Go — this feature reads money without
  ever holding a monetary value in a Go type.
- **Comments say *why*.** Names say what. Exported things carry their contract
  in a doc comment; `usecase/ports.go` is the model.
- **Definition of done:** `make lint && make test` green, at least one
  mutation-checked test per task, `docs/FEATURE_TRACKER.md` and
  `docs/LEARNING.md` updated, and the browser walk in the final task run and
  recorded.

**Running the suites** (`go` is not on `PATH` in a bare shell on this machine,
and the Go suite needs a Docker socket):

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock

cd api && go test ./... -count=1 -timeout=10m     # or: make test-api
cd web && npx vitest run                          # or: make test-web
make lint                                         # arch lint, tsc, eslint, go vet
```

**A colima or Docker Desktop engine must be running** before any Go test in
this plan. `docker context ls` shows which sockets exist; if neither answers,
start one before Task 1. Note that **two engines exist on this machine** and
both can host the stack — check `lsof -i :5173` before trusting what
`http://localhost:5173` is serving. If `go build` refuses on a language
version, run `go version` first: `api/go.mod` names a newer Go than the
toolchain path above, so a second install may be the one in use.

---

## File structure

**Created**

| File | Responsibility |
|---|---|
| `deploy/readonly-role.sql` | Creates `hearth_readonly` and grants it `SELECT`. Idempotent; no password; the one definition, run by three consumers |
| `api/internal/domain/dbbrowse.go` | `ColumnIsRedacted` and the two render markers. Pure, stdlib only, no SQL |
| `api/internal/domain/dbbrowse_test.go` | Its table test |
| `api/internal/adapter/postgres/readonly_pool.go` | `ReadOnlyDB` and `OpenReadOnly` — the second pool, its runtime parameters, and the write-privilege self-check that runs on every connection |
| `api/internal/adapter/postgres/readonly_pool_test.go` | That the self-check refuses a writable role and accepts `hearth_readonly`, and that Postgres itself refuses a write through the pool |
| `api/internal/adapter/postgres/browse_repo.go` | `BrowseRepo` — the `DatabaseBrowser` implementation. The first hand-written pgx file in this package, and says why |
| `api/internal/adapter/postgres/browse_repo_test.go` | Validation, redaction, `NULL` rendering, stable paging, unknown table |
| `api/internal/usecase/admin_browse.go` | `AdminBrowseService` — defaults and clamps paging, nothing else |
| `api/internal/usecase/admin_browse_test.go` | Its clamps and its pass-through of `domain.ErrNotFound` |
| `api/internal/adapter/http/admin_browse_handlers.go` | Two handlers, their DTOs, and the not-configured answer |
| `api/internal/adapter/http/admin_browse_api_test.go` | Every row of the spec's decision 12 table, plus the audit row's contents |
| `web/src/features/admin/adminDatabaseSchemas.ts` | Zod mirrors of the two DTOs |
| `web/src/features/admin/useAdminDatabase.ts` | The two query hooks, their path builders and their keys |
| `web/src/features/admin/AdminDatabasePage.tsx` | Both screens: the table list and the row viewer |
| `web/src/features/admin/AdminDatabasePage.test.tsx` | Not-configured copy, the redaction legend, paging, and the unknown-table answer |

**Modified**

| File | Change |
|---|---|
| `api/internal/testsupport/postgres.go` | Run the role script after the migrations; expose the read-only DSN |
| `api/internal/usecase/ports.go` | The `DatabaseBrowser` port, its three value types, and `ErrBrowseUnavailable` |
| `api/internal/config/config.go` | `DatabaseReadonlyURL` and `BrowseEnabled()` |
| `api/internal/config/config_test.go` | Absent is accepted; present is accepted |
| `api/internal/adapter/http/router.go` | `Deps.AdminBrowse`, and the two routes in the `granted` group |
| `api/internal/adapter/http/errors.go` | One `ErrBrowseUnavailable` branch in `MapDomainError` |
| `api/internal/adapter/http/admin_api_test.go` | The two new paths added to both route lists |
| `api/cmd/api/main.go` | Open the read-only pool, build the service, wire the field, log that it is on |
| `docker-compose.yml` | The `readonly-role` one-shot, `api`'s dependency on it, `DATABASE_READONLY_URL` |
| `Makefile` | `make migrate` and `make dev-local` run the role service too |
| `web/src/features/admin/AdminShell.tsx` | A fourth nav link |
| `web/src/features/admin/AdminShell.test.tsx` | Its label array, which this change breaks |
| `web/src/features/admin/adminBundleSplit.test.ts` | The two new admin files added to the "not statically reachable" assertions |
| `web/src/routes/router.tsx` | Two lazy imports, two routes, the route-shape header comment |
| `deploy/.env.example` | `DATABASE_READONLY_URL`, commented |
| `deploy/PROVISION.md` | A new numbered section for the role |
| `deploy/README.md` | Break-glass gains the browse; Restoring gains the re-run |
| `docs/INFRASTRUCTURE.md` | The gap bullet becomes a real entry; the credentials table gains a row |
| `docs/SYSTEM_DESIGN.md` | Second pool, new port, new adapter, two routes, one screen |
| `docs/FEATURE_TRACKER.md` | The row, and the recount |
| `docs/LEARNING.md` | What this taught |
| `docs/adr/0005-platform-admin-authorization.md` | Amended, not superseded |
| `docs/ADMIN_SURFACE_HANDOVER.md` | §2 gains it, §3 empties |

**Deliberately not modified**

`deploy/docker-compose.prod.yml`. Production's `api` already reads
`env_file: [.env]`, so the variable is an `.env` line and nothing else. A
one-shot added there would also break `deploy.sh`, which verifies the
migration by grepping `ps -a` for a service named `migrate` and would ignore
a second one-shot's non-zero exit entirely.

---

## Task order and why

1. **The role** — everything else needs a read-only connection to exist.
2. **`domain.ColumnIsRedacted`** — pure, no dependencies, and the adapter's
   SQL is built from it.
3. **The port and the service** — the contract the adapter implements.
4. **The read-only pool** — the guard that actually holds.
5. **The adapter** — the SQL.
6. **Config** — the switch.
7. **The routes** — the feature becomes reachable.
8. **Frontend schemas and hooks.**
9. **The page and the nav.**
10. **The documents.**
11. **The browser walk.**

---

### Task 1: `hearth_readonly` — one SQL file, three consumers

The role has to exist before anything else can be written against it. This
task creates it, makes the dev stack and the Go suite run the same file, and
proves with a test that the role Postgres ends up with genuinely cannot write.

**Files:**
- Create: `deploy/readonly-role.sql`
- Create: `api/internal/adapter/postgres/readonly_role_test.go`
- Modify: `api/internal/testsupport/postgres.go`, `docker-compose.yml`, `Makefile`

**Interfaces:**
- Produces: `testsupport.ReadOnlyURL(t *testing.T, adminURL string) string` —
  the same container's DSN with `hearth_readonly` credentials. Tasks 4 and 5
  use it. `testsupport.StartPostgres` keeps its existing signature and now
  also creates the role.

- [ ] **Step 1: Write the SQL file**

Create `deploy/readonly-role.sql`:

```sql
-- Creates the SELECT-only role the admin database browse reads through.
--
-- Idempotent: the dev Compose one-shot runs it on every `make dev` and the Go
-- suite runs it per test container. Postgres has no CREATE ROLE IF NOT
-- EXISTS, hence the DO block; the GRANTs are naturally idempotent.
--
-- Run as a role that may create roles and that owns the tables (`hearth` in
-- every environment today).
--
-- It sets no password, deliberately: psql's :'var' interpolation is a
-- client-side feature, and a file using it is a syntax error when the Go
-- suite sends it to the server through pgx. Every consumer sets the password
-- itself in one statement afterwards -- see the spec's decision 5.

DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'hearth_readonly') THEN
        CREATE ROLE hearth_readonly LOGIN;
    END IF;
END
$$;

-- Read-only twice over: once by what is granted, once by what the role's own
-- session default allows. The two fail independently, and the second survives
-- an adapter that forgets to open a transaction -- SET LOCAL would not. See
-- the spec's decision 3.
ALTER ROLE hearth_readonly SET default_transaction_read_only = on;
ALTER ROLE hearth_readonly SET statement_timeout = '3s';

GRANT CONNECT ON DATABASE hearth TO hearth_readonly;
GRANT USAGE ON SCHEMA public TO hearth_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO hearth_readonly;

-- GRANT ... ON ALL TABLES covers only the tables that exist at the moment it
-- runs. Without this line, the table migration 00014 creates is invisible to
-- the browse -- information_schema lists only what the current role may see --
-- and nothing anywhere reports it. FOR ROLE hearth is load-bearing: default
-- privileges attach to the role that creates the object, and migrations run
-- as hearth. See the spec's decision 2.
ALTER DEFAULT PRIVILEGES FOR ROLE hearth IN SCHEMA public
    GRANT SELECT ON TABLES TO hearth_readonly;
```

Nothing else is granted: no `USAGE` on sequences, no `EXECUTE` on functions,
no `CREATE` anywhere.

- [ ] **Step 2: Write the failing test**

Create `api/internal/adapter/postgres/readonly_role_test.go`:

```go
package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
)

// The role is the guard that actually holds: a mistake in the adapter's SQL
// still cannot write, because Postgres refuses on the other side of the wire.
// Everything else this feature does is defence in depth on top of this, so
// this is the test that must never be allowed to go green for the wrong
// reason -- see the spec's decision 1.
func TestReadOnlyRoleCanReadAndCannotWrite(t *testing.T) {
	adminURL := testsupport.StartPostgres(t)
	ctx := context.Background()

	db, err := postgres.Open(ctx, testsupport.ReadOnlyURL(t, adminURL))
	if err != nil {
		t.Fatalf("Open as hearth_readonly: %v", err)
	}
	t.Cleanup(db.Close)

	t.Run("reads", func(t *testing.T) {
		var count int
		if err := db.Pool().QueryRow(ctx,
			`SELECT count(*) FROM information_schema.tables WHERE table_name = 'households'`,
		).Scan(&count); err != nil {
			t.Fatalf("select: %v", err)
		}
		if count != 1 {
			t.Fatalf("households not visible to hearth_readonly; the GRANT did not run")
		}
	})

	t.Run("cannot insert", func(t *testing.T) {
		_, err := db.Pool().Exec(ctx,
			`INSERT INTO households (id, name) VALUES (gen_random_uuid(), 'nope')`)
		if err == nil {
			t.Fatal("INSERT succeeded as hearth_readonly")
		}
		// Assert on WHY it failed. A NOT NULL violation on some column this
		// INSERT forgot would also be a non-nil error, and this subtest would
		// then pass whether or not the role can write -- which would also
		// make Step 6's mutation check meaningless.
		message := err.Error()
		if !strings.Contains(message, "permission denied") &&
			!strings.Contains(message, "read-only transaction") {
			t.Fatalf("INSERT failed for the wrong reason: %v", err)
		}
	})

	t.Run("cannot create a table", func(t *testing.T) {
		if _, err := db.Pool().Exec(ctx, `CREATE TABLE nope (id int)`); err == nil {
			t.Fatal("CREATE TABLE succeeded as hearth_readonly")
		}
	})

	// The role's own session default, independent of the GRANTs above. A
	// GRANT that was accidentally widened would still be caught here.
	t.Run("its session is read-only by default", func(t *testing.T) {
		var setting string
		if err := db.Pool().QueryRow(ctx, `SHOW default_transaction_read_only`).Scan(&setting); err != nil {
			t.Fatalf("show: %v", err)
		}
		if setting != "on" {
			t.Fatalf("default_transaction_read_only = %q, want \"on\"", setting)
		}
	})

	// Named so a failure says which of the two mechanisms is missing rather
	// than only that a write went through.
	t.Run("its statement timeout is set", func(t *testing.T) {
		var setting string
		if err := db.Pool().QueryRow(ctx, `SHOW statement_timeout`).Scan(&setting); err != nil {
			t.Fatalf("show: %v", err)
		}
		if !strings.HasPrefix(setting, "3") {
			t.Fatalf("statement_timeout = %q, want 3 seconds", setting)
		}
	})
}
```

If the `households` table has required columns beyond `id` and `name`, the
`INSERT` above will fail for the wrong reason and still pass. Read
`api/migrations/00001_init.sql` first and write an `INSERT` whose *only*
problem is the privilege — or assert on the error text containing
`permission denied`, which is what actually proves it.

- [ ] **Step 3: Run it and watch it fail**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestReadOnlyRole -v
```

Expected: FAIL, `undefined: testsupport.ReadOnlyURL`.

- [ ] **Step 4: Teach `testsupport` to run the file**

In `api/internal/testsupport/postgres.go`, call the role script after
`applyMigrations(t, url)` in `StartPostgres`, and add the two helpers below.

The script is executed with `pgx.Connect` and `conn.Exec`, **not** through
`database/sql`: a pgx `Exec` with no arguments is sent over the simple
protocol, which is what allows one call to carry several statements and a
`DO $$ ... $$` block. The `database/sql` path prepares a statement and would
refuse both.

```go
// readOnlyPassword is the role's password in tests only. It is set here
// rather than in deploy/readonly-role.sql because psql's :'var' syntax is
// client-side and unusable from pgx -- the spec's decision 5.
const readOnlyPassword = "hearth-readonly"

// createReadOnlyRole runs deploy/readonly-role.sql against the freshly
// migrated database, so every test in this repository sees the same role
// production will have. A test that opened the browse against DATABASE_URL
// would prove nothing about the guard that matters.
func createReadOnlyRole(t *testing.T, url string) {
	t.Helper()

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect to create the read-only role: %v", err)
	}
	defer conn.Close(ctx)

	script, err := os.ReadFile(readOnlyRoleScript(t))
	if err != nil {
		t.Fatalf("read deploy/readonly-role.sql: %v", err)
	}
	// No arguments, so pgx uses the simple protocol and the whole file --
	// DO block included -- goes in one round trip.
	if _, err := conn.Exec(ctx, string(script)); err != nil {
		t.Fatalf("run deploy/readonly-role.sql: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`ALTER ROLE hearth_readonly PASSWORD '`+readOnlyPassword+`'`); err != nil {
		t.Fatalf("set the read-only role's password: %v", err)
	}
}

// readOnlyRoleScript resolves deploy/readonly-role.sql from this file's own
// location, the same way migrationsDir does, so tests pass regardless of the
// working directory they run from.
func readOnlyRoleScript(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine the caller path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "deploy", "readonly-role.sql")
}

// ReadOnlyURL is the same database as adminURL, reached as hearth_readonly.
// Use it for anything that exercises the database browse: opening the browse
// against the read-write URL would test the SQL and none of the guard.
func ReadOnlyURL(t *testing.T, adminURL string) string {
	t.Helper()
	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse the container url: %v", err)
	}
	parsed.User = url.UserPassword("hearth_readonly", readOnlyPassword)
	return parsed.String()
}
```

Add `"net/url"`, `"os"` and `"github.com/jackc/pgx/v5"` to the imports;
`runtime` and `path/filepath` are already there. Call `createReadOnlyRole(t,
url)` immediately after `applyMigrations(t, url)`.

**If that `Exec` fails with `syntax error at or near "$1"`** or with
`cannot insert multiple commands into a prepared statement`, pgx has taken
the extended-protocol path and is reading the `DO $$` block's dollars as
placeholders. Drop to the connection underneath, which is unambiguously the
simple protocol:

```go
	if err := conn.PgConn().Exec(ctx, string(script)).Close(); err != nil {
```

This is the one failure in this task a reader cannot debug from the error
message alone, which is why it is written down rather than left to be
discovered.

- [ ] **Step 5: Run it and watch it pass**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestReadOnlyRole -v
```

Expected: PASS, all five subtests.

- [ ] **Step 6: Mutation-check the grant**

Delete the `GRANT SELECT ON ALL TABLES` line from `deploy/readonly-role.sql`.
Re-run. Expected: **FAIL** on the `reads` subtest — `households` is no longer
visible. Restore and re-run to green.

Then delete `ALTER ROLE hearth_readonly SET default_transaction_read_only =
on` and re-run. Expected: **FAIL** on `its session is read-only by default`,
and **not** on `cannot insert` — the `GRANT` alone still refuses the write.
That asymmetry is the point of having both; if `cannot insert` also fails,
the `GRANT` is wrong. Restore.

- [ ] **Step 7: Add the dev one-shot**

In `docker-compose.yml`, add the service after `migrate`:

```yaml
  # The read-only role the admin database browse reads through. A one-shot,
  # like migrate, because a role is provisioning rather than schema:
  # migrations do not run as a superuser and creating a role is not a change
  # to the shape of the data.
  #
  # postgres:17-alpine rather than the api image: this needs psql, and the api
  # dev image carries goose. Not docker-entrypoint-initdb.d either -- that
  # runs only when the data directory is created, and hearth-pgdata already
  # exists on every machine that has run this stack, so a developer would get
  # a not-configured panel whose only fix is destroying their database.
  readonly-role:
    image: postgres:17-alpine
    environment:
      PGHOST: postgres
      PGUSER: hearth
      PGPASSWORD: hearth
      PGDATABASE: hearth
      # The dev password, written here rather than in readonly-role.sql: that
      # file is also executed by the Go suite through pgx, and psql's :'var'
      # interpolation is client-side syntax pgx would reject.
      READONLY_PASSWORD: hearth-readonly
    entrypoint: ["/bin/sh", "-c"]
    command:
      - |
        set -e
        psql -v ON_ERROR_STOP=1 -f /sql/readonly-role.sql
        psql -v ON_ERROR_STOP=1 -c "ALTER ROLE hearth_readonly PASSWORD '$$READONLY_PASSWORD'"
    volumes: ["./deploy/readonly-role.sql:/sql/readonly-role.sql:ro"]
    depends_on:
      postgres: { condition: service_healthy }
      migrate: { condition: service_completed_successfully }
    restart: "no"
```

`entrypoint` as a list plus `command` as one literal block is what keeps the
shell quoting readable: Compose passes the block verbatim as the single
argument to `sh -c`, so nothing here depends on how Compose would have split
a one-line string. `$$` is Compose's escape for a literal `$`.

Then add the dependency and the variable to `api`:

```yaml
    depends_on:
      postgres: { condition: service_healthy }
      migrate: { condition: service_completed_successfully }
      readonly-role: { condition: service_completed_successfully }
```

```yaml
      # The database browse's own connection: hearth_readonly, created by the
      # readonly-role one-shot above. Hardcoded here for the same reason
      # MAILPIT_API_URL is -- a developer must not have to configure the
      # feature to discover it exists. Unset means the panel says it is not
      # configured, and there is no fallback to DATABASE_URL.
      DATABASE_READONLY_URL: postgres://hearth_readonly:hearth-readonly@postgres:5432/hearth?sslmode=disable
```

- [ ] **Step 8: Close the two Make targets that skip it**

`make dev` reaches the one-shot through `api`'s `depends_on`. `make migrate`
and `make dev-local` do not: `migrate` is `run --rm migrate`, which starts
that service and its own dependencies but no siblings, and `dev-local` runs
the API natively from `.env` so `api`'s `depends_on` is never evaluated at
all. Both would leave a developer with `DATABASE_READONLY_URL` pointing at a
role that does not exist — and the boot self-check in Task 4 would then refuse
to start the API, with the cause three files away.

In the `Makefile`:

```make
migrate: ## Apply pending migrations
	$(COMPOSE) run --rm migrate
	$(COMPOSE) run --rm readonly-role
```

`dev-local` already calls `$(MAKE) migrate`, so it is fixed by the same line.
No `.PHONY` change is needed: no new target is added.

- [ ] **Step 9: Prove it in the running stack**

```bash
make down && make dev
```

In another shell:

```bash
docker compose exec postgres psql -U hearth -d hearth -c "\du hearth_readonly"
docker compose exec postgres psql -U hearth_readonly -d hearth \
  -c "insert into households (id, name) values (gen_random_uuid(), 'nope')"
```

Expected: the role exists, and the `INSERT` is refused with `permission
denied` (or `cannot execute INSERT in a read-only transaction`). Either
message proves the guard; record which one you saw, because the two come from
the two different mechanisms.

- [ ] **Step 10: Commit**

```bash
git add deploy/readonly-role.sql api/internal/testsupport/postgres.go \
        api/internal/adapter/postgres/readonly_role_test.go docker-compose.yml Makefile
git commit -m "feat(db): hearth_readonly, one idempotent script with three consumers"
```

---

### Task 2: `domain.ColumnIsRedacted`

**Files:**
- Create: `api/internal/domain/dbbrowse.go`
- Test: `api/internal/domain/dbbrowse_test.go`

**Interfaces:**
- Produces: `domain.ColumnIsRedacted(name, dataType string) bool`,
  `domain.RedactedCell` and `domain.NullCell` (both `string` constants). Task 5
  builds its `SELECT` list from all three.

- [ ] **Step 1: Write the failing test**

Create `api/internal/domain/dbbrowse_test.go`:

```go
package domain_test

import (
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// The type rule is the one that survives: every token in this schema is
// stored as bytea, so a secret column added by a migration in 2027 is
// redacted before its author has heard of this file. The name rules exist
// because the type rule is not complete -- users.password_hash is text.
func TestColumnIsRedacted(t *testing.T) {
	cases := []struct {
		name     string
		column   string
		dataType string
		want     bool
	}{
		{"a session token", "token_hash", "bytea", true},
		{"a magic link token", "token_hash", "bytea", true},
		{"a telegram nonce", "nonce_hash", "bytea", true},
		{"any bytea at all, whatever it is called", "avatar", "bytea", true},
		{"the one credential that is not bytea", "password_hash", "text", true},
		{"a column merely mentioning a password", "password_set_at", "timestamptz", true},
		{"an api secret", "webhook_secret", "text", true},
		{"upper case is still a hash", "TOKEN_HASH", "BYTEA", true},
		{"an ordinary column", "display_name", "text", false},
		{"an ordinary amount", "amount_minor", "bigint", false},
		{"a column whose name merely ends in hash-like text", "hashtag", "text", false},
		{"an id", "household_id", "uuid", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := domain.ColumnIsRedacted(c.column, c.dataType); got != c.want {
				t.Fatalf("ColumnIsRedacted(%q, %q) = %v, want %v", c.column, c.dataType, got, c.want)
			}
		})
	}
}

// The markers must be distinguishable from each other and from an empty
// string, because [][]string cannot carry the difference any other way.
func TestTheTwoMarkersAreDistinct(t *testing.T) {
	if domain.RedactedCell == domain.NullCell {
		t.Fatal("the redacted and null markers are the same string")
	}
	if domain.RedactedCell == "" || domain.NullCell == "" {
		t.Fatal("a marker is empty, which is exactly what it exists to distinguish from")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/domain/ -run 'TestColumnIsRedacted|TestTheTwoMarkers' -v
```

Expected: FAIL, `undefined: domain.ColumnIsRedacted`.

- [ ] **Step 3: Write the implementation**

Create `api/internal/domain/dbbrowse.go`:

```go
package domain

import "strings"

// The two markers a browsed cell carries instead of a value.
//
// Guillemets, and not a bare word, so that a real value reading "redacted"
// cannot be mistaken for a withheld one -- and so the screen has something
// specific to explain in its legend. NullCell exists because RowPage carries
// [][]string: without it, a SQL NULL and an empty text column would both
// arrive as "" and a reader would conclude they are the same thing. In this
// schema they are not, and the difference is sometimes the bug being
// investigated (users.password_hash is NULL for a member who has only ever
// signed in with a magic link).
const (
	RedactedCell = "«redacted»"
	NullCell     = "«null»"
)

// redactedColumns is the explicit denylist: columns that are secret in a way
// neither their type nor their name reveals.
//
// It is empty on purpose. A denylist pre-filled with guesses becomes the
// thing people trust, and then the type rule below -- the only one that
// covers a column nobody has thought of yet -- stops being maintained. Every
// entry added here must carry a comment saying why the first two rules
// missed it.
var redactedColumns = map[string]bool{}

// ColumnIsRedacted reports whether a column's values must never be rendered.
// name and dataType come from information_schema, so both are compared
// case-insensitively: nothing here may depend on how a migration happened to
// spell them.
//
// The three rules are deliberately ordered by how much they can be relied on.
// The type rule is first because it is the one that survives a schema this
// file has never seen: every token in Hearth is stored as bytea. The name
// rules exist because that is not complete -- an Argon2 hash is a
// self-describing string, so users.password_hash is text. The denylist is the
// escape hatch for anything the first two would miss.
func ColumnIsRedacted(name, dataType string) bool {
	lowerName := strings.ToLower(name)
	lowerType := strings.ToLower(dataType)

	switch {
	case lowerType == "bytea":
		return true
	case strings.HasSuffix(lowerName, "_hash"), strings.HasSuffix(lowerName, "_secret"):
		return true
	case strings.Contains(lowerName, "password"):
		return true
	default:
		return redactedColumns[lowerName]
	}
}
```

- [ ] **Step 4: Run it and watch it pass**

```bash
cd api && go test ./internal/domain/ -run 'TestColumnIsRedacted|TestTheTwoMarkers' -v
```

Expected: PASS, all twelve subtests.

- [ ] **Step 5: Mutation-check the type rule**

Delete the `lowerType == "bytea"` case. Re-run. Expected: **FAIL** on
`any bytea at all, whatever it is called` — and *not* on the two
`token_hash` cases, which the name rule still catches. That is the check
worth watching: if those also stay green, the type rule was never doing
anything the name rule wasn't, and the test table needs a case that isolates
it. Restore and re-run to green.

- [ ] **Step 6: Commit**

```bash
git add api/internal/domain/dbbrowse.go api/internal/domain/dbbrowse_test.go
git commit -m "feat(domain): ColumnIsRedacted, type first and denylist last"
```

---

### Task 3: The `DatabaseBrowser` port and `AdminBrowseService`

**Files:**
- Create: `api/internal/usecase/admin_browse.go`
- Test: `api/internal/usecase/admin_browse_test.go`
- Modify: `api/internal/usecase/ports.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `usecase.DatabaseBrowser` (the port), `usecase.TableInfo`,
  `usecase.ColumnInfo`, `usecase.RowPage`, `usecase.ErrBrowseUnavailable`,
  `usecase.AdminBrowseService` with
  `Tables(ctx) ([]TableInfo, error)` and
  `Rows(ctx, table string, limit, offset int) (RowPage, error)`, and the
  constants `usecase.BrowseDefaultLimit = 50` and `usecase.BrowseMaxLimit = 100`.
  Task 5 implements the port; Task 7 calls the service; Task 8 mirrors the
  limits in TypeScript.

- [ ] **Step 1: Write the failing test**

Create `api/internal/usecase/admin_browse_test.go`:

```go
package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// browserStub records the limit and offset it was called with, so the tests
// below can assert on what the service asked the port for rather than on what
// it returned.
type browserStub struct {
	tables     []usecase.TableInfo
	page       usecase.RowPage
	err        error
	gotTable   string
	gotLimit   int
	gotOffset  int
	tablesCall int
}

func (b *browserStub) Tables(context.Context) ([]usecase.TableInfo, error) {
	b.tablesCall++
	return b.tables, b.err
}

func (b *browserStub) Rows(_ context.Context, table string, limit, offset int) (usecase.RowPage, error) {
	b.gotTable, b.gotLimit, b.gotOffset = table, limit, offset
	return b.page, b.err
}

// The clamp lives in the service and not in the port, whose contract passes
// the limit straight through to SQL's LIMIT: a caller-supplied limit reaching
// that unbounded is how one request ends up reading a whole table. Same
// reasoning as AdminService.RecentAudit.
func TestRowsClampsTheLimit(t *testing.T) {
	cases := []struct {
		name string
		give int
		want int
	}{
		{"absent becomes the default", 0, usecase.BrowseDefaultLimit},
		{"negative becomes the default", -3, usecase.BrowseDefaultLimit},
		{"a sensible limit is passed through", 10, 10},
		{"the maximum is passed through", usecase.BrowseMaxLimit, usecase.BrowseMaxLimit},
		{"more than the maximum is capped, not refused", 5000, usecase.BrowseMaxLimit},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := &browserStub{}
			svc := usecase.NewAdminBrowseService(stub)

			if _, err := svc.Rows(context.Background(), "accounts", c.give, 0); err != nil {
				t.Fatalf("Rows: %v", err)
			}
			if stub.gotLimit != c.want {
				t.Fatalf("port asked for limit %d, want %d", stub.gotLimit, c.want)
			}
		})
	}
}

// A negative offset is not a request the service can serve, and silently
// treating it as 0 would answer a different question from the one asked.
func TestRowsRefusesANegativeOffset(t *testing.T) {
	stub := &browserStub{}
	svc := usecase.NewAdminBrowseService(stub)

	if _, err := svc.Rows(context.Background(), "accounts", 50, -1); err == nil {
		t.Fatal("Rows accepted a negative offset")
	}
	if stub.gotTable != "" {
		t.Fatal("the port was called with a negative offset")
	}
}

// The service adds nothing to a not-found: the operator must be able to tell
// "no such table" from "the browse is broken", and only the first of those is
// their own typo.
func TestRowsPassesNotFoundThrough(t *testing.T) {
	stub := &browserStub{err: domain.ErrNotFound}
	svc := usecase.NewAdminBrowseService(stub)

	if _, err := svc.Rows(context.Background(), "nope", 50, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v, want domain.ErrNotFound", err)
	}
}

func TestTablesPassesUnavailabilityThrough(t *testing.T) {
	stub := &browserStub{err: usecase.ErrBrowseUnavailable}
	svc := usecase.NewAdminBrowseService(stub)

	if _, err := svc.Tables(context.Background()); !errors.Is(err, usecase.ErrBrowseUnavailable) {
		t.Fatalf("error = %v, want usecase.ErrBrowseUnavailable", err)
	}
}

// A nil slice marshals to JSON null, not [], and the frontend's list
// components break on null -- CLAUDE.md's "every 2xx except 204 carries a
// JSON body" rule, one layer up.
func TestTablesNeverReturnsNil(t *testing.T) {
	svc := usecase.NewAdminBrowseService(&browserStub{tables: nil})

	tables, err := svc.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	if tables == nil {
		t.Fatal("Tables returned a nil slice")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/usecase/ -run 'TestRows|TestTables' -v
```

Expected: FAIL, `undefined: usecase.NewAdminBrowseService`.

- [ ] **Step 3: Add the port**

In `api/internal/usecase/ports.go`, add beside the other platform-admin
ports (`PlatformAdminRepository`, `FeatureFlagRepository`,
`AdminAuditRepository`, `AdminDirectoryRepository`):

```go
// DatabaseBrowser is a read-only, structural view of the database, for the
// operator's admin surface.
//
// Every value it returns is already rendered as text: no driver type, no
// `any`, and nothing a caller could accidentally write through. That is not
// only the clean-architecture rule -- it is what lets the implementation
// render a redacted column as a constant inside its own SELECT list, so the
// secret bytes never leave Postgres at all (the spec's decision 7).
//
// An implementation must:
//   - answer domain.ErrNotFound for a table it cannot see, whether because
//     the name does not exist or because its own role has no privilege on it;
//   - answer ErrBrowseUnavailable for anything else that is a failure of the
//     connection rather than of the request;
//   - never write, and never be reachable through a connection that could.
type DatabaseBrowser interface {
	Tables(ctx context.Context) ([]TableInfo, error)
	Rows(ctx context.Context, table string, limit, offset int) (RowPage, error)
}

// TableInfo is one table as the list screen shows it.
type TableInfo struct {
	Name     string
	RowCount int64
	Columns  []ColumnInfo
}

// ColumnInfo describes one column. Redacted is returned to the screen rather
// than being kept private to the implementation, so the operator can see that
// a column was withheld rather than empty -- "there is no value here" and
// "you may not see the value here" are different facts and the screen must
// not merge them.
type ColumnInfo struct {
	Name     string
	DataType string
	Redacted bool
}

// RowPage is one page of one table.
//
// Rows is column-ordered text, parallel to Columns: a Postgres table may
// carry two columns whose names differ only in ways a JSON object key would
// not preserve, and the column list is being sent anyway. A cell is never a
// bare empty string where the value was absent -- see domain.NullCell.
type RowPage struct {
	Columns []ColumnInfo
	Rows    [][]string
	Total   int64
	Limit   int
	Offset  int
}
```

Add the sentinel beside `ErrOutboxUnavailable`:

```go
// ErrBrowseUnavailable is the database browse's "I could not reach the
// store", as distinct from domain.ErrNotFound's "the store answered, and
// there is no such table". The operator needs different advice in each case:
// the first is something to fix on the box, the second is a typo in a URL.
var ErrBrowseUnavailable = errors.New("database browse unavailable")
```

- [ ] **Step 4: Write the service**

Create `api/internal/usecase/admin_browse.go`:

```go
package usecase

import (
	"context"
	"errors"
)

// AdminBrowseService is the operator's read of the database itself. It is its
// own service rather than more methods on AdminService for the same reason
// AdminDirectoryService and AdminOutboxService are: that service is "who is a
// platform admin, feature flags, the audit log", and this one reads every
// table in the schema through a different connection entirely.
//
// It takes no actor parameter. The /admin guards in the HTTP layer are the
// only gate, as everywhere else in this product -- and here that is worth
// saying twice, because this is the one service in Hearth that can read every
// household's money.
//
// It is deliberately thin. Paging is all it decides; the SQL, the validation
// of a table name and the redaction of a column all belong to the
// implementation of DatabaseBrowser, which is the only layer that can enforce
// them where they actually hold.
type AdminBrowseService struct{ browser DatabaseBrowser }

const (
	// BrowseDefaultLimit is how many rows one page carries when the caller
	// names no limit.
	BrowseDefaultLimit = 50
	// BrowseMaxLimit is the most one page will carry. Its own constant
	// rather than the outbox's or the directory's: the three answer
	// different questions, and sharing one would make a change to any of
	// them move the others.
	BrowseMaxLimit = 100
)

// ErrInvalidOffset is a request the service cannot serve at all, as opposed
// to a limit outside the useful range, which it clamps. Asking for more rows
// than the cap is a reasonable question with a bounded answer; asking to
// start before the beginning is not a question.
var ErrInvalidOffset = errors.New("offset must not be negative")

func NewAdminBrowseService(browser DatabaseBrowser) *AdminBrowseService {
	return &AdminBrowseService{browser: browser}
}

// Tables lists every table the browse's own role can see, including the
// migration bookkeeping and the audit log. Nothing is hidden: an operator
// surface that lied about what is in the database would be worse than no
// surface.
func (s *AdminBrowseService) Tables(ctx context.Context) ([]TableInfo, error) {
	tables, err := s.browser.Tables(ctx)
	if err != nil {
		return nil, err
	}
	if tables == nil {
		tables = []TableInfo{}
	}
	return tables, nil
}

// Rows returns one page of one table.
//
// The limit is clamped here rather than in the port, whose contract passes it
// straight through to SQL's LIMIT: a caller-supplied limit reaching that
// unbounded is exactly how one request ends up reading a whole table.
func (s *AdminBrowseService) Rows(ctx context.Context, table string, limit, offset int) (RowPage, error) {
	if offset < 0 {
		return RowPage{}, ErrInvalidOffset
	}
	switch {
	case limit <= 0:
		limit = BrowseDefaultLimit
	case limit > BrowseMaxLimit:
		limit = BrowseMaxLimit
	}

	page, err := s.browser.Rows(ctx, table, limit, offset)
	if err != nil {
		return RowPage{}, err
	}
	if page.Rows == nil {
		page.Rows = [][]string{}
	}
	if page.Columns == nil {
		page.Columns = []ColumnInfo{}
	}
	return page, nil
}
```

- [ ] **Step 5: Run it and watch it pass**

```bash
cd api && go test ./internal/usecase/ -run 'TestRows|TestTables' -v
```

Expected: PASS.

- [ ] **Step 6: Mutation-check the clamp**

Change `case limit > BrowseMaxLimit:` to `case limit > 10000:`. Re-run.
Expected: **FAIL** on `more than the maximum is capped, not refused`, with
`port asked for limit 5000, want 100`. Restore and re-run to green.

- [ ] **Step 7: Run the whole package and the arch lint**

```bash
cd api && go test ./internal/usecase/ -count=1
make lint-arch
```

Expected: PASS and exit 0. `usecase` must still import nothing but the
standard library and `internal/domain`.

- [ ] **Step 8: Commit**

```bash
git add api/internal/usecase/ports.go api/internal/usecase/admin_browse.go \
        api/internal/usecase/admin_browse_test.go
git commit -m "feat(usecase): DatabaseBrowser port and AdminBrowseService"
```

---

### Task 4: The read-only pool

The guard that actually holds, and the check that proves it is really in
place. This is the task where a mistake is worst, so it is deliberately small
and its test is the one to be most suspicious of.

**Files:**
- Create: `api/internal/adapter/postgres/readonly_pool.go`
- Test: `api/internal/adapter/postgres/readonly_pool_test.go`

**Interfaces:**
- Consumes: `testsupport.ReadOnlyURL` from Task 1.
- Produces: `postgres.ReadOnlyDB` (a distinct type from `postgres.DB`),
  `postgres.OpenReadOnly(ctx context.Context, databaseURL string) (*ReadOnlyDB, error)`,
  `postgres.ErrReadOnlyMisconfigured`, `(*ReadOnlyDB).Pool() *pgxpool.Pool`
  and `(*ReadOnlyDB).Close()`. Task 5's repository takes a `*ReadOnlyDB`;
  Task 7's `main.go` opens it and matches on the sentinel to decide whether a
  failure refuses the boot.

- [ ] **Step 1: Write the failing test**

Create `api/internal/adapter/postgres/readonly_pool_test.go`:

```go
package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
)

// Someone will eventually paste the read-write URL into DATABASE_READONLY_URL
// to make a broken panel work, at 1 a.m., meaning to change it back. That is
// not a hypothetical failure mode; it is the ordinary one. This is the test
// that turns the spec's guard 1 from a claim in a document into a property of
// the running process.
func TestOpenReadOnlyRefusesAConnectionThatCanWrite(t *testing.T) {
	adminURL := testsupport.StartPostgres(t)

	db, err := postgres.OpenReadOnly(context.Background(), adminURL)
	if err == nil {
		db.Close()
		t.Fatal("OpenReadOnly accepted the read-write URL")
	}
	// The message is read by an operator at 3 a.m., so it must name the
	// variable rather than describe a privilege check.
	if !strings.Contains(err.Error(), "DATABASE_READONLY_URL") {
		t.Fatalf("error does not name the variable: %v", err)
	}
	// main.go decides whether to refuse the boot by matching this sentinel,
	// and the error travels out through pgxpool's own wrapping of
	// AfterConnect. If this assertion fails, that chain is broken and
	// main.go would degrade instead of refusing -- exactly backwards.
	if !errors.Is(err, postgres.ErrReadOnlyMisconfigured) {
		t.Fatalf("error does not carry ErrReadOnlyMisconfigured: %v", err)
	}
}

func TestOpenReadOnlyAcceptsTheReadOnlyRole(t *testing.T) {
	adminURL := testsupport.StartPostgres(t)

	db, err := postgres.OpenReadOnly(context.Background(), testsupport.ReadOnlyURL(t, adminURL))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(db.Close)

	var one int
	if err := db.Pool().QueryRow(context.Background(), `SELECT 1`).Scan(&one); err != nil {
		t.Fatalf("select through the read-only pool: %v", err)
	}
}

// The pool carries the timeout itself as well as the role carrying it, so a
// database whose role predates this decision -- an existing box provisioned
// from an older PROVISION.md -- is still bounded.
func TestTheReadOnlyPoolBoundsItsOwnStatements(t *testing.T) {
	adminURL := testsupport.StartPostgres(t)

	db, err := postgres.OpenReadOnly(context.Background(), testsupport.ReadOnlyURL(t, adminURL))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(db.Close)

	var setting string
	if err := db.Pool().QueryRow(context.Background(), `SHOW statement_timeout`).Scan(&setting); err != nil {
		t.Fatalf("show: %v", err)
	}
	if setting == "0" {
		t.Fatal("statement_timeout is unset on the read-only pool")
	}
}

func TestOpenReadOnlyRefusesAnUnparseableURL(t *testing.T) {
	// "host=x port=notanumber", not something vaguely wrong-looking: pgx
	// accepts a bare string as a keyword/value DSN, so a value like "not a
	// dsn at all" might parse into keys rather than fail, and the test would
	// pass or fail depending on pgx's parser rather than on this code. A
	// non-numeric port is refused by ParseConfig for certain.
	_, err := postgres.OpenReadOnly(context.Background(), "host=x port=notanumber dbname=hearth")
	if err == nil {
		t.Fatal("OpenReadOnly accepted a URL that is not a DSN")
	}
	if !errors.Is(err, postgres.ErrReadOnlyMisconfigured) {
		t.Fatalf("error does not carry ErrReadOnlyMisconfigured: %v", err)
	}
}

// The other half of the same decision, and the one that keeps a restore from
// taking the whole product down: a database that is simply not there yet is
// NOT a misconfiguration, so main.go degrades instead of refusing the boot.
func TestOpenReadOnlyTreatsAnUnreachableDatabaseAsMerelyUnavailable(t *testing.T) {
	// A DSN that parses and cannot connect: nothing listens on this port.
	_, err := postgres.OpenReadOnly(context.Background(),
		"postgres://hearth_readonly:pw@127.0.0.1:1/hearth?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("OpenReadOnly succeeded against a port nothing listens on")
	}
	if errors.Is(err, postgres.ErrReadOnlyMisconfigured) {
		t.Fatalf("an unreachable database was reported as a misconfiguration: %v", err)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestOpenReadOnly -v
```

Expected: FAIL, `undefined: postgres.OpenReadOnly`.

- [ ] **Step 3: Write the implementation**

Create `api/internal/adapter/postgres/readonly_pool.go`:

```go
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReadOnlyDB is the second connection pool: the one the admin database browse
// reads through, built from DATABASE_READONLY_URL.
//
// It is its own type rather than another *DB on purpose. Both wrap a
// *pgxpool.Pool and nothing in their shapes would stop a repository being
// handed the wrong one -- so the compiler is made to care instead. Nothing
// but the browse takes a *ReadOnlyDB, and the browse takes nothing else.
type ReadOnlyDB struct {
	pool *pgxpool.Pool
}

// ErrReadOnlyMisconfigured marks the failures a human caused and no retry
// fixes: a DSN that cannot be parsed, and a connection that turns out to be
// able to write. main.go refuses the boot on these and only these.
//
// Everything else -- a database that is not up yet, a host that does not
// resolve, a role that does not exist -- is deliberately NOT this. The day
// that happens is the day someone is restoring onto a fresh box with the
// variable already in .env, and refusing the boot there would take the whole
// household product down over an operator panel.
var ErrReadOnlyMisconfigured = errors.New("DATABASE_READONLY_URL is misconfigured")

// browseStatementTimeout bounds every statement this pool runs, in
// milliseconds because that is the unit Postgres reads an unsuffixed
// statement_timeout in.
//
// It is set here as well as on the hearth_readonly role itself (see
// deploy/readonly-role.sql). Two mechanisms for one rule is right here
// because they fail independently: a box provisioned from an older
// PROVISION.md has a role without the setting, and this still bounds it.
const browseStatementTimeout = "3000"

// browseMaxConns is small because one operator clicks this panel. Open's
// MaxConns of 10 is sized for the product's request traffic; giving the same
// budget to an admin screen would let a runaway browse take connections the
// household product needs.
const browseMaxConns = 3

// OpenReadOnly builds the browse's pool and refuses to return one that can
// write.
//
// The privilege check runs in AfterConnect, so it holds for every connection
// the pool ever opens rather than only for the first: a boot-time-only check
// is a statement about the process that started, and this one stays true
// after a reconnect and after somebody grants the role something at 2 a.m.
// Ping below is what forces the first connection, so a wrong URL fails here
// and not on the first operator request.
func OpenReadOnly(ctx context.Context, databaseURL string) (*ReadOnlyDB, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: parse DATABASE_READONLY_URL: %v", ErrReadOnlyMisconfigured, err)
	}
	cfg.MaxConns = browseMaxConns
	cfg.MaxConnLifetime = time.Hour
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = browseStatementTimeout
	cfg.AfterConnect = assertCannotWrite

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create the read-only pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping the read-only database: %w", err)
	}
	return &ReadOnlyDB{pool: pool}, nil
}

// assertCannotWrite refuses any connection that could modify the product's
// data.
//
// users is the probe table because it exists in migration 00002, is never
// dropped, and holds credentials: if this connection can write *there*,
// nothing else about the configuration matters. The check is a privilege
// lookup and not an attempted write, so it leaves nothing behind even on the
// connection it rejects.
//
// All three outcomes are distinct and only one of them is a pass. An error
// reading the privilege is a refusal too -- "I could not tell" is not
// "read-only, fine", and this is exactly the value CLAUDE.md's fail-closed
// rule is about.
func assertCannotWrite(ctx context.Context, conn *pgx.Conn) error {
	var canWrite bool
	err := conn.QueryRow(ctx,
		`SELECT has_table_privilege(current_user, 'users', 'INSERT')`).Scan(&canWrite)
	if err != nil {
		return fmt.Errorf("could not check whether DATABASE_READONLY_URL is read-only: %w", err)
	}
	if canWrite {
		return fmt.Errorf("%w: DATABASE_READONLY_URL connects as a role that may INSERT into users, "+
			"so it is not a read-only role. Point it at hearth_readonly (deploy/readonly-role.sql)",
			ErrReadOnlyMisconfigured)
	}
	return nil
}

func (db *ReadOnlyDB) Pool() *pgxpool.Pool { return db.pool }
func (db *ReadOnlyDB) Close()              { db.pool.Close() }
```

- [ ] **Step 4: Run it and watch it pass**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestOpenReadOnly -v
cd api && go test ./internal/adapter/postgres/ -run TestTheReadOnlyPool -v
```

Expected: PASS.

- [ ] **Step 5: Mutation-check the self-check**

Invert the check: change `if canWrite {` to `if !canWrite {`. Re-run.
Expected: **FAIL** on `TestOpenReadOnlyRefusesAConnectionThatCanWrite`
**and** on `TestOpenReadOnlyAcceptsTheReadOnlyRole` — both, because the two
tests are the two sides of the same decision and a single-sided test would
have let a permanently-refusing check ship. Restore and re-run to green.

Then remove `cfg.AfterConnect = assertCannotWrite` entirely and re-run:
expected **FAIL** on the refusal test alone. Restore.

Then change the `canWrite` branch to return a bare `errors.New(...)` with no
`%w`. Re-run. Expected: **FAIL** on the `ErrReadOnlyMisconfigured` assertion
in the refusal test, and **not** on anything else — which is the check that
Task 7's `main.go` will actually refuse the boot rather than degrade. If it
fails for a different reason, `pgxpool` is not preserving the error chain out
of `AfterConnect`; in that case match on the sentinel at the point
`assertCannotWrite` is called instead of relying on `Ping`'s wrapping, and say
so in a comment. Restore.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/postgres/readonly_pool.go \
        api/internal/adapter/postgres/readonly_pool_test.go
git commit -m "feat(db): a read-only pool that refuses a connection which can write"
```

---

### Task 5: The adapter — `BrowseRepo`

The SQL. Everything the spec's decisions 2, 6, 7, 9, 10 and 11 promise is
enforced here or nowhere.

**Files:**
- Create: `api/internal/adapter/postgres/browse_repo.go`
- Test: `api/internal/adapter/postgres/browse_repo_test.go`

**Interfaces:**
- Consumes: `postgres.ReadOnlyDB` (Task 4), `usecase.DatabaseBrowser` and its
  value types (Task 3), `domain.ColumnIsRedacted`, `domain.RedactedCell`,
  `domain.NullCell` (Task 2), `testsupport.ReadOnlyURL` (Task 1).
- Produces: `postgres.NewBrowseRepo(db *ReadOnlyDB) *BrowseRepo`, satisfying
  `usecase.DatabaseBrowser`. Task 7's `main.go` constructs it.

- [ ] **Step 1: Write the failing test**

Create `api/internal/adapter/postgres/browse_repo_test.go`:

```go
package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
)

// browseFixture holds both connections to the same container: the read-write
// one only ever used to put rows there, and the read-only one under test.
// One container per Test func, subtests inside -- StartPostgres boots a fresh
// container on every call and there is no reuse.
type browseFixture struct {
	admin *postgres.DB
	repo  *postgres.BrowseRepo
}

func newBrowseFixture(t *testing.T) browseFixture {
	t.Helper()
	ctx := context.Background()
	adminURL := testsupport.StartPostgres(t)

	admin, err := postgres.Open(ctx, adminURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(admin.Close)

	readonly, err := postgres.OpenReadOnly(ctx, testsupport.ReadOnlyURL(t, adminURL))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(readonly.Close)

	return browseFixture{admin: admin, repo: postgres.NewBrowseRepo(readonly)}
}

// A name that is not a table must be answered the same way whether it is a
// typo, a table in another schema, or an attempt to smuggle SQL through the
// URL. The interesting half is the second: these strings must come back as
// ErrNotFound, never as an error from Postgres, because an error from
// Postgres would mean the name reached a query.
func TestRowsRefusesAnythingThatIsNotATable(t *testing.T) {
	f := newBrowseFixture(t)

	for _, name := range []string{
		"no_such_table",
		"pg_catalog.pg_authid",
		`users"; drop table users; --`,
		"users; select 1",
		"'users'",
		"",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.repo.Rows(context.Background(), name, 10, 0)
			if !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("Rows(%q) error = %v, want domain.ErrNotFound", name, err)
			}
		})
	}
}

// The bytes of a redacted column must not be in the answer at all -- not
// hex-encoded, not truncated, not anywhere. This asserts on the whole page
// rather than on the one cell, because "the cell says «redacted»" would still
// pass if the value were also being carried somewhere else.
func TestRowsRedactsSecretsAndNeverCarriesTheirBytes(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	// A session row carries token_hash bytea. Anything recognisable in it is
	// what must not appear.
	seedSession(t, f.admin, []byte{0xDE, 0xAD, 0xBE, 0xEF})

	page, err := f.repo.Rows(ctx, "sessions", 10, 0)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}

	var redactedColumn int = -1
	for i, c := range page.Columns {
		if c.Name == "token_hash" {
			redactedColumn = i
			if !c.Redacted {
				t.Fatal("token_hash is not reported as redacted")
			}
		}
	}
	if redactedColumn < 0 {
		t.Fatal("no token_hash column in the page")
	}
	if page.Rows[0][redactedColumn] != domain.RedactedCell {
		t.Fatalf("token_hash cell = %q, want %q", page.Rows[0][redactedColumn], domain.RedactedCell)
	}

	whole := strings.ToLower(strings.Join(flatten(page.Rows), "|"))
	for _, forbidden := range []string{"deadbeef", `\xdeadbeef`} {
		if strings.Contains(whole, forbidden) {
			t.Fatalf("the page carries the redacted bytes (%q)", forbidden)
		}
	}
}

// NULL and the empty string are different facts and [][]string cannot carry
// the difference on its own.
func TestRowsDistinguishesNullFromEmpty(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	// Pick a table with a nullable text column and insert one row with NULL
	// and one with ''. Read api/migrations/ first and use a real one --
	// users.password_hash is NULL for a magic-link-only member, which is the
	// case this rule exists for.
	seedUserWithoutAPassword(t, f.admin)

	page, err := f.repo.Rows(ctx, "users", 10, 0)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	cell := cellOf(t, page, "password_hash", 0)
	if cell != domain.NullCell {
		t.Fatalf("a NULL rendered as %q, want %q", cell, domain.NullCell)
	}
}

// OFFSET without ORDER BY lets Postgres return rows in any order it likes,
// and it exercises that permission: page 2 can repeat a row from page 1 and
// skip another entirely, with nothing raising an error. The operator simply
// does not see a row that is there.
func TestRowsPagesWithoutRepeatingOrSkipping(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	seedAccounts(t, f.admin, 5)

	seen := map[string]bool{}
	for offset := 0; offset < 6; offset += 2 {
		page, err := f.repo.Rows(ctx, "accounts", 2, offset)
		if err != nil {
			t.Fatalf("Rows(offset=%d): %v", offset, err)
		}
		if page.Total != 5 {
			t.Fatalf("Total = %d, want 5", page.Total)
		}
		for _, row := range page.Rows {
			id := cellByName(t, page, row, "id")
			if seen[id] {
				t.Fatalf("row %s appeared on two pages", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 5 {
		t.Fatalf("saw %d of 5 rows across the pages", len(seen))
	}
}

// A valid question with a boring answer. A 404 here would be
// indistinguishable from the table not existing, which is a different
// problem.
func TestRowsAnswersAnEmptyPagePastTheEnd(t *testing.T) {
	f := newBrowseFixture(t)

	seedAccounts(t, f.admin, 3)

	page, err := f.repo.Rows(context.Background(), "accounts", 10, 1000)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(page.Rows) != 0 {
		t.Fatalf("got %d rows past the end, want 0", len(page.Rows))
	}
	if page.Total != 3 {
		t.Fatalf("Total = %d, want 3", page.Total)
	}
}

// Nothing is hidden from the list: the migration bookkeeping and the audit
// log are tables like any other. admin_audit_log matters twice over, because
// descoping the /admin/audit screen left this browse as its only UI.
func TestTablesListsEverythingIncludingTheBookkeeping(t *testing.T) {
	f := newBrowseFixture(t)

	tables, err := f.repo.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	byName := map[string]int64{}
	for _, tbl := range tables {
		byName[tbl.Name] = tbl.RowCount
	}
	for _, want := range []string{"households", "users", "admin_audit_log", "goose_db_version"} {
		if _, ok := byName[want]; !ok {
			t.Fatalf("%s is missing from the table list", want)
		}
	}
}

// Decision 6, tested rather than asserted in prose. Validating the table name
// through the read-only pool is supposed to answer a stronger question than
// "does this name exist" -- it answers "and may this connection read it".
// Without this test, TestRowsRefusesAnythingThatIsNotATable proves only that
// six names which exist nowhere are refused, which is trivially true whatever
// pool the lookup runs on.
func TestATableThisRoleCannotReadIsNotFound(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	if _, err := f.admin.Pool().Exec(ctx,
		`CREATE TABLE secret_stuff (id bigint primary key)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	// ALTER DEFAULT PRIVILEGES will have granted SELECT on it already, which
	// is decision 2 working; take it away again to make this table stand for
	// one the grants never reached.
	if _, err := f.admin.Pool().Exec(ctx,
		`REVOKE SELECT ON secret_stuff FROM hearth_readonly`); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if _, err := f.repo.Rows(ctx, "secret_stuff", 10, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Rows on an unreadable table: error = %v, want domain.ErrNotFound", err)
	}

	tables, err := f.repo.Tables(ctx)
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	for _, tbl := range tables {
		if tbl.Name == "secret_stuff" {
			t.Fatal("a table this role cannot read is listed by Tables")
		}
	}
}

// Decision 2, tested directly rather than trusted. A table created after the
// role script ran is the shape of every future migration, and without ALTER
// DEFAULT PRIVILEGES it is invisible here with nothing reporting it.
func TestTablesSeesATableCreatedAfterTheRole(t *testing.T) {
	f := newBrowseFixture(t)
	ctx := context.Background()

	if _, err := f.admin.Pool().Exec(ctx,
		`CREATE TABLE later_migration (id bigint primary key, note text)`); err != nil {
		t.Fatalf("create the later table: %v", err)
	}

	tables, err := f.repo.Tables(ctx)
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	for _, tbl := range tables {
		if tbl.Name == "later_migration" {
			return
		}
	}
	t.Fatal("a table created after the role is invisible to the browse: ALTER DEFAULT PRIVILEGES is missing")
}
```

Write the four helpers (`seedSession`, `seedUserWithoutAPassword`,
`seedAccounts`, plus `flatten`, `cellOf` and `cellByName`) against the real
schema — read `api/migrations/00002_identity.sql` and `00004_accounts.sql`
first and satisfy every NOT NULL and every foreign key. A seed helper that
silently inserts nothing turns each of these tests green for the wrong
reason, so assert on the command tag or the row count inside the helper.

- [ ] **Step 2: Run them and watch them fail**

```bash
cd api && go test ./internal/adapter/postgres/ -run 'TestRows|TestTables' -v
```

Expected: FAIL, `undefined: postgres.NewBrowseRepo`.

- [ ] **Step 3: Write the implementation**

Create `api/internal/adapter/postgres/browse_repo.go`:

```go
package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// BrowseRepo reads the database's own shape and contents for the operator's
// admin surface.
//
// It is the only hand-written pgx file in this package, and that is worth the
// sentence: every other repository here is generated by sqlc from a static
// .sql file in queries/. sqlc turns *fixed* SQL into typed Go, and this
// repository's entire job is a SELECT list and a FROM clause chosen at call
// time from the catalogue -- there is no static query to generate from. So
// the SQL lives here as strings, under two rules that are not optional:
//
//   - No value from a request is ever concatenated into SQL. A table name is
//     matched against information_schema first (which, read through this
//     pool, also answers "and may this role see it") and quoted with
//     pgx.Identifier second. Everything else travels as a bind parameter.
//   - A redacted column is never selected. Its place in the SELECT list is a
//     parameter, so the secret bytes stay in Postgres rather than being
//     fetched and then dropped in Go -- see the spec's decision 7.
type BrowseRepo struct{ pool *pgxpool.Pool }

// Its own assertion here rather than in convert.go's block, next to the
// reasoning it has to hold up.
var _ usecase.DatabaseBrowser = (*BrowseRepo)(nil)

// NewBrowseRepo takes a *ReadOnlyDB and nothing else. The type is what stops
// this being constructed over the read-write pool by mistake; there is no
// constructor that would accept one.
func NewBrowseRepo(db *ReadOnlyDB) *BrowseRepo { return &BrowseRepo{pool: db.Pool()} }

func (r *BrowseRepo) Tables(ctx context.Context) ([]usecase.TableInfo, error) {
	names, err := r.tableNames(ctx)
	if err != nil {
		return nil, err
	}
	columns, err := r.allColumns(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := r.rowCounts(ctx, names)
	if err != nil {
		return nil, err
	}

	tables := make([]usecase.TableInfo, 0, len(names))
	for i, name := range names {
		tables = append(tables, usecase.TableInfo{
			Name:     name,
			RowCount: counts[i],
			Columns:  columns[name],
		})
	}
	return tables, nil
}

func (r *BrowseRepo) Rows(ctx context.Context, table string, limit, offset int) (usecase.RowPage, error) {
	columns, err := r.columnsOf(ctx, table)
	if err != nil {
		return usecase.RowPage{}, err
	}
	quoted := pgx.Identifier{table}.Sanitize()

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM `+quoted).Scan(&total); err != nil {
		return usecase.RowPage{}, browseErr(err, "count rows")
	}

	orderBy, err := r.orderBy(ctx, table)
	if err != nil {
		return usecase.RowPage{}, err
	}

	// Parameters are numbered as they are added, so a redacted column and a
	// nullable one both contribute their own placeholder and the count can
	// never drift from the list. Sending a parameter the query does not
	// mention is a bind error, which is what a hand-numbered version would
	// eventually produce on some table nobody tested.
	args := make([]any, 0, len(columns)+2)
	placeholder := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	list := make([]string, 0, len(columns))
	for _, c := range columns {
		if c.Redacted {
			list = append(list, placeholder(domain.RedactedCell)+"::text")
			continue
		}
		list = append(list, "coalesce("+pgx.Identifier{c.Name}.Sanitize()+"::text, "+placeholder(domain.NullCell)+")")
	}

	query := "SELECT " + strings.Join(list, ", ") +
		" FROM " + quoted +
		" ORDER BY " + orderBy +
		" LIMIT " + placeholder(limit) +
		" OFFSET " + placeholder(offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return usecase.RowPage{}, browseErr(err, "read rows")
	}
	defer rows.Close()

	out := make([][]string, 0, limit)
	for rows.Next() {
		cells := make([]string, len(columns))
		dest := make([]any, len(columns))
		for i := range cells {
			dest[i] = &cells[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return usecase.RowPage{}, browseErr(err, "scan row")
		}
		out = append(out, cells)
	}
	if err := rows.Err(); err != nil {
		return usecase.RowPage{}, browseErr(err, "read rows")
	}

	return usecase.RowPage{
		Columns: columns,
		Rows:    out,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	}, nil
}

// columnsOf is both the column list and the table's existence check, and
// running it through this pool makes it a privilege check too:
// information_schema is filtered per role, so a table the GRANT missed
// answers "no such table" here rather than reaching a SELECT that fails with
// a raw Postgres permission error the operator has to interpret.
func (r *BrowseRepo) columnsOf(ctx context.Context, table string) ([]usecase.ColumnInfo, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.column_name, c.data_type
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = 'public'
		  AND t.table_type = 'BASE TABLE'
		  AND c.table_name = $1
		ORDER BY c.ordinal_position`, table)
	if err != nil {
		return nil, browseErr(err, "read columns")
	}
	defer rows.Close()

	var columns []usecase.ColumnInfo
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return nil, browseErr(err, "scan column")
		}
		columns = append(columns, usecase.ColumnInfo{
			Name:     name,
			DataType: dataType,
			Redacted: domain.ColumnIsRedacted(name, dataType),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, browseErr(err, "read columns")
	}
	// No columns means no such table for this role, and that is the only
	// path by which a name from a URL is ever refused -- the name has still
	// not touched a query except as a bind parameter.
	if len(columns) == 0 {
		return nil, domain.ErrNotFound
	}
	return columns, nil
}

// orderBy is what makes "page 2" mean anything. LIMIT/OFFSET without ORDER BY
// lets Postgres return rows in any order it likes, so page 2 can repeat a row
// from page 1 and skip another, with nothing raising an error.
//
// The primary key is the ordering when there is one. ctid is the fallback: an
// arbitrary ordering, but a stable one within a read, and honest about being
// arbitrary. This is not sorting as a feature -- the operator cannot choose
// it and it is not a control on the screen.
func (r *BrowseRepo) orderBy(ctx context.Context, table string) (string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY (i.indkey)
		WHERE i.indrelid = to_regclass('public.' || quote_ident($1))
		  AND i.indisprimary
		ORDER BY array_position(i.indkey::int2[], a.attnum)`, table)
	if err != nil {
		return "", browseErr(err, "read the primary key")
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return "", browseErr(err, "scan the primary key")
		}
		keys = append(keys, pgx.Identifier{name}.Sanitize())
	}
	if err := rows.Err(); err != nil {
		return "", browseErr(err, "read the primary key")
	}
	if len(keys) == 0 {
		return "ctid", nil
	}
	return strings.Join(keys, ", "), nil
}

func (r *BrowseRepo) tableNames(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
		ORDER BY table_name`)
	if err != nil {
		return nil, browseErr(err, "list tables")
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, browseErr(err, "scan table name")
		}
		names = append(names, name)
	}
	return names, browseErr(rows.Err(), "list tables")
}

func (r *BrowseRepo) allColumns(ctx context.Context) (map[string][]usecase.ColumnInfo, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT table_name, column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
		ORDER BY table_name, ordinal_position`)
	if err != nil {
		return nil, browseErr(err, "list columns")
	}
	defer rows.Close()

	byTable := map[string][]usecase.ColumnInfo{}
	for rows.Next() {
		var table, name, dataType string
		if err := rows.Scan(&table, &name, &dataType); err != nil {
			return nil, browseErr(err, "scan column")
		}
		byTable[table] = append(byTable[table], usecase.ColumnInfo{
			Name:     name,
			DataType: dataType,
			Redacted: domain.ColumnIsRedacted(name, dataType),
		})
	}
	return byTable, browseErr(rows.Err(), "list columns")
}

// rowCounts counts every table in one statement rather than one round trip
// per table. The counts are exact: this database has a few dozen small tables
// and a three-second ceiling, and labelling every figure "approx" from
// pg_class.reltuples would buy nothing on a screen whose whole job is to show
// what is actually there. If a table ever grows enough for this to hit the
// timeout, the failure is loud and is the signal to revisit it.
//
// Positional rather than labelled: the names come back in the same order they
// went in, which avoids quoting a table name as a string literal anywhere.
func (r *BrowseRepo) rowCounts(ctx context.Context, names []string) ([]int64, error) {
	if len(names) == 0 {
		return nil, nil
	}
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, "(SELECT count(*) FROM "+pgx.Identifier{name}.Sanitize()+")")
	}

	counts := make([]int64, len(names))
	dest := make([]any, len(names))
	for i := range counts {
		dest[i] = &counts[i]
	}
	if err := r.pool.QueryRow(ctx, "SELECT "+strings.Join(parts, ", ")).Scan(dest...); err != nil {
		return nil, browseErr(err, "count rows")
	}
	return counts, nil
}

// browseErr keeps domain.ErrNotFound intact and turns everything else into
// ErrBrowseUnavailable, so the HTTP layer can tell "no such table" from "the
// reader is broken" -- the operator needs different advice for each. The op
// phrase is kept for the log; the sentinel is what the caller matches on.
func browseErr(err error, op string) error {
	if err == nil {
		return nil
	}
	if translated := translate(err, op); errors.Is(translated, domain.ErrNotFound) {
		return domain.ErrNotFound
	}
	return fmt.Errorf("%w: %s: %v", usecase.ErrBrowseUnavailable, op, err)
}
```

Add `"errors"` to the imports for `browseErr`.

- [ ] **Step 4: Run them and watch them pass**

```bash
cd api && go test ./internal/adapter/postgres/ -run 'TestRows|TestTables' -v
```

Expected: PASS. If `TestRowsRefusesAnythingThatIsNotATable` fails with a
Postgres syntax error rather than `ErrNotFound`, a name reached a query — stop
and fix that before anything else on this branch.

- [ ] **Step 5: Mutation-check the ordering and the redaction**

Delete `" ORDER BY " + orderBy +` from the row query. Re-run. Expected:
**FAIL** on `TestRowsPagesWithoutRepeatingOrSkipping`. If it passes, the test
is not proving anything — five rows in insertion order will often come back
stably by accident, so raise the row count until the test fails without the
`ORDER BY`, then restore it and keep the higher count.

Then change the redacted branch to select the column normally. Re-run.
Expected: **FAIL** on `TestRowsRedactsSecretsAndNeverCarriesTheirBytes`, on
the `deadbeef` assertion rather than only on the `Redacted` flag. Restore.

- [ ] **Step 6: Run the whole package and the arch lint**

```bash
cd api && go test ./internal/adapter/postgres/ -count=1
make lint-arch
```

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/postgres/browse_repo.go \
        api/internal/adapter/postgres/browse_repo_test.go
git commit -m "feat(db): the database browse adapter, redaction inside the SELECT list"
```

---

### Task 6: `DATABASE_READONLY_URL`

**Files:**
- Modify: `api/internal/config/config.go`
- Test: `api/internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config.DatabaseReadonlyURL string` and
  `(config.Config).BrowseEnabled() bool`. Task 7's `main.go` uses both.

**A deliberate difference from the spec.** §7 says a set-but-unparseable value
"refuses the boot ... as `config.go` already refuses a half-set Telegram
pair", which reads as a validation branch in `Load`. It cannot be one.
`config.go` imports the standard library only, and `net/url` does not validate
a Postgres DSN: `url.Parse("host=db user=x")` succeeds with an empty scheme
and empty host, and that keyword/value form is a legal DSN which pgx accepts.
A `url.Parse` check in `Load` would therefore either reject a legal DSN or
accept nonsense. `pgxpool.ParseConfig` is the only honest parser, and importing
pgx into `internal/config` would make it the first non-adapter package with a
third-party dependency.

So the refusal happens one step later and in the right layer: `Load` records
the string, and `postgres.OpenReadOnly`'s existing
`parse DATABASE_READONLY_URL:` error is returned from `run()`. The outcome the
spec asks for — the API does not start — is unchanged. The plan's final
section records this.

- [ ] **Step 1: Write the failing test**

Append to `api/internal/config/config_test.go`, following the shape the file
already uses for `MAILPIT_API_URL` (`setRequiredEnv(t)` then `t.Setenv`):

```go
func TestLoadAcceptsAnAbsentDatabaseReadonlyURL(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseReadonlyURL != "" {
		t.Fatalf("DatabaseReadonlyURL = %q, want empty", cfg.DatabaseReadonlyURL)
	}
	if cfg.BrowseEnabled() {
		t.Fatal("BrowseEnabled() = true with no URL set")
	}
}

func TestLoadAcceptsADatabaseReadonlyURL(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DATABASE_READONLY_URL", "postgres://hearth_readonly:pw@postgres:5432/hearth?sslmode=disable")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.BrowseEnabled() {
		t.Fatal("BrowseEnabled() = false with a URL set")
	}
}

// Unlike MAILPIT_API_URL, Load does NOT reject an unusable value here, and
// that is deliberate rather than an omission: net/url cannot tell a broken
// DSN from a legal keyword/value one, and pgxpool.ParseConfig -- which can --
// belongs to the adapter layer. postgres.OpenReadOnly is where a bad value
// refuses the boot; TestOpenReadOnlyRefusesAnUnparseableURL is its test. This
// test exists to record that the omission was decided, so that nobody
// "fixes" it later with a url.Parse that rejects a legal DSN.
func TestLoadDoesNotItselfValidateTheReadonlyDSN(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DATABASE_READONLY_URL", "host=postgres user=hearth_readonly dbname=hearth")

	if _, err := config.Load(); err != nil {
		t.Fatalf("Load rejected a legal keyword/value DSN: %v", err)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/config/ -run 'TestLoad.*Readonly|TestLoadDoesNot' -v
```

Expected: FAIL, `cfg.DatabaseReadonlyURL undefined`.

- [ ] **Step 3: Add the field, the accessor and the read**

In `api/internal/config/config.go`, add to the `Config` struct beside
`MailpitAPIURL`:

```go
	// DatabaseReadonlyURL is the DSN for hearth_readonly, the SELECT-only
	// role the operator's database browse reads through
	// (deploy/readonly-role.sql creates it). Optional: empty means the
	// browse is unavailable and says which variable is missing.
	//
	// There is deliberately no fallback to DatabaseURL. A half-provisioned
	// box degrades to "you cannot use this panel", never to "you are using
	// it through the read-write connection".
	//
	// It is NOT validated here, unlike every other optional value in this
	// file. net/url cannot tell a broken DSN from a legal keyword/value one
	// ("host=db user=x" parses fine and is valid), and the only honest
	// parser is pgxpool.ParseConfig, which belongs to the adapter layer --
	// this package imports the standard library and nothing else, and that
	// is worth more than moving one error message. postgres.OpenReadOnly
	// refuses the boot on a value it cannot parse, and on one that connects
	// as a role which can write.
	DatabaseReadonlyURL string
```

Add the accessor beside `OutboxEnabled`:

```go
// BrowseEnabled reports whether the operator's database browse is configured.
// When it is false the admin routes answer 503 and name the variable -- never
// 404, because everyone who can reach them has already proved they are a
// platform admin with a live grant, and hiding the route from them would cost
// them the one fact that says what to fix.
func (c Config) BrowseEnabled() bool { return c.DatabaseReadonlyURL != "" }
```

Read it in `Load`, beside `MailpitAPIURL`:

```go
		DatabaseReadonlyURL: os.Getenv("DATABASE_READONLY_URL"),
```

- [ ] **Step 4: Run it and watch it pass**

```bash
cd api && go test ./internal/config/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Mutation-check the accessor**

Change `BrowseEnabled` to `return true`. Re-run. Expected: **FAIL** on
`TestLoadAcceptsAnAbsentDatabaseReadonlyURL`. Restore.

- [ ] **Step 6: Commit**

```bash
git add api/internal/config/config.go api/internal/config/config_test.go
git commit -m "feat(config): DATABASE_READONLY_URL, unset means the browse is off"
```

---

### Task 7: The two routes, and the wiring

The task that makes the feature real end to end: DTOs, handlers, the error
mapping, the route registration and the `main.go` wiring. One task, because
none of those is independently reviewable — a handler with no route is not a
deliverable.

**Files:**
- Create: `api/internal/adapter/http/admin_browse_handlers.go`
- Test: `api/internal/adapter/http/admin_browse_api_test.go`
- Modify: `api/internal/adapter/http/router.go`,
  `api/internal/adapter/http/errors.go`,
  `api/internal/adapter/http/admin_api_test.go`,
  `api/cmd/api/main.go`

**Interfaces:**
- Consumes: `usecase.AdminBrowseService` (Task 3), `postgres.OpenReadOnly` and
  `postgres.NewBrowseRepo` (Tasks 4 and 5), `config.BrowseEnabled` (Task 6).
- Produces: `GET /api/v1/admin/db/tables` and
  `GET /api/v1/admin/db/tables/{table}?limit=&offset=`, and the JSON shapes
  Task 8 mirrors in Zod.

- [ ] **Step 1: Write the failing test**

Create `api/internal/adapter/http/admin_browse_api_test.go`. Follow
`admin_outbox_api_test.go` for the environment helpers (`newTestEnv`,
`makePlatformAdmin`, `grantAdmin`, `authedGet`, `assertErrorResponse`,
`errorCode`) — read that file first and reuse its helpers rather than writing
new ones.

```go
// Every row of the spec's decision 12 table, plus the two properties that are
// this feature's whole reason for existing: the guards apply, and the audit
// row names what was read.
func TestAdminDatabaseTablesAnswersTheTableList(t *testing.T) { /* 200, tables non-empty, households present */ }

func TestAdminDatabaseRowsPages(t *testing.T) { /* 200, limit/offset echoed, total right */ }

// Absent is not an error; present and unusable is.
func TestAdminDatabaseRowsRefusesAnUnusableRange(t *testing.T) {
	for _, query := range []string{"?limit=abc", "?limit=0", "?limit=-1", "?offset=-1", "?offset=x"} {
		// 400 INVALID_RANGE
	}
}

// Over the cap is clamped, not refused: asking for more than the maximum is a
// reasonable request with a bounded answer.
func TestAdminDatabaseRowsClampsALargeLimit(t *testing.T) { /* ?limit=5000 -> 200, body limit == 100 */ }

func TestAdminDatabaseUnknownTableIs404(t *testing.T) { /* NOT_FOUND */ }

// 503 and not 404: everyone who reaches this handler has already proved they
// are a platform admin with a live grant, so hiding the route from them would
// cost them the one fact that says what to fix.
func TestAdminDatabaseAnswers503WhenUnconfigured(t *testing.T) {
	// Deps built with AdminBrowse nil.
	// 503, DB_BROWSE_NOT_CONFIGURED, and the message names DATABASE_READONLY_URL.
}

func TestAdminDatabaseAnswers503WhenTheBrowseIsBroken(t *testing.T) {
	// A stub DatabaseBrowser returning usecase.ErrBrowseUnavailable.
	// 503, DB_BROWSE_UNAVAILABLE -- a different code from the one above,
	// because "not configured" and "configured and broken" send the operator
	// to different places.
}

// The audit row is what makes reading a household's money an act with a
// record. auditAdmin writes before chi matches the route, so the table name
// is in the path and the offset is in the query string -- assert on the row,
// not on the middleware.
func TestReadingRowsLeavesAnAuditRowNamingTheTableAndOffset(t *testing.T) {
	// GET /api/v1/admin/db/tables/accounts?limit=10&offset=20
	// then read admin_audit_log's newest row and assert:
	//   target contains "/api/v1/admin/db/tables/accounts"
	//   detail's query contains "offset=20"
}
```

Write each body out in full against the existing helpers. Where
`admin_outbox_api_test.go` builds a `Deps` with a stub service, copy that
construction exactly.

- [ ] **Step 2: Run them and watch them fail**

```bash
cd api && go test ./internal/adapter/http/ -run TestAdminDatabase -v
```

Expected: FAIL — the routes answer 404 because they do not exist.

- [ ] **Step 3: Write the handlers**

Create `api/internal/adapter/http/admin_browse_handlers.go`:

```go
package httpadapter

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// The operator's database browse: the table list and one page of one table.
// Both are reads inside the /admin granted group, so requirePlatformAdmin,
// auditAdmin, requireCSRF and requireAdminGrant apply by construction --
// nothing here checks who is asking.
//
// This is the one surface in Hearth that can read a household's money, which
// is why it costs a re-authentication and why every request through it is an
// audit row. The table name is a path segment and the offset is a query
// parameter, so auditAdmin -- which runs before chi matches the route --
// already records both without this file doing anything.

type browseColumnDTO struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Redacted bool   `json:"redacted"`
}

type browseTableDTO struct {
	Name     string            `json:"name"`
	RowCount int64             `json:"rowCount"`
	Columns  []browseColumnDTO `json:"columns"`
}

type browseTablesResponse struct {
	Tables []browseTableDTO `json:"tables"`
}

type browseRowsResponse struct {
	Table   string            `json:"table"`
	Columns []browseColumnDTO `json:"columns"`
	Rows    [][]string        `json:"rows"`
	Total   int64             `json:"total"`
	Limit   int               `json:"limit"`
	Offset  int               `json:"offset"`
}

// writeBrowseUnconfigured is the answer when DATABASE_READONLY_URL is unset.
// 503 and not 404 on purpose, and the message names the variable because the
// person reading it is the person who can set it.
func writeBrowseUnconfigured(w http.ResponseWriter) {
	WriteError(w, http.StatusServiceUnavailable, "DB_BROWSE_NOT_CONFIGURED",
		"The database browse is not configured on this install. Set DATABASE_READONLY_URL and restart the API.", nil)
}

func browseColumns(columns []usecase.ColumnInfo) []browseColumnDTO {
	out := make([]browseColumnDTO, 0, len(columns))
	for _, c := range columns {
		out = append(out, browseColumnDTO{Name: c.Name, DataType: c.DataType, Redacted: c.Redacted})
	}
	return out
}

// parseBrowseRange reads limit and offset from the query string.
//
// Absent is not an error -- the service has a default, and the operator typed
// a URL rather than filling in a form. Present and unusable is refused before
// any query runs: limit=0 and offset=-1 are not questions the browse can
// answer, and silently reading them as "the default" would answer a different
// question from the one asked. A limit above the cap is neither -- it is
// clamped by the service, not refused here.
func parseBrowseRange(q url.Values) (limit, offset int, ok bool) {
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return 0, 0, false
		}
		limit = n
	}
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, 0, false
		}
		offset = n
	}
	return limit, offset, true
}

func handleAdminDatabaseTables(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.AdminBrowse == nil {
			writeBrowseUnconfigured(w)
			return
		}
		tables, err := deps.AdminBrowse.Tables(r.Context())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		body := browseTablesResponse{Tables: make([]browseTableDTO, 0, len(tables))}
		for _, t := range tables {
			body.Tables = append(body.Tables, browseTableDTO{
				Name: t.Name, RowCount: t.RowCount, Columns: browseColumns(t.Columns),
			})
		}
		WriteJSON(w, http.StatusOK, body)
	}
}

func handleAdminDatabaseRows(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.AdminBrowse == nil {
			writeBrowseUnconfigured(w)
			return
		}
		limit, offset, ok := parseBrowseRange(r.URL.Query())
		if !ok {
			WriteError(w, http.StatusBadRequest, "INVALID_RANGE",
				"limit must be a positive whole number and offset must not be negative.", nil)
			return
		}

		// The table name is not validated here, and that is deliberate:
		// the only honest check is "does the browse's own role see a table
		// with this name", which lives in the adapter and answers
		// domain.ErrNotFound. A regexp here would be a second, weaker rule
		// that could drift from the real one.
		table := chi.URLParam(r, "table")

		page, err := deps.AdminBrowse.Rows(r.Context(), table, limit, offset)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, browseRowsResponse{
			Table:   table,
			Columns: browseColumns(page.Columns),
			Rows:    page.Rows,
			Total:   page.Total,
			Limit:   page.Limit,
			Offset:  page.Offset,
		})
	}
}
```

- [ ] **Step 4: Map the two new sentinels**

In `api/internal/adapter/http/errors.go`, add beside the
`usecase.ErrOutboxUnavailable` case:

```go
	case errors.Is(err, usecase.ErrBrowseUnavailable):
		// 503 rather than 502: unlike the mail inspector, the failure is not
		// upstream of this service in another process -- it is this
		// install's own second connection, and the advice is "look at
		// DATABASE_READONLY_URL and the hearth_readonly role", not "look at
		// something else". Deliberately a different code from
		// DB_BROWSE_NOT_CONFIGURED: "no value set" and "the value is set and
		// the connection is broken" send the operator to different places.
		WriteError(w, http.StatusServiceUnavailable, "DB_BROWSE_UNAVAILABLE",
			"The database browse cannot reach its read-only connection.", nil)
	case errors.Is(err, usecase.ErrInvalidOffset):
		// Defence in depth: the handler already refuses a negative offset
		// with the same code, and this covers any other caller of the
		// service.
		WriteError(w, http.StatusBadRequest, "INVALID_RANGE",
			"offset must not be negative.", nil)
```

- [ ] **Step 5: Add the Deps field and the routes**

In `api/internal/adapter/http/router.go`, add to `Deps` beside `AdminOutbox`:

```go
	// AdminBrowse is the operator's read-only database browse. Nil when
	// DATABASE_READONLY_URL is unset, which is what makes the two /admin/db
	// routes answer 503 and name the variable; the routes are registered
	// either way, so the route tree never changes with configuration and
	// every test builds the same one.
	AdminBrowse *usecase.AdminBrowseService
```

And register the routes inside the existing `granted` group, immediately
after the two mail routes and **inside** the same `adm.Group(func(granted
chi.Router) {...})` — not a new `adm.Route("/db", ...)`, which would sit
outside `requireAdminGrant`:

```go
					// The operator's database browse: two reads. See
					// admin_browse_handlers.go.
					granted.Get("/db/tables", handleAdminDatabaseTables(deps))
					granted.Get("/db/tables/{table}", handleAdminDatabaseRows(deps))
```

Do **not** add another `requireCSRF`: it already sits at the `/admin` subtree
root, and GET is exempt inside it.

- [ ] **Step 6: Extend the two route-coverage lists**

`admin_api_test.go` enumerates the admin routes explicitly in
`TestAdminRoutesAre404ToANonAdmin` and `TestAdminRoutesNeedAGrant`. Both stay
green without the new routes, so nothing fails — the coverage just silently
stops being complete. Add to each, beside the mail lines:

```go
	rec = env.authedGet(t, "/api/v1/admin/db/tables", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
	rec = env.authedGet(t, "/api/v1/admin/db/tables/accounts", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
```

and, in the grant test, the same two paths expecting
`http.StatusUnauthorized, "ADMIN_REAUTH_REQUIRED"`.

- [ ] **Step 7: Wire it in `main.go`**

In `api/cmd/api/main.go`, immediately after `defer db.Close()` — before the
repositories, so a misconfiguration is caught early rather than reading as a
failure deep in wiring:

```go
	// Nil unless DATABASE_READONLY_URL is set. httpadapter.Deps.AdminBrowse
	// being nil is what makes the two /admin/db routes answer 503 and name
	// the variable; the routes themselves are registered either way.
	//
	// The three outcomes are deliberately not the same. A value that cannot
	// be parsed, or one that connects as a role which may write, refuses the
	// boot: both are things a human typed and no retry fixes them, and
	// serving a "read-only" browse through a writable connection is worse
	// than serving nothing. A database that is merely unreachable does NOT
	// refuse the boot -- the day that happens is the day someone is
	// restoring this product onto a fresh box from the paper key, with the
	// variable already in .env and the role not created yet, and taking the
	// whole household product down over an operator panel would invert the
	// very promise the read-only role exists to keep.
	var adminBrowseSvc *usecase.AdminBrowseService
	if cfg.BrowseEnabled() {
		readonlyDB, err := postgres.OpenReadOnly(ctx, cfg.DatabaseReadonlyURL)
		switch {
		case err == nil:
			defer readonlyDB.Close()
			adminBrowseSvc = usecase.NewAdminBrowseService(postgres.NewBrowseRepo(readonlyDB))
		case errors.Is(err, postgres.ErrReadOnlyMisconfigured):
			return err
		default:
			// No DSN in the message: a Postgres URL carries a password, and
			// credentials never go to a log (see logStartupAddresses).
			slog.Error("the database browse is unavailable; the rest of Hearth is unaffected", "error", err)
		}
	}
```

`errors` and `slog` are both likely imported already; add either only if it
is absent. Add the compile-time assertion beside the existing one near line
37:

```go
var _ usecase.DatabaseBrowser = (*postgres.BrowseRepo)(nil)
```

Add `AdminBrowse: adminBrowseSvc,` to the `Deps` literal, beside
`AdminOutbox`. And log that it is on, beside the outbox line — a boolean, not
the DSN:

```go
	if adminBrowseSvc != nil {
		slog.Info("database browse enabled")
	}
```

Note the predicate: `adminBrowseSvc != nil`, not `cfg.BrowseEnabled()`. They
differ in exactly the case that matters — configured but unreachable — and
logging "enabled" there would contradict the error logged three lines above.

- [ ] **Step 8: Run everything and watch it pass**

```bash
cd api && go test ./internal/adapter/http/ -count=1
cd api && go test ./... -count=1 -timeout=10m
make lint
```

Expected: PASS, and `make lint` exit 0.

- [ ] **Step 9: Mutation-check the not-configured answer**

Change `writeBrowseUnconfigured` to answer `http.StatusNotFound` with
`"NOT_FOUND"`. Re-run. Expected: **FAIL** on
`TestAdminDatabaseAnswers503WhenUnconfigured`. Restore.

Then delete the `deps.AdminBrowse == nil` check from
`handleAdminDatabaseRows` only. Re-run. Expected: **FAIL** — a nil-pointer
panic recovered by `recoverer` into a 500, on the rows test alone. That
asymmetry is the check: it proves the two handlers are guarded separately and
that the list handler's guard was not doing the work for both. Restore.

- [ ] **Step 10: Commit**

```bash
git add api/internal/adapter/http/ api/cmd/api/main.go
git commit -m "feat(admin): GET /admin/db/tables and one page of one table"
```

---

### Task 8: The frontend schemas and hooks

**Files:**
- Create: `web/src/features/admin/adminDatabaseSchemas.ts`
- Create: `web/src/features/admin/browseLimits.ts`
- Create: `web/src/features/admin/useAdminDatabase.ts`
- Test: covered by Task 9's page test, which stubs these exact paths

**Interfaces:**
- Consumes: the JSON shapes from Task 7.
- Produces: `adminDatabaseTablesSchema`, `adminDatabaseRowsSchema` and their
  inferred types; `useAdminDatabaseTables()`, `useAdminDatabaseRows(table,
  limit, offset)`, the exported path builders `adminDatabaseTablesPath()` and
  `adminDatabaseRowsPath(table, limit, offset)`, and the constants
  `BROWSE_DEFAULT_LIMIT` and `BROWSE_MAX_LIMIT`. Task 9's page and its test
  both import the path builders — the test's stub keys and the hook must agree
  byte for byte.

- [ ] **Step 1: Write the schemas**

Create `web/src/features/admin/adminDatabaseSchemas.ts`:

```ts
// Zod mirrors of the DTOs in api/internal/adapter/http/
// admin_browse_handlers.go -- the adminOutboxSchemas.ts convention: follow
// the backend's own structs, not a guess at the shape.
//
// Every object is .strict(), and here that is load-bearing rather than tidy.
// This is the one screen in Hearth that renders arbitrary table contents, so
// a field the frontend did not expect must fail the parse rather than reach
// a page that will happily print whatever it is handed.
import { z } from "zod";

export const adminDatabaseColumnSchema = z
  .object({
    name: z.string(),
    dataType: z.string(),
    // True when the value is withheld rather than absent. The screen must
    // show the difference; see the legend in AdminDatabasePage.
    redacted: z.boolean(),
  })
  .strict();
export type AdminDatabaseColumn = z.infer<typeof adminDatabaseColumnSchema>;

export const adminDatabaseTableSchema = z
  .object({
    name: z.string(),
    rowCount: z.number().int(),
    columns: z.array(adminDatabaseColumnSchema),
  })
  .strict();
export type AdminDatabaseTable = z.infer<typeof adminDatabaseTableSchema>;

export const adminDatabaseTablesSchema = z
  .object({ tables: z.array(adminDatabaseTableSchema) })
  .strict();
export type AdminDatabaseTables = z.infer<typeof adminDatabaseTablesSchema>;

export const adminDatabaseRowsSchema = z
  .object({
    table: z.string(),
    columns: z.array(adminDatabaseColumnSchema),
    // Column-ordered text, parallel to columns. Never objects: a table may
    // carry two columns whose names collide as JSON keys.
    rows: z.array(z.array(z.string())),
    total: z.number().int(),
    limit: z.number().int(),
    offset: z.number().int(),
  })
  .strict();
export type AdminDatabaseRows = z.infer<typeof adminDatabaseRowsSchema>;
```

- [ ] **Step 2: Write the limits leaf module**

Create `web/src/features/admin/browseLimits.ts`:

```ts
// The service's own clamps, mirrored so the page can say "50 rows a page"
// without a round trip, and so router.tsx's validateSearch can bound the
// offset a URL asks for. They must match usecase/admin_browse.go's
// BrowseDefaultLimit and BrowseMaxLimit.
//
// Its own module, not a constant in useAdminDatabase.ts, because router.tsx
// imports them and router.tsx may never statically import a hook file: that
// would pull the admin query hooks into the main bundle, which
// adminBundleSplit.test.ts exists to prevent. directoryLimits.ts is the same
// module for the same reason.
export const BROWSE_DEFAULT_LIMIT = 50;
export const BROWSE_MAX_LIMIT = 100;
```

- [ ] **Step 3: Write the hooks**

Create `web/src/features/admin/useAdminDatabase.ts`:

```ts
// Query hooks over the database browse routes (api/internal/adapter/http/
// admin_browse_handlers.go). Same shape as useAdminOutbox.ts, same two rules:
// refetchOnWindowFocus is off because every request under /admin is an audit
// row -- and on this screen an audit row is the record that someone read a
// household's money -- and a lapsed grant is not handled here, it is routed
// to the one AdminGate AdminShell owns.
//
// Retries are off globally (main.tsx sets retry: false) and neither hook sets
// its own. A retried 503 would be four audit rows per failed page load.
import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import {
  adminDatabaseRowsSchema,
  adminDatabaseTablesSchema,
  type AdminDatabaseRows,
  type AdminDatabaseTables,
} from "./adminDatabaseSchemas";

// Re-exported so a reader looking for the limits finds them beside the hooks
// that use them. They are declared in browseLimits.ts because router.tsx
// needs them for validateSearch and may never statically import a hook file
// -- adminBundleSplit.test.ts walks main.tsx's import graph and fails if any
// admin hook becomes reachable from it. directoryLimits.ts exists for exactly
// this reason.
export { BROWSE_DEFAULT_LIMIT, BROWSE_MAX_LIMIT } from "./browseLimits";

export function adminDatabaseTablesPath(): string {
  return "/api/v1/admin/db/tables";
}

export function adminDatabaseRowsPath(
  table: string,
  limit: number,
  offset: number,
): string {
  return `/api/v1/admin/db/tables/${encodeURIComponent(table)}?limit=${String(limit)}&offset=${String(offset)}`;
}

export function adminDatabaseTablesKey() {
  return ["admin", "database", "tables"] as const;
}

export function adminDatabaseRowsKey(
  table: string,
  limit: number,
  offset: number,
) {
  return ["admin", "database", "rows", { table, limit, offset }] as const;
}

async function fetchTables(): Promise<AdminDatabaseTables> {
  const body = await apiFetch<unknown>(adminDatabaseTablesPath());
  return adminDatabaseTablesSchema.parse(body);
}

async function fetchRows(
  table: string,
  limit: number,
  offset: number,
): Promise<AdminDatabaseRows> {
  const body = await apiFetch<unknown>(
    adminDatabaseRowsPath(table, limit, offset),
  );
  return adminDatabaseRowsSchema.parse(body);
}

export function useAdminDatabaseTables() {
  return useQuery({
    queryKey: adminDatabaseTablesKey(),
    queryFn: fetchTables,
    refetchOnWindowFocus: false,
  });
}

export function useAdminDatabaseRows(
  table: string,
  limit: number,
  offset: number,
) {
  return useQuery({
    queryKey: adminDatabaseRowsKey(table, limit, offset),
    queryFn: () => fetchRows(table, limit, offset),
    refetchOnWindowFocus: false,
  });
}
```

- [ ] **Step 4: Typecheck**

```bash
cd web && npx tsc --noEmit -p tsconfig.json
```

Expected: no errors. There is no test of its own here: a schema with no
consumer proves nothing, and Task 9's page test exercises both files through
the real fetch stub.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/admin/adminDatabaseSchemas.ts \
        web/src/features/admin/browseLimits.ts \
        web/src/features/admin/useAdminDatabase.ts
git commit -m "feat(web): schemas and query hooks for the database browse"
```

---

### Task 9: `AdminDatabasePage`, the fourth nav link, and the routes

**Files:**
- Create: `web/src/features/admin/AdminDatabasePage.tsx`
- Test: `web/src/features/admin/AdminDatabasePage.test.tsx`
- Modify: `web/src/features/admin/AdminShell.tsx`,
  `web/src/features/admin/AdminShell.test.tsx`,
  `web/src/features/admin/adminBundleSplit.test.ts`,
  `web/src/routes/router.tsx`

**Interfaces:**
- Consumes: Task 8's hooks and types, `useCloseSurfaceOnReauth` and
  `isNotFound` from `useAdminDirectory.ts`, `isAdminLayerFailure` from
  `useAdmin.ts`, `PageContainer`.
- Produces: `AdminDatabasePage` (the table list) and `AdminDatabaseTablePage`
  (the row viewer, taking `table`, `limit`, `offset` and two navigation
  callbacks as props), both named exports from one file.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/admin/AdminDatabasePage.test.tsx`, following
`AdminMailPage.test.tsx` exactly — `renderWithRouter` plus `stubFetchRoutes`
for every request, literal strings asserted:

```tsx
// Follows AdminMailPage.test.tsx: renderWithRouter plus stubFetchRoutes for
// every request, literal strings asserted.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AdminDatabasePage, AdminDatabaseTablePage } from "./AdminDatabasePage";
import {
  adminDatabaseRowsPath,
  adminDatabaseTablesPath,
  BROWSE_DEFAULT_LIMIT,
} from "./useAdminDatabase";
import type {
  AdminDatabaseRows,
  AdminDatabaseTables,
} from "./adminDatabaseSchemas";
```

Write these cases in full:

1. **The table list renders a name and its row count.**
2. **The not-configured copy names the variable.** Stub the tables path with
   a 503 carrying `DB_BROWSE_NOT_CONFIGURED`; assert the screen shows
   `DATABASE_READONLY_URL` in as many words. Match on `error.code`, never on
   `error.status === 503` — the two 503s mean different things.
3. **The broken-connection copy is different from the not-configured copy.**
   Stub `DB_BROWSE_UNAVAILABLE`; assert the two messages are not the same
   string. A single "unavailable" message for both would send the operator to
   the wrong place.
4. **A redacted cell renders the marker and its column header says so.** Stub
   a page whose `token_hash` column has `redacted: true` and whose cell is
   `«redacted»`; assert both the cell text and that the header is marked.
5. **The legend explains both markers.** Assert `«redacted»` and `«null»`
   both appear in the legend, because a reader who has never seen either has
   no way to tell a withheld value from an absent one.
6. **Next and Previous move the offset.** Assert `onPage` is called with the
   next offset, and that Previous is disabled at offset 0 and Next is
   disabled on the last page.
7. **An unknown table shows "no such table" and does not close the surface.**
   Stub a 404; assert the page's own message renders and that AdminGate's
   password prompt does not. `isNotFound(query.error)` must be checked
   **before** `isAdminLayerFailure`, which includes `NOT_FOUND` and would
   otherwise take down the whole operator surface.

- [ ] **Step 2: Run it and watch it fail**

```bash
cd web && npx vitest run src/features/admin/AdminDatabasePage.test.tsx
```

Expected: FAIL — the module does not exist.

- [ ] **Step 3: Write the page**

Create `web/src/features/admin/AdminDatabasePage.tsx`, with two named exports
in one file, the way `AdminMailPage.tsx` carries its list and its detail.

Requirements, each with its reason:

- **`AdminDatabasePage`** lists tables: name, row count, column count. Each
  name is a `<Link to="/admin/database/$table">`.
- **`AdminDatabaseTablePage`** takes `{ table, limit, offset, onPage }` as
  props and holds no URL state of its own — the route owns it, the way
  `AdminHouseholdsPage` does. Reload, Back and the audit row then all agree
  on what was shown, which on this screen is the difference between an audit
  log that records what was read and one that records that something was.
- **The rows table scrolls inside its own container**: wrap it in
  `overflow-x-auto`. The page body must never scroll sideways — a browsed
  table can have thirty columns, and the walk checks 305px.
- **The two panes stack below `md`.** A two-column layout cannot hold at
  305px.
- **The legend** sits above the table and names both markers in one line:
  `«redacted»` is a value you may not see, `«null»` is a value that is not
  there. A redacted column's header carries the same word, so the two facts
  are visible together.
- **Unavailability copy** is matched on `error.code`, following
  `outboxErrorCopy` in `AdminMailPage.tsx`:
  `DB_BROWSE_NOT_CONFIGURED` names the variable and says the panel is off on
  this install; `DB_BROWSE_UNAVAILABLE` says the connection is broken and the
  data is not lost. Two distinct strings.
- **`isNotFound(query.error)` is checked before `isAdminLayerFailure`**, with
  a comment saying why — `isAdminLayerFailure` includes `NOT_FOUND`, and
  checking it first would close the entire operator surface through AdminGate
  when all that happened is a mistyped table name.
- **`useCloseSurfaceOnReauth(query.error)` on both page-level queries**, as
  every other admin page does.
- **No `dangerouslySetInnerHTML` anywhere.** Cell values are arbitrary
  database content; they are rendered as text and nothing else.

- [ ] **Step 4: Add the fourth nav link**

In `AdminShell.tsx`, add to `items`:

```tsx
    { to: "/admin/database", label: "Database" },
```

Place it last, after Households, so the existing three keep the order the
previous walk confirmed. Change nothing else about the nav: the `flex-wrap`
on the nav is the fix for a real 305px overflow found when the third link was
added, and this task adds the fourth.

In `AdminShell.test.tsx`, update the label assertion — this change breaks it,
and that is the test doing its job:

```tsx
    expect(labels).toEqual([
      "Flags",
      "Mail",
      "Households",
      "Database",
      "Back to Hearth",
    ]);
```

- [ ] **Step 5: Register the routes**

In `web/src/routes/router.tsx`:

Two lazy imports beside the others:

```tsx
const LazyAdminDatabasePage = lazy(() =>
  import("../features/admin/AdminDatabasePage").then((m) => ({
    default: m.AdminDatabasePage,
  })),
);
const LazyAdminDatabaseTablePage = lazy(() =>
  import("../features/admin/AdminDatabasePage").then((m) => ({
    default: m.AdminDatabaseTablePage,
  })),
);
```

The list route, with no URL state, following `adminMailRoute`. The table
route, holding `limit` and `offset` in the URL, following
`adminHouseholdsRoute` — `validateSearch`, then `useSearch({ from:
"/authenticated/admin/database/$table" })` and `useNavigate` inside a **named**
function component (an inline arrow is refused by
`eslint-plugin-react-hooks`), with the Suspense fallback inlined the way every
other admin route inlines it:

```tsx
// The row viewer keeps its page in the URL, so reload, Back and the audit
// row all agree on what was shown -- the households list's own reasoning,
// and it matters more here: this route's audit row is the record that
// somebody read a particular page of a particular table.
const adminDatabaseTableRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "database/$table",
  validateSearch: (
    search: Record<string, unknown>,
  ): { limit: number; offset: number } => ({
    limit:
      typeof search.limit === "number" &&
      Number.isInteger(search.limit) &&
      search.limit > 0
        ? Math.min(search.limit, BROWSE_MAX_LIMIT)
        : BROWSE_DEFAULT_LIMIT,
    offset:
      typeof search.offset === "number" &&
      Number.isInteger(search.offset) &&
      search.offset >= 0
        ? search.offset
        : 0,
  }),
  component: function AdminDatabaseTableRouteComponent() {
    // ...useParams, useSearch, useNavigate, Suspense, LazyAdminDatabaseTablePage
  },
});
```

Add both to `adminRoute.addChildren([...])`, and update the route-shape
header comment at the top of the file — that block is the map other engineers
read.

- [ ] **Step 6: Extend the bundle-split test**

`adminBundleSplit.test.ts` walks `main.tsx`'s static import graph and asserts
no admin file is reachable from it. Add the two new files:

```ts
    expect(reachable).not.toContain("features/admin/AdminDatabasePage.tsx");
    expect(reachable).not.toContain("features/admin/useAdminDatabase.ts");
```

`router.tsx` imports `BROWSE_MAX_LIMIT` and `BROWSE_DEFAULT_LIMIT` from
`browseLimits.ts` (Task 8), never from `useAdminDatabase.ts` — a hook file
reachable from `router.tsx` would put the admin query hooks in the main
bundle, which is what this test exists to prevent. Check how
`directoryLimits.ts` is treated in this file and match it exactly.

- [ ] **Step 7: Run the suites and watch them pass**

```bash
cd web && npx vitest run
cd web && npx tsc --noEmit -p tsconfig.json && npx eslint .
```

Expected: PASS, including the updated `AdminShell.test.tsx` and
`adminBundleSplit.test.ts`.

- [ ] **Step 8: Mutation-check the not-found ordering**

In `AdminDatabaseTablePage`, move the `isNotFound(query.error)` check to
**after** the `isAdminLayerFailure` branch. Re-run. Expected: **FAIL** on
case 7 — a mistyped table name now closes the whole operator surface. That is
the defect the ordering exists to prevent, and it is invisible in any test
that only checks the happy path. Restore.

Then change the two unavailability strings to the same text. Re-run.
Expected: **FAIL** on case 3. Restore.

- [ ] **Step 9: Commit**

```bash
git add web/src/features/admin/ web/src/routes/router.tsx
git commit -m "feat(web): the operator's database browse screen"
```

---

### Task 10: The documents

Part of the work, not a tidy-up after it. Do this before the walk, so the
walk's own findings land in a file that already exists.

**Files:**
- Modify: `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`,
  `docs/LEARNING.md`, `docs/adr/0005-platform-admin-authorization.md`,
  `docs/INFRASTRUCTURE.md`, `docs/ADMIN_SURFACE_HANDOVER.md`,
  `deploy/PROVISION.md`, `deploy/README.md`, `deploy/.env.example`

- [ ] **Step 1: `docs/SYSTEM_DESIGN.md`**

Use the `maintaining-system-design` skill. Four things changed structurally
and all four belong in a diagram, not only in prose: a **second connection
pool** (a new edge in the component diagram, not just a new box), the
`DatabaseBrowser` port, the `BrowseRepo` adapter, and two routes on the
`/admin` subtree. Change the prose under each diagram too — that is where the
non-obvious reasoning lives, and here the non-obvious part is that the second
pool exists to make a mistake in the adapter's SQL harmless.

- [ ] **Step 2: `deploy/.env.example`**

Add beside `MAILPIT_API_URL`, following that entry's shape:

```bash
# The SELECT-only role the operator's database browse reads through. Created
# by deploy/readonly-role.sql during provisioning -- roles are cluster-level,
# so this is NOT created by a migration and is NOT in any backup. Leave it
# unset and the browse says it is not configured; there is no fallback to
# DATABASE_URL. Must match the password set in PROVISION.md section 9.
DATABASE_READONLY_URL=postgres://hearth_readonly:CHANGEME@postgres:5432/hearth?sslmode=disable
```

No change to `deploy/docker-compose.prod.yml`: `api` already declares
`env_file: [.env]`.

- [ ] **Step 3: `deploy/PROVISION.md`**

Add a new numbered section between `## 8 · `.env`` and `## 9 · First
bring-up`, renumbering the sections after it. **Preserve the U+00B7 middle
dot** and its single spaces exactly — `## 9 · The read-only role`.

The step is: generate a password with `openssl rand -hex 24` (hex, not
base64, for the same reason section 8 gives — it goes into a `postgres://`
userinfo field where `/`, `+` and `=` are all significant), put it in
`DATABASE_READONLY_URL` in `.env`, and run:

```bash
docker compose -f docker-compose.prod.yml exec -T postgres \
  psql -U hearth -d hearth -v ON_ERROR_STOP=1 < readonly-role.sql
docker compose -f docker-compose.prod.yml exec -T postgres \
  psql -U hearth -d hearth -c "ALTER ROLE hearth_readonly PASSWORD '<the generated password>'"
```

Say plainly that the role must exist **before** `DATABASE_READONLY_URL` is
set, or the API logs that the browse is unavailable until it does — and that
this is deliberately not fatal, so the order is a convenience rather than a
requirement.

Also fix section 7's `ls -A` comment: it enumerates `deploy/`'s contents and
now omits `readonly-role.sql`. That comment goes stale the moment this file is
added, and a stale enumeration is how someone concludes their sparse checkout
is broken.

- [ ] **Step 4: `deploy/README.md`**

Two sections:

- **Break-glass** gains the browse as the place a read starts now. Every
  command in that section today is a mutation; reading a row on the box is
  not covered there at all, which is the gap this feature closes. Keep
  `docker compose exec postgres psql` as the fallback for the questions the
  browse cannot answer — no filters, no joins, no free SQL.
- **Restoring** gains the role. `backup.sh` dumps one database with
  `--no-privileges`, and roles are cluster-level, so **neither the role nor
  its grants are in any backup this product takes**. After restoring into a
  database, run `readonly-role.sql` against it again. It is idempotent, so
  "run it after every restore" is the whole instruction.

- [ ] **Step 5: `docs/INFRASTRUCTURE.md`**

The "Known gaps" bullet already says this panel "will cost one new value here
when it ships". **Edit that bullet rather than appending a new one** — this
work is what makes its promise true. It must now say: `DATABASE_READONLY_URL`
is real; unset means the browse says so and never falls back to the
read-write pool; a value that cannot be parsed, or one that connects as a
role which can write, refuses the boot; a database that is merely unreachable
does not; and the role is **not in any backup**, so a restore re-runs
`readonly-role.sql`.

Add a row to the credentials table for the production `hearth_readonly`
password: it lives in `deploy/.env` on the box, mode 600, and is recoverable
— it only talks to the existing database, and `ALTER ROLE` sets a new one.

- [ ] **Step 6: `docs/adr/0005-platform-admin-authorization.md` — amended,
      not superseded**

Its own "Revisit this when" names this feature as the moment the "mutations
stay on the CLI, reads move to the web" narrowing is first tested against a
real database read rather than a flag toggle. Amend it with what held and
what did not. At minimum:

- The narrowing held: no write reaches the web, and the guard is Postgres's
  rather than this codebase's.
- What the ADR did not anticipate: the read-only role is cluster-level and
  therefore outside both the migration path and the backup, which makes the
  first genuinely *infrastructural* dependency in the admin surface.
- The accepted cost restated in its sharpest form: a re-authenticated admin
  session can now read every household's finances, one page at a time, with
  an audit row per page.

- [ ] **Step 7: `docs/FEATURE_TRACKER.md`**

Change the `Read-only database browse` row from ⬜ to ✅ — or 🟡 with the gap
named in the row, which is worth more than a bare ⬜. Say what shipped in the
row's own prose, including the two things the spec did not have: the default
privileges, and the degrade-rather-than-refuse decision.

**Recount the summary table from the symbols in §9**, do not adjust it by a
delta. The table's columns must sum to the stated totals.

Update "Suggested order": item 4 was the last of the four, so the operator
surface is complete and the next work is Marriage's Agreements.

- [ ] **Step 8: `docs/ADMIN_SURFACE_HANDOVER.md`**

§2 gains the feature, §3 loses it and becomes empty. Say so in as many words:
the operator surface is finished. Note the one thing that is finished in code
and unfinished in the world — production ships with the variable unset, by
the product owner's decision on 2026-09-04, so the panel is off until the
operator runs PROVISION.md's new section.

- [ ] **Step 9: `docs/LEARNING.md`**

One entry per thing worth remembering, in the pattern it belongs to rather
than a new section where an existing one fits.

Two are worth writing whether or not anyone trips them, because both are
invisible until long after the change that caused them:

- **`GRANT SELECT ON ALL TABLES` grants on the tables that exist when it
  runs.** Without `ALTER DEFAULT PRIVILEGES`, the next migration's table is
  invisible to the browse and nothing anywhere reports it — no error, no log
  line, just a table missing from a list nobody counts. This belongs with the
  other "configuration that lies" evidence.
- **A read-only role is not in the backup.** `pg_dump` is one database;
  roles are cluster-level; `--no-privileges` drops the grants as well. A
  restore that appears complete leaves the browse broken, and the failure
  arrives days later.

Add anything the build itself taught, and anything the walk finds in Task 11.

- [ ] **Step 10: Commit**

```bash
git add docs/ deploy/
git commit -m "docs: the database browse, and the two traps it taught"
```

---

### Task 11: The browser walk

Tests passing is not the claim. The product owner asked for this explicitly
after a feature that verified 15 of 15 still surprised them in first-run use.

**Files:**
- Create: `docs/superpowers/plans/2026-09-04-hearth-database-browse-verification.md`

Record it criterion by criterion, with what you saw, not only pass or fail.
A criterion met by an interpreted rather than literal path is recorded as
such, never passed over quietly.

- [ ] **Step 1: Bring the stack up clean**

```bash
make down && make dev && make seed
```

Check which engine is serving: `lsof -i :5173`. Two Docker engines exist on
this machine and both can host this stack.

Sign in as `andreas@hearth.family` / `hearth-dev-password`, then **Admin** in
the sidebar, then the password again at the re-auth prompt.

- [ ] **Step 2: Walk the fifteen criteria**

1. **The nav shows four links** — Flags, Mail, Households, Database — and
   Database is the active one on `/admin/database`.
2. **The table list renders** with every table in the schema, including
   `admin_audit_log` and `goose_db_version`, each with a row count. Spot-check
   one count against `make psql` and `select count(*)`. **Record the request's
   wall-clock time** from devtools' Network tab: this page counts every table
   in one statement under a three-second cap, and decision 11's "revisit when
   it times out" clause needs a baseline number rather than a guess about how
   much headroom is left.
3. **Opening `accounts`** shows its columns and its rows, with the figures
   matching what `psql` shows for the same rows.
4. **A money column is readable.** This is the whole point and the whole cost:
   confirm you can see a household's real amounts, and that getting there took
   a re-authentication.
5. **`sessions` renders `token_hash` as `«redacted»`**, and its column header
   says the column is withheld.
6. **The redaction is real, not cosmetic.** With devtools' Network tab open,
   reload that page and read the JSON response: it must contain no hex, no
   `\x`, nothing but the marker. A screen that renders a marker over a value
   it was still sent is a different feature from the one specified.
7. **A `NULL` renders `«null»`** and is distinguishable from an empty string.
   `users.password_hash` is `NULL` for a magic-link-only member; seed one if
   the seeded household has none.
8. **The legend explains both markers** and is readable without hovering
   anything.
9. **Paging works and does not repeat a row.** Page through a table with more
   rows than one page, and check that no row appears twice. Note the URL: it
   must carry the offset, so Back and reload agree with what is on screen.
10. **Previous is disabled on the first page**; Next is disabled on the last.
11. **An unknown table says so** — visit `/admin/database/nope` directly — and
    the operator surface stays open. The password prompt must not appear.
12. **Every page view leaves an audit row.** `select count(*) from
    admin_audit_log` before and after one page view; the difference is
    exactly one. Read the newest row and confirm it names the table and the
    offset.
13. **The role cannot write.** Not a click — run it:
    `docker compose exec postgres psql -U hearth_readonly -d hearth -c "insert
    into households (id, name) values (gen_random_uuid(), 'nope')"`. Record
    the exact refusal. A criterion that cannot be clicked is still a
    criterion, and this is the one the whole design rests on.
14. **The not-configured state.** Stop the API, start it with
    `DATABASE_READONLY_URL` unset, and confirm: the rest of Hearth works, the
    Database screen says the panel is not configured, and it names the
    variable. This is the state production will actually be in.
15. **Four widths, both screens** — 305px, 360px, 768px, 1440px. No
    horizontal scroll on the body at any of them; a wide table scrolls inside
    its own container; the nav wraps rather than overflowing now that it
    carries a fourth link. Screenshot each.

- [ ] **Step 3: Fix what the walk finds, in the walk**

A defect found here is fixed here and pinned by a mutation-checked test, the
way the last three features' walks did. Then sweep for the class rather than
the instance — if the fix is a shape (`AdminBudgetPage` had the same 403 copy
bug as `AdminBillsPage`), grep for its siblings before closing it.

- [ ] **Step 4: Record it and commit**

```bash
git add docs/superpowers/plans/2026-09-04-hearth-database-browse-verification.md
git commit -m "docs: the database browse's browser walk, criterion by criterion"
```

- [ ] **Step 5: The final check**

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make lint && make test
git status
```

`git status` before you push: a file created and never `git add`ed is present
for every local check and absent from the commit.

---

## Differences from the spec, found while writing this plan

The spec wins on design. Where the plan departs from its letter, it is
because the letter could not be implemented as written, and each of these is
worth carrying back into the spec rather than discovering twice.

| The spec says | This plan does | Why |
|---|---|---|
| §7: a set-but-unparseable `DATABASE_READONLY_URL` "refuses the boot ... as `config.go` already refuses a half-set Telegram pair" | `config.Load` records the string; `postgres.OpenReadOnly` refuses it | `config.go` imports the standard library only, and `net/url` cannot tell a broken DSN from a legal keyword/value one. The outcome — the API does not start — is unchanged. Task 6 |
| §4.2: `RowPage.Columns []string` | `RowPage.Columns []ColumnInfo` | The row viewer has to mark a redacted column's header, and a bare name cannot carry that. Task 3 |
| Decision 5's three consumers | Four call sites: the two Make targets that bypass `api`'s `depends_on` needed closing too | `make migrate` is `run --rm migrate` and `make dev-local` runs the API natively, so neither evaluates `api`'s dependencies. Without this, a developer's boot self-check fails on a role that was never created. Task 1 |
| Decision 12: unreachable at boot answers `503` | Same, plus a `postgres.ErrReadOnlyMisconfigured` sentinel so `main.go` can tell the two apart | "Refuse on a typo, degrade on an outage" needs the two failures to be distinguishable in code, not only in prose. Tasks 4 and 7 |
| Silent on the audit row's contents | No audit change at all; the existing middleware already records the path and the query string | Confirmed by reading `auditAdmin`: it writes before chi matches the route, so the table name (a path segment) and the offset (a query parameter) are already there. Task 7 |

---

## Self-review

Run against the spec after the plan was written.

**Spec coverage.** Every section has a task: §1 and §2 need no code; decision
1 is Tasks 1 and 4; decision 2 Task 1, tested in Task 5; decision 3 Task 1 and
Task 4; decision 4 Task 4; decision 5 Task 1; decision 6 Task 5; decision 7
Task 5; decision 8 Task 2; decision 9 Tasks 2 and 5; decision 10 Task 5;
decision 11 Task 5; decision 12 Tasks 4, 6 and 7; decision 13 Task 7;
decision 14 Task 4; decisions 15 and 16 are "do not build", carried as the
absence of a task and named in §2 of the spec; decision 17 Task 7; decision 18
Task 5. §5 is Task 1; §6 Task 7; §7 Task 6; §8 Task 9; §9 spread across every
task's own test step; §10 Task 10; §11 Task 11.

**Placeholders.** Task 7's test file and Task 9's page are the two places
where a shape is described rather than written out in full. Both are
deliberate and both name the file to copy, line by line, because the existing
file is longer than a plan should inline and copying it wrong is the failure
mode a paraphrase would cause. Every other code step carries the code.

**Type consistency.** `usecase.TableInfo`, `usecase.ColumnInfo` and
`usecase.RowPage` are defined once in Task 3 and used unchanged in Tasks 5, 7
and 8. `postgres.ReadOnlyDB` is produced in Task 4 and consumed in Task 5 and
Task 7. `domain.RedactedCell` and `domain.NullCell` are produced in Task 2 and
consumed in Task 5 and in Task 9's test. `BROWSE_DEFAULT_LIMIT` and
`BROWSE_MAX_LIMIT` mirror `usecase.BrowseDefaultLimit` and
`usecase.BrowseMaxLimit` and are asserted equal by nothing — that is a known,
accepted duplication, the same one `OUTBOX_DEFAULT_LIMIT` already carries, and
Task 8's comment says so.
