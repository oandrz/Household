# Building the box from nothing

This rebuilds a Hearth production host from an empty account. It is written
because **everything here was once configured by hand and existed only on the
box** — a host that cannot be rebuilt from this repository is a host you have
already half-lost.

`README.md` is the day-to-day runbook. This file is for the day the box is
gone, or a second one is needed.

It supersedes Task 7 of
`docs/superpowers/plans/2026-08-10-hearth-production-deployment.md`, which still
names a machine Hetzner has renamed out of existence and a mail relay
[ADR 3](../docs/adr/0003-mail-stays-on-the-box.md) removed. Where they disagree,
this file is what was actually done.

Accounts and costs are in [`../docs/INFRASTRUCTURE.md`](../docs/INFRASTRUCTURE.md).

---

## Before you start

You need: a Hetzner Cloud account, a Cloudflare account with R2 enabled, DNS
control for the hostname, a healthchecks.io account, an UptimeRobot account, and
the **`age` private key** for the existing backups — see the escrow section of
`../docs/INFRASTRUCTURE.md`. Without that key the old backups are unreadable and
this is a fresh install rather than a recovery.

## 1 · The server

Hetzner Cloud → **CX23, Falkenstein, Ubuntu 26.04 LTS**, SSH key attached at
creation. Do not tick Backups (a paid Hetzner add-on; `backup.sh` is the real
backup and lives off-provider).

**Attaching a key at creation is not the same as disabling password auth** —
Hetzner's image ships `PasswordAuthentication yes` in a cloud-init drop-in.
Step 4 fixes that.

## 2 · Firewall — cloud-side, not on the host

Hetzner Cloud firewall, applied to the server:

```
inbound:  tcp/22  tcp/80  tcp/443
outbound: all
```

Outside the box, so a misconfigured host cannot open itself. Verify from your
laptop — a success here means Postgres is exposed and something is very wrong:

```bash
nc -z -G 4 <ip> 5432    # expected: refused/timeout
```

## 3 · Base packages and Docker

```bash
ssh root@<ip>
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq ca-certificates curl age git unzip

install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo $VERSION_CODENAME) stable" \
  > /etc/apt/sources.list.d/docker.list
apt-get update -qq
apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
```

The codename comes from `$VERSION_CODENAME` rather than being hardcoded, so this
survives the next Ubuntu. Docker publishes for `resolute` (26.04) — verified
before choosing that release.

**Do not `apt-get install rclone`.** See step 5.

## 4 · The `deploy` user, and closing password auth

```bash
id deploy >/dev/null 2>&1 || adduser --disabled-password --gecos "" deploy
usermod -aG docker deploy

install -d -m 700 -o deploy -g deploy /home/deploy/.ssh
cp /root/.ssh/authorized_keys /home/deploy/.ssh/authorized_keys
chown deploy:deploy /home/deploy/.ssh/authorized_keys
chmod 600 /home/deploy/.ssh/authorized_keys

cp sshd-hardening.conf /etc/ssh/sshd_config.d/00-hearth-hardening.conf
sshd -t && systemctl reload ssh
```

**The `00-` prefix is load-bearing, not tidiness.** Ubuntu includes
`sshd_config.d/*.conf` at the *top* of `sshd_config` and sshd takes the **first**
value it sees for a keyword, so this must sort ahead of `50-cloud-init.conf`.

Verify both directions before you log out:

```bash
ssh -o BatchMode=yes -i ~/.ssh/hearth_prod root@<ip> 'echo key auth ok'
ssh -o PreferredAuthentications=password -o PubkeyAuthentication=no root@<ip> true
#   expected: Permission denied (publickey).
```

## 5 · `rclone` from upstream, and remove the distro copy

Ubuntu ships a four-year-old `rclone`. Backups are the one system where a silent
failure is unacceptable, so take the current release and verify its checksum:

```bash
cd /tmp
V=v1.75.0        # check https://github.com/rclone/rclone/releases for current
BASE="https://github.com/rclone/rclone/releases/download/${V}"
curl -fsSLO "${BASE}/rclone-${V}-linux-amd64.zip"
curl -fsSLO "${BASE}/SHA256SUMS"
grep "rclone-${V}-linux-amd64.zip" SHA256SUMS | sha256sum -c -     # must print OK
unzip -oq "rclone-${V}-linux-amd64.zip"
install -m 0755 "rclone-${V}-linux-amd64/rclone" /usr/local/bin/rclone
apt-get remove -y -qq rclone 2>/dev/null || true
```

**Removing the apt copy matters.** `backup.sh` calls `rclone` bare, and cron's
default `PATH` is `/usr/bin:/bin` — which excludes `/usr/local/bin`. Leave both
installed and cron silently runs the 2022 binary while every check you type by
hand shows the new one. With one binary, a `PATH` mistake fails loudly with
`command not found` instead.

## 6 · DNS

An `A` record for the hostname → the box's IPv4, short TTL (120).

**If it is a dynamic-DNS hostname, make sure the record is static** and not
driven by the provider's update client, or it drifts back and takes the TLS
certificate with it.

Confirm before step 9 — Caddy requests a certificate the moment it starts, and
failed ACME validations count against a per-hostname limit:

```bash
dig +short <hostname> @1.1.1.1     # must be the box's IP
```

## 7 · The sparse checkout

```bash
su - deploy
git clone --depth=1 --filter=blob:none --sparse https://github.com/oandrz/Household.git
cd Household && git sparse-checkout set deploy && cd deploy
ls -A     # Caddyfile README.md PROVISION.md backup.sh crontab.example deploy.sh
          # docker-compose.prod.yml restore.sh sshd-hardening.conf .env.example
```

The application source is deliberately absent. If `api/` or `web/` appear, the
sparse checkout did not take.

**No registry login is needed** — the repository is public, so its GHCR images
pull anonymously. Verified, not assumed. If it is ever made private this becomes
`echo <token> | docker login ghcr.io -u <user> --password-stdin`.

## 8 · `.env`

```bash
cp .env.example .env && chmod 600 .env
```

Fill in only `IMAGE_TAG`, `ACME_EMAIL`, and a generated `POSTGRES_PASSWORD` —
the same value in `DATABASE_URL` and `GOOSE_DBSTRING` too. `README.md`'s "First
install" has the details and the traps.

**Generate the password as hex, not base64**: it is substituted into two
`postgres://` DSNs, and base64's `/`, `+` and `=` are all significant inside a
URL userinfo field. `openssl rand -hex 24`.

## 9 · First bring-up

```bash
./deploy.sh <git sha of a build the images workflow finished>
```

`deploy.sh` refuses `latest`, refuses a tag absent from the registry before it
touches `.env`, and verifies `migrate` exited `0` and `/readyz` answers.

## 10 · Backups

Create the R2 bucket and a **scoped Account API token** (see
`../docs/INFRASTRUCTURE.md`), then, **as `deploy`** — `rclone` reads
`$HOME/.config/rclone/rclone.conf` and the cron runs as `deploy`, so a remote
configured as root is invisible to it and fails silently at 3am:

```bash
rclone config create r2 s3 \
  provider=Cloudflare region=auto \
  access_key_id='...' secret_access_key='...' \
  endpoint='https://<account-id>.r2.cloudflarestorage.com' \
  acl=private no_check_bucket=true

rclone ls r2:hearth-backups     # the correct check
```

**`rclone lsd r2:` will always fail with `403 AccessDenied`** and that is
correct — a bucket-scoped token is not permitted to enumerate every bucket in
the account. Do not debug a working setup with it.

Then the environment file and the schedule:

```bash
cp hearth-backup.env.example ~/hearth-backup.env
chmod 600 ~/hearth-backup.env
# fill in AGE_RECIPIENT and HC_PING_URL

crontab crontab.example         # read it first; the PATH and CRON_TZ lines matter
touch ~/hearth-backup.log && chmod 600 ~/hearth-backup.log
```

Prove it as *cron* will run it, not as you will:

```bash
env -i PATH=/usr/local/bin:/usr/bin:/bin HOME=/home/deploy SHELL=/bin/sh \
  /bin/sh -c 'set -a; . /home/deploy/hearth-backup.env; set +a; /home/deploy/Household/deploy/backup.sh'
```

## 11 · Monitors

- **healthchecks.io** — period 1 day, grace 3 hours. Answers *did the backup
  run*. Its URL goes in `hearth-backup.env`.
- **UptimeRobot** — **Keyword** monitor on `https://<hostname>/readyz`, keyword
  `ready`, 5-minute interval. Answers *is the site up*. Keyword rather than
  plain HTTP so a `200` carrying an error body still trips it.

**Both, not one.** Only having the first is a gap that survived an entire
twelve-criterion walk.

## 12 · Prove it, do not assume it

Work through
`docs/superpowers/plans/2026-08-10-hearth-production-verification.md`. At
minimum: TLS valid from outside, a sign-up completing, `migrate` exited `0`, a
reboot recovering unattended, a restore from a real backup, and **both alarms
fired on purpose**.

When testing a deliberate outage, schedule the recovery *first* so a dead
session cannot leave production down:

```bash
systemd-run --on-active=8min --unit=hearth-api-failsafe \
  runuser -u deploy -- sh -c 'cd /home/deploy/Household/deploy && docker compose -f docker-compose.prod.yml start api'
```
