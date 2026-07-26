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

	// devInviteToken is the first rung of Christine's development invite
	// token ladder (see devInviteTokenAt below). Like DevPassword, it is
	// fixed rather than randomly generated: the token's hash, once
	// persisted, cannot be reversed, so a run of Seed that finds an invite
	// already sitting in the database has no way to recover whatever raw
	// value produced its hash and print a working URL again -- unless that
	// raw value was never random to begin with.
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
// means looking up Andreas's own membership, and so does checking whether
// Christine has already accepted.
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
	// InviteURL is empty when there is nothing useful to print: either
	// ChristineIsMember is true, or a live invite for her address already
	// exists that Seed's own token ladder (see devInviteTokenAt) did not
	// create, and so there is no raw token left to reconstruct a URL from.
	InviteURL string
	// ChristineIsMember is true once Christine has actually accepted an
	// invite and become a member of the household. At that point Seed has
	// nothing left to do for her -- printing an invite link would be
	// actively wrong, not just redundant, even on a build where one would
	// still technically resolve.
	ChristineIsMember bool
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

	// Christine's membership is checked before her invite, exactly the way
	// ensureHouseholdAndAndreas checks Andreas's own membership before
	// deciding whether there is anything left to create: once she has
	// accepted, re-issuing (or even just re-reporting a URL for) an invite
	// is wrong, not merely redundant.
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

// christineIsMember reports whether Christine has already accepted an
// invite and holds a membership in this household -- the same
// ByEmail-then-ByUser resolution ensureHouseholdAndAndreas uses for Andreas,
// applied to the one other member Seed has a fixed identity for.
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

// ensureChildren creates Kayla and Ethan if they are not already members of
// the household, each through InviteService.Create with an empty email --
// the design's "limited member with no credentials of their own" case, which
// that call routes to UserRepository.CreateWithMembership so the user and
// membership are written in one transaction.
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
// silently creating a duplicate -- if a credential-less user with this exact
// display name already exists with no membership anywhere. That is exactly
// the state left behind by removing a child's membership without deleting
// the underlying user row: a child has no email, so there is no unique
// constraint (the way there is for a real address) to make a second Kayla
// impossible.
//
// Guessing which such orphan belongs to this seed run -- reattaching it to a
// fresh membership -- would be worse than stopping: it might not be the
// child anyone thinks it is (a name collision, a half-finished manual
// cleanup), and silently repairing that guess wrong is much harder to notice
// than a seed that simply refuses to run. A seed that refuses is
// recoverable by an operator who can inspect and decide; one that silently
// duplicates is not.
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
// invite ladder: devInviteToken itself for the first rung, then
// "<devInviteToken>-2", "-3", and so on. Every rung's raw value is
// reconstructible from nothing but its position, which is what lets Seed
// recover the URL for an invite a previous run created without ever having
// persisted the raw value anywhere -- SHA-256 is one-way, so devInviteToken
// alone could not survive being reissued once, let alone repeatedly.
func devInviteTokenAt(rung int) string {
	if rung == 1 {
		return devInviteToken
	}
	return fmt.Sprintf("%s-%d", devInviteToken, rung)
}

// maxInviteLadderRungs bounds the walks below. Each rung is consumed only by
// a genuine, unaccepted expiry (inviteTTL in invite.go is seven days) --
// reaching even a handful in a real development database would be
// extraordinary. This ceiling exists purely so a bug cannot spin an
// unbounded loop, not because the ladder is expected to run deep in
// practice.
const maxInviteLadderRungs = 1000

// ensureChristineInvite returns the URL for Christine's pending co-owner
// invite. The caller (Seed) has already established that she is not yet a
// member, so this only has to answer one question: is there already a live,
// unaccepted invite for her address at all -- not just at devInviteToken's
// own hash -- and if not, issue one.
//
// This ordering (ask InviteRepository.LiveInviteForEmail first, only create
// on a miss) is what stops the invite-abandonment bug a token-hash-only
// check had: checking solely "is devInviteToken's own row still usable"
// cannot see a live invite this function itself reissued at a later rung on
// a previous run, so it would reissue *again*, abandoning the previous
// reissue live and pending forever and sending a second real email every
// time Seed ran after the first expiry.
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

// findLiveLadderURL walks the ladder looking for the one rung (if any) that
// is currently live: created, not accepted, not expired. It stops at the
// first never-created rung, since Seed always fills rungs in order and never
// skips one.
//
// An empty string with a nil error means LiveInviteForEmail found something
// this ladder itself did not create -- e.g. a manual create-invite for
// Christine's address from outside Seed. There is something genuinely
// pending in that case, but no raw token this function can honestly
// reconstruct a URL from; the caller is expected to report that state
// without fabricating a link.
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
// never-used rung of the ladder. A used rung -- live, expired or accepted --
// can never be reused: its hash already occupies invites.token_hash's UNIQUE
// constraint permanently, so the first free rung is the only place a new
// invite can legally be written.
//
// domain.ErrAlreadyExists from the Create call itself is tolerated: it
// closes the race between the ByTokenHash check just above and the write,
// where a concurrent caller's insert lands in between -- the database's real
// UNIQUE (token_hash) constraint is the authoritative backstop for that
// window, exactly as it is everywhere else this codebase relies on
// translate's unique-violation mapping.
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
			if err := inviteSvc.Create(ctx, householdID, andreasID, "Christine", christineEmail,
				domain.RoleOwner, domain.AllCapabilities()); err != nil && !errors.Is(err, domain.ErrAlreadyExists) {
				return "", fmt.Errorf("invite christine: %w", err)
			}
			return fmt.Sprintf("%s/invite/%s", d.BaseURL, token), nil
		case err != nil:
			return "", fmt.Errorf("check invite ladder rung %d: %w", rung, err)
		}
		// Rung already used (live, expired or accepted) -- try the next one.
	}
	return "", fmt.Errorf("exhausted the development invite token ladder (%d rungs)", maxInviteLadderRungs)
}

// seedInviteService builds an InviteService over d's ports, wrapping Tokens
// in fixedRawToken so any invite created through it for Kayla or Ethan is
// reproducible too -- though in practice neither call ever reaches Tokens at
// all, since InviteService.Create only generates a token on its
// email-present branch, and both children are invited with an empty one.
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
