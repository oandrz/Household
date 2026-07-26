package usecase_test

import (
	"context"
	"errors"
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
