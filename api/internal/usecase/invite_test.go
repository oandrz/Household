package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// lastInviteToken extracts the raw token from the most recently sent invite
// email's URL -- "<BaseURL>/invite/<token>" -- failing the test if no invite
// was ever sent or the URL carried no token. SendInvite runs synchronously
// on the caller's goroutine (unlike SendMagicLink), so there is no
// background send to wait for the way mailerDouble.waitForSend exists for.
func lastInviteToken(t *testing.T, f *fixture) string {
	t.Helper()
	url := f.mailer.lastInviteURL()
	token := strings.TrimPrefix(url, "http://localhost:5173/invite/")
	if token == "" || token == url {
		t.Fatalf("could not extract a token from invite url %q", url)
	}
	return token
}

func TestCreateWithAnEmailSendsExactlyOneInviteEmail(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Kid", "kid@example.com",
		domain.RoleLimited, domain.Capabilities{domain.CapCalendar}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := f.mailer.invitesSentCount(); got != 1 {
		t.Fatalf("invite emails sent = %d, want 1", got)
	}
	url := f.mailer.lastInviteURL()
	if !strings.HasPrefix(url, "http://localhost:5173/invite/") {
		t.Fatalf("url = %q, want prefix %q", url, "http://localhost:5173/invite/")
	}
	if lastInviteToken(t, f) == "" {
		t.Fatal("invite url carried no token")
	}
}

// TestCreateRejectsAnInviteToAnAddressThatAlreadyHasAUsersRow pins the fix
// for the invite-to-an-existing-member 500: InviteRepo.Accept unconditionally
// calls CreateUser and never reuses an existing row, so an invite to an
// address that already belongs to a user (a mistype of a current member's
// address, or a re-invite) would write successfully, mail successfully, and
// then 500 forever at acceptance -- the invite's own transaction rolling
// back on every retry. Create must refuse this before writing anything, so
// the owner who typed the address sees the problem immediately instead of
// the recipient hitting a dead end later.
func TestCreateRejectsAnInviteToAnAddressThatAlreadyHasAUsersRow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	err := f.invites.Create(ctx, f.householdID, f.andreasID, "Ethan", "ethan@hearth.family",
		domain.RoleLimited, domain.Capabilities{domain.CapCalendar})
	if !errors.Is(err, usecase.ErrInviteeAlreadyRegistered) {
		t.Fatalf("err = %v, want usecase.ErrInviteeAlreadyRegistered", err)
	}
	if got := f.inviteRepo.count(); got != 0 {
		t.Fatalf("invite rows written = %d, want 0", got)
	}
	if got := f.mailer.invitesSentCount(); got != 0 {
		t.Fatalf("invite emails sent = %d, want 0", got)
	}
}

func TestCreateForALimitedMemberWithNoEmailCreatesNoInviteButCreatesTheChild(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	usersBefore, membersBefore := f.users.count(), f.members.count()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Baby", "",
		domain.RoleLimited, domain.Capabilities{domain.CapChores}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := f.inviteRepo.count(); got != 0 {
		t.Fatalf("invite rows = %d, want 0 — a member with no email gets no invite row", got)
	}
	if got := f.mailer.invitesSentCount(); got != 0 {
		t.Fatalf("invite emails sent = %d, want 0", got)
	}
	if got := f.users.count(); got != usersBefore+1 {
		t.Fatalf("users = %d, want %d — the child must still be created", got, usersBefore+1)
	}
	if got := f.members.count(); got != membersBefore+1 {
		t.Fatalf("memberships = %d, want %d — the child must still get a membership", got, membersBefore+1)
	}

	// The child has no email, so it can't be looked up by ByEmail — walk the
	// double's rows directly (same package) to find it by display name, and
	// confirm both halves of the "" <-> SQL NULL convention: an empty email
	// and an empty password hash, not some other placeholder.
	child, ok := findUserByDisplayName(f.users, "Baby")
	if !ok {
		t.Fatal("no user named Baby was created")
	}
	if child.Email != "" {
		t.Fatalf("Email = %q, want \"\" — a child has no login of their own", child.Email)
	}
	if child.PasswordHash != "" {
		t.Fatalf("PasswordHash = %q, want \"\" — a child must be created with an empty password", child.PasswordHash)
	}

	membership, err := f.members.ByUser(ctx, child.ID)
	if err != nil {
		t.Fatalf("ByUser: %v", err)
	}
	if membership.HouseholdID != f.householdID || membership.Role != domain.RoleLimited ||
		len(membership.Capabilities) != 1 || !membership.Capabilities.Has(domain.CapChores) {
		t.Fatalf("membership = %+v", membership)
	}
}

// TestCreateInviteForAChildRollsBackTheUserIfMembershipCreationFails proves
// the fix for the orphaned-user defect a coordinator review caught: the
// child branch used to call Users.Create and Members.Create as two
// independent statements, so a failure in the second left a user row
// committed with a NULL email and no membership -- and because that email
// is NULL, not unique-constrained, a retry would silently create another
// orphan rather than failing loudly. UserRepository.CreateWithMembership
// closes that gap by doing both in one transaction; this test forces the
// membership half to fail (mirroring the real owners_hold_all_capabilities
// constraint) and asserts no user survives.
func TestCreateInviteForAChildRollsBackTheUserIfMembershipCreationFails(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	usersBefore, membersBefore := f.users.count(), f.members.count()
	f.users.failNextCreateWithMembership(errors.New("simulated membership constraint violation"))

	err := f.invites.Create(ctx, f.householdID, f.andreasID, "Baby", "",
		domain.RoleLimited, domain.Capabilities{domain.CapChores})
	if err == nil {
		t.Fatal("expected the simulated membership failure to propagate")
	}

	if got := f.users.count(); got != usersBefore {
		t.Fatalf("users = %d, want %d unchanged — a failed membership insert must not leave an orphaned user", got, usersBefore)
	}
	if got := f.members.count(); got != membersBefore {
		t.Fatalf("memberships = %d, want %d unchanged", got, membersBefore)
	}
}

// findUserByDisplayName is the child-lookup path a real caller doesn't
// need (a child's own display name is not unique in general) but this test
// does, since Create returns nothing to identify the row it wrote and the
// child has no email to look up by.
func findUserByDisplayName(d *userDouble, name string) (usecase.StoredUser, bool) {
	for _, u := range d.byID {
		if u.DisplayName == name {
			return u, true
		}
	}
	return usecase.StoredUser{}, false
}

func TestCreateInviteRejectsALimitedRoleHoldingMarriage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	err := f.invites.Create(ctx, f.householdID, f.andreasID, "Kid", "kid@example.com",
		domain.RoleLimited, domain.Capabilities{domain.CapMarriage})
	if !errors.Is(err, domain.ErrLimitedCannotHoldMarriage) {
		t.Fatalf("err = %v, want ErrLimitedCannotHoldMarriage", err)
	}
	if got := f.inviteRepo.count(); got != 0 {
		t.Fatalf("invite rows = %d, want 0 — the invalid capability set must be rejected before any write", got)
	}
	if got := f.mailer.invitesSentCount(); got != 0 {
		t.Fatalf("invite emails sent = %d, want 0", got)
	}
}

// TestCreateInviteRejectsAnOwnerWithNoEmail guards against the gap a
// coordinator review caught: RoleLimited with an empty email is the design's
// child case (created directly, no invite, no email needed), but any other
// role with an empty email has nowhere for an invite to go. Left unguarded,
// Create would happily write an invite row and "succeed" while the token it
// generated was never mailed to anyone -- a row that just sits there,
// unopenable, until it expires seven days later.
func TestCreateInviteRejectsAnOwnerWithNoEmail(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	usersBefore, membersBefore := f.users.count(), f.members.count()

	err := f.invites.Create(ctx, f.householdID, f.andreasID, "Co-owner", "",
		domain.RoleOwner, domain.AllCapabilities())
	if !errors.Is(err, domain.ErrInviteRequiresEmail) {
		t.Fatalf("err = %v, want ErrInviteRequiresEmail", err)
	}
	if got := f.inviteRepo.count(); got != 0 {
		t.Fatalf("invite rows = %d, want 0 — an undeliverable invite must never be written", got)
	}
	if got := f.mailer.invitesSentCount(); got != 0 {
		t.Fatalf("invite emails sent = %d, want 0", got)
	}
	if got := f.users.count(); got != usersBefore {
		t.Fatalf("users = %d, want %d unchanged — rejecting the combination must create nothing", got, usersBefore)
	}
	if got := f.members.count(); got != membersBefore {
		t.Fatalf("memberships = %d, want %d unchanged", got, membersBefore)
	}
}

func TestPreviewReturnsFamilyNameInviterRoleAndCapabilities(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Kid", "kid@example.com",
		domain.RoleLimited, domain.Capabilities{domain.CapCalendar, domain.CapChores}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := lastInviteToken(t, f)

	preview, err := f.invites.Preview(ctx, token)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if preview.FamilyName != "Oentoro" {
		t.Fatalf("FamilyName = %q, want %q", preview.FamilyName, "Oentoro")
	}
	if preview.InviterName != "Andreas" {
		t.Fatalf("InviterName = %q, want %q", preview.InviterName, "Andreas")
	}
	if preview.Name != "Kid" {
		t.Fatalf("Name = %q, want %q", preview.Name, "Kid")
	}
	if preview.Role != domain.RoleLimited {
		t.Fatalf("Role = %q, want %q", preview.Role, domain.RoleLimited)
	}
	if len(preview.Capabilities) != 2 ||
		!preview.Capabilities.Has(domain.CapCalendar) || !preview.Capabilities.Has(domain.CapChores) {
		t.Fatalf("Capabilities = %+v", preview.Capabilities)
	}
}

func TestPreviewOnAnUnknownInviteTokenReturnsNotFound(t *testing.T) {
	f := newFixture(t)

	if _, err := f.invites.Preview(context.Background(), "no-such-token"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPreviewOnAnExpiredInviteReturnsExpired(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Kid", "kid@example.com",
		domain.RoleLimited, domain.Capabilities{domain.CapCalendar}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := lastInviteToken(t, f)

	f.clock.Advance(7*24*time.Hour + time.Second)

	if _, err := f.invites.Preview(ctx, token); !errors.Is(err, domain.ErrInviteExpired) {
		t.Fatalf("err = %v, want ErrInviteExpired", err)
	}
}

func TestPreviewOnAnAcceptedInviteReturnsAlreadyAccepted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Kid", "kid@example.com",
		domain.RoleLimited, domain.Capabilities{domain.CapCalendar}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := lastInviteToken(t, f)

	if _, err := f.invites.Accept(ctx, token, "supersecretpassword", "Kid"); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	if _, err := f.invites.Preview(ctx, token); !errors.Is(err, domain.ErrInviteAlreadyAccepted) {
		t.Fatalf("err = %v, want ErrInviteAlreadyAccepted", err)
	}
}

func TestAcceptInviteCreatesTheUserTheMembershipAndALiveSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Kid", "kid@example.com",
		domain.RoleLimited, domain.Capabilities{domain.CapCalendar}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := lastInviteToken(t, f)

	result, err := f.invites.Accept(ctx, token, "supersecretpassword", "Kid")
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if result.SessionToken == "" {
		t.Fatal("expected a live session token")
	}
	if result.HouseholdID != f.householdID {
		t.Fatalf("HouseholdID = %q, want %q", result.HouseholdID, f.householdID)
	}
	// live(), not count(): the point of "returns a live session" is that the
	// session is actually usable right now, not merely that a row was
	// created at some point in the past.
	if got := f.sessions.live(); got != 1 {
		t.Fatalf("live sessions = %d, want 1", got)
	}

	user, err := f.users.ByEmail(ctx, "kid@example.com")
	if err != nil {
		t.Fatalf("ByEmail: %v", err)
	}
	if user.ID != result.UserID {
		t.Fatalf("user ID = %q, session UserID = %q", user.ID, result.UserID)
	}

	membership, err := f.members.ByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("ByUser: %v", err)
	}
	if membership.HouseholdID != f.householdID || membership.Role != domain.RoleLimited ||
		len(membership.Capabilities) != 1 || !membership.Capabilities.Has(domain.CapCalendar) {
		t.Fatalf("membership = %+v", membership)
	}

	// The invite itself must be stamped accepted, not just left for the
	// user/membership rows to imply it — Preview is the one caller-visible
	// way to observe that stamp.
	if _, err := f.invites.Preview(ctx, token); !errors.Is(err, domain.ErrInviteAlreadyAccepted) {
		t.Fatalf("Preview after Accept: got %v, want ErrInviteAlreadyAccepted", err)
	}
}

func TestAcceptingAnInviteTwiceFailsTheSecondTimeAndLeavesExactlyOneUserAndMembership(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Kid", "kid@example.com",
		domain.RoleLimited, domain.Capabilities{domain.CapCalendar}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := lastInviteToken(t, f)

	if _, err := f.invites.Accept(ctx, token, "supersecretpassword", "Kid"); err != nil {
		t.Fatalf("first Accept: %v", err)
	}
	usersAfterFirst, membersAfterFirst := f.users.count(), f.members.count()

	if _, err := f.invites.Accept(ctx, token, "supersecretpassword", "Kid"); !errors.Is(err, domain.ErrInviteAlreadyAccepted) {
		t.Fatalf("second Accept: got %v, want ErrInviteAlreadyAccepted", err)
	}

	if got := f.users.count(); got != usersAfterFirst {
		t.Fatalf("users after second Accept = %d, want %d unchanged", got, usersAfterFirst)
	}
	if got := f.members.count(); got != membersAfterFirst {
		t.Fatalf("memberships after second Accept = %d, want %d unchanged", got, membersAfterFirst)
	}
}

func TestAcceptInviteRejectsAPasswordShorterThan12CharactersAndCreatesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Kid", "kid@example.com",
		domain.RoleLimited, domain.Capabilities{domain.CapCalendar}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := lastInviteToken(t, f)
	usersBefore, membersBefore := f.users.count(), f.members.count()

	if _, err := f.invites.Accept(ctx, token, "shortpass", "Kid"); !errors.Is(err, usecase.ErrPasswordTooShort) {
		t.Fatalf("err = %v, want ErrPasswordTooShort", err)
	}

	if got := f.users.count(); got != usersBefore {
		t.Fatalf("users = %d, want %d unchanged", got, usersBefore)
	}
	if got := f.members.count(); got != membersBefore {
		t.Fatalf("memberships = %d, want %d unchanged", got, membersBefore)
	}

	// The invite itself must still be usable — a rejected password must not
	// have consumed it.
	if _, err := f.invites.Preview(ctx, token); err != nil {
		t.Fatalf("Preview after a rejected Accept: %v", err)
	}
}

// TestAcceptInviteRejectsAPasswordOverTheLengthCeilingAndCreatesNothing is
// the mirror of the floor test above: argon2id's cost scales with the size
// of the string it hashes, so Accept must reject an over-length password
// before ever calling Hasher.Hash, the same way it rejects a too-short one.
func TestAcceptInviteRejectsAPasswordOverTheLengthCeilingAndCreatesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Kid", "kid@example.com",
		domain.RoleLimited, domain.Capabilities{domain.CapCalendar}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := lastInviteToken(t, f)
	usersBefore, membersBefore := f.users.count(), f.members.count()

	tooLong := strings.Repeat("a", 257)
	if _, err := f.invites.Accept(ctx, token, tooLong, "Kid"); !errors.Is(err, usecase.ErrPasswordTooLong) {
		t.Fatalf("err = %v, want ErrPasswordTooLong", err)
	}

	if got := f.users.count(); got != usersBefore {
		t.Fatalf("users = %d, want %d unchanged", got, usersBefore)
	}
	if got := f.members.count(); got != membersBefore {
		t.Fatalf("memberships = %d, want %d unchanged", got, membersBefore)
	}

	// The invite itself must still be usable — a rejected password must not
	// have consumed it.
	if _, err := f.invites.Preview(ctx, token); err != nil {
		t.Fatalf("Preview after a rejected Accept: %v", err)
	}
}

// TestAcceptInviteAcceptsAPasswordAtTheLengthCeiling proves the ceiling is
// inclusive: exactly 256 characters is accepted, not rejected as one
// character over the line would be.
func TestAcceptInviteAcceptsAPasswordAtTheLengthCeiling(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Kid", "kid@example.com",
		domain.RoleLimited, domain.Capabilities{domain.CapCalendar}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := lastInviteToken(t, f)

	atLimit := strings.Repeat("b", 256)
	result, err := f.invites.Accept(ctx, token, atLimit, "Kid")
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if result.SessionToken == "" {
		t.Fatal("expected a live session token")
	}
}

// TestListPendingShowsOnlyThisHouseholdsLiveInvites pins "pending" -- not
// accepted, not expired, this household only -- at the service boundary,
// measured by the Clock the service reads rather than by wall time.
func TestListPendingShowsOnlyThisHouseholdsLiveInvites(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ownerCaps := domain.AllCapabilities()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Old", "old@example.com",
		domain.RoleOwner, ownerCaps); err != nil {
		t.Fatalf("Create (old): %v", err)
	}
	f.clock.Advance(8 * 24 * time.Hour) // past the seven-day invite TTL

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Jane", "jane@example.com",
		domain.RoleOwner, ownerCaps); err != nil {
		t.Fatalf("Create (jane): %v", err)
	}
	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Accepted", "accepted@example.com",
		domain.RoleLimited, domain.Capabilities{domain.CapCalendar}); err != nil {
		t.Fatalf("Create (accepted): %v", err)
	}
	if _, err := f.invites.Accept(ctx, lastInviteToken(t, f), "a long enough password", "Accepted"); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if _, err := f.inviteRepo.Create(ctx, "household-2", "stranger@example.com", "Stranger",
		domain.RoleOwner, ownerCaps, []byte("another-household-token-hash"), f.andreasID,
		f.clock.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Create (other household): %v", err)
	}

	got, err := f.invites.ListPending(ctx, f.householdID)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(got) != 1 || got[0].Email != "jane@example.com" || got[0].Role != domain.RoleOwner {
		t.Fatalf("pending = %+v, want only Jane's invite", got)
	}
}

func TestListPendingIsEmptyNotNilForAHouseholdWithNoInvites(t *testing.T) {
	f := newFixture(t)

	got, err := f.invites.ListPending(context.Background(), f.householdID)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want an empty, non-nil slice", got)
	}
}

// TestWithdrawKillsTheInviteLink is the point of withdrawing: the link the
// invitee already holds must stop working, not merely vanish from a list.
func TestWithdrawKillsTheInviteLink(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Jane", "jane@example.com",
		domain.RoleOwner, domain.AllCapabilities()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := lastInviteToken(t, f)
	pending, err := f.invites.ListPending(ctx, f.householdID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("ListPending: %v %+v", err, pending)
	}

	if err := f.invites.Withdraw(ctx, f.householdID, pending[0].ID); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	if _, err := f.invites.Preview(ctx, token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Preview after withdraw: got %v, want domain.ErrNotFound", err)
	}
	if left, _ := f.invites.ListPending(ctx, f.householdID); len(left) != 0 {
		t.Fatalf("still pending after withdraw: %+v", left)
	}
}

func TestWithdrawRefusesAnAcceptedInvite(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Jane", "jane@example.com",
		domain.RoleOwner, domain.AllCapabilities()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	pending, _ := f.invites.ListPending(ctx, f.householdID)
	if _, err := f.invites.Accept(ctx, lastInviteToken(t, f), "a long enough password", "Jane"); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	if err := f.invites.Withdraw(ctx, f.householdID, pending[0].ID); !errors.Is(err, domain.ErrInviteAlreadyAccepted) {
		t.Fatalf("Withdraw after accept: got %v, want domain.ErrInviteAlreadyAccepted", err)
	}
}

func TestWithdrawCannotReachAnotherHouseholdsInvite(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	otherID, err := f.inviteRepo.Create(ctx, "household-2", "stranger@example.com", "Stranger",
		domain.RoleOwner, domain.AllCapabilities(), []byte("another-household-token-hash"), f.andreasID,
		f.clock.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Create (other household): %v", err)
	}

	if err := f.invites.Withdraw(ctx, f.householdID, otherID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Withdraw across households: got %v, want domain.ErrNotFound", err)
	}
	if left, _ := f.inviteRepo.ListPending(ctx, "household-2", f.clock.Now()); len(left) != 1 {
		t.Fatalf("the other household's invite must survive: %+v", left)
	}
}

// --- Task 5: CreateTelegram ---------------------------------------------

// A Telegram invite has no address anywhere: not in the request, not in the
// row, not in any mail. ErrInviteRequiresEmail guarded delivery, not
// identity -- users.email is already nullable and Telegram sign-up already
// creates owners with no address (spec decision 9).
//
// The exact 47-character payload length ("inv_" plus NewToken's 43
// base64url characters, under Telegram's 64-character start limit) is a
// property of the real crypto.TokenGenerator this fixture's seqTokens
// double does not reproduce (it hands out "token-1", "token-2", ...), so
// that invariant is pinned instead by
// TestCreatingATelegramInviteReturnsTheLinkOnce in the http package, which
// runs CreateTelegram over the real token generator end to end.
func TestCreateTelegramWritesARowWithNoEmailAndReturnsTheLinkOnce(t *testing.T) {
	f := newFixture(t)

	link, err := f.invites.CreateTelegram(context.Background(), f.householdID, f.andreasID, "Christine",
		domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	if !strings.HasPrefix(link.URL, "https://t.me/HearthBot?start=inv_") {
		t.Fatalf("link %q does not carry the inv_ payload prefix migration 00018 reserved", link.URL)
	}
	// The raw token is recoverable only by stripping this same prefix --
	// exactly how a real owner's browser would read it off the link, since
	// nothing else ever holds it.
	rawToken := strings.TrimPrefix(link.URL, "https://t.me/HearthBot?start=inv_")
	if rawToken == "" {
		t.Fatal("link carried no token after the inv_ prefix")
	}
	if got, want := link.ExpiresAt, f.clock.now.Add(24*time.Hour); !got.Equal(want) {
		t.Fatalf("expires at %v, want %v (spec decision 8: 24 hours)", got, want)
	}

	row := f.inviteRepo.byID(link.ID)
	if row == nil {
		t.Fatal("no invite row was written")
	}
	if row.Email != "" {
		t.Fatalf("a Telegram invite carries no address, got %q", row.Email)
	}
	if row.Channel != domain.ChannelTelegram {
		t.Fatalf("channel is %q, want telegram", row.Channel)
	}
	if f.mailer.invitesSentCount() != 0 {
		t.Fatal("a Telegram invite must send no mail at all")
	}
}

// The list route never shows the link again: the raw token is not stored,
// so there is nothing to show. This pins that the summary has no way to
// carry one.
func TestPendingListNeverCarriesAnInviteLink(t *testing.T) {
	f := newFixture(t)

	link, err := f.invites.CreateTelegram(context.Background(), f.householdID, f.andreasID, "Christine",
		domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	summaries, err := f.invites.ListPending(context.Background(), f.householdID)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	for _, s := range summaries {
		if s.ID == link.ID && strings.Contains(fmt.Sprint(s), "t.me") {
			t.Fatal("a pending row carried the deep link; it is shown once, at creation")
		}
	}
}

// CreateTelegram is refused outright when no bot is configured on this
// install -- the other half of spec decision 11 (the flag is the HTTP
// layer's job; see TestTelegramInviteIsRefusedWhileTheFlagIsOff in the http
// package).
func TestCreateTelegramRefusesWhenNoBotIsConfigured(t *testing.T) {
	f := newFixture(t)
	f.invites = usecase.NewInviteService(usecase.InviteDeps{
		Invites:           f.inviteRepo,
		Users:             f.users,
		Sessions:          f.sessions,
		Mailer:            f.mailer,
		Hasher:            f.hasher,
		Tokens:            &seqTokens{},
		Clock:             f.clock,
		SessionTTL:        30 * 24 * time.Hour,
		BaseURL:           "http://localhost:5173",
		TelegramInviteTTL: usecase.TelegramInviteTTL,
		// BotUsername left "" on purpose: no bot configured.
	})

	if _, err := f.invites.CreateTelegram(context.Background(), f.householdID, f.andreasID, "Christine",
		domain.RoleOwner, domain.AllCapabilities()); !errors.Is(err, domain.ErrTelegramInvitesUnavailable) {
		t.Fatalf("CreateTelegram with no bot configured: got %v, want domain.ErrTelegramInvitesUnavailable", err)
	}
	if got := f.inviteRepo.count(); got != 0 {
		t.Fatalf("invite rows written = %d, want 0 -- refused before any write", got)
	}
}

// A Telegram invite is admitted by its household's owner, in their own
// browser, and nowhere else (spec decisions 4 and 7). The public web form
// must therefore treat its token as though it had never existed -- not
// refuse it with a reason, which would confirm the token is real.
func TestTheWebFormCannotAcceptATelegramInvite(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	link, err := f.invites.CreateTelegram(ctx, f.householdID, f.andreasID, "Christine",
		domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	// The raw token is recoverable only by stripping the inv_ prefix off the
	// returned link -- exactly how a real owner's browser would read it,
	// since nothing else ever holds it (same approach as
	// TestCreateTelegramWritesARowWithNoEmailAndReturnsTheLinkOnce above).
	rawToken := strings.TrimPrefix(link.URL, "https://t.me/HearthBot?start=inv_")
	if rawToken == "" {
		t.Fatal("link carried no token after the inv_ prefix")
	}

	usersBefore := f.users.count()

	if _, err := f.invites.Preview(ctx, rawToken); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Preview of a Telegram invite: got %v, want domain.ErrNotFound", err)
	}
	if _, err := f.invites.Accept(ctx, rawToken, "a-long-enough-password", "Christine"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Accept of a Telegram invite: got %v, want domain.ErrNotFound", err)
	}

	// Nothing was written: no user, no membership, and the invite is still
	// waiting for its knock.
	row := f.inviteRepo.byID(link.ID)
	if row == nil {
		t.Fatal("the invite row went missing")
	}
	if row.AcceptedAt != nil {
		t.Fatal("the invite was stamped accepted by the web form")
	}
	if got := f.users.count(); got != usersBefore {
		t.Fatalf("users = %d, want %d unchanged -- the web form created a user for a Telegram invite", got, usersBefore)
	}

	// The guard sits before checkInviteLive precisely so this stays
	// domain.ErrNotFound rather than domain.ErrInviteExpired once the
	// invite's TTL has actually passed -- ErrInviteExpired would tell a
	// caller the token was real, just late, which is exactly the leak spec
	// decision 7 rules out. If the guard were ever moved after
	// checkInviteLive, this is the assertion that would start failing.
	f.clock.Advance(usecase.TelegramInviteTTL + time.Second)
	if _, err := f.invites.Preview(ctx, rawToken); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Preview of an expired Telegram invite: got %v, want domain.ErrNotFound", err)
	}
	if _, err := f.invites.Accept(ctx, rawToken, "a-long-enough-password", "Christine"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Accept of an expired Telegram invite: got %v, want domain.ErrNotFound", err)
	}
}

// --- Task 8: NewLink -- a new link, which is also "Not them" -------------

// telegramRawToken recovers the raw token from a TelegramInviteLink's URL by
// stripping the inv_ payload prefix -- exactly how a real owner's browser
// would read it off the link, and the same approach every other Telegram
// invite test in this file uses.
func telegramRawToken(t *testing.T, url string) string {
	t.Helper()
	raw := strings.TrimPrefix(url, "https://t.me/HearthBot?start=inv_")
	if raw == "" || raw == url {
		t.Fatalf("could not extract a token from telegram link %q", url)
	}
	return raw
}

// One route does "get a new link" and "Not them". The old link stops
// working the moment the new one exists -- that is what makes a leaked link
// cost one new link rather than a takeover (spec decision 2).
func TestANewLinkKillsTheOldOneAndClearsTheKnock(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.invites.CreateTelegram(ctx, f.householdID, f.andreasID, "Christine", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	firstToken := telegramRawToken(t, first.URL)
	if _, err := f.invites.Knock(ctx, firstToken, 4242, "jane_t"); err != nil {
		t.Fatalf("Knock: %v", err)
	}

	second, err := f.invites.NewLink(ctx, f.householdID, first.ID)
	if err != nil {
		t.Fatalf("NewLink: %v", err)
	}
	if second.URL == first.URL {
		t.Fatal("the new link is the old link")
	}
	// The knocked chat is told, because from their side the link simply
	// stopped working and nobody would otherwise say why.
	if got := f.chats.lastCancelledChat(); got != 4242 {
		t.Fatalf("cancelled chat %d, want 4242", got)
	}
	// The old token knocks no more, and the row is back to waiting.
	if _, err := f.invites.Knock(ctx, firstToken, 5555, "someone"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the old token still knocks: %v", err)
	}
	if row := f.inviteRepo.byID(first.ID); row.KnockedAt != nil {
		t.Fatal("the knock was not cleared")
	}
	// And the fresh token does.
	secondToken := telegramRawToken(t, second.URL)
	if _, err := f.invites.Knock(ctx, secondToken, 5555, "someone"); err != nil {
		t.Fatalf("the new token does not knock: %v", err)
	}
}

// An email invite has no link to replace. Refused with its own message
// rather than silently converted: the channel is fixed when the invite is
// created (spec decision 9).
func TestANewLinkIsRefusedForAnEmailInvite(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// No flag setup here: the email_invites flag is enforced at the HTTP
	// edge, and InviteService.Create never reads a flag. Setting one here
	// would imply a coupling that does not exist.
	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Jane", "jane@example.com",
		domain.RoleOwner, domain.AllCapabilities()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	pending, err := f.invites.ListPending(ctx, f.householdID)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}

	if _, err := f.invites.NewLink(ctx, f.householdID, pending[0].ID); !errors.Is(err, domain.ErrInviteNotTelegram) {
		t.Fatalf("got %v, want domain.ErrInviteNotTelegram", err)
	}
}

// An id from another household is a 404, never a 403: the answer must not
// confirm that another household's invite exists (docs/LEARNING.md pattern
// 24).
func TestANewLinkForAnotherHouseholdsInviteIsNotFound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mine, err := f.invites.CreateTelegram(ctx, f.householdID, f.andreasID, "Christine", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	if _, err := f.invites.NewLink(ctx, "some-other-household", mine.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want domain.ErrNotFound", err)
	}
}

// The courtesy message to the knocked chat is best-effort: NewLink's own
// doc comment says the send happens after the write and its failure is
// logged, never returned, because the owner must still get the new link
// they asked for. Mutation check: inline the return of that error and this
// test turns red.
func TestANewLinkStillArrivesWhenTellingTheKnockedChatFails(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.invites.CreateTelegram(ctx, f.householdID, f.andreasID, "Christine", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	firstToken := telegramRawToken(t, first.URL)
	if _, err := f.invites.Knock(ctx, firstToken, 4242, "jane_t"); err != nil {
		t.Fatalf("Knock: %v", err)
	}
	f.chats.failNextSendLinkCancelled(errors.New("telegram is down"))

	second, err := f.invites.NewLink(ctx, f.householdID, first.ID)
	if err != nil {
		t.Fatalf("NewLink: %v, want nil -- a courtesy-message failure must not cost the owner their new link", err)
	}
	if second.URL == "" {
		t.Fatal("no link was returned")
	}
	// The send was still attempted, and its failure is what this test
	// arms -- only the return value to the caller is unaffected.
	if got := f.chats.lastCancelledChat(); got != 4242 {
		t.Fatalf("the send was still attempted for chat %d, want 4242", got)
	}
}

// --- Task 9: Admit -- Let in, the whole point of the milestone ----------

// Let in, the whole point of the milestone: one click turns a knock into a
// member, and the bot sends them a sign-in link in their own chat.
func TestAdmitCreatesTheMemberAndSendsTheirSignInLink(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	invite, err := f.invites.CreateTelegram(ctx, f.householdID, f.andreasID, "Christine", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	rawToken := telegramRawToken(t, invite.URL)
	if _, err := f.invites.Knock(ctx, rawToken, 4242, "christine_t"); err != nil {
		t.Fatalf("Knock: %v", err)
	}

	member, err := f.invites.Admit(ctx, f.householdID, invite.ID)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if !member.SignInSent {
		t.Fatal("signInSent is false although the send succeeded")
	}
	if member.Role != domain.RoleOwner {
		t.Fatalf("role is %q; Let in grants exactly what the invite said (spec decision 14)", member.Role)
	}
	if got := f.chats.lastSignInChat(); got != 4242 {
		t.Fatalf("the sign-in link went to chat %d, want the chat that knocked (4242)", got)
	}
	if bound := f.inviteAccounts.userForChat(4242); bound != member.UserID {
		t.Fatalf("chat 4242 is bound to %q, want the new member %q", bound, member.UserID)
	}
	// The invite is spent: a second Let in, and a second knock, both refuse.
	if _, err := f.invites.Admit(ctx, f.householdID, invite.ID); !errors.Is(err, domain.ErrInviteAlreadyAccepted) {
		t.Fatalf("second Admit: got %v, want domain.ErrInviteAlreadyAccepted", err)
	}
	if _, err := f.invites.Knock(ctx, rawToken, 5555, "someone"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second Knock on an admitted invite: got %v, want domain.ErrNotFound", err)
	}
}

// Nobody has knocked yet, or a new link was issued since. Either way there
// is no one to let in, and nothing is written.
func TestAdmitRefusesWhenNobodyIsWaiting(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	invite, err := f.invites.CreateTelegram(ctx, f.householdID, f.andreasID, "Christine", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}

	usersBefore := f.users.count()
	if _, err := f.invites.Admit(ctx, f.householdID, invite.ID); !errors.Is(err, domain.ErrInviteNotKnocked) {
		t.Fatalf("got %v, want domain.ErrInviteNotKnocked", err)
	}
	if got := f.users.count(); got != usersBefore {
		t.Fatal("a user was created for an invite nobody knocked on")
	}
}

// An id from another household is a 404, never a 409 or anything else that
// would confirm the invite exists (docs/LEARNING.md pattern 24).
func TestAdmitForAnotherHouseholdsInviteIsNotFound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	invite, err := f.invites.CreateTelegram(ctx, f.householdID, f.andreasID, "Christine", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	rawToken := telegramRawToken(t, invite.URL)
	if _, err := f.invites.Knock(ctx, rawToken, 4242, "christine_t"); err != nil {
		t.Fatalf("Knock: %v", err)
	}

	if _, err := f.invites.Admit(ctx, "some-other-household", invite.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want domain.ErrNotFound", err)
	}
}

// The member exists; only the message failed. Saying so is the whole of the
// recovery path -- the chat is bound now, so any /start already sends them
// a fresh sign-in link (spec decision 6).
func TestAdmitReportsAFailedSendWithoutLosingTheMember(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	invite, err := f.invites.CreateTelegram(ctx, f.householdID, f.andreasID, "Christine", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	rawToken := telegramRawToken(t, invite.URL)
	if _, err := f.invites.Knock(ctx, rawToken, 4242, "christine_t"); err != nil {
		t.Fatalf("Knock: %v", err)
	}
	f.chats.failNextSignIn()

	member, err := f.invites.Admit(ctx, f.householdID, invite.ID)
	if err != nil {
		t.Fatalf("Admit must succeed when only the send failed: %v", err)
	}
	if member.SignInSent {
		t.Fatal("signInSent is true although the send failed")
	}
	if member.MembershipID == "" {
		t.Fatal("the member was lost because a message could not be sent")
	}
	if bound := f.inviteAccounts.userForChat(4242); bound != member.UserID {
		t.Fatalf("chat 4242 is bound to %q, want the new member %q -- a failed send must not undo the binding", bound, member.UserID)
	}
}

// The chat may have bound itself to a different account between the knock
// and the click (spec decision 15) -- the same race
// TestAdmitLeavesNothingBehindWhenTheChatIsAlreadyBound proves against real
// Postgres. Here it is forced through the double to prove
// InviteService.Admit itself passes the sentinel through untranslated.
func TestAdmitRefusesWhenTheChatIsAlreadyBound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	invite, err := f.invites.CreateTelegram(ctx, f.householdID, f.andreasID, "Christine", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("CreateTelegram: %v", err)
	}
	rawToken := telegramRawToken(t, invite.URL)
	if _, err := f.invites.Knock(ctx, rawToken, 4242, "christine_t"); err != nil {
		t.Fatalf("Knock: %v", err)
	}
	f.inviteAccounts.bind(4242, "someone-else-entirely")

	usersBefore := f.users.count()
	membersBefore := f.members.count()
	if _, err := f.invites.Admit(ctx, f.householdID, invite.ID); !errors.Is(err, domain.ErrChatAlreadyBound) {
		t.Fatalf("got %v, want domain.ErrChatAlreadyBound", err)
	}
	if got := f.users.count(); got != usersBefore {
		t.Errorf("users = %d, want %d -- a refused Admit must leave no user behind", got, usersBefore)
	}
	if got := f.members.count(); got != membersBefore {
		t.Errorf("memberships = %d, want %d -- a refused Admit must leave no membership behind", got, membersBefore)
	}
}
