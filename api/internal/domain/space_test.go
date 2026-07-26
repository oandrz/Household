package domain_test

import (
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestBuiltinSpacesMatchTheDesign(t *testing.T) {
	spaces := domain.BuiltinSpaces("h1")

	if len(spaces) != 3 {
		t.Fatalf("len = %d, want 3 (Money, Marriage, Family)", len(spaces))
	}
	keys := map[string]domain.Visibility{}
	for _, s := range spaces {
		keys[s.Key] = s.Visibility
		if !s.IsBuiltin {
			t.Fatalf("%s should be builtin", s.Key)
		}
		if s.HouseholdID != "h1" {
			t.Fatalf("%s has household %q", s.Key, s.HouseholdID)
		}
	}
	if keys["marriage"] != domain.VisibilityParentsOnly {
		t.Fatal("marriage must be parents only")
	}
	if keys["family"] != domain.VisibilityEveryone {
		t.Fatal("family must be visible to everyone")
	}
}

func TestAKidSeesOnlyTheSpacesTheirCapabilitiesAllow(t *testing.T) {
	all := domain.BuiltinSpaces("h1")
	ethan, err := domain.NewMembership("m3", "h1", "u3", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar})
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}

	visible := domain.VisibleSpaces(all, ethan)

	if len(visible) != 1 || visible[0].Key != "family" {
		t.Fatalf("visible = %+v, want only family", visible)
	}
}

func TestAnOwnerSeesEverySpace(t *testing.T) {
	all := domain.BuiltinSpaces("h1")
	andreas, err := domain.NewMembership("m1", "h1", "u1", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}

	if got := len(domain.VisibleSpaces(all, andreas)); got != 3 {
		t.Fatalf("visible = %d, want 3", got)
	}
}

func TestParentsOnlySpacesAreHiddenFromLimitedMembersEvenWithTheCapability(t *testing.T) {
	all := []domain.Space{{
		ID: "s1", HouseholdID: "h1", Key: "money", Name: "Money",
		Visibility: domain.VisibilityParentsOnly, RequiredCapability: domain.CapMoney,
	}}
	kid, err := domain.NewMembership("m2", "h1", "u2", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar, domain.CapMoney})
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}

	if got := domain.VisibleSpaces(all, kid); len(got) != 0 {
		t.Fatalf("visible = %+v, want none: parents_only outranks the capability", got)
	}
}

func TestOrderingIsStable(t *testing.T) {
	all := domain.BuiltinSpaces("h1")
	for i := 1; i < len(all); i++ {
		if all[i-1].Position >= all[i].Position {
			t.Fatalf("positions are not ascending: %+v", all)
		}
	}
}
