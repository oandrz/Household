package domain

import (
	"fmt"
	"math"
	"time"
)

// AccountType is what kind of thing an account is. It decides which side of
// the net worth subtraction the account falls on, which is why the
// asset-or-liability answer is derived here rather than stored on the row:
// when per-household custom types arrive, IsLiability becomes a lookup and
// no existing account row changes.
type AccountType string

const (
	AccountCash       AccountType = "cash"
	AccountInvestment AccountType = "investment"
	AccountProperty   AccountType = "property"
	AccountLoan       AccountType = "loan"
	AccountCreditCard AccountType = "credit_card"
)

// ParseAccountType refuses anything it does not recognise. The default is the
// point: a type arrives from a request body or a database column, so it is a
// value this code did not construct, and guessing at an unknown one would put
// an account on the wrong side of net worth. It does not trim or case-fold --
// the only writers are this API and this API's own migration, and accepting
// "CASH " here would mean the database CHECK constraint and this function
// disagreed about what is valid.
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
// worth: the balance as given for an asset, negated for a liability. A
// liability's stored amount is the non-negative sum owed (the database's
// liabilities_are_not_negative constraint enforces the same rule), so the
// minus sign is produced here and never typed by a person -- which is what
// makes "typed 14500 for a car loan and net worth counted it as an asset"
// unrepresentable rather than merely unlikely.
//
// It returns an error rather than negating blindly: negating math.MinInt64 in
// two's complement returns math.MinInt64 itself, so a naive negation would
// turn the largest possible debt into the largest possible asset. Money.String
// guards the same edge for the same reason.
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
// OwnerMembershipID follows the same "" <-> SQL NULL convention documented on
// usecase.StoredUser.PasswordHash: "" means the account is shared by the whole
// household, and the column is NULL. There is deliberately no separate
// is_shared flag -- one would allow a row that both names an owner and claims
// to be shared, with nothing to resolve it.
//
// OpeningBalanceAsOf is load-bearing, not decoration. Once transactions exist,
// only those dated after it count toward the derived balance; without it,
// importing last month's transactions would subtract them from a balance that
// already reflected them.
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
