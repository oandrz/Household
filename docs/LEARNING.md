# Hearth — learning log

Every defect found while building slices 0, 1, self-serve sign-up and Accounts
(slice 2's first feature), and what each one teaches. Written because almost
none of them were caught by a failing test — they were caught by someone
asking the right question about code that looked fine and had a green suite.

**Read the patterns first.** They are where the value is. The catalogue below
them is evidence, and a place to check when you touch that area.

**Add to this file when you finish a piece of work.** A defect nobody wrote down
gets rebuilt.

---

## The patterns

### 1. Fixing an instance rarely fixes the class

This happened **six times**. Every time, the fix was correct and the sibling
kept the bug.

- `PATCH` implemented as `PUT` — fixed in `/household` and
  `/notification-preferences`, missed in `/household/members/:id`. Found two
  tasks later by someone building against it.
- A membership-oracle error path closed at the mailer, left in place at
  `NewToken` and `Create` **two lines below**.
- A non-awaited `invalidateQueries` fixed in `MembersPanel`, left in
  `CurrencyPanel` and `NotificationsPanel`.
- A pending-guard added to one control, not to its neighbour.
- `ErrInvalidMoney` mapped off 500 in the same round `fxRateMode` was left
  unvalidated and still 500ing.
- `time.Truncate` operates on the absolute instant, not on a calendar day in a
  particular location — and that misunderstanding shipped at more than one
  site on the accounts branch before anyone named it. An opening-balance
  "not in the future" check compared `asOf.Truncate(24 * time.Hour)` against
  `now.Truncate(24 * time.Hour)`, which refused *today's* date for part of
  every day east of UTC; fixed to allow a day of slack instead of truncating
  either side. The identical mistake then surfaced in the Postgres adapter's
  `dateOnly` helper, which turns `opening_balance_as_of` into a stored date:
  converting to UTC before truncating moved 07:00 on the 26th in Singapore
  back to the 25th. The frontend's own `today()` reads local calendar
  components on purpose and got the logic right the first time, but shipped
  with no test, on the stated belief that pinning it needed a
  `vitest.config.ts` timezone change — a reviewer disproved that belief
  (`process.env.TZ` already works in the test runner) and the test was added
  in the same task.

**When you fix something, grep for its shape before you close it.** The question
that finds these is not "is this fixed?" but "where else does this pattern
appear?" `Truncate` is now that grep for date-and-location bugs specifically —
run it before adding a fourth site.

### 2. A test that cannot fail protects nothing

- A sidebar ordering test supplied spaces **already in ascending order**, so a
  component that re-sorted would have passed identically — and re-sorting was
  the exact bug it existed to prevent.
- `TestUsersWithoutAPasswordCannotSignIn` passed with the guard deleted, because
  the fake hasher happened to reject an empty hash for its own reasons.
- A capability filter had no test that would notice its deletion.
- Five of seven invite tests used a fetch stub that **matched positionally and
  ignored the URL** — they would have passed while the component called a
  different endpoint entirely.
- `-run Lock` matched two of six lockout tests, so "six tests pass" was never
  what was verified.
- A task brief's quoted test filters (`-run TestHousehold`, `-run TestInvite`)
  matched **zero** tests in this repo — `go test` reports "no tests to run" for
  a filter that matches nothing, and exits zero. A filter copied from a brief
  is not evidence anything ran; run the whole package.
- The owner-gated route-walk matrix (`TestOwnerOnlyRoutesRejectALimitedMember`)
  would have refused every accounts write route at `requireCapability` and
  never reached `requireOwner`, because the existing limited-member fixture
  holds no `money` capability — a vacuous green, spotted while writing the
  accounts spec rather than by a failing test. The fixture gained a second
  limited member who does hold `money`, so the walk's caller can fail on
  `requireOwner` specifically instead of being turned away one guard earlier.
  A matrix proves nothing unless the caller can get past every guard except
  the one under test.
- Deleting `NetWorthSummary`'s domain-ordered breakdown loop (replacing it with
  a bare range over the `byType` map) left every test green across eight fresh
  processes, so the rule that two identical requests must not reshuffle the
  chart had nothing behind it. A five-type ordering test, where map iteration
  lands on the sorted order by chance only one time in 120, is what actually
  discriminates.
- Two `FinancesPage` test assertions used a synchronous `getByText` inside an
  already-awaited card, so they never actually waited on the `useCurrencies` /
  `useMe` queries their own subject depended on — both passed only because,
  in practice, every mocked fetch in that test happened to settle within the
  same microtask batch. A reviewer proved it by adding a 20ms delay to one
  unrelated stubbed route: both assertions broke, deterministically, on a
  change to code neither of them exercises.
- Two separate mutation checks in this plan did not discriminate on the first
  attempt. One (validate a patch built only from its non-nil fields, instead
  of the merged account) made three tests fail, but the one the check was
  meant to pin failed for the *wrong* reason (`ErrAccountNicknameRequired`,
  because an isolated `Type: &loan` patch has no nickname) rather than the
  reason it claims to catch (`ErrLiabilityBalanceNegative`) — broad,
  wrong-reason failure proves a mutation breaks *something*, not the specific
  thing it names. The other (`Math.round` in place of a float-safe parse) left
  the *named* test green outright, because `Math.round` happens to repair the
  exact float error that test exists to catch, and a different, unrelated test
  failed as collateral damage instead. Both implementers noticed on their own,
  devised a more surgical mutation that isolated the one claim in question,
  and reported both attempts rather than only the one that worked.
- A comment on `FinancesPage.test.tsx`'s archive-toggle test claimed to be
  "the end-to-end proof" that `invalidateAccounts` (`useAccounts.ts`) returns
  its `Promise.all` rather than firing it and forgetting. Disproven: dropping
  the `return` left that test, and six siblings in the same file, green.
  Two TanStack Query / Testing Library facts combine to make this the
  general shape to watch for, not just an accounts-specific slip:
  `queryClient.invalidateQueries` dispatches its refetch whether or not the
  caller awaits the promise it returns, and `await findByText(...)` polls
  the rendered DOM, not mutation state — so a test built on "the list
  eventually shows the right thing" cannot tell "settled because the
  refetch actually landed" apart from "settled because nobody waited for
  it." What the `return` actually gates is `isPending` (here, the
  Archive/Restore button's `disabled` prop), and only a test that asserts
  on *that* — holding the refetch open with a deferred promise, per
  `SignInScreen.test.tsx`'s existing pattern — can tell the two apart.
  Corrected to describe only what it proves, with a second test added
  alongside it that asserts the disabled state directly.
- `TestEnsureSeededSurvivesConcurrentFirstRequests` (category seeding, Task 5)
  fired eight bare `go func(i) { ... }(i)` goroutines against `EnsureSeeded`
  and stayed green, five runs straight, with the `ON CONFLICT DO NOTHING`
  deliberately removed from `SeedCategories`. The pool in `postgres.Open`
  dials only one connection up front (`Ping`); each goroutine's first query
  pays its own connection-dial latency, which was enough to serialise the
  count-then-insert window the test exists to race. Warming the pool with
  sixteen concurrent `List` calls first, then releasing every seeding
  goroutine through the same closed channel, reproduced the race reliably —
  the same code, unmutated, now fails 15/16 with
  `constraint "categories_household_id_name_key": already exists` the moment
  `ON CONFLICT` is removed, and is stably green five runs in a row restored.
  A concurrency test that starts its goroutines with a bare loop is only
  proven to race if the work per goroutine already dwarfs connection setup;
  otherwise warm the pool and use a start barrier before trusting it.
- Same task, a second instance, found by review after the first was already
  fixed: `TestEnsureSeededDoesNotRebuildOverArchivedCategories`'s comment
  credited the archived-row protection to `SeedCategories`' unique key and
  `ON CONFLICT`. It does not reach either. `CountCategories` has no
  `archived_at` filter, so a household with all thirteen categories archived
  still counts as thirteen; `EnsureSeeded`'s `if count > 0 { return nil }`
  fires before the second `EnsureSeeded` call in that test ever calls
  `SeedCategories`. The test still pins something real — a cleared list stays
  cleared — but the *reason* in the comment was wrong, and it would have
  passed identically with `ON CONFLICT` deleted outright, same as the
  concurrency test above. Fixed by correcting the comment to name the count
  check, and by adding
  `TestSeedCategoriesRespectsTheUniqueKeyEvenWhenEveryRowIsArchived`, which
  calls the generated query directly — bypassing `EnsureSeeded`'s count check
  — against an already-archived household, and does fail with a duplicate-key
  error the moment `ON CONFLICT` is removed. Two short-circuit-shadowed tests
  in one task, one caught only by review after the first was already
  believed fixed, is the point of this section: a passing mutation check on
  a *different* test in the same file is not evidence for this one.

**Mutate to prove a test.** Break the code deliberately, watch the test go red,
restore it. If it stays green, the test is decoration — and if it goes red for
a different reason than the one you meant to prove, that is not yet proof
either; sharpen the mutation until the failure names the claim.

### 3. The simulated environment lied

- `Modal` threw `InvalidStateError` on **every open in every real browser** —
  React renders `<dialog open>` before the effect calls `showModal()`, and the
  spec throws if the attribute is already there. All five tests passed, because
  jsdom's `HTMLDialogElement` is an empty stub with no `showModal` at all. Only
  the fallback path ever ran.
- Fixing that exposed a second bug that had been unreachable: the dialog never
  stretched to the viewport, so there was **no backdrop area to click**.
- The 401 redirect handler bounced every invitee off the invite screen. Green
  suite — because the handler defaults to null and every test installed a stub
  instead of the real wiring.
- The accounts browser walk answered `500 INTERNAL` on every route, and **the
  API logged nothing** — impossible on its face, since the only two code paths
  that produce that response (`logAndWriteInternal`, `recoverer`) both log
  before writing. An hour went into re-reading the code for a bug that was not
  there. The cause was not simulated but *assumed*: this machine runs two
  Docker engines, and a five-hour-old Docker Desktop stack was silently
  holding the host ports colima's stack needed, so the browser and every
  `curl` reached stale code while every `docker compose` command managed a
  container nobody could see or log. The tell was in the response the whole
  time — the request ID's hostname prefix never matched the running
  container, and the per-process request counter never reset across a
  restart, neither of which is possible for a process actually being
  restarted — and went unread because nobody checked which process was
  actually answering.

**If a behaviour depends on the platform, verify it in the platform.** A real
browser found three defects that jsdom structurally could not observe. And
when a service returns an error it did not log, stop debugging the code and
confirm you are talking to the process you think you are — an assumed
environment that is not the one running is the same trap as a simulated one
that cannot tell the truth, and every hypothesis about the code will be wrong
for as long as the premise is.

### 4. Guards scoped to the wrong interval

Twice, a fix closed the reported sequence and left the class open. Both times a
reviewer found it by building a probe rather than reading the diff.

- Clearing an error **on mode change** could not cancel an error that arrived
  *after* the change — an abandoned request settling later rendered on a screen
  the user had already left.
- Disabling a control **while the mutation is pending** stopped too early,
  because `onSuccess` fired `invalidateQueries` without awaiting it. The mutation
  settled when the response arrived, not when the cache refreshed.

**Ask what interval the guard actually covers, and what can arrive outside it.**

### 5. Silent partial success is worse than loud failure

- `HouseholdRepository.Update` accepted six fields and persisted four. A caller
  setting the other two got `nil` back.
- `PATCH /household` blanked every field the caller omitted — the *spec's own
  documented body* returned 500.
- Invite acceptance was three separate writes. A failure in the middle left an
  orphaned user holding the unique email index, so the invite could **never** be
  accepted, by anyone, ever.
- Creating a child was two writes with no transaction; because a child's email
  is NULL there is no constraint to fail loudly, so each retry silently created
  another orphan.
- A failed session revocation returned a bare error indistinguishable from "the
  change did not happen" — while the change *had* happened and the member's old
  session stayed live.
- The account edit modal forwarded the create mutation's request body shape
  to `PATCH /accounts/{id}` unchanged. The backend's real-patch convention
  treats a nil pointer as "leave this field alone", and JSON `null` decodes to
  that same nil — indistinguishable from the field being absent. The form
  models "Shared" as `null` (correct for *create*, where null and omission
  both default to shared), so selecting "Shared" while editing an owned
  account silently left the previous owner in place instead of clearing it —
  the PATCH answered 200 and changed nothing. Found by tracing the wire type
  through both sides of the same field, not by a failing test; fixed with a
  `null -> ""` translation on the update path only, and a dedicated test now
  asserts the PATCH body itself carries `""`.

**Any two writes that must both happen need a transaction or a loud failure.**
And a function that accepts a field must persist it or refuse it — silently
keeping the old value is the same failure wearing a 200.

### 6. Enumeration oracles are rarely in the error code

The magic-link endpoint always returns 202 and sign-in always returns the same
countdown — and both still leaked, through side effects nobody thought of as
output.

- The locked branch skipped recording the attempt, so `lockedUntil` **froze**
  while the unknown-address path's deadline kept advancing. Watching which
  deadline moved told you whether the address was real.
- `Hasher.Verify` ran on only one of four branches, so argon2's deliberate cost —
  tens to hundreds of milliseconds — separated "real member with a password" from
  everything else.
- The mailer's error propagated only on the known-address branch, so a degraded
  relay turned a non-nil result into a discrete membership signal.
- Rate-limited requests were **faster** than unknown ones, because the count
  query joins through `users` and a stranger can never reach the limit.
- Sign-up's per-address limit counted rows in `signups`, but only the
  fresh-address branch ever wrote one — the registered-address branch answered
  `202` with an "already have an account" mail without touching the table its
  own limit was supposedly counted from. Four requests for one registered
  address sent four such mails, then forty, then four hundred: the same
  mailbox oracle this endpoint exists to close, expressed as unbounded mail
  *volume* rather than a discrete signal, on the one branch nobody thought to
  check was gated. The test that should have caught it passed anyway — its
  double let a caller force the counter's state directly (`setEmailCount`), a
  state no amount of real traffic sent through the real path could produce.

**Ask what a caller can measure, not what it is told:** status, body, timing,
number of round trips, whether an email arrived — and how much of it.

### 7. Floating versions break builds you did not touch

`air@latest` raised its Go minimum and broke the image. `goose@latest` sat one
line below it with identical exposure. `sqlc@latest` would have forced the whole
module to a newer Go than the Dockerfile pins.

**Pin tool versions and say why in a comment**, or someone will tidy them back
to `@latest`.

### 8. Configuration that lies

- `.env.example` claimed Compose read it. Compose hardcoded every value inline
  and read nothing.
- `SESSION_SECRET` was required at startup and used by nothing — implying
  sessions were signed when they are random and hashed.
- `APP_ENV` defaulted to `development`, so a production deploy that forgot to
  set it would silently serve non-`Secure` cookies.
- The seed's guard checked `APP_ENV` and said nothing about `DATABASE_URL`, so
  `APP_ENV=development` against a production database passed.
- Cookies are `Secure` in production while nginx listens on plain `:80` — as
  shipped, the browser never returns the session cookie.

**A config value that nothing reads is a lie. A guard that names one thing while
protecting another is worse.**

### 9. A DELETE scoped to a deliberately-nullable column spares exactly the rows that column's nullability exists to create

- `ClearFailures` is `DELETE FROM login_attempts WHERE household_id = $1 AND
  succeeded = false` — the right statement for clearing a *member's* failed
  attempts. But `household_id` is nullable on purpose, so an attempt against
  an address nobody recognises can still be recorded without revealing
  whether it exists (`migrations/00002_identity.sql`). `household_id = $1`
  cannot match `NULL`, so every row a stranger's failed guess ever created was
  deleted by nothing, ever, while every row a real member created was cleared
  on their next success. `login_attempts` was already unbounded before this
  slice touched it; `signups` (this slice's own new table) does not have the
  same defect only because nothing scopes its pruner to a household at all.
- Found by asking "which tables can a stranger grow at will?", not by a test —
  nothing failed, because nothing had ever asserted the unreachable rows got
  cleaned up. `adminctl prune` now covers both tables, with a floor that
  refuses to prune inside `domain.LockoutPolicy.Window` — deleting a row still
  inside it would clear a live lockout as a side effect of "cleanup".

**When a column is nullable for a stated reason, ask what a filter on it
silently excludes.** A cleanup query scoped the same way a lookup query is
scoped inherits that query's blind spot for free.

### 10. Slicing a string by position, and "simple" case mapping, both assume one character is one unit

- `initialOf` took `displayName[:1]` — one *byte* — to get an avatar's first
  letter. Every ASCII name worked by accident; every multi-byte UTF-8 name
  (anything outside Latin, plus accented Latin) produced an invalid fragment
  that rendered as the replacement character, permanently, because there is no
  profile-edit endpoint to fix it. Invisible for as long as every name in the
  seed and every test fixture was ASCII.
- Fixing that to slice the first *rune* surfaced a second, narrower gap:
  `strings.ToUpper` is Go's *simple* case mapping, which leaves German `ß`
  alone rather than expanding it to `SS` — simple mapping is defined to be
  one-rune-to-one-rune, and `ß`→`SS` is not. The fix uses
  `golang.org/x/text/cases.Upper(language.Und)`, which performs full
  (language-independent) case mapping and can turn one rune into two, which is
  *why* `avatar_initial` widened from `char(1)` to `text` in the same
  migration — a one-rune expansion that `char(1)` would otherwise reject
  outright.

**Code that assumes "the first character" fits in one byte, or that upper-casing
never changes a string's length, is written for ASCII and will not say so.**
Both assumptions are invisible until a name that isn't ASCII reaches them.

### 11. Layered limits have asymmetric failure modes, and the one that fails silently and globally must never bind first

- Sign-up had a per-IP limit of 10/hour and a global daily mail ceiling of
  200. Each looked right alone. Together they did not compose: 10 × 24 = 240
  > 200, so a single IP, entirely inside its own hourly budget, could
  exhaust the global ceiling by itself. Once that ceiling trips, **every**
  sign-up on the platform silently answers `202` and mails nothing, for up
  to 24 hours, with nobody told.
- The same slice had no server-side email format validation, so
  `{"email":""}` still wrote a countable row into `signups` — a zero-cost
  way to spend either limit's budget, which makes any ceiling negotiable for
  free rather than merely tight.
- Found by a whole-branch review reading the two numbers together, after
  both limits had already shipped clean and reviewed on their own — not by
  a test. Neither per-task review could have caught it: each limit was
  correct within the scope of its own task, and the composition only exists
  across two separate commits.

**A per-IP `429` inconveniences one caller and announces itself. A global
ceiling tripping is invisible, platform-wide, and indistinguishable from
"nobody is signing up today."** The cheap, reversible limit must bite before
the expensive, silent one — and something must assert that relationship,
because two constants in different packages drift independently. The guard
here is a test, `TestSignUpRateLimitsCompose`, asserting
`signUpRequestsPerIPPerHour * 24 < SignupGlobalDailyLimit` against the live
constants — which is the entire reason `SignupGlobalDailyLimit` is exported
at all.

### 12. A sum over mixed currencies must convert before it adds

`domain.Money.Add` refuses to add two different currencies, by design.
Summing a household's accounts before converting each into the primary
currency would therefore not merely be wrong — it would fail outright, but
only once the household actually holds a second currency. A single-currency
household, or a test suite built only from single-currency fixtures, never
reaches the line that would catch it.

- `AccountService.Summary` converts each account into the household's
  primary currency through `Rate.Apply` *before* any `Add` — specified this
  way from the start, not discovered as a defect, because `domain.Money.Add`
  already refuses to add two currencies and the spec named the ordering
  explicitly. Mutation-checked by reordering the loop to sum raw balances
  first and convert once at the end: exactly `TestSummaryConvertsBeforeAdding`
  went red, with `domain.ErrCurrencyMismatch` surfacing through the same
  SGD/IDR pair the design's own seed household holds. Every single-currency
  test in the same file stayed green — which is the point. An ordering bug
  here is invisible until a household actually holds two currencies.

**A rule that only breaks under a second data point needs a test built from
that second data point.** Writing the mixed-currency test from the design's
own IDR account, rather than from the happy path, is what gave this rule
anything to prove itself against.

---

## Catalogue by area

### Domain and money

- `Money.Add` wrapped silently on overflow. Every balance flows through it.
- `Money.String()` rendered `math.MinInt64` as `-SGD -92233720368547758.-8` —
  negating the most negative int64 returns itself.
- `Money{}` zero values added successfully, because the currency guard compared
  `""` to `""`.
- The last-owner rule never checked the target existed, so removing a
  non-existent membership was approved — and a capability-only edit on a limited
  member tripped `ErrLastOwner` for an operation that never touched ownership.
- `Rate.Apply` multiplied without an overflow guard while `Add` refused to wrap.
  Multiplication overflows far sooner.
- A doc comment promised "an invalid value cannot exist anywhere in the system".
  Go cannot enforce that with exported fields, and the repository layer rebuilds
  these values from database rows.
- `switch` statements on `Role` and `Visibility` had no `default`, so an
  unrecognised value — which arrives from a text column — skipped validation
  entirely. **Fail closed on values you did not construct.**
- `NetWorthSummary.Computable`'s guard was `len(views) > 0 && converted == 0`,
  which counts archived accounts — the loop skips them outright before either
  counter increments, so a household whose only accounts were archived would
  have reported "we cannot compute your net worth" for what this same feature's
  own rule calls a genuine and computable zero. Caught by the implementer
  before it shipped, not by a test; fixed with a `considered` counter
  incremented only for non-archived views, judged separately from the raw row
  count.

### Database and repositories

- `MarkInviteAccepted` had no guard and no `RETURNING`, so two concurrent
  accepts both succeeded. The correct pattern was already forty lines above in
  the same file (`ConsumeMagicLink`).
- A unique-violation surfaced as an opaque driver error because `translate` only
  special-cased `pgx.ErrNoRows`.
- `ListSpaces` ordered by `position` with no tiebreaker, so duplicates made
  sidebar order nondeterministic.
- Deleting a membership leaves the `users` row alive. Three separate symptoms
  traced to that one fact, and no task owned "what if this email already
  exists".
- Widening a sqlc-generated params struct (`CreateHouseholdParams` gained two
  currency fields) does not fail the build at a call site that omits the new
  fields. sqlc generates a keyed struct literal, so Go silently zero-values
  whatever a caller leaves out; `go build` and `go vet` both stay green — a
  plan that predicted a compile error here was wrong. The existing round-trip
  test asserting the persisted values, not the compiler, is what would have
  caught a dropped field. The same keyed-literal blind spot applies to any Go
  struct, not just sqlc's, which is why a later task re-checked all 17 call
  sites of the higher-level `HouseholdRepository.Create` by hand for the
  identical reason.

### HTTP layer

**This is the only place authorisation exists.** No service takes an actor. A
route with a missing guard has no second line of defence.

- `PATCH /household` and `PATCH /notification-preferences` shipped without
  `requireOwner` — a child could change household currency and every
  notification setting.
- `GET /household/members` returned every member's email to any authenticated
  member, including children.
- Nine `json.Decoder` calls with no size limit; three of them pre-auth.
- Two sentinels answered 500 for ordinary user input.
- The session cookie never slid — the database row was extended, the cookie's
  expiry was frozen at sign-in.
- `middleware.Recoverer` wrote a bare 500, the one response without the error
  envelope — and a panic is exactly when a quotable request id matters.
- There was **no structural test for CSRF or ownership**, behind a justification
  that confused introspecting the middleware chain with observing its behaviour.
  You do not need to compare function values; you need to send a request without
  a token and assert 403.
- `chi.middleware.RealIP` resolves the caller's address from the first
  non-empty of `True-Client-IP`, `X-Real-IP`, then `X-Forwarded-For` — with no
  configured trust list, so whichever of those the *client* sets outranks
  whatever the reverse proxy appends. The sign-up per-IP limiter added here is
  the first thing in this codebase that ever made `clientIP` a security
  decision, and it is exactly as strong as the edge's header rewriting, no
  stronger: one `curl -H "X-Real-IP: <vary>"` per request defeats it unless
  the proxy blanks the client-supplied headers first.
- `signUpRequestsPerIPPerHour` (10/hour) and `usecase.SignupGlobalDailyLimit`
  (200/day) each shipped correct and reviewed on their own, and did not
  compose: one IP, entirely inside its own hourly budget, could exhaust the
  global ceiling alone (10 × 24 = 240 > 200), after which sign-up silently
  mailed nothing platform-wide for up to a day. Now 5/hour and 1000/day,
  with `TestSignUpRateLimitsCompose` asserting the arithmetic against the
  live constants so the two cannot drift apart unnoticed again.
- The accounts redaction gate tested `Role == RoleLimited` — blacklisting the
  untrusted role instead of naming the trusted one. Identical behaviour
  today, since `owner` and `limited` are the only two roles, and silently
  wrong the day a third arrives (an adult who is not an owner, which this
  product will plausibly want): that role would receive every balance and
  the net worth, with no test going red, because the state is not yet
  representable to test against. Rewritten to test `Role != RoleOwner`
  instead. `Role` arrives from a database column that `convert.go` casts
  rather than parses, so only a CHECK constraint stands between an
  unrecognised value and this code — the same fail-closed rule as the
  `Role`/`Visibility` switch statements above, just written as a condition
  polarity instead of a missing `default`.
- The same redaction gate above fixed the *role* axis (whitelist `Role ==
  RoleOwner`) but left the *field* axis a blacklist: the handler built the
  full `accountDTO` and nilled `Balance`/`BalanceAsOf` by name. A new money-
  carrying field added to the struct later would reach every limited member
  with no test going red, because the existing test only checked that those
  two named fields were absent — true regardless of what else leaked
  alongside them. Fixed in the test, not the handler: it now asserts the
  redacted account's *exact* JSON key set, so adding a field forces a
  decision at the one moment it matters, instead of a rebuild-by-whitelist
  that risks drifting the other way.
- The accounts redaction's sibling, found but deliberately not fixed here
  (it belongs to a different feature): `member_handlers.go`'s
  `toMemberViewDTO` builds the full `userDTO` and blanks `Email` by name —
  the identical field-axis-blacklist shape as `redactedAccounts` above, just
  emptied rather than nilled. `TestMemberListWithholdsEmailsFromALimitedMember`
  (`api_test.go`) decodes the response into `memberListEntry`, a fixed struct
  with a fixed set of fields — `json.Decode` silently drops any key the
  struct doesn't declare, so a new field added to `userDTO` later would be
  invisible to every assertion in that test, the same way an unasserted key
  set was invisible to the accounts test before it was fixed. Latent, not
  live: nothing leaks today, because the email is blanked rather than
  omitted, so the wire shape is already stable either way. Left as a trap
  for the day someone adds a second personal field to `userDTO` and the test
  still goes green. Tracked as
  [issue #1](https://github.com/oandrz/Household/issues/1).

### Frontend

- A failed magic-link request rendered **nothing at all** — and because the
  backend send is fire-and-forget by design, the frontend is the only place a
  failure can ever surface. It is also the only way back into a locked household.
- Sign-in discarded any error that was not an `ApiError`, so a network rejection
  showed an empty form.
- The locked sign-in form was a dead end: the submit button was disabled by an
  error that could only be cleared inside the submit handler.
- `"Forgot?"` had no pending guard while its neighbour did, and neither
  validated the email — clicking with an empty field posted `{"email":""}`.
- Business rules were re-implemented client-side (the four-capability list,
  twice), which also made a required 422 path impossible for the UI to produce
  and therefore untested.
- A stale error survived `SignUpScreen`'s sent-panel → form transition: a
  failed resend, then "Use a different address", left the resend failure
  showing under the email field. Sibling of a defect already fixed once in
  `SignInScreen` (there, keyed off `mode` rather than cleared inside one
  handler) — the fix did not carry to the newer screen because nothing grepped
  for the shape.
- A task brief's own test snippet asserted on TanStack Router's `path`, which
  strips the leading slash on a child route (`trimPathLeft`); `fullPath`
  reconstructs it and is what a router-walk test must read instead. Would have
  failed against a *correct* implementation — verified against the router's
  own source, not assumed from the property's name.
- A task brief's own `formatMoney` code butted every currency symbol directly
  against the digits (`Rp85,400,000`), contradicting the same brief's own IDR
  test (`Rp 85,400,000`) — neither could have been right as given. Fixed with
  a rule keyed on whether the symbol ends in a Latin letter, checked against
  all 18 currencies the backend serves. The same brief's flat, all-optional
  `Summary` schema also could not be narrowed by TypeScript after
  `if (!summary.computable)`, which would have forced a non-null assertion at
  the exact spot the DTO exists to prevent one; replaced with a discriminated
  union keyed on `computable`.
- The accounts feature's own file list left `FinancesPage.tsx` off, and wiring
  only `AccountsPanel`'s "+ Add account" button — as the file list implied —
  would have left account creation completely unreachable for a household
  with zero accounts: `AccountsPanel` is not mounted at all in that state,
  `FirstRunPanel` renders instead. Would have failed the feature's own
  definition-of-done walk at its very first "add an account" step. Found by
  reading `FinancesPage.tsx`'s own branching, not by a test; `FirstRunPanel`
  got its own button wired to the same modal.
- `FirstRunPanel` had no "Show archived" toggle at all, so an owner who
  archived their household's only account had no way back to it: the list
  emptied, `FirstRunPanel` took over, and nothing on it could ask for the
  archived view — decision 8's restore guarantee broken for exactly the
  household most likely to trigger it. The 15/15 browser walk never caught
  it because the seeded household always kept several accounts, so the
  single-account case never came up. The fix has two halves and both are
  required: the toggle moved into `FirstRunPanel`, and the branch condition
  dropped its `&& !includeArchived` clause. Dropping only the clause without
  adding the toggle leaves the state unreachable in a different way; keeping
  the clause after adding the toggle reintroduces a second bug one level
  down — a truly-empty household that tries the new toggle anyway falls
  through to the three-card zero state (`S$0.00` beside a blank breakdown),
  because switching `includeArchived` on still returns zero rows when there
  is nothing archived either. Both halves are mutation-tested separately:
  reverting either one turns exactly one of the two new tests red.
- `AccountModal`'s balance-parse error reused one message
  ("Enter an amount, like 8240.55.") for two different failures. Switching
  Currency to a no-decimal one (IDR, VND) without touching Balance is a
  common edit path — the balance display doesn't change on a currency
  switch — and it produces exactly that message next to a field showing
  precisely the figure it's being told to enter. Failing closed was already
  right; the copy just needed to name the actual cause instead of restating
  the input back at the person looking at it.

### Tooling and infrastructure

- The architecture lint **never enforced the rule it existed for**. Both branches
  only matched imports *within* the module, so third-party imports in
  `internal/domain` passed. Proven by planting `pgx` and getting exit 0.
- It also read `go list` inside a process substitution, where `set -euo pipefail`
  cannot see failure — so a module that did not compile reported a clean pass.
- `api` had no `depends_on: migrate`, so ordering worked only because the
  Makefile happened to run migrations first.
- The database URL appeared in three places plus a dead variable pointing at a
  different host.
- `make dev-local` could not work on a clean machine: nothing loaded `.env`, and
  `air` was only installed inside the image.
- `readCookie` split on every `=`, truncating base64 CSRF tokens. Every mutating
  request would have failed with 403.
- A brief told the implementer to add `--email` to `create-invite`, which
  already declares `fs.String("email", ...)` for the invitee's address. Two
  flags of the same name on one `FlagSet` panic **at runtime**, the first time
  the flag set is parsed — `go build` and `go vet` both pass. Renamed to
  `--inviter-email`, with the flag name threaded through so each caller's own
  error names the flag it actually means.

---

## Before you call something done

1. `make lint && make test` — both, on the tree you are about to integrate.
2. Mutate at least one new test: break the code, watch it fail *for the
   reason you expect*, restore. A mutation that kills a different test, or
   fails on the right test for the wrong reason, has not proven anything yet.
3. Grep for the shape of anything you fixed. Siblings are the norm here.
4. If it touches the browser, open a browser.
5. If it accepts caller input, ask what a caller can measure.
6. If it writes twice, ask what happens when the second write fails.
7. Add what you learned to this file.
