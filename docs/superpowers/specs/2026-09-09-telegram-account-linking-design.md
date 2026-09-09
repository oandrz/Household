# Hearth Telegram account linking — design

**Date:** 2026-09-09. Extends
[ADR 4](../../adr/0004-telegram-as-a-second-delivery-channel.md) (Telegram as a
second delivery channel) and the sign-in work specified in
[2026-09-01 Telegram sign-in](2026-09-01-telegram-sign-in-design.md).

## What

A signed-in member connects their existing Hearth account to their Telegram
chat from Settings, and disconnects it again.

Today they cannot. `telegram_accounts` rows are written in exactly one place —
inside `SignupRepository.Provision`'s transaction, when a *stranger* creates a
household from a chat. `usecase/ports.go` says so out loud:

> `TelegramAccountRepository` resolves a Telegram chat to the Hearth user it is
> bound to. The binding itself is written inside `SignupRepository.Provision`'s
> transaction, which is why there is no `Create` method here.

So a member who signed up by email and then messages the bot is an *unbound*
chat, and `TelegramAuthService.HandleStart` answers an unbound chat with a
**sign-up** link. The person asking to connect their phone is offered a second
household instead. Everything built on the binding — `/spend`, `/balance`,
`/recent`, free-text intents, the daily digest — is unreachable to every
account that did not begin life in a chat.

## Decisions

1. **Browser-first, two-phase, with the confirm in the minting session.**
   The flow is: Settings mints a nonce carrying the member's `user_id` → the
   deep link opens `t.me/<bot>?start=<nonce>` → the bot's `/start` *consumes
   the nonce and writes nothing to `telegram_accounts`*, replying "go back to
   Hearth and confirm" → the browser that minted it sees which chat redeemed
   it and confirms → the binding is written.

   **The second phase is the security of this feature, not ceremony.** A
   one-phase bind (nonce carries the user, `/start` binds immediately) turns a
   leaked ten-minute deep link — a forwarded message, a shared screen, a
   clipboard — into account takeover: whoever redeems it binds *their* chat to
   *your* account, and from then on a `/start` in their chat mints a magic
   link for your user and signs them in as you. Today's unbound nonce cannot
   do that: redeemed by a stranger it only ever produces a sign-in link for
   the stranger's own chat, or a sign-up for a household of their own. A
   one-phase bind would therefore make an already-reviewed flow strictly
   weaker, which is the thing this design exists to avoid. The confirm keeps
   the deciding click inside a session that is already authenticated, where a
   stolen link cannot reach.

   **To be recorded as ADR 10** when the work lands (it is not written yet),
   because the step looks redundant to anyone reading only the happy path, and
   the next person to touch this flow will otherwise delete it.

2. **The chat gets a bland answer; the minting session gets the reason.**
   Four situations can arrive at `/start` with a link nonce, and the chat is
   told apart only by what it is entitled to know:

   | Situation | The chat is told | The browser is told |
   |---|---|---|
   | Clean | "Go back to Hearth and confirm this chat." | pending, naming the redeeming chat |
   | Chat already bound to the same user | "This chat is already connected." | already connected |
   | Chat bound to a *different* user | "Could not connect this chat. Open Hearth to see why." | that chat belongs to another Hearth account |
   | User already bound to a *different* chat | the same bland line | disconnect the other chat first |

   The asymmetry is deliberate and is the mirror of the sign-in flow's single
   `telegramDeadLinkMessage`: a chat holding a nonce it may have stolen must
   not be able to learn whether the target account exists, already has a chat,
   or belongs to someone else, because each of those is a probe. The session
   that minted the nonce has already proved who it is, so it gets the sentence
   that actually helps.

3. **One nullable column carries the whole feature.**
   `telegram_link_requests.user_id uuid NULL REFERENCES users(id) ON DELETE
   CASCADE`. `user_id IS NOT NULL` is what distinguishes a *link* nonce from a
   *sign-in* nonce in the table they share — not a payload prefix, which stays
   free for the still-unbuilt `inv_<token>` Telegram invites row.

4. **The link branch is taken before the per-chat rate limit, not after.**
   `HandleStart` today consumes the nonce, *then* checks
   `CountLinksSince` against `telegramLinksPerHourLimit`, and answers the
   dead-link message when the chat is over it. A link nonce must be branched
   on before that check, and not only before the sign-in/sign-up split, or the
   two ends of the flow disagree: the row would be consumed and carrying a
   `user_id`, which decision 5 derives as *pending*, so the chat would be told
   "that link is dead" while the browser cheerfully offered a Confirm button
   that worked. The limit is safe to skip here because of what it is for —
   redemption on the sign-in path *mints* a magic-link or sign-up row, and
   this path mints nothing at redemption. Decision 12's per-user cap is what
   bounds this path, and it is applied at mint, where the session is known.

5. **No pending table and no status column.** `Consume` already stamps
   `chat_id` on the row in the same statement (the table's
   `consumed_rows_name_their_chat` CHECK exists to guarantee it). *Consumed,
   carrying a `user_id`, with no `telegram_accounts` row yet* **is** the
   pending state. Status is derived on read. A second source of truth for
   "where is this link up to" is a second thing to keep consistent, and the
   row already knows.

6. **The confirm lives inside the nonce's existing ten minutes.** `expires_at`
   bounds mint → open Telegram → `/start` → confirm, all of it. Nothing
   legitimate takes longer, and it means an abandoned pending link needs no
   sweeper of its own: it expires, and `PruneTelegramLinkRequests` already
   deletes it.

7. **The confirm screen names the chat.** `telegram_link_requests` gains a
   nullable `chat_username text`, stamped at redemption from Telegram's
   `message.from`. Without it the confirm button asks a question the person
   cannot answer — "some chat opened your link, yes or no?" — and a confirm
   that cannot be verified is a confirm that will always be clicked. With it,
   a member who sees `@stranger123` when they are `@andreas` has the one piece
   of evidence the second phase exists to give them. This is the only part of
   the design that widens the adapter's view of Telegram's payload, and it is
   the smallest widening that makes decision 1 mean something.

8. **A browser session, not an API token.** The link and unlink routes sit
   behind `requireSession` + `requireCSRF` + `requireCookieSession`. One group
   covers the reads too: `requireCSRF` returns early for `GET`, `HEAD` and
   `OPTIONS` (`middleware_csrf.go:28`), so the polling route needs no header
   and the router needs no second group. It copies
   the rule already written for minting API tokens
   ([ADR 7](../../adr/0007-personal-api-tokens.md)): a leaked token must not be
   able to make itself permanent. Binding a chat is exactly that — it would
   give the holder a channel that outlives the token's revocation.

9. **Authorisation stays at the edge.** `TelegramLinkService` takes a
   `userID` as the *subject it acts on*, supplied by the handler from the
   session; no service takes an actor parameter and none is added here
   ([ADR 8](../../adr/0008-authorisation-at-each-channels-inbound-edge.md)).
   The chat-side half of the flow is guarded the way ADR 8 already guards the
   Telegram edge: `HandleStart` decides from the row, never from anything the
   chat claims about itself. `chat_id` remains uncounterfeitable — it arrives
   inside an `Update` over the authenticated long poll, which is the fact
   `TelegramAuthService.sendSignUp`'s comment already leans on.

10. **Any member, not owners only.** A limited member's chat is already refused
   by the `Commander`'s own guard (owner **and** money) and by the digest's
   recipients query, so a linked non-owner chat can do nothing an owner has
   not been given. Gating the panel on ownership would duplicate a guard that
   already exists one layer down and block a member who could safely link.

11. **Disconnect ships in the same slice, and refuses to lock anyone out.**
    `DELETE` refuses when `users.email IS NULL`, because a Telegram-only
    account — the second kind of first-class user, created by Telegram
    sign-up — has no other door: `GetUserByEmail` is `WHERE email = $1` and
    NULL never matches a parameter, so there would be no magic link, no
    password reset and no `adminctl reset-password` to get back in. The fix
    for *those* accounts is the already-tracked ⬜ "attach an email address to
    a Telegram-only account"; until it exists, this guard is what stands
    between a member and a household only `make psql` can reopen. A binding
    with no revoke path at all is worse still — a lost phone with no answer —
    which is why disconnect is not deferred.

12. **Per-user mint cap of three an hour**, mirroring
    `telegramLinksPerHourLimit`. The per-chat limit already bounds
    *redemption*; this bounds *minting*, which a session can now do without a
    chat being involved at all. It is table-growth control, not a security
    control — the session is already authenticated — and it is counted from
    the same table.

## Data

Migration `00018_telegram_link_user.sql`:

```sql
-- +goose Up
-- A link nonce is minted by a signed-in member for their own account; a
-- sign-in nonce is minted by a browser that has not said who it is. The two
-- share a table, and this column is the only thing that tells them apart.
-- Nullable, because the sign-in nonce that has existed since 00011 has no
-- user to name.
ALTER TABLE telegram_link_requests
    ADD COLUMN user_id uuid REFERENCES users(id) ON DELETE CASCADE;

-- Stamped at redemption alongside chat_id, from Telegram's message.from.
-- Read by the confirm screen so the person approving can tell their own chat
-- from someone else's; never used to decide anything server-side, because it
-- is a display name a third party controls.
ALTER TABLE telegram_link_requests
    ADD COLUMN chat_username text;

-- +goose Down
ALTER TABLE telegram_link_requests DROP COLUMN chat_username;
ALTER TABLE telegram_link_requests DROP COLUMN user_id;
```

`telegram_accounts` is unchanged. Its two `UNIQUE` constraints — one per user,
one per chat — remain the real gate on every write below; the service's checks
are for the *message*, the constraints are for the *truth*.

## API

All under the existing `/api/v1/auth` prefix, in a group with
`requireSession`, `requireCSRF`, `requireCookieSession` and
`requireFeature(domain.FlagTelegramSignIn)`, and answering `404` when
`deps.Telegram == nil` exactly as `POST /auth/telegram/start` does — an
install with no bot gives away nothing about whether the feature exists.

| Route | Does |
|---|---|
| `GET /auth/telegram` | This user's binding: connected (chat, `linked_at`) or not |
| `POST /auth/telegram/link` | Mints a link nonce; returns `{ id, url, expires_at }` |
| `GET /auth/telegram/link/{id}` | Derived status: `waiting`, `pending` (+ chat name), `expired`, `refused` (+ reason), `confirmed` |
| `POST /auth/telegram/link/{id}/confirm` | Writes the binding |
| `DELETE /auth/telegram` | Removes it |

Every 2xx carries a JSON body; `DELETE` returns the resulting state rather
than `204`, so `apiFetch` never meets an ok response it cannot parse.

`{id}` is the `telegram_link_requests` row id, not the nonce. It is safe to
hand to the browser because it is useless without the session: both the status
read and `confirm` require `row.user_id == session user`, and the raw nonce
they *do* need to protect never leaves the deep link. A row belonging to
someone else answers `404`, not `403`, on both — the same rule the rest of the
API follows, so a row id cannot be tested for existence.

**The status route derives in this order, and the order matters:** binding
first, then refusals, then expiry. A connected panel that is still polling
when `expires_at` passes must keep reading `confirmed`; deriving expiry first
would flip a working connection to "that link expired" ten minutes after it
succeeded.

## Ports

```
TelegramLinkRepository
  Create(ctx, userID string, nonceHash []byte, expiresAt) error   // userID "" = today's sign-in nonce
  Consume(ctx, nonceHash, chatID, chatUsername) (id, userID string, err error)
  ByID(ctx, id string) (TelegramLinkRequest, error)               // for the browser's status read
  CountMintsSince(ctx, userID string, since) (int, error)
  CountLinksSince(ctx, chatID, since) (int, error)                // unchanged
  Prune(ctx, before) (int64, error)                               // unchanged

TelegramAccountRepository
  ByChatID(ctx, chatID) (userID string, err error)                // unchanged
  ByUserID(ctx, userID) (TelegramBinding, error)
  Create(ctx, userID string, chatID int64) error                  // ErrConflict on either UNIQUE
  Delete(ctx, userID string) error
```

`TelegramAccountRepository`'s doc comment currently explains why it has no
`Create`. That sentence becomes false with this change and is rewritten to say
where bindings now come from — both places — rather than deleted.

## Flow

```
Settings                          API                     Telegram              Bot
   |  POST /auth/telegram/link     |                          |                  |
   |------------------------------>| mint nonce, user_id=me   |                  |
   |<---- { id, url, expires } ----|                          |                  |
   |  open url ---------------------------------------------->|  /start <nonce>  |
   |                               |<--------------------------------------------|
   |                               | Consume -> id, user_id, chat, @name         |
   |                               | user_id set? -> PENDING, write no binding   |
   |                               |--------- "go back to Hearth and confirm" -->|
   |  GET  /link/{id}  (poll 3s)   |                          |                  |
   |<--- pending, chat @name ------|                          |                  |
   |  POST /link/{id}/confirm      |                          |                  |
   |------------------------------>| re-check all, insert binding                |
   |<--------- connected ----------|                          |                  |
```

`HandleStart`'s new branch is tested **before** the existing
`Accounts.ByChatID` sign-in/sign-up split: a link nonce is answered as a link
whether or not the chat happens to be bound already. Every refusal on this
path is answered *in the chat* and returns `nil`, for the reason
`HandleStart`'s own doc comment gives — the poller advances its offset before
dispatching, so an update whose handler returns an error is dropped
permanently and the person is told nothing at all.

Confirm re-checks the full set — row belongs to the session's user, row is
consumed so a chat is known, not expired, neither side already bound — and
maps a `23505` from either `UNIQUE` to a conflict, never a 500. The re-check
is not paranoia about the first check: minutes pass between them, and the
other chat can be bound in between.

## Files touched

```
api/migrations/00018_telegram_link_user.sql          user_id, chat_username
api/internal/adapter/postgres/queries/telegram.sql   mint with user, consume returning user,
                                                     by id, count mints, binding CRUD
api/internal/adapter/postgres/telegram_link_repo.go  widened Create/Consume, ByID, CountMintsSince
api/internal/adapter/postgres/telegram_account_repo.go  ByUserID, Create, Delete
api/internal/usecase/ports.go                        both ports, rewritten doc comments
api/internal/usecase/telegram_link.go                TelegramLinkService: Start, Status, Confirm, Unlink
api/internal/usecase/telegram_auth.go                HandleStart's third branch
api/internal/adapter/telegram/update.go              Update.From, StartCommand sender fields
api/internal/adapter/telegram/poller.go              StartHandler carries the sender
api/internal/adapter/http/telegram_handlers.go       five handlers
api/internal/adapter/http/router.go                  the guarded group
api/cmd/api/main.go                                  wiring
web/src/features/settings/TelegramPanel.tsx          three states
web/src/features/settings/SettingsPage.tsx           the panel
web/src/features/settings/schemas.ts                 responses
api/cmd/hearthctl/routes.go                          the five new routes
docs/adr/0010-binding-a-chat-needs-a-confirm.md      decision 1
```

## Testing

**Usecase, against in-memory doubles:** each row of decision 2's matrix; a
confirm after `expires_at`; a confirm by a *different* signed-in user on
someone else's row; the mint cap at three and at four; unlink refused for an
account with no email; unlink then link again.

**Postgres:** both `UNIQUE` collisions surfacing as conflicts, the derived
status across the row's four shapes, `Consume` returning the bound user,
`Prune` still reaching a pending row once it has expired.

**HTTP, one test per guard so a failure names which one broke:** no session
`401`, API token refused, missing CSRF `403`, flag off `404`, `deps.Telegram`
nil `404`.

**Adapter:** `ParseStart` reading `from` and tolerating its absence — Telegram
omits `from` for channel posts, and a `nil` there must not panic the poller.

**Frontend:** the panel's three states and the polling stop on expiry.

**Mutation-checked, at least these two:** delete the `row.user_id == session
user` check in confirm — a test must go red; make `HandleStart` bind
immediately instead of pending — a test must go red. The second is the whole
of decision 1 expressed as a test, and it is the one that will still be there
when someone decides the confirm step looks unnecessary.

**Browser walk** against `@HearthOinkDevBot`, which is the only claim that
counts (`verifying-in-the-real-environment`): connect a real chat to a real
email account from Settings; watch `/balance` answer in a chat that would
previously have offered to build a second household; check the household
count does not move; disconnect; watch `/balance` be refused again.

## Out of scope

- **Attaching an email address to a Telegram-only account** — the sibling gap,
  already ⬜ in `FEATURE_TRACKER.md`. It is the *other* direction and it is
  what decision 11's guard is waiting for.
- **Telegram invites** (`t.me/…?start=inv_<token>`) — still ⬜, still a payload
  change to the same parsing. This design leaves the `inv_` prefix free for it
  by discriminating on `user_id IS NOT NULL` instead.
- **More than one chat per account.** `telegram_accounts` is unique in both
  directions by design; "disconnect first" is the whole answer here.
- **`hearthctl` coverage.** Sign-up, magic link, Telegram and invite
  acceptance are already listed as deliberate CLI gaps; this joins them. Note
  the difference from `api/cmd/hearthctl/routes.go` in Files touched: *wrapping*
  the flow in a command is out of scope, but the hand-kept routes table is
  diffed against `router.go` both ways by `routes_test.go`, so five new routes
  that are not listed there turn `make test` red.

- **Preserving `/nudges off` across a relink.** Disconnecting deletes the
  `telegram_accounts` row, and `nudges_enabled` goes with it, so an owner who
  had muted the digest and later reconnects the same chat is unmuted. Accepted:
  reconnecting is a deliberate act, unmuting is recoverable with one `/nudges
  off`, and carrying the flag across a deleted row means keeping a tombstone
  whose only job is to remember one boolean.
