package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// adminServiceNow is the fixed instant every AdminService test in this file
// runs its Clock at. Nothing here exercises time passing, so an arbitrary
// fixed date is fine (the convention every other fixture in this package
// follows -- see testdouble_test.go's fixedClock).
var adminServiceNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func TestFlagsForAppliesTheHouseholdOverride(t *testing.T) {
	flags := newFakeFlagRepo()
	flags.global[string(domain.FlagFamilyCalendar)] = true
	flags.household["h1"] = map[string]bool{string(domain.FlagFamilyCalendar): false}

	svc := usecase.NewAdminService(usecase.AdminDeps{
		Admins: newFakeAdminRepo(), Flags: flags,
		Audit: newFakeAuditRepo(), Clock: &fixedClock{now: adminServiceNow},
	})

	set, err := svc.FlagsFor(context.Background(), "h1")
	if err != nil {
		t.Fatalf("FlagsFor: %v", err)
	}
	if set.Enabled(domain.FlagFamilyCalendar) {
		t.Fatal("the household's own override was ignored")
	}

	other, err := svc.FlagsFor(context.Background(), "h2")
	if err != nil {
		t.Fatalf("FlagsFor(h2): %v", err)
	}
	if !other.Enabled(domain.FlagFamilyCalendar) {
		t.Fatal("a household with no override should see the global value")
	}
}

// TestGlobalFlagsIgnoresHouseholdOverrides is the pre-auth path: a caller with
// no household must never be handed some household's opinion.
func TestGlobalFlagsIgnoresHouseholdOverrides(t *testing.T) {
	flags := newFakeFlagRepo()
	flags.household["h1"] = map[string]bool{string(domain.FlagSignupsOpen): false}

	svc := usecase.NewAdminService(usecase.AdminDeps{
		Admins: newFakeAdminRepo(), Flags: flags,
		Audit: newFakeAuditRepo(), Clock: &fixedClock{now: adminServiceNow},
	})

	set, err := svc.GlobalFlags(context.Background())
	if err != nil {
		t.Fatalf("GlobalFlags: %v", err)
	}
	if !set.Enabled(domain.FlagSignupsOpen) {
		t.Fatal("a household override leaked into the global set")
	}
}

func TestSetGlobalFlagRefusesAnUnknownKey(t *testing.T) {
	flags := newFakeFlagRepo()
	svc := usecase.NewAdminService(usecase.AdminDeps{
		Admins: newFakeAdminRepo(), Flags: flags,
		Audit: newFakeAuditRepo(), Clock: &fixedClock{now: adminServiceNow},
	})

	err := svc.SetGlobalFlag(context.Background(), "not_a_flag", true, "actor")
	if !errors.Is(err, domain.ErrUnknownFlag) {
		t.Fatalf("SetGlobalFlag(not_a_flag) = %v, want ErrUnknownFlag", err)
	}
	if len(flags.global) != 0 {
		t.Fatalf("an unknown key was written: %v", flags.global)
	}
}

// TestOverviewMarksOrphanedRows: an override row can outlive the const that
// named it. The screen must show those rather than hide them, or nobody ever
// deletes them.
func TestOverviewMarksOrphanedRows(t *testing.T) {
	flags := newFakeFlagRepo()
	flags.global["a_flag_that_was_deleted"] = true

	svc := usecase.NewAdminService(usecase.AdminDeps{
		Admins: newFakeAdminRepo(), Flags: flags,
		Audit: newFakeAuditRepo(), Clock: &fixedClock{now: adminServiceNow},
	})

	overview, err := svc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	var orphans int
	for _, row := range overview {
		if row.Orphaned {
			orphans++
			if row.Key != "a_flag_that_was_deleted" {
				t.Fatalf("orphan = %q, want the deleted key", row.Key)
			}
		}
	}
	if orphans != 1 {
		t.Fatalf("Overview reported %d orphans, want 1", orphans)
	}
	if len(overview) != len(domain.AllFlags())+1 {
		t.Fatalf("Overview has %d rows, want every defined flag plus the orphan", len(overview))
	}
}

// TestOverviewSortsOrphanedRowsFromBothLayers pins decision 1 from the task
// brief: orphan keys are collected from *both* override layers into one set,
// then sorted -- not appended in whatever order a map ranges over them,
// which Go deliberately randomises. One orphan lives only in the global
// layer, the other only in a household's, and their keys are chosen so that
// map order and sorted order would disagree if the sort were ever dropped.
func TestOverviewSortsOrphanedRowsFromBothLayers(t *testing.T) {
	flags := newFakeFlagRepo()
	flags.global["z_orphan_global"] = true
	flags.household["h1"] = map[string]bool{"a_orphan_household": true}

	svc := usecase.NewAdminService(usecase.AdminDeps{
		Admins: newFakeAdminRepo(), Flags: flags,
		Audit: newFakeAuditRepo(), Clock: &fixedClock{now: adminServiceNow},
	})

	overview, err := svc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	var orphanKeys []string
	for _, row := range overview {
		if row.Orphaned {
			orphanKeys = append(orphanKeys, row.Key)
		}
	}
	want := []string{"a_orphan_household", "z_orphan_global"}
	if len(orphanKeys) != len(want) {
		t.Fatalf("orphan keys = %v, want %v", orphanKeys, want)
	}
	for i, k := range want {
		if orphanKeys[i] != k {
			t.Fatalf("orphan keys = %v, want %v (sorted)", orphanKeys, want)
		}
	}
}

// TestRecentAuditClampsTheLimit pins decision 2 from the task brief:
// AdminAuditRepository.Recent takes whatever limit it is given and passes it
// straight to SQL's LIMIT clause (see fakeAuditRepo's own doc comment), so
// the clamp has to happen in AdminService or nothing bounds it at all.
func TestRecentAuditClampsTheLimit(t *testing.T) {
	audit := newFakeAuditRepo()
	svc := usecase.NewAdminService(usecase.AdminDeps{
		Admins: newFakeAdminRepo(), Flags: newFakeFlagRepo(),
		Audit: audit, Clock: &fixedClock{now: adminServiceNow},
	})

	if _, err := svc.RecentAudit(context.Background(), 10_000); err != nil {
		t.Fatalf("RecentAudit(10000): %v", err)
	}
	if audit.lastLimit != 500 {
		t.Fatalf("RecentAudit(10000) reached the repository as limit=%d, want it capped at 500", audit.lastLimit)
	}

	if _, err := svc.RecentAudit(context.Background(), 0); err != nil {
		t.Fatalf("RecentAudit(0): %v", err)
	}
	if audit.lastLimit != 50 {
		t.Fatalf("RecentAudit(0) reached the repository as limit=%d, want the default of 50", audit.lastLimit)
	}

	if _, err := svc.RecentAudit(context.Background(), -5); err != nil {
		t.Fatalf("RecentAudit(-5): %v", err)
	}
	if audit.lastLimit != 50 {
		t.Fatalf("RecentAudit(-5) reached the repository as limit=%d, want the default of 50", audit.lastLimit)
	}
}
