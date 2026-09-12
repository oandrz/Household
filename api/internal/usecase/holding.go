package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// HoldingPositionView is one row of the portfolio screen: the holding, what
// its events fold to, and what it is worth if anyone has said.
//
// HasMarketValue is the point of this struct. A holding nobody has priced has
// NO market value -- not a market value of zero, which on screen reads as
// "this is worthless" rather than "nobody has said what it is worth". The
// caller renders the absence, and producing that absence is this layer's job,
// not the handler's. It is the same "blank the figure and say why" rule the
// net worth card already follows when a primary-currency change strands an
// account.
//
// ValuedAt is when the price it used was true, so a screen can show how stale
// the figure is. Valuations going quietly stale is this feature's largest
// product risk, so the age travels with the number rather than being
// available on request.
type HoldingPositionView struct {
	Holding        domain.Holding
	AccountName    string
	Position       domain.Position
	MarketValue    domain.Money
	HasMarketValue bool
	ValuedAt       time.Time

	// PrimaryMarketValue is the same figure in the household's own currency,
	// present only when the holding is NOT already in it. The PRD's rule: a US
	// stock up 5% in USD while SGD gained 6% against USD made the household
	// poorer, and the primary figure is the one that says so -- while the
	// native figure beside it still says whether the pick was good.
	//
	// It comes from the valuation's own primary unit price, which the owner
	// supplied, never from a rate: this product has no dated rate source.
	// HasPrimaryMarketValue false means "this holding is already in your
	// currency", not "we could not work it out".
	PrimaryMarketValue    domain.Money
	HasPrimaryMarketValue bool
}

// PortfolioView is the whole portfolio screen in one response.
//
// It carries no household total, deliberately. A total would have to convert
// every holding into the primary currency, and this product has no dated rate
// source -- adding one is a decision, not a detail. The per-holding figures
// are honest without it.
type PortfolioView struct {
	Holdings []HoldingPositionView
}

// HoldingDeps is what HoldingService needs and nothing more. Accounts is the
// narrow AccountLookup rather than the whole AccountRepository: this service
// reads an account's type and never writes one.
type HoldingDeps struct {
	Holdings   HoldingRepository
	Events     HoldingEventRepository
	Valuations HoldingValuationRepository
	Income     HoldingIncomeRepository
	Accounts   AccountLookup
	Households HouseholdRepository
}

// HoldingService composes the portfolio screen and every write against it.
// Like every other service here it takes no actor parameter: services enforce
// what is *valid*, the channel's inbound edge enforces who is *asking*
// (ADR 8). The money capability and the owner check live in the router.
type HoldingService struct {
	d HoldingDeps
}

func NewHoldingService(d HoldingDeps) *HoldingService { return &HoldingService{d: d} }

func (s *HoldingService) Create(ctx context.Context, h domain.Holding) (domain.Holding, error) {
	h.Name = strings.TrimSpace(h.Name)
	if h.Name == "" {
		return domain.Holding{}, domain.ErrHoldingNameRequired
	}
	if _, err := domain.ParseInstrumentKind(string(h.Instrument)); err != nil {
		return domain.Holding{}, err
	}
	// NewMoney is the single reference for what a valid currency code is, so
	// the check goes through it rather than re-deciding here.
	if _, err := domain.NewMoney(0, h.Currency); err != nil {
		return domain.Holding{}, err
	}
	code, err := domain.ParseCurrency(h.Currency)
	if err != nil {
		return domain.Holding{}, err
	}
	h.Currency = code
	h.Unit = strings.TrimSpace(h.Unit)
	if h.Unit == "" {
		h.Unit = "unit"
	}
	if err := s.requireInvestmentAccount(ctx, h.HouseholdID, h.AccountID); err != nil {
		return domain.Holding{}, err
	}
	return s.d.Holdings.Create(ctx, h)
}

// requireInvestmentAccount fails closed on the account's type. An account in
// another household reads as ErrNotFound through AccountLookup.Get, never as
// forbidden, so nothing here leaks that it exists.
func (s *HoldingService) requireInvestmentAccount(ctx context.Context, householdID, accountID string) error {
	view, err := s.d.Accounts.Get(ctx, householdID, accountID)
	if err != nil {
		return err
	}
	if view.Account.Type != domain.AccountInvestment {
		return domain.ErrHoldingAccountNotInvestment
	}
	return nil
}

// Get is the single holding, for a caller that needs its currency before it
// can build a money value for an event or a price -- an event carries its
// holding's currency but does not state it. The service re-validates whatever
// that caller then sends, so this read builds a request rather than being
// trusted as one.
func (s *HoldingService) Get(ctx context.Context, householdID, holdingID string) (domain.Holding, error) {
	return s.d.Holdings.Get(ctx, householdID, holdingID)
}

func (s *HoldingService) List(ctx context.Context, householdID string, includeArchived bool) ([]HoldingRecord, error) {
	return s.d.Holdings.List(ctx, householdID, includeArchived)
}

func (s *HoldingService) Update(ctx context.Context, h domain.Holding) (domain.Holding, error) {
	h.Name = strings.TrimSpace(h.Name)
	if h.Name == "" {
		return domain.Holding{}, domain.ErrHoldingNameRequired
	}
	if _, err := domain.ParseInstrumentKind(string(h.Instrument)); err != nil {
		return domain.Holding{}, err
	}
	return s.d.Holdings.Update(ctx, h)
}

// SetArchived archives or restores. now is a parameter rather than read from
// the clock here, so the caller's clock port is the single source of time and
// the behaviour is deterministic in a test -- the rule GoalService follows.
func (s *HoldingService) SetArchived(ctx context.Context, householdID, holdingID string, archived bool, now time.Time) (domain.Holding, error) {
	var stamp *time.Time
	if archived {
		stamp = &now
	}
	return s.d.Holdings.SetArchived(ctx, householdID, holdingID, stamp)
}

// RecordEvent validates an acquisition or disposal against its holding and
// the household's primary currency, then refuses it if it would leave the
// position oversold.
// RecordEvent takes today as a parameter rather than reading a clock here, so
// the behaviour is deterministic in a test and the caller's clock port stays
// the single source of time -- the rule SetArchived and GoalService follow.
func (s *HoldingService) RecordEvent(ctx context.Context, e domain.HoldingEvent, today time.Time) (domain.HoldingEvent, error) {
	if err := refuseFutureDate(e.OccurredOn, today); err != nil {
		return domain.HoldingEvent{}, err
	}
	holding, err := s.d.Holdings.Get(ctx, e.HouseholdID, e.HoldingID)
	if err != nil {
		return domain.HoldingEvent{}, err
	}
	if holding.IsArchived() {
		return domain.HoldingEvent{}, domain.ErrHoldingArchived
	}
	primaryCurrency, err := s.primaryCurrency(ctx, e.HouseholdID)
	if err != nil {
		return domain.HoldingEvent{}, err
	}
	if err := e.Validate(holding.Currency, primaryCurrency); err != nil {
		return domain.HoldingEvent{}, err
	}

	// Overselling is caught at the point of recording, because a ledger that
	// cannot be folded is a screen that cannot render, and the household would
	// only discover it on the next page load.
	//
	// The fold runs INSIDE the write's own transaction, not as a separate read
	// before it: two sales of 30 from a holding of 50 are each legal alone and
	// illegal together, and checking then writing lets both through. The rule
	// stays here; InsertWithFold supplies the lock.
	return s.d.Events.InsertWithFold(ctx, e, func(withThisOne []domain.HoldingEvent) error {
		_, err := holding.Position(withThisOne, primaryCurrency)
		return err
	})
}

// DeleteEvent refuses a delete that would leave the REMAINING events unable to
// fold -- removing a purchase a later sale depended on. The alternative is a
// holding whose page throws every time it loads, which the household cannot
// fix without the very screen that is broken.
func (s *HoldingService) DeleteEvent(ctx context.Context, householdID, holdingID, eventID string) error {
	holding, err := s.d.Holdings.Get(ctx, householdID, holdingID)
	if err != nil {
		return err
	}
	// The fold needs the household's currency for its primary pool even here,
	// where only the error is read: a pool has to know what it is denominated
	// in before it can refuse an amount in something else.
	primaryCurrency, err := s.primaryCurrency(ctx, householdID)
	if err != nil {
		return err
	}
	// Same reasoning as RecordEvent: the remainder is folded inside the
	// delete's own transaction, so a concurrent write cannot slip between the
	// check and the removal.
	return s.d.Events.DeleteWithFold(ctx, householdID, holdingID, eventID, func(remaining []domain.HoldingEvent) error {
		_, err := holding.Position(remaining, primaryCurrency)
		return err
	})
}

func (s *HoldingService) RecordValuation(ctx context.Context, v domain.Valuation, today time.Time) (domain.Valuation, error) {
	if err := refuseFutureDate(v.AsOf, today); err != nil {
		return domain.Valuation{}, err
	}
	holding, err := s.d.Holdings.Get(ctx, v.HouseholdID, v.HoldingID)
	if err != nil {
		return domain.Valuation{}, err
	}
	if holding.IsArchived() {
		return domain.Valuation{}, domain.ErrHoldingArchived
	}
	primaryCurrency, err := s.primaryCurrency(ctx, v.HouseholdID)
	if err != nil {
		return domain.Valuation{}, err
	}
	if err := v.Validate(holding.Currency, primaryCurrency); err != nil {
		return domain.Valuation{}, err
	}
	return s.d.Valuations.Upsert(ctx, v)
}

func (s *HoldingService) ListEvents(ctx context.Context, householdID, holdingID string) ([]domain.HoldingEvent, error) {
	if _, err := s.d.Holdings.Get(ctx, householdID, holdingID); err != nil {
		return nil, err
	}
	return s.d.Events.ListByHolding(ctx, householdID, holdingID)
}

func (s *HoldingService) ListValuations(ctx context.Context, householdID, holdingID string) ([]domain.Valuation, error) {
	if _, err := s.d.Holdings.Get(ctx, householdID, holdingID); err != nil {
		return nil, err
	}
	return s.d.Valuations.ListByHolding(ctx, householdID, holdingID)
}

// Portfolio composes the whole screen from three reads -- the holdings, every
// event, every latest price -- rather than one query per holding, then folds
// each position in memory. The events arrive already ordered by the
// repository's own contract (occurred_on, created_at, id), which is what makes
// the fold's answer deterministic for same-day events.
func (s *HoldingService) Portfolio(ctx context.Context, householdID string, includeArchived bool) (PortfolioView, error) {
	records, err := s.d.Holdings.List(ctx, householdID, includeArchived)
	if err != nil {
		return PortfolioView{}, err
	}
	events, err := s.d.Events.ListByHousehold(ctx, householdID)
	if err != nil {
		return PortfolioView{}, err
	}
	latest, err := s.d.Valuations.ListLatest(ctx, householdID)
	if err != nil {
		return PortfolioView{}, err
	}
	// Looked up once for the whole screen rather than per holding: it is the
	// same answer for every row, and the fold's primary pool needs it.
	primaryCurrency, err := s.primaryCurrency(ctx, householdID)
	if err != nil {
		return PortfolioView{}, err
	}

	// Grouping preserves the order the repository returned, because that order
	// is the fold's tie-break for events sharing a date.
	byHolding := make(map[string][]domain.HoldingEvent, len(records))
	for _, e := range events {
		byHolding[e.HoldingID] = append(byHolding[e.HoldingID], e)
	}
	priceOf := make(map[string]domain.Valuation, len(latest))
	for _, v := range latest {
		priceOf[v.HoldingID] = v
	}

	out := make([]HoldingPositionView, 0, len(records))
	for _, rec := range records {
		position, err := rec.Holding.Position(byHolding[rec.Holding.ID], primaryCurrency)
		if err != nil {
			return PortfolioView{}, err
		}
		view := HoldingPositionView{
			Holding:     rec.Holding,
			AccountName: rec.AccountName,
			Position:    position,
		}
		// No price recorded means no market value -- not a zero one. The
		// caller shows the reason instead of a figure.
		if price, ok := priceOf[rec.Holding.ID]; ok {
			value, err := price.MarketValue(position.Held)
			if err != nil {
				return PortfolioView{}, err
			}
			view.MarketValue, view.HasMarketValue, view.ValuedAt = value, true, price.AsOf
			if price.PrimaryUnitPrice != nil {
				primary, err := price.PrimaryUnitPrice.Mul(position.Held)
				if err != nil {
					return PortfolioView{}, err
				}
				view.PrimaryMarketValue, view.HasPrimaryMarketValue = primary, true
			}
		}
		out = append(out, view)
	}
	return PortfolioView{Holdings: out}, nil
}

func (s *HoldingService) primaryCurrency(ctx context.Context, householdID string) (string, error) {
	household, err := s.d.Households.Get(ctx, householdID)
	if err != nil {
		return "", err
	}
	return household.PrimaryCurrency, nil
}

// refuseFutureDate compares CALENDAR DAYS, not instants. A household recording
// this morning's purchase must not be refused because the clock reads a later
// hour, and this project has already shipped that off-by-one three times in
// its date handling.
func refuseFutureDate(date, today time.Time) error {
	d := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	t := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	if d.After(t) {
		return fmt.Errorf("%w: %s", domain.ErrHoldingDateInFuture, d.Format(time.DateOnly))
	}
	return nil
}

// maxReportPeriods is how far back the report will go in one response.
//
// It is a drawing limit, not a storage one. The chart puts one bar per holding
// inside each period, so twelve quarters against four holdings is already
// forty-eight bars in 320 pixels -- past the point where a bar is a bar. A
// household wanting more history wants a different screen, not a wider one.
const maxReportPeriods = 12

// HoldingReportRow is one holding's whole row in the report: the holding
// itself, and what it earned in each period.
//
// Returns is index-aligned with PortfolioReportView.Periods. A chart reads the
// two together by position rather than matching on labels, which is also what
// stops a holding with a gap in its history silently shifting its own bars.
type HoldingReportRow struct {
	Holding     domain.Holding
	AccountName string
	Returns     []domain.PeriodReturn
}

// PortfolioReportView is the whole report screen in one response.
//
// It carries no household total, for the reason PortfolioView carries none:
// a total would have to convert every holding into the primary currency, and
// this product has no dated rate source. The per-holding primary figures are
// each recorded by the owner; a sum across them would be, too -- but a sum
// that blanks whenever any one holding blanks is worse than no sum at all,
// because it reads as a zero in the season somebody forgot to type a price.
type PortfolioReportView struct {
	Periods         []domain.Period
	PrimaryCurrency string
	Holdings        []HoldingReportRow
}

// RecordIncome stores one dividend, coupon or charge. It takes today for the
// same reason RecordEvent does -- the caller's clock port stays the single
// source of time.
func (s *HoldingService) RecordIncome(ctx context.Context, i domain.HoldingIncome, today time.Time) (domain.HoldingIncome, error) {
	if err := refuseFutureDate(i.ReceivedOn, today); err != nil {
		return domain.HoldingIncome{}, err
	}
	holding, err := s.d.Holdings.Get(ctx, i.HouseholdID, i.HoldingID)
	if err != nil {
		return domain.HoldingIncome{}, err
	}
	if holding.IsArchived() {
		return domain.HoldingIncome{}, domain.ErrHoldingArchived
	}
	primaryCurrency, err := s.primaryCurrency(ctx, i.HouseholdID)
	if err != nil {
		return domain.HoldingIncome{}, err
	}
	if err := i.Validate(holding.Currency, primaryCurrency); err != nil {
		return domain.HoldingIncome{}, err
	}
	// No fold, no lock, no transaction: income enters no pool, so no invariant
	// spans two rows here and there is nothing a concurrent write could
	// invalidate. Contrast RecordEvent, which folds inside its own write.
	return s.d.Income.Insert(ctx, i)
}

func (s *HoldingService) ListIncome(ctx context.Context, householdID, holdingID string) ([]domain.HoldingIncome, error) {
	if _, err := s.d.Holdings.Get(ctx, householdID, holdingID); err != nil {
		return nil, err
	}
	return s.d.Income.ListByHolding(ctx, householdID, holdingID)
}

// DeleteIncome needs no fold check, unlike DeleteEvent: removing a dividend
// cannot leave the remaining rows unable to fold, because they never folded.
func (s *HoldingService) DeleteIncome(ctx context.Context, householdID, holdingID, incomeID string) error {
	if _, err := s.d.Holdings.Get(ctx, householdID, holdingID); err != nil {
		return err
	}
	return s.d.Income.Delete(ctx, householdID, incomeID)
}

// Report is the period screen: what each holding earned over each of the last
// `count` periods of this kind, ending with the one the household is currently
// living in.
//
// It reads the household's WHOLE history once -- every holding, every event,
// every income row, every price -- and computes in memory, rather than issuing
// a query per holding per period.
//
// The work is bigger than "one fold per holding", so here is its real shape.
// Each period needs the position at BOTH of its ends, and each of those is
// folded from the beginning of the holding's life, because average cost
// depends on everything before the window. That is `count x 2` folds per
// holding, each of them O(n log n) in that holding's events: twelve quarters
// is twenty-four full folds. Position also copies and re-sorts the slice every
// time, which the repository's ordering already guarantees -- so at this scale
// the sort is 24x redundant work that is nonetheless kept, because it is the
// fold's own defence against a caller that did not order, and correctness that
// depends on a caller's diligence is not correctness.
//
// At a household's scale (single-digit holdings, tens of events) all of that
// is a handful of rows and microseconds, and it is the simplest thing that is
// correct. It stops being free at maybe a thousand events across dozens of
// holdings, where the answer is a query that folds in SQL -- not a cache: two
// paths computing the same figure is how the report and the portfolio screen
// would start disagreeing.
//
// Archived holdings are included. Selling out of something and tidying it away
// does not unmake the profit it realised that quarter, and dropping it would
// silently change a closed period's answer.
func (s *HoldingService) Report(ctx context.Context, householdID string, kind domain.PeriodKind, count int, today time.Time) (PortfolioReportView, error) {
	if _, err := domain.ParsePeriodKind(string(kind)); err != nil {
		return PortfolioReportView{}, err
	}
	if count < 1 || count > maxReportPeriods {
		return PortfolioReportView{}, fmt.Errorf("%w: %d, the report draws at most %d",
			domain.ErrPeriodCountOutOfRange, count, maxReportPeriods)
	}
	periods, err := domain.PeriodsEndingOn(kind, today, count)
	if err != nil {
		return PortfolioReportView{}, err
	}

	records, err := s.d.Holdings.List(ctx, householdID, true)
	if err != nil {
		return PortfolioReportView{}, err
	}
	events, err := s.d.Events.ListByHousehold(ctx, householdID)
	if err != nil {
		return PortfolioReportView{}, err
	}
	income, err := s.d.Income.ListByHousehold(ctx, householdID)
	if err != nil {
		return PortfolioReportView{}, err
	}
	prices, err := s.d.Valuations.ListForHousehold(ctx, householdID)
	if err != nil {
		return PortfolioReportView{}, err
	}
	primaryCurrency, err := s.primaryCurrency(ctx, householdID)
	if err != nil {
		return PortfolioReportView{}, err
	}

	// Grouping is this function's real job, and getting it wrong would blend
	// two positions into one answer that looks plausible. Each map preserves
	// the order its read returned, which for events is the fold's tie-break
	// for rows sharing a date.
	eventsOf := make(map[string][]domain.HoldingEvent, len(records))
	for _, e := range events {
		eventsOf[e.HoldingID] = append(eventsOf[e.HoldingID], e)
	}
	incomeOf := make(map[string][]domain.HoldingIncome, len(records))
	for _, i := range income {
		incomeOf[i.HoldingID] = append(incomeOf[i.HoldingID], i)
	}
	pricesOf := make(map[string][]domain.Valuation, len(records))
	for _, p := range prices {
		pricesOf[p.HoldingID] = append(pricesOf[p.HoldingID], p)
	}

	out := PortfolioReportView{
		Periods:         periods,
		PrimaryCurrency: primaryCurrency,
		Holdings:        make([]HoldingReportRow, 0, len(records)),
	}
	for _, rec := range records {
		id := rec.Holding.ID
		returns := make([]domain.PeriodReturn, 0, len(periods))
		for _, period := range periods {
			r, err := rec.Holding.ReturnOver(period, eventsOf[id], incomeOf[id], pricesOf[id], primaryCurrency)
			if err != nil {
				return PortfolioReportView{}, err
			}
			returns = append(returns, r)
		}
		out.Holdings = append(out.Holdings, HoldingReportRow{
			Holding:     rec.Holding,
			AccountName: rec.AccountName,
			Returns:     returns,
		})
	}
	return out, nil
}
