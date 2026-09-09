# 10. Binding a Telegram chat needs a confirm inside the minting session

**Status:** Accepted — 2026-09-09.

## Context

`telegram_accounts` rows were written in exactly one place: inside
`SignupRepository.Provision`'s transaction, when a stranger's `/start`
creates a brand-new household from a chat. That is the *only* door a binding
could come through, and it left every account born the *other* way — signed
up by email — with no way to connect a chat at all:
`TelegramAuthService.HandleStart` resolves every `/start` through
`Accounts.ByChatID`, and an unbound chat always falls through to
`sendSignUp`. The person asking to connect their phone was offered a second
household instead. Every capability built on the binding since — `/spend`,
`/balance`, `/recent`, the free-text intents, the daily digest — inherited
the same hole: reachable only by an account born in a chat
(`docs/LEARNING.md` §15, eighth instance).

Fixing this means writing a `telegram_accounts` row from a second call site:
a signed-in browser session, in Settings, naming the chat it wants to
connect.

## Decision

**A chat is bound only by a confirm that happens inside the session that
minted the link, never by the chat's own `/start`.** The flow is two round
trips: Settings mints a link nonce carrying the member's `user_id`
(`POST /auth/telegram/link`) → the deep link opens
`t.me/<bot>?start=<nonce>` → the bot's `/start` consumes the nonce and
records which chat redeemed it, but **writes nothing to
`telegram_accounts`** (`TelegramAuthService.handleLinkStart`,
`telegram_auth.go`) → the browser polls `GET /auth/telegram/link/{id}`, sees
the redeeming chat's `@username`, and only *that* signed-in session's own
click, `POST /auth/telegram/link/{id}/confirm`, writes the binding
(`TelegramLinkService.Confirm`, `telegram_link.go`).

Two shapes were considered:

1. **One-phase bind.** The nonce carries the user; `/start` binds
   immediately, the same shape the existing sign-in nonce already uses for
   *reading* a token. Rejected: a link nonce is a bearer credential for ten
   minutes, and anything that can read it — a forwarded message, a shared
   screen, a synced clipboard — can redeem it. A one-phase bind turns
   redemption into account takeover: **whoever's chat redeems the link gets
   it bound to the victim's account, and from that moment every `/start`
   that chat sends mints the victim a fresh magic link and signs the
   redeemer in as them.** That is strictly worse than the sign-in nonce this
   design sits beside, which a stranger redeeming can only ever turn into a
   sign-in for *their own* chat or a signup for a household of their own —
   never someone else's account.
2. **Confirm inside the minting session.** Chosen. The deciding click moves
   to a session that has already authenticated over HTTP with a cookie and
   a CSRF token — exactly where a stolen ten-minute deep link cannot reach,
   because it has neither.

## Consequences

- **Two round trips and a polling panel**, where a single click would have
  read simpler. `TelegramPanel.tsx` mints, opens the deep link, and polls
  `GET .../link/{id}` every three seconds while the status is `waiting` or
  `pending` (`telegramPollInterval`, `copy.ts`) — UI weight that exists
  entirely to buy the second phase, not for its own sake.
- **A leaked deep link connects nobody.** Redeeming it only ever produces
  the *pending* state on a row belonging to someone else's session; the
  confirm button that would complete it is behind that other session's
  cookie. This is the property decision 1 above exists to keep.
- **`telegram_accounts.chat_username` and `telegram_link_requests.chat_username`
  exist so the confirm can be verified by the person making it**
  (migration `00018_telegram_link_user.sql`). Without a name attached, the
  confirm screen would be asking "some chat opened your link, yes or no?" —
  a question with only one sane answer, which makes it not a confirmation at
  all. With it, a member who sees `@stranger123` when they are `@andreas`
  has the one piece of evidence the second phase exists to give them. It is
  read by the panel and by nothing server-side: `chat_id`, not the display
  name, is what every check in `Confirm` and `handleLinkStart` actually
  compares.
- **The chat gets a bland answer; the minting session gets the real one.**
  Mirroring the sign-in flow's single `telegramDeadLinkMessage`
  (`docs/SYSTEM_DESIGN.md` §5) and the split ADR 8 already draws between
  what a channel may learn and what an authenticated caller may learn,
  `handleLinkStart` gives the chat exactly three possible sentences and
  nothing more specific than that: "Go back to Hearth and confirm this chat
  to finish connecting it." for the clean case; "This chat is already
  connected to your Hearth account." when it is already bound to the same
  user, which is safe to say because that chat's owner already knows it; and
  the bland `telegramLinkRefusedMessage` — "Could not connect this chat.
  Open Hearth to see why." — for every other case (bound to someone else, or
  the nonce's own user already has a different chat), never distinguishing
  *why*, because each reason (the account exists, already has a chat,
  belongs to someone else) is a probe. The browser that minted the nonce has
  already proved who it is, so `Status` and `Confirm` hand it the named
  `domain` error (`ErrTelegramChatTaken`, `ErrTelegramAlreadyLinked`, …)
  instead.
- **The link and unlink routes sit behind a browser session, never an API
  token** (`requireSession` + `requireCSRF` + `requireCookieSession`,
  `router.go`) — the same rule minting a token already follows
  ([ADR 7](0007-personal-api-tokens.md)): a leaked token must not be able to
  make itself permanent. Binding a chat is exactly that kind of permanence,
  so a token is not enough to grant it.
- **Anyone tempted to collapse this into one click is changing a security
  property, not a UX detail.** The two-phase shape looks redundant to
  anyone reading only the happy path — mint, tap, done, why the extra
  screen? — and the answer is entirely in the takeover scenario above, which
  the happy path never exercises.
  `TestHandleStartWithALinkNonceLeavesTheBindingUnwritten`
  (`telegram_auth_test.go`) is the mutation-checked proof: its own comment
  says so — "a one-phase bind would make a leaked deep link an account
  takeover" — and it is the test that must still be there, and still pass,
  the day someone proposes writing the binding straight from `/start`.
