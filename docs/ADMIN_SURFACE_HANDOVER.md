# Hearth — the platform admin surface, handover

Written 2026-09-02, when the first slice of the admin surface finished on
branch `admin-surface`. This is the document to read before picking that work
back up. It assumes you have read `CLAUDE.md` and nothing else.

The product-wide handover is `docs/HANDOVER.md`; this one covers only the admin
surface and is deliberately self-contained.

---

## 1. Status

**Branch `admin-surface`, open as
[PR #15](https://github.com/oandrz/Household/pull/15). Not merged. Not
deployed.** Nothing in this work has ever run in production.

- `make lint` and `make test` both exit 0 on the branch head: 11 of 11 Go
  packages, 716 frontend tests, arch lint clean.
- A browser walk against the running dev stack passed **15 of 15** criteria,
  recorded criterion by criterion in
  `docs/superpowers/plans/2026-09-02-hearth-admin-surface-verification.md`.
- Every task was reviewed, and at least one test per task was mutation-checked
  — broken on purpose, watched go red, restored.

The work was built from
`docs/superpowers/specs/2026-09-01-hearth-admin-surface-design.md` (the spec)
via `docs/superpowers/plans/2026-09-01-hearth-admin-surface.md` (the plan).
**The plan deliberately covers only rollout steps 1–3 of the spec's six.** See
§3 for what that leaves.

## 2. What exists

Five things, in the order they depend on each other.

**The authorization axis.** Platform admin is orthogonal to household `Role`
and `Capability`: those answer "what may this member do inside their own
home", this answers "who runs this install". A `platform_admins` row, checked
by `requirePlatformAdmin` (`api/internal/adapter/http/middleware_admin.go`),
which answers **404, not 403** — to anyone who is not an admin the surface is
indistinguishable from a typo, and the same `writeNotFound` serves both the
hidden route and the genuinely absent one, so they cannot drift apart. A
lookup *failure* is a 500, deliberately, so a database outage never reads as
"you are not an admin".

Admins are created only by `adminctl` — `grant-platform-admin`,
`revoke-platform-admin`, `list-platform-admins` — and there is no HTTP route
anywhere that can mint one. A reviewer verified by grep that
`PlatformAdminRepository.Grant` has exactly one call site.

**The re-authentication grant.** `POST /api/v1/admin/session` verifies the
password again and stamps `sessions.admin_grant_expires_at` 30 minutes out. It
lives on the session row rather than in a second cookie, so sign-out revokes it
for free, and it is **not** extended by activity. Failures are counted in
`admin_reauth_attempts`, a ledger deliberately separate from `login_attempts`
— that table's lockout is household-scoped, so feeding admin mistypes into it
would let an operator lock their whole household out of the product by
fumbling a password on a screen nobody else can see. `adminctl unlock-admin` is
the way back in, and the walk proved the separation holds.

**The audit log.** `admin_audit_log`, written by `auditAdmin` middleware
wrapping the whole subtree, **before** the handler runs, reads included. A
handler that forgets to log is the failure mode; middleware cannot forget. If
the log cannot be written the surface closes with 503 — an admin surface that
works fine with auditing silently off is the state the table exists to prevent.
The table is append-only: there is no delete route anywhere in the product and
`adminctl prune` does not touch it.

**Feature flags.** The registry is compile-time
(`api/internal/domain/featureflag.go`) — adding a flag is one `const` and one
line in `AllFlags()`. Override rows exist only where somebody overrode
something; resolution walks the definitions, never the override maps, so a row
naming a flag this build no longer defines can never enable anything.
Precedence is household override → global override → compile-time default.
`requireFeature` enforces with a 404, and handles pre-auth routes itself by
resolving the global layer alone. Four flags gate real routes today:
`signups_open`, `telegram_sign_in`, `notification_delivery`,
`family_calendar`.

**The admin UI.** `/admin` lazy-loaded so no household member downloads it,
with its own operator chrome. `AdminGate` recognises four codes and renders
not-found for anything else — the only path to the children is `error === null`.
`apiFetch` carries a three-code allowlist so an expired grant shows the
password prompt instead of bouncing the operator to sign-in.

Where to start reading: `middleware_admin.go`, then `router.go`'s `/admin`
subtree and its comment, then `usecase/admin.go`, then
`web/src/features/admin/`.

## 3. What does NOT exist

All are specified in full and none is started. This is the honest gap between
what was asked for and what shipped.

**As of 2026-09-02 these are the product's next work**, ahead of any further
household feature — the product owner's call, recorded with its cost in
`docs/FEATURE_TRACKER.md`'s "Suggested order", where all four now have ⬜ rows
in section 9. The recommended sequence is **audit screen → households and
metrics → message inspector → database browse**: smallest first, the
infrastructure dependency last, and the browse last of all so the grant and the
audit log get real use before the surface that can read every household's
finances arrives.

**A read-only database browse — spec §4.** This was one of the three things
originally asked for. The spec has the whole design: a separate `hearth_readonly`
Postgres role granted `SELECT` only, reached through its own pool from
`DATABASE_READONLY_URL`; table names validated against `information_schema`
rather than interpolated; `statement_timeout` and a page cap; redaction that
fails closed by column *type* (`bytea`) as well as by name, so a secret column
added next year is redacted before anyone remembers this file. **Unset config
means the panel is unavailable — never a fallback to the read-write pool.**
The role is created during provisioning, not by a migration.

This is the natural next slice, and it is the reason the previous three exist:
it cannot be built safely without the re-auth grant and the audit log.

**An outbound message inspector — spec §5.** Proxies Mailpit's HTTP API rather
than storing links, because every token in this schema is stored hashed and
inventing a raw-link store to solve a convenience problem is the wrong trade.
It would close a real, documented pain: `deploy/README.md` describes handing
someone an invite by opening an SSH tunnel to Mailpit and copying the link by
hand.

**Household list and metrics — spec §6.** Also the prerequisite for finding A
below.

**An `/admin/audit` screen.** The log is written and readable through the
repository; it has no UI.

## 4. Running and testing it

```bash
make dev                                    # http://localhost:5173
```

Sign in as `andreas@hearth.family` / `hearth-dev-password` (from
`usecase/seed.go`). That account already holds a `platform_admins` row on the
development database. The **Admin** link is in the sidebar below Settings.

```bash
docker compose exec api go run ./cmd/adminctl grant-platform-admin --email=<addr>
docker compose exec api go run ./cmd/adminctl revoke-platform-admin --email=<addr>
docker compose exec api go run ./cmd/adminctl list-platform-admins
docker compose exec api go run ./cmd/adminctl unlock-admin --email=<addr>
```

**Reset the flags** to compile-time defaults at any time:

```bash
docker exec hearth-postgres-1 psql -U hearth -d hearth \
  -c "delete from feature_flags; delete from household_feature_flags;"
```

**Note for the browser walk:** the segmented flag control sits at the far right
of each row in small muted text and is easy to miss entirely — see finding B.
The Go suite needs Docker and the environment exports in `CLAUDE.md`.

## 5. Open items

### Parked, with a recommendation

Two prose inaccuracies survived the final review. Both are real and neither
affects behaviour; they were parked rather than opening a third fix round.

1. `api/internal/adapter/http/auth_api_test.go` — the two floor comments attach
   the counts 59/41 to "by the time anyone checked again". The only commit
   where anyone re-ran the walk is `c56f7be`, where the real counts were 62/44;
   59/41 is the count one commit earlier. Misleads by one commit.
2. `docs/LEARNING.md` pattern 16's ninth instance says a fix wave carried "one
   more unverified number into the tree". Nothing was carried into the tree —
   the wrong number was in a prompt and was caught in review. Every other
   instance in that catalogue is a claim that *shipped*. **Recommendation:
   demote it to a note under the closing paragraph and revert "All nine
   instances" to eight.**

### From the browser walk

- **A — the admin screen offers no way to create a household override.** Only a
  global On/Off, and Remove on overrides that already exist. Per-household
  targeting is the entire justification for the two-layer flag model and is
  currently reachable only by a hand-written `PUT`. Judged *not* a spec
  violation (§3.6 does not require a creation control, §9 calls this slice
  shippable alone, and there is no household list yet to build a picker
  against) and recorded as a 🟡 gap in `docs/FEATURE_TRACKER.md` §9. **It
  closes naturally with spec §6.**
- **B — the flags page's only interactive control is easy to miss.** 12px muted
  text on a transparent ground, right-aligned far from its label. The contrast
  itself passes AA (~5.4:1) — the problems are placement and that the "Default"
  state has no accessible name. Do not "fix" this by changing colours.
- **C** — the re-auth error reads "That email or password is incorrect." on a
  screen with no email field. Fixing it properly needs a distinct sentinel.
- **D** — a locked operator cannot discover the lock without submitting, and
  submitting extends it. Accepted and documented, matching `AuthService.SignIn`.
- **E** — in development `admin_audit_log.ip` is the proxy container's address;
  production's real-IP configuration is in `web/nginx.conf`. Not a defect.

### Known, deferred

- **No test asserts the audit row's `Target`/`Detail` values in the HTTP layer
  beyond the one added in the final wave** — that one covers the regression
  that shipped; there is no broader coverage of the field's shape.
- A malformed `householdID` on a flag override answers **500** rather than a
  4xx: `uuid()` yields the zero UUID on a parse failure and `translate` has no
  case for a foreign-key violation. The fix touches `translate`, which every
  repository shares — hence deferred, not because it is right.
- `TestAHouseholdOverrideEnablesTheFlagForThatHousehold` proves one household is
  enabled, not that others are unaffected; the harness has no second household.
- `SetHousehold`'s `ON CONFLICT` branch is never exercised twice by a test.
- `runListPlatformAdmins` has a `(no note)` fallback but none for a null email,
  and `--note` accepts newlines that would break its one-line-per-admin output.

## 6. Decisions not to undo without a conversation

These were each argued and are load-bearing. Changing one is fine; changing one
*by accident* is not.

- **404, never 403, for a non-admin** — and a 500, never 404, for a lookup
  failure.
- **`requireCSRF` sits innermost of the `/admin` guards**, behind
  `requirePlatformAdmin` and `auditAdmin`, so a forged admin request still
  leaves an audit row. `TestEveryMutatingRouteRequiresCSRF` grants its owner
  platform admin for this reason; the setup line is load-bearing.
- **The two lockout ledgers never cross.** `login_attempts` is household-scoped,
  `admin_reauth_attempts` is user-scoped.
- **`admin_audit_log.actor_user_id` does not cascade** on user delete. Deleting
  a user with audit history must fail loudly rather than quietly erasing the
  record of what they did.
- **`deps.Admin` is load-bearing for every authenticated request** —
  `requireSession` resolves flags through it. Do not add a nil-check fallback
  that treats a missing service as "allowed"; a nil panics into a 500, which is
  the correct direction.
- **The admin 401 carve-out in `web/src/api/client.ts` is an allowlist.** A
  denylist would exempt a body-less 401 from nginx and leave the operator
  silently signed in.
- **Flags are resolved per request, uncached.** One box, few households; a cache
  stale for a minute after the operator flips a switch is worse than a query.
- **`ClearHouseholdFlag` deletes the row rather than setting it false.** "No
  opinion" and "explicitly off" are different states.

## 7. The paper trail

| What | Where |
|---|---|
| The design this implements | `docs/superpowers/specs/2026-09-01-hearth-admin-surface-design.md` |
| The implementation plan (steps 1–3 only) | `docs/superpowers/plans/2026-09-01-hearth-admin-surface.md` |
| The browser walk, 15 of 15 | `docs/superpowers/plans/2026-09-02-hearth-admin-surface-verification.md` |
| Why the authorization model is what it is | `docs/adr/0005-platform-admin-authorization.md` |
| What this work taught | `docs/LEARNING.md`, pattern 16 and the entries around it |
| What exists and what is still 🟡 | `docs/FEATURE_TRACKER.md` §9 |
| The tables, ports and middleware chain | `docs/SYSTEM_DESIGN.md` |

## 8. If you are picking this up

Read `docs/adr/0005-platform-admin-authorization.md` first — it is short and it
explains why the shape is the shape, including where it deliberately narrows
`adminctl`'s own written position that operator actions "have no business
behind an authenticated HTTP endpoint".

Then, if you are building the next slice, read spec §4 in full before writing
anything. Its four guards are ordered by how much they are trusted, and the
first one — a separate `SELECT`-only Postgres role — is the one that actually
holds. The other three are defence in depth.

One thing this branch taught repeatedly, worth carrying: **state the invariant,
never the enumeration.** Nine comments on this branch asserted a checkable fact
— an ordering, a count, a list of callers, a status code — that the code later
outgrew, and every one of them passed a green suite. "Every caller except
`handleMe`" survives a new caller; "three of four callers" does not.
