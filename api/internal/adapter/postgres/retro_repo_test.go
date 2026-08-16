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
// helper -- that package is a different package this one cannot import, so
// the convention (first of the calendar month, midnight UTC) is repeated
// here rather than shared.
func jul2026() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }

// newRetroRepo opens a fresh test database and one household, the same way
// every other *_repo_test.go in this package does through openTestDB and
// insertTestHousehold. It also returns the *postgres.DB itself: a literal
// `newRetroRepo(t) (repo, householdID)` cannot give seedSecondHousehold
// below anywhere to find the same database without global state keyed by
// *testing.T, which this package has no precedent for and which "do not
// invent a second seeding path" argues against just as much as a second
// SQL statement would.
func newRetroRepo(t *testing.T) (*postgres.RetroRepo, *postgres.DB, string) {
	t.Helper()
	db := openTestDB(t)
	householdID := insertTestHousehold(t, db)
	return postgres.NewRetroRepo(db), db, householdID
}

// seedSecondHousehold inserts a second household into the SAME database a
// prior newRetroRepo(t) call opened, using the one seeding helper this
// package already has for the job (insertTestHousehold) -- the same helper
// TestGoalGetFromAnotherHouseholdIsErrNotFound and its siblings already use
// for "another household" fixtures.
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

// Update's zero-row UPDATE has two possible causes, and the caller needs to
// know which: this test is the "the retro is simply gone" half.
// TestRetroUpdateRefusesAStaleVersionAndWritesNothing above is the "the
// version moved" half. Both start from a zero-row UpdateRetro; only the
// ByMonth lookup Update runs afterward tells them apart, and this test is
// what proves that lookup actually distinguishes them rather than always
// guessing ErrRetroChanged.
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

// Create's own contract (usecase/ports.go): the caller normalises the month
// before calling, and the repository stores and returns it exactly as the
// first-of-month, midnight-UTC value a later caller (RetroService.List's
// month.Equal comparisons) is entitled to rely on without re-normalising.
// This is the one place that proves the database round-trip actually holds
// that promise, rather than merely returning a value close enough that
// startOfMonth would paper over a drift.
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

// List returns every retro newest month first, each carrying its own action
// count -- List's own doc comment on usecase.RetroRepository. Quote is left
// for RetroService.List to overwrite (RetroSummary's own doc comment), so
// this test asserts nothing about it.
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

// Complete is idempotent: finishing an already finished retro keeps the
// FIRST completion timestamp, the same COALESCE shape GoalRepository's
// SetArchived already uses, rather than moving it forward to whatever
// timestamp the second call happens to carry.
func TestRetroCompleteTwiceKeepsTheFirstTimestamp(t *testing.T) {
	ctx := context.Background()
	repo, _, householdID := newRetroRepo(t)

	retro, err := repo.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	first, err := repo.Complete(ctx, householdID, retro.ID, time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("first Complete: %v", err)
	}

	second, err := repo.Complete(ctx, householdID, retro.ID, time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if !second.CompletedAt.Equal(*first.CompletedAt) {
		t.Fatalf("CompletedAt = %v, want %v (the first stamp)", second.CompletedAt, first.CompletedAt)
	}
}
