// This file holds the money slice's cashflow ports. ports.go lists every
// ports file.

package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// CashflowConfirmation is one "looks right, count it" mark on a finished
// month. Month is the first day of that month. ConfirmedByName is the display
// name of the member who confirmed it, or "" when that member has since left.
type CashflowConfirmation struct {
	Month           time.Time
	ConfirmedByName string
	ConfirmedAt     time.Time
}

// CashflowConfirmationRepository stores the months a household has confirmed.
// Every method is scoped to one household.
type CashflowConfirmationRepository interface {
	// List returns every confirmed month, oldest first.
	List(ctx context.Context, householdID string) ([]CashflowConfirmation, error)
	// Confirm is idempotent: an existing row comes back unchanged, so the
	// first person to confirm is the one kept. The caller passes a month that
	// is already the first of a month; the database refuses anything else.
	Confirm(ctx context.Context, householdID string, month time.Time, membershipID string, at time.Time) (CashflowConfirmation, error)
	// Unconfirm treats a missing row as success, not as ErrNotFound.
	Unconfirm(ctx context.Context, householdID string, month time.Time) error
}

// CashflowRow is one transaction as the cashflow page reads it: just enough to
// decide whether it is money in, money out, a loan repayment or a bill payment
// to spread.
type CashflowRow struct {
	TransactionID string
	OccurredOn    time.Time
	Kind          domain.TransactionKind
	// Amount is the from side, what left the source account. For a transfer
	// between currencies it is not what arrived.
	Amount domain.Money
	// ToAccountType and ToAccountName describe the destination account and are
	// "" unless Kind is a transfer.
	ToAccountType domain.AccountType
	ToAccountName string
	// BillCadence and BillName are "" unless the transaction is the expense a
	// bill payment still points at.
	BillCadence domain.Cadence
	BillName    string
}

// CashflowRowReader reads a household's whole history in one query, so the
// service never runs a query per month.
type CashflowRowReader interface {
	// Rows returns every transaction in the household, oldest first. The
	// count is bounded by domain.MaxTransactions. Kind, account type and
	// cadence are parsed with the domain Parse* functions, which refuse
	// unknown values. A transaction that appears twice (two payments linked to
	// it) is an error, not a double count.
	Rows(ctx context.Context, householdID string) ([]CashflowRow, error)
}
