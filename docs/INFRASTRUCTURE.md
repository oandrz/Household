# Infrastructure — every external thing Hearth depends on

What exists outside this repository, what it costs, and **what breaks if it goes
away**. Written 2026-08-15, when the first production install went live.

`deploy/PROVISION.md` rebuilds the box. This file is the map of everything the
box depends on that is not the box.

No secrets are in this file, or in this repository. Where a credential is
needed, this says where it lives.

---

## The services

| Service | What it does | Cost | If it disappears |
|---|---|---|---|
| **Hetzner Cloud** | The server. CX23, Falkenstein, Ubuntu 26.04, `5.75.239.188` | ~$7.07/mo + ~$0.60 IPv4 | The site is down. Rebuild elsewhere with `deploy/PROVISION.md` and restore the newest backup. Data loss is bounded by the last nightly dump |
| **Cloudflare R2** | Backup storage, bucket `hearth-backups` | $0 — 10 GB free tier | **No new backups, and the existing ones are gone.** The most serious entry here. Point `RCLONE_REMOTE` at any other S3-compatible store; nothing else changes |
| **Dynu** | DNS for `oink.mywire.org` (free DDNS hostname) | $0 | The site is unreachable by name and TLS renewal fails. Also blocks mail permanently — see [ADR 3](adr/0003-mail-stays-on-the-box.md) |
| **Telegram** (Bot API) | The second delivery channel for sign-in and sign-up links, when a bot is configured. Outbound only — the API long-polls `api.telegram.org`; there is no webhook and nothing new faces the internet | $0 — free, with no per-message charge and no domain, DNS record, business verification or legal entity required. That is the whole reason it was chosen over WhatsApp and SMS ([ADR 4](adr/0004-telegram-as-a-second-delivery-channel.md)) | **Telegram sign-in stops. Email sign-in is unaffected, and sessions already issued are unaffected** — no session depends on Telegram once it exists, because Telegram only ever carried the link. `POST /api/v1/auth/telegram/start` answers `404` and the sign-in screen hides the control, so the degradation is a missing button rather than a broken one. **Not on the box today** — the change is not deployed there, and `deploy/.env` sets neither value, so this row describes a dependency the product *can* take, not one it currently has |
| **Let's Encrypt** | TLS certificates, via Caddy's ACME HTTP-01 | $0 | Certificates stop renewing after ~90 days. Caddy can point at another ACME CA |
| **GitHub + GHCR** | Source, CI, and the three container images | $0 — public repo | No new deploys. The running box is unaffected; images already pulled keep serving |
| **healthchecks.io** | "Did the backup run?" heartbeat | $0 | Backups could stop silently. They would still be *running* — you just would not be told when they stopped |
| **UptimeRobot** | "Is the site up?" — keyword check on `/readyz` | $0 | Outages go unnoticed until someone opens the site |

**Total ≈ $7.70/month, unchanged by Telegram.** Everything except the server is
free tier, and at this data volume will remain so for years. Telegram is the
only row here with no free-tier *ceiling* to eventually cross — there is no
per-message price to grow into, which is exactly the property WhatsApp lost on
1 October 2026 and SMS never had.

## The credentials, and where they live

**Nothing in this table is in this repository.**

| Secret | Lives | Recoverable? |
|---|---|---|
| **`age` private key** | The operator's laptop at `~/.config/age/hearth.key`, and **on paper** in the escrow envelope | **No. Never. There is no reset.** Lose both copies and every backup is permanently unreadable |
| R2 access key + secret | `~/.config/rclone/rclone.conf` on the box, mode 600 | Yes — delete the token in Cloudflare and issue a new one |
| `POSTGRES_PASSWORD` | `deploy/.env` on the box, mode 600 | Yes, sort of — it is only needed to talk to the *existing* database. A restore into a fresh Postgres uses a new one |
| healthchecks.io ping URL | `/home/deploy/hearth-backup.env` on the box, mode 600 | Yes — create a new check |
| SSH private key | The operator's laptop, `~/.ssh/hearth_prod` | Yes — replace it through Hetzner's console |
| `TELEGRAM_BOT_TOKEN` | `deploy/.env` on the box, mode 600, beside `POSTGRES_PASSWORD` (and `.env` locally — gitignored, never committed). Placeholder lines only in `.env.example` and `deploy/.env.example` | Yes — in Telegram, `@BotFather` → `/mybots` → the bot → **API Token** → **Revoke current token** issues a new one. It travels with `TELEGRAM_BOT_USERNAME`, which is not a secret; `config.Load` refuses to boot if exactly one of the two is set |
| Hetzner / Cloudflare / Dynu / GitHub logins | The operator's password manager | Yes — all send password resets to the operator's email |

**There are two ways to turn Telegram off, and they degrade differently.**
Reach for the second unless you are actually responding to a leaked token.

1. **Revoke the token** (above). Immediate, needs no deploy, and stops the bot
   answering anyone — but it leaves the product *looking* like the feature
   works: `POST /auth/telegram/start` still answers `200`, the **Continue with
   Telegram** button is still on the sign-in screen, and a person who presses it
   gets a `t.me` link, presses Start, and is met with silence. The poller also
   logs a warning every backoff cycle, indefinitely.
2. **Remove both `.env` values and restart.** This is the clean switch: the
   route answers `404`, the frontend hides the control on that response, and
   nothing is offered that cannot work. Slower — it needs a restart — but it is
   the one that leaves the product honest.

**The hierarchy that matters:** every row above except the first can be
recovered by someone who can read the operator's email. The `age` key cannot be
recovered by anyone, ever. That asymmetry is why it, and nothing else, is the
thing printed on paper.

## The escrow envelope

A printed page held outside this machine, for the case where the operator is
unavailable. It carries the `age` private key, an explanation a non-technical
person can act on, the list of services above, and restore instructions for
whichever technical person they hand it to.

**It has been tested** (2026-08-15): the key was typed out fresh from the paper
and used to restore a real backup, reproducing all eleven tables and every
monetary value from live production. An escrow nobody has ever decrypted with is
a hope, not an escrow.

Regenerate it with the procedure in
`docs/superpowers/plans/2026-08-10-hearth-production-verification.md`, criterion
12. **Reprint and re-test it whenever the `age` key changes.**

## The two R2 rules, and why the numbers are not arbitrary

| Rule | Setting | Purpose |
|---|---|---|
| Object lifecycle | delete after **90 days** | Keeps the bucket inside the free 10 GB forever |
| Bucket lock | retention **30 days** | Objects immutable for a month — a compromised box cannot erase backup history |

**30 must stay below 90.** Locks take precedence over lifecycle rules, so a lock
longer than the expiry would block pruning permanently and leave a bucket that
can never be emptied. Never set indefinite retention here.

**Why a lock at all:** R2 has no write-without-delete token (Backblaze B2 does —
that was the main argument for it). The credential on the box therefore *can*
delete objects, so a server-side lock is the only floor under the backup history.

**Verified on the box on 2026-08-15, not read off the dashboard.** A delete of
an existing backup was refused (`failed to delete 1 files`, object still
present) and a fresh `backup.sh` run uploaded normally in the same minute. The
second half matters as much as the first: a lock that blocked *writes* would
have killed the nightly cron silently, with a missing heartbeat as the only
symptom.

This is also why `backup.sh` timestamps objects to the second rather than the
day — bucket locks refuse overwrites as well as deletes, so two runs on one day
would collide against a locked object.

## What runs on a schedule

| When | What | Where |
|---|---|---|
| **19:17 UTC** nightly (= 03:17 Asia/Singapore) | `backup.sh` — dump, gzip, `age`, upload to R2, then ping the heartbeat | `deploy` user's crontab; see `deploy/crontab.example` |
| Every 5 minutes | UptimeRobot polls `/readyz` for the keyword `ready` | UptimeRobot |
| ~Every 60 days | Caddy renews the TLS certificate | Inside the `caddy` container |
| Never automatically | **Deploys.** The box does not update itself | `deploy/deploy.sh`, run by a human |

**The schedule is written in UTC, deliberately.** Ubuntu's cron (vixie 3.0pl1)
**ignores `CRON_TZ`** — proven on this box on 2026-08-16 by scheduling a probe
in Singapore time under `CRON_TZ` and watching it not fire, then repeating it in
plain UTC and watching it fire. An earlier crontab set `CRON_TZ=Asia/Singapore`
and carried a comment claiming the schedule read as local time. It did not: the
backup was really scheduled for 11:17 in the morning, and had not run once.

Singapore observes no daylight saving, so a fixed UTC time always means the same
local time. **A timezone with DST would drift by an hour twice a year** and would
need `systemd` timers, which do honour `OnCalendar` with a timezone.

## Data flows leaving the box

Worth knowing exactly, because the answer is short:

1. **Nightly to Cloudflare R2** — the whole database, `age`-encrypted. Cloudflare
   holds ciphertext and cannot read it.
2. **A heartbeat ping to healthchecks.io** — a URL fetch, no content.
3. **Certificate requests to Let's Encrypt** — the hostname only.
4. **Image pulls from GHCR**, on deploy.
5. **Telegram, if and only if a bot is configured** — and this one is different
   in kind from the four above it, so it is worth stating flatly:
   **Telegram sees every sign-in and sign-up link sent over it.** The others
   leak ciphertext, a hostname, or nothing. This one puts a third party on the
   authentication and recovery path, in the same way Mailpit's inbox already
   does for whoever can reach it — every link that travels this way grants an
   account. It is accepted for the same reason Mailpit is: the alternative on a
   free DDNS hostname is no delivery at all. What bounds it is that the links
   are short-lived and single-use (15 minutes for a magic link, 24 hours for a
   sign-up), that nothing else about the household ever crosses this channel —
   no balances, no names, no household id, only a URL — and that the chat is
   bound to one user, so a link Telegram delivers is one the account's own
   owner asked for. **Nothing flows this way on the live box today** — the
   change is not deployed there and no bot is configured — so this line
   describes a capability rather than traffic.

**Mail does not leave the box at all** ([ADR 3](adr/0003-mail-stays-on-the-box.md)).
Sign-up links, invites and magic links land in Mailpit and are read by hand over
an SSH tunnel. Mailpit's UI is bound to `127.0.0.1:8025` and never `0.0.0.0`,
because that inbox is a complete authentication bypass — every link in it grants
an account with no password.

## Known gaps

- **The install is two-person, and Telegram narrows that without closing it.**
  Nobody outside can sign up or recover an account without the operator relaying
  a link out of Mailpit by hand — *unless a Telegram bot is configured*, which
  on this box it is not. Configuring one (two `.env` values, a restart of a
  build carrying migration `00011`) makes **sign-up and magic-link recovery**
  self-service for a stranger with a Telegram account. It does **not** make
  **invites** self-service: invites deliberately stay on email in this slice,
  and notification preferences still send nothing at all
  ([ADR 4](adr/0004-telegram-as-a-second-delivery-channel.md)). ADR 3's exit
  condition was amended on 2026-09-01 for exactly this reason — "a third person
  needs an email" stopped being the trigger once a third person could arrive
  without one. The mail fix is unchanged: a ~US$11/year domain plus four values
  in `.env`, no code.
- **Telegram sign-in was walked on 2026-09-01 and passed**, against a real
  BotFather bot on a developer machine — both the stranger path (a household
  created from a chat, with a NULL-email owner and a bound `chat_id`) and the
  returning path (a bound chat receiving a magic link rather than a second
  sign-up). `docs/FEATURE_TRACKER.md` moved both rows to ✅ on that basis, and
  `docs/superpowers/plans/2026-09-01-telegram-sign-in-verification.md` records
  the result per criterion. **It has not been exercised against the production
  install**, which has no bot configured — the two `TELEGRAM_*` values in
  `deploy/.env` are what turn it on there.
- **One box, no redundancy** — accepted; see [ADR 2](adr/0002-first-production-host.md).
  An hour of downtime is an inconvenience, not an incident.
- **The platform admin surface (2026-09-02) adds no new external service, and
  this line exists so the next reader knows that was checked rather than
  forgotten.** `platform_admins`, `feature_flags`, `household_feature_flags`,
  `admin_audit_log` and `admin_reauth_attempts` are five tables in the same
  Postgres this box already runs; `adminctl`'s four new commands are the same
  binary this box already deploys. Nothing in "The services" table above
  changes, and nothing new leaves the box. **Not on the box today** — the
  change is not deployed there, the same caveat the Telegram row above
  carries, for the same reason. Two panels the design spec describes but this
  slice does not build will each cost one new value here when they ship: the
  read-only database browse needs `DATABASE_READONLY_URL` (a second,
  `SELECT`-only Postgres role, reached over the connection already open to
  this same database — not a new service), and the outbound-mail inspector
  needs `MAILPIT_API_URL` (Mailpit already runs; this reads its existing HTTP
  API rather than adding anything). Both are free, and the design spec commits
  each panel, when it ships, to saying plainly it is unavailable while its
  value is unset rather than falling back to a wider connection — see
  [ADR 5](adr/0005-platform-admin-authorization.md).
