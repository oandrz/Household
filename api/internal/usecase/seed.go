package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

const (
	// DevPassword is Andreas's development password. Seed writes only its
	// hash (via Hasher) and adminctl prints it on completion, so it is a
	// known constant, not a secret hidden in a diff: Seed's development-only
	// guard, enforced by its caller, is the only thing standing between it
	// and a production database, and that guard protects this exactly like
	// everything else Seed writes.
	DevPassword = "hearth-dev-password"

	// devInviteToken is the first rung of Christine's development invite
	// token ladder (see devInviteTokenAt). Like DevPassword it is fixed, not
	// random: once persisted, its hash can't be reversed, so a Seed run that
	// finds an existing invite could never recover the raw value to print a
	// working URL again.
	devInviteToken = "hearth-dev-invite-token"

	// AndreasEmail identifies the household's first owner. Exported so
	// adminctl's other subcommands (unlock-household, create-invite) can
	// resolve "the household" the same way Seed does -- there is no
	// household-listing endpoint, and exactly one household per deployment.
	AndreasEmail = "andreas@hearth.family"

	christineEmail = "christine@hearth.family"
	householdName  = "Andreas & Christine"
	familyName     = "Oentoro"
)

// SeedDeps gathers every port Seed needs, mirroring AuthDeps/InviteDeps/
// HouseholdDeps. Unlike InviteDeps, it carries a MembershipRepository: Seed
// reads a membership on its already-seeded path, to recover the household
// ID and to check whether Christine has accepted.
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

// SeedResult is what a caller (adminctl) needs back to tell the operator
// what happened to Christine's invite.
type SeedResult struct {
	// InviteURL is empty when there's nothing to print: ChristineIsMember is
	// true, or a live invite for her address exists that the token ladder
	// (see devInviteTokenAt) didn't create, leaving no raw token to
	// reconstruct a URL from.
	InviteURL string
	// ChristineIsMember is true once Christine has accepted an invite and
	// become a member. Seed then has nothing left to do for her -- printing
	// an invite link would be actively wrong, not just redundant.
	ChristineIsMember bool
}

// fixedRawToken always returns the same raw token, so an invite created
// through it has a deterministic hash and URL no matter how many times Seed
// runs. HashToken still delegates to the real generator, so a hash computed
// independently to check whether an invite already exists matches what
// Create would persist.
type fixedRawToken struct {
	inner TokenGenerator
	raw   string
}

func (f fixedRawToken) NewToken() (string, []byte, error) {
	return f.raw, f.inner.HashToken(f.raw), nil
}

func (f fixedRawToken) HashToken(raw string) []byte { return f.inner.HashToken(raw) }

// Seed writes the design's starting household: Andreas as owner, a pending
// co-owner invite for Christine, Kayla and Ethan as limited members, the
// three builtin spaces, and notification preferences with every flag on. It
// reports the URL adminctl prints so an operator can accept Christine's
// invite.
//
// Every write is gated on its own idempotency check, not one top-level
// "already seeded" flag: a call that partially failed (Andreas written,
// Christine's invite mail not yet sent, say) must be safely retryable step
// by step, not short-circuited into reporting a URL for a row that was
// never written.
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

	if _, err := d.Notifications.Upsert(ctx, household.ID, DefaultNotificationPreferences()); err != nil {
		return SeedResult{}, fmt.Errorf("set notification preferences: %w", err)
	}

	// Christine's membership is checked before her invite, the same way
	// ensureHouseholdAndAndreas checks Andreas's: once she has accepted,
	// re-issuing or re-reporting an invite URL is wrong, not merely
	// redundant.
	isMember, err := christineIsMember(ctx, d, household.ID)
	if err != nil {
		return SeedResult{}, err
	}
	if isMember {
		return SeedResult{ChristineIsMember: true}, nil
	}

	inviteURL, err := ensureChristineInvite(ctx, d, household.ID, andreasID)
	if err != nil {
		return SeedResult{}, err
	}

	return SeedResult{InviteURL: inviteURL}, nil
}

// ensureHouseholdAndAndreas returns the seeded household and Andreas's user
// ID, creating both together (via CreateWithMembership) only if Andreas
// doesn't already exist. There's no inviter for the first owner, so this is
// the one member Seed creates directly rather than through
// InviteService.Create.
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

	// Asia/Singapore because the design's household lives there, the same
	// reason its currency is SGD.
	blueprint, err := NewSignupBlueprint(householdName, "Andreas", "SGD", "Asia/Singapore")
	if err != nil {
		return domain.Household{}, "", fmt.Errorf("build the seed blueprint: %w", err)
	}
	// The design's household is the one place that keeps the dual-currency
	// display on: Andreas and Christine really do track SGD against IDR,
	// unlike a self-serve household (secondary == primary, toggle off --
	// see NewSignupBlueprint), so these three fields are overridden here
	// rather than baked into the blueprint constructor.
	blueprint.FamilyName = familyName
	blueprint.SecondaryCurrency = "IDR"
	blueprint.ShowSecondaryCurrency = true

	household, err := d.Households.Create(ctx, blueprint.Household())
	if err != nil {
		return domain.Household{}, "", fmt.Errorf("create household: %w", err)
	}

	passwordHash, err := d.Hasher.Hash(DevPassword)
	if err != nil {
		return domain.Household{}, "", fmt.Errorf("hash development password: %w", err)
	}

	andreasMembership, err := domain.NewMembership("", household.ID, "",
		blueprint.OwnerRole, blueprint.OwnerCapabilities)
	if err != nil {
		return domain.Household{}, "", fmt.Errorf("build andreas's membership: %w", err)
	}

	andreas, _, err := d.Users.CreateWithMembership(ctx, AndreasEmail, passwordHash,
		blueprint.OwnerDisplayName, andreasMembership)
	if err != nil {
		return domain.Household{}, "", fmt.Errorf("create andreas: %w", err)
	}

	return household, andreas.ID, nil
}

// christineIsMember reports whether Christine has accepted an invite and
// holds a membership in this household -- the same ByEmail-then-ByUser
// resolution ensureHouseholdAndAndreas uses for Andreas.
func christineIsMember(ctx context.Context, d SeedDeps, householdID string) (bool, error) {
	user, err := d.Users.ByEmail(ctx, christineEmail)
	switch {
	case err == nil:
		// Falls through to the membership check below.
	case errors.Is(err, domain.ErrNotFound):
		return false, nil
	default:
		return false, fmt.Errorf("check for christine's account: %w", err)
	}

	membership, err := d.Memberships.ByUser(ctx, user.ID)
	switch {
	case err == nil:
		return membership.HouseholdID == householdID, nil
	case errors.Is(err, domain.ErrNotFound):
		// An account exists (e.g. from a prior, unusual acceptance flow) but
		// holds no membership anywhere -- not a member of this household.
		return false, nil
	default:
		return false, fmt.Errorf("check christine's membership: %w", err)
	}
}

// ensureChildren creates Kayla and Ethan if they aren't already members,
// each through InviteService.Create with an empty email -- the "limited
// member with no credentials" case, which writes the user and membership in
// one transaction via UserRepository.CreateWithMembership.
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
		if err := ensureChild(ctx, d, inviteSvc, householdID, andreasID, "Kayla",
			domain.Capabilities{domain.CapCalendar, domain.CapChores}); err != nil {
			return err
		}
	}
	if !haveName["Ethan"] {
		if err := ensureChild(ctx, d, inviteSvc, householdID, andreasID, "Ethan",
			domain.Capabilities{domain.CapCalendar}); err != nil {
			return err
		}
	}
	return nil
}

// ensureChild creates one credential-less child, but refuses -- rather than
// silently duplicating -- if a credential-less user with this exact display
// name already exists with no membership anywhere. That's exactly the state
// left by removing a child's membership without deleting the user row: a
// child has no email, so there's no unique constraint (unlike a real
// address) to make a second Kayla impossible.
//
// Guessing which orphan belongs to this run and reattaching it would be
// worse than stopping: it might not be the right child (a name collision, a
// half-finished cleanup), and a wrong silent repair is much harder to
// notice than a refusal. A seed that refuses is recoverable by an operator
// who can inspect; one that silently duplicates is not.
func ensureChild(ctx context.Context, d SeedDeps, inviteSvc *InviteService,
	householdID, andreasID, name string, caps domain.Capabilities) error {
	orphan, err := d.Users.FindOrphanedChild(ctx, name)
	switch {
	case err == nil:
		return fmt.Errorf(
			"a credential-less user named %q (id %s) already exists with no membership in this household; "+
				"remove that user or restore their membership before running seed again", name, orphan.ID)
	case errors.Is(err, domain.ErrNotFound):
		// No orphan under this name -- safe to create.
	default:
		return fmt.Errorf("check for an orphaned %q: %w", name, err)
	}

	if err := inviteSvc.Create(ctx, householdID, andreasID, name, "", domain.RoleLimited, caps); err != nil {
		return fmt.Errorf("create %s: %w", name, err)
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

// devInviteTokenAt returns the fixed raw token for one rung of Christine's
// invite ladder: devInviteToken for rung 1, then "<devInviteToken>-2", "-3",
// and so on. Every rung's value is reconstructible from its position alone,
// which lets Seed recover the URL for a previously-created invite without
// ever persisting the raw value -- SHA-256 is one-way, so a single fixed
// token couldn't survive being reissued.
func devInviteTokenAt(rung int) string {
	if rung == 1 {
		return devInviteToken
	}
	return fmt.Sprintf("%s-%d", devInviteToken, rung)
}

// maxInviteLadderRungs bounds the walks below. Each rung is consumed only by
// a genuine unaccepted expiry (inviteTTL, seven days), so reaching even a
// handful in a real dev database would be extraordinary -- this ceiling
// exists purely so a bug can't spin an unbounded loop.
const maxInviteLadderRungs = 1000

// ensureChristineInvite returns the URL for Christine's pending co-owner
// invite. Seed has already confirmed she isn't a member, so this only
// answers one question: is there already a live, unaccepted invite for her
// address at all (not just at devInviteToken's own hash) -- and if not,
// issues one.
//
// This checks InviteRepository.LiveInviteForEmail first and only creates on
// a miss. Don't check solely whether devInviteToken's own row is usable:
// that misses a live invite this function reissued at a later rung on a
// previous run, causing a second reissue -- abandoning the first live and
// pending forever, and re-sending mail every time Seed runs after an
// expiry.
func ensureChristineInvite(ctx context.Context, d SeedDeps, householdID, andreasID string) (string, error) {
	_, err := d.Invites.LiveInviteForEmail(ctx, householdID, christineEmail)
	switch {
	case err == nil:
		return findLiveLadderURL(ctx, d)
	case errors.Is(err, domain.ErrNotFound):
		return issueChristineInviteAtNextRung(ctx, d, householdID, andreasID)
	default:
		return "", fmt.Errorf("check for a live invite for christine: %w", err)
	}
}

// findLiveLadderURL walks the ladder for the one rung (if any) that's
// currently live: created, not accepted, not expired. It stops at the first
// never-created rung, since Seed always fills rungs in order.
//
// An empty string with a nil error means LiveInviteForEmail found something
// this ladder didn't create -- e.g. a manual create-invite for Christine's
// address. Something is genuinely pending, but there's no raw token to
// honestly reconstruct a URL from; the caller reports that state without
// fabricating a link.
func findLiveLadderURL(ctx context.Context, d SeedDeps) (string, error) {
	now := d.Clock.Now()
	for rung := 1; rung <= maxInviteLadderRungs; rung++ {
		token := devInviteTokenAt(rung)
		details, err := d.Invites.ByTokenHash(ctx, d.Tokens.HashToken(token))
		switch {
		case errors.Is(err, domain.ErrNotFound):
			return "", nil
		case err != nil:
			return "", fmt.Errorf("check invite ladder rung %d: %w", rung, err)
		}
		if details.AcceptedAt == nil && details.ExpiresAt.After(now) {
			return fmt.Sprintf("%s/invite/%s", d.BaseURL, token), nil
		}
	}
	return "", fmt.Errorf("exhausted the development invite token ladder (%d rungs) looking for a live invite",
		maxInviteLadderRungs)
}

// issueChristineInviteAtNextRung creates Christine's invite at the first
// never-used rung. A used rung -- live, expired or accepted -- can never be
// reused: its hash already occupies invites.token_hash's UNIQUE constraint
// permanently, so the first free rung is the only place a new invite can
// legally be written.
//
// domain.ErrAlreadyExists from Create is tolerated: it closes the race
// between the ByTokenHash check above and the write, where a concurrent
// insert lands in between. The database's UNIQUE (token_hash) constraint is
// the real backstop, exactly as translate's unique-violation mapping relies
// on elsewhere.
//
// ErrInviteeAlreadyRegistered is not tolerated the same way: it means
// christineEmail already has an orphaned users row, left when
// MemberService.Remove deletes a membership but not the user underneath it.
// That's not a race to retry past (InviteService.Create refuses to write
// anything until it's resolved, exactly as intended), so this surfaces it
// through christineOrphanedUserError instead -- naming the row and the
// remedy, the same way ensureChild does for a credential-less child.
func issueChristineInviteAtNextRung(ctx context.Context, d SeedDeps, householdID, andreasID string) (string, error) {
	for rung := 1; rung <= maxInviteLadderRungs; rung++ {
		token := devInviteTokenAt(rung)
		_, err := d.Invites.ByTokenHash(ctx, d.Tokens.HashToken(token))
		switch {
		case errors.Is(err, domain.ErrNotFound):
			inviteSvc := NewInviteService(InviteDeps{
				Invites: d.Invites,
				Users:   d.Users,
				Mailer:  d.Mailer,
				Hasher:  d.Hasher,
				Tokens:  fixedRawToken{inner: d.Tokens, raw: token},
				Clock:   d.Clock,
				BaseURL: d.BaseURL,
			})
			err := inviteSvc.Create(ctx, householdID, andreasID, "Christine", christineEmail,
				domain.RoleOwner, domain.AllCapabilities())
			switch {
			case err == nil, errors.Is(err, domain.ErrAlreadyExists):
				return fmt.Sprintf("%s/invite/%s", d.BaseURL, token), nil
			case errors.Is(err, ErrInviteeAlreadyRegistered):
				return "", christineOrphanedUserError(ctx, d)
			default:
				return "", fmt.Errorf("invite christine: %w", err)
			}
		case err != nil:
			return "", fmt.Errorf("check invite ladder rung %d: %w", rung, err)
		}
		// Rung already used (live, expired or accepted) -- try the next one.
	}
	return "", fmt.Errorf("exhausted the development invite token ladder (%d rungs)", maxInviteLadderRungs)
}

// christineOrphanedUserError builds the actionable error
// issueChristineInviteAtNextRung returns when Create refuses because
// christineEmail already has a users row -- necessarily an orphan, since
// Seed has already confirmed she isn't a member. Names the row and the
// remedy, mirroring ensureChild's orphan case, so an operator gets a
// concrete next step instead of a bare wrapped error.
func christineOrphanedUserError(ctx context.Context, d SeedDeps) error {
	existing, err := d.Users.ByEmail(ctx, christineEmail)
	if err != nil {
		return fmt.Errorf(
			"a users row for %q already exists with no membership in this household, blocking her invite, "+
				"but it could not be looked up to report its id: %w", christineEmail, err)
	}
	return fmt.Errorf(
		"a users row for %q (id %s) already exists with no membership in this household; "+
			"remove that user or restore their membership before running seed again",
		christineEmail, existing.ID)
}

// seedInviteService builds an InviteService over d's ports, wrapping Tokens
// in fixedRawToken so any invite for Kayla or Ethan would also be
// reproducible -- though neither call reaches Tokens in practice, since
// Create only generates a token on its email-present branch, and both
// children are invited with an empty one.
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
