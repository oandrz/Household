package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// inviteTTL is the design's invite lifetime: seven days from the moment
// Create is called, measured against Clock rather than wall time so tests
// can move it.
const inviteTTL = 7 * 24 * time.Hour

// minInvitePasswordLength is Accept's password floor. It is a usecase-level
// rule, not a domain one -- domain has no notion of a password at all -- so
// it lives here rather than in internal/domain, exactly as SignInFailedError
// (also a usecase-only concept) lives in auth.go rather than domain.
const minInvitePasswordLength = 12

// ErrPasswordTooShort is Accept's rejection when the chosen password is
// under minInvitePasswordLength characters. It is a usecase sentinel, not a
// domain one, for the same reason minInvitePasswordLength is defined here:
// domain has no password concept to attach an error to.
var ErrPasswordTooShort = errors.New("password must be at least 12 characters")

// maxPasswordLength is the ceiling applied everywhere a caller-supplied
// password reaches PasswordHasher, in both InviteService.Accept (below) and
// AuthService.SignIn (auth.go): argon2id's cost scales with the size of the
// string it hashes, so with no upper bound a caller could force an
// arbitrarily expensive hash by submitting a multi-megabyte password --
// uncapped CPU cost fronted directly by an HTTP endpoint. 256 characters is
// far beyond any legitimate human-chosen or generator-produced password.
const maxPasswordLength = 256

// ErrPasswordTooLong is Accept's rejection when the chosen password exceeds
// maxPasswordLength. SignIn enforces the identical ceiling but never
// surfaces this error: a too-long password there must fail exactly like a
// wrong password -- same error, same shape -- so SignIn folds the case into
// its ordinary invalid-credentials path (see AuthService.verifyPassword in
// auth.go) instead of returning a distinguishable sentinel.
var ErrPasswordTooLong = errors.New("password must be at most 256 characters")

// ErrInviteeAlreadyRegistered is Create's rejection of an invite to an email
// address that already has a users row. Without this check, Create wrote the
// invite and sent the mail anyway -- InviteRepo.Accept unconditionally calls
// CreateUser and never reuses an existing row, so acceptance always hit
// users.email's unique constraint, translated to domain.ErrAlreadyExists with
// no mapping, and answered 500. The transaction rolled back, so the invite
// stayed live and every retry produced the same 500: the owner who sent the
// invite saw success, and the recipient could never accept it, forever.
//
// Rejecting at creation -- where the owner who typed the address can see the
// error and act on it -- is better than teaching Accept to reuse the
// existing row: re-inviting an address that already belongs to someone is
// almost always a mistake (a mistype, or an accidental re-invite of a
// current member) rather than a genuine intent to hand a second person the
// same account.
var ErrInviteeAlreadyRegistered = errors.New("an account with that email address already exists")

// InviteDeps mirrors AuthDeps: every port InviteService needs, gathered into
// one struct so NewInviteService has a single, named argument rather than a
// long positional list.
//
// There is no MembershipRepository here. Every write InviteService makes to
// a membership goes through a port that creates it transactionally alongside
// the user it belongs to -- UserRepository.CreateWithMembership for the
// child branch of Create, InviteRepository.Accept for Accept -- so a bare
// MembershipRepository.Create call is never the right tool for this service
// (see both ports' doc comments in ports.go for why a lone Create would
// reintroduce the orphaned-user defect this task fixed).
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
}

type InviteService struct {
	d InviteDeps
}

// NewInviteService has nothing that needs a zero-value default the way
// NewAuthService's Policy does -- every InviteDeps field is either required
// or, for BaseURL, safely empty -- so it is a direct wrap, kept as a
// constructor (rather than a struct literal at call sites) purely to match
// AuthService's shape.
func NewInviteService(d InviteDeps) *InviteService {
	return &InviteService{d: d}
}

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

// Create adds a new member to the household. Most of the time that means
// writing an invite row and emailing a link; a limited member with no email
// address of their own -- the design's children -- is instead created
// directly, with no invite and no email at all, because there is no address
// to send one to and no one who will ever type a password for that account.
//
// The membership shape is validated through domain.NewMembership before any
// write happens, in both branches, so an invalid role/capability combination
// (a limited member holding marriage, or an owner missing a capability)
// never reaches a repository call.
func (s *InviteService) Create(ctx context.Context, householdID, invitedByUserID, name, email string,
	role domain.Role, caps domain.Capabilities) error {
	if _, err := domain.NewMembership("", householdID, "", role, caps); err != nil {
		return err
	}

	if email == "" {
		// Only a limited member can be created without an email at all --
		// that's the design's child case, created directly with no
		// credentials. Any other role with no email has nowhere for an
		// invite to go: it would occupy a token hash, sit unopened, and
		// expire silently seven days later while the caller who created it
		// saw success. Reject the combination before writing anything.
		if role != domain.RoleLimited {
			return domain.ErrInviteRequiresEmail
		}
		// The user's own ID isn't known yet -- CreateWithMembership assigns
		// it inside its transaction -- so this validates the role/capability
		// shape with the same empty-userID placeholder Accept uses, before
		// any write happens. CreateWithMembership itself does the user
		// creation and the membership creation together, in one
		// transaction: see its doc comment in ports.go for why that matters
		// (a partial failure here would orphan a user with a NULL email --
		// no unique constraint to make a retry fail loudly, so it would
		// silently create another orphan each time).
		membership, err := domain.NewMembership("", householdID, "", role, caps)
		if err != nil {
			return err
		}
		_, _, err = s.d.Users.CreateWithMembership(ctx, "", "", name, membership)
		return err
	}

	// Reject an invite to an address that already has a users row before
	// writing anything -- see ErrInviteeAlreadyRegistered's doc comment for
	// the 500 this closes. This is a pre-check, not the only gate: two
	// callers inviting the same brand-new address at once can both pass it
	// before either invite is accepted, so the race is still possible at
	// acceptance time. That race is closed by users.email's unique
	// constraint (translated to domain.ErrAlreadyExists, mapped to 409 in
	// MapDomainError's default case) rather than by anything here.
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

// Preview lets a caller see what an invite offers before they sign in or
// create credentials. It shares its expiry/acceptance checks with Accept
// through checkInviteLive.
func (s *InviteService) Preview(ctx context.Context, token string) (InvitePreview, error) {
	details, err := s.d.Invites.ByTokenHash(ctx, s.d.Tokens.HashToken(token))
	if err != nil {
		return InvitePreview{}, err
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
// used, distinguishing an already-accepted invite (409, per the spec) from
// an expired one (410) -- a distinction InviteRepository.Accept's own
// no-rows case cannot make, because its guarded UPDATE collapses both into
// the same zero-rows result. Reading accepted_at and expires_at ourselves,
// via ByTokenHash, is what lets Preview and Accept report the two apart;
// InviteRepository.Accept's answer is then authoritative only for the race
// window between this read and that write.
func checkInviteLive(details InviteDetails, now time.Time) error {
	if details.AcceptedAt != nil {
		return domain.ErrInviteAlreadyAccepted
	}
	if !details.ExpiresAt.After(now) {
		return domain.ErrInviteExpired
	}
	return nil
}

// Accept turns an invite into a real account: it hashes the chosen password,
// creates the user and membership and stamps the invite as accepted --all in
// the one transaction InviteRepository.Accept performs -- and then signs the
// new member in exactly as SignIn does, through the same issueSession
// function.
//
// Do not compose this from MarkAccepted plus separate CreateUser and
// CreateMembership calls: a failure between those three steps would leave an
// orphaned user occupying the unique email index, and the invite could then
// never be accepted by anyone, ever (see InviteRepository.Accept's doc
// comment in ports.go).
func (s *InviteService) Accept(ctx context.Context, token, password, displayName string) (SignInResult, error) {
	if len(password) < minInvitePasswordLength {
		return SignInResult{}, ErrPasswordTooShort
	}
	if len(password) > maxPasswordLength {
		return SignInResult{}, ErrPasswordTooLong
	}

	now := s.d.Clock.Now()
	details, err := s.d.Invites.ByTokenHash(ctx, s.d.Tokens.HashToken(token))
	if err != nil {
		return SignInResult{}, err
	}
	if err := checkInviteLive(details, now); err != nil {
		return SignInResult{}, err
	}

	// Validate the membership shape before any write, exactly as Create
	// does -- this is what catches an invite whose stored role/capabilities
	// would otherwise reach the repository's transaction and fail there
	// instead of here.
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
