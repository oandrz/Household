package domain_test

import (
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func owner(id string) domain.Membership {
	m, _ := domain.NewMembership(id, "h1", "u-"+id, domain.RoleOwner,
		domain.Capabilities{domain.CapCalendar, domain.CapChores, domain.CapMoney, domain.CapMarriage})
	return m
}

func kid(id string, caps domain.Capabilities) domain.Membership {
	m, _ := domain.NewMembership(id, "h1", "u-"+id, domain.RoleLimited, caps)
	return m
}

func TestLimitedMemberCannotHoldMarriage(t *testing.T) {
	_, err := domain.NewMembership("m1", "h1", "u1", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar, domain.CapMarriage})

	if !errors.Is(err, domain.ErrLimitedCannotHoldMarriage) {
		t.Fatalf("err = %v, want ErrLimitedCannotHoldMarriage", err)
	}
}

func TestLimitedMemberMayHoldCalendarAndChores(t *testing.T) {
	if _, err := domain.NewMembership("m1", "h1", "u1", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar, domain.CapChores}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemovingTheLastOwnerIsRejected(t *testing.T) {
	all := []domain.Membership{owner("m1"), kid("m2", domain.Capabilities{domain.CapCalendar})}

	if err := domain.ValidateMembershipRemoval(all, "m1"); !errors.Is(err, domain.ErrLastOwner) {
		t.Fatalf("err = %v, want ErrLastOwner", err)
	}
}

func TestRemovingAnOwnerWhenAnotherRemainsIsAllowed(t *testing.T) {
	all := []domain.Membership{owner("m1"), owner("m2")}

	if err := domain.ValidateMembershipRemoval(all, "m1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDemotingTheLastOwnerIsRejected(t *testing.T) {
	all := []domain.Membership{owner("m1"), kid("m2", domain.Capabilities{domain.CapCalendar})}

	err := domain.ValidateMembershipChange(all, "m1", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar})

	if !errors.Is(err, domain.ErrLastOwner) {
		t.Fatalf("err = %v, want ErrLastOwner", err)
	}
}

func TestChangingAMemberToAnInvalidCapabilitySetIsRejected(t *testing.T) {
	all := []domain.Membership{owner("m1"), kid("m2", domain.Capabilities{domain.CapCalendar})}

	err := domain.ValidateMembershipChange(all, "m2", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar, domain.CapMarriage})

	if !errors.Is(err, domain.ErrLimitedCannotHoldMarriage) {
		t.Fatalf("err = %v, want ErrLimitedCannotHoldMarriage", err)
	}
}

func TestParseCapabilitiesRejectsUnknownValues(t *testing.T) {
	if _, err := domain.ParseCapabilities([]string{"calendar", "spaceships"}); !errors.Is(err, domain.ErrUnknownCapability) {
		t.Fatalf("err = %v, want ErrUnknownCapability", err)
	}
}

func TestParseCapabilitiesDeduplicates(t *testing.T) {
	caps, err := domain.ParseCapabilities([]string{"calendar", "calendar", "money"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(caps) != 2 {
		t.Fatalf("len = %d, want 2", len(caps))
	}
}

func TestHasReportsMembership(t *testing.T) {
	caps := domain.Capabilities{domain.CapCalendar, domain.CapChores}
	if !caps.Has(domain.CapChores) {
		t.Fatal("expected chores")
	}
	if caps.Has(domain.CapMarriage) {
		t.Fatal("did not expect marriage")
	}
}
