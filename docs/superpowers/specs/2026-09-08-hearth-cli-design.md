# Hearth CLI (`hearthctl`) — design

**Date:** 2026-09-08. **Status:** built in the same change as this spec.

## Why

The product owner wants Hearth to be operable by an AI agent, not only by a
person clicking. The cheapest, most universal way to hand an agent an
application is a command-line tool: every agent harness can run a shell
command, and a CLI is scriptable by cron, Make and humans as well. An MCP
server or a chat bot can wrap a CLI later; a CLI cannot be recovered from
either.

## Shape decision

`hearthctl` is a **plain HTTP client of the existing API**. It signs in the
way the browser does (`POST /api/v1/auth/sign-in`), keeps the two cookies the
server issues, and echoes the CSRF cookie back in `X-CSRF-Token` on every
write. It lives in `api/cmd/hearthctl` and imports **nothing** from
`internal/`.

Two alternatives were considered and rejected for now:

1. **A second `adminctl`-style tool wiring repositories directly.**
   Rejected. `CLAUDE.md`'s rule is "authorisation exists only in the HTTP
   layer". `adminctl` gets away with going around it because its commands
   are operator actions no member could ever be allowed to do through a
   route. "Insert a transaction" is the opposite: a member action with a
   guard stack (session, capability, owner, CSRF). A tool that bypasses that
   stack is a second, unguarded front door.
2. **Personal API tokens** (a new table, a Bearer middleware, a CSRF bypass
   path). Deferred, not rejected. Sessions live 30 days, so one `login` a
   month covers the automation case with zero new auth surface. Tokens are
   the follow-up when a headless machine or a second agent needs its own
   credential, and this CLI is unchanged when they arrive — only `login`
   grows a second way to obtain a credential.

## Decisions

1. **Base URL** comes from `--url` or `HEARTH_URL`, default
   `http://localhost:8080` (the Go server, not Vite's 5173).
2. **Credentials** live in `$XDG_CONFIG_HOME/hearth/` (default
   `~/.config/hearth/`), one file per base-URL host, mode 0600, so a
   localhost session and the production session never mix. The file holds
   the session cookie value, the CSRF cookie value, the expiry, and the
   signed-in email.
3. **Password never goes on the command line.** `login` reads it from
   `HEARTH_PASSWORD` or, failing that, from stdin (a hidden prompt on a
   terminal, a plain line otherwise, so `echo pw | hearthctl login` works
   in a script).
4. **Never retry sign-in, never auto-login.** The household lockout is real
   (`adminctl unlock-household` exists because of it). A 401 on any
   command answers "run hearthctl login" and exits 2; a 401 or 423 on
   `login` itself prints the API's body (it carries `attemptsRemaining`)
   and exits 2 with a message that says **do not retry** — never "run
   login again". An agent looping on a bad password would
   lock the household out of its own app.
5. **Never retry writes.** The API has no idempotency keys; a retried POST
   is a duplicate row. Known gap, recorded in the tracker.
6. **Money is minor units on the command line**, as on the wire
   (`--amount-minor 1234` is S$12.34). No decimal parsing, so no exponent
   table and no `float64` anywhere in the tool.
7. **Output contract.** Response JSON to stdout untouched; anything for a
   human to stderr. Exit codes: 0 success, 1 usage, 2 sign-in needed, 3 the
   API refused (4xx/5xx other than 401, body still printed), 4 could not
   reach the server. A 204 prints nothing and exits 0.
8. **Two layers of command.** `api <METHOD> <path> [--data JSON|@file]` is
   the escape hatch that reaches every route. Typed verbs exist only for
   the inserts the owner named — `transaction add`, `account add`,
   `bill add`, `goal add`, `category add` — plus `list <kind>` so an agent
   can resolve the ids those inserts need. Nothing else is typed.
9. **`routes` is the agent-facing manual.** It prints every route, its
   guard (who may call it) and its request shape. A test checks every path
   it prints exists in `router.go`, so it cannot silently rot.
10. **Writes need an owner.** A limited member's session gets 403 on every
    insert; the help text says so.

## Files

```
api/cmd/hearthctl/
  main.go          dispatch, usage, exit codes
  client.go        the HTTP client: cookies in, CSRF header out
  store.go         the credential file
  cmd_auth.go      login / logout / whoami
  cmd_api.go       api, list
  cmd_add.go       the five typed inserts
  routes.go        the route table `routes` prints
  *_test.go
docs/CLI.md        how an agent (or a person) uses it
docs/adr/0006-a-cli-as-the-automation-surface.md
```

## Testing

- `client_test.go`: a fake server asserts the session cookie arrives on
  every call, the CSRF header arrives on a write and not on a read, a 401
  maps to exit 2, a 204 prints nothing.
- `store_test.go`: the file is written 0600 and keyed by host.
- `cmd_add_test.go`: each typed verb builds the exact wire body the handler
  decodes (field names copied from the handler request structs).
- `routes_test.go`: every path in the table appears in `router.go`.
- Real environment: `hearthctl transaction add` against the local stack,
  then the row seen in the ledger in a browser.
