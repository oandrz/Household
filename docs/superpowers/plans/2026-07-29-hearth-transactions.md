# Transactions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give a household a ledger — expenses, income and transfers attached to
the accounts it already has — and turn `AccountView.Balance` from a copy of the
opening balance into a real sum.

**Architecture:** Two new tables (`categories`, `transactions`), one domain file
each, one service each, one narrow repository port each with a Postgres adapter,
five HTTP routes gated on `money` **and** owner, and a new frontend page at
`/money/transactions` plus a card on the existing Finances page. Dependencies
point inward; the accounts feature is the pattern to copy in every layer.

**Tech Stack:** Go 1.x, chi, pgx v5, sqlc, goose migrations, testcontainers;
React + TypeScript, TanStack Router + Query, Zod, Vitest + Testing Library.

**Spec:** `docs/superpowers/specs/2026-07-29-hearth-transactions-design.md`.
Every decision number referenced below is a section-2 decision in that file.

## Global Constraints

- **Clean architecture, enforced by `make lint-arch`, test files included.**
  `internal/domain` imports the standard library only. `internal/usecase` may
  add `internal/domain`. Everything else lives under `internal/adapter/**` or
  `cmd/**`. No pgx, chi or HTTP type crosses out of an adapter.
- **Money is `int64` minor units plus an ISO 4217 code, everywhere.** `float64`
  never appears in a monetary path, on either side of the stack.
- **Authorisation exists only in the HTTP layer.** No service takes an actor
  parameter. Services enforce what is *valid*; middleware enforces who is
  *asking*.
- **Every 2xx except 204 carries a JSON body** — `apiFetch` throws on an ok
  response it cannot parse. `DELETE /api/v1/transactions/{id}` is the one 204.
- **Fail closed on values you did not construct.** A `switch` over a value from
  a request or a database column needs a `default` that refuses.
- **Comments say why, never what.** Every non-obvious decision is written down
  at the point someone would try to change it.
- **Tests read as documentation.** The name states the behaviour; the body shows
  it.
- **`make lint && make test` green before any task is called done.** The Go
  suite needs Docker:
  ```bash
  export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
  export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
  ```
- **Regenerate sqlc after touching any `.sql` query file:** `make sqlc`. Never
  hand-edit anything under `internal/adapter/postgres/sqlcgen/`.
- **Frontend tests use `stubFetchRoutes`**, which matches on method *and* URL
  and throws on an unregistered request. A stub that ignores the URL has
  silently passed broken code twice in this project.
- **Commit after every task**, with the `feat:`/`test:`/`docs:` prefix the
  repository already uses.

---

## File Structure

**Created**

| Path | Responsibility |
|---|---|
| `api/migrations/00005_transactions.sql` | Both new tables and their constraints |
| `api/internal/domain/category.go` | `Category`, `CategoryKind`, the starter set |
| `api/internal/domain/category_test.go` | Starter set shape, kind parsing |
| `api/internal/domain/transaction.go` | `Transaction`, `TransactionKind`, `BalanceEffect` |
| `api/internal/domain/transaction_test.go` | Kind parsing, balance effect per side |
| `api/internal/usecase/category.go` | `CategoryService` — list, seeding on first use |
| `api/internal/usecase/category_test.go` | Seeding behaviour against a double |
| `api/internal/usecase/transaction.go` | `TransactionService` — validation, CRUD |
| `api/internal/usecase/transaction_test.go` | Validation rules |
| `api/internal/usecase/monthsummary.go` | The month's count and spend |
| `api/internal/usecase/monthsummary_test.go` | Convert-then-add, exclusions |
| `api/internal/adapter/postgres/queries/transaction.sql` | Every transaction and category query |
| `api/internal/adapter/postgres/category_repo.go` | `CategoryRepository` adapter |
| `api/internal/adapter/postgres/category_repo_test.go` | Seeding idempotence, against real Postgres |
| `api/internal/adapter/postgres/transaction_repo.go` | `TransactionRepository` adapter |
| `api/internal/adapter/postgres/transaction_repo_test.go` | Paging, filters, cascades |
| `api/internal/adapter/http/transaction_handlers.go` | Transaction DTOs and handlers |
| `api/internal/adapter/http/category_handlers.go` | The categories list |
| `web/src/features/money/transactionSchemas.ts` | Zod mirrors of the DTOs |
| `web/src/features/money/useTransactions.ts` | Query and mutation hooks |
| `web/src/features/money/TransactionModal.tsx` | The design's "Log a transaction" |
| `web/src/features/money/TransactionModal.test.tsx` | Kind switching, the received-amount field |
| `web/src/features/money/TransactionFilters.tsx` | The five filters |
| `web/src/features/money/TransactionsPage.tsx` | Header, ledger, paging |
| `web/src/features/money/TransactionsPage.test.tsx` | Screen states |
| `web/src/features/money/RecentTransactionsCard.tsx` | The Finances strip |
| `web/src/features/money/transactionCopy.ts` | Wording for the above |

**Modified**

| Path | Change |
|---|---|
| `api/internal/domain/errors.go` | Seven new sentinels |
| `api/internal/usecase/ports.go` | `CategoryRepository`, `TransactionRepository`, their views |
| `api/internal/usecase/testdouble_test.go` | In-memory doubles for both ports |
| `api/internal/adapter/postgres/queries/account.sql` | The balance sum join |
| `api/internal/adapter/postgres/account_repo.go` | `buildView` takes the summed balance |
| `api/internal/adapter/http/router.go` | Five routes, guards, `Deps` fields |
| `api/internal/adapter/http/api_test.go` | Route-walk matrices |
| `api/internal/adapter/http/errors.go` | New sentinel → code mappings |
| `api/cmd/api/main.go` | Wire the two repositories and two services |
| `web/src/routes/router.tsx` | `/money/transactions` |
| `web/src/features/money/FinancesPage.tsx` | The recent-transactions card |
| `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`, `docs/HANDOVER.md` | Kept true, in the same change |

---

## Task 1: The migration

**Files:**
- Create: `api/migrations/00005_transactions.sql`
- Test: `api/internal/adapter/postgres/schema_test.go` (add to it)

**Interfaces:**
- Consumes: the existing `accounts`, `memberships` and `households` tables.
- Produces: tables `categories` and `transactions` with the constraint names
  `accounts_match_kind`, `received_amount_pairs`,
  `received_amount_is_a_transfer_thing`, `received_amount_is_positive`,
  `transfer_has_no_category`, and the index
  `transactions_household_date_idx`. Later tasks name these exactly.

- [ ] **Step 1: Write the failing schema test**

Append to `api/internal/adapter/postgres/schema_test.go`. It proves the
constraints are armed — a `CHECK` nobody tests is a `CHECK` nobody notices
when a later migration drops it.

```go
// TestTransactionSchemaRefusesNonsenseRows pins the constraints that make a
// wrong balance unrepresentable. Each insert below is a row the service also
// refuses; the database is the second line of defence, and a second line
// nobody tests is decoration.
func TestTransactionSchemaRefusesNonsenseRows(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	var householdID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO households (name, family_name) VALUES ('Test', 'Test') RETURNING id`).
		Scan(&householdID); err != nil {
		t.Fatalf("insert household: %v", err)
	}

	newAccount := func(nickname string) string {
		var id string
		if err := db.Pool().QueryRow(ctx,
			`INSERT INTO accounts (household_id, nickname, type, opening_balance_minor,
			                       opening_balance_currency, opening_balance_as_of)
			 VALUES ($1, $2, 'cash', 0, 'SGD', DATE '2026-07-01') RETURNING id`,
			householdID, nickname).Scan(&id); err != nil {
			t.Fatalf("insert account %s: %v", nickname, err)
		}
		return id
	}
	from, to := newAccount("DBS"), newAccount("OCBC")

	// A real category, because the "a transfer carrying a category" case below
	// needs a non-NULL category_id to exercise transfer_has_no_category at
	// all. A subselect over an empty table yields NULL, the constraint is
	// satisfied, the insert succeeds -- and the subtest fails claiming the
	// database accepted something it never saw.
	var categoryID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO categories (household_id, name, kind, sort_order)
		 VALUES ($1, 'Groceries', 'expense', 1) RETURNING id`, householdID).
		Scan(&categoryID); err != nil {
		t.Fatalf("insert category: %v", err)
	}

	cases := []struct {
		name string
		sql  string
		args []any
	}{
		{
			name: "an expense with a destination account",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, to_account_id, amount_minor, amount_currency)
			      VALUES ($1, 'expense', DATE '2026-07-18', 'Cold Storage', $2, $3, 5230, 'SGD')`,
			args: []any{householdID, from, to},
		},
		{
			name: "a transfer with only one leg",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, amount_minor, amount_currency)
			      VALUES ($1, 'transfer', DATE '2026-07-18', 'To BCA', $2, 50000, 'SGD')`,
			args: []any{householdID, from},
		},
		{
			name: "a transfer from an account to itself",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, to_account_id, amount_minor, amount_currency)
			      VALUES ($1, 'transfer', DATE '2026-07-18', 'Round trip', $2, $2, 50000, 'SGD')`,
			args: []any{householdID, from},
		},
		{
			name: "a received amount with no currency",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, to_account_id, amount_minor, amount_currency,
			          received_amount_minor)
			      VALUES ($1, 'transfer', DATE '2026-07-18', 'To BCA', $2, $3, 50000, 'SGD', 49800)`,
			args: []any{householdID, from, to},
		},
		{
			name: "a received amount on an expense",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, amount_minor, amount_currency,
			          received_amount_minor, received_amount_currency)
			      VALUES ($1, 'expense', DATE '2026-07-18', 'Cold Storage', $2, 5230, 'SGD', 5230, 'SGD')`,
			args: []any{householdID, from},
		},
		{
			name: "a transfer carrying a category",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, to_account_id, amount_minor, amount_currency, category_id)
			      VALUES ($1, 'transfer', DATE '2026-07-18', 'To BCA', $2, $3, 50000, 'SGD', $4)`,
			args: []any{householdID, from, to, categoryID},
		},
		{
			name: "a zero amount",
			sql: `INSERT INTO transactions (household_id, kind, occurred_on, description,
			          from_account_id, amount_minor, amount_currency)
			      VALUES ($1, 'expense', DATE '2026-07-18', 'Free', $2, 0, 'SGD')`,
			args: []any{householdID, from},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.Pool().Exec(ctx, tc.sql, tc.args...); err == nil {
				t.Fatal("the database accepted it")
			}
		})
	}
}

// TestCategoryNamesAreUniquePerHousehold pins the constraint EnsureSeeded's
// ON CONFLICT depends on. Without it the seed is not idempotent and two
// simultaneous first requests produce two starter sets.
func TestCategoryNamesAreUniquePerHousehold(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	var householdID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO households (name, family_name) VALUES ('Test', 'Test') RETURNING id`).
		Scan(&householdID); err != nil {
		t.Fatalf("insert household: %v", err)
	}
	insert := `INSERT INTO categories (household_id, name, kind, sort_order)
	           VALUES ($1, 'Groceries', 'expense', 1)`
	if _, err := db.Pool().Exec(ctx, insert, householdID); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, insert, householdID); err == nil {
		t.Fatal("the database accepted a duplicate category name")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/adapter/postgres/ -run 'TestTransactionSchema|TestCategoryNames' -v
```

Expected: FAIL — `relation "transactions" does not exist`.

- [ ] **Step 3: Write the migration**

```bash
make migrate-new NAME=transactions
```

That creates `api/migrations/00005_transactions.sql`. Replace its body with the
following. The comments are part of the deliverable: this file is where someone
will try to change these rules.

```sql
-- +goose Up

-- categories is the household's own spending taxonomy. It ships with
-- Transactions rather than with Budget because the design's own
-- "Log a transaction" modal has a Category dropdown, and Budget's envelopes
-- are sums over these rows.
--
-- A household's set is created the first time anything reads it (see
-- CategoryService), not at household creation: seeding here would reach into
-- SignupRepository.Provision, the transaction a stranger's sign-up depends
-- on, for a feature that does not need it. See the spec's decision 1.
CREATE TABLE categories (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name         text        NOT NULL,
    kind         text        NOT NULL CHECK (kind IN ('expense', 'income')),
    -- The order the design draws, not alphabetical: sorting by name would put
    -- "Dining out" above "Groceries" for no reason a household would recognise.
    sort_order   integer     NOT NULL,
    -- Budget's "Edit categories" screen archives rather than deletes, so a
    -- category that transactions already reference keeps its name. It also
    -- keeps its unique key, which is what stops a household that cleared its
    -- list from being silently re-seeded.
    archived_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- Load-bearing, not a nicety: this is the conflict target that makes
    -- EnsureSeeded idempotent under two simultaneous first requests.
    UNIQUE (household_id, name)
);

-- transactions is what happened to a household's accounts.
--
-- One row per event, including a transfer, which carries both of its accounts.
-- Two mirrored legs would keep the balance sum uniform and were rejected: two
-- rows that must always be written, edited and deleted together is precisely
-- the partial-write shape four defects in this project have had. One row
-- cannot half-exist. See the spec's decision 2.
CREATE TABLE transactions (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id             uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    kind                     text        NOT NULL
                                         CHECK (kind IN ('expense', 'income', 'transfer')),
    -- A date, not a timestamptz: "18 July" is a fact about a day. This product
    -- stores no timezone for a household, so an instant would mean different
    -- days to the server and to the person who typed it.
    occurred_on              date        NOT NULL,
    description              text        NOT NULL,
    -- ON DELETE SET NULL on both references, for the reason accounts uses it
    -- for a removed member's accounts: losing a label is the least valuable
    -- thing on the row, and refusing the deletion would mean an owner cannot
    -- remove a departed member without first reassigning every transaction
    -- they ever paid for.
    category_id              uuid        REFERENCES categories(id) ON DELETE SET NULL,
    paid_by_membership_id    uuid        REFERENCES memberships(id) ON DELETE SET NULL,
    -- ON DELETE CASCADE, and RESTRICT would be wrong. The application never
    -- deletes an account -- accounts archive -- so this clause is unreachable
    -- in ordinary use. It fires in exactly one case: deleting a household
    -- cascades to its accounts, and a RESTRICT here would make that cascade
    -- fail with a foreign key violation.
    from_account_id          uuid        REFERENCES accounts(id) ON DELETE CASCADE,
    to_account_id            uuid        REFERENCES accounts(id) ON DELETE CASCADE,
    -- Stored positive; whether it adds or subtracts comes from kind and from
    -- which side of the row the account sits on. Letting someone type a
    -- negative expense makes "I typed -52.30 for groceries and my balance went
    -- up" representable. See the spec's decision 9.
    amount_minor             bigint      NOT NULL CHECK (amount_minor > 0),
    amount_currency          char(3)     NOT NULL,
    -- What actually arrived in the destination account, in that account's own
    -- currency: required when a transfer crosses currencies, optional when it
    -- does not, so a bank fee on a same-currency transfer is recordable. NULL
    -- means "the same figure that left". See the spec's decision 3.
    received_amount_minor    bigint,
    received_amount_currency char(3),
    created_at               timestamptz NOT NULL DEFAULT now(),

    -- The constraint that makes a nonsense row impossible: an expense with a
    -- destination, a transfer with one leg, a transfer from an account to
    -- itself. Every one of those produces a balance that is wrong with nothing
    -- on screen to explain it.
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
    -- The Transactions screen's own banner says a category feeds Budget spend.
    -- A transfer is not spend, so it cannot carry one.
    CONSTRAINT transfer_has_no_category CHECK (
        kind <> 'transfer' OR category_id IS NULL
    )
);

-- Column order is the sort order the ledger reads in, so the keyset cursor in
-- ListTransactions can walk this index rather than sorting a heap.
CREATE INDEX transactions_household_date_idx
    ON transactions (household_id, occurred_on DESC, id DESC);

-- The accounts-balance sum filters by each side; without these it degrades to
-- a sequential scan of every transaction in the database per account listed.
CREATE INDEX transactions_from_account_idx ON transactions (from_account_id);
CREATE INDEX transactions_to_account_idx   ON transactions (to_account_id);

-- +goose Down
DROP TABLE transactions;
DROP TABLE categories;
```

- [ ] **Step 4: Apply it and run the test**

```bash
make migrate
cd api && go test ./internal/adapter/postgres/ -run 'TestTransactionSchema|TestCategoryNames' -v
```

Expected: PASS, every subtest.

- [ ] **Step 5: Mutation-check one constraint**

Temporarily delete the `AND from_account_id <> to_account_id` clause from
`accounts_match_kind`, re-run `make migrate-down && make migrate`, and confirm
the "a transfer from an account to itself" subtest goes **red**. Restore the
clause, re-migrate, confirm green. A constraint test that passes against a
missing constraint is not protecting anything.

- [ ] **Step 6: Commit**

```bash
git add api/migrations/00005_transactions.sql api/internal/adapter/postgres/schema_test.go
git commit -m "feat(db): add categories and transactions tables

The account foreign keys cascade rather than restrict: the application never
deletes an account, so the clause fires only when a household is deleted and
the cascade reaches its accounts -- where RESTRICT would fail."
```

---

## Task 2: The category domain type

**Files:**
- Create: `api/internal/domain/category.go`, `api/internal/domain/category_test.go`
- Modify: `api/internal/domain/errors.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type CategoryKind string` with `CategoryExpense`, `CategoryIncome`
  - `func ParseCategoryKind(s string) (CategoryKind, error)`
  - `type Category struct { ID, HouseholdID, Name string; Kind CategoryKind; SortOrder int; ArchivedAt *time.Time }`
  - `func (c Category) IsArchived() bool`
  - `func StarterCategories() []Category` — 13 entries, `SortOrder` 1..13
  - `var ErrUnknownCategoryKind error`

- [ ] **Step 1: Write the failing test**

`api/internal/domain/category_test.go`:

```go
package domain_test

import (
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestParseCategoryKindRefusesAnythingElse(t *testing.T) {
	for _, in := range []string{"", "Expense", "spending", "transfer"} {
		if _, err := domain.ParseCategoryKind(in); !errors.Is(err, domain.ErrUnknownCategoryKind) {
			t.Fatalf("ParseCategoryKind(%q) = %v, want ErrUnknownCategoryKind", in, err)
		}
	}
	for _, in := range []domain.CategoryKind{domain.CategoryExpense, domain.CategoryIncome} {
		got, err := domain.ParseCategoryKind(string(in))
		if err != nil || got != in {
			t.Fatalf("ParseCategoryKind(%q) = %q, %v", in, got, err)
		}
	}
}

// The starter set is the design's own Budget screen. It is asserted here
// rather than in the service so that changing it is a deliberate edit to a
// test, not a silent edit to a slice of strings.
func TestStarterCategoriesAreTheDesignsOwnList(t *testing.T) {
	starter := domain.StarterCategories()

	wantNames := []string{
		"Groceries", "Dining out", "Transport", "Petrol", "Household",
		"Kids & school", "Health", "Utilities", "Insurance", "Subscriptions",
		"Fun & hobbies", "Giving", "Income",
	}
	if len(starter) != len(wantNames) {
		t.Fatalf("starter set has %d categories, want %d", len(starter), len(wantNames))
	}
	for i, want := range wantNames {
		if starter[i].Name != want {
			t.Fatalf("starter[%d].Name = %q, want %q", i, starter[i].Name, want)
		}
		if starter[i].SortOrder != i+1 {
			t.Fatalf("starter[%d].SortOrder = %d, want %d", i, starter[i].SortOrder, i+1)
		}
	}

	// Exactly one income category. An income transaction with nothing to pick
	// would be a dead end in the modal.
	income := 0
	for _, c := range starter {
		if c.Kind == domain.CategoryIncome {
			income++
		}
	}
	if income != 1 {
		t.Fatalf("starter set has %d income categories, want exactly 1", income)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/domain/ -run TestParseCategoryKind -v
```

Expected: FAIL — `undefined: domain.ParseCategoryKind`.

- [ ] **Step 3: Write the implementation**

`api/internal/domain/category.go`:

```go
package domain

import (
	"fmt"
	"time"
)

// CategoryKind splits what a household spends from what it receives, so the
// "Log a transaction" modal can offer Groceries for an expense and never for
// an income.
type CategoryKind string

const (
	CategoryExpense CategoryKind = "expense"
	CategoryIncome  CategoryKind = "income"
)

// ParseCategoryKind refuses anything it does not recognise. The default is the
// point: a kind arrives from a database column, so it is a value this code did
// not construct, and guessing at an unknown one would offer a spending
// category for income.
func ParseCategoryKind(s string) (CategoryKind, error) {
	switch CategoryKind(s) {
	case CategoryExpense:
		return CategoryExpense, nil
	case CategoryIncome:
		return CategoryIncome, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownCategoryKind, s)
	}
}

// Category is one line of a household's spending taxonomy. Budget's envelopes
// are sums over the transactions that reference these rows.
type Category struct {
	ID          string
	HouseholdID string
	Name        string
	Kind        CategoryKind
	SortOrder   int
	ArchivedAt  *time.Time
}

// IsArchived reports whether Budget's "Edit categories" has retired this one.
// An archived category keeps its row so transactions that reference it keep
// their name, and keeps its unique key so the starter set is not re-seeded
// over it.
func (c Category) IsArchived() bool { return c.ArchivedAt != nil }

// StarterCategories is what a household gets the first time anything reads its
// categories. It is the design's own Budget screen list, in the design's own
// order -- which is why SortOrder is explicit rather than alphabetical.
//
// It lives in the domain rather than in CategoryService so that the seeding
// path and any future template share exactly one definition. Adding to it is
// safe for existing households: EnsureSeeded only fires for a household that
// has none, so a new entry reaches new households and leaves everyone else's
// list as they have arranged it.
func StarterCategories() []Category {
	names := []struct {
		name string
		kind CategoryKind
	}{
		{"Groceries", CategoryExpense},
		{"Dining out", CategoryExpense},
		{"Transport", CategoryExpense},
		{"Petrol", CategoryExpense},
		{"Household", CategoryExpense},
		{"Kids & school", CategoryExpense},
		{"Health", CategoryExpense},
		{"Utilities", CategoryExpense},
		{"Insurance", CategoryExpense},
		{"Subscriptions", CategoryExpense},
		{"Fun & hobbies", CategoryExpense},
		{"Giving", CategoryExpense},
		{"Income", CategoryIncome},
	}
	out := make([]Category, 0, len(names))
	for i, n := range names {
		out = append(out, Category{Name: n.name, Kind: n.kind, SortOrder: i + 1})
	}
	return out
}
```

Add to `api/internal/domain/errors.go`, beside the existing sentinels:

```go
// ErrUnknownCategoryKind is returned for a category kind this code did not
// construct -- a database column holding something other than expense or
// income.
var ErrUnknownCategoryKind = errors.New("unknown category kind")
```

- [ ] **Step 4: Run the tests**

```bash
cd api && go test ./internal/domain/ -run 'TestParseCategoryKind|TestStarterCategories' -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/internal/domain/category.go api/internal/domain/category_test.go api/internal/domain/errors.go
git commit -m "feat(domain): add Category and the starter set

The starter set lives in the domain, not the service, so the seeding path and
any future template share one definition."
```

---

## Task 3: The transaction domain type

**Files:**
- Create: `api/internal/domain/transaction.go`, `api/internal/domain/transaction_test.go`
- Modify: `api/internal/domain/errors.go`

**Interfaces:**
- Consumes: `domain.Money`, `domain.ErrAmountOverflow` (both exist).
- Produces:
  - `type TransactionKind string` with `TransactionExpense`, `TransactionIncome`, `TransactionTransfer`
  - `func ParseTransactionKind(s string) (TransactionKind, error)`
  - `type Transaction struct { ID, HouseholdID string; Kind TransactionKind; OccurredOn time.Time; Description string; CategoryID, PaidByMembershipID, FromAccountID, ToAccountID string; Amount Money; ReceivedAmount *Money }`
  - `func (t Transaction) BalanceEffect(accountID string) (Money, bool)`
  - `func (t Transaction) CreditedAmount() Money`
  - sentinels `ErrUnknownTransactionKind`, `ErrTransactionDescriptionRequired`,
    `ErrTransactionAmountNotPositive`, `ErrTransactionAccountsInvalid`,
    `ErrReceivedAmountRequired`, `ErrReceivedAmountNotAllowed`,
    `ErrCategoryKindMismatch`

- [ ] **Step 1: Write the failing test**

`api/internal/domain/transaction_test.go`:

```go
package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestParseTransactionKindRefusesAnythingElse(t *testing.T) {
	for _, in := range []string{"", "Expense", "spend", "withdrawal"} {
		if _, err := domain.ParseTransactionKind(in); !errors.Is(err, domain.ErrUnknownTransactionKind) {
			t.Fatalf("ParseTransactionKind(%q) = %v, want ErrUnknownTransactionKind", in, err)
		}
	}
	for _, in := range []domain.TransactionKind{
		domain.TransactionExpense, domain.TransactionIncome, domain.TransactionTransfer,
	} {
		got, err := domain.ParseTransactionKind(string(in))
		if err != nil || got != in {
			t.Fatalf("ParseTransactionKind(%q) = %q, %v", in, got, err)
		}
	}
}

func sgd(minor int64) domain.Money { return domain.Money{Amount: minor, Currency: "SGD"} }
func idr(minor int64) domain.Money { return domain.Money{Amount: minor, Currency: "IDR"} }

func TestBalanceEffectSubtractsFromTheSourceAndAddsToTheDestination(t *testing.T) {
	day := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)

	expense := domain.Transaction{
		Kind: domain.TransactionExpense, OccurredOn: day,
		FromAccountID: "dbs", Amount: sgd(5230),
	}
	got, ok := expense.BalanceEffect("dbs")
	if !ok || got != sgd(-5230) {
		t.Fatalf("expense on its own account = %v, %v; want -5230 SGD", got, ok)
	}
	if _, ok := expense.BalanceEffect("ocbc"); ok {
		t.Fatal("an expense reported an effect on an account it does not touch")
	}

	income := domain.Transaction{
		Kind: domain.TransactionIncome, OccurredOn: day,
		ToAccountID: "dbs", Amount: sgd(640000),
	}
	if got, ok := income.BalanceEffect("dbs"); !ok || got != sgd(640000) {
		t.Fatalf("income = %v, %v; want +640000 SGD", got, ok)
	}
}

// The defect this prevents: crediting the destination with the amount that
// left rather than the amount that arrived adds Singapore dollars to a rupiah
// balance, and the account ends up wrong by a factor of ten thousand.
func TestBalanceEffectCreditsTheReceivedAmountOnACrossCurrencyTransfer(t *testing.T) {
	received := idr(620000000)
	transfer := domain.Transaction{
		Kind:          domain.TransactionTransfer,
		OccurredOn:    time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
		FromAccountID: "dbs", ToAccountID: "bca",
		Amount: sgd(50000), ReceivedAmount: &received,
	}

	if got, ok := transfer.BalanceEffect("dbs"); !ok || got != sgd(-50000) {
		t.Fatalf("source side = %v, %v; want -50000 SGD", got, ok)
	}
	if got, ok := transfer.BalanceEffect("bca"); !ok || got != idr(620000000) {
		t.Fatalf("destination side = %v, %v; want +620000000 IDR", got, ok)
	}
}

// A same-currency transfer may still carry a received amount -- a bank fee.
// When it does not, what arrives is what left.
func TestBalanceEffectFallsBackToTheAmountSentWhenNothingElseWasRecorded(t *testing.T) {
	transfer := domain.Transaction{
		Kind:          domain.TransactionTransfer,
		OccurredOn:    time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
		FromAccountID: "dbs", ToAccountID: "ocbc", Amount: sgd(50000),
	}
	if got, ok := transfer.BalanceEffect("ocbc"); !ok || got != sgd(50000) {
		t.Fatalf("destination side = %v, %v; want +50000 SGD", got, ok)
	}

	fee := sgd(49800)
	withFee := transfer
	withFee.ReceivedAmount = &fee
	if got, ok := withFee.BalanceEffect("ocbc"); !ok || got != sgd(49800) {
		t.Fatalf("destination side with a fee = %v, %v; want +49800 SGD", got, ok)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/domain/ -run 'TestParseTransactionKind|TestBalanceEffect' -v
```

Expected: FAIL — `undefined: domain.ParseTransactionKind`.

- [ ] **Step 3: Write the implementation**

`api/internal/domain/transaction.go`:

```go
package domain

import (
	"fmt"
	"math"
	"time"
)

// TransactionKind is what a transaction did: money left an account, money
// arrived in one, or money moved between two of them.
type TransactionKind string

const (
	TransactionExpense  TransactionKind = "expense"
	TransactionIncome   TransactionKind = "income"
	TransactionTransfer TransactionKind = "transfer"
)

// ParseTransactionKind refuses anything it does not recognise. The default is
// the point: a kind arrives from a request body or a database column, so it is
// a value this code did not construct, and guessing at an unknown one would
// put money on the wrong side of an account.
func ParseTransactionKind(s string) (TransactionKind, error) {
	switch TransactionKind(s) {
	case TransactionExpense:
		return TransactionExpense, nil
	case TransactionIncome:
		return TransactionIncome, nil
	case TransactionTransfer:
		return TransactionTransfer, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownTransactionKind, s)
	}
}

// Transaction is one thing that happened to a household's accounts.
//
// The four id fields follow the same "" <-> SQL NULL convention as
// Account.OwnerMembershipID: "" means the column is NULL. An expense has no
// ToAccountID, an income has no FromAccountID, and a transfer has both -- the
// accounts_match_kind constraint enforces that combination at the database
// too, because a row that breaks it produces a balance that is wrong with
// nothing on screen to explain it.
//
// ReceivedAmount is what landed in the destination account, in that account's
// own currency. It is nil when nothing but the amount sent is known, which is
// the ordinary same-currency case. It is required for a transfer whose two
// accounts differ in currency, and permitted for one whose accounts match so
// that a bank fee is recordable.
type Transaction struct {
	ID                 string
	HouseholdID        string
	Kind               TransactionKind
	OccurredOn         time.Time
	Description        string
	CategoryID         string
	PaidByMembershipID string
	FromAccountID      string
	ToAccountID        string
	Amount             Money
	ReceivedAmount     *Money
}

// CreditedAmount is what arrives in the destination account: the received
// amount when one was recorded, and otherwise the amount that left. One
// function so the balance sum, the ledger and any later reader cannot disagree
// about which figure the destination gets.
func (t Transaction) CreditedAmount() Money {
	if t.ReceivedAmount != nil {
		return *t.ReceivedAmount
	}
	return t.Amount
}

// BalanceEffect reports what this transaction does to the named account's
// balance, and whether it touches that account at all.
//
// A transfer supplies both of its effects from this one row, which is why a
// transfer cannot change net worth: the two sides are the same money, and
// there is no second row that could go missing. That invariant is a property
// of the shape rather than a rule someone has to remember.
//
// It returns ok=false rather than a zero for an account it does not touch,
// because zero is a real effect -- a caller must be able to tell "this
// transaction moved nothing here" from "this transaction is not about this
// account at all".
func (t Transaction) BalanceEffect(accountID string) (Money, bool) {
	if accountID == "" {
		return Money{}, false
	}
	switch {
	case accountID == t.FromAccountID:
		// math.MinInt64 has no positive counterpart in two's complement, so a
		// naive negation would turn the largest possible outflow into an
		// inflow. The amount is constrained positive at the database and in
		// the service, so this is unreachable -- and it is guarded anyway, for
		// the same reason AccountType.SignedNetWorthAmount guards it.
		if t.Amount.Amount == math.MinInt64 {
			return Money{}, false
		}
		return Money{Amount: -t.Amount.Amount, Currency: t.Amount.Currency}, true
	case accountID == t.ToAccountID:
		return t.CreditedAmount(), true
	default:
		return Money{}, false
	}
}
```

Add to `api/internal/domain/errors.go`:

```go
// The transaction sentinels. Each maps to a 422 with a field-specific code in
// the HTTP layer (see errors.go there); none of them is an internal failure.
var (
	ErrUnknownTransactionKind        = errors.New("unknown transaction kind")
	ErrTransactionDescriptionRequired = errors.New("transaction description is required")
	ErrTransactionAmountNotPositive  = errors.New("transaction amount must be positive")
	// ErrTransactionAccountsInvalid covers every wrong combination of the two
	// account fields: an expense with a destination, a transfer with one leg,
	// a transfer from an account to itself, or an account in another
	// household. They are one sentinel because the screen shows one message
	// next to the account pickers, and splitting them would tell an attacker
	// which ids exist elsewhere.
	ErrTransactionAccountsInvalid = errors.New("transaction accounts are not valid for its kind")
	ErrReceivedAmountRequired     = errors.New("a cross-currency transfer needs the amount received")
	ErrReceivedAmountNotAllowed   = errors.New("only a transfer can record an amount received")
	ErrCategoryKindMismatch       = errors.New("category does not match the transaction kind")
)
```

- [ ] **Step 4: Run the tests**

```bash
cd api && go test ./internal/domain/ -v
```

Expected: PASS, including the existing domain tests.

- [ ] **Step 5: Mutation-check the cross-currency case**

In `CreditedAmount`, temporarily `return t.Amount` unconditionally. Re-run
`TestBalanceEffectCreditsTheReceivedAmountOnACrossCurrencyTransfer` and confirm
it goes **red**. Restore. This is the test standing between the product and an
account wrong by a factor of ten thousand — it must be able to fail.

- [ ] **Step 6: Commit**

```bash
git add api/internal/domain/transaction.go api/internal/domain/transaction_test.go api/internal/domain/errors.go
git commit -m "feat(domain): add Transaction and its balance effect

One row supplies both sides of a transfer, which is what makes 'a transfer
cannot change net worth' a property of the shape rather than a rule to
remember."
```

---

## Task 4: The two ports

**Files:**
- Modify: `api/internal/usecase/ports.go`

**Interfaces:**
- Consumes: `domain.Category`, `domain.Transaction` (Tasks 2 and 3).
- Produces, for every later task to code against:
  - `type TransactionView struct { Transaction domain.Transaction; CategoryName, PaidByName, FromAccountName, ToAccountName string; BeforeFromAccountOpening, BeforeToAccountOpening *bool }`
  - `type TransactionFilter struct { Kind, AccountID, CategoryID, PaidByMembershipID string; Month time.Time; CursorDate time.Time; CursorID string; Limit int }`
  - `type CategoryRepository interface { List(...); EnsureSeeded(...) }`
  - `type TransactionRepository interface { List, Get, Create, Update, Delete, MonthTotals }`

- [ ] **Step 1: Write the port declarations**

There is no test step for this task: a port is a declaration, and the tests
that prove it are the adapter's (Tasks 5, 7, 8) and the service's (Tasks 6,
10, 11). This task exists on its own because every later task needs these exact
names, and a reviewer can reject the shape before six files depend on it.

Append to `api/internal/usecase/ports.go`, after the account section:

```go
// TransactionView is a transaction joined to the names the ledger displays --
// its category, who paid, and each account's nickname. Same shape and same
// reason as MemberView and AccountView above: every consumer of the list wants
// the names, and re-reading them per row is a query per row.
//
// The two Before...Opening fields answer whether this transaction predates the
// opening-balance date of the account on that side, and so does not move that
// account's balance. Each is nil when there is no account on that side.
//
// It is two fields rather than one because a transfer has two accounts with
// two different opening dates: it can predate one and not the other, moving
// one balance and leaving the other alone. A single flag would mark such a row
// with a note that is half true. The server answers this rather than the
// frontend recomputing it, so the rule lives in exactly one place.
type TransactionView struct {
	Transaction     domain.Transaction
	CategoryName    string
	PaidByName      string
	FromAccountName string
	ToAccountName   string

	BeforeFromAccountOpening *bool
	BeforeToAccountOpening   *bool
}

// TransactionFilter is the design's five filters plus paging. An empty field
// means no filtering on it, following the same "" <-> unset convention the
// rest of this file uses.
//
// AccountID matches a transaction on *either* side. A filter that only matched
// from_account_id would hide money arriving in the account someone selected,
// which is half of what they were looking for.
//
// Paging is keyset, not offset: CursorDate and CursorID are the last row of
// the previous page, and the query asks for rows ordered after that pair.
// Offset paging shifts every later row by one when a transaction is added
// mid-scroll, so a page boundary silently repeats or skips a transaction.
type TransactionFilter struct {
	Kind               string
	AccountID          string
	CategoryID         string
	PaidByMembershipID string
	// Month is any instant inside the calendar month to list. Zero means every
	// month.
	Month time.Time

	CursorDate time.Time
	CursorID   string
	Limit      int
}

type CategoryRepository interface {
	// List returns one household's categories in sort_order. Archived
	// categories are included only when includeArchived is true.
	List(ctx context.Context, householdID string, includeArchived bool) ([]domain.Category, error)
	// EnsureSeeded creates the starter set for a household that has none.
	//
	// It is idempotent and safe to run concurrently: one INSERT ... ON
	// CONFLICT DO NOTHING against UNIQUE (household_id, name), never a
	// read-then-write, which would race two simultaneous first requests into
	// two starter sets.
	//
	// An archived category still occupies its unique key, so a household that
	// cleared its list is not silently re-seeded over.
	EnsureSeeded(ctx context.Context, householdID string, starter []domain.Category) error
}

type TransactionRepository interface {
	// List returns one household's transactions, newest first, matching every
	// filter that is set. It returns at most f.Limit+1 rows so the caller can
	// tell whether another page exists without a second query.
	List(ctx context.Context, householdID string, f TransactionFilter) ([]TransactionView, error)
	// Get reports domain.ErrNotFound when no transaction with this id exists
	// in this household -- including when one exists in a different household,
	// which must be indistinguishable from not existing at all.
	Get(ctx context.Context, householdID, transactionID string) (TransactionView, error)
	// Create writes the "" <-> SQL NULL convention for every optional id:
	// category, payer, and whichever account side the kind leaves empty.
	// t.ID is ignored -- the database assigns it.
	Create(ctx context.Context, t domain.Transaction) (domain.Transaction, error)
	// Update replaces every mutable column. TransactionService is what turns a
	// partial PATCH into a complete Transaction; this port never merges.
	Update(ctx context.Context, t domain.Transaction) (domain.Transaction, error)
	// Delete removes the row, and reports domain.ErrNotFound when there was
	// none to remove. Nothing references a transaction, so nothing is
	// orphaned -- which is why this differs from accounts, where SetArchived
	// exists and no delete does.
	Delete(ctx context.Context, householdID, transactionID string) error
	// MonthTotals returns every transaction in one calendar month, which the
	// service converts and sums.
	//
	// It returns rows rather than a SQL SUM deliberately, and the bound is one
	// household's transactions in one month -- the design's own busiest
	// example is 247. A SQL SUM would be correct only for a household whose
	// transactions are all in its primary currency; having two code paths
	// whose answers could disagree is the trade this refuses. The FX provider
	// lives in this layer, so the conversion cannot move down here anyway.
	MonthTotals(ctx context.Context, householdID string, month time.Time) ([]TransactionView, error)
}
```

- [ ] **Step 2: Confirm it compiles and the architecture lint still passes**

```bash
cd api && go build ./... && cd .. && make lint-arch
```

Expected: both clean. `ports.go` gains no import beyond `time`, which it
already has.

- [ ] **Step 3: Commit**

```bash
git add api/internal/usecase/ports.go
git commit -m "feat(usecase): declare the category and transaction ports

TransactionView carries two before-opening flags, not one: a transfer has two
accounts with two opening dates and can predate one without predating the
other."
```

---

## Task 5: The category adapter and its seeding

**Files:**
- Create: `api/internal/adapter/postgres/queries/transaction.sql` (category
  queries only for now), `api/internal/adapter/postgres/category_repo.go`,
  `api/internal/adapter/postgres/category_repo_test.go`

**Interfaces:**
- Consumes: `usecase.CategoryRepository` (Task 4), `domain.StarterCategories` (Task 2).
- Produces: `postgres.NewCategoryRepo(db *DB) *CategoryRepo`, satisfying
  `usecase.CategoryRepository`.

- [ ] **Step 1: Write the failing test**

`api/internal/adapter/postgres/category_repo_test.go`:

```go
package postgres_test

import (
	"context"
	"sync"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// The seed is the one place in this product where a read writes, so its
// idempotence is not a nicety -- every categories request runs it.
func TestEnsureSeededIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewCategoryRepo(db)
	householdID := insertHousehold(t, db)

	for i := 0; i < 3; i++ {
		if err := repo.EnsureSeeded(ctx, householdID, domain.StarterCategories()); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	got, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != len(domain.StarterCategories()) {
		t.Fatalf("got %d categories after three seeds, want %d",
			len(got), len(domain.StarterCategories()))
	}
	// Sort order, not insertion order or alphabetical.
	if got[0].Name != "Groceries" {
		t.Fatalf("first category is %q, want Groceries", got[0].Name)
	}
}

// Two simultaneous first requests are the case ON CONFLICT exists for. A
// read-then-write seed passes the test above and fails this one.
func TestEnsureSeededSurvivesConcurrentFirstRequests(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewCategoryRepo(db)
	householdID := insertHousehold(t, db)

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = repo.EnsureSeeded(ctx, householdID, domain.StarterCategories())
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent seed %d: %v", i, err)
		}
	}

	got, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != len(domain.StarterCategories()) {
		t.Fatalf("got %d categories after eight concurrent seeds, want %d",
			len(got), len(domain.StarterCategories()))
	}
}

// A household that cleared its list has arranged it deliberately. An archived
// row keeps its unique key, which is what stops the seed rebuilding over it.
func TestEnsureSeededDoesNotRebuildOverArchivedCategories(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewCategoryRepo(db)
	householdID := insertHousehold(t, db)

	if err := repo.EnsureSeeded(ctx, householdID, domain.StarterCategories()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`UPDATE categories SET archived_at = now() WHERE household_id = $1`, householdID); err != nil {
		t.Fatalf("archive all: %v", err)
	}
	if err := repo.EnsureSeeded(ctx, householdID, domain.StarterCategories()); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	live, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list live: %v", err)
	}
	if len(live) != 0 {
		t.Fatalf("the seed rebuilt %d categories over an archived list", len(live))
	}
	all, err := repo.List(ctx, householdID, true)
	if err != nil {
		t.Fatalf("list including archived: %v", err)
	}
	if len(all) != len(domain.StarterCategories()) {
		t.Fatalf("got %d rows including archived, want %d",
			len(all), len(domain.StarterCategories()))
	}
}
```

Add the helper `insertHousehold` to `api/internal/adapter/postgres/repos_test.go`
if it is not already there (check first — the accounts tests may already have
one under a different name; reuse rather than duplicate):

```go
// insertHousehold is the minimum a repository test needs to have somewhere to
// put rows. It deliberately does not go through HouseholdRepo: a repository
// test that sets its fixtures up through another repository fails for two
// reasons at once.
func insertHousehold(t *testing.T, db *postgres.DB) string {
	t.Helper()
	var id string
	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO households (name, family_name) VALUES ('Test', 'Test') RETURNING id`).Scan(&id)
	if err != nil {
		t.Fatalf("insert household: %v", err)
	}
	return id
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestEnsureSeeded -v
```

Expected: FAIL — `undefined: postgres.NewCategoryRepo`.

- [ ] **Step 3: Write the queries**

Create `api/internal/adapter/postgres/queries/transaction.sql` with the category
half. The transaction queries join it in Tasks 7 and 8.

```sql
-- SeedCategories is one statement, not a read-then-write, and that is the
-- whole point: two simultaneous first requests would both read "no categories"
-- and both insert. ON CONFLICT DO NOTHING against UNIQUE (household_id, name)
-- makes the loser of that race a no-op instead of a duplicate-key error.
--
-- unnest turns four parallel arrays into rows, so the thirteen starter
-- categories are one round trip rather than thirteen.
-- name: SeedCategories :exec
INSERT INTO categories (household_id, name, kind, sort_order)
SELECT $1, name, kind, sort_order
FROM unnest($2::text[], $3::text[], $4::integer[]) AS t(name, kind, sort_order)
ON CONFLICT (household_id, name) DO NOTHING;

-- name: CountCategories :one
SELECT count(*) FROM categories WHERE household_id = $1;

-- name: ListCategories :many
SELECT id, household_id, name, kind, sort_order, archived_at, created_at
FROM categories
WHERE household_id = $1 AND archived_at IS NULL
ORDER BY sort_order;

-- name: ListCategoriesIncludingArchived :many
SELECT id, household_id, name, kind, sort_order, archived_at, created_at
FROM categories
WHERE household_id = $1
ORDER BY sort_order;

-- CategoryBelongsToHousehold answers whether a category is in this household,
-- so a transaction can never reference one from another. It mirrors
-- MembershipBelongsToHousehold, and exists for the same reason: the check must
-- not be a Get that leaks whether the id exists elsewhere.
-- name: CategoryBelongsToHousehold :one
SELECT EXISTS (
    SELECT 1 FROM categories
    WHERE id = $1 AND household_id = $2 AND archived_at IS NULL
);

-- name: GetCategoryKind :one
SELECT kind FROM categories WHERE id = $1 AND household_id = $2;
```

Regenerate, then **read the struct sqlc produced before writing any adapter
code**:

```bash
make sqlc
grep -n "SeedCategoriesParams" -A 8 api/internal/adapter/postgres/sqlcgen/transaction.sql.go
```

`SeedCategories` mixes a named `$1` with three `unnest` arrays, and sqlc names
array parameters positionally in a way that depends on how it resolves that
mix. The adapter below assumes `Column2`/`Column3`/`Column4`; use whatever the
generated struct actually declares. If sqlc rejects the query outright, replace
the `unnest` with thirteen ordinary `VALUES` rows carrying the same `ON
CONFLICT (household_id, name) DO NOTHING` — one round trip either way, and the
conflict clause is the part that matters.

- [ ] **Step 4: Write the adapter**

`api/internal/adapter/postgres/category_repo.go`:

```go
package postgres

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

type CategoryRepo struct{ q *sqlcgen.Queries }

func NewCategoryRepo(db *DB) *CategoryRepo {
	return &CategoryRepo{q: sqlcgen.New(db.Pool())}
}

func (r *CategoryRepo) List(ctx context.Context, householdID string, includeArchived bool) ([]domain.Category, error) {
	if includeArchived {
		rows, err := r.q.ListCategoriesIncludingArchived(ctx, uuid(householdID))
		if err != nil {
			return nil, translate(err, "list categories including archived")
		}
		out := make([]domain.Category, 0, len(rows))
		for _, row := range rows {
			out = append(out, toCategory(sqlcgen.Category(row)))
		}
		return out, nil
	}

	rows, err := r.q.ListCategories(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list categories")
	}
	out := make([]domain.Category, 0, len(rows))
	for _, row := range rows {
		out = append(out, toCategory(sqlcgen.Category(row)))
	}
	return out, nil
}

// EnsureSeeded inserts the starter set for a household that has none.
//
// The count check is an optimisation, not the correctness argument: without it
// every categories request would issue a thirteen-row insert that the unique
// index throws away. The correctness comes from SeedCategories' ON CONFLICT DO
// NOTHING, which is what makes two simultaneous first requests produce one set
// rather than a duplicate-key error. Removing the count would still be
// correct; removing the ON CONFLICT would not.
func (r *CategoryRepo) EnsureSeeded(ctx context.Context, householdID string, starter []domain.Category) error {
	count, err := r.q.CountCategories(ctx, uuid(householdID))
	if err != nil {
		return translate(err, "count categories")
	}
	if count > 0 {
		return nil
	}

	names := make([]string, 0, len(starter))
	kinds := make([]string, 0, len(starter))
	orders := make([]int32, 0, len(starter))
	for _, c := range starter {
		names = append(names, c.Name)
		kinds = append(kinds, string(c.Kind))
		orders = append(orders, int32(c.SortOrder))
	}

	err = r.q.SeedCategories(ctx, sqlcgen.SeedCategoriesParams{
		HouseholdID: uuid(householdID),
		Column2:     names,
		Column3:     kinds,
		Column4:     orders,
	})
	if err != nil {
		return translate(err, "seed categories")
	}
	return nil
}

func (r *CategoryRepo) BelongsToHousehold(ctx context.Context, householdID, categoryID string) (bool, error) {
	ok, err := r.q.CategoryBelongsToHousehold(ctx, sqlcgen.CategoryBelongsToHouseholdParams{
		ID:          uuid(categoryID),
		HouseholdID: uuid(householdID),
	})
	if err != nil {
		return false, translate(err, "check category household")
	}
	return ok, nil
}

func toCategory(c sqlcgen.Category) domain.Category {
	return domain.Category{
		ID:          uuidToString(c.ID),
		HouseholdID: uuidToString(c.HouseholdID),
		Name:        c.Name,
		Kind:        domain.CategoryKind(c.Kind),
		SortOrder:   int(c.SortOrder),
		ArchivedAt:  timePtrOf(c.ArchivedAt),
	}
}
```

**Note for the implementer:** never hand-edit anything under `sqlcgen/`. If the
field names differ from the ones above, change this adapter, not the generated
file.

- [ ] **Step 5: Run the tests**

```bash
cd api && go test ./internal/adapter/postgres/ -run 'TestEnsureSeeded' -v
```

Expected: PASS, all three.

- [ ] **Step 6: Mutation-check the concurrency guard**

Temporarily change `SeedCategories` to a plain `INSERT` with no `ON CONFLICT`,
run `make sqlc`, and confirm `TestEnsureSeededSurvivesConcurrentFirstRequests`
goes **red** with a duplicate-key error. Restore, regenerate, confirm green.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/postgres/queries/transaction.sql \
        api/internal/adapter/postgres/category_repo.go \
        api/internal/adapter/postgres/category_repo_test.go \
        api/internal/adapter/postgres/sqlcgen/
git commit -m "feat(postgres): add the category repository and its seed

The seed is one INSERT ... ON CONFLICT DO NOTHING, not a read-then-write:
every categories request runs it, and two simultaneous first requests would
otherwise both see an empty list."
```

---

## Task 6: `CategoryService`

**Files:**
- Create: `api/internal/usecase/category.go`, `api/internal/usecase/category_test.go`
- Modify: `api/internal/usecase/testdouble_test.go`

**Interfaces:**
- Consumes: `usecase.CategoryRepository` (Task 4), `domain.StarterCategories` (Task 2).
- Produces: `usecase.NewCategoryService(repo CategoryRepository) *CategoryService`
  with `List(ctx, householdID string) ([]domain.Category, error)`.

- [ ] **Step 1: Add the in-memory double**

Append to `api/internal/usecase/testdouble_test.go`, following the doubles
already there:

```go
// fakeCategoryRepo records how many times EnsureSeeded actually inserted, so a
// test can tell "seeded once" from "seeded on every call".
type fakeCategoryRepo struct {
	categories []domain.Category
	seeds      int
}

func (f *fakeCategoryRepo) List(_ context.Context, householdID string, includeArchived bool) ([]domain.Category, error) {
	out := []domain.Category{}
	for _, c := range f.categories {
		if c.HouseholdID != householdID {
			continue
		}
		if c.IsArchived() && !includeArchived {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeCategoryRepo) EnsureSeeded(_ context.Context, householdID string, starter []domain.Category) error {
	for _, c := range f.categories {
		if c.HouseholdID == householdID {
			return nil
		}
	}
	f.seeds++
	for i, c := range starter {
		c.ID = fmt.Sprintf("cat-%d", i+1)
		c.HouseholdID = householdID
		f.categories = append(f.categories, c)
	}
	return nil
}
```

- [ ] **Step 2: Write the failing test**

`api/internal/usecase/category_test.go`:

```go
package usecase_test

import (
	"context"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// The modal needs a category list before the household's first transaction
// exists, which is why the read is what seeds. A first-run household must
// never be shown an empty dropdown.
func TestListSeedsTheStarterSetForAHouseholdWithNone(t *testing.T) {
	repo := &fakeCategoryRepo{}
	svc := usecase.NewCategoryService(repo)

	got, err := svc.List(context.Background(), "house-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != len(domain.StarterCategories()) {
		t.Fatalf("got %d categories on first read, want the starter set of %d",
			len(got), len(domain.StarterCategories()))
	}
	if repo.seeds != 1 {
		t.Fatalf("seeded %d times on one read, want 1", repo.seeds)
	}
}

func TestListDoesNotReseedAHouseholdThatAlreadyHasCategories(t *testing.T) {
	repo := &fakeCategoryRepo{}
	svc := usecase.NewCategoryService(repo)
	ctx := context.Background()

	if _, err := svc.List(ctx, "house-1"); err != nil {
		t.Fatalf("first list: %v", err)
	}
	if _, err := svc.List(ctx, "house-1"); err != nil {
		t.Fatalf("second list: %v", err)
	}
	if repo.seeds != 1 {
		t.Fatalf("seeded %d times across two reads, want 1", repo.seeds)
	}
}
```

- [ ] **Step 3: Run it and watch it fail**

```bash
cd api && go test ./internal/usecase/ -run TestListSeeds -v
```

Expected: FAIL — `undefined: usecase.NewCategoryService`.

- [ ] **Step 4: Write the service**

`api/internal/usecase/category.go`:

```go
package usecase

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// CategoryService is the household's spending taxonomy: one job, reading it.
//
// Nothing here renames, adds or archives a category. Those controls live on
// Budget's "Edit categories" screen, which is the next feature; this service
// exists so Transactions has a list to show and Budget has one to sum over.
type CategoryService struct {
	repo CategoryRepository
}

func NewCategoryService(repo CategoryRepository) *CategoryService {
	return &CategoryService{repo: repo}
}

// List returns the household's live categories, seeding the starter set first
// if it has none.
//
// A read that writes is unusual, and it is deliberate. Seeding at household
// creation would reach into SignupRepository.Provision -- the transaction a
// stranger's sign-up depends on, whose atomicity is documented at length --
// for a feature that does not need to be there. Seeding on first *write* is
// too late: the "Log a transaction" modal needs a category list before the
// household's first transaction exists. First read is the only moment left.
//
// EnsureSeeded is idempotent and concurrency-safe (see its port doc), so
// calling it on every read costs one cheap count and nothing else.
func (s *CategoryService) List(ctx context.Context, householdID string) ([]domain.Category, error) {
	if err := s.repo.EnsureSeeded(ctx, householdID, domain.StarterCategories()); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, householdID, false)
}
```

- [ ] **Step 5: Run the tests**

```bash
cd api && go test ./internal/usecase/ -run TestList -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/category.go api/internal/usecase/category_test.go \
        api/internal/usecase/testdouble_test.go
git commit -m "feat(usecase): add CategoryService

The read is what seeds: household creation is the wrong place (it reaches into
the sign-up provisioning transaction) and first write is too late (the modal
needs the list before a transaction exists)."
```

---

## Task 7: The transaction adapter — write, read one, delete

**Files:**
- Modify: `api/internal/adapter/postgres/queries/transaction.sql`
- Create: `api/internal/adapter/postgres/transaction_repo.go`,
  `api/internal/adapter/postgres/transaction_repo_test.go`

**Interfaces:**
- Consumes: `usecase.TransactionRepository`, `usecase.TransactionView` (Task 4);
  `domain.Transaction` (Task 3).
- Produces: `postgres.NewTransactionRepo(db *DB) *TransactionRepo` with
  `Create`, `Get`, `Update`, `Delete` implemented. `List` and `MonthTotals`
  arrive in Task 8 — declare them returning `nil, nil` so the type compiles,
  with a `// Task 8` comment, and delete that stub there.

- [ ] **Step 1: Write the failing test**

`api/internal/adapter/postgres/transaction_repo_test.go`:

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

func july(day int) time.Time {
	return time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC)
}

// insertTestAccount gives the transaction tests something to attach to
// without going through AccountRepo -- a repository test that builds its
// fixtures through another repository fails for two reasons at once.
func insertTestAccount(t *testing.T, db *postgres.DB, householdID, nickname, currency string) string {
	t.Helper()
	var id string
	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO accounts (household_id, nickname, type, opening_balance_minor,
		                       opening_balance_currency, opening_balance_as_of)
		 VALUES ($1, $2, 'cash', 0, $3, DATE '2026-07-01') RETURNING id`,
		householdID, nickname, currency).Scan(&id)
	if err != nil {
		t.Fatalf("insert account %s: %v", nickname, err)
	}
	return id
}

func TestTransactionRoundTrips(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewTransactionRepo(db)
	householdID := insertHousehold(t, db)
	dbs := insertTestAccount(t, db, householdID, "DBS Everyday", "SGD")

	created, err := repo.Create(ctx, domain.Transaction{
		HouseholdID:   householdID,
		Kind:          domain.TransactionExpense,
		OccurredOn:    july(18),
		Description:   "Cold Storage",
		FromAccountID: dbs,
		Amount:        domain.Money{Amount: 5230, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("create returned no id")
	}

	view, err := repo.Get(ctx, householdID, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.Transaction.Description != "Cold Storage" {
		t.Fatalf("description = %q", view.Transaction.Description)
	}
	if view.FromAccountName != "DBS Everyday" {
		t.Fatalf("fromAccountName = %q, want the account's nickname", view.FromAccountName)
	}
	// An absent side is "" -- the "" <-> NULL convention -- never the zero uuid,
	// which would read as a real account that happens not to exist.
	if view.Transaction.ToAccountID != "" {
		t.Fatalf("toAccountId = %q, want \"\" for an expense", view.Transaction.ToAccountID)
	}
	// A date, compared as a date. Storing an instant would make this assertion
	// depend on the server's zone.
	if !view.Transaction.OccurredOn.Equal(july(18)) {
		t.Fatalf("occurredOn = %v, want %v", view.Transaction.OccurredOn, july(18))
	}
}

// An id from another household must be indistinguishable from one that does
// not exist. A 404 that differs from a 403 tells a caller which ids are real.
func TestGetRefusesATransactionInAnotherHousehold(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewTransactionRepo(db)

	mine, theirs := insertHousehold(t, db), insertHousehold(t, db)
	theirAccount := insertTestAccount(t, db, theirs, "Their DBS", "SGD")
	created, err := repo.Create(ctx, domain.Transaction{
		HouseholdID: theirs, Kind: domain.TransactionExpense, OccurredOn: july(18),
		Description: "Theirs", FromAccountID: theirAccount,
		Amount: domain.Money{Amount: 100, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := repo.Get(ctx, mine, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get across households = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, mine, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("delete across households = %v, want ErrNotFound", err)
	}
}

func TestDeleteRemovesTheRow(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewTransactionRepo(db)
	householdID := insertHousehold(t, db)
	dbs := insertTestAccount(t, db, householdID, "DBS", "SGD")

	created, err := repo.Create(ctx, domain.Transaction{
		HouseholdID: householdID, Kind: domain.TransactionExpense, OccurredOn: july(18),
		Description: "Typo", FromAccountID: dbs,
		Amount: domain.Money{Amount: 100, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Delete(ctx, householdID, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, householdID, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, householdID, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
}

// Both are database behaviour, so only a database can prove them. The
// membership case is why the column is ON DELETE SET NULL; the household case
// is why the account columns are CASCADE and not RESTRICT.
func TestDeletingAMemberKeepsTheirTransactionsAndDeletingAHouseholdTakesThemAway(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewTransactionRepo(db)
	householdID := insertHousehold(t, db)
	dbs := insertTestAccount(t, db, householdID, "DBS", "SGD")
	membershipID := insertTestMembership(t, db, householdID, "Christine")

	created, err := repo.Create(ctx, domain.Transaction{
		HouseholdID: householdID, Kind: domain.TransactionExpense, OccurredOn: july(18),
		Description: "Cold Storage", FromAccountID: dbs, PaidByMembershipID: membershipID,
		Amount: domain.Money{Amount: 5230, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := db.Pool().Exec(ctx, `DELETE FROM memberships WHERE id = $1`, membershipID); err != nil {
		t.Fatalf("delete membership: %v", err)
	}
	view, err := repo.Get(ctx, householdID, created.ID)
	if err != nil {
		t.Fatalf("get after member removal: %v", err)
	}
	if view.Transaction.PaidByMembershipID != "" {
		t.Fatalf("paidBy = %q after the member was removed, want \"\"",
			view.Transaction.PaidByMembershipID)
	}

	// The cascade RESTRICT would have blocked.
	if _, err := db.Pool().Exec(ctx, `DELETE FROM households WHERE id = $1`, householdID); err != nil {
		t.Fatalf("delete household: %v", err)
	}
}
```

Add `insertTestMembership` to the same file if the repository tests do not
already have one — check `repos_test.go` first and reuse:

```go
func insertTestMembership(t *testing.T, db *postgres.DB, householdID, displayName string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO users (display_name, avatar_initial) VALUES ($1, left($1, 1)) RETURNING id`,
		displayName).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var membershipID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO memberships (household_id, user_id, role, capabilities)
		 VALUES ($1, $2, 'owner', ARRAY['calendar','chores','money','marriage']) RETURNING id`,
		householdID, userID).Scan(&membershipID); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	return membershipID
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestTransactionRoundTrips -v
```

Expected: FAIL — `undefined: postgres.NewTransactionRepo`.

- [ ] **Step 3: Add the queries**

Append to `api/internal/adapter/postgres/queries/transaction.sql`:

```sql
-- transactionColumns is repeated in full in each query below rather than
-- factored into a view: sqlc generates a distinct row struct per query, and a
-- view would hide which columns each one actually reads.
--
-- The three LEFT JOINs are what let an expense (no destination), a shared
-- transaction (no payer) and an uncategorised one come back as rows with NULL
-- names rather than vanishing.
--
-- before_from_opening and before_to_opening are computed here, next to the
-- dates they compare, so the rule that only transactions after an account's
-- opening date move its balance lives in one place. A strict < against
-- opening_balance_as_of, mirroring the strict > in the balance sum: a
-- transaction dated *on* the opening date is already reflected in the figure
-- someone asserted was true that day.

-- name: GetTransaction :one
SELECT t.id, t.household_id, t.kind, t.occurred_on, t.description,
       t.category_id, t.paid_by_membership_id, t.from_account_id, t.to_account_id,
       t.amount_minor, t.amount_currency,
       t.received_amount_minor, t.received_amount_currency, t.created_at,
       c.name AS category_name,
       u.display_name AS paid_by_name,
       fa.nickname AS from_account_name,
       ta.nickname AS to_account_name,
       (fa.id IS NOT NULL AND t.occurred_on <= fa.opening_balance_as_of) AS before_from_opening,
       (ta.id IS NOT NULL AND t.occurred_on <= ta.opening_balance_as_of) AS before_to_opening
FROM transactions t
LEFT JOIN categories  c  ON c.id  = t.category_id
LEFT JOIN memberships m  ON m.id  = t.paid_by_membership_id
LEFT JOIN users       u  ON u.id  = m.user_id
LEFT JOIN accounts    fa ON fa.id = t.from_account_id
LEFT JOIN accounts    ta ON ta.id = t.to_account_id
WHERE t.household_id = $1 AND t.id = $2;

-- name: CreateTransaction :one
INSERT INTO transactions (
    household_id, kind, occurred_on, description, category_id,
    paid_by_membership_id, from_account_id, to_account_id,
    amount_minor, amount_currency, received_amount_minor, received_amount_currency
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING id, household_id, kind, occurred_on, description, category_id,
          paid_by_membership_id, from_account_id, to_account_id,
          amount_minor, amount_currency, received_amount_minor,
          received_amount_currency, created_at;

-- name: UpdateTransaction :one
UPDATE transactions
SET kind                     = $3,
    occurred_on              = $4,
    description              = $5,
    category_id              = $6,
    paid_by_membership_id    = $7,
    from_account_id          = $8,
    to_account_id            = $9,
    amount_minor             = $10,
    amount_currency          = $11,
    received_amount_minor    = $12,
    received_amount_currency = $13
WHERE household_id = $1 AND id = $2
RETURNING id, household_id, kind, occurred_on, description, category_id,
          paid_by_membership_id, from_account_id, to_account_id,
          amount_minor, amount_currency, received_amount_minor,
          received_amount_currency, created_at;

-- DeleteTransaction is scoped by household_id like every other query here, and
-- returns the id so the caller can tell "removed" from "there was nothing to
-- remove" without a second round trip. A transaction is hard deleted -- unlike
-- an account, nothing references it, so nothing is orphaned.
-- name: DeleteTransaction :one
DELETE FROM transactions
WHERE household_id = $1 AND id = $2
RETURNING id;
```

```bash
make sqlc
```

- [ ] **Step 4: Write the adapter**

`api/internal/adapter/postgres/transaction_repo.go`:

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

type TransactionRepo struct{ q *sqlcgen.Queries }

func NewTransactionRepo(db *DB) *TransactionRepo {
	return &TransactionRepo{q: sqlcgen.New(db.Pool())}
}

func (r *TransactionRepo) Get(ctx context.Context, householdID, transactionID string) (usecase.TransactionView, error) {
	row, err := r.q.GetTransaction(ctx, sqlcgen.GetTransactionParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(transactionID),
	})
	if err != nil {
		return usecase.TransactionView{}, translate(err, "get transaction")
	}
	return toTransactionViewFromGet(row), nil
}

func (r *TransactionRepo) Create(ctx context.Context, t domain.Transaction) (domain.Transaction, error) {
	row, err := r.q.CreateTransaction(ctx, sqlcgen.CreateTransactionParams{
		HouseholdID:            uuid(t.HouseholdID),
		Kind:                   string(t.Kind),
		OccurredOn:             dateOnly(t.OccurredOn),
		Description:            t.Description,
		CategoryID:             nullableUUID(optionalID(t.CategoryID)),
		PaidByMembershipID:     nullableUUID(optionalID(t.PaidByMembershipID)),
		FromAccountID:          nullableUUID(optionalID(t.FromAccountID)),
		ToAccountID:            nullableUUID(optionalID(t.ToAccountID)),
		AmountMinor:            t.Amount.Amount,
		AmountCurrency:         t.Amount.Currency,
		ReceivedAmountMinor:    receivedMinor(t.ReceivedAmount),
		ReceivedAmountCurrency: receivedCurrency(t.ReceivedAmount),
	})
	if err != nil {
		return domain.Transaction{}, translate(err, "create transaction")
	}
	return toTransaction(row), nil
}

func (r *TransactionRepo) Update(ctx context.Context, t domain.Transaction) (domain.Transaction, error) {
	row, err := r.q.UpdateTransaction(ctx, sqlcgen.UpdateTransactionParams{
		HouseholdID:            uuid(t.HouseholdID),
		ID:                     uuid(t.ID),
		Kind:                   string(t.Kind),
		OccurredOn:             dateOnly(t.OccurredOn),
		Description:            t.Description,
		CategoryID:             nullableUUID(optionalID(t.CategoryID)),
		PaidByMembershipID:     nullableUUID(optionalID(t.PaidByMembershipID)),
		FromAccountID:          nullableUUID(optionalID(t.FromAccountID)),
		ToAccountID:            nullableUUID(optionalID(t.ToAccountID)),
		AmountMinor:            t.Amount.Amount,
		AmountCurrency:         t.Amount.Currency,
		ReceivedAmountMinor:    receivedMinor(t.ReceivedAmount),
		ReceivedAmountCurrency: receivedCurrency(t.ReceivedAmount),
	})
	if err != nil {
		return domain.Transaction{}, translate(err, "update transaction")
	}
	return toTransaction(row), nil
}

func (r *TransactionRepo) Delete(ctx context.Context, householdID, transactionID string) error {
	_, err := r.q.DeleteTransaction(ctx, sqlcgen.DeleteTransactionParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(transactionID),
	})
	if err != nil {
		// translate maps pgx.ErrNoRows to domain.ErrNotFound, which is what
		// makes deleting something that is not there indistinguishable from
		// deleting something in another household.
		return translate(err, "delete transaction")
	}
	return nil
}

// receivedMinor and receivedCurrency implement the nil <-> NULL half of the
// received amount. They are two functions rather than one returning both
// because sqlc's generated params take them as separate fields, and a single
// helper returning a pair would be unpacked at both call sites anyway.
func receivedMinor(m *domain.Money) *int64 {
	if m == nil {
		return nil
	}
	amount := m.Amount
	return &amount
}

func receivedCurrency(m *domain.Money) *string {
	if m == nil {
		return nil
	}
	currency := m.Currency
	return &currency
}

func toTransaction(t sqlcgen.Transaction) domain.Transaction {
	out := domain.Transaction{
		ID:                 uuidToString(t.ID),
		HouseholdID:        uuidToString(t.HouseholdID),
		Kind:               domain.TransactionKind(t.Kind),
		OccurredOn:         dateToTime(t.OccurredOn),
		Description:        t.Description,
		CategoryID:         optionalIDToString(t.CategoryID),
		PaidByMembershipID: optionalIDToString(t.PaidByMembershipID),
		FromAccountID:      optionalIDToString(t.FromAccountID),
		ToAccountID:        optionalIDToString(t.ToAccountID),
		Amount:             domain.Money{Amount: t.AmountMinor, Currency: t.AmountCurrency},
	}
	if t.ReceivedAmountMinor != nil && t.ReceivedAmountCurrency != nil {
		out.ReceivedAmount = &domain.Money{
			Amount:   *t.ReceivedAmountMinor,
			Currency: *t.ReceivedAmountCurrency,
		}
	}
	return out
}

// buildTransactionView is the one place a row's joined names and its two
// before-opening flags become a view, so the Get and List converters cannot
// disagree about them.
func buildTransactionView(
	t domain.Transaction,
	categoryName, paidByName, fromName, toName *string,
	beforeFrom, beforeTo *bool,
) usecase.TransactionView {
	view := usecase.TransactionView{
		Transaction:     t,
		CategoryName:    stringOrEmpty(categoryName),
		PaidByName:      stringOrEmpty(paidByName),
		FromAccountName: stringOrEmpty(fromName),
		ToAccountName:   stringOrEmpty(toName),
	}
	// nil when there is no account on that side at all -- an expense has no
	// destination, so "does this predate the destination's opening date" has
	// no answer rather than a false one.
	if t.FromAccountID != "" {
		view.BeforeFromAccountOpening = beforeFrom
	}
	if t.ToAccountID != "" {
		view.BeforeToAccountOpening = beforeTo
	}
	return view
}

func toTransactionViewFromGet(row sqlcgen.GetTransactionRow) usecase.TransactionView {
	return buildTransactionView(
		toTransaction(sqlcgen.Transaction{
			ID: row.ID, HouseholdID: row.HouseholdID, Kind: row.Kind,
			OccurredOn: row.OccurredOn, Description: row.Description,
			CategoryID: row.CategoryID, PaidByMembershipID: row.PaidByMembershipID,
			FromAccountID: row.FromAccountID, ToAccountID: row.ToAccountID,
			AmountMinor: row.AmountMinor, AmountCurrency: row.AmountCurrency,
			ReceivedAmountMinor: row.ReceivedAmountMinor,
			ReceivedAmountCurrency: row.ReceivedAmountCurrency,
			CreatedAt: row.CreatedAt,
		}),
		row.CategoryName, row.PaidByName, row.FromAccountName, row.ToAccountName,
		&row.BeforeFromOpening, &row.BeforeToOpening,
	)
}

// List and MonthTotals arrive in Task 8. Declared here so TransactionRepo
// satisfies usecase.TransactionRepository and main.go can wire it.
func (r *TransactionRepo) List(ctx context.Context, householdID string, f usecase.TransactionFilter) ([]usecase.TransactionView, error) {
	return nil, nil // Task 8
}

func (r *TransactionRepo) MonthTotals(ctx context.Context, householdID string, month time.Time) ([]usecase.TransactionView, error) {
	return nil, nil // Task 8
}

```

**Note for the implementer:** import `pgtype` only if the generated parameter
structs actually need it — `make sqlc` decides that, and `account_repo.go`
imports it for `pgtype.Timestamptz`. Never add a blank identifier to keep an
unused import alive.

- [ ] **Step 5: Run the tests**

```bash
cd api && go test ./internal/adapter/postgres/ -run 'TestTransaction|TestGetRefuses|TestDelete|TestDeletingAMember' -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/postgres/queries/transaction.sql \
        api/internal/adapter/postgres/transaction_repo.go \
        api/internal/adapter/postgres/transaction_repo_test.go \
        api/internal/adapter/postgres/sqlcgen/
git commit -m "feat(postgres): add transaction create, read, update and delete

The two before-opening flags are computed in SQL beside the dates they
compare, so the only-after-the-opening-date rule lives in one place."
```

---

## Task 8: The ledger query — filters and keyset paging

**Files:**
- Modify: `api/internal/adapter/postgres/queries/transaction.sql`,
  `api/internal/adapter/postgres/transaction_repo.go`,
  `api/internal/adapter/postgres/transaction_repo_test.go`

**Interfaces:**
- Consumes: `usecase.TransactionFilter` (Task 4).
- Produces: `List` and `MonthTotals` really implemented; the Task 7 stubs gone.

- [ ] **Step 1: Write the failing test**

Append to `api/internal/adapter/postgres/transaction_repo_test.go`:

```go
// The reason paging is keyset and not offset. With OFFSET, inserting a row
// dated between page one and page two shifts every later row by one, so the
// reader either sees a transaction twice or never sees it at all -- silently,
// in a list of their own money.
func TestPagingIsStableWhenARowIsInsertedMidScroll(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewTransactionRepo(db)
	householdID := insertHousehold(t, db)
	dbs := insertTestAccount(t, db, householdID, "DBS", "SGD")

	// Ten transactions, one per day, newest 20 July.
	for day := 11; day <= 20; day++ {
		if _, err := repo.Create(ctx, domain.Transaction{
			HouseholdID: householdID, Kind: domain.TransactionExpense,
			OccurredOn: july(day), Description: "Day " + time.Month(7).String(),
			FromAccountID: dbs, Amount: domain.Money{Amount: int64(day), Currency: "SGD"},
		}); err != nil {
			t.Fatalf("create day %d: %v", day, err)
		}
	}

	first, err := repo.List(ctx, householdID, usecase.TransactionFilter{Limit: 4})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	// Limit+1, so the caller can tell there is another page without a second
	// query.
	if len(first) != 5 {
		t.Fatalf("first page returned %d rows, want limit+1 = 5", len(first))
	}
	last := first[3]

	// Someone else logs a transaction dated in the middle of what we are
	// reading.
	if _, err := repo.Create(ctx, domain.Transaction{
		HouseholdID: householdID, Kind: domain.TransactionExpense,
		OccurredOn: july(15), Description: "Inserted mid-scroll",
		FromAccountID: dbs, Amount: domain.Money{Amount: 999, Currency: "SGD"},
	}); err != nil {
		t.Fatalf("insert mid-scroll: %v", err)
	}

	second, err := repo.List(ctx, householdID, usecase.TransactionFilter{
		Limit:      4,
		CursorDate: last.Transaction.OccurredOn,
		CursorID:   last.Transaction.ID,
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}

	seen := map[string]bool{}
	for _, v := range first[:4] {
		seen[v.Transaction.ID] = true
	}
	for _, v := range second {
		if seen[v.Transaction.ID] {
			t.Fatalf("transaction %s appeared on both pages", v.Transaction.ID)
		}
		if v.Transaction.OccurredOn.After(last.Transaction.OccurredOn) {
			t.Fatalf("page two contains %v, newer than the cursor %v",
				v.Transaction.OccurredOn, last.Transaction.OccurredOn)
		}
	}
}

// An account filter that only matched from_account_id would hide money
// arriving in the account someone selected -- half of what they asked for.
func TestTheAccountFilterMatchesBothSidesOfATransfer(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewTransactionRepo(db)
	householdID := insertHousehold(t, db)
	dbs := insertTestAccount(t, db, householdID, "DBS", "SGD")
	ocbc := insertTestAccount(t, db, householdID, "OCBC", "SGD")

	if _, err := repo.Create(ctx, domain.Transaction{
		HouseholdID: householdID, Kind: domain.TransactionTransfer,
		OccurredOn: july(18), Description: "To savings",
		FromAccountID: dbs, ToAccountID: ocbc,
		Amount: domain.Money{Amount: 50000, Currency: "SGD"},
	}); err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	for _, accountID := range []string{dbs, ocbc} {
		got, err := repo.List(ctx, householdID, usecase.TransactionFilter{
			AccountID: accountID, Limit: 20,
		})
		if err != nil {
			t.Fatalf("list for %s: %v", accountID, err)
		}
		if len(got) != 1 {
			t.Fatalf("account filter returned %d rows for one side of a transfer, want 1", len(got))
		}
	}
}

func TestMonthTotalsCoversTheWholeMonthAndNothingElse(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewTransactionRepo(db)
	householdID := insertHousehold(t, db)
	dbs := insertTestAccount(t, db, householdID, "DBS", "SGD")

	days := []time.Time{
		time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC), // out, previous month
		july(1),                                      // in, first day
		july(31),                                     // in, last day
		time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),  // out, next month
	}
	for i, day := range days {
		if _, err := repo.Create(ctx, domain.Transaction{
			HouseholdID: householdID, Kind: domain.TransactionExpense,
			OccurredOn: day, Description: "Row", FromAccountID: dbs,
			Amount: domain.Money{Amount: int64(i + 1), Currency: "SGD"},
		}); err != nil {
			t.Fatalf("create %v: %v", day, err)
		}
	}

	got, err := repo.MonthTotals(ctx, householdID, july(15))
	if err != nil {
		t.Fatalf("month totals: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("July contains %d transactions, want the 1st and the 31st only", len(got))
	}
}
```

The test file needs `"github.com/andreasoentoro/hearth/api/internal/usecase"`
added to its imports.

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/adapter/postgres/ -run 'TestPagingIsStable|TestTheAccountFilter|TestMonthTotals' -v
```

Expected: FAIL — `List` returns nil, so every assertion about row counts fails.

- [ ] **Step 3: Add the queries**

Append to `api/internal/adapter/postgres/queries/transaction.sql`. Both queries
use `sqlc.narg` so one prepared statement serves every combination of filters —
a query built by string concatenation per filter combination is where SQL
injection lives, and this codebase has no query builder.

```sql
-- ListTransactions serves the ledger and all five of its filters.
--
-- Each filter is written as `(sqlc.narg(x)::type IS NULL OR column = ...)`, so
-- an unset filter is a no-op inside one prepared statement rather than a
-- separate query per combination. Thirty-two hand-written variants would drift
-- from each other, and a concatenated string would be an injection surface.
--
-- The account filter matches EITHER side: a transfer belongs in the ledger of
-- both accounts it touches.
--
-- The keyset predicate is the row-value comparison (occurred_on, id) < (cursor
-- date, cursor id), which matches transactions_household_date_idx exactly.
-- Comparing the pair rather than the date alone is what makes two transactions
-- on the same day page correctly.
--
-- LIMIT is $N + 1 in the caller, not here: the extra row is how the caller
-- learns another page exists without counting the table.
-- name: ListTransactions :many
SELECT t.id, t.household_id, t.kind, t.occurred_on, t.description,
       t.category_id, t.paid_by_membership_id, t.from_account_id, t.to_account_id,
       t.amount_minor, t.amount_currency,
       t.received_amount_minor, t.received_amount_currency, t.created_at,
       c.name AS category_name,
       u.display_name AS paid_by_name,
       fa.nickname AS from_account_name,
       ta.nickname AS to_account_name,
       (fa.id IS NOT NULL AND t.occurred_on <= fa.opening_balance_as_of) AS before_from_opening,
       (ta.id IS NOT NULL AND t.occurred_on <= ta.opening_balance_as_of) AS before_to_opening
FROM transactions t
LEFT JOIN categories  c  ON c.id  = t.category_id
LEFT JOIN memberships m  ON m.id  = t.paid_by_membership_id
LEFT JOIN users       u  ON u.id  = m.user_id
LEFT JOIN accounts    fa ON fa.id = t.from_account_id
LEFT JOIN accounts    ta ON ta.id = t.to_account_id
WHERE t.household_id = $1
  AND (sqlc.narg('kind')::text IS NULL OR t.kind = sqlc.narg('kind')::text)
  AND (sqlc.narg('account_id')::uuid IS NULL
       OR t.from_account_id = sqlc.narg('account_id')::uuid
       OR t.to_account_id   = sqlc.narg('account_id')::uuid)
  AND (sqlc.narg('category_id')::uuid IS NULL OR t.category_id = sqlc.narg('category_id')::uuid)
  AND (sqlc.narg('paid_by')::uuid IS NULL OR t.paid_by_membership_id = sqlc.narg('paid_by')::uuid)
  AND (sqlc.narg('month_start')::date IS NULL
       OR (t.occurred_on >= sqlc.narg('month_start')::date
           AND t.occurred_on < (sqlc.narg('month_start')::date + INTERVAL '1 month')))
  AND (sqlc.narg('cursor_date')::date IS NULL
       OR (t.occurred_on, t.id) < (sqlc.narg('cursor_date')::date, sqlc.narg('cursor_id')::uuid))
ORDER BY t.occurred_on DESC, t.id DESC
LIMIT sqlc.arg('row_limit');

-- MonthTotalsQuery returns every transaction in one calendar month. The
-- service converts each amount into the household's primary currency before
-- summing, which SQL cannot do -- the FX provider lives in the usecase layer.
-- name: MonthTotalsQuery :many
SELECT t.id, t.household_id, t.kind, t.occurred_on, t.description,
       t.category_id, t.paid_by_membership_id, t.from_account_id, t.to_account_id,
       t.amount_minor, t.amount_currency,
       t.received_amount_minor, t.received_amount_currency, t.created_at,
       c.name AS category_name,
       u.display_name AS paid_by_name,
       fa.nickname AS from_account_name,
       ta.nickname AS to_account_name,
       (fa.id IS NOT NULL AND t.occurred_on <= fa.opening_balance_as_of) AS before_from_opening,
       (ta.id IS NOT NULL AND t.occurred_on <= ta.opening_balance_as_of) AS before_to_opening
FROM transactions t
LEFT JOIN categories  c  ON c.id  = t.category_id
LEFT JOIN memberships m  ON m.id  = t.paid_by_membership_id
LEFT JOIN users       u  ON u.id  = m.user_id
LEFT JOIN accounts    fa ON fa.id = t.from_account_id
LEFT JOIN accounts    ta ON ta.id = t.to_account_id
WHERE t.household_id = $1
  AND t.occurred_on >= $2::date
  AND t.occurred_on < ($2::date + INTERVAL '1 month')
ORDER BY t.occurred_on DESC, t.id DESC;
```

```bash
make sqlc
```

- [ ] **Step 4: Replace the two stubs**

In `api/internal/adapter/postgres/transaction_repo.go`, delete the Task 7 stubs
and write:

```go
// List asks for one row more than the caller wanted. That extra row is the
// whole "is there another page" answer -- a COUNT(*) over a filtered ledger
// costs a scan to learn something the page itself already implies.
func (r *TransactionRepo) List(ctx context.Context, householdID string, f usecase.TransactionFilter) ([]usecase.TransactionView, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultTransactionLimit
	}
	if limit > maxTransactionLimit {
		limit = maxTransactionLimit
	}

	params := sqlcgen.ListTransactionsParams{
		HouseholdID: uuid(householdID),
		RowLimit:    int32(limit + 1),
	}
	if f.Kind != "" {
		kind := f.Kind
		params.Kind = &kind
	}
	if f.AccountID != "" {
		params.AccountID = nullableUUID(&f.AccountID)
	}
	if f.CategoryID != "" {
		params.CategoryID = nullableUUID(&f.CategoryID)
	}
	if f.PaidByMembershipID != "" {
		params.PaidBy = nullableUUID(&f.PaidByMembershipID)
	}
	if !f.Month.IsZero() {
		params.MonthStart = dateOnlyPtr(startOfMonth(f.Month))
	}
	// Both cursor halves or neither: a date with no id cannot order two
	// transactions on the same day, and the row-value comparison would compare
	// against a NULL id and return nothing at all.
	if !f.CursorDate.IsZero() && f.CursorID != "" {
		params.CursorDate = dateOnlyPtr(f.CursorDate)
		params.CursorID = nullableUUID(&f.CursorID)
	}

	rows, err := r.q.ListTransactions(ctx, params)
	if err != nil {
		return nil, translate(err, "list transactions")
	}
	out := make([]usecase.TransactionView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toTransactionViewFromList(row))
	}
	return out, nil
}

func (r *TransactionRepo) MonthTotals(ctx context.Context, householdID string, month time.Time) ([]usecase.TransactionView, error) {
	rows, err := r.q.MonthTotalsQuery(ctx, sqlcgen.MonthTotalsQueryParams{
		HouseholdID: uuid(householdID),
		Column2:     dateOnly(startOfMonth(month)),
	})
	if err != nil {
		return nil, translate(err, "month totals")
	}
	out := make([]usecase.TransactionView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toTransactionViewFromMonthTotals(row))
	}
	return out, nil
}

const (
	// defaultTransactionLimit matches the ledger's own page size. maxTransactionLimit
	// is what stops a caller asking for the whole ledger in one request --
	// nothing in the UI sends a limit at all, so this only ever bounds a
	// hand-written request.
	defaultTransactionLimit = 50
	maxTransactionLimit     = 200
)

// startOfMonth normalises any instant to the first day of its month, in UTC.
// occurred_on is a date column and this product stores no timezone per
// household, so a month is a calendar month and not a range of instants.
func startOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
```

Add the two converters beside `toTransactionViewFromGet`. They are three
near-identical functions because sqlc generates a distinct row struct per query
and flattens the columns onto it — the same reason `account_repo.go` has three
account-view converters:

```go
func toTransactionViewFromList(row sqlcgen.ListTransactionsRow) usecase.TransactionView {
	return buildTransactionView(
		toTransaction(sqlcgen.Transaction{
			ID: row.ID, HouseholdID: row.HouseholdID, Kind: row.Kind,
			OccurredOn: row.OccurredOn, Description: row.Description,
			CategoryID: row.CategoryID, PaidByMembershipID: row.PaidByMembershipID,
			FromAccountID: row.FromAccountID, ToAccountID: row.ToAccountID,
			AmountMinor: row.AmountMinor, AmountCurrency: row.AmountCurrency,
			ReceivedAmountMinor: row.ReceivedAmountMinor,
			ReceivedAmountCurrency: row.ReceivedAmountCurrency,
			CreatedAt: row.CreatedAt,
		}),
		row.CategoryName, row.PaidByName, row.FromAccountName, row.ToAccountName,
		&row.BeforeFromOpening, &row.BeforeToOpening,
	)
}

func toTransactionViewFromMonthTotals(row sqlcgen.MonthTotalsQueryRow) usecase.TransactionView {
	return buildTransactionView(
		toTransaction(sqlcgen.Transaction{
			ID: row.ID, HouseholdID: row.HouseholdID, Kind: row.Kind,
			OccurredOn: row.OccurredOn, Description: row.Description,
			CategoryID: row.CategoryID, PaidByMembershipID: row.PaidByMembershipID,
			FromAccountID: row.FromAccountID, ToAccountID: row.ToAccountID,
			AmountMinor: row.AmountMinor, AmountCurrency: row.AmountCurrency,
			ReceivedAmountMinor: row.ReceivedAmountMinor,
			ReceivedAmountCurrency: row.ReceivedAmountCurrency,
			CreatedAt: row.CreatedAt,
		}),
		row.CategoryName, row.PaidByName, row.FromAccountName, row.ToAccountName,
		&row.BeforeFromOpening, &row.BeforeToOpening,
	)
}
```

**Note for the implementer:** `dateOnlyPtr` may not exist in `convert.go` yet —
check, and add it beside `dateOnly` if not:

```go
// dateOnlyPtr is dateOnly for an optional filter: a nil date parameter is
// "no filter", which is not the same as the zero date.
func dateOnlyPtr(t time.Time) *pgtype.Date {
	d := dateOnly(t)
	return &d
}
```

Confirm the generated `ListTransactionsParams` field names after `make sqlc`
and use exactly those.

- [ ] **Step 5: Run the tests**

```bash
cd api && go test ./internal/adapter/postgres/ -v
```

Expected: PASS, the whole package.

- [ ] **Step 6: Mutation-check the keyset paging**

Temporarily change the keyset predicate to compare only the date —
`t.occurred_on < sqlc.narg('cursor_date')::date` — run `make sqlc`, and confirm
`TestPagingIsStableWhenARowIsInsertedMidScroll` goes **red** (two transactions
on the boundary day are skipped). Restore, regenerate, confirm green.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/postgres/ && git commit -m "feat(postgres): add the ledger query with filters and keyset paging

Keyset, not offset: a transaction added mid-scroll shifts every later row and
makes a page boundary repeat or skip one, silently, in a list of their own
money. The account filter matches both sides of a transfer."
```

---

## Task 9: The accounts balance becomes a real sum

**Files:**
- Modify: `api/internal/adapter/postgres/queries/account.sql`,
  `api/internal/adapter/postgres/account_repo.go`,
  `api/internal/adapter/postgres/account_repo_test.go`

**Interfaces:**
- Consumes: the `transactions` table (Task 1).
- Produces: no signature change anywhere. `usecase.AccountView.Balance` starts
  reflecting transactions. This is the promise `AccountRepository.List`'s doc
  comment has carried since Accounts shipped.

- [ ] **Step 1: Write the failing test**

Append to `api/internal/adapter/postgres/account_repo_test.go`:

```go
// The doc comment on AccountRepository.List has promised this since Accounts
// shipped: Balance is the opening balance plus every transaction dated after
// opening_balance_as_of.
func TestAccountBalanceSumsItsTransactions(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	accounts := postgres.NewAccountRepo(db)
	transactions := postgres.NewTransactionRepo(db)
	householdID := insertHousehold(t, db)

	var accountID string
	err := db.Pool().QueryRow(ctx,
		`INSERT INTO accounts (household_id, nickname, type, opening_balance_minor,
		                       opening_balance_currency, opening_balance_as_of)
		 VALUES ($1, 'DBS Everyday', 'cash', 100000, 'SGD', DATE '2026-07-10') RETURNING id`,
		householdID).Scan(&accountID)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	mustCreate := func(kind domain.TransactionKind, day int, minor int64, from, to string) {
		t.Helper()
		if _, err := transactions.Create(ctx, domain.Transaction{
			HouseholdID: householdID, Kind: kind, OccurredOn: july(day),
			Description: "Row", FromAccountID: from, ToAccountID: to,
			Amount: domain.Money{Amount: minor, Currency: "SGD"},
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	mustCreate(domain.TransactionExpense, 12, 5000, accountID, "")
	mustCreate(domain.TransactionIncome, 14, 20000, "", accountID)
	// Dated ON the opening date: already reflected in the figure someone
	// asserted was true that day, so it must not be counted again.
	mustCreate(domain.TransactionExpense, 10, 7777, accountID, "")
	// Dated before it: same reasoning.
	mustCreate(domain.TransactionExpense, 3, 9999, accountID, "")

	views, err := accounts.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("got %d accounts, want 1", len(views))
	}
	// 100000 - 5000 + 20000
	if got := views[0].Balance.Amount; got != 115000 {
		t.Fatalf("balance = %d, want 115000 (opening 100000, -5000, +20000, "+
			"and nothing from the two dated on or before the opening date)", got)
	}
	// Get must agree with List. Two queries computing one figure is where they
	// drift.
	view, err := accounts.Get(ctx, householdID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if view.Balance.Amount != views[0].Balance.Amount {
		t.Fatalf("Get says %d and List says %d", view.Balance.Amount, views[0].Balance.Amount)
	}
}

// The defect this prevents: crediting the destination with the amount that
// left rather than what arrived would add Singapore dollars to a rupiah
// balance -- the account ends up wrong by a factor of ten thousand.
func TestACrossCurrencyTransferCreditsTheDestinationInItsOwnCurrency(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	accounts := postgres.NewAccountRepo(db)
	transactions := postgres.NewTransactionRepo(db)
	householdID := insertHousehold(t, db)

	dbs := insertTestAccount(t, db, householdID, "DBS", "SGD")
	bca := insertTestAccount(t, db, householdID, "BCA Tahapan", "IDR")

	received := domain.Money{Amount: 620000000, Currency: "IDR"}
	if _, err := transactions.Create(ctx, domain.Transaction{
		HouseholdID: householdID, Kind: domain.TransactionTransfer,
		OccurredOn: july(20), Description: "Transfer to BCA",
		FromAccountID: dbs, ToAccountID: bca,
		Amount: domain.Money{Amount: 50000, Currency: "SGD"}, ReceivedAmount: &received,
	}); err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	views, err := accounts.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byName := map[string]int64{}
	for _, v := range views {
		byName[v.Account.Nickname] = v.Balance.Amount
	}
	if byName["DBS"] != -50000 {
		t.Fatalf("DBS balance = %d, want -50000", byName["DBS"])
	}
	if byName["BCA Tahapan"] != 620000000 {
		t.Fatalf("BCA balance = %d, want 620000000 (the received amount, not the sent one)",
			byName["BCA Tahapan"])
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/adapter/postgres/ -run 'TestAccountBalanceSums|TestACrossCurrencyTransferCredits' -v
```

Expected: FAIL — the balance is still the opening balance, so `100000 != 115000`.

- [ ] **Step 3: Change the three account queries**

In `api/internal/adapter/postgres/queries/account.sql`, add the summed column to
`ListAccounts`, `ListAccountsIncludingArchived` and `GetAccount`. All three, or
Get and List will disagree — this project's repeated lesson is a rule fixed at
one site while its siblings keep the bug.

Add this column to each `SELECT`, after `u.display_name AS owner_name`:

```sql
       ,
       -- balance_minor is the opening balance plus every transaction dated
       -- AFTER opening_balance_as_of. The strict > is load-bearing: a
       -- transaction dated ON the opening date is already reflected in the
       -- figure someone asserted was true that day, and counting it again
       -- would make the account wrong by that transaction with nothing on
       -- screen to explain it.
       --
       -- Two filtered sums rather than one, because an account can be the
       -- source of one transfer and the destination of another. The incoming
       -- side takes received_amount_minor when there is one: that is what
       -- actually landed, in this account's own currency. Using amount_minor
       -- there would add the sending account's currency to this one's.
       --
       -- No conversion happens here and none can: every figure in this
       -- expression is already in this account's own currency.
       (a.opening_balance_minor
        - COALESCE((SELECT SUM(t.amount_minor) FROM transactions t
                    WHERE t.from_account_id = a.id
                      AND t.occurred_on > a.opening_balance_as_of), 0)
        + COALESCE((SELECT SUM(COALESCE(t.received_amount_minor, t.amount_minor))
                    FROM transactions t
                    WHERE t.to_account_id = a.id
                      AND t.occurred_on > a.opening_balance_as_of), 0)
       )::bigint AS balance_minor
```

Correlated subqueries rather than a `LEFT JOIN ... GROUP BY`: joining the same
table twice and grouping would multiply the account row by its transactions and
then need every selected column in the `GROUP BY`. Both indexes from Task 1
(`transactions_from_account_idx`, `transactions_to_account_idx`) exist for these
two subqueries.

```bash
make sqlc
```

- [ ] **Step 4: Use it in the adapter**

In `api/internal/adapter/postgres/account_repo.go`, `buildView` takes the summed
figure instead of copying the opening balance:

```go
// buildView is where AccountView.Balance is decided. It is the opening balance
// plus every transaction dated after opening_balance_as_of, summed in SQL --
// see the balance_minor column in queries/account.sql for why the comparison
// is strict and why the incoming side prefers received_amount_minor.
//
// The currency is the account's own. Every transaction on an account is
// denominated in that account's currency, so nothing here converts, and a
// mixed-currency household's accounts each stay in their own unit until
// AccountService.Summary converts them.
func buildView(a domain.Account, ownerName *string, balanceMinor int64) usecase.AccountView {
	return usecase.AccountView{
		Account:   a,
		OwnerName: stringOrEmpty(ownerName),
		Balance:   domain.Money{Amount: balanceMinor, Currency: a.OpeningBalance.Currency},
	}
}
```

Each of the three converters passes its row's `BalanceMinor` through:

```go
	}), row.OwnerName, row.BalanceMinor)
```

- [ ] **Step 5: Run the whole Postgres suite**

```bash
cd api && go test ./internal/adapter/postgres/ -v
```

Expected: PASS. The existing accounts tests still pass unchanged — an account
with no transactions has an empty sum, so its balance is still its opening
balance.

- [ ] **Step 6: Mutation-check the strict comparison**

This is the plan's designated mutation check for decision 6. Change `>` to `>=`
in all three queries, run `make sqlc`, and confirm
`TestAccountBalanceSumsItsTransactions` goes **red** — the transaction dated on
the opening date gets counted and the balance reads 107223. Restore,
regenerate, confirm green. If nothing goes red, the rule is not protected by
anything.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/postgres/ && git commit -m "feat(postgres): sum transactions into the account balance

The promise AccountRepository.List's doc comment has carried since Accounts
shipped. The strict > is what stops a transaction dated on the opening date
being counted twice; the incoming side prefers received_amount_minor so a
cross-currency transfer credits the destination in its own currency."
```

---

## Task 10: `TransactionService` — validation

**Files:**
- Create: `api/internal/usecase/transaction.go`, `api/internal/usecase/transaction_test.go`
- Modify: `api/internal/usecase/testdouble_test.go`, `api/internal/usecase/ports.go`

**Interfaces:**
- Consumes: `TransactionRepository`, `CategoryRepository`, `AccountRepository`,
  `Clock`.
- Produces:
  - `type NewTransaction struct { HouseholdID, Kind string; OccurredOn time.Time; Description, CategoryID, PaidByMembershipID, FromAccountID, ToAccountID string; AmountMinor int64; ReceivedAmountMinor *int64 }`
  - `type TransactionUpdate struct { Kind *string; OccurredOn *time.Time; Description, CategoryID, PaidByMembershipID, FromAccountID, ToAccountID *string; AmountMinor *int64; ReceivedAmountMinor *int64; ClearReceivedAmount bool }`
  - `type TransactionDeps struct { Transactions TransactionRepository; Categories CategoryLookup; Accounts AccountLookup; Households HouseholdRepository; FX FXRateProvider; Clock Clock }`
  - `usecase.NewTransactionService(d TransactionDeps) *TransactionService` with
    `List`, `Get`, `Create`, `Update`, `Delete`
  - two small lookup ports added to `ports.go`:
    ```go
    // CategoryLookup is what TransactionService needs of categories: whether an
    // id is one of this household's, and what kind it is. Narrow on purpose --
    // it does not need List or EnsureSeeded, and a port that hands it those is
    // a port that invites a service to seed as a side effect of validation.
    type CategoryLookup interface {
        BelongsToHousehold(ctx context.Context, householdID, categoryID string) (bool, error)
        Kind(ctx context.Context, householdID, categoryID string) (domain.CategoryKind, error)
    }

    // AccountLookup is what TransactionService needs of accounts: the currency
    // an account is denominated in, and whether it is this household's. Get
    // returns domain.ErrNotFound for an account in another household, which is
    // what makes "that account is not yours" indistinguishable from "there is
    // no such account".
    type AccountLookup interface {
        Get(ctx context.Context, householdID, accountID string) (AccountView, error)
    }
    ```
  `*postgres.AccountRepo` already satisfies `AccountLookup`. `*postgres.CategoryRepo`
  gains `Kind` in this task (one query, `GetCategoryKind`, already written in
  Task 5).

- [ ] **Step 1: Add the in-memory doubles**

Append to `api/internal/usecase/testdouble_test.go`:

```go
type fakeTransactionRepo struct {
	transactions []domain.Transaction
	nextID       int
}

func (f *fakeTransactionRepo) Create(_ context.Context, t domain.Transaction) (domain.Transaction, error) {
	f.nextID++
	t.ID = fmt.Sprintf("txn-%d", f.nextID)
	f.transactions = append(f.transactions, t)
	return t, nil
}

func (f *fakeTransactionRepo) Get(_ context.Context, householdID, id string) (usecase.TransactionView, error) {
	for _, t := range f.transactions {
		if t.ID == id && t.HouseholdID == householdID {
			return usecase.TransactionView{Transaction: t}, nil
		}
	}
	return usecase.TransactionView{}, domain.ErrNotFound
}

func (f *fakeTransactionRepo) Update(_ context.Context, t domain.Transaction) (domain.Transaction, error) {
	for i, existing := range f.transactions {
		if existing.ID == t.ID {
			f.transactions[i] = t
			return t, nil
		}
	}
	return domain.Transaction{}, domain.ErrNotFound
}

func (f *fakeTransactionRepo) Delete(_ context.Context, householdID, id string) error {
	for i, t := range f.transactions {
		if t.ID == id && t.HouseholdID == householdID {
			f.transactions = append(f.transactions[:i], f.transactions[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeTransactionRepo) List(_ context.Context, householdID string, _ usecase.TransactionFilter) ([]usecase.TransactionView, error) {
	out := []usecase.TransactionView{}
	for _, t := range f.transactions {
		if t.HouseholdID == householdID {
			out = append(out, usecase.TransactionView{Transaction: t})
		}
	}
	return out, nil
}

func (f *fakeTransactionRepo) MonthTotals(_ context.Context, householdID string, month time.Time) ([]usecase.TransactionView, error) {
	out := []usecase.TransactionView{}
	for _, t := range f.transactions {
		if t.HouseholdID != householdID {
			continue
		}
		if t.OccurredOn.Year() == month.Year() && t.OccurredOn.Month() == month.Month() {
			out = append(out, usecase.TransactionView{Transaction: t})
		}
	}
	return out, nil
}

// fakeCategoryLookup answers the two questions validation asks, and nothing
// else -- the narrow port it stands in for.
type fakeCategoryLookup struct {
	kinds map[string]domain.CategoryKind // category id -> kind, for one household
}

func (f *fakeCategoryLookup) BelongsToHousehold(_ context.Context, _, categoryID string) (bool, error) {
	_, ok := f.kinds[categoryID]
	return ok, nil
}

func (f *fakeCategoryLookup) Kind(_ context.Context, _, categoryID string) (domain.CategoryKind, error) {
	kind, ok := f.kinds[categoryID]
	if !ok {
		return "", domain.ErrNotFound
	}
	return kind, nil
}

// fakeAccountLookup holds currencies by account id. An id it does not know is
// an account in another household, and answers ErrNotFound for the reason the
// port documents.
type fakeAccountLookup struct {
	currencies map[string]string
}

func (f *fakeAccountLookup) Get(_ context.Context, _, accountID string) (usecase.AccountView, error) {
	currency, ok := f.currencies[accountID]
	if !ok {
		return usecase.AccountView{}, domain.ErrNotFound
	}
	return usecase.AccountView{
		Account: domain.Account{
			ID:             accountID,
			OpeningBalance: domain.Money{Currency: currency},
		},
		Balance: domain.Money{Currency: currency},
	}, nil
}
```

- [ ] **Step 2: Write the failing test**

`api/internal/usecase/transaction_test.go`:

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

func transactionFixture(t *testing.T) (*usecase.TransactionService, *fakeTransactionRepo) {
	t.Helper()
	repo := &fakeTransactionRepo{}
	svc := usecase.NewTransactionService(usecase.TransactionDeps{
		Transactions: repo,
		Categories: &fakeCategoryLookup{kinds: map[string]domain.CategoryKind{
			"cat-groceries": domain.CategoryExpense,
			"cat-income":    domain.CategoryIncome,
		}},
		Accounts: &fakeAccountLookup{currencies: map[string]string{
			"dbs": "SGD", "ocbc": "SGD", "bca": "IDR",
		}},
		Households: &fakeHouseholdRepo{primaryCurrency: "SGD"},
		FX:         staticTestFX{},
		Clock:      fixedClock{now: time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)},
	})
	return svc, repo
}

func expenseInput() usecase.NewTransaction {
	return usecase.NewTransaction{
		HouseholdID:   "house-1",
		Kind:          "expense",
		OccurredOn:    time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC),
		Description:   "Cold Storage",
		CategoryID:    "cat-groceries",
		FromAccountID: "dbs",
		AmountMinor:   5230,
	}
}

// The service derives the currency from the account. A request cannot name one
// -- NewTransaction has no currency field at all, which is what stops a
// handler accepting a value it never persists.
func TestCreateTakesTheAccountsCurrency(t *testing.T) {
	svc, _ := transactionFixture(t)

	created, err := svc.Create(context.Background(), expenseInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Amount.Currency != "SGD" {
		t.Fatalf("currency = %q, want the account's SGD", created.Amount.Currency)
	}

	idrExpense := expenseInput()
	idrExpense.FromAccountID = "bca"
	idrExpense.CategoryID = "cat-groceries"
	created, err = svc.Create(context.Background(), idrExpense)
	if err != nil {
		t.Fatalf("create on an IDR account: %v", err)
	}
	if created.Amount.Currency != "IDR" {
		t.Fatalf("currency = %q, want the account's IDR", created.Amount.Currency)
	}
}

func TestCreateRefusesTheWrongAccountsForItsKind(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()

	cases := map[string]func(usecase.NewTransaction) usecase.NewTransaction{
		"an expense with a destination": func(in usecase.NewTransaction) usecase.NewTransaction {
			in.ToAccountID = "ocbc"
			return in
		},
		"an expense with no source": func(in usecase.NewTransaction) usecase.NewTransaction {
			in.FromAccountID = ""
			return in
		},
		"a transfer with one leg": func(in usecase.NewTransaction) usecase.NewTransaction {
			in.Kind, in.CategoryID = "transfer", ""
			return in
		},
		"a transfer to and from the same account": func(in usecase.NewTransaction) usecase.NewTransaction {
			in.Kind, in.CategoryID = "transfer", ""
			in.ToAccountID = in.FromAccountID
			return in
		},
		"an account in another household": func(in usecase.NewTransaction) usecase.NewTransaction {
			in.FromAccountID = "someone-elses"
			return in
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Create(ctx, mutate(expenseInput()))
			if !errors.Is(err, domain.ErrTransactionAccountsInvalid) {
				t.Fatalf("create = %v, want ErrTransactionAccountsInvalid", err)
			}
		})
	}
}

// Decision 3: required across currencies so what arrived is recorded rather
// than guessed at a rate we do not have; permitted within one currency so a
// transfer fee is recordable; refused on anything that is not a transfer,
// where it would have nothing to mean.
func TestTheReceivedAmountFollowsTheCurrencies(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()

	crossCurrency := usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "transfer",
		OccurredOn: time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
		Description: "To BCA", FromAccountID: "dbs", ToAccountID: "bca",
		AmountMinor: 50000,
	}
	if _, err := svc.Create(ctx, crossCurrency); !errors.Is(err, domain.ErrReceivedAmountRequired) {
		t.Fatalf("cross-currency transfer with no received amount = %v, want ErrReceivedAmountRequired", err)
	}

	received := int64(620000000)
	crossCurrency.ReceivedAmountMinor = &received
	created, err := svc.Create(ctx, crossCurrency)
	if err != nil {
		t.Fatalf("cross-currency transfer: %v", err)
	}
	if created.ReceivedAmount == nil || created.ReceivedAmount.Currency != "IDR" {
		t.Fatalf("received amount = %v, want 620000000 IDR", created.ReceivedAmount)
	}

	// Same currency, with a fee. Accepted.
	fee := int64(49800)
	sameCurrency := crossCurrency
	sameCurrency.ToAccountID = "ocbc"
	sameCurrency.ReceivedAmountMinor = &fee
	if _, err := svc.Create(ctx, sameCurrency); err != nil {
		t.Fatalf("same-currency transfer with a fee: %v", err)
	}

	// An expense cannot carry one.
	expense := expenseInput()
	expense.ReceivedAmountMinor = &fee
	if _, err := svc.Create(ctx, expense); !errors.Is(err, domain.ErrReceivedAmountNotAllowed) {
		t.Fatalf("expense with a received amount = %v, want ErrReceivedAmountNotAllowed", err)
	}
}

func TestCreateRefusesACategoryOfTheWrongKind(t *testing.T) {
	svc, _ := transactionFixture(t)

	income := usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "income",
		OccurredOn: time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		Description: "Bonus", ToAccountID: "dbs",
		CategoryID: "cat-groceries", AmountMinor: 120000,
	}
	if _, err := svc.Create(context.Background(), income); !errors.Is(err, domain.ErrCategoryKindMismatch) {
		t.Fatalf("income categorised as Groceries = %v, want ErrCategoryKindMismatch", err)
	}

	transfer := usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "transfer",
		OccurredOn: time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		Description: "To savings", FromAccountID: "dbs", ToAccountID: "ocbc",
		CategoryID: "cat-groceries", AmountMinor: 50000,
	}
	if _, err := svc.Create(context.Background(), transfer); !errors.Is(err, domain.ErrCategoryKindMismatch) {
		t.Fatalf("transfer with a category = %v, want ErrCategoryKindMismatch", err)
	}
}

func TestCreateRefusesAnEmptyDescriptionAndANonPositiveAmount(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()

	blank := expenseInput()
	blank.Description = "   "
	if _, err := svc.Create(ctx, blank); !errors.Is(err, domain.ErrTransactionDescriptionRequired) {
		t.Fatalf("blank description = %v, want ErrTransactionDescriptionRequired", err)
	}

	for _, amount := range []int64{0, -100} {
		bad := expenseInput()
		bad.AmountMinor = amount
		if _, err := svc.Create(ctx, bad); !errors.Is(err, domain.ErrTransactionAmountNotPositive) {
			t.Fatalf("amount %d = %v, want ErrTransactionAmountNotPositive", amount, err)
		}
	}
}

// Update validates the merged result, never the incoming fields -- switching
// the kind to transfer and leaving a category alone are each legal on their
// own and illegal together.
func TestUpdateValidatesTheMergedResult(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, expenseInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	kind := "transfer"
	_, err = svc.Update(ctx, "house-1", created.ID, usecase.TransactionUpdate{Kind: &kind})
	if err == nil {
		t.Fatal("switching an expense to a transfer kept its category and one leg, and was accepted")
	}
}
```

`fakeHouseholdRepo`, `staticTestFX` and `fixedClock` already exist in
`testdouble_test.go` from the accounts work — reuse them; if the field names
differ, follow what is there rather than renaming.

- [ ] **Step 3: Run it and watch it fail**

```bash
cd api && go test ./internal/usecase/ -run TestCreate -v
```

Expected: FAIL — `undefined: usecase.NewTransactionService`.

- [ ] **Step 4: Write the service**

`api/internal/usecase/transaction.go`:

```go
package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// NewTransaction is the create input.
//
// It carries no currency field, deliberately. A transaction is denominated in
// its account's currency, so the service derives it -- a request that could
// name one would be a field a handler accepts and never persists, which is the
// shape four defects in this project have had. The same goes for the received
// amount: only its figure crosses the wire, and its currency comes from the
// destination account.
type NewTransaction struct {
	HouseholdID         string
	Kind                string
	OccurredOn          time.Time
	Description         string
	CategoryID          string
	PaidByMembershipID  string
	FromAccountID       string
	ToAccountID         string
	AmountMinor         int64
	ReceivedAmountMinor *int64
}

// TransactionUpdate is a real patch: a nil pointer means "leave this alone".
//
// ClearReceivedAmount is a separate bool rather than a **int64 because the two
// states a caller needs -- "leave it" and "remove it" -- are otherwise
// indistinguishable from a nil pointer. It is how a transfer that stops
// crossing currencies loses the figure that no longer applies.
type TransactionUpdate struct {
	Kind                *string
	OccurredOn          *time.Time
	Description         *string
	CategoryID          *string
	PaidByMembershipID  *string
	FromAccountID       *string
	ToAccountID         *string
	AmountMinor         *int64
	ReceivedAmountMinor *int64
	ClearReceivedAmount bool
}

// TransactionDeps gathers every port TransactionService needs, mirroring
// AccountDeps.
type TransactionDeps struct {
	Transactions TransactionRepository
	Categories   CategoryLookup
	Accounts     AccountLookup
	Households   HouseholdRepository
	FX           FXRateProvider
	Clock        Clock
}

// TransactionService covers the ledger: the transactions themselves and the
// month summary computed from them (monthsummary.go).
//
// It takes no actor parameter, by the rule this codebase follows: services
// enforce what is *valid*, middleware enforces who is *asking*. Every
// transactions route is gated on the money capability and on owner in the
// router.
type TransactionService struct {
	d TransactionDeps
}

func NewTransactionService(d TransactionDeps) *TransactionService {
	return &TransactionService{d: d}
}

func (s *TransactionService) List(ctx context.Context, householdID string, f TransactionFilter) ([]TransactionView, error) {
	return s.d.Transactions.List(ctx, householdID, f)
}

func (s *TransactionService) Get(ctx context.Context, householdID, id string) (TransactionView, error) {
	return s.d.Transactions.Get(ctx, householdID, id)
}

func (s *TransactionService) Delete(ctx context.Context, householdID, id string) error {
	return s.d.Transactions.Delete(ctx, householdID, id)
}

func (s *TransactionService) Create(ctx context.Context, in NewTransaction) (domain.Transaction, error) {
	t := domain.Transaction{
		HouseholdID:        in.HouseholdID,
		Kind:               domain.TransactionKind(in.Kind),
		OccurredOn:         in.OccurredOn,
		Description:        in.Description,
		CategoryID:         in.CategoryID,
		PaidByMembershipID: in.PaidByMembershipID,
		FromAccountID:      in.FromAccountID,
		ToAccountID:        in.ToAccountID,
		Amount:             domain.Money{Amount: in.AmountMinor},
	}
	if in.ReceivedAmountMinor != nil {
		t.ReceivedAmount = &domain.Money{Amount: *in.ReceivedAmountMinor}
	}
	if err := s.validate(ctx, &t); err != nil {
		return domain.Transaction{}, err
	}
	return s.d.Transactions.Create(ctx, t)
}

// Update merges the patch onto the stored transaction and validates the
// *result*, never the incoming fields. That ordering is the point: switching a
// kind to transfer and leaving a category alone are each legal in isolation
// and illegal together, so validating the patch would let the pair through.
// AccountService.Update is the same shape for the same reason.
func (s *TransactionService) Update(ctx context.Context, householdID, id string, patch TransactionUpdate) (domain.Transaction, error) {
	view, err := s.d.Transactions.Get(ctx, householdID, id)
	if err != nil {
		return domain.Transaction{}, err
	}
	t := view.Transaction

	if patch.Kind != nil {
		t.Kind = domain.TransactionKind(*patch.Kind)
	}
	if patch.OccurredOn != nil {
		t.OccurredOn = *patch.OccurredOn
	}
	if patch.Description != nil {
		t.Description = *patch.Description
	}
	if patch.CategoryID != nil {
		t.CategoryID = *patch.CategoryID
	}
	if patch.PaidByMembershipID != nil {
		t.PaidByMembershipID = *patch.PaidByMembershipID
	}
	if patch.FromAccountID != nil {
		t.FromAccountID = *patch.FromAccountID
	}
	if patch.ToAccountID != nil {
		t.ToAccountID = *patch.ToAccountID
	}
	if patch.AmountMinor != nil {
		t.Amount.Amount = *patch.AmountMinor
	}
	if patch.ClearReceivedAmount {
		t.ReceivedAmount = nil
	} else if patch.ReceivedAmountMinor != nil {
		t.ReceivedAmount = &domain.Money{Amount: *patch.ReceivedAmountMinor}
	}

	if err := s.validate(ctx, &t); err != nil {
		return domain.Transaction{}, err
	}
	return s.d.Transactions.Update(ctx, t)
}

// validate normalises and checks an assembled transaction in place. Shared by
// Create and Update so the two cannot drift -- the defect class this project
// has hit six times is a rule fixed at one call site while its sibling keeps
// the bug.
func (s *TransactionService) validate(ctx context.Context, t *domain.Transaction) error {
	kind, err := domain.ParseTransactionKind(string(t.Kind))
	if err != nil {
		return err
	}
	t.Kind = kind

	t.Description = strings.TrimSpace(t.Description)
	if t.Description == "" {
		return domain.ErrTransactionDescriptionRequired
	}

	if t.Amount.Amount <= 0 {
		return domain.ErrTransactionAmountNotPositive
	}

	// The account combination the kind requires, mirroring the
	// accounts_match_kind constraint. One sentinel for every wrong shape: the
	// screen shows one message beside the account pickers, and separate errors
	// for "not yours" and "does not exist" would tell a caller which ids are
	// real elsewhere.
	switch t.Kind {
	case domain.TransactionExpense:
		if t.FromAccountID == "" || t.ToAccountID != "" {
			return domain.ErrTransactionAccountsInvalid
		}
	case domain.TransactionIncome:
		if t.ToAccountID == "" || t.FromAccountID != "" {
			return domain.ErrTransactionAccountsInvalid
		}
	case domain.TransactionTransfer:
		if t.FromAccountID == "" || t.ToAccountID == "" || t.FromAccountID == t.ToAccountID {
			return domain.ErrTransactionAccountsInvalid
		}
	}

	// The currencies come from the accounts, never from the request.
	var fromCurrency, toCurrency string
	if t.FromAccountID != "" {
		view, err := s.d.Accounts.Get(ctx, t.HouseholdID, t.FromAccountID)
		if err != nil {
			return accountLookupError(err)
		}
		fromCurrency = view.Balance.Currency
	}
	if t.ToAccountID != "" {
		view, err := s.d.Accounts.Get(ctx, t.HouseholdID, t.ToAccountID)
		if err != nil {
			return accountLookupError(err)
		}
		toCurrency = view.Balance.Currency
	}

	// The amount is denominated in the account money left, or arrived in.
	if fromCurrency != "" {
		t.Amount.Currency = fromCurrency
	} else {
		t.Amount.Currency = toCurrency
	}

	if err := s.validateReceivedAmount(t, fromCurrency, toCurrency); err != nil {
		return err
	}

	if err := s.validateCategory(ctx, t); err != nil {
		return err
	}

	// An account can only be paid for by someone in this household. The check
	// lives on AccountLookup because *postgres.AccountRepo already answers it
	// for account ownership, and a second port asking the same question of the
	// same table would be two answers waiting to disagree.
	if t.PaidByMembershipID != "" {
		ok, err := s.d.Accounts.MembershipBelongsToHousehold(ctx, t.HouseholdID, t.PaidByMembershipID)
		if err != nil {
			return err
		}
		if !ok {
			return domain.ErrTransactionAccountsInvalid
		}
	}
	return nil
}

// validateReceivedAmount implements decision 3: required when a transfer
// crosses currencies, optional when it does not (a bank fee), and refused on
// anything that is not a transfer.
func (s *TransactionService) validateReceivedAmount(t *domain.Transaction, fromCurrency, toCurrency string) error {
	if t.Kind != domain.TransactionTransfer {
		if t.ReceivedAmount != nil {
			return domain.ErrReceivedAmountNotAllowed
		}
		return nil
	}
	if t.ReceivedAmount == nil {
		if fromCurrency != toCurrency {
			return domain.ErrReceivedAmountRequired
		}
		return nil
	}
	if t.ReceivedAmount.Amount <= 0 {
		return domain.ErrTransactionAmountNotPositive
	}
	t.ReceivedAmount.Currency = toCurrency
	return nil
}

// validateCategory keeps the ledger's promise that a category feeds Budget
// spend: a transfer is not spend, and an income is not Groceries.
func (s *TransactionService) validateCategory(ctx context.Context, t *domain.Transaction) error {
	if t.CategoryID == "" {
		return nil
	}
	if t.Kind == domain.TransactionTransfer {
		return domain.ErrCategoryKindMismatch
	}
	ok, err := s.d.Categories.BelongsToHousehold(ctx, t.HouseholdID, t.CategoryID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrCategoryKindMismatch
	}
	kind, err := s.d.Categories.Kind(ctx, t.HouseholdID, t.CategoryID)
	if err != nil {
		return err
	}
	want := domain.CategoryExpense
	if t.Kind == domain.TransactionIncome {
		want = domain.CategoryIncome
	}
	if kind != want {
		return domain.ErrCategoryKindMismatch
	}
	return nil
}

// accountLookupError turns "there is no such account in this household" into
// the same sentinel every other wrong-account case returns. A distinct error
// here would let a caller tell an id that exists in another household from one
// that does not exist at all.
func accountLookupError(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrTransactionAccountsInvalid
	}
	return err
}
```

**Note for the implementer:** `AccountLookup` in `ports.go` therefore carries
two methods, not one:

```go
type AccountLookup interface {
    Get(ctx context.Context, householdID, accountID string) (AccountView, error)
    MembershipBelongsToHousehold(ctx context.Context, householdID, membershipID string) (bool, error)
}
```

`*postgres.AccountRepo` already satisfies both. Add
`MembershipBelongsToHousehold` to `fakeAccountLookup` in
`testdouble_test.go` — returning true for a small set of known ids and false
otherwise — and add `"errors"` to `transaction.go`'s imports.

Also add `Kind` to `postgres.CategoryRepo`, using the `GetCategoryKind` query
written in Task 5:

```go
func (r *CategoryRepo) Kind(ctx context.Context, householdID, categoryID string) (domain.CategoryKind, error) {
	kind, err := r.q.GetCategoryKind(ctx, sqlcgen.GetCategoryKindParams{
		ID:          uuid(categoryID),
		HouseholdID: uuid(householdID),
	})
	if err != nil {
		return "", translate(err, "get category kind")
	}
	return domain.CategoryKind(kind), nil
}
```

- [ ] **Step 5: Run the tests**

```bash
cd api && go test ./internal/usecase/ -v
```

Expected: PASS.

- [ ] **Step 6: Mutation-check the merged-result rule**

In `Update`, temporarily validate the incoming patch instead of the merged
transaction (move the `validate` call above the merge and pass a
freshly-assembled transaction from the patch alone). Confirm
`TestUpdateValidatesTheMergedResult` goes **red**. Restore.

- [ ] **Step 7: Commit**

```bash
git add api/internal/usecase/ api/internal/adapter/postgres/category_repo.go
git commit -m "feat(usecase): add TransactionService and its validation

NewTransaction has no currency field: the service derives it from the account,
so there is no field a handler can accept and never persist. Update validates
the merged result, not the patch."
```

---

## Task 11: The month summary — the two derived figures

**Files:**
- Create: `api/internal/usecase/monthsummary.go`, `api/internal/usecase/monthsummary_test.go`

**Interfaces:**
- Consumes: `TransactionDeps` (Task 10).
- Produces:
  - `type ExcludedTransaction struct { TransactionID, Currency string }`
  - `type MonthSummary struct { Currency string; Month time.Time; Count int; Spent domain.Money; ExcludedNoRate []ExcludedTransaction }`
  - `func (s *TransactionService) MonthSummary(ctx context.Context, householdID string, month time.Time) (MonthSummary, error)`

- [ ] **Step 1: Write the failing test**

`api/internal/usecase/monthsummary_test.go`:

```go
package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestSpentCountsExpensesOnly(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()
	july := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "expense", OccurredOn: july.AddDate(0, 0, 17),
		Description: "Cold Storage", CategoryID: "cat-groceries",
		FromAccountID: "dbs", AmountMinor: 5230,
	})
	mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "income", OccurredOn: july.AddDate(0, 0, 15),
		Description: "Bonus", CategoryID: "cat-income",
		ToAccountID: "dbs", AmountMinor: 120000,
	})
	mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "transfer", OccurredOn: july.AddDate(0, 0, 10),
		Description: "To savings", FromAccountID: "dbs", ToAccountID: "ocbc",
		AmountMinor: 50000,
	})

	got, err := svc.MonthSummary(ctx, "house-1", july)
	if err != nil {
		t.Fatalf("month summary: %v", err)
	}
	// The count is what the ledger shows: all three kinds.
	if got.Count != 3 {
		t.Fatalf("count = %d, want 3 (every kind)", got.Count)
	}
	// Spent is expenses only. Income is not spending, and a transfer is the
	// same money arriving somewhere else -- counting either would tell a
	// household it spent money it still has.
	if got.Spent.Amount != 5230 {
		t.Fatalf("spent = %d, want 5230 (the expense alone)", got.Spent.Amount)
	}
}

// domain.Money.Add refuses to add two currencies, deliberately. Summing first
// and converting after fails on the second transaction of a mixed-currency
// household -- LEARNING.md pattern 12, the same order AccountService.Summary
// uses.
func TestSpentConvertsEachTransactionBeforeSumming(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()
	july := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "expense", OccurredOn: july.AddDate(0, 0, 17),
		Description: "Cold Storage", CategoryID: "cat-groceries",
		FromAccountID: "dbs", AmountMinor: 5230,
	})
	// An IDR expense. staticTestFX knows SGD<->IDR, so this converts.
	mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "expense", OccurredOn: july.AddDate(0, 0, 12),
		Description: "Warung", CategoryID: "cat-groceries",
		FromAccountID: "bca", AmountMinor: 5000000,
	})

	got, err := svc.MonthSummary(ctx, "house-1", july)
	if err != nil {
		t.Fatalf("month summary: %v", err)
	}
	if got.Currency != "SGD" {
		t.Fatalf("currency = %q, want the household's SGD", got.Currency)
	}
	if got.Spent.Amount <= 5230 {
		t.Fatalf("spent = %d, want more than the SGD expense alone -- the IDR one did not convert",
			got.Spent.Amount)
	}
	if len(got.ExcludedNoRate) != 0 {
		t.Fatalf("excluded %d transactions that had a rate", len(got.ExcludedNoRate))
	}
}

// A quietly short total looks identical to a correct one. Net worth already
// follows this rule; the ledger follows the same one.
func TestATransactionWithNoRateIsExcludedAndNamed(t *testing.T) {
	svc, _ := transactionFixtureWithAccount(t, "usd-card", "USD")
	ctx := context.Background()
	july := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "expense", OccurredOn: july.AddDate(0, 0, 8),
		Description: "Steam", CategoryID: "cat-groceries",
		FromAccountID: "usd-card", AmountMinor: 3999,
	})

	got, err := svc.MonthSummary(ctx, "house-1", july)
	if err != nil {
		t.Fatalf("month summary: %v", err)
	}
	if got.Spent.Amount != 0 {
		t.Fatalf("spent = %d, want 0 -- nothing convertible was spent", got.Spent.Amount)
	}
	if len(got.ExcludedNoRate) != 1 || got.ExcludedNoRate[0].Currency != "USD" {
		t.Fatalf("excluded = %v, want one USD transaction named", got.ExcludedNoRate)
	}
}

// Decision 6's split, asserted in one test because it is the thing that will
// get "simplified" later: the balance ignores a transaction dated before the
// account's opening date, and spend does not. The money was spent.
func TestSpendCountsATransactionDatedBeforeTheAccountsOpeningBalance(t *testing.T) {
	svc, _ := transactionFixture(t)
	ctx := context.Background()
	july := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	// The fixture's accounts open on 1 July; this is the 2nd, and the account
	// was added later -- the ordinary "log yesterday's lunch on an account I
	// added today" case.
	mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "expense", OccurredOn: july.AddDate(0, 0, 1),
		Description: "Kopitiam", CategoryID: "cat-groceries",
		FromAccountID: "dbs", AmountMinor: 840,
	})

	got, err := svc.MonthSummary(ctx, "house-1", july)
	if err != nil {
		t.Fatalf("month summary: %v", err)
	}
	if got.Spent.Amount != 840 {
		t.Fatalf("spent = %d, want 840 -- a transaction the balance ignores was still spent",
			got.Spent.Amount)
	}
}

func mustCreate(t *testing.T, svc *usecase.TransactionService, in usecase.NewTransaction) {
	t.Helper()
	if _, err := svc.Create(context.Background(), in); err != nil {
		t.Fatalf("create %q: %v", in.Description, err)
	}
}
```

`transactionFixtureWithAccount` is `transactionFixture` with one extra entry in
the `fakeAccountLookup` currency map — add it beside the existing fixture
helper rather than duplicating the whole builder.

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/usecase/ -run TestSpent -v
```

Expected: FAIL — `svc.MonthSummary undefined`.

- [ ] **Step 3: Write it**

`api/internal/usecase/monthsummary.go`:

```go
package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// ExcludedTransaction names one transaction left out of the month's spend
// because no rate was available, so the screen can say which currency. An
// explicit field rather than something the frontend infers by comparing the
// list to the total: a total that is quietly short looks identical to a
// correct one, which is the failure worth preventing.
type ExcludedTransaction struct {
	TransactionID string
	Currency      string
}

// MonthSummary is the two figures the Transactions screen shows above the
// ledger. Both were undefined before this feature; they are pinned here.
//
// Count is "247 in July" -- every transaction in the month, all three kinds,
// because it counts what the ledger below it is showing.
//
// Spent is "Spent this month S$3,420.18" -- expenses only. Income is not
// spending, and a transfer is the same money arriving somewhere else; counting
// either would tell a household it spent money it still has.
type MonthSummary struct {
	Currency       string
	Month          time.Time
	Count          int
	Spent          domain.Money
	ExcludedNoRate []ExcludedTransaction
}

// MonthSummary composes both figures from one read of the month.
//
// The order of operations is not incidental: domain.Money.Add refuses to add
// two different currencies, deliberately, so each expense is converted into
// the household's primary currency *first* and only then summed. Summing first
// and converting after fails on the second transaction of a mixed-currency
// household -- docs/LEARNING.md pattern 12, and the same order
// AccountService.Summary uses for the same reason. Rounding therefore happens
// per transaction (half away from zero, as Rate.Apply already does) and the
// total is never re-rounded, so the figure is deterministic.
//
// A transaction dated before its account's opening balance still counts here.
// The money was spent; only the account's *balance* ignores it, because a
// balance is anchored to a figure someone asserted was true on a date and
// spend is not. See the spec's decision 6 -- this split is the thing that will
// get "simplified" later.
func (s *TransactionService) MonthSummary(ctx context.Context, householdID string, month time.Time) (MonthSummary, error) {
	household, err := s.d.Households.Get(ctx, householdID)
	if err != nil {
		return MonthSummary{}, err
	}
	primary := household.PrimaryCurrency

	zero, err := domain.NewMoney(0, primary)
	if err != nil {
		return MonthSummary{}, err
	}

	views, err := s.d.Transactions.MonthTotals(ctx, householdID, month)
	if err != nil {
		return MonthSummary{}, err
	}

	summary := MonthSummary{
		Currency: primary,
		Month:    month,
		Count:    len(views),
		Spent:    zero,
	}

	for _, view := range views {
		if view.Transaction.Kind != domain.TransactionExpense {
			continue
		}
		inPrimary, err := s.convert(ctx, view.Transaction.Amount, primary)
		if err != nil {
			summary.ExcludedNoRate = append(summary.ExcludedNoRate, ExcludedTransaction{
				TransactionID: view.Transaction.ID,
				Currency:      view.Transaction.Amount.Currency,
			})
			continue
		}
		summary.Spent, err = summary.Spent.Add(inPrimary)
		if err != nil {
			return MonthSummary{}, err
		}
	}
	return summary, nil
}

// convert turns one amount into the household's primary currency. A
// same-currency amount short-circuits without consulting the provider at all:
// that is the overwhelmingly common case, it is exact, and it means a
// single-currency household never depends on a rate table it does not need.
//
// This duplicates AccountService.convert deliberately rather than sharing it:
// the two services declare their own dependencies, and hoisting this into a
// shared helper would give one service a reason to change when the other's FX
// needs do.
func (s *TransactionService) convert(ctx context.Context, m domain.Money, primary string) (domain.Money, error) {
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

- [ ] **Step 4: Run the tests**

```bash
cd api && go test ./internal/usecase/ -v
```

Expected: PASS.

- [ ] **Step 5: Mutation-check the convert-then-add order**

Temporarily sum the raw amounts and convert the total at the end. Confirm
`TestSpentConvertsEachTransactionBeforeSumming` goes **red** — `Money.Add`
refuses the currency mix. Restore.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/monthsummary.go api/internal/usecase/monthsummary_test.go
git commit -m "feat(usecase): pin the month's count and spend

Spent is expenses only, converted per transaction before summing. A
transaction with no rate is excluded and named -- a quietly short total looks
identical to a correct one."
```

---

## Task 12: The HTTP layer

**Files:**
- Create: `api/internal/adapter/http/transaction_handlers.go`,
  `api/internal/adapter/http/category_handlers.go`
- Modify: `api/internal/adapter/http/errors.go`

**Interfaces:**
- Consumes: `TransactionService`, `CategoryService`.
- Produces: `handleListTransactions`, `handleCreateTransaction`,
  `handleUpdateTransaction`, `handleDeleteTransaction`, `handleListCategories`,
  all `func(deps Deps) http.HandlerFunc`. `Deps` gains
  `Transactions *usecase.TransactionService` and
  `Categories *usecase.CategoryService` in Task 13.

- [ ] **Step 1: Map the new sentinels**

In `api/internal/adapter/http/errors.go`, add to the existing sentinel table:

```go
	case errors.Is(err, domain.ErrTransactionDescriptionRequired):
		WriteError(w, http.StatusUnprocessableEntity, "DESCRIPTION_REQUIRED",
			"Give this transaction a description.", nil)
	case errors.Is(err, domain.ErrUnknownTransactionKind):
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_KIND",
			"That is not a kind of transaction Hearth records.", nil)
	case errors.Is(err, domain.ErrTransactionAmountNotPositive):
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_AMOUNT",
			"Enter an amount greater than zero. Whether it adds or subtracts comes from the kind.", nil)
	// One message for every wrong-account shape, including an account in
	// another household: separate ones would tell a caller which ids are real
	// elsewhere.
	case errors.Is(err, domain.ErrTransactionAccountsInvalid):
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_ACCOUNTS",
			"Choose accounts that match this kind of transaction.", nil)
	case errors.Is(err, domain.ErrReceivedAmountRequired):
		WriteError(w, http.StatusUnprocessableEntity, "RECEIVED_AMOUNT_REQUIRED",
			"These accounts are in different currencies. Enter what actually arrived.", nil)
	case errors.Is(err, domain.ErrReceivedAmountNotAllowed):
		WriteError(w, http.StatusUnprocessableEntity, "RECEIVED_AMOUNT_NOT_ALLOWED",
			"Only a transfer records an amount received.", nil)
	case errors.Is(err, domain.ErrCategoryKindMismatch):
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_CATEGORY",
			"That category does not belong to this kind of transaction.", nil)
	case errors.Is(err, domain.ErrUnknownCategoryKind):
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_CATEGORY",
			"That category does not belong to this kind of transaction.", nil)
```

- [ ] **Step 2: Write the transaction handlers**

`api/internal/adapter/http/transaction_handlers.go`:

```go
package httpadapter

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// occurredOnLayout is the wire format for a transaction's date: a calendar
// date, because "18 July" is a fact about a day. Same layout and same reason
// as openingBalanceLayout in account_handlers.go.
const occurredOnLayout = "2006-01-02"

// monthLayout is the wire format for the month filter.
const monthLayout = "2006-01"

type transactionDTO struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	OccurredOn  string `json:"occurredOn"`
	Description string `json:"description"`

	CategoryID   *string `json:"categoryId"`
	CategoryName *string `json:"categoryName"`

	PaidByMembershipID *string `json:"paidByMembershipId"`
	PaidByName         *string `json:"paidByName"`

	FromAccountID   *string `json:"fromAccountId"`
	FromAccountName *string `json:"fromAccountName"`
	ToAccountID     *string `json:"toAccountId"`
	ToAccountName   *string `json:"toAccountName"`

	Amount         moneyDTO  `json:"amount"`
	ReceivedAmount *moneyDTO `json:"receivedAmount"`

	// Two flags, not one: a transfer has two accounts with two opening dates
	// and can predate one without predating the other. null means there is no
	// account on that side at all.
	BeforeFromAccountOpeningBalance *bool `json:"beforeFromAccountOpeningBalance"`
	BeforeToAccountOpeningBalance   *bool `json:"beforeToAccountOpeningBalance"`
}

type monthSummaryDTO struct {
	Currency       string                  `json:"currency"`
	Month          string                  `json:"month"`
	Count          int                     `json:"count"`
	SpentMinor     int64                   `json:"spentMinor"`
	ExcludedNoRate []excludedTransactionDTO `json:"excludedNoRate"`
}

type excludedTransactionDTO struct {
	TransactionID string `json:"transactionId"`
	Currency      string `json:"currency"`
}

type transactionsResponse struct {
	Transactions []transactionDTO `json:"transactions"`
	// null on the last page. That is what the "Load older transactions" link
	// hides itself on -- a row count would be wrong on a page that happens to
	// be exactly full.
	NextCursor *string         `json:"nextCursor"`
	Summary    monthSummaryDTO `json:"summary"`
}

// handleListTransactions serves the ledger and the two figures above it
// together, because they are one screen and must describe the same month.
func handleListTransactions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Every transactions route sits behind requireCapability and
		// requireOwner (router.go), so by the time this runs the caller is an
		// owner holding money and the explicit check other handlers make would
		// be dead code. Unlike accounts, there is no redaction branch here at
		// all: a limited member never reaches this handler.
		scope, _ := RequestScope(r)

		filter, month, ok := parseTransactionFilter(w, r)
		if !ok {
			return
		}

		views, err := deps.Transactions.List(r.Context(), scope.HouseholdID, filter)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		summary, err := deps.Transactions.MonthSummary(r.Context(), scope.HouseholdID, month)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		out := transactionsResponse{
			Transactions: make([]transactionDTO, 0, len(views)),
			Summary:      toMonthSummaryDTO(summary),
		}
		// The repository returns limit+1 rows so we can tell there is another
		// page without counting the table. The extra row is the cursor, not
		// content -- returning it would show one row twice.
		limit := filter.Limit
		if len(views) > limit {
			last := views[limit-1]
			cursor := encodeCursor(last.Transaction.OccurredOn, last.Transaction.ID)
			out.NextCursor = &cursor
			views = views[:limit]
		}
		for _, v := range views {
			out.Transactions = append(out.Transactions, toTransactionDTO(v))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// encodeCursor and decodeCursor are the opaque page marker: the date and id of
// the last row of a page, which is exactly what the keyset predicate needs.
// Opaque to the frontend on purpose -- it must not construct one, or a change
// to the sort order becomes a breaking change to the client.
func encodeCursor(occurredOn time.Time, id string) string {
	return occurredOn.Format(occurredOnLayout) + ":" + id
}

func decodeCursor(raw string) (time.Time, string, bool) {
	// A date is exactly ten characters, so the split point is fixed and an id
	// containing a colon cannot confuse it.
	if len(raw) < len(occurredOnLayout)+2 || raw[len(occurredOnLayout)] != ':' {
		return time.Time{}, "", false
	}
	date, err := time.Parse(occurredOnLayout, raw[:len(occurredOnLayout)])
	if err != nil {
		return time.Time{}, "", false
	}
	return date, raw[len(occurredOnLayout)+1:], true
}

// parseTransactionFilter reads the design's five filters plus paging. It
// answers the month separately because the summary is always about a month
// even when the ledger is not filtered to one -- an unfiltered ledger still
// shows "247 in July".
func parseTransactionFilter(w http.ResponseWriter, r *http.Request) (usecase.TransactionFilter, time.Time, bool) {
	q := r.URL.Query()
	filter := usecase.TransactionFilter{
		Kind:               q.Get("kind"),
		AccountID:          q.Get("account_id"),
		CategoryID:         q.Get("category_id"),
		PaidByMembershipID: q.Get("paid_by"),
		Limit:              defaultPageSize,
	}

	month := time.Now().UTC()
	if raw := q.Get("month"); raw != "" {
		parsed, err := time.Parse(monthLayout, raw)
		if err != nil {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_MONTH",
				"That month could not be read. Use YYYY-MM.", nil)
			return usecase.TransactionFilter{}, time.Time{}, false
		}
		month = parsed
		filter.Month = parsed
	}

	if raw := q.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_LIMIT",
				"Limit must be a positive whole number.", nil)
			return usecase.TransactionFilter{}, time.Time{}, false
		}
		filter.Limit = parsed
	}

	if raw := q.Get("cursor"); raw != "" {
		date, id, ok := decodeCursor(raw)
		if !ok {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_CURSOR",
				"That page marker could not be read.", nil)
			return usecase.TransactionFilter{}, time.Time{}, false
		}
		filter.CursorDate, filter.CursorID = date, id
	}
	return filter, month, true
}

const defaultPageSize = 50
```

Continue the same file with the DTO converters and the three write handlers:

```go
func toTransactionDTO(v usecase.TransactionView) transactionDTO {
	t := v.Transaction
	dto := transactionDTO{
		ID:          t.ID,
		Kind:        string(t.Kind),
		OccurredOn:  t.OccurredOn.Format(occurredOnLayout),
		Description: t.Description,
		Amount:      moneyDTO{AmountMinor: t.Amount.Amount, Currency: t.Amount.Currency},

		BeforeFromAccountOpeningBalance: v.BeforeFromAccountOpening,
		BeforeToAccountOpeningBalance:   v.BeforeToAccountOpening,
	}
	// "" means the column is NULL, and the wire form of absent is JSON null --
	// not an empty string, which would read as a real id that happens to be
	// blank. Same convention as accountDTO.OwnerMembershipID.
	if t.CategoryID != "" {
		id, name := t.CategoryID, v.CategoryName
		dto.CategoryID, dto.CategoryName = &id, &name
	}
	if t.PaidByMembershipID != "" {
		id, name := t.PaidByMembershipID, v.PaidByName
		dto.PaidByMembershipID, dto.PaidByName = &id, &name
	}
	if t.FromAccountID != "" {
		id, name := t.FromAccountID, v.FromAccountName
		dto.FromAccountID, dto.FromAccountName = &id, &name
	}
	if t.ToAccountID != "" {
		id, name := t.ToAccountID, v.ToAccountName
		dto.ToAccountID, dto.ToAccountName = &id, &name
	}
	if t.ReceivedAmount != nil {
		dto.ReceivedAmount = &moneyDTO{
			AmountMinor: t.ReceivedAmount.Amount,
			Currency:    t.ReceivedAmount.Currency,
		}
	}
	return dto
}

func toMonthSummaryDTO(s usecase.MonthSummary) monthSummaryDTO {
	dto := monthSummaryDTO{
		Currency:       s.Currency,
		Month:          s.Month.Format(monthLayout),
		Count:          s.Count,
		SpentMinor:     s.Spent.Amount,
		ExcludedNoRate: make([]excludedTransactionDTO, 0, len(s.ExcludedNoRate)),
	}
	for _, ex := range s.ExcludedNoRate {
		dto.ExcludedNoRate = append(dto.ExcludedNoRate, excludedTransactionDTO{
			TransactionID: ex.TransactionID, Currency: ex.Currency,
		})
	}
	return dto
}

// createTransactionRequest carries no currency, deliberately: a transaction is
// denominated in its account's currency and the service derives it. A field
// here that the service overwrote would be a field this handler accepts and
// never persists -- the shape guarding-partial-writes exists for.
type createTransactionRequest struct {
	Kind                string  `json:"kind"`
	OccurredOn          string  `json:"occurredOn"`
	Description         string  `json:"description"`
	CategoryID          *string `json:"categoryId"`
	PaidByMembershipID  *string `json:"paidByMembershipId"`
	FromAccountID       *string `json:"fromAccountId"`
	ToAccountID         *string `json:"toAccountId"`
	AmountMinor         int64   `json:"amountMinor"`
	ReceivedAmountMinor *int64  `json:"receivedAmountMinor"`
}

// updateTransactionRequest's fields are all pointers so a field the caller did
// not name reaches usecase.TransactionUpdate as nil and keeps its stored
// value -- the same real-patch convention TestUpdateHouseholdIsARealPatch
// pins.
//
// clearReceivedAmount is how a transfer that stops crossing currencies loses
// the figure that no longer applies: with pointers alone, "remove it" and
// "leave it" are the same nil.
type updateTransactionRequest struct {
	Kind                *string `json:"kind"`
	OccurredOn          *string `json:"occurredOn"`
	Description         *string `json:"description"`
	CategoryID          *string `json:"categoryId"`
	PaidByMembershipID  *string `json:"paidByMembershipId"`
	FromAccountID       *string `json:"fromAccountId"`
	ToAccountID         *string `json:"toAccountId"`
	AmountMinor         *int64  `json:"amountMinor"`
	ReceivedAmountMinor *int64  `json:"receivedAmountMinor"`
	ClearReceivedAmount bool    `json:"clearReceivedAmount"`
}

func handleCreateTransaction(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req createTransactionRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		occurredOn, err := time.Parse(occurredOnLayout, req.OccurredOn)
		if err != nil {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_DATE",
				"That date could not be read. Use YYYY-MM-DD.", nil)
			return
		}

		in := usecase.NewTransaction{
			HouseholdID:         scope.HouseholdID,
			Kind:                req.Kind,
			OccurredOn:          occurredOn,
			Description:         req.Description,
			AmountMinor:         req.AmountMinor,
			ReceivedAmountMinor: req.ReceivedAmountMinor,
		}
		if req.CategoryID != nil {
			in.CategoryID = *req.CategoryID
		}
		if req.PaidByMembershipID != nil {
			in.PaidByMembershipID = *req.PaidByMembershipID
		}
		if req.FromAccountID != nil {
			in.FromAccountID = *req.FromAccountID
		}
		if req.ToAccountID != nil {
			in.ToAccountID = *req.ToAccountID
		}

		created, err := deps.Transactions.Create(r.Context(), in)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeTransaction(w, r, deps, scope.HouseholdID, created.ID, http.StatusCreated)
	}
}

func handleUpdateTransaction(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req updateTransactionRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		patch := usecase.TransactionUpdate{
			Kind:                req.Kind,
			Description:         req.Description,
			CategoryID:          req.CategoryID,
			PaidByMembershipID:  req.PaidByMembershipID,
			FromAccountID:       req.FromAccountID,
			ToAccountID:         req.ToAccountID,
			AmountMinor:         req.AmountMinor,
			ReceivedAmountMinor: req.ReceivedAmountMinor,
			ClearReceivedAmount: req.ClearReceivedAmount,
		}
		if req.OccurredOn != nil {
			occurredOn, err := time.Parse(occurredOnLayout, *req.OccurredOn)
			if err != nil {
				WriteError(w, http.StatusUnprocessableEntity, "INVALID_DATE",
					"That date could not be read. Use YYYY-MM-DD.", nil)
				return
			}
			patch.OccurredOn = &occurredOn
		}

		id := chi.URLParam(r, "id")
		if _, err := deps.Transactions.Update(r.Context(), scope.HouseholdID, id, patch); err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeTransaction(w, r, deps, scope.HouseholdID, id, http.StatusOK)
	}
}

// handleDeleteTransaction answers 204 with no body -- the one status in this
// API permitted to carry none, and permitted because apiFetch does not try to
// parse it. A transaction is hard deleted: nothing references it, so nothing
// is orphaned. See the spec's decision 8 for why this differs from accounts.
func handleDeleteTransaction(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		if err := deps.Transactions.Delete(r.Context(), scope.HouseholdID, chi.URLParam(r, "id")); err != nil {
			MapDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// writeTransaction re-reads through Get so every mutating response carries the
// joined names the write queries do not return -- the same reason
// writeAccount does it.
func writeTransaction(w http.ResponseWriter, r *http.Request, deps Deps, householdID, id string, status int) {
	view, err := deps.Transactions.Get(r.Context(), householdID, id)
	if err != nil {
		MapDomainError(w, r, err)
		return
	}
	WriteJSON(w, status, toTransactionDTO(view))
}
```

- [ ] **Step 3: Write the categories handler**

`api/internal/adapter/http/category_handlers.go`:

```go
package httpadapter

import "net/http"

type categoryDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type categoriesResponse struct {
	Categories []categoryDTO `json:"categories"`
}

// handleListCategories is the modal's dropdown. It is also what seeds a
// household's starter set the first time anything asks -- see
// CategoryService.List for why a read is the moment that does it.
func handleListCategories(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		categories, err := deps.Categories.List(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		out := categoriesResponse{Categories: make([]categoryDTO, 0, len(categories))}
		for _, c := range categories {
			out.Categories = append(out.Categories, categoryDTO{
				ID: c.ID, Name: c.Name, Kind: string(c.Kind),
			})
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

```

This file needs no `usecase` import at all — it reads `deps.Categories` and
`domain.Category`'s exported fields, nothing more. Do not commit a blank
identifier.

- [ ] **Step 4: Confirm it compiles**

```bash
cd api && go build ./...
```

Expected: fails only on `Deps.Transactions` / `Deps.Categories`, which Task 13
adds. If anything else fails, fix it here.

- [ ] **Step 5: Commit**

```bash
git add api/internal/adapter/http/transaction_handlers.go \
        api/internal/adapter/http/category_handlers.go \
        api/internal/adapter/http/errors.go
git commit -m "feat(http): add the transaction and category handlers

The request struct carries no currency: the service derives it from the
account, so there is no field the handler accepts and never persists. DELETE
answers 204, the one status permitted to carry no body."
```

---

## Task 13: Routes, guards and wiring

**Files:**
- Modify: `api/internal/adapter/http/router.go`, `api/internal/adapter/http/api_test.go`,
  `api/cmd/api/main.go`

**Interfaces:**
- Consumes: every handler from Task 12, both services, both repositories.
- Produces: the five live routes; `Deps.Transactions`, `Deps.Categories`.

- [ ] **Step 1: Write the failing guard test**

Extend the route-walk matrices in `api/internal/adapter/http/api_test.go`. This
is the test that proves decision 5 rather than assuming it — a limited member
holding `money` is refused the ledger entirely, **reads included**, which is
where transactions differ from accounts.

```go
// Every transactions route requires money AND owner, reads included -- unlike
// accounts, whose read is open to any money holder. A ledger with every figure
// blanked is not a product, so a limited member gets no page at all rather
// than an empty one. See the spec's decision 5. The obvious "fix" is to make
// the two consistent, which is why this test names the difference.
func TestTransactionRoutesRequireMoneyAndOwner(t *testing.T) {
	env := newTestEnv(t)

	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/transactions"},
		{http.MethodPost, "/api/v1/transactions"},
		{http.MethodPatch, "/api/v1/transactions/" + uuidZero},
		{http.MethodDelete, "/api/v1/transactions/" + uuidZero},
		{http.MethodGet, "/api/v1/categories"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			// No session at all.
			res := env.do(t, route.method, route.path, nil, noSession)
			if res.StatusCode != http.StatusUnauthorized {
				t.Fatalf("no session = %d, want 401", res.StatusCode)
			}

			// Signed in, but without the money capability.
			res = env.do(t, route.method, route.path, nil, env.signIn(t, env.limitedEmail, env.limitedPassword))
			if res.StatusCode != http.StatusForbidden {
				t.Fatalf("no money capability = %d, want 403", res.StatusCode)
			}

			// A limited member who DOES hold money. Refused anyway -- this is
			// the case that separates transactions from accounts.
			res = env.do(t, route.method, route.path, nil,
				env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword))
			if res.StatusCode != http.StatusForbidden {
				t.Fatalf("limited member holding money = %d, want 403", res.StatusCode)
			}

			// An owner reaches the handler. 404 for the two id routes (that id
			// does not exist), and not 401/403 for the rest -- what matters
			// here is that the guards let them through.
			res = env.do(t, route.method, route.path, nil, env.signIn(t, env.ownerEmail, env.ownerPassword))
			if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
				t.Fatalf("owner = %d, want the handler to be reached", res.StatusCode)
			}
		})
	}
}
```

Follow the existing matrices' helper names exactly — `env.do`, `env.signIn`,
`noSession` and `uuidZero` are illustrative; use whatever `api_test.go` already
provides, and add `uuidZero` only if there is no equivalent.

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/adapter/http/ -run TestTransactionRoutesRequire -v
```

Expected: FAIL — the routes do not exist, so an owner gets 404 where the test
wants the handler reached, and the limited cases get 404 rather than 403.

- [ ] **Step 3: Add the `Deps` fields and the routes**

In `api/internal/adapter/http/router.go`, add to `Deps`:

```go
	Transactions *usecase.TransactionService
	Categories   *usecase.CategoryService
```

and, beside the accounts group:

```go
			// Transactions requires money AND owner for reads as well as
			// writes, which is deliberately unlike accounts above.
			//
			// A limited member's accounts view shows names with no amounts
			// (accounts decision 5). Applied to a ledger that is a table whose
			// every figure is blank, next to a "Spent this month" that has to
			// be absent rather than zero -- a page that reads as broken. So
			// for a limited member the money capability means "see which
			// accounts this household has" and nothing further. Do not
			// "simplify" this to match the accounts group.
			g.Group(func(txn chi.Router) {
				txn.Use(requireCapability(domain.CapMoney))
				txn.Use(requireOwner)

				txn.Get("/transactions", handleListTransactions(deps))
				txn.Get("/categories", handleListCategories(deps))

				txn.Group(func(w chi.Router) {
					w.Use(requireCSRF)
					w.Post("/transactions", handleCreateTransaction(deps))
					w.Patch("/transactions/{id}", handleUpdateTransaction(deps))
					w.Delete("/transactions/{id}", handleDeleteTransaction(deps))
				})
			})
```

The capability guard is stacked on top of `requireOwner` even though an owner
without `money` is not a representable state today
(`domain.ValidateMembershipChange`). That is accounts decision 4's reasoning,
unchanged: the routes must not lean on an invariant enforced in another layer
for another reason, because relaxing it would silently open every route that
leaned on it, with no failing test.

- [ ] **Step 4: Wire it in `main.go`**

In `api/cmd/api/main.go`, beside the account wiring:

```go
	categoryRepo := postgres.NewCategoryRepo(db)
	transactionRepo := postgres.NewTransactionRepo(db)

	categoryService := usecase.NewCategoryService(categoryRepo)
	transactionService := usecase.NewTransactionService(usecase.TransactionDeps{
		Transactions: transactionRepo,
		Categories:   categoryRepo,
		Accounts:     accountRepo,
		Households:   householdRepo,
		FX:           fxProvider,
		Clock:        clock,
	})
```

and add both to the `httpadapter.Deps` literal. Use the variable names already
in that file rather than these if they differ.

- [ ] **Step 5: Run the tests**

```bash
cd api && go test ./... 
```

Expected: PASS, the whole API suite.

- [ ] **Step 6: Mutation-check the owner guard**

This is the plan's designated mutation check for decision 5. Remove
`txn.Use(requireOwner)` from the router and confirm
`TestTransactionRoutesRequireMoneyAndOwner` goes **red** on the
"limited member holding money" case. Restore, confirm green. If nothing goes
red, the guard matrix is decoration and the decision is unprotected.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/http/router.go api/internal/adapter/http/api_test.go \
        api/cmd/api/main.go
git commit -m "feat(http): wire the transactions and categories routes

Reads require owner too, unlike accounts: a ledger with every figure blanked
is not a product, so a limited member gets no page rather than an empty one.
The route-walk matrix proves it rather than assuming it."
```

---

## Task 14: Frontend schemas and hooks

**Files:**
- Create: `web/src/features/money/transactionSchemas.ts`,
  `web/src/features/money/useTransactions.ts`,
  `web/src/features/money/transactionCopy.ts`

**Interfaces:**
- Consumes: the DTOs from Task 12.
- Produces:
  - `transactionSchema`, `transactionsResponseSchema`, `categoriesResponseSchema`,
    and the inferred types `Transaction`, `TransactionsResponse`, `Category`
  - `useTransactions(filters)`, `useCategories()`, `useCreateTransaction()`,
    `useUpdateTransaction()`, `useDeleteTransaction()`
  - `TRANSACTIONS_COPY`

- [ ] **Step 1: Write the schemas**

`web/src/features/money/transactionSchemas.ts`:

```ts
// Zod mirrors of the DTOs in api/internal/adapter/http/transaction_handlers.go
// (transactionDTO, monthSummaryDTO) and category_handlers.go. These follow the
// backend's own structs rather than the design, because the backend's comments
// are what say which fields can be absent and why.
import { z } from "zod";

export const transactionKindSchema = z.enum(["expense", "income", "transfer"]);
export type TransactionKind = z.infer<typeof transactionKindSchema>;

const moneySchema = z.object({
  amountMinor: z.number(),
  currency: z.string(),
});

// The two before-opening flags are nullable rather than boolean: null means
// there is no account on that side at all, which is different from "there is
// one and this transaction does not predate it". A transfer can predate one
// side and not the other, which is why there are two.
export const transactionSchema = z.object({
  id: z.string(),
  kind: transactionKindSchema,
  occurredOn: z.string(),
  description: z.string(),
  categoryId: z.string().nullable(),
  categoryName: z.string().nullable(),
  paidByMembershipId: z.string().nullable(),
  paidByName: z.string().nullable(),
  fromAccountId: z.string().nullable(),
  fromAccountName: z.string().nullable(),
  toAccountId: z.string().nullable(),
  toAccountName: z.string().nullable(),
  amount: moneySchema,
  receivedAmount: moneySchema.nullable(),
  beforeFromAccountOpeningBalance: z.boolean().nullable(),
  beforeToAccountOpeningBalance: z.boolean().nullable(),
});
export type Transaction = z.infer<typeof transactionSchema>;

const excludedTransactionSchema = z.object({
  transactionId: z.string(),
  currency: z.string(),
});

export const monthSummarySchema = z.object({
  currency: z.string(),
  month: z.string(),
  count: z.number(),
  spentMinor: z.number(),
  excludedNoRate: z.array(excludedTransactionSchema),
});
export type MonthSummary = z.infer<typeof monthSummarySchema>;

export const transactionsResponseSchema = z.object({
  transactions: z.array(transactionSchema),
  // null on the last page. The "Load older transactions" link keys off this
  // and not off a row count, which would be wrong on a page that happens to be
  // exactly full.
  nextCursor: z.string().nullable(),
  summary: monthSummarySchema,
});
export type TransactionsResponse = z.infer<typeof transactionsResponseSchema>;

export const categorySchema = z.object({
  id: z.string(),
  name: z.string(),
  kind: z.enum(["expense", "income"]),
});
export type Category = z.infer<typeof categorySchema>;

export const categoriesResponseSchema = z.object({
  categories: z.array(categorySchema),
});
```

- [ ] **Step 2: Write the copy**

`web/src/features/money/transactionCopy.ts`:

```ts
// Copy for the Transactions screen, in a plain .ts module for the same reason
// features/money/copy.ts is: eslint's react-refresh/only-export-components
// rule never has to think about a file that mixes components with other
// exports.
export const TRANSACTIONS_COPY = {
  title: "All transactions",
  backToFinances: "‹ Finances",
  add: "+ Add transaction",
  loadOlder: "Load older transactions ↓",
  spentThisMonth: "Spent this month",
  countInMonth: (count: number, month: string) =>
    `${count} in ${month}`,
  // The design's own banner. It is the promise categories exist to keep.
  categoriesFeedBudget:
    "Every expense's category feeds that category's Budget spend automatically.",

  // First run. A household sees this the day after it adds its first account,
  // so it is a real screen rather than an edge case.
  emptyTitle: "Nothing logged yet.",
  emptyBody: "Add an expense, some income or a transfer, and it will show up here.",
  // Deliberately different from the above: a household that filtered to
  // "Income · Petrol" and saw the first-run panel would think its ledger had
  // been wiped.
  noMatchesTitle: "Nothing matches those filters.",
  noMatchesAction: "Clear filters",

  // The button is disabled rather than opening a modal whose account dropdown
  // is empty -- a dead end reached after four clicks.
  noAccountsYet: "Add an account first, and transactions can attach to it.",

  excludedNoRate: (count: number, currencies: string) =>
    `${count} ${count === 1 ? "transaction is" : "transactions are"} not counted: no exchange rate for ${currencies}.`,

  // Names the account, because a transfer can predate one side's opening
  // balance and not the other's -- "does not move the balance" would be half
  // true without saying which.
  beforeOpeningBalance: (accountName: string) =>
    `Before ${accountName}'s opening balance — it doesn't change that balance.`,

  amountReceived: "Amount received",
  amountReceivedHint: (currency: string) =>
    `What actually arrived, in ${currency}.`,
} as const;
```

- [ ] **Step 3: Write the hooks**

`web/src/features/money/useTransactions.ts`, following `useAccounts.ts`
exactly — including the returned (not fired-and-forgotten) invalidation, which
is the defect `CurrencyPanel` documents:

```ts
import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { accountsQueryKey } from "./useAccounts";
import {
  categoriesResponseSchema,
  transactionSchema,
  transactionsResponseSchema,
  type Category,
  type Transaction,
  type TransactionsResponse,
} from "./transactionSchemas";

export type TransactionFilters = {
  kind?: string;
  accountId?: string;
  categoryId?: string;
  paidBy?: string;
  month?: string;
  cursor?: string;
};

export function transactionsQueryKey(filters: TransactionFilters) {
  return ["transactions", filters] as const;
}

export function categoriesQueryKey() {
  return ["categories"] as const;
}

function toQueryString(filters: TransactionFilters): string {
  const params = new URLSearchParams();
  if (filters.kind) params.set("kind", filters.kind);
  if (filters.accountId) params.set("account_id", filters.accountId);
  if (filters.categoryId) params.set("category_id", filters.categoryId);
  if (filters.paidBy) params.set("paid_by", filters.paidBy);
  if (filters.month) params.set("month", filters.month);
  if (filters.cursor) params.set("cursor", filters.cursor);
  const query = params.toString();
  return query ? `?${query}` : "";
}

export function useTransactions(filters: TransactionFilters) {
  return useQuery({
    queryKey: transactionsQueryKey(filters),
    queryFn: async (): Promise<TransactionsResponse> => {
      const body = await apiFetch<unknown>(`/api/v1/transactions${toQueryString(filters)}`);
      return transactionsResponseSchema.parse(body);
    },
  });
}

export function useCategories() {
  return useQuery({
    queryKey: categoriesQueryKey(),
    queryFn: async (): Promise<Category[]> => {
      const body = await apiFetch<unknown>("/api/v1/categories");
      return categoriesResponseSchema.parse(body).categories;
    },
  });
}

// Every transaction mutation invalidates the accounts queries too: a
// transaction changes an account's balance and the net worth built from it, so
// leaving those cached shows a ledger that disagrees with the Finances page
// one click away.
//
// Returned, not fired-and-forgotten: TanStack Query awaits a mutation's
// onSuccess return value before treating it as settled, and settled is what
// isPending -- every submit button's disabled condition -- reflects. Skipping
// the return re-enables the button while the list is still serving stale data.
// That is the defect web/src/features/settings/CurrencyPanel.tsx:49 documents.
function invalidateLedger(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: ["transactions"] }),
    queryClient.invalidateQueries({ queryKey: accountsQueryKey(false) }),
    queryClient.invalidateQueries({ queryKey: accountsQueryKey(true) }),
  ]);
}

export function useCreateTransaction() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: unknown): Promise<Transaction> => {
      const raw = await apiFetch<unknown>("/api/v1/transactions", {
        method: "POST",
        body: JSON.stringify(body),
      });
      return transactionSchema.parse(raw);
    },
    onSuccess: () => invalidateLedger(queryClient),
  });
}

export function useUpdateTransaction() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, body }: { id: string; body: unknown }): Promise<Transaction> => {
      const raw = await apiFetch<unknown>(`/api/v1/transactions/${id}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      });
      return transactionSchema.parse(raw);
    },
    onSuccess: () => invalidateLedger(queryClient),
  });
}

// DELETE answers 204 with no body, which apiFetch does not try to parse. Every
// other 2xx in this API carries JSON for exactly that reason.
export function useDeleteTransaction() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<void> => {
      await apiFetch<unknown>(`/api/v1/transactions/${id}`, { method: "DELETE" });
    },
    onSuccess: () => invalidateLedger(queryClient),
  });
}
```

**Already verified:** `apiFetch` returns `undefined` for a 204 without trying to
parse a body — `web/src/api/client.ts:108`. The delete hook above relies on
that, and this is the product's only 204. If that branch is ever removed,
delete stops working with an `INVALID_RESPONSE` throw and nothing else changes.

- [ ] **Step 4: Typecheck**

```bash
make typecheck
```

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/money/transactionSchemas.ts \
        web/src/features/money/useTransactions.ts \
        web/src/features/money/transactionCopy.ts
git commit -m "feat(web): add transaction schemas, hooks and copy

Every transaction mutation invalidates the accounts queries too -- a
transaction moves a balance, and a stale Finances page one click away would
disagree with the ledger."
```

---

## Task 15: The "Log a transaction" modal

**Files:**
- Create: `web/src/features/money/TransactionModal.tsx`,
  `web/src/features/money/TransactionModal.test.tsx`

**Interfaces:**
- Consumes: `components/Modal`, `useCategories`, `useAccounts`, the copy module.
- Produces: `TransactionModal` with props
  `{ open: boolean; onClose: () => void; onSubmit: (values: TransactionFormValues) => Promise<unknown>; onDelete?: () => Promise<unknown>; initial?: Transaction; accounts: Account[]; members: { id: string; name: string }[] }`
  and the exported type `TransactionFormValues`, matching
  `createTransactionRequest` field for field. `initial` is what makes the same
  component serve edit; `onDelete` is passed only then, and renders the delete
  control behind an in-page confirmation (never `window.confirm`, which blocks
  every browser event).

- [ ] **Step 1: Write the failing test**

`web/src/features/money/TransactionModal.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TransactionModal } from "./TransactionModal";

const accounts = [
  { id: "dbs", nickname: "DBS Everyday", currency: "SGD" },
  { id: "bca", nickname: "BCA Tahapan", currency: "IDR" },
];

// Follow whatever shape AccountsPanel.test.tsx already builds its account
// fixtures with; these three fields are what the modal reads.

describe("TransactionModal", () => {
  it("offers a category for an expense and no category at all for a transfer", async () => {
    const user = userEvent.setup();
    renderModal();

    expect(screen.getByLabelText(/category/i)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Transfer" }));
    expect(screen.queryByLabelText(/category/i)).not.toBeInTheDocument();
    expect(screen.getByLabelText(/from account/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/to account/i)).toBeInTheDocument();
  });

  // The field is optional within one currency -- a bank fee -- and required
  // across two, where there is no honest figure to prefill it with.
  it("requires the amount received only when the two accounts differ in currency", async () => {
    const user = userEvent.setup();
    renderModal();

    await user.click(screen.getByRole("button", { name: "Transfer" }));
    await user.selectOptions(screen.getByLabelText(/from account/i), "dbs");
    await user.selectOptions(screen.getByLabelText(/to account/i), "ocbc");

    const optional = screen.getByLabelText(/amount received/i);
    expect(optional).not.toBeRequired();

    await user.selectOptions(screen.getByLabelText(/to account/i), "bca");
    const required = screen.getByLabelText(/amount received/i);
    expect(required).toBeRequired();
    // Labelled with the destination's currency, not the household's.
    expect(screen.getByText(/IDR/)).toBeInTheDocument();
  });

  it("sends no currency field at all — the server derives it from the account", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    renderModal({ onSubmit });

    await user.type(screen.getByLabelText(/description/i), "Cold Storage");
    await user.type(screen.getByLabelText(/^amount/i), "52.30");
    await user.selectOptions(screen.getByLabelText(/account/i), "dbs");
    await user.click(screen.getByRole("button", { name: /save transaction/i }));

    expect(onSubmit).toHaveBeenCalledTimes(1);
    const body = onSubmit.mock.calls[0][0];
    expect(body).not.toHaveProperty("currency");
    expect(body).not.toHaveProperty("amountCurrency");
    // Minor units, never a float: 52.30 is 5230 cents.
    expect(body.amountMinor).toBe(5230);
  });
});
```

Write `renderModal` as a small local helper in the same file, following the way
`AccountModal.test.tsx` sets its modal up (the `<dialog>` primitive needs the
same treatment).

- [ ] **Step 2: Run it and watch it fail**

```bash
cd web && npx vitest run src/features/money/TransactionModal.test.tsx
```

Expected: FAIL — the module does not exist.

- [ ] **Step 3: Build the modal**

Follow `AccountModal.tsx` closely — same `components/Modal` primitive (the one
that reaches genuine `:modal` state; **do not reintroduce a declarative `open`
attribute**), same form-state approach, same error rendering. The parts
specific to this modal:

- **Three kind buttons** — Expense, Income, Transfer — switching which fields
  render:
  - Expense: amount, date, description, category (expense categories only),
    account, paid by.
  - Income: amount, date, description, category (income categories only),
    account. **No "paid by"** — nobody paid.
  - Transfer: amount, date, description, from account, to account, **no
    category**, and the amount-received field.
- **The amount-received field is always present on a transfer**, prefilled with
  the amount sent and optional while the two accounts share a currency; when
  they differ it starts empty and is required, and its hint names the
  destination's currency. There is no honest figure to prefill across
  currencies.
- **Amounts are parsed to minor units in one helper**, reusing the existing
  `formatMoney` module's inverse if there is one; if not, add `parseMinorUnits`
  there rather than inline in the component, so the modal and any later screen
  cannot disagree about "52.30".
- **No currency field anywhere in the submitted body.** The server derives it
  from the account. A field here that the server ignores is the shape
  `guarding-partial-writes` exists for.
- The design's connected-bank header strip is dropped, as `AccountModal` drops
  it, for the same reason: it describes a sync that does not exist.

- [ ] **Step 4: Run the tests**

```bash
cd web && npx vitest run src/features/money/TransactionModal.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/money/TransactionModal.tsx \
        web/src/features/money/TransactionModal.test.tsx
git commit -m "feat(web): add the Log a transaction modal

The amount-received field is prefilled and optional within one currency and
empty and required across two -- there is no honest figure to prefill with
when the bank's rate is what decides it."
```

---

## Task 16: The Transactions page

**Files:**
- Create: `web/src/features/money/TransactionFilters.tsx`,
  `web/src/features/money/TransactionsPage.tsx`,
  `web/src/features/money/TransactionsPage.test.tsx`

**Interfaces:**
- Consumes: `useTransactions`, `useCategories`, `useAccounts`,
  `TransactionModal`, the copy module.
- Produces: `TransactionsPage`, mounted by the router in Task 17.

- [ ] **Step 1: Write the failing test**

`web/src/features/money/TransactionsPage.test.tsx`. Use `stubFetchRoutes` for
**every** request — it matches on method and URL and throws on anything
unregistered:

```tsx
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TransactionsPage } from "./TransactionsPage";

// Follow the stub and provider setup FinancesPage.test.tsx already uses.

describe("TransactionsPage", () => {
  it("shows the first-run panel when the household has logged nothing", async () => {
    renderPage({ transactions: [], summary: { count: 0, spentMinor: 0 } });
    expect(await screen.findByText(/nothing logged yet/i)).toBeInTheDocument();
  });

  // A household that filtered to "Income · Petrol" and saw the first-run panel
  // would think its ledger had been wiped.
  it("distinguishes an empty ledger from filters that match nothing", async () => {
    const user = userEvent.setup();
    renderPage({
      transactions: [expenseFixture()],
      summary: { count: 1, spentMinor: 5230 },
      filtered: { transactions: [], summary: { count: 0, spentMinor: 0 } },
    });

    await user.selectOptions(await screen.findByLabelText(/kind/i), "income");
    expect(await screen.findByText(/nothing matches those filters/i)).toBeInTheDocument();
    expect(screen.queryByText(/nothing logged yet/i)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /clear filters/i })).toBeInTheDocument();
  });

  it("hides Load older transactions when there is no next cursor", async () => {
    renderPage({
      transactions: [expenseFixture()],
      summary: { count: 1, spentMinor: 5230 },
      nextCursor: null,
    });
    await screen.findByText("Cold Storage");
    expect(screen.queryByRole("button", { name: /load older/i })).not.toBeInTheDocument();
  });

  it("shows Load older transactions when the server sent a cursor", async () => {
    renderPage({
      transactions: [expenseFixture()],
      summary: { count: 1, spentMinor: 5230 },
      nextCursor: "2026-07-16:txn-9",
    });
    expect(await screen.findByRole("button", { name: /load older/i })).toBeInTheDocument();
  });

  // A quietly short total looks identical to a correct one.
  it("names the transactions left out of the month's spend", async () => {
    renderPage({
      transactions: [expenseFixture()],
      summary: {
        count: 1,
        spentMinor: 0,
        excludedNoRate: [{ transactionId: "txn-1", currency: "USD" }],
      },
    });
    expect(await screen.findByText(/no exchange rate for USD/i)).toBeInTheDocument();
  });

  // Naming the account matters: a transfer can predate one side's opening
  // balance and not the other's.
  it("marks a row that predates its account's opening balance, naming the account", async () => {
    renderPage({
      transactions: [{ ...expenseFixture(), beforeFromAccountOpeningBalance: true }],
      summary: { count: 1, spentMinor: 5230 },
    });
    expect(await screen.findByText(/before DBS Everyday's opening balance/i)).toBeInTheDocument();
  });

  it("disables Add transaction when the household has no accounts", async () => {
    renderPage({ transactions: [], summary: { count: 0, spentMinor: 0 }, accounts: [] });
    expect(await screen.findByRole("button", { name: /add transaction/i })).toBeDisabled();
    expect(screen.getByText(/add an account first/i)).toBeInTheDocument();
  });

  // Editing is how a mistyped row gets corrected instead of deleted and
  // retyped, and it is the only caller PATCH /transactions/{id} has. Without
  // this the endpoint and its hook exist with nothing reaching them.
  it("opens the modal populated when a row is clicked, and patches on save", async () => {
    const user = userEvent.setup();
    const { patched } = renderPage({
      transactions: [expenseFixture()],
      summary: { count: 1, spentMinor: 5230 },
    });

    await user.click(await screen.findByRole("button", { name: /Cold Storage/i }));

    // Populated, not blank: an edit form that opens empty silently clears
    // every field the person does not retype.
    expect(screen.getByLabelText(/description/i)).toHaveValue("Cold Storage");
    expect(screen.getByLabelText(/^amount/i)).toHaveValue("52.30");

    await user.clear(screen.getByLabelText(/description/i));
    await user.type(screen.getByLabelText(/description/i), "Cold Storage — milk");
    await user.click(screen.getByRole("button", { name: /save transaction/i }));

    expect(patched).toHaveBeenCalledWith(
      "/api/v1/transactions/txn-1",
      expect.objectContaining({ description: "Cold Storage — milk" }),
    );
  });

  it("removes a transaction and asks for confirmation first", async () => {
    const user = userEvent.setup();
    const { deleted } = renderPage({
      transactions: [expenseFixture()],
      summary: { count: 1, spentMinor: 5230 },
    });

    await user.click(await screen.findByRole("button", { name: /Cold Storage/i }));
    await user.click(screen.getByRole("button", { name: /delete/i }));
    // In-page confirmation, never window.confirm: a native dialog blocks every
    // browser event and would freeze an automated walk.
    await user.click(screen.getByRole("button", { name: /yes, delete/i }));

    expect(deleted).toHaveBeenCalledWith("/api/v1/transactions/txn-1");
  });
});
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd web && npx vitest run src/features/money/TransactionsPage.test.tsx
```

Expected: FAIL — the module does not exist.

- [ ] **Step 3: Build the filter bar**

`TransactionFilters.tsx` — the design's five controls: kind (All / Expense /
Income as a segmented control), account, category, person, month. Each is a
labelled control (the tests query by label, and a filter nobody can reach by
keyboard is a filter half the household cannot use). Filter state lives in the
page and is passed down; the component itself holds none, so the page can clear
all five at once.

- [ ] **Step 4: Build the page**

`TransactionsPage.tsx`, in the shape `FinancesPage.tsx` already uses:

- Header: `‹ Finances`, the title, and the count line built from
  `summary.count` and `summary.month`. The design's "auto-imported from linked
  accounts" is **dropped** — nothing is imported, and copy that describes a
  sync this product cannot have is a promise it cannot keep.
- The design's banner about categories feeding Budget spend.
- The filter bar, with "Spent this month" and its figure on the right.
- The ledger, grouped by day with the design's date headings, each row showing
  description, account · person · category, and the amount — negative for an
  expense, positive and green for income, and a transfer showing both sides.
- The five screen states from the spec's section 7.1: first run, no matches,
  excluded-no-rate line, a marked pre-opening row, and the disabled add button
  when there are no accounts.
- Paging appends: "Load older transactions" fetches with the `nextCursor` and
  appends to what is shown. It renders only when `nextCursor` is not null.
- **Each ledger row is a button that opens the modal populated with that
  transaction**, submitting through `useUpdateTransaction`. Same component as
  add — the only difference is whether it opens populated. This is the only
  caller `PATCH /api/v1/transactions/{id}` has; without it the endpoint and its
  hook exist with nothing reaching them.
- **The populated modal carries a Delete control**, behind an in-page
  confirmation — never `window.confirm`, which blocks every browser event and
  would freeze the Task 19 walk. It submits through `useDeleteTransaction`.
  Deletion is permanent (decision 8), so the confirmation says so rather than
  offering an undo that does not exist.

- [ ] **Step 5: Run the tests**

```bash
cd web && npx vitest run src/features/money/
```

Expected: PASS.

- [ ] **Step 6: Mutation-check the two empty states**

Temporarily render the first-run panel for both empty cases (drop the
"are any filters set" condition). Confirm
`distinguishes an empty ledger from filters that match nothing` goes **red**.
Restore.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/money/TransactionFilters.tsx \
        web/src/features/money/TransactionsPage.tsx \
        web/src/features/money/TransactionsPage.test.tsx
git commit -m "feat(web): add the Transactions page

Five screen states, and the two empty ones are deliberately different: a
household that filtered to something with no matches must not be shown the
first-run panel."
```

---

## Task 17: Finances gets its strip, and the route exists

**Files:**
- Create: `web/src/features/money/RecentTransactionsCard.tsx`
- Modify: `web/src/features/money/FinancesPage.tsx`,
  `web/src/features/money/FinancesPage.test.tsx`,
  `web/src/routes/router.tsx`, `web/src/routes/router.test.tsx`

**Interfaces:**
- Consumes: `useTransactions`, `TransactionsPage`.
- Produces: `/money/transactions` in the route tree; the recent-transactions
  card on Finances.

- [ ] **Step 1: Write the failing tests**

Add to `web/src/features/money/FinancesPage.test.tsx`:

```tsx
it("shows the five newest transactions with a link through to the ledger", async () => {
  renderFinances({ transactions: [/* six fixtures */] });
  expect(await screen.findByText(/recent transactions/i)).toBeInTheDocument();
  // Five, not six: the design's strip is a preview, and "See all" is the way
  // to the rest.
  expect(screen.getAllByTestId("recent-transaction-row")).toHaveLength(5);
  expect(screen.getByRole("link", { name: /see all/i })).toHaveAttribute(
    "href",
    "/money/transactions",
  );
});
```

Add to `web/src/routes/router.test.tsx`, following the router-walk test that is
already there:

```tsx
it("mounts the Transactions page under the money capability guard", async () => {
  // /money/transactions must sit under moneyGuardRoute, not beside it: a
  // member without the money capability reaching a ledger because the route
  // was hung off the shell is a UI guard that never ran.
  await renderAt("/money/transactions", { capabilities: ["calendar"] });
  expect(await screen.findByText(/you do not have access/i)).toBeInTheDocument();
});
```

Use whatever the existing router test's helpers and copy actually are.

- [ ] **Step 2: Run them and watch them fail**

```bash
cd web && npx vitest run src/features/money/FinancesPage.test.tsx src/routes/router.test.tsx
```

Expected: FAIL.

- [ ] **Step 3: Build the card and add the route**

`RecentTransactionsCard.tsx`: the design's strip — five newest, each with
description, `date · category · account`, and the amount. It reads through
`useTransactions({})` with no filters, so it shares the query cache with the
ledger rather than adding an endpoint.

In `FinancesPage.tsx`, render it where the design puts it. The card was
deferred by the accounts spec for having no data; it has data now.

In `web/src/routes/router.tsx`, add the route **under `moneyGuardRoute`**,
beside `moneyIndexRoute`, and before `moneySplatRoute` so the splat does not
swallow it:

```tsx
const moneyTransactionsRoute = createRoute({
  getParentRoute: () => moneyGuardRoute,
  path: "transactions",
  component: TransactionsPage,
});
```

and add it to `moneyGuardRoute.addChildren([...])`. Update the route-tree
comment at the top of the file — it is the map someone reads first, and a map
that omits a route is worse than none.

- [ ] **Step 4: Run the whole frontend suite**

```bash
make test-web && make typecheck && make lint-web
```

Expected: all clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/money/RecentTransactionsCard.tsx \
        web/src/features/money/FinancesPage.tsx \
        web/src/features/money/FinancesPage.test.tsx \
        web/src/routes/router.tsx web/src/routes/router.test.tsx
git commit -m "feat(web): add the recent-transactions strip and the ledger route

The strip was deferred by the accounts spec for having no data. It has data
now. The route sits under the money guard, and a test proves it."
```

---

## Task 18: The documents, in the same change

**Files:**
- Modify: `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`,
  `docs/LEARNING.md`, `docs/HANDOVER.md`

**Interfaces:**
- Consumes: everything Tasks 1–17 built.
- Produces: documents that are true. This is not tidy-up after the work; it is
  part of it. A diagram nobody trusts is worse than no diagram.

- [ ] **Step 1: `docs/SYSTEM_DESIGN.md`**

Use the **`maintaining-system-design`** skill. What changed:

- The `categories` and `transactions` tables in the data diagram, with their
  relationships to `households`, `accounts`, `categories` and `memberships`,
  and the `ON DELETE` behaviour of each foreign key — the cascade-versus-restrict
  reasoning belongs in the prose under the diagram, because that is where
  someone will try to change it.
- The five new routes and their guards in the route table. Note that
  transactions require owner for **reads**, unlike accounts.
- The reshaped accounts-balance query: `AccountView.Balance` is now a sum.
- Transactions as a real screen rather than a placeholder in the frontend map.

Change the prose under each diagram too, not only the diagram.

- [ ] **Step 2: `docs/FEATURE_TRACKER.md`**

- Move **Full ledger with filters**, **Add transaction (modal)** and **Recent
  transactions strip** from ⬜ to ✅.
- **Export CSV** stays ⬜, with decision 7's reason on the row: it needs a
  non-JSON response path out of the frontend, with its own guard and test.
- **Inline category editing** stays ⬜, with its reason: the modal already
  edits a category.
- Add a row for **Categories** — the design draws them only inside Budget, and
  this feature built them. Mark ✅ for the list and seeding; note that renaming,
  adding and archiving belong to Budget's "Edit categories".
- Update the "Where things stand" paragraph.
- **Recount the summary table by counting symbols per section**, not by
  adjusting the totals. They were wrong on the first attempt precisely because
  they were estimated.

- [ ] **Step 3: `docs/LEARNING.md`**

Add what this work taught. Likely entries, but write what actually happened
rather than these if they differ:

- **`RESTRICT` versus `CASCADE` on a foreign key that only fires under another
  cascade.** The instinct was `RESTRICT` on `from_account_id` — accounts are
  never deleted, so it looked free. It would have made deleting a household
  fail: the household cascade reaches its accounts, and the restrict from
  transactions blocks it. Found by reasoning about the cascade, not by a test.
  What would have caught it sooner: a test that deletes a household with data
  in it, which now exists.
- Whatever the keyset-paging and category-seeding mutation checks turned up.
- If any test passed against deliberately broken code during a mutation check,
  that is an entry in the "test that cannot fail" pattern with the disproof.

- [ ] **Step 4: `docs/HANDOVER.md`**

- Section 1's table: slice 2 now has Accounts **and** Transactions; Budget,
  Goals and Bills remain.
- Section 4: the next feature is **Budget**, and it must pin the five derived
  figures still undefined — `66% used`, `S$137/day left`, `on pace to save
  S$1,780`, `4 of 4 on track`, and unspent budget rolling into a nominated goal
  at month end. Transactions pinned only the two figures its own screen shows.
- Note that "Edit categories" is Budget's screen and the table is already
  there waiting for it.
- Record the browser walk's result (Task 19) once it has run.

- [ ] **Step 5: Commit**

```bash
git add docs/
git commit -m "docs: bring the tracker, design and learning log current with Transactions

Recounted the summary table by counting symbols rather than adjusting totals."
```

---

## Task 19: The browser walk

**Files:**
- Create: `docs/superpowers/plans/2026-07-29-hearth-transactions-verification.md`

**Interfaces:**
- Consumes: the whole feature.
- Produces: a written record of what passed and what did not. Slices 0–1,
  self-serve sign-up and Accounts each have one; this is the format.

jsdom's `<dialog>` is a stub, and five passing tests once hid a modal that
threw on every open in production. A green suite is not this claim.

- [ ] **Step 1: Start clean**

```bash
make down && make up      # not a bare `make up` -- see HANDOVER §5, a running
                          # stack keeps its already-succeeded migrate container
make seed
```

Check `docker ps` on **both** Docker engines first. This machine can run
colima and Docker Desktop at once, and a stale Docker Desktop stack silently
holds ports 5173/8080/8025 — an hour was lost to exactly that during the
Accounts walk.

- [ ] **Step 2: Write the criteria before walking**

Write all fifteen into the verification file first, then walk them. Criteria
written afterwards describe what happened rather than what should.

1. Signed in as Andreas, `/money/transactions` loads and the sidebar reaches it.
2. The category dropdown is populated on a **fresh** household — sign up a new
   one and confirm the starter set appears without any transaction existing.
3. Logging an expense on DBS lowers that account's balance on Finances by
   exactly the amount, and lowers net worth by the same.
4. Logging income on DBS raises both by exactly the amount.
5. A transfer between two SGD accounts moves both balances and leaves **net
   worth unchanged to the cent**.
6. A transfer from DBS (SGD) to BCA (IDR) requires the amount received, and
   credits BCA with the rupiah figure typed — not a converted one.
7. A same-currency transfer with a smaller received amount (a fee) is accepted,
   and net worth falls by exactly the fee.
8. An expense dated before the account's opening balance is saved, marked on
   the row naming the account, leaves the balance unchanged, and **is included**
   in "Spent this month".
9. "Spent this month" counts expenses only — logging income and a transfer does
   not move it.
10. The five filters each narrow the list, and the account filter shows a
    transfer under **both** of its accounts.
11. With more than 50 transactions, "Load older transactions" appends without
    repeating or skipping a row; adding one mid-scroll does not break it.
12. Clicking a ledger row opens the modal **populated**; changing the amount
    and saving moves the account balance by the difference. Deleting from that
    same modal removes the row and restores the balance.
13. A limited member holding Money (invite one, give them Money in Settings)
    sees **no** Transactions link and gets a 403 on `/api/v1/transactions` —
    reads included.
14. The Finances page shows the five newest transactions and "See all" reaches
    the ledger.
15. A household with no accounts sees the add button disabled with the
    explanation, not an empty modal.

- [ ] **Step 3: Walk them and record the result**

Record every criterion pass or fail, and for each failure what was done about
it. Criterion 12 of the Accounts walk was itself defective — "sign in as Kayla",
which is not executable, since seeded children are credential-less. If a
criterion here turns out not to be executable, fix the criterion in the record
and say so, rather than quietly skipping it.

- [ ] **Step 4: Fix what the walk finds, then re-walk what failed**

Every defect the walk finds gets a `docs/LEARNING.md` entry. Then use the
**`hunting-sibling-defects`** skill: fixing one instance has failed to fix the
class six times in this project.

- [ ] **Step 5: Final gate**

```bash
make lint && make test
```

Both green on the tree being integrated.

- [ ] **Step 6: Commit**

```bash
git add docs/superpowers/plans/2026-07-29-hearth-transactions-verification.md docs/
git commit -m "docs: record the Transactions browser walk"
```

---

## Definition of done

1. `make lint && make test` green.
2. The mutation checks in Tasks 1, 3, 5, 8, 9, 10, 11, 13 and 16 each ran, and
   each went red before being restored. **Task 9's is the designated one** —
   `>` to `>=` in the balance sum — because it protects the rule the whole
   before-opening-date decision rests on.
3. The browser walk passes, recorded in its own verification file.
4. `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md` and
   `docs/HANDOVER.md` updated as part of the work.
