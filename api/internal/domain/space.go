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

// BuiltinSpaces is the set every household starts with, from the design's
// Settings screen. Marriage is locked to parents (VisibilityParentsOnly; the
// domain also forbids a limited member from holding CapMarriage). Money is
// visible to everyone but gated on CapMoney, so the invite modal's "Money &
// balances" toggle can grant it to a child -- gating on visibility would
// make that toggle meaningless, since visibility is checked first. Family
// carries no required capability: the design labels its audience "Everyone".
//
// ID is left empty: the spaces table's gen_random_uuid() default assigns it
// when sign-up or the seed inserts them, not this constructor.
func BuiltinSpaces(householdID string) []Space {
	return []Space{
		{HouseholdID: householdID, Key: "money", Name: "Money",
			Visibility: VisibilityEveryone, Position: 1, IsBuiltin: true, RequiredCapability: CapMoney},
		{HouseholdID: householdID, Key: "marriage", Name: "Marriage",
			Visibility: VisibilityParentsOnly, Position: 2, IsBuiltin: true, RequiredCapability: CapMarriage},
		{HouseholdID: householdID, Key: "family", Name: "Family",
			Visibility: VisibilityEveryone, Position: 3, IsBuiltin: true},
	}
}

// VisibleSpaces filters spaces for one membership. Visibility is checked
// before capability, so a parents-only space stays hidden from a limited
// member even if their capabilities would allow it. An unrecognised
// Visibility is owner-only, not everyone -- see the default case below.
//
// The result preserves the input order; VisibleSpaces does not sort. Callers
// must supply all already ordered by Position -- SpaceRepo.List's query does
// this with an ORDER BY, and the frontend sidebar (Sidebar.tsx) relies on the
// order coming out as given.
func VisibleSpaces(all []Space, m Membership) []Space {
	visible := make([]Space, 0, len(all))
	for _, s := range all {
		switch s.Visibility {
		case VisibilityEveryone:
			// No visibility restriction; the capability check below still applies.
		case VisibilityParentsOnly:
			if m.Role != RoleOwner {
				continue
			}
		case VisibilityCustom:
			// Provisional: per-space member lists don't exist yet, and the
			// design marks custom space pages "not built". An unbuilt
			// membership model must fail closed, not default to maximum
			// exposure -- so custom spaces are owner-only until it exists.
			if m.Role != RoleOwner {
				continue
			}
		default:
			// An unrecognised Visibility is a data or version problem, not a
			// choice -- the same situation validateCapabilitiesForRole faces
			// with an unknown Role (see ErrUnknownRole). With no error return
			// to report that, VisibleSpaces fails closed instead: "I don't
			// know who may see this" reads as "not everyone", so an unknown
			// value is owner-only, like VisibilityCustom.
			if m.Role != RoleOwner {
				continue
			}
		}
		if s.RequiredCapability != "" && !m.Capabilities.Has(s.RequiredCapability) {
			continue
		}
		visible = append(visible, s)
	}
	return visible
}
