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
| **Let's Encrypt** | TLS certificates, via Caddy's ACME HTTP-01 | $0 | Certificates stop renewing after ~90 days. Caddy can point at another ACME CA |
| **GitHub + GHCR** | Source, CI, and the three container images | $0 — public repo | No new deploys. The running box is unaffected; images already pulled keep serving |
| **healthchecks.io** | "Did the backup run?" heartbeat | $0 | Backups could stop silently. They would still be *running* — you just would not be told when they stopped |
| **UptimeRobot** | "Is the site up?" — keyword check on `/readyz` | $0 | Outages go unnoticed until someone opens the site |

**Total ≈ $7.70/month.** Everything except the server is free tier, and at this
data volume will remain so for years.

## The credentials, and where they live

**Nothing in this table is in this repository.**

| Secret | Lives | Recoverable? |
|---|---|---|
| **`age` private key** | The operator's laptop at `~/.config/age/hearth.key`, and **on paper** in the escrow envelope | **No. Never. There is no reset.** Lose both copies and every backup is permanently unreadable |
| R2 access key + secret | `~/.config/rclone/rclone.conf` on the box, mode 600 | Yes — delete the token in Cloudflare and issue a new one |
| `POSTGRES_PASSWORD` | `deploy/.env` on the box, mode 600 | Yes, sort of — it is only needed to talk to the *existing* database. A restore into a fresh Postgres uses a new one |
| healthchecks.io ping URL | `/home/deploy/hearth-backup.env` on the box, mode 600 | Yes — create a new check |
| SSH private key | The operator's laptop, `~/.ssh/hearth_prod` | Yes — replace it through Hetzner's console |
| Hetzner / Cloudflare / Dynu / GitHub logins | The operator's password manager | Yes — all send password resets to the operator's email |

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

**Mail does not leave the box at all** ([ADR 3](adr/0003-mail-stays-on-the-box.md)).
Sign-up links, invites and magic links land in Mailpit and are read by hand over
an SSH tunnel. Mailpit's UI is bound to `127.0.0.1:8025` and never `0.0.0.0`,
because that inbox is a complete authentication bypass — every link in it grants
an account with no password.

## Known gaps

- **The install is two-person.** Nobody outside can sign up or recover an account
  without the operator relaying a link out of Mailpit by hand. ADR 3's exit
  condition is the day a third person needs an email, and the fix is a ~US$11/year
  domain plus four values in `.env` — no code changes.
- **One box, no redundancy** — accepted; see [ADR 2](adr/0002-first-production-host.md).
  An hour of downtime is an inconvenience, not an incident.
