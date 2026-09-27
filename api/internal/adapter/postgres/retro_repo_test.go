package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// jul2026 is this file's own copy of the usecase test package's month
// helper: that package can't be imported from here, so the convention
// (first of the calendar month, midnight UTC) is repeated rather than shared.
func jul2026() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }

// newRetroRepo opens a fresh test database and one household, the same way
// every other *_repo_test.go in this package does (openTestDB,
// insertTestHousehold). It also returns the *postgres.DB itself, since
// seedSecondHousehold below needs the same database and this package has
// no precedent for global state keyed by *testing.T.
func newRetroRepo(t *testing.T) (*postgres.RetroRepo, *postgres.DB, string) {
	t.Helper()
	db := openTestDB(t)
	householdID := insertTestHousehold(t, db)
	return postgres.NewRetroRepo(db), db, householdID
}

// seedSecondHousehold inserts a second household into the SAME database a
// prior newRetroRepo(t) call opened, using the one seeding helper this
// package already has (insertTestHousehold) -- the same helper
// TestGoalGetFromAnotherHouseholdIsErrNotFound already uses.
func seedSecondHousehold(t *testing.T, db *postgres.DB) string {
	t.Helper()
	return insertTestHousehold(t, db)
}

// Two editors, one draft. The second save carries the version the first one
// invalidated, and it must be refused outright -- not merged, not applied.
func TestRetroUpdateRefusesAStaleVersionAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	repo, _, householdID := newRetroRepo(t)

	draft, err := repo.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	first, err := repo.Update(ctx, usecase.RetroUpdate{
		HouseholdID: householdID, RetroID: draft.ID, Month: jul2026(), Notes: "mine", Version: draft.Version,
	})
	if err != nil {
		t.Fatalf("first update: %v", err)
	}
	if first.Version != draft.Version+1 {
		t.Fatalf("version = %d, want %d", first.Version, draft.Version+1)
	}

	_, err = repo.Update(ctx, usecase.RetroUpdate{
		HouseholdID: householdID, RetroID: draft.ID, Month: jul2026(), Notes: "theirs", Version: draft.Version,
	})
	if !errors.Is(err, domain.ErrRetroChanged) {
		t.Fatalf("err = %v, want ErrRetroChanged", err)
	}

	after, err := repo.ByMonth(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("ByMonth: %v", err)
	}
	if after.Notes != "mine" {
		t.Fatalf("notes = %q -- the refused write landed anyway", after.Notes)
	}
}

// Update's zero-row UPDATE has two possible causes: this is the "retro is
// simply gone" half, and TestRetroUpdateRefusesAStaleVersionAndWritesNothing
// is the "version moved" half. Both start from a zero-row UpdateRetro; only
// the ByMonth lookup afterward tells them apart, and this test proves that
// lookup actually distinguishes them instead of always guessing
// ErrRetroChanged.
func TestRetroUpdateOnADeletedRetroIsNotFound(t *testing.T) {
	ctx := context.Background()
	repo, _, householdID := newRetroRepo(t)

	draft, err := repo.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.DeleteDraft(ctx, householdID, draft.ID); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}

	_, err = repo.Update(ctx, usecase.RetroUpdate{
		HouseholdID: householdID, RetroID: draft.ID, Month: jul2026(), Notes: "too late", Version: draft.Version,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound (the retro was deleted, not just out of date)", err)
	}
}

// A zero-row match must be an error, not a silent success. This is the
// SetBillNextDue defect, written as a test before the code exists.
func TestDeleteDraftRefusesAFinishedRetro(t *testing.T) {
	ctx := context.Background()
	repo, _, householdID := newRetroRepo(t)

	retro, err := repo.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.Complete(ctx, householdID, retro.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	err = repo.DeleteDraft(ctx, householdID, retro.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if _, err := repo.ByMonth(ctx, householdID, jul2026()); err != nil {
		t.Fatalf("the finished retro was deleted anyway: %v", err)
	}
}

// The UNIQUE constraint surfaces as a domain error, never as a raw pgx one.
func TestRetroCreateTwiceInOneMonthIsAlreadyExists(t *testing.T) {
	ctx := context.Background()
	repo, _, householdID := newRetroRepo(t)

	if _, err := repo.Create(ctx, householdID, jul2026()); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := repo.Create(ctx, householdID, jul2026())
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("err = %v, want ErrAlreadyExists", err)
	}
}

// Another household's retro is indistinguishable from one that never existed.
func TestRetroByMonthIsScopedToItsHousehold(t *testing.T) {
	ctx := context.Background()
	repo, db, householdID := newRetroRepo(t)
	other := seedSecondHousehold(t, db)

	if _, err := repo.Create(ctx, other, jul2026()); err != nil {
		t.Fatalf("create in other household: %v", err)
	}
	if _, err := repo.ByMonth(ctx, householdID, jul2026()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// RetroRepository.Create's contract: the caller normalises the month
// first, and the repository stores and returns it exactly as that
// first-of-month, midnight-UTC value -- RetroService.List's month.Equal
// comparisons rely on that without re-normalising. This is the one test
// that proves the round-trip actually holds the promise, not just a value
// close enough that startOfMonth would paper over.
func TestRetroCreateRoundTripsMonthAsNormalisedUTC(t *testing.T) {
	ctx := context.Background()
	repo, _, householdID := newRetroRepo(t)

	got, err := repo.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !got.Month.Equal(jul2026()) {
		t.Fatalf("Month = %v, want %v", got.Month, jul2026())
	}
	if got.Month.Location() != time.UTC {
		t.Fatalf("Month location = %v, want time.UTC", got.Month.Location())
	}
}

// List returns every retro newest month first, each carrying its own
// action count (see usecase.RetroRepository.List). Quote is left for
// RetroService.List to overwrite, so this test asserts nothing about it.
func TestRetroListOrdersNewestMonthFirstWithActionCounts(t *testing.T) {
	ctx := context.Background()
	repo, _, householdID := newRetroRepo(t)

	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if _, err := repo.Create(ctx, householdID, june); err != nil {
		t.Fatalf("create june: %v", err)
	}
	if _, err := repo.Create(ctx, householdID, jul2026()); err != nil {
		t.Fatalf("create july: %v", err)
	}

	summaries, err := repo.List(ctx, householdID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("len(summaries) = %d, want 2", len(summaries))
	}
	if !summaries[0].Retro.Month.Equal(jul2026()) {
		t.Fatalf("summaries[0].Retro.Month = %v, want july (newest first)", summaries[0].Retro.Month)
	}
	if !summaries[1].Retro.Month.Equal(june) {
		t.Fatalf("summaries[1].Retro.Month = %v, want june", summaries[1].Retro.Month)
	}
	if summaries[0].ActionCount != 0 {
		t.Fatalf("ActionCount = %d, want 0 -- no actions inserted", summaries[0].ActionCount)
	}
}

// ActionCount and OpenActionCount must disagree the moment even one action
// is ticked. Don't feed Overview's "Next retro" card ActionCount: a
// fully-ticked retro would still claim outstanding work on the home page.
// Three actions, two ticked, is enough to prove the open subquery filters
// on done_at rather than repeating the total's count(*).
func TestRetroListReportsOpenActionCountSeparatelyFromTheTotal(t *testing.T) {
	ctx := context.Background()
	retros, db, householdID := newRetroRepo(t)
	actions := postgres.NewRetroActionRepo(db)

	retro, err := retros.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var ids []string
	for _, body := range []string{"phone-free dinners", "call the accountant", "plan the September trip"} {
		a, err := actions.Add(ctx, usecase.RetroActionInput{HouseholdID: householdID, RetroID: retro.ID, Body: body})
		if err != nil {
			t.Fatalf("Add(%q): %v", body, err)
		}
		ids = append(ids, a.ID)
	}
	for _, id := range ids[:2] {
		if err := actions.SetDone(ctx, householdID, id, true, time.Now().UTC()); err != nil {
			t.Fatalf("SetDone(%s): %v", id, err)
		}
	}

	summaries, err := retros.List(ctx, householdID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("len(summaries) = %d, want 1", len(summaries))
	}
	if got := summaries[0].ActionCount; got != 3 {
		t.Fatalf("ActionCount = %d, want 3 (ticked actions still count toward the total)", got)
	}
	if got := summaries[0].OpenActionCount; got != 1 {
		t.Fatalf("OpenActionCount = %d, want 1 (only the unticked action)", got)
	}
}

// Complete is idempotent: finishing an already-finished retro keeps the
// FIRST completion timestamp (the same COALESCE shape GoalRepository's
// SetArchived uses), not whatever the second call happens to carry. Both
// calls are checked against the actual `at` each passed, not just against
// each other -- comparing only second against first would pass just as
// happily against coalesce(completed_at, now()) silently ignoring the
// caller's `at`, which the port doesn't promise.
func TestRetroCompleteTwiceKeepsTheFirstTimestamp(t *testing.T) {
	ctx := context.Background()
	repo, _, householdID := newRetroRepo(t)

	retro, err := repo.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	aug1 := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	first, err := repo.Complete(ctx, householdID, retro.ID, aug1)
	if err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	if first.CompletedAt == nil || !first.CompletedAt.Equal(aug1) {
		t.Fatalf("first CompletedAt = %v, want %v", first.CompletedAt, aug1)
	}

	aug2 := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	second, err := repo.Complete(ctx, householdID, retro.ID, aug2)
	if err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if second.CompletedAt == nil || !second.CompletedAt.Equal(aug1) {
		t.Fatalf("second CompletedAt = %v, want %v (the first stamp, not %v)", second.CompletedAt, aug1, aug2)
	}
}

// A version outside int32's range must never be silently truncated into a
// value that happens to match the real one: retros.version is a Postgres
// `integer`, and int32(4294967297) == 1, so without versionParam's guard
// this draft (created at version 1) would be overwritten by a version
// number nobody legitimately arrived at.
func TestRetroUpdateWithOutOfInt32RangeVersionIsRefused(t *testing.T) {
	ctx := context.Background()
	repo, _, householdID := newRetroRepo(t)

	draft, err := repo.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = repo.Update(ctx, usecase.RetroUpdate{
		HouseholdID: householdID, RetroID: draft.ID, Month: jul2026(), Notes: "hijacked", Version: 4294967297,
	})
	if !errors.Is(err, domain.ErrRetroChanged) {
		t.Fatalf("err = %v, want ErrRetroChanged", err)
	}

	after, err := repo.ByMonth(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("ByMonth: %v", err)
	}
	if after.Notes != "" {
		t.Fatalf("notes = %q -- the truncated-version write landed anyway", after.Notes)
	}
}

// Another household must not be able to edit this one's retro. An id
// alone is not proof of ownership -- UpdateRetro's WHERE also requires
// household_id; without that clause, any household holding or guessing a
// retro id could overwrite another household's month.
func TestRetroUpdateIsScopedToItsHousehold(t *testing.T) {
	ctx := context.Background()
	repo, db, householdID := newRetroRepo(t)
	other := seedSecondHousehold(t, db)

	retro, err := repo.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = repo.Update(ctx, usecase.RetroUpdate{
		HouseholdID: other, RetroID: retro.ID, Month: jul2026(), Notes: "stolen", Version: retro.Version,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound -- another household has no retro this month, so this must read as gone, not as a conflict", err)
	}

	after, err := repo.ByMonth(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("ByMonth: %v", err)
	}
	if after.Notes != "" || after.Version != retro.Version {
		t.Fatalf("notes = %q, version = %d -- another household's write landed", after.Notes, after.Version)
	}
}

// Another household must not be able to finish this one's retro.
func TestRetroCompleteIsScopedToItsHousehold(t *testing.T) {
	ctx := context.Background()
	repo, db, householdID := newRetroRepo(t)
	other := seedSecondHousehold(t, db)

	retro, err := repo.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := repo.Complete(ctx, other, retro.ID, time.Now().UTC()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	after, err := repo.ByMonth(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("ByMonth: %v", err)
	}
	if after.CompletedAt != nil {
		t.Fatalf("CompletedAt = %v, want nil -- another household finished this draft", after.CompletedAt)
	}
}

// Another household must not be able to delete this one's draft. Removing
// household_id from DeleteDraftRetro's WHERE turns this red, since the
// delete then succeeds instead of reporting ErrNotFound.
func TestRetroDeleteDraftIsScopedToItsHousehold(t *testing.T) {
	ctx := context.Background()
	repo, db, householdID := newRetroRepo(t)
	other := seedSecondHousehold(t, db)

	retro, err := repo.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.DeleteDraft(ctx, other, retro.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	if _, err := repo.ByMonth(ctx, householdID, jul2026()); err != nil {
		t.Fatalf("another household deleted this draft: %v", err)
	}
}
