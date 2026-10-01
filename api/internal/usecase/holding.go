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
// HasMarketValue is the point of this struct: a holding nobody has priced
// has NO market value, not a value of zero -- zero would read as
// "worthless" rather than "nobody has said". Producing that absence is this
// layer's job, the same "blank the figure and say why" rule the net worth
// card follows.
//
// ValuedAt is when the price it used was true, so the screen can show how
// stale the figure is -- valuations going quietly stale is this feature's
// largest product risk, so the age travels with the number.
type HoldingPositionView struct {
	Holding        domain.Holding
	AccountName    string
	Position       domain.Position
	MarketValue    domain.Money
	HasMarketValue bool
	ValuedAt       time.Time

	// PrimaryMarketValue is the same figure in the household's currency,
	// present only when the holding isn't already in it -- the PRD's rule: a
	// currency move can make the household poorer even when the pick gained,
	// and this figure is the one that says so, while the native one says
	// whether the pick was good.
	//
	// It comes from the valuation's owner-supplied primary unit price, never
	// a rate (no dated rate source). HasPrimaryMarketValue false means
	// "already in your currency," not "couldn't work it out."
	PrimaryMarketValue    domain.Money
	HasPrimaryMarketValue bool
}

// PortfolioView is the whole portfolio screen in one response.
//
// It carries no household total, deliberately: a total would have to
// convert every holding into the primary currency, and this product has no
// dated rate source -- adding one is a decision, not a detail.
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

// Get is the single holding, for a caller that needs its currency before
// building a money value for an event or price -- an event carries its
// holding's currency but doesn't state it. The service re-validates
// whatever the caller sends, so this read only builds a request.
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
// position oversold. today is the household's calendar day, passed in by
// the caller so that this service reads no clock.
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

	// Overselling is caught at recording time: an unfoldable ledger is a
	// screen that can't render. The fold runs INSIDE the write's own
	// transaction, not a separate read before it -- two sales of 30 from a
	// holding of 50 are legal alone, illegal together, and check-then-write
	// lets both through. InsertWithFold supplies the lock.
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

// Portfolio composes the whole screen from three reads -- holdings, every
// event, every latest price -- rather than one query per holding, then
// folds each position in memory. The repository's own ordering (occurred_on,
// created_at, id) is what makes the fold deterministic for same-day events.
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

// maxReportPeriods is how far back the report will go in one response.
//
// It is a drawing limit, not a storage one: twelve quarters against four
// holdings is already forty-eight bars in 320 pixels. A household wanting
// more history wants a different screen, not a wider one.
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
// It carries no household total, for the reason PortfolioView carries none
// (see its doc comment). A sum here would also blank whenever any one
// holding blanks, which is worse than no sum at all -- it would read as a
// zero in the season somebody forgot to type a price.
type PortfolioReportView struct {
	Periods         []domain.Period
	PrimaryCurrency string
	Holdings        []HoldingReportRow
}

// RecordIncome stores one dividend, coupon or charge. today is the
// household's calendar day, as for RecordEvent.
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
	// No fold, no lock, no transaction: income enters no pool, so nothing
	// here spans two rows for a concurrent write to invalidate -- contrast
	// RecordEvent, which folds inside its own write.
	return s.d.Income.Insert(ctx, i)
}

func (s *HoldingService) ListIncome(ctx context.Context, householdID, holdingID string) ([]domain.HoldingIncome, error) {
	if _, err := s.d.Holdings.Get(ctx, householdID, holdingID); err != nil {
		return nil, err
	}
	return s.d.Income.ListByHolding(ctx, householdID, holdingID)
}

// DeleteIncome needs no fold check, unlike DeleteEvent: removing a dividend
// can't leave the remaining rows unable to fold, since they never folded.
//
// The holding is read first so a row under a holding this household doesn't
// own is a 404, not a no-op delete -- and passed down too, so the
// repository's scope keeps naming one holding from removing another's row.
func (s *HoldingService) DeleteIncome(ctx context.Context, householdID, holdingID, incomeID string) error {
	if _, err := s.d.Holdings.Get(ctx, householdID, holdingID); err != nil {
		return err
	}
	return s.d.Income.Delete(ctx, householdID, holdingID, incomeID)
}

// Report is the period screen: what each holding earned over each of the last
// `count` periods of this kind, ending with the one the household is currently
// living in.
//
// It reads the household's WHOLE history once -- every holding, event,
// income row and price -- and computes in memory, rather than querying per
// holding per period.
//
// Each period needs the position at BOTH ends, each folded from the
// holding's start since average cost depends on everything before the
// window: `count x 2` folds per holding, O(n log n) each. Position also
// re-sorts every time as its own defence against an unordered caller --
// redundant here, but correctness that depends on a caller's diligence
// isn't correctness.
//
// At a household's scale (single-digit holdings, tens of events) this is
// microseconds and the simplest correct thing. It stops being free around a
// thousand events, where the fix is a query that folds in SQL -- not a
// cache, since two paths computing the same figure is how this report and
// the portfolio screen would start disagreeing.
//
// Archived holdings are included: selling out of something doesn't unmake
// the profit it realised, and dropping it would silently change a closed
// period's answer.
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

	// Grouping is this function's real job -- getting it wrong blends two
	// positions into one answer that looks plausible. Each map preserves its
	// read's order, the fold's tie-break for events sharing a date.
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
