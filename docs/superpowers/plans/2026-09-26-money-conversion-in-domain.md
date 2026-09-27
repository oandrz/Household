# Money conversion in domain — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace five hand-copied "convert into the primary currency"
methods with one shared per-request `Converter`. Move `Rate` and its rounding
into `domain`. Make "no rate" a named error, so a screen leaves an amount out
of a total only when a rate is truly missing.

**Architecture:**
- `domain` gains `Rate` (moved from `usecase/ports.go`), the sentinels
  `ErrNoRate` and `ErrInvalidRate`, and one unexported 128-bit
  multiply-divide-round helper. `Rate.Apply`, `Quantity.Value` and
  `Money.Prorate` all use that helper.
- `usecase` gains an exported `Converter` in its own file, built once per
  request from the `FXRateProvider` each service already declares.
- Every caller leaves an item out only on `domain.ErrNoRate`, and returns every
  other error.

**Tech Stack:** Go 1.25 (`api/go.mod`), `math/bits`, the standard `testing`
package. Usecase tests use the in-memory doubles in
`api/internal/usecase/testdouble_test.go`.

**Spec:** the decisions are in `docs/reviews/2026-09-26-architecture-review.md`:
item 2, and the Log entry "2b decisions". Terms are in `CONTEXT.md` (Primary
currency, Rate, No rate). Read both before starting.

## Global Constraints

- Money is `int64` minor units plus an ISO 4217 code. `float64` never appears
  on a monetary path. Rates are fractions, never scaled decimals (CLAUDE.md).
- The dependencies rule, checked by `make lint-arch`:
  - `internal/domain` imports only the standard library.
  - `internal/usecase` may import `domain`.
  - Adapters may import both.
- Each service keeps `FX FXRateProvider` in its own `Deps`. No service gets a
  shared converter injected; each builds one per request with `NewConverter`.
- A caller may leave an amount out of a total (the `ExcludedNoRate` lists and
  counts) **only** when `errors.Is(err, domain.ErrNoRate)`. Every other error
  from `Converter.Convert` is returned.
- The net-worth trend (`usecase/networth_trend.go`) keeps its current behaviour:
  any conversion error fails it.
- `BillService.List`'s probe-then-convert structure stays as it is. Swap the
  call only (item 3 owns the restructure).
- Comments say **why**, at the point someone would change the code. When a
  comment that called the old duplication "deliberate" is deleted, the
  `Converter` doc comment is where that reasoning now lives.
- Every task ends green: `go build ./...` and the task's tests pass.
- Toolchain on this machine: run from `api/` with
  `export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH`.
  `GOTOOLCHAIN=auto` fetches 1.25. The Postgres suite and `make test` also
  need:
  `export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock`
  `export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`

## Review Focus

1. **A rate provider outage, or a request cancelled mid-flight.** The screen
   must answer with an error, not a smaller total that lists every foreign
   item as "no rate". Pinned per caller in Tasks 3–6.
2. **A single-currency household during a provider outage.** It must keep
   working, because a primary-currency amount never asks the provider. Pinned
   in Task 3 (`TestConverterNeverAsksTheProviderForAPrimaryAmount`).
3. **A provider returning a zero or negative numerator or denominator.** Today
   `Apply` divides by zero and panics. After this change it must return
   `ErrInvalidRate`. Pinned in Task 1.
4. **Negative amounts** (credit cards, loans) round half away from zero exactly
   as positive ones do, down to `math.MinInt64`. Pinned in Task 1.
5. **Two amounts in the same foreign currency on one screen** must use one
   rate lookup, so figures on one page cannot disagree. Pinned in Task 3.

---

## File map

| File | Change | Responsibility after |
|---|---|---|
| `api/internal/domain/muldiv.go` | create | the one multiply-divide-round rule |
| `api/internal/domain/rate.go` | create | `Rate` and `Rate.Apply` |
| `api/internal/domain/rate_test.go` | create | `Rate.Apply` table tests (moved from `adapter/fx/static_test.go` and extended) |
| `api/internal/domain/errors.go` | modify | add `ErrNoRate`, `ErrInvalidRate` |
| `api/internal/domain/quantity.go` | modify | `Quantity.Value` and `Money.Prorate` call `mulDivRoundHalfAway` |
| `api/internal/usecase/ports.go` | modify | delete `Rate`, `Apply` and `mulOverflows`; `FXRateProvider` returns `domain.Rate` and documents `ErrNoRate` |
| `api/internal/adapter/fx/static.go` + `static_test.go` | modify | return `domain.Rate`; an unknown pair wraps `domain.ErrNoRate` |
| `api/internal/usecase/converter.go` + `converter_test.go` | create | the per-request `Converter` |
| `api/internal/usecase/networth.go`, `networth_trend.go` | modify | use `Converter`; delete the private `converter` type |
| `api/internal/usecase/monthsummary.go`, `budget.go`, `goal.go`, `bill.go` | modify | use `Converter`; delete each private `convert` |
| `api/internal/usecase/testdouble_test.go` | modify | one `fxDouble` replaces `staticTestRates`, `noRateFX` and `countingRates` |
| `docs/SYSTEM_DESIGN.md`, `docs/LEARNING.md` | modify | keep the docs true |
| `docs/reviews/2026-09-26-architecture-review.md` | modify, **never commit** | status and log of the ranked list (see Task 0) |

---

### Task 0: Branch

- [ ] **Step 1: Check out the branch**

The branch already exists. Its first commit holds this plan and `CONTEXT.md`.

```bash
cd /Volumes/Oink_Machine/Intelij/HouseholdDashboard
git checkout refactor/money-conversion-in-domain
git log --oneline -1   # expect: "docs: plan for money conversion in domain, and CONTEXT.md"
```

`docs/reviews/2026-09-26-architecture-review.md` is deliberately **untracked**.
It names a security weakness (B2) that is not fixed yet, and the repository is
public. Read it and edit it, but never `git add` it, until B2 has landed.

---

### Task 1: `domain.Rate` and the one rounding rule

**Files:**
- Create: `api/internal/domain/muldiv.go`
- Create: `api/internal/domain/rate.go`
- Create: `api/internal/domain/rate_test.go`
- Modify: `api/internal/domain/errors.go` (the `var` block that holds `ErrAmountOverflow`, around line 51)
- Modify: `api/internal/domain/quantity.go` (`Quantity.Value` ~line 64, `Money.Prorate` ~line 194)

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `type domain.Rate struct { Numerator, Denominator int64 }`
  - `func (r domain.Rate) Apply(minorUnits int64) (int64, error)`, which returns `ErrInvalidRate` or `ErrAmountOverflow`, each wrapped.
  - `var domain.ErrNoRate`
  - `var domain.ErrInvalidRate`
  - unexported `mulDivRoundHalfAway(a, num, den int64) (int64, bool)`

`usecase.Rate` still exists after this task. Task 2 removes it.

- [ ] **Step 1: Write the failing tests**

Create `api/internal/domain/rate_test.go`:

```go
package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestRateApplyRoundsHalfAwayFromZero(t *testing.T) {
	cases := []struct {
		name string
		rate domain.Rate
		in   int64
		want int64
	}{
		{"SGD to IDR, whole multiple", domain.Rate{Numerator: 12_410, Denominator: 1}, 10_000, 124_100_000},
		{"IDR to SGD, exact", domain.Rate{Numerator: 1, Denominator: 12_410}, 124_100_000, 10_000},
		{"zero stays zero", domain.Rate{Numerator: 1, Denominator: 12_410}, 0, 0},
		{"exact half rounds up", domain.Rate{Numerator: 1, Denominator: 2}, 5, 3},
		{"below half rounds down", domain.Rate{Numerator: 1, Denominator: 3}, 4, 1},
		{"above half rounds up", domain.Rate{Numerator: 1, Denominator: 3}, 5, 2},
		// A credit card or loan balance is negative. "Away from zero" must
		// mean the same distance from zero on both sides, never "towards
		// minus infinity".
		{"negative exact half rounds away from zero", domain.Rate{Numerator: 1, Denominator: 2}, -5, -3},
		{"negative below half", domain.Rate{Numerator: 1, Denominator: 3}, -4, -1},
		{"negative above half", domain.Rate{Numerator: 1, Denominator: 3}, -5, -2},
		{"most negative amount at the identity rate", domain.Rate{Numerator: 1, Denominator: 1}, math.MinInt64, math.MinInt64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.rate.Apply(tc.in)
			if err != nil {
				t.Fatalf("Apply(%d): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("Apply(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestRateApplyRefusesAnAnswerTooBigForInt64(t *testing.T) {
	cases := []struct {
		name string
		rate domain.Rate
		in   int64
	}{
		{"largest amount into IDR", domain.Rate{Numerator: 12_410, Denominator: 1}, math.MaxInt64},
		{"most negative amount doubled", domain.Rate{Numerator: 2, Denominator: 1}, math.MinInt64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.rate.Apply(tc.in); !errors.Is(err, domain.ErrAmountOverflow) {
				t.Fatalf("Apply(%d) error = %v, want domain.ErrAmountOverflow", tc.in, err)
			}
		})
	}
}

// The old usecase.Rate.Apply multiplied in 64 bits and refused whenever the
// intermediate product overflowed, even when the answer fitted. The product
// is now taken in 128 bits, so only the answer can be refused. This is the one
// deliberate behaviour change in the move.
func TestRateApplyAcceptsAnAmountWhoseAnswerFitsEvenWhenTheProductDoesNot(t *testing.T) {
	rate := domain.Rate{Numerator: 3, Denominator: 2}
	in := int64(math.MaxInt64 / 2) // 4_611_686_018_427_387_903; times 3 overflows int64

	got, err := rate.Apply(in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// 4_611_686_018_427_387_903 * 3 / 2 = 6_917_529_027_641_081_854.5, rounded away from zero.
	if want := int64(6_917_529_027_641_081_855); got != want {
		t.Fatalf("Apply = %d, want %d", got, want)
	}
}

// A rate arrives from a provider this code did not construct, so it fails
// closed. A zero denominator used to divide by zero and panic.
func TestRateApplyRefusesARateThatIsNotPositive(t *testing.T) {
	for _, r := range []domain.Rate{
		{Numerator: 0, Denominator: 1},
		{Numerator: 1, Denominator: 0},
		{Numerator: -1, Denominator: 1},
		{Numerator: 1, Denominator: -1},
	} {
		if _, err := r.Apply(100); !errors.Is(err, domain.ErrInvalidRate) {
			t.Fatalf("%+v.Apply error = %v, want domain.ErrInvalidRate", r, err)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd api && go test ./internal/domain/ -run 'TestRateApply' -v`
Expected: FAIL to compile, with `undefined: domain.Rate`.

- [ ] **Step 3: Add the two sentinels**

In `api/internal/domain/errors.go`, inside the `var (...)` block that declares
`ErrAmountOverflow`, add, directly under it:

```go
	// ErrNoRate: there is no rate between two currencies. An FXRateProvider
	// returns it, wrapped, for a pair it does not cover. It is the ONLY
	// conversion failure a screen may answer by leaving an amount out of a
	// total (CONTEXT.md, "No rate"); every other conversion error fails the
	// request, because a smaller total labelled "no rate" would be a false
	// statement about the household's money.
	ErrNoRate = errors.New("no rate between these currencies")
	// ErrInvalidRate: a rate whose numerator or denominator is not positive.
	// Rates come from a provider this code does not construct, so Rate.Apply
	// checks rather than trusts.
	ErrInvalidRate = errors.New("rate is invalid")
```

- [ ] **Step 4: Write the rounding helper**

Create `api/internal/domain/muldiv.go`:

```go
package domain

import (
	"math"
	"math/bits"
)

// mulDivRoundHalfAway returns a*num/den, rounded half away from zero. It is
// the one rounding rule for every monetary multiply-then-divide in Hearth:
// currency conversion (Rate.Apply), a holding's value (Quantity.Value) and a
// cost pool's share (Money.Prorate). It exists so that those three stop each
// carrying a copy with a comment promising the copies match.
//
// a may be negative. num must be >= 0 and den must be > 0; every caller checks
// that first and reports its own error, so ok=false here means only "the
// answer does not fit in an int64". The product is taken in 128 bits, so an
// intermediate that is too big never causes a refusal on its own; only the
// answer can.
func mulDivRoundHalfAway(a, num, den int64) (int64, bool) {
	if num < 0 || den <= 0 {
		return 0, false
	}
	negative := a < 0
	magnitude := uint64(a)
	if negative {
		// |a| without negating a: -math.MinInt64 does not fit in an int64,
		// but ^a + 1 in uint64 is exactly its magnitude, 2^63.
		magnitude = uint64(^a) + 1
	}

	hi, lo := bits.Mul64(magnitude, uint64(num))
	// bits.Div64 PANICS when the quotient needs more than 64 bits, which is
	// exactly when hi >= den. Check first and report instead: a panic on a
	// monetary path is worse than the overflow it would replace.
	if hi >= uint64(den) {
		return 0, false
	}
	quo, rem := bits.Div64(hi, lo, uint64(den))

	// rem < den <= math.MaxInt64, so doubling it cannot wrap.
	if rem*2 >= uint64(den) {
		quo++
		if quo == 0 { // wrapped past the largest uint64
			return 0, false
		}
	}

	if negative {
		if quo > 1<<63 {
			return 0, false
		}
		if quo == 1<<63 {
			return math.MinInt64, true
		}
		return -int64(quo), true
	}
	if quo > math.MaxInt64 {
		return 0, false
	}
	return int64(quo), true
}
```

- [ ] **Step 5: Write `Rate`**

Create `api/internal/domain/rate.go`:

```go
package domain

import "fmt"

// Rate is how many units of one currency one unit of another is worth, held
// as a fraction rather than a scaled decimal. SGD to IDR is {12410, 1}; IDR to
// SGD is {1, 12410}. A scaled decimal cannot represent the second direction --
// 0.0000806 truncates to zero at any sane scale -- and IDR to SGD is precisely
// the direction the design's Finances screen uses.
type Rate struct {
	Numerator   int64
	Denominator int64
}

// Apply converts an amount of minor units, rounding half away from zero.
//
// A rate whose numerator or denominator is not positive is refused with
// ErrInvalidRate. It comes from a provider this code did not construct, and a
// zero denominator would otherwise divide by zero. An answer that does not fit
// in an int64 is refused with ErrAmountOverflow rather than silently wrapping.
func (r Rate) Apply(minorUnits int64) (int64, error) {
	if r.Numerator <= 0 || r.Denominator <= 0 {
		return 0, fmt.Errorf("%w: %d/%d", ErrInvalidRate, r.Numerator, r.Denominator)
	}
	out, ok := mulDivRoundHalfAway(minorUnits, r.Numerator, r.Denominator)
	if !ok {
		return 0, fmt.Errorf("%w: %d at %d/%d", ErrAmountOverflow, minorUnits, r.Numerator, r.Denominator)
	}
	return out, nil
}
```

- [ ] **Step 6: Run the new tests**

Run: `cd api && go test ./internal/domain/ -run 'TestRateApply' -v`
Expected: PASS, all four tests.

- [ ] **Step 7: Point `Quantity.Value` and `Money.Prorate` at the helper**

In `api/internal/domain/quantity.go`, `Quantity.Value`: keep the currency and
negative-price refusals at the top of the function. Replace everything from
`hi, lo := bits.Mul64(uint64(q.nano), uint64(unitPrice.Amount))` down to the
final `return` with:

```go
	out, ok := mulDivRoundHalfAway(q.nano, unitPrice.Amount, QuantityScale)
	if !ok {
		return Money{}, fmt.Errorf("%w: %d nano units at %d", ErrAmountOverflow, q.nano, unitPrice.Amount)
	}
	return Money{Amount: out, Currency: unitPrice.Currency}, nil
```

In `Money.Prorate`: keep the currency, negative-amount, `whole <= 0` and
`part > whole` refusals. Replace everything from
`hi, lo := bits.Mul64(uint64(m.Amount), uint64(part.nano))` down to the final
`return` with:

```go
	out, ok := mulDivRoundHalfAway(m.Amount, part.nano, whole.nano)
	if !ok {
		return Money{}, fmt.Errorf("%w: %d prorated by %d/%d", ErrAmountOverflow, m.Amount, part.nano, whole.nano)
	}
	return Money{Amount: out, Currency: m.Currency}, nil
```

Delete every comment in `quantity.go` that says "matching usecase.Rate.Apply"
or "matching Quantity.Value and usecase.Rate.Apply". The rule now has one
home, `mulDivRoundHalfAway`, and its comment says so. Where the `Value` doc
comment says "the same commitment Money.Add and usecase.Rate.Apply make",
change `usecase.Rate.Apply` to `Rate.Apply`. In `quantity_test.go` around line
117, change `usecase.Rate.Apply` to `domain.Rate.Apply` in the comment. Remove
any import the compiler now reports unused (`math/bits` and `math` may still be
needed elsewhere in the file; let the compiler decide).

- [ ] **Step 8: Run the whole domain suite**

Run: `cd api && go test ./internal/domain/ -v 2>&1 | tail -20`
Expected: PASS, with every existing `Quantity` and `Prorate` test unchanged. If
a test asserts the old overflow message text `rounding %d nano units`, update
only the string. The sentinel it checks with `errors.Is` must stay
`ErrAmountOverflow`.

- [ ] **Step 9: Mutation-check the rounding rule**

In `muldiv.go`, change `if rem*2 >= uint64(den)` to `if rem*2 > uint64(den)`.
Run `cd api && go test ./internal/domain/ -run 'TestRateApply|Quantity|Prorate'`.
Expected: FAIL, including "exact half rounds up" and "negative exact half
rounds away from zero". Revert the change and re-run. Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add api/internal/domain/muldiv.go api/internal/domain/rate.go api/internal/domain/rate_test.go \
        api/internal/domain/errors.go api/internal/domain/quantity.go api/internal/domain/quantity_test.go
git commit -m "refactor(domain): one rounding rule, and Rate lives beside Money

Rate.Apply, Quantity.Value and Money.Prorate share mulDivRoundHalfAway
(128-bit, half away from zero). Rate.Apply now refuses a non-positive rate
with ErrInvalidRate instead of dividing by zero, and accepts an amount whose
answer fits even when the 64-bit product would not. Adds ErrNoRate for the
FX port to use.

Claude-Session: https://claude.ai/code/session_01K72Ud8izwe2cu8pVv7mLzU"
```

---

### Task 2: The FX port returns `domain.Rate`, the adapter returns `ErrNoRate`, and one FX double

**Files:**
- Modify: `api/internal/usecase/ports.go` (the `Rate` type through `mulOverflows`, ~lines 916-957; `FXRateProvider` ~line 960)
- Modify: `api/internal/adapter/fx/static.go`, `api/internal/adapter/fx/static_test.go`
- Modify: `api/internal/usecase/networth.go` (the `converter` struct's `rates` field and its construction at line 73)
- Modify: `api/internal/usecase/testdouble_test.go` (`staticTestRates`, ~line 2780)
- Modify: `api/internal/usecase/bill_test.go` (delete `noRateFX`, ~line 149-156)
- Modify: `api/internal/usecase/networth_test.go` (delete `countingRates`, ~line 300-306)
- Modify: `account_test.go`, `budget_test.go`, `goal_test.go`, `transaction_test.go`, `telegram_command_test.go` (swap the double)

**Interfaces:**
- Consumes: `domain.Rate` and `domain.ErrNoRate` (Task 1).
- Produces:
  - `FXRateProvider.Rate(ctx context.Context, from, to string) (domain.Rate, error)`
  - the test double `fxDouble`:
    - `newFXDouble() *fxDouble` knows SGD↔IDR only
    - `(*fxDouble).withNoRates() *fxDouble`
    - `(*fxDouble).failWith(err error) *fxDouble`
    - a `calls int` field

- [ ] **Step 1: Write the failing adapter test**

In `api/internal/adapter/fx/static_test.go`, replace
`TestStaticProviderRejectsAnUnknownPair` with the version below, and delete
`TestApplyConvertsAnOrdinaryAmount` and
`TestApplyReturnsErrAmountOverflowOnOverflow`, which now live in
`domain/rate_test.go`:

```go
// The port's contract: a pair the provider does not cover is ErrNoRate, and
// only that. It is what lets a screen leave an amount out instead of failing.
func TestStaticProviderAnswersAnUnknownPairWithErrNoRate(t *testing.T) {
	p := fx.NewStaticProvider()

	_, err := p.Rate(context.Background(), "SGD", "JPY")
	if !errors.Is(err, domain.ErrNoRate) {
		t.Fatalf("Rate(SGD, JPY) error = %v, want domain.ErrNoRate", err)
	}
}
```

Remove any import the file no longer uses (`math` may become unused).

- [ ] **Step 2: Run it to verify it fails**

Run: `cd api && go test ./internal/adapter/fx/ -run TestStaticProviderAnswersAnUnknownPairWithErrNoRate -v`
Expected: FAIL, with `error = no rate available for SGD to JPY, want domain.ErrNoRate`.

- [ ] **Step 3: Change the port**

In `api/internal/usecase/ports.go`:
- delete the `Rate` type, its `Apply` method and `mulOverflows`, with their comments;
- replace the `FXRateProvider` declaration with:

```go
// FXRateProvider looks up the rate between two currencies. The design labels
// the rate "auto"; a live provider replaces the static one without any caller
// changing.
//
// For a pair it has no rate for, it returns an error wrapping domain.ErrNoRate,
// and callers rely on that: it is the only failure a screen may answer by
// leaving an amount out of a total. Any other error means the lookup itself
// failed (a provider outage, a cancelled request), and the caller fails the
// request. Callers do not use this directly for arithmetic; they build a
// Converter (converter.go) per request.
type FXRateProvider interface {
	Rate(ctx context.Context, from, to string) (domain.Rate, error)
}
```

Remove the `math` import from `ports.go` if the compiler reports it unused.

- [ ] **Step 4: Change the adapter**

In `api/internal/adapter/fx/static.go`, change every `usecase.Rate` to
`domain.Rate`, and make the final return:

```go
	return domain.Rate{}, fmt.Errorf("%w: %s to %s", domain.ErrNoRate, from, to)
```

Import `github.com/andreasoentoro/hearth/api/internal/domain`. If the
`usecase` import is now unused, keep the compile-time check instead of
deleting the import, by adding this above `NewStaticProvider`:

```go
// The adapter honours the whole port contract, ErrNoRate included.
var _ usecase.FXRateProvider = (*StaticProvider)(nil)
```

- [ ] **Step 5: Change net worth's cache type**

In `api/internal/usecase/networth.go`, change the `converter` struct field to
`rates map[string]domain.Rate`, and line 73's construction to
`rates: map[string]domain.Rate{}`. (Task 3 replaces this type entirely; this
step only keeps the build green.)

- [ ] **Step 6: Replace the three FX doubles with one**

In `api/internal/usecase/testdouble_test.go`, replace the whole
`staticTestRates` type and its method with:

```go
// fxDouble is the one FX double. By default it knows exactly the pair
// fx.StaticProvider knows (SGD<->IDR) and answers every other pair with
// domain.ErrNoRate, as the real provider does.
//
//   - withNoRates empties the table, for "this currency has no rate".
//   - failWith makes every lookup fail with an error that is NOT ErrNoRate,
//     standing in for a provider outage. A caller must fail the request on it,
//     never leave the amount out.
//   - calls counts lookups, for the Converter's per-request cache.
type fxDouble struct {
	rates map[[2]string]domain.Rate
	fail  error
	calls int
}

func newFXDouble() *fxDouble {
	return &fxDouble{rates: map[[2]string]domain.Rate{
		{"SGD", "IDR"}: {Numerator: 12_410, Denominator: 1},
		{"IDR", "SGD"}: {Numerator: 1, Denominator: 12_410},
	}}
}

func (f *fxDouble) withNoRates() *fxDouble {
	f.rates = map[[2]string]domain.Rate{}
	return f
}

func (f *fxDouble) failWith(err error) *fxDouble {
	f.fail = err
	return f
}

func (f *fxDouble) Rate(_ context.Context, from, to string) (domain.Rate, error) {
	f.calls++
	if f.fail != nil {
		return domain.Rate{}, f.fail
	}
	if from == to {
		return domain.Rate{Numerator: 1, Denominator: 1}, nil
	}
	if r, ok := f.rates[[2]string{from, to}]; ok {
		return r, nil
	}
	return domain.Rate{}, fmt.Errorf("%w: %s to %s", domain.ErrNoRate, from, to)
}
```

Then delete `noRateFX` (type, method and comment) from `bill_test.go`, and
`countingRates` (type, method and comment) from `networth_test.go`. Swap every
use:

```bash
cd api/internal/usecase
sed -i '' 's/staticTestRates{}/newFXDouble()/g' account_test.go bill_test.go budget_test.go goal_test.go transaction_test.go telegram_command_test.go
sed -i '' 's/noRateFX{}/newFXDouble().withNoRates()/g' bill_test.go
sed -i '' 's/&countingRates{}/newFXDouble()/g' networth_test.go
grep -rn 'staticTestRates\|noRateFX\|countingRates' . 
```

The final `grep` will still find **comments** that name the old doubles.
Reword each to say "the FX double" (for example: "USD: the FX double only
knows SGD<->IDR, so this has no rate."). Expected after rewording: no output.

- [ ] **Step 7: Build and run everything this task touches**

Run:

```bash
cd api && go build ./... && go vet ./internal/usecase/ ./internal/adapter/fx/ \
  && go test ./internal/domain/ ./internal/usecase/ ./internal/adapter/fx/
```

Expected: PASS. The behaviour is unchanged so far, because every caller still
treats any error as "no rate". Remove imports the compiler reports unused (for
example `fmt` in `bill_test.go`).

- [ ] **Step 8: Commit**

```bash
git add -A api/internal
git commit -m "refactor(fx): the port returns domain.Rate and names ErrNoRate

FXRateProvider's contract now says an unsupported pair is ErrNoRate and
anything else is a failed lookup. The static adapter honours it. Three
usecase FX doubles become one fxDouble that can also stand in for an outage.

Claude-Session: https://claude.ai/code/session_01K72Ud8izwe2cu8pVv7mLzU"
```

---

### Task 3: The `Converter`, and net worth on it

**Files:**
- Create: `api/internal/usecase/converter.go`
- Create: `api/internal/usecase/converter_test.go`
- Modify: `api/internal/usecase/networth.go` (delete `converter` and its `convert`, ~lines 185-222; `Summary` ~lines 73 and 104-111)
- Modify: `api/internal/usecase/networth_trend.go` (param `conv *converter` ~line 107; call ~line 161; comment ~line 52)
- Test: `api/internal/usecase/networth_test.go`

**Interfaces:**
- Consumes: `FXRateProvider` returning `domain.Rate` (Task 2), `fxDouble` (Task 2).
- Produces:
  - `func usecase.NewConverter(fx FXRateProvider, primary string) *usecase.Converter`
  - `func (c *usecase.Converter) Convert(ctx context.Context, m domain.Money) (domain.Money, error)`

- [ ] **Step 1: Write the failing converter tests**

Create `api/internal/usecase/converter_test.go`:

```go
package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

var errProviderDown = errors.New("rate provider unreachable")

// A single-currency household must keep working while the provider is down,
// so a primary-currency amount never asks it.
func TestConverterNeverAsksTheProviderForAPrimaryAmount(t *testing.T) {
	fx := newFXDouble().failWith(errProviderDown)
	c := usecase.NewConverter(fx, "SGD")

	got, err := c.Convert(context.Background(), domain.Money{Amount: 824_055, Currency: "SGD"})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if got != (domain.Money{Amount: 824_055, Currency: "SGD"}) {
		t.Fatalf("Convert = %+v, want the amount unchanged", got)
	}
	if fx.calls != 0 {
		t.Fatalf("provider asked %d times, want 0", fx.calls)
	}
}

func TestConverterConvertsAtTheProvidersRate(t *testing.T) {
	c := usecase.NewConverter(newFXDouble(), "SGD")

	got, err := c.Convert(context.Background(), domain.Money{Amount: 124_100_000, Currency: "IDR"})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if got != (domain.Money{Amount: 10_000, Currency: "SGD"}) {
		t.Fatalf("Convert = %+v, want S$100.00", got)
	}
}

// One rate per currency per request: two figures on one screen can never be
// converted at two different rates.
func TestConverterAsksOncePerCurrencyPerRequest(t *testing.T) {
	fx := newFXDouble()
	c := usecase.NewConverter(fx, "SGD")
	ctx := context.Background()

	for _, amount := range []int64{124_100_000, 12_410} {
		if _, err := c.Convert(ctx, domain.Money{Amount: amount, Currency: "IDR"}); err != nil {
			t.Fatalf("Convert(%d): %v", amount, err)
		}
	}
	if fx.calls != 1 {
		t.Fatalf("provider asked %d times, want 1", fx.calls)
	}
}

func TestConverterReportsNoRateAsErrNoRate(t *testing.T) {
	c := usecase.NewConverter(newFXDouble(), "SGD")

	_, err := c.Convert(context.Background(), domain.Money{Amount: 500, Currency: "EUR"})
	if !errors.Is(err, domain.ErrNoRate) {
		t.Fatalf("Convert(EUR) error = %v, want domain.ErrNoRate", err)
	}
}

// A failed lookup is not "no rate". The caller must be able to tell the two
// apart, or an outage would render as a smaller total.
func TestConverterPassesAFailedLookupThroughAsSomethingOtherThanNoRate(t *testing.T) {
	c := usecase.NewConverter(newFXDouble().failWith(errProviderDown), "SGD")

	_, err := c.Convert(context.Background(), domain.Money{Amount: 12_410, Currency: "IDR"})
	if !errors.Is(err, errProviderDown) {
		t.Fatalf("Convert error = %v, want the provider's own error", err)
	}
	if errors.Is(err, domain.ErrNoRate) {
		t.Fatal("a failed lookup must not read as ErrNoRate")
	}
}

// A failure is not remembered, so the next conversion in the same request
// asks again rather than repeating a stale answer.
func TestConverterDoesNotRememberAFailedLookup(t *testing.T) {
	fx := newFXDouble().failWith(errProviderDown)
	c := usecase.NewConverter(fx, "SGD")
	ctx := context.Background()
	idr := domain.Money{Amount: 12_410, Currency: "IDR"}

	if _, err := c.Convert(ctx, idr); err == nil {
		t.Fatal("first Convert: want the provider's error")
	}
	fx.fail = nil
	if _, err := c.Convert(ctx, idr); err != nil {
		t.Fatalf("second Convert: %v", err)
	}
	if fx.calls != 2 {
		t.Fatalf("provider asked %d times, want 2", fx.calls)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd api && go test ./internal/usecase/ -run TestConverter -v`
Expected: FAIL to compile, with `undefined: usecase.NewConverter`.

- [ ] **Step 3: Write `Converter`**

Create `api/internal/usecase/converter.go`:

```go
package usecase

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// Converter turns amounts into one household's primary currency for the
// length of one request. Build a new one per request with NewConverter. Never
// keep one between requests, or a changed rate would never be seen.
//
// Why one type rather than a convert method on each service: the algorithm
// (skip the provider when the amount is already primary, look the rate up,
// apply it, remember it) has no reason to differ between screens, and it
// drifted when each service had its own copy; only net worth remembered its
// rates. Each service still declares FXRateProvider in its own Deps, so no
// service gains a reason to change when another's FX needs do. Only the
// algorithm is shared. Do not copy it back into the services.
//
// Remembering each rate for the request is load-bearing, not an
// optimisation. Every figure on one screen must use the same rate for the same
// currency, or, the day a live provider returns two different rates within one
// request, a headline and the chart beneath it would disagree.
type Converter struct {
	fx      FXRateProvider
	primary string
	rates   map[string]domain.Rate
}

// NewConverter returns a Converter into primary, backed by fx.
func NewConverter(fx FXRateProvider, primary string) *Converter {
	return &Converter{fx: fx, primary: primary, rates: map[string]domain.Rate{}}
}

// Convert returns m in the primary currency. An amount already in primary is
// returned unchanged without asking the provider. That is exact, and it means
// a single-currency household never depends on a rate provider being up.
//
// An error wrapping domain.ErrNoRate means m's currency has no rate. That is
// the ONLY error a caller may answer by leaving m out of a total (CONTEXT.md,
// "No rate"). Anything else (a failed lookup, domain.ErrInvalidRate,
// domain.ErrAmountOverflow) must fail the request. A failed lookup is not
// remembered, so the next Convert asks again.
func (c *Converter) Convert(ctx context.Context, m domain.Money) (domain.Money, error) {
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

- [ ] **Step 4: Run the converter tests**

Run: `cd api && go test ./internal/usecase/ -run TestConverter -v`
Expected: PASS, all six.

- [ ] **Step 5: Write the failing net-worth test**

Add to `api/internal/usecase/networth_test.go`:

```go
// A provider outage must fail the summary, not render a net worth that
// silently leaves out every foreign account as "no rate".
func TestNetWorthSummaryFailsWhenTheRateLookupItselfFails(t *testing.T) {
	svc := newAccountServiceWithFX(t, newFXDouble().failWith(errProviderDown))

	_, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{
		account(t, domain.AccountCash, 824_055, "SGD"),
		account(t, domain.AccountCash, 124_100_000, "IDR"),
	}, fixedNow)
	if !errors.Is(err, errProviderDown) {
		t.Fatalf("Summary error = %v, want the provider's error", err)
	}
}
```

Add `"errors"` to the imports if it is missing.

- [ ] **Step 6: Run to verify it fails**

Run: `cd api && go test ./internal/usecase/ -run TestNetWorthSummaryFailsWhenTheRateLookupItselfFails -v`
Expected: FAIL, with `Summary error = <nil>, want the provider's error`. The IDR
account is excluded today instead.

- [ ] **Step 7: Move net worth onto `Converter`**

In `api/internal/usecase/networth.go`:
- delete the `converter` struct, its doc comment and its `convert` method. The
  doc comment's reasoning now lives on `Converter`.
- line 73: `conv := NewConverter(s.d.FX, primary)`
- replace the conversion in the accounts loop (~lines 104-111) with:

```go
		inPrimary, err := conv.Convert(ctx, view.Balance)
		if errors.Is(err, domain.ErrNoRate) {
			summary.ExcludedNoRate = append(summary.ExcludedNoRate, ExcludedAccount{
				AccountID: view.Account.ID,
				Currency:  view.Balance.Currency,
			})
			continue
		}
		if err != nil {
			return NetWorthSummary{}, err
		}
```

Add the `"errors"` import if missing. If `conv` is passed on to the trend,
leave that call as it is.

In `api/internal/usecase/networth_trend.go`: change the parameter
`conv *converter` to `conv *Converter`, and `conv.convert(` to `conv.Convert(`.
Its `if err != nil { return nil, err }` stays exactly as it is: the trend fails
on any error, per the Global Constraints. In the comment near line 52, change
"the converter's per-request rate cache" to "Converter's per-request rate
cache".

- [ ] **Step 8: Run the usecase suite**

Run: `cd api && go test ./internal/usecase/`
Expected: PASS, including the existing
`TestSummaryExcludesAndNamesAnAccountWithNoRate` and the cache test in
`networth_test.go` that counts provider calls.

- [ ] **Step 9: Commit**

```bash
git add api/internal/usecase/converter.go api/internal/usecase/converter_test.go \
        api/internal/usecase/networth.go api/internal/usecase/networth_trend.go api/internal/usecase/networth_test.go
git commit -m "refactor(usecase): one per-request Converter; net worth fails on a failed lookup

Net worth's private converter becomes the shared Converter. Summary now
excludes an account only on ErrNoRate; a provider outage fails the request
instead of rendering a smaller net worth.

Claude-Session: https://claude.ai/code/session_01K72Ud8izwe2cu8pVv7mLzU"
```

---

### Task 4: Month summary and budget on `Converter`

**Files:**
- Modify: `api/internal/usecase/monthsummary.go` (loop ~lines 78-94; delete `convert` ~lines 97-120)
- Modify: `api/internal/usecase/budget.go` (`tallySpend` ~lines 308-340; delete `convert` ~lines 465-484)
- Modify: `api/internal/usecase/transaction_test.go` (add `newTransactionFixtureWithFX`)
- Modify: `api/internal/usecase/budget_test.go` (add an `fx *fxDouble` field to `budgetFixture`)
- Test: `api/internal/usecase/monthsummary_test.go`, `api/internal/usecase/budget_test.go`

**Interfaces:**
- Consumes: `NewConverter`, `(*Converter).Convert` (Task 3), `fxDouble` and `errProviderDown` (Tasks 2-3).
- Produces: nothing new for later tasks.

- [ ] **Step 1: Let the fixtures accept or expose the FX double**

In `transaction_test.go`, rename `newTransactionFixture`'s body into a new
function that takes the double, and keep the old name as a one-line call:

```go
func newTransactionFixture(t *testing.T, extraAccounts map[string]fakeAccountRecord) (*usecase.TransactionService, *fakeTransactionRepo) {
	t.Helper()
	return newTransactionFixtureWithFX(t, extraAccounts, newFXDouble())
}

// newTransactionFixtureWithFX is newTransactionFixture with the FX double
// chosen by the test, the same shape as bill_test.go's newBillServiceWithFX.
func newTransactionFixtureWithFX(t *testing.T, extraAccounts map[string]fakeAccountRecord, fx usecase.FXRateProvider) (*usecase.TransactionService, *fakeTransactionRepo) {
	t.Helper()
	// ... the previous body of newTransactionFixture, unchanged, except
	// `FX: newFXDouble(),` becomes `FX: fx,`
}
```

(Move the body verbatim; the only edit inside it is the `FX:` line.)

In `budget_test.go`:
- add `fx *fxDouble` as the last field of `budgetFixture`;
- in `newBudgetFixture`, create `fx := newFXDouble()` before
  `usecase.NewBudgetService(...)`;
- pass `FX: fx,` to `NewBudgetService`;
- add `fx: fx,` to the returned `&budgetFixture{...}`.

- [ ] **Step 2: Write the two failing tests**

Add to `api/internal/usecase/monthsummary_test.go`:

```go
// A failed lookup must fail the month summary rather than report the IDR
// expense as "no rate" and a Spent figure that leaves it out.
func TestMonthSummaryFailsWhenTheRateLookupItselfFails(t *testing.T) {
	svc, _ := newTransactionFixtureWithFX(t, map[string]fakeAccountRecord{
		"idr-card": {householdID: "house-1", currency: "IDR"},
	}, newFXDouble().failWith(errProviderDown))
	ctx := context.Background()
	july := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	mustCreate(t, svc, usecase.NewTransaction{
		HouseholdID: "house-1", Kind: "expense", OccurredOn: july.AddDate(0, 0, 8),
		Description: "Warung", CategoryID: "cat-groceries",
		FromAccountID: "idr-card", AmountMinor: 12_410,
	})

	_, err := svc.MonthSummary(ctx, "house-1", july)
	if !errors.Is(err, errProviderDown) {
		t.Fatalf("MonthSummary error = %v, want the provider's error", err)
	}
}
```

Add to `api/internal/usecase/budget_test.go`:

```go
// The budget's Spent reuses the month-summary rule, including this half of
// it: only ErrNoRate may leave an expense out; a failed lookup fails Month.
func TestBudgetMonthFailsWhenTheRateLookupItselfFails(t *testing.T) {
	f := newBudgetFixture(t)
	f.fx.failWith(errProviderDown)
	ctx := context.Background()
	july := julyMonth()

	f.addExpense("tx-idr", "cat-groceries", "", july.AddDate(0, 0, 3), 12_410, "IDR")

	_, err := f.svc.Month(ctx, "house-1", july, july.AddDate(0, 0, 17))
	if !errors.Is(err, errProviderDown) {
		t.Fatalf("Month error = %v, want the provider's error", err)
	}
}
```

Add `"errors"` to either file's imports if it is missing.

If `mustCreate` itself fails with `errProviderDown`, because
`TransactionService.Create` consults FX somewhere in its validation, insert
the transaction straight into the repo double instead, the way
`budgetFixture.addExpense` does. The test is about `MonthSummary`, not
`Create`.

- [ ] **Step 3: Run to verify both fail**

Run: `cd api && go test ./internal/usecase/ -run 'TestMonthSummaryFailsWhenTheRateLookupItselfFails|TestBudgetMonthFailsWhenTheRateLookupItselfFails' -v`
Expected: both FAIL, with `error = <nil>, want the provider's error`.

- [ ] **Step 4: Move month summary onto `Converter`**

In `monthsummary.go`, just before the `for _, view := range views` loop, add
`conv := NewConverter(s.d.FX, primary)`. Replace the conversion in the loop
with:

```go
		inPrimary, err := conv.Convert(ctx, view.Transaction.Amount)
		if errors.Is(err, domain.ErrNoRate) {
			summary.ExcludedNoRate = append(summary.ExcludedNoRate, ExcludedTransaction{
				TransactionID: view.Transaction.ID,
				Currency:      view.Transaction.Amount.Currency,
			})
			continue
		}
		if err != nil {
			return MonthSummary{}, err
		}
```

Delete `TransactionService.convert` and its doc comment. Add the `"errors"`
import if missing.

- [ ] **Step 5: Move the budget onto `Converter`**

In `budget.go`, `tallySpend`: add `conv := NewConverter(s.d.FX, primary)` as
the first line of the function body. Replace the conversion inside the loop
with:

```go
		inPrimary, err := conv.Convert(ctx, t.Amount)
		if errors.Is(err, domain.ErrNoRate) {
			tally.excluded = append(tally.excluded, ExcludedTransaction{
				TransactionID: t.ID,
				Currency:      t.Amount.Currency,
			})
			continue
		}
		if err != nil {
			return spendTally{}, err
		}
```

Delete `BudgetService.convert` and its doc comment. Run
`grep -n "s.convert(" budget.go`; if it finds any other call, replace it with a
`Converter` built once at the top of that method, using the same
`ErrNoRate`-then-`err != nil` shape. Add the `"errors"` import if missing.

- [ ] **Step 6: Run the suite**

Run: `cd api && go test ./internal/usecase/`
Expected: PASS, including `TestATransactionWithNoRateIsExcludedAndNamed` and
`TestBudgetMonthSpentReusesTheMonthSummaryRule`, which are unchanged.

- [ ] **Step 7: Mutation-check the new rule** (the one the Definition of done requires)

In `budget.go` `tallySpend`, change `if errors.Is(err, domain.ErrNoRate) {` to
`if err != nil {`. Run
`cd api && go test ./internal/usecase/ -run TestBudgetMonthFailsWhenTheRateLookupItselfFails`.
Expected: FAIL. Revert, re-run. Expected: PASS. Note the result for the
LEARNING.md entry in Task 7.

- [ ] **Step 8: Commit**

```bash
git add api/internal/usecase/monthsummary.go api/internal/usecase/budget.go \
        api/internal/usecase/transaction_test.go api/internal/usecase/monthsummary_test.go api/internal/usecase/budget_test.go
git commit -m "refactor(usecase): month summary and budget convert through Converter

Both exclude an expense only on ErrNoRate and fail the request on any other
conversion error. Two private convert copies deleted.

Claude-Session: https://claude.ai/code/session_01K72Ud8izwe2cu8pVv7mLzU"
```

---

### Task 5: Goals on `Converter`

**Files:**
- Modify: `api/internal/usecase/goal.go` (`List` loop ~lines 187-192; `monthlyInPrimary` ~lines 286-309; delete `convert` ~lines 492-509)
- Modify: `api/internal/usecase/goal_test.go` (add an `fx *fxDouble` field to `goalFixture`)

**Interfaces:**
- Consumes: `NewConverter`, `Convert`, `fxDouble`, `errProviderDown`.
- Produces: `monthlyInPrimary` gains a `*Converter` parameter and an `error` result (unexported, used only by `List`).

- [ ] **Step 1: Expose the double from the fixture**

In `goal_test.go`:
- add `fx *fxDouble` to `goalFixture`;
- in `newGoalFixture`, create `fx := newFXDouble()`;
- pass `FX: fx,`;
- return `&goalFixture{svc: svc, goals: goals, households: households, fx: fx}`.

- [ ] **Step 2: Write the failing test**

Add to `goal_test.go`:

```go
// A failed lookup must fail the goals list rather than count the IDR goal in
// ExcludedNoRate and leave it out of both monthly totals.
func TestGoalListFailsWhenTheRateLookupItselfFails(t *testing.T) {
	f := newGoalFixture(t)
	f.fx.failWith(errProviderDown)
	today := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

	f.seedGoal(domain.Goal{
		Name: "IDR goal", Target: domain.Money{Amount: 500_000_000, Currency: "IDR"},
		PlannedMonthly: domain.Money{Amount: 12_410_000, Currency: "IDR"},
	})

	_, err := f.svc.List(context.Background(), "house-1", false, today)
	if !errors.Is(err, errProviderDown) {
		t.Fatalf("List error = %v, want the provider's error", err)
	}
}
```

Add `"errors"` to the imports if it is missing.

- [ ] **Step 3: Run to verify it fails**

Run: `cd api && go test ./internal/usecase/ -run TestGoalListFailsWhenTheRateLookupItselfFails -v`
Expected: FAIL, with `List error = <nil>, want the provider's error`.

- [ ] **Step 4: Move goals onto `Converter`**

In `goal.go`, replace `monthlyInPrimary` with the version below. Keep its
existing "Convert-then-add, per goal" paragraph, which still holds, and add the
last paragraph shown:

```go
// monthlyInPrimary converts one goal's planned monthly figure and, if the goal
// received anything this month, its actual figure into primary. excluded means
// the goal's currency has no rate, and neither figure may be added to a total.
//
// Convert-then-add, per goal: the planned and actual figures share the goal's
// one currency, so either both convert or neither does. Splitting these into
// two independently-guarded conversions would let the two totals disagree
// about which goals had a rate, which List's "excluded from BOTH totals" rule
// forbids.
//
// Only domain.ErrNoRate means excluded. Any other conversion error is
// returned, and List fails with it.
func (s *GoalService) monthlyInPrimary(ctx context.Context, conv *Converter, g domain.Goal, actualByGoal map[string]int64) (planned, actual domain.Money, hasActual, excluded bool, err error) {
	planned, err = conv.Convert(ctx, g.PlannedMonthly)
	if errors.Is(err, domain.ErrNoRate) {
		return domain.Money{}, domain.Money{}, false, true, nil
	}
	if err != nil {
		return domain.Money{}, domain.Money{}, false, false, err
	}
	amount, ok := actualByGoal[g.ID]
	if !ok {
		return planned, domain.Money{}, false, false, nil
	}
	actual, err = conv.Convert(ctx, domain.Money{Amount: amount, Currency: g.Target.Currency})
	if errors.Is(err, domain.ErrNoRate) {
		return domain.Money{}, domain.Money{}, false, true, nil
	}
	if err != nil {
		return domain.Money{}, domain.Money{}, false, false, err
	}
	return planned, actual, true, false, nil
}
```

In `List`, add `conv := NewConverter(s.d.FX, primary)` just before
`views := make(...)`, and replace the call site with:

```go
		plannedInPrimary, actualInPrimary, hasActual, excluded, err := s.monthlyInPrimary(ctx, conv, g, actualByGoal)
		if err != nil {
			return GoalsView{}, err
		}
		if excluded {
			excludedNoRate++
			continue
		}
```

If `err` is already declared in `List`'s scope, `:=` still compiles here,
because `plannedInPrimary` and the others are new. Delete
`GoalService.convert` and its doc comment. Run `grep -n "s.convert(" goal.go`;
expected: no output. Add the `"errors"` import if missing.

- [ ] **Step 5: Run the suite**

Run: `cd api && go test ./internal/usecase/`
Expected: PASS, including `TestGoalListPlannedTotalConvertsThenAdds`, unchanged.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/goal.go api/internal/usecase/goal_test.go
git commit -m "refactor(usecase): goals convert through Converter

A goal is excluded only on ErrNoRate; any other conversion error fails List.

Claude-Session: https://claude.ai/code/session_01K72Ud8izwe2cu8pVv7mLzU"
```

---

### Task 6: Bills on `Converter` (swap only)

**Files:**
- Modify: `api/internal/usecase/bill.go`:
  - `List`: the probe at ~line 270, the calls to `addSubscriptionAnnual`, `countExcludedPayments` and `sumConvertible`
  - `addSubscriptionAnnual` ~line 377
  - `countExcludedPayments` ~line 400
  - `sumConvertible` ~line 419
  - delete `convert` ~lines 915-934
- Test: `api/internal/usecase/bill_test.go`

**Interfaces:**
- Consumes: `NewConverter`, `Convert`, `fxDouble`, `errProviderDown`.
- Produces: `addSubscriptionAnnual`, `countExcludedPayments` and
  `sumConvertible` take `conv *Converter` in place of the `primary string` and
  `s` receiver's FX. `countExcludedPayments` now returns `(int, error)`. All
  three are unexported and called only from `List`.

**Keep the structure.** The probe-then-convert shape stays exactly as it is
(item 3 of the ranked list restructures it). Each `s.convert(ctx, x, primary)`
becomes `conv.Convert(ctx, x)`, and each place that treated any error as "no
rate" now treats only `ErrNoRate` that way and returns every other error.

- [ ] **Step 1: Write the failing test**

Add to `bill_test.go`:

```go
// A failed lookup must fail the bills list rather than count the IDR bill as
// "no rate" and leave it out of DueThisMonth.
func TestBillsSummaryFailsWhenTheRateLookupItselfFails(t *testing.T) {
	svc := newBillServiceWithFX(t, newFXDouble().failWith(errProviderDown),
		bill("SP utilities", "2026-08-08", 14230),              // SGD
		billOn("Arisan", "IDR", "2026-08-15", 50_000_000),      // needs a rate
	)

	_, err := svc.List(context.Background(), "h1", false, day("2026-08-09"))
	if !errors.Is(err, errProviderDown) {
		t.Fatalf("List error = %v, want the provider's error", err)
	}
}
```

Add `"errors"` to the imports if it is missing.

- [ ] **Step 2: Run to verify it fails**

Run: `cd api && go test ./internal/usecase/ -run TestBillsSummaryFailsWhenTheRateLookupItselfFails -v`
Expected: FAIL, with `List error = <nil>, want the provider's error`.

- [ ] **Step 3: Swap each call site**

In `List`, add `conv := NewConverter(s.deps.FX, primary)` immediately after
`primary` is known, above the per-bill loop.

The due-this-month probe (~line 270) becomes:

```go
		if b.NextDue != nil && dueInMonthOf(*b.NextDue, today) {
			_, convErr := conv.Convert(ctx, b.Amount)
			if errors.Is(convErr, domain.ErrNoRate) {
				excludedThisBill = true
			} else if convErr != nil {
				return BillsView{}, convErr
			}
		}
```

`addSubscriptionAnnual`: change the signature to
`func (s *BillService) addSubscriptionAnnual(ctx context.Context, conv *Converter, total domain.Money, b domain.Bill) (sum domain.Money, noRate bool, err error)`.
Its conversion becomes:

```go
	converted, convErr := conv.Convert(ctx, domain.Money{Amount: annual, Currency: b.Amount.Currency})
	if errors.Is(convErr, domain.ErrNoRate) {
		return total, true, nil
	}
	if convErr != nil {
		return domain.Money{}, false, convErr
	}
```

Its call in `List` becomes
`subscriptionsAnnual, noRate, err = s.addSubscriptionAnnual(ctx, conv, subscriptionsAnnual, b)`.
The existing `if err != nil { return BillsView{}, err }` after it already
handles the new error.

`countExcludedPayments`: change the signature to
`func (s *BillService) countExcludedPayments(ctx context.Context, conv *Converter, payments []BillPaymentRecord, excludedBillIDs map[string]bool) (int, error)`.
The body becomes:

```go
	count := 0
	for _, p := range payments {
		if excludedBillIDs[p.Payment.BillID] {
			continue
		}
		_, convErr := conv.Convert(ctx, p.Payment.Amount)
		if errors.Is(convErr, domain.ErrNoRate) {
			count++
			excludedBillIDs[p.Payment.BillID] = true
			continue
		}
		if convErr != nil {
			return 0, convErr
		}
	}
	return count, nil
```

Update its doc comment's last sentence to: "Only domain.ErrNoRate counts; any
other conversion error is returned." Its call in `List` becomes:

```go
	excludedPayments, err := s.countExcludedPayments(ctx, conv, paymentRecords, excludedBillIDs)
	if err != nil {
		return BillsView{}, err
	}
	excludedNoRate += excludedPayments
```

`sumConvertible`: change the signature to
`func (s *BillService) sumConvertible(ctx context.Context, conv *Converter, byCurrency map[string]int64, zero domain.Money) (domain.Money, error)`.
Its conversion becomes:

```go
		converted, convErr := conv.Convert(ctx, domain.Money{Amount: amount, Currency: currency})
		if errors.Is(convErr, domain.ErrNoRate) {
			continue
		}
		if convErr != nil {
			return domain.Money{}, convErr
		}
```

Both calls in `List` gain `conv` as the second argument.

Delete `BillService.convert` and its doc comment. Run
`grep -n "s.convert(" bill.go`; expected: no output. Add the `"errors"` import
if missing.

- [ ] **Step 4: Run the suite**

Run: `cd api && go test ./internal/usecase/`
Expected: PASS, including every existing `TestSummary...NoRate...` bill test,
unchanged.

- [ ] **Step 5: Confirm no copy is left**

Run: `cd api && grep -rn "func (s \*[A-Za-z]*) convert(\|type converter\b" internal/usecase/`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/bill.go api/internal/usecase/bill_test.go
git commit -m "refactor(usecase): bills convert through Converter

Swap only: List's probe-then-convert shape is unchanged (item 3 owns that).
Each site excludes on ErrNoRate and fails the request on any other error.
The last private convert copy is gone.

Claude-Session: https://claude.ai/code/session_01K72Ud8izwe2cu8pVv7mLzU"
```

---

### Task 7: Docs, full gate, and a real browser walk

**Files:**
- Modify: `docs/SYSTEM_DESIGN.md` (the `FXRateProvider` row ~line 906; the sequence diagrams naming `FXRateProvider` ~lines 2228-2237 and 2387-2397)
- Modify: `docs/LEARNING.md`
- Modify: `docs/reviews/2026-09-26-architecture-review.md`

- [ ] **Step 1: Keep SYSTEM_DESIGN.md true**

Use the `maintaining-system-design` skill. At minimum:
- **The port row (~906):** say it returns `domain.Rate`, that an unsupported
  pair is `domain.ErrNoRate`, and that callers go through a per-request
  `Converter` (`usecase/converter.go`). Callers are net worth, month summary,
  budget, goals and bills.
- **The two sequence diagrams:** insert the `Converter` between the usecase
  and `FX` as a participant, and add the rule "exclude only on ErrNoRate,
  otherwise error" to the prose beneath each.

- [ ] **Step 2: Add what this taught to LEARNING.md**

Add an entry under the pattern it fits. It is evidence for **5. Silent partial
success is worse than loud failure**, so add it there rather than starting a
section. Use this text, adjusting the mutation-check result to what Task 4
Step 7 actually showed:

```markdown
- **Every conversion failure read as "no rate", 2026-09-26.** Five services
  each carried a private `convert`, and every caller treated *any* error from
  it as "this currency has no rate": the item was left out of the total and
  listed under `ExcludedNoRate`. A provider outage, a cancelled request or an
  overflow therefore rendered as a smaller total with a false reason beside
  it. Nothing distinguished them, because the static provider returned a
  plain `fmt.Errorf`. Fixed by `domain.ErrNoRate`, a port contract that
  requires it, and one `usecase.Converter`. Callers exclude only on
  `ErrNoRate`, and fail otherwise. Pinned per caller by a
  `…FailsWhenTheRateLookupItselfFails` test; the budget one was
  mutation-checked (`errors.Is(err, domain.ErrNoRate)` → `err != nil` fails
  it). Found by an architecture review, not by a user. The same review found
  `Rate.Apply` would divide by zero on a zero-denominator rate from a
  provider; it now returns `ErrInvalidRate`. **What would have caught it
  sooner:** a port whose failure modes are named in its contract. "Returns an
  error" is not a contract when callers must treat two kinds of error
  differently.
```

- [ ] **Step 3: Update the ranked list**

In `docs/reviews/2026-09-26-architecture-review.md`, set item 2's status to
"🔵 2b ✅, 2a ⬜". Add a Log line:
`- <date> — 2b merged (<PR link>). 2a (Month type) still waits on the "whose timezone?" decision.`

- [ ] **Step 4: The finish line**

Run through the "Before you call something done" checklist at the end of
`docs/LEARNING.md`. `docs/FEATURE_TRACKER.md` does not change, because no
feature was added or changed; say so in the PR description.

- [ ] **Step 5: The full gate**

Run from the repository root:

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make lint && make test
```

Expected: both green. `make lint-arch` must pass, meaning `domain` still
imports only the standard library. If `make lint-dead` flags something this
branch made unreachable, delete it.

- [ ] **Step 6: Walk it in a real browser** (CLAUDE.md requires this before "done")

Check `lsof -i :5173` first, because two Docker engines can host the stack.
Then `make dev` and `make seed`, and sign in with the printed details at
http://localhost:5173. Using browser automation:
1. **Overview / net worth:** note the net worth figure and any "excluded" note.
2. **Accounts:** add a cash account in **EUR** (no rate) with a balance. Net
   worth must be **unchanged**, and the screen must name the EUR account as
   excluded.
3. **Transactions:** add an expense from an **IDR** account. The month's
   spent figure must rise by the converted SGD amount (Rp 124,100 = S$10.00).
4. **Budget:** the same month's spent must match step 3.
5. **Goals:** add an IDR goal with a monthly plan. The planned total must rise
   by its converted amount.
6. **Bills:** add an IDR bill due this month. Due-this-month must rise by its
   converted amount. A EUR bill must be counted as excluded, not summed.

Record what you clicked and the before/after figures in the PR description.
If any figure differs from what it was before this branch for the same data,
stop and investigate. This refactor must not move a number on a normal day.

- [ ] **Step 7: Commit and open the PR**

```bash
git add docs/SYSTEM_DESIGN.md docs/LEARNING.md   # NOT the ranked list -- see Task 0
git commit -m "docs: the Converter, ErrNoRate, and what the no-rate conflation taught

Claude-Session: https://claude.ai/code/session_01K72Ud8izwe2cu8pVv7mLzU"
git push -u origin refactor/money-conversion-in-domain
gh pr create --title "Money conversion in domain: one Converter, Rate beside Money, ErrNoRate" \
  --body "Item 2b of docs/reviews/2026-09-26-architecture-review.md. <summary, test evidence, mutation check, browser walk figures>

https://claude.ai/code/session_01K72Ud8izwe2cu8pVv7mLzU"
```

- [ ] **Step 8: Review the diff**

Run `/code-review` on the branch, and address the findings before merging.
