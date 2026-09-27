// This file holds the money slice's bill ports. ports.go lists every ports
// file.

package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// BillRecord is a bill joined to the names the screen displays -- its
// category and its pay-from account's nickname. Same shape and reason as
// AccountView and TransactionView: every consumer wants the names, and
// re-reading them per row is a query per row.
//
// Bill.Amount carries the pay-from account's currency, since a bill has no
// currency column of its own (00008_bills.sql). Every method that returns a
// BillRecord, Create included, must populate Bill.Amount.Currency from
// that account join, the same way TransactionService.Create forces an
// expense's currency to its from-account's. There is no second Currency
// field on purpose: two fields carrying the same fact could disagree with
// nothing to catch it.
type BillRecord struct {
	Bill         domain.Bill
	CategoryName string
	AccountName  string
}

// BillPaymentRecord is one settled occurrence joined to its bill's name and
// autopay flag, for the "Paid this month" list ("Singtel fibre · Internet ·
// autopay · DBS").
//
// ListPayments populates both joined fields. RecordPayment populates
// BillName only and leaves Autopay false: its caller already holds the
// flag from reading the whole bill, so joining it back would be a
// redundant read.
type BillPaymentRecord struct {
	Payment  domain.BillPayment
	BillName string
	Autopay  bool
}

// NewBillRow is Create's input. DueAnchorDay is derived by the service from
// NextDue, never supplied by a caller: an anchor that disagreed with the first
// due date would drift on the very first advance. BillService derives it the
// same way on any update that moves NextDue, so the one calendar computation
// lives in a single layer rather than being duplicated in the repository.
type NewBillRow struct {
	HouseholdID        string
	Name               string
	AmountMinor        int64
	Cadence            domain.Cadence
	NextDue            time.Time
	DueAnchorDay       int
	CategoryID         string
	PayFromAccountID   string
	PaidByMembershipID string
	Autopay            bool
	IsSubscription     bool
}

// PaymentWrite is everything RecordPayment needs to write all three rows.
// The service assembles it; the repository does not look anything up.
//
// Currency is the pay-from account's, resolved by the service through
// AccountLookup. Description is the bill's name, so the ledger row reads
// as the bill's own -- which is what makes an accidental duplicate entry
// visible.
type PaymentWrite struct {
	HouseholdID        string
	BillID             string
	DueOn              time.Time
	PaidOn             time.Time
	AmountMinor        int64
	Currency           string
	Description        string
	CategoryID         string
	PayFromAccountID   string
	PaidByMembershipID string
	// NextDue is what bills.next_due becomes, already computed by
	// domain.NextDue. nil settles a one-off.
	NextDue *time.Time
}

// BillRepository is one household's bills and their payment history. Two
// contracts here are load-bearing and neither is enforced by the database:
//
//   - bill_payments has no constraint tying its household_id to its bill's, so
//     a row could disagree with the bill it names. Every method that reads or
//     writes a payment must filter by household_id AND bill_id together, never
//     by payment id alone, or a payment leaks across households -- same rule
//     GoalRepository follows.
//
//   - MonthTotals cannot be computed from bills alone: a monthly bill paid on
//     8 July has next_due = 8 August, so filtering on bills.next_due alone
//     misses every bill already paid. The implementation must union
//     bill_payments by due_on with unpaid bills by next_due.
//
//     The two halves filter archived bills differently on purpose: the
//     unpaid half excludes an archived bill (nobody intends to pay it
//     again), but the paid half includes it (the money already left, and
//     archiving afterwards must not retroactively empty a month it was
//     paid in).
type BillRepository interface {
	// List returns one household's bills with their category and account
	// names. includeArchived is a UNION, not a filter swap: false returns
	// the live bills, true returns live AND archived together, each
	// carrying its own ArchivedAt -- the AccountRepository.List /
	// GoalRepository.List contract. Don't implement it as "archived
	// instead".
	List(ctx context.Context, householdID string, includeArchived bool) ([]BillRecord, error)
	// Get reports domain.ErrNotFound when no bill with this id exists in this
	// household -- including when one exists in a different household, which
	// must be indistinguishable from not existing at all.
	Get(ctx context.Context, householdID, billID string) (BillRecord, error)
	// Create writes one row. A name colliding with UNIQUE (household_id, name)
	// -- archived rows included -- surfaces as domain.ErrBillNameTaken. The
	// returned Bill.Amount.Currency comes from the pay-from account, per
	// BillRecord's own comment -- NewBillRow carries no currency of its own.
	Create(ctx context.Context, in NewBillRow) (BillRecord, error)
	// Update replaces every mutable column. BillService is what turns a
	// partial PATCH into a complete domain.Bill; this port never merges. Same
	// collision contract as Create.
	Update(ctx context.Context, b domain.Bill) (BillRecord, error)
	// SetArchived stamps archived_at with at, or clears it when archived is
	// false, and returns the bill as it now stands. Same
	// at-supplied-by-the-caller convention as AccountRepository.SetArchived and
	// GoalRepository.SetArchived; returns BillRecord rather than a bare
	// domain.Bill (so the joined names come with it) or a bare error -- every
	// 2xx except 204 carries a JSON body in this product, so a bare error would
	// force the handler into a second Get just to build its response.
	SetArchived(ctx context.Context, householdID, billID string, archived bool, at time.Time) (BillRecord, error)
	// RecordPayment writes the bill_payments row, the expense transaction and
	// the advanced next_due in ONE database transaction. A bill left advanced
	// with no payment, or a payment with no expense, is not a state this port
	// can produce. An occurrence already paid surfaces as
	// domain.ErrAlreadyExists, from UNIQUE (bill_id, due_on).
	RecordPayment(ctx context.Context, in PaymentWrite) (BillPaymentRecord, error)
	// UndoPayment deletes the payment, deletes its transaction when the link
	// still points at one, and rewinds next_due to the payment's due_on -- all
	// in ONE database transaction, all three or none.
	//
	// It refuses any payment that is not the bill's most recent, with
	// *domain.BillPaymentNotLatestError (Unwrap is domain.ErrForbidden, so
	// matching the bare sentinel still works): undoing an older payment would
	// rewind next_due behind a period that is still paid, showing a due date
	// for money already spent. The error carries the due date that would have
	// been accepted, so the HTTP layer can name it instead of a bare refusal.
	UndoPayment(ctx context.Context, householdID, billID, paymentID string) error
	// ListPayments returns one household's payments whose due_on falls in the
	// month containing `month`, newest paid_on first, ties by bill name.
	ListPayments(ctx context.Context, householdID string, month time.Time) ([]BillPaymentRecord, error)
	// MonthTotals returns the two figures the stat cards pair: paidMinor is
	// the sum of payments due in the month, dueMinor is that plus every
	// unarchived bill still due in it -- see this interface's header
	// comment for why the second can't come from bills alone.
	//
	// Both are per-currency, keyed by the pay-from account's currency,
	// since a household can hold accounts in more than one. The service
	// converts and adds; the repository never does money arithmetic across
	// currencies.
	MonthTotals(ctx context.Context, householdID string, month time.Time) (dueMinor, paidMinor map[string]int64, err error)
}
