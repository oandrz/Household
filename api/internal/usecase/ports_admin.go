// This file holds the platform-admin slice's ports: admins, feature flags,
// the audit log, re-authentication, the household directory, the database
// browse and the outbound mail inspector. ports.go lists every ports file.

package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

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

// AdminAuditRepository is write-only on purpose. The log is append-only by
// convention and nothing in the product reads it back: the audit screen was
// descoped on 2026-09-02, and the read path it left behind was deleted on
// 2026-09-13 rather than kept alive only for its own tests. An operator reads
// admin_audit_log through psql; tests read the table directly.
type AdminAuditRepository interface {
	Record(ctx context.Context, entry AdminAuditEntry) error
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

// PendingInvite is the operator admin directory's own, differently-shaped
// view of an invite -- read-only, no ID, never withdrawn from this screen.
// See InviteSummary's doc comment for why the two keep separate names
// despite the design doc calling both "PendingInvite".
type PendingInvite struct {
	Name  string
	Email string // "" for a Telegram invite, which has no address
	Role  domain.Role
	// Channel is domain.InviteChannel, not the MemberChannel above: it comes
	// straight off the invite row's own channel column (via
	// domain.ParseInviteChannel), not derived from a telegram_accounts join
	// the way a member's channel is.
	Channel       domain.InviteChannel
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
