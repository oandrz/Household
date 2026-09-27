package usecase

import (
	"context"
	"errors"
	"sort"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// AdminService is the operator's surface: feature flags, whether a user is
// a platform admin, and the audit log. No actor parameter decides
// *permission* -- only the HTTP layer's requirePlatformAdmin gates that.
// actorUserID here is attribution only (updated_by, audit rows), never a
// permission check.
type AdminService struct{ d AdminDeps }

type AdminDeps struct {
	Admins PlatformAdminRepository
	Flags  FeatureFlagRepository
	Audit  AdminAuditRepository
	Clock  Clock
}

func NewAdminService(d AdminDeps) *AdminService { return &AdminService{d: d} }

// IsPlatformAdmin distinguishes "not an admin" from "the lookup failed": a
// database outage must not read as a clean no, because the caller turns a no
// into a 404 that hides the whole surface.
func (s *AdminService) IsPlatformAdmin(ctx context.Context, userID string) (bool, error) {
	_, err := s.d.Admins.Get(ctx, userID)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, domain.ErrNotFound):
		return false, nil
	default:
		return false, err
	}
}

// FlagsFor resolves every defined flag for one household.
func (s *AdminService) FlagsFor(ctx context.Context, householdID string) (domain.FlagSet, error) {
	global, household, err := s.d.Flags.OverridesFor(ctx, householdID)
	if err != nil {
		return nil, err
	}
	return domain.ResolveFlags(domain.AllFlags(), asFlagMap(global), asFlagMap(household)), nil
}

// GlobalFlags resolves every defined flag for a caller with no household
// (the pre-auth routes), passing a nil household layer -- household
// overrides are meaningless before there is a household.
func (s *AdminService) GlobalFlags(ctx context.Context) (domain.FlagSet, error) {
	global, err := s.d.Flags.GlobalOverrides(ctx)
	if err != nil {
		return nil, err
	}
	return domain.ResolveFlags(domain.AllFlags(), asFlagMap(global), nil), nil
}

// FlagOverview is one row of the admin flags screen.
type FlagOverview struct {
	Key           string
	Description   string
	Default       bool
	GlobalSet     bool // an override row exists globally
	GlobalEnabled bool // its value, meaningless when GlobalSet is false
	Effective     bool // what an install-wide caller gets today
	Overrides     []HouseholdFlagOverride
	// Orphaned marks an override row naming a flag this build does not
	// define. Such a row enables nothing (see domain.ResolveFlags); it is
	// listed only so somebody can delete it.
	Orphaned bool
}

// Overview is the admin screen's model: one row per defined flag, plus one row
// per override key this build does not define. Those orphans are shown rather
// than filtered out -- a row nobody can see is a row nobody deletes.
func (s *AdminService) Overview(ctx context.Context) ([]FlagOverview, error) {
	global, err := s.d.Flags.GlobalOverrides(ctx)
	if err != nil {
		return nil, err
	}
	overrides, err := s.d.Flags.AllHouseholdOverrides(ctx)
	if err != nil {
		return nil, err
	}

	byKey := map[string][]HouseholdFlagOverride{}
	for _, o := range overrides {
		byKey[o.Key] = append(byKey[o.Key], o)
	}

	defined := map[string]bool{}
	out := make([]FlagOverview, 0, len(domain.AllFlags()))
	for _, def := range domain.AllFlags() {
		key := string(def.Flag)
		defined[key] = true
		value, set := global[key]
		effective := def.Default
		if set {
			effective = value
		}
		out = append(out, FlagOverview{
			Key:           key,
			Description:   def.Description,
			Default:       def.Default,
			GlobalSet:     set,
			GlobalEnabled: value,
			Effective:     effective,
			Overrides:     byKey[key],
		})
	}

	// One row per override key this build does not define, from either layer,
	// listed once. Sorted so the screen does not reorder itself between reads:
	// Go's map iteration order is deliberately random.
	orphans := map[string]bool{}
	for key := range global {
		if !defined[key] {
			orphans[key] = true
		}
	}
	for key := range byKey {
		if !defined[key] {
			orphans[key] = true
		}
	}
	keys := make([]string, 0, len(orphans))
	for key := range orphans {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, FlagOverview{Key: key, Orphaned: true, Overrides: byKey[key]})
	}
	return out, nil
}

// SetGlobalFlag writes a global override. It refuses a key domain.ParseFlag
// does not recognise before writing anything -- fail closed on a value this
// build did not construct, per the project's own rule.
func (s *AdminService) SetGlobalFlag(ctx context.Context, key string, enabled bool, actorUserID string) error {
	flag, err := domain.ParseFlag(key)
	if err != nil {
		return err
	}
	return s.d.Flags.SetGlobal(ctx, string(flag), enabled, actorUserID)
}

// SetHouseholdFlag writes one household's override, subject to the same
// refusal as SetGlobalFlag.
func (s *AdminService) SetHouseholdFlag(ctx context.Context, householdID, key string, enabled bool, actorUserID string) error {
	flag, err := domain.ParseFlag(key)
	if err != nil {
		return err
	}
	return s.d.Flags.SetHousehold(ctx, householdID, string(flag), enabled, actorUserID)
}

// ClearHouseholdFlag removes an override rather than setting it false: "no
// opinion" and "explicitly off" are different states the screen shows. It
// skips ParseFlag deliberately -- deleting an orphaned row must work on a
// key this build no longer defines.
func (s *AdminService) ClearHouseholdFlag(ctx context.Context, householdID, key string) error {
	return s.d.Flags.ClearHousehold(ctx, householdID, key)
}

// RecordAudit appends one row to the audit log, stamping At from the
// service's Clock when the caller left it zero -- the common case, since
// the HTTP layer builds an AdminAuditEntry without touching a clock.
func (s *AdminService) RecordAudit(ctx context.Context, entry AdminAuditEntry) error {
	if entry.At.IsZero() {
		entry.At = s.d.Clock.Now()
	}
	return s.d.Audit.Record(ctx, entry)
}

// asFlagMap re-keys a repository's string map for the domain. The repositories
// speak strings because a column can hold anything; the domain speaks Flag
// because it validated.
func asFlagMap(in map[string]bool) map[domain.Flag]bool {
	out := make(map[domain.Flag]bool, len(in))
	for key, enabled := range in {
		out[domain.Flag(key)] = enabled
	}
	return out
}
