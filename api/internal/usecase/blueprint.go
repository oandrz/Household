package usecase

import (
	"errors"
	"strings"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

var (
	// ErrHouseholdNameRequired and ErrDisplayNameRequired follow
	// ErrSpaceNameRequired's shape: a usecase sentinel the HTTP layer maps to
	// 422 with a field-specific code.
	ErrHouseholdNameRequired = errors.New("a household name is required")
	ErrDisplayNameRequired   = errors.New("a display name is required")
)

// HouseholdBlueprint is the single definition of what a new household consists
// of. Seed and SignupRepository.Provision both build one and then apply it
// differently -- Seed step-by-step and idempotently, because a partially failed
// seed run must be retryable (see Seed's own doc comment); Provision in one
// transaction, because a partially provisioned household leaves a users row
// occupying users.email's unique index and permanently blocks that address.
//
// Sharing the blueprint rather than one implementation is deliberate: a single
// implementation would have to give up either the step-idempotency or the
// atomicity, and both are load-bearing where they are.
//
// Spaces are not a field. domain.BuiltinSpaces needs the household ID, which
// does not exist until inside Provision's transaction, so the adapter calls it
// there. The knowledge of which spaces a household starts with stays in domain.
type HouseholdBlueprint struct {
	Name                  string
	FamilyName            string
	PrimaryCurrency       string
	SecondaryCurrency     string
	ShowSecondaryCurrency bool
	OwnerDisplayName      string
	OwnerRole             domain.Role
	OwnerCapabilities     domain.Capabilities
	Notifications         NotificationPreferences
}

// DefaultNotificationPreferences is every flag on, which is what the design
// shows a new household with. Seed and Provision both use it so the two cannot
// disagree about what "default" means.
func DefaultNotificationPreferences() NotificationPreferences {
	return NotificationPreferences{
		BillReminders:   true,
		OverspendAlerts: true,
		RetroReminder:   true,
		WeeklyDigest:    true,
	}
}

// NewSignupBlueprint validates and assembles the blueprint for a self-serve
// household. Every rule it applies is here rather than in the handler or the
// repository, so there is one place to read what a new household looks like.
func NewSignupBlueprint(householdName, displayName, currency string) (HouseholdBlueprint, error) {
	name := strings.TrimSpace(householdName)
	if name == "" {
		return HouseholdBlueprint{}, ErrHouseholdNameRequired
	}
	owner := strings.TrimSpace(displayName)
	if owner == "" {
		return HouseholdBlueprint{}, ErrDisplayNameRequired
	}
	// ParseSelectableCurrency, not ParseCurrency: a currency chosen for the
	// first time at sign-up must be one domain.Money can actually render, not
	// merely one ISO 4217 recognises -- see domain.SelectableCurrencies for
	// why, and household.go's normalizeCurrency for the PATCH path, which
	// stays on ParseCurrency because it must keep accepting stored data.
	code, err := domain.ParseSelectableCurrency(currency)
	if err != nil {
		return HouseholdBlueprint{}, err
	}

	return HouseholdBlueprint{
		Name: name,
		// The design's create card asks for one name only ("Household name",
		// helper "Shown at the top of the sidebar"), so family_name mirrors it
		// rather than adding a field the design does not draw. The invite
		// preview then reads "join the Ade & Kris household", which is fine.
		FamilyName:      name,
		PrimaryCurrency: code,
		// Equal to primary, and the toggle off. Not the column's IDR default:
		// CurrencyPanel renders "Show {secondaryCurrency} equivalents" straight
		// from the column, so a household in Sao Paulo would otherwise find a
		// reference to Indonesian rupiah in Settings. Equal-to-primary makes
		// the toggle inert but coherent, and makes the missing
		// secondary-currency picker a visible gap rather than a surprise.
		SecondaryCurrency:     code,
		ShowSecondaryCurrency: false,
		OwnerDisplayName:      owner,
		OwnerRole:             domain.RoleOwner,
		// An owner must hold every capability -- domain.NewMembership enforces
		// it and the memberships CHECK constraint enforces it again. Passing
		// anything else here produces a row Postgres refuses.
		OwnerCapabilities: domain.AllCapabilities(),
		Notifications:     DefaultNotificationPreferences(),
	}, nil
}

// Household renders the blueprint as the domain value HouseholdRepository.Create
// takes. ID and FXRateMode are left zero: the database assigns the ID, and
// fx_rate_mode keeps its column default of 'auto', which the CHECK constraint
// makes the only safe value to assume at creation time.
func (b HouseholdBlueprint) Household() domain.Household {
	return domain.Household{
		Name:                  b.Name,
		FamilyName:            b.FamilyName,
		PrimaryCurrency:       b.PrimaryCurrency,
		ShowSecondaryCurrency: b.ShowSecondaryCurrency,
		SecondaryCurrency:     b.SecondaryCurrency,
	}
}
