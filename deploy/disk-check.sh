#!/bin/sh
# Hearth disk alarm, run by cron every 15 minutes (see crontab.example).
#
# The database lives on the box's only disk, and when that disk fills
# Postgres stops. This pings a healthchecks.io check: a normal ping while the
# disk is under the limit, a /fail ping once it is over, so the owner is
# mailed while there is still room to act. If the script stops running at
# all, the missed pings mail the owner too.
set -eu

limit="${DISK_ALARM_PERCENT:-80}"
used=$(df --output=pcent / | tail -1 | tr -dc 0-9)

if [ "$used" -gt "$limit" ]; then
  curl -fsS -m 10 --retry 3 --data-raw "disk ${used}% full (alarm at ${limit}%)" \
    "$HC_DISK_PING_URL/fail" >/dev/null
  echo "$(date -u +%FT%TZ) disk ${used}% full, alarm sent"
else
  curl -fsS -m 10 --retry 3 "$HC_DISK_PING_URL" >/dev/null
fi
