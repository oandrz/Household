# Hearth Self-Serve Sign-Up Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A stranger arrives at the app, verifies their email address, creates their own household, and becomes its first owner — without the endpoint revealing which addresses are already registered.

**Architecture:** A `signups` table holds a hashed, single-use, 24-hour token against an email address and nothing else. `POST /auth/sign-up` answers identically for every address and mails either a create-household link or a "you already have an account" note. Clicking the link reaches a form whose submission runs one Postgres transaction that creates the household, the owner, their membership, the builtin spaces and the notification preferences together — then signs them in through the same `issueSession` that sign-in and invite acceptance use.

**Tech Stack:** Everything from the identity plan. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-07-27-hearth-self-serve-signup-design.md`

**Prerequisite:** `docs/superpowers/plans/2026-07-26-hearth-identity.md` complete and green. Task numbering continues from it (that plan ended at Task 21).

## Global Constraints

- **`internal/domain` may import the standard library only.** `make lint-arch` enforces it, test files included. `time` is stdlib, so `domain.TokenLifecycle` taking `time.Time` is legal; `golang.org/x/text` is not, anywhere in domain or usecase.
- **Only hashes are stored.** A raw sign-up token exists in memory and in an email, never in a row. `signups.token_hash` is `bytea NOT NULL UNIQUE`.
- **`POST /auth/sign-up` answers `202 {"status":"accepted"}` for every input** — fresh address, registered address, rate-limited address, mailer down. It never returns an error. This matches `handleRequestMagicLink` exactly.
- **`usecase.ErrInviteeAlreadyRegistered` must never be returned by any sign-up path.** It maps to `409 EMAIL_ALREADY_REGISTERED` (`adapter/http/errors.go:153`), which is the enumeration oracle this whole design exists to prevent.
- **Every 2xx except `204` carries a JSON body.** `apiFetch` throws `INVALID_RESPONSE` on an ok response it cannot parse.
- **Password floor is 12, ceiling 256.** The design document's create card says "At least 10 characters"; that copy changes to "At least 12 characters". Do not lower the floor — `MapDomainError` already answers "Password must be at least 12 characters" and two floors across the two account-creation paths is how this becomes a defect.
- **Sign-up token TTL is 24 hours.** Magic link's 15 minutes is too short, the invite's 7 days too long for an unverified address.
- **Per-address sign-up rate limit is 3 per hour**, matching `magicLinkPerHourLimit`.
- **All user-visible copy comes verbatim from `design/Household Dashboard.dc.html`** (synced 2026-07-27, 1314 lines). The create-card strings are: `Start your household.` / `One household, two owners. Set it up once and invite your partner in.` / `Household name` / `Shown at the top of the sidebar. Change it any time.` / `Your name` / `Create household` / `No household yet? ` + `Create one` / `Already set up? ` + `Sign in` / `You can invite your partner right after — nothing is shared until they accept.`
- **Sign-up screens hide `Forgot?`, the `or` divider and the magic-link button.** The design gates all three on `authNotCreate`.
- Time enters through the `Clock` port, randomness through `TokenGenerator`. Tests never sleep.
- Every task ends with a commit.

## Scope note: currencies with non-2 minor units

`domain.Money.String()` is `fmt.Sprintf("%s%s %d.%02d", sign, m.Currency, magnitude/100, magnitude%100)` (`domain/money.go:58`) — two minor digits, hard-coded. A household that picked JPY (0 minor units) or KWD (3) would have every amount rendered wrong.

So: `domain.Currency` carries `MinorUnits` from the start, `ParseCurrency` accepts any active code (the household PATCH path already accepts arbitrary codes today and must not start rejecting them), but **`GET /api/v1/currencies` serves only `MinorUnits == 2` currencies in this slice**, because those are the only ones the money path renders correctly. Task 22 records this in a doc comment at the filter, so the person who later teaches `Money.String()` about minor units knows exactly which line to delete.

## File Structure

| Path | Responsibility |
|---|---|
| `api/internal/domain/currency.go` | The active ISO 4217 code set, `ParseCurrency`, `MinorUnits` |
| `api/internal/domain/token.go` | `TokenState` and `TokenLifecycle` — the consumed-before-expired rule, once |
| `api/migrations/00003_signups.sql` | `signups`, the `avatar_initial` widen |
| `api/internal/adapter/postgres/queries/signup.sql` | sqlc source for `signups` and the two pruners |
| `api/internal/adapter/postgres/signup_repo.go` | `SignupRepository`, including `Provision`'s transaction |
| `api/internal/usecase/password.go` | The password floor/ceiling and `validatePassword`, moved out of `invite.go` |
| `api/internal/usecase/blueprint.go` | `HouseholdBlueprint`, `DefaultNotificationPreferences` |
| `api/internal/usecase/signup.go` | `SignupService` — `Request`, `Preview`, `Complete` |
| `api/internal/adapter/http/signup_handlers.go` | The three sign-up endpoints |
| `api/internal/adapter/http/currency_handlers.go` | `GET /api/v1/currencies` |
| `api/internal/adapter/http/middleware_ratelimit.go` | The per-IP token bucket |
| `web/src/routes/publicRoutes.ts` | The one list of pre-auth route and API prefixes |
| `web/src/features/auth/CheckYourEmailPanel.tsx` | The shared sent-panel `MagicLinkSentPanel` becomes a caller of |
| `web/src/features/auth/SignUpScreen.tsx` | Step 1 — email only |
| `web/src/features/auth/SignUpCompleteScreen.tsx` | Step 2 — household name, currency, your name, password |

---

### Task 22: Domain — the currency allowlist and the token lifecycle rule

**Files:**
- Create: `api/internal/domain/currency.go`, `api/internal/domain/currency_test.go`
- Create: `api/internal/domain/token.go`, `api/internal/domain/token_test.go`
- Modify: `api/internal/domain/money.go:12-22` (`NewMoney` delegates to `ParseCurrency`)
- Modify: `api/internal/domain/money_test.go` (the `ZZZ`-is-accepted expectation)
- Modify: `api/internal/usecase/household.go:118-124` (`normalizeCurrency` delegates)
- Modify: `api/internal/usecase/invite.go:214-222` (`checkInviteLive` uses `TokenLifecycle`)

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `domain.ParseCurrency(code string) (string, error)` — uppercases, validates against the active set, returns the normalised code. Returns an error wrapping `domain.ErrInvalidMoney` for an unknown code.
  - `domain.CurrencyMinorUnits(code string) (int, bool)`
  - `domain.ActiveCurrencies() []Currency` where `type Currency struct { Code, Name string; MinorUnits int }`, ordered by `Code`.
  - `domain.TokenState` with constants `TokenLive`, `TokenConsumed`, `TokenExpired`.
  - `domain.TokenLifecycle(now, expiresAt time.Time, consumedAt *time.Time) TokenState`.

- [ ] **Step 1: Write the failing currency test**

Create `api/internal/domain/currency_test.go`:

```go
package domain

import (
	"errors"
	"testing"
)

func TestParseCurrencyNormalisesCase(t *testing.T) {
	got, err := ParseCurrency("sgd")
	if err != nil {
		t.Fatalf("ParseCurrency(sgd): %v", err)
	}
	if got != "SGD" {
		t.Fatalf("got %q, want SGD", got)
	}
}

// ZZZ is three uppercase letters, which is all NewMoney used to check. It is
// not an ISO 4217 code, and sign-up is the first place a stranger picks this
// value, so it must be refused.
func TestParseCurrencyRejectsAWellFormedNonCurrency(t *testing.T) {
	if _, err := ParseCurrency("ZZZ"); !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf("ParseCurrency(ZZZ) error = %v, want ErrInvalidMoney", err)
	}
}

func TestParseCurrencyRejectsWrongLength(t *testing.T) {
	for _, code := range []string{"", "S", "SG", "SGDX"} {
		if _, err := ParseCurrency(code); !errors.Is(err, ErrInvalidMoney) {
			t.Fatalf("ParseCurrency(%q) error = %v, want ErrInvalidMoney", code, err)
		}
	}
}

func TestCurrencyMinorUnits(t *testing.T) {
	for _, tc := range []struct {
		code string
		want int
	}{
		{"SGD", 2},
		{"IDR", 2},
		{"JPY", 0},
		{"KWD", 3},
	} {
		got, ok := CurrencyMinorUnits(tc.code)
		if !ok {
			t.Fatalf("CurrencyMinorUnits(%s): not found", tc.code)
		}
		if got != tc.want {
			t.Fatalf("CurrencyMinorUnits(%s) = %d, want %d", tc.code, got, tc.want)
		}
	}
	if _, ok := CurrencyMinorUnits("ZZZ"); ok {
		t.Fatal("CurrencyMinorUnits(ZZZ): want not found")
	}
}

// ActiveCurrencies must be sorted and must not hand out a slice the caller can
// mutate into the package's own state.
func TestActiveCurrenciesIsSortedAndCopied(t *testing.T) {
	first := ActiveCurrencies()
	if len(first) < 100 {
		t.Fatalf("ActiveCurrencies() returned %d entries, want the full ISO 4217 active set", len(first))
	}
	for i := 1; i < len(first); i++ {
		if first[i-1].Code >= first[i].Code {
			t.Fatalf("not sorted at %d: %q then %q", i, first[i-1].Code, first[i].Code)
		}
	}
	first[0].Code = "MUTATED"
	if ActiveCurrencies()[0].Code == "MUTATED" {
		t.Fatal("ActiveCurrencies() handed out its own backing array")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd api && go test ./internal/domain/ -run TestParseCurrency -v`
Expected: FAIL — `undefined: ParseCurrency`.

- [ ] **Step 3: Write the currency data and functions**

Create `api/internal/domain/currency.go`. `MinorUnits` is present from the start because `Money.String()` hard-codes two decimal places and something has to know which currencies that is wrong for — see the plan's scope note.

```go
package domain

import (
	"fmt"
	"sort"
	"strings"
)

// Currency is one active ISO 4217 currency. MinorUnits is the number of
// decimal places the currency actually has: 2 for most, 0 for JPY and KRW, 3
// for the Gulf dinars. Money.String() hard-codes two, so MinorUnits is what
// lets a caller refuse to offer a currency the money path would render wrong.
type Currency struct {
	Code       string
	Name       string
	MinorUnits int
}

// activeCurrencies is the ISO 4217 active list. It is a package-level var
// rather than a function-local literal so ActiveCurrencies can sort it once,
// in init, instead of on every call.
//
// This is the single reference for "is this a currency" in this codebase:
// ParseCurrency below is the only validator, NewMoney delegates to it, and
// usecase.normalizeCurrency delegates to NewMoney. Do not add a second list
// anywhere -- the frontend deliberately reads GET /api/v1/currencies rather
// than keeping its own.
var activeCurrencies = []Currency{
	{"AED", "UAE dirham", 2}, {"AFN", "Afghan afghani", 2},
	{"ALL", "Albanian lek", 2}, {"AMD", "Armenian dram", 2},
	{"ANG", "Netherlands Antillean guilder", 2}, {"AOA", "Angolan kwanza", 2},
	{"ARS", "Argentine peso", 2}, {"AUD", "Australian dollar", 2},
	{"AWG", "Aruban florin", 2}, {"AZN", "Azerbaijani manat", 2},
	{"BAM", "Bosnia and Herzegovina convertible mark", 2},
	{"BBD", "Barbados dollar", 2}, {"BDT", "Bangladeshi taka", 2},
	{"BGN", "Bulgarian lev", 2}, {"BHD", "Bahraini dinar", 3},
	{"BIF", "Burundian franc", 0}, {"BMD", "Bermudian dollar", 2},
	{"BND", "Brunei dollar", 2}, {"BOB", "Boliviano", 2},
	{"BRL", "Brazilian real", 2}, {"BSD", "Bahamian dollar", 2},
	{"BTN", "Bhutanese ngultrum", 2}, {"BWP", "Botswana pula", 2},
	{"BYN", "Belarusian ruble", 2}, {"BZD", "Belize dollar", 2},
	{"CAD", "Canadian dollar", 2}, {"CDF", "Congolese franc", 2},
	{"CHF", "Swiss franc", 2}, {"CLP", "Chilean peso", 0},
	{"CNY", "Renminbi", 2}, {"COP", "Colombian peso", 2},
	{"CRC", "Costa Rican colon", 2}, {"CUP", "Cuban peso", 2},
	{"CVE", "Cape Verdean escudo", 2}, {"CZK", "Czech koruna", 2},
	{"DJF", "Djiboutian franc", 0}, {"DKK", "Danish krone", 2},
	{"DOP", "Dominican peso", 2}, {"DZD", "Algerian dinar", 2},
	{"EGP", "Egyptian pound", 2}, {"ERN", "Eritrean nakfa", 2},
	{"ETB", "Ethiopian birr", 2}, {"EUR", "Euro", 2},
	{"FJD", "Fiji dollar", 2}, {"FKP", "Falkland Islands pound", 2},
	{"GBP", "Pound sterling", 2}, {"GEL", "Georgian lari", 2},
	{"GHS", "Ghanaian cedi", 2}, {"GIP", "Gibraltar pound", 2},
	{"GMD", "Gambian dalasi", 2}, {"GNF", "Guinean franc", 0},
	{"GTQ", "Guatemalan quetzal", 2}, {"GYD", "Guyanese dollar", 2},
	{"HKD", "Hong Kong dollar", 2}, {"HNL", "Honduran lempira", 2},
	{"HTG", "Haitian gourde", 2}, {"HUF", "Hungarian forint", 2},
	{"IDR", "Indonesian rupiah", 2}, {"ILS", "Israeli new shekel", 2},
	{"INR", "Indian rupee", 2}, {"IQD", "Iraqi dinar", 3},
	{"IRR", "Iranian rial", 2}, {"ISK", "Icelandic krona", 0},
	{"JMD", "Jamaican dollar", 2}, {"JOD", "Jordanian dinar", 3},
	{"JPY", "Japanese yen", 0}, {"KES", "Kenyan shilling", 2},
	{"KGS", "Kyrgyzstani som", 2}, {"KHR", "Cambodian riel", 2},
	{"KMF", "Comoro franc", 0}, {"KPW", "North Korean won", 2},
	{"KRW", "South Korean won", 0}, {"KWD", "Kuwaiti dinar", 3},
	{"KYD", "Cayman Islands dollar", 2}, {"KZT", "Kazakhstani tenge", 2},
	{"LAK", "Lao kip", 2}, {"LBP", "Lebanese pound", 2},
	{"LKR", "Sri Lankan rupee", 2}, {"LRD", "Liberian dollar", 2},
	{"LSL", "Lesotho loti", 2}, {"LYD", "Libyan dinar", 3},
	{"MAD", "Moroccan dirham", 2}, {"MDL", "Moldovan leu", 2},
	{"MGA", "Malagasy ariary", 2}, {"MKD", "Macedonian denar", 2},
	{"MMK", "Myanmar kyat", 2}, {"MNT", "Mongolian togrog", 2},
	{"MOP", "Macanese pataca", 2}, {"MRU", "Mauritanian ouguiya", 2},
	{"MUR", "Mauritian rupee", 2}, {"MVR", "Maldivian rufiyaa", 2},
	{"MWK", "Malawian kwacha", 2}, {"MXN", "Mexican peso", 2},
	{"MYR", "Malaysian ringgit", 2}, {"MZN", "Mozambican metical", 2},
	{"NAD", "Namibian dollar", 2}, {"NGN", "Nigerian naira", 2},
	{"NIO", "Nicaraguan cordoba", 2}, {"NOK", "Norwegian krone", 2},
	{"NPR", "Nepalese rupee", 2}, {"NZD", "New Zealand dollar", 2},
	{"OMR", "Omani rial", 3}, {"PAB", "Panamanian balboa", 2},
	{"PEN", "Peruvian sol", 2}, {"PGK", "Papua New Guinean kina", 2},
	{"PHP", "Philippine peso", 2}, {"PKR", "Pakistani rupee", 2},
	{"PLN", "Polish zloty", 2}, {"PYG", "Paraguayan guarani", 0},
	{"QAR", "Qatari riyal", 2}, {"RON", "Romanian leu", 2},
	{"RSD", "Serbian dinar", 2}, {"RUB", "Russian ruble", 2},
	{"RWF", "Rwandan franc", 0}, {"SAR", "Saudi riyal", 2},
	{"SBD", "Solomon Islands dollar", 2}, {"SCR", "Seychelles rupee", 2},
	{"SDG", "Sudanese pound", 2}, {"SEK", "Swedish krona", 2},
	{"SGD", "Singapore dollar", 2}, {"SHP", "Saint Helena pound", 2},
	{"SLE", "Sierra Leonean leone", 2}, {"SOS", "Somalian shilling", 2},
	{"SRD", "Surinamese dollar", 2}, {"SSP", "South Sudanese pound", 2},
	{"STN", "Sao Tome and Principe dobra", 2}, {"SVC", "Salvadoran colon", 2},
	{"SYP", "Syrian pound", 2}, {"SZL", "Swazi lilangeni", 2},
	{"THB", "Thai baht", 2}, {"TJS", "Tajikistani somoni", 2},
	{"TMT", "Turkmenistan manat", 2}, {"TND", "Tunisian dinar", 3},
	{"TOP", "Tongan paanga", 2}, {"TRY", "Turkish lira", 2},
	{"TTD", "Trinidad and Tobago dollar", 2}, {"TWD", "New Taiwan dollar", 2},
	{"TZS", "Tanzanian shilling", 2}, {"UAH", "Ukrainian hryvnia", 2},
	{"UGX", "Ugandan shilling", 0}, {"USD", "United States dollar", 2},
	{"UYU", "Uruguayan peso", 2}, {"UZS", "Uzbekistani sum", 2},
	{"VED", "Venezuelan digital bolivar", 2}, {"VES", "Venezuelan bolivar soberano", 2},
	{"VND", "Vietnamese dong", 0}, {"VUV", "Vanuatu vatu", 0},
	{"WST", "Samoan tala", 2}, {"XAF", "Central African CFA franc", 0},
	{"XCD", "East Caribbean dollar", 2}, {"XOF", "West African CFA franc", 0},
	{"XPF", "CFP franc", 0}, {"YER", "Yemeni rial", 2},
	{"ZAR", "South African rand", 2}, {"ZMW", "Zambian kwacha", 2},
	{"ZWG", "Zimbabwe gold", 2},
}

// byCode indexes activeCurrencies for O(1) lookup. Built in init rather than
// lazily so there is no locking and no first-call cost on a request path.
var byCode = map[string]Currency{}

func init() {
	sort.Slice(activeCurrencies, func(i, j int) bool {
		return activeCurrencies[i].Code < activeCurrencies[j].Code
	})
	for _, c := range activeCurrencies {
		byCode[c.Code] = c
	}
}

// ParseCurrency uppercases a currency code and checks it against the active
// ISO 4217 list. It is the single validator: NewMoney delegates to it, and so
// (transitively) does every caller-supplied currency on the household PATCH
// path.
//
// The error wraps ErrInvalidMoney rather than introducing a new sentinel,
// because adapter/http/errors.go already maps that to 422 INVALID_CURRENCY
// with the copy "That currency code is not valid." -- exactly what an unknown
// code deserves. A new sentinel would need a new case for no gain.
func ParseCurrency(code string) (string, error) {
	upper := strings.ToUpper(strings.TrimSpace(code))
	if _, ok := byCode[upper]; !ok {
		return "", fmt.Errorf("%w: %q is not an active ISO 4217 currency", ErrInvalidMoney, code)
	}
	return upper, nil
}

// CurrencyMinorUnits reports how many decimal places a currency has. The bool
// is false for an unknown code, exactly as a map lookup would be.
func CurrencyMinorUnits(code string) (int, bool) {
	c, ok := byCode[strings.ToUpper(strings.TrimSpace(code))]
	if !ok {
		return 0, false
	}
	return c.MinorUnits, true
}

// ActiveCurrencies returns the whole active list, sorted by code. It copies,
// so a caller cannot mutate the package's own state -- GET /api/v1/currencies
// hands its result straight to a JSON encoder and a filter, and neither should
// be able to reorder or corrupt this list for every later request.
func ActiveCurrencies() []Currency {
	out := make([]Currency, len(activeCurrencies))
	copy(out, activeCurrencies)
	return out
}
```

- [ ] **Step 4: Run the currency test to verify it passes**

Run: `cd api && go test ./internal/domain/ -run 'TestParseCurrency|TestCurrencyMinorUnits|TestActiveCurrencies' -v`
Expected: PASS, five tests.

- [ ] **Step 5: Point `NewMoney` at `ParseCurrency`**

Replace `api/internal/domain/money.go` lines 12-22 (the body of `NewMoney`) with:

```go
// NewMoney validates the currency through ParseCurrency, which is the single
// reference for what a valid code is. It used to check only "three uppercase
// letters", which accepted ZZZ -- fine while the only codes in the system came
// from a migration default, wrong the moment sign-up let a stranger choose one.
func NewMoney(amount int64, currency string) (Money, error) {
	code, err := ParseCurrency(currency)
	if err != nil {
		return Money{}, err
	}
	return Money{Amount: amount, Currency: code}, nil
}
```

Note that this also normalises: `NewMoney(1, "sgd")` now yields `Currency: "SGD"` where it previously errored. That is a deliberate widening, and it is what lets `normalizeCurrency` stop doing its own `strings.ToUpper`.

- [ ] **Step 6: Fix the money tests this invalidates**

Run: `cd api && go test ./internal/domain/ -run TestNewMoney -v`
Expected: FAIL — whichever existing case asserts that a lowercase code errors, or that `ZZZ` is accepted.

Update those cases in `api/internal/domain/money_test.go`: a lowercase code now succeeds and normalises, and add the regression case:

```go
func TestNewMoneyRejectsAWellFormedNonCurrency(t *testing.T) {
	if _, err := NewMoney(100, "ZZZ"); !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf("NewMoney(ZZZ) error = %v, want ErrInvalidMoney", err)
	}
}
```

- [ ] **Step 7: Simplify `normalizeCurrency`**

Replace `api/internal/usecase/household.go` lines 114-124 with:

```go
// normalizeCurrency validates a currency code through domain.ParseCurrency --
// the single reference for what a valid code looks like, shared by both of
// Update's currency fields so the two checks cannot drift apart. It no longer
// uppercases first: ParseCurrency does that itself, and NewMoney (which this
// used to call) now delegates to the same function.
//
// The error is returned as-is rather than re-wrapped: ParseCurrency already
// wraps ErrInvalidMoney, which is the sentinel adapter/http/errors.go maps to
// 422 INVALID_CURRENCY. Wrapping it twice added nothing.
func normalizeCurrency(currency string) (string, error) {
	return domain.ParseCurrency(currency)
}
```

Then check whether `strings` and `fmt` are still used elsewhere in the file:

Run: `cd api && go build ./... 2>&1 | head`
Expected: either clean, or an "imported and not used" error naming `strings` or `fmt`. Remove whichever import it names.

- [ ] **Step 8: Run the household service tests**

Run: `cd api && go test ./internal/usecase/ -run TestHousehold -v`
Expected: PASS. If a case asserted that `ZZZ` was accepted by `Update`, it now fails — change it to assert `ErrInvalidMoney`, which is the whole point.

- [ ] **Step 9: Write the failing token-lifecycle test**

Create `api/internal/domain/token_test.go`:

```go
package domain

import (
	"testing"
	"time"
)

func TestTokenLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	t.Run("live when unconsumed and unexpired", func(t *testing.T) {
		if got := TokenLifecycle(now, future, nil); got != TokenLive {
			t.Fatalf("got %v, want TokenLive", got)
		}
	})

	t.Run("expired at exactly the expiry instant", func(t *testing.T) {
		// Not After(now) is the rule checkInviteLive already used: a token
		// whose expiry is exactly now is spent, not live.
		if got := TokenLifecycle(now, now, nil); got != TokenExpired {
			t.Fatalf("got %v, want TokenExpired", got)
		}
	})

	t.Run("consumed beats expired", func(t *testing.T) {
		// This ordering is the whole reason this function exists. An invite
		// that was accepted and has since passed its expiry must report
		// accepted -- telling the holder "expired, ask for another" would
		// send them chasing a second invite for an account they already have.
		if got := TokenLifecycle(now, past, &past); got != TokenConsumed {
			t.Fatalf("got %v, want TokenConsumed", got)
		}
	})

	t.Run("consumed while still inside its window", func(t *testing.T) {
		if got := TokenLifecycle(now, future, &past); got != TokenConsumed {
			t.Fatalf("got %v, want TokenConsumed", got)
		}
	})
}
```

- [ ] **Step 10: Run it to verify it fails**

Run: `cd api && go test ./internal/domain/ -run TestTokenLifecycle -v`
Expected: FAIL — `undefined: TokenLifecycle`.

- [ ] **Step 11: Write `token.go`**

Create `api/internal/domain/token.go`:

```go
package domain

import "time"

// TokenState is what a single-use, expiring token can be. It exists so the
// three token flows in this codebase -- invites, magic links and sign-ups --
// share one answer to "is this still usable", rather than three.
type TokenState int

const (
	TokenLive TokenState = iota
	TokenConsumed
	TokenExpired
)

func (s TokenState) String() string {
	switch s {
	case TokenLive:
		return "live"
	case TokenConsumed:
		return "consumed"
	case TokenExpired:
		return "expired"
	default:
		return "unknown"
	}
}

// TokenLifecycle reports whether a token is still usable, and if not, why.
//
// The ordering is the load-bearing part: consumed is checked before expired.
// A token that was used and has since passed its expiry must report consumed,
// because the two cases need different answers -- "you already used this, sign
// in" versus "this lapsed, start again" -- and reporting expiry for an
// already-used token sends someone chasing a replacement for an account they
// already have. usecase.checkInviteLive has always had this ordering; this is
// where it now lives so sign-up cannot get it backwards.
//
// consumedAt is a pointer because "not consumed" is the absence of a
// timestamp, matching the nullable column it is read from.
func TokenLifecycle(now, expiresAt time.Time, consumedAt *time.Time) TokenState {
	if consumedAt != nil {
		return TokenConsumed
	}
	if !expiresAt.After(now) {
		return TokenExpired
	}
	return TokenLive
}
```

- [ ] **Step 12: Run the token test to verify it passes**

Run: `cd api && go test ./internal/domain/ -run TestTokenLifecycle -v`
Expected: PASS, four subtests.

- [ ] **Step 13: Mutation-check the ordering**

The ordering is the only reason this function exists, so prove the test defends it. Temporarily swap the two `if` blocks in `TokenLifecycle` so expiry is checked first.

Run: `cd api && go test ./internal/domain/ -run TestTokenLifecycle -v`
Expected: FAIL on `consumed_beats_expired`. If it passes, the test is not protecting anything — fix the test before restoring the code.

Restore the correct order and re-run. Expected: PASS.

- [ ] **Step 14: Point `checkInviteLive` at `TokenLifecycle`**

Replace `api/internal/usecase/invite.go` lines 206-222 with:

```go
// checkInviteLive reports the specific reason an invite can no longer be used,
// distinguishing an already-accepted invite (409, per the spec) from an
// expired one (410) -- a distinction InviteRepository.Accept's own no-rows case
// cannot make, because its guarded UPDATE collapses both into the same
// zero-rows result. Reading accepted_at and expires_at ourselves, via
// ByTokenHash, is what lets Preview and Accept report the two apart;
// InviteRepository.Accept's answer is then authoritative only for the race
// window between this read and that write.
//
// The consumed-before-expired ordering now lives in domain.TokenLifecycle,
// shared with sign-up. The sentinels stay invite-specific because the HTTP
// layer maps them to different statuses with different copy.
func checkInviteLive(details InviteDetails, now time.Time) error {
	switch domain.TokenLifecycle(now, details.ExpiresAt, details.AcceptedAt) {
	case domain.TokenLive:
		return nil
	case domain.TokenConsumed:
		return domain.ErrInviteAlreadyAccepted
	case domain.TokenExpired:
		return domain.ErrInviteExpired
	default:
		// A state this switch does not recognise must fail closed rather than
		// treat the invite as usable. Adding a TokenState without adding a
		// case here refuses the invite; it does not silently accept it.
		return domain.ErrInviteExpired
	}
}
```

- [ ] **Step 15: Run the invite tests to verify nothing changed**

Run: `cd api && go test ./internal/usecase/ -run TestInvite -v`
Expected: PASS, unchanged. This was a refactor with no behaviour change; a failure here means the extraction altered the ordering.

- [ ] **Step 16: Run the full gate for this task**

Run: `cd api && go vet ./... && go test ./internal/domain/ ./internal/usecase/ -count=1`
Expected: PASS.

Run: `cd .. && make lint-arch`
Expected: PASS — `domain/currency.go` imports `fmt`, `sort`, `strings` and `domain/token.go` imports `time`, all stdlib.

- [ ] **Step 17: Commit**

```bash
git add api/internal/domain/currency.go api/internal/domain/currency_test.go \
        api/internal/domain/token.go api/internal/domain/token_test.go \
        api/internal/domain/money.go api/internal/domain/money_test.go \
        api/internal/usecase/household.go api/internal/usecase/invite.go
git commit -m "feat(domain): add the ISO 4217 allowlist and one token-lifecycle rule

NewMoney accepted ZZZ, which was harmless while every currency in the system
came from a migration default and wrong the moment sign-up lets a stranger
pick one. ParseCurrency is now the single validator and NewMoney delegates to
it, which closes the format-only validation HANDOVER lists as outstanding.

TokenLifecycle holds the consumed-before-expired ordering that until now lived
only inside checkInviteLive. Sign-up is the third token flow and needed the
same rule; the sentinels stay per-flow because the HTTP layer maps them to
different statuses."
```

---

### Task 23: The signups schema, the pruners, and the avatar-initial widen

**Files:**
- Create: `api/migrations/00003_signups.sql`
- Create: `api/internal/adapter/postgres/queries/signup.sql`
- Modify: `api/internal/adapter/postgres/queries/identity.sql` (add `CreateHousehold`'s currency parameters, add `PruneLoginAttempts`, make `GetMembershipByUser` deterministic)
- Test: `api/internal/adapter/postgres/schema_test.go` (extend)

**Interfaces:**
- Consumes: `testsupport.StartPostgres`.
- Produces: the `signups` table; `users.avatar_initial` as `text`; and these generated methods in `sqlcgen` — `CreateSignup`, `GetSignupByTokenHash`, `CountSignupsForEmailSince`, `CountSignupsSince`, `ConsumeSignup`, `PruneSignups`, `PruneLoginAttempts`, and a `CreateHousehold` that now takes every household column.

`00001_init.sql` and `00002_identity.sql` are **not** touched. Both have already been applied on every developer's database and goose will not re-run them.

- [ ] **Step 1: Write the migration**

Create `api/migrations/00003_signups.sql`:

```sql
-- +goose Up

-- signups holds a verified-address intent, and nothing else: no household
-- name, no display name, no password. Those are collected on the screen the
-- mailed token leads to, deliberately -- if they were captured before
-- verification, someone could submit a sign-up for another person's address
-- with a household name and display name of their choosing, and the mail
-- would invite that person into a household a stranger had configured.
--
-- Shaped after magic_links, with one difference: there is no user_id, because
-- there is no user yet. That absence is the reason this is a new table rather
-- than a column on magic_links -- magic_links.user_id is NOT NULL REFERENCES
-- users(id), and relaxing it would put a nullable branch on the recovery path
-- that has to keep working while a household is locked.
CREATE TABLE signups (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email       citext      NOT NULL,
    token_hash  bytea       NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- email is deliberately NOT unique. Several live tokens for one address are
-- fine: the first consumed wins, and a second consume then collides with
-- users.email's unique index, which translate() turns into
-- domain.ErrAlreadyExists and MapDomainError answers 409. Making email unique
-- here would instead make a second sign-up request for the same address fail
-- loudly, which is itself an enumeration oracle.
CREATE INDEX signups_email_created_idx ON signups (email, created_at DESC);

-- Supports both the global daily mail ceiling and PruneSignups.
CREATE INDEX signups_created_idx ON signups (created_at DESC);

-- avatar_initial was char(1). One character is enough for a single letter in
-- any script, so this is not about non-ASCII names fitting -- it is about
-- strings.ToUpper growing a rune: German 'ß' uppercases to "SS", two
-- characters, which char(1) rejects outright. Nobody could reach that before,
-- because initialOf sliced bytes and produced mojibake long before it reached
-- the column; fixing initialOf to slice runes is what makes the expansion
-- case reachable. text also leaves room for a future profile editor to store a
-- two-character initial.
ALTER TABLE users ALTER COLUMN avatar_initial TYPE text;

-- +goose Down
ALTER TABLE users ALTER COLUMN avatar_initial TYPE char(1);
DROP TABLE signups;
```

Note on the Down: narrowing back to `char(1)` fails if any row holds a longer
initial by then. That is correct — a Down that silently truncated a user's
avatar would be worse than one that refuses.

- [ ] **Step 2: Apply it and confirm goose is happy**

Run: `make up && make migrate-up`

If there is no `migrate-up` target, check what the Makefile calls it:

Run: `make | grep -i migrat`

Expected: the migration applies with no error, and `signups` exists.

- [ ] **Step 3: Write the sign-up queries**

Create `api/internal/adapter/postgres/queries/signup.sql`:

```sql
-- name: CreateSignup :exec
INSERT INTO signups (email, token_hash, expires_at)
VALUES ($1, $2, $3);

-- name: GetSignupByTokenHash :one
SELECT id, email, expires_at, consumed_at
FROM signups
WHERE token_hash = $1;

-- name: CountSignupsForEmailSince :one
SELECT count(*) FROM signups
WHERE email = $1 AND created_at >= $2;

-- name: CountSignupsSince :one
SELECT count(*) FROM signups
WHERE created_at >= $1;

-- ConsumeSignup is the transactional gate for Provision. The guard lives here,
-- in the UPDATE, rather than in the caller: a zero-rows result means the signup
-- was already consumed or has expired, and that answer is authoritative for the
-- race between SignupService.Complete's read and this write. It returns the
-- email so Provision reads the verified address from the row it is already
-- touching, rather than trusting one passed in by a caller.
-- name: ConsumeSignup :one
UPDATE signups
SET consumed_at = now()
WHERE id = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING id, email;

-- name: PruneSignups :execrows
DELETE FROM signups
WHERE created_at < $1
  AND (consumed_at IS NOT NULL OR expires_at <= now());
```

- [ ] **Step 4: Add the three identity-query changes**

In `api/internal/adapter/postgres/queries/identity.sql`:

First, make `GetMembershipByUser` deterministic. Find it (around line 52) and change the statement to:

```sql
-- ByUser cannot take a household scope -- sign-in resolves the household from
-- it -- so it remains LIMIT 1 while one account belongs to exactly one
-- household. The ORDER BY makes which row it picks deterministic rather than
-- whatever the planner returns first, so if that invariant is ever broken the
-- failure is reproducible instead of intermittent.
-- name: GetMembershipByUser :one
SELECT id, household_id, user_id, role, capabilities
FROM memberships WHERE user_id = $1
ORDER BY joined_at, id
LIMIT 1;
```

Second, widen `CreateHousehold` so no caller depends on column defaults for currency. Find it and change it to:

```sql
-- name: CreateHousehold :one
INSERT INTO households (name, family_name, primary_currency,
                        show_secondary_currency, secondary_currency)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, family_name, primary_currency,
          show_secondary_currency, secondary_currency, fx_rate_mode;
```

`fx_rate_mode` keeps its column default (`'auto'`) and is returned, not passed —
nothing chooses it at creation time, and the CHECK constraint means the default
is the only safe value to assume.

Third, append the login-attempts pruner:

```sql
-- PruneLoginAttempts deletes attempts older than the cutoff, including the
-- NULL-household_id rows an unknown-address sign-in attempt records.
-- ClearFailures cannot reach those: it is scoped WHERE household_id = $1, and
-- household_id = $1 never matches NULL. So the rows a member generates are the
-- only ones anything ever deleted, and the rows a stranger generates were
-- deleted by nothing at all.
--
-- The caller is responsible for a cutoff well outside
-- domain.LockoutPolicy.Window. Deleting a row still inside that window would
-- clear a live lockout -- a security regression dressed as a cleanup.
-- name: PruneLoginAttempts :execrows
DELETE FROM login_attempts WHERE at < $1;
```

- [ ] **Step 5: Regenerate and confirm the new methods exist**

Run: `make sqlc`
Expected: no error.

Run: `cd api && grep -c 'func (q \*Queries) \(CreateSignup\|GetSignupByTokenHash\|CountSignupsForEmailSince\|CountSignupsSince\|ConsumeSignup\|PruneSignups\|PruneLoginAttempts\)' internal/adapter/postgres/sqlcgen/*.go`
Expected: `7`.

- [ ] **Step 6: Fix the `CreateHousehold` call site the signature change breaks**

Run: `cd api && go build ./... 2>&1 | head`
Expected: FAIL in `internal/adapter/postgres/household_repo.go` — `CreateHouseholdParams` now has five fields.

This is deliberate: Task 25 rewrites `HouseholdRepository.Create` to take a
`domain.Household`. For now, keep the build green by passing the values the
column defaults used to supply, so this task's schema work can be committed and
reviewed on its own:

```go
	row, err := r.q.CreateHousehold(ctx, sqlcgen.CreateHouseholdParams{
		Name:                  name,
		FamilyName:            familyName,
		PrimaryCurrency:       "SGD",
		ShowSecondaryCurrency: true,
		SecondaryCurrency:     "IDR",
	})
```

Add a comment above it: `// Task 25 replaces these literals with the caller's HouseholdBlueprint; they reproduce the column defaults exactly so this change is behaviour-neutral.`

- [ ] **Step 7: Write the schema test**

Append to `api/internal/adapter/postgres/schema_test.go`:

```go
func TestSignupsSchema(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	t.Run("token_hash is unique", func(t *testing.T) {
		hash := []byte("a-token-hash-32-bytes-long------")
		expires := time.Now().Add(24 * time.Hour)
		if _, err := pool.Exec(ctx,
			`INSERT INTO signups (email, token_hash, expires_at) VALUES ($1, $2, $3)`,
			"first@example.test", hash, expires); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		_, err := pool.Exec(ctx,
			`INSERT INTO signups (email, token_hash, expires_at) VALUES ($1, $2, $3)`,
			"second@example.test", hash, expires)
		if err == nil {
			t.Fatal("expected a unique violation on token_hash")
		}
	})

	t.Run("email is deliberately not unique", func(t *testing.T) {
		expires := time.Now().Add(24 * time.Hour)
		for i, hash := range [][]byte{[]byte("hash-one-aaaaaaaaaaaaaaaaaaaaaaa"), []byte("hash-two-bbbbbbbbbbbbbbbbbbbbbbb")} {
			if _, err := pool.Exec(ctx,
				`INSERT INTO signups (email, token_hash, expires_at) VALUES ($1, $2, $3)`,
				"repeat@example.test", hash, expires); err != nil {
				t.Fatalf("insert %d for the same address: %v", i, err)
			}
		}
	})

	t.Run("email is case-insensitive, like every other address column", func(t *testing.T) {
		expires := time.Now().Add(24 * time.Hour)
		if _, err := pool.Exec(ctx,
			`INSERT INTO signups (email, token_hash, expires_at) VALUES ($1, $2, $3)`,
			"Mixed@Example.Test", []byte("hash-three-ccccccccccccccccccccc"), expires); err != nil {
			t.Fatalf("insert: %v", err)
		}
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM signups WHERE email = $1`, "mixed@example.test").Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 1 {
			t.Fatalf("citext lookup found %d rows, want 1", count)
		}
	})
}

// The reason avatar_initial was widened: strings.ToUpper can grow a rune, and
// char(1) rejects the result outright.
func TestAvatarInitialHoldsAMultiCharacterUppercase(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (display_name, avatar_initial) VALUES ($1, $2)`,
		"Straße", "SS"); err != nil {
		t.Fatalf("insert a two-character initial: %v", err)
	}
}
```

If `newTestPool` is not the helper name this package uses, check:

Run: `cd api && grep -n "func newTestPool\|func testPool\|StartPostgres" internal/adapter/postgres/*_test.go | head`

Use whatever helper the existing tests in this package use, and match how they
isolate state between tests.

- [ ] **Step 8: Run the schema test**

Run: `cd api && go test ./internal/adapter/postgres/ -run 'TestSignupsSchema|TestAvatarInitial' -v -count=1`
Expected: PASS, four subtests.

- [ ] **Step 9: Confirm nothing else broke**

Run: `cd api && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add api/migrations/00003_signups.sql \
        api/internal/adapter/postgres/queries/signup.sql \
        api/internal/adapter/postgres/queries/identity.sql \
        api/internal/adapter/postgres/sqlcgen/ \
        api/internal/adapter/postgres/household_repo.go \
        api/internal/adapter/postgres/schema_test.go
git commit -m "feat(db): add the signups table, two pruners, and widen avatar_initial

signups holds a verified-address intent and nothing else. email is
deliberately not unique -- making it unique would turn a second sign-up
request for the same address into a loud failure, which is itself an
enumeration oracle.

PruneLoginAttempts exists because ClearFailures is scoped WHERE household_id =
\$1, which never matches the NULL rows an unknown-address sign-in attempt
records. The rows a member generates were the only ones anything ever deleted;
the rows a stranger generates were deleted by nothing.

avatar_initial widens because strings.ToUpper can grow a rune -- 'ß' becomes
\"SS\" -- and char(1) rejects that. Unreachable until initialOf stops slicing
bytes, which is the next task."
```

---

### Task 24: `initialOf` slices runes, not bytes

**Files:**
- Modify: `api/internal/adapter/postgres/user_repo.go:131-137`
- Test: `api/internal/adapter/postgres/user_repo_test.go` (add cases)

**Interfaces:**
- Consumes: nothing.
- Produces: `initialOf` unchanged in signature — `func initialOf(displayName string) string` — but correct for non-ASCII input. Task 26's `Provision` relies on it.

Why this is its own task, before any sign-up code: `Provision` creates users through `initialOf`, so fixing it afterwards would mean Task 26's tests first encode the broken behaviour and then get rewritten.

- [ ] **Step 1: Write the failing test**

Append to `api/internal/adapter/postgres/user_repo_test.go`:

```go
// initialOf used to be strings.ToUpper(name[:1]) -- a *byte* slice. For any
// name starting outside ASCII that takes the first byte of a multi-byte UTF-8
// rune, producing an invalid fragment that renders as the replacement
// character. There is no profile-edit endpoint, so the wrong initial was
// permanent. Two known adults never hit it; a public sign-up form does.
func TestInitialOfHandlesNonASCIINames(t *testing.T) {
	for _, tc := range []struct {
		name        string
		displayName string
		want        string
	}{
		{"ascii", "Andreas", "A"},
		{"accented latin", "Émile", "É"},
		{"cyrillic", "Дмитрий", "Д"},
		{"greek", "Ωμέγα", "Ω"},
		{"cjk has no case", "李明", "李"},
		{"leading whitespace is trimmed", "  Christine", "C"},
		{"already uppercase", "ANDREAS", "A"},
		{"empty falls back", "", "?"},
		{"whitespace only falls back", "   ", "?"},
		{"uppercase can grow a rune", "ßeta", "SS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := initialOf(tc.displayName); got != tc.want {
				t.Fatalf("initialOf(%q) = %q, want %q", tc.displayName, got, tc.want)
			}
		})
	}
}

// The fragment the old implementation produced, spelled out, so this test
// documents the defect rather than only the fix.
func TestInitialOfNeverReturnsInvalidUTF8(t *testing.T) {
	for _, name := range []string{"Émile", "Дмитрий", "李明", "🙂nonymous"} {
		got := initialOf(name)
		if !utf8.ValidString(got) {
			t.Fatalf("initialOf(%q) = %q, which is not valid UTF-8", name, got)
		}
	}
}
```

Add `"unicode/utf8"` to the test file's imports.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd api && go test ./internal/adapter/postgres/ -run TestInitialOf -v -count=1`
Expected: FAIL on `accented_latin`, `cyrillic`, `greek`, `cjk_has_no_case` and `uppercase_can_grow_a_rune`, plus both cases in the UTF-8 test. The failure messages show the mojibake fragment.

- [ ] **Step 3: Fix it**

Replace `api/internal/adapter/postgres/user_repo.go` lines 131-137 with:

```go
// initialOf derives the avatar initial from a display name. It slices the
// first *rune*, not the first byte: name[:1] took one byte of what may be a
// multi-byte UTF-8 sequence, so "Émile" produced an invalid fragment that
// rendered as the replacement character -- permanently, since there is no
// profile-edit endpoint to correct it.
//
// The result can be more than one character: strings.ToUpper("ß") is "SS".
// users.avatar_initial is text (migration 00003) rather than char(1) for
// exactly that case.
//
// A rune is not always a whole grapheme cluster -- an emoji built from a
// zero-width joiner sequence yields only its first component here. Handling
// that properly needs golang.org/x/text, and this file is in an adapter so the
// dependency would be legal, but a single rune is the correct fix for the
// actual defect and a name beginning with a ZWJ sequence still produces valid,
// renderable UTF-8.
func initialOf(displayName string) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		return "?"
	}
	first := []rune(name)[0]
	return strings.ToUpper(string(first))
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd api && go test ./internal/adapter/postgres/ -run TestInitialOf -v -count=1`
Expected: PASS, ten subtests plus the UTF-8 test.

- [ ] **Step 5: Mutation-check**

Change `[]rune(name)[0]` back to `name[:1]`.

Run: `cd api && go test ./internal/adapter/postgres/ -run TestInitialOf -count=1`
Expected: FAIL. If it passes, the test is not defending the fix.

Restore and re-run. Expected: PASS.

- [ ] **Step 6: Confirm the wider suite**

Run: `cd api && go test ./internal/adapter/postgres/ -count=1`
Expected: PASS. Any existing test asserting a mojibake initial should be
corrected, not accommodated.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/postgres/user_repo.go api/internal/adapter/postgres/user_repo_test.go
git commit -m "fix(postgres): derive the avatar initial from the first rune, not the first byte

initialOf was strings.ToUpper(name[:1]), which takes one byte of what may be a
multi-byte UTF-8 sequence. Every display name starting outside ASCII got a
replacement-character avatar, permanently, because there is no profile-edit
endpoint. Invisible with two known adults; a public sign-up form makes it the
first thing a new customer sees."
```

---

### Task 25: Ports, the shared password rules, and `HouseholdBlueprint`

**Files:**
- Create: `api/internal/usecase/password.go`, `api/internal/usecase/blueprint.go`
- Create: `api/internal/usecase/blueprint_test.go`
- Modify: `api/internal/usecase/ports.go` (add `SignupRepository`, two `Mailer` methods, `LoginAttemptRepository.Prune`, change `HouseholdRepository.Create`)
- Modify: `api/internal/usecase/invite.go:11-44` (remove the moved constants and errors), `:236-241` (call `validatePassword`)
- Modify: `api/internal/usecase/seed.go:154-194` (build from a blueprint)
- Modify: `api/internal/usecase/testdouble_test.go` (the doubles the new/changed ports need)

**Interfaces:**
- Consumes: `domain.ParseCurrency` and `domain.AllCapabilities` from Task 22.
- Produces:
  - `usecase.validatePassword(plain string) error` — returns `ErrPasswordTooShort` under 12, `ErrPasswordTooLong` over 256, nil otherwise. `minPasswordLength = 12`, `maxPasswordLength = 256` (both unexported).
  - `usecase.HouseholdBlueprint` (fields below) and `usecase.DefaultNotificationPreferences() NotificationPreferences`.
  - `usecase.SignupRepository`, `usecase.SignupDetails`, `usecase.ProvisionedHousehold`.
  - `Mailer.SendSignupLink(ctx, to, url string) error` and `Mailer.SendSignupForExistingAccount(ctx, to, signInURL string) error`.
  - `LoginAttemptRepository.Prune(ctx, before time.Time) (int64, error)`.
  - `HouseholdRepository.Create(ctx context.Context, h domain.Household) (domain.Household, error)` — was `Create(ctx, name, familyName string)`.

- [ ] **Step 1: Move the password rules into their own file**

Create `api/internal/usecase/password.go`:

```go
package usecase

import "errors"

// minPasswordLength is the floor for every password this application accepts:
// invite acceptance and sign-up alike. It was minInvitePasswordLength in
// invite.go, which named the one caller that existed rather than the rule.
//
// The design document's create-household card says "At least 10 characters".
// That copy is wrong for this codebase, not the other way round -- 12 is what
// InviteService.Accept has always enforced and what
// adapter/http/errors.go's PASSWORD_TOO_SHORT message already tells a caller.
// Two different floors across the two ways of creating an account is how a
// defect gets in, so the copy changes and the rule does not.
const minPasswordLength = 12

// maxPasswordLength is the ceiling applied everywhere a caller-supplied
// password reaches PasswordHasher. argon2id's cost scales with the size of the
// string it hashes, so with no upper bound a caller could force an arbitrarily
// expensive hash by submitting a multi-megabyte password -- uncapped CPU cost
// fronted directly by an unauthenticated HTTP endpoint. 256 characters is far
// beyond any legitimate human-chosen or generator-produced password.
const maxPasswordLength = 256

// ErrPasswordTooShort and ErrPasswordTooLong are usecase sentinels rather than
// domain ones because domain has no notion of a password at all.
var (
	ErrPasswordTooShort = errors.New("password must be at least 12 characters")
	ErrPasswordTooLong  = errors.New("password must be at most 256 characters")
)

// validatePassword is the single gate for a chosen password. Both
// InviteService.Accept and SignupService.Complete call it, so the two account
// creation paths cannot drift.
//
// AuthService.SignIn deliberately does NOT call this. It enforces the same
// ceiling privately (see verifyPassword in auth.go) and must never surface a
// distinguishable sentinel: a too-long password at sign-in has to fail exactly
// like a wrong one.
func validatePassword(plain string) error {
	if len(plain) < minPasswordLength {
		return ErrPasswordTooShort
	}
	if len(plain) > maxPasswordLength {
		return ErrPasswordTooLong
	}
	return nil
}
```

- [ ] **Step 2: Delete the moved declarations from `invite.go` and call the new function**

In `api/internal/usecase/invite.go`, delete `minInvitePasswordLength`,
`ErrPasswordTooShort`, `maxPasswordLength` and `ErrPasswordTooLong` (lines
18-44), keeping `inviteTTL` and `ErrInviteeAlreadyRegistered`.

Then replace the first four lines of `Accept`'s body (lines 236-241) with:

```go
	if err := validatePassword(password); err != nil {
		return SignInResult{}, err
	}
```

- [ ] **Step 3: Confirm the move compiled and changed nothing**

Run: `cd api && go build ./... && go test ./internal/usecase/ -run TestInvite -count=1 -v`
Expected: PASS, unchanged. `auth.go` still references `maxPasswordLength`, which now resolves to `password.go`'s copy — same value, same package.

- [ ] **Step 4: Write the failing blueprint test**

Create `api/internal/usecase/blueprint_test.go`:

```go
package usecase_test

import (
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestDefaultNotificationPreferencesAreAllOn(t *testing.T) {
	got := usecase.DefaultNotificationPreferences()
	if !got.BillReminders || !got.OverspendAlerts || !got.RetroReminder || !got.WeeklyDigest {
		t.Fatalf("got %+v, want every flag true", got)
	}
}

func TestBlueprintForSignupValidates(t *testing.T) {
	t.Run("normalises the currency and mirrors it into secondary", func(t *testing.T) {
		b, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "sgd")
		if err != nil {
			t.Fatalf("NewSignupBlueprint: %v", err)
		}
		if b.PrimaryCurrency != "SGD" {
			t.Fatalf("PrimaryCurrency = %q, want SGD", b.PrimaryCurrency)
		}
		// Equal to primary, not the column's IDR default: CurrencyPanel renders
		// its toggle label straight from the column, so a household that never
		// chose IDR must not find "Show IDR equivalents" in Settings.
		if b.SecondaryCurrency != "SGD" {
			t.Fatalf("SecondaryCurrency = %q, want SGD", b.SecondaryCurrency)
		}
		if b.ShowSecondaryCurrency {
			t.Fatal("ShowSecondaryCurrency = true, want false for a self-serve household")
		}
	})

	t.Run("family name mirrors the household name", func(t *testing.T) {
		b, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "SGD")
		if err != nil {
			t.Fatalf("NewSignupBlueprint: %v", err)
		}
		if b.FamilyName != "Ade & Kris" {
			t.Fatalf("FamilyName = %q, want the household name", b.FamilyName)
		}
	})

	t.Run("the owner holds every capability", func(t *testing.T) {
		b, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "SGD")
		if err != nil {
			t.Fatalf("NewSignupBlueprint: %v", err)
		}
		if b.OwnerRole != domain.RoleOwner {
			t.Fatalf("OwnerRole = %q, want owner", b.OwnerRole)
		}
		for _, want := range domain.AllCapabilities() {
			if !b.OwnerCapabilities.Has(want) {
				t.Fatalf("OwnerCapabilities missing %q -- the memberships CHECK would reject this row", want)
			}
		}
	})

	t.Run("a blank household name is refused", func(t *testing.T) {
		for _, name := range []string{"", "   ", "\t\n"} {
			if _, err := usecase.NewSignupBlueprint(name, "Ade", "SGD"); err != usecase.ErrHouseholdNameRequired {
				t.Fatalf("NewSignupBlueprint(%q) error = %v, want ErrHouseholdNameRequired", name, err)
			}
		}
	})

	t.Run("a blank display name is refused", func(t *testing.T) {
		if _, err := usecase.NewSignupBlueprint("Ade & Kris", "  ", "SGD"); err != usecase.ErrDisplayNameRequired {
			t.Fatalf("error = %v, want ErrDisplayNameRequired", err)
		}
	})

	t.Run("an unknown currency is refused", func(t *testing.T) {
		if _, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "ZZZ"); err == nil {
			t.Fatal("NewSignupBlueprint accepted ZZZ")
		}
	})

	t.Run("names are trimmed", func(t *testing.T) {
		b, err := usecase.NewSignupBlueprint("  Ade & Kris  ", "  Ade  ", "SGD")
		if err != nil {
			t.Fatalf("NewSignupBlueprint: %v", err)
		}
		if b.Name != "Ade & Kris" || b.OwnerDisplayName != "Ade" {
			t.Fatalf("got Name=%q OwnerDisplayName=%q, want both trimmed", b.Name, b.OwnerDisplayName)
		}
	})
}
```

- [ ] **Step 5: Run it to verify it fails**

Run: `cd api && go test ./internal/usecase/ -run 'TestDefaultNotification|TestBlueprint' -v -count=1`
Expected: FAIL — `undefined: usecase.NewSignupBlueprint`.

- [ ] **Step 6: Write `blueprint.go`**

Create `api/internal/usecase/blueprint.go`:

```go
package usecase

import (
	"errors"
	"strings"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

var (
	// ErrHouseholdNameRequired and ErrDisplayNameRequired follow
	// ErrSpaceNameRequired's shape: a usecase sentinel the HTTP layer maps to
	// 422 with a field-specific code.
	ErrHouseholdNameRequired = errors.New("a household name is required")
	ErrDisplayNameRequired   = errors.New("a display name is required")
)

// HouseholdBlueprint is the single definition of what a new household consists
// of. Seed and SignupRepository.Provision both build one and then apply it
// differently -- Seed step-by-step and idempotently, because a partially failed
// seed run must be retryable (see Seed's own doc comment); Provision in one
// transaction, because a partially provisioned household leaves a users row
// occupying users.email's unique index and permanently blocks that address.
//
// Sharing the blueprint rather than one implementation is deliberate: a single
// implementation would have to give up either the step-idempotency or the
// atomicity, and both are load-bearing where they are.
//
// Spaces are not a field. domain.BuiltinSpaces needs the household ID, which
// does not exist until inside Provision's transaction, so the adapter calls it
// there. The knowledge of which spaces a household starts with stays in domain.
type HouseholdBlueprint struct {
	Name                  string
	FamilyName            string
	PrimaryCurrency       string
	SecondaryCurrency     string
	ShowSecondaryCurrency bool
	OwnerDisplayName      string
	OwnerRole             domain.Role
	OwnerCapabilities     domain.Capabilities
	Notifications         NotificationPreferences
}

// DefaultNotificationPreferences is every flag on, which is what the design
// shows a new household with. Seed and Provision both use it so the two cannot
// disagree about what "default" means.
func DefaultNotificationPreferences() NotificationPreferences {
	return NotificationPreferences{
		BillReminders:   true,
		OverspendAlerts: true,
		RetroReminder:   true,
		WeeklyDigest:    true,
	}
}

// NewSignupBlueprint validates and assembles the blueprint for a self-serve
// household. Every rule it applies is here rather than in the handler or the
// repository, so there is one place to read what a new household looks like.
func NewSignupBlueprint(householdName, displayName, currency string) (HouseholdBlueprint, error) {
	name := strings.TrimSpace(householdName)
	if name == "" {
		return HouseholdBlueprint{}, ErrHouseholdNameRequired
	}
	owner := strings.TrimSpace(displayName)
	if owner == "" {
		return HouseholdBlueprint{}, ErrDisplayNameRequired
	}
	code, err := domain.ParseCurrency(currency)
	if err != nil {
		return HouseholdBlueprint{}, err
	}

	return HouseholdBlueprint{
		Name: name,
		// The design's create card asks for one name only ("Household name",
		// helper "Shown at the top of the sidebar"), so family_name mirrors it
		// rather than adding a field the design does not draw. The invite
		// preview then reads "join the Ade & Kris household", which is fine.
		FamilyName:      name,
		PrimaryCurrency: code,
		// Equal to primary, and the toggle off. Not the column's IDR default:
		// CurrencyPanel renders "Show {secondaryCurrency} equivalents" straight
		// from the column, so a household in Sao Paulo would otherwise find a
		// reference to Indonesian rupiah in Settings. Equal-to-primary makes
		// the toggle inert but coherent, and makes the missing
		// secondary-currency picker a visible gap rather than a surprise.
		SecondaryCurrency:     code,
		ShowSecondaryCurrency: false,
		OwnerDisplayName:      owner,
		OwnerRole:             domain.RoleOwner,
		// An owner must hold every capability -- domain.NewMembership enforces
		// it and the memberships CHECK constraint enforces it again. Passing
		// anything else here produces a row Postgres refuses.
		OwnerCapabilities: domain.AllCapabilities(),
		Notifications:     DefaultNotificationPreferences(),
	}, nil
}

// Household renders the blueprint as the domain value HouseholdRepository.Create
// takes. ID and FXRateMode are left zero: the database assigns the ID, and
// fx_rate_mode keeps its column default of 'auto', which the CHECK constraint
// makes the only safe value to assume at creation time.
func (b HouseholdBlueprint) Household() domain.Household {
	return domain.Household{
		Name:                  b.Name,
		FamilyName:            b.FamilyName,
		PrimaryCurrency:       b.PrimaryCurrency,
		ShowSecondaryCurrency: b.ShowSecondaryCurrency,
		SecondaryCurrency:     b.SecondaryCurrency,
	}
}
```

- [ ] **Step 7: Run the blueprint test to verify it passes**

Run: `cd api && go test ./internal/usecase/ -run 'TestDefaultNotification|TestBlueprint' -v -count=1`
Expected: PASS, eight subtests.

- [ ] **Step 8: Add the new and changed ports**

In `api/internal/usecase/ports.go`:

Add to the `Mailer` interface:

```go
	// SendSignupLink mails the create-household link. There is no name
	// parameter: at sign-up-request time nobody has told us one, and inventing
	// a greeting from the local part of the address would read worse than
	// having none.
	SendSignupLink(ctx context.Context, to, url string) error
	// SendSignupForExistingAccount mails "you already have an account" with no
	// token. It is as load-bearing as SendSignupLink, not a courtesy: if only
	// the fresh-address branch sent mail, the *absence* of an email would tell
	// anyone who can observe the mailbox that the address is registered, which
	// is the oracle the identical 202 exists to prevent.
	SendSignupForExistingAccount(ctx context.Context, to, signInURL string) error
```

Add to `LoginAttemptRepository`:

```go
	// Prune deletes attempts older than before, including the
	// NULL-household_id rows an unknown-address attempt records -- which
	// ClearFailures cannot reach, because it is scoped WHERE household_id = $1
	// and that never matches NULL.
	//
	// The caller is responsible for a cutoff well outside
	// domain.LockoutPolicy.Window. Deleting a row still inside that window
	// would clear a live lockout: a security regression dressed as a cleanup.
	Prune(ctx context.Context, before time.Time) (int64, error)
```

Change `HouseholdRepository.Create`:

```go
	// Create writes a household from a fully-populated domain.Household.
	// It takes the value rather than (name, familyName) so no caller depends on
	// the table's currency column defaults -- Seed used to, silently, and a
	// self-serve household needs different values.
	//
	// h.ID is ignored (the database assigns it) and h.FXRateMode is ignored
	// (the column default 'auto' is the only value the CHECK constraint makes
	// safe to assume at creation).
	Create(ctx context.Context, h domain.Household) (domain.Household, error)
```

Add the sign-up port. Place it after `InviteRepository`:

```go
// SignupDetails is a pending sign-up, read back by token.
type SignupDetails struct {
	ID         string
	Email      string
	ExpiresAt  time.Time
	ConsumedAt *time.Time
}

// ProvisionedHousehold is what a successful provision produces.
type ProvisionedHousehold struct {
	UserID       string
	HouseholdID  string
	MembershipID string
}

type SignupRepository interface {
	Create(ctx context.Context, email string, tokenHash []byte, expiresAt time.Time) error
	ByTokenHash(ctx context.Context, tokenHash []byte) (SignupDetails, error)
	// CountForEmailSince counts sign-up requests for one address since a
	// cutoff. Unlike MagicLinkRepository.CountSince it does not join through
	// users -- there is no user to join to -- so it can report a non-zero count
	// for an address with no account, and must: a limit that could only be hit
	// by a registered address would itself distinguish the two.
	CountForEmailSince(ctx context.Context, email string, since time.Time) (int, error)
	// CountSince counts every sign-up request since a cutoff, for the global
	// daily mail ceiling. It reads the table rather than an in-memory counter
	// so restarting the API cannot reset the ceiling.
	CountSince(ctx context.Context, since time.Time) (int, error)
	// Provision creates the household, the owner user, the owner membership,
	// every builtin space and the notification preferences, and stamps the
	// signup consumed -- all in one transaction. Either all of it happens or
	// none of it does.
	//
	// The owner's email address is read from the signup row this transaction is
	// already touching; it is deliberately NOT a parameter. The address that
	// gets an account must be the one the mailed token actually proved, and
	// passing it in would let a caller substitute a different one between
	// SignupService.Complete's read and this write.
	//
	// A partial provision leaves a users row occupying users.email's unique
	// index with no membership under it, which makes that address permanently
	// unable to sign up again: a retry could never create a second user with
	// it. That is the same failure InviteRepository.Accept's doc comment
	// describes, and this method exists for the same reason.
	//
	// Returns domain.ErrTokenExpired, with nothing written, when the signup is
	// no longer usable -- consumed or expired. Like InviteRepository.Accept's
	// guarded UPDATE this collapses the two cases into one zero-rows result and
	// cannot tell them apart; SignupService.Complete's own TokenLifecycle read
	// is what distinguishes them for a caller, and this answer is authoritative
	// only for the race window between that read and this write.
	Provision(ctx context.Context, signupID, passwordHash string,
		b HouseholdBlueprint) (ProvisionedHousehold, error)
	// Prune deletes consumed and expired rows older than before.
	Prune(ctx context.Context, before time.Time) (int64, error)
}
```

- [ ] **Step 9: Build and fix every call site the compiler names**

Run: `cd api && go build ./... 2>&1 | head -20`
Expected: FAIL at `Households.Create` call sites and at the `SMTPMailer` (which no longer satisfies `Mailer`).

Fix `api/internal/usecase/seed.go`. Replace the `Households.Create` call and the
lines around it (lines 173-193) with:

```go
	blueprint, err := NewSignupBlueprint(householdName, "Andreas", "SGD")
	if err != nil {
		return domain.Household{}, "", fmt.Errorf("build the seed blueprint: %w", err)
	}
	// The design's household is the one place that keeps the dual-currency
	// display on: Andreas and Christine genuinely track SGD against IDR. A
	// self-serve household gets secondary == primary and the toggle off (see
	// NewSignupBlueprint), so these three are overridden here rather than
	// baked into the blueprint constructor.
	blueprint.FamilyName = familyName
	blueprint.SecondaryCurrency = "IDR"
	blueprint.ShowSecondaryCurrency = true

	household, err := d.Households.Create(ctx, blueprint.Household())
	if err != nil {
		return domain.Household{}, "", fmt.Errorf("create household: %w", err)
	}

	passwordHash, err := d.Hasher.Hash(DevPassword)
	if err != nil {
		return domain.Household{}, "", fmt.Errorf("hash development password: %w", err)
	}

	andreasMembership, err := domain.NewMembership("", household.ID, "",
		blueprint.OwnerRole, blueprint.OwnerCapabilities)
	if err != nil {
		return domain.Household{}, "", fmt.Errorf("build andreas's membership: %w", err)
	}

	andreas, _, err := d.Users.CreateWithMembership(ctx, AndreasEmail, passwordHash,
		blueprint.OwnerDisplayName, andreasMembership)
	if err != nil {
		return domain.Household{}, "", fmt.Errorf("create andreas: %w", err)
	}
```

Note that `NewSignupBlueprint` is reused here rather than a second constructor
for Seed: the three overrides above are the *whole* difference between the
design's household and a self-serve one, and writing them as three explicit
assignments makes that visible. A `NewSeedBlueprint` would hide it.

Also replace `Seed`'s notification-preferences call (lines 122-126) with:

```go
	if _, err := d.Notifications.Upsert(ctx, household.ID, DefaultNotificationPreferences()); err != nil {
		return SeedResult{}, fmt.Errorf("set notification preferences: %w", err)
	}
```

- [ ] **Step 10: Stub the two new mailer methods so the build passes**

Append to `api/internal/adapter/mail/smtp.go`, matching `SendMagicLink`'s shape
exactly (read lines 85-110 first and copy the structure):

```go
// SendSignupLink mails the create-household link. No name: the sign-up request
// carries only an address.
func (m *SMTPMailer) SendSignupLink(ctx context.Context, to, url string) error {
	body := "Welcome to Hearth.\n\n" +
		"Use this link to set up your household. It works once, and expires in 24 hours.\n\n" +
		url + "\n\n" +
		"If you did not ask for this, you can ignore this email -- nothing has been created.\n"
	return m.send(ctx, to, "Set up your Hearth household", body)
}

// SendSignupForExistingAccount answers a sign-up request for an address that
// already has an account. It carries no token, and it exists so that both
// branches of a sign-up request send mail -- if only the fresh branch did, the
// absence of an email would reveal that an address is registered.
func (m *SMTPMailer) SendSignupForExistingAccount(ctx context.Context, to, signInURL string) error {
	body := "You already have a Hearth account for this address.\n\n" +
		"Sign in here:\n\n" + signInURL + "\n\n" +
		"If you have forgotten your password, that page can email you a one-time sign-in link.\n"
	return m.send(ctx, to, "You already have a Hearth account", body)
}
```

- [ ] **Step 11: Add the test doubles the changed ports need**

In `api/internal/usecase/testdouble_test.go`:

- Change `householdDouble`'s `Create` (or whatever the existing household double
  is called — run `grep -n "Households\|householdDouble" internal/usecase/testdouble_test.go` to find it) to the new signature, storing every field of `h`.
- Add `Prune` to `loginAttemptDouble`, deleting from its recorded slice by `at`
  and returning the count.
- Add `SendSignupLink` and `SendSignupForExistingAccount` to the mailer double,
  each appending to its own recorded slice so a test can assert **which** mail
  was sent.
- Add a `signupDouble` implementing `usecase.SignupRepository`, with:
  - a `provisionCalls` counter and a `failNextProvision(err error)` hook,
  - `Provision` returning `domain.ErrTokenExpired` when the row is already
    consumed or expired, so the double honours the port's documented contract
    rather than only its happy path.

- [ ] **Step 12: Run the whole usecase suite**

Run: `cd api && go build ./... && go test ./internal/usecase/ -count=1`
Expected: PASS.

Run: `cd api && go test ./... -count=1`
Expected: PASS. `TestNewSMTPMailerWiresEveryConfigValue` still passes; the two
new methods are additive.

- [ ] **Step 13: Confirm the architecture rule still holds**

Run: `cd .. && make lint-arch`
Expected: PASS. `blueprint.go` imports `errors`, `strings` and
`internal/domain`; `password.go` imports `errors`.

- [ ] **Step 14: Commit**

```bash
git add api/internal/usecase/password.go api/internal/usecase/blueprint.go \
        api/internal/usecase/blueprint_test.go api/internal/usecase/ports.go \
        api/internal/usecase/invite.go api/internal/usecase/seed.go \
        api/internal/usecase/testdouble_test.go \
        api/internal/adapter/mail/smtp.go \
        api/internal/adapter/postgres/household_repo.go
git commit -m "feat(usecase): add HouseholdBlueprint, share the password rules, declare SignupRepository

The password floor and ceiling were named for invites but were never
invite-specific. One validatePassword now serves invite acceptance and sign-up,
so the two ways of creating an account cannot drift to different floors.

HouseholdBlueprint is the single definition of what a new household consists
of. Seed and Provision apply it differently on purpose -- Seed
step-idempotently so a partial run is retryable, Provision atomically so a
partial provision cannot permanently block an email address -- and sharing one
implementation would cost one of them that property.

HouseholdRepository.Create now takes a domain.Household. Seed was silently
depending on the currency column defaults, which is not a dependency a
self-serve household can share."
```

---

### Task 26: The Postgres sign-up repository and `Provision`'s transaction

**Files:**
- Create: `api/internal/adapter/postgres/signup_repo.go`, `api/internal/adapter/postgres/signup_repo_test.go`
- Modify: `api/internal/adapter/postgres/household_repo.go` (`Create` takes a `domain.Household`)
- Modify: `api/internal/adapter/postgres/loginattempt_repo.go` (add `Prune`)

**Interfaces:**
- Consumes: `usecase.SignupRepository` and `usecase.HouseholdBlueprint` (Task 25); `domain.BuiltinSpaces` and `initialOf` (Tasks 22, 24); the generated queries (Task 23).
- Produces: `postgres.NewSignupRepo(pool *pgxpool.Pool) *SignupRepo`, satisfying `usecase.SignupRepository`. `cmd/api/main.go` and `cmd/adminctl/main.go` wire it in Tasks 28-29.

- [ ] **Step 1: Rewrite `HouseholdRepository.Create` for the new signature**

Replace the `Create` method in `api/internal/adapter/postgres/household_repo.go`
(the one Task 23 left with hard-coded literals) with:

```go
// Create writes a household from a fully-populated domain.Household. h.ID is
// ignored -- the database assigns it -- and h.FXRateMode is ignored, because
// fx_rate_mode keeps its column default of 'auto' and the CHECK constraint
// makes that the only value safe to assume at creation time.
func (r *HouseholdRepo) Create(ctx context.Context, h domain.Household) (domain.Household, error) {
	row, err := r.q.CreateHousehold(ctx, sqlcgen.CreateHouseholdParams{
		Name:                  h.Name,
		FamilyName:            h.FamilyName,
		PrimaryCurrency:       h.PrimaryCurrency,
		ShowSecondaryCurrency: h.ShowSecondaryCurrency,
		SecondaryCurrency:     h.SecondaryCurrency,
	})
	if err != nil {
		return domain.Household{}, translate(err, "create household")
	}
	return toDomainHousehold(row.ID, row.Name, row.FamilyName, row.PrimaryCurrency,
		row.ShowSecondaryCurrency, row.SecondaryCurrency, row.FxRateMode), nil
}
```

If `toDomainHousehold`'s parameter list differs, check it:

Run: `cd api && grep -n -A6 "func toDomainHousehold" internal/adapter/postgres/convert.go`

and match the call to the actual signature.

- [ ] **Step 2: Add `Prune` to the login-attempt repository**

Append to `api/internal/adapter/postgres/loginattempt_repo.go`:

```go
// Prune deletes attempts older than before, including the NULL-household_id
// rows an unknown-address sign-in attempt records. ClearFailures cannot reach
// those: it is scoped WHERE household_id = $1, which never matches NULL, so
// until this existed the rows a stranger generated were deleted by nothing at
// all.
//
// before must sit well outside domain.LockoutPolicy.Window. Deleting a row
// still inside that window would clear a live lockout, which would let someone
// hold a household open by pruning rather than by waiting -- a security
// regression dressed as a cleanup. The caller (adminctl) enforces the floor.
func (r *LoginAttemptRepo) Prune(ctx context.Context, before time.Time) (int64, error) {
	deleted, err := r.q.PruneLoginAttempts(ctx, timestamptz(before))
	if err != nil {
		return 0, translate(err, "prune login attempts")
	}
	return deleted, nil
}
```

- [ ] **Step 3: Write the failing repository test**

Create `api/internal/adapter/postgres/signup_repo_test.go`. The atomicity test is
the important one — read it before the others.

```go
package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestSignupRepoRoundTrip(t *testing.T) {
	pool := newTestPool(t)
	repo := NewSignupRepo(pool)
	ctx := context.Background()

	expires := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	hash := []byte("round-trip-hash-aaaaaaaaaaaaaaaa")
	if err := repo.Create(ctx, "Round@Trip.test", hash, expires); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.ByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	// citext preserves the stored casing; the lookup is what is
	// case-insensitive. Assert the address survives as typed.
	if got.Email != "Round@Trip.test" {
		t.Fatalf("Email = %q, want the address as given", got.Email)
	}
	if got.ConsumedAt != nil {
		t.Fatalf("ConsumedAt = %v, want nil on a fresh signup", got.ConsumedAt)
	}
	if !got.ExpiresAt.Equal(expires) {
		t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, expires)
	}
}

func TestSignupRepoByTokenHashReportsNotFound(t *testing.T) {
	repo := NewSignupRepo(newTestPool(t))
	_, err := repo.ByTokenHash(context.Background(), []byte("no-such-hash-bbbbbbbbbbbbbbbbbbb"))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v, want domain.ErrNotFound", err)
	}
}

func TestSignupRepoCountForEmailSinceDoesNotJoinThroughUsers(t *testing.T) {
	pool := newTestPool(t)
	repo := NewSignupRepo(pool)
	ctx := context.Background()

	// The address has no account at all. The count must still be 2: a limit
	// that could only be reached by a registered address would itself tell a
	// caller which addresses are registered.
	for i, hash := range [][]byte{
		[]byte("count-one-cccccccccccccccccccccc"),
		[]byte("count-two-dddddddddddddddddddddd"),
	} {
		if err := repo.Create(ctx, "stranger@example.test", hash, time.Now().Add(time.Hour)); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	got, err := repo.CountForEmailSince(ctx, "stranger@example.test", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CountForEmailSince: %v", err)
	}
	if got != 2 {
		t.Fatalf("CountForEmailSince = %d, want 2", got)
	}
}

func TestSignupRepoProvisionCreatesTheWholeHousehold(t *testing.T) {
	pool := newTestPool(t)
	repo := NewSignupRepo(pool)
	ctx := context.Background()

	hash := []byte("provision-hash-eeeeeeeeeeeeeeeee")
	if err := repo.Create(ctx, "founder@example.test", hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	signup, err := repo.ByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}

	blueprint, err := usecase.NewSignupBlueprint("Ade & Kris", "Ade", "SGD")
	if err != nil {
		t.Fatalf("NewSignupBlueprint: %v", err)
	}

	got, err := repo.Provision(ctx, signup.ID, "hashed-password", blueprint)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if got.UserID == "" || got.HouseholdID == "" || got.MembershipID == "" {
		t.Fatalf("Provision returned %+v, want every id populated", got)
	}

	t.Run("the user carries the verified address, not one passed in", func(t *testing.T) {
		var email, initial string
		if err := pool.QueryRow(ctx,
			`SELECT email, avatar_initial FROM users WHERE id = $1`, got.UserID).Scan(&email, &initial); err != nil {
			t.Fatalf("query user: %v", err)
		}
		if email != "founder@example.test" {
			t.Fatalf("email = %q, want the address from the signup row", email)
		}
		if initial != "A" {
			t.Fatalf("avatar_initial = %q, want A", initial)
		}
	})

	t.Run("the owner holds every capability", func(t *testing.T) {
		var role string
		var caps []string
		if err := pool.QueryRow(ctx,
			`SELECT role, capabilities FROM memberships WHERE id = $1`, got.MembershipID).Scan(&role, &caps); err != nil {
			t.Fatalf("query membership: %v", err)
		}
		if role != "owner" {
			t.Fatalf("role = %q, want owner", role)
		}
		if len(caps) != len(domain.AllCapabilities()) {
			t.Fatalf("capabilities = %v, want all %d", caps, len(domain.AllCapabilities()))
		}
	})

	t.Run("all three builtin spaces exist, in position order", func(t *testing.T) {
		rows, err := pool.Query(ctx,
			`SELECT key FROM spaces WHERE household_id = $1 ORDER BY position`, got.HouseholdID)
		if err != nil {
			t.Fatalf("query spaces: %v", err)
		}
		defer rows.Close()
		var keys []string
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				t.Fatalf("scan: %v", err)
			}
			keys = append(keys, key)
		}
		want := []string{"money", "marriage", "family"}
		if len(keys) != len(want) {
			t.Fatalf("keys = %v, want %v", keys, want)
		}
		for i := range want {
			if keys[i] != want[i] {
				t.Fatalf("keys = %v, want %v", keys, want)
			}
		}
	})

	t.Run("notification preferences are on", func(t *testing.T) {
		var bills, overspend, retro, digest bool
		if err := pool.QueryRow(ctx,
			`SELECT bill_reminders, overspend_alerts, retro_reminder, weekly_digest
			 FROM notification_preferences WHERE household_id = $1`, got.HouseholdID).
			Scan(&bills, &overspend, &retro, &digest); err != nil {
			t.Fatalf("query preferences: %v", err)
		}
		if !bills || !overspend || !retro || !digest {
			t.Fatal("want every notification flag true")
		}
	})

	t.Run("secondary currency mirrors primary and the toggle is off", func(t *testing.T) {
		var primary, secondary string
		var show bool
		if err := pool.QueryRow(ctx,
			`SELECT primary_currency, secondary_currency, show_secondary_currency
			 FROM households WHERE id = $1`, got.HouseholdID).Scan(&primary, &secondary, &show); err != nil {
			t.Fatalf("query household: %v", err)
		}
		if primary != "SGD" || secondary != "SGD" || show {
			t.Fatalf("got primary=%q secondary=%q show=%v, want SGD/SGD/false", primary, secondary, show)
		}
	})

	t.Run("the signup is consumed, so it cannot be used twice", func(t *testing.T) {
		_, err := repo.Provision(ctx, signup.ID, "hashed-password", blueprint)
		if !errors.Is(err, domain.ErrTokenExpired) {
			t.Fatalf("second Provision error = %v, want domain.ErrTokenExpired", err)
		}
	})
}

// This is the test the whole transaction exists for. A partial provision leaves
// a users row occupying users.email's unique index with no membership under it,
// which makes that address permanently unable to sign up again -- no retry
// could ever create a second user with it.
//
// The failure is forced with a real constraint rather than a mock: an owner
// holding fewer than every capability violates the memberships
// owners_hold_all_capabilities CHECK, which fires *after* the household and the
// user have already been inserted in the same transaction. That is exactly the
// mid-transaction position a partial write would occupy.
func TestSignupRepoProvisionIsAllOrNothing(t *testing.T) {
	pool := newTestPool(t)
	repo := NewSignupRepo(pool)
	ctx := context.Background()

	hash := []byte("atomic-hash-fffffffffffffffffff")
	if err := repo.Create(ctx, "atomic@example.test", hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	signup, err := repo.ByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}

	blueprint, err := usecase.NewSignupBlueprint("Doomed household", "Ade", "SGD")
	if err != nil {
		t.Fatalf("NewSignupBlueprint: %v", err)
	}
	// An owner with only one capability. domain.NewMembership would refuse this
	// too, but Provision is handed a blueprint directly, so the database CHECK
	// is the gate being exercised here.
	blueprint.OwnerCapabilities = domain.Capabilities{domain.CapCalendar}

	if _, err := repo.Provision(ctx, signup.ID, "hashed-password", blueprint); err == nil {
		t.Fatal("Provision succeeded with an under-capable owner; the CHECK constraint did not fire")
	}

	t.Run("no household survived", func(t *testing.T) {
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM households WHERE name = $1`, "Doomed household").Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 0 {
			t.Fatalf("found %d households, want 0", count)
		}
	})

	t.Run("no user survived, so the address is not blocked", func(t *testing.T) {
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM users WHERE email = $1`, "atomic@example.test").Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 0 {
			t.Fatalf("found %d users, want 0 -- a surviving row permanently blocks this address", count)
		}
	})

	t.Run("the signup is still unconsumed, so a retry is possible", func(t *testing.T) {
		again, err := repo.ByTokenHash(ctx, hash)
		if err != nil {
			t.Fatalf("ByTokenHash: %v", err)
		}
		if again.ConsumedAt != nil {
			t.Fatalf("ConsumedAt = %v, want nil -- a consumed signup after a failed provision is unrecoverable",
				again.ConsumedAt)
		}
	})
}

func TestSignupRepoProvisionRefusesAnExpiredSignup(t *testing.T) {
	pool := newTestPool(t)
	repo := NewSignupRepo(pool)
	ctx := context.Background()

	hash := []byte("expired-hash-gggggggggggggggggg")
	if err := repo.Create(ctx, "late@example.test", hash, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	signup, err := repo.ByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	blueprint, err := usecase.NewSignupBlueprint("Too late", "Ade", "SGD")
	if err != nil {
		t.Fatalf("NewSignupBlueprint: %v", err)
	}
	if _, err := repo.Provision(ctx, signup.ID, "hashed-password", blueprint); !errors.Is(err, domain.ErrTokenExpired) {
		t.Fatalf("error = %v, want domain.ErrTokenExpired", err)
	}
}

func TestSignupRepoPruneLeavesLiveRows(t *testing.T) {
	pool := newTestPool(t)
	repo := NewSignupRepo(pool)
	ctx := context.Background()

	// One live, one expired. Both created long enough ago to be inside the
	// prune cutoff, so only liveness decides.
	if err := repo.Create(ctx, "live@example.test", []byte("live-hash-hhhhhhhhhhhhhhhhhhhhh"),
		time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("Create live: %v", err)
	}
	if err := repo.Create(ctx, "dead@example.test", []byte("dead-hash-iiiiiiiiiiiiiiiiiiiii"),
		time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("Create expired: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE signups SET created_at = now() - interval '40 days'`); err != nil {
		t.Fatalf("age the rows: %v", err)
	}

	deleted, err := repo.Prune(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("Prune deleted %d, want 1 -- only the expired row", deleted)
	}
	if _, err := repo.ByTokenHash(ctx, []byte("live-hash-hhhhhhhhhhhhhhhhhhhhh")); err != nil {
		t.Fatalf("the live signup was pruned: %v", err)
	}
}

// The reason PruneLoginAttempts exists: ClearFailures is scoped
// WHERE household_id = $1, which never matches the NULL rows an
// unknown-address attempt records.
func TestLoginAttemptRepoPruneReachesNullHouseholdRows(t *testing.T) {
	pool := newTestPool(t)
	repo := NewLoginAttemptRepo(pool)
	ctx := context.Background()

	old := time.Now().Add(-40 * 24 * time.Hour)
	if err := repo.Record(ctx, nil, nil, "stranger@example.test", false, old); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// ClearFailures cannot touch it -- there is no household to scope by.
	deleted, err := repo.Prune(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("Prune deleted %d, want 1", deleted)
	}
}

func TestLoginAttemptRepoPruneKeepsRecentRows(t *testing.T) {
	pool := newTestPool(t)
	repo := NewLoginAttemptRepo(pool)
	ctx := context.Background()

	// Inside the lockout window. Pruning this would clear a live lockout.
	if err := repo.Record(ctx, nil, nil, "recent@example.test", false, time.Now()); err != nil {
		t.Fatalf("Record: %v", err)
	}
	deleted, err := repo.Prune(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("Prune deleted %d recent rows, want 0", deleted)
	}
}
```

If `NewLoginAttemptRepo` is named differently, check:

Run: `cd api && grep -n "func New.*LoginAttempt" internal/adapter/postgres/loginattempt_repo.go`

- [ ] **Step 4: Run it to verify it fails**

Run: `cd api && go test ./internal/adapter/postgres/ -run TestSignupRepo -v -count=1`
Expected: FAIL — `undefined: NewSignupRepo`.

- [ ] **Step 5: Write the repository**

Create `api/internal/adapter/postgres/signup_repo.go`:

```go
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// SignupRepo implements usecase.SignupRepository. It holds the pool as well as
// the generated queries because Provision needs a transaction -- the same
// reason InviteRepo does.
type SignupRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewSignupRepo(pool *pgxpool.Pool) *SignupRepo {
	return &SignupRepo{pool: pool, q: sqlcgen.New(pool)}
}

func (r *SignupRepo) Create(ctx context.Context, email string, tokenHash []byte, expiresAt time.Time) error {
	return translate(r.q.CreateSignup(ctx, sqlcgen.CreateSignupParams{
		Email:     email,
		TokenHash: tokenHash,
		ExpiresAt: timestamptz(expiresAt),
	}), "create signup")
}

func (r *SignupRepo) ByTokenHash(ctx context.Context, tokenHash []byte) (usecase.SignupDetails, error) {
	row, err := r.q.GetSignupByTokenHash(ctx, tokenHash)
	if err != nil {
		return usecase.SignupDetails{}, translate(err, "get signup by token hash")
	}
	return usecase.SignupDetails{
		ID:         uuidToString(row.ID),
		Email:      row.Email,
		ExpiresAt:  timeOf(row.ExpiresAt),
		ConsumedAt: timePtrOf(row.ConsumedAt),
	}, nil
}

func (r *SignupRepo) CountForEmailSince(ctx context.Context, email string, since time.Time) (int, error) {
	n, err := r.q.CountSignupsForEmailSince(ctx, sqlcgen.CountSignupsForEmailSinceParams{
		Email:     email,
		CreatedAt: timestamptz(since),
	})
	if err != nil {
		return 0, translate(err, "count signups for email")
	}
	return int(n), nil
}

func (r *SignupRepo) CountSince(ctx context.Context, since time.Time) (int, error) {
	n, err := r.q.CountSignupsSince(ctx, timestamptz(since))
	if err != nil {
		return 0, translate(err, "count signups")
	}
	return int(n), nil
}

// Provision creates a whole household in one transaction: the household, the
// owner, their membership, the three builtin spaces and the notification
// preferences, with the signup stamped consumed first.
//
// The ordering matters. ConsumeSignup runs before any insert, so its guarded
// UPDATE is what serialises two concurrent completions of the same token -- the
// loser gets zero rows and returns domain.ErrTokenExpired having written
// nothing. Doing it last would let both callers create a household.
//
// Do not decompose this into separate repository calls. A failure between them
// would leave a users row occupying users.email's unique index with no
// membership under it, and that address could then never sign up again: a retry
// cannot create a second user with the same email, so there is no path forward
// short of manual SQL. InviteRepository.Accept exists for the identical reason.
func (r *SignupRepo) Provision(ctx context.Context, signupID, passwordHash string,
	b usecase.HouseholdBlueprint) (usecase.ProvisionedHousehold, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return usecase.ProvisionedHousehold{}, fmt.Errorf("begin provision transaction: %w", err)
	}
	// A no-op once Commit has succeeded; the error from a post-commit Rollback
	// is deliberately discarded, matching the standard defer-rollback pattern
	// for pgx transactions.
	defer func() { _ = tx.Rollback(ctx) }()

	q := r.q.WithTx(tx)

	// The guard is in the SQL (consumed_at IS NULL AND expires_at > now()), so
	// this single statement both claims the signup and tells us whether it was
	// claimable. It returns the email, which is how the verified address reaches
	// the user row without a caller being able to substitute a different one.
	claimed, err := q.ConsumeSignup(ctx, uuid(signupID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Consumed or expired -- indistinguishable here, deliberately.
			// SignupService.Complete's own TokenLifecycle read is what tells the
			// two apart for a caller; this answer is authoritative only for the
			// race between that read and this write.
			return usecase.ProvisionedHousehold{}, domain.ErrTokenExpired
		}
		return usecase.ProvisionedHousehold{}, translate(err, "consume signup")
	}

	householdRow, err := q.CreateHousehold(ctx, sqlcgen.CreateHouseholdParams{
		Name:                  b.Name,
		FamilyName:            b.FamilyName,
		PrimaryCurrency:       b.PrimaryCurrency,
		ShowSecondaryCurrency: b.ShowSecondaryCurrency,
		SecondaryCurrency:     b.SecondaryCurrency,
	})
	if err != nil {
		return usecase.ProvisionedHousehold{}, translate(err, "create household for signup")
	}
	householdID := uuidToString(householdRow.ID)

	userRow, err := q.CreateUser(ctx, sqlcgen.CreateUserParams{
		Email:         nullableText(claimed.Email),
		PasswordHash:  nullableText(passwordHash),
		DisplayName:   b.OwnerDisplayName,
		AvatarInitial: initialOf(b.OwnerDisplayName),
	})
	if err != nil {
		// A unique violation here means the address gained an account between
		// SignupService.Complete's check and this insert -- two live tokens for
		// one address, both completed. translate maps it to
		// domain.ErrAlreadyExists, which MapDomainError answers 409.
		return usecase.ProvisionedHousehold{}, translate(err, "create owner for signup")
	}

	membershipRow, err := q.CreateMembership(ctx, sqlcgen.CreateMembershipParams{
		HouseholdID:  householdRow.ID,
		UserID:       userRow.ID,
		Role:         string(b.OwnerRole),
		Capabilities: b.OwnerCapabilities.Strings(),
	})
	if err != nil {
		return usecase.ProvisionedHousehold{}, translate(err, "create owner membership for signup")
	}

	// domain.BuiltinSpaces is called here, inside the transaction, because it
	// needs the household ID -- which does not exist until the insert above.
	// The knowledge of which spaces a household starts with stays in domain;
	// this only executes it.
	for _, s := range domain.BuiltinSpaces(householdID) {
		if _, err := q.CreateSpace(ctx, sqlcgen.CreateSpaceParams{
			HouseholdID:        householdRow.ID,
			Key:                s.Key,
			Name:               s.Name,
			Visibility:         string(s.Visibility),
			Position:           int32(s.Position),
			IsBuiltin:          s.IsBuiltin,
			RequiredCapability: string(s.RequiredCapability),
		}); err != nil {
			return usecase.ProvisionedHousehold{}, translate(err, fmt.Sprintf("create builtin space %q", s.Key))
		}
	}

	if _, err := q.UpsertNotificationPreferences(ctx, sqlcgen.UpsertNotificationPreferencesParams{
		HouseholdID:     householdRow.ID,
		BillReminders:   b.Notifications.BillReminders,
		OverspendAlerts: b.Notifications.OverspendAlerts,
		RetroReminder:   b.Notifications.RetroReminder,
		WeeklyDigest:    b.Notifications.WeeklyDigest,
	}); err != nil {
		return usecase.ProvisionedHousehold{}, translate(err, "set notification preferences for signup")
	}

	if err := tx.Commit(ctx); err != nil {
		return usecase.ProvisionedHousehold{}, fmt.Errorf("commit provision transaction: %w", err)
	}

	return usecase.ProvisionedHousehold{
		UserID:       uuidToString(userRow.ID),
		HouseholdID:  householdID,
		MembershipID: uuidToString(membershipRow.ID),
	}, nil
}

func (r *SignupRepo) Prune(ctx context.Context, before time.Time) (int64, error) {
	deleted, err := r.q.PruneSignups(ctx, timestamptz(before))
	if err != nil {
		return 0, translate(err, "prune signups")
	}
	return deleted, nil
}
```

- [ ] **Step 6: Run the repository tests**

Run: `cd api && go test ./internal/adapter/postgres/ -run 'TestSignupRepo|TestLoginAttemptRepoPrune' -v -count=1`
Expected: PASS, every subtest.

- [ ] **Step 7: Mutation-check the transaction**

The atomicity test is the one that must not be able to pass against broken code.
Temporarily move the `tx.Commit(ctx)` call to immediately after the
`CreateUser` block, leaving the rest of the function as-is.

Run: `cd api && go test ./internal/adapter/postgres/ -run TestSignupRepoProvisionIsAllOrNothing -v -count=1`
Expected: FAIL on `no_user_survived` — the user is now committed before the
membership fails, which is precisely the permanently-blocked address the
transaction prevents.

Restore the `Commit` to the end and re-run. Expected: PASS.

- [ ] **Step 8: Mutation-check the consume-first ordering**

Temporarily move the `ConsumeSignup` call to just before `tx.Commit`.

Run: `cd api && go test ./internal/adapter/postgres/ -run 'TestSignupRepoProvisionRefusesAnExpiredSignup|TestSignupRepoProvisionCreatesTheWholeHousehold' -v -count=1`
Expected: FAIL — an expired signup now provisions a whole household before the
guard runs.

Restore and re-run. Expected: PASS.

- [ ] **Step 9: Full suite**

Run: `cd api && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add api/internal/adapter/postgres/signup_repo.go \
        api/internal/adapter/postgres/signup_repo_test.go \
        api/internal/adapter/postgres/household_repo.go \
        api/internal/adapter/postgres/loginattempt_repo.go
git commit -m "feat(postgres): provision a whole household in one transaction

Provision creates the household, owner, membership, three builtin spaces and
notification preferences together, with the signup consumed first so its
guarded UPDATE serialises two concurrent completions of the same token.

Consuming first is not a detail: doing it last would let both callers of one
token create a household. And the transaction is not caution -- a partial
provision leaves a users row occupying users.email's unique index with no
membership under it, and that address could then never sign up again, because
no retry can create a second user with the same email.

The verified address is read from the signup row inside the transaction rather
than passed in, so nothing can substitute a different address between the
caller's check and this write."
```

---

### Task 27: `SignupService` — request, preview, complete

**Files:**
- Create: `api/internal/usecase/signup.go`, `api/internal/usecase/signup_test.go`
- Modify: `api/internal/usecase/testdouble_test.go` (add the ordered read log described in Step 1)

**Interfaces:**
- Consumes: `SignupRepository`, `Mailer`'s two new methods, `validatePassword`, `NewSignupBlueprint`, `domain.TokenLifecycle`, and the package-level `issueSession` from `auth.go`.
- Produces:
  - `usecase.NewSignupService(d SignupDeps) *SignupService`
  - `(*SignupService).Request(ctx context.Context, email string) error` — always nil.
  - `(*SignupService).Preview(ctx context.Context, token string) (SignupPreview, error)`, `SignupPreview{Email string}`.
  - `(*SignupService).Complete(ctx context.Context, token, householdName, displayName, currency, password string) (SignInResult, error)`
  - `usecase.ErrSignupAlreadyUsed`
  - Exported constants `SignupTTL = 24 * time.Hour`, and unexported `signupPerHourLimit = 3`, `signupGlobalDailyLimit = 200`.

- [ ] **Step 1: Give the test doubles an ordered read log**

In `api/internal/usecase/testdouble_test.go`, add a shared recorder and have the
three doubles the sign-up request path touches append to it:

```go
// readLog records the sequence of repository reads a service performed, in
// order. SignupService.Request's contract is that the same reads happen, in the
// same order, on every branch -- a read that is *skipped* on one branch is the
// exact defect RequestMagicLink shipped with, and no assertion about return
// values can catch it. Only an ordered log can.
type readLog struct{ calls []string }

func (l *readLog) record(name string) { l.calls = append(l.calls, name) }
func (l *readLog) seq() []string      { return l.calls }
func (l *readLog) reset()             { l.calls = nil }
```

Wire it into `userDouble.ByEmail` (`d.log.record("Users.ByEmail")`) and into the
new `signupDouble`'s `CountSince` and `CountForEmailSince`. Give each double a
`log *readLog` field set by the test.

`signupDouble` must implement `usecase.SignupRepository` plus exactly these
test-only methods, which the tests in Step 2 call by name:

```go
type signupRow struct {
	id, email  string
	tokenHash  []byte
	expiresAt  time.Time
	consumedAt *time.Time
}

type signupDouble struct {
	log *readLog

	rows        []signupRow
	nextID      int
	creates     int
	provisions  int
	lastPwHash  string
	emailCounts map[string]int // overrides for CountForEmailSince
	globalCount int            // override for CountSince
	failCreate  error
	failProvide error
}

// Test-only surface, all called from signup_test.go:
func (d *signupDouble) setEmailCount(email string, n int)              // force CountForEmailSince
func (d *signupDouble) setGlobalCount(n int)                           // force CountSince
func (d *signupDouble) markConsumed(tokenHash []byte, at time.Time)    // stamp a row consumed
func (d *signupDouble) failNextCreate(err error)                       // one-shot Create failure
func (d *signupDouble) failNextProvision(err error)                    // one-shot Provision failure
func (d *signupDouble) createCount() int                               // rows written
func (d *signupDouble) provisionCalls() int                            // Provision invocations
func (d *signupDouble) lastProvisionPasswordHash() string               // what Provision was handed
```

`Provision` on the double must honour the port's documented contract, not just
its happy path: return `domain.ErrTokenExpired` when the row is already consumed
or expired, and increment `provisions` **before** any early return, so
`provisionCalls()` distinguishes "was not called" from "was called and refused".

The mailer double needs:

```go
type sentMail struct{ to, url string }

type mailerDouble struct {
	mu                     sync.Mutex
	magicLinks             []sentMail
	invites                []sentMail
	signupLinks            []sentMail
	existingAccountNotices []sentMail
	sendErr                error
	sent                   chan struct{} // buffered, signalled after every send attempt
}

func (d *mailerDouble) failEverySend(err error)
func (d *mailerDouble) waitForSends(t *testing.T, n int)
func (d *mailerDouble) assertNoSendsWithin(t *testing.T, d time.Duration)
```

Every field read by a test is guarded by `mu`, because `sendAsync` writes them
from a goroutine — the `-race` run in Step 8 fails otherwise. `waitForSends`
receives `n` times from `sent` with a generous timeout; `assertNoSendsWithin`
does a `select` over `sent` and `time.After(d)` and fails if anything arrives.
That bounded wait is the one place a test may wait on the clock, and only
because proving a *negative* about an asynchronous send has no alternative.

`seqTokens` needs one addition:

```go
func (t *seqTokens) failNext(err error) // one-shot NewToken failure
```

- [ ] **Step 2: Write the failing oracle test**

Create `api/internal/usecase/signup_test.go`. This first test is the reason the
service is shaped the way it is:

```go
package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// wantReadSequence is the order every branch of Request must produce. It is
// asserted as an exact sequence, not a set: the point is that no branch skips a
// read and no branch reorders them.
var wantReadSequence = []string{
	"Signups.CountSince",
	"Signups.CountForEmailSince",
	"Users.ByEmail",
}

func TestSignupRequestIsIndistinguishableAcrossBranches(t *testing.T) {
	type branch struct {
		name  string
		email string
		setUp func(t *testing.T, f *signupFixture)
	}

	branches := []branch{
		{
			name:  "fresh address",
			email: "fresh@example.test",
			setUp: func(*testing.T, *signupFixture) {},
		},
		{
			name:  "address that already has an account",
			email: "taken@example.test",
			setUp: func(t *testing.T, f *signupFixture) {
				f.users.put(usecase.StoredUser{
					User:         domain.User{ID: "u1", Email: "taken@example.test", DisplayName: "Ade"},
					PasswordHash: "hashed:whatever",
				})
			},
		},
		{
			name:  "address at its hourly limit",
			email: "busy@example.test",
			setUp: func(t *testing.T, f *signupFixture) {
				f.signups.setEmailCount("busy@example.test", 3)
			},
		},
		{
			name:  "global daily mail ceiling reached",
			email: "unlucky@example.test",
			setUp: func(t *testing.T, f *signupFixture) {
				f.signups.setGlobalCount(1000)
			},
		},
		{
			name:  "mailer is down",
			email: "relay@example.test",
			setUp: func(t *testing.T, f *signupFixture) {
				f.mailer.failEverySend(errors.New("relay refused"))
			},
		},
		{
			name:  "token generation fails",
			email: "entropy@example.test",
			setUp: func(t *testing.T, f *signupFixture) {
				f.tokens.failNext(errors.New("entropy exhausted"))
			},
		},
		{
			name:  "the signup insert fails",
			email: "dbdown@example.test",
			setUp: func(t *testing.T, f *signupFixture) {
				f.signups.failNextCreate(errors.New("statement timeout"))
			},
		},
	}

	for _, b := range branches {
		t.Run(b.name, func(t *testing.T) {
			f := newSignupFixture(t)
			b.setUp(t, f)

			// Every branch returns nil. Not "returns nil unless the mailer
			// broke" -- an error on one branch only is a discrete yes/no oracle,
			// cheaper to exploit than any timing measurement.
			if err := f.svc.Request(context.Background(), b.email); err != nil {
				t.Fatalf("Request returned %v, want nil", err)
			}

			got := f.log.seq()
			if len(got) != len(wantReadSequence) {
				t.Fatalf("read sequence = %v, want %v", got, wantReadSequence)
			}
			for i := range wantReadSequence {
				if got[i] != wantReadSequence[i] {
					t.Fatalf("read sequence = %v, want %v", got, wantReadSequence)
				}
			}
		})
	}
}

// The registered branch must send mail too. If only the fresh branch did, the
// absence of an email would tell anyone who can read the mailbox that the
// address is registered -- the oracle the identical return value exists to
// prevent.
func TestSignupRequestMailsBothBranches(t *testing.T) {
	t.Run("fresh address gets a create-household link", func(t *testing.T) {
		f := newSignupFixture(t)
		if err := f.svc.Request(context.Background(), "fresh@example.test"); err != nil {
			t.Fatalf("Request: %v", err)
		}
		f.mailer.waitForSends(t, 1)
		if n := len(f.mailer.signupLinks); n != 1 {
			t.Fatalf("sent %d signup links, want 1", n)
		}
		if n := len(f.mailer.existingAccountNotices); n != 0 {
			t.Fatalf("sent %d existing-account notices, want 0", n)
		}
		if !strings.Contains(f.mailer.signupLinks[0].url, "/sign-up/") {
			t.Fatalf("link url = %q, want it to contain /sign-up/", f.mailer.signupLinks[0].url)
		}
	})

	t.Run("registered address gets an existing-account notice with no token", func(t *testing.T) {
		f := newSignupFixture(t)
		f.users.put(usecase.StoredUser{
			User:         domain.User{ID: "u1", Email: "taken@example.test", DisplayName: "Ade"},
			PasswordHash: "hashed:whatever",
		})
		if err := f.svc.Request(context.Background(), "taken@example.test"); err != nil {
			t.Fatalf("Request: %v", err)
		}
		f.mailer.waitForSends(t, 1)
		if n := len(f.mailer.existingAccountNotices); n != 1 {
			t.Fatalf("sent %d existing-account notices, want 1", n)
		}
		if n := len(f.mailer.signupLinks); n != 0 {
			t.Fatalf("sent %d signup links to a registered address, want 0", n)
		}
		// No signup row was written, so there is no token that could provision
		// a second household for this address.
		if f.signups.createCount() != 0 {
			t.Fatalf("wrote %d signup rows for a registered address, want 0", f.signups.createCount())
		}
	})

	t.Run("a rate-limited address gets no mail at all", func(t *testing.T) {
		// Both branches are gated by the limit, deliberately: if only the fresh
		// branch were, someone could flood a registered address's inbox.
		for _, tc := range []struct {
			name  string
			email string
			setUp func(f *signupFixture)
		}{
			{"fresh but limited", "fresh@example.test", func(f *signupFixture) {
				f.signups.setEmailCount("fresh@example.test", 3)
			}},
			{"registered and limited", "taken@example.test", func(f *signupFixture) {
				f.users.put(usecase.StoredUser{
					User:         domain.User{ID: "u1", Email: "taken@example.test", DisplayName: "Ade"},
					PasswordHash: "hashed:whatever",
				})
				f.signups.setEmailCount("taken@example.test", 3)
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newSignupFixture(t)
				tc.setUp(f)
				if err := f.svc.Request(context.Background(), tc.email); err != nil {
					t.Fatalf("Request: %v", err)
				}
				f.mailer.assertNoSendsWithin(t, 100*time.Millisecond)
			})
		}
	})
}

func TestSignupPreview(t *testing.T) {
	t.Run("returns the address a live token was issued for", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "founder@example.test", f.clock.Now().Add(usecase.SignupTTL))
		got, err := f.svc.Preview(context.Background(), token)
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		if got.Email != "founder@example.test" {
			t.Fatalf("Email = %q, want founder@example.test", got.Email)
		}
	})

	t.Run("an expired token reports ErrTokenExpired", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "late@example.test", f.clock.Now().Add(-time.Minute))
		if _, err := f.svc.Preview(context.Background(), token); !errors.Is(err, domain.ErrTokenExpired) {
			t.Fatalf("error = %v, want domain.ErrTokenExpired", err)
		}
	})

	t.Run("a consumed token reports ErrSignupAlreadyUsed, not expiry", func(t *testing.T) {
		// The distinction is the point: "you already used this, sign in" and
		// "this lapsed, start again" need different answers.
		f := newSignupFixture(t)
		token := f.issueSignup(t, "done@example.test", f.clock.Now().Add(usecase.SignupTTL))
		f.signups.markConsumed(f.tokens.HashToken(token), f.clock.Now())
		if _, err := f.svc.Preview(context.Background(), token); !errors.Is(err, usecase.ErrSignupAlreadyUsed) {
			t.Fatalf("error = %v, want usecase.ErrSignupAlreadyUsed", err)
		}
	})

	t.Run("an unknown token reports ErrNotFound", func(t *testing.T) {
		f := newSignupFixture(t)
		if _, err := f.svc.Preview(context.Background(), "never-issued"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("error = %v, want domain.ErrNotFound", err)
		}
	})
}

func TestSignupComplete(t *testing.T) {
	t.Run("provisions and issues a session", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "founder@example.test", f.clock.Now().Add(usecase.SignupTTL))

		got, err := f.svc.Complete(context.Background(), token, "Ade & Kris", "Ade", "SGD", "a-long-enough-password")
		if err != nil {
			t.Fatalf("Complete: %v", err)
		}
		if got.SessionToken == "" {
			t.Fatal("SessionToken is empty")
		}
		if got.UserID == "" || got.HouseholdID == "" {
			t.Fatalf("got %+v, want UserID and HouseholdID populated", got)
		}
		// Issued through the same issueSession sign-in uses, so the expiry
		// schedule is identical.
		if want := f.clock.Now().Add(f.sessionTTL); !got.ExpiresAt.Equal(want) {
			t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, want)
		}
		if f.sessions.count() != 1 {
			t.Fatalf("created %d sessions, want 1", f.sessions.count())
		}
	})

	t.Run("the password is hashed, never stored raw", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "founder@example.test", f.clock.Now().Add(usecase.SignupTTL))
		if _, err := f.svc.Complete(context.Background(), token, "Ade & Kris", "Ade", "SGD", "a-long-enough-password"); err != nil {
			t.Fatalf("Complete: %v", err)
		}
		if got := f.signups.lastProvisionPasswordHash(); got != "hashed:a-long-enough-password" {
			t.Fatalf("password hash = %q, want the hasher's output", got)
		}
	})

	t.Run("a short password is refused before anything is written", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "founder@example.test", f.clock.Now().Add(usecase.SignupTTL))
		if _, err := f.svc.Complete(context.Background(), token, "Ade & Kris", "Ade", "SGD", "short"); !errors.Is(err, usecase.ErrPasswordTooShort) {
			t.Fatalf("error = %v, want ErrPasswordTooShort", err)
		}
		if f.signups.provisionCalls() != 0 {
			t.Fatal("Provision was called for a rejected password")
		}
	})

	t.Run("a blank household name is refused before anything is written", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "founder@example.test", f.clock.Now().Add(usecase.SignupTTL))
		if _, err := f.svc.Complete(context.Background(), token, "   ", "Ade", "SGD", "a-long-enough-password"); !errors.Is(err, usecase.ErrHouseholdNameRequired) {
			t.Fatalf("error = %v, want ErrHouseholdNameRequired", err)
		}
		if f.signups.provisionCalls() != 0 {
			t.Fatal("Provision was called for a rejected household name")
		}
	})

	t.Run("an unknown currency is refused before anything is written", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "founder@example.test", f.clock.Now().Add(usecase.SignupTTL))
		if _, err := f.svc.Complete(context.Background(), token, "Ade & Kris", "Ade", "ZZZ", "a-long-enough-password"); !errors.Is(err, domain.ErrInvalidMoney) {
			t.Fatalf("error = %v, want domain.ErrInvalidMoney", err)
		}
		if f.signups.provisionCalls() != 0 {
			t.Fatal("Provision was called for a rejected currency")
		}
	})

	t.Run("a consumed token is refused with ErrSignupAlreadyUsed", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "done@example.test", f.clock.Now().Add(usecase.SignupTTL))
		f.signups.markConsumed(f.tokens.HashToken(token), f.clock.Now())
		if _, err := f.svc.Complete(context.Background(), token, "Ade & Kris", "Ade", "SGD", "a-long-enough-password"); !errors.Is(err, usecase.ErrSignupAlreadyUsed) {
			t.Fatalf("error = %v, want ErrSignupAlreadyUsed", err)
		}
	})

	t.Run("Provision's own refusal is returned as-is", func(t *testing.T) {
		// Provision's guarded UPDATE is authoritative for the race between the
		// TokenLifecycle read above and the write. Its answer must not be
		// re-derived from the stale read.
		f := newSignupFixture(t)
		token := f.issueSignup(t, "racer@example.test", f.clock.Now().Add(usecase.SignupTTL))
		f.signups.failNextProvision(domain.ErrTokenExpired)
		if _, err := f.svc.Complete(context.Background(), token, "Ade & Kris", "Ade", "SGD", "a-long-enough-password"); !errors.Is(err, domain.ErrTokenExpired) {
			t.Fatalf("error = %v, want domain.ErrTokenExpired", err)
		}
		if f.sessions.count() != 0 {
			t.Fatal("a session was issued for a failed provision")
		}
	})
}
```

Write the `signupFixture` helper in the same file. It builds a `SignupService`
over the doubles, exposes them as fields (`users`, `signups`, `sessions`,
`mailer`, `tokens`, `clock`, `log`, `sessionTTL`), and provides `issueSignup`,
which calls `Request` for the address and returns the raw token the token double
handed out — so a test never has to know how the token was generated. Follow
`auth_test.go`'s existing fixture style; check it first:

Run: `cd api && grep -n "func newAuthFixture\|type authFixture" internal/usecase/auth_test.go`

`mailer.waitForSends` and `assertNoSendsWithin` are needed because the sends are
asynchronous (Step 4). Implement them with a buffered channel the mailer double
signals on, not with a bare `time.Sleep` — the global constraint is that tests
never sleep, and `assertNoSendsWithin` is the one place a bounded wait is
unavoidable, so it uses a `select` with `time.After` and asserts nothing arrived.

- [ ] **Step 3: Run it to verify it fails**

Run: `cd api && go test ./internal/usecase/ -run TestSignup -v -count=1`
Expected: FAIL — `undefined: usecase.NewSignupService`.

- [ ] **Step 4: Write the service**

Create `api/internal/usecase/signup.go`:

```go
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

const (
	// SignupTTL is how long a create-household token lives. Exported because
	// the frontend's copy states it ("expires in 24 hours") and the mail
	// template repeats it, so the value has one source.
	//
	// 24 hours, not magicLinkTTL's 15 minutes and not inviteTTL's 7 days: a
	// person who asks to create a household may well finish the job that
	// evening, but an unverified address should not hold a provisioning token
	// for a week.
	SignupTTL = 24 * time.Hour

	// signupPerHourLimit mirrors magicLinkPerHourLimit. Being over it is
	// silent, like every other branch.
	signupPerHourLimit = 3

	// signupGlobalDailyLimit is the backstop for the case both the per-address
	// and per-IP limits are being worked at once. It is counted from the
	// signups table rather than an in-memory counter, so restarting the API
	// cannot reset it.
	//
	// Sign-up is open to anyone (a deliberate product decision), which makes
	// this the last thing standing between the SMTP relay and a stranger with a
	// loop. Raising it is a decision about how much mail the relay may send on
	// a stranger's behalf, not a tuning knob.
	signupGlobalDailyLimit = 200

	// signupSendTimeout bounds the background send so a wedged relay cannot
	// leak goroutines forever. Generous because nothing waits on it.
	signupSendTimeout = 30 * time.Second
)

// ErrSignupAlreadyUsed is Preview's and Complete's answer for a token that has
// already provisioned a household. It is deliberately NOT domain.ErrAlreadyExists:
// that sentinel's copy is "That already exists.", which tells the holder of a
// spent link nothing useful, and its own doc comment scopes it to a
// unique-constraint race between concurrent writers.
var ErrSignupAlreadyUsed = errors.New("this sign-up link has already been used")

type SignupDeps struct {
	Signups    SignupRepository
	Users      UserRepository
	Sessions   SessionRepository
	Mailer     Mailer
	Hasher     PasswordHasher
	Tokens     TokenGenerator
	Clock      Clock
	SessionTTL time.Duration
	BaseURL    string
}

type SignupService struct {
	d SignupDeps
}

func NewSignupService(d SignupDeps) *SignupService {
	return &SignupService{d: d}
}

// SignupPreview is what the create-household screen needs before anything is
// created: the address the token proved, so the form can show it read-only.
// Nothing else -- there is no household yet to describe.
type SignupPreview struct {
	Email string
}

// Request is deliberately quiet. It returns nil for a fresh address, an address
// that already has an account, an address over its hourly limit, a day over the
// global mail ceiling, and every internal failure below the branch point. Any
// observable difference between those would let a caller discover which
// addresses are registered.
//
// Three properties make that true, and all three are load-bearing:
//
//  1. All three reads below run unconditionally, in this fixed order, on every
//     call. RequestMagicLink once returned as soon as its rate-limit check
//     decided the outcome, which made the *number of repository reads*
//     distinguish the rate-limited case just as surely as an error would have.
//     A read that is skipped on one branch is the defect; the ordered read log
//     in signup_test.go is what defends against it.
//
//  2. Mail is sent off the request path (see sendAsync), so a slow or wedged
//     relay cannot make the fresh-address branch measurably slower than the
//     others.
//
//  3. Everything after the branch point -- token generation, the INSERT, the
//     send -- is reachable only by a fresh, under-limit address, so a
//     propagated error from any of them would be a discrete yes/no oracle for
//     "is this address registered", cheaper to exploit than any timing
//     measurement. Each is logged at error level with a hashed address and
//     returns nil instead. ANYONE ADDING A STEP BELOW THE BRANCH POINT OWES IT
//     THE SAME TREATMENT.
func (s *SignupService) Request(ctx context.Context, email string) error {
	now := s.d.Clock.Now()

	globalCount, err := s.d.Signups.CountSince(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	addressCount, err := s.d.Signups.CountForEmailSince(ctx, email, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	_, err = s.d.Users.ByEmail(ctx, email)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	alreadyRegistered := err == nil

	// Both limits gate both branches. If only the fresh-address branch were
	// gated, someone could flood a registered address's inbox with
	// existing-account notices, and the differing behaviour would itself
	// distinguish the two cases.
	if addressCount >= signupPerHourLimit || globalCount >= signupGlobalDailyLimit {
		slog.Info("sign-up request declined by a rate limit",
			"email_hash", hashPrefix(s.d.Tokens.HashToken(email), 12),
			"address_count", addressCount,
			"global_count", globalCount,
		)
		return nil
	}

	if alreadyRegistered {
		// No token, no signup row: there is nothing for this person to
		// provision. The mail still goes, because its absence would be the
		// oracle.
		s.sendAsync(func(ctx context.Context) error {
			return s.d.Mailer.SendSignupForExistingAccount(ctx, email, s.d.BaseURL+"/sign-in")
		}, email, "existing account notice")
		return nil
	}

	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		slog.Error("sign-up token generation failed",
			"error", err, "email_hash", hashPrefix(s.d.Tokens.HashToken(email), 12))
		return nil
	}
	if err := s.d.Signups.Create(ctx, email, hash, now.Add(SignupTTL)); err != nil {
		slog.Error("sign-up persistence failed",
			"error", err, "email_hash", hashPrefix(s.d.Tokens.HashToken(email), 12))
		return nil
	}

	url := fmt.Sprintf("%s/sign-up/%s", s.d.BaseURL, raw)
	s.sendAsync(func(ctx context.Context) error {
		return s.d.Mailer.SendSignupLink(ctx, email, url)
	}, email, "sign-up link")
	return nil
}

// sendAsync fires a send off the request path and returns immediately, for the
// same two reasons sendMagicLinkAsync does: timing parity between the branches,
// and the fact that Request's contract is "always nil, always silent", so a
// relay that is down must not become a caller-visible error on one branch only.
//
// The context is derived from context.Background(), not the request's, because
// the request context is cancelled the moment the handler returns -- which
// happens before this goroutine would otherwise run.
func (s *SignupService) sendAsync(send func(context.Context) error, email, what string) {
	// Computed on the caller's goroutine, which the HTTP recoverer still
	// covers, so the recover below can reuse it without hashing again inside a
	// panic handler.
	emailHash := hashPrefix(s.d.Tokens.HashToken(email), 12)

	go func() {
		// Nothing supervises this goroutine: the HTTP recoverer guards only the
		// request goroutine. An unrecovered panic here would take down every
		// unrelated in-flight request, not just this send.
		defer func() {
			if r := recover(); r != nil {
				slog.Error("sign-up mail panicked", "panic", r, "kind", what, "email_hash", emailHash)
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), signupSendTimeout)
		defer cancel()
		if err := send(ctx); err != nil {
			slog.Error("sign-up mail failed to send", "error", err, "kind", what, "email_hash", emailHash)
		}
	}()
}

// Preview lets the create-household screen show which address it is about to
// create an account for. It shares its liveness check with Complete through
// checkSignupLive.
func (s *SignupService) Preview(ctx context.Context, token string) (SignupPreview, error) {
	details, err := s.d.Signups.ByTokenHash(ctx, s.d.Tokens.HashToken(token))
	if err != nil {
		return SignupPreview{}, err
	}
	if err := checkSignupLive(details, s.d.Clock.Now()); err != nil {
		return SignupPreview{}, err
	}
	return SignupPreview{Email: details.Email}, nil
}

// checkSignupLive reports why a sign-up token can no longer be used, keeping
// consumed and expired apart because the next action differs: a consumed token
// means the household exists and the answer is to sign in, an expired one means
// start again. The ordering rule lives in domain.TokenLifecycle, shared with
// invites.
func checkSignupLive(details SignupDetails, now time.Time) error {
	switch domain.TokenLifecycle(now, details.ExpiresAt, details.ConsumedAt) {
	case domain.TokenLive:
		return nil
	case domain.TokenConsumed:
		return ErrSignupAlreadyUsed
	case domain.TokenExpired:
		return domain.ErrTokenExpired
	default:
		// An unrecognised state refuses rather than treating the token as
		// usable. Adding a TokenState without a case here rejects the sign-up;
		// it does not silently accept it.
		return domain.ErrTokenExpired
	}
}

// Complete turns a verified address into a household and signs its owner in.
//
// Every validation happens before the hash and before Provision, so a rejected
// form never consumes the token -- someone who mistypes their password can
// simply resubmit. The session is minted by the same package-level issueSession
// that SignIn and InviteService.Accept use, so a session from sign-up is
// indistinguishable from theirs, down to how it is created.
func (s *SignupService) Complete(ctx context.Context, token, householdName, displayName,
	currency, password string) (SignInResult, error) {
	now := s.d.Clock.Now()

	if err := validatePassword(password); err != nil {
		return SignInResult{}, err
	}
	blueprint, err := NewSignupBlueprint(householdName, displayName, currency)
	if err != nil {
		return SignInResult{}, err
	}

	details, err := s.d.Signups.ByTokenHash(ctx, s.d.Tokens.HashToken(token))
	if err != nil {
		return SignInResult{}, err
	}
	if err := checkSignupLive(details, now); err != nil {
		return SignInResult{}, err
	}

	passwordHash, err := s.d.Hasher.Hash(password)
	if err != nil {
		return SignInResult{}, fmt.Errorf("hash sign-up password: %w", err)
	}

	// Provision's guarded UPDATE is the concurrency gate: its answer is
	// authoritative for the race between the read above and this write, so its
	// error is returned as-is rather than re-derived from the stale read.
	provisioned, err := s.d.Signups.Provision(ctx, details.ID, passwordHash, blueprint)
	if err != nil {
		return SignInResult{}, err
	}

	return issueSession(ctx, s.d.Sessions, s.d.Tokens, s.d.SessionTTL,
		provisioned.UserID, provisioned.HouseholdID, now)
}
```

- [ ] **Step 5: Run the service tests**

Run: `cd api && go test ./internal/usecase/ -run TestSignup -v -count=1`
Expected: PASS, every subtest.

- [ ] **Step 6: Mutation-check the read ordering — the important one**

Move the `Users.ByEmail` block above the two count reads, so the sequence
changes but every branch still behaves correctly otherwise.

Run: `cd api && go test ./internal/usecase/ -run TestSignupRequestIsIndistinguishable -count=1`
Expected: FAIL on every branch, reporting the wrong sequence.

Now the sharper mutation: restore the order, then make the address read
conditional — wrap the `Users.ByEmail` call in `if addressCount < signupPerHourLimit {`.
This is the exact defect `RequestMagicLink` shipped with.

Run: `cd api && go test ./internal/usecase/ -run TestSignupRequestIsIndistinguishable -count=1`
Expected: FAIL on `address_at_its_hourly_limit` only — one branch performs two
reads where the others perform three.

Restore and re-run. Expected: PASS. If either mutation passed, the test is not
protecting the property and must be fixed before continuing.

- [ ] **Step 7: Mutation-check the both-branches-mail property**

Delete the `sendAsync` call in the `alreadyRegistered` branch.

Run: `cd api && go test ./internal/usecase/ -run TestSignupRequestMailsBothBranches -count=1`
Expected: FAIL on `registered_address_gets_an_existing-account_notice_with_no_token`.

Restore and re-run. Expected: PASS.

- [ ] **Step 8: Full gate**

Run: `cd api && go test ./... -count=1 && cd .. && make lint-arch`
Expected: PASS. Run the usecase package with the race detector too, since
`sendAsync` starts goroutines the tests observe:

Run: `cd api && go test ./internal/usecase/ -race -count=1`
Expected: PASS with no race reported.

- [ ] **Step 9: Commit**

```bash
git add api/internal/usecase/signup.go api/internal/usecase/signup_test.go \
        api/internal/usecase/testdouble_test.go
git commit -m "feat(usecase): add SignupService, silent on every branch

Request returns nil for a fresh address, a registered one, one over its hourly
limit, a day over the global mail ceiling, and every internal failure below the
branch point. Any observable difference between those would let a caller
discover which addresses are registered.

Three reads run unconditionally in a fixed order on every call, and the test
asserts the sequence rather than the return values -- a read that is *skipped*
on one branch is the defect RequestMagicLink shipped with, and no assertion
about results can catch it.

Both branches send mail. If only the fresh one did, the absence of an email
would be the oracle the identical return value exists to prevent."
```

---

### Task 28: The HTTP surface — three sign-up routes, currencies, and the per-IP limiter

**Files:**
- Create: `api/internal/adapter/http/signup_handlers.go`, `api/internal/adapter/http/currency_handlers.go`, `api/internal/adapter/http/middleware_ratelimit.go`, `api/internal/adapter/http/middleware_ratelimit_test.go`
- Modify: `api/internal/adapter/http/router.go` (register the routes), `api/internal/adapter/http/errors.go` (three new cases), `api/internal/adapter/http/api_test.go` (`noopMailer`, the route matrices, new tests)
- Modify: `api/cmd/api/main.go` (wire `SignupService` and `SignupRepo` into `Deps`)

**Interfaces:**
- Consumes: `usecase.SignupService`, `usecase.ErrSignupAlreadyUsed`, `usecase.ErrHouseholdNameRequired`, `usecase.ErrDisplayNameRequired`, `domain.ActiveCurrencies`, `domain.CurrencyMinorUnits`, `completeSignIn` and `decodeJSONBody` (existing).
- Produces: `Deps.Signups *usecase.SignupService`, and these routes:

| Route | Answer |
|---|---|
| `POST /api/v1/auth/sign-up` | `202 {"status":"accepted"}` always |
| `GET /api/v1/auth/sign-up/{token}` | `200 {"email":"…"}`; `410 TOKEN_EXPIRED`; `409 SIGNUP_ALREADY_USED`; `404 NOT_FOUND` |
| `POST /api/v1/auth/sign-up/{token}/complete` | `completeSignIn`'s me bundle plus both cookies |
| `GET /api/v1/currencies` | `200 {"currencies":[…]}` |

- [ ] **Step 1: Add the three error cases**

In `api/internal/adapter/http/errors.go`, insert these before the
`domain.ErrAlreadyExists` case (order matters — `errors.Is` walks in case order):

```go
	case errors.Is(err, usecase.ErrSignupAlreadyUsed):
		// Deliberately not folded into ALREADY_EXISTS, whose copy ("That
		// already exists.") tells the holder of a spent sign-up link nothing
		// useful, and whose own comment scopes it to a write race.
		WriteError(w, http.StatusConflict, "SIGNUP_ALREADY_USED",
			"This link has already been used. Try signing in instead.", nil)
	case errors.Is(err, usecase.ErrHouseholdNameRequired):
		WriteError(w, http.StatusUnprocessableEntity, "HOUSEHOLD_NAME_REQUIRED",
			"A household name is required.", nil)
	case errors.Is(err, usecase.ErrDisplayNameRequired):
		WriteError(w, http.StatusUnprocessableEntity, "DISPLAY_NAME_REQUIRED",
			"Your name is required.", nil)
```

- [ ] **Step 2: Write the failing handler tests**

In `api/internal/adapter/http/api_test.go`, first add the two methods
`noopMailer` now needs (it stops satisfying `usecase.Mailer` otherwise):

```go
func (noopMailer) SendSignupLink(context.Context, string, string) error               { return nil }
func (noopMailer) SendSignupForExistingAccount(context.Context, string, string) error { return nil }
```

Then append:

```go
// The HTTP half of the indistinguishability property. The service-level test
// (usecase/signup_test.go) pins the read sequence; this pins what a caller can
// actually see.
func TestSignUpAnswersIdenticallyForEveryAddress(t *testing.T) {
	env := newTestEnv(t)

	// The seeded household's owner address exists; the other two do not.
	for _, email := range []string{usecase.AndreasEmail, "stranger@example.test", "another@example.test"} {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up", map[string]string{"email": email})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("POST sign-up for %q = %d, want 202", email, rec.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body for %q is not JSON: %v", email, err)
		}
		if body["status"] != "accepted" {
			t.Fatalf("body for %q = %v, want {\"status\":\"accepted\"}", email, body)
		}
	}
}

// Repeating the same request past the hourly limit must not change the answer.
func TestSignUpStaysSilentPastTheRateLimit(t *testing.T) {
	env := newTestEnv(t)
	for i := 0; i < 6; i++ {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up",
			map[string]string{"email": "persistent@example.test"})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("attempt %d = %d, want 202 -- a 429 here is an oracle in its own right", i, rec.Code)
		}
	}
}

func TestSignUpPreviewAndComplete(t *testing.T) {
	env := newTestEnv(t)

	rec := env.do(http.MethodPost, "/api/v1/auth/sign-up", map[string]string{"email": "founder@example.test"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("request = %d, want 202", rec.Code)
	}
	token := env.lastSignupToken(t)

	t.Run("preview returns the address", func(t *testing.T) {
		rec := env.do(http.MethodGet, "/api/v1/auth/sign-up/"+token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("preview = %d, want 200", rec.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body: %v", err)
		}
		if body["email"] != "founder@example.test" {
			t.Fatalf("email = %q, want founder@example.test", body["email"])
		}
	})

	t.Run("an unknown token is 404", func(t *testing.T) {
		rec := env.do(http.MethodGet, "/api/v1/auth/sign-up/never-issued", nil)
		assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
	})

	t.Run("a blank household name is 422", func(t *testing.T) {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up/"+token+"/complete", map[string]string{
			"householdName": "  ", "displayName": "Ade", "primaryCurrency": "SGD",
			"password": "a-long-enough-password",
		})
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "HOUSEHOLD_NAME_REQUIRED")
	})

	t.Run("an unknown currency is 422", func(t *testing.T) {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up/"+token+"/complete", map[string]string{
			"householdName": "Ade & Kris", "displayName": "Ade", "primaryCurrency": "ZZZ",
			"password": "a-long-enough-password",
		})
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "INVALID_CURRENCY")
	})

	t.Run("a short password is 422", func(t *testing.T) {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up/"+token+"/complete", map[string]string{
			"householdName": "Ade & Kris", "displayName": "Ade", "primaryCurrency": "SGD",
			"password": "short",
		})
		assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "PASSWORD_TOO_SHORT")
	})

	// Every rejection above left the token usable, which is why this still works.
	t.Run("completing signs the new owner in", func(t *testing.T) {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up/"+token+"/complete", map[string]string{
			"householdName": "Ade & Kris", "displayName": "Ade", "primaryCurrency": "SGD",
			"password": "a-long-enough-password",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("complete = %d, want 200: %s", rec.Code, rec.Body.String())
		}

		var body meResponseBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body is not a me bundle: %v", err)
		}
		if body.Household.Name != "Ade & Kris" {
			t.Fatalf("household name = %q", body.Household.Name)
		}
		if body.Household.PrimaryCurrency != "SGD" || body.Household.SecondaryCurrency != "SGD" ||
			body.Household.ShowSecondaryCurrency {
			t.Fatalf("currency fields = %+v, want SGD/SGD/false", body.Household)
		}
		if body.Membership.Role != "owner" {
			t.Fatalf("role = %q, want owner", body.Membership.Role)
		}
		if len(body.Spaces) != 3 {
			t.Fatalf("got %d spaces, want the three builtins", len(body.Spaces))
		}

		var session, csrf bool
		for _, c := range rec.Result().Cookies() {
			switch c.Name {
			case "hearth_session":
				session = true
			case "csrf_token":
				csrf = true
			}
		}
		if !session || !csrf {
			t.Fatalf("cookies: session=%v csrf=%v, want both", session, csrf)
		}
	})

	t.Run("the token cannot be used twice", func(t *testing.T) {
		rec := env.do(http.MethodPost, "/api/v1/auth/sign-up/"+token+"/complete", map[string]string{
			"householdName": "Second household", "displayName": "Ade", "primaryCurrency": "SGD",
			"password": "a-long-enough-password",
		})
		assertErrorResponse(t, rec, http.StatusConflict, "SIGNUP_ALREADY_USED")
	})

	t.Run("preview of a consumed token is 409, not 410", func(t *testing.T) {
		rec := env.do(http.MethodGet, "/api/v1/auth/sign-up/"+token, nil)
		assertErrorResponse(t, rec, http.StatusConflict, "SIGNUP_ALREADY_USED")
	})
}

func TestCurrenciesIsPublicAndOnlyOffersTwoMinorUnitCodes(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(http.MethodGet, "/api/v1/currencies", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("= %d, want 200 with no session", rec.Code)
	}
	var body struct {
		Currencies []struct {
			Code, Symbol, Name string
		} `json:"currencies"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if len(body.Currencies) < 100 {
		t.Fatalf("got %d currencies, want the two-minor-unit majority of ISO 4217", len(body.Currencies))
	}
	byCode := map[string]bool{}
	for _, c := range body.Currencies {
		byCode[c.Code] = true
	}
	if !byCode["SGD"] || !byCode["USD"] || !byCode["BRL"] {
		t.Fatal("want SGD, USD and BRL offered")
	}
	// Money.String() hard-codes two decimal places, so a household that picked
	// one of these would have every amount rendered wrong.
	for _, code := range []string{"JPY", "KRW", "KWD", "BHD", "ISK"} {
		if byCode[code] {
			t.Fatalf("%s is offered, but Money.String() renders it wrong", code)
		}
	}
}
```

`env.lastSignupToken` is a new helper. The test env's mailer must capture the
URL the sign-up mail carried so the test can extract the raw token, exactly as
the existing magic-link tests recover theirs. Check how they do it:

Run: `cd api && grep -n "magic\|Mailer\|capture" internal/adapter/http/api_test.go | grep -i "token\|url" | head`

Follow that pattern: replace `noopMailer` with a recording mailer in
`newTestEnv`, or add a recording field, and implement `lastSignupToken` as
`strings.TrimPrefix(lastURL, baseURL+"/sign-up/")`.

- [ ] **Step 3: Extend the route matrices**

In `TestEveryProtectedRouteRejectsAnUnauthenticatedCaller`, the new routes must
**not** be added to the protected list — they are public. Instead add them to
whichever list that test uses to record deliberately-public routes (read lines
465-530 to see how `/auth/sign-in` and `/invites/{token}` are handled there) so
a future reader can see the four new routes were classified, not forgotten.

Then add:

```go
// Sign-up is pre-auth and pre-session, so it must not require CSRF -- there is
// no csrf_token cookie to double-submit yet.
func TestSignUpRoutesDoNotRequireCSRF(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(http.MethodPost, "/api/v1/auth/sign-up", map[string]string{"email": "nocsrf@example.test"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("= %d, want 202 with no CSRF token", rec.Code)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `cd api && go test ./internal/adapter/http/ -run 'TestSignUp|TestCurrencies' -v -count=1`
Expected: FAIL — the routes 404, and the package does not compile until
`Deps.Signups` exists.

- [ ] **Step 5: Write the per-IP limiter**

Create `api/internal/adapter/http/middleware_ratelimit.go`:

```go
package httpadapter

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// ipRateLimiter is a fixed-window counter keyed by client IP, used to bound the
// unauthenticated sign-up endpoint.
//
// Sign-up is open to anyone -- a deliberate product decision -- and the
// per-address limit in SignupService is trivially bypassed by varying the
// address. That makes this the thing standing between the SMTP relay and a
// stranger with a loop, not a hardening nicety.
//
// IT IS IN-MEMORY AND THEREFORE PER-PROCESS. A second API replica doubles the
// effective limit, and a restart clears it. Anyone adding a replica must
// replace this with a shared counter -- a signup_attempts table indexed by
// (ip, at) is the obvious move, and SignupService's global daily ceiling
// already reads from the database for exactly this reason. It is in-memory here
// because there is one API container today and a table per request rejected is
// a worse trade at that scale.
type ipRateLimiter struct {
	mu      sync.Mutex
	counts  map[string]int
	window  time.Duration
	limit   int
	resetAt time.Time
	now     func() time.Time
}

func newIPRateLimiter(limit int, window time.Duration, now func() time.Time) *ipRateLimiter {
	return &ipRateLimiter{
		counts:  map[string]int{},
		window:  window,
		limit:   limit,
		resetAt: now().Add(window),
		now:     now,
	}
}

// allow reports whether this IP may proceed, and counts the request.
func (l *ipRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if now := l.now(); !now.Before(l.resetAt) {
		// Whole-map reset rather than per-key expiry: it bounds memory without
		// a sweeper goroutine, and the imprecision at a window boundary does
		// not matter for a limit whose job is to stop a loop.
		l.counts = map[string]int{}
		l.resetAt = now.Add(l.window)
	}

	if l.counts[ip] >= l.limit {
		return false
	}
	l.counts[ip]++
	return true
}

// clientIP prefers the address chi's middleware.RealIP has already resolved
// (it rewrites r.RemoteAddr from X-Forwarded-For, and the router installs it),
// falling back to the raw RemoteAddr. The port is stripped so repeat requests
// from one client, which arrive on different ephemeral ports, count together --
// forgetting that makes the limiter count nothing at all.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimitByIP answers 429 when an IP is over its window. This is the one
// place in the sign-up flow that may answer something other than 202, and that
// is safe: the limit is keyed by IP, not by address, so what it reveals is "you
// have sent a lot of requests" -- something the caller already knows -- and
// never anything about whether any particular address is registered.
func rateLimitByIP(l *ipRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.allow(clientIP(r)) {
				WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED",
					"Too many requests. Try again later.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 6: Test the limiter**

Create `api/internal/adapter/http/middleware_ratelimit_test.go`:

```go
package httpadapter

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIPRateLimiterCountsPerIPAndResets(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	l := newIPRateLimiter(2, time.Minute, clock)

	if !l.allow("1.2.3.4") || !l.allow("1.2.3.4") {
		t.Fatal("the first two requests must be allowed")
	}
	if l.allow("1.2.3.4") {
		t.Fatal("the third request must be refused")
	}
	if !l.allow("5.6.7.8") {
		t.Fatal("a different IP has its own budget")
	}

	now = now.Add(time.Minute)
	if !l.allow("1.2.3.4") {
		t.Fatal("the window must reset")
	}
}

// Counting ports separately would make the limiter count nothing: a client's
// repeat requests arrive on different ephemeral ports.
func TestClientIPStripsThePort(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "203.0.113.7:54321"
	if got := clientIP(r); got != "203.0.113.7" {
		t.Fatalf("clientIP = %q, want 203.0.113.7", got)
	}
}

func TestRateLimitByIPAnswersTheStandardEnvelope(t *testing.T) {
	now := time.Now()
	l := newIPRateLimiter(1, time.Minute, func() time.Time { return now })
	h := rateLimitByIP(l)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i, wantCode := range []int{http.StatusOK, http.StatusTooManyRequests} {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "198.51.100.1:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != wantCode {
			t.Fatalf("request %d = %d, want %d", i, rec.Code, wantCode)
		}
	}
}
```

Run: `cd api && go test ./internal/adapter/http/ -run 'TestIPRateLimiter|TestClientIP|TestRateLimitByIP' -v -count=1`
Expected: PASS after Step 5.

- [ ] **Step 7: Write the handlers**

Create `api/internal/adapter/http/signup_handlers.go`:

```go
package httpadapter

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type signUpRequest struct {
	Email string `json:"email"`
}

// handleSignUp always answers 202 with the same body. SignupService.Request's
// contract is "always nil" (see its doc comment); this still checks err so a
// future change to that contract fails loudly here instead of being swallowed.
func handleSignUp(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req signUpRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if err := deps.Signups.Request(r.Context(), req.Email); err != nil {
			MapDomainError(w, r, err)
			return
		}
		// Byte-identical to handleRequestMagicLink's answer, deliberately.
		WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	}
}

// handleSignUpPreview reads no body -- the token is in the path -- so it does
// not call decodeJSONBody, exactly as handleInvitePreview does not.
func handleSignUpPreview(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		preview, err := deps.Signups.Preview(r.Context(), chi.URLParam(r, "token"))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"email": preview.Email})
	}
}

type completeSignUpRequest struct {
	HouseholdName   string `json:"householdName"`
	DisplayName     string `json:"displayName"`
	PrimaryCurrency string `json:"primaryCurrency"`
	Password        string `json:"password"`
}

// handleCompleteSignUp provisions the household and signs the new owner in
// through completeSignIn -- the same tail sign-in, magic-link consumption and
// invite acceptance use, so all four answer with the identical me bundle and
// the identical pair of cookies.
func handleCompleteSignUp(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req completeSignUpRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		result, err := deps.Signups.Complete(r.Context(), chi.URLParam(r, "token"),
			req.HouseholdName, req.DisplayName, req.PrimaryCurrency, req.Password)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		completeSignIn(w, r, deps, result)
	}
}
```

Create `api/internal/adapter/http/currency_handlers.go`:

```go
package httpadapter

import (
	"net/http"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

type currencyDTO struct {
	Code   string `json:"code"`
	Symbol string `json:"symbol,omitempty"`
	Name   string `json:"name"`
}

// currencySymbols carries the few symbols worth showing. It is deliberately
// partial: an unknown code renders as the bare code, which is what
// currencyLabel in the frontend already did. This lives here rather than in the
// frontend so there is one list, served rather than duplicated.
var currencySymbols = map[string]string{
	"AUD": "A$", "BRL": "R$", "CAD": "C$", "CHF": "CHF", "CNY": "¥",
	"EUR": "€", "GBP": "£", "HKD": "HK$", "IDR": "Rp", "INR": "₹",
	"MYR": "RM", "NZD": "NZ$", "PHP": "₱", "SGD": "S$", "THB": "฿",
	"USD": "$", "VND": "₫", "ZAR": "R",
}

// handleListCurrencies serves the currencies a household may choose. It is
// public: the sign-up form fetches it before any session exists.
//
// Only two-minor-unit currencies are offered. domain.Money.String() is
// fmt.Sprintf("%s%s %d.%02d", ...) -- two decimal places, hard-coded -- so a
// household that picked JPY (0) or KWD (3) would have every amount rendered
// wrong. domain.ParseCurrency still accepts every active code, because the
// household PATCH path has always accepted arbitrary codes and must not start
// rejecting existing data; this filter is only about what we *offer*.
//
// WHEN Money LEARNS ABOUT MINOR UNITS, DELETE THIS FILTER. It is the only thing
// keeping the list short.
func handleListCurrencies() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		all := domain.ActiveCurrencies()
		out := make([]currencyDTO, 0, len(all))
		for _, c := range all {
			if c.MinorUnits != 2 {
				continue
			}
			out = append(out, currencyDTO{Code: c.Code, Symbol: currencySymbols[c.Code], Name: c.Name})
		}
		WriteJSON(w, http.StatusOK, map[string]any{"currencies": out})
	}
}
```

- [ ] **Step 8: Register the routes**

In `api/internal/adapter/http/router.go`, add `Signups *usecase.SignupService`
to `Deps`, then inside the `/auth` group, beside the other public routes:

```go
			// Public: no session exists yet, and no CSRF cookie either.
			//
			// The per-IP limiter wraps only the request endpoint. The preview
			// and complete endpoints need a token that was mailed to a real
			// address, so they are not a path to unbounded mail; the request
			// endpoint is.
			auth.Group(func(su chi.Router) {
				su.Use(rateLimitByIP(newIPRateLimiter(signUpRequestsPerIPPerHour, time.Hour, deps.Clock.Now)))
				su.Post("/sign-up", handleSignUp(deps))
			})
			auth.Get("/sign-up/{token}", handleSignUpPreview(deps))
			auth.Post("/sign-up/{token}/complete", handleCompleteSignUp(deps))
```

and beside `/invites`:

```go
		// Public: the sign-up form reads this before any session exists, and
		// Settings reads it after one. One list, served rather than duplicated
		// in the frontend.
		api.Get("/currencies", handleListCurrencies())
```

Add the constant near the top of `router.go`:

```go
// signUpRequestsPerIPPerHour bounds one client's sign-up requests. The
// per-address limit in SignupService is bypassed by varying the address; this is
// what actually bounds outbound mail. 10 is generous for a household setting
// itself up (one request, maybe a couple of retries) and far below what a loop
// needs to be useful.
const signUpRequestsPerIPPerHour = 10
```

- [ ] **Step 9: Wire it in `main.go`**

In `api/cmd/api/main.go`, construct the repository and service and pass the
service into `Deps`:

```go
	signupRepo := postgres.NewSignupRepo(pool)
	signupSvc := usecase.NewSignupService(usecase.SignupDeps{
		Signups:    signupRepo,
		Users:      userRepo,
		Sessions:   sessionRepo,
		Mailer:     mailer,
		Hasher:     hasher,
		Tokens:     tokens,
		Clock:      clk,
		SessionTTL: httpadapter.SessionTTL,
		BaseURL:    cfg.AppBaseURL,
	})
```

Use whatever the existing local variable names are — read how `AuthService` is
built a few lines above and match it.

- [ ] **Step 10: Run everything**

Run: `cd api && go build ./... && go test ./internal/adapter/http/ -v -count=1 -run 'TestSignUp|TestCurrencies|TestIPRateLimiter|TestClientIP|TestRateLimitByIP'`
Expected: PASS.

Run: `cd api && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 11: Mutation-check the identical-answer property**

In `handleSignUp`, make the answer conditional on whether mail was sent — for
instance return `204` when `req.Email` contains `"stranger"`. Any difference
will do; the point is to confirm the test notices.

Run: `cd api && go test ./internal/adapter/http/ -run TestSignUpAnswersIdentically -count=1`
Expected: FAIL.

Restore and re-run. Expected: PASS.

- [ ] **Step 12: Commit**

```bash
git add api/internal/adapter/http/signup_handlers.go \
        api/internal/adapter/http/currency_handlers.go \
        api/internal/adapter/http/middleware_ratelimit.go \
        api/internal/adapter/http/middleware_ratelimit_test.go \
        api/internal/adapter/http/router.go api/internal/adapter/http/errors.go \
        api/internal/adapter/http/api_test.go api/cmd/api/main.go
git commit -m "feat(http): expose sign-up, the currency list, and a per-IP limit

POST /auth/sign-up answers 202 {\"status\":\"accepted\"} for every input --
byte-identical to the magic-link endpoint. A 429 from the per-address limit
would be an oracle in its own right, so that limit is silent; the per-IP
limiter may answer 429 because what it reveals is how many requests the caller
has sent, which they already know.

The limiter is in-memory and therefore per-process. That is recorded at the
line someone adding a replica would read, with the migration path named.

GET /currencies offers only two-minor-unit codes, because Money.String() hard-
codes two decimal places and a JPY household would see every amount rendered
wrong. ParseCurrency still accepts every active code -- the household PATCH
path has always taken arbitrary codes and must not start rejecting stored data."
```

---

### Task 29: `adminctl prune`, and `--email` instead of Andreas

**Files:**
- Modify: `api/cmd/adminctl/main.go`

**Interfaces:**
- Consumes: `SignupRepository.Prune`, `LoginAttemptRepository.Prune`.
- Produces: `adminctl prune [--older-than DAYS]`, and an `--email` flag on
  `unlock-household` and `create-invite`.

- [ ] **Step 1: Add `--email` to the two household-scoped subcommands**

`resolveSeededHouseholdAndOwner` (around line 335) resolves "the household"
through `usecase.AndreasEmail`, and its own comment says that works because
there is "exactly one household per deployment". Once a stranger signs up that
is false, and both subcommands would act on the wrong household.

Replace it with:

```go
// resolveHouseholdByEmail finds the household the given address belongs to,
// along with that user's id. It replaces a version that resolved "the
// household" through usecase.AndreasEmail, which was correct only while there
// was exactly one household per deployment -- self-serve sign-up ended that, and
// an operator unlocking the wrong customer's household is a worse failure than
// having to type an address.
//
// AndreasEmail remains the default in development so `make unlock-household`
// keeps working with no arguments against a seeded database.
func resolveHouseholdByEmail(ctx context.Context, users usecase.UserRepository,
	memberships usecase.MembershipRepository, email string) (householdID, userID string, err error) {
	user, err := users.ByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", "", fmt.Errorf("no account for %q; pass --email with a member's address", email)
		}
		return "", "", fmt.Errorf("look up %q: %w", email, err)
	}
	membership, err := memberships.ByUser(ctx, user.ID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", "", fmt.Errorf("%q has an account but belongs to no household", email)
		}
		return "", "", fmt.Errorf("resolve the household for %q: %w", email, err)
	}
	return membership.HouseholdID, user.ID, nil
}
```

Then in `runUnlockHousehold` and the `create-invite` handler, declare the flag
and pass it through, following how `create-invite` already declares its `name`
and `email` flags (read lines 300-325 first):

```go
	email := fs.String("email", usecase.AndreasEmail,
		"any member's address in the household to act on; defaults to the seeded owner")
```

Update `runUnlockHousehold`'s doc comment so it no longer claims the household
is resolved through Andreas.

- [ ] **Step 2: Add the `prune` subcommand**

Add `case "prune":` to the switch at line 102, and:

```go
// pruneFloor is the shortest retention window `prune` will accept. It is far
// outside domain.LockoutPolicy.Window (15 minutes), because deleting a
// login_attempts row still inside that window would clear a live lockout --
// which would turn a cleanup command into a way to unlock a household that is
// actively being guessed at. The floor is enforced rather than documented so
// nobody can reach that state with a plausible-looking flag value.
const pruneFloor = 7 * 24 * time.Hour

func runPrune(ctx context.Context, signups usecase.SignupRepository,
	attempts usecase.LoginAttemptRepository, olderThan time.Duration) error {
	if olderThan < pruneFloor {
		return fmt.Errorf("--older-than must be at least %d days: pruning login attempts inside the "+
			"lockout window would clear a live lockout", int(pruneFloor.Hours()/24))
	}
	before := time.Now().Add(-olderThan)

	prunedSignups, err := signups.Prune(ctx, before)
	if err != nil {
		return fmt.Errorf("prune signups: %w", err)
	}
	// login_attempts is pruned here rather than in its own command because
	// ClearFailures -- the only thing that ever deleted from it -- is scoped
	// WHERE household_id = $1, which never matches the NULL rows an
	// unknown-address sign-in attempt records. Those rows were deleted by
	// nothing at all, and a public sign-in endpoint means a stranger can create
	// them without limit.
	prunedAttempts, err := attempts.Prune(ctx, before)
	if err != nil {
		return fmt.Errorf("prune login attempts: %w", err)
	}

	fmt.Printf("Pruned %d signups and %d login attempts older than %s.\n",
		prunedSignups, prunedAttempts, before.Format(time.RFC3339))
	return nil
}
```

Declare the flag with a 30-day default and wire the case:

```go
	case "prune":
		fs := flag.NewFlagSet("prune", flag.ExitOnError)
		days := fs.Int("older-than", 30, "delete consumed/expired rows older than this many days")
		if err := fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		return runPrune(ctx, postgres.NewSignupRepo(db), loginAttempts,
			time.Duration(*days)*24*time.Hour)
```

Match the surrounding cases' exact flag-parsing style — read one of them first.

- [ ] **Step 3: Update the usage text**

Find where the subcommands are listed for an unknown command and add `prune`,
with the `--older-than` default and the `--email` flags on the two changed
commands.

- [ ] **Step 4: Verify by hand against a real database**

```bash
make up && make seed
cd api && go run ./cmd/adminctl prune --older-than 3
```
Expected: refused, naming the 7-day floor.

```bash
cd api && go run ./cmd/adminctl prune
```
Expected: `Pruned 0 signups and 0 login attempts older than …` on a fresh seed.

```bash
cd api && go run ./cmd/adminctl unlock-household --email andreas@hearth.family
```
Expected: `Household unlocked.`

```bash
cd api && go run ./cmd/adminctl unlock-household --email nobody@example.test
```
Expected: `no account for "nobody@example.test"; pass --email with a member's address`

- [ ] **Step 5: Build and test**

Run: `cd api && go build ./... && go vet ./... && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add api/cmd/adminctl/main.go
git commit -m "feat(adminctl): add prune, and resolve the household by --email

unlock-household and create-invite resolved 'the household' through
usecase.AndreasEmail, which was correct only while there was exactly one
household per deployment. Self-serve sign-up ends that, and an operator
unlocking the wrong customer's household is a worse failure than having to type
an address. The Andreas default survives so make unlock-household still works
against a seeded development database.

prune covers signups and login_attempts. It refuses a window under seven days:
deleting a login_attempts row inside the 15-minute lockout window would clear a
live lockout, turning a cleanup command into a way to unlock a household
somebody is actively guessing at."
```

---

### Task 30: One public-route list, the sign-up hooks, and step 1 of the card

**Files:**
- Create: `web/src/routes/publicRoutes.ts`, `web/src/routes/publicRoutes.test.ts`
- Create: `web/src/features/auth/SignUpScreen.tsx`, `web/src/features/auth/SignUpScreen.test.tsx`
- Create: `web/src/features/auth/CheckYourEmailPanel.tsx`
- Modify: `web/src/api/client.ts:71-75` and `web/src/api/unauthorizedRedirect.ts:42` (import the shared list)
- Modify: `web/src/features/auth/useAuth.ts` (add three hooks), `web/src/features/auth/schemas.ts` (two schemas)
- Modify: `web/src/features/auth/MagicLinkSentPanel.tsx` (become a caller of `CheckYourEmailPanel`)
- Modify: `web/src/features/auth/SignInScreen.tsx` (the design's footer link)
- Modify: `web/src/routes/router.tsx` (two routes)

**Interfaces:**
- Consumes: `apiFetch`, `meQueryKey`, `meSchema` (existing).
- Produces:
  - `PUBLIC_ROUTE_PREFIXES: readonly string[]` and `PRE_AUTH_API_PREFIXES: readonly string[]`
  - `useRequestSignUp()`, `useSignUpPreview(token)`, `useCompleteSignUp()`
  - `<CheckYourEmailPanel heading body pending error resendLabel onResend onBack backLabel />`
  - `<SignUpScreen />`, and the routes `/sign-up` and `/sign-up/$token`.

- [ ] **Step 1: Write the failing public-route test**

Create `web/src/routes/publicRoutes.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { routeTree } from "./router";
import { PRE_AUTH_API_PREFIXES, PUBLIC_ROUTE_PREFIXES } from "./publicRoutes";

// Walks the real route tree and collects every path that is NOT under the
// pathless `authenticated` layout route. Those are exactly the routes reachable
// with no session, which is the set PUBLIC_ROUTE_PREFIXES has to cover.
//
// HANDOVER.md flagged the two hand-maintained lists as a bug already fixed once:
// a future public route whose component calls useMe() reintroduces it. This test
// is the thing that ties the lists to the tree.
function publicRoutePaths(): string[] {
  const children = (routeTree.children ?? []) as Array<{
    id?: string;
    path?: string;
  }>;
  return children
    .filter((route) => route.id !== "authenticated" && typeof route.path === "string")
    .map((route) => route.path as string);
}

describe("public routes", () => {
  it("covers every route reachable without a session", () => {
    const uncovered = publicRoutePaths().filter(
      (path) => !PUBLIC_ROUTE_PREFIXES.some((prefix) => path.startsWith(prefix)),
    );
    expect(uncovered).toEqual([]);
  });

  it("lists the sign-up routes", () => {
    const paths = publicRoutePaths();
    expect(paths).toContain("/sign-up");
    expect(paths).toContain("/sign-up/$token");
  });

  it("exempts the sign-up and currency endpoints from the 401 handler", () => {
    expect(PRE_AUTH_API_PREFIXES).toContain("/api/v1/auth/sign-up");
    expect(PRE_AUTH_API_PREFIXES).toContain("/api/v1/currencies");
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/routes/publicRoutes.test.ts`
Expected: FAIL — cannot resolve `./publicRoutes`.

- [ ] **Step 3: Write the shared list and point both files at it**

Create `web/src/routes/publicRoutes.ts`:

```ts
// The one list of things reachable before a session exists.
//
// This used to be two hand-maintained lists with nothing tying either to the
// route tree: preAuthPathPrefixes in api/client.ts and publicRoutePrefixes in
// api/unauthorizedRedirect.ts. HANDOVER.md records that a public route whose
// component calls useMe() reintroduces a bug already fixed once, and sign-up
// added two such routes. publicRoutes.test.ts walks the real tree and fails if
// a route escapes this list.
//
// The two exports are genuinely different things and must not be merged:
// PUBLIC_ROUTE_PREFIXES is about *which screen is mounted*, and
// PRE_AUTH_API_PREFIXES is about *which request path* a 401 must not react to.

// PUBLIC_ROUTE_PREFIXES names every route whose component calls useMe() while
// genuinely reachable with no session -- the sign-in screen checking "is
// someone already signed in", the invite screen checking "are you signed in as
// someone else". "/sign-in" covers /sign-in and /sign-in/magic; "/invite/"
// covers /invite/$token; "/sign-up" covers /sign-up and /sign-up/$token.
export const PUBLIC_ROUTE_PREFIXES = [
  "/sign-in",
  "/invite/",
  "/sign-up",
] as const;

// PRE_AUTH_API_PREFIXES names the request paths a 401 must never react to.
// Every one is reachable before any session exists, so a 401 from one means
// "that specific attempt failed" -- a wrong password, an expired link, a spent
// token -- not "the session this tab thought it had is gone". Reacting there
// would clear the cache and bounce a caller off the very screen they were using
// to establish a session.
//
// /api/v1/auth/sign-up covers the request, preview and complete endpoints with
// one prefix. /api/v1/currencies is here because the sign-up form's currency
// select fetches it before any session exists.
//
// GET /api/v1/auth/me is deliberately absent: that is the exact endpoint whose
// 401 the handler exists to react to. A screen that is reachable pre-auth but
// still calls useMe() is a different problem, solved by PUBLIC_ROUTE_PREFIXES
// above. Do not add "/api/v1/auth/me" here.
export const PRE_AUTH_API_PREFIXES = [
  "/api/v1/auth/sign-in",
  "/api/v1/auth/magic-link",
  "/api/v1/auth/sign-up",
  "/api/v1/invites/",
  "/api/v1/currencies",
] as const;
```

In `web/src/api/client.ts`, delete the `preAuthPathPrefixes` array (keeping the
explanatory comment above it, which is still accurate and now lives beside the
import) and change the predicate:

```ts
import { PRE_AUTH_API_PREFIXES } from "../routes/publicRoutes";

function isPreAuthRequest(path: string): boolean {
  return PRE_AUTH_API_PREFIXES.some((prefix) => path.startsWith(prefix));
}
```

In `web/src/api/unauthorizedRedirect.ts`, do the same:

```ts
import { PUBLIC_ROUTE_PREFIXES } from "../routes/publicRoutes";

export function isOnPublicRoute(pathname: string): boolean {
  return PUBLIC_ROUTE_PREFIXES.some((prefix) => pathname.startsWith(prefix));
}
```

- [ ] **Step 4: Add the schemas and hooks**

In `web/src/features/auth/schemas.ts`:

```ts
export const signUpPreviewSchema = z.object({ email: z.string() });
export type SignUpPreview = z.infer<typeof signUpPreviewSchema>;

export const currencySchema = z.object({
  code: z.string(),
  // Go marks symbol `omitempty`, so it is absent for codes we have no symbol
  // for -- optional here for the same reason spaceDTO.requiredCapability is.
  symbol: z.string().optional(),
  name: z.string(),
});
export const currencyListSchema = z.object({ currencies: z.array(currencySchema) });
export type Currency = z.infer<typeof currencySchema>;
```

In `web/src/features/auth/useAuth.ts`, following `useRequestMagicLink`'s and
`useAcceptInvite`'s existing shapes exactly:

```ts
// POST /auth/sign-up always answers 202 with the same body, whether or not the
// address has an account. Nothing here can tell the difference, and nothing
// should try: the screen says "check your email" either way.
export function useRequestSignUp() {
  return useMutation({
    mutationFn: (vars: { email: string }) =>
      apiFetch("/api/v1/auth/sign-up", {
        method: "POST",
        body: vars,
        schema: z.object({ status: z.string() }),
      }),
  });
}

export function useSignUpPreview(token: string) {
  return useQuery({
    queryKey: ["sign-up-preview", token] as const,
    queryFn: () =>
      apiFetch(`/api/v1/auth/sign-up/${encodeURIComponent(token)}`, {
        schema: signUpPreviewSchema,
      }),
    // A spent or expired token will never become valid by retrying, and each
    // retry costs the caller a wait before they see the message telling them
    // what to do instead.
    retry: false,
  });
}

export function useCurrencies() {
  return useQuery({
    queryKey: ["currencies"] as const,
    queryFn: () => apiFetch("/api/v1/currencies", { schema: currencyListSchema }),
    // The active ISO 4217 list does not change during a session.
    staleTime: Infinity,
  });
}

export function useCompleteSignUp() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: {
      token: string;
      householdName: string;
      displayName: string;
      primaryCurrency: string;
      password: string;
    }) =>
      apiFetch(`/api/v1/auth/sign-up/${encodeURIComponent(vars.token)}/complete`, {
        method: "POST",
        body: {
          householdName: vars.householdName,
          displayName: vars.displayName,
          primaryCurrency: vars.primaryCurrency,
          password: vars.password,
        },
        schema: meSchema,
      }),
    // The response IS the me bundle and the cookies are already set, so seed
    // the cache directly rather than invalidating and refetching -- the same
    // thing useSignIn does.
    onSuccess: (data) => {
      queryClient.setQueryData(meQueryKey, data);
    },
  });
}
```

Check how `useSignIn` handles its success (line 27-42) and match it exactly — if
it invalidates rather than seeds, do that instead, so all four session-creating
mutations behave identically.

- [ ] **Step 5: Extract `CheckYourEmailPanel`**

Create `web/src/features/auth/CheckYourEmailPanel.tsx` by moving the markup out
of `MagicLinkSentPanel.tsx` verbatim and parameterising only the copy. Keep every
behavioural prop — `pending`, `error`, `onResend`, `onBack` — because each is
load-bearing (the existing file's comments explain why the `role="alert"` error
block cannot be dropped: this panel is the only thing on screen once "sent" mode
is entered, so if it does not surface a failed resend, nothing will).

```tsx
// The shared "we've sent you something" card. MagicLinkSentPanel and
// SignUpScreen's sent state are both callers; the markup lives here once so the
// two cannot drift apart visually.
//
// The design document has no equivalent screen for this state, so the copy is
// original, written in the sign-in screen's voice.
export function CheckYourEmailPanel({
  heading,
  body,
  pending,
  error,
  resendLabel,
  pendingResendLabel,
  onResend,
  backLabel,
  onBack,
}: {
  heading: string;
  body: string;
  pending: boolean;
  // A resend calls the same endpoint as the original send; a failure (429, 500,
  // a network rejection) must not look identical to success, because this panel
  // is the only thing on screen.
  error: string | null;
  resendLabel: string;
  pendingResendLabel: string;
  onResend: () => void;
  backLabel: string;
  onBack: () => void;
}) {
  return (
    <main className="min-h-screen grid place-items-center bg-canvas p-6 font-sans text-ink">
      <div className="flex flex-col items-center gap-[22px]">
        <div className="flex items-center gap-2.5">
          <div className="h-[30px] w-[30px] rounded-[9px] bg-accent" />
          <div className="text-[17px] font-semibold tracking-[-0.01em]">Hearth</div>
        </div>

        <div className="w-[428px] rounded-2xl border border-hairline bg-card px-8 pb-[26px] pt-[30px] text-center shadow-[var(--shadow-auth-card)]">
          <h1 className="mb-1 mt-0.5 font-serif text-[27px] font-medium tracking-[-0.015em]">
            {heading}
          </h1>
          <p className="mb-1 text-[13px] leading-relaxed text-muted">{body}</p>
          <p className="mb-5 text-[13px] leading-relaxed text-muted">
            Don't see it? Check spam, or send another.
          </p>

          <button
            type="button"
            onClick={onResend}
            disabled={pending}
            className="w-full rounded-[9px] bg-accent py-3 text-center text-[13.5px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
          >
            {pending ? pendingResendLabel : resendLabel}
          </button>
          {error && (
            <div
              role="alert"
              className="mt-2 flex items-start justify-center gap-1.5 text-xs leading-snug text-danger"
            >
              <span className="font-bold">!</span>
              <span>{error}</span>
            </div>
          )}
          <button
            type="button"
            onClick={onBack}
            className="mt-3 text-[12.5px] font-medium text-accent"
          >
            {backLabel}
          </button>
        </div>
      </div>
    </main>
  );
}
```

Then reduce `MagicLinkSentPanel.tsx` to a caller supplying its existing copy
unchanged, so its rendered output is byte-identical to before:

```tsx
export function MagicLinkSentPanel({
  email,
  pending,
  error,
  onResend,
  onBack,
}: {
  email: string;
  pending: boolean;
  error: string | null;
  onResend: () => void;
  onBack: () => void;
}) {
  return (
    <CheckYourEmailPanel
      heading="Check your email."
      body={`If ${email || "that address"} has an account, we've sent a one-time sign-in link. It's good for the next 15 minutes.`}
      pending={pending}
      error={error}
      resendLabel="Send another link"
      pendingResendLabel="Sending…"
      onResend={onResend}
      backLabel="Use a password instead"
      onBack={onBack}
    />
  );
}
```

Keep this file's existing doc comment: it explains why the 15-minute figure and
the spam hint are in the copy at all, and why the conditional "If {email} has an
account" phrasing is deliberate rather than clumsy.

The original body is split across two `<p>` elements in the current file (lines
37-44). Collapsing the first into one `body` string is the only rendered change,
and it is why Step 6 runs the existing tests: if any of them asserts on the two
paragraphs separately, keep the split by giving `CheckYourEmailPanel` a second
optional `bodyDetail` prop rather than changing the assertion.

- [ ] **Step 6: Run the existing magic-link tests to prove the extraction changed nothing**

Run: `cd web && npx vitest run src/features/auth/SignInScreen.test.tsx`
Expected: PASS, unchanged. This is a pure refactor; a failure means the copy or
the behaviour moved.

- [ ] **Step 7: Write the failing `SignUpScreen` test**

Create `web/src/features/auth/SignUpScreen.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { SignUpScreen } from "./SignUpScreen";

describe("SignUpScreen", () => {
  it("renders the design's create-household copy", () => {
    stubFetchRoutes({});
    renderWithRouter(<SignUpScreen />);
    expect(screen.getByText("Start your household.")).toBeInTheDocument();
    expect(
      screen.getByText(
        "One household, two owners. Set it up once and invite your partner in.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create household" })).toBeInTheDocument();
  });

  // The design gates all three on authNotCreate.
  it("hides the magic-link controls the design hides in this state", () => {
    stubFetchRoutes({});
    renderWithRouter(<SignUpScreen />);
    expect(screen.queryByRole("button", { name: "Forgot?" })).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Email me a one-time sign-in link" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("or")).not.toBeInTheDocument();
  });

  it("asks only for an email address at this step", () => {
    stubFetchRoutes({});
    renderWithRouter(<SignUpScreen />);
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
    // Household name, your name and password come after the link is clicked --
    // nothing is stored until the address is verified.
    expect(screen.queryByLabelText("Household name")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
  });

  it("shows the sent panel after a successful request", async () => {
    stubFetchRoutes({
      "POST /api/v1/auth/sign-up": { status: 202, body: { status: "accepted" } },
    });
    renderWithRouter(<SignUpScreen />);

    await userEvent.type(screen.getByLabelText("Email"), "founder@example.test");
    await userEvent.click(screen.getByRole("button", { name: "Create household" }));

    await waitFor(() => {
      expect(screen.getByText("Check your email.")).toBeInTheDocument();
    });
    // The panel must describe both outcomes: it cannot know which mail was sent,
    // and must not appear to.
    expect(screen.getByText(/already have an account/i)).toBeInTheDocument();
  });

  it("does not post an implausible address", async () => {
    // stubFetchRoutes throws on an unregistered request, so registering nothing
    // is the assertion: if the component posts, the test fails.
    stubFetchRoutes({});
    renderWithRouter(<SignUpScreen />);
    await userEvent.type(screen.getByLabelText("Email"), "not-an-email");
    await userEvent.click(screen.getByRole("button", { name: "Create household" }));
    expect(
      await screen.findByText("Enter your email address to create a household."),
    ).toBeInTheDocument();
  });
});
```

Check `stubFetchRoutes`'s exact call signature first — it matches on method and
URL and throws on an unregistered request:

Run: `cd web && sed -n '1,40p' src/test/fetchStub.ts`

- [ ] **Step 8: Run it to verify it fails, then write the screen**

Run: `cd web && npx vitest run src/features/auth/SignUpScreen.test.tsx`
Expected: FAIL — cannot resolve `./SignUpScreen`.

Create `web/src/features/auth/SignUpScreen.tsx`:

```tsx
// Step 1 of the design's "Create household" state (design/Household
// Dashboard.dc.html, the authCreate branch): the email address only.
//
// The design draws one card that creates the household on submit. This is
// deliberately split in two, because collecting the household name and display
// name before the address is verified would let someone submit a sign-up for
// another person's address with a household name and a display name of their
// choosing -- and the mail would then invite that person into a household a
// stranger had configured. The person who clicks the link supplies their own
// details, on SignUpCompleteScreen.
//
// Forgot?, the "or" divider and the magic-link button are all absent because
// the design gates them on authNotCreate.
import { type FormEvent, useState } from "react";
import { Link } from "@tanstack/react-router";
import { apiErrorMessage, isPlausibleEmail } from "./copy";
import { CheckYourEmailPanel } from "./CheckYourEmailPanel";
import { useRequestSignUp } from "./useAuth";

export function SignUpScreen() {
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  // unknown, not ApiError | null: an onError handler receives whatever the
  // mutation rejected with, which can be a network TypeError or a zod
  // ParseError, not only an ApiError.
  const [error, setError] = useState<unknown>(null);
  const [validationError, setValidationError] = useState<string | null>(null);

  const requestSignUp = useRequestSignUp();

  function submit() {
    if (requestSignUp.isPending) return;

    const trimmed = email.trim();
    // The button is inside a <form>, so `required` covers the empty case -- but
    // an obviously-malformed address must not reach the API either, or the sent
    // panel appears for a request that could never deliver.
    if (!isPlausibleEmail(trimmed)) {
      setValidationError("Enter your email address to create a household.");
      return;
    }
    setValidationError(null);
    setError(null);
    requestSignUp.mutate(
      { email: trimmed },
      {
        onSuccess: () => setSent(true),
        onError: (err) => setError(err),
      },
    );
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    submit();
  }

  if (sent) {
    return (
      <CheckYourEmailPanel
        heading="Check your email."
        // Describes both outcomes in one sentence. This panel cannot know which
        // mail was sent -- the API answers identically for a fresh address and a
        // registered one -- and must not appear to.
        body={`We've sent a link to ${email.trim() || "that address"}. If that address already has an account, we've sent sign-in instructions instead. Either way it's good for the next 24 hours.`}
        pending={requestSignUp.isPending}
        error={
          error ? apiErrorMessage(error, "That didn't go through. Please try again.") : null
        }
        resendLabel="Send another link"
        pendingResendLabel="Sending…"
        onResend={submit}
        backLabel="Use a different address"
        onBack={() => setSent(false)}
      />
    );
  }

  return (
    <main className="min-h-screen grid place-items-center bg-canvas p-6 font-sans text-ink">
      <div className="flex flex-col items-center gap-[22px]">
        <div className="flex items-center gap-2.5">
          <div className="h-[30px] w-[30px] rounded-[9px] bg-accent" />
          <div className="text-[17px] font-semibold tracking-[-0.01em]">Hearth</div>
        </div>

        <div className="w-[428px] rounded-2xl border border-hairline bg-card px-8 pb-[26px] pt-[30px] shadow-[var(--shadow-auth-card)]">
          <h1 className="mt-0.5 mb-1 font-serif text-[27px] font-medium tracking-[-0.015em]">
            Start your household.
          </h1>
          <p className="mb-5 text-[13px] leading-relaxed text-muted">
            One household, two owners. Set it up once and invite your partner in.
          </p>

          <form className="flex flex-col gap-3.5" onSubmit={handleSubmit}>
            <div className="flex flex-col gap-1.5">
              <label htmlFor="sign-up-email" className="text-xs font-semibold text-label">
                Email
              </label>
              <input
                id="sign-up-email"
                type="email"
                autoComplete="email"
                required
                value={email}
                onChange={(event) => {
                  setEmail(event.target.value);
                  if (validationError) setValidationError(null);
                  if (error) setError(null);
                }}
                className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
              />
              {(validationError || error) && (
                <div
                  role="alert"
                  className="mt-px flex items-start gap-1.5 text-xs leading-snug text-danger"
                >
                  <span className="font-bold">!</span>
                  <span>
                    {validationError ??
                      apiErrorMessage(error, "That didn't go through. Please try again.")}
                  </span>
                </div>
              )}
            </div>

            <button
              type="submit"
              disabled={requestSignUp.isPending}
              className="mt-1 rounded-[9px] bg-accent py-3 text-center text-[13.5px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
            >
              Create household
            </button>
          </form>

          <div className="mt-[18px] border-t border-hairline pt-[15px] text-center text-[12.5px] text-muted">
            <span>Already set up? </span>
            <Link to="/sign-in" className="cursor-pointer font-semibold text-accent">
              Sign in
            </Link>
          </div>
        </div>

        <p className="max-w-[428px] text-center text-xs leading-relaxed text-muted">
          You can invite your partner right after — nothing is shared until they accept.
          <br />
          <span className="text-[11px]">
            Your household data stays between the two of you.
          </span>
        </p>
      </div>
    </main>
  );
}
```

Two things to verify rather than assume, because both come from the existing
codebase and a wrong guess here is a compile error:

Run: `cd web && grep -n "export function isPlausibleEmail\|export function apiErrorMessage" src/features/auth/copy.ts`

Expected: both exist. `SignInScreen.tsx` already imports both.

- [ ] **Step 9: Add the sign-in footer link**

In `SignInScreen.tsx`, after the magic-link button and its error block, before
the closing card `</div>`, add the design's footer verbatim:

```tsx
        <div className="mt-[18px] border-t border-[var(--hairline-soft)] pt-[15px] text-center text-[12.5px] text-muted">
          <span>No household yet? </span>
          <Link to="/sign-up" className="cursor-pointer font-semibold text-accent">
            Create one
          </Link>
        </div>
```

Import `Link` from `@tanstack/react-router`. If `--hairline-soft` is not a
defined token, use the existing `border-hairline` class instead — check
`web/src/index.css` for which tokens exist rather than inventing one.

- [ ] **Step 10: Register the routes**

In `web/src/routes/router.tsx`, add beside `inviteRoute`:

```tsx
const signUpRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/sign-up",
  component: function SignUpRouteComponent() {
    const me = useMe();
    // Same "already signed in -> enter the app" wrapper the sign-in route uses:
    // someone with a live session has no use for a create-household form.
    if (me.isSuccess) return <Navigate to="/" replace />;
    return <SignUpScreen />;
  },
});

const signUpCompleteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/sign-up/$token",
  // Deliberately does NOT bounce an already-signed-in caller, for the same
  // reason inviteRoute does not: the link is often opened on a shared device
  // that is already signed in as someone else, and dropping that visitor on the
  // dashboard with no explanation would be worse than showing the form.
  component: function SignUpCompleteRouteComponent() {
    const { token } = useParams({ from: "/sign-up/$token" });
    return <SignUpCompleteScreen token={token} />;
  },
});
```

Add both to `rootRoute.addChildren([...])`. `SignUpCompleteScreen` arrives in
Task 31 — for this task, import it and let the build fail, or create the file
with a minimal component and finish it next task. Prefer the latter so this task
commits green:

```tsx
// Filled in by Task 31.
export function SignUpCompleteScreen({ token }: { token: string }) {
  return <SignUpScreen key={token} />;
}
```

Replace that body entirely in Task 31 — it exists only so this task's route
registration compiles and its tests run.

- [ ] **Step 11: Run the frontend gate**

Run: `cd web && npx vitest run && npm run lint && npx tsc --noEmit`
Expected: PASS. The `publicRoutes` test now sees `/sign-up` and `/sign-up/$token`
in the tree and finds both covered.

- [ ] **Step 12: Commit**

```bash
git add web/src/routes/publicRoutes.ts web/src/routes/publicRoutes.test.ts \
        web/src/routes/router.tsx web/src/api/client.ts web/src/api/unauthorizedRedirect.ts \
        web/src/features/auth/CheckYourEmailPanel.tsx \
        web/src/features/auth/MagicLinkSentPanel.tsx \
        web/src/features/auth/SignUpScreen.tsx web/src/features/auth/SignUpScreen.test.tsx \
        web/src/features/auth/SignInScreen.tsx \
        web/src/features/auth/useAuth.ts web/src/features/auth/schemas.ts
git commit -m "feat(web): add the create-household entry point and one public-route list

preAuthPathPrefixes and publicRoutePrefixes were two hand-maintained lists with
nothing tying either to the route tree, and HANDOVER.md already recorded that a
public route calling useMe() reintroduces a fixed bug. Sign-up added two such
routes, so they are now one module with a test that walks the real tree and
fails if a route escapes it.

Step 1 of the design's create card asks only for an email address. Nothing is
stored until the address is verified, so a stranger cannot submit a sign-up for
someone else's address with a household name of their choosing."
```

---

### Task 31: Step 2 of the card — household name, currency, your name, password

**Files:**
- Rewrite: `web/src/features/auth/SignUpCompleteScreen.tsx` (replacing Task 30's stand-in)
- Create: `web/src/features/auth/SignUpCompleteScreen.test.tsx`
- Modify: `web/src/features/settings/copy.ts` (`currencyLabel` reads served data)

**Interfaces:**
- Consumes: `useSignUpPreview`, `useCurrencies`, `useCompleteSignUp` (Task 30).
- Produces: the finished `<SignUpCompleteScreen token={string} />`.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/auth/SignUpCompleteScreen.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { SignUpCompleteScreen } from "./SignUpCompleteScreen";

const preview = {
  "GET /api/v1/auth/sign-up/tok": { status: 200, body: { email: "founder@example.test" } },
  "GET /api/v1/currencies": {
    status: 200,
    body: {
      currencies: [
        { code: "BRL", symbol: "R$", name: "Brazilian real" },
        { code: "SGD", symbol: "S$", name: "Singapore dollar" },
      ],
    },
  },
};

describe("SignUpCompleteScreen", () => {
  it("shows the verified address read-only", async () => {
    stubFetchRoutes(preview);
    renderWithRouter(<SignUpCompleteScreen token="tok" />);

    const email = await screen.findByLabelText("Email");
    expect(email).toHaveValue("founder@example.test");
    // The address is what the token proved. Letting it be edited would mean the
    // form could create an account for an address nobody verified.
    expect(email).toHaveAttribute("readonly");
  });

  it("asks for the design's fields, with its helper text and the corrected hint", async () => {
    stubFetchRoutes(preview);
    renderWithRouter(<SignUpCompleteScreen token="tok" />);

    expect(await screen.findByLabelText("Household name")).toBeInTheDocument();
    expect(
      screen.getByText("Shown at the top of the sidebar. Change it any time."),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Your name")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toBeInTheDocument();
    // The design says "At least 10 characters"; the codebase enforces 12.
    expect(screen.getByText("At least 12 characters")).toBeInTheDocument();
  });

  it("offers no pre-selected currency", async () => {
    stubFetchRoutes(preview);
    renderWithRouter(<SignUpCompleteScreen token="tok" />);
    // Defaulting to SGD would ship a wrong-currency first impression to
    // everyone who does not notice the field.
    expect(await screen.findByLabelText("Primary currency")).toHaveValue("");
  });

  it("submits every field and enters the app", async () => {
    let posted: unknown = null;
    stubFetchRoutes({
      ...preview,
      "POST /api/v1/auth/sign-up/tok/complete": {
        status: 200,
        body: {
          user: { id: "u1", email: "founder@example.test", displayName: "Ade", avatarInitial: "A" },
          household: {
            id: "h1", name: "Ade & Kris", familyName: "Ade & Kris",
            primaryCurrency: "BRL", showSecondaryCurrency: false,
            secondaryCurrency: "BRL", fxRateMode: "auto",
          },
          membership: {
            id: "m1", householdId: "h1", userId: "u1", role: "owner",
            capabilities: ["calendar", "chores", "money", "marriage"],
          },
          capabilities: ["calendar", "chores", "money", "marriage"],
          spaces: [],
        },
        capture: (body: unknown) => {
          posted = body;
        },
      },
    });
    renderWithRouter(<SignUpCompleteScreen token="tok" />);

    await userEvent.type(await screen.findByLabelText("Household name"), "Ade & Kris");
    await userEvent.selectOptions(screen.getByLabelText("Primary currency"), "BRL");
    await userEvent.type(screen.getByLabelText("Your name"), "Ade");
    await userEvent.type(screen.getByLabelText("Password"), "a-long-enough-password");
    await userEvent.click(screen.getByRole("button", { name: "Create household" }));

    await waitFor(() => {
      expect(posted).toEqual({
        householdName: "Ade & Kris",
        displayName: "Ade",
        primaryCurrency: "BRL",
        password: "a-long-enough-password",
      });
    });
  });

  it("explains a spent token and points back at sign-up", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/sign-up/tok": {
        status: 409,
        body: {
          error: {
            code: "SIGNUP_ALREADY_USED",
            message: "This link has already been used. Try signing in instead.",
          },
        },
      },
      ...{ "GET /api/v1/currencies": preview["GET /api/v1/currencies"] },
    });
    renderWithRouter(<SignUpCompleteScreen token="tok" />);
    expect(await screen.findByText(/already been used/i)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /sign in/i })).toBeInTheDocument();
  });

  it("explains an expired token", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/sign-up/tok": {
        status: 410,
        body: {
          error: { code: "TOKEN_EXPIRED", message: "This link has expired or has already been used." },
        },
      },
      ...{ "GET /api/v1/currencies": preview["GET /api/v1/currencies"] },
    });
    renderWithRouter(<SignUpCompleteScreen token="tok" />);
    expect(
      await screen.findByText("This link has expired. Start again to get a new one."),
    ).toBeInTheDocument();
  });
});
```

`stubFetchRoutes` may not support a `capture` callback. Check:

Run: `cd web && cat src/test/fetchStub.ts`

If it does not, add it — a per-route optional `capture(body)` invoked with the
parsed request body — rather than asserting on a global fetch spy, so the stub
stays the single place tests describe the network.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/features/auth/SignUpCompleteScreen.test.tsx`
Expected: FAIL — the stand-in component from Task 30 renders `SignUpScreen`, so
`Household name` is absent.

- [ ] **Step 3: Write the screen**

Rewrite `web/src/features/auth/SignUpCompleteScreen.tsx`. Structure, matching the
design's own ordering (the create-specific block sits *above* the shared
email/password block):

1. While `useSignUpPreview` is loading, render the card shell with a quiet
   loading line — not a blank screen, which reads as a broken link.
2. On a preview error, render the whole card as an explanation, following
   `InviteScreen`'s `InvitePreviewError` pattern: branch on the `ApiError`
   `code`, never on the server `message`. `SIGNUP_ALREADY_USED` →
   `This link has already been used.` plus a `Sign in` link to `/sign-in`.
   `TOKEN_EXPIRED` → `This link has expired. Start again to get a new one.` plus
   a `Create a household` link to `/sign-up`. `NOT_FOUND` → the same as expired,
   because a token that never existed and one that has lapsed need the same next
   step. Anything else → the generic message via `apiErrorMessage`.
3. On success, the form: **Household name** (with the design's helper text),
   **Primary currency** (a `<select>` from `useCurrencies`, first option
   `<option value="">Choose a currency</option>`, then `{code} — {name}`),
   **Your name**, **Email** (read-only, value from the preview), **Password**
   with the hint `At least 12 characters`, and the `Create household` submit.
4. Client-side guards before submitting, so an obviously-invalid form never
   posts: non-blank household name, a chosen currency, non-blank display name,
   password at least 12 characters. Each renders a `role="alert"` message.
5. `useCompleteSignUp` on submit. On success `navigate({ to: "/", replace: true })`
   — the cookies are already set and the me cache is seeded, so the app is ready.
6. On error, show `apiErrorMessage(err, "Something went wrong. Please try again.")`,
   and leave every field populated: the token survives a rejected submission, so
   the person must be able to correct one field and retry.

Take the card shell — `<main>`, the logo block, the `w-[428px]` card, the footer
paragraphs — verbatim from `SignUpScreen.tsx` (Task 30, Step 8 has it in full),
and the field markup verbatim from `SignInScreen.tsx`, so all three cards are
visually identical.

The error branch has no analogue elsewhere in this plan, so here it is concretely:

```tsx
// Branches on the ApiError code, never on the server's message. InviteScreen
// does the same, for the reason its own comment gives: the message is copy the
// backend owns and may reword, while the code is the contract.
function SignUpTokenError({ error }: { error: unknown }) {
  const code = error instanceof ApiError ? error.code : null;

  let message: string;
  let action: { to: string; label: string };
  switch (code) {
    case "SIGNUP_ALREADY_USED":
      message = "This link has already been used.";
      action = { to: "/sign-in", label: "Sign in" };
      break;
    case "TOKEN_EXPIRED":
    // A token that never existed and one that has lapsed need the same next
    // step, so they share a branch rather than telling the visitor which of the
    // two it was -- which would also confirm whether a given token was ever
    // issued.
    case "NOT_FOUND":
      message = "This link has expired. Start again to get a new one.";
      action = { to: "/sign-up", label: "Create a household" };
      break;
    default:
      message = apiErrorMessage(error, "We couldn't open that link. Please try again.");
      action = { to: "/sign-up", label: "Create a household" };
  }

  return (
    <main className="min-h-screen grid place-items-center bg-canvas p-6 font-sans text-ink">
      <div className="flex flex-col items-center gap-[22px]">
        <div className="flex items-center gap-2.5">
          <div className="h-[30px] w-[30px] rounded-[9px] bg-accent" />
          <div className="text-[17px] font-semibold tracking-[-0.01em]">Hearth</div>
        </div>
        <div className="w-[428px] rounded-2xl border border-hairline bg-card px-8 pb-[26px] pt-[30px] text-center shadow-[var(--shadow-auth-card)]">
          <h1 className="mb-1 mt-0.5 font-serif text-[27px] font-medium tracking-[-0.015em]">
            That link won't work.
          </h1>
          <p role="alert" className="mb-5 text-[13px] leading-relaxed text-muted">
            {message}
          </p>
          <Link
            to={action.to}
            className="block w-full rounded-[9px] bg-accent py-3 text-center text-[13.5px] font-semibold text-white"
          >
            {action.label}
          </Link>
        </div>
      </div>
    </main>
  );
}
```

And the currency select, which is the one field with no precedent in the codebase:

```tsx
            <div className="flex flex-col gap-1.5">
              <label htmlFor="sign-up-currency" className="text-xs font-semibold text-label">
                Primary currency
              </label>
              <select
                id="sign-up-currency"
                required
                value={currency}
                onChange={(event) => setCurrency(event.target.value)}
                className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
              >
                {/* No pre-selection. Defaulting to SGD would ship a
                    wrong-currency first impression to everyone who did not
                    notice the field, which is the reason the field exists. */}
                <option value="">Choose a currency</option>
                {(currencies.data?.currencies ?? []).map((c) => (
                  <option key={c.code} value={c.code}>
                    {c.symbol ? `${c.code} (${c.symbol}) — ${c.name}` : `${c.code} — ${c.name}`}
                  </option>
                ))}
              </select>
            </div>
```

Import `ApiError` from `../../api/client` and `Link` from
`@tanstack/react-router`.

- [ ] **Step 4: Run the tests**

Run: `cd web && npx vitest run src/features/auth/SignUpCompleteScreen.test.tsx`
Expected: PASS, six tests.

- [ ] **Step 5: Point `currencyLabel` at served data**

In `web/src/features/settings/copy.ts`, `CURRENCY_SYMBOLS` is a parallel list of
three symbols. Change `currencyLabel` to take an optional symbol:

```ts
// The symbol now comes from GET /api/v1/currencies rather than a list
// maintained here -- one list, and it lives in the backend. Callers that have
// not loaded the currency list yet pass nothing and get the bare code, which is
// what an unrecognised code always rendered as.
export function currencyLabel(code: string, symbol?: string): string {
  return symbol ? `${code} (${symbol})` : code;
}
```

Delete `CURRENCY_SYMBOLS`. Then update `CurrencyPanel.tsx` to call
`useCurrencies()` and pass the matching symbol through. Run the panel's tests and
fix whatever the signature change breaks:

Run: `cd web && npx vitest run src/features/settings/CurrencyPanel.test.tsx`
Expected: PASS once the test's stub registers `GET /api/v1/currencies`.

- [ ] **Step 6: Full frontend gate**

Run: `cd web && npx vitest run && npm run lint && npx tsc --noEmit`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/auth/SignUpCompleteScreen.tsx \
        web/src/features/auth/SignUpCompleteScreen.test.tsx \
        web/src/features/settings/copy.ts web/src/features/settings/CurrencyPanel.tsx \
        web/src/features/settings/CurrencyPanel.test.tsx \
        web/src/test/fetchStub.ts
git commit -m "feat(web): finish the create-household form

The email is read-only and comes from the preview: it is the address the token
proved, and letting it be edited would mean the form could create an account for
an address nobody verified.

No currency is pre-selected. Defaulting to SGD would ship a wrong-currency first
impression to everyone who did not notice the field, which is the whole reason
the field exists.

The password hint reads 'At least 12 characters', not the design's 'At least
10' -- the backend has always enforced 12 and MapDomainError already says so.

currencyLabel now takes a symbol from the served list instead of the frontend
keeping its own three-entry copy."
```

---

### Task 32: Walk the definition of done

**Files:**
- Create: `docs/superpowers/plans/2026-07-27-hearth-signup-verification.md`
- Modify: `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`, `docs/HANDOVER.md`

- [ ] **Step 1: Start from nothing**

```bash
make down
docker volume rm hearth_hearth-pgdata || true
make up && make seed
make dev
```

- [ ] **Step 2: Walk every criterion, recording PASS or the failure as you go**

At `http://localhost:5173`, with Mailpit at `http://localhost:8025`:

1. On `/sign-in`, confirm the footer reads `No household yet?` with a `Create one` link. Click it.
2. On `/sign-up`, confirm there is no `Forgot?`, no `or` divider and no magic-link button, and that only an Email field is present.
3. Submit `founder@example.test`. Confirm the `Check your email.` panel appears and its copy mentions both outcomes.
4. In Mailpit, open the sign-up mail and follow its link.
5. Confirm the form shows `founder@example.test` read-only, a **Household name** field with the helper `Shown at the top of the sidebar. Change it any time.`, a **Primary currency** select with nothing chosen, **Your name**, and a password hint of `At least 12 characters`.
6. Submit with an 8-character password; confirm it is refused without consuming the link.
7. Submit properly, choosing **BRL**. Confirm you land in the app signed in, and that the sidebar shows Overview, Money, Marriage, Family and Settings.
8. In Settings → Currency & region, confirm the primary currency reads BRL and the secondary-currency toggle is **off** and does **not** mention IDR.
9. Sign out. Re-open the same sign-up link. Confirm it explains the link has already been used and offers a `Sign in` link.
10. Submit a sign-up for `founder@example.test` again — now a registered address. Confirm the screen is identical to step 3, and that Mailpit received a "you already have an account" mail with **no** token link.
11. Submit sign-ups for six different fresh addresses in quick succession from the same browser. The **first five** must each render `Check your email.` — identical whether or not the address exists, which is the property under test. The **sixth is expected to show an error**: the per-IP limit is 5/hour, and a `429 RATE_LIMITED` there is correct behaviour, not a failure. That asymmetry is deliberate — a per-IP 429 reveals only how many requests you have sent, which you already know, whereas the per-address and global limits stay silent because those *would* reveal whether an address is registered. Confirm Mailpit received five mails and not six.
12. Sign in as the new owner, invite a second member from Settings, and accept that invite in a private window. Confirm the new household has two owners and that the invite copy names the household.
13. Sign in as Andreas (`make seed`'s account) and confirm his household is untouched: SGD primary, IDR secondary, toggle on, four members.
14. Run `cd api && go run ./cmd/adminctl unlock-household --email founder@example.test` and confirm it reports success against the *new* household, not Andreas's.
15. Run `cd api && go run ./cmd/adminctl prune --older-than 3` and confirm it refuses, naming the seven-day floor.

- [ ] **Step 3: Run the full gate**

```bash
make lint && make test
```
Expected: PASS. Paste the summary lines into the verification file.

- [ ] **Step 4: Update the three documents that must not go stale**

Use the `maintaining-system-design` skill for `SYSTEM_DESIGN.md`: the `signups`
table, the four new routes, the provisioning transaction, and the fact that a
household can now exist without an invite or a seed.

`FEATURE_TRACKER.md`: add rows for self-serve sign-up, household provisioning,
the currency list endpoint and `adminctl prune`; mark the missing
secondary-currency picker 🟡 with its reason; recount the summary table rather
than guessing.

`LEARNING.md`: at minimum the two defects this work found, since both are
patterns rather than one-offs —

- A `DELETE` scoped to a deliberately-nullable column silently spares exactly the rows the nullable case creates (`ClearFailures` and `login_attempts`). Found by asking which tables a stranger can grow, not by any test.
- Slicing a string by byte index to get its "first character" produces invalid UTF-8 for every non-ASCII input (`initialOf`). Invisible while every name was ASCII.

`HANDOVER.md`: the build order changed — record that this slice landed before
slice 2 (Money), and that `requireCapability` is still unused.

- [ ] **Step 5: Commit**

```bash
git add docs/
git commit -m "docs: record the sign-up slice verification walkthrough"
```

---

## Definition of done for this plan

Every criterion in Task 32 passes, recorded in the verification file, with
`make lint` and `make test` green. A stranger can create a household; the
endpoint reveals nothing about which addresses are registered; a partial
provision is impossible; and `login_attempts` no longer grows without bound.

Slice 2 (Money) is next, starting from the derived-figure formulas the
foundation spec's closing section lists. Spec B — the platform admin console —
is specified separately, and `adminctl unlock-household --email` is the
capability its first console screen replaces.
