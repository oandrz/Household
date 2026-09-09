# Telegram account linking — verification walkthrough

> **STATUS: WALKED (browser half), 2026-09-09. 9 of 12 criteria pass
> directly, plus 2 more found beyond the twelve. 3 criteria are NOT
> WALKED** — every one of them needs `/start` sent from the owner's own
> phone signed into Telegram, which this session did not have — **and are
> covered instead by a named test, not by a live chat.** This is why
> `docs/FEATURE_TRACKER.md`'s row moved ⬜ → **🟡**, not ✅: this project's
> bar is that ✅ means verified, and a third of this feature's own test plan
> genuinely was not.
>
> **How the walkable two-thirds were produced without a phone.** Where a
> criterion needed a chat to have redeemed a link — moving a row from
> `waiting` to `pending` — the row was written directly into
> `telegram_link_requests` with the exact statement `Consume` itself issues
> (`consumed_at`, `chat_id`, `chat_username`, in one `UPDATE`), never a raw
> `INSERT` improvising the shape. **Everything downstream of that row —
> `Status`'s derivation, `Confirm`'s re-checks and the binding it writes,
> the panel's rendering, the rate limit, the expiry — is exercised for
> real** through the actual HTTP routes and the actual browser. **Everything
> upstream of that row — `HandleStart` itself, the poller, and Telegram's
> own delivery of a real `/start`** — is what the three NOT WALKED criteria
> below cover, and is not exercised by this technique at all.
>
> Full raw notes: `.superpowers/sdd/2026-09-09-telegram-account-linking/walk-notes.md`.
> Two findings from the same walk that look like bugs and are not are
> recorded at the end of this file, after the criteria.

Criteria 1–12 are Step 3 of
`.superpowers/sdd/2026-09-09-telegram-account-linking/task-8-brief.md`, which
lists them from the design spec's own "Testing" section
(`docs/superpowers/specs/2026-09-09-telegram-account-linking-design.md`).
Criteria 13–14 are two checks earlier reviews of this feature asked the walk
to add, not in the brief's original twelve. Traps and setup are new to this
file — the sign-in walk's own traps 1–3 and 6–7 still apply and are repeated
below in short form; trap 4 (tap the bot's link on the machine running the
app) does **not** apply here in the same way — see the note under Setup.

The feature: a signed-in member connects their existing Hearth account to
their Telegram chat from Settings, and disconnects it again — the door that
was missing for every account that signed up by email rather than by
messaging the bot. Design decisions 1–12 in the spec; the flow is drawn in
`docs/SYSTEM_DESIGN.md` §5 ("Telegram — connecting an existing account, and
why the binding waits for a confirm"); the security property is
`docs/adr/0010-binding-a-chat-needs-a-confirm.md`.

---

## Read this first — traps, most of them about which account to use

1. **`TELEGRAM_BOT_USERNAME` takes no `@`.** `HearthOinkDevBot`, never
   `@HearthOinkDevBot`.
2. **Both `.env` values, or neither** — the API refuses to boot otherwise,
   and that check runs ahead of every `adminctl` subcommand too, so a
   leftover half-set pair also breaks `make seed`.
3. **Use the dev bot, never the production token.** `docs/adr/0008-authorisation-at-each-channels-inbound-edge.md`'s
   own consequences section records what happens with the shared production
   token: another poller holds it, `getUpdates` answers `terminated by other
   getUpdates request`, and the owner's own messages vanish into whichever
   process actually won the long poll. `@HearthOinkDevBot`'s token belongs in
   the local `.env` only; the production token stays in `deploy/.env` on the
   box and this walk never touches it.
4. **Two Docker daemons on this machine.** Run every `docker compose`,
   `make psql` and log check with `DOCKER_HOST` **unset** — colima is what
   the Go test suite uses via `CLAUDE.md`'s exported variables, and querying
   it here reads back an empty database that looks exactly like a broken
   feature.
5. **The chat you already have may already be bound — to a Telegram-only
   user, not necessarily to the email account you plan to use — check
   before you start, not after criterion 3 looks wrong.** The 2026-09-01
   sign-in walk had the owner's own `/start` provision a brand-new,
   **email-less** household from that chat; the 2026-09-08 chat-commands and
   free-text walks then ran "from the owner's already-linked chat"
   (`FEATURE_TRACKER.md`'s rows for both). If that dev Postgres volume has
   survived since — `make down` does not delete it, only `docker volume rm
   hearth_hearth-pgdata` does — the owner's own Telegram chat is most likely
   bound today to that email-less Telegram-only user, **not** to
   `andreas@hearth.family`. A private chat's id is the person's own Telegram
   user id, the same value regardless of which bot they message, so
   switching to `@HearthOinkDevBot` does not give a "fresh" chat either. Run
   the unfiltered query in **Setup, step 0** before criterion 1 and decide
   from its result which account this walk actually uses — do not assume
   either "unbound" or "bound to the email account."
6. **The mint cap is shared across this whole walk.** `TelegramLinkService.Start`
   refuses a fourth mint from one user inside an hour (`CountMintsSince`,
   decision 12). Criteria 2, 8 and 12 (weak form) each mint at least once,
   and a false-negative 429 on a later criterion is not a product defect if
   it is really this limit. **Run the criteria in the order below and keep a
   tally**; if you need to redo a step, that redo counts too. If the tally
   is at risk of hitting 3 before criterion 12, mint 12's link **first**,
   note its id and mint time, and confirm/expire it out of order rather than
   burning a fourth mint later in the walk.
7. **`make dev` occupies its own terminal**; keep `docker compose logs -f
   api` open in a second one throughout, as the sign-in walk's own trap 6
   says.

**Trap 4 from the sign-in walk (tap the bot's link on the machine running the
app) does not bind you here.** That trap exists because the sign-in flow
*completes on the device that taps* — the session lands wherever Telegram
was opened. This flow is the opposite: the browser that minted the link is
what completes the connection, by clicking Confirm, not the chat. `/start`
can therefore be sent from a phone while Settings stays open on a laptop;
the deep link only has to open *some* Telegram client signed into the
person's own account.

---

## Setup

### Step 0 — find out what the owner's own chat is *already* bound to, before assuming anything is unbound

**Query every `telegram_accounts` row, unfiltered — do not filter on
`email IS NOT NULL`.** The 2026-09-01 sign-in walk had the owner's own chat
create a brand-new, **email-less** household
(`FEATURE_TRACKER.md`: "provisioned the new owner signed in with `email`
NULL"), and the 2026-09-08 chat-commands and free-text walks then ran "from
their already-linked chat". Put together, the owner's personal Telegram
chat is quite possibly bound today to a **Telegram-only** user, not to
`andreas@hearth.family` — a query that only looks at users with an email
would report that chat as free when it is not, and every criterion that
sends `/start` from it would then fail against a chat the bot already
recognises.

```bash
docker compose exec postgres psql -U hearth -d hearth -c \
  "SELECT u.id, u.email, ta.chat_id, ta.chat_username, ta.linked_at
     FROM telegram_accounts ta JOIN users u ON u.id = ta.user_id;"
```

- **No rows at all** — the owner's chat has never been bound in this
  Postgres volume. Use `andreas@hearth.family` for criteria 1–10 and 13–14
  below; skip straight to Step 1.
- **A row naming `andreas@hearth.family`** (or whichever seeded owner you
  plan to use) — that account is already connected. Either use it as-is to
  walk criteria 6–10 only (skip 1–5, since it starts already connected —
  note this in the Result lines rather than forcing a fresh start), or clear
  it first: `DELETE FROM telegram_accounts WHERE chat_id = <the chat_id
  above>;` via `make psql`, then use the account from a clean, unbound
  state.
- **A row naming a Telegram-only user** (`email IS NULL`) — this is the
  household the 2026-09-01 walk created from the owner's own `/start`, and
  it is the one case where the product's own UI **cannot** clear the
  binding: `Unlink` refuses with `ErrTelegramUnlinkWouldLockOut` for exactly
  this account (decision 11 — see `docs/FEATURE_TRACKER.md`'s ⬜ row "Attach
  an email address to a Telegram-only account", the sibling gap this ADR's
  decision 11 names). Clear it by hand instead, since this is a dev database and the
  household it names has no other data anyone needs:
  `DELETE FROM telegram_accounts WHERE chat_id = <the chat_id above>;` via
  `make psql`. That leaves the Telegram-only household itself intact but
  with no bot access any more, which is fine for this walk — it is not the
  account being tested.
- **Either clearing step above, or a full reset** — `make down && docker
  volume rm hearth_hearth-pgdata && make up && make seed` — are both valid;
  the reset is heavier (it also clears every other feature's dev data) but
  guarantees a clean slate if the query above is confusing to read.
  **Record exactly which path you took and which account the walk used**,
  because criterion 4's household-count invariant and criterion 9's
  "refuses again" both depend on knowing this account's exact starting
  state.

**What actually happened, 2026-09-09.** The walk used
`andreas@hearth.family` throughout (every row in `walk-notes.md` names it or
its bound chat), and it used the synthetic-redemption technique described
in the STATUS banner for every step that would otherwise have needed a real
chat — including the `walk_test`/`999000111` chat used in criteria 5, 6, 8,
which is a fabricated id, not the owner's real personal chat. Because of
that, **Step 0's concern above was never actually exercised this time**:
this walk never sent a real `/start` from any phone, so whether the owner's
*genuine* Telegram chat was already bound in this Postgres volume never
came up. It is still exactly as live a question for whoever completes
criteria 3, 7 and 10 — re-run Step 0's query fresh before sending the first
real `/start`, rather than assuming this walk already settled it. By the
end of criterion 9, `andreas@hearth.family` was disconnected again, back to
the unbound state Step 0 would have found it in at the start.

**Not walked by hand at all in this file: the stolen-link takeover
scenario decision 1 exists to prevent.** It needs a *second* Telegram
account redeeming a link nonce minted by the first — this walk, run by one
person with one Telegram account, cannot produce that collision. It is
covered instead by two tests, cited again at criterion 8 below:
`TestConfirmRefusesAnotherMembersLink` (a different session's `userID`
calling `Confirm` on this session's own pending row — `ErrNotFound`, not the
binding) and `TestHandleStartWithALinkNonceLeavesTheBindingUnwritten` (the
mutation-checked proof that `/start` alone, with no confirm, never writes
anything — see its own comment: "a one-phase bind would make a leaked deep
link an account takeover").

### Step 1 — configure and start

**Check `.env` first — the dev bot has been configured there since the
2026-09-08 chat-commands walk, so appending blindly risks a duplicate,
possibly-conflicting pair of `TELEGRAM_BOT_*` lines:**

```bash
grep TELEGRAM_BOT .env
```

If both `TELEGRAM_BOT_TOKEN` and `TELEGRAM_BOT_USERNAME` are already set to
`HearthOinkDevBot`'s values, leave `.env` alone and skip to `make dev`
below. Only if either is missing or set to something else, add or correct
the pair (each `echo` on its own line, no unescaped apostrophe inside the
quoted value):

```bash
echo "TELEGRAM_BOT_TOKEN=<HearthOinkDevBot token from BotFather>" >> .env
echo "TELEGRAM_BOT_USERNAME=HearthOinkDevBot" >> .env
make dev            # own terminal, tails logs, does not return
```

In a second terminal:

```bash
docker compose logs -f api | grep -i telegram
# expect: telegram sign-in enabled bot_username=HearthOinkDevBot
```

`FlagTelegramSignIn` defaults **on** (`domain/featureflag.go`), so no
`/admin/flags` step is needed unless a previous walk turned it off for this
household.

### Step 2 — sign in and open Settings

Sign in as the account chosen in Step 0
(`docs/GUIDE.md`'s credentials for the seeded owner, or the invited member's
own password from completing their invite). Navigate to **Settings** and
find the **Telegram** card.

---

## The criteria

Telegram-needed criteria (must send `/start`, a command, or wait on a chat
reply from the owner's own Telegram account): **3, 4, 5, 7, 9's second half,
10, and 12's strong form.** Browser/API-only criteria (no Telegram account
needed at all): **1, 2, 6's click, 8's observable half, 11, 12's weak form,
13, 14.**

### 1. Settings shows "Not connected" with a Connect button — PASS

**Do:** With the chosen account signed in and unbound (Step 0), open
Settings and find the Telegram card.

**Look for:** The card's heading "Telegram", a sentence like "Connect a
Telegram chat to get reminders and use the bot from there," and a "Connect
Telegram" button — `TelegramPanel.tsx`'s unbound-and-not-linking state (the
last `binding.isSuccess && !binding.data.connected && linkId === null`
branch).

**Would count as failure:** No card at all (check the 404 case, criterion
14, is not accidentally firing); a card that shows "Connected" for an
account Step 0 confirmed unbound.

**Telegram needed:** No.

**Result:** PASS. Signed in as `andreas@hearth.family` via a real magic link
(requested from `POST /api/v1/auth/magic-link`, read out of Mailpit — no
credential changed). The Telegram card showed exactly: "Telegram / Connect
a Telegram chat to get reminders and use the bot from there. / Connect
Telegram".

---

### 2. Connect opens Telegram on the bot with a `?start=` payload — PASS

**Do:** Click "Connect Telegram". Watch the network panel for
`POST /api/v1/auth/telegram/link` and the tab/window it opens.

**Look for:** A `200` response shaped `{id, url, expiresAt}`, `url` matching
`https://t.me/HearthOinkDevBot?start=<nonce>`; a new tab or window opens to
that URL (`window.open(start.url, "_blank", "noopener")`); the panel moves to
its waiting state ("Open Telegram and press Start to connect this chat.
We'll check automatically.").

**Would count as failure:** The URL naming the wrong bot username; the popup
not opening (that alone is not a failure — see criterion 13 for the
fallback); the panel not moving to the waiting state.

**Telegram needed:** No — the mint and the URL can be fully checked without
ever pressing Start in the opened tab. (This step does count toward trap 6's
mint tally.)

**Result:** PASS. `POST /api/v1/auth/telegram/link` opened a new tab at
`https://t.me/HearthOinkDevBot?start=smejlCc5-l9mc5AP4DLX0v9t97B3ZJb2ravf83JVIMo`
and the panel moved to the waiting view. The same view also carried the
popup-blocked fallback link ("Didn't open? Open Telegram.") — see criterion
13 below, which this observation also answers.

---

### 3. `/start` replies with the confirm instruction and no sign-up link — NOT WALKED

**Do:** In the Telegram tab/app that opened, press **Start**.

**Look for:** A reply reading "Go back to Hearth and confirm this chat to
finish connecting it." — `handleLinkStartForUnboundChat`'s clean-case
message. No sign-up link (`/sign-up/<token>`), no sign-in link
(`/sign-in/magic?token=`), nowhere in the reply.

**Would count as failure:** Any link to complete a sign-up or sign-in
appearing in this reply — that would mean `HandleStart` fell through to
`sendSignUp`/`sendSignIn` instead of taking the link branch, i.e. decision 4
of the spec did not hold.

**Telegram needed:** Yes.

**Result:** NOT WALKED. No phone signed into Telegram was available to this
session, so no real `/start` was ever sent and no reply was ever read from
the bot. **Covered instead by `TestHandleStartWithALinkNonceLeavesTheBindingUnwritten`**
(`telegram_auth_test.go`) — it drives `HandleStart` directly with a live
link nonce and asserts the reply contains "confirm" and mints no magic
link, and its own comment states the property this criterion exists to
check: "a one-phase bind would make a leaked deep link an account
takeover." The absence of a sign-up link specifically is implied by that
same assertion (no `signups` row is minted on this path) rather than a
separate substring check on the reply text.

---

### 4. `SELECT count(*) FROM households` is unchanged — PASS, with a caveat

**Do:** Before criterion 3's `/start` (or from a `psql` session opened
before the walk began), record `SELECT count(*) FROM households;`. Repeat
the same query immediately after criterion 3.

**Look for:** The two counts equal. This is the discriminating check the
brief names explicitly: before this feature shipped, an unbound chat's
`/start` unconditionally minted a sign-up, which — had the person tapped the
link `SignUpCompleteScreen` would have shown — created a second household
for the same person.

**Would count as failure:** The count increases by one after criterion 3's
`/start`, meaning a link nonce still fell through to `sendSignUp`.

**Telegram needed:** Yes (it is timed around criterion 3's `/start`).

**Result:** PASS — households: 6 before, 6 after; signups: 8 before, 8
after. **Caveat, said plainly:** since criterion 3's real `/start` was not
walked, this count was taken before and after the walk's own
synthetic-redemption write (the `Consume`-shaped `UPDATE` described in the
STATUS banner above) rather than around a live `HandleStart` call. It
genuinely shows the redemption row itself creates neither a household nor a
signup — real evidence, since nothing in this codebase polls
`telegram_link_requests` and reacts to a row appearing in it — but it does
not, by itself, prove `HandleStart`'s link branch skips `sendSignUp` on a
real `/start`; that half of the guarantee is what criterion 3's cited test
covers instead.

---

### 5. The panel moves to pending and names the chat — PASS

**Do:** Back in the browser, wait up to 3 seconds (the panel's own poll
interval, `telegramPollInterval`) after criterion 3's `/start`.

**Look for:** The panel's status flips from waiting to pending without a
manual reload, showing the redeeming chat's `@username` (the account
`/start` was sent from) and a **Confirm** button. Check the network panel:
`GET /api/v1/auth/telegram/link/{id}` answering `{"status":"pending",
"chatUsername":"<your username>"}`.

**Would count as failure:** The panel never leaves the waiting state (poll
not firing, or the status endpoint still returning `waiting`); the chat name
shown does not match the account that actually sent `/start`.

**Telegram needed:** No new Telegram action — this is the browser observing
criterion 3's effect, but it depends on criterion 3 having happened.

**Result:** PASS, via the synthetic redemption in place of a live `/start`
(STATUS banner above). The panel read: "**@walk_test** opened this link.
Confirm it's you to finish connecting." with a Confirm button — the
`pending` state, correctly naming the chat that redeemed it.

---

### 6. Confirm writes the binding; the panel shows connected with a `linkedAt` — PASS

**Do:** Click **Confirm**.

**Look for:** `POST /api/v1/auth/telegram/link/{id}/confirm` answers `200`
with `{"connected":true,"chatUsername":"...","linkedAt":"..."}`; the panel
re-renders to its connected state — "Connected as **@username**", "Linked
<date>", and a **Disconnect** button.

**Would count as failure:** A `409` from any of the five named errors (would
mean Step 0's unbound check was wrong, or a race with another criterion);
the panel staying on the pending screen after a `200`.

**Telegram needed:** No — this is a browser click. (Criterion 3–5 needed
Telegram to reach this point.)

**Result:** PASS. `Confirm` answered `200`; the panel read "Connected as
**@walk_test** / Linked Sep 9, 2026 / Disconnect". The
`telegram_accounts` row: id `4138040e…`, chat `999000111`, username
`walk_test`.

---

### 7. `/balance` in that chat now answers, where it previously refused — NOT WALKED

**Do:** **Before** criterion 3's `/start` (i.e. earlier in the walk, on the
still-unbound chat), send `/balance` from the same Telegram account and
record what it said. After criterion 6's Confirm, send `/balance` again from
the same chat.

**Look for:** The **before** reply is the Commander's ordinary refusal for
an unbound or unauthorised chat (`docs/adr/0008-authorisation-at-each-channels-inbound-edge.md`
— "one sentence and no service call"). The **after** reply is a real balance
answer, the same shape the 2026-09-08 chat-commands walk recorded.

**Would count as failure:** `/balance` answering correctly even *before*
Confirm (would mean the chat was bound some other way, invalidating the
whole walk's premise); `/balance` still refusing *after* Confirm.

**Telegram needed:** Yes, twice — once before criterion 3, once after
criterion 6. **Send the "before" message early**, since it is the baseline
this criterion needs and is easy to forget once the flow is under way.

**Result:** NOT WALKED. No phone signed into Telegram was available, so
neither the "before" nor the "after" `/balance` was actually sent. **Covered
instead by two adapter tests, composed:** `TestAnUnlinkedChatIsRefusedBeforeAnyServiceCall`
(`commands_test.go`) proves the Commander's chat-resolution guard refuses
before any service call for an unbound chat — it exercises `/spend` as its
example command, but the guard it tests runs identically ahead of every
command including `/balance`, resolving chat → membership before dispatch
— and `TestBalanceFormatsEachAccountInItsCurrency` proves `/balance`
answers correctly for a chat the resolver reports as linked to an owner.
Together they prove both halves this criterion asks for, without proving
they are the *same* chat before and after a real Confirm — that composition
is what a live walk would add.

---

### 8. A second Connect attempt is refused with "already has a Telegram chat" — PASS

**Corrected from the original plan.** The first draft of this file assumed
`ErrTelegramAlreadyLinked` needed a second, real Telegram account to
produce — it does not. The chat that redeems a link nonce is identified
purely by `chat_id`, a plain integer the service never validates against
Telegram itself, so a second pending link can be redeemed with any unused
`chat_id` via the same synthetic-write technique the STATUS banner
describes, with no second phone involved at all.

**Do:** With `andreas@hearth.family` already connected from criterion 6,
mint a second link (`POST /api/v1/auth/telegram/link`) and redeem it with a
synthetic row naming a **different, still-unbound** `chat_id`. Attempt
`POST /auth/telegram/link/{id}/confirm` on that second row.

**Look for:** `Status` on the second row derives `refused` with the reason
`ErrTelegramAlreadyLinked.Error()` ("this account already has a telegram
chat"); `Confirm` answers `409 TELEGRAM_ALREADY_LINKED`.

**Result:** PASS — exactly this. `Status` derived `refused` with reason
"this account already has a telegram chat"; `Confirm` answered `409
TELEGRAM_ALREADY_LINKED`.

**A related but distinct scenario is still not walked, and is worth naming
separately: `ErrTelegramChatTaken`** — a link redeemed by a chat *already
bound to someone else*, rather than the confirming user already having a
different chat. The notes above do not report this collision being
produced. **Covered instead by `TestConfirmRefusesAChatSomeoneElseHasBound`**
(usecase — binds chat `705` to `user-9`, then has `user-1` try to confirm a
link redeemed by that same chat, asserting `ErrTelegramChatTaken`) and
`TestHandleStartWithALinkNonceForAChatSomeoneElseOwnsSaysNothingUseful`
(the chat-side bland answer to the same case).

**Telegram needed:** No — both the walked scenario and the still-uncovered
one are producible with database state alone; neither needs a phone.

---

### 9. Disconnect removes the binding; `/balance` refuses again — PASS (disconnect half only)

**Do:** Click **Disconnect** on the connected panel. Confirm the panel
returns to its unbound state. Send `/balance` from the same Telegram
account again.

**Look for:** `DELETE /api/v1/auth/telegram` answers `200
{"connected":false}`; the panel shows "Connect Telegram" again; `/balance`
now answers with the Commander's refusal, the same wording as criterion 7's
"before".

**Would count as failure:** A `409 ErrTelegramUnlinkWouldLockOut` — would
mean the account chosen in Step 0 has `email IS NULL`, which should not be
possible for an email-signed-up account and would indicate Step 0 picked
the wrong account; `/balance` still answering after Disconnect (the binding
did not actually clear).

**Telegram needed:** Only for the `/balance` half; Disconnect itself is a
browser click.

**Result:** PASS on the Disconnect half, which is what was actually
checked. `DELETE /api/v1/auth/telegram` returned the panel to "Connect
Telegram"; a direct query confirmed `andreas@hearth.family` now has no
`telegram_accounts` row, and — worth recording since Step 0 warned this
account's own chat sits in a shared table with the owner's pre-existing
Telegram-only household — that other binding was left untouched.
**The `/balance` refuses-again half was not separately verified live**, for
the same reason as criterion 7: no phone was available. It follows from the
binding being genuinely gone (confirmed above) plus the same Commander
guard cited for criterion 7 (`TestAnUnlinkedChatIsRefusedBeforeAnyServiceCall`),
which refuses any command — `/balance` included — the moment `ByChatID`
reports no binding.

---

### 10. `/start` after disconnecting offers a sign-up link again — NOT WALKED

**Do:** After criterion 9's Disconnect, send a **fresh** `/start` (not a
replayed payload — open a new Connect flow, or use the bot's own menu/Start
button if Telegram offers a bare `/start` with no payload) from the same
chat.

**Look for:** The bot now treats the chat as a stranger again — a **sign-up**
link (`/sign-up/<token>`), the same reply `sendSignUp` produces for a chat
`Accounts.ByChatID` cannot find. This is the discriminating result: the
account is a stranger to the bot once more, exactly the state it was in
before criterion 3.

**Would count as failure:** A sign-in link instead (would mean the binding
was not actually removed); the confirm-instruction message (would mean a
stale, still-pending link nonce from earlier in the walk was replayed
instead of a fresh `/start` being processed as ordinary sign-in/sign-up).

**Do not tap the sign-up link that arrives.** Tapping it would complete a
*second* household for this person and permanently change the household
count criterion 4 relied on staying flat — observe the reply's text only.

**Telegram needed:** Yes.

**Result:** NOT WALKED. No phone signed into Telegram was available.
**Covered instead by two tests composed:** `TestUnlinkRemovesTheBinding`
(usecase — proves `Unlink` genuinely clears the `telegram_accounts` row)
and `TestHandleStartSendsASignUpLinkToAnUnknownChat`
(`telegram_auth_test.go` — proves an unbound chat's `/start` sends a
sign-up link, `sendSignUp`'s ordinary path). Together they prove the two
halves this criterion composes — disconnection genuinely unbinds, and an
unbound chat gets a sign-up link — without proving it live on the *same*
chat in one continuous session, which is what an actual walk would add.

---

### 11. A fourth `POST /auth/telegram/link` within the hour answers 429 — PASS

**Do:** Using `hearthctl` (it holds the cookie session and CSRF token from
`hearthctl login`, so it passes `requireCookieSession` the same as the
browser): `hearthctl api POST /auth/telegram/link` four times in a row for
the account used in this walk, counting **every** mint this walk has already
made against the same account (criteria 2 and 8 each mint once; see trap 6).

**Look for:** The call that is this account's **fourth** mint inside the
rolling hour answers `429` — the `ErrTelegramMintsRateLimited` case in
`api/internal/adapter/http/errors.go`'s mapping table; read the actual body
back and record it here rather than assuming its shape.

**Would count as failure:** A fourth mint still answering `200`; a mint
before the fourth already answering `429` (would mean the tally in trap 6
was miscounted, not a product defect — recount before concluding a failure).

**Telegram needed:** No.

**Result:** PASS. Attempts 1–3 answered `200`; attempt 4 answered `429`
`TELEGRAM_LINK_RATE_LIMITED`.

---

### 12. An expired link confirms nothing and the panel says so — PASS

**Two forms — do the weak one always; do the strong one if the walk's own
timing allows.**

**Weak form (no Telegram needed):**

**Do:** Mint a link (`POST /auth/telegram/link`) and **do not** send
`/start` to it at all. `cmd/api/main.go` wires the running api to the real
system clock (`sysClock`), not a mockable one, so there is no in-process way
to fast-forward it — the fast path is to edit the row directly instead of
waiting the full ten minutes:

```bash
docker compose exec postgres psql -U hearth -d hearth -c \
  "UPDATE telegram_link_requests SET expires_at = now() - interval '1 minute'
   WHERE id = '<the link id from the mint response>';"
```

(Waiting the real ten minutes works too, if editing the row by hand feels
like cheating the walk — just say in the Result which one you did.) Then
poll `GET /auth/telegram/link/{id}` and attempt
`POST /auth/telegram/link/{id}/confirm` (`hearthctl api POST
/auth/telegram/link/{id}/confirm`).

**Look for:** `Status` answers `expired`; the panel, if left open on this
link, stops polling once it reads `expired` (`telegramPollInterval` returns
`false` for that status — check the network panel shows no further requests
after the transition) and shows "That link expired. Start again to get a
new one." `Confirm` answers `409 ErrTelegramLinkNotPending`.

**Strong form (needs Telegram + the wait):**

**Do:** Mint a link, send `/start` to it (moving it to pending), then either
wait the real ten minutes or run the same `UPDATE … SET expires_at = now() -
interval '1 minute'` from the weak form above against this now-consumed row,
**without** confirming first.

**Look for:** The same `expired` status and refused confirm as the weak
form, now from a row that was genuinely consumed and pending first — this is
the closer match to `TestConfirmRefusesAfterExpiry`
(`row.Consumed && !row.ExpiresAt.After(now)`).

**Would count as failure (either form):** `Confirm` succeeding on an
expired row; the panel continuing to poll after `expired`.

**Telegram needed:** Only for the strong form.

**Result (weak form):** PASS. `Confirm` on an expired link answered `409
TELEGRAM_LINK_NOT_PENDING`.

**Result (strong form, if run):** Not separately distinguished in the walk
notes. The notes report a single expired-link result (above) without
stating whether the row had been consumed by a redemption first, and the
STATUS banner's own account of the walk says a synthetic redemption was
used only "where a redemption was needed" — the weak form needs none, so
that is the more likely reading, but this file will not claim the strong
form ran when nothing in the notes says so. `TestConfirmRefusesAfterExpiry`
(cited above) is the test that pins the strong form's exact shape
(`row.Consumed && !row.ExpiresAt.After(now)`) if a future walk wants to
close this specifically.

---

## Two checks beyond the brief's twelve, found and walked on the day

Neither of these was in the brief's original list; the walk ran them
anyway because both are directly implicated by decision 11 and by the
route table's own 404-vs-403 rule, and both were easy to check with the
tooling already in hand.

### Unlink refused for an account with no email — PASS

**Do:** With `users.email` temporarily set to `NULL` for the connected
account (`make psql`), call `DELETE /api/v1/auth/telegram`.

**Look for:** `409 ErrTelegramUnlinkWouldLockOut` — decision 11's guard: a
Telegram-only account has no other door back in, so `Unlink` must refuse
rather than strand it.

**Result:** PASS. `DELETE` answered `409 TELEGRAM_UNLINK_LOCKOUT`. The
temporary `NULL` and the test binding were both restored immediately
afterwards and verified back in place — this was a real, if brief, edit to
`andreas@hearth.family`'s own row, not a fixture.

### A row belonging to nobody answers 404, not 403 — PASS

**Do:** `GET /api/v1/auth/telegram/link/00000000-0000-0000-0000-000000000000`
(a well-formed but non-existent id).

**Look for:** `404`, never `403` — the same rule the design's own API
section states: a row id must not be testable for existence by getting a
different status for "not yours" versus "does not exist."

**Result:** PASS. The request answered `404`.

---

### 13. The `window.open` popup-blocked fallback link in the waiting view — PARTIALLY WALKED

Added by an earlier review of this feature; not in the brief's original
twelve.

**Do:** In a browser configured to block popups (or a browser/profile where
`window.open` with `_blank` reliably fails — `TelegramPanel.tsx`'s own
comment on `handleConnect` names this as the expected WebKit behaviour,
since `window.open` runs inside the mutation's `onSuccess`, after the
click's own user-activation gesture has already passed), click "Connect
Telegram".

**Look for:** Even though the new tab does not open, the waiting-view text
("Open Telegram and press Start...") is followed by "Didn't open? **Open
Telegram**." — a real `<a href="...">` link carrying the same `start.url`
(`TelegramPanel.test.tsx`'s own test, "offers the deep link as a plain
fallback while waiting, for a blocked popup", already pins this at the unit
level; this criterion is confirming it in an actual browser with a real
popup blocker, not jsdom). Clicking that link opens Telegram in a normal tab
and the rest of the flow proceeds as in criteria 3–6.

**Would count as failure:** No fallback link rendered when the popup is
blocked — a dead end with no way to reach Telegram at all.

**Telegram needed:** No, to observe the link exists; yes, to confirm
clicking it actually completes the flow (can reuse the walk's normal
Connect attempt instead of a separate one, to conserve the mint tally from
trap 6).

**Result:** PARTIALLY WALKED, not a dedicated popup-blocked test. In
criterion 2's ordinary Connect click, `window.open` succeeded (a new tab
opened normally — this browser was not configured to block popups) *and*
the waiting view still rendered "Didn't open? **Open Telegram**." as a real
link carrying `start.url`, exactly as `TelegramPanel.test.tsx`'s own test
predicts. That confirms the fallback link exists and renders correctly
whenever a link is waiting, regardless of whether the popup actually
opened — but the specific trigger this criterion asks about (a browser
that genuinely blocks the popup) was never induced, so the dead-end case
this criterion exists to rule out was not directly exercised. Worth a
dedicated pass in a popup-blocking browser profile before this criterion is
called fully done.

---

### 14. Whether a no-bot install ever paints the panel's heading before its 404 lands — NOT ATTEMPTED

Added by an earlier review of this feature; not in the brief's original
twelve.

**Do:** In a **separate** run of the dev stack with both `TELEGRAM_BOT_TOKEN`
and `TELEGRAM_BOT_USERNAME` **unset** (both empty — decision from
`config.go`'s "both or neither"), sign in and open Settings. Watch the
Telegram card's mount closely — a screen recording or the Performance/Network
panel's timeline is more reliable than watching live, since the code
predicts this is a brief flash rather than a stable state.

**Look for what the code predicts, and confirm whether it is actually
visible:** `TelegramPanel`'s `bindingUnavailable` check (`binding.isError &&
… status === 404`) is only true **after** `GET /api/v1/auth/telegram`
resolves. Before that response lands, `binding.isPending` is true and
`bindingUnavailable` is false, so the component renders its full card —
the `<h2>Telegram</h2>` heading and "Loading…" — and only unmounts to `null`
once the 404 arrives. On a fast local network this may resolve within a
single paint and never be perceptible; on a throttled connection (Chrome
DevTools' network throttling is the easiest way to force this) it should be
visible as a heading-then-gone flash.

**Would count as failure — but record it as a defect to fix separately, not
mid-walk:** a perceptible flash of "Telegram" chrome on an install that has
no bot at all, before the row disappears. This is a real UX gap the code
predicts rather than a criterion this feature's tests were meant to catch
(no jsdom test has a network delay to observe against, the same class of gap
`docs/LEARNING.md` pattern 15's seventh instance names for a different
screen).

**Telegram needed:** No — this is entirely about `GET /auth/telegram`'s
timing on a no-bot install, no Telegram account involved.

**Result:** NOT ATTEMPTED. This walk ran against the ordinary dev stack
with `TELEGRAM_BOT_TOKEN`/`TELEGRAM_BOT_USERNAME` set throughout; a
separate no-bot run was never started, so nothing here confirms or refutes
the predicted flash. Still open for a future pass.

## Findings from the walk that look like bugs and are not

Both recorded in the raw notes
(`.superpowers/sdd/2026-09-09-telegram-account-linking/walk-notes.md`),
carried here in full because both are exactly the kind of thing the next
person debugging "the panel is broken" would otherwise waste an hour on.

**1. The panel stops polling while its tab is hidden.** TanStack Query's
`refetchInterval` — the mechanism behind `telegramPollInterval`'s "every
3 seconds while waiting or pending" — is paused by the browser whenever
`document.visibilityState === "hidden"`, which is precisely the state a
person is in the instant they switch away to Telegram to press Start. This
is not a bug: the request resumes, and the panel catches up, the moment the
tab becomes visible again — proven live during this walk, where the panel
sat on the waiting view with the API already answering
`{"status":"pending","chatUsername":"walk_test"}` underneath it, and
flipped to the named-chat confirm view within one tick of the tab regaining
focus. The flow this feature is actually built for — leave the tab, press
Start in another app, come back — self-heals for free. What is worth
writing down is that neither this plan's own criteria nor
`telegramPollInterval`'s "every 3 seconds" description say anything about
tab visibility, so a person watching a background tab and expecting a
3-second update will file this as broken before it self-heals.

**2. When a link is both expired and refusable another way, the panel and
`Confirm` disagree about why.** `Status` derives refusals *before* expiry
(the order decision 5 and this plan's §5-citing prose both call
deliberate — a connected panel must not flip to "expired" ten minutes after
it actually succeeded), so a link that is both `TELEGRAM_ALREADY_LINKED`-shaped
and past its `expiresAt` still shows the panel "this account already has a
telegram chat." `Confirm`, called directly, checks expiry first and answers
`409 TELEGRAM_LINK_NOT_PENDING` on the same row. Both are dead ends for the
person holding the link, and the panel's sentence is the more specific,
more actionable one of the two — so this is working exactly as the spec's
own derivation order intends, not a mismatch to fix. Recorded because the
two answers disagreeing, side by side, reads like a bug until the ordering
is understood to be chosen on purpose.

---

## Where this leaves the feature

Nine of the twelve brief criteria pass directly; two more checks beyond the
twelve pass (unlink refused for an account with no email; a foreign link
row answers 404, not 403); criterion 13 is partially confirmed; criterion
14 was not attempted. **Three criteria — 3, 7, 10 — are NOT WALKED**, each
because it needs the owner's own Telegram account sending something from a
phone, which this session did not have; each is covered instead by a named
test, cited under its own heading above. `docs/FEATURE_TRACKER.md`'s row
moves ⬜ → **🟡** for exactly this reason, not ✅ — see that row for the
full statement.

**When the remaining three (and 13's dedicated form, and 14) are walked
against a real phone:**

- `docs/FEATURE_TRACKER.md`'s row moves 🟡 → ✅, and this file's own STATUS
  banner is updated to say so.
- `docs/SYSTEM_DESIGN.md`'s §5 subsection is revisited if the walk found the
  diagram or its prose said anything the real flow contradicted.

If any of those three later fails when actually walked, it is a defect to
fix and re-verify — the same standard the households and outbound-inspector
walks held to, and the reason this row is 🟡 rather than a ✅ carrying a
caveat.
