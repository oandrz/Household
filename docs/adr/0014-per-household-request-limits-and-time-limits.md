# 14. Per-household request limits and time limits

**Status:** Accepted — 2026-10-17.

## Context

Hearth runs every household on one server and one database pool of ten
connections ([ADR 2](0002-first-production-host.md)). Sign-up is self-serve,
so a signed-in caller is not necessarily a trusted one.
[ADR 13](0013-per-household-limits-on-stored-data.md) bounded what one
household can *store*. This decision bounds how one household can *ask*:

- The only request limits were per address, on four public routes (sign-in,
  magic link, sign-up, Telegram start). Signed-in routes had none.
- Nothing bounded how long a request or a query could run, and nginx opened
  a new connection to the API for every request.

## Decision

**Each household has two request limits**, applied by one middleware,
`limitHousehold`, with the numbers in `ProductionHouseholdLimits`
(`api/internal/adapter/http/middleware_household_limit.go`):

| Limit | Number | Why this number |
|---|---|---|
| An allowance that refills | 60 requests, refilling at 10 a second | The home screen fires 11 calls at once, so 60 covers about five quick reloads. The heaviest read on capped data, the account list at 100,000 transactions, measured 18 ms (p50) on production's CPU and memory limits, so 10 a second keeps one household near a fifth of one CPU, a tenth of the box. |
| Requests running at once | 4 | The request pool has 10 connections; one household holds at most 4. |
| Waiting for a slot | at most 56 waiting (the allowance less the 4 running), each at most 5 s | Everything the allowance lets in waits its turn instead of failing, so a browser restoring several Hearth tabs loads them all; the home screen's 11 calls run in about three batches, which costs milliseconds. A waiter holds no database connection. |

- **Where it runs:** immediately after `requireSession`, in every signed-in
  route group, keyed by household. `401` always comes first, so a stranger
  never sees `429` and learns nothing about which routes exist. `429` comes
  before every other guard (`requireCSRF`, `requireOwner`,
  `requireCapability`, `requireCookieSession`, `requireFeature`,
  `requirePlatformAdmin`), because a request those will refuse has still
  cost the session check's reads. A refused request answers `429
  RATE_LIMITED`, "This household is sending too many requests. Wait a few
  seconds and try again.", with `Retry-After` in whole seconds.
- **A missing limiter fails closed.** `Deps.HouseholdLimiter` nil answers
  every signed-in request `500`, so a forgotten wiring breaks loudly instead
  of shipping unlimited. Router tests that are not about limits pass limits
  that never bind.
- **`hearthctl transaction import` sends 5 rows a second**, half the
  allowance, so the people using the app in the same household keep the
  other half while a file goes in. On a `429` it waits `Retry-After` (at
  most 30 s) and resends the same row, which its idempotency key makes safe,
  up to 10 times before listing the row as failed.

**Time limits fire innermost first,** so a slow request is answered by the
API's own JSON and never by nginx's HTML error page, which the app cannot
read:

| Layer | Limit | Where |
|---|---|---|
| One query on the request pool | 5 s | `postgres.RequestStatementTimeout`, set on every connection `postgres.Open` makes |
| One request inside the API | 15 s, from arrival, so waiting in the household's line counts | `httpadapter.RequestDeadline` |
| Go writing the answer | 20 s | `serverWriteTimeout`, `api/cmd/api/server.go` |
| nginx waiting for the API | 30 s | `proxy_read_timeout`, `web/nginx.conf` |

- A request past its deadline answers `503 TIMEOUT`, "Hearth took too long
  to answer. Try again in a moment.", logged at warn. A caller who left is
  logged at info. `logAndWriteInternal` decides both from the request's own
  context, not from the error, so an unrelated timeout inside a live request
  is still a `500` someone sees.
- A query stopped at 5 s stays a `500` logged as an unhandled error: every
  query a request makes reads capped data, so one running that long is a
  defect.
- The slowest normal request is a sign-in waiting in the password hasher's
  queue: about 8 derivations of 0.35 to 0.5 s on the production box, about
  4 s. 15 s leaves room; a test ties the two.

**nginx reuses its connections to the API**: up to 16 kept open, dropped
after 60 s idle. Go drops an idle connection after 90 s. The order matters:
if Go closed first, nginx could send a request down a connection Go is
already closing. Go's `IdleTimeout` must be set explicitly, because when it
is zero `net/http` uses `ReadTimeout` (15 s) instead. `cmd/api/server_test.go`
reads `web/nginx.conf` and checks this pair and the chain above, so changing
one side alone fails a test.

**The daily prune has its own pool**, `postgres.OpenJobs`: 2 connections
and 60 s per query, over the same database role. A delete over a table that
grew during a busy month may need longer than 5 s, and running it on its own
connections keeps it from taking ones a request is waiting for. `adminctl`
uses the same settings. The daily digest stays on the request pool; its
queries are household-sized reads of capped data.

## Who can reach a limit, and who is affected

Any member of the household, its API tokens, and `hearthctl`, all sharing one
allowance. Only that household is affected: it waits a few seconds, and no
other household notices. A large household using the app all at once, or a
household running an import while using the app, shares the same allowance;
the import's pacing leaves the app half of it.

## Accepted trade-offs

- **In memory, per process**, like the existing per-address limiters: a
  restart resets every household, and a second API container would double
  each household's limits.
- **The session check still runs on a refused request**: 3 to 5 small,
  indexed reads. Refusing before it would need limits in nginx.
- **A request the line refuses has still spent its token.** It was sent;
  counting it keeps a household that floods the line from also keeping its
  whole allowance.

## What this does not cover

- **Many households at once.** The limits bound one household; many
  households together are bounded by the sign-up limits.
- **The Telegram bot.** It is the second inbound edge
  ([ADR 8](0008-authorisation-at-each-channels-inbound-edge.md)), but one
  poller receives updates and handles them one at a time, so a household
  cannot run requests in parallel through it.
- **Limits in nginx**, which could refuse a request before the session
  check runs. They are left for later work on the public routes.

## Revisit when

- a second API container runs (move the limiter to a shared counter);
- limits in nginx arrive;
- the pool size, the caps in ADR 13, or the heaviest read changes;
- a real household meets a `429`.
