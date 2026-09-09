package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// telegramLinkMintsPerHourLimit bounds how many link attempts one member can
// start in an hour. The per-chat limit in telegram_auth.go bounds redemption;
// this bounds minting, which a signed-in session can now do with no chat
// involved at all.
const telegramLinkMintsPerHourLimit = 3

// TelegramLinkDeps is what NewTelegramLinkService needs to build one.
type TelegramLinkDeps struct {
	Links       TelegramLinkRepository
	Accounts    TelegramAccountRepository
	Users       UserRepository
	Tokens      TokenGenerator
	Clock       Clock
	BotUsername string
}

// TelegramLinkService connects a Hearth account that already exists to a
// Telegram chat, and disconnects it again. It is deliberately separate from
// TelegramAuthService: that service delivers Hearth's existing tokens over a
// chat, this one writes the binding those deliveries depend on.
//
// It takes a userID as the subject it acts on, supplied by the handler from
// the session. That is not an actor parameter (ADR 8): the service enforces
// what is valid, the HTTP edge enforces who is asking.
type TelegramLinkService struct{ d TelegramLinkDeps }

func NewTelegramLinkService(d TelegramLinkDeps) *TelegramLinkService {
	return &TelegramLinkService{d: d}
}

// TelegramLinkStart is the deep link a browser sends the person to, plus the
// row id it polls Status with.
type TelegramLinkStart struct {
	ID        string
	URL       string
	ExpiresAt time.Time
}

// TelegramLinkStatus is where a link request has got to. Status is one of
// exactly five values: "waiting", "pending", "connected", "refused",
// "expired" -- the same set the zod enum in Task 7 accepts.
type TelegramLinkStatus struct {
	Status       string
	ChatUsername string
	ChatID       int64
	// Reason is set only for "refused" -- a small stable code
	// (TelegramLinkReasonChatTaken or TelegramLinkReasonAlreadyLinked), never
	// a sentence and never a database error. This package may not import the
	// http adapter, so it cannot own the copy a person reads; the HTTP layer
	// maps the code onto the same sentence errors.go's 409 mapping uses for
	// the identical condition reached through confirm, so the two routes
	// never drift into saying the same refusal two different ways.
	Reason string
}

// The two refusal codes Status.Reason can carry. Exported so the HTTP layer
// can switch on them by name instead of repeating the string literal.
const (
	TelegramLinkReasonChatTaken     = "chat_taken"
	TelegramLinkReasonAlreadyLinked = "already_linked"
)

// Start mints a link nonce and returns the deep link that carries it into
// Telegram, plus the row id the browser polls with.
func (s *TelegramLinkService) Start(ctx context.Context, userID string) (TelegramLinkStart, error) {
	// Refuse before minting: a limit checked after the write is a limit that
	// still grows the table it is protecting.
	count, err := s.d.Links.CountMintsSince(ctx, userID, s.d.Clock.Now().Add(-time.Hour))
	if err != nil {
		return TelegramLinkStart{}, fmt.Errorf("count telegram link mints: %w", err)
	}
	if count >= telegramLinkMintsPerHourLimit {
		return TelegramLinkStart{}, domain.ErrTelegramMintsRateLimited
	}

	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return TelegramLinkStart{}, fmt.Errorf("generate telegram link nonce: %w", err)
	}
	expiresAt := s.d.Clock.Now().Add(telegramNonceTTL)
	id, err := s.d.Links.Create(ctx, userID, hash, expiresAt)
	if err != nil {
		return TelegramLinkStart{}, fmt.Errorf("store telegram link nonce: %w", err)
	}
	return TelegramLinkStart{
		ID:        id,
		URL:       fmt.Sprintf("https://t.me/%s?start=%s", s.d.BotUsername, raw),
		ExpiresAt: expiresAt,
	}, nil
}

// Confirm writes the binding. It re-checks everything Status derived, because
// minutes pass between the two and the other chat can be bound in between;
// the UNIQUE constraints are the real gate and this is only the message.
func (s *TelegramLinkService) Confirm(ctx context.Context, userID, linkID string) (TelegramBinding, error) {
	row, err := s.d.Links.ByID(ctx, linkID)
	if err != nil {
		return TelegramBinding{}, err // ErrNotFound passes straight through
	}
	// An unknown row and another member's row are the same answer, so a row
	// id cannot be tested for existence.
	if row.UserID != userID {
		return TelegramBinding{}, domain.ErrNotFound
	}
	if !row.Consumed || !row.ExpiresAt.After(s.d.Clock.Now()) {
		return TelegramBinding{}, domain.ErrTelegramLinkNotPending
	}
	if boundTo, err := s.d.Accounts.ByChatID(ctx, row.ChatID); err == nil {
		if boundTo == userID {
			return s.d.Accounts.ByUserID(ctx, userID) // idempotent: already ours
		}
		return TelegramBinding{}, domain.ErrTelegramChatTaken
	} else if !errors.Is(err, domain.ErrNotFound) {
		return TelegramBinding{}, fmt.Errorf("look up telegram account by chat: %w", err)
	}
	if _, err := s.d.Accounts.ByUserID(ctx, userID); err == nil {
		return TelegramBinding{}, domain.ErrTelegramAlreadyLinked
	} else if !errors.Is(err, domain.ErrNotFound) {
		return TelegramBinding{}, fmt.Errorf("look up telegram account by user: %w", err)
	}

	binding := TelegramBinding{UserID: userID, ChatID: row.ChatID, ChatUsername: row.ChatUsername, LinkedAt: s.d.Clock.Now()}
	if err := s.d.Accounts.Create(ctx, binding); err != nil {
		if !errors.Is(err, domain.ErrAlreadyExists) {
			return TelegramBinding{}, fmt.Errorf("create telegram account: %w", err)
		}
		// Both UNIQUEs arrive as ErrAlreadyExists, and by now this service
		// genuinely does not know which one fired: the pre-checks above ran
		// against state that is, at worst, this row's whole ten-minute
		// window old, and it is the constraints -- not those checks -- that
		// are the real gate. Re-read the chat side to find out. Bound to
		// this same user is the idempotent case: our own earlier confirm,
		// or a second tab open on the same link, already wrote this exact
		// row, so hand back the binding rather than an error. Bound to
		// someone else is the chat-side collision. Still unbound means the
		// chat side was never the problem, so it was the user-side UNIQUE --
		// a second pending link for this user won the race instead.
		boundTo, chatErr := s.d.Accounts.ByChatID(ctx, row.ChatID)
		switch {
		case chatErr == nil && boundTo == userID:
			return s.d.Accounts.ByUserID(ctx, userID)
		case chatErr == nil:
			return TelegramBinding{}, domain.ErrTelegramChatTaken
		case errors.Is(chatErr, domain.ErrNotFound):
			return TelegramBinding{}, domain.ErrTelegramAlreadyLinked
		default:
			return TelegramBinding{}, fmt.Errorf("look up telegram account by chat after conflict: %w", chatErr)
		}
	}
	return binding, nil
}

// Status derives where a link has got to, in this order: binding first, then
// refusals, then expiry. Expiry last is deliberate -- a connected panel that
// is still polling when the nonce's ten minutes run out must keep reading
// "connected", not flip to "expired" ten minutes after it succeeded.
func (s *TelegramLinkService) Status(ctx context.Context, userID, linkID string) (TelegramLinkStatus, error) {
	row, err := s.d.Links.ByID(ctx, linkID)
	if err != nil {
		return TelegramLinkStatus{}, err // ErrNotFound passes straight through
	}
	// Same rule as Confirm: an unknown row and another member's row are the
	// same answer, so a row id cannot be tested for existence.
	if row.UserID != userID {
		return TelegramLinkStatus{}, domain.ErrNotFound
	}

	binding, err := s.d.Accounts.ByUserID(ctx, userID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return TelegramLinkStatus{}, fmt.Errorf("look up telegram account by user: %w", err)
	}
	bound := err == nil

	// Binding first: a connected panel still polling after expires_at passes
	// must keep reading "connected", never flip to "expired" ten minutes
	// after it worked.
	if bound && binding.ChatID == row.ChatID {
		return TelegramLinkStatus{Status: "connected", ChatID: binding.ChatID, ChatUsername: binding.ChatUsername}, nil
	}

	if !row.Consumed {
		if row.ExpiresAt.After(s.d.Clock.Now()) {
			return TelegramLinkStatus{Status: "waiting"}, nil
		}
		return TelegramLinkStatus{Status: "expired"}, nil
	}

	// Refusals next, before expiry: a row another chat has already claimed,
	// or that this user has already superseded with a different chat,
	// deserves the specific reason rather than a bare "expired".
	if boundTo, err := s.d.Accounts.ByChatID(ctx, row.ChatID); err == nil {
		if boundTo != userID {
			return TelegramLinkStatus{
				Status: "refused", ChatID: row.ChatID, ChatUsername: row.ChatUsername,
				Reason: TelegramLinkReasonChatTaken,
			}, nil
		}
	} else if !errors.Is(err, domain.ErrNotFound) {
		return TelegramLinkStatus{}, fmt.Errorf("look up telegram account by chat: %w", err)
	}
	if bound {
		return TelegramLinkStatus{
			Status: "refused", ChatID: row.ChatID, ChatUsername: row.ChatUsername,
			Reason: TelegramLinkReasonAlreadyLinked,
		}, nil
	}

	if row.ExpiresAt.After(s.d.Clock.Now()) {
		return TelegramLinkStatus{Status: "pending", ChatID: row.ChatID, ChatUsername: row.ChatUsername}, nil
	}
	return TelegramLinkStatus{Status: "expired"}, nil
}

// Binding returns this user's connected chat, domain.ErrNotFound when there
// is none -- the same shape GET /auth/telegram (Task 6) answers from.
func (s *TelegramLinkService) Binding(ctx context.Context, userID string) (TelegramBinding, error) {
	return s.d.Accounts.ByUserID(ctx, userID)
}

// Unlink removes the binding, refusing when the account has no email address
// -- see domain.ErrTelegramUnlinkWouldLockOut for what that account would
// lose. Delete is idempotent, so unlinking twice is not an error.
func (s *TelegramLinkService) Unlink(ctx context.Context, userID string) error {
	user, err := s.d.Users.ByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("look up user: %w", err)
	}
	if user.Email == "" {
		return domain.ErrTelegramUnlinkWouldLockOut
	}
	return s.d.Accounts.Delete(ctx, userID)
}
