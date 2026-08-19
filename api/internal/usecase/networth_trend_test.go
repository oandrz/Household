package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// month is the first of a month in the window fixedNow (28 July 2026) opens:
// August 2025 through July 2026, which is the design's own axis.
func month(year int, m time.Month) time.Time {
	return time.Date(year, m, 1, 0, 0, 0, 0, time.UTC)
}

// openedOn and withBalance extend networth_test.go's `account` builder. An
// account with no opening date set reads as tracked forever, which is what
// every pre-existing test wants.
func openedOn(on time.Time) func(*usecase.AccountView) {
	return func(v *usecase.AccountView) { v.Account.OpeningBalanceAsOf = on }
}

func withBalance(minor int64) func(*usecase.AccountView) {
	return func(v *usecase.AccountView) { v.Balance.Amount = minor }
}

func movement(accountID string, on time.Time, minor int64, currency string) usecase.AccountMonthMovement {
	return usecase.AccountMonthMovement{
		AccountID: accountID,
		Month:     on,
		Delta:     domain.Money{Amount: minor, Currency: currency},
	}
}

// TestTheNewestBarIsTheHeadlineFigure is this feature's discriminating test.
// The chart is the third place net worth is computed, and the one place a
// disagreement is visible to the eye: the newest bar sits directly under the
// figure it must equal.
//
// The future-dated movement is the trap. AccountView.Balance has no upper
// bound on the transaction date, so a transaction dated next month is already
// inside it. Bucket that movement into its own month and it is never
// subtracted on the way back, so every older bar is wrong by 500 -- while the
// newest bar still matches and the numbers still look reasonable.
func TestTheNewestBarIsTheHeadlineFigure(t *testing.T) {
	svc, repo := newAccountService(t)
	sgd := account(t, domain.AccountCash, 824_055, "SGD", withBalance(830_055))
	idr := account(t, domain.AccountCash, 8_540_000_000, "IDR")
	repo.addMovement(movement(sgd.Account.ID, month(2026, time.July), 6_500, "SGD"))
	repo.addMovement(movement(sgd.Account.ID, month(2026, time.August), -500, "SGD")) // dated ahead

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{sgd, idr}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Trend == nil {
		t.Fatal("Trend is nil for a computable summary")
	}
	if len(got.Trend.Points) != 12 {
		t.Fatalf("points = %d, want 12", len(got.Trend.Points))
	}

	newest := got.Trend.Points[11]
	if newest.NetWorth == nil {
		t.Fatal("the newest point has no figure")
	}
	if newest.NetWorth.Amount != got.NetWorth.Amount {
		t.Errorf("newest bar = %d, headline = %d -- these must be the same number",
			newest.NetWorth.Amount, got.NetWorth.Amount)
	}
	if !newest.Month.Equal(month(2026, time.July)) {
		t.Errorf("newest month = %s, want 2026-07", newest.Month.Format("2006-01"))
	}
	if !got.Trend.Points[0].Month.Equal(month(2025, time.August)) {
		t.Errorf("oldest month = %s, want 2025-08", got.Trend.Points[0].Month.Format("2006-01"))
	}

	// July's own movement AND the future-dated one both leave on the first
	// step backwards: 830055 - 6500 - (-500) = 824055 for the SGD account,
	// plus the IDR account's unchanged 688155.
	previous := got.Trend.Points[10]
	if previous.NetWorth == nil || previous.NetWorth.Amount != 824_055+688_155 {
		t.Errorf("June = %v, want %d -- a transaction dated next month is inside "+
			"today's balance and must come off on the first step back",
			previous.NetWorth, 824_055+688_155)
	}
}

// TestAMonthBeforeAnAccountWasTrackedIsAGap is the "zero is a claim" rule on
// this chart. A bar of zero says the household had nothing; the truth is that
// we do not know.
func TestAMonthBeforeAnAccountWasTrackedIsAGap(t *testing.T) {
	svc, _ := newAccountService(t)
	view := account(t, domain.AccountCash, 500_000, "SGD", openedOn(month(2026, time.June)))

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{view}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	for i, point := range got.Trend.Points[:10] { // Aug 2025 .. May 2026
		if point.NetWorth != nil {
			t.Fatalf("point %d (%s) = %v, want nil -- no account was tracked yet",
				i, point.Month.Format("2006-01"), point.NetWorth)
		}
		if point.Complete {
			t.Errorf("point %d is marked complete, but it has no figure at all", i)
		}
	}
	if got.Trend.Points[10].NetWorth == nil || got.Trend.Points[11].NetWorth == nil {
		t.Error("June and July have a figure and must be drawn")
	}
}

// TestAnAccountOpenedNextMonthByClockSkewIsInTheNewestBar is the tracked-from
// counterpart of TestTheNewestBarIsTheHeadlineFigure's future-dated movement.
//
// account.go:167 gives OpeningBalanceAsOf a day of slack so a household in
// UTC+8 can enter their own "today" while the server's UTC clock is still on
// the previous day (account_test.go:116-117 documents why). At a month
// boundary that slack can store an opening date in the month AFTER `current`
// -- entered "1 August" from Singapore while the server is still on 31 July
// UTC. Summary has already put this account in the headline regardless of
// that date -- it counts every non-archived, counted, convertible view -- so
// it must be in the newest bar too, the same reason deltasByAccountMonth
// clamps a future-dated movement into the current month rather than losing it
// in a month the walk never visits.
func TestAnAccountOpenedNextMonthByClockSkewIsInTheNewestBar(t *testing.T) {
	svc, _ := newAccountService(t)
	sinceTheStart := account(t, domain.AccountCash, 400_000, "SGD", openedOn(month(2025, time.August)))
	sinceTheStart.Account.ID = "since-the-start"
	openedAhead := account(t, domain.AccountCash, 200_000, "SGD", openedOn(month(2026, time.August)))
	openedAhead.Account.ID = "opened-ahead"

	got, err := svc.Summary(context.Background(), "h-1",
		[]usecase.AccountView{sinceTheStart, openedAhead}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	newest := got.Trend.Points[11]
	if newest.NetWorth == nil || newest.NetWorth.Amount != got.NetWorth.Amount {
		t.Errorf("newest bar = %v, headline = %d -- an account opened next month by clock "+
			"skew is already in the headline and must be in the newest bar too",
			newest.NetWorth, got.NetWorth.Amount)
	}
	if !newest.Complete {
		t.Error("newest bar is marked incomplete, but both accounts are already in the headline")
	}
}

// TestAMonthMissingOneAccountIsMarkedIncomplete is the middle state, and the
// reason the chart is drawable at all for a household that adds an account.
// The bar is real; it is missing an account the newest bar has, and the
// screen has to be able to say so rather than let the step up read as growth.
func TestAMonthMissingOneAccountIsMarkedIncomplete(t *testing.T) {
	svc, _ := newAccountService(t)
	old := account(t, domain.AccountCash, 100_000, "SGD", openedOn(month(2025, time.August)))
	old.Account.ID = "old"
	recent := account(t, domain.AccountInvestment, 900_000, "SGD", openedOn(month(2026, time.July)))
	recent.Account.ID = "recent"

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{old, recent}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	june := got.Trend.Points[10]
	if june.NetWorth == nil || june.NetWorth.Amount != 100_000 {
		t.Errorf("June = %v, want 100000 -- the account that existed then, and only it", june.NetWorth)
	}
	if june.Complete {
		t.Error("June is marked complete, but the investment account was not tracked yet")
	}
	if !got.Trend.Points[11].Complete {
		t.Error("July is marked incomplete, but both accounts were tracked by then")
	}
}

// TestArchivedAndUncountedAccountsAreInNoBar: whatever is out of the headline
// is out of every bar. An account excluded from the total but drawn into the
// history would make the chart's last bar the only one that agrees with it.
//
// MonthlyMovements includes archived accounts by contract (its own doc
// comment says so, deliberately -- the caller decides what counts). So both
// excluded accounts here get a movement too, dated in an older month, not
// just a balance: without one, deltasByAccountMonth's "not in counted, drop
// it" branch never runs in this test, and a regression that let an excluded
// account's movement through would go unnoticed.
func TestArchivedAndUncountedAccountsAreInNoBar(t *testing.T) {
	svc, repo := newAccountService(t)
	counted := account(t, domain.AccountCash, 100_000, "SGD")
	counted.Account.ID = "counted"
	skipped := account(t, domain.AccountCash, 900_000, "SGD", notCounted)
	skipped.Account.ID = "skipped"
	gone := account(t, domain.AccountCash, 700_000, "SGD", archived)
	gone.Account.ID = "gone"
	repo.addMovement(movement(skipped.Account.ID, month(2026, time.June), 50_000, "SGD"))
	repo.addMovement(movement(gone.Account.ID, month(2026, time.June), 30_000, "SGD"))

	got, err := svc.Summary(context.Background(), "h-1",
		[]usecase.AccountView{counted, skipped, gone}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	for _, point := range got.Trend.Points {
		if point.NetWorth == nil || point.NetWorth.Amount != 100_000 {
			t.Fatalf("%s = %v, want 100000 on every bar", point.Month.Format("2006-01"), point.NetWorth)
		}
	}
}

// TestALiabilityPullsTheBarDown: the sign comes from the account type, never
// from a number someone typed, and it has to be applied per month as well as
// once at the headline.
func TestALiabilityPullsTheBarDown(t *testing.T) {
	svc, repo := newAccountService(t)
	cash := account(t, domain.AccountCash, 500_000, "SGD")
	cash.Account.ID = "cash"
	loan := account(t, domain.AccountLoan, 200_000, "SGD", withBalance(200_000))
	loan.Account.ID = "loan"
	// A delta is a movement of the account's own BALANCE, and a liability's
	// balance is the sum owed: +50000 in July means the household borrowed
	// 50000 more that month, so June's debt was smaller and June's net worth
	// was HIGHER. The direction is the whole test -- get the sign backwards
	// and SignedNetWorthAmount can be missing entirely without anyone noticing.
	repo.addMovement(movement("loan", month(2026, time.July), 50_000, "SGD"))

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{cash, loan}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Trend.Points[11].NetWorth.Amount != 300_000 {
		t.Errorf("July = %d, want 300000 (500000 - 200000)", got.Trend.Points[11].NetWorth.Amount)
	}
	if got.Trend.Points[10].NetWorth.Amount != 350_000 {
		t.Errorf("June = %d, want 350000 (500000 - 150000) -- the debt was 50000 smaller "+
			"before July's borrowing, so net worth was higher",
			got.Trend.Points[10].NetWorth.Amount)
	}
}

// TestNoCountedAccountsMeansNoTrend: a household whose only accounts are
// excluded has a computable, genuinely zero net worth and nothing to chart.
// Nil, rather than twelve nil-valued points, so the wire carries no trend at
// all and the screen has one absence to branch on rather than two.
func TestNoCountedAccountsMeansNoTrend(t *testing.T) {
	svc, _ := newAccountService(t)
	excluded := account(t, domain.AccountCash, 900_000, "SGD", notCounted)

	got, err := svc.Summary(context.Background(), "h-1", []usecase.AccountView{excluded}, fixedNow)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.Trend != nil {
		t.Errorf("Trend = %+v, want nil for a household with no counted accounts", got.Trend)
	}
}
