# Hearth Skeleton Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A clean-architecture Go API and a React frontend that start together with one command, backed by a migrated Postgres, with the dependency rule enforced by CI.

**Architecture:** Two deployables in one repository. The Go service layers `domain` → `usecase` → `adapter`, with dependencies pointing inward only; a lint script fails the build if that is violated. The React app talks only to `/api/v1`, proxied by Vite in development so requests stay same-origin.

**Tech Stack:** Go 1.25.7, chi v5, pgx v5, goose, testcontainers-go, Postgres 17, Vite 6, React 19, TypeScript 5 (strict), TanStack Router, TanStack Query, Tailwind 4, Docker Compose, GNU Make.

**Spec:** `docs/superpowers/specs/2026-07-26-hearth-foundation-design.md`

**Follow-on plan:** `docs/superpowers/plans/2026-07-26-hearth-identity.md` — do not start it until this plan's Task 5 is committed and green.

## Global Constraints

- Go module path: `github.com/andreasoentoro/hearth/api`. Go 1.25.7 — goose declares that minimum, so `golang:1.25-alpine` is not sufficient; pin the patch version.
- The directory `internal/adapter/http` declares `package httpadapter`, so it never shadows the standard library's `net/http`.
- `internal/domain` imports nothing from `internal/`. `internal/usecase` imports `internal/domain` only. Only `internal/adapter/**`, `cmd/**` and `internal/testsupport` may import third-party infrastructure libraries (pgx, chi, testcontainers). `internal/testsupport` is fixture code imported exclusively from `_test.go` files, which is why it is exempt. Enforced by `make lint-arch`.
- All money is `int64` minor units plus an ISO 4217 currency code. `float64` never appears in a monetary path.
- All configuration comes from environment variables. `APP_ENV` is one of `development`, `test`, `production`.
- The application URL during development is always `http://localhost:5173`. Port 8080 is reached through the Vite proxy, never directly by the browser.
- `design/` is reference material. Nothing in the build reads it.
- Every task ends with a commit. Commit messages use Conventional Commits (`feat:`, `chore:`, `test:`, `docs:`).

## File Structure

| Path | Responsibility |
|---|---|
| `api/go.mod` | Go module definition |
| `api/cmd/api/main.go` | Process entry: read config, open the pool, build the router, serve, shut down |
| `api/internal/config/config.go` | Environment variable parsing, one struct, no other responsibility |
| `api/internal/adapter/http/router.go` | Route table and middleware chain. Knows every handler, no business logic |
| `api/internal/adapter/http/health.go` | `/healthz` and `/readyz` handlers |
| `api/internal/adapter/http/respond.go` | JSON write helpers and the error envelope. Every handler uses these |
| `api/internal/adapter/postgres/pool.go` | pgx pool construction and `Ping` |
| `api/migrations/*.sql` | goose migrations |
| `api/Dockerfile` | Multi-stage: `dev` (air) and `prod` (distroless) |
| `api/.air.toml` | Hot reload configuration |
| `web/package.json` `web/vite.config.ts` | Frontend build and the `/api` dev proxy |
| `web/tailwind.config.ts` | Design tokens extracted from the design document |
| `web/src/main.tsx` `web/src/routes/` | App entry and route tree |
| `scripts/arch-lint.sh` | Dependency-rule enforcement |
| `docker-compose.yml` | postgres, migrate, api, web, mailpit |
| `Makefile` | Every command a developer types |
| `.env.example` | Documented configuration surface |

---

### Task 1: Go module, config, and a tested health endpoint

**Files:**
- Create: `api/go.mod`, `api/cmd/api/main.go`, `api/internal/config/config.go`, `api/internal/adapter/http/router.go`, `api/internal/adapter/http/health.go`, `api/internal/adapter/http/respond.go`
- Test: `api/internal/adapter/http/health_test.go`, `api/internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `config.Config{AppEnv string; Port int; DatabaseURL string; SessionSecret string}` and `config.Load() (Config, error)`
  - `httpadapter.Deps{Pinger Pinger}` where `type Pinger interface { Ping(ctx context.Context) error }`
  - `httpadapter.NewRouter(deps Deps) http.Handler`
  - `httpadapter.WriteJSON(w http.ResponseWriter, status int, body any)`
  - `httpadapter.WriteError(w http.ResponseWriter, status int, code, message string, details map[string]any)`

- [ ] **Step 1: Initialise the module**

```bash
mkdir -p api && cd api
go mod init github.com/andreasoentoro/hearth/api
go get github.com/go-chi/chi/v5@latest
```

- [ ] **Step 2: Write the failing config test**

Create `api/internal/config/config_test.go`:

```go
package config_test

import (
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/config"
)

func TestLoadReadsEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "postgres://hearth:hearth@localhost:5432/hearth?sslmode=disable")
	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789abcdef")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AppEnv != "development" || cfg.Port != 8080 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789abcdef")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error when DATABASE_URL is empty")
	}
}

func TestLoadRejectsUnknownAppEnv(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789abcdef")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error for an unknown APP_ENV")
	}
}
```

- [ ] **Step 3: Run it and watch it fail**

Run: `cd api && go test ./internal/config/...`
Expected: FAIL — `no required module provides package .../internal/config`.

- [ ] **Step 4: Implement config**

Create `api/internal/config/config.go`:

```go
// Package config turns environment variables into a validated Config value.
// It is the only place in the service that reads os.Getenv.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	AppEnv        string
	Port          int
	DatabaseURL   string
	SessionSecret string
}

func (c Config) IsDevelopment() bool { return c.AppEnv == "development" }

func Load() (Config, error) {
	cfg := Config{
		AppEnv:        env("APP_ENV", "development"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
	}

	switch cfg.AppEnv {
	case "development", "test", "production":
	default:
		return Config{}, fmt.Errorf("APP_ENV must be development, test or production, got %q", cfg.AppEnv)
	}

	port, err := strconv.Atoi(env("PORT", "8080"))
	if err != nil {
		return Config{}, fmt.Errorf("PORT must be a number: %w", err)
	}
	if port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("PORT must be between 1 and 65535, got %d", port)
	}
	cfg.Port = port

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if len(cfg.SessionSecret) < 32 {
		return Config{}, fmt.Errorf("SESSION_SECRET must be at least 32 characters")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

- [ ] **Step 5: Run the config tests**

Run: `cd api && go test ./internal/config/...`
Expected: PASS.

- [ ] **Step 6: Write the failing health test**

Create `api/internal/adapter/http/health_test.go`:

```go
package httpadapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
)

type stubPinger struct{ err error }

func (s stubPinger) Ping(context.Context) error { return s.err }

func TestHealthzIgnoresTheDatabase(t *testing.T) {
	r := httpadapter.NewRouter(httpadapter.Deps{Pinger: stubPinger{err: errors.New("database is down")}})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

func TestReadyzFailsWhenTheDatabaseIsUnreachable(t *testing.T) {
	r := httpadapter.NewRouter(httpadapter.Deps{Pinger: stubPinger{err: errors.New("database is down")}})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestReadyzSucceedsWhenTheDatabaseAnswers(t *testing.T) {
	r := httpadapter.NewRouter(httpadapter.Deps{Pinger: stubPinger{}})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestUnknownRouteReturnsTheErrorEnvelope(t *testing.T) {
	r := httpadapter.NewRouter(httpadapter.Deps{Pinger: stubPinger{}})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "NOT_FOUND" {
		t.Fatalf("error code = %q, want NOT_FOUND", body.Error.Code)
	}
}
```

- [ ] **Step 7: Run it and watch it fail**

Run: `cd api && go test ./internal/adapter/http/...`
Expected: FAIL — package does not exist.

- [ ] **Step 8: Implement the response helpers**

Create `api/internal/adapter/http/respond.go`:

```go
// Package httpadapter is the HTTP interface layer. It translates requests into
// use-case calls and use-case results into responses. It contains no business
// rules. The package is named httpadapter rather than http so that handlers can
// still refer to the standard library.
package httpadapter

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// WriteJSON writes body as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write json response", "error", err)
	}
}

// WriteError writes the single error envelope every failure response uses.
func WriteError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	WriteJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message, Details: details}})
}
```

- [ ] **Step 9: Implement the health handlers**

Create `api/internal/adapter/http/health.go`:

```go
package httpadapter

import (
	"context"
	"log/slog"
	"net/http"
)

// Pinger reports whether a backing store is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// handleHealthz answers liveness. It must never touch the database: an
// unreachable database is a readiness problem, not a reason to restart.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleReadyz(p Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := p.Ping(r.Context()); err != nil {
			slog.Warn("readiness check failed", "error", err)
			WriteError(w, http.StatusServiceUnavailable, "NOT_READY", "The service is not ready to accept traffic.", nil)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
```

- [ ] **Step 10: Implement the router**

Create `api/internal/adapter/http/router.go`:

```go
package httpadapter

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Deps carries everything the HTTP layer needs. Handlers receive their
// collaborators through this struct rather than reaching for globals.
type Deps struct {
	Pinger Pinger
}

func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, "NOT_FOUND", "That endpoint does not exist.", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "That method is not allowed here.", nil)
	})

	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", handleReadyz(deps.Pinger))

	return r
}
```

- [ ] **Step 11: Run the health tests**

Run: `cd api && go test ./...`
Expected: PASS, all four health tests plus the config tests.

- [ ] **Step 12: Write main**

Create `api/cmd/api/main.go`:

```go
// Command api is the Hearth HTTP service. It does wiring and nothing else:
// every decision it makes is which implementation to construct.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/config"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

// nilPinger stands in until the database pool exists in Task 2.
type nilPinger struct{}

func (nilPinger) Ping(context.Context) error { return nil }

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           httpadapter.NewRouter(httpadapter.Deps{Pinger: nilPinger{}}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Buffered so the goroutine never blocks, and so a bind failure is
	// distinguishable from a signal. Without this, a failed listen unblocks
	// ctx.Done() exactly like SIGTERM, Shutdown finds nothing to drain and
	// returns nil, and the process exits 0 having served zero requests — which
	// a supervisor reads as a clean stop rather than a crash.
	serveErr := make(chan error, 1)

	go func() {
		slog.Info("listening", "addr", srv.Addr, "env", cfg.AppEnv)
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			stop()
			return
		}
		serveErr <- nil
	}()

	<-ctx.Done()

	// The send happens-before stop(), which happens-before ctx.Done() returns,
	// so a bind error is already in the channel here. On a real signal the
	// channel is still empty and the default case falls through to shutdown.
	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("listen: %w", err)
		}
	default:
	}

	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
```

Add a config test asserting `Load()` rejects `PORT=0`, `-1` and `70000` and accepts `1` and `65535`, and a `cmd/api` test that binds a wildcard listener on port 0, points the server at the same port, and asserts `run()` returns a non-nil error. Run both with `-race`.

- [ ] **Step 13: Commit**

```bash
cd /Volumes/Oink_Machine/Intelij/HouseholdDashboard
git add api/
git commit -m "feat: add the Go service skeleton with config and health endpoints"
```

---

### Task 2: Postgres pool, goose migrations, and a real readiness check

**Files:**
- Create: `api/internal/adapter/postgres/pool.go`, `api/migrations/00001_init.sql`, `api/internal/adapter/postgres/pool_test.go`, `api/internal/testsupport/postgres.go`
- Modify: `api/cmd/api/main.go` (replace `nilPinger` with the real pool)

**Interfaces:**
- Consumes: `config.Config` from Task 1.
- Produces:
  - `postgres.Open(ctx context.Context, databaseURL string) (*postgres.DB, error)`
  - `(*postgres.DB).Ping(ctx context.Context) error` — satisfies `httpadapter.Pinger`
  - `(*postgres.DB).Pool() *pgxpool.Pool` — used by repositories in the identity plan
  - `(*postgres.DB).Close()`
  - `testsupport.StartPostgres(t *testing.T) string` — boots a throwaway Postgres with all migrations applied and returns its URL. Every repository test in the identity plan uses this.

- [ ] **Step 1: Add the dependencies**

```bash
cd api
go get github.com/jackc/pgx/v5/pgxpool@latest
go get github.com/pressly/goose/v3@latest
go get github.com/testcontainers/testcontainers-go@latest
go get github.com/testcontainers/testcontainers-go/modules/postgres@latest
```

- [ ] **Step 2: Write the first migration**

Create `api/migrations/00001_init.sql`:

```sql
-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE schema_smoke (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE schema_smoke;
```

This table exists only so the first migration is verifiable. The identity plan's first migration drops it.

- [ ] **Step 3: Write the failing pool test**

Create `api/internal/adapter/postgres/pool_test.go`:

```go
package postgres_test

import (
	"context"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
)

func TestOpenAndPing(t *testing.T) {
	url := testsupport.StartPostgres(t)

	db, err := postgres.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(db.Close)

	if err := db.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestMigrationsCreatedTheSchema(t *testing.T) {
	url := testsupport.StartPostgres(t)

	db, err := postgres.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(db.Close)

	var count int
	err = db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_name = 'schema_smoke'`).Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("schema_smoke table not found; migrations did not run")
	}
}

func TestOpenRejectsAnUnreachableDatabase(t *testing.T) {
	if _, err := postgres.Open(context.Background(), "postgres://nobody@127.0.0.1:1/none"); err == nil {
		t.Fatal("expected an error for an unreachable database")
	}
}
```

- [ ] **Step 4: Run it and watch it fail**

Run: `cd api && go test ./internal/adapter/postgres/...`
Expected: FAIL — neither package exists.

- [ ] **Step 5: Implement the pool**

Create `api/internal/adapter/postgres/pool.go`:

```go
// Package postgres holds every implementation that talks to Postgres. Nothing
// above the adapter layer imports it.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	pool *pgxpool.Pool
}

// Open builds a connection pool and verifies it is usable before returning.
func Open(ctx context.Context, databaseURL string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &DB{pool: pool}, nil
}

func (db *DB) Ping(ctx context.Context) error { return db.pool.Ping(ctx) }
func (db *DB) Pool() *pgxpool.Pool            { return db.pool }
func (db *DB) Close()                         { db.pool.Close() }
```

- [ ] **Step 6: Implement the test-container helper**

Create `api/internal/testsupport/postgres.go`:

```go
// Package testsupport provides fixtures shared by tests. It is only ever
// imported from _test.go files.
package testsupport

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// StartPostgres boots a disposable Postgres, applies every migration, and
// returns its connection URL. The container is removed when the test ends.
func StartPostgres(t *testing.T) string {
	t.Helper()

	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("hearth"),
		tcpostgres.WithUsername("hearth"),
		tcpostgres.WithPassword("hearth"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate container: %v", err)
		}
	})

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	applyMigrations(t, url)
	return url
}

func applyMigrations(t *testing.T, url string) {
	t.Helper()

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open for migrations: %v", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.Up(db, migrationsDir(t)); err != nil {
		t.Fatalf("goose up: %v", err)
	}
}

// migrationsDir resolves api/migrations from this file's own location, so tests
// pass regardless of the working directory they run from.
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine the caller path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
}
```

- [ ] **Step 7: Run the pool tests**

Run: `cd api && go test ./internal/adapter/postgres/... -v`
Expected: PASS. First run pulls `postgres:17-alpine`, so allow a minute. Docker must be running.

- [ ] **Step 8: Wire the real pool into main**

In `api/cmd/api/main.go`, delete the `nilPinger` type and its method, and replace the server construction inside `run()`:

```go
	db, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           httpadapter.NewRouter(httpadapter.Deps{Pinger: db}),
		ReadHeaderTimeout: 10 * time.Second,
	}
```

Move the `signal.NotifyContext` call above the `postgres.Open` call so `ctx` exists, and add the import `"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"`.

- [ ] **Step 9: Verify the whole suite still builds and passes**

Run: `cd api && go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add api/
git commit -m "feat: add the Postgres pool, goose migrations and a real readiness check"
```

---

### Task 3: The architecture lint

**Files:**
- Create: `scripts/arch-lint.sh`, `scripts/arch-lint_test.sh`

**Interfaces:**
- Consumes: the Go module from Task 1.
- Produces: `scripts/arch-lint.sh`, exit 0 when the dependency rule holds, exit 1 with a per-violation message otherwise. Invoked by `make lint-arch` in Task 4 and by CI.

- [ ] **Step 1: Write the failing check for the checker**

Create `scripts/arch-lint_test.sh`:

```bash
#!/usr/bin/env bash
# Verifies that arch-lint.sh actually rejects a violation, rather than passing
# vacuously. It plants a forbidden import, expects failure, then removes it.
set -euo pipefail

cd "$(dirname "$0")/.."

echo "case 1: the current tree must pass"
./scripts/arch-lint.sh

echo "case 2: a domain package importing an adapter must fail"
mkdir -p api/internal/domain
cat > api/internal/domain/violation.go <<'GO'
package domain

import _ "github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
GO

if ./scripts/arch-lint.sh; then
    rm -f api/internal/domain/violation.go
    rmdir api/internal/domain
    echo "FAIL: arch-lint.sh accepted a domain -> adapter import"
    exit 1
fi

rm -f api/internal/domain/violation.go
rmdir api/internal/domain

echo "case 3: a broken build must fail the lint"
mkdir -p api/internal/domain
cat > api/internal/domain/broken.go <<'GO'
package domain

stray statement here
GO

if ./scripts/arch-lint.sh; then
    rm -f api/internal/domain/broken.go
    rmdir api/internal/domain
    echo "FAIL: arch-lint.sh accepted a broken build"
    exit 1
fi

rm -f api/internal/domain/broken.go
rmdir api/internal/domain

echo "case 4: a violation in a _test.go file must be caught"
mkdir -p api/internal/domain
cat > api/internal/domain/domain.go <<'GO'
package domain
GO

cat > api/internal/domain/violation_test.go <<'GO'
package domain

import _ "github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
GO

if ./scripts/arch-lint.sh; then
    rm -f api/internal/domain/domain.go api/internal/domain/violation_test.go
    rmdir api/internal/domain
    echo "FAIL: arch-lint.sh accepted a test import violation"
    exit 1
fi

rm -f api/internal/domain/domain.go api/internal/domain/violation_test.go
rmdir api/internal/domain
echo "arch-lint self-check passed"
```

```bash
chmod +x scripts/arch-lint_test.sh
```

- [ ] **Step 2: Run it and watch it fail**

Run: `./scripts/arch-lint_test.sh`
Expected: FAIL — `./scripts/arch-lint.sh: No such file or directory`.

- [ ] **Step 3: Implement the linter**

Create `scripts/arch-lint.sh`:

```bash
#!/usr/bin/env bash
# Enforces the clean-architecture dependency rule:
#   internal/domain  imports no other internal package
#   internal/usecase imports internal/domain only
# Anything under internal/adapter, internal/testsupport and cmd may import
# whatever it needs.
set -euo pipefail

cd "$(dirname "$0")/../api"

MODULE="github.com/andreasoentoro/hearth/api"
violations=0

# A module that does not compile must be a hard error, not a violation-free
# pass. go list tolerates breakage that the compiler rejects — it can exit 0
# and silently omit the offending package's imports — so gate on a real build
# first, and run go list outside a process substitution so set -e can see it.
go build ./... >/dev/null

imports=$(go list -f '{{$p := .ImportPath}}{{range .Imports}}{{$p}} {{.}}
{{end}}{{range .TestImports}}{{$p}} {{.}}
{{end}}{{range .XTestImports}}{{$p}} {{.}}
{{end}}' ./...)

while read -r pkg imp; do
    [ -n "$pkg" ] || continue
    case "$pkg" in
        "$MODULE/internal/domain"|"$MODULE/internal/domain"/*)
            case "$imp" in
                "$MODULE/internal/domain"|"$MODULE/internal/domain"/*) ;;
                "$MODULE"|"$MODULE"/*)
                    echo "domain must not import internal packages: $pkg -> $imp"
                    violations=$((violations + 1))
                    ;;
            esac
            ;;
        "$MODULE/internal/usecase"|"$MODULE/internal/usecase"/*)
            case "$imp" in
                "$MODULE/internal/domain"|"$MODULE/internal/domain"/*) ;;
                "$MODULE/internal/usecase"|"$MODULE/internal/usecase"/*) ;;
                "$MODULE"|"$MODULE"/*)
                    echo "usecase may import domain only: $pkg -> $imp"
                    violations=$((violations + 1))
                    ;;
            esac
            ;;
    esac
done <<< "$imports"

if [ "$violations" -gt 0 ]; then
    echo "architecture lint failed with $violations violation(s)"
    exit 1
fi

echo "architecture lint passed"
```

```bash
chmod +x scripts/arch-lint.sh
```

- [ ] **Step 4: Run the self-check**

The self-check covers four cases: the clean tree passes; a planted `domain -> adapter` import is rejected; a package that does not compile is rejected rather than silently passing; and a violation that appears only in a `_test.go` file is rejected. Every case must leave the tree exactly as it found it, directories included.

Run: `./scripts/arch-lint_test.sh`
Expected: all four cases behave as stated, then `arch-lint self-check passed`. Confirm `git status --porcelain` is empty afterwards.

- [ ] **Step 5: Commit**

```bash
git add scripts/
git commit -m "chore: enforce the clean-architecture dependency rule in CI"
```

---

### Task 4: Docker, Compose and the Makefile

**Files:**
- Create: `api/Dockerfile`, `api/.air.toml`, `api/.dockerignore`, `docker-compose.yml`, `Makefile`, `.env.example`
- Modify: `.gitignore`

**Interfaces:**
- Consumes: the Go service from Tasks 1–2, the linter from Task 3.
- Produces: `make dev`, `make up`, `make down`, `make logs`, `make ps`, `make migrate`, `make migrate-new`, `make test-api`, `make lint`, `make lint-arch`, `make fmt`, `make psql`, `make shell-api`, `make build`. `make dev-local`, `make seed`, `make sqlc`, `make test-web` and the `adminctl` targets are added by Tasks 5 and by the identity plan; every target added later follows the same `##` help convention.

- [ ] **Step 1: Write the API Dockerfile**

Create `api/Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1

# --- dev: hot reload -------------------------------------------------------
FROM golang:1.25.7-alpine AS dev
WORKDIR /src
# air and goose are pinned, not @latest: air's newest release already requires a
# newer Go than this image ships, and goose could do the same tomorrow. Pinning
# keeps `docker compose build` from breaking out from under us with no warning.
# Bump these deliberately after checking the target version's go.mod against the
# golang:1.25.7-alpine base above.
RUN go install github.com/air-verse/air@v1.66.1 \
 && go install github.com/pressly/goose/v3/cmd/goose@v3.27.3
COPY go.mod go.sum ./
RUN go mod download
COPY . .
EXPOSE 8080
CMD ["air", "-c", ".air.toml"]

# --- builder ---------------------------------------------------------------
FROM golang:1.25.7-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# --- prod ------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS prod
COPY --from=builder /out/api /app/api
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/api"]
```

Create `api/.dockerignore`:

```
tmp/
*_test.go
.git
```

Create `api/.air.toml`:

```toml
root = "."
tmp_dir = "tmp"

[build]
  cmd = "go build -o ./tmp/api ./cmd/api"
  bin = "./tmp/api"
  include_ext = ["go", "sql"]
  exclude_dir = ["tmp", "migrations"]
  delay = 200

[log]
  time = true
```

- [ ] **Step 2: Write the Compose file**

Create `docker-compose.yml`:

```yaml
name: hearth

services:
  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: hearth
      POSTGRES_PASSWORD: hearth
      POSTGRES_DB: hearth
    ports: ["5432:5432"]
    volumes: ["hearth-pgdata:/var/lib/postgresql/data"]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U hearth -d hearth"]
      interval: 3s
      timeout: 3s
      retries: 20

  migrate:
    build: { context: ./api, target: dev }
    working_dir: /src
    environment:
      DATABASE_URL: postgres://hearth:hearth@postgres:5432/hearth?sslmode=disable
    command: sh -c 'goose -dir ./migrations postgres "$$DATABASE_URL" up'
    volumes: ["./api:/src"]
    depends_on:
      postgres: { condition: service_healthy }
    restart: "no"

  api:
    build: { context: ./api, target: dev }
    environment:
      APP_ENV: development
      PORT: "8080"
      DATABASE_URL: postgres://hearth:hearth@postgres:5432/hearth?sslmode=disable
      SESSION_SECRET: development-session-secret-not-for-production
      SMTP_ADDR: mailpit:1025
      APP_BASE_URL: http://localhost:5173
    ports: ["8080:8080"]
    volumes: ["./api:/src"]
    depends_on:
      postgres: { condition: service_healthy }
      migrate: { condition: service_completed_successfully }

  web:
    image: node:22-alpine
    working_dir: /app
    command: sh -c "npm install && npm run dev -- --host 0.0.0.0"
    environment:
      VITE_API_PROXY_TARGET: http://api:8080
    ports: ["5173:5173"]
    volumes:
      - ./web:/app
      - hearth-node-modules:/app/node_modules
    depends_on: [api]

  mailpit:
    image: axllent/mailpit:v1.30.5
    ports: ["8025:8025", "1025:1025"]

volumes:
  hearth-pgdata:
  hearth-node-modules:
```

- [ ] **Step 3: Write the Makefile**

Create `Makefile`:

```makefile
.DEFAULT_GOAL := help
SHELL := /bin/bash
COMPOSE := docker compose
.PHONY: help dev up down restart logs ps migrate migrate-down migrate-new \
        test test-api lint lint-arch fmt psql shell-api build

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

dev: ## Start everything and tail the logs — http://localhost:5173
	$(COMPOSE) up -d postgres mailpit
	$(COMPOSE) up --build api web

up: ## Start everything in the background
	$(COMPOSE) up -d --build postgres mailpit api web

down: ## Stop everything and remove the containers
	$(COMPOSE) down

restart: down up ## Restart everything

logs: ## Tail the api and web logs
	$(COMPOSE) logs -f api web

ps: ## Show container status
	$(COMPOSE) ps

migrate: ## Apply pending migrations
	$(COMPOSE) run --rm migrate

migrate-down: ## Roll back the most recent migration
	$(COMPOSE) run --rm migrate sh -c \
	  'goose -dir ./migrations postgres "$$DATABASE_URL" down'

migrate-new: ## Create a migration. make migrate-new NAME=add_users
	@test -n "$(NAME)" || { echo "NAME is required, e.g. make migrate-new NAME=add_users"; exit 1; }
	cd api && goose -dir ./migrations create $(NAME) sql

test: test-api ## Run every test suite

test-api: ## Run the Go tests (needs Docker for testcontainers)
	cd api && go test ./... -count=1

lint: lint-arch ## Run every linter
	cd api && go vet ./...

lint-arch: ## Check the clean-architecture dependency rule
	./scripts/arch-lint.sh

fmt: ## Format the Go code
	cd api && gofmt -w .

psql: ## Open a psql shell against the development database
	$(COMPOSE) exec postgres psql -U hearth -d hearth

shell-api: ## Open a shell inside the api container
	$(COMPOSE) exec api sh

build: ## Build the production images
	docker build --target prod -t hearth-api:latest ./api
	docker build --target prod -t hearth-web:latest ./web
```

`--target` is what selects a Dockerfile stage; a `--build-arg` cannot. `./web` has no Dockerfile until Task 5, so `make build` fails until then — that is expected and Task 5 closes it.

- [ ] **Step 4: Write `.env.example` and update `.gitignore`**

Create `.env.example`:

```bash
# Copy to .env for local overrides. Compose already sets development defaults,
# so .env is only needed when you want to change them.
APP_ENV=development
PORT=8080
DATABASE_URL=postgres://hearth:hearth@localhost:5432/hearth?sslmode=disable
SESSION_SECRET=development-session-secret-not-for-production
SMTP_ADDR=localhost:1025
APP_BASE_URL=http://localhost:5173
```

Append to `.gitignore`:

```
.env
api/tmp/
web/node_modules/
web/dist/
```

- [ ] **Step 5: Verify the stack starts**

Run: `make up && sleep 5 && curl -sf http://localhost:8080/readyz`
Expected: `{"status":"ready"}`. The `web` service will fail until Task 5 creates `web/package.json`; that is expected at this point.

Then: `make down`

- [ ] **Step 6: Verify the help output**

Run: `make`
Expected: the target list, `dev` described as `Start everything and tail the logs — http://localhost:5173`.

- [ ] **Step 7: Commit**

```bash
git add Makefile docker-compose.yml .env.example .gitignore api/Dockerfile api/.air.toml api/.dockerignore
git commit -m "chore: add Docker, Compose and the Makefile development workflow"
```

---

### Task 5: The React application shell and design tokens

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/index.html`, `web/src/main.tsx`, `web/src/App.tsx`, `web/src/index.css`, `web/src/api/client.ts`, `web/src/api/client.test.ts`, `web/vitest.config.ts`, `web/Dockerfile`, `web/nginx.conf`, `web/.dockerignore`
- Modify: `Makefile` (add `dev-local`, `test-web`, `fmt` for the frontend)

**Interfaces:**
- Consumes: `/healthz` from Task 1, the Compose `web` service from Task 4.
- Produces:
  - `apiFetch<T>(path: string, init?: RequestInit): Promise<T>` — throws `ApiError` on a non-2xx response
  - `class ApiError extends Error { code: string; status: number; details: Record<string, unknown> }`
  - Tailwind 4 theme tokens declared in `web/src/index.css` via `@theme` — colours `canvas`, `surface`, `card`, `ink`, `muted`, `hairline`, `accent`, `accent-dark`; fonts `sans`, `serif`, `alt`, `mono`. Tailwind 4 needs no `tailwind.config.ts`; the CSS block is the config.
  - `web/Dockerfile` with a `prod` stage, so `make build` succeeds from this task onward
  - No router is installed in this task — `App.tsx` is a single placeholder component. The identity plan adds `web/src/features/` and builds the route tree from scratch there.

- [ ] **Step 1: Scaffold the frontend**

```bash
cd /Volumes/Oink_Machine/Intelij/HouseholdDashboard
npm create vite@latest web -- --template react-ts
cd web
npm install
npm install @tanstack/react-router @tanstack/react-query zod react-hook-form @hookform/resolvers
npm install -D tailwindcss @tailwindcss/vite vitest @testing-library/react @testing-library/jest-dom jsdom prettier
```

Pin every version rather than accepting a floating `@latest` — Task 4 was broken twice by floating tool versions and the same exposure applies here. Add `@types/node` too: the Vite config below reads `process.env`.

Collapse whatever `tsconfig.*.json` files the scaffold generates into the single `web/tsconfig.json` this plan specifies, and delete any `tailwind.config.ts` it produces — Tailwind 4 is configured by the `@theme` block in `src/index.css`, and a config file would be a second source of truth. Delete the rest of the template debris as well: `App.css`, `assets/`, and the scaffold's own `index.css` body.

- [ ] **Step 2: Configure Vite with the API proxy**

Replace `web/vite.config.ts`:

```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// The browser only ever talks to this dev server. /api is proxied to the Go
// service so requests stay same-origin and the session cookie applies without
// any CORS configuration.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: process.env.VITE_API_PROXY_TARGET ?? "http://localhost:8080",
        changeOrigin: false,
      },
      "/healthz": {
        target: process.env.VITE_API_PROXY_TARGET ?? "http://localhost:8080",
      },
    },
  },
});
```

- [ ] **Step 3: Extract the design tokens**

Create `web/src/index.css`:

```css
@import "tailwindcss";

/* Tokens read from design/Household Dashboard.dc.html. Every screen composes
   from these; nothing hard-codes a hex value. */
@theme {
  --color-canvas: #f0eee9;
  --color-surface: #fafaf9;
  --color-card: #ffffff;
  --color-ink: #1c1b18;
  --color-muted: rgba(0, 0, 0, 0.55);
  --color-hairline: rgba(0, 0, 0, 0.08);
  --color-accent: #1a6b52;
  --color-accent-dark: #12503d;

  --font-sans: "Schibsted Grotesk", system-ui, sans-serif;
  --font-serif: "Newsreader", Georgia, serif;
  --font-alt: "Karla", system-ui, sans-serif;
  --font-mono: "IBM Plex Mono", ui-monospace, Menlo, monospace;

  --radius-card: 8px;
  --shadow-card: 0 1px 3px rgba(0, 0, 0, 0.06);
}

body {
  margin: 0;
  background: var(--color-canvas);
  color: var(--color-ink);
  font-family: var(--font-sans);
}
```

Replace `web/index.html`'s `<head>` contents with the title and the Google Fonts links copied from the design document's `<helmet>` block (Schibsted Grotesk, Newsreader, Karla, IBM Plex Mono, IBM Plex Sans), then `<link rel="stylesheet" href="/src/index.css">` is unnecessary because `main.tsx` imports it.

- [ ] **Step 4: Write the failing API-client test**

Create `web/src/api/client.test.ts`:

```ts
import { describe, expect, it, vi, afterEach } from "vitest";
import { apiFetch, ApiError } from "./client";

afterEach(() => vi.unstubAllGlobals());

function stubFetch(status: number, body: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
}

describe("apiFetch", () => {
  it("returns the parsed body on success", async () => {
    stubFetch(200, { status: "ok" });
    await expect(apiFetch<{ status: string }>("/healthz")).resolves.toEqual({
      status: "ok",
    });
  });

  it("throws an ApiError carrying the server's code", async () => {
    stubFetch(401, {
      error: { code: "INVALID_CREDENTIALS", message: "Wrong password." },
    });

    await expect(apiFetch("/api/v1/auth/me")).rejects.toMatchObject({
      code: "INVALID_CREDENTIALS",
      status: 401,
    });
  });

  it("throws ApiError with an UNKNOWN code when the body is not our envelope", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("<html>502</html>", { status: 502 })),
    );

    // Narrow at the use site: apiFetch returns Promise<unknown>, so the caught
    // value is unknown and has no .code until it is asserted.
    const error = await apiFetch("/api/v1/anything").catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).code).toBe("UNKNOWN");
  });
});
```

Create `web/vitest.config.ts`:

```ts
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test-setup.ts"],
  },
});
```

Create `web/src/test-setup.ts` so `@testing-library/jest-dom`'s matchers exist — without it the library is installed but never registered, and the first component test in the next plan fails on a missing matcher:

```ts
import "@testing-library/jest-dom/vitest";
```

- [ ] **Step 5: Run it and watch it fail**

Run: `cd web && npx vitest run`
Expected: FAIL — cannot resolve `./client`.

- [ ] **Step 6: Implement the API client**

Create `web/src/api/client.ts`:

```ts
// The single entry point for talking to the Go service. Every request goes
// through here so that credentials, the CSRF header and error decoding are
// handled in exactly one place.

export class ApiError extends Error {
  readonly code: string;
  readonly status: number;
  readonly details: Record<string, unknown>;

  constructor(
    status: number,
    code: string,
    message: string,
    details: Record<string, unknown> = {},
  ) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

function readCookie(name: string): string | undefined {
  return document.cookie
    .split("; ")
    .find((row) => row.startsWith(`${name}=`))
    ?.split("=")[1];
}

export async function apiFetch<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const method = (init.method ?? "GET").toUpperCase();
  const headers = new Headers(init.headers);

  if (init.body !== undefined && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  if (method !== "GET" && method !== "HEAD") {
    const token = readCookie("csrf_token");
    if (token) headers.set("X-CSRF-Token", token);
  }

  const response = await fetch(path, {
    ...init,
    headers,
    credentials: "same-origin",
  });

  if (response.status === 204) {
    return undefined as T;
  }

  const text = await response.text();
  let parsed: unknown = undefined;
  try {
    parsed = text ? JSON.parse(text) : undefined;
  } catch {
    parsed = undefined;
  }

  if (!response.ok) {
    const envelope = parsed as
      | { error?: { code?: string; message?: string; details?: Record<string, unknown> } }
      | undefined;
    throw new ApiError(
      response.status,
      envelope?.error?.code ?? "UNKNOWN",
      envelope?.error?.message ?? `Request failed with status ${response.status}.`,
      envelope?.error?.details ?? {},
    );
  }

  // An ok response whose body is absent or unparseable must fail loudly.
  // Returning `undefined as T` here would hand callers a type lie they cannot
  // detect, and the crash would surface far from the cause.
  if (parsed === undefined) {
    throw new ApiError(
      response.status,
      "INVALID_RESPONSE",
      "The server returned a body that is not JSON.",
    );
  }

  return parsed as T;
}
```

- [ ] **Step 7: Run the client tests**

Run: `cd web && npx vitest run`
Expected: PASS, three tests.

- [ ] **Step 8: Build the placeholder app shell**

Create `web/src/main.tsx`:

```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "./index.css";
import { App } from "./App";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: 30_000 } },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
);
```

Create `web/src/App.tsx`:

```tsx
import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "./api/client";

// A deliberately minimal shell. The identity plan replaces this with the real
// sidebar; its only job today is to prove the proxy and the stack are wired.
export function App() {
  const health = useQuery({
    queryKey: ["healthz"],
    queryFn: () => apiFetch<{ status: string }>("/healthz"),
  });

  return (
    <main className="min-h-screen grid place-items-center p-10">
      <div className="bg-card border border-hairline rounded-[8px] shadow-[var(--shadow-card)] p-8 max-w-md">
        <h1 className="font-serif text-2xl mb-2">Hearth</h1>
        <p className="text-muted text-sm mb-6">
          Skeleton is running. Identity arrives in the next plan.
        </p>
        <p className="font-mono text-xs">
          API:{" "}
          {health.isPending
            ? "checking…"
            : health.isError
              ? "unreachable"
              : health.data?.status}
        </p>
      </div>
    </main>
  );
}
```

Delete the Vite template leftovers: `web/src/App.css`, `web/src/assets/react.svg`, and the template body of `web/src/index.css` if the scaffold wrote one above your `@import`.

- [ ] **Step 8b: Write the web production image**

Create `web/Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1

FROM node:22-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine AS prod
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
```

Create `web/nginx.conf`:

```nginx
server {
    listen 80;
    root /usr/share/nginx/html;

    # The SPA owns routing: any unknown path serves index.html.
    location / {
        try_files $uri $uri/ /index.html;
    }

    # Health endpoints must reach the API in production exactly as the Vite
    # proxy routes them in development. Without these, they fall through to the
    # SPA fallback below and return index.html with a 200 — a difference that
    # only appears in production.
    location = /healthz {
        proxy_pass http://api:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /readyz {
        proxy_pass http://api:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Same-origin API, so no CORS in production either.
    location /api/ {
        proxy_pass http://api:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Create `web/.dockerignore`:

```
node_modules
dist
```

Verify: `make build`
Expected: both images build. This is the first point at which `make build` can succeed.

- [ ] **Step 9: Add the frontend Make targets**

In `Makefile`, add to `.PHONY` and append these targets:

```makefile
dev-local: ## Run api and web natively, infra in Docker (Ctrl-C stops both)
	$(COMPOSE) up -d postgres mailpit
	$(MAKE) migrate
	@trap 'kill 0' EXIT INT TERM; \
	 (cd api && air -c .air.toml 2>&1 | sed 's/^/[api] /') & \
	 (cd web && npm run dev 2>&1 | sed 's/^/[web] /') & \
	 wait

test-web: ## Run the frontend tests
	cd web && npx vitest run

typecheck: ## Type-check the frontend
	cd web && npx tsc --noEmit
```

Change `test:` to depend on both: `test: test-api test-web ## Run every test suite`; extend `fmt:` with `&& cd ../web && npx prettier --write src`; and make `lint` depend on `typecheck` as well as `lint-arch`, so the enforced gate actually type-checks the frontend. `vitest` transforms with esbuild and never type-checks, so without this a type error reaches only `make build`.

- [ ] **Step 10: Verify the full stack end to end**

Run: `make dev`

Then, in a second terminal:

```bash
curl -sf http://localhost:5173/healthz
```

Expected: `{"status":"ok"}` — proving the Vite proxy reaches the Go service. Open `http://localhost:5173` in a browser and confirm the card reads `API: ok`. Ctrl-C the `make dev` terminal and confirm both services stop.

- [ ] **Step 11: Verify the whole gate**

Run: `make lint-arch && make test`
Expected: architecture lint passes; Go tests pass; Vitest passes.

- [ ] **Step 12: Commit**

```bash
git add web/ Makefile
git commit -m "feat: add the React frontend shell, design tokens and API client"
```

---

## Definition of done for this plan

On a clean checkout:

1. `make` prints the target list.
2. `make dev` starts Postgres, Mailpit, migrations, the API and the web server, and tailing logs show both.
3. `http://localhost:5173` renders the Hearth card reading `API: ok`.
4. `curl http://localhost:8080/readyz` returns `{"status":"ready"}`.
5. `make test` passes both suites.
6. `make lint-arch` passes, and `./scripts/arch-lint_test.sh` proves it can fail.
7. Ctrl-C on `make dev` leaves no process holding port 5173 or 8080.

Only then start `docs/superpowers/plans/2026-07-26-hearth-identity.md`.
