# Telegram account linking — verification walkthrough

> **STATUS: NOT YET RUN.** This is a walk *plan*, not a walk *result* — the
> shape `2026-09-01-telegram-sign-in-verification.md` and
> `2026-09-02-hearth-admin-households-verification.md` are in once they have
> actually been walked. The code is complete, reviewed across eight tasks,
> and `make lint && make test` is green on the tree (confirm this again
> before starting: `git log --oneline -1` should show the docs commit this
> file shipped in, and nothing uncommitted).
>
> Running it needs the running dev stack and the owner's own Telegram
> account — a second Telegram account for the one criterion that needs one
> (see criterion 8's note). This file was written by an agent that did
> **not** have either; every "Result" line below is empty on purpose, for
> whoever runs it to fill in. Do not write a pass/fail into this file
> without having actually done the step above it.
>
> Do not mark `docs/FEATURE_TRACKER.md`'s "Link an existing account to a
> Telegram chat" row ✅ until this file's own STATUS line is changed to say
> the walk ran, the same rule the sign-in and households walks followed.

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

### 1. Settings shows "Not connected" with a Connect button

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

**Result:**

---

### 2. Connect opens Telegram on the bot with a `?start=` payload

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

**Result:**

---

### 3. `/start` replies with the confirm instruction and no sign-up link

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

**Result:**

---

### 4. `SELECT count(*) FROM households` is unchanged

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

**Result:**

---

### 5. The panel moves to pending and names the chat

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

**Result:**

---

### 6. Confirm writes the binding; the panel shows connected with a `linkedAt`

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

**Result:**

---

### 7. `/balance` in that chat now answers, where it previously refused

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

**Result:**

---

### 8. A second Connect attempt is refused with "already has a Telegram chat"

**Cannot be fully walked with one Telegram account — read this before
attempting it.** `ErrTelegramAlreadyLinked` only fires on `Confirm` when a
**different** chat redeems a **second** pending link for a user who is
already bound — this account only has one Telegram account to test with, so
that exact collision cannot be produced by hand here.

**What can be observed with the account already connected (do this
part):**

**Do:** With the account still connected from criterion 6, look at the
Settings card, and separately call `POST /api/v1/auth/telegram/link` again
(e.g. `hearthctl api POST /auth/telegram/link`, or a second click if the
panel still exposes one).

**Look for:** The panel's connected state offers no Connect button at all —
only Disconnect — so the UI-level "you already have one" is the absence of
the control, not a refusal message. The API mint itself still succeeds
(`200`, a new *waiting* link — nothing has redeemed it yet) — minting is not
what decision 2's fourth row guards; **confirming** a second chat onto an
already-bound user is. Sending
`/start` from the *same, already-bound* chat against this new nonce answers
"This chat is already connected to your Hearth account." (`handleLinkStart`'s
same-user branch) rather than the confirm-instruction message.

**Would count as failure:** The panel showing both Connect and Disconnect
at once for a connected account; the same-chat `/start` producing the
plain confirm-instruction message instead of the already-connected one.

**Not walked by hand — the actual chat-taken refusal:** covered by
`TestConfirmRefusesWhenTheMemberAlreadyHasAChat` (usecase, this exact
collision), `TestStatusRefusesWhenTheMemberAlreadyHasADifferentChat`
(the `Status` side of the same case) and
`TestHandleStartWithALinkNonceForAUserAlreadyBoundToADifferentChatSaysNothingUseful`
(the chat-side answer to it) — the same treatment the brief gives the
stolen-link case, itself recorded in Step 0 above.

**Telegram needed:** Only for the same-chat `/start` sub-check above; the
mint and the missing-Connect-button parts are browser/API only.

**Result:**

---

### 9. Disconnect removes the binding; `/balance` refuses again

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

**Result:**

---

### 10. `/start` after disconnecting offers a sign-up link again

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

**Result:**

---

### 11. A fourth `POST /auth/telegram/link` within the hour answers 429

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

**Result:**

---

### 12. An expired link confirms nothing and the panel says so

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

**Result (weak form):**

**Result (strong form, if run):**

---

### 13. The `window.open` popup-blocked fallback link in the waiting view

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

**Result:**

---

### 14. Whether a no-bot install ever paints the panel's heading before its 404 lands

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

**Result:**

---

## When every criterion passes

Update, in the same change:

- `docs/FEATURE_TRACKER.md`'s "Link an existing account to a Telegram chat"
  row, ⬜ → ✅, with what the walk showed and a link to this file — following
  the recount-by-symbol rule the file's own header states, not a delta.
- This file's own STATUS banner, to say the walk ran, when, and against
  which account (Step 0's decision).
- `docs/SYSTEM_DESIGN.md`'s §5 subsection, if the walk found the diagram or
  its prose said anything the real flow contradicted.

If any criterion fails, it is a defect to fix and re-verify, not a reason to
mark the row ✅ with a caveat unless the caveat names a real, accepted gap —
the same standard the households and outbound-inspector walks held to.
