# Telegram account linking — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A signed-in member connects their existing Hearth account to their
Telegram chat from Settings, and disconnects it again.

**Architecture:** Browser-first and two-phase. Settings mints a nonce carrying
the member's `user_id` into the existing `telegram_link_requests` table; the
bot's `/start` consumes it, records which chat redeemed it and writes *no*
binding; the browser that minted it sees which chat arrived and confirms; only
then is the `telegram_accounts` row written. The confirm is the security of
the feature — see the spec's decision 1 — not ceremony.

**Tech Stack:** Go 1.24 (chi, pgx, sqlc, goose), Postgres, React + TypeScript
(TanStack Query, zod, Tailwind), testcontainers for the Postgres suites.

**Spec:** `docs/superpowers/specs/2026-09-09-telegram-account-linking-design.md`
— read it before Task 1 and keep it open; every task below argues from it.

## Global Constraints

- Clean architecture, enforced by `make lint-arch` including in tests:
  `internal/domain` imports the standard library only; `internal/usecase` may
  add `internal/domain`; everything else is `internal/adapter/**` or `cmd/**`.
  No `pgx`, `chi` or Telegram type leaves the adapter layer.
- **Authorisation lives at each channel's inbound edge** (ADR 8). No service
  takes an actor parameter. The HTTP handler reads `RequestScope(r).UserID`
  and passes it as the subject; the Telegram edge decides from the row.
- Every 2xx except 204 carries a JSON body — `apiFetch` throws on an ok
  response it cannot parse.
- Fail closed on values you did not construct: every `switch` over a value
  from a database column or a third party needs a refusing `default`.
- A missing row becomes `domain.ErrNotFound` at the adapter boundary, never
  `pgx.ErrNoRows` further up.
- Doc comments carry the contract; comments say **why**, never what the line
  already says.
- Definition of done: `make lint && make test` green, at least one new test
  mutation-checked, `docs/FEATURE_TRACKER.md` and `docs/LEARNING.md` updated,
  and the product driven in a real browser.

**Environment for a bare shell on this machine** (needed by every `go` and
`make test` step):

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
```

**Branch:** every feature in this repo's log arrives as a PR merge. Start with
`git switch -c telegram-account-linking`.

---

## File Structure

**Created**

| File | Responsibility |
|---|---|
| `api/migrations/00018_telegram_link_user.sql` | `telegram_link_requests.user_id`, `.chat_username`, `telegram_accounts.chat_username` |
| `api/internal/usecase/telegram_link.go` | `TelegramLinkService`: Start, Status, Confirm, Unlink |
| `api/internal/usecase/telegram_link_test.go` | its tests |
| `api/internal/adapter/postgres/telegram_link_schema_test.go` | the new columns and the two UNIQUE collisions |
| `web/src/features/settings/TelegramPanel.tsx` | the three-state panel |
| `web/src/features/settings/TelegramPanel.test.tsx` | its tests |
| `docs/adr/0010-binding-a-chat-needs-a-confirm.md` | spec decision 1 |
| `docs/superpowers/plans/2026-09-09-telegram-account-linking-verification.md` | the browser walk record (Task 8) |

**Modified**

| File | Change |
|---|---|
| `api/internal/adapter/postgres/queries/telegram.sql` | mint with a user, consume returning the user, by id, count mints, binding read/write/delete |
| `api/internal/adapter/postgres/telegram_link_repo.go` | widened `Create`/`Consume`, new `ByID`, `CountMintsSince` |
| `api/internal/adapter/postgres/telegram_account_repo.go` | `ByUserID`, `Create`, `Delete` |
| `api/internal/usecase/ports.go` | both ports and their doc comments |
| `api/internal/usecase/telegram_auth.go` | `HandleStart`'s link branch |
| `api/internal/usecase/testdouble_test.go` | both doubles follow the ports |
| `api/internal/domain/errors.go` | five named errors |
| `api/internal/adapter/http/errors.go` | their status codes |
| `api/internal/adapter/telegram/update.go` | `Update.From`, `StartCommand` sender fields |
| `api/internal/adapter/telegram/poller.go` | `StartHandler` carries the sender |
| `api/internal/adapter/http/telegram_handlers.go` | five handlers |
| `api/internal/adapter/http/router.go` | the guarded group |
| `api/cmd/api/main.go` | wiring |
| `api/cmd/hearthctl/routes.go` | the five new routes |
| `web/src/features/settings/schemas.ts` | response schemas |
| `web/src/features/settings/SettingsPage.tsx` | the panel |
| `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md` | Task 8 |

---

### Task 1: The row can carry a user

**Files:**
- Create: `api/migrations/00018_telegram_link_user.sql`
- Create: `api/internal/adapter/postgres/telegram_link_schema_test.go`
- Modify: `api/internal/adapter/postgres/queries/telegram.sql`
- Modify: `api/internal/adapter/postgres/telegram_link_repo.go`
- Modify: `api/internal/usecase/ports.go:176-204`
- Modify: `api/internal/usecase/telegram_auth.go` (call sites only)
- Modify: `api/internal/usecase/testdouble_test.go:3495-3600` (the link double)

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  // usecase/ports.go
  type TelegramLinkRedemption struct {
      ID     string // the telegram_link_requests row id
      UserID string // "" for a sign-in nonce; set for a link nonce
  }

  type TelegramLinkRequest struct {
      ID           string
      UserID       string
      ChatID       int64
      ChatUsername string
      Consumed     bool
      ExpiresAt    time.Time
  }

  type TelegramLinkRepository interface {
      Create(ctx context.Context, userID string, nonceHash []byte, expiresAt time.Time) error
      Consume(ctx context.Context, nonceHash []byte, chatID int64, chatUsername string) (TelegramLinkRedemption, error)
      ByID(ctx context.Context, id string) (TelegramLinkRequest, error)
      CountMintsSince(ctx context.Context, userID string, since time.Time) (int, error)
      CountLinksSince(ctx context.Context, chatID int64, since time.Time) (int, error)
      Prune(ctx context.Context, before time.Time) (int64, error)
  }
  ```

- [ ] **Step 1: Write the migration**

`api/migrations/00018_telegram_link_user.sql`:

```sql
-- +goose Up

-- A link nonce is minted by a signed-in member for their own account; a
-- sign-in nonce is minted by a browser that has not said who it is. The two
-- share this table, and this column is the only thing that tells them apart
-- -- deliberately not a payload prefix, which stays free for the still
-- unbuilt inv_<token> Telegram invites. Nullable, because every row written
-- since 00011 is a sign-in nonce with no user to name.
ALTER TABLE telegram_link_requests
    ADD COLUMN user_id uuid REFERENCES users(id) ON DELETE CASCADE;

-- Stamped at redemption alongside chat_id, from Telegram's message.from.
-- The confirm screen reads it so the person approving can tell their own
-- chat from a stranger's; nothing server-side ever decides anything from
-- it, because it is a display name a third party controls.
ALTER TABLE telegram_link_requests
    ADD COLUMN chat_username text;

-- The same name, carried onto the binding when it is confirmed. The link
-- request it came from is pruned within the month and the Settings panel has
-- to keep naming the connected chat long after that.
ALTER TABLE telegram_accounts ADD COLUMN chat_username text;

-- +goose Down
ALTER TABLE telegram_accounts DROP COLUMN chat_username;
ALTER TABLE telegram_link_requests DROP COLUMN chat_username;
ALTER TABLE telegram_link_requests DROP COLUMN user_id;
```

- [ ] **Step 2: Apply it and confirm it is reversible**

```bash
make migrate && make migrate-down && make migrate
```

Expected: three clean runs. A `Down` that fails here is a `Down` that fails in
production at the worst possible moment.

- [ ] **Step 3: Write the queries**

In `api/internal/adapter/postgres/queries/telegram.sql`, replace
`CreateTelegramLinkRequest` and `ConsumeTelegramLinkRequest` and add three:

```sql
-- name: CreateTelegramLinkRequest :exec
INSERT INTO telegram_link_requests (nonce_hash, expires_at, user_id)
VALUES ($1, $2, $3);

-- ConsumeTelegramLinkRequest is the single-use gate, and it records the
-- redeeming chat in the same statement. The guard lives here rather than in
-- the caller for the same reason ConsumeSignup's does: zero rows is the
-- authoritative answer to the race between a read and this write. It now
-- returns user_id as well, because the caller's next decision -- link, sign
-- in, or sign up -- is exactly that column.
-- name: ConsumeTelegramLinkRequest :one
UPDATE telegram_link_requests
SET consumed_at = now(), chat_id = $2, chat_username = $3
WHERE nonce_hash = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING id, user_id;

-- name: GetTelegramLinkRequest :one
SELECT id, user_id, chat_id, chat_username, consumed_at, expires_at
FROM telegram_link_requests WHERE id = $1;

-- CountTelegramLinkMintsSince bounds how many link nonces one member can
-- mint. The per-chat limit below bounds redemption; this bounds minting,
-- which a signed-in session can now do with no chat involved at all.
-- name: CountTelegramLinkMintsSince :one
SELECT count(*) FROM telegram_link_requests
WHERE user_id = $1 AND created_at >= $2;
```

- [ ] **Step 4: Regenerate and check it compiles**

```bash
make sqlc && cd api && go build ./...
```

Expected: `telegram_link_repo.go` fails to compile — the generated params
gained a field. That is the next step's work.

- [ ] **Step 5: Widen the port**

In `api/internal/usecase/ports.go`, replace the `Create` and `Consume` lines
of `TelegramLinkRepository` and add the two new methods. Keep every existing
doc comment; add these:

```go
// Create stores a nonce. userID is "" for a sign-in nonce -- the browser has
// not said who it is -- and a user id for a link nonce minted by a signed-in
// member for their own account. That difference is the only thing separating
// the two kinds of row, so a Create that dropped it would silently turn a
// link into a sign-in.
Create(ctx context.Context, userID string, nonceHash []byte, expiresAt time.Time) error

// Consume stamps the row consumed and records which chat redeemed it, in one
// statement, and returns the row's id and the user it was minted for. The
// chat is unknown when the nonce is minted -- the browser has not met
// Telegram yet -- so redemption is the only moment the two can be joined, and
// CountLinksSince depends on it happening here. Returns domain.ErrNotFound if
// the nonce is unknown, expired or already consumed; those three are
// deliberately indistinguishable to a caller.
Consume(ctx context.Context, nonceHash []byte, chatID int64, chatUsername string) (TelegramLinkRedemption, error)

// ByID reads one link request for the browser that minted it. The caller must
// check the row's UserID against the session's own before showing anything:
// this method deliberately does not, because a repository that enforced
// ownership would be a second place authorisation lives (ADR 8).
ByID(ctx context.Context, id string) (TelegramLinkRequest, error)

// CountMintsSince counts link nonces this user has minted since a point in
// time, consumed or not. Bounded table growth, not a security control -- the
// session is already authenticated.
CountMintsSince(ctx context.Context, userID string, since time.Time) (int, error)
```

Add the two structs from the Interfaces block above the interface.

- [ ] **Step 6: Update the Postgres repository**

`api/internal/adapter/postgres/telegram_link_repo.go`:

```go
func (r *TelegramLinkRepo) Create(ctx context.Context, userID string, nonceHash []byte, expiresAt time.Time) error {
	return translate(r.q.CreateTelegramLinkRequest(ctx, sqlcgen.CreateTelegramLinkRequestParams{
		NonceHash: nonceHash,
		ExpiresAt: timestamptz(expiresAt),
		UserID:    nullableUUID(optionalString(userID)),
	}), "create telegram link request")
}

// Consume goes through translate, so an unknown, expired or already-consumed
// nonce all surface as domain.ErrNotFound. Keeping the three indistinguishable
// is deliberate: the bot answers all of them with one message, so none of them
// can be told apart by probing.
func (r *TelegramLinkRepo) Consume(ctx context.Context, nonceHash []byte, chatID int64, chatUsername string) (usecase.TelegramLinkRedemption, error) {
	row, err := r.q.ConsumeTelegramLinkRequest(ctx, sqlcgen.ConsumeTelegramLinkRequestParams{
		NonceHash:    nonceHash,
		ChatID:       &chatID,
		ChatUsername: optionalText(chatUsername),
	})
	if err != nil {
		return usecase.TelegramLinkRedemption{}, translate(err, "consume telegram link request")
	}
	return usecase.TelegramLinkRedemption{
		ID:     uuidToString(row.ID),
		UserID: uuidOrEmpty(row.UserID),
	}, nil
}

func (r *TelegramLinkRepo) ByID(ctx context.Context, id string) (usecase.TelegramLinkRequest, error) {
	row, err := r.q.GetTelegramLinkRequest(ctx, uuid(id))
	if err != nil {
		return usecase.TelegramLinkRequest{}, translate(err, "get telegram link request")
	}
	return usecase.TelegramLinkRequest{
		ID:           uuidToString(row.ID),
		UserID:       uuidOrEmpty(row.UserID),
		ChatID:       int64Or(row.ChatID),
		ChatUsername: textOr(row.ChatUsername),
		Consumed:     row.ConsumedAt.Valid,
		ExpiresAt:    timeOf(row.ExpiresAt),
	}, nil
}

func (r *TelegramLinkRepo) CountMintsSince(ctx context.Context, userID string, since time.Time) (int, error) {
	count, err := r.q.CountTelegramLinkMintsSince(ctx, sqlcgen.CountTelegramLinkMintsSinceParams{
		UserID:    uuid(userID),
		CreatedAt: timestamptz(since),
	})
	if err != nil {
		return 0, translate(err, "count telegram link mints")
	}
	return int(count), nil
}
```

Add whichever of `optionalString`, `optionalText`, `uuidOrEmpty`, `int64Or`,
`textOr` do not already exist to `api/internal/adapter/postgres/convert.go`,
following the file's existing naming — check first, several are likely there
under another name, and a second helper doing an existing helper's job is the
duplication `convert.go` exists to prevent. Each is three lines:

```go
// uuidOrEmpty renders a nullable uuid column as "" rather than the zero
// UUID's "00000000-...", so a caller can test it with a plain == "".
func uuidOrEmpty(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return u.String()
}
```

- [ ] **Step 7: Update the two call sites and the double**

In `telegram_auth.go`, `StartLink` now calls `s.d.Links.Create(ctx, "", hash,
expiresAt)` — a sign-in nonce names no user — and `HandleStart` takes the
redemption value and ignores it for now:

```go
if _, err := s.d.Links.Consume(ctx, s.d.Tokens.HashToken(payload), chatID, ""); err != nil {
```

In `testdouble_test.go`, `telegramLinkRow` gains `UserID`, `ChatUsername`;
`Create` stores `userID`; `Consume` returns the redemption; `mintLive` keeps
its signature and mints an unbound row; add `mintLiveFor(t, userID,
expiresAt)`, `byID`, `countMintsSince` mirroring the SQL exactly.

- [ ] **Step 8: Write the schema test**

`api/internal/adapter/postgres/telegram_link_schema_test.go` — follow the
existing `agreement_schema_test.go` for the fixture helpers:

```go
func TestTelegramLinkRequestCarriesItsUserThroughRedemption(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo := postgres.NewTelegramLinkRepo(db)
	user := seedUser(t, db) // an existing helper; see invite_repo_test.go

	if err := repo.Create(ctx, user.ID, []byte("hash-1"), time.Now().Add(10*time.Minute)); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	got, err := repo.Consume(ctx, []byte("hash-1"), 8801, "andreas")
	if err != nil {
		t.Fatalf("Consume() = %v, want nil", err)
	}
	if got.UserID != user.ID {
		t.Fatalf("UserID = %q, want %q", got.UserID, user.ID)
	}

	row, err := repo.ByID(ctx, got.ID)
	if err != nil {
		t.Fatalf("ByID() = %v, want nil", err)
	}
	// Consumed, carrying a user and a chat, is the pending state the browser
	// polls for -- there is no status column, so this is the whole of it.
	if !row.Consumed || row.ChatID != 8801 || row.ChatUsername != "andreas" {
		t.Fatalf("row = %+v, want consumed by chat 8801 (andreas)", row)
	}
}

func TestSignInNonceCarriesNoUser(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo := postgres.NewTelegramLinkRepo(db)

	if err := repo.Create(ctx, "", []byte("hash-2"), time.Now().Add(10*time.Minute)); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	got, err := repo.Consume(ctx, []byte("hash-2"), 8802, "")
	if err != nil {
		t.Fatalf("Consume() = %v, want nil", err)
	}
	// "" and not the zero UUID: HandleStart branches on this being empty.
	if got.UserID != "" {
		t.Fatalf("UserID = %q, want empty", got.UserID)
	}
}
```

- [ ] **Step 9: Run the API suite**

```bash
cd api && go test ./... 2>&1 | tail -20
```

Expected: PASS, including the existing `telegram_auth_test.go` and
`telegram_link_repo_test.go` untouched by this task.

- [ ] **Step 10: Commit**

```bash
git add api/migrations api/internal/adapter/postgres api/internal/usecase
git commit -m "Telegram links: a nonce can name the user who minted it

A link nonce and a sign-in nonce share telegram_link_requests, and user_id
IS NOT NULL is the only thing that tells them apart -- deliberately not a
payload prefix, which stays free for Telegram invites. Consume returns it
alongside the row id, because the caller's next decision is exactly that
column. chat_username rides along on both the request and the binding so a
confirm screen can name the chat that arrived."
```

---

### Task 2: A binding can be written, read and removed

**Files:**
- Modify: `api/internal/adapter/postgres/queries/telegram.sql`
- Modify: `api/internal/adapter/postgres/telegram_account_repo.go`
- Modify: `api/internal/usecase/ports.go:196-204`
- Modify: `api/internal/adapter/postgres/telegram_link_schema_test.go`
- Modify: `api/internal/usecase/testdouble_test.go` (the account double)

**Interfaces:**
- Consumes: Task 1's migration (`telegram_accounts.chat_username`).
- Produces:
  ```go
  type TelegramBinding struct {
      UserID       string
      ChatID       int64
      ChatUsername string
      LinkedAt     time.Time
  }

  type TelegramAccountRepository interface {
      ByChatID(ctx context.Context, chatID int64) (userID string, err error)
      ByUserID(ctx context.Context, userID string) (TelegramBinding, error)
      Create(ctx context.Context, b TelegramBinding) error
      Delete(ctx context.Context, userID string) error
  }
  ```

- [ ] **Step 1: Write the failing repository test**

Append to `telegram_link_schema_test.go`:

```go
func TestBindingRefusesASecondChatForTheSameUserAndASecondUserForTheSameChat(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo := postgres.NewTelegramAccountRepo(db)
	alice, bob := seedUser(t, db), seedUser(t, db)

	first := usecase.TelegramBinding{UserID: alice.ID, ChatID: 9001, ChatUsername: "alice"}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	// One chat per user: the same person cannot accumulate phones, or a
	// disconnect would miss one.
	second := usecase.TelegramBinding{UserID: alice.ID, ChatID: 9002}
	if err := repo.Create(ctx, second); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("second chat for the same user = %v, want domain.ErrAlreadyExists", err)
	}
	// One user per chat: two accounts on one chat would make a sign-in
	// ambiguous.
	stolen := usecase.TelegramBinding{UserID: bob.ID, ChatID: 9001}
	if err := repo.Create(ctx, stolen); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("second user for the same chat = %v, want domain.ErrAlreadyExists", err)
	}

	got, err := repo.ByUserID(ctx, alice.ID)
	if err != nil {
		t.Fatalf("ByUserID() = %v, want nil", err)
	}
	if got.ChatID != 9001 || got.ChatUsername != "alice" || got.LinkedAt.IsZero() {
		t.Fatalf("binding = %+v, want chat 9001 (alice) with a linked_at", got)
	}

	if err := repo.Delete(ctx, alice.ID); err != nil {
		t.Fatalf("Delete() = %v, want nil", err)
	}
	if _, err := repo.ByUserID(ctx, alice.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ByUserID after Delete = %v, want domain.ErrNotFound", err)
	}
	// The chat is free again: a disconnect that left the chat claimed would
	// make relinking the same phone impossible.
	if err := repo.Create(ctx, usecase.TelegramBinding{UserID: bob.ID, ChatID: 9001}); err != nil {
		t.Fatalf("rebinding a freed chat = %v, want nil", err)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestBindingRefuses -v 2>&1 | tail -20
```

Expected: FAIL — `repo.Create undefined`.

- [ ] **Step 3: Add the queries**

```sql
-- name: CreateTelegramAccount :exec
INSERT INTO telegram_accounts (user_id, chat_id, chat_username) VALUES ($1, $2, $3);

-- name: GetTelegramAccountByUserID :one
SELECT chat_id, chat_username, linked_at FROM telegram_accounts WHERE user_id = $1;

-- name: DeleteTelegramAccount :exec
DELETE FROM telegram_accounts WHERE user_id = $1;
```

The existing `CreateTelegramAccount` (two columns) is replaced, not added to —
`SignupRepository.Provision` calls it and now passes `""` for the username,
because a Telegram sign-up learns the chat from the update and has no browser
to show a name to.

- [ ] **Step 4: Implement the repository and the port**

```go
func (r *TelegramAccountRepo) ByUserID(ctx context.Context, userID string) (usecase.TelegramBinding, error) {
	row, err := r.q.GetTelegramAccountByUserID(ctx, uuid(userID))
	if err != nil {
		return usecase.TelegramBinding{}, translate(err, "get telegram account by user id")
	}
	return usecase.TelegramBinding{
		UserID:       userID,
		ChatID:       row.ChatID,
		ChatUsername: textOr(row.ChatUsername),
		LinkedAt:     timeOf(row.LinkedAt),
	}, nil
}

// Create returns domain.ErrAlreadyExists for either UNIQUE -- one chat per
// user, one user per chat. Which of the two collided is not distinguished
// here: the service knows which side it was asked about and says so; a
// repository that guessed would be guessing about the caller's intent.
func (r *TelegramAccountRepo) Create(ctx context.Context, b usecase.TelegramBinding) error {
	return translate(r.q.CreateTelegramAccount(ctx, sqlcgen.CreateTelegramAccountParams{
		UserID:       uuid(b.UserID),
		ChatID:       b.ChatID,
		ChatUsername: optionalText(b.ChatUsername),
	}), "create telegram account")
}

// Delete is idempotent: removing a binding that is not there is not an error,
// because the caller's goal -- this user has no chat -- is already true.
func (r *TelegramAccountRepo) Delete(ctx context.Context, userID string) error {
	return translate(r.q.DeleteTelegramAccount(ctx, uuid(userID)), "delete telegram account")
}
```

Rewrite the port's doc comment, which currently says there is no `Create`:

```go
// TelegramAccountRepository is the binding between a Telegram chat and the
// Hearth user it belongs to. Bindings are written in two places and nowhere
// else: inside SignupRepository.Provision's transaction, when a stranger
// creates a household from a chat, and by TelegramLinkService.Confirm, when
// a member who already has an account connects their chat from Settings.
// Both directions are UNIQUE in the database -- one chat per user, one user
// per chat -- and that constraint, not any check in Go, is what makes a
// sign-in unambiguous.
```

- [ ] **Step 5: Update the double, then run the suite**

`telegramAccountRepoDouble` gains a `byUserID` map kept in step with
`byChatID`, and `Create`/`Delete` maintaining both. `bind` stays for the
tests that pre-populate a binding.

```bash
cd api && go test ./... 2>&1 | tail -20
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add api/internal
git commit -m "Telegram bindings: create, read by user, delete

The binding used to be written in exactly one place, inside Provision's
transaction, and the port said so. Confirm is the second writer, so the port
gains Create, ByUserID and Delete and its doc comment now names both. Either
UNIQUE surfaces as domain.ErrAlreadyExists; which side collided is the
service's to say, not the repository's."
```

---

### Task 3: The bot learns who is speaking

**Files:**
- Modify: `api/internal/adapter/telegram/update.go`
- Modify: `api/internal/adapter/telegram/poller.go:12-14,113-122`
- Modify: `api/internal/adapter/telegram/update_test.go`
- Modify: `api/internal/adapter/telegram/poller_test.go`
- Modify: `api/internal/usecase/telegram_auth.go` (`HandleStart`'s signature)

**Interfaces:**
- Produces:
  ```go
  type StartCommand struct {
      ChatID   int64
      Payload  string
      Username string // Telegram's @name, "" when the account has none
  }

  // usecase
  func (s *TelegramAuthService) HandleStart(ctx context.Context, chatID int64, payload, username string) error
  ```

- [ ] **Step 1: Write the failing parser test**

In `update_test.go`:

```go
func TestParseStartReadsTheSenderName(t *testing.T) {
	u := telegram.Update{UpdateID: 7, Message: &telegram.Message{Text: "/start abc"}}
	u.Message.Chat.ID = 501
	u.Message.From = &telegram.User{Username: "andreas", FirstName: "Andreas"}

	got, ok := telegram.ParseStart(u)
	if !ok || got.Username != "andreas" {
		t.Fatalf("ParseStart() = %+v, %v; want Username \"andreas\"", got, ok)
	}
}

// Telegram omits `from` on a channel post. A nil there must not panic the
// poller: the update is still a /start, it just names nobody.
func TestParseStartToleratesAMissingSender(t *testing.T) {
	u := telegram.Update{UpdateID: 8, Message: &telegram.Message{Text: "/start abc"}}
	u.Message.Chat.ID = 502

	got, ok := telegram.ParseStart(u)
	if !ok || got.Username != "" {
		t.Fatalf("ParseStart() = %+v, %v; want ok with an empty Username", got, ok)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/adapter/telegram/ -run TestParseStart -v 2>&1 | tail -15
```

Expected: FAIL — `undefined: telegram.User`.

- [ ] **Step 3: Widen the payload**

```go
type Message struct {
	Text string `json:"text"`
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	// From is absent on a channel post, so this is a pointer and every
	// reader must handle nil. Read for display only -- the confirm screen
	// names the chat that redeemed a link -- never to decide anything:
	// a username is chosen by its owner and Telegram lets it change.
	From *User `json:"from"`
}

type User struct {
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

// senderName prefers the @username, falls back to the first name, and is ""
// when Telegram sent neither. "" is a legitimate value the confirm screen
// renders as "an unnamed chat" -- not an error.
func senderName(m *Message) string {
	if m.From == nil {
		return ""
	}
	if m.From.Username != "" {
		return m.From.Username
	}
	return m.From.FirstName
}
```

`ParseStart`'s `/start` case returns `StartCommand{ChatID: ..., Payload: ...,
Username: senderName(u.Message)}`.

- [ ] **Step 4: Carry it to the handler**

`StartHandler` becomes:

```go
// StartHandler is what the poller hands a parsed /start to. It is declared
// here, in the adapter, rather than imported from usecase, so this package
// depends on a shape rather than on a concrete service. username is display
// only; the chat id is the identity, and it is the only one of the two that
// Telegram guarantees.
type StartHandler interface {
	HandleStart(ctx context.Context, chatID int64, payload, username string) error
}
```

`dispatch` passes `start.Username`; `HandleStart` in `telegram_auth.go` takes
the parameter and passes it to `Links.Consume`; `poller_test.go`'s spy follows.

- [ ] **Step 5: Run the adapter and usecase suites**

```bash
cd api && go test ./internal/adapter/telegram/ ./internal/usecase/ 2>&1 | tail -15
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/telegram api/internal/usecase
git commit -m "Telegram: carry the sender's name off the update

The confirm screen has to name the chat that redeemed a link, or the person
approving is answering a question they cannot check. From is a pointer
because Telegram omits it on channel posts, and the name is display only --
the chat id remains the identity."
```

---

### Task 4: `/start` with a link nonce writes nothing

**Files:**
- Modify: `api/internal/usecase/telegram_auth.go:88-118`
- Modify: `api/internal/usecase/telegram_auth_test.go`
- Modify: `api/internal/domain/errors.go`

**Interfaces:**
- Consumes: `TelegramLinkRedemption` (Task 1), `TelegramAccountRepository`
  (Task 2), `HandleStart`'s username (Task 3).
- Produces: the pending state that Task 5's `Status` reads. No new exported
  Go symbol.

- [ ] **Step 1: Write the failing tests**

```go
func TestHandleStartWithALinkNonceLeavesTheBindingUnwritten(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	raw := doubles.links.mintLiveFor(t, "user-7", time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 601, raw, "andreas"); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	// The whole design in one assertion: the chat is recorded, and the
	// binding is not written until the browser that minted the nonce says so.
	// A one-phase bind would make a leaked deep link an account takeover.
	if _, err := doubles.accounts.ByChatID(context.Background(), 601); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("binding after /start = %v, want it still unwritten", err)
	}
	if got := doubles.sender.lastTo(601); !strings.Contains(got, "confirm") {
		t.Fatalf("message = %q, want it to send the person back to Hearth to confirm", got)
	}
	if doubles.magicLinks.countFor("user-7") != 0 {
		t.Fatal("a magic link was minted; a link nonce must mint no token at all")
	}
}

func TestHandleStartWithALinkNonceForAChatSomeoneElseOwnsSaysNothingUseful() {
	// ... same fixture; doubles.accounts.bind(602, "someone-else"), then a
	// link nonce for "user-7" redeemed from 602. Assert the chat is told the
	// bland line and the existing binding is untouched.
}

func TestHandleStartAnswersALinkNonceBeforeTheRateLimitBites(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	// Four redemptions from one chat: the fourth is over the 3/hour limit.
	for i := 0; i < 3; i++ {
		raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))
		_ = svc.HandleStart(context.Background(), 603, raw, "")
	}
	raw := doubles.links.mintLiveFor(t, "user-7", time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 603, raw, "andreas"); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	// The two ends of the flow must agree. If the limit ran first, the row
	// would be consumed and carrying a user id -- which the browser derives
	// as pending -- while the chat had been told the link was dead.
	if got := doubles.sender.lastTo(603); strings.Contains(got, "expired") {
		t.Fatalf("message = %q, want the confirm instruction, not the dead-link line", got)
	}
}
```

- [ ] **Step 2: Run and watch them fail**

```bash
cd api && go test ./internal/usecase/ -run TestHandleStart -v 2>&1 | tail -25
```

Expected: FAIL — `mintLiveFor` exists from Task 1, the service does not branch.

- [ ] **Step 3: Add the branch**

In `HandleStart`, immediately after `Consume` succeeds and **before**
`CountLinksSince`:

```go
	// A link nonce is answered here and goes no further: it mints no token,
	// so the per-chat limit below -- which exists to bound magic-link and
	// signup rows -- has nothing to bound on this path. Ordering matters
	// beyond tidiness: a rate-limited link nonce is already consumed and
	// already carrying a user id, which the browser derives as "pending", so
	// refusing it here would tell the chat the link was dead while the
	// browser offered a Confirm button that worked. Decision 4 of the spec.
	if redemption.UserID != "" {
		return s.handleLinkStart(ctx, chatID, redemption)
	}
```

```go
// handleLinkStart answers a /start that redeemed a link nonce. It writes no
// binding: the browser session that minted the nonce confirms, and that is
// the whole of the protection against a leaked deep link (ADR 10).
//
// Every refusal here is bland and identical, for the reason
// telegramDeadLinkMessage gives: a chat holding a nonce it may have stolen
// must not learn whether the account exists, already has a chat, or belongs
// to someone else. The session that minted it is told the real reason,
// because it has already proved who it is.
func (s *TelegramAuthService) handleLinkStart(ctx context.Context, chatID int64, r TelegramLinkRedemption) error {
	boundTo, err := s.d.Accounts.ByChatID(ctx, chatID)
	switch {
	case err == nil && boundTo == r.UserID:
		return s.say(ctx, chatID, "This chat is already connected to your Hearth account.")
	case err == nil:
		return s.say(ctx, chatID, telegramLinkRefusedMessage)
	case errors.Is(err, domain.ErrNotFound):
		return s.say(ctx, chatID, "Go back to Hearth and confirm this chat to finish connecting it.")
	default:
		return fmt.Errorf("look up telegram account: %w", err)
	}
}
```

with

```go
// telegramLinkRefusedMessage is the answer for every link that cannot be
// completed from this chat's side. It deliberately says nothing about why
// and points at the browser, which can say why safely.
const telegramLinkRefusedMessage = "Could not connect this chat. Open Hearth to see why."
```

- [ ] **Step 4: Run the tests**

```bash
cd api && go test ./internal/usecase/ -run TestHandleStart -v 2>&1 | tail -25
```

Expected: PASS.

- [ ] **Step 5: Mutation-check the ordering**

Move the `redemption.UserID != ""` branch to *after* the `CountLinksSince`
check and rerun. Expected:
`TestHandleStartAnswersALinkNonceBeforeTheRateLimitBites` FAILS. Restore the
order, rerun, PASS. Record both outcomes in the commit message.

- [ ] **Step 6: Commit**

```bash
git add api/internal
git commit -m "Telegram: a link nonce is answered, never bound

/start with a link nonce records which chat redeemed it and writes no
binding -- the browser that minted the nonce confirms, which is what stops a
leaked deep link from becoming an account takeover.

The branch runs before the per-chat rate limit, not after. Mutation-checked:
moving it after the limit turns
TestHandleStartAnswersALinkNonceBeforeTheRateLimitBites red, because a
rate-limited link leaves a consumed row the browser reads as pending while
the chat was told the link was dead."
```

---

### Task 5: `TelegramLinkService`

**Files:**
- Create: `api/internal/usecase/telegram_link.go`
- Create: `api/internal/usecase/telegram_link_test.go`
- Modify: `api/internal/domain/errors.go`
- Modify: `api/internal/usecase/testdouble_test.go` (a fixture for this service)

**Interfaces:**
- Consumes: both repositories from Tasks 1–2, `UserRepository.ByID`,
  `TokenGenerator`, `Clock`.
- Produces:
  ```go
  type TelegramLinkDeps struct {
      Links       TelegramLinkRepository
      Accounts    TelegramAccountRepository
      Users       UserRepository
      Tokens      TokenGenerator
      Clock       Clock
      BotUsername string
  }

  func NewTelegramLinkService(d TelegramLinkDeps) *TelegramLinkService

  type TelegramLinkStart struct{ ID, URL string; ExpiresAt time.Time }

  // Status is one of exactly these five, and the zod enum in Task 7 accepts
  // the same set: "waiting", "pending", "connected", "refused", "expired".
  type TelegramLinkStatus struct {
      Status       string
      ChatUsername string
      ChatID       int64
      // Reason is set only for "refused" -- the sentence the panel shows,
      // chosen by the service, never a database error.
      Reason       string
  }

  func (s *TelegramLinkService) Start(ctx context.Context, userID string) (TelegramLinkStart, error)
  func (s *TelegramLinkService) Status(ctx context.Context, userID, linkID string) (TelegramLinkStatus, error)
  func (s *TelegramLinkService) Confirm(ctx context.Context, userID, linkID string) (TelegramBinding, error)
  func (s *TelegramLinkService) Binding(ctx context.Context, userID string) (TelegramBinding, error)
  func (s *TelegramLinkService) Unlink(ctx context.Context, userID string) error
  ```

- [ ] **Step 1: Add the domain errors**

```go
	// ErrTelegramChatTaken is a chat already bound to a different Hearth
	// user. Named separately from ErrTelegramAlreadyLinked because the two
	// need different sentences: one is "that phone belongs to someone else",
	// the other is "you already have a phone".
	ErrTelegramChatTaken = errors.New("that telegram chat is connected to another account")

	// ErrTelegramAlreadyLinked is this user already having a chat. One chat
	// per user is a database constraint; this is how it reads to a person.
	ErrTelegramAlreadyLinked = errors.New("this account already has a telegram chat")

	// ErrTelegramLinkNotPending covers a confirm before any chat redeemed
	// the link, and a confirm after it expired. The two are one error
	// because the panel's next instruction is the same for both: start again.
	ErrTelegramLinkNotPending = errors.New("no chat has opened this link")

	// ErrTelegramUnlinkWouldLockOut is a disconnect refused because the
	// account has no email address. GetUserByEmail is WHERE email = $1 and
	// NULL never matches a parameter, so a Telegram-only account that
	// disconnects has no magic link, no password reset and no adminctl path
	// back in -- only make psql by hand.
	ErrTelegramUnlinkWouldLockOut = errors.New("this account has no email address to sign in with")

	// ErrTelegramMintsRateLimited bounds how many link attempts one member
	// can start in an hour. Table growth, not a security control.
	ErrTelegramMintsRateLimited = errors.New("too many telegram link attempts")
```

- [ ] **Step 2: Write the failing tests**

`api/internal/usecase/telegram_link_test.go` — the whole matrix:

```go
func TestStartMintsALinkNonceCarryingTheMember(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)

	got, err := svc.Start(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("Start() = %v, want nil", err)
	}
	if !strings.HasPrefix(got.URL, "https://t.me/HearthBot?start=") {
		t.Fatalf("URL = %q, want a t.me deep link", got.URL)
	}
	raw := strings.TrimPrefix(got.URL, "https://t.me/HearthBot?start=")
	if doubles.links.hasRaw(raw) {
		t.Fatal("the raw nonce was stored; it must be stored hashed")
	}
	if n, _ := doubles.links.CountMintsSince(context.Background(), "user-1", doubles.clock.Now().Add(-time.Hour)); n != 1 {
		t.Fatalf("mints for user-1 = %d, want 1", n)
	}
}

func TestStatusIsPendingOnlyAfterAChatRedeemsIt(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")

	got, err := svc.Status(context.Background(), "user-1", start.ID)
	if err != nil {
		t.Fatalf("Status() = %v, want nil", err)
	}
	if got.Status != "waiting" {
		t.Fatalf("Status = %q, want \"waiting\" before any chat arrives", got.Status)
	}

	doubles.links.redeem(start.ID, 701, "andreas")
	got, _ = svc.Status(context.Background(), "user-1", start.ID)
	if got.Status != "pending" || got.ChatUsername != "andreas" {
		t.Fatalf("Status = %+v, want pending naming andreas", got)
	}
}

func TestStatusHidesAnotherMembersLink(t *testing.T) {
	svc, _ := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")

	// 404, not 403: a row id must not be testable for existence.
	if _, err := svc.Status(context.Background(), "user-2", start.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Status for another member = %v, want domain.ErrNotFound", err)
	}
}

func TestConfirmWritesTheBinding(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 702, "andreas")

	got, err := svc.Confirm(context.Background(), "user-1", start.ID)
	if err != nil {
		t.Fatalf("Confirm() = %v, want nil", err)
	}
	if got.ChatID != 702 || got.ChatUsername != "andreas" {
		t.Fatalf("binding = %+v, want chat 702 (andreas)", got)
	}
	if id, _ := doubles.accounts.ByChatID(context.Background(), 702); id != "user-1" {
		t.Fatalf("ByChatID = %q, want user-1", id)
	}
}

func TestConfirmRefusesAnotherMembersLink(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 703, "thief")

	// The mutation target: delete the ownership check in Confirm and this is
	// the test that goes red.
	if _, err := svc.Confirm(context.Background(), "user-2", start.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Confirm by another member = %v, want domain.ErrNotFound", err)
	}
	if _, err := doubles.accounts.ByChatID(context.Background(), 703); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("a binding was written for a link the caller does not own")
	}
}

func TestConfirmRefusesAfterExpiry(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 704, "andreas")
	doubles.clock.advance(11 * time.Minute)

	if _, err := svc.Confirm(context.Background(), "user-1", start.ID); !errors.Is(err, domain.ErrTelegramLinkNotPending) {
		t.Fatalf("Confirm after expiry = %v, want domain.ErrTelegramLinkNotPending", err)
	}
}

func TestConfirmRefusesAChatSomeoneElseHasBound(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	doubles.accounts.bind(705, "user-9")
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 705, "andreas")

	if _, err := svc.Confirm(context.Background(), "user-1", start.ID); !errors.Is(err, domain.ErrTelegramChatTaken) {
		t.Fatalf("Confirm = %v, want domain.ErrTelegramChatTaken", err)
	}
}

func TestConfirmRefusesWhenTheMemberAlreadyHasAChat(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	doubles.accounts.bind(706, "user-1")
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 707, "andreas")

	if _, err := svc.Confirm(context.Background(), "user-1", start.ID); !errors.Is(err, domain.ErrTelegramAlreadyLinked) {
		t.Fatalf("Confirm = %v, want domain.ErrTelegramAlreadyLinked", err)
	}
}

func TestStatusStaysConnectedAfterTheLinkExpires(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 708, "andreas")
	if _, err := svc.Confirm(context.Background(), "user-1", start.ID); err != nil {
		t.Fatalf("Confirm() = %v, want nil", err)
	}
	doubles.clock.advance(11 * time.Minute)

	// Binding first, expiry last. A panel still polling when the nonce
	// expires must not be told its working connection expired.
	got, _ := svc.Status(context.Background(), "user-1", start.ID)
	if got.Status != "connected" {
		t.Fatalf("Status = %q, want \"connected\"", got.Status)
	}
}

func TestStartRefusesTheFourthMintInAnHour(t *testing.T) {
	svc, _ := newTelegramLinkService(t)
	for i := 0; i < 3; i++ {
		if _, err := svc.Start(context.Background(), "user-1"); err != nil {
			t.Fatalf("Start() #%d = %v, want nil", i+1, err)
		}
	}
	if _, err := svc.Start(context.Background(), "user-1"); !errors.Is(err, domain.ErrTelegramMintsRateLimited) {
		t.Fatalf("fourth Start() = %v, want domain.ErrTelegramMintsRateLimited", err)
	}
}

func TestUnlinkRefusesAnAccountWithNoEmail(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	doubles.users.addTelegramOnly("user-3") // email ""
	doubles.accounts.bind(709, "user-3")

	if err := svc.Unlink(context.Background(), "user-3"); !errors.Is(err, domain.ErrTelegramUnlinkWouldLockOut) {
		t.Fatalf("Unlink() = %v, want domain.ErrTelegramUnlinkWouldLockOut", err)
	}
	if id, _ := doubles.accounts.ByChatID(context.Background(), 709); id != "user-3" {
		t.Fatal("the binding was removed; that account would have no way back in")
	}
}

func TestUnlinkRemovesTheBinding(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	doubles.accounts.bind(710, "user-1") // user-1 has an email in the fixture

	if err := svc.Unlink(context.Background(), "user-1"); err != nil {
		t.Fatalf("Unlink() = %v, want nil", err)
	}
	if _, err := doubles.accounts.ByChatID(context.Background(), 710); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("binding after Unlink = %v, want gone", err)
	}
}
```

Add to the doubles: `telegramLinkRepoDouble.redeem(id, chatID, username)`
(stamps a row consumed by its id, standing in for the bot's `/start`),
`fixedClock.advance` if it is not already there, and
`userDouble.addTelegramOnly(id)` writing a user with `Email: ""`.

- [ ] **Step 3: Run and watch them fail**

```bash
cd api && go test ./internal/usecase/ -run TelegramLink -v 2>&1 | tail -25
```

Expected: FAIL — `undefined: usecase.NewTelegramLinkService`.

- [ ] **Step 4: Write the service**

`api/internal/usecase/telegram_link.go`. The load-bearing parts:

```go
// telegramLinkMintsPerHourLimit bounds how many link attempts one member can
// start in an hour. The per-chat limit in telegram_auth.go bounds redemption;
// this bounds minting, which a signed-in session can now do with no chat
// involved at all.
const telegramLinkMintsPerHourLimit = 3

// TelegramLinkService connects a Hearth account that already exists to a
// Telegram chat, and disconnects it again. It is deliberately separate from
// TelegramAuthService: that service delivers Hearth's existing tokens over a
// chat, this one writes the binding those deliveries depend on.
//
// It takes a userID as the subject it acts on, supplied by the handler from
// the session. That is not an actor parameter (ADR 8): the service enforces
// what is valid, the HTTP edge enforces who is asking.
type TelegramLinkService struct{ d TelegramLinkDeps }

// Start mints a link nonce and returns the deep link that carries it into
// Telegram, plus the row id the browser polls with.
func (s *TelegramLinkService) Start(ctx context.Context, userID string) (TelegramLinkStart, error) {
	// Refuse before minting: a limit checked after the write is a limit that
	// still grows the table it is protecting.
	count, err := s.d.Links.CountMintsSince(ctx, userID, s.d.Clock.Now().Add(-time.Hour))
	if err != nil {
		return TelegramLinkStart{}, fmt.Errorf("count telegram link mints: %w", err)
	}
	if count >= telegramLinkMintsPerHourLimit {
		return TelegramLinkStart{}, domain.ErrTelegramMintsRateLimited
	}
	// ... NewToken, Create(ctx, userID, hash, now+telegramNonceTTL), ByID to
	// read back the id the browser will poll with.
}

// Confirm writes the binding. It re-checks everything Status derived, because
// minutes pass between the two and the other chat can be bound in between;
// the UNIQUE constraints are the real gate and this is only the message.
func (s *TelegramLinkService) Confirm(ctx context.Context, userID, linkID string) (TelegramBinding, error) {
	row, err := s.d.Links.ByID(ctx, linkID)
	if err != nil {
		return TelegramBinding{}, err // ErrNotFound passes straight through
	}
	// An unknown row and another member's row are the same answer, so a row
	// id cannot be tested for existence.
	if row.UserID != userID {
		return TelegramBinding{}, domain.ErrNotFound
	}
	if !row.Consumed || !row.ExpiresAt.After(s.d.Clock.Now()) {
		return TelegramBinding{}, domain.ErrTelegramLinkNotPending
	}
	if boundTo, err := s.d.Accounts.ByChatID(ctx, row.ChatID); err == nil {
		if boundTo == userID {
			return s.d.Accounts.ByUserID(ctx, userID) // idempotent: already ours
		}
		return TelegramBinding{}, domain.ErrTelegramChatTaken
	} else if !errors.Is(err, domain.ErrNotFound) {
		return TelegramBinding{}, fmt.Errorf("look up telegram account by chat: %w", err)
	}
	if _, err := s.d.Accounts.ByUserID(ctx, userID); err == nil {
		return TelegramBinding{}, domain.ErrTelegramAlreadyLinked
	} else if !errors.Is(err, domain.ErrNotFound) {
		return TelegramBinding{}, fmt.Errorf("look up telegram account by user: %w", err)
	}

	binding := TelegramBinding{UserID: userID, ChatID: row.ChatID, ChatUsername: row.ChatUsername, LinkedAt: s.d.Clock.Now()}
	if err := s.d.Accounts.Create(ctx, binding); err != nil {
		// Both UNIQUEs arrive as ErrAlreadyExists; only this service knows
		// which side it just checked, so it says which one lost the race.
		if errors.Is(err, domain.ErrAlreadyExists) {
			return TelegramBinding{}, domain.ErrTelegramChatTaken
		}
		return TelegramBinding{}, fmt.Errorf("create telegram account: %w", err)
	}
	return binding, nil
}

// Status derives where a link has got to, in this order: binding first, then
// refusals, then expiry. Expiry last is deliberate -- a connected panel that
// is still polling when the nonce's ten minutes run out must keep reading
// "connected", not flip to "expired" ten minutes after it succeeded.
func (s *TelegramLinkService) Status(ctx context.Context, userID, linkID string) (TelegramLinkStatus, error) {
	// ... ByID, ownership -> ErrNotFound, then:
	//   binding for userID whose ChatID == row.ChatID  -> "connected"
	//   !row.Consumed && not expired                   -> "waiting"
	//   row.Consumed && chat bound elsewhere           -> "refused", ErrTelegramChatTaken's sentence
	//   row.Consumed && this user already bound        -> "refused", ErrTelegramAlreadyLinked's sentence
	//   row.Consumed && live                           -> "pending"
	//   default                                        -> "expired"
}

// Unlink removes the binding, refusing when the account has no email address
// -- see domain.ErrTelegramUnlinkWouldLockOut for what that account would
// lose. Delete is idempotent, so unlinking twice is not an error.
func (s *TelegramLinkService) Unlink(ctx context.Context, userID string) error {
	user, err := s.d.Users.ByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("look up user: %w", err)
	}
	if user.Email == "" {
		return domain.ErrTelegramUnlinkWouldLockOut
	}
	return s.d.Accounts.Delete(ctx, userID)
}
```

Fill in the elided bodies; the comments above are the contract each one owes.

- [ ] **Step 5: Run the tests**

```bash
cd api && go test ./internal/usecase/ -run TelegramLink -v 2>&1 | tail -30
```

Expected: PASS, all twelve.

- [ ] **Step 6: Mutation-check the ownership guard**

Delete `if row.UserID != userID` from `Confirm`, rerun. Expected:
`TestConfirmRefusesAnotherMembersLink` FAILS. Restore, rerun, PASS.

- [ ] **Step 7: Run the whole suite and commit**

```bash
cd api && go test ./... 2>&1 | tail -10
git add api/internal
git commit -m "TelegramLinkService: start, status, confirm, unlink

Confirm re-checks everything Status derived, because minutes pass between
them and the other chat can be bound in between; both UNIQUEs remain the
real gate and the service only chooses the sentence. Status derives binding
first and expiry last, so a connected panel still polling does not flip to
expired ten minutes after it worked. Unlink refuses an account with no email
address, which would otherwise have no door left.

Mutation-checked: removing Confirm's ownership check turns
TestConfirmRefusesAnotherMembersLink red."
```

---

### Task 6: The routes

**Files:**
- Modify: `api/internal/adapter/http/telegram_handlers.go`
- Modify: `api/internal/adapter/http/router.go:170-200`
- Modify: `api/internal/adapter/http/errors.go`
- Modify: `api/internal/adapter/http/telegram_api_test.go`
- Modify: `api/cmd/hearthctl/routes.go`
- Modify: `api/cmd/api/main.go`

**Interfaces:**
- Consumes: `TelegramLinkService` (Task 5).
- Produces: `Deps.TelegramLink *usecase.TelegramLinkService` — nil means no
  bot is configured and every route in the group answers 404.

- [ ] **Step 1: Write the failing guard tests**

In `telegram_api_test.go`, one test per guard so a failure names which broke:

```go
func TestTelegramLinkRoutesRefuseWithoutASession(t *testing.T)      // 401
func TestTelegramLinkRoutesRefuseAnAPIToken(t *testing.T)           // 401/403 from requireCookieSession
func TestTelegramLinkPostRefusesWithoutCSRF(t *testing.T)           // 403
func TestTelegramLinkRoutesAre404WhenTheFlagIsOff(t *testing.T)     // 404
func TestTelegramLinkRoutesAre404WithNoBotConfigured(t *testing.T)  // 404
func TestTelegramLinkPollingRouteNeedsNoCSRFHeader(t *testing.T)    // 200
```

The last one pins the reason the five routes share one group: `requireCSRF`
returns early for `GET`, `HEAD` and `OPTIONS` (`middleware_csrf.go:28`), so
the polling route needs no header. Copy the fixture from the existing API
token tests, which exercise the same guard stack.

- [ ] **Step 2: Run and watch them fail**

```bash
cd api && go test ./internal/adapter/http/ -run TestTelegramLink -v 2>&1 | tail -20
```

Expected: FAIL — 404 from an unrouted path on every one.

- [ ] **Step 3: Write the handlers**

Five handlers in `telegram_handlers.go`, each opening with the same nil check
the existing `handleTelegramStart` uses, then `scope, _ := RequestScope(r)`
and a call taking `scope.UserID`. Response shapes:

```go
type telegramBindingResponse struct {
	Connected    bool       `json:"connected"`
	ChatUsername string     `json:"chatUsername,omitempty"`
	LinkedAt     *time.Time `json:"linkedAt,omitempty"`
}

type telegramLinkStartResponse struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Reason is the sentence the panel shows for a refused link. Empty for every
// other status, and never populated from a database error -- the service
// chooses it (see errors.go's mapping).
type telegramLinkStatusResponse struct {
	Status       string `json:"status"`
	ChatUsername string `json:"chatUsername,omitempty"`
	Reason       string `json:"reason,omitempty"`
}
```

`DELETE` returns `telegramBindingResponse{Connected: false}` with 200, not
204 — the panel re-renders from the body, and `apiFetch` would throw on an
unparseable ok response.

- [ ] **Step 4: Map the errors**

In `errors.go`, beside the existing conflict cases:

```go
	case errors.Is(err, domain.ErrTelegramChatTaken):
		WriteError(w, http.StatusConflict, "TELEGRAM_CHAT_TAKEN",
			"That Telegram chat is already connected to another Hearth account.", nil)
	case errors.Is(err, domain.ErrTelegramAlreadyLinked):
		WriteError(w, http.StatusConflict, "TELEGRAM_ALREADY_LINKED",
			"This account already has a Telegram chat. Disconnect it first.", nil)
	case errors.Is(err, domain.ErrTelegramLinkNotPending):
		WriteError(w, http.StatusConflict, "TELEGRAM_LINK_NOT_PENDING",
			"No Telegram chat has opened this link, or it expired. Start again.", nil)
	case errors.Is(err, domain.ErrTelegramUnlinkWouldLockOut):
		WriteError(w, http.StatusConflict, "TELEGRAM_UNLINK_LOCKOUT",
			"Add an email address to this account before disconnecting Telegram.", nil)
	case errors.Is(err, domain.ErrTelegramMintsRateLimited):
		WriteError(w, http.StatusTooManyRequests, "TELEGRAM_LINK_RATE_LIMITED",
			"Too many attempts. Try again in an hour.", nil)
```

- [ ] **Step 5: Route them**

In `router.go`, inside the `auth` group, after the existing Telegram group:

```go
			// Connecting a chat needs a browser session, not a token: a
			// leaked token must not be able to bind a channel that outlives
			// its own revocation (the same rule as minting a token, ADR 7).
			// One group covers the reads too -- requireCSRF returns early for
			// GET, HEAD and OPTIONS -- so the polling route needs no header.
			auth.Group(func(tl chi.Router) {
				tl.Use(requireSession(deps))
				tl.Use(requireFeature(deps, domain.FlagTelegramSignIn))
				tl.Use(requireCSRF)
				tl.Use(requireCookieSession)
				tl.Get("/telegram", handleTelegramBinding(deps))
				tl.Delete("/telegram", handleTelegramUnlink(deps))
				tl.Post("/telegram/link", handleTelegramLinkStart(deps))
				tl.Get("/telegram/link/{id}", handleTelegramLinkStatus(deps))
				tl.Post("/telegram/link/{id}/confirm", handleTelegramLinkConfirm(deps))
			})
```

- [ ] **Step 6: Add the routes to `hearthctl`'s table**

```go
	{"GET", "/auth/telegram", "browser session", "-"},
	{"DELETE", "/auth/telegram", "browser session+csrf", "-"},
	{"POST", "/auth/telegram/link", "browser session+csrf", "-"},
	{"GET", "/auth/telegram/link/{id}", "browser session", "-"},
	{"POST", "/auth/telegram/link/{id}/confirm", "browser session+csrf", "-"},
```

`routes_test.go` diffs this table against `router.go` **both ways**, so a
missing line here fails the build. This is not the CLI coverage the spec puts
out of scope — that is about wrapping the flow in a command, which stays
unbuilt.

- [ ] **Step 7: Wire it in `main.go`**

Construct `TelegramLinkService` in the same `if` that already constructs
`TelegramAuthService` when a bot token is configured, and assign
`deps.TelegramLink`. No bot means both stay nil and every route 404s.

- [ ] **Step 8: Run everything**

```bash
cd api && go test ./... 2>&1 | tail -10 && cd .. && make lint
```

Expected: PASS and a clean lint, `routes_test.go` included.

- [ ] **Step 9: Commit**

```bash
git add api
git commit -m "Routes for connecting and disconnecting a Telegram chat

Five routes behind session, CSRF, cookie-session and the telegram_sign_in
flag, 404 when no bot is configured. One group covers the reads because
requireCSRF exempts GET. hearthctl's routes table gains all five; its test
diffs that table against the router both ways."
```

---

### Task 7: The Settings panel

**Files:**
- Create: `web/src/features/settings/TelegramPanel.tsx`
- Create: `web/src/features/settings/TelegramPanel.test.tsx`
- Modify: `web/src/features/settings/schemas.ts`
- Modify: `web/src/features/settings/SettingsPage.tsx`

**Interfaces:**
- Consumes: Task 6's five routes.
- Produces: `<TelegramPanel />`.

- [ ] **Step 1: Add the schemas**

```ts
export const telegramBindingSchema = z.object({
  connected: z.boolean(),
  chatUsername: z.string().optional(),
  linkedAt: z.string().optional(),
});
export type TelegramBinding = z.infer<typeof telegramBindingSchema>;

export const telegramLinkStartSchema = z.object({
  id: z.string(),
  url: z.string(),
  expiresAt: z.string(),
});

// status is a closed set the server owns. Parsed as an enum rather than a
// string so an unknown value fails loudly here instead of rendering a panel
// with no branch taken.
export const telegramLinkStatusSchema = z.object({
  status: z.enum(["waiting", "pending", "connected", "refused", "expired"]),
  chatUsername: z.string().optional(),
  reason: z.string().optional(),
});
```

- [ ] **Step 2: Write the failing panel tests**

`TelegramPanel.test.tsx`, following `NotificationsPanel.test.tsx`'s harness:

```tsx
it("shows the connected chat and offers Disconnect", async () => { /* ... */ });

it("names the chat that opened the link before asking to confirm", async () => {
  // The point of the second phase: the person must be able to see it is
  // their own chat before they confirm.
});

it("stops polling and offers a fresh start when the link expires", async () => { /* ... */ });

it("shows the server's reason when the chat belongs to someone else", async () => { /* ... */ });
```

- [ ] **Step 3: Run and watch them fail**

```bash
cd web && npm test -- TelegramPanel 2>&1 | tail -20
```

Expected: FAIL — the module does not exist.

- [ ] **Step 4: Write the panel**

Three states off one `useQuery` on `/api/v1/auth/telegram`, plus a second
query on `/api/v1/auth/telegram/link/{id}` enabled only while a link id is
held, `refetchInterval: 3000` while the status is `waiting` or `pending` and
`false` otherwise. Connect opens the deep link with
`window.open(url, "_blank", "noopener")`. Confirm posts and invalidates the
binding query. Disconnect deletes and invalidates.

Header comment states what a reader cannot see:

```tsx
// The Telegram panel. Two phases, deliberately: the deep link only tells the
// bot which account is asking, and the binding is written when this panel --
// inside the member's own session -- confirms the chat that turned up. A
// leaked deep link therefore connects nobody. See
// docs/adr/0010-binding-a-chat-needs-a-confirm.md before simplifying this
// into a single click.
```

- [ ] **Step 5: Mount it and run the frontend checks**

Add `<TelegramPanel />` to `SettingsPage.tsx` beside `<NotificationsPanel />`.

```bash
cd web && npm test 2>&1 | tail -10 && npm run typecheck && npm run lint
```

Expected: PASS, clean.

- [ ] **Step 6: Commit**

```bash
git add web
git commit -m "Settings: connect and disconnect a Telegram chat

Three states, and the pending one names the chat that opened the link --
without that the confirm asks a question the person cannot check, which is
most of what the second phase is for."
```

---

### Task 8: Documentation and the browser walk

**Files:**
- Create: `docs/adr/0010-binding-a-chat-needs-a-confirm.md`
- Create: `docs/superpowers/plans/2026-09-09-telegram-account-linking-verification.md`
- Modify: `docs/SYSTEM_DESIGN.md` (§5, the Telegram channel)
- Modify: `docs/FEATURE_TRACKER.md` (the ⬜ row added 2026-09-09 → ✅)
- Modify: `docs/LEARNING.md`

- [ ] **Step 1: Write ADR 10**

Context: the binding existed only for accounts born in a chat. Decision: a
chat is bound only by a confirm inside the session that minted the nonce.
Consequences: two round trips and a polling panel; a leaked deep link
connects nobody; `chat_username` exists so the confirm can be verified;
anyone tempted to collapse this to one click is changing a security property,
not a UX detail. Follow the existing ADRs' shape.

- [ ] **Step 2: Update `SYSTEM_DESIGN.md`**

Use the `maintaining-system-design` skill. §5's Telegram flow gains the link
path and the new routes; the prose under the diagram says why the binding is
not written at `/start`. The `telegram_link_requests` and `telegram_accounts`
boxes gain their new columns.

- [ ] **Step 3: Run the browser walk**

Start the stack, sign in as an email account, and drive it with a real bot
(`@HearthOinkDevBot`; a shared production token makes the dev poller lose
every update — see the FEATURE_TRACKER entry for chat commands). Criteria,
each recorded pass or fail with what was seen:

1. Settings shows "Not connected" with a Connect button.
2. Connect opens Telegram on the bot with a `?start=` payload.
3. `/start` replies with the confirm instruction and **no** sign-up link.
4. `SELECT count(*) FROM households` is unchanged — the discriminating check:
   before this feature that `/start` would have minted a sign-up.
5. The panel moves to pending and names the chat.
6. Confirm writes the binding; the panel shows connected with a `linkedAt`.
7. `/balance` in that chat now answers, where it previously refused.
8. A second Connect attempt is refused with "already has a Telegram chat".
9. Disconnect removes the binding; `/balance` refuses again.
10. `/start` after disconnecting offers a sign-up link again — the account is
    a stranger to the bot once more.
11. A fourth `POST /auth/telegram/link` within the hour answers 429.
12. An expired link (wait past ten minutes, or move the clock) confirms
    nothing and the panel says so.

Record it in the verification file, with the caveats named. A criterion that
cannot be run — the stolen-link case needs a second Telegram account — is
recorded as "not walked by hand; covered by
`TestConfirmRefusesAnotherMembersLink`", not silently dropped.

- [ ] **Step 4: Update the tracker and `LEARNING.md`**

Move the ⬜ row added on 2026-09-09 to ✅ with what the walk showed, recount
the summary table by symbol (not by delta), and add the running note. Totals
become **95/18/16/5 = 134**, denominator **129** — confirm by counting.

`LEARNING.md` gains one entry: *a feature can be complete for the users who
arrived one way and invisible to everyone else*. The Telegram surface —
commands, free text, the digest — was built and walked while being reachable
only by accounts created from a chat, and nothing in the tests said so
because every test that needed a binding created one. What would have caught
it: asking, for each new capability behind an identity, *which existing
users can reach this today?*

- [ ] **Step 5: Final gate and commit**

```bash
make lint && make test
git add docs
git commit -m "Telegram linking: ADR 10, the walk, and the docs it moved"
```

- [ ] **Step 6: Open the PR**

```bash
git push -u origin telegram-account-linking
gh pr create --title "Link an existing Hearth account to a Telegram chat" --body "..."
```

Body: the gap, the two-phase decision and why one phase is unsafe, the walk's
criteria table, and the named gaps (the sibling ⬜ "attach an email address to
a Telegram-only account", `/nudges off` reset by a relink).

---

## Self-review

**Spec coverage.** Decision 1 → Tasks 4, 5, 7 and ADR 10 (Task 8). Decision 2
→ Task 4's matrix and Task 6's error mapping. Decision 3 → Task 1. Decision 4
→ Task 4 step 3 plus its mutation check. Decision 5 → Task 1's schema test
and Task 5's `Status`. Decision 6 → `Confirm`'s expiry check
(`TestConfirmRefusesAfterExpiry`). Decision 7 → Task 3 and Task 7 step 4.
Decision 8 → Task 6 step 5. Decision 9 → Task 5's doc comment, Task 6's
handlers. Decision 10 → no owner check anywhere, asserted by no test needing
one. Decision 11 → `Unlink` and its two tests. Decision 12 → `Start`'s cap
and `TestStartRefusesTheFourthMintInAnHour`. Errors table → Task 5 step 1 and
Task 6 step 4. Data → Task 1 step 1. API → Task 6. Ports → Tasks 1 and 2.
Testing → each task's own steps, browser walk in Task 8.

**Elided bodies.** Task 5 step 4 gives `Confirm` in full and states the
contract for `Start`, `Status` and `Unlink` in their doc comments with the
derivation order spelled out; Task 4's second and third tests name their
setup and assertion rather than repeating boilerplate. These are the only
places, and each names exactly what the code must do.

**Type consistency.** `TelegramLinkRedemption{ID, UserID}`,
`TelegramLinkRequest{ID, UserID, ChatID, ChatUsername, Consumed, ExpiresAt}`
and `TelegramBinding{UserID, ChatID, ChatUsername, LinkedAt}` are used with
those exact field names in Tasks 1, 2, 5, 6. `Status` returns the same five
strings the zod enum in Task 7 accepts. `Create(ctx, userID, nonceHash,
expiresAt)` and `Consume(ctx, nonceHash, chatID, chatUsername)` match between
port, repository, double and every call site named.
