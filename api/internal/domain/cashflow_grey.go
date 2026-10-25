package domain

import (
	"math/big"
	"slices"
	"time"
)

const (
	// usualWindow is how many recent solid months feed the "usual" median.
	usualWindow = 6
	// usualMinMonths is how many earlier solid months must exist before the
	// under-half-of-usual rule applies at all.
	usualMinMonths = 3
)

// MarkCashflowGrey walks the finished months from oldest to newest and
// sets Grey, Status and Confirmed. Months that are not solid on entry (the
// current month, for one) are left alone.
//
// Confirming a month ("looks right, count it") clears no_income and
// under_half_usual but never unconvertible: a missing exchange rate is a
// fact, not a judgement. A month that ends up grey does not enter later
// months' "usual"; a confirmed month that ends up solid does.
func MarkCashflowGrey(months []CashflowMonth, confirmed map[time.Time]bool) {
	var solidOuts []int64
	for i := range months {
		m := &months[i]
		if m.Status != CashflowSolid && m.Status != CashflowGrey {
			continue
		}
		judgement := judgementReasons(*m, solidOuts)
		m.Confirmed = confirmed[m.Month] && len(judgement) > 0
		m.Grey = nil
		if !m.Confirmed {
			m.Grey = append(m.Grey, judgement...)
		}
		if len(m.UnconvertibleCurrencies) > 0 {
			m.Grey = append(m.Grey, GreyUnconvertible)
		}
		m.Status = CashflowSolid
		if len(m.Grey) > 0 {
			m.Status = CashflowGrey
		} else {
			solidOuts = append(solidOuts, m.OutMinor)
		}
	}
}

// judgementReasons are the grey reasons a person may overrule: no income, and
// spending under half of usual.
func judgementReasons(m CashflowMonth, solidOuts []int64) []GreyReason {
	var reasons []GreyReason
	if m.InMinor <= 0 {
		reasons = append(reasons, GreyNoIncome)
	}
	if underHalfOfUsual(m.OutMinor, solidOuts) {
		reasons = append(reasons, GreyUnderUsual)
	}
	return reasons
}

// underHalfOfUsual reports whether out is less than half the median of the
// last usualWindow entries of solidOuts. It needs at least usualMinMonths
// entries. The comparison multiplies instead of dividing so it is exact:
// with an odd count and middle m it is 2·out < m; with an even count and
// middle pair a, b it is 4·out < a + b. big.Int keeps the products from
// overflowing on absurdly large amounts.
func underHalfOfUsual(out int64, solidOuts []int64) bool {
	if len(solidOuts) < usualMinMonths {
		return false
	}
	recent := slices.Clone(solidOuts[max(0, len(solidOuts)-usualWindow):])
	slices.Sort(recent)
	n := len(recent)
	var scaledOut, median *big.Int
	if n%2 == 1 {
		scaledOut = new(big.Int).Mul(big.NewInt(out), big.NewInt(2))
		median = big.NewInt(recent[n/2])
	} else {
		scaledOut = new(big.Int).Mul(big.NewInt(out), big.NewInt(4))
		median = new(big.Int).Add(big.NewInt(recent[n/2-1]), big.NewInt(recent[n/2]))
	}
	return scaledOut.Cmp(median) < 0
}
