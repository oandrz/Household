# Partner invite lobby, milestone 2 — verification walk

**Walked 2026-09-20**, against a stack built from this branch
(`worktree-partner-invite-lobby-m2`), with real Postgres (migration `00021`
applied, goose version 21) and the development bot `@HearthOinkDevBot`
configured. Driven through Playwright at `http://localhost:5173`.

**Read the honesty note in §3 before treating this as a complete walk.** Six
of the fifteen criteria need a second person with a real phone and are
**not** verified here.

## 1. What was verified in a real browser

| # | Criterion | Result |
|---|---|---|
| 1 | Sign in as the seeded owner; Settings → Members shows "+ Invite" | ✅ signed in, panel renders |
| 2 | The modal asks for **no email address** (`email_invites` off) and offers Parent | ✅ no email field anywhere. A "How do they sign in?" radio group offers "Profile only (no sign-in)" and "Can sign in (Telegram link)". Selecting **Parent** removes the group entirely — a parent must be able to sign in, so there is no choice to present — and every capability becomes checked-and-disabled |
| 3 | Submitting creates the invite; the modal becomes the waiting card with the link, a QR code and "Shown once." | ✅ the modal did **not** close. QR rendered client-side, full link shown, Copy + Share to Telegram, and "Shown once. You can get a new link any time." |
| 4 | Copy puts a `https://t.me/…?start=inv_…` URL on the clipboard | 🟡 the link text and Copy control are present and correct (`https://t.me/HearthOinkDevBot?start=inv_EHHDOnpZkYLK5E-6H3UennLq96e7Mn0R80aAmRqIbOs`); the clipboard write itself was not read back |
| 5 | The Pending section lists the invite with role, channel and expiry | ✅ and it distinguishes the two channels correctly: the email invite shows `blaze796@gmail.com · Expires Sep 27`; the Telegram invite shows name + `Expires Sep 21` (24h) with **Get a new link**, because the list never holds a link |
| 7 | Within three seconds, with no reload, the card shows who tapped and the same four digits | ✅ **no reload**. The poll picked up the knock and the card flipped to "@walkuser_t tapped the link" / "Does their phone show **4812**?" with [Let in] [Not them] [Withdraw] |
| 11 | **Let in** creates the member with the role the invite carried | ✅ "Walkuser — Parent · full access — Owner" appears in Members, and the invite leaves Pending. Both lists refreshed without a reload |
| — | **`signInSent: false` recovery line** (spec decision 6) | ✅ **the most important result here.** The send genuinely failed (the knock's chat id is not a real chat), and the card showed *"Let in. If no message arrived, ask them to send /start to the bot."* Before Task 11's fix round this line **could never render**: `useAdmitInvite`'s `onSettled` returned its invalidation promise, and TanStack Query withholds a mutation's `data` until that resolves, so the row — and the card reading it — had already left the list. Every test passed anyway, because each built an isolated `QueryClient` with no active observer |

## 2. Verified against the running server, outside the browser

Real HTTP against the running API and Postgres, not the test suite:

| Check | Result |
|---|---|
| Create Telegram invite | `201 {id, expiresAt, link}` |
| Payload budget | raw token **43 chars**; `inv_` + 43 = 47, under Telegram's 64-character `start` limit |
| Expiry | exactly **24 hours** (spec decision 8) |
| Pending list shape | Telegram row `email: ""`, `channel: "telegram"`, `knock: null`; the pre-existing email invite still `channel: "email"` |
| Email invite while the flag is off | `409 EMAIL_INVITES_DISABLED` — "Email can't leave this install yet. Use a Telegram link." |
| Admit with nobody knocking | `409 INVITE_NOT_KNOCKED` — "No one is waiting on this link." |
| **Web accept of a Telegram token** (spec decision 7) | **`404` on both `GET /invites/{token}` and `POST /invites/{token}/accept`** — indistinguishable from an unknown token. This is the milestone's sharpest security property: without it, a link holder accepts in a browser with a password of their own and skips the owner entirely |
| New link kills the old one | the previous raw token now answers `404` |

## 3. NOT verified — and why

**These six need a second person holding a real phone on `@HearthOinkDevBot`.
They were not performed, and nothing here should be read as evidence for
them.**

| # | Criterion | Why not |
|---|---|---|
| 6 | Tapping the link makes the bot reply with a four-digit code | needs a real Telegram client tapping a real deep link |
| 8 | A second tap of the same link gets the dead-link reply | same |
| 9 | **Not them** replaces the link and tells the knocked chat | the *route* is proven (§2: the old token 404s); the chat message is not |
| 10 | The new link knocks again with a **different** code | needs a real tap |
| 12 | The phone receives a sign-in link and it signs that person in | needs a real chat |
| 15 | A limited member sees no Pending section and no invite controls | not testable on this seed: kid profiles are sign-in-less by design, and no limited member with credentials exists |

**How the knock in criterion 7 was produced.** Since no phone was available,
the knock row was written directly into Postgres exactly as
`RecordInviteKnock` writes it (`knock_chat_id`, `knock_chat_username`,
`knock_code`, `knocked_at`). That is a faithful stand-in for the bot's
database write, and it proves the **frontend** poll and knocked card. It
proves nothing about `HandleStart`, the `inv_` routing, the one-knock guard
or the bot's replies — all of which are covered by the Go suites and by
mutation checks, but not by this walk.

**Criteria 13 and 14** (Overview's "Invite your partner" step completing, and
Agreements unlocking) were not meaningfully testable: the seeded household
already has two owners, so both were already satisfied before the walk began.
Adding a third owner does not change either.

## 4. Environment note

`docker-compose.yml` pins `name: hearth` on its first line, so a stack brought
up from this worktree **shares the main checkout's `hearth_hearth-pgdata`
volume** rather than getting its own. Migration `00021` was therefore applied
to the existing development database during this walk. The migration is
additive and backward-compatible — `channel` is `NOT NULL DEFAULT 'email'`
and the pre-milestone code always supplies an address — so the older branch
continues to work against it. A down migration exists if a revert is wanted.
