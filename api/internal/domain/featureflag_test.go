package domain_test

import (
	"errors"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestParseFlagRefusesAnUnknownKey(t *testing.T) {
	if _, err := domain.ParseFlag("signups_open"); err != nil {
		t.Fatalf("ParseFlag(signups_open) = %v, want nil", err)
	}
	_, err := domain.ParseFlag("not_a_flag")
	if !errors.Is(err, domain.ErrUnknownFlag) {
		t.Fatalf("ParseFlag(not_a_flag) = %v, want ErrUnknownFlag", err)
	}
}

func TestResolveFlagsPrefersTheHouseholdOverride(t *testing.T) {
	defs := []domain.FlagDefinition{
		{Flag: domain.FlagFamilyCalendar, Description: "calendar", Default: false},
	}

	cases := []struct {
		name      string
		global    map[domain.Flag]bool
		household map[domain.Flag]bool
		want      bool
	}{
		{"no overrides falls back to the compile-time default", nil, nil, false},
		{"a global override beats the default",
			map[domain.Flag]bool{domain.FlagFamilyCalendar: true}, nil, true},
		{"a household override beats a global one",
			map[domain.Flag]bool{domain.FlagFamilyCalendar: true},
			map[domain.Flag]bool{domain.FlagFamilyCalendar: false}, false},
		{"a household override beats the default with no global",
			nil, map[domain.Flag]bool{domain.FlagFamilyCalendar: true}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.ResolveFlags(defs, tc.global, tc.household)
			if got.Enabled(domain.FlagFamilyCalendar) != tc.want {
				t.Fatalf("Enabled = %v, want %v", got.Enabled(domain.FlagFamilyCalendar), tc.want)
			}
		})
	}
}

// TestResolveFlagsIgnoresAKeyThisBuildDoesNotDefine is the fail-closed case.
// An override row can outlive the const that named it, because `key` has no
// foreign key to anything. Such a row must never enable a route.
func TestResolveFlagsIgnoresAKeyThisBuildDoesNotDefine(t *testing.T) {
	defs := []domain.FlagDefinition{
		{Flag: domain.FlagFamilyCalendar, Description: "calendar", Default: false},
	}
	orphan := domain.Flag("a_flag_that_was_deleted")

	got := domain.ResolveFlags(defs,
		map[domain.Flag]bool{orphan: true},
		map[domain.Flag]bool{orphan: true})

	if got.Enabled(orphan) {
		t.Fatal("an override naming a flag this build does not define enabled it")
	}
	if _, present := got[orphan]; present {
		t.Fatalf("resolved set contains the orphan key: %v", got)
	}
}

// TestFlagSetEnabledIsFalseForAnUndefinedFlag protects every caller: a typo in
// a requireFeature call must close a route, not open one.
func TestFlagSetEnabledIsFalseForAnUndefinedFlag(t *testing.T) {
	set := domain.FlagSet{}
	if set.Enabled(domain.Flag("typo")) {
		t.Fatal("Enabled on an absent flag = true, want false")
	}
}

// TestAllFlagsHasNoDuplicateKeys guards the registry itself: two definitions
// sharing a key would make resolution depend on iteration order.
func TestAllFlagsHasNoDuplicateKeys(t *testing.T) {
	seen := map[domain.Flag]bool{}
	for _, def := range domain.AllFlags() {
		if seen[def.Flag] {
			t.Fatalf("AllFlags contains %q twice", def.Flag)
		}
		seen[def.Flag] = true
		if def.Description == "" {
			t.Fatalf("flag %q has no description; the admin screen renders it", def.Flag)
		}
	}
}

// TestEveryDefinedFlagParses keeps ParseFlag and AllFlags from drifting apart.
// ParseFlag is what guards the write path, so a flag it refuses could be
// listed in the admin UI and then rejected on save.
func TestEveryDefinedFlagParses(t *testing.T) {
	for _, def := range domain.AllFlags() {
		if _, err := domain.ParseFlag(string(def.Flag)); err != nil {
			t.Fatalf("ParseFlag(%q) = %v, want nil", def.Flag, err)
		}
	}
}
