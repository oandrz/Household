// This file holds the marriage slice's vision ports, including the one-
// method goal reader VisionService declares for itself. ports.go lists
// every ports file.

package usecase

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// GoalProgress is the only thing Vision needs to know about a goal: what to
// call it and how far along it is. Percent is already
// domain.GoalProgressPercent's own capped 0-100 figure -- Vision does not
// recompute it, because a second percent formula in this codebase is exactly
// the kind of drift the Money specs spent five features avoiding.
type GoalProgress struct {
	GoalID  string
	Name    string
	Percent int
}

// GoalProgressReader is one method wide on purpose. VisionService needs the
// progress of a handful of goal ids; handing it GoalRepository -- whose own
// contract runs to forty lines about contribution scoping -- would be
// interface segregation traded away for one percentage.
type GoalProgressReader interface {
	// ProgressByIDs returns an entry, keyed by goal id, for each id that
	// exists in THIS household. A missing id is a miss, not an error: a
	// measure whose goal was deleted renders as a label with no figure, and
	// making that an error path would turn an ordinary page render into a
	// failure. Scoped by householdID in SQL, so a goal in another household
	// is indistinguishable from one that does not exist.
	//
	// An archived goal counts as found and keeps its figure: archiving is
	// not deletion anywhere else in this product either (spec decision 8),
	// and only a real DELETE unlinks a measure by firing goals.id's
	// ON DELETE SET NULL into vision_measures.goal_id. The implementing SQL
	// must NOT filter on archived_at -- unlike GoalRepository.List, which
	// takes an explicit includeArchived switch because its callers sometimes
	// want live goals only, this method has no such caller: Vision always
	// wants the figure a linked measure is pointing at, archived or not.
	ProgressByIDs(ctx context.Context, householdID string, goalIDs []string) (map[string]GoalProgress, error)
}

// VisionRepository stores one household's per-year visions. Every method is
// scoped by householdID and must filter on it in SQL.
type VisionRepository interface {
	// Get reports domain.ErrNotFound when the household has no vision for
	// that year. Turning that into the empty vision the screen renders is
	// VisionService's job, not this one's -- a repository that invented a
	// row would make "never set" and "set to blank" indistinguishable here.
	Get(ctx context.Context, householdID string, year int) (domain.Vision, error)
	// Save replaces the whole document in ONE transaction: upsert the parent,
	// delete every child, insert the submitted ones. Partial success must be
	// impossible -- the same transactional shape BudgetRepo.Upsert uses, and
	// ONLY that shape: Budget carries no version and no concurrency guard at
	// all, so its unconditional ON CONFLICT DO UPDATE is the right move
	// there and the wrong one here. Reaching for that same clause on Vision's
	// create path would silently destroy the guard the next paragraph
	// describes.
	//
	// Concurrency, in two cases that must not be collapsed:
	//   v.Version == 0  -- a create. Succeeds only while that household-year
	//                      has no row; reports domain.ErrVisionChanged if one
	//                      appeared since the caller read the empty vision.
	//                      The created row lands at version 1.
	//   v.Version  > 0  -- an update, WHERE version = v.Version. Zero rows
	//                      affected means either the vision was deleted or
	//                      the other partner saved first, and those are
	//                      different answers: re-read to tell them apart and
	//                      report domain.ErrNotFound or
	//                      domain.ErrVisionChanged accordingly.
	//                      RetroRepo.Update's own comment explains why the
	//                      cheap second read is worth it.
	//
	// Either way, the domain.Vision returned on success carries the version
	// AS STORED after the write -- 1 for a create, the stored value plus one
	// for an update -- the same contract RetroRepository.Update documents:
	// the caller never has to guess what to send on the next save.
	//
	// A measure naming a goal outside this household must be refused with
	// domain.ErrVisionGoalUnknown, checked INSIDE the transaction: the
	// vision_measures FK only proves a goal exists somewhere, never that it
	// is this household's -- the same hole validateLineCategories closes for
	// budget lines.
	Save(ctx context.Context, v domain.Vision) (domain.Vision, error)
}
