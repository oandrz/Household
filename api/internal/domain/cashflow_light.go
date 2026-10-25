package domain

import (
	"math/bits"
	"time"
)

// CashflowAverage is the sum of the solid months in the three-month window.
// Grey months drop out, so Months is 0, 1, 2 or 3.
type CashflowAverage struct {
	Months int
	SumIn  int64
	SumOut int64
}

// SumLeft is what the window kept in total. Money in and out are never
// negative, so the subtraction cannot wrap.
func (a CashflowAverage) SumLeft() int64 { return a.SumIn - a.SumOut }

// PerMonth is sum divided by the months averaged, rounded half away from zero
// once from the sum. It is 0 when no month was averaged, so a caller shows an
// empty average rather than dividing by zero.
func (a CashflowAverage) PerMonth(sum int64) int64 {
	if a.Months <= 0 {
		return 0
	}
	v, _ := mulDivRoundHalfAway(sum, 1, int64(a.Months))
	return v
}

// cashflowAverageWindow is how many finished months the average looks back.
const cashflowAverageWindow = 3

// CashflowAverageOf takes the three calendar months before current. The
// current month is unfinished and never counts; a month that is not solid
// (grey, before history, missing from months) drops out.
func CashflowAverageOf(months []CashflowMonth, current time.Time) CashflowAverage {
	var avg CashflowAverage
	current = monthKey(current)
	first := current.AddDate(0, -cashflowAverageWindow, 0)
	for _, m := range months {
		if m.Status != CashflowSolid || m.Month.Before(first) || !m.Month.Before(current) {
			continue
		}
		avg.Months++
		avg.SumIn += m.InMinor
		avg.SumOut += m.OutMinor
	}
	return avg
}

// KeptBasisPoints is leftMinor as a share of inMinor in basis points
// (1,900 = 19.00%), rounded half away from zero. It is for display only:
// thresholds compare exact products instead. ok is false when there is no
// income to take a share of.
func KeptBasisPoints(leftMinor, inMinor int64) (int64, bool) {
	if inMinor <= 0 {
		return 0, false
	}
	return mulDivRoundHalfAway(leftMinor, 10_000, inMinor)
}

// CushionTenths is cash measured in tenths of a month of the usual spending:
// cash / (sumOut / n), so 42 means 4.2 months. It rounds once. ok is false
// when there is no window or no spending to measure against.
func CushionTenths(cashMinor, sumOut int64, n int) (int64, bool) {
	if n <= 0 || sumOut <= 0 {
		return 0, false
	}
	return mulDivRoundHalfAway(cashMinor, int64(n)*10, sumOut)
}

// CushionReason is why the cash cushion could not be worked out. It is empty
// when the cushion is known.
type CushionReason string

const (
	CushionNoCashAccounts       CushionReason = "no_cash_accounts"
	CushionUnconvertibleAccount CushionReason = "unconvertible_account"
	CushionNoSolidMonths        CushionReason = "no_solid_months"
	CushionNoSpending           CushionReason = "no_spending"
)

// CashCushion is the household's spendable cash. CashMinor means nothing
// unless Known is true. Known is not enough on its own: with no spending in
// the average the cushion is undefined, and JudgeCashflow treats that as
// unknown too, so the light can never be green on it.
type CashCushion struct {
	Known     bool
	CashMinor int64
	Reason    CushionReason
}

// BillDue is a quarterly or yearly bill falling due inside the warning window.
type BillDue struct {
	Name        string
	AmountMinor int64
	// Unconvertible is true when the amount has no exchange rate. The bill
	// still warns, because Hearth cannot show it is affordable.
	Unconvertible bool
	NextDue       time.Time
}

// LightColour is the traffic light.
type LightColour string

const (
	LightGrey   LightColour = "grey"
	LightRed    LightColour = "red"
	LightYellow LightColour = "yellow"
	LightGreen  LightColour = "green"
)

// Reason codes. The frontend keys its wording on these strings.
const (
	ReasonNoFinishedMonth    = "no_finished_month"
	ReasonLastMonthNotEnough = "last_month_not_enough_data"
	ReasonNoMonthsToAverage  = "no_months_to_average"
	ReasonLeftNotPositive    = "left_not_positive"
	ReasonCushionUnder1      = "cushion_under_1"
	ReasonKeptUnder10        = "kept_under_10"
	ReasonLastMonthNegative  = "last_month_negative"
	ReasonCushionUnder3      = "cushion_under_3"
	ReasonCushionUnknown     = "cushion_unknown"
	ReasonBillDueSoon        = "bill_due_soon"
	// ReasonShortAverage is yellow whenever fewer than three months were
	// averaged. The "spending under half of usual" check needs three solid
	// months before it can run, so with fewer a month whose spending was
	// never entered looks healthy. Hearth will not call that on track.
	ReasonShortAverage = "short_average"
)

// Thresholds from the spec, applied as exact products.
const (
	cashflowKeptPercentFloor    = 10 // kept under this percent is yellow
	cashflowCushionRedMonths    = 1
	cashflowCushionYellowMonths = 3
)

// LightReason is one fact behind the colour. Only the figure that belongs to
// Code is filled in.
type LightReason struct {
	Code            string
	KeptBasisPoints *int64
	CushionTenths   *int64
	BillName        string
}

// CashflowVerdict is the light and the reasons of its colour only. Green has
// none.
type CashflowVerdict struct {
	Colour  LightColour
	Reasons []LightReason
}

// JudgeCashflow runs every check and returns the worst colour with only that
// colour's reasons. When in doubt it picks the colour that does not flatter:
// missing data is grey or yellow, never green.
//
// hasFinished is false when the household's history started in the current
// month, so there is no last finished month to judge.
func JudgeCashflow(last CashflowMonth, hasFinished bool, avg CashflowAverage, c CashCushion, dueSoon []BillDue) CashflowVerdict {
	if grey := greyReasons(last, hasFinished, avg); len(grey) > 0 {
		return CashflowVerdict{Colour: LightGrey, Reasons: grey}
	}
	if red := redReasons(avg, c); len(red) > 0 {
		return CashflowVerdict{Colour: LightRed, Reasons: red}
	}
	if yellow := yellowReasons(last, avg, c, dueSoon); len(yellow) > 0 {
		return CashflowVerdict{Colour: LightYellow, Reasons: yellow}
	}
	return CashflowVerdict{Colour: LightGreen}
}

func greyReasons(last CashflowMonth, hasFinished bool, avg CashflowAverage) []LightReason {
	if !hasFinished {
		return []LightReason{{Code: ReasonNoFinishedMonth}}
	}
	var out []LightReason
	// Only a solid month can be judged; any other status is not trusted.
	if last.Status != CashflowSolid {
		out = append(out, LightReason{Code: ReasonLastMonthNotEnough})
	}
	if avg.Months == 0 {
		out = append(out, LightReason{Code: ReasonNoMonthsToAverage})
	}
	return out
}

func redReasons(avg CashflowAverage, c CashCushion) []LightReason {
	var out []LightReason
	if avg.SumIn <= avg.SumOut {
		out = append(out, LightReason{Code: ReasonLeftNotPositive})
	}
	// An unknown cushion is not a danger Hearth can see, so it is never red.
	if cushionMeasurable(c, avg) && productLess(c.CashMinor, int64(avg.Months), avg.SumOut, cashflowCushionRedMonths) {
		out = append(out, LightReason{Code: ReasonCushionUnder1, CushionTenths: cushionTenthsPtr(c, avg)})
	}
	return out
}

func yellowReasons(last CashflowMonth, avg CashflowAverage, c CashCushion, dueSoon []BillDue) []LightReason {
	var out []LightReason
	if avg.Months < cashflowAverageWindow {
		out = append(out, LightReason{Code: ReasonShortAverage})
	}
	// 10 * sumLeft < sumIn is "kept under 10%" without rounding a percentage.
	if productLess(cashflowKeptPercentFloor, avg.SumLeft(), avg.SumIn, 1) {
		r := LightReason{Code: ReasonKeptUnder10}
		if bp, ok := KeptBasisPoints(avg.SumLeft(), avg.SumIn); ok {
			r.KeptBasisPoints = &bp
		}
		out = append(out, r)
	}
	if last.LeftMinor() < 0 {
		out = append(out, LightReason{Code: ReasonLastMonthNegative})
	}
	switch {
	case !cushionMeasurable(c, avg):
		out = append(out, LightReason{Code: ReasonCushionUnknown})
	case productLess(c.CashMinor, int64(avg.Months), avg.SumOut, cashflowCushionYellowMonths):
		out = append(out, LightReason{Code: ReasonCushionUnder3, CushionTenths: cushionTenthsPtr(c, avg)})
	}
	for _, b := range dueSoon {
		// A bill is a worry when the usual monthly left-over cannot cover it:
		// amount > sumLeft / n, compared as amount * n > sumLeft.
		if b.Unconvertible || productLess(avg.SumLeft(), 1, b.AmountMinor, int64(avg.Months)) {
			out = append(out, LightReason{Code: ReasonBillDueSoon, BillName: b.Name})
		}
	}
	return out
}

// cushionMeasurable is false when the cushion is "—": the caller could not
// work it out, or there is no spending to divide by (income-only months).
// Either way the light must not be green, so both read as cushion_unknown.
func cushionMeasurable(c CashCushion, avg CashflowAverage) bool {
	return c.Known && avg.SumOut > 0
}

func cushionTenthsPtr(c CashCushion, avg CashflowAverage) *int64 {
	tenths, ok := CushionTenths(c.CashMinor, avg.SumOut, avg.Months)
	if !ok {
		return nil
	}
	return &tenths
}

// productLess reports a*b < c*d for signed values, in 128 bits, so a large
// household's figures cannot overflow a threshold check.
func productLess(a, b, c, d int64) bool {
	an, ahi, alo := signedProduct(a, b)
	cn, chi, clo := signedProduct(c, d)
	if an != cn {
		return an // a negative product is less than a non-negative one
	}
	if ahi != chi || alo != clo {
		less := ahi < chi || (ahi == chi && alo < clo)
		return less != an // for two negatives the larger magnitude is smaller
	}
	return false
}

// signedProduct returns the sign and the 128-bit magnitude of a*b. Zero is
// never negative.
func signedProduct(a, b int64) (negative bool, hi, lo uint64) {
	negative = (a < 0) != (b < 0) && a != 0 && b != 0
	hi, lo = bits.Mul64(absUint64(a), absUint64(b))
	return negative, hi, lo
}

func absUint64(v int64) uint64 {
	if v < 0 {
		return uint64(^v) + 1
	}
	return uint64(v)
}
