// This file holds the identity slice's self-serve sign-up ports. ports.go
// lists every ports file.

package usecase

import (
	"context"
	"time"
)

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
	// afterward. SignupService.Request calls this instead of Create on the
	// already-registered branch, so that branch still advances
	// CountForEmailSince/CountSince: those counters read every row
	// regardless of consumed_at, and skipping this write would silently
	// zero the rate limit for that branch again. A row created this way
	// can never provision anything -- Provision's guarded UPDATE
	// (ConsumeSignup) requires consumed_at IS NULL -- so it exists solely
	// to be counted, and its token is never mailed to anyone.
	CreateConsumed(ctx context.Context, email string, tokenHash []byte, expiresAt time.Time) error
	// CreateForTelegram writes a signup row whose channel is a Telegram chat
	// rather than an email address. It and Create are mutually exclusive per
	// row, enforced by signups_have_exactly_one_channel.
	CreateForTelegram(ctx context.Context, chatID int64, tokenHash []byte, expiresAt time.Time) error
	ByTokenHash(ctx context.Context, tokenHash []byte) (SignupDetails, error)
	// CountForEmailSince counts sign-up requests for one address since a
	// cutoff, over rows written by both Create and CreateConsumed. Unlike
	// MagicLinkRepository.CountSince it does not join through users -- there
	// is no user to join to -- so it must be able to report a non-zero
	// count for an address with no account: a limit only a registered
	// address could hit would itself distinguish the two.
	CountForEmailSince(ctx context.Context, email string, since time.Time) (int, error)
	// CountSince counts every sign-up request since a cutoff, for the global
	// daily mail ceiling. It reads the table rather than an in-memory counter
	// so restarting the API cannot reset the ceiling.
	CountSince(ctx context.Context, since time.Time) (int, error)
	// Provision creates the household, the owner user, the owner membership,
	// every builtin space and the notification preferences, binds the Telegram
	// chat when the signup names one, and stamps the signup consumed -- all in
	// one transaction, all of it or none.
	//
	// The owner's email, or the Telegram chat id, is read from the signup row
	// this transaction is already touching; neither is a parameter,
	// deliberately. The identity that gets an account must be the one the token
	// actually proved -- the mailed link for email, the chat that redeemed the
	// sign-up for Telegram -- and passing either in would let a caller
	// substitute a different one between SignupService.Complete's read and this
	// write, which for the chat id is the whole point of the feature: it would
	// let someone bind a household to a chat they control, not the one that
	// completed the sign-up.
	//
	// A partial provision leaves a users row occupying users.email's unique
	// index with no membership under it, making that address permanently unable
	// to sign up again -- the same failure InviteRepository.Accept's doc comment
	// describes.
	//
	// Returns domain.ErrTokenExpired, with nothing written, when the signup is
	// no longer usable -- consumed or expired. Like InviteRepository.Accept's
	// guarded UPDATE, this collapses both cases into one zero-rows result;
	// SignupService.Complete's own TokenLifecycle read is what distinguishes
	// them for a caller, and this answer is authoritative only for the race
	// window between that read and this write.
	Provision(ctx context.Context, signupID, passwordHash string,
		b HouseholdBlueprint) (ProvisionedHousehold, error)
	// Prune deletes consumed and expired rows older than before.
	Prune(ctx context.Context, before time.Time) (int64, error)
}
