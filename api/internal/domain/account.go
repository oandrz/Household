package domain

import (
	"fmt"
	"math"
	"time"
)

// AccountType decides which side of the net worth subtraction an account
// falls on. IsLiability derives the answer rather than storing it, so a
// future per-household custom type needs no migration of existing rows.
type AccountType string

const (
	AccountCash       AccountType = "cash"
	AccountInvestment AccountType = "investment"
	AccountProperty   AccountType = "property"
	AccountLoan       AccountType = "loan"
	AccountCreditCard AccountType = "credit_card"
)

// ParseAccountType refuses anything it does not recognise: the value
// arrives from a request body or a database column this code did not
// construct, and guessing wrong would put an account on the wrong side of
// net worth. Unlike ParseCurrency it does not trim or case-fold -- this
// value only ever comes from a fixed select or a column this API wrote, so
// an unexpected shape means something upstream is wrong, not something to
// repair.
func ParseAccountType(s string) (AccountType, error) {
	switch AccountType(s) {
	case AccountCash:
		return AccountCash, nil
	case AccountInvestment:
		return AccountInvestment, nil
	case AccountProperty:
		return AccountProperty, nil
	case AccountLoan:
		return AccountLoan, nil
	case AccountCreditCard:
		return AccountCreditCard, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownAccountType, s)
	}
}

// AccountTypes returns every type, in the order the breakdown chart should
// draw them: assets first, then debts. One list, so the HTTP layer and the
// frontend cannot disagree about what exists.
func AccountTypes() []AccountType {
	return []AccountType{
		AccountCash, AccountInvestment, AccountProperty,
		AccountLoan, AccountCreditCard,
	}
}

// IsLiability reports whether this type is money owed rather than money held.
func (t AccountType) IsLiability() bool {
	return t == AccountLoan || t == AccountCreditCard
}

// SignedNetWorthAmount returns the amount this account contributes to net
// worth: as given for an asset, negated for a liability. The minus sign is
// produced here, never typed by a person, because the stored liability amount
// is always non-negative (the liabilities_are_not_negative constraint
// enforces it) -- this is what makes "entered a car loan and net worth
// counted it as an asset" unrepresentable. It errors rather than negating
// blindly: negating math.MinInt64 overflows in two's complement, which would
// turn the largest debt into the largest asset (Money.String guards the same
// edge).
func (t AccountType) SignedNetWorthAmount(m Money) (Money, error) {
	if !t.IsLiability() {
		return m, nil
	}
	if m.Amount == math.MinInt64 {
		return Money{}, fmt.Errorf("%w: cannot negate %d", ErrAmountOverflow, m.Amount)
	}
	return Money{Amount: -m.Amount, Currency: m.Currency}, nil
}

// Account is one thing a household owns or owes.
//
// OwnerMembershipID follows the "" <-> SQL NULL convention documented on
// usecase.StoredUser.PasswordHash: "" means the whole household owns it.
// There is no separate is_shared flag, so a row can never claim both an
// owner and shared status.
//
// OpeningBalanceAsOf is load-bearing: transactions before it don't count
// toward the derived balance, so importing the past can't double-subtract.
type Account struct {
	ID                      string
	HouseholdID             string
	Nickname                string
	Type                    AccountType
	OwnerMembershipID       string
	OpeningBalance          Money
	OpeningBalanceAsOf      time.Time
	CountTowardNetWorth     bool
	VisibleToLimitedMembers bool
	ArchivedAt              *time.Time
}

// IsArchived reports whether this account has been archived. Archived accounts
// leave the list, net worth and the breakdown; they are never deleted, because
// transactions will reference them.
func (a Account) IsArchived() bool { return a.ArchivedAt != nil }
