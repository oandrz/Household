package domain

import (
	"fmt"
	"sort"
	"time"
)

// InstrumentKind labels a holding for display only; it decides no arithmetic,
// since gold and a share fold identically. A holding that can't be priced as a
// quantity at a unit price is recorded as "other" at a quantity of one,
// keeping a single arithmetic path rather than a second for lump sums.
type InstrumentKind string

const (
	InstrumentStock InstrumentKind = "stock"
	InstrumentGold  InstrumentKind = "gold"
	InstrumentOther InstrumentKind = "other"
)

// ParseInstrumentKind refuses anything it does not recognise -- a value from a
// database column or a request body is refused, not carried further, the same
// rule ParseContributionSource and ParseTransactionKind follow.
func ParseInstrumentKind(s string) (InstrumentKind, error) {
	switch InstrumentKind(s) {
	case InstrumentStock:
		return InstrumentStock, nil
	case InstrumentGold:
		return InstrumentGold, nil
	case InstrumentOther:
		return InstrumentOther, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownInstrumentKind, s)
	}
}

// HoldingEventKind is which direction a holding event moved (exactly two
// values). Income (a dividend) is deliberately not among them: folding it in
// would corrupt the average, so it arrives separately with the period report.
type HoldingEventKind string

const (
	HoldingAcquisition HoldingEventKind = "acquisition"
	HoldingDisposal    HoldingEventKind = "disposal"
)

func ParseHoldingEventKind(s string) (HoldingEventKind, error) {
	switch HoldingEventKind(s) {
	case HoldingAcquisition:
		return HoldingAcquisition, nil
	case HoldingDisposal:
		return HoldingDisposal, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownHoldingEventKind, s)
	}
}

// Holding is one thing a household owns some of, inside one investment
// account. Currency is the holding's own, not the household's, the same reason
// goals carry theirs: a primary-currency change restating a holding's cost
// would restate every event behind it, and a US stock in a Singapore brokerage
// is the ordinary case. Unit is a display label only ("share", "gram", "unit")
// that nothing computes with.
type Holding struct {
	ID          string
	HouseholdID string
	AccountID   string
	Name        string
	Instrument  InstrumentKind
	Unit        string
	Currency    string
	ArchivedAt  *time.Time
}

func (h Holding) IsArchived() bool { return h.ArchivedAt != nil }

// HoldingEvent is one acquisition or one disposal. Amount is the whole event,
// not a unit price -- the figure on the owner's bank statement; dividing a
// unit price out of it would round early. PrimaryAmount is the same event in
// the household's currency: nil when the holding is already in that currency,
// required otherwise, so the two figures can't disagree -- the same contract
// Transaction.ReceivedAmount carries.
type HoldingEvent struct {
	ID            string
	HoldingID     string
	HouseholdID   string
	Kind          HoldingEventKind
	Quantity      Quantity
	Amount        Money
	PrimaryAmount *Money
	OccurredOn    time.Time
	Note          string
}

// Validate checks the event against the currency of the holding it belongs to
// and the household's primary currency. It is the whole cross-currency rule in
// one place, so no service has to reinvent it.
func (e HoldingEvent) Validate(holdingCurrency, primaryCurrency string) error {
	if _, err := ParseHoldingEventKind(string(e.Kind)); err != nil {
		return err
	}
	// Zero is a legal Quantity -- a holding sold down to nothing -- but not a
	// legal event: nothing changed hands, so there is nothing to record.
	if e.Quantity.Nano() <= 0 {
		return fmt.Errorf("%w: %d", ErrHoldingEventQuantityNotPositive, e.Quantity.Nano())
	}
	if e.Amount.Currency != holdingCurrency {
		return fmt.Errorf("%w: event is %s, holding is %s", ErrCurrencyMismatch, e.Amount.Currency, holdingCurrency)
	}
	if e.Amount.Amount < 0 {
		return fmt.Errorf("%w: an event amount cannot be negative, got %d", ErrNegativeAmount, e.Amount.Amount)
	}

	return validatePrimaryAmount(e.PrimaryAmount, holdingCurrency, primaryCurrency)
}

// validatePrimaryAmount is the cross-currency rule an event and a valuation
// both obey, one place so they can't drift -- required when the primary figure
// differs from the native one, refused when it would just duplicate it
// (Transaction.ReceivedAmount's contract). Stored as a figure, not a rate: no
// dated rate source exists, and the owner knows the amount that left their
// bank, not the ratio.
func validatePrimaryAmount(primary *Money, holdingCurrency, primaryCurrency string) error {
	if holdingCurrency == primaryCurrency {
		if primary != nil {
			return ErrHoldingPrimaryAmountNotAllowed
		}
		return nil
	}
	if primary == nil {
		return ErrHoldingPrimaryAmountRequired
	}
	if primary.Currency != primaryCurrency {
		return fmt.Errorf("%w: primary amount is %s, primary currency is %s",
			ErrCurrencyMismatch, primary.Currency, primaryCurrency)
	}
	if primary.Amount < 0 {
		return fmt.Errorf("%w: a primary amount cannot be negative, got %d", ErrNegativeAmount, primary.Amount)
	}
	return nil
}

// Valuation is what one unit of a holding was worth on a given day -- the
// other half a portfolio screen needs alongside Position. UnitPrice is per
// unit, unlike HoldingEvent.Amount which is the whole event: it's what the
// owner reads off a screen, not the account total. AsOf is the day the price
// was true, not typed -- they differ when someone backfills a quarter, and the
// report needs the former.
type Valuation struct {
	ID               string
	HoldingID        string
	HouseholdID      string
	UnitPrice        Money
	PrimaryUnitPrice *Money
	AsOf             time.Time
	Note             string
}

// Validate applies the same cross-currency rule a holding event obeys.
func (v Valuation) Validate(holdingCurrency, primaryCurrency string) error {
	if v.UnitPrice.Currency != holdingCurrency {
		return fmt.Errorf("%w: valuation is %s, holding is %s", ErrCurrencyMismatch, v.UnitPrice.Currency, holdingCurrency)
	}
	if v.UnitPrice.Amount < 0 {
		return fmt.Errorf("%w: a unit price cannot be negative, got %d", ErrNegativeAmount, v.UnitPrice.Amount)
	}
	return validatePrimaryAmount(v.PrimaryUnitPrice, holdingCurrency, primaryCurrency)
}

// MarketValue is what held is worth at this valuation's price. It is a thin
// wrapper over Quantity.Value and deliberately adds no arithmetic of its own --
// there is one multiply in this package and this is not a second one.
func (v Valuation) MarketValue(held Quantity) (Money, error) {
	return held.Value(v.UnitPrice)
}

// PrimaryMarketValue is the household-currency figure the owner actually
// recorded, never the native price with a rate applied -- this product has no
// dated rate source. A nil PrimaryUnitPrice means the holding is already in
// the household's currency (validatePrimaryAmount's contract), so the native
// price serves as the primary one.
func (v Valuation) PrimaryMarketValue(held Quantity) (Money, error) {
	if v.PrimaryUnitPrice != nil {
		return held.Value(*v.PrimaryUnitPrice)
	}
	return held.Value(v.UnitPrice)
}

// Position is what a holding's events add up to: how much is still held, what
// that cost, and what's already been realised by selling. Cost is carried
// TWICE -- native and household currency -- since the second can't be derived
// from the first afterwards (two lots at the same USD price under different
// exchange rates blend to an SGD cost per unit matching neither rate). Cost is
// only what is STILL held; the sold share has moved into Realised, so
// Cost/Held is the average cost at any moment. CostPrimary and RealisedPrimary
// say the same thing in the household's own money -- the figure answering "did
// this make us richer".
type Position struct {
	Held     Quantity
	Cost     Money
	Realised Money

	CostPrimary     Money
	RealisedPrimary Money
}

// costPool is one currency's side of the fold, kept as a type rather than
// duplicated eight-line arithmetic -- the disposal branch holds the
// average-cost basis, and two copies would be two places to drift.
type costPool struct {
	cost     Money
	realised Money
}

func (p costPool) acquire(amount Money) (costPool, error) {
	next, err := p.cost.Add(amount)
	if err != nil {
		return costPool{}, err
	}
	p.cost = next
	return p, nil
}

// dispose moves the sold units' share of the pool from cost into realised.
// part and whole are the quantity sold and held BEFORE the sale, so what
// remains keeps the same average cost -- the asymmetry average-cost basis
// depends on.
func (p costPool) dispose(proceeds Money, part, whole Quantity) (costPool, error) {
	costOut, err := p.cost.Prorate(part, whole)
	if err != nil {
		return costPool{}, err
	}
	negated := Money{Amount: -costOut.Amount, Currency: costOut.Currency}
	gain, err := proceeds.Add(negated)
	if err != nil {
		return costPool{}, err
	}
	realised, err := p.realised.Add(gain)
	if err != nil {
		return costPool{}, err
	}
	cost, err := p.cost.Add(negated)
	if err != nil {
		return costPool{}, err
	}
	return costPool{cost: cost, realised: realised}, nil
}

// inPrimary is the event's amount in the household's own currency: the
// recorded one when the holding is in another currency, the native amount
// otherwise. A nil PrimaryAmount means the holding is already in the
// household's currency (validatePrimaryAmount's contract) -- a row reaching
// here with a nil primary amount and a different native currency is refused by
// the pool's Add, not silently treated as the household's own.
func (e HoldingEvent) inPrimary() Money {
	if e.PrimaryAmount != nil {
		return *e.PrimaryAmount
	}
	return e.Amount
}

// Position folds the events into what they add up to, on the average-cost
// basis the PRD pins (not FIFO -- "the first gram" isn't a thing).
// primaryCurrency is a parameter, not a field on Holding, because a
// household's currency can change while a holding's cannot, so figures are
// recomputed under the new one, not restated. It sorts by date itself rather
// than trusting the caller: rows in insertion order, or a caller appending a
// backdated correction, would otherwise give a different, silently wrong
// answer for the same holding.
func (h Holding) Position(events []HoldingEvent, primaryCurrency string) (Position, error) {
	held, err := NewQuantity(0)
	if err != nil {
		return Position{}, err
	}
	native := costPool{
		cost:     Money{Amount: 0, Currency: h.Currency},
		realised: Money{Amount: 0, Currency: h.Currency},
	}
	primary := costPool{
		cost:     Money{Amount: 0, Currency: primaryCurrency},
		realised: Money{Amount: 0, Currency: primaryCurrency},
	}

	// Copy before sorting: the caller's slice is theirs, and reordering it
	// under them is the kind of surprise that shows up three files away.
	ordered := make([]HoldingEvent, len(events))
	copy(ordered, events)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].OccurredOn.Before(ordered[j].OccurredOn)
	})

	for _, e := range ordered {
		switch e.Kind {
		case HoldingAcquisition:
			nextHeld, err := NewQuantity(held.Nano() + e.Quantity.Nano())
			if err != nil {
				return Position{}, err
			}
			if native, err = native.acquire(e.Amount); err != nil {
				return Position{}, err
			}
			if primary, err = primary.acquire(e.inPrimary()); err != nil {
				return Position{}, err
			}
			held = nextHeld

		case HoldingDisposal:
			if e.Quantity.Nano() > held.Nano() {
				return Position{}, fmt.Errorf("%w: %d of %d on %s",
					ErrHoldingOversold, e.Quantity.Nano(), held.Nano(), e.OccurredOn.Format(time.DateOnly))
			}
			// Both pools are prorated against the holding as it stands at
			// THIS event, which is why the fold has to be ordered.
			if native, err = native.dispose(e.Amount, e.Quantity, held); err != nil {
				return Position{}, err
			}
			if primary, err = primary.dispose(e.inPrimary(), e.Quantity, held); err != nil {
				return Position{}, err
			}
			// Subtracting through NewQuantity rather than raw int64 is what
			// makes a negative holding unrepresentable rather than merely
			// unlikely.
			nextHeld, err := NewQuantity(held.Nano() - e.Quantity.Nano())
			if err != nil {
				return Position{}, err
			}
			held = nextHeld

		default:
			// A kind that is neither reaches here only from a row this
			// package did not construct. Refuse it rather than silently
			// folding nothing.
			return Position{}, fmt.Errorf("%w: %q", ErrUnknownHoldingEventKind, e.Kind)
		}
	}

	return Position{
		Held:            held,
		Cost:            native.cost,
		Realised:        native.realised,
		CostPrimary:     primary.cost,
		RealisedPrimary: primary.realised,
	}, nil
}
