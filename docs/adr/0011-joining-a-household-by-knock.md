# 11. Joining a household by knock

**Status:** Accepted — 2026-09-20.

## Context

[ADR 3](0003-mail-stays-on-the-box.md) records why production mail cannot
leave the box, and its 2026-09-01 amendment names the consequence this ADR
closes: **an invite to someone who is not on Telegram** was already the live
trigger for ADR 3's exit condition, because [ADR 4](0004-telegram-as-a-second-delivery-channel.md)
deliberately left invites on email while it fixed sign-up and sign-in. That
was the right call for its own slice — invites needed the delivery channel
Telegram now provides before they could move — but it left a self-serve
household unable to bring in a second owner at all: the invite link is
mailed to Mailpit on the box, nobody but the operator can read it, and
[Agreements stays locked until a household has a second owner](../../.claude/prds/partner-invite-lobby.prd.md),
so every self-serve household loses a whole Marriage feature on day one.

`.claude/prds/partner-invite-lobby.prd.md` frames the fix as a **knock**: the
owner hands their partner a one-time Telegram link, the partner's tap is
recorded, and the owner — not the chat — decides whether that tap becomes a
member. This ADR is the decision record the PRD's own header promised,
written once milestone 2 shipped rather than at design time, because the
shape below only became load-bearing once real code and a real security
review had tested it.

Two documents this ADR sits directly beside:

- [ADR 10](0010-binding-a-chat-needs-a-confirm.md), whose whole reasoning
  this one extends: a Telegram chat proves *a* device tapped a link, never
  *which* person is really holding it, so the decision that grants access
  has to happen somewhere a bare tap cannot reach.
- The security review dated 2026-09-19
  (`docs/reviews/2026-09-19-security-review.md`), whose finding 2 — a bound
  **group** chat made every member of the room the account holder, and a
  working sign-in link would have been posted into it — this milestone
  fixes as a precondition. The knock is meaningless if "the chat that
  tapped" can mean more than one person.

## Decision

**A tap records a knock and nothing else. A signed-in owner decides. The
four digits are compared by eye and accepted by no endpoint. One knock per
link.**

Concretely:

1. `POST /household/members/invite` with `channel: "telegram"` returns a
   one-time `t.me/<bot>?start=inv_<token>` link, shown once, never
   recoverable a second time (the same rule an API token's own secret
   follows).
2. `/start inv_<token>` — gated by the private-chats-only fix below — runs
   one guarded `UPDATE` (`RecordKnock`) that succeeds only for the first tap
   on a live, unexpired, unaccepted, not-yet-knocked Telegram invite. Every
   other case — unknown token, wrong channel, expired, accepted, already
   knocked — answers the identical bland dead-link reply, because the chat
   must not be able to tell those apart by probing (the same reasoning
   `telegramDeadLinkMessage` already applies). **This is the whole of "a
   tap records and nothing else":** the statement that records the knock
   creates no session, no user, no membership, and grants nothing.
3. The four digits (`PairingCodes.NewCode`, `crypto/rand`, `adapter/crypto`)
   are shown in the partner's chat and, via `GET /household/invites`'s
   `knock` field, on the owner's own screen. The owner compares them by eye
   — evidence that the chat which knocked is the one in front of the
   partner, the same role `chat_username` plays in ADR 10's confirm screen.
4. The owner clicks **Let in** (`POST /household/invites/{id}/admit`) or
   **Not them** (`POST /household/invites/{id}/link`, which also serves "get
   a new link"). Let in is one transaction — user, membership, the
   `telegram_accounts` binding and the acceptance stamp — with the sign-in
   link sent to the new member's chat only *after* the commit, reporting
   `signInSent: false` rather than hiding a delivery failure (the same
   silent-partial-success refusal `docs/LEARNING.md` pattern 5 names). Not
   them clears the knock and the token together in one statement and tells
   the dead chat its link no longer works.

### Why the code is not typed in anywhere

[ADR 4](0004-telegram-as-a-second-delivery-channel.md) rejected a numeric
one-time code as a sign-in mechanism outright: *"A short code is guessable,
so it needs its own attempt limiting, its own lockout and its own timing
analysis — a second security surface to review beside the one that already
exists."* The four digits here look like the same shape and are not: **no
endpoint accepts them.** They grant nothing on their own, so there is
nothing to guess, brute-force or rate-limit — the code is read, not
redeemed. `TestTheAdmitRequestHasNoCodeField` pins that the admit route's
request body carries no code field at all, which is the mutation-checked
proof that this stays true.

**If the code is ever typed into a form, ADR 4's rejection applies again in
full.** The instant a four-digit value becomes something an endpoint
compares against a stored value to grant access, it inherits every problem
ADR 4 named — attempt limiting, lockout, timing — that this design was built
specifically to avoid. Anyone tempted to add a "type the code here" screen
(cross-device, a partner without the owner's phone in the room, or any other
convenience) is proposing a different, weaker security property, not a UI
tweak, and should re-read ADR 4's table before doing it.

The code exists at all because Telegram `@username`s are optional and this
product deliberately never shows a first name in the confirm-adjacent
screens (`adapter/telegram/update.go`, `senderName`): without it, an owner
with no `@username` to compare would be asking "did *someone* tap my link,
yes or no?" — a question with only one sane answer, the same objection ADR
10 raises against an unnamed confirm.

### Why the deciding click sits where a stolen link cannot reach

This is ADR 10's reasoning, unchanged, applied to a second flow. A knock is
recorded by whoever's chat redeemed the link — forwarded, screenshotted,
read over someone's shoulder, it does not matter which. **Let in and Not
them both require a signed-in browser session** (`requireCookieSession`,
stacked with `requireOwner` and `requireCSRF` on `POST
/household/members/invite`, `DELETE /household/invites/{id}`, `POST
/household/invites/{id}/link` and `POST /household/invites/{id}/admit`) —
never a personal API token. A leaked invite link therefore costs, at worst,
one confused knock and a fresh link; a leaked **API token** must not be able
to mint a permanent co-owner by clicking Let in on the token holder's
behalf, which is exactly the property [ADR 7](0007-personal-api-tokens.md)'s
rule 2 already protects for token minting and revocation and for the
Telegram link/confirm group. `hearthctl` is unaffected — it signs in with a
cookie.

### Why the bot must answer only a private chat first

The knock is worthless if "the chat that tapped" can mean a room full of
people rather than one partner. Security review finding 2 found exactly
that hole, unrelated to invites at the time it was found: a bound **group**
chat made every member of the room the account holder for `/spend`,
`/balance` and a pending transaction's confirm, and a Telegram sign-in
request posted a **working magic link into the group**. `Message.Chat` now
carries Telegram's own `Type`, and `isPrivateChatWithItsOwner`
(`adapter/telegram/update.go`) gates both `ParseStart` and `ParseCommand`
with a `switch` whose `default` refuses — fail closed on a value this build
did not construct, `CLAUDE.md`'s own rule — plus `From.ID == Chat.ID`, the
arithmetic that only holds in a genuine one-to-one chat. This had to land
*before* the knock could mean anything: a knock recorded from a group would
carry the same ambiguity ADR 10 already refuses to accept for a binding.

## Consequences

- **Email invites are now flag-gated and off by default.** `email_invites`
  (`domain.FlagEmailInvites`) defaults to `false`, enforced both at the HTTP
  edge (`POST /household/members/invite` answers `409
  EMAIL_INVITES_DISABLED` for an email-channel request while it is off) and
  in the invite modal, which never offers the option. This follows
  `notification_delivery`'s own reasoning: a flag that is on for something
  that cannot happen — a stranger's mailed link landing only in the
  operator's own Mailpit — is a lie. `docs/adr/0003-mail-stays-on-the-box.md`
  carries this as an amendment to its own consequences, pointing back here.
- **The public web invite form now refuses a Telegram token exactly as it
  refuses an unknown one**, and the ordering is load-bearing, not
  incidental: `Preview` and `Accept` both check `details.Channel !=
  domain.ChannelEmail` **before** the three-way liveness switch
  (`checkInviteLive`), so an **expired** Telegram token still answers
  `domain.ErrNotFound` rather than `ErrInviteExpired`. Checking the channel
  after the liveness switch would have told a caller holding a dead
  Telegram link "this token was real, it's just too late" — precisely the
  leak this ordering exists to close (`docs/LEARNING.md`). A public form
  must not serve a channel it does not own: a link-holder who reached this
  route with a Telegram token could otherwise skip the owner's Let in
  entirely.
- **The operator's mail viewer now carries fewer live invite links than
  before this milestone**, because an invite created on the default
  configuration is a Telegram invite with no email at all. This narrows —
  but does not close — the security review's finding 3 (the admin mail
  viewer shows working magic links, letting an admin sign in as any
  customer silently): fewer invite links pass through Mailpit, but every
  magic link, sign-up link, and any email invite an operator turns
  `email_invites` on for, still does. Finding 3's own fix (hiding tokens in
  the viewer) is not part of this milestone.
- **The knock adds no new rate-limit surface.** It consumes the link and
  creates nothing — no session, no magic link, no sign-up row — so the
  existing per-chat hourly limit on those rows is not involved. A chat
  hammering `/start inv_…` after its one knock gets the same dead-link reply
  every time and writes nothing further.
- **A leaked link costs one new link, never a takeover**, mirroring ADR 10's
  own headline consequence: redeeming a Telegram invite link only ever
  produces a knock waiting on a decision behind someone else's session. The
  confirm-equivalent step — Let in — is behind that other session's cookie,
  not the chat's own next message.
- **One knock per link is enforced in the database, not only in Go.**
  `RecordKnock`'s guarded `UPDATE` includes `knocked_at IS NULL` in its
  `WHERE` clause, so two taps at the same instant leave exactly one winner
  and the other gets the ordinary dead-link reply — the same "guard in the
  SQL, not a service `if`" discipline `docs/LEARNING.md` and `CLAUDE.md`
  both already ask for on this kind of race.
- **A narrow, accepted race remains in "get a new link".**
  `ReplaceInviteToken`'s `RETURNING` clause reads the chat that had knocked
  through a subselect evaluated against the statement's own snapshot before
  its row lock is taken; a knock landing in that exact window is cleared by
  the same statement without the knocking chat ever being told. No security
  consequence — the knock is simply lost, not honoured wrongly — and it is
  recorded as a defect in `docs/LEARNING.md` rather than fixed here.
- **`invites_knock_chat_is_a_person`** (`CHECK (knock_chat_id IS NULL OR
  knock_chat_id > 0)`, migration `00021`) is the same private-chat gate as
  the application-layer check above, a second time, in the one place a
  future caller cannot forget it. It was free to add because no row in
  `invites` has ever had this column. The equivalent `CHECK` on
  `telegram_accounts` and `telegram_link_requests` — the two tables that
  actually hold group-chat-shaped rows if the application-layer gate is
  ever bypassed — is **not** added this milestone: both hold real
  production rows, and adding a `CHECK` to a populated table needs a
  restored-dump run first (`00011`'s own rule), plus confirming against the
  Bot API that a private chat id is always positive. That gap stays a live,
  un-closed item from the security review, not silently dropped.

## See also

- [ADR 3 — mail stays on the box](0003-mail-stays-on-the-box.md), amended by
  this ADR's flag-gating of email invites
- [ADR 4 — Telegram as a second delivery channel](0004-telegram-as-a-second-delivery-channel.md),
  whose "Invites over Telegram" out-of-scope item this ADR closes
- [ADR 7 — personal API tokens](0007-personal-api-tokens.md), rule 2, which
  the browser-session-only guard on Let in and Not them extends
- [ADR 10 — binding a chat needs a confirm](0010-binding-a-chat-needs-a-confirm.md),
  whose reasoning this ADR applies to a second flow
- `docs/reviews/2026-09-19-security-review.md`, finding 2 (fixed here) and
  finding 3 (narrowed, not closed)
- `.claude/prds/partner-invite-lobby.prd.md` and
  `docs/superpowers/specs/2026-09-19-hearth-partner-invite-lobby-design.md`,
  the spec this was built from
- `docs/SYSTEM_DESIGN.md` §5 (the flow), §6 (the `invites` table)
