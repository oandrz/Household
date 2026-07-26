package domain

import "fmt"

type Role string

const (
	RoleOwner   Role = "owner"
	RoleLimited Role = "limited"
)

func ParseRole(s string) (Role, error) {
	switch Role(s) {
	case RoleOwner:
		return RoleOwner, nil
	case RoleLimited:
		return RoleLimited, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownRole, s)
	}
}

type Capability string

const (
	CapCalendar Capability = "calendar"
	CapChores   Capability = "chores"
	CapMoney    Capability = "money"
	CapMarriage Capability = "marriage"
)

type Capabilities []Capability

func ParseCapabilities(values []string) (Capabilities, error) {
	seen := make(map[Capability]bool, len(values))
	out := make(Capabilities, 0, len(values))
	for _, v := range values {
		c := Capability(v)
		switch c {
		case CapCalendar, CapChores, CapMoney, CapMarriage:
		default:
			return nil, fmt.Errorf("%w: %q", ErrUnknownCapability, v)
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out, nil
}

func (c Capabilities) Has(want Capability) bool {
	for _, got := range c {
		if got == want {
			return true
		}
	}
	return false
}

func (c Capabilities) Strings() []string {
	out := make([]string, len(c))
	for i, cap := range c {
		out[i] = string(cap)
	}
	return out
}

// AllCapabilities is the capability set every owner must hold. It is
// enforced (not just documented) by validateCapabilitiesForRole.
func AllCapabilities() Capabilities {
	return Capabilities{CapCalendar, CapChores, CapMoney, CapMarriage}
}

type User struct {
	ID            string
	Email         string // empty for members without credentials
	DisplayName   string
	AvatarInitial string
}

type Household struct {
	ID                    string
	Name                  string
	FamilyName            string
	PrimaryCurrency       string
	ShowSecondaryCurrency bool
	SecondaryCurrency     string
	FXRateMode            string // "auto" or "manual"; inert until a live provider exists
}

type Membership struct {
	ID           string
	HouseholdID  string
	UserID       string
	Role         Role
	Capabilities Capabilities
}

// NewMembership is the enforcement point for the capability rules: a limited
// member cannot hold the marriage capability, and an owner must hold every
// capability. It is not the only gate. Membership's fields are exported, so
// any caller in this module -- or Task 9 reading a row back from Postgres --
// can build a Membership struct literal directly and skip this check. The
// database's CHECK constraints on membership are the second gate, and are
// what actually makes an invalid row impossible to persist.
func NewMembership(id, householdID, userID string, role Role, caps Capabilities) (Membership, error) {
	if err := validateCapabilitiesForRole(role, caps); err != nil {
		return Membership{}, err
	}
	return Membership{
		ID: id, HouseholdID: householdID, UserID: userID, Role: role, Capabilities: caps,
	}, nil
}

func validateCapabilitiesForRole(role Role, caps Capabilities) error {
	switch role {
	case RoleLimited:
		if caps.Has(CapMarriage) {
			return ErrLimitedCannotHoldMarriage
		}
	case RoleOwner:
		for _, want := range AllCapabilities() {
			if !caps.Has(want) {
				return ErrOwnerMustHoldAllCapabilities
			}
		}
	}
	return nil
}

// ValidateMembershipChange checks a proposed role and capability change against
// the whole household, because the last-owner rule is not a property of one
// membership in isolation.
//
// The last-owner rule is only consulted when it is actually at stake: the
// target must be found first (an unknown target is ErrNotFound, not silently
// approved), and the rule applies only when the target currently holds
// RoleOwner and the change would take that away. A capability-only edit on a
// member who is already RoleLimited never touches ownership and must not be
// blocked by a household that happens to have no owners at all.
func ValidateMembershipChange(all []Membership, targetID string, newRole Role, newCaps Capabilities) error {
	target, err := findMembership(all, targetID)
	if err != nil {
		return err
	}
	if err := validateCapabilitiesForRole(newRole, newCaps); err != nil {
		return err
	}
	if target.Role != RoleOwner || newRole == RoleOwner {
		return nil
	}
	return requireAnotherOwner(all, targetID)
}

// ValidateMembershipRemoval refuses to leave a household without an owner.
// As with ValidateMembershipChange, the target must exist, and the rule only
// applies when removing it would actually reduce the owner count -- i.e. the
// target currently holds RoleOwner.
func ValidateMembershipRemoval(all []Membership, targetID string) error {
	target, err := findMembership(all, targetID)
	if err != nil {
		return err
	}
	if target.Role != RoleOwner {
		return nil
	}
	return requireAnotherOwner(all, targetID)
}

func findMembership(all []Membership, id string) (Membership, error) {
	for _, m := range all {
		if m.ID == id {
			return m, nil
		}
	}
	return Membership{}, ErrNotFound
}

func requireAnotherOwner(all []Membership, excludeID string) error {
	for _, m := range all {
		if m.ID != excludeID && m.Role == RoleOwner {
			return nil
		}
	}
	return ErrLastOwner
}
