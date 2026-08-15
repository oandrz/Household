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

# Timestamp to the second, not just the date, so no two runs ever write the
# same key. That matters because R2 Bucket Lock makes objects immutable for a
# retention window and refuses OVERWRITES as well as deletes -- with a
# date-only name, any second run on the same day (a manual backup before a
# risky change, say) would fail against a locked object. The lock is worth
# having: the token on this box can delete, so without it a compromised box
# can erase the backup history.
#
# `T%H%M%SZ` rather than %H:%M:%S -- colons are legal in S3 keys but a menace
# in shells, URLs and on any filesystem the file gets copied to during a
# restore. The format still sorts lexicographically, so `rclone lsf | sort |
# tail -1` is the newest backup.
stamp="$(date -u +%Y-%m-%dT%H%M%SZ)"
file="hearth-${stamp}.sql.gz.age"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# pg_dump runs inside the container because postgres is not published.
docker compose -f docker-compose.prod.yml exec -T postgres \
  pg_dump -U hearth -d hearth --format=plain --no-owner --no-privileges \
  | gzip -9 \
  | age -r "$AGE_RECIPIENT" -o "$tmp/$file"

# pipefail already aborts the realistic failures -- bad auth, no container,
# connection refused. What it does not catch is a stage exiting 0 on
# truncated input, such as the connection dropping mid-dump: gzip and age both
# happily produce a small-but-valid file from a short read. Better to fail
# loudly than to upload a file that looks like a backup.
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
