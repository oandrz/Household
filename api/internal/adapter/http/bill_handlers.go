package httpadapter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// billDTO is one row on the Bills screen: the stored bill plus the derived
// figures BillService.List (and every write) computes -- Overdue, DueSoon
// and Settled. AmountMinor and Currency are the BILL's own -- the pay-from
// account's -- never the household's primary (BillView's own comment): a
// row for an IDR account renders in IDR.
//
// Settled must never be dropped from this DTO. A settled one-off (paid, no
// next occurrence) belongs to neither Due soon nor Later by their own
// 30-day/overdue definitions -- both require a non-null NextDue -- yet it is
// still counted in the summary's BillCount and autopay count. Without this
// field the frontend has no way to place such a bill anywhere on the page,
// even though the header above it still counts it: a feature no screen can
// reach, the exact gap Task 7's own review found in an earlier sketch of
// this DTO.
type billDTO struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	AmountMinor        int64   `json:"amountMinor"`
	Currency           string  `json:"currency"`
	Cadence            string  `json:"cadence"`
	NextDue            *string `json:"nextDue"`
	CategoryID         string  `json:"categoryId"`
	CategoryName       string  `json:"categoryName"`
	PayFromAccountID   string  `json:"payFromAccountId"`
	AccountName        string  `json:"accountName"`
	PaidByMembershipID string  `json:"paidByMembershipId"`
	Autopay            bool    `json:"autopay"`
	IsSubscription     bool    `json:"isSubscription"`
	Overdue            bool    `json:"overdue"`
	DueSoon            bool    `json:"dueSoon"`
	Settled            bool    `json:"settled"`
	ArchivedAt         *string `json:"archivedAt"`
}

// billResponse is the {"bill": ...} shape every single-bill write route
// answers -- Create, Update, Archive and Restore alike -- mirroring
// goalResponse and categoryResponse's own wrapper convention.
type billResponse struct {
	Bill billDTO `json:"bill"`
}

// nextDueBillDTO is billsSummaryDTO's "what's coming up next" card, the
// Bills screen's own version of goalsSummaryDTO's nextGoalDTO. It is nil --
// serialised as JSON null, the pointer itself is the signal -- whenever the
// household has no live bill carrying a next_due at all, mirroring
// BillsSummary.NextDueOn's own "nil is the field to gate on" rule. Its
// AmountMinor/Currency are deliberately the bill's OWN currency, never
// converted to the household's primary -- BillsSummary's own comment on
// NextDueAmount explains why: converting only this figure would leave the
// card pairing an amount with a currency symbol that disagrees with it.
type nextDueBillDTO struct {
	BillID      string `json:"billId"`
	BillName    string `json:"billName"`
	DueOn       string `json:"dueOn"`
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
	Overdue     bool   `json:"overdue"`
	Autopay     bool   `json:"autopay"`
}

// billsSummaryDTO is the page header and the three stat cards. Currency,
// DueThisMonthMinor, PaidSoFarMinor, SubscriptionsMonthlyMinor and
// SubscriptionsAnnualMinor are all in the household's primary currency --
// BillsSummary's own comment -- unlike nextDueBillDTO's own figures above.
type billsSummaryDTO struct {
	Currency                  string          `json:"currency"`
	DueThisMonthMinor         int64           `json:"dueThisMonthMinor"`
	PaidSoFarMinor            int64           `json:"paidSoFarMinor"`
	NextDue                   *nextDueBillDTO `json:"nextDue"`
	AutopayCount              int             `json:"autopayCount"`
	BillCount                 int             `json:"billCount"`
	SubscriptionsMonthlyMinor int64           `json:"subscriptionsMonthlyMinor"`
	SubscriptionsAnnualMinor  int64           `json:"subscriptionsAnnualMinor"`
	ExcludedNoRate            int             `json:"excludedNoRate"`
}

// billPaymentDTO is one row of "Paid this month" -- BillPaymentView
// unwrapped at this layer, the same way billDTO unwraps BillView. BillID
// (alongside ID, the payment's own) is what Task 10's undo route
// (DELETE /bills/{id}/payments/{paymentId}) needs to build its URL.
type billPaymentDTO struct {
	ID          string `json:"id"`
	BillID      string `json:"billId"`
	BillName    string `json:"billName"`
	DueOn       string `json:"dueOn"`
	PaidOn      string `json:"paidOn"`
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
	Autopay     bool   `json:"autopay"`
}

// billsResponse is GET /bills' whole body: every row, the paid-this-month
// list and the page summary, composed by one call to BillService.List. The
// frontend splits Bills into Due soon and Later on each row's own DueSoon
// flag rather than recomputing the 30-day window in TypeScript -- BillView's
// own comment on why that rule lives in exactly one place.
type billsResponse struct {
	Bills         []billDTO        `json:"bills"`
	PaidThisMonth []billPaymentDTO `json:"paidThisMonth"`
	Summary       billsSummaryDTO  `json:"summary"`
}

// createBillRequest is POST /bills' body. NextDue is a required string, not
// a pointer: a bill can only ever arrive at NextDue == nil by being settled
// through MarkPaid (BillView.Settled's own comment) -- there is no way to
// CREATE one already in that state, so Create leaves no way to omit it.
type createBillRequest struct {
	Name               string `json:"name"`
	AmountMinor        int64  `json:"amountMinor"`
	Cadence            string `json:"cadence"`
	NextDue            string `json:"nextDue"`
	CategoryID         string `json:"categoryId"`
	PayFromAccountID   string `json:"payFromAccountId"`
	PaidByMembershipID string `json:"paidByMembershipId"`
	Autopay            bool   `json:"autopay"`
	IsSubscription     bool   `json:"isSubscription"`
}

// updateBillRequest is PATCH /bills/{id}'s body. Every field is a pointer so
// an absent key round-trips as "unchanged" (usecase.BillPatch's own
// convention) rather than a stored zero. ClearCategory and ClearPayer mirror
// BillPatch's own explicit-clear fields -- the same reason
// updateGoalRequest carries no bare "unset" via nil: a nil pointer already
// means "unchanged", so it cannot also mean "clear".
type updateBillRequest struct {
	Name               *string `json:"name"`
	AmountMinor        *int64  `json:"amountMinor"`
	Cadence            *string `json:"cadence"`
	NextDue            *string `json:"nextDue"`
	CategoryID         *string `json:"categoryId"`
	ClearCategory      bool    `json:"clearCategory"`
	PayFromAccountID   *string `json:"payFromAccountId"`
	PaidByMembershipID *string `json:"paidByMembershipId"`
	ClearPayer         bool    `json:"clearPayer"`
	Autopay            *bool   `json:"autopay"`
	IsSubscription     *bool   `json:"isSubscription"`
}

// parseBillDueDate parses a "YYYY-MM-DD" body field the same way
// occurredOnLayout already reads a transaction's or a goal contribution's
// own date -- a bill's next_due is a calendar date, not an instant, the same
// reasoning openingBalanceLayout documents for accounts.
func parseBillDueDate(w http.ResponseWriter, raw string) (time.Time, bool) {
	t, err := time.Parse(occurredOnLayout, raw)
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "INVALID_DATE", "That date could not be read. Use YYYY-MM-DD.", nil)
		return time.Time{}, false
	}
	return t, true
}

// handleListBills serves the whole Bills screen: every row (live only by
// default, live-and-archived together when include_archived=true -- a
// union, not a filter swap, BillService.List's own contract) plus the page
// summary, which counts live bills only either way. include_archived is
// spelled the way account_handlers.go and goal_handlers.go already spell
// the same parameter, so every list route on this API agrees.
func handleListBills(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		includeArchived := r.URL.Query().Get("include_archived") == "true"

		view, err := deps.Bills.List(r.Context(), scope.HouseholdID, includeArchived, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, toBillsResponse(view))
	}
}

// handleCreateBill adds one bill. Unlike Goals' Create, no re-read is
// needed to build the response: BillService.Create already returns the
// complete BillView -- CategoryName and AccountName joined, Overdue/DueSoon/
// Settled computed -- in the one call (BillRecord's own comment on why every
// method that returns one populates the joined names directly), so writeBill
// below just wraps what Create already handed back.
func handleCreateBill(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req createBillRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		today := deps.Clock.Now()

		nextDue, ok := parseBillDueDate(w, req.NextDue)
		if !ok {
			return
		}

		created, err := deps.Bills.Create(r.Context(), usecase.NewBill{
			HouseholdID:        scope.HouseholdID,
			Name:               req.Name,
			AmountMinor:        req.AmountMinor,
			Cadence:            domain.Cadence(req.Cadence),
			NextDue:            nextDue,
			CategoryID:         req.CategoryID,
			PayFromAccountID:   req.PayFromAccountID,
			PaidByMembershipID: req.PaidByMembershipID,
			Autopay:            req.Autopay,
			IsSubscription:     req.IsSubscription,
		}, today)
		if err != nil {
			// ErrForbidden is Create's own "that pay-from account is
			// archived" refusal (BillService.Create's own comment) -- the
			// ONLY reason this sentinel can reach this handler at all, so
			// the message can safely name it without checking anything
			// further. MapDomainError's shared ErrForbidden case answers a
			// bare 403 with no detail; that case exists for a caller this
			// codebase has not needed yet, and widening it here would be
			// guessing at what every OTHER future caller wants this
			// message to say. Update carries no equivalent check (see the
			// task report), so this interception belongs only here.
			if errors.Is(err, domain.ErrForbidden) {
				WriteError(w, http.StatusUnprocessableEntity, "ACCOUNT_ARCHIVED",
					"That account is archived and cannot be used to pay a bill.", nil)
				return
			}
			writeBillWriteError(w, r, deps, scope.HouseholdID, req.Name, today, err)
			return
		}
		writeBill(w, created, http.StatusCreated)
	}
}

// handleUpdateBill applies a patch. A currency mismatch (patch names a
// payFromAccountId whose account currency differs from the bill's current
// one) needs its own interception before the shared write-error path:
// BillService.Update's own doc comment says naming both currencies in the
// message is deliberately this layer's job, and that needs a lookup
// (the bill's own current currency, plus the target account's) that
// writeBillWriteError below is never given.
func handleUpdateBill(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req updateBillRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		id := chi.URLParam(r, "id")
		today := deps.Clock.Now()

		patch := usecase.BillPatch{
			Name:               req.Name,
			AmountMinor:        req.AmountMinor,
			CategoryID:         req.CategoryID,
			ClearCategory:      req.ClearCategory,
			PayFromAccountID:   req.PayFromAccountID,
			PaidByMembershipID: req.PaidByMembershipID,
			ClearPayer:         req.ClearPayer,
			Autopay:            req.Autopay,
			IsSubscription:     req.IsSubscription,
		}
		if req.Cadence != nil {
			c := domain.Cadence(*req.Cadence)
			patch.Cadence = &c
		}
		if req.NextDue != nil {
			nextDue, ok := parseBillDueDate(w, *req.NextDue)
			if !ok {
				return
			}
			patch.NextDue = &nextDue
		}

		attemptedName := ""
		if req.Name != nil {
			attemptedName = *req.Name
		}

		updated, err := deps.Bills.Update(r.Context(), scope.HouseholdID, id, patch, today)
		if err != nil {
			if errors.Is(err, domain.ErrBillCurrencyImmutable) && req.PayFromAccountID != nil {
				writeBillCurrencyMismatch(w, r, deps, scope.HouseholdID, id, *req.PayFromAccountID, today)
				return
			}
			writeBillWriteError(w, r, deps, scope.HouseholdID, attemptedName, today, err)
			return
		}
		writeBill(w, updated, http.StatusOK)
	}
}

func handleArchiveBill(deps Deps) http.HandlerFunc { return setBillArchived(deps, true) }
func handleRestoreBill(deps Deps) http.HandlerFunc { return setBillArchived(deps, false) }

// setBillArchived backs both the archive and the restore route -- the same
// "one function, not two near-identical ones" shape account_handlers.go's
// setArchived and goal_handlers.go's setGoalArchived both use. Archive and
// restore are their own routes rather than a field on PATCH: an ordinary
// rename that happened to include it would archive the bill as a side
// effect of saving a name, the reason router.go's own comment gives for
// accounts, categories and goals.
//
// No re-read is needed here either, for the same reason handleCreateBill's
// own comment gives: BillRepository.SetArchived already returns the full
// record (its own doc comment in ports.go), specifically so this handler
// never needs a second Get to build its response.
func setBillArchived(deps Deps, archived bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		id := chi.URLParam(r, "id")
		today := deps.Clock.Now()
		view, err := deps.Bills.SetArchived(r.Context(), scope.HouseholdID, id, archived, today)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeBill(w, view, http.StatusOK)
	}
}

// writeBill answers a Create/Update/SetArchived call with the BillView it
// already returned, wrapped in the {"bill": ...} shape every write route on
// this API answers. status is a parameter because Create answers 201 while
// Update, Archive and Restore all answer 200 -- edits to a row that already
// existed, not the creation of one, the same reasoning writeAccount's own
// comment gives for varying by caller.
func writeBill(w http.ResponseWriter, view usecase.BillView, status int) {
	WriteJSON(w, status, billResponse{Bill: toBillDTO(view)})
}

// writeBillWriteError is Create and Update's shared failure path, once each
// has already handled whatever needs context this function is not given
// (Create's own ErrForbidden interception above; Update's own
// ErrBillCurrencyImmutable interception below).
//
// ErrBillNameTaken gets writeGoalNameConflict's own richer-409 treatment:
// look for an archived bill holding the same name, and if one exists, offer
// its id so the modal can propose Restore instead of a dead end (Task 7's
// own review named this exact gap for goals; the same fix applies here,
// since an archived bill still occupies its name -- 00008_bills.sql's own
// comment). A live-name collision, or a failure of the lookup itself, falls
// back to MapDomainError's own BILL_NAME_TAKEN case.
func writeBillWriteError(w http.ResponseWriter, r *http.Request, deps Deps, householdID, attemptedName string, today time.Time, err error) {
	if !errors.Is(err, domain.ErrBillNameTaken) {
		MapDomainError(w, r, err)
		return
	}
	if views, listErr := listBillViews(r.Context(), deps, householdID, today); listErr == nil {
		if archived, ok := findArchivedBillByName(views, attemptedName); ok {
			WriteError(w, http.StatusConflict, "BILL_NAME_TAKEN",
				fmt.Sprintf("%q is the name of an archived bill. Restore it, or choose a different name.",
					strings.TrimSpace(attemptedName)),
				map[string]any{"archivedBillId": archived.Bill.ID})
			return
		}
	}
	MapDomainError(w, r, err)
}

// writeBillCurrencyMismatch answers Update's ErrBillCurrencyImmutable with a
// message naming both currencies -- the bill's own stored one (read back via
// listBillViews, since BillService exposes no bare Get -- List's own
// live-and-archived union is every lookup this file needs, the same reason
// goal_handlers.go has none either) and the target account's (via
// deps.Accounts.Get, the AccountService already wired into Deps).
func writeBillCurrencyMismatch(w http.ResponseWriter, r *http.Request, deps Deps, householdID, billID, targetAccountID string, today time.Time) {
	views, err := listBillViews(r.Context(), deps, householdID, today)
	if err != nil {
		MapDomainError(w, r, err)
		return
	}
	current, found := findBillViewByID(views, billID)
	if !found {
		// Update already succeeded in finding this bill (it read it before
		// failing on the currency check) -- a miss on this immediate re-read
		// is this handler's own invariant broken, not a client mistake.
		logAndWriteInternal(w, r, fmt.Errorf("bill %s not found on the re-read after a currency-mismatch refusal", billID))
		return
	}
	acct, err := deps.Accounts.Get(r.Context(), householdID, targetAccountID)
	if err != nil {
		MapDomainError(w, r, err)
		return
	}
	WriteError(w, http.StatusUnprocessableEntity, "BILL_CURRENCY_IMMUTABLE",
		fmt.Sprintf("This bill is in %s; that account is in %s. A bill's currency cannot be changed after it is created.",
			current.Bill.Amount.Currency, acct.Balance.Currency),
		map[string]any{"billCurrency": current.Bill.Amount.Currency, "accountCurrency": acct.Balance.Currency})
}

// listBillViews is BillService.List's live-and-archived union, stripped
// down to the slice callers in this file want -- the identical shape
// listGoalViews already uses for goals, and for the same reason: no service
// here exposes a single-bill getter, so this is the one lookup every helper
// that needs to inspect a specific existing bill shares.
func listBillViews(ctx context.Context, deps Deps, householdID string, today time.Time) ([]usecase.BillView, error) {
	view, err := deps.Bills.List(ctx, householdID, true, today)
	if err != nil {
		return nil, err
	}
	return view.Bills, nil
}

func findBillViewByID(views []usecase.BillView, id string) (usecase.BillView, bool) {
	for _, v := range views {
		if v.Bill.ID == id {
			return v, true
		}
	}
	return usecase.BillView{}, false
}

// findArchivedBillByName is writeBillWriteError's lookup: among every bill
// this collision could be against, the one whose exact (trimmed) name
// matches and which is archived -- the row a Restore action could actually
// bring back. Mirrors findArchivedGoalByName exactly.
func findArchivedBillByName(views []usecase.BillView, name string) (usecase.BillView, bool) {
	trimmed := strings.TrimSpace(name)
	for _, v := range views {
		if v.Bill.Name == trimmed && v.Bill.IsArchived() {
			return v, true
		}
	}
	return usecase.BillView{}, false
}

func toBillDTO(v usecase.BillView) billDTO {
	b := v.Bill
	dto := billDTO{
		ID:                 b.ID,
		Name:               b.Name,
		AmountMinor:        b.Amount.Amount,
		Currency:           b.Amount.Currency,
		Cadence:            string(b.Cadence),
		CategoryID:         b.CategoryID,
		CategoryName:       v.CategoryName,
		PayFromAccountID:   b.PayFromAccountID,
		AccountName:        v.AccountName,
		PaidByMembershipID: b.PaidByMembershipID,
		Autopay:            b.Autopay,
		IsSubscription:     b.IsSubscription,
		Overdue:            v.Overdue,
		DueSoon:            v.DueSoon,
		Settled:            v.Settled,
	}
	if b.NextDue != nil {
		next := b.NextDue.Format(occurredOnLayout)
		dto.NextDue = &next
	}
	if b.ArchivedAt != nil {
		archived := b.ArchivedAt.Format(time.RFC3339)
		dto.ArchivedAt = &archived
	}
	return dto
}

func toBillPaymentDTO(v usecase.BillPaymentView) billPaymentDTO {
	p := v.Payment
	return billPaymentDTO{
		ID:          p.ID,
		BillID:      p.BillID,
		BillName:    v.BillName,
		DueOn:       p.DueOn.Format(occurredOnLayout),
		PaidOn:      p.PaidOn.Format(occurredOnLayout),
		AmountMinor: p.Amount.Amount,
		Currency:    p.Amount.Currency,
		Autopay:     v.Autopay,
	}
}

func toBillsSummaryDTO(s usecase.BillsSummary) billsSummaryDTO {
	dto := billsSummaryDTO{
		Currency:                  s.Currency,
		DueThisMonthMinor:         s.DueThisMonth.Amount,
		PaidSoFarMinor:            s.PaidSoFar.Amount,
		AutopayCount:              s.AutopayCount,
		BillCount:                 s.BillCount,
		SubscriptionsMonthlyMinor: s.SubscriptionsMonthly.Amount,
		SubscriptionsAnnualMinor:  s.SubscriptionsAnnual.Amount,
		ExcludedNoRate:            s.ExcludedNoRate,
	}
	// NextDueOn == nil is the field to gate on -- BillsSummary's own comment
	// -- not any field inside nextDueBillDTO, several of which (Overdue,
	// Autopay) are legitimately false for a real next-due bill too.
	if s.NextDueOn != nil {
		dto.NextDue = &nextDueBillDTO{
			BillID:      s.NextDueBillID,
			BillName:    s.NextDueBillName,
			DueOn:       s.NextDueOn.Format(occurredOnLayout),
			AmountMinor: s.NextDueAmount.Amount,
			Currency:    s.NextDueAmount.Currency,
			Overdue:     s.NextDueOverdue,
			Autopay:     s.NextDueAutopay,
		}
	}
	return dto
}

func toBillsResponse(view usecase.BillsView) billsResponse {
	out := billsResponse{
		Bills:         make([]billDTO, 0, len(view.Bills)),
		PaidThisMonth: make([]billPaymentDTO, 0, len(view.PaidThisMonth)),
		Summary:       toBillsSummaryDTO(view.Summary),
	}
	for _, b := range view.Bills {
		out.Bills = append(out.Bills, toBillDTO(b))
	}
	for _, p := range view.PaidThisMonth {
		out.PaidThisMonth = append(out.PaidThisMonth, toBillPaymentDTO(p))
	}
	return out
}
