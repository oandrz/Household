# Telegram sign-in — a second delivery channel for the links Hearth already mints

**Date:** 2026-09-01
**Status:** Design, approved in brainstorming. Not yet planned or built.

## Why this exists

Production mail does not leave the box. `deploy/.env.example` points `SMTP_ADDR`
at `mailpit:1025`, and Mailpit's UI is bound to `127.0.0.1:8025`, so every
sign-up link, magic link and invite lands in an inbox only the operator can
read, over an SSH tunnel. [ADR 3](../../adr/0003-mail-stays-on-the-box.md)
records why: `oink.mywire.org` is a free Dynu DDNS hostname, Dynu refuses `TXT`
records on free third-level hostnames under 30 days old, no `TXT` means no DKIM,
and no DKIM means no hosted relay will send for it.

That matters more here than in most products, because **mail is not a feature of
this one, it is the entry to it.** `POST /auth/sign-up` answers `202` and mails a
link; the household does not exist until someone clicks it. A stranger who signs
up today waits forever for an email that was delivered to a machine they cannot
reach, and sees no error, because `SignupService.Request` is deliberately silent
in every branch.

A Telegram bot can deliver those same links for **$0 per message, forever**, with
no domain, no DNS record, no business verification and no legal entity. That is
the only free and durable messaging channel available — WhatsApp's free
in-window replies are billed from 1 October 2026, and every SMS route costs per
message.

This design adds Telegram as a **second delivery channel for tokens the system
already mints**. It does not change what a token is, who may hold one, or what
consuming one does.

## Decisions

### 1. Telegram delivers existing tokens. It is not a new identity.

`users.email` stays the identity key wherever a user has one. Telegram carries
the sign-up link and the magic link; consuming either runs the existing
handlers and issues the existing session.

Rejected: making `telegram_user_id` a first-class identity alongside email. It
doubles the identity surface — two unique keys, two enumeration analyses,
`adminctl` lookups reworked, and an account-merge question the product does not
need answered yet.

### 2. Link-back, not cross-device handoff.

The bot replies with a URL. Tapping it signs the user in **on the device holding
Telegram**. There is no polling endpoint and no session-status route.

The obvious alternative — browser starts the flow, polls for completion — has an
account-takeover hole. The nonce is minted by a browser and then bound to
whichever chat taps it, so an attacker can start a flow, send the `t.me/…` link
to a victim ("tap this for me"), and have their own browser signed in as the
victim. No credentials required.

Cost of link-back: a desktop user needs Telegram Desktop or Telegram Web.
Accepted. Cross-device can be added later, but only with a confirmation code the
user types into the originating browser, binding both ends.

### 3. Sign-up and sign-in only. Invites are unchanged.

Invites keep going to an email address and keep being relayed from Mailpit by
hand. The install is two-person; that is one person's inconvenience, not a
blocker. Making the invite a shareable `t.me/…?start=inv_<token>` link is a
worthwhile follow-up, not part of this slice.

### 4. Outbound only. Nothing new faces the internet.

Bot updates arrive by `getUpdates` long-polling from inside the API process. No
webhook route is added.

Rejected: a webhook at `POST /api/v1/telegram/webhook`. It would be a new
public, unauthenticated route whose only guard is a shared secret header, in a
codebase whose rule is that a route without its guard has no second line of
defence. It would also need a CSRF exemption, its own rate limit, its own oracle
review, and re-registration whenever the hostname changes — and the hostname
changing is a live possibility here.

### 5. No new port abstraction over email and Telegram.

`Mailer` is untouched. `AuthService` and `SignupService` are untouched. A
`Notifier` port spanning both channels would require editing the four send sites
inside the two services carrying the most careful security reasoning in the
repository, and would need a `Destination` union with a fail-closed switch.

`CLAUDE.md` is explicit that a port arrives when a second implementation and a
second caller genuinely exist. Extracting that abstraction later, from two
working adapters, is cheaper and better-informed than designing it now.

**Where the chat binding is written, since it is not obvious.**
`SignupService.Complete` does **not** create the user itself — it calls
`SignupRepository.Provision`, and the transaction lives in the adapter at
`signup_repo.go:97`: `ConsumeSignup`, `CreateHousehold`, `CreateUser`,
`CreateMembership`, all under one `tx`.

So the `telegram_accounts` insert is a fifth statement inside that existing
transaction, and `ConsumeSignup` returns `telegram_chat_id` alongside the email
it already returns. That is deliberate reuse of the reasoning already written
at `signup_repo.go:112`: the verified value reaches the user row from the row
being claimed, so no caller can substitute a different one. A chat id passed in
by a caller would be exactly the substitution that comment refuses.

The consequence is better than expected: **`SignupService` gains no new
dependency and no new branch.** It hands `Provision` a signup id, and the row
decides which channel it belongs to. `AuthService` is untouched, `SignupService`
keeps its `SignupDeps` unchanged, and all four `Mailer` send sites are
untouched.

The one usecase-level change is a read: `SignupPreview` gains a channel field so
the create-household screen knows whether to render the read-only email box it
renders today at `SignUpCompleteScreen.tsx:241`. A Telegram sign-up has no
address to show there, and shipping an empty read-only input would be the
"looks automatic but is not" shape this project has refused twice before.

## Architecture

### New adapter

```
api/internal/adapter/telegram/
  client.go   outbound sendMessage; implements usecase.TelegramSender
  poller.go   getUpdates loop: offset, backoff, ctx cancellation, recover
  update.go   parse an Update into {chatID, startPayload}; switch with a
              default that ignores everything else
```

One file, one job. `client.go` never reads the environment; `main.go` hands it
config values, exactly as `mail.NewSMTPMailer` is constructed today.

### New ports in `usecase/ports.go`

```go
// TelegramSender delivers a plain-text message to one Telegram chat. Same
// shape and justification as Mailer: the usecase layer must not hold an
// HTTP client, and the service must be testable against a double.
type TelegramSender interface {
    SendMessage(ctx context.Context, chatID int64, text string) error
}

// TelegramLinkRepository stores pending deep-link nonces, hashed.
type TelegramLinkRepository interface {
    Create(ctx context.Context, nonceHash []byte, expiresAt time.Time) error
    // Consume stamps the row consumed and records which chat redeemed it, in
    // one statement. The chat is unknown when the nonce is minted -- the
    // browser has not met Telegram yet -- so redemption is the only moment
    // the two can be joined, and CountLinksSince depends on it happening here.
    Consume(ctx context.Context, nonceHash []byte, chatID int64) error // ErrNotFound if absent, expired or already consumed
    CountLinksSince(ctx context.Context, chatID int64, since time.Time) (int, error)
}

// TelegramAccountRepository binds a Telegram chat to a Hearth user.
type TelegramAccountRepository interface {
    ByChatID(ctx context.Context, chatID int64) (userID string, err error)
}
```

Narrow ports, per the nine-small-repositories rule. `CountLinksSince` sits on the
**link** repository, not the account repository: the per-chat limit has to bind
chats that have no account yet, and a stranger repeating `/start` has no user
row to count against.

`SignupRepository` gains exactly one method, beside the existing `Create` and
`CreateConsumed`:

```go
// CreateForTelegram writes a signup row whose channel is a Telegram chat
// rather than an email address. The signups_have_exactly_one_channel
// constraint means this and Create are mutually exclusive per row.
CreateForTelegram(ctx context.Context, chatID int64, tokenHash []byte, expiresAt time.Time) error
```

`SignupDetails` gains `TelegramChatID *int64` so `Preview` can report the
channel, and `SignupPreview` gains `Channel string` — `"email"` or
`"telegram"` — parsed with a `default` that refuses, per the fail-closed rule.

### New usecase

`TelegramAuthService`, beside `AuthService`. It **mints no token types of its
own** — it calls the existing `MagicLinkRepository` and `SignupRepository`.

This constraint is load-bearing. A parallel token table would mean two expiry
rules, two rate limits and two oracle analyses drifting apart, and the second
one would be the one nobody reviews.

### Wiring

`main.go` constructs the client and starts the poller in the `go func()` pattern
already present at `api/cmd/api/main.go:248`, cancelled by the
`signal.NotifyContext` at `:58` and drained alongside the HTTP server's
`Shutdown` at `:278`.

## Data model

New migration, `api/migrations/00011_telegram.sql`.

```sql
CREATE TABLE telegram_link_requests (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    nonce_hash  bytea       NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    -- Both NULL until the nonce is redeemed, then both set in one statement.
    -- chat_id is what the per-chat rate limit counts, so it cannot be a
    -- separate write that might not happen.
    consumed_at timestamptz,
    chat_id     bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT consumed_rows_name_their_chat
        CHECK ((consumed_at IS NULL) = (chat_id IS NULL))
);

CREATE INDEX telegram_link_requests_chat_consumed_idx
    ON telegram_link_requests (chat_id, consumed_at DESC);

CREATE TABLE telegram_accounts (
    id        uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   uuid        NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    chat_id   bigint      NOT NULL UNIQUE,
    linked_at timestamptz NOT NULL DEFAULT now()
);
```

The nonce is stored **hashed**, never raw, matching `invites.token_hash` and the
magic-link convention. Both unique constraints are required: one Telegram
account per user, and one user per Telegram account.

### The signups table has to change

`signups.email` is `citext NOT NULL` (`api/migrations/00003_signups.sql:17`). A
Telegram sign-up has no email at all. The same migration therefore does:

```sql
ALTER TABLE signups ALTER COLUMN email DROP NOT NULL;
ALTER TABLE signups ADD COLUMN telegram_chat_id bigint;
ALTER TABLE signups ADD CONSTRAINT signups_have_exactly_one_channel
    CHECK ((email IS NULL) <> (telegram_chat_id IS NULL));
```

The `CHECK` is the fail-closed half. A signup row carrying both channels, or
neither, is refused by the database rather than reasoned about in Go.

**The backfill is a no-op, and that is worth confirming rather than assuming.**
`ADD CONSTRAINT ... CHECK` validates against existing rows at migration time,
and this is the first migration here to constrain a table that already holds
production data. Every existing `signups` row has a non-NULL `email` and, after
the `ADD COLUMN`, a NULL `telegram_chat_id`, so
`(email IS NULL) <> (telegram_chat_id IS NULL)` is true for all of them. The
migration therefore needs no data step — but the plan should still run it
against a restored production dump before it runs against production.

`users.email` needs no change. It is already nullable
(`api/migrations/00002_identity.sql:20`), and `user_repo.go:70` already
documents a real user with no email of their own — a limited member, typically a
child.

### Completing a Telegram sign-up is one transaction

`SignupRepo.Provision` (`signup_repo.go:97`) already runs `ConsumeSignup`,
`CreateHousehold`, `CreateUser` and `CreateMembership` under a single `tx`. The
`telegram_accounts` insert becomes a fifth statement in that same transaction,
and `ConsumeSignup` is widened to return `telegram_chat_id` beside the email it
already returns.

A partial write here produces an account whose owner can never sign in again —
the user exists, the chat is not bound, and the sign-up token is spent. That is
precisely the failure the `guarding-partial-writes` skill exists for, and the
reason the binding cannot be a second statement after the commit.

## Configuration

Two new values in `internal/config`:

| Variable | Meaning |
|---|---|
| `TELEGRAM_BOT_TOKEN` | The BotFather token. Never logged |
| `TELEGRAM_BOT_USERNAME` | Used to build `https://t.me/<username>?start=<nonce>` |

**Both set, or both empty.** `config.Load` enforces this the same way it already
enforces it for `SMTP_USERNAME`/`SMTP_PASSWORD` at `config.go:86`, and for the
same reason: a half-configured channel misbehaves silently.

Both empty means the feature is off — the route answers `404` and the poller
never starts. Development and the current production deployment keep booting
unchanged, and `adminctl`, which runs `config.Load` before every subcommand, is
unaffected.

The username is configured rather than fetched with `getMe` at startup: no
cleverness, and no startup dependency on Telegram being reachable.

## Flows

### New route

```
POST /api/v1/auth/telegram/start
```

Public — no session exists yet. No request body. Wrapped in the existing
`newIPRateLimiter`, the same per-IP limiter already guarding `POST /auth/sign-up`
in `router.go`.

Answers `200` with a JSON body, because every 2xx except 204 must:

```json
{ "url": "https://t.me/HearthBot?start=<nonce>", "expiresAt": "2026-09-01T10:10:00Z" }
```

When the feature is off it answers `404`.

### Flow 1 — a returning member signs in

1. Browser posts to `/api/v1/auth/telegram/start`.
2. Server calls `Tokens.NewToken()` for a raw nonce and its hash, inserts a
   `telegram_link_requests` row with `expires_at = now + 10 minutes`, and returns
   the `t.me` URL.
3. The user taps it. Telegram delivers `/start <nonce>` to the bot.
4. The poller parses the update and calls
   `TelegramAuthService.HandleStart(ctx, chatID, payload)`.
5. `Links.Consume(HashToken(payload), chatID)` — a single atomic consume that
   also records the redeeming chat, the same shape as `MagicLinks.Consume`.
   Absent, expired or already consumed returns `domain.ErrNotFound`, and the bot
   replies "That sign-in link has expired. Start again from the app." The flow
   stops there.
6. `Links.CountLinksSince(chatID, now - 1h)` — at 3 or more, the bot replies
   with the same expiry message and the flow stops. Checked **after** the
   consume, so a rate-limited attempt still spends its nonce and cannot be
   retried with the same link.
7. `Accounts.ByChatID(chatID)`:
   - **Found.** `MagicLinks.Create(userID, hash, now + magicLinkTTL)` — 15
     minutes, unchanged. The bot sends `{AppBaseURL}/sign-in/magic?token=<raw>`.
   - **Not found.** `Signups.CreateForTelegram(chatID, hash, now + SignupTTL)` —
     24 hours, unchanged. The bot sends `{AppBaseURL}/sign-up/<raw>`.
8. The user taps that link inside Telegram. The browser opens, and the
   **existing** `handleConsumeMagicLink` or `handleSignUpPreview` runs.

No new session-issuing code exists anywhere in this design.

### Flow 2 — a stranger creates a household

Identical through step 7b. The existing sign-up screens then collect household
name, display name, password and currencies. `SignupService.Complete` finds
`telegram_chat_id` set and `email` NULL, and writes the user (with a NULL
email), the membership, and the `telegram_accounts` row in one transaction.

From that point the person signs in exactly as in Flow 1, because their chat is
now bound.

## Security analysis

**No enumeration oracle is introduced.** `/auth/telegram/start` accepts no
identifier at all. There is nothing to probe — strictly better than
`/auth/magic-link`, which takes an address and needs `AuthService.decoy()` and
timing equalisation to stay quiet. `docs/LEARNING.md:1604` ("Enumeration oracles
are rarely in the error code") is the reason this paragraph exists.

**The bot's two replies differ by branch, and that is safe here.** A sign-in link
and a sign-up link are distinguishable, which is normally exactly the tell an
oracle analysis hunts for. It is safe in this flow because the only recipient is
the owner of the chat, who already knows whether they have an account. No third
party observes the difference. This is written down because the next reader will
flag it and should find the reasoning already here.

**No brute-force surface is added.** The nonce is a 32-byte `TokenGenerator`
token, hashed at rest, single-use, with a 10-minute TTL — the same class of
secret as a magic link. There is no numeric code anywhere in this design, which
is the main reason it was preferred over an OTP.

**Rate limits, two of them.** Per-IP on `/auth/telegram/start`, reusing
`newIPRateLimiter`. Per-chat, at most 3 issued links per hour, mirroring
`magicLinkPerHourLimit` at `auth.go:320`. Without the second, a chat spamming
`/start` is a free path to burn magic-link rows.

A rate-limited chat gets the **same** reply as an expired nonce, word for word.
That is what "silent" means here: being over the limit is not distinguishable
from being late, so the limit cannot be probed for. It is also why the limit is
checked after the consume rather than before — a refused attempt still spends
its nonce, so the same link cannot be retried until the hour rolls over.

**Logging.** `chat_id` is logged only through `hashPrefix`, exactly as
`email_hash` is today. The bot token is never logged, in any branch, including
error paths.

**Account takeover by link forwarding is closed by decision 2**, not mitigated.
Because the session lands on the device that taps, forwarding the link gives the
forwarder nothing.

## Failure modes

**Exactly one process may call `getUpdates`.** Telegram hands each update to one
caller. A second replica would silently steal updates, and the symptom would be
"sign-in works about half the time". True on one box today; it belongs in the
poller's doc comment because it is invisible until it bites.

**The offset is kept in memory.** After a restart Telegram redelivers
unconfirmed updates, so a `/start` can be processed twice. This is safe
*because* the nonce was already consumed, so the second pass takes the expired
branch and the bot says so. Stated deliberately rather than left to luck.

**Network errors back off.** Capped exponential backoff, logged at warn. The
loop never exits: the API must keep serving HTTP while Telegram is unreachable.

**Panics are recovered.** The poller is a bare goroutine and chi's
`middleware.Recoverer` does not cover it, so an unrecovered panic would take
down the whole process and every unrelated in-flight request. The pattern is
already established in `sendMagicLinkAsync` at `auth.go:490`.

**Telegram being down degrades partially.** Telegram sign-in stops; the email
path is unaffected; sessions already issued are unaffected.

## Testing

- `TelegramAuthService` against in-memory doubles, following the existing
  `usecase/testdouble_test.go` pattern. Every branch of `HandleStart`: unknown
  nonce, expired nonce, consumed nonce, known chat, unknown chat, rate-limited
  chat.
- Table test on update parsing, including the `default` branch that ignores
  every update that is not a `/start`.
- Poller loop against an `httptest` server standing in for `api.telegram.org`:
  offset advance, backoff on error, and redelivery after a simulated restart.
- Repository tests on testcontainers Postgres, including the new `CHECK`
  refusing a signup row with both channels **and** one with neither.
- A transactional test that forces the `telegram_accounts` insert to fail and
  asserts no orphan user row survives.
- At least one test mutation-checked, per `proving-tests-can-fail`.
- `make lint` must stay green, including `make lint-arch`. The new
  `adapter/telegram` package imports `internal/usecase` for the port types and
  the poller calls a usecase service — the same shape `adapter/mail` and
  `adapter/postgres` already have, so the arch lint is satisfied by
  construction. Nothing in `internal/domain` or `internal/usecase` may import
  the Telegram adapter or any Telegram library type.
- A browser walk against the running app and the real bot before this is called
  done, per `verifying-in-the-real-environment` and the product owner's standing
  request of 2026-07-30.

## Documentation shipped with the work

- `docs/SYSTEM_DESIGN.md` — new adapter, two new tables, altered `signups`, a
  new request flow. Use the `maintaining-system-design` skill.
- `docs/FEATURE_TRACKER.md` — rows for Telegram sign-up and Telegram sign-in.
- `docs/INFRASTRUCTURE.md` — Telegram is a new external dependency and needs a
  row: what it costs ($0), where the bot token lives, and what breaks without it.
- `docs/adr/0003-mail-stays-on-the-box.md` — **amend**. Its exit condition, "the
  day a person who is not Andreas or Christine needs to receive an email", stops
  being the trigger once Telegram carries strangers.
- `docs/adr/0004-telegram-as-a-second-delivery-channel.md` — **new**. Why
  Telegram, why link-back rather than cross-device, and why no `Notifier` port.
- `docs/LEARNING.md` — after the work, one entry per defect worth remembering.

## Out of scope

- **Invites over Telegram.** A shareable `t.me/…?start=inv_<token>` link is the
  natural follow-up and is deliberately not in this slice.
- **Cross-device sign-in.** Requires a typed confirmation code binding both ends;
  see decision 2.
- **A `Notifier` port over email and Telegram.** See decision 5. Extract it when
  a third channel arrives.
- **Removing Mailpit from production.** `SMTP_ADDR` is required by
  `config.Load`, and email flows still serve every user who has an address.
- **The four pre-existing dead-code items** found while scoping this
  (`SMTPMailer.baseURL`, `CategoryService.List`, `TokenState.String`, and the
  unwired fail-closed parser `domain.ParseCategoryKind`). They are unrelated to
  this change and belong in their own small change, so this diff stays reviewable.
