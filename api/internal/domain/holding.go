package domain

import (
	"fmt"
	"sort"
	"time"
)

// InstrumentKind is what sort of thing a holding is. It decides nothing about
// the arithmetic -- gold and a share are folded identically -- and exists so a
// screen can group and label, and so "other" has a name rather than being the
// absence of one.
//
// A holding the household cannot express as a quantity at a unit price is
// recorded as "other" with a quantity of one, so that there is a single
// arithmetic path through this package rather than a second one for lump sums.
type InstrumentKind string

const (
	InstrumentStock InstrumentKind = "stock"
	InstrumentGold  InstrumentKind = "gold"
	InstrumentOther InstrumentKind = "other"
)

// ParseInstrumentKind refuses anything it does not recognise. The default is
// the point: this value arrives from a database column or a request body, so
// an unrecognised one is refused rather than carried further -- the same rule
// ParseContributionSource and ParseTransactionKind follow.
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

// HoldingEventKind is which direction a holding event moved. There are exactly
// two, and income (a dividend) is deliberately not among them: income does not
// change what is held or what it cost, so folding it here would corrupt the
// average. It arrives with the period report, on its own footing.
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
// account.
//
// Currency is the holding's own, not the household's, for the reason goals
// carry theirs (00007_goals.sql): a holding accumulates for years, and letting
// a primary-currency change restate its cost would restate every event behind
// it. A US stock inside a Singapore brokerage is the ordinary case, not the
// exotic one.
//
// Unit is what one of it is called -- "share", "gram", "unit". It is a label
// for a screen and nothing computes with it.
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

// HoldingEvent is one acquisition or one disposal.
//
// Amount is the whole event, not a unit price: what the lot cost, or what the
// sale brought in. That is the figure the owner's bank statement shows, and a
// unit price divided out of it would round before anything else got the chance.
//
// PrimaryAmount is the same event in the household's primary currency. It is
// nil when the holding is already in that currency, and required when it is
// not -- exactly the contract Transaction.ReceivedAmount carries, including the
// reason: storing it in the same-currency case invites the two figures to
// disagree later with nothing to say which is true.
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
		return fmt.Errorf("%w: an event amount cannot be negative, got %d", ErrInvalidMoney, e.Amount.Amount)
	}

	return validatePrimaryAmount(e.PrimaryAmount, holdingCurrency, primaryCurrency)
}

// validatePrimaryAmount is the cross-currency rule both a holding event and a
// valuation obey, in one place so the two cannot drift apart. It is the same
// contract Transaction.ReceivedAmount carries: the primary-currency figure is
// required exactly when it is a different number from the native one, and
// refused when it would merely duplicate it.
//
// The figure is stored rather than a rate because this product has no dated
// rate source -- and because what the owner actually knows is the amount that
// left their bank, not the ratio behind it.
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
		return fmt.Errorf("%w: a primary amount cannot be negative, got %d", ErrInvalidMoney, primary.Amount)
	}
	return nil
}

// Valuation is what one unit of a holding was worth on a given day. It is the
// other half of what a portfolio screen needs: Position says how much is held
// and what it cost, a Valuation says what it is worth now.
//
// UnitPrice is per unit, unlike HoldingEvent.Amount which is the whole event.
// A price is genuinely per-unit -- it is what the owner reads off a screen --
// whereas a lot's cost is the total that left their account.
//
// AsOf is the day the price was true, not the day it was typed. The two differ
// whenever someone backfills a quarter, and the report needs the former.
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
		return fmt.Errorf("%w: a unit price cannot be negative, got %d", ErrInvalidMoney, v.UnitPrice.Amount)
	}
	return validatePrimaryAmount(v.PrimaryUnitPrice, holdingCurrency, primaryCurrency)
}

// MarketValue is what held is worth at this valuation's price. It is a thin
// wrapper over Quantity.Value and deliberately adds no arithmetic of its own --
// there is one multiply in this package and this is not a second one.
func (v Valuation) MarketValue(held Quantity) (Money, error) {
	return held.Value(v.UnitPrice)
}

// PrimaryMarketValue is the same figure in the household's own currency, from
// the price the owner recorded in that currency -- never from the native price
// with a rate applied, because this product has no dated rate source.
//
// A nil PrimaryUnitPrice means the holding is already in the household's
// currency, the contract validatePrimaryAmount enforces on the way in, so the
// native price is the primary one.
func (v Valuation) PrimaryMarketValue(held Quantity) (Money, error) {
	if v.PrimaryUnitPrice != nil {
		return held.Value(*v.PrimaryUnitPrice)
	}
	return held.Value(v.UnitPrice)
}

// Position is what a holding's events add up to: how much is still held, what
// that remainder cost, and what selling has already realised. Each of those
// costs is carried TWICE -- once in the holding's own currency, once in the
// household's -- because the second cannot be derived from the first
// afterwards. Two lots bought at the same USD price under different exchange
// rates blend to an SGD cost per unit that is neither rate, and no single rate
// applied to the USD figure reproduces it.
//
// Cost is the cost of what is STILL held, not of everything ever bought -- the
// part belonging to sold units has already moved into Realised. That is what
// makes Cost divided by Held the average cost at any moment. CostPrimary and
// RealisedPrimary say the same thing about the household's own money, which is
// the figure that answers "did this make us richer".
type Position struct {
	Held     Quantity
	Cost     Money
	Realised Money

	CostPrimary     Money
	RealisedPrimary Money
}

// costPool is one currency's side of the fold. The fold keeps two and runs
// identical arithmetic on each, so this is a type rather than the same eight
// lines written out twice -- the disposal branch in particular is where the
// average-cost basis lives, and two copies of it is two places to drift.
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

// dispose moves the share of the pool belonging to the units leaving it out of
// cost and into realised. part and whole are the quantity sold and the
// quantity held BEFORE the sale, so the average cost of what remains is
// unchanged -- the asymmetry average-cost basis depends on.
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
// separately recorded one when the holding is in some other currency, and the
// native amount itself when it is not.
//
// A nil PrimaryAmount means "this holding is already in the household's
// currency" -- the contract validatePrimaryAmount enforces on the way in. If a
// row ever reaches here with a nil primary amount and a native currency that
// is NOT the household's, the Add in the pool refuses it rather than silently
// treating dollars as Singapore dollars.
func (e HoldingEvent) inPrimary() Money {
	if e.PrimaryAmount != nil {
		return *e.PrimaryAmount
	}
	return e.Amount
}

// Position folds the events into what they add up to, on the average-cost basis
// the PRD pins (not FIFO: "the first gram" is not a thing that exists).
//
// primaryCurrency is the household's own, and is what the second pool is
// denominated in. It is a parameter rather than a field on Holding because a
// household's primary currency can change while a holding's cannot -- the
// figures are recomputed under the new one, not restated.
//
// It sorts by date itself rather than trusting the caller. A repository
// returning rows in insertion order, or a caller appending a backdated
// correction, would otherwise produce a different answer for the same holding,
// and the difference would be silent.
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
