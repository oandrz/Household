# Hearth — handover

Written 2026-07-27, after slices 0 and 1 shipped, and updated the same day once
self-serve sign-up's code (not yet its browser walk — §1) was done too. This
is the document to read before picking the work back up, whether that is you
in three months or someone new.

---

## 1. Where things stand

Three of six slices are now walked end to end in a browser. Self-serve sign-up
is complete and reviewed — every task's code was reviewed clean, including fix
rounds — but **still** has not had its own browser walk. Say that plainly
rather than letting "verified end to end" quietly absorb it: `make lint &&
make test` passing is not the same claim as a human clicking through it.
Accounts (slice 2's first feature) closed that gap for itself: its walk ran
and passed, 15 of 15. Transactions (slice 2's second feature) is code-complete
and reviewed the same way, seventeen tasks deep with every task's own review
clean including fix rounds — and its own browser walk (Task 19, fifteen
criteria) has now run too. **Result: 15 of 15 pass**, recorded in
`docs/superpowers/plans/2026-07-29-hearth-transactions-verification.md`. One
real defect surfaced: the ledger's Kind filter (All/Expense/Income) hid its
real, keyboard-focusable `<input type="radio">`s with `sr-only`, and the
visible pill standing in for each one never reacted to the hidden input's
own focus state — Tab and arrow-key navigation moved real focus with no
visible indicator at all, catchable only in a real browser (`fireEvent.click`,
what every existing test used, never presses a key). Fixed in
`web/src/features/money/TransactionFilters.tsx`, and the fix's own first
attempt was itself caught half-wrong by the same walk: a single ring colour
was invisible against the selected pill's near-black background (two
screenshots, before and after, came back byte-identical), so the ring colour
had to become conditional on which of the two pill backgrounds it sits
against. Pinned by a new, mutation-checked test in
`TransactionsPage.test.tsx`. Two criteria were met by an interpreted rather
than fully literal path — the sidebar reaches Transactions via Money →
Finances → "See all" rather than a direct sub-link (by the design's own
documented scoping), and the limited-member capability was granted via
`adminctl create-invite --capabilities=money` before being additionally
exercised through the Settings toggle Andreas would actually use — both
recorded in the verification file rather than passed over quietly.

| Slice | Contents | State |
|---|---|---|
| 0 — Skeleton | Clean-architecture layout, Docker, Compose, Make, migrations, health endpoints | **Done** |
| 1 — Household & identity | Sign-in, magic link, invite acceptance, lockout, members, roles, capabilities, spaces, Settings | **Done** |
| — Self-serve sign-up | Sign-up, household provisioning, an ISO 4217 currency allowlist and list endpoint, `adminctl prune`, a per-IP rate limiter | Code-complete; browser walk **still pending** |
| 2 — Money | **Accounts**: manual entry, net worth, assets/liabilities breakdown, archive and restore — **done, browser walk 15/15**. **Transactions**: ledger, categories, filters, keyset paging, month-to-date spend — **done, browser walk 15/15**. Budget, Goals, Bills: not started | In progress |
| 3 — Marriage | Retros, Vision, Agreements | Not started |
| 4 — Family | Calendar | Not started |
| 5 — Overview | Read-only aggregation across 2–4 | Not started |

Self-serve sign-up carries no slice number on purpose: it was specified and
built between slices 1 and 2, ahead of Money (see "What to do next" below for
why), and the rest of the roadmap keeps its original numbers so every existing
reference to "slice 2 (Money)" elsewhere still points at the right thing.

The definition of done for slices 0 and 1 was walked in a real browser on a
wiped database: **10 of 10 criteria pass**. The record, including the three that
failed on the first walk and what was done about them, is in
`docs/superpowers/plans/2026-07-26-hearth-identity-verification.md`.

Self-serve sign-up's own definition of done is a 15-criterion walk, written
down in `docs/superpowers/plans/2026-07-27-hearth-self-serve-signup.md`
(Task 32) — a stranger creating a household, the endpoint's silence holding
across the first five rapid sign-ups and the per-IP limit correctly answering
`429` on the sixth, `adminctl
unlock-household --email` resolving the right household, and `adminctl prune`
refusing a window under seven days among them. It has not been run. Start it
from `make down && make up` (or an explicit `make migrate`), not a bare
`make up` — see §5's Makefile item.

Accounts' own definition of done is a 15-criterion walk, written down in
`docs/superpowers/plans/2026-07-28-hearth-accounts.md` (Task 41). **Result: 15
of 15 pass**, recorded in
`docs/superpowers/plans/2026-07-28-hearth-accounts-verification.md` — the sign
rule end to end (a loan reads positive in the accounts list and negative in
net worth), convert-then-add against a real database (an IDR account raised
net worth by the exchange rate's cent-accurate figure), the redaction at the
wire (a limited member's response carries no `balance` key, no `balanceAsOf`,
no `summary` at all), and the primary-currency-change state (switching to EUR
blanks the net worth figure with a named explanation, never a zero) among
them. The walk's own script had one defect: criterion 12 said "sign in as
Kayla in a private window", which is not executable — seeded children are
credential-less by design — so it was met instead by inviting a limited
member with real credentials, which is the more realistic path anyway. Fixed
in the record, not in the product.

The walk itself lost most of its first hour to something unrelated to the
feature: this machine runs two Docker engines, and a five-hour-old Docker
Desktop stack was silently holding the host ports colima's stack needed. See
`docs/LEARNING.md` for the lesson that cost the hour.

Transactions' own definition of done is also a 15-criterion walk, written down
in `docs/superpowers/plans/2026-07-29-hearth-transactions.md` (Task 19) — a
fresh household's category dropdown populated before any transaction exists,
the sign rule and the currency-mismatch rule both proven against a real
database (a cross-currency transfer credits the destination in the figure
actually typed, not a converted one), a transaction dated before its account's
opening balance saved and marked but still counted in "Spent this month," the
five filters each narrowing the ledger with the account filter matching a
transfer on both sides, keyset paging surviving a row inserted mid-scroll, and
a limited member holding Money refused the ledger itself (reads included, not
only writes) among them. **Result: 15 of 15 pass**, recorded in
`docs/superpowers/plans/2026-07-29-hearth-transactions-verification.md`. All
seven money-movement criteria (expense, income, same-currency transfer,
cross-currency transfer, a same-currency transfer's fee) reconciled to the
cent against a real database, including the account-opening-date boundary
that Task 9's designated mutation (`>` to `>=`) protects. One real defect
came out of the walk rather than a false claim from a stub: the Kind
filter's radios are `sr-only` and their visible label never reacted to the
hidden input's own focus state, so keyboard navigation moved real focus
with no visible sign of it at all — a defect no unit test using
`fireEvent.click` could ever have pressed a key to find. Fixed, and the
fix's own first version was itself caught half-wrong by the same walk
before-and-after screenshots (a single ring colour disappeared against the
selected pill's dark background) — pattern 3 of `docs/LEARNING.md` records
both. Two criteria were met by an interpreted path rather than a fully
literal one and are recorded as such in the verification file, the same
standard the Accounts walk set for its own criterion 12: the sidebar
reaches Transactions through Money → Finances → "See all" rather than a
direct sub-link, by the design's own documented single-nav-item scoping;
and the limited member's Money capability was granted at invite time via
`adminctl` before also being exercised through the Settings toggle the
criterion's wording names.

Two screens the design marks "· not built" are deliberately absent: the **kids
view** and **custom space pages**. That is the design's own scoping, not an
omission.

---

## 2. Running it

```bash
make dev      # everything, logs tailed — http://localhost:5173
make seed     # the design's household; prints Christine's invite URL
```

`make seed` gives you Andreas signed-in-able with a known development password,
a pending invite for Christine, Kayla (12) and Ethan (8) as credential-less
members, the three builtin spaces, and notification preferences.

Bare `make` lists every target. The ones you will actually use:

| | |
|---|---|
| `make dev` / `make down` | start and stop |
| `make dev-local` | API and web as native processes for a debugger; infra stays in Docker |
| `make seed` | the household above; refuses to run outside `APP_ENV=development` **and** against a non-local database |
| `make reset-password EMAIL=…` | prompts on stdin, never a flag; also revokes that user's sessions |
| `make unlock-household` | clears a lockout without waiting 15 minutes |
| `make migrate-new NAME=…` | runs through the pinned dev image, not a host binary |
| `make lint` | arch lint, frontend typecheck, eslint, `go vet` |
| `make test` | Go suite (needs Docker) plus 117 frontend tests |

**Docker is colima on the original machine.** The Go suite uses testcontainers
and needs:

```bash
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
```

This machine can also run Docker Desktop at the same time. If it is up, it
silently wins host ports 5173/8080/8025 out from under colima's stack, so
`make up` succeeds, `docker compose` manages colima's containers, and the
browser and every `curl` still talk to whatever Docker Desktop published —
check `docker ps` on both engines before concluding the code is broken.

Mailpit catches all outbound mail at `http://localhost:8025`. Magic links and
invites land there in development.

---

## 3. The shape of the code

```
api/
  cmd/api/            wiring only — read config, open the pool, build the router, serve
  cmd/adminctl/       seed, reset-password, unlock-household, create-invite, prune
  internal/domain/    rules. Imports the standard library and nothing else.
  internal/usecase/   services + every port interface (ports.go)
  internal/adapter/   http, postgres, crypto, mail, clock, fx
  migrations/         goose
web/src/
  api/client.ts       apiFetch — the only way the app talks to the server
  components/         generic primitives only (Modal)
  features/           auth, shell, settings, money, placeholder
  routes/router.tsx   the route tree
```

**Dependencies point inward, and `make lint-arch` enforces it mechanically** —
including in test files. `internal/domain` may import stdlib only;
`internal/usecase` may add `internal/domain`. Everything else lives in an
adapter. The lint runs a real build first, because `go list` alone tolerates
breakage the compiler rejects.

`internal/usecase/ports.go` is the contract. Read it before writing a service or
a repository; it carries doc comments that are load-bearing, not decorative
(the `""` ⇄ SQL NULL convention, the transactional-accept warning).

---

## 4. What to do next

**The build order changed once already: self-serve sign-up shipped ahead of
slice 2.** The original four-slice order below (Money, then Marriage, then
Family, then Overview) was dependency-driven and none of that changed. Sign-up
is a separate piece of work that cut the line for a different, recorded
reason: its own spec's decision 1 ships it before the platform admin console
that will manage it (a deferred, separate spec) so it earns real usage first
— and a household has to be able to exist before there is anything for that
console to administer.

**Slice 2 (Money) is under way.** Accounts and Transactions, its first two
features, are code-complete and reviewed; Budget, Goals and Bills are not
started. It is still the largest area and still the design's centre of
gravity. Slice 5 (Overview) must still be last — it only aggregates, so
building it early means stubbing everything it reads.

Each slice gets its own spec → plan → implementation cycle, the same way these
did. The originating spec for slices 0–1 is
`docs/superpowers/specs/2026-07-26-hearth-foundation-design.md`; self-serve
sign-up's own is
`docs/superpowers/specs/2026-07-27-hearth-self-serve-signup-design.md`;
Accounts' own is `docs/superpowers/specs/2026-07-28-hearth-accounts-design.md`;
Transactions' own is
`docs/superpowers/specs/2026-07-29-hearth-transactions-design.md`. The
completed plans beside them are worth skimming for house style before writing
a fifth.

### What Accounts and Transactions closed, and what Budget must pin next

Three things a prior review flagged as "must not be forgotten" before slice 2's
first task. Accounts closed the first, upheld the second, and pinned the start
of the third; Transactions pinned two more figures of its own; Budget inherits
what is left:

1. **`requireCapability` middleware exists and no route uses it — closed.**
   The spec promised the server enforces capabilities independently of the
   UI; until Accounts, that promise was vacuous. `GET /api/v1/accounts` and
   its four write routes are now gated on the `money` capability (reads) and
   `money` plus owner (writes); Transactions and Categories go further —
   `money` **and** owner gate their reads too, not just their writes, because
   a ledger with every figure blank reads as broken rather than merely
   restricted (`docs/SYSTEM_DESIGN.md` §4). The route-walk test matrices in
   `api/internal/adapter/http/api_test.go` cover both shapes.
2. **Money is `int64` minor units plus an ISO 4217 code, everywhere — held.**
   `domain.Money` refuses to mix currencies; `AccountService.Summary` and
   `TransactionService.MonthSummary` both convert into the household's primary
   currency before summing, for exactly that reason (`docs/LEARNING.md`,
   pattern 12). No `float64` entered a monetary path on either side of the
   stack.
3. **The derived figures the design shows are still mostly undefined.** Net
   worth (Accounts) is pinned and built. Transactions pinned two more, and
   only the two its own screen shows: `Count` ("247 in July") and `Spent`
   ("Spent this month S$3,420.18" — expenses only, income and transfers
   excluded). **`66% used`, `S$137/day left`, `on pace to save S$1,780`,
   `4 of 4 on track`, and unspent budget rolling into a nominated goal at
   month end are still undefined** and are Budget's and Goals' to pin, in
   their own specs, before an implementer invents one.

**Budget is the next feature.** It builds directly on Transactions: an
envelope per category is a sum over `transactions` filtered by `category_id`
and month, which `TransactionRepository.MonthTotals`'s shape already supports.
**"Edit categories" — rename, add, archive, the design's three seeding
templates — is Budget's screen, not Transactions'**, and the table it edits
(`categories`) already exists and is already seeded; Budget adds the controls,
not the data. Before writing any of it, pin the five figures named above —
an implementer who invents a formula for "on pace to save" or "4 of 4 on
track" without a decision recorded first is building on sand.

### The seams slice 2 will use

- **Bank sync is not a port anyone has built.** SGFinDex is restricted to
  licensed institutions, so `BankSyncProvider` does not exist, and Accounts —
  Money's first feature — shipped without it: manual entry needs no port at
  all, and a port with one implementation and no second caller is the wrong
  shape. It arrives when CSV import gives it a second implementation to
  abstract over. The design's 3-step Link-account modal was cut to one step
  for the same reason: a chooser between "connect a bank" (permanently dead)
  and "manual account" teaches nothing with only one live branch, so
  `+ Add account` opens the manual form directly.
- **`AccountView.Balance` is a real sum, computed in the repository's SQL, not
  in a service.** Transactions is what made it one: before, `Balance` copied
  `opening_balance_minor` because there was nothing to add. See
  `docs/SYSTEM_DESIGN.md` §5.
- **A transaction is hard-deleted; an account never is.** Nothing references
  a transaction, so archiving one would only be a screen nobody asked for.
  Budget's category archiving follows the *account* pattern instead — a
  category is referenced by transactions, so it archives rather than deletes,
  the same reasoning, applied to a different table.
- **The sidebar renders from `me.spaces`**, filtered and ordered by the server.
  Accounts added a real page under the existing Money space (Finances,
  replacing its placeholder); Transactions added a second, real sibling route
  (`/money/transactions`) under the same guard rather than the placeholder's
  catch-all; neither touched the sidebar itself.
- **`components/Modal`** is the shared primitive. Roughly fifteen modals across
  slices 2–4 build on it. It reaches genuine `:modal` state — do not
  reintroduce a declarative `open` attribute.
- **`stubFetchRoutes`** matches on method and URL and throws on an unregistered
  request. Use it for every frontend test; a stub that ignores the URL has
  silently passed broken code twice in this project.

---

## 5. Remaining items

`docs/superpowers/plans/2026-07-26-hearth-follow-ups.md` is the full list, with
the reasoning for each. The headlines:

### Decisions, not defects — do not "fix" these without a conversation

- **The lockout is household-wide, and that leaks which addresses share a
  household.** Someone who knows one member's address can confirm a second by
  watching the attempts countdown. Accepted deliberately: the design's copy says
  the household locks. Revisit only if the lock's scope changes.
- **The lock is uncapped**, so repeated guessing can hold a household locked
  indefinitely. Accepted: magic link is deliberately ungated and is the way back
  in.
- **Sign-up's rate-limit numbers (per-IP 5/hour, global 1000/day, reset at
  midnight) were changed deliberately by the product owner**, after a
  whole-branch review found the previous pair (10/hour, 200/day) did not
  compose — one IP alone, inside its own hourly budget, could exhaust the
  global ceiling, after which sign-up silently mailed nothing platform-wide
  for up to a day. The two failure modes are asymmetric: a per-IP `429` is
  cheap and self-announcing, a tripped global ceiling is silent and
  platform-wide, so the global number must stay one that legitimate traffic
  never approaches. Do not change either number without re-reading
  `docs/LEARNING.md`'s pattern 11 and re-checking
  `TestSignUpRateLimitsCompose`, the test that asserts the two stay composed.

All three are documented in the code at the point a future editor would change them.

### Worth doing when convenient

Four items previously listed here are now done. Self-serve sign-up closed
three: `preAuthPathPrefixes`/`publicRoutePrefixes` became one
`web/src/routes/publicRoutes.ts` backed by a router-walk test; non-ASCII
display names now get a correct avatar initial (`initialOf` slices the first
rune and case-folds through `cases.Upper`, and `avatar_initial` widened to
`text` for the rare expansion case); and backend currency validation now
checks membership in a real ISO 4217 allowlist (`domain.ParseCurrency`) rather
than format only, so `ZZZ` is refused. Accounts closed the fourth:
`requireCapability` was unused and carried a deadline (§4 above) — it now
gates the accounts routes, and the route-walk matrices exercise it.

- `apiFetch` has no timeout or abort, so a request that never settles leaves its
  control disabled indefinitely.
- `CurrencyPanel` and `NotificationsPanel` are correct but unprotected: neither
  has a test that would catch a regression to the non-awaited invalidation.
- **`make up` can silently skip a migration.** `api` declares `depends_on:
  migrate` with `service_completed_successfully`, but Compose only
  re-evaluates that condition when it recreates `api` — so a stack left
  running across a newly added migration keeps its already-succeeded
  `migrate` container and never reruns it. `make dev-local` already runs
  `make migrate` explicitly before starting anything; `make up` and a bare
  `docker compose up` do not. Found while grounding this slice's own docs
  update, not by a test — `make down && make up` (which forces recreation) or
  an explicit `make migrate` sidesteps it for now.

Transactions' reviews flagged nine more, judged non-blocking at the time.
Five are worth doing before they compound; four were judged noise and are
left out of this list on purpose (a redundant `var _ AccountLookup`
assertion that duplicates a check `main.go` already pins; the ordering of
`requireCSRF` before or after `requireOwner` on transactions versus accounts,
which has no observable effect either way; `seedSize = 13` coupling the
adapter to the starter-set count, which fails loudly and is already commented
where it would bite; and no dedicated index on `categories(household_id)`,
which needs none — `UNIQUE (household_id, name)` already puts `household_id`
as that index's leading column, so an equality lookup on it alone is served
by the same index a name lookup uses):

- **The goose `Down` migration for `00005_transactions.sql` is correct by
  inspection but no test has ever run it.** Every other migration in this
  project is in the same position; this is not a new gap, just a fresh
  reminder of an old one.
- `api/internal/adapter/http/api_test.go` is now 2036 lines. It wants
  splitting by feature area (auth, household, accounts, transactions) before
  the next feature adds a fifth block to one file.
- `web/src/features/money/TransactionsPage.tsx` is over 500 lines doing
  fetch orchestration, pagination, PATCH-body translation and row rendering
  together. Budget will add a similar page; split this one first rather than
  copying the shape.
- `RecentTransactionsCard` has its own date formatter, duplicating one
  `TransactionsPage` already has. Small today; this is exactly the "fixed in
  one place, left in the sibling" shape pattern 1 warns about, so worth
  merging before a date-formatting bug has two places to hide in.
- Only two of the four `clearReceivedAmount` input combinations have a test
  (see `docs/LEARNING.md`'s Task 16 entry for what the other two would need
  to assert). Cheap to close while the PATCH-translation logic is still
  fresh in mind.

### Before this is deployed anywhere real

- **TLS termination in front is mandatory.** Cookies are `Secure` outside
  development while nginx listens on plain `:80`. Without TLS the browser never
  returns the session cookie and everything 401s. The API logs a warning at
  startup; `.env.example` and `web/nginx.conf` say so.
- **SMTP now takes TLS policy and credentials from configuration**, defaulting
  to mandatory TLS outside development. Set `SMTP_USERNAME` / `SMTP_PASSWORD`
  for a hosted relay. If mail cannot leave the box, magic link — the only way
  back into a locked household — fails silently by design.
- The two production images have no wiring between them. `web/nginx.conf`
  hard-codes `proxy_pass http://api:8080`, so `hearth-web` cannot start alone.
- **The sign-up per-IP rate limiter's fix protects the production image only.**
  `web/nginx.conf` now sets `X-Real-IP` to `$remote_addr` and suppresses
  `True-Client-IP` on every API-proxying location, so a client can no longer
  spoof `chi`'s `middleware.RealIP` resolution through those headers there.
  But `docker-compose.yml` has no nginx service at all — in development, Vite
  proxies `/api` straight to `api:8080` with no header rewriting — so the
  per-IP limiter stays fully spoofable in development, and the pending browser
  walk (§1) cannot exercise the fix either way. The global daily mail ceiling
  (1000/day, reset at midnight, counted from `signups`) is what actually bounds
  the damage in the meantime.

---

## 6. Conventions worth keeping

These were each learned from a defect that shipped past a green test suite.

- **Fix the class, not the instance.** Six times in this project a defect was
  fixed at one site while its siblings kept the bug — a PATCH corrected in two
  of three endpoints, an error oracle closed at the mailer and left two lines
  away, a non-awaited invalidation fixed in one panel with two untouched, and
  (most recently) a `time.Truncate`-and-location mistake that shipped at two
  separate call sites before a third, correctly-written one got its test.
  When you fix something, grep for its shape.
- **Verify UI behaviour in a real browser.** jsdom's `<dialog>` is a stub, so
  five passing tests hid a modal that threw on every open in production. If a
  behaviour depends on the platform, a simulated DOM cannot tell you it works.
- **A test that cannot fail is not protecting anything.** Mutate it: break the
  code deliberately, confirm the test goes red, restore. This caught a sidebar
  ordering test whose fixture agreed with the wrong answer, and a guard test
  that asserted a transient state.
- **Ask what a caller can measure, not just what it is told.** Two enumeration
  oracles here were built from timing and from which deadline moved, not from
  error codes.
- **Pin tool versions.** Floating `@latest` broke this build twice.
- **Every 2xx except 204 carries a JSON body.** `apiFetch` throws on an ok
  response it cannot parse.

---

## 7. If you are handing this to an agent

The completed plans are the format that worked:
`docs/superpowers/plans/2026-07-26-hearth-skeleton.md`, `…-identity.md`, and
`2026-07-27-hearth-self-serve-signup.md`. All three were executed task-by-task
with a fresh implementer per task, an independent review after each, and fix
rounds until clean. Every task found a real defect in the plan — budget for
that rather than treating it as failure.

The most valuable review instruction was not "check this task" but **"what
sibling of this defect exists elsewhere?"**
