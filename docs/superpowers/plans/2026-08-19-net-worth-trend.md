# Net Worth 12-Month Trend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Draw the design's twelve-month net worth bar chart on Finances, and the `▲ 2.1%` change on both Finances and Overview.

**Architecture:** No new table. A balance here is already derived from the opening balance plus transactions, so the series is derived too: one new grouped-by-month query returns each account's movement per calendar month, and `AccountService.Summary` walks backwards from the live balance it already computes (`bal(m−1) = bal(m) − movement(m)`). The trend is built **inside** `Summary`, reusing the per-account converted balances the headline is summed from, so the newest bar cannot disagree with the figure printed above it. The series rides on the existing `GET /accounts` response, which already carries the owner-only guard.

**Tech Stack:** Go 1.24, chi, pgx v5, sqlc, Postgres 16, testcontainers · React 19, TypeScript, Zod, TanStack Query, Tailwind v4, Vitest + Testing Library.

**Spec:** `docs/superpowers/specs/2026-08-19-hearth-net-worth-trend-design.md`

## Global Constraints

- **Clean architecture, enforced by `make lint-arch`.** `internal/domain` imports the standard library only; `internal/usecase` may add `internal/domain`; everything else lives under `internal/adapter/**` or `cmd/**`. No pgx, chi or sqlc type crosses out of the adapter layer.
- **Money is `int64` minor units plus an ISO 4217 code, everywhere.** `float64` never appears in a monetary path. The change percentage travels the wire as **integer basis points** (`210` = 2.10%), never as a float.
- **Zero is a claim.** A month with no known figure is `null`, never `0`. A suppressed percentage is absent, never `0`.
- **Authorisation exists only in the HTTP layer.** No service takes an actor parameter.
- **Every 2xx except 204 carries a JSON body.**
- **Fail closed on values you did not construct.** A currency or a type arriving from a database column needs a branch that refuses.
- **Comments say why, never what.** Exported things carry their contract in a doc comment.
- `go` is not on `PATH` in a bare shell on this machine — it is at `/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin`. Add it before any `go` or `make` command.
- The Go suite needs a Docker socket:
  ```bash
  export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
  export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
  export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
  ```

---

### Task 1: The monthly-movements query, port and repository

**Files:**
- Modify: `api/internal/adapter/postgres/queries/account.sql` (append at end)
- Modify: `api/internal/usecase/ports.go` (add `AccountMonthMovement` next to `AccountView`; add one method to `AccountRepository`)
- Modify: `api/internal/adapter/postgres/account_repo.go` (add `MonthlyMovements`)
- Modify: `api/internal/usecase/testdouble_test.go` (`fakeAccountRepo` must satisfy the widened port)
- Test: `api/internal/adapter/postgres/account_repo_test.go` (append)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `usecase.AccountMonthMovement{AccountID string; Month time.Time; Delta domain.Money}`
  - `AccountRepository.MonthlyMovements(ctx context.Context, householdID string, since time.Time) ([]AccountMonthMovement, error)`
  - `fakeAccountRepo.addMovement(m usecase.AccountMonthMovement)` for later usecase tests.

- [ ] **Step 1: Write the failing Postgres test**

Append to `api/internal/adapter/postgres/account_repo_test.go`. `july(day)`, `insertTestHousehold`, `insertTestAccountAsOf` and `openTestDB` already exist in this package's tests.

```go
// TestMonthlyMovementsSplitsTheBalanceExpressionByMonth is the trend's whole
// correctness argument in one test. The chart walks backwards from
// AccountView.Balance by subtracting these deltas, so this query and the
// balance_minor expression in ListAccounts must apply the same filter to the
// same rows. Change the >= to a > here and the oldest bars drift away from
// the headline figure -- silently, and by a plausible amount.
func TestMonthlyMovementsSplitsTheBalanceExpressionByMonth(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	accounts := postgres.NewAccountRepo(db)
	transactions := postgres.NewTransactionRepo(db)
	householdID := insertTestHousehold(t, db)

	// Opened on 1 June. The 1 June expense is ON the opening date, so it
	// counts (the start-of-day rule); the 31 May one is before it and does
	// not, because that history is already inside the opening figure.
	dbs := insertTestAccountAsOf(t, db, householdID, "DBS", "SGD", 100_000,
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))

	for _, tx := range []struct {
		on     time.Time
		amount int64
	}{
		{time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC), 5_000},
		{time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 1_000},
		{july(4), 2_000},
		{july(20), 3_000},
	} {
		if _, err := transactions.Create(ctx, domain.Transaction{
			HouseholdID: householdID, Kind: domain.TransactionExpense,
			OccurredOn: tx.on, Description: "Groceries",
			FromAccountID: dbs,
			Amount:        domain.Money{Amount: tx.amount, Currency: "SGD"},
		}); err != nil {
			t.Fatalf("create transaction on %s: %v", tx.on, err)
		}
	}

	got, err := accounts.MonthlyMovements(ctx, householdID,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("MonthlyMovements: %v", err)
	}

	byMonth := map[string]int64{}
	for _, m := range got {
		if m.AccountID != dbs {
			t.Fatalf("movement for account %s, want only %s", m.AccountID, dbs)
		}
		if m.Delta.Currency != "SGD" {
			t.Errorf("delta currency = %q, want SGD (the account's own)", m.Delta.Currency)
		}
		byMonth[m.Month.Format("2006-01")] = m.Delta.Amount
	}

	// June: only the 1st counts, and an expense leaves the account.
	if byMonth["2026-06"] != -1_000 {
		t.Errorf("June = %d, want -1000 (the 31 May expense is before the opening date)", byMonth["2026-06"])
	}
	if byMonth["2026-07"] != -5_000 {
		t.Errorf("July = %d, want -5000 (2000 + 3000)", byMonth["2026-07"])
	}
	if _, ok := byMonth["2026-05"]; ok {
		t.Errorf("May is present: %v -- a transaction before the opening date must not appear at all", byMonth)
	}

	// The invariant the trend rests on: opening balance plus every delta in
	// the window equals the balance the Finances screen prints.
	views, err := accounts.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var walked int64 = 100_000
	for _, delta := range byMonth {
		walked += delta
	}
	if got := balancesByNickname(t, views)["DBS"]; got != walked {
		t.Fatalf("balance = %d but opening plus the deltas is %d -- the two "+
			"expressions disagree, which is exactly what makes the chart lie", got, walked)
	}
}

// TestMonthlyMovementsCreditsTheReceivingSideInItsOwnCurrency covers the half
// a single-account test cannot: a cross-currency transfer credits the
// destination with received_amount_minor, which is what actually landed. Use
// amount_minor there and an IDR account would be credited a figure of SGD.
func TestMonthlyMovementsCreditsTheReceivingSideInItsOwnCurrency(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	accounts := postgres.NewAccountRepo(db)
	transactions := postgres.NewTransactionRepo(db)
	householdID := insertTestHousehold(t, db)

	dbs := insertTestAccountAsOf(t, db, householdID, "DBS", "SGD", 100_000, july(1))
	bca := insertTestAccountAsOf(t, db, householdID, "BCA", "IDR", 0, july(1))

	if _, err := transactions.Create(ctx, domain.Transaction{
		HouseholdID: householdID, Kind: domain.TransactionTransfer,
		OccurredOn: july(10), Description: "To Jakarta",
		FromAccountID: dbs, ToAccountID: bca,
		Amount:         domain.Money{Amount: 10_000, Currency: "SGD"},
		ReceivedAmount: &domain.Money{Amount: 124_100_000, Currency: "IDR"},
	}); err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	got, err := accounts.MonthlyMovements(ctx, householdID, july(1))
	if err != nil {
		t.Fatalf("MonthlyMovements: %v", err)
	}
	byAccount := map[string]usecase.AccountMonthMovement{}
	for _, m := range got {
		byAccount[m.AccountID] = m
	}
	if byAccount[dbs].Delta.Amount != -10_000 || byAccount[dbs].Delta.Currency != "SGD" {
		t.Errorf("DBS = %+v, want -10000 SGD", byAccount[dbs].Delta)
	}
	if byAccount[bca].Delta.Amount != 124_100_000 || byAccount[bca].Delta.Currency != "IDR" {
		t.Errorf("BCA = %+v, want 124100000 IDR -- what landed, in the account's own currency", byAccount[bca].Delta)
	}
}
```

Check `domain.Transaction`'s received-amount field name before running (`grep -n "ReceivedAmount" api/internal/domain/transaction.go`) and match it exactly; the rest of the test is unaffected.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
cd api && go test ./internal/adapter/postgres/ -run MonthlyMovements -count=1
```

Expected: FAIL — `accounts.MonthlyMovements undefined (type *postgres.AccountRepo has no field or method MonthlyMovements)`.

- [ ] **Step 3: Add the query**

Append to `api/internal/adapter/postgres/queries/account.sql`:

```sql
-- ListAccountMonthlyMovements is the twelve-month trend's only new read. One
-- row per account per calendar month that has any movement, summed in that
-- account's own currency -- no conversion happens here and none can, because
-- the FX provider lives in the usecase layer (MonthTotalsQuery says the same).
--
-- The filter is ListAccounts's balance expression split by month, and must
-- stay identical to it: the trend walks backwards from AccountView.Balance by
-- subtracting these deltas, so one row's difference makes the older bars
-- disagree with the headline figure while still looking plausible.
--
-- There is deliberately no upper bound on occurred_on, for the same reason
-- ListAccounts has none: a future-dated transaction is already inside the
-- balance the walk anchors on, so it must be inside these rows too. The
-- service buckets any month later than the current one into the current one.
-- name: ListAccountMonthlyMovements :many
SELECT account_id,
       month,
       SUM(delta_minor)::bigint AS delta_minor,
       currency
FROM (
    SELECT t.from_account_id AS account_id,
           DATE_TRUNC('month', t.occurred_on)::date AS month,
           -t.amount_minor AS delta_minor,
           a.opening_balance_currency AS currency
    FROM transactions t
    JOIN accounts a ON a.id = t.from_account_id
    WHERE a.household_id = sqlc.arg('household_id')
      AND t.occurred_on >= a.opening_balance_as_of
      AND t.occurred_on >= sqlc.arg('since')::date
    UNION ALL
    SELECT t.to_account_id,
           DATE_TRUNC('month', t.occurred_on)::date,
           COALESCE(t.received_amount_minor, t.amount_minor),
           a.opening_balance_currency
    FROM transactions t
    JOIN accounts a ON a.id = t.to_account_id
    WHERE a.household_id = sqlc.arg('household_id')
      AND t.occurred_on >= a.opening_balance_as_of
      AND t.occurred_on >= sqlc.arg('since')::date
) movements
GROUP BY account_id, month, currency
ORDER BY account_id, month;
```

Regenerate:

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
make sqlc
git diff --stat api/internal/adapter/postgres/sqlcgen/
```

Read the generated `ListAccountMonthlyMovementsParams` and `ListAccountMonthlyMovementsRow` in `sqlcgen/account.sql.go` and use whatever field names sqlc actually produced in Step 5.

- [ ] **Step 4: Add the port**

In `api/internal/usecase/ports.go`, directly after the `AccountView` struct:

```go
// AccountMonthMovement is one account's net movement across one calendar
// month, in that account's own currency. It is the twelve-month net worth
// chart's only new input.
//
// Delta is signed: money leaving the account is negative, money arriving is
// positive, and a month with no movement produces no row rather than a zero
// one -- a caller reads an absent month as "nothing moved", which is what an
// absent row means.
//
// Month is the first of the month at midnight, so two values for the same
// month compare equal.
type AccountMonthMovement struct {
	AccountID string
	Month     time.Time
	Delta     domain.Money
}
```

And inside the `AccountRepository` interface, after `List`:

```go
	// MonthlyMovements returns every account's per-month net movement from
	// since onward, counting only transactions dated on or after that
	// account's own opening_balance_as_of -- the same filter
	// AccountView.Balance is computed with. The two must stay the same: the
	// trend walks backwards from Balance by subtracting these, so a filter
	// that differs by one row makes the older bars wrong and plausible at the
	// same time.
	//
	// There is no upper bound on the transaction date, deliberately. Balance
	// has none either, so a future-dated transaction is already inside the
	// figure the walk anchors on and must be inside these rows too.
	//
	// Archived accounts are included; the caller decides what counts, exactly
	// as it does for Balance.
	MonthlyMovements(ctx context.Context, householdID string, since time.Time) ([]AccountMonthMovement, error)
```

- [ ] **Step 5: Implement the repository method**

In `api/internal/adapter/postgres/account_repo.go`, after `List`:

```go
func (r *AccountRepo) MonthlyMovements(ctx context.Context, householdID string, since time.Time) ([]usecase.AccountMonthMovement, error) {
	rows, err := r.q.ListAccountMonthlyMovements(ctx, sqlcgen.ListAccountMonthlyMovementsParams{
		HouseholdID: uuid(householdID),
		Since:       dateOnly(since),
	})
	if err != nil {
		return nil, translate(err, "list account monthly movements")
	}
	out := make([]usecase.AccountMonthMovement, 0, len(rows))
	for _, row := range rows {
		out = append(out, usecase.AccountMonthMovement{
			AccountID: uuidToString(row.AccountID),
			Month:     dateToTime(row.Month),
			Delta:     domain.Money{Amount: row.DeltaMinor, Currency: row.Currency},
		})
	}
	return out, nil
}
```

- [ ] **Step 6: Satisfy the port in the in-memory double**

In `api/internal/usecase/testdouble_test.go`, add a field to `fakeAccountRepo` and the method. Find the struct (search for `type fakeAccountRepo struct`) and extend it:

```go
type fakeAccountRepo struct {
	accounts    map[string]domain.Account
	memberships map[string]string
	movements   []usecase.AccountMonthMovement
	nextID      int
}
```

Then, after `List`:

```go
// addMovement is how a trend test says "this account moved by this much in
// this month" without going near SQL.
func (r *fakeAccountRepo) addMovement(m usecase.AccountMonthMovement) {
	r.movements = append(r.movements, m)
}

// MonthlyMovements honours `since` and nothing else. It deliberately does not
// filter by household or by opening date: every AccountService test runs one
// household, and re-implementing the real query's filters here would be test
// code asserting itself. Those filters have their own Postgres test.
func (r *fakeAccountRepo) MonthlyMovements(_ context.Context, _ string, since time.Time) ([]usecase.AccountMonthMovement, error) {
	var out []usecase.AccountMonthMovement
	for _, m := range r.movements {
		if m.Month.Before(since) {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}
```

- [ ] **Step 7: Run the tests to verify they pass**

```bash
cd api && go test ./internal/adapter/postgres/ -run MonthlyMovements -count=1 -v
cd api && go build ./... && go vet ./...
```

Expected: both new tests PASS, build clean.

- [ ] **Step 8: Mutation-check the filter**

Change `t.occurred_on >= a.opening_balance_as_of` to `>` in **both** halves of the new query, run `make sqlc`, and re-run the test.

Expected: `TestMonthlyMovementsSplitsTheBalanceExpressionByMonth` FAILS on the June figure. Revert both, re-run `make sqlc`, confirm green again.

- [ ] **Step 9: Commit**

```bash
git add api/internal/adapter/postgres/queries/account.sql api/internal/adapter/postgres/sqlcgen/ \
        api/internal/adapter/postgres/account_repo.go api/internal/adapter/postgres/account_repo_test.go \
        api/internal/usecase/ports.go api/internal/usecase/testdouble_test.go
git commit -m "feat(finances): read each account's monthly movement

The twelve-month net worth trend walks backwards from the live balance,
so it needs that balance's own expression split by month -- same filter,
same two sides, same received-amount preference on the incoming one.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `Summary` takes `today`, and looks a rate up once

**Files:**
- Modify: `api/internal/usecase/networth.go`
- Modify: `api/internal/adapter/http/account_handlers.go:119`
- Test: `api/internal/usecase/networth_test.go` (update the ten existing call sites)

**Interfaces:**
- Consumes: nothing from Task 1 yet.
- Produces:
  - `(*AccountService).Summary(ctx context.Context, householdID string, views []AccountView, today time.Time) (NetWorthSummary, error)`
  - `converter{fx FXRateProvider; primary string; rates map[string]Rate}` with `convert(ctx, domain.Money) (domain.Money, error)`, unexported, in `networth.go`.

This task is pure groundwork and changes no behaviour. It is separate because the signature change touches every existing test, and mixing that with new logic makes both unreviewable.

- [ ] **Step 1: Write the failing test**

Add to `api/internal/usecase/networth_test.go`:

```go
// TestSummaryLooksUpEachRateOnce is the guard on the trend's central promise,
// written before the trend exists. The newest bar must equal the headline
// figure, and it can only do that if both are converted with the same rate --
// so Summary must consult the provider once per currency, not once per
// account and again per month. fx.StaticProvider returns one number forever,
// so nothing else in the suite would ever notice the difference.
func TestSummaryLooksUpEachRateOnce(t *testing.T) {
	counter := &countingRates{}
	svc := newAccountServiceWithFX(t, counter)

	// Distinct ids: `account()` derives one from the currency and the type, so
	// three IDR cash accounts would otherwise share it -- harmless to Summary,
	// but the trend keys its movements by account id and a later test adding
	// one here would silently give all three the same history.
	views := []usecase.AccountView{
		account(t, domain.AccountCash, 1_000_000_000, "IDR"),
		account(t, domain.AccountCash, 2_000_000_000, "IDR"),
		account(t, domain.AccountCash, 3_000_000_000, "IDR"),
	}
	for i := range views {
		views[i].Account.ID = fmt.Sprintf("idr-%d", i)
	}

	if _, err := svc.Summary(context.Background(), "h-1", views, fixedNow); err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if counter.calls != 1 {
		t.Errorf("provider called %d times for three accounts in one currency, want 1", counter.calls)
	}
}
```

Two helpers this needs — put both in `networth_test.go`:

```go
// countingRates is staticTestRates that remembers how often it was asked.
type countingRates struct{ calls int }

func (c *countingRates) Rate(ctx context.Context, from, to string) (usecase.Rate, error) {
	c.calls++
	return staticTestRates{}.Rate(ctx, from, to)
}

// newAccountServiceWithFX is newAccountService with the FX double swapped,
// the same shape bill_test.go's newBillServiceWithFX already uses.
func newAccountServiceWithFX(t *testing.T, fx usecase.FXRateProvider) *usecase.AccountService {
	t.Helper()
	households := newHouseholdDouble()
	households.put(domain.Household{
		ID: "h-1", Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", ShowSecondaryCurrency: true, SecondaryCurrency: "IDR", FXRateMode: "auto",
	})
	return usecase.NewAccountService(usecase.AccountDeps{
		Accounts:   newFakeAccountRepo(),
		Households: households,
		FX:         fx,
		Clock:      &fixedClock{now: fixedNow},
	})
}
```

`fmt` joins that file's imports.

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd api && go test ./internal/usecase/ -run TestSummaryLooksUpEachRateOnce -count=1
```

Expected: FAIL to compile — `too many arguments in call to svc.Summary`.

- [ ] **Step 3: Change the signature and add the converter**

In `api/internal/usecase/networth.go`, replace the `convert` method at the bottom of the file with:

```go
// converter turns balances into one primary currency, looking each rate up at
// most once per request.
//
// One lookup, reused, is not an optimisation. Summary's headline and the
// trend's newest bar must apply the SAME rate to the same account, or the
// chart's last bar disagrees with the figure printed directly above it.
// fx.StaticProvider returns one number forever, so two independent lookups
// agree today by coincidence; a live provider could return two different rates
// inside one request, and no test against the static provider would ever see
// it.
type converter struct {
	fx      FXRateProvider
	primary string
	rates   map[string]Rate
}

// convert turns one balance into the household's primary currency. A
// same-currency balance short-circuits without consulting the provider at all
// -- that is the overwhelmingly common case, it is exact, and it means a
// single-currency household never depends on a rate table it does not need.
func (c *converter) convert(ctx context.Context, m domain.Money) (domain.Money, error) {
	if m.Currency == c.primary {
		return m, nil
	}
	rate, ok := c.rates[m.Currency]
	if !ok {
		var err error
		rate, err = c.fx.Rate(ctx, m.Currency, c.primary)
		if err != nil {
			return domain.Money{}, err
		}
		c.rates[m.Currency] = rate
	}
	amount, err := rate.Apply(m.Amount)
	if err != nil {
		return domain.Money{}, err
	}
	return domain.Money{Amount: amount, Currency: c.primary}, nil
}
```

Then change `Summary`'s signature and its one conversion call. Add to the doc comment above `Summary`:

```go
// today drives the twelve-month window and is taken as a parameter, never read
// from a clock in here, so every figure is deterministic in tests and the wall
// clock is read exactly once, at the HTTP layer. RetroService.List is the same
// shape for the same reason.
func (s *AccountService) Summary(ctx context.Context, householdID string, views []AccountView, today time.Time) (NetWorthSummary, error) {
```

Add `"time"` to the imports. Immediately after `primary := household.PrimaryCurrency`:

```go
	conv := &converter{fx: s.d.FX, primary: primary, rates: map[string]Rate{}}
```

And change the conversion inside the loop:

```go
		inPrimary, err := conv.convert(ctx, view.Balance)
```

- [ ] **Step 4: Update the call sites**

`api/internal/adapter/http/account_handlers.go:119`:

```go
		summary, err := deps.Accounts.Summary(r.Context(), scope.HouseholdID, views, deps.Clock.Now())
```

And every `svc.Summary(context.Background(), "h-1", ...)` in `networth_test.go` gains `, fixedNow` as a fourth argument:

```bash
cd api && grep -n "svc.Summary(" internal/usecase/networth_test.go
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd api && go test ./internal/usecase/ ./internal/adapter/http/ -count=1
```

Expected: PASS, including the new `TestSummaryLooksUpEachRateOnce`.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/networth.go api/internal/usecase/networth_test.go \
        api/internal/adapter/http/account_handlers.go
git commit -m "refactor(finances): Summary takes today, and looks each rate up once

Both are groundwork for the trend. The rate cache is the load-bearing
half: the newest bar has to be converted with the same rate as the
headline it sits under, and two independent lookups only agree because
the static provider returns one number forever.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: The twelve-month series

**Files:**
- Create: `api/internal/usecase/networth_trend.go`
- Modify: `api/internal/usecase/networth.go` (collect counted accounts; call the builder)
- Test: `api/internal/usecase/networth_trend_test.go`

**Interfaces:**
- Consumes: `AccountRepository.MonthlyMovements` (Task 1); `converter` and the four-argument `Summary` (Task 2).
- Produces:
  - `usecase.TrendPoint{Month time.Time; NetWorth *domain.Money; Complete bool}`
  - `usecase.NetWorthTrend{Points []TrendPoint; ChangeBasisPoints *int64}`
  - `NetWorthSummary.Trend *NetWorthTrend`
  - unexported: `trendAccount`, `(*AccountService).trend`, `walkBack`, `subtractDelta`, `deltasByAccountMonth`.

`ChangeBasisPoints` is defined here but always nil until Task 4.

- [ ] **Step 1: Write the failing tests**

Create `api/internal/usecase/networth_trend_test.go`:

```go
package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// month is the first of a month in the window fixedNow (28 July 2026) opens:
// August 2025 through July 2026, which is the design's own axis.
func month(year int, m time.Month) time.Time {
	return time.Date(year, m, 1, 0, 0, 0, 0, time.UTC)
}

// openedOn and withBalance extend networth_test.go's `account` builder. An
// account with no opening date set reads as tracked forever, which is what
// every pre-existing test wants.
func openedOn(on time.Time) func(*usecase.AccountView) {
	return func(v *usecase.AccountView) { v.Account.OpeningBalanceAsOf = on }
}

func withBalance(minor int64) func(*usecase.AccountView) {
	return func(v *usecase.AccountView) { v.Balance.Amount = minor }
}

func movement(accountID string, on time.Time, minor int64, currency string) usecase.AccountMonthMovement {
	return usecase.AccountMonthMovement{
		AccountID: accountID,
		Month:     on,
		Delta:     domain.Money{Amount: minor, Currency: currency},
	}
}

// TestTheNewestBarIsTheHeadlineFigure is this feature's discriminating test.
// The chart is the third place net worth is computed, and the one place a
// disagreement is visible to the eye: the newest bar sits directly under the
// figure it must equal.
//
// The future-dated movement is the trap. AccountView.Balance has no upper
// bound on the transaction date, so a transaction dated next month is already
// inside it. Bucket that movement into its own month and it is never
// subtracted on the way back, so every older bar is wrong by 500 -- while the
// newest bar still matches and the numbers still look reasonable.
func TestTheNewestBarIsTheHeadlineFigure(t *testing.T) {
	svc, repo := newAccountService(t)
	sgd := account(t, domain.AccountCash, 824_055, "SGD", withBalance(830_055))
	idr := account(t, domain.AccountCash, 8_540_000_000, "IDR")
	repo.addMovement(movement(sgd.Account.ID, month(2026, time.July), 6_500, "SGD"))
	repo.addMovement(movement(sgd.Account.ID, month(2026, time.August), -500, "SGD")) // dated ahead

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{sgd, idr}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Trend == nil {
		t.Fatal("Trend is nil for a computable summary")
	}
	if len(got.Trend.Points) != 12 {
		t.Fatalf("points = %d, want 12", len(got.Trend.Points))
	}

	newest := got.Trend.Points[11]
	if newest.NetWorth == nil {
		t.Fatal("the newest point has no figure")
	}
	if newest.NetWorth.Amount != got.NetWorth.Amount {
		t.Errorf("newest bar = %d, headline = %d -- these must be the same number",
			newest.NetWorth.Amount, got.NetWorth.Amount)
	}
	if !newest.Month.Equal(month(2026, time.July)) {
		t.Errorf("newest month = %s, want 2026-07", newest.Month.Format("2006-01"))
	}
	if !got.Trend.Points[0].Month.Equal(month(2025, time.August)) {
		t.Errorf("oldest month = %s, want 2025-08", got.Trend.Points[0].Month.Format("2006-01"))
	}

	// July's own movement AND the future-dated one both leave on the first
	// step backwards: 830055 - 6500 - (-500) = 824055 for the SGD account,
	// plus the IDR account's unchanged 688155.
	previous := got.Trend.Points[10]
	if previous.NetWorth == nil || previous.NetWorth.Amount != 824_055+688_155 {
		t.Errorf("June = %v, want %d -- a transaction dated next month is inside "+
			"today's balance and must come off on the first step back",
			previous.NetWorth, 824_055+688_155)
	}
}

// TestAMonthBeforeAnAccountWasTrackedIsAGap is the "zero is a claim" rule on
// this chart. A bar of zero says the household had nothing; the truth is that
// we do not know.
func TestAMonthBeforeAnAccountWasTrackedIsAGap(t *testing.T) {
	svc, _ := newAccountService(t)
	view := account(t, domain.AccountCash, 500_000, "SGD", openedOn(month(2026, time.June)))

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{view}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	for i, point := range got.Trend.Points[:10] { // Aug 2025 .. May 2026
		if point.NetWorth != nil {
			t.Fatalf("point %d (%s) = %v, want nil -- no account was tracked yet",
				i, point.Month.Format("2006-01"), point.NetWorth)
		}
		if point.Complete {
			t.Errorf("point %d is marked complete, but it has no figure at all", i)
		}
	}
	if got.Trend.Points[10].NetWorth == nil || got.Trend.Points[11].NetWorth == nil {
		t.Error("June and July have a figure and must be drawn")
	}
}

// TestAMonthMissingOneAccountIsMarkedIncomplete is the middle state, and the
// reason the chart is drawable at all for a household that adds an account.
// The bar is real; it is missing an account the newest bar has, and the
// screen has to be able to say so rather than let the step up read as growth.
func TestAMonthMissingOneAccountIsMarkedIncomplete(t *testing.T) {
	svc, _ := newAccountService(t)
	old := account(t, domain.AccountCash, 100_000, "SGD", openedOn(month(2025, time.August)))
	old.Account.ID = "old"
	recent := account(t, domain.AccountSavings, 900_000, "SGD", openedOn(month(2026, time.July)))
	recent.Account.ID = "recent"

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{old, recent}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	june := got.Trend.Points[10]
	if june.NetWorth == nil || june.NetWorth.Amount != 100_000 {
		t.Errorf("June = %v, want 100000 -- the account that existed then, and only it", june.NetWorth)
	}
	if june.Complete {
		t.Error("June is marked complete, but the savings account was not tracked yet")
	}
	if !got.Trend.Points[11].Complete {
		t.Error("July is marked incomplete, but both accounts were tracked by then")
	}
}

// TestArchivedAndUncountedAccountsAreInNoBar: whatever is out of the headline
// is out of every bar. An account excluded from the total but drawn into the
// history would make the chart's last bar the only one that agrees with it.
func TestArchivedAndUncountedAccountsAreInNoBar(t *testing.T) {
	svc, _ := newAccountService(t)
	counted := account(t, domain.AccountCash, 100_000, "SGD")
	counted.Account.ID = "counted"
	skipped := account(t, domain.AccountCash, 900_000, "SGD", notCounted)
	skipped.Account.ID = "skipped"
	gone := account(t, domain.AccountCash, 700_000, "SGD", archived)
	gone.Account.ID = "gone"

	got, err := svc.Summary(context.Background(), "h-1",
		[]usecase.AccountView{counted, skipped, gone}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	for _, point := range got.Trend.Points {
		if point.NetWorth == nil || point.NetWorth.Amount != 100_000 {
			t.Fatalf("%s = %v, want 100000 on every bar", point.Month.Format("2006-01"), point.NetWorth)
		}
	}
}

// TestALiabilityPullsTheBarDown: the sign comes from the account type, never
// from a number someone typed, and it has to be applied per month as well as
// once at the headline.
func TestALiabilityPullsTheBarDown(t *testing.T) {
	svc, repo := newAccountService(t)
	cash := account(t, domain.AccountCash, 500_000, "SGD")
	cash.Account.ID = "cash"
	loan := account(t, domain.AccountLoan, 200_000, "SGD", withBalance(200_000))
	loan.Account.ID = "loan"
	// A delta is a movement of the account's own BALANCE, and a liability's
	// balance is the sum owed: +50000 in July means the household borrowed
	// 50000 more that month, so June's debt was smaller and June's net worth
	// was HIGHER. The direction is the whole test -- get the sign backwards
	// and SignedNetWorthAmount can be missing entirely without anyone noticing.
	repo.addMovement(movement("loan", month(2026, time.July), 50_000, "SGD"))

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{cash, loan}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Trend.Points[11].NetWorth.Amount != 300_000 {
		t.Errorf("July = %d, want 300000 (500000 - 200000)", got.Trend.Points[11].NetWorth.Amount)
	}
	if got.Trend.Points[10].NetWorth.Amount != 350_000 {
		t.Errorf("June = %d, want 350000 (500000 - 150000) -- the debt was 50000 smaller "+
			"before July's borrowing, so net worth was higher",
			got.Trend.Points[10].NetWorth.Amount)
	}
}

// TestNoCountedAccountsMeansNoTrend: a household whose only accounts are
// excluded has a computable, genuinely zero net worth and nothing to chart.
// Nil, rather than twelve nil-valued points, so the wire carries no trend at
// all and the screen has one absence to branch on rather than two.
func TestNoCountedAccountsMeansNoTrend(t *testing.T) {
	svc, _ := newAccountService(t)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Trend != nil {
		t.Errorf("Trend = %+v, want nil for a household with no counted accounts", got.Trend)
	}
}
```

Check `domain.AccountSavings` exists (`grep -n "Account[A-Z]" api/internal/domain/account.go`); substitute a real constant if it is named differently.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd api && go test ./internal/usecase/ -run "Trend|Bar|Gap|Incomplete" -count=1
```

Expected: FAIL to compile — `got.Trend undefined`.

- [ ] **Step 3: Write the trend builder**

Create `api/internal/usecase/networth_trend.go`:

```go
package usecase

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// trendMonths is the window the design draws: twelve bars, `Aug '25` to
// `Jul '26` on its own axis.
const trendMonths = 12

// TrendPoint is one bar of the twelve-month net worth chart.
//
// NetWorth is nil for a month no counted account had been tracked through
// yet. It is nil rather than zero for the reason NetWorthSummary.Computable
// exists: zero is a claim about the household's money, and the truth in that
// month is that we cannot know it.
//
// Complete is false when at least one counted account was still untracked in
// that month -- the bar is real, but it is missing an account the newest bar
// has, and the step up between them is coverage rather than growth. It is
// also false on a month with no figure at all, so a caller that reads
// Complete without checking NetWorth cannot mistake an empty month for a
// whole one.
type TrendPoint struct {
	Month    time.Time
	NetWorth *domain.Money
	Complete bool
}

// NetWorthTrend is the twelve-month series and the month-to-date change.
//
// ChangeBasisPoints is integer basis points -- 210 means 2.10% -- and is nil
// far more often than it is set. changeBasisPoints below has the four
// conditions and why each one exists.
type NetWorthTrend struct {
	Points            []TrendPoint
	ChangeBasisPoints *int64
}

// trendAccount is one counted account, carried out of Summary's own loop.
//
// inPrimary is the value that loop already added to the headline. Keeping it
// is the whole point: the newest bar reuses that number instead of converting
// the same balance a second time, so the bar and the figure above it cannot
// disagree even if the rate provider is asked twice and answers differently.
type trendAccount struct {
	account   domain.Account
	balance   domain.Money
	inPrimary domain.Money
}

// trend builds the twelve-month series for the accounts Summary counted.
//
// Every month is converted at TODAY's rate, not the rate that held in that
// month: there is no historical rate table, and fx.StaticProvider has one
// number in it. The chart therefore shows how the household's balances moved
// with the exchange rate held still -- the more useful of the two charts
// anyway, since an account whose balance never changed should not appear to
// rise and fall because a currency did (spec decision 2).
func (s *AccountService) trend(
	ctx context.Context,
	householdID string,
	conv *converter,
	counted []trendAccount,
	today time.Time,
	zero domain.Money,
) (*NetWorthTrend, error) {
	months := make([]time.Time, trendMonths)
	current := startOfMonth(today)
	for i := range months {
		months[i] = current.AddDate(0, -(trendMonths - 1 - i), 0)
	}

	movements, err := s.d.Accounts.MonthlyMovements(ctx, householdID, months[0])
	if err != nil {
		return nil, err
	}
	deltas, err := deltasByAccountMonth(movements, current, counted)
	if err != nil {
		return nil, err
	}

	// running/known/missing are accumulated across accounts and folded into
	// points at the end, because one account can only ever contribute to a
	// month, never decide it: "complete" is a fact about all of them.
	running := make([]domain.Money, trendMonths)
	known := make([]bool, trendMonths)
	missing := make([]bool, trendMonths)
	for i := range running {
		running[i] = zero
	}

	for _, a := range counted {
		native, err := walkBack(a.balance.Amount, deltas[a.account.ID], months)
		if err != nil {
			return nil, err
		}
		trackedFrom := startOfMonth(a.account.OpeningBalanceAsOf)

		for i, m := range months {
			if trackedFrom.After(m) {
				missing[i] = true
				continue
			}

			inPrimary := a.inPrimary
			if i != trendMonths-1 {
				inPrimary, err = conv.convert(ctx, domain.Money{
					Amount:   native[i],
					Currency: a.balance.Currency,
				})
				if err != nil {
					return nil, err
				}
			}

			signed, err := a.account.Type.SignedNetWorthAmount(inPrimary)
			if err != nil {
				return nil, err
			}
			running[i], err = running[i].Add(signed)
			if err != nil {
				return nil, err
			}
			known[i] = true
		}
	}

	points := make([]TrendPoint, trendMonths)
	for i := range points {
		points[i] = TrendPoint{Month: months[i], Complete: known[i] && !missing[i]}
		if known[i] {
			total := running[i]
			points[i].NetWorth = &total
		}
	}

	return &NetWorthTrend{Points: points}, nil
}

// deltasByAccountMonth indexes the repository's rows by account and month.
//
// A month later than the current one is counted as the current one. That is
// not a rounding convenience: AccountView.Balance has no upper bound on the
// transaction date, so a transaction dated next month is already inside the
// balance the walk anchors on. Left in its own bucket it would never be
// subtracted, and every bar older than today would be wrong by its amount
// while the newest bar still matched the headline.
func deltasByAccountMonth(
	movements []AccountMonthMovement,
	current time.Time,
	counted []trendAccount,
) (map[string]map[int]domain.Money, error) {
	currencies := make(map[string]string, len(counted))
	for _, a := range counted {
		currencies[a.account.ID] = a.balance.Currency
	}

	out := map[string]map[int]domain.Money{}
	for _, m := range movements {
		want, ok := currencies[m.AccountID]
		if !ok {
			// Archived, excluded by choice, or not in the views this summary
			// describes. Whatever is out of the headline is out of the chart.
			continue
		}
		// Fail closed. A delta in another currency cannot be subtracted from
		// this account's balance, and adding it anyway would corrupt every
		// older bar with a figure that still looks like money.
		if m.Delta.Currency != want {
			return nil, fmt.Errorf("%w: movement for account %s is %s, the account is %s",
				domain.ErrCurrencyMismatch, m.AccountID, m.Delta.Currency, want)
		}

		month := startOfMonth(m.Month)
		if month.After(current) {
			month = current
		}
		byMonth, ok := out[m.AccountID]
		if !ok {
			byMonth = map[int]domain.Money{}
			out[m.AccountID] = byMonth
		}
		key := monthKey(month)
		if existing, ok := byMonth[key]; ok {
			summed, err := existing.Add(m.Delta)
			if err != nil {
				return nil, err
			}
			byMonth[key] = summed
			continue
		}
		byMonth[key] = m.Delta
	}
	return out, nil
}

// walkBack turns one account's current balance into its balance at the end of
// every earlier month in the window: each step removes the month it is
// leaving. The newest slot is the live balance itself, untouched.
func walkBack(current int64, byMonth map[int]domain.Money, months []time.Time) ([]int64, error) {
	native := make([]int64, len(months))
	native[len(months)-1] = current
	for i := len(months) - 2; i >= 0; i-- {
		back, err := subtractDelta(native[i+1], byMonth[monthKey(months[i+1])].Amount)
		if err != nil {
			return nil, err
		}
		native[i] = back
	}
	return native, nil
}

// subtractDelta is balance - delta with the overflow refused rather than
// wrapped. math.MinInt64 is checked on its own because it has no positive
// counterpart, so negating it returns itself -- the same edge
// AccountType.SignedNetWorthAmount and Money.String already guard.
func subtractDelta(balance, delta int64) (int64, error) {
	if delta == math.MinInt64 {
		return 0, domain.ErrAmountOverflow
	}
	negated := -delta
	if (negated > 0 && balance > math.MaxInt64-negated) ||
		(negated < 0 && balance < math.MinInt64-negated) {
		return 0, domain.ErrAmountOverflow
	}
	return balance + negated, nil
}
```

- [ ] **Step 4: Collect the counted accounts in `Summary`**

In `api/internal/usecase/networth.go`, add the field to `NetWorthSummary`:

```go
	// Trend is the twelve-month series, nil when there is nothing to chart --
	// an incomputable summary, or a household with no counted accounts.
	Trend *NetWorthTrend
```

Declare the slice beside `considered`/`converted`:

```go
	counted := make([]trendAccount, 0, len(views))
```

Record each account that survives every filter — insert immediately after the `if !view.Account.CountTowardNetWorth { ... continue }` block, before the asset/liability split:

```go
		// Everything past this point is in the headline, so it is in the
		// chart: the two must describe the same set of accounts or only the
		// newest bar agrees with the figure above it.
		counted = append(counted, trendAccount{
			account:   view.Account,
			balance:   view.Balance,
			inPrimary: inPrimary,
		})
```

And build the trend after the `Computable` decision, before the breakdown loop:

```go
	if summary.Computable && len(counted) > 0 {
		trend, err := s.trend(ctx, householdID, conv, counted, today, zero)
		if err != nil {
			return NetWorthSummary{}, err
		}
		summary.Trend = trend
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd api && go test ./internal/usecase/ -count=1
```

Expected: PASS, all six new tests plus the pre-existing ten.

- [ ] **Step 6: Mutation-check the discriminating test**

Two mutations, both must go red in `TestTheNewestBarIsTheHeadlineFigure`:

1. In `deltasByAccountMonth`, delete the clamp (`if month.After(current) { month = current }`). Run — expect the June assertion to fail.
2. In `trend`, drop the `if i != trendMonths-1` guard so the newest month re-converts instead of reusing `a.inPrimary`, and temporarily make the FX double return a different rate on its second call. Run — expect the newest-bar assertion to fail.

Revert both. Re-run and confirm green.

- [ ] **Step 7: Commit**

```bash
git add api/internal/usecase/networth_trend.go api/internal/usecase/networth_trend_test.go \
        api/internal/usecase/networth.go
git commit -m "feat(finances): derive the twelve-month net worth series

Walks backwards from the live balance rather than recomputing each
month-end, so the newest bar is the headline figure by construction. A
month before an account was tracked is a gap, never a zero, and a month
missing an account the newest bar has is marked rather than drawn as
though it were whole.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: The month-to-date percentage

**Files:**
- Modify: `api/internal/usecase/networth_trend.go`
- Test: `api/internal/usecase/networth_trend_test.go` (append)

**Interfaces:**
- Consumes: `TrendPoint`, `NetWorthTrend` (Task 3).
- Produces: `changeBasisPoints(current, previous TrendPoint) *int64`, unexported; `NetWorthTrend.ChangeBasisPoints` now populated.

- [ ] **Step 1: Write the failing tests**

Append to `api/internal/usecase/networth_trend_test.go`:

```go
// TestTheChangeIsBasisPointsOfLastMonth is the design's own "▲ 2.1%", as an
// integer. A percentage is not money, but there is no reason to put a float
// on this wire when 210 says 2.10% exactly.
func TestTheChangeIsBasisPointsOfLastMonth(t *testing.T) {
	svc, repo := newAccountService(t)
	view := account(t, domain.AccountCash, 1_000_000, "SGD",
		openedOn(month(2025, time.August)), withBalance(1_021_000))
	repo.addMovement(movement(view.Account.ID, month(2026, time.July), 21_000, "SGD"))

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{view}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Trend.ChangeBasisPoints == nil {
		t.Fatal("ChangeBasisPoints is nil, want 210")
	}
	if *got.Trend.ChangeBasisPoints != 210 {
		t.Errorf("ChangeBasisPoints = %d, want 210 (21000 on 1000000)", *got.Trend.ChangeBasisPoints)
	}
}

// TestTheChangeIsSuppressedWhenLastMonthWasIncomplete is the guard that stops
// the product calling coverage growth. A household that starts tracking a
// second account this month did not get 400% richer.
func TestTheChangeIsSuppressedWhenLastMonthWasIncomplete(t *testing.T) {
	svc, _ := newAccountService(t)
	old := account(t, domain.AccountCash, 100_000, "SGD", openedOn(month(2025, time.August)))
	old.Account.ID = "old"
	fresh := account(t, domain.AccountCash, 400_000, "SGD", openedOn(month(2026, time.July)))
	fresh.Account.ID = "fresh"

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{old, fresh}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Trend.ChangeBasisPoints != nil {
		t.Errorf("ChangeBasisPoints = %d, want nil -- June was missing an account July has",
			*got.Trend.ChangeBasisPoints)
	}
}

// TestTheChangeIsSuppressedOnANonPositiveBase: a percentage of zero is
// undefined, and off a negative base it inverts its own sign -- a household
// climbing from -10000 to -5000 would be shown as having fallen 50%.
func TestTheChangeIsSuppressedOnANonPositiveBase(t *testing.T) {
	svc, repo := newAccountService(t)
	// -5000 in July: the sum owed fell from 10000 to 5000 that month. Both
	// months are therefore negative net worth, which is the state this rule
	// exists for.
	loan := account(t, domain.AccountLoan, 10_000, "SGD",
		openedOn(month(2025, time.August)), withBalance(5_000))
	repo.addMovement(movement(loan.Account.ID, month(2026, time.July), -5_000, "SGD"))

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{loan}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Trend.Points[10].NetWorth.Amount != -10_000 {
		t.Fatalf("June = %d, want -10000 -- the fixture is wrong, not the rule",
			got.Trend.Points[10].NetWorth.Amount)
	}
	if got.Trend.ChangeBasisPoints != nil {
		t.Errorf("ChangeBasisPoints = %d, want nil on a negative base",
			*got.Trend.ChangeBasisPoints)
	}
}

// TestTheChangeRoundsHalfAwayFromZero matches Rate.Apply's rule, so every
// rounding decision in a monetary path in this codebase is the same one.
func TestTheChangeRoundsHalfAwayFromZero(t *testing.T) {
	svc, repo := newAccountService(t)
	// 15 on 20000 is 0.075% -- 7.5 basis points, which must round to 8.
	view := account(t, domain.AccountCash, 20_000, "SGD",
		openedOn(month(2025, time.August)), withBalance(20_015))
	repo.addMovement(movement(view.Account.ID, month(2026, time.July), 15, "SGD"))

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{view}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Trend.ChangeBasisPoints == nil || *got.Trend.ChangeBasisPoints != 8 {
		t.Errorf("ChangeBasisPoints = %v, want 8 (7.5 rounds away from zero)", got.Trend.ChangeBasisPoints)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd api && go test ./internal/usecase/ -run TestTheChange -count=1
```

Expected: FAIL — `ChangeBasisPoints is nil, want 210`.

- [ ] **Step 3: Implement it**

Append to `api/internal/usecase/networth_trend.go`:

```go
// changeBasisPoints is the "▲ 2.1%" beside the headline figure, in integer
// basis points: 210 means 2.10%.
//
// It returns nil far more often than it returns a number, and each condition
// is a claim the product must not make:
//
//   - either month unknown: there is no comparison to draw.
//   - either month incomplete: the step between them is partly coverage, not
//     growth. A household that started tracking a second account this month
//     did not get richer by its balance.
//   - a base of zero or less: a percentage of zero is undefined, and off a
//     negative base it inverts its own sign -- a household climbing from
//     -10,000 to -5,000 would be shown as -50%.
//   - arithmetic that would overflow: the same fail-closed rule as everywhere,
//     rather than a wrapped number that still renders.
//
// The rounding is half away from zero, matching Rate.Apply, so every rounding
// decision on this screen is the same decision.
func changeBasisPoints(current, previous TrendPoint) *int64 {
	if current.NetWorth == nil || previous.NetWorth == nil {
		return nil
	}
	if !current.Complete || !previous.Complete {
		return nil
	}

	base := previous.NetWorth.Amount
	if base <= 0 {
		return nil
	}
	now := current.NetWorth.Amount
	if now < math.MinInt64+base {
		return nil
	}
	delta := now - base

	if delta > math.MaxInt64/10_000 || delta < math.MinInt64/10_000 {
		return nil
	}
	scaled := delta * 10_000
	half := base / 2
	if scaled > math.MaxInt64-half || scaled < math.MinInt64+half {
		return nil
	}
	if scaled < 0 {
		scaled -= half
	} else {
		scaled += half
	}
	points := scaled / base
	return &points
}
```

And in `trend`, replace the return:

```go
	return &NetWorthTrend{
		Points:            points,
		ChangeBasisPoints: changeBasisPoints(points[trendMonths-1], points[trendMonths-2]),
	}, nil
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd api && go test ./internal/usecase/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Mutation-check the suppression rule**

Delete the `if !current.Complete || !previous.Complete { return nil }` branch and run:

```bash
cd api && go test ./internal/usecase/ -run TestTheChangeIsSuppressedWhenLastMonthWasIncomplete -count=1
```

Expected: FAIL. Restore it and confirm green.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/networth_trend.go api/internal/usecase/networth_trend_test.go
git commit -m "feat(finances): month-to-date change, in basis points

Suppressed unless both months are known, both are complete and the base
is positive. The completeness rule is the one that matters: starting to
track an account is not growth, and the product must not say it is.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Put the trend on the wire

**Files:**
- Modify: `api/internal/adapter/http/account_handlers.go`
- Test: `api/internal/adapter/http/accounts_api_test.go` (append)

**Interfaces:**
- Consumes: `usecase.NetWorthTrend` (Tasks 3–4).
- Produces: JSON `summary.trend = { points: [{month, netWorthMinor, complete}], changeBasisPoints? }`.

- [ ] **Step 1: Write the failing tests**

Append to `api/internal/adapter/http/accounts_api_test.go`:

```go
// TestOwnerSeesTheTwelveMonthTrend pins the wire shape the Finances chart
// reads. The window is fixed by the clock, so the months are assertable.
func TestOwnerSeesTheTwelveMonthTrend(t *testing.T) {
	env := newTestEnvWithClock(t, &movableClock{now: time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)})
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	env.mustCreateAccount(t, session, csrf, map[string]any{
		"nickname": "DBS Everyday", "type": "cash",
		"openingBalanceMinor": 824_055, "openingBalanceCurrency": "SGD",
		"openingBalanceAsOf": "2026-07-01",
	})

	rec := env.authedGet(t, "/api/v1/accounts", session)
	var got struct {
		Summary *struct {
			Trend *struct {
				Points []struct {
					Month         string `json:"month"`
					NetWorthMinor *int64 `json:"netWorthMinor"`
					Complete      bool   `json:"complete"`
				} `json:"points"`
				ChangeBasisPoints *int64 `json:"changeBasisPoints"`
			} `json:"trend"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Summary == nil || got.Summary.Trend == nil {
		t.Fatal("no trend for an owner with an account")
	}
	points := got.Summary.Trend.Points
	if len(points) != 12 {
		t.Fatalf("points = %d, want 12", len(points))
	}
	if points[0].Month != "2025-08" || points[11].Month != "2026-07" {
		t.Errorf("window = %s..%s, want 2025-08..2026-07", points[0].Month, points[11].Month)
	}
	// An account opened this month: every earlier month is a gap, and a gap
	// is a null, never a zero.
	if points[0].NetWorthMinor != nil {
		t.Errorf("2025-08 = %d, want null -- nothing was tracked then", *points[0].NetWorthMinor)
	}
	if points[11].NetWorthMinor == nil || *points[11].NetWorthMinor != 824_055 {
		t.Errorf("2026-07 = %v, want 824055", points[11].NetWorthMinor)
	}
	if got.Summary.Trend.ChangeBasisPoints != nil {
		t.Errorf("changeBasisPoints = %d, want absent -- June is unknown",
			*got.Summary.Trend.ChangeBasisPoints)
	}

	// netWorthMinor must be PRESENT and null on a gap month, not omitted:
	// the frontend needs the slot to keep the axis aligned.
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"netWorthMinor":null`)) {
		t.Error("a gap month omits netWorthMinor entirely; it must be sent as null")
	}
}

// TestALimitedMemberGetsNoTrend needs no new guard to pass, and that is the
// point: the trend rides inside the summary, which is already withheld whole.
// The test exists so that a later refactor moving the trend to its own field
// or its own route cannot leak amounts without going red.
func TestALimitedMemberGetsNoTrend(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.moneyLimitedEmail, env.moneyLimitedPassword)

	rec := env.authedGet(t, "/api/v1/accounts", session)
	if bytes.Contains(rec.Body.Bytes(), []byte(`"trend"`)) {
		t.Errorf("a limited member's response carries a trend: %s", rec.Body.String())
	}
}
```

Add `"bytes"` and `"time"` to that file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd api && go test ./internal/adapter/http/ -run "Trend" -count=1
```

Expected: FAIL — "no trend for an owner with an account".

- [ ] **Step 3: Add the DTOs and map them**

In `api/internal/adapter/http/account_handlers.go`, beside the other DTOs:

```go
// trendPointDTO is one bar. NetWorthMinor is a pointer WITHOUT omitempty, so
// an unknown month arrives as an explicit null rather than a missing key: the
// chart needs the slot to keep its axis aligned, and a zero would be a claim
// about the household's money that nobody can make.
type trendPointDTO struct {
	Month         string `json:"month"`
	NetWorthMinor *int64 `json:"netWorthMinor"`
	Complete      bool   `json:"complete"`
}

// trendDTO carries the change as integer basis points -- 210 is 2.10%. A
// percentage is not money, so the int64-minor-units rule does not literally
// apply, but there is no reason to put a float on this wire either, and
// omitempty is wrong for it too: the field is absent when suppressed, and 0
// is a real reading meaning "unchanged".
type trendDTO struct {
	Points            []trendPointDTO `json:"points"`
	ChangeBasisPoints *int64          `json:"changeBasisPoints,omitempty"`
}
```

Add the field to `summaryDTO`, after `Computable`:

```go
	Trend            *trendDTO      `json:"trend,omitempty"`
```

And in `toSummaryDTO`, inside the existing `if s.Computable` block:

```go
	if s.Computable && s.Trend != nil {
		points := make([]trendPointDTO, 0, len(s.Trend.Points))
		for _, p := range s.Trend.Points {
			point := trendPointDTO{Month: p.Month.Format(monthLayout), Complete: p.Complete}
			if p.NetWorth != nil {
				amount := p.NetWorth.Amount
				point.NetWorthMinor = &amount
			}
			points = append(points, point)
		}
		dto.Trend = &trendDTO{Points: points, ChangeBasisPoints: s.Trend.ChangeBasisPoints}
	}
```

`monthLayout` ("2006-01") already exists in this package — see `budget_handlers.go`.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd api && go test ./internal/adapter/http/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Run the whole Go suite and the arch lint**

```bash
cd api && go test ./... -count=1 -timeout=5m
cd .. && make lint-arch && cd api && go vet ./...
```

Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/http/account_handlers.go api/internal/adapter/http/accounts_api_test.go
git commit -m "feat(api): carry the net worth trend on the accounts summary

One endpoint for one screen, so the limited-member redaction stays
written once -- a separate route would mean a second copy of the role
check guarding the most revealing numbers on the wire.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: The chart component

**Files:**
- Modify: `web/src/features/money/schemas.ts`
- Modify: `web/src/features/money/copy.ts`
- Create: `web/src/features/money/NetWorthChart.tsx`
- Test: `web/src/features/money/NetWorthChart.test.tsx`

**Interfaces:**
- Consumes: the wire shape from Task 5.
- Produces:
  - `TrendPoint` and `Trend` types exported from `schemas.ts`; `summary.trend` optional on the computable branch.
  - `<NetWorthChart points={points} />`
  - `FINANCES_COPY.trendWindow / trendEmpty / trendIncomplete / trendIncompleteUnknownStart / trendChange / trendChartLabel`, and `monthTickLabel`.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/money/NetWorthChart.test.tsx`:

```tsx
// No fetch stub: NetWorthChart takes its points as a prop, the same data
// useAccounts() already fetched. MoodChart.test.tsx is the pattern.
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { NetWorthChart } from "./NetWorthChart";
import type { TrendPoint } from "./schemas";

const MONTHS = [
  "2025-08", "2025-09", "2025-10", "2025-11",
  "2025-12", "2026-01", "2026-02", "2026-03",
  "2026-04", "2026-05", "2026-06", "2026-07",
];

function twelvePoints(overrides: Record<string, Partial<TrendPoint>> = {}): TrendPoint[] {
  return MONTHS.map((month, index) => ({
    month,
    netWorthMinor: 1_000_000 + index * 10_000,
    complete: true,
    ...(overrides[month] ?? {}),
  }));
}

describe("NetWorthChart", () => {
  it("draws one bar per month that has a figure", () => {
    const { container } = render(<NetWorthChart points={twelvePoints()} />);
    expect(container.querySelectorAll("[data-testid='net-worth-bar']").length).toBe(12);
  });

  // A gap is a gap. Zero is a claim about the household's money, and on a bar
  // chart a zero-height bar reads as "they had nothing".
  it("draws no bar at all for a month with no figure", () => {
    const points = twelvePoints({
      "2025-08": { netWorthMinor: null, complete: false },
      "2025-09": { netWorthMinor: null, complete: false },
    });
    const { container } = render(<NetWorthChart points={points} />);

    const bars = container.querySelectorAll("[data-testid='net-worth-bar']");
    expect(bars.length).toBe(10);
    expect([...bars].map((b) => b.getAttribute("data-month"))).not.toContain("2025-08");
  });

  // Fewer than two known months is not a trend. This is the state every new
  // household is in on their first day -- every account opened today -- and a
  // single bar pinned to the right-hand edge is worse than saying so.
  it("says there is not enough history when only one month is known", () => {
    const points = twelvePoints(
      Object.fromEntries(
        MONTHS.slice(0, 11).map((month) => [month, { netWorthMinor: null, complete: false }]),
      ),
    );
    const { container } = render(<NetWorthChart points={points} />);

    expect(container.querySelector("svg")).toBeNull();
    expect(screen.getByTestId("net-worth-chart-empty")).toBeInTheDocument();
  });

  it("marks the months that are missing an account added later", () => {
    const points = twelvePoints({
      "2025-08": { complete: false },
      "2025-09": { complete: false },
    });
    const { container } = render(<NetWorthChart points={points} />);

    const incomplete = container.querySelectorAll("[data-complete='false']");
    expect(incomplete.length).toBe(2);
    expect(screen.getByTestId("net-worth-chart-note")).toHaveTextContent("2025");
  });

  it("draws negative net worth below the baseline, not off the chart", () => {
    const points = twelvePoints({ "2025-08": { netWorthMinor: -500_000 } });
    const { container } = render(<NetWorthChart points={points} />);

    const bars = [...container.querySelectorAll("[data-testid='net-worth-bar']")];
    const negative = bars.find((b) => b.getAttribute("data-month") === "2025-08");
    const positive = bars.find((b) => b.getAttribute("data-month") === "2026-07");
    // The negative bar starts at the baseline and runs down, so its top edge
    // is BELOW the positive bar's top edge (y grows downward in SVG).
    expect(Number(negative?.getAttribute("y"))).toBeGreaterThan(
      Number(positive?.getAttribute("y")),
    );
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd web && npx vitest run src/features/money/NetWorthChart.test.tsx
```

Expected: FAIL — cannot resolve `./NetWorthChart`.

- [ ] **Step 3: Extend the schema**

In `web/src/features/money/schemas.ts`, above `computableSummarySchema`:

```ts
// netWorthMinor is nullable and REQUIRED: the server sends an explicit null
// for a month nothing was tracked through yet, so the chart keeps the slot
// and the axis stays aligned. A zero would be a claim about the household's
// money; a missing key would silently shorten the series.
const trendPointSchema = z.object({
  month: z.string(),
  netWorthMinor: z.number().nullable(),
  complete: z.boolean(),
});

// changeBasisPoints is integer basis points -- 210 is 2.10%. It is optional
// because the server omits it whenever the comparison would be dishonest
// (either month unknown or incomplete, or a base of zero or less), and
// `optional` rather than `nullable` because absence is exactly what that
// means: there is no percentage, not a percentage of nothing.
const trendSchema = z.object({
  points: z.array(trendPointSchema),
  changeBasisPoints: z.number().optional(),
});

export type TrendPoint = z.infer<typeof trendPointSchema>;
export type Trend = z.infer<typeof trendSchema>;
```

And add to `computableSummarySchema` only — an incomputable summary carries no figures, so it carries no trend:

```ts
  trend: trendSchema.optional(),
```

- [ ] **Step 4: Add the copy**

In `web/src/features/money/copy.ts`, inside `FINANCES_COPY`:

```ts
  // The twelve-month trend (design line 354).
  trendWindow: "Last 12 months",
  // Fewer than two known months. Every household is in this state on their
  // first day, so it is a real screen and not an edge case.
  trendEmpty: "Not enough history yet — the chart starts once there are two months to compare.",
  trendIncomplete: (from: string) =>
    `Lighter bars, before ${from}, are missing accounts that were added later.`,
  // The same note when no month in the window is complete, so there is no
  // "before" to name.
  trendIncompleteUnknownStart: "Lighter bars are missing accounts that were added later.",
  // 210 -> "▲ 2.1%". The arrow carries the direction as well as the sign, so
  // the figure reads at a glance and not only to someone checking for a minus.
  trendChange: (basisPoints: number) =>
    `${basisPoints < 0 ? "▼" : "▲"} ${(Math.abs(basisPoints) / 100).toFixed(1)}%`,
  trendChartLabel: (from: string, to: string, known: number) =>
    `Net worth from ${from} to ${to}. ${known} of 12 months have a figure.`,
```

And, at the bottom of the file beside the other exports:

```ts
// "2025-08" -> "Aug '25", the design's own axis format. Deliberately not
// shared with retroCopy's monthShortLabel, which is a different format (no
// year) for a different chart -- one helper reused two ways would have to
// grow a mode parameter to serve both.
export function monthTickLabel(month: string): string {
  const [year, monthNumber] = month.split("-").map(Number);
  const short = new Date(year, monthNumber - 1, 2).toLocaleDateString("en-US", { month: "short" });
  return `${short} '${String(year).slice(-2)}`;
}
```

- [ ] **Step 5: Write the chart**

Create `web/src/features/money/NetWorthChart.tsx`:

```tsx
// The twelve-month net worth chart, as inline SVG. Twelve bars is less code
// than a charting dependency, and this project's own floating-dependency
// history is why no new package arrives for it -- MoodChart.tsx made the same
// call for the same reason.
//
// `points` is whatever useAccounts() already fetched: the chart wires against
// data the page holds, never a second request.
import { FINANCES_COPY, monthTickLabel } from "./copy";
import type { TrendPoint } from "./schemas";

const WIDTH = 320;
const HEIGHT = 150;
const PAD_X = 6;
const PAD_TOP = 10;
const PAD_BOTTOM = 26; // room for the month-label row under the plot
const PLOT_WIDTH = WIDTH - PAD_X * 2;
const PLOT_HEIGHT = HEIGHT - PAD_TOP - PAD_BOTTOM;
const BAR_GAP = 4;

// The design draws the same green at three strengths. One colour at three
// opacities rather than three hard-coded tints, so a theme change carries.
const NEWEST = 1;
const COMPLETE = 0.35;
const INCOMPLETE = 0.15;

// Ticks at the first, fourth, seventh and last month -- the design's own axis
// (`Aug '25 · Nov '25 · Feb '26 · Jul '26`). Twelve labels overlap at this
// width, and an evenly-spaced rule would drop the newest month, which is the
// one the eye goes to first.
const TICKS = [0, 3, 6, 11];

export function NetWorthChart({ points }: { points: TrendPoint[] }) {
  const known = points.filter((point) => point.netWorthMinor !== null);

  // Fewer than two known months is not a trend. A brand-new household has
  // every account opened today, so it has exactly one -- and a single bar
  // pinned to the right-hand edge with eleven empty slots beside it says less
  // than the sentence does.
  if (known.length < 2) {
    return (
      <p data-testid="net-worth-chart-empty" className="mt-4 text-[12.5px] text-muted">
        {FINANCES_COPY.trendEmpty}
      </p>
    );
  }

  const values = known.map((point) => point.netWorthMinor as number);
  // The baseline is zero, not the smallest figure: a debt-heavy month has to
  // read as below the line, and bars measured from an arbitrary floor would
  // make a household worth 1000 look like one worth nothing.
  const max = Math.max(0, ...values);
  const min = Math.min(0, ...values);
  const span = max - min || 1; // every figure is zero: draw them on the line
  const baselineY = PAD_TOP + (max / span) * PLOT_HEIGHT;
  const barWidth = (PLOT_WIDTH - BAR_GAP * (points.length - 1)) / points.length;

  const firstComplete = points.find((point) => point.netWorthMinor !== null && point.complete);
  const hasIncomplete = known.some((point) => !point.complete);

  return (
    <div className="mt-4">
      <svg
        viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
        // role="img" + aria-label: bars have nothing for a screen reader to
        // announce on their own, and the label names the range and how much of
        // it carries a figure rather than merely saying "chart".
        role="img"
        aria-label={FINANCES_COPY.trendChartLabel(
          monthTickLabel(points[0].month),
          monthTickLabel(points[points.length - 1].month),
          known.length,
        )}
        className="w-full text-accent"
      >
        {points.map((point, index) => {
          if (point.netWorthMinor === null) return null;
          const y = PAD_TOP + ((max - point.netWorthMinor) / span) * PLOT_HEIGHT;
          return (
            <rect
              key={point.month}
              data-testid="net-worth-bar"
              data-month={point.month}
              data-complete={point.complete}
              x={PAD_X + index * (barWidth + BAR_GAP)}
              y={Math.min(y, baselineY)}
              width={barWidth}
              // A month that is exactly zero still gets a visible sliver, so
              // "we knew, and it was nothing" does not look like a gap.
              height={Math.max(1, Math.abs(baselineY - y))}
              rx={2}
              fill="currentColor"
              opacity={
                index === points.length - 1 ? NEWEST : point.complete ? COMPLETE : INCOMPLETE
              }
            />
          );
        })}
        {TICKS.map((index) => (
          <text
            key={points[index].month}
            x={PAD_X + index * (barWidth + BAR_GAP) + barWidth / 2}
            y={HEIGHT - 8}
            textAnchor="middle"
            fontSize={9}
            fill="currentColor"
            className="text-muted"
          >
            {monthTickLabel(points[index].month)}
          </text>
        ))}
      </svg>
      {hasIncomplete && (
        <p data-testid="net-worth-chart-note" className="mt-2 text-[11.5px] text-muted">
          {firstComplete
            ? FINANCES_COPY.trendIncomplete(monthTickLabel(firstComplete.month))
            : FINANCES_COPY.trendIncompleteUnknownStart}
        </p>
      )}
    </div>
  );
}
```

- [ ] **Step 6: Run the tests to verify they pass**

```bash
cd web && npx vitest run src/features/money/NetWorthChart.test.tsx
```

Expected: PASS, all five.

- [ ] **Step 7: Mutation-check the gap test**

In `NetWorthChart.tsx`, change `if (point.netWorthMinor === null) return null;` to treat a gap as zero:

```tsx
          const value = point.netWorthMinor ?? 0;
```
(and use `value` below).

Run the suite. Expected: "draws no bar at all for a month with no figure" FAILS. Revert and confirm green.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/money/NetWorthChart.tsx web/src/features/money/NetWorthChart.test.tsx \
        web/src/features/money/schemas.ts web/src/features/money/copy.ts
git commit -m "feat(web): the twelve-month net worth chart

Inline SVG, no charting dependency. A month with no figure draws no bar
rather than a zero-height one, and fewer than two known months draws the
sentence instead -- which is the state every household is in on day one.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Mount it, and the percentage on both screens

**Files:**
- Modify: `web/src/features/money/NetWorthCard.tsx`
- Modify: `web/src/features/money/FinancesPage.tsx:150-152`
- Modify: `web/src/features/overview/OverviewPage.tsx:79`
- Modify: `web/src/features/overview/copy.ts`
- Test: `web/src/features/money/FinancesPage.test.tsx`, `web/src/features/overview/OverviewPage.test.tsx`

**Interfaces:**
- Consumes: `<NetWorthChart>`, `FINANCES_COPY.trendChange` (Task 6).
- Produces: `<NetWorthCard summary={summary} chart?={ReactNode} changeNote?={string} />`.

- [ ] **Step 1: Write the failing tests**

In `web/src/features/money/FinancesPage.test.tsx`, add a series helper beside `EMPTY_TRANSACTIONS`:

```tsx
// Twelve complete months, oldest first, in the window the server sends. A
// helper rather than a literal in each test: the two tests below differ only
// in whether changeBasisPoints is present, and spelling the points out twice
// would bury the one difference that matters.
function trendBody(changeBasisPoints?: number) {
  const months = [
    "2025-08", "2025-09", "2025-10", "2025-11",
    "2025-12", "2026-01", "2026-02", "2026-03",
    "2026-04", "2026-05", "2026-06", "2026-07",
  ];
  return {
    points: months.map((month, index) => ({
      month,
      netWorthMinor: 800000 + index * 2000,
      complete: true,
    })),
    ...(changeBasisPoints === undefined ? {} : { changeBasisPoints }),
  };
}
```

Then append two tests inside the existing `describe("FinancesPage", ...)`:

```tsx
  it("shows the twelve-month chart and the change beside the figure", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture("owner") },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/transactions": EMPTY_TRANSACTIONS,
      "GET /api/v1/accounts": {
        status: 200,
        body: {
          accounts: [{
            id: "a1", nickname: "DBS Everyday", type: "cash",
            ownerMembershipId: null, ownerName: null,
            balance: { amountMinor: 824055, currency: "SGD" },
            openingBalance: { amountMinor: 800000, currency: "SGD" },
            balanceAsOf: "2026-07-26",
            countTowardNetWorth: true, visibleToLimitedMembers: false,
            archivedAt: null,
          }],
          summary: {
            currency: "SGD", computable: true, netWorthMinor: 824055,
            assetsMinor: 824055, liabilitiesMinor: 0,
            breakdown: [{ type: "cash", totalMinor: 824055 }],
            excludedNoRate: [], excludedByChoice: 0,
            trend: trendBody(210),
          },
        },
      },
    });

    const { container } = renderWithRouter(<FinancesPage />);

    expect(await screen.findByText("▲ 2.1%")).toBeInTheDocument();
    expect(screen.getByText(FINANCES_COPY.trendWindow)).toBeInTheDocument();
    await waitFor(() =>
      expect(container.querySelectorAll("[data-testid='net-worth-bar']").length).toBe(12),
    );
  });

  // The server omits changeBasisPoints whenever the comparison would be
  // dishonest -- either month unknown or incomplete, or a base of zero or
  // less. Absent must render as nothing: not "0.0%", not a dash, which would
  // each read as a measurement that came back empty rather than one nobody
  // can honestly make.
  it("renders no change at all when the server sent none", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture("owner") },
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/transactions": EMPTY_TRANSACTIONS,
      "GET /api/v1/accounts": {
        status: 200,
        body: {
          accounts: [{
            id: "a1", nickname: "DBS Everyday", type: "cash",
            ownerMembershipId: null, ownerName: null,
            balance: { amountMinor: 824055, currency: "SGD" },
            openingBalance: { amountMinor: 800000, currency: "SGD" },
            balanceAsOf: "2026-07-26",
            countTowardNetWorth: true, visibleToLimitedMembers: false,
            archivedAt: null,
          }],
          summary: {
            currency: "SGD", computable: true, netWorthMinor: 824055,
            assetsMinor: 824055, liabilitiesMinor: 0,
            breakdown: [{ type: "cash", totalMinor: 824055 }],
            excludedNoRate: [], excludedByChoice: 0,
            trend: trendBody(),
          },
        },
      },
    });

    const { container } = renderWithRouter(<FinancesPage />);

    await waitFor(() =>
      expect(container.querySelectorAll("[data-testid='net-worth-bar']").length).toBe(12),
    );
    // The arrows, not "%": no other figure on this screen carries one, so
    // their absence is the precise claim, and it holds whichever direction a
    // future change might have gone in.
    expect(screen.queryByText(/[▲▼]/)).toBeNull();
  });
```

In `web/src/features/overview/OverviewPage.test.tsx`, extend the existing `summaryBody` helper and add a series of its own — spelled out here rather than imported from the money tests, the same reason that file's own comment gives for its transaction fixture:

```tsx
function summaryBody(netWorthMinor: number, trend?: unknown) {
  return {
    computable: true,
    currency: "SGD",
    netWorthMinor,
    assetsMinor: netWorthMinor,
    liabilitiesMinor: 0,
    breakdown: [],
    excludedNoRate: [],
    excludedByChoice: 0,
    ...(trend === undefined ? {} : { trend }),
  };
}

// Two complete months is all the card needs: it draws no chart, only the
// percentage between the newest month and the one before it.
function trendBody(changeBasisPoints: number) {
  const months = [
    "2025-08", "2025-09", "2025-10", "2025-11",
    "2025-12", "2026-01", "2026-02", "2026-03",
    "2026-04", "2026-05", "2026-06", "2026-07",
  ];
  return {
    points: months.map((month, index) => ({
      month,
      netWorthMinor: 1200000 + index * 4000,
      complete: true,
    })),
    changeBasisPoints,
  };
}
```

Then add one test. Copy the route map from this file's own first test — "shows net worth, this month's budget, the next bill and goals on track to an owner" — verbatim, changing only the accounts route:

```tsx
  it("shows the change on the net worth card, and never the chart", async () => {
    const { container } = renderOverview({
      // ...the same routes as the first test in this file, except:
      "GET /api/v1/accounts": {
        status: 200,
        body: { accounts: [], summary: summaryBody(1248000, trendBody(210)) },
      },
    });

    expect(await screen.findByText("▲ 2.1% this month")).toBeInTheDocument();
    // The design draws no chart here, and the card must not grow one just
    // because the data to draw it arrived.
    expect(container.querySelector("[data-testid='net-worth-bar']")).toBeNull();
  });
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd web && npx vitest run src/features/money/FinancesPage.test.tsx src/features/overview/OverviewPage.test.tsx
```

Expected: FAIL — the `▲ 2.1%` text is not found.

- [ ] **Step 3: Give `NetWorthCard` the change and a chart slot**

In `web/src/features/money/NetWorthCard.tsx`, change the signature and the computable branch. Add `import type { ReactNode } from "react";`.

```tsx
// chart is a slot rather than something this card decides for itself: the
// design draws the bars on Finances and not on Overview, and both screens
// mount this same card. changeNote is the one word of copy that differs
// between them -- Overview's card says "this month", Finances' figure sits
// beside its own "Last 12 months" heading and does not need it.
export function NetWorthCard({
  summary,
  chart,
  changeNote,
}: {
  summary: Summary;
  chart?: ReactNode;
  changeNote?: string;
}) {
```

Inside the computable return, after the figure paragraph:

```tsx
      <p className="mt-1.5 text-[30px] font-semibold tracking-[-0.03em] text-ink">
        {formatMoney(summary.netWorthMinor, summary.currency, symbol)}
        {summary.trend?.changeBasisPoints !== undefined && (
          <span
            className={`ml-2 text-[13px] font-semibold tracking-normal ${
              summary.trend.changeBasisPoints < 0 ? "text-danger" : "text-accent"
            }`}
          >
            {FINANCES_COPY.trendChange(summary.trend.changeBasisPoints)}
            {changeNote ? ` ${changeNote}` : ""}
          </span>
        )}
      </p>
      {chart}
```

The not-computable branch is untouched: it returns before any of this, and a summary with no figure has no trend either.

- [ ] **Step 4: Mount the chart on Finances**

In `web/src/features/money/FinancesPage.tsx`, import `NetWorthChart`, and replace the grid:

```tsx
      {/* 1.7fr/1fr, not two equal columns: the chart needs the width the
          design gives it, and the breakdown card is a list that does not. */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-[1.7fr_1fr]">
        <NetWorthCard
          summary={summary}
          chart={summary.computable && summary.trend
            ? <NetWorthChart points={summary.trend.points} />
            : null}
        />
        <BreakdownCard summary={summary} />
      </div>
```

Add the "Last 12 months" label from the design to the card's heading row — in `NetWorthCard`, wrap the `<h2>` so the label sits opposite it, and only when a chart is present:

```tsx
      <div className="flex items-baseline justify-between">
        <h2 id="net-worth-heading" className="text-xs text-muted">
          {FINANCES_COPY.netWorth}
        </h2>
        {chart && <span className="text-xs text-muted">{FINANCES_COPY.trendWindow}</span>}
      </div>
```

- [ ] **Step 5: Give Overview its note**

In `web/src/features/overview/copy.ts`, add to `OVERVIEW_COPY`:

```ts
  // The design's card reads "▲ 2.1% this month" -- the same figure Finances
  // shows, with the window named, because there is no "Last 12 months"
  // heading here to imply it.
  netWorthChangeNote: "this month",
```

And in `OverviewPage.tsx:79`:

```tsx
            {accounts.data?.summary && (
              <NetWorthCard summary={accounts.data.summary} changeNote={OVERVIEW_COPY.netWorthChangeNote} />
            )}
```

- [ ] **Step 6: Run the frontend suite**

```bash
cd web && npx vitest run
```

Expected: PASS, including every pre-existing test — the trend is optional in the schema, so old fixtures still parse.

- [ ] **Step 7: Run the linters**

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
make lint
```

Expected: arch lint, typecheck, eslint and `go vet` all clean.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/money/NetWorthCard.tsx web/src/features/money/FinancesPage.tsx \
        web/src/features/money/FinancesPage.test.tsx \
        web/src/features/overview/OverviewPage.tsx web/src/features/overview/copy.ts \
        web/src/features/overview/OverviewPage.test.tsx
git commit -m "feat(web): mount the trend on Finances, the change on both

One card, two screens: Overview takes the figure and the percentage,
Finances fills the chart slot as well. An absent percentage renders as
nothing, because the server omits it exactly when saying one would be
dishonest.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Browser walk and the documents

**Files:**
- Modify: `docs/FEATURE_TRACKER.md`
- Modify: `docs/SYSTEM_DESIGN.md`
- Modify: `docs/LEARNING.md`

**Interfaces:**
- Consumes: everything above.
- Produces: nothing code depends on.

- [ ] **Step 1: Run the whole suite**

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make lint && make test
```

Expected: green. Do not proceed on a failure — fix it first.

- [ ] **Step 2: Walk it in a real browser**

```bash
make dev    # http://localhost:5173
make seed   # prints sign-in details
```

Drive it with the browser tools (Claude in Chrome or Playwright MCP). The checks, in order:

1. Sign in as the seeded owner. On **Overview**, the net worth card shows a figure and, if the seed has two comparable months, `▲ x.x% this month`.
2. Go to **Finances**. The card shows the figure, `Last 12 months` opposite the heading, twelve bars, and four month labels ending at the current month.
3. **Read the newest bar against the headline.** They must agree — this is the whole feature.
4. Add an account dated **today** with a large opening balance. Reload. Earlier months are now lighter, the note appears under the chart, and the percentage is **gone**.
5. Log an expense on an existing account dated **today**. The headline drops, and the newest bar drops with it.
6. Log an expense dated **next month** — the transaction form has no upper bound on the date and the service has no future-date guard, unlike an account's opening date, so this is reachable through the UI. The headline drops again; the newest bar follows; the second-newest bar does **not** move.
7. Sign in as the limited member with `money`. Finances shows account names, no amounts, no chart.
8. Narrow the window to a phone width. The chart scales and nothing scrolls sideways.

Record what each step actually showed. If anything surprises you, stop and fix it — the browser is the environment this ships in.

- [ ] **Step 3: Update `docs/FEATURE_TRACKER.md`**

- Finances table: `| Net worth with 12-month trend | 🟡 |` becomes `| Net worth with 12-month trend | ✅ |`.
- **Rewrite the prose that begins "Net worth is missing only its 12-month trend."** It asserts the trend needs a snapshot table, a second table and a scheduling decision. It does not, and never did. Replace it with what is now true: the series is derived from the same transactions the balance is, all twelve bars are recomputed on every read, and the trade-off (history is not frozen) is decision 1 of the spec.
- Overview row: `| Net worth card | 🟡 — ... |` keeps its 🟡 but the gap narrows to the assets/liabilities split only; the trend half is done.
- **Recount the summary table at the top.** Its columns must sum to the stated totals — count the rows, do not adjust the totals by hand.

- [ ] **Step 4: Update `docs/SYSTEM_DESIGN.md`**

Use the **`maintaining-system-design`** skill. What changed:

- a new query, `ListAccountMonthlyMovements`, and a new method on `AccountRepository`
- `AccountService.Summary` now takes `today` and returns a `Trend`
- `GET /accounts` carries `summary.trend`
- the Finances request flow gains one repository read

The prose under the diagrams is where the non-obvious part lives: say that the chart is derived rather than stored, and that its newest bar is the headline figure by construction.

- [ ] **Step 5: Add the entry to `docs/LEARNING.md`**

The defect is not in code. `docs/FEATURE_TRACKER.md` asserted an implementation constraint — "the trend needs balance snapshots: a second table" — that nobody checked against the code. It was false when written: balances here have always been derived. A shippable feature sat behind an imagined migration.

What would have caught it sooner: reading the query before believing the note about it. A document that states a constraint should cite the code that imposes it, so a later reader can check the citation instead of trusting the claim.

If this matches an existing pattern in that file, add it there as evidence rather than opening a new section.

- [ ] **Step 6: Hunt sibling defects**

Use the **`hunting-sibling-defects`** skill. The specific question: is there anywhere else that computes a balance or a net worth from these rows with a filter that has now drifted from `ListAccounts`'s?

```bash
grep -rn "opening_balance_as_of" api/internal/adapter/postgres/queries/
```

Every hit must apply the same `>=` rule. Note what you found in the commit message even if the answer is "nothing".

- [ ] **Step 7: Commit**

```bash
git add docs/FEATURE_TRACKER.md docs/SYSTEM_DESIGN.md docs/LEARNING.md
git commit -m "docs: net worth trend shipped, and the note that delayed it

The feature tracker asserted this needed a snapshot table. It never did,
and nobody checked -- the lesson goes in LEARNING.md, because a document
that states a constraint without citing the code imposing it is a claim
the next reader will believe.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Notes for the executor

- **`make sqlc` regenerates `sqlcgen/`.** Never hand-edit those files. If a generated field name differs from what Task 1 Step 5 assumes, follow the generated name.
- **The one assertion that matters** is that the newest bar equals the headline figure. If any later change makes it awkward, that is a signal about the change, not about the test.
- **A gap is `null` everywhere it appears** — in Go (`*domain.Money`), on the wire (`"netWorthMinor": null`), and in the chart (no `<rect>` at all). Any layer that turns one into a `0` breaks the rule the whole feature is built on.
