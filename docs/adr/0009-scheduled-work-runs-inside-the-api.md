# 9. Scheduled work runs inside the api process

**Date:** 2026-09-08. **Status:** accepted.

## Context

The daily digest (spec `2026-09-08-hearth-nudges-design.md`) is Hearth's
first piece of work that happens on a clock rather than in answer to a
request or a chat message. Something has to wake up once a day and decide
who to message. The box already runs cron for backups
(`deploy/crontab.example`), so cron was the obvious first thought.

## Decision

The api process owns its own schedule: a goroutine beside the Telegram
poller ticks every fifteen minutes, and a claimed row in `nudge_deliveries`
decides whether a tick does anything. Configuration is two environment
values (`NUDGES_AT`, `NUDGES_TIMEZONE`) read by `config.Load` like every
other optional feature.

## Why not cron on the box

- **A second way in.** Cron would need either a new `adminctl` verb that
  talks to the database directly — the path ADR 6 rejected for anything
  that acts on a member's behalf — or an HTTP endpoint with a shared
  secret, which is a new credential to provision, rotate and document.
- **Two places to configure one feature.** The bot token, the digest time
  and the zone would live in `.env` and the crontab respectively, and
  `deploy.sh` touches only one of them.
- **Nothing to gain.** The tick is one cheap query per recipient; the
  process is already long-lived; the poller already proves the shape.

## Consequences

- **Exactly-once comes from the database, not the scheduler.** Two api
  instances would both tick and both try to claim; the primary key lets one
  win. The scheduler is allowed to be sloppy because the ledger is not.
- **A restart delays the digest by at most fifteen minutes**, never skips
  it, because the tick asks "is it past the time today" rather than "is it
  the time".
- **One process, one clock.** The zone is global. Per-household zones need
  a column, not a scheduler change — the tick would simply ask `NudgeDue`
  per recipient.
- The same goroutine is the home for the next scheduled thing (a monthly
  budget rollover reminder, a weekly recap) — one ticker, several `Due`
  checks — before anyone reaches for cron again.
