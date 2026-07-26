package domain

type Visibility string

const (
	VisibilityEveryone    Visibility = "everyone"
	VisibilityParentsOnly Visibility = "parents_only"
	VisibilityCustom      Visibility = "custom"
)

// Space is a sidebar grouping. The sidebar is rendered from these rows rather
// than from code, which is what lets "+ New space" extend the navigation.
type Space struct {
	ID                 string
	HouseholdID        string
	Key                string
	Name               string
	Visibility         Visibility
	Position           int
	IsBuiltin          bool
	RequiredCapability Capability // empty means no capability is required
}

// BuiltinSpaces is the set every household starts with, taken from the design's
// Settings screen: Money and Marriage are parents-only, Family is for everyone.
//
// ID is left empty: identifiers are assigned by the database when a household
// is seeded (Task 9's schema, Task 17's seed), not by this constructor.
func BuiltinSpaces(householdID string) []Space {
	return []Space{
		{HouseholdID: householdID, Key: "money", Name: "Money",
			Visibility: VisibilityParentsOnly, Position: 1, IsBuiltin: true, RequiredCapability: CapMoney},
		{HouseholdID: householdID, Key: "marriage", Name: "Marriage",
			Visibility: VisibilityParentsOnly, Position: 2, IsBuiltin: true, RequiredCapability: CapMarriage},
		{HouseholdID: householdID, Key: "family", Name: "Family",
			Visibility: VisibilityEveryone, Position: 3, IsBuiltin: true, RequiredCapability: CapCalendar},
	}
}

// VisibleSpaces filters spaces for one membership. Visibility is checked before
// capability, so a parents-only space stays hidden from a limited member even
// if their capability set would otherwise allow it.
func VisibleSpaces(all []Space, m Membership) []Space {
	visible := make([]Space, 0, len(all))
	for _, s := range all {
		if s.Visibility == VisibilityParentsOnly && m.Role != RoleOwner {
			continue
		}
		if s.RequiredCapability != "" && !m.Capabilities.Has(s.RequiredCapability) {
			continue
		}
		visible = append(visible, s)
	}
	return visible
}
