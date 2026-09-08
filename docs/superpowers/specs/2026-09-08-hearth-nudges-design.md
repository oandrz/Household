# Hearth daily digest ("nudges") — design

**Date:** 2026-09-08. **Stage 6** of the automation roadmap
([ADR 6](../../adr/0006-a-cli-as-the-automation-surface.md) set the order):
the bot speaks first.

## What

Once a day, at a configured local time, each linked owner with Money gets
one Telegram message per household naming:

- bills overdue or due within the next three days, excluding autopay;
- budget categories at or past 80% of their cap this month.

Nothing to say means no message. Members stop it per chat with
`/nudges off` and resume with `/nudges on`.

## Decisions

1. **Rules, not a model.** The digest is deterministic text built from
   `BillService.List` and `BudgetService.Month`, the same reads the screens
   make. It costs nothing, needs no `OPENROUTER_*`, and is testable to the
   boundary (exactly 80%, exactly three days, cap of zero). A model may
   phrase it later; it will never decide what goes in it.
2. **At most once per day, by a claimed row.** `nudge_deliveries (chat_id,
   household_id, day)` is inserted *before* the send and deleted if the
   send fails — insert-first, the same shape as the transaction idempotency
   key. Two ticks, a restart mid-run, or two api instances cannot deliver
   twice; the primary key is the only lock. A quiet day keeps its claim, so
   a household is composed once per day, not once per tick.
3. **The recipients query is the authorisation for the outbound direction**
   ([ADR 8](../../adr/0008-authorisation-at-each-channels-inbound-edge.md)
   gains a consequence): `telegram_accounts ⋈ memberships` where
   `role = 'owner' AND 'money' = ANY(capabilities) AND nudges_enabled`.
   A digest carries money, so it goes only where `/balance` would already
   be answered. A postgres test plants a limited member with Money and an
   opted-out owner and expects neither.
4. **Keyed on household as well as chat.** One person can belong to two
   households; each household's digest is its own message and its own
   claim.
5. **Opt-out on the chat binding** (`telegram_accounts.nudges_enabled`),
   not the membership: it is "this phone stops buzzing", and it survives
   joining a second household. `/nudges` sits behind the same guard as
   `/spend`; on an install with no digest configured it says so rather than
   saving a choice nothing reads.
6. **Scheduler in-process, on a 15-minute tick**
   ([ADR 9](../../adr/0009-scheduled-work-runs-inside-the-api.md)).
   `NudgeDue(now, "HH:MM", loc)` says whether local time is at or past the
   clock and which local date to claim. A restart at 09:01 still delivers
   at 09:15; every later tick is one `INSERT … ON CONFLICT DO NOTHING` per
   recipient. Deliveries older than a month are pruned on the same tick.
   The tick recovers per tick, like the poller per update, so a panic
   costs one attempt and not the digest until the next deploy. **Two dates
   in play:** `NudgeDue` yields the local calendar date; `RunOnce` turns it
   into UTC midnight — the shape `billStartOfDay` and the parsed budget
   month already use — for both the claim and the reads. Passing the zoned
   instant through would make a UTC+8 tick before 08:00 read yesterday.
7. **Configuration:** `NUDGES_AT` and `NUDGES_TIMEZONE`, both or neither,
   parsed at boot (a bad clock or an unknown zone refuses to start), and
   **refused without Telegram** — a digest with no channel is a
   misconfiguration, not "off". **One zone for the whole install** is the
   named gap: right for one household, wrong the day a second one signs up
   from another zone; the column belongs on `households` when that day
   comes.
8. **Autopay bills are excluded.** Reminding someone to pay a bill that
   pays itself is the noise that gets the digest muted on day two.
9. **Three days, not `DueSoon`'s thirty.** That heading organises a screen;
   this interrupts a phone.
10. **Integer arithmetic for the 80% line:** `spent × 100 ≥ cap × 80`, no
    float near money.
11. **Goals are out** of the first cut; a missed planned contribution is a
    natural third section when someone asks for it.

## Files

```
api/migrations/00017_nudges.sql                      table + nudges_enabled column
api/internal/adapter/postgres/queries/nudge.sql      recipients, claim, release, toggle, prune
api/internal/adapter/postgres/nudge_repo.go          NudgeRepository
api/internal/usecase/nudge.go                        NudgeService: Compose, RunOnce, NudgeDue
api/internal/usecase/ports.go                        NudgeRecipient, NudgeRepository
api/internal/usecase/telegram_command.go             SetNudges
api/internal/adapter/telegram/commands.go            /nudges on|off
api/internal/config/config.go                        NUDGES_AT, NUDGES_TIMEZONE
api/cmd/api/main.go                                  runNudges, wiring
```

## Testing

Postgres: the recipients predicates (limited-with-Money, opted-out; "owner
without Money" is impossible by `owners_hold_all_capabilities`, noted in
the test), claim twice = true then false, release then claim again, prune.
Usecase: compose at the boundaries, nothing-to-say, claim-before-send across
two ticks and the next day, a failing sender releases and the loop goes on,
a quiet day keeps its claim, `NudgeDue` across SGT midnight and a bad clock.
Config: both-or-neither, needs Telegram, bad clock, unknown zone. Telegram:
`/nudges on|off|other`, a limited member never reaches the toggle.
Mutation-checked: the three-day horizon (4 went red), the 80% comparison
(`<=` went red), the claim check (ignoring it went red).

**Walked live 2026-09-08:** a bill due in two days inserted for the linked
household, `NUDGES_AT=00:01` so the first tick fires at boot,
`docker compose up -d api`; log `nudge sent`, Telegram accepted the send,
one `nudge_deliveries` row; `docker compose restart api` sent nothing and
the row count stayed one. Not yet seen: the rendered message on the phone,
and `/nudges off` → `on` sent live (the toggle is covered by the adapter
and postgres tests).
