package httpadapter

import (
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// occurredOnLayout is the wire format for a transaction's date: a calendar
// date, because "18 July" is a fact about a day. Same layout and same reason
// as openingBalanceLayout in account_handlers.go.
const occurredOnLayout = "2006-01-02"

// monthLayout is the wire format for the month filter.
const monthLayout = "2006-01"

// defaultPageSize and maxPageSize mirror TransactionRepository.List's own
// constants (see its doc comment in usecase/ports.go): the repository
// defaults an unset or non-positive limit to 50 and clamps anything above 200
// down to it. The handler clamps here too, before the filter is ever built, so
// filter.Limit always holds the limit the repository will *actually* use.
// Without this, a caller asking for limit=500 would get back at most 201
// rows (200 clamped by the repository, +1 peek), but this handler's own trim
// check (len(views) > filter.Limit) would compare that 201 against the
// unclamped 500, never trim the peek row, and never set nextCursor -- silently
// serving a truncated "last" page that looks complete.
const (
	defaultPageSize = 50
	maxPageSize     = 200
)

// uuidPattern is the canonical 8-4-4-4-12 hex form every id in this system is
// generated in. Filters below use it to fail closed on a malformed id at the
// HTTP boundary -- the same defence in depth TransactionRepository.List
// already applies one layer down (Task 8): that repository turns a malformed
// account_id, category_id or paid_by into an empty result rather than an
// error, because a nullable-filter query cannot tell "bad id" apart from "no
// filter". A caller who mistyped an id deserves a 422 telling them so, not a
// silently empty page that reads exactly like "nothing matched."
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func isValidUUID(s string) bool {
	return uuidPattern.MatchString(s)
}

type transactionDTO struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	OccurredOn  string `json:"occurredOn"`
	Description string `json:"description"`

	CategoryID   *string `json:"categoryId"`
	CategoryName *string `json:"categoryName"`

	PaidByMembershipID *string `json:"paidByMembershipId"`
	PaidByName         *string `json:"paidByName"`

	FromAccountID   *string `json:"fromAccountId"`
	FromAccountName *string `json:"fromAccountName"`
	ToAccountID     *string `json:"toAccountId"`
	ToAccountName   *string `json:"toAccountName"`

	Amount         moneyDTO  `json:"amount"`
	ReceivedAmount *moneyDTO `json:"receivedAmount"`

	// Two flags, not one: a transfer has two accounts with two opening dates
	// and can predate one without predating the other. null means there is no
	// account on that side at all.
	BeforeFromAccountOpeningBalance *bool `json:"beforeFromAccountOpeningBalance"`
	BeforeToAccountOpeningBalance   *bool `json:"beforeToAccountOpeningBalance"`
}

type monthSummaryDTO struct {
	Currency       string                   `json:"currency"`
	Month          string                   `json:"month"`
	Count          int                      `json:"count"`
	SpentMinor     int64                    `json:"spentMinor"`
	ExcludedNoRate []excludedTransactionDTO `json:"excludedNoRate"`
}

type excludedTransactionDTO struct {
	TransactionID string `json:"transactionId"`
	Currency      string `json:"currency"`
}

type transactionsResponse struct {
	Transactions []transactionDTO `json:"transactions"`
	// null on the last page. That is what the "Load older transactions" link
	// hides itself on -- a row count would be wrong on a page that happens to
	// be exactly full.
	NextCursor *string         `json:"nextCursor"`
	Summary    monthSummaryDTO `json:"summary"`
}

// handleListTransactions serves the ledger and the two figures above it
// together, because they are one screen and must describe the same month.
func handleListTransactions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Every transactions route sits behind requireCapability and
		// requireOwner (router.go), so by the time this runs the caller is an
		// owner holding money and the explicit check other handlers make would
		// be dead code. Unlike accounts, there is no redaction branch here at
		// all: a limited member never reaches this handler.
		scope, _ := RequestScope(r)

		filter, month, ok := parseTransactionFilter(w, r)
		if !ok {
			return
		}

		views, err := deps.Transactions.List(r.Context(), scope.HouseholdID, filter)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		summary, err := deps.Transactions.MonthSummary(r.Context(), scope.HouseholdID, month)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		out := transactionsResponse{
			Transactions: make([]transactionDTO, 0, len(views)),
			Summary:      toMonthSummaryDTO(summary),
		}
		// The repository returns limit+1 rows so we can tell there is another
		// page without counting the table. The extra row is the cursor, not
		// content -- returning it would show one row twice. filter.Limit is
		// already the effective limit (defaulted and clamped in
		// parseTransactionFilter), so it agrees with what the repository
		// actually used to build views.
		limit := filter.Limit
		if len(views) > limit {
			last := views[limit-1]
			cursor := encodeCursor(last.Transaction.OccurredOn, last.Transaction.ID)
			out.NextCursor = &cursor
			views = views[:limit]
		}
		for _, v := range views {
			out.Transactions = append(out.Transactions, toTransactionDTO(v))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// encodeCursor and decodeCursor are the opaque page marker: the date and id of
// the last row of a page, which is exactly what the keyset predicate needs.
// Opaque to the frontend on purpose -- it must not construct one, or a change
// to the sort order becomes a breaking change to the client.
func encodeCursor(occurredOn time.Time, id string) string {
	return occurredOn.Format(occurredOnLayout) + ":" + id
}

func decodeCursor(raw string) (time.Time, string, bool) {
	// A date is exactly ten characters, so the split point is fixed and an id
	// containing a colon cannot confuse it.
	if len(raw) < len(occurredOnLayout)+2 || raw[len(occurredOnLayout)] != ':' {
		return time.Time{}, "", false
	}
	date, err := time.Parse(occurredOnLayout, raw[:len(occurredOnLayout)])
	if err != nil {
		return time.Time{}, "", false
	}
	id := raw[len(occurredOnLayout)+1:]
	// The id half needs its own check. Left to SQL, a garbage id arrives as
	// NULL inside `(occurred_on, id) < (date, NULL)`, and that does not
	// return zero rows: row comparison stops at the first unequal pair, so
	// every transaction dated strictly before the cursor date still matches
	// and only the ones dated ON it drop out. The page would look almost
	// right, which is worse than looking empty -- a caller paging through the
	// ledger would silently lose one day's rows and see the rest repeated.
	// Refusing the cursor here, with a 422 that names the problem, is what
	// stops that. TransactionRepository.List carries the matching fail-closed
	// guard for the same value, so neither layer depends on the other.
	if !isValidUUID(id) {
		return time.Time{}, "", false
	}
	return date, id, true
}

// parseTransactionFilter reads the design's five filters plus paging. It
// answers the month separately because the summary is always about a month
// even when the ledger is not filtered to one -- an unfiltered ledger still
// shows "247 in July".
func parseTransactionFilter(w http.ResponseWriter, r *http.Request) (usecase.TransactionFilter, time.Time, bool) {
	q := r.URL.Query()
	filter := usecase.TransactionFilter{
		Kind:  q.Get("kind"),
		Limit: defaultPageSize,
	}

	// account_id, category_id and paid_by are refused at 422 when malformed,
	// rather than passed down to become a silently empty page. Task 8 made
	// TransactionRepository.List fail closed on a bad id for exactly this
	// filter set; this is the second line of defence in front of it, and the
	// one that can actually tell the caller what was wrong.
	if raw := q.Get("account_id"); raw != "" {
		if !isValidUUID(raw) {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_ACCOUNT_FILTER",
				"That account id could not be read.", nil)
			return usecase.TransactionFilter{}, time.Time{}, false
		}
		filter.AccountID = raw
	}
	if raw := q.Get("category_id"); raw != "" {
		if !isValidUUID(raw) {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_CATEGORY_FILTER",
				"That category id could not be read.", nil)
			return usecase.TransactionFilter{}, time.Time{}, false
		}
		filter.CategoryID = raw
	}
	if raw := q.Get("paid_by"); raw != "" {
		if !isValidUUID(raw) {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_PAID_BY_FILTER",
				"That member id could not be read.", nil)
			return usecase.TransactionFilter{}, time.Time{}, false
		}
		filter.PaidByMembershipID = raw
	}

	month := time.Now().UTC()
	if raw := q.Get("month"); raw != "" {
		parsed, err := time.Parse(monthLayout, raw)
		if err != nil {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_MONTH",
				"That month could not be read. Use YYYY-MM.", nil)
			return usecase.TransactionFilter{}, time.Time{}, false
		}
		month = parsed
		filter.Month = parsed
	}

	if raw := q.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_LIMIT",
				"Limit must be a positive whole number.", nil)
			return usecase.TransactionFilter{}, time.Time{}, false
		}
		// Clamped here, not just left for the repository to clamp: this
		// handler's own trim logic below reads filter.Limit to decide where
		// the peek row is, so it must already equal the limit the repository
		// will actually apply.
		if parsed > maxPageSize {
			parsed = maxPageSize
		}
		filter.Limit = parsed
	}

	if raw := q.Get("cursor"); raw != "" {
		date, id, ok := decodeCursor(raw)
		if !ok {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_CURSOR",
				"That page marker could not be read.", nil)
			return usecase.TransactionFilter{}, time.Time{}, false
		}
		filter.CursorDate, filter.CursorID = date, id
	}
	return filter, month, true
}

func toTransactionDTO(v usecase.TransactionView) transactionDTO {
	t := v.Transaction
	dto := transactionDTO{
		ID:          t.ID,
		Kind:        string(t.Kind),
		OccurredOn:  t.OccurredOn.Format(occurredOnLayout),
		Description: t.Description,
		Amount:      moneyDTO{AmountMinor: t.Amount.Amount, Currency: t.Amount.Currency},

		BeforeFromAccountOpeningBalance: v.BeforeFromAccountOpening,
		BeforeToAccountOpeningBalance:   v.BeforeToAccountOpening,
	}
	// "" means the column is NULL, and the wire form of absent is JSON null --
	// not an empty string, which would read as a real id that happens to be
	// blank. Same convention as accountDTO.OwnerMembershipID.
	if t.CategoryID != "" {
		id, name := t.CategoryID, v.CategoryName
		dto.CategoryID, dto.CategoryName = &id, &name
	}
	if t.PaidByMembershipID != "" {
		id, name := t.PaidByMembershipID, v.PaidByName
		dto.PaidByMembershipID, dto.PaidByName = &id, &name
	}
	if t.FromAccountID != "" {
		id, name := t.FromAccountID, v.FromAccountName
		dto.FromAccountID, dto.FromAccountName = &id, &name
	}
	if t.ToAccountID != "" {
		id, name := t.ToAccountID, v.ToAccountName
		dto.ToAccountID, dto.ToAccountName = &id, &name
	}
	if t.ReceivedAmount != nil {
		dto.ReceivedAmount = &moneyDTO{
			AmountMinor: t.ReceivedAmount.Amount,
			Currency:    t.ReceivedAmount.Currency,
		}
	}
	return dto
}

func toMonthSummaryDTO(s usecase.MonthSummary) monthSummaryDTO {
	dto := monthSummaryDTO{
		Currency:       s.Currency,
		Month:          s.Month.Format(monthLayout),
		Count:          s.Count,
		SpentMinor:     s.Spent.Amount,
		ExcludedNoRate: make([]excludedTransactionDTO, 0, len(s.ExcludedNoRate)),
	}
	for _, ex := range s.ExcludedNoRate {
		dto.ExcludedNoRate = append(dto.ExcludedNoRate, excludedTransactionDTO{
			TransactionID: ex.TransactionID, Currency: ex.Currency,
		})
	}
	return dto
}

// createTransactionRequest carries no currency, deliberately: a transaction is
// denominated in its account's currency and the service derives it. A field
// here that the service overwrote would be a field this handler accepts and
// never persists -- the shape guarding-partial-writes exists for.
type createTransactionRequest struct {
	Kind                string  `json:"kind"`
	OccurredOn          string  `json:"occurredOn"`
	Description         string  `json:"description"`
	CategoryID          *string `json:"categoryId"`
	PaidByMembershipID  *string `json:"paidByMembershipId"`
	FromAccountID       *string `json:"fromAccountId"`
	ToAccountID         *string `json:"toAccountId"`
	AmountMinor         int64   `json:"amountMinor"`
	ReceivedAmountMinor *int64  `json:"receivedAmountMinor"`
}

// updateTransactionRequest's fields are all pointers so a field the caller did
// not name reaches usecase.TransactionUpdate as nil and keeps its stored
// value -- the same real-patch convention TestUpdateHouseholdIsARealPatch
// pins.
//
// clearReceivedAmount is how a transfer that stops crossing currencies loses
// the figure that no longer applies: with pointers alone, "remove it" and
// "leave it" are the same nil.
type updateTransactionRequest struct {
	Kind                *string `json:"kind"`
	OccurredOn          *string `json:"occurredOn"`
	Description         *string `json:"description"`
	CategoryID          *string `json:"categoryId"`
	PaidByMembershipID  *string `json:"paidByMembershipId"`
	FromAccountID       *string `json:"fromAccountId"`
	ToAccountID         *string `json:"toAccountId"`
	AmountMinor         *int64  `json:"amountMinor"`
	ReceivedAmountMinor *int64  `json:"receivedAmountMinor"`
	ClearReceivedAmount bool    `json:"clearReceivedAmount"`
}

func handleCreateTransaction(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req createTransactionRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		occurredOn, err := time.Parse(occurredOnLayout, req.OccurredOn)
		if err != nil {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_DATE",
				"That date could not be read. Use YYYY-MM-DD.", nil)
			return
		}

		in := usecase.NewTransaction{
			HouseholdID:         scope.HouseholdID,
			Kind:                req.Kind,
			OccurredOn:          occurredOn,
			Description:         req.Description,
			AmountMinor:         req.AmountMinor,
			ReceivedAmountMinor: req.ReceivedAmountMinor,
		}
		if req.CategoryID != nil {
			in.CategoryID = *req.CategoryID
		}
		if req.PaidByMembershipID != nil {
			in.PaidByMembershipID = *req.PaidByMembershipID
		}
		if req.FromAccountID != nil {
			in.FromAccountID = *req.FromAccountID
		}
		if req.ToAccountID != nil {
			in.ToAccountID = *req.ToAccountID
		}

		created, err := deps.Transactions.Create(r.Context(), in)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeTransaction(w, r, deps, scope.HouseholdID, created.ID, http.StatusCreated)
	}
}

func handleUpdateTransaction(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req updateTransactionRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		patch := usecase.TransactionUpdate{
			Kind:                req.Kind,
			Description:         req.Description,
			CategoryID:          req.CategoryID,
			PaidByMembershipID:  req.PaidByMembershipID,
			FromAccountID:       req.FromAccountID,
			ToAccountID:         req.ToAccountID,
			AmountMinor:         req.AmountMinor,
			ReceivedAmountMinor: req.ReceivedAmountMinor,
			ClearReceivedAmount: req.ClearReceivedAmount,
		}
		if req.OccurredOn != nil {
			occurredOn, err := time.Parse(occurredOnLayout, *req.OccurredOn)
			if err != nil {
				WriteError(w, http.StatusUnprocessableEntity, "INVALID_DATE",
					"That date could not be read. Use YYYY-MM-DD.", nil)
				return
			}
			patch.OccurredOn = &occurredOn
		}

		id := chi.URLParam(r, "id")
		if _, err := deps.Transactions.Update(r.Context(), scope.HouseholdID, id, patch); err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeTransaction(w, r, deps, scope.HouseholdID, id, http.StatusOK)
	}
}

// handleDeleteTransaction answers 204 with no body -- the one status in this
// API permitted to carry none, and permitted because apiFetch does not try to
// parse it. A transaction is hard deleted: nothing references it, so nothing
// is orphaned. See the spec's decision 8 for why this differs from accounts.
func handleDeleteTransaction(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		if err := deps.Transactions.Delete(r.Context(), scope.HouseholdID, chi.URLParam(r, "id")); err != nil {
			MapDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// writeTransaction re-reads through Get so every mutating response carries the
// joined names the write queries do not return -- the same reason
// writeAccount does it.
func writeTransaction(w http.ResponseWriter, r *http.Request, deps Deps, householdID, id string, status int) {
	view, err := deps.Transactions.Get(r.Context(), householdID, id)
	if err != nil {
		MapDomainError(w, r, err)
		return
	}
	WriteJSON(w, status, toTransactionDTO(view))
}
