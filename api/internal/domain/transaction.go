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
// The four id fields use "" for SQL NULL, as Account.OwnerMembershipID
// does: an expense has no ToAccountID, an income no FromAccountID, a
// transfer both -- accounts_match_kind enforces this at the database
// too, against a silently wrong balance.
//
// ReceivedAmount is what landed in the destination account, in its own
// currency: nil in the ordinary same-currency case, required when a
// transfer's accounts differ, optional (a bank fee) when they match.
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
	// IdempotencyKey is the caller's own handle on this create, "" when none
	// was given (every row the web app writes). Unique per household while
	// the row exists; see ValidateIdempotencyKey and the 00015 migration.
	IdempotencyKey string
}

// MaxIdempotencyKeyLength caps a caller-supplied key, whose shape is refused
// rather than trusted: 1 to 128 printable ASCII characters, no spaces --
// long enough for a hash, short enough to index, and safe to carry verbatim
// in a header or log line.
const MaxIdempotencyKeyLength = 128

// ValidateIdempotencyKey reports ErrIdempotencyKeyInvalid for anything a
// key must not be. Empty is invalid on purpose: "" is the stored meaning
// of "no key", so an empty header is refused, not silently read as none.
func ValidateIdempotencyKey(key string) error {
	if key == "" || len(key) > MaxIdempotencyKeyLength {
		return ErrIdempotencyKeyInvalid
	}
	for i := 0; i < len(key); i++ {
		if key[i] <= ' ' || key[i] > '~' {
			return ErrIdempotencyKeyInvalid
		}
	}
	return nil
}

// SameCreate reports whether a repeated create with this key asked for the
// same transaction as the stored one: every caller-controlled field,
// compared after validation normalises both sides. The id and key are
// excluded -- the key is what matched them, the id is the server's -- and
// a nil ReceivedAmount is not treated as equal to a non-nil one.
func (t Transaction) SameCreate(stored Transaction) bool {
	if t.Kind != stored.Kind ||
		!t.OccurredOn.Equal(stored.OccurredOn) ||
		t.Description != stored.Description ||
		t.CategoryID != stored.CategoryID ||
		t.PaidByMembershipID != stored.PaidByMembershipID ||
		t.FromAccountID != stored.FromAccountID ||
		t.ToAccountID != stored.ToAccountID ||
		t.Amount != stored.Amount {
		return false
	}
	switch {
	case t.ReceivedAmount == nil && stored.ReceivedAmount == nil:
		return true
	case t.ReceivedAmount == nil || stored.ReceivedAmount == nil:
		return false
	default:
		return *t.ReceivedAmount == *stored.ReceivedAmount
	}
}

// CreditedAmount is what arrives in the destination account: the received
// amount if one was recorded, else the amount sent. No production code
// calls it or BalanceEffect: balances are summed in SQL (balance_minor in
// adapter/postgres/queries/account.sql), so editing them moves no balance.
// They state the rule in Go; a future Go caller must agree with that SQL.
func (t Transaction) CreditedAmount() Money {
	if t.ReceivedAmount != nil {
		return *t.ReceivedAmount
	}
	return t.Amount
}

// BalanceEffect reports what this transaction does to the named account's
// balance, and whether it touches that account at all. No production
// caller today -- see CreditedAmount above for why. A transfer supplies
// both effects from one row, so it can never move net worth: the two
// sides are the same money, with no second row to go missing.
//
// ok=false means either the account isn't touched (zero is a real effect,
// so this must be distinguishable from "not about this account"), or the
// unreachable math.MinInt64 overflow case, so ok=false is not proof the
// account was untouched. It's bool, not error, because that overflow
// guard never fires -- the database enforces positive amounts -- so
// error would be a needless failure mode for every caller.
func (t Transaction) BalanceEffect(accountID string) (Money, bool) {
	if accountID == "" {
		return Money{}, false
	}
	switch {
	case accountID == t.FromAccountID:
		// math.MinInt64 has no positive counterpart in two's complement, so
		// naive negation would flip the largest outflow into an inflow.
		// Unreachable since the database constrains amounts to be positive;
		// guarded anyway, matching AccountType.SignedNetWorthAmount.
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
