// Package usecase holds the application services. It depends on domain and on
// the port interfaces declared here — never on an adapter.
package usecase

import (
	"context"
	"errors"
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

// TelegramSender delivers a plain-text message to one Telegram chat. Same
// shape and justification as Mailer: the usecase layer must not hold an HTTP
// client, and the service must be testable against a double.
type TelegramSender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
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
	// AdminGrantExpiresAt is nil for every ordinary session. It is non-nil
	// only between a successful POST /admin/session and that grant's expiry.
	AdminGrantExpiresAt *time.Time
	// LastSeenAt is nil until the first Touch after migration 00013.
	// Readers treat nil as "use CreatedAt" -- see the migration's comment.
	LastSeenAt *time.Time
}

type SessionRepository interface {
	Create(ctx context.Context, tokenHash []byte, userID, householdID string, expiresAt time.Time) error
	ByTokenHash(ctx context.Context, tokenHash []byte) (SessionRecord, error)
	Extend(ctx context.Context, tokenHash []byte, expiresAt time.Time) error
	RevokeByToken(ctx context.Context, tokenHash []byte) error
	RevokeAllForUser(ctx context.Context, userID string) error
	// GrantAdmin stamps this session's admin re-auth grant. A nil expiry
	// clears it. It writes one column: session extension and this must not
	// overwrite each other.
	GrantAdmin(ctx context.Context, tokenHash []byte, expiresAt *time.Time) error
	// Touch records that the session was used at `at`. It writes one
	// column, last_seen_at, and must not be folded into Extend or
	// GrantAdmin: each of the three owns its column so none can overwrite
	// another's. Callers throttle it (middleware_session.go); the
	// repository does not.
	Touch(ctx context.Context, tokenHash []byte, at time.Time) error
}

type MagicLinkRepository interface {
	Create(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time) error
	Consume(ctx context.Context, tokenHash []byte) (userID string, err error)
	CountSince(ctx context.Context, email string, since time.Time) (int, error)
}

// TelegramLinkRedemption is what Consume hands back: the row's id and the
// user it was minted for, "" for a sign-in nonce. The caller's next decision
// -- link, sign in, or sign up -- is exactly this pair.
type TelegramLinkRedemption struct {
	ID     string // the telegram_link_requests row id
	UserID string // "" for a sign-in nonce; set for a link nonce
}

// TelegramLinkRequest is one row of telegram_link_requests, read back for the
// browser that minted it. Consumed, carrying a UserID, with no
// telegram_accounts row yet, is the pending state a confirm screen polls
// for -- there is no separate status column (see decision 5 of the linking
// design).
type TelegramLinkRequest struct {
	ID           string
	UserID       string
	ChatID       int64
	ChatUsername string
	Consumed     bool
	ExpiresAt    time.Time
}

// TelegramLinkRepository stores the pending deep-link nonces that carry a
// browser's sign-in request across to Telegram. Nonces are stored hashed,
// never raw, like every other token in this system.
type TelegramLinkRepository interface {
	// Create stores a nonce and returns the new row's id. userID is "" for a
	// sign-in nonce -- the browser has not said who it is -- and a user id for
	// a link nonce minted by a signed-in member for their own account. That
	// difference is the only thing separating the two kinds of row, so a
	// Create that dropped it would silently turn a link into a sign-in. The
	// id is returned because TelegramLinkService.Start hands it straight back
	// to the browser to poll with -- Create is the only moment it exists to
	// return; a later lookup by nonce_hash would be a second way to address a
	// row by its secret.
	Create(ctx context.Context, userID string, nonceHash []byte, expiresAt time.Time) (string, error)
	// Consume stamps the row consumed and records which chat redeemed it, in one
	// statement, and returns the row's id and the user it was minted for. The
	// chat is unknown when the nonce is minted -- the browser has not met
	// Telegram yet -- so redemption is the only moment the two can be joined, and
	// CountLinksSince depends on it happening here. Returns domain.ErrNotFound if
	// the nonce is unknown, expired or already consumed; those three are
	// deliberately indistinguishable to a caller.
	Consume(ctx context.Context, nonceHash []byte, chatID int64, chatUsername string) (TelegramLinkRedemption, error)
	// ByID reads one link request for the browser that minted it. The caller must
	// check the row's UserID against the session's own before showing anything:
	// this method deliberately does not, because a repository that enforced
	// ownership would be a second place authorisation lives (ADR 8).
	ByID(ctx context.Context, id string) (TelegramLinkRequest, error)
	// CountMintsSince counts link nonces this user has minted since a point in
	// time, consumed or not. Bounded table growth, not a security control -- the
	// session is already authenticated.
	CountMintsSince(ctx context.Context, userID string, since time.Time) (int, error)
	// CountLinksSince counts links this chat has redeemed since a point in
	// time. It lives here rather than on TelegramAccountRepository because the
	// per-chat limit must also bind chats that have no account yet: a stranger
	// repeating /start has no user row to count against.
	CountLinksSince(ctx context.Context, chatID int64, since time.Time) (int, error)
	// Prune deletes consumed and expired rows older than before, the same
	// contract as SignupRepository.Prune. A nonce nobody ever redeemed has no
	// chat_id, so nothing else ever bounds how many a stranger can mint.
	Prune(ctx context.Context, before time.Time) (int64, error)
}

// TelegramBinding is one chat bound to one Hearth user.
type TelegramBinding struct {
	UserID       string
	ChatID       int64
	ChatUsername string
	LinkedAt     time.Time
}

// TelegramAccountRepository is the binding between a Telegram chat and the
// Hearth user it belongs to. Bindings are written in two places and nowhere
// else: inside SignupRepository.Provision's transaction, when a stranger
// creates a household from a chat, and by TelegramLinkService.Confirm, when
// a member who already has an account connects their chat from Settings.
// Both directions are UNIQUE in the database -- one chat per user, one user
// per chat -- and that constraint, not any check in Go, is what makes a
// sign-in unambiguous.
type TelegramAccountRepository interface {
	// ByChatID returns domain.ErrNotFound when the chat is bound to no user,
	// which is the ordinary "this person has no account yet" case, not an error
	// condition.
	ByChatID(ctx context.Context, chatID int64) (userID string, err error)
	// ByUserID returns domain.ErrNotFound when this user has no chat bound.
	ByUserID(ctx context.Context, userID string) (TelegramBinding, error)
	// Create returns domain.ErrAlreadyExists for either UNIQUE -- one chat per
	// user, one user per chat. Which of the two collided is not distinguished:
	// the caller knows which side it was asking about (a fresh sign-up binds a
	// chat that must be free; Confirm binds a user who must have no chat yet)
	// and chooses the sentence, rather than a repository guessing at intent.
	// b.LinkedAt is ignored -- the store assigns it, the same as any other
	// created-at column.
	Create(ctx context.Context, b TelegramBinding) error
	// Delete is idempotent: removing a binding that is not there is not an
	// error, because the caller's goal -- this user has no chat -- is already
	// true.
	Delete(ctx context.Context, userID string) error
}

// NudgeRecipient is one chat that may receive one household's daily digest:
// an owner with the money capability whose chat has not opted out. The
// repository's query is the authorisation for this outbound direction (ADR 8):
// a digest carries money, so it goes only where /balance would already be
// answered.
type NudgeRecipient struct {
	ChatID       int64
	HouseholdID  string
	MembershipID string
	Currency     string
}

// NudgeRepository is the at-most-once ledger behind the daily digest, plus the
// per-chat opt-out. Claim is insert-first, the same shape as the transaction
// idempotency key: the row is written before the message is sent, and the
// primary key decides who sends.
type NudgeRepository interface {
	Recipients(ctx context.Context) ([]NudgeRecipient, error)
	// Claim returns false when a delivery for this chat, household and day
	// already exists -- sent, or being sent by another tick. The caller
	// skips; it never sends on false.
	Claim(ctx context.Context, chatID int64, householdID string, day time.Time) (bool, error)
	// Release deletes the claim after a failed send so the next tick may
	// try again. A claim that is not released stands for the whole day,
	// which is what "nothing to say today" also leaves behind on purpose.
	Release(ctx context.Context, chatID int64, householdID string, day time.Time) error
	// SetEnabled is /nudges on|off. domain.ErrNotFound when the chat is not
	// bound to anyone, which the Commander's guard has already ruled out.
	SetEnabled(ctx context.Context, chatID int64, enabled bool) error
	// Prune deletes delivery rows for days before the given one.
	Prune(ctx context.Context, before time.Time) (int64, error)
}

// APITokenRepository stores a member's long-lived credentials. Only the
// hash of a token is ever persisted; the raw value is shown once and never
// written anywhere.
type APITokenRepository interface {
	// Create stores one token and returns the row. tokenHash is
	// TokenGenerator.HashToken of the raw secret; prefix is the raw
	// secret's opening characters, for listing.
	Create(ctx context.Context, tokenHash []byte, prefix string, t domain.APIToken) (domain.APIToken, error)
	// ByTokenHash resolves a live token: revoked or expired is
	// domain.ErrNotFound, indistinguishable from unknown. The middleware
	// depends on that -- it never checks expiry itself.
	ByTokenHash(ctx context.Context, tokenHash []byte) (domain.APIToken, error)
	// ListForUser returns one person's live tokens, newest first.
	ListForUser(ctx context.Context, userID string) ([]domain.APIToken, error)
	// Revoke stamps one token, scoped to its owner: another user's id, an
	// unknown id and an already-revoked token are all domain.ErrNotFound.
	Revoke(ctx context.Context, userID, tokenID string) error
	// RevokeAllForUser is the "this person is gone" call MemberService
	// makes beside SessionRepository.RevokeAllForUser.
	RevokeAllForUser(ctx context.Context, userID string) error
	// Touch records the last use. Callers throttle it; the repository
	// does not.
	Touch(ctx context.Context, tokenID string, at time.Time) error
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

// The platform admin ports. Platform admin is an axis orthogonal to
// household Role and Capabilities (see domain/admin.go): these repositories
// answer "who runs this install", never "what may this member do".
//
// Nothing here decides whether a caller is allowed to do something. The HTTP
// layer's requirePlatformAdmin does that. Where a userID is passed into a
// write below it is stored -- in updated_by, or in the audit row -- and never
// consulted for permission.

type PlatformAdminRepository interface {
	Get(ctx context.Context, userID string) (domain.PlatformAdmin, error)
	Grant(ctx context.Context, userID, note string) error
	Revoke(ctx context.Context, userID string) error
	List(ctx context.Context) ([]PlatformAdminListing, error)
}

type PlatformAdminListing struct {
	UserID      string
	Email       string
	DisplayName string
	Note        string
	CreatedAt   time.Time
}

type FeatureFlagRepository interface {
	OverridesFor(ctx context.Context, householdID string) (global, household map[string]bool, err error)
	GlobalOverrides(ctx context.Context) (map[string]bool, error)
	AllHouseholdOverrides(ctx context.Context) ([]HouseholdFlagOverride, error)
	SetGlobal(ctx context.Context, key string, enabled bool, updatedBy string) error
	SetHousehold(ctx context.Context, householdID, key string, enabled bool, updatedBy string) error
	ClearHousehold(ctx context.Context, householdID, key string) error
}

type HouseholdFlagOverride struct {
	HouseholdID   string
	HouseholdName string
	Key           string
	Enabled       bool
}

type AdminAuditEntry struct {
	ActorUserID string
	Action      string
	Target      string
	Detail      map[string]any
	IP          string
	At          time.Time
}

type AdminAuditRepository interface {
	Record(ctx context.Context, entry AdminAuditEntry) error
	Recent(ctx context.Context, limit int) ([]AdminAuditEntry, error)
}

type AdminReauthAttemptRepository interface {
	Record(ctx context.Context, userID string, succeeded bool, at time.Time) error
	FailuresSince(ctx context.Context, userID string, since time.Time) ([]time.Time, error)
	ClearFailures(ctx context.Context, userID string) error
}

// AdminDirectoryRepository is the operator's read-only view across every
// household. It is the only port in the product that reads across
// household boundaries; every other repository answers for one household.
// Nothing on it writes. Its consumer is AdminDirectoryService and its
// callers are guarded in the HTTP layer alone.
type AdminDirectoryRepository interface {
	// Metrics answers the four counters on the households page. The
	// cutoffs are passed in rather than computed here so the service's
	// clock is the only clock (see AdminDirectoryService).
	Metrics(ctx context.Context, activeSince, signupsSince, now time.Time) (DirectoryMetrics, error)

	// SearchHouseholds returns up to limit households matching q, most
	// recently active first, never-active last. An empty q matches every
	// household. The predicate is the spec's §4: case-insensitive substring
	// over household name, family name, member display name and member
	// email. The caller passes limit+1 to learn whether more exist.
	SearchHouseholds(ctx context.Context, q string, limit int, now time.Time) ([]HouseholdListing, error)

	// Household returns one household with its members and the invites
	// still pending at now. A missing household is domain.ErrNotFound.
	Household(ctx context.Context, householdID string, now time.Time) (HouseholdDetail, error)
}

type DirectoryMetrics struct {
	Households       int
	ActiveHouseholds int
	SignupsRequested int
	SignupsCompleted int
	PendingInvites   int
}

type HouseholdListing struct {
	ID              string
	Name            string
	FamilyName      string
	MemberCount     int
	CreatedAt       time.Time
	LastActiveAt    *time.Time // nil when no member has ever had a session
	PrimaryCurrency string
	// Match names the member whose name or email matched the search when
	// the household's own name and family name did not. Nil otherwise, and
	// always nil for an empty search.
	Match *MemberMatch
}

type MemberMatch struct {
	Name  string
	Email *string // nil for a Telegram-only member
}

type HouseholdDetail struct {
	ID              string
	Name            string
	FamilyName      string
	CreatedAt       time.Time
	PrimaryCurrency string
	Members         []HouseholdMember
	PendingInvites  []PendingInvite
}

// MemberChannel is how a member signs in. The repository sets it from the
// telegram_accounts join, never by inferring from a NULL email: a user
// with neither is a bug the screen should surface, not a state it names.
type MemberChannel string

const (
	ChannelEmail    MemberChannel = "email"
	ChannelTelegram MemberChannel = "telegram"
)

type HouseholdMember struct {
	UserID       string
	Name         string
	Email        *string
	Channel      MemberChannel
	Role         domain.Role
	Capabilities domain.Capabilities
	LastActiveAt *time.Time
}

type PendingInvite struct {
	Name          string
	Email         string
	Role          domain.Role
	InvitedByName string
	ExpiresAt     time.Time
}

// DatabaseBrowser is a read-only, structural view of the database, for the
// operator's admin surface.
//
// Every value it returns is already rendered as text: no driver type, no
// `any`, and nothing a caller could accidentally write through. That is not
// only the clean-architecture rule -- it is what lets the implementation
// render a redacted column as a constant inside its own SELECT list, so the
// secret bytes never leave Postgres at all (the spec's decision 7).
//
// An implementation must:
//   - answer domain.ErrNotFound for a table it cannot see, whether because
//     the name does not exist or because its own role has no privilege on it;
//   - answer ErrBrowseUnavailable for anything else that is a failure of the
//     connection rather than of the request;
//   - never write, and never be reachable through a connection that could.
type DatabaseBrowser interface {
	Tables(ctx context.Context) ([]TableInfo, error)
	Rows(ctx context.Context, table string, limit, offset int) (RowPage, error)
}

// TableInfo is one table as the list screen shows it.
type TableInfo struct {
	Name     string
	RowCount int64
	Columns  []ColumnInfo
}

// ColumnInfo describes one column. Redacted is returned to the screen rather
// than being kept private to the implementation, so the operator can see that
// a column was withheld rather than empty -- "there is no value here" and
// "you may not see the value here" are different facts and the screen must
// not merge them.
//
// DataType is a type name for a human to read, not a catalogue value to
// branch on: "citext", "text[]", "timestamp with time zone". An
// implementation reading information_schema must not put data_type here
// unexamined, because that column reports a category rather than a name for
// arrays and for domains -- see adapter/postgres's displayType.
type ColumnInfo struct {
	Name     string
	DataType string
	Redacted bool
}

// RowPage is one page of one table.
//
// Rows is column-ordered text, parallel to Columns: a Postgres table may
// carry two columns whose names differ only in ways a JSON object key would
// not preserve, and the column list is being sent anyway. A cell is never a
// bare empty string where the value was absent -- see domain.NullCell.
type RowPage struct {
	Columns []ColumnInfo
	Rows    [][]string
	Total   int64
	Limit   int
	Offset  int
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

// SignupDetails is a pending sign-up, read back by token. Exactly one of
// Email and TelegramChatID is set -- the signups_have_exactly_one_channel
// constraint makes that a database guarantee, not a convention.
type SignupDetails struct {
	ID             string
	Email          string
	TelegramChatID *int64
	ExpiresAt      time.Time
	ConsumedAt     *time.Time
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
	// CreateForTelegram writes a signup row whose channel is a Telegram chat
	// rather than an email address. It and Create are mutually exclusive per
	// row, enforced by signups_have_exactly_one_channel.
	CreateForTelegram(ctx context.Context, chatID int64, tokenHash []byte, expiresAt time.Time) error
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
	// every builtin space and the notification preferences, binds the
	// Telegram chat when the signup names one, and stamps the signup
	// consumed -- all in one transaction. Either all of it happens or none of
	// it does.
	//
	// The owner's email address, or the Telegram chat id, is read from the
	// signup row this transaction is already touching; neither is a
	// parameter, deliberately. The identity that gets an account must be the
	// one the token actually proved -- the mailed link for email, the chat
	// that redeemed the sign-up for Telegram -- and passing either in would
	// let a caller substitute a different one between SignupService.Complete's
	// read and this write. For the chat id this is the whole point of the
	// feature: a caller-substitutable chat id would let someone bind a
	// household to a chat they control, not the one that actually completed
	// the sign-up.
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
	// category, payer, and whichever account side the kind leaves empty --
	// and for t.IdempotencyKey, which is stored NULL when "". t.ID is
	// ignored; the database assigns it. A non-empty key this household has
	// already stored reports domain.ErrIdempotencyKeyInUse and writes
	// nothing; the service decides whether that is a replay.
	Create(ctx context.Context, t domain.Transaction) (domain.Transaction, error)
	// GetByIdempotencyKey is the replay lookup after Create reported
	// ErrIdempotencyKeyInUse. Household-scoped: another household's row
	// under the same key is domain.ErrNotFound, indistinguishable from no
	// row at all.
	GetByIdempotencyKey(ctx context.Context, householdID, key string) (domain.Transaction, error)
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

// AgreementSectionRecord is one stored section: a label, not a promise --
// creating one is immediate and unsigned (decision 8).
type AgreementSectionRecord struct {
	ID, Name  string
	CreatedAt time.Time
}

// AgreementRecord is one LIVE agreement. A removed one never appears here:
// removal is a stamp, not a delete (decision 9), and removed_at IS NULL
// belongs in SQL, never in a caller.
type AgreementRecord struct {
	ID, SectionID, Body, AddedByProposalID string
	CreatedAt                              time.Time
}

// AgreementProposalRecord is one proposed change as stored. Kind and Status
// hold the column re-parsed, never cast. ResolvedAt is a pointer because
// "still open" is a state and the zero time is not a moment.
// SignedByMembershipIDs may hold a signature from someone who is no longer an
// owner: a true record that stops counting (decision 4).
type AgreementProposalRecord struct {
	ID, Kind, Status, SectionID, TargetAgreementID, Body, PreviousBody, Note, ParkNote,
	ProposedByMembershipID string
	CreatedAt             time.Time
	ResolvedAt            *time.Time
	SignedByMembershipIDs []string
}

// AgreementDocument is the whole screen in one read. Open and Accepted are
// two slices because they are in two orders and one cannot hold both;
// withdrawn proposals appear in neither, excluded in SQL. Every slice
// non-nil, so a caller ranges over it without a check.
type AgreementDocument struct {
	Sections   []AgreementSectionRecord
	Agreements []AgreementRecord         // live only, removed_at IS NULL
	Open       []AgreementProposalRecord // pending and parked, created_at asc
	Accepted   []AgreementProposalRecord // accepted only, resolved_at asc
}

// AgreementProposalWrite is one propose. HouseholdID and
// ProposedByMembershipID are stamped by the service from the route and the
// session, never read from a request body. SectionID is set only for an add:
// on an edit or a remove the repository copies the target's own section, so
// there is nothing here for a caller to get wrong.
type AgreementProposalWrite struct {
	HouseholdID, Kind, SectionID, TargetAgreementID, Body, PreviousBody, Note,
	ProposedByMembershipID string
	CreatedAt time.Time
}

// AgreementSignatureWrite is one Agree. MembershipID is STORED, never
// consulted for permission -- PaymentWrite.PaidByMembershipID's contract.
type AgreementSignatureWrite struct {
	HouseholdID, ProposalID, MembershipID string
	At                                    time.Time
}

// AgreementRepository stores one household's agreements, the sections that
// group them, and the append-only log of every change ever proposed. Every
// method filters on householdID in SQL: another household's row must be
// indistinguishable from one that does not exist. Ordering is created_at, id
// everywhere except AgreementDocument.Accepted, which is resolved_at, id --
// a version is the k-th ACCEPTANCE, and two proposals created A then B can be
// accepted B then A. Nothing here reads a clock.
type AgreementRepository interface {
	// Document is the whole screen in one read, kind and status re-parsed and
	// never cast. Unbounded on purpose: a household writes a few agreements a
	// year, and the version, the history list and the Retros page's
	// To-discuss block all derive from more than one of its four slices.
	Document(ctx context.Context, householdID string) (AgreementDocument, error)
	// Proposal returns one proposal whatever its status, withdrawn included,
	// so the withdraw handler's proposer check costs one query rather than a
	// composed document. domain.ErrNotFound for an unknown or foreign id.
	Proposal(ctx context.Context, householdID, proposalID string) (AgreementProposalRecord, error)
	// CreateSection adds one label. A clash with UNIQUE (household_id, name)
	// is domain.ErrAgreementSectionNameTaken, mapped by CONSTRAINT NAME above
	// the generic 23505 case (decision 19), or the screen cannot tell "you
	// already have that section" from anything else that could collide.
	CreateSection(ctx context.Context, householdID, name string, createdAt time.Time) (AgreementSectionRecord, error)
	// CreateSections is "Use starter set" (decision 17): every name in ONE
	// transaction, ON CONFLICT DO NOTHING so a second click is a no-op rather
	// than a 409, then read back inside it. Two of four landing would leave a
	// household half-seeded with no button left to ask for the rest. The
	// read-back is how the caller proves all four landed; nothing renders
	// from its order, since the route answers with the whole freshly composed
	// document and render order is always the document's.
	CreateSections(ctx context.Context, householdID string, names []string, createdAt time.Time) ([]AgreementSectionRecord, error)
	// CreateProposal writes the proposal row AND the proposer's implicit
	// signature (decision 5), verifying the target first for an edit or a
	// remove, all in ONE transaction: either all of it happens or none of it
	// does. A proposal without its proposer's signature would ask both owners
	// to be the second signer of a set of one, and no route here could repair
	// it -- the failure InviteRepository.Accept's own comment describes. The
	// target must exist in this household, still be live, and its body must
	// equal PreviousBody exactly, compared as stored and never re-trimmed;
	// either failure is domain.ErrAgreementChanged with nothing written. The
	// target's section_id is copied onto the proposal in the same statement,
	// which is why domain.AgreementProposal.Validate refuses a
	// caller-supplied one on an edit or a remove. Zero rows on the proposer
	// lookup through memberships (this household, role = 'owner') rolls it
	// all back with domain.ErrForbidden.
	CreateProposal(ctx context.Context, in AgreementProposalWrite) (AgreementProposalRecord, error)
	// Sign records one Agree and, when that completes the signing set,
	// applies the change -- all in ONE transaction, on that transaction's OWN
	// connection: either all of it happens or none of it does. A pool-backed
	// call inside pgx.BeginFunc takes a second connection while the first is
	// held, and enough concurrent signers deadlock against MaxConns -- the
	// hang VisionRepo.Save shipped. It is the only method that writes an
	// agreements row. The set is every CURRENT owner, counted in this
	// transaction (decision 4), and the lock that matters is on the TARGET
	// agreement, not the proposal (decision 12); that target check runs
	// BEFORE the signature lands, or a middle signer's agreement is recorded
	// against wording that has already moved. Applying is a switch on kind
	// with a refusing default: an add inserts, a remove stamps, an edit does
	// both -- so an edit CHANGES the agreement's id, and the new row sorts
	// last in its section exactly as created_at, id puts it.
	// domain.ErrNotFound for an unknown id, domain.ErrAgreementNotOpen for a
	// resolved one, domain.ErrAgreementChanged when the target moved,
	// domain.ErrForbidden when the signer is not an owner here.
	Sign(ctx context.Context, in AgreementSignatureWrite) (AgreementProposalRecord, error)
	// Park is Discuss: the proposal stays OPEN and moves to the Retros page's
	// To-discuss block (decision 7); parking twice replaces the note. One
	// guarded UPDATE with the status condition in the WHERE clause, never a
	// service if, because a check-then-write races; zero rows is diagnosed by
	// one re-read -- gone is domain.ErrNotFound, resolved is
	// domain.ErrAgreementNotOpen, the re-read's own failure passes through
	// untouched. NOTHING here touches a retro table and there is no foreign
	// key to a retro row: the next retro usually does not exist yet, which is
	// exactly when a couple parks something.
	Park(ctx context.Context, householdID, proposalID, note string, at time.Time) (AgreementProposalRecord, error)
	// Withdraw is the same guarded UPDATE plus AND (proposed_by_membership_id
	// = $by OR that membership is no longer an owner here): proposer-only
	// until the proposer leaves, then any owner (decision 15). That clause is
	// a BACKSTOP -- the handler decides and answers first, and this method
	// never branches on $by. Four diagnose legs, in order: gone is
	// domain.ErrNotFound; resolved is domain.ErrAgreementNotOpen; open,
	// someone else's, and that someone still an owner is domain.ErrForbidden;
	// the re-read's own failure is folded into none of the other three.
	Withdraw(ctx context.Context, householdID, proposalID, byMembershipID string, at time.Time) (AgreementProposalRecord, error)
}

// ErrBrowseUnavailable is the database browse's "I could not reach the
// store", as distinct from domain.ErrNotFound's "the store answered, and
// there is no such table". The operator needs different advice in each case:
// the first is something to fix on the box, the second is a typo in a URL.
var ErrBrowseUnavailable = errors.New("database browse unavailable")

// ErrOutboxUnavailable means the outbox itself could not be read: it is
// unreachable, it timed out, or it answered something this code cannot map.
// It is declared here rather than in an adapter because it is part of the
// port's contract -- every implementation must be able to say "the store is
// there, I just could not reach it", and the HTTP layer answers 502 for it.
//
// It is deliberately distinct from domain.ErrNotFound, which means the outbox
// answered and does not hold that message. An operator needs different advice
// in each case: one is "Mailpit is down", the other is "that message has aged
// out of a store with no volume".
var ErrOutboxUnavailable = errors.New("the message outbox could not be read")

// MailOutbox reads messages the product has sent. It exists so the operator
// can hand someone a link that mail cannot deliver (see ADR 3). The only
// implementation today reads Mailpit; when mail leaves the box this port gets
// a second one instead of a rewrite.
//
// Message reports domain.ErrNotFound for a message the outbox does not hold.
// Both methods report ErrOutboxUnavailable when the outbox itself cannot be
// read.
//
// Neither method extracts anything: an implementation hands back the body
// parts exactly as the store gave them, and AdminOutboxService turns those
// into what a screen shows. That split is what keeps "which strings are
// links" testable without an HTTP server.
type MailOutbox interface {
	// Recent returns up to limit messages, newest first, with both body
	// fields left empty -- a list never carries a body, because a body can
	// contain a live single-use link and listing must not be the act that
	// reveals one.
	Recent(ctx context.Context, limit int) (OutboxPage, error)
	// Message returns one message including both body parts.
	Message(ctx context.Context, id string) (OutboxMessage, error)
}

// OutboxPage is one screenful of the outbox. Total is how many messages the
// outbox holds altogether, not how many were returned -- the screen says
// "showing 50 of 128", and a caller cannot infer that from a slice exactly as
// long as the limit it asked for.
type OutboxPage struct {
	Messages []OutboxMessage
	Total    int
}

// OutboxMessage is a message as the outbox holds it. Text and HTML are
// populated by Message and empty in Recent.
type OutboxMessage struct {
	ID      string
	To      string
	Subject string
	SentAt  time.Time
	Text    string
	HTML    string
}

// HoldingRecord is a holding joined to the account it sits in, which is what
// every consumer of the holdings list actually wants -- the same shape and the
// same reason as AccountView and MemberView above.
type HoldingRecord struct {
	Holding         domain.Holding
	AccountName     string
	AccountArchived bool
}

// HoldingRepository stores what a household owns inside its investment
// accounts. It is deliberately separate from AccountRepository and touches no
// balance: a holding is invisible to ListAccounts, to net worth and to the
// twelve-month trend, which is milestone 1's whole boundary. Making an
// account's balance read from holdings is a later, separate decision about
// whether an investment account also carries uninvested cash.
type HoldingRepository interface {
	// List returns one household's holdings ordered by name, each joined to
	// its account's nickname and whether that account is archived -- an
	// archived account's holdings are still real money and still listed, with
	// the account labelled. includeArchived is a UNION, not a filter swap:
	// false returns the live holdings, true returns live AND archived
	// together, each carrying its own ArchivedAt. The AccountRepository.List
	// and GoalRepository.List contract; do not implement it as "archived
	// instead".
	List(ctx context.Context, householdID string, includeArchived bool) ([]HoldingRecord, error)
	// Get reports domain.ErrNotFound when no holding with this id exists in
	// this household -- including when it exists in another one, so nothing
	// leaks the existence of another household's rows.
	Get(ctx context.Context, householdID, holdingID string) (domain.Holding, error)
	// Create reports domain.ErrHoldingNameTaken on a name collision within
	// the same ACCOUNT, archived holdings included.
	Create(ctx context.Context, h domain.Holding) (domain.Holding, error)
	// Update changes name, instrument and unit only. Currency and AccountID
	// are not mutable: a holding's currency is what every one of its events
	// is denominated in, and moving a holding between accounts would move
	// money between accounts with no ledger row to say so.
	Update(ctx context.Context, h domain.Holding) (domain.Holding, error)
	// SetArchived archives (non-nil) or restores (nil). A holding is never
	// deleted: its events and valuations reference it, and a sold-out
	// position is still part of the year's realised profit.
	SetArchived(ctx context.Context, householdID, holdingID string, archivedAt *time.Time) (domain.Holding, error)
	// CountLiveForAccount is what stops an account's type being changed out
	// from under its holdings. AccountService patches Type freely, so without
	// this a cash account could end up holding 300g of gold.
	CountLiveForAccount(ctx context.Context, householdID, accountID string) (int64, error)
}

// HoldingCounter is what services OUTSIDE this feature need to know about
// holdings, and nothing more: whether an account still holds anything, and
// whether the household holds anything at all. A narrow port rather than the
// whole HoldingRepository, by the same interface-segregation rule that gives
// this file nine small repositories instead of one object with forty methods
// -- and so that the accounts and household services cannot grow a dependency
// on holdings they were never meant to have.
type HoldingCounter interface {
	CountLiveForAccount(ctx context.Context, householdID, accountID string) (int64, error)
	// CountForHousehold counts ARCHIVED holdings too. An archived holding
	// still has events, and those events still have to fold -- archiving is
	// how a household stops looking at something, not how it forgets it.
	CountForHousehold(ctx context.Context, householdID string) (int64, error)
}

// HoldingEventRepository stores the acquisitions and disposals a holding is
// made of. Income (a dividend) is deliberately not among them: it changes
// neither what is held nor what it cost, so folding it here would corrupt the
// average cost.
type HoldingEventRepository interface {
	// ListByHolding returns one holding's events ordered by
	// (OccurredOn, CreatedAt, ID). THAT ORDER IS A CONTRACT, NOT A
	// PREFERENCE, and an implementation may not relax it.
	//
	// OccurredOn is a date, so two events can share one -- buying and selling
	// the same morning is ordinary -- and domain.Holding.Position sorts
	// STABLY, which means it keeps whatever order it is handed for a tie. The
	// tie is therefore broken here, by the order the events were actually
	// recorded in. Return them in any other order and a household's realised
	// gain changes silently: on identical same-day events, buy-then-sell
	// realises 750 where sell-then-buy realises 1000.
	ListByHolding(ctx context.Context, householdID, holdingID string) ([]domain.HoldingEvent, error)
	// ListByHousehold is the same contract across every holding, grouped by
	// holding, so a portfolio page folds every position without one query
	// per holding.
	ListByHousehold(ctx context.Context, householdID string) ([]domain.HoldingEvent, error)
	Insert(ctx context.Context, e domain.HoldingEvent) (domain.HoldingEvent, error)
	// InsertWithFold is Insert with the holding's invariant held ACROSS the
	// write, and it is what a service must use for anything the fold can
	// refuse. The implementation locks the holding, lists its events in the
	// same transaction, calls fold with them, and inserts only if fold returns
	// nil -- so a second writer blocks and then folds the first one's result
	// rather than a stale copy.
	//
	// Reading, folding and writing as three separate calls is NOT equivalent:
	// two sales of 30 from a holding of 50 would each pass and both commit,
	// leaving events that cannot be folded at all. fold is the caller's own
	// rule (domain.Holding.Position); this port owns the transaction and the
	// lock, never the rule.
	InsertWithFold(ctx context.Context, e domain.HoldingEvent, fold func([]domain.HoldingEvent) error) (domain.HoldingEvent, error)
	// DeleteWithFold is the same guarantee in the other direction: removing a
	// purchase a later sale was costed against must not be able to race a
	// concurrent write. fold receives the events that WOULD remain.
	DeleteWithFold(ctx context.Context, householdID, holdingID, eventID string, fold func([]domain.HoldingEvent) error) error
	// Delete reports domain.ErrNotFound when the event is not this
	// household's AND this holding's, rather than silently succeeding. Both
	// halves of that scope are load-bearing: the household keeps two families
	// apart, and the holding keeps the URL honest -- a caller naming holding A
	// must not be able to remove a row of holding B, whose fold would then
	// never have been checked.
	Delete(ctx context.Context, householdID, holdingID, eventID string) error
}

// HoldingValuationRepository stores what one unit of a holding was worth on a
// given day.
type HoldingValuationRepository interface {
	// ListByHolding returns one holding's valuations, newest first.
	ListByHolding(ctx context.Context, householdID, holdingID string) ([]domain.Valuation, error)
	// ListLatest returns at most one row per holding: the newest price each
	// has. A holding with NO valuation produces no row at all rather than a
	// zero one -- the caller reads an absent holding as "no price recorded",
	// which is what a screen must say instead of showing a figure of zero.
	ListLatest(ctx context.Context, householdID string) ([]domain.Valuation, error)
	// Upsert writes one price per holding per day: a second write for the
	// same AsOf replaces the first. Re-entering a day's price is a
	// correction, not a second opinion, and two rows for one day would leave
	// the report with no way to choose between them.
	// ListForHousehold returns EVERY valuation the household has, not one per
	// holding. The period report needs the whole history: a quarter opens at
	// a price recorded in the quarter before it, and ListLatest has already
	// discarded that one.
	ListForHousehold(ctx context.Context, householdID string) ([]domain.Valuation, error)
	// Upsert writes one price per holding per day: a second write for the
	// same AsOf replaces the first. Re-entering a day's price is a
	// correction, not a second opinion, and two rows for one day would leave
	// the report with no way to choose between them.
	Upsert(ctx context.Context, v domain.Valuation) (domain.Valuation, error)
	Delete(ctx context.Context, householdID, valuationID string) error
}

// HoldingIncomeRepository stores the dividends a holding paid and the charges
// made against it.
//
// Unlike HoldingEventRepository there is no ordering contract here and no
// fold-inside-the-write: income never enters the average-cost pool, so no
// invariant spans two rows, no order changes the answer, and nothing needs a
// lock. Any order is correct because summing a period is commutative.
type HoldingIncomeRepository interface {
	Insert(ctx context.Context, i domain.HoldingIncome) (domain.HoldingIncome, error)
	ListByHolding(ctx context.Context, householdID, holdingID string) ([]domain.HoldingIncome, error)
	ListByHousehold(ctx context.Context, householdID string) ([]domain.HoldingIncome, error)
	// Delete returns domain.ErrNotFound when the row is not this household's
	// and this holding's, never a silent success. See
	// HoldingEventRepository.Delete for why the holding is part of the scope
	// and not only the household.
	Delete(ctx context.Context, householdID, holdingID, incomeID string) error
}
