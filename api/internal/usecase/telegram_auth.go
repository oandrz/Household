package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

const (
	// telegramNonceTTL is short because the nonce is only ever held for the
	// few seconds between tapping a button in a browser and Telegram opening.
	// Nothing legitimate waits ten minutes.
	telegramNonceTTL = 10 * time.Minute

	// telegramLinksPerHourLimit mirrors magicLinkPerHourLimit. Without it, a
	// chat repeating /start is a free path to burn magic-link and signup rows.
	telegramLinksPerHourLimit = 3
)

// telegramDeadLinkMessage answers an unknown, expired or already-consumed
// nonce, a chat over its hourly limit, and a /start during a breached global
// sign-up ceiling (see sendSignUp) -- one message for all five, so a caller
// can't tell "rate-limited" from "ceiling breached" from "dead link" by
// probing.
const telegramDeadLinkMessage = "That sign-in link has expired. Start again from the app."

// telegramLinkRefusedMessage is the answer for every link that cannot be
// completed from this chat's side. It deliberately says nothing about why
// and points at the browser, which can say why safely.
const telegramLinkRefusedMessage = "Could not connect this chat. Open Hearth to see why."

type TelegramAuthDeps struct {
	Links       TelegramLinkRepository
	Accounts    TelegramAccountRepository
	MagicLinks  MagicLinkRepository
	Signups     SignupRepository
	Sender      TelegramSender
	Tokens      TokenGenerator
	Clock       Clock
	BaseURL     string
	BotUsername string
	// Invites answers an inv_ payload: everything about the household
	// invite it names lives behind this one method, so this service holds
	// no invite rule of its own.
	Invites InviteKnocker
}

// TelegramAuthService delivers Hearth's existing sign-in and sign-up tokens
// over Telegram. It mints no token type of its own: a parallel token table
// would mean two expiry rules, two rate limits and two enumeration analyses
// drifting apart, and the second one would be the one nobody reviews.
type TelegramAuthService struct{ d TelegramAuthDeps }

func NewTelegramAuthService(d TelegramAuthDeps) *TelegramAuthService {
	return &TelegramAuthService{d: d}
}

// TelegramStartLink is the deep link a browser sends the person to.
type TelegramStartLink struct {
	URL       string
	ExpiresAt time.Time
}

// StartLink mints a nonce and returns the deep link that carries it into
// Telegram. It takes no identifier -- no email, no username -- so there's
// nothing to probe for.
func (s *TelegramAuthService) StartLink(ctx context.Context) (TelegramStartLink, error) {
	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return TelegramStartLink{}, fmt.Errorf("generate telegram nonce: %w", err)
	}
	expiresAt := s.d.Clock.Now().Add(telegramNonceTTL)
	// The id Create returns is unused here: a sign-in nonce is never polled
	// by row id. HandleStart delivers the resulting magic link straight into
	// the chat, and the waiting browser never learns this row exists.
	if _, err := s.d.Links.Create(ctx, "", hash, expiresAt); err != nil {
		return TelegramStartLink{}, fmt.Errorf("store telegram nonce: %w", err)
	}
	return TelegramStartLink{
		URL:       fmt.Sprintf("https://t.me/%s?start=%s", s.d.BotUsername, raw),
		ExpiresAt: expiresAt,
	}, nil
}

// HandleStart is called by the poller for every /start. It returns an error
// only for failures worth retrying or alerting on; an ordinary refusal is
// answered in the chat and returns nil. This split matters beyond style: the
// poller advances its offset before dispatching (poller.go), so a handler
// that returns an error drops the update permanently -- an ordinary refusal
// MUST be answered here or nowhere.
func (s *TelegramAuthService) HandleStart(ctx context.Context, chatID int64, payload, username string) error {
	// An invite payload is routed here before the nonce table is touched: the
	// inv_ prefix (migration 00018) means an invite never occupies a
	// telegram_link_requests row, which lives ten minutes while an invite
	// lives a day. Consuming first would spend a nonce that was never minted
	// and answer a real invite as a dead link.
	if rawToken, ok := strings.CutPrefix(payload, telegramInvitePayloadPrefix); ok {
		return s.handleInviteStart(ctx, chatID, rawToken, username)
	}

	now := s.d.Clock.Now()

	// Consume first, then check the limit. A refused attempt still spends its
	// nonce, so the same link cannot be retried until the hour rolls over.
	redemption, err := s.d.Links.Consume(ctx, s.d.Tokens.HashToken(payload), chatID, username)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return s.say(ctx, chatID, telegramDeadLinkMessage)
		}
		return fmt.Errorf("consume telegram nonce: %w", err)
	}

	// A link nonce is answered here and goes no further: it mints no token,
	// so the per-chat limit below has nothing to bound on this path.
	// Ordering matters beyond tidiness: a rate-limited link nonce is already
	// consumed and carries a user id the browser reads as "pending" --
	// refusing it here would tell the chat the link was dead while the
	// browser's Confirm button still worked.
	if redemption.UserID != "" {
		return s.handleLinkStart(ctx, chatID, redemption)
	}

	count, err := s.d.Links.CountLinksSince(ctx, chatID, now.Add(-time.Hour))
	if err != nil {
		return fmt.Errorf("count telegram links: %w", err)
	}
	// The row just consumed is included in the count, so the limit is reached
	// at telegramLinksPerHourLimit redemptions, not one past it.
	if count > telegramLinksPerHourLimit {
		slog.Info("telegram link rate limit reached", "chat_hash", hashPrefix(s.d.Tokens.HashToken(fmt.Sprint(chatID)), 12))
		return s.say(ctx, chatID, telegramDeadLinkMessage)
	}

	userID, err := s.d.Accounts.ByChatID(ctx, chatID)
	switch {
	case err == nil:
		return s.sendSignIn(ctx, chatID, userID)
	case errors.Is(err, domain.ErrNotFound):
		return s.sendSignUp(ctx, chatID, now)
	default:
		return fmt.Errorf("look up telegram account: %w", err)
	}
}

// handleLinkStart answers a /start that redeemed a link nonce. It writes no
// binding: the browser session that minted the nonce confirms, and that's
// the whole protection against a leaked deep link (ADR 10).
//
// Every refusal here is bland and identical (telegramDeadLinkMessage's own
// reason): a chat holding a possibly-stolen nonce must not learn whether
// the account exists, has a chat, or belongs to someone else. The session
// that minted it gets the real reason, since it has already proved who it
// is.
func (s *TelegramAuthService) handleLinkStart(ctx context.Context, chatID int64, r TelegramLinkRedemption) error {
	boundTo, err := s.d.Accounts.ByChatID(ctx, chatID)
	switch {
	case err == nil && boundTo == r.UserID:
		return s.say(ctx, chatID, "This chat is already connected to your Hearth account.")
	case err == nil:
		return s.say(ctx, chatID, telegramLinkRefusedMessage)
	case errors.Is(err, domain.ErrNotFound):
		return s.handleLinkStartForUnboundChat(ctx, chatID, r.UserID)
	default:
		return fmt.Errorf("look up telegram account: %w", err)
	}
}

// handleLinkStartForUnboundChat answers a link nonce redeemed from a chat
// with no binding of its own. If the nonce's user already has a *different*
// chat bound, this chat is refused with the same bland line, not told to go
// confirm -- without this check, the person would be sent back to Hearth
// believing the link worked, only for Confirm to refuse them there.
func (s *TelegramAuthService) handleLinkStartForUnboundChat(ctx context.Context, chatID int64, userID string) error {
	_, err := s.d.Accounts.ByUserID(ctx, userID)
	switch {
	case err == nil:
		return s.say(ctx, chatID, telegramLinkRefusedMessage)
	case errors.Is(err, domain.ErrNotFound):
		return s.say(ctx, chatID, "Go back to Hearth and confirm this chat to finish connecting it.")
	default:
		return fmt.Errorf("look up telegram binding by user: %w", err)
	}
}

// handleInviteStart answers a tap on a household invite link. It writes no
// membership: the owner's signed-in browser admits, the whole protection
// against a leaked link (ADR 11, following ADR 10).
//
// Every refusal is telegramDeadLinkMessage, the same line an unknown
// sign-in nonce gets, for the same reason. A chat that already belongs to a
// Hearth account gets a different, self-describing line instead: it tells
// the tapper only about their own chat, which they already know, and saves
// them tapping a dead link forever.
func (s *TelegramAuthService) handleInviteStart(ctx context.Context, chatID int64, rawToken, username string) error {
	code, err := s.d.Invites.Knock(ctx, rawToken, chatID, username)
	switch {
	case err == nil:
		return s.say(ctx, chatID, fmt.Sprintf(
			"Your code is %s.\n\nShow it to whoever invited you. They'll let you in, and then I'll send you a sign-in link.",
			code))
	case errors.Is(err, domain.ErrChatAlreadyBound):
		return s.say(ctx, chatID, "This Telegram account already belongs to a Hearth household.")
	case errors.Is(err, domain.ErrNotFound):
		return s.say(ctx, chatID, telegramDeadLinkMessage)
	default:
		return fmt.Errorf("record invite knock: %w", err)
	}
}

func (s *TelegramAuthService) sendSignIn(ctx context.Context, chatID int64, userID string) error {
	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return fmt.Errorf("generate magic link token: %w", err)
	}
	if err := s.d.MagicLinks.Create(ctx, userID, hash, s.d.Clock.Now().Add(magicLinkTTL)); err != nil {
		return fmt.Errorf("store magic link: %w", err)
	}
	return s.say(ctx, chatID, fmt.Sprintf(
		"Tap to sign in to Hearth:\n%s/sign-in/magic?token=%s\n\nThis link works once, for 15 minutes.",
		s.d.BaseURL, raw))
}

// sendSignUp mints a Telegram-channel sign-up token, the same way
// SignupService.Request mints one for a fresh email address.
//
// Before minting, this checks the same global daily ceiling
// SignupService.Request checks (SignupGlobalDailyLimit, counted from
// startOfDay -- see startOfDay for which zone's midnight). Signups.CountSince
// has no channel filter: it counts every row regardless of which writer
// created it. Without this check, a flood of /start commands could run that
// shared counter up and silently stop email sign-up too, while Telegram
// sign-up itself had no ceiling of its own -- the worst of both directions
// at once.
//
// This deliberately does NOT have the same "every read runs unconditionally
// on every branch" symmetry SignupService.Request enforces, and that's safe
// because it concerns the branch key, not the nonce. Request needs that
// symmetry because its branch key -- the email address -- is caller-supplied:
// whoever guesses addresses picks which branch they land on, so the
// branches must look and cost the same from outside. HandleStart's branch
// key is chat_id, and chat_id is never caller-supplied: it arrives inside an
// Update Telegram delivers over its authenticated long-poll connection
// (adapter/telegram/poller.go, Client.GetUpdates), so a caller can only ever
// land on their own chat's branch -- there's no address to guess. (StartLink
// taking no identifier is a separate fact, not what makes this safe;
// chat_id's authenticity is.) If chat_id ever became caller-suppliable -- an
// unsigned webhook, a debug endpoint, a replayed update accepted without
// verifying its source -- this asymmetry would need revisiting before that
// change shipped.
func (s *TelegramAuthService) sendSignUp(ctx context.Context, chatID int64, now time.Time) error {
	globalCount, err := s.d.Signups.CountSince(ctx, startOfDay(now))
	if err != nil {
		return fmt.Errorf("count signups since start of day: %w", err)
	}
	if globalCount >= SignupGlobalDailyLimit {
		// Named "sign-up ceiling", not "mail ceiling": this path sends no
		// mail. signup.go's line says "mail ceiling" because that path
		// really does relay SMTP -- an operator grepping for a mail outage
		// shouldn't chase SMTP for a Telegram request that never touched it.
		slog.Error("telegram sign-up declined by the global daily sign-up ceiling",
			"global_count", globalCount,
			"global_daily_limit", SignupGlobalDailyLimit,
		)
		return s.say(ctx, chatID, telegramDeadLinkMessage)
	}

	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return fmt.Errorf("generate signup token: %w", err)
	}
	if err := s.d.Signups.CreateForTelegram(ctx, chatID, hash, now.Add(SignupTTL)); err != nil {
		return fmt.Errorf("store telegram signup: %w", err)
	}
	return s.say(ctx, chatID, fmt.Sprintf(
		"Tap to create your Hearth household:\n%s/sign-up/%s\n\nThis link works once, for 24 hours.",
		s.d.BaseURL, raw))
}

// SendSignIn and SendLinkCancelled implement InviteChats. They live here,
// not InviteService, because TelegramAuthService owns every word the bot
// says and the one path that mints a magic link -- a second path would mean
// two expiry rules and two rate limits drifting apart.
func (s *TelegramAuthService) SendSignIn(ctx context.Context, chatID int64, userID string) error {
	return s.sendSignIn(ctx, chatID, userID)
}

func (s *TelegramAuthService) SendLinkCancelled(ctx context.Context, chatID int64) error {
	return s.say(ctx, chatID, "That link is no longer valid. Ask whoever invited you for a new one.")
}

var _ InviteChats = (*TelegramAuthService)(nil)

// say sends text to chatID and wraps any failure with the calling method's
// context, never with text itself: text carries a live magic-link or
// sign-up URL, and this error can reach poller.go's
// slog.Error("telegram start handler failed", ...) -- wrapping the body
// would write a live credential into the logs the instant the send failed.
func (s *TelegramAuthService) say(ctx context.Context, chatID int64, text string) error {
	if err := s.d.Sender.SendMessage(ctx, chatID, text); err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}
	return nil
}
