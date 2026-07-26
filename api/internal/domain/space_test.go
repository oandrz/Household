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
	if keys["money"] != domain.VisibilityEveryone {
		t.Fatal("money must be visible to everyone (gated by capability, not visibility)")
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

func TestALimitedMemberGrantedMoneyCapabilitySeesMoney(t *testing.T) {
	all := domain.BuiltinSpaces("h1")
	member, err := domain.NewMembership("m4", "h1", "u4", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar, domain.CapMoney})
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}

	visible := domain.VisibleSpaces(all, member)

	found := false
	for _, s := range visible {
		if s.Key == "money" {
			found = true
		}
	}
	if !found {
		t.Fatalf("visible = %+v, want money present: a limited member holding CapMoney must see it", visible)
	}
}

func TestALimitedMemberWithoutMoneyCapabilityDoesNotSeeMoney(t *testing.T) {
	all := domain.BuiltinSpaces("h1")
	member, err := domain.NewMembership("m5", "h1", "u5", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar})
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}

	visible := domain.VisibleSpaces(all, member)

	for _, s := range visible {
		if s.Key == "money" {
			t.Fatalf("visible = %+v, want money absent: a limited member without CapMoney must not see it", visible)
		}
	}
}

func TestALimitedMemberWithOnlyChoresStillSeesFamily(t *testing.T) {
	all := domain.BuiltinSpaces("h1")
	member, err := domain.NewMembership("m6", "h1", "u6", domain.RoleLimited,
		domain.Capabilities{domain.CapChores})
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}

	visible := domain.VisibleSpaces(all, member)

	found := false
	for _, s := range visible {
		if s.Key == "family" {
			found = true
		}
	}
	if !found {
		t.Fatalf("visible = %+v, want family present: Family is unconditional (\"Everyone\", no qualifier)", visible)
	}
}

func TestALimitedMemberDoesNotSeeACustomSpace(t *testing.T) {
	all := []domain.Space{{
		ID: "s2", HouseholdID: "h1", Key: "book-club", Name: "Book Club",
		Visibility: domain.VisibilityCustom,
	}}
	member, err := domain.NewMembership("m7", "h1", "u7", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar, domain.CapChores, domain.CapMoney})
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}

	if got := domain.VisibleSpaces(all, member); len(got) != 0 {
		t.Fatalf("visible = %+v, want none: custom spaces are provisionally owner-only", got)
	}
}

func TestAnOwnerSeesACustomSpace(t *testing.T) {
	all := []domain.Space{{
		ID: "s2", HouseholdID: "h1", Key: "book-club", Name: "Book Club",
		Visibility: domain.VisibilityCustom,
	}}
	andreas, err := domain.NewMembership("m8", "h1", "u8", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}

	if got := domain.VisibleSpaces(all, andreas); len(got) != 1 {
		t.Fatalf("visible = %+v, want the custom space present for an owner", got)
	}
}

// TestAnUnrecognisedVisibilityFailsClosed pins the default case in
// VisibleSpaces's switch. Without it, a Visibility value that is not
// "everyone", "parents_only" or "custom" -- e.g. a future value, a bad
// migration, or corrupt data read back from Postgres -- would fall through
// with no restriction and be treated as visible to everyone, subject only to
// the capability check. That is the fail-open direction this package
// rejects elsewhere (validateCapabilitiesForRole's ErrUnknownRole for an
// unrecognised Role in identity.go). This test exists so the default case
// is not deleted as unreachable dead code.
func TestAnUnrecognisedVisibilityFailsClosed(t *testing.T) {
	all := []domain.Space{{
		ID: "s3", HouseholdID: "h1", Key: "grandma", Name: "Grandma's Space",
		Visibility: domain.Visibility("shared-with-grandma"),
	}}

	limited, err := domain.NewMembership("m9", "h1", "u9", domain.RoleLimited, domain.Capabilities{domain.CapCalendar})
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}
	if got := domain.VisibleSpaces(all, limited); len(got) != 0 {
		t.Fatalf("visible = %+v, want none: an unrecognised visibility must fail closed", got)
	}

	andreas, err := domain.NewMembership("m10", "h1", "u10", domain.RoleOwner, domain.AllCapabilities())
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}
	if got := domain.VisibleSpaces(all, andreas); len(got) != 1 {
		t.Fatalf("visible = %+v, want the space present for an owner", got)
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
