package usecase

import (
	"context"
	"errors"
	"strings"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// ErrSpaceVisibilityNotSupported is CreateSpace's rejection of any visibility
// other than "everyone" or "parents_only". It is a usecase sentinel because
// this is a feature-completeness gate, not a domain rule: "custom" is a
// valid domain.Visibility (VisibleSpaces treats it as owner-only), so the
// domain cannot reject it; there is no per-space member list yet to back
// custom pages. Accepting the value here would silently create a space only
// an owner could ever see, with no way to change that.
var ErrSpaceVisibilityNotSupported = errors.New("space visibility must be \"everyone\" or \"parents_only\"; custom spaces are not supported yet")

// ErrSpaceNameTaken is CreateSpace's rejection of a name that collides, once
// lowercased and hyphenated, with an existing space's key, including a
// builtin one -- the same UNIQUE (household_id, key) constraint the database
// enforces (migrations/00002_identity.sql). The list-then-compare pre-check
// isn't transactional, so two concurrent creates can both pass it; the
// database constraint is the real backstop, and CreateSpace maps its
// translated domain.ErrAlreadyExists onto this same sentinel so the caller
// sees one error either way.
var ErrSpaceNameTaken = errors.New("a space with that name already exists in this household")

// ErrSpaceNameRequired is CreateSpace's rejection of a name that is empty
// once trimmed. Without it, a blank name derives the empty key, and a second
// blank name would then fail as the more confusing ErrSpaceNameTaken.
var ErrSpaceNameRequired = errors.New("space name is required")

// ErrInvalidFXRateMode is Update's rejection of any fxRateMode value other
// than "auto" or "manual" -- see Update's doc comment for why this exists at
// the usecase level as well as in the database's own CHECK constraint.
var ErrInvalidFXRateMode = errors.New(`fxRateMode must be "auto" or "manual"`)

// HouseholdDeps mirrors AuthDeps/InviteDeps: every port HouseholdService
// needs, gathered into one struct so NewHouseholdService has a single, named
// argument.
type HouseholdDeps struct {
	Households    HouseholdRepository
	Spaces        SpaceRepository
	Notifications NotificationRepository
	// Holdings is consulted for one question only -- does this household hold
	// anything -- and only when the primary currency is being changed. See
	// Update.
	Holdings HoldingCounter
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

// Update persists every field on h, normalising and validating
// PrimaryCurrency and SecondaryCurrency through normalizeCurrency
// (domain.ParseCurrency) -- the same rule Money enforces on the monetary
// path.
//
// Both currency fields are validated and persisted, not just the primary:
// silently dropping one was a defect, and a bad secondary code would
// otherwise surface later as a missing rate rather than at write time. The
// error is wrapped in domain.ErrInvalidMoney so a caller can test it with
// errors.Is.
//
// FXRateMode gets the same treatment for the same reason: the database's own
// CHECK (fx_rate_mode IN ('auto', 'manual')) would otherwise turn a bad value
// into an unmapped 500.
//
// Timezone must be a zone domain.ParseTimezone can load. Changing it rewrites
// no stored date: it only changes which day counts as "today" from the next
// request on.
func (s *HouseholdService) Update(ctx context.Context, h domain.Household) (domain.Household, error) {
	primary, err := normalizeCurrency(h.PrimaryCurrency)
	if err != nil {
		return domain.Household{}, err
	}
	if err := s.refusePrimaryCurrencyChangeWhileHolding(ctx, h.ID, primary); err != nil {
		return domain.Household{}, err
	}
	secondary, err := normalizeCurrency(h.SecondaryCurrency)
	if err != nil {
		return domain.Household{}, err
	}
	switch h.FXRateMode {
	case "auto", "manual":
	default:
		return domain.Household{}, ErrInvalidFXRateMode
	}
	if _, err := domain.ParseTimezone(h.Timezone); err != nil {
		return domain.Household{}, err
	}
	h.PrimaryCurrency = primary
	h.SecondaryCurrency = secondary
	return s.d.Households.Update(ctx, h)
}

// normalizeCurrency validates a currency code through domain.ParseCurrency,
// shared by both of Update's currency fields so the checks cannot drift. Its
// error is returned as-is -- ParseCurrency already wraps
// domain.ErrInvalidMoney, which maps to 422 INVALID_CURRENCY.
func normalizeCurrency(currency string) (string, error) {
	return domain.ParseCurrency(currency)
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

// CreateSpace adds a custom space to the household's sidebar: it validates
// visibility (ErrSpaceVisibilityNotSupported), rejects a blank name
// (ErrSpaceNameRequired), derives the key by trimming, lowercasing and
// hyphenating the name, and rejects a collision (ErrSpaceNameTaken) before
// writing. The new space is never builtin and needs no capability.
//
// The stored Name is the same trimmed value the key comes from, since there
// is no rename endpoint to fix stray whitespace later.
//
// See ErrSpaceNameTaken's doc comment for how the database closes the race
// this pre-check alone cannot.
func (s *HouseholdService) CreateSpace(ctx context.Context, householdID, name string, visibility domain.Visibility) (domain.Space, error) {
	switch visibility {
	case domain.VisibilityEveryone, domain.VisibilityParentsOnly:
	default:
		return domain.Space{}, ErrSpaceVisibilityNotSupported
	}

	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
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
		Name:        trimmedName,
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

// refusePrimaryCurrencyChangeWhileHolding is the one rule the portfolio adds
// to this screen: a holding event or valuation stores its cost in whatever
// currency was primary when it was written, and there is no rate to
// re-express it under a new one. Changing the primary currency once a
// household holds anything would strand the portfolio -- the fold would
// refuse every holding, and the only screen that could fix it is the one now
// broken. Refusing at the edit costs nothing before a household holds
// anything, which is when a currency actually gets chosen.
func (s *HouseholdService) refusePrimaryCurrencyChangeWhileHolding(ctx context.Context, householdID, primary string) error {
	current, err := s.d.Households.Get(ctx, householdID)
	if err != nil {
		return err
	}
	if current.PrimaryCurrency == primary {
		return nil
	}
	held, err := s.d.Holdings.CountForHousehold(ctx, householdID)
	if err != nil {
		return err
	}
	if held > 0 {
		return domain.ErrPrimaryCurrencyHeldByHoldings
	}
	return nil
}
