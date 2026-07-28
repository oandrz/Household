# Transactions — the second feature of slice 2 (Money)

Written 2026-07-29. Accounts gave the household somewhere to put a number.
This gives it somewhere to put what happened to that number.

It is the feature the rest of Money waits on. Budget's envelopes are sums of
categorised expenses. Goals fund from somewhere. Bills are transactions with a
due date. `AccountView.Balance` is currently a copy of the opening balance
because there is nothing to add to it; this is what turns it into a real sum,
and `AccountRepository.List`'s doc comment was written as a sum from the start
so that this slice adds a join rather than changing a contract.

---

## 1. Scope

**In:**

- Two tables — `transactions` and `categories` — with a domain type, a service,
  a repository port and a Postgres adapter for each.
- Four routes under `/api/v1/transactions` and one under `/api/v1/categories`,
  all gated on `money` **and** owner.
- The Transactions page at `/money/transactions`: the design's five filters,
  the day-grouped ledger, and "Load older transactions".
- The "Log a transaction" modal with its three kinds — Expense, Income,
  Transfer.
- The recent-transactions strip on Finances, which had no data until now.
- `AccountView.Balance` becomes the opening balance plus every transaction
  dated after `opening_balance_as_of`.

**Out, and why:**

- **Export CSV.** Section 2, decision 7. It is a seam, not a button.
- **The Budget screen.** Envelopes, pace, spending by person, the templates and
  "Edit categories" are the next feature. This one produces the data that
  screen reads and stops there.
- **Inline category editing on a ledger row.** The modal already edits a
  transaction's category. A second control for one field is more surface for
  the same outcome; the tracker row stays ⬜ with this reason.
- **Editing the category list.** The design puts "Edit categories" on the
  Budget screen. This slice seeds the list and reads it; nothing here renames,
  adds or archives a category.

**Not made smaller than it is.** Two tables, five routes, a page with five
filters and keyset pagination, a three-mode modal, a new card on an existing
page, and a change to a query that already ships. That is stated here rather
than discovered halfway through. If it needs cutting, the filter bar is the
clean severable piece — a ledger without filters is still a ledger.

---

## 2. Decisions

Each was asked and answered rather than assumed. Where one goes against an
existing precedent in this codebase, the reason is recorded at the point
someone would try to change it back.

### Decision 1 — a real `categories` table, seeded on first use

The design's modal has a Category dropdown; the Transactions screen's own
banner says "Every expense's **category** feeds that category's Budget spend
automatically"; and the Budget screen (not built) owns "13 categories", "Edit
categories" and three seeding templates. So either Transactions ships a
taxonomy or it borrows one that does not exist yet.

A free-text string on the transaction was rejected: the ledger's "Groceries"
and Budget's "Groceries" would be two unrelated values, and Budget would have
to backfill by matching text — a migration that is wrong for every household
that typed "groceries" or "Grocery".

Seeding the categories at household creation was rejected for the reason
Accounts decision 3 rejected custom account types: it reaches into
`HouseholdBlueprint` (`api/internal/usecase/blueprint.go:32`) and the
provisioning transaction in `SignupRepository.Provision`. That transaction is
what a stranger signing up depends on, and its atomicity is documented at
length. The trade did not flip; the same answer applies.

So the table ships now and a household gets its starter set the first time
anything asks for it.

**"First use" needs a definite moment, and it is `GET /api/v1/categories`.**
That endpoint seeds the starter set when the household has none. This is a read
that writes, which is unusual enough to carry a comment saying so. It is the
only moment that works: the modal needs a category list before the household's
first transaction exists, so seeding on first write is too late.

**The seed is one idempotent statement**, `INSERT … ON CONFLICT DO NOTHING`
against `UNIQUE (household_id, name)`, so two simultaneous first requests
cannot produce two sets. Not a read-then-write, which races.

**A household that clears its list does not get it silently rebuilt.**
Categories archive rather than delete (Budget's screen will do the archiving),
and an archived row still occupies its unique key, so the seed's `ON CONFLICT`
finds it and does nothing.

The starter set is the design's own, from its Budget screen: `Groceries`,
`Dining out`, `Transport`, `Petrol`, `Household`, `Kids & school`, `Health`,
`Utilities`, `Insurance`, `Subscriptions`, `Fun & hobbies`, `Giving` as
expense categories, and `Income` as the single income category.

### Decision 2 — a transfer is one row, not two mirrored legs

The design's modal has a third tab and the Finances strip shows `Transfer to
BCA · −S$500.00 · ≈ Rp 6.2 jt`. One action, two accounts.

Two mirrored rows sharing a group id would keep the balance query as one
uniform `SUM`, and was rejected. This project has four recorded defects where
an operation wrote part of what it accepted and reported success, and a skill
(`guarding-partial-writes`) that exists because of them. Two rows that must
always be created, edited and deleted together is that failure mode invited in
for a tidier query.

One row carries `from_account_id` and `to_account_id`. An expense leaves `to`
NULL; an income leaves `from` NULL; a transfer fills both. A transfer cannot
half-exist, and editing one is one write.

**The cost is paid in the balance query and the account filter**, and is paid
once. An account's balance subtracts rows where it is `from` and adds rows
where it is `to`, so the sum has a branch; "All accounts ▾" matches either
side. Both are written with the reason above them.

**A transfer must not change net worth and must not count as spend.** Money
leaving DBS and arriving in BCA is the same money. The two effects cancel by
construction — the same row supplies both — which is why this invariant is a
property of the shape rather than a rule someone has to remember.

### Decision 3 — a cross-currency transfer stores both amounts

The design's own example is cross-currency. One amount cannot describe both
sides: what arrives depends on the bank's rate and its spread, not on a
mid-market figure this product holds.

Converting on read through `FXRateProvider` was rejected twice over.
`fx.StaticProvider` (`api/internal/adapter/fx/static.go:20`) knows exactly one
pair, SGD↔IDR, so most cross-currency transfers would have no rate at all. And
a converted-on-read figure restates itself whenever the rate changes, so last
year's transfer shows a different number today — history that moves.

Refusing cross-currency transfers was rejected because the design's worked
example and the seeded household are exactly the case it forbids.

So the row stores `amount_minor` / `amount_currency` — what left, in the
from-account's currency — and `received_amount_minor` /
`received_amount_currency` — what arrived, in the to-account's.

**The received amount is required when the currencies differ, and optional
otherwise.** The first draft of this decision forbade it on a same-currency
transfer, on the grounds that the two figures must be equal. They are not: a
bank that charges S$2 to move S$500 between two SGD accounts credits S$498, and
forbidding the field would leave a household with no way to record what
actually arrived. So the modal always offers "Amount received" on a transfer,
prefilled with the amount sent; it is a required field only when the two
accounts differ in currency.

A separate fee transaction was the alternative, and is worse here: it needs a
category the starter set does not have, and it separates a fee from the
transfer that caused it, so deleting the transfer leaves the fee behind.

**Net worth therefore moves by the spread, and that is correct.** Sending
S$500 and receiving Rp 6,200,000 worth slightly less is a real loss, recorded
rather than hidden. A transfer between two accounts in the same currency
leaves net worth untouched to the cent, because both sides carry the same
figure.

### Decision 4 — a transaction has no currency of its own

A transaction is denominated in its account's currency: an expense on an IDR
account is in IDR. There is no currency picker on the modal, because a
transaction in a currency the account is not held in is not a thing that
happens — the bank converted it before it hit the statement.

The currency is nonetheless **stored on the transaction row**, not read through
the account. An account's currency is editable in the accounts modal;
correcting a mistyped account currency must not restate the currency of
everything that already happened on it.

### Decision 5 — a limited member has no Transactions page at all

This is a **deliberate divergence from the accounts precedent**, and the
reason belongs here because someone will try to make the two consistent.

Accounts decision 5 says a limited member sees the accounts flagged
visible-to-kids, by nickname and type, with **no amounts anywhere**. Applied
mechanically to a ledger, that is a table whose every figure is blank, next to
a "Spent this month" that has to be absent rather than zero. That is not a
product; it is a page that reads as broken.

So the transactions routes require **owner** for reads as well as writes, and
the frontend never offers a limited member the link. For a limited member, the
`money` capability means "see which accounts this household has" and nothing
further.

The alternative — showing real figures for transactions on accounts already
shared with them — was rejected because it overturns accounts decision 5 by the
back door: a few months of visible deltas reconstruct the balance the toggle
exists to hide.

**Consequence for the guard stack.** Every transactions route carries
`requireCapability(CapMoney)` **and** `requireOwner`, the same pair the
accounts write routes use, for the same reason accounts decision 4 gives: an
owner without `money` is not a representable state today
(`domain.ValidateMembershipChange`, `internal/domain/identity.go:123`), but the
routes must not lean on an invariant enforced in another layer for another
reason. The redundant guard costs one middleware call; the coupling would cost
a security regression with no failing test.

### Decision 6 — a transaction before the account's opening date is kept, marked, and excluded from the balance

Accounts decision 2 pinned that only transactions dated **after**
`opening_balance_as_of` move an account's balance — otherwise importing last
month's spending subtracts it from a balance that already reflected it, and the
account ends up a month wrong with nothing on screen explaining why.

The ordinary case that hits this is not an import. It is: add an account today
with today's balance, then log yesterday's kopitiam lunch. Today's balance
already includes that lunch.

Refusing the transaction was rejected — it blocks a first-week action and the
way out (editing the account's opening date) is something the error message
would have to teach. Counting it was rejected because it reopens accounts
decision 2 and reintroduces the double-count.

So it is **saved, listed, and marked**: the ledger row carries a plain note
that it predates the account's known balance and does not move it.

**It still counts toward budget spend and "Spent this month."** The money was
spent. Only the *balance* ignores it, because a balance is anchored to a figure
someone asserted was true on a date; spend is not anchored to anything. That
split is the non-obvious part and is commented where the sum is written.

### Decision 7 — Export CSV is deferred, because it is a seam

`web/src/api/client.ts`'s `apiFetch` is the only way the app talks to the
server, and it throws on an ok response it cannot parse as JSON. A CSV
download is not JSON, so it needs a second path out of the frontend.

Building the file in the browser from what the page has already fetched was
rejected outright: with keyset pagination the export silently omits everything
past the first page, producing a wrong file that looks right.

A real `GET /api/v1/transactions.csv` — session-cookie authenticated,
`Content-Disposition` attachment, honouring the same filters — is the correct
answer and is the first non-JSON response in the product. It needs its own
guard and its own test, and none of that is about the ledger.

The tracker row stays ⬜ with this reason recorded, so the next person knows
what they are picking up.

### Decision 8 — a transaction is hard deleted

This is a **deliberate override of accounts decision 8** ("archive, never
delete"), and the reason belongs here.

An account is never deleted because transactions reference it, and destroying
one takes its history with it. Nothing references a transaction. A soft
`deleted_at` would add a filter that every query and every total must remember
— the kind of omission this codebase has shipped before — in exchange for a
recovery screen the design never draws. An archive-with-restore view is a
whole screen state for rows that take four seconds to retype.

`DELETE /api/v1/transactions/{id}` removes the row and answers 204, the only
status in this product permitted to carry no body.

### Decision 9 — the amount is stored positive and the sign comes from the kind

Same shape and same reason as accounts decision 9. `amount_minor` carries a
`CHECK (amount_minor > 0)`; whether it adds to or subtracts from an account is
derived from `kind` and from which side of the row the account sits on.

Letting someone type a negative expense makes "I typed −52.30 for groceries and
my balance went up" representable. Deriving the sign makes it unrepresentable.

---

## 3. Data model

Two new tables, one new migration.

### 3.1 `categories`

```sql
CREATE TABLE categories (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name         text        NOT NULL,
    kind         text        NOT NULL CHECK (kind IN ('expense', 'income')),
    sort_order   integer     NOT NULL,
    archived_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, name)
);
```

`kind` splits expense categories from income ones so the modal offers Groceries
for an expense and never for an income. `text` with a CHECK rather than an
enum, for the reason accounts decision 3 gives: widening a CHECK is one
migration.

`UNIQUE (household_id, name)` is what makes the seed idempotent. It is not a
nicety.

`sort_order` fixes the order the design draws rather than sorting
alphabetically, which would put "Buffer" above "Groceries" for no reason a
household would recognise.

`archived_at` exists so Budget's "Edit categories" has somewhere to write
without this slice needing to build it, and so a cleared list is not silently
re-seeded (decision 1).

### 3.2 `transactions`

```sql
CREATE TABLE transactions (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id             uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    kind                     text        NOT NULL
                                         CHECK (kind IN ('expense', 'income', 'transfer')),
    occurred_on              date        NOT NULL,
    description              text        NOT NULL,
    category_id              uuid        REFERENCES categories(id) ON DELETE SET NULL,
    paid_by_membership_id    uuid        REFERENCES memberships(id) ON DELETE SET NULL,
    from_account_id          uuid        REFERENCES accounts(id) ON DELETE CASCADE,
    to_account_id            uuid        REFERENCES accounts(id) ON DELETE CASCADE,
    amount_minor             bigint      NOT NULL CHECK (amount_minor > 0),
    amount_currency          char(3)     NOT NULL,
    received_amount_minor    bigint,
    received_amount_currency char(3),
    created_at               timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT accounts_match_kind CHECK (
        (kind = 'expense'  AND from_account_id IS NOT NULL AND to_account_id IS NULL)
     OR (kind = 'income'   AND to_account_id IS NOT NULL AND from_account_id IS NULL)
     OR (kind = 'transfer' AND from_account_id IS NOT NULL AND to_account_id IS NOT NULL
                           AND from_account_id <> to_account_id)
    ),
    CONSTRAINT received_amount_pairs CHECK (
        (received_amount_minor IS NULL) = (received_amount_currency IS NULL)
    ),
    CONSTRAINT received_amount_is_a_transfer_thing CHECK (
        kind = 'transfer' OR received_amount_minor IS NULL
    ),
    CONSTRAINT received_amount_is_positive CHECK (
        received_amount_minor IS NULL OR received_amount_minor > 0
    ),
    CONSTRAINT transfer_has_no_category CHECK (
        kind <> 'transfer' OR category_id IS NULL
    )
);

CREATE INDEX transactions_household_date_idx
    ON transactions (household_id, occurred_on DESC, id DESC);
```

**Notes on the shape, in the order someone would question them.**

`accounts_match_kind` is the constraint that makes a nonsense row impossible: an
expense with a destination, a transfer with one leg, a transfer from an account
to itself. Every one of those would produce a balance that is wrong with
nothing on screen to explain it, so it is worth a check at the database as well
as in the service.

**The account foreign keys are `ON DELETE CASCADE`, and `RESTRICT` would be
wrong.** The application never deletes an account — accounts archive
(accounts decision 8) — so this clause is unreachable in ordinary use. It fires
in exactly one case: deleting a household cascades to its accounts, and a
`RESTRICT` from transactions would make that cascade fail. `CASCADE` is the
behaviour that is correct when it happens and never happens otherwise.

`category_id` and `paid_by_membership_id` are `ON DELETE SET NULL`, following
the same reasoning accounts decision 7 gives for a removed member's accounts:
losing a label is the least valuable thing on the row, and refusing the removal
means an owner cannot clean up a departed member without first reassigning
every transaction they paid for.

`received_amount_pairs` stops half a received amount — a figure with no
currency, or a currency with no figure — from ever being stored.
`received_amount_is_a_transfer_thing` keeps it off an expense or an income,
where it would have nothing to mean.

`transfer_has_no_category` keeps the design's own promise. The banner says a
category feeds Budget spend; a transfer is not spend, so it cannot carry one.

The index matches the keyset cursor exactly (section 6.2). Its column order is
the sort order, not a guess.

**There is no `updated_at`**, deliberately, for the reason the accounts spec
gives: no table in this schema has one, and a column named "last updated" that
nothing maintains is a lie the next reader will believe.

### 3.3 The change to `accounts`

None. The table is untouched. What changes is the query behind
`AccountRepository.List` — section 5.3.

---

## 4. Domain

New file `internal/domain/transaction.go`, importing the standard library only.

```go
// TransactionKind is what a transaction did: money left an account, money
// arrived in one, or money moved between two of them.
type TransactionKind string

const (
    TransactionExpense  TransactionKind = "expense"
    TransactionIncome   TransactionKind = "income"
    TransactionTransfer TransactionKind = "transfer"
)

// ParseTransactionKind refuses anything it does not recognise. The default
// clause is the point: a kind arriving from a request or a database column is
// a value this code did not construct, and guessing at one would put money on
// the wrong side of an account.
func ParseTransactionKind(s string) (TransactionKind, error)

// BalanceEffect reports what this transaction does to the named account's
// balance: negative when money left it, positive when money arrived, and zero
// when the account is not one of the two sides.
//
// A transfer supplies both effects from one row, which is why a transfer
// cannot change net worth -- the two sides are the same money, and there is no
// second row that could go missing.
//
// The received amount is what lands in the destination account, in that
// account's own currency. For a same-currency transfer it equals the amount
// sent; for a cross-currency one it is what the bank actually credited,
// including its spread. See the spec's decision 3.
func (t Transaction) BalanceEffect(accountID string) (Money, bool)
```

`Transaction` is a plain struct of the columns above, with `Money` for the
amount and for the optional received amount. New sentinels join
`domain/errors.go`:

- `ErrUnknownTransactionKind`
- `ErrTransactionDescriptionRequired`
- `ErrTransactionAmountNotPositive`
- `ErrTransactionAccountsInvalid` — the wrong accounts for the kind, or a
  transfer to and from the same account
- `ErrReceivedAmountRequired` — a cross-currency transfer without one
- `ErrReceivedAmountNotAllowed` — an expense or an income carrying one, where
  it would have nothing to mean. A same-currency transfer *may* carry one, so
  that a transfer fee is recordable (decision 3)
- `ErrCategoryKindMismatch` — an income categorised as Groceries

---

## 5. Usecase

### 5.1 `CategoryRepository`

Added to `usecase/ports.go`.

```go
type CategoryRepository interface {
    // List returns one household's categories in sort order. Archived
    // categories are included only when includeArchived is true.
    List(ctx context.Context, householdID string, includeArchived bool) ([]domain.Category, error)
    // EnsureSeeded creates the starter set for a household that has none. It
    // is idempotent and safe to run concurrently: it is one INSERT ... ON
    // CONFLICT DO NOTHING against UNIQUE (household_id, name), never a
    // read-then-write, which would race two simultaneous first requests into
    // two sets. An archived category still occupies its unique key, so a
    // household that cleared its list is not silently re-seeded. See the
    // spec's decision 1.
    EnsureSeeded(ctx context.Context, householdID string, starter []domain.Category) error
}
```

### 5.2 `TransactionRepository`

```go
type TransactionRepository interface {
    // List returns one household's transactions, newest first, matching every
    // filter that is set. It returns at most limit+1 rows so the caller can
    // tell whether another page exists without a second query.
    //
    // Paging is keyset on (occurred_on, id) descending, matching
    // transactions_household_date_idx. Offset paging was rejected: a
    // transaction added while someone is reading shifts every later row by
    // one, so a page boundary silently repeats or skips a transaction.
    List(ctx context.Context, householdID string, f TransactionFilter) ([]TransactionView, error)
    // Get reports domain.ErrNotFound when no transaction with this id exists
    // in this household -- including when one exists in a different
    // household, which must be indistinguishable from not existing at all.
    Get(ctx context.Context, householdID, transactionID string) (TransactionView, error)
    Create(ctx context.Context, t domain.Transaction) (domain.Transaction, error)
    // Update replaces every mutable column. TransactionService is what turns a
    // partial PATCH into a complete Transaction; this port never merges.
    Update(ctx context.Context, t domain.Transaction) (domain.Transaction, error)
    // Delete removes the row. Nothing references a transaction, so nothing is
    // orphaned -- see the spec's decision 8 for why this differs from
    // accounts, which are archived and never deleted.
    Delete(ctx context.Context, householdID, transactionID string) error
    // MonthTotals returns every transaction in one calendar month, which the
    // service converts and sums. It returns rows rather than a SQL SUM because
    // a mixed-currency household cannot be summed in SQL: each amount must go
    // through the FX provider first, and the provider lives in the usecase
    // layer. See the spec's section 5.5.
    //
    // Returning rows to produce two numbers is deliberate, not an oversight.
    // The bound is one household's transactions in one month -- the design's
    // own busiest example is 247 -- so the row count is small and known. A
    // SQL SUM would be correct only for a household whose transactions are all
    // in its primary currency, and having two code paths whose answers could
    // disagree is the trade this refuses. If profiling ever says otherwise,
    // the honest fix is a SQL sum per currency, not per row.
    MonthTotals(ctx context.Context, householdID string, month time.Time) ([]TransactionView, error)
}
```

`TransactionView` is the transaction joined to the names it displays — the
category's name, the payer's display name, and each account's nickname and
currency — which is what every consumer of the ledger actually wants. Same
shape and same reason as the existing `MemberView` and `AccountView`.

`TransactionFilter` carries the design's five filters plus paging: `Kind`,
`AccountID`, `CategoryID`, `PaidByMembershipID`, `Month`, `Cursor`, `Limit`.
An unset field means no filtering on it, following the `""` ⇄ SQL NULL
convention `ports.go` already documents.

**`AccountID` matches either side of a transfer.** A filter that only matched
`from_account_id` would hide money arriving in the account someone selected,
which is the half of the story they were looking for.

### 5.3 The change to `AccountRepository.List`

The port's contract does not change. Its doc comment already promises that
`Balance` is "the opening balance plus every transaction dated after
`Account.OpeningBalanceAsOf`", and that today the sum is empty. This slice
makes the sum real, in SQL:

```
opening_balance_minor
  - COALESCE(SUM(t.amount_minor) FILTER (WHERE t.from_account_id = a.id
             AND t.occurred_on > a.opening_balance_as_of), 0)
  + COALESCE(SUM(CASE WHEN t.received_amount_minor IS NOT NULL
                      THEN t.received_amount_minor ELSE t.amount_minor END)
             FILTER (WHERE t.to_account_id = a.id
             AND t.occurred_on > a.opening_balance_as_of), 0)
```

Three things in that expression are load-bearing and each gets a comment where
it is written:

- **The strict `>`** implements accounts decision 2 and this spec's decision 6.
  A transaction dated *on* the opening date is already reflected in the figure
  someone asserted was true that day.
- **The two filtered sums** are the branch decision 2 said this shape costs. An
  account can be the source of one transfer and the destination of another.
- **`received_amount_minor` on the incoming side** is what makes a
  cross-currency transfer credit the destination in its own currency. Using
  `amount_minor` there would add Singapore dollars to an Indonesian rupiah
  balance and the account would be wrong by a factor of ten thousand.

No currency conversion happens in this SQL, and none can: every amount in it is
already in the account's own currency (decision 4).

### 5.4 `CategoryService`

New file `usecase/category.go`. One job: return a household's categories,
seeding the starter set first when it has none.

The starter set lives in the domain as data, not in the service, so the seed
and any future template share one definition.

### 5.5 `TransactionService`

New file `usecase/transaction.go`. Takes no actor parameter — authorisation is
the HTTP layer's job, and this service enforces only what makes a transaction
*valid*.

**Validation, on create and update:**

- description required after trimming;
- `kind` through `domain.ParseTransactionKind`;
- amount positive;
- the accounts present and absent as `accounts_match_kind` requires, each
  belonging to this household, and a transfer's two accounts distinct;
- the amount's currency equal to the source account's (an expense or transfer)
  or the destination's (an income) — the service *derives* it from the account,
  and **the request struct carries no currency field at all**. Not a field the
  service silently overwrites: a handler that accepts a value it never persists
  is the exact shape `guarding-partial-writes` exists for, and four defects here
  have been that shape. The same applies to the received amount's currency;
- `received_amount` required when a transfer's two accounts differ in currency,
  optional when they match (a fee — decision 3), refused on an expense or an
  income, and always positive and in the destination account's currency;
- `category_id`, when given, belonging to this household and matching the
  kind — an income cannot be Groceries — and absent for a transfer;
- `paid_by_membership_id`, when given, belonging to this household.

**`MonthSummary` composes the two figures the design shows**, and both are
pinned here because neither was specified anywhere:

- **`Count`** — "247 in July" — every transaction whose `occurred_on` falls in
  the selected month, all three kinds. It is the count of what the ledger is
  showing, so it counts what the ledger shows.
- **`Spent`** — "Spent this month S$3,420.18" — the sum of `expense`
  transactions in that month. Income excluded, transfers excluded, because
  neither is spending.

**Convert, then add.** Each expense's amount goes through `Rate.Apply` into the
household's primary currency *before* being summed, because `domain.Money.Add`
refuses to add two currencies — deliberately. Summing first and converting
after fails on the second account of a mixed-currency household. This is
`docs/LEARNING.md` pattern 12, and `AccountService.Summary` already does the
same thing for the same reason. Rounding therefore happens per transaction,
half away from zero, as `Rate.Apply` already does; the total is never
re-rounded.

**A transaction with no available rate is excluded and named**, exactly as net
worth already handles it: `ExcludedNoRate` carries each one's currency so the
screen can say "3 transactions not included: no exchange rate for USD". A
quietly short total looks identical to a correct one, which is the failure
worth preventing.

**A transaction dated before its account's opening date still counts toward
`Spent`.** The money was spent; only the balance ignores it (decision 6). The
comment lives at the sum.

---

## 6. HTTP surface

```
GET    /api/v1/transactions        session + money + owner
POST   /api/v1/transactions        session + money + owner + CSRF
PATCH  /api/v1/transactions/{id}   session + money + owner + CSRF
DELETE /api/v1/transactions/{id}   session + money + owner + CSRF
GET    /api/v1/categories          session + money + owner
```

**Reads require owner, unlike the accounts read**, which is decision 5. The
divergence is deliberate and the handler carries a comment saying so, because
the obvious "fix" is to make the two consistent.

`DELETE` answers **204 with no body**. Every other 2xx in this product carries
JSON, because `apiFetch` throws on an ok response it cannot parse; 204 is the
one status exempt, and it is exempt because the frontend does not try to parse
it.

### 6.1 The list request

```
GET /api/v1/transactions
    ?kind=expense
    &account_id=…
    &category_id=…
    &paid_by=…
    &month=2026-07
    &cursor=2026-07-16:01J…
    &limit=50
```

Every filter is optional. `month` is `YYYY-MM` and is interpreted in the
household's own sense of a month — there is no timezone field in this product,
and `occurred_on` is a `date`, not a timestamp, precisely so that "18 July"
means 18 July regardless of where the server runs.

`cursor` is opaque to the frontend: `occurred_on` and `id` of the last row of
the previous page, which is exactly what the keyset predicate needs. `limit`
defaults to 50 and is capped, so a caller cannot ask for the whole ledger in
one request.

### 6.2 The list response

```json
{
  "transactions": [
    {
      "id": "…",
      "kind": "expense",
      "occurredOn": "2026-07-18",
      "description": "Cold Storage",
      "categoryId": "…",
      "categoryName": "Groceries",
      "paidByMembershipId": "…",
      "paidByName": "Christine",
      "fromAccountId": "…",
      "fromAccountName": "DBS Everyday",
      "toAccountId": null,
      "toAccountName": null,
      "amount": { "amountMinor": 5230, "currency": "SGD" },
      "receivedAmount": null,
      "beforeFromAccountOpeningBalance": false,
      "beforeToAccountOpeningBalance": null
    }
  ],
  "nextCursor": "2026-07-16:01J…",
  "summary": {
    "currency": "SGD",
    "month": "2026-07",
    "count": 247,
    "spentMinor": 342018,
    "excludedNoRate": [{ "transactionId": "…", "currency": "USD" }]
  }
}
```

`nextCursor` is `null` on the last page. That is what "Load older transactions
↓" hides itself on — not a row count, which would be wrong on a page that
happens to be exactly full.

**The before-opening-date flag is per side, and one boolean would have been
wrong.** A transfer has two accounts with two different `opening_balance_as_of`
dates, so it can predate one and not the other — money that leaves an account
that ignores it and arrives in one that counts it. A single flag would mark
such a row with a note that is half true, and the section 5.3 SQL already gets
this right per account. So the wire carries `beforeFromAccountOpeningBalance`
and `beforeToAccountOpeningBalance`, each `null` when that side is absent.

Both are the server's answer, not the frontend's arithmetic. The rule lives in
one place (decision 6), and a frontend recomputing it from `occurredOn` and an
account's `balanceAsOf` would be a second implementation of a rule that must
not drift.

Amounts cross the wire as minor units plus a code, never as formatted strings,
for the reason the accounts spec gives.

### 6.3 The categories response

```json
{ "categories": [ { "id": "…", "name": "Groceries", "kind": "expense" } ] }
```

Ordered by `sort_order`. Archived categories are omitted.

### 6.4 Error mapping

Following the existing pattern — a usecase sentinel the HTTP layer maps to a
422 with a field-specific code:

| Sentinel | Status | Code |
|---|---|---|
| `ErrTransactionDescriptionRequired` | 422 | `DESCRIPTION_REQUIRED` |
| `ErrUnknownTransactionKind` | 422 | `INVALID_KIND` |
| `ErrTransactionAmountNotPositive` | 422 | `INVALID_AMOUNT` |
| `ErrTransactionAccountsInvalid` | 422 | `INVALID_ACCOUNTS` |
| `ErrReceivedAmountRequired` | 422 | `RECEIVED_AMOUNT_REQUIRED` |
| `ErrReceivedAmountNotAllowed` | 422 | `RECEIVED_AMOUNT_NOT_ALLOWED` |
| `ErrCategoryKindMismatch` | 422 | `INVALID_CATEGORY` |
| an account or category in another household | 422 | `INVALID_ACCOUNTS` / `INVALID_CATEGORY` |
| `ErrNotFound` | 404 | `NOT_FOUND` |

An account or category belonging to another household maps to the same 422 as
an invalid one rather than a 404, because the caller may not learn whether that
id exists elsewhere.

---

## 7. Frontend

**A new page at `/money/transactions`**, with the design's `‹ Finances` link
back. `/money/$` keeps its placeholder for Budget, Goals and Bills.

**`web/src/features/money/` gains:**

- `TransactionsPage.tsx` — the header with its count line, the filter bar, the
  day-grouped ledger, "Load older transactions".
- `TransactionModal.tsx` — the design's "Log a transaction" panel on the
  existing `components/Modal` primitive, the one that reaches genuine `:modal`
  state. **Do not reintroduce a declarative `open` attribute.** The same
  component serves add and edit; the only difference is whether it opens
  populated.
- `RecentTransactionsCard.tsx` — the Finances strip, five newest, "See all"
  through to the page.
- `transactionKinds.ts` and additions to `copy.ts` and `schemas.ts`, following
  the shape the accounts work already established.

**The money formatting helper built for accounts is reused, not reimplemented.**
Four components formatting independently will disagree about separators.

**The modal's three kinds change which fields exist**, and that is the whole
interaction:

- **Expense** — amount, date, description, category (expense kinds only),
  account, paid by.
- **Income** — amount, date, description, category (income kinds only),
  account, and no "paid by", since nobody paid.
- **Transfer** — amount, date, description, from account, to account, no
  category, and an **"Amount received"** field labelled with the destination
  account's currency. It is prefilled with the amount sent and is optional
  while the two currencies match, so a transfer fee is recordable; it is
  required, and starts empty, when they differ — there is no honest figure to
  prefill it with.

### 7.1 The screen states

**No transactions at all** — one first-run panel with the modal's own opening,
not an empty table with headers. This is what a household sees the day after it
adds its first account.

**No transactions matching the filters** — different copy from the above, and a
way to clear the filters. A household that filtered to "Income · Petrol" and
saw the first-run panel would think its ledger had been wiped.

**Some transactions not counted in the month's spend** — a line naming how many
were excluded for want of an exchange rate and which currencies, matching the
line Finances already carries beneath net worth.

**A row dated before its account's opening balance** — the row shows a plain
marker saying it predates the account's known balance and does not move it,
**naming the account**, since a transfer can predate one side and not the
other and "does not move the balance" would otherwise be half true.

**No accounts yet** — the "+ Add transaction" button is disabled with an
explanation and a link to Finances, since a transaction with no account to
attach to cannot be created. The alternative, a modal whose account dropdown is
empty, is a dead end reached after four clicks.

---

## 8. Testing

**Domain.** `ParseTransactionKind` refuses an unknown value. `BalanceEffect`
for each kind and each side: an expense subtracts from its source, an income
adds to its destination, a transfer subtracts the sent amount from one and adds
the received amount to the other, and any account that is neither side gets
zero.

**Service, against in-memory doubles**, the way every existing service is
tested:

- A transfer between two accounts leaves net worth unchanged. This is the
  invariant decision 2 exists to protect, so it is asserted rather than assumed.
- A cross-currency transfer credits the destination with the *received* amount,
  not the sent one — the defect that would otherwise add SGD to an IDR balance.
- A cross-currency transfer with no received amount is refused. A
  same-currency transfer *with* one is accepted and credits the destination
  with the smaller figure — the fee case in decision 3. An expense carrying a
  received amount is refused.
- A transfer never appears in `Spent`, and neither does an income.
- A transaction dated before its account's opening date is absent from the
  account's balance and present in the month's spend. One test, both
  assertions, because the split is the thing that will get "simplified" later.
- A transfer that predates one account's opening date and not the other's
  moves exactly one of the two balances, and reports the two flags
  independently. This is the case a single boolean would have got wrong.
- An expense in a currency with no rate is excluded from `Spent` and named in
  `ExcludedNoRate`.
- Conversion happens before summation — a household spending in SGD and IDR
  totals correctly.
- An income categorised as an expense category is refused.
- An account or category from another household is refused.

**Repository, against real Postgres via testcontainers**, like the eleven
existing ones:

- Keyset pagination is stable: read page one, insert a transaction dated in the
  middle, read page two, and confirm no row is repeated or skipped. Offset
  paging fails this, which is why the port says what it says.
- `EnsureSeeded` run twice produces one set of categories. Run concurrently, it
  still produces one — the race the `ON CONFLICT` exists for.
- `EnsureSeeded` does nothing for a household whose only categories are
  archived.
- Deleting a membership leaves its transactions in place with `paid_by` NULL;
  deleting a household removes its accounts and transactions together. Both are
  database behaviour, so only a database can prove them.
- `accounts_match_kind` rejects a transfer to and from the same account. The
  constraint is a second line of defence and gets a test that proves it is
  armed.

**HTTP.** The route-walk matrices in `api/internal/adapter/http/api_test.go`
extend to every transactions and categories route against every caller shape:
no session; a signed-in member without `money`; **a limited member holding
`money`**; an owner. The third is the one that proves decision 5 — a limited
member with the capability is refused the ledger entirely, reads included. The
fixture for that caller already exists; the accounts work added it.

**Frontend.** `stubFetchRoutes` for every request — it matches on method and
URL and throws on anything unregistered. A stub that ignores the URL has twice
silently passed broken code in this project. Cover: the modal showing a second
amount field only when the two transfer accounts differ in currency; the
filter-empty state being distinct from the first-run state; "Load older
transactions" hiding itself when `nextCursor` is null.

**At least one test mutation-checked.** The candidate: change the balance sum's
strict `>` to `>=` and confirm the before-opening-date test goes red. If it
does not, decision 6 is not actually protected by anything.

**A browser walk before this is called done**, with its own written criteria
list, the way slices 0 and 1 and Accounts each have. jsdom's `<dialog>` is a
stub, and five passing tests once hid a modal that threw on every open in
production.

---

## 9. Definition of done

1. `make lint && make test` green.
2. At least one new test mutation-checked.
3. The browser walk passes, recorded in its own verification file.
4. The documents in section 11 updated as part of the work, not after it.

---

## 10. What this closes, and what it opens

**Closes.** `AccountView.Balance` stops being a copy of the opening balance and
becomes the sum its doc comment has promised since Accounts shipped. The
recent-transactions strip on Finances, deferred by the accounts spec for having
no data, gets its data.

**Opens.** Budget can be built: categorised expenses per month are exactly what
an envelope sums. Goals can name a funding source that means something. Bills
are transactions with a due date and an autopay flag.

**Still undefined after this.** `66% used`, `S$137/day left`, `on pace to save
S$1,780`, `4 of 4 on track`, and unspent budget rolling into a nominated goal
at month end. All five are Budget and Goals territory and must be pinned in
their own specs before an implementer invents one. This spec pins only the two
figures its own screen shows.

---

## 11. Docs to update in the same change

**`docs/SYSTEM_DESIGN.md`** — use the `maintaining-system-design` skill. The
two new tables and their relationships to `accounts` and `memberships`, the
five new routes and their guards, the reshaped accounts-balance query, and
Transactions as a real screen rather than a placeholder.

**`docs/FEATURE_TRACKER.md`** — move "Full ledger with filters", "Add
transaction (modal)" and "Recent transactions strip" out of ⬜. "Export CSV"
stays ⬜ with decision 7's reason recorded on the row. "Inline category
editing" stays ⬜ with its reason. Add a row for categories, which the design
draws only inside Budget. Recount the summary table by counting symbols, not by
adjusting the totals.

**`docs/LEARNING.md`** — whatever this work teaches. Likely candidates: the
`RESTRICT`-versus-`CASCADE` trap on a foreign key that only fires under a
household cascade, and whatever the keyset pagination test finds.

**`docs/HANDOVER.md`** — the next feature after this is Budget, and the five
derived figures in section 10 are what it must pin first.

---

## 12. Deferred

- **Export CSV.** Decision 7. It needs a non-JSON response path out of the
  frontend, with its own guard and test.
- **Inline category editing.** The modal already edits a category; a second
  control for one field is more surface for the same outcome.
- **Editing the category list** — rename, add, archive, and the design's three
  seeding templates. All of it lives on Budget's screen.
- **Recurring transactions.** Bills territory; the design draws recurrence only
  there.
- **Attachments and receipts.** The design draws none.
- **The 12-month net worth trend**, still. Transactions make a historical
  balance computable, which makes the trend cheaper than it was — but it is
  still its own decision about snapshots versus recomputation, and its own
  spec.
