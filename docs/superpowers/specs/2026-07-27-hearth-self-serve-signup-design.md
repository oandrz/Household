# Self-serve sign-up and household provisioning

Written 2026-07-27. Spec A of two. Spec B — the platform admin console — is
deliberately not in scope here; see "Deferred" at the end.

Today a household can only come into existence two ways: `adminctl seed`, or a
mailed invite into a household that already exists. This spec adds the third:
a stranger arrives at the app, creates their own household, and becomes its
first owner.

---

## 1. Why this is small

Multi-tenancy is already structural. `households`, `memberships (household_id,
user_id)` and `sessions.household_id` all exist, every household-scoped
service is already scoped, and `requireSession` already resolves a caller's
household from their session rather than from a global. Nothing in this spec
adds a tenant dimension; it adds a way to create a tenant.

The one seam is `GetMembershipByUser` (`queries/identity.sql:54`), documented
in `ports.go` as the place multi-tenancy will need attention. It stays a
`LIMIT 1` — see decision 2.

## 2. Decisions

Each of these was settled before the spec was written. The reasoning is
recorded because the reasoning is what a future editor needs, not the verdict.

**1. Sign-up and the platform admin console are two specs.** Sign-up first, so
it earns real usage before the console that manages it is designed. A
consequence worth stating plainly: this displaces slice 2 (Money) in
`docs/HANDOVER.md`'s build order.

**2. One account belongs to exactly one household.** A second household means a
second account. This is what today's auth path already assumes, so it costs
nothing; the alternative reworks `SignIn`, `ConsumeMagicLink`,
`requireSession` and `GetMembershipByUser` — the files carrying this
codebase's timing-oracle comments — plus a switcher UI. The schema still
permits many-per-user whenever that becomes a feature.

**3. Sign-up verifies the address before anything is created.** `POST
/auth/sign-up` answers identically for a fresh address, a registered one and a
rate-limited one; the mail either carries a create-household token or says
"you already have an account". Two reasons, both load-bearing:

- **It is not an enumeration oracle.** This codebase has closed four of those
  (decoy hashing in `SignIn`, always-silent `RequestMagicLink`, address-scoped
  countdowns, the ex-member branch) and has a project skill,
  `finding-disclosure-oracles`, that exists because they shipped. "That email
  is already registered" undoes all of it in one line. Note that
  `MapDomainError` already maps `usecase.ErrInviteeAlreadyRegistered` to `409
  EMAIL_ALREADY_REGISTERED`; **sign-up must never return that sentinel.**
- **It keeps unverified addresses from owning households.** An invite proves
  the address via a mailed token. Instant sign-up would not, so a typo'd
  address would own a household with no way back in — magic link is the only
  recovery path and it goes to the wrong inbox.

**4. Sign-up is open from the start.** No `SIGNUP_MODE` gate. This was weighed
against the alternative and chosen deliberately. Its consequence is not
optional: **rate limiting is the only thing between the SMTP relay and a
stranger with a loop**, so section 8 is a requirement, not a hardening pass.

**5. The completion form asks for the household's primary currency.** The
design does not draw this field; it predates any intent to sell outside
Singapore. A money dashboard showing the wrong currency on first load is the
worst available first impression, so the field is added as a documented
deviation (section 6).

**6. The design's single create card is split across two screens.** The design
draws one card that creates the household on submit — instant sign-up, which
decision 3 rules out. Rather than redraw it, the same card renders twice with
different field subsets: email only before verification, the rest after. The
design already reuses one card across five `authScreen` states via `sc-if`, so
a sixth is idiomatic to its own structure, and the create-specific fields
already sit in a block *above* the shared email/password block, which makes
the split mechanical.

The rejected alternative — all four fields on one screen, mail sent on submit,
details held in `signups` until the link is clicked — is truer to the design
but lets an attacker pre-configure a household in someone else's name: submit
a sign-up for a victim's address, choose the household name and the victim's
display name, and the mail invites them to click into a household a stranger
set up. Splitting the card means the person who clicks the link supplies their
own details.

**7. The password floor stays 12, not the design's "At least 10 characters".**
`minInvitePasswordLength` is 12 and `MapDomainError` already answers "Password
must be at least 12 characters". The design's hint copy changes to match. Two
different floors across the two account-creation paths is exactly the split
that becomes a defect.

**8. `family_name` is set equal to the household name at provision.** The
design asks for one name only ("Household name", helper "Shown at the top of
the sidebar"), which maps to `households.name`. Rather than add a field the
design does not draw, `family_name` takes the same value. The invite preview
then reads "join the Andreas & Christine household", which is fine.

---

## 3. Data model

One migration, `00003_signups.sql`.

```sql
CREATE TABLE signups (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email       citext      NOT NULL,
    token_hash  bytea       NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX signups_email_created_idx ON signups (email, created_at DESC);
```

Shaped after `magic_links`, deliberately. Three properties are load-bearing:

- **`email` is not unique.** Several live tokens for one address are fine; the
  first consumed wins, and the second then finds the address registered and
  fails with `domain.ErrAlreadyExists` (mapped to 409 by `MapDomainError`'s
  backstop case, which exists for precisely this race).
- **`token_hash` is unique**, and only the hash is ever stored — the same rule
  every token in this codebase follows.
- **No `user_id`**, because there is no user yet. This is why `signups` is a
  new table rather than a column on an existing one; see section 3.1.

The TTL is 24 hours: `magicLinkTTL`'s 15 minutes is too short for "I'll do
this tonight", and `inviteTTL`'s 7 days is too long to leave an unverified
address holding a provisioning token.

### 3.1 Why a new table rather than reusing one

Recorded because "why not reuse `magic_links`" is the first question anyone
will ask.

| Candidate | Why it fails |
|---|---|
| `magic_links` | `user_id` is `NOT NULL REFERENCES users(id)`. A sign-up has no user. Reuse means nullable `user_id`, a new `email` column, a `purpose` discriminator, and a branch in `MagicLinkRepository.Consume` — which returns a userID — on the path that must keep working while a household is locked. |
| `invites` | `household_id` and `invited_by` are both `NOT NULL REFERENCES`. A sign-up has neither. Reuse means dropping both, and teaching `InviteRepository.Accept` — whose doc comment is about permanently unusable invites — to create a household first. |
| `invites`, household created up front | Creates households with no owner. `requireAnotherOwner` and `ValidateMembershipRemoval` exist to make an ownerless household impossible. Also leaves an abandoned household row per unverified request, which on an open endpoint means strangers filling `households`. |
| No table, self-signed token | Nothing to stamp consumed, so one token provisions unlimited households until it expires; nothing to count, so the per-address rate limit has no data. Needs a new signing key, and no signed-token machinery exists here. |

Each reuse drops a `NOT NULL` foreign key that is currently doing real work.

### 3.2 Retention

`signups` is the first table in this codebase a stranger can grow at will, and
**nothing here prunes anything** — `magic_links`, `invites`, `sessions` and
`login_attempts` all grow forever. `adminctl prune-signups` ships with the
table, deleting consumed and expired rows past a window. The migration carries
a comment saying why this table has a pruner when its siblings do not, so the
asymmetry reads as a decision rather than an oversight.

---

## 4. Domain

**`domain/currency.go`** — the ISO 4217 active-code set and
`ParseCurrency(code) (string, error)`, which uppercases and checks membership.
`normalizeCurrency` (`usecase/household.go`) delegates to it, and so does
`domain.NewMoney`, which today accepts `ZZZ` because it only checks
three-uppercase-letters. This closes `HANDOVER.md`'s outstanding format-only
item as a side effect, because sign-up is the first place a stranger picks the
value. Existing `money_test.go` expectations change accordingly.

**`domain.TokenLifecycle(now, expiresAt, consumedAt) TokenState`** — returns
`TokenLive | TokenConsumed | TokenExpired`. The rule it holds is that
**consumed is checked before expired**; that ordering is load-bearing (an
accepted-then-expired invite must report accepted) and currently lives only
inside `checkInviteLive`. Three token flows now need it.

The *states* are shared; the *errors* are not. `checkInviteLive` keeps
returning `domain.ErrInviteAlreadyAccepted` / `ErrInviteExpired` because the
HTTP layer maps those to 409/410 with invite-specific copy. Sign-up maps
`TokenExpired` to the existing `domain.ErrTokenExpired` (410) and
`TokenConsumed` to a new `usecase.ErrSignupAlreadyUsed` (409). The two are kept
apart because the next action differs: an expired link means start again, a
consumed one means the household already exists and the answer is to sign in.
`domain.ErrAlreadyExists` is deliberately *not* reused for the consumed case —
its copy is "That already exists.", which tells that person nothing useful.

The `switch` over `TokenState` gets a `default` that refuses, per this
project's fail-closed rule.

`magic_links` is left alone: its liveness check lives in SQL, in `Consume`'s
guarded `UPDATE`, so there is no Go-side duplicate to extract.

---

## 5. Usecase

### 5.1 `HouseholdBlueprint`

The single definition of what a new household *consists of*:

```go
type HouseholdBlueprint struct {
    Name                  string
    FamilyName            string
    PrimaryCurrency       string
    SecondaryCurrency     string
    ShowSecondaryCurrency bool
    OwnerDisplayName      string
    OwnerRole             domain.Role        // always RoleOwner
    OwnerCapabilities     domain.Capabilities // always AllCapabilities()
    Notifications         NotificationPreferences
    // Spaces are not a field: domain.BuiltinSpaces needs the household ID,
    // which does not exist until inside Provision's transaction.
}
```

`Seed` and `Provision` both build from it and then apply it differently.
**They deliberately do not share an implementation.** `Seed`'s doc comment
states that each write is gated on its own idempotency check so a partially
failed run is retryable step by step — non-transactional on purpose.
`Provision` must be atomic. One implementation would cost one of them its
defining property. What they *do* share is the blueprint, plus
`domain.BuiltinSpaces` (already shared) and a new
`usecase.DefaultNotificationPreferences()`, so the defaults cannot drift.

Seed passes `SGD` / `IDR` / show=true, keeping the design's household as it
is. Provision passes the chosen currency, `SecondaryCurrency` **equal to it**,
and show=false.

Setting the two currencies equal is deliberate, and checked: `households` has
no constraint requiring them to differ, and `normalizeCurrency`
(`usecase/household.go:118`) validates each field independently, so equality is
accepted rather than a provisioning failure waiting inside `Provision`'s
transaction. The reason not to leave the column's `IDR` default is that
`CurrencyPanel` renders its toggle label straight from the column — a
household in São Paulo would otherwise find "Show IDR equivalents" sitting in
Settings, referring to a currency nobody chose. Equal-to-primary makes the
toggle inert but coherent, and makes the missing secondary picker (section 9) a
visible gap rather than a surprise.

### 5.2 `usecase/password.go`

`minInvitePasswordLength`, `maxPasswordLength`, `ErrPasswordTooShort` and
`ErrPasswordTooLong` move out of `invite.go` — they are invite-named but not
invite-specific — into one `validatePassword(plain string) error` used by
`InviteService.Accept` and `SignupService.Complete`. The constant loses its
`Invite` prefix.

`AuthService.verifyPassword` keeps its own ceiling check and its deliberate
refusal to surface a sentinel: a too-long password at sign-in must fail
exactly like a wrong one. Its existing comment explaining why stays.

### 5.3 `SignupRepository`

```go
type SignupDetails struct {
    ID         string
    Email      string
    ExpiresAt  time.Time
    ConsumedAt *time.Time
}

type ProvisionedHousehold struct {
    UserID       string
    HouseholdID  string
    MembershipID string
}

type SignupRepository interface {
    Create(ctx context.Context, email string, tokenHash []byte, expiresAt time.Time) error
    ByTokenHash(ctx context.Context, tokenHash []byte) (SignupDetails, error)
    // CountSince counts sign-up requests for this address since a cutoff.
    // Unlike MagicLinkRepository.CountSince it does not join through users --
    // there is no user to join to -- so it can report a non-zero count for an
    // address that has no account, and must, or the rate limit would itself
    // distinguish registered addresses from unregistered ones.
    CountSince(ctx context.Context, email string, since time.Time) (int, error)
    // Provision creates the household, the owner user, the owner membership,
    // every builtin space and the notification preferences, and stamps the
    // signup consumed -- all in one transaction. Either all of it happens or
    // none of it does.
    //
    // The owner's email address is read from the signup row this transaction
    // is already touching; it is deliberately NOT a parameter. The address
    // that gets an account must be the one the mailed token actually proved,
    // and passing it in would let a caller substitute a different one between
    // Complete's read and this write.
    //
    // A partial provision leaves a users row occupying users.email's unique
    // index with no membership under it, which makes that address permanently
    // unable to sign up again: a retry could never create a second user with
    // it. That is the same failure InviteRepository.Accept's doc comment
    // describes, and this method exists for the same reason.
    //
    // Returns domain.ErrTokenExpired, with nothing written, when the signup is
    // no longer usable -- consumed or expired. Like InviteRepository.Accept's
    // guarded UPDATE, this collapses the two cases into one zero-rows result
    // and so cannot tell them apart; SignupService.Complete's own
    // TokenLifecycle read is what distinguishes them for the caller, and this
    // answer is authoritative only for the race window between that read and
    // this write.
    Provision(ctx context.Context, signupID, passwordHash string,
        b HouseholdBlueprint) (ProvisionedHousehold, error)
    // Prune deletes consumed and expired rows older than the cutoff.
    Prune(ctx context.Context, before time.Time) (int64, error)
}
```

`Provision` calls `domain.BuiltinSpaces(newHouseholdID)` itself, because the
ID does not exist until inside the transaction. The knowledge of which spaces
a household starts with stays in domain; the adapter only executes it inside
the transaction that needs it.

### 5.4 `SignupService`

**`Request(ctx, email) error` — always returns nil, always silent.** Its
contract is `RequestMagicLink`'s, for the identical reason, and its doc
comment must say so:

- Both the rate-limit count and the "does this address have an account" read
  run **unconditionally, in a fixed order, on every call**. `RequestMagicLink`
  once returned as soon as the rate-limit check decided the outcome, and the
  resulting difference in the *number of repository reads* distinguished the
  rate-limited case as surely as an error would have.
- Mail is sent off the request path, so a slow or wedged relay cannot make the
  known-address branch measurably slower.
- Every failure below the branch point — token generation, the `INSERT`, the
  send — is logged at error level with a hashed address and returns nil, never
  propagates. Each is reachable only from that branch, so propagating any of
  them is the same oracle a propagated mailer error would be. **Anyone adding
  a step below that point owes it the same treatment**, and the comment says
  so; that comment is the reason the existing one has not leaked.
- Both branches send mail, through the two new `Mailer` methods in section 5.5.
  A fresh address gets a create-household link; a registered one gets "you
  already have an account, sign in here" with no token. That is what keeps the
  endpoint silent while remaining useful to a human who forgot.

**`Preview(ctx, token) (SignupPreview, error)`** — resolves the token, checks
liveness via `domain.TokenLifecycle`, returns `{Email}`. Mirrors
`InviteService.Preview`.

**`Complete(ctx, token, householdName, displayName, currency, password) (SignInResult, error)`** —
validates the password through `validatePassword`, the currency through
`domain.ParseCurrency`, and the household name as non-blank; builds the
blueprint; calls `Provision`; then signs the new owner in through the same
package-level `issueSession` that `SignIn` and `InviteService.Accept` use, so
the session is indistinguishable from theirs down to how it is minted.

`SignupDeps` mirrors `AuthDeps` and `InviteDeps`. It carries no
`MembershipRepository`: every membership write goes through `Provision`'s
transaction, and a bare `MembershipRepository.Create` here would reintroduce
the orphaned-user defect both `CreateWithMembership` and `Accept` exist to
prevent.

### 5.5 The `Mailer` port grows two methods

`usecase.Mailer` is currently exactly `SendMagicLink` and `SendInvite`. Both
sign-up branches need one:

```go
SendSignupLink(ctx context.Context, to, url string) error
SendSignupForExistingAccount(ctx context.Context, to, signInURL string) error
```

Neither takes a display name: at request time nobody has told us one, and
inventing a greeting from the local part of the address would be worse than
having none. Two new templates in the mail adapter accordingly.

**Both are sent off the request goroutine**, through the same fire-and-forget
shape as `sendMagicLinkAsync` — including its `recover()`, since nothing
supervises a goroutine once it leaves the request path and an unrecovered
panic there takes down every unrelated in-flight request. Both log a hashed
address on failure and return nothing, per section 5.4's contract.

`SendSignupForExistingAccount` matters as much as the other for the property
in decision 3: if only the fresh branch sent mail, the *absence* of an email
would tell anyone who could observe the mailbox that the address is
registered.

---

## 6. HTTP surface

| Route | Guard | Answer |
|---|---|---|
| `POST /api/v1/auth/sign-up` | public | Always `202 {"status":"accepted"}` — identical for fresh, registered and rate-limited addresses. Matches `handleRequestMagicLink`'s existing body exactly. |
| `GET /api/v1/auth/sign-up/{token}` | public | `200 {"email":"…"}`. `410 TOKEN_EXPIRED`, `409 ALREADY_EXISTS`. |
| `POST /api/v1/auth/sign-up/{token}/complete` | public | `completeSignIn` — the me bundle plus session and CSRF cookies, byte-identical in shape to sign-in. |
| `GET /api/v1/currencies` | public | `200 {"currencies":[{"code":"SGD","symbol":"S$","name":"Singapore dollar"},…]}` |

The three sign-up routes sit under `/auth/sign-up` rather than a top-level
`/sign-ups/{token}` mirroring `/invites/{token}`. The deviation is deliberate:
the request step must sit beside `/auth/magic-link`, and splitting one flow
across two prefixes is worse than the asymmetry. `/currencies` is top-level
because it is not part of the sign-up flow — the Settings panel reads it too.

Every handler decodes through `decodeJSONBody`, so the 1 KiB
`maxRequestBodyBytes` bound applies. The completion body — household name,
display name, currency, password — fits well inside it.

`completeSignIn` is reused verbatim. It assembles the me bundle and generates
the CSRF token *before* writing either cookie, so a failure at either step
never leaves a live session cookie beside a 500. Sign-up gets that property
for free by not reimplementing it.

### 6.1 Error mapping

Most of what sign-up can fail with is already mapped:
`ErrPasswordTooShort`/`TooLong`, `ErrInvalidMoney` (→ `422 INVALID_CURRENCY`),
`ErrTokenExpired` (→ `410 TOKEN_EXPIRED`, whose copy "This link has expired or
has already been used." already fits) and `ErrAlreadyExists` (→ `409`, the
backstop for the two-live-tokens race in section 3).

Two new sentinels, both following `ErrSpaceNameRequired`'s existing shape:

| Sentinel | Status | Code |
|---|---|---|
| `usecase.ErrHouseholdNameRequired` | 422 | `HOUSEHOLD_NAME_REQUIRED` |
| `usecase.ErrSignupAlreadyUsed` | 409 | `SIGNUP_ALREADY_USED` — "This link has already been used. Try signing in instead." |

**`usecase.ErrInviteeAlreadyRegistered` must never be returned by any sign-up
path.** It maps to `409 EMAIL_ALREADY_REGISTERED`, which is the oracle
decision 3 exists to prevent.

---

## 7. Frontend

**`/sign-up` → `SignUpScreen`.** The design's create card with email only.
CTA "Create household". No `Forgot?`, no or-divider, no magic-link button —
the design gates all three on `authNotCreate`. Footer "Already set up? /
Sign in".

**`/sign-up/:token` → `SignUpCompleteScreen`.** Previews the token, then
renders the same card with **Household name** (helper "Shown at the top of the
sidebar. Change it any time."), **currency select**, **Your name**,
**Password** (hint "At least 12 characters"), and the email pre-filled and
read-only. CTA "Create household". On success the session cookie is already
set, so it redirects into the app.

**`SignInScreen` gains the design's footer verbatim:** "No household yet? /
Create one" → `/sign-up`.

**`MagicLinkSentPanel` becomes a thin caller of a shared
`CheckYourEmailPanel`** taking heading, body and resend copy. Sign-up's body
describes *both* outcomes in one sentence — the panel cannot know which mail
was sent and must not appear to. The existing panel's conditional phrasing
("If {email} has an account, we've sent…") is the technique to copy.

**Token failure states reuse `InviteScreen`'s established voice** — it already
handles expired ("This invite has expired. Ask whoever invited you to send a
new one.") and already-accepted. Sign-up's equivalents point back at
`/sign-up`.

### 7.1 One public-route module

`preAuthPathPrefixes` (`web/src/api/client.ts:71`) and `publicRoutePrefixes`
(`web/src/api/unauthorizedRedirect.ts:42`) are two hand-maintained lists with
nothing tying either to the route tree. `HANDOVER.md` already flags that a
future public route calling `useMe()` reintroduces a bug fixed once. Sign-up
adds two routes and one API prefix to both, so this stops being optional.

One exported module holds them; both files import it. A test walks the router
tree and fails if a route rendering a pre-auth screen is absent from the list.
`/api/v1/currencies` belongs in `preAuthPathPrefixes` too — the currency
select fetches before any session exists.

### 7.2 Currency select

Populated from `GET /api/v1/currencies`, not free text: a stranger should not
have to know ISO codes. The frontend stops maintaining `CURRENCY_SYMBOLS`
(`features/settings/copy.ts:55`) as a parallel list — `currencyLabel` renders
from server data. One list, one home, which is the backend's.

**Required, with no pre-selected currency.** Defaulting it to SGD would ship
the exact wrong-currency first impression decision 5 exists to avoid, silently,
for everyone who does not notice the field.

---

## 8. Abuse controls

Not hardening — the direct consequence of decision 4.

**Per address: 3 requests/hour**, counted through `SignupRepository.CountSince`,
mirroring `magicLinkPerHourLimit`. Being over the limit is silent, like every
other branch.

**Per IP: an in-memory token bucket in the HTTP layer.** Varying the address
defeats the per-address limit entirely, and that is the SMTP amplification
path. **This is per-process and does not survive a second replica** — stated
in a comment at the line someone adding one would read, with the migration
path named (a `signup_attempts` table, or a shared store).

**A global daily ceiling on sign-up mail**, as a backstop for the case both
limits are being worked at once. Counted from `signups` rows created since
midnight rather than held in memory — unlike the per-IP bucket this one must
survive a restart, or restarting the API becomes the way to reset it.

All three answer `202 {"status":"accepted"}`. A rate limit that answered `429`
would be an oracle in its own right, distinguishing a real address from a
stranger's by which one hit a limit.

---

## 9. In scope because sign-up makes them reachable

**`adminctl unlock-household` and `create-invite` take `--email`.** Both
resolve "the household" through `usecase.AndreasEmail` today
(`cmd/adminctl/main.go:337`), whose own comment says it exists because there
is "exactly one household per deployment". The moment a stranger signs up,
those commands act on the wrong household. `--email` resolves through any
member's address; the Andreas default survives for development only.

**`AvatarInitial` derives from the first Unicode grapheme, and the column
widens from `char(1)` to `text`.** A non-ASCII display name currently produces
a permanent replacement-character avatar with no profile-edit endpoint to fix
it. Cosmetic for two known adults; stranger-facing the moment anyone
registers.

**`CurrencyPanel` has no secondary-currency picker.** A household that enables
the toggle cannot choose what to compare against. Existing gap, now reachable
by strangers. Goes in `FEATURE_TRACKER.md` as 🟡 with that reason. **Not built
here** — it is Settings work, not sign-up work.

**`GetMembershipByUser` gains `ORDER BY joined_at, id`.** Still `LIMIT 1`,
still one household per account, but deterministic rather than arbitrary, so
the seam degrades predictably across many tenants instead of picking at
random.

### 9.1 Recorded, not re-litigated

The household-wide lockout is uncapped, and both properties were accepted
deliberately for two spouses who already know each other's addresses
(`usecase/auth.go`, at length). For paying customers it becomes "anyone who
knows a customer's address can hold them out indefinitely, and support has no
button". The decision is not reopened here. `unlock-household` is the support
tool, and it is spec B's first console action.

---

## 10. Testing

The bar is `docs/LEARNING.md`'s checklist. Specifically:

**The oracle test asserts what is actually true**, which is narrower than
"everything is identical" and needs stating precisely, because the obvious
over-claim is untestable and an implementer will discover that on day one.

The three branches genuinely differ in what they write: a fresh under-limit
address does `NewToken` + `signups.Create`; a registered one does neither; an
over-limit one does neither. `RequestMagicLink` has exactly the same
asymmetry (`MagicLinks.Create` on the known branch only) and it was accepted,
so this is not a new exposure. What the test must assert:

- **Identical status and body** — `202 {"status":"accepted"}` on all three.
- **No error propagated** on any branch, including when the mailer fails.
- **Both reads always run, unconditionally, in the same fixed order** — the
  rate-limit count and the address lookup, on every call, whatever the
  outcome. This is the real lesson from `RequestMagicLink`: the leak there was
  a read being *skipped* on one branch, not a differing write count. A test
  that pins the read order and the fact that neither read is conditional is
  the one that would have caught it.

**The transaction test forces a failure mid-`Provision`** and asserts no
household, no user, no membership, no spaces, and the signup still unconsumed.

**Both get mutation-checked** per `proving-tests-can-fail`: break the code
deliberately, watch the test go red, restore. Five tests in this project
passed against deliberately broken code, so a green test proves nothing until
it has been seen to fail.

**The route-walk matrices in `api_test.go` extend to all four new routes**,
including that none of them requires a session and none requires CSRF.

**Frontend tests register every request with `stubFetchRoutes`** — a stub that
ignores the URL has silently passed broken code twice here.

**The flow is walked in a real browser** per
`verifying-in-the-real-environment`: a form plus a redirect plus a cookie is
exactly the shape jsdom has lied about in this codebase before.

**Sibling sweep before closing.** Per `hunting-sibling-defects`, after each
fix, grep for the shape rather than the instance — five defects here were
fixed at one site while siblings kept the bug.

---

## 11. Docs to update in the same change

- **`SYSTEM_DESIGN.md`** — the `signups` table, the four routes, the
  provisioning flow, and the fact that a household can now be created without
  an invite. Use the `maintaining-system-design` skill.
- **`FEATURE_TRACKER.md`** — new rows for sign-up and provisioning; the 🟡 for
  the missing secondary-currency picker; recount the summary table.
- **`LEARNING.md`** — whatever this work teaches.
- **`HANDOVER.md`** — the build order changed; say so.

---

## 12. Deferred

**Spec B — the platform admin console.** A platform admin belongs to no
household and reads across all of them, so it is a **different principal type,
not a third `domain.Role`**. Everything resists the role approach: `role IN
('owner','limited')`, `owners_hold_all_capabilities`, non-null
`memberships.household_id`, `requireOwner`, and
`validateCapabilitiesForRole`'s fail-closed default. Its own table, its own
auth surface, its own `/api/v1/admin/**` route group with its own middleware —
which also means it can be deleted later, where a third role woven through
domain invariants could not.

**Many households per account**, with a switcher. Decision 2 leaves the schema
able to express it.

**A secondary-currency picker** in `CurrencyPanel` (section 9).

**Retention for `magic_links`, `invites`, `sessions` and `login_attempts`.**
`prune-signups` handles only the table this spec adds. The others still grow
forever.
