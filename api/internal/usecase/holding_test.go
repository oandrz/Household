package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// HoldingService enforces what is VALID, never who is asking: the money
// capability and the owner check live at the router, per ADR 8. Everything
// below is a validity rule.

func holdingDay(day int) time.Time {
	return time.Date(2026, 3, day, 0, 0, 0, 0, time.UTC)
}

func money(t *testing.T, minor int64, currency string) domain.Money {
	t.Helper()
	m, err := domain.NewMoney(minor, currency)
	if err != nil {
		t.Fatalf("NewMoney(%d, %s): %v", minor, currency, err)
	}
	return m
}

func qty(t *testing.T, units int64) domain.Quantity {
	t.Helper()
	q, err := domain.NewQuantity(units * domain.QuantityScale)
	if err != nil {
		t.Fatalf("NewQuantity: %v", err)
	}
	return q
}

// holdingFixture wires the service to in-memory doubles and returns the ids a
// test needs. Every service here is testable this way because it depends on
// interfaces it declares -- the dependency-inversion rule in CLAUDE.md.
type holdingFixture struct {
	svc         *usecase.HoldingService
	holdings    *holdingRepoDouble
	events      *holdingEventRepoDouble
	valuations  *valuationRepoDouble
	income      *holdingIncomeRepoDouble
	accounts    *accountLookupDouble
	householdID string
	accountID   string
}

func newHoldingFixture(t *testing.T, primaryCurrency string) *holdingFixture {
	t.Helper()
	households := newHouseholdDouble()
	households.put(domain.Household{ID: "hh", PrimaryCurrency: primaryCurrency})

	accounts := newAccountLookupDouble()
	accounts.put("acct", domain.AccountInvestment)

	f := &holdingFixture{
		holdings:    newHoldingRepoDouble(),
		events:      newHoldingEventRepoDouble(),
		valuations:  newValuationRepoDouble(),
		income:      newHoldingIncomeRepoDouble(),
		accounts:    accounts,
		householdID: "hh",
		accountID:   "acct",
	}
	f.svc = usecase.NewHoldingService(usecase.HoldingDeps{
		Holdings:   f.holdings,
		Events:     f.events,
		Valuations: f.valuations,
		Income:     f.income,
		Accounts:   accounts,
		Households: households,
	})
	return f
}

func (f *holdingFixture) create(t *testing.T, name, currency string) domain.Holding {
	t.Helper()
	h, err := f.svc.Create(context.Background(), domain.Holding{
		HouseholdID: f.householdID, AccountID: f.accountID, Name: name,
		Instrument: domain.InstrumentStock, Unit: "share", Currency: currency,
	})
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	return h
}

func (f *holdingFixture) buy(t *testing.T, holdingID string, when time.Time, qty, costMinor int64) {
	t.Helper()
	f.event(t, holdingID, domain.HoldingAcquisition, when, qty, costMinor)
}

func (f *holdingFixture) sell(t *testing.T, holdingID string, when time.Time, qty, proceedsMinor int64) {
	t.Helper()
	f.event(t, holdingID, domain.HoldingDisposal, when, qty, proceedsMinor)
}

func (f *holdingFixture) event(t *testing.T, holdingID string, kind domain.HoldingEventKind, when time.Time, qty, minor int64) {
	t.Helper()
	quantity, err := domain.NewQuantity(qty * domain.QuantityScale)
	if err != nil {
		t.Fatalf("NewQuantity: %v", err)
	}
	amount, err := domain.NewMoney(minor, "SGD")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	if _, err := f.svc.RecordEvent(context.Background(), domain.HoldingEvent{
		HouseholdID: f.householdID, HoldingID: holdingID, Kind: kind,
		Quantity: quantity, Amount: amount, OccurredOn: when,
	}, reportToday); err != nil {
		t.Fatalf("RecordEvent %s: %v", kind, err)
	}
}

func (f *holdingFixture) price(t *testing.T, holdingID string, when time.Time, unitPriceMinor int64) {
	t.Helper()
	price, err := domain.NewMoney(unitPriceMinor, "SGD")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	if _, err := f.svc.RecordValuation(context.Background(), domain.Valuation{
		HouseholdID: f.householdID, HoldingID: holdingID, UnitPrice: price, AsOf: when,
	}, reportToday); err != nil {
		t.Fatalf("RecordValuation: %v", err)
	}
}

// A holding belongs in an INVESTMENT account. Fail closed on the type rather
// than trusting the screen to have offered only the right accounts.
func TestCreateRefusesAnAccountThatIsNotAnInvestmentAccount(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	f.accounts.put("cash", domain.AccountCash)

	_, err := f.svc.Create(context.Background(), domain.Holding{
		HouseholdID: f.householdID, AccountID: "cash", Name: "D05",
		Instrument: domain.InstrumentStock, Unit: "share", Currency: "SGD",
	})
	if !errors.Is(err, domain.ErrHoldingAccountNotInvestment) {
		t.Fatalf("error = %v, want ErrHoldingAccountNotInvestment", err)
	}
}

func TestCreateRefusesAnAccountFromAnotherHousehold(t *testing.T) {
	f := newHoldingFixture(t, "SGD")

	_, err := f.svc.Create(context.Background(), domain.Holding{
		HouseholdID: f.householdID, AccountID: "nope", Name: "D05",
		Instrument: domain.InstrumentStock, Unit: "share", Currency: "SGD",
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestCreateRefusesAnUnknownCurrency(t *testing.T) {
	f := newHoldingFixture(t, "SGD")

	_, err := f.svc.Create(context.Background(), domain.Holding{
		HouseholdID: f.householdID, AccountID: f.accountID, Name: "D05",
		Instrument: domain.InstrumentStock, Unit: "share", Currency: "ZZZ",
	})
	if !errors.Is(err, domain.ErrInvalidMoney) {
		t.Fatalf("error = %v, want ErrInvalidMoney", err)
	}
}

func TestCreateRefusesABlankName(t *testing.T) {
	f := newHoldingFixture(t, "SGD")

	_, err := f.svc.Create(context.Background(), domain.Holding{
		HouseholdID: f.householdID, AccountID: f.accountID, Name: "   ",
		Instrument: domain.InstrumentStock, Unit: "share", Currency: "SGD",
	})
	if !errors.Is(err, domain.ErrHoldingNameRequired) {
		t.Fatalf("error = %v, want ErrHoldingNameRequired", err)
	}
}

// The cross-currency rule reaches the caller through the SERVICE, not only
// through the domain: the service is what knows the household's primary
// currency, which the domain rule needs and the handler must not have to fetch.
func TestRecordEventRequiresAPrimaryAmountForAForeignCurrencyHolding(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "VOO", "USD")

	_, err := f.svc.RecordEvent(context.Background(), domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: f.householdID, Kind: domain.HoldingAcquisition,
		Quantity: qty(t, 10), Amount: money(t, 50000, "USD"), OccurredOn: holdingDay(1),
	}, holdingDay(28))
	if !errors.Is(err, domain.ErrHoldingPrimaryAmountRequired) {
		t.Fatalf("error = %v, want ErrHoldingPrimaryAmountRequired", err)
	}
}

func TestRecordEventRefusesAnAmountInTheWrongCurrency(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")

	_, err := f.svc.RecordEvent(context.Background(), domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: f.householdID, Kind: domain.HoldingAcquisition,
		Quantity: qty(t, 10), Amount: money(t, 1000, "USD"), OccurredOn: holdingDay(1),
	}, holdingDay(28))
	if !errors.Is(err, domain.ErrCurrencyMismatch) {
		t.Fatalf("error = %v, want ErrCurrencyMismatch", err)
	}
}

// An archived holding takes no new events, the same rule an archived goal
// follows: restoring it is a deliberate act, and writing to it silently
// un-retires a position the household said was finished.
func TestRecordEventRefusesAnArchivedHolding(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")
	if _, err := f.svc.SetArchived(context.Background(), f.householdID, h.ID, true, holdingDay(2)); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	_, err := f.svc.RecordEvent(context.Background(), domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: f.householdID, Kind: domain.HoldingAcquisition,
		Quantity: qty(t, 10), Amount: money(t, 1000, "SGD"), OccurredOn: holdingDay(3),
	}, holdingDay(28))
	if !errors.Is(err, domain.ErrHoldingArchived) {
		t.Fatalf("error = %v, want ErrHoldingArchived", err)
	}
}

// Selling more than is held is refused at the point of recording, not left to
// blow up the fold later. The service re-folds with the new event applied.
func TestRecordEventRefusesADisposalLargerThanThePosition(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")
	ctx := context.Background()
	if _, err := f.svc.RecordEvent(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: f.householdID, Kind: domain.HoldingAcquisition,
		Quantity: qty(t, 10), Amount: money(t, 1000, "SGD"), OccurredOn: holdingDay(1),
	}, holdingDay(28)); err != nil {
		t.Fatalf("buy: %v", err)
	}

	_, err := f.svc.RecordEvent(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: f.householdID, Kind: domain.HoldingDisposal,
		Quantity: qty(t, 11), Amount: money(t, 5000, "SGD"), OccurredOn: holdingDay(2),
	}, holdingDay(28))
	if !errors.Is(err, domain.ErrHoldingOversold) {
		t.Fatalf("error = %v, want ErrHoldingOversold", err)
	}
}

// Deleting an event that a LATER event depends on would leave a ledger that
// cannot be folded -- which is a screen that cannot render. Refuse it, rather
// than letting the page break the next time it loads.
func TestDeleteEventRefusesWhenItWouldLeaveTheRemainderOversold(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")
	ctx := context.Background()
	buy, err := f.svc.RecordEvent(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: f.householdID, Kind: domain.HoldingAcquisition,
		Quantity: qty(t, 10), Amount: money(t, 1000, "SGD"), OccurredOn: holdingDay(1),
	}, holdingDay(28))
	if err != nil {
		t.Fatalf("buy: %v", err)
	}
	if _, err := f.svc.RecordEvent(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: f.householdID, Kind: domain.HoldingDisposal,
		Quantity: qty(t, 5), Amount: money(t, 1500, "SGD"), OccurredOn: holdingDay(2),
	}, holdingDay(28)); err != nil {
		t.Fatalf("sell: %v", err)
	}

	if err := f.svc.DeleteEvent(ctx, f.householdID, h.ID, buy.ID); !errors.Is(err, domain.ErrHoldingOversold) {
		t.Fatalf("error = %v, want ErrHoldingOversold", err)
	}
}

// The portfolio view folds each holding from ONE ListByHousehold call, so the
// grouping is the thing most likely to go wrong: two holdings interleaved must
// not leak events into each other's position.
func TestPortfolioFoldsEachHoldingFromItsOwnEventsOnly(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	ctx := context.Background()
	first := f.create(t, "Alpha", "SGD")
	second := f.create(t, "Beta", "SGD")

	for _, e := range []struct {
		holding string
		day     int
		units   int64
		minor   int64
	}{
		{first.ID, 1, 10, 1000},
		{second.ID, 1, 4, 800},
		{first.ID, 2, 10, 3000},
		{second.ID, 2, 6, 1200},
	} {
		if _, err := f.svc.RecordEvent(ctx, domain.HoldingEvent{
			HoldingID: e.holding, HouseholdID: f.householdID, Kind: domain.HoldingAcquisition,
			Quantity: qty(t, e.units), Amount: money(t, e.minor, "SGD"), OccurredOn: holdingDay(e.day),
		}, holdingDay(28)); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	view, err := f.svc.Portfolio(ctx, f.householdID, false)
	if err != nil {
		t.Fatalf("Portfolio: %v", err)
	}
	if len(view.Holdings) != 2 {
		t.Fatalf("len = %d, want 2", len(view.Holdings))
	}
	byName := map[string]usecase.HoldingPositionView{}
	for _, p := range view.Holdings {
		byName[p.Holding.Name] = p
	}
	if got := byName["Alpha"]; got.Position.Held.Nano() != 20*domain.QuantityScale || got.Position.Cost.Amount != 4000 {
		t.Fatalf("Alpha = %d units costing %d, want 20 and 4000", got.Position.Held.Nano(), got.Position.Cost.Amount)
	}
	if got := byName["Beta"]; got.Position.Held.Nano() != 10*domain.QuantityScale || got.Position.Cost.Amount != 2000 {
		t.Fatalf("Beta = %d units costing %d, want 10 and 2000", got.Position.Held.Nano(), got.Position.Cost.Amount)
	}
}

// The PRD's "no figure and a reason" rule, and it is the SERVICE's job to
// produce the absence rather than the handler's. A holding nobody has priced
// has no market value -- not a market value of zero, which reads as "this is
// worthless" rather than "nobody has said".
func TestAHoldingWithNoValuationHasNoMarketValueRatherThanZero(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	ctx := context.Background()
	h := f.create(t, "D05", "SGD")
	if _, err := f.svc.RecordEvent(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: f.householdID, Kind: domain.HoldingAcquisition,
		Quantity: qty(t, 10), Amount: money(t, 1000, "SGD"), OccurredOn: holdingDay(1),
	}, holdingDay(28)); err != nil {
		t.Fatalf("buy: %v", err)
	}

	view, err := f.svc.Portfolio(ctx, f.householdID, false)
	if err != nil {
		t.Fatalf("Portfolio: %v", err)
	}
	if view.Holdings[0].HasMarketValue {
		t.Fatalf("HasMarketValue = true with no valuation recorded; MarketValue = %+v", view.Holdings[0].MarketValue)
	}
	if view.Holdings[0].MarketValue.Amount != 0 {
		t.Fatalf("MarketValue = %d, want the zero value alongside HasMarketValue false", view.Holdings[0].MarketValue.Amount)
	}
}

func TestAPricedHoldingCarriesItsMarketValueAndTheDateOfThatPrice(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	ctx := context.Background()
	h := f.create(t, "D05", "SGD")
	if _, err := f.svc.RecordEvent(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: f.householdID, Kind: domain.HoldingAcquisition,
		Quantity: qty(t, 10), Amount: money(t, 1000, "SGD"), OccurredOn: holdingDay(1),
	}, holdingDay(28)); err != nil {
		t.Fatalf("buy: %v", err)
	}
	if _, err := f.svc.RecordValuation(ctx, domain.Valuation{
		HoldingID: h.ID, HouseholdID: f.householdID,
		UnitPrice: money(t, 250, "SGD"), AsOf: holdingDay(5),
	}, holdingDay(28)); err != nil {
		t.Fatalf("RecordValuation: %v", err)
	}

	view, err := f.svc.Portfolio(ctx, f.householdID, false)
	if err != nil {
		t.Fatalf("Portfolio: %v", err)
	}
	got := view.Holdings[0]
	if !got.HasMarketValue {
		t.Fatal("HasMarketValue = false after a price was recorded")
	}
	if got.MarketValue.Amount != 2500 {
		t.Fatalf("MarketValue = %d, want 2500 (10 units at 250)", got.MarketValue.Amount)
	}
	if !got.ValuedAt.Equal(holdingDay(5)) {
		t.Fatalf("ValuedAt = %v, want %v -- the page shows how stale a price is", got.ValuedAt, holdingDay(5))
	}
}

func TestRecordValuationRefusesAPriceInTheWrongCurrency(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")

	_, err := f.svc.RecordValuation(context.Background(), domain.Valuation{
		HoldingID: h.ID, HouseholdID: f.householdID,
		UnitPrice: money(t, 250, "USD"), AsOf: holdingDay(5),
	}, holdingDay(28))
	if !errors.Is(err, domain.ErrCurrencyMismatch) {
		t.Fatalf("error = %v, want ErrCurrencyMismatch", err)
	}
}

// The PRD reports every figure in the household's primary currency with the
// instrument's own beside it: a US stock up 5% in USD while SGD gained 6%
// against USD made the household poorer, and the primary figure has to say so.
// The valuation carries both prices, so the view carries both values.
func TestAForeignHoldingCarriesItsValueInBothCurrencies(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	ctx := context.Background()
	h := f.create(t, "VOO", "USD")

	primaryCost := money(t, 67500, "SGD")
	if _, err := f.svc.RecordEvent(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: f.householdID, Kind: domain.HoldingAcquisition,
		Quantity: qty(t, 10), Amount: money(t, 50000, "USD"),
		PrimaryAmount: &primaryCost, OccurredOn: holdingDay(1),
	}, holdingDay(28)); err != nil {
		t.Fatalf("buy: %v", err)
	}
	primaryPrice := money(t, 6800, "SGD")
	if _, err := f.svc.RecordValuation(ctx, domain.Valuation{
		HoldingID: h.ID, HouseholdID: f.householdID,
		UnitPrice: money(t, 5200, "USD"), PrimaryUnitPrice: &primaryPrice, AsOf: holdingDay(5),
	}, holdingDay(28)); err != nil {
		t.Fatalf("RecordValuation: %v", err)
	}

	view, err := f.svc.Portfolio(ctx, f.householdID, false)
	if err != nil {
		t.Fatalf("Portfolio: %v", err)
	}
	got := view.Holdings[0]
	if got.MarketValue.Amount != 52000 || got.MarketValue.Currency != "USD" {
		t.Fatalf("native market value = %+v, want 52000 USD", got.MarketValue)
	}
	if !got.HasPrimaryMarketValue {
		t.Fatal("HasPrimaryMarketValue = false; a foreign holding must report its own currency AND the household's")
	}
	if got.PrimaryMarketValue.Amount != 68000 || got.PrimaryMarketValue.Currency != "SGD" {
		t.Fatalf("primary market value = %+v, want 68000 SGD", got.PrimaryMarketValue)
	}
}

// A holding already in the household's currency carries ONE figure, not two
// identical ones -- the same reason its events refuse a redundant primary
// amount.
func TestAHoldingAlreadyInPrimaryCurrencyReportsOneFigure(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	ctx := context.Background()
	h := f.create(t, "D05", "SGD")
	if _, err := f.svc.RecordEvent(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: f.householdID, Kind: domain.HoldingAcquisition,
		Quantity: qty(t, 10), Amount: money(t, 1000, "SGD"), OccurredOn: holdingDay(1),
	}, holdingDay(28)); err != nil {
		t.Fatalf("buy: %v", err)
	}
	if _, err := f.svc.RecordValuation(ctx, domain.Valuation{
		HoldingID: h.ID, HouseholdID: f.householdID,
		UnitPrice: money(t, 250, "SGD"), AsOf: holdingDay(5),
	}, holdingDay(28)); err != nil {
		t.Fatalf("RecordValuation: %v", err)
	}

	view, err := f.svc.Portfolio(ctx, f.householdID, false)
	if err != nil {
		t.Fatalf("Portfolio: %v", err)
	}
	if view.Holdings[0].HasPrimaryMarketValue {
		t.Fatal("HasPrimaryMarketValue = true for a holding already in the primary currency")
	}
}

// An archived holding still folds to its real position. Archiving is how a
// household retires a position it no longer wants on the page; it is not a
// claim that the position was always empty, and reporting held 0 / cost 0
// would be exactly that claim.
func TestAnArchivedHoldingStillReportsWhatItHeld(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	ctx := context.Background()
	h := f.create(t, "D05", "SGD")
	if _, err := f.svc.RecordEvent(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: f.householdID, Kind: domain.HoldingAcquisition,
		Quantity: qty(t, 10), Amount: money(t, 1000, "SGD"), OccurredOn: holdingDay(1),
	}, holdingDay(28)); err != nil {
		t.Fatalf("buy: %v", err)
	}
	if _, err := f.svc.SetArchived(ctx, f.householdID, h.ID, true, holdingDay(2)); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	live, err := f.svc.Portfolio(ctx, f.householdID, false)
	if err != nil {
		t.Fatalf("Portfolio(live): %v", err)
	}
	if len(live.Holdings) != 0 {
		t.Fatalf("live portfolio has %d holdings, want 0", len(live.Holdings))
	}

	all, err := f.svc.Portfolio(ctx, f.householdID, true)
	if err != nil {
		t.Fatalf("Portfolio(all): %v", err)
	}
	if len(all.Holdings) != 1 {
		t.Fatalf("portfolio with archived has %d holdings, want 1", len(all.Holdings))
	}
	if all.Holdings[0].Position.Held.Nano() != 10*domain.QuantityScale {
		t.Fatalf("archived holding reports %d units; archiving must not empty a position",
			all.Holdings[0].Position.Held.Nano())
	}
	if all.Holdings[0].Position.Cost.Amount != 1000 {
		t.Fatalf("archived holding reports cost %d, want 1000", all.Holdings[0].Position.Cost.Amount)
	}
}

// Accounts already refuse a future opening balance (ErrOpeningBalanceInFuture),
// and the reason applies here with more force: ListLatestValuations orders by
// as_of, so a price mistyped as 2030 wins forever. The holding's market value
// is then pinned to a number nobody can explain, and the "as of" label on the
// page reads a date in the future without comment.
func TestRecordValuationRefusesADateInTheFuture(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")

	_, err := f.svc.RecordValuation(context.Background(), domain.Valuation{
		HoldingID: h.ID, HouseholdID: f.householdID,
		UnitPrice: money(t, 250, "SGD"), AsOf: holdingDay(20),
	}, holdingDay(10))
	if !errors.Is(err, domain.ErrHoldingDateInFuture) {
		t.Fatalf("error = %v, want ErrHoldingDateInFuture", err)
	}
}

// Today itself is not the future. A household recording this morning's
// purchase must not be refused -- the bug this project has already shipped
// three times is an off-by-one at exactly this boundary.
func TestRecordValuationAcceptsToday(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")

	if _, err := f.svc.RecordValuation(context.Background(), domain.Valuation{
		HoldingID: h.ID, HouseholdID: f.householdID,
		UnitPrice: money(t, 250, "SGD"), AsOf: holdingDay(10),
	}, holdingDay(10)); err != nil {
		t.Fatalf("today's date was refused: %v", err)
	}
}

func TestRecordEventRefusesADateInTheFuture(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")

	_, err := f.svc.RecordEvent(context.Background(), domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: f.householdID, Kind: domain.HoldingAcquisition,
		Quantity: qty(t, 10), Amount: money(t, 1000, "SGD"), OccurredOn: holdingDay(20),
	}, holdingDay(10))
	if !errors.Is(err, domain.ErrHoldingDateInFuture) {
		t.Fatalf("error = %v, want ErrHoldingDateInFuture", err)
	}
}

// --- income, and the period report ------------------------------------------

func (f *holdingFixture) recordIncome(t *testing.T, holdingID string, kind domain.IncomeKind, minor int64, when time.Time) domain.HoldingIncome {
	t.Helper()
	amount, err := domain.NewMoney(minor, "SGD")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	in, err := f.svc.RecordIncome(context.Background(), domain.HoldingIncome{
		HouseholdID: f.householdID, HoldingID: holdingID, Kind: kind,
		Amount: amount, ReceivedOn: when,
	}, reportToday)
	if err != nil {
		t.Fatalf("RecordIncome: %v", err)
	}
	return in
}

// reportToday sits inside Q3 2026, so the current period in every test below
// is Q3 and the two before it have closed.
var reportToday = time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)

func reportDay(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 0, 0, 0, 0, time.UTC)
}

// A dividend dated tomorrow is a typo, and one dated 2030 would sit in a
// period nobody can reach. The same guard events and valuations already carry.
func TestRecordIncomeRefusesAFutureDate(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")
	amount, _ := domain.NewMoney(4500, "SGD")

	_, err := f.svc.RecordIncome(context.Background(), domain.HoldingIncome{
		HouseholdID: f.householdID, HoldingID: h.ID, Kind: domain.IncomeReceived,
		Amount: amount, ReceivedOn: reportToday.AddDate(0, 0, 1),
	}, reportToday)
	if !errors.Is(err, domain.ErrHoldingDateInFuture) {
		t.Fatalf("error = %v, want ErrHoldingDateInFuture", err)
	}
}

// The cross-currency rule is the holding's, not the row's: a USD holding in an
// SGD household needs the SGD figure beside the USD one.
func TestRecordIncomeAppliesTheHoldingsCrossCurrencyRule(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "VOO", "USD")
	usd, _ := domain.NewMoney(1200, "USD")

	_, err := f.svc.RecordIncome(context.Background(), domain.HoldingIncome{
		HouseholdID: f.householdID, HoldingID: h.ID, Kind: domain.IncomeReceived,
		Amount: usd, ReceivedOn: reportDay(time.July, 5),
	}, reportToday)
	if !errors.Is(err, domain.ErrHoldingPrimaryAmountRequired) {
		t.Fatalf("error = %v, want ErrHoldingPrimaryAmountRequired", err)
	}
}

func TestRecordIncomeRefusesAnArchivedHolding(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")
	if _, err := f.svc.SetArchived(context.Background(), f.householdID, h.ID, true, reportToday); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}
	amount, _ := domain.NewMoney(4500, "SGD")

	_, err := f.svc.RecordIncome(context.Background(), domain.HoldingIncome{
		HouseholdID: f.householdID, HoldingID: h.ID, Kind: domain.IncomeReceived,
		Amount: amount, ReceivedOn: reportDay(time.July, 5),
	}, reportToday)
	if !errors.Is(err, domain.ErrHoldingArchived) {
		t.Fatalf("error = %v, want ErrHoldingArchived", err)
	}
}

// The report is a series ending with the period the household is living in --
// the owner's first question is "how am I doing now", and a closed-periods-only
// report would be empty until a quarter ended.
func TestReportEndsWithTheCurrentPeriodAndRunsOldestFirst(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	f.create(t, "D05", "SGD")

	view, err := f.svc.Report(context.Background(), f.householdID, domain.PeriodQuarter, 3, reportToday)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	want := []string{"Q1 2026", "Q2 2026", "Q3 2026"}
	if len(view.Periods) != len(want) {
		t.Fatalf("got %d periods, want %d", len(view.Periods), len(want))
	}
	for i, label := range want {
		if view.Periods[i].Label() != label {
			t.Errorf("period %d = %q, want %q", i, view.Periods[i].Label(), label)
		}
	}
	if !view.Periods[2].IsCurrent(reportToday) || view.Periods[1].IsCurrent(reportToday) {
		t.Error("only the last period is the current one")
	}
	if view.PrimaryCurrency != "SGD" {
		t.Errorf("PrimaryCurrency = %q, want SGD", view.PrimaryCurrency)
	}
}

// Each holding's row carries one return per period, in the same order as
// view.Periods -- which is what lets a chart read the two together without
// matching on labels.
func TestReportCarriesOneReturnPerPeriodPerHolding(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")
	f.buy(t, h.ID, reportDay(time.January, 5), 10, 100000)
	f.price(t, h.ID, reportDay(time.March, 31), 10000)
	f.price(t, h.ID, reportDay(time.June, 30), 12000)
	f.recordIncome(t, h.ID, domain.IncomeReceived, 500, reportDay(time.May, 1))

	view, err := f.svc.Report(context.Background(), f.householdID, domain.PeriodQuarter, 3, reportToday)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(view.Holdings) != 1 {
		t.Fatalf("got %d holdings, want 1", len(view.Holdings))
	}
	row := view.Holdings[0]
	if len(row.Returns) != len(view.Periods) {
		t.Fatalf("got %d returns for %d periods", len(row.Returns), len(view.Periods))
	}

	q2 := row.Returns[1]
	if q2.Unrealised == nil {
		t.Fatalf("Q2 blanked with %q", q2.Reason)
	}
	if q2.Unrealised.Native.Amount != 20000 {
		t.Errorf("Q2 unrealised = %d, want 20000", q2.Unrealised.Native.Amount)
	}
	if q2.Income.Native.Amount != 500 {
		t.Errorf("Q2 income = %d, want 500", q2.Income.Native.Amount)
	}
	// Q3 has no price of its own yet, so it cannot be valued -- and says so
	// rather than reporting zero.
	if row.Returns[2].Unrealised != nil {
		t.Errorf("Q3 unrealised = %v, want blank", row.Returns[2].Unrealised)
	}
	if row.Returns[2].Reason != domain.ReasonNoClosingPrice {
		t.Errorf("Q3 reason = %q, want %q", row.Returns[2].Reason, domain.ReasonNoClosingPrice)
	}
}

// An archived holding is still in the report. Selling out of something and
// tidying it away does not unmake the profit it realised that quarter, and
// leaving it out would silently change a closed period's answer.
func TestReportIncludesArchivedHoldings(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	h := f.create(t, "D05", "SGD")
	f.buy(t, h.ID, reportDay(time.January, 5), 10, 100000)
	f.sell(t, h.ID, reportDay(time.May, 20), 10, 130000)
	if _, err := f.svc.SetArchived(context.Background(), f.householdID, h.ID, true, reportToday); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	view, err := f.svc.Report(context.Background(), f.householdID, domain.PeriodQuarter, 3, reportToday)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(view.Holdings) != 1 {
		t.Fatalf("got %d holdings, want the archived one included", len(view.Holdings))
	}
	if view.Holdings[0].Returns[1].Realised.Native.Amount != 30000 {
		t.Errorf("Q2 realised = %d, want 30000", view.Holdings[0].Returns[1].Realised.Native.Amount)
	}
}

// One holding's rows must never reach another's fold. Grouping is the service's
// job, and getting it wrong would blend two positions into one wrong answer.
func TestReportKeepsEachHoldingsRowsToItself(t *testing.T) {
	f := newHoldingFixture(t, "SGD")
	gold := f.create(t, "Gold", "SGD")
	stock := f.create(t, "D05", "SGD")
	f.buy(t, gold.ID, reportDay(time.January, 5), 10, 100000)
	f.buy(t, stock.ID, reportDay(time.January, 5), 10, 500000)
	f.price(t, gold.ID, reportDay(time.March, 31), 10000)
	f.price(t, gold.ID, reportDay(time.June, 30), 12000)
	f.price(t, stock.ID, reportDay(time.March, 31), 50000)
	f.price(t, stock.ID, reportDay(time.June, 30), 50000)
	f.recordIncome(t, gold.ID, domain.IncomeReceived, 500, reportDay(time.May, 1))

	// Three periods, so index 1 is Q2 -- the quarter these prices and this
	// dividend belong to.
	view, err := f.svc.Report(context.Background(), f.householdID, domain.PeriodQuarter, 3, reportToday)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	byName := map[string]usecase.HoldingReportRow{}
	for _, row := range view.Holdings {
		byName[row.Holding.Name] = row
	}
	if got := byName["Gold"].Returns[1]; got.Income.Native.Amount != 500 {
		t.Errorf("Gold Q2 income = %d, want 500", got.Income.Native.Amount)
	}
	if got := byName["D05"].Returns[1]; got.Income.Native.Amount != 0 {
		t.Errorf("D05 Q2 income = %d, want 0 -- that dividend was the gold's", got.Income.Native.Amount)
	}
	if got := byName["D05"].Returns[1]; got.Unrealised == nil || got.Unrealised.Native.Amount != 0 {
		t.Errorf("D05 Q2 unrealised = %v, want 0", got.Unrealised)
	}
}

func TestReportRefusesACountBeyondWhatItWillDraw(t *testing.T) {
	f := newHoldingFixture(t, "SGD")

	for _, count := range []int{0, -1, 13} {
		if _, err := f.svc.Report(context.Background(), f.householdID, domain.PeriodQuarter, count, reportToday); !errors.Is(err, domain.ErrPeriodCountOutOfRange) {
			t.Errorf("count %d error = %v, want ErrPeriodCountOutOfRange", count, err)
		}
	}
}

func TestReportRefusesAPeriodKindItDoesNotKnow(t *testing.T) {
	f := newHoldingFixture(t, "SGD")

	if _, err := f.svc.Report(context.Background(), f.householdID, domain.PeriodKind("month"), 3, reportToday); !errors.Is(err, domain.ErrUnknownPeriodKind) {
		t.Fatalf("error = %v, want ErrUnknownPeriodKind", err)
	}
}
