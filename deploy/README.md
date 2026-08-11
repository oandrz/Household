# Deploying and operating Hearth

The reasoning lives in `docs/adr/0001-optimise-for-exit-cost.md`,
`docs/adr/0002-first-production-host.md` and
`docs/superpowers/specs/2026-08-10-hearth-production-deployment-design.md`.
This file is the operations surface only.

Every command below runs from this directory (`~/Household/deploy`) on the box.

## Deploying a change

1. Merge to `main` and wait for the `images` workflow to go green.
2. `ssh box && cd Household/deploy`
3. `git pull` — only needed if the Compose file, Caddyfile or scripts changed.
4. Set the new tag:

   ```bash
   sed -i "s/^IMAGE_TAG=.*/IMAGE_TAG=<the git sha>/" .env
   ```

   **Never `latest`.** A migration-only change produces a byte-identical api
   image; without a changed tag Compose will not recreate `api`, will not
   re-evaluate `depends_on: migrate`, and the migration silently will not run.

5. ```bash
   docker compose -f docker-compose.prod.yml pull
   docker compose -f docker-compose.prod.yml up -d
   ```

   `migrate` runs to completion before `api` starts. If it fails, `api` does
   not start and the previous container keeps serving.

6. ```bash
   curl -fsS "https://$(grep '^DOMAIN=' .env | cut -d= -f2 | tr -d '"')/readyz" && echo
   docker compose -f docker-compose.prod.yml ps
   ```

   Confirm nothing is restarting.

## Rolling back

Set `IMAGE_TAG` to the previous SHA and repeat steps 5–6.

**This rolls back code only.** It does not undo a migration: the goose `Down`
migrations are correct by inspection and no test has ever run one. A bad
migration is a restore from backup, not a rollback — which is why deploys are
manual and watched.

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

`--email` is effectively required on `unlock-household` here: its default
resolves the *seeded* owner, and production is never seeded.

`adminctl seed` refuses outside development and refuses a non-local database,
checked before it opens a connection. It cannot damage this install.

**If any `adminctl` command above (not `goose`) exits with `SMTP_USERNAME and
SMTP_PASSWORD must both be set, or both left empty`**, that is `.env`, not a
lockout: config loading refuses before any subcommand runs, so a `.env` with
one of that pair filled and the other blank breaks every `adminctl` command,
not just mail-sending ones. Fix `.env`, then retry.

## Backups

A host cron runs `backup.sh` nightly:

```cron
17 3 * * * AGE_RECIPIENT=age1… RCLONE_REMOTE=r2:hearth-backups HC_PING_URL=https://hc-ping.com/… /home/deploy/Household/deploy/backup.sh >> /var/log/hearth-backup.log 2>&1
```

A missed night emails you, via the heartbeat. That is the only monitoring the
backups have, and it is the one that matters: the failure mode is silence.

## Restoring

`restore.sh` never touches the live database directly — it always writes into
a target you name, so rehearse it against a throwaway container:

```bash
rclone copyto r2:hearth-backups/hearth-2026-08-10.sql.gz.age /tmp/b.age

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

| | |
|---|---|
| Compose project | `hearth` |
| Data | Docker volume `hearth-pgdata` |
| Certificates | Docker volume `caddy-data` |
| Caddy runtime state (OCSP staples, etc.) | Docker volume `caddy-config` |
| Secrets | `deploy/.env`, mode 600, gitignored |
| Registry | `ghcr.io/oandrz/hearth-{api,web,admin}` |
| Logs | `docker compose -f docker-compose.prod.yml logs -f <service>` |
