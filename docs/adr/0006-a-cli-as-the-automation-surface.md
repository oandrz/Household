# 6. A CLI over the existing API is the automation surface

**Status:** Accepted — 2026-09-08.

## Context

The product owner wants Hearth to be operable by an AI agent as well as by a
person: inserting transactions, bills and goals from a conversation, a
script or a schedule rather than a form. The question was what shape to give
an agent. Three were on the table:

1. A command-line client of the existing HTTP API.
2. Personal API tokens: a new table, a Bearer-token middleware, and a path
   that skips CSRF for non-browser callers.
3. An `adminctl`-style binary that wires the repositories directly and
   writes rows without going through HTTP at all.

## Decision

**Option 1, and only option 1, for now.** `api/cmd/hearthctl` signs in with
`POST /auth/sign-in` like the browser does, keeps the session and CSRF
cookies in a 0600 file, and echoes the CSRF cookie as a header on every
write. It imports nothing from `internal/`. `docs/CLI.md` is its manual and
`docs/superpowers/specs/2026-09-08-hearth-cli-design.md` the spec.

Option 3 is **rejected**, not deferred. `CLAUDE.md`'s rule is that
authorisation exists only in the HTTP layer; `adminctl` is allowed around it
because its commands are operator actions that no member could ever be
permitted to do through a route. "Insert a transaction" is the opposite —
a member action with a four-deep guard stack (session, capability, owner,
CSRF). A second binary that bypasses that stack is an unguarded front door,
and every future guard would have to be built twice.

Option 2 is **deferred**. Sessions last 30 days, so the automation case is
covered by one interactive login a month with zero new authentication
surface. Tokens become worth their table and middleware when a headless
machine, or a second agent, needs a credential that is not a person's
session. When they arrive, the CLI does not change shape: `login` gains a
second way to obtain a credential and `client.go` a second header to send.

## Consequences

- An agent can do exactly what the signed-in member can, no more. A limited
  member's session gets the same 403s through the CLI as in the app. This is
  a feature: there is one authorisation model, not two.
- The CLI never retries a sign-in (the household lockout is real). It also
  never retries a write on its own — but a write can now be *made* safe to
  retry: **amended 2026-09-08, the same day**, `POST /transactions` accepts
  an `Idempotency-Key` header (spec
  `docs/superpowers/specs/2026-09-08-hearth-idempotent-import-design.md`),
  `transaction add --key` and `transaction import` send one, and the
  "duplicate on retry" gap this ADR originally recorded is closed for
  transactions. Other creates still have no key; the tracker says so.
- `hearthctl routes` is hand-maintained prose an agent trusts, so a test
  diffs it against `router.go` in both directions. Adding a route without
  adding its line fails `go test`.
- The credential file is a bearer secret. It is 0600, per host, and written
  under `~/.config/hearth`; a compromised laptop is a compromised session,
  the same as a browser's cookie jar.
- An MCP server, a chat bot or a scheduled agent can wrap this CLI. None of
  them needs a new server-side surface.
