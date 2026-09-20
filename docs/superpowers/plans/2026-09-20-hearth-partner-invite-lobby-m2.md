# Partner invite lobby, milestone 2 — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An owner invites their partner, hands over a one-time Telegram link, sees the partner's knock and a four-digit matching code, clicks **Let in**, and the partner is a member with a sign-in link in their own chat — with no operator involved.

**Architecture:** The `invites` table gains a `channel` and four knock columns (migration `00021`); no new table. The bot's existing `/start` handler routes an `inv_`-prefixed payload to a new `InviteKnocker` port before it touches its own nonce table, records the knock in one guarded `UPDATE`, and replies with a code. The owner's browser polls the milestone-1 pending-invite list, which now carries the knock, and admits through a new route whose repository method does the user, membership, `telegram_accounts` row and acceptance stamp in one transaction. Email invites go behind a new `email_invites` flag, off by default, enforced at the HTTP edge as well as in the UI.

**Tech Stack:** Go 1.25.7 (clean architecture: `domain` → `usecase` → `adapter`), chi router, pgx + sqlc, Postgres, React 19 + TypeScript + TanStack Query + Zod, Tailwind, Vitest, testcontainers.

**Spec:** [`docs/superpowers/specs/2026-09-19-hearth-partner-invite-lobby-design.md`](../specs/2026-09-19-hearth-partner-invite-lobby-design.md)
**Handover (read it first):** [`docs/superpowers/plans/2026-09-20-hearth-partner-invite-lobby-m2-handover.md`](2026-09-20-hearth-partner-invite-lobby-m2-handover.md)
**PRD:** `.claude/prds/partner-invite-lobby.prd.md`
**Security review folded in:** [`docs/reviews/2026-09-19-security-review.md`](../../reviews/2026-09-19-security-review.md) finding 2 (Task 1)

---

## Global Constraints

Every task's requirements implicitly include this section.

**Environment (this machine)**

- `go` is not on `PATH` in a bare shell. Add it: `export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH`. Inside `api/`, `GOTOOLCHAIN=auto` downloads 1.25.7, which `api/go.mod` asks for.
- A tool run with `go run some/tool@version` builds outside the module and does **not** switch toolchains. Set `GOTOOLCHAIN=go1.25.7` for those.
- The Go suite uses testcontainers and needs a Docker socket:
  ```bash
  export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
  export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
  ```
- Two Docker engines exist here (Desktop and colima). Before trusting `localhost:5173`, run `lsof -nP -iTCP:5173 -sTCP:LISTEN`.
- colima stops when the machine sleeps: `colima start`, then `make up` (**not** `make dev`, which blocks tailing logs).
- Branch off `origin/main` (M1 landed there as `38c4b15`); local `main` is stale.
- **Never `git add -A`, `git add .` or `git add docs`.** The working tree carries other people's uncommitted files (`docs/SKILL_TRACKER.md`, `docs/reviews/2026-09-19-security-review.md` and its `docs/HANDOVER.md` paragraph). Stage by explicit path, every time.

**Code rules (CLAUDE.md, not preferences)**

- Dependencies point inward. `internal/domain` imports the standard library only; `internal/usecase` may add `internal/domain`; everything else is `internal/adapter/**` or `cmd/**`. `make lint-arch` enforces it, test files included.
- No database, HTTP or third-party type crosses out of the adapter layer. A missing row becomes `domain.ErrNotFound` at that boundary.
- **Authorisation exists only at each channel's inbound edge** (ADR 8). No service takes an actor parameter. Services enforce what is *valid*; the route enforces who is *asking*.
- **Every 2xx except 204 carries a JSON body** — the frontend's `apiFetch` throws on an ok response it cannot parse.
- **Fail closed on values you did not construct.** A `switch` over a value from a database column or a request needs a `default` that refuses.
- Comments say **why**, never what the line already says. Exported things carry their contract in a doc comment; `usecase/ports.go` is the model.

**Values fixed by the spec — copy them exactly**

- Telegram invite TTL: **24 hours**. Email invites keep the existing 7 days (`inviteTTL` in `usecase/invite.go`).
- Deep-link payload prefix: **`inv_`** (reserved by migration `00018`'s comment). `inv_` + 43 base64url characters = 47, under Telegram's 64-character `start` limit.
- Matching code: **four digits**, from `crypto/rand`, **display only — no endpoint ever accepts it**.
- Flag key: **`email_invites`**, default **false**.
- QR dependency: **`qrcode-generator` 2.0.4**, pinned exact (no `^`), MIT, browser-only.
- Poll interval: **3 s**, and only while at least one Telegram invite has no knock.
- The one bland chat refusal is the existing `telegramDeadLinkMessage`: `"That sign-in link has expired. Start again from the app."`

**Mutation checks (spec's named four).** Each must turn a test red when you break it on purpose. Task 15 runs them all; the owning task runs its own.

1. Remove the household filter from `ListPending`. (Task 3)
2. Remove `knocked_at IS NULL` from `RecordKnock`. (Task 7)
3. Remove `requireCookieSession` from admit. (Task 9)
4. Let web accept through a Telegram invite. (Task 6)

**Definition of done for the branch:** `make lint && make test` green, the four mutation checks actually run, a fifteen-criterion browser walk recorded in a verification file, and `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md` and `docs/SYSTEM_DESIGN.md` updated in the same change.

---

## File Structure

**New files**

| Path | Responsibility |
|---|---|
| `api/migrations/00021_invite_channels.sql` | The channel column, the four knock columns, four CHECK constraints |
| `api/internal/domain/invitechannel.go` | `InviteChannel` and its fail-closed parser |
| `api/internal/adapter/crypto/paircode.go` | `PairCodes.NewCode()` — four digits from `crypto/rand` |
| `api/internal/adapter/http/invite_lobby_handlers.go` | The two new owner routes: new link, admit |
| `api/internal/adapter/telegram/chat_type_test.go` | Group chats are refused (security review finding 2) |
| `web/src/features/settings/PendingInviteCard.tsx` | One card, three states: waiting, knocked, admitted |
| `web/src/features/settings/InviteLinkShare.tsx` | Copy / QR / Share-to-Telegram for one link |
| `web/src/features/settings/PendingInviteCard.test.tsx` | The card's three states |
| `docs/adr/0011-joining-a-household-by-knock.md` | The decision record |
| `docs/superpowers/plans/2026-09-20-hearth-partner-invite-lobby-m2-verification.md` | The browser walk record |

**Modified files**

| Path | Change |
|---|---|
| `api/internal/adapter/telegram/update.go` | `Chat.Type`, private-only `ParseStart` |
| `api/internal/adapter/telegram/commands.go` | private-only `ParseCommand` |
| `api/internal/adapter/http/router.go` | Two new routes; `requireCookieSession` on invite create and both member routes |
| `api/internal/adapter/http/member_handlers.go` | `channel` on the create body; `201 {id, expiresAt, link?}` |
| `api/internal/adapter/http/pending_invite_handlers.go` | `channel` and `knock` on the list DTO |
| `api/internal/adapter/http/invite_handlers.go` | Web preview/accept refuse a Telegram invite |
| `api/internal/adapter/http/errors.go` | Five new rows in `domainErrorResponses` |
| `api/internal/domain/errors.go` | Five new sentinels |
| `api/internal/domain/featureflag.go` | `FlagEmailInvites` |
| `api/internal/usecase/ports.go` | `InviteRepository` gains four methods; `InviteKnocker`, `InviteChats`, `PairingCodes` |
| `api/internal/usecase/invite.go` | `CreateTelegram`, `NewLink`, `Admit`, `Knock` |
| `api/internal/usecase/telegram_auth.go` | The `inv_` branch, before `Links.Consume` |
| `api/internal/adapter/postgres/invite_repo.go` | `CreateTelegram`, `RecordKnock`, `ReplaceToken`, `Admit` |
| `api/internal/adapter/postgres/queries/identity.sql` | The queries behind them |
| `api/cmd/api/main.go` | `InviteDeps.Chats`, `.Codes`, `.BotUsername`, `.TelegramInviteTTL`; `TelegramAuthDeps.Invites` |
| `api/internal/adapter/http/api_test.go` | The same wiring, in its own `Deps` literal |
| `api/cmd/hearthctl/routes.go` | Two new rows, three changed guard strings |
| `web/src/features/settings/schemas.ts` | `channel`, `knock` — both optional |
| `web/src/features/settings/usePendingInvites.ts` | The 3 s poll, `useNewInviteLink`, `useAdmitInvite` |
| `web/src/features/settings/useInviteMember.ts` | `channel` in, `{id, expiresAt, link?}` out |
| `web/src/features/settings/InviteMemberModal.tsx` | Channel choice; success becomes the waiting card |
| `web/src/features/settings/PendingInvitesList.tsx` | Renders `PendingInviteCard` per row |
| `web/src/features/settings/copy.ts` | The knock lines |
| `web/src/features/admin/AdminHouseholdPage.tsx` | A Telegram invite's blank email reads "Telegram link" |

---

## Task 1: Only a private chat may talk to the bot

Security review finding 2, folded in because the knock inherits it: `Knock` records whatever `chat_id` sent `/start`, and Task 9's transaction then binds `telegram_accounts` to that chat. If a **group** taps the invite link, every member of that group becomes the new member, and the sign-in link Task 9 sends lands in the group. Spec decision 15 does not cover this — it checks *already bound*, not *is a person*.

**Files:**
- Modify: `api/internal/adapter/telegram/update.go` (the `Message.Chat` struct and `ParseStart`)
- Modify: `api/internal/adapter/telegram/commands.go:50-68` (`ParseCommand`)
- Test: `api/internal/adapter/telegram/chat_type_test.go` (create)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `ParseStart(u Update) (StartCommand, bool)` and `ParseCommand(u Update) (Command, bool)` keep their signatures. Both now answer `false` for any chat that is not a one-to-one private chat. Every later task may assume a knock's `chatID` belongs to one person.

- [ ] **Step 1: Write the failing test**

Create `api/internal/adapter/telegram/chat_type_test.go`:

```go
package telegram

import "testing"

// privateUpdate is the shape Telegram sends for a one-to-one chat: the chat
// id and the sender id are the same number, and the type is "private".
func privateUpdate(text string) Update {
	u := Update{UpdateID: 1, Message: &Message{Text: text}}
	u.Message.Chat.ID = 4242
	u.Message.Chat.Type = "private"
	u.Message.From = &User{ID: 4242, Username: "jane_t"}
	return u
}

// groupUpdate is a group: a negative chat id, a "group" type, and a sender
// who is one of many members rather than the chat itself.
func groupUpdate(text string) Update {
	u := Update{UpdateID: 2, Message: &Message{Text: text}}
	u.Message.Chat.ID = -100500
	u.Message.Chat.Type = "group"
	u.Message.From = &User{ID: 4242, Username: "jane_t"}
	return u
}

func TestParseStartAcceptsOnlyAPrivateChat(t *testing.T) {
	if _, ok := ParseStart(privateUpdate("/start inv_abc")); !ok {
		t.Fatal("a private /start must parse")
	}
	for name, u := range map[string]Update{
		"group":       groupUpdate("/start inv_abc"),
		"supergroup":  withChatType(groupUpdate("/start inv_abc"), "supergroup"),
		"channel":     withChatType(groupUpdate("/start inv_abc"), "channel"),
		"empty type":  withChatType(privateUpdate("/start inv_abc"), ""),
		"unknown type": withChatType(privateUpdate("/start inv_abc"), "secret_new_kind"),
	} {
		if _, ok := ParseStart(u); ok {
			t.Errorf("%s: /start must be refused", name)
		}
	}
}

// A "private" chat whose sender is somebody else is refused too: the chat id
// is what every downstream write keys on, so it must be the person's own.
func TestParseStartRefusesASenderWhoIsNotTheChat(t *testing.T) {
	u := privateUpdate("/start inv_abc")
	u.Message.From = &User{ID: 9999, Username: "someone_else"}
	if _, ok := ParseStart(u); ok {
		t.Fatal("a private chat whose From.ID is not the chat id must be refused")
	}
	u.Message.From = nil
	if _, ok := ParseStart(u); ok {
		t.Fatal("a message with no From must be refused")
	}
}

func TestParseCommandAcceptsOnlyAPrivateChat(t *testing.T) {
	if _, ok := ParseCommand(privateUpdate("/balance")); !ok {
		t.Fatal("a private /balance must parse")
	}
	if _, ok := ParseCommand(groupUpdate("/balance")); ok {
		t.Fatal("a group /balance must be refused")
	}
	if _, ok := ParseCommand(groupUpdate("lunch 12.50")); ok {
		t.Fatal("group free text must be refused")
	}
}

func withChatType(u Update, chatType string) Update {
	u.Message.Chat.Type = chatType
	return u
}
```

- [ ] **Step 2: Run the test and watch it fail**

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
cd api && go test ./internal/adapter/telegram/ -run 'ChatType|PrivateChat|ParseStart|ParseCommand' -v
```

Expected: a compile failure — `u.Message.Chat.Type undefined` and `User has no field ID`. That is the test failing for the right reason; the field does not exist yet.

- [ ] **Step 3: Add the two fields**

In `api/internal/adapter/telegram/update.go`, extend the `Chat` struct and `User`:

```go
type Message struct {
	Text string `json:"text"`
	Chat struct {
		ID int64 `json:"id"`
		// Type is "private", "group", "supergroup" or "channel". Only
		// "private" is a single person, and every write keyed on a chat id
		// -- the Telegram binding, the pending-spend map, a knock -- assumes
		// one person. In a group the chat is everyone in it: any member
		// could confirm another member's spend, and a sign-in link sent to
		// the chat would be posted to the room (security review
		// 2026-09-19, finding 2).
		Type string `json:"type"`
	} `json:"chat"`
	// From is absent on a channel post, so this is a pointer and every
	// reader must handle nil. Read for display only -- the confirm screen
	// names the chat that redeemed a link -- never to decide anything:
	// a username is chosen by its owner and Telegram lets it change.
	From *User `json:"from"`
}

type User struct {
	// ID is compared with Chat.ID and nothing else. In a private chat the
	// two are the same number; anywhere else they differ, which is the
	// second gate behind Chat.Type.
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}
```

- [ ] **Step 4: Refuse every chat that is not the sender's own private chat**

Still in `update.go`, add the predicate and call it from `ParseStart`:

```go
// isPrivateChatWithItsOwner answers whether this message came from one
// person's own one-to-one chat with the bot.
//
// Two gates, deliberately, because each fails differently. Chat.Type is
// Telegram's own answer and is checked with a switch whose default refuses,
// so a chat kind this build has never heard of is refused rather than
// guessed at (CLAUDE.md: fail closed on values you did not construct).
// From.ID == Chat.ID is the arithmetic that holds only in a private chat,
// and it still holds if Telegram ever adds a private-like type we would
// otherwise have to enumerate.
func isPrivateChatWithItsOwner(m *Message) bool {
	switch m.Chat.Type {
	case "private":
		return m.From != nil && m.From.ID == m.Chat.ID
	default:
		return false
	}
}
```

and in `ParseStart`, immediately after the `u.Message == nil` guard:

```go
	if !isPrivateChatWithItsOwner(u.Message) {
		return StartCommand{}, false
	}
```

- [ ] **Step 5: Do the same for `ParseCommand`**

In `api/internal/adapter/telegram/commands.go`, after its own `u.Message == nil` guard:

```go
	if !isPrivateChatWithItsOwner(u.Message) {
		return Command{}, false
	}
```

Leave the `strings.Cut(strings.ToLower(word), "@")` line alone and change its comment, because the reason it exists has changed:

```go
	// Telegram appends @botname to commands in groups: "/spend@HearthBot".
	// Group traffic no longer reaches here (isPrivateChatWithItsOwner above),
	// but a person may still type the suffix by hand after copying a command
	// out of a group, so the strip stays.
```

- [ ] **Step 6: Run the test and watch it pass**

```bash
cd api && go test ./internal/adapter/telegram/ -v
```

Expected: PASS, including the package's existing tests. Any existing test that builds an `Update` by hand without a chat type now fails — fix those fixtures by setting `Chat.Type = "private"` and `From = &User{ID: <same as chat id>}`, which is what Telegram really sends.

- [ ] **Step 7: Mutation-check the new guard**

Temporarily change `isPrivateChatWithItsOwner`'s `default:` to `return true`. Run the test. `TestParseStartAcceptsOnlyAPrivateChat` and `TestParseCommandAcceptsOnlyAPrivateChat` must fail. Put it back.

- [ ] **Step 8: Commit**

```bash
git add api/internal/adapter/telegram/update.go api/internal/adapter/telegram/commands.go api/internal/adapter/telegram/chat_type_test.go
git commit -m "fix: the bot answers only a private chat, never a group

A group chat bound to an account makes every member of that group the
account holder: any member can confirm another's spend, and a sign-in link
sent to the chat is posted to the room. Security review 2026-09-19,
finding 2. Folded into the invite lobby because a knock records whatever
chat taps the link."
```

---

## Task 2: A leaked API token cannot change who is in the household

Spec decision 12. Milestone 1 put `requireCookieSession` on withdraw only. `POST /household/members/invite` and `PATCH`/`DELETE /household/members/{id}` still accept a personal API token, so a leaked owner token can mint a co-owner or demote the other owner. Same guard, same test shape, no service change.

**Files:**
- Modify: `api/internal/adapter/http/router.go:270-290`
- Modify: `api/cmd/hearthctl/routes.go:43-45`
- Test: `api/internal/adapter/http/pending_invites_api_test.go` (add cases)

**Interfaces:**
- Consumes: nothing.
- Produces: three routes that now answer `401` to a personal API token. Later tasks add routes **inside** the same `requireCookieSession` group.

- [ ] **Step 1: Write the failing test**

Add to `api/internal/adapter/http/pending_invites_api_test.go` (match the file's existing helper names — read the top of the file first; the helpers that sign in an owner and mint a token already exist there and in `api_test.go`):

```go
// A personal API token is a headless credential. It must not be able to
// change who can get into the household: minting a co-owner, demoting the
// other owner, or removing them are all browser-session actions (spec
// decision 12, the reason behind ADR 7 rule 2).
func TestATokenCannotChangeWhoIsInTheHousehold(t *testing.T) {
	env := newAPITestEnv(t)
	owner := env.signInSeededOwner(t)
	token := env.mintAPIToken(t, owner)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"invite", http.MethodPost, "/api/v1/household/members/invite",
			`{"name":"Jane","email":"jane@example.com","role":"owner","capabilities":["money","calendar","chores","marriage"],"channel":"email"}`},
		{"update member", http.MethodPatch, "/api/v1/household/members/" + owner.MembershipID, `{"role":"limited"}`},
		{"remove member", http.MethodDelete, "/api/v1/household/members/" + owner.MembershipID, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := env.doWithToken(t, token, tc.method, tc.path, tc.body)
			if res.Code != http.StatusUnauthorized {
				t.Fatalf("token reached %s %s: got %d, want 401", tc.method, tc.path, res.Code)
			}
		})
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
cd api && go test ./internal/adapter/http/ -run TestATokenCannotChangeWhoIsInTheHousehold -v
```

Expected: FAIL — the invite case answers `201` (or `422` for the unknown `channel` field, which Task 4 adds) and the member cases answer `200`/`204`. Any answer that is not `401` is the failure this task fixes.

- [ ] **Step 3: Move the three routes inside the cookie-session group**

In `api/internal/adapter/http/router.go`, the owner group currently registers invite/member writes outside `requireCookieSession` and withdraw inside it. Make one cookie-session group hold all four:

```go
				m.Group(func(o chi.Router) {
					o.Use(requireOwner)
					o.Patch("/household", handleUpdateHousehold(deps))
					o.Patch("/notification-preferences", handleUpdateNotificationPreferences(deps))
					o.Post("/spaces", handleCreateSpace(deps))

					// Creating, changing or removing a way into the
					// household needs a browser session as well as an
					// owner: a leaked API token must not be able to mint a
					// co-owner, demote the other owner, or remove them
					// (partner-invite spec decision 12, the reason behind
					// ADR 7 rule 2). hearthctl is unaffected -- it signs in
					// with a cookie.
					o.Group(func(c chi.Router) {
						c.Use(requireCookieSession)
						c.Post("/household/members/invite", handleInviteMember(deps))
						c.Patch("/household/members/{id}", handleUpdateMember(deps))
						c.Delete("/household/members/{id}", handleRemoveMember(deps))
						c.Delete("/household/invites/{id}", handleWithdrawInvite(deps))
					})
				})
```

- [ ] **Step 4: Update the hearthctl route table**

`api/cmd/hearthctl/routes.go` is checked against the real router by `routes_test.go`, and the guard string is documentation the test does not check — so fix it by hand:

```go
	{"POST", "/household/members/invite", "owner+browser session+csrf", `{"name","role","capabilities":[...],"channel":"profile"|"email"|"telegram","email"?}` + " -> {id,expiresAt,link?}"},
	{"PATCH", "/household/members/{id}", "owner+browser session+csrf", `{"role"?,"capabilities"?}`},
	{"DELETE", "/household/members/{id}", "owner+browser session+csrf", "-"},
```

(The invite body and response shape land in Tasks 4 and 5; write them now so the table is not edited twice.)

- [ ] **Step 5: Run the tests and watch them pass**

```bash
cd api && go test ./internal/adapter/http/ -run 'TestATokenCannotChangeWhoIsInTheHousehold|Invite|Member' -v && go test ./cmd/hearthctl/ -v
```

Expected: PASS. If `routes_test.go` fails, the route table and the router disagree — that is the test doing its job; fix the table, not the test.

- [ ] **Step 6: Mutation-check the guard**

Delete the `c.Use(requireCookieSession)` line. Run `go test ./internal/adapter/http/ -run TestATokenCannotChangeWhoIsInTheHousehold`. It must fail on all three cases. Put it back.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/http/router.go api/internal/adapter/http/pending_invites_api_test.go api/cmd/hearthctl/routes.go
git commit -m "fix: inviting, changing and removing members need a browser session

Spec decision 12: a leaked API token must not be able to mint a co-owner or
demote the other owner. Milestone 1 guarded only the invite routes."
```

---

## Task 3: Migration 00021, and the channel on the read path

The schema the rest of the milestone writes into, plus the smallest end-to-end slice that proves it: `GET /household/invites` returns `channel` for every row. No behaviour changes for an owner yet — every existing row is an email invite, and the `DEFAULT 'email'` says so.

**Files:**
- Create: `api/migrations/00021_invite_channels.sql`
- Create: `api/internal/domain/invitechannel.go`
- Modify: `api/internal/adapter/postgres/queries/identity.sql` (`ListPendingInvites`)
- Modify: `api/internal/adapter/postgres/invite_repo.go` (`ListPending`)
- Modify: `api/internal/usecase/ports.go` (`InviteSummary`)
- Modify: `api/internal/adapter/http/pending_invite_handlers.go` (`inviteSummaryDTO`)
- Modify: `api/internal/usecase/testdouble_test.go` (`inviteRow`, `inviteDouble.ListPending`)
- Test: `api/internal/adapter/postgres/invite_repo_test.go` (constraint tests)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `domain.InviteChannel` (`string`), constants `domain.ChannelEmail = "email"` and `domain.ChannelTelegram = "telegram"`, and `domain.ParseInviteChannel(s string) (InviteChannel, error)`.
  - `usecase.InviteSummary` gains `Channel domain.InviteChannel` and `Knock *InviteKnock`.
  - `usecase.InviteKnock{Username string; Code string; KnockedAt time.Time}` — `Username` is `""` when Telegram sent none.
  - The list route's JSON row gains `"channel"` and `"knock"` (the latter `null` until Task 7 records one).

- [ ] **Step 1: Write the migration**

Create `api/migrations/00021_invite_channels.sql`:

```sql
-- Telegram invites (partner-invite spec, milestone 2). An invite gains a
-- channel, and a Telegram invite carries its knock on the same row rather
-- than in a table of its own: one knock per link (spec decision 2) means
-- there is never more than one to hold.
--
-- Every existing row is an email invite with an address, so DEFAULT 'email'
-- satisfies invites_channel_matches_email at the moment it is added. Run
-- this against a restored production dump before deploying, as 00011's own
-- comment asks for any migration that constrains a table already holding
-- real rows.

-- +goose Up
ALTER TABLE invites ALTER COLUMN email DROP NOT NULL;

ALTER TABLE invites ADD COLUMN channel text NOT NULL DEFAULT 'email'
    CHECK (channel IN ('email', 'telegram'));

-- An email invite has an address; a Telegram invite has none. Written as an
-- equality between two booleans so neither direction can drift: an email
-- invite with a NULL address and a Telegram invite carrying one are both
-- refused by this single constraint.
ALTER TABLE invites ADD CONSTRAINT invites_channel_matches_email
    CHECK ((channel = 'email') = (email IS NOT NULL));

ALTER TABLE invites ADD COLUMN knock_chat_id       bigint;
ALTER TABLE invites ADD COLUMN knock_chat_username text;
ALTER TABLE invites ADD COLUMN knock_code          text;
ALTER TABLE invites ADD COLUMN knocked_at          timestamptz;

-- A knock is whole or absent. knock_chat_username stays out of it: Telegram
-- legitimately sends no username, so NULL there is data, not a half-written
-- row.
ALTER TABLE invites ADD CONSTRAINT invites_knock_is_whole
    CHECK ((knocked_at IS NULL) = (knock_chat_id IS NULL)
       AND (knocked_at IS NULL) = (knock_code IS NULL));

ALTER TABLE invites ADD CONSTRAINT invites_knock_needs_telegram
    CHECK (knocked_at IS NULL OR channel = 'telegram');

-- A group chat's id is negative. The bot already refuses a group before a
-- knock is ever recorded (adapter/telegram/update.go,
-- isPrivateChatWithItsOwner, security review 2026-09-19 finding 2); this is
-- the second gate, in the place a future caller cannot forget. It is free
-- here because no row has ever had this column.
ALTER TABLE invites ADD CONSTRAINT invites_knock_chat_is_a_person
    CHECK (knock_chat_id IS NULL OR knock_chat_id > 0);

-- +goose Down
ALTER TABLE invites DROP CONSTRAINT invites_knock_chat_is_a_person;
ALTER TABLE invites DROP CONSTRAINT invites_knock_needs_telegram;
ALTER TABLE invites DROP CONSTRAINT invites_knock_is_whole;
ALTER TABLE invites DROP COLUMN knocked_at;
ALTER TABLE invites DROP COLUMN knock_code;
ALTER TABLE invites DROP COLUMN knock_chat_username;
ALTER TABLE invites DROP COLUMN knock_chat_id;
ALTER TABLE invites DROP CONSTRAINT invites_channel_matches_email;
ALTER TABLE invites DROP COLUMN channel;
-- email stays nullable: a Telegram invite written while this migration was
-- applied would fail the old NOT NULL, and a down migration must not lose
-- rows.
```

**Before writing it, open `api/migrations/00020_holding_income.sql` and copy its directive style exactly** (`-- +goose Up` / `-- +goose Down`, or whatever that file uses — match it; do not assume).

- [ ] **Step 2: Write the failing constraint test**

Add to `api/internal/adapter/postgres/invite_repo_test.go`:

```go
// The two CHECK constraints are the schema's own fail-closed rule: a row
// that says one thing in its channel and another in its columns never
// exists, whatever a future caller writes. Asserted against real Postgres
// because a constraint is not a Go rule -- it holds for psql, adminctl and
// anything else that ever writes this table.
func TestInviteChannelConstraintsRefuseHalfWrittenRows(t *testing.T) {
	ctx := context.Background()
	db, h := newInviteTestHousehold(t) // the file's existing helper

	cases := []struct {
		name string
		sql  string
	}{
		{"an email invite with no address",
			`INSERT INTO invites (household_id, email, name, role, capabilities, token_hash, invited_by, expires_at, channel)
			 VALUES ($1, NULL, 'Nobody', 'owner', '{money}', '\x01', $2, now() + interval '1 day', 'email')`},
		{"a telegram invite carrying an address",
			`INSERT INTO invites (household_id, email, name, role, capabilities, token_hash, invited_by, expires_at, channel)
			 VALUES ($1, 'jane@example.com', 'Jane', 'owner', '{money}', '\x02', $2, now() + interval '1 day', 'telegram')`},
		{"a knock with no code",
			`INSERT INTO invites (household_id, email, name, role, capabilities, token_hash, invited_by, expires_at, channel, knock_chat_id, knocked_at)
			 VALUES ($1, NULL, 'Jane', 'owner', '{money}', '\x03', $2, now() + interval '1 day', 'telegram', 4242, now())`},
		{"a knock on an email invite",
			`INSERT INTO invites (household_id, email, name, role, capabilities, token_hash, invited_by, expires_at, channel, knock_chat_id, knock_code, knocked_at)
			 VALUES ($1, 'jane@example.com', 'Jane', 'owner', '{money}', '\x04', $2, now() + interval '1 day', 'email', 4242, '4812', now())`},
		{"a knock from a group chat",
			`INSERT INTO invites (household_id, email, name, role, capabilities, token_hash, invited_by, expires_at, channel, knock_chat_id, knock_code, knocked_at)
			 VALUES ($1, NULL, 'Jane', 'owner', '{money}', '\x05', $2, now() + interval '1 day', 'telegram', -100500, '4812', now())`},
		{"a channel this build does not define",
			`INSERT INTO invites (household_id, email, name, role, capabilities, token_hash, invited_by, expires_at, channel)
			 VALUES ($1, NULL, 'Jane', 'owner', '{money}', '\x06', $2, now() + interval '1 day', 'carrier_pigeon')`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.Pool().Exec(ctx, tc.sql, h.ID, h.OwnerUserID); err == nil {
				t.Fatal("the row was accepted; a CHECK constraint should have refused it")
			}
		})
	}
}
```

Adapt `newInviteTestHousehold` and the household/owner field names to whatever the file already uses — **read the top of `invite_repo_test.go` first** and reuse its fixture rather than inventing a second one.

- [ ] **Step 3: Run it and watch it fail**

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
cd api && go test ./internal/adapter/postgres/ -run TestInviteChannelConstraintsRefuseHalfWrittenRows -v
```

Expected: FAIL with `column "channel" of relation "invites" does not exist` until the migration is applied by the test container's own migrate step. Once it is, the rows are accepted (no constraints yet) — both are the right failure.

- [ ] **Step 4: Add the domain type**

Create `api/internal/domain/invitechannel.go`:

```go
package domain

import "fmt"

// An InviteChannel is how an invite reaches the person it is for. It is
// stored in invites.channel and arrives from a request body, so it is
// parsed rather than cast: a value this build does not define must refuse,
// not fall through to a default that sends something nowhere.
type InviteChannel string

const (
	// ChannelEmail is the original path: an address, a mailed link, seven
	// days. It is gated by FlagEmailInvites, which is off by default while
	// production mail cannot leave the box (ADR 3).
	ChannelEmail InviteChannel = "email"
	// ChannelTelegram is a one-time t.me deep link the owner hands over,
	// with no address at all. It carries its knock on the same row
	// (ADR 11).
	ChannelTelegram InviteChannel = "telegram"
)

// AllInviteChannels is the whole set, so a caller enumerating channels
// cannot miss one that was added later.
func AllInviteChannels() []InviteChannel { return []InviteChannel{ChannelEmail, ChannelTelegram} }

// ParseInviteChannel turns a database column into an InviteChannel,
// refusing anything else -- including "". A request does not come through
// here: the HTTP layer has its own three-valued vocabulary, because a kid
// profile writes no invite row and so has no channel to store
// (adapter/http, parseInviteChannelChoice).
func ParseInviteChannel(s string) (InviteChannel, error) {
	for _, c := range AllInviteChannels() {
		if InviteChannel(s) == c {
			return c, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownInviteChannel, s)
}
```

Add the sentinel to `api/internal/domain/errors.go`, beside the other invite errors:

```go
	ErrUnknownInviteChannel      = errors.New("unknown invite channel")
```

- [ ] **Step 5: Carry the channel and knock out of the repository**

In `api/internal/usecase/ports.go`, extend `InviteSummary` and add `InviteKnock` above it:

```go
// InviteKnock is one tap on a Telegram invite link: who tapped, and the
// four digits their chat was shown. The owner compares those digits with
// the ones on the phone in front of them and then admits (ADR 11).
//
// Code is display-only. No endpoint accepts it, so there is nothing to
// guess and nothing to rate-limit; a test pins that the admit request has
// no code field. Username is "" when Telegram sent none, which is ordinary
// -- a @username is optional -- and the screen says so in words rather than
// rendering an empty "@".
type InviteKnock struct {
	Username  string
	Code      string
	KnockedAt time.Time
}

type InviteSummary struct {
	ID           string
	Name         string
	Email        string // "" for a Telegram invite, which has no address
	Role         domain.Role
	Capabilities domain.Capabilities
	Channel      domain.InviteChannel
	// Knock is nil until someone taps the link, and always nil for an email
	// invite. A pointer rather than a zero-valued struct because "nobody has
	// knocked" and "somebody knocked at the zero time" must not look alike.
	Knock     *InviteKnock
	ExpiresAt time.Time
	CreatedAt time.Time
}
```

In `api/internal/adapter/postgres/queries/identity.sql`, extend the list query:

```sql
-- name: ListPendingInvites :many
-- "Pending" is the partner-invite spec's one definition: not accepted and not
-- expired. $2 is the caller's clock rather than now(), so a test can move it,
-- the same shape ListPendingInvitesForAdmin uses.
SELECT id, email, name, role, capabilities, channel,
       knock_chat_username, knock_code, knocked_at,
       expires_at, created_at
FROM invites
WHERE household_id = $1 AND accepted_at IS NULL AND expires_at > $2
ORDER BY created_at, id;
```

Regenerate sqlc, then map the new columns in `invite_repo.go`'s `ListPending`, reading the channel through the parser the same way role and capabilities are read:

```go
		channel, err := domain.ParseInviteChannel(row.Channel)
		if err != nil {
			return nil, err
		}
		summary := usecase.InviteSummary{
			ID:           uuidToString(row.ID),
			Name:         row.Name,
			Email:        stringOrEmpty(row.Email), // "" for NULL -- see nullableText's inverse
			Role:         role,
			Capabilities: caps,
			Channel:      channel,
			ExpiresAt:    timeOf(row.ExpiresAt),
			CreatedAt:    timeOf(row.CreatedAt),
		}
		// knocked_at is the column invites_knock_is_whole ties the other two
		// to, so it alone decides whether there is a knock to report.
		if row.KnockedAt.Valid {
			summary.Knock = &usecase.InviteKnock{
				Username:  stringOrEmpty(row.KnockChatUsername),
				Code:      stringOrEmpty(row.KnockCode),
				KnockedAt: timeOf(row.KnockedAt),
			}
		}
		out = append(out, summary)
```

The NULL-to-`""` helper is `stringOrEmpty` in `api/internal/adapter/postgres/convert.go:38` (`nullableText` is its inverse). Use it; do not add a second one.

- [ ] **Step 6: Regenerate sqlc, and expect four call sites to break**

```bash
make sqlc          # runs: cd api && go tool sqlc generate
cd api && go build ./...
```

`ALTER COLUMN email DROP NOT NULL` makes sqlc regenerate **every** query that selects `invites.email` as `*string` instead of `string`. Four places stop compiling, and each needs `stringOrEmpty`:

| File | What breaks |
|---|---|
| `api/internal/adapter/postgres/invite_repo.go:64` | `InviteDetails.Email` from `GetInviteByTokenHash` |
| `api/internal/adapter/postgres/invite_repo.go` (`LiveInviteForEmail`) | same, from `GetLiveInviteForEmail` |
| `api/internal/adapter/postgres/invite_repo.go` (`ListPending`) | handled in Step 5 |
| `api/internal/adapter/postgres/admin_directory_repo.go:165,175,179` | `ListPendingInvitesForAdmin`'s `i.email` |

This is a compile error, not a silent change, which is the point of the nullable column. Fix each with `stringOrEmpty(row.Email)` and change nothing else — `""` is what every caller above the adapter already means by "no address". Task 13 then gives the admin screen the words for it.

Editor diagnostics lag behind the tree; `go build ./...` is the truth.

- [ ] **Step 7: Put the channel on the wire**

In `api/internal/adapter/http/pending_invite_handlers.go`:

```go
// inviteKnockDTO is the knock half of a row. Username is a pointer because
// Telegram legitimately sends none, and the frontend renders the two cases
// with different words -- "@jane_t tapped the link" against "Someone with
// no Telegram username tapped the link" -- so "" and absent must not
// collapse into each other.
type inviteKnockDTO struct {
	Username  *string   `json:"username"`
	Code      string    `json:"code"`
	KnockedAt time.Time `json:"knockedAt"`
}

type inviteSummaryDTO struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Email        string          `json:"email"`
	Role         string          `json:"role"`
	Capabilities []string        `json:"capabilities"`
	Channel      string          `json:"channel"`
	Knock        *inviteKnockDTO `json:"knock"`
	ExpiresAt    time.Time       `json:"expiresAt"`
}
```

and in the loop:

```go
			row := inviteSummaryDTO{
				ID:           invite.ID,
				Name:         invite.Name,
				Email:        invite.Email,
				Role:         string(invite.Role),
				Capabilities: invite.Capabilities.Strings(),
				Channel:      string(invite.Channel),
				ExpiresAt:    invite.ExpiresAt,
			}
			if invite.Knock != nil {
				row.Knock = &inviteKnockDTO{Code: invite.Knock.Code, KnockedAt: invite.Knock.KnockedAt}
				if invite.Knock.Username != "" {
					username := invite.Knock.Username
					row.Knock.Username = &username
				}
			}
			out = append(out, row)
```

- [ ] **Step 8: Teach the in-memory double the same columns**

In `api/internal/usecase/testdouble_test.go`, add `Channel domain.InviteChannel` and the four knock fields to `inviteRow`, default `Channel` to `domain.ChannelEmail` in `inviteDouble.Create`, and copy both into the `InviteSummary` that `ListPending` returns. The double mirrors the real repository; a field it does not model is a field the usecase tests cannot see.

- [ ] **Step 9: Run everything and watch it pass**

```bash
cd api && go test ./... 2>&1 | tail -20
```

Expected: PASS. `TestInviteChannelConstraintsRefuseHalfWrittenRows` now refuses all six rows.

- [ ] **Step 10: Mutation check 1 — the household filter**

Delete `household_id = $1 AND` from `ListPendingInvites`, regenerate sqlc, and run `go test ./internal/adapter/postgres/ -run Pending`. The milestone-1 test that lists another household's invites must fail. Put it back and regenerate.

- [ ] **Step 11: Commit**

```bash
git add api/migrations/00021_invite_channels.sql api/internal/domain/invitechannel.go api/internal/domain/errors.go api/internal/adapter/postgres/queries/identity.sql api/internal/adapter/postgres/sqlcgen api/internal/adapter/postgres/invite_repo.go api/internal/adapter/postgres/invite_repo_test.go api/internal/usecase/ports.go api/internal/usecase/testdouble_test.go api/internal/adapter/http/pending_invite_handlers.go
git commit -m "feat: invites carry a channel, and a Telegram invite carries its knock

Migration 00021. Every existing row is an email invite, which the default
says. The knock columns are written by nobody yet; the CHECK constraints
that keep them whole are in place first."
```

---

## Task 4: The `email_invites` flag, refused at the edge as well as in the UI

Spec decision 10. Production mail never leaves the box, so an email invite lands in the operator's Mailpit and the partner never gets it. Hide the channel behind a flag that is **off by default**, and refuse it at the HTTP edge too — otherwise `hearthctl` or a crafted request still creates an invite that can never be delivered.

`adminctl create-invite` is deliberately unaffected: it calls `InviteService` directly, not through the HTTP edge, and printing the captured URL is how an operator hands an invite over today.

**Files:**
- Modify: `api/internal/domain/featureflag.go`
- Modify: `api/internal/domain/errors.go`
- Modify: `api/internal/adapter/http/errors.go` (`domainErrorResponses`)
- Modify: `api/internal/adapter/http/member_handlers.go` (`inviteMemberRequest`, `handleInviteMember`)
- Test: `api/internal/adapter/http/pending_invites_api_test.go`

**Interfaces:**
- Consumes: `domain.InviteChannel` and its constants (Task 3).
- Produces:
  - `domain.FlagEmailInvites Flag = "email_invites"`, default `false`, listed in `AllFlags()`.
  - `domain.ErrEmailInvitesDisabled` → `409 EMAIL_INVITES_DISABLED` "Email can't leave this install yet. Use a Telegram link."
  - `domain.ErrTelegramInvitesUnavailable` → `409 TELEGRAM_INVITES_UNAVAILABLE` "Inviting by Telegram is unavailable on this install."
  - `inviteMemberRequest` gains `Channel string \`json:"channel"\`` — parsed, never defaulted.
  - `httpadapter.inviteChannelChoice` with three values: `"profile"`, `"email"`, `"telegram"`, and `parseInviteChannelChoice(s string) (inviteChannelChoice, error)`.
  - `/me`'s `features` map already carries every flag to the frontend; no change needed there.

**Why a third value that the database never stores.** The spec asks for two things that collide if you read them alone: `channel` is "parsed with a default that refuses", and "the kid-profile path stays as it is today". A kid profile writes **no invite row at all** — the member is created directly, with no token and nowhere for a link to go — so it has no channel in `domain`'s sense, and a body that omits the field would be refused by the fail-closed parser.

Resolve it by saying the third case out loud at the edge rather than inferring it from `role == "limited"` and an absent field. Inference is what sends an invite down a channel nobody meant. So:

- `domain.InviteChannel` keeps exactly the two values the column stores (`email`, `telegram`). It is a **stored** vocabulary.
- `inviteChannelChoice` lives in the HTTP layer and has three. It is a **request** vocabulary, and `profile` is an answer to "how does this person sign in?" — namely, they do not.
- `profile` is valid only with `role: "limited"`; any other role with `profile` is refused, exactly as the service already refuses an owner with no email today (`domain.ErrInviteRequiresEmail`).

- [ ] **Step 1: Write the failing test**

```go
// Email invites are hidden while mail cannot leave the box (ADR 3). The
// flag is not only a UI affordance: hearthctl and any crafted request reach
// the same route, and an invite nobody can deliver is worse than a refusal
// the owner can read (spec decision 10).
func TestEmailInviteIsRefusedWhileTheFlagIsOff(t *testing.T) {
	env := newAPITestEnv(t)
	owner := env.signInSeededOwner(t)

	res := env.do(t, owner, http.MethodPost, "/api/v1/household/members/invite",
		`{"name":"Jane","email":"jane@example.com","role":"owner","capabilities":["money","calendar","chores","marriage"],"channel":"email"}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("email invite with the flag off: got %d, want 409", res.Code)
	}
	if code := errorCodeOf(t, res); code != "EMAIL_INVITES_DISABLED" {
		t.Fatalf("got error code %q, want EMAIL_INVITES_DISABLED", code)
	}
}

// A channel this build does not define is refused before anything is
// written, and so is an absent one: the spec asks for a default that
// refuses rather than one that guesses.
func TestInviteChannelMustBeNamedExplicitly(t *testing.T) {
	env := newAPITestEnv(t)
	owner := env.signInSeededOwner(t)

	for name, body := range map[string]string{
		"absent":  `{"name":"Jane","role":"owner","capabilities":["money","calendar","chores","marriage"]}`,
		"empty":   `{"name":"Jane","role":"owner","capabilities":["money","calendar","chores","marriage"],"channel":""}`,
		"unknown": `{"name":"Jane","role":"owner","capabilities":["money","calendar","chores","marriage"],"channel":"carrier_pigeon"}`,
		"profile for an owner": `{"name":"Jane","role":"owner","capabilities":["money","calendar","chores","marriage"],"channel":"profile"}`,
	} {
		t.Run(name, func(t *testing.T) {
			res := env.do(t, owner, http.MethodPost, "/api/v1/household/members/invite", body)
			if res.Code == http.StatusCreated {
				t.Fatalf("channel %s was accepted; it must be refused", name)
			}
		})
	}
}

// The kid profile is unchanged by this milestone: a limited member with no
// sign-in, created directly, with no invite row and no link. It says
// "profile" out loud rather than being inferred from an absent field,
// because inference is how an invite goes somewhere nobody meant.
func TestAProfileOnlyMemberIsStillCreatedDirectly(t *testing.T) {
	env := newAPITestEnv(t)
	owner := env.signInSeededOwner(t)

	res := env.do(t, owner, http.MethodPost, "/api/v1/household/members/invite",
		`{"name":"Kayla","role":"limited","capabilities":["calendar","chores"],"channel":"profile"}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201: %s", res.Code, res.Body.String())
	}
	// No invite row: the member exists already, so there is nothing pending.
	list := env.do(t, owner, http.MethodGet, "/api/v1/household/invites", "")
	if strings.Contains(list.Body.String(), "Kayla") {
		t.Fatal("a kid profile wrote an invite row")
	}
	members := env.do(t, owner, http.MethodGet, "/api/v1/household/members", "")
	if !strings.Contains(members.Body.String(), "Kayla") {
		t.Fatal("the kid profile was not created")
	}
}

// With the flag on, the email path is exactly what it was before this
// milestone: the flag hides a channel, it does not change one.
func TestEmailInviteWorksWhenTheOperatorTurnsTheFlagOn(t *testing.T) {
	env := newAPITestEnv(t)
	env.setGlobalFlag(t, "email_invites", true)
	owner := env.signInSeededOwner(t)

	res := env.do(t, owner, http.MethodPost, "/api/v1/household/members/invite",
		`{"name":"Jane","email":"jane@example.com","role":"owner","capabilities":["money","calendar","chores","marriage"],"channel":"email"}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("email invite with the flag on: got %d, want 201", res.Code)
	}
}
```

`errorCodeOf` and `setGlobalFlag` may not exist under those names — **read `api_test.go` and the admin flag tests first** and reuse whatever helper already decodes an error body and sets a global flag override. Add one only if there is none.

- [ ] **Step 2: Run them and watch them fail**

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
cd api && go test ./internal/adapter/http/ -run 'EmailInvite|InviteChannelMustBeNamed' -v
```

Expected: FAIL — every case answers `201`, because `channel` is an unknown field today and the handler ignores it.

- [ ] **Step 3: Add the flag**

In `api/internal/domain/featureflag.go`, one const and one line — that is the whole registry:

```go
	// FlagEmailInvites gates the email channel on an invite. Default off:
	// production mail never leaves the box (ADR 3), so an email invite
	// lands in the operator's Mailpit and the partner never receives it.
	// This follows FlagNotificationDelivery's own reasoning -- a flag that
	// is on for something that cannot happen is a lie. The operator turns
	// it on in /admin the day real mail exists, with no code change.
	FlagEmailInvites Flag = "email_invites"
```

```go
		{FlagEmailInvites, "Offer email as an invite channel. Off while mail cannot leave the box.", false},
```

- [ ] **Step 4: Add the two sentinels and their responses**

`api/internal/domain/errors.go`:

```go
	ErrEmailInvitesDisabled       = errors.New("email invites are disabled on this install")
	ErrTelegramInvitesUnavailable = errors.New("telegram invites are unavailable on this install")
```

`api/internal/adapter/http/errors.go`, beside the other invite rows:

```go
	{
		sentinels: []error{domain.ErrEmailInvitesDisabled},
		status:    http.StatusConflict,
		code:      "EMAIL_INVITES_DISABLED",
		message:   "Email can't leave this install yet. Use a Telegram link.",
	},
	{
		sentinels: []error{domain.ErrTelegramInvitesUnavailable},
		status:    http.StatusConflict,
		code:      "TELEGRAM_INVITES_UNAVAILABLE",
		message:   "Inviting by Telegram is unavailable on this install.",
	},
	{
		sentinels: []error{domain.ErrUnknownInviteChannel},
		status:    http.StatusUnprocessableEntity,
		code:      "INVALID_INVITE_CHANNEL",
		message:   "That invite channel is not valid.",
	},
```

- [ ] **Step 5: Parse and enforce the channel at the edge**

In `api/internal/adapter/http/member_handlers.go`, add the field to `inviteMemberRequest` and switch on it in `handleInviteMember`, after the role and capability parsing:

```go
		choice, err := parseInviteChannelChoice(req.Channel)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		// The flag is enforced here as well as in the modal, because this
		// route is reachable from hearthctl and from anything else holding
		// a session: an invite that can never be delivered must not be
		// creatable at all (spec decision 10). adminctl is deliberately
		// outside this gate -- it calls InviteService directly and prints
		// the URL it captured, which is how an operator hands an invite
		// over today.
		switch choice {
		case channelChoiceProfile:
			// Today's kid path, untouched: Create's own empty-email branch
			// creates the member directly and writes no invite row. It
			// refuses any role but limited, which is the check this arm
			// deliberately does not repeat -- one rule, one place.
			if err := deps.Invites.Create(r.Context(), scope.HouseholdID, scope.UserID,
				req.Name, "", role, caps); err != nil {
				MapDomainError(w, r, err)
				return
			}
		case channelChoiceEmail:
			if !scope.Flags.Enabled(domain.FlagEmailInvites) {
				MapDomainError(w, r, domain.ErrEmailInvitesDisabled)
				return
			}
		case channelChoiceTelegram:
			// Task 5 fills this in. Until then it refuses, which is the
			// correct answer for a channel this build cannot yet deliver.
			MapDomainError(w, r, domain.ErrTelegramInvitesUnavailable)
			return
		default:
			// Unreachable while parseInviteChannelChoice is the only way
			// in, and present anyway: a choice added without a case here
			// refuses rather than falling through to the email path.
			MapDomainError(w, r, domain.ErrUnknownInviteChannel)
			return
		}
```

The email arm still falls through to the existing `deps.Invites.Create(...)` call and its `201 {"status":"invited"}` body; Task 5 changes the response shape once there is a link to return. The profile arm calls `Create` with an empty email, which is exactly what the modal sends today — that path is not being rewritten, only named.

Define the choice type beside the handler:

```go
// An inviteChannelChoice is how the person being added will sign in, as the
// request says it. It has one value more than domain.InviteChannel, and the
// extra one is the point: "profile" means they never sign in at all, so no
// invite row is written and there is no channel to store. Keeping it out of
// the domain type keeps that type equal to what the column holds.
type inviteChannelChoice string

const (
	channelChoiceProfile  inviteChannelChoice = "profile"
	channelChoiceEmail    inviteChannelChoice = "email"
	channelChoiceTelegram inviteChannelChoice = "telegram"
)

// parseInviteChannelChoice refuses anything else, "" included -- which is
// what an omitted field decodes to. A caller must say how this person signs
// in; inferring it from which other fields happen to be filled in is how an
// invite goes somewhere nobody meant.
func parseInviteChannelChoice(s string) (inviteChannelChoice, error) {
	switch inviteChannelChoice(s) {
	case channelChoiceProfile:
		return channelChoiceProfile, nil
	case channelChoiceEmail:
		return channelChoiceEmail, nil
	case channelChoiceTelegram:
		return channelChoiceTelegram, nil
	default:
		return "", fmt.Errorf("%w: %q", domain.ErrUnknownInviteChannel, s)
	}
}
```

- [ ] **Step 6: Run the tests and watch them pass**

```bash
cd api && go test ./internal/adapter/http/ -run 'EmailInvite|InviteChannelMustBeNamed' -v
```

Expected: PASS. Other tests in the package that post an invite body without `channel` now get a refusal — **that is this task's blast radius**. Add `"channel":"email"` to those bodies and `env.setGlobalFlag(t, "email_invites", true)` to those tests; each one is asserting the email path, which the flag hides, not the channel choice.

- [ ] **Step 7: Check the frontend's own invite tests**

`web/src/features/settings/*.test.tsx` post the same body through `stubFetchRoutes`. They do not reach the Go edge, so they keep passing — but `useInviteMember` changes in Task 12, and these tests change with it. Do nothing here.

- [ ] **Step 8: Commit**

```bash
git add api/internal/domain/featureflag.go api/internal/domain/errors.go api/internal/adapter/http/errors.go api/internal/adapter/http/member_handlers.go api/internal/adapter/http/pending_invites_api_test.go
git commit -m "feat: email invites sit behind a flag, off by default

Spec decision 10. Mail cannot leave the box (ADR 3), so an email invite
lands in the operator's Mailpit. The flag is enforced at the HTTP edge as
well as in the modal: hearthctl reaches the same route."
```

---

## Task 5: Creating a Telegram invite, and the link that comes back once

The owner picks Telegram, and the response carries a `t.me/<bot>?start=inv_<token>` link. The raw token is never stored and never shown again by the list route — getting another one is Task 8's job.

**Files:**
- Modify: `api/internal/usecase/ports.go` (`InviteRepository.CreateTelegram`, `PairingCodes` declared for Task 7, `InviteDeps`)
- Modify: `api/internal/usecase/invite.go` (`CreateTelegram`)
- Modify: `api/internal/adapter/postgres/queries/identity.sql` (`CreateTelegramInvite`)
- Modify: `api/internal/adapter/postgres/invite_repo.go`
- Modify: `api/internal/adapter/http/member_handlers.go`
- Modify: `api/cmd/api/main.go`, `api/internal/adapter/http/api_test.go` (wiring)
- Test: `api/internal/usecase/invite_test.go`, `api/internal/adapter/http/pending_invites_api_test.go`

**Interfaces:**
- Consumes: `domain.ChannelTelegram`, `domain.ErrTelegramInvitesUnavailable` (Tasks 3, 4).
- Produces:
  - ```go
    // TelegramInviteLink is a one-time deep link, returned once at creation
    // and once per new link. The raw token it carries is never stored.
    type TelegramInviteLink struct {
        ID        string
        URL       string
        ExpiresAt time.Time
    }
    ```
  - `func (s *InviteService) CreateTelegram(ctx context.Context, householdID, invitedByUserID, name string, role domain.Role, caps domain.Capabilities) (TelegramInviteLink, error)`
  - `InviteRepository.CreateTelegram(ctx context.Context, householdID, name string, role domain.Role, caps domain.Capabilities, tokenHash []byte, invitedBy string, expiresAt time.Time) (string, error)`
  - `InviteDeps` gains `BotUsername string` and `TelegramInviteTTL time.Duration`.
  - `POST /household/members/invite` answers `201 {"id":"…","expiresAt":"…","link":"https://t.me/…"}`; `link` is present only for a Telegram invite.

**Why a second service method rather than a `channel` parameter on `Create`:** `Create` has seven parameters already and three production callers (`seed.go:298`, `seed.go:442`, `cmd/adminctl/main.go:380`), none of which will ever want a Telegram invite. Two methods keep each path's contract readable — the email one requires an address, the Telegram one has no place to put one — which is exactly spec decision 9's rule that an invite for someone who will sign in has exactly one channel.

- [ ] **Step 1: Write the failing usecase test**

In `api/internal/usecase/invite_test.go`:

```go
// A Telegram invite has no address anywhere: not in the request, not in the
// row, not in any mail. ErrInviteRequiresEmail guarded delivery, not
// identity -- users.email is already nullable and Telegram sign-up already
// creates owners with no address (spec decision 9).
func TestCreateTelegramWritesARowWithNoEmailAndReturnsTheLinkOnce(t *testing.T) {
	svc, doubles := newInviteService(t) // the file's existing fixture

	link, err := svc.CreateTelegram(context.Background(), householdID, ownerID, "Christine",
		domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	if !strings.HasPrefix(link.URL, "https://t.me/HearthBot?start=inv_") {
		t.Fatalf("link %q does not carry the inv_ payload prefix migration 00018 reserved", link.URL)
	}
	// 47 characters: "inv_" plus NewToken's 43 base64url characters, under
	// Telegram's 64-character start limit.
	payload := link.URL[strings.Index(link.URL, "start=")+len("start="):]
	if len(payload) != 47 {
		t.Fatalf("payload is %d characters; Telegram's start limit is 64 and the spec budgets 47", len(payload))
	}
	if got, want := link.ExpiresAt, doubles.clock.now.Add(24*time.Hour); !got.Equal(want) {
		t.Fatalf("expires at %v, want %v (spec decision 8: 24 hours)", got, want)
	}

	row := doubles.invites.byID(link.ID)
	if row == nil {
		t.Fatal("no invite row was written")
	}
	if row.Email != "" {
		t.Fatalf("a Telegram invite carries no address, got %q", row.Email)
	}
	if row.Channel != domain.ChannelTelegram {
		t.Fatalf("channel is %q, want telegram", row.Channel)
	}
	if doubles.mailer.sentCount() != 0 {
		t.Fatal("a Telegram invite must send no mail at all")
	}
}

// The list route never shows the link again: the raw token is not stored,
// so there is nothing to show. This pins that the summary has no way to
// carry one.
func TestPendingListNeverCarriesAnInviteLink(t *testing.T) {
	svc, _ := newInviteService(t)
	link, err := svc.CreateTelegram(context.Background(), householdID, ownerID, "Christine",
		domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	summaries, err := svc.ListPending(context.Background(), householdID)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	for _, s := range summaries {
		if s.ID == link.ID && strings.Contains(fmt.Sprint(s), "t.me") {
			t.Fatal("a pending row carried the deep link; it is shown once, at creation")
		}
	}
}
```

Reuse the fixture and identifiers `invite_test.go` already has (`newInviteService`, its household and owner ids, `mailerDouble`) — read the top of the file rather than inventing new ones.

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/usecase/ -run CreateTelegram -v
```

Expected: FAIL — `svc.CreateTelegram undefined`.

- [ ] **Step 3: Add the port and the deps**

In `api/internal/usecase/ports.go`, inside `InviteRepository`:

```go
	// CreateTelegram writes a telegram-channel invite: no email address at
	// all, which invites_channel_matches_email in migration 00021 requires
	// for this channel. Returns the new invite's id. A colliding token hash
	// reports domain.ErrAlreadyExists, exactly as Create does.
	CreateTelegram(ctx context.Context, householdID, name string, role domain.Role,
		caps domain.Capabilities, tokenHash []byte, invitedBy string, expiresAt time.Time) (string, error)
```

and extend `InviteDeps` in `api/internal/usecase/invite.go`:

```go
	// BotUsername is the @name in the t.me deep link. Empty means no bot is
	// configured on this install, which is one of the two conditions that
	// make a Telegram invite impossible (spec decision 11).
	BotUsername string
	// TelegramInviteTTL is 24 hours (spec decision 8). Email invites keep
	// inviteTTL's seven days: getting a new Telegram link is one click, so
	// a short life costs almost nothing, and a link forgotten in a chat
	// history dies the next day.
	TelegramInviteTTL time.Duration
```

- [ ] **Step 4: Write the service method**

In `api/internal/usecase/invite.go`:

```go
// TelegramInviteLink is a one-time deep link, returned once at creation and
// once per new link (NewLink). The raw token is never stored -- only its
// hash is -- so nothing can show this URL a second time.
type TelegramInviteLink struct {
	ID        string
	URL       string
	ExpiresAt time.Time
}

// CreateTelegram writes an invite nobody has to have an email address for,
// and returns the deep link the owner hands over.
//
// There is no ErrInviteeAlreadyRegistered pre-check here, and that absence
// is deliberate: that check exists because users.email is unique, so a
// second account for the same address could never be created. A Telegram
// invite has no address, and the collision it *can* hit -- the chat already
// belonging to a Hearth account -- is not knowable at creation time,
// because nobody has tapped yet. It is checked at the knock (spec decision
// 15) and again inside Admit's transaction.
func (s *InviteService) CreateTelegram(ctx context.Context, householdID, invitedByUserID, name string,
	role domain.Role, caps domain.Capabilities) (TelegramInviteLink, error) {
	if s.d.BotUsername == "" {
		return TelegramInviteLink{}, domain.ErrTelegramInvitesUnavailable
	}
	if _, err := domain.NewMembership("", householdID, "", role, caps); err != nil {
		return TelegramInviteLink{}, err
	}

	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return TelegramInviteLink{}, fmt.Errorf("generate telegram invite token: %w", err)
	}
	expiresAt := s.d.Clock.Now().Add(s.d.TelegramInviteTTL)
	id, err := s.d.Invites.CreateTelegram(ctx, householdID, name, role, caps, hash, invitedByUserID, expiresAt)
	if err != nil {
		return TelegramInviteLink{}, err
	}
	return TelegramInviteLink{ID: id, URL: s.telegramInviteURL(raw), ExpiresAt: expiresAt}, nil
}

// telegramInviteURL is the one place the inv_ prefix is written. Migration
// 00018's comment reserved it so an invite routes by payload rather than
// through telegram_link_requests; HandleStart strips it before handing the
// rest to the knocker.
func (s *InviteService) telegramInviteURL(rawToken string) string {
	return fmt.Sprintf("https://t.me/%s?start=%s%s", s.d.BotUsername, telegramInvitePayloadPrefix, rawToken)
}
```

and the shared constant, at the top of the file beside `inviteTTL`:

```go
// telegramInvitePayloadPrefix routes a /start payload to an invite rather
// than to the sign-in nonce table. Reserved by migration 00018's own
// comment. "inv_" plus NewToken's 43 base64url characters is 47, under
// Telegram's 64-character start limit -- check that arithmetic again before
// ever lengthening either half.
const telegramInvitePayloadPrefix = "inv_"
```

- [ ] **Step 5: Implement the repository method**

`api/internal/adapter/postgres/queries/identity.sql`:

```sql
-- name: CreateTelegramInvite :one
-- No email column at all, which is what invites_channel_matches_email
-- requires of this channel (migration 00021).
INSERT INTO invites (household_id, name, role, capabilities, token_hash, invited_by, expires_at, channel)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'telegram')
RETURNING id;
```

`api/internal/adapter/postgres/invite_repo.go`:

```go
func (r *InviteRepo) CreateTelegram(ctx context.Context, householdID, name string, role domain.Role,
	caps domain.Capabilities, tokenHash []byte, invitedBy string, expiresAt time.Time) (string, error) {
	id, err := r.q.CreateTelegramInvite(ctx, sqlcgen.CreateTelegramInviteParams{
		HouseholdID:  uuid(householdID),
		Name:         name,
		Role:         string(role),
		Capabilities: caps.Strings(),
		TokenHash:    tokenHash,
		InvitedBy:    uuid(invitedBy),
		ExpiresAt:    timestamptz(expiresAt),
	})
	if err != nil {
		return "", translate(err, "create telegram invite")
	}
	return uuidToString(id), nil
}
```

Add the same method to `inviteDouble` in `api/internal/usecase/testdouble_test.go`, writing a row with `Email: ""` and `Channel: domain.ChannelTelegram`.

- [ ] **Step 6: Return the link from the route**

In `api/internal/adapter/http/member_handlers.go`, replace Task 4's `ChannelTelegram` refusal and the `{"status":"invited"}` body:

```go
// inviteCreatedDTO answers both channels. Link is omitted for an email
// invite, which has none, rather than sent as an empty string: the
// frontend switches on its presence to decide whether to show the waiting
// card. Every 2xx except 204 carries a JSON body (CLAUDE.md), so this is
// returned on the email path too.
type inviteCreatedDTO struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expiresAt"`
	Link      string    `json:"link,omitempty"`
}
```

```go
		switch choice {
		case channelChoiceProfile:
			if err := deps.Invites.Create(r.Context(), scope.HouseholdID, scope.UserID,
				req.Name, "", role, caps); err != nil {
				MapDomainError(w, r, err)
				return
			}
			WriteJSON(w, http.StatusCreated, inviteCreatedDTO{})
		case channelChoiceEmail:
			if !scope.Flags.Enabled(domain.FlagEmailInvites) {
				MapDomainError(w, r, domain.ErrEmailInvitesDisabled)
				return
			}
			if err := deps.Invites.Create(r.Context(), scope.HouseholdID, scope.UserID,
				req.Name, req.Email, role, caps); err != nil {
				MapDomainError(w, r, err)
				return
			}
			// The email path has no id to report: Create predates this
			// response shape and returns none. It is the deprecated channel
			// and gains nothing from being reshaped -- the frontend reads
			// only `link`, and the list route supplies the row.
			WriteJSON(w, http.StatusCreated, inviteCreatedDTO{})
		case channelChoiceTelegram:
			if !scope.Flags.Enabled(domain.FlagTelegramSignIn) {
				MapDomainError(w, r, domain.ErrTelegramInvitesUnavailable)
				return
			}
			link, err := deps.Invites.CreateTelegram(r.Context(), scope.HouseholdID, scope.UserID,
				req.Name, role, caps)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			WriteJSON(w, http.StatusCreated, inviteCreatedDTO{ID: link.ID, ExpiresAt: link.ExpiresAt, Link: link.URL})
		default:
			MapDomainError(w, r, domain.ErrUnknownInviteChannel)
			return
		}
```

Spec decision 11: a Telegram invite needs **both** `telegram_sign_in` on and a bot configured. The flag is checked here; the missing bot is `CreateTelegram`'s own `domain.ErrTelegramInvitesUnavailable`, so both arrive as the same 409.

- [ ] **Step 7: Wire the two new deps in both places**

`docs/LEARNING.md` pattern 23: a port added to a service must be wired in `cmd/api/main.go` **and** in `api_test.go`'s own `Deps` literal, or the route panics on a nil interface in tests while production works.

`api/cmd/api/main.go`, in the `usecase.InviteDeps{…}` literal (line ~151):

```go
		BotUsername:       cfg.TelegramBotUsername,
		TelegramInviteTTL: usecase.TelegramInviteTTL,
```

`api/internal/adapter/http/api_test.go`, in its own literal (line ~259), the same two fields with a literal bot name (`"HearthBot"`). Read how `main.go` names the existing bot-username config field (`TelegramAuthDeps.BotUsername` is already wired there) and use the same source.

Export the TTL from `usecase` so both wirings name the same constant:

```go
// TelegramInviteTTL is 24 hours (partner-invite spec decision 8), exported
// so main.go and the test wiring cannot drift apart on it.
const TelegramInviteTTL = 24 * time.Hour
```

- [ ] **Step 8: Write the HTTP test**

```go
func TestCreatingATelegramInviteReturnsTheLinkOnce(t *testing.T) {
	env := newAPITestEnv(t)
	owner := env.signInSeededOwner(t)

	res := env.do(t, owner, http.MethodPost, "/api/v1/household/members/invite",
		`{"name":"Christine","role":"owner","capabilities":["money","calendar","chores","marriage"],"channel":"telegram"}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201: %s", res.Code, res.Body.String())
	}
	var body struct {
		ID        string `json:"id"`
		Link      string `json:"link"`
		ExpiresAt string `json:"expiresAt"`
	}
	decodeBody(t, res, &body)
	if !strings.Contains(body.Link, "?start=inv_") {
		t.Fatalf("link %q carries no inv_ payload", body.Link)
	}

	// The list route shows the invite but never the link again.
	list := env.do(t, owner, http.MethodGet, "/api/v1/household/invites", "")
	if strings.Contains(list.Body.String(), "t.me") {
		t.Fatal("the pending list leaked the deep link")
	}
	if !strings.Contains(list.Body.String(), `"channel":"telegram"`) {
		t.Fatalf("the pending list does not report the channel: %s", list.Body.String())
	}
}
```

- [ ] **Step 9: Run everything and watch it pass**

```bash
cd api && go build ./... && go test ./internal/usecase/ ./internal/adapter/http/ ./internal/adapter/postgres/ 2>&1 | tail -20
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add api/internal/usecase/ports.go api/internal/usecase/invite.go api/internal/usecase/invite_test.go api/internal/usecase/testdouble_test.go api/internal/adapter/postgres/queries/identity.sql api/internal/adapter/postgres/sqlcgen api/internal/adapter/postgres/invite_repo.go api/internal/adapter/http/member_handlers.go api/internal/adapter/http/pending_invites_api_test.go api/cmd/api/main.go api/internal/adapter/http/api_test.go
git commit -m "feat: an owner can create a Telegram invite and gets the link once

No email address anywhere on this path. The raw token is never stored, so
the list route cannot show the link again -- getting another one is its own
route."
```

---

## Task 6: The web accept route refuses a Telegram invite

Spec decision 7, and mutation check 4. Without this, anyone holding the raw token accepts through the browser form with a password of their own and skips the owner's Let in entirely — the whole protection this milestone adds, walked around in one request.

Do this **before** the knock, so the hole never exists on the branch.

**Files:**
- Modify: `api/internal/usecase/invite.go` (`Preview`, `Accept`)
- Modify: `api/internal/usecase/ports.go` (`InviteDetails` gains `Channel`)
- Modify: `api/internal/adapter/postgres/queries/identity.sql` (two selects)
- Modify: `api/internal/adapter/postgres/invite_repo.go`
- Test: `api/internal/usecase/invite_test.go`, `api/internal/adapter/http/` (the public invite tests)

**Interfaces:**
- Consumes: `domain.InviteChannel` (Task 3), `CreateTelegram` (Task 5).
- Produces: `usecase.InviteDetails` gains `Channel domain.InviteChannel`. `GET /invites/{token}` and `POST /invites/{token}/accept` answer a Telegram invite's token **exactly as they answer an unknown one**.

- [ ] **Step 1: Write the failing test**

In `api/internal/usecase/invite_test.go`:

```go
// A Telegram invite is admitted by its household's owner, in their own
// browser, and nowhere else (spec decisions 4 and 7). The public web form
// must therefore treat its token as though it had never existed -- not
// refuse it with a reason, which would confirm the token is real.
func TestTheWebFormCannotAcceptATelegramInvite(t *testing.T) {
	svc, doubles := newInviteService(t)
	ctx := context.Background()

	link, err := svc.CreateTelegram(ctx, householdID, ownerID, "Christine",
		domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	rawToken := doubles.tokens.lastRaw() // the fixture's own accessor

	if _, err := svc.Preview(ctx, rawToken); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Preview of a Telegram invite: got %v, want domain.ErrNotFound", err)
	}
	if _, err := svc.Accept(ctx, rawToken, "a-long-enough-password", "Christine"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Accept of a Telegram invite: got %v, want domain.ErrNotFound", err)
	}
	// Nothing was written: no user, no membership, and the invite is still
	// waiting for its knock.
	if row := doubles.invites.byID(link.ID); row.AcceptedAt != nil {
		t.Fatal("the invite was stamped accepted by the web form")
	}
	if doubles.users.count() != 1 { // just the owner the fixture seeded
		t.Fatal("the web form created a user for a Telegram invite")
	}
}
```

Use whatever accessor the fixture already has for the last raw token (`seqTokens` in `testdouble_test.go` — read it; if it has none, add a two-line `lastRaw()` there rather than reaching into its fields from the test).

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/usecase/ -run TestTheWebFormCannotAcceptATelegramInvite -v
```

Expected: FAIL — `Preview` returns a real preview and `Accept` creates a user. That failure is the vulnerability; the next steps close it.

- [ ] **Step 3: Carry the channel into `InviteDetails`**

`api/internal/usecase/ports.go`:

```go
type InviteDetails struct {
	ID           string
	HouseholdID  string
	Email        string
	Name         string
	Role         domain.Role
	Capabilities domain.Capabilities
	// Channel is read by the public web routes, which serve only the email
	// channel: a Telegram invite is admitted by an owner in their own
	// browser, so the web form must answer its token as an unknown one
	// (spec decision 7).
	Channel      domain.InviteChannel
	FamilyName   string
	InviterName  string
	ExpiresAt    time.Time
	AcceptedAt   *time.Time
}
```

Add `i.channel` to both `GetInviteByTokenHash` and `GetLiveInviteForEmail` in `identity.sql`, regenerate sqlc, and map it through `domain.ParseInviteChannel` in `invite_repo.go`'s `ByTokenHash` and `LiveInviteForEmail` — the same fail-closed read the role and capabilities already get. Add the field to `inviteRow`/`inviteDouble` too.

- [ ] **Step 4: Refuse in the service, at the one shared checkpoint**

In `api/internal/usecase/invite.go`, extend `checkInviteLive`'s caller side. Put the check in **both** `Preview` and `Accept`, immediately after `ByTokenHash` and before `checkInviteLive`:

```go
	// A Telegram invite is not servable here at all, so it is answered as
	// an unknown token would be -- before the liveness check, so that even
	// the difference between "expired" and "unknown" cannot leak for a
	// token this route never serves (spec decision 7). This is one of the
	// milestone's named mutation checks: removing it must turn
	// TestTheWebFormCannotAcceptATelegramInvite red.
	if details.Channel != domain.ChannelEmail {
		return InvitePreview{}, domain.ErrNotFound
	}
```

(and the `SignInResult{}` form of the same three lines in `Accept`).

**Do not** put this inside `checkInviteLive`: that function's job is the token's lifecycle, its three outcomes map to three different HTTP statuses, and folding a fourth, differently-shaped rule into it is how the next person misses one of the two call sites.

- [ ] **Step 5: Run the test and watch it pass**

```bash
cd api && go test ./internal/usecase/ -run Invite -v && go test ./internal/adapter/http/ -run Invite -v
```

Expected: PASS.

- [ ] **Step 6: Add the HTTP-level proof**

The service test proves the rule; this proves the route reports it as a stranger's 404, not a 500 or a 410:

```go
func TestPublicInviteRoutesTreatATelegramTokenAsUnknown(t *testing.T) {
	env := newAPITestEnv(t)
	owner := env.signInSeededOwner(t)

	res := env.do(t, owner, http.MethodPost, "/api/v1/household/members/invite",
		`{"name":"Christine","role":"owner","capabilities":["money","calendar","chores","marriage"],"channel":"telegram"}`)
	var created struct{ Link string `json:"link"` }
	decodeBody(t, res, &created)
	rawToken := created.Link[strings.Index(created.Link, "start=inv_")+len("start=inv_"):]

	preview := env.doPublic(t, http.MethodGet, "/api/v1/invites/"+rawToken, "")
	if preview.Code != http.StatusNotFound {
		t.Fatalf("preview: got %d, want 404", preview.Code)
	}
	accept := env.doPublic(t, http.MethodPost, "/api/v1/invites/"+rawToken+"/accept",
		`{"password":"a-long-enough-password","displayName":"Christine"}`)
	if accept.Code != http.StatusNotFound {
		t.Fatalf("accept: got %d, want 404", accept.Code)
	}
}
```

- [ ] **Step 7: Mutation check 4 — let the web form through**

Delete the `details.Channel != domain.ChannelEmail` guard from `Accept` only. Run `go test ./internal/usecase/ -run TestTheWebFormCannotAcceptATelegramInvite`. It must fail. Restore it, then delete the one in `Preview` and confirm the same test fails again. Both call sites must be covered — that is why the check is asserted twice in one test.

- [ ] **Step 8: Commit**

```bash
git add api/internal/usecase/ports.go api/internal/usecase/invite.go api/internal/usecase/invite_test.go api/internal/usecase/testdouble_test.go api/internal/adapter/postgres/queries/identity.sql api/internal/adapter/postgres/sqlcgen api/internal/adapter/postgres/invite_repo.go api/internal/adapter/http/pending_invites_api_test.go
git commit -m "fix: the public invite form answers a Telegram token as unknown

Spec decision 7. Without this, whoever holds the raw token accepts in a
browser with a password of their own and skips the owner's Let in -- the
whole point of the lobby."
```

---

## Task 7: The knock

The partner taps the link. The bot records one knock, draws a four-digit code, and shows it. Everything else about `/start` behaves exactly as it did.

**Files:**
- Create: `api/internal/adapter/crypto/paircode.go`
- Modify: `api/internal/usecase/ports.go` (`InviteKnocker`, `PairingCodes`, `InviteRepository.RecordKnock`)
- Modify: `api/internal/usecase/invite.go` (`Knock`)
- Modify: `api/internal/usecase/telegram_auth.go` (the `inv_` branch)
- Modify: `api/internal/adapter/postgres/queries/identity.sql`, `invite_repo.go`
- Modify: `api/cmd/api/main.go`, `api/internal/adapter/http/api_test.go`
- Test: `api/internal/usecase/telegram_auth_test.go`, `api/internal/usecase/invite_test.go`, `api/internal/adapter/postgres/invite_repo_test.go`

**Interfaces:**
- Consumes: `telegramInvitePayloadPrefix`, `CreateTelegram` (Task 5); private-chat-only parsing (Task 1).
- Produces:
  - ```go
    // InviteKnocker is the one thing TelegramAuthService needs from the
    // invite side: turn a raw invite token and a chat into a code to show,
    // or refuse. InviteService implements it. The split keeps every word
    // the bot says inside TelegramAuthService and every invite rule inside
    // InviteService.
    type InviteKnocker interface {
        // Knock records the first tap on a Telegram invite link and
        // returns the four digits to show the tapper. It reports
        // domain.ErrNotFound for every refusal without exception --
        // unknown, expired, accepted, already knocked, email-channel, and
        // a chat that already belongs to a Hearth account -- because the
        // chat gets one bland reply for all of them and must not be able
        // to tell them apart by probing.
        Knock(ctx context.Context, rawToken string, chatID int64, username string) (code string, err error)
    }
    ```
  - ```go
    // PairingCodes draws the four digits an owner compares by eye. A port
    // so tests are deterministic; crypto.PairCodes is the implementation.
    type PairingCodes interface {
        // NewCode returns exactly four decimal digits, "0000" through
        // "9999", leading zeros kept.
        NewCode() (string, error)
    }
    ```
  - `InviteRepository.RecordKnock(ctx context.Context, tokenHash []byte, chatID int64, username, code string, now time.Time) error`
  - `InviteDeps` gains `Codes PairingCodes` and `Accounts TelegramAccountRepository`.
  - `TelegramAuthDeps` gains `Invites InviteKnocker`.

- [ ] **Step 1: Write the failing code-generator test**

`api/internal/adapter/crypto/paircode_test.go`:

```go
func TestNewCodeIsAlwaysFourDigits(t *testing.T) {
	codes := crypto.PairCodes{}
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		code, err := codes.NewCode()
		if err != nil {
			t.Fatalf("NewCode: %v", err)
		}
		if len(code) != 4 {
			t.Fatalf("code %q is %d characters, want 4 -- a leading zero must be kept", code, len(code))
		}
		for _, r := range code {
			if r < '0' || r > '9' {
				t.Fatalf("code %q is not four decimal digits", code)
			}
		}
		seen[code] = true
	}
	// Not a statistical test, just a smoke alarm: a constant or a badly
	// seeded source would show up here as a handful of values.
	if len(seen) < 100 {
		t.Fatalf("500 draws produced only %d distinct codes; the source is not random", len(seen))
	}
}
```

- [ ] **Step 2: Run it and watch it fail, then implement**

```bash
cd api && go test ./internal/adapter/crypto/ -run TestNewCodeIsAlwaysFourDigits -v
```

Expected: FAIL — `undefined: crypto.PairCodes`. Then create `api/internal/adapter/crypto/paircode.go`:

```go
package crypto

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// PairCodes draws the four digits an owner and their partner compare by eye
// before the owner admits them (ADR 11).
//
// crypto/rand rather than math/rand even though the code grants nothing and
// is never accepted by any endpoint: a predictable code would let someone
// watching the owner's screen over their shoulder -- or a future change
// that did start accepting it -- turn a display into a credential. The
// boring, obvious source costs nothing here.
type PairCodes struct{}

// NewCode returns exactly four decimal digits, leading zeros kept: "0007"
// is a valid code and must render as four characters, because the owner is
// matching it character by character against a phone.
func (PairCodes) NewCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "", fmt.Errorf("draw pairing code: %w", err)
	}
	return fmt.Sprintf("%04d", n.Int64()), nil
}
```

- [ ] **Step 3: Write the failing knock tests**

In `api/internal/usecase/telegram_auth_test.go`:

```go
// The bot's inv_ branch: a first tap records a knock and is answered with
// the code, and every other outcome gets the one bland dead-link reply
// (telegramDeadLinkMessage), so a chat holding a link it may have stolen
// learns nothing by probing.
func TestStartWithAnInviteTokenRecordsOneKnock(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	ctx := context.Background()
	rawToken := doubles.seedTelegramInvite(t, householdID, "Christine")

	if err := svc.HandleStart(ctx, 4242, "inv_"+rawToken, "jane_t"); err != nil {
		t.Fatalf("HandleStart: %v", err)
	}
	said := doubles.sender.lastTo(4242)
	code := doubles.invites.knockCode(rawToken)
	if !strings.Contains(said, code) {
		t.Fatalf("the chat was told %q, which does not contain its code %q", said, code)
	}

	// One knock per link (spec decision 2). A second tap -- by the same
	// chat or another -- gets the dead-link reply and changes nothing.
	if err := svc.HandleStart(ctx, 9999, "inv_"+rawToken, "someone_else"); err != nil {
		t.Fatalf("second HandleStart: %v", err)
	}
	if got := doubles.sender.lastTo(9999); got != "That sign-in link has expired. Start again from the app." {
		t.Fatalf("second tap was told %q, want the bland dead-link reply", got)
	}
	if doubles.invites.knockChatID(rawToken) != 4242 {
		t.Fatal("the second tap overwrote the first knock")
	}
}

// Every refusal is the same sentence. Listed together because the point is
// that they are indistinguishable, which a test per case would not show.
func TestEveryRefusedInviteStartGetsTheSameReply(t *testing.T) {
	const dead = "That sign-in link has expired. Start again from the app."
	for name, setup := range map[string]func(t *testing.T, d *telegramDoubles) (payload string, chatID int64){
		"unknown token":     func(t *testing.T, d *telegramDoubles) (string, int64) { return "inv_nosuchtoken", 4242 },
		"expired invite":    func(t *testing.T, d *telegramDoubles) (string, int64) { return "inv_" + d.seedExpiredTelegramInvite(t), 4242 },
		"email invite":      func(t *testing.T, d *telegramDoubles) (string, int64) { return "inv_" + d.seedEmailInvite(t), 4242 },
		"already accepted":  func(t *testing.T, d *telegramDoubles) (string, int64) { return "inv_" + d.seedAcceptedTelegramInvite(t), 4242 },
		"chat already ours": func(t *testing.T, d *telegramDoubles) (string, int64) { return "inv_" + d.seedTelegramInvite(t, householdID, "Christine"), d.seedBoundChat(t) },
	} {
		t.Run(name, func(t *testing.T) {
			svc, doubles := newTelegramAuthService(t)
			payload, chatID := setup(t, doubles)
			if err := svc.HandleStart(context.Background(), chatID, payload, "jane_t"); err != nil {
				t.Fatalf("HandleStart: %v", err)
			}
			if got := doubles.sender.lastTo(chatID); got != dead {
				t.Fatalf("got %q, want the one bland reply %q", got, dead)
			}
		})
	}
}

// Everything that is not an invite payload behaves exactly as it did
// before: the sign-in nonce, the sign-up path and the chat-link path are
// untouched.
func TestStartWithoutTheInvitePrefixIsUnchanged(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	nonce := doubles.seedSignInNonce(t)
	if err := svc.HandleStart(context.Background(), 4242, nonce, "jane_t"); err != nil {
		t.Fatalf("HandleStart: %v", err)
	}
	if !strings.Contains(doubles.sender.lastTo(4242), "/sign-up/") &&
		!strings.Contains(doubles.sender.lastTo(4242), "/sign-in/magic?token=") {
		t.Fatalf("an ordinary nonce no longer produces a link: %q", doubles.sender.lastTo(4242))
	}
}
```

The `seed*` helpers on `telegramDoubles` do not exist yet — add them to `testdouble_test.go` beside the fixture, each one a few lines over the existing doubles. The fixture must now also build an `inviteDouble` and hand it to `TelegramAuthDeps.Invites`.

- [ ] **Step 4: Run them and watch them fail**

```bash
cd api && go test ./internal/usecase/ -run 'InviteStart|OneKnock|WithoutTheInvitePrefix' -v
```

Expected: FAIL — `TelegramAuthDeps has no field Invites`.

- [ ] **Step 5: Add the repository method**

`identity.sql`:

```sql
-- name: RecordInviteKnock :one
-- One guarded UPDATE is the whole of "one knock per link" (spec decision
-- 2): knocked_at IS NULL is what makes the second tap -- and two taps at
-- the same instant -- lose. Every other condition is here for the same
-- reason it is in the SQL and not in Go: a caller cannot forget it.
UPDATE invites
SET knock_chat_id = $2, knock_chat_username = $3, knock_code = $4, knocked_at = $5
WHERE token_hash = $1
  AND channel = 'telegram'
  AND accepted_at IS NULL
  AND expires_at > $5
  AND knocked_at IS NULL
RETURNING id;
```

`invite_repo.go`:

```go
// RecordKnock reports domain.ErrNotFound for every case its guarded UPDATE
// does not match -- unknown token, email channel, accepted, expired, or
// already knocked. That is deliberately one answer: the caller turns it
// into the bot's one bland reply, and any difference between these cases
// would be something a chat could probe for.
func (r *InviteRepo) RecordKnock(ctx context.Context, tokenHash []byte, chatID int64,
	username, code string, now time.Time) error {
	_, err := r.q.RecordInviteKnock(ctx, sqlcgen.RecordInviteKnockParams{
		TokenHash:         tokenHash,
		KnockChatID:       nullableInt8(chatID),
		KnockChatUsername: nullableText(username),
		KnockCode:         nullableText(code),
		KnockedAt:         timestamptz(now),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return fmt.Errorf("record invite knock: %w", err)
	}
	return nil
}
```

Check `convert.go` for the nullable-int helper; if there is none, add `nullableInt8` beside `nullableText` with a comment saying which column needs it.

- [ ] **Step 6: Write the service method**

In `api/internal/usecase/invite.go`:

```go
// Knock records the first tap on a Telegram invite link and returns the
// four digits to show the tapper. It implements InviteKnocker, so
// TelegramAuthService can route an inv_ payload here without knowing any
// invite rule.
//
// Every refusal about the *link* is domain.ErrNotFound, with no exception:
// unknown, expired, accepted, already knocked and email-channel all collapse
// into one answer, because a caller that could tell them apart would hand a
// chat holding a stolen link a way to probe for somebody else's invite.
//
// The one exception is domain.ErrChatAlreadyBound, and it is safe precisely
// because it is not about the link: it tells the tapper only that their own
// chat already belongs to an account, which they can discover by sending
// /start with no payload at all. Answering it plainly saves them tapping a
// link that will never work (spec decision 15).
//
// That check is first, before the guarded UPDATE, so a chat that already
// belongs to an account cannot consume somebody else's invite link on its
// way to being refused. It runs again inside Admit's transaction, because
// the chat may sign up somewhere else between the knock and the click.
func (s *InviteService) Knock(ctx context.Context, rawToken string, chatID int64, username string) (string, error) {
	if _, err := s.d.Accounts.ByChatID(ctx, chatID); err == nil {
		return "", domain.ErrChatAlreadyBound
	} else if !errors.Is(err, domain.ErrNotFound) {
		return "", err
	}

	code, err := s.d.Codes.NewCode()
	if err != nil {
		return "", err
	}
	if err := s.d.Invites.RecordKnock(ctx, s.d.Tokens.HashToken(rawToken), chatID, username, code,
		s.d.Clock.Now()); err != nil {
		return "", err
	}
	return code, nil
}
```

`InviteDeps` gains `Codes PairingCodes` and `Accounts TelegramAccountRepository`.

- [ ] **Step 7: Route the payload in `HandleStart`**

In `api/internal/usecase/telegram_auth.go`, as the **first** statement of `HandleStart`, before `Links.Consume`:

```go
	// An invite payload is routed here before the nonce table is touched at
	// all: migration 00018's comment reserved the inv_ prefix so an invite
	// never has to occupy a telegram_link_requests row, which lives ten
	// minutes while an invite lives a day. Consuming first would spend a
	// nonce that was never minted and answer a real invite as a dead link.
	if rawToken, ok := strings.CutPrefix(payload, telegramInvitePayloadPrefix); ok {
		return s.handleInviteStart(ctx, chatID, rawToken, username)
	}
```

and the handler, beside the others:

```go
// handleInviteStart answers a tap on a household invite link. It writes no
// membership: the owner's signed-in browser admits, and that is the whole
// of the protection against a leaked link (ADR 11, following ADR 10).
//
// Every refusal is telegramDeadLinkMessage, the same sentence an unknown
// sign-in nonce gets, for the reason that constant's own comment gives.
// A chat that already belongs to a Hearth account is refused with a
// different, self-describing line: it tells the tapper only about their own
// chat, which they already know, and saves them tapping a dead link
// forever (spec decision 15).
func (s *TelegramAuthService) handleInviteStart(ctx context.Context, chatID int64, rawToken, username string) error {
	code, err := s.d.Invites.Knock(ctx, rawToken, chatID, username)
	switch {
	case err == nil:
		return s.say(ctx, chatID, fmt.Sprintf(
			"Your code is %s.\n\nShow it to whoever invited you. They'll let you in, and then I'll send you a sign-in link.",
			code))
	case errors.Is(err, domain.ErrChatAlreadyBound):
		return s.say(ctx, chatID, "This Telegram account already belongs to a Hearth household.")
	case errors.Is(err, domain.ErrNotFound):
		return s.say(ctx, chatID, telegramDeadLinkMessage)
	default:
		return fmt.Errorf("record invite knock: %w", err)
	}
}
```

Add the sentinel to `domain/errors.go`:

```go
	ErrChatAlreadyBound = errors.New("this telegram chat already belongs to an account")
```

`telegramInvitePayloadPrefix` is the constant Task 5 added in `invite.go`; both files are in package `usecase`, so it is referenced, never redeclared. One definition of that prefix exists, and migration `00018`'s comment is the other half of the contract.

- [ ] **Step 8: Wire `Codes`, `Accounts` and `Invites` in both places**

Pattern 23 again, and this task adds three at once:

- `api/cmd/api/main.go`: `InviteDeps{… Codes: crypto.PairCodes{}, Accounts: telegramAccounts …}` and `TelegramAuthDeps{… Invites: inviteSvc …}`. **`inviteSvc` is constructed before `telegramAuthSvc` already — check the order at `main.go:151` and `:211`, and move the construction if it is not.**
- `api/internal/adapter/http/api_test.go`: the same three fields in its own literals.

- [ ] **Step 9: Run the tests and watch them pass**

```bash
cd api && go build ./... && go test ./internal/usecase/ ./internal/adapter/crypto/ -v 2>&1 | tail -30
```

Expected: PASS.

- [ ] **Step 10: Write the Postgres race test**

In `api/internal/adapter/postgres/invite_repo_test.go` — `docs/LEARNING.md` pattern 19: force the overlap with a barrier, never hope for it.

```go
// Two chats tap the same link at the same instant. Exactly one knock is
// recorded, and the other gets the same ErrNotFound a dead link gets. The
// overlap is forced with a barrier rather than hoped for: two goroutines
// started back to back usually do not overlap, so a test without one would
// pass while the guard was missing.
func TestTwoSimultaneousKnocksProduceExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	db, h := newInviteTestHousehold(t)
	invites := postgres.NewInviteRepo(db)

	tokenHash := []byte("a-token-hash-32-bytes-long-------")
	if _, err := invites.CreateTelegram(ctx, h.ID, "Christine", domain.RoleOwner,
		domain.AllCapabilities(), tokenHash, h.OwnerUserID, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, chatID := range []int64{4242, 9999} {
		go func(chatID int64) {
			<-start // the barrier: both goroutines are parked here
			results <- invites.RecordKnock(ctx, tokenHash, chatID, "someone", "4812", time.Now())
		}(chatID)
	}
	close(start)

	var wins, refusals int
	for i := 0; i < 2; i++ {
		switch err := <-results; {
		case err == nil:
			wins++
		case errors.Is(err, domain.ErrNotFound):
			refusals++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 || refusals != 1 {
		t.Fatalf("got %d wins and %d refusals, want exactly 1 and 1", wins, refusals)
	}
}
```

- [ ] **Step 11: Mutation check 2 — the one-knock guard**

Delete `AND knocked_at IS NULL` from `RecordInviteKnock`, regenerate sqlc, and run both `TestTwoSimultaneousKnocksProduceExactlyOneWinner` and `TestStartWithAnInviteTokenRecordsOneKnock`. Both must fail. Put it back and regenerate.

- [ ] **Step 12: Commit**

```bash
git add api/internal/adapter/crypto/paircode.go api/internal/adapter/crypto/paircode_test.go api/internal/usecase/ports.go api/internal/usecase/invite.go api/internal/usecase/telegram_auth.go api/internal/usecase/telegram_auth_test.go api/internal/usecase/invite_test.go api/internal/usecase/testdouble_test.go api/internal/domain/errors.go api/internal/adapter/postgres/queries/identity.sql api/internal/adapter/postgres/sqlcgen api/internal/adapter/postgres/invite_repo.go api/internal/adapter/postgres/invite_repo_test.go api/cmd/api/main.go api/internal/adapter/http/api_test.go
git commit -m "feat: tapping an invite link records one knock and shows a code

One guarded UPDATE is the whole of one-knock-per-link. Every refusal --
unknown, expired, accepted, already knocked, email-channel -- gets the one
bland dead-link reply, so a chat holding a stolen link learns nothing."
```

---

## Task 8: A new link, which is also "Not them"

One route does both. It clears any knock, replaces the token, tells the chat that knocked that its link is dead, and returns the new link. Separate routes would give the waiting card a third state ("cancelled, no link yet") for no benefit. An owner who suspects a leak and wants *no* new link withdraws the invite instead — that route already exists.

**Files:**
- Create: `api/internal/adapter/http/invite_lobby_handlers.go`
- Modify: `api/internal/usecase/ports.go` (`InviteRepository.ReplaceToken`, `InviteChats`)
- Modify: `api/internal/usecase/invite.go` (`NewLink`)
- Modify: `api/internal/usecase/telegram_auth.go` (`SendLinkCancelled`, `SendSignIn` — the `InviteChats` implementation)
- Modify: `api/internal/adapter/http/router.go`, `api/cmd/hearthctl/routes.go`
- Modify: `api/internal/adapter/postgres/queries/identity.sql`, `invite_repo.go`
- Modify: `api/cmd/api/main.go`, `api/internal/adapter/http/api_test.go`

**Interfaces:**
- Consumes: `TelegramInviteLink`, `telegramInvitePayloadPrefix` (Task 5); the knock columns (Task 7).
- Produces:
  - ```go
    // InviteChats is what InviteService needs from the Telegram side:
    // the two messages an invite causes. TelegramAuthService implements it,
    // reusing its own sendSignIn, so no second magic-link path exists.
    type InviteChats interface {
        // SendSignIn delivers an ordinary magic link to a chat that has
        // just been admitted. It is called after the commit, never inside
        // it (spec decision 6).
        SendSignIn(ctx context.Context, chatID int64, userID string) error
        // SendLinkCancelled tells a chat that knocked that its link is no
        // longer valid, because the owner asked for a new one.
        SendLinkCancelled(ctx context.Context, chatID int64) error
    }
    ```
  - `InviteRepository.ReplaceToken(ctx context.Context, householdID, inviteID string, tokenHash []byte, expiresAt time.Time) (knockedChatID int64, err error)` — `0` when nobody had knocked.
  - `func (s *InviteService) NewLink(ctx context.Context, householdID, inviteID string) (TelegramInviteLink, error)`
  - `POST /household/invites/{id}/link` → `200 {"link":"…","expiresAt":"…"}`
  - `domain.ErrInviteNotTelegram` → `409 INVITE_NOT_TELEGRAM` "Only Telegram invites have a link."

- [ ] **Step 1: Write the failing test**

In `api/internal/usecase/invite_test.go`:

```go
// One route does "get a new link" and "Not them". The old link stops
// working the moment the new one exists -- that is what makes a leaked link
// cost one new link rather than a takeover (spec decision 2).
func TestANewLinkKillsTheOldOneAndClearsTheKnock(t *testing.T) {
	svc, doubles := newInviteService(t)
	ctx := context.Background()

	first, err := svc.CreateTelegram(ctx, householdID, ownerID, "Christine", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	firstToken := doubles.tokens.lastRaw()
	if _, err := svc.Knock(ctx, firstToken, 4242, "jane_t"); err != nil {
		t.Fatalf("Knock: %v", err)
	}

	second, err := svc.NewLink(ctx, householdID, first.ID)
	if err != nil {
		t.Fatalf("NewLink: %v", err)
	}
	if second.URL == first.URL {
		t.Fatal("the new link is the old link")
	}
	// The knocked chat is told, because from their side the link simply
	// stopped working and nobody would otherwise say why.
	if got := doubles.chats.lastCancelledChat(); got != 4242 {
		t.Fatalf("cancelled chat %d, want 4242", got)
	}
	// The old token knocks no more, and the row is back to waiting.
	if _, err := svc.Knock(ctx, firstToken, 5555, "someone"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the old token still knocks: %v", err)
	}
	if row := doubles.invites.byID(first.ID); row.KnockedAt != nil {
		t.Fatal("the knock was not cleared")
	}
	// And the fresh token does.
	if _, err := svc.Knock(ctx, doubles.tokens.lastRaw(), 5555, "someone"); err != nil {
		t.Fatalf("the new token does not knock: %v", err)
	}
}

// An email invite has no link to replace. Refused with its own message
// rather than silently converted: the channel is fixed when the invite is
// created (spec decision 14's sibling rule, decision 9).
func TestANewLinkIsRefusedForAnEmailInvite(t *testing.T) {
	svc, doubles := newInviteService(t)
	ctx := context.Background()
	doubles.flags.set(domain.FlagEmailInvites, true)
	if err := svc.Create(ctx, householdID, ownerID, "Jane", "jane@example.com",
		domain.RoleOwner, domain.AllCapabilities()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	pending, _ := svc.ListPending(ctx, householdID)

	if _, err := svc.NewLink(ctx, householdID, pending[0].ID); !errors.Is(err, domain.ErrInviteNotTelegram) {
		t.Fatalf("got %v, want domain.ErrInviteNotTelegram", err)
	}
}

// An id from another household is a 404, never a 403: the answer must not
// confirm that another household's invite exists (docs/LEARNING.md pattern
// 24).
func TestANewLinkForAnotherHouseholdsInviteIsNotFound(t *testing.T) {
	svc, _ := newInviteService(t)
	ctx := context.Background()
	mine, err := svc.CreateTelegram(ctx, householdID, ownerID, "Christine", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	if _, err := svc.NewLink(ctx, "some-other-household", mine.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want domain.ErrNotFound", err)
	}
}
```

`doubles.chats` is a new `inviteChatsDouble` (two recorded calls); `doubles.flags` is whatever the fixture already uses for flags — check before adding one.

- [ ] **Step 2: Run them and watch them fail**

```bash
cd api && go test ./internal/usecase/ -run NewLink -v
```

Expected: FAIL — `svc.NewLink undefined`.

- [ ] **Step 3: Add the query and the repository method**

```sql
-- name: ReplaceInviteToken :one
-- One statement replaces the token and clears the knock together, so there
-- is never an instant where a fresh link carries a stale knock. It returns
-- the chat that had knocked, if any, so the caller can tell them their link
-- is dead -- from their side it simply stopped working.
UPDATE invites
SET token_hash = $3, expires_at = $4,
    knock_chat_id = NULL, knock_chat_username = NULL, knock_code = NULL, knocked_at = NULL
WHERE id = $1 AND household_id = $2
  AND channel = 'telegram'
  AND accepted_at IS NULL
RETURNING (SELECT knock_chat_id FROM invites WHERE id = $1)::bigint AS previous_knock_chat_id;
```

The subselect reads the pre-update value: in Postgres a statement sees the row as it was when the statement began, so this returns the chat that had knocked rather than the `NULL` just written. **Verify that against the real database in Step 5's test before trusting it** — if it does not behave as described, read the row with a `SELECT … FOR UPDATE` inside a short transaction in `invite_repo.go` instead, and say in a comment why the single statement was not enough.

```go
// ReplaceToken clears the knock in the same statement that replaces the
// token, and returns the chat that had knocked (0 when nobody had) so the
// caller can tell them. Household-scoped in the SQL, so an id from another
// household matches nothing and reports domain.ErrNotFound -- the same
// answer an id that never existed gets.
//
// An email invite matches nothing either, which the caller cannot tell
// apart from "no such invite" -- so it reads the row first to report
// domain.ErrInviteNotTelegram for the case an owner can actually act on.
func (r *InviteRepo) ReplaceToken(ctx context.Context, householdID, inviteID string,
	tokenHash []byte, expiresAt time.Time) (int64, error)
```

- [ ] **Step 4: Write the service method**

```go
// NewLink replaces a Telegram invite's link, which is also what "Not them"
// does: the knock is cleared, the old token stops working, and whoever
// knocked is told. One method for both because the owner's two intentions
// -- "that wasn't them" and "I lost the link" -- need exactly the same
// four effects, and a second route would give the waiting card a third
// state for no benefit.
//
// An owner who suspects a leak and wants no new link withdraws the invite
// instead (Withdraw, which deletes the row).
func (s *InviteService) NewLink(ctx context.Context, householdID, inviteID string) (TelegramInviteLink, error) {
	if s.d.BotUsername == "" {
		return TelegramInviteLink{}, domain.ErrTelegramInvitesUnavailable
	}
	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return TelegramInviteLink{}, fmt.Errorf("generate telegram invite token: %w", err)
	}
	expiresAt := s.d.Clock.Now().Add(s.d.TelegramInviteTTL)
	knockedChatID, err := s.d.Invites.ReplaceToken(ctx, householdID, inviteID, hash, expiresAt)
	if err != nil {
		return TelegramInviteLink{}, err
	}
	// After the write, never before: a failure here must not leave the
	// owner without the new link they asked for. The chat is told as a
	// courtesy -- their link already stopped working the moment the row
	// changed -- so the error is logged, not returned
	// (docs/LEARNING.md pattern 5 is about naming a failure, and this one
	// is named in the log rather than in a response field nobody could act
	// on).
	if knockedChatID != 0 {
		if err := s.d.Chats.SendLinkCancelled(ctx, knockedChatID); err != nil {
			slog.Error("could not tell a knocked chat its invite link was replaced", "error", err)
		}
	}
	return TelegramInviteLink{ID: inviteID, URL: s.telegramInviteURL(raw), ExpiresAt: expiresAt}, nil
}
```

- [ ] **Step 5: Implement `InviteChats` on `TelegramAuthService`**

In `api/internal/usecase/telegram_auth.go`, at the bottom:

```go
// SendSignIn and SendLinkCancelled implement InviteChats. They live here
// rather than in InviteService because TelegramAuthService owns every word
// the bot says and owns the one path that mints a magic link -- a second
// one would mean two expiry rules and two rate limits drifting apart, which
// is the same reasoning this type's own doc comment gives.
func (s *TelegramAuthService) SendSignIn(ctx context.Context, chatID int64, userID string) error {
	return s.sendSignIn(ctx, chatID, userID)
}

func (s *TelegramAuthService) SendLinkCancelled(ctx context.Context, chatID int64) error {
	return s.say(ctx, chatID, "That link is no longer valid. Ask whoever invited you for a new one.")
}

var _ InviteChats = (*TelegramAuthService)(nil)
```

**Wiring order:** `InviteService` needs `Chats` (a `*TelegramAuthService`) and `TelegramAuthService` needs `Invites` (the `*InviteService` from Task 7). That is a cycle in the constructors, not in the types. Break it in `main.go` the way the file already breaks such knots — construct `inviteSvc` first with `Chats` unset, construct `telegramAuthSvc` with `Invites: inviteSvc`, then assign. If `InviteDeps` is copied by value into the service (it is), add a small setter with a comment naming the cycle:

```go
// SetChats completes the two-way wiring between this service and
// TelegramAuthService: the invite side needs to send two messages, and the
// Telegram side needs to record a knock. Neither can be constructed with
// the other already built, so main.go builds both and closes the loop here.
// Called exactly once, at startup, before any request is served.
func (s *InviteService) SetChats(chats InviteChats) { s.d.Chats = chats }
```

- [ ] **Step 6: Add the route and the handler**

Create `api/internal/adapter/http/invite_lobby_handlers.go` — the knock half of the invite routes, kept out of `pending_invite_handlers.go` so each file keeps one job:

```go
// The owner's side of the invite lobby: getting a fresh link (which is also
// "Not them") and letting the knocker in. The list and withdraw routes live
// in pending_invite_handlers.go; the public, pre-sign-in routes live in
// invite_handlers.go.

type inviteLinkDTO struct {
	Link      string    `json:"link"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// handleNewInviteLink serves both "Get a new link" and "Not them": the
// effects are identical (spec, API section), so there is one route.
func handleNewInviteLink(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		link, err := deps.Invites.NewLink(r.Context(), scope.HouseholdID, chi.URLParam(r, "id"))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, inviteLinkDTO{Link: link.URL, ExpiresAt: link.ExpiresAt})
	}
}
```

In `router.go`, inside the same `requireCookieSession` group Task 2 built:

```go
						c.Post("/household/invites/{id}/link", handleNewInviteLink(deps))
```

In `api/cmd/hearthctl/routes.go`:

```go
	{"POST", "/household/invites/{id}/link", "owner+browser session+csrf", "-> {link,expiresAt}; telegram invites only"},
```

Add `domain.ErrInviteNotTelegram` and its 409 row (`INVITE_NOT_TELEGRAM`, "Only Telegram invites have a link.").

- [ ] **Step 7: Run everything and watch it pass**

```bash
cd api && go build ./... && go test ./... 2>&1 | tail -20 && go test ./cmd/hearthctl/ -v
```

Expected: PASS, `routes_test.go` included.

- [ ] **Step 8: Commit**

```bash
git add api/internal/usecase/ports.go api/internal/usecase/invite.go api/internal/usecase/invite_test.go api/internal/usecase/telegram_auth.go api/internal/usecase/testdouble_test.go api/internal/domain/errors.go api/internal/adapter/http/errors.go api/internal/adapter/http/invite_lobby_handlers.go api/internal/adapter/http/router.go api/internal/adapter/postgres/queries/identity.sql api/internal/adapter/postgres/sqlcgen api/internal/adapter/postgres/invite_repo.go api/cmd/hearthctl/routes.go api/cmd/api/main.go api/internal/adapter/http/api_test.go
git commit -m "feat: one route issues a new invite link, which is also Not them

Clearing the knock and replacing the token happen in one statement, so a
fresh link never carries a stale knock. The chat that knocked is told,
because from their side the link simply stopped working."
```

---

## Task 9: Let in

The transaction the whole milestone exists for: one repository method creates the user, the membership and the `telegram_accounts` row and stamps the invite accepted. Either all four happen or none do. The sign-in link is sent **after** the commit, and a failed send is named, not hidden.

**Files:**
- Modify: `api/internal/usecase/ports.go` (`InviteRepository.Admit`, `AdmittedMember`)
- Modify: `api/internal/usecase/invite.go` (`Admit`)
- Modify: `api/internal/adapter/postgres/invite_repo.go` (the transaction)
- Modify: `api/internal/adapter/postgres/queries/identity.sql`
- Modify: `api/internal/adapter/http/invite_lobby_handlers.go`, `router.go`, `errors.go`
- Modify: `api/cmd/hearthctl/routes.go`
- Test: `api/internal/usecase/invite_test.go`, `api/internal/adapter/postgres/invite_repo_test.go`, `api/internal/adapter/http/`

**Interfaces:**
- Consumes: everything from Tasks 3, 5, 7, 8.
- Produces:
  - ```go
    // AdmittedMember is what Let in produced. SignInSent is false when the
    // member exists but the bot could not reach their chat -- named rather
    // than hidden (spec decision 6), because the owner is the only person
    // who can tell them to send /start.
    type AdmittedMember struct {
        MembershipID string
        UserID       string
        Name         string
        Role         domain.Role
        Capabilities domain.Capabilities
        SignInSent   bool
    }
    ```
  - `InviteRepository.Admit(ctx context.Context, householdID, inviteID string, now time.Time) (AdmittedInvite, error)` where `AdmittedInvite{UserID, MembershipID, Name string; Role domain.Role; Capabilities domain.Capabilities; ChatID int64}`.
  - `func (s *InviteService) Admit(ctx context.Context, householdID, inviteID string) (AdmittedMember, error)`
  - `POST /household/invites/{id}/admit` → `200 {"member":{…},"signInSent":true}`. **The request body is empty** — no code field, ever (spec decision 3).
  - `domain.ErrInviteNotKnocked` → `409 INVITE_NOT_KNOCKED` "No one is waiting on this link."
  - `domain.ErrChatAlreadyBound` (Task 7) → `409 CHAT_ALREADY_BOUND` "That Telegram account joined another household. Get a new link."

- [ ] **Step 1: Write the failing usecase tests**

```go
// Let in, the whole point of the milestone: one click turns a knock into a
// member, and the bot sends them a sign-in link in their own chat.
func TestAdmitCreatesTheMemberAndSendsTheirSignInLink(t *testing.T) {
	svc, doubles := newInviteService(t)
	ctx := context.Background()
	invite, err := svc.CreateTelegram(ctx, householdID, ownerID, "Christine", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	if _, err := svc.Knock(ctx, doubles.tokens.lastRaw(), 4242, "christine_t"); err != nil {
		t.Fatalf("Knock: %v", err)
	}

	member, err := svc.Admit(ctx, householdID, invite.ID)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if !member.SignInSent {
		t.Fatal("signInSent is false although the send succeeded")
	}
	if member.Role != domain.RoleOwner {
		t.Fatalf("role is %q; Let in grants exactly what the invite said (spec decision 14)", member.Role)
	}
	if got := doubles.chats.lastSignInChat(); got != 4242 {
		t.Fatalf("the sign-in link went to chat %d, want the chat that knocked (4242)", got)
	}
	if bound := doubles.accounts.userForChat(4242); bound != member.UserID {
		t.Fatalf("chat 4242 is bound to %q, want the new member %q", bound, member.UserID)
	}
	// The invite is spent: a second Let in, and a second knock, both refuse.
	if _, err := svc.Admit(ctx, householdID, invite.ID); !errors.Is(err, domain.ErrInviteAlreadyAccepted) {
		t.Fatalf("second Admit: got %v, want domain.ErrInviteAlreadyAccepted", err)
	}
}

// Nobody has knocked yet, or a new link was issued since. Either way there
// is no one to let in, and nothing is written.
func TestAdmitRefusesWhenNobodyIsWaiting(t *testing.T) {
	svc, doubles := newInviteService(t)
	ctx := context.Background()
	invite, err := svc.CreateTelegram(ctx, householdID, ownerID, "Christine", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}

	if _, err := svc.Admit(ctx, householdID, invite.ID); !errors.Is(err, domain.ErrInviteNotKnocked) {
		t.Fatalf("got %v, want domain.ErrInviteNotKnocked", err)
	}
	if doubles.users.count() != 1 {
		t.Fatal("a user was created for an invite nobody knocked on")
	}
}

// The member exists; only the message failed. Saying so is the whole of the
// recovery path -- the chat is bound now, so any /start already sends them
// a fresh sign-in link (spec decision 6).
func TestAdmitReportsAFailedSendWithoutLosingTheMember(t *testing.T) {
	svc, doubles := newInviteService(t)
	ctx := context.Background()
	invite, err := svc.CreateTelegram(ctx, householdID, ownerID, "Christine", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	if _, err := svc.Knock(ctx, doubles.tokens.lastRaw(), 4242, "christine_t"); err != nil {
		t.Fatalf("Knock: %v", err)
	}
	doubles.chats.failNextSignIn()

	member, err := svc.Admit(ctx, householdID, invite.ID)
	if err != nil {
		t.Fatalf("Admit must succeed when only the send failed: %v", err)
	}
	if member.SignInSent {
		t.Fatal("signInSent is true although the send failed")
	}
	if member.MembershipID == "" {
		t.Fatal("the member was lost because a message could not be sent")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

```bash
cd api && go test ./internal/usecase/ -run Admit -v
```

Expected: FAIL — `svc.Admit undefined`.

- [ ] **Step 3: Write the transaction**

`identity.sql` — three statements the repository runs inside one transaction. The first is the guard:

```sql
-- name: ClaimKnockedInvite :one
-- The guard and the read in one statement: it stamps the invite accepted
-- only if it is a telegram invite, unaccepted, unexpired, and somebody has
-- knocked -- and returns everything the rest of the transaction needs, so
-- no separate read can see a different row. Zero rows means one of those
-- five conditions failed; the caller tells them apart with one more read,
-- as Delete already does.
UPDATE invites
SET accepted_at = $3
WHERE id = $1 AND household_id = $2
  AND channel = 'telegram'
  AND accepted_at IS NULL
  AND expires_at > $3
  AND knocked_at IS NOT NULL
RETURNING name, role, capabilities, knock_chat_id, knock_chat_username;
```

`invite_repo.go`:

```go
// Admit is spec decision 5: the user, the membership, the telegram_accounts
// row and the acceptance stamp, in one transaction. Either all four happen
// or none do.
//
// Do not compose this from separate calls. A failure between them would
// leave a user with no membership and no email -- no unique constraint to
// make a retry fail loudly, so each retry would silently orphan another
// one. This is the same rule Accept and Provision state in ports.go.
//
// The stamp goes first, for the reason Accept's does: it is what makes a
// second, concurrent Let in fail cheaply, before any row is written, rather
// than failing on the telegram_accounts unique index with an error nobody
// can map.
func (r *InviteRepo) Admit(ctx context.Context, householdID, inviteID string, now time.Time) (usecase.AdmittedInvite, error)
```

Write it against `Accept`'s shape (same file, line ~123): `Begin`, `defer Rollback`, `q := r.q.WithTx(tx)`, then `ClaimKnockedInvite` → `CreateUser` (email `nil`, password hash `nil`, display name from the invite) → `CreateMembership` → `CreateTelegramAccount` → `Commit`.

- `pgx.ErrNoRows` from `ClaimKnockedInvite` → read the row once more (household-scoped) to answer `domain.ErrInviteAlreadyAccepted` if it is accepted, `domain.ErrInviteNotKnocked` if it is not, `domain.ErrNotFound` if there is none.
- A unique-violation from `CreateTelegramAccount` → `domain.ErrChatAlreadyBound`. **This is the re-check spec decision 15 asks for**: the chat may have signed up somewhere else between the knock and the click, and the constraint is the only place that can see it atomically. Check how `translate` maps a unique violation before writing this branch, and map it here explicitly if `translate` would flatten it to `domain.ErrAlreadyExists`.

- [ ] **Step 4: Write the service method**

```go
// Admit turns a knock into a member. The transaction is the repository's;
// this method's own job is the order of the two things that cannot be in
// one: the write, then the message.
//
// The sign-in link is sent after the commit and a failure is reported as
// SignInSent: false, never swallowed (spec decision 6, docs/LEARNING.md
// pattern 5). Sending inside the transaction would mean a message
// promising an account that a later rollback took away.
func (s *InviteService) Admit(ctx context.Context, householdID, inviteID string) (AdmittedMember, error) {
	admitted, err := s.d.Invites.Admit(ctx, householdID, inviteID, s.d.Clock.Now())
	if err != nil {
		return AdmittedMember{}, err
	}
	member := AdmittedMember{
		MembershipID: admitted.MembershipID,
		UserID:       admitted.UserID,
		Name:         admitted.Name,
		Role:         admitted.Role,
		Capabilities: admitted.Capabilities,
		SignInSent:   true,
	}
	if err := s.d.Chats.SendSignIn(ctx, admitted.ChatID, admitted.UserID); err != nil {
		// Named, not hidden. The chat is bound now, so the recovery path
		// already exists and needs no new code: any /start sends them a
		// fresh sign-in link.
		slog.Error("admitted a member but could not send their sign-in link", "error", err)
		member.SignInSent = false
	}
	return member, nil
}
```

- [ ] **Step 5: Add the route**

In `invite_lobby_handlers.go`:

```go
// admitResultDTO carries the new member and whether their sign-in link
// actually went out. There is no code field anywhere in this request: the
// four digits are compared by eye and accepted by no endpoint (spec
// decision 3). A test pins that.
type admitResultDTO struct {
	Member     admittedMemberDTO `json:"member"`
	SignInSent bool              `json:"signInSent"`
}

type admittedMemberDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

// handleAdmitInvite reads no body at all. Its guards are on the route:
// owner, CSRF and a browser session -- the deciding click must sit where a
// stolen link cannot reach (ADR 10, spec decision 4).
func handleAdmitInvite(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		member, err := deps.Invites.Admit(r.Context(), scope.HouseholdID, chi.URLParam(r, "id"))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, admitResultDTO{
			Member: admittedMemberDTO{
				ID:           member.MembershipID,
				Name:         member.Name,
				Role:         string(member.Role),
				Capabilities: member.Capabilities.Strings(),
			},
			SignInSent: member.SignInSent,
		})
	}
}
```

`router.go`, in the same cookie-session group:

```go
						c.Post("/household/invites/{id}/admit", handleAdmitInvite(deps))
```

`api/cmd/hearthctl/routes.go`:

```go
	{"POST", "/household/invites/{id}/admit", "owner+browser session+csrf", "-> {member,signInSent}; no request body"},
```

- [ ] **Step 6: Write the HTTP tests, including decision 3's pin**

```go
// The matching code is compared by eye and accepted by nothing. If this
// ever fails, someone has turned a display into a credential, and ADR 4's
// rejection of guessable one-time codes applies (spec decision 3).
func TestTheAdmitRequestHasNoCodeField(t *testing.T) {
	body, err := os.ReadFile("invite_lobby_handlers.go")
	if err != nil {
		t.Fatalf("read handler source: %v", err)
	}
	for _, forbidden := range []string{"Code string", `json:"code"`} {
		if bytes.Contains(body, []byte(forbidden)) {
			t.Fatalf("the admit handler declares %q; the matching code must never be accepted by an endpoint", forbidden)
		}
	}
}

func TestTheLobbyRoutesNeedAnOwnerAndABrowserSession(t *testing.T) {
	env := newAPITestEnv(t)
	owner := env.signInSeededOwner(t)
	limited := env.signInSeededLimitedMember(t)
	token := env.mintAPIToken(t, owner)

	res := env.do(t, owner, http.MethodPost, "/api/v1/household/members/invite",
		`{"name":"Christine","role":"owner","capabilities":["money","calendar","chores","marriage"],"channel":"telegram"}`)
	var created struct{ ID string `json:"id"` }
	decodeBody(t, res, &created)

	for _, path := range []string{
		"/api/v1/household/invites/" + created.ID + "/link",
		"/api/v1/household/invites/" + created.ID + "/admit",
	} {
		t.Run(path, func(t *testing.T) {
			// A limited member is not an owner.
			if got := env.do(t, limited, http.MethodPost, path, "").Code; got != http.StatusForbidden {
				t.Errorf("limited member: got %d, want 403", got)
			}
			// A personal API token must not be able to manage who gets in,
			// even holding an owner's authority (spec decision 12).
			if got := env.doWithToken(t, token, http.MethodPost, path, "").Code; got != http.StatusUnauthorized {
				t.Errorf("api token: got %d, want 401", got)
			}
			// No CSRF token: refused before the handler runs.
			if got := env.doWithoutCSRF(t, owner, http.MethodPost, path, "").Code; got != http.StatusForbidden {
				t.Errorf("missing CSRF: got %d, want 403", got)
			}
		})
	}
}
```

If the package already has a route-guard matrix test covering the milestone-1 routes, **add these two rows to it** instead of keeping this as a separate function — one matrix is easier to keep complete than two. Likewise, if there is already a convention for pinning an absent request field, prefer it over the source-reading test above and say so in a comment; the source read is the crude fallback, not the goal.

- [ ] **Step 7: Write the Postgres all-or-nothing test**

```go
// Admit is all or nothing. Forcing the last insert to fail -- by binding
// the chat to somebody else first -- must leave no user and no membership
// behind, because a half-admitted member is a row nobody can clean up from
// the product.
func TestAdmitLeavesNothingBehindWhenTheChatIsAlreadyBound(t *testing.T) {
	ctx := context.Background()
	db, h := newInviteTestHousehold(t)
	invites := postgres.NewInviteRepo(db)
	accounts := postgres.NewTelegramAccountRepo(db)

	tokenHash := []byte("another-token-hash-32-bytes-----")
	inviteID, err := invites.CreateTelegram(ctx, h.ID, "Christine", domain.RoleOwner,
		domain.AllCapabilities(), tokenHash, h.OwnerUserID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	if err := invites.RecordKnock(ctx, tokenHash, 4242, "christine_t", "4812", time.Now()); err != nil {
		t.Fatalf("RecordKnock: %v", err)
	}
	// The chat signs up somewhere else between the knock and the click --
	// spec decision 15's race, forced rather than waited for.
	if err := accounts.Link(ctx, h.OwnerUserID, 4242, "christine_t"); err != nil {
		t.Fatalf("bind the chat elsewhere: %v", err)
	}

	usersBefore := countRows(t, db, "users")
	membershipsBefore := countRows(t, db, "memberships")

	if _, err := invites.Admit(ctx, h.ID, inviteID, time.Now()); !errors.Is(err, domain.ErrChatAlreadyBound) {
		t.Fatalf("Admit: got %v, want domain.ErrChatAlreadyBound", err)
	}
	if got := countRows(t, db, "users"); got != usersBefore {
		t.Errorf("users: %d rows after a refused Admit, want %d -- the transaction leaked a user", got, usersBefore)
	}
	if got := countRows(t, db, "memberships"); got != membershipsBefore {
		t.Errorf("memberships: %d rows after a refused Admit, want %d", got, membershipsBefore)
	}
	var acceptedAt *time.Time
	if err := db.Pool().QueryRow(ctx, `SELECT accepted_at FROM invites WHERE id = $1`, inviteID).
		Scan(&acceptedAt); err != nil {
		t.Fatalf("read the invite back: %v", err)
	}
	if acceptedAt != nil {
		t.Error("the invite was stamped accepted although nothing else was written")
	}
}
```

`accounts.Link` and `countRows` are placeholders for whatever the package already calls them — read `telegram_account_repo.go` for the binding method's real name, and reuse the file's existing row-counting helper rather than adding a second one.

- [ ] **Step 8: Mutation check 3 — the browser-session guard on admit**

Move `c.Post("/household/invites/{id}/admit", …)` out of the `requireCookieSession` group (leave it under `requireOwner` + `requireCSRF`). Run the guard-matrix test. It must fail with the API-token case reaching the route. Put it back.

- [ ] **Step 9: Run everything**

```bash
cd api && go build ./... && go test ./... 2>&1 | tail -20
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add api/internal/usecase/ports.go api/internal/usecase/invite.go api/internal/usecase/invite_test.go api/internal/usecase/testdouble_test.go api/internal/domain/errors.go api/internal/adapter/http/errors.go api/internal/adapter/http/invite_lobby_handlers.go api/internal/adapter/http/router.go api/internal/adapter/http/pending_invites_api_test.go api/internal/adapter/postgres/queries/identity.sql api/internal/adapter/postgres/sqlcgen api/internal/adapter/postgres/invite_repo.go api/internal/adapter/postgres/invite_repo_test.go api/cmd/hearthctl/routes.go
git commit -m "feat: Let in creates the member in one transaction and sends their link

User, membership, telegram_accounts row and acceptance stamp together, or
none of them. The sign-in link goes out after the commit, and a failed send
is reported as signInSent: false rather than hidden."
```

---

## Task 10: The frontend's data layer — schema, poll, mutations

No visible change yet. This is the hook and schema work the two UI tasks build on, and it is where the polling rule and the invalidation rules live.

**Files:**
- Modify: `web/src/features/settings/schemas.ts`
- Modify: `web/src/features/settings/usePendingInvites.ts`
- Test: `web/src/features/settings/usePendingInvites.test.ts` (create)

**Interfaces:**
- Consumes: the list route's `channel` and `knock` (Task 3), the two new routes (Tasks 8, 9).
- Produces:
  - `pendingInviteSchema` gains `channel: z.string().default("email")` and `knock: inviteKnockSchema.nullish()`.
  - `export function invitePollInterval(invites: PendingInvite[] | undefined): number | false`
  - `export function useNewInviteLink()` → mutation over `POST /household/invites/{id}/link`, returns `{link, expiresAt}`.
  - `export function useAdmitInvite()` → mutation over `POST /household/invites/{id}/admit`, returns `{member, signInSent}`.
  - `pendingInvitesQueryKey` is unchanged (`["household", "invites"]`).

- [ ] **Step 1: Write the failing test**

Create `web/src/features/settings/usePendingInvites.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { invitePollInterval } from "./usePendingInvites";
import { pendingInviteSchema } from "./schemas";

describe("pendingInviteSchema", () => {
  // Milestone 1's server sends neither field. The frontend must read that
  // as an email invite with no knock, so the two milestones deploy
  // independently.
  it("reads a row from a server that predates channel and knock", () => {
    const row = pendingInviteSchema.parse({
      id: "1", name: "Jane", email: "jane@example.com", role: "owner",
      capabilities: ["money"], expiresAt: "2026-09-27T00:00:00Z",
    });
    expect(row.channel).toBe("email");
    expect(row.knock ?? null).toBeNull();
  });

  it("reads a knock whose Telegram username is null", () => {
    const row = pendingInviteSchema.parse({
      id: "1", name: "Christine", email: "", role: "owner",
      capabilities: ["money"], expiresAt: "2026-09-21T00:00:00Z",
      channel: "telegram",
      knock: { username: null, code: "4812", knockedAt: "2026-09-20T10:00:00Z" },
    });
    expect(row.knock?.username ?? null).toBeNull();
    expect(row.knock?.code).toBe("4812");
  });
});

describe("invitePollInterval", () => {
  const waiting = { id: "1", name: "C", email: "", role: "owner", capabilities: [],
    expiresAt: "", channel: "telegram", knock: null } as const;
  const knocked = { ...waiting, id: "2", knock: { username: "c_t", code: "4812", knockedAt: "" } };
  const emailRow = { ...waiting, id: "3", channel: "email" };

  // The poll exists to catch a knock. Once there is one, or once there is
  // nothing that could produce one, it stops -- the TelegramPanel rule,
  // asserted against the exported function rather than over real elapsed
  // time (TelegramPanel.test.tsx says why).
  it("polls only while a Telegram invite is still waiting for its knock", () => {
    expect(invitePollInterval([waiting])).toBe(3000);
    expect(invitePollInterval([waiting, knocked])).toBe(3000);
    expect(invitePollInterval([knocked])).toBe(false);
    expect(invitePollInterval([emailRow])).toBe(false);
    expect(invitePollInterval([])).toBe(false);
    expect(invitePollInterval(undefined)).toBe(false);
  });
});
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd web && npx vitest run src/features/settings/usePendingInvites.test.ts
```

Expected: FAIL — `invitePollInterval` is not exported, and `channel` is stripped by the schema.

- [ ] **Step 3: Extend the schema**

In `web/src/features/settings/schemas.ts`:

```ts
// A knock is one tap on a Telegram invite link. `username` is null when
// Telegram sent none -- an @username is optional, and the card says so in
// words rather than rendering an empty "@". `code` is the four digits the
// owner compares with the phone in front of them; it is display-only and no
// request ever carries it back.
export const inviteKnockSchema = z.object({
  username: z.string().nullable(),
  code: z.string(),
  knockedAt: z.string(),
});

export const pendingInviteSchema = z.object({
  id: z.string(),
  name: z.string(),
  email: z.string(),
  role: z.string(),
  capabilities: z.array(z.string()),
  // Both default to the milestone-1 shape, so a server that predates
  // migration 00021 still parses: this is the same rule the /me schema's
  // `features` default follows (auth/schemas.ts), and the reason the two
  // milestones can deploy independently.
  channel: z.string().default("email"),
  knock: inviteKnockSchema.nullish(),
  expiresAt: z.string(),
});
```

- [ ] **Step 4: Add the poll rule and the two mutations**

In `web/src/features/settings/usePendingInvites.ts`:

```ts
// invitePollInterval is exported so a test can assert the rule directly:
// proving it over real elapsed time would mean waiting out several
// three-second polls (the reason TelegramPanel.test.tsx gives for the same
// shape).
//
// The poll exists for one event -- a knock arriving from a phone the
// browser cannot hear about any other way. It runs only while something
// could still knock, and stops the moment one has, so a settled Settings
// tab is not refetching every three seconds forever.
export function invitePollInterval(invites: PendingInvite[] | undefined): number | false {
  const waiting = (invites ?? []).some(
    (invite) => invite.channel === "telegram" && !invite.knock,
  );
  return waiting ? 3000 : false;
}

export function usePendingInvites({ enabled }: { enabled: boolean }) {
  return useQuery({
    queryKey: pendingInvitesQueryKey,
    queryFn: fetchPendingInvites,
    enabled,
    refetchInterval: (query) => invitePollInterval(query.state.data),
    // A hidden tab has nobody watching it. TanStack's default already
    // pauses interval refetching in the background; this states it, because
    // the whole point of the interval is a person looking at the screen.
    refetchIntervalInBackground: false,
  });
}

export function useNewInviteLink() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<InviteLink> =>
      fetchAndParse(inviteLinkSchema, `/api/v1/household/invites/${encodeURIComponent(id)}/link`, {
        method: "POST",
      }),
    // The knock is cleared server-side, so the row on screen is stale the
    // moment this returns -- and it is stale on a failure too (another
    // owner may have withdrawn the invite), which is why this is onSettled.
    onSettled: () => queryClient.invalidateQueries({ queryKey: pendingInvitesQueryKey }),
  });
}

export function useAdmitInvite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<AdmitResult> =>
      fetchAndParse(admitResultSchema, `/api/v1/household/invites/${encodeURIComponent(id)}/admit`, {
        method: "POST",
      }),
    // Three lists change, and naming each one is the rule
    // docs/LEARNING.md pattern 22 exists for: the invite leaves the pending
    // list, the member joins the members list, and Overview's setup
    // checklist counts owners -- a screen that reads a derived figure needs
    // its query invalidated by name, or it shows yesterday's answer.
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: pendingInvitesQueryKey }),
        queryClient.invalidateQueries({ queryKey: householdMembersQueryKey }),
        queryClient.invalidateQueries({ queryKey: meQueryKey }),
      ]),
  });
}
```

Add `inviteLinkSchema` and `admitResultSchema` to `schemas.ts` beside the others, matching Tasks 8 and 9's response bodies exactly.

- [ ] **Step 5: Run the test and watch it pass**

```bash
cd web && npx vitest run src/features/settings/ && npx tsc --noEmit
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/settings/schemas.ts web/src/features/settings/usePendingInvites.ts web/src/features/settings/usePendingInvites.test.ts
git commit -m "feat(web): the pending-invite list carries a channel and a knock

The poll runs only while something could still knock and stops the moment
one has. Admit refreshes the members list and the Overview checklist by
name, not only the invite list."
```

---

## Task 11: The waiting card

One component, three states: waiting for a tap, somebody has tapped, and (briefly) let in. It is mounted in two places — the modal's success state and the Pending section — so it owns no fetching of its own beyond the two mutations.

**Files:**
- Create: `web/src/features/settings/PendingInviteCard.tsx`
- Create: `web/src/features/settings/InviteLinkShare.tsx`
- Create: `web/src/features/settings/PendingInviteCard.test.tsx`
- Modify: `web/src/features/settings/PendingInvitesList.tsx`
- Modify: `web/src/features/settings/copy.ts`
- Modify: `web/package.json` (add `qrcode-generator`)

**Interfaces:**
- Consumes: `PendingInvite`, `useNewInviteLink`, `useAdmitInvite`, `useWithdrawInvite` (Task 10).
- Produces:
  ```tsx
  export function PendingInviteCard({
    invite,
    link,          // the raw link, only while this session still holds it
    onNewLink,     // called with the fresh link so the parent can keep it
  }: {
    invite: PendingInvite;
    link?: string;
    onNewLink?: (link: string) => void;
  }): JSX.Element
  ```

- [ ] **Step 1: Add the QR dependency, pinned exact**

```bash
cd web && npm install --save-exact qrcode-generator@2.0.4
```

`docs/LEARNING.md` pattern 7: pinned exact, no `^`. It is MIT, has no dependencies of its own, ships its own types, and runs in the browser only — **the link never travels back to the server to be rendered**, which is the reason a client-side generator was chosen over an image endpoint.

- [ ] **Step 2: Write the failing test**

Create `web/src/features/settings/PendingInviteCard.test.tsx`:

```tsx
// The card's three states. Each is what the owner is looking at while they
// wait for a person in the same room to pick up their phone, so the words
// matter as much as the buttons.
describe("PendingInviteCard", () => {
  it("shows the link and the shown-once warning while nobody has knocked", () => {
    render(<PendingInviteCard invite={waitingInvite} link="https://t.me/HearthBot?start=inv_abc" />);
    expect(screen.getByRole("button", { name: /copy/i })).toBeInTheDocument();
    expect(screen.getByText(/shown once/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /let in/i })).not.toBeInTheDocument();
  });

  it("offers a new link, not the old one, when this session no longer holds it", () => {
    render(<PendingInviteCard invite={waitingInvite} />);
    expect(screen.queryByRole("button", { name: /copy/i })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /get a new link/i })).toBeInTheDocument();
  });

  it("asks the owner to compare the code once somebody has knocked", () => {
    render(<PendingInviteCard invite={knockedInvite} />);
    expect(screen.getByText(/@christine_t tapped the link/i)).toBeInTheDocument();
    expect(screen.getByText("4812")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /let in/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /not them/i })).toBeInTheDocument();
  });

  // A @username is optional on Telegram, and the codebase never falls back
  // to a first name (adapter/telegram/update.go, senderName). The card says
  // so plainly instead of rendering an empty "@".
  it("says so plainly when the knocker has no Telegram username", () => {
    render(<PendingInviteCard invite={{ ...knockedInvite, knock: { ...knockedInvite.knock!, username: null } }} />);
    expect(screen.getByText(/someone with no telegram username tapped the link/i)).toBeInTheDocument();
    expect(screen.getByText("4812")).toBeInTheDocument();
  });

  it("names a sign-in link that could not be sent", async () => {
    stubFetchRoutes({
      "POST /api/v1/household/invites/2/admit": {
        status: 200,
        body: { member: { id: "m1", name: "Christine", role: "owner", capabilities: ["money"] }, signInSent: false },
      },
      "GET /api/v1/household/invites": { status: 200, body: [] },
      "GET /api/v1/household/members": { status: 200, body: [] },
      "GET /api/v1/auth/me": { status: 200, body: meBody },
    });
    render(<PendingInviteCard invite={knockedInvite} />);
    fireEvent.click(screen.getByRole("button", { name: /let in/i }));
    expect(await screen.findByText(/if no message arrived, ask them to send \/start to the bot/i)).toBeInTheDocument();
  });
});
```

**`stubFetchRoutes` throws on any unregistered request.** Every route the card touches must be registered in *every* test that renders it, and every existing test that renders `PendingInvitesList` or `MembersPanel` now needs the two new routes registered too. Budget a step for that — it is the trap milestone 1 hit.

- [ ] **Step 3: Run it and watch it fail**

```bash
cd web && npx vitest run src/features/settings/PendingInviteCard.test.tsx
```

Expected: FAIL — the module does not exist.

- [ ] **Step 4: Write `InviteLinkShare`**

One job: hand a link over three ways. Kept separate from the card because the card is about *state* and this is about *one link* — and because a QR canvas in the middle of a state machine is the file nobody wants to read.

```tsx
// Copy, QR and Share-to-Telegram for one invite link.
//
// The QR is drawn in the browser by qrcode-generator, never fetched: the
// link is a live credential for 24 hours, and an image endpoint would put
// it in a server log and a browser cache.
export function InviteLinkShare({ link }: { link: string }) {
  // qrcode-generator's own API: typeNumber 0 lets it choose a size, "M" is
  // the middling error-correction level -- a code shown on a screen for
  // twenty seconds needs no more.
  const svg = useMemo(() => {
    const qr = qrcode(0, "M");
    qr.addData(link);
    qr.make();
    return qr.createSvgTag({ cellSize: 4, margin: 2 });
  }, [link]);
  …
}
```

Share to Telegram is `https://t.me/share/url?url=<encodeURIComponent(link)>`, opened with `window.open(url, "_blank", "noopener")`.

- [ ] **Step 5: Write `PendingInviteCard`**

Three states, decided in this order — **read them off the data, never off a local flag**, so two tabs looking at the same invite agree:

1. `invite.knock` is present → **knocked**: "@name tapped the link" or "Someone with no Telegram username tapped the link", then "Does their phone show **4812**?", with [Let in], [Not them], [Withdraw].
2. no knock, `link` prop present → **waiting, link in hand**: `InviteLinkShare`, "Shown once. You can get a new link any time.", [Get a new link], [Withdraw].
3. no knock, no link → **waiting, link not in hand**: the expiry line, [Get a new link], [Withdraw].

An email invite renders milestone 1's row unchanged — the card is only for `channel === "telegram"`.

"Not them" and "Get a new link" are the **same mutation** (`useNewInviteLink`); only the button's label and its confirmation copy differ. Say that in a comment, because the next person will look for two endpoints.

Put the words in `copy.ts` beside `pendingInviteExpiryLine`, so the tests and the component cannot drift:

```ts
// The knocker's line. Telegram's @username is optional and this codebase
// never falls back to a first name (adapter/telegram/update.go, senderName
// explains why: a first name is attacker-chosen, so "@andreas" could be
// forged). With no username the sentence carries no identity at all, which
// is honest -- the four-digit code is what identifies them.
export function knockLine(username: string | null): string {
  return username ? `@${username} tapped the link` : "Someone with no Telegram username tapped the link";
}
```

- [ ] **Step 6: Mount it in the list**

In `PendingInvitesList.tsx`, render `PendingInviteCard` for a `channel === "telegram"` row and keep `PendingInviteRow` for an email one. The list holds no link — a link is only ever in the hand of the session that just minted it — so it passes no `link` prop.

- [ ] **Step 7: Register the new routes in every test that renders the list**

```bash
cd web && npx vitest run src/features/settings/ 2>&1 | tail -30
```

Every failure reading `no stub registered for POST /api/v1/household/invites/…` is this step's work. Add the routes to those tests' `stubFetchRoutes` maps.

- [ ] **Step 8: Run everything and watch it pass**

```bash
cd web && npx vitest run && npx tsc --noEmit && npx eslint src
```

- [ ] **Step 9: Commit**

```bash
git add web/package.json web/package-lock.json web/src/features/settings/PendingInviteCard.tsx web/src/features/settings/InviteLinkShare.tsx web/src/features/settings/PendingInviteCard.test.tsx web/src/features/settings/PendingInvitesList.tsx web/src/features/settings/copy.ts web/src/features/settings/*.test.tsx
git commit -m "feat(web): the waiting card, from link in hand to Let in

Three states read off the invite itself, never off a local flag, so two
tabs looking at the same invite agree. The QR is drawn in the browser: the
link is a live credential and must not reach a server log."
```

---

## Task 12: The modal picks a channel, and stays open

With `email_invites` off, an owner invite needs no address and the modal's success state becomes the waiting card instead of closing.

**Files:**
- Modify: `web/src/features/settings/InviteMemberModal.tsx`
- Modify: `web/src/features/settings/useInviteMember.ts`
- Modify: `web/src/features/settings/InviteMemberModal.test.tsx`

**Interfaces:**
- Consumes: `features` from `/me` (`useAuth`), `PendingInviteCard` (Task 11).
- Produces: `useInviteMember()` sends `{name, role, capabilities, channel, email?}` and returns `{id, expiresAt, link?}`.

- [ ] **Step 1: Write the failing test**

```tsx
// With mail hidden, an owner invite is a Telegram link and needs no
// address at all -- ErrInviteRequiresEmail guarded delivery, not identity
// (spec decision 9).
it("asks for no email when email invites are off", () => {
  renderModal({ features: { email_invites: false, telegram_sign_in: true } });
  expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
});

it("offers the channel choice when the operator turns email invites on", () => {
  renderModal({ features: { email_invites: true, telegram_sign_in: true } });
  expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
});

// Neither channel available: say so, rather than offering a button that
// goes nowhere (spec decision 11).
it("says inviting is unavailable when neither channel exists", () => {
  renderModal({ features: { email_invites: false, telegram_sign_in: false } });
  expect(screen.getByText(/inviting is unavailable on this install/i)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /send invite/i })).not.toBeInTheDocument();
});

// The modal does not close on success: the link is shown once, and closing
// would throw it away.
it("becomes the waiting card when a Telegram invite is created", async () => {
  stubFetchRoutes({
    "POST /api/v1/household/members/invite": {
      status: 201,
      body: { id: "1", expiresAt: "2026-09-21T00:00:00Z", link: "https://t.me/HearthBot?start=inv_abc" },
    },
    // …every route the card and the list then fetch
  });
  renderModal({ features: { email_invites: false, telegram_sign_in: true } });
  fireEvent.change(screen.getByLabelText(/name/i), { target: { value: "Christine" } });
  fireEvent.click(screen.getByRole("button", { name: /send invite/i }));
  expect(await screen.findByRole("button", { name: /copy/i })).toBeInTheDocument();
  expect(screen.getByText(/shown once/i)).toBeInTheDocument();
});

// A limited member with no sign-in is today's kid path, untouched: the
// member is created directly, with no invite row, so there is no channel to
// choose and no link to hand over.
it("keeps the profile-only path for a kid", async () => {
  const stub = stubFetchRoutes({
    "POST /api/v1/household/members/invite": { status: 201, body: {} },
    "GET /api/v1/household/invites": { status: 200, body: [] },
    "GET /api/v1/household/members": { status: 200, body: [] },
    "GET /api/v1/auth/me": { status: 200, body: meBody },
  });
  renderModal({ features: { email_invites: false, telegram_sign_in: true } });
  fireEvent.change(screen.getByLabelText(/name/i), { target: { value: "Kayla" } });
  fireEvent.change(screen.getByLabelText(/role/i), { target: { value: "limited" } });
  fireEvent.click(screen.getByRole("button", { name: /send invite/i }));

  await waitFor(() => expect(stub.calls("POST /api/v1/household/members/invite")).toHaveLength(1));
  const body = JSON.parse(stub.calls("POST /api/v1/household/members/invite")[0].body as string);
  // "profile" is said out loud: the route refuses an omitted channel, and a
  // kid writes no invite row, so it is not "email with no address".
  expect(body.channel).toBe("profile");
  expect(body.email ?? "").toBe("");
  // No link came back, so the modal closes rather than becoming a card.
  expect(screen.queryByRole("button", { name: /copy/i })).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run it, watch it fail, then implement**

The role select gains a third meaning for `limited`: "Profile only (no sign-in)" — today's path, created directly — or "Can sign in (Telegram link)". Keep the existing capability toggles exactly as they are.

Decide the channel like this, and put the reason in a comment:

```tsx
// The channel is decided here and sent explicitly, never inferred
// server-side from whether an email field was filled in: the route parses
// `channel` with a default that refuses (spec, API section), and a UI that
// left it out would be refused rather than guessed at. That is deliberate
// -- guessing is how an invite goes to a channel nobody meant.
```

- [ ] **Step 3: Run the whole frontend suite**

```bash
cd web && npx vitest run && npx tsc --noEmit && npx eslint src
```

- [ ] **Step 4: Commit**

```bash
git add web/src/features/settings/InviteMemberModal.tsx web/src/features/settings/useInviteMember.ts web/src/features/settings/InviteMemberModal.test.tsx
git commit -m "feat(web): the invite modal picks a channel and keeps the link on screen

With mail hidden, an owner invite needs no address. The modal's success
state becomes the waiting card, because the link is shown once and closing
would throw it away."
```

---

## Task 13: The operator's screen reads a Telegram invite honestly

`ListPendingInvitesForAdmin` now returns a `NULL` email for a Telegram invite. Task 3 made that compile as `""`; this makes it read as words instead of a blank cell.

**Files:**
- Modify: `web/src/features/admin/AdminHouseholdPage.tsx:151-170`
- Modify: `web/src/features/admin/AdminHouseholdPage.test.tsx`
- Modify: `api/internal/adapter/postgres/queries/admin_directory.sql` (add `i.channel`)
- Modify: `api/internal/usecase/ports.go` (the admin `PendingInvite` type), `admin_directory_repo.go`, the admin handler DTO and the frontend schema

- [ ] **Step 1: Write the failing test**

```tsx
it("shows a Telegram invite as a Telegram link, not a blank cell", () => {
  renderAdminHousehold({
    pendingInvites: [{ name: "Christine", email: "", channel: "telegram", role: "owner",
      expiresAt: "2026-09-21T00:00:00Z", invitedByName: "Andreas" }],
  });
  expect(screen.getByText("Telegram link")).toBeInTheDocument();
});
```

- [ ] **Step 2: Carry `channel` through the admin path and render it**

Four small edits (query, repo, DTO, component) plus the frontend schema. The rule on screen: `channel === "telegram" ? "Telegram link" : invite.email`.

- [ ] **Step 3: Run and commit**

```bash
cd api && go test ./internal/adapter/http/ -run Admin -v
cd ../web && npx vitest run src/features/admin/ && npx tsc --noEmit
```

```bash
git add api/internal/adapter/postgres/queries/admin_directory.sql api/internal/adapter/postgres/sqlcgen api/internal/adapter/postgres/admin_directory_repo.go api/internal/usecase/ports.go api/internal/adapter/http/admin_directory_handlers.go web/src/features/admin/AdminHouseholdPage.tsx web/src/features/admin/AdminHouseholdPage.test.tsx web/src/features/admin/schemas.ts
git commit -m "fix(admin): a Telegram invite reads as a Telegram link, not a blank"
```

---

## Task 14: ADR 11, and the three documents that must stay true

Not a tidy-up afterwards — part of the work, per CLAUDE.md. A defect nobody wrote down gets rebuilt, and a feature nobody ticked off gets built twice.

**Files:**
- Create: `docs/adr/0011-joining-a-household-by-knock.md`
- Modify: `docs/adr/0004-telegram-as-a-second-delivery-channel.md:185-189` (the out-of-scope item)
- Modify: `docs/adr/0003-mail-stays-on-the-box.md` (consequences)
- Modify: `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`

- [ ] **Step 1: Write ADR 11**

Follow the shape of `docs/adr/0010-binding-a-chat-needs-a-confirm.md` — that is the decision this one extends. Cover, at minimum:

- **Context:** production mail cannot leave the box (ADR 3), so a self-serve household cannot get a second owner, and Agreements stays locked for them.
- **Decision:** a tap records a knock and nothing else; a signed-in owner admits; the four digits are compared by eye and accepted by no endpoint; one knock per link.
- **Why the code is not typed in:** ADR 4 rejected guessable one-time codes. This is not one — it grants nothing, so there is nothing to guess. **If it is ever typed into a form, that rejection applies again.**
- **Why the deciding click is in the browser:** ADR 10's reasoning, unchanged — a stolen link must not reach the decision.
- **Consequences:** email invites are flag-gated and off by default; the operator's mail viewer now carries fewer live invite links than before, which narrows (but does not close) the security review's finding 3; the bot answers only private chats (finding 2), which the knock depends on.

- [ ] **Step 2: Amend ADR 4 and ADR 3 with one line each**

ADR 4's out-of-scope item gets a closing line rather than a rewrite:

```markdown
- **Invites over Telegram.** … **Done: [ADR 11](0011-joining-a-household-by-knock.md), 2026-09-20.**
```

ADR 3 gets a line in its consequences saying email invites are now behind `email_invites`, off by default, and pointing at ADR 11. **Follow ADR 3's own amendment convention** — it amends at the bottom and leaves the original text standing.

- [ ] **Step 3: Update `docs/SYSTEM_DESIGN.md`**

Use the **`maintaining-system-design`** skill. This milestone trips several of its triggers at once: new routes and their guards, new columns, three new ports, a reshaped `/start` flow. Change the prose under each diagram too — that is where the non-obvious reasoning lives, and a diagram whose caption still says invites go by email is worse than no diagram.

- [ ] **Step 4: Update `docs/FEATURE_TRACKER.md`**

- "Telegram invites" (§ Identity) ⬜ → ✅.
- Add rows for anything built here that no row covers: the `email_invites` flag, the invite lobby itself, private-chat-only bot handling.
- **Recount the summary table at the top.** Its columns must sum to the stated totals; recount rather than guessing.

- [ ] **Step 5: Update `docs/LEARNING.md`**

One entry per defect worth remembering. At minimum:

- **The group-chat finding** — a credential was checked but not bound to the context that presented it. The security review names findings 1, 2 and 5 as one pattern; if that pattern already has a section, add this as evidence there rather than starting a new one.
- **Whatever actually broke while building this.** Write it down as it happens, not from memory at the end.

- [ ] **Step 6: Commit (explicit paths only)**

```bash
git add docs/adr/0011-joining-a-household-by-knock.md docs/adr/0004-telegram-as-a-second-delivery-channel.md docs/adr/0003-mail-stays-on-the-box.md docs/SYSTEM_DESIGN.md docs/FEATURE_TRACKER.md docs/LEARNING.md
git commit -m "docs: ADR 11, joining a household by knock"
```

**Do not** `git add docs`. The working tree carries `docs/SKILL_TRACKER.md` and `docs/reviews/2026-09-19-security-review.md`, which belong to somebody else's change.

---

## Task 15: The browser walk

`make lint && make test` green is not the claim that this works. The product owner asked for a real-browser walk explicitly on 2026-07-30, after a feature verified "15 of 15" still surprised them in first-run use. Use the **`verifying-in-the-real-environment`** skill.

**Files:**
- Create: `docs/superpowers/plans/2026-09-20-hearth-partner-invite-lobby-m2-verification.md`

**Setup**

```bash
colima start
make up                       # not make dev -- it blocks tailing logs
lsof -nP -iTCP:5173 -sTCP:LISTEN   # two Docker engines exist here; confirm which one answers
make seed                     # prints sign-in details
```

**Restart the web container after the last frontend edit**, before walking. A stale module already cost one walk in September.

This walk needs **a real second phone on the development bot** (`@HearthOinkDevBot`, token in the local `.env` only — the production token was unusable for a dev walk; see the Telegram commands record).

- [ ] **Run the fifteen criteria and record each one**

1. Sign in as the seeded owner. Settings → Members shows "+ Invite".
2. The modal asks for no email address (`email_invites` is off) and offers Parent.
3. Submitting creates the invite; the modal becomes the waiting card, with the link, a QR code and "Shown once."
4. Copy puts a `https://t.me/…?start=inv_…` URL on the clipboard.
5. The Pending section lists the invite with role, channel and expiry.
6. On the second phone, tapping the link makes the bot reply with a four-digit code.
7. Within three seconds, and with no reload, the owner's card says "@… tapped the link" and shows **the same four digits**.
8. A second tap of the same link gets the dead-link reply, and the card does not change.
9. **Not them** replaces the link, the card returns to waiting, and the phone is told the link is no longer valid.
10. Tapping the *new* link knocks again, with a *different* code.
11. **Let in** creates the member: the Members list shows them, with the role the invite carried.
12. The phone receives a sign-in link, and it signs that person in — as a member of this household, with the right role.
13. Overview's setup checklist "Invite your partner" step is now done, without a reload.
14. Agreements is unlocked, which is the reason this milestone exists.
15. Sign in as a **limited** member: no Pending section, no invite controls, and no error message in place of them.

Record each criterion with what was actually observed, not "as expected". Where something surprised you, write the surprise down — that is the entry `docs/LEARNING.md` wants.

- [ ] **Run the four mutation checks end to end**

Each was run in its own task. Run them again on the finished branch, because a later task can quietly re-open an earlier hole:

1. Household filter off `ListPending` → a milestone-1 test fails.
2. `knocked_at IS NULL` off `RecordKnock` → the race test and the one-knock test fail.
3. `requireCookieSession` off admit → the guard-matrix test fails.
4. The web form's Telegram refusal removed → `TestTheWebFormCannotAcceptATelegramInvite` fails.

- [ ] **Final gate**

```bash
make lint && make test
```

Both green, on the tree being integrated. If anything is red, it is not done — say so plainly with the output rather than describing the work as finished.

- [ ] **Commit the verification record**

```bash
git add docs/superpowers/plans/2026-09-20-hearth-partner-invite-lobby-m2-verification.md
git commit -m "docs: milestone 2 browser walk, fifteen criteria"
```

---

## Open questions settled before this plan was written

Handover §5, closed. Do not relitigate these mid-task; if one turns out wrong, say so explicitly rather than quietly doing something else.

| # | Question | Answer |
|---|---|---|
| 1 | ADR 4 amendment or a new ADR? | **New ADR 11**, plus a one-line pointer in ADR 4's out-of-scope item and in ADR 3's consequences. Task 14. |
| 2 | Link lifetime | **24 hours**, confirmed by the owner 2026-09-20. |
| 3 | Wording when the knocker has no `@username` | "Someone with no Telegram username tapped the link." Task 11. |
| 4 | Several knocks on one link | **One.** Spec decision 2. The PRD still listed it as open; Task 14 closes that line. |
| 5 | Role change at Let in | **No.** Spec decision 14 — Let in grants exactly what the invite said; changing it afterwards is an ordinary member edit. |
| 6 | Existing pending email invites once email is hidden | They stay listed and withdrawable. "Get a new link" on an email invite is a 409 (`INVITE_NOT_TELEGRAM`). |
| 7 | Security review finding 2 (group chats) | **Folded into this milestone as Task 1**, decided by the owner 2026-09-20: the knock records whatever chat taps, so the milestone would otherwise inherit the finding. |

Findings 1, 3 and 5–18 of the security review are **out of scope here** and stay open. Finding 3 (the admin mail viewer showing working links) narrows on its own as email invites go behind their flag, and ADR 11 says so in its consequences — it is not closed.
