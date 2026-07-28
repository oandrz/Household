# HouseholdDashboard

Hearth — a shared dashboard for a household. Go backend on clean architecture,
React + TypeScript frontend, Postgres, Docker Compose.

## Read these first

- **`docs/LEARNING.md`** — every defect found while building this, and what each
  one teaches. **Consult it whenever something breaks or behaves oddly**; most
  failure modes here have been seen before and the patterns section explains
  why. Its "Before you call something done" checklist is the bar for finishing.
- **`docs/FEATURE_TRACKER.md`** — every feature in the design and whether it
  exists yet. **Check it before starting anything**, to see whether the work is
  already listed and what it depends on.
- **`docs/SYSTEM_DESIGN.md`** — component, flow and data diagrams. **Read it
  before changing anything structural**, and keep it true as you go (below).
- `docs/SKILL_TRACKER.md` — the project skills in `.claude/skills/`, and when to
  reach for each. Each exists because a real defect got through here.
- `docs/HANDOVER.md` — current state, what to build next, open items.
- `docs/GUIDE.md` — how to use the product.
- `docs/superpowers/specs/` and `docs/superpowers/plans/` — the specs and plans
  the work was built from.
- Explain everything in simple, straightforward and easy to be understood by the junior engineer

## Keeping the docs current

Three documents must not be allowed to go stale. Update them **as part of the
work**, before calling it finished — not as a separate tidy-up later.

**`docs/SYSTEM_DESIGN.md`** — use the **`maintaining-system-design`** skill. It
triggers on more than shipped features: a route or its guards changing, a table
or column, a port or interface, a new service or adapter, a reshaped request
flow, or any refactor across a boundary. This is the document a human engineer
onboards from, so an out-of-date diagram actively misleads — worse than none,
because it is believed. Change the prose under a diagram too; that is where the
non-obvious reasoning lives.

**`docs/FEATURE_TRACKER.md`**

- **When a feature is finished**, change its row from ⬜ to ✅. If it shipped
  with a known gap, mark it 🟡 and say what the gap is — a 🟡 with no
  explanation is worse than a ⬜.
- **When you build something the design does not describe**, add a row for it.
  The tracker is the map of what exists, not only of what was drawn.
- **When you discover a feature in the design that no row covers**, add it as ⬜.
  Finding a gap in the list counts as work on the list.
- **Update the summary table at the top.** Its columns must sum to the stated
  totals; if you change a row, recount rather than guessing.
- If a feature turns out not to be buildable, say so where the row is, with the
  reason — as the Money section does for automatic bank sync.

**`docs/LEARNING.md`**

- **When a piece of work is finished, add what it taught.** One entry per defect
  worth remembering: what broke, what the symptom looked like, and what would
  have caught it sooner.
- If the defect matches an existing pattern, add it there as evidence rather
  than starting a new section — the repetition is the point.

A defect nobody wrote down gets rebuilt, and a feature nobody ticked off gets
built twice.

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

## How code here is written

These are requirements, not preferences. A feature that works but nobody can
safely change is not finished.

### Clean architecture

Dependencies point inward and `make lint-arch` enforces it mechanically,
including in test files:

- `internal/domain` imports the standard library and nothing else
- `internal/usecase` may add `internal/domain`
- everything else lives under `internal/adapter/**` or `cmd/**`

No database, HTTP or third-party type crosses out of the adapter layer. A
missing row becomes `domain.ErrNotFound` at that boundary, never `pgx.ErrNoRows`
further up.

`internal/usecase/ports.go` is the contract between the layers. Its doc comments
are load-bearing — read them before writing a service or a repository.

### SOLID, as it applies here

- **Single responsibility** — one file, one job. When describing a file needs
  the word "and", split it.
- **Open/closed** — extend by adding an adapter, not by editing a service.
  `FXRateProvider` exists so a real rate source can arrive without touching
  its callers. `BankSyncProvider` does not exist yet: manual account entry
  needs no port, and one arrives when CSV import gives it a second
  implementation to abstract over — a port with one implementation and no
  second caller is the wrong shape.
- **Liskov** — an adapter honours its port's whole contract, errors included. A
  caller must never need to know which implementation it holds.
- **Interface segregation** — narrow ports for what a caller needs. Nine small
  repositories, not one object with forty methods.
- **Dependency inversion** — services depend on interfaces they declare;
  `cmd/api/main.go` chooses the implementations. This is why every service is
  testable against in-memory doubles.

### Readable by a junior engineer in their first week

- Names say what a thing is. Comments say **why** — never what the line already
  says.
- Small, focused files beat clever ones. If understanding a function needs three
  other files open, the seam is in the wrong place.
- Exported things carry their contract in a doc comment. `usecase/ports.go` is
  the model.
- Write every non-obvious decision down **at the point someone would try to
  change it**. Where a trade-off was accepted, say so and why — the lockout and
  magic-link comments are the pattern.
- Tests read as documentation: the name states the behaviour, the body shows it.
- No cleverness in security-sensitive code. Obvious and boring wins.

### Rules specific to this product

**Authorisation exists only in the HTTP layer.** No service takes an actor
parameter; services enforce what is *valid*, middleware enforces who is
*asking*. A route without its guard has no second line of defence.

**Money is `int64` minor units plus an ISO 4217 code, everywhere.** `float64`
never appears in a monetary path. Exchange rates are fractions, not scaled
decimals.

**Every 2xx except 204 carries a JSON body**, because the frontend's `apiFetch`
throws on an ok response it cannot parse.

**Fail closed on values you did not construct.** A `switch` over a type that
arrives from a database column or a request needs a `default` that refuses.

### Definition of done

`make lint && make test` green on the tree you are integrating, at least one new
test mutation-checked, `docs/FEATURE_TRACKER.md` and `docs/LEARNING.md` updated.
The full checklist is at the end of `docs/LEARNING.md`.

## Agent skills

### Issue tracker

Issues live as GitHub issues, managed via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five-role vocabulary; label strings equal role names. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
