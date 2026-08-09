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
// ExcludedNoRate counts a BILL, never a currency and never a total: it is
// built once, while walking every unarchived bill below (the NetWorthSummary
// and GoalsSummary precedent -- one entity, one exclusion, whichever totals
// it would have touched). Bills.MonthTotals only returns currency-aggregated
// sums, though, so a currency that first turns up unconvertible there (a
// payment made before its bill was archived, in a currency no live bill
// carries any more) is counted the first time it is seen and never again --
// noRateCurrencies is the set shared across both passes for exactly that
// reason.
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

	noRateCurrencies := map[string]bool{}
	excludedNoRate := 0

	var (
		views                          []BillView
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
		}

		if !b.IsSubscription {
			continue
		}
		annual, ok := domain.AnnualEquivalentMinor(b.Cadence, b.Amount.Amount)
		if !ok {
			continue // a one-off is not a recurring cost, ticked or not
		}
		converted, convErr := s.convert(ctx, domain.Money{Amount: annual, Currency: b.Amount.Currency}, primary)
		if convErr != nil {
			if !noRateCurrencies[b.Amount.Currency] {
				noRateCurrencies[b.Amount.Currency] = true
				excludedNoRate++
			}
			continue
		}
		subscriptionsAnnual, err = subscriptionsAnnual.Add(converted)
		if err != nil {
			return BillsView{}, err
		}
	}

	// Due this month and paid so far can only be walked per currency --
	// Bills.MonthTotals already collapsed every bill into one sum per
	// currency, for the reason its own doc comment gives (the paid half
	// cannot be reconstructed from bills alone). noRateCurrencies is
	// consulted, never re-added-to for a currency it already names, so a
	// currency already excluded by the subscriptions pass above does not
	// count twice.
	for currency, amount := range dueMinor {
		converted, convErr := s.convertCurrency(ctx, currency, amount, primary, noRateCurrencies, &excludedNoRate)
		if convErr != nil {
			continue
		}
		dueTotal, err = dueTotal.Add(converted)
		if err != nil {
			return BillsView{}, err
		}
	}
	for currency, amount := range paidMinor {
		converted, convErr := s.convertCurrency(ctx, currency, amount, primary, noRateCurrencies, &excludedNoRate)
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

// convertCurrency is convert's wrapper for the two currency-aggregated
// passes in List (dueMinor/paidMinor): it consults and updates
// noRateCurrencies/excludedNoRate itself, so neither pass has to repeat the
// "have we already counted this currency" check inline.
func (s *BillService) convertCurrency(ctx context.Context, currency string, amountMinor int64, primary string,
	noRateCurrencies map[string]bool, excludedNoRate *int) (domain.Money, error) {
	converted, err := s.convert(ctx, domain.Money{Amount: amountMinor, Currency: currency}, primary)
	if err != nil {
		if !noRateCurrencies[currency] {
			noRateCurrencies[currency] = true
			*excludedNoRate++
		}
		return domain.Money{}, err
	}
	return converted, nil
}
