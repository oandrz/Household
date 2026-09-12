package usecase

import (
	"context"
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
func (s *HoldingService) RecordEvent(ctx context.Context, e domain.HoldingEvent) (domain.HoldingEvent, error) {
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

	// Re-fold with the new event applied rather than trusting a running total
	// held somewhere else. Overselling is caught here, at the point of
	// recording, because a ledger that cannot be folded is a screen that
	// cannot render -- and the household would only discover it on the next
	// page load.
	existing, err := s.d.Events.ListByHolding(ctx, e.HouseholdID, e.HoldingID)
	if err != nil {
		return domain.HoldingEvent{}, err
	}
	if _, err := holding.Position(append(existing, e)); err != nil {
		return domain.HoldingEvent{}, err
	}
	return s.d.Events.Insert(ctx, e)
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
	existing, err := s.d.Events.ListByHolding(ctx, householdID, holdingID)
	if err != nil {
		return err
	}
	remaining := make([]domain.HoldingEvent, 0, len(existing))
	found := false
	for _, e := range existing {
		if e.ID == eventID {
			found = true
			continue
		}
		remaining = append(remaining, e)
	}
	if !found {
		return domain.ErrNotFound
	}
	if _, err := holding.Position(remaining); err != nil {
		return err
	}
	return s.d.Events.Delete(ctx, householdID, eventID)
}

func (s *HoldingService) RecordValuation(ctx context.Context, v domain.Valuation) (domain.Valuation, error) {
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
		position, err := rec.Holding.Position(byHolding[rec.Holding.ID])
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
