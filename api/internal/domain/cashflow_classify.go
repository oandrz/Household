package domain

import (
	"errors"
	"fmt"
)

// ErrUnknownCashflowInput is returned when a transaction kind, destination
// account type or cadence is not one the cashflow rules know. The values come
// from database rows, so an unexpected one is refused rather than guessed at.
var ErrUnknownCashflowInput = errors.New("unrecognised cashflow input")

// CashflowRole says how one transaction counts towards a month's cashflow.
type CashflowRole int

const (
	// CashflowIgnore is a transfer that moves money without spending it.
	// A transfer into a credit card is ignored because the card's purchases
	// were already counted as expenses when they were made; counting the
	// payment as well would count the same money twice.
	CashflowIgnore CashflowRole = iota
	// CashflowIn is income.
	CashflowIn
	// CashflowOut is an expense, counted in the month it happened.
	CashflowOut
	// CashflowLoanOut is a transfer into a loan account. Money paid to a loan
	// is money the household can't save, so it counts as money out even
	// though it is a transfer.
	CashflowLoanOut
	// CashflowSpreadOut is an expense linked to a quarterly or yearly bill
	// payment. It is spread over the months the payment covers, not counted
	// all in the month it was paid.
	CashflowSpreadOut
)

// ClassifyCashflow decides how a transaction counts. cadence is "" when the
// transaction is not linked to a bill payment. toType is read only for
// transfers and ignored for income and expense.
func ClassifyCashflow(kind TransactionKind, toType AccountType, cadence Cadence) (CashflowRole, error) {
	switch kind {
	case TransactionIncome:
		return CashflowIn, nil
	case TransactionExpense:
		return classifyExpense(cadence)
	case TransactionTransfer:
		return classifyTransfer(toType)
	default:
		return CashflowIgnore, fmt.Errorf("%w: transaction kind %q", ErrUnknownCashflowInput, kind)
	}
}

func classifyExpense(cadence Cadence) (CashflowRole, error) {
	switch cadence {
	case "", CadenceOneOff, CadenceMonthly:
		return CashflowOut, nil
	case CadenceQuarterly, CadenceYearly:
		return CashflowSpreadOut, nil
	default:
		return CashflowIgnore, fmt.Errorf("%w: cadence %q", ErrUnknownCashflowInput, cadence)
	}
}

func classifyTransfer(toType AccountType) (CashflowRole, error) {
	switch toType {
	case AccountLoan:
		return CashflowLoanOut, nil
	case AccountCash, AccountInvestment, AccountProperty, AccountCreditCard:
		return CashflowIgnore, nil
	default:
		return CashflowIgnore, fmt.Errorf("%w: destination account type %q", ErrUnknownCashflowInput, toType)
	}
}

// SpreadMonths is how many months a bill payment covers: 12 for yearly, 3 for
// quarterly, 1 for anything else.
func SpreadMonths(c Cadence) int {
	switch c {
	case CadenceYearly:
		return 12
	case CadenceQuarterly:
		return 3
	default:
		return 1
	}
}

// SpreadForward splits amountMinor into months parts using integer division.
// The remainder goes to the first part, so the parts add up exactly to
// amountMinor. For a negative amount Go's % keeps the sign, so the remainder
// is still added to the first part and the sum stays exact.
func SpreadForward(amountMinor int64, months int) []int64 {
	parts := make([]int64, months)
	share := amountMinor / int64(months)
	for i := range parts {
		parts[i] = share
	}
	parts[0] += amountMinor % int64(months)
	return parts
}
