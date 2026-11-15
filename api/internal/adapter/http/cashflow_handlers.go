package httpadapter

import (
	"net/http"
	"strconv"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// cashflowDayLayout is the wire form of a calendar day.
const cashflowDayLayout = "2006-01-02"

type cashflowReasonDTO struct {
	Code              string `json:"code"`
	KeptBasisPoints   *int64 `json:"keptBasisPoints"`
	CushionTenths     *int64 `json:"cushionTenths"`
	BillName          string `json:"billName"`
	BillUnconvertible bool   `json:"billUnconvertible"`
}

type cashflowLightDTO struct {
	Colour  string              `json:"colour"`
	Reasons []cashflowReasonDTO `json:"reasons"`
}

// cashflowAverageDTO holds per-month figures, each rounded once from the sums.
type cashflowAverageDTO struct {
	Months          int    `json:"months"`
	InMinor         int64  `json:"inMinor"`
	OutMinor        int64  `json:"outMinor"`
	LeftMinor       int64  `json:"leftMinor"`
	KeptBasisPoints *int64 `json:"keptBasisPoints"`
}

type cashflowCushionDTO struct {
	Known  bool   `json:"known"`
	Tenths *int64 `json:"tenths"`
	Reason string `json:"reason"`
}

type cashflowConfirmationDTO struct {
	ConfirmedAt     time.Time `json:"confirmedAt"`
	ConfirmedByName string    `json:"confirmedByName"`
}

type cashflowContributionDTO struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	AmountMinor int64  `json:"amountMinor"`
	PaidOn      string `json:"paidOn"`
}

// cashflowSumLineDTO is one step from the ledger's spent figure to money out.
type cashflowSumLineDTO struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	AmountMinor int64  `json:"amountMinor"`
	PaidOn      string `json:"paidOn"`
	ShareIndex  int    `json:"shareIndex"`
	Spread      int    `json:"spread"`
}

// cashflowReconciliationDTO is a month's worked sum. Lines is empty unless
// Status is "shown": a sum that does not add up is hidden, never sent wrong.
type cashflowReconciliationDTO struct {
	Status             string               `json:"status"`
	SpentMinor         int64                `json:"spentMinor"`
	Lines              []cashflowSumLineDTO `json:"lines"`
	UnratedCount       int                  `json:"unratedCount"`
	UnratedEarlierBill bool                 `json:"unratedEarlierBill"`
	UnratedCurrencies  []string             `json:"unratedCurrencies"`
}

type cashflowMonthDTO struct {
	Month                   string                    `json:"month"`
	Status                  string                    `json:"status"`
	InMinor                 int64                     `json:"inMinor"`
	OutMinor                int64                     `json:"outMinor"`
	LeftMinor               int64                     `json:"leftMinor"`
	KeptBasisPoints         *int64                    `json:"keptBasisPoints"`
	SpreadMinor             int64                     `json:"spreadMinor"`
	LoanMinor               int64                     `json:"loanMinor"`
	TransactionCount        int                       `json:"transactionCount"`
	Grey                    []string                  `json:"grey"`
	UnconvertibleCurrencies []string                  `json:"unconvertibleCurrencies"`
	ProjectedLeftMinor      *int64                    `json:"projectedLeftMinor"`
	Confirmation            *cashflowConfirmationDTO  `json:"confirmation"`
	Contributions           []cashflowContributionDTO `json:"contributions"`
	// Reconciliation is null for a month with no figures ("future", "none").
	Reconciliation *cashflowReconciliationDTO `json:"reconciliation"`
}

type cashflowBillToComeDTO struct {
	BillID        string `json:"billId"`
	Name          string `json:"name"`
	AmountMinor   int64  `json:"amountMinor"`
	NextDue       string `json:"nextDue"`
	Unconvertible bool   `json:"unconvertible"`
	Overdue       bool   `json:"overdue"`
}

type cashflowYearDTO struct {
	Year                    int                     `json:"year"`
	Months                  []cashflowMonthDTO      `json:"months"`
	KeptSoFarMinor          int64                   `json:"keptSoFarMinor"`
	IncomeMinor             int64                   `json:"incomeMinor"`
	GreyMonths              int                     `json:"greyMonths"`
	KeptBasisPoints         *int64                  `json:"keptBasisPoints"`
	SameMonthsLastYearMinor *int64                  `json:"sameMonthsLastYearMinor"`
	HeadingForMinor         *int64                  `json:"headingForMinor"`
	BillsToCome             []cashflowBillToComeDTO `json:"billsToCome"`
}

type cashflowPageResponse struct {
	Currency        string             `json:"currency"`
	HasTransactions bool               `json:"hasTransactions"`
	OldestYear      int                `json:"oldestYear"`
	Light           cashflowLightDTO   `json:"light"`
	Average         cashflowAverageDTO `json:"average"`
	Cushion         cashflowCushionDTO `json:"cushion"`
	LastFinished    *cashflowMonthDTO  `json:"lastFinished"`
	Current         cashflowMonthDTO   `json:"current"`
	Recent          []cashflowMonthDTO `json:"recent"`
	Year            cashflowYearDTO    `json:"year"`
}

type cashflowConfirmResponse struct {
	Month           string    `json:"month"`
	ConfirmedAt     time.Time `json:"confirmedAt"`
	ConfirmedByName string    `json:"confirmedByName"`
}

// handleGetCashflow serves the whole Cashflow page. A missing ?year= means the
// current year (year 0 to the service); one that is not a whole number is
// refused here, and one outside the household's history by the service.
func handleGetCashflow(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		year := 0
		if raw := r.URL.Query().Get("year"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				WriteError(w, http.StatusBadRequest, "INVALID_YEAR", "That year could not be read. Use a four-digit year.", nil)
				return
			}
			year = parsed
		}

		page, err := deps.Cashflow.Page(r.Context(), scope.HouseholdID, scope.Today, year)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, toCashflowPageResponse(page))
	}
}

// handleConfirmCashflowMonth marks a finished month "looks right, count it".
// The confirmer is always the caller's own membership, never a request value.
func handleConfirmCashflowMonth(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		month, ok := parseBudgetMonth(w, r)
		if !ok {
			return
		}
		c, err := deps.Cashflow.Confirm(r.Context(), scope.HouseholdID, month, scope.Membership.ID, scope.Today)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, cashflowConfirmResponse{
			Month: c.Month.Format(monthLayout), ConfirmedAt: c.ConfirmedAt, ConfirmedByName: c.ConfirmedByName,
		})
	}
}

// handleUnconfirmCashflowMonth undoes a confirmation. Undoing one that does
// not exist is still a success.
func handleUnconfirmCashflowMonth(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		month, ok := parseBudgetMonth(w, r)
		if !ok {
			return
		}
		if err := deps.Cashflow.Unconfirm(r.Context(), scope.HouseholdID, month, scope.Today); err != nil {
			MapDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func toCashflowPageResponse(p usecase.CashflowPage) cashflowPageResponse {
	resp := cashflowPageResponse{
		Currency:        p.Currency,
		HasTransactions: p.HasTransactions,
		OldestYear:      p.OldestYear,
		Light:           toCashflowLightDTO(p.Light),
		Average:         toCashflowAverageDTO(p.Average),
		Cushion:         cashflowCushionDTO{Known: p.Cushion.Known, Tenths: p.CushionTenths, Reason: string(p.Cushion.Reason)},
		Current:         toCashflowMonthDTO(p.Current, nil, p.Confirmations),
		Recent:          make([]cashflowMonthDTO, 0, len(p.Recent)),
	}
	if p.LastFinished != nil {
		m := toCashflowMonthDTO(*p.LastFinished, nil, p.Confirmations)
		resp.LastFinished = &m
	}
	for _, m := range p.Recent {
		resp.Recent = append(resp.Recent, toCashflowMonthDTO(m, nil, p.Confirmations))
	}
	resp.Year = toCashflowYearDTO(p)
	return resp
}

func toCashflowLightDTO(v domain.CashflowVerdict) cashflowLightDTO {
	reasons := make([]cashflowReasonDTO, 0, len(v.Reasons))
	for _, r := range v.Reasons {
		reasons = append(reasons, cashflowReasonDTO{
			Code: r.Code, KeptBasisPoints: r.KeptBasisPoints, CushionTenths: r.CushionTenths,
			BillName: r.BillName, BillUnconvertible: r.BillUnconvertible,
		})
	}
	return cashflowLightDTO{Colour: string(v.Colour), Reasons: reasons}
}

func toCashflowAverageDTO(a domain.CashflowAverage) cashflowAverageDTO {
	dto := cashflowAverageDTO{
		Months:    a.Months,
		InMinor:   a.PerMonth(a.SumIn),
		OutMinor:  a.PerMonth(a.SumOut),
		LeftMinor: a.PerMonth(a.SumLeft()),
	}
	dto.KeptBasisPoints = keptBasisPointsPtr(a.SumLeft(), a.SumIn)
	return dto
}

func toCashflowYearDTO(p usecase.CashflowPage) cashflowYearDTO {
	y := p.Year
	dto := cashflowYearDTO{
		Year:                    y.Year,
		Months:                  make([]cashflowMonthDTO, 0, len(y.Months)),
		KeptSoFarMinor:          y.KeptSoFarMinor,
		IncomeMinor:             y.IncomeMinor,
		GreyMonths:              y.GreyMonths,
		KeptBasisPoints:         keptBasisPointsPtr(y.KeptSoFarMinor, y.IncomeMinor),
		SameMonthsLastYearMinor: y.SameMonthsLastYearMinor,
		HeadingForMinor:         y.HeadingForMinor,
		BillsToCome:             make([]cashflowBillToComeDTO, 0, len(p.BillsToCome)),
	}
	for _, slot := range y.Months {
		dto.Months = append(dto.Months, toCashflowMonthDTO(slot.CashflowMonth, slot.ProjectedLeftMinor, p.Confirmations))
	}
	for _, b := range p.BillsToCome {
		dto.BillsToCome = append(dto.BillsToCome, cashflowBillToComeDTO{
			BillID: b.BillID, Name: b.Name, AmountMinor: b.AmountMinor,
			NextDue: b.NextDue.Format(cashflowDayLayout), Unconvertible: b.Unconvertible,
			Overdue: b.Overdue,
		})
	}
	return dto
}

func toCashflowMonthDTO(m domain.CashflowMonth, projected *int64, confirmations map[time.Time]usecase.CashflowConfirmation) cashflowMonthDTO {
	dto := cashflowMonthDTO{
		Month:                   m.Month.Format(monthLayout),
		Status:                  string(m.Status),
		InMinor:                 m.InMinor,
		OutMinor:                m.OutMinor,
		LeftMinor:               m.LeftMinor(),
		KeptBasisPoints:         keptBasisPointsPtr(m.LeftMinor(), m.InMinor),
		SpreadMinor:             m.SpreadMinor,
		LoanMinor:               m.LoanMinor,
		TransactionCount:        m.TransactionCount,
		Grey:                    make([]string, 0, len(m.Grey)),
		UnconvertibleCurrencies: append([]string{}, m.UnconvertibleCurrencies...),
		ProjectedLeftMinor:      projected,
		Contributions:           make([]cashflowContributionDTO, 0, len(m.Contributions)),
	}
	for _, g := range m.Grey {
		dto.Grey = append(dto.Grey, string(g))
	}
	for _, c := range m.Contributions {
		dto.Contributions = append(dto.Contributions, cashflowContributionDTO{
			Kind: c.Kind, Name: c.Name, AmountMinor: c.AmountMinor, PaidOn: c.PaidOn.Format(cashflowDayLayout),
		})
	}
	if m.Status == domain.CashflowSolid || m.Status == domain.CashflowGrey || m.Status == domain.CashflowCurrent {
		dto.Reconciliation = toCashflowReconciliationDTO(domain.ReconcileCashflowMonth(m))
	}
	if c, ok := confirmations[m.Month]; ok {
		dto.Confirmation = &cashflowConfirmationDTO{ConfirmedAt: c.ConfirmedAt, ConfirmedByName: c.ConfirmedByName}
	}
	return dto
}

// toCashflowReconciliationDTO converts with string(...) because the status and
// kinds come from ReconcileCashflowMonth's own constants, not from a row or a
// request.
func toCashflowReconciliationDTO(r domain.CashflowReconciliation) *cashflowReconciliationDTO {
	dto := &cashflowReconciliationDTO{
		Status:             string(r.Status),
		SpentMinor:         r.SpentMinor,
		Lines:              make([]cashflowSumLineDTO, 0, len(r.Lines)),
		UnratedCount:       r.UnratedCount,
		UnratedEarlierBill: r.UnratedEarlierBill,
		UnratedCurrencies:  append([]string{}, r.UnratedCurrencies...),
	}
	for _, l := range r.Lines {
		dto.Lines = append(dto.Lines, cashflowSumLineDTO{
			Kind: string(l.Kind), Name: l.Name, AmountMinor: l.AmountMinor,
			PaidOn: l.PaidOn.Format(cashflowDayLayout), ShareIndex: l.ShareIndex, Spread: l.Spread,
		})
	}
	return dto
}

// keptBasisPointsPtr is the share of income kept, or nil when there is no
// income to measure it against.
func keptBasisPointsPtr(leftMinor, inMinor int64) *int64 {
	bp, ok := domain.KeptBasisPoints(leftMinor, inMinor)
	if !ok {
		return nil
	}
	return &bp
}
