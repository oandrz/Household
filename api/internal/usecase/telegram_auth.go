package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// telegramDeadLinkMessage is the answer for an unknown nonce, an expired one,
// an already-consumed one, a chat over its hourly limit, and a /start that
// lands while the global daily sign-up ceiling is breached (see sendSignUp).
// One message for all five, deliberately: any difference between them would
// let a caller tell the cases apart by probing, and none of "rate-limited",
// "the platform is over its daily ceiling" or "this link is dead" is
// something a caller should be able to confirm over "start again from the
// app".
const telegramDeadLinkMessage = "That sign-in link has expired. Start again from the app."

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
// Telegram. It takes no identifier -- no email, no username, nothing -- which
// is why this endpoint has no enumeration oracle to defend: there is nothing
// to probe for.
func (s *TelegramAuthService) StartLink(ctx context.Context) (TelegramStartLink, error) {
	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return TelegramStartLink{}, fmt.Errorf("generate telegram nonce: %w", err)
	}
	expiresAt := s.d.Clock.Now().Add(telegramNonceTTL)
	if err := s.d.Links.Create(ctx, hash, expiresAt); err != nil {
		return TelegramStartLink{}, fmt.Errorf("store telegram nonce: %w", err)
	}
	return TelegramStartLink{
		URL:       fmt.Sprintf("https://t.me/%s?start=%s", s.d.BotUsername, raw),
		ExpiresAt: expiresAt,
	}, nil
}

// HandleStart is called by the poller for every /start. It returns an error
// only for failures worth retrying or alerting on; every ordinary refusal is
// answered in the chat and returns nil, because the person on the other end
// needs an answer, not a stack trace. This split matters beyond style: the
// poller advances its offset before dispatching (see poller.go), so an update
// whose handler returns an error is dropped permanently and nobody is ever
// told anything -- an ordinary refusal MUST be answered here, in the chat, or
// it is answered nowhere.
func (s *TelegramAuthService) HandleStart(ctx context.Context, chatID int64, payload string) error {
	now := s.d.Clock.Now()

	// Consume first, then check the limit. A refused attempt still spends its
	// nonce, so the same link cannot be retried until the hour rolls over.
	if err := s.d.Links.Consume(ctx, s.d.Tokens.HashToken(payload), chatID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return s.say(ctx, chatID, telegramDeadLinkMessage)
		}
		return fmt.Errorf("consume telegram nonce: %w", err)
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
// CONTROLLER RULING R5: before minting, this checks the same global daily
// ceiling SignupService.Request checks (SignupGlobalDailyLimit, counted from
// startOfDay -- see Request's own doc comment for why a calendar day and not
// a rolling 24 hours). Signups.CountSince has no channel filter: it counts
// every row in the signups table regardless of whether Create,
// CreateConsumed or CreateForTelegram wrote it. Without this check, a flood
// of /start commands could run that shared counter up and silently stop
// email sign-up too, while Telegram sign-up itself had no ceiling of its own
// at all -- the worst of both directions at once.
//
// This is deliberately NOT the same "every read runs unconditionally, on
// every branch" symmetry SignupService.Request enforces between its two
// branches, and that is safe for a reason specific to the branch key, not to
// the nonce. Request needs that symmetry because its branch key -- the email
// address -- is supplied by the caller: whoever is guessing addresses picks
// which branch of Request they land on, so the two branches must look and
// cost the same from outside, or the caller learns which addresses are
// registered. HandleStart's branch key is chat_id, and chat_id is never
// caller-supplied: it arrives inside an Update Telegram itself delivers over
// the authenticated long-poll connection (see adapter/telegram/poller.go and
// Client.GetUpdates), so whoever is on the other end of this call can only
// ever land on their own chat's branch -- there is no address to guess, only
// the one chat that is already theirs. (StartLink taking no identifier is a
// separate, true fact -- it means there is no enumeration surface on that
// endpoint either -- but it is not what makes this asymmetry safe; chat_id's
// authenticity is.) If chat_id ever became caller-suppliable -- an unsigned
// webhook, a debug endpoint, a replayed update accepted without verifying
// its source -- this asymmetry would need revisiting before that change
// shipped, not after.
func (s *TelegramAuthService) sendSignUp(ctx context.Context, chatID int64, now time.Time) error {
	globalCount, err := s.d.Signups.CountSince(ctx, startOfDay(now))
	if err != nil {
		return fmt.Errorf("count signups since start of day: %w", err)
	}
	if globalCount >= SignupGlobalDailyLimit {
		// Named "sign-up ceiling", not "mail ceiling": this path sends no
		// mail at all. signup.go's own log line says "mail ceiling" because
		// that path really does relay through SMTP; an operator grepping
		// this line for a mail outage would otherwise chase SMTP for a
		// Telegram request that never touched it.
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

// say sends text to chatID and wraps any failure with the calling method's
// context, never with text itself: text carries a live magic-link or sign-up
// URL on the sign-in and sign-up paths, and this error can reach
// poller.go's slog.Error("telegram start handler failed", "error", err) --
// wrapping the message body would write a live credential into the logs the
// instant the send itself failed.
func (s *TelegramAuthService) say(ctx context.Context, chatID int64, text string) error {
	if err := s.d.Sender.SendMessage(ctx, chatID, text); err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}
	return nil
}
