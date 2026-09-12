package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func insertTestInvestmentAccount(t *testing.T, db *postgres.DB, householdID, nickname string) string {
	t.Helper()
	var id string
	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO accounts (household_id, nickname, type, opening_balance_minor,
		                       opening_balance_currency, opening_balance_as_of)
		 VALUES ($1, $2, 'investment', 0, 'SGD', DATE '2026-07-01') RETURNING id`,
		householdID, nickname).Scan(&id)
	if err != nil {
		t.Fatalf("insert investment account %s: %v", nickname, err)
	}
	return id
}

func newTestHolding(householdID, accountID, name string) domain.Holding {
	return domain.Holding{
		HouseholdID: householdID,
		AccountID:   accountID,
		Name:        name,
		Instrument:  domain.InstrumentStock,
		Unit:        "share",
		Currency:    "SGD",
	}
}

func testQuantity(t *testing.T, units int64) domain.Quantity {
	t.Helper()
	q, err := domain.NewQuantity(units * domain.QuantityScale)
	if err != nil {
		t.Fatalf("NewQuantity: %v", err)
	}
	return q
}

func TestHoldingCreateAndGetRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewHoldingRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")

	created, err := repo.Create(ctx, newTestHolding(householdID, accountID, "D05"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, householdID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "D05" || got.Currency != "SGD" || got.Instrument != domain.InstrumentStock || got.Unit != "share" {
		t.Fatalf("got %+v, want the holding as created", got)
	}
	if got.AccountID != accountID {
		t.Fatalf("AccountID = %q, want %q", got.AccountID, accountID)
	}
}

// A holding id belonging to another household must read as ABSENT, never as
// forbidden -- a 403 would confirm the row exists.
func TestHoldingGetFromAnotherHouseholdIsNotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewHoldingRepo(db)
	mine := insertTestHousehold(t, db)
	theirs := insertTestHousehold(t, db)
	theirAccount := insertTestInvestmentAccount(t, db, theirs, "Their brokerage")

	created, err := repo.Create(ctx, newTestHolding(theirs, theirAccount, "D05"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := repo.Get(ctx, mine, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get across households: error = %v, want ErrNotFound", err)
	}
}

// The unique key is (account_id, name), NOT (household_id, name): holding the
// same ticker in two brokerages is ordinary and they are genuinely different
// positions with different cost bases.
func TestHoldingNameIsUniquePerAccountNotPerHousehold(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewHoldingRepo(db)
	householdID := insertTestHousehold(t, db)
	first := insertTestInvestmentAccount(t, db, householdID, "Brokerage A")
	second := insertTestInvestmentAccount(t, db, householdID, "Brokerage B")

	if _, err := repo.Create(ctx, newTestHolding(householdID, first, "D05")); err != nil {
		t.Fatalf("Create in first account: %v", err)
	}
	if _, err := repo.Create(ctx, newTestHolding(householdID, second, "D05")); err != nil {
		t.Fatalf("the same name in a SECOND account must be allowed: %v", err)
	}
	if _, err := repo.Create(ctx, newTestHolding(householdID, first, "D05")); !errors.Is(err, domain.ErrHoldingNameTaken) {
		t.Fatalf("duplicate in the same account: error = %v, want ErrHoldingNameTaken", err)
	}
}

// An archived holding still occupies its name, so the collision offers restore
// rather than silently creating a second one -- the goals and categories rule.
func TestAnArchivedHoldingStillOccupiesItsName(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewHoldingRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")

	created, err := repo.Create(ctx, newTestHolding(householdID, accountID, "D05"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	archivedAt := july(5)
	if _, err := repo.SetArchived(ctx, householdID, created.ID, &archivedAt); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	if _, err := repo.Create(ctx, newTestHolding(householdID, accountID, "D05")); !errors.Is(err, domain.ErrHoldingNameTaken) {
		t.Fatalf("error = %v, want ErrHoldingNameTaken", err)
	}
}

func TestHoldingListCarriesItsAccountNameAndArchivedFlag(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewHoldingRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")
	if _, err := repo.Create(ctx, newTestHolding(householdID, accountID, "D05")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	records, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len = %d, want 1", len(records))
	}
	if records[0].AccountName != "Brokerage" {
		t.Fatalf("AccountName = %q, want Brokerage", records[0].AccountName)
	}
	if records[0].AccountArchived {
		t.Fatal("AccountArchived = true, want false")
	}
}

// includeArchived is a UNION, not a filter swap: true returns live AND
// archived together, never archived instead.
func TestHoldingListIncludeArchivedReturnsBoth(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewHoldingRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")

	live, _ := repo.Create(ctx, newTestHolding(householdID, accountID, "Alive"))
	gone, _ := repo.Create(ctx, newTestHolding(householdID, accountID, "Gone"))
	archivedAt := july(5)
	if _, err := repo.SetArchived(ctx, householdID, gone.ID, &archivedAt); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	liveOnly, err := repo.List(ctx, householdID, false)
	if err != nil {
		t.Fatalf("List(false): %v", err)
	}
	if len(liveOnly) != 1 || liveOnly[0].Holding.ID != live.ID {
		t.Fatalf("List(false) = %d rows, want just the live one", len(liveOnly))
	}

	both, err := repo.List(ctx, householdID, true)
	if err != nil {
		t.Fatalf("List(true): %v", err)
	}
	if len(both) != 2 {
		t.Fatalf("List(true) = %d rows, want 2 -- includeArchived is a union, not a swap", len(both))
	}
}

func TestCountLiveForAccountIgnoresArchivedHoldings(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewHoldingRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")

	if _, err := repo.Create(ctx, newTestHolding(householdID, accountID, "Alive")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	gone, _ := repo.Create(ctx, newTestHolding(householdID, accountID, "Gone"))
	archivedAt := july(5)
	if _, err := repo.SetArchived(ctx, householdID, gone.ID, &archivedAt); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	n, err := repo.CountLiveForAccount(ctx, householdID, accountID)
	if err != nil {
		t.Fatalf("CountLiveForAccount: %v", err)
	}
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
}

// THE ordering contract. occurred_on is a date, so two events share one when
// a household buys and sells the same morning, and domain.Holding.Position
// sorts stably -- it keeps whatever order it is handed for a tie. So the tie
// has to be broken here, by the order the events were actually recorded in.
// Return them any other way and realised gain changes silently.
func TestHoldingEventsComeBackInRecordedOrderForTheSameDay(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	holdings := postgres.NewHoldingRepo(db)
	events := postgres.NewHoldingEventRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")
	h, err := holdings.Create(ctx, newTestHolding(householdID, accountID, "D05"))
	if err != nil {
		t.Fatalf("Create holding: %v", err)
	}

	sameDay := july(5)
	first, err := events.Insert(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: householdID, Kind: domain.HoldingAcquisition,
		Quantity: testQuantity(t, 10), Amount: moneyOf(1000), OccurredOn: sameDay,
	})
	if err != nil {
		t.Fatalf("Insert first: %v", err)
	}
	second, err := events.Insert(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: householdID, Kind: domain.HoldingDisposal,
		Quantity: testQuantity(t, 5), Amount: moneyOf(1500), OccurredOn: sameDay,
	})
	if err != nil {
		t.Fatalf("Insert second: %v", err)
	}

	got, err := events.ListByHolding(ctx, householdID, h.ID)
	if err != nil {
		t.Fatalf("ListByHolding: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != first.ID || got[1].ID != second.ID {
		t.Fatalf("order = %q,%q; want %q,%q -- same-day events must come back in the order recorded",
			got[0].ID, got[1].ID, first.ID, second.ID)
	}
	// The fold reads these directly, so the round trip has to preserve what it
	// folds on, not merely the ids.
	if got[0].Quantity.Nano() != 10*domain.QuantityScale || got[0].Amount.Amount != 1000 {
		t.Fatalf("first event round-tripped as %+v", got[0])
	}
	if got[0].Amount.Currency != "SGD" {
		t.Fatalf("Currency = %q, want SGD -- an event is denominated in its holding's currency",
			got[0].Amount.Currency)
	}
}

// A cross-currency event stores its primary-currency twin AND that currency's
// code, because unlike a transfer's ReceivedAmount there is no account to join
// the code from.
func TestHoldingEventRoundTripsItsPrimaryAmountAndCurrency(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	holdings := postgres.NewHoldingRepo(db)
	events := postgres.NewHoldingEventRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")
	usd := newTestHolding(householdID, accountID, "VOO")
	usd.Currency = "USD"
	h, err := holdings.Create(ctx, usd)
	if err != nil {
		t.Fatalf("Create holding: %v", err)
	}

	native, _ := domain.NewMoney(50000, "USD")
	primary, _ := domain.NewMoney(67500, "SGD")
	if _, err := events.Insert(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: householdID, Kind: domain.HoldingAcquisition,
		Quantity: testQuantity(t, 10), Amount: native, PrimaryAmount: &primary,
		OccurredOn: july(5),
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := events.ListByHolding(ctx, householdID, h.ID)
	if err != nil {
		t.Fatalf("ListByHolding: %v", err)
	}
	if got[0].PrimaryAmount == nil {
		t.Fatal("PrimaryAmount = nil, want the SGD figure back")
	}
	if got[0].PrimaryAmount.Amount != 67500 || got[0].PrimaryAmount.Currency != "SGD" {
		t.Fatalf("PrimaryAmount = %+v, want 67500 SGD", *got[0].PrimaryAmount)
	}
}

// A same-currency event stores no primary amount at all, and must come back as
// nil rather than as a zero Money -- Money.Add refuses a zero-value currency,
// so a zero here would fail on first use.
func TestASameCurrencyEventHasNoPrimaryAmountOnTheWayBack(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	holdings := postgres.NewHoldingRepo(db)
	events := postgres.NewHoldingEventRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")
	h, _ := holdings.Create(ctx, newTestHolding(householdID, accountID, "D05"))

	if _, err := events.Insert(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: householdID, Kind: domain.HoldingAcquisition,
		Quantity: testQuantity(t, 10), Amount: moneyOf(1000), OccurredOn: july(5),
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, _ := events.ListByHolding(ctx, householdID, h.ID)
	if got[0].PrimaryAmount != nil {
		t.Fatalf("PrimaryAmount = %+v, want nil", *got[0].PrimaryAmount)
	}
}

// One price per holding per day: re-entering a day's price is a correction,
// not a second opinion.
func TestValuationUpsertReplacesTheSameDayRatherThanAddingASecond(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	holdings := postgres.NewHoldingRepo(db)
	valuations := postgres.NewHoldingValuationRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")
	h, _ := holdings.Create(ctx, newTestHolding(householdID, accountID, "D05"))

	asOf := july(5)
	if _, err := valuations.Upsert(ctx, domain.Valuation{
		HoldingID: h.ID, HouseholdID: householdID, UnitPrice: moneyOf(100), AsOf: asOf,
	}); err != nil {
		t.Fatalf("Upsert first: %v", err)
	}
	if _, err := valuations.Upsert(ctx, domain.Valuation{
		HoldingID: h.ID, HouseholdID: householdID, UnitPrice: moneyOf(250), AsOf: asOf,
	}); err != nil {
		t.Fatalf("Upsert correction: %v", err)
	}

	got, err := valuations.ListByHolding(ctx, householdID, h.ID)
	if err != nil {
		t.Fatalf("ListByHolding: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 -- a second price for one day is a correction", len(got))
	}
	if got[0].UnitPrice.Amount != 250 {
		t.Fatalf("UnitPrice = %d, want 250", got[0].UnitPrice.Amount)
	}
}

// ListLatest gives at most one row per holding, and NO row for a holding that
// has never been priced -- the caller has to be able to say "no price
// recorded" rather than show a figure of zero.
func TestListLatestValuationsSkipsAHoldingWithNoPrice(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	holdings := postgres.NewHoldingRepo(db)
	valuations := postgres.NewHoldingValuationRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")
	priced, _ := holdings.Create(ctx, newTestHolding(householdID, accountID, "Priced"))
	if _, err := holdings.Create(ctx, newTestHolding(householdID, accountID, "Unpriced")); err != nil {
		t.Fatalf("Create unpriced: %v", err)
	}

	for _, v := range []struct {
		day   int
		minor int64
	}{{4, 100}, {6, 300}, {5, 200}} {
		if _, err := valuations.Upsert(ctx, domain.Valuation{
			HoldingID: priced.ID, HouseholdID: householdID,
			UnitPrice: moneyOf(v.minor), AsOf: july(v.day),
		}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	latest, err := valuations.ListLatest(ctx, householdID)
	if err != nil {
		t.Fatalf("ListLatest: %v", err)
	}
	if len(latest) != 1 {
		t.Fatalf("len = %d, want 1 -- an unpriced holding must produce no row at all", len(latest))
	}
	if latest[0].UnitPrice.Amount != 300 {
		t.Fatalf("UnitPrice = %d, want 300 (the newest as_of, not the newest write)", latest[0].UnitPrice.Amount)
	}
	if !latest[0].AsOf.Equal(july(6)) {
		t.Fatalf("AsOf = %v, want %v", latest[0].AsOf, july(6))
	}
}

func TestDeletingAnotherHouseholdsEventIsNotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	holdings := postgres.NewHoldingRepo(db)
	events := postgres.NewHoldingEventRepo(db)
	mine := insertTestHousehold(t, db)
	theirs := insertTestHousehold(t, db)
	theirAccount := insertTestInvestmentAccount(t, db, theirs, "Theirs")
	h, _ := holdings.Create(ctx, newTestHolding(theirs, theirAccount, "D05"))
	e, err := events.Insert(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: theirs, Kind: domain.HoldingAcquisition,
		Quantity: testQuantity(t, 1), Amount: moneyOf(100), OccurredOn: july(5),
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := events.Delete(ctx, mine, e.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Delete across households: error = %v, want ErrNotFound", err)
	}
}

// Two sales of the same holding, racing. Each on its own is legal -- 30 of 50
// grams -- and together they are not: 60 of 50. Without a lock both fold
// against the same 50 and both commit, leaving a holding whose events cannot
// be folded at all, which is a page that throws every time it loads and can
// only be fixed from the page that is broken.
//
// InsertWithFold is what closes that: it locks the holding row, lists the
// events inside the same transaction, and inserts only if the caller's fold
// accepts them. The fold itself stays in the domain -- the repository owns the
// transaction and the lock, never the rule.
func TestTwoRacingDisposalsCannotBothCommit(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	holdings := postgres.NewHoldingRepo(db)
	events := postgres.NewHoldingEventRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")
	h, err := holdings.Create(ctx, newTestHolding(householdID, accountID, "D05"))
	if err != nil {
		t.Fatalf("Create holding: %v", err)
	}
	if _, err := events.InsertWithFold(ctx, domain.HoldingEvent{
		HoldingID: h.ID, HouseholdID: householdID, Kind: domain.HoldingAcquisition,
		Quantity: testQuantity(t, 50), Amount: moneyOf(5000), OccurredOn: july(1),
	}, func([]domain.HoldingEvent) error { return nil }); err != nil {
		t.Fatalf("buy: %v", err)
	}

	// The fold deliberately sleeps, to hold the check-to-write window open long
	// enough for the two goroutines to genuinely overlap. Without it they
	// serialise by luck and the test passes even with no lock at all -- which
	// is exactly what happened the first time this was written, and what a
	// mutation run caught.
	//
	// With the lock: the second writer blocks inside LockHolding before it ever
	// reaches fold, and folds the first one's committed result.
	// Without it: both fold the same starting position, both sleep, both write.
	sell := func() error {
		_, err := events.InsertWithFold(ctx, domain.HoldingEvent{
			HoldingID: h.ID, HouseholdID: householdID, Kind: domain.HoldingDisposal,
			Quantity: testQuantity(t, 30), Amount: moneyOf(4000), OccurredOn: july(2),
		}, func(existing []domain.HoldingEvent) error {
			time.Sleep(300 * time.Millisecond)
			// The real fold: the service passes exactly this.
			_, err := h.Position(existing, "SGD")
			return err
		})
		return err
	}

	results := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < 2; i++ {
		go func() {
			start.Wait()
			results <- sell()
		}()
	}
	start.Done()
	first, second := <-results, <-results

	oversold := 0
	for _, err := range []error{first, second} {
		if errors.Is(err, domain.ErrHoldingOversold) {
			oversold++
		} else if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if oversold != 1 {
		t.Fatalf("%d of 2 racing sales were refused, want exactly 1 (errors: %v, %v)", oversold, first, second)
	}

	// And the surviving position must still fold.
	after, err := events.ListByHolding(ctx, householdID, h.ID)
	if err != nil {
		t.Fatalf("ListByHolding: %v", err)
	}
	position, err := h.Position(after, "SGD")
	if err != nil {
		t.Fatalf("the holding no longer folds after the race: %v", err)
	}
	if position.Held.Nano() != 20*domain.QuantityScale {
		t.Fatalf("Held = %d, want 20 units (50 bought, 30 sold once)", position.Held.Nano())
	}
}

// --- income and the report's valuation read ---------------------------------

func TestHoldingIncomeRoundTripsBothKinds(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	holdings := postgres.NewHoldingRepo(db)
	incomeRepo := postgres.NewHoldingIncomeRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")
	h, err := holdings.Create(ctx, newTestHolding(householdID, accountID, "D05"))
	if err != nil {
		t.Fatalf("Create holding: %v", err)
	}

	dividend, _ := domain.NewMoney(4500, "SGD")
	custody, _ := domain.NewMoney(250, "SGD")
	for _, in := range []domain.HoldingIncome{
		{HoldingID: h.ID, HouseholdID: householdID, Kind: domain.IncomeReceived,
			Amount: dividend, ReceivedOn: july(5), Note: "H1 dividend"},
		{HoldingID: h.ID, HouseholdID: householdID, Kind: domain.IncomeFee,
			Amount: custody, ReceivedOn: july(9)},
	} {
		if _, err := incomeRepo.Insert(ctx, in); err != nil {
			t.Fatalf("Insert %s: %v", in.Kind, err)
		}
	}

	got, err := incomeRepo.ListByHolding(ctx, householdID, h.ID)
	if err != nil {
		t.Fatalf("ListByHolding: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	// Newest first, which is the order a panel under the holding reads them in.
	if got[0].Kind != domain.IncomeFee || got[0].Amount.Amount != 250 {
		t.Errorf("first row = %+v, want the 9 July fee", got[0])
	}
	if got[1].Kind != domain.IncomeReceived || got[1].Note != "H1 dividend" {
		t.Errorf("second row = %+v, want the 5 July dividend", got[1])
	}
	// The currency comes from the holding's own column through the join; the
	// table has none.
	if got[0].Amount.Currency != "SGD" {
		t.Errorf("currency = %q, want SGD from the holding", got[0].Amount.Currency)
	}
}

func TestHoldingIncomeRoundTripsItsPrimaryAmount(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	holdings := postgres.NewHoldingRepo(db)
	incomeRepo := postgres.NewHoldingIncomeRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")
	usd := newTestHolding(householdID, accountID, "VOO")
	usd.Currency = "USD"
	h, err := holdings.Create(ctx, usd)
	if err != nil {
		t.Fatalf("Create holding: %v", err)
	}

	native, _ := domain.NewMoney(1200, "USD")
	primary, _ := domain.NewMoney(1620, "SGD")
	if _, err := incomeRepo.Insert(ctx, domain.HoldingIncome{
		HoldingID: h.ID, HouseholdID: householdID, Kind: domain.IncomeReceived,
		Amount: native, PrimaryAmount: &primary, ReceivedOn: july(5),
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := incomeRepo.ListByHousehold(ctx, householdID)
	if err != nil {
		t.Fatalf("ListByHousehold: %v", err)
	}
	if len(got) != 1 || got[0].PrimaryAmount == nil {
		t.Fatalf("got %+v, want one row carrying its primary amount", got)
	}
	if got[0].PrimaryAmount.Amount != 1620 || got[0].PrimaryAmount.Currency != "SGD" {
		t.Errorf("PrimaryAmount = %+v, want 1620 SGD", *got[0].PrimaryAmount)
	}
}

// Deleting a row that is not there reads as absent at this boundary, never as
// a zero row count travelling further up.
func TestDeletingAnAbsentIncomeRowIsNotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	incomeRepo := postgres.NewHoldingIncomeRepo(db)
	householdID := insertTestHousehold(t, db)

	err := incomeRepo.Delete(ctx, householdID, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}
}

// The zero-amount rule is enforced in the domain AND in the schema, on purpose
// -- the same deliberate redundancy 00007_goals.sql describes. This test is
// what proves the CHECK is really there rather than only the Go guard.
func TestTheSchemaRefusesAZeroIncomeAmount(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	holdings := postgres.NewHoldingRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")
	h, err := holdings.Create(ctx, newTestHolding(householdID, accountID, "D05"))
	if err != nil {
		t.Fatalf("Create holding: %v", err)
	}

	_, err = db.Pool().Exec(ctx,
		`INSERT INTO holding_income (holding_id, household_id, kind, amount_minor, received_on)
		 VALUES ($1, $2, 'income', 0, DATE '2026-07-05')`, h.ID, householdID)
	if err == nil {
		t.Fatal("the schema accepted a zero income amount")
	}
}

// The period report cannot use ListLatest: a quarter opens at a price recorded
// in the quarter before it, which the latest-only read has thrown away. This
// read keeps the whole history, oldest first per holding.
func TestValuationsForHouseholdReturnsEveryPriceNotOnlyTheNewest(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	holdings := postgres.NewHoldingRepo(db)
	valuations := postgres.NewHoldingValuationRepo(db)
	householdID := insertTestHousehold(t, db)
	accountID := insertTestInvestmentAccount(t, db, householdID, "Brokerage")
	h, err := holdings.Create(ctx, newTestHolding(householdID, accountID, "D05"))
	if err != nil {
		t.Fatalf("Create holding: %v", err)
	}

	for _, day := range []int{5, 9, 1} {
		price, _ := domain.NewMoney(int64(1000+day), "SGD")
		if _, err := valuations.Upsert(ctx, domain.Valuation{
			HoldingID: h.ID, HouseholdID: householdID, UnitPrice: price, AsOf: july(day),
		}); err != nil {
			t.Fatalf("Upsert %d July: %v", day, err)
		}
	}

	got, err := valuations.ListForHousehold(ctx, householdID)
	if err != nil {
		t.Fatalf("ListForHousehold: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d prices, want all 3", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].AsOf.Before(got[i-1].AsOf) {
			t.Fatalf("prices came back out of order: %s before %s",
				got[i].AsOf.Format(time.DateOnly), got[i-1].AsOf.Format(time.DateOnly))
		}
	}
}
