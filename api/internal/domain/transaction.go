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
// amount when one was recorded, and otherwise the amount that left.
//
// No production code calls this or BalanceEffect below. Read that before
// changing either: the balance every screen shows is summed in SQL, by the
// balance_minor expression in adapter/postgres/queries/account.sql, and
// editing these two would not move it by a cent. They are kept as the
// domain's written statement of what a transaction does to an account -- the
// rule in one readable place, tested in transaction_test.go -- and the SQL
// carries its own copy of it. If a second reader ever needs this arithmetic
// in Go (a projection, an import preview, an undo), this is the shape it
// should take, and the SQL is what it has to agree with.
func (t Transaction) CreditedAmount() Money {
	if t.ReceivedAmount != nil {
		return *t.ReceivedAmount
	}
	return t.Amount
}

// BalanceEffect reports what this transaction does to the named account's
// balance, and whether it touches that account at all. Like CreditedAmount
// above, it has no production caller today -- see that comment for why it is
// still here and what actually computes the balances a household sees.
//
// A transfer supplies both of its effects from this one row, which is why a
// transfer cannot change net worth: the two sides are the same money, and
// there is no second row that could go missing. That invariant is a property
// of the shape rather than a rule someone has to remember.
//
// It returns ok=false in two cases. First, for any account the transaction
// does not touch, because zero is a real effect -- a caller must be able to
// tell "this transaction moved nothing here" from "this transaction is not
// about this account at all". Second, for the unreachable overflow case when
// the amount is math.MinInt64, which cannot be safely negated. The signature
// returns bool rather than error because the overflow guard is unreachable
// through any path this product ships -- the database enforces positive
// amounts -- so an error return would be a second failure mode every future
// caller has to handle in exchange for nothing. A caller must not read
// ok=false as proof the account was untouched.
func (t Transaction) BalanceEffect(accountID string) (Money, bool) {
	if accountID == "" {
		return Money{}, false
	}
	switch {
	case accountID == t.FromAccountID:
		// math.MinInt64 has no positive counterpart in two's complement, so a
		// naive negation would turn the largest possible outflow into an
		// inflow. The amount is constrained positive at the database (see Task 1),
		// making this unreachable -- and it is guarded anyway, for the same
		// reason AccountType.SignedNetWorthAmount guards it: when an amount flows
		// out, negating it must not inadvertently turn it into an inflow.
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
