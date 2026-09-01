# Telegram sign-in — verification walkthrough

> **STATUS: RUN AND PASSED, 2026-09-01. 12 of 12 numbered criteria pass**,
> plus all five checks that carry no number of their own: the setup gate, the
> bot-token-log check, the `/sign-up` dead end, the inert password, and the
> off switch. No product defect was found.
>
> This is now a walk *result*, not a walk *plan* — the same shape as
> `2026-08-16-hearth-retros-verification.md` and
> `2026-08-28-hearth-vision-verification.md`, which it named as its models
> while it was still unrun. Telegram sign-in shipped code-complete, reviewed
> across nine tasks, with `make lint && make test` green, and the walk sat
> unrun for exactly the reason this banner used to say: running it needs a
> Telegram account and a BotFather token. Both now exist — the human partner
> ran the walk on their own machine, against `make dev`, with a real
> BotFather bot, `HearthOinkBot`.
>
> Two traps not on the original five-trap list cost real time before a
> single criterion could be exercised, neither of them a product defect —
> they are now traps 6 and 7 in the list below: `make dev` occupies its own
> terminal, and this machine's two Docker daemons mean querying the wrong
> one's postgres reads back an empty database that looks exactly like a
> broken feature.
>
> Every criterion's Result cell below is filled in. `docs/FEATURE_TRACKER.md`
> §1 now marks **Telegram sign-up** and **Telegram sign-in** ✅, dated
> 2026-09-01; `docs/SYSTEM_DESIGN.md`'s scope paragraph and
> [ADR 4](../../adr/0004-telegram-as-a-second-delivery-channel.md)'s status
> line say the same — the three edits "When every criterion passes" below
> called for, made in the same change as this file.

Criteria are Steps 3–6 of
`.superpowers/sdd/2026-09-01-telegram-sign-in/task-10-brief.md`, expanded with
the traps and the SQL that discriminate a real defect from a false failure. That
brief is git-ignored scratch; this file is the tracked record, the same split
the Retros, Goals, Bills and Vision walks already use.

The feature: Telegram delivers the sign-in and sign-up links Hearth already
mints, for installs where mail cannot leave the box
([ADR 3](../../adr/0003-mail-stays-on-the-box.md),
[ADR 4](../../adr/0004-telegram-as-a-second-delivery-channel.md)). The flow is
drawn in `docs/SYSTEM_DESIGN.md` §5.

---

## Read this first — seven traps before you get to a real result

Five of these (1–4, 7) will make the walk fail for a reason that is not a
defect. One (5) does the opposite — a spent payload can falsely "confirm" a
criterion nobody actually exercised. One (6) is neither: it costs you a
second terminal before you can run a single command in Setup.

1. **`TELEGRAM_BOT_USERNAME` takes no `@`.** The service builds
   `https://t.me/%s?start=%s`, so `HearthSignInBot`, never `@HearthSignInBot`.

2. **Both values, or neither.** Setting exactly one makes the API refuse to
   boot with
   `TELEGRAM_BOT_TOKEN and TELEGRAM_BOT_USERNAME must both be set, or both left empty`.
   That check runs before **every** `adminctl` subcommand too, so one leftover
   value also breaks `make seed`, `unlock-household` and `reset-password`.

3. **No trailing whitespace on the token.** Use the `echo … >> .env` form
   below rather than pasting into an editor. A token carrying a trailing
   newline makes URL parsing fail on the first send. The credential leak that
   used to expose the token through *that* error path is closed
   (`docs/LEARNING.md`, Tooling and infrastructure), but you would still be
   debugging a send that never works.

4. **Tap the bot's link on the machine running the app.** This is link-back by
   design — the session lands on the device that taps (ADR 4, "Why the link
   comes back to the tapper"). With `APP_BASE_URL=http://localhost:5173`,
   tapping in **phone** Telegram cannot work, because the phone has no such
   host. Use **Telegram Desktop** or <https://web.telegram.org> on the same
   machine. **This is the single most likely false failure.**

5. **Every `/start` needs its own fresh nonce.** Pressing Start again in
   Telegram replays a *spent* payload, and a spent payload returns the
   word-for-word identical refusal message a rate-limited chat gets — so it
   would falsely "confirm" criterion 11. Press **Continue with Telegram** in
   the browser before each Start.

6. **`make dev` occupies the terminal.** It ends with
   `$(COMPOSE) up --build api web`, which tails logs in the foreground and
   does not return, so every setup-gate command below — and every `curl` or
   `docker compose logs` you run through the rest of this walk — needs a
   second terminal open beside it.

7. **This machine has two Docker daemons.** colima (used by the Go test
   suite via `DOCKER_HOST`/`TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE`, per
   `CLAUDE.md`) and Docker Desktop, which is what `make dev` uses by
   default. Querying the wrong daemon's postgres returns an **empty
   database that looks exactly like a broken feature** — zero rows for a
   flow that just succeeded. Run `docker compose` and `make psql` for walk
   verification with `DOCKER_HOST` **unset**, and if a count is
   unexpectedly zero, check `docker ps` shows `hearth-api-1` before
   concluding anything.

---

## Setup

In Telegram, message **@BotFather** → `/newbot`, give it a display name and a
username ending in `bot`, and take the token and the username it replies with.

```bash
echo 'TELEGRAM_BOT_TOKEN=<the token BotFather gave you>' >> .env
echo 'TELEGRAM_BOT_USERNAME=<the username, no @>'        >> .env
make dev
```

`.env` is gitignored. **Do not commit it, and never paste the token into a
tracked file — including into this one when you record the results.**

Keep `docker compose logs -f api` visible throughout. **`make dev` above
does not return a shell (trap 6)** — run it in one terminal and everything
from here on, including `docker compose logs -f api`, in a second.

---

## The criteria

### Setup gate — the API actually sees the configuration

This is not a product criterion; it is what separates a real defect from
traps 2 and 3, and from the `docker-compose.yml` passthrough the wiring task
added (`docs/SYSTEM_DESIGN.md` §1).

```bash
docker compose logs api | grep 'telegram sign-in enabled'
# expect: telegram sign-in enabled bot_username=<your bot>

curl -s -o /dev/null -w '%{http_code}\n' -X POST \
     http://localhost:8080/api/v1/auth/telegram/start
# expect: 200        404 means the API is not seeing the two values
```

If it answers `404`, `docker compose config | grep TELEGRAM` shows exactly
what the container is being handed.

**The bot token never appears in the logs.** "Never logged in any branch" is
a named security property with one unit test behind it
(`TestSendMessageDoesNotLeakTokenWhenTokenBreaksTheURL` and its sibling in
`internal/adapter/telegram/client_test.go`) — this checks the running
container agrees, after the setup gate above has generated some real traffic
for the token to have leaked into. **A false-failure trap specific to this
check, on top of the seven above:** the `echo … >> .env` step above never
exports
`TELEGRAM_BOT_TOKEN` into your shell, so if the variable is read unset below,
`cut` emits an empty string and `grep -c ""` matches *every* line in the
logs — a large, alarming number that looks exactly like a leak but is really
just "the check never ran." Load it from `.env` first, in the same shell:

```bash
TELEGRAM_BOT_TOKEN=$(grep '^TELEGRAM_BOT_TOKEN=' .env | cut -d= -f2)
docker compose logs api | grep -c "$(cut -d: -f1 <<<"$TELEGRAM_BOT_TOKEN")"
# expect: 0
```

**Result:** PASS — `docker compose logs api | grep 'telegram sign-in enabled'`
read `telegram sign-in enabled bot_username=HearthOinkBot`;
`POST /api/v1/auth/telegram/start` answered `200`; loading
`TELEGRAM_BOT_TOKEN` from `.env` in the same shell first (per the trap above)
and grepping the logs for it returned `0`; the web app answered `200`.

---

### The `/sign-up` dead end

Whole-branch review, Item 7. `SignUpScreen.tsx` — "No household yet? Create
one", directly beneath the Telegram button on the sign-in screen — only knows
how to reach a household through a mailed link, so a stranger with no
reachable inbox used to have nowhere to go from there. The fix is a line of
copy pointing back to sign-in's Telegram option, not a second Telegram flow
built onto this screen (controller ruling R17).

| # | Criterion | Expect exactly | Result |
|---|---|---|---|
| — | **From the sign-in screen, follow "No household yet? Create one".** Note whether someone with no reachable email can get anywhere from the screen it lands on | A sentence pointing back to "Continue with Telegram" on the sign-in screen — a stranger with no inbox is never left with only an email form and no way forward | **PASS** — exercised; no separate note was supplied beyond the pass |

---

### The stranger path — a household created from a chat

| # | Criterion | Expect exactly | Result |
|---|---|---|---|
| 1 | **Continue with Telegram opens Telegram** — press it on the sign-in screen at <http://localhost:5173> | A new tab on `t.me/<yourbot>`. **If the browser blocks the popup**, the screen shows *"Your browser blocked the popup."* and an **Open Telegram** link — that is the designed fallback, not a failure. Click it | **PASS** |
| 2 | **The bot answers a stranger with a sign-up link** — press **Start** | `Tap to create your Hearth household:` · `http://localhost:5173/sign-up/<token>` · `This link works once, for 24 hours.` | **PASS** — the bot sent a create-household link |
| 3 | **The create-household screen knows the channel** — click that link | In place of the Email box: *"You are signing up with Telegram. Your sign-in links come to that chat."* **There must be no empty email input anywhere on this screen** | **PASS** — the create-household screen showed the Telegram sentence and no email box |
| 4 | **Provisioning completes** — fill household name, display name, currency and password, submit | You land signed in, inside the household | **PASS** — the form completed and landed signed in |
| 5 | **The owner has no email and a bound chat** — the SQL below | `email` NULL, one `telegram_accounts` row | **PASS** — `display_name=TestOinkTGest`, `email_null=t`, `has_pw=t`, `chat_bound=t` |

Criterion 5, via `make psql`:

```sql
SELECT u.id, u.email, u.display_name, t.chat_id, t.linked_at
  FROM users u JOIN telegram_accounts t ON t.user_id = u.id;
-- email must be NULL. One row per Telegram-provisioned user.
-- Note the chat_id — criterion 11 needs it.

SELECT id, email, telegram_chat_id, consumed_at
  FROM signups
 WHERE telegram_chat_id IS NOT NULL;
-- email NULL, telegram_chat_id set, consumed_at set.
```

| # | Criterion | Expect exactly | Result |
|---|---|---|---|
| 6 | **The exactly-one-channel CHECK is real** — the insert below | `new row for relation "signups" violates check constraint "signups_have_exactly_one_channel"` | **PASS** — the `INSERT` naming both `email` and `telegram_chat_id` was refused with `signups_have_exactly_one_channel` |

```sql
INSERT INTO signups (email, telegram_chat_id, token_hash, expires_at)
VALUES ('x@example.test', 123, '\x00', now() + interval '1 day');
```

*(Worth running once. It is the fail-closed half of the data model — a row
carrying both channels, or neither, is refused by Postgres rather than
reasoned about in Go — and no product path can produce it, so SQL is the only
way to exercise it. Same shape as Retros' criterion 10 and Vision's own
deleted-goal criterion, both of which needed raw SQL for a state the product
has no button for.)*

---

### The inert password

Whole-branch review, Item 1: a Telegram sign-up still collects a password
(`SignUpCompleteScreen.tsx`), but `users.email` is NULL for that account and
`GetUserByEmail` can never match NULL, so the password has nowhere to be
checked against. The walk cannot catch this on its own — nothing here fails
loudly — so it is written out explicitly.

| # | Criterion | Expect exactly | Result |
|---|---|---|---|
| — | **The password chosen at criterion 4 does not work.** Sign out, then on the ordinary sign-in form try the password chosen at criterion 4 (there is no email to pair it with, since the account has none — try the sign-in form's email field empty, or with any address you like) | It does not sign anyone in. There is no account `GetUserByEmail` can match, by design, until "attach an email address to a Telegram-only account" (`docs/FEATURE_TRACKER.md` §1) ships. This is not a defect to report — it is exactly what the sign-up screen's own copy now warns will happen | **PASS** — exercised, and it did not sign anyone in |

---

### The returning path

| # | Criterion | Expect exactly | Result |
|---|---|---|---|
| 7 | **A bound chat gets a sign-in link, not a sign-up link** — sign out, press **Continue with Telegram**, press **Start** | `Tap to sign in to Hearth:` · `http://localhost:5173/sign-in/magic?token=<token>` · `This link works once, for 15 minutes.` | **PASS** — the bot sent a **sign-in** link, not a create-household link. This is the discriminating result: an unbound chat would have minted a second signup and could have created a second household; instead `Accounts.ByChatID` found the binding and took the sign-in branch. Database afterwards: `magic_links = 1`, `consumed = 1`, `tg_signups` still `1`, `telegram_accounts` still `1`, `households` still `6` |
| 8 | **It signs in the same user** — tap it | Signed in — same display name, same household, same data | **PASS** — tapping it signed in |
| 9 | **The magic link is single-use** — sign out, tap the same link again | Refused | **PASS** — the second tap of the spent link was refused. Nonce accounting confirmed the single-use behaviour against the real table: 3 minted, 1 consumed |

---

### The refusal paths

| # | Criterion | Expect exactly | Result |
|---|---|---|---|
| 10 | **An expired nonce is refused.** Press **Continue with Telegram** and do **not** press Start. Wait **11 minutes** (the TTL is 10 — do not cut it fine). Then press Start | Word for word: *"That sign-in link has expired. Start again from the app."* | **PASS** — recorded from the walk's overall result; no separate note was supplied for this criterion |
| 11 | **The per-chat limit delivers three and refuses the fourth.** Per trap 5, mint a fresh nonce for each: press **Continue with Telegram** → **Start**, four times | Links on attempts 1, 2 and 3. **No link on the fourth** — the same message as criterion 10, word for word | **PASS** — links delivered on attempts 1–3, the fourth refused with the identical wording |
| 12 | **An update that arrives while the API is down is processed exactly once on restart.** Order matters — per trap 5, `make down` first would leave no way to mint a fresh nonce, and the only nonce left to send would already be spent, which the bot correctly refuses (that refusal is not a FAIL of this criterion, it is criterion 10/11's territory reached by the wrong door). So: press **Continue with Telegram** in the browser first (mints a fresh nonce and opens the `t.me` link), *then* `make down`, *then* tap the `t.me` link in Telegram (sends the fresh `/start` while the API is stopped), *then* `make up` | **One** reply, and it is the ordinary one for that chat (a sign-in link if the chat is bound, a sign-up link if not) — **not** the refusal message, and not two replies. No panic in `docker compose logs api`, and the process still serving afterwards | **PASS** — recorded from the walk's overall result; no separate note was supplied for this criterion |

**Criterion 12 is deliberately not the redelivery test, and that is worth
stating so nobody records a pass for something they did not exercise.**
Pressing Start (or tapping the fresh `t.me` link) while the API is down never
delivers the update to anyone: it sits pending at Telegram, and the fresh
poller collects it once and handles it
normally. That is genuinely worth walking — it is the "an update arrived while
we were down" case an operator will actually meet — but it is not redelivery.
**Real redelivery needs the process to die after receiving a batch and before
its next `getUpdates` confirms it**, and the loop re-polls immediately after
dispatching, so that window is milliseconds wide and cannot be staged by hand.

What makes redelivery *safe* is not a walk step at all: a redelivered `/start`
carries a nonce that is already consumed, so `Links.Consume` returns
`domain.ErrNotFound` and the bot answers the refusal instead of signing anyone
in a second time. That branch is covered by
`TestHandleStartAnswersIdenticallyForEveryDeadNonce` in
`usecase/telegram_auth_test.go`. Note also what is **not** covered anywhere: the
design spec's testing section asked for a poller test simulating a restart, and
`poller_test.go` has none — offset advance, backoff, panic recovery and
cancellation are tested, redelivery is not. Recorded rather than assumed.

Criterion 11 needs one SQL check, or a spent nonce would look identical to a
refused one — which is the whole point of the two answering the same way:

```sql
SELECT count(*) FROM telegram_link_requests
 WHERE chat_id = <the chat id from criterion 5>
   AND consumed_at > now() - interval '1 hour';
-- expect 4 at the refusal: three delivered, the fourth spent and refused.
```

---

### The off switch

| # | Criterion | Expect exactly | Result |
|---|---|---|---|
| — | **The feature fails closed when unconfigured, even though the button itself does not know that yet.** Remove both lines from `.env`, `make down && make dev`, reload the sign-in screen, press **Continue with Telegram** | The button **is present** on load — `SignInScreen.tsx`'s `telegramUnavailable` starts `false` and there is no mount-time probe, so nothing hides the button before it is clicked — but the click gets a `404` and the button then **disappears with no visible error message**. Reload the screen and it **returns**, because that hidden state lives only in the component instance, not in anything persisted. This is expected, not a defect: `docs/SYSTEM_DESIGN.md` §7 and the `/auth/telegram/start` row in §4's route matrix describe exactly this — "the frontend hides the control on that response" (a `404` response, i.e. after a click) — so the real off switch is `curl -X POST …/api/v1/auth/telegram/start` answering `404`, not the button's absence on load | **PASS** — exercised; no separate note was supplied beyond the pass |

Cheap, and it closes the loop on the `404` contract: an install without a bot
gives nothing away, because `404` is the same answer any unrouted path gets.
Put the lines back afterwards if you want to keep using it.

---

**Score: 12 of 12 numbered criteria pass, plus all five unnumbered checks.**
No product defect needed a code fix anywhere in this walk.

---

## When every criterion passes

**Done, 2026-09-01 — all three edits made in the same change as this file's
own Result cells.** The instructions below are kept verbatim as the record
of what was done and why, not as an outstanding checklist.

Three edits, and nothing else. **Recount, do not adjust by delta** —
`docs/FEATURE_TRACKER.md` records that adjusting in place has produced wrong
numbers before.

1. **`docs/FEATURE_TRACKER.md` §1** — *Telegram sign-up* and *Telegram
   sign-in* move 🟡 → ✅, and their gap sentences are replaced with what this
   walk confirmed, and its date.
2. **`docs/FEATURE_TRACKER.md` totals** — Entry and authentication becomes
   `12 / 1 / 2 / 0`. The `2` not started is *Telegram invites* (unchanged)
   plus the fix wave's own new row, *attach an email address to a
   Telegram-only account* — that one stays ⬜ after this walk passes; it
   needs a settings-page flow, not a browser walk, to close. The totals
   table becomes `73 / 15 / 21 / 2 = 111`, and the headline stays
   **88 of 111**, because a 🟡 and a ✅ count the same there. That is
   precisely why the gap had to be named in the cell rather than left to
   the headline. Add a short recount paragraph above the table, as every
   prior update in that file does.
3. **`docs/SYSTEM_DESIGN.md`** scope paragraph and **ADR 4**'s status line —
   both currently say the feature has never been walked. Replace with what
   this walk found, and link back here.

**If the walk finds a defect:** it goes in `docs/LEARNING.md` with what would
have caught it sooner, the rows stay 🟡, and the criterion above records FAIL
with the symptom rather than being quietly re-run after a fix.
