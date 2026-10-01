package httpadapter

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// holdingDTO is one holding as the portfolio screen sees it.
//
// Quantity crosses the wire as both a string (for display) and its nano
// integer (for exact arithmetic) -- deliberately. The browser must never
// divide the nano integer by 1e9 itself: that's float64 arithmetic on a money
// figure (docs/LEARNING.md: 333333 * 0.3 === 99999.90000000001 in
// JavaScript). Sending both keeps the division here, in integers.
type holdingDTO struct {
	ID          string     `json:"id"`
	AccountID   string     `json:"accountId"`
	AccountName string     `json:"accountName"`
	Name        string     `json:"name"`
	Instrument  string     `json:"instrument"`
	Unit        string     `json:"unit"`
	Currency    string     `json:"currency"`
	ArchivedAt  *time.Time `json:"archivedAt"`

	HeldNano      int64  `json:"heldNano"`
	Held          string `json:"held"`
	CostMinor     int64  `json:"costMinor"`
	RealisedMinor int64  `json:"realisedMinor"`

	// HasMarketValue false means no figure, not a figure of zero: an unpriced
	// holding is unknowable, not worthless, so the screen shows the reason
	// instead -- same rule the net worth card follows for a stranded account.
	// ValuedAt is null then, and otherwise says how stale the price is.
	MarketValueMinor int64   `json:"marketValueMinor"`
	HasMarketValue   bool    `json:"hasMarketValue"`
	ValuedAt         *string `json:"valuedAt"`

	// The same value in the household's own currency, present only when the
	// holding isn't already in it. Null means "already in your currency", not
	// "couldn't work it out" -- so the screen shows one figure, not two.
	PrimaryMarketValueMinor *int64  `json:"primaryMarketValueMinor"`
	PrimaryCurrency         *string `json:"primaryCurrency"`
}

// portfolioResponse carries NotInNetWorth as a literal wire-level fact rather
// than letting the page hard-code it. Holdings are deliberately kept out of
// net worth and the trend for now; the page says so on its face, and when
// that changes, this flag and the label change with it.
type portfolioResponse struct {
	Holdings      []holdingDTO `json:"holdings"`
	NotInNetWorth bool         `json:"notInNetWorth"`
}

type holdingResponse struct {
	Holding holdingDTO `json:"holding"`
}

type holdingEventDTO struct {
	ID                 string  `json:"id"`
	Kind               string  `json:"kind"`
	QuantityNano       int64   `json:"quantityNano"`
	Quantity           string  `json:"quantity"`
	AmountMinor        int64   `json:"amountMinor"`
	Currency           string  `json:"currency"`
	PrimaryAmountMinor *int64  `json:"primaryAmountMinor"`
	PrimaryCurrency    *string `json:"primaryCurrency"`
	OccurredOn         string  `json:"occurredOn"`
	Note               string  `json:"note"`
}

type holdingValuationDTO struct {
	ID                    string  `json:"id"`
	UnitPriceMinor        int64   `json:"unitPriceMinor"`
	Currency              string  `json:"currency"`
	PrimaryUnitPriceMinor *int64  `json:"primaryUnitPriceMinor"`
	PrimaryCurrency       *string `json:"primaryCurrency"`
	AsOf                  string  `json:"asOf"`
	Note                  string  `json:"note"`
}

const holdingDateLayout = "2006-01-02"

func toHoldingDTO(v usecase.HoldingPositionView) holdingDTO {
	dto := holdingDTO{
		ID:            v.Holding.ID,
		AccountID:     v.Holding.AccountID,
		AccountName:   v.AccountName,
		Name:          v.Holding.Name,
		Instrument:    string(v.Holding.Instrument),
		Unit:          v.Holding.Unit,
		Currency:      v.Holding.Currency,
		ArchivedAt:    v.Holding.ArchivedAt,
		HeldNano:      v.Position.Held.Nano(),
		Held:          domain.FormatQuantity(v.Position.Held),
		CostMinor:     v.Position.Cost.Amount,
		RealisedMinor: v.Position.Realised.Amount,
	}
	if v.HasMarketValue {
		asOf := v.ValuedAt.Format(holdingDateLayout)
		dto.MarketValueMinor, dto.HasMarketValue, dto.ValuedAt = v.MarketValue.Amount, true, &asOf
		if v.HasPrimaryMarketValue {
			amount, currency := v.PrimaryMarketValue.Amount, v.PrimaryMarketValue.Currency
			dto.PrimaryMarketValueMinor, dto.PrimaryCurrency = &amount, &currency
		}
	}
	return dto
}

func handleListHoldings(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		// include_archived is a union, not a filter swap, and is spelled the
		// way accounts and goals spell it.
		includeArchived := r.URL.Query().Get("include_archived") == "true"
		view, err := deps.Holdings.Portfolio(r.Context(), scope.HouseholdID, includeArchived)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		out := make([]holdingDTO, 0, len(view.Holdings))
		for _, h := range view.Holdings {
			out = append(out, toHoldingDTO(h))
		}
		WriteJSON(w, http.StatusOK, portfolioResponse{Holdings: out, NotInNetWorth: true})
	}
}

type createHoldingRequest struct {
	AccountID  string `json:"accountId"`
	Name       string `json:"name"`
	Instrument string `json:"instrument"`
	Unit       string `json:"unit"`
	Currency   string `json:"currency"`
}

func handleCreateHolding(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		var req createHoldingRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		instrument, err := domain.ParseInstrumentKind(strings.TrimSpace(req.Instrument))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		currency := strings.TrimSpace(req.Currency)
		if currency == "" {
			household, err := deps.Households.Get(r.Context(), scope.HouseholdID)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			currency = household.PrimaryCurrency
		}

		created, err := deps.Holdings.Create(r.Context(), domain.Holding{
			HouseholdID: scope.HouseholdID,
			AccountID:   req.AccountID,
			Name:        req.Name,
			Instrument:  instrument,
			Unit:        req.Unit,
			Currency:    currency,
		})
		if err != nil {
			writeHoldingNameConflict(w, r, deps, scope.HouseholdID, req.AccountID, req.Name, err)
			return
		}
		writeOneHolding(w, r, deps, scope.HouseholdID, created.ID, http.StatusCreated)
	}
}

type updateHoldingRequest struct {
	Name       string `json:"name"`
	Instrument string `json:"instrument"`
	Unit       string `json:"unit"`
}

func handleUpdateHolding(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		id := chi.URLParam(r, "id")
		var req updateHoldingRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		instrument, err := domain.ParseInstrumentKind(strings.TrimSpace(req.Instrument))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		if _, err := deps.Holdings.Update(r.Context(), domain.Holding{
			ID: id, HouseholdID: scope.HouseholdID,
			Name: req.Name, Instrument: instrument, Unit: req.Unit,
		}); err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeOneHolding(w, r, deps, scope.HouseholdID, id, http.StatusOK)
	}
}

// Archive and restore are their own routes, not a field on PATCH -- same
// reasoning as accounts, categories and goals: a patchable field risks an
// ordinary rename archiving the holding as a side effect.
func handleArchiveHolding(deps Deps) http.HandlerFunc { return setHoldingArchived(deps, true) }
func handleRestoreHolding(deps Deps) http.HandlerFunc { return setHoldingArchived(deps, false) }

func setHoldingArchived(deps Deps, archived bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		id := chi.URLParam(r, "id")
		if _, err := deps.Holdings.SetArchived(r.Context(), scope.HouseholdID, id, archived, deps.Clock.Now()); err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeOneHolding(w, r, deps, scope.HouseholdID, id, http.StatusOK)
	}
}

// writeOneHolding re-reads the whole portfolio so a write answers with the
// same shape a read does, derived figures included -- the same reason
// writeGoal re-reads rather than converting what Create returned.
//
// The cost: every write issues three queries (holdings, events, valuations)
// and folds every position, just to return one row -- so recording ten lots
// costs ten portfolio reads. That's free at a household's scale (single-digit
// holdings, tens of events); the alternative is a write response computed
// differently from a read's, which is how the two drift apart. It stops being
// free around hundreds of events, where a per-holding read replaces this.
func writeOneHolding(w http.ResponseWriter, r *http.Request, deps Deps, householdID, holdingID string, status int) {
	// includeArchived is always true here: archiving must still answer with
	// the holding it just archived, folded as live so the response doesn't
	// read as an empty position.
	view, err := deps.Holdings.Portfolio(r.Context(), householdID, true)
	if err != nil {
		MapDomainError(w, r, err)
		return
	}
	for _, h := range view.Holdings {
		if h.Holding.ID == holdingID {
			WriteJSON(w, status, holdingResponse{Holding: toHoldingDTO(h)})
			return
		}
	}
	MapDomainError(w, r, domain.ErrNotFound)
}

// writeHoldingNameConflict turns the plain 409 into one the modal can act on
// when the colliding holding is archived: it carries that holding's id so the
// screen can offer Restore instead of a dead end. Archived rows still occupy
// their name (archived_at isn't part of the unique key), so without this, an
// archived name blocks re-adding with no visible cause.
//
// Same precedent as goal_handlers.go's writeGoalNameConflict: a failed lookup
// of the archived row falls back to the plain error, not a worse one.
func writeHoldingNameConflict(w http.ResponseWriter, r *http.Request, deps Deps, householdID, accountID, name string, err error) {
	if !errors.Is(err, domain.ErrHoldingNameTaken) {
		MapDomainError(w, r, err)
		return
	}
	view, listErr := deps.Holdings.List(r.Context(), householdID, true)
	if listErr != nil {
		MapDomainError(w, r, err)
		return
	}
	for _, rec := range view {
		if rec.Holding.AccountID == accountID && rec.Holding.Name == strings.TrimSpace(name) && rec.Holding.IsArchived() {
			WriteError(w, http.StatusConflict, "HOLDING_NAME_TAKEN",
				"You archived a holding with that name in this account. Restore it instead?",
				map[string]any{"archivedHoldingId": rec.Holding.ID})
			return
		}
	}
	MapDomainError(w, r, err)
}

func handleListHoldingEvents(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		events, err := deps.Holdings.ListEvents(r.Context(), scope.HouseholdID, chi.URLParam(r, "id"))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		out := make([]holdingEventDTO, 0, len(events))
		for _, e := range events {
			dto := holdingEventDTO{
				ID: e.ID, Kind: string(e.Kind),
				QuantityNano: e.Quantity.Nano(), Quantity: domain.FormatQuantity(e.Quantity),
				AmountMinor: e.Amount.Amount, Currency: e.Amount.Currency,
				OccurredOn: e.OccurredOn.Format(holdingDateLayout), Note: e.Note,
			}
			if e.PrimaryAmount != nil {
				amount, currency := e.PrimaryAmount.Amount, e.PrimaryAmount.Currency
				dto.PrimaryAmountMinor, dto.PrimaryCurrency = &amount, &currency
			}
			out = append(out, dto)
		}
		WriteJSON(w, http.StatusOK, map[string]any{"events": out})
	}
}

type createHoldingEventRequest struct {
	Kind               string `json:"kind"`
	Quantity           string `json:"quantity"`
	AmountMinor        int64  `json:"amountMinor"`
	PrimaryAmountMinor *int64 `json:"primaryAmountMinor"`
	OccurredOn         string `json:"occurredOn"`
	Note               string `json:"note"`
}

func handleCreateHoldingEvent(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		holdingID := chi.URLParam(r, "id")
		var req createHoldingEventRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		kind, err := domain.ParseHoldingEventKind(strings.TrimSpace(req.Kind))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		// The string-to-nano conversion lives here, at the edge, so nothing
		// above this layer ever handles a quantity as text.
		quantity, err := domain.ParseQuantity(req.Quantity)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		occurredOn, ok := parseHoldingDate(w, r, req.OccurredOn)
		if !ok {
			return
		}
		// Read for the holding's currency: an event doesn't state its own and
		// is denominated in the holding's by construction. The service
		// re-reads and re-validates, so this read only builds the request.
		holding, err := deps.Holdings.Get(r.Context(), scope.HouseholdID, holdingID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		amount, err := domain.NewMoney(req.AmountMinor, holding.Currency)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		event := domain.HoldingEvent{
			HoldingID: holdingID, HouseholdID: scope.HouseholdID, Kind: kind,
			Quantity: quantity, Amount: amount,
			OccurredOn: occurredOn, Note: req.Note,
		}
		if req.PrimaryAmountMinor != nil {
			household, err := deps.Households.Get(r.Context(), scope.HouseholdID)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			primary, err := domain.NewMoney(*req.PrimaryAmountMinor, household.PrimaryCurrency)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			event.PrimaryAmount = &primary
		}
		if _, err := deps.Holdings.RecordEvent(r.Context(), event, scope.Today); err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeOneHolding(w, r, deps, scope.HouseholdID, holdingID, http.StatusCreated)
	}
}

func handleDeleteHoldingEvent(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		holdingID := chi.URLParam(r, "id")
		if err := deps.Holdings.DeleteEvent(r.Context(), scope.HouseholdID, holdingID, chi.URLParam(r, "eventId")); err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeOneHolding(w, r, deps, scope.HouseholdID, holdingID, http.StatusOK)
	}
}

func handleListHoldingValuations(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		valuations, err := deps.Holdings.ListValuations(r.Context(), scope.HouseholdID, chi.URLParam(r, "id"))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		out := make([]holdingValuationDTO, 0, len(valuations))
		for _, v := range valuations {
			dto := holdingValuationDTO{
				ID: v.ID, UnitPriceMinor: v.UnitPrice.Amount, Currency: v.UnitPrice.Currency,
				AsOf: v.AsOf.Format(holdingDateLayout), Note: v.Note,
			}
			if v.PrimaryUnitPrice != nil {
				amount, currency := v.PrimaryUnitPrice.Amount, v.PrimaryUnitPrice.Currency
				dto.PrimaryUnitPriceMinor, dto.PrimaryCurrency = &amount, &currency
			}
			out = append(out, dto)
		}
		WriteJSON(w, http.StatusOK, map[string]any{"valuations": out})
	}
}

type createValuationRequest struct {
	UnitPriceMinor        int64  `json:"unitPriceMinor"`
	PrimaryUnitPriceMinor *int64 `json:"primaryUnitPriceMinor"`
	AsOf                  string `json:"asOf"`
	Note                  string `json:"note"`
}

// Recording a valuation answers 200, never 201: one price per holding per day
// means a second write for the same date replaces the first instead of
// creating anything, and "Created" for a correction would mislead the frontend.
func handleCreateHoldingValuation(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		holdingID := chi.URLParam(r, "id")
		var req createValuationRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		asOf, ok := parseHoldingDate(w, r, req.AsOf)
		if !ok {
			return
		}
		holding, err := deps.Holdings.Get(r.Context(), scope.HouseholdID, holdingID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		price, err := domain.NewMoney(req.UnitPriceMinor, holding.Currency)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		valuation := domain.Valuation{
			HoldingID: holdingID, HouseholdID: scope.HouseholdID,
			UnitPrice: price, AsOf: asOf, Note: req.Note,
		}
		if req.PrimaryUnitPriceMinor != nil {
			household, err := deps.Households.Get(r.Context(), scope.HouseholdID)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			primary, err := domain.NewMoney(*req.PrimaryUnitPriceMinor, household.PrimaryCurrency)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			valuation.PrimaryUnitPrice = &primary
		}
		if _, err := deps.Holdings.RecordValuation(r.Context(), valuation, scope.Today); err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeOneHolding(w, r, deps, scope.HouseholdID, holdingID, http.StatusOK)
	}
}

func parseHoldingDate(w http.ResponseWriter, r *http.Request, text string) (time.Time, bool) {
	return parseTimeOrRefuse(w, strings.TrimSpace(text), holdingDateLayout,
		http.StatusUnprocessableEntity, "INVALID_DATE", "Enter a date as YYYY-MM-DD.")
}

// --- income and the period report -------------------------------------------

type holdingIncomeDTO struct {
	ID                 string  `json:"id"`
	Kind               string  `json:"kind"`
	AmountMinor        int64   `json:"amountMinor"`
	Currency           string  `json:"currency"`
	PrimaryAmountMinor *int64  `json:"primaryAmountMinor"`
	PrimaryCurrency    *string `json:"primaryCurrency"`
	ReceivedOn         string  `json:"receivedOn"`
	Note               string  `json:"note"`
}

func toHoldingIncomeDTO(i domain.HoldingIncome) holdingIncomeDTO {
	dto := holdingIncomeDTO{
		ID:          i.ID,
		Kind:        string(i.Kind),
		AmountMinor: i.Amount.Amount,
		Currency:    i.Amount.Currency,
		ReceivedOn:  i.ReceivedOn.Format(holdingDateLayout),
		Note:        i.Note,
	}
	if i.PrimaryAmount != nil {
		amount, currency := i.PrimaryAmount.Amount, i.PrimaryAmount.Currency
		dto.PrimaryAmountMinor, dto.PrimaryCurrency = &amount, &currency
	}
	return dto
}

type createIncomeRequest struct {
	Kind               string `json:"kind"`
	AmountMinor        int64  `json:"amountMinor"`
	PrimaryAmountMinor *int64 `json:"primaryAmountMinor"`
	ReceivedOn         string `json:"receivedOn"`
	Note               string `json:"note"`
}

func handleListHoldingIncome(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		rows, err := deps.Holdings.ListIncome(r.Context(), scope.HouseholdID, chi.URLParam(r, "id"))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		out := make([]holdingIncomeDTO, 0, len(rows))
		for _, row := range rows {
			out = append(out, toHoldingIncomeDTO(row))
		}
		WriteJSON(w, http.StatusOK, map[string]any{"income": out})
	}
}

func handleCreateHoldingIncome(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		holdingID := chi.URLParam(r, "id")
		var req createIncomeRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		receivedOn, ok := parseHoldingDate(w, r, req.ReceivedOn)
		if !ok {
			return
		}
		kind, err := domain.ParseIncomeKind(req.Kind)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		holding, err := deps.Holdings.Get(r.Context(), scope.HouseholdID, holdingID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		// The amount is denominated in the HOLDING's currency, never in one the
		// request chose. A dividend arrives in whatever the holding pays in.
		amount, err := domain.NewMoney(req.AmountMinor, holding.Currency)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		income := domain.HoldingIncome{
			HoldingID: holdingID, HouseholdID: scope.HouseholdID, Kind: kind,
			Amount: amount, ReceivedOn: receivedOn, Note: req.Note,
		}
		if req.PrimaryAmountMinor != nil {
			household, err := deps.Households.Get(r.Context(), scope.HouseholdID)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			primary, err := domain.NewMoney(*req.PrimaryAmountMinor, household.PrimaryCurrency)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			income.PrimaryAmount = &primary
		}
		created, err := deps.Holdings.RecordIncome(r.Context(), income, scope.Today)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusCreated, map[string]any{"income": toHoldingIncomeDTO(created)})
	}
}

func handleDeleteHoldingIncome(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		err := deps.Holdings.DeleteIncome(r.Context(), scope.HouseholdID,
			chi.URLParam(r, "id"), chi.URLParam(r, "incomeId"))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// componentDTO is one figure in both currencies: the primary side answers
// "did this make us richer", and the native side beside it lets the owner
// tell a good pick from a favourable exchange rate.
type componentDTO struct {
	NativeMinor  int64 `json:"nativeMinor"`
	PrimaryMinor int64 `json:"primaryMinor"`
}

func toComponentDTO(c domain.ReturnComponent) componentDTO {
	return componentDTO{NativeMinor: c.Native.Amount, PrimaryMinor: c.Primary.Amount}
}

func toComponentPointer(c *domain.ReturnComponent) *componentDTO {
	if c == nil {
		return nil
	}
	dto := toComponentDTO(*c)
	return &dto
}

// periodReturnDTO is one holding's figures for one period.
//
// Unrealised and total are null, not zero, when they can't be known, and
// `reason` says which price was missing -- the screen must render that
// reason, never a zero: an unpriced quarter is unknowable, not flat. Realised,
// income and fees are always present since no price is involved in them.
type periodReturnDTO struct {
	Unrealised *componentDTO `json:"unrealised"`
	Realised   componentDTO  `json:"realised"`
	Income     componentDTO  `json:"income"`
	Fees       componentDTO  `json:"fees"`
	Total      *componentDTO `json:"total"`
	Reason     string        `json:"reason"`

	// Dates, not a "we have a price" boolean: a screen that only knows a price
	// exists can't say how stale it is, and staleness is this feature's top
	// product risk. Null means no price was consulted -- holding nothing at
	// that end.
	OpeningPriceAsOf *string `json:"openingPriceAsOf"`
	ClosingPriceAsOf *string `json:"closingPriceAsOf"`
}

type reportPeriodDTO struct {
	Kind  string `json:"kind"`
	Year  int    `json:"year"`
	Index int    `json:"index"`
	Label string `json:"label"`
	Start string `json:"start"`
	End   string `json:"end"`
	// Current marks the period the household is still living in, which the
	// screen labels "to date" rather than presenting as a closed result.
	Current bool `json:"current"`
}

type reportHoldingDTO struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	AccountName string            `json:"accountName"`
	Instrument  string            `json:"instrument"`
	Unit        string            `json:"unit"`
	Currency    string            `json:"currency"`
	Archived    bool              `json:"archived"`
	Returns     []periodReturnDTO `json:"returns"`
}

// reportResponse carries the periods once, with every holding's figures
// aligned to them by position. A chart reads the two together by index
// rather than matching labels, so a holding with a gap can't shift its bars.
type reportResponse struct {
	Kind            string             `json:"kind"`
	PrimaryCurrency string             `json:"primaryCurrency"`
	Periods         []reportPeriodDTO  `json:"periods"`
	Holdings        []reportHoldingDTO `json:"holdings"`
}

// defaultReportPeriods is how far back each kind looks when the request
// doesn't say. It lives here, not in the frontend, so there's no second copy
// of the rule to drift -- and because these numbers are chosen against the
// chart's own bar budget.
var defaultReportPeriods = map[domain.PeriodKind]int{
	domain.PeriodQuarter: 6,
	domain.PeriodHalf:    4,
	domain.PeriodYear:    3,
}

func handleHoldingReport(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		kind, err := domain.ParsePeriodKind(r.URL.Query().Get("kind"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_PERIOD_KIND",
				"Ask for a quarter, a half or a year.", nil)
			return
		}
		count := defaultReportPeriods[kind]
		if raw := r.URL.Query().Get("count"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				WriteError(w, http.StatusBadRequest, "INVALID_PERIOD_COUNT",
					"How many periods? Give a number.", nil)
				return
			}
			count = parsed
		}

		view, err := deps.Holdings.Report(r.Context(), scope.HouseholdID, kind, count, scope.Today)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		periods := make([]reportPeriodDTO, 0, len(view.Periods))
		for _, p := range view.Periods {
			periods = append(periods, reportPeriodDTO{
				Kind:    string(p.Kind()),
				Year:    p.Year(),
				Index:   p.Index(),
				Label:   p.Label(),
				Start:   p.Start().Format(holdingDateLayout),
				End:     p.End().Format(holdingDateLayout),
				Current: p.IsCurrent(scope.Today),
			})
		}

		holdings := make([]reportHoldingDTO, 0, len(view.Holdings))
		for _, row := range view.Holdings {
			returns := make([]periodReturnDTO, 0, len(row.Returns))
			for _, ret := range row.Returns {
				returns = append(returns, periodReturnDTO{
					Unrealised:       toComponentPointer(ret.Unrealised),
					Realised:         toComponentDTO(ret.Realised),
					Income:           toComponentDTO(ret.Income),
					Fees:             toComponentDTO(ret.Fees),
					Total:            toComponentPointer(ret.Total),
					Reason:           string(ret.Reason),
					OpeningPriceAsOf: formatHoldingDate(ret.OpeningPriceAsOf),
					ClosingPriceAsOf: formatHoldingDate(ret.ClosingPriceAsOf),
				})
			}
			holdings = append(holdings, reportHoldingDTO{
				ID:          row.Holding.ID,
				Name:        row.Holding.Name,
				AccountName: row.AccountName,
				Instrument:  string(row.Holding.Instrument),
				Unit:        row.Holding.Unit,
				Currency:    row.Holding.Currency,
				Archived:    row.Holding.IsArchived(),
				Returns:     returns,
			})
		}

		WriteJSON(w, http.StatusOK, reportResponse{
			Kind:            string(kind),
			PrimaryCurrency: view.PrimaryCurrency,
			Periods:         periods,
			Holdings:        holdings,
		})
	}
}

func formatHoldingDate(t *time.Time) *string {
	if t == nil {
		return nil
	}
	out := t.Format(holdingDateLayout)
	return &out
}
