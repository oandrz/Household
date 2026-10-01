package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// NewAccount is the create input: a struct, not nine parameters, because
// five of them share two types and a swapped pair would still compile.
// OwnerMembershipID follows the "" <-> SQL NULL convention: "" means shared.
type NewAccount struct {
	HouseholdID             string
	Nickname                string
	Type                    string
	OwnerMembershipID       string
	OpeningBalanceMinor     int64
	OpeningBalanceCurrency  string
	OpeningBalanceAsOf      time.Time
	CountTowardNetWorth     bool
	VisibleToLimitedMembers bool
}

// AccountUpdate is a real patch: a nil pointer means "leave this field
// alone". OwnerMembershipID is a *string, not **string: nil leaves the
// owner unchanged, a pointer to "" clears it to shared -- without that
// second state, a wrongly assigned account could never be un-assigned.
type AccountUpdate struct {
	Nickname                *string
	Type                    *string
	OwnerMembershipID       *string
	OpeningBalanceMinor     *int64
	OpeningBalanceCurrency  *string
	OpeningBalanceAsOf      *time.Time
	CountTowardNetWorth     *bool
	VisibleToLimitedMembers *bool
}

// AccountDeps mirrors HouseholdDeps: every port AccountService needs, gathered
// into one named argument.
type AccountDeps struct {
	Accounts   AccountRepository
	Households HouseholdRepository
	FX         FXRateProvider
	Clock      Clock
	// Holdings answers "does this account still hold anything", which is
	// what blocks a type change out from under live holdings. Required, not
	// optional: a nil here would silently disable that guard.
	Holdings HoldingCounter
}

// AccountService covers the Finances screen: accounts and the net worth
// summary computed from them. No actor parameter: services enforce what is
// *valid*; the router enforces who is *asking*, via the money capability
// and owner check.
type AccountService struct {
	d AccountDeps
}

func NewAccountService(d AccountDeps) *AccountService {
	return &AccountService{d: d}
}

func (s *AccountService) List(ctx context.Context, householdID string, includeArchived bool) ([]AccountView, error) {
	return s.d.Accounts.List(ctx, householdID, includeArchived)
}

func (s *AccountService) Get(ctx context.Context, householdID, accountID string) (AccountView, error) {
	return s.d.Accounts.Get(ctx, householdID, accountID)
}

// Create adds an account. today is the household's calendar day, passed in by
// the caller: an opening balance is a recorded fact and may not be dated
// after it (refuseFutureOpeningBalance).
func (s *AccountService) Create(ctx context.Context, in NewAccount, today time.Time) (domain.Account, error) {
	account := domain.Account{
		HouseholdID:             in.HouseholdID,
		Nickname:                in.Nickname,
		OwnerMembershipID:       in.OwnerMembershipID,
		OpeningBalanceAsOf:      in.OpeningBalanceAsOf,
		CountTowardNetWorth:     in.CountTowardNetWorth,
		VisibleToLimitedMembers: in.VisibleToLimitedMembers,
		OpeningBalance:          domain.Money{Amount: in.OpeningBalanceMinor, Currency: in.OpeningBalanceCurrency},
		Type:                    domain.AccountType(in.Type),
	}
	if err := s.validate(ctx, &account); err != nil {
		return domain.Account{}, err
	}
	if err := refuseFutureOpeningBalance(account.OpeningBalanceAsOf, today); err != nil {
		return domain.Account{}, err
	}
	return s.d.Accounts.Create(ctx, account)
}

// Update merges the patch onto the stored account, then validates the
// *result*, never the fields alone: type "loan" plus a negative balance is
// illegal together though legal separately, so validating fields alone
// would let that pair through.
//
// The opening date is the one rule checked only when the patch changes it.
// A stored date can be after today without anyone having typed a future
// date: the household's zone was moved west after the row was written. A
// rename of that account must not be refused over a date nobody touched.
func (s *AccountService) Update(ctx context.Context, householdID, accountID string, patch AccountUpdate, today time.Time) (domain.Account, error) {
	view, err := s.d.Accounts.Get(ctx, householdID, accountID)
	if err != nil {
		return domain.Account{}, err
	}
	account := view.Account

	if patch.Nickname != nil {
		account.Nickname = *patch.Nickname
	}
	if patch.Type != nil {
		// HoldingService anchors a holding to an investment account on create;
		// this guards the reverse, blocking a type change that would strand
		// live holdings under a type that never accepts them. Only a CHANGE
		// is refused -- renaming, or re-setting the same type, is fine.
		if domain.AccountType(*patch.Type) != account.Type {
			held, err := s.d.Holdings.CountLiveForAccount(ctx, householdID, accountID)
			if err != nil {
				return domain.Account{}, err
			}
			if held > 0 {
				return domain.Account{}, domain.ErrAccountHasHoldings
			}
		}
		account.Type = domain.AccountType(*patch.Type)
	}
	if patch.OwnerMembershipID != nil {
		account.OwnerMembershipID = *patch.OwnerMembershipID
	}
	if patch.OpeningBalanceMinor != nil {
		account.OpeningBalance.Amount = *patch.OpeningBalanceMinor
	}
	if patch.OpeningBalanceCurrency != nil {
		account.OpeningBalance.Currency = *patch.OpeningBalanceCurrency
	}
	dateChanged := false
	if patch.OpeningBalanceAsOf != nil {
		dateChanged = !sameDay(*patch.OpeningBalanceAsOf, account.OpeningBalanceAsOf)
		account.OpeningBalanceAsOf = *patch.OpeningBalanceAsOf
	}
	if patch.CountTowardNetWorth != nil {
		account.CountTowardNetWorth = *patch.CountTowardNetWorth
	}
	if patch.VisibleToLimitedMembers != nil {
		account.VisibleToLimitedMembers = *patch.VisibleToLimitedMembers
	}

	if err := s.validate(ctx, &account); err != nil {
		return domain.Account{}, err
	}
	if dateChanged {
		if err := refuseFutureOpeningBalance(account.OpeningBalanceAsOf, today); err != nil {
			return domain.Account{}, err
		}
	}
	return s.d.Accounts.Update(ctx, account)
}

func (s *AccountService) SetArchived(ctx context.Context, householdID, accountID string, archived bool) (domain.Account, error) {
	return s.d.Accounts.SetArchived(ctx, householdID, accountID, archived, s.d.Clock.Now())
}

// validate normalises and checks an assembled account in place, shared by
// Create and Update so the two rules cannot drift -- a defect class this
// project has hit four times: a rule fixed at one call site, not its sibling.
func (s *AccountService) validate(ctx context.Context, a *domain.Account) error {
	a.Nickname = strings.TrimSpace(a.Nickname)
	if a.Nickname == "" {
		return domain.ErrAccountNicknameRequired
	}

	accountType, err := domain.ParseAccountType(string(a.Type))
	if err != nil {
		return err
	}
	a.Type = accountType

	// ParseSelectableCurrency, not ParseCurrency: Money.String hard-codes two
	// decimal places, so a JPY or KWD account would render every amount
	// wrong. The identical gate a household's primary currency goes through.
	code, err := domain.ParseSelectableCurrency(a.OpeningBalance.Currency)
	if err != nil {
		return err
	}
	a.OpeningBalance.Currency = code

	if a.Type.IsLiability() && a.OpeningBalance.Amount < 0 {
		return domain.ErrLiabilityBalanceNegative
	}
	// The opening balance is the first term of the account's balance sum, and
	// an asset's may be negative, so the limit applies on both sides of zero.
	if err := domain.CheckAmountWithinLimit(a.OpeningBalance.Amount); err != nil {
		return err
	}

	if a.OwnerMembershipID != "" {
		ok, err := s.d.Accounts.MembershipBelongsToHousehold(ctx, a.HouseholdID, a.OwnerMembershipID)
		if err != nil {
			return err
		}
		if !ok {
			return domain.ErrAccountOwnerNotInHousehold
		}
	}
	return nil
}

// refuseFutureOpeningBalance refuses an opening balance dated after the
// household's today. today is the household's calendar day
// (domain.TodayIn), so today itself is always allowed and there is no
// tolerance past it: the server knows which day it is for this household.
func refuseFutureOpeningBalance(asOf, today time.Time) error {
	if domain.IsAfterDay(asOf, today) {
		return domain.ErrOpeningBalanceInFuture
	}
	return nil
}
