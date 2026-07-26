package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// seedLiveSession gives userID a live session, keyed by a token derived from
// the userID itself so tests never need to reuse fixture.sessions' token
// bookkeeping -- only its liveForUser/live counts.
func seedLiveSession(t *testing.T, f *fixture, userID string) {
	t.Helper()
	if err := f.sessions.Create(context.Background(), []byte("session-for-"+userID), userID, f.householdID,
		f.clock.now.Add(24*time.Hour)); err != nil {
		t.Fatalf("seed live session for %s: %v", userID, err)
	}
}

func TestUpdateDemotingTheOnlyOwnerReturnsErrLastOwnerAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	seedLiveSession(t, f, f.andreasID)

	// Andreas is the household's only owner. Demoting him to limited -- with
	// a capability set that is otherwise perfectly valid for a limited role
	// (no marriage) -- must be rejected for the last-owner reason, not any
	// capability-shape reason.
	err := f.memberSvc.Update(ctx, f.householdID, "membership-andreas", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar, domain.CapChores, domain.CapMoney})
	if !errors.Is(err, domain.ErrLastOwner) {
		t.Fatalf("err = %v, want domain.ErrLastOwner", err)
	}

	m, ok := f.members.byID["membership-andreas"]
	if !ok {
		t.Fatal("membership-andreas vanished")
	}
	if m.Role != domain.RoleOwner || len(m.Capabilities) != len(domain.AllCapabilities()) {
		t.Fatalf("membership = %+v, want unchanged owner with all capabilities", m)
	}
	if got := f.sessions.liveForUser(f.andreasID); got != 1 {
		t.Fatalf("live sessions for Andreas = %d, want 1 (untouched) — a rejected update must revoke nothing", got)
	}
}

func TestUpdateGrantingMarriageToALimitedMemberReturnsErrLimitedCannotHoldMarriage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	err := f.memberSvc.Update(ctx, f.householdID, "membership-ethan", domain.RoleLimited,
		domain.Capabilities{domain.CapChores, domain.CapMarriage})
	if !errors.Is(err, domain.ErrLimitedCannotHoldMarriage) {
		t.Fatalf("err = %v, want domain.ErrLimitedCannotHoldMarriage", err)
	}

	m := f.members.byID["membership-ethan"]
	if len(m.Capabilities) != 1 || !m.Capabilities.Has(domain.CapChores) {
		t.Fatalf("membership = %+v, want unchanged", m)
	}
}

func TestUpdatePromotingAMemberToOwnerWithAPartialCapabilitySetReturnsErrOwnerMustHoldAllCapabilities(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	err := f.memberSvc.Update(ctx, f.householdID, "membership-ethan", domain.RoleOwner,
		domain.Capabilities{domain.CapCalendar, domain.CapChores, domain.CapMoney}) // missing marriage
	if !errors.Is(err, domain.ErrOwnerMustHoldAllCapabilities) {
		t.Fatalf("err = %v, want domain.ErrOwnerMustHoldAllCapabilities", err)
	}

	m := f.members.byID["membership-ethan"]
	if m.Role != domain.RoleLimited {
		t.Fatalf("membership = %+v, want unchanged limited role", m)
	}
}

func TestUpdateOnAMembershipIDThatDoesNotExistInTheHouseholdReturnsErrNotFound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	err := f.memberSvc.Update(ctx, f.householdID, "no-such-membership", domain.RoleLimited,
		domain.Capabilities{domain.CapChores})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}
}

// TestUpdateChangingOnlyCapabilitiesOfALimitedMemberSucceedsWithNoOtherOwner
// proves the last-owner rule is consulted only when ownership is actually at
// stake, in a household that has no owner at all. That state is unreachable
// through MemberService's own API -- Update and Remove both refuse to strip
// a household of its last owner -- so the test reaches directly into
// membershipDouble's unexported byID/byUser maps to delete Andreas' owner
// membership, bypassing the service entirely. This is a test-only shortcut
// to construct a state the domain rule must still handle correctly, not a
// capability MemberService offers or a path a real caller can reach. A pure
// capability edit on Ethan, who stays RoleLimited throughout, must still
// succeed once that state exists.
func TestUpdateChangingOnlyCapabilitiesOfALimitedMemberSucceedsWithNoOtherOwner(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	delete(f.members.byID, "membership-andreas")
	delete(f.members.byUser, f.andreasID)

	err := f.memberSvc.Update(ctx, f.householdID, "membership-ethan", domain.RoleLimited,
		domain.Capabilities{domain.CapChores, domain.CapCalendar})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	m := f.members.byID["membership-ethan"]
	if len(m.Capabilities) != 2 || !m.Capabilities.Has(domain.CapChores) || !m.Capabilities.Has(domain.CapCalendar) {
		t.Fatalf("membership = %+v, want both capabilities", m)
	}
}

func TestUpdateSucceedingRevokesThatMembersSessionsOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	seedLiveSession(t, f, f.ethanID)
	seedLiveSession(t, f, f.andreasID)

	err := f.memberSvc.Update(ctx, f.householdID, "membership-ethan", domain.RoleLimited,
		domain.Capabilities{domain.CapChores, domain.CapCalendar})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if got := f.sessions.liveForUser(f.ethanID); got != 0 {
		t.Fatalf("live sessions for Ethan = %d, want 0 — the update must revoke them", got)
	}
	if got := f.sessions.liveForUser(f.andreasID); got != 1 {
		t.Fatalf("live sessions for Andreas = %d, want 1 (untouched) — the revoke must be scoped to the target member", got)
	}
}

func TestRemoveOfTheOnlyOwnerReturnsErrLastOwner(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	err := f.memberSvc.Remove(ctx, f.householdID, "membership-andreas")
	if !errors.Is(err, domain.ErrLastOwner) {
		t.Fatalf("err = %v, want domain.ErrLastOwner", err)
	}
	if _, ok := f.members.byID["membership-andreas"]; !ok {
		t.Fatal("membership-andreas was deleted, want it left in place")
	}
}

func TestRemoveSucceedingDeletesTheMembershipAndRevokesThatUsersSessions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	seedLiveSession(t, f, f.ethanID)
	seedLiveSession(t, f, f.andreasID)

	if err := f.memberSvc.Remove(ctx, f.householdID, "membership-ethan"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, ok := f.members.byID["membership-ethan"]; ok {
		t.Fatal("membership-ethan still exists, want it deleted")
	}
	if got := f.sessions.liveForUser(f.ethanID); got != 0 {
		t.Fatalf("live sessions for Ethan = %d, want 0", got)
	}
	if got := f.sessions.liveForUser(f.andreasID); got != 1 {
		t.Fatalf("live sessions for Andreas = %d, want 1 (untouched)", got)
	}
}

func TestRemoveOnAMembershipIDThatDoesNotExistInTheHouseholdReturnsErrNotFound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.memberSvc.Remove(ctx, f.householdID, "no-such-membership"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want domain.ErrNotFound", err)
	}
}

func TestListReturnsEveryMemberOfTheHousehold(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	views, err := f.memberSvc.List(ctx, f.householdID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("members = %d, want 2", len(views))
	}
	seen := map[string]usecase.MemberView{}
	for _, v := range views {
		seen[v.Membership.ID] = v
	}
	if seen["membership-andreas"].User.DisplayName != "Andreas" {
		t.Fatalf("membership-andreas's user = %+v, want Andreas", seen["membership-andreas"].User)
	}
	if seen["membership-ethan"].User.DisplayName != "Ethan" {
		t.Fatalf("membership-ethan's user = %+v, want Ethan", seen["membership-ethan"].User)
	}
}
