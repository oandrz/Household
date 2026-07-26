package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

const (
	// DevPassword is Andreas's development password. Seed writes it (through
	// Hasher, so only the hash is ever stored) and adminctl prints it on
	// completion, so it is a known constant rather than a secret hidden in a
	// diff: Seed's development-only guard, enforced by its caller, is the
	// only thing standing between it and a production database, and that
	// guard protects this value exactly as it protects everything else Seed
	// writes.
	DevPassword = "hearth-dev-password"

	// devInviteToken is Christine's development invite token. Like
	// DevPassword, it is fixed rather than randomly generated: the token's
	// hash, once persisted, cannot be reversed, so a second run of Seed that
	// finds Christine's invite already sitting in the database has no way to
	// recover whatever raw value produced that hash and print a working URL
	// again -- unless that raw value was never random to begin with. Reusing
	// this constant is also what makes the invite step idempotent at all:
	// Seed looks the invite up by hashing this same string, rather than by
	// any household/email query InviteRepository does not expose.
	devInviteToken = "hearth-dev-invite-token"

	// AndreasEmail identifies the household's first owner. It is exported so
	// adminctl's other subcommands (unlock-household, create-invite) can
	// resolve "the household" the same way Seed itself does, in an app with
	// no household-listing endpoint and exactly one household per
	// deployment.
	AndreasEmail = "andreas@hearth.family"

	christineEmail = "christine@hearth.family"
	householdName  = "Andreas & Christine"
	familyName     = "Oentoro"
)

// SeedDeps gathers every port Seed needs, mirroring AuthDeps/InviteDeps/
// HouseholdDeps. Unlike InviteDeps, it does carry a MembershipRepository:
// InviteDeps never reads a membership (every write it makes goes through a
// port that creates one transactionally), but Seed does, on its
// already-seeded path -- recovering the household ID a second run needs
// means looking up Andreas's own membership.
type SeedDeps struct {
	Households    HouseholdRepository
	Users         UserRepository
	Memberships   MembershipRepository
	Spaces        SpaceRepository
	Notifications NotificationRepository
	Invites       InviteRepository
	Mailer        Mailer
	Hasher        PasswordHasher
	Tokens        TokenGenerator
	Clock         Clock
	BaseURL       string
}

// SeedResult is what a caller (adminctl) needs back: the URL Christine's
// invite lives at.
type SeedResult struct {
	InviteURL string
}

// fixedRawToken always returns the same raw token, so an invite created
// through it produces a deterministic hash -- and therefore a deterministic
// URL -- no matter how many times Seed runs. HashToken still delegates to
// the real generator, so a hash Seed computes independently, to check
// whether that invite already exists, matches exactly what a Create call
// through this wrapper would persist.
type fixedRawToken struct {
	inner TokenGenerator
	raw   string
}

func (f fixedRawToken) NewToken() (string, []byte, error) {
	return f.raw, f.inner.HashToken(f.raw), nil
}

func (f fixedRawToken) HashToken(raw string) []byte { return f.inner.HashToken(raw) }

// Seed writes the design's starting household -- Andreas as a fully
// capable owner, a pending co-owner invite for Christine, Kayla and Ethan as
// limited members, the three builtin spaces, and notification preferences
// with every flag on -- and reports the URL Task 21 needs to accept
// Christine's invite.
//
// Every write is gated on its own idempotency check rather than one
// top-level "already seeded" flag: a Seed call that partially failed
// (Andreas written, Christine's invite email not yet sent because Mailpit
// was still starting up, say) must be safely retryable step by step, not
// short-circuited into reporting an invite URL for a row that was never
// written.
func Seed(ctx context.Context, d SeedDeps) (SeedResult, error) {
	household, andreasID, err := ensureHouseholdAndAndreas(ctx, d)
	if err != nil {
		return SeedResult{}, err
	}

	if err := ensureChildren(ctx, d, household.ID, andreasID); err != nil {
		return SeedResult{}, err
	}

	if err := ensureSpaces(ctx, d, household.ID); err != nil {
		return SeedResult{}, err
	}

	if _, err := d.Notifications.Upsert(ctx, household.ID, NotificationPreferences{
		BillReminders: true, OverspendAlerts: true, RetroReminder: true, WeeklyDigest: true,
	}); err != nil {
		return SeedResult{}, fmt.Errorf("set notification preferences: %w", err)
	}

	inviteURL, err := ensureChristineInvite(ctx, d, household.ID, andreasID)
	if err != nil {
		return SeedResult{}, err
	}

	return SeedResult{InviteURL: inviteURL}, nil
}

// ensureHouseholdAndAndreas returns the seeded household and Andreas's user
// ID, creating both -- together, via CreateWithMembership -- only if Andreas
// does not already exist. There is no inviter for the first owner, so this
// is the one member of the household Seed creates directly rather than
// through InviteService.Create.
func ensureHouseholdAndAndreas(ctx context.Context, d SeedDeps) (domain.Household, string, error) {
	existing, err := d.Users.ByEmail(ctx, AndreasEmail)
	switch {
	case err == nil:
		membership, err := d.Memberships.ByUser(ctx, existing.ID)
		if err != nil {
			return domain.Household{}, "", fmt.Errorf("resolve seeded household: %w", err)
		}
		household, err := d.Households.Get(ctx, membership.HouseholdID)
		if err != nil {
			return domain.Household{}, "", fmt.Errorf("load seeded household: %w", err)
		}
		return household, existing.ID, nil
	case errors.Is(err, domain.ErrNotFound):
		// Falls through to creation below.
	default:
		return domain.Household{}, "", fmt.Errorf("check for existing seed: %w", err)
	}

	household, err := d.Households.Create(ctx, householdName, familyName)
	if err != nil {
		return domain.Household{}, "", fmt.Errorf("create household: %w", err)
	}

	passwordHash, err := d.Hasher.Hash(DevPassword)
	if err != nil {
		return domain.Household{}, "", fmt.Errorf("hash development password: %w", err)
	}

	andreasMembership, err := domain.NewMembership("", household.ID, "", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		return domain.Household{}, "", fmt.Errorf("build andreas's membership: %w", err)
	}

	andreas, _, err := d.Users.CreateWithMembership(ctx, AndreasEmail, passwordHash, "Andreas", andreasMembership)
	if err != nil {
		return domain.Household{}, "", fmt.Errorf("create andreas: %w", err)
	}

	return household, andreas.ID, nil
}

// ensureChildren creates Kayla and Ethan if they are not already members of
// the household, each through InviteService.Create with an empty email --
// the design's "limited member with no credentials of their own" case, which
// that call routes to UserRepository.CreateWithMembership so the user and
// membership are written in one transaction. There is no unique constraint
// on a credential-less member the way there is on an invite's token hash or
// a real email address, so this check (by display name, within the
// household) is the only thing standing between a retried Seed and a second
// Kayla.
func ensureChildren(ctx context.Context, d SeedDeps, householdID, andreasID string) error {
	inviteSvc := seedInviteService(d)

	members, err := d.Memberships.List(ctx, householdID)
	if err != nil {
		return fmt.Errorf("list members: %w", err)
	}
	haveName := make(map[string]bool, len(members))
	for _, v := range members {
		haveName[v.User.DisplayName] = true
	}

	if !haveName["Kayla"] {
		if err := inviteSvc.Create(ctx, householdID, andreasID, "Kayla", "", domain.RoleLimited,
			domain.Capabilities{domain.CapCalendar, domain.CapChores}); err != nil {
			return fmt.Errorf("create kayla: %w", err)
		}
	}
	if !haveName["Ethan"] {
		if err := inviteSvc.Create(ctx, householdID, andreasID, "Ethan", "", domain.RoleLimited,
			domain.Capabilities{domain.CapCalendar}); err != nil {
			return fmt.Errorf("create ethan: %w", err)
		}
	}
	return nil
}

// ensureSpaces creates whichever of domain.BuiltinSpaces the household is
// still missing, keyed on Space.Key -- the same key SpaceRepository's own
// UNIQUE (household_id, key) constraint is built around.
func ensureSpaces(ctx context.Context, d SeedDeps, householdID string) error {
	existing, err := d.Spaces.List(ctx, householdID)
	if err != nil {
		return fmt.Errorf("list spaces: %w", err)
	}
	haveKey := make(map[string]bool, len(existing))
	for _, s := range existing {
		haveKey[s.Key] = true
	}

	for _, s := range domain.BuiltinSpaces(householdID) {
		if haveKey[s.Key] {
			continue
		}
		if _, err := d.Spaces.Create(ctx, s); err != nil {
			return fmt.Errorf("create space %q: %w", s.Key, err)
		}
	}
	return nil
}

// ensureChristineInvite returns the URL for Christine's pending co-owner
// invite, creating it -- with devInviteToken, so the URL is reproducible
// across runs -- only if no invite with that hash exists yet.
// domain.ErrAlreadyExists from the Create call itself is tolerated too,
// closing the race between the ByTokenHash check just above and the write --
// the database's UNIQUE (token_hash) constraint is the authoritative
// backstop for that window, exactly as it is everywhere else this codebase
// relies on translate's unique-violation mapping.
//
// An invite that exists but has expired unaccepted is not treated as "still
// there": inviteTTL (invite.go) is seven days, and a fixed-token invite that
// outlives that window is dead -- InviteService.Accept would reject it with
// domain.ErrInviteExpired -- yet its row still occupies devInviteToken's
// hash, so it can never be recreated under that same value. Reporting the
// fixed URL anyway would hand back one that 410s, the exact silent dead end
// per-step idempotency elsewhere in Seed is meant to avoid. reissueInvite
// mints a genuinely new, random token instead, the only way forward once the
// known one is unusable.
func ensureChristineInvite(ctx context.Context, d SeedDeps, householdID, andreasID string) (string, error) {
	inviteURL := fmt.Sprintf("%s/invite/%s", d.BaseURL, devInviteToken)

	details, err := d.Invites.ByTokenHash(ctx, d.Tokens.HashToken(devInviteToken))
	switch {
	case err == nil:
		if details.AcceptedAt != nil || details.ExpiresAt.After(d.Clock.Now()) {
			// Either already accepted (Task 21 has already succeeded, and a
			// re-seed has nothing useful left to do here) or still pending
			// and live: the fixed URL is correct either way.
			return inviteURL, nil
		}
		return reissueChristineInvite(ctx, d, householdID, andreasID)
	case errors.Is(err, domain.ErrNotFound):
		// Falls through to creation below.
	default:
		return "", fmt.Errorf("check for existing invite: %w", err)
	}

	inviteSvc := seedInviteService(d)
	if err := inviteSvc.Create(ctx, householdID, andreasID, "Christine", christineEmail,
		domain.RoleOwner, domain.AllCapabilities()); err != nil && !errors.Is(err, domain.ErrAlreadyExists) {
		return "", fmt.Errorf("invite christine: %w", err)
	}

	return inviteURL, nil
}

// reissueChristineInvite creates a fresh invite for Christine with a real,
// randomly generated token -- used only once devInviteToken's own invite has
// expired unaccepted (see ensureChristineInvite) and so can no longer be
// recreated under its fixed hash. capturingTokens is what lets this function
// learn the raw value InviteService.Create minted, so it can build the URL
// that value belongs to; Create itself never returns it.
func reissueChristineInvite(ctx context.Context, d SeedDeps, householdID, andreasID string) (string, error) {
	captured := &capturingTokens{inner: d.Tokens}
	inviteSvc := NewInviteService(InviteDeps{
		Invites: d.Invites,
		Users:   d.Users,
		Mailer:  d.Mailer,
		Hasher:  d.Hasher,
		Tokens:  captured,
		Clock:   d.Clock,
		BaseURL: d.BaseURL,
	})
	if err := inviteSvc.Create(ctx, householdID, andreasID, "Christine", christineEmail,
		domain.RoleOwner, domain.AllCapabilities()); err != nil {
		return "", fmt.Errorf("reissue christine's invite: %w", err)
	}
	return fmt.Sprintf("%s/invite/%s", d.BaseURL, captured.lastRaw), nil
}

// capturingTokens wraps a real TokenGenerator, recording the raw value of
// the last token it minted. reissueChristineInvite is its one caller: it
// needs the token InviteService.Create actually used in order to build the
// matching URL, which Create itself never returns. lastRaw is set only once
// NewToken has actually succeeded, so a caller can never observe a token for
// a mint that failed.
type capturingTokens struct {
	inner   TokenGenerator
	lastRaw string
}

func (c *capturingTokens) NewToken() (string, []byte, error) {
	raw, hash, err := c.inner.NewToken()
	if err != nil {
		return "", nil, err
	}
	c.lastRaw = raw
	return raw, hash, nil
}

func (c *capturingTokens) HashToken(raw string) []byte { return c.inner.HashToken(raw) }

// seedInviteService builds an InviteService over d's ports, wrapping Tokens
// in fixedRawToken so any invite this Seed call creates for Christine gets
// the reproducible development token rather than a random one. Kayla and
// Ethan's calls through the same service never touch Tokens at all --
// InviteService.Create only generates a token on its email-present branch --
// so sharing one instance across all three calls is safe.
func seedInviteService(d SeedDeps) *InviteService {
	return NewInviteService(InviteDeps{
		Invites: d.Invites,
		Users:   d.Users,
		Mailer:  d.Mailer,
		Hasher:  d.Hasher,
		Tokens:  fixedRawToken{inner: d.Tokens, raw: devInviteToken},
		Clock:   d.Clock,
		BaseURL: d.BaseURL,
	})
}
