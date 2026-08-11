# Hearth production deployment — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Take Hearth from "has only ever run under `docker compose` on a laptop" to a live, backed-up, TLS-terminated install on one VPS, verified by a browser walk.

**Architecture:** The existing Compose stack, unchanged in shape, on one Linux box. Caddy terminates TLS in front of the existing nginx; nginx keeps serving the SPA and proxying `/api` same-origin. A new `admin` Dockerfile target carries `goose` and `adminctl` so a deployed install can migrate itself and be rescued. GitHub Actions builds three images tagged with the git SHA; the box only pulls. A host cron dumps Postgres nightly, encrypts with `age`, and ships the ciphertext off-provider.

**Tech Stack:** Docker Compose, Caddy 2, nginx 1.27, Postgres 17, distroless Go images, GitHub Actions, GHCR, `age`, `rclone`, healthchecks.io, UptimeRobot.

**Spec:** `docs/superpowers/specs/2026-08-10-hearth-production-deployment-design.md`. Its thirteen decisions are binding; where this plan appears to disagree, the spec wins and this plan is wrong.

## Global Constraints

- **The production images have no shell.** `gcr.io/distroless/static-debian12:nonroot`. Every Compose `command:` must be **exec form** (a JSON array). A string command runs through `/bin/sh` and fails with a misleading "no such file or directory".
- **Shell-form `${VAR}` expansion is therefore unavailable inside containers.** Anything a binary needs from the environment must arrive as a real environment variable, not as an interpolated argument.
- **Images are tagged with the git SHA and named through `IMAGE_TAG`.** A moving `:latest` must never appear in the Compose file (spec decision 4).
- **`pg_dump` output is plain SQL, never custom format** (ADR 1). Readable by any future Postgres and by a human.
- **The backup copy lives off the hosting provider.** Not a Hetzner Storage Box, not a Hetzner snapshot (ADR 1).
- **Secrets are never committed.** `deploy/.env` is gitignored; only `deploy/.env.example` is tracked.
- **nginx must trust exactly the Compose network subnet and nothing wider.** `set_real_ip_from` with a broader range than the private bridge re-opens the sign-up limiter to spoofing.
- **Docker for the local test runs is colima on the original machine:**
  ```bash
  export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
  export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
  ```
  If Docker Desktop is also running it will silently win host ports 80/443/5173/8080 — check `docker ps` on both engines before concluding something is broken (`docs/HANDOVER.md` §2).

---

## File structure

| File | Responsibility |
|---|---|
| `api/Dockerfile` | Gains an `admin` target: `adminctl` + `goose` + `migrations/` on distroless |
| `.github/workflows/images.yml` | Builds three images, tags each with the git SHA, pushes to GHCR |
| `deploy/docker-compose.prod.yml` | The six production services and one private network with a fixed subnet |
| `deploy/.env.example` | Every variable, secrets blank. The tracked template |
| `deploy/.env` | The real values on the box. Gitignored, `chmod 600` |
| `deploy/Caddyfile` | One site block, TLS, reverse proxy to `web:80` |
| `deploy/backup.sh` | dump → gzip → age → rclone → heartbeat |
| `deploy/restore.sh` | The reverse, into a throwaway database. Exists so the drill is repeatable rather than remembered |
| `deploy/README.md` | Deploy runbook, break-glass commands, restore procedure |
| `web/nginx.conf` | Gains three real-IP lines |
| `.gitignore` | Gains `deploy/.env` |

Each task below ends with something independently verifiable. Tasks 1–6 are verifiable on the laptop with no server in existence. Task 7 provisions the box. Task 8 is the walk.

---

### Task 1: The `admin` image

**Files:**
- Modify: `api/Dockerfile` (append after the existing `prod` stage, which ends at line 32)

**Interfaces:**
- Consumes: the existing `builder` stage in `api/Dockerfile`
- Produces: a Docker build target named `admin`, containing `/app/adminctl`, `/app/goose`, and `/app/migrations/`

- [ ] **Step 1: Write the failing test — a script that builds the target and runs both binaries**

Create `deploy/test-admin-image.sh` (temporary; it is deleted in step 6 once the plan's later tasks cover the same ground through the runbook):

```bash
#!/usr/bin/env bash
set -euo pipefail

docker build --target admin -t hearth-admin:test ./api

echo "--- adminctl prints usage with no arguments and no environment ---"
out="$(docker run --rm hearth-admin:test /app/adminctl 2>&1 || true)"
echo "$out" | grep -q "usage: adminctl <command>" || { echo "FAIL: no usage"; exit 1; }

echo "--- goose runs ---"
docker run --rm hearth-admin:test /app/goose --version

echo "--- migrations are present ---"
docker run --rm hearth-admin:test /app/goose -dir /app/migrations status 2>&1 \
  | grep -q "00001_init" || { echo "FAIL: migrations missing from image"; exit 1; }

echo "PASS"
```

`adminctl` with no arguments returns the usage string before `config.Load()` is ever called (`api/cmd/adminctl/main.go:57-60`), which is why this test needs no environment.

- [ ] **Step 2: Run it and watch it fail**

```bash
chmod +x deploy/test-admin-image.sh && ./deploy/test-admin-image.sh
```

Expected: FAIL at the `docker build` line — `failed to solve: target stage "admin" could not be found`.

- [ ] **Step 3: Add the target**

Append to `api/Dockerfile`:

```dockerfile
# --- admin: migrations and break-glass -------------------------------------
# The prod image above is deliberately a single binary with no shell, which
# also means a deployed install cannot migrate itself or be rescued. This
# target carries the two tools that do, on the same distroless base, so the
# production surface grows by two static binaries rather than by a Go
# toolchain. CGO_ENABLED=0 on the goose install is load-bearing: the alpine
# builder has no C compiler, and a dynamically linked binary would not run on
# distroless/static at all.
FROM builder AS admin-builder
RUN CGO_ENABLED=0 go install github.com/pressly/goose/v3/cmd/goose@v3.27.3 \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/adminctl ./cmd/adminctl

FROM gcr.io/distroless/static-debian12:nonroot AS admin
COPY --from=admin-builder /out/adminctl /app/adminctl
COPY --from=admin-builder /go/bin/goose /app/goose
COPY --from=admin-builder /src/migrations /app/migrations
USER nonroot:nonroot
# No ENTRYPOINT on purpose: this image is two tools, not one service. The
# binary is named at run time -- `docker compose run --rm admin /app/adminctl …`
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
./deploy/test-admin-image.sh
```

Expected: `PASS`, and the goose version line prints `v3.27.3`.

- [ ] **Step 5: Prove the image can migrate a database — both ways of telling goose where it is**

The second invocation is not redundant. Task 3's `migrate` service has no shell, so it cannot expand `${DATABASE_URL}` into an argument; it depends entirely on goose reading `GOOSE_DRIVER`, `GOOSE_DBSTRING` and `GOOSE_MIGRATION_DIR` from the environment. If v3.27.3 does not honour those, that failure must surface here, on a laptop, and not against the live box in Task 8.

```bash
docker network create hearth-t1
docker run -d --name pg-t1 --network hearth-t1 \
  -e POSTGRES_USER=hearth -e POSTGRES_PASSWORD=t -e POSTGRES_DB=hearth postgres:17-alpine
sleep 5
dsn="postgres://hearth:t@pg-t1:5432/hearth?sslmode=disable"

echo "--- A: arguments ---"
docker run --rm --network hearth-t1 hearth-admin:test \
  /app/goose -dir /app/migrations postgres "$dsn" up

echo "--- B: environment only, exactly as the migrate service will run it ---"
docker run --rm --network hearth-t1 \
  -e GOOSE_DRIVER=postgres -e GOOSE_DBSTRING="$dsn" -e GOOSE_MIGRATION_DIR=/app/migrations \
  hearth-admin:test /app/goose status

docker run --rm --network hearth-t1 postgres:17-alpine \
  psql "$dsn" -c "select version_id from goose_db_version order by id desc limit 1;"
docker rm -f pg-t1 && docker network rm hearth-t1
```

Expected: A reports `OK` for all eight migrations; B prints the status table without being given a single flag; the query prints `8`.

**If B fails**, goose does not read those variables in this version. Change Task 3's `migrate` service to the interpolated argument form instead — `command: ["/app/goose", "-dir", "/app/migrations", "postgres", "${GOOSE_DBSTRING}", "up"]`, which works because *Compose* performs that substitution before the container starts, not a shell inside it. The cost is that the DSN then appears in `docker compose config` output; note it in `deploy/README.md` if you take that path.

- [ ] **Step 6: Delete the temporary test script and commit**

The script's job was to fail before the target existed. Task 6's runbook is where these commands live permanently.

```bash
rm deploy/test-admin-image.sh
git add api/Dockerfile
git commit -m "feat(deploy): add an admin image target carrying goose and adminctl

The prod image is distroless with no shell, so a deployed install could not
apply a migration, reset a password or unlock a locked-out household -- and
mail, the only other recovery path, is documented as failing silently. This
adds the two tools on the same base rather than shipping a Go toolchain to
production."
```

---

### Task 2: CI builds three images and tags them with the git SHA

**Files:**
- Create: `.github/workflows/images.yml`

**Interfaces:**
- Consumes: the `prod` target in `web/Dockerfile`, and `prod` + `admin` in `api/Dockerfile`
- Produces: `ghcr.io/oandrz/hearth-api:<sha>`, `ghcr.io/oandrz/hearth-web:<sha>`, `ghcr.io/oandrz/hearth-admin:<sha>` — the three image references Task 3's Compose file consumes through `IMAGE_TAG`

- [ ] **Step 1: Write the workflow**

```yaml
name: images

# Only main builds images. A branch build would push tags nobody deploys and
# spend the free minutes that make this arrangement free.
on:
  push:
    branches: [main]
  workflow_dispatch:

permissions:
  contents: read
  packages: write

jobs:
  build:
    runs-on: ubuntu-latest
    strategy:
      # One job per image so a failure names which image failed, rather than
      # one job whose log has to be read to find out.
      matrix:
        include:
          - name: hearth-api
            context: ./api
            target: prod
          - name: hearth-admin
            context: ./api
            target: admin
          - name: hearth-web
            context: ./web
            target: prod
    steps:
      - uses: actions/checkout@v4

      - uses: docker/setup-buildx-action@v3

      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - uses: docker/build-push-action@v6
        with:
          context: ${{ matrix.context }}
          target: ${{ matrix.target }}
          push: true
          # The server is amd64. Building only that platform keeps the job
          # fast; nothing here ever runs on arm64.
          platforms: linux/amd64
          # The SHA tag is the one the Compose file names. :latest exists for
          # humans reading the registry and must never be referenced by a
          # deployment -- see the spec's decision 4 on why a moving tag lets a
          # migration-only change skip its own migration.
          tags: |
            ghcr.io/oandrz/${{ matrix.name }}:${{ github.sha }}
            ghcr.io/oandrz/${{ matrix.name }}:latest
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

- [ ] **Step 2: Verify the workflow parses before pushing**

```bash
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/images.yml')); print('parses')"
```

Expected: `parses`.

- [ ] **Step 3: Commit and push the branch, then trigger the workflow manually**

```bash
git add .github/workflows/images.yml
git commit -m "ci: build the three production images and tag them with the git SHA"
git push -u origin HEAD
gh workflow run images.yml --ref "$(git rev-parse --abbrev-ref HEAD)"
```

`workflow_dispatch` is in the trigger list precisely so this can be proven before anything merges to `main`.

- [ ] **Step 4: Watch it and confirm three images exist**

```bash
gh run watch
gh api "/users/oandrz/packages?package_type=container" --jq '.[].name'
```

Expected: the run succeeds, and the three package names are listed.

- [ ] **Step 5: Confirm the SHA tag is real and pullable**

```bash
docker pull "ghcr.io/oandrz/hearth-admin:$(git rev-parse HEAD)"
```

Expected: the pull succeeds. If it 403s, the packages are private and `docker login ghcr.io` with a `read:packages` token is needed — that token is the third secret named in the spec's Configuration section.

---

### Task 3: The production Compose file

**Files:**
- Create: `deploy/docker-compose.prod.yml`
- Create: `deploy/.env.example`
- Modify: `.gitignore`

**Interfaces:**
- Consumes: the three image references Task 2 produces
- Produces: services named `caddy`, `web`, `api`, `postgres`, `migrate`, `admin`; a network `hearth` with subnet `172.28.0.0/16` (Task 4's nginx configuration trusts exactly this range); the variable names `IMAGE_TAG`, `POSTGRES_PASSWORD`, `DOMAIN`, `ACME_EMAIL`, `GOOSE_DBSTRING`

- [ ] **Step 1: Write `deploy/.env.example`**

```bash
# Copy to deploy/.env on the box and fill in. `chmod 600` it. Never commit it.
#
# Compose reads THIS file for two different jobs and the difference matters:
# `${VAR}` interpolation in docker-compose.prod.yml only ever reads a file
# literally named `.env` in the directory Compose runs from -- which is why
# the real file is `.env` and not `.env.production`. The same file is also
# passed to containers via `env_file:`.

# --- what to run -----------------------------------------------------------
# The git SHA CI tagged the images with. Changing this value is what a deploy
# IS. It must never be `latest`: a migration-only change produces a
# byte-identical api image, and without a changed tag Compose will not recreate
# `api`, will not re-evaluate depends_on: migrate, and the migration silently
# does not run.
IMAGE_TAG=

# --- the box ---------------------------------------------------------------
DOMAIN=hearth.example.com
ACME_EMAIL=you@example.com

# --- application (every variable config.Load() requires) -------------------
APP_ENV=production
PORT=8080
# sslmode=disable is correct and deliberate: postgres is on the private compose
# bridge and is never published, so this connection does not cross a network.
# See the spec's decision 9.
DATABASE_URL=postgres://hearth:CHANGEME@postgres:5432/hearth?sslmode=disable
APP_BASE_URL=https://hearth.example.com
SMTP_ADDR=smtp.resend.com:587
SMTP_FROM="Hearth <noreply@hearth.example.com>"
# SMTP_USERNAME and SMTP_PASSWORD: set BOTH together (Resend's SMTP username is
# literally "resend"; the password is the API key) or leave BOTH blank.
# config.Load() refuses one set and the other empty, and it runs before every
# adminctl subcommand too -- so a lone leftover value here does not just
# affect mail, it breaks unlock-household, reset-password, create-invite and
# prune as well, with an error that says nothing about any of them. Blank is
# the safe starting value for exactly that reason: it cannot produce this trap
# by omission the way a pre-filled username with no password can.
SMTP_USERNAME=
SMTP_PASSWORD=
# SMTP_TLS_MODE is deliberately unset: it defaults to "mandatory" whenever
# APP_ENV is not development.
ARGON2_TIME=3
ARGON2_MEMORY_KIB=65536
ARGON2_THREADS=2

# --- postgres --------------------------------------------------------------
# Must match the password inside DATABASE_URL above.
POSTGRES_PASSWORD=CHANGEME

# --- migrations ------------------------------------------------------------
# goose takes its DSN from the environment rather than from an argument,
# because the admin image has no shell and an exec-form command cannot expand
# ${DATABASE_URL}. Same value as DATABASE_URL; kept as its own variable so the
# migrate service needs no interpolation of a secret into the compose config.
GOOSE_DBSTRING=postgres://hearth:CHANGEME@postgres:5432/hearth?sslmode=disable
```

- [ ] **Step 2: Write `deploy/docker-compose.prod.yml`**

```yaml
# `hearth-prod`, deliberately not `hearth`: the development stack in the repo
# root declares `name: hearth` and a volume `hearth-pgdata` too, so both files
# used to resolve to the identical Docker volume `hearth_hearth-pgdata`. The
# consequence that decides this is not the mixed-up containers -- it is that
# `backup.sh` run from a developer's checkout would dump the *development*
# database, encrypt it, upload it to the production bucket under tonight's
# date and ping the heartbeat green. Every detector reports healthy while the
# backup holds the wrong data, which is worse than having no backup. Two more
# follow from the same collision: `down -v` from either directory destroys the
# other's database, and a prod `POSTGRES_PASSWORD` is silently ignored against
# an already-initialised dev volume, which then looks like a secret problem
# rather than a volume problem. Renaming makes every prod resource
# `hearth-prod_*`, so a wrong-directory command fails loudly instead.
name: hearth-prod

services:
  # Caddy is the only thing on the public internet. It exists to obtain and
  # renew certificates automatically for as long as this product runs; the
  # routing is still nginx's job below.
  caddy:
    image: caddy:2-alpine
    restart: unless-stopped
    ports: ["80:80", "443:443"]
    environment:
      DOMAIN: ${DOMAIN}
      ACME_EMAIL: ${ACME_EMAIL}
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy-data:/data
      - caddy-config:/config
    depends_on: [web]
    networks: [hearth]

  web:
    image: ghcr.io/oandrz/hearth-web:${IMAGE_TAG}
    restart: unless-stopped
    depends_on: [api]
    networks: [hearth]

  api:
    image: ghcr.io/oandrz/hearth-api:${IMAGE_TAG}
    restart: unless-stopped
    env_file: [.env]
    depends_on:
      postgres: {condition: service_healthy}
      migrate: {condition: service_completed_successfully}
    networks: [hearth]

  # One-shot. `api` will not start until this exits 0, so a failed migration
  # blocks the new API rather than half-upgrading the install.
  migrate:
    image: ghcr.io/oandrz/hearth-admin:${IMAGE_TAG}
    restart: "no"
    env_file: [.env]
    environment:
      GOOSE_DRIVER: postgres
      GOOSE_MIGRATION_DIR: /app/migrations
    # Exec form is mandatory: the image has no shell.
    command: ["/app/goose", "up"]
    depends_on:
      postgres: {condition: service_healthy}
    networks: [hearth]

  # Never started by `up`. Reached with:
  #   docker compose -f docker-compose.prod.yml run --rm admin /app/adminctl …
  admin:
    image: ghcr.io/oandrz/hearth-admin:${IMAGE_TAG}
    profiles: [manual]
    env_file: [.env]
    depends_on:
      postgres: {condition: service_healthy}
    networks: [hearth]

  postgres:
    image: postgres:17-alpine
    restart: unless-stopped
    # Deliberately no `ports:`. The database is reachable only from this
    # network, which is what makes sslmode=disable correct.
    environment:
      POSTGRES_USER: hearth
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: hearth
    volumes: ["hearth-pgdata:/var/lib/postgresql/data"]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U hearth -d hearth"]
      interval: 3s
      timeout: 3s
      retries: 20
    networks: [hearth]

networks:
  # The subnet is fixed rather than assigned, because web/nginx.conf names it
  # in set_real_ip_from. A floating subnet would silently break the per-IP
  # sign-up rate limiter the first time Docker chose a different range.
  hearth:
    ipam:
      config:
        - subnet: 172.28.0.0/16

volumes:
  hearth-pgdata:
  caddy-data:
  caddy-config:
```

- [ ] **Step 3: Add the ignore rule**

Append to `.gitignore`:

```gitignore
# The real production environment file. Only deploy/.env.example is tracked.
deploy/.env
```

- [ ] **Step 4: Verify the file parses and interpolates**

```bash
cd deploy
cp .env.example .env
sed -i '' "s/^IMAGE_TAG=/IMAGE_TAG=$(git rev-parse HEAD)/" .env
docker compose -f docker-compose.prod.yml config >/dev/null && echo "parses"
docker compose -f docker-compose.prod.yml config | grep -c "IMAGE_TAG"
```

Expected: `parses`, and the grep count is `0` — every `${IMAGE_TAG}` has been substituted. A non-zero count means Compose did not find `.env`, which is the failure this file's naming exists to avoid.

- [ ] **Step 5: Confirm `deploy/.env` is actually ignored**

```bash
git status --porcelain deploy/.env
```

Expected: no output. Any output means the secret file is about to be committed.

- [ ] **Step 6: Commit**

```bash
git add deploy/docker-compose.prod.yml deploy/.env.example .gitignore
git commit -m "feat(deploy): add the production compose file and its environment template

Six services on one private network with a fixed subnet, which web/nginx.conf
will name in set_real_ip_from. Postgres is unpublished, migrations are a
one-shot service the api waits on, and the admin image sits behind a profile so
it is only ever run on demand."
```

---

### Task 4: Caddy in front, and nginx learning to trust exactly it

**Files:**
- Create: `deploy/Caddyfile`
- Modify: `web/nginx.conf` (inside the `server` block, before the first `location`)

**Interfaces:**
- Consumes: the `hearth` network subnet `172.28.0.0/16` and the `DOMAIN` / `ACME_EMAIL` variables from Task 3
- Produces: a working TLS edge, and an `api` that sees real client addresses rather than Caddy's

This is the security-critical task in the plan. The failure it prevents is silent: nothing errors, the per-IP sign-up limiter simply stops distinguishing callers.

- [ ] **Step 1: Write the Caddyfile**

```
{
	email {$ACME_EMAIL}
}

{$DOMAIN} {
	encode zstd gzip

	# CORRECTED during task 4 — this plan originally said Caddy APPENDS the
	# real peer to X-Forwarded-For. It does not. Caddy REPLACES the header
	# with the address it is talking to, preserving a caller's list only when
	# that caller is listed in trusted_proxies, and none is configured here.
	# Measured, not assumed: nginx logged $http_x_forwarded_for as
	# "172.28.0.6" for a request whose client sent "9.9.9.88". The shipped
	# wording is in deploy/Caddyfile; see task-4-report.md.
	reverse_proxy web:80
}
```

- [ ] **Step 2: Write the failing test — two clients must get two rate-limit budgets**

Create `deploy/test-real-ip.sh`:

```bash
#!/usr/bin/env bash
# Proves the per-IP sign-up limiter keys on the real client rather than on
# Caddy. Six requests from one container should end in 429; a request from a
# SECOND container must still succeed. Six-from-one alone proves nothing: if
# the real-IP chain is broken every caller shares one bucket and the sixth
# still answers 429.
set -euo pipefail
cd "$(dirname "$0")"

C="docker compose -f docker-compose.prod.yml"
url="https://${DOMAIN:?set DOMAIN}/api/v1/auth/sign-up"

burst() { # $1 = container name, $2 = email prefix
  docker run --rm --network hearth-prod_hearth --name "$1" curlimages/curl:8.11.1 \
    sh -c "for i in 1 2 3 4 5 6; do
             curl -sk -o /dev/null -w '%{http_code} ' -X POST '$url' \
               -H 'Content-Type: application/json' \
               -d '{\"email\":\"$2-'\$i'@example.test\"}';
           done; echo"
}

echo "--- client A, six requests ---"
a="$(burst ripA a)"
echo "$a"
[[ "$a" == *"429"* ]] || { echo "FAIL: client A never hit the limit"; exit 1; }

echo "--- client B, one request, must NOT be limited ---"
b="$(docker run --rm --network hearth-prod_hearth curlimages/curl:8.11.1 \
      curl -sk -o /dev/null -w '%{http_code}' -X POST "$url" \
      -H 'Content-Type: application/json' -d '{"email":"b-1@example.test"}')"
echo "$b"
[[ "$b" == "202" ]] || { echo "FAIL: client B got $b, want 202 — the limiter is keying on the proxy, not the client"; exit 1; }

echo "PASS"
```

Sign-up answers `202` for every accepted address (the enumeration-safe contract), so `202` is the success code here, not `200`. Mail sending is fire-and-forget and the limiter counts in the HTTP layer, so an unreachable SMTP host does not affect this test.

- [ ] **Step 3: Bring the stack up locally with internal TLS, and run the test to watch it fail**

Add a temporary first line to `deploy/Caddyfile` inside the site block — `tls internal` — so Caddy issues a local certificate instead of trying to reach Let's Encrypt for a domain that does not resolve:

```
{$DOMAIN} {
	tls internal
	encode zstd gzip
	reverse_proxy web:80
}
```

Then:

```bash
cd deploy
sed -i '' 's/^DOMAIN=.*/DOMAIN=hearth.localhost/' .env
sed -i '' 's|^APP_BASE_URL=.*|APP_BASE_URL=https://hearth.localhost|' .env
sed -i '' 's/^SMTP_ADDR=.*/SMTP_ADDR=127.0.0.1:1025/' .env
docker compose -f docker-compose.prod.yml up -d
sleep 10
DOMAIN=hearth.localhost ../deploy/test-real-ip.sh
```

Expected: **FAIL** at client B with a `429` — nginx is still reading `$remote_addr`, which is Caddy's address for every request, so both containers share one bucket.

- [ ] **Step 4: Teach nginx to recover the real client address**

Insert into `web/nginx.conf`, immediately after `root /usr/share/nginx/html;`:

```nginx
    # SECURITY: these three lines are what make the X-Real-IP rewriting below
    # meaningful once Caddy sits in front. Without them $remote_addr is Caddy's
    # own address on every request, so the API's per-IP sign-up limiter keys the
    # entire world to one bucket and stops limiting -- silently, with no error
    # anywhere. The subnet is the fixed one declared in
    # deploy/docker-compose.prod.yml; it must stay in step with that file.
    # real_ip_recursive is left off (nginx's default, stated here because it is
    # load-bearing rather than incidental): with it off, nginx takes the LAST
    # address in X-Forwarded-For, which is the one written by the trusted
    # upstream. CORRECTED during task 4 — that entry is there because Caddy
    # REPLACES the header, not because it appends to a client's value; the
    # shipped wording is in web/nginx.conf, and task-4-report.md has the
    # measurement.
    set_real_ip_from  172.28.0.0/16;
    real_ip_header    X-Forwarded-For;
    real_ip_recursive off;
```

- [ ] **Step 5: Rebuild the web image locally, restart, and run the test to verify it passes**

The Compose file names a registry tag, so the local rebuild has to be tagged to match:

```bash
cd deploy
docker build --target prod -t "ghcr.io/oandrz/hearth-web:$(git rev-parse HEAD)" ../web
docker compose -f docker-compose.prod.yml up -d --force-recreate web
sleep 5
DOMAIN=hearth.localhost ./test-real-ip.sh
```

Expected: `PASS` — client A ends in `429`, client B gets `202`.

- [ ] **Step 6: Prove a client cannot spoof past it**

```bash
docker run --rm --network hearth-prod_hearth curlimages/curl:8.11.1 \
  curl -sk -o /dev/null -w '%{http_code}\n' -X POST "https://hearth.localhost/api/v1/auth/sign-up" \
  -H 'Content-Type: application/json' \
  -H 'X-Forwarded-For: 9.9.9.9' -H 'X-Real-IP: 9.9.9.9' -H 'True-Client-IP: 9.9.9.9' \
  -d '{"email":"spoof-1@example.test"}'
```

Run it six times. Expected: the sixth answers `429`. A client that could spoof would get `202` forever, because each forged address would open a fresh bucket.

- [ ] **Step 7: Remove `tls internal`, tear down, delete the test script, commit**

`tls internal` is a local-testing device and must not reach the box, where it would serve a certificate no browser trusts.

```bash
cd deploy
docker compose -f docker-compose.prod.yml down -v
# remove the `tls internal` line from Caddyfile
rm test-real-ip.sh
cd ..
git add deploy/Caddyfile web/nginx.conf
git commit -m "feat(deploy): terminate TLS in Caddy and recover the real client IP in nginx

nginx's X-Real-IP rewriting assumed it was the edge. With Caddy in front,
\$remote_addr is Caddy's address on every request, so the per-IP sign-up
limiter keyed the whole world to one bucket -- silently. set_real_ip_from over
the compose subnet restores it; proven locally by two containers getting two
independent budgets, and by a forged X-Forwarded-For failing to open a third."
```

The test script is deleted rather than kept because it needs the whole stack up on a chosen domain; criterion 10 of the walk is where this behaviour is verified permanently.

---

### Task 5: Nightly backup, and a restore that has actually been run

**Files:**
- Create: `deploy/backup.sh`
- Create: `deploy/restore.sh`

**Interfaces:**
- Consumes: the running `postgres` service from Task 3
- Produces: objects named `hearth-YYYY-MM-DD.sql.gz.age` at `$RCLONE_REMOTE`, and a heartbeat ping

- [ ] **Step 1: Write `deploy/backup.sh`**

```bash
#!/usr/bin/env bash
# Nightly: dump -> gzip -> age -> off-provider bucket -> heartbeat.
#
# Plain SQL, never custom format: it is readable by any future Postgres and by
# a human, which is the property that matters over a forty-year horizon
# (docs/adr/0001-optimise-for-exit-cost.md).
#
# The dump is a household's entire financial history plus email addresses and
# password hashes, so it is encrypted here, before it leaves the box. The
# bucket only ever holds ciphertext.
set -euo pipefail

: "${AGE_RECIPIENT:?AGE_RECIPIENT is required}"
: "${RCLONE_REMOTE:?RCLONE_REMOTE is required, e.g. r2:hearth-backups}"
: "${HC_PING_URL:?HC_PING_URL is required}"

cd "$(dirname "$0")"

stamp="$(date -u +%Y-%m-%d)"
file="hearth-${stamp}.sql.gz.age"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# pg_dump runs inside the container because postgres is not published.
docker compose -f docker-compose.prod.yml exec -T postgres \
  pg_dump -U hearth -d hearth --format=plain --no-owner --no-privileges \
  | gzip -9 \
  | age -r "$AGE_RECIPIENT" -o "$tmp/$file"

# A zero-length or absurdly small object means the pipeline failed upstream in
# a way `set -o pipefail` did not catch. Better to fail loudly than to upload a
# file that looks like a backup.
size="$(wc -c < "$tmp/$file")"
if [ "$size" -lt 1024 ]; then
  echo "refusing to upload a ${size}-byte backup" >&2
  exit 1
fi

rclone copyto "$tmp/$file" "$RCLONE_REMOTE/$file"

# The heartbeat is pinged only after a successful upload. A backup that
# silently stops is the standard way this fails, and a missed ping is what
# turns that into an email instead of a discovery during a restore.
curl -fsS -m 10 --retry 3 "$HC_PING_URL" >/dev/null
echo "uploaded $file (${size} bytes)"
```

- [ ] **Step 2: Write `deploy/restore.sh`**

```bash
#!/usr/bin/env bash
# Restore a backup into a THROWAWAY database and report what came back.
#
# This exists so the drill is repeatable rather than remembered. It never
# writes to the live database: the target is passed in, and the script refuses
# a DSN pointing at the production database name on the compose network.
set -euo pipefail

usage() { echo "usage: restore.sh <file.sql.gz.age> <target-dsn> [age-identity-file]"; exit 1; }
[ $# -ge 2 ] || usage

file="$1"; target="$2"; identity="${3:-$HOME/.config/age/hearth.key}"

case "$target" in
  *@postgres:5432/hearth*) echo "refusing to restore over the live database" >&2; exit 1 ;;
esac

age -d -i "$identity" "$file" | gunzip | psql "$target" -v ON_ERROR_STOP=1 -q

echo "--- restored row counts ---"
psql "$target" -At -c "
  select 'households', count(*) from households
  union all select 'users', count(*) from users
  union all select 'transactions', count(*) from transactions
  union all select 'bills', count(*) from bills;"
```

- [ ] **Step 3: Generate a test key and run the round trip locally**

```bash
cd deploy
mkdir -p ~/.config/age && age-keygen -o ~/.config/age/hearth-test.key
export AGE_RECIPIENT="$(age-keygen -y ~/.config/age/hearth-test.key)"
export RCLONE_REMOTE="/tmp/hearth-backup-test"   # a plain local path stands in for the bucket
export HC_PING_URL="https://example.com"
mkdir -p "$RCLONE_REMOTE"

docker compose -f docker-compose.prod.yml up -d postgres
sleep 5
docker compose -f docker-compose.prod.yml run --rm migrate
chmod +x backup.sh restore.sh
./backup.sh
```

Expected: `uploaded hearth-YYYY-MM-DD.sql.gz.age (NNNN bytes)`.

- [ ] **Step 4: Restore it into a throwaway database and check the counts**

```bash
docker run -d --name pg-restore -p 55432:5432 \
  -e POSTGRES_USER=hearth -e POSTGRES_PASSWORD=t -e POSTGRES_DB=restored postgres:17-alpine
sleep 5
./restore.sh "/tmp/hearth-backup-test/hearth-$(date -u +%Y-%m-%d).sql.gz.age" \
  "postgres://hearth:t@localhost:55432/restored?sslmode=disable" \
  ~/.config/age/hearth-test.key
```

Expected: the four counts print. On a freshly migrated database every count is `0` — that is a pass. The point being proven here is that the encryption, the compression and the SQL all round-trip, not that there is data.

- [ ] **Step 5: Prove the guard refuses the live database**

```bash
./restore.sh /tmp/x.age "postgres://hearth:p@postgres:5432/hearth?sslmode=disable" ; echo "exit=$?"
```

Expected: `refusing to restore over the live database`, `exit=1`.

- [ ] **Step 6: Clean up and commit**

```bash
docker rm -f pg-restore
docker compose -f docker-compose.prod.yml down -v
rm -rf /tmp/hearth-backup-test ~/.config/age/hearth-test.key
cd .. && git add deploy/backup.sh deploy/restore.sh
git commit -m "feat(deploy): nightly encrypted backup and a rehearsed restore

Plain SQL so any future Postgres and any human can read it, encrypted before
it leaves the box because the dump is a household's whole financial history,
and heartbeat-pinged only after a successful upload so a cron that stops
firing sends mail instead of being discovered during a restore."
```

---

### Task 6: The runbook

**Files:**
- Create: `deploy/README.md`

**Interfaces:**
- Consumes: everything Tasks 1–5 produced
- Produces: the document the box is operated from, and the only place the break-glass commands are written down

- [ ] **Step 1: Write `deploy/README.md`**

````markdown
# Deploying and operating Hearth

The reasoning lives in `docs/adr/0001-optimise-for-exit-cost.md`,
`docs/adr/0002-first-production-host.md` and
`docs/superpowers/specs/2026-08-10-hearth-production-deployment-design.md`.
This file is the operations surface only.

Every command below runs from this directory on the box.

## First install

`cp .env.example .env`, `chmod 600 .env`, then fill in `IMAGE_TAG`, `DOMAIN`,
`ACME_EMAIL`, `POSTGRES_PASSWORD` (the same password again inside both
`DATABASE_URL` and `GOOSE_DBSTRING`), `APP_BASE_URL`, `SMTP_FROM`,
`SMTP_USERNAME` (literally `resend`) and `SMTP_PASSWORD` (the Resend API key).

The full table, and why leaving *both* SMTP values blank produces an install
that looks completely healthy and mails nothing, is written out in the shipped
`deploy/README.md`. It has to live there rather than only in this plan: the box
sparse-checks out `deploy/` alone and never receives this file.

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
   curl -fsS https://$DOMAIN/readyz && echo
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

# Set a member's password. Prompts on stdin, so -it is required.
$C run --rm -it admin /app/adminctl reset-password --email=someone@example.com

# Invite a member from the command line.
$C run --rm admin /app/adminctl create-invite \
  --email=new@example.com --name=Name --role=limited \
  --capabilities=money --inviter-email=owner@example.com

# Retention: delete consumed/expired signups and stale login attempts.
$C run --rm admin /app/adminctl prune --older-than=30

# Migration status.
$C run --rm admin /app/goose status
```

`--email` is effectively required on `unlock-household` here: its default
resolves the *seeded* owner, and production is never seeded.

`adminctl seed` refuses outside development and refuses a non-local database,
checked before it opens a connection. It cannot damage this install.

## Backups

A host cron runs `backup.sh` nightly. It is the **`deploy` user's** crontab —
`crontab -e` as `deploy`, not `sudo crontab -e` — and `rclone config` must be
run as that same user, since `rclone` reads `$HOME/.config/rclone/rclone.conf`:

```cron
17 3 * * * AGE_RECIPIENT=age1… RCLONE_REMOTE=r2:hearth-backups HC_PING_URL=https://hc-ping.com/… /home/deploy/Household/deploy/backup.sh >> /home/deploy/hearth-backup.log 2>&1
```

The log path is under `/home/deploy`, not `/var/log`: on Ubuntu 24.04
`/var/log` is `drwxrwxr-x root:syslog`, so cron's `/bin/sh` fails on the
redirection before `backup.sh` runs at all.

A missed night emails you, via the heartbeat. That is the only monitoring the
backups have, and it is the one that matters: the failure mode is silence.

## Restoring

```bash
rclone copyto r2:hearth-backups/hearth-2026-08-10.sql.gz.age /tmp/b.age
./restore.sh /tmp/b.age "postgres://hearth:PASS@localhost:55432/restored?sslmode=disable"
```

`restore.sh` refuses a DSN pointing at the live database.

**Rehearse this with the escrowed copy of the key, not your own.** An escrow
that has never been used is not an escrow; it is a hope.

## What is where

Resolved names — what `docker volume ls` prints, not the bare names declared in
the Compose file.

| | |
|---|---|
| Compose project | `hearth-prod` |
| Data | Docker volume `hearth-prod_hearth-pgdata` |
| Certificates | Docker volume `hearth-prod_caddy-data` |
| Network | `hearth-prod_hearth`, subnet `172.28.0.0/16` |
| Secrets | `deploy/.env`, mode 600, gitignored |
| Registry | `ghcr.io/oandrz/hearth-{api,web,admin}` |
| Logs | `docker compose -f docker-compose.prod.yml logs -f <service>` |
| Backup log | `/home/deploy/hearth-backup.log` |
````

- [ ] **Step 2: Check every command in it is real**

```bash
grep -o "adminctl [a-z-]*" deploy/README.md | sort -u
grep -n "usage: adminctl" -A 12 api/cmd/adminctl/main.go
```

Expected: every subcommand named in the README appears in the usage block — `unlock-household`, `reset-password`, `create-invite`, `prune`. Any that does not is a command that will fail at the worst moment.

- [ ] **Step 3: Commit**

```bash
git add deploy/README.md
git commit -m "docs(deploy): add the operations runbook

Deploy, rollback, break-glass, backup and restore, with the reasoning left in
the ADRs and only the commands here."
```

---

### Task 7: Provision the box

No code. This is the manual task, written down so it is repeatable when the box is rebuilt — which over the intended lifetime of this product will happen.

**Interfaces:**
- Consumes: `deploy/` from Tasks 3–6
- Produces: a reachable host, a resolving domain, a verified mail domain, and the escrow envelope

- [ ] **Step 1: Create the server**

Hetzner Cloud, **CPX11, Singapore**, Ubuntu 24.04 LTS, SSH key only. Record the IPv4 address.

- [ ] **Step 2: Firewall — 22, 80, 443 and nothing else**

Use the Hetzner Cloud firewall (outside the box, so a misconfigured host cannot open itself):

```
inbound: tcp/22 tcp/80 tcp/443
outbound: all
```

Verify from the laptop:

```bash
nc -zv -w3 <ip> 5432   # expected: refused/timeout. A success here means postgres is exposed.
```

- [ ] **Step 3: Install Docker and the tools the scripts need**

```bash
ssh root@<ip>
apt-get update && apt-get install -y ca-certificates curl age rclone
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo $VERSION_CODENAME) stable" > /etc/apt/sources.list.d/docker.list
apt-get update && apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
adduser --disabled-password --gecos "" deploy && usermod -aG docker deploy
```

- [ ] **Step 4: DNS**

An `A` record for the domain to the box's IPv4. Confirm before continuing — Caddy's certificate request will fail if the name does not resolve to this host:

```bash
dig +short <domain>
```

- [ ] **Step 5: Resend**

Create the account, add the domain, publish the SPF, DKIM and DMARC `TXT` records it gives you, and wait for verification to go green. Create an API key. `SMTP_FROM` must be an address on this domain or Resend rejects the send.

- [ ] **Step 6: The sparse checkout**

```bash
su - deploy
git clone --depth=1 --filter=blob:none --sparse https://github.com/oandrz/Household.git
cd Household && git sparse-checkout set deploy && cd deploy
ls   # expected: Caddyfile, README.md, backup.sh, docker-compose.prod.yml, restore.sh, .env.example
```

The source tree is deliberately absent. If `api/` or `web/` appear, the sparse checkout did not take.

- [ ] **Step 7: Secrets**

```bash
cp .env.example .env && chmod 600 .env
```

Fill in: `IMAGE_TAG` (the SHA CI built), `DOMAIN`, `ACME_EMAIL`, a generated `POSTGRES_PASSWORD` (and the same password inside both `DATABASE_URL` and `GOOSE_DBSTRING`), **`SMTP_USERNAME` (literally `resend`)**, `SMTP_PASSWORD` (the Resend API key), and the real `APP_BASE_URL` / `SMTP_FROM`.

`SMTP_USERNAME` ships **blank** and must be filled — leaving it out is the one omission here that does not announce itself. Filling only `SMTP_PASSWORD` makes `config.Load()` refuse and `api` restart-loop, which is loud. Leaving *both* blank is accepted by `config.Load()`: `api` boots, `/readyz` is green and sign-up answers `202`, but `SMTP_TLS_MODE` defaults to `mandatory` with no AUTH, Resend rejects every send, and `sendMagicLinkAsync` is fire-and-forget — so the install looks healthy and mails nothing, with mail being the only account-recovery path this product has. `deploy/README.md`'s "First install" section is the copy of this that actually reaches the box, since the box sparse-checks out `deploy/` only and never sees this file.

Then log in to the registry:

```bash
echo "<github token with read:packages>" | docker login ghcr.io -u oandrz --password-stdin
```

- [ ] **Step 8: The age key and the escrow envelope**

```bash
age-keygen -o ~/.config/age/hearth.key    # on YOUR machine, not the box
age-keygen -y ~/.config/age/hearth.key    # the public recipient — this goes in the cron line
```

Only the **public** recipient goes on the box. The private key never does: a box that can decrypt its own backups offers an attacker both.

The escrow envelope, given to the trusted second person, contains four things:

1. the age private key
2. the `POSTGRES_PASSWORD`
3. the GHCR read token
4. a printed copy of `deploy/README.md`'s restore section

Without all four, a successor has ciphertext they cannot read or images they cannot pull. This is the concrete answer to the succession gap in `docs/HANDOVER.md` §5.

- [ ] **Step 9: `rclone` and the bucket**

Create a Cloudflare R2 bucket, then on the box, **as the `deploy` user** (`su - deploy` first if you are not already):

```bash
rclone config    # S3-compatible provider, R2 endpoint, name the remote `r2`
rclone lsd r2:   # expected: the bucket is listed
```

The user matters. `rclone` reads `$HOME/.config/rclone/rclone.conf`, and the backup cron runs as `deploy` (Task 8 step 2) — so a remote configured as root is invisible to it, and the nightly upload fails with `didn't find section in config file` long after anyone is watching.

- [ ] **Step 10: Monitoring**

Create the UptimeRobot check (`https://<domain>/readyz`, five minutes, email on failure) and the healthchecks.io check (period one day, grace three hours). Record the ping URL for the cron line.

---

### Task 8: First deploy and the twelve-criterion walk

**Interfaces:**
- Consumes: everything above
- Produces: a live install, and `docs/superpowers/plans/2026-08-10-hearth-production-verification.md` recording each criterion's result

- [ ] **Step 1: Bring it up**

```bash
cd ~/Household/deploy
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
docker compose -f docker-compose.prod.yml ps
```

Expected: `migrate` shows `exited (0)`; `caddy`, `web`, `api`, `postgres` are up and none is restarting.

- [ ] **Step 2: Install the backup cron**

As the **`deploy` user** — `crontab -e`, not `sudo crontab -e`. Everything on this box runs as `deploy` (step 6), and both the log path and the `rclone` remote are per-user.

```bash
crontab -e
# 17 3 * * * AGE_RECIPIENT=… RCLONE_REMOTE=r2:hearth-backups HC_PING_URL=… /home/deploy/Household/deploy/backup.sh >> /home/deploy/hearth-backup.log 2>&1
./backup.sh    # run it once by hand now; this proves the script, not the schedule
```

The log goes under `/home/deploy`, **not `/var/log`**: on Ubuntu 24.04 `/var/log` is `drwxrwxr-x root:syslog` and `deploy` is in neither group, so cron's `/bin/sh` fails on the redirection *before `backup.sh` runs at all* — no dump, no upload, no heartbeat ping. The heartbeat catches it within its grace window, so it is not silent, but it burns the first night and points at the script rather than at permissions.

- [ ] **Step 3: Create the verification file**

Create `docs/superpowers/plans/2026-08-10-hearth-production-verification.md` with the twelve criteria below as headings, and fill each in as you go — result, evidence, and anything that had to be interpreted rather than followed literally. That is the standard the five Money walks set; three of them recorded interpreted criteria rather than passing over them quietly.

- [ ] **Step 4: Criteria 1–6 — it works at all**

1. `https://<domain>` serves with a valid certificate; `http://<domain>` redirects to it
2. Sign-up completes **from a phone on mobile data** — a real network, not the laptop
3. That mail arrives in a Gmail **inbox**, not spam
4. The link completes and the household exists
5. The session cookie is `Secure`, `HttpOnly`, `SameSite=Lax` on the real domain (read it in the browser's devtools, not from the code)
6. An account, a transaction, a budget, a goal and a bill each save, and every derived figure moves

Screenshots are the record, not the evidence. Assert on numbers — read `innerText` or `getBoundingClientRect()` in page script — because a walk here has twice concluded an element was missing when it was rendered small and far right (`docs/HANDOVER.md` §2).

- [ ] **Step 5: Criteria 7–9 — recovery and schema**

7. Three wrong passwords locks the household; a magic link recovers it
8. `docker compose -f docker-compose.prod.yml run --rm admin /app/adminctl unlock-household --email=<your address>` clears a lockout against the live database
9. `docker compose -f docker-compose.prod.yml run --rm admin /app/goose status` shows all eight migrations applied

- [ ] **Step 6: Criterion 10 — the limiter keys on the real client**

**Restart `api` first.** The per-IP limiter is an in-memory bucket of 5/hour, and criteria 2–4 have already spent part of the budget for whichever network ran them — the sign-up walk of 2026-07-30 hit exactly this and recorded it.

```bash
docker compose -f docker-compose.prod.yml restart api
```

Then, from the laptop's network, submit six sign-ups in quick succession and confirm the sixth answers `429`. Then, from the phone on mobile data — already established as a genuinely separate client by criterion 2 — submit one and confirm it is accepted.

A `429` from the phone means `set_real_ip_from` is not working and every caller is sharing one bucket. Six-from-one alone proves nothing.

- [ ] **Step 7: Criterion 11 — it survives a reboot**

```bash
sudo reboot
# wait, then from the laptop:
curl -fsS https://<domain>/readyz && echo
```

Expected: `readyz` answers without anyone starting anything.

- [ ] **Step 8: Criterion 12 — the scheduled backup, and a restore with the escrowed key**

This one waits for the cron to fire, so the walk is two sittings rather than one. The next morning:

1. Confirm healthchecks.io shows the ping.
2. `rclone ls r2:hearth-backups` shows the night's object.
3. Restore it on the laptop with the **escrowed** copy of the key — the one held by the other person, retrieved for this purpose — into a throwaway database, and confirm the row counts match what the live install actually holds.

A manual `backup.sh` passing (step 2) proves the script. Only this proves the schedule and the escrow.

- [ ] **Step 9: Record the result and update the trackers**

Fill in the verification file with all twelve results. Then, in the same change:

- `docs/HANDOVER.md` §1 and §4 — the install is live; the seven pre-deployment items are closed; state which criteria needed interpreting
- `docs/HANDOVER.md` §5 — remove the production-administration gap and the stacked-proxy gap from "Before this is deployed anywhere real", since both are now closed; leave the domain, succession and long-horizon assumption entries, and note the escrow envelope as the succession item's first real answer
- `docs/SYSTEM_DESIGN.md` §1 — the production topology diagram stops being a target. Remove the "decided, not yet deployed" framing and the dashed admin box, and correct the scope statement at the top of the file
- `docs/SYSTEM_DESIGN.md` §8 — Hosting, TLS, Backups and Production administration rows all change state
- `docs/LEARNING.md` — one entry per defect the walk found, in the existing pattern sections where one matches

- [ ] **Step 10: Commit**

```bash
git add docs/
git commit -m "docs: record the production walk, twelve of twelve

<summary of what the walk found, and what was interpreted rather than followed
literally>"
```

---

## Self-review

**Spec coverage.** Every decision in the spec maps to a task: 1 (private launch — no task, it is a scoping decision that removes work); 2 and 4 (CI, SHA tags) → Task 2; 3 (manual deploy) → Task 6's runbook, exercised in Task 8; 5 (migrations) → Task 3's `migrate` service, proven in Task 1 step 5 and Task 8 step 1; 6 (encrypted, escrowed backups) → Tasks 5 and 7 step 8; 7 (DNS only) → Task 7 step 4 and Task 4's single trusted subnet; 8 (`deploy/`) → Tasks 3 and 7 step 6; 9 (`sslmode=disable`) → Task 3's `.env.example` comment; 10 (rollback is image-only) → Task 6's runbook; 11 (mail budget) → recorded in the spec, no code, correctly no task; 12 (monitoring) → Task 7 step 10; 13 (starts empty) → Task 8's criterion 2 creating the first household through the real sign-up form.

**One deviation from the spec, deliberate.** The spec calls the environment file `deploy/.env.production`. This plan uses `deploy/.env`, because Compose only reads a file literally named `.env` for `${VAR}` interpolation, and `IMAGE_TAG` must interpolate for decision 4 to work at all. The alternative — passing `--env-file .env.production` on every command — puts a flag between the operator and every break-glass command at the moment they are least able to remember it. **The spec should be corrected to match rather than the other way round.**

**Placeholders.** Two remain and both are deliberate: `<the git sha>` and `<domain>` in the runbook, which are per-deploy values, and the commit message body in Task 8 step 10, which cannot be written before the walk has found whatever it finds.

**Type consistency.** The image names (`hearth-api`, `hearth-web`, `hearth-admin`), the binary paths (`/app/adminctl`, `/app/goose`, `/app/migrations`), the service names (`caddy`, `web`, `api`, `postgres`, `migrate`, `admin`), the subnet (`172.28.0.0/16` in both the Compose file and `nginx.conf`) and the variable names (`IMAGE_TAG`, `GOOSE_DBSTRING`, `AGE_RECIPIENT`, `RCLONE_REMOTE`, `HC_PING_URL`) are used identically in every task that names them.

**One assumption that would otherwise surface late.** `goose` reading `GOOSE_DRIVER` / `GOOSE_DBSTRING` / `GOOSE_MIGRATION_DIR` from the environment is what lets the `migrate` service work without a shell. If v3.27.3 does not honour them, `docker compose config` in Task 3 still passes and the failure would not appear until `migrate` exited non-zero against the live box. Task 1 step 5 therefore runs goose **both ways** — arguments and environment-only — so the assumption is tested on a laptop, and step 5 carries the fallback if it does not hold.
