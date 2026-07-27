# Accounts — the first feature of slice 2 (Money)

Written 2026-07-28. This is the first slice of Money, and the first feature in
the product where the `money` capability actually guards a route.

A household can sign up, invite people and configure itself, and then has
nowhere to put a number. This spec adds accounts: what the household owns and
owes, entered by hand, totalled into a net worth figure.

It is deliberately the root of the Money dependency graph. Transactions attach
to an account, net worth is a sum of account balances, bills name a paying
account, and goals name a funding source. None of those can be built first
without inventing an account to hang them on.

---

## 1. Scope

**In:**

- One `accounts` table, one domain type, one service, one repository port and
  its Postgres adapter.
- Five routes under `/api/v1/accounts`, gated on the `money` capability for
  reads and on `money` plus owner for writes.
- The Finances page at `/money`, with three cards: net worth, the
  assets-and-liabilities breakdown, and the accounts list.
- An add/edit modal built on the existing `components/Modal`.
- Archive and restore.

**Out, and why:**

- **The 12-month net worth trend.** It needs balance snapshots — a second
  table and a separate decision about when a snapshot is written. Section 12.
- **The recent-transactions strip.** It has no data until Transactions ships,
  and the design draws no empty state for it. An empty card that promises
  future usefulness is a placeholder that looks considered, which this project
  treats as worse than an absence.
- **Automatic bank sync.** SGFinDex access is restricted to licensed financial
  institutions. Decision 1.
- **Custom account types.** Decision 3.

**Not made smaller than it is.** This started as "one table, one service, one
modal" and grew a page with three cards, five routes, two guards wired for the
first time, archive with restore, and four empty states. That is stated here
rather than discovered halfway through. If it needs cutting, archive and
restore are the clean severable piece.

---

## 2. Decisions

Each of these was asked and answered rather than assumed. Where a decision
went against the design or against the smaller option, the reason is recorded
at the point someone would try to change it.

### Decision 1 — no source picker; the button opens the form

The design's `3a` is a chooser with two cards: "Connect a Singapore bank ·
Recommended" and "Manual account". SGFinDex access is restricted to licensed
financial institutions, so the first card can never turn on for this product.
It is not a "coming soon" — it is a permanently dead option.

A chooser with one live branch is a click that teaches nothing, and a
permanently disabled card reads as a promise we cannot keep. So the button
opens the form directly, and it is labelled **"+ Add account"** rather than the
design's "+ Link account", because nothing is being linked to anything.

This is a deliberate divergence from the design's copy. Do not "fix" it back
without first making bank sync possible.

**`BankSyncProvider` is not created by this spec.** Three documents currently
claim it exists (`docs/HANDOVER.md:172`, `docs/FEATURE_TRACKER.md:57`,
`CLAUDE.md:108`); it exists in none of them — only `docs/SYSTEM_DESIGN.md:145`
is accurate. Manual entry needs no port, and a port with one implementation and
no second caller is the wrong shape. Section 11 corrects the three documents.
Add the port when CSV import arrives and there is a second implementation for it
to abstract over.

### Decision 2 — balance is opening balance plus transactions

An account stores an **opening balance** and the date that balance was true.
The current balance is that figure plus everything transactions have done
since. Today there is no transactions table, so the sum is empty and the
current balance equals the opening balance — but the repository method is
written as a sum from the start, so next slice adds a join rather than changing
a contract.

The rejected alternative was a typed balance that transactions never move. It
is simpler and it goes stale: net worth silently drifts from reality with
nothing on screen to say so.

**The balance is derived on read, not kept as a running total.** A stored
running total has to be updated by every write path that touches a transaction
— create, edit, delete, recategorise — and the first one that forgets leaves a
balance that is wrong with no way to tell since when. A household's transaction
count is small (the design shows 247 in a month), so a SQL sum is cheap and
cannot disagree with the ledger it is derived from.

**`opening_balance_as_of` is load-bearing, not decoration.** Enter an account
with today's balance, then import last month's transactions, and without that
date those transactions are subtracted from a balance that already reflected
them — the account ends up wrong by a month of spending, and nothing on screen
explains it. Only transactions dated after `opening_balance_as_of` count
toward the derived balance.

### Decision 3 — five fixed types, custom types deferred

The types are `cash`, `investment`, `property`, `loan`, `credit_card`. The
first three are assets; the last two are liabilities. `credit_card` is not in
the design's four breakdown bars; it is here because a household with a credit
card and no way to record it notices immediately.

Custom per-household types were considered and deferred. The cost is not the
name — it is that a custom type must also declare which side of the net worth
subtraction it falls on, that the breakdown chart needs a rule for types it has
never seen, and that per-household types must be seeded at household creation,
which reaches into `HouseholdBlueprint` (`api/internal/usecase/blueprint.go:32`)
and the provisioning transaction in `SignupRepository.Provision`. That
transaction is what a stranger signing up depends on, and its atomicity is
documented at length. Touching it for a feature nobody has asked for yet is the
wrong trade.

What a household gets instead: the **Nickname** field is free text, so "Gold
savings" or "Arisan" is a `cash` account with the name the household uses. All
they lose is a separate bar in the breakdown.

Two choices keep custom types cheap later, and both are free now:

- **Type is `text` with a CHECK constraint**, not a Postgres `enum`. Widening a
  CHECK is one migration; altering an enum type has real restrictions.
- **Asset-or-liability is derived in the domain from the type, never stored on
  the account row.** When custom types arrive, that function becomes a lookup
  against a new table and no existing account row changes. No backfill.

`docs/FEATURE_TRACKER.md` gains a ⬜ row for custom account types so this is
recorded rather than remembered.

### Decision 4 — `money` to read, `money` plus owner to write

Roles here are `owner` and `limited`. The seed makes the two adults owners with
every capability; the two children are `limited`, and neither holds `money`
(`api/internal/usecase/seed.go:255`). But Settings lets an owner switch Money on
for a child — the design says "off for kids by default", not "never for kids" —
so a twelve-year-old with Money access is a state the product genuinely
supports.

That is what decides it. If the `money` capability granted everything, granting
it to a child would let them edit the family's balances, and nothing in the
design suggests that is intended. So reads need `money`; writes need `money`
**and** owner.

The guards stack rather than substitute — but not for the reason it first
appears. **An owner without the `money` capability is not a representable
state.** `domain.ValidateMembershipChange` (`internal/domain/identity.go:123`)
refuses any owner who does not hold every capability, and every creation path
grants `AllCapabilities()`. So `requireCapability(CapMoney)` on a write route is
redundant *given today's invariant*.

It is stacked anyway, deliberately: the alternative is for the write routes to
depend on a rule enforced in a different layer for a different reason. If the
owner-holds-everything invariant is ever relaxed — and it is a product decision,
not a law — every write route that had leaned on it would silently open. The
cost of the redundant guard is one middleware call; the cost of the coupling is
a security regression with no failing test.

**This means there is no test that can prove the stacking matters**, because the
state it defends against cannot be constructed. Section 8 says so where the test
list would otherwise imply one exists.

A fifth "manage money" capability — letting an adult see finances without
editing them — was considered and rejected: it is a new switch in Settings that
the design does not draw, for a distinction nobody has asked for.

**Authorisation lives in the HTTP layer.** `AccountService` takes no actor
parameter. It enforces what makes a *valid* account; the router enforces who
may ask. A route without its guard has no second line of defence.

### Decision 5 — a limited member sees no figures at all

The design's toggle copy is "Kayla & Ethan can see this account exists, not the
balance." That says what happens to an account row and not what happens to the
rest of the screen, which is almost entirely money.

A limited member sees only the accounts whose Visible-to-kids toggle is on,
listed by nickname and type, **with no amounts anywhere** — no net worth card,
no breakdown chart, no per-account balance.

Showing the family total while hiding the parts was rejected for two reasons.
It leaks: with the list of which accounts exist and a running total, individual
balances become inferable as accounts are added and removed. And it raises "why
can I see the total but not the parts?", which no screen in the design answers.

Gating the whole page on owner instead was rejected because it leaves a switch
in Settings that visibly does nothing.

**"Kids" means members with the `limited` role.** There is no age field
anywhere in the product, so that is the only definition the data supports. The
column and API field are named `visible_to_limited_members` /
`visibleToLimitedMembers` for that reason; the UI label stays the design's
"Visible to kids", because that is what a person calls it.

### Decision 6 — an account carries its own currency

The design shows BCA Tahapan holding `Rp 85,400,000` with `≈ S$6,880` beneath
it, inside a household whose net worth is a single SGD figure. So accounts
carry their own currency and net worth converts everything into the
household's primary currency.

An account's currency may be any code **`domain.ParseSelectableCurrency`**
accepts — the two-minor-unit gate, not the broader `ParseCurrency`.
`Money.String()` hard-codes two decimal places, so an account denominated in
JPY (zero minor units) or KWD (three) would have every amount rendered wrong,
which is the identical reason a household cannot choose one as its primary
currency. One rule for both, so the two cannot drift apart.

**The FX provider knows exactly one pair.** `fx.StaticProvider`
(`api/internal/adapter/fx/static.go:20`) holds SGD↔IDR, "per the design's
Settings screen", and returns an error for everything else. Since self-serve
sign-up lets a stranger pick any allowlisted currency as their primary, a
household mixing currencies outside that pair has no rate available.

In practice most households hold every account in their primary currency, where
conversion is the identity and exact; the seed household's SGD/IDR pair is
covered. The gap is real but narrow.

**When no rate exists, the account is excluded from net worth and the exclusion
is stated on screen** — "2 accounts not included: no exchange rate for USD".
A net worth quietly missing an account looks identical to a correct one, which
is the failure worth preventing.

**The reachable case is not a mixed-currency household — it is Settings.** An
owner can change the household's primary currency on a screen that already
exists. A household whose accounts are all in SGD, switching its primary to
EUR, turns *every* account unconvertible at once: net worth goes blank and the
screen would otherwise say "4 accounts not included: no exchange rate for SGD"
to a household whose accounts are all in one currency. That is an ordinary
action, not an exotic one, and it is the state most likely to be hit.

So the Finances screen distinguishes two cases with different copy. **Some
accounts unconvertible** — net worth is shown, with the exclusion line beneath
it. **Every account unconvertible** — no net worth figure at all, and a plain
explanation that no exchange rate is available between the household's currency
and the accounts it holds, naming the currencies involved so the way out
(change the primary currency back, or wait for a live rate source) is
discoverable. A net worth of zero must never be shown here; zero is a claim
about the household's money, and the truth is that we cannot compute it.

Warning the owner *before* a primary-currency change that would strand every
account is the better product answer and is deferred, not rejected — it belongs
to the Settings screen, needs its own copy and test, and the state it prevents
is visible and reversible without it. Section 12 records it.

Restricting accounts to the household's primary or secondary currency was
rejected: a self-serve household currently has its secondary set equal to its
primary with no screen to change it (the existing 🟡 in the tracker), so that
restriction would mean "primary only" for every household but the seeded one,
and lifting it properly means building a currency picker this slice has no room
for.

### Decision 7 — owner is a nullable membership reference; NULL means shared

The `3c` form offers Andreas, Christine, Shared. One nullable reference stores
all three states, and "shared" is the natural absence of an owner.

An owner reference plus a separate `is_shared` boolean was rejected because it
can represent a row that names Christine *and* claims to be shared, and nothing
would resolve that. Make the bad state unrepresentable.

**Every member of the household appears in the picker**, not only owners. A
teenager with their own savings account is ordinary, and excluding limited
members would be a rule the design never states. Narrowing a list later is
easy; widening one after people have data is not.

**A removed member's accounts become shared.** The column is `ON DELETE SET
NULL`, so this is the database's behaviour and no application code runs.
Refusing the removal instead would mean an owner cannot clean up a departed
member without first reassigning every account they own — a dead end if they do
not know which ones those are. Deleting the accounts would destroy financial
history, and once transactions exist it would take those with it. Falling back
to shared loses only the label, which is the least valuable thing on the row.

### Decision 8 — archive, never delete

The design has no remove control anywhere in the Money screens — not on the
`3c` form, not on the accounts list. This is an addition to the design, and is
recorded as one.

An account is never deleted. Archiving stamps `archived_at`; the account leaves
the accounts list, leaves net worth, and leaves the breakdown. Any transaction
that later points at it keeps working. A "Show archived" view lists archived
accounts with a restore action, so a mistake is recoverable.

Delete-while-empty-then-archive-once-used was rejected: the same control would
do two different things depending on invisible state, and the boundary moves
under the user with no explanation on screen. No removal at all was rejected
because "I typed the wrong thing and now it is permanent" is a bad first
impression for a product being sold.

### Decision 9 — debts are stored positive; the sign comes from the type

A `loan` or `credit_card` account stores the amount **owed as a non-negative
number**, and the domain negates it when computing net worth, based on the
type.

Letting someone type a negative figure sounds more flexible and is a trap: type
`14500` for a car loan under that scheme and net worth counts the debt as an
asset, so the household appears twenty-nine thousand dollars richer than it is,
with nothing on screen to catch it. Deriving the sign from the type makes that
error unrepresentable.

Asset types accept any figure, including a negative one for an overdrawn
account. Liability types refuse a negative opening balance.

### Decision 10 — one endpoint returns both the list and the summary

`GET /api/v1/accounts` returns the accounts and the summary together, because
they are one screen and they must agree with each other.

Two endpoints were rejected because the limited-member redaction rule would
then be written twice, in two handlers, and this project has a specific,
repeated lesson about a defect fixed at one site while its siblings kept the
bug. Computing net worth in the frontend was rejected outright: converting IDR
to SGD in TypeScript puts floating point in a monetary path.

---

## 3. Data model

One new table.

```sql
CREATE TABLE accounts (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id             uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    nickname                 text        NOT NULL,
    type                     text        NOT NULL
                                         CHECK (type IN ('cash', 'investment', 'property',
                                                         'loan', 'credit_card')),
    owner_membership_id      uuid        REFERENCES memberships(id) ON DELETE SET NULL,
    opening_balance_minor    bigint      NOT NULL,
    opening_balance_currency char(3)     NOT NULL,
    opening_balance_as_of    date        NOT NULL,
    count_toward_net_worth   boolean     NOT NULL DEFAULT true,
    visible_to_limited_members boolean   NOT NULL DEFAULT false,
    archived_at              timestamptz,
    created_at               timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT liabilities_are_not_negative CHECK (
        type NOT IN ('loan', 'credit_card') OR opening_balance_minor >= 0
    )
);

CREATE INDEX accounts_household_idx ON accounts (household_id) WHERE archived_at IS NULL;
```

**Notes on the shape, in the order someone would question them.**

`type` is `text` with a CHECK, not an enum — decision 3.

`owner_membership_id` is nullable and means shared when NULL — decision 7. It
references `memberships(id)` rather than `users(id)` because an account belongs
to someone *in this household*; a user reference would let an account name
somebody who is not a member.

The default on `count_toward_net_worth` is `true` and on
`visible_to_limited_members` is `false`, matching the design's own toggle
states at `3c` — the first is drawn filled, the second grey.

`liabilities_are_not_negative` enforces decision 9 at the database as well as in
the service, because it is the constraint that stops a debt from being counted
as an asset and that is worth two lines of defence.

The partial index matches the query the accounts list actually runs: live
accounts for one household. Archived accounts are read rarely and can use a
sequential scan.

**Currency codes are stored on the account, not inherited from the household.**
A household's primary currency can change in Settings; an account's balance was
denominated in whatever it was denominated in, and rewriting it would silently
restate history.

**There is no `updated_at`**, deliberately. No existing table has one —
`households`, `users` and `memberships` all carry only `created_at` or
`joined_at` — and a column named "last updated" that nothing maintains is a
lie the next reader will believe. The question it would answer for an account,
"when was this balance last true", is already answered better by
`opening_balance_as_of`.

---

## 4. Domain

New file `internal/domain/account.go`, importing the standard library only.

```go
// AccountType is what kind of thing an account is. It decides which side of
// the net worth subtraction the account falls on, which is why the asset or
// liability answer is derived here rather than stored on the row: when
// per-household custom types arrive, this function becomes a lookup and no
// existing account changes.
type AccountType string

const (
    AccountCash       AccountType = "cash"
    AccountInvestment AccountType = "investment"
    AccountProperty   AccountType = "property"
    AccountLoan       AccountType = "loan"
    AccountCreditCard AccountType = "credit_card"
)

// ParseAccountType refuses anything it does not recognise. The default clause
// is the point: a type arriving from a request or a database column is a value
// this code did not construct, and guessing at one would put an account on the
// wrong side of net worth.
func ParseAccountType(s string) (AccountType, error)

// IsLiability reports whether this type is money owed rather than money held.
func (t AccountType) IsLiability() bool

// SignedNetWorthAmount returns the amount this account contributes to net
// worth: the balance as given for an asset, negated for a liability. A
// liability's stored amount is the non-negative sum owed (see the
// liabilities_are_not_negative constraint), so the minus sign is produced here
// and never typed by a person.
//
// It returns an error rather than negating blindly: negating math.MinInt64 in
// two's complement returns math.MinInt64 itself, so a naive negation would
// turn the largest possible debt into the largest possible asset. Money.String
// already guards the same edge for the same reason.
func (t AccountType) SignedNetWorthAmount(m Money) (Money, error)
```

`Account` itself is a plain struct of the columns above, with `Money` for the
opening balance. New sentinels join `domain/errors.go`:
`ErrUnknownAccountType`, `ErrAccountNicknameRequired`,
`ErrLiabilityBalanceNegative`, `ErrOpeningBalanceInFuture`.

---

## 5. Usecase

### 5.1 `AccountRepository`

Added to `usecase/ports.go`, narrow, following the nine repositories already
there.

```go
type AccountRepository interface {
    // List returns one household's accounts. Archived accounts are included
    // only when includeArchived is true; they never contribute to any total.
    //
    // Balance is the opening balance plus every transaction dated after
    // opening_balance_as_of. There is no transactions table yet, so today the
    // sum is empty and Balance equals the opening balance -- the method is
    // shaped as a sum from the start so the next slice adds a join rather than
    // changing this contract. See the spec's decision 2 for why the balance is
    // derived on read rather than kept as a running total.
    List(ctx context.Context, householdID string, includeArchived bool) ([]AccountView, error)
    Get(ctx context.Context, householdID, accountID string) (AccountView, error)
    Create(ctx context.Context, a domain.Account) (domain.Account, error)
    Update(ctx context.Context, a domain.Account) (domain.Account, error)
    // SetArchived stamps or clears archived_at. Accounts are never deleted --
    // transactions will reference them, and destroying an account would take
    // its history with it.
    SetArchived(ctx context.Context, householdID, accountID string, archived bool, at time.Time) error
    // MembershipBelongsToHousehold answers whether a membership is in this
    // household, so an account can never be assigned to a member of another
    // one. It lives here rather than on MembershipRepository because that port
    // is already used by sign-in and does not need widening for this.
    MembershipBelongsToHousehold(ctx context.Context, householdID, membershipID string) (bool, error)
}
```

`AccountView` is the account joined to its owner's display name, which is what
every consumer of the list actually wants — the same shape and the same reason
as the existing `MemberView`.

### 5.2 `AccountService`

New file `usecase/account.go`. Takes no actor parameter.

**Validation, on create and update:** nickname required after trimming; type
through `domain.ParseAccountType`; currency through
`domain.ParseSelectableCurrency`, the same validator sign-up uses, so `ZZZ` is
refused here for the same reason and JPY for the reason in decision 6;
`opening_balance_as_of` not in the future, read from the injected `Clock`; a
liability's opening balance not negative; and an owner membership, if given,
belonging to this household.

**`NetWorth` composes the summary.** It needs the household's primary currency
(`HouseholdRepository`) and the `FXRateProvider`.

The order of operations is not incidental and is written down where the loop
is: **convert, then add.** `domain.Money.Add` refuses to add two different
currencies — deliberately — so each account's minor units go through
`Rate.Apply` into the primary currency first, and only then get summed. Summing
first and converting after fails on the second account of a mixed-currency
household. Rounding therefore happens per account, half away from zero, as
`Rate.Apply` already does; the total is never re-rounded, so the figure is
deterministic.

The summary it returns:

- `Currency` — the household's primary.
- `NetWorth` — assets minus liabilities, over accounts that are live and have
  `count_toward_net_worth` set and could be converted.
- `Assets`, `Liabilities` — the two sides of that subtraction.
- `Breakdown` — one entry per type that has at least one live account, with the
  type's total. Not a fixed four buckets: the design draws "Car loan" as its
  own bar, which is per-type behaviour, and it keeps `loan` and `credit_card`
  visually separate.
- `ExcludedNoRate` — accounts left out because no rate was available, each with
  its currency, so the screen can name it.
- `ExcludedByChoice` — how many accounts were left out because their
  Count-toward-net-worth toggle is off.

**An account with the toggle off still appears in the breakdown.** The toggle's
copy is "Include this balance in the family total" — the total, specifically.
The consequence is that the bars will not always sum to the stated net worth,
so the screen carries one line explaining why when that happens.

---

## 6. HTTP surface

```
GET    /api/v1/accounts              session + money
POST   /api/v1/accounts              session + money + CSRF + owner
PATCH  /api/v1/accounts/{id}         session + money + CSRF + owner
POST   /api/v1/accounts/{id}/archive session + money + CSRF + owner
POST   /api/v1/accounts/{id}/restore session + money + CSRF + owner
```

This is the first use of `requireCapability` anywhere in the product. Until
now the promise that the server enforces capabilities independently of the UI
has been vacuous — the middleware existed and no route used it.

**Archive and restore get their own routes rather than a field on PATCH.** If
archiving were a patchable field, an ordinary edit that happened to include it
would archive the account as a side effect of saving a nickname. A state
transition that removes something from view deserves its own door.

**`requireCapability` moves to its own file.** It currently lives in
`middleware_csrf.go` (line 72) for no reason other than where it was first
written. This is the change that makes someone look at it, and one file / one
job is a requirement here rather than a preference.

### 6.1 The response

`GET /api/v1/accounts` answers 200 with both halves:

```json
{
  "accounts": [
    {
      "id": "…",
      "nickname": "DBS Everyday",
      "type": "cash",
      "ownerMembershipId": "…",
      "ownerName": "Andreas",
      "balance": { "amountMinor": 824055, "currency": "SGD" },
      "balanceAsOf": "2026-07-26",
      "countTowardNetWorth": true,
      "visibleToLimitedMembers": false,
      "archivedAt": null
    }
  ],
  "summary": {
    "currency": "SGD",
    "netWorthMinor": 24835000,
    "assetsMinor": 26285000,
    "liabilitiesMinor": 1450000,
    "breakdown": [{ "type": "cash", "totalMinor": 6199000 }],
    "excludedNoRate": [{ "accountId": "…", "currency": "USD" }],
    "excludedByChoice": 1
  }
}
```

`?include_archived=true` adds archived accounts to `accounts`. They never
affect `summary`.

Amounts cross the wire as minor units plus a code, never as formatted strings.
`domain.Money.String()` hard-codes two decimals and puts the code in front — it
stays a backend debugging aid, and formatting for a screen is the frontend's
job.

### 6.2 The redacted response

For a caller whose role is `limited`, the handler — not the service — shapes a
different response:

- `accounts` contains only rows with `visibleToLimitedMembers` true.
- Each row **omits `balance` and `balanceAsOf` entirely.** Absent, not zero: a
  zeroed amount still reads as a real balance.
- `summary` is **omitted entirely.** A net worth of zero would say "this family
  has nothing", which is a different and worse untruth than saying nothing.

The response still carries a JSON body, as every 2xx except 204 must, because
`apiFetch` throws on an ok response it cannot parse.

### 6.3 Error mapping

Following the existing pattern — a usecase sentinel the HTTP layer maps to a
422 with a field-specific code:

| Sentinel | Status | Code |
|---|---|---|
| `ErrAccountNicknameRequired` | 422 | `NICKNAME_REQUIRED` |
| `ErrUnknownAccountType` | 422 | `INVALID_TYPE` |
| `ErrInvalidMoney` (from `ParseSelectableCurrency`) | 422 | `INVALID_CURRENCY` |
| `ErrLiabilityBalanceNegative` | 422 | `INVALID_BALANCE` |
| `ErrOpeningBalanceInFuture` | 422 | `INVALID_AS_OF` |
| owner not in this household | 422 | `INVALID_OWNER` |
| `ErrNotFound` | 404 | `NOT_FOUND` |

---

## 7. Frontend

**Finances replaces the placeholder at `/money`.** The design treats Finances
as the Money space's landing page, with Transactions, Budget, Goals and Bills
as siblings beneath it. `/money/$` keeps its placeholder for those four. The
sidebar is untouched — it renders from the server's space list and this slice
adds nothing to it.

**New `web/src/features/money/`**, following the shape `features/settings/`
already uses: a page component, one component per card, the modal, `schemas.ts`
for parsing the response, `copy.ts` for the design's wording, and a small module
mapping the five types to their labels. Every server call goes through
`apiFetch`.

**The modal is the design's `3c` panel** on the existing `components/Modal`
primitive — the one that reaches genuine `:modal` state. Do not reintroduce a
declarative `open` attribute. The same component serves add and edit; the only
difference is whether it opens populated. It drops `3c`'s connected-bank header
strip, which describes a sync that does not exist.

**One money formatting helper**, taking minor units, a currency code and the
symbol `GET /api/v1/currencies` already serves
(`api/internal/adapter/http/currency_handlers.go:19`). It uses
`Intl.NumberFormat` for grouping and renders IDR without decimals, matching the
design's `Rp 85,400,000`, even though the allowlist treats IDR as a two-decimal
currency. That is a display choice only — the stored value keeps its minor
units, and nothing rounds on the way into the database. One helper rather than per-component formatting, because four
components formatting independently will disagree about separators.

### 7.1 The four states

**No accounts at all** — one first-run panel, not three empty cards. This is the
screen every new household sees straight after signing up.

**Some accounts not counted** — a line beneath net worth naming both reasons
separately: how many were left out for having no exchange rate, and how many
because their toggle is off. The fixes are different — one is "we cannot", the
other is "you chose" — so they are not merged into one number.

**No account convertible at all** — no net worth figure, and the explanation
from decision 6 naming the currencies involved. Never a zero. This is the state
a household lands in by changing its primary currency in Settings, so it is a
first-class screen state rather than an error.

**A limited member** — the accounts shared with them, no figures. If nothing has
been shared, a plain explanation rather than an empty list, so it does not read
as a broken page.

**Show archived** — archived accounts with a restore action, and a plain line
when there are none.

---

## 8. Testing

**Domain.** `ParseAccountType` refuses an unknown value; the asset-or-liability
answer for each of the five types; a liability's positive balance becoming
negative through `SignedNetWorthAmount`.

**Service, against in-memory doubles**, the way every existing service is
tested:

- An owner membership from another household is refused.
- Conversion happens before summation — a household holding SGD and IDR
  accounts totals correctly.
- An account with no available rate is excluded from net worth *and* named in
  `ExcludedNoRate`.
- When *no* account can be converted, the summary reports no net worth figure
  rather than a zero — the state a primary-currency change in Settings
  produces.
- An account with `count_toward_net_worth` off is absent from the total and
  present in the breakdown.
- Archived accounts are in neither.

**Repository, against real Postgres via testcontainers**, like the nine
existing ones:

- A shared account round-trips as NULL owner and returns as shared, not as a
  sentinel.
- Deleting a membership leaves its accounts in place, owner now NULL. This is
  database behaviour, so only a database can prove it.

**HTTP — the important ones**, because this is the first capability-gated
route. The route-walk matrices in `api/internal/adapter/http/api_test.go` extend
to cover every accounts route against every caller shape that can exist: no
session; a signed-in member without `money`; a limited member **with** `money`;
an owner.

**The third is the one that needs a new fixture.** `testEnv`'s limited member
holds `calendar` and `chores` only (`api_test.go:267`), so as things stand every
accounts write route would refuse them at `requireCapability` and the
owner-gated matrix would pass without ever exercising `requireOwner` — a
vacuous green. The env gains a second limited member who *does* hold `money`,
and that is the caller the owner matrix uses for these routes.

**There is no "owner without `money`" case**, because it cannot be built:
`domain.ValidateMembershipChange` refuses an owner missing any capability. The
guard is still stacked, for the reason decision 4 gives; it simply has no
reachable state to be tested against, and pretending otherwise would mean
writing a test that asserts nothing.

The redaction test asserts the limited member's response has **no balance key at
all**. Asserting `balance === 0` would pass against exactly the bug it exists to
prevent.

**Frontend.** `stubFetchRoutes` for every request — it matches on method and URL
and throws on anything unregistered. A stub that ignores the URL has twice
silently passed broken code in this project.

**A browser walk before this is called done**, with its own written criteria
list the way slices 0 and 1 have. jsdom's `<dialog>` is a stub, and five passing
tests once hid a modal that threw on every open in production.

**At least one test mutation-checked.** The candidate: remove `requireOwner`
from the create route and confirm a test goes red. If nothing does, the guard
matrix is decoration.

---

## 9. Definition of done

1. `make lint && make test` green.
2. At least one new test mutation-checked.
3. The browser walk passes, recorded in its own verification file.
4. The three documents in section 11 updated as part of the work.

---

## 10. What this closes

- **`requireCapability` is used.** The deadline recorded in `docs/HANDOVER.md`
  section 4 item 1 closes here.
- **The first Money table exists**, so Transactions has an account to attach to
  and a visibility rule to inherit rather than invent.

---

## 11. Docs to update in the same change

**`docs/SYSTEM_DESIGN.md`** — use the `maintaining-system-design` skill. The
`accounts` table, the five new routes and their guards, the first
capability-gated route in the request pipeline, and the fact that Finances is a
real screen rather than a placeholder.

**`docs/FEATURE_TRACKER.md`** — move Manual account entry, Accounts by owner,
Net worth (without the trend) and Assets and liabilities breakdown out of ⬜.
Net worth is 🟡 until the 12-month trend exists, and the gap is named. Add ⬜
rows for custom account types and for the archived-accounts view if it ships
separately. Recount the summary table by counting symbols, not by adjusting the
totals.

**`docs/LEARNING.md`** — whatever this work teaches. The convert-then-add
ordering is a likely first entry: it is a defect that only appears in a
mixed-currency household, so a single-currency test suite would never see it.

**Correct the `BankSyncProvider` claim** in `docs/HANDOVER.md:172`,
`docs/FEATURE_TRACKER.md:57` and `CLAUDE.md:108`. The port does not exist. Only
`docs/SYSTEM_DESIGN.md:145` is currently accurate.

---

## 12. Deferred

- **12-month net worth trend.** Needs balance snapshots — a second table, plus
  a decision about when a snapshot is written (nightly? on every balance
  change? on read?). Its own small spec.
- **Custom account types.** Decision 3. Needs the breakdown-chart rule and the
  management screens the design does not draw.
- **The recent-transactions strip on Finances.** Arrives with Transactions.
- **Bank sync.** Not buildable — SGFinDex is restricted to licensed financial
  institutions.
- **The secondary-currency picker** (the existing 🟡). Decision 6 works around
  its absence rather than closing it; a household still cannot choose what its
  second currency is.
- **A warning in Settings before a primary-currency change strands every
  account.** Decision 6. It belongs to the Settings screen, needs its own copy
  and test, and the state it prevents is visible and reversible without it.
