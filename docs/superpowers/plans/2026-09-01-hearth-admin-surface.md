# Platform admin surface — implementation plan (rollout steps 1–3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the operator a `/admin` surface, reachable only by a platform
admin who has just re-entered their password, where feature flags can be turned
on and off globally or for one household — with every admin request audited.

**Architecture:** Platform admin is a new authorization axis, orthogonal to
household roles and capabilities: a `platform_admins` row, checked by
`requirePlatformAdmin` middleware that answers 404 (not 403) to everyone else.
Entering `/admin` requires re-authentication, which stamps a 30-minute grant on
the session row. Feature flags are a compile-time registry in `internal/domain`
plus override rows in Postgres, resolved by a pure function and enforced by a
`requireFeature` middleware that returns 404 when a flag is off.

**Tech Stack:** Go 1.24, chi v5, pgx/v5 + sqlc, goose migrations, testcontainers;
React 19 + TypeScript, TanStack Router + Query, Zod, Tailwind, Vitest.

**Spec:** `docs/superpowers/specs/2026-09-01-hearth-admin-surface-design.md`

**Out of scope for this plan** (their own plans, on this foundation): the
read-only database browse (spec §4), the outbound message inspector (§5), and
household metrics (§6).

## Global Constraints

- **Clean architecture, enforced by `make lint-arch`.** `internal/domain`
  imports only the standard library. `internal/usecase` may add
  `internal/domain`. Everything else is `internal/adapter/**` or `cmd/**`. No
  database, HTTP or third-party type crosses out of the adapter layer. This
  holds in test files too.
- **Authorisation exists only in the HTTP layer.** No service takes an actor
  parameter *for a permission decision*. Where `actorUserID` appears in a
  service signature below it is written to an audit or `updated_by` column and
  never consulted to decide whether something is allowed — each such parameter
  carries a doc comment saying so.
- **Every 2xx except 204 carries a JSON body.** `apiFetch` throws on an ok
  response it cannot parse.
- **Fail closed on values you did not construct.** Every `switch` over a value
  arriving from a database column or a request needs a `default` that refuses.
- **Comments say why, never what.** Exported things carry their contract in a
  doc comment; `usecase/ports.go` is the model.
- **Environment.** `go` is not on `PATH` by default on this machine:
  `export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH`. The Go
  suite needs Docker: `export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock`
  and `export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`.
- **Definition of done** (per task): `make lint && make test` green, and the
  task's own new test mutation-checked — break the implementation on purpose,
  watch the test go red, put it back.

---

### Task 1: The migration

**Files:**
- Create: `api/migrations/00012_admin.sql`
- Test: `api/internal/adapter/postgres/admin_schema_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: tables `platform_admins`, `feature_flags`, `household_feature_flags`,
  `admin_audit_log`, `admin_reauth_attempts`, and column
  `sessions.admin_grant_expires_at timestamptz` (nullable).

- [ ] **Step 1: Write the failing test**

Create `api/internal/adapter/postgres/admin_schema_test.go`. It asserts the two
schema properties that are decisions rather than typing: deleting a user takes
their admin grant with them, and the audit log's defaults let a row be written
with only actor and action.

```go
package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/testsupport"
)

// TestPlatformAdminRowsFollowTheirUser proves the ON DELETE CASCADE: an admin
// grant must not outlive the user it names. A stale row here would be an
// orphan whose user_id could, in principle, be reissued.
func TestPlatformAdminRowsFollowTheirUser(t *testing.T) {
	ctx := context.Background()
	pool := openPool(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, display_name) VALUES ('admin@example.test', 'Admin') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO platform_admins (user_id, note) VALUES ($1, 'the operator')`, userID,
	); err != nil {
		t.Fatalf("insert platform admin: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var remaining int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM platform_admins WHERE user_id = $1`, userID,
	).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("platform_admins rows after deleting the user = %d, want 0", remaining)
	}
}

// TestAuditRowsNeedOnlyAnActorAndAnAction proves the defaults on target,
// detail and ip. auditAdmin writes one row per request from middleware, where
// there is not always a target to name; a NOT NULL with no default would make
// that middleware's simplest case impossible.
func TestAuditRowsNeedOnlyAnActorAndAnAction(t *testing.T) {
	ctx := context.Background()
	pool := openPool(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, display_name) VALUES ('auditor@example.test', 'Auditor') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO admin_audit_log (id, actor_user_id, action) VALUES (gen_random_uuid(), $1, 'GET /admin/flags')`,
		userID,
	); err != nil {
		t.Fatalf("insert audit row: %v", err)
	}

	var target, ip string
	var detail []byte
	if err := pool.QueryRow(ctx,
		`SELECT target, detail, ip FROM admin_audit_log WHERE actor_user_id = $1`, userID,
	).Scan(&target, &detail, &ip); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if target != "" || ip != "" || string(detail) != "{}" {
		t.Fatalf("defaults = target %q, detail %q, ip %q; want empty, {}, empty", target, detail, ip)
	}
}

// openPool starts the shared test Postgres and hands back a raw pool. These
// two tests assert on the schema itself rather than on a repository, so they
// deliberately bypass the postgres package's own types.
func openPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), testsupport.StartPostgres(t))
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
```

- [ ] **Step 2: Run the test and watch it fail**

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
cd api && go test ./internal/adapter/postgres/ -run 'TestPlatformAdminRowsFollowTheirUser|TestAuditRowsNeedOnlyAnActorAndAnAction' -v
```

Expected: FAIL — `relation "platform_admins" does not exist`.

- [ ] **Step 3: Write the migration**

Create `api/migrations/00012_admin.sql`:

```sql
-- +goose Up

-- Platform admin is a different axis from household role. A member's Role
-- (owner/limited) and Capabilities answer "what may this person do inside
-- their own household"; this table answers "who runs this install". The two
-- must never be expressed in terms of each other, which is why this is a
-- table of its own rather than a column on memberships.
--
-- Rows are created only by adminctl, never over HTTP: an admin surface that
-- can mint its own admins turns one stolen session into permanent access.
CREATE TABLE platform_admins (
    user_id    uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    note       text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- The re-auth grant. The session cookie lives 30 days, which is right for a
-- household member and far too long for a surface that can read every
-- household's data, so entering /admin costs the password again and stamps a
-- short expiry here.
--
-- On the session row rather than in a second cookie: sign-out already revokes
-- the session, so the grant dies with it for free, and there is no second
-- cookie whose Secure/SameSite/HttpOnly flags could be got wrong independently
-- of the first.
ALTER TABLE sessions ADD COLUMN admin_grant_expires_at timestamptz;

-- Feature flag overrides. The registry of flags is compile-time
-- (domain.AllFlags), so there is deliberately no table of flag definitions and
-- no foreign key on `key`: a row here can outlive the const that named it.
-- Resolution ignores keys domain.ParseFlag refuses, so an orphaned row can
-- never turn anything on -- see domain.ResolveFlags.
--
-- A row exists only where somebody overrode something. An install with no rows
-- at all behaves exactly as AllFlags says.
CREATE TABLE feature_flags (
    key        text PRIMARY KEY,
    enabled    boolean     NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by uuid        REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE household_feature_flags (
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    key          text        NOT NULL,
    enabled      boolean     NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    updated_by   uuid        REFERENCES users(id) ON DELETE SET NULL,
    PRIMARY KEY (household_id, key)
);

-- Append-only. No delete route exists anywhere in the product and adminctl
-- prune does not touch this table: a log the operator can edit stops meaning
-- anything the moment the operator makes a mistake.
--
-- target, detail and ip default rather than being NOT NULL without one,
-- because auditAdmin writes from middleware where there is not always a target
-- to name. detail records what was looked at, never what was seen -- no
-- passwords, no tokens, no row values.
CREATE TABLE admin_audit_log (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action        text        NOT NULL,
    target        text        NOT NULL DEFAULT '',
    detail        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    ip            text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX admin_audit_log_created_at_idx ON admin_audit_log (created_at DESC);

-- Admin re-auth failures get their own ledger rather than login_attempts,
-- because that table's lockout is HOUSEHOLD-scoped: three failures lock
-- password sign-in for every member. Feeding an operator's mistypes into it
-- would lock their whole household out of the ordinary product as a side
-- effect of fumbling a password on a screen nobody else can see.
--
-- The policy is shared even though the ledger is not: domain.LockoutPolicy
-- evaluates both.
CREATE TABLE admin_reauth_attempts (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    succeeded boolean     NOT NULL,
    at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX admin_reauth_attempts_user_at_idx ON admin_reauth_attempts (user_id, at DESC);

-- +goose Down
DROP TABLE admin_reauth_attempts;
DROP TABLE admin_audit_log;
DROP TABLE household_feature_flags;
DROP TABLE feature_flags;
ALTER TABLE sessions DROP COLUMN admin_grant_expires_at;
DROP TABLE platform_admins;
```

- [ ] **Step 4: Run the test and watch it pass**

```bash
cd api && go test ./internal/adapter/postgres/ -run 'TestPlatformAdminRowsFollowTheirUser|TestAuditRowsNeedOnlyAnActorAndAnAction' -v
```

Expected: PASS, both.

- [ ] **Step 5: Mutation-check it**

Change `ON DELETE CASCADE` to `ON DELETE SET NULL` on `platform_admins.user_id`
— which cannot work against a `PRIMARY KEY`, so instead drop the
`ON DELETE CASCADE` clause entirely. Re-run: the cascade test must fail with a
foreign-key violation on the `DELETE FROM users`. Put the clause back and
confirm green.

- [ ] **Step 6: Commit**

```bash
git add api/migrations/00012_admin.sql api/internal/adapter/postgres/admin_schema_test.go
git commit -m "feat(db): tables for platform admin, feature flags and the audit log"
```

---

### Task 2: The domain — admin identity and the flag registry

**Files:**
- Create: `api/internal/domain/admin.go`
- Create: `api/internal/domain/featureflag.go`
- Modify: `api/internal/domain/errors.go`
- Test: `api/internal/domain/featureflag_test.go`

**Interfaces:**
- Consumes: `domain.LockoutPolicy` (already exists, `domain/lockout.go`).
- Produces:
  - `domain.PlatformAdmin{UserID string; Note string; CreatedAt time.Time}`
  - `domain.Flag` (string), constants `FlagSignupsOpen`, `FlagTelegramSignIn`,
    `FlagNotificationDelivery`, `FlagFamilyCalendar`
  - `domain.FlagDefinition{Flag Flag; Description string; Default bool}`
  - `domain.AllFlags() []FlagDefinition`
  - `domain.ParseFlag(s string) (Flag, error)`
  - `domain.FlagSet` (`map[Flag]bool`) with `Enabled(Flag) bool` and `Strings() map[string]bool`
  - `domain.ResolveFlags(defs []FlagDefinition, global, household map[Flag]bool) FlagSet`
  - `domain.ErrUnknownFlag`, `domain.ErrAdminLocked`

- [ ] **Step 1: Write the failing test**

Create `api/internal/domain/featureflag_test.go`:

```go
package domain_test

import (
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestParseFlagRefusesAnUnknownKey(t *testing.T) {
	if _, err := domain.ParseFlag("signups_open"); err != nil {
		t.Fatalf("ParseFlag(signups_open) = %v, want nil", err)
	}
	_, err := domain.ParseFlag("not_a_flag")
	if !errors.Is(err, domain.ErrUnknownFlag) {
		t.Fatalf("ParseFlag(not_a_flag) = %v, want ErrUnknownFlag", err)
	}
}

func TestResolveFlagsPrefersTheHouseholdOverride(t *testing.T) {
	defs := []domain.FlagDefinition{
		{Flag: domain.FlagFamilyCalendar, Description: "calendar", Default: false},
	}

	cases := []struct {
		name      string
		global    map[domain.Flag]bool
		household map[domain.Flag]bool
		want      bool
	}{
		{"no overrides falls back to the compile-time default", nil, nil, false},
		{"a global override beats the default",
			map[domain.Flag]bool{domain.FlagFamilyCalendar: true}, nil, true},
		{"a household override beats a global one",
			map[domain.Flag]bool{domain.FlagFamilyCalendar: true},
			map[domain.Flag]bool{domain.FlagFamilyCalendar: false}, false},
		{"a household override beats the default with no global",
			nil, map[domain.Flag]bool{domain.FlagFamilyCalendar: true}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.ResolveFlags(defs, tc.global, tc.household)
			if got.Enabled(domain.FlagFamilyCalendar) != tc.want {
				t.Fatalf("Enabled = %v, want %v", got.Enabled(domain.FlagFamilyCalendar), tc.want)
			}
		})
	}
}

// TestResolveFlagsIgnoresAKeyThisBuildDoesNotDefine is the fail-closed case.
// An override row can outlive the const that named it, because `key` has no
// foreign key to anything. Such a row must never enable a route.
func TestResolveFlagsIgnoresAKeyThisBuildDoesNotDefine(t *testing.T) {
	defs := []domain.FlagDefinition{
		{Flag: domain.FlagFamilyCalendar, Description: "calendar", Default: false},
	}
	orphan := domain.Flag("a_flag_that_was_deleted")

	got := domain.ResolveFlags(defs,
		map[domain.Flag]bool{orphan: true},
		map[domain.Flag]bool{orphan: true})

	if got.Enabled(orphan) {
		t.Fatal("an override naming a flag this build does not define enabled it")
	}
	if _, present := got[orphan]; present {
		t.Fatalf("resolved set contains the orphan key: %v", got)
	}
}

// TestFlagSetEnabledIsFalseForAnUndefinedFlag protects every caller: a typo in
// a requireFeature call must close a route, not open one.
func TestFlagSetEnabledIsFalseForAnUndefinedFlag(t *testing.T) {
	set := domain.FlagSet{}
	if set.Enabled(domain.Flag("typo")) {
		t.Fatal("Enabled on an absent flag = true, want false")
	}
}

// TestAllFlagsHasNoDuplicateKeys guards the registry itself: two definitions
// sharing a key would make resolution depend on iteration order.
func TestAllFlagsHasNoDuplicateKeys(t *testing.T) {
	seen := map[domain.Flag]bool{}
	for _, def := range domain.AllFlags() {
		if seen[def.Flag] {
			t.Fatalf("AllFlags contains %q twice", def.Flag)
		}
		seen[def.Flag] = true
		if def.Description == "" {
			t.Fatalf("flag %q has no description; the admin screen renders it", def.Flag)
		}
	}
}

// TestEveryDefinedFlagParses keeps ParseFlag and AllFlags from drifting apart.
// ParseFlag is what guards the write path, so a flag it refuses could be
// listed in the admin UI and then rejected on save.
func TestEveryDefinedFlagParses(t *testing.T) {
	for _, def := range domain.AllFlags() {
		if _, err := domain.ParseFlag(string(def.Flag)); err != nil {
			t.Fatalf("ParseFlag(%q) = %v, want nil", def.Flag, err)
		}
	}
}
```

- [ ] **Step 2: Run the test and watch it fail**

```bash
cd api && go test ./internal/domain/ -run 'Flag' -v
```

Expected: FAIL — `undefined: domain.ParseFlag`.

- [ ] **Step 3: Write the implementation**

Create `api/internal/domain/admin.go`:

```go
package domain

import "time"

// PlatformAdmin is an operator of this install: the person who runs it, not a
// member of any one household. It is deliberately empty of permissions. There
// are no admin levels today, and adding one later means adding a field here
// rather than reinterpreting Role or Capabilities, which belong to a different
// axis entirely (see identity.go).
type PlatformAdmin struct {
	UserID    string
	Note      string
	CreatedAt time.Time
}
```

Create `api/internal/domain/featureflag.go`:

```go
package domain

import "fmt"

// A Capability answers *who may use this*. A Flag answers *whether this
// install has it at all*. A route may carry both, and neither substitutes for
// the other: turning a flag off hides a feature from everybody, including an
// owner who holds every capability.
type Flag string

const (
	// FlagSignupsOpen gates POST /auth/sign-up and the public sign-up screen,
	// so registration can be closed without a redeploy.
	FlagSignupsOpen Flag = "signups_open"
	// FlagTelegramSignIn gates the Telegram routes, which until now were
	// reachable or not purely by whether a bot was configured (ADR 4).
	FlagTelegramSignIn Flag = "telegram_sign_in"
	// FlagNotificationDelivery gates sending on the notification preferences.
	// Default off: the preferences are real, nothing sends them yet, and a
	// flag that is on for something that cannot happen is a lie.
	FlagNotificationDelivery Flag = "notification_delivery"
	// FlagFamilyCalendar gates an unbuilt page. It exists now so that
	// dark-shipping is exercised before it is needed in anger.
	FlagFamilyCalendar Flag = "family_calendar"
)

// FlagDefinition is one flag as this build knows it. Default is what a fresh
// install does with no override rows at all.
type FlagDefinition struct {
	Flag        Flag
	Description string
	Default     bool
}

// AllFlags is the whole registry. Adding a flag is one const above and one
// line here; nothing else in the system needs to learn about it.
func AllFlags() []FlagDefinition {
	return []FlagDefinition{
		{FlagSignupsOpen, "Accept new sign-ups from the public form.", true},
		{FlagTelegramSignIn, "Offer Telegram as a sign-in and sign-up channel.", true},
		{FlagNotificationDelivery, "Actually send the notifications members have asked for.", false},
		{FlagFamilyCalendar, "Show the Family calendar page.", false},
	}
}

// ParseFlag turns a key from a database column or a request into a Flag,
// refusing anything this build does not define.
func ParseFlag(s string) (Flag, error) {
	for _, def := range AllFlags() {
		if def.Flag == Flag(s) {
			return def.Flag, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownFlag, s)
}

// FlagSet is one household's resolved answer for every flag this build
// defines. Every defined key is present, so a caller never has to decide what
// a missing key means.
type FlagSet map[Flag]bool

// Enabled answers false for a flag this build does not define, so a typo in a
// caller closes a route rather than opening one.
func (f FlagSet) Enabled(flag Flag) bool { return f[flag] }

// Strings renders the set for the wire, where JSON keys are strings.
func (f FlagSet) Strings() map[string]bool {
	out := make(map[string]bool, len(f))
	for flag, enabled := range f {
		out[string(flag)] = enabled
	}
	return out
}

// ResolveFlags answers, for one household, what every flag in defs is set to.
//
// Precedence: a household override beats a global override, which beats the
// compile-time default. Keys neither map's caller could have validated are
// simply not consulted -- the result is built by walking defs, never by
// walking the overrides -- so an override row naming a flag this build does
// not define can never enable anything. That row can exist: `key` has no
// foreign key, deliberately, because the registry is compile-time.
//
// Pass a nil household map for a caller with no household (the pre-auth
// routes); household overrides are meaningless before there is a household and
// must never be treated as "on".
func ResolveFlags(defs []FlagDefinition, global, household map[Flag]bool) FlagSet {
	out := make(FlagSet, len(defs))
	for _, def := range defs {
		enabled := def.Default
		if v, ok := global[def.Flag]; ok {
			enabled = v
		}
		if v, ok := household[def.Flag]; ok {
			enabled = v
		}
		out[def.Flag] = enabled
	}
	return out
}
```

Add to `api/internal/domain/errors.go`, in the `var (...)` block:

```go
	// ErrUnknownFlag is returned for a feature-flag key this build does not
	// define -- from a request, or from an override row that outlived the
	// const that named it.
	ErrUnknownFlag = errors.New("unknown feature flag")

	// ErrAdminLocked is the admin surface's own lockout, evaluated over
	// admin_reauth_attempts. It is deliberately separate from
	// ErrHouseholdLocked: locking the operator out of /admin must never lock
	// their household out of the product.
	ErrAdminLocked = errors.New("admin re-authentication is locked")
```

- [ ] **Step 4: Run the test and watch it pass**

```bash
cd api && go test ./internal/domain/ -run 'Flag' -v
```

Expected: PASS, all six.

- [ ] **Step 5: Mutation-check the fail-closed path**

In `ResolveFlags`, add `for flag, enabled := range household { out[flag] = enabled }`
at the end — the shape a well-meaning refactor would produce.
`TestResolveFlagsIgnoresAKeyThisBuildDoesNotDefine` must fail. Remove it and
confirm green.

- [ ] **Step 6: Commit**

```bash
git add api/internal/domain/admin.go api/internal/domain/featureflag.go \
        api/internal/domain/errors.go api/internal/domain/featureflag_test.go
git commit -m "feat(domain): the feature flag registry and platform admin identity"
```

---

### Task 3: Ports and Postgres repositories

**Files:**
- Modify: `api/internal/usecase/ports.go`
- Create: `api/internal/adapter/postgres/queries/admin.sql`
- Create: `api/internal/adapter/postgres/admin_repo.go`
- Modify: `api/internal/adapter/postgres/queries/identity.sql` (add `GrantAdminSession`, widen `GetLiveSession`)
- Modify: `api/internal/adapter/postgres/session_repo.go`
- Test: `api/internal/adapter/postgres/admin_repo_test.go`

**Interfaces:**
- Consumes: `domain.PlatformAdmin`, `domain.Flag` (Task 2); the migration's
  tables (Task 1); `postgres.translate`, `uuid`, `uuidToString`, `timestamptz`,
  `timeOf`, `timePtrOf` (existing, `postgres/convert.go` and `user_repo.go`).
- Produces, in `usecase`:

```go
type PlatformAdminRepository interface {
	Get(ctx context.Context, userID string) (domain.PlatformAdmin, error)
	Grant(ctx context.Context, userID, note string) error
	Revoke(ctx context.Context, userID string) error
	List(ctx context.Context) ([]PlatformAdminListing, error)
}

type PlatformAdminListing struct {
	UserID      string
	Email       string
	DisplayName string
	Note        string
	CreatedAt   time.Time
}

type FeatureFlagRepository interface {
	OverridesFor(ctx context.Context, householdID string) (global, household map[string]bool, err error)
	GlobalOverrides(ctx context.Context) (map[string]bool, error)
	AllHouseholdOverrides(ctx context.Context) ([]HouseholdFlagOverride, error)
	SetGlobal(ctx context.Context, key string, enabled bool, updatedBy string) error
	SetHousehold(ctx context.Context, householdID, key string, enabled bool, updatedBy string) error
	ClearHousehold(ctx context.Context, householdID, key string) error
}

type HouseholdFlagOverride struct {
	HouseholdID   string
	HouseholdName string
	Key           string
	Enabled       bool
}

type AdminAuditEntry struct {
	ActorUserID string
	Action      string
	Target      string
	Detail      map[string]any
	IP          string
	At          time.Time
}

type AdminAuditRepository interface {
	Record(ctx context.Context, entry AdminAuditEntry) error
	Recent(ctx context.Context, limit int) ([]AdminAuditEntry, error)
}

type AdminReauthAttemptRepository interface {
	Record(ctx context.Context, userID string, succeeded bool, at time.Time) error
	FailuresSince(ctx context.Context, userID string, since time.Time) ([]time.Time, error)
	ClearFailures(ctx context.Context, userID string) error
}
```

  plus, on the existing `SessionRepository`:

```go
	// GrantAdmin stamps this session's admin re-auth grant. A nil expiry
	// clears it. It writes one column: session extension and this must not
	// overwrite each other.
	GrantAdmin(ctx context.Context, tokenHash []byte, expiresAt *time.Time) error
```

  and the new field on the existing `SessionRecord`:

```go
	// AdminGrantExpiresAt is nil for every ordinary session. It is non-nil
	// only between a successful POST /admin/session and that grant's expiry.
	AdminGrantExpiresAt *time.Time
```

- [ ] **Step 1: Write the failing test**

Create `api/internal/adapter/postgres/admin_repo_test.go`:

```go
package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
)

func TestPlatformAdminRepoGrantsAndRevokes(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	users := postgres.NewUserRepo(db)
	admins := postgres.NewPlatformAdminRepo(db)

	user, err := users.Create(ctx, "operator@example.test", "", "Operator")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := admins.Get(ctx, user.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get before Grant = %v, want ErrNotFound", err)
	}

	if err := admins.Grant(ctx, user.ID, "the operator"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	// Granting twice must not fail: adminctl is run by a human who will run it
	// twice, and a crash there reads as "it didn't work".
	if err := admins.Grant(ctx, user.ID, "the operator again"); err != nil {
		t.Fatalf("Grant twice: %v", err)
	}

	got, err := admins.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("Get after Grant: %v", err)
	}
	if got.UserID != user.ID || got.Note != "the operator again" {
		t.Fatalf("Get = %+v, want the second note", got)
	}

	if err := admins.Revoke(ctx, user.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := admins.Get(ctx, user.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get after Revoke = %v, want ErrNotFound", err)
	}
}

func TestFeatureFlagRepoSeparatesGlobalFromHouseholdOverrides(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	flags := postgres.NewFeatureFlagRepo(db)

	household, err := households.Create(ctx, "Test", "Test", "SGD")
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	actor, err := users.Create(ctx, "flags@example.test", "", "Flags")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := flags.SetGlobal(ctx, string(domain.FlagFamilyCalendar), true, actor.ID); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}
	if err := flags.SetHousehold(ctx, household.ID, string(domain.FlagFamilyCalendar), false, actor.ID); err != nil {
		t.Fatalf("SetHousehold: %v", err)
	}

	global, house, err := flags.OverridesFor(ctx, household.ID)
	if err != nil {
		t.Fatalf("OverridesFor: %v", err)
	}
	if global[string(domain.FlagFamilyCalendar)] != true {
		t.Fatalf("global = %v, want the flag true", global)
	}
	if house[string(domain.FlagFamilyCalendar)] != false {
		t.Fatalf("household = %v, want the flag false", house)
	}

	// SetGlobal twice is an update, not a second row: the key is the primary
	// key, and an INSERT without ON CONFLICT would fail on the second save.
	if err := flags.SetGlobal(ctx, string(domain.FlagFamilyCalendar), false, actor.ID); err != nil {
		t.Fatalf("SetGlobal twice: %v", err)
	}
	global, _, err = flags.OverridesFor(ctx, household.ID)
	if err != nil {
		t.Fatalf("OverridesFor again: %v", err)
	}
	if global[string(domain.FlagFamilyCalendar)] != false {
		t.Fatalf("global after update = %v, want false", global)
	}

	if err := flags.ClearHousehold(ctx, household.ID, string(domain.FlagFamilyCalendar)); err != nil {
		t.Fatalf("ClearHousehold: %v", err)
	}
	_, house, err = flags.OverridesFor(ctx, household.ID)
	if err != nil {
		t.Fatalf("OverridesFor after clear: %v", err)
	}
	if _, present := house[string(domain.FlagFamilyCalendar)]; present {
		t.Fatalf("household override survived ClearHousehold: %v", house)
	}
}

// TestExtendingASessionKeepsItsAdminGrant is the test the spec asks for by
// name. ExtendSession writes one column today; the day someone widens it to a
// whole-row update, every live admin grant would silently reset and nothing
// else in the suite would notice.
func TestExtendingASessionKeepsItsAdminGrant(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	users := postgres.NewUserRepo(db)
	households := postgres.NewHouseholdRepo(db)
	sessions := postgres.NewSessionRepo(db)

	household, err := households.Create(ctx, "Test", "Test", "SGD")
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	user, err := users.Create(ctx, "session@example.test", "", "Session")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	tokenHash := []byte("a-32-byte-looking-token-hash----")
	if err := sessions.Create(ctx, tokenHash, user.ID, household.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}

	grant := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Millisecond)
	if err := sessions.GrantAdmin(ctx, tokenHash, &grant); err != nil {
		t.Fatalf("GrantAdmin: %v", err)
	}
	if err := sessions.Extend(ctx, tokenHash, time.Now().Add(48*time.Hour)); err != nil {
		t.Fatalf("Extend: %v", err)
	}

	record, err := sessions.ByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	if record.AdminGrantExpiresAt == nil {
		t.Fatal("extending the session cleared its admin grant")
	}
	if !record.AdminGrantExpiresAt.Equal(grant) {
		t.Fatalf("grant = %v, want %v", record.AdminGrantExpiresAt, grant)
	}

	if err := sessions.GrantAdmin(ctx, tokenHash, nil); err != nil {
		t.Fatalf("GrantAdmin(nil): %v", err)
	}
	record, err = sessions.ByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("ByTokenHash after clear: %v", err)
	}
	if record.AdminGrantExpiresAt != nil {
		t.Fatalf("grant after clear = %v, want nil", record.AdminGrantExpiresAt)
	}
}

func TestAdminAuditRepoRecordsAndReadsBack(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	users := postgres.NewUserRepo(db)
	audit := postgres.NewAdminAuditRepo(db)

	user, err := users.Create(ctx, "audited@example.test", "", "Audited")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := audit.Record(ctx, usecase.AdminAuditEntry{
		ActorUserID: user.ID,
		Action:      "PUT /api/v1/admin/flags/{key}",
		Target:      "family_calendar",
		Detail:      map[string]any{"enabled": true},
		IP:          "203.0.113.5",
		At:          time.Now(),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := audit.Recent(ctx, 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Recent returned %d entries, want 1", len(entries))
	}
	if entries[0].Target != "family_calendar" || entries[0].Detail["enabled"] != true {
		t.Fatalf("entry = %+v, want the target and detail written", entries[0])
	}
}

func TestAdminReauthAttemptsAreScopedToOneUser(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	users := postgres.NewUserRepo(db)
	attempts := postgres.NewAdminReauthAttemptRepo(db)

	a, err := users.Create(ctx, "a@example.test", "", "A")
	if err != nil {
		t.Fatalf("create user a: %v", err)
	}
	b, err := users.Create(ctx, "b@example.test", "", "B")
	if err != nil {
		t.Fatalf("create user b: %v", err)
	}

	now := time.Now()
	for i := 0; i < 3; i++ {
		if err := attempts.Record(ctx, a.ID, false, now); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if err := attempts.Record(ctx, a.ID, true, now); err != nil {
		t.Fatalf("record success: %v", err)
	}

	failures, err := attempts.FailuresSince(ctx, a.ID, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("FailuresSince: %v", err)
	}
	if len(failures) != 3 {
		t.Fatalf("FailuresSince(a) = %d, want 3 (successes are not failures)", len(failures))
	}

	otherFailures, err := attempts.FailuresSince(ctx, b.ID, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("FailuresSince(b): %v", err)
	}
	if len(otherFailures) != 0 {
		t.Fatalf("FailuresSince(b) = %d, want 0 -- one admin's failures must not lock another", len(otherFailures))
	}

	if err := attempts.ClearFailures(ctx, a.ID); err != nil {
		t.Fatalf("ClearFailures: %v", err)
	}
	failures, err = attempts.FailuresSince(ctx, a.ID, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("FailuresSince after clear: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("FailuresSince after ClearFailures = %d, want 0", len(failures))
	}
}

// openDB starts the shared test Postgres and opens the package's own DB type.
func openDB(t *testing.T) *postgres.DB {
	t.Helper()
	db, err := postgres.Open(context.Background(), testsupport.StartPostgres(t))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}
```

Add the `usecase` import to that file's import block:
`"github.com/andreasoentoro/hearth/api/internal/usecase"`.

If `openDB` (or an equivalent) already exists in this package's tests, use the
existing one and delete this copy rather than declaring it twice.

- [ ] **Step 2: Run the test and watch it fail**

```bash
cd api && go test ./internal/adapter/postgres/ -run 'TestPlatformAdminRepo|TestFeatureFlagRepo|TestExtendingASession|TestAdminAudit|TestAdminReauth' -v
```

Expected: FAIL — `undefined: postgres.NewPlatformAdminRepo`.

- [ ] **Step 3: Write the queries**

Create `api/internal/adapter/postgres/queries/admin.sql`:

```sql
-- name: GetPlatformAdmin :one
SELECT user_id, note, created_at FROM platform_admins WHERE user_id = $1;

-- name: GrantPlatformAdmin :exec
INSERT INTO platform_admins (user_id, note) VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET note = EXCLUDED.note;

-- name: RevokePlatformAdmin :exec
DELETE FROM platform_admins WHERE user_id = $1;

-- name: ListPlatformAdmins :many
SELECT pa.user_id, u.email, u.display_name, pa.note, pa.created_at
FROM platform_admins pa
JOIN users u ON u.id = pa.user_id
ORDER BY pa.created_at;

-- name: GetGlobalFlagOverrides :many
SELECT key, enabled FROM feature_flags;

-- name: GetHouseholdFlagOverrides :many
SELECT key, enabled FROM household_feature_flags WHERE household_id = $1;

-- name: ListHouseholdFlagOverrides :many
SELECT hff.household_id, h.name AS household_name, hff.key, hff.enabled
FROM household_feature_flags hff
JOIN households h ON h.id = hff.household_id
ORDER BY h.name, hff.key;

-- name: SetGlobalFlag :exec
INSERT INTO feature_flags (key, enabled, updated_by) VALUES ($1, $2, $3)
ON CONFLICT (key) DO UPDATE
  SET enabled = EXCLUDED.enabled, updated_at = now(), updated_by = EXCLUDED.updated_by;

-- name: SetHouseholdFlag :exec
INSERT INTO household_feature_flags (household_id, key, enabled, updated_by) VALUES ($1, $2, $3, $4)
ON CONFLICT (household_id, key) DO UPDATE
  SET enabled = EXCLUDED.enabled, updated_at = now(), updated_by = EXCLUDED.updated_by;

-- name: ClearHouseholdFlag :exec
DELETE FROM household_feature_flags WHERE household_id = $1 AND key = $2;

-- name: RecordAdminAudit :exec
INSERT INTO admin_audit_log (actor_user_id, action, target, detail, ip, created_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: RecentAdminAudit :many
SELECT actor_user_id, action, target, detail, ip, created_at
FROM admin_audit_log ORDER BY created_at DESC LIMIT $1;

-- name: RecordAdminReauthAttempt :exec
INSERT INTO admin_reauth_attempts (user_id, succeeded, at) VALUES ($1, $2, $3);

-- name: AdminReauthFailuresSince :many
SELECT at FROM admin_reauth_attempts
WHERE user_id = $1 AND succeeded = false AND at >= $2
ORDER BY at;

-- name: ClearAdminReauthFailures :exec
DELETE FROM admin_reauth_attempts WHERE user_id = $1;
```

Modify `api/internal/adapter/postgres/queries/identity.sql`. Widen the session
read and add the grant write, leaving `ExtendSession` exactly as it is:

```sql
-- name: GetLiveSession :one
SELECT id, user_id, household_id, expires_at, admin_grant_expires_at FROM sessions
WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now();

-- name: GrantAdminSession :exec
-- One column, deliberately: ExtendSession writes expires_at and this writes
-- the grant, so neither can silently undo the other.
UPDATE sessions SET admin_grant_expires_at = $2 WHERE token_hash = $1;
```

Regenerate:

```bash
cd api && sqlc generate
```

If `sqlc` is not installed: `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`.

- [ ] **Step 4: Write the ports**

Add to `api/internal/usecase/ports.go` — the exact interfaces, structs and
doc comments listed under **Interfaces** above. Put them together, after
`LoginAttemptRepository`, under a section comment:

```go
// The platform admin ports. Platform admin is an axis orthogonal to
// household Role and Capabilities (see domain/admin.go): these repositories
// answer "who runs this install", never "what may this member do".
//
// Nothing here decides whether a caller is allowed to do something. The HTTP
// layer's requirePlatformAdmin does that. Where a userID is passed into a
// write below it is stored -- in updated_by, or in the audit row -- and never
// consulted for permission.
```

Add `GrantAdmin` to the existing `SessionRepository` interface and
`AdminGrantExpiresAt *time.Time` to the existing `SessionRecord` struct, with
the doc comments from **Interfaces** above.

- [ ] **Step 5: Write the repositories**

Create `api/internal/adapter/postgres/admin_repo.go` with `PlatformAdminRepo`,
`FeatureFlagRepo`, `AdminAuditRepo` and `AdminReauthAttemptRepo`, following
`session_repo.go`'s shape exactly — a struct wrapping `*sqlcgen.Queries`, a
`NewXRepo(db *DB)` constructor, and every error passed through
`translate(err, "<op>")`:

```go
package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type PlatformAdminRepo struct{ q *sqlcgen.Queries }

func NewPlatformAdminRepo(db *DB) *PlatformAdminRepo {
	return &PlatformAdminRepo{q: sqlcgen.New(db.Pool())}
}

func (r *PlatformAdminRepo) Get(ctx context.Context, userID string) (domain.PlatformAdmin, error) {
	row, err := r.q.GetPlatformAdmin(ctx, uuid(userID))
	if err != nil {
		return domain.PlatformAdmin{}, translate(err, "get platform admin")
	}
	return domain.PlatformAdmin{
		UserID:    uuidToString(row.UserID),
		Note:      row.Note,
		CreatedAt: timeOf(row.CreatedAt),
	}, nil
}

// Grant is an upsert rather than an insert: adminctl is run by a person who
// will run it twice, and failing the second time reads as "it did not work".
func (r *PlatformAdminRepo) Grant(ctx context.Context, userID, note string) error {
	return translate(r.q.GrantPlatformAdmin(ctx, sqlcgen.GrantPlatformAdminParams{
		UserID: uuid(userID),
		Note:   note,
	}), "grant platform admin")
}

func (r *PlatformAdminRepo) Revoke(ctx context.Context, userID string) error {
	return translate(r.q.RevokePlatformAdmin(ctx, uuid(userID)), "revoke platform admin")
}

func (r *PlatformAdminRepo) List(ctx context.Context) ([]usecase.PlatformAdminListing, error) {
	rows, err := r.q.ListPlatformAdmins(ctx)
	if err != nil {
		return nil, translate(err, "list platform admins")
	}
	out := make([]usecase.PlatformAdminListing, 0, len(rows))
	for _, row := range rows {
		listing := usecase.PlatformAdminListing{
			UserID:      uuidToString(row.UserID),
			DisplayName: row.DisplayName,
			Note:        row.Note,
			CreatedAt:   timeOf(row.CreatedAt),
		}
		// users.email is nullable -- a member created without credentials has
		// none. sqlc renders it as *string with emit_pointers_for_null_types.
		if row.Email != nil {
			listing.Email = *row.Email
		}
		out = append(out, listing)
	}
	return out, nil
}

type FeatureFlagRepo struct{ q *sqlcgen.Queries }

func NewFeatureFlagRepo(db *DB) *FeatureFlagRepo {
	return &FeatureFlagRepo{q: sqlcgen.New(db.Pool())}
}

// OverridesFor returns the two override layers separately rather than merged,
// because merging is domain.ResolveFlags' job and it needs to know which layer
// each value came from.
func (r *FeatureFlagRepo) OverridesFor(ctx context.Context, householdID string) (map[string]bool, map[string]bool, error) {
	global, err := r.GlobalOverrides(ctx)
	if err != nil {
		return nil, nil, err
	}
	rows, err := r.q.GetHouseholdFlagOverrides(ctx, uuid(householdID))
	if err != nil {
		return nil, nil, translate(err, "get household flag overrides")
	}
	household := make(map[string]bool, len(rows))
	for _, row := range rows {
		household[row.Key] = row.Enabled
	}
	return global, household, nil
}

func (r *FeatureFlagRepo) GlobalOverrides(ctx context.Context) (map[string]bool, error) {
	rows, err := r.q.GetGlobalFlagOverrides(ctx)
	if err != nil {
		return nil, translate(err, "get global flag overrides")
	}
	global := make(map[string]bool, len(rows))
	for _, row := range rows {
		global[row.Key] = row.Enabled
	}
	return global, nil
}

func (r *FeatureFlagRepo) AllHouseholdOverrides(ctx context.Context) ([]usecase.HouseholdFlagOverride, error) {
	rows, err := r.q.ListHouseholdFlagOverrides(ctx)
	if err != nil {
		return nil, translate(err, "list household flag overrides")
	}
	out := make([]usecase.HouseholdFlagOverride, 0, len(rows))
	for _, row := range rows {
		out = append(out, usecase.HouseholdFlagOverride{
			HouseholdID:   uuidToString(row.HouseholdID),
			HouseholdName: row.HouseholdName,
			Key:           row.Key,
			Enabled:       row.Enabled,
		})
	}
	return out, nil
}

func (r *FeatureFlagRepo) SetGlobal(ctx context.Context, key string, enabled bool, updatedBy string) error {
	return translate(r.q.SetGlobalFlag(ctx, sqlcgen.SetGlobalFlagParams{
		Key:       key,
		Enabled:   enabled,
		UpdatedBy: nullableUUID(&updatedBy),
	}), "set global flag")
}

func (r *FeatureFlagRepo) SetHousehold(ctx context.Context, householdID, key string, enabled bool, updatedBy string) error {
	return translate(r.q.SetHouseholdFlag(ctx, sqlcgen.SetHouseholdFlagParams{
		HouseholdID: uuid(householdID),
		Key:         key,
		Enabled:     enabled,
		UpdatedBy:   nullableUUID(&updatedBy),
	}), "set household flag")
}

func (r *FeatureFlagRepo) ClearHousehold(ctx context.Context, householdID, key string) error {
	return translate(r.q.ClearHouseholdFlag(ctx, sqlcgen.ClearHouseholdFlagParams{
		HouseholdID: uuid(householdID),
		Key:         key,
	}), "clear household flag")
}

type AdminAuditRepo struct{ q *sqlcgen.Queries }

func NewAdminAuditRepo(db *DB) *AdminAuditRepo { return &AdminAuditRepo{q: sqlcgen.New(db.Pool())} }

func (r *AdminAuditRepo) Record(ctx context.Context, entry usecase.AdminAuditEntry) error {
	detail := entry.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return translate(err, "encode audit detail")
	}
	return translate(r.q.RecordAdminAudit(ctx, sqlcgen.RecordAdminAuditParams{
		ActorUserID: uuid(entry.ActorUserID),
		Action:      entry.Action,
		Target:      entry.Target,
		Detail:      encoded,
		Ip:          entry.IP,
		CreatedAt:   timestamptz(entry.At),
	}), "record admin audit")
}

func (r *AdminAuditRepo) Recent(ctx context.Context, limit int) ([]usecase.AdminAuditEntry, error) {
	rows, err := r.q.RecentAdminAudit(ctx, int32(limit))
	if err != nil {
		return nil, translate(err, "recent admin audit")
	}
	out := make([]usecase.AdminAuditEntry, 0, len(rows))
	for _, row := range rows {
		detail := map[string]any{}
		// A detail column this code cannot decode must not fail the whole
		// read: the log is for looking at after something went wrong, and
		// that is exactly when a malformed row is most likely.
		_ = json.Unmarshal(row.Detail, &detail)
		out = append(out, usecase.AdminAuditEntry{
			ActorUserID: uuidToString(row.ActorUserID),
			Action:      row.Action,
			Target:      row.Target,
			Detail:      detail,
			IP:          row.Ip,
			At:          timeOf(row.CreatedAt),
		})
	}
	return out, nil
}

type AdminReauthAttemptRepo struct{ q *sqlcgen.Queries }

func NewAdminReauthAttemptRepo(db *DB) *AdminReauthAttemptRepo {
	return &AdminReauthAttemptRepo{q: sqlcgen.New(db.Pool())}
}

func (r *AdminReauthAttemptRepo) Record(ctx context.Context, userID string, succeeded bool, at time.Time) error {
	return translate(r.q.RecordAdminReauthAttempt(ctx, sqlcgen.RecordAdminReauthAttemptParams{
		UserID:    uuid(userID),
		Succeeded: succeeded,
		At:        timestamptz(at),
	}), "record admin reauth attempt")
}

func (r *AdminReauthAttemptRepo) FailuresSince(ctx context.Context, userID string, since time.Time) ([]time.Time, error) {
	rows, err := r.q.AdminReauthFailuresSince(ctx, sqlcgen.AdminReauthFailuresSinceParams{
		UserID: uuid(userID),
		At:     timestamptz(since),
	})
	if err != nil {
		return nil, translate(err, "admin reauth failures since")
	}
	out := make([]time.Time, 0, len(rows))
	for _, at := range rows {
		out = append(out, timeOf(at))
	}
	return out, nil
}

func (r *AdminReauthAttemptRepo) ClearFailures(ctx context.Context, userID string) error {
	return translate(r.q.ClearAdminReauthFailures(ctx, uuid(userID)), "clear admin reauth failures")
}
```

Modify `api/internal/adapter/postgres/session_repo.go`: populate the new field
in `ByTokenHash` and add `GrantAdmin`:

```go
	return usecase.SessionRecord{
		UserID:              uuidToString(row.UserID),
		HouseholdID:         uuidToString(row.HouseholdID),
		ExpiresAt:           timeOf(row.ExpiresAt),
		AdminGrantExpiresAt: timePtrOf(row.AdminGrantExpiresAt),
	}, nil
```

```go
// GrantAdmin writes only admin_grant_expires_at. Extend writes only
// expires_at. Keeping them to one column each is what makes extending a
// session near its expiry safe in the middle of an admin session.
func (r *SessionRepo) GrantAdmin(ctx context.Context, tokenHash []byte, expiresAt *time.Time) error {
	var stamp pgtype.Timestamptz
	if expiresAt != nil {
		stamp = timestamptz(*expiresAt)
	}
	return translate(r.q.GrantAdminSession(ctx, sqlcgen.GrantAdminSessionParams{
		TokenHash:           tokenHash,
		AdminGrantExpiresAt: stamp,
	}), "grant admin session")
}
```

Add `"github.com/jackc/pgx/v5/pgtype"` to that file's imports.

- [ ] **Step 6: Run the tests and watch them pass**

```bash
cd api && go test ./internal/adapter/postgres/ -run 'TestPlatformAdminRepo|TestFeatureFlagRepo|TestExtendingASession|TestAdminAudit|TestAdminReauth' -v
```

Expected: PASS, all five.

- [ ] **Step 7: Mutation-check the grant/extend independence**

Change `ExtendSession` in `queries/identity.sql` to
`UPDATE sessions SET expires_at = $2, admin_grant_expires_at = NULL WHERE token_hash = $1`,
regenerate, and re-run. `TestExtendingASessionKeepsItsAdminGrant` must fail.
Revert, regenerate, confirm green.

- [ ] **Step 8: Commit**

```bash
git add api/internal/usecase/ports.go api/internal/adapter/postgres/
git commit -m "feat(data): repositories for platform admins, flags, audit and re-auth attempts"
```

---

### Task 4: The services

**Files:**
- Create: `api/internal/usecase/admin.go`
- Create: `api/internal/usecase/admin_reauth.go`
- Test: `api/internal/usecase/admin_test.go`
- Test: `api/internal/usecase/admin_reauth_test.go`

**Interfaces:**
- Consumes: every port from Task 3; `domain.AllFlags`, `domain.ParseFlag`,
  `domain.ResolveFlags`, `domain.LockoutPolicy`, `usecase.PasswordHasher`,
  `usecase.UserRepository.ByID` (returns `StoredUser` with `.User` and
  `.PasswordHash`).
- Produces:

```go
type AdminDeps struct {
	Admins PlatformAdminRepository
	Flags  FeatureFlagRepository
	Audit  AdminAuditRepository
	Clock  Clock
}
func NewAdminService(d AdminDeps) *AdminService

func (s *AdminService) IsPlatformAdmin(ctx context.Context, userID string) (bool, error)
func (s *AdminService) FlagsFor(ctx context.Context, householdID string) (domain.FlagSet, error)
func (s *AdminService) GlobalFlags(ctx context.Context) (domain.FlagSet, error)
func (s *AdminService) Overview(ctx context.Context) ([]FlagOverview, error)
func (s *AdminService) SetGlobalFlag(ctx context.Context, key string, enabled bool, actorUserID string) error
func (s *AdminService) SetHouseholdFlag(ctx context.Context, householdID, key string, enabled bool, actorUserID string) error
func (s *AdminService) ClearHouseholdFlag(ctx context.Context, householdID, key string) error
func (s *AdminService) RecordAudit(ctx context.Context, entry AdminAuditEntry) error
func (s *AdminService) RecentAudit(ctx context.Context, limit int) ([]AdminAuditEntry, error)

type FlagOverview struct {
	Key           string
	Description   string
	Default       bool
	GlobalSet     bool  // an override row exists globally
	GlobalEnabled bool  // its value, meaningless when GlobalSet is false
	Effective     bool  // what an install-wide caller gets today
	Overrides     []HouseholdFlagOverride
	Orphaned      bool  // true for override rows naming no defined flag
}

type AdminReauthDeps struct {
	Users    UserRepository
	Attempts AdminReauthAttemptRepository
	Hasher   PasswordHasher
	Clock    Clock
	Policy   domain.LockoutPolicy
}
func NewAdminReauthService(d AdminReauthDeps) *AdminReauthService
func (s *AdminReauthService) Verify(ctx context.Context, userID, password string) error
```

- [ ] **Step 1: Write the failing tests**

Create `api/internal/usecase/admin_test.go`:

```go
package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestFlagsForAppliesTheHouseholdOverride(t *testing.T) {
	flags := newFakeFlagRepo()
	flags.global[string(domain.FlagFamilyCalendar)] = true
	flags.household["h1"] = map[string]bool{string(domain.FlagFamilyCalendar): false}

	svc := usecase.NewAdminService(usecase.AdminDeps{
		Admins: newFakeAdminRepo(), Flags: flags,
		Audit: newFakeAuditRepo(), Clock: fixedClock{},
	})

	set, err := svc.FlagsFor(context.Background(), "h1")
	if err != nil {
		t.Fatalf("FlagsFor: %v", err)
	}
	if set.Enabled(domain.FlagFamilyCalendar) {
		t.Fatal("the household's own override was ignored")
	}

	other, err := svc.FlagsFor(context.Background(), "h2")
	if err != nil {
		t.Fatalf("FlagsFor(h2): %v", err)
	}
	if !other.Enabled(domain.FlagFamilyCalendar) {
		t.Fatal("a household with no override should see the global value")
	}
}

// TestGlobalFlagsIgnoresHouseholdOverrides is the pre-auth path: a caller with
// no household must never be handed some household's opinion.
func TestGlobalFlagsIgnoresHouseholdOverrides(t *testing.T) {
	flags := newFakeFlagRepo()
	flags.household["h1"] = map[string]bool{string(domain.FlagSignupsOpen): false}

	svc := usecase.NewAdminService(usecase.AdminDeps{
		Admins: newFakeAdminRepo(), Flags: flags,
		Audit: newFakeAuditRepo(), Clock: fixedClock{},
	})

	set, err := svc.GlobalFlags(context.Background())
	if err != nil {
		t.Fatalf("GlobalFlags: %v", err)
	}
	if !set.Enabled(domain.FlagSignupsOpen) {
		t.Fatal("a household override leaked into the global set")
	}
}

func TestSetGlobalFlagRefusesAnUnknownKey(t *testing.T) {
	flags := newFakeFlagRepo()
	svc := usecase.NewAdminService(usecase.AdminDeps{
		Admins: newFakeAdminRepo(), Flags: flags,
		Audit: newFakeAuditRepo(), Clock: fixedClock{},
	})

	err := svc.SetGlobalFlag(context.Background(), "not_a_flag", true, "actor")
	if !errors.Is(err, domain.ErrUnknownFlag) {
		t.Fatalf("SetGlobalFlag(not_a_flag) = %v, want ErrUnknownFlag", err)
	}
	if len(flags.global) != 0 {
		t.Fatalf("an unknown key was written: %v", flags.global)
	}
}

// TestOverviewMarksOrphanedRows: an override row can outlive the const that
// named it. The screen must show those rather than hide them, or nobody ever
// deletes them.
func TestOverviewMarksOrphanedRows(t *testing.T) {
	flags := newFakeFlagRepo()
	flags.global["a_flag_that_was_deleted"] = true

	svc := usecase.NewAdminService(usecase.AdminDeps{
		Admins: newFakeAdminRepo(), Flags: flags,
		Audit: newFakeAuditRepo(), Clock: fixedClock{},
	})

	overview, err := svc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	var orphans int
	for _, row := range overview {
		if row.Orphaned {
			orphans++
			if row.Key != "a_flag_that_was_deleted" {
				t.Fatalf("orphan = %q, want the deleted key", row.Key)
			}
		}
	}
	if orphans != 1 {
		t.Fatalf("Overview reported %d orphans, want 1", orphans)
	}
	if len(overview) != len(domain.AllFlags())+1 {
		t.Fatalf("Overview has %d rows, want every defined flag plus the orphan", len(overview))
	}
}
```

Create `api/internal/usecase/admin_reauth_test.go`:

```go
package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestAdminReauthVerifiesTheRightPassword(t *testing.T) {
	users := newFakeUserRepo()
	user := users.mustCreate(t, "operator@example.test", "hashed:correct-horse", "Operator")
	attempts := newFakeReauthAttemptRepo()

	svc := usecase.NewAdminReauthService(usecase.AdminReauthDeps{
		Users: users, Attempts: attempts, Hasher: fakeHasher{},
		Clock: fixedClock{}, Policy: domain.DefaultLockoutPolicy(),
	})

	if err := svc.Verify(context.Background(), user.ID, "correct-horse"); err != nil {
		t.Fatalf("Verify with the right password = %v, want nil", err)
	}
	if err := svc.Verify(context.Background(), user.ID, "wrong"); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("Verify with a wrong password = %v, want ErrInvalidCredentials", err)
	}
}

// TestAdminReauthLocksAfterThreeFailures uses the same policy as household
// sign-in, over its own ledger.
func TestAdminReauthLocksAfterThreeFailures(t *testing.T) {
	users := newFakeUserRepo()
	user := users.mustCreate(t, "operator@example.test", "hashed:correct-horse", "Operator")
	attempts := newFakeReauthAttemptRepo()

	svc := usecase.NewAdminReauthService(usecase.AdminReauthDeps{
		Users: users, Attempts: attempts, Hasher: fakeHasher{},
		Clock: fixedClock{}, Policy: domain.DefaultLockoutPolicy(),
	})

	for i := 0; i < 3; i++ {
		if err := svc.Verify(context.Background(), user.ID, "wrong"); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("attempt %d = %v, want ErrInvalidCredentials", i+1, err)
		}
	}

	// Even the correct password is refused while locked -- otherwise the lock
	// bounds nothing, since guessing right is exactly what it must prevent.
	if err := svc.Verify(context.Background(), user.ID, "correct-horse"); !errors.Is(err, domain.ErrAdminLocked) {
		t.Fatalf("Verify while locked = %v, want ErrAdminLocked", err)
	}
}

// TestAdminReauthFailuresStayOutOfTheHouseholdLedger is the whole reason the
// second table exists. The fake login-attempt repo must never be touched.
func TestAdminReauthFailuresStayOutOfTheHouseholdLedger(t *testing.T) {
	users := newFakeUserRepo()
	user := users.mustCreate(t, "operator@example.test", "hashed:correct-horse", "Operator")
	attempts := newFakeReauthAttemptRepo()
	household := newFakeLoginAttemptRepo()

	svc := usecase.NewAdminReauthService(usecase.AdminReauthDeps{
		Users: users, Attempts: attempts, Hasher: fakeHasher{},
		Clock: fixedClock{}, Policy: domain.DefaultLockoutPolicy(),
	})

	for i := 0; i < 5; i++ {
		_ = svc.Verify(context.Background(), user.ID, "wrong")
	}

	if household.recorded != 0 {
		t.Fatalf("admin re-auth wrote %d rows to login_attempts; it must write none", household.recorded)
	}
	failures, err := attempts.FailuresSince(context.Background(), user.ID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("FailuresSince: %v", err)
	}
	if len(failures) != 5 {
		t.Fatalf("admin ledger holds %d failures, want 5", len(failures))
	}
}
```

Add the fakes to `api/internal/usecase/testdouble_test.go`, following the
in-memory doubles already there: `fakeAdminRepo`, `fakeFlagRepo` (fields
`global map[string]bool`, `household map[string]map[string]bool`),
`fakeAuditRepo`, `fakeReauthAttemptRepo`, `fakeLoginAttemptRepo` (with a
`recorded int` counter), `fixedClock`, and `fakeHasher` whose `Hash` returns
`"hashed:" + plain` and whose `Verify` compares `encoded == "hashed:"+plain`.
Reuse any of these that already exist under another name rather than declaring
a second one.

- [ ] **Step 2: Run the tests and watch them fail**

```bash
cd api && go test ./internal/usecase/ -run 'TestFlagsFor|TestGlobalFlags|TestSetGlobalFlag|TestOverview|TestAdminReauth' -v
```

Expected: FAIL — `undefined: usecase.NewAdminService`.

- [ ] **Step 3: Write `admin.go`**

```go
package usecase

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// AdminService is the operator's surface: reading and writing feature flags,
// answering whether a user is a platform admin, and appending to the audit
// log.
//
// It takes no actor parameter for any *permission* decision -- the HTTP
// layer's requirePlatformAdmin is the only gate. The actorUserID arguments
// below are written to updated_by and to audit rows, and are never consulted
// to decide whether a call is allowed.
type AdminService struct{ d AdminDeps }

type AdminDeps struct {
	Admins PlatformAdminRepository
	Flags  FeatureFlagRepository
	Audit  AdminAuditRepository
	Clock  Clock
}

func NewAdminService(d AdminDeps) *AdminService { return &AdminService{d: d} }

// IsPlatformAdmin distinguishes "not an admin" from "the lookup failed": a
// database outage must not read as a clean no, because the caller turns a no
// into a 404 that hides the whole surface.
func (s *AdminService) IsPlatformAdmin(ctx context.Context, userID string) (bool, error) {
	_, err := s.d.Admins.Get(ctx, userID)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, domain.ErrNotFound):
		return false, nil
	default:
		return false, err
	}
}

// FlagsFor resolves every defined flag for one household.
func (s *AdminService) FlagsFor(ctx context.Context, householdID string) (domain.FlagSet, error) {
	global, household, err := s.d.Flags.OverridesFor(ctx, householdID)
	if err != nil {
		return nil, err
	}
	return domain.ResolveFlags(domain.AllFlags(), asFlagMap(global), asFlagMap(household)), nil
}

// GlobalFlags resolves every defined flag for a caller with no household --
// the pre-auth routes. It passes a nil household layer rather than picking
// one, because household overrides are meaningless before there is a
// household.
func (s *AdminService) GlobalFlags(ctx context.Context) (domain.FlagSet, error) {
	global, err := s.d.Flags.GlobalOverrides(ctx)
	if err != nil {
		return nil, err
	}
	return domain.ResolveFlags(domain.AllFlags(), asFlagMap(global), nil), nil
}

// Overview is the admin screen's model: one row per defined flag, plus one row
// per override key this build does not define. Those orphans are shown rather
// than filtered out -- a row nobody can see is a row nobody deletes.
func (s *AdminService) Overview(ctx context.Context) ([]FlagOverview, error) {
	global, err := s.d.Flags.GlobalOverrides(ctx)
	if err != nil {
		return nil, err
	}
	overrides, err := s.d.Flags.AllHouseholdOverrides(ctx)
	if err != nil {
		return nil, err
	}

	byKey := map[string][]HouseholdFlagOverride{}
	for _, o := range overrides {
		byKey[o.Key] = append(byKey[o.Key], o)
	}

	defined := map[string]bool{}
	out := make([]FlagOverview, 0, len(domain.AllFlags()))
	for _, def := range domain.AllFlags() {
		key := string(def.Flag)
		defined[key] = true
		value, set := global[key]
		effective := def.Default
		if set {
			effective = value
		}
		out = append(out, FlagOverview{
			Key:           key,
			Description:   def.Description,
			Default:       def.Default,
			GlobalSet:     set,
			GlobalEnabled: value,
			Effective:     effective,
			Overrides:     byKey[key],
		})
	}

	for key := range global {
		if !defined[key] {
			out = append(out, FlagOverview{Key: key, Orphaned: true, Overrides: byKey[key]})
		}
	}
	for key := range byKey {
		if !defined[key] && global[key] == false {
			if _, alreadyGlobal := global[key]; !alreadyGlobal {
				out = append(out, FlagOverview{Key: key, Orphaned: true, Overrides: byKey[key]})
			}
		}
	}
	return out, nil
}

// FlagOverview is one row of the admin flags screen.
type FlagOverview struct {
	Key           string
	Description   string
	Default       bool
	GlobalSet     bool
	GlobalEnabled bool
	Effective     bool
	Overrides     []HouseholdFlagOverride
	// Orphaned marks an override row naming a flag this build does not
	// define. Such a row enables nothing (see domain.ResolveFlags); it is
	// listed only so somebody can delete it.
	Orphaned bool
}

func (s *AdminService) SetGlobalFlag(ctx context.Context, key string, enabled bool, actorUserID string) error {
	flag, err := domain.ParseFlag(key)
	if err != nil {
		return err
	}
	return s.d.Flags.SetGlobal(ctx, string(flag), enabled, actorUserID)
}

func (s *AdminService) SetHouseholdFlag(ctx context.Context, householdID, key string, enabled bool, actorUserID string) error {
	flag, err := domain.ParseFlag(key)
	if err != nil {
		return err
	}
	return s.d.Flags.SetHousehold(ctx, householdID, string(flag), enabled, actorUserID)
}

// ClearHouseholdFlag removes an override rather than setting it false: "no
// opinion" and "explicitly off" are different states, and the screen shows
// all three.
//
// It does not call ParseFlag, deliberately: deleting an orphaned row is the
// one operation that must work on a key this build no longer defines.
func (s *AdminService) ClearHouseholdFlag(ctx context.Context, householdID, key string) error {
	return s.d.Flags.ClearHousehold(ctx, householdID, key)
}

func (s *AdminService) RecordAudit(ctx context.Context, entry AdminAuditEntry) error {
	if entry.At.IsZero() {
		entry.At = s.d.Clock.Now()
	}
	return s.d.Audit.Record(ctx, entry)
}

func (s *AdminService) RecentAudit(ctx context.Context, limit int) ([]AdminAuditEntry, error) {
	return s.d.Audit.Recent(ctx, limit)
}

// asFlagMap re-keys a repository's string map for the domain. The repositories
// speak strings because a column can hold anything; the domain speaks Flag
// because it validated.
func asFlagMap(in map[string]bool) map[domain.Flag]bool {
	out := make(map[domain.Flag]bool, len(in))
	for key, enabled := range in {
		out[domain.Flag(key)] = enabled
	}
	return out
}
```

Add `"errors"` to the import block.

Simplify `Overview`'s orphan loop to this, which is what the test asserts and
avoids the duplicated condition above:

```go
	seen := map[string]bool{}
	for key := range global {
		seen[key] = true
	}
	for key := range byKey {
		seen[key] = true
	}
	for key := range seen {
		if defined[key] {
			continue
		}
		out = append(out, FlagOverview{Key: key, Orphaned: true, Overrides: byKey[key]})
	}
```

- [ ] **Step 4: Write `admin_reauth.go`**

```go
package usecase

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// AdminReauthService verifies the password again before the admin surface
// opens. The session cookie lives 30 days; this exists so a stolen cookie
// alone is not the key to every household's data.
//
// Its failures are counted in their own ledger, never in login_attempts. That
// table's lockout is household-scoped, so an operator's mistypes there would
// lock their whole household out of the ordinary product -- a bad outcome
// caused by a screen nobody else can even see.
type AdminReauthService struct{ d AdminReauthDeps }

type AdminReauthDeps struct {
	Users    UserRepository
	Attempts AdminReauthAttemptRepository
	Hasher   PasswordHasher
	Clock    Clock
	Policy   domain.LockoutPolicy
}

// NewAdminReauthService fills in a zero-valued Policy, which would otherwise
// never lock -- the same guard NewAuthService applies for the same reason.
func NewAdminReauthService(d AdminReauthDeps) *AdminReauthService {
	if d.Policy.MaxAttempts == 0 {
		d.Policy = domain.DefaultLockoutPolicy()
	}
	return &AdminReauthService{d: d}
}

// Verify answers nil when password is this user's, domain.ErrInvalidCredentials
// when it is not, and domain.ErrAdminLocked while the lockout is in force --
// including for the correct password, since guessing right is exactly what the
// lock exists to stop.
func (s *AdminReauthService) Verify(ctx context.Context, userID, password string) error {
	now := s.d.Clock.Now()

	failures, err := s.d.Attempts.FailuresSince(ctx, userID, now.Add(-s.d.Policy.Window))
	if err != nil {
		return err
	}
	if state := s.d.Policy.Evaluate(failures, now); state.Locked {
		return domain.ErrAdminLocked
	}

	user, err := s.d.Users.ByID(ctx, userID)
	if err != nil {
		return err
	}

	// A user with no password at all (a member created without credentials)
	// can never satisfy this. Verify is not asked; an empty stored hash is a
	// refusal, not something to compare against.
	if user.PasswordHash == "" || !s.d.Hasher.Verify(password, user.PasswordHash) {
		if recErr := s.d.Attempts.Record(ctx, userID, false, now); recErr != nil {
			return recErr
		}
		return domain.ErrInvalidCredentials
	}

	if err := s.d.Attempts.Record(ctx, userID, true, now); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 5: Run the tests and watch them pass**

```bash
cd api && go test ./internal/usecase/ -run 'TestFlagsFor|TestGlobalFlags|TestSetGlobalFlag|TestOverview|TestAdminReauth' -v
```

Expected: PASS, all seven.

- [ ] **Step 6: Mutation-check the lockout**

In `Verify`, move the lockout check to after the password comparison.
`TestAdminReauthLocksAfterThreeFailures` must fail on its final assertion (the
correct password would succeed while locked). Put it back and confirm green.

- [ ] **Step 7: Commit**

```bash
git add api/internal/usecase/
git commit -m "feat(usecase): admin flag service and its own re-auth lockout"
```

---

### Task 5: The admin gate — middleware and `POST /admin/session`

**Files:**
- Create: `api/internal/adapter/http/middleware_admin.go`
- Create: `api/internal/adapter/http/admin_handlers.go`
- Modify: `api/internal/adapter/http/router.go`
- Modify: `api/internal/adapter/http/errors.go` (map `ErrAdminLocked`)
- Modify: `api/internal/adapter/http/api_test.go` (env gains an admin)
- Test: `api/internal/adapter/http/admin_api_test.go`

**Interfaces:**
- Consumes: `usecase.AdminService`, `usecase.AdminReauthService` (Task 4);
  `Deps`, `Scope`, `RequestScope`, `requireSession`, `requireCSRF`,
  `WriteError`, `WriteJSON`, `decodeJSONBody`, `MapDomainError` (existing).
- Produces:
  - `Deps.Admin *usecase.AdminService`, `Deps.AdminReauth *usecase.AdminReauthService`
  - `adminGrantTTL = 30 * time.Minute`
  - middlewares `requirePlatformAdmin(deps)`, `auditAdmin(deps)`, `requireAdminGrant(deps)`
  - route `POST /api/v1/admin/session` and `GET /api/v1/admin/flags`
  - error code `ADMIN_REAUTH_REQUIRED` (401), `ADMIN_LOCKED` (423)

- [ ] **Step 1: Write the failing test**

Create `api/internal/adapter/http/admin_api_test.go`:

```go
package httpadapter_test

import (
	"net/http"
	"testing"
)

// TestAdminRoutesAre404ToANonAdmin is the shape of the whole gate: to anyone
// who is not a platform admin, /admin is indistinguishable from a typo. A 403
// would confirm the surface exists and that they found the right path.
func TestAdminRoutesAre404ToANonAdmin(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/admin/flags", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")

	rec = env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}

func TestAdminRoutesNeedAGrant(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/admin/flags", session)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "ADMIN_REAUTH_REQUIRED")
}

func TestAdminSessionMintsAGrant(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /admin/session = %d, body %s; want 204", rec.Code, rec.Body.String())
	}

	rec = env.authedGet(t, "/api/v1/admin/flags", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/flags after re-auth = %d, body %s; want 200", rec.Code, rec.Body.String())
	}
}

func TestAdminSessionRefusesTheWrongPassword(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": "not-the-password"}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "INVALID_CREDENTIALS")

	rec = env.authedGet(t, "/api/v1/admin/flags", session)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "ADMIN_REAUTH_REQUIRED")
}

// TestAdminReauthLockoutLeavesHouseholdSignInWorking is the separation the
// second ledger exists for, asserted end to end.
func TestAdminReauthLockoutLeavesHouseholdSignInWorking(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	for i := 0; i < 3; i++ {
		env.authed(t, http.MethodPost, "/api/v1/admin/session",
			map[string]string{"password": "wrong"}, session, csrf)
	}

	rec := env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)
	assertErrorResponse(t, rec, http.StatusLocked, "ADMIN_LOCKED")

	// The household is untouched: a fresh password sign-in still works.
	env.signIn(t, env.ownerEmail, env.ownerPassword)
}

// TestSigningOutRevokesTheAdminGrant: the grant lives on the session row, so
// revoking the session must take it with it.
func TestSigningOutRevokesTheAdminGrant(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)

	env.authed(t, http.MethodPost, "/api/v1/auth/sign-out", nil, session, csrf)

	rec := env.authedGet(t, "/api/v1/admin/flags", session)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "UNAUTHENTICATED")
}

// TestEveryAdminRequestIsAudited: the log is written from middleware, so even
// a plain read leaves a row.
func TestEveryAdminRequestIsAudited(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)

	before := env.auditRowCount(t)
	env.authedGet(t, "/api/v1/admin/flags", session)
	after := env.auditRowCount(t)

	if after != before+1 {
		t.Fatalf("audit rows went %d -> %d; a read must write exactly one row", before, after)
	}
}
```

Add two helpers to `api/internal/adapter/http/api_test.go`, and store the
repositories the env already builds so they can be reached:

```go
// makePlatformAdmin grants the platform-admin row directly through the
// repository rather than over HTTP, because there is deliberately no HTTP
// route that can create one (see the spec, §2.1).
func (env *testEnv) makePlatformAdmin(t *testing.T, email string) {
	t.Helper()
	user, err := env.users.ByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("look up %s: %v", email, err)
	}
	if err := env.platformAdmins.Grant(context.Background(), user.User.ID, "test"); err != nil {
		t.Fatalf("grant platform admin: %v", err)
	}
}

// auditRowCount reads the audit log's length through the repository. The
// admin API's own /admin/audit route is not used here: a test that asserts on
// auditing must not depend on a route that is itself audited.
func (env *testEnv) auditRowCount(t *testing.T) int {
	t.Helper()
	entries, err := env.adminAudit.Recent(context.Background(), 1000)
	if err != nil {
		t.Fatalf("recent audit: %v", err)
	}
	return len(entries)
}
```

with `users`, `platformAdmins` and `adminAudit` added to `testEnv`'s fields and
assigned in `newTestEnvWithClock`.

- [ ] **Step 2: Run the tests and watch them fail**

```bash
cd api && go test ./internal/adapter/http/ -run 'TestAdmin|TestSigningOutRevokes|TestEveryAdminRequest' -v
```

Expected: FAIL — `env.makePlatformAdmin undefined`, then 404s once it compiles.

- [ ] **Step 3: Write the middleware**

Create `api/internal/adapter/http/middleware_admin.go`:

```go
package httpadapter

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// adminGrantTTL is how long one re-authentication opens the admin surface for.
// It is deliberately not extended by activity, unlike the session itself: a
// long admin session is re-authenticated, not renewed silently.
const adminGrantTTL = 30 * time.Minute

// requirePlatformAdmin answers 404 -- not 403 -- to a caller with no
// platform_admins row. A 403 would confirm both that /admin exists and that
// this path is the right one; to everyone else the whole surface must look
// like a typo.
//
// A lookup *failure* is a 500, not a 404. "The database is down" must not read
// as a clean "you are not an admin", or an outage would silently lock the
// operator out with a message saying the page does not exist.
func requirePlatformAdmin(deps Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := RequestScope(r)
			if !ok {
				WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
				return
			}
			isAdmin, err := deps.Admin.IsPlatformAdmin(r.Context(), scope.UserID)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			if !isAdmin {
				writeNotFound(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// auditAdmin writes one admin_audit_log row per request that reaches it,
// reads included. It is middleware rather than a call in each handler because
// a handler that forgets is the failure mode, and middleware cannot forget.
//
// The row is written before the handler runs, so a handler that panics or
// times out still leaves a trace of the attempt. detail carries the route
// pattern's parameters -- never a body, a password or a row value.
func auditAdmin(deps Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := RequestScope(r)
			if !ok {
				WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
				return
			}

			target := ""
			detail := map[string]any{}
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				for i, key := range rctx.URLParams.Keys {
					if i < len(rctx.URLParams.Values) {
						detail[key] = rctx.URLParams.Values[i]
						if target == "" {
							target = rctx.URLParams.Values[i]
						}
					}
				}
			}

			if err := deps.Admin.RecordAudit(r.Context(), usecase.AdminAuditEntry{
				ActorUserID: scope.UserID,
				Action:      r.Method + " " + r.URL.Path,
				Target:      target,
				Detail:      detail,
				IP:          r.RemoteAddr,
				At:          deps.Clock.Now(),
			}); err != nil {
				// An unwritable audit log closes the surface. The alternative
				// -- serve the request and log a warning -- is an admin
				// surface that works fine with auditing silently off, which is
				// the exact state this table exists to make impossible.
				slog.ErrorContext(r.Context(), "admin audit write failed",
					"request_id", middleware.GetReqID(r.Context()), "error", err)
				WriteError(w, http.StatusServiceUnavailable, "AUDIT_UNAVAILABLE",
					"The admin surface is closed because its audit log cannot be written.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireAdminGrant refuses until the caller has re-entered their password
// within adminGrantTTL. Its 401 carries ADMIN_REAUTH_REQUIRED rather than
// UNAUTHENTICATED so the frontend can show a password prompt instead of
// bouncing the operator all the way out to sign-in.
func requireAdminGrant(deps Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			grant, ok := RequestAdminGrant(r)
			if !ok || grant == nil || !grant.After(deps.Clock.Now()) {
				WriteError(w, http.StatusUnauthorized, "ADMIN_REAUTH_REQUIRED",
					"Confirm your password to open the admin surface.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// adminGrantKey is the request-context key requireSession stores the session
// row's admin grant under. Unexported so no other package can collide.
type adminGrantKey struct{}

// RequestAdminGrant reads the grant expiry requireSession placed on r. The
// bool is false for any request that never passed through requireSession.
func RequestAdminGrant(r *http.Request) (*time.Time, bool) {
	grant, ok := r.Context().Value(adminGrantKey{}).(*time.Time)
	return grant, ok
}

func withAdminGrant(ctx context.Context, expiresAt *time.Time) context.Context {
	return context.WithValue(ctx, adminGrantKey{}, expiresAt)
}

// writeNotFound answers exactly what the router's own NotFound handler does,
// so a hidden admin route and a genuinely absent one are byte-identical.
func writeNotFound(w http.ResponseWriter) {
	WriteError(w, http.StatusNotFound, "NOT_FOUND", "That endpoint does not exist.", nil)
}

var _ = errors.Is // kept if unused after edits; remove rather than leaving dead
var _ = domain.ErrNotFound
```

Delete the two `var _ =` lines; they are there only to name the imports you may
or may not need after wiring, and dead code must not ship.

In `middleware_session.go`, store the grant on the context alongside the scope:

```go
			ctx = withAdminGrant(context.WithValue(ctx, scopeKey{}, scope), record.AdminGrantExpiresAt)
```

— matching however the existing code builds the scope context; the point is
that both values are set, from the same `record`.

- [ ] **Step 4: Write the handlers and routes**

Create `api/internal/adapter/http/admin_handlers.go`:

```go
package httpadapter

import (
	"net/http"
	"time"
)

type adminSessionRequest struct {
	Password string `json:"password"`
}

// handleAdminSession is the re-authentication. It answers 204: there is
// nothing to tell the caller that they do not already know, and the grant
// lives on the session row rather than in the response.
func handleAdminSession(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		var req adminSessionRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if err := deps.AdminReauth.Verify(r.Context(), scope.UserID, req.Password); err != nil {
			MapDomainError(w, r, err)
			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		expiresAt := deps.Clock.Now().Add(adminGrantTTL)
		if err := deps.Sessions.GrantAdmin(r.Context(), deps.Tokens.HashToken(cookie.Value), &expiresAt); err != nil {
			MapDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type flagOverrideDTO struct {
	HouseholdID   string `json:"householdId"`
	HouseholdName string `json:"householdName"`
	Enabled       bool   `json:"enabled"`
}

type flagDTO struct {
	Key           string            `json:"key"`
	Description   string            `json:"description"`
	Default       bool              `json:"default"`
	GlobalSet     bool              `json:"globalSet"`
	GlobalEnabled bool              `json:"globalEnabled"`
	Effective     bool              `json:"effective"`
	Orphaned      bool              `json:"orphaned"`
	Overrides     []flagOverrideDTO `json:"overrides"`
}

type flagsResponse struct {
	Flags []flagDTO `json:"flags"`
}

func handleListFlags(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		overview, err := deps.Admin.Overview(r.Context())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		body := flagsResponse{Flags: make([]flagDTO, 0, len(overview))}
		for _, row := range overview {
			dto := flagDTO{
				Key: row.Key, Description: row.Description, Default: row.Default,
				GlobalSet: row.GlobalSet, GlobalEnabled: row.GlobalEnabled,
				Effective: row.Effective, Orphaned: row.Orphaned,
				Overrides: make([]flagOverrideDTO, 0, len(row.Overrides)),
			}
			for _, o := range row.Overrides {
				dto.Overrides = append(dto.Overrides, flagOverrideDTO{
					HouseholdID: o.HouseholdID, HouseholdName: o.HouseholdName, Enabled: o.Enabled,
				})
			}
			body.Flags = append(body.Flags, dto)
		}
		WriteJSON(w, http.StatusOK, body)
	}
}

var _ = time.Minute // remove if unused
```

Delete that last line.

In `router.go`, add the subtree inside the existing `api.Group(func(g chi.Router) { g.Use(requireSession(deps)) ... })`:

```go
			// The admin surface. requirePlatformAdmin answers 404 to everyone
			// else, so this whole subtree is invisible to a household member.
			// auditAdmin wraps it rather than each handler: a handler that
			// forgets to log is the failure mode.
			g.Route("/admin", func(adm chi.Router) {
				adm.Use(requirePlatformAdmin(deps))
				adm.Use(auditAdmin(deps))

				// The one route that must be reachable without a grant --
				// it is how a grant is obtained.
				adm.Group(func(reauth chi.Router) {
					reauth.Use(requireCSRF)
					reauth.Post("/session", handleAdminSession(deps))
				})

				adm.Group(func(granted chi.Router) {
					granted.Use(requireAdminGrant(deps))
					granted.Get("/flags", handleListFlags(deps))
				})
			})
```

Add to `Deps`:

```go
	// Admin and AdminReauth are nil on no deployment: the admin surface is
	// always routed, and requirePlatformAdmin's 404 is what hides it.
	Admin       *usecase.AdminService
	AdminReauth *usecase.AdminReauthService
```

In `errors.go`'s `MapDomainError`, add:

```go
	case errors.Is(err, domain.ErrAdminLocked):
		WriteError(w, http.StatusLocked, "ADMIN_LOCKED",
			"Too many failed attempts. Try again in a few minutes.", nil)
		return
```

Wire the services in `cmd/api/main.go` alongside the existing ones, and add the
four new repositories there.

- [ ] **Step 5: Run the tests and watch them pass**

```bash
cd api && go test ./internal/adapter/http/ -run 'TestAdmin|TestSigningOutRevokes|TestEveryAdminRequest' -v
```

Expected: PASS, all seven.

- [ ] **Step 6: Run the whole route-walk matrix**

```bash
cd api && go test ./internal/adapter/http/ -v
```

`TestEveryProtectedRouteRejectsAnUnauthenticatedCaller` and
`TestEveryMutatingRouteRequiresCSRF` walk every registered route. Both must
still pass with the new subtree; if `POST /admin/session` trips the CSRF
matrix, the fix is to keep `requireCSRF` on it, never to exempt it.

- [ ] **Step 7: Mutation-check the 404**

Change `writeNotFound(w)` in `requirePlatformAdmin` to a 403 `FORBIDDEN`.
`TestAdminRoutesAre404ToANonAdmin` must fail. Put it back.

- [ ] **Step 8: Commit**

```bash
git add api/internal/adapter/http/ api/cmd/api/main.go
git commit -m "feat(api): the admin gate -- platform admin check, re-auth grant, audit middleware"
```

---

### Task 6: `requireFeature`, and flags on the session scope

**Files:**
- Modify: `api/internal/adapter/http/middleware_session.go` (resolve flags onto `Scope`)
- Create: `api/internal/adapter/http/middleware_feature.go`
- Modify: `api/internal/adapter/http/router.go` (gate sign-up and Telegram start)
- Test: `api/internal/adapter/http/feature_flag_api_test.go`

**Interfaces:**
- Consumes: `usecase.AdminService.FlagsFor`, `.GlobalFlags` (Task 4);
  `Scope` (existing).
- Produces:
  - `Scope.Flags domain.FlagSet`
  - `requireFeature(flag domain.Flag) func(http.Handler) http.Handler`

- [ ] **Step 1: Write the failing test**

Create `api/internal/adapter/http/feature_flag_api_test.go`:

```go
package httpadapter_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// TestADisabledFeatureAnswers404 -- not 403. On this install the feature does
// not exist, and 403 would leak the roadmap.
func TestADisabledFeatureAnswers404(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// family_calendar is off by default, and its route is registered.
	rec := env.authedGet(t, "/api/v1/family/calendar", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")

	if err := env.flags.SetGlobal(context.Background(),
		string(domain.FlagFamilyCalendar), true, ""); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}

	rec = env.authedGet(t, "/api/v1/family/calendar", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("after enabling the flag = %d, body %s; want 200", rec.Code, rec.Body.String())
	}
}

// TestAHouseholdOverrideEnablesOnlyThatHousehold.
func TestAHouseholdOverrideEnablesOnlyThatHousehold(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	if err := env.flags.SetHousehold(context.Background(), env.householdID,
		string(domain.FlagFamilyCalendar), true, ""); err != nil {
		t.Fatalf("SetHousehold: %v", err)
	}

	rec := env.authedGet(t, "/api/v1/family/calendar", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("the household with the override = %d, want 200", rec.Code)
	}
}

// TestSignupsOpenGatesThePublicRoute is the pre-auth case: no session exists,
// so there is no household layer to consult, and requireFeature must resolve
// the global set on its own rather than failing or treating it as on.
func TestSignupsOpenGatesThePublicRoute(t *testing.T) {
	env := newTestEnv(t)

	rec := env.do(http.MethodPost, "/api/v1/auth/sign-up", map[string]string{"email": "new@example.test"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("sign-up with signups_open on = %d, want 202", rec.Code)
	}

	if err := env.flags.SetGlobal(context.Background(),
		string(domain.FlagSignupsOpen), false, ""); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}

	rec = env.do(http.MethodPost, "/api/v1/auth/sign-up", map[string]string{"email": "later@example.test"})
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}

// TestAHouseholdOverrideCannotOpenAClosedPublicRoute: household overrides are
// meaningless before there is a household, and must never be treated as "on".
func TestAHouseholdOverrideCannotOpenAClosedPublicRoute(t *testing.T) {
	env := newTestEnv(t)

	if err := env.flags.SetGlobal(context.Background(),
		string(domain.FlagSignupsOpen), false, ""); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}
	if err := env.flags.SetHousehold(context.Background(), env.householdID,
		string(domain.FlagSignupsOpen), true, ""); err != nil {
		t.Fatalf("SetHousehold: %v", err)
	}

	rec := env.do(http.MethodPost, "/api/v1/auth/sign-up", map[string]string{"email": "sneaky@example.test"})
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}
```

Add `flags` (a `*postgres.FeatureFlagRepo`) to `testEnv`, assigned in
`newTestEnvWithClock`.

`/api/v1/family/calendar` does not exist yet. Register it in this task as a
minimal, real route — the Family calendar page is ⬜ in the tracker, and this
gives dark-shipping something honest to gate:

```go
			// The Family calendar's API stub, dark behind its flag. It answers
			// an empty list rather than 501: a flag-gated route must behave
			// like a real route once its flag is on, or the flag proves
			// nothing about the feature it guards.
			g.Group(func(f chi.Router) {
				f.Use(requireFeature(domain.FlagFamilyCalendar))
				f.Get("/family/calendar", handleListCalendarEvents(deps))
			})
```

with, in a new `api/internal/adapter/http/calendar_handlers.go`:

```go
package httpadapter

import "net/http"

// handleListCalendarEvents is the Family calendar's first endpoint. There are
// no events yet -- the feature is ⬜ in docs/FEATURE_TRACKER.md -- so it
// answers an empty list. It exists now because dark-shipping needs a real
// route to prove itself against, and because a 2xx with no body would break
// apiFetch (see CLAUDE.md).
func handleListCalendarEvents(_ Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]any{"events": []any{}})
	}
}
```

- [ ] **Step 2: Run the tests and watch them fail**

```bash
cd api && go test ./internal/adapter/http/ -run 'TestADisabledFeature|TestAHouseholdOverride|TestSignupsOpen' -v
```

Expected: FAIL — `undefined: requireFeature`.

- [ ] **Step 3: Resolve flags onto the scope**

In `middleware_session.go`, after the membership cross-check and before the
context is built:

```go
			// Flags are resolved per request, not cached. One box, few
			// households, and a cache that is stale for a minute after the
			// operator flips a switch is a worse defect than one indexed
			// query. A cache belongs here when a measurement asks for one.
			flags, err := deps.Admin.FlagsFor(ctx, record.HouseholdID)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
```

and add `Flags: flags` to the `Scope` literal, plus the field:

```go
type Scope struct {
	UserID      string
	HouseholdID string
	Membership  domain.Membership
	// Flags is this household's resolved answer for every flag this build
	// defines -- every key present, so a reader never has to interpret an
	// absence.
	Flags domain.FlagSet
}
```

- [ ] **Step 4: Write `requireFeature`**

Create `api/internal/adapter/http/middleware_feature.go`:

```go
package httpadapter

import (
	"net/http"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// requireFeature answers 404 -- not 403 -- unless flag is on for this caller.
// On this install a disabled feature does not exist, and a 403 would confirm a
// route that is meant to be invisible.
//
// It handles the pre-auth routes itself. With no Scope on the request there is
// no household whose overrides could apply, so it resolves the global set
// alone. That fallback is why enforcement is one middleware rather than a
// middleware plus a helper public handlers remember to call: a hand-rolled
// check as a handler's first statement is the shape that gets forgotten on the
// next public route, and forgetting it fails open.
func requireFeature(flag domain.Flag) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if scope, ok := RequestScope(r); ok {
				if !scope.Flags.Enabled(flag) {
					writeNotFound(w)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			flags, err := depsFromRequest(r).Admin.GlobalFlags(r.Context())
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			if !flags.Enabled(flag) {
				writeNotFound(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

`depsFromRequest` does not exist and must not be invented: change the signature
to `requireFeature(deps Deps, flag domain.Flag)` and close over `deps`, the way
every other middleware in this package does. Update the call sites accordingly.

- [ ] **Step 5: Gate the two real routes**

In `router.go`, wrap the sign-up group and the Telegram start group:

```go
			auth.Group(func(su chi.Router) {
				now := func() time.Time { return deps.Clock.Now() }
				su.Use(rateLimitByIP(newIPRateLimiter(signUpRequestsPerIPPerHour, time.Hour, now)))
				su.Use(requireFeature(deps, domain.FlagSignupsOpen))
				su.Post("/sign-up", handleSignUp(deps))
			})
```

The limiter stays outermost: a closed sign-up route must not become a way to
make unlimited flag lookups.

Do the same for `tg.Post("/telegram/start", ...)` with
`domain.FlagTelegramSignIn`, and for the two token routes
`GET /auth/sign-up/{token}` and `POST /auth/sign-up/{token}/complete` — a
half-finished sign-up must not be completable after registration closes, and a
test for that belongs here:

```go
// TestClosingSignupsAlsoClosesTheCompletionRoutes -- otherwise a token minted
// before the switch stays redeemable indefinitely.
func TestClosingSignupsAlsoClosesTheCompletionRoutes(t *testing.T) {
	env := newTestEnv(t)
	env.do(http.MethodPost, "/api/v1/auth/sign-up", map[string]string{"email": "half@example.test"})
	token := env.lastSignupToken(t)

	if err := env.flags.SetGlobal(context.Background(),
		string(domain.FlagSignupsOpen), false, ""); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}

	rec := env.do(http.MethodGet, "/api/v1/auth/sign-up/"+token, nil)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}
```

- [ ] **Step 6: Run the tests and watch them pass**

```bash
cd api && go test ./internal/adapter/http/ -v
```

Expected: PASS, including the whole existing suite.

- [ ] **Step 7: Mutation-check the pre-auth fallback**

In `requireFeature`, change the no-scope branch to `next.ServeHTTP(w, r)`.
`TestSignupsOpenGatesThePublicRoute` must fail. Put it back.

- [ ] **Step 8: Commit**

```bash
git add api/internal/adapter/http/ 
git commit -m "feat(api): requireFeature, and flags resolved onto the request scope"
```

---

### Task 7: `/auth/me` carries the flags and the admin bit

**Files:**
- Modify: `api/internal/adapter/http/auth_handlers.go` (`meResponseBody`, `buildMeResponse`)
- Modify: `api/internal/adapter/http/api_test.go` (nothing new; the assertions live in the test below)
- Test: `api/internal/adapter/http/admin_api_test.go` (append)

**Interfaces:**
- Consumes: `Scope.Flags` (Task 6), `deps.Admin.IsPlatformAdmin` (Task 4).
- Produces: `meResponseBody.IsPlatformAdmin bool` (`json:"isPlatformAdmin"`)
  and `meResponseBody.Features map[string]bool` (`json:"features"`).

- [ ] **Step 1: Write the failing test**

Append to `api/internal/adapter/http/admin_api_test.go`:

```go
// TestMeCarriesEveryDefinedFlag: every key is always present, so the frontend
// never has to decide what a missing key means.
func TestMeCarriesEveryDefinedFlag(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/auth/me", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /auth/me = %d, want 200", rec.Code)
	}

	var body struct {
		IsPlatformAdmin bool            `json:"isPlatformAdmin"`
		Features        map[string]bool `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if body.IsPlatformAdmin {
		t.Fatal("an ordinary owner is not a platform admin")
	}
	for _, def := range domain.AllFlags() {
		if _, present := body.Features[string(def.Flag)]; !present {
			t.Fatalf("features is missing %q: %v", def.Flag, body.Features)
		}
	}
}

func TestMeMarksAPlatformAdmin(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/auth/me", session)
	var body struct {
		IsPlatformAdmin bool `json:"isPlatformAdmin"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if !body.IsPlatformAdmin {
		t.Fatal("a platform admin's me bundle does not say so")
	}
}
```

Add `"encoding/json"` and the `domain` import to that file.

- [ ] **Step 2: Run and watch it fail**

```bash
cd api && go test ./internal/adapter/http/ -run 'TestMe' -v
```

Expected: FAIL — `features is missing "signups_open"`.

- [ ] **Step 3: Implement**

Add to `meResponseBody`:

```go
	// IsPlatformAdmin is what makes the sidebar's /admin link appear. It is
	// not authorization: requirePlatformAdmin decides that, and lying here
	// would only produce a link to a 404.
	IsPlatformAdmin bool `json:"isPlatformAdmin"`
	// Features carries every flag this build defines, resolved for this
	// caller. Every key is present, so the client never interprets an
	// absence.
	Features map[string]bool `json:"features"`
```

`buildMeResponse` takes the caller's already-resolved flags rather than
resolving them a second time — change its signature to accept the `Scope` where
one exists. `completeSignIn` runs before any `Scope` exists (it is the call
that creates the session), so it resolves them itself:

```go
func buildMeResponse(ctx context.Context, deps Deps, userID, householdID string) (meResponseBody, error) {
	// ... existing loads ...
	flags, err := deps.Admin.FlagsFor(ctx, householdID)
	if err != nil {
		return meResponseBody{}, err
	}
	isAdmin, err := deps.Admin.IsPlatformAdmin(ctx, userID)
	if err != nil {
		return meResponseBody{}, err
	}
	return meResponseBody{
		// ... existing fields ...
		IsPlatformAdmin: isAdmin,
		Features:        flags.Strings(),
	}, nil
}
```

- [ ] **Step 4: Run and watch it pass**

```bash
cd api && go test ./internal/adapter/http/ -run 'TestMe' -v
```

- [ ] **Step 5: Commit**

```bash
git add api/internal/adapter/http/
git commit -m "feat(api): /auth/me carries resolved feature flags and the admin bit"
```

---

### Task 8: Writing flags from the admin API

**Files:**
- Modify: `api/internal/adapter/http/admin_handlers.go`
- Modify: `api/internal/adapter/http/router.go`
- Test: `api/internal/adapter/http/admin_api_test.go` (append)

**Interfaces:**
- Consumes: `AdminService.SetGlobalFlag`, `.SetHouseholdFlag`,
  `.ClearHouseholdFlag` (Task 4).
- Produces routes:
  - `PUT /api/v1/admin/flags/{key}` — body `{"enabled": bool}`
  - `PUT /api/v1/admin/flags/{key}/households/{householdID}` — body `{"enabled": bool}`
  - `DELETE /api/v1/admin/flags/{key}/households/{householdID}`
  - error code `UNKNOWN_FLAG` (422)

- [ ] **Step 1: Write the failing test**

```go
func TestAdminCanToggleAFlagGlobally(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)

	rec := env.authed(t, http.MethodPut, "/api/v1/admin/flags/family_calendar",
		map[string]bool{"enabled": true}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT flag = %d, body %s; want 200", rec.Code, rec.Body.String())
	}

	rec = env.authedGet(t, "/api/v1/family/calendar", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("the gated route after enabling = %d, want 200", rec.Code)
	}
}

func TestAdminFlagWritesRefuseAnUnknownKey(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)

	rec := env.authed(t, http.MethodPut, "/api/v1/admin/flags/not_a_flag",
		map[string]bool{"enabled": true}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "UNKNOWN_FLAG")
}

// TestClearingAHouseholdOverrideIsNotTheSameAsTurningItOff.
func TestClearingAHouseholdOverrideIsNotTheSameAsTurningItOff(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)

	env.authed(t, http.MethodPut, "/api/v1/admin/flags/family_calendar",
		map[string]bool{"enabled": true}, session, csrf)
	env.authed(t, http.MethodPut,
		"/api/v1/admin/flags/family_calendar/households/"+env.householdID,
		map[string]bool{"enabled": false}, session, csrf)

	rec := env.authedGet(t, "/api/v1/family/calendar", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")

	rec = env.authed(t, http.MethodDelete,
		"/api/v1/admin/flags/family_calendar/households/"+env.householdID, nil, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE override = %d, want 200", rec.Code)
	}

	// With the override gone the household falls back to the global true.
	rec = env.authedGet(t, "/api/v1/family/calendar", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("after clearing the override = %d, want 200", rec.Code)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

```bash
cd api && go test ./internal/adapter/http/ -run 'TestAdminCanToggle|TestAdminFlagWrites|TestClearingAHousehold' -v
```

- [ ] **Step 3: Implement the handlers**

```go
type setFlagRequest struct {
	Enabled bool `json:"enabled"`
}

// handleSetGlobalFlag writes the install-wide override. It answers the whole
// refreshed flag list rather than 204, so the screen never has to guess what
// the write did to the effective values.
func handleSetGlobalFlag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		var req setFlagRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		key := chi.URLParam(r, "key")
		if err := deps.Admin.SetGlobalFlag(r.Context(), key, req.Enabled, scope.UserID); err != nil {
			MapDomainError(w, r, err)
			return
		}
		handleListFlags(deps)(w, r)
	}
}

func handleSetHouseholdFlag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		var req setFlagRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		key := chi.URLParam(r, "key")
		householdID := chi.URLParam(r, "householdID")
		if err := deps.Admin.SetHouseholdFlag(r.Context(), householdID, key, req.Enabled, scope.UserID); err != nil {
			MapDomainError(w, r, err)
			return
		}
		handleListFlags(deps)(w, r)
	}
}

// handleClearHouseholdFlag removes the override. "No opinion" and "explicitly
// off" are different states; DELETE is how the first is reached.
func handleClearHouseholdFlag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := chi.URLParam(r, "key")
		householdID := chi.URLParam(r, "householdID")
		if err := deps.Admin.ClearHouseholdFlag(r.Context(), householdID, key); err != nil {
			MapDomainError(w, r, err)
			return
		}
		handleListFlags(deps)(w, r)
	}
}
```

Routes, inside the granted group from Task 5:

```go
					granted.Group(func(w chi.Router) {
						w.Use(requireCSRF)
						w.Put("/flags/{key}", handleSetGlobalFlag(deps))
						w.Put("/flags/{key}/households/{householdID}", handleSetHouseholdFlag(deps))
						w.Delete("/flags/{key}/households/{householdID}", handleClearHouseholdFlag(deps))
					})
```

In `errors.go`:

```go
	case errors.Is(err, domain.ErrUnknownFlag):
		WriteError(w, http.StatusUnprocessableEntity, "UNKNOWN_FLAG",
			"That feature flag does not exist in this build.", nil)
		return
```

- [ ] **Step 4: Run and watch them pass**

```bash
cd api && go test ./internal/adapter/http/ -v
```

- [ ] **Step 5: Mutation-check the clear**

Change `ClearHouseholdFlag` to call `SetHousehold(..., false, ...)`.
`TestClearingAHouseholdOverrideIsNotTheSameAsTurningItOff` must fail on its
last assertion. Put it back.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/http/
git commit -m "feat(api): admin routes for setting and clearing feature flags"
```

---

### Task 9: `adminctl` commands

**Files:**
- Modify: `api/cmd/adminctl/main.go`
- Test: `api/cmd/adminctl/main_test.go` (append)

**Interfaces:**
- Consumes: `postgres.NewPlatformAdminRepo`, `postgres.NewAdminReauthAttemptRepo`
  (Task 3); `usecase.UserRepository` (existing).
- Produces commands: `grant-platform-admin`, `revoke-platform-admin`,
  `list-platform-admins`, `unlock-admin`.

- [ ] **Step 1: Write the failing test**

```go
// TestGrantPlatformAdminNeedsAnEmail: every one of these commands resolves a
// person by address, and a missing flag must say so rather than acting on
// whoever happens to be first in the table.
func TestGrantPlatformAdminNeedsAnEmail(t *testing.T) {
	err := runGrantPlatformAdmin(context.Background(), nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "--email") {
		t.Fatalf("runGrantPlatformAdmin with no flags = %v, want an error naming --email", err)
	}
}

// TestUsageListsEveryAdminCommand keeps the help text honest: a command
// nobody can discover is a command nobody uses.
func TestUsageListsEveryAdminCommand(t *testing.T) {
	for _, want := range []string{
		"grant-platform-admin", "revoke-platform-admin",
		"list-platform-admins", "unlock-admin",
	} {
		if !strings.Contains(usage, want) {
			t.Fatalf("usage does not mention %q", want)
		}
	}
}
```

- [ ] **Step 2: Run and watch it fail**

```bash
cd api && go test ./cmd/adminctl/ -v
```

- [ ] **Step 3: Implement**

Extend `usage`:

```
  grant-platform-admin --email= [--note=]      make this person an operator of this install
  revoke-platform-admin --email=               take that away
  list-platform-admins                         who can reach /admin
  unlock-admin [--email=]                      clear a locked admin re-auth (defaults to the
                                                seeded owner)
```

Add the repositories to `run`'s wiring block:

```go
	platformAdmins := postgres.NewPlatformAdminRepo(db)
	adminAttempts := postgres.NewAdminReauthAttemptRepo(db)
```

Add the cases:

```go
	case "grant-platform-admin":
		fs := flag.NewFlagSet("grant-platform-admin", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		email := fs.String("email", "", "the person's email address")
		note := fs.String("note", "", "why they have this")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return runGrantPlatformAdmin(ctx, users, platformAdmins, *email, *note)
	case "revoke-platform-admin":
		// ... same shape, calling runRevokePlatformAdmin
	case "list-platform-admins":
		return runListPlatformAdmins(ctx, platformAdmins)
	case "unlock-admin":
		// ... resolves the address the way unlock-household does, then
		// adminAttempts.ClearFailures
```

and the functions, each with the doc comment explaining why it is a CLI
command:

```go
// runGrantPlatformAdmin is the only way a platform admin comes into existence.
// There is deliberately no HTTP route for this: an admin surface that can mint
// its own admins turns one stolen session into permanent access, so the box
// itself is the boundary.
func runGrantPlatformAdmin(ctx context.Context, users usecase.UserRepository,
	admins usecase.PlatformAdminRepository, email, note string) error {
	if email == "" {
		return errors.New("grant-platform-admin needs --email")
	}
	user, err := users.ByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("look up %s: %w", email, err)
	}
	if err := admins.Grant(ctx, user.User.ID, note); err != nil {
		return err
	}
	fmt.Printf("%s can now reach /admin. They will be asked for their password on the way in.\n", email)
	return nil
}
```

Adjust the test's call to match the final signature — the plan's test above
calls it with three arguments; use the real one.

- [ ] **Step 4: Run and watch it pass**

```bash
cd api && go test ./cmd/adminctl/ -v
```

- [ ] **Step 5: Verify by hand against the dev database**

```bash
make dev            # in one shell
make seed
cd api && go run ./cmd/adminctl grant-platform-admin --email=andreas@example.com --note="the operator"
cd api && go run ./cmd/adminctl list-platform-admins
```

Expected: the grant prints its confirmation, and the list shows one row.

- [ ] **Step 6: Commit**

```bash
git add api/cmd/adminctl/
git commit -m "feat(adminctl): grant, revoke, list platform admins and unlock admin re-auth"
```

---

### Task 10: The frontend contract — schema, `useFeature`, the sidebar link

**Files:**
- Modify: `web/src/features/auth/schemas.ts`
- Create: `web/src/features/admin/useFeature.ts`
- Modify: `web/src/features/shell/Sidebar.tsx`
- Test: `web/src/features/admin/useFeature.test.tsx`
- Test: `web/src/features/shell/Sidebar.test.tsx` (append)

**Interfaces:**
- Consumes: `/auth/me`'s new fields (Task 7); `useMe` (existing).
- Produces:
  - `meQuerySchema` gains `isPlatformAdmin: z.boolean().default(false)` and
    `features: z.record(z.string(), z.boolean()).default({})`
  - `useFeature(key: string): boolean`

- [ ] **Step 1: Write the failing test**

Create `web/src/features/admin/useFeature.test.tsx`:

```tsx
import { describe, expect, it } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useFeature } from "./useFeature";
import { meQueryKey } from "../auth/useAuth";

function wrapperWithFeatures(features: Record<string, boolean>) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(meQueryKey, {
    user: { id: "u1", email: "a@b.c", displayName: "A", avatarInitial: "A" },
    household: {
      id: "h1", name: "H", familyName: "H", primaryCurrency: "SGD",
      showSecondaryCurrency: false, secondaryCurrency: "", fxRateMode: "manual",
    },
    membership: { id: "m1", householdId: "h1", userId: "u1", role: "owner", capabilities: [] },
    capabilities: [],
    spaces: [],
    isPlatformAdmin: false,
    features,
  });
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("useFeature", () => {
  it("is true when the flag is on", async () => {
    const { result } = renderHook(() => useFeature("family_calendar"), {
      wrapper: wrapperWithFeatures({ family_calendar: true }),
    });
    await waitFor(() => expect(result.current).toBe(true));
  });

  // An unknown key must close a door, never open one -- the same fail-closed
  // rule the server's FlagSet.Enabled follows.
  it("is false for a key the server did not send", async () => {
    const { result } = renderHook(() => useFeature("typo"), {
      wrapper: wrapperWithFeatures({ family_calendar: true }),
    });
    await waitFor(() => expect(result.current).toBe(false));
  });

  it("is false before /auth/me has answered", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useFeature("family_calendar"), { wrapper });
    expect(result.current).toBe(false);
  });
});
```

- [ ] **Step 2: Run and watch it fail**

```bash
cd web && npx vitest run src/features/admin/useFeature.test.tsx
```

- [ ] **Step 3: Implement**

In `schemas.ts`, inside `meQuerySchema`:

```ts
  // Defaulted rather than required: an older server that does not send these
  // must not make the whole bundle fail to parse, which would sign everyone
  // out. Both defaults are the closed state.
  isPlatformAdmin: z.boolean().default(false),
  features: z.record(z.string(), z.boolean()).default({}),
```

Create `web/src/features/admin/useFeature.ts`:

```ts
// useFeature answers whether a feature flag is on for the signed-in caller.
//
// It reads the already-cached /auth/me bundle rather than fetching anything:
// the server resolved these once, per request, and a second source of truth
// in the client would drift from the guard that actually enforces.
//
// This is not enforcement. requireFeature on the server is; this only avoids
// showing a door that opens onto a 404.
import { useMe } from "../auth/useAuth";

export function useFeature(key: string): boolean {
  const { data } = useMe();
  return data?.features?.[key] === true;
}
```

In `Sidebar.tsx`, render the admin link only when `me.isPlatformAdmin`:

```tsx
      {me.isPlatformAdmin && (
        <Link
          to="/admin"
          className="mt-6 inline-flex min-h-11 items-center text-muted"
        >
          Admin
        </Link>
      )}
```

with a test appended to `Sidebar.test.tsx` asserting the link is absent for an
ordinary owner and present for an admin.

- [ ] **Step 4: Run and watch them pass**

```bash
cd web && npx vitest run src/features/admin src/features/shell
```

- [ ] **Step 5: Commit**

```bash
git add web/src/features/auth/schemas.ts web/src/features/admin web/src/features/shell
git commit -m "feat(web): feature flags and the admin bit on the me bundle"
```

---

### Task 11: The admin shell and the flags screen

**Files:**
- Create: `web/src/features/admin/AdminGate.tsx`
- Create: `web/src/features/admin/AdminShell.tsx`
- Create: `web/src/features/admin/AdminFlagsPage.tsx`
- Create: `web/src/features/admin/useAdmin.ts`
- Modify: `web/src/routes/router.tsx`
- Test: `web/src/features/admin/AdminGate.test.tsx`
- Test: `web/src/features/admin/AdminFlagsPage.test.tsx`

**Interfaces:**
- Consumes: `apiFetch`, `ApiError` (existing); the admin routes (Tasks 5, 8).
- Produces:
  - `useAdminFlags()`, `useSetGlobalFlag()`, `useSetHouseholdFlag()`,
    `useClearHouseholdFlag()`, `useAdminSession()` in `useAdmin.ts`
  - lazy routes `/admin` and `/admin/flags`

- [ ] **Step 1: Write the failing tests**

`AdminGate.test.tsx` asserts the three states:

```tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AdminGate } from "./AdminGate";
import { ApiError } from "../../api/client";

function renderGate(error: ApiError | null) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <AdminGate error={error} onSubmit={vi.fn()}>
        <div>the admin surface</div>
      </AdminGate>
    </QueryClientProvider>,
  );
}

describe("AdminGate", () => {
  it("renders the surface when nothing has failed", async () => {
    renderGate(null);
    await waitFor(() => expect(screen.getByText("the admin surface")).toBeInTheDocument());
  });

  // The distinct code is the whole reason the server sends one: a password
  // prompt, not a bounce to sign-in.
  it("asks for the password on ADMIN_REAUTH_REQUIRED", async () => {
    renderGate(new ApiError(401, "ADMIN_REAUTH_REQUIRED", "Confirm your password."));
    await waitFor(() => expect(screen.getByLabelText(/password/i)).toBeInTheDocument());
    expect(screen.queryByText("the admin surface")).not.toBeInTheDocument();
  });

  // A non-admin must see exactly what any other wrong URL gives them.
  it("renders not-found on a 404", async () => {
    renderGate(new ApiError(404, "NOT_FOUND", "That endpoint does not exist."));
    await waitFor(() => expect(screen.getByText(/not found/i)).toBeInTheDocument());
    expect(screen.queryByLabelText(/password/i)).not.toBeInTheDocument();
  });

  it("says so while the admin surface is locked", async () => {
    renderGate(new ApiError(423, "ADMIN_LOCKED", "Too many failed attempts."));
    await waitFor(() => expect(screen.getByText(/too many failed attempts/i)).toBeInTheDocument());
  });
});
```

`AdminFlagsPage.test.tsx` asserts the three states of one flag — default, an
explicit global value, and a household override — render distinguishably, and
that clicking a toggle issues the `PUT`. Mock `apiFetch` with `vi.mock`, the
way the existing feature tests in this repo do.

- [ ] **Step 2: Run and watch them fail**

```bash
cd web && npx vitest run src/features/admin
```

- [ ] **Step 3: Implement the hooks**

```ts
// Query and mutation hooks over the admin routes. Every one of them can fail
// with ADMIN_REAUTH_REQUIRED at any moment -- the grant expires on a wall
// clock, not on activity -- so the page passes the error down to AdminGate
// rather than each hook handling it.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";

export const adminFlagsKey = ["admin", "flags"] as const;

export type AdminFlag = {
  key: string;
  description: string;
  default: boolean;
  globalSet: boolean;
  globalEnabled: boolean;
  effective: boolean;
  orphaned: boolean;
  overrides: { householdId: string; householdName: string; enabled: boolean }[];
};

export function useAdminFlags() {
  return useQuery({
    queryKey: adminFlagsKey,
    queryFn: () => apiFetch<{ flags: AdminFlag[] }>("/api/v1/admin/flags"),
  });
}

export function useAdminSession() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (password: string) =>
      apiFetch<void>("/api/v1/admin/session", {
        method: "POST",
        body: JSON.stringify({ password }),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: adminFlagsKey }),
  });
}

export function useSetGlobalFlag() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: { key: string; enabled: boolean }) =>
      apiFetch<{ flags: AdminFlag[] }>(`/api/v1/admin/flags/${encodeURIComponent(vars.key)}`, {
        method: "PUT",
        body: JSON.stringify({ enabled: vars.enabled }),
      }),
    onSuccess: (data) => queryClient.setQueryData(adminFlagsKey, data),
  });
}
```

plus `useSetHouseholdFlag` and `useClearHouseholdFlag` in the same shape,
hitting `/api/v1/admin/flags/{key}/households/{householdId}` with `PUT` and
`DELETE`.

- [ ] **Step 4: Implement the components**

- `AdminGate` — takes the current `ApiError | null` and renders: the children,
  the password prompt (`ADMIN_REAUTH_REQUIRED`), the app's not-found screen
  (404), or the lockout message (`ADMIN_LOCKED`). Fail closed: an error code it
  does not recognise renders not-found, never the children.
- `AdminShell` — the operator chrome: a header that says **Operator** in as
  many words and is visually distinct from the household app, plus an outlet.
  Knowing which surface you are on must not require reading the URL.
- `AdminFlagsPage` — one row per flag: key, description, the compile-time
  default, a three-state global control (default / on / off), the count of
  household overrides with a disclosure listing them, and a delete control per
  override. Orphaned rows render in their own group headed "Orphaned — safe to
  delete", with the explanation that they enable nothing.

- [ ] **Step 5: Wire the lazy routes**

In `router.tsx`, alongside the existing routes, under `RequireAuth` but
**outside** `AppShell` — the admin surface has its own chrome:

```tsx
// Lazily loaded so no household member ever downloads the admin bundle. The
// server's requirePlatformAdmin is what enforces; this only keeps the code
// out of everyone else's tab.
const AdminShell = lazy(() =>
  import("../features/admin/AdminShell").then((m) => ({ default: m.AdminShell })),
);
const AdminFlagsPage = lazy(() =>
  import("../features/admin/AdminFlagsPage").then((m) => ({ default: m.AdminFlagsPage })),
);
```

with `/admin` redirecting to `/admin/flags`, matching how `/money` and
`/marriage` give a bare space URL a real page.

- [ ] **Step 6: Run the frontend suite**

```bash
cd web && npx vitest run && npx tsc --noEmit
```

- [ ] **Step 7: Mutation-check the gate**

Change `AdminGate`'s unrecognised-code branch to render its children.
The 404 test must fail. Put it back.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/admin web/src/routes/router.tsx
git commit -m "feat(web): the admin shell, re-auth gate and feature flag screen"
```

---

### Task 12: Verify in a browser, then write the docs

**Files:**
- Create: `docs/adr/0005-platform-admin-authorization.md`
- Modify: `docs/FEATURE_TRACKER.md`
- Modify: `docs/SYSTEM_DESIGN.md`
- Modify: `docs/INFRASTRUCTURE.md`
- Modify: `docs/LEARNING.md`
- Create: `docs/superpowers/plans/2026-09-01-hearth-admin-surface-verification.md`

- [ ] **Step 1: Run the full suite**

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make lint && make test
```

Both green before anything below. Paste the real output into the verification
document; do not summarise it.

- [ ] **Step 2: Walk it in a real browser**

`make dev`, then `make seed`, then
`go run ./cmd/adminctl grant-platform-admin --email=<the seeded owner>`.
Drive http://localhost:5173 with the browser tools and record each criterion
pass or fail in
`docs/superpowers/plans/2026-09-01-hearth-admin-surface-verification.md`:

1. A signed-in **non-admin** typing `/admin` sees the app's ordinary not-found
   screen, and the network tab shows a **404**, not a 403.
2. The non-admin's sidebar has no Admin link.
3. The admin's sidebar has one.
4. Clicking it asks for the password before showing anything.
5. The wrong password is refused and the surface stays closed.
6. Three wrong passwords lock the admin surface — and signing out and back in
   with the *correct* password still works, proving the household is untouched.
7. `go run ./cmd/adminctl unlock-admin --email=…` reopens it.
8. The right password opens `/admin/flags`, which lists every flag with its
   description and default.
9. Turning `family_calendar` on globally makes `GET /api/v1/family/calendar`
   answer 200 (check the network tab).
10. Overriding it **off** for the seeded household closes it again for that
    household only.
11. Deleting that override reopens it — proving "no opinion" differs from
    "explicitly off".
12. Turning `signups_open` off makes the public sign-up form's POST answer 404,
    and turning it back on restores it.
13. Waiting out the grant (or clearing `admin_grant_expires_at` by hand) makes
    the next admin request show the password prompt again rather than signing
    the operator out.
14. `SELECT count(*) FROM admin_audit_log` grows by one for a plain page view.
15. Signing out closes the admin surface without another request.

Screenshot criteria 1, 4, 8 and 10. A criterion that fails is a defect to fix,
not a note to write.

- [ ] **Step 3: Write ADR 0005**

`docs/adr/0005-platform-admin-authorization.md`, in the shape of the existing
four: context, decision, consequences. It must say, explicitly:

- Platform admin is an axis orthogonal to household role and capability, and
  why merging them was rejected.
- Admins are created only by `adminctl`, and why there is no HTTP route.
- The re-auth grant, its 30 minutes, and why it lives on the session row.
- **That this narrows, rather than overturns, `adminctl`'s written position
  that operator actions "have no business behind an authenticated HTTP
  endpoint"**: mutations stay on the CLI; reads move to the web behind re-auth
  and an audit log. Name the file and the line.
- The separate re-auth ledger, and the household-lockout defect it avoids.

- [ ] **Step 4: Update the trackers**

- `docs/FEATURE_TRACKER.md` — via the `hearth-product-driver` skill. Add rows
  for the admin surface, feature flags and the audit log; the Family calendar
  API stub is 🟡 with its gap named ("route and flag exist; no events, no UI").
  **Recount the summary table**; do not guess it.
- `docs/SYSTEM_DESIGN.md` — via the `maintaining-system-design` skill: the new
  authorization axis, five new tables, the `/admin` middleware chain, and
  `requireFeature` in the request-flow diagram.
- `docs/LEARNING.md` — one entry: admin re-auth failures were about to be
  written to `login_attempts`, whose lockout is household-scoped, which would
  have let an operator's mistyped password lock their household out of the
  product. What would have caught it sooner: reading the table's own doc
  comment before reusing it.
- `docs/INFRASTRUCTURE.md` — no new external service in this slice; note that
  explicitly rather than leaving the file untouched, so the next reader knows
  it was considered.

- [ ] **Step 5: Commit**

```bash
git add docs/
git commit -m "docs: ADR 0005, tracker and system design for the admin surface"
```

---

## Self-review

**Spec coverage (§ by §):** §1 boundary comments — Task 2. §2.1 identity and
adminctl-only creation — Tasks 1, 2, 3, 9. §2.2 re-auth grant and the separate
ledger — Tasks 1, 3, 4, 5. §2.3 middleware chain — Task 5. §2.4 audit log —
Tasks 1, 3, 5. §3.1–3.3 registry and resolution — Task 2. §3.4 enforcement,
including the pre-auth fallback — Task 6. §3.5 the me bundle — Task 7. §3.6 the
admin screen and its three states — Tasks 8, 11. §7 frontend shell — Tasks 10,
11. §8 testing — every task's own steps, plus Task 12's browser walk. §9
rollout steps 1–3 — this plan; steps 4–6 are out of scope by design. §10 docs —
Task 12.

**Not covered, deliberately:** spec §4 (database browse), §5 (outbound message
inspector), §6 (households and metrics), and the `/admin/audit` *screen* —
the audit log is written and readable through the repository, and its screen
belongs with the plan that adds the other admin pages.

**Names checked across tasks:** `PlatformAdminRepository.Get/Grant/Revoke/List`;
`FeatureFlagRepository.OverridesFor/GlobalOverrides/AllHouseholdOverrides/
SetGlobal/SetHousehold/ClearHousehold`; `AdminService.IsPlatformAdmin/FlagsFor/
GlobalFlags/Overview/SetGlobalFlag/SetHouseholdFlag/ClearHouseholdFlag/
RecordAudit/RecentAudit`; `AdminReauthService.Verify`;
`SessionRepository.GrantAdmin`; `SessionRecord.AdminGrantExpiresAt`;
`Scope.Flags`; `requirePlatformAdmin/auditAdmin/requireAdminGrant/requireFeature`;
`useFeature/useAdminFlags/useAdminSession/useSetGlobalFlag/useSetHouseholdFlag/
useClearHouseholdFlag`. `requireFeature` takes `(deps Deps, flag domain.Flag)`
everywhere — Task 6 corrects its own first draft inline, and Tasks 8 and 11 use
the corrected form.
