# Hearth — learning log

Every defect found while building slices 0 and 1, and what each one teaches.
Written because almost none of them were caught by a failing test — they were
caught by someone asking the right question about code that looked fine and had
a green suite.

**Read the patterns first.** They are where the value is. The catalogue below
them is evidence, and a place to check when you touch that area.

**Add to this file when you finish a piece of work.** A defect nobody wrote down
gets rebuilt.

---

## The patterns

### 1. Fixing an instance rarely fixes the class

This happened **five times**. Every time, the fix was correct and the sibling
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

**When you fix something, grep for its shape before you close it.** The question
that finds these is not "is this fixed?" but "where else does this pattern
appear?"

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

**Mutate to prove a test.** Break the code deliberately, watch the test go red,
restore it. If it stays green, the test is decoration.

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

**If a behaviour depends on the platform, verify it in the platform.** A real
browser found three defects that jsdom structurally could not observe.

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

**Any two writes that must both happen need a transaction or a loud failure.**
And a function that accepts a field must persist it or refuse it.

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

**Ask what a caller can measure, not what it is told:** status, body, timing,
number of round trips, whether an email arrived.

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

---

## Before you call something done

1. `make lint && make test` — both, on the tree you are about to integrate.
2. Mutate at least one new test: break the code, watch it fail, restore.
3. Grep for the shape of anything you fixed. Siblings are the norm here.
4. If it touches the browser, open a browser.
5. If it accepts caller input, ask what a caller can measure.
6. If it writes twice, ask what happens when the second write fails.
7. Add what you learned to this file.
