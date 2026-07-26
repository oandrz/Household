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

// AllCapabilities is what an owner holds.
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

// NewMembership enforces the capability rules at construction, so an invalid
// Membership value cannot exist anywhere in the system.
func NewMembership(id, householdID, userID string, role Role, caps Capabilities) (Membership, error) {
	if err := validateCapabilitiesForRole(role, caps); err != nil {
		return Membership{}, err
	}
	return Membership{
		ID: id, HouseholdID: householdID, UserID: userID, Role: role, Capabilities: caps,
	}, nil
}

func validateCapabilitiesForRole(role Role, caps Capabilities) error {
	if role == RoleLimited && caps.Has(CapMarriage) {
		return ErrLimitedCannotHoldMarriage
	}
	return nil
}

// ValidateMembershipChange checks a proposed role and capability change against
// the whole household, because the last-owner rule is not a property of one
// membership in isolation.
func ValidateMembershipChange(all []Membership, targetID string, newRole Role, newCaps Capabilities) error {
	if err := validateCapabilitiesForRole(newRole, newCaps); err != nil {
		return err
	}
	if newRole == RoleOwner {
		return nil
	}
	return requireAnotherOwner(all, targetID)
}

// ValidateMembershipRemoval refuses to leave a household without an owner.
func ValidateMembershipRemoval(all []Membership, targetID string) error {
	return requireAnotherOwner(all, targetID)
}

func requireAnotherOwner(all []Membership, excludeID string) error {
	for _, m := range all {
		if m.ID != excludeID && m.Role == RoleOwner {
			return nil
		}
	}
	return ErrLastOwner
}
