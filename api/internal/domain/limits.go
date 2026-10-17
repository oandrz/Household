package domain

import (
	"fmt"
	"time"
)

// The caps below bound how much one household can store. Each is about four
// to five times what a very heavy real family would reach, so no real
// household meets one; what they bound is how much any one account can put
// on the shared disk and into a single read. ADR 13 records the reasons and
// when to revisit them. Raising one is a product decision, not a tidy-up.
const (
	// MaxTransactionsPerMonth bounds every read that loads a whole month
	// (the month summary, the budget month). A very busy family is about
	// 600 a month.
	MaxTransactionsPerMonth = 3_000
	// MaxTransactions is the total. It is needed as well as the monthly cap
	// because a transaction's date has no lower bound.
	MaxTransactions = 100_000
	// MaxNamedThings is per kind of named thing. These are archived, not
	// deleted, so archived rows count: archive-and-recreate must not get
	// round the cap.
	MaxNamedThings = 200
	// MaxRecords is per kind of record: a contribution every day for fifty
	// years. Agreement proposals are never deleted, so this is their only
	// bound.
	MaxRecords = 20_000
	// MaxMembers and MaxPendingInvites are far above any household.
	MaxMembers        = 30
	MaxPendingInvites = 20
	// MaxActiveAPITokens counts tokens neither revoked nor expired. Spent
	// tokens are pruned daily, so rotating them does not pile up rows.
	MaxActiveAPITokens = 20
)

// LimitKind names one thing a household's stored amount is capped on.
type LimitKind string

const (
	LimitTransactions        LimitKind = "transactions"
	LimitTransactionsInMonth LimitKind = "transactions_in_month"
	LimitAccounts            LimitKind = "accounts"
	LimitCategories          LimitKind = "categories"
	LimitGoals               LimitKind = "goals"
	LimitBills               LimitKind = "bills"
	LimitHoldings            LimitKind = "holdings"
	LimitSpaces              LimitKind = "spaces"
	LimitAgreementSections   LimitKind = "agreement_sections"
	LimitGoalContributions   LimitKind = "goal_contributions"
	LimitBillPayments        LimitKind = "bill_payments"
	LimitHoldingEvents       LimitKind = "holding_events"
	LimitHoldingValuations   LimitKind = "holding_valuations"
	LimitHoldingIncome       LimitKind = "holding_income"
	LimitRetroActions        LimitKind = "retro_actions"
	LimitAgreementProposals  LimitKind = "agreement_proposals"
	LimitMembers             LimitKind = "members"
	LimitPendingInvites      LimitKind = "pending_invites"
	LimitActiveAPITokens     LimitKind = "active_api_tokens"
)

// CountedLimitKinds is every kind a household-wide count answers. The
// monthly transaction cap is absent: it is counted per month, not per
// household.
var CountedLimitKinds = []LimitKind{
	LimitTransactions,
	LimitAccounts, LimitCategories, LimitGoals, LimitBills, LimitHoldings,
	LimitSpaces, LimitAgreementSections,
	LimitGoalContributions, LimitBillPayments, LimitHoldingEvents,
	LimitHoldingValuations, LimitHoldingIncome, LimitRetroActions,
	LimitAgreementProposals,
	LimitMembers, LimitPendingInvites, LimitActiveAPITokens,
}

// Max is the kind's cap. An unknown kind's cap is 0, which refuses every
// write: a kind this code did not construct must never mean "unlimited".
func (k LimitKind) Max() int {
	switch k {
	case LimitTransactions:
		return MaxTransactions
	case LimitTransactionsInMonth:
		return MaxTransactionsPerMonth
	case LimitAccounts, LimitCategories, LimitGoals, LimitBills, LimitHoldings,
		LimitSpaces, LimitAgreementSections:
		return MaxNamedThings
	case LimitGoalContributions, LimitBillPayments, LimitHoldingEvents,
		LimitHoldingValuations, LimitHoldingIncome, LimitRetroActions,
		LimitAgreementProposals:
		return MaxRecords
	case LimitMembers:
		return MaxMembers
	case LimitPendingInvites:
		return MaxPendingInvites
	case LimitActiveAPITokens:
		return MaxActiveAPITokens
	default:
		return 0
	}
}

// LimitReachedError says a household already holds Limit of Kind. Month is
// set only for LimitTransactionsInMonth, so the message can name the month.
type LimitReachedError struct {
	Kind  LimitKind
	Limit int
	Month time.Time
}

func (e *LimitReachedError) Error() string {
	return fmt.Sprintf("household already holds %d %s", e.Limit, e.Kind)
}

func (e *LimitReachedError) Unwrap() error { return ErrLimitReached }

// BudgetMonthsAhead is how far past the current month a budget may be set.
const BudgetMonthsAhead = 24

// BudgetMonthAllowed reports whether a budget may be saved, or rolled over,
// for month: from January 2000 to BudgetMonthsAhead months after today's
// month. Without a window nothing bounds how many budgets a household can
// hold, since each month is its own row.
func BudgetMonthAllowed(month, today time.Time) bool {
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	earliest := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	latest := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, BudgetMonthsAhead, 0)
	return !first.Before(earliest) && !first.After(latest)
}
