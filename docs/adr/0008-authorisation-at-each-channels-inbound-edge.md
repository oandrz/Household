# 8. Authorisation lives at each channel's inbound edge — Telegram commands

**Status:** Accepted — 2026-09-08.

## Context

`CLAUDE.md` states a rule this product has held since slice 1: *authorisation
exists only in the HTTP layer*. No service takes an actor; services enforce
what is valid, middleware enforces who is asking. Every guard is in
`internal/adapter/http`, and a route without its guard has no second line of
defence.

Telegram chat commands (`/spend`, `/income`, `/balance`, `/recent`) are the
first way to **write** to the product that does not arrive over HTTP. A
message reaches the poller, not a router; there is no middleware chain to
put `requireOwner` in. The rule as written cannot be followed literally, and
the choice was between bending it silently and restating it.

Three shapes were considered:

1. **Put the check in the service.** `TelegramCommandService.LogSpend` would
   resolve the chat to a membership and refuse a non-owner. Rejected: that is
   a service taking an actor, which the rule forbids for a reason — a
   service that checks one caller's role invites the next service to check
   a different way, and the guards drift.
2. **Have the bot call the HTTP API** with a per-user token (ADR 7) so the
   existing middleware guards it. Rejected: a loopback HTTP call from inside
   the same process, and a token the bot must mint for every linked chat —
   a hidden credential store with the same lifetime and revocation questions
   tokens already answer once. It would satisfy the rule's letter by adding
   a second copy of the problem.
3. **Guard at the channel's edge.** The Telegram adapter's `Commander`
   resolves chat → user → membership (through a usecase resolver that
   decides nothing) and refuses anyone who is not an owner with the money
   capability, *before* any service is called — exactly what
   `requireSession → requireCapability → requireOwner` does for a request.
   Chosen.

## Decision

The rule is restated, not overridden:

> **Authorisation lives at the inbound edge of each channel, never in a
> service.** For HTTP that edge is the router and its middleware. For
> Telegram it is `adapter/telegram/commands.go`'s `Commander`. A third
> channel gets its own edge and its own guard, and services stay ignorant
> of all of them.

`CLAUDE.md`'s sentence should be read with this ADR beside it. The rule's
purpose — one place per channel where "who may do this" is decided, and no
service that can be reached without passing through it — is intact.

Two supporting decisions:

- **The same stack, both halves.** The Commander checks the capability
  *and* the role, as `router.go` stacks them for the money routes, and for
  the same reason: neither may lean on an invariant enforced elsewhere.
- **Every command is idempotent for free.** Telegram redelivers unacked
  updates after a restart (the poller's own comment). Each `/spend` carries
  `Idempotency-Key = telegram-update-<update_id>` (the stage-3 mechanism),
  so a redelivered command is a replay, not a second row.

## Consequences

- A limited member, or an owner without Money, gets one sentence and no
  service call. An unbound chat gets one sentence that does not reveal
  whether the chat, the user or the membership was the missing piece.
- The bot's writes are exactly as guarded as the browser's. A future
  Telegram capability for limited members is a change to one `if` in one
  file, mirrored on one route group.
- Chat commands were **walked against a real bot on 2026-09-08**, but only
  after the owner created a second, development bot. The first attempt used
  the production token from the local `.env`: another poller held it, the
  dev api logged `terminated by other getUpdates request` every minute, and
  the owner's `/balance` went to the other process and vanished. Telegram
  hands each update to exactly one `getUpdates` caller, so a dev stack and a
  deployed one can never share a bot. The dev bot lives in the local `.env`;
  the production token stays in `deploy/.env` on the box.
