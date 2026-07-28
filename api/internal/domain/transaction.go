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
		// the same reason AccountType.SignedNetWorthAmount guards it: when an
		// amount flows out, negating it must not inadvertently turn it into an
		// inflow.
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
