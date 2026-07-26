# HouseholdDashboard

Hearth — a shared dashboard for a household. Go backend on clean architecture,
React + TypeScript frontend, Postgres, Docker Compose.

## Read these first

- **`docs/LEARNING.md`** — every defect found while building this, and what each
  one teaches. **Consult it whenever something breaks or behaves oddly**; most
  failure modes here have been seen before and the patterns section explains
  why. Its "Before you call something done" checklist is the bar for finishing.
- `docs/HANDOVER.md` — current state, what to build next, open items.
- `docs/GUIDE.md` — how to use the product.
- `docs/superpowers/specs/` and `docs/superpowers/plans/` — the specs and plans
  the work was built from.

## Keeping the learning log current

**When a piece of work is finished, add what it taught to `docs/LEARNING.md`
before closing it out.** One entry per defect worth remembering: what broke,
what the symptom looked like, and what would have caught it sooner. If the
defect matches an existing pattern, add it there as evidence rather than
starting a new section — the repetition is the point.

A defect nobody wrote down gets rebuilt.

## Running it

```bash
make dev     # everything, logs tailed — http://localhost:5173
make seed    # the design's household; prints sign-in details and an invite URL
make test    # Go suite (needs Docker) plus the frontend tests
make lint    # arch lint, frontend typecheck, eslint, go vet
```

Bare `make` lists every target. Mail is caught by Mailpit at
http://localhost:8025.

The Go suite uses testcontainers and needs a Docker socket. On the original
machine that is colima:

```bash
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
```

## Architecture rules

Dependencies point inward and `make lint-arch` enforces it mechanically,
including in test files:

- `internal/domain` imports the standard library and nothing else
- `internal/usecase` may add `internal/domain`
- everything else lives under `internal/adapter/**` or `cmd/**`

`internal/usecase/ports.go` is the contract between the layers. Its doc comments
are load-bearing — read them before writing a service or a repository.

**Authorisation exists only in the HTTP layer.** No service takes an actor
parameter; services enforce what is *valid*, middleware enforces who is
*asking*. A route without its guard has no second line of defence.

Money is `int64` minor units plus an ISO 4217 code, everywhere. `float64` never
appears in a monetary path.

## Agent skills

### Issue tracker

Issues live as GitHub issues, managed via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five-role vocabulary; label strings equal role names. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
