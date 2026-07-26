# Hearth Identity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A household of members who can be invited, sign in with a password or a magic link, be locked out after three wrong passwords, and see only the spaces their capabilities allow.

**Architecture:** Rules live in `internal/domain` as pure functions and value objects — capability constraints, the last-owner rule, and the lockout window are all unit-testable without a database or a clock. `internal/usecase` orchestrates them through ports; `internal/adapter` implements those ports with Postgres, argon2id, and SMTP. The React app holds no authority: it mirrors server state fetched from `GET /api/v1/auth/me` and every rule it appears to enforce is re-enforced server-side.

**Tech Stack:** Everything from the skeleton plan, plus `golang.org/x/crypto/argon2`, `sqlc`, `github.com/wneessen/go-mail`, and `@tanstack/react-router` route guards.

**Spec:** `docs/superpowers/specs/2026-07-26-hearth-foundation-design.md`

**Prerequisite:** `docs/superpowers/plans/2026-07-26-hearth-skeleton.md` complete and green. Task numbering continues from it.

## Global Constraints

- Every rule in `internal/domain` is a pure function or a method on a value. No domain code imports `context`, `database/sql`, `net/http`, or reads a clock directly.
- Time enters through the `Clock` port. Randomness enters through the `TokenGenerator` port. Tests never sleep.
- Only hashes are stored — passwords, session tokens, magic-link tokens, invite tokens. A raw token exists in memory and in an email, never in a row.
- Every repository method takes `householdID` as its first argument after `ctx`, except the ones that resolve a user by email before a household is known.
- Session cookie: name `hearth_session`, `HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` when `APP_ENV != development`, 30-day expiry extended on use.
- CSRF cookie: name `csrf_token`, **not** `HttpOnly`, same lifetime, echoed in `X-CSRF-Token`.
- Lockout: 3 failed password attempts inside a 15-minute window locks the household's **password** sign-in for 15 minutes. Magic link is never gated by the lock.
- Magic-link rate limit: 3 requests per email address per hour.
- Capability strings are exactly `calendar`, `chores`, `money`, `marriage`. Role strings are exactly `owner`, `limited`.
- All user-visible copy is taken verbatim from `design/Household Dashboard.dc.html`. The wrong-password message is: `That password doesn't match. Two tries left before we lock the household for 15 minutes.` — with the count substituted.
- **Every 2xx response except `204` carries a JSON body.** The frontend's `apiFetch` throws `ApiError` with code `INVALID_RESPONSE` on an ok response whose body is absent or unparseable, so an empty `200` or `202` is a client-visible failure. `POST /auth/magic-link` answers `202` with `{"status":"accepted"}`; `POST /auth/sign-out` and `DELETE /household/members/:id` answer `204` with no body, which `apiFetch` handles explicitly.
- `make lint-arch` now rejects **any** third-party import in `internal/domain` and `internal/usecase`, including imports that appear only in `_test.go` files. Those packages and their tests may use the standard library and in-module packages only. Assertion libraries do not belong there; the tests in this plan use stdlib `testing` throughout.
- Every task ends with a commit.

## File Structure

| Path | Responsibility |
|---|---|
| `api/internal/domain/errors.go` | The typed error set every layer above maps from |
| `api/internal/domain/money.go` | `Money` value object |
| `api/internal/domain/identity.go` | `User`, `Household`, `Membership`, `Role`, `Capability` and their rules |
| `api/internal/domain/lockout.go` | The lockout window calculation, pure |
| `api/internal/domain/space.go` | `Space`, visibility, and which spaces a membership may see |
| `api/internal/usecase/ports.go` | Every port interface, in one file so the contract is readable at a glance |
| `api/internal/usecase/auth.go` | Sign-in, sign-out, magic link |
| `api/internal/usecase/invite.go` | Create, read, accept |
| `api/internal/usecase/member.go` | List, update capabilities, remove |
| `api/internal/usecase/household.go` | Settings, notification preferences, spaces |
| `api/internal/adapter/postgres/queries/*.sql` | sqlc source queries |
| `api/internal/adapter/postgres/*_repo.go` | Port implementations wrapping generated code |
| `api/internal/adapter/crypto/argon2.go` | `PasswordHasher`, `TokenGenerator` |
| `api/internal/adapter/mail/smtp.go` | `Mailer` |
| `api/internal/adapter/http/middleware_session.go` | Session resolution and household scoping |
| `api/internal/adapter/http/middleware_csrf.go` | Double-submit verification |
| `api/internal/adapter/http/auth_handlers.go` etc. | One handler file per resource group |
| `api/cmd/adminctl/main.go` | `seed`, `reset-password`, `unlock-household`, `create-invite` |
| `web/src/features/auth/` | Sign-in state machine, invite acceptance |
| `web/src/features/shell/` | Sidebar, header, `RequireAuth`, `RequireCapability` |
| `web/src/components/` | Generic primitives only: `Modal`, `Button`, `Field` |
| `web/src/features/settings/` | Members, spaces, currency, notifications |

---

### Task 6: Domain — money, roles, capabilities and their rules

**Files:**
- Create: `api/internal/domain/errors.go`, `api/internal/domain/money.go`, `api/internal/domain/identity.go`
- Test: `api/internal/domain/money_test.go`, `api/internal/domain/identity_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `domain.Money{Amount int64; Currency string}`, `domain.NewMoney(amount int64, currency string) (Money, error)`, `(Money).Add(Money) (Money, error)`, `(Money).String() string`
  - `domain.Role` with `domain.RoleOwner`, `domain.RoleLimited`
  - `domain.Capability` with `domain.CapCalendar`, `domain.CapChores`, `domain.CapMoney`, `domain.CapMarriage`
  - `domain.Capabilities []Capability` with `.Has(Capability) bool`, `.Strings() []string`, `domain.ParseCapabilities([]string) (Capabilities, error)`
  - `domain.Membership{ID, HouseholdID, UserID string; Role Role; Capabilities Capabilities}`
  - `domain.NewMembership(id, householdID, userID string, role Role, caps Capabilities) (Membership, error)`
  - `domain.ValidateMembershipChange(all []Membership, targetID string, newRole Role, newCaps Capabilities) error`
  - `domain.ValidateMembershipRemoval(all []Membership, targetID string) error`
  - Errors: `ErrInvalidCredentials`, `ErrHouseholdLocked`, `ErrLastOwner`, `ErrLimitedCannotHoldMarriage`, `ErrOwnerMustHoldAllCapabilities`, `ErrUnknownCapability`, `ErrUnknownRole`, `ErrCurrencyMismatch`, `ErrAmountOverflow`, `ErrInvalidMoney`, `ErrNotFound`, `ErrForbidden`, `ErrInviteExpired`, `ErrInviteAlreadyAccepted`, `ErrTokenExpired`, `ErrRateLimited`

**Two contract rules established here that every later task depends on.** An owner must hold all four capabilities — the design shows both parents with full access, and leaving it unenforced made space visibility ambiguous. And `ValidateMembershipChange` / `ValidateMembershipRemoval` return `ErrNotFound` when the target membership is absent, rather than silently approving; they consult the last-owner rule only when the target is currently an owner and the change would stop it being one, so a capability edit on a limited member never trips it.

- [ ] **Step 1: Write the failing money test**

Create `api/internal/domain/money_test.go`:

```go
package domain_test

import (
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestNewMoneyRejectsAnUnknownCurrency(t *testing.T) {
	if _, err := domain.NewMoney(100, "sgd"); err == nil {
		t.Fatal("expected lowercase currency to be rejected")
	}
	if _, err := domain.NewMoney(100, "SG"); err == nil {
		t.Fatal("expected a two-letter currency to be rejected")
	}
}

func TestAddRefusesToMixCurrencies(t *testing.T) {
	sgd, _ := domain.NewMoney(1000, "SGD")
	idr, _ := domain.NewMoney(1000, "IDR")

	if _, err := sgd.Add(idr); !errors.Is(err, domain.ErrCurrencyMismatch) {
		t.Fatalf("err = %v, want ErrCurrencyMismatch", err)
	}
}

func TestAddSumsMinorUnits(t *testing.T) {
	a, _ := domain.NewMoney(824055, "SGD")
	b, _ := domain.NewMoney(100, "SGD")

	sum, err := a.Add(b)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if sum.Amount != 824155 {
		t.Fatalf("Amount = %d, want 824155", sum.Amount)
	}
}

func TestStringFormatsMinorUnits(t *testing.T) {
	m, _ := domain.NewMoney(824055, "SGD")
	if got := m.String(); got != "SGD 8240.55" {
		t.Fatalf("String() = %q, want %q", got, "SGD 8240.55")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd api && go test ./internal/domain/...`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement errors and money**

Create `api/internal/domain/errors.go`:

```go
// Package domain holds the business rules. Nothing here imports another
// internal package, a database driver, an HTTP library, or a clock.
package domain

import "errors"

var (
	ErrInvalidCredentials        = errors.New("invalid credentials")
	ErrHouseholdLocked           = errors.New("household is locked")
	ErrLastOwner                 = errors.New("a household must keep at least one owner")
	ErrLimitedCannotHoldMarriage = errors.New("a limited member cannot hold the marriage capability")
	ErrUnknownCapability         = errors.New("unknown capability")
	ErrUnknownRole               = errors.New("unknown role")
	ErrCurrencyMismatch          = errors.New("cannot combine different currencies")
	ErrNotFound                  = errors.New("not found")
	ErrForbidden                 = errors.New("forbidden")
	ErrInviteExpired             = errors.New("invite has expired")
	ErrInviteAlreadyAccepted     = errors.New("invite has already been accepted")
	ErrTokenExpired              = errors.New("token has expired or been used")
	ErrRateLimited               = errors.New("too many requests")
)
```

Create `api/internal/domain/money.go`:

```go
package domain

import "fmt"

// Money is an exact amount in an ISO 4217 currency, held in minor units.
// Floating point never appears in a monetary path.
type Money struct {
	Amount   int64
	Currency string
}

func NewMoney(amount int64, currency string) (Money, error) {
	if len(currency) != 3 {
		return Money{}, fmt.Errorf("currency must be three letters, got %q", currency)
	}
	for _, r := range currency {
		if r < 'A' || r > 'Z' {
			return Money{}, fmt.Errorf("currency must be uppercase, got %q", currency)
		}
	}
	return Money{Amount: amount, Currency: currency}, nil
}

func (m Money) Add(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("%w: %s and %s", ErrCurrencyMismatch, m.Currency, other.Currency)
	}
	return Money{Amount: m.Amount + other.Amount, Currency: m.Currency}, nil
}

func (m Money) String() string {
	sign := ""
	amount := m.Amount
	if amount < 0 {
		sign, amount = "-", -amount
	}
	return fmt.Sprintf("%s%s %s%d.%02d", sign, m.Currency, "", amount/100, amount%100)
}
```

- [ ] **Step 4: Run the money tests**

Run: `cd api && go test ./internal/domain/... -run Money`
Expected: PASS.

- [ ] **Step 5: Write the failing identity test**

Create `api/internal/domain/identity_test.go`:

```go
package domain_test

import (
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func owner(id string) domain.Membership {
	m, _ := domain.NewMembership(id, "h1", "u-"+id, domain.RoleOwner,
		domain.Capabilities{domain.CapCalendar, domain.CapChores, domain.CapMoney, domain.CapMarriage})
	return m
}

func kid(id string, caps domain.Capabilities) domain.Membership {
	m, _ := domain.NewMembership(id, "h1", "u-"+id, domain.RoleLimited, caps)
	return m
}

func TestLimitedMemberCannotHoldMarriage(t *testing.T) {
	_, err := domain.NewMembership("m1", "h1", "u1", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar, domain.CapMarriage})

	if !errors.Is(err, domain.ErrLimitedCannotHoldMarriage) {
		t.Fatalf("err = %v, want ErrLimitedCannotHoldMarriage", err)
	}
}

func TestLimitedMemberMayHoldCalendarAndChores(t *testing.T) {
	if _, err := domain.NewMembership("m1", "h1", "u1", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar, domain.CapChores}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemovingTheLastOwnerIsRejected(t *testing.T) {
	all := []domain.Membership{owner("m1"), kid("m2", domain.Capabilities{domain.CapCalendar})}

	if err := domain.ValidateMembershipRemoval(all, "m1"); !errors.Is(err, domain.ErrLastOwner) {
		t.Fatalf("err = %v, want ErrLastOwner", err)
	}
}

func TestRemovingAnOwnerWhenAnotherRemainsIsAllowed(t *testing.T) {
	all := []domain.Membership{owner("m1"), owner("m2")}

	if err := domain.ValidateMembershipRemoval(all, "m1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDemotingTheLastOwnerIsRejected(t *testing.T) {
	all := []domain.Membership{owner("m1"), kid("m2", domain.Capabilities{domain.CapCalendar})}

	err := domain.ValidateMembershipChange(all, "m1", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar})

	if !errors.Is(err, domain.ErrLastOwner) {
		t.Fatalf("err = %v, want ErrLastOwner", err)
	}
}

func TestChangingAMemberToAnInvalidCapabilitySetIsRejected(t *testing.T) {
	all := []domain.Membership{owner("m1"), kid("m2", domain.Capabilities{domain.CapCalendar})}

	err := domain.ValidateMembershipChange(all, "m2", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar, domain.CapMarriage})

	if !errors.Is(err, domain.ErrLimitedCannotHoldMarriage) {
		t.Fatalf("err = %v, want ErrLimitedCannotHoldMarriage", err)
	}
}

func TestParseCapabilitiesRejectsUnknownValues(t *testing.T) {
	if _, err := domain.ParseCapabilities([]string{"calendar", "spaceships"}); !errors.Is(err, domain.ErrUnknownCapability) {
		t.Fatalf("err = %v, want ErrUnknownCapability", err)
	}
}

func TestParseCapabilitiesDeduplicates(t *testing.T) {
	caps, err := domain.ParseCapabilities([]string{"calendar", "calendar", "money"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(caps) != 2 {
		t.Fatalf("len = %d, want 2", len(caps))
	}
}

func TestHasReportsMembership(t *testing.T) {
	caps := domain.Capabilities{domain.CapCalendar, domain.CapChores}
	if !caps.Has(domain.CapChores) {
		t.Fatal("expected chores")
	}
	if caps.Has(domain.CapMarriage) {
		t.Fatal("did not expect marriage")
	}
}
```

- [ ] **Step 6: Run it and watch it fail**

Run: `cd api && go test ./internal/domain/...`
Expected: FAIL — undefined identifiers.

- [ ] **Step 7: Implement identity**

Create `api/internal/domain/identity.go`:

```go
package domain

import "fmt"

type Role string

const (
	RoleOwner   Role = "owner"
	RoleLimited Role = "limited"
)

func ParseRole(s string) (Role, error) {
	switch Role(s) {
	case RoleOwner:
		return RoleOwner, nil
	case RoleLimited:
		return RoleLimited, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownRole, s)
	}
}

type Capability string

const (
	CapCalendar Capability = "calendar"
	CapChores   Capability = "chores"
	CapMoney    Capability = "money"
	CapMarriage Capability = "marriage"
)

type Capabilities []Capability

func ParseCapabilities(values []string) (Capabilities, error) {
	seen := make(map[Capability]bool, len(values))
	out := make(Capabilities, 0, len(values))
	for _, v := range values {
		c := Capability(v)
		switch c {
		case CapCalendar, CapChores, CapMoney, CapMarriage:
		default:
			return nil, fmt.Errorf("%w: %q", ErrUnknownCapability, v)
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out, nil
}

func (c Capabilities) Has(want Capability) bool {
	for _, got := range c {
		if got == want {
			return true
		}
	}
	return false
}

func (c Capabilities) Strings() []string {
	out := make([]string, len(c))
	for i, cap := range c {
		out[i] = string(cap)
	}
	return out
}

// AllCapabilities is what an owner holds.
func AllCapabilities() Capabilities {
	return Capabilities{CapCalendar, CapChores, CapMoney, CapMarriage}
}

type User struct {
	ID            string
	Email         string // empty for members without credentials
	DisplayName   string
	AvatarInitial string
}

type Household struct {
	ID                    string
	Name                  string
	FamilyName            string
	PrimaryCurrency       string
	ShowSecondaryCurrency bool
	SecondaryCurrency     string
	FXRateMode            string // "auto" or "manual"; inert until a live provider exists
}

type Membership struct {
	ID           string
	HouseholdID  string
	UserID       string
	Role         Role
	Capabilities Capabilities
}

// NewMembership enforces the capability rules at construction, so an invalid
// Membership value cannot exist anywhere in the system.
func NewMembership(id, householdID, userID string, role Role, caps Capabilities) (Membership, error) {
	if err := validateCapabilitiesForRole(role, caps); err != nil {
		return Membership{}, err
	}
	return Membership{
		ID: id, HouseholdID: householdID, UserID: userID, Role: role, Capabilities: caps,
	}, nil
}

func validateCapabilitiesForRole(role Role, caps Capabilities) error {
	if role == RoleLimited && caps.Has(CapMarriage) {
		return ErrLimitedCannotHoldMarriage
	}
	return nil
}

// ValidateMembershipChange checks a proposed role and capability change against
// the whole household, because the last-owner rule is not a property of one
// membership in isolation.
func ValidateMembershipChange(all []Membership, targetID string, newRole Role, newCaps Capabilities) error {
	if err := validateCapabilitiesForRole(newRole, newCaps); err != nil {
		return err
	}
	if newRole == RoleOwner {
		return nil
	}
	return requireAnotherOwner(all, targetID)
}

// ValidateMembershipRemoval refuses to leave a household without an owner.
func ValidateMembershipRemoval(all []Membership, targetID string) error {
	return requireAnotherOwner(all, targetID)
}

func requireAnotherOwner(all []Membership, excludeID string) error {
	for _, m := range all {
		if m.ID != excludeID && m.Role == RoleOwner {
			return nil
		}
	}
	return ErrLastOwner
}
```

- [ ] **Step 8: Run the domain tests**

Run: `cd api && go test ./internal/domain/... -v`
Expected: PASS, every test.

- [ ] **Step 9: Commit**

```bash
git add api/internal/domain/
git commit -m "feat: add money, roles and capability rules to the domain"
```

---

### Task 7: Domain — the lockout window

**Files:**
- Create: `api/internal/domain/lockout.go`
- Test: `api/internal/domain/lockout_test.go`

**Interfaces:**
- Consumes: `domain` errors from Task 6.
- Produces:
  - `domain.LockoutPolicy{MaxAttempts int; Window time.Duration; LockFor time.Duration}`
  - `domain.DefaultLockoutPolicy() LockoutPolicy` — 3 attempts, 15-minute window, 15-minute lock
  - `domain.LockState{Locked bool; Until time.Time; AttemptsRemaining int}`
  - `(LockoutPolicy).Evaluate(failures []time.Time, now time.Time) LockState`

- [ ] **Step 1: Write the failing lockout test**

Create `api/internal/domain/lockout_test.go`:

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// Package-scoped, and every domain test file shares the domain_test package —
// so this name is deliberately specific rather than a bare `now`.
var lockoutNow = time.Date(2026, 7, 18, 9, 41, 0, 0, time.UTC)

func TestNoFailuresMeansFullAllowance(t *testing.T) {
	state := domain.DefaultLockoutPolicy().Evaluate(nil, now)

	if state.Locked {
		t.Fatal("did not expect a lock")
	}
	if state.AttemptsRemaining != 3 {
		t.Fatalf("AttemptsRemaining = %d, want 3", state.AttemptsRemaining)
	}
}

func TestOneFailureLeavesTwoTries(t *testing.T) {
	failures := []time.Time{lockoutNow.Add(-time.Minute)}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, lockoutNow)

	if state.Locked {
		t.Fatal("did not expect a lock")
	}
	if state.AttemptsRemaining != 2 {
		t.Fatalf("AttemptsRemaining = %d, want 2 — the design's copy says \"Two tries left\"", state.AttemptsRemaining)
	}
}

func TestThreeFailuresInsideTheWindowLock(t *testing.T) {
	failures := []time.Time{
		lockoutNow.Add(-10 * time.Minute),
		lockoutNow.Add(-5 * time.Minute),
		lockoutNow.Add(-1 * time.Minute),
	}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, lockoutNow)

	if !state.Locked {
		t.Fatal("expected a lock")
	}
	if state.AttemptsRemaining != 0 {
		t.Fatalf("AttemptsRemaining = %d, want 0", state.AttemptsRemaining)
	}
	want := lockoutNow.Add(-1 * time.Minute).Add(15 * time.Minute)
	if !state.Until.Equal(want) {
		t.Fatalf("Until = %v, want %v — the lock runs from the most recent failure", state.Until, want)
	}
}

func TestFailuresOlderThanTheWindowAreIgnored(t *testing.T) {
	failures := []time.Time{
		lockoutNow.Add(-40 * time.Minute),
		lockoutNow.Add(-30 * time.Minute),
		lockoutNow.Add(-20 * time.Minute),
	}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, lockoutNow)

	if state.Locked {
		t.Fatal("did not expect a lock from failures outside the window")
	}
	if state.AttemptsRemaining != 3 {
		t.Fatalf("AttemptsRemaining = %d, want 3", state.AttemptsRemaining)
	}
}

func TestTheLockExpiresAfterFifteenMinutes(t *testing.T) {
	failures := []time.Time{
		lockoutNow.Add(-20 * time.Minute),
		lockoutNow.Add(-19 * time.Minute),
		lockoutNow.Add(-18 * time.Minute),
	}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, lockoutNow)

	if state.Locked {
		t.Fatal("the lock should have expired 3 minutes ago")
	}
}

func TestFailuresNeedNotBeSorted(t *testing.T) {
	failures := []time.Time{
		lockoutNow.Add(-1 * time.Minute),
		lockoutNow.Add(-10 * time.Minute),
		lockoutNow.Add(-5 * time.Minute),
	}

	state := domain.DefaultLockoutPolicy().Evaluate(failures, lockoutNow)

	if !state.Locked {
		t.Fatal("expected a lock regardless of input order")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd api && go test ./internal/domain/... -run Lock`
Expected: FAIL — undefined `domain.DefaultLockoutPolicy`.

- [ ] **Step 3: Implement the policy**

Create `api/internal/domain/lockout.go`:

```go
package domain

import "time"

// LockoutPolicy describes how repeated password failures suspend password
// sign-in. Magic-link sign-in is deliberately not covered: it is the recovery
// path, and gating it would let either member lock the household out with no
// way back in short of a terminal.
type LockoutPolicy struct {
	MaxAttempts int
	Window      time.Duration
	LockFor     time.Duration
}

func DefaultLockoutPolicy() LockoutPolicy {
	return LockoutPolicy{MaxAttempts: 3, Window: 15 * time.Minute, LockFor: 15 * time.Minute}
}

type LockState struct {
	Locked            bool
	Until             time.Time
	AttemptsRemaining int
}

// Evaluate reports the lock state given the household's failed password
// attempts. It is pure: callers supply both the failures and the current time.
func (p LockoutPolicy) Evaluate(failures []time.Time, now time.Time) LockState {
	cutoff := now.Add(-p.Window)

	recent := 0
	var latest time.Time
	for _, at := range failures {
		if at.Before(cutoff) {
			continue
		}
		recent++
		if at.After(latest) {
			latest = at
		}
	}

	if recent >= p.MaxAttempts {
		until := latest.Add(p.LockFor)
		if now.Before(until) {
			return LockState{Locked: true, Until: until, AttemptsRemaining: 0}
		}
		// A lock that has been served resets the allowance by design. This is
		// only coherent while LockFor >= Window: with a shorter lock, the lock
		// could expire while failures are still inside the counting window and
		// the caller would get a full fresh allowance anyway.
		return LockState{AttemptsRemaining: p.MaxAttempts}
	}

	return LockState{AttemptsRemaining: p.MaxAttempts - recent}
}
```

- [ ] **Step 4: Run the lockout tests**

Run: `cd api && go test ./internal/domain/... -count=1 -v`
Expected: PASS. Do not filter with `-run Lock` — it substring-matches only two of the six test names, and renaming tests to fit a filter is the wrong direction.

- [ ] **Step 5: Commit**

```bash
git add api/internal/domain/lockout.go api/internal/domain/lockout_test.go
git commit -m "feat: add the household lockout window as a pure domain policy"
```

---

### Task 8: Domain — spaces and visibility

**Files:**
- Create: `api/internal/domain/space.go`
- Test: `api/internal/domain/space_test.go`

**Interfaces:**
- Consumes: `Membership`, `Capabilities`, `Role` from Task 6.
- Produces:
  - `domain.Visibility` with `domain.VisibilityEveryone`, `domain.VisibilityParentsOnly`, `domain.VisibilityCustom`
  - `domain.Space{ID, HouseholdID, Key, Name string; Visibility Visibility; Position int; IsBuiltin bool; RequiredCapability Capability}`
  - `domain.VisibleSpaces(all []Space, m Membership) []Space`
  - `domain.BuiltinSpaces(householdID string) []Space` — the Money, Marriage and Family definitions the seed and every new household use

- [ ] **Step 1: Write the failing test**

Create `api/internal/domain/space_test.go`:

```go
package domain_test

import (
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestBuiltinSpacesMatchTheDesign(t *testing.T) {
	spaces := domain.BuiltinSpaces("h1")

	if len(spaces) != 3 {
		t.Fatalf("len = %d, want 3 (Money, Marriage, Family)", len(spaces))
	}
	keys := map[string]domain.Visibility{}
	for _, s := range spaces {
		keys[s.Key] = s.Visibility
		if !s.IsBuiltin {
			t.Fatalf("%s should be builtin", s.Key)
		}
		if s.HouseholdID != "h1" {
			t.Fatalf("%s has household %q", s.Key, s.HouseholdID)
		}
	}
	if keys["marriage"] != domain.VisibilityParentsOnly {
		t.Fatal("marriage must be parents only")
	}
	if keys["family"] != domain.VisibilityEveryone {
		t.Fatal("family must be visible to everyone")
	}
}

func TestAKidSeesOnlyTheSpacesTheirCapabilitiesAllow(t *testing.T) {
	all := domain.BuiltinSpaces("h1")
	ethan, err := domain.NewMembership("m3", "h1", "u3", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar})
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}

	visible := domain.VisibleSpaces(all, ethan)

	if len(visible) != 1 || visible[0].Key != "family" {
		t.Fatalf("visible = %+v, want only family", visible)
	}
}

func TestAnOwnerSeesEverySpace(t *testing.T) {
	all := domain.BuiltinSpaces("h1")
	andreas, err := domain.NewMembership("m1", "h1", "u1", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}

	if got := len(domain.VisibleSpaces(all, andreas)); got != 3 {
		t.Fatalf("visible = %d, want 3", got)
	}
}

func TestParentsOnlySpacesAreHiddenFromLimitedMembersEvenWithTheCapability(t *testing.T) {
	all := []domain.Space{{
		ID: "s1", HouseholdID: "h1", Key: "money", Name: "Money",
		Visibility: domain.VisibilityParentsOnly, RequiredCapability: domain.CapMoney,
	}}
	kid, err := domain.NewMembership("m2", "h1", "u2", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar, domain.CapMoney})
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}

	if got := domain.VisibleSpaces(all, kid); len(got) != 0 {
		t.Fatalf("visible = %+v, want none: parents_only outranks the capability", got)
	}
}

func TestOrderingIsStable(t *testing.T) {
	all := domain.BuiltinSpaces("h1")
	for i := 1; i < len(all); i++ {
		if all[i-1].Position >= all[i].Position {
			t.Fatalf("positions are not ascending: %+v", all)
		}
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd api && go test ./internal/domain/... -run Space`
Expected: FAIL — undefined identifiers.

- [ ] **Step 3: Implement spaces**

Create `api/internal/domain/space.go`:

```go
package domain

type Visibility string

const (
	VisibilityEveryone    Visibility = "everyone"
	VisibilityParentsOnly Visibility = "parents_only"
	VisibilityCustom      Visibility = "custom"
)

// Space is a sidebar grouping. The sidebar is rendered from these rows rather
// than from code, which is what lets "+ New space" extend the navigation.
type Space struct {
	ID                 string
	HouseholdID        string
	Key                string
	Name               string
	Visibility         Visibility
	Position           int
	IsBuiltin          bool
	RequiredCapability Capability // empty means no capability is required
}

// BuiltinSpaces is the set every household starts with, taken from the design's
// Settings screen: Money and Marriage are parents-only, Family is for everyone.
func BuiltinSpaces(householdID string) []Space {
	return []Space{
		// Money is capability-gated, not structurally locked. The design's
		// Settings screen labels it "Parents" without a lock, and the invite
		// modal offers "Money & balances" as a toggle that is off for kids by
		// default — meaning it can be switched on. Marriage carries the lock
		// icon and is the only structurally parents-only space.
		{HouseholdID: householdID, Key: "money", Name: "Money",
			Visibility: VisibilityEveryone, Position: 1, IsBuiltin: true, RequiredCapability: CapMoney},
		{HouseholdID: householdID, Key: "marriage", Name: "Marriage",
			Visibility: VisibilityParentsOnly, Position: 2, IsBuiltin: true, RequiredCapability: CapMarriage},
		// Family is "Everyone" with no qualifier in the design. Requiring a
		// capability here would hide it from a member holding only chores.
		{HouseholdID: householdID, Key: "family", Name: "Family",
			Visibility: VisibilityEveryone, Position: 3, IsBuiltin: true, RequiredCapability: ""},
	}
}

// VisibleSpaces filters spaces for one membership. Visibility is checked before
// capability, so a parents-only space stays hidden from a limited member even
// if their capability set would otherwise allow it. An unrecognised Visibility
// value is treated as owner-only rather than as everyone -- see the default
// case below.
//
// The result preserves the input order; VisibleSpaces does not sort. Callers
// must supply all already ordered by Position — Task 9's query does this with
// an ORDER BY, and Task 19's sidebar relies on the order coming out as given.
func VisibleSpaces(all []Space, m Membership) []Space {
	visible := make([]Space, 0, len(all))
	for _, s := range all {
		switch s.Visibility {
		case VisibilityEveryone:
			// No visibility restriction; the capability check below still applies.
		case VisibilityParentsOnly:
			if m.Role != RoleOwner {
				continue
			}
		case VisibilityCustom:
			// Provisional: per-space member lists do not exist yet, and the
			// design marks custom space pages "not built". A visibility mode
			// whose membership model is unbuilt must fail closed rather than
			// default to maximum exposure, so custom spaces are owner-only
			// until per-space membership is implemented.
			if m.Role != RoleOwner {
				continue
			}
		default:
			// An unrecognised Visibility is a data or version problem, not a
			// choice anyone made -- the same situation validateCapabilitiesForRole
			// faces with an unknown Role rebuilt from a Postgres column (see
			// ErrUnknownRole in identity.go). VisibleSpaces has no error return
			// to report that, so it fails closed instead: the safe reading of
			// "I do not know who may see this" is "not everyone", so an unknown
			// value is owner-only, like VisibilityCustom.
			if m.Role != RoleOwner {
				continue
			}
		}
		if s.RequiredCapability != "" && !m.Capabilities.Has(s.RequiredCapability) {
			continue
		}
		visible = append(visible, s)
	}
	return visible
}
```

- [ ] **Step 4: Run the space tests**

Run: `cd api && go test ./internal/domain/... -v`
Expected: PASS, every domain test including the earlier ones.

- [ ] **Step 5: Commit**

```bash
git add api/internal/domain/space.go api/internal/domain/space_test.go
git commit -m "feat: add spaces and visibility filtering to the domain"
```

---

### Task 9: The identity schema and sqlc

**Files:**
- Create: `api/migrations/00002_identity.sql`, `api/sqlc.yaml`, `api/internal/adapter/postgres/queries/identity.sql`
- Modify: `api/internal/adapter/postgres/pool_test.go` (its `schema_smoke` assertion, which this migration invalidates)
- Modify: `Makefile` (add `sqlc`)

`api/migrations/00001_init.sql` is **not** touched. It has already been applied on every developer's database and goose will not re-run it, so anything this task needs — including the `citext` extension — belongs in `00002`.
- Test: `api/internal/adapter/postgres/schema_test.go`

**Interfaces:**
- Consumes: `testsupport.StartPostgres` from the skeleton plan's Task 2.
- Produces: the tables `users`, `households`, `memberships`, `invites`, `sessions`, `magic_links`, `login_attempts`, `spaces`, `notification_preferences`, and the generated package `api/internal/adapter/postgres/sqlcgen`.

- [ ] **Step 1: Write the migration**

Create `api/migrations/00002_identity.sql`:

```sql
-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;

DROP TABLE IF EXISTS schema_smoke;

CREATE TABLE households (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                    text        NOT NULL,
    family_name             text        NOT NULL,
    primary_currency        char(3)     NOT NULL DEFAULT 'SGD',
    show_secondary_currency boolean     NOT NULL DEFAULT true,
    secondary_currency      char(3)     NOT NULL DEFAULT 'IDR',
    fx_rate_mode            text        NOT NULL DEFAULT 'auto'
                                        CHECK (fx_rate_mode IN ('auto', 'manual')),
    created_at              timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email          citext UNIQUE,
    password_hash  text,
    display_name   text        NOT NULL,
    avatar_initial char(1)     NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE memberships (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role         text        NOT NULL CHECK (role IN ('owner', 'limited')),
    capabilities text[]      NOT NULL DEFAULT '{}',
    joined_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, user_id),
    CONSTRAINT capabilities_are_known CHECK (
        capabilities <@ ARRAY['calendar', 'chores', 'money', 'marriage']::text[]
    ),
    CONSTRAINT limited_members_have_no_marriage CHECK (
        role <> 'limited' OR NOT ('marriage' = ANY (capabilities))
    ),
    -- Mirrors the domain rule that an owner holds every capability. The domain
    -- constructor is the first gate; this is the second, for rows written by
    -- anything that bypasses it.
    CONSTRAINT owners_hold_all_capabilities CHECK (
        role <> 'owner'
        OR capabilities @> ARRAY['calendar', 'chores', 'money', 'marriage']::text[]
    )
);

CREATE TABLE invites (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    email        citext      NOT NULL,
    name         text        NOT NULL,
    role         text        NOT NULL CHECK (role IN ('owner', 'limited')),
    capabilities text[]      NOT NULL DEFAULT '{}',
    token_hash   bytea       NOT NULL UNIQUE,
    invited_by   uuid        NOT NULL REFERENCES users(id),
    expires_at   timestamptz NOT NULL,
    accepted_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash   bytea       NOT NULL UNIQUE,
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz
);
CREATE INDEX sessions_user_idx ON sessions (user_id) WHERE revoked_at IS NULL;

CREATE TABLE magic_links (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  bytea       NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX magic_links_user_created_idx ON magic_links (user_id, created_at DESC);

-- household_id and user_id are nullable so that an attempt against an unknown
-- address can still be recorded for global rate limiting without revealing
-- whether that address exists.
CREATE TABLE login_attempts (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid REFERENCES households(id) ON DELETE CASCADE,
    user_id      uuid REFERENCES users(id) ON DELETE CASCADE,
    email        citext      NOT NULL,
    succeeded    boolean     NOT NULL,
    at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX login_attempts_household_at_idx ON login_attempts (household_id, at DESC);

CREATE TABLE spaces (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id        uuid    NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    key                 text    NOT NULL,
    name                text    NOT NULL,
    visibility          text    NOT NULL CHECK (visibility IN ('everyone', 'parents_only', 'custom')),
    position            integer NOT NULL,
    is_builtin          boolean NOT NULL DEFAULT false,
    required_capability text    NOT NULL DEFAULT '',
    UNIQUE (household_id, key)
);

CREATE TABLE notification_preferences (
    household_id     uuid PRIMARY KEY REFERENCES households(id) ON DELETE CASCADE,
    bill_reminders   boolean NOT NULL DEFAULT true,
    overspend_alerts boolean NOT NULL DEFAULT true,
    retro_reminder   boolean NOT NULL DEFAULT true,
    weekly_digest    boolean NOT NULL DEFAULT true
);

-- +goose Down
DROP TABLE notification_preferences, spaces, login_attempts, magic_links,
           sessions, invites, memberships, users, households;
CREATE TABLE schema_smoke (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at timestamptz NOT NULL DEFAULT now()
);
```

- [ ] **Step 2: Write the failing schema test**

Create `api/internal/adapter/postgres/schema_test.go`:

```go
package postgres_test

import (
	"context"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/testsupport"
)

func TestIdentitySchemaEnforcesTheCapabilityConstraints(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	var householdID, userID string
	err := db.Pool().QueryRow(ctx,
		`INSERT INTO households (name, family_name) VALUES ('Andreas & Christine', 'Oentoro') RETURNING id`).
		Scan(&householdID)
	if err != nil {
		t.Fatalf("insert household: %v", err)
	}
	err = db.Pool().QueryRow(ctx,
		`INSERT INTO users (display_name, avatar_initial) VALUES ('Ethan', 'E') RETURNING id`).
		Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	_, err = db.Pool().Exec(ctx,
		`INSERT INTO memberships (household_id, user_id, role, capabilities)
		 VALUES ($1, $2, 'limited', ARRAY['calendar','marriage'])`, householdID, userID)
	if err == nil {
		t.Fatal("the database accepted a limited member holding marriage")
	}

	_, err = db.Pool().Exec(ctx,
		`INSERT INTO memberships (household_id, user_id, role, capabilities)
		 VALUES ($1, $2, 'limited', ARRAY['spaceships'])`, householdID, userID)
	if err == nil {
		t.Fatal("the database accepted an unknown capability")
	}
}

func TestLoginAttemptsAcceptAnUnknownAddress(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Pool().Exec(context.Background(),
		`INSERT INTO login_attempts (email, succeeded) VALUES ('stranger@example.com', false)`)
	if err != nil {
		t.Fatalf("a login attempt with no household or user must be recordable: %v", err)
	}
}

func openTestDB(t *testing.T) *postgres.DB {
	t.Helper()
	db, err := postgres.Open(context.Background(), testsupport.StartPostgres(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}
```

- [ ] **Step 3: Run it and watch it fail**

Run: `cd api && go test ./internal/adapter/postgres/... -run Identity`
Expected: FAIL — relation `households` does not exist, because the migration has not been written into `migrations/` yet or has a syntax error. Fix any SQL errors reported here before continuing.

- [ ] **Step 3b: Update the skeleton plan's schema assertion**

This migration drops `schema_smoke`, so `TestMigrationsCreatedTheSchema` in `api/internal/adapter/postgres/pool_test.go` no longer describes reality. Change its query to assert the identity schema instead:

```go
	var count int
	err = db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_name = 'households'`).Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("households table not found; migrations did not run")
	}
```

- [ ] **Step 4: Run the schema tests**

Run: `cd api && go test ./internal/adapter/postgres/... -v`
Expected: PASS, including the updated `TestMigrationsCreatedTheSchema`.

- [ ] **Step 5: Configure sqlc**

Create `api/sqlc.yaml`:

```yaml
version: "2"
sql:
  - engine: postgresql
    schema: ./migrations
    queries: ./internal/adapter/postgres/queries
    gen:
      go:
        package: sqlcgen
        out: ./internal/adapter/postgres/sqlcgen
        sql_package: pgx/v5
        emit_pointers_for_null_types: true
```

Add to `Makefile`:

```makefile
sqlc: ## Regenerate the typed queries from SQL
	cd api && go tool sqlc generate
```

and add the tool dependency:

```bash
cd api && go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc@latest
```

- [ ] **Step 6: Write the queries**

Create `api/internal/adapter/postgres/queries/identity.sql`:

```sql
-- name: GetUserByEmail :one
SELECT id, email, password_hash, display_name, avatar_initial FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT id, email, password_hash, display_name, avatar_initial FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name, avatar_initial)
VALUES ($1, $2, $3, $4)
RETURNING id, email, password_hash, display_name, avatar_initial;

-- name: SetPasswordHash :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: GetHousehold :one
SELECT id, name, family_name, primary_currency, show_secondary_currency,
       secondary_currency, fx_rate_mode
FROM households WHERE id = $1;

-- name: UpdateHousehold :one
UPDATE households
SET family_name = $2, primary_currency = $3, show_secondary_currency = $4, fx_rate_mode = $5
WHERE id = $1
RETURNING id, name, family_name, primary_currency, show_secondary_currency,
          secondary_currency, fx_rate_mode;

-- name: CreateHousehold :one
INSERT INTO households (name, family_name) VALUES ($1, $2)
RETURNING id, name, family_name, primary_currency, show_secondary_currency,
          secondary_currency, fx_rate_mode;

-- name: ListMemberships :many
SELECT m.id, m.household_id, m.user_id, m.role, m.capabilities,
       u.email, u.display_name, u.avatar_initial
FROM memberships m JOIN users u ON u.id = m.user_id
WHERE m.household_id = $1
ORDER BY m.role DESC, u.display_name;

-- name: GetMembershipByUser :one
SELECT id, household_id, user_id, role, capabilities
FROM memberships WHERE user_id = $1 LIMIT 1;

-- name: CreateMembership :one
INSERT INTO memberships (household_id, user_id, role, capabilities)
VALUES ($1, $2, $3, $4)
RETURNING id, household_id, user_id, role, capabilities;

-- name: UpdateMembership :exec
UPDATE memberships SET role = $3, capabilities = $4 WHERE household_id = $1 AND id = $2;

-- name: DeleteMembership :exec
DELETE FROM memberships WHERE household_id = $1 AND id = $2;

-- name: CreateSession :one
INSERT INTO sessions (token_hash, user_id, household_id, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: GetLiveSession :one
SELECT id, user_id, household_id, expires_at FROM sessions
WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now();

-- name: ExtendSession :exec
UPDATE sessions SET expires_at = $2 WHERE token_hash = $1;

-- name: RevokeSessionByToken :exec
UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeSessionsForUser :exec
UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;

-- name: CreateMagicLink :exec
INSERT INTO magic_links (user_id, token_hash, expires_at) VALUES ($1, $2, $3);

-- name: ConsumeMagicLink :one
UPDATE magic_links SET consumed_at = now()
WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING user_id;

-- name: CountRecentMagicLinks :one
SELECT count(*) FROM magic_links m JOIN users u ON u.id = m.user_id
WHERE u.email = $1 AND m.created_at > $2;

-- name: RecordLoginAttempt :exec
INSERT INTO login_attempts (household_id, user_id, email, succeeded, at)
VALUES ($1, $2, $3, $4, $5);

-- name: ListRecentFailures :many
SELECT at FROM login_attempts
WHERE household_id = $1 AND succeeded = false AND at > $2
ORDER BY at DESC;

-- name: ListRecentFailuresByEmail :many
SELECT at FROM login_attempts
WHERE email = $1 AND succeeded = false AND at > $2
ORDER BY at DESC;

-- name: ClearFailures :exec
DELETE FROM login_attempts WHERE household_id = $1 AND succeeded = false;

-- name: CreateInvite :one
INSERT INTO invites (household_id, email, name, role, capabilities, token_hash, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id;

-- name: GetInviteByTokenHash :one
SELECT i.id, i.household_id, i.email, i.name, i.role, i.capabilities,
       i.expires_at, i.accepted_at, h.family_name, u.display_name AS inviter_name
FROM invites i
JOIN households h ON h.id = i.household_id
JOIN users u ON u.id = i.invited_by
WHERE i.token_hash = $1;

-- name: MarkInviteAccepted :exec
UPDATE invites SET accepted_at = now() WHERE id = $1;

-- name: ListSpaces :many
SELECT id, household_id, key, name, visibility, position, is_builtin, required_capability
FROM spaces WHERE household_id = $1 ORDER BY position;

-- name: CreateSpace :one
INSERT INTO spaces (household_id, key, name, visibility, position, is_builtin, required_capability)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, household_id, key, name, visibility, position, is_builtin, required_capability;

-- name: NextSpacePosition :one
SELECT coalesce(max(position), 0) + 1 FROM spaces WHERE household_id = $1;

-- name: GetNotificationPreferences :one
SELECT household_id, bill_reminders, overspend_alerts, retro_reminder, weekly_digest
FROM notification_preferences WHERE household_id = $1;

-- name: UpsertNotificationPreferences :one
INSERT INTO notification_preferences (household_id, bill_reminders, overspend_alerts, retro_reminder, weekly_digest)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (household_id) DO UPDATE
SET bill_reminders = excluded.bill_reminders,
    overspend_alerts = excluded.overspend_alerts,
    retro_reminder = excluded.retro_reminder,
    weekly_digest = excluded.weekly_digest
RETURNING household_id, bill_reminders, overspend_alerts, retro_reminder, weekly_digest;
```

- [ ] **Step 7: Generate and build**

Run: `make sqlc && cd api && go build ./...`
Expected: `internal/adapter/postgres/sqlcgen/` appears and compiles.

- [ ] **Step 8: Commit**

```bash
git add api/migrations api/sqlc.yaml api/internal/adapter/postgres Makefile
git commit -m "feat: add the identity schema and generate typed queries with sqlc"
```

---

### Task 10: Ports, argon2id hashing and token generation

**Files:**
- Create: `api/internal/usecase/ports.go`, `api/internal/adapter/crypto/argon2.go`, `api/internal/adapter/crypto/tokens.go`, `api/internal/adapter/clock/clock.go`
- Test: `api/internal/adapter/crypto/argon2_test.go`, `api/internal/adapter/crypto/tokens_test.go`

**Interfaces:**
- Consumes: `domain` from Tasks 6–8.
- Produces:
  - `config.Config` gains `SMTPAddr`, `SMTPFrom`, `AppBaseURL`, `Argon2Time`, `Argon2MemoryKiB`, `Argon2Threads` — read from `SMTP_ADDR`, `SMTP_FROM`, `APP_BASE_URL`, `ARGON2_TIME`, `ARGON2_MEMORY_KIB`, `ARGON2_THREADS`. The first three are required; the argon2 three default to 3, 65536 and 2. The spec requires argon2 parameters to live in configuration so they can be raised without a code change, and the mailer cannot be constructed without the first three.
  - `usecase.Clock interface { Now() time.Time }`
  - `usecase.PasswordHasher interface { Hash(plain string) (string, error); Verify(plain, encoded string) bool }`
  - `usecase.TokenGenerator interface { NewToken() (raw string, hash []byte, err error); HashToken(raw string) []byte }`
  - `usecase.Mailer interface { SendMagicLink(ctx context.Context, to, name, url string) error; SendInvite(ctx context.Context, to, name, inviterName, url string) error }`
  - `usecase.UserRepository`, `usecase.HouseholdRepository`, `usecase.MembershipRepository`, `usecase.SessionRepository`, `usecase.MagicLinkRepository`, `usecase.LoginAttemptRepository`, `usecase.InviteRepository`, `usecase.SpaceRepository`, `usecase.NotificationRepository` — full signatures below
  - `usecase.Rate{Numerator, Denominator int64}` and `usecase.FXRateProvider interface { Rate(ctx context.Context, from, to string) (Rate, error) }` — a ratio, not a scaled decimal, so that inverting a rate is exact in both directions
  - `crypto.NewArgon2Hasher() *Argon2Hasher`, `crypto.NewTokenGenerator() *Tokens`, `clock.System{}`, `fx.NewStaticProvider() *StaticProvider`

- [ ] **Step 0: Extend the configuration**

`config.Config` today carries only `AppEnv`, `Port`, `DatabaseURL` and `SessionSecret`. This task's hasher and the next task's mailer both need more. Add the six fields listed under Produces above, validating that `SMTP_ADDR`, `SMTP_FROM` and `APP_BASE_URL` are non-empty and that the three argon2 values are positive, in the same style as the existing `DATABASE_URL` and `SESSION_SECRET` checks. Extend `config_test.go` to cover a missing `SMTP_FROM` and a zero `ARGON2_TIME`.

`docker-compose.yml` already sets `SMTP_ADDR`, `SMTP_FROM` and `APP_BASE_URL`; add the three argon2 variables there and to `.env.example`. Confirm `make up` still reaches `{"status":"ready"}` before moving on — a required variable that Compose does not set would stop the API from starting at all.

- [ ] **Step 1: Write the failing hasher test**

Create `api/internal/adapter/crypto/argon2_test.go`:

```go
package crypto_test

import (
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
)

func TestHashThenVerify(t *testing.T) {
	h := crypto.NewArgon2Hasher(3, 64*1024, 2)

	encoded, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$") {
		t.Fatalf("encoded = %q, want the argon2id PHC format", encoded)
	}
	if !h.Verify("correct horse battery staple", encoded) {
		t.Fatal("Verify rejected the correct password")
	}
	if h.Verify("wrong password", encoded) {
		t.Fatal("Verify accepted the wrong password")
	}
}

func TestHashIsSaltedPerCall(t *testing.T) {
	h := crypto.NewArgon2Hasher(3, 64*1024, 2)

	first, _ := h.Hash("same")
	second, _ := h.Hash("same")

	if first == second {
		t.Fatal("two hashes of the same password must differ")
	}
}

func TestVerifyRejectsMalformedInput(t *testing.T) {
	h := crypto.NewArgon2Hasher(3, 64*1024, 2)

	for _, encoded := range []string{"", "not-a-hash", "$argon2id$v=19$m=1", "$bcrypt$whatever"} {
		if h.Verify("anything", encoded) {
			t.Fatalf("Verify accepted %q", encoded)
		}
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd api && go test ./internal/adapter/crypto/...`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement the hasher**

```bash
cd api && go get golang.org/x/crypto/argon2@latest
```

Create `api/internal/adapter/crypto/argon2.go`:

```go
// Package crypto implements the hashing and token ports. Parameters live in
// one place so they can be raised without touching any caller.
package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Argon2Hasher struct {
	time    uint32
	memory  uint32
	threads uint8
	keyLen  uint32
	saltLen int
}

// NewArgon2Hasher takes its cost parameters from configuration so they can be
// raised without a code change, as the spec requires. Callers pass
// cfg.Argon2Time, cfg.Argon2MemoryKiB and cfg.Argon2Threads.
func NewArgon2Hasher(time uint32, memoryKiB uint32, threads uint8) *Argon2Hasher {
	return &Argon2Hasher{time: time, memory: memoryKiB, threads: threads, keyLen: 32, saltLen: 16}
}

func (h *Argon2Hasher) Hash(plain string) (string, error) {
	salt := make([]byte, h.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	key := argon2.IDKey([]byte(plain), salt, h.time, h.memory, h.threads, h.keyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.memory, h.time, h.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify is constant-time in the comparison and tolerant of malformed input:
// any parse failure is a rejection, never a panic.
func (h *Argon2Hasher) Verify(plain, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}

	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}

	got := argon2.IDKey([]byte(plain), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
```

- [ ] **Step 4: Write the failing token test**

Create `api/internal/adapter/crypto/tokens_test.go`:

```go
package crypto_test

import (
	"bytes"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
)

func TestNewTokenIsUrlSafeAndUnique(t *testing.T) {
	g := crypto.NewTokenGenerator()

	first, firstHash, err := g.NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	second, _, _ := g.NewToken()

	if first == second {
		t.Fatal("two tokens must differ")
	}
	if len(first) < 32 {
		t.Fatalf("token is only %d characters", len(first))
	}
	for _, r := range first {
		if r == '+' || r == '/' || r == '=' {
			t.Fatalf("token %q is not URL-safe", first)
		}
	}
	if !bytes.Equal(firstHash, g.HashToken(first)) {
		t.Fatal("HashToken must reproduce the hash returned by NewToken")
	}
}

func TestHashTokenIsDeterministic(t *testing.T) {
	g := crypto.NewTokenGenerator()

	if !bytes.Equal(g.HashToken("abc"), g.HashToken("abc")) {
		t.Fatal("HashToken must be deterministic")
	}
	if bytes.Equal(g.HashToken("abc"), g.HashToken("abd")) {
		t.Fatal("different tokens must hash differently")
	}
}
```

- [ ] **Step 5: Run it and watch it fail**

Run: `cd api && go test ./internal/adapter/crypto/... -run Token`
Expected: FAIL — undefined `crypto.NewTokenGenerator`.

- [ ] **Step 6: Implement token generation and the clock**

Create `api/internal/adapter/crypto/tokens.go`:

```go
package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// Tokens issues opaque bearer strings for sessions, magic links and invites.
// Only the SHA-256 hash is ever persisted; the raw value lives in a cookie or
// an email and nowhere else.
type Tokens struct{}

func NewTokenGenerator() *Tokens { return &Tokens{} }

func (t *Tokens) NewToken() (string, []byte, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, fmt.Errorf("read random bytes: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	return raw, t.HashToken(raw), nil
}

func (t *Tokens) HashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
```

Create `api/internal/adapter/clock/clock.go`:

```go
// Package clock provides the production implementation of the Clock port.
// Tests inject a fixed clock instead.
package clock

import "time"

type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }
```

- [ ] **Step 7: Declare every port**

Create `api/internal/usecase/ports.go`:

```go
// Package usecase holds the application services. It depends on domain and on
// the port interfaces declared here — never on an adapter.
package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

type Clock interface {
	Now() time.Time
}

type PasswordHasher interface {
	Hash(plain string) (string, error)
	Verify(plain, encoded string) bool
}

type TokenGenerator interface {
	NewToken() (raw string, hash []byte, err error)
	HashToken(raw string) []byte
}

type Mailer interface {
	SendMagicLink(ctx context.Context, to, name, url string) error
	SendInvite(ctx context.Context, to, name, inviterName, url string) error
}

// StoredUser carries the password hash, which never leaves the usecase layer.
type StoredUser struct {
	domain.User
	PasswordHash string
}

type UserRepository interface {
	ByEmail(ctx context.Context, email string) (StoredUser, error)
	ByID(ctx context.Context, id string) (StoredUser, error)
	Create(ctx context.Context, email, passwordHash, displayName string) (domain.User, error)
	SetPasswordHash(ctx context.Context, userID, hash string) error
}

type HouseholdRepository interface {
	Get(ctx context.Context, householdID string) (domain.Household, error)
	Update(ctx context.Context, h domain.Household) (domain.Household, error)
	Create(ctx context.Context, name, familyName string) (domain.Household, error)
}

// MemberView is a membership joined to its user, which is what every consumer
// of the members list actually wants.
type MemberView struct {
	Membership domain.Membership
	User       domain.User
}

type MembershipRepository interface {
	List(ctx context.Context, householdID string) ([]MemberView, error)
	// ByUser is the one method that cannot take a household scope, because
	// sign-in resolves the household from it. It is therefore the seam where
	// multi-tenancy will need attention: today it returns the single membership
	// a user has, and the query's LIMIT 1 would pick arbitrarily if a user ever
	// belonged to two households.
	ByUser(ctx context.Context, userID string) (domain.Membership, error)
	Create(ctx context.Context, m domain.Membership) (domain.Membership, error)
	Update(ctx context.Context, householdID, membershipID string, role domain.Role, caps domain.Capabilities) error
	Delete(ctx context.Context, householdID, membershipID string) error
}

type SessionRecord struct {
	UserID      string
	HouseholdID string
	ExpiresAt   time.Time
}

type SessionRepository interface {
	Create(ctx context.Context, tokenHash []byte, userID, householdID string, expiresAt time.Time) error
	ByTokenHash(ctx context.Context, tokenHash []byte) (SessionRecord, error)
	Extend(ctx context.Context, tokenHash []byte, expiresAt time.Time) error
	RevokeByToken(ctx context.Context, tokenHash []byte) error
	RevokeAllForUser(ctx context.Context, userID string) error
}

type MagicLinkRepository interface {
	Create(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time) error
	Consume(ctx context.Context, tokenHash []byte) (userID string, err error)
	CountSince(ctx context.Context, email string, since time.Time) (int, error)
}

type LoginAttemptRepository interface {
	Record(ctx context.Context, householdID, userID *string, email string, succeeded bool, at time.Time) error
	FailuresSince(ctx context.Context, householdID string, since time.Time) ([]time.Time, error)
	// FailuresSinceForEmail counts attempts by address rather than by household.
	// Sign-in uses it for addresses that match no user, so a stranger sees the
	// same countdown a member does and cannot tell the two apart.
	FailuresSinceForEmail(ctx context.Context, email string, since time.Time) ([]time.Time, error)
	ClearFailures(ctx context.Context, householdID string) error
}

type InviteDetails struct {
	ID            string
	HouseholdID   string
	Email         string
	Name          string
	Role          domain.Role
	Capabilities  domain.Capabilities
	FamilyName    string
	InviterName   string
	ExpiresAt     time.Time
	AcceptedAt    *time.Time
}

type InviteRepository interface {
	Create(ctx context.Context, householdID, email, name string, role domain.Role,
		caps domain.Capabilities, tokenHash []byte, invitedBy string, expiresAt time.Time) (string, error)
	ByTokenHash(ctx context.Context, tokenHash []byte) (InviteDetails, error)
	MarkAccepted(ctx context.Context, inviteID string) error
}

type SpaceRepository interface {
	List(ctx context.Context, householdID string) ([]domain.Space, error)
	Create(ctx context.Context, s domain.Space) (domain.Space, error)
	NextPosition(ctx context.Context, householdID string) (int, error)
}

type NotificationPreferences struct {
	BillReminders   bool
	OverspendAlerts bool
	RetroReminder   bool
	WeeklyDigest    bool
}

type NotificationRepository interface {
	Get(ctx context.Context, householdID string) (NotificationPreferences, error)
	Upsert(ctx context.Context, householdID string, p NotificationPreferences) (NotificationPreferences, error)
}

// Rate is a ratio, held as a fraction rather than a scaled decimal. SGD to IDR
// is {12410, 1}; IDR to SGD is {1, 12410}. A scaled decimal cannot represent
// the second direction — 0.0000806 truncates to zero at any sane scale — and
// IDR to SGD is precisely the direction the design's Finances screen uses.
type Rate struct {
	Numerator   int64
	Denominator int64
}

// Apply converts an amount of minor units, rounding half away from zero.
func (r Rate) Apply(minorUnits int64) int64 {
	num := minorUnits * r.Numerator
	half := r.Denominator / 2
	if num < 0 {
		return (num - half) / r.Denominator
	}
	return (num + half) / r.Denominator
}

// FXRateProvider converts between the household's primary and secondary
// currencies. The design labels the rate "auto"; a live provider replaces the
// static one without any caller changing.
type FXRateProvider interface {
	Rate(ctx context.Context, from, to string) (Rate, error)
}
```

- [ ] **Step 7b: Write the failing FX test**

Create `api/internal/adapter/fx/static_test.go`:

```go
package fx_test

import (
	"context"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/fx"
)

func TestStaticProviderKnowsTheDesignsRate(t *testing.T) {
	p := fx.NewStaticProvider()

	rate, err := p.Rate(context.Background(), "SGD", "IDR")
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if rate.Numerator != 12_410 || rate.Denominator != 1 {
		t.Fatalf("rate = %+v, want {12410, 1} (S$1 = Rp 12,410)", rate)
	}
}

func TestStaticProviderInvertsExactly(t *testing.T) {
	p := fx.NewStaticProvider()

	rate, err := p.Rate(context.Background(), "IDR", "SGD")
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if rate.Numerator != 1 || rate.Denominator != 12_410 {
		t.Fatalf("rate = %+v, want {1, 12410}", rate)
	}

	// The design's Finances screen: Rp 85,400,000 shown as approximately
	// S$6,880. In minor units that is 8_540_000_000 IDR.
	// 8_540_000_000 / 12_410 = 688_154.7…, which rounds to 688_155 → S$6,881.55.
	if got := rate.Apply(8_540_000_000); got != 688_155 {
		t.Fatalf("Apply = %d, want 688155", got)
	}
}

func TestStaticProviderReturnsUnityForTheSameCurrency(t *testing.T) {
	p := fx.NewStaticProvider()

	rate, err := p.Rate(context.Background(), "SGD", "SGD")
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if rate.Apply(1234) != 1234 {
		t.Fatalf("a same-currency rate must be the identity, got %+v", rate)
	}
}

func TestStaticProviderRejectsAnUnknownPair(t *testing.T) {
	p := fx.NewStaticProvider()

	if _, err := p.Rate(context.Background(), "SGD", "JPY"); err == nil {
		t.Fatal("expected an error for a pair the static table does not cover")
	}
}
```

Run: `cd api && go test ./internal/adapter/fx/...`
Expected: FAIL — package does not exist.

- [ ] **Step 7c: Implement the static provider**

Create `api/internal/adapter/fx/static.go`:

```go
// Package fx implements the FXRateProvider port. Today the table is fixed; when
// a live source arrives it becomes another implementation of the same port.
package fx

import (
	"context"
	"fmt"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type StaticProvider struct {
	// units maps a currency pair to how many units of the second currency one
	// unit of the first buys. Stored one way only; the inverse is exact because
	// a Rate is a fraction.
	units map[[2]string]int64
}

func NewStaticProvider() *StaticProvider {
	return &StaticProvider{units: map[[2]string]int64{
		{"SGD", "IDR"}: 12_410, // per the design's Settings screen
	}}
}

func (p *StaticProvider) Rate(_ context.Context, from, to string) (usecase.Rate, error) {
	if from == to {
		return usecase.Rate{Numerator: 1, Denominator: 1}, nil
	}
	if n, ok := p.units[[2]string{from, to}]; ok {
		return usecase.Rate{Numerator: n, Denominator: 1}, nil
	}
	if n, ok := p.units[[2]string{to, from}]; ok {
		return usecase.Rate{Numerator: 1, Denominator: n}, nil
	}
	return usecase.Rate{}, fmt.Errorf("no rate available for %s to %s", from, to)
}
```

Note the import direction: `adapter/fx` imports `usecase` to satisfy its port, which is exactly what the dependency rule allows. `make lint-arch` confirms it.

Run: `cd api && go test ./internal/adapter/fx/... -v`
Expected: PASS, three tests.

- [ ] **Step 8: Run every test and the architecture lint**

Run: `cd api && go build ./... && go test ./internal/adapter/crypto/... -v && cd .. && make lint-arch`
Expected: PASS on all three.

- [ ] **Step 9: Commit**

```bash
git add api/internal/usecase api/internal/adapter/crypto api/internal/adapter/clock api/internal/adapter/fx
git commit -m "feat: declare the usecase ports, argon2id hashing and the static FX provider"
```

---

### Task 11: Postgres repositories

**Files:**
- Create: `api/internal/adapter/postgres/user_repo.go`, `household_repo.go`, `membership_repo.go`, `session_repo.go`, `magiclink_repo.go`, `loginattempt_repo.go`, `invite_repo.go`, `space_repo.go`, `notification_repo.go`
- Test: `api/internal/adapter/postgres/repos_test.go`

**Interfaces:**
- Consumes: every port from Task 10, the generated `sqlcgen` package from Task 9.
- Produces: `postgres.NewUserRepo(db *DB) *UserRepo` and one constructor per repository, each satisfying its port. All return `domain.ErrNotFound` when a row is absent, so no caller ever sees `pgx.ErrNoRows`.

- [ ] **Step 1: Write the failing repository test**

Create `api/internal/adapter/postgres/repos_test.go`:

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

func TestUserRepoRoundTrip(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.NewUserRepo(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, "andreas@hearth.family", "$argon2id$fake", "Andreas")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.AvatarInitial != "A" {
		t.Fatalf("AvatarInitial = %q, want A — it is derived from the display name", created.AvatarInitial)
	}

	found, err := repo.ByEmail(ctx, "ANDREAS@HEARTH.FAMILY")
	if err != nil {
		t.Fatalf("ByEmail is case-insensitive because the column is citext: %v", err)
	}
	if found.ID != created.ID {
		t.Fatal("ByEmail returned a different user")
	}

	if _, err := repo.ByEmail(ctx, "nobody@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}
}

func TestMembershipRepoRejectsAnInvalidCapabilitySet(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	members := postgres.NewMembershipRepo(db)

	h, err := households.Create(ctx, "Andreas & Christine", "Oentoro")
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	u, err := users.Create(ctx, "", "", "Ethan")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// The domain constructor is the first gate; the database check constraint is
	// the second. This asserts the second, by bypassing the first.
	_, err = members.Create(ctx, domain.Membership{
		HouseholdID: h.ID, UserID: u.ID, Role: domain.RoleLimited,
		Capabilities: domain.Capabilities{domain.CapMarriage},
	})
	if err == nil {
		t.Fatal("expected the database constraint to reject marriage for a limited member")
	}
}

func TestSessionLifecycle(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	h, _ := postgres.NewHouseholdRepo(db).Create(ctx, "H", "H")
	u, _ := postgres.NewUserRepo(db).Create(ctx, "a@b.c", "hash", "Andreas")
	sessions := postgres.NewSessionRepo(db)

	hash := []byte("0123456789abcdef0123456789abcdef")
	expiry := time.Now().Add(30 * 24 * time.Hour)

	if err := sessions.Create(ctx, hash, u.ID, h.ID, expiry); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rec, err := sessions.ByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	if rec.UserID != u.ID || rec.HouseholdID != h.ID {
		t.Fatalf("record = %+v", rec)
	}

	if err := sessions.RevokeByToken(ctx, hash); err != nil {
		t.Fatalf("RevokeByToken: %v", err)
	}
	if _, err := sessions.ByTokenHash(ctx, hash); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a revoked session must not resolve, got err = %v", err)
	}
}

func TestLoginAttemptsRespectTheWindow(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	h, _ := postgres.NewHouseholdRepo(db).Create(ctx, "H", "H")
	attempts := postgres.NewLoginAttemptRepo(db)

	base := time.Now().UTC()
	for _, offset := range []time.Duration{-40 * time.Minute, -5 * time.Minute, -1 * time.Minute} {
		if err := attempts.Record(ctx, &h.ID, nil, "a@b.c", false, base.Add(offset)); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	failures, err := attempts.FailuresSince(ctx, h.ID, base.Add(-15*time.Minute))
	if err != nil {
		t.Fatalf("FailuresSince: %v", err)
	}
	if len(failures) != 2 {
		t.Fatalf("len = %d, want 2 — the 40-minute-old failure is outside the window", len(failures))
	}

	if err := attempts.ClearFailures(ctx, h.ID); err != nil {
		t.Fatalf("ClearFailures: %v", err)
	}
	failures, _ = attempts.FailuresSince(ctx, h.ID, base.Add(-time.Hour))
	if len(failures) != 0 {
		t.Fatalf("len = %d, want 0 after clearing", len(failures))
	}
}

func TestSpaceRepoListsInPositionOrder(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	h, _ := postgres.NewHouseholdRepo(db).Create(ctx, "H", "H")
	spaces := postgres.NewSpaceRepo(db)

	for _, s := range domain.BuiltinSpaces(h.ID) {
		if _, err := spaces.Create(ctx, s); err != nil {
			t.Fatalf("Create %s: %v", s.Key, err)
		}
	}

	listed, err := spaces.List(ctx, h.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 3 || listed[0].Key != "money" || listed[2].Key != "family" {
		t.Fatalf("listed = %+v", listed)
	}

	next, err := spaces.NextPosition(ctx, h.ID)
	if err != nil {
		t.Fatalf("NextPosition: %v", err)
	}
	if next != 4 {
		t.Fatalf("NextPosition = %d, want 4", next)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd api && go test ./internal/adapter/postgres/... -run Repo`
Expected: FAIL — undefined constructors.

- [ ] **Step 3: Implement the user and household repositories**

Create `api/internal/adapter/postgres/user_repo.go`:

```go
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type UserRepo struct{ q *sqlcgen.Queries }

func NewUserRepo(db *DB) *UserRepo { return &UserRepo{q: sqlcgen.New(db.Pool())} }

func (r *UserRepo) ByEmail(ctx context.Context, email string) (usecase.StoredUser, error) {
	row, err := r.q.GetUserByEmail(ctx, text(email))
	if err != nil {
		return usecase.StoredUser{}, translate(err, "get user by email")
	}
	return toStoredUser(row.ID, row.Email, row.PasswordHash, row.DisplayName, row.AvatarInitial), nil
}

func (r *UserRepo) ByID(ctx context.Context, id string) (usecase.StoredUser, error) {
	row, err := r.q.GetUserByID(ctx, uuid(id))
	if err != nil {
		return usecase.StoredUser{}, translate(err, "get user by id")
	}
	return toStoredUser(row.ID, row.Email, row.PasswordHash, row.DisplayName, row.AvatarInitial), nil
}

func (r *UserRepo) Create(ctx context.Context, email, passwordHash, displayName string) (domain.User, error) {
	row, err := r.q.CreateUser(ctx, sqlcgen.CreateUserParams{
		Email:         nullableText(email),
		PasswordHash:  nullableText(passwordHash),
		DisplayName:   displayName,
		AvatarInitial: initialOf(displayName),
	})
	if err != nil {
		return domain.User{}, translate(err, "create user")
	}
	return toStoredUser(row.ID, row.Email, row.PasswordHash, row.DisplayName, row.AvatarInitial).User, nil
}

func (r *UserRepo) SetPasswordHash(ctx context.Context, userID, hash string) error {
	return translate(r.q.SetPasswordHash(ctx, sqlcgen.SetPasswordHashParams{
		ID: uuid(userID), PasswordHash: nullableText(hash),
	}), "set password hash")
}

func initialOf(displayName string) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		return "?"
	}
	return strings.ToUpper(name[:1])
}

// translate converts driver errors into domain errors so nothing above the
// adapter layer ever sees pgx types.
func translate(err error, op string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return domain.ErrNotFound
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
```

Write `household_repo.go`, `membership_repo.go`, `session_repo.go`, `magiclink_repo.go`, `loginattempt_repo.go`, `invite_repo.go`, `space_repo.go` and `notification_repo.go` in the same shape: a struct holding `*sqlcgen.Queries`, a `New…Repo(db *DB)` constructor, one method per port method, every returned error passed through `translate`, and every `domain` value converted at the boundary rather than leaking a `sqlcgen` type upward.

The helper functions `text`, `nullableText`, `uuid`, `toStoredUser`, and the `domain.Capabilities` conversion belong in a shared `api/internal/adapter/postgres/convert.go`; write them once and use them from every repository. The exact `pgtype` wrappers depend on what `sqlc generate` produced in Task 9 — read `sqlcgen/models.go` before writing them.

- [ ] **Step 4: Run the repository tests**

Run: `cd api && go test ./internal/adapter/postgres/... -v`
Expected: PASS, every test including the schema tests from Task 9.

- [ ] **Step 5: Verify the architecture lint still passes**

Run: `make lint-arch`
Expected: `architecture lint passed`.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/postgres
git commit -m "feat: implement the Postgres repositories behind the usecase ports"
```

---

### Task 12: Sign-in, sign-out and the lockout, end to end

**Files:**
- Create: `api/internal/usecase/auth.go`, `api/internal/usecase/auth_test.go`, `api/internal/usecase/testdouble_test.go`
- Modify: `api/internal/adapter/http/router.go`

**Interfaces:**
- Consumes: every port from Task 10, `domain.DefaultLockoutPolicy` from Task 7.
- Produces:
  - `usecase.AuthService` with `NewAuthService(deps AuthDeps) *AuthService`
  - `AuthDeps{Users UserRepository; Members MembershipRepository; Sessions SessionRepository; Attempts LoginAttemptRepository; MagicLinks MagicLinkRepository; Mailer Mailer; Hasher PasswordHasher; Tokens TokenGenerator; Clock Clock; Policy domain.LockoutPolicy; SessionTTL time.Duration; BaseURL string}`
  - `(*AuthService).SignIn(ctx, email, password string) (SignInResult, error)` where `SignInResult{SessionToken string; ExpiresAt time.Time; UserID, HouseholdID string}`
  - `(*AuthService).SignOut(ctx, sessionToken string) error`
  - `SignInFailure{AttemptsRemaining int; LockedUntil time.Time}` returned as a typed error `*SignInFailedError` so the handler can render the design's copy

- [ ] **Step 1: Write the in-memory test doubles**

Create `api/internal/usecase/testdouble_test.go` implementing `UserRepository`, `MembershipRepository`, `SessionRepository`, `LoginAttemptRepository`, `MagicLinkRepository` and `Mailer` over maps and slices, plus:

```go
type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time  { return c.now }
func (c *fixedClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

type fakeHasher struct{}

func (fakeHasher) Hash(plain string) (string, error) { return "hashed:" + plain, nil }
func (fakeHasher) Verify(plain, encoded string) bool { return encoded == "hashed:"+plain }

type seqTokens struct{ n int }

func (t *seqTokens) NewToken() (string, []byte, error) {
	t.n++
	raw := fmt.Sprintf("token-%d", t.n)
	return raw, t.HashToken(raw), nil
}
func (t *seqTokens) HashToken(raw string) []byte { return []byte("hash:" + raw) }
```

The doubles are deliberately dumb: they store what they are given and return it. Any logic in a double is logic not being tested.

- [ ] **Step 2: Write the failing sign-in tests**

Create `api/internal/usecase/auth_test.go`:

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

func TestSignInWithTheCorrectPasswordCreatesASession(t *testing.T) {
	f := newFixture(t)

	result, err := f.auth.SignIn(context.Background(), "andreas@hearth.family", "hunter2")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if result.SessionToken == "" {
		t.Fatal("expected a session token")
	}
	if result.HouseholdID != f.householdID {
		t.Fatalf("HouseholdID = %q, want %q", result.HouseholdID, f.householdID)
	}
	if got := f.sessions.count(); got != 1 {
		t.Fatalf("sessions = %d, want 1", got)
	}
}

func TestSignInWithAWrongPasswordReportsTwoTriesLeft(t *testing.T) {
	f := newFixture(t)

	_, err := f.auth.SignIn(context.Background(), "andreas@hearth.family", "wrong")

	var failure *usecase.SignInFailedError
	if !errors.As(err, &failure) {
		t.Fatalf("err = %v, want *SignInFailedError", err)
	}
	if failure.AttemptsRemaining != 2 {
		t.Fatalf("AttemptsRemaining = %d, want 2", failure.AttemptsRemaining)
	}
	if failure.Locked {
		t.Fatal("one failure must not lock")
	}
}

func TestThreeWrongPasswordsLockTheHousehold(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		f.auth.SignIn(ctx, "andreas@hearth.family", "wrong")
		f.clock.Advance(time.Second)
	}

	_, err := f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2")

	var failure *usecase.SignInFailedError
	if !errors.As(err, &failure) {
		t.Fatalf("err = %v, want *SignInFailedError", err)
	}
	if !failure.Locked {
		t.Fatal("expected the household to be locked")
	}
	if failure.LockedUntil.IsZero() {
		t.Fatal("expected LockedUntil to be set")
	}
}

func TestTheLockLiftsAfterFifteenMinutes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		f.auth.SignIn(ctx, "andreas@hearth.family", "wrong")
		f.clock.Advance(time.Second)
	}
	f.clock.Advance(16 * time.Minute)

	if _, err := f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2"); err != nil {
		t.Fatalf("SignIn after the lock expired: %v", err)
	}
}

func TestASuccessfulSignInClearsTheFailureCount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.auth.SignIn(ctx, "andreas@hearth.family", "wrong")
	f.clock.Advance(time.Second)
	f.auth.SignIn(ctx, "andreas@hearth.family", "wrong")
	f.clock.Advance(time.Second)

	if _, err := f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2"); err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	_, err := f.auth.SignIn(ctx, "andreas@hearth.family", "wrong")
	var failure *usecase.SignInFailedError
	errors.As(err, &failure)
	if failure.AttemptsRemaining != 2 {
		t.Fatalf("AttemptsRemaining = %d, want 2 — success resets the counter", failure.AttemptsRemaining)
	}
}

func TestAnUnknownEmailFailsIdenticallyToAWrongPassword(t *testing.T) {
	f := newFixture(t)

	_, unknownErr := f.auth.SignIn(context.Background(), "stranger@example.com", "whatever")
	_, wrongErr := f.auth.SignIn(context.Background(), "andreas@hearth.family", "wrong")

	var a, b *usecase.SignInFailedError
	if !errors.As(unknownErr, &a) || !errors.As(wrongErr, &b) {
		t.Fatalf("both must be *SignInFailedError: %v / %v", unknownErr, wrongErr)
	}
	if a.Locked != b.Locked {
		t.Fatal("the two failures must be indistinguishable to a caller")
	}
	if a.AttemptsRemaining != b.AttemptsRemaining {
		t.Fatalf("AttemptsRemaining differs: unknown = %d, wrong password = %d — "+
			"the countdown itself must not reveal whether the address exists",
			a.AttemptsRemaining, b.AttemptsRemaining)
	}
}

func TestAnUnknownEmailNeverLocksARealHousehold(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		f.auth.SignIn(ctx, "stranger@example.com", "whatever")
		f.clock.Advance(time.Second)
	}

	if _, err := f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2"); err != nil {
		t.Fatalf("attempts against an unknown address must not lock the household: %v", err)
	}
}

func TestSignOutRevokesTheSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	result, err := f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	if err := f.auth.SignOut(ctx, result.SessionToken); err != nil {
		t.Fatalf("SignOut: %v", err)
	}
	if f.sessions.live() != 0 {
		t.Fatal("expected the session to be revoked")
	}
}

func TestUsersWithoutAPasswordCannotSignIn(t *testing.T) {
	f := newFixture(t)

	_, err := f.auth.SignIn(context.Background(), "ethan@hearth.family", "")

	var failure *usecase.SignInFailedError
	if !errors.As(err, &failure) {
		t.Fatalf("err = %v, want *SignInFailedError — a credential-less member must not sign in", err)
	}
}

var _ = domain.DefaultLockoutPolicy
```

`newFixture(t *testing.T) *fixture` is defined in `testdouble_test.go` from Step 1, not here — both files are in `package usecase_test`, so they share it. It builds an `AuthService` over the in-memory doubles containing the design's household: Andreas with password `hunter2`, Ethan with no password at all, both members of one household, and a `fixedClock` starting at `2026-07-18T09:41:00Z`. The `fixture` struct exposes `auth *usecase.AuthService`, `clock *fixedClock`, `sessions *sessionDouble`, `mailer *mailerDouble` and `householdID string`. The session double needs `count() int` (rows ever created) and `live() int` (rows not revoked); the mailer double needs `magicLinks []sentMail` and `lastMagicToken(t *testing.T) string`, which extracts the `token` query parameter from the most recent URL and fails the test if none was sent.

- [ ] **Step 3: Run them and watch them fail**

Run: `cd api && go test ./internal/usecase/...`
Expected: FAIL — undefined `usecase.SignInFailedError` and `AuthService`.

- [ ] **Step 4: Implement the auth service**

Create `api/internal/usecase/auth.go`:

```go
package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// SignInFailedError is the only failure a caller sees, whether the address was
// unknown, the password wrong, or the household locked. The fields drive the
// design's copy; they never reveal whether the address exists.
type SignInFailedError struct {
	AttemptsRemaining int
	Locked            bool
	LockedUntil       time.Time
}

func (e *SignInFailedError) Error() string { return "sign in failed" }
func (e *SignInFailedError) Unwrap() error {
	if e.Locked {
		return domain.ErrHouseholdLocked
	}
	return domain.ErrInvalidCredentials
}

type AuthDeps struct {
	Users      UserRepository
	Members    MembershipRepository
	Sessions   SessionRepository
	Attempts   LoginAttemptRepository
	MagicLinks MagicLinkRepository
	Mailer     Mailer
	Hasher     PasswordHasher
	Tokens     TokenGenerator
	Clock      Clock
	Policy     domain.LockoutPolicy
	SessionTTL time.Duration
	BaseURL    string
}

type AuthService struct{ d AuthDeps }

// NewAuthService fills in a zero-valued Policy. A LockoutPolicy{} never locks
// anyone out while reporting AttemptsRemaining as 0 — an inconsistent state
// that would silently disable the lockout while the UI showed "0 tries left".
// A struct literal that forgets the field is the obvious way to reach it.
func NewAuthService(d AuthDeps) *AuthService {
	if d.Policy.MaxAttempts == 0 {
		d.Policy = domain.DefaultLockoutPolicy()
	}
	return &AuthService{d: d}
}

type SignInResult struct {
	SessionToken string
	ExpiresAt    time.Time
	UserID       string
	HouseholdID  string
}

func (s *AuthService) SignIn(ctx context.Context, email, password string) (SignInResult, error) {
	now := s.d.Clock.Now()

	user, err := s.d.Users.ByEmail(ctx, email)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			return SignInResult{}, err
		}
		// Record the attempt with no household, so guessing at unknown addresses
		// cannot lock a real household. Then evaluate the same policy over that
		// address's own failures, so the countdown a stranger sees is
		// indistinguishable from the one a member sees.
		if err := s.d.Attempts.Record(ctx, nil, nil, email, false, now); err != nil {
			return SignInResult{}, err
		}
		failures, err := s.d.Attempts.FailuresSinceForEmail(ctx, email, now.Add(-s.d.Policy.Window))
		if err != nil {
			return SignInResult{}, err
		}
		state := s.d.Policy.Evaluate(failures, now)
		return SignInResult{}, &SignInFailedError{
			AttemptsRemaining: state.AttemptsRemaining,
			Locked:            state.Locked,
			LockedUntil:       state.Until,
		}
	}

	membership, err := s.d.Members.ByUser(ctx, user.ID)
	if err != nil {
		return SignInResult{}, err
	}
	householdID := membership.HouseholdID

	failures, err := s.d.Attempts.FailuresSince(ctx, householdID, now.Add(-s.d.Policy.Window))
	if err != nil {
		return SignInResult{}, err
	}
	if state := s.d.Policy.Evaluate(failures, now); state.Locked {
		return SignInResult{}, &SignInFailedError{Locked: true, LockedUntil: state.Until}
	}

	if user.PasswordHash == "" || !s.d.Hasher.Verify(password, user.PasswordHash) {
		if err := s.d.Attempts.Record(ctx, &householdID, &user.ID, email, false, now); err != nil {
			return SignInResult{}, err
		}
		failures, err := s.d.Attempts.FailuresSince(ctx, householdID, now.Add(-s.d.Policy.Window))
		if err != nil {
			return SignInResult{}, err
		}
		state := s.d.Policy.Evaluate(failures, now)
		return SignInResult{}, &SignInFailedError{
			AttemptsRemaining: state.AttemptsRemaining,
			Locked:            state.Locked,
			LockedUntil:       state.Until,
		}
	}

	if err := s.d.Attempts.ClearFailures(ctx, householdID); err != nil {
		return SignInResult{}, err
	}
	if err := s.d.Attempts.Record(ctx, &householdID, &user.ID, email, true, now); err != nil {
		return SignInResult{}, err
	}
	return s.issueSession(ctx, user.ID, householdID, now)
}

func (s *AuthService) issueSession(ctx context.Context, userID, householdID string, now time.Time) (SignInResult, error) {
	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return SignInResult{}, fmt.Errorf("generate session token: %w", err)
	}
	expiresAt := now.Add(s.d.SessionTTL)
	if err := s.d.Sessions.Create(ctx, hash, userID, householdID, expiresAt); err != nil {
		return SignInResult{}, err
	}
	return SignInResult{SessionToken: raw, ExpiresAt: expiresAt, UserID: userID, HouseholdID: householdID}, nil
}

func (s *AuthService) SignOut(ctx context.Context, sessionToken string) error {
	return s.d.Sessions.RevokeByToken(ctx, s.d.Tokens.HashToken(sessionToken))
}
```

- [ ] **Step 5: Run the auth tests**

Run: `cd api && go test ./internal/usecase/... -v`
Expected: PASS, nine tests.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase
git commit -m "feat: add sign-in, sign-out and the household lockout"
```

---

### Task 13: Magic link

**Files:**
- Modify: `api/internal/usecase/auth.go`, `api/internal/usecase/auth_test.go`
- Create: `api/internal/adapter/mail/smtp.go`

**Interfaces:**
- Consumes: `MagicLinkRepository`, `Mailer` from Task 10.
- Produces:
  - `(*AuthService).RequestMagicLink(ctx, email string) error` — always returns nil for an unknown address
  - `(*AuthService).ConsumeMagicLink(ctx, token string) (SignInResult, error)`
  - `mail.NewSMTPMailer(addr, from, baseURL string) *SMTPMailer`

- [ ] **Step 1: Write the failing tests**

Append to `api/internal/usecase/auth_test.go`:

```go
func TestRequestMagicLinkSendsAnEmail(t *testing.T) {
	f := newFixture(t)

	if err := f.auth.RequestMagicLink(context.Background(), "andreas@hearth.family"); err != nil {
		t.Fatalf("RequestMagicLink: %v", err)
	}
	if len(f.mailer.magicLinks) != 1 {
		t.Fatalf("sent = %d, want 1", len(f.mailer.magicLinks))
	}
	if !strings.HasPrefix(f.mailer.magicLinks[0].url, "http://localhost:5173/sign-in/magic?token=") {
		t.Fatalf("url = %q", f.mailer.magicLinks[0].url)
	}
}

func TestRequestMagicLinkStaysSilentForAnUnknownAddress(t *testing.T) {
	f := newFixture(t)

	if err := f.auth.RequestMagicLink(context.Background(), "stranger@example.com"); err != nil {
		t.Fatalf("an unknown address must not produce an error: %v", err)
	}
	if len(f.mailer.magicLinks) != 0 {
		t.Fatal("no email should have been sent")
	}
}

func TestMagicLinkIsRateLimitedSilentlyToThreePerHour(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := f.auth.RequestMagicLink(ctx, "andreas@hearth.family"); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		f.clock.Advance(time.Minute)
	}

	// The fourth request must look exactly like the first three from the
	// outside. Returning an error here would make four requests an oracle for
	// whether the address belongs to a member, which is the property this
	// endpoint exists to avoid.
	if err := f.auth.RequestMagicLink(ctx, "andreas@hearth.family"); err != nil {
		t.Fatalf("the rate limit must be silent, got err = %v", err)
	}
	if len(f.mailer.magicLinks) != 3 {
		t.Fatalf("sent = %d, want 3 — the fourth request sends nothing", len(f.mailer.magicLinks))
	}
}

func TestMagicLinkRequestsAreIndistinguishableAtEveryCount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		knownErr := f.auth.RequestMagicLink(ctx, "andreas@hearth.family")
		unknownErr := f.auth.RequestMagicLink(ctx, "stranger@example.com")
		if knownErr != nil || unknownErr != nil {
			t.Fatalf("request %d: known = %v, unknown = %v — both must be nil", i, knownErr, unknownErr)
		}
		f.clock.Advance(time.Minute)
	}
}

func TestConsumingAMagicLinkSignsIn(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.auth.RequestMagicLink(ctx, "andreas@hearth.family"); err != nil {
		t.Fatalf("RequestMagicLink: %v", err)
	}
	token := f.mailer.lastMagicToken(t)

	result, err := f.auth.ConsumeMagicLink(ctx, token)
	if err != nil {
		t.Fatalf("ConsumeMagicLink: %v", err)
	}
	if result.SessionToken == "" {
		t.Fatal("expected a session")
	}
}

func TestAMagicLinkCannotBeUsedTwice(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.auth.RequestMagicLink(ctx, "andreas@hearth.family")
	token := f.mailer.lastMagicToken(t)
	f.auth.ConsumeMagicLink(ctx, token)

	if _, err := f.auth.ConsumeMagicLink(ctx, token); !errors.Is(err, domain.ErrTokenExpired) {
		t.Fatalf("err = %v, want ErrTokenExpired", err)
	}
}

func TestAMagicLinkExpiresAfterFifteenMinutes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.auth.RequestMagicLink(ctx, "andreas@hearth.family")
	token := f.mailer.lastMagicToken(t)
	f.clock.Advance(16 * time.Minute)

	if _, err := f.auth.ConsumeMagicLink(ctx, token); !errors.Is(err, domain.ErrTokenExpired) {
		t.Fatalf("err = %v, want ErrTokenExpired", err)
	}
}

func TestAMagicLinkWorksWhileTheHouseholdIsLocked(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		f.auth.SignIn(ctx, "andreas@hearth.family", "wrong")
		f.clock.Advance(time.Second)
	}
	if _, err := f.auth.SignIn(ctx, "andreas@hearth.family", "hunter2"); err == nil {
		t.Fatal("expected the household to be locked")
	}

	if err := f.auth.RequestMagicLink(ctx, "andreas@hearth.family"); err != nil {
		t.Fatalf("magic link must remain available while locked: %v", err)
	}
	if _, err := f.auth.ConsumeMagicLink(ctx, f.mailer.lastMagicToken(t)); err != nil {
		t.Fatalf("consuming a magic link while locked must succeed: %v", err)
	}
}
```

Add `"strings"` to the test file's imports.

- [ ] **Step 2: Run them and watch them fail**

Run: `cd api && go test ./internal/usecase/... -run Magic`
Expected: FAIL — undefined `RequestMagicLink`.

- [ ] **Step 3: Implement magic link**

Append to `api/internal/usecase/auth.go`:

```go
const (
	magicLinkTTL          = 15 * time.Minute
	magicLinkPerHourLimit = 3
)

// RequestMagicLink is deliberately quiet. Neither an unknown address nor an
// exhausted rate limit produces an error, because any observable difference
// between the two would let a caller discover who is a member.
func (s *AuthService) RequestMagicLink(ctx context.Context, email string) error {
	now := s.d.Clock.Now()

	count, err := s.d.MagicLinks.CountSince(ctx, email, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	if count >= magicLinkPerHourLimit {
		slog.Info("magic link rate limit reached", "email_hash", fmt.Sprintf("%x", s.d.Tokens.HashToken(email))[:12])
		return nil
	}

	user, err := s.d.Users.ByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}

	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return fmt.Errorf("generate magic link token: %w", err)
	}
	if err := s.d.MagicLinks.Create(ctx, user.ID, hash, now.Add(magicLinkTTL)); err != nil {
		return err
	}

	url := fmt.Sprintf("%s/sign-in/magic?token=%s", s.d.BaseURL, raw)
	return s.d.Mailer.SendMagicLink(ctx, user.Email, user.DisplayName, url)
}

// ConsumeMagicLink signs the holder in. It is not gated by the household lock:
// the lock exists to stop password guessing, and this is the recovery path.
func (s *AuthService) ConsumeMagicLink(ctx context.Context, token string) (SignInResult, error) {
	now := s.d.Clock.Now()

	userID, err := s.d.MagicLinks.Consume(ctx, s.d.Tokens.HashToken(token))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return SignInResult{}, domain.ErrTokenExpired
		}
		return SignInResult{}, err
	}

	membership, err := s.d.Members.ByUser(ctx, userID)
	if err != nil {
		return SignInResult{}, err
	}
	return s.issueSession(ctx, userID, membership.HouseholdID, now)
}
```

Add `"log/slog"` to the imports. `domain.ErrRateLimited` is no longer returned from this path — it stays defined and mapped to `429 RATE_LIMITED` in Task 16 for endpoints where an explicit rejection is safe, which magic link is not.

The in-memory `MagicLinkRepository` double must honour `expires_at` against the fixed clock; the Postgres implementation already does through `expires_at > now()` in the `ConsumeMagicLink` query.

- [ ] **Step 4: Implement the SMTP mailer**

```bash
cd api && go get github.com/wneessen/go-mail@latest
```

Create `api/internal/adapter/mail/smtp.go` with `SMTPMailer` implementing both methods. Message bodies are plain text using the design's voice — the magic-link email subject is `Your Hearth sign-in link`, the invite subject is `<InviterName> invited you to Hearth`. In development it points at Mailpit on `SMTP_ADDR`, with no authentication and no TLS.

- [ ] **Step 5: Run the tests**

Run: `cd api && go test ./internal/usecase/... -v && go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase api/internal/adapter/mail
git commit -m "feat: add magic-link sign-in with rate limiting"
```

---

### Task 14: Invites

**Files:**
- Create: `api/internal/usecase/invite.go`, `api/internal/usecase/invite_test.go`

**Interfaces:**
- Consumes: `InviteRepository`, `UserRepository`, `MembershipRepository`, `Mailer`, `TokenGenerator`, `Clock`.
- Produces:
  - `usecase.InviteService`, `NewInviteService(d InviteDeps) *InviteService`
  - `(*InviteService).Create(ctx, householdID, invitedByUserID, name, email string, role domain.Role, caps domain.Capabilities) error`
  - `(*InviteService).Preview(ctx, token string) (InvitePreview, error)` where `InvitePreview{FamilyName, InviterName, Name string; Role domain.Role; Capabilities domain.Capabilities}`
  - `(*InviteService).Accept(ctx, token, password, displayName string) (SignInResult, error)`

- [ ] **Step 1: Write the failing tests**

Create `api/internal/usecase/invite_test.go` covering, each as its own test function with the assertion style used in Task 12:

1. `Create` with an email sends exactly one invite email whose URL is `http://localhost:5173/invite/<token>`.
2. `Create` for a `limited` member with no email address creates no invite and sends nothing, but does create the user and membership — matching the spec's rule for children.
3. `Create` rejects a `limited` role carrying `marriage`, returning `domain.ErrLimitedCannotHoldMarriage`.
4. `Preview` returns the family name, the inviter's display name, the role and the capabilities.
5. `Preview` on an unknown token returns `domain.ErrNotFound`.
6. `Preview` on an expired invite returns `domain.ErrInviteExpired`.
7. `Preview` on an accepted invite returns `domain.ErrInviteAlreadyAccepted`.
8. `Accept` creates the user, creates the membership with the invited role and capabilities, marks the invite accepted, and returns a live session.
9. `Accept` twice with the same token fails the second time with `domain.ErrInviteAlreadyAccepted`.
10. `Accept` with a password shorter than 12 characters returns an error and creates nothing.

- [ ] **Step 2: Run them and watch them fail**

Run: `cd api && go test ./internal/usecase/... -run Invite`
Expected: FAIL — undefined `usecase.InviteService`.

- [ ] **Step 3: Implement the invite service**

Create `api/internal/usecase/invite.go`. Rules to encode:

- Invite lifetime is 7 days from `Clock.Now()`.
- `Create` builds the `domain.Membership` through `domain.NewMembership` *before* any write, so an invalid capability set is rejected without touching the database.
- A `limited` member with no email is created directly as a user and membership with a null password, and no invite row is written.
- `Accept` requires a password of at least 12 characters, hashes it with `PasswordHasher`, creates the user, creates the membership from the invite's role and capabilities, marks the invite accepted, then issues a session through the same `issueSession` path sign-in uses.
- `Preview` and `Accept` both check expiry and prior acceptance, returning the specific domain error for each.

- [ ] **Step 4: Run the tests**

Run: `cd api && go test ./internal/usecase/... -v`
Expected: PASS, every test.

- [ ] **Step 5: Commit**

```bash
git add api/internal/usecase
git commit -m "feat: add invite creation, preview and acceptance"
```

---

### Task 15: Members, household settings, spaces and notifications

**Files:**
- Create: `api/internal/usecase/member.go`, `api/internal/usecase/household.go`, `api/internal/usecase/member_test.go`, `api/internal/usecase/household_test.go`

**Interfaces:**
- Consumes: `MembershipRepository`, `SessionRepository`, `HouseholdRepository`, `SpaceRepository`, `NotificationRepository`.
- Produces:
  - `usecase.MemberService` with `List(ctx, householdID) ([]MemberView, error)`, `Update(ctx, householdID, membershipID string, role domain.Role, caps domain.Capabilities) error`, `Remove(ctx, householdID, membershipID string) error`
  - `usecase.HouseholdService` with `Get`, `Update`, `Spaces(ctx, householdID string, m domain.Membership) ([]domain.Space, error)`, `CreateSpace(ctx, householdID, name string, visibility domain.Visibility) (domain.Space, error)`, `Notifications`, `UpdateNotifications`

- [ ] **Step 1: Write the failing tests**

Create the two test files covering:

1. `Update` demoting the only owner returns `domain.ErrLastOwner` and writes nothing.
2. `Update` granting `marriage` to a `limited` member returns `domain.ErrLimitedCannotHoldMarriage`.
2b. `Update` promoting a member to `owner` with a partial capability set returns `domain.ErrOwnerMustHoldAllCapabilities`.
2c. `Update` on a membership ID that does not exist in the household returns `domain.ErrNotFound`.
2d. `Update` changing only the capabilities of a `limited` member succeeds in a household with no other owner — the last-owner rule must not be consulted when ownership is not at stake.
3. `Update` succeeding revokes that member's sessions — assert the session double is empty afterwards, because a capability change must not remain effective in an open tab.
4. `Remove` of the only owner returns `domain.ErrLastOwner`.
5. `Remove` succeeding deletes the membership and revokes that user's sessions.
6. `Spaces` for a limited member with only `calendar` returns exactly the Family space.
7. `CreateSpace` assigns the next position and is not builtin.
7b. `CreateSpace` accepts only `everyone` and `parents_only`; `custom` is rejected until per-space member lists exist.
8. `CreateSpace` rejects a duplicate name within the household.
9. `Update` on the household normalises the primary currency to uppercase and rejects a currency that is not three letters.
10. `UpdateNotifications` round-trips all four flags.

- [ ] **Step 2: Run them and watch them fail**

Run: `cd api && go test ./internal/usecase/... -run 'Member|Household'`
Expected: FAIL — undefined services.

- [ ] **Step 3: Implement both services**

Create `api/internal/usecase/member.go` and `api/internal/usecase/household.go`. Every mutation loads the full membership list first and validates through `domain.ValidateMembershipChange` or `domain.ValidateMembershipRemoval` — the rule lives in the domain, and the service's only job is to fetch the facts it needs and act on the verdict. Successful member mutations call `SessionRepository.RevokeAllForUser`.

`CreateSpace` derives the key by lowercasing the name and replacing spaces with hyphens, calls `SpaceRepository.NextPosition`, and sets `IsBuiltin: false` with an empty `RequiredCapability`.

- [ ] **Step 4: Run the tests**

Run: `cd api && go test ./internal/usecase/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/internal/usecase
git commit -m "feat: add member, space and household settings services"
```

---

### Task 16: HTTP middleware, handlers and the authorisation matrix

**Files:**
- Create: `api/internal/adapter/http/middleware_session.go`, `middleware_csrf.go`, `auth_handlers.go`, `invite_handlers.go`, `member_handlers.go`, `household_handlers.go`, `errors.go`
- Modify: `api/internal/adapter/http/router.go`, `api/cmd/api/main.go`
- Test: `api/internal/adapter/http/api_test.go`

**Interfaces:**
- Consumes: every service from Tasks 12–15.
- Produces:
  - `httpadapter.Deps` gains `Auth *usecase.AuthService`, `Invites *usecase.InviteService`, `Members *usecase.MemberService`, `Households *usecase.HouseholdService`, `Sessions usecase.SessionRepository`, `Tokens usecase.TokenGenerator`, `Clock usecase.Clock`, `Secure bool`
  - `httpadapter.RequestScope(r *http.Request) (Scope, bool)` where `Scope{UserID, HouseholdID string; Membership domain.Membership}`
  - `httpadapter.MapDomainError(w http.ResponseWriter, err error)` — the single table mapping domain errors to codes and statuses

- [ ] **Step 1: Write the failing API test**

Create `api/internal/adapter/http/api_test.go`, running against the full router with a real database from `testsupport.StartPostgres`. Cover:

1. `POST /api/v1/auth/sign-in` with the correct password returns 200 and sets `hearth_session` with `HttpOnly` and `SameSite=Lax`, plus a `csrf_token` cookie that is **not** `HttpOnly`.
2. The same request with a wrong password returns 401, `code: INVALID_CREDENTIALS`, and `details.attemptsRemaining: 2`.
3. A fourth attempt returns 423 with `code: HOUSEHOLD_LOCKED` and a `details.lockedUntil` timestamp.
4. `GET /api/v1/auth/me` without a cookie returns 401 `UNAUTHENTICATED`.
5. `GET /api/v1/auth/me` with a session returns the user, household, capabilities and visible spaces in one body.
6. A mutating request with a valid session but no `X-CSRF-Token` header returns 403 `CSRF_INVALID`.
7. A mutating request whose header does not match the cookie returns 403 `CSRF_INVALID`.
8. `POST /api/v1/auth/sign-out` revokes the session, after which `/auth/me` returns 401.
9. A table-driven test asserting that **every** route under `/api/v1` except `/auth/sign-in`, `/auth/magic-link`, `/auth/magic-link/consume` and `/invites/*` returns 401 when called without a session.
10. `PATCH /api/v1/household/members/:id` called by a `limited` member returns 403 `FORBIDDEN`.
11. `POST /api/v1/spaces` called by a `limited` member returns 403 `FORBIDDEN`.

- [ ] **Step 2: Run it and watch it fail**

Run: `cd api && go test ./internal/adapter/http/...`
Expected: FAIL — routes do not exist.

- [ ] **Step 3: Implement the error table**

Create `api/internal/adapter/http/errors.go` mapping, in one `switch`: `domain.ErrInvalidCredentials` → 401 `INVALID_CREDENTIALS`; `domain.ErrHouseholdLocked` → 423 `HOUSEHOLD_LOCKED`; `domain.ErrNotFound` → 404 `NOT_FOUND`; `domain.ErrForbidden` → 403 `FORBIDDEN`; `domain.ErrLastOwner` → 409 `LAST_OWNER`; `domain.ErrLimitedCannotHoldMarriage` → 422 `INVALID_CAPABILITIES`; `domain.ErrOwnerMustHoldAllCapabilities` → 422 `INVALID_CAPABILITIES`; `domain.ErrUnknownCapability` → 422 `INVALID_CAPABILITIES`; `domain.ErrUnknownRole` → 422 `INVALID_ROLE`; `domain.ErrAmountOverflow` and `domain.ErrInvalidMoney` → 500 `INTERNAL`, since either reaching the HTTP layer means a calculation is wrong rather than a request being bad; `domain.ErrInviteExpired` → 410 `INVITE_EXPIRED`; `domain.ErrInviteAlreadyAccepted` → 409 `INVITE_ALREADY_ACCEPTED`; `domain.ErrTokenExpired` → 410 `TOKEN_EXPIRED`; `domain.ErrRateLimited` → 429 `RATE_LIMITED`; default → 500 `INTERNAL` with the request ID in `details.requestId` and the real error logged, never returned.

- [ ] **Step 4: Implement the middleware**

`middleware_session.go` reads the `hearth_session` cookie, hashes it through `Tokens.HashToken`, resolves the session, loads the membership, puts a `Scope` in the request context, and extends the session when it is more than a day from expiry. A missing or unresolvable cookie yields 401 `UNAUTHENTICATED`.

`middleware_csrf.go` skips `GET`, `HEAD` and `OPTIONS`; for everything else it compares the `csrf_token` cookie with the `X-CSRF-Token` header using `subtle.ConstantTimeCompare` and rejects a mismatch or an absence with 403 `CSRF_INVALID`.

A `requireCapability(cap domain.Capability)` middleware and a `requireOwner` middleware both read the `Scope` and answer 403 `FORBIDDEN`.

- [ ] **Step 5: Implement the handlers and wire the router**

Every route from the spec, grouped: `/auth/*` public except `/auth/me` and `/auth/sign-out`; `/invites/:token` and `/invites/:token/accept` public; everything else behind session then CSRF, with `/household/members` mutations and `/spaces` creation additionally behind `requireOwner`.

Update `cmd/api/main.go` to construct every repository, service and adapter and pass them in `Deps`. `Secure: !cfg.IsDevelopment()`.

- [ ] **Step 6: Run the API tests**

Run: `cd api && go test ./internal/adapter/http/... -v`
Expected: PASS, every case including the 401 matrix.

- [ ] **Step 7: Verify the architecture lint**

Run: `make lint-arch && make test-api`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add api/
git commit -m "feat: add the HTTP layer with session, CSRF and capability middleware"
```

---

### Task 17: adminctl and the seed

**Files:**
- Create: `api/cmd/adminctl/main.go`, `api/internal/usecase/seed.go`, `api/internal/usecase/seed_test.go`
- Modify: `Makefile`, `docker-compose.yml`

**Interfaces:**
- Consumes: every repository and service.
- Produces:
  - `usecase.Seed(ctx, d SeedDeps) (SeedResult, error)` where `SeedResult{InviteURL string}`
  - `adminctl seed`, `adminctl reset-password --email=…`, `adminctl unlock-household`, `adminctl create-invite --email=… --name=… --role=…`
  - Make targets `seed`, `reset-password`, `unlock-household`

- [ ] **Step 1: Write the failing seed test**

Create `api/internal/usecase/seed_test.go` asserting that after `Seed`:

1. Exactly one household exists, named `Andreas & Christine`.
2. Andreas exists as an owner with all four capabilities and a usable password.
3. A pending invite exists for Christine as an owner, and `SeedResult.InviteURL` contains its token.
4. Kayla exists as `limited` with `calendar` and `chores`; Ethan as `limited` with `calendar`; neither has a password.
5. The three builtin spaces exist with the visibilities from `domain.BuiltinSpaces`.
6. Notification preferences exist with all four flags true.
7. Running `Seed` twice does not duplicate anything — the second call is a no-op that still returns a usable invite URL.

- [ ] **Step 2: Run it and watch it fail**

Run: `cd api && go test ./internal/usecase/... -run Seed`
Expected: FAIL — undefined `usecase.Seed`.

- [ ] **Step 3: Implement the seed**

Create `api/internal/usecase/seed.go`. The development password is the constant `hearth-dev-password`, defined in this file and printed by `adminctl seed` so it is never a secret hidden in a diff.

- [ ] **Step 4: Implement adminctl**

Create `api/cmd/adminctl/main.go` with four subcommands parsed by `flag`. `seed` refuses to run unless `cfg.IsDevelopment()`, exiting 1 with `refusing to seed outside development (APP_ENV=%s)`. `reset-password` prompts for the new password on stdin, hashes it, and calls `UserRepository.SetPasswordHash`. `unlock-household` calls `LoginAttemptRepository.ClearFailures`. `create-invite` calls `InviteService.Create` and prints the URL.

- [ ] **Step 5: Add the Make targets**

```makefile
seed: ## Seed the design's household and print Christine's invite URL
	$(COMPOSE) exec api go run ./cmd/adminctl seed

reset-password: ## Set a member's password. make reset-password EMAIL=you@example.com
	@test -n "$(EMAIL)" || { echo "EMAIL is required"; exit 1; }
	$(COMPOSE) exec api go run ./cmd/adminctl reset-password --email=$(EMAIL)

unlock-household: ## Clear a household lock immediately
	$(COMPOSE) exec api go run ./cmd/adminctl unlock-household
```

Add all three to `.PHONY`.

- [ ] **Step 6: Verify against the running stack**

Run: `make up && make seed`
Expected: prints Andreas's email, the development password, and an invite URL of the form `http://localhost:5173/invite/<token>`.

Run: `make seed` a second time.
Expected: no duplicates, an invite URL still printed.

- [ ] **Step 7: Commit**

```bash
git add api/cmd/adminctl api/internal/usecase Makefile docker-compose.yml
git commit -m "feat: add adminctl with seed, password reset and household unlock"
```

---

### Task 18: The frontend authentication screens

**Files:**
- Create: `web/src/features/auth/schemas.ts`, `useAuth.ts`, `SignInScreen.tsx`, `InviteScreen.tsx`, `MagicLinkSentPanel.tsx`, `SignInScreen.test.tsx`
- Modify: `web/src/App.tsx`, `web/src/main.tsx`

**Interfaces:**
- Consumes: `apiFetch`, `ApiError` from the skeleton plan's Task 5.
- Produces:
  - `meQuerySchema` / `Me` type — `{user, household, membership, capabilities, spaces}`
  - `useMe()` — TanStack Query over `['me']`
  - `useSignIn()`, `useSignOut()`, `useRequestMagicLink()`, `useAcceptInvite()`
  - `<SignInScreen />` handling all four states: default, wrong password, locked, magic-link sent
  - `<InviteScreen token={string} />`

- [ ] **Step 1: Write the failing component test**

Create `web/src/features/auth/SignInScreen.test.tsx` covering:

1. Rendering shows the design's copy: `Welcome back.` and `Sign in to pick up where you both left off.`
2. Submitting valid credentials calls `POST /api/v1/auth/sign-in` once with the entered email and password.
3. A 401 with `attemptsRemaining: 2` renders exactly `That password doesn't match. Two tries left before we lock the household for 15 minutes.`
4. A 401 with `attemptsRemaining: 1` renders `One try left` rather than `1 tries left`.
5. A 423 renders the locked message and disables the submit button.
6. Clicking `Email me a one-time sign-in link` calls `POST /api/v1/auth/magic-link` and then shows the sent confirmation instead of the form.
7. The password field has `type="password"` and `autoComplete="current-password"`.

Mock `fetch` with `vi.stubGlobal` exactly as the API-client test does.

- [ ] **Step 2: Run it and watch it fail**

Run: `cd web && npx vitest run SignInScreen`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement the schemas and hooks**

`schemas.ts` declares zod schemas for the `/auth/me` response and the sign-in request, exporting inferred types. `useAuth.ts` wraps them in TanStack Query hooks; every mutation that changes identity calls `queryClient.invalidateQueries({queryKey: ['me']})`.

- [ ] **Step 4: Implement the screens**

`SignInScreen.tsx` is a single component with a `mode` state of `'password' | 'magic-sent'` and an `error` state derived from the last `ApiError`. Copy comes verbatim from the design document. The attempts message pluralises through a small helper so `1` reads `One try left`.

`InviteScreen.tsx` fetches `GET /api/v1/invites/:token`, renders `Christine invited you in.` with the real inviter name, the shared-spaces sentence, and the `Joining as co-owner` line, then collects a password and posts to accept.

- [ ] **Step 5: Run the tests**

Run: `cd web && npx vitest run`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/auth
git commit -m "feat: add the sign-in, invite and magic-link screens"
```

---

### Task 19: The application shell

**Files:**
- Create: `web/src/features/shell/AppShell.tsx`, `Sidebar.tsx`, `RequireAuth.tsx`, `RequireCapability.tsx`, `Sidebar.test.tsx`, `web/src/components/Modal.tsx`, `web/src/components/Modal.test.tsx`, `web/src/routes/router.tsx`, `web/src/features/placeholder/PlaceholderPage.tsx`
- Modify: `web/src/App.tsx`

**Interfaces:**
- Consumes: `useMe` from Task 18.
- Produces:
  - `<AppShell />` — sidebar, header, outlet
  - `<Modal open onClose title>` — backdrop dismissal, `✕`, focus trap, Escape. Slices 2–4 build every one of their modals on this.
  - `<RequireAuth />`, `<RequireCapability cap="marriage" />`
  - The route tree: `/sign-in`, `/sign-in/magic`, `/invite/$token`, `/` (Overview placeholder), `/money/*`, `/marriage/*`, `/family/calendar`, `/settings`

- [ ] **Step 1: Write the failing tests**

`Sidebar.test.tsx`:

1. Given a `me` payload whose `spaces` contains only Family, the sidebar renders `Family` and does not render `Money` or `Marriage`.
2. Given all three spaces, all three render, in position order.
3. The footer shows the household name and a `Sign out` control.
4. Clicking `Sign out` calls `POST /api/v1/auth/sign-out`.

`Modal.test.tsx`:

1. `open={false}` renders nothing.
2. `open` renders the title and children.
3. Escape calls `onClose`.
4. Clicking the backdrop calls `onClose`; clicking inside the panel does not.
5. Focus moves into the modal on open and returns to the trigger on close.

- [ ] **Step 2: Run them and watch them fail**

Run: `cd web && npx vitest run`
Expected: FAIL — modules not found.

- [ ] **Step 3: Implement the shell**

Build `components/Modal.tsx` on the native `<dialog>` element — it lives in `components/`, not in a feature folder, because slices 2-4 build roughly fifteen modals on it and moving a shared primitive once it has fifteen call sites is expensive so focus trapping and Escape come from the platform rather than from hand-written key handling. `Sidebar.tsx` renders from `me.spaces` — never from a hard-coded list, because that is the property that makes "+ New space" work. `RequireAuth.tsx` redirects to `/sign-in` when `useMe` fails with a 401.

`PlaceholderPage.tsx` takes a `slice` prop and renders the page name with `Arriving in slice N` so an unfinished area is honest rather than broken.

- [ ] **Step 4: Run the tests**

Run: `cd web && npx vitest run`
Expected: PASS.

- [ ] **Step 5: Verify in the browser**

Run: `make dev`, then sign in at `http://localhost:5173` as Andreas with the seeded password.
Expected: the sidebar shows Overview, Money, Marriage, Family and Settings; every non-Settings destination renders its placeholder; `Sign out` returns to the sign-in screen.

- [ ] **Step 6: Commit**

```bash
git add web/src
git commit -m "feat: add the application shell, sidebar and modal primitive"
```

---

### Task 20: The Settings screens

**Files:**
- Create: `web/src/features/settings/SettingsPage.tsx`, `MembersPanel.tsx`, `SpacesPanel.tsx`, `CurrencyPanel.tsx`, `NotificationsPanel.tsx`, `InviteMemberModal.tsx`, `NewSpaceModal.tsx`, `MembersPanel.test.tsx`
- Modify: `web/src/routes/router.tsx`

**Interfaces:**
- Consumes: `components/Modal` from Task 19, the household endpoints from Task 16.
- Produces: the Settings route, complete except the Connected-accounts panel, which belongs to slice 2.

- [ ] **Step 1: Write the failing test**

`MembersPanel.test.tsx`:

1. Renders each member with their role label — `Parent · full access` / `Owner` for adults, `Kid · calendar & chores only` / `Limited` for Kayla, `Kid · calendar only` / `Limited` for Ethan, exactly as the design writes them.
2. Toggling a capability for a kid issues `PATCH /api/v1/household/members/:id` with the new capability array.
3. A `409 LAST_OWNER` response renders the message inline and leaves the toggle in its previous position.
4. The `marriage` capability is not offered for a `limited` member.

- [ ] **Step 2: Run it and watch it fail**

Run: `cd web && npx vitest run MembersPanel`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement the panels**

Four panels inside one page, each owning its own mutation and invalidating `['me']` and its own query on success. `InviteMemberModal` and `NewSpaceModal` are built on the `components/Modal` primitive and mirror the design's fields exactly, including the `Off for kids by default` helper text on the money capability and the space templates Kids, Home, Travel and Blank. The New space modal's visibility choices are `Everyone` and `Parents only`; `Custom` is shown disabled with a note that per-space membership is not built yet, matching the design's own "not built" marker rather than offering a control that silently behaves like `Everyone`.

The plan label reads `Free plan` as static text with no link, per the spec.

- [ ] **Step 4: Run every test**

Run: `make test`
Expected: PASS on both suites.

- [ ] **Step 5: Commit**

```bash
git add web/src
git commit -m "feat: add the Settings screens for members, spaces, currency and notifications"
```

---

### Task 21: Walk the definition of done

**Files:**
- Create: `docs/superpowers/plans/2026-07-26-hearth-identity-verification.md` (the recorded result)

- [ ] **Step 1: Start from nothing**

```bash
make down
docker volume rm hearth_hearth-pgdata || true
make up && make seed
```

- [ ] **Step 2: Walk every criterion and record the result**

Perform each in the browser at `http://localhost:5173`, writing PASS or the failure into the verification file as you go:

1. Open the invite URL printed by `make seed`; accept as Christine with a password of at least 12 characters.
2. Sign out; sign in as Christine with that password.
3. Sign out; enter Andreas's email with a wrong password three times. Confirm the second attempt reads `Two tries left`, the third reads `One try left`, and the fourth returns the locked message.
4. While locked, click `Email me a one-time sign-in link`. Open Mailpit at `http://localhost:8025`, follow the link, and confirm it signs in.
5. Confirm the sidebar shows Overview, Money, Marriage, Family and Settings.
6. In Settings, change the primary currency and toggle every notification; reload and confirm both persisted.
7. In Settings, invite a new member; confirm the email arrives in Mailpit.
8. In Settings, remove Kayla's `chores` capability; confirm it persists after a reload.
9. Attempt to remove Andreas while he is the only owner; confirm the `LAST_OWNER` message appears and nothing changes.
10. Sign out and confirm `/` redirects to `/sign-in`.

- [ ] **Step 3: Run the full gate**

Run: `make lint && make test`
Expected: PASS. Paste the summary lines into the verification file.

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/plans/2026-07-26-hearth-identity-verification.md
git commit -m "docs: record the identity slice verification walkthrough"
```

---

## Definition of done for this plan

Every criterion in the spec's own definition of done passes, recorded in the verification file, with `make lint` and `make test` green. Slice 2 (Money) is then specified separately, starting from the derived-figure formulas the spec's closing section lists.
