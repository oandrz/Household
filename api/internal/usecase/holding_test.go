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
		accounts:    accounts,
		householdID: "hh",
		accountID:   "acct",
	}
	f.svc = usecase.NewHoldingService(usecase.HoldingDeps{
		Holdings:   f.holdings,
		Events:     f.events,
		Valuations: f.valuations,
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
