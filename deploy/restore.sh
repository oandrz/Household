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

# psql runs inside postgres:17-alpine rather than as a host binary: the box's
# later provisioning task installs only age, rclone and Docker, no Postgres
# client, and a client older than the dump's v17 server would be reading
# forward. Running it in the same image the server itself uses means the
# client version always matches and the box needs no extra package.
# --network host so a DSN naming "localhost" (as the drill invoking this
# script does) reaches a port this host published, exactly as a
# host-installed psql would have.
psql_image="postgres:17-alpine"

age -d -i "$identity" "$file" | gunzip \
  | docker run --rm -i --network host "$psql_image" psql "$target" -v ON_ERROR_STOP=1 -q

echo "--- restored row counts ---"
docker run --rm --network host "$psql_image" psql "$target" -At -c "
  select 'households', count(*) from households
  union all select 'users', count(*) from users
  union all select 'transactions', count(*) from transactions
  union all select 'bills', count(*) from bills;"
