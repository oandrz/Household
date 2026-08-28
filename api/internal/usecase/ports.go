// Package usecase holds the application services. It depends on domain and on
// the port interfaces declared here — never on an adapter.
package usecase

import (
	"context"
	"math"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

type Clock interface {
	Now() time.Time
}

type PasswordHasher interface {
	Hash(plain string) (string, error)
	Verify(plain, encoded string) bool
}

type TokenGenerator interface {
	NewToken() (raw string, hash []byte, err error)
	HashToken(raw string) []byte
}

type Mailer interface {
	SendMagicLink(ctx context.Context, to, name, url string) error
	SendInvite(ctx context.Context, to, name, inviterName, url string) error
	// SendSignupLink mails the create-household link. There is no name
	// parameter: at sign-up-request time nobody has told us one, and inventing
	// a greeting from the local part of the address would read worse than
	// having none.
	SendSignupLink(ctx context.Context, to, url string) error
	// SendSignupForExistingAccount mails "you already have an account" with no
	// token. It is as load-bearing as SendSignupLink, not a courtesy: if only
	// the fresh-address branch sent mail, the *absence* of an email would tell
	// anyone who can observe the mailbox that the address is registered, which
	// is the oracle the identical 202 exists to prevent.
	SendSignupForExistingAccount(ctx context.Context, to, signInURL string) error
}

// StoredUser carries the password hash, which never leaves the usecase layer.
//
// users.password_hash is nullable in the database, and sqlc generates *string
// for it — but PasswordHash here is a plain string by design, not an
// unwritten gap the Postgres implementation has to paper over. The
// convention, both directions: SQL NULL maps to "", and "" maps to SQL NULL.
// A user created without credentials (e.g. an invited member with no
// password yet) has PasswordHash == "", and Task 12 already treats that
// empty string as "cannot sign in" — the repository must not turn a NULL
// into any other sentinel.
//
// The embedded domain.User.Email follows the identical convention, for the
// identical reason: users.email is also nullable (and citext UNIQUE, so
// storing "" rather than NULL for two credential-less members would collide
// on the unique index where two NULLs do not). A member created without an
// email of their own — the same invited-member-with-no-login case — has
// Email == "", and the repository must round-trip SQL NULL to "" and "" to
// SQL NULL there exactly as it does for PasswordHash.
type StoredUser struct {
	domain.User
	PasswordHash string
}

type UserRepository interface {
	ByEmail(ctx context.Context, email string) (StoredUser, error)
	ByID(ctx context.Context, id string) (StoredUser, error)
	// Create writes email and passwordHash following the same "" <-> NULL
	// convention as StoredUser.PasswordHash (and, by the same reasoning,
	// StoredUser's embedded domain.User.Email): passing "" for either stores
	// SQL NULL, not an empty string in the column. Children (members with no
	// login of their own) are created this way, with email == "" and
	// passwordHash == "".
	Create(ctx context.Context, email, passwordHash, displayName string) (domain.User, error)
	SetPasswordHash(ctx context.Context, userID, hash string) error
	// CreateWithMembership creates the user and their membership in one
	// transaction. Either both happen or neither does: a partial failure leaves an
	// orphaned user with no membership, and because a child's email is NULL there
	// is no unique constraint to make the retry fail loudly — it silently creates
	// another orphan each time.
	CreateWithMembership(ctx context.Context, email, passwordHash, displayName string,
		m domain.Membership) (domain.User, domain.Membership, error)
	// FindOrphanedChild returns the credential-less user (no email, no
	// password) with this exact display name that currently holds no
	// membership anywhere, if one exists. It reports domain.ErrNotFound when
	// there is none. This is the state removing a membership leaves behind
	// without deleting the user row underneath it -- a credential-less
	// member has no email for a unique constraint to protect the way a real
	// address does, so nothing else stops a second create under the same
	// name from silently duplicating one.
	FindOrphanedChild(ctx context.Context, displayName string) (domain.User, error)
}

type HouseholdRepository interface {
	Get(ctx context.Context, householdID string) (domain.Household, error)
	Update(ctx context.Context, h domain.Household) (domain.Household, error)
	// Create writes a household from a fully-populated domain.Household.
	// It takes the value rather than (name, familyName) so no caller depends on
	// the table's currency column defaults -- Seed used to, silently, and a
	// self-serve household needs different values.
	//
	// h.ID is ignored (the database assigns it) and h.FXRateMode is ignored
	// (the column default 'auto' is the only value the CHECK constraint makes
	// safe to assume at creation time).
	Create(ctx context.Context, h domain.Household) (domain.Household, error)
}

// MemberView is a membership joined to its user, which is what every consumer
// of the members list actually wants.
type MemberView struct {
	Membership domain.Membership
	User       domain.User
}

type MembershipRepository interface {
	List(ctx context.Context, householdID string) ([]MemberView, error)
	// ByUser is the one method that cannot take a household scope, because
	// sign-in resolves the household from it. It is therefore the seam where
	// multi-tenancy will need attention: today it returns the single membership
	// a user has, and the query's LIMIT 1 would pick arbitrarily if a user ever
	// belonged to two households.
	ByUser(ctx context.Context, userID string) (domain.Membership, error)
	Create(ctx context.Context, m domain.Membership) (domain.Membership, error)
	Update(ctx context.Context, householdID, membershipID string, role domain.Role, caps domain.Capabilities) error
	Delete(ctx context.Context, householdID, membershipID string) error
}

type SessionRecord struct {
	UserID      string
	HouseholdID string
	ExpiresAt   time.Time
}

type SessionRepository interface {
	Create(ctx context.Context, tokenHash []byte, userID, householdID string, expiresAt time.Time) error
	ByTokenHash(ctx context.Context, tokenHash []byte) (SessionRecord, error)
	Extend(ctx context.Context, tokenHash []byte, expiresAt time.Time) error
	RevokeByToken(ctx context.Context, tokenHash []byte) error
	RevokeAllForUser(ctx context.Context, userID string) error
}

type MagicLinkRepository interface {
	Create(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time) error
	Consume(ctx context.Context, tokenHash []byte) (userID string, err error)
	CountSince(ctx context.Context, email string, since time.Time) (int, error)
}

type LoginAttemptRepository interface {
	Record(ctx context.Context, householdID, userID *string, email string, succeeded bool, at time.Time) error
	FailuresSince(ctx context.Context, householdID string, since time.Time) ([]time.Time, error)
	// FailuresSinceForEmail counts attempts by address rather than by household.
	// Sign-in uses it for addresses that match no user, so a stranger sees the
	// same countdown a member does and cannot tell the two apart.
	FailuresSinceForEmail(ctx context.Context, email string, since time.Time) ([]time.Time, error)
	ClearFailures(ctx context.Context, householdID string) error
	// Prune deletes attempts older than before, including the
	// NULL-household_id rows an unknown-address attempt records -- which
	// ClearFailures cannot reach, because it is scoped WHERE household_id = $1
	// and that never matches NULL.
	//
	// The caller is responsible for a cutoff well outside
	// domain.LockoutPolicy.Window. Deleting a row still inside that window
	// would clear a live lockout: a security regression dressed as a cleanup.
	Prune(ctx context.Context, before time.Time) (int64, error)
}

type InviteDetails struct {
	ID           string
	HouseholdID  string
	Email        string
	Name         string
	Role         domain.Role
	Capabilities domain.Capabilities
	FamilyName   string
	InviterName  string
	ExpiresAt    time.Time
	AcceptedAt   *time.Time
}

// AcceptedInvite is what a successful acceptance produces.
type AcceptedInvite struct {
	UserID       string
	MembershipID string
	HouseholdID  string
}

type InviteRepository interface {
	Create(ctx context.Context, householdID, email, name string, role domain.Role,
		caps domain.Capabilities, tokenHash []byte, invitedBy string, expiresAt time.Time) (string, error)
	ByTokenHash(ctx context.Context, tokenHash []byte) (InviteDetails, error)
	// LiveInviteForEmail answers "is there already something usable in
	// flight for this address in this household" -- neither accepted nor
	// expired -- without requiring the raw token that produced it, which is
	// never persisted anywhere. It reports domain.ErrNotFound when there is
	// none, exactly as ByTokenHash does for an unknown token: "no live
	// invite" and "no invite at all" are the same absence from a caller's
	// point of view.
	LiveInviteForEmail(ctx context.Context, householdID, email string) (InviteDetails, error)
	// Returns domain.ErrInviteAlreadyAccepted when the invite was already
	// accepted or has expired — the guard lives in the SQL, not in the caller.
	MarkAccepted(ctx context.Context, inviteID string) error
	// Accept creates the user, creates the membership, and marks the invite
	// accepted in one transaction. Either all three happen or none do -- a
	// partial acceptance would leave an orphaned user occupying the unique
	// email index, which makes the invite permanently unusable: a retry could
	// never create a second user with that address, so there would be no path
	// forward short of manual SQL. Returns domain.ErrInviteAlreadyAccepted,
	// with nothing written, when the invite was already accepted or has
	// expired.
	Accept(ctx context.Context, inviteID, email, passwordHash, displayName string,
		householdID string, role domain.Role, caps domain.Capabilities) (AcceptedInvite, error)
}

// SignupDetails is a pending sign-up, read back by token.
type SignupDetails struct {
	ID         string
	Email      string
	ExpiresAt  time.Time
	ConsumedAt *time.Time
}

// ProvisionedHousehold is what a successful provision produces.
type ProvisionedHousehold struct {
	UserID       string
	HouseholdID  string
	MembershipID string
}

type SignupRepository interface {
	Create(ctx context.Context, email string, tokenHash []byte, expiresAt time.Time) error
	// CreateConsumed writes a signup row that is already consumed at insert
	// time -- consumed_at is set to now in the same statement, not stamped
	// afterward. SignupService.Request calls this on the already-registered
	// branch instead of Create, so that branch also advances
	// CountForEmailSince/CountSince: those counters read every row in this
	// table regardless of consumed_at, but before this method existed,
	// nothing was ever written here for a registered address, so its
	// counters stayed at zero forever and signupPerHourLimit/
	// SignupGlobalDailyLimit never fired for that branch no matter how many
	// requests arrived (see the fix-round note in signup.go's Request doc
	// comment). A row created this way can never provision anything --
	// Provision's guarded UPDATE (ConsumeSignup) requires consumed_at IS
	// NULL -- so it exists solely to be counted, and its token is never
	// mailed to anyone.
	CreateConsumed(ctx context.Context, email string, tokenHash []byte, expiresAt time.Time) error
	ByTokenHash(ctx context.Context, tokenHash []byte) (SignupDetails, error)
	// CountForEmailSince counts sign-up requests for one address since a
	// cutoff, over rows written by both Create and CreateConsumed. Unlike
	// MagicLinkRepository.CountSince it does not join through users -- there
	// is no user to join to -- so it can report a non-zero count for an
	// address with no account, and must: a limit that could only be hit by
	// a registered address would itself distinguish the two.
	CountForEmailSince(ctx context.Context, email string, since time.Time) (int, error)
	// CountSince counts every sign-up request since a cutoff, for the global
	// daily mail ceiling. It reads the table rather than an in-memory counter
	// so restarting the API cannot reset the ceiling.
	CountSince(ctx context.Context, since time.Time) (int, error)
	// Provision creates the household, the owner user, the owner membership,
	// every builtin space and the notification preferences, and stamps the
	// signup consumed -- all in one transaction. Either all of it happens or
	// none of it does.
	//
	// The owner's email address is read from the signup row this transaction is
	// already touching; it is deliberately NOT a parameter. The address that
	// gets an account must be the one the mailed token actually proved, and
	// passing it in would let a caller substitute a different one between
	// SignupService.Complete's read and this write.
	//
	// A partial provision leaves a users row occupying users.email's unique
	// index with no membership under it, which makes that address permanently
	// unable to sign up again: a retry could never create a second user with
	// it. That is the same failure InviteRepository.Accept's doc comment
	// describes, and this method exists for the same reason.
	//
	// Returns domain.ErrTokenExpired, with nothing written, when the signup is
	// no longer usable -- consumed or expired. Like InviteRepository.Accept's
	// guarded UPDATE this collapses the two cases into one zero-rows result and
	// cannot tell them apart; SignupService.Complete's own TokenLifecycle read
	// is what distinguishes them for a caller, and this answer is authoritative
	// only for the race window between that read and this write.
	Provision(ctx context.Context, signupID, passwordHash string,
		b HouseholdBlueprint) (ProvisionedHousehold, error)
	// Prune deletes consumed and expired rows older than before.
	Prune(ctx context.Context, before time.Time) (int64, error)
}

type SpaceRepository interface {
	List(ctx context.Context, householdID string) ([]domain.Space, error)
	Create(ctx context.Context, s domain.Space) (domain.Space, error)
	NextPosition(ctx context.Context, householdID string) (int, error)
}

type NotificationPreferences struct {
	BillReminders   bool
	OverspendAlerts bool
	RetroReminder   bool
	WeeklyDigest    bool
}

type NotificationRepository interface {
	Get(ctx context.Context, householdID string) (NotificationPreferences, error)
	Upsert(ctx context.Context, householdID string, p NotificationPreferences) (NotificationPreferences, error)
}

// Rate is a ratio, held as a fraction rather than a scaled decimal. SGD to IDR
// is {12410, 1}; IDR to SGD is {1, 12410}. A scaled decimal cannot represent
// the second direction — 0.0000806 truncates to zero at any sane scale — and
// IDR to SGD is precisely the direction the design's Finances screen uses.
type Rate struct {
	Numerator   int64
	Denominator int64
}

// Apply converts an amount of minor units, rounding half away from zero. It
// reports domain.ErrAmountOverflow rather than silently wrapping when
// minorUnits * r.Numerator would not fit in an int64 — the same failure mode
// domain.Money.Add already refuses to allow on this codebase's monetary
// path, and multiplication overflows far sooner than addition does.
func (r Rate) Apply(minorUnits int64) (int64, error) {
	if mulOverflows(minorUnits, r.Numerator) {
		return 0, domain.ErrAmountOverflow
	}
	num := minorUnits * r.Numerator
	half := r.Denominator / 2
	if num < 0 {
		return (num - half) / r.Denominator, nil
	}
	return (num + half) / r.Denominator, nil
}

// mulOverflows reports whether a*b would overflow an int64. It computes the
// product (which may wrap, but wrapping a signed integer is well-defined in
// Go, never a panic) and divides back by b: for any b != 0, (a*b)/b == a
// unless the multiplication actually wrapped. The one case that check misses
// is a==-1,b==math.MinInt64 (or the reverse): math.MinInt64 has no positive
// counterpart, so negating it wraps right back to math.MinInt64 and the
// divide-back reproduces a even though the multiplication did overflow —
// that pair is therefore checked explicitly, before the general rule runs.
func mulOverflows(a, b int64) bool {
	if a == 0 || b == 0 {
		return false
	}
	if (a == -1 && b == math.MinInt64) || (b == -1 && a == math.MinInt64) {
		return true
	}
	return (a*b)/b != a
}

// FXRateProvider converts between the household's primary and secondary
// currencies. The design labels the rate "auto"; a live provider replaces the
// static one without any caller changing.
type FXRateProvider interface {
	Rate(ctx context.Context, from, to string) (Rate, error)
}

// AccountView is an account joined to its owner's display name, which is what
// every consumer of the accounts list actually wants -- the same shape and the
// same reason as MemberView above.
//
// Balance is the account's current balance: its opening balance plus every
// transaction dated on or after Account.OpeningBalanceAsOf, summed by the
// repository. It is denominated in the account's own currency, because every
// transaction on an account is; nothing here converts.
//
// It is a separate field from Account.OpeningBalance, and the two are
// different numbers as soon as an account has a transaction on it. Balance
// answers "what does this hold now"; Account.OpeningBalance answers "what did
// someone assert it held on Account.OpeningBalanceAsOf", and is the only one
// of the two a caller may ever write back. Reading Balance and storing it as
// an opening balance moves the household's net worth by every transaction
// since -- a real defect this project shipped in the account edit form, see
// docs/LEARNING.md.
//
// OwnerName is "" for a shared account, following the same "" <-> SQL NULL
// convention as domain.Account.OwnerMembershipID.
type AccountView struct {
	Account   domain.Account
	OwnerName string
	Balance   domain.Money
}

// AccountMonthMovement is one account's net movement across one calendar
// month, in that account's own currency. It is the twelve-month net worth
// chart's only new input.
//
// Delta is signed: money leaving the account is negative, money arriving is
// positive, and a month with no movement produces no row rather than a zero
// one -- a caller reads an absent month as "nothing moved", which is what an
// absent row means.
//
// Month is the first of the month at midnight, so two values for the same
// month compare equal.
type AccountMonthMovement struct {
	AccountID string
	Month     time.Time
	Delta     domain.Money
}

type AccountRepository interface {
	// List returns one household's accounts, ordered oldest first. Archived
	// accounts are included only when includeArchived is true, and never
	// contribute to any total regardless.
	List(ctx context.Context, householdID string, includeArchived bool) ([]AccountView, error)
	// MonthlyMovements returns every account's per-month net movement from
	// since onward, counting only transactions dated on or after that
	// account's own opening_balance_as_of -- the same filter
	// AccountView.Balance is computed with. The two must stay the same: the
	// trend walks backwards from Balance by subtracting these, so a filter
	// that differs by one row makes the older bars wrong and plausible at the
	// same time.
	//
	// There is no upper bound on the transaction date, deliberately. Balance
	// has none either, so a future-dated transaction is already inside the
	// figure the walk anchors on and must be inside these rows too.
	//
	// Archived accounts are included; the caller decides what counts, exactly
	// as it does for Balance.
	MonthlyMovements(ctx context.Context, householdID string, since time.Time) ([]AccountMonthMovement, error)
	// Get reports domain.ErrNotFound when no account with this id exists in
	// this household -- including when one exists in a different household,
	// which must be indistinguishable from not existing at all.
	Get(ctx context.Context, householdID, accountID string) (AccountView, error)
	// Create writes a.OwnerMembershipID following the "" <-> SQL NULL
	// convention: "" stores NULL, meaning shared. a.ID and a.ArchivedAt are
	// ignored -- the database assigns the first and a new account is never
	// born archived.
	Create(ctx context.Context, a domain.Account) (domain.Account, error)
	// Update replaces every mutable column. AccountService is what turns a
	// partial PATCH into a complete Account; this port never merges.
	Update(ctx context.Context, a domain.Account) (domain.Account, error)
	// SetArchived stamps archived_at with at, or clears it when archived is
	// false. Accounts are never deleted: transactions will reference these
	// rows, and destroying an account would take its history with it.
	SetArchived(ctx context.Context, householdID, accountID string, archived bool, at time.Time) (domain.Account, error)
	// MembershipBelongsToHousehold answers whether a membership is in this
	// household, so an account can never be assigned to a member of another
	// one. It lives here rather than on MembershipRepository because that port
	// is already consumed by sign-in and does not need widening for this.
	MembershipBelongsToHousehold(ctx context.Context, householdID, membershipID string) (bool, error)
}

// AccountLookup is what TransactionService needs of accounts: the currency an
// account is denominated in, whether it belongs to this household, and
// whether a membership does too, for the paid-by check. Get returns
// domain.ErrNotFound for an account in another household, the same as
// AccountRepository.Get above -- "that account is not yours" must be
// indistinguishable from "there is no such account" here as well.
//
// *postgres.AccountRepo already satisfies this: both methods exist on it
// already, for AccountRepository above.
type AccountLookup interface {
	Get(ctx context.Context, householdID, accountID string) (AccountView, error)
	MembershipBelongsToHousehold(ctx context.Context, householdID, membershipID string) (bool, error)
}

// TransactionView is a transaction joined to the names the ledger displays --
// its category, who paid, and each account's nickname. Same shape and same
// reason as MemberView and AccountView above: every consumer of the list wants
// the names, and re-reading them per row is a query per row.
//
// The two Before...Opening fields answer whether this transaction predates the
// opening-balance date of the account on that side, and so does not move that
// account's balance. Each is nil when there is no account on that side.
//
// It is two fields rather than one because a transfer has two accounts with
// two different opening dates: it can predate one and not the other, moving
// one balance and leaving the other alone. A single flag would mark such a row
// with a note that is half true. The server answers this rather than the
// frontend recomputing it, so the rule lives in exactly one place.
type TransactionView struct {
	Transaction     domain.Transaction
	CategoryName    string
	PaidByName      string
	FromAccountName string
	ToAccountName   string

	BeforeFromAccountOpening *bool
	BeforeToAccountOpening   *bool
}

// TransactionFilter is the design's five filters plus paging. An empty field
// means no filtering on it, following the same "" <-> unset convention the
// rest of this file uses.
//
// AccountID matches a transaction on *either* side. A filter that only matched
// from_account_id would hide money arriving in the account someone selected,
// which is half of what they were looking for.
//
// Paging is keyset, not offset: CursorDate and CursorID are the last row of
// the previous page, and the query asks for rows ordered after that pair.
// Offset paging shifts every later row by one when a transaction is added
// mid-scroll, so a page boundary silently repeats or skips a transaction.
type TransactionFilter struct {
	Kind               string
	AccountID          string
	CategoryID         string
	PaidByMembershipID string
	// Month is any instant inside the calendar month to list. Zero means every
	// month -- but note the HTTP adapter never sends zero by default: an absent
	// `month` parameter defaults to the current month, and only an explicit
	// `month=all` produces zero here (see parseTransactionFilter).
	Month time.Time

	CursorDate time.Time
	CursorID   string
	Limit      int
}

type CategoryRepository interface {
	// List returns one household's categories in sort_order. Archived
	// categories are included only when includeArchived is true.
	List(ctx context.Context, householdID string, includeArchived bool) ([]domain.Category, error)
	// EnsureSeeded creates the starter set for a household that has none.
	//
	// It is idempotent and safe to run concurrently: one INSERT ... ON
	// CONFLICT DO NOTHING against UNIQUE (household_id, name), never a
	// read-then-write, which would race two simultaneous first requests into
	// two starter sets.
	//
	// An archived category still occupies its unique key. An implementation
	// must count it as already seeded -- never treat "no live categories" as
	// "has none" -- so a household that cleared its whole list is not
	// silently re-seeded over; the unique key is the backstop of last resort,
	// for any path that reaches the insert without going through that count
	// at all.
	EnsureSeeded(ctx context.Context, householdID string, starter []domain.Category) error
	// Create adds one category at the end of the household's sort order.
	// A name colliding with UNIQUE (household_id, name) — archived rows
	// included — surfaces as domain.ErrCategoryNameTaken.
	Create(ctx context.Context, c domain.Category) (domain.Category, error)
	// Rename changes the name only, same collision contract as Create.
	// domain.ErrNotFound when the id is not this household's.
	Rename(ctx context.Context, householdID, categoryID, name string) (domain.Category, error)
	// SetArchived stamps or clears archived_at. Archiving is idempotent,
	// keeps every transaction and budget line referencing the row, and is
	// the only removal that exists — there is no delete.
	SetArchived(ctx context.Context, householdID, categoryID string, archived bool) (domain.Category, error)
}

// CategoryLookup is what TransactionService needs of categories: whether an
// id is one of this household's, and what kind it is. Narrow on purpose -- it
// does not need List or EnsureSeeded, and a port that hands it those is a
// port that invites a service to seed as a side effect of validation.
type CategoryLookup interface {
	BelongsToHousehold(ctx context.Context, householdID, categoryID string) (bool, error)
	Kind(ctx context.Context, householdID, categoryID string) (domain.CategoryKind, error)
}

type TransactionRepository interface {
	// List returns one household's transactions, newest first, matching every
	// filter that is set. It returns at most f.Limit+1 rows so the caller can
	// tell whether another page exists without a second query.
	//
	// f.Limit <= 0 is treated as 50, and any f.Limit above 200 is clamped down
	// to it -- both are the implementation's own constants, not configurable,
	// so a caller (Task 12's handler) that passes through an unvalidated
	// request-provided limit must know it can get back at most 201 rows, not
	// limit+1, and that "no limit sent" does not mean "no cap applied."
	List(ctx context.Context, householdID string, f TransactionFilter) ([]TransactionView, error)
	// Get reports domain.ErrNotFound when no transaction with this id exists
	// in this household -- including when one exists in a different household,
	// which must be indistinguishable from not existing at all.
	Get(ctx context.Context, householdID, transactionID string) (TransactionView, error)
	// Create writes the "" <-> SQL NULL convention for every optional id:
	// category, payer, and whichever account side the kind leaves empty.
	// t.ID is ignored -- the database assigns it.
	Create(ctx context.Context, t domain.Transaction) (domain.Transaction, error)
	// Update replaces every mutable column. TransactionService is what turns a
	// partial PATCH into a complete Transaction; this port never merges.
	Update(ctx context.Context, t domain.Transaction) (domain.Transaction, error)
	// Delete removes the row, and reports domain.ErrNotFound when there was
	// none to remove. Nothing references a transaction, so nothing is
	// orphaned -- which is why this differs from accounts, where SetArchived
	// exists and no delete does.
	Delete(ctx context.Context, householdID, transactionID string) error
	// MonthTotals returns every transaction in one calendar month, which the
	// service converts and sums.
	//
	// It returns rows rather than a SQL SUM deliberately, and the bound is one
	// household's transactions in one month -- the design's own busiest
	// example is 247. A SQL SUM would be correct only for a household whose
	// transactions are all in its primary currency; having two code paths
	// whose answers could disagree is the trade this refuses. The FX provider
	// lives in this layer, so the conversion cannot move down here anyway.
	MonthTotals(ctx context.Context, householdID string, month time.Time) ([]TransactionView, error)
}

// RollOverToGoalInput is what one rollover needs. Note is deliberately absent:
// the row's note stays empty and the frontend renders "From July's unspent
// budget" from source + sourceBudgetMonth, because user-facing copy does not
// belong in a Go handler.
type RollOverToGoalInput struct {
	HouseholdID string
	Month       time.Time // the budget month being rolled over
	GoalID      string
	Amount      domain.Money
	OccurredOn  time.Time
}

type BudgetRepository interface {
	// Get returns one household-month's budget. domain.ErrNotFound means the
	// month has never been budgeted — callers translate that to the empty
	// state, not an error. month is any instant in the month.
	Get(ctx context.Context, householdID string, month time.Time) (domain.Budget, error)
	// Upsert replaces the month's budget wholesale in one transaction:
	// parent row upserted on (household_id, month), lines deleted and
	// rewritten. Full-replace, never merge — the modal always holds the
	// entire budget, and replace makes removed rows unambiguous. b.ID and
	// line IDs are ignored; the database assigns them.
	Upsert(ctx context.Context, b domain.Budget) (domain.Budget, error)
	// History returns the budgets for the closed months in [from, month),
	// plus the viewed month if budgeted — newest first, months without a
	// budget row simply absent, never zero-filled.
	History(ctx context.Context, householdID string, month time.Time, months int) ([]domain.Budget, error)
	// RollOverToGoal writes a budget month's unspent money into a goal as one
	// contribution and stamps the month, in ONE transaction. The stamp is set
	// by a conditional UPDATE (... AND rolled_over_at IS NULL), so a second
	// concurrent call finds no row to update and gets
	// domain.ErrRolloverAlreadyDone rather than writing a second contribution.
	// domain.ErrNotFound when the month has no budget row at all — a state
	// Budget decision 4 makes reachable, since a closed month can have spend
	// and no caps.
	RollOverToGoal(ctx context.Context, in RollOverToGoalInput) (domain.GoalContribution, error)
}

// GoalRecord is one goal with the only derived figure the repository can
// supply: the sum of its contributions. Every other figure on the screen
// (percent, status, required monthly) is domain arithmetic the service does,
// not something SQL should be asked to know.
type GoalRecord struct {
	Goal             domain.Goal
	ContributedMinor int64
}

// GoalMonthTotal is one goal's contributions inside one calendar month.
type GoalMonthTotal struct {
	GoalID      string
	AmountMinor int64
}

// GoalRepository's implementation must not trust a contribution's household
// scoping to be self-evident: 00007_goals.sql's goal_contributions table has
// no database-level constraint tying goal_contributions.household_id to its
// own goal_id's household_id, so a row could in principle carry a
// household_id that disagrees with the goal it names. Every method below
// that reads or writes a contribution -- AddContribution, DeleteContribution,
// ListContributions, MonthContributionTotals -- must therefore filter its SQL
// by household_id AND goal_id together, never by contribution id or goal id
// alone, or a contribution could leak across households. Later tasks
// implement this port; this is the contract they must honour.
type GoalRepository interface {
	// List returns one household's goals with their contributed totals,
	// ordered: dated goals first, newest TargetMonth first, ties by name;
	// dateless goals (TargetMonth == nil) last, by name among themselves.
	// A dateless goal never sorts ahead of a dated one, and never carries a
	// "newest" of its own to compare by -- pinned here so an implementation's
	// ORDER BY (e.g. target_month DESC NULLS LAST, name) cannot silently pick
	// the opposite NULL placement. includeArchived is a UNION, not a filter
	// swap: false returns the live goals, true returns the live ones AND the
	// archived ones together, each carrying its own ArchivedAt. That is the
	// AccountRepository.List / CategoryRepository.List contract, and the
	// accounts screen's own "(archived)" row is what it renders as. Do not
	// implement it as "archived instead".
	List(ctx context.Context, householdID string, includeArchived bool) ([]GoalRecord, error)
	// Get reports domain.ErrNotFound when no goal with this id exists in this
	// household — including when one exists in a different household, which
	// must be indistinguishable from not existing at all.
	Get(ctx context.Context, householdID, goalID string) (GoalRecord, error)
	// Create writes the goal and, when startingBalanceMinor is non-zero, its
	// opening contribution (source starting_balance, dated createdOn) in ONE
	// transaction. A goal whose opening contribution is missing is not a state
	// this port can produce. A name colliding with UNIQUE (household_id, name)
	// — archived rows included — surfaces as domain.ErrGoalNameTaken.
	Create(ctx context.Context, g domain.Goal, startingBalanceMinor int64, createdOn time.Time) (domain.Goal, error)
	// Update replaces every mutable column: name, target, target month
	// (nil clears it), planned monthly. Currency is NOT mutable — see
	// GoalService.Update's own comment. Same collision contract as Create.
	Update(ctx context.Context, g domain.Goal) (domain.Goal, error)
	// SetArchived stamps archived_at with at, or clears it when archived is
	// false -- the same signature AccountRepository.SetArchived uses, at
	// supplied by the caller rather than read with time.Now() inside the
	// port implementation, so today is always a parameter, never a clock
	// reached for down here. Archiving is idempotent: a second archive call
	// keeps the FIRST stamp rather than moving it forward to at
	// (COALESCE(archived_at, $at) — the rule CategoryRepository.SetArchived's
	// own SQL already applies, here with a caller-supplied timestamp in place
	// of that query's now()), and keeps every contribution and rollover
	// reference intact; there is no delete, the accounts precedent.
	SetArchived(ctx context.Context, householdID, goalID string, archived bool, at time.Time) (domain.Goal, error)
	// AddContribution writes one row. c.ID is ignored; the database assigns
	// it. c.Amount's currency must equal the goal's — the service checks, and
	// the column does not exist to hold a second answer.
	AddContribution(ctx context.Context, c domain.GoalContribution) (domain.GoalContribution, error)
	// DeleteContribution removes one row and, when that row is a
	// budget_rollover, clears its month's rolled_over_at and rollover_goal_id
	// on budgets IN THE SAME TRANSACTION. Leaving the stamp would strand the
	// household: money gone from the goal, a month claiming it rolled over,
	// and 409 on every retry. domain.ErrNotFound when there was nothing to
	// remove.
	DeleteContribution(ctx context.Context, householdID, goalID, contributionID string) error
	// ListContributions returns one goal's contributions, newest first, at
	// most limit rows. limit follows TransactionRepository.List's own
	// convention rather than inventing a second one: limit <= 0 is treated
	// as 50, and anything above 200 is clamped down to it. A real LIMIT 0
	// returns zero rows, the opposite of "no cap", which is exactly why this
	// port pins a default instead of passing limit through unclamped.
	ListContributions(ctx context.Context, householdID, goalID string, limit int) ([]domain.GoalContribution, error)
	// MonthContributionTotals sums each unarchived goal's contributions inside
	// one calendar month, EXCLUDING source 'starting_balance'. The exclusion is
	// load-bearing and lives here so no caller can forget it: a household
	// creating four goals with existing balances would otherwise read
	// "S$41,200 added in August" for money that never moved.
	MonthContributionTotals(ctx context.Context, householdID string, month time.Time) ([]GoalMonthTotal, error)
}

// BillRecord is a bill joined to the names the screen displays -- its category
// and its pay-from account's nickname. Same shape and same reason as
// AccountView and TransactionView above: every consumer of the list wants
// the names, and re-reading them per row is a query per row.
//
// Bill.Amount carries the pay-from account's currency: a bill has no
// currency column of its own (see 00008_bills.sql's own comment), so every
// method below that returns a BillRecord -- Create included -- populates
// Bill.Amount.Currency from the same account join that supplies AccountName,
// the same way TransactionService.Create already forces an expense's
// currency to its from-account's. There is deliberately no second Currency
// field here: two fields carrying the same fact would let them disagree
// with nothing to catch it.
type BillRecord struct {
	Bill         domain.Bill
	CategoryName string
	AccountName  string
}

// BillPaymentRecord is one settled occurrence joined to its bill's name and
// autopay flag, which is what the "Paid this month" list renders ("Singtel
// fibre · Internet · autopay · DBS").
//
// ListPayments populates both joined fields. RecordPayment populates BillName
// only and leaves Autopay false: its caller has just read the whole bill and
// already holds the flag, so joining it back would be a second read of
// something the service is looking at.
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

// PaymentWrite is everything RecordPayment needs to write all three rows. The
// service assembles it; the repository does not look anything up.
//
// Currency is the pay-from account's, resolved by the service through
// AccountLookup. Description is the bill's name, so the ledger row is
// recognisable as the bill's own -- which is what makes a household's
// accidental duplicate entry visible rather than invisible.
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

// RetroRecord is one stored retro. Mood is a pointer because "nobody has
// picked an emoji yet" is a real state and 0 is not a mood; CompletedAt is a
// pointer for the same reason -- nil IS the draft concept.
type RetroRecord struct {
	ID string
	// Month is always the first of the calendar month, midnight UTC -- the
	// same normalised convention budgets.month and
	// TransactionRepository.MonthTotals's own month parameter use. A
	// repository must store and return it that way; a caller comparing two
	// Month values (RetroService does, for the mood chart and the startable
	// month) may rely on that rather than re-normalising itself.
	Month       time.Time
	Mood        *int
	WentWell    string
	WasHard     string
	Notes       string
	CompletedAt *time.Time
	Version     int
}

// RetroSummary is one row of the history list: the stored retro plus the
// action counts the row displays. Quote is the exception to "what the
// repository can supply": RetroService.List always overwrites it with
// domain.FirstSentence(Retro.Notes) (per the spec's formulas table, "History
// row"), unconditionally, so a RetroRepository.List implementation has no
// reason to populate it -- whatever it puts there is discarded, not merged.
// This struct carries the field anyway so Tasks 4-8 have one name for the
// row rather than a repository type plus a service-only wrapper around it.
type RetroSummary struct {
	Retro RetroRecord
	// ActionCount is every action the retro has ever recorded, ticked or
	// not -- the History row's own "K actions" figure (spec's formulas
	// table: "K counts all of that retro's actions, ticked or not").
	ActionCount int
	// OpenActionCount is the subset of ActionCount still undone --
	// count(*) WHERE done_at IS NULL, the same predicate SetActionDone's
	// own done=false branch clears. Overview's "Next retro" card reads
	// THIS field, never ActionCount: a retro whose three actions are all
	// ticked has ActionCount 3 but OpenActionCount 0, and the card exists
	// to answer "is there anything still outstanding," not "how many
	// actions were ever written down." Ticking an action leaves this
	// number; it never rejoins it.
	OpenActionCount int
	Quote           string
}

// RetroActionInput is what Add receives. AssigneeMembershipIDs may be empty
// (an action nobody owns yet) or hold one or both owners; CarriedFrom is the
// id of last month's action when this one was carried, "" otherwise.
type RetroActionInput struct {
	HouseholdID           string
	RetroID               string
	Body                  string
	AssigneeMembershipIDs []string
	CarriedFrom           string
}

// RetroActionRecord is one action. DoneAt nil means open.
type RetroActionRecord struct {
	ID                    string
	RetroID               string
	Body                  string
	DoneAt                *time.Time
	CarriedFrom           string
	AssigneeMembershipIDs []string
}

// RetroUpdate is one save of the retro's own fields. Version is the version
// the editor loaded; the repository refuses the write when it no longer
// matches. Mood nil clears the mood, which a household can legitimately do.
type RetroUpdate struct {
	HouseholdID string
	RetroID     string
	// Month is the retro's own month, carried so the repository can tell a
	// retro that no longer exists (ErrNotFound) from one whose version moved
	// under the editor (ErrRetroChanged) after a zero-row UPDATE. The HTTP
	// layer reads {month} from the URL, but passes it through un-normalised
	// -- RetroService.Save is the caller that normalises it (with
	// startOfMonth) before ever setting this field, the same way it already
	// normalises before comparing in List and Month. Always the first of the
	// month, midnight UTC by the time it reaches here -- RetroRecord.Month's
	// own convention; the repository does not normalise it either.
	Month    time.Time
	Mood     *int
	WentWell string
	WasHard  string
	Notes    string
	Version  int
}

// RetroRepository stores one household's monthly retros. Every method is
// scoped by householdID and must filter on it in SQL: a retro that belongs to
// another household must be indistinguishable from one that does not exist.
type RetroRepository interface {
	// Create writes an empty draft for the month and returns it. A month that
	// already has a retro surfaces as domain.ErrAlreadyExists -- the UNIQUE
	// (household_id, month) constraint, translated, never a raw pgx error.
	// This is also what makes a double-clicked button harmless. month must
	// already be the first of the calendar month, midnight UTC -- the caller
	// (RetroService) normalises before calling; this method does not.
	Create(ctx context.Context, householdID string, month time.Time) (RetroRecord, error)
	// ByMonth reports domain.ErrNotFound when the month has no retro, which
	// the page reads as "not started" rather than as an error. month must
	// already be normalised the same way Create's own parameter is -- see
	// that method's comment.
	ByMonth(ctx context.Context, householdID string, month time.Time) (RetroRecord, error)
	// List returns every retro, newest month first, each carrying its own
	// action count AND open action count (RetroSummary's own doc comment
	// says which is which). Deliberately unbounded: a household writes
	// twelve rows a year, so a decade is 120 rows and one query, and the
	// design's "Show 2025 (7 more)" is a disclosure over data the page
	// already holds, not a second request. Do not add paging without a
	// household the flat list actually hurts.
	List(ctx context.Context, householdID string) ([]RetroSummary, error)
	// Update replaces mood and the three text columns, and bumps version, but
	// ONLY when the stored version equals u.Version. A mismatch returns
	// domain.ErrRetroChanged and writes nothing -- the other partner saved
	// while this one was typing, and merging the two would silently lose one
	// of them. The returned record carries the NEW version, so a caller never
	// has to guess what to send next. u.Month must already be normalised --
	// see its own doc comment on RetroUpdate.
	Update(ctx context.Context, u RetroUpdate) (RetroRecord, error)
	// Complete stamps completed_at with at. Idempotent: completing an already
	// finished retro leaves the original timestamp and is not an error, the
	// same shape GoalRepository.SetArchived takes.
	Complete(ctx context.Context, householdID, retroID string, at time.Time) (RetroRecord, error)
	// DeleteDraft removes a retro that has NOT been finished. The
	// completed_at IS NULL condition belongs in the WHERE clause, not in a
	// service if: a check-then-delete can race, and -- the reason that
	// matters here -- a zero-row match must report domain.ErrNotFound rather
	// than success. SetBillNextDue shipped the other way round and committed
	// two of three writes on a zero-row match (docs/LEARNING.md, database
	// catalogue).
	DeleteDraft(ctx context.Context, householdID, retroID string) error
}

// RetroActionRepository stores what a retro decided to do next month.
type RetroActionRepository interface {
	// Add writes the action AND its assignees inside one transaction: an
	// assignee that is not a membership of this household fails the whole
	// insert, so no orphan action survives a half-written assignment.
	Add(ctx context.Context, in RetroActionInput) (RetroActionRecord, error)
	// ForRetro returns a retro's actions in insertion order (created_at, id).
	// There is no position column to sort by -- see 00009_retros.sql for why.
	ForRetro(ctx context.Context, householdID, retroID string) ([]RetroActionRecord, error)
	// SetDone ticks or unticks. done=false clears done_at rather than
	// stamping a "not done" time. Reports domain.ErrNotFound on a zero-row
	// match, for the same reason DeleteDraft does.
	SetDone(ctx context.Context, householdID, actionID string, done bool, at time.Time) error
	// Remove hard-deletes an action. Nothing references an action except a
	// later action's carried_from, which is ON DELETE SET NULL, so removal
	// cannot orphan anything.
	Remove(ctx context.Context, householdID, actionID string) error
	// OpenInMonth returns that month's unticked actions -- the "Still open
	// from July" offer. The caller passes the immediately previous month
	// only: a household that skipped four months must not be handed an
	// unbounded backlog on the night it comes back (spec decision 4). month
	// must already be the first of the calendar month, midnight UTC --
	// RetroRecord.Month's own convention; the caller normalises, not this
	// method.
	OpenInMonth(ctx context.Context, householdID string, month time.Time) ([]RetroActionRecord, error)
}

// BillRepository is one household's bills and their payment history.
//
// Two contracts here are load-bearing and neither is enforced by the database:
//
//   - bill_payments has no constraint tying its household_id to its bill's, so
//     a row could in principle carry a household_id that disagrees with the
//     bill it names. Every method that reads or writes a payment must filter
//     by household_id AND bill_id together, never by payment id alone, or a
//     payment leaks across households. This is the GoalRepository contract,
//     for the same reason.
//
//   - MonthTotals cannot be computed from bills alone. A monthly bill paid on
//     8 July has next_due = 8 August, so a query filtering bills.next_due into
//     the month misses every bill already paid -- which is the entire "paid so
//     far" half of the figure. The implementation must union bill_payments by
//     due_on with unpaid bills by next_due. The naive query passes review and
//     returns a wrong number.
//
//     The two halves filter archived bills differently, on purpose. The unpaid
//     half excludes an archived bill: a bill nobody intends to pay again is
//     not an obligation. The paid half includes it: the money left the
//     household, and archiving a bill afterwards must not retroactively empty
//     the month it was paid in. A reviewer meeting this asymmetry cold will
//     read it as a bug, which is why it is written here.
type BillRepository interface {
	// List returns one household's bills with their category and account
	// names. includeArchived is a UNION, not a filter swap: false returns the
	// live bills, true returns the live ones AND the archived ones together,
	// each carrying its own ArchivedAt. That is the AccountRepository.List and
	// GoalRepository.List contract; do not implement it as "archived instead".
	List(ctx context.Context, householdID string, includeArchived bool) ([]BillRecord, error)
	// Get reports domain.ErrNotFound when no bill with this id exists in this
	// household -- including when one exists in a different household, which
	// must be indistinguishable from not existing at all.
	Get(ctx context.Context, householdID, billID string) (BillRecord, error)
	// Create writes one row. A name colliding with UNIQUE (household_id, name)
	// -- archived rows included -- surfaces as domain.ErrBillNameTaken. The
	// returned record's Bill.Amount.Currency comes from the pay-from account,
	// per BillRecord's own comment -- NewBillRow carries no currency of its
	// own for Create to fall back on.
	Create(ctx context.Context, in NewBillRow) (BillRecord, error)
	// Update replaces every mutable column. BillService is what turns a
	// partial PATCH into a complete domain.Bill; this port never merges. Same
	// collision contract as Create.
	Update(ctx context.Context, b domain.Bill) (BillRecord, error)
	// SetArchived stamps archived_at with at, or clears it when archived is
	// false, and returns the bill as it now stands -- the same
	// at-supplied-by-the-caller convention AccountRepository.SetArchived and
	// GoalRepository.SetArchived use, returning the record (BillRecord here,
	// rather than a bare domain.Bill, so the joined names come with it) as
	// they do rather than a bare error. Every 2xx except 204 carries a JSON
	// body in this product, so a bare error would force the archive handler
	// into a second Get purely to build its response.
	SetArchived(ctx context.Context, householdID, billID string, archived bool, at time.Time) (BillRecord, error)
	// RecordPayment writes the bill_payments row, the expense transaction and
	// the advanced next_due in ONE database transaction. A bill left advanced
	// with no payment, or a payment with no expense, is not a state this port
	// can produce. An occurrence already paid surfaces as
	// domain.ErrAlreadyExists, from UNIQUE (bill_id, due_on).
	RecordPayment(ctx context.Context, in PaymentWrite) (BillPaymentRecord, error)
	// UndoPayment deletes the payment, deletes its transaction when the link
	// still points at one, and rewinds next_due to the payment's due_on -- in
	// ONE database transaction, all three or none.
	//
	// It refuses any payment that is not the bill's most recent, with
	// *domain.BillPaymentNotLatestError (whose Unwrap is domain.ErrForbidden,
	// so a caller matching the bare sentinel still works): undoing an older
	// one would rewind next_due behind a period that is still paid, and the
	// screen would show a due date for money already spent. The error itself
	// carries the due date that WOULD have been accepted, so the HTTP layer
	// can name it rather than answering a bare, contextless refusal.
	UndoPayment(ctx context.Context, householdID, billID, paymentID string) error
	// ListPayments returns one household's payments whose due_on falls in the
	// month containing `month`, newest paid_on first, ties by bill name.
	ListPayments(ctx context.Context, householdID string, month time.Time) ([]BillPaymentRecord, error)
	// MonthTotals returns the two figures the stat cards pair: paidMinor is
	// the sum of payments due in the month, and dueMinor is that plus every
	// unarchived bill still due in it. See this interface's own header comment
	// for why the second cannot come from bills alone.
	//
	// Both are per-currency, keyed by the pay-from account's currency, because
	// a household can hold accounts in more than one. The service converts and
	// adds; the repository never does money arithmetic across currencies.
	MonthTotals(ctx context.Context, householdID string, month time.Time) (dueMinor, paidMinor map[string]int64, err error)
}

// GoalProgress is the only thing Vision needs to know about a goal: what to
// call it and how far along it is. Percent is already
// domain.GoalProgressPercent's own capped 0-100 figure -- Vision does not
// recompute it, because a second percent formula in this codebase is exactly
// the kind of drift the Money specs spent five features avoiding.
type GoalProgress struct {
	GoalID  string
	Name    string
	Percent int
}

// GoalProgressReader is one method wide on purpose. VisionService needs the
// progress of a handful of goal ids; handing it GoalRepository -- whose own
// contract runs to forty lines about contribution scoping -- would be
// interface segregation traded away for one percentage.
type GoalProgressReader interface {
	// ProgressByIDs returns an entry, keyed by goal id, for each id that
	// exists in THIS household. A missing id is a miss, not an error: a
	// measure whose goal was deleted renders as a label with no figure, and
	// making that an error path would turn an ordinary page render into a
	// failure. Scoped by householdID in SQL, so a goal in another household
	// is indistinguishable from one that does not exist.
	//
	// An archived goal counts as found and keeps its figure: archiving is
	// not deletion anywhere else in this product either (spec decision 8),
	// and only a real DELETE unlinks a measure by firing goals.id's
	// ON DELETE SET NULL into vision_measures.goal_id. The implementing SQL
	// must NOT filter on archived_at -- unlike GoalRepository.List, which
	// takes an explicit includeArchived switch because its callers sometimes
	// want live goals only, this method has no such caller: Vision always
	// wants the figure a linked measure is pointing at, archived or not.
	ProgressByIDs(ctx context.Context, householdID string, goalIDs []string) (map[string]GoalProgress, error)
}

// VisionRepository stores one household's per-year visions. Every method is
// scoped by householdID and must filter on it in SQL.
type VisionRepository interface {
	// Get reports domain.ErrNotFound when the household has no vision for
	// that year. Turning that into the empty vision the screen renders is
	// VisionService's job, not this one's -- a repository that invented a
	// row would make "never set" and "set to blank" indistinguishable here.
	Get(ctx context.Context, householdID string, year int) (domain.Vision, error)
	// Save replaces the whole document in ONE transaction: upsert the parent,
	// delete every child, insert the submitted ones. Partial success must be
	// impossible -- the same transactional shape BudgetRepo.Upsert uses, and
	// ONLY that shape: Budget carries no version and no concurrency guard at
	// all, so its unconditional ON CONFLICT DO UPDATE is the right move
	// there and the wrong one here. Reaching for that same clause on Vision's
	// create path would silently destroy the guard the next paragraph
	// describes.
	//
	// Concurrency, in two cases that must not be collapsed:
	//   v.Version == 0  -- a create. Succeeds only while that household-year
	//                      has no row; reports domain.ErrVisionChanged if one
	//                      appeared since the caller read the empty vision.
	//                      The created row lands at version 1.
	//   v.Version  > 0  -- an update, WHERE version = v.Version. Zero rows
	//                      affected means either the vision was deleted or
	//                      the other partner saved first, and those are
	//                      different answers: re-read to tell them apart and
	//                      report domain.ErrNotFound or
	//                      domain.ErrVisionChanged accordingly.
	//                      RetroRepo.Update's own comment explains why the
	//                      cheap second read is worth it.
	//
	// Either way, the domain.Vision returned on success carries the version
	// AS STORED after the write -- 1 for a create, the stored value plus one
	// for an update -- the same contract RetroRepository.Update documents:
	// the caller never has to guess what to send on the next save.
	//
	// A measure naming a goal outside this household must be refused with
	// domain.ErrVisionGoalUnknown, checked INSIDE the transaction: the
	// vision_measures FK only proves a goal exists somewhere, never that it
	// is this household's -- the same hole validateLineCategories closes for
	// budget lines.
	Save(ctx context.Context, v domain.Vision) (domain.Vision, error)
}
