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
// Settings screen. Marriage is structurally locked to parents (VisibilityParentsOnly;
// the domain also forbids a limited member from ever holding CapMarriage). Money is
// visible to everyone but gated on CapMoney, matching the invite modal's "Money &
// balances" toggle ("Off for kids by default") — a child can be granted access, and
// gating on visibility instead of capability would make that toggle meaningless,
// since visibility is checked before capability. Family is unconditional: the design
// labels its audience "Everyone" with no qualifier, so it carries no required
// capability; gating the Calendar page's content is a later slice, not this one.
//
// ID is left empty: identifiers are assigned by the database when a household
// is seeded (Task 9's schema, Task 17's seed), not by this constructor.
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

// VisibleSpaces filters spaces for one membership. Visibility is checked before
// capability, so a parents-only space stays hidden from a limited member even
// if their capability set would otherwise allow it. An unrecognised Visibility
// value is treated as owner-only rather than as everyone -- see the default
// case below.
//
// The result preserves the input order; VisibleSpaces does not sort. Callers
// must supply all already ordered by Position — Task 9's query does this with
// an ORDER BY, and Task 19's sidebar relies on the order coming out as given.
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
			// Provisional: per-space member lists do not exist yet, and the
			// design marks custom space pages "not built". A visibility mode
			// whose membership model is unbuilt must fail closed rather than
			// default to maximum exposure, so custom spaces are owner-only
			// until per-space membership is implemented.
			if m.Role != RoleOwner {
				continue
			}
		default:
			// An unrecognised Visibility is a data or version problem, not a
			// choice anyone made -- the same situation validateCapabilitiesForRole
			// faces with an unknown Role rebuilt from a Postgres column (see
			// ErrUnknownRole in identity.go). VisibleSpaces has no error return
			// to report that, so it fails closed instead: the safe reading of
			// "I do not know who may see this" is "not everyone", so an unknown
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
