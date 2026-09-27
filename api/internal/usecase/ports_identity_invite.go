// This file holds the identity slice's invite ports: inviting a member, the
// Telegram knock, and the pairing code. ports.go lists every ports file.

package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

type InviteDetails struct {
	ID           string
	HouseholdID  string
	Email        string
	Name         string
	Role         domain.Role
	Capabilities domain.Capabilities
	// Channel is read by the public web routes, which serve only the email
	// channel: a Telegram invite is admitted by an owner in their own
	// browser, so the web form must answer its token as an unknown one.
	Channel     domain.InviteChannel
	FamilyName  string
	InviterName string
	ExpiresAt   time.Time
	AcceptedAt  *time.Time
}

// AcceptedInvite is what a successful acceptance produces.
type AcceptedInvite struct {
	UserID       string
	MembershipID string
	HouseholdID  string
}

// AdmittedMember is what Let in produced: the household's newest member,
// and whether the bot managed to hand them a sign-in link in their own
// chat. SignInSent is false when the member exists but the message could
// not be sent -- named rather than hidden, because the owner is the only
// person who can tell the new member to send /start themselves.
type AdmittedMember struct {
	MembershipID string
	UserID       string
	Name         string
	Role         domain.Role
	Capabilities domain.Capabilities
	SignInSent   bool
}

// AdmittedInvite is what InviteRepository.Admit produced inside its
// transaction: the user and membership it just created, and the chat to
// send their sign-in link to. ChatID travels back out this way rather than
// the caller re-reading the invite, because by the time Admit returns, the
// invite that carried it has already been stamped accepted.
type AdmittedInvite struct {
	UserID       string
	MembershipID string
	Name         string
	Role         domain.Role
	Capabilities domain.Capabilities
	ChatID       int64
}

// InviteKnock is one tap on a Telegram invite link: who tapped, and the
// four digits their chat was shown. The owner compares those digits with
// the ones on the phone in front of them and then admits (ADR 11).
//
// Code is display-only. No endpoint accepts it, so there is nothing to
// guess and nothing to rate-limit; a test pins that the admit request has
// no code field. Username is "" when Telegram sent none, which is ordinary
// -- a @username is optional -- and the screen says so in words rather than
// rendering an empty "@".
type InviteKnock struct {
	Username  string
	Code      string
	KnockedAt time.Time
}

// InviteSummary is one invite a household has sent that nobody has accepted
// and that has not expired: what Settings lists so an owner can see, and
// withdraw, what they sent. It carries no token -- the raw token is never
// stored, and the hash is nobody's business above the adapter.
//
// Named InviteSummary, not the design doc's "PendingInvite", because that
// name is already the operator admin directory's own, differently-shaped
// view of an invite (no ID, no Capabilities, read-only, never withdrawn) --
// different data for a different audience, so don't rename either to make
// room for the other.
type InviteSummary struct {
	ID           string
	Name         string
	Email        string // "" for a Telegram invite, which has no address
	Role         domain.Role
	Capabilities domain.Capabilities
	Channel      domain.InviteChannel
	// Knock is nil until someone taps the link, and always nil for an email
	// invite. A pointer rather than a zero-valued struct because "nobody has
	// knocked" and "somebody knocked at the zero time" must not look alike.
	Knock     *InviteKnock
	ExpiresAt time.Time
	CreatedAt time.Time
}

// PairingCodes draws the four digits an owner compares by eye. A port
// so tests are deterministic; crypto.PairCodes is the implementation.
type PairingCodes interface {
	// NewCode returns exactly four decimal digits, "0000" through
	// "9999", leading zeros kept.
	NewCode() (string, error)
}

type InviteRepository interface {
	Create(ctx context.Context, householdID, email, name string, role domain.Role,
		caps domain.Capabilities, tokenHash []byte, invitedBy string, expiresAt time.Time) (string, error)
	// CreateTelegram writes a telegram-channel invite: no email address at
	// all, which invites_channel_matches_email in migration 00021 requires
	// for this channel. Returns the new invite's id. A colliding token hash
	// reports domain.ErrAlreadyExists, exactly as Create does.
	CreateTelegram(ctx context.Context, householdID, name string, role domain.Role,
		caps domain.Capabilities, tokenHash []byte, invitedBy string, expiresAt time.Time) (string, error)
	ByTokenHash(ctx context.Context, tokenHash []byte) (InviteDetails, error)
	// LiveInviteForEmail answers "is there already something usable in
	// flight for this address in this household" -- neither accepted nor
	// expired -- without the raw token, which is never persisted anywhere.
	// It reports domain.ErrNotFound when there is none, exactly as
	// ByTokenHash does for an unknown token: "no live invite" and "no
	// invite at all" are the same absence from a caller's point of view.
	LiveInviteForEmail(ctx context.Context, householdID, email string) (InviteDetails, error)
	// Returns domain.ErrInviteAlreadyAccepted when the invite was already
	// accepted or has expired — the guard lives in the SQL, not in the caller.
	MarkAccepted(ctx context.Context, inviteID string) error
	// Accept creates the user, creates the membership, and marks the invite
	// accepted in one transaction: all three happen or none do. A partial
	// acceptance would leave an orphaned user occupying the unique email
	// index, making the invite permanently unusable -- a retry can never
	// create a second user with that address, and only manual SQL recovers
	// it. Returns domain.ErrInviteAlreadyAccepted, with nothing written,
	// when the invite was already accepted or has expired.
	Accept(ctx context.Context, inviteID, email, passwordHash, displayName string,
		householdID string, role domain.Role, caps domain.Capabilities) (AcceptedInvite, error)
	// ListPending returns householdID's invites that are neither accepted
	// nor expired as of now, oldest first. "Pending" has this one definition
	// everywhere (the partner-invite spec's Data section). An empty result
	// is an empty slice, never nil, so the HTTP layer encodes it as [].
	ListPending(ctx context.Context, householdID string, now time.Time) ([]InviteSummary, error)
	// Delete removes an invite nobody has accepted, scoped to householdID in
	// the SQL itself: an id from another household deletes nothing and
	// reports domain.ErrNotFound, the same as an id that never existed, so
	// the answer never confirms another household's invite exists. An
	// accepted invite is history rather than something to withdraw: it
	// reports domain.ErrInviteAlreadyAccepted with nothing deleted. An
	// expired, unaccepted invite is deletable.
	Delete(ctx context.Context, householdID, inviteID string) error
	// RecordKnock reports domain.ErrNotFound for every case its guarded
	// UPDATE does not match -- unknown token, email channel, accepted,
	// expired, or already knocked. That is deliberately one answer: the
	// caller turns it into the bot's one bland reply, and any difference
	// between these cases would be something a chat could probe for.
	RecordKnock(ctx context.Context, tokenHash []byte, chatID int64, username, code string, now time.Time) error
	// ReplaceToken is the single write behind "get a new link", also "Not them"
	// (see InviteService.NewLink for why one write serves both). It clears the
	// knock in the same statement that replaces the token, so no instant ever
	// has a fresh link carrying a stale knock, and returns the chat that had
	// knocked, 0 when nobody had, so the caller can tell them. Household- and
	// channel-scoped in the SQL itself: an id from another household matches
	// nothing, domain.ErrNotFound, the same answer an unknown id gets; an email
	// invite also matches nothing, answered as domain.ErrInviteNotTelegram
	// instead (see the postgres implementation for how it tells the two apart).
	// An already-accepted invite also reports domain.ErrNotFound, unlike
	// Delete's domain.ErrInviteAlreadyAccepted for the same state: "get a new
	// link" is not something an accepted invite offers, and the fallback read
	// is scoped by accepted_at IS NULL, so an accepted row looks identical to
	// one that does not exist. An expired invite, by contrast, IS replaceable:
	// expires_at is deliberately left out of the SQL, since an expired link is
	// the main reason an owner asks for a new one.
	ReplaceToken(ctx context.Context, householdID, inviteID string, tokenHash []byte, expiresAt time.Time) (knockedChatID int64, err error)
	// Admit is Let in: the user, the membership, the telegram_accounts binding
	// and the acceptance stamp, in one transaction. All four happen or none do
	// -- a failure between them would leave a user with no membership and no
	// email, and because nothing constrains that user's identity to be unique, a
	// retry would silently orphan another one each time rather than failing
	// loudly.
	//
	// The stamp runs first, for the same reason Accept's guard does: it makes a
	// concurrent Let in fail cheaply as domain.ErrInviteAlreadyAccepted before
	// any row is written, rather than failing later on the telegram_accounts
	// unique index with an unmappable error.
	//
	// Reports domain.ErrInviteNotKnocked when nobody has knocked (or a new link
	// cleared the knock since), domain.ErrInviteAlreadyAccepted when this invite
	// was already admitted, domain.ErrNotFound when there is no such invite in
	// this household, and domain.ErrChatAlreadyBound when the knocked chat bound
	// itself to a different account between the knock and this call. The
	// knock-time check cannot see that race, so the telegram_accounts UNIQUE
	// inside this transaction is what closes it.
	Admit(ctx context.Context, householdID, inviteID string, now time.Time) (AdmittedInvite, error)
}
