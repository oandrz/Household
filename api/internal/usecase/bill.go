package usecase

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// BillView is one row on the screen: the stored bill plus the derived
// figures. Amount is in the BILL's own currency, not the household's
// primary -- only the summary totals below convert (see GoalView's comment).
type BillView struct {
	Bill         domain.Bill
	CategoryName string
	AccountName  string
	Overdue      bool
	// DueSoon is true when the bill belongs above the "Later" heading:
	// overdue, or due within 30 days inclusive. Computed here rather than in
	// the frontend so the rule lives in exactly one place.
	DueSoon bool
	// Settled is true for a live bill with no next occurrence -- a paid
	// one-off. Both "Due soon" and "Later" require a non-NULL next_due, so
	// a bill like this fits neither, and without this flag the frontend
	// can't tell "far out" from "done" -- both read NextDue nil. It stays
	// visible rather than being dropped: 00008_bills.sql says a settled
	// one-off is deliberately not auto-archived, so Settled is how it
	// renders without being miscategorised.
	Settled bool
}

// BillPaymentView is one row of "Paid this month", unwrapped from
// ListPayments' BillPaymentRecord like BillView unwraps BillRecord. Nothing
// here is derived -- a settled payment has no overdue/due-soon to compute.
type BillPaymentView struct {
	Payment  domain.BillPayment
	BillName string
	Autopay  bool
}

// BillsSummary is the page header and the three stat cards. DueThisMonth,
// PaidSoFar, SubscriptionsMonthly and SubscriptionsAnnual are all in the
// household's primary currency; a bill with no rate to primary is excluded
// from all of them and counted in ExcludedNoRate instead of silently
// dropped.
//
// NextDueAmount does NOT convert -- it stays in the next-due bill's own
// currency, the way BillView's Amount does, so the card never pairs an
// amount with a mismatched currency symbol. It is zero Money{} when there is
// no next-due bill; gate on NextDueOn == nil, not on a zero Money (see
// TestNextDueIsOmittedWhenThereIsNone).
type BillsSummary struct {
	Currency             string
	DueThisMonth         domain.Money
	PaidSoFar            domain.Money
	NextDueBillID        string
	NextDueBillName      string
	NextDueOn            *time.Time
	NextDueAmount        domain.Money
	NextDueOverdue       bool
	NextDueAutopay       bool
	AutopayCount         int
	BillCount            int
	SubscriptionsMonthly domain.Money
	SubscriptionsAnnual  domain.Money
	ExcludedNoRate       int
}

// BillsView is the whole Bills screen in one response: every row, the paid
// list and the page summary, composed by one call to List.
type BillsView struct {
	Bills         []BillView
	PaidThisMonth []BillPaymentView
	Summary       BillsSummary
}

// NewBill is Create's input. It carries no DueAnchorDay: Create derives the
// anchor from NextDue.Day() itself, the same pattern GoalUpdate's missing
// Currency field follows.
type NewBill struct {
	HouseholdID        string
	Name               string
	AmountMinor        int64
	Cadence            domain.Cadence
	NextDue            time.Time
	CategoryID         string
	PayFromAccountID   string
	PaidByMembershipID string
	Autopay            bool
	IsSubscription     bool
}

// BillPatch is a PATCH: a nil field is unchanged. There is no ArchivedAt
// field -- archive and restore are their own routes, so a rename cannot
// archive a bill as a side effect.
//
// ClearCategory and ClearPayer are the explicit-clear convention
// (clearReceivedAmount's, on transactions): a nil pointer already means
// "unchanged", so it cannot also mean "clear".
type BillPatch struct {
	Name               *string
	AmountMinor        *int64
	Cadence            *domain.Cadence
	NextDue            *time.Time
	CategoryID         *string
	ClearCategory      bool
	PayFromAccountID   *string
	PaidByMembershipID *string
	ClearPayer         bool
	Autopay            *bool
	IsSubscription     *bool
}

// MarkPayment is MarkPaid's input. AmountMinor is optional: nil pays the
// bill's own stored amount, a value is this payment's own figure -- a
// utility bill varies month to month, and paying one instance never changes
// the bill's own standing amount.
//
// The default is decided by MarkPaid, not by each caller, so the CLI and the
// Telegram bot can reuse the same rule instead of recomputing it themselves.
type MarkPayment struct {
	HouseholdID string
	BillID      string
	AmountMinor *int64
	PaidOn      time.Time
}

// BillDeps gathers every port BillService needs, mirroring GoalDeps. There
// is no Clock: every method takes the date it needs as a parameter (today,
// at), so nothing here reads the wall clock and every test is deterministic.
//
// Categories is the same narrow CategoryLookup TransactionDeps carries:
// MarkPaid writes a real expense carrying the bill's own category_id, so an
// income category would produce spend Budget's buildCategoryViews skips
// entirely. A bill's category is validated at Create and Update, not at the
// point it is spent.
type BillDeps struct {
	Bills      BillRepository
	Households HouseholdRepository
	FX         FXRateProvider
	Accounts   AccountLookup
	Categories CategoryLookup
}

// BillService composes the Bills screen and every write against it. Like
// every other service here it takes no actor parameter: services enforce
// what is *valid*, middleware enforces who is *asking* -- the money
// capability and the owner check live in the router.
type BillService struct {
	deps BillDeps
}

func NewBillService(deps BillDeps) *BillService {
	return &BillService{deps: deps}
}

// List composes the whole Bills screen for one household: every row (each
// carrying Overdue/DueSoon), the paid-this-month list and the page summary,
// in four repository calls regardless of how many bills or payments exist.
// today is always a parameter -- see BillDeps' own comment -- so every
// figure is deterministic in tests and driven by the clock port in
// production.
//
// ExcludedNoRate counts once per BILL, even one that would otherwise touch
// two totals, plus once per PAYMENT not already counted that way -- a
// payment can be a distinct no-rate fact from its bill's current state.
//
// DueThisMonth and PaidSoFar sum from Bills.MonthTotals' own
// currency-aggregated maps, which cannot say which bill or payment a
// currency's contribution came from -- so ExcludedNoRate is recovered
// separately, by walking `records` and `paymentRecords` directly (see
// sumConvertible for why the no-rate count is not taken again there).
func (s *BillService) List(ctx context.Context, householdID string, includeArchived bool, today time.Time) (BillsView, error) {
	household, err := s.deps.Households.Get(ctx, householdID)
	if err != nil {
		return BillsView{}, err
	}
	primary := household.PrimaryCurrency
	conv := NewConverter(s.deps.FX, primary)

	records, err := s.deps.Bills.List(ctx, householdID, includeArchived)
	if err != nil {
		return BillsView{}, err
	}
	dueMinor, paidMinor, err := s.deps.Bills.MonthTotals(ctx, householdID, today)
	if err != nil {
		return BillsView{}, err
	}
	paymentRecords, err := s.deps.Bills.ListPayments(ctx, householdID, today)
	if err != nil {
		return BillsView{}, err
	}

	zero, err := domain.NewMoney(0, primary)
	if err != nil {
		return BillsView{}, err
	}
	subscriptionsAnnual := zero
	excludedNoRate := 0
	// excludedBillIDs stops the per-bill and per-payment passes below from
	// counting the same bill twice -- a bill that is both due this month and
	// a ticked subscription is one no-rate fact, not two.
	excludedBillIDs := map[string]bool{}

	// views is built with make(..., 0, ...), never left nil: a household
	// with no bills must still serialise Bills as JSON [], not null (the
	// GoalService.List precedent for the same reason).
	views := make([]BillView, 0, len(records))
	var next nextDueBill
	var autopayCount, billCount int

	for _, rec := range records {
		view := s.toView(rec, today)
		views = append(views, view)
		b := rec.Bill

		if b.IsArchived() {
			// The row still renders (above); the summary never counts an
			// archived bill, in any figure -- the GoalsSummary precedent.
			continue
		}
		billCount++
		if b.Autopay {
			autopayCount++
		}
		next.consider(b, view.Overdue)

		// excludedThisBill covers both totals this bill might touch below.
		// The due-this-month probe only recovers per-bill identity for the
		// count -- dueMinor further down already excludes a no-rate currency.
		excludedThisBill := false
		if b.NextDue != nil && dueInMonthOf(*b.NextDue, today) {
			_, hasRate, convErr := conv.TryConvert(ctx, b.Amount)
			if convErr != nil {
				return BillsView{}, convErr
			}
			if !hasRate {
				excludedThisBill = true
			}
		}

		if b.IsSubscription {
			var noRate bool
			subscriptionsAnnual, noRate, err = s.addSubscriptionAnnual(ctx, conv, subscriptionsAnnual, b)
			if err != nil {
				return BillsView{}, err
			}
			if noRate {
				excludedThisBill = true
			}
		}

		if excludedThisBill {
			excludedNoRate++
			excludedBillIDs[b.ID] = true
		}
	}

	excludedPayments, err := s.countExcludedPayments(ctx, conv, paymentRecords, excludedBillIDs)
	if err != nil {
		return BillsView{}, err
	}
	excludedNoRate += excludedPayments

	dueTotal, err := s.sumConvertible(ctx, conv, dueMinor, zero)
	if err != nil {
		return BillsView{}, err
	}
	paidSoFarTotal, err := s.sumConvertible(ctx, conv, paidMinor, zero)
	if err != nil {
		return BillsView{}, err
	}

	sortBillViews(views)

	paidViews := make([]BillPaymentView, 0, len(paymentRecords))
	for _, p := range paymentRecords {
		paidViews = append(paidViews, BillPaymentView(p))
	}

	// Integer-first, one division: subscriptionsAnnual is already the sum of
	// every bill's own annual equivalent, converted then added -- the only
	// division in the whole rollup happens here, exactly once.
	subscriptionsMonthly := domain.Money{Amount: subscriptionsAnnual.Amount / 12, Currency: primary}

	return BillsView{
		Bills:         views,
		PaidThisMonth: paidViews,
		Summary: BillsSummary{
			Currency:             primary,
			DueThisMonth:         dueTotal,
			PaidSoFar:            paidSoFarTotal,
			NextDueBillID:        next.id,
			NextDueBillName:      next.name,
			NextDueOn:            next.on,
			NextDueAmount:        next.amount,
			NextDueOverdue:       next.overdue,
			NextDueAutopay:       next.autopay,
			AutopayCount:         autopayCount,
			BillCount:            billCount,
			SubscriptionsMonthly: subscriptionsMonthly,
			SubscriptionsAnnual:  subscriptionsAnnual,
			ExcludedNoRate:       excludedNoRate,
		},
	}, nil
}

// nextDueBill is the summary's "next due" bill as List walks the live bills:
// the earliest NextDue wins, and a tie goes to the name that sorts first. A
// bill with no NextDue is never a candidate.
type nextDueBill struct {
	id, name         string
	on               *time.Time
	amount           domain.Money
	overdue, autopay bool
}

func (n *nextDueBill) consider(b domain.Bill, overdue bool) {
	if b.NextDue == nil {
		return
	}
	candidate := *b.NextDue
	if n.on == nil || candidate.Before(*n.on) ||
		(candidate.Equal(*n.on) && b.Name < n.name) {
		n.id, n.name = b.ID, b.Name
		n.on = &candidate
		n.amount = b.Amount
		n.overdue = overdue
		n.autopay = b.Autopay
	}
}

// dueInMonthOf reports whether due falls in today's calendar month, after
// both are normalised through billStartOfDay -- Bills.MonthTotals scopes its
// month in UTC, and an unconverted today would compare against the caller's
// own zone instead (see billStartOfDay's own comment).
func dueInMonthOf(due, today time.Time) bool {
	c, t := billStartOfDay(due), billStartOfDay(today)
	return c.Year() == t.Year() && c.Month() == t.Month()
}

// addSubscriptionAnnual adds one subscription's annual equivalent, converted
// into primary, to total. noRate is true when the bill's currency has no
// rate -- nothing is added, and List counts it in ExcludedNoRate. A one-off
// is not a recurring cost: total comes back unchanged and noRate false. Any
// Converter.TryConvert error is returned.
func (s *BillService) addSubscriptionAnnual(ctx context.Context, conv *Converter, total domain.Money, b domain.Bill) (sum domain.Money, noRate bool, err error) {
	annual, ok := domain.AnnualEquivalentMinor(b.Cadence, b.Amount.Amount)
	if !ok {
		return total, false, nil
	}
	converted, hasRate, convErr := conv.TryConvert(ctx, domain.Money{Amount: annual, Currency: b.Amount.Currency})
	if convErr != nil {
		return domain.Money{}, false, convErr
	}
	if !hasRate {
		return total, true, nil
	}
	sum, err = total.Add(converted)
	if err != nil {
		return domain.Money{}, false, err
	}
	return sum, false, nil
}

// countExcludedPayments counts this month's payments whose currency has no
// rate. A payment is a distinct entity from the bill that generated it, so
// it gets its own count here unless that bill was already counted in
// List's per-bill pass -- excludedBillIDs is updated as it goes. Any error
// from Converter.TryConvert is returned.
func (s *BillService) countExcludedPayments(ctx context.Context, conv *Converter, payments []BillPaymentRecord, excludedBillIDs map[string]bool) (int, error) {
	count := 0
	for _, p := range payments {
		if excludedBillIDs[p.Payment.BillID] {
			continue
		}
		_, hasRate, convErr := conv.TryConvert(ctx, p.Payment.Amount)
		if convErr != nil {
			return 0, convErr
		}
		if !hasRate {
			count++
			excludedBillIDs[p.Payment.BillID] = true
		}
	}
	return count, nil
}

// sumConvertible sums DueThisMonth and PaidSoFar from Bills.MonthTotals'
// aggregated map, one currency at a time, converted then added. A no-rate
// currency is skipped WITHOUT incrementing ExcludedNoRate: that count is
// already exact, from List's separate per-bill and per-payment passes (an
// aggregated map can't say which bill or payment it belongs to). Any
// Converter.TryConvert error is returned.
func (s *BillService) sumConvertible(ctx context.Context, conv *Converter, byCurrency map[string]int64, zero domain.Money) (domain.Money, error) {
	total := zero
	for currency, amount := range byCurrency {
		converted, hasRate, convErr := conv.TryConvert(ctx, domain.Money{Amount: amount, Currency: currency})
		if convErr != nil {
			return domain.Money{}, convErr
		}
		if !hasRate {
			continue
		}
		var err error
		total, err = total.Add(converted)
		if err != nil {
			return domain.Money{}, err
		}
	}
	return total, nil
}

// sortBillViews orders the Bills list ascending by due date, nil last, ties by
// name -- one order across the whole list rather than two separately-sorted
// Due-soon/Later slices: the frontend splits on each row's own DueSoon flag,
// so the order they arrive in is the order both halves render in.
func sortBillViews(views []BillView) {
	sort.SliceStable(views, func(i, j int) bool {
		a, b := views[i].Bill.NextDue, views[j].Bill.NextDue
		switch {
		case a == nil && b == nil:
			return views[i].Bill.Name < views[j].Bill.Name
		case a == nil:
			return false
		case b == nil:
			return true
		case !a.Equal(*b):
			return a.Before(*b)
		default:
			return views[i].Bill.Name < views[j].Bill.Name
		}
	})
}

// Create validates and writes a new bill. DueAnchorDay is derived from
// NextDue.Day() here, never accepted from a caller -- NewBill has no field
// for one, so it can never disagree with the bill's own first due date.
//
// today is a parameter because BillDeps carries no Clock: the returned
// BillView's Overdue and DueSoon are meaningless without it.
//
// A non-empty PaidByMembershipID outside this household is refused with
// domain.ErrAccountOwnerNotInHousehold, the same check AccountService.Create
// and TransactionService run for their own payer/owner fields. A CategoryID
// that is not this household's, or not an expense category, is refused with
// domain.ErrCategoryKindMismatch (see validateCategory's own comment).
func (s *BillService) Create(ctx context.Context, in NewBill, today time.Time) (BillView, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return BillView{}, domain.ErrBillNameRequired
	}
	if in.AmountMinor <= 0 {
		return BillView{}, domain.ErrBillAmountNotPositive
	}
	if err := domain.CheckAmountWithinLimit(in.AmountMinor); err != nil {
		return BillView{}, err
	}
	cadence, err := domain.ParseCadence(string(in.Cadence))
	if err != nil {
		return BillView{}, err
	}

	acct, err := s.deps.Accounts.Get(ctx, in.HouseholdID, in.PayFromAccountID)
	if err != nil {
		return BillView{}, err
	}
	if acct.Account.IsArchived() {
		return BillView{}, domain.ErrForbidden
	}

	// "" means unattributed (domain.Bill.PaidByMembershipID's own
	// convention) and is always valid, so this only runs for a
	// caller-supplied id. The error is shared with AccountService.Create's
	// identical check on OwnerMembershipID -- its wording ("that member is
	// not in this household") is generic enough to reuse.
	if in.PaidByMembershipID != "" {
		ok, err := s.deps.Accounts.MembershipBelongsToHousehold(ctx, in.HouseholdID, in.PaidByMembershipID)
		if err != nil {
			return BillView{}, err
		}
		if !ok {
			return BillView{}, domain.ErrAccountOwnerNotInHousehold
		}
	}

	if err := s.validateCategory(ctx, in.HouseholdID, in.CategoryID); err != nil {
		return BillView{}, err
	}

	rec, err := s.deps.Bills.Create(ctx, NewBillRow{
		HouseholdID:        in.HouseholdID,
		Name:               name,
		AmountMinor:        in.AmountMinor,
		Cadence:            cadence,
		NextDue:            in.NextDue,
		DueAnchorDay:       in.NextDue.Day(),
		CategoryID:         in.CategoryID,
		PayFromAccountID:   in.PayFromAccountID,
		PaidByMembershipID: in.PaidByMembershipID,
		Autopay:            in.Autopay,
		IsSubscription:     in.IsSubscription,
	})
	if err != nil {
		return BillView{}, err
	}
	return s.toView(rec, today), nil
}

// Update gets the stored bill, applies each non-nil field of patch onto it,
// and hands BillRepository.Update the complete result -- the port never
// merges (AccountRepository.Update and TransactionRepository.Update state
// the same rule for their own tables).
//
// DueAnchorDay is re-derived from the new NextDue whenever the patch moves
// it -- an explicit edit is the household choosing a new anchor. This is
// the mirror image of domain.NextDue's own mechanical rewind, which must
// NOT touch the anchor: that case is the bill advancing on its own cadence,
// this case is a person typing a new date into the form.
//
// Update refuses, before the write: a PayFromAccountID whose currency
// differs from the bill's current one -> domain.ErrBillCurrencyImmutable
// (Update only) -- a bill's amount is stored in its pay-from account's
// currency, so re-pointing across currencies would silently reinterpret
// every past figure. Naming both currencies in the message is the HTTP
// layer's job, not this one's.
//
// Mirroring Create's own checks: an ARCHIVED pay-from account ->
// domain.ErrForbidden (else the bill sits unpayable until the household
// hits the dead end at the pay button); a PaidByMembershipID outside this
// household -> domain.ErrAccountOwnerNotInHousehold; a CategoryID that is
// not this household's, or not an expense category ->
// domain.ErrCategoryKindMismatch. The last two are re-run here because a
// patch can name a different membership or category than Create validated.
//
// today is a parameter for the same reason Create's own comment gives.
func (s *BillService) Update(ctx context.Context, householdID, billID string, patch BillPatch, today time.Time) (BillView, error) {
	rec, err := s.deps.Bills.Get(ctx, householdID, billID)
	if err != nil {
		return BillView{}, err
	}
	b := rec.Bill

	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if name == "" {
			return BillView{}, domain.ErrBillNameRequired
		}
		b.Name = name
	}
	if patch.AmountMinor != nil {
		if *patch.AmountMinor <= 0 {
			return BillView{}, domain.ErrBillAmountNotPositive
		}
		if err := domain.CheckAmountWithinLimit(*patch.AmountMinor); err != nil {
			return BillView{}, err
		}
		b.Amount.Amount = *patch.AmountMinor
	}
	if patch.Cadence != nil {
		cadence, err := domain.ParseCadence(string(*patch.Cadence))
		if err != nil {
			return BillView{}, err
		}
		b.Cadence = cadence
	}
	if patch.NextDue != nil {
		nextDue := *patch.NextDue
		b.NextDue = &nextDue
		b.DueAnchorDay = nextDue.Day() // see this method's own comment
	}
	// ClearCategory wins over a nil CategoryID, the same convention
	// GoalUpdate.ClearTargetMonth uses: without it there is no way to tell
	// "leave alone" from "picked uncategorised" -- both arrive as nil.
	if patch.ClearCategory {
		b.CategoryID = ""
	} else if patch.CategoryID != nil {
		// Checked again here: a patch can name a DIFFERENT category than
		// the one Create validated. ClearCategory above needs no check --
		// "" is always valid (uncategorised).
		if err := s.validateCategory(ctx, householdID, *patch.CategoryID); err != nil {
			return BillView{}, err
		}
		b.CategoryID = *patch.CategoryID
	}
	if patch.PayFromAccountID != nil {
		acct, err := s.deps.Accounts.Get(ctx, householdID, *patch.PayFromAccountID)
		if err != nil {
			return BillView{}, err
		}
		if acct.Balance.Currency != b.Amount.Currency {
			return BillView{}, domain.ErrBillCurrencyImmutable
		}
		if acct.Account.IsArchived() {
			// Same refusal as Create -- see this method's own doc comment.
			return BillView{}, domain.ErrForbidden
		}
		b.PayFromAccountID = *patch.PayFromAccountID
	}
	if patch.ClearPayer {
		b.PaidByMembershipID = ""
	} else if patch.PaidByMembershipID != nil {
		// nil means "leave alone" (ClearPayer already handled "unset"), so
		// the check below only runs for a caller naming a real member --
		// checked again because Update's patch can name a DIFFERENT
		// membership than Create validated. An empty-but-non-nil pointer
		// still clears without a check.
		if *patch.PaidByMembershipID != "" {
			ok, err := s.deps.Accounts.MembershipBelongsToHousehold(ctx, householdID, *patch.PaidByMembershipID)
			if err != nil {
				return BillView{}, err
			}
			if !ok {
				return BillView{}, domain.ErrAccountOwnerNotInHousehold
			}
		}
		b.PaidByMembershipID = *patch.PaidByMembershipID
	}
	if patch.Autopay != nil {
		b.Autopay = *patch.Autopay
	}
	if patch.IsSubscription != nil {
		b.IsSubscription = *patch.IsSubscription
	}

	updated, err := s.deps.Bills.Update(ctx, b)
	if err != nil {
		return BillView{}, err
	}
	return s.toView(updated, today), nil
}

// SetArchived archives or restores a bill, stamping ArchivedAt with at --
// the same caller-supplied convention AccountRepository.SetArchived and
// GoalRepository.SetArchived use. BillRepository.SetArchived already
// returns the full record, so no second Get is needed. at also doubles as
// "today" for the returned view's Overdue/DueSoon, since BillDeps carries
// no Clock.
func (s *BillService) SetArchived(ctx context.Context, householdID, billID string, archived bool, at time.Time) (BillView, error) {
	rec, err := s.deps.Bills.SetArchived(ctx, householdID, billID, archived, at)
	if err != nil {
		return BillView{}, err
	}
	return s.toView(rec, at), nil
}

// MarkPaid writes the payment, the expense and the advanced due date,
// through BillRepository.RecordPayment's single transaction -- the seam
// where Bills writes into the ledger, feeding Budget's Spent, the daily
// pace, Spending by person and net worth. Getting the currency or date
// wrong here is wrong money on three other screens.
//
// The amount is the caller's when MarkPayment carries one, else the bill's
// own stored figure (see MarkPayment's own comment); either way the bill's
// amount_minor itself is left untouched by paying.
//
// A caller's amount must be positive, checked here rather than left to
// bill_payments' own CHECK constraint -- a raw constraint violation
// surfacing as a 500 is not an acceptable answer to a bad request. Checked
// before the Get below, same as Create's validate-input-first order. The
// bill's own amount needs no re-check: Create and Update already refuse a
// non-positive one.
//
// Three conditions refuse with *domain.BillNotPayableError, not a bare
// domain.ErrForbidden: an archived bill, a settled one-off, and an archived
// pay-from account each mean something different to the household. Reason
// is what the HTTP layer switches on to answer each with its own message,
// without disturbing an errors.Is(err, domain.ErrForbidden) caller.
func (s *BillService) MarkPaid(ctx context.Context, in MarkPayment) (BillPaymentView, error) {
	if in.AmountMinor != nil && *in.AmountMinor <= 0 {
		return BillPaymentView{}, domain.ErrBillAmountNotPositive
	}
	if in.AmountMinor != nil {
		if err := domain.CheckAmountWithinLimit(*in.AmountMinor); err != nil {
			return BillPaymentView{}, err
		}
	}
	rec, err := s.deps.Bills.Get(ctx, in.HouseholdID, in.BillID)
	if err != nil {
		return BillPaymentView{}, err
	}
	if rec.Bill.IsArchived() {
		return BillPaymentView{}, &domain.BillNotPayableError{Reason: domain.BillArchived}
	}
	if rec.Bill.NextDue == nil {
		// A settled one-off has no occurrence left to pay -- nil here means
		// exactly that (Bill.NextDue's own comment), not "not yet loaded".
		return BillPaymentView{}, &domain.BillNotPayableError{Reason: domain.BillSettled}
	}
	amount := rec.Bill.Amount.Amount
	if in.AmountMinor != nil {
		amount = *in.AmountMinor
	}

	acct, err := s.deps.Accounts.Get(ctx, in.HouseholdID, rec.Bill.PayFromAccountID)
	if err != nil {
		return BillPaymentView{}, err
	}
	if acct.Account.IsArchived() {
		return BillPaymentView{}, &domain.BillNotPayableError{Reason: domain.PayFromAccountArchived}
	}
	// The expense's currency is the pay-from ACCOUNT's, never the bill's own
	// stored figure reinterpreted -- TransactionService.validate applies the
	// identical rule to every transaction, and a test asserts the two agree.
	currency := acct.Balance.Currency

	// dueOn is the occurrence being settled: the bill's CURRENT next_due, not
	// PaidOn. A bill due the 8th paid on the 11th still settles the 8th's
	// occurrence -- PaidOn only ever feeds the payment's own paid_on column
	// and, for a recurring bill, the advance below.
	dueOn := *rec.Bill.NextDue

	var next *time.Time
	// Advance from the DUE date, never PaidOn: paying three days late must
	// not shift the bill's day, or a year of late payments walks it a month
	// off (domain.NextDue's own comment). ok is false only for a one-off,
	// which settles with no next occurrence (PaymentWrite.NextDue stays nil).
	if n, ok := domain.NextDue(rec.Bill.Cadence, dueOn, rec.Bill.DueAnchorDay); ok {
		next = &n
	}

	pay, err := s.deps.Bills.RecordPayment(ctx, PaymentWrite{
		HouseholdID:        in.HouseholdID,
		BillID:             in.BillID,
		DueOn:              dueOn,
		PaidOn:             in.PaidOn,
		AmountMinor:        amount,
		Currency:           currency,
		Description:        rec.Bill.Name,
		CategoryID:         rec.Bill.CategoryID,
		PayFromAccountID:   rec.Bill.PayFromAccountID,
		PaidByMembershipID: rec.Bill.PaidByMembershipID,
		NextDue:            next,
	})
	if err != nil {
		return BillPaymentView{}, err
	}
	// Autopay comes from the bill already read, not from pay: RecordPayment
	// deliberately leaves Autopay false since its caller (this method)
	// already holds the flag -- joining it back would re-read what's in hand.
	return BillPaymentView{Payment: pay.Payment, BillName: pay.BillName, Autopay: rec.Bill.Autopay}, nil
}

// UndoPayment is a straight delegation. The repository owns the whole
// transaction -- deleting the payment, deleting its linked expense, rewinding
// next_due -- and owns the most-recent-only refusal (domain.ErrForbidden):
// this method neither swallows nor reinterprets whatever comes back.
func (s *BillService) UndoPayment(ctx context.Context, householdID, billID, paymentID string) error {
	return s.deps.Bills.UndoPayment(ctx, householdID, billID, paymentID)
}

// View is one bill exactly as List renders its row, archived included, for
// a caller that needs a single one -- a write handler answering with the
// row it just changed. One repository read, where List would cost four
// plus a summary nobody asked for; a missing bill is domain.ErrNotFound.
func (s *BillService) View(ctx context.Context, householdID, billID string, today time.Time) (BillView, error) {
	rec, err := s.deps.Bills.Get(ctx, householdID, billID)
	if err != nil {
		return BillView{}, err
	}
	return s.toView(rec, today), nil
}

// toView composes one BillView from a repository record, computing Overdue
// and DueSoon against today -- the one place every returning method shares,
// so List, View, Create, Update and SetArchived cannot drift on the rule.
func (s *BillService) toView(rec BillRecord, today time.Time) BillView {
	b := rec.Bill
	overdue := b.NextDue != nil && domain.IsOverdue(*b.NextDue, today)
	dueSoon := overdue
	if !dueSoon && b.NextDue != nil {
		// Day boundaries, not a raw duration: billStartOfDay strips both
		// times to UTC midnight first, the same normalisation domain.IsOverdue
		// applies on the Overdue line above. The two must agree -- a bill the
		// page calls overdue but not due soon is a contradiction users can see.
		dueSoon = billStartOfDay(*b.NextDue).Sub(billStartOfDay(today)) <= 30*24*time.Hour
	}
	return BillView{
		Bill:         b,
		CategoryName: rec.CategoryName,
		AccountName:  rec.AccountName,
		Overdue:      overdue,
		DueSoon:      dueSoon,
		// See BillView.Settled's own comment: a live bill with no next_due
		// (only possible once MarkPaid settles a one-off) is neither Due soon
		// nor Later.
		Settled: !b.IsArchived() && b.NextDue == nil,
	}
}

// billStartOfDay is midnight UTC for t, exactly as domain.startOfDay does
// it.
//
// This package also has signup.go's startOfDay, which deliberately does
// NOT convert, since the signup rate limit resets at the household's own
// local midnight; bills' dates are UTC calendar days instead. Using
// signup's version here would leave Overdue (which converts) and DueSoon
// (which wouldn't) disagreeing for eight hours a day in UTC+8 -- the
// read-a-date-in-its-own-Location family under docs/LEARNING.md pattern 1.
// Do not "simplify" this by pointing it back at signup.go's.
func billStartOfDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// validateCategory refuses a category that is not this household's, and one
// that is not an EXPENSE category -- the same two checks
// TransactionService.validateCategory runs, minus its choice between expense
// and income: a bill is always a payment to a company, so there is no kind
// here to branch on.
//
// Without this, an income category id could slip through from an API
// caller bypassing the modal's own filter: the expense MarkPaid later
// writes from it would land in Budget's Spent total while appearing in no
// category row at all (buildCategoryViews walks expense categories only).
// domain.ErrCategoryKindMismatch is reused because the HTTP layer already
// answers 422 INVALID_CATEGORY for it.
//
// "" is uncategorised, always valid -- the same convention
// PaidByMembershipID's own check above uses.
func (s *BillService) validateCategory(ctx context.Context, householdID, categoryID string) error {
	if categoryID == "" {
		return nil
	}
	ok, err := s.deps.Categories.BelongsToHousehold(ctx, householdID, categoryID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrCategoryKindMismatch
	}
	kind, err := s.deps.Categories.Kind(ctx, householdID, categoryID)
	if err != nil {
		return err
	}
	if kind != domain.CategoryExpense {
		return domain.ErrCategoryKindMismatch
	}
	return nil
}
