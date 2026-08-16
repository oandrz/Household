# Deploying and operating Hearth

The reasoning lives in `docs/adr/0001-optimise-for-exit-cost.md`,
`docs/adr/0002-first-production-host.md` and
`docs/superpowers/specs/2026-08-10-hearth-production-deployment-design.md`.
This file is the operations surface only — running the box that exists.

- **Rebuilding the box from nothing:** `PROVISION.md`.
- **Every external service, cost and credential location:**
  `../docs/INFRASTRUCTURE.md`.

Every command below runs from this directory (`~/Household/deploy`) on the box.

## First install

The box sparse-checks out `deploy/` only, so this directory is the whole of
what a rebuild has to work from. Everything needed to fill in `.env` is
therefore written here rather than in the deployment plan, which never reaches
the box.

```bash
cp .env.example .env
chmod 600 .env      # it holds the database password
```

Then fill in the values below — and **only** those. Everything else in the
template already ships correct for this install: `DOMAIN`, `APP_BASE_URL`,
`APP_ENV`, `PORT`, the three `ARGON2_*` values and the entire `SMTP_*` block are
right as written and must be left alone.

| Value | What to put there |
|---|---|
| `IMAGE_TAG` | The git SHA the `images` workflow built. **Never `latest`** — see step 4 of "Deploying a change" for why |
| `ACME_EMAIL` | Where Let's Encrypt sends expiry warnings |
| `POSTGRES_PASSWORD` | `openssl rand -hex 24`. **Hex, not base64** — this value is substituted into two `postgres://` DSNs, and base64's `/`, `+` and `=` are all significant inside a URL userinfo field. The failure is an authentication error at boot that looks nothing like a quoting problem |
| `DATABASE_URL` and `GOOSE_DBSTRING` | The **same** password inside both DSNs — replace the two `CHANGEME`s |

**The password appears three times and all three must match.**
`POSTGRES_PASSWORD` initialises the database; `DATABASE_URL` is how `api`
connects; `GOOSE_DBSTRING` is how `migrate` connects. A mismatch in the second
or third fails loudly at boot, which is the good case.

**Leave `SMTP_USERNAME` and `SMTP_PASSWORD` blank, and leave `SMTP_TLS_MODE=none`
where it is.** Mail does not leave this box — `docs/adr/0003-mail-stays-on-the-box.md`
carries the reasoning and "Reading mail" below is how you collect it. Those three
lines are what aim the mailer at the `mailpit` service, and each has a different
failure mode:

- Fill **only one** of `SMTP_USERNAME` / `SMTP_PASSWORD` and `config.Load()`
  refuses: `api` exits, the `unless-stopped` policy restart-loops it, `/readyz`
  502s, and **every `adminctl` command breaks with it**, because config loading
  runs before any subcommand. Loud, and the break-glass section below names the
  error.
- Delete `SMTP_TLS_MODE=none` and every send fails **silently**. It defaults to
  `mandatory` outside development, Mailpit speaks plain SMTP, and the send is
  fire-and-forget — nothing surfaces in any status, any check or any response.
  **The install looks completely healthy and mails nothing**, and mail is the
  only account-recovery path this product has, so nobody can sign in and nobody
  can be invited.

So after the first `up`, do not treat a green `/readyz` as proof. Sign up once
through the real form and confirm the message actually lands in Mailpit.

## Deploying a change

1. Merge to `main` and wait for the `images` workflow to go green.
2. `ssh box && cd Household/deploy`
3. `git pull` — only needed if the Compose file, Caddyfile or scripts changed.
4. ```bash
   ./deploy.sh <the git sha>
   ```

That is the whole deploy. The script writes `IMAGE_TAG`, pulls, brings the
stack up, and then **verifies rather than assumes**: that all three images
existed before it touched `.env`, that `migrate` exited `0`, that nothing is
restart-looping, and that `/readyz` answers on the public domain. Any of those
failing stops it with a message naming the next command to run.

```bash
./deploy.sh --latest      # deploy whatever is newest on origin/main
./deploy.sh --current     # what is running right now
./deploy.sh --rollback    # back to the tag deploy.sh last replaced
```

**`--latest` is a convenience, not a second mode.** It resolves `origin/main` to
a real commit SHA and deploys *that*, so `.env` still records the exact commit —
`--rollback` still works and `--current` still names precisely what is live. It
exits early and does nothing if that commit is already running.

**Why the tag is a SHA and never the string `latest`.** A change that only
touches SQL produces a byte-identical `api` image. Without a changed tag Compose
sees nothing new, does not recreate `api`, does not re-evaluate
`depends_on: migrate`, and **the migration silently does not run** — while
`/readyz` stays green. New code expectations against an old schema, with no
signal. The SHA is also what makes rollback and "what is running?" answerable at
all.

**Why a script rather than four commands.** Two of the four failed silently:

- `sed -i "s/^IMAGE_TAG=.*/.../" .env` run from the wrong directory edits
  nothing, prints nothing and exits `0`. The deploy then "succeeds" while
  serving the old build.
- `IMAGE_TAG=latest` deploys nothing while looking like it worked. A
  migration-only change produces a byte-identical `api` image, so Compose does
  not recreate `api`, does not re-evaluate `depends_on: migrate`, and **the
  migration does not run** — with every check still green.

Both are refused before anything changes, and `latest` gets its own message
because the reason is not guessable from the symptom.

The by-hand equivalent is still `sed` the tag, then `docker compose pull`, then
`docker compose up -d` — worth knowing for the day the script itself is what is
broken.

## Rolling back

```bash
./deploy.sh --rollback
```

It reads `.previous-image-tag`, which `deploy.sh` writes **before** changing
anything, so rollback works even if a deploy died partway. To go further back
than one release, pass the SHA directly: `./deploy.sh <older sha>`.

**This rolls back code only.** It does not undo a migration: the goose `Down`
migrations are correct by inspection and no test has ever run one. A bad
migration is a restore from backup, not a rollback — which is why deploys are
manual and watched.

**If a migration fails, expect the new SPA against the old API.** `api` declares
`depends_on: migrate` and so refuses to start, keeping the previous container
serving — that is the guarantee, and it holds. But `web` declares only
`depends_on: api`, not `depends_on: migrate`, so nginx comes up with the new
bundle regardless. That is correct behaviour and deliberate: `web` is static
files that never touch the database, and blocking it would take the whole site
down over a failure that only affects the API.

What it looks like undiagnosed is a site that loads, looks new, and returns
stale or failing data from endpoints the new bundle expects — which reads as a
frontend bug rather than a stopped migration. So when anything looks wrong after
a deploy, check `docker compose -f docker-compose.prod.yml ps` for `migrate`
first: `exited (0)` is success, any other exit code is the answer. Roll
`IMAGE_TAG` back and both halves return to the previous version together.

## Reading mail

Mail does not leave this box. `docs/adr/0003-mail-stays-on-the-box.md` has the
reasoning; operationally it means every sign-up link, invite and magic link lands
in Mailpit, and you read it by hand.

Mailpit is bound to loopback, so open a tunnel from your laptop:

```bash
ssh -L 8025:127.0.0.1:8025 deploy@<box>
```

Leave that running, then open <http://localhost:8025> in your browser. Newest
message at the top; click it and click the link inside.

**This inbox is a complete authentication bypass.** Every magic link in it grants
full access to an account with no password. That is why port 8025 is published as
`127.0.0.1:8025` and not `8025` — bound to `0.0.0.0` it would hand every account
on this install to anyone who scans the box. Never "simplify" that prefix away,
and never put Mailpit behind Caddy.

The API reaches Mailpit at `mailpit:1025` over the Compose network. No SMTP port
is published.

Faster than the browser, if you just want the newest link:

```bash
curl -s http://localhost:8025/api/v1/messages?limit=1
curl -s http://localhost:8025/api/v1/message/<ID>
```

To send someone else their link — Christine, or anyone you invite — copy it out
of Mailpit and pass it to them however you normally talk. The link is
single-use and time-limited, so treat it like a password while it is in transit.

**When a real domain arrives**, four values in `.env` change together
(`SMTP_ADDR`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`), the
`SMTP_TLS_MODE=none` line is deleted, and the `mailpit` service comes out of the
Compose file. No application code changes.

## Break-glass

The API image has no shell. These run from the admin image:

```bash
C="docker compose -f docker-compose.prod.yml"

# Clear a household's lockout without waiting 15 minutes.
$C run --rm admin /app/adminctl unlock-household --email=someone@example.com

# Set a member's password. Prompts on stdin, so -it is required — without it
# there is no controlling terminal to read from and the command fails with
# "inappropriate ioctl for device".
$C run --rm -it admin /app/adminctl reset-password --email=someone@example.com

# Invite a member from the command line.
$C run --rm admin /app/adminctl create-invite \
  --email=new@example.com --name=Name --role=limited \
  --capabilities=money --inviter-email=owner@example.com

# Retention: delete consumed/expired signups and stale login attempts.
$C run --rm admin /app/adminctl prune --older-than=30

# Migration status. `admin` only gets GOOSE_DBSTRING from .env — GOOSE_DRIVER
# and GOOSE_MIGRATION_DIR are set for the `migrate` service, not this one — so
# both must be supplied here, or goose prints its usage block and exits 1
# instead of a status table (proven while building this image: there is no
# driver-less "list local migration files" mode).
$C run --rm -e GOOSE_DRIVER=postgres -e GOOSE_MIGRATION_DIR=/app/migrations \
  admin /app/goose status
```

**`--email` on `unlock-household` and `--inviter-email` on `create-invite` are
both effectively required here.** Both default to the *seeded* owner
(`api/cmd/adminctl/main.go:274` and `:301`), and production is never seeded —
omit either in production and the command fails with `no account for
"andreas@hearth.family"`, not a helpful error about a missing flag. On
`create-invite` this is easy to lose: it is the fifth flag on a wrapped line,
and trimming it under stress still parses, so nothing about the command looks
wrong until it runs.

`adminctl seed` refuses outside development and refuses a non-local database,
checked before it opens a connection. It cannot damage this install.

**If any `adminctl` command above (not `goose`) exits with `SMTP_USERNAME and
SMTP_PASSWORD must both be set, or both left empty`**, that is `.env`, not a
lockout: config loading refuses before any subcommand runs, so a `.env` with
one of that pair filled and the other blank breaks every `adminctl` command,
not just mail-sending ones. Fix `.env`, then retry.

## Backups

**Object naming and the two R2 rules.** Backups are `hearth-<UTC
timestamp>.sql.gz.age` — timestamped to the second, not just the date, so no
two runs ever collide. That is a requirement of the bucket lock below, which
refuses overwrites as well as deletes.

Two rules are configured on the bucket, and their order matters:

| Rule | Setting | Why |
|---|---|---|
| Object lifecycle | delete after **90 days** | Keeps the bucket inside R2's free 10 GB forever |
| Bucket lock | retention **30 days** | Objects are immutable for a month, so a compromised box cannot erase backup history |

**Never set the lock to indefinite retention.** Locks take precedence over
lifecycle rules, so an indefinite lock would block the 90-day expiry
permanently and the bucket could never be emptied. 30 < 90 is what lets the two
coexist.

The lock exists because R2 has no write-without-delete token — the credential
on the box can delete objects, so the lock is the only server-side floor under
the backup history.



A host cron runs `backup.sh` nightly. It is the **`deploy` user's** crontab —
`crontab -e` as `deploy`, not `sudo crontab -e`:

```cron
17 3 * * * AGE_RECIPIENT=age1… RCLONE_REMOTE=r2:hearth-backups HC_PING_URL=https://hc-ping.com/… /home/deploy/Household/deploy/backup.sh >> /home/deploy/hearth-backup.log 2>&1
```

**The log path is under `/home/deploy` deliberately.** On current Ubuntu
`/var/log` is `drwxrwxr-x root:syslog` and `deploy` is in neither, so a
redirect into `/var/log` fails in `/bin/sh` *before `backup.sh` is ever
reached* — no dump, no upload, no ping, and an error that names the redirection
rather than anything about backups.

**The `deploy` user is also why `rclone config` must be run as `deploy`.**
`rclone` reads `$HOME/.config/rclone/rclone.conf`; a remote configured as root
is invisible to a cron running as `deploy`, and fails the same silent way.

A missed night emails you, via the heartbeat. That is the only monitoring the
backups have, and it is the one that matters: the failure mode is silence.

## Restoring

`restore.sh` never touches the live database directly — it always writes into
a target you name, so rehearse it against a throwaway container:

```bash
# Backups are named hearth-<UTC timestamp>.sql.gz.age and sort chronologically,
# so this is always the newest one.
NEWEST=$(rclone lsf r2:hearth-backups | sort | tail -1)
rclone copyto "r2:hearth-backups/$NEWEST" /tmp/b.age

# A throwaway target to restore into — restore.sh's guard (below) refuses
# anything that looks like the live database, so this always has to exist first.
docker run -d --name pg-restore -p 55432:5432 \
  -e POSTGRES_USER=hearth -e POSTGRES_PASSWORD=drill -e POSTGRES_DB=restored \
  postgres:17-alpine

./restore.sh /tmp/b.age "postgres://hearth:drill@localhost:55432/restored?sslmode=disable"

# Done inspecting it? Tear it down.
docker rm -f pg-restore
```

`restore.sh` runs `psql` through `docker run --rm -i --network host
postgres:17-alpine`, not a host binary — the box installs only Docker, `age`
and `rclone`, and a client older than the dump's v17 server would be reading
forward. `--network host` is why `localhost:55432` above resolves to the port
the container just published.

**The live-database guard is fail-closed.** It parses the target DSN and
refuses to proceed unless *both* of these hold: the database name is not
`hearth`, and the host is not `postgres` (the compose service name). Anything
it cannot even parse as a DSN is refused too — an unrecognised shape does not
fall through. So a legitimate drill just needs a database name other than
`hearth` and a host other than the literal string `postgres`; `localhost`, an
IP, or another hostname are all fine, which is why the example above uses
`restored` and `localhost`.

**Rehearse this with the escrowed copy of the key, not your own.** An escrow
that has never been used is not an escrow; it is a hope. Pass it as the third
argument: `./restore.sh /tmp/b.age "<dsn>" /path/to/escrowed/hearth.key`. Omit
it and `restore.sh` defaults to `$HOME/.config/age/hearth.key` — the box's own
key, not the escrow, so an escrow drill must always pass this argument
explicitly.

## What is where

Names below are the **resolved** ones — what `docker volume ls` prints and what
you type. Compose prefixes every resource with the project name, so the
`hearth-pgdata:` declared in the Compose file is really `hearth-prod_hearth-pgdata`.

| | |
|---|---|
| Compose project | `hearth-prod` |
| Data | Docker volume `hearth-prod_hearth-pgdata` |
| Certificates | Docker volume `hearth-prod_caddy-data` |
| Caddy runtime state (OCSP staples, etc.) | Docker volume `hearth-prod_caddy-config` |
| Network | `hearth-prod_hearth`, subnet `172.28.0.0/16` (named in `web/nginx.conf`'s `set_real_ip_from`; the two move together) |
| Secrets | `deploy/.env`, mode 600, gitignored |
| Registry | `ghcr.io/oandrz/hearth-{api,web,admin}` |
| Logs | `docker compose -f docker-compose.prod.yml logs -f <service>` |
| Backup log | `/home/deploy/hearth-backup.log` |

**Why `hearth-prod` and not `hearth`:** the development stack in the repo root
declares `name: hearth` and its own `hearth-pgdata` volume, so before this
rename both files resolved to the same volume. The prefix is what makes
`backup.sh` fail loudly when it is run from the wrong directory instead of
quietly uploading a development database to the production bucket. The Compose
file carries the full reasoning at the top.
