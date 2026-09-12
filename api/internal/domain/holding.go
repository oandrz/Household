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

	if holdingCurrency == primaryCurrency {
		if e.PrimaryAmount != nil {
			return ErrHoldingPrimaryAmountNotAllowed
		}
		return nil
	}
	if e.PrimaryAmount == nil {
		return ErrHoldingPrimaryAmountRequired
	}
	if e.PrimaryAmount.Currency != primaryCurrency {
		return fmt.Errorf("%w: primary amount is %s, primary currency is %s",
			ErrCurrencyMismatch, e.PrimaryAmount.Currency, primaryCurrency)
	}
	if e.PrimaryAmount.Amount < 0 {
		return fmt.Errorf("%w: a primary amount cannot be negative, got %d", ErrInvalidMoney, e.PrimaryAmount.Amount)
	}
	return nil
}

// Position is what a holding's events add up to: how much is still held, what
// that remainder cost, and what selling has already realised. All three are in
// the holding's own currency.
//
// Cost is the cost of what is STILL held, not of everything ever bought -- the
// part belonging to sold units has already moved into Realised. That is what
// makes Cost divided by Held the average cost at any moment.
type Position struct {
	Held     Quantity
	Cost     Money
	Realised Money
}

// Position folds the events into what they add up to, on the average-cost basis
// the PRD pins (not FIFO: "the first gram" is not a thing that exists).
//
// It sorts by date itself rather than trusting the caller. A repository
// returning rows in insertion order, or a caller appending a backdated
// correction, would otherwise produce a different answer for the same holding,
// and the difference would be silent.
func (h Holding) Position(events []HoldingEvent) (Position, error) {
	zero := Money{Amount: 0, Currency: h.Currency}
	held, err := NewQuantity(0)
	if err != nil {
		return Position{}, err
	}
	p := Position{Held: held, Cost: zero, Realised: zero}

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
			nextHeld, err := NewQuantity(p.Held.Nano() + e.Quantity.Nano())
			if err != nil {
				return Position{}, err
			}
			nextCost, err := p.Cost.Add(e.Amount)
			if err != nil {
				return Position{}, err
			}
			p.Held, p.Cost = nextHeld, nextCost

		case HoldingDisposal:
			if e.Quantity.Nano() > p.Held.Nano() {
				return Position{}, fmt.Errorf("%w: %d of %d on %s",
					ErrHoldingOversold, e.Quantity.Nano(), p.Held.Nano(), e.OccurredOn.Format(time.DateOnly))
			}
			// The cost leaving the pool is proportional to the quantity
			// leaving it, which is precisely what keeps the average cost of
			// the remainder unchanged -- the asymmetry average cost depends
			// on. It is computed against the pool as it stands at THIS event,
			// which is why the fold has to be ordered.
			costOut, err := p.Cost.Prorate(e.Quantity, p.Held)
			if err != nil {
				return Position{}, err
			}
			gain, err := e.Amount.Add(Money{Amount: -costOut.Amount, Currency: costOut.Currency})
			if err != nil {
				return Position{}, err
			}
			realised, err := p.Realised.Add(gain)
			if err != nil {
				return Position{}, err
			}
			// Subtracting through NewQuantity rather than raw int64 is what
			// makes a negative holding unrepresentable rather than merely
			// unlikely.
			nextHeld, err := NewQuantity(p.Held.Nano() - e.Quantity.Nano())
			if err != nil {
				return Position{}, err
			}
			nextCost, err := p.Cost.Add(Money{Amount: -costOut.Amount, Currency: costOut.Currency})
			if err != nil {
				return Position{}, err
			}
			p.Held, p.Cost, p.Realised = nextHeld, nextCost, realised

		default:
			// A kind that is neither reaches here only from a row this
			// package did not construct. Refuse it rather than silently
			// folding nothing.
			return Position{}, fmt.Errorf("%w: %q", ErrUnknownHoldingEventKind, e.Kind)
		}
	}
	return p, nil
}
