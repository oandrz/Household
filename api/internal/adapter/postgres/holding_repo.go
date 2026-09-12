package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// HoldingRepo, HoldingEventRepo and HoldingValuationRepo are three narrow
// repositories over three tables rather than one object with fifteen methods,
// following the same interface-segregation rule the other nine repositories
// here follow. They share this file because they share a migration and a set
// of converters; they do not share an interface.
type HoldingRepo struct{ q *sqlcgen.Queries }

func NewHoldingRepo(db *DB) *HoldingRepo { return &HoldingRepo{q: sqlcgen.New(db.Pool())} }

// The var _ lines pin each repository to its port at compile time, so a
// signature drift is caught here rather than in whichever task first wires
// the service up -- the reason account_repo.go pins AccountRepo to
// AccountLookup.
var (
	_ usecase.HoldingRepository           = (*HoldingRepo)(nil)
	_ usecase.HoldingEventRepository      = (*HoldingEventRepo)(nil)
	_ usecase.HoldingValuationRepository  = (*HoldingValuationRepo)(nil)
)

func (r *HoldingRepo) List(ctx context.Context, householdID string, includeArchived bool) ([]usecase.HoldingRecord, error) {
	rows, err := r.q.ListHoldings(ctx, sqlcgen.ListHoldingsParams{
		HouseholdID:     uuid(householdID),
		IncludeArchived: includeArchived,
	})
	if err != nil {
		return nil, translate(err, "list holdings")
	}
	out := make([]usecase.HoldingRecord, 0, len(rows))
	for _, row := range rows {
		h, err := toHolding(row.ID, row.HouseholdID, row.AccountID, row.Name, row.Instrument,
			row.Unit, row.Currency, row.ArchivedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, usecase.HoldingRecord{
			Holding:         h,
			AccountName:     row.AccountName,
			AccountArchived: row.AccountArchived,
		})
	}
	return out, nil
}

func (r *HoldingRepo) Get(ctx context.Context, householdID, holdingID string) (domain.Holding, error) {
	row, err := r.q.GetHolding(ctx, sqlcgen.GetHoldingParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(holdingID),
	})
	if err != nil {
		return domain.Holding{}, translate(err, "get holding")
	}
	return toHolding(row.ID, row.HouseholdID, row.AccountID, row.Name, row.Instrument,
		row.Unit, row.Currency, row.ArchivedAt)
}

func (r *HoldingRepo) Create(ctx context.Context, h domain.Holding) (domain.Holding, error) {
	row, err := r.q.CreateHolding(ctx, sqlcgen.CreateHoldingParams{
		HouseholdID: uuid(h.HouseholdID),
		AccountID:   uuid(h.AccountID),
		Name:        h.Name,
		Instrument:  string(h.Instrument),
		Unit:        h.Unit,
		Currency:    h.Currency,
	})
	if err != nil {
		return domain.Holding{}, translate(err, "create holding")
	}
	return toHolding(row.ID, row.HouseholdID, row.AccountID, row.Name, row.Instrument,
		row.Unit, row.Currency, row.ArchivedAt)
}

func (r *HoldingRepo) Update(ctx context.Context, h domain.Holding) (domain.Holding, error) {
	row, err := r.q.UpdateHolding(ctx, sqlcgen.UpdateHoldingParams{
		HouseholdID: uuid(h.HouseholdID),
		ID:          uuid(h.ID),
		Name:        h.Name,
		Instrument:  string(h.Instrument),
		Unit:        h.Unit,
	})
	if err != nil {
		return domain.Holding{}, translate(err, "update holding")
	}
	return toHolding(row.ID, row.HouseholdID, row.AccountID, row.Name, row.Instrument,
		row.Unit, row.Currency, row.ArchivedAt)
}

func (r *HoldingRepo) SetArchived(ctx context.Context, householdID, holdingID string, archivedAt *time.Time) (domain.Holding, error) {
	stamp := pgtype.Timestamptz{}
	if archivedAt != nil {
		stamp = timestamptz(*archivedAt)
	}
	row, err := r.q.SetHoldingArchived(ctx, sqlcgen.SetHoldingArchivedParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(holdingID),
		ArchivedAt:  stamp,
	})
	if err != nil {
		return domain.Holding{}, translate(err, "set holding archived")
	}
	return toHolding(row.ID, row.HouseholdID, row.AccountID, row.Name, row.Instrument,
		row.Unit, row.Currency, row.ArchivedAt)
}

func (r *HoldingRepo) CountLiveForAccount(ctx context.Context, householdID, accountID string) (int64, error) {
	n, err := r.q.CountLiveHoldingsForAccount(ctx, sqlcgen.CountLiveHoldingsForAccountParams{
		HouseholdID: uuid(householdID),
		AccountID:   uuid(accountID),
	})
	if err != nil {
		return 0, translate(err, "count live holdings for account")
	}
	return n, nil
}

// toHolding runs the instrument column through the domain's own parser rather
// than casting it. The CHECK constraint and the parser are deliberately
// redundant: a value that somehow got past the database must still not be
// carried further, which is this codebase's fail-closed rule for anything
// arriving from a column.
func toHolding(id, householdID, accountID pgtype.UUID, name, instrument, unit, currency string,
	archivedAt pgtype.Timestamptz) (domain.Holding, error) {
	kind, err := domain.ParseInstrumentKind(instrument)
	if err != nil {
		return domain.Holding{}, err
	}
	return domain.Holding{
		ID:          uuidToString(id),
		HouseholdID: uuidToString(householdID),
		AccountID:   uuidToString(accountID),
		Name:        name,
		Instrument:  kind,
		Unit:        unit,
		Currency:    currency,
		ArchivedAt:  timePtrOf(archivedAt),
	}, nil
}

// HoldingEventRepo keeps the pool alongside the pool-backed *sqlcgen.Queries,
// like GoalRepo and BudgetRepo, because InsertWithFold and DeleteWithFold each
// begin their own transaction -- something a *sqlcgen.Queries built once at
// construction time cannot do on its own.
type HoldingEventRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewHoldingEventRepo(db *DB) *HoldingEventRepo {
	return &HoldingEventRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

func (r *HoldingEventRepo) ListByHolding(ctx context.Context, householdID, holdingID string) ([]domain.HoldingEvent, error) {
	rows, err := r.q.ListHoldingEvents(ctx, sqlcgen.ListHoldingEventsParams{
		HouseholdID: uuid(householdID),
		HoldingID:   uuid(holdingID),
	})
	if err != nil {
		return nil, translate(err, "list holding events")
	}
	out := make([]domain.HoldingEvent, 0, len(rows))
	for _, row := range rows {
		e, err := toHoldingEvent(eventRow{
			ID: row.ID, HoldingID: row.HoldingID, HouseholdID: row.HouseholdID, Kind: row.Kind,
			QuantityNano: row.QuantityNano, AmountMinor: row.AmountMinor,
			PrimaryAmountMinor: row.PrimaryAmountMinor, PrimaryCurrency: row.PrimaryCurrency,
			OccurredOn: row.OccurredOn, Note: row.Note, Currency: row.Currency,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func (r *HoldingEventRepo) ListByHousehold(ctx context.Context, householdID string) ([]domain.HoldingEvent, error) {
	rows, err := r.q.ListHoldingEventsForHousehold(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list holding events for household")
	}
	out := make([]domain.HoldingEvent, 0, len(rows))
	for _, row := range rows {
		e, err := toHoldingEvent(eventRow{
			ID: row.ID, HoldingID: row.HoldingID, HouseholdID: row.HouseholdID, Kind: row.Kind,
			QuantityNano: row.QuantityNano, AmountMinor: row.AmountMinor,
			PrimaryAmountMinor: row.PrimaryAmountMinor, PrimaryCurrency: row.PrimaryCurrency,
			OccurredOn: row.OccurredOn, Note: row.Note, Currency: row.Currency,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func (r *HoldingEventRepo) Insert(ctx context.Context, e domain.HoldingEvent) (domain.HoldingEvent, error) {
	var primaryMinor *int64
	var primaryCurrency *string
	if e.PrimaryAmount != nil {
		amount, currency := e.PrimaryAmount.Amount, e.PrimaryAmount.Currency
		primaryMinor, primaryCurrency = &amount, &currency
	}
	row, err := r.q.InsertHoldingEvent(ctx, sqlcgen.InsertHoldingEventParams{
		HoldingID:          uuid(e.HoldingID),
		HouseholdID:        uuid(e.HouseholdID),
		Kind:               string(e.Kind),
		QuantityNano:       e.Quantity.Nano(),
		AmountMinor:        e.Amount.Amount,
		PrimaryAmountMinor: primaryMinor,
		PrimaryCurrency:    primaryCurrency,
		OccurredOn:         dateOnly(e.OccurredOn),
		Note:               e.Note,
	})
	if err != nil {
		return domain.HoldingEvent{}, translate(err, "insert holding event")
	}
	return toHoldingEvent(eventRow{
		ID: row.ID, HoldingID: row.HoldingID, HouseholdID: row.HouseholdID, Kind: row.Kind,
		QuantityNano: row.QuantityNano, AmountMinor: row.AmountMinor,
		PrimaryAmountMinor: row.PrimaryAmountMinor, PrimaryCurrency: row.PrimaryCurrency,
		OccurredOn: row.OccurredOn, Note: row.Note, Currency: e.Amount.Currency,
	})
}

// InsertWithFold is Insert with the holding's invariant held across the write.
//
// It locks the holding row, lists that holding's events inside the same
// transaction, and hands them to fold -- the caller's own rule, which is
// domain.Holding.Position. Only if fold accepts does the insert happen, and
// the lock is not released until the transaction commits. A second writer
// blocks on the lock and therefore folds the FIRST one's result, not a stale
// copy of it.
//
// Without this, two sales of 30 from a holding of 50 each fold against the
// same 50 and both commit, leaving events that cannot be folded at all -- a
// page that throws every time it loads, fixable only from the page that is
// broken. The fold stays in the domain; this method owns the transaction and
// the lock, never the rule.
func (r *HoldingEventRepo) InsertWithFold(
	ctx context.Context,
	e domain.HoldingEvent,
	fold func([]domain.HoldingEvent) error,
) (domain.HoldingEvent, error) {
	var inserted domain.HoldingEvent
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := q.LockHolding(ctx, sqlcgen.LockHoldingParams{
			HouseholdID: uuid(e.HouseholdID),
			ID:          uuid(e.HoldingID),
		}); err != nil {
			return translate(err, "lock holding")
		}
		existing, err := listEventsTx(ctx, q, e.HouseholdID, e.HoldingID)
		if err != nil {
			return err
		}
		if err := fold(append(existing, e)); err != nil {
			return err
		}
		inserted, err = insertEventTx(ctx, q, e)
		return err
	})
	if err != nil {
		return domain.HoldingEvent{}, err
	}
	return inserted, nil
}

// DeleteWithFold is Delete with the same guarantee in the other direction:
// removing a purchase a later sale was costed against would leave the
// remainder unfoldable, and the check has to hold until the row is gone.
func (r *HoldingEventRepo) DeleteWithFold(
	ctx context.Context,
	householdID, holdingID, eventID string,
	fold func([]domain.HoldingEvent) error,
) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := q.LockHolding(ctx, sqlcgen.LockHoldingParams{
			HouseholdID: uuid(householdID),
			ID:          uuid(holdingID),
		}); err != nil {
			return translate(err, "lock holding")
		}
		existing, err := listEventsTx(ctx, q, householdID, holdingID)
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
		if err := fold(remaining); err != nil {
			return err
		}
		n, err := q.DeleteHoldingEvent(ctx, sqlcgen.DeleteHoldingEventParams{
			HouseholdID: uuid(householdID),
			ID:          uuid(eventID),
		})
		if err != nil {
			return translate(err, "delete holding event")
		}
		if n == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// listEventsTx and insertEventTx are the transaction-scoped halves of
// ListByHolding and Insert, so the guarded methods above reuse the same
// conversion rather than a second copy of it.
func listEventsTx(ctx context.Context, q *sqlcgen.Queries, householdID, holdingID string) ([]domain.HoldingEvent, error) {
	rows, err := q.ListHoldingEvents(ctx, sqlcgen.ListHoldingEventsParams{
		HouseholdID: uuid(householdID),
		HoldingID:   uuid(holdingID),
	})
	if err != nil {
		return nil, translate(err, "list holding events")
	}
	out := make([]domain.HoldingEvent, 0, len(rows))
	for _, row := range rows {
		e, err := toHoldingEvent(eventRow{
			ID: row.ID, HoldingID: row.HoldingID, HouseholdID: row.HouseholdID, Kind: row.Kind,
			QuantityNano: row.QuantityNano, AmountMinor: row.AmountMinor,
			PrimaryAmountMinor: row.PrimaryAmountMinor, PrimaryCurrency: row.PrimaryCurrency,
			OccurredOn: row.OccurredOn, Note: row.Note, Currency: row.Currency,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func insertEventTx(ctx context.Context, q *sqlcgen.Queries, e domain.HoldingEvent) (domain.HoldingEvent, error) {
	var primaryMinor *int64
	var primaryCurrency *string
	if e.PrimaryAmount != nil {
		amount, currency := e.PrimaryAmount.Amount, e.PrimaryAmount.Currency
		primaryMinor, primaryCurrency = &amount, &currency
	}
	row, err := q.InsertHoldingEvent(ctx, sqlcgen.InsertHoldingEventParams{
		HoldingID:          uuid(e.HoldingID),
		HouseholdID:        uuid(e.HouseholdID),
		Kind:               string(e.Kind),
		QuantityNano:       e.Quantity.Nano(),
		AmountMinor:        e.Amount.Amount,
		PrimaryAmountMinor: primaryMinor,
		PrimaryCurrency:    primaryCurrency,
		OccurredOn:         dateOnly(e.OccurredOn),
		Note:               e.Note,
	})
	if err != nil {
		return domain.HoldingEvent{}, translate(err, "insert holding event")
	}
	return toHoldingEvent(eventRow{
		ID: row.ID, HoldingID: row.HoldingID, HouseholdID: row.HouseholdID, Kind: row.Kind,
		QuantityNano: row.QuantityNano, AmountMinor: row.AmountMinor,
		PrimaryAmountMinor: row.PrimaryAmountMinor, PrimaryCurrency: row.PrimaryCurrency,
		OccurredOn: row.OccurredOn, Note: row.Note, Currency: e.Amount.Currency,
	})
}

func (r *HoldingEventRepo) Delete(ctx context.Context, householdID, eventID string) error {
	n, err := r.q.DeleteHoldingEvent(ctx, sqlcgen.DeleteHoldingEventParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(eventID),
	})
	if err != nil {
		return translate(err, "delete holding event")
	}
	// A DELETE that matched nothing is not success. Without this the caller
	// cannot tell "removed" from "was never yours", and a 204 would confirm
	// another household's row exists.
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// eventRow is the handful of fields every holding_events query returns. The
// generated row types differ only in whether they carry the joined currency,
// so the conversion is written once against this rather than three times.
type eventRow struct {
	ID, HoldingID, HouseholdID       pgtype.UUID
	Kind                             string
	QuantityNano, AmountMinor        int64
	PrimaryAmountMinor               *int64
	PrimaryCurrency                  *string
	OccurredOn                       pgtype.Date
	Note, Currency                   string
}

// toHoldingEvent takes the currency from the row's own join, because
// holding_events has no currency column: an event is denominated in its
// holding's currency by construction, exactly as a goal contribution is
// denominated in its goal's (00007_goals.sql). Joining it is what lets the
// fold run on what comes back without a second lookup.
func toHoldingEvent(row eventRow) (domain.HoldingEvent, error) {
	kind, err := domain.ParseHoldingEventKind(row.Kind)
	if err != nil {
		return domain.HoldingEvent{}, err
	}
	quantity, err := domain.NewQuantity(row.QuantityNano)
	if err != nil {
		return domain.HoldingEvent{}, err
	}
	amount, err := domain.NewMoney(row.AmountMinor, row.Currency)
	if err != nil {
		return domain.HoldingEvent{}, err
	}
	// A primary amount is stored with its own currency code because, unlike a
	// transfer's ReceivedAmount, there is no account to join one from. Both
	// columns are NULL together (the primary_amount_is_whole CHECK), so a
	// half-set pair cannot arrive here.
	var primary *domain.Money
	if row.PrimaryAmountMinor != nil && row.PrimaryCurrency != nil {
		p, err := domain.NewMoney(*row.PrimaryAmountMinor, *row.PrimaryCurrency)
		if err != nil {
			return domain.HoldingEvent{}, err
		}
		primary = &p
	}
	return domain.HoldingEvent{
		ID:            uuidToString(row.ID),
		HoldingID:     uuidToString(row.HoldingID),
		HouseholdID:   uuidToString(row.HouseholdID),
		Kind:          kind,
		Quantity:      quantity,
		Amount:        amount,
		PrimaryAmount: primary,
		OccurredOn:    dateToTime(row.OccurredOn),
		Note:          row.Note,
	}, nil
}

type HoldingValuationRepo struct{ q *sqlcgen.Queries }

func NewHoldingValuationRepo(db *DB) *HoldingValuationRepo {
	return &HoldingValuationRepo{q: sqlcgen.New(db.Pool())}
}

func (r *HoldingValuationRepo) ListByHolding(ctx context.Context, householdID, holdingID string) ([]domain.Valuation, error) {
	rows, err := r.q.ListValuations(ctx, sqlcgen.ListValuationsParams{
		HouseholdID: uuid(householdID),
		HoldingID:   uuid(holdingID),
	})
	if err != nil {
		return nil, translate(err, "list valuations")
	}
	out := make([]domain.Valuation, 0, len(rows))
	for _, row := range rows {
		v, err := toValuation(valuationRow{
			ID: row.ID, HoldingID: row.HoldingID, HouseholdID: row.HouseholdID,
			UnitPriceMinor: row.UnitPriceMinor, PrimaryUnitPriceMinor: row.PrimaryUnitPriceMinor,
			PrimaryCurrency: row.PrimaryCurrency, AsOf: row.AsOf, Note: row.Note, Currency: row.Currency,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (r *HoldingValuationRepo) ListLatest(ctx context.Context, householdID string) ([]domain.Valuation, error) {
	rows, err := r.q.ListLatestValuations(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list latest valuations")
	}
	out := make([]domain.Valuation, 0, len(rows))
	for _, row := range rows {
		v, err := toValuation(valuationRow{
			ID: row.ID, HoldingID: row.HoldingID, HouseholdID: row.HouseholdID,
			UnitPriceMinor: row.UnitPriceMinor, PrimaryUnitPriceMinor: row.PrimaryUnitPriceMinor,
			PrimaryCurrency: row.PrimaryCurrency, AsOf: row.AsOf, Note: row.Note, Currency: row.Currency,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (r *HoldingValuationRepo) Upsert(ctx context.Context, v domain.Valuation) (domain.Valuation, error) {
	var primaryMinor *int64
	var primaryCurrency *string
	if v.PrimaryUnitPrice != nil {
		amount, currency := v.PrimaryUnitPrice.Amount, v.PrimaryUnitPrice.Currency
		primaryMinor, primaryCurrency = &amount, &currency
	}
	row, err := r.q.UpsertValuation(ctx, sqlcgen.UpsertValuationParams{
		HoldingID:             uuid(v.HoldingID),
		HouseholdID:           uuid(v.HouseholdID),
		UnitPriceMinor:        v.UnitPrice.Amount,
		PrimaryUnitPriceMinor: primaryMinor,
		PrimaryCurrency:       primaryCurrency,
		AsOf:                  dateOnly(v.AsOf),
		Note:                  v.Note,
	})
	if err != nil {
		return domain.Valuation{}, translate(err, "upsert valuation")
	}
	return toValuation(valuationRow{
		ID: row.ID, HoldingID: row.HoldingID, HouseholdID: row.HouseholdID,
		UnitPriceMinor: row.UnitPriceMinor, PrimaryUnitPriceMinor: row.PrimaryUnitPriceMinor,
		PrimaryCurrency: row.PrimaryCurrency, AsOf: row.AsOf, Note: row.Note,
		Currency: v.UnitPrice.Currency,
	})
}

func (r *HoldingValuationRepo) Delete(ctx context.Context, householdID, valuationID string) error {
	n, err := r.q.DeleteValuation(ctx, sqlcgen.DeleteValuationParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(valuationID),
	})
	if err != nil {
		return translate(err, "delete valuation")
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// valuationRow is the shape every holding_valuations query returns, with the
// currency joined from its holding.
type valuationRow struct {
	ID, HoldingID, HouseholdID pgtype.UUID
	UnitPriceMinor             int64
	PrimaryUnitPriceMinor      *int64
	PrimaryCurrency            *string
	AsOf                       pgtype.Date
	Note, Currency             string
}

// toValuation carries the same "currency comes from the holding" convention
// toHoldingEvent does, and for the same reason: holding_valuations has no
// currency column, because a price is denominated in its holding's currency
// by construction.
func toValuation(row valuationRow) (domain.Valuation, error) {
	price, err := domain.NewMoney(row.UnitPriceMinor, row.Currency)
	if err != nil {
		return domain.Valuation{}, err
	}
	var primary *domain.Money
	if row.PrimaryUnitPriceMinor != nil && row.PrimaryCurrency != nil {
		p, err := domain.NewMoney(*row.PrimaryUnitPriceMinor, *row.PrimaryCurrency)
		if err != nil {
			return domain.Valuation{}, err
		}
		primary = &p
	}
	return domain.Valuation{
		ID:               uuidToString(row.ID),
		HoldingID:        uuidToString(row.HoldingID),
		HouseholdID:      uuidToString(row.HouseholdID),
		UnitPrice:        price,
		PrimaryUnitPrice: primary,
		AsOf:             dateToTime(row.AsOf),
		Note:             row.Note,
	}, nil
}
