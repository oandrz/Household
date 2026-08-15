#!/usr/bin/env bash
#
# Deploy one build to this box -- or refuse, for a reason you can read.
#
#   ./deploy.sh <git-sha>     deploy that build
#   ./deploy.sh --current     print what is running right now
#   ./deploy.sh --rollback    go back to the tag this script last replaced
#
# This replaces four commands typed by hand, two of which fail quietly:
#
#   * `sed -i "s/^IMAGE_TAG=.*/.../" .env` run from the wrong directory edits
#     nothing, prints nothing, and exits 0. The deploy then "succeeds" while
#     serving the old build.
#   * IMAGE_TAG=latest deploys nothing at all while looking like it worked. A
#     migration-only change produces a byte-identical api image, so Compose
#     does not recreate `api`, does not re-evaluate `depends_on: migrate`, and
#     the migration silently does not run.
#
# Both are refused below, before anything is changed.

set -euo pipefail

COMPOSE_FILE="docker-compose.prod.yml"
REGISTRY="ghcr.io/oandrz"
IMAGES=(hearth-api hearth-web hearth-admin)
PREVIOUS_TAG_FILE=".previous-image-tag"

# Run from this script's own directory rather than trusting the caller's. The
# dev stack in the repo root is a different Compose project with its own
# database and its own volume, and every path below is relative.
cd "$(dirname "$0")"

fail() { printf '\nREFUSING: %s\n' "$*" >&2; exit 1; }

# --- guards on where we are ------------------------------------------------
#
# Fail closed. These check we are pointed at the production stack, not merely
# that some compose file exists -- a dev checkout would satisfy the weaker test
# and then deploy into the wrong database.
[ -f "$COMPOSE_FILE" ] || fail "no $COMPOSE_FILE in $(pwd)"
[ -f .env ]            || fail "no .env in $(pwd) -- see README 'First install'"
grep -q '^name: hearth-prod' "$COMPOSE_FILE" \
  || fail "$COMPOSE_FILE is not the production stack (its project name is not hearth-prod)"

C=(docker compose -f "$COMPOSE_FILE")

current_tag() { grep '^IMAGE_TAG=' .env | cut -d= -f2-; }

# --- --current -------------------------------------------------------------
if [ "${1:-}" = "--current" ]; then
  echo "IMAGE_TAG: $(current_tag)"
  "${C[@]}" ps --format 'table {{.Service}}\t{{.Status}}'
  exit 0
fi

# --- work out the tag we are moving to -------------------------------------
if [ "${1:-}" = "--rollback" ]; then
  [ -s "$PREVIOUS_TAG_FILE" ] || fail "no $PREVIOUS_TAG_FILE -- nothing to roll back to"
  SHA="$(cat "$PREVIOUS_TAG_FILE")"
  echo "Rolling back to $SHA"
else
  SHA="${1:-}"
  [ -n "$SHA" ] || fail "usage: ./deploy.sh <git-sha> | --current | --rollback"
fi

# `latest` gets its own message because the reason is not obvious and the
# failure it causes is silent.
if [ "$SHA" = "latest" ]; then
  fail "IMAGE_TAG must never be 'latest'.
  A migration-only change builds a byte-identical api image. Without a changed
  tag Compose will not recreate api, will not re-evaluate depends_on: migrate,
  and the migration will not run -- while every check stays green.
  Use the git SHA that CI built."
fi

[[ "$SHA" =~ ^[0-9a-f]{7,40}$ ]] \
  || fail "'$SHA' is not a git SHA (expected 7-40 hex characters)"

# --- the tag must exist in the registry BEFORE .env is touched -------------
#
# Checked first so a typo leaves .env untouched and the running stack
# undisturbed, rather than leaving .env pointing at a tag that cannot be
# pulled -- which turns a typo into a broken file someone has to notice.
echo "Checking all three images exist at $SHA ..."
for img in "${IMAGES[@]}"; do
  if docker manifest inspect "$REGISTRY/$img:$SHA" >/dev/null 2>&1; then
    echo "  ok  $img"
  else
    fail "$REGISTRY/$img:$SHA is not in the registry.
  Has the images workflow finished for this commit? It only builds on main."
  fi
done

# --- warn if deploy/ itself is stale ---------------------------------------
#
# Deliberately a warning and not a `git pull`: this script is inside the
# checkout, and pulling here would rewrite the file that is currently
# executing. Bash reads scripts incrementally, so that can run half of one
# version and half of another.
if git rev-parse --git-dir >/dev/null 2>&1; then
  git fetch --quiet origin main 2>/dev/null || true
  if [ -n "$(git diff --name-only HEAD origin/main -- . 2>/dev/null)" ]; then
    echo
    echo "NOTE: deploy/ differs from origin/main. If this release changed the"
    echo "      Compose file, Caddyfile or these scripts, run 'git pull' and"
    echo "      then re-run this script."
  fi
fi

FROM_TAG="$(current_tag)"
echo
echo "  from: ${FROM_TAG:-<unset>}"
echo "    to: $SHA"
echo

# Record where we came from before changing anything, so --rollback works even
# if the rest of this run dies partway.
if [ -n "$FROM_TAG" ] && [ "$FROM_TAG" != "$SHA" ]; then
  printf '%s\n' "$FROM_TAG" > "$PREVIOUS_TAG_FILE"
fi

sed -i "s|^IMAGE_TAG=.*|IMAGE_TAG=${SHA}|" .env
[ "$(current_tag)" = "$SHA" ] || fail "failed to write IMAGE_TAG into .env"

"${C[@]}" pull
"${C[@]}" up -d

# --- verify, rather than assume --------------------------------------------
echo
echo "Verifying ..."

# migrate is one-shot: `docker compose ps` hides it once it exits, so -a is
# required or this check silently passes on a stack where it never ran.
MIGRATE_STATUS="$("${C[@]}" ps -a --format '{{.Service}} {{.Status}}' | grep '^migrate ' || true)"
case "$MIGRATE_STATUS" in
  *"Exited (0)"*) echo "  ok  migrations: $MIGRATE_STATUS" ;;
  "")             fail "migrate did not run at all. The stack is in an unexpected state." ;;
  *)              fail "migrations FAILED: $MIGRATE_STATUS
  api will not have started, so the previous container is still serving.
  Check '${C[*]} logs migrate', then './deploy.sh --rollback'." ;;
esac

RESTARTING="$("${C[@]}" ps --format '{{.Service}} {{.Status}}' | grep -i restarting || true)"
[ -z "$RESTARTING" ] || fail "a service is restart-looping:
$RESTARTING
  Check its logs, then './deploy.sh --rollback'."

DOMAIN="$(grep '^DOMAIN=' .env | cut -d= -f2- | tr -d '"')"
# Give the new api a moment to bind before calling a refusal a failure.
for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if BODY="$(curl -fsS --max-time 5 "https://${DOMAIN}/readyz" 2>/dev/null)"; then
    echo "  ok  https://${DOMAIN}/readyz -> ${BODY}"
    echo
    echo "Deployed ${SHA}."
    [ -s "$PREVIOUS_TAG_FILE" ] && echo "Roll back with: ./deploy.sh --rollback   (to $(cat "$PREVIOUS_TAG_FILE"))"
    exit 0
  fi
  sleep 3
done

fail "https://${DOMAIN}/readyz did not answer within 30s.
  Check '${C[*]} ps' and '${C[*]} logs api', then './deploy.sh --rollback'."
