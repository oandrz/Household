package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// newRetroActionRepo opens a fresh test database, one household, and both
// repositories an action test needs: RetroActionRepo to exercise, and
// RetroRepo because every action belongs to a retro that must exist first --
// mirroring newRetroRepo's own reasoning for why it hands back a
// *postgres.DB rather than just an id.
func newRetroActionRepo(t *testing.T) (*postgres.RetroActionRepo, *postgres.RetroRepo, string) {
	t.Helper()
	actions, retros, _, householdID := newRetroActionFixture(t)
	return actions, retros, householdID
}

// newRetroActionFixture is newRetroActionRepo plus the *postgres.DB itself,
// for the tests below that also need seedSecondHousehold or a real
// membership row (insertTestMembership, transaction_repo_test.go) neither of
// which newRetroActionRepo's three-value signature -- fixed by the brief's
// own first test -- has room to return.
func newRetroActionFixture(t *testing.T) (*postgres.RetroActionRepo, *postgres.RetroRepo, *postgres.DB, string) {
	t.Helper()
	db := openTestDB(t)
	householdID := insertTestHousehold(t, db)
	return postgres.NewRetroActionRepo(db), postgres.NewRetroRepo(db), db, householdID
}

// The action and its assignees are one write. A bad assignee must leave no
// action behind -- an action nobody can see the owner of is worse than a
// refused insert.
func TestAddActionWithABadAssigneeWritesNothingAtAll(t *testing.T) {
	ctx := context.Background()
	actions, retros, householdID := newRetroActionRepo(t)

	retro, err := retros.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = actions.Add(ctx, usecase.RetroActionInput{
		HouseholdID:           householdID,
		RetroID:               retro.ID,
		Body:                  "phone-free dinners",
		AssigneeMembershipIDs: []string{uuid.NewString()}, // not a membership
	})
	if err == nil {
		t.Fatal("Add accepted an assignee that is not a membership")
	}

	got, err := actions.ForRetro(ctx, householdID, retro.ID)
	if err != nil {
		t.Fatalf("ForRetro: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("%d actions survived the failed insert, want 0", len(got))
	}
}

// Both owners on one action -- the design's "A C" row.
func TestAddActionKeepsBothAssignees(t *testing.T) {
	ctx := context.Background()
	actions, retros, db, householdID := newRetroActionFixture(t)

	retro, err := retros.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	alex := insertTestMembership(t, db, householdID, "Alex")
	casey := insertTestMembership(t, db, householdID, "Casey")

	added, err := actions.Add(ctx, usecase.RetroActionInput{
		HouseholdID:           householdID,
		RetroID:               retro.ID,
		Body:                  "book the getaway",
		AssigneeMembershipIDs: []string{alex, casey},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(added.AssigneeMembershipIDs) != 2 {
		t.Fatalf("Add returned %d assignees, want 2", len(added.AssigneeMembershipIDs))
	}

	got, err := actions.ForRetro(ctx, householdID, retro.ID)
	if err != nil {
		t.Fatalf("ForRetro: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if len(got[0].AssigneeMembershipIDs) != 2 {
		t.Fatalf("assignees = %v, want both %s and %s", got[0].AssigneeMembershipIDs, alex, casey)
	}
	present := map[string]bool{got[0].AssigneeMembershipIDs[0]: true, got[0].AssigneeMembershipIDs[1]: true}
	if !present[alex] || !present[casey] {
		t.Fatalf("assignees = %v, want both %s and %s", got[0].AssigneeMembershipIDs, alex, casey)
	}
}

// Picking the same person twice in the modal is a redundant selection, not
// a state conflict: Add must succeed and store exactly one assignee row,
// not answer 409 for input that costs nothing to accept.
func TestAddActionWithADuplicateAssigneeSucceedsWithOneRow(t *testing.T) {
	ctx := context.Background()
	actions, retros, db, householdID := newRetroActionFixture(t)

	retro, err := retros.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	alex := insertTestMembership(t, db, householdID, "Alex")

	added, err := actions.Add(ctx, usecase.RetroActionInput{
		HouseholdID:           householdID,
		RetroID:               retro.ID,
		Body:                  "picking the same person twice",
		AssigneeMembershipIDs: []string{alex, alex},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(added.AssigneeMembershipIDs) != 1 {
		t.Fatalf("Add returned %d assignees, want 1 (deduped)", len(added.AssigneeMembershipIDs))
	}

	got, err := actions.ForRetro(ctx, householdID, retro.ID)
	if err != nil {
		t.Fatalf("ForRetro: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if len(got[0].AssigneeMembershipIDs) != 1 {
		t.Fatalf("stored assignees = %v, want exactly one row for %s", got[0].AssigneeMembershipIDs, alex)
	}
}

// OpenInMonth is the carry-over offer: that month, unticked only.
func TestOpenInMonthReturnsOnlyThatMonthsUntickedActions(t *testing.T) {
	ctx := context.Background()
	actions, retros, householdID := newRetroActionRepo(t)

	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	juneRetro, err := retros.Create(ctx, householdID, june)
	if err != nil {
		t.Fatalf("create june retro: %v", err)
	}
	julyRetro, err := retros.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("create july retro: %v", err)
	}

	if _, err := actions.Add(ctx, usecase.RetroActionInput{HouseholdID: householdID, RetroID: juneRetro.ID, Body: "june open"}); err != nil {
		t.Fatalf("add june open: %v", err)
	}
	julyDone, err := actions.Add(ctx, usecase.RetroActionInput{HouseholdID: householdID, RetroID: julyRetro.ID, Body: "july done"})
	if err != nil {
		t.Fatalf("add july done: %v", err)
	}
	julyOpen, err := actions.Add(ctx, usecase.RetroActionInput{HouseholdID: householdID, RetroID: julyRetro.ID, Body: "july open"})
	if err != nil {
		t.Fatalf("add july open: %v", err)
	}
	if err := actions.SetDone(ctx, householdID, julyDone.ID, true, time.Now().UTC()); err != nil {
		t.Fatalf("SetDone: %v", err)
	}

	got, err := actions.OpenInMonth(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("OpenInMonth: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (june's open action and july's done action must both be excluded)", len(got))
	}
	if got[0].ID != julyOpen.ID {
		t.Fatalf("got action %q, want the july-open action %q", got[0].ID, julyOpen.ID)
	}
}

// ForRetro's own contract (usecase/ports.go, and 00009_retros.sql's comment
// on why retro_actions carries no position column) is insertion order:
// created_at, id. ListRetroActions computes this with a GROUP BY, whose
// output order Postgres does not guarantee is insertion order on its own --
// the ORDER BY clause is what makes it so, and every other test in this file
// puts at most one action in a retro, so none of them would notice that
// clause going missing. This is the one that would.
func TestForRetroReturnsActionsInInsertionOrder(t *testing.T) {
	ctx := context.Background()
	actions, retros, householdID := newRetroActionRepo(t)

	retro, err := retros.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	first, err := actions.Add(ctx, usecase.RetroActionInput{HouseholdID: householdID, RetroID: retro.ID, Body: "first"})
	if err != nil {
		t.Fatalf("add first: %v", err)
	}
	second, err := actions.Add(ctx, usecase.RetroActionInput{HouseholdID: householdID, RetroID: retro.ID, Body: "second"})
	if err != nil {
		t.Fatalf("add second: %v", err)
	}
	third, err := actions.Add(ctx, usecase.RetroActionInput{HouseholdID: householdID, RetroID: retro.ID, Body: "third"})
	if err != nil {
		t.Fatalf("add third: %v", err)
	}

	got, err := actions.ForRetro(ctx, householdID, retro.ID)
	if err != nil {
		t.Fatalf("ForRetro: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	want := []string{first.ID, second.ID, third.ID}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("got[%d].ID = %q, want %q -- order was first, second, third, not %v", i, got[i].ID, id, got)
		}
	}
}

// Unticking clears the stamp rather than recording a "not done" time.
func TestSetDoneFalseClearsTheTimestamp(t *testing.T) {
	ctx := context.Background()
	actions, retros, householdID := newRetroActionRepo(t)

	retro, err := retros.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	action, err := actions.Add(ctx, usecase.RetroActionInput{HouseholdID: householdID, RetroID: retro.ID, Body: "phone-free dinners"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := actions.SetDone(ctx, householdID, action.ID, true, time.Now().UTC()); err != nil {
		t.Fatalf("SetDone(true): %v", err)
	}
	ticked, err := actions.ForRetro(ctx, householdID, retro.ID)
	if err != nil {
		t.Fatalf("ForRetro after tick: %v", err)
	}
	if ticked[0].DoneAt == nil {
		t.Fatal("DoneAt is nil right after ticking, want a timestamp")
	}

	if err := actions.SetDone(ctx, householdID, action.ID, false, time.Now().UTC()); err != nil {
		t.Fatalf("SetDone(false): %v", err)
	}
	unticked, err := actions.ForRetro(ctx, householdID, retro.ID)
	if err != nil {
		t.Fatalf("ForRetro after untick: %v", err)
	}
	if unticked[0].DoneAt != nil {
		t.Fatalf("DoneAt = %v after unticking, want nil -- unticking must clear the stamp, not record a new one", unticked[0].DoneAt)
	}
}

// SetDone on an action id that does not exist at all is the same zero-row
// contract as the household-scoping tests below, minus the second household.
func TestSetDoneOnAMissingActionIsNotFound(t *testing.T) {
	ctx := context.Background()
	actions, _, householdID := newRetroActionRepo(t)

	err := actions.SetDone(ctx, householdID, uuid.NewString(), true, time.Now().UTC())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// Remove on an action id that does not exist at all must not be reported as
// success -- the SetBillNextDue defect, written as a test before the
// scoping tests below add a second household to the picture.
func TestRemoveOnAMissingActionIsNotFound(t *testing.T) {
	ctx := context.Background()
	actions, _, householdID := newRetroActionRepo(t)

	err := actions.Remove(ctx, householdID, uuid.NewString())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// Another household must not be able to write an action onto this one's
// retro by quoting its retro id. AddRetroAction's WHERE also requires
// household_id, and this is the test that would catch its absence.
func TestAddActionIsScopedToItsHousehold(t *testing.T) {
	ctx := context.Background()
	actions, retros, db, householdID := newRetroActionFixture(t)
	other := seedSecondHousehold(t, db)

	retro, err := retros.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = actions.Add(ctx, usecase.RetroActionInput{
		HouseholdID: other,
		RetroID:     retro.ID,
		Body:        "stolen action",
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound -- another household's retro id must not accept a write", err)
	}

	got, err := actions.ForRetro(ctx, householdID, retro.ID)
	if err != nil {
		t.Fatalf("ForRetro: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("%d actions survived a write claiming the wrong household, want 0", len(got))
	}
}

// A membership that is real, but belongs to a different household, must be
// refused exactly like a membership id that does not exist at all --
// usecase.RetroActionRepository.Add's own doc comment says "a membership of
// this household", not merely "a membership". This is what proves
// AddRetroActionAssignee's household_id clause is doing real work rather
// than the retro_action_assignees.membership_id foreign key alone, which
// would happily accept this id.
func TestAddActionRefusesAnAssigneeFromAnotherHousehold(t *testing.T) {
	ctx := context.Background()
	actions, retros, db, householdID := newRetroActionFixture(t)
	other := seedSecondHousehold(t, db)
	outsider := insertTestMembership(t, db, other, "Outsider")

	retro, err := retros.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = actions.Add(ctx, usecase.RetroActionInput{
		HouseholdID:           householdID,
		RetroID:               retro.ID,
		Body:                  "not this household's owner",
		AssigneeMembershipIDs: []string{outsider},
	})
	if err == nil {
		t.Fatal("Add accepted an assignee that is a membership of a different household")
	}

	got, err := actions.ForRetro(ctx, householdID, retro.ID)
	if err != nil {
		t.Fatalf("ForRetro: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("%d actions survived the cross-household assignee, want 0", len(got))
	}
}

// carried_from gets the identical treatment: retro_actions.carried_from's
// foreign key only proves the id exists SOMEWHERE in retro_actions, not
// that it belongs to this household, so an id naming another household's
// action must be refused exactly like a bad assignee -- the whole write
// failing, no orphan action left behind.
func TestAddActionRefusesACarriedFromFromAnotherHousehold(t *testing.T) {
	ctx := context.Background()
	actions, retros, db, householdID := newRetroActionFixture(t)
	other := seedSecondHousehold(t, db)

	otherRetro, err := retros.Create(ctx, other, jul2026())
	if err != nil {
		t.Fatalf("create in other household: %v", err)
	}
	otherAction, err := actions.Add(ctx, usecase.RetroActionInput{HouseholdID: other, RetroID: otherRetro.ID, Body: "not yours to carry from"})
	if err != nil {
		t.Fatalf("add in other household: %v", err)
	}

	retro, err := retros.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = actions.Add(ctx, usecase.RetroActionInput{
		HouseholdID: householdID,
		RetroID:     retro.ID,
		Body:        "carried from someone else's action",
		CarriedFrom: otherAction.ID,
	})
	if err == nil {
		t.Fatal("Add accepted a carried_from id belonging to a different household")
	}

	got, err := actions.ForRetro(ctx, householdID, retro.ID)
	if err != nil {
		t.Fatalf("ForRetro: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("%d actions survived the cross-household carried_from, want 0", len(got))
	}
}

// The happy path the test above must not have broken: carrying an action
// forward within the SAME household still succeeds and the new row records
// the original action's id.
func TestAddActionCarriedFromWithinTheSameHouseholdSucceeds(t *testing.T) {
	ctx := context.Background()
	actions, retros, householdID := newRetroActionRepo(t)

	julyRetro, err := retros.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("create july retro: %v", err)
	}
	original, err := actions.Add(ctx, usecase.RetroActionInput{HouseholdID: householdID, RetroID: julyRetro.ID, Body: "book the getaway"})
	if err != nil {
		t.Fatalf("add original: %v", err)
	}

	august := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	augustRetro, err := retros.Create(ctx, householdID, august)
	if err != nil {
		t.Fatalf("create august retro: %v", err)
	}
	carried, err := actions.Add(ctx, usecase.RetroActionInput{
		HouseholdID: householdID,
		RetroID:     augustRetro.ID,
		Body:        "book the getaway",
		CarriedFrom: original.ID,
	})
	if err != nil {
		t.Fatalf("add carried: %v", err)
	}
	if carried.CarriedFrom != original.ID {
		t.Fatalf("CarriedFrom = %q, want %q", carried.CarriedFrom, original.ID)
	}
}

// Another household must not be able to tick this one's action.
func TestSetDoneIsScopedToItsHousehold(t *testing.T) {
	ctx := context.Background()
	actions, retros, db, householdID := newRetroActionFixture(t)
	other := seedSecondHousehold(t, db)

	retro, err := retros.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	action, err := actions.Add(ctx, usecase.RetroActionInput{HouseholdID: householdID, RetroID: retro.ID, Body: "not yours to tick"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := actions.SetDone(ctx, other, action.ID, true, time.Now().UTC()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	got, err := actions.ForRetro(ctx, householdID, retro.ID)
	if err != nil {
		t.Fatalf("ForRetro: %v", err)
	}
	if got[0].DoneAt != nil {
		t.Fatalf("DoneAt = %v, want nil -- another household ticked this action", got[0].DoneAt)
	}
}

// Another household must not be able to delete this one's action.
func TestRemoveIsScopedToItsHousehold(t *testing.T) {
	ctx := context.Background()
	actions, retros, db, householdID := newRetroActionFixture(t)
	other := seedSecondHousehold(t, db)

	retro, err := retros.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	action, err := actions.Add(ctx, usecase.RetroActionInput{HouseholdID: householdID, RetroID: retro.ID, Body: "not yours to delete"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := actions.Remove(ctx, other, action.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	got, err := actions.ForRetro(ctx, householdID, retro.ID)
	if err != nil {
		t.Fatalf("ForRetro: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("%d actions remain, want 1 -- another household deleted this one", len(got))
	}
}

// Another household's actions must not leak into ForRetro even given the
// exact retro id -- the same "guessing an id is not proof of ownership"
// property TestRetroUpdateIsScopedToItsHousehold already proves for retros.
func TestForRetroIsScopedToItsHousehold(t *testing.T) {
	ctx := context.Background()
	actions, retros, db, householdID := newRetroActionFixture(t)
	other := seedSecondHousehold(t, db)

	retro, err := retros.Create(ctx, other, jul2026())
	if err != nil {
		t.Fatalf("create in other household: %v", err)
	}
	if _, err := actions.Add(ctx, usecase.RetroActionInput{HouseholdID: other, RetroID: retro.ID, Body: "not yours to read"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := actions.ForRetro(ctx, householdID, retro.ID)
	if err != nil {
		t.Fatalf("ForRetro: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("%d actions leaked from another household, want 0", len(got))
	}
}

// Another household's open actions must not surface in OpenInMonth either.
func TestOpenInMonthIsScopedToItsHousehold(t *testing.T) {
	ctx := context.Background()
	actions, retros, db, householdID := newRetroActionFixture(t)
	other := seedSecondHousehold(t, db)

	retro, err := retros.Create(ctx, other, jul2026())
	if err != nil {
		t.Fatalf("create in other household: %v", err)
	}
	if _, err := actions.Add(ctx, usecase.RetroActionInput{HouseholdID: other, RetroID: retro.ID, Body: "not yours to read"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := actions.OpenInMonth(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("OpenInMonth: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("%d actions leaked from another household, want 0", len(got))
	}
}
