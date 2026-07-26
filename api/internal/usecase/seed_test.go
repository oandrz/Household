package usecase_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// seedFixture wires fresh, empty doubles for usecase.Seed -- deliberately not
// newFixture's already-populated household, since Seed's whole job is to
// create that household from nothing and do so idempotently.
type seedFixture struct {
	deps          usecase.SeedDeps
	clock         *fixedClock
	households    *householdDouble
	users         *userDouble
	members       *membershipDouble
	spaces        *spaceDouble
	notifications *notificationDouble
	inviteRepo    *inviteDouble
	mailer        *mailerDouble
}

func newSeedFixture() *seedFixture {
	clock := &fixedClock{now: time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)}
	users := newUserDouble()
	members := newMembershipDouble(users)
	users.setMembers(members)
	households := newHouseholdDouble()
	spaces := newSpaceDouble()
	notifications := newNotificationDouble()
	inviteRepo := newInviteDouble(clock, users, members)
	mailer := newMailerDouble()
	hasher := &fakeHasher{}
	tokens := &seqTokens{}

	return &seedFixture{
		deps: usecase.SeedDeps{
			Households:    households,
			Users:         users,
			Memberships:   members,
			Spaces:        spaces,
			Notifications: notifications,
			Invites:       inviteRepo,
			Mailer:        mailer,
			Hasher:        hasher,
			Tokens:        tokens,
			Clock:         clock,
			BaseURL:       "http://localhost:5173",
		},
		clock:      clock,
		households: households, users: users, members: members,
		spaces: spaces, notifications: notifications, inviteRepo: inviteRepo, mailer: mailer,
	}
}

// theHousehold returns the fixture's one household, failing the test if
// there isn't exactly one.
func (f *seedFixture) theHousehold(t *testing.T) domain.Household {
	t.Helper()
	if got := len(f.households.rows); got != 1 {
		t.Fatalf("households = %d, want 1", got)
	}
	for _, h := range f.households.rows {
		return h
	}
	panic("unreachable")
}

func (f *seedFixture) memberByName(t *testing.T, name string) usecase.MemberView {
	t.Helper()
	views, err := f.members.List(context.Background(), f.theHousehold(t).ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, v := range views {
		if v.User.DisplayName == name {
			return v
		}
	}
	t.Fatalf("no member named %q", name)
	panic("unreachable")
}

func TestSeedCreatesExactlyOneHousehold(t *testing.T) {
	f := newSeedFixture()
	if _, err := usecase.Seed(context.Background(), f.deps); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	h := f.theHousehold(t)
	if h.Name != "Andreas & Christine" {
		t.Fatalf("household name = %q, want %q", h.Name, "Andreas & Christine")
	}
}

func TestSeedCreatesAndreasAsAFullyCapableOwner(t *testing.T) {
	f := newSeedFixture()
	if _, err := usecase.Seed(context.Background(), f.deps); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	andreas, err := f.users.ByEmail(context.Background(), usecase.AndreasEmail)
	if err != nil {
		t.Fatalf("ByEmail: %v", err)
	}
	if andreas.PasswordHash == "" {
		t.Fatal("andreas has no password hash")
	}
	hasher := f.deps.Hasher
	if !hasher.Verify(usecase.DevPassword, andreas.PasswordHash) {
		t.Fatal("andreas's password hash does not verify against the development password")
	}

	view := f.memberByName(t, "Andreas")
	if view.Membership.Role != domain.RoleOwner {
		t.Fatalf("andreas's role = %q, want owner", view.Membership.Role)
	}
	for _, cap := range domain.AllCapabilities() {
		if !view.Membership.Capabilities.Has(cap) {
			t.Fatalf("andreas is missing capability %q", cap)
		}
	}
}

func TestSeedCreatesAPendingOwnerInviteForChristine(t *testing.T) {
	f := newSeedFixture()
	result, err := usecase.Seed(context.Background(), f.deps)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}

	const prefix = "http://localhost:5173/invite/"
	if !strings.HasPrefix(result.InviteURL, prefix) {
		t.Fatalf("invite url = %q, want prefix %q", result.InviteURL, prefix)
	}
	token := strings.TrimPrefix(result.InviteURL, prefix)
	if token == "" {
		t.Fatal("invite url carried no token")
	}

	details, err := f.inviteRepo.ByTokenHash(context.Background(), f.deps.Tokens.HashToken(token))
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	if details.Role != domain.RoleOwner {
		t.Fatalf("christine's invited role = %q, want owner", details.Role)
	}
	if details.Name != "Christine" {
		t.Fatalf("invited name = %q, want Christine", details.Name)
	}
	if details.AcceptedAt != nil {
		t.Fatal("christine's invite must still be pending")
	}
	for _, cap := range domain.AllCapabilities() {
		if !details.Capabilities.Has(cap) {
			t.Fatalf("christine's invite is missing capability %q", cap)
		}
	}
}

func TestSeedCreatesKaylaAndEthanAsLimitedMembersWithNoPassword(t *testing.T) {
	f := newSeedFixture()
	if _, err := usecase.Seed(context.Background(), f.deps); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	kayla := f.memberByName(t, "Kayla")
	if kayla.Membership.Role != domain.RoleLimited {
		t.Fatalf("kayla's role = %q, want limited", kayla.Membership.Role)
	}
	if !kayla.Membership.Capabilities.Has(domain.CapCalendar) || !kayla.Membership.Capabilities.Has(domain.CapChores) {
		t.Fatalf("kayla's capabilities = %v, want calendar and chores", kayla.Membership.Capabilities)
	}
	if len(kayla.Membership.Capabilities) != 2 {
		t.Fatalf("kayla's capabilities = %v, want exactly calendar and chores", kayla.Membership.Capabilities)
	}
	kaylaUser, err := f.users.ByID(context.Background(), kayla.User.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if kaylaUser.PasswordHash != "" {
		t.Fatal("kayla must have no password")
	}

	ethan := f.memberByName(t, "Ethan")
	if ethan.Membership.Role != domain.RoleLimited {
		t.Fatalf("ethan's role = %q, want limited", ethan.Membership.Role)
	}
	if !ethan.Membership.Capabilities.Has(domain.CapCalendar) {
		t.Fatalf("ethan's capabilities = %v, want calendar", ethan.Membership.Capabilities)
	}
	if len(ethan.Membership.Capabilities) != 1 {
		t.Fatalf("ethan's capabilities = %v, want exactly calendar", ethan.Membership.Capabilities)
	}
	ethanUser, err := f.users.ByID(context.Background(), ethan.User.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if ethanUser.PasswordHash != "" {
		t.Fatal("ethan must have no password")
	}
}

func TestSeedCreatesTheThreeBuiltinSpaces(t *testing.T) {
	f := newSeedFixture()
	if _, err := usecase.Seed(context.Background(), f.deps); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	householdID := f.theHousehold(t).ID
	got, err := f.spaces.List(context.Background(), householdID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("spaces = %d, want 3 (Money, Marriage, Family)", len(got))
	}
	want := domain.BuiltinSpaces(householdID)
	visibilityByKey := make(map[string]domain.Visibility, len(want))
	for _, s := range want {
		visibilityByKey[s.Key] = s.Visibility
	}
	for _, s := range got {
		wantVis, ok := visibilityByKey[s.Key]
		if !ok {
			t.Fatalf("unexpected space key %q", s.Key)
		}
		if s.Visibility != wantVis {
			t.Fatalf("space %q visibility = %q, want %q", s.Key, s.Visibility, wantVis)
		}
	}
}

func TestSeedCreatesNotificationPreferencesWithEveryFlagOn(t *testing.T) {
	f := newSeedFixture()
	if _, err := usecase.Seed(context.Background(), f.deps); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	householdID := f.theHousehold(t).ID
	prefs, err := f.notifications.Get(context.Background(), householdID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !prefs.BillReminders || !prefs.OverspendAlerts || !prefs.RetroReminder || !prefs.WeeklyDigest {
		t.Fatalf("notification preferences = %+v, want every flag true", prefs)
	}
}

// TestSeedRunTwiceDoesNotDuplicateAnything is the property that makes Task
// 21 reachable: `make seed` runs exactly once by design, but nothing stops
// an operator (or a flaky script) from running it again, and a second run
// must not double up the household's members, spaces or invites -- and must
// still hand back a usable invite URL.
func TestSeedRunTwiceDoesNotDuplicateAnything(t *testing.T) {
	f := newSeedFixture()
	ctx := context.Background()

	first, err := usecase.Seed(ctx, f.deps)
	if err != nil {
		t.Fatalf("first Seed: %v", err)
	}

	second, err := usecase.Seed(ctx, f.deps)
	if err != nil {
		t.Fatalf("second Seed: %v", err)
	}

	if second.InviteURL != first.InviteURL {
		t.Fatalf("second invite url = %q, want the same url as the first run %q", second.InviteURL, first.InviteURL)
	}
	const prefix = "http://localhost:5173/invite/"
	token := strings.TrimPrefix(second.InviteURL, prefix)
	if _, err := f.inviteRepo.ByTokenHash(ctx, f.deps.Tokens.HashToken(token)); err != nil {
		t.Fatalf("the second run's invite url must still resolve: %v", err)
	}

	if got := len(f.households.rows); got != 1 {
		t.Fatalf("households = %d, want 1", got)
	}
	if got := f.users.count(); got != 3 {
		t.Fatalf("users = %d, want 3 (andreas, kayla, ethan -- christine has not accepted)", got)
	}
	if got := f.members.count(); got != 3 {
		t.Fatalf("memberships = %d, want 3", got)
	}
	if got := f.inviteRepo.count(); got != 1 {
		t.Fatalf("invites = %d, want 1", got)
	}
	householdID := f.theHousehold(t).ID
	spaces, err := f.spaces.List(ctx, householdID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := len(spaces); got != 3 {
		t.Fatalf("spaces = %d, want 3", got)
	}
	if got := f.mailer.invitesSentCount(); got != 1 {
		t.Fatalf("invite emails sent = %d, want 1 -- the second run must not re-send it", got)
	}
}

// TestSeedReissuesChristinesInviteOnceTheFixedOneHasExpired guards the gap a
// pure "does an invite already exist" check would leave open: devInviteToken
// is fixed so its URL is reproducible, but a fixed token also means its
// invite row's hash can never be recreated once that row is dead. If Seed
// re-ran after the 7-day inviteTTL had passed and just kept reporting the
// same, now-expired URL, that would be exactly the silent dead end the
// per-step idempotency design elsewhere in Seed exists to avoid.
func TestSeedReissuesChristinesInviteOnceTheFixedOneHasExpired(t *testing.T) {
	f := newSeedFixture()
	ctx := context.Background()

	first, err := usecase.Seed(ctx, f.deps)
	if err != nil {
		t.Fatalf("first Seed: %v", err)
	}

	f.clock.Advance(8 * 24 * time.Hour) // past the seven-day invite TTL

	second, err := usecase.Seed(ctx, f.deps)
	if err != nil {
		t.Fatalf("second Seed: %v", err)
	}

	if second.InviteURL == first.InviteURL {
		t.Fatalf("expected a fresh invite url once the first (%q) has expired, got the same one back", first.InviteURL)
	}

	const prefix = "http://localhost:5173/invite/"
	token := strings.TrimPrefix(second.InviteURL, prefix)
	if token == "" || token == second.InviteURL {
		t.Fatalf("could not extract a token from reissued invite url %q", second.InviteURL)
	}
	details, err := f.inviteRepo.ByTokenHash(ctx, f.deps.Tokens.HashToken(token))
	if err != nil {
		t.Fatalf("ByTokenHash: %v", err)
	}
	if details.AcceptedAt != nil {
		t.Fatal("the reissued invite must be pending, not accepted")
	}
	if !details.ExpiresAt.After(f.clock.Now()) {
		t.Fatal("the reissued invite must not itself already be expired")
	}
	if details.Role != domain.RoleOwner || details.Name != "Christine" {
		t.Fatalf("reissued invite = %+v, want Christine as owner", details)
	}
}
