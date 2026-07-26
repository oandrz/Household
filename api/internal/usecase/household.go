package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// ErrSpaceVisibilityNotSupported is CreateSpace's rejection of any visibility
// other than "everyone" or "parents_only". It is a usecase sentinel, not a
// domain one, for the same reason ErrPasswordTooShort lives in invite.go
// rather than internal/domain: this is not a rule about who may act, but a
// feature-completeness gate -- "custom" is a real domain.Visibility value,
// valid enough for domain.VisibleSpaces to filter on (it currently treats it
// as owner-only, see space.go), but the design marks custom space pages "not
// built" and there is no per-space member list yet to back it. Accepting the
// value here would silently create a space no one but an owner could ever
// see, with no way to change that.
var ErrSpaceVisibilityNotSupported = errors.New("space visibility must be \"everyone\" or \"parents_only\"; custom spaces are not supported yet")

// ErrSpaceNameTaken is CreateSpace's rejection of a name that collides, once
// lowercased and hyphenated, with an existing space's key -- including a
// builtin one. The database enforces the identical constraint
// (UNIQUE (household_id, key), migrations/00002_identity.sql), so this check
// exists to fail with a clear, typed error before a write is even attempted,
// not to duplicate a rule the schema wouldn't otherwise enforce.
var ErrSpaceNameTaken = errors.New("a space with that name already exists in this household")

// HouseholdDeps mirrors AuthDeps/InviteDeps: every port HouseholdService
// needs, gathered into one struct so NewHouseholdService has a single, named
// argument.
type HouseholdDeps struct {
	Households    HouseholdRepository
	Spaces        SpaceRepository
	Notifications NotificationRepository
}

// HouseholdService covers the household settings screen: the household
// record itself, its spaces, and its notification preferences.
type HouseholdService struct {
	d HouseholdDeps
}

func NewHouseholdService(d HouseholdDeps) *HouseholdService {
	return &HouseholdService{d: d}
}

func (s *HouseholdService) Get(ctx context.Context, householdID string) (domain.Household, error) {
	return s.d.Households.Get(ctx, householdID)
}

// Update persists every field on h, after normalising PrimaryCurrency to
// uppercase and validating it through domain.NewMoney's existing currency
// check -- the same three-letters, uppercase-only rule Money already
// enforces on the monetary path, rather than a second, independently
// invented check that could drift from it. domain.NewMoney's own error is
// wrapped in domain.ErrInvalidMoney (the sentinel Money.Add already uses for
// the same family of problem) rather than returned bare, so a caller -- and
// eventually Task 16's HTTP layer -- can test for it with errors.Is instead
// of matching an fmt.Errorf string.
//
// SecondaryCurrency is passed through unchanged: the brief's enumerated
// behaviour only calls for normalising the primary currency, and
// ShowSecondaryCurrency == false legitimately pairs with an empty
// SecondaryCurrency, which domain.NewMoney would reject outright.
func (s *HouseholdService) Update(ctx context.Context, h domain.Household) (domain.Household, error) {
	currency := strings.ToUpper(h.PrimaryCurrency)
	if _, err := domain.NewMoney(0, currency); err != nil {
		return domain.Household{}, fmt.Errorf("%w: %v", domain.ErrInvalidMoney, err)
	}
	h.PrimaryCurrency = currency
	return s.d.Households.Update(ctx, h)
}

// Spaces lists the spaces visible to one membership, via
// domain.VisibleSpaces. The result preserves SpaceRepository.List's own
// order (by position) -- VisibleSpaces does not sort, and neither does this.
func (s *HouseholdService) Spaces(ctx context.Context, householdID string, m domain.Membership) ([]domain.Space, error) {
	all, err := s.d.Spaces.List(ctx, householdID)
	if err != nil {
		return nil, err
	}
	return domain.VisibleSpaces(all, m), nil
}

// CreateSpace adds a custom space to the household's sidebar. It accepts only
// domain.VisibilityEveryone and domain.VisibilityParentsOnly -- see
// ErrSpaceVisibilityNotSupported -- and derives the space's key by trimming,
// lowercasing and hyphenating the name, rejecting a collision against any
// existing space (builtin or custom) with ErrSpaceNameTaken before writing
// anything. The new space is never builtin and carries no required
// capability: only the three seeded spaces gate on one.
func (s *HouseholdService) CreateSpace(ctx context.Context, householdID, name string, visibility domain.Visibility) (domain.Space, error) {
	switch visibility {
	case domain.VisibilityEveryone, domain.VisibilityParentsOnly:
	default:
		return domain.Space{}, ErrSpaceVisibilityNotSupported
	}

	key := spaceKey(name)

	existing, err := s.d.Spaces.List(ctx, householdID)
	if err != nil {
		return domain.Space{}, err
	}
	for _, sp := range existing {
		if sp.Key == key {
			return domain.Space{}, ErrSpaceNameTaken
		}
	}

	position, err := s.d.Spaces.NextPosition(ctx, householdID)
	if err != nil {
		return domain.Space{}, err
	}

	return s.d.Spaces.Create(ctx, domain.Space{
		HouseholdID: householdID,
		Key:         key,
		Name:        name,
		Visibility:  visibility,
		Position:    position,
		IsBuiltin:   false,
	})
}

// spaceKey derives a space's key from its name: trimmed, lowercased, spaces
// replaced with hyphens. Trimming first matters -- " Movie Night " must
// collide with "Movie Night" as the same key, not become "-movie-night-".
func spaceKey(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "-")
}

func (s *HouseholdService) Notifications(ctx context.Context, householdID string) (NotificationPreferences, error) {
	return s.d.Notifications.Get(ctx, householdID)
}

func (s *HouseholdService) UpdateNotifications(ctx context.Context, householdID string, p NotificationPreferences) (NotificationPreferences, error) {
	return s.d.Notifications.Upsert(ctx, householdID, p)
}
