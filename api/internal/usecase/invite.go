package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// inviteTTL is the design's invite lifetime: seven days from the moment
// Create is called, measured against Clock rather than wall time so tests
// can move it.
const inviteTTL = 7 * 24 * time.Hour

// TelegramInviteTTL is 24 hours, exported so main.go and the test wiring
// cannot drift apart on it.
const TelegramInviteTTL = 24 * time.Hour

// telegramInvitePayloadPrefix routes a /start payload to an invite, not the
// sign-in nonce table (migration 00018 reserved it). "inv_" plus NewToken's
// 43 base64url characters is 47, under Telegram's 64-character start limit
// -- recheck before lengthening either half.
const telegramInvitePayloadPrefix = "inv_"

// ErrInviteeAlreadyRegistered is Create's rejection of an invite to an email
// address that already has a users row.
//
// Without this check, Create wrote the invite and mailed it anyway: Accept
// always hits users.email's unique constraint (it never reuses an existing
// row), rolls back, and answers 500 on every retry forever -- the sender
// saw success, and the recipient could never accept.
//
// Rejecting here, where the owner who typed the address can see the error,
// beats teaching Accept to reuse the row: re-inviting an existing address is
// almost always a mistake, not genuine intent to share an account.
var ErrInviteeAlreadyRegistered = errors.New("an account with that email address already exists")

// InviteDeps mirrors AuthDeps: every port InviteService needs, gathered into
// one struct so NewInviteService has a single, named argument.
//
// There is no MembershipRepository here: every membership write goes through
// a port that creates it transactionally alongside its user --
// UserRepository.CreateWithMembership or InviteRepository.Accept -- so a bare
// MembershipRepository.Create call would reintroduce the orphaned-user
// defect (see both ports' doc comments).
type InviteDeps struct {
	Invites    InviteRepository
	Users      UserRepository
	Sessions   SessionRepository
	Mailer     Mailer
	Hasher     PasswordHasher
	Tokens     TokenGenerator
	Clock      Clock
	SessionTTL time.Duration
	BaseURL    string
	// BotUsername is the @name in the t.me deep link. Empty means no bot is
	// configured; a Telegram invite is also impossible when the
	// telegram_sign_in flag is off.
	BotUsername string
	// TelegramInviteTTL is 24 hours; email invites keep inviteTTL's seven
	// days. A new Telegram link is one click, so the short life costs
	// little, and a link forgotten in a chat dies the next day.
	TelegramInviteTTL time.Duration
	// Codes draws the four digits Knock shows the tapper.
	Codes PairingCodes
	// Accounts is read by Knock, before it ever touches the invite: a chat
	// that already belongs to a Hearth account is refused before it can
	// spend somebody else's link.
	Accounts TelegramAccountRepository
	// Chats sends the two messages a Telegram invite causes. It cannot be
	// set here at construction time -- see SetChats' own doc comment for
	// the constructor cycle that forces it to arrive later.
	Chats InviteChats
}

type InviteService struct {
	d InviteDeps
}

// NewInviteService is a direct wrap -- no field here needs a zero-value
// default the way NewAuthService's Policy does -- kept as a constructor
// purely to match AuthService's shape.
func NewInviteService(d InviteDeps) *InviteService {
	return &InviteService{d: d}
}

// SetChats completes the two-way wiring between this service and
// TelegramAuthService: the invite side sends messages, the Telegram side
// records a knock (InviteKnocker), and neither can be built with the other
// already constructed. main.go builds both and closes this half of the loop
// here, exactly once, at startup, before any request is served.
//
// NewLink dereferences d.Chats with no nil check: it can never reach a nil
// Chats, because it returns early whenever BotUsername is empty. Admit can
// reach one -- see its own doc comment for why, and for the nil check that
// covers it.
func (s *InviteService) SetChats(chats InviteChats) { s.d.Chats = chats }

// InvitePreview is what a caller sees before signing in: enough to render
// "Andreas invited you to join the Oentoro household as Kid, with calendar
// and chores access" without exposing anything else about the invite.
type InvitePreview struct {
	FamilyName   string
	InviterName  string
	Name         string
	Role         domain.Role
	Capabilities domain.Capabilities
}

// Create adds a new member to the household. Usually that means writing an
// invite row and emailing a link; a limited member with no email (the
// design's children) is instead created directly, with no invite and no
// credentials, since there's no address to send one to.
//
// The membership shape is validated through domain.NewMembership before any
// write, in both branches, so an invalid role/capability combination never
// reaches a repository call.
func (s *InviteService) Create(ctx context.Context, householdID, invitedByUserID, name, email string,
	role domain.Role, caps domain.Capabilities) error {
	if _, err := domain.NewMembership("", householdID, "", role, caps); err != nil {
		return err
	}

	if email == "" {
		// Only a limited member can be created without an email -- the
		// design's child case, no credentials. Any other role with no email
		// has nowhere for an invite to go: it would sit unopened and expire
		// silently while the caller saw success.
		if role != domain.RoleLimited {
			return domain.ErrInviteRequiresEmail
		}
		// The user's own ID isn't known yet -- CreateWithMembership assigns it
		// inside its transaction -- so this validates the role/capability shape
		// with the same empty-userID placeholder Accept uses.
		// CreateWithMembership creates the user and membership together in one
		// transaction: a partial failure would otherwise orphan a user with a
		// NULL email, silently, on every retry (see its own doc comment).
		membership, err := domain.NewMembership("", householdID, "", role, caps)
		if err != nil {
			return err
		}
		_, _, err = s.d.Users.CreateWithMembership(ctx, "", "", name, membership)
		return err
	}

	// Reject an invite to an address that already has a users row before
	// writing anything (see ErrInviteeAlreadyRegistered's doc comment). This
	// is a pre-check, not the only gate: two callers inviting the same new
	// address at once can both pass it, so the race is closed instead by
	// users.email's unique constraint (mapped to 409 in MapDomainError).
	if _, err := s.d.Users.ByEmail(ctx, email); err == nil {
		return ErrInviteeAlreadyRegistered
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}

	now := s.d.Clock.Now()
	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return fmt.Errorf("generate invite token: %w", err)
	}
	if _, err := s.d.Invites.Create(ctx, householdID, email, name, role, caps, hash, invitedByUserID,
		now.Add(inviteTTL)); err != nil {
		return err
	}

	inviter, err := s.d.Users.ByID(ctx, invitedByUserID)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/invite/%s", s.d.BaseURL, raw)
	return s.d.Mailer.SendInvite(ctx, email, name, inviter.DisplayName, url)
}

// TelegramInviteLink is a one-time deep link, returned once at creation and
// once per new link (NewLink). The raw token is never stored -- only its
// hash is -- so nothing can show this URL a second time.
type TelegramInviteLink struct {
	ID        string
	URL       string
	ExpiresAt time.Time
}

// CreateTelegram writes an invite nobody has to have an email address for,
// and returns the deep link the owner hands over.
//
// There's deliberately no ErrInviteeAlreadyRegistered check here: that
// exists because users.email is unique, but Telegram has no address. Its
// real collision -- the chat already belonging to an account -- isn't
// knowable until someone taps, so it's checked at the knock and again
// inside Admit's transaction.
func (s *InviteService) CreateTelegram(ctx context.Context, householdID, invitedByUserID, name string,
	role domain.Role, caps domain.Capabilities) (TelegramInviteLink, error) {
	if s.d.BotUsername == "" {
		return TelegramInviteLink{}, domain.ErrTelegramInvitesUnavailable
	}
	if _, err := domain.NewMembership("", householdID, "", role, caps); err != nil {
		return TelegramInviteLink{}, err
	}

	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return TelegramInviteLink{}, fmt.Errorf("generate telegram invite token: %w", err)
	}
	expiresAt := s.d.Clock.Now().Add(s.d.TelegramInviteTTL)
	id, err := s.d.Invites.CreateTelegram(ctx, householdID, name, role, caps, hash, invitedByUserID, expiresAt)
	if err != nil {
		return TelegramInviteLink{}, err
	}
	return TelegramInviteLink{ID: id, URL: s.telegramInviteURL(raw), ExpiresAt: expiresAt}, nil
}

// telegramInviteURL is the one place the inv_ prefix is written;
// HandleStart strips it before handing the rest to the knocker.
func (s *InviteService) telegramInviteURL(rawToken string) string {
	return fmt.Sprintf("https://t.me/%s?start=%s%s", s.d.BotUsername, telegramInvitePayloadPrefix, rawToken)
}

// NewLink replaces a Telegram invite's link -- also what "Not them" does:
// the knock is cleared, the old token stops working, and whoever knocked is
// told. One method serves both, since the owner's two intents ("that
// wasn't them" and "I lost the link") need the same four effects.
//
// An owner who wants no new link at all uses Withdraw instead (deletes the
// row).
func (s *InviteService) NewLink(ctx context.Context, householdID, inviteID string) (TelegramInviteLink, error) {
	if s.d.BotUsername == "" {
		return TelegramInviteLink{}, domain.ErrTelegramInvitesUnavailable
	}
	raw, hash, err := s.d.Tokens.NewToken()
	if err != nil {
		return TelegramInviteLink{}, fmt.Errorf("generate telegram invite token: %w", err)
	}
	expiresAt := s.d.Clock.Now().Add(s.d.TelegramInviteTTL)
	knockedChatID, err := s.d.Invites.ReplaceToken(ctx, householdID, inviteID, hash, expiresAt)
	if err != nil {
		return TelegramInviteLink{}, err
	}
	// After the write, never before: a failure here must not leave the
	// owner without the new link they asked for. The chat is told as a
	// courtesy -- their link already stopped working the moment the row
	// changed -- so the error is logged, not returned.
	if knockedChatID != 0 {
		if err := s.d.Chats.SendLinkCancelled(ctx, knockedChatID); err != nil {
			slog.Error("could not tell a knocked chat its invite link was replaced", "error", err)
		}
	}
	return TelegramInviteLink{ID: inviteID, URL: s.telegramInviteURL(raw), ExpiresAt: expiresAt}, nil
}

// Knock records the first tap on a Telegram invite link and returns the
// four digits to show the tapper. It implements InviteKnocker, so
// TelegramAuthService can route an inv_ payload here without knowing any
// invite rule.
//
// Every refusal about the *link* is domain.ErrNotFound, with no exception:
// unknown, expired, accepted, already-knocked and email-channel all
// collapse into one answer, so a stolen link can't be used to probe for
// someone else's invite.
//
// domain.ErrChatAlreadyBound is the one exception, and it's safe: it only
// tells the tapper their own chat is already bound, which a bare /start
// would reveal anyway, and answering plainly saves them tapping a link
// that can never work. This check runs first, before the guarded UPDATE, so
// an already-bound chat can't consume someone else's link on its way to
// being refused -- and again inside Admit's transaction, since the chat
// could sign up elsewhere in between.
func (s *InviteService) Knock(ctx context.Context, rawToken string, chatID int64, username string) (string, error) {
	if _, err := s.d.Accounts.ByChatID(ctx, chatID); err == nil {
		return "", domain.ErrChatAlreadyBound
	} else if !errors.Is(err, domain.ErrNotFound) {
		return "", err
	}

	code, err := s.d.Codes.NewCode()
	if err != nil {
		return "", err
	}
	if err := s.d.Invites.RecordKnock(ctx, s.d.Tokens.HashToken(rawToken), chatID, username, code,
		s.d.Clock.Now()); err != nil {
		return "", err
	}
	return code, nil
}

// Admit is Let in: the owner has compared the four digits by eye and
// clicked to turn the waiting knock into a real member. The write -- user,
// membership, telegram_accounts binding and acceptance stamp together -- is
// entirely InviteRepository.Admit's transaction (see its own doc comment).
// This method's job is just the order: write, then message.
//
// The sign-in link is sent after the commit, never inside it: sending
// inside the transaction could promise an account a later rollback takes
// away. A failed send is reported as SignInSent: false rather than
// swallowed (docs/LEARNING.md pattern 5) -- no new recovery path is needed,
// since the chat is bound by then and any /start gets a fresh link.
//
// A nil Chats is checked here, unlike NewLink: the write above has already
// committed, so a knocked invite can still be waiting when this runs on an
// install that has since dropped its bot (docs/INFRASTRUCTURE.md's
// leaked-token runbook). Reported the same way as a failed send, never a
// panic: the member is not lost because nobody was left to tell.
func (s *InviteService) Admit(ctx context.Context, householdID, inviteID string) (AdmittedMember, error) {
	admitted, err := s.d.Invites.Admit(ctx, householdID, inviteID, s.d.Clock.Now())
	if err != nil {
		return AdmittedMember{}, err
	}
	member := AdmittedMember{
		MembershipID: admitted.MembershipID,
		UserID:       admitted.UserID,
		Name:         admitted.Name,
		Role:         admitted.Role,
		Capabilities: admitted.Capabilities,
		SignInSent:   true,
	}
	if s.d.Chats == nil {
		slog.Error("admitted a member but no telegram chat sender is configured")
		member.SignInSent = false
		return member, nil
	}
	if err := s.d.Chats.SendSignIn(ctx, admitted.ChatID, admitted.UserID); err != nil {
		slog.Error("admitted a member but could not send their sign-in link", "error", err)
		member.SignInSent = false
	}
	return member, nil
}

// Preview lets a caller see what an invite offers before they sign in or
// create credentials. It shares its expiry/acceptance checks with Accept
// through checkInviteLive.
func (s *InviteService) Preview(ctx context.Context, token string) (InvitePreview, error) {
	details, err := s.d.Invites.ByTokenHash(ctx, s.d.Tokens.HashToken(token))
	if err != nil {
		return InvitePreview{}, err
	}
	// A Telegram invite isn't servable here: without this, the raw token
	// could accept through the web form with a password, skipping Let in.
	// It's answered as an unknown token would be, before the liveness
	// check, so "expired" can't be told apart from "unknown" either.
	// TestTheWebFormCannotAcceptATelegramInvite pins this.
	if details.Channel != domain.ChannelEmail {
		return InvitePreview{}, domain.ErrNotFound
	}
	if err := checkInviteLive(details, s.d.Clock.Now()); err != nil {
		return InvitePreview{}, err
	}
	return InvitePreview{
		FamilyName:   details.FamilyName,
		InviterName:  details.InviterName,
		Name:         details.Name,
		Role:         details.Role,
		Capabilities: details.Capabilities,
	}, nil
}

// checkInviteLive reports the specific reason an invite can no longer be
// used, telling an already-accepted invite (409) apart from an expired one
// (410) -- InviteRepository.Accept's own no-rows case can't, since its
// guarded UPDATE collapses both into zero rows. Reading accepted_at and
// expires_at via ByTokenHash is what lets Preview and Accept tell them
// apart; Accept's own answer is authoritative only for the race window
// between this read and that write.
//
// The consumed-before-expired ordering lives in domain.TokenLifecycle,
// shared with sign-up; the sentinels stay invite-specific since the HTTP
// layer maps them to different statuses and copy.
func checkInviteLive(details InviteDetails, now time.Time) error {
	switch domain.TokenLifecycle(now, details.ExpiresAt, details.AcceptedAt) {
	case domain.TokenLive:
		return nil
	case domain.TokenConsumed:
		return domain.ErrInviteAlreadyAccepted
	case domain.TokenExpired:
		return domain.ErrInviteExpired
	default:
		// A state this switch does not recognise must fail closed rather than
		// treat the invite as usable. Adding a TokenState without adding a
		// case here refuses the invite; it does not silently accept it.
		return domain.ErrInviteExpired
	}
}

// Accept turns an invite into a real account: hashes the password, creates
// the user and membership, and stamps the invite accepted -- all in
// InviteRepository.Accept's one transaction -- then signs the member in via
// the same issueSession SignIn uses.
//
// Don't compose this from separate MarkAccepted/CreateUser/CreateMembership
// calls: a failure between them would orphan a user in the unique email
// index, and the invite could never be accepted by anyone (see
// InviteRepository.Accept's doc comment).
func (s *InviteService) Accept(ctx context.Context, token, password, displayName string) (SignInResult, error) {
	if err := validatePassword(password); err != nil {
		return SignInResult{}, err
	}

	now := s.d.Clock.Now()
	details, err := s.d.Invites.ByTokenHash(ctx, s.d.Tokens.HashToken(token))
	if err != nil {
		return SignInResult{}, err
	}
	// Same refusal as Preview, for the same reason -- see its comment.
	if details.Channel != domain.ChannelEmail {
		return SignInResult{}, domain.ErrNotFound
	}
	if err := checkInviteLive(details, now); err != nil {
		return SignInResult{}, err
	}

	// Validate the membership shape before any write, exactly as Create: this
	// catches an invite whose stored role/capabilities would otherwise fail
	// inside the repository's transaction instead of here.
	if _, err := domain.NewMembership("", details.HouseholdID, "", details.Role, details.Capabilities); err != nil {
		return SignInResult{}, err
	}

	passwordHash, err := s.d.Hasher.Hash(password)
	if err != nil {
		return SignInResult{}, fmt.Errorf("hash invite password: %w", err)
	}

	// InviteRepository.Accept is the concurrency gate: its guarded update is
	// the authoritative answer for the race window between the ByTokenHash
	// read above and this call, so its domain.ErrInviteAlreadyAccepted is
	// returned as-is rather than re-derived from the stale read.
	accepted, err := s.d.Invites.Accept(ctx, details.ID, details.Email, passwordHash, displayName,
		details.HouseholdID, details.Role, details.Capabilities)
	if err != nil {
		return SignInResult{}, err
	}

	return issueSession(ctx, s.d.Sessions, s.d.Tokens, s.d.SessionTTL, accepted.UserID, accepted.HouseholdID, now)
}

// ListPending is what an owner sees in Settings: every invite the household
// has sent that nobody has accepted and that has not expired, measured
// against Clock so tests can move it. See InviteRepository.ListPending for
// the one definition of "pending".
func (s *InviteService) ListPending(ctx context.Context, householdID string) ([]InviteSummary, error) {
	return s.d.Invites.ListPending(ctx, householdID, s.d.Clock.Now())
}

// Withdraw deletes an invite nobody has accepted, so the link its invitee
// already holds stops working. It deletes rather than stamps: a "withdrawn"
// stamp would need every invite query to remember one more condition, while
// a deleted row can't be accepted by any of them.
func (s *InviteService) Withdraw(ctx context.Context, householdID, inviteID string) error {
	return s.d.Invites.Delete(ctx, householdID, inviteID)
}
