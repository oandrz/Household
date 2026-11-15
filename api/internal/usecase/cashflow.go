package usecase

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// CashflowDeps are the ports CashflowService reads and writes.
type CashflowDeps struct {
	Households    HouseholdRepository // for the primary currency
	Rows          CashflowRowReader
	Confirmations CashflowConfirmationRepository
	Accounts      CashflowAccounts
	Bills         CashflowBills
	FX            FXRateProvider
}

// CashflowService builds the Money > Cashflow page. It takes no actor: the
// router decides who may ask (ADR 8).
type CashflowService struct {
	d CashflowDeps
}

func NewCashflowService(d CashflowDeps) *CashflowService {
	return &CashflowService{d: d}
}

// CashflowBillToCome is a live quarterly or yearly bill still to be paid this
// year, overdue ones included. Unconvertible means the amount has no rate and
// AmountMinor is 0. Overdue uses domain.IsOverdue, the Bills page's rule, so
// the two pages never disagree about the same bill.
type CashflowBillToCome struct {
	BillID        string
	Name          string
	AmountMinor   int64
	NextDue       time.Time
	Unconvertible bool
	Overdue       bool
}

// CashflowPage is everything the page shows, in the household's primary
// currency.
type CashflowPage struct {
	Currency        string
	HasTransactions bool
	OldestYear      int // 0 when HasTransactions is false; never before cashflowFirstYear otherwise
	Light           domain.CashflowVerdict
	Average         domain.CashflowAverage
	Cushion         domain.CashCushion
	CushionTenths   *int64
	LastFinished    *domain.CashflowMonth // nil when history started this month
	Current         domain.CashflowMonth
	Recent          [12]domain.CashflowMonth // ending at Current; padded with CashflowNone
	Year            domain.CashflowYear
	BillsToCome     []CashflowBillToCome // only when Year.Year is the current year
	Confirmations   map[time.Time]CashflowConfirmation
}

const (
	// cashflowDueSoonDays is the window for the cash-timing warning about a
	// quarterly or yearly bill.
	cashflowDueSoonDays = 60
	// cashflowFirstYear is the earliest year the page will show or confirm.
	cashflowFirstYear = 2000
)

// Page reads the household's whole history once and works everything out from
// it. year 0 means the current year. today is the household's calendar day.
//
// An amount with no exchange rate greys its month instead of vanishing from
// it. Any other conversion failure fails the whole read: a smaller total that
// looks correct is the failure the owner fears most.
func (s *CashflowService) Page(ctx context.Context, householdID string, today time.Time, year int) (CashflowPage, error) {
	household, err := s.d.Households.Get(ctx, householdID)
	if err != nil {
		return CashflowPage{}, err
	}
	primary := household.PrimaryCurrency
	rows, err := s.d.Rows.Rows(ctx, householdID)
	if err != nil {
		return CashflowPage{}, err
	}

	current := monthStart(today)
	first := current
	oldestYear := 0
	if len(rows) > 0 {
		// Rows come oldest first. A row dated after today can't be the start.
		if oldest := monthStart(rows[0].OccurredOn); oldest.Before(first) {
			first = oldest
		}
		// The page never shows a year before cashflowFirstYear, so one
		// mistyped older date must not offer one in the year picker.
		oldestYear = max(first.Year(), cashflowFirstYear)
	}
	if year == 0 {
		year = current.Year()
	}
	if !cashflowYearAllowed(year, oldestYear, current.Year()) {
		return CashflowPage{}, domain.ErrCashflowYearOutOfRange
	}

	conv := NewConverter(s.d.FX, primary)
	items, err := cashflowItems(ctx, conv, rows)
	if err != nil {
		return CashflowPage{}, err
	}
	months, err := domain.BuildCashflowMonths(items, first, current)
	if err != nil {
		return CashflowPage{}, err
	}

	confirmations, err := s.d.Confirmations.List(ctx, householdID)
	if err != nil {
		return CashflowPage{}, err
	}
	confirmedBy := make(map[time.Time]CashflowConfirmation, len(confirmations))
	confirmedMonths := make(map[time.Time]bool, len(confirmations))
	for _, c := range confirmations {
		// MarkCashflowGrey looks keys up with ==, so every key is normalised.
		key := monthStart(c.Month)
		confirmedBy[key] = c
		confirmedMonths[key] = true
	}
	domain.MarkCashflowGrey(months, confirmedMonths)

	avg := domain.CashflowAverageOf(months, current)
	cushion, tenths, err := s.cushion(ctx, householdID, conv, avg)
	if err != nil {
		return CashflowPage{}, err
	}
	bills, err := s.liveSpreadBills(ctx, householdID, conv, today)
	if err != nil {
		return CashflowPage{}, err
	}

	var last domain.CashflowMonth
	hasFinished := len(months) >= 2
	page := CashflowPage{
		Currency:        primary,
		HasTransactions: len(rows) > 0,
		OldestYear:      oldestYear,
		Average:         avg,
		Cushion:         cushion,
		CushionTenths:   tenths,
		Current:         months[len(months)-1],
		Recent:          recentMonths(months, current),
		Year:            domain.SummariseCashflowYear(months, year, current, avg),
		Confirmations:   confirmedBy,
	}
	if hasFinished {
		last = months[len(months)-2]
		page.LastFinished = &last
	}
	page.Light = domain.JudgeCashflow(last, hasFinished, avg, cushion, bills.dueSoon(today))
	if year == current.Year() {
		page.BillsToCome = bills.toCome(today)
	}
	return page, nil
}

// Confirm marks a finished month "looks right, count it". month must be the
// first of a month, in 2000 or later, and before the current month. The window
// is checked before the repository is touched. confirmedByMembershipID is
// stored as given; the HTTP edge passes the caller's own membership.
func (s *CashflowService) Confirm(ctx context.Context, householdID string, month time.Time, confirmedByMembershipID string, today time.Time) (CashflowConfirmation, error) {
	if err := checkConfirmWindow(month, today); err != nil {
		return CashflowConfirmation{}, err
	}
	return s.d.Confirmations.Confirm(ctx, householdID, month, confirmedByMembershipID, today)
}

// Unconfirm undoes Confirm, under the same window.
func (s *CashflowService) Unconfirm(ctx context.Context, householdID string, month time.Time, today time.Time) error {
	if err := checkConfirmWindow(month, today); err != nil {
		return err
	}
	return s.d.Confirmations.Unconfirm(ctx, householdID, month)
}

func checkConfirmWindow(month, today time.Time) error {
	exact := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	switch {
	case !month.Equal(exact):
		return fmt.Errorf("%w: not the first of a month", domain.ErrCashflowMonthOutOfRange)
	case month.Year() < cashflowFirstYear:
		return fmt.Errorf("%w: before %d", domain.ErrCashflowMonthOutOfRange, cashflowFirstYear)
	case !month.Before(monthStart(today)):
		return fmt.Errorf("%w: not a finished month", domain.ErrCashflowMonthOutOfRange)
	}
	return nil
}

// cashflowYearAllowed accepts the years from the oldest transaction's year to
// the current one. With no transactions only the current year is shown.
func cashflowYearAllowed(year, oldestYear, currentYear int) bool {
	if year < cashflowFirstYear || year > currentYear {
		return false
	}
	return oldestYear == 0 && year == currentYear || oldestYear != 0 && year >= oldestYear
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// cashflowItems classifies every row first and converts second: a transfer
// between the household's own accounts is ignored, so a missing rate on it
// must not grey a month.
func cashflowItems(ctx context.Context, conv *Converter, rows []CashflowRow) ([]domain.CashflowItem, error) {
	items := make([]domain.CashflowItem, 0, len(rows))
	for _, row := range rows {
		role, err := domain.ClassifyCashflow(row.Kind, row.ToAccountType, row.BillCadence)
		if err != nil {
			return nil, err
		}
		item := domain.CashflowItem{OccurredOn: row.OccurredOn, Role: role}
		if role != domain.CashflowIgnore {
			inPrimary, hasRate, err := conv.TryConvert(ctx, row.Amount)
			if err != nil {
				return nil, err
			}
			if hasRate {
				item.AmountMinor = inPrimary.Amount
			} else {
				item.Unconvertible = row.Amount.Currency
			}
		}
		switch role {
		case domain.CashflowSpreadOut:
			item.Spread = domain.SpreadMonths(row.BillCadence)
			item.Name = row.BillName
		case domain.CashflowLoanOut:
			item.Name = row.ToAccountName
		}
		items = append(items, item)
	}
	return items, nil
}

// recentMonths returns the twelve months ending at current, with a
// CashflowNone placeholder for any month before the history began.
func recentMonths(months []domain.CashflowMonth, current time.Time) [12]domain.CashflowMonth {
	byMonth := make(map[time.Time]domain.CashflowMonth, len(months))
	for _, m := range months {
		byMonth[m.Month] = m
	}
	var recent [12]domain.CashflowMonth
	for i := range recent {
		key := current.AddDate(0, i-11, 0)
		if m, ok := byMonth[key]; ok {
			recent[i] = m
		} else {
			recent[i] = domain.CashflowMonth{Month: key, Status: domain.CashflowNone}
		}
	}
	return recent
}

// cushion sums the balances of live cash accounts that count toward net worth
// and divides by the 3-month average spending. It is "unknown" rather than
// zero whenever a figure is missing: Hearth doesn't know the balance, it isn't
// nothing.
func (s *CashflowService) cushion(ctx context.Context, householdID string, conv *Converter, avg domain.CashflowAverage) (domain.CashCushion, *int64, error) {
	accounts, err := s.d.Accounts.List(ctx, householdID, false)
	if err != nil {
		return domain.CashCushion{}, nil, err
	}
	var cash domain.Money
	found, convertible := false, true
	for _, view := range accounts {
		a := view.Account
		if a.IsArchived() || a.Type != domain.AccountCash || !a.CountTowardNetWorth {
			continue
		}
		found = true
		inPrimary, hasRate, err := conv.TryConvert(ctx, view.Balance)
		if err != nil {
			return domain.CashCushion{}, nil, err
		}
		if !hasRate {
			convertible = false
			continue
		}
		if cash.Currency == "" {
			cash = domain.Money{Currency: inPrimary.Currency}
		}
		if cash, err = cash.Add(inPrimary); err != nil {
			return domain.CashCushion{}, nil, err
		}
	}
	switch {
	case !found:
		return domain.CashCushion{Reason: domain.CushionNoCashAccounts}, nil, nil
	case !convertible:
		return domain.CashCushion{Reason: domain.CushionUnconvertibleAccount}, nil, nil
	}
	tenths, ok := domain.CushionTenths(cash.Amount, avg.SumOut, avg.Months)
	if !ok {
		reason := domain.CushionNoSpending
		if avg.Months == 0 {
			reason = domain.CushionNoSolidMonths
		}
		return domain.CashCushion{Reason: reason}, nil, nil
	}
	return domain.CashCushion{Known: true, CashMinor: cash.Amount}, &tenths, nil
}

// spreadBills are the live quarterly and yearly bills, with amounts in the
// primary currency.
type spreadBills []CashflowBillToCome

// liveSpreadBills reads the non-archived quarterly and yearly bills that have
// a next due date. A one-off, monthly or settled bill never appears.
func (s *CashflowService) liveSpreadBills(ctx context.Context, householdID string, conv *Converter, today time.Time) (spreadBills, error) {
	records, err := s.d.Bills.List(ctx, householdID, false)
	if err != nil {
		return nil, err
	}
	var out spreadBills
	for _, rec := range records {
		b := rec.Bill
		if b.IsArchived() || b.NextDue == nil {
			continue
		}
		if b.Cadence != domain.CadenceQuarterly && b.Cadence != domain.CadenceYearly {
			continue
		}
		bill := CashflowBillToCome{BillID: b.ID, Name: b.Name, NextDue: dayStart(*b.NextDue)}
		inPrimary, hasRate, err := conv.TryConvert(ctx, b.Amount)
		if err != nil {
			return nil, err
		}
		if hasRate {
			bill.AmountMinor = inPrimary.Amount
		} else {
			bill.Unconvertible = true
		}
		out = append(out, bill)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].NextDue.Before(out[j].NextDue) })
	return out, nil
}

// dueSoon keeps bills due within the next 60 days. An overdue bill is still
// due: the money has to exist on the day.
func (b spreadBills) dueSoon(today time.Time) []domain.BillDue {
	limit := dayStart(today).AddDate(0, 0, cashflowDueSoonDays)
	var out []domain.BillDue
	for _, bill := range b {
		if bill.NextDue.After(limit) {
			continue
		}
		out = append(out, domain.BillDue{
			Name: bill.Name, AmountMinor: bill.AmountMinor,
			Unconvertible: bill.Unconvertible, NextDue: bill.NextDue,
		})
	}
	return out
}

// toCome keeps bills due on or before 31 December of today's year, overdue
// ones included. The list is information only; it never changes a projection.
func (b spreadBills) toCome(today time.Time) []CashflowBillToCome {
	endOfYear := time.Date(today.Year(), time.December, 31, 0, 0, 0, 0, time.UTC)
	var out []CashflowBillToCome
	for _, bill := range b {
		if !bill.NextDue.After(endOfYear) {
			bill.Overdue = domain.IsOverdue(bill.NextDue, today)
			out = append(out, bill)
		}
	}
	return out
}
