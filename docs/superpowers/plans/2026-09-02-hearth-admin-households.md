# Admin households and metrics — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the platform admin a `/admin/households` list with four
counters and an explicit search over households and members, and a read-only
`/admin/households/{id}` drill-in showing members, pending invites and the
household's sign-in lockout — with "active" meaning *used*, via a throttled
`sessions.last_seen_at`.

**Architecture:** One new usecase service (`AdminDirectoryService`) over one
new read-only port (`AdminDirectoryRepository`) that is the only port in the
product reading across household boundaries. Two `GET` routes inside the
existing `/admin` granted group, so re-auth, audit and CSRF apply by
construction. The session middleware gains a once-an-hour `Touch`. Two lazy
React pages under the existing `AdminShell`.

**Tech Stack:** Go 1.24, chi v5, pgx/v5 + sqlc (`go tool sqlc`), goose
migrations, testcontainers; React 19 + TypeScript, TanStack Router + Query,
Zod, Tailwind, Vitest.

**Spec:** `docs/superpowers/specs/2026-09-02-hearth-admin-households-design.md`
— read it first; the plan argues from it and does not repeat its reasoning.

**Branch:** `admin-households`, already created off `main` with the spec
committed (`1d2d998`). Work there.

## Global Constraints

- **Clean architecture, enforced by `make lint-arch`.** `internal/domain`
  imports only the standard library; `internal/usecase` may add
  `internal/domain`; everything else is `internal/adapter/**` or `cmd/**`.
  No pgx, chi or JSON type crosses out of the adapter layer. Holds in test
  files too.
- **Authorisation exists only in the HTTP layer.** `AdminDirectoryService`
  takes no actor parameter. The granted group's four guards are the only
  gate.
- **Every 2xx except 204 carries a JSON body.** Both new routes answer 200
  with a body. Slices are built with `make`, never left nil, so an empty
  list serialises as `[]`.
- **Fail closed on values you did not construct.** The `MemberChannel`
  switch in the DTO mapper has a `default` that returns an error.
- **Missing row is `domain.ErrNotFound` at the adapter**, never
  `pgx.ErrNoRows` further up. `translate` already does this.
- **No money on either screen.** Response key sets are asserted exactly.
- **`sessions.last_seen_at` is read as `COALESCE(last_seen_at, created_at)`**
  in exactly one SQL file, `queries/admin_directory.sql`.
- **Every request under `/admin` is one audit row.** No refetch on window
  focus; no fetch on keystroke.
- **`go` lives at `/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin`** and the
  Go suite needs `DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock`
  and `TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`. Every
  `go test` command below assumes both are exported.
- **Comments state invariants, not enumerations** — "every caller" survives a
  new caller; "three of four" does not (admin-surface handover §8).
- **Commit after every task** with a message in the repository's style,
  ending with the `Co-Authored-By` and `Claude-Session` trailers the session
  uses.

## File structure

**Backend — created**

| File | Job |
|---|---|
| `api/migrations/00013_session_last_seen.sql` | one nullable column |
| `api/internal/adapter/postgres/queries/admin_directory.sql` | the eight sqlc queries, and the only place `COALESCE(last_seen_at, created_at)` appears |
| `api/internal/adapter/postgres/admin_directory_repo.go` | `AdminDirectoryRepo` implementing the port |
| `api/internal/adapter/postgres/admin_directory_repo_test.go` | testcontainers tests for the SQL |
| `api/internal/usecase/admin_directory.go` | `AdminDirectoryService`: clamp, windows, truncation, lockout |
| `api/internal/usecase/admin_directory_test.go` | service tests against doubles |
| `api/internal/adapter/http/admin_directory_handlers.go` | two handlers and their DTOs |
| `api/internal/adapter/http/admin_directory_api_test.go` | route tests through the real router |
| `api/internal/adapter/http/session_touch_api_test.go` | the touch rule through the real router |

**Backend — modified**

| File | Change |
|---|---|
| `api/internal/adapter/postgres/queries/identity.sql` | `GetLiveSession` selects `last_seen_at`; new `TouchSession` |
| `api/internal/adapter/postgres/session_repo.go` | `Touch`; `ByTokenHash` maps `LastSeenAt` |
| `api/internal/adapter/postgres/admin_repo_test.go` | one-column test for `Touch` beside the grant one |
| `api/internal/usecase/ports.go` | `SessionRecord.LastSeenAt`, `SessionRepository.Touch`, the new port and its structs |
| `api/internal/usecase/testdouble_test.go` | `sessionDouble.Touch` |
| `api/internal/adapter/http/middleware_session.go` | the touch, beside the extend |
| `api/internal/adapter/http/middleware_admin.go` | `Detail.query` |
| `api/internal/adapter/http/admin_api_test.go` | the three gate tests learn the two new routes; one query-string test |
| `api/internal/adapter/http/router.go` | `Deps.AdminDirectory`; two `Get`s in the granted group |
| `api/internal/adapter/http/api_test.go` | wires the service into the test env |
| `api/cmd/api/main.go` | wires the service |

**Frontend — created**

| File | Job |
|---|---|
| `web/src/features/admin/adminDirectorySchemas.ts` | strict zod mirrors of the two responses |
| `web/src/features/admin/useAdminDirectory.ts` | two query hooks; the reauth-closes-surface effect |
| `web/src/features/admin/directoryCopy.ts` | relative and exact time labels, the page's strings |
| `web/src/features/admin/directoryCopy.test.ts` | boundaries of the time labels |
| `web/src/features/admin/AdminHouseholdsPage.tsx` | search form, tiles, table, five states |
| `web/src/features/admin/AdminHouseholdsPage.test.tsx` | |
| `web/src/features/admin/AdminHouseholdPage.tsx` | drill-in: header, lockout, members, invites |
| `web/src/features/admin/AdminHouseholdPage.test.tsx` | |

**Frontend — modified**

| File | Change |
|---|---|
| `web/src/features/admin/AdminShell.tsx` | Flags · Households nav via `useMatchRoute` |
| `web/src/routes/router.tsx` | two lazy routes; `validateSearch` for `q`/`limit` |
| `web/src/features/admin/adminBundleSplit.test.ts` | pins both new pages out of the main bundle |

**Docs — modified in Task 10**: `docs/SYSTEM_DESIGN.md`,
`docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`, the admin-surface spec,
`docs/ADMIN_SURFACE_HANDOVER.md`, `docs/HANDOVER.md`, `deploy/README.md`.

---

### Task 1: `sessions.last_seen_at` and `SessionRepository.Touch`

**Files:**
- Create: `api/migrations/00013_session_last_seen.sql`
- Modify: `api/internal/adapter/postgres/queries/identity.sql:81-91`
- Modify: `api/internal/adapter/postgres/session_repo.go`
- Modify: `api/internal/usecase/ports.go:136-157`
- Modify: `api/internal/usecase/testdouble_test.go` (`sessionRow`, `sessionDouble`)
- Test: `api/internal/adapter/postgres/admin_repo_test.go`

**Interfaces:**
- Produces: `usecase.SessionRecord.LastSeenAt *time.Time`;
  `usecase.SessionRepository.Touch(ctx, tokenHash []byte, at time.Time) error`.
  Task 2 calls `Touch` from the middleware; Task 5's SQL reads the column.

- [ ] **Step 1: Write the failing repository test**

Append to `api/internal/adapter/postgres/admin_repo_test.go`, after
`TestExtendingASessionKeepsItsAdminGrant`:

```go
// TestTouchWritesOnlyLastSeenAt is Touch's half of the one-column rule
// TestExtendingASessionKeepsItsAdminGrant states for Extend and GrantAdmin:
// each of the three writes its own column and none can overwrite another's.
func TestTouchWritesOnlyLastSeenAt(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	users := postgres.NewUserRepo(db)
	households := postgres.NewHouseholdRepo(db)
	sessions := postgres.NewSessionRepo(db)

	household, err := households.Create(ctx, domain.Household{Name: "Test", FamilyName: "Test", PrimaryCurrency: "SGD"})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	user, err := users.Create(ctx, "touch@example.test", "", "Touch")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	tokenHash := []byte("a-32-byte-looking-token-hash-tch")
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)
	if err := sessions.Create(ctx, tokenHash, user.ID, household.ID, expiry); err != nil {
		t.Fatalf("create session: %v", err)
	}
	grant := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Millisecond)
	if err := sessions.GrantAdmin(ctx, tokenHash, &grant); err != nil {
		t.Fatalf("GrantAdmin: %v", err)
	}

	before, err := sessions.ByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("ByTokenHash before touch: %v", err)
	}
	if before.LastSeenAt != nil {
		t.Fatalf("LastSeenAt before any touch = %v, want nil", before.LastSeenAt)
	}

	seen := time.Now().UTC().Truncate(time.Millisecond)
	if err := sessions.Touch(ctx, tokenHash, seen); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	after, err := sessions.ByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("ByTokenHash after touch: %v", err)
	}
	if after.LastSeenAt == nil || !after.LastSeenAt.Equal(seen) {
		t.Fatalf("LastSeenAt = %v, want %v", after.LastSeenAt, seen)
	}
	if !after.ExpiresAt.Equal(expiry) {
		t.Fatalf("Touch moved expires_at: %v, want %v", after.ExpiresAt, expiry)
	}
	if after.AdminGrantExpiresAt == nil || !after.AdminGrantExpiresAt.Equal(grant) {
		t.Fatalf("Touch changed the admin grant: %v, want %v", after.AdminGrantExpiresAt, grant)
	}
}
```

- [ ] **Step 2: Run it and watch it fail to compile**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestTouchWritesOnlyLastSeenAt -count=1
```
Expected: compile error — `sessions.Touch undefined`, `before.LastSeenAt undefined`.

- [ ] **Step 3: The migration**

Create `api/migrations/00013_session_last_seen.sql`:

```sql
-- +goose Up

-- When this session was last used, refreshed by requireSession at most once
-- an hour (middleware_session.go's sessionTouchInterval). It exists so the
-- operator's "active in the last 7 days" counter means *used*, not *signed
-- in*: a session lives 30 days and is extended in place, so created_at
-- alone reads a daily user as gone for a month.
--
-- NULL means "not touched since this column existed". Every reader treats
-- that as created_at -- COALESCE(last_seen_at, created_at) is the one
-- expression for "when was this session last used", and it lives in
-- queries/admin_directory.sql alone so it cannot drift. No backfill, no
-- index: the spec's §3 says when an index would be earned.
ALTER TABLE sessions ADD COLUMN last_seen_at timestamptz;

-- +goose Down
ALTER TABLE sessions DROP COLUMN last_seen_at;
```

- [ ] **Step 4: The queries**

In `api/internal/adapter/postgres/queries/identity.sql`, change
`GetLiveSession` to select the column, and add `TouchSession` after
`GrantAdminSession`:

```sql
-- name: GetLiveSession :one
SELECT id, user_id, household_id, expires_at, admin_grant_expires_at, last_seen_at FROM sessions
WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now();
```

```sql
-- name: TouchSession :exec
-- One column, the same rule ExtendSession and GrantAdminSession follow:
-- expires_at, admin_grant_expires_at and last_seen_at are each written by
-- exactly one statement, so none can silently undo another.
UPDATE sessions SET last_seen_at = $2 WHERE token_hash = $1;
```

Regenerate:

```bash
make sqlc
```
Expected: `sqlcgen/identity.sql.go` now has `LastSeenAt pgtype.Timestamptz`
on `GetLiveSessionRow` and a `TouchSession(ctx, TouchSessionParams{TokenHash, LastSeenAt})`
method. Open the file and confirm the field names before writing the repo.

- [ ] **Step 5: The port**

In `api/internal/usecase/ports.go`, add the field to `SessionRecord` and the
method to `SessionRepository`:

```go
type SessionRecord struct {
	UserID      string
	HouseholdID string
	ExpiresAt   time.Time
	// AdminGrantExpiresAt is nil for every ordinary session. It is non-nil
	// only between a successful POST /admin/session and that grant's expiry.
	AdminGrantExpiresAt *time.Time
	// LastSeenAt is nil until the first Touch after migration 00013.
	// Readers treat nil as "use CreatedAt" -- see the migration's comment.
	LastSeenAt *time.Time
}
```

```go
	// Touch records that the session was used at `at`. It writes one
	// column, last_seen_at, and must not be folded into Extend or
	// GrantAdmin: each of the three owns its column so none can overwrite
	// another's. Callers throttle it (middleware_session.go); the
	// repository does not.
	Touch(ctx context.Context, tokenHash []byte, at time.Time) error
```

- [ ] **Step 6: The repository**

In `api/internal/adapter/postgres/session_repo.go`, map the field in
`ByTokenHash` and add `Touch`:

```go
	return usecase.SessionRecord{
		UserID:              uuidToString(row.UserID),
		HouseholdID:         uuidToString(row.HouseholdID),
		ExpiresAt:           timeOf(row.ExpiresAt),
		AdminGrantExpiresAt: timePtrOf(row.AdminGrantExpiresAt),
		LastSeenAt:          timePtrOf(row.LastSeenAt),
	}, nil
```

```go
// Touch writes only last_seen_at -- see GrantAdmin's comment above for why
// each of the session's three timestamps has its own one-column statement.
func (r *SessionRepo) Touch(ctx context.Context, tokenHash []byte, at time.Time) error {
	return translate(r.q.TouchSession(ctx, sqlcgen.TouchSessionParams{
		TokenHash:  tokenHash,
		LastSeenAt: timestamptz(at),
	}), "touch session")
}
```

- [ ] **Step 7: The in-memory double**

In `api/internal/usecase/testdouble_test.go`, add the field to `sessionRow`,
return it from `ByTokenHash`, and add `Touch`:

```go
type sessionRow struct {
	UserID              string
	HouseholdID         string
	ExpiresAt           time.Time
	Revoked             bool
	AdminGrantExpiresAt *time.Time
	LastSeenAt          *time.Time
}
```

In `ByTokenHash`'s returned `SessionRecord`, add `LastSeenAt: row.LastSeenAt,`.

```go
// Touch mirrors the real repository's one-column write, the same
// separation Extend and GrantAdmin keep here.
func (d *sessionDouble) Touch(_ context.Context, tokenHash []byte, at time.Time) error {
	if row, ok := d.rows[string(tokenHash)]; ok {
		seen := at
		row.LastSeenAt = &seen
	}
	return nil // TouchSession is :exec -- an unknown token is a silent no-op.
}
```

- [ ] **Step 8: Run the test and the two packages that compile against the port**

```bash
cd api && go build ./... && go test ./internal/adapter/postgres/ -run 'TestTouchWritesOnlyLastSeenAt|TestExtendingASessionKeepsItsAdminGrant' -count=1 && go vet ./...
```
Expected: PASS, and `go build` clean — the `Touch` method satisfies the
interface everywhere the port is implemented (the real repo, the usecase
double; the HTTP test env uses the real repo).

- [ ] **Step 9: Mutation check — make `Touch` write `expires_at` too**

Temporarily change `TouchSession` to
`UPDATE sessions SET last_seen_at = $2, expires_at = $2 WHERE token_hash = $1;`,
run `make sqlc`, run the test: it must fail on "Touch moved expires_at".
Restore the query, `make sqlc`, run again: PASS. Note the result for
Task 10's learning entry.

- [ ] **Step 10: Commit**

```bash
git add api/migrations/00013_session_last_seen.sql api/internal/adapter/postgres/queries/identity.sql api/internal/adapter/postgres/sqlcgen api/internal/adapter/postgres/session_repo.go api/internal/adapter/postgres/admin_repo_test.go api/internal/usecase/ports.go api/internal/usecase/testdouble_test.go
git commit -m "feat(sessions): last_seen_at column and a one-column Touch"
```

---

### Task 2: The session middleware touches at most once an hour

**Files:**
- Modify: `api/internal/adapter/http/middleware_session.go:57-110`
- Create: `api/internal/adapter/http/session_touch_api_test.go`

**Interfaces:**
- Consumes: `deps.Sessions.Touch` (Task 1), `record.LastSeenAt` (Task 1),
  `deps.Clock.Now()`.
- Produces: `sessionTouchInterval` constant; the behaviour Task 5's
  "active in the last 7 days" counter reads.

- [ ] **Step 1: Write the failing tests**

Create `api/internal/adapter/http/session_touch_api_test.go`:

```go
package httpadapter_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// lastSeen reads the session row behind a cookie, through the same
// repository the router uses -- not through any route, since the property
// under test is what a route does to the row.
func lastSeen(t *testing.T, env *testEnv, session *http.Cookie) *time.Time {
	t.Helper()
	record, err := env.deps.Sessions.ByTokenHash(context.Background(), env.deps.Tokens.HashToken(session.Value))
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	return record.LastSeenAt
}

// Anchored on real now, for the reason movableClock's own comment gives:
// GetLiveSession's WHERE clause uses Postgres's now(), so a clock pinned to
// a past date signs in against wall time and expires one SessionTTL later.
func touchClock() *movableClock {
	return &movableClock{now: time.Now().UTC().Truncate(time.Second)}
}

func TestAFreshSessionIsTouchedOnItsFirstRequest(t *testing.T) {
	clk := touchClock()
	env := newTestEnvWithClock(t, clk)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	if got := lastSeen(t, env, session); got != nil {
		t.Fatalf("sign-in alone set last_seen_at = %v; only an authenticated request should", got)
	}

	rec := env.authedGet(t, "/api/v1/me", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /me: %d %s", rec.Code, rec.Body.String())
	}
	got := lastSeen(t, env, session)
	if got == nil || !got.Equal(clk.now) {
		t.Fatalf("last_seen_at after first request = %v, want %v", got, clk.now)
	}
}

func TestASessionTouchedTenMinutesAgoIsNotTouchedAgain(t *testing.T) {
	clk := touchClock()
	env := newTestEnvWithClock(t, clk)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authedGet(t, "/api/v1/me", session)
	first := lastSeen(t, env, session)

	clk.Advance(10 * time.Minute)
	env.authedGet(t, "/api/v1/me", session)

	second := lastSeen(t, env, session)
	if second == nil || !second.Equal(*first) {
		t.Fatalf("a request ten minutes after a touch moved last_seen_at from %v to %v; the throttle is an hour", first, second)
	}
}

func TestASessionTouchedOverAnHourAgoIsTouchedAgain(t *testing.T) {
	clk := touchClock()
	env := newTestEnvWithClock(t, clk)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authedGet(t, "/api/v1/me", session)

	clk.Advance(61 * time.Minute)
	env.authedGet(t, "/api/v1/me", session)

	got := lastSeen(t, env, session)
	if got == nil || !got.Equal(clk.now) {
		t.Fatalf("last_seen_at after an hour = %v, want %v", got, clk.now)
	}
}

// touchFailingSessions is the real repository with Touch broken, the same
// swap-one-port seam routerWithMemberships uses.
type touchFailingSessions struct{ usecase.SessionRepository }

var errTouchFailed = errors.New("touch failed")

func (touchFailingSessions) Touch(context.Context, []byte, time.Time) error { return errTouchFailed }

func TestATouchFailureDoesNotFailTheRequest(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	d := env.deps
	d.Sessions = touchFailingSessions{env.deps.Sessions}
	router := httpadapter.NewRouter(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("a failed touch answered %d; a usage timestamp must never fail a request", rec.Code)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

```bash
cd api && go test ./internal/adapter/http/ -run 'Touched|TouchFailure' -count=1
```
Expected: the first and third fail with `last_seen_at after ... = <nil>`;
the second passes vacuously (nothing touches yet); the fourth passes. That
second one is why Step 4's mutation check exists.

- [ ] **Step 3: The middleware**

In `api/internal/adapter/http/middleware_session.go`, add the constant after
`sessionExtendThreshold`:

```go
// sessionTouchInterval is how stale sessions.last_seen_at may be before a
// request refreshes it. One write per session-hour rather than one per
// request: the column answers "was this household active this week", which
// an hour's resolution serves completely, and this middleware runs on every
// authenticated request. Compare sessionExtendThreshold above, which bounds
// a different write for the same reason.
const sessionTouchInterval = time.Hour
```

Then, in `requireSession`, after the whole `if record.ExpiresAt.Sub(now) < sessionExtendThreshold { ... }`
block and before the `flags, err := deps.Admin.FlagsFor(...)` line:

```go
			if record.LastSeenAt == nil || now.Sub(*record.LastSeenAt) >= sessionTouchInterval {
				if err := deps.Sessions.Touch(ctx, hash, now); err != nil {
					// Best-effort, exactly like Extend above: a usage
					// timestamp that could not be written must not turn an
					// authenticated request into a failure. The next request
					// tries again.
					slog.Warn("failed to touch session", "error", err)
				}
			}
```

- [ ] **Step 4: Run, then mutation-check the throttle**

```bash
cd api && go test ./internal/adapter/http/ -run 'Touched|TouchFailure' -count=1
```
Expected: all four PASS.

Now remove the throttle: change the condition to `if true {`. Run again:
`TestASessionTouchedTenMinutesAgoIsNotTouchedAgain` must fail with "moved
last_seen_at". Restore. This is mutation check 3 of the spec's three.

- [ ] **Step 5: Whole HTTP package, then commit**

```bash
cd api && go test ./internal/adapter/http/ -count=1 && go vet ./...
```
Expected: PASS.

```bash
git add api/internal/adapter/http/middleware_session.go api/internal/adapter/http/session_touch_api_test.go
git commit -m "feat(sessions): touch last_seen_at at most once an hour per session"
```

---

### Task 3: The audit row carries the query string

**Files:**
- Modify: `api/internal/adapter/http/middleware_admin.go:70-105`
- Modify: `api/internal/adapter/http/admin_api_test.go` (after `TestAdminAuditRowRecordsTheRealRequest`)

**Interfaces:**
- Produces: `admin_audit_log.detail = {"query": "<raw query string>"}` on
  any admin request whose URL has one. Task 6's search relies on it; the
  walk's criterion 8 reads it.

- [ ] **Step 1: Write the failing test**

Append to `api/internal/adapter/http/admin_api_test.go`:

```go
// TestAdminAuditRowRecordsTheQueryString: a search is a fact the log should
// hold ("the operator looked for christine@"), and it is the one part of a
// request that is available before chi has parsed route parameters -- see
// auditAdmin's own comment on why Detail is otherwise empty.
func TestAdminAuditRowRecordsTheQueryString(t *testing.T) {
	env := newTestEnv(t)
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)

	env.authedGet(t, "/api/v1/admin/flags?probe=1&limit=5", session)
	entries, err := env.adminAudit.Recent(context.Background(), 1)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if got := entries[0].Detail["query"]; got != "probe=1&limit=5" {
		t.Fatalf("Detail[query] = %v, want the raw query string", got)
	}

	env.authedGet(t, "/api/v1/admin/flags", session)
	entries, err = env.adminAudit.Recent(context.Background(), 1)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if _, present := entries[0].Detail["query"]; present {
		t.Fatalf("a request with no query string still wrote Detail[query] = %v", entries[0].Detail["query"])
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/adapter/http/ -run TestAdminAuditRowRecordsTheQueryString -count=1
```
Expected: FAIL, `Detail[query] = <nil>`.

- [ ] **Step 3: The middleware**

In `auditAdmin`, build the detail map before the `RecordAudit` call and use
it, replacing `Detail: map[string]any{},`:

```go
			// The query string is the one part of a request that is both
			// meaningful to record (a search term, a limit) and available
			// here, before chi has matched the route -- see the comment
			// above on why route parameters are not. Absent on a URL with
			// none, so the common row stays {}.
			detail := map[string]any{}
			if r.URL.RawQuery != "" {
				detail["query"] = r.URL.RawQuery
			}
```

and in the entry: `Detail: detail,`.

Also amend the doc comment's last sentence, which now lies. Replace

> `Detail is left an empty object rather than populated with data that isn't there yet.`

with

> `Detail therefore never carries route parameters; it carries the raw query string when the URL has one (a search term is worth recording, and it is parsed before routing), and is otherwise an empty object.`

- [ ] **Step 4: Run the admin tests, commit**

```bash
cd api && go test ./internal/adapter/http/ -run 'Admin' -count=1
```
Expected: PASS.

```bash
git add api/internal/adapter/http/middleware_admin.go api/internal/adapter/http/admin_api_test.go
git commit -m "feat(admin): audit rows record the request's query string"
```

---

### Task 4: The port and `AdminDirectoryService`

**Files:**
- Modify: `api/internal/usecase/ports.go` (append after `AdminReauthAttemptRepository`)
- Create: `api/internal/usecase/admin_directory.go`
- Create: `api/internal/usecase/admin_directory_test.go`

**Interfaces:**
- Consumes: `LoginAttemptRepository.FailuresSince`, `domain.LockoutPolicy`,
  `Clock`.
- Produces, for Task 5 to implement and Task 6 to call:

```go
type AdminDirectoryRepository interface {
	Metrics(ctx context.Context, activeSince, signupsSince, now time.Time) (DirectoryMetrics, error)
	SearchHouseholds(ctx context.Context, q string, limit int, now time.Time) ([]HouseholdListing, error)
	Household(ctx context.Context, householdID string, now time.Time) (HouseholdDetail, error)
}
func NewAdminDirectoryService(d AdminDirectoryDeps) *AdminDirectoryService
func (s *AdminDirectoryService) Overview(ctx, q string, limit int) (DirectoryOverview, error)
func (s *AdminDirectoryService) Household(ctx, householdID string) (HouseholdPage, error)
```

- [ ] **Step 1: The port and its structs**

Append to `api/internal/usecase/ports.go`:

```go
// AdminDirectoryRepository is the operator's read-only view across every
// household. It is the only port in the product that reads across
// household boundaries; every other repository answers for one household.
// Nothing on it writes. Its consumer is AdminDirectoryService and its
// callers are guarded in the HTTP layer alone.
type AdminDirectoryRepository interface {
	// Metrics answers the four counters on the households page. The
	// cutoffs are passed in rather than computed here so the service's
	// clock is the only clock (see AdminDirectoryService).
	Metrics(ctx context.Context, activeSince, signupsSince, now time.Time) (DirectoryMetrics, error)

	// SearchHouseholds returns up to limit households matching q, most
	// recently active first, never-active last. An empty q matches every
	// household. The predicate is the spec's §4: case-insensitive substring
	// over household name, family name, member display name and member
	// email. The caller passes limit+1 to learn whether more exist.
	SearchHouseholds(ctx context.Context, q string, limit int, now time.Time) ([]HouseholdListing, error)

	// Household returns one household with its members and the invites
	// still pending at now. A missing household is domain.ErrNotFound.
	Household(ctx context.Context, householdID string, now time.Time) (HouseholdDetail, error)
}

type DirectoryMetrics struct {
	Households       int
	ActiveHouseholds int
	SignupsRequested int
	SignupsCompleted int
	PendingInvites   int
}

type HouseholdListing struct {
	ID              string
	Name            string
	FamilyName      string
	MemberCount     int
	CreatedAt       time.Time
	LastActiveAt    *time.Time // nil when no member has ever had a session
	PrimaryCurrency string
	// Match names the member whose name or email matched the search when
	// the household's own name and family name did not. Nil otherwise, and
	// always nil for an empty search.
	Match *MemberMatch
}

type MemberMatch struct {
	Name  string
	Email *string // nil for a Telegram-only member
}

type HouseholdDetail struct {
	ID              string
	Name            string
	FamilyName      string
	CreatedAt       time.Time
	PrimaryCurrency string
	Members         []HouseholdMember
	PendingInvites  []PendingInvite
}

// MemberChannel is how a member signs in. The repository sets it from the
// telegram_accounts join, never by inferring from a NULL email: a user
// with neither is a bug the screen should surface, not a state it names.
type MemberChannel string

const (
	ChannelEmail    MemberChannel = "email"
	ChannelTelegram MemberChannel = "telegram"
)

type HouseholdMember struct {
	UserID       string
	Name         string
	Email        *string
	Channel      MemberChannel
	Role         domain.Role
	Capabilities domain.Capabilities
	LastActiveAt *time.Time
}

type PendingInvite struct {
	Name          string
	Email         string
	Role          domain.Role
	InvitedByName string
	ExpiresAt     time.Time
}
```

- [ ] **Step 2: Write the failing service tests**

Create `api/internal/usecase/admin_directory_test.go`:

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

var directoryNow = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

// fakeDirectoryRepo records every argument it is given and answers what the
// test configured, so a test can assert both what the service asked for and
// what it did with the answer.
type fakeDirectoryRepo struct {
	metrics      usecase.DirectoryMetrics
	rows         []usecase.HouseholdListing
	detail       usecase.HouseholdDetail
	detailErr    error
	gotQ         string
	gotLimit     int
	gotActive    time.Time
	gotSignups   time.Time
	householdCalls int
}

func (f *fakeDirectoryRepo) Metrics(_ context.Context, activeSince, signupsSince, _ time.Time) (usecase.DirectoryMetrics, error) {
	f.gotActive, f.gotSignups = activeSince, signupsSince
	return f.metrics, nil
}

func (f *fakeDirectoryRepo) SearchHouseholds(_ context.Context, q string, limit int, _ time.Time) ([]usecase.HouseholdListing, error) {
	f.gotQ, f.gotLimit = q, limit
	if len(f.rows) > limit {
		return f.rows[:limit], nil
	}
	return f.rows, nil
}

func (f *fakeDirectoryRepo) Household(_ context.Context, _ string, _ time.Time) (usecase.HouseholdDetail, error) {
	f.householdCalls++
	return f.detail, f.detailErr
}

// directoryAttempts is a LoginAttemptRepository that answers FailuresSince
// with a fixed list and counts how often it was asked. Every other method
// fails loudly so a test cannot lean on one by accident.
type directoryAttempts struct {
	failures []time.Time
	calls    int
}

var errAttemptsUnexpected = errors.New("directoryAttempts: unexpected call")

func (d *directoryAttempts) Record(context.Context, *string, *string, string, bool, time.Time) error {
	return errAttemptsUnexpected
}
func (d *directoryAttempts) FailuresSince(_ context.Context, _ string, _ time.Time) ([]time.Time, error) {
	d.calls++
	return d.failures, nil
}
func (d *directoryAttempts) FailuresSinceForEmail(context.Context, string, time.Time) ([]time.Time, error) {
	return nil, errAttemptsUnexpected
}
func (d *directoryAttempts) ClearFailures(context.Context, string) error { return errAttemptsUnexpected }
func (d *directoryAttempts) Prune(context.Context, time.Time) (int64, error) {
	return 0, errAttemptsUnexpected
}

func listings(n int) []usecase.HouseholdListing {
	out := make([]usecase.HouseholdListing, n)
	for i := range out {
		out[i] = usecase.HouseholdListing{ID: "h" + string(rune('a'+i)), Name: "House"}
	}
	return out
}

func newDirectoryService(repo *fakeDirectoryRepo, attempts *directoryAttempts) *usecase.AdminDirectoryService {
	return usecase.NewAdminDirectoryService(usecase.AdminDirectoryDeps{
		Directory:     repo,
		LoginAttempts: attempts,
		Clock:         &fixedClock{now: directoryNow},
	})
}

func TestOverviewDefaultsAndClampsTheLimit(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, usecase.DirectoryDefaultLimit},
		{-1, usecase.DirectoryDefaultLimit},
		{7, 7},
		{usecase.DirectoryMaxLimit, usecase.DirectoryMaxLimit},
		{500, usecase.DirectoryMaxLimit},
	}
	for _, tc := range cases {
		repo := &fakeDirectoryRepo{}
		svc := newDirectoryService(repo, &directoryAttempts{})
		if _, err := svc.Overview(context.Background(), "", tc.in); err != nil {
			t.Fatalf("Overview(limit=%d): %v", tc.in, err)
		}
		// limit+1: that extra row is how Truncated is known.
		if repo.gotLimit != tc.want+1 {
			t.Fatalf("limit %d reached the repository as %d, want %d", tc.in, repo.gotLimit, tc.want+1)
		}
	}
}

func TestOverviewTrimsTheQuery(t *testing.T) {
	repo := &fakeDirectoryRepo{}
	svc := newDirectoryService(repo, &directoryAttempts{})
	if _, err := svc.Overview(context.Background(), "  christine@ ", 10); err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if repo.gotQ != "christine@" {
		t.Fatalf("q reached the repository as %q", repo.gotQ)
	}
}

func TestOverviewIsTruncatedOnlyWhenAnExtraRowCameBack(t *testing.T) {
	repo := &fakeDirectoryRepo{rows: listings(4)}
	svc := newDirectoryService(repo, &directoryAttempts{})

	got, err := svc.Overview(context.Background(), "", 3)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if !got.Truncated || len(got.Households) != 3 {
		t.Fatalf("4 rows for limit 3: Truncated=%v len=%d, want true and 3", got.Truncated, len(got.Households))
	}

	got, err = svc.Overview(context.Background(), "", 4)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if got.Truncated || len(got.Households) != 4 {
		t.Fatalf("4 rows for limit 4: Truncated=%v len=%d, want false and 4", got.Truncated, len(got.Households))
	}
}

func TestOverviewAsksForTheSpecsWindows(t *testing.T) {
	repo := &fakeDirectoryRepo{}
	svc := newDirectoryService(repo, &directoryAttempts{})
	if _, err := svc.Overview(context.Background(), "", 0); err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if want := directoryNow.Add(-7 * 24 * time.Hour); !repo.gotActive.Equal(want) {
		t.Fatalf("active window cutoff = %v, want %v", repo.gotActive, want)
	}
	if want := directoryNow.Add(-30 * 24 * time.Hour); !repo.gotSignups.Equal(want) {
		t.Fatalf("sign-up window cutoff = %v, want %v", repo.gotSignups, want)
	}
}

func TestHouseholdReportsLockedUntilWhenThePolicySaysLocked(t *testing.T) {
	policy := domain.DefaultLockoutPolicy()
	latest := directoryNow.Add(-time.Minute)
	attempts := &directoryAttempts{failures: []time.Time{
		latest.Add(-2 * time.Minute), latest.Add(-time.Minute), latest,
	}}
	svc := newDirectoryService(&fakeDirectoryRepo{detail: usecase.HouseholdDetail{ID: "h1"}}, attempts)

	page, err := svc.Household(context.Background(), "h1")
	if err != nil {
		t.Fatalf("Household: %v", err)
	}
	if page.LockedUntil == nil {
		t.Fatal("three failures inside the window did not report a lock")
	}
	if want := latest.Add(policy.LockFor); !page.LockedUntil.Equal(want) {
		t.Fatalf("LockedUntil = %v, want %v (latest failure + LockFor)", page.LockedUntil, want)
	}
}

func TestHouseholdReportsNoLockWithTwoFailures(t *testing.T) {
	attempts := &directoryAttempts{failures: []time.Time{directoryNow.Add(-2 * time.Minute), directoryNow.Add(-time.Minute)}}
	svc := newDirectoryService(&fakeDirectoryRepo{detail: usecase.HouseholdDetail{ID: "h1"}}, attempts)

	page, err := svc.Household(context.Background(), "h1")
	if err != nil {
		t.Fatalf("Household: %v", err)
	}
	if page.LockedUntil != nil {
		t.Fatalf("two failures reported a lock until %v", page.LockedUntil)
	}
}

func TestHouseholdNotFoundNeverConsultsLoginAttempts(t *testing.T) {
	attempts := &directoryAttempts{}
	svc := newDirectoryService(&fakeDirectoryRepo{detailErr: domain.ErrNotFound}, attempts)

	_, err := svc.Household(context.Background(), "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound unchanged", err)
	}
	if attempts.calls != 0 {
		t.Fatalf("FailuresSince was called %d times for a household that does not exist", attempts.calls)
	}
}
```

- [ ] **Step 3: Run them and watch them fail to compile**

```bash
cd api && go test ./internal/usecase/ -run 'Overview|TestHousehold' -count=1
```
Expected: compile error — `usecase.AdminDirectoryService` undefined.

- [ ] **Step 4: The service**

Create `api/internal/usecase/admin_directory.go`:

```go
package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// AdminDirectoryService is the operator's read-only view of the install:
// how many households exist, which are active, who signed up, and -- for
// one household -- who its members are and whether its sign-in is locked.
//
// It is separate from AdminService on purpose: that service is "who is a
// platform admin, feature flags, the audit log", and this one reads across
// every household, a boundary nothing else in the product crosses. It takes
// no actor parameter; the HTTP layer's /admin guards are the only gate.
type AdminDirectoryService struct{ d AdminDirectoryDeps }

type AdminDirectoryDeps struct {
	Directory     AdminDirectoryRepository
	LoginAttempts LoginAttemptRepository
	Clock         Clock
	// Policy is the household sign-in lockout policy, the same one
	// AuthService.SignIn evaluates. Zero means domain.DefaultLockoutPolicy,
	// filled in by the constructor exactly as NewAuthService does, so the
	// two can never disagree by omission.
	Policy domain.LockoutPolicy
}

const (
	// DirectoryDefaultLimit is how many households Overview returns when
	// the caller names no limit or an unusable one.
	DirectoryDefaultLimit = 50
	// DirectoryMaxLimit is the most Overview will return; past it the
	// screen tells the operator to search instead.
	DirectoryMaxLimit = 200

	directoryActiveWindow = 7 * 24 * time.Hour
	directorySignupWindow = 30 * 24 * time.Hour
)

func NewAdminDirectoryService(d AdminDirectoryDeps) *AdminDirectoryService {
	if d.Policy.MaxAttempts == 0 {
		d.Policy = domain.DefaultLockoutPolicy()
	}
	return &AdminDirectoryService{d: d}
}

type DirectoryOverview struct {
	Metrics    DirectoryMetrics
	Households []HouseholdListing
	// Truncated is true when more households matched than were returned.
	Truncated bool
}

type HouseholdPage struct {
	HouseholdDetail
	// LockedUntil is non-nil while the household's password sign-in is
	// locked, computed by the same policy sign-in itself applies.
	LockedUntil *time.Time
}

// clampDirectoryLimit: anything unusable falls back to the default rather
// than failing -- the operator typed a URL, not a form.
func clampDirectoryLimit(limit int) int {
	switch {
	case limit <= 0:
		return DirectoryDefaultLimit
	case limit > DirectoryMaxLimit:
		return DirectoryMaxLimit
	default:
		return limit
	}
}

// Overview is the households page: the four counters and the matching
// households, in one call so one page view is one request.
func (s *AdminDirectoryService) Overview(ctx context.Context, q string, limit int) (DirectoryOverview, error) {
	now := s.d.Clock.Now()
	limit = clampDirectoryLimit(limit)
	q = strings.TrimSpace(q)

	metrics, err := s.d.Directory.Metrics(ctx, now.Add(-directoryActiveWindow), now.Add(-directorySignupWindow), now)
	if err != nil {
		return DirectoryOverview{}, err
	}
	// limit+1: one row past the limit is how "more exist" is learned
	// without a second COUNT.
	rows, err := s.d.Directory.SearchHouseholds(ctx, q, limit+1, now)
	if err != nil {
		return DirectoryOverview{}, err
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	if rows == nil {
		rows = []HouseholdListing{}
	}
	return DirectoryOverview{Metrics: metrics, Households: rows, Truncated: truncated}, nil
}

// Household is the drill-in page. The lockout is evaluated only after the
// household is known to exist, so an unknown id is ErrNotFound before any
// second query runs.
func (s *AdminDirectoryService) Household(ctx context.Context, householdID string) (HouseholdPage, error) {
	now := s.d.Clock.Now()
	detail, err := s.d.Directory.Household(ctx, householdID, now)
	if err != nil {
		return HouseholdPage{}, err
	}
	failures, err := s.d.LoginAttempts.FailuresSince(ctx, householdID, now.Add(-s.d.Policy.Window))
	if err != nil {
		return HouseholdPage{}, err
	}
	page := HouseholdPage{HouseholdDetail: detail}
	if state := s.d.Policy.Evaluate(failures, now); state.Locked {
		until := state.Until
		page.LockedUntil = &until
	}
	return page, nil
}
```

- [ ] **Step 5: Run, lint, commit**

```bash
cd api && go test ./internal/usecase/ -run 'Overview|TestHousehold' -count=1 && cd .. && make lint-arch
```
Expected: PASS; arch lint clean (`usecase` imports only `domain`).

```bash
git add api/internal/usecase/ports.go api/internal/usecase/admin_directory.go api/internal/usecase/admin_directory_test.go
git commit -m "feat(usecase): AdminDirectoryService over a read-only cross-household port"
```

---

### Task 5: The SQL and `AdminDirectoryRepo`

**Files:**
- Create: `api/internal/adapter/postgres/queries/admin_directory.sql`
- Create: `api/internal/adapter/postgres/admin_directory_repo.go`
- Create: `api/internal/adapter/postgres/admin_directory_repo_test.go`

**Interfaces:**
- Consumes: the port and structs from Task 4; `translate`, `uuid`,
  `uuidToString`, `timeOf`, `timePtrOf`, `timestamptz` from `convert.go`.
- Produces: `postgres.NewAdminDirectoryRepo(db *DB) *AdminDirectoryRepo`
  for Task 6's wiring.

- [ ] **Step 1: The queries**

Create `api/internal/adapter/postgres/queries/admin_directory.sql`. Every
`COALESCE(s.last_seen_at, s.created_at)` in the codebase is in this file.

```sql
-- The operator's read-only view across every household. Nothing here
-- writes. "When was this session last used" is COALESCE(last_seen_at,
-- created_at) -- a session from before migration 00013, or one never
-- touched, counts from its creation -- and that expression appears only in
-- this file so it cannot drift.

-- name: CountHouseholds :one
SELECT COUNT(*) FROM households;

-- name: CountActiveHouseholdsSince :one
SELECT COUNT(*) FROM households h
WHERE EXISTS (
    SELECT 1 FROM sessions s
    WHERE s.household_id = h.id
      AND COALESCE(s.last_seen_at, s.created_at) >= $1
);

-- name: CountSignupsSince :one
-- Both channels: the table's own CHECK guarantees exactly one of email or
-- telegram_chat_id is set, so a plain count is a count of sign-ups.
SELECT
    COUNT(*)::bigint AS requested,
    COUNT(*) FILTER (WHERE consumed_at IS NOT NULL)::bigint AS completed
FROM signups
WHERE created_at >= $1;

-- name: CountPendingInvites :one
SELECT COUNT(*) FROM invites WHERE accepted_at IS NULL AND expires_at > $1;

-- name: SearchHouseholds :many
-- pattern is the caller-escaped '%q%' (see likePattern in the repo);
-- has_query is false for an empty search, which must return every
-- household and name no matched member. The lateral join finds the first
-- member (by joined_at) whose name or email matched, so a row can say why
-- it appeared when the household itself did not match.
SELECT
    h.id,
    h.name,
    h.family_name,
    h.primary_currency,
    h.created_at,
    (SELECT COUNT(*) FROM memberships m WHERE m.household_id = h.id)::bigint AS member_count,
    (SELECT MAX(COALESCE(s.last_seen_at, s.created_at)) FROM sessions s
       WHERE s.household_id = h.id)::timestamptz AS last_active_at,
    (sqlc.arg(has_query)::boolean
       AND (h.name ILIKE sqlc.arg(pattern) OR h.family_name ILIKE sqlc.arg(pattern)))::boolean AS household_matched,
    mm.display_name AS match_name,
    mm.email AS match_email
FROM households h
LEFT JOIN LATERAL (
    SELECT u.display_name, u.email
    FROM memberships m
    JOIN users u ON u.id = m.user_id
    WHERE sqlc.arg(has_query)::boolean
      AND m.household_id = h.id
      AND (u.display_name ILIKE sqlc.arg(pattern) OR u.email ILIKE sqlc.arg(pattern))
    ORDER BY m.joined_at
    LIMIT 1
) mm ON true
WHERE NOT sqlc.arg(has_query)::boolean
   OR h.name ILIKE sqlc.arg(pattern)
   OR h.family_name ILIKE sqlc.arg(pattern)
   OR mm.display_name IS NOT NULL
ORDER BY last_active_at DESC NULLS LAST, h.created_at DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: GetHouseholdForAdmin :one
SELECT id, name, family_name, primary_currency, created_at
FROM households WHERE id = $1;

-- name: ListMembersForAdmin :many
-- has_telegram comes from the join, never from email IS NULL: a user with
-- neither is a defect the screen should surface, not a state it names.
SELECT
    u.id AS user_id,
    u.display_name,
    u.email,
    m.role,
    m.capabilities,
    (ta.user_id IS NOT NULL)::boolean AS has_telegram,
    (SELECT MAX(COALESCE(s.last_seen_at, s.created_at)) FROM sessions s
       WHERE s.user_id = u.id AND s.household_id = m.household_id)::timestamptz AS last_active_at
FROM memberships m
JOIN users u ON u.id = m.user_id
LEFT JOIN telegram_accounts ta ON ta.user_id = u.id
WHERE m.household_id = $1
ORDER BY m.joined_at;

-- name: ListPendingInvitesForAdmin :many
SELECT i.name, i.email, i.role, i.expires_at, inviter.display_name AS invited_by_name
FROM invites i
JOIN users inviter ON inviter.id = i.invited_by
WHERE i.household_id = $1 AND i.accepted_at IS NULL AND i.expires_at > $2
ORDER BY i.created_at;
```

```bash
make sqlc
```
Expected: `sqlcgen/admin_directory.sql.go` exists. **Open it and check the
generated names before Step 3** — the repo below assumes:
`SearchHouseholdsParams{HasQuery bool, Pattern string, RowLimit int32}`,
`SearchHouseholdsRow{ID pgtype.UUID, Name, FamilyName, PrimaryCurrency string, CreatedAt pgtype.Timestamptz, MemberCount int64, LastActiveAt pgtype.Timestamptz, HouseholdMatched bool, MatchName *string, MatchEmail *string}`,
`CountSignupsSinceRow{Requested, Completed int64}`,
`ListMembersForAdminRow{UserID pgtype.UUID, DisplayName string, Email *string, Role string, Capabilities []string, HasTelegram bool, LastActiveAt pgtype.Timestamptz}`,
`ListPendingInvitesForAdminParams{HouseholdID pgtype.UUID, ExpiresAt pgtype.Timestamptz}`.
If sqlc chose a different name (an `interface{}` for a computed column, say),
add the explicit `::type` cast the column is missing and regenerate rather
than adapting the Go to a loose type.

- [ ] **Step 2: Write the failing repository tests**

Create `api/internal/adapter/postgres/admin_directory_repo_test.go`:

```go
package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// directoryFixture is one household with an owner, built the way every
// other repository test here builds one. Extra members, sessions, invites
// and sign-ups are added per test, because each test's point is which of
// them the query sees.
type directoryFixture struct {
	db         *postgres.DB
	households *postgres.HouseholdRepo
	users      *postgres.UserRepo
	members    *postgres.MembershipRepo
	sessions   *postgres.SessionRepo
	invites    *postgres.InviteRepo
	signups    *postgres.SignupRepo
	dir        *postgres.AdminDirectoryRepo
}

func newDirectoryFixture(t *testing.T) *directoryFixture {
	t.Helper()
	db := openTestDB(t)
	return &directoryFixture{
		db:         db,
		households: postgres.NewHouseholdRepo(db),
		users:      postgres.NewUserRepo(db),
		members:    postgres.NewMembershipRepo(db),
		sessions:   postgres.NewSessionRepo(db),
		invites:    postgres.NewInviteRepo(db),
		signups:    postgres.NewSignupRepo(db),
		dir:        postgres.NewAdminDirectoryRepo(db),
	}
}

func (f *directoryFixture) household(t *testing.T, name, family string) domain.Household {
	t.Helper()
	h, err := f.households.Create(context.Background(), domain.Household{
		Name: name, FamilyName: family, PrimaryCurrency: "SGD", SecondaryCurrency: "IDR",
	})
	if err != nil {
		t.Fatalf("create household %q: %v", name, err)
	}
	return h
}

// member creates a user (email "" means a Telegram-only account: users.email
// is NULL) and their membership.
func (f *directoryFixture) member(t *testing.T, householdID, email, name string, role domain.Role, caps domain.Capabilities) domain.User {
	t.Helper()
	u, err := f.users.Create(context.Background(), email, "", name)
	if err != nil {
		t.Fatalf("create user %q: %v", name, err)
	}
	if _, err := f.members.Create(context.Background(), domain.Membership{
		HouseholdID: householdID, UserID: u.ID, Role: role, Capabilities: caps,
	}); err != nil {
		t.Fatalf("create membership for %q: %v", name, err)
	}
	return u
}

func (f *directoryFixture) linkTelegram(t *testing.T, userID string, chatID int64) {
	t.Helper()
	// There is deliberately no repository method for this outside the
	// sign-up transaction (SignupRepo.Provision); a test fixture writes the
	// row directly.
	if _, err := f.db.Pool().Exec(context.Background(),
		"INSERT INTO telegram_accounts (user_id, chat_id) VALUES ($1, $2)", userID, chatID); err != nil {
		t.Fatalf("link telegram: %v", err)
	}
}

// session creates a session and, when seen is non-zero, touches it. The
// token hash only has to be unique.
func (f *directoryFixture) session(t *testing.T, userID, householdID, token string, seen time.Time) []byte {
	t.Helper()
	hash := []byte(token + "-------------------------------")[:32]
	if err := f.sessions.Create(context.Background(), hash, userID, householdID, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if !seen.IsZero() {
		if err := f.sessions.Touch(context.Background(), hash, seen); err != nil {
			t.Fatalf("touch session: %v", err)
		}
	}
	return hash
}

// backdateSession moves a session's created_at, which the repository never
// lets a caller set -- the "old session, recently touched" case needs it.
func (f *directoryFixture) backdateSession(t *testing.T, hash []byte, createdAt time.Time) {
	t.Helper()
	if _, err := f.db.Pool().Exec(context.Background(),
		"UPDATE sessions SET created_at = $1 WHERE token_hash = $2", createdAt, hash); err != nil {
		t.Fatalf("backdate session: %v", err)
	}
}

func TestSearchHouseholdsMatchesEveryFieldCaseInsensitively(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()
	now := time.Now()
	h := f.household(t, "Andreas & Christine", "Oentoro")
	f.member(t, h.ID, "christine@hearth.family", "Christine", domain.RoleOwner, domain.AllCapabilities())
	other := f.household(t, "Tan", "Tan")
	f.member(t, other.ID, "wei@example.test", "Wei", domain.RoleOwner, domain.AllCapabilities())

	cases := []struct {
		q         string
		wantID    string
		wantMatch string // "" when the household itself matched
	}{
		{"andreas", h.ID, ""},
		{"OENTORO", h.ID, ""},
		{"CHRISTINE@", h.ID, "Christine"},
		{"christ", h.ID, "Christine"},
		{"wei@example", other.ID, "Wei"},
	}
	for _, tc := range cases {
		rows, err := f.dir.SearchHouseholds(ctx, tc.q, 10, now)
		if err != nil {
			t.Fatalf("SearchHouseholds(%q): %v", tc.q, err)
		}
		if len(rows) != 1 || rows[0].ID != tc.wantID {
			t.Fatalf("SearchHouseholds(%q) = %+v, want exactly household %s", tc.q, rows, tc.wantID)
		}
		switch {
		case tc.wantMatch == "" && rows[0].Match != nil:
			t.Fatalf("SearchHouseholds(%q): household matched by name but Match = %+v", tc.q, rows[0].Match)
		case tc.wantMatch != "" && (rows[0].Match == nil || rows[0].Match.Name != tc.wantMatch):
			t.Fatalf("SearchHouseholds(%q): Match = %+v, want member %q", tc.q, rows[0].Match, tc.wantMatch)
		}
	}
}

func TestSearchHouseholdsEmptyQueryReturnsEveryoneWithNoMatch(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()
	h := f.household(t, "A", "A")
	f.member(t, h.ID, "a@example.test", "Alpha", domain.RoleOwner, domain.AllCapabilities())
	f.household(t, "B", "B")

	rows, err := f.dir.SearchHouseholds(ctx, "", 10, time.Now())
	if err != nil {
		t.Fatalf("SearchHouseholds: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("empty query returned %d rows, want 2", len(rows))
	}
	for _, r := range rows {
		if r.Match != nil {
			t.Fatalf("empty query named a matched member on %q: %+v", r.Name, r.Match)
		}
	}
}

// An underscore is a LIKE wildcard; unescaped it matches every household.
func TestSearchHouseholdsEscapesLikeWildcards(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()
	f.household(t, "Plain", "Plain")
	f.household(t, "Under_score", "X")

	rows, err := f.dir.SearchHouseholds(ctx, "_", 10, time.Now())
	if err != nil {
		t.Fatalf("SearchHouseholds: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "Under_score" {
		t.Fatalf("searching for a literal underscore returned %+v", rows)
	}
}

func TestSearchHouseholdsOrdersMostRecentlyActiveFirstNeverActiveLast(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	stale := f.household(t, "Stale", "S")
	staleOwner := f.member(t, stale.ID, "s@example.test", "S", domain.RoleOwner, domain.AllCapabilities())
	f.session(t, staleOwner.ID, stale.ID, "stale", now.Add(-72*time.Hour))

	fresh := f.household(t, "Fresh", "F")
	freshOwner := f.member(t, fresh.ID, "f@example.test", "F", domain.RoleOwner, domain.AllCapabilities())
	f.session(t, freshOwner.ID, fresh.ID, "fresh", now.Add(-time.Hour))

	f.household(t, "Never", "N")

	rows, err := f.dir.SearchHouseholds(ctx, "", 10, now)
	if err != nil {
		t.Fatalf("SearchHouseholds: %v", err)
	}
	if len(rows) != 3 || rows[0].Name != "Fresh" || rows[1].Name != "Stale" || rows[2].Name != "Never" {
		names := make([]string, len(rows))
		for i, r := range rows {
			names[i] = r.Name
		}
		t.Fatalf("order = %v, want [Fresh Stale Never]", names)
	}
	if rows[2].LastActiveAt != nil {
		t.Fatalf("a household with no sessions reported LastActiveAt = %v", rows[2].LastActiveAt)
	}
	if rows[0].MemberCount != 1 {
		t.Fatalf("MemberCount = %d, want 1", rows[0].MemberCount)
	}
}

func TestMetricsCountsActiveByLastSeenNotCreatedAt(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	// Signed in 40 days ago, used yesterday: active.
	old := f.household(t, "Old", "O")
	oldOwner := f.member(t, old.ID, "o@example.test", "O", domain.RoleOwner, domain.AllCapabilities())
	hash := f.session(t, oldOwner.ID, old.ID, "old", now.Add(-24*time.Hour))
	f.backdateSession(t, hash, now.Add(-40*24*time.Hour))

	// Signed in 10 days ago, never touched: not active.
	gone := f.household(t, "Gone", "G")
	goneOwner := f.member(t, gone.ID, "g@example.test", "G", domain.RoleOwner, domain.AllCapabilities())
	goneHash := f.session(t, goneOwner.ID, gone.ID, "gone", time.Time{})
	f.backdateSession(t, goneHash, now.Add(-10*24*time.Hour))

	m, err := f.dir.Metrics(ctx, now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour), now)
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	if m.Households != 2 {
		t.Fatalf("Households = %d, want 2", m.Households)
	}
	if m.ActiveHouseholds != 1 {
		t.Fatalf("ActiveHouseholds = %d, want 1 (touched yesterday counts; signed in 10 days ago does not)", m.ActiveHouseholds)
	}
}

func TestMetricsCountsSignupsAcrossBothChannelsAndInvitesStillPending(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	expires := now.Add(time.Hour)

	if err := f.signups.Create(ctx, "one@example.test", []byte("signup-hash-1-------------------"), expires); err != nil {
		t.Fatalf("signup 1: %v", err)
	}
	if err := f.signups.CreateConsumed(ctx, "two@example.test", []byte("signup-hash-2-------------------"), expires); err != nil {
		t.Fatalf("signup 2: %v", err)
	}
	if err := f.signups.CreateForTelegram(ctx, 4242, []byte("signup-hash-3-------------------"), expires); err != nil {
		t.Fatalf("signup 3: %v", err)
	}

	h := f.household(t, "H", "H")
	owner := f.member(t, h.ID, "owner@example.test", "Owner", domain.RoleOwner, domain.AllCapabilities())
	pending := func(email, hash string, expiresAt time.Time) string {
		id, err := f.invites.Create(ctx, h.ID, email, "Someone", domain.RoleLimited,
			domain.Capabilities{domain.CapCalendar}, []byte(hash), owner.ID, expiresAt)
		if err != nil {
			t.Fatalf("invite %s: %v", email, err)
		}
		return id
	}
	pending("p1@example.test", "invite-hash-1-------------------", expires)
	pending("p2@example.test", "invite-hash-2-------------------", now.Add(-time.Hour)) // expired
	accepted := pending("p3@example.test", "invite-hash-3-------------------", expires)
	if _, err := f.db.Pool().Exec(ctx, "UPDATE invites SET accepted_at = now() WHERE id = $1", accepted); err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	m, err := f.dir.Metrics(ctx, now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour), now)
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	if m.SignupsRequested != 3 || m.SignupsCompleted != 1 {
		t.Fatalf("sign-ups = %d requested / %d completed, want 3 / 1", m.SignupsRequested, m.SignupsCompleted)
	}
	if m.PendingInvites != 1 {
		t.Fatalf("PendingInvites = %d, want 1 (expired and accepted excluded)", m.PendingInvites)
	}
}

func TestHouseholdDetailNamesTheChannelFromTheTelegramJoin(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	h := f.household(t, "H", "H")
	owner := f.member(t, h.ID, "owner@example.test", "Owner", domain.RoleOwner, domain.AllCapabilities())
	kid := f.member(t, h.ID, "", "Kid", domain.RoleLimited, domain.Capabilities{domain.CapCalendar})
	f.linkTelegram(t, kid.ID, 777)
	if _, err := f.invites.Create(ctx, h.ID, "c@example.test", "Christine", domain.RoleOwner,
		domain.AllCapabilities(), []byte("invite-hash-c-------------------"), owner.ID, now.Add(time.Hour)); err != nil {
		t.Fatalf("invite: %v", err)
	}

	detail, err := f.dir.Household(ctx, h.ID, now)
	if err != nil {
		t.Fatalf("Household: %v", err)
	}
	if detail.Name != "H" || len(detail.Members) != 2 {
		t.Fatalf("detail = %+v", detail)
	}
	byName := map[string]usecase.HouseholdMember{}
	for _, m := range detail.Members {
		byName[m.Name] = m
	}
	if o := byName["Owner"]; o.Channel != usecase.ChannelEmail || o.Email == nil || *o.Email != "owner@example.test" || o.Role != domain.RoleOwner {
		t.Fatalf("owner = %+v", o)
	}
	if k := byName["Kid"]; k.Channel != usecase.ChannelTelegram || k.Email != nil || k.Role != domain.RoleLimited || !k.Capabilities.Has(domain.CapCalendar) {
		t.Fatalf("kid = %+v", k)
	}
	if byName["Kid"].LastActiveAt != nil {
		t.Fatalf("a member with no session reported LastActiveAt = %v", byName["Kid"].LastActiveAt)
	}
	if len(detail.PendingInvites) != 1 || detail.PendingInvites[0].InvitedByName != "Owner" || detail.PendingInvites[0].Email != "c@example.test" {
		t.Fatalf("pending invites = %+v", detail.PendingInvites)
	}

	// A Telegram-only member is found by name, and the listing's match has
	// no email to show.
	rows, err := f.dir.SearchHouseholds(ctx, "kid", 10, now)
	if err != nil {
		t.Fatalf("SearchHouseholds: %v", err)
	}
	if len(rows) != 1 || rows[0].Match == nil || rows[0].Match.Name != "Kid" || rows[0].Match.Email != nil {
		t.Fatalf("search by a Telegram-only member's name = %+v", rows)
	}
}

func TestHouseholdDetailUnknownIsNotFound(t *testing.T) {
	f := newDirectoryFixture(t)
	_, err := f.dir.Household(context.Background(), "00000000-0000-0000-0000-000000000000", time.Now())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 3: Run them and watch them fail to compile**

```bash
cd api && go test ./internal/adapter/postgres/ -run 'SearchHouseholds|Metrics|HouseholdDetail' -count=1
```
Expected: compile error — `postgres.NewAdminDirectoryRepo` undefined.

- [ ] **Step 4: The repository**

Create `api/internal/adapter/postgres/admin_directory_repo.go`:

```go
package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// AdminDirectoryRepo implements usecase.AdminDirectoryRepository over
// queries/admin_directory.sql. It holds the pool as well as the queries
// because Metrics runs its four counts inside one read-only transaction.
type AdminDirectoryRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewAdminDirectoryRepo(db *DB) *AdminDirectoryRepo {
	return &AdminDirectoryRepo{pool: db.Pool(), q: sqlcgen.New(db.Pool())}
}

// Metrics runs the four counts at REPEATABLE READ so the four tiles
// describe one instant. Invisible on this install's size, and free.
func (r *AdminDirectoryRepo) Metrics(ctx context.Context, activeSince, signupsSince, now time.Time) (usecase.DirectoryMetrics, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return usecase.DirectoryMetrics{}, fmt.Errorf("begin metrics transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit

	q := r.q.WithTx(tx)
	households, err := q.CountHouseholds(ctx)
	if err != nil {
		return usecase.DirectoryMetrics{}, translate(err, "count households")
	}
	active, err := q.CountActiveHouseholdsSince(ctx, timestamptz(activeSince))
	if err != nil {
		return usecase.DirectoryMetrics{}, translate(err, "count active households")
	}
	signups, err := q.CountSignupsSince(ctx, timestamptz(signupsSince))
	if err != nil {
		return usecase.DirectoryMetrics{}, translate(err, "count signups")
	}
	pending, err := q.CountPendingInvites(ctx, timestamptz(now))
	if err != nil {
		return usecase.DirectoryMetrics{}, translate(err, "count pending invites")
	}
	if err := tx.Commit(ctx); err != nil {
		return usecase.DirectoryMetrics{}, fmt.Errorf("commit metrics transaction: %w", err)
	}
	return usecase.DirectoryMetrics{
		Households:       int(households),
		ActiveHouseholds: int(active),
		SignupsRequested: int(signups.Requested),
		SignupsCompleted: int(signups.Completed),
		PendingInvites:   int(pending),
	}, nil
}

// likePattern wraps q for ILIKE and escapes its wildcards, so a search for
// "_" finds households with an underscore rather than every household.
// Postgres's default ESCAPE character is the backslash.
func likePattern(q string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
	return "%" + escaped + "%"
}

func (r *AdminDirectoryRepo) SearchHouseholds(ctx context.Context, q string, limit int, _ time.Time) ([]usecase.HouseholdListing, error) {
	rows, err := r.q.SearchHouseholds(ctx, sqlcgen.SearchHouseholdsParams{
		HasQuery: q != "",
		Pattern:  likePattern(q),
		RowLimit: int32(limit),
	})
	if err != nil {
		return nil, translate(err, "search households")
	}
	out := make([]usecase.HouseholdListing, 0, len(rows))
	for _, row := range rows {
		listing := usecase.HouseholdListing{
			ID:              uuidToString(row.ID),
			Name:            row.Name,
			FamilyName:      row.FamilyName,
			MemberCount:     int(row.MemberCount),
			CreatedAt:       timeOf(row.CreatedAt),
			LastActiveAt:    timePtrOf(row.LastActiveAt),
			PrimaryCurrency: row.PrimaryCurrency,
		}
		// The lateral join names a matching member for every row of a
		// non-empty search; it is only a *reason the row appeared* when the
		// household's own fields did not match.
		if !row.HouseholdMatched && row.MatchName != nil {
			listing.Match = &usecase.MemberMatch{Name: *row.MatchName, Email: row.MatchEmail}
		}
		out = append(out, listing)
	}
	return out, nil
}

func (r *AdminDirectoryRepo) Household(ctx context.Context, householdID string, now time.Time) (usecase.HouseholdDetail, error) {
	h, err := r.q.GetHouseholdForAdmin(ctx, uuid(householdID))
	if err != nil {
		return usecase.HouseholdDetail{}, translate(err, "get household for admin")
	}
	memberRows, err := r.q.ListMembersForAdmin(ctx, uuid(householdID))
	if err != nil {
		return usecase.HouseholdDetail{}, translate(err, "list members for admin")
	}
	inviteRows, err := r.q.ListPendingInvitesForAdmin(ctx, sqlcgen.ListPendingInvitesForAdminParams{
		HouseholdID: uuid(householdID),
		ExpiresAt:   timestamptz(now),
	})
	if err != nil {
		return usecase.HouseholdDetail{}, translate(err, "list pending invites for admin")
	}

	detail := usecase.HouseholdDetail{
		ID:              uuidToString(h.ID),
		Name:            h.Name,
		FamilyName:      h.FamilyName,
		CreatedAt:       timeOf(h.CreatedAt),
		PrimaryCurrency: h.PrimaryCurrency,
		Members:         make([]usecase.HouseholdMember, 0, len(memberRows)),
		PendingInvites:  make([]usecase.PendingInvite, 0, len(inviteRows)),
	}
	for _, row := range memberRows {
		// Role and capabilities arrive from columns; parse them rather than
		// cast, so a value nothing in the domain constructed is refused
		// here rather than rendered.
		role, err := domain.ParseRole(row.Role)
		if err != nil {
			return usecase.HouseholdDetail{}, fmt.Errorf("member %s: %w", uuidToString(row.UserID), err)
		}
		caps, err := domain.ParseCapabilities(row.Capabilities)
		if err != nil {
			return usecase.HouseholdDetail{}, fmt.Errorf("member %s: %w", uuidToString(row.UserID), err)
		}
		channel := usecase.ChannelEmail
		if row.HasTelegram {
			channel = usecase.ChannelTelegram
		}
		detail.Members = append(detail.Members, usecase.HouseholdMember{
			UserID:       uuidToString(row.UserID),
			Name:         row.DisplayName,
			Email:        row.Email,
			Channel:      channel,
			Role:         role,
			Capabilities: caps,
			LastActiveAt: timePtrOf(row.LastActiveAt),
		})
	}
	for _, row := range inviteRows {
		role, err := domain.ParseRole(row.Role)
		if err != nil {
			return usecase.HouseholdDetail{}, fmt.Errorf("invite for %s: %w", row.Email, err)
		}
		detail.PendingInvites = append(detail.PendingInvites, usecase.PendingInvite{
			Name:          row.Name,
			Email:         row.Email,
			Role:          role,
			InvitedByName: row.InvitedByName,
			ExpiresAt:     timeOf(row.ExpiresAt),
		})
	}
	return detail, nil
}
```

If `db.Pool()` is not the accessor's name, use whatever `NewUserRepo` uses
to reach the pool — `user_repo.go` holds one for `CreateWithMembership`.

- [ ] **Step 5: Run; then the two mutation checks**

```bash
cd api && go test ./internal/adapter/postgres/ -run 'SearchHouseholds|Metrics|HouseholdDetail' -count=1
```
Expected: PASS.

Mutation check 1 — in `CountActiveHouseholdsSince`, replace
`COALESCE(s.last_seen_at, s.created_at)` with `s.created_at`, `make sqlc`,
rerun: `TestMetricsCountsActiveByLastSeenNotCreatedAt` must fail with
"ActiveHouseholds = 0, want 1". Restore, regenerate.

Mutation check 2 — in `SearchHouseholds`' lateral join, delete
`OR u.email ILIKE sqlc.arg(pattern)`, `make sqlc`, rerun:
`TestSearchHouseholdsMatchesEveryFieldCaseInsensitively` must fail on the
`CHRISTINE@` case. Restore, regenerate. Both results go in Task 10's
learning entry.

- [ ] **Step 6: Whole package, lint, commit**

```bash
cd api && go test ./internal/adapter/postgres/ -count=1 && go vet ./... && cd .. && make lint-arch
```

```bash
git add api/internal/adapter/postgres/queries/admin_directory.sql api/internal/adapter/postgres/sqlcgen api/internal/adapter/postgres/admin_directory_repo.go api/internal/adapter/postgres/admin_directory_repo_test.go
git commit -m "feat(postgres): AdminDirectoryRepo -- the operator's cross-household reads"
```

---

### Task 6: The two routes

**Files:**
- Create: `api/internal/adapter/http/admin_directory_handlers.go`
- Create: `api/internal/adapter/http/admin_directory_api_test.go`
- Modify: `api/internal/adapter/http/router.go` (`Deps`, the granted group)
- Modify: `api/internal/adapter/http/admin_api_test.go` (the three gate tests)
- Modify: `api/internal/adapter/http/api_test.go` (wire the service)
- Modify: `api/cmd/api/main.go` (wire the service)

**Interfaces:**
- Consumes: `usecase.AdminDirectoryService` (Task 4),
  `postgres.NewAdminDirectoryRepo` (Task 5), `MapDomainError`, `WriteJSON`,
  `logAndWriteInternal`, `github.com/google/uuid` (already in `go.mod`).
- Produces: `GET /api/v1/admin/households` and
  `GET /api/v1/admin/households/{householdID}` with the spec §6 bodies;
  `Deps.AdminDirectory`.

- [ ] **Step 1: Wire the service into the test env and `main.go`**

In `api/internal/adapter/http/router.go`'s `Deps`, after `AdminReauth`:

```go
	AdminDirectory *usecase.AdminDirectoryService
```

In `api/internal/adapter/http/api_test.go`, after `adminReauthSvc` is built:

```go
	// Policy is left zero here too: the drill-in's lockout line must be
	// computed by the identical policy sign-in applies, and both
	// constructors fill in domain.DefaultLockoutPolicy() when handed none.
	adminDirectorySvc := usecase.NewAdminDirectoryService(usecase.AdminDirectoryDeps{
		Directory:     postgres.NewAdminDirectoryRepo(db),
		LoginAttempts: loginAttempts,
		Clock:         clk,
	})
```

and `AdminDirectory: adminDirectorySvc,` in the `Deps` literal after
`AdminReauth`. Same two additions in `api/cmd/api/main.go`, using its
`loginAttempts` and `sysClock`.

- [ ] **Step 2: Extend the three gate tests**

In `api/internal/adapter/http/admin_api_test.go`:

`TestAdminRoutesAre404ToANonAdmin` — after the existing flags assertion:

```go
	rec = env.authedGet(t, "/api/v1/admin/households", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
	rec = env.authedGet(t, "/api/v1/admin/households/"+env.householdID, session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
```

`TestAdminRoutesNeedAGrant` — after the existing assertion:

```go
	rec = env.authedGet(t, "/api/v1/admin/households", session)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "ADMIN_REAUTH_REQUIRED")
	rec = env.authedGet(t, "/api/v1/admin/households/"+env.householdID, session)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "ADMIN_REAUTH_REQUIRED")
```

`TestEveryAdminRequestIsAudited` — after the existing count check:

```go
	for _, path := range []string{"/api/v1/admin/households", "/api/v1/admin/households/" + env.householdID} {
		before = env.auditRowCount(t)
		env.authedGet(t, path, session)
		if after := env.auditRowCount(t); after != before+1 {
			t.Fatalf("%s: audit rows went %d -> %d; a read must write exactly one row", path, before, after)
		}
	}
```

- [ ] **Step 3: Write the failing route tests**

Create `api/internal/adapter/http/admin_directory_api_test.go`:

```go
package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"sort"
	"testing"
)

// grantedAdmin signs the owner in as a platform admin with a fresh grant,
// the state every test in this file starts from.
func grantedAdmin(t *testing.T, env *testEnv) *http.Cookie {
	t.Helper()
	env.makePlatformAdmin(t, env.ownerEmail)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	rec := env.authed(t, http.MethodPost, "/api/v1/admin/session",
		map[string]string{"password": env.ownerPassword}, session, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin session: %d %s", rec.Code, rec.Body.String())
	}
	return session
}

func sortedKeys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not an object: %s", raw)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func assertKeys(t *testing.T, what string, raw json.RawMessage, want ...string) {
	t.Helper()
	got := sortedKeys(t, raw)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("%s keys = %v, want exactly %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s keys = %v, want exactly %v", what, got, want)
		}
	}
}

type householdsBody struct {
	Metrics    json.RawMessage   `json:"metrics"`
	Households []json.RawMessage `json:"households"`
	Truncated  bool              `json:"truncated"`
}

type listingBody struct {
	Name        string          `json:"name"`
	MemberCount int             `json:"memberCount"`
	Match       json.RawMessage `json:"match"`
}

// The key sets are asserted exactly: the spec's "no money on either screen"
// is a property of the wire shape, and a field added to a DTO by accident
// must fail here rather than pass through.
func TestAdminHouseholdsListsTheSeededHouseholdWithExactlyTheSpecsKeys(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)

	rec := env.authedGet(t, "/api/v1/admin/households", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body householdsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertKeys(t, "top level", rec.Body.Bytes(), "metrics", "households", "truncated")
	assertKeys(t, "metrics", body.Metrics, "households", "activeHouseholds7d", "signups30d", "pendingInvites")
	if len(body.Households) != 1 {
		t.Fatalf("households = %d rows, want the one seeded household", len(body.Households))
	}
	assertKeys(t, "household row", body.Households[0],
		"id", "name", "familyName", "memberCount", "createdAt", "lastActiveAt", "primaryCurrency", "match")

	var row listingBody
	if err := json.Unmarshal(body.Households[0], &row); err != nil {
		t.Fatalf("decode row: %v", err)
	}
	if row.Name != "Andreas & Christine" || row.MemberCount != 3 {
		t.Fatalf("row = %+v, want the seeded household with its three members", row)
	}
	if string(row.Match) != "null" {
		t.Fatalf("an unsearched list named a match: %s", row.Match)
	}
	if body.Truncated {
		t.Fatal("one household reported truncated")
	}
}

func TestAdminHouseholdsSearchByMemberEmailNamesTheMatch(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)

	rec := env.authedGet(t, "/api/v1/admin/households?q=ethan%40", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body householdsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Households) != 1 {
		t.Fatalf("search by a member's email found %d households, want 1", len(body.Households))
	}
	var match struct {
		MemberName  string  `json:"memberName"`
		MemberEmail *string `json:"memberEmail"`
	}
	var row listingBody
	if err := json.Unmarshal(body.Households[0], &row); err != nil {
		t.Fatalf("decode row: %v", err)
	}
	if err := json.Unmarshal(row.Match, &match); err != nil || match.MemberName != "Ethan" {
		t.Fatalf("match = %s, want Ethan", row.Match)
	}

	rec = env.authedGet(t, "/api/v1/admin/households?q=nobody-here", session)
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Households) != 0 {
		t.Fatalf("a search that matches nothing returned %d rows", len(body.Households))
	}
	if string(body.Metrics) == "" {
		t.Fatal("a no-match search dropped the metrics")
	}
}

type householdPageBody struct {
	Household      json.RawMessage   `json:"household"`
	Members        []json.RawMessage `json:"members"`
	PendingInvites []json.RawMessage `json:"pendingInvites"`
	Lockout        json.RawMessage   `json:"lockout"`
}

func TestAdminHouseholdDrillInShowsMembersAndTheLockout(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)
	path := "/api/v1/admin/households/" + env.householdID

	rec := env.authedGet(t, path, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var page householdPageBody
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertKeys(t, "top level", rec.Body.Bytes(), "household", "members", "pendingInvites", "lockout")
	assertKeys(t, "household", page.Household, "id", "name", "familyName", "createdAt", "primaryCurrency")
	if len(page.Members) != 3 {
		t.Fatalf("members = %d, want 3", len(page.Members))
	}
	assertKeys(t, "member", page.Members[0],
		"userId", "name", "email", "channel", "role", "capabilities", "lastActiveAt")
	if string(page.Lockout) != "null" {
		t.Fatalf("an unlocked household reported lockout = %s", page.Lockout)
	}

	// Three wrong passwords lock the household's sign-in (the same policy
	// AuthService applies); the drill-in must now say so.
	for i := 0; i < 3; i++ {
		env.do(http.MethodPost, "/api/v1/auth/sign-in",
			map[string]string{"email": env.limitedEmail, "password": "wrong-password"})
	}
	rec = env.authedGet(t, path, session)
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(page.Lockout) == "null" {
		t.Fatal("three failed sign-ins did not surface as a lockout on the drill-in")
	}
	assertKeys(t, "lockout", page.Lockout, "lockedUntil")
}

func TestAdminHouseholdUnknownAndMalformedIDsAre404(t *testing.T) {
	env := newTestEnv(t)
	session := grantedAdmin(t, env)

	rec := env.authedGet(t, "/api/v1/admin/households/00000000-0000-0000-0000-000000000000", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")

	rec = env.authedGet(t, "/api/v1/admin/households/not-a-uuid", session)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}
```

- [ ] **Step 4: Run them and watch them fail**

```bash
cd api && go test ./internal/adapter/http/ -run 'AdminHousehold|TestAdminRoutes|TestEveryAdminRequestIsAudited' -count=1
```
Expected: the new tests fail with 404 (no route yet); the gate tests'
new assertions pass for the wrong reason (also 404). Step 6's run is what
proves them.

- [ ] **Step 5: The handlers**

Create `api/internal/adapter/http/admin_directory_handlers.go`:

```go
package httpadapter

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// The households page and its drill-in. Both are reads inside the /admin
// granted group, so requirePlatformAdmin, auditAdmin, requireCSRF and
// requireAdminGrant apply by construction -- nothing here checks who is
// asking. Every timestamp leaves as RFC 3339 in UTC.

type signupsDTO struct {
	Requested int `json:"requested"`
	Completed int `json:"completed"`
}

type directoryMetricsDTO struct {
	Households         int        `json:"households"`
	ActiveHouseholds7d int        `json:"activeHouseholds7d"`
	Signups30d         signupsDTO `json:"signups30d"`
	PendingInvites     int        `json:"pendingInvites"`
}

type memberMatchDTO struct {
	MemberName  string  `json:"memberName"`
	MemberEmail *string `json:"memberEmail"`
}

type householdListingDTO struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	FamilyName      string          `json:"familyName"`
	MemberCount     int             `json:"memberCount"`
	CreatedAt       time.Time       `json:"createdAt"`
	LastActiveAt    *time.Time      `json:"lastActiveAt"`
	PrimaryCurrency string          `json:"primaryCurrency"`
	Match           *memberMatchDTO `json:"match"`
}

type householdsResponse struct {
	Metrics    directoryMetricsDTO   `json:"metrics"`
	Households []householdListingDTO `json:"households"`
	Truncated  bool                  `json:"truncated"`
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// handleAdminHouseholds is the list page: metrics and rows in one answer, so
// one page view is one audit row. limit that fails to parse is 0, which the
// service turns into its default -- the operator typed a URL, not a form.
func handleAdminHouseholds(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		overview, err := deps.AdminDirectory.Overview(r.Context(), r.URL.Query().Get("q"), limit)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		body := householdsResponse{
			Metrics: directoryMetricsDTO{
				Households:         overview.Metrics.Households,
				ActiveHouseholds7d: overview.Metrics.ActiveHouseholds,
				Signups30d:         signupsDTO{Requested: overview.Metrics.SignupsRequested, Completed: overview.Metrics.SignupsCompleted},
				PendingInvites:     overview.Metrics.PendingInvites,
			},
			Households: make([]householdListingDTO, 0, len(overview.Households)),
			Truncated:  overview.Truncated,
		}
		for _, h := range overview.Households {
			dto := householdListingDTO{
				ID: h.ID, Name: h.Name, FamilyName: h.FamilyName, MemberCount: h.MemberCount,
				CreatedAt: h.CreatedAt.UTC(), LastActiveAt: utcPtr(h.LastActiveAt), PrimaryCurrency: h.PrimaryCurrency,
			}
			if h.Match != nil {
				dto.Match = &memberMatchDTO{MemberName: h.Match.Name, MemberEmail: h.Match.Email}
			}
			body.Households = append(body.Households, dto)
		}
		WriteJSON(w, http.StatusOK, body)
	}
}

type householdHeaderDTO struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	FamilyName      string    `json:"familyName"`
	CreatedAt       time.Time `json:"createdAt"`
	PrimaryCurrency string    `json:"primaryCurrency"`
}

type householdMemberDTO struct {
	UserID       string     `json:"userId"`
	Name         string     `json:"name"`
	Email        *string    `json:"email"`
	Channel      string     `json:"channel"`
	Role         string     `json:"role"`
	Capabilities []string   `json:"capabilities"`
	LastActiveAt *time.Time `json:"lastActiveAt"`
}

type pendingInviteDTO struct {
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	Role          string    `json:"role"`
	InvitedByName string    `json:"invitedByName"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

type lockoutDTO struct {
	LockedUntil time.Time `json:"lockedUntil"`
}

type householdPageResponse struct {
	Household      householdHeaderDTO   `json:"household"`
	Members        []householdMemberDTO `json:"members"`
	PendingInvites []pendingInviteDTO   `json:"pendingInvites"`
	Lockout        *lockoutDTO          `json:"lockout"`
}

// channelString fails closed: a MemberChannel nobody constructed is an
// error and a 500, never an empty string in the JSON.
func channelString(c usecase.MemberChannel) (string, error) {
	switch c {
	case usecase.ChannelEmail:
		return "email", nil
	case usecase.ChannelTelegram:
		return "telegram", nil
	default:
		return "", fmt.Errorf("unknown member channel %q", c)
	}
}

// handleAdminHousehold is the drill-in. The id is parsed here, before the
// service is called, so a malformed one answers the same 404 a missing
// household does rather than the zero-UUID 500 the flag override routes
// still carry (ADMIN_SURFACE_HANDOVER.md, "Known, deferred").
func handleAdminHousehold(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "householdID")
		if _, err := uuid.Parse(id); err != nil {
			MapDomainError(w, r, domain.ErrNotFound)
			return
		}
		page, err := deps.AdminDirectory.Household(r.Context(), id)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		body := householdPageResponse{
			Household: householdHeaderDTO{
				ID: page.ID, Name: page.Name, FamilyName: page.FamilyName,
				CreatedAt: page.CreatedAt.UTC(), PrimaryCurrency: page.PrimaryCurrency,
			},
			Members:        make([]householdMemberDTO, 0, len(page.Members)),
			PendingInvites: make([]pendingInviteDTO, 0, len(page.PendingInvites)),
		}
		for _, m := range page.Members {
			channel, err := channelString(m.Channel)
			if err != nil {
				logAndWriteInternal(w, r, err)
				return
			}
			body.Members = append(body.Members, householdMemberDTO{
				UserID: m.UserID, Name: m.Name, Email: m.Email, Channel: channel,
				Role: string(m.Role), Capabilities: m.Capabilities.Strings(),
				LastActiveAt: utcPtr(m.LastActiveAt),
			})
		}
		for _, i := range page.PendingInvites {
			body.PendingInvites = append(body.PendingInvites, pendingInviteDTO{
				Name: i.Name, Email: i.Email, Role: string(i.Role),
				InvitedByName: i.InvitedByName, ExpiresAt: i.ExpiresAt.UTC(),
			})
		}
		if page.LockedUntil != nil {
			body.Lockout = &lockoutDTO{LockedUntil: page.LockedUntil.UTC()}
		}
		WriteJSON(w, http.StatusOK, body)
	}
}
```

If `Capabilities.Strings()` returns nil for an empty set, wrap it:
`caps := m.Capabilities.Strings(); if caps == nil { caps = []string{} }` —
the frontend's zod schema wants an array, never null.

In `router.go`'s granted group, after the flag routes:

```go
					// The operator's directory: two reads. See
					// admin_directory_handlers.go.
					granted.Get("/households", handleAdminHouseholds(deps))
					granted.Get("/households/{householdID}", handleAdminHousehold(deps))
```

- [ ] **Step 6: Run everything in the HTTP package**

```bash
cd api && go test ./internal/adapter/http/ -count=1 && go vet ./... && go build ./... && cd .. && make lint-arch
```
Expected: PASS, including `TestEveryMutatingRouteRequiresCSRF` (unchanged:
both new routes are `GET`).

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/http/admin_directory_handlers.go api/internal/adapter/http/admin_directory_api_test.go api/internal/adapter/http/router.go api/internal/adapter/http/admin_api_test.go api/internal/adapter/http/api_test.go api/cmd/api/main.go
git commit -m "feat(admin): GET /admin/households and /admin/households/{id}"
```

---

### Task 7: Schemas, hooks and the time labels

**Files:**
- Create: `web/src/features/admin/adminDirectorySchemas.ts`
- Create: `web/src/features/admin/useAdminDirectory.ts`
- Create: `web/src/features/admin/directoryCopy.ts`
- Create: `web/src/features/admin/directoryCopy.test.ts`
- Create: `web/src/features/admin/adminDirectorySchemas.test.ts`

**Interfaces:**
- Consumes: `apiFetch`, `ApiError` from `web/src/api/client.ts`;
  `adminFlagsKey`, `toAdminGateError` from `useAdmin.ts`.
- Produces, for Tasks 8 and 9:

```ts
export type AdminHouseholdListing, AdminHouseholdsResponse, AdminHouseholdPage
export function adminHouseholdsPath(q: string, limit: number): string
export function useAdminHouseholds(q: string, limit: number)
export function useAdminHousehold(householdId: string)
export function useCloseSurfaceOnReauth(error: unknown): void
export function relativeTimeLabel(iso: string | null, now: Date): string
export function dateLabel(iso: string): string
export function exactTimeLabel(iso: string): string
```

- [ ] **Step 1: Write the failing tests**

Create `web/src/features/admin/directoryCopy.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { relativeTimeLabel } from "./directoryCopy";

const now = new Date("2026-09-02T12:00:00Z");
const ago = (ms: number) => new Date(now.getTime() - ms).toISOString();
const minute = 60_000;
const hour = 60 * minute;
const day = 24 * hour;

describe("relativeTimeLabel", () => {
  it("says never for null", () => {
    expect(relativeTimeLabel(null, now)).toBe("never");
  });
  it("walks the boundaries", () => {
    expect(relativeTimeLabel(ago(10_000), now)).toBe("just now");
    expect(relativeTimeLabel(ago(5 * minute), now)).toBe("5 min ago");
    expect(relativeTimeLabel(ago(3 * hour), now)).toBe("3 h ago");
    expect(relativeTimeLabel(ago(25 * hour), now)).toBe("yesterday");
    expect(relativeTimeLabel(ago(4 * day), now)).toBe("4 days ago");
    expect(relativeTimeLabel(ago(45 * day), now)).toBe("1 month ago");
    expect(relativeTimeLabel(ago(400 * day), now)).toBe("1 year ago");
  });
  it("never goes negative for a timestamp slightly in the future (clock skew)", () => {
    expect(relativeTimeLabel(new Date(now.getTime() + 5000).toISOString(), now)).toBe("just now");
  });
});
```

Create `web/src/features/admin/adminDirectorySchemas.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { adminHouseholdPageSchema, adminHouseholdsResponseSchema } from "./adminDirectorySchemas";

const listing = {
  id: "h1", name: "Oentoro", familyName: "Oentoro", memberCount: 4,
  createdAt: "2026-08-15T02:11:09Z", lastActiveAt: null, primaryCurrency: "SGD", match: null,
};
const response = {
  metrics: { households: 1, activeHouseholds7d: 0, signups30d: { requested: 0, completed: 0 }, pendingInvites: 0 },
  households: [listing],
  truncated: false,
};

describe("adminDirectorySchemas", () => {
  it("accepts the spec's list shape", () => {
    expect(adminHouseholdsResponseSchema.parse(response).households[0].name).toBe("Oentoro");
  });
  // .strict(): a money field added to a DTO by accident must fail here, not
  // pass through to a screen the spec says never shows money.
  it("rejects an unexpected key on a household row", () => {
    const withBalance = { ...response, households: [{ ...listing, balance: 100 }] };
    expect(() => adminHouseholdsResponseSchema.parse(withBalance)).toThrow();
  });
  it("rejects a missing nullable field rather than defaulting it", () => {
    const { lastActiveAt: _dropped, ...withoutLastActive } = listing;
    expect(() => adminHouseholdsResponseSchema.parse({ ...response, households: [withoutLastActive] })).toThrow();
  });
  it("accepts a Telegram-only member and a null lockout on the drill-in", () => {
    const page = {
      household: { id: "h1", name: "O", familyName: "O", createdAt: "2026-08-15T02:11:09Z", primaryCurrency: "SGD" },
      members: [{ userId: "u1", name: "Kid", email: null, channel: "telegram", role: "limited", capabilities: ["calendar"], lastActiveAt: null }],
      pendingInvites: [],
      lockout: null,
    };
    expect(adminHouseholdPageSchema.parse(page).members[0].channel).toBe("telegram");
  });
  it("rejects a channel the API never produces", () => {
    const page = {
      household: { id: "h1", name: "O", familyName: "O", createdAt: "2026-08-15T02:11:09Z", primaryCurrency: "SGD" },
      members: [{ userId: "u1", name: "Kid", email: null, channel: "carrier-pigeon", role: "limited", capabilities: [], lastActiveAt: null }],
      pendingInvites: [],
      lockout: null,
    };
    expect(() => adminHouseholdPageSchema.parse(page)).toThrow();
  });
});
```

- [ ] **Step 2: Run them and watch them fail**

```bash
cd web && npx vitest run src/features/admin/directoryCopy.test.ts src/features/admin/adminDirectorySchemas.test.ts
```
Expected: both fail to import.

- [ ] **Step 3: The schemas**

Create `web/src/features/admin/adminDirectorySchemas.ts`:

```ts
// Zod mirrors of the DTOs in api/internal/adapter/http/
// admin_directory_handlers.go -- the adminSchemas.ts convention: follow the
// backend's own structs, not a guess at the shape. Every object is .strict()
// so a key the backend did not promise (a money field, say) fails the parse
// rather than reaching a screen the spec says never shows money.
import { z } from "zod";

export const adminMemberMatchSchema = z
  .object({ memberName: z.string(), memberEmail: z.string().nullable() })
  .strict();

export const adminHouseholdListingSchema = z
  .object({
    id: z.string(),
    name: z.string(),
    familyName: z.string(),
    memberCount: z.number().int(),
    createdAt: z.string(),
    lastActiveAt: z.string().nullable(),
    primaryCurrency: z.string(),
    // Set only when a member, not the household, matched the search.
    match: adminMemberMatchSchema.nullable(),
  })
  .strict();
export type AdminHouseholdListing = z.infer<typeof adminHouseholdListingSchema>;

export const adminDirectoryMetricsSchema = z
  .object({
    households: z.number().int(),
    activeHouseholds7d: z.number().int(),
    signups30d: z.object({ requested: z.number().int(), completed: z.number().int() }).strict(),
    pendingInvites: z.number().int(),
  })
  .strict();

export const adminHouseholdsResponseSchema = z
  .object({
    metrics: adminDirectoryMetricsSchema,
    households: z.array(adminHouseholdListingSchema),
    truncated: z.boolean(),
  })
  .strict();
export type AdminHouseholdsResponse = z.infer<typeof adminHouseholdsResponseSchema>;

export const adminHouseholdMemberSchema = z
  .object({
    userId: z.string(),
    name: z.string(),
    email: z.string().nullable(),
    // The backend's channelString fails closed on anything else; so does
    // this enum, on the client's side of the same boundary.
    channel: z.enum(["email", "telegram"]),
    role: z.enum(["owner", "limited"]),
    capabilities: z.array(z.string()),
    lastActiveAt: z.string().nullable(),
  })
  .strict();
export type AdminHouseholdMember = z.infer<typeof adminHouseholdMemberSchema>;

export const adminPendingInviteSchema = z
  .object({
    name: z.string(),
    email: z.string(),
    role: z.enum(["owner", "limited"]),
    invitedByName: z.string(),
    expiresAt: z.string(),
  })
  .strict();

export const adminHouseholdPageSchema = z
  .object({
    household: z
      .object({
        id: z.string(),
        name: z.string(),
        familyName: z.string(),
        createdAt: z.string(),
        primaryCurrency: z.string(),
      })
      .strict(),
    members: z.array(adminHouseholdMemberSchema),
    pendingInvites: z.array(adminPendingInviteSchema),
    lockout: z.object({ lockedUntil: z.string() }).strict().nullable(),
  })
  .strict();
export type AdminHouseholdPage = z.infer<typeof adminHouseholdPageSchema>;
```

- [ ] **Step 4: The copy module**

Create `web/src/features/admin/directoryCopy.ts`:

```ts
// Time labels and copy for the households pages, in their own module so
// AdminHouseholdsPage.tsx and AdminHouseholdPage.tsx export only components
// (eslint-plugin-react-refresh's only-export-components rule) and so the
// boundaries below can be unit-tested without rendering anything.

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

// relativeTimeLabel is the coarse text shown in a table cell; the exact
// instant belongs in the element's title (exactTimeLabel). null is "never":
// the API sends null when no session has ever existed, and "never" is the
// honest word for it on a page whose job is "is anyone using this".
export function relativeTimeLabel(iso: string | null, now: Date): string {
  if (iso === null) return "never";
  const elapsed = Math.max(0, now.getTime() - new Date(iso).getTime());
  if (elapsed < MINUTE) return "just now";
  if (elapsed < HOUR) return `${Math.floor(elapsed / MINUTE)} min ago`;
  if (elapsed < DAY) return `${Math.floor(elapsed / HOUR)} h ago`;
  const days = Math.floor(elapsed / DAY);
  if (days === 1) return "yesterday";
  if (days < 30) return `${days} days ago`;
  if (days < 365) {
    const months = Math.floor(days / 30);
    return months === 1 ? "1 month ago" : `${months} months ago`;
  }
  const years = Math.floor(days / 365);
  return years === 1 ? "1 year ago" : `${years} years ago`;
}

// Same locale retroCopy.completedDateLabel uses, with the year: an
// operator's list spans months.
export function dateLabel(iso: string): string {
  return new Date(iso).toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" });
}

export function exactTimeLabel(iso: string): string {
  return new Date(iso).toLocaleString("en-US", {
    month: "short", day: "numeric", year: "numeric", hour: "numeric", minute: "2-digit",
  });
}

export function noMatchLabel(q: string): string {
  return `Nothing matches '${q}'.`;
}

export function showingLabel(shown: number, truncated: boolean, atCap: boolean): string {
  if (!truncated) return `Showing ${shown} of ${shown}`;
  if (atCap) return `Showing the first ${shown} — search to narrow`;
  return `Showing the first ${shown}`;
}

export function lockoutLabel(lockedUntilIso: string, now: Date): string {
  const minutes = Math.max(1, Math.ceil((new Date(lockedUntilIso).getTime() - now.getTime()) / MINUTE));
  const time = new Date(lockedUntilIso).toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit" });
  return `Sign-in is locked until ${time} (in ${minutes} min).`;
}
```

- [ ] **Step 5: The hooks**

Create `web/src/features/admin/useAdminDirectory.ts`:

```ts
// Query hooks over the directory routes (api/internal/adapter/http/
// admin_directory_handlers.go). Same shape as useAdmin.ts's useAdminFlags,
// same two rules: refetchOnWindowFocus is off because every request under
// /admin is an audit row, and a lapsed grant is not handled here -- it is
// routed to the one AdminGate AdminShell owns (useCloseSurfaceOnReauth).
import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, apiFetch } from "../../api/client";
import { adminFlagsKey } from "./useAdmin";
import {
  adminHouseholdPageSchema,
  adminHouseholdsResponseSchema,
  type AdminHouseholdPage,
  type AdminHouseholdsResponse,
} from "./adminDirectorySchemas";

export const DIRECTORY_DEFAULT_LIMIT = 50;
export const DIRECTORY_MAX_LIMIT = 200;

// adminHouseholdsPath builds the exact URL the page requests, exported so a
// test's fetch stub and the hook agree byte for byte. q is omitted when
// empty so the audit row for a plain page view stays {}.
export function adminHouseholdsPath(q: string, limit: number): string {
  const params = new URLSearchParams();
  if (q !== "") params.set("q", q);
  params.set("limit", String(limit));
  return `/api/v1/admin/households?${params.toString()}`;
}

export function adminHouseholdsKey(q: string, limit: number) {
  return ["admin", "households", { q, limit }] as const;
}

export function adminHouseholdKey(householdId: string) {
  return ["admin", "household", householdId] as const;
}

async function fetchAdminHouseholds(q: string, limit: number): Promise<AdminHouseholdsResponse> {
  const body = await apiFetch<unknown>(adminHouseholdsPath(q, limit));
  return adminHouseholdsResponseSchema.parse(body);
}

async function fetchAdminHousehold(householdId: string): Promise<AdminHouseholdPage> {
  const body = await apiFetch<unknown>(`/api/v1/admin/households/${encodeURIComponent(householdId)}`);
  return adminHouseholdPageSchema.parse(body);
}

export function useAdminHouseholds(q: string, limit: number) {
  return useQuery({
    queryKey: adminHouseholdsKey(q, limit),
    queryFn: () => fetchAdminHouseholds(q, limit),
    refetchOnWindowFocus: false,
  });
}

export function useAdminHousehold(householdId: string) {
  return useQuery({
    queryKey: adminHouseholdKey(householdId),
    queryFn: () => fetchAdminHousehold(householdId),
    refetchOnWindowFocus: false,
  });
}

// useCloseSurfaceOnReauth: a page-level query that meets a lapsed grant
// does not render its own prompt. Invalidating adminFlagsKey makes the
// query AdminShell's gate already watches refetch, hit the same 401, and
// close the whole surface -- the identical route useAdmin.ts's write
// mutations take. NOT_FOUND is deliberately not here: on the drill-in a
// 404 means "no such household" and is the page's own to render.
export function useCloseSurfaceOnReauth(error: unknown): void {
  const queryClient = useQueryClient();
  useEffect(() => {
    if (error instanceof ApiError && error.code === "ADMIN_REAUTH_REQUIRED") {
      queryClient.invalidateQueries({ queryKey: adminFlagsKey });
    }
  }, [error, queryClient]);
}

export function isNotFound(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
}
```

- [ ] **Step 6: Run, typecheck, commit**

```bash
cd web && npx vitest run src/features/admin/directoryCopy.test.ts src/features/admin/adminDirectorySchemas.test.ts && npx tsc --noEmit
```
Expected: PASS, no type errors.

```bash
git add web/src/features/admin/adminDirectorySchemas.ts web/src/features/admin/adminDirectorySchemas.test.ts web/src/features/admin/useAdminDirectory.ts web/src/features/admin/directoryCopy.ts web/src/features/admin/directoryCopy.test.ts
git commit -m "feat(web): schemas, hooks and time labels for the admin directory"
```

---

### Task 8: The households list page, its route and the nav

**Files:**
- Create: `web/src/features/admin/AdminHouseholdsPage.tsx`
- Create: `web/src/features/admin/AdminHouseholdsPage.test.tsx`
- Modify: `web/src/features/admin/AdminShell.tsx` (header nav)
- Modify: `web/src/routes/router.tsx` (lazy import, route, `addChildren`)
- Modify: `web/src/features/admin/adminBundleSplit.test.ts`

**Interfaces:**
- Consumes: Task 7's hooks, schemas and labels; `PageContainer`;
  `NotFoundScreen` is not needed here.
- Produces: `AdminHouseholdsPage({ q, limit, onSearch, onShowMore })` —
  the page takes its URL state as props and hands navigation back to the
  route, so the component is testable under `renderWithRouter`'s bare root
  route, the same seam the magic-link route uses for its `token`.

- [ ] **Step 1: Write the failing page tests**

Create `web/src/features/admin/AdminHouseholdsPage.test.tsx`:

```tsx
// Follows AdminFlagsPage.test.tsx: renderWithRouter plus stubFetchRoutes
// for every request, literal strings asserted.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AdminHouseholdsPage } from "./AdminHouseholdsPage";
import { adminHouseholdsPath } from "./useAdminDirectory";
import type { AdminHouseholdListing, AdminHouseholdsResponse } from "./adminDirectorySchemas";

function listing(overrides: Partial<AdminHouseholdListing> = {}): AdminHouseholdListing {
  return {
    id: "h1", name: "Oentoro", familyName: "Oentoro", memberCount: 4,
    createdAt: "2026-08-15T02:11:09Z", lastActiveAt: "2026-09-02T07:40:12Z",
    primaryCurrency: "SGD", match: null, ...overrides,
  };
}

function response(households: AdminHouseholdListing[], truncated = false): AdminHouseholdsResponse {
  return {
    metrics: { households: households.length, activeHouseholds7d: 1, signups30d: { requested: 9, completed: 4 }, pendingInvites: 2 },
    households,
    truncated,
  };
}

function renderPage(q = "", limit = 50, handlers: Partial<{ onSearch: (q: string) => void; onShowMore: () => void }> = {}) {
  return renderWithRouter(
    <AdminHouseholdsPage q={q} limit={limit} onSearch={handlers.onSearch ?? vi.fn()} onShowMore={handlers.onShowMore ?? vi.fn()} />,
  );
}

describe("AdminHouseholdsPage", () => {
  it("renders the four tiles and a household row with a link to its drill-in", async () => {
    stubFetchRoutes({ [`GET ${adminHouseholdsPath("", 50)}`]: { status: 200, body: response([listing()]) } });
    renderPage();

    expect(await screen.findByRole("heading", { name: "Households" })).toBeInTheDocument();
    const tiles = screen.getByRole("list", { name: "Install metrics" });
    expect(within(tiles).getByText("9 requested")).toBeInTheDocument();
    expect(within(tiles).getByText("4 completed")).toBeInTheDocument();
    expect(within(tiles).getByText("Invites pending")).toBeInTheDocument();

    const link = screen.getByRole("link", { name: "Oentoro" });
    expect(link.getAttribute("href")).toMatch(/\/admin\/households\/h1$/);
    expect(screen.getByText("Showing 1 of 1")).toBeInTheDocument();
  });

  it("names the matched member under a row that matched through a member", async () => {
    stubFetchRoutes({
      [`GET ${adminHouseholdsPath("christine@", 50)}`]: {
        status: 200,
        body: response([listing({ match: { memberName: "Christine", memberEmail: "c@example.org" } })]),
      },
    });
    renderPage("christine@");

    expect(await screen.findByText("matched Christine · c@example.org")).toBeInTheDocument();
  });

  // Submitting is the only thing that searches: typing must not fetch.
  it("calls onSearch with the typed value on submit, and not before", async () => {
    stubFetchRoutes({ [`GET ${adminHouseholdsPath("", 50)}`]: { status: 200, body: response([listing()]) } });
    const onSearch = vi.fn();
    renderPage("", 50, { onSearch });
    await screen.findByText("Showing 1 of 1");

    fireEvent.change(screen.getByLabelText("Search"), { target: { value: "wei" } });
    expect(onSearch).not.toHaveBeenCalled();
    fireEvent.submit(screen.getByRole("search"));
    expect(onSearch).toHaveBeenCalledWith("wei");
  });

  it("shows the empty-install message with the tiles at zero", async () => {
    stubFetchRoutes({
      [`GET ${adminHouseholdsPath("", 50)}`]: {
        status: 200,
        body: { metrics: { households: 0, activeHouseholds7d: 0, signups30d: { requested: 0, completed: 0 }, pendingInvites: 0 }, households: [], truncated: false },
      },
    });
    renderPage();
    expect(await screen.findByText("No households yet.")).toBeInTheDocument();
  });

  it("shows the no-match message with the query and a Clear link", async () => {
    stubFetchRoutes({ [`GET ${adminHouseholdsPath("zzz", 50)}`]: { status: 200, body: response([]) } });
    renderPage("zzz");
    expect(await screen.findByText("Nothing matches 'zzz'.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Clear" })).toBeInTheDocument();
  });

  it("offers Show more when truncated under the cap, and says search to narrow at it", async () => {
    stubFetchRoutes({
      [`GET ${adminHouseholdsPath("", 50)}`]: { status: 200, body: response([listing()], true) },
      [`GET ${adminHouseholdsPath("", 200)}`]: { status: 200, body: response([listing()], true) },
    });
    const onShowMore = vi.fn();
    const { unmount } = renderPage("", 50, { onShowMore });
    fireEvent.click(await screen.findByRole("button", { name: "Show more" }));
    expect(onShowMore).toHaveBeenCalled();
    unmount();

    renderPage("", 200);
    expect(await screen.findByText("Showing the first 1 — search to narrow")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Show more" })).not.toBeInTheDocument();
  });

  it("renders a skeleton, not a spinner, while loading", () => {
    stubFetchRoutes({ [`GET ${adminHouseholdsPath("", 50)}`]: { status: 200, body: response([listing()]) } });
    renderPage();
    expect(screen.getByTestId("households-skeleton")).toBeInTheDocument();
  });

  it("shows an inline error for a failure the gate does not own", async () => {
    stubFetchRoutes({
      [`GET ${adminHouseholdsPath("", 50)}`]: { status: 500, body: { error: { code: "INTERNAL", message: "Something broke." } } },
    });
    renderPage();
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Something broke."));
  });
});
```

If `apiFetch` reads the error envelope under a different key than
`error.message`, match whatever `AdminFlagsPage.test.tsx`'s own error test
sends — copy that body shape verbatim.

- [ ] **Step 2: Run and watch them fail**

```bash
cd web && npx vitest run src/features/admin/AdminHouseholdsPage.test.tsx
```
Expected: fails to import `./AdminHouseholdsPage`.

- [ ] **Step 3: The page**

Create `web/src/features/admin/AdminHouseholdsPage.tsx`:

```tsx
// The operator's households list: four counters, an explicit search over
// households and members, and one row per household linking to its
// drill-in. URL state (q, limit) arrives as props and navigation goes back
// out through onSearch/onShowMore -- the route in router.tsx owns the URL,
// the same seam the magic-link route uses, so this component renders under
// renderWithRouter's bare root route in tests.
//
// Search is a form, never a keystroke listener: every request under /admin
// is an audit row (useAdminDirectory.ts), so one submitted search is one
// request and one row.
import { type FormEvent, useState } from "react";
import { Link } from "@tanstack/react-router";
import { PageContainer } from "../../components/PageContainer";
import { ApiError } from "../../api/client";
import type { AdminHouseholdListing, AdminHouseholdsResponse } from "./adminDirectorySchemas";
import { dateLabel, exactTimeLabel, noMatchLabel, relativeTimeLabel, showingLabel } from "./directoryCopy";
import { DIRECTORY_MAX_LIMIT, useAdminHouseholds, useCloseSurfaceOnReauth } from "./useAdminDirectory";
import { isAdminLayerFailure } from "./useAdmin";

export function AdminHouseholdsPage({
  q,
  limit,
  onSearch,
  onShowMore,
}: {
  q: string;
  limit: number;
  onSearch: (q: string) => void;
  onShowMore: () => void;
}) {
  const query = useAdminHouseholds(q, limit);
  useCloseSurfaceOnReauth(query.error);
  const [draft, setDraft] = useState(q);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    onSearch(draft.trim());
  }

  // A gate-layer failure (lapsed grant, admin revoked) is about to be
  // replaced by AdminShell's own gate; rendering it inline too would flash
  // a second message for the same failure.
  const inlineError = query.error && !isAdminLayerFailure(query.error) ? query.error : null;

  return (
    <PageContainer>
      <h1 className="font-serif text-[22px] font-medium tracking-[-0.01em]">Households</h1>

      <form role="search" onSubmit={handleSubmit} className="flex flex-col gap-1.5 sm:max-w-[480px]">
        <label htmlFor="household-search" className="text-[12px] font-semibold text-label">
          Search
        </label>
        <div className="flex items-center gap-2">
          <input
            id="household-search"
            type="search"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder="Household, member name or email"
            className="min-w-0 flex-1 rounded-lg border border-hairline bg-card px-3 py-1.5 text-[13px] text-ink"
          />
          <button
            type="submit"
            className="rounded-lg bg-accent px-3 py-1.5 text-[12.5px] font-semibold text-white active:translate-y-px"
          >
            Search
          </button>
          {q !== "" && (
            <button
              type="button"
              onClick={() => {
                setDraft("");
                onSearch("");
              }}
              className="text-[12.5px] font-medium text-muted hover:text-ink"
            >
              Clear
            </button>
          )}
        </div>
      </form>

      {inlineError && (
        <div
          role="alert"
          className="rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[12.5px] text-danger"
        >
          {inlineError instanceof ApiError ? inlineError.message : "Something went wrong loading the households."}
        </div>
      )}

      {query.isPending ? (
        <HouseholdsSkeleton />
      ) : query.data ? (
        <>
          <MetricTiles metrics={query.data.metrics} />
          <HouseholdsTable data={query.data} q={q} limit={limit} onClear={() => onSearch("")} onShowMore={onShowMore} />
        </>
      ) : null}
    </PageContainer>
  );
}

function HouseholdsSkeleton() {
  return (
    <div data-testid="households-skeleton" aria-hidden="true" className="flex flex-col gap-5">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="h-[72px] rounded-lg border border-hairline bg-canvas" />
        ))}
      </div>
      <div className="flex flex-col divide-y divide-hairline">
        {[0, 1, 2, 3, 4].map((i) => (
          <div key={i} className="h-10 bg-canvas" />
        ))}
      </div>
    </div>
  );
}

function MetricTiles({ metrics }: { metrics: AdminHouseholdsResponse["metrics"] }) {
  const tiles: { label: string; lines: string[] }[] = [
    { label: "Households", lines: [String(metrics.households)] },
    { label: "Active, 7 days", lines: [String(metrics.activeHouseholds7d)] },
    { label: "Sign-ups, 30 days", lines: [`${metrics.signups30d.requested} requested`, `${metrics.signups30d.completed} completed`] },
    { label: "Invites pending", lines: [String(metrics.pendingInvites)] },
  ];
  return (
    <ul aria-label="Install metrics" className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      {tiles.map((tile) => (
        <li key={tile.label} className="rounded-lg border border-hairline bg-card px-4 py-3">
          <p className="text-[11.5px] font-semibold uppercase tracking-[0.04em] text-label">{tile.label}</p>
          {tile.lines.map((line) => (
            <p key={line} className="mt-1 text-[20px] font-medium tabular-nums text-ink">
              {line}
            </p>
          ))}
        </li>
      ))}
    </ul>
  );
}

function HouseholdsTable({
  data,
  q,
  limit,
  onClear,
  onShowMore,
}: {
  data: AdminHouseholdsResponse;
  q: string;
  limit: number;
  onClear: () => void;
  onShowMore: () => void;
}) {
  const now = new Date();
  if (data.households.length === 0) {
    return q === "" ? (
      <p className="text-[13px] text-muted">No households yet.</p>
    ) : (
      <p className="text-[13px] text-muted">
        {noMatchLabel(q)}{" "}
        <button type="button" onClick={onClear} className="font-medium text-accent">
          Clear
        </button>
      </p>
    );
  }
  const atCap = limit >= DIRECTORY_MAX_LIMIT;
  return (
    <div className="flex flex-col gap-3">
      <table className="w-full text-[12.5px]">
        <thead className="hidden md:table-header-group">
          <tr className="text-left text-[11.5px] font-semibold text-label">
            <th scope="col" className="py-1.5 pr-3">Name</th>
            <th scope="col" className="py-1.5 pr-3">Family</th>
            <th scope="col" className="py-1.5 pr-3">Members</th>
            <th scope="col" className="py-1.5 pr-3">Created</th>
            <th scope="col" className="py-1.5 pr-3">Last active</th>
            <th scope="col" className="py-1.5">Currency</th>
          </tr>
        </thead>
        <tbody>
          {data.households.map((h) => (
            <HouseholdRow key={h.id} household={h} now={now} />
          ))}
        </tbody>
      </table>
      <p className="flex items-center gap-3 text-[12px] text-muted">
        <span>{showingLabel(data.households.length, data.truncated, atCap)}</span>
        {data.truncated && !atCap && (
          <button type="button" onClick={onShowMore} className="font-semibold text-accent">
            Show more
          </button>
        )}
      </p>
    </div>
  );
}

// Below md each row is two lines: name and family, then members and last
// active -- the same collapse every table in this product makes at 320px.
function HouseholdRow({ household, now }: { household: AdminHouseholdListing; now: Date }) {
  const lastActive = relativeTimeLabel(household.lastActiveAt, now);
  return (
    <tr className="border-b border-hairline last:border-b-0 md:table-row">
      <td className="block py-2 pr-3 md:table-cell">
        <Link
          to="/admin/households/$householdId"
          params={{ householdId: household.id }}
          className="font-semibold text-ink hover:text-accent"
        >
          {household.name}
        </Link>
        {household.match && (
          <p className="text-[11.5px] text-muted">
            matched {household.match.memberName}
            {household.match.memberEmail ? ` · ${household.match.memberEmail}` : ""}
          </p>
        )}
        <span className="text-muted md:hidden"> · {household.familyName}</span>
      </td>
      <td className="hidden py-2 pr-3 md:table-cell">{household.familyName}</td>
      <td className="block pb-2 pr-3 text-muted md:table-cell md:py-2 md:text-ink">
        <span className="md:hidden">{household.memberCount} members · </span>
        <span className="hidden md:inline tabular-nums">{household.memberCount}</span>
        <span className="md:hidden" title={household.lastActiveAt ? exactTimeLabel(household.lastActiveAt) : undefined}>
          {lastActive}
        </span>
      </td>
      <td className="hidden py-2 pr-3 md:table-cell">{dateLabel(household.createdAt)}</td>
      <td className="hidden py-2 pr-3 md:table-cell" title={household.lastActiveAt ? exactTimeLabel(household.lastActiveAt) : undefined}>
        {lastActive}
      </td>
      <td className="hidden py-2 md:table-cell">{household.primaryCurrency}</td>
    </tr>
  );
}
```

- [ ] **Step 4: Run the page tests**

```bash
cd web && npx vitest run src/features/admin/AdminHouseholdsPage.test.tsx
```
Expected: PASS. If the `Link` to a route the test harness does not register
throws, wrap the anchor in the test's root route by giving `renderWithRouter`
a second child route — but check first: `AdminShell` already renders
`<Link to="/">` under the same harness in `AdminGate.test.tsx`, so an
unregistered `to` is expected to render.

- [ ] **Step 5: The route and the nav**

In `web/src/routes/router.tsx`, after `LazyAdminFlagsPage`:

```tsx
const LazyAdminHouseholdsPage = lazy(() =>
  import("../features/admin/AdminHouseholdsPage").then((m) => ({ default: m.AdminHouseholdsPage })),
);
```

after `adminFlagsRoute`:

```tsx
// The households list keeps its search and limit in the URL, so reload,
// back and the audit row all agree on what was shown. The page itself
// takes them as props and hands navigation back here -- the same split
// signInMagicRoute makes for its token.
const adminHouseholdsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "households",
  validateSearch: (search: Record<string, unknown>): { q: string; limit: number } => ({
    q: typeof search.q === "string" ? search.q : "",
    limit: typeof search.limit === "number" && Number.isInteger(search.limit) && search.limit > 0 ? search.limit : 50,
  }),
  component: () => {
    const { q, limit } = useSearch({ from: "/admin/households" });
    const navigate = useNavigate();
    return (
      <Suspense
        fallback={
          <main className="grid min-h-dvh place-items-center">
            <p className="text-sm text-muted">Loading…</p>
          </main>
        }
      >
        <LazyAdminHouseholdsPage
          q={q}
          limit={limit}
          onSearch={(next) => navigate({ to: "/admin/households", search: { q: next, limit: 50 } })}
          onShowMore={() => navigate({ to: "/admin/households", search: { q, limit: Math.min(limit * 2, 200) } })}
        />
      </Suspense>
    );
  },
});
```

`useNavigate` joins the existing `@tanstack/react-router` import. Add
`adminHouseholdsRoute` to `adminRoute.addChildren([...])`.

In `web/src/features/admin/AdminShell.tsx`, replace the header's right-hand
`Link` with a nav. Each link states its own single colour class and computes
its own active state with `useMatchRoute` — never `activeProps`, whose
className is concatenated onto the base and lost to the cascade
(`docs/LEARNING.md`, Frontend, the Money links).

```tsx
import { Link, Outlet, useMatchRoute } from "@tanstack/react-router";
```

```tsx
function OperatorNav() {
  const matchRoute = useMatchRoute();
  const items = [
    { to: "/admin/flags", label: "Flags" },
    { to: "/admin/households", label: "Households" },
  ] as const;
  return (
    <nav aria-label="Operator" className="flex items-center gap-4">
      {items.map((item) => {
        const active = Boolean(matchRoute({ to: item.to, fuzzy: true }));
        return (
          <Link
            key={item.to}
            to={item.to}
            aria-current={active ? "page" : undefined}
            className={active ? "text-[12.5px] font-semibold text-white" : "text-[12.5px] font-medium text-white/60 hover:text-white"}
          >
            {item.label}
          </Link>
        );
      })}
      <Link to="/" className="text-[12.5px] font-medium text-white/70 hover:text-white">
        Back to Hearth
      </Link>
    </nav>
  );
}
```

and in `AdminShell`'s header, replace the `Back to Hearth` `Link` with
`<OperatorNav />`. `matchRoute` with `fuzzy: true` also matches
`/admin/households/<id>`, so the drill-in keeps Households lit.

- [ ] **Step 6: Pin the bundle split**

In `web/src/features/admin/adminBundleSplit.test.ts`, extend the first
test's expectations:

```ts
    expect(reachable).not.toContain(join(SRC_ROOT, "features", "admin", "AdminHouseholdsPage.tsx"));
    expect(reachable).not.toContain(join(SRC_ROOT, "features", "admin", "AdminHouseholdPage.tsx"));
```

(The second file arrives in Task 9; the walk only checks absence, so the
assertion holds now and keeps holding.) Rename the test to
"never statically reaches any admin page from main.tsx".

- [ ] **Step 7: Run the whole web suite, lint, commit**

```bash
cd web && npx vitest run && npx tsc --noEmit && npm run lint
```
Expected: PASS. `router.test.tsx`'s "redirects a platform admin's bare /admin
to /admin/flags" still passes: the redirect is unchanged.

```bash
git add web/src/features/admin/AdminHouseholdsPage.tsx web/src/features/admin/AdminHouseholdsPage.test.tsx web/src/features/admin/AdminShell.tsx web/src/routes/router.tsx web/src/features/admin/adminBundleSplit.test.ts
git commit -m "feat(web): /admin/households -- tiles, explicit search, household rows"
```

---

### Task 9: The drill-in page

**Files:**
- Create: `web/src/features/admin/AdminHouseholdPage.tsx`
- Create: `web/src/features/admin/AdminHouseholdPage.test.tsx`
- Modify: `web/src/routes/router.tsx` (lazy import, route, `addChildren`)

**Interfaces:**
- Consumes: `useAdminHousehold`, `useCloseSurfaceOnReauth`, `isNotFound`
  (Task 7); `NotFoundScreen` from `../shell/NotFoundScreen`;
  `memberBadgeLabel` from `../settings/copy`; labels from `directoryCopy.ts`.
- Produces: `AdminHouseholdPage({ householdId })`.

- [ ] **Step 1: Write the failing tests**

Create `web/src/features/admin/AdminHouseholdPage.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AdminHouseholdPage } from "./AdminHouseholdPage";
import type { AdminHouseholdPage as PageData } from "./adminDirectorySchemas";

function page(overrides: Partial<PageData> = {}): PageData {
  return {
    household: { id: "h1", name: "Oentoro", familyName: "Oentoro", createdAt: "2026-08-15T02:11:09Z", primaryCurrency: "SGD" },
    members: [
      { userId: "u1", name: "Andreas", email: "andreas@example.org", channel: "email", role: "owner", capabilities: ["calendar", "chores", "money", "marriage"], lastActiveAt: "2026-09-02T07:40:12Z" },
      { userId: "u2", name: "Kid", email: null, channel: "telegram", role: "limited", capabilities: ["calendar"], lastActiveAt: null },
    ],
    pendingInvites: [
      { name: "Christine", email: "c@example.org", role: "owner", invitedByName: "Andreas", expiresAt: "2026-09-05T02:11:09Z" },
    ],
    lockout: null,
    ...overrides,
  };
}

const route = "GET /api/v1/admin/households/h1";

describe("AdminHouseholdPage", () => {
  it("renders the header, members with channel and role, and the pending invite", async () => {
    stubFetchRoutes({ [route]: { status: 200, body: page() } });
    renderWithRouter(<AdminHouseholdPage householdId="h1" />);

    expect(await screen.findByRole("heading", { name: "Oentoro" })).toBeInTheDocument();
    const members = within(screen.getByRole("table", { name: "Members" }));
    expect(members.getByText("andreas@example.org")).toBeInTheDocument();
    expect(members.getByText("Telegram")).toBeInTheDocument();
    expect(members.getByText("never")).toBeInTheDocument();
    expect(members.getAllByText("Owner").length).toBeGreaterThan(0);

    const invites = within(screen.getByRole("region", { name: "Pending invites" }));
    expect(invites.getByText("c@example.org")).toBeInTheDocument();
    expect(invites.getByText(/invited by Andreas/)).toBeInTheDocument();
  });

  it("shows the lockout callout only when the household is locked", async () => {
    stubFetchRoutes({ [route]: { status: 200, body: page() } });
    const { unmount } = renderWithRouter(<AdminHouseholdPage householdId="h1" />);
    await screen.findByRole("heading", { name: "Oentoro" });
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    unmount();

    stubFetchRoutes({
      [route]: { status: 200, body: page({ lockout: { lockedUntil: new Date(Date.now() + 14 * 60_000).toISOString() } }) },
    });
    renderWithRouter(<AdminHouseholdPage householdId="h1" />);
    const callout = await screen.findByRole("status");
    expect(callout).toHaveTextContent(/Sign-in is locked until/);
    expect(callout).toHaveTextContent("adminctl unlock-household");
  });

  it("says none pending when there are no invites", async () => {
    stubFetchRoutes({ [route]: { status: 200, body: page({ pendingInvites: [] }) } });
    renderWithRouter(<AdminHouseholdPage householdId="h1" />);
    expect(await screen.findByText("None pending.")).toBeInTheDocument();
  });

  it("renders the not-found screen for a 404, the page a non-admin would see", async () => {
    stubFetchRoutes({ [route]: { status: 404, body: { error: { code: "NOT_FOUND", message: "Not found." } } } });
    renderWithRouter(<AdminHouseholdPage householdId="h1" />);
    await waitFor(() => expect(screen.getByRole("heading", { name: /not found/i })).toBeInTheDocument());
  });
});
```

Match the 404 body shape and `NotFoundScreen`'s heading text to what
`AdminGate.test.tsx` already asserts — copy both literally from there.

- [ ] **Step 2: Run and watch them fail**

```bash
cd web && npx vitest run src/features/admin/AdminHouseholdPage.test.tsx
```
Expected: fails to import.

- [ ] **Step 3: The page**

Create `web/src/features/admin/AdminHouseholdPage.tsx`:

```tsx
// One household, read-only: who is in it, how they sign in, whether its
// password sign-in is currently locked, and who has been invited but has
// not arrived. No money -- the spec's boundary; financial data costs the
// database browse's deliberate second step.
import { Link } from "@tanstack/react-router";
import { PageContainer } from "../../components/PageContainer";
import { ApiError } from "../../api/client";
import { NotFoundScreen } from "../shell/NotFoundScreen";
import { memberBadgeLabel } from "../settings/copy";
import type { AdminHouseholdMember, AdminHouseholdPage as PageData } from "./adminDirectorySchemas";
import { dateLabel, exactTimeLabel, lockoutLabel, relativeTimeLabel } from "./directoryCopy";
import { isNotFound, useAdminHousehold, useCloseSurfaceOnReauth } from "./useAdminDirectory";
import { isAdminLayerFailure } from "./useAdmin";

export function AdminHouseholdPage({ householdId }: { householdId: string }) {
  const query = useAdminHousehold(householdId);
  useCloseSurfaceOnReauth(query.error);

  // A 404 here is "no such household" (or a malformed id, which the API
  // answers identically) and renders the same screen a non-admin sees for
  // the whole subtree -- nothing about the miss is worth distinguishing.
  if (isNotFound(query.error)) return <NotFoundScreen />;

  const inlineError = query.error && !isAdminLayerFailure(query.error) ? query.error : null;

  return (
    <PageContainer>
      <Link to="/admin/households" className="text-[12.5px] font-medium text-muted hover:text-ink">
        ‹ Households
      </Link>

      {inlineError && (
        <div role="alert" className="rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[12.5px] text-danger">
          {inlineError instanceof ApiError ? inlineError.message : "Something went wrong loading the household."}
        </div>
      )}

      {query.isPending ? (
        <div data-testid="household-skeleton" aria-hidden="true" className="flex flex-col gap-3">
          <div className="h-7 w-48 rounded bg-canvas" />
          <div className="h-4 w-80 rounded bg-canvas" />
          <div className="mt-4 h-24 rounded bg-canvas" />
        </div>
      ) : query.data ? (
        <HouseholdDetail data={query.data} />
      ) : null}
    </PageContainer>
  );
}

function HouseholdDetail({ data }: { data: PageData }) {
  const now = new Date();
  const { household, members, pendingInvites, lockout } = data;
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="font-serif text-[22px] font-medium tracking-[-0.01em]">{household.name}</h1>
        <p className="mt-0.5 text-[12.5px] text-muted">
          Family {household.familyName} · created {dateLabel(household.createdAt)} · {household.primaryCurrency} ·{" "}
          {members.length} {members.length === 1 ? "member" : "members"}
        </p>
      </header>

      {lockout && (
        <div
          role="status"
          className="rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[12.5px] text-danger"
        >
          <p className="font-semibold">{lockoutLabel(lockout.lockedUntil, now)}</p>
          <p className="mt-0.5">
            Clear it early with <code className="font-semibold">adminctl unlock-household --email &lt;owner&gt;</code>.
          </p>
        </div>
      )}

      <section aria-labelledby="members-heading" className="flex flex-col gap-2">
        <h2 id="members-heading" className="text-[13px] font-semibold text-ink">
          Members
        </h2>
        <table aria-label="Members" className="w-full text-[12.5px]">
          <thead className="hidden md:table-header-group">
            <tr className="text-left text-[11.5px] font-semibold text-label">
              <th scope="col" className="py-1.5 pr-3">Name</th>
              <th scope="col" className="py-1.5 pr-3">Channel</th>
              <th scope="col" className="py-1.5 pr-3">Role</th>
              <th scope="col" className="py-1.5 pr-3">Capabilities</th>
              <th scope="col" className="py-1.5">Last active</th>
            </tr>
          </thead>
          <tbody>
            {members.map((m) => (
              <MemberRow key={m.userId} member={m} now={now} />
            ))}
          </tbody>
        </table>
      </section>

      <section aria-labelledby="invites-heading" className="flex flex-col gap-2">
        <h2 id="invites-heading" className="text-[13px] font-semibold text-ink">
          Pending invites
        </h2>
        {pendingInvites.length === 0 ? (
          <p className="text-[12.5px] text-muted">None pending.</p>
        ) : (
          <ul className="divide-y divide-hairline text-[12.5px]">
            {pendingInvites.map((invite) => (
              <li key={invite.email} className="flex flex-col gap-0.5 py-2 md:flex-row md:items-center md:gap-4">
                <span className="font-semibold text-ink">{invite.name}</span>
                <span className="text-ink">{invite.email}</span>
                <span className="text-muted">{memberBadgeLabel(invite.role)}</span>
                <span className="text-muted">invited by {invite.invitedByName}</span>
                <span className="text-muted">expires {dateLabel(invite.expiresAt)}</span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

// Channel is text: the email address itself, or the word "Telegram". No
// icon -- the operator is reading a support ticket, not scanning a feed.
function MemberRow({ member, now }: { member: AdminHouseholdMember; now: Date }) {
  const channel = member.channel === "telegram" ? "Telegram" : (member.email ?? "");
  const lastActive = relativeTimeLabel(member.lastActiveAt, now);
  const badge =
    member.role === "limited" ? "bg-badge-limited-bg text-label" : "bg-badge-owner-bg text-accent";
  return (
    <tr className="border-b border-hairline last:border-b-0">
      <td className="block py-2 pr-3 md:table-cell">
        <span className="font-semibold text-ink">{member.name}</span>
        <span className="text-muted md:hidden"> · {channel}</span>
      </td>
      <td className="hidden py-2 pr-3 md:table-cell">{channel}</td>
      <td className="block pb-2 pr-3 md:table-cell md:py-2">
        <span className={`rounded-full px-2 py-0.5 text-[11px] font-semibold ${badge}`}>{memberBadgeLabel(member.role)}</span>
        <span className="ml-2 text-muted md:hidden">{member.capabilities.join(" ")} · </span>
        <span className="text-muted md:hidden" title={member.lastActiveAt ? exactTimeLabel(member.lastActiveAt) : undefined}>
          {lastActive}
        </span>
      </td>
      <td className="hidden py-2 pr-3 text-muted md:table-cell">{member.capabilities.join(" ")}</td>
      <td className={`hidden py-2 md:table-cell ${member.lastActiveAt ? "" : "text-muted"}`} title={member.lastActiveAt ? exactTimeLabel(member.lastActiveAt) : undefined}>
        {lastActive}
      </td>
    </tr>
  );
}
```

If `memberBadgeLabel` in `../settings/copy` takes a `domain`-shaped role
string and returns "Owner"/"Limited", the test's `getAllByText("Owner")`
holds; if its labels differ, assert whatever it returns for `"owner"`.

- [ ] **Step 4: Run the page tests**

```bash
cd web && npx vitest run src/features/admin/AdminHouseholdPage.test.tsx
```
Expected: PASS.

- [ ] **Step 5: The route**

In `web/src/routes/router.tsx`, after `LazyAdminHouseholdsPage`:

```tsx
const LazyAdminHouseholdPage = lazy(() =>
  import("../features/admin/AdminHouseholdPage").then((m) => ({ default: m.AdminHouseholdPage })),
);
```

after `adminHouseholdsRoute`:

```tsx
const adminHouseholdRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "households/$householdId",
  component: () => {
    const { householdId } = useParams({ from: "/admin/households/$householdId" });
    return (
      <Suspense
        fallback={
          <main className="grid min-h-dvh place-items-center">
            <p className="text-sm text-muted">Loading…</p>
          </main>
        }
      >
        <LazyAdminHouseholdPage householdId={householdId} />
      </Suspense>
    );
  },
});
```

Add `adminHouseholdRoute` to `adminRoute.addChildren([...])`.

- [ ] **Step 6: Whole web suite, lint, commit**

```bash
cd web && npx vitest run && npx tsc --noEmit && npm run lint
```
Expected: PASS.

```bash
git add web/src/features/admin/AdminHouseholdPage.tsx web/src/features/admin/AdminHouseholdPage.test.tsx web/src/routes/router.tsx
git commit -m "feat(web): /admin/households/{id} -- members, invites, lockout"
```

---

### Task 10: The documents that must not go stale

**Files:**
- Modify: `docs/SYSTEM_DESIGN.md` (§2 line ~391, §3 ports table, §4 route table ~line 805, §6 sessions entity ~line 2193, §7 frontend)
- Modify: `docs/FEATURE_TRACKER.md` (headline line 17, summary table ~line 637-647, §9 rows ~line 1384-1389, "Suggested order")
- Modify: `docs/LEARNING.md` (pattern 8 evidence; Catalogue by area)
- Modify: `docs/superpowers/specs/2026-09-01-hearth-admin-surface-design.md` (§6, §9)
- Modify: `docs/ADMIN_SURFACE_HANDOVER.md` (§3), `docs/HANDOVER.md` (§4)
- Modify: `deploy/README.md` (~line 207, the prune command)

No code. Use the `maintaining-system-design` skill for the first file; it
lists what each diagram must show.

- [ ] **Step 1: `docs/SYSTEM_DESIGN.md`**

Five places, each beside its existing admin sibling:

1. **§2 Backend layers**, next to the `AdminSvc` node: add
   `AdminDirectorySvc["AdminDirectoryService — Overview (metrics + search),<br/>Household (members, invites, lockout);<br/>reads across every household; no writes"]`
   with an edge to a new `AdminDirectoryRepository` port node and one to the
   existing `LoginAttemptRepository` node (the lockout reuses it). Under the
   diagram, one paragraph: why it is a separate service from `AdminService`
   (spec decision 8) and why it is the only cross-household port.
2. **§3 Ports and their adapters** table: a row for `AdminDirectoryRepository`
   — `adapter/postgres`, "Twenty-fifth. `Metrics`/`SearchHouseholds`/`Household`,
   read-only, the one port that reads across household boundaries; its SQL
   file is the only place `COALESCE(last_seen_at, created_at)` appears". Amend
   `SessionRepository`'s row (or the prose near line 535 on one-column writes)
   to name `Touch` as the third one-column write beside `Extend` and `GrantAdmin`.
3. **§4 Route table**: two rows after the flag routes —
   `GET | /admin/households?q=&limit= | session · admin · grant` and
   `GET | /admin/households/{householdID} | session · admin · grant`. In the
   `/admin` subtree prose, one sentence: the audit row now carries the query
   string in `detail`.
4. **§4 Request pipeline**, the `requireSession` description: add the touch
   after the extend — "touches `last_seen_at` when it is null or older than
   an hour; best-effort, like the extend".
5. **§6 Data model**, the `sessions` entity: add
   `timestamptz last_seen_at "nullable — last use, refreshed at most hourly; readers COALESCE with created_at"`.
6. **§7 Frontend structure**: the admin subtree gains `/admin/households`
   and `/admin/households/$householdId`, both lazy; `AdminShell` has a nav.

- [ ] **Step 2: `docs/FEATURE_TRACKER.md`**

1. §9 "Households and metrics" row: ⬜ → ✅. Cell text: "Design spec §6,
   expanded in `2026-09-02-hearth-admin-households-design.md`. Four tiles,
   explicit search over households and members (Telegram-only members by
   name), most-recently-active ordering from a throttled
   `sessions.last_seen_at`, and a read-only drill-in with members, channel,
   pending invites and the household's sign-in lockout. No money on either
   screen, asserted by exact key sets. Walked in Chrome, 15 of 15,
   `docs/superpowers/plans/2026-09-02-hearth-admin-households-verification.md`."
2. §9 "Admin flags screen" row stays 🟡; reword its first gap: "there is
   still no control to *create* a household override — **the household
   list that a picker needs now exists (`/admin/households`, 2026-09-02),
   the control itself does not**; the product owner chose to build support
   lookup and metrics first (households spec §1)". Leave the placement gap
   text as is.
3. Add a dated note at the top in the style of the others: what moved, and
   the recount.
4. Recount by the file's own rule — the first symbol in each row's cell —
   for §9 only, since nothing else changed: 5 ✅ / 1 🟡 / 2 ⬜ / 1 🚫. Summary
   table row "Platform administration | 5 | 1 | 2 | 1"; total row
   "78 | 17 | 22 | 3"; headline "95 of 120 features built or partly built".
   Count the symbols, do not adjust by delta — this file records why.
5. "Suggested order": strike item 2 as done the way item 1 is struck, and
   note the next item is the message inspector.

- [ ] **Step 3: `docs/LEARNING.md`**

1. Under pattern 8 ("Configuration that lies"), add evidence: a 30-day
   counter over a table that `adminctl prune --older-than` trims — the
   runbook's 30 keeps them aligned by convention only; the spec chose to
   document rather than couple a CLI flag to a query constant (households
   spec decision 10). What would catch it sooner: any counter over a pruned
   table should name its retention dependency where the prune is invoked.
2. In "Catalogue by area", record the three mutation checks (Tasks 1, 2, 5)
   with what each one killed, and any defect the walk in Task 11 finds.
3. If the walk found nothing, say so in one line — an empty result is a
   result.

- [ ] **Step 4: The admin-surface spec, the two handovers, the runbook**

- `2026-09-01-hearth-admin-surface-design.md` §6: prepend "**Expanded in
  `2026-09-02-hearth-admin-households-design.md`, which wins where the two
  differ.**" §9: change "Each of 4, 5 and 6 sits behind its own
  configuration" to "Each of 4 and 5 sits behind its own configuration …;
  6 needs none and is always available to a granted admin."
- `docs/ADMIN_SURFACE_HANDOVER.md` §3: move "Household list and metrics" out
  of "does NOT exist" into §2 with a two-line summary and the spec path;
  the recommended sequence becomes "message inspector → database browse".
- `docs/HANDOVER.md` §4: the paragraph beginning "The build order changed a
  second time" — households is built; two remain, inspector then browse.
- `deploy/README.md`, beside `adminctl prune --older-than=30`: "Keep this at
  30 or above: `/admin/households`' 30-day sign-up counter reads the rows
  this deletes."

- [ ] **Step 5: Commit**

```bash
git add docs/SYSTEM_DESIGN.md docs/FEATURE_TRACKER.md docs/LEARNING.md docs/superpowers/specs/2026-09-01-hearth-admin-surface-design.md docs/ADMIN_SURFACE_HANDOVER.md docs/HANDOVER.md deploy/README.md
git commit -m "docs: households and metrics -- design, tracker, learning log, handovers"
```

---

### Task 11: The browser walk, and finishing the branch

**Files:**
- Create: `docs/superpowers/plans/2026-09-02-hearth-admin-households-verification.md`
- Modify (if the walk finds a defect): whatever it names, plus
  `docs/LEARNING.md` and `docs/FEATURE_TRACKER.md` from Task 10

**Interfaces:** none. This task proves the others.

- [ ] **Step 1: Full suite on the integrated tree**

```bash
make lint && make test
```
Expected: green. Fix anything red before walking; a walk over a red tree
proves nothing.

- [ ] **Step 2: Seed and run**

```bash
make dev
make seed
```

Then, against the running database, add what the seed does not: a
Telegram-only member, a pending invite, and a second household with no
sessions. The exact commands:

```bash
# grant the seeded owner platform admin (adminctl prints the seed's email)
docker compose run --rm admin /app/adminctl grant-platform-admin --email <owner-email> --note "walk"
# a second household with nobody in it, and a Telegram-only member of the first
make psql <<'SQL'
INSERT INTO households (name, family_name) VALUES ('Tan', 'Tan');
WITH kid AS (
  INSERT INTO users (email, password_hash, display_name, avatar_initial) VALUES (NULL, NULL, 'Kid', 'K') RETURNING id
), m AS (
  INSERT INTO memberships (household_id, user_id, role, capabilities)
  SELECT h.id, kid.id, 'limited', '{calendar}' FROM households h, kid WHERE h.family_name = 'Oentoro' RETURNING user_id
)
INSERT INTO telegram_accounts (user_id, chat_id) SELECT user_id, 424242 FROM m;
SQL
```

The pending invite comes from the product: sign in as the owner, Settings →
invite `christine@example.org`. Do not accept it yet.

- [ ] **Step 3: Walk the fifteen criteria**

Use the browser tools (Claude in Chrome / Playwright MCP) at
http://localhost:5173, and record each criterion in the verification file in
the format `2026-09-02-hearth-admin-surface-verification.md` uses: the
criterion, what was done, what was seen, pass/fail, and a note where the
pass has a caveat.

1. Signed in as the limited member, `/admin/households` shows the not-found
   page, indistinguishable from a typo.
2. As the platform admin, the re-auth prompt appears; the correct password
   opens the list and the four tiles render.
3. Each tile equals the matching `psql` count, run in the same minute:
   `SELECT COUNT(*) FROM households`; the active query from
   `admin_directory.sql`; `SELECT COUNT(*), COUNT(consumed_at) FROM signups WHERE created_at >= now() - interval '30 days'`;
   `SELECT COUNT(*) FROM invites WHERE accepted_at IS NULL AND expires_at > now()`.
4. The header shows Flags and Households; the active link is visibly
   distinct — read `getComputedStyle(...).color` on both links, not only
   `aria-current`.
5. Searching the limited member's email (the seed prints it) finds the
   household and the row names them. Searching `christine@` finds nothing:
   an invitee is not a member until the invite is accepted. Record both.
6. Searching `oentoro` in the wrong case finds the household with no
   "matched" line.
7. Searching nonsense shows "Nothing matches 'nonsense'." and Clear
   restores the list.
8. One search is one request in the network panel and one new
   `admin_audit_log` row (`SELECT action, detail FROM admin_audit_log ORDER BY at DESC LIMIT 1`)
   whose `detail` holds `{"query": "q=...&limit=50"}`.
9. Searching `kid` finds the household through the Telegram-only member; the
   drill-in shows "Telegram" in that member's channel cell and no email.
10. The drill-in's members table shows roles, capabilities and last active;
    Kid shows "never".
11. The pending invite is listed with inviter and expiry. Accept it (Mailpit
    at :8025 has the link) in another browser profile; on reload it is gone,
    "None pending." shows, and the member count in the header rose by one.
12. Three wrong passwords on the household's sign-in make the lockout
    callout appear with a time; `docker compose run --rm admin /app/adminctl unlock-household --email <owner-email>`
    makes it disappear on reload.
13. A member's first request after sign-in sets `last_seen_at`
    (`SELECT last_seen_at FROM sessions ORDER BY created_at DESC LIMIT 1`);
    a second request a minute later leaves it unchanged.
14. At 320px (DevTools device toolbar), list rows collapse to two lines and
    neither page scrolls horizontally: `document.documentElement.scrollWidth <= window.innerWidth`
    on both.
15. `/admin/households/00000000-0000-0000-0000-000000000000` and
    `/admin/households/not-a-uuid` both show the not-found page.

Walk it as a first-time operator, not only against the list: the checklist
at the end of `docs/LEARNING.md` says why.

- [ ] **Step 4: Fix what the walk finds**

Any failure: fix it in the code with a test that would have caught it, add
the defect to `docs/LEARNING.md`, and re-run the criterion. A criterion
that passes with a caveat is recorded as a pass with the caveat named — a
🟡 with no explanation is worse than a ⬜.

- [ ] **Step 5: Commit the verification file, finish the branch**

```bash
git add docs/superpowers/plans/2026-09-02-hearth-admin-households-verification.md
git commit -m "docs: households and metrics -- the fifteen-criterion browser walk"
git status   # nothing untracked that the branch needs
```

Then the `superpowers:finishing-a-development-branch` skill: push
`admin-households`, open the PR against `main` with a body that names the
spec, the three mutation checks and the walk result, ending with the
session's PR trailer.

---

## Self-review against the spec

**Spec coverage.** §1 (not flag targeting, not money, not actions): Task 6
asserts key sets; nothing writes. §2 decisions 1–11: 3 → Tasks 1–2; 5 →
Task 3; 2, 4, 9 → Tasks 4–6; 6 → Tasks 4 and 9; 8 → Task 4; 10 → Task 10;
11 → Task 10. §3 → Task 1. §4 formulas → Task 5's SQL and Task 4's clamp.
§5 → Tasks 4–5. §6 → Task 6. §7 → Tasks 7–9 (nav, list, drill-in, data
layer, states). §8 → Tasks 2 (touch failure), 5 (`ErrNotFound`), 6
(malformed id, `channelString`), 9 (404 screen). §9 → every task's tests;
the three mutation checks in Tasks 1, 2 and 5; the walk in Task 11. §10 →
nothing built. §11 → Task 10. §12 → nothing to build; the migration is
Task 1. §13 → Task 11.

**Placeholders.** None: every step carries its code or its exact command.
Two steps say "check the generated name / copy the literal from the sibling
test" — those are verification instructions against files the executor has
in front of them, not deferred work.

**Type consistency.** `usecase.MemberChannel` / `ChannelEmail` /
`ChannelTelegram` (Task 4) are what Task 5 sets and Task 6 switches on.
`AdminDirectoryDeps{Directory, LoginAttempts, Clock, Policy}` (Task 4) is
what Task 6 wires. `adminHouseholdsPath(q, limit)` (Task 7) is what Task 8's
tests and hook both use. `AdminHouseholdsPage({q, limit, onSearch, onShowMore})`
(Task 8) is what the route passes. `AdminHouseholdPage({householdId})`
(Task 9) is what its route passes. `SessionRecord.LastSeenAt` and
`Sessions.Touch` (Task 1) are what Task 2 reads and calls.
