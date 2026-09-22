# Household Access List (Partner Invite Lobby, Milestone 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One Access panel in Settings where an owner sees every live API token and linked Telegram chat in the household, and every member mints and revokes their own tokens without a terminal.

**Architecture:** One new cookie-only read route, `GET /api/v1/household/access`, served by a new `AccessListService` that reads through three narrow ports (tokens by household, chats by household, members for names). The HTTP handler picks household-wide (owner) or self-only (limited) — the service never sees who is asking. The frontend adds an `AccessPanel` with an API-token list (create + revoke over the existing `/auth/tokens` routes) and a linked-chats list that embeds the existing Telegram connect flow, renamed `TelegramConnection`.

**Tech Stack:** Go 1.25 (chi, pgx v5, sqlc, testcontainers), React + TypeScript (TanStack Query, zod, vitest, Testing Library), Postgres.

**Spec:** `docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md` — read it first; this plan argues from it.

## Global Constraints

- Clean architecture: `internal/domain` imports stdlib only; `internal/usecase` adds `internal/domain`; everything else is `internal/adapter/**` or `cmd/**`. `make lint-arch` enforces it, test files included.
- No service takes an actor parameter. Owner-vs-limited and cookie-vs-token are decided in `internal/adapter/http` only.
- A `switch` over a value from a request or a DB column has a `default` that refuses (fail closed).
- Every 2xx except 204 carries a JSON body. Empty lists are `[]`, never `null`.
- JSON field names are camelCase.
- The Telegram `chat_id` never appears in any response.
- No new write route. No migration.
- Before running `go` or `make`: `export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH`, `export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock`, `export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`. Run `go` commands from `api/`, `npx` commands from `web/`, `make` from the repo root.
- Branch: `partner-invite-lobby-m3` (already created; the spec is committed on it).
- Definition of done: `make lint && make test` green, the mutation checks in Task 8 run and recorded, the browser walk in Task 9 passed, docs updated.

## File Map

**Create**
- `api/internal/usecase/access_list.go` — `AccessListService`, its three narrow ports, its row types
- `api/internal/usecase/access_list_test.go` — service tests with inline doubles
- `api/internal/adapter/postgres/access_list_repo_test.go` — repo tests for both household listers
- `api/internal/adapter/http/access_handlers.go` — `handleHouseholdAccess` and its DTOs
- `api/internal/adapter/http/access_api_test.go` — end-to-end HTTP tests
- `web/src/features/settings/useHouseholdAccess.ts` — the access query + token create/revoke mutations
- `web/src/features/settings/TelegramConnection.tsx` (+ `.test.tsx`) — renamed from `TelegramPanel` (git mv)
- `web/src/features/settings/ApiTokenList.tsx` + `ApiTokenList.test.tsx`
- `web/src/features/settings/NewApiTokenModal.tsx` + `NewApiTokenModal.test.tsx`
- `web/src/features/settings/LinkedChatList.tsx`
- `web/src/features/settings/AccessPanel.tsx` + `AccessPanel.test.tsx`

**Modify**
- `api/internal/adapter/postgres/queries/api_token.sql`, `queries/telegram.sql` (+ regenerated `sqlcgen/`)
- `api/internal/adapter/postgres/api_token_repo.go`, `telegram_account_repo.go`
- `api/internal/adapter/http/router.go` (Deps field + route)
- `api/cmd/api/main.go` (wiring)
- `api/internal/adapter/http/api_test.go` (test env wiring + `telegramAccounts` field)
- `api/cmd/hearthctl/routes.go` (route table row)
- `web/src/features/settings/schemas.ts`, `copy.ts`, `useTelegram.ts`, `SettingsPage.tsx`, `MembersPanel.tsx` (one `id`)
- Docs in Task 8

---

### Task 1: Postgres — list live tokens and linked chats by household

**Files:**
- Modify: `api/internal/adapter/postgres/queries/api_token.sql`
- Modify: `api/internal/adapter/postgres/queries/telegram.sql`
- Modify: `api/internal/adapter/postgres/api_token_repo.go`
- Modify: `api/internal/adapter/postgres/telegram_account_repo.go`
- Regenerate: `api/internal/adapter/postgres/sqlcgen/` (`make sqlc`)
- Test: `api/internal/adapter/postgres/access_list_repo_test.go`

**Interfaces:**
- Produces: `(*APITokenRepo).ListForHousehold(ctx context.Context, householdID string) ([]domain.APIToken, error)` and `(*TelegramAccountRepo).ListForHousehold(ctx context.Context, householdID string) ([]usecase.TelegramBinding, error)`. Task 2 declares ports these satisfy.

- [ ] **Step 1: Write the failing repo tests**

Create `api/internal/adapter/postgres/access_list_repo_test.go`:

```go
package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// Household access list (docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md).
// These two listers are the only queries that read another member's
// credentials, so the household boundary is what they are tested for.

type accessFixture struct {
	db                 *postgres.DB
	householdID        string
	ownerID            string
	partnerID          string
	strangerID         string
	strangersHousehold string
}

func seedAccessFixture(t *testing.T) accessFixture {
	t.Helper()
	ctx := context.Background()
	db := openTestDB(t)
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	members := postgres.NewMembershipRepo(db)

	newHousehold := func(name string) domain.Household {
		h, err := households.Create(ctx, domain.Household{
			Name: name, FamilyName: "Test",
			PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
		})
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		return h
	}
	newOwner := func(h domain.Household, email, name string) string {
		u, err := users.Create(ctx, email, "", name)
		if err != nil {
			t.Fatalf("create user %s: %v", email, err)
		}
		if _, err := members.Create(ctx, domain.Membership{
			HouseholdID: h.ID, UserID: u.ID, Role: domain.RoleOwner,
			Capabilities: domain.Capabilities{domain.CapCalendar, domain.CapChores},
		}); err != nil {
			t.Fatalf("create membership %s: %v", email, err)
		}
		return u.ID
	}

	ours := newHousehold("Ours")
	theirs := newHousehold("Theirs")
	return accessFixture{
		db:                 db,
		householdID:        ours.ID,
		ownerID:            newOwner(ours, "owner@example.com", "Owner"),
		partnerID:          newOwner(ours, "partner@example.com", "Partner"),
		strangerID:         newOwner(theirs, "stranger@example.com", "Stranger"),
		strangersHousehold: theirs.ID,
	}
}

func mustToken(t *testing.T, repo *postgres.APITokenRepo, userID, householdID, name string, expiresAt time.Time) domain.APIToken {
	t.Helper()
	tok, err := repo.Create(context.Background(), []byte("hash-"+name), "pfx-"+name, domain.APIToken{
		UserID: userID, HouseholdID: householdID, Name: name, ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("create token %s: %v", name, err)
	}
	return tok
}

func tokenNames(tokens []domain.APIToken) []string {
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		out = append(out, tok.Name)
	}
	return out
}

func TestListTokensForHouseholdShowsEveryMembersLiveTokensAndNoOtherHouseholds(t *testing.T) {
	f := seedAccessFixture(t)
	repo := postgres.NewAPITokenRepo(f.db)
	week := time.Now().Add(7 * 24 * time.Hour)

	mustToken(t, repo, f.ownerID, f.householdID, "owner-laptop", week)
	mustToken(t, repo, f.partnerID, f.householdID, "partner-script", week)
	mustToken(t, repo, f.strangerID, f.strangersHousehold, "stranger-token", week)

	got, err := repo.ListForHousehold(context.Background(), f.householdID)
	if err != nil {
		t.Fatalf("ListForHousehold() = %v", err)
	}
	names := tokenNames(got)
	if len(names) != 2 || names[0] != "partner-script" || names[1] != "owner-laptop" {
		t.Fatalf("ListForHousehold() names = %v, want [partner-script owner-laptop] (newest first, ours only)", names)
	}
}

func TestListTokensForHouseholdLeavesOutRevokedAndExpiredTokens(t *testing.T) {
	f := seedAccessFixture(t)
	ctx := context.Background()
	repo := postgres.NewAPITokenRepo(f.db)

	mustToken(t, repo, f.ownerID, f.householdID, "live", time.Now().Add(time.Hour))
	mustToken(t, repo, f.ownerID, f.householdID, "expired", time.Now().Add(-time.Minute))
	revoked := mustToken(t, repo, f.ownerID, f.householdID, "revoked", time.Now().Add(time.Hour))
	if err := repo.Revoke(ctx, f.ownerID, revoked.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	got, err := repo.ListForHousehold(ctx, f.householdID)
	if err != nil {
		t.Fatalf("ListForHousehold() = %v", err)
	}
	if names := tokenNames(got); len(names) != 1 || names[0] != "live" {
		t.Fatalf("ListForHousehold() names = %v, want [live]", names)
	}
}

func TestListTokensForHouseholdWithNoneIsAnEmptySliceNotAnError(t *testing.T) {
	f := seedAccessFixture(t)
	got, err := postgres.NewAPITokenRepo(f.db).ListForHousehold(context.Background(), f.householdID)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("ListForHousehold() = %#v, %v; want empty non-nil slice, nil", got, err)
	}
}

func TestListChatsForHouseholdShowsOnlyThisHouseholdsChats(t *testing.T) {
	f := seedAccessFixture(t)
	ctx := context.Background()
	repo := postgres.NewTelegramAccountRepo(f.db)

	for _, b := range []usecase.TelegramBinding{
		{UserID: f.ownerID, ChatID: 1001, ChatUsername: "owner_tg"},
		{UserID: f.partnerID, ChatID: 1002},
		{UserID: f.strangerID, ChatID: 1003, ChatUsername: "stranger_tg"},
	} {
		if err := repo.Create(ctx, b); err != nil {
			t.Fatalf("bind %s: %v", b.UserID, err)
		}
	}

	got, err := repo.ListForHousehold(ctx, f.householdID)
	if err != nil {
		t.Fatalf("ListForHousehold() = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListForHousehold() = %+v, want the owner's and the partner's chats only", got)
	}
	byUser := map[string]usecase.TelegramBinding{}
	for _, b := range got {
		byUser[b.UserID] = b
	}
	if byUser[f.ownerID].ChatUsername != "owner_tg" {
		t.Fatalf("owner's chat = %+v", byUser[f.ownerID])
	}
	if b, ok := byUser[f.partnerID]; !ok || b.ChatUsername != "" || b.LinkedAt.IsZero() {
		t.Fatalf("partner's chat = %+v, want present, no username, a linked-at time", b)
	}
	if _, ok := byUser[f.strangerID]; ok {
		t.Fatal("another household's chat is listed")
	}
}

func TestListChatsForHouseholdWithNoneIsAnEmptySliceNotAnError(t *testing.T) {
	f := seedAccessFixture(t)
	got, err := postgres.NewTelegramAccountRepo(f.db).ListForHousehold(context.Background(), f.householdID)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("ListForHousehold() = %#v, %v; want empty non-nil slice, nil", got, err)
	}
}
```

The household and user literals follow `TestMembershipRepoRejectsAnInvalidCapabilitySet` in `repos_test.go`, the known-good fixture.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/adapter/postgres/ -run 'ListTokensForHousehold|ListChatsForHousehold' -count=1`
Expected: FAIL — compile error, `repo.ListForHousehold undefined`.

- [ ] **Step 3: Add the two queries**

Append to `api/internal/adapter/postgres/queries/api_token.sql`:

```sql
-- The household access list: every member's LIVE tokens. Unlike
-- ListAPITokensForUser this also leaves out expired rows -- the list answers
-- "what can get in right now", and an expired token cannot.
-- name: ListLiveAPITokensForHousehold :many
SELECT * FROM api_tokens
WHERE household_id = $1 AND revoked_at IS NULL AND expires_at > now()
ORDER BY created_at DESC;
```

Append to `api/internal/adapter/postgres/queries/telegram.sql`:

```sql
-- The household access list's chats. telegram_accounts carries only
-- user_id, so the household boundary comes from memberships. chat_id is
-- selected because TelegramBinding has it; the HTTP layer never sends it.
-- name: ListTelegramAccountsForHousehold :many
SELECT ta.user_id, ta.chat_id, ta.chat_username, ta.linked_at
FROM telegram_accounts ta
JOIN memberships m ON m.user_id = ta.user_id
WHERE m.household_id = $1
ORDER BY ta.linked_at DESC;
```

Run: `make sqlc`. Expected: the two generated functions appear in `sqlcgen/api_token.sql.go` and `sqlcgen/telegram.sql.go`.

- [ ] **Step 4: Add the repo methods**

In `api/internal/adapter/postgres/api_token_repo.go`, after `ListForUser`:

```go
// ListForHousehold returns every member's live tokens -- not revoked, not
// expired -- newest first. It is the only token query that crosses members,
// so its WHERE clause is the household boundary; see the query's comment.
func (r *APITokenRepo) ListForHousehold(ctx context.Context, householdID string) ([]domain.APIToken, error) {
	rows, err := r.q.ListLiveAPITokensForHousehold(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list household api tokens")
	}
	out := make([]domain.APIToken, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAPIToken(row))
	}
	return out, nil
}
```

In `api/internal/adapter/postgres/telegram_account_repo.go`, after `ByUserID`:

```go
// ListForHousehold returns every chat bound to a member of this household,
// most recently linked first. Empty is an empty slice, never ErrNotFound.
func (r *TelegramAccountRepo) ListForHousehold(ctx context.Context, householdID string) ([]usecase.TelegramBinding, error) {
	rows, err := r.q.ListTelegramAccountsForHousehold(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list household telegram accounts")
	}
	out := make([]usecase.TelegramBinding, 0, len(rows))
	for _, row := range rows {
		out = append(out, usecase.TelegramBinding{
			UserID:       uuidToString(row.UserID),
			ChatID:       row.ChatID,
			ChatUsername: stringOrEmpty(row.ChatUsername),
			LinkedAt:     timeOf(row.LinkedAt),
		})
	}
	return out, nil
}
```

The generated row type's field names come from the SELECT list (`UserID`, `ChatID`, `ChatUsername`, `LinkedAt`); confirm in `sqlcgen/telegram.sql.go`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/adapter/postgres/ -run 'ListTokensForHousehold|ListChatsForHousehold' -count=1 -v`
Expected: PASS, 5 tests.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/postgres/
git commit -m "feat(api): list a household's live API tokens and linked chats"
```

---

### Task 2: Usecase — `AccessListService`

**Files:**
- Create: `api/internal/usecase/access_list.go`
- Test: `api/internal/usecase/access_list_test.go`
- Modify: `api/internal/adapter/postgres/api_token_repo.go`, `telegram_account_repo.go` (compile-time port checks)

**Interfaces:**
- Consumes: Task 1's two repo methods (through the ports declared here); the existing `usecase.MembershipRepository` (satisfies `MemberLister`).
- Produces (Task 3 uses these exact names):

```go
type HouseholdTokenLister interface { ListForHousehold(ctx context.Context, householdID string) ([]domain.APIToken, error) }
type HouseholdChatLister  interface { ListForHousehold(ctx context.Context, householdID string) ([]TelegramBinding, error) }
type MemberLister         interface { List(ctx context.Context, householdID string) ([]MemberView, error) }
type AccessListDeps struct { Tokens HouseholdTokenLister; Chats HouseholdChatLister; Members MemberLister }
type AccessToken struct { Token domain.APIToken; MemberName string }
type AccessChat  struct { UserID, MemberName, ChatUsername string; LinkedAt time.Time }
type AccessList  struct { Tokens []AccessToken; Chats []AccessChat }
func NewAccessListService(d AccessListDeps) *AccessListService
func (s *AccessListService) ForHousehold(ctx context.Context, householdID string, withChats bool) (AccessList, error)
func (s *AccessListService) ForMember(ctx context.Context, householdID, userID string, withChats bool) (AccessList, error)
```

- [ ] **Step 1: Write the failing tests**

Create `api/internal/usecase/access_list_test.go`:

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

// The three ports are one method each, so each double is a func.

type tokenListerFunc func(ctx context.Context, householdID string) ([]domain.APIToken, error)

func (f tokenListerFunc) ListForHousehold(ctx context.Context, h string) ([]domain.APIToken, error) {
	return f(ctx, h)
}

type chatListerFunc func(ctx context.Context, householdID string) ([]usecase.TelegramBinding, error)

func (f chatListerFunc) ListForHousehold(ctx context.Context, h string) ([]usecase.TelegramBinding, error) {
	return f(ctx, h)
}

type memberListerFunc func(ctx context.Context, householdID string) ([]usecase.MemberView, error)

func (f memberListerFunc) List(ctx context.Context, h string) ([]usecase.MemberView, error) {
	return f(ctx, h)
}

func accessMember(userID, name string) usecase.MemberView {
	return usecase.MemberView{
		Membership: domain.Membership{UserID: userID, HouseholdID: "h1"},
		User:       domain.User{ID: userID, DisplayName: name},
	}
}

// accessService builds the service over a household of Alex and Sam, each
// with one token and one chat, plus a token belonging to someone who is no
// longer a member.
func accessService(t *testing.T, chatsCalled *bool) *usecase.AccessListService {
	t.Helper()
	linked := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	return usecase.NewAccessListService(usecase.AccessListDeps{
		Members: memberListerFunc(func(context.Context, string) ([]usecase.MemberView, error) {
			return []usecase.MemberView{accessMember("u-alex", "Alex"), accessMember("u-sam", "Sam")}, nil
		}),
		Tokens: tokenListerFunc(func(context.Context, string) ([]domain.APIToken, error) {
			return []domain.APIToken{
				{ID: "t-sam", UserID: "u-sam", Name: "sam script"},
				{ID: "t-alex", UserID: "u-alex", Name: "alex laptop"},
				{ID: "t-gone", UserID: "u-gone", Name: "left behind"},
			}, nil
		}),
		Chats: chatListerFunc(func(context.Context, string) ([]usecase.TelegramBinding, error) {
			if chatsCalled != nil {
				*chatsCalled = true
			}
			return []usecase.TelegramBinding{
				{UserID: "u-sam", ChatID: 2, ChatUsername: "sam_k", LinkedAt: linked},
				{UserID: "u-alex", ChatID: 1, LinkedAt: linked},
			}, nil
		}),
	})
}

func TestForHouseholdListsEveryMembersTokensAndChatsWithTheirNames(t *testing.T) {
	got, err := accessService(t, nil).ForHousehold(context.Background(), "h1", true)
	if err != nil {
		t.Fatalf("ForHousehold() = %v", err)
	}
	if len(got.Tokens) != 2 || got.Tokens[0].MemberName != "Sam" || got.Tokens[1].MemberName != "Alex" {
		t.Fatalf("tokens = %+v, want Sam's then Alex's, order kept", got.Tokens)
	}
	if len(got.Chats) != 2 || got.Chats[0].MemberName != "Sam" || got.Chats[0].ChatUsername != "sam_k" {
		t.Fatalf("chats = %+v", got.Chats)
	}
}

// A token whose user is no longer a member has no name to show and no
// business being on the list. Dropping it is the fail-closed choice.
func TestForHouseholdDropsRowsWhoseUserIsNotACurrentMember(t *testing.T) {
	got, _ := accessService(t, nil).ForHousehold(context.Background(), "h1", true)
	for _, tok := range got.Tokens {
		if tok.Token.ID == "t-gone" {
			t.Fatal("a non-member's token is listed")
		}
	}
}

func TestForMemberReturnsOnlyThatMembersTokensAndChat(t *testing.T) {
	got, err := accessService(t, nil).ForMember(context.Background(), "h1", "u-alex", true)
	if err != nil {
		t.Fatalf("ForMember() = %v", err)
	}
	if len(got.Tokens) != 1 || got.Tokens[0].Token.ID != "t-alex" {
		t.Fatalf("tokens = %+v, want Alex's only", got.Tokens)
	}
	if len(got.Chats) != 1 || got.Chats[0].UserID != "u-alex" {
		t.Fatalf("chats = %+v, want Alex's only", got.Chats)
	}
}

func TestWithoutChatsTheChatListerIsNeverAsked(t *testing.T) {
	called := false
	got, err := accessService(t, &called).ForHousehold(context.Background(), "h1", false)
	if err != nil {
		t.Fatalf("ForHousehold() = %v", err)
	}
	if called {
		t.Fatal("the chat lister was asked although chats are off")
	}
	if got.Chats == nil || len(got.Chats) != 0 {
		t.Fatalf("chats = %#v, want an empty non-nil slice", got.Chats)
	}
}

func TestAnAccessListRepositoryFailureIsReturnedNotSwallowed(t *testing.T) {
	boom := errors.New("boom")
	svc := usecase.NewAccessListService(usecase.AccessListDeps{
		Members: memberListerFunc(func(context.Context, string) ([]usecase.MemberView, error) { return nil, nil }),
		Tokens:  tokenListerFunc(func(context.Context, string) ([]domain.APIToken, error) { return nil, boom }),
		Chats:   chatListerFunc(func(context.Context, string) ([]usecase.TelegramBinding, error) { return nil, nil }),
	})
	if _, err := svc.ForHousehold(context.Background(), "h1", true); !errors.Is(err, boom) {
		t.Fatalf("ForHousehold() error = %v, want boom", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/usecase/ -run 'ForHousehold|ForMember|WithoutChats|AccessListRepositoryFailure' -count=1`
Expected: FAIL — `undefined: usecase.NewAccessListService`.

- [ ] **Step 3: Write the service**

Create `api/internal/usecase/access_list.go`:

```go
package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// The household access list (partner invite lobby, milestone 3): every live
// way into a household that is not a password -- API tokens and linked
// Telegram chats. docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md.
//
// This service takes no actor. Whether the caller sees the whole household
// (an owner) or only their own rows (a limited member) is decided by the
// HTTP handler, which picks ForHousehold or ForMember (ADR 8).

// HouseholdTokenLister returns a household's live API tokens -- not revoked,
// not expired -- newest first, and never another household's. Empty is an
// empty slice, not domain.ErrNotFound. Its own narrow port rather than a
// method on APITokenRepository, so the doubles of that wider port do not
// have to grow a method they never use.
type HouseholdTokenLister interface {
	ListForHousehold(ctx context.Context, householdID string) ([]domain.APIToken, error)
}

// HouseholdChatLister returns every Telegram chat bound to a member of the
// household, most recently linked first. Empty is an empty slice.
type HouseholdChatLister interface {
	ListForHousehold(ctx context.Context, householdID string) ([]TelegramBinding, error)
}

// MemberLister is the one MembershipRepository method this service needs:
// the display name to label each row with.
type MemberLister interface {
	List(ctx context.Context, householdID string) ([]MemberView, error)
}

type AccessListDeps struct {
	Tokens  HouseholdTokenLister
	Chats   HouseholdChatLister
	Members MemberLister
}

type AccessListService struct{ d AccessListDeps }

func NewAccessListService(d AccessListDeps) *AccessListService { return &AccessListService{d: d} }

// AccessToken is one token row, labelled with whose it is.
type AccessToken struct {
	Token      domain.APIToken
	MemberName string
}

// AccessChat is one linked chat, labelled with whose it is. It carries no
// chat id: nothing on the access list needs one, so it stops here.
type AccessChat struct {
	UserID       string
	MemberName   string
	ChatUsername string
	LinkedAt     time.Time
}

// AccessList is the whole answer. Both slices are non-nil, so the HTTP
// layer sends [] rather than null.
type AccessList struct {
	Tokens []AccessToken
	Chats  []AccessChat
}

// ForHousehold is every member's rows. withChats false skips the chat query
// entirely -- the caller passes false when Telegram is off for this
// household or no bot is configured.
func (s *AccessListService) ForHousehold(ctx context.Context, householdID string, withChats bool) (AccessList, error) {
	return s.list(ctx, householdID, "", withChats)
}

// ForMember is ForHousehold narrowed to one member's own rows. It filters
// the same household query rather than running a second one, so both paths
// share one query and one set of repository tests.
func (s *AccessListService) ForMember(ctx context.Context, householdID, userID string, withChats bool) (AccessList, error) {
	return s.list(ctx, householdID, userID, withChats)
}

// list does the work. onlyUserID "" means everyone.
func (s *AccessListService) list(ctx context.Context, householdID, onlyUserID string, withChats bool) (AccessList, error) {
	members, err := s.d.Members.List(ctx, householdID)
	if err != nil {
		return AccessList{}, err
	}
	names := make(map[string]string, len(members))
	for _, m := range members {
		names[m.User.ID] = m.User.DisplayName
	}
	// A row whose user is not a current member is dropped: there is no name
	// to show and no reason for it to be here (fail closed).
	include := func(userID string) (string, bool) {
		name, isMember := names[userID]
		if !isMember {
			return "", false
		}
		if onlyUserID != "" && userID != onlyUserID {
			return "", false
		}
		return name, true
	}

	tokens, err := s.d.Tokens.ListForHousehold(ctx, householdID)
	if err != nil {
		return AccessList{}, err
	}
	out := AccessList{Tokens: []AccessToken{}, Chats: []AccessChat{}}
	for _, tok := range tokens {
		if name, ok := include(tok.UserID); ok {
			out.Tokens = append(out.Tokens, AccessToken{Token: tok, MemberName: name})
		}
	}

	if !withChats {
		return out, nil
	}
	chats, err := s.d.Chats.ListForHousehold(ctx, householdID)
	if err != nil {
		return AccessList{}, err
	}
	for _, c := range chats {
		if name, ok := include(c.UserID); ok {
			out.Chats = append(out.Chats, AccessChat{
				UserID: c.UserID, MemberName: name, ChatUsername: c.ChatUsername, LinkedAt: c.LinkedAt,
			})
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Add compile-time port checks**

In `api/internal/adapter/postgres/api_token_repo.go`, add the `usecase` import and, under the imports:

```go
var _ usecase.HouseholdTokenLister = (*APITokenRepo)(nil)
```

In `api/internal/adapter/postgres/telegram_account_repo.go`, beside the existing `var _ usecase.TelegramAccountRepository = ...`:

```go
var _ usecase.HouseholdChatLister = (*TelegramAccountRepo)(nil)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/usecase/ -run 'ForHousehold|ForMember|WithoutChats|AccessListRepositoryFailure' -count=1 -v`
Expected: builds; PASS, 5 tests.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/access_list.go api/internal/usecase/access_list_test.go api/internal/adapter/postgres/api_token_repo.go api/internal/adapter/postgres/telegram_account_repo.go
git commit -m "feat(api): AccessListService assembles the household access list"
```

---

### Task 3: HTTP — `GET /api/v1/household/access`

**Files:**
- Create: `api/internal/adapter/http/access_handlers.go`
- Modify: `api/internal/adapter/http/router.go` (Deps field `Access`, route)
- Modify: `api/cmd/api/main.go` (wiring)
- Modify: `api/internal/adapter/http/api_test.go` (wiring + `telegramAccounts` field)
- Modify: `api/cmd/hearthctl/routes.go` (table row)
- Test: `api/internal/adapter/http/access_api_test.go`

**Interfaces:**
- Consumes: Task 2's `usecase.NewAccessListService`, `ForHousehold`, `ForMember`, `AccessList`.
- Produces the wire shape Task 4 parses:

```json
{ "telegramEnabled": true,
  "tokens": [{ "id", "memberId", "memberName", "name", "prefix", "createdAt", "expiresAt", "lastUsedAt": null | "..." }],
  "chats":  [{ "memberId", "memberName", "chatUsername": "" | "...", "linkedAt" }] }
```

- [ ] **Step 1: Add the Deps field so the tests can compile against it**

In `api/internal/adapter/http/router.go`, in `type Deps struct`, after `TelegramLink`:

```go
	// Access serves the household access list (GET /household/access).
	Access *usecase.AccessListService
```

- [ ] **Step 2: Wire the test env**

In `api/internal/adapter/http/api_test.go`:
1. Add a field to `testEnv`, next to `apiTokens`:
```go
	// telegramAccounts lets a test bind a chat directly: binding one through
	// the API needs a real bot round trip the test env does not have.
	telegramAccounts usecase.TelegramAccountRepository
```
2. After `apiTokenSvc := ...`:
```go
	accessSvc := usecase.NewAccessListService(usecase.AccessListDeps{
		Tokens: apiTokens, Chats: telegramAccounts, Members: memberships,
	})
```
3. In `deps := httpadapter.Deps{...}` add `Access: accessSvc,`.
4. In `env := &testEnv{...}` add `telegramAccounts: telegramAccounts,`.

- [ ] **Step 3: Write the failing API tests**

Create `api/internal/adapter/http/access_api_test.go`:

```go
package httpadapter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// Household access list: docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md.

type accessBody struct {
	TelegramEnabled bool `json:"telegramEnabled"`
	Tokens          []struct {
		ID         string     `json:"id"`
		MemberID   string     `json:"memberId"`
		MemberName string     `json:"memberName"`
		Name       string     `json:"name"`
		Prefix     string     `json:"prefix"`
		CreatedAt  time.Time  `json:"createdAt"`
		ExpiresAt  time.Time  `json:"expiresAt"`
		LastUsedAt *time.Time `json:"lastUsedAt"`
	} `json:"tokens"`
	Chats []struct {
		MemberID     string    `json:"memberId"`
		MemberName   string    `json:"memberName"`
		ChatUsername string    `json:"chatUsername"`
		LinkedAt     time.Time `json:"linkedAt"`
	} `json:"chats"`
}

const accessURL = "/api/v1/household/access"

// withBot rebuilds the router with a non-nil TelegramLink: the test env has
// no bot, and "no bot configured" is exactly what hides the chats. Only the
// nil check is exercised, so the service's own deps can stay empty.
func (env *testEnv) withBot() {
	d := env.deps
	d.TelegramLink = usecase.NewTelegramLinkService(usecase.TelegramLinkDeps{})
	env.deps = d
	env.router = httpadapter.NewRouter(d)
}

func (env *testEnv) userIDFor(t *testing.T, email string) string {
	t.Helper()
	u, err := env.users.ByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("look up %s: %v", email, err)
	}
	return u.ID
}

func (env *testEnv) bindChat(t *testing.T, email string, chatID int64, username string) {
	t.Helper()
	if err := env.telegramAccounts.Create(context.Background(), usecase.TelegramBinding{
		UserID: env.userIDFor(t, email), ChatID: chatID, ChatUsername: username,
	}); err != nil {
		t.Fatalf("bind chat for %s: %v", email, err)
	}
}

func (env *testEnv) access(t *testing.T, session *http.Cookie) (accessBody, string) {
	t.Helper()
	rec := env.authedGet(t, accessURL, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET access: %d %s", rec.Code, rec.Body.String())
	}
	var body accessBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode access: %v %s", err, rec.Body.String())
	}
	return body, rec.Body.String()
}

func TestAnOwnerSeesEveryMembersTokensAndChats(t *testing.T) {
	env := newTestEnv(t)
	env.withBot()
	ownerSession, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	limitedSession, limitedCSRF := env.signIn(t, env.limitedEmail, env.limitedPassword)
	env.mustCreateToken(t, ownerSession, ownerCSRF, "owner-laptop")
	env.mustCreateToken(t, limitedSession, limitedCSRF, "ethan-script")
	env.bindChat(t, env.limitedEmail, 7001, "ethan_tg")

	body, _ := env.access(t, ownerSession)

	if !body.TelegramEnabled {
		t.Fatal("telegramEnabled = false with a bot configured and the flag on")
	}
	names := map[string]string{}
	for _, tok := range body.Tokens {
		names[tok.Name] = tok.MemberName
	}
	if names["owner-laptop"] == "" || names["ethan-script"] != "Ethan" {
		t.Fatalf("tokens = %+v, want both members' tokens labelled", body.Tokens)
	}
	if len(body.Chats) != 1 || body.Chats[0].MemberName != "Ethan" || body.Chats[0].ChatUsername != "ethan_tg" {
		t.Fatalf("chats = %+v, want Ethan's chat", body.Chats)
	}
}

// PRD 15. The owner has a token and a chat too, so a handler that served
// the household view to everyone fails this test rather than passing it by
// accident.
func TestALimitedMemberSeesOnlyTheirOwnTokensAndChat(t *testing.T) {
	env := newTestEnv(t)
	env.withBot()
	ownerSession, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	limitedSession, limitedCSRF := env.signIn(t, env.limitedEmail, env.limitedPassword)
	env.mustCreateToken(t, ownerSession, ownerCSRF, "owner-laptop")
	env.mustCreateToken(t, limitedSession, limitedCSRF, "ethan-script")
	env.bindChat(t, env.ownerEmail, 7000, "owner_tg")
	env.bindChat(t, env.limitedEmail, 7001, "ethan_tg")

	body, _ := env.access(t, limitedSession)

	ethan := env.userIDFor(t, env.limitedEmail)
	if len(body.Tokens) != 1 || body.Tokens[0].Name != "ethan-script" || body.Tokens[0].MemberID != ethan {
		t.Fatalf("tokens = %+v, want only Ethan's own", body.Tokens)
	}
	if len(body.Chats) != 1 || body.Chats[0].MemberID != ethan {
		t.Fatalf("chats = %+v, want only Ethan's own", body.Chats)
	}
}

// Decision 5: a leaked token must not be able to map the household's other
// credentials.
func TestTheAccessListRefusesAnAPIToken(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	tok := env.mustCreateToken(t, session, csrf, "claude")

	rec := env.bearer(t, http.MethodGet, accessURL, nil, tok.Token)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "SESSION_REQUIRED") {
		t.Fatalf("GET access with a token: %d %s, want 403 SESSION_REQUIRED", rec.Code, rec.Body.String())
	}
}

func TestTheAccessListNeedsASignIn(t *testing.T) {
	env := newTestEnv(t)
	if rec := env.do(http.MethodGet, accessURL, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET access signed out: %d %s", rec.Code, rec.Body.String())
	}
}

func TestWithTelegramOffTheAccessListHasNoChats(t *testing.T) {
	env := newTestEnv(t)
	env.withBot()
	if err := env.featureFlags.SetGlobal(context.Background(), string(domain.FlagTelegramSignIn), false, ""); err != nil {
		t.Fatalf("turn telegram off: %v", err)
	}
	env.bindChat(t, env.ownerEmail, 7000, "owner_tg")
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	body, raw := env.access(t, session)
	if body.TelegramEnabled || len(body.Chats) != 0 || !strings.Contains(raw, `"chats":[]`) {
		t.Fatalf("with the flag off: %s, want telegramEnabled false and chats []", raw)
	}
}

func TestWithNoBotConfiguredTheAccessListHasNoChats(t *testing.T) {
	env := newTestEnv(t) // no withBot(): deps.TelegramLink is nil
	env.bindChat(t, env.ownerEmail, 7000, "owner_tg")
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	body, _ := env.access(t, session)
	if body.TelegramEnabled || len(body.Chats) != 0 {
		t.Fatalf("with no bot: %+v, want telegramEnabled false and no chats", body)
	}
}

// Decision 9: the chat id is a Telegram identifier the list does not need.
func TestTheAccessListNeverSendsAChatID(t *testing.T) {
	env := newTestEnv(t)
	env.withBot()
	env.bindChat(t, env.ownerEmail, 987654321, "owner_tg")
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	_, raw := env.access(t, session)
	if strings.Contains(raw, "987654321") || strings.Contains(strings.ToLower(raw), "chatid") {
		t.Fatalf("access body leaks the chat id: %s", raw)
	}
}

func TestAnEmptyAccessListAnswersEmptyListsNotNull(t *testing.T) {
	env := newTestEnv(t)
	env.withBot()
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	_, raw := env.access(t, session)
	if !strings.Contains(raw, `"tokens":[]`) || !strings.Contains(raw, `"chats":[]`) {
		t.Fatalf("empty access body = %s, want [] for both lists", raw)
	}
}

// Decision 6: seeing a partner's token is not the power to revoke it. The
// existing DELETE is scoped to the token's owner, and this pins it.
func TestAnOwnerCannotRevokeAnotherMembersToken(t *testing.T) {
	env := newTestEnv(t)
	ownerSession, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	limitedSession, limitedCSRF := env.signIn(t, env.limitedEmail, env.limitedPassword)
	ethans := env.mustCreateToken(t, limitedSession, limitedCSRF, "ethan-script")

	rec := env.authed(t, http.MethodDelete, "/api/v1/auth/tokens/"+ethans.ID, nil, ownerSession, ownerCSRF)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("owner revoking Ethan's token: %d %s, want 404", rec.Code, rec.Body.String())
	}
	if rec := env.bearer(t, http.MethodGet, "/api/v1/auth/me", nil, ethans.Token); rec.Code != http.StatusOK {
		t.Fatalf("Ethan's token after the refused revoke: %d, want it still working", rec.Code)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/adapter/http/ -run 'AccessList|EveryMembersTokensAndChats|OnlyTheirOwnTokensAndChat|RevokeAnotherMembersToken' -count=1`
Expected: FAIL — the route does not exist yet, so `GET access` answers 404 (`GET access: 404`).

- [ ] **Step 5: Write the handler**

Create `api/internal/adapter/http/access_handlers.go`:

```go
package httpadapter

import (
	"net/http"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// The household access list: every live API token and linked Telegram chat,
// each labelled with its member. docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md.
// Revoking reuses DELETE /auth/tokens/{id} and DELETE /auth/telegram, both
// already scoped to the caller's own rows.

type accessTokenDTO struct {
	ID         string     `json:"id"`
	MemberID   string     `json:"memberId"`
	MemberName string     `json:"memberName"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

// accessChatDTO has no chat id on purpose (spec decision 9).
type accessChatDTO struct {
	MemberID     string    `json:"memberId"`
	MemberName   string    `json:"memberName"`
	ChatUsername string    `json:"chatUsername"`
	LinkedAt     time.Time `json:"linkedAt"`
}

type accessResponse struct {
	TelegramEnabled bool             `json:"telegramEnabled"`
	Tokens          []accessTokenDTO `json:"tokens"`
	Chats           []accessChatDTO  `json:"chats"`
}

func handleHouseholdAccess(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		// Chats only when this household has Telegram on AND a bot exists --
		// the same two conditions that decide whether the Telegram routes
		// answer at all (requireFeature, and handleTelegramBinding's nil check).
		withChats := deps.TelegramLink != nil && scope.Flags.Enabled(domain.FlagTelegramSignIn)

		// Who sees what is decided here, not in the service (ADR 8). The
		// switch refuses any role it does not know: a new role must be given
		// a rule here before it can see anything.
		var list usecase.AccessList
		var err error
		switch scope.Membership.Role {
		case domain.RoleOwner:
			list, err = deps.Access.ForHousehold(r.Context(), scope.HouseholdID, withChats)
		case domain.RoleLimited:
			list, err = deps.Access.ForMember(r.Context(), scope.HouseholdID, scope.UserID, withChats)
		default:
			WriteError(w, http.StatusForbidden, "FORBIDDEN", "You can't see this household's access list.", nil)
			return
		}
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		out := accessResponse{
			TelegramEnabled: withChats,
			Tokens:          make([]accessTokenDTO, 0, len(list.Tokens)),
			Chats:           make([]accessChatDTO, 0, len(list.Chats)),
		}
		for _, row := range list.Tokens {
			t := row.Token
			out.Tokens = append(out.Tokens, accessTokenDTO{
				ID: t.ID, MemberID: t.UserID, MemberName: row.MemberName, Name: t.Name, Prefix: t.Prefix,
				CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt,
			})
		}
		for _, c := range list.Chats {
			out.Chats = append(out.Chats, accessChatDTO{
				MemberID: c.UserID, MemberName: c.MemberName, ChatUsername: c.ChatUsername, LinkedAt: c.LinkedAt,
			})
		}
		WriteJSON(w, http.StatusOK, out)
	}
}
```

- [ ] **Step 6: Add the route**

In `api/internal/adapter/http/router.go`, inside the `api.Group` that uses `requireSession(deps)`, directly after the owner-only `/household/invites` group:

```go
			// The household access list. Any member may read it -- the
			// handler narrows a limited member to their own rows -- but only
			// from a browser session: a leaked token that could list every
			// member's token prefixes and chats would hand an attacker a map
			// of the household's other credentials (spec decision 5). A GET,
			// so no CSRF guard.
			g.Group(func(a chi.Router) {
				a.Use(requireCookieSession)
				a.Get("/household/access", handleHouseholdAccess(deps))
			})
```

- [ ] **Step 7: Wire it in `main.go`**

In `api/cmd/api/main.go`, after `apiTokenSvc := ...`:

```go
	accessSvc := usecase.NewAccessListService(usecase.AccessListDeps{
		Tokens: apiTokens, Chats: telegramAccounts, Members: memberships,
	})
```

and in the `httpadapter.Deps{...}` literal add `Access: accessSvc,` beside `TelegramLink:`.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/adapter/http/ -run 'AccessList|EveryMembersTokensAndChats|OnlyTheirOwnTokensAndChat|RevokeAnotherMembersToken' -count=1 -v`
Expected: PASS, 9 tests.

- [ ] **Step 9: Add the hearthctl route row**

In `api/cmd/hearthctl/routes.go`, `routeTable`, after the `{"GET", "/household/invites", ...}` line:

```go
	{"GET", "/household/access", "browser session (a token cannot read it)", "-> {telegramEnabled,tokens,chats}; an owner sees every member, a limited member only themselves"},
```

Run: `go test ./cmd/hearthctl/ -count=1`
Expected: PASS (the table-vs-router test is green).

- [ ] **Step 10: Commit**

```bash
git add api/internal/adapter/http/ api/cmd/api/main.go api/cmd/hearthctl/routes.go
git commit -m "feat(api): GET /household/access lists the household's tokens and chats"
```

---

### Task 4: Frontend data — schema and hooks

**Files:**
- Modify: `web/src/features/settings/schemas.ts`
- Create: `web/src/features/settings/useHouseholdAccess.ts`
- Modify: `web/src/features/settings/useTelegram.ts`

**Interfaces:**
- Consumes: Task 3's wire shape; existing `POST /api/v1/auth/tokens` (`{name, expiresInDays}` → `201 {id,name,prefix,createdAt,expiresAt,lastUsedAt,token}`) and `DELETE /api/v1/auth/tokens/{id}` (`204`).
- Produces: `householdAccessSchema`, `HouseholdAccess`, `AccessToken`, `AccessChat`, `createdApiTokenSchema`, `CreatedApiToken`; `householdAccessQueryKey`, `useHouseholdAccess()`, `useCreateApiToken()`, `useRevokeApiToken()`.

- [ ] **Step 1: Add the schemas**

Append to `web/src/features/settings/schemas.ts`:

```ts
// GET /household/access (access_handlers.go). An owner gets every member's
// rows, a limited member only their own -- the server decides, so this
// schema has no notion of role. chatUsername is "" when Telegram sent none.
export const accessTokenSchema = z.object({
  id: z.string(),
  memberId: z.string(),
  memberName: z.string(),
  name: z.string(),
  prefix: z.string(),
  createdAt: z.string(),
  expiresAt: z.string(),
  lastUsedAt: z.string().nullable(),
});
export type AccessToken = z.infer<typeof accessTokenSchema>;

export const accessChatSchema = z.object({
  memberId: z.string(),
  memberName: z.string(),
  chatUsername: z.string(),
  linkedAt: z.string(),
});
export type AccessChat = z.infer<typeof accessChatSchema>;

export const householdAccessSchema = z.object({
  telegramEnabled: z.boolean(),
  tokens: z.array(accessTokenSchema),
  chats: z.array(accessChatSchema),
});
export type HouseholdAccess = z.infer<typeof householdAccessSchema>;

// POST /auth/tokens' 201 body. `token` is the raw secret, and this is the
// only response that ever carries it (ADR 7 rule 7).
export const createdApiTokenSchema = z.object({
  id: z.string(),
  name: z.string(),
  prefix: z.string(),
  expiresAt: z.string(),
  token: z.string(),
});
export type CreatedApiToken = z.infer<typeof createdApiTokenSchema>;
```

- [ ] **Step 2: Add the hooks**

Create `web/src/features/settings/useHouseholdAccess.ts`:

```ts
// The household access list (Settings' Access panel) and the two token
// writes it offers. Both writes go to the existing /auth/tokens routes,
// which only ever touch the caller's own tokens -- this file adds no power
// the API did not already give (spec decision 6).
//
// The key starts with "household" for the reason pendingInvitesQueryKey's
// does: PATCH /household invalidates by that prefix.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch, fetchAndParse } from "../../api/client";
import {
  createdApiTokenSchema,
  householdAccessSchema,
  type CreatedApiToken,
  type HouseholdAccess,
} from "./schemas";

export const householdAccessQueryKey = ["household", "access"] as const;

async function fetchHouseholdAccess(): Promise<HouseholdAccess> {
  return fetchAndParse(householdAccessSchema, "/api/v1/household/access");
}

export function useHouseholdAccess() {
  return useQuery({ queryKey: householdAccessQueryKey, queryFn: fetchHouseholdAccess });
}

export type NewApiToken = { name: string; expiresInDays: number };

export function useCreateApiToken() {
  const queryClient = useQueryClient();
  return useMutation({
    // apiFetch sets Content-Type: application/json whenever a body is given.
    mutationFn: async (input: NewApiToken): Promise<CreatedApiToken> =>
      fetchAndParse(createdApiTokenSchema, "/api/v1/auth/tokens", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: householdAccessQueryKey }),
  });
}

export function useRevokeApiToken() {
  const queryClient = useQueryClient();
  return useMutation({
    // 204, nothing to parse.
    mutationFn: async (id: string) => {
      await apiFetch<unknown>(`/api/v1/auth/tokens/${encodeURIComponent(id)}`, { method: "DELETE" });
    },
    // onSettled: a 404 means the token was already revoked in another tab,
    // and the row on screen is stale either way. Returned so Revoke stays
    // disabled until the refetch lands.
    onSettled: () => queryClient.invalidateQueries({ queryKey: householdAccessQueryKey }),
  });
}
```

- [ ] **Step 3: Refresh the access list when the caller's own chat changes**

In `web/src/features/settings/useTelegram.ts`, add `import { householdAccessQueryKey } from "./useHouseholdAccess";` and, in both `useConfirmTelegramLink` and `useDisconnectTelegram`, replace the `onSuccess` line with:

```ts
    // The access list shows this chat too (other owners see it there), so
    // it goes stale at the same moment the binding does.
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: telegramBindingQueryKey }),
        queryClient.invalidateQueries({ queryKey: householdAccessQueryKey }),
      ]),
```

- [ ] **Step 4: Typecheck and run the existing Telegram tests**

Run: `npx tsc --noEmit -p . && npx vitest run src/features/settings/TelegramPanel.test.tsx`
Expected: no type errors; PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/settings/schemas.ts web/src/features/settings/useHouseholdAccess.ts web/src/features/settings/useTelegram.ts
git commit -m "feat(web): access-list schema and token create/revoke hooks"
```

---

### Task 5: Rename `TelegramPanel` to `TelegramConnection`, no behaviour change

**Files:**
- Rename: `web/src/features/settings/TelegramPanel.tsx` → `TelegramConnection.tsx`
- Rename: `web/src/features/settings/TelegramPanel.test.tsx` → `TelegramConnection.test.tsx`
- Modify: `web/src/features/settings/SettingsPage.tsx` (import only; Task 7 moves it)

**Interfaces:**
- Produces: `export function TelegramConnection()` — same behaviour as `TelegramPanel`, drawn without its own card or `<h2>`; still returns `null` when `GET /auth/telegram` answers 404.

- [ ] **Step 1: Rename with git**

```bash
git mv web/src/features/settings/TelegramPanel.tsx web/src/features/settings/TelegramConnection.tsx
git mv web/src/features/settings/TelegramPanel.test.tsx web/src/features/settings/TelegramConnection.test.tsx
```

- [ ] **Step 2: Update the test file first**

In `TelegramConnection.test.tsx`: change the import to `import { TelegramConnection } from "./TelegramConnection";`, the render to `<TelegramConnection />`, and `describe("TelegramPanel", ...)` to `describe("TelegramConnection", ...)`. The 404 test's final `expect(screen.queryByText("Telegram")).not.toBeInTheDocument();` stays — still true.

Run: `npx vitest run src/features/settings/TelegramConnection.test.tsx`
Expected: FAIL — `TelegramConnection` is not exported.

- [ ] **Step 3: Update the component**

In `TelegramConnection.tsx`:
- rename `export function TelegramPanel()` to `export function TelegramConnection()`;
- replace `<section className="rounded-xl border border-hairline bg-card p-[22px]">` and the `<h2 className="mb-4 text-sm font-semibold text-ink">Telegram</h2>` line with a plain `<div>`, and the closing `</section>` with `</div>`;
- append to the header comment: `Rendered inside AccessPanel's Linked chats group as the caller's own row (docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md, decision 10). Renamed from TelegramPanel; behaviour unchanged.`

Change nothing else. In `SettingsPage.tsx`, change the import and usage to `TelegramConnection` (temporary — Task 7 replaces it).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `npx vitest run src/features/settings/TelegramConnection.test.tsx`
Expected: PASS, same test count as before the rename.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/settings/
git commit -m "refactor(web): TelegramPanel becomes TelegramConnection, no card of its own"
```

---

### Task 6: Token list and the new-token dialog

**Files:**
- Create: `web/src/features/settings/ApiTokenList.tsx` + `ApiTokenList.test.tsx`
- Create: `web/src/features/settings/NewApiTokenModal.tsx` + `NewApiTokenModal.test.tsx`
- Modify: `web/src/features/settings/copy.ts`

**Interfaces:**
- Consumes: Task 4's `AccessToken`, `useCreateApiToken`, `useRevokeApiToken`; existing `useConfirmAction` (`ask()`, `cancel()`, `confirm(action)`, `isConfirming()`, `isPending()`, `errorFor()`), `Modal`, `ModalActions`, `Field`, `FIELD_CONTROL_CLASS`.
- Produces: `ApiTokenList({ tokens, myUserId }: { tokens: AccessToken[]; myUserId: string })`, `NewApiTokenModal({ open, onClose }: { open: boolean; onClose: () => void })`; in `copy.ts`: `tokenMetaLine(token)`, `TOKEN_LIFETIME_OPTIONS`.

- [ ] **Step 1: Add the copy helpers**

Append to `web/src/features/settings/copy.ts`:

```ts
// The expiry choices the new-token dialog offers, inside ADR 7's bounds
// (default 90 days, at most 365).
export const TOKEN_LIFETIME_OPTIONS = [
  { days: 30, label: "30 days" },
  { days: 90, label: "90 days" },
  { days: 365, label: "1 year" },
] as const;

function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
}

// One line under a token's name: when it was last used and when it dies.
// "Never used" is worth saying -- a token nobody has used is the likeliest
// one to have been forgotten.
export function tokenMetaLine(token: { lastUsedAt: string | null; expiresAt: string }): string {
  const used = token.lastUsedAt ? `Last used ${shortDate(token.lastUsedAt)}` : "Never used";
  return `${used} · Expires ${shortDate(token.expiresAt)}`;
}
```

- [ ] **Step 2: Write the failing list test**

Create `web/src/features/settings/ApiTokenList.test.tsx`:

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { ApiTokenList } from "./ApiTokenList";
import type { AccessToken } from "./schemas";

function token(overrides: Partial<AccessToken>): AccessToken {
  return {
    id: "t-1",
    memberId: "u-me",
    memberName: "Andreas",
    name: "laptop",
    prefix: "ab12cd34",
    createdAt: "2026-09-01T10:00:00Z",
    expiresAt: "2026-12-01T10:00:00Z",
    lastUsedAt: null,
    ...overrides,
  };
}

function renderList(tokens: AccessToken[]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <ApiTokenList tokens={tokens} myUserId="u-me" />
    </QueryClientProvider>,
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("ApiTokenList", () => {
  it("offers Revoke on my own tokens only", () => {
    stubFetchRoutes({});
    renderList([
      token({ id: "t-mine", name: "my laptop" }),
      token({ id: "t-partner", name: "partner script", memberId: "u-partner", memberName: "Christine" }),
    ]);

    expect(screen.getByText("my laptop")).toBeInTheDocument();
    expect(screen.getByText("partner script")).toBeInTheDocument();
    expect(screen.getByText(/Christine/)).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Revoke" })).toHaveLength(1);
  });

  it("revokes my token after a confirm, with a DELETE to its id", async () => {
    const fetchMock = stubFetchRoutes({
      "DELETE /api/v1/auth/tokens/t-mine": { status: 204, body: undefined },
    });
    renderList([token({ id: "t-mine", name: "my laptop" })]);

    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    fireEvent.click(await screen.findByRole("button", { name: "Yes, revoke" }));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input) === "/api/v1/auth/tokens/t-mine" && (init?.method ?? "").toUpperCase() === "DELETE",
      );
      expect(call).toBeDefined();
    });
  });

  it("says so when there are no tokens", () => {
    stubFetchRoutes({});
    renderList([]);
    expect(screen.getByText("No API tokens.")).toBeInTheDocument();
  });

  it("shows never-used and the prefix, never a secret", () => {
    stubFetchRoutes({});
    renderList([token({})]);
    expect(screen.getByText(/Never used/)).toBeInTheDocument();
    expect(screen.getByText(/ab12cd34/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run it to verify it fails**

Run: `npx vitest run src/features/settings/ApiTokenList.test.tsx`
Expected: FAIL — cannot resolve `./ApiTokenList`.

- [ ] **Step 4: Write the list**

Create `web/src/features/settings/ApiTokenList.tsx`:

```tsx
// The API tokens group of the Access panel. Every row the server sent is
// shown; Revoke only on the caller's own, because DELETE /auth/tokens/{id}
// only ever revokes the caller's own (spec decision 6) -- a button on
// someone else's row would be a button that always 404s.
//
// The confirm step is in-page (useConfirmAction), never window.confirm, for
// the reason PendingInviteCard.tsx gives.
import { useConfirmAction } from "../../components/useConfirmAction";
import { tokenMetaLine } from "./copy";
import type { AccessToken } from "./schemas";
import { useRevokeApiToken } from "./useHouseholdAccess";

export function ApiTokenList({ tokens, myUserId }: { tokens: AccessToken[]; myUserId: string }) {
  if (tokens.length === 0) {
    return <p className="text-xs text-muted">No API tokens.</p>;
  }
  return (
    <ul className="flex flex-col divide-y divide-hairline">
      {tokens.map((t) => (
        <TokenRow key={t.id} token={t} isMine={t.memberId === myUserId} />
      ))}
    </ul>
  );
}

function TokenRow({ token, isMine }: { token: AccessToken; isMine: boolean }) {
  const revoke = useRevokeApiToken();
  const action = useConfirmAction("Couldn't revoke that token. Please try again.");
  const error = action.errorFor();

  return (
    <li className="flex flex-col gap-1.5 py-2.5 text-[13px]">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate text-ink">
            <span className="font-semibold">{token.name}</span>{" "}
            <span className="text-muted">· {token.memberName}</span>
          </div>
          <div className="mt-0.5 text-[11.5px] text-muted">
            <span className="font-mono">{token.prefix}…</span> · {tokenMetaLine(token)}
          </div>
        </div>
        {isMine && !action.isConfirming() && (
          <button
            type="button"
            onClick={() => action.ask()}
            disabled={action.isPending()}
            className="min-h-11 shrink-0 rounded-lg border border-hairline px-3 py-1.5 text-[11px] font-semibold text-danger disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
          >
            Revoke
          </button>
        )}
        {isMine && action.isConfirming() && (
          <div className="flex shrink-0 items-center gap-2">
            <button
              type="button"
              onClick={() => void action.confirm(() => revoke.mutateAsync(token.id))}
              disabled={action.isPending()}
              className="min-h-11 rounded-lg bg-danger px-3 py-1.5 text-[11px] font-semibold text-white disabled:opacity-60 sm:min-h-0"
            >
              Yes, revoke
            </button>
            <button
              type="button"
              onClick={() => action.cancel()}
              className="min-h-11 rounded-lg border border-hairline px-3 py-1.5 text-[11px] font-semibold text-label sm:min-h-0"
            >
              Keep
            </button>
          </div>
        )}
      </div>
      {error && (
        <p role="alert" className="text-[11px] text-danger">
          {error}
        </p>
      )}
    </li>
  );
}
```

- [ ] **Step 5: Run the list test to verify it passes**

Run: `npx vitest run src/features/settings/ApiTokenList.test.tsx`
Expected: PASS, 4 tests.

- [ ] **Step 6: Write the failing dialog test**

Create `web/src/features/settings/NewApiTokenModal.test.tsx`:

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { NewApiTokenModal } from "./NewApiTokenModal";

const SECRET = "hearth_ab12cd34THE-REST-OF-THE-SECRET";
const CREATED = { id: "t-1", name: "laptop", prefix: "ab12cd34", expiresAt: "2026-12-22T00:00:00Z", token: SECRET };
const EMPTY_ACCESS = { telegramEnabled: false, tokens: [], chats: [] };

function Harness() {
  const [open, setOpen] = useState(true);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        reopen
      </button>
      <NewApiTokenModal open={open} onClose={() => setOpen(false)} />
    </>
  );
}

function renderModal() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <Harness />
    </QueryClientProvider>,
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("NewApiTokenModal", () => {
  it("creates a token with the name and 90 days by default, then shows the secret", async () => {
    let posted: unknown;
    stubFetchRoutes({
      "POST /api/v1/auth/tokens": {
        status: 201,
        body: CREATED,
        capture: (body) => {
          posted = body;
        },
      },
      "GET /api/v1/household/access": { status: 200, body: EMPTY_ACCESS },
    });
    renderModal();

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "laptop" } });
    fireEvent.click(screen.getByRole("button", { name: "Create token" }));

    expect(await screen.findByText(SECRET)).toBeInTheDocument();
    expect(posted).toEqual({ name: "laptop", expiresInDays: 90 });
  });

  it("never shows the secret again after closing", async () => {
    stubFetchRoutes({
      "POST /api/v1/auth/tokens": { status: 201, body: CREATED },
      "GET /api/v1/household/access": { status: 200, body: EMPTY_ACCESS },
    });
    renderModal();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "laptop" } });
    fireEvent.click(screen.getByRole("button", { name: "Create token" }));
    await screen.findByText(SECRET);

    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    fireEvent.click(screen.getByRole("button", { name: "reopen" }));

    expect(screen.queryByText(SECRET)).not.toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveValue("");
  });

  it("will not create a token without a name", () => {
    stubFetchRoutes({});
    renderModal();
    expect(screen.getByRole("button", { name: "Create token" })).toBeDisabled();
  });
});
```

- [ ] **Step 7: Run it to verify it fails**

Run: `npx vitest run src/features/settings/NewApiTokenModal.test.tsx`
Expected: FAIL — cannot resolve `./NewApiTokenModal`.

- [ ] **Step 8: Write the dialog**

Create `web/src/features/settings/NewApiTokenModal.tsx`:

```tsx
// Create a personal API token, then show its secret exactly once (ADR 7
// rule 7; PRD item 14). The secret lives only in this component's mutation
// state: closing the dialog resets it, and nothing else ever holds it -- not
// the query cache, not storage.
//
// Copy only. No QR and no Share-to-Telegram, unlike InviteLinkShare.tsx: an
// invite link is meant for another person; a token is a long-lived
// credential for the member's own scripts and should not travel.
import { useId, useState } from "react";
import { apiErrorMessage } from "../../api/errorMessage";
import { Field } from "../../components/Field";
import { FIELD_CONTROL_CLASS } from "../../components/fieldClasses";
import { Modal } from "../../components/Modal";
import { ModalActions } from "../../components/ModalActions";
import { TOKEN_LIFETIME_OPTIONS } from "./copy";
import { useCreateApiToken } from "./useHouseholdAccess";

const DEFAULT_DAYS = 90;

export function NewApiTokenModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const nameId = useId();
  const lifetimeId = useId();
  const [name, setName] = useState("");
  const [days, setDays] = useState<number>(DEFAULT_DAYS);
  const [copied, setCopied] = useState(false);
  const create = useCreateApiToken();

  function close() {
    setName("");
    setDays(DEFAULT_DAYS);
    setCopied(false);
    create.reset(); // drops the secret from the mutation's own state
    onClose();
  }

  async function copy(secret: string) {
    // Guarded for the reason InviteLinkShare.tsx gives: no clipboard on
    // plain HTTP or in jsdom. The secret stays on screen to copy by hand.
    if (!navigator.clipboard) return;
    await navigator.clipboard.writeText(secret);
    setCopied(true);
  }

  const created = create.data;

  return (
    <Modal open={open} onClose={close} title={created ? "Your new token" : "New API token"}>
      {created ? (
        <div className="flex flex-col gap-3 text-[13px]">
          <p className="text-ink">
            Copy it now. <span className="font-semibold">You won't see it again</span> — Hearth keeps only a
            fingerprint of it.
          </p>
          <code className="block break-all rounded-lg border border-hairline bg-canvas p-3 font-mono text-[12px] text-ink">
            {created.token}
          </code>
          <ModalActions
            secondaryLabel={copied ? "Copied" : "Copy"}
            onSecondary={() => void copy(created.token)}
            primaryLabel="Done"
            primaryType="button"
            onPrimary={close}
          />
        </div>
      ) : (
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate({ name: name.trim(), expiresInDays: days });
          }}
        >
          <Field label="Name" htmlFor={nameId}>
            <input
              id={nameId}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. laptop script"
              className={FIELD_CONTROL_CLASS}
              maxLength={60}
            />
          </Field>
          <Field label="Expires after" htmlFor={lifetimeId}>
            <select
              id={lifetimeId}
              value={days}
              onChange={(e) => setDays(Number(e.target.value))}
              className={FIELD_CONTROL_CLASS}
            >
              {TOKEN_LIFETIME_OPTIONS.map((o) => (
                <option key={o.days} value={o.days}>
                  {o.label}
                </option>
              ))}
            </select>
          </Field>
          {create.isError && (
            <p role="alert" className="text-[11px] text-danger">
              {apiErrorMessage(create.error, "Couldn't create that token. Please try again.")}
            </p>
          )}
          <ModalActions
            secondaryLabel="Cancel"
            onSecondary={close}
            primaryLabel="Create token"
            primaryType="submit"
            primaryDisabled={name.trim() === "" || create.isPending}
          />
        </form>
      )}
    </Modal>
  );
}
```

If `FIELD_CONTROL_CLASS` is not the class `InviteMemberModal.tsx` puts on its text inputs, use the one it does use.

- [ ] **Step 9: Run both test files to verify they pass**

Run: `npx vitest run src/features/settings/ApiTokenList.test.tsx src/features/settings/NewApiTokenModal.test.tsx`
Expected: PASS, 7 tests.

- [ ] **Step 10: Commit**

```bash
git add web/src/features/settings/
git commit -m "feat(web): API token list with revoke, and a new-token dialog that shows the secret once"
```

---

### Task 7: The Access panel in Settings

**Files:**
- Create: `web/src/features/settings/LinkedChatList.tsx`
- Create: `web/src/features/settings/AccessPanel.tsx` + `AccessPanel.test.tsx`
- Modify: `web/src/features/settings/SettingsPage.tsx`, `MembersPanel.tsx`, `copy.ts`

**Interfaces:**
- Consumes: `useHouseholdAccess` (Task 4), `ApiTokenList`, `NewApiTokenModal` (Task 6), `TelegramConnection` (Task 5), existing `useMe` (`features/auth/useAuth`), `usePendingInvites({ enabled })`, `telegramChatLabel`, `formatTelegramLinkedAt`.
- Produces: `AccessPanel()`, `LinkedChatList({ chats, myUserId })`, `pendingInvitesPointer(count: number): string | null`.

- [ ] **Step 1: Add the pointer copy and the Members anchor**

Append to `web/src/features/settings/copy.ts`:

```ts
// The Access panel's one line about invites. Pending invites stay listed
// under Members, beside "+ Invite" (spec decision 2); this only points there.
export function pendingInvitesPointer(count: number): string | null {
  if (count === 0) return null;
  return count === 1 ? "1 pending invite — in Members" : `${count} pending invites — in Members`;
}
```

In `web/src/features/settings/MembersPanel.tsx`, line ~244, give the outer card an id the pointer can scroll to:
`<section id="members" className="rounded-xl border border-hairline bg-card p-[22px]">`

- [ ] **Step 2: Write the failing panel test**

Create `web/src/features/settings/AccessPanel.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { meFixture } from "../marriage/agreementFixtures";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AccessPanel } from "./AccessPanel";

const ACCESS_URL = "/api/v1/household/access";
const EMPTY_ACCESS = { telegramEnabled: false, tokens: [], chats: [] };

function renderPanel() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <AccessPanel />
    </QueryClientProvider>,
  );
}

// meFixture's user id is "u-andreas".
const partnerChat = { memberId: "u-christine", memberName: "Christine", chatUsername: "chris_o", linkedAt: "2026-09-01T10:00:00Z" };
const myChat = { memberId: "u-andreas", memberName: "Andreas", chatUsername: "andreas_o", linkedAt: "2026-09-01T10:00:00Z" };

function pendingInvite(id: string, name: string) {
  return { id, name, email: "", role: "owner", capabilities: [], channel: "telegram", knock: null, expiresAt: "2026-12-01T00:00:00Z" };
}

afterEach(() => vi.unstubAllGlobals());

describe("AccessPanel", () => {
  it("lists the partner's chat, and mine only once, through my own connection", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      [`GET ${ACCESS_URL}`]: { status: 200, body: { telegramEnabled: true, tokens: [], chats: [partnerChat, myChat] } },
      "GET /api/v1/auth/telegram": {
        status: 200,
        body: { connected: true, chatUsername: "andreas_o", linkedAt: "2026-09-01T10:00:00Z" },
      },
      "GET /api/v1/household/invites": { status: 200, body: [] },
    });
    renderPanel();

    expect(await screen.findByText("@chris_o")).toBeInTheDocument();
    expect(await screen.findAllByText("@andreas_o")).toHaveLength(1);
  });

  it("hides the Linked chats group when Telegram is off", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      [`GET ${ACCESS_URL}`]: { status: 200, body: EMPTY_ACCESS },
      "GET /api/v1/household/invites": { status: 200, body: [] },
    });
    renderPanel();

    expect(await screen.findByText("No API tokens.")).toBeInTheDocument();
    expect(screen.queryByText("Linked chats")).not.toBeInTheDocument();
  });

  it("points an owner at pending invites in Members", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      [`GET ${ACCESS_URL}`]: { status: 200, body: EMPTY_ACCESS },
      "GET /api/v1/household/invites": { status: 200, body: [pendingInvite("i-1", "Jane"), pendingInvite("i-2", "Kid")] },
    });
    renderPanel();

    expect(await screen.findByRole("button", { name: "2 pending invites — in Members" })).toBeInTheDocument();
  });

  it("never asks a limited member's browser for the owner-only invite list", async () => {
    const owner = meFixture();
    const fetchMock = stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture({ membership: { ...owner.membership, role: "limited" } }) },
      [`GET ${ACCESS_URL}`]: { status: 200, body: EMPTY_ACCESS },
    });
    renderPanel();

    await screen.findByText("No API tokens.");
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes("/household/invites"))).toBe(false);
  });

  it("offers New token", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      [`GET ${ACCESS_URL}`]: { status: 200, body: EMPTY_ACCESS },
      "GET /api/v1/household/invites": { status: 200, body: [] },
    });
    renderPanel();
    expect(await screen.findByRole("button", { name: "New token" })).toBeInTheDocument();
  });
});
```

If `pendingInvite(...)` does not parse against `pendingInviteSchema`, copy a row from `PendingInvitesList.test.tsx`.

- [ ] **Step 3: Run it to verify it fails**

Run: `npx vitest run src/features/settings/AccessPanel.test.tsx`
Expected: FAIL — cannot resolve `./AccessPanel`.

- [ ] **Step 4: Write the chat list**

Create `web/src/features/settings/LinkedChatList.tsx`:

```tsx
// The Linked chats group. The caller's own chat is TelegramConnection --
// connect, confirm, disconnect, unchanged from the old Telegram card -- so
// it is left out of the read-only rows below, or it would be drawn twice
// (spec decision 10). The rows are the other members' chats, which an
// owner can see and nobody here can disconnect (decision 6).
import { formatTelegramLinkedAt, telegramChatLabel } from "./copy";
import type { AccessChat } from "./schemas";
import { TelegramConnection } from "./TelegramConnection";

export function LinkedChatList({ chats, myUserId }: { chats: AccessChat[]; myUserId: string }) {
  const others = chats.filter((c) => c.memberId !== myUserId);
  return (
    <div className="flex flex-col gap-3">
      <TelegramConnection />
      {others.length > 0 && (
        <ul className="flex flex-col divide-y divide-hairline border-t border-hairline">
          {others.map((c) => (
            <li key={c.memberId} className="py-2.5 text-[13px]">
              <div className="text-ink">
                {/* "" means Telegram sent no username; the label helper
                    already words that case, given undefined. */}
                <span className="font-semibold">{telegramChatLabel(c.chatUsername || undefined)}</span>{" "}
                <span className="text-muted">· {c.memberName}</span>
              </div>
              <div className="mt-0.5 text-[11.5px] text-muted">Linked {formatTelegramLinkedAt(c.linkedAt)}</div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
```

- [ ] **Step 5: Write the panel**

Create `web/src/features/settings/AccessPanel.tsx`:

```tsx
// Settings' Access panel: every live way into the household that is not a
// password -- API tokens and linked Telegram chats -- each labelled with its
// member. docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md.
//
// An owner gets every member's rows and a limited member only their own;
// the server decides (GET /household/access), so nothing here branches on
// role for *what to list*. The one role check is for the pending-invite
// pointer, whose list is owner-only: a limited member must never send a
// request whose 403 would then need hiding (usePendingInvites.ts).
import { useState } from "react";
import { useMe } from "../auth/useAuth";
import { ApiTokenList } from "./ApiTokenList";
import { pendingInvitesPointer } from "./copy";
import { LinkedChatList } from "./LinkedChatList";
import { NewApiTokenModal } from "./NewApiTokenModal";
import { useHouseholdAccess } from "./useHouseholdAccess";
import { usePendingInvites } from "./usePendingInvites";

export function AccessPanel() {
  const me = useMe();
  const access = useHouseholdAccess();
  const isOwner = me.data?.membership.role === "owner";
  const invites = usePendingInvites({ enabled: isOwner });
  const [creating, setCreating] = useState(false);

  const pointer = isOwner ? pendingInvitesPointer(invites.data?.length ?? 0) : null;

  return (
    <section className="rounded-xl border border-hairline bg-card p-[22px]">
      <div className="mb-1 flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold text-ink">Access</h2>
        <button
          type="button"
          onClick={() => setCreating(true)}
          className="min-h-11 rounded-lg border border-hairline px-3 py-1.5 text-[11px] font-semibold text-label sm:min-h-0"
        >
          New token
        </button>
      </div>
      <p className="mb-4 text-xs text-muted">
        {isOwner ? "Every way into your household besides a password." : "Your own ways in besides a password."}
      </p>

      {pointer && (
        <p className="mb-4 text-xs">
          {/* The Members card is on this same page, so this scrolls rather
              than navigates. */}
          <button
            type="button"
            onClick={() => document.getElementById("members")?.scrollIntoView({ behavior: "smooth" })}
            className="font-semibold text-accent"
          >
            {pointer}
          </button>
        </p>
      )}

      {(access.isPending || me.isPending) && <p className="text-xs text-muted">Loading…</p>}
      {access.isError && (
        <p role="alert" className="text-xs text-danger">
          Couldn't load the access list.
        </p>
      )}

      {access.isSuccess && me.isSuccess && (
        <div className="flex flex-col gap-5">
          <div>
            <h3 className="mb-2 text-[12px] font-semibold uppercase tracking-wide text-muted">API tokens</h3>
            <ApiTokenList tokens={access.data.tokens} myUserId={me.data.user.id} />
          </div>
          {access.data.telegramEnabled && (
            <div>
              <h3 className="mb-2 text-[12px] font-semibold uppercase tracking-wide text-muted">Linked chats</h3>
              <LinkedChatList chats={access.data.chats} myUserId={me.data.user.id} />
            </div>
          )}
        </div>
      )}

      <NewApiTokenModal open={creating} onClose={() => setCreating(false)} />
    </section>
  );
}
```

- [ ] **Step 6: Put it on the Settings page**

In `web/src/features/settings/SettingsPage.tsx`:
- replace `import { TelegramConnection } from "./TelegramConnection";` with `import { AccessPanel } from "./AccessPanel";`
- replace `<TelegramConnection />` with `<AccessPanel />`
- in the header comment, replace the sentence about the Telegram card with: `plus Access -- API tokens and linked Telegram chats, which the design never drew (milestone 3 spec, docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md). The Telegram card that used to stand alone is now Access's Linked chats group.`

- [ ] **Step 7: Run all settings tests and the typecheck**

Run: `npx tsc --noEmit -p . && npx vitest run src/features/settings/`
Expected: no type errors; PASS, every file.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/settings/
git commit -m "feat(web): Access panel lists the household's tokens and chats in Settings"
```

---

### Task 8: Full gate, mutation checks, docs

**Files:**
- Modify: `docs/FEATURE_TRACKER.md`, `docs/SYSTEM_DESIGN.md`, `docs/LEARNING.md`, `docs/CLI.md`, `docs/adr/0007-personal-api-tokens.md`, `.claude/prds/partner-invite-lobby.prd.md`
- Create: `docs/superpowers/plans/2026-09-23-hearth-household-access-list-m3-verification.md`

- [ ] **Step 1: Full lint and test**

Run: `make lint && make test`
Expected: both green. If `lint-dead` flags new code, find the caller that should reach it rather than deleting it.

- [ ] **Step 2: Mutation checks — each must turn its test red, then be reverted**

Create the verification file with a "Mutation checks" section. For each: make the change, run the test, paste the failing line into the file, then `git checkout -- <file>` (and `make sqlc` again for #3).

1. `access_handlers.go`: in the `case domain.RoleLimited:` branch, call `deps.Access.ForHousehold(r.Context(), scope.HouseholdID, withChats)` instead.
   Run: `go test ./internal/adapter/http/ -run TestALimitedMemberSeesOnlyTheirOwnTokensAndChat -count=1` → must FAIL.
2. `router.go`: delete `a.Use(requireCookieSession)` from the access group.
   Run: `go test ./internal/adapter/http/ -run TestTheAccessListRefusesAnAPIToken -count=1` → must FAIL.
3. `queries/api_token.sql`: in `ListLiveAPITokensForHousehold`, change `WHERE household_id = $1 AND` to `WHERE $1::uuid IS NOT NULL AND`; `make sqlc`.
   Run: `go test ./internal/adapter/postgres/ -run TestListTokensForHouseholdShowsEveryMembersLiveTokensAndNoOtherHouseholds -count=1` → must FAIL.
4. `ApiTokenList.tsx`: change `isMine={t.memberId === myUserId}` to `isMine={true}`.
   Run: `npx vitest run src/features/settings/ApiTokenList.test.tsx` → must FAIL.

After reverting all four, re-run `make test` → green.

- [ ] **Step 3: Update the docs**

- `docs/FEATURE_TRACKER.md`:
  - Rewrite the "Manage API tokens in Settings" row: it no longer says "a frontend screen over existing endpoints". Describe what shipped — the Access panel's token list with Revoke on own rows only, the New token dialog (30 / 90 / 365 days, secret shown once, Copy only) — and mark it 🟡 "built, not yet walked" until Task 9 passes.
  - Add a row "Household access list — every member's tokens and chats (no mockup)": the route, owner-sees-all / limited-sees-own, browser session only, `telegramEnabled`, the spec path. 🟡 until Task 9.
  - In the "Personal API tokens for headless callers" row, change the closing "**Gap, named:** no Settings screen — the row below" to point at the Access panel.
  - Recount the summary table at the top; its columns must sum to the stated totals.
- `docs/SYSTEM_DESIGN.md` — use the `maintaining-system-design` skill: `GET /household/access` and its guards in the route map; `AccessListService` and its three ports in the component diagram; the Settings change (Access panel replaces the Telegram card). Update the prose under each diagram touched.
- `docs/adr/0007-personal-api-tokens.md` — replace the Consequences line "There is no Settings screen for tokens yet; the feature tracker carries it as a ⬜ row." with: "Settings' Access panel lists, creates and revokes a member's own tokens, and shows owners every member's tokens read-only (`docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md`)."
- `docs/CLI.md` — where the token routes are described, add `GET /household/access` and that it needs a browser session.
- `.claude/prds/partner-invite-lobby.prd.md` — replace "Milestone 3, the household access list, is not yet planned." with a pointer to this spec and plan.
- `docs/LEARNING.md` — what this work taught. At least: `ListForUser` keeps expired tokens while the new household query drops them — two "list tokens" queries answering different questions, and the comment on each now says which. Add anything Task 9 finds. Put each under an existing pattern where one fits.

- [ ] **Step 4: Commit**

```bash
git add docs/ .claude/prds/partner-invite-lobby.prd.md
git commit -m "docs: household access list in the tracker, system design, ADR 7 and learning log"
```

---

### Task 9: Browser walk

**Files:**
- Complete: `docs/superpowers/plans/2026-09-23-hearth-household-access-list-m3-verification.md`

Use the `verifying-in-the-real-environment` skill. First run `lsof -i :5173` — two Docker engines can both host the stack here; make sure the one serving 5173 runs this branch. Then `make dev` and `make seed`. Use two browser sessions (a normal window and a separate profile) for owner and partner.

Record pass/fail and what was actually seen for each:

- [ ] 1. As the owner, Settings shows an **Access** card with "API tokens" and (Telegram flag on) "Linked chats". No standalone Telegram card remains.
- [ ] 2. Owner clicks **New token**, names it `walk-owner`, keeps 90 days, creates it: the `hearth_…` secret is shown once with Copy.
- [ ] 3. **Done**, then **New token** again: the dialog is empty and the previous secret is nowhere on the page.
- [ ] 4. `walk-owner` is in the token list with its prefix, "Never used", an expiry about 90 days out, and a **Revoke** button.
- [ ] 5. `hearthctl login --token <secret>` then `hearthctl whoami` works. After a browser refresh the row shows "Last used" with today's date.
- [ ] 6. `curl -H "Authorization: Bearer <secret>" http://localhost:5173/api/v1/household/access` answers `403` with `SESSION_REQUIRED`.
- [ ] 7. As the partner (second session, also an owner), create `walk-partner`. In the owner's session, refresh: `walk-partner · <partner name>` is listed **with no Revoke button**.
- [ ] 8. As the partner, revoke `walk-partner` (the confirm step appears first). Owner refreshes: the row is gone.
- [ ] 9. As the owner, revoke `walk-owner`: the row is gone, and `hearthctl whoami` from step 5 now fails (exit 2).
- [ ] 10. With a pending invite outstanding, the Access card shows "1 pending invite — in Members"; clicking it scrolls to the Members card.
- [ ] 11. Signed in as a limited member (the seeded kid): Access shows only their own tokens and chat, no invite pointer, and the network tab shows no request to `/household/invites`.
- [ ] 12. The owner's own Telegram connection still works inside Linked chats: Connect opens the bot; Disconnect removes it and the list refreshes. If no dev bot is available, record that and check rendering only.
- [ ] 13. At 375px wide: no horizontal scroll on Settings; long token names truncate; Revoke and New token stay tappable.
- [ ] 14. No console errors during any of the above.

When all pass, move the two tracker rows from 🟡 to ✅ with "walked 2026-09-.. — N of 14" and the verification file path, then:

```bash
git add docs/
git commit -m "docs: household access list walked in a real browser"
```

Then use `superpowers:finishing-a-development-branch`.
