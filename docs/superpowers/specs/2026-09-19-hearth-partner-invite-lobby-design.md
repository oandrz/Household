# Hearth partner invite lobby — design

**Date:** 2026-09-19. Built from
[`.claude/prds/partner-invite-lobby.prd.md`](../../../.claude/prds/partner-invite-lobby.prd.md)
(milestones 1 and 2; milestone 3, the access list, gets its own small spec).
Extends [ADR 4](../../adr/0004-telegram-as-a-second-delivery-channel.md)
(closes its "Invites over Telegram" out-of-scope item) and follows the shape of
[ADR 10](../../adr/0010-binding-a-chat-needs-a-confirm.md) (a Telegram tap
records, a signed-in browser decides). To be recorded as **ADR 11, "Joining a
household by knock"**, when milestone 2 lands.

## What

An owner invites their partner and gets a one-time Telegram link to hand over:
copy it, show it as a QR code, or share it to Telegram. The partner taps it.
The bot records a **knock** and shows the partner a four-digit code. The
owner's browser shows the same code and asks "Does their phone show 4812?".
The owner clicks **Let in** or **Not them**. Let in creates the member, and the
bot sends the partner an ordinary sign-in link.

Before any of that, milestone 1 makes pending invites **visible**. Today an
invite is written to the `invites` table and nothing reads it back. The Invite
modal just closes, Settings shows no trace, and in production the email lands
in the operator's Mailpit
([ADR 3](../../adr/0003-mail-stays-on-the-box.md)), so the partner never
receives it.

Why it matters: **Agreements stays locked until a household has a second
owner**, so without this every self-serve household loses a whole Marriage
feature.

## Decisions

1. **Extend the `invites` table; no new table.** An invite gains a `channel`
   (`email` or `telegram`), and a Telegram invite carries its knock on the same
   row. Two alternatives were rejected:
   - **Reusing `telegram_link_requests` with an `invite_id`.** Those nonces
     live ten minutes and an invite outlives them, so each invite would hold
     two tokens. Migration `00018` deliberately left the `inv_` payload prefix
     free so invites would route by payload rather than through that table.
   - **A separate `invite_knocks` table.** It only earns its place if several
     knocks per link must be shown. Decision 2 removes that need.

2. **One knock per link.** The first `/start` with a link records the knock;
   every later tap gets the standard dead-link reply. "Not them" kills the link
   and issues a new one. A leaked link therefore costs one new link, never a
   takeover. This is the same stance ADR 10 takes.

3. **The matching code is display-only. No endpoint ever accepts it.** It is
   drawn at knock time from a secure random source, shown in the partner's chat
   and on the owner's screen, and compared by eye. It grants nothing, so there
   is nothing to guess or brute-force. **If it were ever typed into a form,
   ADR 4's rejection of guessable one-time codes would apply.** A test pins
   that the admit request has no code field. It exists because Telegram
   @usernames are optional, and the codebase deliberately never shows a
   first name (`adapter/telegram/update.go`, `senderName`): anyone can call
   themselves "Jane".

4. **Let in happens only in the owner's signed-in browser session**, never
   from the chat. Same reason as ADR 10: the deciding click must sit where a
   stolen link cannot reach.

5. **Let in is one repository method in one transaction.** It creates the user
   (no email, no password; display name from the invite), creates the
   membership (role and capabilities from the invite), writes the
   `telegram_accounts` row, and stamps the invite accepted. Either all four
   happen or none do. This is the rule `InviteRepository.Accept` and
   `SignupRepository.Provision` already state in `ports.go`: never build this
   from separate calls, or a failure between them leaves an orphaned user.

6. **The sign-in link is sent after the commit, and a failed send is named,
   not hidden** (`docs/LEARNING.md` pattern 5). The response carries
   `signInSent`. When it is `false`, the waiting card says "Let in. If no
   message arrived, ask them to send /start to the bot." No new recovery code
   is needed: the chat is now linked, so any `/start` already sends a fresh
   sign-in link (`TelegramAuthService.HandleStart` → `sendSignIn`).

7. **The public web accept route refuses Telegram invites.**
   `GET /invites/{token}` and `POST /invites/{token}/accept` treat a
   Telegram-channel invite exactly like an unknown token. Without this, anyone
   holding the raw token could accept through the web form with a password,
   skipping the owner's Let in entirely. A test proves it, and it is one of
   the named mutation checks.

8. **Telegram invite links live 24 hours.** Email invites keep seven days.
   Getting a new link is one click, so a short life costs almost nothing, and
   a link forgotten in a chat history dies the next day.

9. **Owner invites no longer need an email.** `ErrInviteRequiresEmail` guarded
   delivery, not identity: `users.email` is already nullable, and Telegram
   sign-up already creates owners with no address. The rule becomes: **an
   invite for someone who will sign in has exactly one channel.** Kid profiles
   (a limited member with no sign-in) keep today's path: created directly, no
   invite row.

10. **Email invites sit behind a new flag, `email_invites`, off by default.**
    This follows the precedent in `FlagNotificationDelivery`'s own comment: "a
    flag that is on for something that cannot happen is a lie". `/me` already
    sends flags to the frontend (`Features`), so the modal hides the email
    option. **The HTTP edge also refuses `channel: email` while the flag is
    off**; otherwise `hearthctl` or a crafted request could still create an
    invite that can never be delivered. The operator turns it on in `/admin`
    the day real email exists, with no code change. The flag ships with
    milestone 2, never with milestone 1, because hiding email before the
    Telegram link exists would leave no way to invite anyone.
    `adminctl create-invite` is an operator tool. It calls `InviteService`
    directly, not through the HTTP edge, so the flag doesn't apply to it. It
    still mails (to Mailpit) and also prints the URL it captured, which is how
    an operator hands an invite over today. It stays an email-channel invite.

11. **Telegram invites need the `telegram_sign_in` flag on and a bot
    configured.** If neither channel is available, the modal says "Inviting is
    unavailable on this install" instead of offering something that goes
    nowhere.

12. **Every route that creates or removes a way into the household needs a
    browser session** (`requireCookieSession`), not only owner and CSRF. That
    covers invite create, withdraw, new link and admit. A leaked API token
    must not be able to mint a co-owner; that is the reason behind ADR 7 rule
    2, even though the rule's text names only token and sign-out actions.
    **The same gap sits on `PATCH` and `DELETE /household/members/{id}`**, where
    a leaked owner token could demote or remove the other owner. Both gain the
    guard in milestone 2, with the same test. `hearthctl` is unaffected: it
    signs in with a cookie.

13. **Withdrawing deletes the row**, scoped by household. A "withdrawn" stamp
    would need every invite query (`ByTokenHash`, `LiveInviteForEmail`, the
    pending list, the operator's list) to remember one more condition. That is
    the "fixed one place, missed another" shape (`docs/LEARNING.md` pattern 1).
    A deleted row cannot be accepted by any query, current or future.

14. **The role is fixed when the invite is created.** Let in grants exactly
    what the invite says. Changing it afterwards is an ordinary member edit.

15. **A chat already linked to any Hearth account cannot knock.** The reply is
    "This Telegram account already belongs to a Hearth household." It tells the
    tapper only about their own chat. Let in checks again inside its
    transaction, because the chat may have signed up somewhere else between the
    knock and the click.

## Data

One migration, `00021_invite_channels.sql`, shipped with milestone 2.
Milestone 1 needs no schema change.

```sql
ALTER TABLE invites ALTER COLUMN email DROP NOT NULL;
ALTER TABLE invites ADD COLUMN channel text NOT NULL DEFAULT 'email'
    CHECK (channel IN ('email', 'telegram'));
-- An email invite has an address; a Telegram invite has none.
ALTER TABLE invites ADD CONSTRAINT invites_channel_matches_email
    CHECK ((channel = 'email') = (email IS NOT NULL));

ALTER TABLE invites ADD COLUMN knock_chat_id       bigint;
ALTER TABLE invites ADD COLUMN knock_chat_username text;   -- @username, NULL when Telegram sent none
ALTER TABLE invites ADD COLUMN knock_code          text;
ALTER TABLE invites ADD COLUMN knocked_at          timestamptz;
-- A knock is whole or absent, and only a Telegram invite can have one.
ALTER TABLE invites ADD CONSTRAINT invites_knock_is_whole
    CHECK ((knocked_at IS NULL) = (knock_chat_id IS NULL)
       AND (knocked_at IS NULL) = (knock_code IS NULL));
ALTER TABLE invites ADD CONSTRAINT invites_knock_needs_telegram
    CHECK (knocked_at IS NULL OR channel = 'telegram');
```

- **Existing rows** are all email invites with an address, so the `DEFAULT
  'email'` satisfies every new constraint when it is added. Run it against a
  restored production dump first, as `00011`'s own comment asks for any
  migration that constrains a table already holding real rows.
- `knock_chat_username` is display only. Nothing server-side decides anything
  from it, the same rule `00018` states for `chat_username`.
- **"Pending"** means `accepted_at IS NULL AND expires_at > now`. It is one
  definition, used by the list, the checklist and the operator's view.

## API

| Route | Guards | Answer |
|---|---|---|
| `GET /household/invites` (M1) | owner | `200` `[PendingInvite]`; an empty list is `[]`, never `null` |
| `DELETE /household/invites/{id}` (M1) | owner, CSRF, browser session | `204` |
| `POST /household/members/invite` (M2 changes it) | owner, CSRF, **browser session (new)** | `201 {id, expiresAt, link?}`; `link` only for `channel: "telegram"`, shown once |
| `POST /household/invites/{id}/link` (M2) | owner, CSRF, browser session | `200 {link, expiresAt}`. Telegram invites only. Clears any knock and kills every earlier link. Used by both "Get a new link" and "Not them" |
| `POST /household/invites/{id}/admit` (M2) | owner, CSRF, browser session | `200 {member, signInSent}` |
| `PATCH`, `DELETE /household/members/{id}` (M2 changes them) | owner, CSRF, **browser session (new)** | unchanged |

`PendingInvite`:

```json
{
  "id": "…", "name": "Jane", "role": "owner", "capabilities": ["money", "…"],
  "channel": "telegram", "email": null, "expiresAt": "…",
  "knock": { "username": "jane_t", "code": "4812", "knockedAt": "…" }
}
```

`knock` is `null` until someone taps; `username` is `null` when Telegram sent
none. **Milestone 1 ships without `channel` and `knock`** (the columns don't
exist until migration `00021`). Milestone 2 adds both, and the frontend schema
treats their absence as `"email"` and `null` so the two milestones deploy
independently.

**Invite create body** gains `"channel": "email" | "telegram"`, parsed with a
`default` that refuses (fail closed). The kid-profile path (limited, no
sign-in) stays as it is today.

**"Not them" goes through the new-link route.** One endpoint does both: it
clears the knock, replaces the token, tells the knocked chat "That link is no
longer valid.", and returns the new link. Separate routes would give the
waiting card a third state ("cancelled, no link yet") for no benefit. An owner
who suspects a leak and wants no new link can withdraw the invite instead.

## Errors

| Case | Status | Message the owner sees |
|---|---|---|
| Admit, nobody has knocked (or the link was renewed meanwhile) | `409` | "No one is waiting on this link." |
| Admit, the knocked chat joined another household meanwhile | `409` | "That Telegram account joined another household. Get a new link." |
| Admit or withdraw, already accepted (two owners clicked at once) | `409` | "This invite was already accepted." |
| Email invite while `email_invites` is off | `409` | "Email can't leave this install yet. Use a Telegram link." |
| Telegram invite while Telegram is off or unconfigured | `409` | "Inviting by Telegram is unavailable on this install." |
| New link for an email invite | `409` | "Only Telegram invites have a link." |
| Any invite id from another household | `404` | Never `403`, so the id's existence isn't confirmed |

Each is a new `domain` error plus a row in `domainErrorResponses`. What the
**chat** sees stays bland (decision 2 and `telegramDeadLinkMessage`'s own
reasoning): unknown, expired, used and already-knocked links all get the one
dead-link reply.

## Ports

All in `usecase/ports.go`, each with its contract in a doc comment.

- **`InviteRepository`** gains:
  - `ListPending(ctx, householdID, now)`: pending invites for one household.
    Every query is scoped by household **in the SQL itself**.
  - `Delete(ctx, householdID, inviteID)`: deletes only a not-yet-accepted
    invite. It returns `domain.ErrInviteAlreadyAccepted` for an accepted one
    (that row is history, not something to withdraw), and `domain.ErrNotFound`
    when no row matches in that household.
  - `CreateTelegram(ctx, householdID, name, role, caps, tokenHash, invitedBy,
    expiresAt) (id string, err error)`: a `telegram`-channel row with no email.
  - `RecordKnock(ctx, tokenHash, chatID, username, code, now)`: one guarded
    `UPDATE`, succeeding only when the channel is `telegram`, the invite is
    pending and `knocked_at IS NULL`. It reports `domain.ErrNotFound` for every
    other case, deliberately one answer.
  - `ReplaceToken(ctx, householdID, inviteID, tokenHash, expiresAt)`: clears
    the knock in the same statement and returns the chat that had knocked, if
    any, so it can be told.
  - `Admit(ctx, householdID, inviteID, now)`: decision 5's transaction.
    Returns `domain.ErrInviteNotKnocked`, `domain.ErrChatAlreadyBound` or
    `domain.ErrInviteAlreadyAccepted`, with nothing written.
- **`InviteKnocker`** is a new, one-method port declared for
  `TelegramAuthService`: `Knock(ctx, rawToken, chatID, username) (code string,
  err error)`. `InviteService` implements it. `TelegramAuthService` keeps
  ownership of every word the bot says; `InviteService` keeps ownership of
  invite rules.
- **`InviteChats`** is a new port declared for `InviteService`:
  `SendSignIn(ctx, chatID, userID) error` and `SendLinkCancelled(ctx, chatID)
  error`. `TelegramAuthService` implements it, reusing its existing
  `sendSignIn`, so no second magic-link path exists.
- **`PairingCodes`** is a new one-method port: `NewCode() (string, error)`,
  four digits from `crypto/rand`, in `adapter/crypto`. It is a port so tests
  are deterministic.

**Wiring risk, named now (`docs/LEARNING.md` pattern 23):** `InviteDeps` gains
`Chats`, `Codes`, `BotUsername` and a Telegram-invite TTL. Each must be wired
in `cmd/api/main.go` **and** in `api_test.go`'s own deps literal, or every new
route panics on a nil interface in tests while production works.

## Flow

```
Owner (browser)            API                          Telegram bot         Partner (phone)
     │ POST invite {telegram} │                             │                     │
     │───────────────────────▶│ create row, token hash       │                     │
     │◀─ 201 {link} ──────────│                             │                     │
     │   hands link over: copy / QR / share                  │                     │
     │                         │                             │◀── taps inv_… ──────│
     │                         │◀── HandleStart(inv_…) ──────│                     │
     │                         │ Knock: guarded update,      │                     │
     │                         │ code 4812                   │                     │
     │                         │──── "Your code: 4812" ─────▶│────────────────────▶│
     │ polls GET invites (3 s) │                             │                     │
     │◀─ knock {@jane_t,4812} ─│                             │                     │
     │   owner compares codes by eye                         │                     │
     │ POST …/admit ──────────▶│ one tx: user, membership,   │                     │
     │                         │ telegram_accounts, accepted │                     │
     │◀─ 200 {member,          │── after commit: sign-in ───▶│── sign-in link ────▶│
     │        signInSent} ─────│                             │                     │
```

`HandleStart` checks for the `inv_` prefix **before** its existing
`Links.Consume`, strips it, and hands the rest to `InviteKnocker`. Every other
payload behaves exactly as today.

**No new rate-limit surface.** A knock uses up the link and creates nothing:
no session, no magic link, no sign-up row. The existing per-chat hourly limit,
which bounds those rows, is not involved. A chat hammering `/start inv_…` gets
the dead-link reply and writes nothing.

## Frontend

- **`InviteMemberModal.tsx`**
  - Owner role, `email_invites` off: no email field; submitting creates a
    Telegram invite.
  - Limited role: "Profile only (no sign-in)", today's kid path, or "Can sign
    in (Telegram link)".
  - With `email_invites` on, email returns as a channel choice.
  - On success for a Telegram invite, the modal becomes the waiting card
    instead of closing.
- **`PendingInviteCard.tsx`** (new): one component, mounted in the modal's
  success state and in the Pending section.
  - **Waiting:** the link, if this session still holds it, with Copy, QR code
    and Share to Telegram (`https://t.me/share/url?url=…`), plus "Shown once.
    You can get a new link any time." Also has [Get a new link] and [Withdraw].
  - **Knocked:** "@jane_t tapped the link" (or "Someone with no Telegram
    username tapped the link"), then "Does their phone show **4812**?", with
    [Let in], [Not them] and [Withdraw].
  - **After Let in with `signInSent: false`:** decision 6's message.
- **Pending section in `MembersPanel.tsx`** (M1): every pending invite, with
  name, role, channel, expiry and Withdraw. It renders only for owners; a
  limited member's `403` is routine and never shown as an error.
- **`usePendingInvites.ts`** (new):
  - The query key comes from an exported builder, not a literal.
  - It polls every 3 s only while at least one Telegram invite has no knock,
    and stops when there's a knock, when nothing is pending, or when the tab
    is hidden (the `TelegramPanel` pattern).
  - Create, withdraw, new link and admit refresh this list. **Admit also
    refreshes the members list and the Overview checklist**
    (`docs/LEARNING.md` pattern 22).
- **`SetupChecklist.tsx`** (M1): a fourth step, "Invite your partner". It is
  done when the household has two or more owners. It shows "invite sent" while
  an owner-role invite is pending, and links to Settings → Members. Its header
  comment, which explains why the step was missing, is rewritten. The
  Agreements "Invite your partner" link goes to the same place.
- **The operator's household drill-in** already lists pending invites
  (`ListPendingInvitesForAdmin`). It must show a Telegram invite's missing
  email as "Telegram link", not a blank or a crash.
- **QR dependency:** `qrcode-generator` **2.0.4**, MIT, no dependencies of its
  own, types included. Pinned exact (`docs/LEARNING.md` pattern 7). It runs in
  the browser only, so the link never travels back to the server.

## Testing

**Usecase (in-memory doubles):**
- `HandleStart` routes `inv_` to the knocker and every other payload exactly as
  before.
- Knock order: a chat already linked is refused and nothing is recorded.
- One knock per link: the second tap gets the dead-link reply.
- Unknown, expired, email-channel and already-knocked tokens all get the
  **same** reply.
- Admit's re-checks each refuse with nothing written.
- A new link kills the old one; "Not them" tells the knocked chat.
- `signInSent: false` when the sender fails, with the member still created.
- The email-flag refusal and the Telegram-unavailable refusal.

**Postgres (testcontainers):**
- Both CHECK constraints refuse bad rows.
- **Two knocks at the same moment: exactly one wins**, with the overlap forced
  by a barrier, not hoped for (`docs/LEARNING.md` pattern 19).
- Admit is all or nothing: fail the `telegram_accounts` insert and assert no
  user or membership remains.
- Delete and the pending list, given another household's id, touch nothing
  (`docs/LEARNING.md` pattern 24).

**HTTP:**
- Route-walk guard matrix: a limited member gets `403` on every new route; **a
  personal API token is refused on create, withdraw, new link, admit, and
  `PATCH`/`DELETE` members**; a missing CSRF token is refused.
- **Web accept of a Telegram invite behaves like an unknown token**
  (decision 7).
- The admit body has no code field (decision 3).
- `hearthctl`'s `routes.go` parity test passes with the new routes.

**Frontend (vitest):** the modal's channel choices under both flag states; the
card's three states; polling stops on a knock; a limited member's `403` stays
silent.

**Mutation checks named in advance.** Each must turn a test red:
1. Remove the household filter from `ListPending`.
2. Remove `knocked_at IS NULL` from `RecordKnock`.
3. Remove `requireCookieSession` from admit.
4. Let web accept through a Telegram invite.

**Browser walk (15 criteria, written in the plan):** it includes a real second
phone on the development bot. The PRD's dry-run metrics are criteria: zero
operator steps, and under two minutes from invite to partner inside, in the
same room.

## Milestones

| # | Ships | Schema | Visible change |
|---|---|---|---|
| 1 | `GET` and `DELETE` invites; the Pending section; the checklist step; the Agreements link target | none | Owners see and withdraw pending invites; the checklist asks them to invite their partner |
| 2 | Migration `00021`; Telegram invites; knock; admit; new link; the `email_invites` flag; the cookie-session guards (decision 12); web accept refusing Telegram invites; ADR 11 | `00021` | The partner joins from their phone with no operator involved |

Each milestone ends at the project's definition of done: `make lint && make
test` green, mutation checks run, a browser walk, and `docs/FEATURE_TRACKER.md`,
`docs/LEARNING.md` and `docs/SYSTEM_DESIGN.md` updated. **Tracker rows:**
- "Telegram invites" (§ Identity, currently ⬜) becomes ✅ at M2.
- The Overview checklist note loses its "joins the list in the change that
  exposes pending invites" gap at M1.
- New rows: the pending list and withdraw (M1); knock and Let in (M2); the
  `email_invites` flag (M2).
- "Manage API tokens in Settings" stays ⬜ until the milestone 3 spec.

## Out of scope

- **The access list** (PRD milestone 3): API tokens and linked chats in one
  Settings area. Its own spec.
- **Real email and a domain.** A separate ADR 3 decision. The flag in decision
  10 is how email comes back.
- **Revoking another member's token or chat.** It would need an ADR 7
  amendment.
- **Several knocks per link.** Decision 2.
- **Changing the role at Let in.** Decision 14.
- **Adding an email address to a Telegram-only member later.** Not needed to
  join. Note that `TelegramLinkService.Unlink` already refuses to unlink an
  account with no email, so a Telegram-only member keeps their chat.
- **Invites over WhatsApp, SMS or other channels.**
- **A list of signed-in devices.** A natural later row for the access list.
