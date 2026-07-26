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
// exists to fail with a clear, typed error in the common case, before a write
// is even attempted. It is not the only gate: CreateSpace's pre-check is a
// plain list-then-compare, not a transaction, so two concurrent creates that
// derive the same key can both pass it before either insert lands. The
// database's constraint is the backstop for that race, and the postgres
// adapter's translate function reports the identical violation as
// domain.ErrAlreadyExists; CreateSpace maps that onto this same sentinel
// below, so a caller sees one error regardless of which gate caught it.
var ErrSpaceNameTaken = errors.New("a space with that name already exists in this household")

// ErrSpaceNameRequired is CreateSpace's rejection of a name that is empty
// once trimmed. Without this check, a blank name would derive the empty key
// "" and create a nameless space; a second blank name would then collide
// with it and report ErrSpaceNameTaken, a confusing way to say "a name is
// required" for what is really a missing-input problem, not a naming
// collision.
var ErrSpaceNameRequired = errors.New("space name is required")

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

// Update persists every field on h, after normalising both PrimaryCurrency
// and SecondaryCurrency to uppercase and validating each through
// domain.NewMoney's existing currency check -- the same three-letters,
// uppercase-only rule Money already enforces on the monetary path, rather
// than a second, independently invented check that could drift from it. Both
// fields get identical treatment, not just the primary: both are persisted
// (Task 11 widened UpdateHousehold specifically because silently dropping a
// field was a defect), and both feed FXRateProvider.Rate(from, to) on the
// conversion path -- a malformed secondary code would not fail here at write
// time, only later as a missing rate, far from the edit that caused it.
// domain.NewMoney's own error is wrapped in domain.ErrInvalidMoney (the
// sentinel Money.Add already uses for the same family of problem) rather
// than returned bare, so a caller -- and eventually Task 16's HTTP layer --
// can test for it with errors.Is instead of matching an fmt.Errorf string.
func (s *HouseholdService) Update(ctx context.Context, h domain.Household) (domain.Household, error) {
	primary, err := normalizeCurrency(h.PrimaryCurrency)
	if err != nil {
		return domain.Household{}, err
	}
	secondary, err := normalizeCurrency(h.SecondaryCurrency)
	if err != nil {
		return domain.Household{}, err
	}
	h.PrimaryCurrency = primary
	h.SecondaryCurrency = secondary
	return s.d.Households.Update(ctx, h)
}

// normalizeCurrency uppercases a currency code and validates it through
// domain.NewMoney -- the single reference for what a valid currency code
// looks like, shared by both of Update's currency fields so the two checks
// cannot drift apart.
func normalizeCurrency(currency string) (string, error) {
	upper := strings.ToUpper(currency)
	if _, err := domain.NewMoney(0, upper); err != nil {
		return "", fmt.Errorf("%w: %v", domain.ErrInvalidMoney, err)
	}
	return upper, nil
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
// ErrSpaceVisibilityNotSupported -- rejects a name that is blank once
// trimmed (ErrSpaceNameRequired), and derives the space's key by trimming,
// lowercasing and hyphenating the name, rejecting a collision against any
// existing space (builtin or custom) with ErrSpaceNameTaken before writing
// anything. The new space is never builtin and carries no required
// capability: only the three seeded spaces gate on one.
//
// The list-then-compare duplicate check above is not transactional, so it is
// not the only gate against a collision: Create's own domain.ErrAlreadyExists
// (the database's UNIQUE (household_id, key) constraint, translated -- see
// ErrSpaceNameTaken's doc comment) is mapped onto the identical
// ErrSpaceNameTaken sentinel, so the race between two concurrent creates
// deriving the same key is closed at the database and reported identically
// to the caller, whichever gate actually caught it.
func (s *HouseholdService) CreateSpace(ctx context.Context, householdID, name string, visibility domain.Visibility) (domain.Space, error) {
	switch visibility {
	case domain.VisibilityEveryone, domain.VisibilityParentsOnly:
	default:
		return domain.Space{}, ErrSpaceVisibilityNotSupported
	}

	if strings.TrimSpace(name) == "" {
		return domain.Space{}, ErrSpaceNameRequired
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

	created, err := s.d.Spaces.Create(ctx, domain.Space{
		HouseholdID: householdID,
		Key:         key,
		Name:        name,
		Visibility:  visibility,
		Position:    position,
		IsBuiltin:   false,
	})
	if err != nil {
		if errors.Is(err, domain.ErrAlreadyExists) {
			return domain.Space{}, ErrSpaceNameTaken
		}
		return domain.Space{}, err
	}
	return created, nil
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
