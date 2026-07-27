# Hearth Accounts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A household records what it owns and owes by hand, and sees a net worth figure built from it — the first feature of slice 2 (Money), and the first route in the product that the `money` capability actually guards.

**Architecture:** One `accounts` table holds a nickname, a type, an optional owner, and an opening balance in its own currency. `GET /api/v1/accounts` returns the list and a computed summary together, converting each account into the household's primary currency through the existing `FXRateProvider` before summing, because `domain.Money.Add` refuses to add two currencies. Reads are gated on the `money` capability; writes on `money` plus owner. A `limited` member's response omits every amount and the whole summary.

**Tech Stack:** Everything from the identity and sign-up plans. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-07-28-hearth-accounts-design.md`

**Prerequisite:** `docs/superpowers/plans/2026-07-27-hearth-self-serve-signup.md` complete and green. Task numbering continues from it (that plan ended at Task 32).

## Global Constraints

- **`internal/domain` may import the standard library only.** `make lint-arch` enforces it, test files included. `time` is stdlib, so `domain.Account` holding `time.Time` is legal.
- **Money is `int64` minor units plus an ISO 4217 code, everywhere.** `float64` never appears in a monetary path, in Go or in TypeScript. `usecase.Rate` is a fraction, not a scaled decimal.
- **Convert, then add.** `domain.Money.Add` returns `ErrCurrencyMismatch` for two different currencies. Every summation converts each account into the household's primary currency through `Rate.Apply` first. Summing first and converting after fails on the second account of a mixed-currency household.
- **An account's currency goes through `domain.ParseSelectableCurrency`**, not `ParseCurrency`. `Money.String()` hard-codes two decimal places, so JPY (0 minor units) and KWD (3) are refused here for the identical reason a household cannot pick one as its primary currency.
- **Authorisation lives in the HTTP layer only.** `AccountService` takes no actor parameter. It enforces what makes a *valid* account; the router enforces who may ask.
- **A liability's stored amount is non-negative.** `loan` and `credit_card` store the sum owed as a positive number; the minus sign is produced by `AccountType.SignedNetWorthAmount` and never typed by a person.
- **Every 2xx except `204` carries a JSON body.** `apiFetch` throws `INVALID_RESPONSE` on an ok response it cannot parse.
- **Accounts are never deleted.** Archiving stamps `archived_at`. Transactions will reference these rows next slice.
- **A `switch` over a value that arrives from a request or a database column has a `default` that refuses.** `ParseAccountType` is the instance of this rule in this plan.
- **All user-visible copy comes verbatim from `design/Household Dashboard.dc.html`** (1314 lines), except where this plan names a deliberate divergence. The `3c` strings are: `Account details` / `Nickname` / `Owner` / `Type` / `Count toward net worth` / `Include this balance in the family total` / `Visible to kids` / `Kayla & Ethan can see this account exists, not the balance` / `Add account`. The Finances strings are: `Finances` / `Net worth` / `Last 12 months` / `Assets & liabilities` / `Net` / `Accounts`.
- **The button reads `+ Add account`, not the design's `+ Link account`.** Deliberate divergence — decision 1. Do not "fix" it back.
- Time enters through the `Clock` port. Tests never sleep.
- Every task ends with a commit.

## Scope note: what this plan does not build

`BankSyncProvider` is **not** created. `docs/HANDOVER.md:172`, `docs/FEATURE_TRACKER.md:57` and `CLAUDE.md:108` all claim it exists; it does not, and Task 41 corrects those three lines. Manual entry needs no port, and a port with one implementation and no second caller is the wrong shape.

The 12-month net worth trend, the recent-transactions strip, custom account types, and a warning before a primary-currency change are all deferred with reasons in the spec's section 12.

## File Structure

| Path | Responsibility |
|---|---|
| `api/internal/domain/account.go` | `AccountType`, its parser, the asset/liability rule, `SignedNetWorthAmount`, the `Account` struct |
| `api/internal/domain/errors.go` | Four new sentinels (modify) |
| `api/migrations/00004_accounts.sql` | The `accounts` table and its partial index |
| `api/internal/adapter/postgres/queries/account.sql` | sqlc source for the six account queries |
| `api/internal/adapter/postgres/account_repo.go` | `AccountRepository`'s Postgres implementation |
| `api/internal/usecase/ports.go` | `AccountRepository`, `AccountView` (modify) |
| `api/internal/usecase/account.go` | `AccountService` — validation, list, create, update, archive |
| `api/internal/usecase/networth.go` | `NetWorthSummary` and the convert-then-add composition |
| `api/internal/adapter/http/account_handlers.go` | The five account endpoints and the limited-member redaction |
| `api/internal/adapter/http/middleware_capability.go` | `requireCapability`, moved out of `middleware_csrf.go` |
| `api/internal/adapter/http/router.go` | Five routes, two guards (modify) |
| `web/src/features/money/schemas.ts` | zod schemas for the accounts response |
| `web/src/features/money/copy.ts` | The design's Finances and `3c` strings |
| `web/src/features/money/accountTypes.ts` | The five types and their labels |
| `web/src/features/money/formatMoney.ts` | The one place minor units become a string |
| `web/src/features/money/useAccounts.ts` | The query and the four mutations |
| `web/src/features/money/FinancesPage.tsx` | The page and its four states |
| `web/src/features/money/NetWorthCard.tsx` | Net worth and the exclusion lines |
| `web/src/features/money/BreakdownCard.tsx` | Assets & liabilities, one bar per populated type |
| `web/src/features/money/AccountsPanel.tsx` | The list, "Show archived", restore |
| `web/src/features/money/AccountModal.tsx` | The `3c` form, add and edit |

---

### Task 33: Domain — `AccountType`, the sign rule, and the `Account` struct

**Files:**
- Create: `api/internal/domain/account.go`
- Create: `api/internal/domain/account_test.go`
- Modify: `api/internal/domain/errors.go`

**Interfaces:**
- Consumes: `domain.Money`, `domain.ErrInvalidMoney`, `domain.ErrAmountOverflow` (all existing).
- Produces: `domain.AccountType` with constants `AccountCash`, `AccountInvestment`, `AccountProperty`, `AccountLoan`, `AccountCreditCard`; `domain.ParseAccountType(string) (AccountType, error)`; `(AccountType).IsLiability() bool`; `(AccountType).SignedNetWorthAmount(Money) (Money, error)`; the `domain.Account` struct; sentinels `ErrUnknownAccountType`, `ErrAccountNicknameRequired`, `ErrLiabilityBalanceNegative`, `ErrOpeningBalanceInFuture`, `ErrAccountOwnerNotInHousehold`.

- [ ] **Step 1: Write the failing test**

Create `api/internal/domain/account_test.go`:

```go
package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestParseAccountTypeAcceptsTheFiveKnownTypes(t *testing.T) {
	for _, want := range []domain.AccountType{
		domain.AccountCash, domain.AccountInvestment, domain.AccountProperty,
		domain.AccountLoan, domain.AccountCreditCard,
	} {
		got, err := domain.ParseAccountType(string(want))
		if err != nil {
			t.Fatalf("ParseAccountType(%q): %v", want, err)
		}
		if got != want {
			t.Errorf("ParseAccountType(%q) = %q", want, got)
		}
	}
}

// TestParseAccountTypeRefusesAnythingElse is the fail-closed rule: an account
// type arrives from a request body or a database column, so it is a value this
// code did not construct. Guessing at an unrecognised one would put an account
// on the wrong side of the net worth subtraction.
func TestParseAccountTypeRefusesAnythingElse(t *testing.T) {
	for _, input := range []string{"", "savings", "CASH", "cash ", "crypto"} {
		if _, err := domain.ParseAccountType(input); !errors.Is(err, domain.ErrUnknownAccountType) {
			t.Errorf("ParseAccountType(%q) err = %v, want ErrUnknownAccountType", input, err)
		}
	}
}

func TestIsLiabilityIsTrueOnlyForDebts(t *testing.T) {
	assets := []domain.AccountType{domain.AccountCash, domain.AccountInvestment, domain.AccountProperty}
	debts := []domain.AccountType{domain.AccountLoan, domain.AccountCreditCard}

	for _, a := range assets {
		if a.IsLiability() {
			t.Errorf("%q.IsLiability() = true, want false", a)
		}
	}
	for _, d := range debts {
		if !d.IsLiability() {
			t.Errorf("%q.IsLiability() = false, want true", d)
		}
	}
}

// TestSignedNetWorthAmountNegatesOnlyDebts pins the rule that stops a car loan
// from making a household look richer. The stored figure is the sum owed, as a
// positive number; the minus sign is produced here.
func TestSignedNetWorthAmountNegatesOnlyDebts(t *testing.T) {
	owed, err := domain.NewMoney(1_450_000, "SGD") // S$14,500.00
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}

	got, err := domain.AccountLoan.SignedNetWorthAmount(owed)
	if err != nil {
		t.Fatalf("SignedNetWorthAmount: %v", err)
	}
	if got.Amount != -1_450_000 || got.Currency != "SGD" {
		t.Errorf("loan = %+v, want {-1450000 SGD}", got)
	}

	got, err = domain.AccountCash.SignedNetWorthAmount(owed)
	if err != nil {
		t.Fatalf("SignedNetWorthAmount: %v", err)
	}
	if got.Amount != 1_450_000 {
		t.Errorf("cash = %+v, want {1450000 SGD}", got)
	}
}

// TestSignedNetWorthAmountRefusesMinInt64 guards the two's-complement edge:
// negating math.MinInt64 returns math.MinInt64 itself, so a naive negation
// would turn the largest possible debt into the largest possible asset.
func TestSignedNetWorthAmountRefusesMinInt64(t *testing.T) {
	worst := domain.Money{Amount: math.MinInt64, Currency: "SGD"}

	if _, err := domain.AccountLoan.SignedNetWorthAmount(worst); !errors.Is(err, domain.ErrAmountOverflow) {
		t.Fatalf("err = %v, want ErrAmountOverflow", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd api && go test ./internal/domain/ -run TestParseAccountType -v
```
Expected: FAIL — `undefined: domain.ParseAccountType`.

- [ ] **Step 3: Add the four sentinels**

Append to the second `var` block in `api/internal/domain/errors.go` (the one holding `ErrAmountOverflow` and `ErrInvalidMoney`):

```go
	ErrUnknownAccountType         = errors.New("unknown account type")
	ErrAccountNicknameRequired    = errors.New("an account nickname is required")
	ErrLiabilityBalanceNegative   = errors.New("a debt's balance is the amount owed and cannot be negative")
	ErrOpeningBalanceInFuture     = errors.New("an opening balance cannot be dated in the future")
	ErrAccountOwnerNotInHousehold = errors.New("that member is not in this household")
```

- [ ] **Step 4: Write `api/internal/domain/account.go`**

```go
package domain

import (
	"fmt"
	"math"
	"time"
)

// AccountType is what kind of thing an account is. It decides which side of
// the net worth subtraction the account falls on, which is why the
// asset-or-liability answer is derived here rather than stored on the row:
// when per-household custom types arrive, IsLiability becomes a lookup and
// no existing account row changes.
type AccountType string

const (
	AccountCash       AccountType = "cash"
	AccountInvestment AccountType = "investment"
	AccountProperty   AccountType = "property"
	AccountLoan       AccountType = "loan"
	AccountCreditCard AccountType = "credit_card"
)

// ParseAccountType refuses anything it does not recognise. The default is the
// point: a type arrives from a request body or a database column, so it is a
// value this code did not construct, and guessing at an unknown one would put
// an account on the wrong side of net worth. It does not trim or case-fold --
// the only writers are this API and this API's own migration, and accepting
// "CASH " here would mean the database CHECK constraint and this function
// disagreed about what is valid.
func ParseAccountType(s string) (AccountType, error) {
	switch AccountType(s) {
	case AccountCash:
		return AccountCash, nil
	case AccountInvestment:
		return AccountInvestment, nil
	case AccountProperty:
		return AccountProperty, nil
	case AccountLoan:
		return AccountLoan, nil
	case AccountCreditCard:
		return AccountCreditCard, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownAccountType, s)
	}
}

// AccountTypes returns every type, in the order the breakdown chart should
// draw them: assets first, then debts. One list, so the HTTP layer and the
// frontend cannot disagree about what exists.
func AccountTypes() []AccountType {
	return []AccountType{
		AccountCash, AccountInvestment, AccountProperty,
		AccountLoan, AccountCreditCard,
	}
}

// IsLiability reports whether this type is money owed rather than money held.
func (t AccountType) IsLiability() bool {
	return t == AccountLoan || t == AccountCreditCard
}

// SignedNetWorthAmount returns the amount this account contributes to net
// worth: the balance as given for an asset, negated for a liability. A
// liability's stored amount is the non-negative sum owed (the database's
// liabilities_are_not_negative constraint enforces the same rule), so the
// minus sign is produced here and never typed by a person -- which is what
// makes "typed 14500 for a car loan and net worth counted it as an asset"
// unrepresentable rather than merely unlikely.
//
// It returns an error rather than negating blindly: negating math.MinInt64 in
// two's complement returns math.MinInt64 itself, so a naive negation would
// turn the largest possible debt into the largest possible asset. Money.String
// guards the same edge for the same reason.
func (t AccountType) SignedNetWorthAmount(m Money) (Money, error) {
	if !t.IsLiability() {
		return m, nil
	}
	if m.Amount == math.MinInt64 {
		return Money{}, fmt.Errorf("%w: cannot negate %d", ErrAmountOverflow, m.Amount)
	}
	return Money{Amount: -m.Amount, Currency: m.Currency}, nil
}

// Account is one thing a household owns or owes.
//
// OwnerMembershipID follows the same "" <-> SQL NULL convention documented on
// usecase.StoredUser.PasswordHash: "" means the account is shared by the whole
// household, and the column is NULL. There is deliberately no separate
// is_shared flag -- one would allow a row that both names an owner and claims
// to be shared, with nothing to resolve it.
//
// OpeningBalanceAsOf is load-bearing, not decoration. Once transactions exist,
// only those dated after it count toward the derived balance; without it,
// importing last month's transactions would subtract them from a balance that
// already reflected them.
type Account struct {
	ID                      string
	HouseholdID             string
	Nickname                string
	Type                    AccountType
	OwnerMembershipID       string
	OpeningBalance          Money
	OpeningBalanceAsOf      time.Time
	CountTowardNetWorth     bool
	VisibleToLimitedMembers bool
	ArchivedAt              *time.Time
}

// IsArchived reports whether this account has been archived. Archived accounts
// leave the list, net worth and the breakdown; they are never deleted, because
// transactions will reference them.
func (a Account) IsArchived() bool { return a.ArchivedAt != nil }
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd api && go test ./internal/domain/ -run 'TestParseAccountType|TestIsLiability|TestSignedNetWorth' -v
```
Expected: PASS, five tests.

- [ ] **Step 6: Mutation-check the fail-closed rule**

Temporarily change `ParseAccountType`'s `default` branch to `return AccountCash, nil`. Run the tests. `TestParseAccountTypeRefusesAnythingElse` must fail. Restore the `default`.

- [ ] **Step 7: Run the arch lint**

```bash
make lint-arch
```
Expected: PASS. `account.go` imports `fmt`, `math` and `time` — all stdlib.

- [ ] **Step 8: Commit**

```bash
git add api/internal/domain/account.go api/internal/domain/account_test.go api/internal/domain/errors.go
git commit -m "feat(domain): account types, and the rule that gives a debt its sign

A liability stores the sum owed as a positive number and takes its minus sign
from its type, so a person typing 14500 for a car loan cannot make the
household appear richer. SignedNetWorthAmount refuses math.MinInt64 rather
than negating it into itself."
```

---

### Task 34: The `accounts` migration and the sqlc queries

**Files:**
- Create: `api/migrations/00004_accounts.sql`
- Create: `api/internal/adapter/postgres/queries/account.sql`
- Modify: `api/internal/adapter/postgres/schema_test.go`

**Interfaces:**
- Consumes: `households(id)`, `memberships(id)` from `00002_identity.sql`.
- Produces: the `accounts` table; sqlc-generated `ListAccounts`, `ListAccountsIncludingArchived`, `GetAccount`, `CreateAccount`, `UpdateAccount`, `SetAccountArchived`, `MembershipBelongsToHousehold` in `sqlcgen`.

- [ ] **Step 1: Write the migration**

Create `api/migrations/00004_accounts.sql`:

```sql
-- +goose Up

-- accounts is what a household owns and owes. One row per account; the
-- balance is an opening figure plus, from the next slice, every transaction
-- dated after opening_balance_as_of.
--
-- There is deliberately no updated_at. No other table in this schema has one,
-- nothing in the application would maintain it, and a column named "last
-- updated" that never changes is a lie the next reader will believe. The
-- question it would answer for an account -- when was this balance last true
-- -- is answered better by opening_balance_as_of.
CREATE TABLE accounts (
    id                         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id               uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    nickname                   text        NOT NULL,
    type                       text        NOT NULL
                                           CHECK (type IN ('cash', 'investment', 'property',
                                                           'loan', 'credit_card')),
    -- NULL means shared by the whole household. ON DELETE SET NULL is what
    -- makes a removed member's accounts fall back to shared with no
    -- application code running: refusing the removal instead would mean an
    -- owner cannot clean up a departed member without first reassigning every
    -- account they own, and deleting the accounts would destroy financial
    -- history that transactions will soon depend on.
    --
    -- It references memberships rather than users because an account belongs
    -- to someone *in this household*; a user reference would let an account
    -- name somebody who is not a member.
    owner_membership_id        uuid        REFERENCES memberships(id) ON DELETE SET NULL,
    opening_balance_minor      bigint      NOT NULL,
    -- The currency is stored per account, not inherited from the household. A
    -- household's primary currency can change in Settings; this balance was
    -- denominated in whatever it was denominated in, and restating it would
    -- silently rewrite history.
    opening_balance_currency   char(3)     NOT NULL,
    opening_balance_as_of      date        NOT NULL,
    count_toward_net_worth     boolean     NOT NULL DEFAULT true,
    visible_to_limited_members boolean     NOT NULL DEFAULT false,
    archived_at                timestamptz,
    created_at                 timestamptz NOT NULL DEFAULT now(),
    -- The second line of defence for domain.AccountType.SignedNetWorthAmount:
    -- a debt's amount is the sum owed, as a positive number, and the minus
    -- sign is derived from the type. Worth enforcing twice because the failure
    -- it prevents -- a debt counted as an asset -- is silent and wrong in the
    -- flattering direction.
    CONSTRAINT liabilities_are_not_negative CHECK (
        type NOT IN ('loan', 'credit_card') OR opening_balance_minor >= 0
    )
);

-- Matches the query the accounts list actually runs: live accounts for one
-- household. Archived accounts are read rarely and can use a sequential scan.
CREATE INDEX accounts_household_idx ON accounts (household_id) WHERE archived_at IS NULL;

-- +goose Down
DROP TABLE accounts;
```

- [ ] **Step 2: Write the queries**

Create `api/internal/adapter/postgres/queries/account.sql`:

```sql
-- ListAccounts and ListAccountsIncludingArchived are two queries rather than
-- one with a boolean parameter, because the live-only form is what the
-- partial index accounts_household_idx covers and a `WHERE archived_at IS
-- NULL OR $2` predicate would not use it.
--
-- The LEFT JOIN is what makes a shared account (owner_membership_id IS NULL)
-- come back as a row with a NULL owner name rather than vanishing.

-- name: ListAccounts :many
SELECT a.*, u.display_name AS owner_name
FROM accounts a
LEFT JOIN memberships m ON m.id = a.owner_membership_id
LEFT JOIN users u ON u.id = m.user_id
WHERE a.household_id = $1 AND a.archived_at IS NULL
ORDER BY a.created_at;

-- name: ListAccountsIncludingArchived :many
SELECT a.*, u.display_name AS owner_name
FROM accounts a
LEFT JOIN memberships m ON m.id = a.owner_membership_id
LEFT JOIN users u ON u.id = m.user_id
WHERE a.household_id = $1
ORDER BY a.archived_at NULLS FIRST, a.created_at;

-- GetAccount is scoped by household_id as well as id. Every account query in
-- this file is: an id alone would let a caller in one household read a row in
-- another by guessing a uuid, and the HTTP layer's session gives us the
-- household for free.
-- name: GetAccount :one
SELECT a.*, u.display_name AS owner_name
FROM accounts a
LEFT JOIN memberships m ON m.id = a.owner_membership_id
LEFT JOIN users u ON u.id = m.user_id
WHERE a.household_id = $1 AND a.id = $2;

-- name: CreateAccount :one
INSERT INTO accounts (
    household_id, nickname, type, owner_membership_id,
    opening_balance_minor, opening_balance_currency, opening_balance_as_of,
    count_toward_net_worth, visible_to_limited_members
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpdateAccount :one
UPDATE accounts
SET nickname                   = $3,
    type                       = $4,
    owner_membership_id        = $5,
    opening_balance_minor      = $6,
    opening_balance_currency   = $7,
    opening_balance_as_of      = $8,
    count_toward_net_worth     = $9,
    visible_to_limited_members = $10
WHERE household_id = $1 AND id = $2
RETURNING *;

-- SetAccountArchived stamps or clears archived_at. There is no DELETE query in
-- this file, deliberately: transactions will reference these rows next slice,
-- and destroying an account would take its history with it.
-- name: SetAccountArchived :one
UPDATE accounts
SET archived_at = $3
WHERE household_id = $1 AND id = $2
RETURNING *;

-- MembershipBelongsToHousehold answers whether a membership is in this
-- household, so an account can never be assigned to a member of another one.
-- name: MembershipBelongsToHousehold :one
SELECT EXISTS (
    SELECT 1 FROM memberships WHERE id = $1 AND household_id = $2
);
```

- [ ] **Step 3: Regenerate sqlc**

```bash
make sqlc
```
Expected: `api/internal/adapter/postgres/sqlcgen` gains `account.sql.go` with the seven methods.

- [ ] **Step 4: Extend the schema test**

`api/internal/adapter/postgres/schema_test.go` asserts the migrated schema matches expectations. Add an `accounts` case to whatever table list it walks, asserting the table exists and that `owner_membership_id` is nullable while `household_id` is not. Read the file first and follow its existing shape rather than inventing a new one.

Add this test to the same file:

```go
// TestAccountsRefusesANegativeLiability proves the CHECK constraint is real,
// not just documentation. The application enforces the same rule in
// AccountService, but this is the layer that holds when something writes
// around it.
func TestAccountsRefusesANegativeLiability(t *testing.T) {
	db := openTestDB(t) // follow the existing helper's name in this file
	ctx := context.Background()

	householdID := insertTestHousehold(t, db) // ditto

	_, err := db.Pool().Exec(ctx, `
		INSERT INTO accounts (household_id, nickname, type,
		                      opening_balance_minor, opening_balance_currency,
		                      opening_balance_as_of)
		VALUES ($1, 'Car loan', 'loan', -1450000, 'SGD', current_date)`,
		householdID)
	if err == nil {
		t.Fatal("insert succeeded; the liabilities_are_not_negative constraint is missing")
	}
	if !strings.Contains(err.Error(), "liabilities_are_not_negative") {
		t.Fatalf("err = %v, want a liabilities_are_not_negative violation", err)
	}
}
```

- [ ] **Step 5: Run the migration and the schema test**

```bash
make migrate
cd api && go test ./internal/adapter/postgres/ -run TestAccounts -v
```
Expected: PASS.

- [ ] **Step 6: Verify the down migration**

```bash
make migrate-down && make migrate
```
Expected: both succeed. A down migration that fails leaves the next developer unable to step backwards.

- [ ] **Step 7: Commit**

```bash
git add api/migrations/00004_accounts.sql api/internal/adapter/postgres/queries/account.sql api/internal/adapter/postgres/sqlcgen api/internal/adapter/postgres/schema_test.go
git commit -m "feat(db): the accounts table

Owner is a nullable membership reference where NULL means shared, so a removed
member's accounts fall back to shared through ON DELETE SET NULL rather than
through application code. A CHECK constraint refuses a negative liability,
mirroring the domain rule, because a debt counted as an asset fails silently
and in the flattering direction."
```

---

### Task 35: Ports, `AccountService` validation, and the in-memory double

**Files:**
- Modify: `api/internal/usecase/ports.go`
- Create: `api/internal/usecase/account.go`
- Create: `api/internal/usecase/account_test.go`
- Modify: `api/internal/usecase/testdouble_test.go`

**Interfaces:**
- Consumes: `domain.Account`, `domain.ParseAccountType`, `domain.ParseSelectableCurrency`, `usecase.Clock`, `usecase.HouseholdRepository` (all existing after Task 33).
- Produces: `usecase.AccountView`, `usecase.AccountRepository`, `usecase.NewAccount`, `usecase.AccountUpdate`, `usecase.AccountDeps`, `usecase.AccountService` with `List`, `Get`, `Create`, `Update`, `SetArchived`.

- [ ] **Step 1: Add the port to `ports.go`**

Append to `api/internal/usecase/ports.go`:

```go
// AccountView is an account joined to its owner's display name, which is what
// every consumer of the accounts list actually wants -- the same shape and the
// same reason as MemberView above.
//
// Balance is the account's current balance: its opening balance plus every
// transaction dated after Account.OpeningBalanceAsOf. There is no transactions
// table yet, so today Balance always equals Account.OpeningBalance. It is a
// separate field from the start so the next slice adds a join rather than
// changing this struct's shape under its consumers.
//
// OwnerName is "" for a shared account, following the same "" <-> SQL NULL
// convention as domain.Account.OwnerMembershipID.
type AccountView struct {
	Account   domain.Account
	OwnerName string
	Balance   domain.Money
}

type AccountRepository interface {
	// List returns one household's accounts, ordered oldest first. Archived
	// accounts are included only when includeArchived is true, and never
	// contribute to any total regardless.
	List(ctx context.Context, householdID string, includeArchived bool) ([]AccountView, error)
	// Get reports domain.ErrNotFound when no account with this id exists in
	// this household -- including when one exists in a different household,
	// which must be indistinguishable from not existing at all.
	Get(ctx context.Context, householdID, accountID string) (AccountView, error)
	// Create writes a.OwnerMembershipID following the "" <-> SQL NULL
	// convention: "" stores NULL, meaning shared. a.ID and a.ArchivedAt are
	// ignored -- the database assigns the first and a new account is never
	// born archived.
	Create(ctx context.Context, a domain.Account) (domain.Account, error)
	// Update replaces every mutable column. AccountService is what turns a
	// partial PATCH into a complete Account; this port never merges.
	Update(ctx context.Context, a domain.Account) (domain.Account, error)
	// SetArchived stamps archived_at with at, or clears it when archived is
	// false. Accounts are never deleted: transactions will reference these
	// rows, and destroying an account would take its history with it.
	SetArchived(ctx context.Context, householdID, accountID string, archived bool, at time.Time) (domain.Account, error)
	// MembershipBelongsToHousehold answers whether a membership is in this
	// household, so an account can never be assigned to a member of another
	// one. It lives here rather than on MembershipRepository because that port
	// is already consumed by sign-in and does not need widening for this.
	MembershipBelongsToHousehold(ctx context.Context, householdID, membershipID string) (bool, error)
}
```

- [ ] **Step 2: Write the failing validation tests**

Create `api/internal/usecase/account_test.go`:

```go
package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// fixedNow is the clock every test in this file runs against, so "in the
// future" is a fact about the input rather than about when the suite ran.
var fixedNow = time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)

func newAccountService(t *testing.T) (*usecase.AccountService, *fakeAccountRepo) {
	t.Helper()
	repo := newFakeAccountRepo()
	repo.memberships["m-1"] = "h-1"
	svc := usecase.NewAccountService(usecase.AccountDeps{
		Accounts:   repo,
		Households: newFakeHouseholdRepo(t),
		FX:         staticTestRates{},
		Clock:      fixedClock{now: fixedNow},
	})
	return svc, repo
}

func validNewAccount() usecase.NewAccount {
	return usecase.NewAccount{
		HouseholdID:            "h-1",
		Nickname:               "DBS Everyday",
		Type:                   "cash",
		OpeningBalanceMinor:    824_055,
		OpeningBalanceCurrency: "SGD",
		OpeningBalanceAsOf:     fixedNow.AddDate(0, 0, -2),
		CountTowardNetWorth:    true,
	}
}

func TestCreateRefusesABlankNickname(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.Nickname = "   "

	_, err := svc.Create(context.Background(), in)
	if !errors.Is(err, domain.ErrAccountNicknameRequired) {
		t.Fatalf("err = %v, want ErrAccountNicknameRequired", err)
	}
}

func TestCreateTrimsTheNickname(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.Nickname = "  DBS Everyday  "

	got, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Nickname != "DBS Everyday" {
		t.Errorf("Nickname = %q, want %q", got.Nickname, "DBS Everyday")
	}
}

func TestCreateRefusesAnUnknownType(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.Type = "crypto"

	_, err := svc.Create(context.Background(), in)
	if !errors.Is(err, domain.ErrUnknownAccountType) {
		t.Fatalf("err = %v, want ErrUnknownAccountType", err)
	}
}

// TestCreateRefusesACurrencyTheMoneyPathRendersWrong covers JPY: it is a real
// ISO 4217 code, so ParseCurrency would accept it, but Money.String hard-codes
// two decimal places and would render every JPY amount a hundred times too
// small. The same gate a household's primary currency goes through.
func TestCreateRefusesACurrencyTheMoneyPathRendersWrong(t *testing.T) {
	svc, _ := newAccountService(t)
	for _, code := range []string{"ZZZ", "JPY", "KWD"} {
		in := validNewAccount()
		in.OpeningBalanceCurrency = code

		if _, err := svc.Create(context.Background(), in); !errors.Is(err, domain.ErrInvalidMoney) {
			t.Errorf("%s: err = %v, want ErrInvalidMoney", code, err)
		}
	}
}

func TestCreateRefusesAFutureOpeningBalanceDate(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.OpeningBalanceAsOf = fixedNow.AddDate(0, 0, 1)

	_, err := svc.Create(context.Background(), in)
	if !errors.Is(err, domain.ErrOpeningBalanceInFuture) {
		t.Fatalf("err = %v, want ErrOpeningBalanceInFuture", err)
	}
}

func TestCreateRefusesANegativeDebt(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.Type = "loan"
	in.OpeningBalanceMinor = -1_450_000

	_, err := svc.Create(context.Background(), in)
	if !errors.Is(err, domain.ErrLiabilityBalanceNegative) {
		t.Fatalf("err = %v, want ErrLiabilityBalanceNegative", err)
	}
}

// TestCreateAllowsANegativeAsset is the other half of the rule: an overdrawn
// current account is an ordinary thing, and only debts are constrained.
func TestCreateAllowsANegativeAsset(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.OpeningBalanceMinor = -12_000

	if _, err := svc.Create(context.Background(), in); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

// TestCreateRefusesAnOwnerFromAnotherHousehold is the check that only shows up
// once there are two households in the database -- which, since self-serve
// sign-up shipped, is every deployment.
func TestCreateRefusesAnOwnerFromAnotherHousehold(t *testing.T) {
	svc, repo := newAccountService(t)
	repo.memberships["m-other"] = "h-2"

	in := validNewAccount()
	in.OwnerMembershipID = "m-other"

	_, err := svc.Create(context.Background(), in)
	if !errors.Is(err, domain.ErrAccountOwnerNotInHousehold) {
		t.Fatalf("err = %v, want ErrAccountOwnerNotInHousehold", err)
	}
}

func TestCreateAcceptsAnEmptyOwnerAsShared(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.OwnerMembershipID = ""

	got, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.OwnerMembershipID != "" {
		t.Errorf("OwnerMembershipID = %q, want \"\" (shared)", got.OwnerMembershipID)
	}
}

// TestUpdateIsARealPatch mirrors TestUpdateHouseholdIsARealPatch: a field the
// caller did not name keeps its stored value rather than being reset to a
// zero.
func TestUpdateIsARealPatch(t *testing.T) {
	svc, _ := newAccountService(t)
	created, err := svc.Create(context.Background(), validNewAccount())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	nickname := "DBS Salary"
	got, err := svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{
		Nickname: &nickname,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Nickname != "DBS Salary" {
		t.Errorf("Nickname = %q, want %q", got.Nickname, "DBS Salary")
	}
	if got.Type != domain.AccountCash {
		t.Errorf("Type = %q, want cash -- an unnamed field was reset", got.Type)
	}
	if got.OpeningBalance.Amount != 824_055 {
		t.Errorf("OpeningBalance = %d, want 824055 -- an unnamed field was reset", got.OpeningBalance.Amount)
	}
}

// TestUpdateCanClearTheOwnerToShared documents the one subtlety in the patch
// shape: a nil pointer means "leave it alone", and a pointer to "" means "make
// this shared". Without the second, an account assigned to the wrong person
// could never be un-assigned.
func TestUpdateCanClearTheOwnerToShared(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.OwnerMembershipID = "m-1"
	created, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	shared := ""
	got, err := svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{
		OwnerMembershipID: &shared,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.OwnerMembershipID != "" {
		t.Errorf("OwnerMembershipID = %q, want \"\"", got.OwnerMembershipID)
	}
}

// TestUpdateRefusesANegativeBalanceWhenTheTypeBecomesADebt is the case a
// per-field patch makes reachable: neither change is invalid alone, and the
// pair is. Validation therefore runs against the merged account, never against
// the incoming fields.
func TestUpdateRefusesANegativeBalanceWhenTheTypeBecomesADebt(t *testing.T) {
	svc, _ := newAccountService(t)
	in := validNewAccount()
	in.OpeningBalanceMinor = -12_000 // legal for cash
	created, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	loan := "loan"
	_, err = svc.Update(context.Background(), "h-1", created.ID, usecase.AccountUpdate{Type: &loan})
	if !errors.Is(err, domain.ErrLiabilityBalanceNegative) {
		t.Fatalf("err = %v, want ErrLiabilityBalanceNegative", err)
	}
}

func TestListExcludesArchivedAccountsByDefault(t *testing.T) {
	svc, _ := newAccountService(t)
	created, err := svc.Create(context.Background(), validNewAccount())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.SetArchived(context.Background(), "h-1", created.ID, true); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	live, err := svc.List(context.Background(), "h-1", false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(live) != 0 {
		t.Errorf("live accounts = %d, want 0", len(live))
	}

	all, err := svc.List(context.Background(), "h-1", true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("all accounts = %d, want 1", len(all))
	}
}
```

- [ ] **Step 3: Add the in-memory double**

Append to `api/internal/usecase/testdouble_test.go`. Follow the file's existing naming; `fixedClock` and `newFakeHouseholdRepo` may already exist there — reuse them rather than redefining.

```go
// fakeAccountRepo is the in-memory AccountRepository every AccountService test
// runs against. memberships maps a membership id to the household it belongs
// to, which is all MembershipBelongsToHousehold needs to answer.
type fakeAccountRepo struct {
	accounts    map[string]domain.Account
	memberships map[string]string
	nextID      int
}

func newFakeAccountRepo() *fakeAccountRepo {
	return &fakeAccountRepo{
		accounts:    map[string]domain.Account{},
		memberships: map[string]string{},
	}
}

func (r *fakeAccountRepo) List(_ context.Context, householdID string, includeArchived bool) ([]usecase.AccountView, error) {
	var out []usecase.AccountView
	for _, a := range r.accounts {
		if a.HouseholdID != householdID {
			continue
		}
		if a.IsArchived() && !includeArchived {
			continue
		}
		out = append(out, usecase.AccountView{
			Account: a,
			Balance: a.OpeningBalance,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Account.ID < out[j].Account.ID })
	return out, nil
}

func (r *fakeAccountRepo) Get(_ context.Context, householdID, accountID string) (usecase.AccountView, error) {
	a, ok := r.accounts[accountID]
	if !ok || a.HouseholdID != householdID {
		return usecase.AccountView{}, domain.ErrNotFound
	}
	return usecase.AccountView{Account: a, Balance: a.OpeningBalance}, nil
}

func (r *fakeAccountRepo) Create(_ context.Context, a domain.Account) (domain.Account, error) {
	r.nextID++
	a.ID = fmt.Sprintf("acct-%d", r.nextID)
	a.ArchivedAt = nil
	r.accounts[a.ID] = a
	return a, nil
}

func (r *fakeAccountRepo) Update(_ context.Context, a domain.Account) (domain.Account, error) {
	existing, ok := r.accounts[a.ID]
	if !ok || existing.HouseholdID != a.HouseholdID {
		return domain.Account{}, domain.ErrNotFound
	}
	a.ArchivedAt = existing.ArchivedAt
	r.accounts[a.ID] = a
	return a, nil
}

func (r *fakeAccountRepo) SetArchived(_ context.Context, householdID, accountID string, archived bool, at time.Time) (domain.Account, error) {
	a, ok := r.accounts[accountID]
	if !ok || a.HouseholdID != householdID {
		return domain.Account{}, domain.ErrNotFound
	}
	if archived {
		stamp := at
		a.ArchivedAt = &stamp
	} else {
		a.ArchivedAt = nil
	}
	r.accounts[accountID] = a
	return a, nil
}

func (r *fakeAccountRepo) MembershipBelongsToHousehold(_ context.Context, householdID, membershipID string) (bool, error) {
	return r.memberships[membershipID] == householdID, nil
}

// staticTestRates knows the one pair fx.StaticProvider knows, and errors on
// everything else -- so a test for the no-rate branch does not have to invent
// a second, differently-behaved double.
type staticTestRates struct{}

func (staticTestRates) Rate(_ context.Context, from, to string) (usecase.Rate, error) {
	switch {
	case from == to:
		return usecase.Rate{Numerator: 1, Denominator: 1}, nil
	case from == "SGD" && to == "IDR":
		return usecase.Rate{Numerator: 12_410, Denominator: 1}, nil
	case from == "IDR" && to == "SGD":
		return usecase.Rate{Numerator: 1, Denominator: 12_410}, nil
	default:
		return usecase.Rate{}, fmt.Errorf("no rate available for %s to %s", from, to)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

```bash
cd api && go test ./internal/usecase/ -run TestCreate -v
```
Expected: FAIL — `undefined: usecase.NewAccountService`.

- [ ] **Step 5: Write `api/internal/usecase/account.go`**

```go
package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// NewAccount is the create input. It is a struct rather than nine parameters
// because five of them are the same two types and a caller swapping two would
// compile.
//
// OwnerMembershipID follows the "" <-> SQL NULL convention: "" means shared.
type NewAccount struct {
	HouseholdID             string
	Nickname                string
	Type                    string
	OwnerMembershipID       string
	OpeningBalanceMinor     int64
	OpeningBalanceCurrency  string
	OpeningBalanceAsOf      time.Time
	CountTowardNetWorth     bool
	VisibleToLimitedMembers bool
}

// AccountUpdate is a real patch: a nil pointer means "leave this field alone".
//
// OwnerMembershipID is a *string rather than a **string, and the two states a
// caller needs are both reachable: nil leaves the owner unchanged, and a
// pointer to "" makes the account shared. Without the second, an account
// assigned to the wrong person could never be un-assigned.
type AccountUpdate struct {
	Nickname                *string
	Type                    *string
	OwnerMembershipID       *string
	OpeningBalanceMinor     *int64
	OpeningBalanceCurrency  *string
	OpeningBalanceAsOf      *time.Time
	CountTowardNetWorth     *bool
	VisibleToLimitedMembers *bool
}

// AccountDeps mirrors HouseholdDeps: every port AccountService needs, gathered
// into one named argument.
type AccountDeps struct {
	Accounts   AccountRepository
	Households HouseholdRepository
	FX         FXRateProvider
	Clock      Clock
}

// AccountService covers the Finances screen: the accounts themselves and the
// net worth summary computed from them.
//
// It takes no actor parameter, by the rule this whole codebase follows:
// services enforce what is *valid*, middleware enforces who is *asking*. The
// money capability and the owner check live in the router.
type AccountService struct {
	d AccountDeps
}

func NewAccountService(d AccountDeps) *AccountService {
	return &AccountService{d: d}
}

func (s *AccountService) List(ctx context.Context, householdID string, includeArchived bool) ([]AccountView, error) {
	return s.d.Accounts.List(ctx, householdID, includeArchived)
}

func (s *AccountService) Get(ctx context.Context, householdID, accountID string) (AccountView, error) {
	return s.d.Accounts.Get(ctx, householdID, accountID)
}

func (s *AccountService) Create(ctx context.Context, in NewAccount) (domain.Account, error) {
	account := domain.Account{
		HouseholdID:             in.HouseholdID,
		Nickname:                in.Nickname,
		OwnerMembershipID:       in.OwnerMembershipID,
		OpeningBalanceAsOf:      in.OpeningBalanceAsOf,
		CountTowardNetWorth:     in.CountTowardNetWorth,
		VisibleToLimitedMembers: in.VisibleToLimitedMembers,
		OpeningBalance:          domain.Money{Amount: in.OpeningBalanceMinor, Currency: in.OpeningBalanceCurrency},
		Type:                    domain.AccountType(in.Type),
	}
	if err := s.validate(ctx, &account); err != nil {
		return domain.Account{}, err
	}
	return s.d.Accounts.Create(ctx, account)
}

// Update merges the patch onto the stored account and then validates the
// *result*, never the incoming fields. That ordering is the point: switching a
// type to "loan" and leaving a negative balance alone are each legal in
// isolation and illegal together, so validating the patch would let the pair
// through.
func (s *AccountService) Update(ctx context.Context, householdID, accountID string, patch AccountUpdate) (domain.Account, error) {
	view, err := s.d.Accounts.Get(ctx, householdID, accountID)
	if err != nil {
		return domain.Account{}, err
	}
	account := view.Account

	if patch.Nickname != nil {
		account.Nickname = *patch.Nickname
	}
	if patch.Type != nil {
		account.Type = domain.AccountType(*patch.Type)
	}
	if patch.OwnerMembershipID != nil {
		account.OwnerMembershipID = *patch.OwnerMembershipID
	}
	if patch.OpeningBalanceMinor != nil {
		account.OpeningBalance.Amount = *patch.OpeningBalanceMinor
	}
	if patch.OpeningBalanceCurrency != nil {
		account.OpeningBalance.Currency = *patch.OpeningBalanceCurrency
	}
	if patch.OpeningBalanceAsOf != nil {
		account.OpeningBalanceAsOf = *patch.OpeningBalanceAsOf
	}
	if patch.CountTowardNetWorth != nil {
		account.CountTowardNetWorth = *patch.CountTowardNetWorth
	}
	if patch.VisibleToLimitedMembers != nil {
		account.VisibleToLimitedMembers = *patch.VisibleToLimitedMembers
	}

	if err := s.validate(ctx, &account); err != nil {
		return domain.Account{}, err
	}
	return s.d.Accounts.Update(ctx, account)
}

func (s *AccountService) SetArchived(ctx context.Context, householdID, accountID string, archived bool) (domain.Account, error) {
	return s.d.Accounts.SetArchived(ctx, householdID, accountID, archived, s.d.Clock.Now())
}

// validate normalises and checks an assembled account in place. It is shared by
// Create and Update so the two cannot drift -- the class of defect this project
// has hit four times is a rule fixed at one call site while its sibling kept
// the bug.
func (s *AccountService) validate(ctx context.Context, a *domain.Account) error {
	a.Nickname = strings.TrimSpace(a.Nickname)
	if a.Nickname == "" {
		return domain.ErrAccountNicknameRequired
	}

	accountType, err := domain.ParseAccountType(string(a.Type))
	if err != nil {
		return err
	}
	a.Type = accountType

	// ParseSelectableCurrency, not ParseCurrency: Money.String hard-codes two
	// decimal places, so a JPY or KWD account would render every amount
	// wrong. The identical gate a household's primary currency goes through.
	code, err := domain.ParseSelectableCurrency(a.OpeningBalance.Currency)
	if err != nil {
		return err
	}
	a.OpeningBalance.Currency = code

	// Compared by calendar day, not by instant: opening_balance_as_of is a
	// date column, and a balance stated as true "today" must not be refused
	// because the request arrived at 09:00 and Clock.Now() reads 08:59 in
	// another zone.
	now := s.d.Clock.Now().UTC()
	asOf := a.OpeningBalanceAsOf.UTC()
	if asOf.Truncate(24 * time.Hour).After(now.Truncate(24 * time.Hour)) {
		return domain.ErrOpeningBalanceInFuture
	}

	if a.Type.IsLiability() && a.OpeningBalance.Amount < 0 {
		return domain.ErrLiabilityBalanceNegative
	}

	if a.OwnerMembershipID != "" {
		ok, err := s.d.Accounts.MembershipBelongsToHousehold(ctx, a.HouseholdID, a.OwnerMembershipID)
		if err != nil {
			return err
		}
		if !ok {
			return domain.ErrAccountOwnerNotInHousehold
		}
	}
	return nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

```bash
cd api && go test ./internal/usecase/ -run 'TestCreate|TestUpdate|TestList' -v
```
Expected: PASS, thirteen tests.

- [ ] **Step 7: Mutation-check the merge-then-validate ordering**

Temporarily move the `validate` call in `Update` to run against a `domain.Account` built only from the non-nil patch fields. `TestUpdateRefusesANegativeBalanceWhenTheTypeBecomesADebt` must fail. Restore.

- [ ] **Step 8: Commit**

```bash
git add api/internal/usecase/ports.go api/internal/usecase/account.go api/internal/usecase/account_test.go api/internal/usecase/testdouble_test.go
git commit -m "feat(usecase): AccountService and its validation

Update merges the patch onto the stored account and validates the result, not
the incoming fields: switching a type to loan and leaving a negative balance
are each legal alone and illegal together, so validating the patch would let
the pair through.

Currency goes through ParseSelectableCurrency rather than ParseCurrency, so a
JPY account is refused for the same reason a JPY household is -- Money.String
hard-codes two decimal places."
```

---

### Task 36: `NetWorthSummary` — convert, then add

**Files:**
- Create: `api/internal/usecase/networth.go`
- Create: `api/internal/usecase/networth_test.go`

**Interfaces:**
- Consumes: `usecase.AccountView`, `usecase.Rate`, `usecase.FXRateProvider`, `usecase.HouseholdRepository`, `domain.AccountType.SignedNetWorthAmount` (all from Tasks 33 and 35).
- Produces: `usecase.NetWorthSummary`, `usecase.BreakdownEntry`, `usecase.ExcludedAccount`, and `(*AccountService).Summary(ctx, householdID string, views []AccountView) (NetWorthSummary, error)`.

- [ ] **Step 1: Write the failing tests**

Create `api/internal/usecase/networth_test.go`:

```go
package usecase_test

import (
	"context"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// account is a terse builder for the views Summary consumes, so each test
// below reads as the scenario it describes rather than as struct literals.
func account(t *testing.T, kind domain.AccountType, minor int64, currency string, opts ...func(*usecase.AccountView)) usecase.AccountView {
	t.Helper()
	money, err := domain.NewMoney(minor, currency)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	v := usecase.AccountView{
		Account: domain.Account{
			ID: currency + "-" + string(kind), HouseholdID: "h-1", Type: kind,
			OpeningBalance: money, CountTowardNetWorth: true,
		},
		Balance: money,
	}
	for _, opt := range opts {
		opt(&v)
	}
	return v
}

func notCounted(v *usecase.AccountView) { v.Account.CountTowardNetWorth = false }

// TestSummarySubtractsDebtsFromAssets is the design's own Finances figures in
// miniature: assets minus liabilities, in the household's primary currency.
func TestSummarySubtractsDebtsFromAssets(t *testing.T) {
	svc, _ := newAccountService(t) // household h-1 has primary SGD

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCash, 6_199_000, "SGD"),
		account(t, domain.AccountLoan, 1_450_000, "SGD"),
	})
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !got.Computable {
		t.Fatal("Computable = false, want true")
	}
	if got.NetWorth.Amount != 4_749_000 {
		t.Errorf("NetWorth = %d, want 4749000", got.NetWorth.Amount)
	}
	if got.Assets.Amount != 6_199_000 {
		t.Errorf("Assets = %d, want 6199000", got.Assets.Amount)
	}
	if got.Liabilities.Amount != 1_450_000 {
		t.Errorf("Liabilities = %d, want 1450000 (the sum owed, unsigned)", got.Liabilities.Amount)
	}
}

// TestSummaryConvertsBeforeAdding is the test that fails if the loop is
// written the other way round. domain.Money.Add refuses two currencies, so
// summing first and converting after errors on the second account.
//
// The expected figure comes from the design's own screen: Rp 85,400,000 is
// 8_540_000_000 IDR minor units, which at {1, 12410} is 688_155 SGD minor
// units -- S$6,881.55, the "≈ S$6,880" the mockup rounds for display.
func TestSummaryConvertsBeforeAdding(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCash, 824_055, "SGD"),
		account(t, domain.AccountCash, 8_540_000_000, "IDR"),
	})
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.NetWorth.Currency != "SGD" {
		t.Errorf("Currency = %q, want SGD", got.NetWorth.Currency)
	}
	if got.NetWorth.Amount != 1_512_210 {
		t.Errorf("NetWorth = %d, want 1512210 (824055 + 688155)", got.NetWorth.Amount)
	}
}

func TestSummaryExcludesAndNamesAnAccountWithNoRate(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCash, 824_055, "SGD"),
		account(t, domain.AccountCash, 500_000, "USD"),
	})
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.NetWorth.Amount != 824_055 {
		t.Errorf("NetWorth = %d, want 824055 -- the USD account must not be counted", got.NetWorth.Amount)
	}
	if len(got.ExcludedNoRate) != 1 || got.ExcludedNoRate[0].Currency != "USD" {
		t.Errorf("ExcludedNoRate = %+v, want one USD entry", got.ExcludedNoRate)
	}
}

// TestSummaryIsNotComputableWhenNothingConverts is the state a household
// reaches by changing its primary currency in Settings. Zero would be a claim
// about their money; the truth is that we cannot compute it.
func TestSummaryIsNotComputableWhenNothingConverts(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCash, 500_000, "USD"),
		account(t, domain.AccountCash, 300_000, "EUR"),
	})
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Computable {
		t.Fatal("Computable = true, want false when no account converts")
	}
	if len(got.ExcludedNoRate) != 2 {
		t.Errorf("ExcludedNoRate = %d entries, want 2", len(got.ExcludedNoRate))
	}
}

// TestSummaryHasNoAccountsIsComputable distinguishes "nothing to add up, so
// zero" from "cannot add up". A household with no accounts genuinely has a net
// worth of zero.
func TestSummaryHasNoAccountsIsComputable(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", nil)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !got.Computable || got.NetWorth.Amount != 0 {
		t.Errorf("got %+v, want a computable zero", got)
	}
}

// TestSummaryKeepsAnUncountedAccountInTheBreakdown pins the consequence of the
// toggle's own copy, "Include this balance in the family total": the total,
// specifically. The bars will not always sum to net worth, and the screen says
// so rather than the service quietly hiding the account.
func TestSummaryKeepsAnUncountedAccountInTheBreakdown(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCash, 100_000, "SGD"),
		account(t, domain.AccountInvestment, 900_000, "SGD", notCounted),
	})
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.NetWorth.Amount != 100_000 {
		t.Errorf("NetWorth = %d, want 100000", got.NetWorth.Amount)
	}
	if got.ExcludedByChoice != 1 {
		t.Errorf("ExcludedByChoice = %d, want 1", got.ExcludedByChoice)
	}
	if len(got.Breakdown) != 2 {
		t.Fatalf("Breakdown = %d entries, want 2 -- an uncounted account still has a bar", len(got.Breakdown))
	}
}

// TestSummaryBreakdownDrawsOnlyPopulatedTypes: the chart is one bar per type
// that has an account, not a fixed five, so a household with two cash accounts
// does not get three empty bars.
func TestSummaryBreakdownDrawsOnlyPopulatedTypes(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCash, 100_000, "SGD"),
		account(t, domain.AccountCash, 200_000, "SGD"),
	})
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if len(got.Breakdown) != 1 {
		t.Fatalf("Breakdown = %d entries, want 1", len(got.Breakdown))
	}
	if got.Breakdown[0].Type != domain.AccountCash || got.Breakdown[0].Total.Amount != 300_000 {
		t.Errorf("Breakdown[0] = %+v, want {cash 300000}", got.Breakdown[0])
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd api && go test ./internal/usecase/ -run TestSummary -v
```
Expected: FAIL — `svc.Summary undefined`.

- [ ] **Step 3: Write `api/internal/usecase/networth.go`**

```go
package usecase

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// BreakdownEntry is one bar of the assets-and-liabilities chart. Totals are
// unsigned sums of what that type holds or owes -- the chart draws debts below
// the line from Type.IsLiability(), rather than from a negative number.
type BreakdownEntry struct {
	Type  domain.AccountType
	Total domain.Money
}

// ExcludedAccount names one account that could not be converted into the
// household's primary currency, so the screen can say which and why. It is an
// explicit field rather than something the frontend infers by comparing lists:
// a limited member's response carries no amounts at all, so inference there
// would produce a wrong or empty notice.
type ExcludedAccount struct {
	AccountID string
	Currency  string
}

// NetWorthSummary is everything the Finances screen shows above the accounts
// list.
//
// Computable is false when at least one account exists and none of them could
// be converted -- the state a household reaches by changing its primary
// currency in Settings while fx.StaticProvider knows only SGD<->IDR. A zero
// must never be shown for it: zero is a claim about the household's money, and
// the truth is that we cannot compute it. A household with no accounts at all
// is computable and genuinely zero.
type NetWorthSummary struct {
	Currency         string
	NetWorth         domain.Money
	Assets           domain.Money
	Liabilities      domain.Money
	Breakdown        []BreakdownEntry
	ExcludedNoRate   []ExcludedAccount
	ExcludedByChoice int
	Computable       bool
}

// Summary composes the figures above the accounts list from views the caller
// has already listed, rather than listing again -- the handler needs both
// halves of one response and they must describe the same set of rows.
//
// The order of operations is not incidental. domain.Money.Add refuses to add
// two different currencies, deliberately, so each account is converted into
// the household's primary currency *first* and only then summed. Summing first
// and converting after fails on the second account of a mixed-currency
// household. Rounding therefore happens per account (half away from zero, as
// Rate.Apply already does) and the total is never re-rounded, so the figure is
// deterministic.
func (s *AccountService) Summary(ctx context.Context, householdID string, views []AccountView) (NetWorthSummary, error) {
	household, err := s.d.Households.Get(ctx, householdID)
	if err != nil {
		return NetWorthSummary{}, err
	}
	primary := household.PrimaryCurrency

	zero, err := domain.NewMoney(0, primary)
	if err != nil {
		return NetWorthSummary{}, err
	}

	summary := NetWorthSummary{
		Currency:    primary,
		NetWorth:    zero,
		Assets:      zero,
		Liabilities: zero,
		Computable:  true,
	}

	byType := map[domain.AccountType]domain.Money{}
	converted := 0

	for _, view := range views {
		if view.Account.IsArchived() {
			continue
		}

		inPrimary, err := s.convert(ctx, view.Balance, primary)
		if err != nil {
			summary.ExcludedNoRate = append(summary.ExcludedNoRate, ExcludedAccount{
				AccountID: view.Account.ID,
				Currency:  view.Balance.Currency,
			})
			continue
		}
		converted++

		// The breakdown covers every convertible account, counted or not: the
		// toggle's copy is "Include this balance in the family total", and the
		// total is what it governs.
		running, ok := byType[view.Account.Type]
		if !ok {
			running = zero
		}
		running, err = running.Add(inPrimary)
		if err != nil {
			return NetWorthSummary{}, err
		}
		byType[view.Account.Type] = running

		if !view.Account.CountTowardNetWorth {
			summary.ExcludedByChoice++
			continue
		}

		if view.Account.Type.IsLiability() {
			summary.Liabilities, err = summary.Liabilities.Add(inPrimary)
		} else {
			summary.Assets, err = summary.Assets.Add(inPrimary)
		}
		if err != nil {
			return NetWorthSummary{}, err
		}

		signed, err := view.Account.Type.SignedNetWorthAmount(inPrimary)
		if err != nil {
			return NetWorthSummary{}, err
		}
		summary.NetWorth, err = summary.NetWorth.Add(signed)
		if err != nil {
			return NetWorthSummary{}, err
		}
	}

	if len(views) > 0 && converted == 0 {
		summary.Computable = false
	}

	// Ordered by domain.AccountTypes rather than by map iteration, so the
	// chart's bars do not reshuffle between two identical requests.
	for _, accountType := range domain.AccountTypes() {
		if total, ok := byType[accountType]; ok {
			summary.Breakdown = append(summary.Breakdown, BreakdownEntry{Type: accountType, Total: total})
		}
	}
	return summary, nil
}

// convert turns one account's balance into the household's primary currency.
// A same-currency balance short-circuits without consulting the provider at
// all -- that is the overwhelmingly common case, it is exact, and it means a
// single-currency household never depends on a rate table it does not need.
func (s *AccountService) convert(ctx context.Context, m domain.Money, primary string) (domain.Money, error) {
	if m.Currency == primary {
		return m, nil
	}
	rate, err := s.d.FX.Rate(ctx, m.Currency, primary)
	if err != nil {
		return domain.Money{}, err
	}
	amount, err := rate.Apply(m.Amount)
	if err != nil {
		return domain.Money{}, err
	}
	return domain.Money{Amount: amount, Currency: primary}, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd api && go test ./internal/usecase/ -run TestSummary -v
```
Expected: PASS, seven tests.

- [ ] **Step 5: Mutation-check the convert-then-add ordering**

Temporarily rewrite the loop to add `view.Balance` (unconverted) into `summary.Assets` and convert `summary.NetWorth` at the end. `TestSummaryConvertsBeforeAdding` must fail with `ErrCurrencyMismatch`. Restore.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/networth.go api/internal/usecase/networth_test.go
git commit -m "feat(usecase): net worth, converting before adding

domain.Money.Add refuses two currencies, so every account is converted into the
household's primary currency before it is summed. Summing first and converting
after fails on the second account of a mixed-currency household, which is a
defect a single-currency test suite would never see.

Computable is false, rather than a zero, when accounts exist and none convert
-- the state a household reaches by changing its primary currency in Settings."
```

---

### Task 37: The Postgres account repository

**Files:**
- Create: `api/internal/adapter/postgres/account_repo.go`
- Create: `api/internal/adapter/postgres/account_repo_test.go`
- Modify: `api/internal/adapter/postgres/convert.go`

**Interfaces:**
- Consumes: `usecase.AccountRepository` (Task 35), the sqlc methods from Task 34.
- Produces: `postgres.NewAccountRepo(db *DB) *AccountRepo` satisfying `usecase.AccountRepository`.

- [ ] **Step 1: Write the failing tests**

Create `api/internal/adapter/postgres/account_repo_test.go`. Follow the existing repo tests' setup helpers in this package rather than inventing new ones — read `notification_repo_test.go` first.

```go
package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// TestSharedAccountRoundTripsAsAnEmptyOwner is the "" <-> SQL NULL convention
// at the boundary it exists for: a shared account stores NULL and must come
// back as "", never as a zero uuid or any other sentinel.
func TestSharedAccountRoundTripsAsAnEmptyOwner(t *testing.T) {
	db, householdID, _ := newAccountFixture(t)
	repo := postgres.NewAccountRepo(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, domain.Account{
		HouseholdID:        householdID,
		Nickname:           "OCBC Joint Savings",
		Type:               domain.AccountCash,
		OwnerMembershipID:  "",
		OpeningBalance:     domain.Money{Amount: 4_690_000, Currency: "SGD"},
		OpeningBalanceAsOf: time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.OwnerMembershipID != "" {
		t.Errorf("OwnerMembershipID = %q, want \"\"", created.OwnerMembershipID)
	}

	view, err := repo.Get(ctx, householdID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if view.Account.OwnerMembershipID != "" || view.OwnerName != "" {
		t.Errorf("read back %+v, want an empty owner and an empty owner name", view)
	}
}

// TestRemovingAMemberLeavesTheirAccountsShared proves ON DELETE SET NULL. This
// is database behaviour, so only a database can show it -- no service test can.
func TestRemovingAMemberLeavesTheirAccountsShared(t *testing.T) {
	db, householdID, membershipID := newAccountFixture(t)
	repo := postgres.NewAccountRepo(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, domain.Account{
		HouseholdID:        householdID,
		Nickname:           "DBS Everyday",
		Type:               domain.AccountCash,
		OwnerMembershipID:  membershipID,
		OpeningBalance:     domain.Money{Amount: 824_055, Currency: "SGD"},
		OpeningBalanceAsOf: time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := db.Pool().Exec(ctx, `DELETE FROM memberships WHERE id = $1`, membershipID); err != nil {
		t.Fatalf("delete membership: %v", err)
	}

	view, err := repo.Get(ctx, householdID, created.ID)
	if err != nil {
		t.Fatalf("Get after member removal: %v", err)
	}
	if view.Account.OwnerMembershipID != "" {
		t.Errorf("OwnerMembershipID = %q, want \"\" -- the account should have fallen back to shared",
			view.Account.OwnerMembershipID)
	}
	if view.Account.Nickname != "DBS Everyday" {
		t.Error("the account itself was deleted; removing a member must not take their accounts with it")
	}
}

// TestGetInAnotherHouseholdIsNotFound: an account in someone else's household
// must be indistinguishable from one that does not exist. A caller who can
// tell the two apart can enumerate account ids across tenants.
func TestGetInAnotherHouseholdIsNotFound(t *testing.T) {
	db, householdID, _ := newAccountFixture(t)
	repo := postgres.NewAccountRepo(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, domain.Account{
		HouseholdID:        householdID,
		Nickname:           "BCA Tahapan",
		Type:               domain.AccountCash,
		OpeningBalance:     domain.Money{Amount: 8_540_000_000, Currency: "IDR"},
		OpeningBalanceAsOf: time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	other := insertSecondHousehold(t, db)
	if _, err := repo.Get(ctx, other, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}
}

func TestSetArchivedHidesAndRestores(t *testing.T) {
	db, householdID, _ := newAccountFixture(t)
	repo := postgres.NewAccountRepo(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, domain.Account{
		HouseholdID:        householdID,
		Nickname:           "Old card",
		Type:               domain.AccountCreditCard,
		OpeningBalance:     domain.Money{Amount: 12_000, Currency: "SGD"},
		OpeningBalanceAsOf: time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	at := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)
	if _, err := repo.SetArchived(ctx, householdID, created.ID, true, at); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	live, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(live) != 0 {
		t.Errorf("live = %d accounts, want 0", len(live))
	}

	all, err := repo.List(ctx, householdID, true)
	if err != nil {
		t.Fatalf("List including archived: %v", err)
	}
	if len(all) != 1 || !all[0].Account.IsArchived() {
		t.Fatalf("all = %+v, want one archived account", all)
	}

	if _, err := repo.SetArchived(ctx, householdID, created.ID, false, at); err != nil {
		t.Fatalf("restore: %v", err)
	}
	live, err = repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("List after restore: %v", err)
	}
	if len(live) != 1 {
		t.Errorf("live after restore = %d, want 1", len(live))
	}
}

func TestMembershipBelongsToHousehold(t *testing.T) {
	db, householdID, membershipID := newAccountFixture(t)
	repo := postgres.NewAccountRepo(db)
	ctx := context.Background()

	ok, err := repo.MembershipBelongsToHousehold(ctx, householdID, membershipID)
	if err != nil || !ok {
		t.Fatalf("own membership: ok = %v, err = %v", ok, err)
	}

	other := insertSecondHousehold(t, db)
	ok, err = repo.MembershipBelongsToHousehold(ctx, other, membershipID)
	if err != nil {
		t.Fatalf("other household: %v", err)
	}
	if ok {
		t.Error("a membership resolved against the wrong household")
	}
}
```

Write `newAccountFixture(t) (*postgres.DB, householdID, membershipID string)` and `insertSecondHousehold(t, db) string` in the same file, using the package's existing container helper (`testsupport.StartPostgres`) and `postgres.Open`, mirroring how `notification_repo_test.go` builds its fixture.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestSharedAccount -v
```
Expected: FAIL — `undefined: postgres.NewAccountRepo`.

- [ ] **Step 3: Add the date converters**

Append to `api/internal/adapter/postgres/convert.go`:

```go
// dateOnly converts a domain time into the pgtype.Date that
// opening_balance_as_of is stored as. The column is a date, not a
// timestamptz, deliberately: "the balance was true on the 26th" is a calendar
// fact, and storing an instant would make it depend on the zone the request
// arrived from.
func dateOnly(t time.Time) pgtype.Date {
	return pgtype.Date{Time: t.UTC().Truncate(24 * time.Hour), Valid: true}
}

func dateToTime(d pgtype.Date) time.Time { return d.Time }
```

- [ ] **Step 4: Write `api/internal/adapter/postgres/account_repo.go`**

```go
package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type AccountRepo struct{ q *sqlcgen.Queries }

func NewAccountRepo(db *DB) *AccountRepo {
	return &AccountRepo{q: sqlcgen.New(db.Pool())}
}

func (r *AccountRepo) List(ctx context.Context, householdID string, includeArchived bool) ([]usecase.AccountView, error) {
	if includeArchived {
		rows, err := r.q.ListAccountsIncludingArchived(ctx, uuid(householdID))
		if err != nil {
			return nil, translate(err, "list accounts including archived")
		}
		out := make([]usecase.AccountView, 0, len(rows))
		for _, row := range rows {
			out = append(out, toAccountViewIncludingArchived(row))
		}
		return out, nil
	}

	rows, err := r.q.ListAccounts(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list accounts")
	}
	out := make([]usecase.AccountView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAccountView(row))
	}
	return out, nil
}

func (r *AccountRepo) Get(ctx context.Context, householdID, accountID string) (usecase.AccountView, error) {
	row, err := r.q.GetAccount(ctx, sqlcgen.GetAccountParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(accountID),
	})
	if err != nil {
		return usecase.AccountView{}, translate(err, "get account")
	}
	return toAccountViewFromGet(row), nil
}

func (r *AccountRepo) Create(ctx context.Context, a domain.Account) (domain.Account, error) {
	row, err := r.q.CreateAccount(ctx, sqlcgen.CreateAccountParams{
		HouseholdID:             uuid(a.HouseholdID),
		Nickname:                a.Nickname,
		Type:                    string(a.Type),
		OwnerMembershipID:       nullableUUID(optionalID(a.OwnerMembershipID)),
		OpeningBalanceMinor:     a.OpeningBalance.Amount,
		OpeningBalanceCurrency:  a.OpeningBalance.Currency,
		OpeningBalanceAsOf:      dateOnly(a.OpeningBalanceAsOf),
		CountTowardNetWorth:     a.CountTowardNetWorth,
		VisibleToLimitedMembers: a.VisibleToLimitedMembers,
	})
	if err != nil {
		return domain.Account{}, translate(err, "create account")
	}
	return toAccount(row), nil
}

func (r *AccountRepo) Update(ctx context.Context, a domain.Account) (domain.Account, error) {
	row, err := r.q.UpdateAccount(ctx, sqlcgen.UpdateAccountParams{
		HouseholdID:             uuid(a.HouseholdID),
		ID:                      uuid(a.ID),
		Nickname:                a.Nickname,
		Type:                    string(a.Type),
		OwnerMembershipID:       nullableUUID(optionalID(a.OwnerMembershipID)),
		OpeningBalanceMinor:     a.OpeningBalance.Amount,
		OpeningBalanceCurrency:  a.OpeningBalance.Currency,
		OpeningBalanceAsOf:      dateOnly(a.OpeningBalanceAsOf),
		CountTowardNetWorth:     a.CountTowardNetWorth,
		VisibleToLimitedMembers: a.VisibleToLimitedMembers,
	})
	if err != nil {
		return domain.Account{}, translate(err, "update account")
	}
	return toAccount(row), nil
}

func (r *AccountRepo) SetArchived(ctx context.Context, householdID, accountID string, archived bool, at time.Time) (domain.Account, error) {
	stamp := pgtype.Timestamptz{}
	if archived {
		stamp = timestamptz(at)
	}
	row, err := r.q.SetAccountArchived(ctx, sqlcgen.SetAccountArchivedParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(accountID),
		ArchivedAt:  stamp,
	})
	if err != nil {
		return domain.Account{}, translate(err, "set account archived")
	}
	return toAccount(row), nil
}

func (r *AccountRepo) MembershipBelongsToHousehold(ctx context.Context, householdID, membershipID string) (bool, error) {
	ok, err := r.q.MembershipBelongsToHousehold(ctx, sqlcgen.MembershipBelongsToHouseholdParams{
		ID:          uuid(membershipID),
		HouseholdID: uuid(householdID),
	})
	if err != nil {
		return false, translate(err, "check membership household")
	}
	return ok, nil
}

// optionalID turns the "" <-> NULL convention into the *string nullableUUID
// expects. It is the account owner's half of the same rule nullableText
// implements for users.email and users.password_hash.
func optionalID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}
```

sqlc generates a distinct row struct per query, so each list/get query needs its own thin converter feeding one shared builder. Write them in the same file:

```go
// toAccount maps the columns every account query returns into the domain
// type. The row structs sqlc generates differ per query (ListAccountsRow,
// GetAccountRow, and the plain Account for the RETURNING queries), so the
// converters below are thin adapters onto this one function -- the mapping
// itself exists once.
func toAccount(a sqlcgen.Account) domain.Account {
	return domain.Account{
		ID:                      uuidToString(a.ID),
		HouseholdID:             uuidToString(a.HouseholdID),
		Nickname:                a.Nickname,
		Type:                    domain.AccountType(a.Type),
		OwnerMembershipID:       optionalIDToString(a.OwnerMembershipID),
		OpeningBalance:          domain.Money{Amount: a.OpeningBalanceMinor, Currency: a.OpeningBalanceCurrency},
		OpeningBalanceAsOf:      dateToTime(a.OpeningBalanceAsOf),
		CountTowardNetWorth:     a.CountTowardNetWorth,
		VisibleToLimitedMembers: a.VisibleToLimitedMembers,
		ArchivedAt:              timePtrOf(a.ArchivedAt),
	}
}

// optionalIDToString is optionalID's inverse: a NULL owner_membership_id
// comes back as "", meaning shared -- never as the zero uuid, which would
// read as a real membership that happens not to exist.
func optionalIDToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuidToString(u)
}

// buildView is where AccountView.Balance is decided. It is the opening
// balance today because there is no transactions table yet; when one
// arrives, these queries grow a summed column and this is the only function
// that changes. AccountView's shape does not, and neither does any caller.
func buildView(a domain.Account, ownerName *string) usecase.AccountView {
	return usecase.AccountView{
		Account:   a,
		OwnerName: stringOrEmpty(ownerName),
		Balance:   a.OpeningBalance,
	}
}

func toAccountView(row sqlcgen.ListAccountsRow) usecase.AccountView {
	return buildView(toAccount(row.Account), row.OwnerName)
}

func toAccountViewIncludingArchived(row sqlcgen.ListAccountsIncludingArchivedRow) usecase.AccountView {
	return buildView(toAccount(row.Account), row.OwnerName)
}

func toAccountViewFromGet(row sqlcgen.GetAccountRow) usecase.AccountView {
	return buildView(toAccount(row.Account), row.OwnerName)
}
```

If sqlc flattens `a.*` into individual fields rather than embedding an `Account`, adjust the three wrappers to build a `sqlcgen.Account` from the row's fields first — `toAccount` and `buildView` stay untouched either way.

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd api && export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
go test ./internal/adapter/postgres/ -run 'TestSharedAccount|TestRemovingAMember|TestGetInAnother|TestSetArchived|TestMembershipBelongs' -v
```
Expected: PASS, five tests.

- [ ] **Step 6: Mutation-check the household scope**

Temporarily drop `AND a.household_id = $1` from `GetAccount` in `queries/account.sql`, run `make sqlc`, and re-run the tests. `TestGetInAnotherHouseholdIsNotFound` must fail. Restore the predicate and regenerate.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/postgres/account_repo.go api/internal/adapter/postgres/account_repo_test.go api/internal/adapter/postgres/convert.go
git commit -m "feat(postgres): the accounts repository

Every query is scoped by household_id as well as id, so an account in another
household is indistinguishable from one that does not exist -- a caller who
could tell them apart could enumerate account ids across tenants.

A test proves ON DELETE SET NULL leaves a removed member's accounts in place as
shared. That is database behaviour, so no service test could show it."
```

---

### Task 38: The HTTP surface — five routes, the first capability gate, and redaction

**Files:**
- Create: `api/internal/adapter/http/account_handlers.go`
- Create: `api/internal/adapter/http/middleware_capability.go`
- Modify: `api/internal/adapter/http/middleware_csrf.go` (remove `requireCapability`)
- Modify: `api/internal/adapter/http/router.go`
- Modify: `api/internal/adapter/http/errors.go`
- Modify: `api/internal/adapter/http/api_test.go`
- Modify: `api/cmd/api/main.go`

**Interfaces:**
- Consumes: `usecase.AccountService` (Tasks 35–36), `postgres.NewAccountRepo` (Task 37), `requireSession`, `requireCSRF`, `requireOwner`, `RequestScope` (all existing).
- Produces: the five routes; `Deps.Accounts *usecase.AccountService`.

- [ ] **Step 1: Add the new fixture and the failing guard tests**

In `api/internal/adapter/http/api_test.go`, add a second limited member to `testEnv` who **does** hold `money`:

```go
	// moneyLimitedEmail is a limited member who holds the money capability --
	// the state Settings' "off for kids by default" switch produces when an
	// owner turns Money on for a child.
	//
	// It exists because env.limitedEmail holds only calendar and chores, so
	// every accounts write route would refuse them at requireCapability and
	// TestOwnerOnlyRoutesRejectALimitedMember would pass without ever
	// exercising requireOwner -- a green that proves nothing about the guard
	// it is named after.
	moneyLimitedEmail    string
	moneyLimitedPassword string
```

Create that member in `newTestEnvWithClock` beside the existing one, with `domain.Capabilities{domain.CapCalendar, domain.CapChores, domain.CapMoney}`.

Then add:

```go
// TestAccountsListRequiresTheMoneyCapability is the first capability gate in
// the product. Until this route existed, requireCapability was defined and
// unused, so the promise that the server enforces capabilities independently
// of the UI was vacuous.
func TestAccountsListRequiresTheMoneyCapability(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.limitedEmail, env.limitedPassword) // calendar + chores

	rec := env.authedGet(t, "/api/v1/accounts", session)
	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")
}

// TestAccountsWriteRequiresOwnership is the half the capability gate does not
// cover: a limited member who *does* hold money can read the screen and must
// not be able to change it. Kids look, parents manage.
func TestAccountsWriteRequiresOwnership(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)

	rec := env.authed(t, http.MethodPost, "/api/v1/accounts", map[string]any{
		"nickname": "Sneaky", "type": "cash",
		"openingBalanceMinor": 100, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26",
	}, session, csrf)
	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")
}

// TestAccountsAreRedactedForALimitedMember asserts the amount fields are
// ABSENT, not zero. A zeroed balance still reads as a real one, and a zeroed
// net worth says "this family has nothing" -- a different and worse untruth
// than saying nothing.
func TestAccountsAreRedactedForALimitedMember(t *testing.T) {
	env := newTestEnv(t)
	ownerSession, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// One visible to limited members, one not.
	env.mustCreateAccount(t, ownerSession, ownerCSRF, map[string]any{
		"nickname": "OCBC Joint Savings", "type": "cash",
		"openingBalanceMinor": 4_690_000, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26", "visibleToLimitedMembers": true,
	})
	env.mustCreateAccount(t, ownerSession, ownerCSRF, map[string]any{
		"nickname": "DBS Everyday", "type": "cash",
		"openingBalanceMinor": 824_055, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26", "visibleToLimitedMembers": false,
	})

	session, _ := env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)
	rec := env.authedGet(t, "/api/v1/accounts", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, present := raw["summary"]; present {
		t.Error("summary is present for a limited member; it must be omitted entirely")
	}

	accounts, ok := raw["accounts"].([]any)
	if !ok || len(accounts) != 1 {
		t.Fatalf("accounts = %v, want exactly the one shared account", raw["accounts"])
	}
	entry := accounts[0].(map[string]any)
	if entry["nickname"] != "OCBC Joint Savings" {
		t.Errorf("nickname = %v, want the shared account", entry["nickname"])
	}
	for _, field := range []string{"balance", "balanceAsOf"} {
		if _, present := entry[field]; present {
			t.Errorf("%q is present for a limited member; it must be absent, not zeroed", field)
		}
	}
}

// TestOwnerSeesEveryAccountAndTheSummary is the control for the test above: a
// redaction test that passed because the endpoint returns nothing to anybody
// would be worthless.
func TestOwnerSeesEveryAccountAndTheSummary(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	env.mustCreateAccount(t, session, csrf, map[string]any{
		"nickname": "DBS Everyday", "type": "cash",
		"openingBalanceMinor": 824_055, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26",
	})
	env.mustCreateAccount(t, session, csrf, map[string]any{
		"nickname": "Car loan", "type": "loan",
		"openingBalanceMinor": 1_450_000, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-26",
	})

	rec := env.authedGet(t, "/api/v1/accounts", session)
	var got struct {
		Accounts []struct {
			Nickname string `json:"nickname"`
			Balance  struct {
				AmountMinor int64  `json:"amountMinor"`
				Currency    string `json:"currency"`
			} `json:"balance"`
		} `json:"accounts"`
		Summary *struct {
			NetWorthMinor int64 `json:"netWorthMinor"`
			Computable    bool  `json:"computable"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(got.Accounts))
	}
	if got.Summary == nil {
		t.Fatal("summary is missing for an owner")
	}
	if !got.Summary.Computable || got.Summary.NetWorthMinor != -625_945 {
		t.Errorf("summary = %+v, want a computable net worth of -625945 (824055 - 1450000)", got.Summary)
	}
}
```

Add `env.authedGet` and `env.mustCreateAccount` helpers beside the existing `env.authed`, following its shape.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd api && go test ./internal/adapter/http/ -run TestAccounts -v
```
Expected: FAIL — 404, the routes do not exist.

- [ ] **Step 3: Move `requireCapability` into its own file**

Create `api/internal/adapter/http/middleware_capability.go` holding `requireCapability` verbatim from `middleware_csrf.go:67-83`, with its doc comment rewritten now that the claim in it is out of date:

```go
package httpadapter

import (
	"net/http"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// requireCapability answers 403 FORBIDDEN unless the caller's Scope carries
// cap. The accounts routes are its first users: before them it was defined and
// unwired, which made the promise that the server enforces capabilities
// independently of the UI vacuous.
//
// On the account write routes it is stacked with requireOwner, and today that
// is redundant -- domain.ValidateMembershipChange refuses an owner who does not
// hold every capability, so "an owner without money" is not a representable
// state. It is stacked anyway: the alternative is for these routes to depend on
// an invariant enforced in a different layer for a different reason, and if
// that invariant is ever relaxed every route leaning on it opens silently. One
// extra middleware call is a cheaper price than that coupling.
func requireCapability(cap domain.Capability) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := RequestScope(r)
			if !ok || !scope.Membership.Capabilities.Has(cap) {
				WriteError(w, http.StatusForbidden, "FORBIDDEN", "You do not have permission to do that.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

Delete it from `middleware_csrf.go`, and delete the now-dangling `domain` import there if nothing else in that file uses it.

- [ ] **Step 4: Write `api/internal/adapter/http/account_handlers.go`**

```go
package httpadapter

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// openingBalanceLayout is the wire format for opening_balance_as_of: a
// calendar date, because "the balance was true on the 26th" is a fact about a
// day and not about an instant.
const openingBalanceLayout = "2006-01-02"

type moneyDTO struct {
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
}

// accountDTO omits Balance and BalanceAsOf entirely for a limited member --
// hence the pointers and omitempty rather than zero values. A zeroed amount
// still reads as a real balance, which is the failure this shape exists to
// make impossible.
type accountDTO struct {
	ID                      string    `json:"id"`
	Nickname                string    `json:"nickname"`
	Type                    string    `json:"type"`
	OwnerMembershipID       *string   `json:"ownerMembershipId"`
	OwnerName               *string   `json:"ownerName"`
	Balance                 *moneyDTO `json:"balance,omitempty"`
	BalanceAsOf             *string   `json:"balanceAsOf,omitempty"`
	CountTowardNetWorth     bool      `json:"countTowardNetWorth"`
	VisibleToLimitedMembers bool      `json:"visibleToLimitedMembers"`
	ArchivedAt              *time.Time `json:"archivedAt"`
}

type breakdownDTO struct {
	Type       string `json:"type"`
	TotalMinor int64  `json:"totalMinor"`
}

type excludedDTO struct {
	AccountID string `json:"accountId"`
	Currency  string `json:"currency"`
}

// summaryDTO's NetWorthMinor, AssetsMinor and LiabilitiesMinor are pointers so
// that an incomputable summary carries no figures at all rather than zeros.
// Zero is a claim about the household's money; the truth in that state is that
// we cannot compute it.
type summaryDTO struct {
	Currency         string         `json:"currency"`
	Computable       bool           `json:"computable"`
	NetWorthMinor    *int64         `json:"netWorthMinor,omitempty"`
	AssetsMinor      *int64         `json:"assetsMinor,omitempty"`
	LiabilitiesMinor *int64         `json:"liabilitiesMinor,omitempty"`
	Breakdown        []breakdownDTO `json:"breakdown"`
	ExcludedNoRate   []excludedDTO  `json:"excludedNoRate"`
	ExcludedByChoice int            `json:"excludedByChoice"`
}

type accountsResponse struct {
	Accounts []accountDTO `json:"accounts"`
	Summary  *summaryDTO  `json:"summary,omitempty"`
}

// handleListAccounts is the one endpoint the Finances screen reads. It returns
// the list and the summary together because they are one screen and must
// describe the same set of rows -- two endpoints would mean writing the
// redaction below twice, and a rule written twice is a rule fixed once.
func handleListAccounts(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		includeArchived := r.URL.Query().Get("include_archived") == "true"

		views, err := deps.Accounts.List(r.Context(), scope.HouseholdID, includeArchived)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		// The redaction is here, in the handler, not in AccountService: it is a
		// rule about who is asking, and services in this codebase never take an
		// actor.
		if scope.Membership.Role == domain.RoleLimited {
			WriteJSON(w, http.StatusOK, accountsResponse{Accounts: redactedAccounts(views)})
			return
		}

		summary, err := deps.Accounts.Summary(r.Context(), scope.HouseholdID, views)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		out := accountsResponse{Accounts: make([]accountDTO, 0, len(views))}
		for _, v := range views {
			out.Accounts = append(out.Accounts, toAccountDTO(v))
		}
		dto := toSummaryDTO(summary)
		out.Summary = &dto
		WriteJSON(w, http.StatusOK, out)
	}
}

// redactedAccounts is what a limited member sees: only the accounts shared
// with them, and no amounts anywhere. The summary is omitted by the caller.
func redactedAccounts(views []usecase.AccountView) []accountDTO {
	out := make([]accountDTO, 0, len(views))
	for _, v := range views {
		if !v.Account.VisibleToLimitedMembers {
			continue
		}
		dto := toAccountDTO(v)
		dto.Balance = nil
		dto.BalanceAsOf = nil
		out = append(out, dto)
	}
	return out
}
```

The converters and the four mutating handlers go in the same file:

```go
func toAccountDTO(v usecase.AccountView) accountDTO {
	dto := accountDTO{
		ID:                      v.Account.ID,
		Nickname:                v.Account.Nickname,
		Type:                    string(v.Account.Type),
		CountTowardNetWorth:     v.Account.CountTowardNetWorth,
		VisibleToLimitedMembers: v.Account.VisibleToLimitedMembers,
		ArchivedAt:              v.Account.ArchivedAt,
		Balance:                 &moneyDTO{AmountMinor: v.Balance.Amount, Currency: v.Balance.Currency},
	}
	// "" means shared, and the wire form of shared is JSON null -- not an
	// empty string, which would read as a member whose id happens to be blank.
	if v.Account.OwnerMembershipID != "" {
		id, name := v.Account.OwnerMembershipID, v.OwnerName
		dto.OwnerMembershipID, dto.OwnerName = &id, &name
	}
	asOf := v.Account.OpeningBalanceAsOf.Format(openingBalanceLayout)
	dto.BalanceAsOf = &asOf
	return dto
}

func toSummaryDTO(s usecase.NetWorthSummary) summaryDTO {
	dto := summaryDTO{
		Currency:         s.Currency,
		Computable:       s.Computable,
		Breakdown:        make([]breakdownDTO, 0, len(s.Breakdown)),
		ExcludedNoRate:   make([]excludedDTO, 0, len(s.ExcludedNoRate)),
		ExcludedByChoice: s.ExcludedByChoice,
	}
	// The three figures are attached only when they mean something. An
	// incomputable summary carries none of them, so the frontend cannot render
	// a zero it was never given.
	if s.Computable {
		netWorth, assets, liabilities := s.NetWorth.Amount, s.Assets.Amount, s.Liabilities.Amount
		dto.NetWorthMinor, dto.AssetsMinor, dto.LiabilitiesMinor = &netWorth, &assets, &liabilities
	}
	for _, entry := range s.Breakdown {
		dto.Breakdown = append(dto.Breakdown, breakdownDTO{
			Type: string(entry.Type), TotalMinor: entry.Total.Amount,
		})
	}
	for _, ex := range s.ExcludedNoRate {
		dto.ExcludedNoRate = append(dto.ExcludedNoRate, excludedDTO{
			AccountID: ex.AccountID, Currency: ex.Currency,
		})
	}
	return dto
}

type createAccountRequest struct {
	Nickname                string  `json:"nickname"`
	Type                    string  `json:"type"`
	OwnerMembershipID       *string `json:"ownerMembershipId"`
	OpeningBalanceMinor     int64   `json:"openingBalanceMinor"`
	OpeningBalanceCurrency  string  `json:"openingBalanceCurrency"`
	OpeningBalanceAsOf      string  `json:"openingBalanceAsOf"`
	CountTowardNetWorth     *bool   `json:"countTowardNetWorth"`
	VisibleToLimitedMembers *bool   `json:"visibleToLimitedMembers"`
}

// updateAccountRequest's fields are all pointers so a field the caller did not
// name reaches usecase.AccountUpdate as nil and keeps its stored value --
// the same real-patch convention TestUpdateHouseholdIsARealPatch pins for
// PATCH /household.
type updateAccountRequest struct {
	Nickname                *string `json:"nickname"`
	Type                    *string `json:"type"`
	OwnerMembershipID       *string `json:"ownerMembershipId"`
	OpeningBalanceMinor     *int64  `json:"openingBalanceMinor"`
	OpeningBalanceCurrency  *string `json:"openingBalanceCurrency"`
	OpeningBalanceAsOf      *string `json:"openingBalanceAsOf"`
	CountTowardNetWorth     *bool   `json:"countTowardNetWorth"`
	VisibleToLimitedMembers *bool   `json:"visibleToLimitedMembers"`
}

func handleCreateAccount(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req createAccountRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		asOf, err := time.Parse(openingBalanceLayout, req.OpeningBalanceAsOf)
		if err != nil {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_AS_OF",
				"That date could not be read. Use YYYY-MM-DD.", nil)
			return
		}

		in := usecase.NewAccount{
			HouseholdID:            scope.HouseholdID,
			Nickname:               req.Nickname,
			Type:                   req.Type,
			OpeningBalanceMinor:    req.OpeningBalanceMinor,
			OpeningBalanceCurrency: req.OpeningBalanceCurrency,
			OpeningBalanceAsOf:     asOf,
			// The design draws these toggles on and off respectively, and an
			// omitted field must land on the same default the form shows --
			// otherwise a client that sends neither gets an account that
			// counts toward nothing and is visible to children.
			CountTowardNetWorth:     true,
			VisibleToLimitedMembers: false,
		}
		if req.OwnerMembershipID != nil {
			in.OwnerMembershipID = *req.OwnerMembershipID
		}
		if req.CountTowardNetWorth != nil {
			in.CountTowardNetWorth = *req.CountTowardNetWorth
		}
		if req.VisibleToLimitedMembers != nil {
			in.VisibleToLimitedMembers = *req.VisibleToLimitedMembers
		}

		created, err := deps.Accounts.Create(r.Context(), in)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeAccount(w, r, deps, scope.HouseholdID, created.ID)
	}
}

func handleUpdateAccount(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req updateAccountRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		patch := usecase.AccountUpdate{
			Nickname:                req.Nickname,
			Type:                    req.Type,
			OwnerMembershipID:       req.OwnerMembershipID,
			OpeningBalanceMinor:     req.OpeningBalanceMinor,
			OpeningBalanceCurrency:  req.OpeningBalanceCurrency,
			CountTowardNetWorth:     req.CountTowardNetWorth,
			VisibleToLimitedMembers: req.VisibleToLimitedMembers,
		}
		if req.OpeningBalanceAsOf != nil {
			asOf, err := time.Parse(openingBalanceLayout, *req.OpeningBalanceAsOf)
			if err != nil {
				WriteError(w, http.StatusUnprocessableEntity, "INVALID_AS_OF",
					"That date could not be read. Use YYYY-MM-DD.", nil)
				return
			}
			patch.OpeningBalanceAsOf = &asOf
		}

		id := chi.URLParam(r, "id")
		if _, err := deps.Accounts.Update(r.Context(), scope.HouseholdID, id, patch); err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeAccount(w, r, deps, scope.HouseholdID, id)
	}
}

func handleArchiveAccount(deps Deps) http.HandlerFunc { return setArchived(deps, true) }
func handleRestoreAccount(deps Deps) http.HandlerFunc { return setArchived(deps, false) }

// setArchived backs both the archive and the restore route. One function
// rather than two near-identical ones: the pair differ by a single boolean,
// and this project's repeated lesson is that a rule written twice is a rule
// fixed once.
func setArchived(deps Deps, archived bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		id := chi.URLParam(r, "id")
		if _, err := deps.Accounts.SetArchived(r.Context(), scope.HouseholdID, id, archived); err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeAccount(w, r, deps, scope.HouseholdID, id)
	}
}

// writeAccount re-reads the account through Get so every mutating response
// carries the owner's display name, which the write queries do not return.
// It answers 200 with a body, never 204: apiFetch throws INVALID_RESPONSE on
// an ok response it cannot parse.
func writeAccount(w http.ResponseWriter, r *http.Request, deps Deps, householdID, accountID string) {
	view, err := deps.Accounts.Get(r.Context(), householdID, accountID)
	if err != nil {
		MapDomainError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, toAccountDTO(view))
}
```

- [ ] **Step 5: Add the error mappings**

Insert into `MapDomainError`'s switch in `api/internal/adapter/http/errors.go`, before the `domain.ErrAlreadyExists` backstop:

```go
	case errors.Is(err, domain.ErrAccountNicknameRequired):
		WriteError(w, http.StatusUnprocessableEntity, "NICKNAME_REQUIRED", "An account name is required.", nil)
	case errors.Is(err, domain.ErrUnknownAccountType):
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_TYPE", "That account type is not recognised.", nil)
	case errors.Is(err, domain.ErrLiabilityBalanceNegative):
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_BALANCE",
			"Enter what you owe as a positive amount — Hearth subtracts it for you.", nil)
	case errors.Is(err, domain.ErrOpeningBalanceInFuture):
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_AS_OF", "That date is in the future.", nil)
	case errors.Is(err, domain.ErrAccountOwnerNotInHousehold):
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_OWNER", "That person is not in this household.", nil)
```

`domain.ErrInvalidMoney` already maps to `422 INVALID_CURRENCY` (line 118), which is what `ParseSelectableCurrency` returns — no new case needed for currency.

- [ ] **Step 6: Wire the routes**

Add `Accounts *usecase.AccountService` to `Deps` in `router.go`, and inside the `requireSession` group:

```go
			// Accounts: the first capability-gated routes in the product. Reads
			// need money; writes need money and owner, stacked -- see
			// middleware_capability.go for why the redundancy is deliberate.
			g.Group(func(a chi.Router) {
				a.Use(requireCapability(domain.CapMoney))
				a.Get("/accounts", handleListAccounts(deps))

				a.Group(func(w chi.Router) {
					w.Use(requireCSRF)
					w.Use(requireOwner)
					w.Post("/accounts", handleCreateAccount(deps))
					w.Patch("/accounts/{id}", handleUpdateAccount(deps))
					// Archive and restore are their own routes rather than a
					// field on PATCH: if archiving were patchable, an ordinary
					// edit that happened to include it would archive the account
					// as a side effect of saving a nickname.
					w.Post("/accounts/{id}/archive", handleArchiveAccount(deps))
					w.Post("/accounts/{id}/restore", handleRestoreAccount(deps))
				})
			})
```

In `api/cmd/api/main.go`, build the repo and service and pass them into `Deps`:

```go
	accountSvc := usecase.NewAccountService(usecase.AccountDeps{
		Accounts:   postgres.NewAccountRepo(db),
		Households: households,
		FX:         fxProvider,
		Clock:      clk,
	})
```

- [ ] **Step 7: Update the route-walk allowlists**

`TestOwnerOnlyRoutesRejectALimitedMember` signs in as `env.limitedEmail`, who has no `money` — so the four account write routes would 403 at `requireCapability` and the walk would prove nothing about `requireOwner`. Change that test to sign in as `env.moneyLimitedEmail` instead. Both members are limited, so every existing assertion still holds, and the accounts routes now genuinely exercise the owner gate.

No entry is added to `TestEveryProtectedRouteRejectsAnUnauthenticatedCaller`'s `public` map — all five routes are behind a session — and its `checked < 12` floor rises to `checked < 17`.

- [ ] **Step 8: Run the whole HTTP suite**

```bash
cd api && go test ./internal/adapter/http/ -v
```
Expected: PASS, including the four new tests and every existing one.

- [ ] **Step 9: Mutation-check the owner gate**

Temporarily remove `w.Use(requireOwner)` from the write group. `TestAccountsWriteRequiresOwnership` must fail with a 201 or 200 where it expected 403. Restore it.

Then temporarily remove `dto.Balance = nil` from `redactedAccounts`. `TestAccountsAreRedactedForALimitedMember` must fail. Restore.

- [ ] **Step 10: Commit**

```bash
git add api/internal/adapter/http api/cmd/api/main.go
git commit -m "feat(http): the accounts routes, and the first capability gate

requireCapability was defined and unused until now, which made the promise that
the server enforces capabilities independently of the UI vacuous. Reads need
money; writes need money and owner.

The owner-gated route walk now signs in as a limited member who holds money.
The existing fixture holds calendar and chores only, so every accounts write
would have been refused at requireCapability and the walk would have passed
without ever reaching requireOwner.

A limited member's response omits balance, balanceAsOf and the whole summary
rather than zeroing them: a zeroed net worth says the family has nothing, which
is a worse untruth than saying nothing."
```

---

### Task 39: Frontend — the Finances page and its four states

**Files:**
- Create: `web/src/features/money/schemas.ts`
- Create: `web/src/features/money/copy.ts`
- Create: `web/src/features/money/accountTypes.ts`
- Create: `web/src/features/money/formatMoney.ts`
- Create: `web/src/features/money/formatMoney.test.ts`
- Create: `web/src/features/money/useAccounts.ts`
- Create: `web/src/features/money/FinancesPage.tsx`
- Create: `web/src/features/money/FinancesPage.test.tsx`
- Create: `web/src/features/money/NetWorthCard.tsx`
- Create: `web/src/features/money/BreakdownCard.tsx`
- Create: `web/src/features/money/AccountsPanel.tsx`
- Modify: `web/src/routes/router.tsx`

**Interfaces:**
- Consumes: `GET /api/v1/accounts` (Task 38), `apiFetch`, `useMe`, `useCurrencies`, `stubFetchRoutes`.
- Produces: `formatMoney(minor, currency, symbol?)`, `useAccounts(includeArchived)`, `accountsResponseSchema`, `ACCOUNT_TYPE_LABELS`.

- [ ] **Step 1: Write the failing formatter test**

Create `web/src/features/money/formatMoney.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { formatMoney } from "./formatMoney";

describe("formatMoney", () => {
  it("renders the design's SGD figure", () => {
    expect(formatMoney(824055, "SGD", "S$")).toBe("S$8,240.55");
  });

  it("renders a debt with its minus sign outside the symbol", () => {
    expect(formatMoney(-1450000, "SGD", "S$")).toBe("−S$14,500.00");
  });

  // IDR is a two-minor-unit currency in the allowlist, but the design draws
  // Rp 85,400,000 with no decimals -- nobody quotes rupiah cents. This is a
  // display choice; the stored value keeps its minor units.
  it("renders IDR without decimals, as the design does", () => {
    expect(formatMoney(8540000000, "IDR", "Rp")).toBe("Rp 85,400,000");
  });

  it("falls back to the bare code when no symbol is known", () => {
    expect(formatMoney(100000, "BRL")).toBe("BRL 1,000.00");
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd web && npx vitest run src/features/money/formatMoney.test.ts
```
Expected: FAIL — cannot resolve `./formatMoney`.

- [ ] **Step 3: Write `formatMoney.ts`**

```ts
// The one place minor units become a string. One helper rather than
// per-component formatting, because four components formatting independently
// will disagree about thousands separators -- and this project has a rule
// about fixing the class rather than the instance.
//
// The backend sends minor units plus an ISO 4217 code and never a formatted
// string: domain.Money.String() hard-codes two decimals and puts the code in
// front, which is right for a log line and wrong for a screen.

// NO_DECIMAL_CURRENCIES are the codes this app renders whole even though the
// backend's allowlist treats them as two-minor-unit currencies. IDR is here
// because the design draws Rp 85,400,000 and nobody quotes rupiah cents. This
// affects display only -- the stored value keeps every minor unit.
// Exported because AccountModal's toMinorUnits reads the same set: a currency
// this file renders whole must be parsed whole too, or "85400000" typed into
// the form and "Rp 85,400,000" shown back would disagree by two decimal places.
export const NO_DECIMAL_CURRENCIES = new Set(["IDR", "VND"]);

// U+2212 MINUS SIGN, not a hyphen: it aligns with digits at the same width,
// which a hyphen does not, and every negative figure in this app is in a
// column of numbers.
const MINUS = "−";

export function formatMoney(
  amountMinor: number,
  currency: string,
  symbol?: string,
): string {
  const whole = NO_DECIMAL_CURRENCIES.has(currency);
  const digits = whole ? 0 : 2;
  const magnitude = Math.abs(amountMinor) / 100;

  const formatted = new Intl.NumberFormat("en-SG", {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(magnitude);

  // A known symbol butts against the digits (S$8,240.55); a bare code needs a
  // space (BRL 1,000.00) or it reads as one token.
  const prefix = symbol ? symbol : `${currency} `;
  const sign = amountMinor < 0 ? MINUS : "";
  return `${sign}${prefix}${formatted}`;
}
```

- [ ] **Step 4: Run the formatter test**

```bash
cd web && npx vitest run src/features/money/formatMoney.test.ts
```
Expected: PASS, four assertions.

- [ ] **Step 5: Write the schemas, copy and type labels**

`web/src/features/money/schemas.ts`:

```ts
import { z } from "zod";

export const accountTypeSchema = z.enum([
  "cash",
  "investment",
  "property",
  "loan",
  "credit_card",
]);
export type AccountType = z.infer<typeof accountTypeSchema>;

const moneySchema = z.object({
  amountMinor: z.number(),
  currency: z.string(),
});

// balance and balanceAsOf are optional because a limited member's response
// omits them entirely. Modelling them as `number | null` instead would let a
// component render a zero balance for someone who is not allowed to see one.
export const accountSchema = z.object({
  id: z.string(),
  nickname: z.string(),
  type: accountTypeSchema,
  ownerMembershipId: z.string().nullable(),
  ownerName: z.string().nullable(),
  balance: moneySchema.optional(),
  balanceAsOf: z.string().optional(),
  countTowardNetWorth: z.boolean(),
  visibleToLimitedMembers: z.boolean(),
  archivedAt: z.string().nullable(),
});
export type Account = z.infer<typeof accountSchema>;

export const summarySchema = z.object({
  currency: z.string(),
  computable: z.boolean(),
  netWorthMinor: z.number().optional(),
  assetsMinor: z.number().optional(),
  liabilitiesMinor: z.number().optional(),
  breakdown: z.array(z.object({ type: accountTypeSchema, totalMinor: z.number() })),
  excludedNoRate: z.array(z.object({ accountId: z.string(), currency: z.string() })),
  excludedByChoice: z.number(),
});
export type Summary = z.infer<typeof summarySchema>;

export const accountsResponseSchema = z.object({
  accounts: z.array(accountSchema),
  summary: summarySchema.optional(),
});
export type AccountsResponse = z.infer<typeof accountsResponseSchema>;
```

`web/src/features/money/accountTypes.ts`:

```ts
import type { AccountType } from "./schemas";

// The five types, in the order the breakdown chart draws them: assets first,
// then debts. Mirrors domain.AccountTypes() -- the two lists must not drift,
// and this comment is where someone adding a sixth will look.
export const ACCOUNT_TYPES: AccountType[] = [
  "cash",
  "investment",
  "property",
  "loan",
  "credit_card",
];

export const ACCOUNT_TYPE_LABELS: Record<AccountType, string> = {
  cash: "Cash & savings",
  investment: "Investments",
  property: "Property",
  loan: "Loan",
  credit_card: "Credit card",
};

export const LIABILITY_TYPES: ReadonlySet<AccountType> = new Set(["loan", "credit_card"]);
```

`web/src/features/money/copy.ts` holds the design's strings named in Global Constraints, plus the states this plan adds:

```ts
export const FINANCES_COPY = {
  title: "Finances",
  netWorth: "Net worth",
  assetsAndLiabilities: "Assets & liabilities",
  net: "Net",
  accounts: "Accounts",
  addAccount: "+ Add account",
  // First run. Every household sees this immediately after signing up, so it
  // is a real screen rather than an edge case.
  emptyTitle: "Nothing here yet.",
  emptyBody: "Add what your household owns and owes, and Hearth will keep the total.",
  // No account could be converted into the household's currency. Never a zero
  // -- zero is a claim about their money, and the truth is that we cannot
  // compute it.
  notComputable: (household: string, others: string) =>
    `We can't work out a total yet: there's no exchange rate between ${household} and ${others}.`,
  excludedNoRate: (count: number, currencies: string) =>
    `${count} ${count === 1 ? "account" : "accounts"} not included: no exchange rate for ${currencies}.`,
  excludedByChoice: (count: number) =>
    `${count} ${count === 1 ? "account is" : "accounts are"} set not to count toward net worth.`,
  // The bars will not always sum to net worth, because an account can be in
  // the breakdown and out of the total. Say so rather than letting it look
  // like an arithmetic bug.
  breakdownFootnote: "Includes accounts that don't count toward net worth.",
  limitedEmpty: "No accounts have been shared with you yet.",
  archivedToggle: "Show archived",
  archivedEmpty: "No archived accounts.",
} as const;
```

- [ ] **Step 6: Write `useAccounts.ts`**

```ts
import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { accountsResponseSchema, type AccountsResponse } from "./schemas";

export function accountsQueryKey(includeArchived: boolean) {
  return ["accounts", { includeArchived }] as const;
}

async function fetchAccounts(includeArchived: boolean): Promise<AccountsResponse> {
  const suffix = includeArchived ? "?include_archived=true" : "";
  const body = await apiFetch<unknown>(`/api/v1/accounts${suffix}`);
  return accountsResponseSchema.parse(body);
}

export function useAccounts(includeArchived: boolean) {
  return useQuery({
    queryKey: accountsQueryKey(includeArchived),
    queryFn: () => fetchAccounts(includeArchived),
  });
}
```

- [ ] **Step 7: Write the failing page tests**

Create `web/src/features/money/FinancesPage.test.tsx`. Every request goes through `stubFetchRoutes`, which matches on method and URL and throws on anything unregistered.

```tsx
import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { renderWithRouter } from "../../test/renderWithRouter";
import { FinancesPage } from "./FinancesPage";

afterEach(() => vi.unstubAllGlobals());

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

describe("FinancesPage", () => {
  it("shows the first-run panel and no empty cards when there are no accounts", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/accounts": {
        status: 200,
        body: {
          accounts: [],
          summary: {
            currency: "SGD", computable: true, netWorthMinor: 0,
            assetsMinor: 0, liabilitiesMinor: 0,
            breakdown: [], excludedNoRate: [], excludedByChoice: 0,
          },
        },
      },
    });

    renderWithRouter(<FinancesPage />);

    expect(await screen.findByText("Nothing here yet.")).toBeInTheDocument();
    expect(screen.queryByText("Net worth")).not.toBeInTheDocument();
  });

  it("shows net worth and one bar per populated type", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/accounts": {
        status: 200,
        body: {
          accounts: [{
            id: "a1", nickname: "DBS Everyday", type: "cash",
            ownerMembershipId: null, ownerName: null,
            balance: { amountMinor: 824055, currency: "SGD" },
            balanceAsOf: "2026-07-26",
            countTowardNetWorth: true, visibleToLimitedMembers: false,
            archivedAt: null,
          }],
          summary: {
            currency: "SGD", computable: true, netWorthMinor: 824055,
            assetsMinor: 824055, liabilitiesMinor: 0,
            breakdown: [{ type: "cash", totalMinor: 824055 }],
            excludedNoRate: [], excludedByChoice: 0,
          },
        },
      },
    });

    renderWithRouter(<FinancesPage />);

    expect(await screen.findByText("S$8,240.55")).toBeInTheDocument();
    expect(screen.getByText("Cash & savings")).toBeInTheDocument();
    expect(screen.queryByText("Property")).not.toBeInTheDocument();
  });

  // The state a household reaches by changing its primary currency in
  // Settings. A zero here would say they have nothing.
  it("shows no figure at all when nothing can be converted", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/accounts": {
        status: 200,
        body: {
          accounts: [{
            id: "a1", nickname: "Chase", type: "cash",
            ownerMembershipId: null, ownerName: null,
            balance: { amountMinor: 500000, currency: "USD" },
            balanceAsOf: "2026-07-26",
            countTowardNetWorth: true, visibleToLimitedMembers: false,
            archivedAt: null,
          }],
          summary: {
            currency: "SGD", computable: false,
            breakdown: [],
            excludedNoRate: [{ accountId: "a1", currency: "USD" }],
            excludedByChoice: 0,
          },
        },
      },
    });

    renderWithRouter(<FinancesPage />);

    expect(await screen.findByText(/no exchange rate between SGD and USD/)).toBeInTheDocument();
    expect(screen.queryByText("S$0.00")).not.toBeInTheDocument();
  });

  // A limited member's response carries no summary and no amounts. The page
  // must not synthesise either.
  it("shows a limited member the shared accounts and no figures", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/accounts": {
        status: 200,
        body: {
          accounts: [{
            id: "a1", nickname: "OCBC Joint Savings", type: "cash",
            ownerMembershipId: null, ownerName: null,
            countTowardNetWorth: true, visibleToLimitedMembers: true,
            archivedAt: null,
          }],
        },
      },
    });

    renderWithRouter(<FinancesPage />);

    expect(await screen.findByText("OCBC Joint Savings")).toBeInTheDocument();
    expect(screen.queryByText("Net worth")).not.toBeInTheDocument();
    expect(screen.queryByText(/S\$/)).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 8: Run them to verify they fail**

```bash
cd web && npx vitest run src/features/money/FinancesPage.test.tsx
```
Expected: FAIL — cannot resolve `./FinancesPage`.

- [ ] **Step 9: Write the three card components and the page**

`NetWorthCard.tsx` renders `FINANCES_COPY.netWorth`, the figure through `formatMoney`, and the exclusion lines. When `summary.computable` is false it renders `FINANCES_COPY.notComputable` with the household's currency and the distinct currencies in `excludedNoRate`, and **no figure**.

`BreakdownCard.tsx` renders `FINANCES_COPY.assetsAndLiabilities`, one row per `summary.breakdown` entry (label from `ACCOUNT_TYPE_LABELS`, amount through `formatMoney`, debts shown negative via `LIABILITY_TYPES`), a `Net` row, and `FINANCES_COPY.breakdownFootnote` when `excludedByChoice > 0`.

`AccountsPanel.tsx` renders `FINANCES_COPY.accounts`, the `+ Add account` button (owners only — read `useMe().data?.membership.role`), and one row per account: nickname, a subtitle of the owner name or "Shared" plus the type label, and the balance when present. It also holds the "Show archived" toggle, which flips the `includeArchived` argument to `useAccounts`.

`FinancesPage.tsx` composes them and chooses between the four states:

```tsx
// The Money space's landing page. It replaces the placeholder at /money; the
// four sibling pages (Transactions, Budget, Goals, Bills) keep theirs, and the
// sidebar is untouched -- it renders from the server's space list.
//
// The recent-transactions strip the design draws is deliberately absent: it has
// no data until Transactions ships, and an empty card promising future
// usefulness is a placeholder that looks considered.
export function FinancesPage() {
  const [includeArchived, setIncludeArchived] = useState(false);
  const accounts = useAccounts(includeArchived);
  const me = useMe();
  const currencies = useCurrencies();
  const isOwner = me.data?.membership.role === "owner";

  if (accounts.isPending) return <p className="text-xs text-muted">Loading…</p>;
  if (accounts.isError) {
    return (
      <p role="alert" className="text-xs text-danger">
        Couldn't load your accounts.
      </p>
    );
  }

  const { accounts: rows, summary } = accounts.data;

  // No summary at all means the caller is a limited member -- the server omits
  // it rather than zeroing it, and the page must not invent one.
  if (!summary) {
    return <LimitedMemberView accounts={rows} />;
  }
  if (rows.length === 0 && !includeArchived) {
    return <FirstRunPanel canAdd={isOwner} />;
  }
  // ... the three cards
}
```

- [ ] **Step 10: Point the route at it**

In `web/src/routes/router.tsx`, change `moneyIndexRoute`'s component from `<PlaceholderPage page="Money" slice={2} />` to `<FinancesPage />`. Leave `moneySplatRoute` on the placeholder — Transactions, Budget, Goals and Bills do not exist yet.

- [ ] **Step 11: Run the page tests and the typecheck**

```bash
cd web && npx vitest run src/features/money/ && npm run typecheck
```
Expected: PASS, eight tests.

- [ ] **Step 12: Mutation-check the limited-member branch**

Temporarily change `if (!summary)` to `if (false)`. The limited-member test must fail — the page would try to render a net worth card with no summary. Restore.

- [ ] **Step 13: Commit**

```bash
git add web/src/features/money web/src/routes/router.tsx
git commit -m "feat(web): the Finances page and its four states

An absent summary is how the page knows the caller is a limited member -- the
server omits it rather than zeroing it, and the page never synthesises one.

An incomputable net worth renders no figure at all. That is the state a
household reaches by changing its primary currency in Settings, and a zero
there would say they have nothing."
```

---

### Task 40: Frontend — the add/edit modal, archive and restore

**Files:**
- Create: `web/src/features/money/AccountModal.tsx`
- Create: `web/src/features/money/AccountModal.test.tsx`
- Modify: `web/src/features/money/useAccounts.ts`
- Modify: `web/src/features/money/AccountsPanel.tsx`

**Interfaces:**
- Consumes: `components/Modal`, `POST/PATCH /api/v1/accounts`, the archive and restore routes, `useCurrencies`, the members list.
- Produces: `useCreateAccount()`, `useUpdateAccount()`, `useSetAccountArchived()`, `<AccountModal>`.

- [ ] **Step 1: Add the mutations**

Append to `useAccounts.ts`. Each `onSuccess` **returns** its invalidation promise rather than firing and forgetting — a non-awaited invalidation re-enables the submit button while the list is still serving its stale cached value, which is the defect `CurrencyPanel` documents at `web/src/features/settings/CurrencyPanel.tsx:49`.

```ts
function invalidateAccounts(queryClient: QueryClient) {
  // Both keys: the panel may be showing either the live list or the archived
  // one, and an archive performed from one must not leave the other stale.
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: accountsQueryKey(false) }),
    queryClient.invalidateQueries({ queryKey: accountsQueryKey(true) }),
  ]);
}

export function useCreateAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: AccountFormValues): Promise<Account> => {
      const raw = await apiFetch<unknown>("/api/v1/accounts", {
        method: "POST",
        body: JSON.stringify(body),
      });
      return accountSchema.parse(raw);
    },
    onSuccess: () => invalidateAccounts(queryClient),
  });
}
```

`useUpdateAccount` is the same against `PATCH /api/v1/accounts/${id}`; `useSetAccountArchived` posts to `/archive` or `/restore` with no body and parses the returned account.

- [ ] **Step 2: Write the failing modal tests**

Create `web/src/features/money/AccountModal.test.tsx`:

```tsx
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { renderWithRouter } from "../../test/renderWithRouter";
import { AccountModal } from "./AccountModal";

afterEach(() => vi.unstubAllGlobals());

describe("AccountModal", () => {
  it("posts what the form was given, in minor units", async () => {
    let posted: unknown;
    stubFetchRoutes({
      "GET /api/v1/currencies": {
        status: 200,
        body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
      },
      "GET /api/v1/household/members": { status: 200, body: { members: [] } },
      "POST /api/v1/accounts": {
        status: 200,
        body: {
          id: "a1", nickname: "DBS Everyday", type: "cash",
          ownerMembershipId: null, ownerName: null,
          balance: { amountMinor: 824055, currency: "SGD" },
          balanceAsOf: "2026-07-26",
          countTowardNetWorth: true, visibleToLimitedMembers: false,
          archivedAt: null,
        },
        capture: (body) => { posted = body; },
      },
    });

    renderWithRouter(<AccountModal open onClose={() => {}} />);

    fireEvent.change(screen.getByLabelText("Nickname"), { target: { value: "DBS Everyday" } });
    fireEvent.change(screen.getByLabelText("Balance"), { target: { value: "8240.55" } });
    fireEvent.click(screen.getByRole("button", { name: "Add account" }));

    await waitFor(() => expect(posted).toBeDefined());
    expect(posted).toMatchObject({
      nickname: "DBS Everyday",
      type: "cash",
      openingBalanceMinor: 824055,
      openingBalanceCurrency: "SGD",
    });
  });

  // The rule that stops a car loan from making a household look richer, said
  // in the form rather than only in a 422. The backend refuses it either way.
  it("tells you to enter a debt as a positive amount", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": {
        status: 200,
        body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
      },
      "GET /api/v1/household/members": { status: 200, body: { members: [] } },
    });

    renderWithRouter(<AccountModal open onClose={() => {}} />);

    fireEvent.change(screen.getByLabelText("Nickname"), { target: { value: "Car loan" } });
    fireEvent.change(screen.getByLabelText("Type"), { target: { value: "loan" } });
    fireEvent.change(screen.getByLabelText("Balance"), { target: { value: "-14500" } });
    fireEvent.click(screen.getByRole("button", { name: "Add account" }));

    expect(
      await screen.findByText(/Enter what you owe as a positive amount/),
    ).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run them to verify they fail**

```bash
cd web && npx vitest run src/features/money/AccountModal.test.tsx
```
Expected: FAIL — cannot resolve `./AccountModal`.

- [ ] **Step 4: Write `AccountModal.tsx`**

The design's `3c` panel on `components/Modal`, minus the connected-bank header strip, which describes a sync that does not exist. Fields, with the design's own labels: **Nickname**; **Owner** (a select listing every member of the household plus a "Shared" option, which posts `ownerMembershipId: null`); **Type** (the five labels from `ACCOUNT_TYPE_LABELS`); **Balance** with a currency select defaulting to the household's primary; a date input for the as-of date defaulting to today; and the two toggles on `components/ToggleSwitch` — **Count toward net worth**, defaulting on, and **Visible to kids** with the design's helper copy, defaulting off.

Two things the component owns:

```tsx
// AccountFormValues is exactly the POST body the create route accepts, so the
// modal and useCreateAccount cannot disagree about field names.
export type AccountFormValues = {
  nickname: string;
  type: AccountType;
  ownerMembershipId: string | null;
  openingBalanceMinor: number;
  openingBalanceCurrency: string;
  openingBalanceAsOf: string;
  countTowardNetWorth: boolean;
  visibleToLimitedMembers: boolean;
};

// The form takes a decimal string ("8240.55") and posts minor units (824055).
// Splitting on the decimal point rather than multiplying a float by 100 is the
// frontend half of the rule that no floating-point value enters a monetary
// path: 8240.55 * 100 is 824054.9999999999 in IEEE 754, and Math.round hides
// that for most inputs while quietly getting some wrong.
//
// Returns null for anything that is not a number, which the caller shows as a
// field error rather than posting a NaN.
//
// Minor units are ALWAYS hundredths, for every currency this app accepts:
// domain.ParseSelectableCurrency admits only two-minor-unit codes, so IDR is
// stored in hundredths of a rupiah exactly as SGD is stored in cents. The
// no-decimal treatment is a display and input convention -- how many digits a
// person may type -- and never a change to the scale. Conflating the two
// would store Rp 85,400,000 as 85400000 and render it back as Rp 854,000.
const MINOR_UNITS_PER_MAJOR = 100;

function toMinorUnits(input: string, currency: string): number | null {
  const trimmed = input.trim().replace(/,/g, "");
  if (!/^-?\d+(\.\d+)?$/.test(trimmed)) return null;

  const negative = trimmed.startsWith("-");
  const [whole, fraction = ""] = trimmed.replace("-", "").split(".");
  const allowedDecimals = NO_DECIMAL_CURRENCIES.has(currency) ? 0 : 2;

  // More precision than the field offers is a typo, not a rounding problem.
  // Refusing is honest; silently truncating "8240.555" to 8240.55 would change
  // a figure the person is looking at.
  if (fraction.length > allowedDecimals) return null;

  const cents = fraction.padEnd(2, "0");
  const minor = Number(whole) * MINOR_UNITS_PER_MAJOR + Number(cents);
  return negative ? -minor : minor;
}
```

Add these two cases to `formatMoney.test.ts` in Task 39 — they are the pair that catches the scale confusion, and each fails against the wrong implementation:

```ts
it("round-trips the design's IDR figure through both directions", () => {
  // 8_540_000_000 hundredths-of-a-rupiah is Rp 85,400,000 -- the same scale
  // SGD uses, displayed without its cents.
  expect(formatMoney(8540000000, "IDR", "Rp")).toBe("Rp 85,400,000");
});

it("parses a figure whose cents would be wrong as a float", () => {
  // 8240.55 * 100 is 824054.9999999999 in IEEE 754.
  expect(toMinorUnits("8240.55", "SGD")).toBe(824055);
  expect(toMinorUnits("0.29", "SGD")).toBe(29);
});
```

`toMinorUnits` therefore lives in `formatMoney.ts` beside the formatter and the currency set, not in the modal — one module owns the scale, and the test above imports both halves of it from the same place.

Export `NO_DECIMAL_CURRENCIES` from `formatMoney.ts` so this function and the formatter cannot disagree about which currencies have cents.

```tsx
// The same rule domain.AccountType.SignedNetWorthAmount enforces, said where
// someone can act on it. The backend refuses a negative debt regardless (422
// INVALID_BALANCE); this exists so the person finds out while they are looking
// at the field rather than after submitting.
const debtIsNegative = LIABILITY_TYPES.has(type) && minorUnits !== null && minorUnits < 0;
```

The submit button reads `Add account` when creating and `Save` when editing. On success the modal closes; on an `ApiError` it renders `apiErrorMessage(error, …)` in a `role="alert"`, the same way `NewSpaceModal` does.

- [ ] **Step 5: Wire archive and restore into the panel**

`AccountsPanel.tsx` gains, for owners only: an edit affordance per row opening `AccountModal` populated; an archive action per live row; and, in the archived view, a restore action. Every one of them goes through `useSetAccountArchived` or `useUpdateAccount`, so the invalidation is the shared one.

Do **not** use `window.confirm` for archive — a browser modal blocks the extension used for the browser walk in Task 41, and archiving is reversible from the archived view anyway.

- [ ] **Step 6: Run the whole frontend suite**

```bash
cd web && npx vitest run && npm run typecheck
```
Expected: PASS, the existing 117 tests plus the new ones.

- [ ] **Step 7: Mutation-check the minor-units conversion**

Temporarily change `toMinorUnits` to `Math.round(parseFloat(input) * 100)`. The `"parses a figure whose cents would be wrong as a float"` test from Task 39 must fail. Restore.

Then temporarily change `allowedDecimals` to also drive the scale (`Number(whole) * 10 ** allowedDecimals + …`). The IDR round-trip test must fail — `Rp 85,400,000` typed in would come back as `Rp 854,000`. Restore.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/money
git commit -m "feat(web): the add-account modal, archive and restore

The form parses a decimal string into minor units by splitting on the decimal
point rather than multiplying a float by 100, so no floating-point value ever
enters the monetary path on this side either.

It refuses a negative debt in the field rather than only on the 422, so the
person finds out while they are still looking at what they typed."
```

---

### Task 41: Walk the definition of done, and update the four documents

**Files:**
- Create: `docs/superpowers/plans/2026-07-28-hearth-accounts-verification.md`
- Modify: `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`, `docs/HANDOVER.md`, `CLAUDE.md`

- [ ] **Step 1: Start from nothing**

```bash
make down
docker volume rm hearth_hearth-pgdata || true
make up && make seed
make dev
```

Start from `make down && make up`, not a bare `make up`: Compose only re-evaluates `depends_on: migrate` when it recreates `api`, so a stack left running across a new migration keeps its already-succeeded migrate container and never runs `00004_accounts.sql`.

- [ ] **Step 2: Walk every criterion, recording PASS or the failure as you go**

At `http://localhost:5173`, signed in as Andreas:

1. The sidebar's **Money** entry opens Finances, not a placeholder. The four sibling routes under `/money/…` still show theirs.
2. With no accounts, the page shows `Nothing here yet.` and a single add affordance — not three empty cards.
3. `+ Add account` opens the modal. Confirm there is no source picker, no Singpass card, and no connected-bank header strip.
4. Add `DBS Everyday`, type Cash & savings, owner Andreas, balance `8240.55` SGD, as-of today. Confirm the list shows `S$8,240.55` and net worth reads the same.
5. Add `Car loan`, type Loan, balance `14500`. Confirm the accounts list shows it and net worth now reads `−S$6,259.45`.
6. Edit the car loan and try to save a balance of `-14500`. Confirm it is refused with "Enter what you owe as a positive amount".
7. Add `BCA Tahapan`, type Cash & savings, balance `85400000` **IDR**. Confirm the row renders `Rp 85,400,000` with no decimals, and that net worth rose by roughly S$6,881.
8. Add an account in **USD**. Confirm net worth still shows a figure, and a line beneath it reads "1 account not included: no exchange rate for USD".
9. Switch one account's **Count toward net worth** off. Confirm net worth drops, the account keeps its bar in the breakdown, and the footnote appears.
10. Archive an account. Confirm it leaves the list, leaves net worth and leaves the breakdown. Turn on **Show archived**, confirm it is listed, restore it, confirm it returns.
11. In Settings → Currency & region, change the primary currency to **EUR**. Return to Finances and confirm **no net worth figure is shown at all** — not a zero — and the copy names EUR and the currencies it could not convert. Change it back to SGD and confirm the figure returns.
12. In Settings, switch **Money** on for Kayla. Sign in as Kayla in a private window. Confirm Finances opens, shows only accounts whose **Visible to kids** is on, shows no amounts anywhere, and shows no net worth card. Confirm there is no `+ Add account` button.
13. As Kayla, use the browser devtools network tab to `POST /api/v1/accounts` directly. Confirm `403 FORBIDDEN` — the UI hiding the button is not the enforcement.
14. Sign in as Christine, add an account owned by Kayla, then remove Kayla in Settings. Confirm the account survives and now reads **Shared**.
15. Switch **Money** off for Kayla again and confirm `GET /api/v1/accounts` answers `403`.

- [ ] **Step 3: Run the full gate**

```bash
make lint && make test
```
Expected: PASS. Paste the summary lines into the verification file.

- [ ] **Step 4: Update the four documents**

**`docs/SYSTEM_DESIGN.md`** — use the `maintaining-system-design` skill. The `accounts` table in section 6, the five routes in the section 4 route table with their guards, the first capability-gated route in the request pipeline, and Finances as a real screen in section 7.

**`docs/FEATURE_TRACKER.md`** — move *Manual account entry*, *Accounts by owner, with SGD/IDR split*, and *Assets and liabilities breakdown* to ✅. *Net worth with 12-month trend* becomes 🟡 with the gap named: the trend needs balance snapshots. Add ⬜ rows for **custom account types** and for a **warning before a primary-currency change strands every account**. Add a ✅ row for **archive and restore**, which the design does not draw. Recount the summary table by counting symbols per section rather than adjusting the totals.

**`docs/LEARNING.md`** — at minimum:

- *A sum over mixed currencies must convert before it adds.* `domain.Money.Add` refuses two currencies by design, so the wrong loop order fails only once a second currency exists — invisible to a single-currency test suite. Caught by writing the test from the design's own IDR account rather than from the happy path.
- *A route-walk matrix can pass without exercising the guard it is named after.* `TestOwnerOnlyRoutesRejectALimitedMember` would have 403'd every accounts write at `requireCapability`, never reaching `requireOwner`, because the fixture's limited member holds no `money`. A matrix proves nothing unless the caller can get past every guard except the one under test.
- *An unreachable state cannot be defended by a test.* The spec claimed an owner with Money switched off must still be refused; `domain.ValidateMembershipChange` makes that state unbuildable. Found while mapping tests to the spec, not by any test.

**`docs/HANDOVER.md`** — slice 2 is under way; `requireCapability` is now wired, so remove it from the "remaining items" list; record Transactions as the next feature and that it inherits the account visibility rule rather than inventing one.

**Correct the `BankSyncProvider` claim in three places.** It does not exist:
- `docs/HANDOVER.md:172` — "so `BankSyncProvider` exists with manual and CSV adapters behind it" is wrong on both counts.
- `docs/FEATURE_TRACKER.md:57` — "`FXRateProvider` and `BankSyncProvider` exist precisely so…" — only `FXRateProvider` exists.
- `CLAUDE.md:108` — the same sentence, as a project instruction.

Say in each that manual entry needs no port, and that one arrives when CSV import gives it a second implementation to abstract over.

- [ ] **Step 5: Commit**

```bash
git add docs CLAUDE.md
git commit -m "docs: record the accounts walk and correct the BankSyncProvider claim

Three documents said BankSyncProvider exists, including CLAUDE.md as a project
instruction. It exists in none of them; only SYSTEM_DESIGN.md was accurate.
Manual account entry needs no port, and inventing one with a single
implementation and no second caller would be the wrong shape."
```

---

## Definition of done for this plan

Every criterion in Task 41 passes, recorded in the verification file, with `make lint` and `make test` green. A household can record what it owns and owes and see a net worth built from it; the `money` capability guards a real route for the first time; a limited member sees no amounts; and an account whose currency cannot be converted is named on screen rather than silently dropped.

Transactions is the next feature. It attaches to these accounts, inherits their visibility rule rather than inventing one, and turns `AccountView.Balance` from the opening balance into a real sum.
