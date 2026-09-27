// This file holds the channel slice's ports: Telegram sending, account
// linking, the invite hand-offs between the bot and InviteService, and
// nudges. ports.go lists every ports file.

package usecase

import (
	"context"
	"time"
)

// TelegramSender delivers a plain-text message to one Telegram chat. Same
// shape and justification as Mailer: the usecase layer must not hold an HTTP
// client, and the service must be testable against a double.
type TelegramSender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
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
// for -- there is no separate status column.
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
	// sign-in nonce -- the browser has not said who it is -- or a user id
	// for a link nonce minted by a signed-in member for their own account;
	// that is the only thing separating the two kinds of row, so dropping it
	// would silently turn a link into a sign-in. The id is returned because
	// TelegramLinkService.Start hands it straight back to the browser to
	// poll with, the only moment it exists to return; a later lookup by
	// nonce_hash would be a second way to address a row by its secret.
	Create(ctx context.Context, userID string, nonceHash []byte, expiresAt time.Time) (string, error)
	// Consume stamps the row consumed and records which chat redeemed it, in
	// one statement, returning the row's id and the user it was minted for.
	// The chat is unknown at mint time -- the browser has not met Telegram
	// yet -- so redemption is the only moment the two can be joined, and
	// CountLinksSince depends on it happening here. Returns domain.ErrNotFound
	// if the nonce is unknown, expired or already consumed; those three are
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
// Hearth user it belongs to. Bindings are written in exactly three places:
// inside SignupRepository.Provision's transaction, when a stranger creates a
// household from a chat; inside InviteRepo.Admit's own transaction, when an
// owner lets a knocked Telegram invite in -- both write it themselves because
// the write has to be inside a transaction this port cannot join; and by
// TelegramLinkService.Confirm, when an existing member connects their chat
// from Settings. Both directions are UNIQUE in the database -- one chat per
// user, one user per chat -- and that constraint, not any check in Go, is
// what makes a sign-in unambiguous.
type TelegramAccountRepository interface {
	// ByChatID returns domain.ErrNotFound when the chat is bound to no user,
	// which is the ordinary "this person has no account yet" case, not an error
	// condition.
	ByChatID(ctx context.Context, chatID int64) (userID string, err error)
	// ByUserID returns domain.ErrNotFound when this user has no chat bound.
	ByUserID(ctx context.Context, userID string) (TelegramBinding, error)
	// Create returns domain.ErrAlreadyExists for either UNIQUE -- one chat per
	// user, one user per chat -- without distinguishing which one collided:
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

// InviteKnocker is the one thing TelegramAuthService needs from the
// invite side: turn a raw invite token and a chat into a code to show,
// or refuse. InviteService implements it. The split keeps every word
// the bot says inside TelegramAuthService and every invite rule inside
// InviteService.
type InviteKnocker interface {
	// Knock records the first tap on a Telegram invite link and returns the four
	// digits to show the tapper. Every refusal about the *link* -- unknown,
	// expired, accepted, already knocked, email-channel -- is
	// domain.ErrNotFound: one bland reply for all of them, so a chat cannot tell
	// them apart by probing. The exception is domain.ErrChatAlreadyBound, for a
	// chat already bound to a Hearth account -- safe to name plainly, since it
	// reveals only the tapper's own chat state, which /start with no payload
	// would tell them anyway.
	Knock(ctx context.Context, rawToken string, chatID int64, username string) (code string, err error)
}

// InviteChats is what InviteService needs from the Telegram side: the two
// messages an invite causes. TelegramAuthService implements it, reusing its
// own sendSignIn so no second magic-link path exists. The split mirrors
// InviteKnocker in the opposite direction; main.go closes that cycle with
// InviteService.SetChats.
type InviteChats interface {
	// SendSignIn delivers an ordinary magic link to a chat that has just
	// been admitted. It is called after the commit, never inside it.
	SendSignIn(ctx context.Context, chatID int64, userID string) error
	// SendLinkCancelled tells a chat that knocked that its link is no
	// longer valid, because the owner asked for a new one.
	SendLinkCancelled(ctx context.Context, chatID int64) error
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
