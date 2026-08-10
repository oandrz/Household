# 1. Optimise deployment for exit cost, not provider permanence

**Status:** Accepted — 2026-08-10

## Context

The product owner intends to run Hearth for roughly **forty years**. It holds a
household's accounts, ledger, budgets, goals and bills; the value of the data
compounds, so losing it in year 30 is worse than never having started.

Forty years is longer than most hosting companies have existed. AWS is around
twenty years old, Hetzner around thirty, Vercel about ten, Supabase about six.
Choosing a provider "because it will still be here" is a prediction nobody can
make. Over that horizon we will move hosts three to five times whether we plan
to or not — a provider dies, gets acquired, raises prices, or drops the region
we are in.

There is also a cost ceiling. The owner already pays roughly S$20/month on AWS
for infrastructure that does not run this product, and wants the real hosting
bill to be lower than that.

## Decision

**We choose hosts by how cheaply we can leave them, not by how long we expect
them to last.**

Concretely, Hearth runs as the Docker Compose stack it already is, on a plain
Linux host, with a Postgres we own. We do not adopt:

- managed authentication (Supabase Auth, Cognito, Firebase) — identity is ours
- proprietary database extensions or vendor-specific SQL
- a serverless-only runtime that cannot run a persistent process
- any vendor SDK in a monetary or identity code path
- a topology that forces the frontend and API onto different origins

A host is then a commodity. Moving is a `pg_dump`, a `docker compose up` and a
DNS change — measured in hours, not months.

## What this protects

These properties are already true of the codebase. The decision above exists to
stop them being traded away for a short-term convenience:

- **Plain Postgres.** goose migrations, sqlc-generated queries, no extensions.
  Any Postgres anywhere can take the dump.
- **Instants are `timestamptz`; calendar facts are `date`.** Every migration
  follows this, and `api/migrations/00005_transactions.sql` writes down why:
  "18 July" is a fact about a day, not a moment in time. Over forty years
  timezone rules change repeatedly, and naive `timestamp` columns would let
  each `tzdata` update silently rewrite history.
- **A static Go binary.** `CGO_ENABLED=0`, no system libraries to match. Go's
  compatibility promise is the strongest in mainstream languages.
- **Identity is ours.** Argon2id hashing, a sessions table, magic links,
  invites, the lockout policy, CSRF — all in `internal/`. No vendor holds a
  user record.
- **Money is `int64` minor units plus an ISO 4217 code.** No floating-point
  drift to compound across four decades.
- **One origin.** Session and CSRF cookies are `SameSite=Lax`
  (`middleware_session.go`, `middleware_csrf.go`) and there is no CORS
  middleware anywhere, because nginx serves the bundle and proxies `/api` on
  the same origin. This is a security simplification, and it is also why a
  frontend-only host is not an option for us.

## Consequences

**We own the operational work.** Postgres major upgrades, backups, TLS, OS
patching. That is the price of the low exit cost, and it is deliberate.

**A backup is only real once restored.** Dumps are plain SQL (not custom
format) so a human can read them and any future Postgres can load them, and at
least one copy lives off the hosting provider entirely — a lapsed payment card
takes the server and its snapshots together.

**We must keep refusing things that raise exit cost.** When a vendor feature
would be convenient, the question is not "does this work" but "what does it
cost to leave." Three examples already declined, recorded so the reasoning is
not re-litigated from scratch:

| Rejected | Why it raises exit cost |
|---|---|
| Vercel for the frontend | Forces the SPA and API onto different origins. `SameSite=Lax` cookies stop being sent, so it needs `SameSite=None`, a CORS middleware that does not exist, and a CSRF re-review — a rewrite of security-sensitive code, permanently carried |
| Supabase | Cannot host the Go API at all (Edge Functions are TypeScript/Deno), so it is a database bill *on top of* a compute bill. Its main value is an auth layer we already built and would have to delete to benefit from |
| Cloud Run / Lambda / any serverless runtime | The per-IP sign-up rate limiter is an in-memory token bucket in the HTTP layer. Across N request-scoped instances it fragments into N buckets and stops limiting — a correctness regression, not just a port |

**Free tiers get worse over this horizon, not better.** Oracle Always Free
reclaims instances idle over a 7-day window, and Supabase's free plan pauses a
project after a week of inactivity. A household dashboard is idle most nights.
A saving that turns into an outage every quiet month is a forty-year liability.

## Revisit this when

- Managed Postgres with point-in-time recovery becomes worth its price — the
  exit cost stays low as long as it is *plain* Postgres, so this does not
  contradict the decision.
- The same-origin constraint is deliberately re-decided, with the cookie and
  CSRF work costed in. Until then, treat "just put the frontend on a CDN" as
  the architectural change it is.

## See also

- [ADR 2 — first production host](0002-first-production-host.md), which applies
  this decision to a concrete purchase.
- `docs/HANDOVER.md` §5, "Before this is deployed anywhere real".
