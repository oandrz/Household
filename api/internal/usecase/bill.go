package usecase

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// BillView is one row on the screen: the stored bill plus the derived
// figures. Amount is in the BILL's own currency -- the pay-from account's --
// not the household's primary: a row for an IDR account renders in IDR.
// Only the summary totals below convert, for the reason GoalView's own
// comment gives.
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
	// one-off. The design's own formula table defines BOTH "Due soon" and
	// "Later" as requiring a non-NULL next_due, so a bill like this belongs
	// in neither: DueSoon is always false for one, and without this flag the
	// frontend has no way to tell "genuinely Later, just far out" from "done,
	// nothing left to schedule" -- both arrive with NextDue nil once a bill
	// this old existed before MarkPaid could produce one.
	//
	// It is never dropped from the page to compensate: 00008_bills.sql's own
	// comment on next_due says a settled one-off is deliberately not
	// auto-archived, "that would hide a record the household may still want
	// to see." Settled is how it stays visible without being miscategorised.
	Settled bool
}

// BillPaymentView is one row of "Paid this month" -- ListPayments' own
// BillPaymentRecord, unwrapped at this layer the same way BillView unwraps
// BillRecord, though nothing here is derived: a settled payment has no next
// occurrence to be overdue or due-soon about.
type BillPaymentView struct {
	Payment  domain.BillPayment
	BillName string
	Autopay  bool
}

// BillsSummary is the page header and the three stat cards. DueThisMonth,
// PaidSoFar, SubscriptionsMonthly and SubscriptionsAnnual are all in the
// household's primary currency; a bill whose account currency has no rate to
// primary is excluded from every one of them and counted in ExcludedNoRate,
// never silently dropped (the BudgetRolloverCard precedent, 8a1114b).
//
// NextDueAmount is the one figure here that does NOT convert: it is the
// single next-due bill's own Amount, in that bill's own currency, exactly
// the way BillView's own Amount is -- converting only it would leave the
// card pairing an amount with a currency symbol from Summary.Currency that
// disagrees with it. It is the zero domain.Money{} (no currency at all) when
// there is no next-due bill; NextDueOn == nil is the field to gate on, the
// same "nil, not a zero Money" rule TestNextDueIsOmittedWhenThereIsNone pins
// for NextDueOn itself.
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

// NewBill is Create's input. Unlike NewBillRow, it carries no DueAnchorDay --
// the anchor is derived from NextDue.Day() by the service itself (see
// Create's own doc comment), so a caller cannot structurally supply one, the
// same reasoning GoalUpdate's missing Currency field documents for goals.
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

// BillPatch is a PATCH: a nil field is unchanged. There is deliberately no
// ArchivedAt field -- archive and restore are their own routes, so an
// ordinary rename cannot archive a bill as a side effect (router.go's own
// comment for accounts, categories and goals).
//
// ClearCategory and ClearPayer are how a set field is unset, the same
// explicit -clear convention clearReceivedAmount uses on transactions: a nil
// pointer already means "unchanged", so it cannot also mean "clear".
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

// MarkPayment is MarkPaid's input. AmountMinor is the caller's, not the
// bill's own stored figure: the modal that produces this prefills the bill's
// amount but leaves it editable, because a utility bill varies month to
// month, and marking one payment does not change the bill's own standing
// amount.
type MarkPayment struct {
	HouseholdID string
	BillID      string
	AmountMinor int64
	PaidOn      time.Time
}

// BillDeps gathers every port BillService needs, mirroring GoalDeps. There is
// no Clock here for the same reason: every method that needs the current
// date takes it as a parameter (today, at), so nothing in this service reads
// the wall clock and every test is deterministic.
type BillDeps struct {
	Bills      BillRepository
	Households HouseholdRepository
	FX         FXRateProvider
	Accounts   AccountLookup
}

// BillService composes the Bills screen and every write against it. Like
// every other service here it takes no actor parameter: services enforce
// what is *valid*, middleware enforces who is *asking* -- the money
// capability and the owner check live in the router (Task 9).
type BillService struct {
	deps BillDeps
}

func NewBillService(deps BillDeps) *BillService {
	return &BillService{deps: deps}
}

// List composes the whole Bills screen for one household: every row (each
// carrying Overdue/DueSoon), the paid-this-month list and the page summary.
// today is always a parameter -- see BillDeps' own comment -- so every figure
// is deterministic in tests and driven by the clock port at the HTTP layer in
// production.
//
// The household is read once for the primary currency, the bills once via
// Bills.List, the due/paid figures once via Bills.MonthTotals, and the paid
// list once via Bills.ListPayments -- four repository calls regardless of how
// many bills or payments the household has.
//
// ExcludedNoRate counts once per BILL (a bill due this month that is also
// ticked as a subscription counts once, not twice, even though it would
// otherwise touch two totals -- the NetWorthSummary and GoalService.List
// precedent: one entity, one exclusion) plus once per PAYMENT whose own bill
// was not already counted that way (a payment made from a bill that has
// since stopped being due this month, or been archived, is a distinct fact
// from that bill's current state, so it is not folded into the same count).
//
// DueThisMonth and PaidSoFar themselves still sum from Bills.MonthTotals'
// own currency-aggregated maps -- that repository call exists specifically
// because the paid half cannot be reconstructed from bills alone (its own
// header comment explains why), and nothing here second-guesses that. What
// an aggregated map cannot do is say which bill or payment a given currency's
// contribution came from, which is what ExcludedNoRate needs to be precise
// -- so that identity is recovered separately, by walking the same `records`
// and `paymentRecords` this method already fetches for the page's other
// purposes, entirely independent of the dueMinor/paidMinor summing below.
func (s *BillService) List(ctx context.Context, householdID string, includeArchived bool, today time.Time) (BillsView, error) {
	household, err := s.deps.Households.Get(ctx, householdID)
	if err != nil {
		return BillsView{}, err
	}
	primary := household.PrimaryCurrency

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
	dueTotal, paidSoFarTotal, subscriptionsAnnual := zero, zero, zero
	excludedNoRate := 0
	// excludedBillIDs is what stops the per-bill pass below and the
	// per-payment pass after it from counting the same bill twice: a bill
	// paid this month that is also a currently-ticked subscription is one
	// underlying no-rate fact, not two.
	excludedBillIDs := map[string]bool{}

	// views is built with make(..., 0, ...), never left nil: a household
	// with no bills must still serialise Bills as JSON [], not null (the
	// GoalService.List precedent for the same reason).
	views := make([]BillView, 0, len(records))
	var (
		autopayCount, billCount        int
		nextDueBillID, nextDueBillName string
		nextDueOn                      *time.Time
		nextDueAmount                  domain.Money
		nextDueOverdue, nextDueAutopay bool
	)

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
		dueThisMonth := false
		if b.NextDue != nil {
			candidate := *b.NextDue
			if nextDueOn == nil || candidate.Before(*nextDueOn) ||
				(candidate.Equal(*nextDueOn) && b.Name < nextDueBillName) {
				nextDueBillID, nextDueBillName = b.ID, b.Name
				nextDueOn = &candidate
				nextDueAmount = b.Amount
				nextDueOverdue = view.Overdue
				nextDueAutopay = b.Autopay
			}
			dueThisMonth = candidate.Year() == today.Year() && candidate.Month() == today.Month()
		}

		// excludedThisBill is one flag for the whole bill, covering both
		// totals it might touch below, per this method's own comment on
		// ExcludedNoRate. The due-this-month probe here exists only to
		// recover per-bill identity for the count -- DueThisMonth's actual
		// figure is unaffected either way, since it sums dueMinor
		// (Bills.MonthTotals' own aggregated figure) further down, which
		// already omits this bill's contribution when its currency has no
		// rate.
		excludedThisBill := false
		if dueThisMonth {
			if _, convErr := s.convert(ctx, b.Amount, primary); convErr != nil {
				excludedThisBill = true
			}
		}

		if b.IsSubscription {
			if annual, ok := domain.AnnualEquivalentMinor(b.Cadence, b.Amount.Amount); ok {
				converted, convErr := s.convert(ctx, domain.Money{Amount: annual, Currency: b.Amount.Currency}, primary)
				if convErr != nil {
					excludedThisBill = true
				} else {
					subscriptionsAnnual, err = subscriptionsAnnual.Add(converted)
					if err != nil {
						return BillsView{}, err
					}
				}
			}
			// ok == false is a one-off: not a recurring cost, ticked or not,
			// and never a reason to exclude anything.
		}

		if excludedThisBill {
			excludedNoRate++
			excludedBillIDs[b.ID] = true
		}
	}

	// Payments due this month also feed DueThisMonth (the union rule
	// Bills.MonthTotals' own header comment states), but a payment is a
	// distinct entity from the bill that generated it, so its own
	// no-rate exclusion is counted here -- unless that bill was already
	// counted above, per this method's own comment.
	for _, p := range paymentRecords {
		if excludedBillIDs[p.Payment.BillID] {
			continue
		}
		if _, convErr := s.convert(ctx, p.Payment.Amount, primary); convErr != nil {
			excludedNoRate++
			excludedBillIDs[p.Payment.BillID] = true
		}
	}

	// The actual sums: Bills.MonthTotals' own currency-aggregated maps,
	// converted per currency then added. A currency with no rate is simply
	// skipped here -- its exclusion was already counted, precisely, by the
	// two per-entity passes above; counting it again here (once per
	// currency) is exactly the bug this method's own comment describes
	// fixing.
	for currency, amount := range dueMinor {
		converted, convErr := s.convert(ctx, domain.Money{Amount: amount, Currency: currency}, primary)
		if convErr != nil {
			continue
		}
		dueTotal, err = dueTotal.Add(converted)
		if err != nil {
			return BillsView{}, err
		}
	}
	for currency, amount := range paidMinor {
		converted, convErr := s.convert(ctx, domain.Money{Amount: amount, Currency: currency}, primary)
		if convErr != nil {
			continue
		}
		paidSoFarTotal, err = paidSoFarTotal.Add(converted)
		if err != nil {
			return BillsView{}, err
		}
	}

	// Ascending by due date, nil last, ties by name -- one order across the
	// whole list rather than two separately-sorted Due-soon/Later slices: the
	// frontend splits on each row's own DueSoon flag (Task 9's own comment),
	// so the order they arrive in is the order both halves render in.
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

	paidViews := make([]BillPaymentView, 0, len(paymentRecords))
	for _, p := range paymentRecords {
		paidViews = append(paidViews, BillPaymentView{Payment: p.Payment, BillName: p.BillName, Autopay: p.Autopay})
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
			NextDueBillID:        nextDueBillID,
			NextDueBillName:      nextDueBillName,
			NextDueOn:            nextDueOn,
			NextDueAmount:        nextDueAmount,
			NextDueOverdue:       nextDueOverdue,
			NextDueAutopay:       nextDueAutopay,
			AutopayCount:         autopayCount,
			BillCount:            billCount,
			SubscriptionsMonthly: subscriptionsMonthly,
			SubscriptionsAnnual:  subscriptionsAnnual,
			ExcludedNoRate:       excludedNoRate,
		},
	}, nil
}

// Create validates and writes a new bill. DueAnchorDay is derived from
// NextDue.Day() here, never accepted from a caller -- NewBill carries no
// field for one (see that type's own comment), so an anchor that disagreed
// with the bill's own first due date is not a state Create can even be asked
// to produce.
//
// today is one parameter wider than the brief's own sketch of this method,
// for the same reason GoalService.SetArchived documents its own widening:
// the returned BillView's Overdue and DueSoon are meaningless without it, and
// BillDeps carries no Clock (see its own comment) for this method to read the
// date from instead.
//
// A non-empty PaidByMembershipID that does not belong to this household is
// refused with domain.ErrAccountOwnerNotInHousehold, the identical check
// AccountService.Create runs for OwnerMembershipID and TransactionService
// runs for its own PaidByMembershipID: a bill's payer, like an account's
// owner, is a validity question this layer answers, not something left for
// the HTTP layer to have caught by then.
func (s *BillService) Create(ctx context.Context, in NewBill, today time.Time) (BillView, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return BillView{}, domain.ErrBillNameRequired
	}
	if in.AmountMinor <= 0 {
		return BillView{}, domain.ErrBillAmountNotPositive
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

	// "" is the "" <-> SQL NULL convention (PaidByMembershipID's own comment
	// in ports.go): unattributed is always valid, so this only runs for a
	// caller-supplied id. AccountService.Create's identical check on
	// OwnerMembershipID is the reason domain.ErrAccountOwnerNotInHousehold's
	// own wording ("that member is not in this household") is already
	// generic enough to share rather than duplicate as a bills-only
	// sentinel -- see that sentinel's own comment in errors.go.
	if in.PaidByMembershipID != "" {
		ok, err := s.deps.Accounts.MembershipBelongsToHousehold(ctx, in.HouseholdID, in.PaidByMembershipID)
		if err != nil {
			return BillView{}, err
		}
		if !ok {
			return BillView{}, domain.ErrAccountOwnerNotInHousehold
		}
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

// Update Gets the stored bill, applies each non-nil field of patch onto it,
// and hands BillRepository.Update the complete result -- the port never
// merges (its own doc comment states the rule; AccountRepository.Update and
// TransactionRepository.Update state it for their own tables).
//
// DueAnchorDay is re-derived from the new NextDue whenever the patch moves
// it: an explicit edit is the household choosing a new anchor, and leaving
// the stored one would make the NEXT advance land on a day they did not
// pick. This is deliberately the mirror image of domain.NextDue's own
// mechanical rewind (Task 5's anchor test), which must NOT touch the anchor
// -- that case is the bill quietly moving forward on its own cadence, this
// case is a person typing a new date into the edit form.
//
// A PayFromAccountID pointed at an account whose currency differs from the
// bill's current one is refused with domain.ErrBillCurrencyImmutable: a
// bill's amount is stored in its pay-from account's currency (BillRecord's
// own comment), so re-pointing across a currency boundary would silently
// reinterpret every past figure. The message naming both currencies is the
// HTTP layer's job, not this one's.
//
// A non-empty PaidByMembershipID is refused with
// domain.ErrAccountOwnerNotInHousehold when it does not belong to this
// household, the same check Create runs and AccountService.Create runs for
// OwnerMembershipID -- services enforce what is valid, and a payer from
// another household is not.
//
// today is one parameter wider than the brief's own sketch, for the same
// reason Create's own comment gives.
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
	// ClearCategory wins over a nil CategoryID being ambiguous, the same
	// reason GoalUpdate.ClearTargetMonth wins over a nil TargetMonth: without
	// it there would be no way to tell "leave alone" from "the household
	// picked uncategorised," and both would arrive as patch.CategoryID == nil.
	if patch.ClearCategory {
		b.CategoryID = ""
	} else if patch.CategoryID != nil {
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
		b.PayFromAccountID = *patch.PayFromAccountID
	}
	if patch.ClearPayer {
		b.PaidByMembershipID = ""
	} else if patch.PaidByMembershipID != nil {
		// A nil patch.PaidByMembershipID means "leave alone" and ClearPayer
		// already handled "unset it" above, so the membership check below
		// only ever needs to run for a caller genuinely naming a member --
		// the same guard Create's own comment explains, checked again here
		// because Update's patch can name a DIFFERENT membership than the
		// one Create originally validated. An empty-but-non-nil pointer
		// (which a well-behaved caller sends via ClearPayer instead, never
		// this field) still clears it exactly as it always has, needing no
		// check.
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
// GoalRepository.SetArchived use. BillRepository.SetArchived already returns
// the full record, so this needs no second Get the way a bare-error port
// would force on it (that port method's own doc comment). at doubles as
// "today" for the returned view's Overdue/DueSoon, the same reason Create's
// own comment explains BillDeps carrying no Clock.
func (s *BillService) SetArchived(ctx context.Context, householdID, billID string, archived bool, at time.Time) (BillView, error) {
	rec, err := s.deps.Bills.SetArchived(ctx, householdID, billID, archived, at)
	if err != nil {
		return BillView{}, err
	}
	return s.toView(rec, at), nil
}

// MarkPaid writes the payment, the expense and the advanced due date, through
// BillRepository.RecordPayment's single transaction -- this is the seam
// where Bills writes into the ledger: the expense it creates is what feeds
// Budget's Spent, the daily pace figures, Spending by person and net worth,
// so getting the currency or the date wrong here is wrong money on three
// other screens.
//
// The amount is the caller's, not the bill's own stored figure -- see
// MarkPayment's own comment. The bill's own amount_minor is left untouched by
// paying.
//
// AmountMinor must be positive, the same domain.ErrBillAmountNotPositive
// refusal Create (bill.go's own check) and Update give -- checked here, in
// the service, not by the HTTP layer: bill_payments' own CHECK
// (amount_minor > 0) would otherwise be the first thing to catch a
// non-positive amount, and a raw constraint violation surfacing as a 500 is
// not an acceptable answer to a bad request body. Checked before the Get
// below, the same "validate the caller's own input before spending a query
// on it" order Create uses.
func (s *BillService) MarkPaid(ctx context.Context, in MarkPayment) (BillPaymentView, error) {
	if in.AmountMinor <= 0 {
		return BillPaymentView{}, domain.ErrBillAmountNotPositive
	}
	rec, err := s.deps.Bills.Get(ctx, in.HouseholdID, in.BillID)
	if err != nil {
		return BillPaymentView{}, err
	}
	if rec.Bill.IsArchived() {
		return BillPaymentView{}, domain.ErrForbidden
	}
	if rec.Bill.NextDue == nil {
		// A settled one-off has no occurrence left to pay -- nil here means
		// exactly that (Bill.NextDue's own comment), not "not yet loaded".
		return BillPaymentView{}, domain.ErrForbidden
	}

	acct, err := s.deps.Accounts.Get(ctx, in.HouseholdID, rec.Bill.PayFromAccountID)
	if err != nil {
		return BillPaymentView{}, err
	}
	if acct.Account.IsArchived() {
		return BillPaymentView{}, domain.ErrForbidden
	}
	// The expense's currency is the pay-from ACCOUNT's, never the bill's own
	// stored figure reinterpreted -- transaction.go:232 is the identical rule
	// TransactionService.Create applies, and a test asserts the two agree.
	currency := acct.Balance.Currency

	// dueOn is the occurrence being settled: the bill's CURRENT next_due, not
	// PaidOn. A bill due the 8th paid on the 11th still settles the 8th's
	// occurrence -- PaidOn only ever feeds the payment's own paid_on column
	// and, for a recurring bill, the advance below.
	dueOn := *rec.Bill.NextDue

	var next *time.Time
	// Advance from the DUE date, never from PaidOn: domain.NextDue's own
	// comment states the same rule for the mechanical rewind it performs --
	// paying three days late must not shift the bill's day, or a year of late
	// payments walks it a month off. ok is false only for a one-off, which
	// settles with no next occurrence at all (PaymentWrite.NextDue stays
	// nil).
	if n, ok := domain.NextDue(rec.Bill.Cadence, dueOn, rec.Bill.DueAnchorDay); ok {
		next = &n
	}

	pay, err := s.deps.Bills.RecordPayment(ctx, PaymentWrite{
		HouseholdID:        in.HouseholdID,
		BillID:             in.BillID,
		DueOn:              dueOn,
		PaidOn:             in.PaidOn,
		AmountMinor:        in.AmountMinor,
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
	// Autopay comes from the bill this method already read, not from pay:
	// BillPaymentRecord's own doc comment says RecordPayment deliberately
	// leaves Autopay false because its caller -- this method -- already holds
	// the flag, so joining it back would be a second read of something
	// already in hand.
	return BillPaymentView{Payment: pay.Payment, BillName: pay.BillName, Autopay: rec.Bill.Autopay}, nil
}

// UndoPayment is a straight delegation. The repository owns the whole
// transaction -- deleting the payment, deleting its linked expense, rewinding
// next_due -- and owns the most-recent-only refusal (domain.ErrForbidden):
// this method neither swallows nor reinterprets whatever comes back.
func (s *BillService) UndoPayment(ctx context.Context, householdID, billID, paymentID string) error {
	return s.deps.Bills.UndoPayment(ctx, householdID, billID, paymentID)
}

// toView composes one BillView from a repository record, computing Overdue
// and DueSoon against today -- the one calculation every method that returns
// a BillView shares, so List, Create, Update and SetArchived cannot drift on
// what "overdue" means the way transaction.go's validate exists to stop
// Create and Update drifting on transaction rules.
func (s *BillService) toView(rec BillRecord, today time.Time) BillView {
	b := rec.Bill
	overdue := b.NextDue != nil && domain.IsOverdue(*b.NextDue, today)
	dueSoon := overdue
	if !dueSoon && b.NextDue != nil {
		// Day boundaries, not a raw duration: startOfDay strips both times
		// down to midnight (in their own location, which is UTC everywhere a
		// bill's dates come from) before the 30-day comparison, the same
		// normalisation domain.IsOverdue applies to NextDue and today before
		// comparing them.
		dueSoon = startOfDay(*b.NextDue).Sub(startOfDay(today)) <= 30*24*time.Hour
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

// convert turns one amount into the household's primary currency. This
// duplicates GoalService.convert and AccountService.convert deliberately --
// see either's own comment: each service declares its own dependencies, and
// hoisting this into a shared helper would give one service a reason to
// change when another's FX needs do.
func (s *BillService) convert(ctx context.Context, m domain.Money, primary string) (domain.Money, error) {
	if m.Currency == primary {
		return m, nil
	}
	rate, err := s.deps.FX.Rate(ctx, m.Currency, primary)
	if err != nil {
		return domain.Money{}, err
	}
	amount, err := rate.Apply(m.Amount)
	if err != nil {
		return domain.Money{}, err
	}
	return domain.Money{Amount: amount, Currency: primary}, nil
}
