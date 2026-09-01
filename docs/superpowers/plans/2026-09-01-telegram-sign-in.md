# Telegram Sign-In Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a person sign up for, and sign in to, Hearth through a Telegram bot, so the product works in production where email cannot be delivered.

**Architecture:** A new outbound-only adapter (`internal/adapter/telegram`) long-polls Telegram's `getUpdates` from inside the API process and sends messages back. It adds no public HTTP route. A new `TelegramAuthService` mints **no tokens of its own** — it calls the existing `MagicLinkRepository` and `SignupRepository`, so consuming a Telegram-delivered link runs the handlers that already exist. The bot replies with a URL, and tapping it signs the user in on the device holding Telegram; there is no cross-device handoff.

**Tech Stack:** Go 1.24, chi, pgx/v5, sqlc, goose migrations, testcontainers-go, Postgres 17. Frontend: React + TypeScript + Vite + Vitest.

**Spec:** `docs/superpowers/specs/2026-09-01-telegram-sign-in-design.md` — read it before Task 1. The plan argues from the spec; both travel together.

## Global Constraints

- **Go is not on `PATH`.** Prefix every Go or `make` command with:
  `export PATH=$PATH:/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin`
- **Go tests need a Docker socket** (testcontainers). On this machine:
  `export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock`
  `export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`
- **Clean architecture, enforced by `make lint-arch`.** `internal/domain` imports only the standard library. `internal/usecase` may add `internal/domain`. Everything else lives under `internal/adapter/**` or `cmd/**`. No Telegram library type, HTTP type or pgx type may appear in `domain` or `usecase`.
- **A missing row becomes `domain.ErrNotFound` at the adapter boundary**, never `pgx.ErrNoRows` further up. Use the existing `translate(err, "...")` helper in `internal/adapter/postgres`.
- **Every 2xx except 204 carries a JSON body.** `apiFetch` throws on an ok response it cannot parse.
- **Fail closed on values you did not construct.** Any `switch` over a value from a database column or a request needs a `default` that refuses.
- **Authorisation exists only in the HTTP layer.** No service takes an actor parameter.
- **Secrets are never logged.** `TELEGRAM_BOT_TOKEN` appears in no log line, including error paths. `chat_id` is logged only through `hashPrefix(...)`, exactly as `email_hash` is today.
- **Comments say why, not what.** Exported things carry their contract in a doc comment; `usecase/ports.go` is the model.
- **After every task:** `make lint && make test` green before the commit in that task's final step.
- **Nonce TTL is 10 minutes. Per-chat limit is 3 issued links per hour.** `magicLinkTTL` (15 min) and `SignupTTL` (24 h) are unchanged and are reused as-is.

---

## File Structure

**Created:**

| Path | Responsibility |
|---|---|
| `api/migrations/00011_telegram.sql` | Two new tables; widen `signups` to carry a chat instead of an email |
| `api/internal/adapter/postgres/queries/telegram.sql` | sqlc queries for the two new tables |
| `api/internal/adapter/postgres/telegram_link_repo.go` | Pending deep-link nonces |
| `api/internal/adapter/postgres/telegram_account_repo.go` | Chat ↔ user binding |
| `api/internal/adapter/telegram/client.go` | Outbound `sendMessage` |
| `api/internal/adapter/telegram/update.go` | Parse one Telegram Update into `StartCommand` |
| `api/internal/adapter/telegram/poller.go` | `getUpdates` loop: offset, backoff, recover |
| `api/internal/usecase/telegram_auth.go` | `TelegramAuthService.HandleStart` |
| `api/internal/adapter/http/telegram_handlers.go` | `POST /api/v1/auth/telegram/start` |
| `docs/adr/0004-telegram-as-a-second-delivery-channel.md` | The decision record |

**Modified:**

| Path | Change |
|---|---|
| `api/internal/config/config.go` | Two new values, both-or-neither |
| `api/internal/usecase/ports.go` | Three new ports; `SignupRepository.CreateForTelegram`; `SignupDetails.TelegramChatID`; `SignupPreview.Channel` |
| `api/internal/usecase/signup.go` | `Preview` reports the channel. No new dependency |
| `api/internal/adapter/postgres/queries/signup.sql` | `CreateTelegramSignup`; `ConsumeSignup` returns the chat id |
| `api/internal/adapter/postgres/signup_repo.go` | `CreateForTelegram`; `Provision` binds the chat inside its existing transaction |
| `api/internal/adapter/http/router.go` | `Deps.Telegram`, the new route, feature-off 404 |
| `api/cmd/api/main.go` | Construct client, repos, service; start the poller |
| `web/src/features/auth/SignInScreen.tsx` | "Continue with Telegram" |
| `web/src/features/auth/SignUpCompleteScreen.tsx` | Render the channel, not an empty email box |
| `web/src/features/auth/copy.ts` | New strings |
| `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/INFRASTRUCTURE.md`, `docs/LEARNING.md`, `docs/adr/0003-mail-stays-on-the-box.md` | Kept true, per CLAUDE.md |

---

## Task 1: Schema and generated queries

**Files:**
- Create: `api/migrations/00011_telegram.sql`
- Create: `api/internal/adapter/postgres/queries/telegram.sql`
- Modify: `api/internal/adapter/postgres/queries/signup.sql`
- Test: `api/internal/adapter/postgres/telegram_schema_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: tables `telegram_link_requests`, `telegram_accounts`; `signups.telegram_chat_id`; generated `sqlcgen` methods `CreateTelegramLinkRequest`, `ConsumeTelegramLinkRequest`, `CountTelegramLinksSince`, `GetTelegramAccountByChatID`, `CreateTelegramAccount`, `CreateTelegramSignup`; `ConsumeSignup` now returns `TelegramChatID *int64`.

- [ ] **Step 1: Write the migration**

Create `api/migrations/00011_telegram.sql`:

```sql
-- +goose Up
CREATE TABLE telegram_link_requests (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    nonce_hash  bytea       NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    -- Both NULL until the nonce is redeemed, then both set in one statement.
    -- The chat is unknown when the nonce is minted: the browser has not met
    -- Telegram yet. Redemption is the only moment the two can be joined, and
    -- the per-chat rate limit counts these rows, so a redemption that failed
    -- to record its chat would be a limit that silently never fires.
    consumed_at timestamptz,
    chat_id     bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT consumed_rows_name_their_chat
        CHECK ((consumed_at IS NULL) = (chat_id IS NULL))
);

CREATE INDEX telegram_link_requests_chat_consumed_idx
    ON telegram_link_requests (chat_id, consumed_at DESC);

-- One Telegram account per user, and one user per Telegram account. Both
-- directions matter: without the chat_id unique, two users could bind the same
-- chat and a sign-in would be ambiguous; without the user_id unique, a user
-- could accumulate chats and a revocation would miss one.
CREATE TABLE telegram_accounts (
    id        uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   uuid        NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    chat_id   bigint      NOT NULL UNIQUE,
    linked_at timestamptz NOT NULL DEFAULT now()
);

-- A Telegram sign-up has no address at all, so email stops being mandatory and
-- a chat id takes its place. The CHECK is the fail-closed half: a row carrying
-- both channels, or neither, is refused by the database rather than reasoned
-- about in Go.
--
-- The backfill is a no-op. Every existing row has a non-NULL email and, after
-- the ADD COLUMN, a NULL telegram_chat_id, so the constraint holds for all of
-- them at ADD CONSTRAINT time. Run this against a restored production dump
-- before running it against production -- it is the first migration here to
-- constrain a table that already holds real rows.
ALTER TABLE signups ALTER COLUMN email DROP NOT NULL;
ALTER TABLE signups ADD COLUMN telegram_chat_id bigint;
ALTER TABLE signups ADD CONSTRAINT signups_have_exactly_one_channel
    CHECK ((email IS NULL) <> (telegram_chat_id IS NULL));

-- +goose Down
ALTER TABLE signups DROP CONSTRAINT signups_have_exactly_one_channel;
ALTER TABLE signups DROP COLUMN telegram_chat_id;
DELETE FROM signups WHERE email IS NULL;
ALTER TABLE signups ALTER COLUMN email SET NOT NULL;
DROP TABLE telegram_accounts;
DROP TABLE telegram_link_requests;
```

- [ ] **Step 2: Write the queries**

Create `api/internal/adapter/postgres/queries/telegram.sql`:

```sql
-- name: CreateTelegramLinkRequest :exec
INSERT INTO telegram_link_requests (nonce_hash, expires_at)
VALUES ($1, $2);

-- ConsumeTelegramLinkRequest is the single-use gate, and it records the
-- redeeming chat in the same statement. The guard lives here rather than in
-- the caller for the same reason ConsumeSignup's does: zero rows is the
-- authoritative answer to the race between a read and this write.
-- name: ConsumeTelegramLinkRequest :one
UPDATE telegram_link_requests
SET consumed_at = now(), chat_id = $2
WHERE nonce_hash = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING id;

-- name: CountTelegramLinksSince :one
SELECT count(*) FROM telegram_link_requests
WHERE chat_id = $1 AND consumed_at >= $2;

-- name: GetTelegramAccountByChatID :one
SELECT user_id FROM telegram_accounts WHERE chat_id = $1;

-- name: CreateTelegramAccount :exec
INSERT INTO telegram_accounts (user_id, chat_id) VALUES ($1, $2);
```

- [ ] **Step 3: Widen the signup queries**

In `api/internal/adapter/postgres/queries/signup.sql`, add:

```sql
-- CreateTelegramSignup is CreateSignup's Telegram twin. The two are mutually
-- exclusive per row, enforced by signups_have_exactly_one_channel.
-- name: CreateTelegramSignup :exec
INSERT INTO signups (telegram_chat_id, token_hash, expires_at)
VALUES ($1, $2, $3);
```

and change `ConsumeSignup`'s and `GetSignupByTokenHash`'s `RETURNING`/`SELECT` lists to carry the chat id, so the verified channel reaches the user row from the row being claimed rather than from a caller:

```sql
-- name: GetSignupByTokenHash :one
SELECT id, email, telegram_chat_id, expires_at, consumed_at
FROM signups
WHERE token_hash = $1;

-- name: ConsumeSignup :one
UPDATE signups
SET consumed_at = now()
WHERE id = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING id, email, telegram_chat_id;
```

- [ ] **Step 4: Regenerate and apply**

```bash
export PATH=$PATH:/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin
make sqlc
make migrate
```

Expected: `sqlcgen` gains the new methods; `sqlcgen.ConsumeSignupRow` gains `TelegramChatID *int64`; `make migrate` reports 00011 applied. Compilation of `signup_repo.go` will now fail because `claimed.Email` is `*string` — that is fixed in Task 4, so do not chase it here.

- [ ] **Step 5: Write the schema test**

Create `api/internal/adapter/postgres/telegram_schema_test.go`. Follow the existing testcontainers pattern in this package (see any `*_repo_test.go` for how `testsupport.StartPostgres` is used).

```go
package postgres_test

import (
	"context"
	"testing"
)

// TestSignupsRefuseBothChannels and its sibling pin the fail-closed half of
// the schema: the constraint, not the Go code, is what refuses a row that
// names two channels or none.
func TestSignupsRefuseBothChannels(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t) // existing helper in this package

	_, err := pool.Exec(ctx,
		`INSERT INTO signups (email, telegram_chat_id, token_hash, expires_at)
		 VALUES ('a@b.co', 12345, '\x00', now() + interval '1 hour')`)
	if err == nil {
		t.Fatal("insert with both channels succeeded, want constraint violation")
	}
}

func TestSignupsRefuseNeitherChannel(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	_, err := pool.Exec(ctx,
		`INSERT INTO signups (token_hash, expires_at)
		 VALUES ('\x01', now() + interval '1 hour')`)
	if err == nil {
		t.Fatal("insert with no channel succeeded, want constraint violation")
	}
}

// TestConsumedLinkRequestsMustNameTheirChat pins the other half: a row cannot
// be marked consumed without recording which chat consumed it, because the
// per-chat rate limit counts exactly those rows.
func TestConsumedLinkRequestsMustNameTheirChat(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	_, err := pool.Exec(ctx,
		`INSERT INTO telegram_link_requests (nonce_hash, expires_at, consumed_at)
		 VALUES ('\x02', now() + interval '1 hour', now())`)
	if err == nil {
		t.Fatal("consumed row with no chat_id succeeded, want constraint violation")
	}
}
```

If `newTestPool` does not exist under that name in this package, use whatever the neighbouring `*_repo_test.go` files use to obtain a pool; do not invent a second helper.

- [ ] **Step 6: Run the tests and watch them pass**

```bash
export PATH=$PATH:/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
cd api && go test ./internal/adapter/postgres/ -run 'TestSignupsRefuse|TestConsumedLinkRequests' -v
```

Expected: three PASS. These are constraint tests, so they pass on first write — the mutation check for this task is Step 7.

- [ ] **Step 7: Prove the constraints are load-bearing**

Temporarily delete the `signups_have_exactly_one_channel` line from the migration, `make migrate-down && make migrate`, and re-run. Expected: `TestSignupsRefuseBothChannels` and `TestSignupsRefuseNeitherChannel` FAIL. Restore the line, re-migrate, confirm green again. This is the `proving-tests-can-fail` step for Task 1 — do not skip it.

- [ ] **Step 8: Commit**

```bash
git add api/migrations/00011_telegram.sql \
        api/internal/adapter/postgres/queries/telegram.sql \
        api/internal/adapter/postgres/queries/signup.sql \
        api/internal/adapter/postgres/sqlcgen/ \
        api/internal/adapter/postgres/telegram_schema_test.go
git commit -m "feat(db): tables and queries for Telegram sign-in"
```

---

## Task 2: Configuration

**Files:**
- Modify: `api/internal/config/config.go`
- Test: `api/internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `config.Config.TelegramBotToken string`, `config.Config.TelegramBotUsername string`, and `func (c Config) TelegramEnabled() bool`.

- [ ] **Step 1: Write the failing tests**

Append to `api/internal/config/config_test.go`. Follow the existing tests in that file for how the environment is set — they use `t.Setenv` and a helper that fills the required variables.

```go
func TestTelegramValuesMustBeBothOrNeither(t *testing.T) {
	setRequiredEnv(t) // existing helper: APP_ENV, DATABASE_URL, SMTP_*, APP_BASE_URL
	t.Setenv("TELEGRAM_BOT_TOKEN", "123:abc")
	// TELEGRAM_BOT_USERNAME deliberately unset.

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() with a token and no username succeeded, want an error")
	}
}

func TestTelegramDisabledWhenBothEmpty(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}
	if cfg.TelegramEnabled() {
		t.Fatal("TelegramEnabled() = true with both values empty, want false")
	}
}

func TestTelegramEnabledWhenBothSet(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("TELEGRAM_BOT_TOKEN", "123:abc")
	t.Setenv("TELEGRAM_BOT_USERNAME", "HearthBot")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}
	if !cfg.TelegramEnabled() {
		t.Fatal("TelegramEnabled() = false with both values set, want true")
	}
	if cfg.TelegramBotUsername != "HearthBot" {
		t.Fatalf("TelegramBotUsername = %q, want %q", cfg.TelegramBotUsername, "HearthBot")
	}
}
```

If `setRequiredEnv` has a different name in that file, use the existing one rather than adding another.

- [ ] **Step 2: Run them and watch them fail**

```bash
export PATH=$PATH:/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin
cd api && go test ./internal/config/ -run TestTelegram -v
```

Expected: compile failure — `cfg.TelegramEnabled undefined`.

- [ ] **Step 3: Implement**

In `api/internal/config/config.go`, add to the `Config` struct, beside the SMTP values:

```go
	// TelegramBotToken and TelegramBotUsername are optional and travel
	// together: both set turns Telegram sign-in on, both empty leaves it off.
	// One without the other is refused for the same reason SMTP_USERNAME and
	// SMTP_PASSWORD are -- a half-configured channel misbehaves silently, and
	// the symptom (links that are minted and never delivered) looks exactly
	// like nobody using the feature.
	//
	// The username is configured rather than read from Telegram's getMe at
	// startup: no cleverness, and no startup dependency on Telegram being
	// reachable.
	TelegramBotToken    string
	TelegramBotUsername string
```

Add to the `cfg := Config{...}` literal in `Load`:

```go
		TelegramBotToken:    os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramBotUsername: os.Getenv("TELEGRAM_BOT_USERNAME"),
```

Add the validation immediately after the existing SMTP both-or-neither check:

```go
	if (cfg.TelegramBotToken == "") != (cfg.TelegramBotUsername == "") {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN and TELEGRAM_BOT_USERNAME must both be set, or both left empty")
	}
```

And beside `IsDevelopment`:

```go
// TelegramEnabled reports whether Telegram sign-in is configured. When it is
// false the route answers 404 and the poller never starts, so an install that
// has not set up a bot behaves exactly as it did before this feature existed.
func (c Config) TelegramEnabled() bool { return c.TelegramBotToken != "" }
```

- [ ] **Step 4: Run them and watch them pass**

```bash
cd api && go test ./internal/config/ -run TestTelegram -v
```

Expected: three PASS.

- [ ] **Step 5: Mutation-check the both-or-neither rule**

Change the validation's `!=` to `==`, re-run, and confirm `TestTelegramValuesMustBeBothOrNeither` FAILS. Restore it and confirm green. Per `proving-tests-can-fail`.

- [ ] **Step 6: Document the variables**

Add to `.env.example`, commented out (development does not need a bot), and to `deploy/.env.example`, with a note that these are what make production sign-up work at all:

```
# TELEGRAM_BOT_TOKEN and TELEGRAM_BOT_USERNAME turn Telegram sign-in on. Both
# or neither -- config.Load refuses one without the other, and that check runs
# before every adminctl subcommand too, so a lone leftover value here breaks
# unlock-household and reset-password as well as mail.
#TELEGRAM_BOT_TOKEN=
#TELEGRAM_BOT_USERNAME=
```

- [ ] **Step 7: Commit**

```bash
git add api/internal/config/ .env.example deploy/.env.example
git commit -m "feat(config): TELEGRAM_BOT_TOKEN and TELEGRAM_BOT_USERNAME, both or neither"
```

---

## Task 3: The two Telegram repositories

**Files:**
- Create: `api/internal/adapter/postgres/telegram_link_repo.go`
- Create: `api/internal/adapter/postgres/telegram_account_repo.go`
- Modify: `api/internal/usecase/ports.go`
- Test: `api/internal/adapter/postgres/telegram_repo_test.go`

**Interfaces:**
- Consumes: Task 1's generated `sqlcgen` methods.
- Produces:
  - `usecase.TelegramLinkRepository` — `Create(ctx, nonceHash []byte, expiresAt time.Time) error`, `Consume(ctx, nonceHash []byte, chatID int64) error`, `CountLinksSince(ctx, chatID int64, since time.Time) (int, error)`
  - `usecase.TelegramAccountRepository` — `ByChatID(ctx, chatID int64) (string, error)`
  - `postgres.NewTelegramLinkRepo(db *DB) *TelegramLinkRepo`, `postgres.NewTelegramAccountRepo(db *DB) *TelegramAccountRepo`

- [ ] **Step 1: Declare the ports**

In `api/internal/usecase/ports.go`, beside `MagicLinkRepository`:

```go
// TelegramLinkRepository stores the pending deep-link nonces that carry a
// browser's sign-in request across to Telegram. Nonces are stored hashed,
// never raw, like every other token in this system.
type TelegramLinkRepository interface {
	Create(ctx context.Context, nonceHash []byte, expiresAt time.Time) error
	// Consume stamps the row consumed and records which chat redeemed it, in
	// one statement. The chat is unknown when the nonce is minted -- the
	// browser has not met Telegram yet -- so redemption is the only moment the
	// two can be joined, and CountLinksSince depends on it happening here.
	// Returns domain.ErrNotFound if the nonce is unknown, expired or already
	// consumed; those three are deliberately indistinguishable to a caller.
	Consume(ctx context.Context, nonceHash []byte, chatID int64) error
	// CountLinksSince counts links this chat has redeemed since a point in
	// time. It lives here rather than on TelegramAccountRepository because the
	// per-chat limit must also bind chats that have no account yet: a stranger
	// repeating /start has no user row to count against.
	CountLinksSince(ctx context.Context, chatID int64, since time.Time) (int, error)
}

// TelegramAccountRepository resolves a Telegram chat to the Hearth user it is
// bound to. The binding itself is written inside SignupRepository.Provision's
// transaction, which is why there is no Create method here.
type TelegramAccountRepository interface {
	// ByChatID returns domain.ErrNotFound when the chat is bound to no user,
	// which is the ordinary "this person has no account yet" case, not an error
	// condition.
	ByChatID(ctx context.Context, chatID int64) (userID string, err error)
}
```

- [ ] **Step 2: Write the failing repository tests**

Create `api/internal/adapter/postgres/telegram_repo_test.go`:

```go
package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestTelegramLinkConsumeIsSingleUse(t *testing.T) {
	ctx := context.Background()
	repo := newTelegramLinkRepo(t) // add this helper beside the package's existing ones
	hash := []byte("nonce-hash-one")

	if err := repo.Create(ctx, hash, time.Now().Add(10*time.Minute)); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	if err := repo.Consume(ctx, hash, 4242); err != nil {
		t.Fatalf("first Consume() = %v, want nil", err)
	}
	if err := repo.Consume(ctx, hash, 4242); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second Consume() = %v, want domain.ErrNotFound", err)
	}
}

func TestTelegramLinkConsumeRefusesAnExpiredNonce(t *testing.T) {
	ctx := context.Background()
	repo := newTelegramLinkRepo(t)
	hash := []byte("nonce-hash-expired")

	if err := repo.Create(ctx, hash, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	if err := repo.Consume(ctx, hash, 4242); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Consume() on an expired nonce = %v, want domain.ErrNotFound", err)
	}
}

// The count is what the per-chat rate limit reads, and it must see only rows
// this chat actually redeemed -- not every nonce ever minted.
func TestTelegramLinkCountsOnlyThisChatsRedemptions(t *testing.T) {
	ctx := context.Background()
	repo := newTelegramLinkRepo(t)
	since := time.Now().Add(-time.Hour)

	for i, hash := range [][]byte{[]byte("c1-a"), []byte("c1-b")} {
		_ = i
		if err := repo.Create(ctx, hash, time.Now().Add(10*time.Minute)); err != nil {
			t.Fatalf("Create() = %v", err)
		}
		if err := repo.Consume(ctx, hash, 1111); err != nil {
			t.Fatalf("Consume() = %v", err)
		}
	}
	if err := repo.Create(ctx, []byte("c2-a"), time.Now().Add(10*time.Minute)); err != nil {
		t.Fatalf("Create() = %v", err)
	}
	if err := repo.Consume(ctx, []byte("c2-a"), 2222); err != nil {
		t.Fatalf("Consume() = %v", err)
	}

	got, err := repo.CountLinksSince(ctx, 1111, since)
	if err != nil {
		t.Fatalf("CountLinksSince() = %v, want nil", err)
	}
	if got != 2 {
		t.Fatalf("CountLinksSince(chat 1111) = %d, want 2", got)
	}
}

func TestTelegramAccountByChatIDIsNotFoundWhenUnbound(t *testing.T) {
	ctx := context.Background()
	repo := newTelegramAccountRepo(t)

	if _, err := repo.ByChatID(ctx, 999999); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ByChatID() on an unbound chat = %v, want domain.ErrNotFound", err)
	}
}
```

- [ ] **Step 3: Run them and watch them fail**

```bash
export PATH=$PATH:/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
cd api && go test ./internal/adapter/postgres/ -run TestTelegram -v
```

Expected: compile failure — `newTelegramLinkRepo` and the repository types do not exist.

- [ ] **Step 4: Implement the link repository**

Create `api/internal/adapter/postgres/telegram_link_repo.go`, following `magiclink_repo.go` exactly:

```go
package postgres

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
)

type TelegramLinkRepo struct{ q *sqlcgen.Queries }

func NewTelegramLinkRepo(db *DB) *TelegramLinkRepo {
	return &TelegramLinkRepo{q: sqlcgen.New(db.Pool())}
}

func (r *TelegramLinkRepo) Create(ctx context.Context, nonceHash []byte, expiresAt time.Time) error {
	return translate(r.q.CreateTelegramLinkRequest(ctx, sqlcgen.CreateTelegramLinkRequestParams{
		NonceHash: nonceHash,
		ExpiresAt: timestamptz(expiresAt),
	}), "create telegram link request")
}

// Consume goes through translate, so an unknown, expired or already-consumed
// nonce all surface as domain.ErrNotFound. Keeping the three indistinguishable
// is deliberate: the bot answers all of them with one message, so none of them
// can be told apart by probing.
func (r *TelegramLinkRepo) Consume(ctx context.Context, nonceHash []byte, chatID int64) error {
	_, err := r.q.ConsumeTelegramLinkRequest(ctx, sqlcgen.ConsumeTelegramLinkRequestParams{
		NonceHash: nonceHash,
		ChatID:    &chatID,
	})
	return translate(err, "consume telegram link request")
}

func (r *TelegramLinkRepo) CountLinksSince(ctx context.Context, chatID int64, since time.Time) (int, error) {
	count, err := r.q.CountTelegramLinksSince(ctx, sqlcgen.CountTelegramLinksSinceParams{
		ChatID:     &chatID,
		ConsumedAt: timestamptz(since),
	})
	if err != nil {
		return 0, translate(err, "count telegram links")
	}
	return int(count), nil
}
```

The exact generated parameter names and pointer-ness depend on `sqlc`'s output for these columns (`emit_pointers_for_null_types: true` makes nullable columns pointers). Read `sqlcgen/telegram.sql.go` after Task 1 and match it; do not guess.

- [ ] **Step 5: Implement the account repository**

Create `api/internal/adapter/postgres/telegram_account_repo.go`:

```go
package postgres

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
)

type TelegramAccountRepo struct{ q *sqlcgen.Queries }

func NewTelegramAccountRepo(db *DB) *TelegramAccountRepo {
	return &TelegramAccountRepo{q: sqlcgen.New(db.Pool())}
}

func (r *TelegramAccountRepo) ByChatID(ctx context.Context, chatID int64) (string, error) {
	id, err := r.q.GetTelegramAccountByChatID(ctx, chatID)
	if err != nil {
		return "", translate(err, "get telegram account by chat id")
	}
	return uuidToString(id), nil
}
```

- [ ] **Step 6: Add the test helpers, run, watch them pass**

Add `newTelegramLinkRepo` and `newTelegramAccountRepo` beside the package's existing per-repository test helpers, constructing from the same shared test database those helpers use.

```bash
cd api && go test ./internal/adapter/postgres/ -run TestTelegram -v
```

Expected: four PASS.

- [ ] **Step 7: Mutation-check the single-use guard**

In `queries/telegram.sql`, remove `AND consumed_at IS NULL` from `ConsumeTelegramLinkRequest`, run `make sqlc`, re-run the tests. Expected: `TestTelegramLinkConsumeIsSingleUse` FAILS. Restore, regenerate, confirm green.

- [ ] **Step 8: Commit**

```bash
git add api/internal/usecase/ports.go api/internal/adapter/postgres/
git commit -m "feat(db): Telegram link and account repositories"
```

---

## Task 4: Signups gain a Telegram channel

**Files:**
- Modify: `api/internal/usecase/ports.go`
- Modify: `api/internal/usecase/signup.go`
- Modify: `api/internal/adapter/postgres/signup_repo.go`
- Test: `api/internal/adapter/postgres/signup_repo_test.go`, `api/internal/usecase/signup_test.go`

**Interfaces:**
- Consumes: Task 1's widened `ConsumeSignup`, Task 3's ports.
- Produces:
  - `usecase.SignupRepository.CreateForTelegram(ctx, chatID int64, tokenHash []byte, expiresAt time.Time) error`
  - `usecase.SignupDetails.TelegramChatID *int64`
  - `usecase.SignupPreview.Channel string` — `"email"` or `"telegram"`

- [ ] **Step 1: Write the failing tests**

In `api/internal/adapter/postgres/signup_repo_test.go`:

```go
// Provision binds the chat inside its own transaction, from the row it is
// claiming -- not from anything a caller passed in. A Telegram sign-up that
// provisioned a household but left the chat unbound would be an account its
// owner can never sign into again.
func TestProvisionBindsTheChatFromTheClaimedRow(t *testing.T) {
	ctx := context.Background()
	repo := newSignupRepo(t)
	pool := newTestPool(t)

	const chatID int64 = 777001
	hash := []byte("telegram-signup-token")
	if err := repo.CreateForTelegram(ctx, chatID, hash, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("CreateForTelegram() = %v, want nil", err)
	}
	details, err := repo.ByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("ByTokenHash() = %v, want nil", err)
	}
	if details.TelegramChatID == nil || *details.TelegramChatID != chatID {
		t.Fatalf("TelegramChatID = %v, want %d", details.TelegramChatID, chatID)
	}

	provisioned, err := repo.Provision(ctx, details.ID, "argon2-hash", testBlueprint())
	if err != nil {
		t.Fatalf("Provision() = %v, want nil", err)
	}

	var boundUser string
	err = pool.QueryRow(ctx,
		`SELECT user_id::text FROM telegram_accounts WHERE chat_id = $1`, chatID).Scan(&boundUser)
	if err != nil {
		t.Fatalf("no telegram_accounts row after Provision: %v", err)
	}
	if boundUser != provisioned.UserID {
		t.Fatalf("bound user = %q, want %q", boundUser, provisioned.UserID)
	}
}

// An email sign-up must not gain a telegram_accounts row.
func TestProvisionBindsNoChatForAnEmailSignup(t *testing.T) {
	ctx := context.Background()
	repo := newSignupRepo(t)
	pool := newTestPool(t)

	hash := []byte("email-signup-token")
	if err := repo.Create(ctx, "someone@example.com", hash, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	details, err := repo.ByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("ByTokenHash() = %v, want nil", err)
	}
	provisioned, err := repo.Provision(ctx, details.ID, "argon2-hash", testBlueprint())
	if err != nil {
		t.Fatalf("Provision() = %v, want nil", err)
	}

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM telegram_accounts WHERE user_id = $1`, provisioned.UserID).Scan(&n); err != nil {
		t.Fatalf("count = %v", err)
	}
	if n != 0 {
		t.Fatalf("telegram_accounts rows for an email sign-up = %d, want 0", n)
	}
}

// The orphan test: if the binding insert fails, nothing survives. A pre-existing
// row on the same chat_id makes the unique constraint fire inside the
// transaction.
func TestProvisionRollsBackWhenTheChatIsAlreadyBound(t *testing.T) {
	ctx := context.Background()
	repo := newSignupRepo(t)
	pool := newTestPool(t)

	const chatID int64 = 777002
	// Bind the chat to an unrelated user first.
	var otherUser string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (display_name, avatar_initial) VALUES ('Other', 'O') RETURNING id::text`).
		Scan(&otherUser); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO telegram_accounts (user_id, chat_id) VALUES ($1, $2)`, otherUser, chatID); err != nil {
		t.Fatalf("seed binding: %v", err)
	}

	hash := []byte("doomed-signup-token")
	if err := repo.CreateForTelegram(ctx, chatID, hash, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("CreateForTelegram() = %v", err)
	}
	details, err := repo.ByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("ByTokenHash() = %v", err)
	}

	if _, err := repo.Provision(ctx, details.ID, "argon2-hash", testBlueprint()); err == nil {
		t.Fatal("Provision() succeeded with an already-bound chat, want an error")
	}

	// Nothing may survive: not the household, not the user, and the signup
	// must still be claimable.
	var consumed *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT consumed_at FROM signups WHERE token_hash = $1`, hash).Scan(&consumed); err != nil {
		t.Fatalf("read signup: %v", err)
	}
	if consumed != nil {
		t.Fatal("signup was consumed despite the rolled-back transaction")
	}
}
```

`testBlueprint()` and `newSignupRepo` already exist in this package's test files if the neighbouring `Provision` tests use them; reuse those names. If they do not exist, add `testBlueprint()` returning a valid `usecase.HouseholdBlueprint` via `usecase.NewSignupBlueprint("Test", "Owner", "SGD")`.

In `api/internal/usecase/signup_test.go`:

```go
func TestPreviewReportsTheTelegramChannel(t *testing.T) {
	chatID := int64(4242)
	svc, repo := newSignupServiceWithDoubles(t) // existing helper pattern
	repo.putTelegramSignup("token", chatID, time.Now().Add(time.Hour))

	preview, err := svc.Preview(context.Background(), "token")
	if err != nil {
		t.Fatalf("Preview() = %v, want nil", err)
	}
	if preview.Channel != "telegram" {
		t.Fatalf("Channel = %q, want %q", preview.Channel, "telegram")
	}
	if preview.Email != "" {
		t.Fatalf("Email = %q, want empty for a Telegram sign-up", preview.Email)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestProvision -v
cd api && go test ./internal/usecase/ -run TestPreviewReportsTheTelegramChannel -v
```

Expected: compile failures — `CreateForTelegram`, `TelegramChatID` and `Channel` do not exist.

- [ ] **Step 3: Widen the ports**

In `api/internal/usecase/ports.go`:

```go
// SignupDetails is a pending sign-up, read back by token. Exactly one of Email
// and TelegramChatID is set -- the signups_have_exactly_one_channel constraint
// makes that a database guarantee, not a convention.
type SignupDetails struct {
	ID             string
	Email          string
	TelegramChatID *int64
	ExpiresAt      time.Time
	ConsumedAt     *time.Time
}
```

and inside `SignupRepository`:

```go
	// CreateForTelegram writes a signup row whose channel is a Telegram chat
	// rather than an email address. It and Create are mutually exclusive per
	// row, enforced by signups_have_exactly_one_channel.
	CreateForTelegram(ctx context.Context, chatID int64, tokenHash []byte, expiresAt time.Time) error
```

- [ ] **Step 4: Report the channel from Preview**

In `api/internal/usecase/signup.go`:

```go
// SignupPreview is what the create-household screen needs before anything is
// created. Channel tells the screen which identity the token proved, so it can
// show a read-only address for an email sign-up and say "Telegram" for a
// Telegram one -- rather than rendering an empty address box, which would look
// like a field the person forgot to fill in.
type SignupPreview struct {
	Email   string
	Channel string
}

// signupChannel refuses a row that names neither channel rather than guessing.
// The database constraint should make that unreachable; this is the second
// gate, for rows written by anything that bypasses it.
func signupChannel(d SignupDetails) (string, error) {
	switch {
	case d.Email != "":
		return "email", nil
	case d.TelegramChatID != nil:
		return "telegram", nil
	default:
		return "", fmt.Errorf("signup %s names no channel", d.ID)
	}
}
```

and in `Preview`, replace the return with:

```go
	channel, err := signupChannel(details)
	if err != nil {
		return SignupPreview{}, err
	}
	return SignupPreview{Email: details.Email, Channel: channel}, nil
```

`Complete` is unchanged. It hands `Provision` a signup id; the row decides its own channel.

- [ ] **Step 5: Implement the repository side**

In `api/internal/adapter/postgres/signup_repo.go`, add:

```go
func (r *SignupRepo) CreateForTelegram(ctx context.Context, chatID int64, tokenHash []byte, expiresAt time.Time) error {
	return translate(r.q.CreateTelegramSignup(ctx, sqlcgen.CreateTelegramSignupParams{
		TelegramChatID: &chatID,
		TokenHash:      tokenHash,
		ExpiresAt:      timestamptz(expiresAt),
	}), "create telegram signup")
}
```

`ByTokenHash` now carries the chat id through into `SignupDetails.TelegramChatID`, and `Email` becomes `""` when the column is NULL (the same `"" <-> NULL` convention `StoredUser` already uses).

In `Provision`, `claimed.Email` is now a `*string`, so the `CreateUser` call becomes:

```go
	userRow, err := q.CreateUser(ctx, sqlcgen.CreateUserParams{
		Email:         claimed.Email, // already *string; NULL for a Telegram sign-up
		PasswordHash:  nullableText(passwordHash),
		DisplayName:   b.OwnerDisplayName,
		AvatarInitial: initialOf(b.OwnerDisplayName),
	})
```

and immediately after the `CreateMembership` call, still inside the transaction and before `tx.Commit`:

```go
	// Bind the chat from the row that was just claimed, never from a caller.
	// This is the same rule ConsumeSignup's comment states for the email: the
	// verified value reaches the user row from the row being claimed, so no
	// caller can substitute a different one.
	//
	// It is inside this transaction, not after it, because a household that
	// exists with its chat unbound is an account its owner can never sign into
	// again -- the token is spent and there is no other way in.
	if claimed.TelegramChatID != nil {
		if err := q.CreateTelegramAccount(ctx, sqlcgen.CreateTelegramAccountParams{
			UserID: userRow.ID,
			ChatID: *claimed.TelegramChatID,
		}); err != nil {
			return usecase.ProvisionedHousehold{}, translate(err, "bind telegram account for signup")
		}
	}
```

- [ ] **Step 6: Run everything and watch it pass**

```bash
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
cd api && go test ./internal/adapter/postgres/ ./internal/usecase/ -v
```

Expected: all PASS, including every pre-existing signup test. If an existing test broke, the `"" <-> NULL` conversion in `ByTokenHash` is the first place to look.

- [ ] **Step 7: Mutation-check the transaction**

Move the `CreateTelegramAccount` block to **after** `tx.Commit(ctx)`. Re-run. Expected: `TestProvisionRollsBackWhenTheChatIsAlreadyBound` FAILS, because the household now survives a failed binding. Move it back and confirm green. This is the single most important mutation check in the plan.

- [ ] **Step 8: Commit**

```bash
git add api/internal/usecase/ api/internal/adapter/postgres/
git commit -m "feat(signup): a signup may name a Telegram chat instead of an email"
```

---

## Task 5: The Telegram client and update parsing

**Files:**
- Create: `api/internal/adapter/telegram/client.go`
- Create: `api/internal/adapter/telegram/update.go`
- Test: `api/internal/adapter/telegram/client_test.go`, `api/internal/adapter/telegram/update_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `telegram.NewClient(token string) *Client`
  - `(*Client).SendMessage(ctx context.Context, chatID int64, text string) error` — satisfies `usecase.TelegramSender`
  - `(*Client).GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error)`
  - `telegram.Update` struct, `telegram.StartCommand{ChatID int64; Payload string}`, `telegram.ParseStart(u Update) (StartCommand, bool)`

- [ ] **Step 1: Write the failing parse tests**

Create `api/internal/adapter/telegram/update_test.go`:

```go
package telegram

import "testing"

func TestParseStart(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantOK  bool
		payload string
	}{
		{name: "start with a payload", text: "/start abc123", wantOK: true, payload: "abc123"},
		{name: "start with no payload", text: "/start", wantOK: true, payload: ""},
		{name: "start with trailing space", text: "/start  abc123  ", wantOK: true, payload: "abc123"},
		{name: "another command", text: "/help", wantOK: false},
		{name: "ordinary chatter", text: "hello", wantOK: false},
		{name: "empty", text: "", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := Update{UpdateID: 1}
			u.Message = &Message{Text: tc.text}
			u.Message.Chat.ID = 55

			got, ok := ParseStart(u)
			if ok != tc.wantOK {
				t.Fatalf("ParseStart(%q) ok = %v, want %v", tc.text, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got.Payload != tc.payload {
				t.Fatalf("payload = %q, want %q", got.Payload, tc.payload)
			}
			if got.ChatID != 55 {
				t.Fatalf("chatID = %d, want 55", got.ChatID)
			}
		})
	}
}

// An update with no message at all -- an edited message, a callback query, a
// channel post -- must be ignored, not panic.
func TestParseStartIgnoresUpdatesWithNoMessage(t *testing.T) {
	if _, ok := ParseStart(Update{UpdateID: 9}); ok {
		t.Fatal("ParseStart on a message-less update returned ok, want false")
	}
}
```

- [ ] **Step 2: Run and watch it fail**

```bash
export PATH=$PATH:/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin
cd api && go test ./internal/adapter/telegram/ -v
```

Expected: the package does not exist.

- [ ] **Step 3: Implement update parsing**

Create `api/internal/adapter/telegram/update.go`:

```go
// Package telegram is the adapter that owns Hearth's Telegram dependency. It
// talks to Telegram's Bot API over outbound HTTPS only -- there is no webhook
// and no inbound route, so nothing in this package faces the internet.
package telegram

import "strings"

// Update is the subset of Telegram's Update object this product reads. Every
// other field Telegram sends is deliberately ignored: a bot that parses only
// what it acts on cannot be surprised by a payload shape it did not expect.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

type Message struct {
	Text string `json:"text"`
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
}

// StartCommand is a /start carrying the deep-link payload the browser minted.
type StartCommand struct {
	ChatID  int64
	Payload string
}

// ParseStart returns false for everything that is not a /start, including
// updates with no message at all. The switch has a default that ignores rather
// than one that guesses: this value arrives from a third party, so the rule is
// the same as for a database column -- refuse what you did not construct.
func ParseStart(u Update) (StartCommand, bool) {
	if u.Message == nil {
		return StartCommand{}, false
	}
	command, payload, _ := strings.Cut(strings.TrimSpace(u.Message.Text), " ")
	switch command {
	case "/start":
		return StartCommand{ChatID: u.Message.Chat.ID, Payload: strings.TrimSpace(payload)}, true
	default:
		return StartCommand{}, false
	}
}
```

- [ ] **Step 4: Run and watch it pass**

```bash
cd api && go test ./internal/adapter/telegram/ -run TestParseStart -v
```

Expected: PASS, all subtests.

- [ ] **Step 5: Write the failing client tests**

Create `api/internal/adapter/telegram/client_test.go`:

```go
package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSendMessagePostsTheChatAndText(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()

	c := newClientWithBase("secret-token", srv.URL)
	if err := c.SendMessage(context.Background(), 4242, "hello"); err != nil {
		t.Fatalf("SendMessage() = %v, want nil", err)
	}
	if !strings.HasSuffix(gotPath, "/sendMessage") {
		t.Fatalf("path = %q, want it to end in /sendMessage", gotPath)
	}
	if gotBody["text"] != "hello" {
		t.Fatalf("text = %v, want %q", gotBody["text"], "hello")
	}
}

// Telegram answers 200 with ok:false for application-level failures, so a
// status check alone would treat a refused send as a success.
func TestSendMessageFailsWhenTelegramAnswersNotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	defer srv.Close()

	c := newClientWithBase("secret-token", srv.URL)
	err := c.SendMessage(context.Background(), 4242, "hello")
	if err == nil {
		t.Fatal("SendMessage() = nil, want an error when ok is false")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatal("the bot token leaked into an error message")
	}
}

func TestGetUpdatesDecodesResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":[
			{"update_id":7,"message":{"text":"/start abc","chat":{"id":99}}}]}`))
	}))
	defer srv.Close()

	c := newClientWithBase("secret-token", srv.URL)
	updates, err := c.GetUpdates(context.Background(), 0, time.Second)
	if err != nil {
		t.Fatalf("GetUpdates() = %v, want nil", err)
	}
	if len(updates) != 1 || updates[0].UpdateID != 7 {
		t.Fatalf("updates = %+v, want one update with id 7", updates)
	}
}
```

- [ ] **Step 6: Run and watch it fail**

Expected: `newClientWithBase` undefined.

- [ ] **Step 7: Implement the client**

Create `api/internal/adapter/telegram/client.go`:

```go
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client talks to Telegram's Bot API. It holds the bot token and must never
// put it in an error, a log line, or anything else that leaves this file --
// the token is a full credential for the bot, and Telegram's own API URLs
// embed it in the path, which is exactly why errors here are built from the
// method name rather than from the request URL.
type Client struct {
	token string
	base  string
	http  *http.Client
}

// NewClient uses Telegram's public API host. The send timeout is generous
// because nothing waits on a send: the poller is not on any request path.
func NewClient(token string) *Client {
	return newClientWithBase(token, "https://api.telegram.org")
}

// newClientWithBase exists so tests can point the client at an httptest server
// without reaching Telegram. It is unexported: production has one host.
func newClientWithBase(token, base string) *Client {
	return &Client{token: token, base: base, http: &http.Client{Timeout: 70 * time.Second}}
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

// call posts a JSON body to one Bot API method and refuses anything that is
// not ok. Telegram answers 200 with ok:false for application-level failures,
// so a status-code check alone would read a refused send as a success.
func (c *Client) call(ctx context.Context, method string, body any, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", method, err)
	}
	url := fmt.Sprintf("%s/bot%s/%s", c.base, c.token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("build %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// Deliberately not %w on err alone: url.Error carries the request URL,
		// and the URL contains the bot token.
		return fmt.Errorf("telegram %s: request failed", method)
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	if !parsed.OK {
		return fmt.Errorf("telegram %s refused: %s", method, parsed.Description)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(parsed.Result, out); err != nil {
		return fmt.Errorf("decode %s result: %w", method, err)
	}
	return nil
}

// SendMessage satisfies usecase.TelegramSender. Messages are plain text by
// design -- there is no template system to keep in sync with the product copy,
// exactly as the mailer has none.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	return c.call(ctx, "sendMessage", map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"disable_web_page_preview": true,
	}, nil)
}

// GetUpdates long-polls. timeout is Telegram's own server-side wait, which is
// why the HTTP client's timeout above is comfortably longer than any value a
// caller will pass.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	var updates []Update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset":  offset,
		"timeout": int(timeout.Seconds()),
	}, &updates)
	if err != nil {
		return nil, err
	}
	return updates, nil
}
```

- [ ] **Step 8: Run and watch it pass**

```bash
cd api && go test ./internal/adapter/telegram/ -v
```

Expected: all PASS.

- [ ] **Step 9: Mutation-check the ok:false guard**

Delete the `if !parsed.OK` block. Re-run. Expected: `TestSendMessageFailsWhenTelegramAnswersNotOK` FAILS. Restore, confirm green.

- [ ] **Step 10: Commit**

```bash
git add api/internal/adapter/telegram/
git commit -m "feat(telegram): bot API client and /start parsing"
```

---

## Task 6: The poller

**Files:**
- Create: `api/internal/adapter/telegram/poller.go`
- Test: `api/internal/adapter/telegram/poller_test.go`

**Interfaces:**
- Consumes: Task 5's `Client`, `Update`, `ParseStart`.
- Produces: `telegram.StartHandler` interface (`HandleStart(ctx context.Context, chatID int64, payload string) error`), `telegram.NewPoller(c *Client, h StartHandler) *Poller`, `(*Poller).Run(ctx context.Context)`.

- [ ] **Step 1: Write the failing tests**

Create `api/internal/adapter/telegram/poller_test.go`:

```go
package telegram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type handlerSpy struct {
	mu    sync.Mutex
	calls []StartCommand
	err   error
	panic bool
}

func (h *handlerSpy) HandleStart(_ context.Context, chatID int64, payload string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, StartCommand{ChatID: chatID, Payload: payload})
	if h.panic {
		panic("handler exploded")
	}
	return h.err
}

func (h *handlerSpy) seen() []StartCommand {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]StartCommand(nil), h.calls...)
}

func TestPollerDispatchesStartCommands(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":[
			{"update_id":11,"message":{"text":"/start nonce-a","chat":{"id":501}}},
			{"update_id":12,"message":{"text":"chatter","chat":{"id":501}}}]}`))
	}))
	defer srv.Close()

	spy := &handlerSpy{}
	p := NewPoller(newClientWithBase("t", srv.URL), spy)
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx)

	waitFor(t, func() bool { return len(spy.seen()) == 1 })
	cancel()

	got := spy.seen()
	if got[0].Payload != "nonce-a" || got[0].ChatID != 501 {
		t.Fatalf("dispatched %+v, want chat 501 payload nonce-a", got[0])
	}
}

// The offset must advance past every update returned, including the ones that
// were ignored -- otherwise Telegram redelivers the ignored update forever and
// the loop never makes progress.
func TestPollerAdvancesTheOffsetPastIgnoredUpdates(t *testing.T) {
	var mu sync.Mutex
	var offsets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Offset int64 `json:"offset"`
		}
		_ = decodeJSON(r, &body)
		mu.Lock()
		offsets = append(offsets, itoa(body.Offset))
		first := len(offsets) == 1
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if first {
			_, _ = w.Write([]byte(`{"ok":true,"result":[
				{"update_id":30,"message":{"text":"/help","chat":{"id":9}}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer srv.Close()

	p := NewPoller(newClientWithBase("t", srv.URL), &handlerSpy{})
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx)
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(offsets) >= 2 })
	cancel()

	mu.Lock()
	defer mu.Unlock()
	if offsets[1] != "31" {
		t.Fatalf("second offset = %s, want 31", offsets[1])
	}
}

// The poller is a bare goroutine; chi's recoverer does not cover it. A panic
// in the handler must not take the process down.
func TestPollerSurvivesAPanickingHandler(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":[
			{"update_id":40,"message":{"text":"/start boom","chat":{"id":7}}}]}`))
	}))
	defer srv.Close()

	spy := &handlerSpy{panic: true}
	p := NewPoller(newClientWithBase("t", srv.URL), spy)
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx) // must not crash the test binary
	waitFor(t, func() bool { return len(spy.seen()) >= 1 })
	cancel()
}

func TestPollerBacksOffAndKeepsGoingAfterAnError(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			_, _ = w.Write([]byte(`{"ok":false,"description":"nope"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer srv.Close()

	p := NewPoller(newClientWithBase("t", srv.URL), &handlerSpy{})
	p.baseBackoff = time.Millisecond // keep the test fast
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx)
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return calls >= 2 })
	cancel()
}

func TestPollerStopsWhenTheContextIsCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer srv.Close()

	p := NewPoller(newClientWithBase("t", srv.URL), &handlerSpy{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of cancellation")
	}
}
```

Add the two small test helpers `waitFor(t, cond)` (poll every 5ms up to 2s, `t.Fatal` on timeout), `decodeJSON(r *http.Request, v any) error` and `itoa(int64) string` at the bottom of this file. Write them out in full; do not leave them implied.

- [ ] **Step 2: Run and watch them fail**

Expected: `NewPoller` undefined.

- [ ] **Step 3: Implement the poller**

Create `api/internal/adapter/telegram/poller.go`:

```go
package telegram

import (
	"context"
	"log/slog"
	"time"
)

// StartHandler is what the poller hands a parsed /start to. It is declared
// here, in the adapter, rather than imported from usecase, so this package
// depends on a shape rather than on a concrete service.
type StartHandler interface {
	HandleStart(ctx context.Context, chatID int64, payload string) error
}

const (
	// pollTimeout is Telegram's server-side long-poll wait. Long, because a
	// short one is just a busy loop against someone else's API.
	pollTimeout = 50 * time.Second
	// maxBackoff caps the retry delay after an error. Telegram being down for
	// an hour must not become an hour-long sleep that outlives the outage.
	maxBackoff = 60 * time.Second
)

// Poller long-polls Telegram for updates and dispatches /start commands.
//
// Exactly one process may run this. Telegram hands each update to a single
// getUpdates caller, so a second replica would silently steal updates and the
// symptom would be "sign-in works about half the time". True on one box today;
// this comment is here because the constraint is invisible until it bites.
//
// The offset is held in memory. After a restart Telegram redelivers updates it
// was never acknowledged for, so a /start can be processed twice. That is safe
// because the nonce was already consumed: the second pass takes the
// already-consumed branch and the bot says the link expired. Recorded rather
// than left to luck.
type Poller struct {
	client      *Client
	handler     StartHandler
	offset      int64
	baseBackoff time.Duration
}

func NewPoller(c *Client, h StartHandler) *Poller {
	return &Poller{client: c, handler: h, baseBackoff: time.Second}
}

// Run blocks until ctx is cancelled. It never returns on error: the API must
// keep serving HTTP while Telegram is unreachable, so a failure backs off and
// tries again rather than ending the loop.
func (p *Poller) Run(ctx context.Context) {
	backoff := p.baseBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		updates, err := p.client.GetUpdates(ctx, p.offset, pollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("telegram getUpdates failed", "error", err, "retry_in", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = p.baseBackoff

		for _, u := range updates {
			// Advance past every update, including ignored ones. Leaving the
			// offset behind an update we chose not to act on makes Telegram
			// redeliver it forever and the loop never progresses.
			if u.UpdateID >= p.offset {
				p.offset = u.UpdateID + 1
			}
			start, ok := ParseStart(u)
			if !ok {
				continue
			}
			p.dispatch(ctx, start)
		}
	}
}

// dispatch recovers, because this runs on a bare goroutine that chi's
// middleware.Recoverer does not cover: an unrecovered panic here would take
// down the whole process and every unrelated in-flight request, not just this
// one update. Same reasoning as sendMagicLinkAsync's recover.
func (p *Poller) dispatch(ctx context.Context, start StartCommand) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("telegram start handler panicked", "panic", r)
		}
	}()
	if err := p.handler.HandleStart(ctx, start.ChatID, start.Payload); err != nil {
		slog.Error("telegram start handler failed", "error", err)
	}
}
```

- [ ] **Step 4: Run and watch them pass**

```bash
cd api && go test ./internal/adapter/telegram/ -v -race
```

Expected: all PASS under `-race`.

- [ ] **Step 5: Mutation-check the offset advance**

Move the offset assignment to inside the `if ok` branch, so ignored updates no longer advance it. Re-run. Expected: `TestPollerAdvancesTheOffsetPastIgnoredUpdates` FAILS. Restore, confirm green.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/telegram/
git commit -m "feat(telegram): getUpdates poller with backoff and panic recovery"
```

---

## Task 7: TelegramAuthService

**Files:**
- Create: `api/internal/usecase/telegram_auth.go`
- Test: `api/internal/usecase/telegram_auth_test.go`
- Modify: `api/internal/usecase/testdouble_test.go`

**Interfaces:**
- Consumes: Task 3's `TelegramLinkRepository` and `TelegramAccountRepository`; the existing `MagicLinkRepository`, `SignupRepository`, `TokenGenerator`, `Clock`; Task 4's `SignupRepository.CreateForTelegram`.
- Produces:
  - `usecase.TelegramSender` port — `SendMessage(ctx context.Context, chatID int64, text string) error`
  - `usecase.TelegramAuthDeps{Links, Accounts, MagicLinks, Signups, Sender, Tokens, Clock, BaseURL, BotUsername}`
  - `usecase.NewTelegramAuthService(TelegramAuthDeps) *TelegramAuthService`
  - `(*TelegramAuthService).StartLink(ctx) (TelegramStartLink, error)` where `TelegramStartLink{URL string; ExpiresAt time.Time}`
  - `(*TelegramAuthService).HandleStart(ctx, chatID int64, payload string) error` — satisfies `telegram.StartHandler`

- [ ] **Step 1: Declare the sender port**

In `api/internal/usecase/ports.go`, beside `Mailer`:

```go
// TelegramSender delivers a plain-text message to one Telegram chat. Same
// shape and justification as Mailer: the usecase layer must not hold an HTTP
// client, and the service must be testable against a double.
type TelegramSender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}
```

- [ ] **Step 2: Write the failing tests**

Create `api/internal/usecase/telegram_auth_test.go`. Follow the in-memory double style already used in `testdouble_test.go`; add `telegramLinkRepoDouble`, `telegramAccountRepoDouble` and `telegramSenderDouble` there rather than in this file.

```go
package usecase_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestStartLinkMintsADeepLinkAndStoresTheNonceHashed(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)

	link, err := svc.StartLink(context.Background())
	if err != nil {
		t.Fatalf("StartLink() = %v, want nil", err)
	}
	if !strings.HasPrefix(link.URL, "https://t.me/HearthBot?start=") {
		t.Fatalf("URL = %q, want a t.me deep link for HearthBot", link.URL)
	}
	raw := strings.TrimPrefix(link.URL, "https://t.me/HearthBot?start=")
	if doubles.links.hasRaw(raw) {
		t.Fatal("the raw nonce was stored; it must be stored hashed")
	}
	if !doubles.links.hasHashOf(raw) {
		t.Fatal("no row was stored for the minted nonce")
	}
}

func TestHandleStartSendsASignInLinkToAKnownChat(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	doubles.accounts.bind(501, "user-1")
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 501, raw); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	sent := doubles.sender.lastTo(501)
	if !strings.Contains(sent, "/sign-in/magic?token=") {
		t.Fatalf("message = %q, want it to carry a magic-link URL", sent)
	}
	if doubles.magicLinks.countFor("user-1") != 1 {
		t.Fatalf("magic links minted = %d, want 1", doubles.magicLinks.countFor("user-1"))
	}
}

func TestHandleStartSendsASignUpLinkToAnUnknownChat(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 777, raw); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	sent := doubles.sender.lastTo(777)
	if !strings.Contains(sent, "/sign-up/") {
		t.Fatalf("message = %q, want it to carry a sign-up URL", sent)
	}
	if doubles.signups.telegramCount(777) != 1 {
		t.Fatalf("telegram signups created = %d, want 1", doubles.signups.telegramCount(777))
	}
}

// An unknown nonce, an expired one and an already-consumed one must all answer
// identically, so none of them can be told apart by probing.
func TestHandleStartAnswersIdenticallyForEveryDeadNonce(t *testing.T) {
	unknown := func(d *telegramDoubles) string { return "never-minted" }
	expired := func(d *telegramDoubles) string { return d.links.mintLive(nil, time.Now().Add(-time.Minute)) }
	consumed := func(d *telegramDoubles) string {
		raw := d.links.mintLive(nil, time.Now().Add(10*time.Minute))
		d.links.markConsumed(raw, 900)
		return raw
	}

	var answers []string
	for _, mint := range []func(*telegramDoubles) string{unknown, expired, consumed} {
		svc, doubles := newTelegramAuthService(t)
		raw := mint(doubles)
		if err := svc.HandleStart(context.Background(), 900, raw); err != nil {
			t.Fatalf("HandleStart() = %v, want nil", err)
		}
		answers = append(answers, doubles.sender.lastTo(900))
	}
	if answers[0] != answers[1] || answers[1] != answers[2] {
		t.Fatalf("dead-nonce answers differ: %q", answers)
	}
	if strings.Contains(answers[0], "/sign-in/") || strings.Contains(answers[0], "/sign-up/") {
		t.Fatalf("a dead nonce leaked a link: %q", answers[0])
	}
}

// Over the per-chat limit answers with the same message a dead nonce gets, so
// being rate-limited is not distinguishable from being late.
func TestHandleStartRateLimitsPerChatWithTheSameAnswer(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	doubles.accounts.bind(600, "user-2")
	doubles.links.recordRedemptions(600, 3, time.Now().Add(-time.Minute))
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 600, raw); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	if doubles.magicLinks.countFor("user-2") != 0 {
		t.Fatal("a magic link was minted for a rate-limited chat")
	}
	if got := doubles.sender.lastTo(600); !strings.Contains(got, "expired") {
		t.Fatalf("message = %q, want the same expiry answer a dead nonce gets", got)
	}
}

// The nonce is spent even when the chat is over its limit, so the same link
// cannot be retried until the hour rolls over.
func TestHandleStartSpendsTheNonceEvenWhenRateLimited(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	doubles.links.recordRedemptions(601, 3, time.Now().Add(-time.Minute))
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	_ = svc.HandleStart(context.Background(), 601, raw)
	if !doubles.links.isConsumed(raw) {
		t.Fatal("a rate-limited attempt left its nonce unspent")
	}
}
```

Write `newTelegramAuthService(t)` returning the service and a `*telegramDoubles` struct holding the four doubles, and implement every double method the tests call (`hasRaw`, `hasHashOf`, `mintLive`, `markConsumed`, `isConsumed`, `recordRedemptions`, `bind`, `lastTo`, `countFor`, `telegramCount`). `mintLive` takes `*testing.T` and tolerates nil, because two call sites above pass nil.

- [ ] **Step 3: Run and watch them fail**

```bash
cd api && go test ./internal/usecase/ -run 'TestStartLink|TestHandleStart' -v
```

Expected: `usecase.NewTelegramAuthService` undefined.

- [ ] **Step 4: Implement the service**

Create `api/internal/usecase/telegram_auth.go`:

```go
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

const (
	// telegramNonceTTL is short because the nonce is only ever held for the
	// few seconds between tapping a button in a browser and Telegram opening.
	// Nothing legitimate waits ten minutes.
	telegramNonceTTL = 10 * time.Minute

	// telegramLinksPerHourLimit mirrors magicLinkPerHourLimit. Without it, a
	// chat repeating /start is a free path to burn magic-link and signup rows.
	telegramLinksPerHourLimit = 3
)

// telegramDeadLinkMessage is the answer for an unknown nonce, an expired one,
// an already-consumed one, and a chat over its hourly limit. One message for
// all four, deliberately: any difference between them would let a caller tell
// the four apart by probing, and being rate-limited is not something a caller
// should be able to confirm.
const telegramDeadLinkMessage = "That sign-in link has expired. Start again from the app."

type TelegramAuthDeps struct {
	Links       TelegramLinkRepository
	Accounts    TelegramAccountRepository
	MagicLinks  MagicLinkRepository
	Signups     SignupRepository
	Sender      TelegramSender
	Tokens      TokenGenerator
	Clock       Clock
	BaseURL     string
	BotUsername string
}

// TelegramAuthService delivers Hearth's existing sign-in and sign-up tokens
// over Telegram. It mints no token type of its own: a parallel token table
// would mean two expiry rules, two rate limits and two enumeration analyses
// drifting apart, and the second one would be the one nobody reviews.
type TelegramAuthService struct{ d TelegramAuthDeps }

func NewTelegramAuthService(d TelegramAuthDeps) *TelegramAuthService {
	return &TelegramAuthService{d: d}
}

// TelegramStartLink is the deep link a browser sends the person to.
type TelegramStartLink struct {
	URL       string
	ExpiresAt time.Time
}

// StartLink mints a nonce and returns the deep link that carries it into
// Telegram. It takes no identifier -- no email, no username, nothing -- which
// is why this endpoint has no enumeration oracle to defend: there is nothing
// to probe for.
func (s *TelegramAuthService) StartLink(ctx context.Context) (TelegramStartLink, error) {
	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return TelegramStartLink{}, fmt.Errorf("generate telegram nonce: %w", err)
	}
	expiresAt := s.d.Clock.Now().Add(telegramNonceTTL)
	if err := s.d.Links.Create(ctx, hash, expiresAt); err != nil {
		return TelegramStartLink{}, fmt.Errorf("store telegram nonce: %w", err)
	}
	return TelegramStartLink{
		URL:       fmt.Sprintf("https://t.me/%s?start=%s", s.d.BotUsername, raw),
		ExpiresAt: expiresAt,
	}, nil
}

// HandleStart is called by the poller for every /start. It returns an error
// only for failures worth retrying or alerting on; every ordinary refusal is
// answered in the chat and returns nil, because the person on the other end
// needs an answer, not a stack trace.
func (s *TelegramAuthService) HandleStart(ctx context.Context, chatID int64, payload string) error {
	now := s.d.Clock.Now()

	// Consume first, then check the limit. A refused attempt still spends its
	// nonce, so the same link cannot be retried until the hour rolls over.
	if err := s.d.Links.Consume(ctx, s.d.Tokens.HashToken(payload), chatID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return s.say(ctx, chatID, telegramDeadLinkMessage)
		}
		return fmt.Errorf("consume telegram nonce: %w", err)
	}

	count, err := s.d.Links.CountLinksSince(ctx, chatID, now.Add(-time.Hour))
	if err != nil {
		return fmt.Errorf("count telegram links: %w", err)
	}
	// The row just consumed is included in the count, so the limit is reached
	// at telegramLinksPerHourLimit redemptions, not one past it.
	if count > telegramLinksPerHourLimit {
		slog.Info("telegram link rate limit reached", "chat_hash", hashPrefix(s.d.Tokens.HashToken(fmt.Sprint(chatID)), 12))
		return s.say(ctx, chatID, telegramDeadLinkMessage)
	}

	userID, err := s.d.Accounts.ByChatID(ctx, chatID)
	switch {
	case err == nil:
		return s.sendSignIn(ctx, chatID, userID)
	case errors.Is(err, domain.ErrNotFound):
		return s.sendSignUp(ctx, chatID)
	default:
		return fmt.Errorf("look up telegram account: %w", err)
	}
}

func (s *TelegramAuthService) sendSignIn(ctx context.Context, chatID int64, userID string) error {
	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return fmt.Errorf("generate magic link token: %w", err)
	}
	if err := s.d.MagicLinks.Create(ctx, userID, hash, s.d.Clock.Now().Add(magicLinkTTL)); err != nil {
		return fmt.Errorf("store magic link: %w", err)
	}
	return s.say(ctx, chatID, fmt.Sprintf(
		"Tap to sign in to Hearth:\n%s/sign-in/magic?token=%s\n\nThis link works once, for 15 minutes.",
		s.d.BaseURL, raw))
}

func (s *TelegramAuthService) sendSignUp(ctx context.Context, chatID int64) error {
	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return fmt.Errorf("generate signup token: %w", err)
	}
	if err := s.d.Signups.CreateForTelegram(ctx, chatID, hash, s.d.Clock.Now().Add(SignupTTL)); err != nil {
		return fmt.Errorf("store telegram signup: %w", err)
	}
	return s.say(ctx, chatID, fmt.Sprintf(
		"Tap to create your Hearth household:\n%s/sign-up/%s\n\nThis link works once, for 24 hours.",
		s.d.BaseURL, raw))
}

func (s *TelegramAuthService) say(ctx context.Context, chatID int64, text string) error {
	if err := s.d.Sender.SendMessage(ctx, chatID, text); err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run and watch them pass**

```bash
cd api && go test ./internal/usecase/ -v
```

Expected: all PASS, including every pre-existing usecase test.

- [ ] **Step 6: Mutation-check the identical-answer property**

Change `sendSignUp`'s branch so a dead nonce falls through to it instead of returning `telegramDeadLinkMessage` — i.e. delete the `errors.Is(err, domain.ErrNotFound)` early return in `HandleStart`. Re-run. Expected: `TestHandleStartAnswersIdenticallyForEveryDeadNonce` FAILS. Restore, confirm green.

- [ ] **Step 7: Commit**

```bash
git add api/internal/usecase/
git commit -m "feat(auth): TelegramAuthService delivers existing tokens over Telegram"
```

---

## Task 8: The HTTP route

**Files:**
- Create: `api/internal/adapter/http/telegram_handlers.go`
- Modify: `api/internal/adapter/http/router.go`
- Modify: `api/internal/adapter/http/middleware_ratelimit.go`
- Test: `api/internal/adapter/http/telegram_api_test.go`

**Interfaces:**
- Consumes: Task 7's `*usecase.TelegramAuthService`.
- Produces: `Deps.Telegram *usecase.TelegramAuthService` (nil means the feature is off), route `POST /api/v1/auth/telegram/start`, constant `telegramStartsPerIPPerHour`.

- [ ] **Step 1: Write the failing tests**

Create `api/internal/adapter/http/telegram_api_test.go`, following the existing `auth_api_test.go` for how a router and its `Deps` are built in this package.

```go
func TestTelegramStartReturnsADeepLink(t *testing.T) {
	srv := newTestServerWithTelegram(t) // builds Deps with a real TelegramAuthService over doubles
	defer srv.Close()

	resp := post(t, srv, "/api/v1/auth/telegram/start", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		URL       string `json:"url"`
		ExpiresAt string `json:"expiresAt"`
	}
	decodeBody(t, resp, &body)
	if !strings.HasPrefix(body.URL, "https://t.me/") {
		t.Fatalf("url = %q, want a t.me link", body.URL)
	}
	if body.ExpiresAt == "" {
		t.Fatal("expiresAt is empty; every 2xx here must carry a usable body")
	}
}

// With no bot configured the route must not exist at all, so an install that
// never set up Telegram behaves exactly as it did before this feature.
func TestTelegramStartIs404WhenTheFeatureIsOff(t *testing.T) {
	srv := newTestServer(t) // the existing helper: Deps.Telegram is nil
	defer srv.Close()

	resp := post(t, srv, "/api/v1/auth/telegram/start", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when Telegram is not configured", resp.StatusCode)
	}
}

// The route takes no identifier, so there is nothing to enumerate -- but it
// still mints a row per call, so it must be limited per IP like sign-up is.
func TestTelegramStartIsRateLimitedPerIP(t *testing.T) {
	srv := newTestServerWithTelegram(t)
	defer srv.Close()

	var last int
	for i := 0; i < telegramStartsPerIPPerHourForTest+1; i++ {
		last = post(t, srv, "/api/v1/auth/telegram/start", nil).StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("status after exceeding the limit = %d, want 429", last)
	}
}
```

Export the limit to the test through a package-level constant read directly (`telegramStartsPerIPPerHour`); the `...ForTest` name above is a placeholder for whichever the package's existing rate-limit tests use — match them rather than adding a new convention.

- [ ] **Step 2: Run and watch them fail**

Expected: `Deps.Telegram` and `telegramStartsPerIPPerHour` undefined.

- [ ] **Step 3: Implement the handler**

Create `api/internal/adapter/http/telegram_handlers.go`:

```go
package httpadapter

import (
	"net/http"
	"time"
)

type telegramStartResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// handleTelegramStart mints the deep link that carries a sign-in request into
// Telegram. It reads no body and takes no identifier: the person is not
// claiming to be anyone yet, which is why this route needs no oracle defence.
//
// A nil Deps.Telegram means no bot is configured, and the route answers 404 --
// the same answer any unrouted path gets, so an install without Telegram gives
// away nothing about whether the feature exists.
func handleTelegramStart(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Telegram == nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "That endpoint does not exist.", nil)
			return
		}
		link, err := deps.Telegram.StartLink(r.Context())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, telegramStartResponse{URL: link.URL, ExpiresAt: link.ExpiresAt})
	}
}
```

- [ ] **Step 4: Wire the route and the limiter**

In `api/internal/adapter/http/middleware_ratelimit.go`, beside `signUpRequestsPerIPPerHour`:

```go
// telegramStartsPerIPPerHour is more generous than the sign-up limit because
// this endpoint mails nothing and costs one small row -- but it is limited all
// the same, since it is reachable without a session.
const telegramStartsPerIPPerHour = 20
```

In `api/internal/adapter/http/router.go`, add to `Deps`:

```go
	// Telegram is nil when no bot is configured. The route checks for nil
	// rather than being conditionally registered, so the router's shape does
	// not change with configuration and every test builds the same tree.
	Telegram *usecase.TelegramAuthService
```

and inside the `auth` route group, after the sign-up group:

```go
			// Its own limiter instance, not the sign-up group's: a person who
			// has just signed up should not find Telegram sign-in already
			// spent, and vice versa.
			auth.Group(func(tg chi.Router) {
				now := func() time.Time { return deps.Clock.Now() }
				tg.Use(rateLimitByIP(newIPRateLimiter(telegramStartsPerIPPerHour, time.Hour, now)))
				tg.Post("/telegram/start", handleTelegramStart(deps))
			})
```

- [ ] **Step 5: Run and watch them pass**

```bash
cd api && go test ./internal/adapter/http/ -v
```

Expected: all PASS, including the pre-existing router tests.

- [ ] **Step 6: Mutation-check the feature-off answer**

Delete the `deps.Telegram == nil` guard. Re-run. Expected: `TestTelegramStartIs404WhenTheFeatureIsOff` FAILS (with a panic or a 500 rather than a 404). Restore, confirm green.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/http/
git commit -m "feat(api): POST /auth/telegram/start"
```

---

## Task 9: The frontend

**Files:**
- Modify: `web/src/features/auth/useAuth.ts`
- Modify: `web/src/features/auth/SignInScreen.tsx`
- Modify: `web/src/features/auth/SignUpCompleteScreen.tsx`
- Modify: `web/src/features/auth/copy.ts`
- Modify: `web/src/features/auth/schemas.ts`
- Test: `web/src/features/auth/SignInScreen.test.tsx`, `web/src/features/auth/SignUpCompleteScreen.test.tsx`

**Interfaces:**
- Consumes: Task 8's `POST /api/v1/auth/telegram/start` returning `{url, expiresAt}`; Task 4's `GET /api/v1/auth/sign-up/{token}` now returning `{email, channel}`.
- Produces: `useStartTelegramSignIn()` mutation hook; a "Continue with Telegram" control on the sign-in screen; a channel-aware create-household screen.

- [ ] **Step 1: Write the failing tests**

In `web/src/features/auth/SignInScreen.test.tsx`:

```tsx
it("opens the returned Telegram deep link when Continue with Telegram is pressed", async () => {
  const open = vi.fn();
  vi.stubGlobal("open", open);
  server.use(
    http.post("/api/v1/auth/telegram/start", () =>
      HttpResponse.json({
        url: "https://t.me/HearthBot?start=abc123",
        expiresAt: "2026-09-01T10:10:00Z",
      }),
    ),
  );

  render(<SignInScreen />, { wrapper: Providers });
  await userEvent.click(screen.getByRole("button", { name: /continue with telegram/i }));

  await waitFor(() =>
    expect(open).toHaveBeenCalledWith("https://t.me/HearthBot?start=abc123", "_blank", "noopener"),
  );
});

// The control must not appear on an install with no bot configured, where the
// endpoint answers 404 -- a button that always fails is worse than no button.
it("hides Continue with Telegram once the endpoint answers 404", async () => {
  server.use(
    http.post("/api/v1/auth/telegram/start", () =>
      HttpResponse.json({ error: { code: "NOT_FOUND", message: "That endpoint does not exist." } }, { status: 404 }),
    ),
  );

  render(<SignInScreen />, { wrapper: Providers });
  await userEvent.click(screen.getByRole("button", { name: /continue with telegram/i }));

  await waitFor(() =>
    expect(screen.queryByRole("button", { name: /continue with telegram/i })).not.toBeInTheDocument(),
  );
});
```

In `web/src/features/auth/SignUpCompleteScreen.test.tsx`:

```tsx
// A Telegram sign-up has no address to show. Rendering an empty read-only
// email box would look like a field someone forgot to fill in.
it("shows the Telegram channel instead of an empty email box", async () => {
  server.use(
    http.get("/api/v1/auth/sign-up/:token", () =>
      HttpResponse.json({ email: "", channel: "telegram" }),
    ),
  );

  render(<SignUpCompleteScreen token="tok" />, { wrapper: Providers });

  await screen.findByText(/telegram/i);
  expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
});

it("still shows the read-only address for an email sign-up", async () => {
  server.use(
    http.get("/api/v1/auth/sign-up/:token", () =>
      HttpResponse.json({ email: "someone@example.com", channel: "email" }),
    ),
  );

  render(<SignUpCompleteScreen token="tok" />, { wrapper: Providers });

  expect(await screen.findByDisplayValue("someone@example.com")).toBeInTheDocument();
});
```

Match this package's existing test setup (`Providers`, the msw `server`, and how `SignUpCompleteScreen` currently receives its token) rather than introducing new harness conventions.

- [ ] **Step 2: Run and watch them fail**

```bash
cd web && npx vitest run src/features/auth/SignInScreen.test.tsx src/features/auth/SignUpCompleteScreen.test.tsx
```

Expected: no such button; the channel field does not exist.

- [ ] **Step 3: Widen the sign-up preview schema**

In `web/src/features/auth/schemas.ts`, add `channel` to the sign-up preview schema:

```ts
// channel is "email" | "telegram". Parsed as a union rather than a string so an
// unrecognised value fails loudly here instead of silently rendering the wrong
// screen -- the same refuse-what-you-did-not-construct rule the API follows.
export const signUpPreviewSchema = z.object({
  email: z.string(),
  channel: z.union([z.literal("email"), z.literal("telegram")]),
});
```

- [ ] **Step 4: Add the mutation hook**

In `web/src/features/auth/useAuth.ts`:

```ts
// Telegram sign-in is optional: an install with no bot configured answers 404,
// and the screen hides the control rather than offering a button that always
// fails.
export function useStartTelegramSignIn() {
  return useMutation({
    mutationFn: () =>
      apiFetch<{ url: string; expiresAt: string }>("/auth/telegram/start", {
        method: "POST",
      }),
  });
}
```

- [ ] **Step 5: Add the control to the sign-in screen**

In `SignInScreen.tsx`, add `const [telegramUnavailable, setTelegramUnavailable] = useState(false)` and a `startTelegram = useStartTelegramSignIn()`, then render below the existing magic-link control:

```tsx
{!telegramUnavailable && (
  <button
    type="button"
    onClick={() =>
      startTelegram.mutate(undefined, {
        onSuccess: (data) => window.open(data.url, "_blank", "noopener"),
        // A 404 means this install has no bot. Hide the control rather than
        // showing an error the person can do nothing about.
        onError: (err) => {
          if (err instanceof ApiError && err.status === 404) {
            setTelegramUnavailable(true);
            return;
          }
          setTelegramError(apiErrorMessage(err, TELEGRAM_FALLBACK_ERROR));
        },
      })
    }
  >
    Continue with Telegram
  </button>
)}
```

Add `TELEGRAM_FALLBACK_ERROR` to `copy.ts`:

```ts
// Deliberately says nothing about why. The person cannot act on "Telegram
// returned 500", and this screen's other errors follow the same rule.
export const TELEGRAM_FALLBACK_ERROR =
  "We could not start Telegram sign-in just now. Try again in a moment.";
```

Match the existing markup's class names and error-rendering shape; the button above shows behaviour, not final styling.

- [ ] **Step 6: Make the create-household screen channel-aware**

In `SignUpCompleteScreen.tsx`, `CompleteSignUpForm` takes `channel` alongside `email`, and the read-only email block at line ~241 becomes:

```tsx
{channel === "email" ? (
  <>
    <label htmlFor="sign-up-email" className="text-xs font-semibold text-label">
      Email
    </label>
    <input id="sign-up-email" type="email" autoComplete="email" readOnly value={email} />
  </>
) : (
  <p className="text-xs text-label">
    You are signing up with Telegram. Your sign-in links come to that chat.
  </p>
)}
```

and the call site becomes `<CompleteSignUpForm token={token} email={preview.data.email} channel={preview.data.channel} />`.

- [ ] **Step 7: Run and watch them pass**

```bash
cd web && npx vitest run src/features/auth/
export PATH=$PATH:/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin
make lint
```

Expected: all frontend auth tests PASS; `make lint` (arch, typecheck, eslint) green.

- [ ] **Step 8: Mutation-check the 404 branch**

Change `err.status === 404` to `err.status === 500`. Re-run. Expected: "hides Continue with Telegram once the endpoint answers 404" FAILS. Restore, confirm green.

- [ ] **Step 9: Commit**

```bash
git add web/src/features/auth/
git commit -m "feat(web): continue with Telegram, and a channel-aware sign-up screen"
```

---

## Task 10: Wiring, documentation, and the real-browser walk

**Files:**
- Modify: `api/cmd/api/main.go`
- Modify: `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/INFRASTRUCTURE.md`, `docs/LEARNING.md`
- Modify: `docs/adr/0003-mail-stays-on-the-box.md`
- Create: `docs/adr/0004-telegram-as-a-second-delivery-channel.md`
- Test: the running application, in a browser

**Interfaces:**
- Consumes: everything above.
- Produces: a working feature.

- [ ] **Step 1: Wire it in `main.go`**

Beside the other repository constructions:

```go
	telegramLinks := postgres.NewTelegramLinkRepo(db)
	telegramAccounts := postgres.NewTelegramAccountRepo(db)
```

After `signupSvc` is built:

```go
	// Nil unless a bot is configured. httpadapter.Deps.Telegram being nil is
	// what makes the route answer 404, so "not configured" is expressed once,
	// here, rather than in every consumer.
	var telegramSvc *usecase.TelegramAuthService
	var telegramPoller *telegram.Poller
	if cfg.TelegramEnabled() {
		client := telegram.NewClient(cfg.TelegramBotToken)
		telegramSvc = usecase.NewTelegramAuthService(usecase.TelegramAuthDeps{
			Links:       telegramLinks,
			Accounts:    telegramAccounts,
			MagicLinks:  magicLinks,
			Signups:     signups,
			Sender:      client,
			Tokens:      tokens,
			Clock:       sysClock,
			BaseURL:     cfg.AppBaseURL,
			BotUsername: cfg.TelegramBotUsername,
		})
		telegramPoller = telegram.NewPoller(client, telegramSvc)
	}
```

Add `Telegram: telegramSvc` to the `httpadapter.Deps` literal, and start the poller beside the existing background goroutine at `main.go:248`:

```go
	if telegramPoller != nil {
		go telegramPoller.Run(ctx) // ctx is the signal.NotifyContext from :58
	}
```

- [ ] **Step 2: Verify the whole suite**

```bash
export PATH=$PATH:/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make lint && make test
```

Expected: both green. Do not proceed until they are.

- [ ] **Step 3: Create a real bot and run the app**

Message `@BotFather` on Telegram, `/newbot`, and take the token and username. Then:

```bash
echo 'TELEGRAM_BOT_TOKEN=<token>'       >> .env
echo 'TELEGRAM_BOT_USERNAME=<username>' >> .env
make dev
```

- [ ] **Step 4: Walk the stranger path in a real browser**

At `http://localhost:5173`, with the API logs visible:

1. Press **Continue with Telegram** on the sign-in screen. A Telegram tab or app opens on the bot.
2. Press **Start**. The bot answers with a create-household link.
3. Tap that link. The create-household screen opens and says you are signing up with Telegram — **no empty email box**.
4. Fill in household name, display name, currency and password. Submit.
5. You land signed in, inside the household.
6. Check the database: `make psql`, then
   `SELECT u.id, u.email, t.chat_id FROM users u JOIN telegram_accounts t ON t.user_id = u.id;`
   The new user must have a NULL email and a bound chat.

- [ ] **Step 5: Walk the returning path**

1. Sign out.
2. Press **Continue with Telegram** again, press Start.
3. The bot now answers with a **sign-in** link, not a sign-up link.
4. Tap it. You are signed in as the same user.
5. Tap the same link a second time. It must refuse — the magic link is single-use.

- [ ] **Step 6: Walk the refusal paths**

1. Press Continue with Telegram, but do not tap the link. Wait 10 minutes, then tap it. The bot says the link expired.
2. Repeat Start four times inside an hour. The fourth must get the same expiry message, and no fourth link.
3. Stop the API (`make down`), press Start in Telegram, restart (`make up`). The redelivered update must not sign anyone in twice or crash the process.

- [ ] **Step 7: Update the documentation**

- **`docs/SYSTEM_DESIGN.md`** — use the `maintaining-system-design` skill. New adapter, two new tables, the altered `signups`, and the new request flow. Change the prose under the diagrams too; that is where the non-obvious reasoning lives.
- **`docs/FEATURE_TRACKER.md`** — add rows for Telegram sign-up and Telegram sign-in, marked ✅. Add a ⬜ row for Telegram invites, which this slice deliberately does not build. Recount the summary table at the top.
- **`docs/INFRASTRUCTURE.md`** — add Telegram to the services table: what it is (delivery channel for sign-in and sign-up links), cost ($0), and what breaks without it (Telegram sign-in stops; email sign-in unaffected; existing sessions unaffected). Add the bot token to the credentials table, saying where it lives.
- **`docs/adr/0003-mail-stays-on-the-box.md`** — amend. Its exit condition, "the day a person who is not Andreas or Christine needs to receive an email", is no longer the trigger, because Telegram now carries strangers. Say what the new trigger is rather than deleting the old one.
- **`docs/adr/0004-telegram-as-a-second-delivery-channel.md`** — write it. Why Telegram over WhatsApp and SMS (per-message cost, Meta business verification, the 1 October 2026 change to WhatsApp's free in-window replies); why link-back rather than cross-device (the forwarding takeover); why no `Notifier` port yet.
- **`docs/LEARNING.md`** — one entry per defect this work turned up. If a defect matches an existing pattern, add it there as evidence rather than starting a new section.

- [ ] **Step 8: Final verification and commit**

```bash
make lint && make test
git add api/cmd/api/main.go docs/
git commit -m "feat: Telegram sign-in, wired and documented"
```

---

## Self-Review Notes

Checked after writing, per the writing-plans skill:

**Spec coverage.** Every spec section maps to a task: decisions 1–5 → Tasks 4, 5, 6, 7; the data model → Task 1; configuration → Task 2; both flows → Tasks 7, 8, 9; the security analysis → Task 7's identical-answer and rate-limit tests plus Task 8's route tests; failure modes → Task 6; testing → every task's TDD cycle plus Task 10's browser walk; documentation → Task 10 Step 7.

**One deliberate spec deviation, recorded here rather than buried.** The spec says `SignupPreview` gains a `Channel` field; the plan also widens `GET /auth/sign-up/{token}`'s JSON body to carry it, which the spec does not spell out. The frontend cannot render the right screen otherwise.

**Type consistency.** `HandleStart(ctx, chatID int64, payload string) error` is identical in `telegram.StartHandler` (Task 6) and `TelegramAuthService` (Task 7). `TelegramSender.SendMessage(ctx, chatID int64, text string) error` is identical in the port (Task 7) and `*telegram.Client` (Task 5). `Consume(ctx, nonceHash []byte, chatID int64) error` is identical in the port (Task 3) and the repository (Task 3).

**Known soft spots for the implementer.** Two names are taken from neighbouring test files rather than verified: the postgres package's pool/repo test helpers (Task 1 Step 5, Task 3 Step 6) and the config package's `setRequiredEnv` (Task 2 Step 1). Use whatever those packages already call them; do not add a second convention. The `sqlcgen` parameter types for nullable columns (Task 3 Step 4) depend on generated output — read `sqlcgen/telegram.sql.go` after Task 1 and match it exactly.
