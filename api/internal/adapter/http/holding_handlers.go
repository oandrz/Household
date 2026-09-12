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
// Quantity crosses the wire as BOTH a string and its nano integer, and that is
// deliberate. The string is what a screen renders; the integer is what a
// caller that does exact arithmetic reads. The browser must never divide the
// integer by 1e9 to get the string -- that is float64 arithmetic on a figure a
// money screen shows, and docs/LEARNING.md records what it costs
// (333333 * 0.3 === 99999.90000000001 in JavaScript). Sending both keeps the
// division on this side, in integers.
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

	// HasMarketValue false means NO figure, not a figure of zero. A holding
	// nobody has priced is unknowable, not worthless, and the screen shows the
	// reason instead of a number -- the same rule the net worth card follows
	// when a primary-currency change strands an account. ValuedAt is null in
	// that case, and otherwise says how stale the price is.
	MarketValueMinor int64   `json:"marketValueMinor"`
	HasMarketValue   bool    `json:"hasMarketValue"`
	ValuedAt         *string `json:"valuedAt"`

	// The same value in the household's own currency, present only when the
	// holding is not already in it. Null means "this holding is already in
	// your currency", not "we could not work it out" -- so the screen shows
	// one figure rather than two identical ones.
	PrimaryMarketValueMinor *int64  `json:"primaryMarketValueMinor"`
	PrimaryCurrency         *string `json:"primaryCurrency"`
}

// portfolioResponse carries NotInNetWorth as a literal wire-level fact rather
// than letting the page hard-code it. Milestone 1 deliberately keeps holdings
// out of net worth and the twelve-month trend, and the page says so on its
// face; when milestone 3 changes that, this flag changes with the server and
// the label follows.
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

// Archive and restore are their own routes rather than a field on PATCH, the
// same reasoning accounts, categories and goals follow: if archiving were
// patchable, an ordinary rename that happened to include the field would
// archive the holding as a side effect of saving a name.
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

// writeOneHolding re-reads the WHOLE portfolio so a write answers with the same
// shape a read does, derived figures included.
//
// The cost is deliberate and worth stating, because it is not obvious: every
// write here issues three queries (all holdings, all events, all valuations)
// and folds every position, to return one row. Recording ten lots therefore
// costs ten full portfolio reads. At a household's scale -- single-digit
// holdings, tens of events -- that is free, and the alternative is a write
// response whose figures are computed differently from a read's, which is how
// the two drift apart.
//
// It stops being free somewhere around a household with hundreds of events,
// where the answer is a per-holding read rather than a portfolio one. Until
// then the simplicity is worth more than the queries. A write's own return value
// carries the stored row but not the fold or the price -- the same reason
// writeGoal re-reads rather than converting what Create handed back.
func writeOneHolding(w http.ResponseWriter, r *http.Request, deps Deps, householdID, holdingID string, status int) {
	// includeArchived is true unconditionally here, and that matters: archiving
	// answers with the holding it just archived, and folding it as if it were
	// live is what stops that response claiming the position was always empty.
	// An archived holding still held what it held.
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
// when the colliding holding turns out to be ARCHIVED: it carries that
// holding's id, so the screen offers Restore rather than a dead end. Archived
// rows still occupy their name (archived_at is not part of the unique key), so
// without this, re-adding something the household archived last year is a
// refusal with no way forward -- and the household cannot see the row that is
// blocking them.
//
// The precedent and the shape are goal_handlers.go's writeGoalNameConflict. As
// there, a failure to look the archived row up falls back to the plain error
// rather than replacing one problem with another.
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
		// The holding is read for its currency, which an event carries but
		// does not state: an event is denominated in its holding's currency by
		// construction. The service re-reads and re-validates, so this read is
		// for building the request, not for trusting.
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
		if _, err := deps.Holdings.RecordEvent(r.Context(), event, deps.Clock.Now()); err != nil {
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
// means a second write for the same date replaces the first rather than
// creating anything. Saying "Created" for a correction would be a lie the
// frontend could act on.
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
		if _, err := deps.Holdings.RecordValuation(r.Context(), valuation, deps.Clock.Now()); err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeOneHolding(w, r, deps, scope.HouseholdID, holdingID, http.StatusOK)
	}
}

func parseHoldingDate(w http.ResponseWriter, r *http.Request, text string) (time.Time, bool) {
	d, err := time.Parse(holdingDateLayout, strings.TrimSpace(text))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_DATE", "Enter a date as YYYY-MM-DD.", nil)
		return time.Time{}, false
	}
	return d, true
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
		created, err := deps.Holdings.RecordIncome(r.Context(), income, deps.Clock.Now())
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

// componentDTO is one figure in both currencies. The primary one is the
// household's own and is what answers "did this make us richer"; the native one
// sits beside it so the owner can still tell whether the PICK was good and the
// exchange rate was the problem.
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
// unrealised and total are NULL rather than zero when they cannot be known,
// and `reason` says which price was missing. A screen must render the reason,
// never a zero: a quarter nobody priced is unknowable, not flat. realised,
// income and fees are always present -- no price is involved in them, so a
// missing valuation cannot take them away.
type periodReturnDTO struct {
	Unrealised *componentDTO `json:"unrealised"`
	Realised   componentDTO  `json:"realised"`
	Income     componentDTO  `json:"income"`
	Fees       componentDTO  `json:"fees"`
	Total      *componentDTO `json:"total"`
	Reason     string        `json:"reason"`

	// The days each end was measured at, as dates rather than as a "we have a
	// price" boolean: a screen that only knows a price EXISTS cannot say how
	// stale it is, and stale valuations are this feature's top product risk.
	// Null means no price was consulted, which is what holding nothing at that
	// end means.
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

// reportResponse carries the periods once and every holding's figures aligned
// to them by POSITION. A chart reads the two together by index rather than
// matching labels, which is also what stops a holding with a gap in its
// history shifting its own bars.
type reportResponse struct {
	Kind            string             `json:"kind"`
	PrimaryCurrency string             `json:"primaryCurrency"`
	Periods         []reportPeriodDTO  `json:"periods"`
	Holdings        []reportHoldingDTO `json:"holdings"`
}

// defaultReportPeriods is how far back each kind looks when the request does
// not say. It lives HERE rather than in the frontend because a default in the
// browser would be a second copy of the window rule, free to drift from this
// one -- and because the chart's own bar budget is what these numbers are
// chosen against.
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

		view, err := deps.Holdings.Report(r.Context(), scope.HouseholdID, kind, count, deps.Clock.Now())
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
				Current: p.IsCurrent(deps.Clock.Now()),
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
