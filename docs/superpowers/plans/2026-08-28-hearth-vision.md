# Vision & goals Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Marriage's second page — the yearly theme, its pillars with measures, longer-horizon milestones, the Edit-vision modal, and Vision's two surfaces on the Overview.

**Architecture:** Four relational tables (`visions` and three child tables) written by one whole-document `PUT` inside a single transaction, exactly the shape `BudgetRepo.Upsert` already uses. A `version` column guards concurrent edits. A measure is either typed or linked to a savings goal; a linked one resolves its percentage through a new one-method port, `GoalProgressReader`, implemented by the existing goals repository.

**Tech Stack:** Go 1.24, chi, pgx/v5, sqlc, goose migrations, testcontainers; React 19 + TypeScript, TanStack Query, TanStack Router, zod, Tailwind, Vitest.

**Spec:** `docs/superpowers/specs/2026-08-28-hearth-vision-design.md` — the plan argues from it; read both.

## Global Constraints

- **Clean architecture, enforced by `make lint-arch`.** `internal/domain` imports the standard library only; `internal/usecase` may add `internal/domain`; everything else is `internal/adapter/**` or `cmd/**`. No pgx, chi or JSON type crosses out of the adapter layer. A missing row becomes `domain.ErrNotFound` at the adapter boundary, never `pgx.ErrNoRows` above it.
- **Authorisation lives only in the HTTP layer.** No service takes an actor parameter. `VisionService` never asks who is calling.
- **Every 2xx except 204 carries a JSON body** — `apiFetch` throws on an ok response it cannot parse.
- **Fail closed on values you did not construct.** Every `switch` over a value arriving from a database column or a request body needs a `default` that refuses.
- **No `float64` anywhere in this feature.** Vision holds no money. A linked measure renders an `int` percentage from `domain.GoalProgressPercent`; it never renders a currency amount.
- **Guards:** both routes join the existing marriage group — `requireCapability(domain.CapMarriage)` stacked on `requireOwner` (`api/internal/adapter/http/router.go:301-302`), the write additionally behind `requireCSRF`.
- **Caps, copied verbatim from the spec:** 12 pillars per vision, 8 measures per pillar, 24 milestones, theme ≤ 120 chars, description ≤ 2000 chars, year within 1900–2200.
- **Comments say why, never what.** Every decision below that a future editor might undo needs its reason at the line they would change it.
- **Running Go on this machine:**
  ```bash
  export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
  export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
  export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
  ```

---

## File Structure

**Created**

| File | Responsibility |
|---|---|
| `api/migrations/00010_vision.sql` | The four tables |
| `api/internal/domain/vision.go` | `Vision`, `Pillar`, `Measure`, `Milestone`, their validation |
| `api/internal/domain/vision_test.go` | Validation table tests |
| `api/internal/adapter/postgres/queries/vision.sql` | sqlc queries |
| `api/internal/adapter/postgres/vision_repo.go` | `VisionRepo.Get` / `.Save` |
| `api/internal/adapter/postgres/vision_repo_test.go` | Repository tests against real Postgres |
| `api/internal/usecase/vision.go` | `VisionService`, the view types |
| `api/internal/usecase/vision_test.go` | Service tests against in-memory doubles |
| `api/internal/adapter/http/vision_handlers.go` | DTOs + two handlers |
| `api/internal/adapter/http/vision_api_test.go` | Route-level tests incl. CSRF and non-owner |
| `web/src/features/marriage/visionSchemas.ts` | zod mirrors of the DTOs |
| `web/src/features/marriage/visionQueryKeys.ts` | Query keys, shared like `retroQueryKeys.ts` |
| `web/src/features/marriage/useVision.ts` | Fetch + save orchestration |
| `web/src/features/marriage/VisionPage.tsx` | The page shell: hero, grid, milestones, empty state |
| `web/src/features/marriage/PillarCard.tsx` | One pillar and its measures |
| `web/src/features/marriage/MilestoneGrid.tsx` | The "Longer horizon" panel |
| `web/src/features/marriage/VisionModal.tsx` | The whole-document editor |
| `web/src/features/overview/VisionCard.tsx` | Overview's `Vision 2026` card |
| Tests beside each frontend file | `*.test.tsx` / `*.test.ts` |

**Modified**

| File | Change |
|---|---|
| `api/internal/domain/errors.go` | The vision sentinel errors |
| `api/internal/usecase/ports.go` | `VisionRepository`, `GoalProgressReader`, `GoalProgress` |
| `api/internal/adapter/postgres/goal_repo.go` | `ProgressByIDs` |
| `api/internal/adapter/postgres/queries/goal.sql` | Its query |
| `api/internal/adapter/http/errors.go` | Map the new domain errors |
| `api/internal/adapter/http/router.go` | Two routes in the marriage group |
| `api/cmd/api/main.go` | Wire repo + service |
| `web/src/routes/router.tsx` | `marriageVisionRoute` + the `/marriage` index redirect |
| `web/src/features/shell/Sidebar.tsx` | `SPACE_PAGES.marriage` gains Vision |
| `web/src/features/overview/OverviewPage.tsx` | Mount `VisionCard` |
| `web/src/features/overview/NextRetroCard.tsx` | The Vision check-in strip |
| `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`, `docs/SYSTEM_DESIGN.md` | Task 14 |

---

### Task 1: The migration

**Files:**
- Create: `api/migrations/00010_vision.sql`
- Test: `api/internal/adapter/postgres/vision_repo_test.go` (schema tests only in this task)

**Interfaces:**
- Consumes: nothing.
- Produces: tables `visions`, `vision_pillars`, `vision_measures`, `vision_milestones`; constraint name `measure_is_typed_or_linked`.

- [ ] **Step 1: Write the failing schema tests**

Create `api/internal/adapter/postgres/vision_repo_test.go`. These two tests are the whole reason the CHECK has three branches — the second one is what stops Vision breaking the Goals feature.

```go
package postgres_test

import (
	"context"
	"testing"
)

// A measure carrying BOTH a goal link and a typed target renders two
// different answers to the same question. The domain refuses it too; this
// proves the database does not depend on the domain being correct.
func TestVisionMeasureCannotBeBothTypedAndLinked(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	householdID := insertTestHousehold(t, db)

	var visionID, pillarID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO visions (household_id, year, theme) VALUES ($1, 2026, 'Slow down together') RETURNING id`,
		householdID).Scan(&visionID); err != nil {
		t.Fatalf("insert vision: %v", err)
	}
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO vision_pillars (vision_id, position, name) VALUES ($1, 0, 'Us before logistics') RETURNING id`,
		visionID).Scan(&pillarID); err != nil {
		t.Fatalf("insert pillar: %v", err)
	}
	goalID := insertTestGoal(t, db, householdID)

	_, err := db.Pool().Exec(ctx,
		`INSERT INTO vision_measures (pillar_id, position, label, current_value, target_value, goal_id)
		 VALUES ($1, 0, 'Emergency fund', 2, 4, $2)`, pillarID, goalID)
	if err == nil {
		t.Fatal("expected measure_is_typed_or_linked to refuse a measure that is both typed and linked")
	}
}

// Deleting a goal must unlink the measure, not fail. ON DELETE SET NULL is
// an UPDATE, and Postgres enforces CHECK constraints on UPDATE -- so without
// the constraint's third (all-null) branch this delete raises a violation
// inside the GOALS feature, where nobody would think to look at Vision.
func TestDeletingALinkedGoalUnlinksTheMeasureInsteadOfFailing(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	householdID := insertTestHousehold(t, db)
	goalID := insertTestGoal(t, db, householdID)

	var visionID, pillarID, measureID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO visions (household_id, year, theme) VALUES ($1, 2026, 'Slow down together') RETURNING id`,
		householdID).Scan(&visionID); err != nil {
		t.Fatalf("insert vision: %v", err)
	}
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO vision_pillars (vision_id, position, name) VALUES ($1, 0, 'Money without fear') RETURNING id`,
		visionID).Scan(&pillarID); err != nil {
		t.Fatalf("insert pillar: %v", err)
	}
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO vision_measures (pillar_id, position, label, goal_id)
		 VALUES ($1, 0, 'Emergency fund', $2) RETURNING id`, pillarID, goalID).Scan(&measureID); err != nil {
		t.Fatalf("insert measure: %v", err)
	}

	if _, err := db.Pool().Exec(ctx, `DELETE FROM goals WHERE id = $1`, goalID); err != nil {
		t.Fatalf("delete goal: %v", err)
	}

	var goalRef *string
	var current, target *int32
	if err := db.Pool().QueryRow(ctx,
		`SELECT goal_id, current_value, target_value FROM vision_measures WHERE id = $1`,
		measureID).Scan(&goalRef, &current, &target); err != nil {
		t.Fatalf("measure should still exist after its goal was deleted: %v", err)
	}
	if goalRef != nil || current != nil || target != nil {
		t.Fatalf("expected an unlinked, figureless measure; got goal=%v current=%v target=%v", goalRef, current, target)
	}
}
```

Add the two helpers at the bottom of the same file if the package has none by these names (check first — `openTestDB` already exists in this package):

```go
func insertTestHousehold(t *testing.T, db *postgres.DB) string {
	t.Helper()
	var id string
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO households (name, family_name) VALUES ('Andreas & Christine', 'Oentoro') RETURNING id`).
		Scan(&id); err != nil {
		t.Fatalf("insert household: %v", err)
	}
	return id
}

func insertTestGoal(t *testing.T, db *postgres.DB, householdID string) string {
	t.Helper()
	var id string
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO goals (household_id, name, target_amount_minor, currency)
		 VALUES ($1, 'Emergency fund', 1000000, 'SGD') RETURNING id`, householdID).Scan(&id); err != nil {
		t.Fatalf("insert goal: %v", err)
	}
	return id
}
```

Check `goals`' real column list in `api/migrations/00007_goals.sql` before running, and match it — this insert must be valid against the actual schema, not a guessed one.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd api && go test ./internal/adapter/postgres/ -run 'TestVisionMeasure|TestDeletingALinkedGoal' -v
```
Expected: FAIL — `relation "visions" does not exist`.

- [ ] **Step 3: Write the migration**

Create `api/migrations/00010_vision.sql`:

```sql
-- +goose Up

-- visions is one household-year's theme. One row per calendar year (UNIQUE
-- below): a theme is set every January and the previous year's stays
-- readable forever, which is why this is a row per year rather than one
-- mutable row per household (spec decision 4).
CREATE TABLE visions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    -- A vision is a calendar year, not a month or a day -- unlike retros.month,
    -- which is a date because a retro happens on one.
    year         smallint    NOT NULL CHECK (year BETWEEN 1900 AND 2200),
    -- Defaulted rather than merely NOT NULL because GET returns an empty
    -- vision for a year never set (spec decision 9), and the simplest shape
    -- for that is a row that can exist while still blank.
    theme        text        NOT NULL DEFAULT '',
    description  text        NOT NULL DEFAULT '',
    -- Optimistic concurrency (spec decision 10). A whole-document replace
    -- makes a lost update worse than a field-level one: a partner's entire
    -- set of pillars can be silently overwritten by a stale editor. Version 0
    -- never appears in this column -- it is the wire value meaning "I read a
    -- year that had no vision", and a create lands here at 1.
    version      integer     NOT NULL DEFAULT 1,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, year)
);

-- Children carry an explicit position, unlike retro_actions, which
-- deliberately has none: its only safe writer (max(position)+1) raced when
-- two partners added an action at once. Vision has no such race -- one save
-- writes every child of a document in one transaction, so position is
-- assigned from the submitted array's index by a single writer. The design
-- also numbers pillars visibly ("Pillar 1", "Pillar 2"), so the order is
-- something the household sees rather than an accident of insertion.
CREATE TABLE vision_pillars (
    id          uuid     PRIMARY KEY DEFAULT gen_random_uuid(),
    vision_id   uuid     NOT NULL REFERENCES visions(id) ON DELETE CASCADE,
    position    smallint NOT NULL,
    name        text     NOT NULL,
    description text     NOT NULL DEFAULT '',
    UNIQUE (vision_id, position)
);

CREATE TABLE vision_measures (
    id            uuid     PRIMARY KEY DEFAULT gen_random_uuid(),
    pillar_id     uuid     NOT NULL REFERENCES vision_pillars(id) ON DELETE CASCADE,
    position      smallint NOT NULL,
    label         text     NOT NULL,
    current_value integer,
    target_value  integer,
    -- ON DELETE SET NULL: deleting a goal must never delete a pillar's
    -- measure, let alone cascade into the vision (spec decision 8).
    goal_id       uuid     REFERENCES goals(id) ON DELETE SET NULL,
    UNIQUE (pillar_id, position),
    -- Three branches, and the third is NOT optional. A referential SET NULL
    -- is an UPDATE, and Postgres enforces CHECK constraints on UPDATE -- so
    -- with only the typed and linked branches, deleting a goal would raise a
    -- constraint violation inside the GOALS feature. The third branch is the
    -- broken-link state the Vision page renders as a label with no figure.
    -- The database permits that state only because SET NULL produces it; the
    -- domain still refuses to CREATE one, so a PUT carrying a measure with
    -- neither a goal nor a target is 422. Read tolerantly, write strictly.
    CONSTRAINT measure_is_typed_or_linked CHECK (
        -- typed
        (goal_id IS NULL     AND target_value IS NOT NULL AND target_value > 0
                             AND current_value IS NOT NULL AND current_value >= 0)
        -- linked
     OR (goal_id IS NOT NULL AND target_value IS NULL AND current_value IS NULL)
        -- link broken by a deleted goal
     OR (goal_id IS NULL     AND target_value IS NULL AND current_value IS NULL)
    )
);

CREATE INDEX vision_measures_pillar_id_idx ON vision_measures (pillar_id);

CREATE TABLE vision_milestones (
    id        uuid     PRIMARY KEY DEFAULT gen_random_uuid(),
    vision_id uuid     NOT NULL REFERENCES visions(id) ON DELETE CASCADE,
    year      smallint NOT NULL CHECK (year BETWEEN 1900 AND 2200),
    title     text     NOT NULL,
    note      text     NOT NULL DEFAULT '',
    position  smallint NOT NULL,
    UNIQUE (vision_id, position)
);

-- +goose Down
DROP TABLE vision_milestones;
DROP TABLE vision_measures;
DROP TABLE vision_pillars;
DROP TABLE visions;
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd api && go test ./internal/adapter/postgres/ -run 'TestVisionMeasure|TestDeletingALinkedGoal' -v
```
Expected: PASS, both.

- [ ] **Step 5: Mutation-check the constraint's third branch**

Delete the third `OR` branch from the CHECK, re-run, and confirm `TestDeletingALinkedGoal…` fails with a check-constraint violation. Restore it. A green test proves nothing until you have seen it go red for the right reason.

- [ ] **Step 6: Commit**

```bash
git add api/migrations/00010_vision.sql api/internal/adapter/postgres/vision_repo_test.go
git commit -m "feat: vision tables, with the CHECK branch that keeps goal deletion working"
```

---

### Task 2: Domain types and validation

**Files:**
- Create: `api/internal/domain/vision.go`, `api/internal/domain/vision_test.go`
- Modify: `api/internal/domain/errors.go`

**Interfaces:**
- Consumes: nothing (stdlib only — this package may import nothing else).
- Produces: `domain.Vision`, `domain.Pillar`, `domain.Measure`, `domain.Milestone`, `domain.MeasureKind` with `MeasureTyped`/`MeasureLinked`/`MeasureBroken`, `Vision.Validate() error`, the caps as exported constants, and the sentinel errors below.

- [ ] **Step 1: Write the failing tests**

Create `api/internal/domain/vision_test.go`:

```go
package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func validVision() domain.Vision {
	return domain.Vision{
		HouseholdID: "h1",
		Year:        2026,
		Theme:       "Slow down together",
		Description: "Fewer commitments, more presence.",
		Pillars: []domain.Pillar{{
			Name:        "Us before logistics",
			Description: "We're partners first.",
			Measures: []domain.Measure{
				{Label: "Date nights / month", Kind: domain.MeasureTyped, Current: 2, Target: 2},
				{Label: "Emergency fund", Kind: domain.MeasureLinked, GoalID: "g1"},
			},
		}},
		Milestones: []domain.Milestone{{Year: 2029, Title: "Upgrade to a bigger place"}},
	}
}

func TestAValidVisionValidates(t *testing.T) {
	if err := validVision().Validate(); err != nil {
		t.Fatalf("expected the fixture to be valid, got %v", err)
	}
}

func TestVisionValidationRefuses(t *testing.T) {
	cases := []struct {
		name string
		mutate func(*domain.Vision)
		want error
	}{
		{"an empty theme", func(v *domain.Vision) { v.Theme = "   " }, domain.ErrVisionThemeRequired},
		{"an over-long theme", func(v *domain.Vision) { v.Theme = strings.Repeat("x", 121) }, domain.ErrVisionThemeTooLong},
		{"an over-long description", func(v *domain.Vision) { v.Description = strings.Repeat("x", 2001) }, domain.ErrVisionDescriptionTooLong},
		{"a year below the range", func(v *domain.Vision) { v.Year = 1899 }, domain.ErrVisionYearOutOfRange},
		{"a year above the range", func(v *domain.Vision) { v.Year = 2201 }, domain.ErrVisionYearOutOfRange},
		{"a nameless pillar", func(v *domain.Vision) { v.Pillars[0].Name = "" }, domain.ErrVisionPillarNameRequired},
		{"an unlabelled measure", func(v *domain.Vision) { v.Pillars[0].Measures[0].Label = "" }, domain.ErrVisionMeasureLabelRequired},
		{"a typed measure with a zero target", func(v *domain.Vision) { v.Pillars[0].Measures[0].Target = 0 }, domain.ErrVisionMeasureTargetNotPositive},
		{"a typed measure with a negative current", func(v *domain.Vision) { v.Pillars[0].Measures[0].Current = -1 }, domain.ErrVisionMeasureCurrentNegative},
		{"a typed measure that also names a goal", func(v *domain.Vision) { v.Pillars[0].Measures[0].GoalID = "g9" }, domain.ErrVisionMeasureAmbiguous},
		{"a linked measure that also carries a target", func(v *domain.Vision) { v.Pillars[0].Measures[1].Target = 5 }, domain.ErrVisionMeasureAmbiguous},
		{"a linked measure naming no goal", func(v *domain.Vision) { v.Pillars[0].Measures[1].GoalID = "" }, domain.ErrVisionMeasureGoalRequired},
		// The database tolerates a broken link because ON DELETE SET NULL
		// produces one. A save must never create one.
		{"a broken measure on the write path", func(v *domain.Vision) { v.Pillars[0].Measures[1].Kind = domain.MeasureBroken }, domain.ErrVisionMeasureAmbiguous},
		{"an unknown measure kind", func(v *domain.Vision) { v.Pillars[0].Measures[1].Kind = "sideways" }, domain.ErrVisionMeasureAmbiguous},
		{"a titleless milestone", func(v *domain.Vision) { v.Milestones[0].Title = " " }, domain.ErrVisionMilestoneTitleRequired},
		{"a milestone year out of range", func(v *domain.Vision) { v.Milestones[0].Year = 3000 }, domain.ErrVisionYearOutOfRange},
		{"too many pillars", func(v *domain.Vision) {
			v.Pillars = make([]domain.Pillar, domain.MaxVisionPillars+1)
			for i := range v.Pillars {
				v.Pillars[i] = domain.Pillar{Name: "P"}
			}
		}, domain.ErrVisionTooManyPillars},
		{"too many measures on one pillar", func(v *domain.Vision) {
			v.Pillars[0].Measures = make([]domain.Measure, domain.MaxPillarMeasures+1)
			for i := range v.Pillars[0].Measures {
				v.Pillars[0].Measures[i] = domain.Measure{Label: "M", Kind: domain.MeasureTyped, Target: 1}
			}
		}, domain.ErrVisionTooManyMeasures},
		{"too many milestones", func(v *domain.Vision) {
			v.Milestones = make([]domain.Milestone, domain.MaxVisionMilestones+1)
			for i := range v.Milestones {
				v.Milestones[i] = domain.Milestone{Year: 2030, Title: "M"}
			}
		}, domain.ErrVisionTooManyMilestones},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := validVision()
			tc.mutate(&v)
			err := v.Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd api && go test ./internal/domain/ -run TestVision -v
```
Expected: FAIL — `undefined: domain.Vision`.

- [ ] **Step 3: Add the sentinel errors**

Append to `api/internal/domain/errors.go`, each with the one-line doc comment the file's existing entries all carry:

```go
// ErrVisionThemeRequired is a save with no theme. The empty vision GET
// returns for a year never set is allowed to have none; a save is not.
var ErrVisionThemeRequired = errors.New("a vision needs a theme")

var ErrVisionThemeTooLong = errors.New("a vision theme is too long")

var ErrVisionDescriptionTooLong = errors.New("a vision description is too long")

var ErrVisionYearOutOfRange = errors.New("a year must be between 1900 and 2200")

var ErrVisionPillarNameRequired = errors.New("a pillar needs a name")

var ErrVisionMeasureLabelRequired = errors.New("a measure needs a label")

var ErrVisionMeasureTargetNotPositive = errors.New("a measure target must be positive")

var ErrVisionMeasureCurrentNegative = errors.New("a measure's current value cannot be negative")

// ErrVisionMeasureAmbiguous covers every shape that is neither cleanly typed
// nor cleanly linked -- both at once, neither at all, or an unrecognised
// kind. One error rather than three: from the editor's point of view they are
// the same mistake, and the database's measure_is_typed_or_linked refuses the
// same set.
var ErrVisionMeasureAmbiguous = errors.New("a measure is either typed or linked to a goal, never both")

var ErrVisionMeasureGoalRequired = errors.New("a linked measure needs a goal")

var ErrVisionMilestoneTitleRequired = errors.New("a milestone needs a title")

var ErrVisionTooManyPillars = errors.New("too many pillars")

var ErrVisionTooManyMeasures = errors.New("too many measures on one pillar")

var ErrVisionTooManyMilestones = errors.New("too many milestones")

// ErrVisionChanged is the optimistic-concurrency refusal, the twin of
// ErrRetroChanged. It also covers the first save: two owners who both read an
// unset year both hold version 0, and the second one must be told rather than
// silently overwriting a whole year of pillars.
var ErrVisionChanged = errors.New("this vision changed while you were editing it")

// ErrVisionGoalUnknown is a measure naming a goal that is not this
// household's. Indistinguishable from a goal that does not exist, the scoping
// rule every repository here already follows.
var ErrVisionGoalUnknown = errors.New("a measure's goal does not belong to this household")
```

- [ ] **Step 4: Write the domain types**

Create `api/internal/domain/vision.go`:

```go
package domain

import "strings"

// The collection caps. A save rewrites every child of a document, so the cost
// of one write has to be bounded by something other than whatever a request
// body happens to contain. The numbers are generous against a design that
// draws three pillars and three milestones.
const (
	MaxVisionPillars        = 12
	MaxPillarMeasures       = 8
	MaxVisionMilestones     = 24
	MaxVisionThemeLen       = 120
	MaxVisionDescriptionLen = 2000
	MinVisionYear           = 1900
	MaxVisionYear           = 2200
)

// MeasureKind is which of three shapes a measure has. MeasureBroken exists
// only because ON DELETE SET NULL produces it when a linked goal is deleted:
// the page renders such a measure as a label with no figure, and Validate
// refuses to create one. Read tolerantly, write strictly.
type MeasureKind string

const (
	MeasureTyped  MeasureKind = "typed"
	MeasureLinked MeasureKind = "linked"
	MeasureBroken MeasureKind = "broken"
)

// Measure is one line under a pillar. A typed measure carries Current and
// Target; a linked one carries GoalID and reads its figure from that goal.
type Measure struct {
	ID      string
	Label   string
	Kind    MeasureKind
	Current int
	Target  int
	GoalID  string
}

type Pillar struct {
	ID          string
	Name        string
	Description string
	Measures    []Measure
}

type Milestone struct {
	ID    string
	Year  int
	Title string
	Note  string
}

// Vision is one household-year. Version is the optimistic-concurrency token:
// 0 means "read from a year that had no vision", so a save carrying 0 is a
// create.
type Vision struct {
	ID          string
	HouseholdID string
	Year        int
	Theme       string
	Description string
	Version     int
	Pillars     []Pillar
	Milestones  []Milestone
}

// Validate is the write path's rules. It is deliberately stricter than the
// database: MeasureBroken passes the schema's third CHECK branch but is
// refused here, because nothing should be able to create a measure whose
// figure is missing on purpose.
func (v Vision) Validate() error {
	if strings.TrimSpace(v.Theme) == "" {
		return ErrVisionThemeRequired
	}
	if len(v.Theme) > MaxVisionThemeLen {
		return ErrVisionThemeTooLong
	}
	if len(v.Description) > MaxVisionDescriptionLen {
		return ErrVisionDescriptionTooLong
	}
	if v.Year < MinVisionYear || v.Year > MaxVisionYear {
		return ErrVisionYearOutOfRange
	}
	if len(v.Pillars) > MaxVisionPillars {
		return ErrVisionTooManyPillars
	}
	for _, p := range v.Pillars {
		if err := p.validate(); err != nil {
			return err
		}
	}
	if len(v.Milestones) > MaxVisionMilestones {
		return ErrVisionTooManyMilestones
	}
	for _, m := range v.Milestones {
		if strings.TrimSpace(m.Title) == "" {
			return ErrVisionMilestoneTitleRequired
		}
		if m.Year < MinVisionYear || m.Year > MaxVisionYear {
			return ErrVisionYearOutOfRange
		}
	}
	return nil
}

func (p Pillar) validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return ErrVisionPillarNameRequired
	}
	if len(p.Measures) > MaxPillarMeasures {
		return ErrVisionTooManyMeasures
	}
	for _, m := range p.Measures {
		if err := m.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (m Measure) validate() error {
	if strings.TrimSpace(m.Label) == "" {
		return ErrVisionMeasureLabelRequired
	}
	// Fail closed: Kind arrives from a request body, so anything this switch
	// does not recognise -- MeasureBroken included -- is refused rather than
	// falling through to a default shape.
	switch m.Kind {
	case MeasureTyped:
		if m.GoalID != "" {
			return ErrVisionMeasureAmbiguous
		}
		if m.Target <= 0 {
			return ErrVisionMeasureTargetNotPositive
		}
		if m.Current < 0 {
			return ErrVisionMeasureCurrentNegative
		}
		return nil
	case MeasureLinked:
		if m.Current != 0 || m.Target != 0 {
			return ErrVisionMeasureAmbiguous
		}
		if m.GoalID == "" {
			return ErrVisionMeasureGoalRequired
		}
		return nil
	default:
		return ErrVisionMeasureAmbiguous
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd api && go test ./internal/domain/ -run TestVision -v
```
Expected: PASS, every subtest.

- [ ] **Step 6: Mutation-check the fail-closed default**

Change the `default:` branch to `return nil` and re-run. `an unknown measure kind` and `a broken measure on the write path` must both fail. Restore.

- [ ] **Step 7: Commit**

```bash
git add api/internal/domain/vision.go api/internal/domain/vision_test.go api/internal/domain/errors.go
git commit -m "feat: vision domain types and their validation"
```

---

### Task 3: The ports

**Files:**
- Modify: `api/internal/usecase/ports.go`

**Interfaces:**
- Consumes: `domain.Vision` (Task 2).
- Produces: `usecase.VisionRepository`, `usecase.GoalProgressReader`, `usecase.GoalProgress`.

- [ ] **Step 1: Add the two ports**

`ports.go` is the contract between the layers and its doc comments are load-bearing. Append:

```go
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
	// ProgressByIDs returns an entry for each id that exists in THIS
	// household. A missing id is a miss, not an error: a measure whose goal
	// was deleted renders as a label with no figure, and making that an
	// error path would turn an ordinary page render into a failure.
	// Scoped by householdID in SQL, so a goal in another household is
	// indistinguishable from one that does not exist.
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
	// impossible -- BudgetRepo.Upsert is the model.
	//
	// Concurrency, in two cases that must not be collapsed:
	//   v.Version == 0  -- a create. Succeeds only while that household-year
	//                      has no row; reports domain.ErrVisionChanged if one
	//                      appeared since the caller read the empty vision.
	//   v.Version  > 0  -- an update, WHERE version = v.Version. Zero rows
	//                      affected means either the vision was deleted or
	//                      the other partner saved first, and those are
	//                      different answers: re-read to tell them apart and
	//                      report domain.ErrNotFound or
	//                      domain.ErrVisionChanged accordingly.
	//                      RetroRepo.Update's own comment explains why the
	//                      cheap second read is worth it.
	//
	// A measure naming a goal outside this household must be refused with
	// domain.ErrVisionGoalUnknown, checked INSIDE the transaction: the
	// vision_measures FK only proves a goal exists somewhere, never that it
	// is this household's -- the same hole validateLineCategories closes for
	// budget lines.
	Save(ctx context.Context, v domain.Vision) (domain.Vision, error)
}
```

- [ ] **Step 2: Verify it compiles and the arch lint still passes**

```bash
cd api && go build ./... && cd .. && make lint-arch
```
Expected: no output from the build; lint-arch passes.

- [ ] **Step 3: Commit**

```bash
git add api/internal/usecase/ports.go
git commit -m "feat: VisionRepository and the narrow GoalProgressReader port"
```

---

### Task 4: Queries and `VisionRepo.Get`

**Files:**
- Create: `api/internal/adapter/postgres/queries/vision.sql`, `api/internal/adapter/postgres/vision_repo.go`
- Modify: `api/internal/adapter/postgres/vision_repo_test.go`

**Interfaces:**
- Consumes: `usecase.VisionRepository` (Task 3), `domain.Vision` (Task 2).
- Produces: `postgres.NewVisionRepo(db *DB) *VisionRepo`, `(*VisionRepo).Get`.

- [ ] **Step 1: Write the failing test**

Append to `api/internal/adapter/postgres/vision_repo_test.go`:

```go
func TestVisionRepoGetReportsNotFoundForAYearNeverSet(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)

	_, err := repo.Get(context.Background(), householdID, 2026)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want domain.ErrNotFound, got %v", err)
	}
}

func TestVisionRepoGetReadsPillarsMeasuresAndMilestonesInPositionOrder(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)

	var visionID, pillarID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO visions (household_id, year, theme, description)
		 VALUES ($1, 2026, 'Slow down together', 'Fewer commitments.') RETURNING id`,
		householdID).Scan(&visionID); err != nil {
		t.Fatalf("insert vision: %v", err)
	}
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO vision_pillars (vision_id, position, name, description)
		 VALUES ($1, 0, 'Us before logistics', 'Partners first.') RETURNING id`,
		visionID).Scan(&pillarID); err != nil {
		t.Fatalf("insert pillar: %v", err)
	}
	// Inserted out of order on purpose: the ORDER BY is what this asserts.
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO vision_measures (pillar_id, position, label, current_value, target_value)
		 VALUES ($1, 1, 'Weekends away', 2, 4), ($1, 0, 'Date nights / month', 2, 2)`, pillarID); err != nil {
		t.Fatalf("insert measures: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO vision_milestones (vision_id, position, year, title, note)
		 VALUES ($1, 1, 2029, 'Bigger place', ''), ($1, 0, 2027, 'Sabbatical', 'Indonesia')`, visionID); err != nil {
		t.Fatalf("insert milestones: %v", err)
	}

	got, err := repo.Get(ctx, householdID, 2026)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Theme != "Slow down together" || got.Version != 1 {
		t.Fatalf("theme/version wrong: %+v", got)
	}
	if len(got.Pillars) != 1 || len(got.Pillars[0].Measures) != 2 {
		t.Fatalf("want one pillar with two measures, got %+v", got.Pillars)
	}
	if got.Pillars[0].Measures[0].Label != "Date nights / month" {
		t.Fatalf("measures out of position order: %+v", got.Pillars[0].Measures)
	}
	if got.Pillars[0].Measures[0].Kind != domain.MeasureTyped {
		t.Fatalf("want a typed measure, got kind %q", got.Pillars[0].Measures[0].Kind)
	}
	if len(got.Milestones) != 2 || got.Milestones[0].Title != "Sabbatical" {
		t.Fatalf("milestones out of position order: %+v", got.Milestones)
	}
}

func TestVisionRepoGetReadsABrokenLinkAsBroken(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)
	goalID := insertTestGoal(t, db, householdID)

	var visionID, pillarID string
	_ = db.Pool().QueryRow(ctx,
		`INSERT INTO visions (household_id, year, theme) VALUES ($1, 2026, 'T') RETURNING id`,
		householdID).Scan(&visionID)
	_ = db.Pool().QueryRow(ctx,
		`INSERT INTO vision_pillars (vision_id, position, name) VALUES ($1, 0, 'P') RETURNING id`,
		visionID).Scan(&pillarID)
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO vision_measures (pillar_id, position, label, goal_id) VALUES ($1, 0, 'Emergency fund', $2)`,
		pillarID, goalID); err != nil {
		t.Fatalf("insert measure: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `DELETE FROM goals WHERE id = $1`, goalID); err != nil {
		t.Fatalf("delete goal: %v", err)
	}

	got, err := repo.Get(ctx, householdID, 2026)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Pillars[0].Measures[0].Kind != domain.MeasureBroken {
		t.Fatalf("want MeasureBroken after the goal was deleted, got %q", got.Pillars[0].Measures[0].Kind)
	}
}
```

Add `"errors"` and the `domain` import at the top of the file.

- [ ] **Step 2: Run to verify it fails**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestVisionRepoGet -v
```
Expected: FAIL — `undefined: postgres.NewVisionRepo`.

- [ ] **Step 3: Write the queries**

Create `api/internal/adapter/postgres/queries/vision.sql`:

```sql
-- GetVision reads one household-year's parent row. Scoped by household_id:
-- a vision belonging to another household must be indistinguishable from one
-- that does not exist.
-- name: GetVision :one
SELECT id, household_id, year, theme, description, version
FROM visions
WHERE household_id = $1 AND year = $2;

-- ListVisionPillars returns a vision's pillars in the order the household
-- sees them -- the design numbers them visibly ("Pillar 1", "Pillar 2"), so
-- position is not an implementation detail.
-- name: ListVisionPillars :many
SELECT id, position, name, description
FROM vision_pillars
WHERE vision_id = $1
ORDER BY position;

-- ListVisionMeasures returns every measure of every pillar of one vision in a
-- single round trip, joined back to its pillar so the repository can group
-- them without a query per pillar.
-- name: ListVisionMeasures :many
SELECT m.id, m.pillar_id, m.position, m.label, m.current_value, m.target_value, m.goal_id
FROM vision_measures m
JOIN vision_pillars p ON p.id = m.pillar_id
WHERE p.vision_id = $1
ORDER BY p.position, m.position;

-- name: ListVisionMilestones :many
SELECT id, position, year, title, note
FROM vision_milestones
WHERE vision_id = $1
ORDER BY position;

-- CreateVision is the version-0 path: a first save for a household-year.
-- ON CONFLICT DO NOTHING rather than an upsert, so a zero-row result means
-- "someone else created it while this editor was typing" and the repository
-- can answer domain.ErrVisionChanged instead of silently overwriting a whole
-- year of pillars.
--
-- Both parent writes MUST return the post-write version: VisionRepo.Save
-- hands it straight back as the token the next save will send, so a RETURNING
-- list that gave back the version it read would make every subsequent save
-- conflict against itself. Do not trim `version` from either RETURNING.
-- name: CreateVision :one
INSERT INTO visions (household_id, year, theme, description)
VALUES ($1, $2, $3, $4)
ON CONFLICT (household_id, year) DO NOTHING
RETURNING id, household_id, year, theme, description, version;

-- UpdateVision is the version-guarded path. A zero-row result is ambiguous
-- (deleted, or the other partner saved first) and the caller re-reads to tell
-- the two apart -- RetroRepo.Update's own comment gives the reasoning.
-- name: UpdateVision :one
UPDATE visions
SET theme = $3, description = $4, version = version + 1, updated_at = now()
WHERE household_id = $1 AND year = $2 AND version = $5
RETURNING id, household_id, year, theme, description, version;

-- name: DeleteVisionPillars :exec
DELETE FROM vision_pillars WHERE vision_id = $1;

-- name: DeleteVisionMilestones :exec
DELETE FROM vision_milestones WHERE vision_id = $1;

-- name: InsertVisionPillar :one
INSERT INTO vision_pillars (vision_id, position, name, description)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: InsertVisionMeasure :exec
INSERT INTO vision_measures (pillar_id, position, label, current_value, target_value, goal_id)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: InsertVisionMilestone :exec
INSERT INTO vision_milestones (vision_id, position, year, title, note)
VALUES ($1, $2, $3, $4, $5);

-- CountGoalsInHousehold answers how many of the given goal ids actually belong
-- to this household. VisionRepo.Save compares this against the number of
-- distinct ids it asked about, inside the same transaction as the write --
-- vision_measures' own FK only proves a goal exists somewhere, never that it
-- is this household's. The identical hole CountCategoriesInHousehold closes
-- for budget lines.
-- name: CountGoalsInHousehold :one
SELECT count(*) FROM goals
WHERE id = ANY(sqlc.arg(goal_ids)::uuid[]) AND household_id = $1;
```

- [ ] **Step 4: Regenerate sqlc**

```bash
cd api && sqlc generate && go build ./...
```
Expected: `sqlcgen/vision.sql.go` appears; the build succeeds.

- [ ] **Step 5: Write `VisionRepo.Get`**

Create `api/internal/adapter/postgres/vision_repo.go`:

```go
package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// VisionRepo keeps the pool alongside the pool-backed *sqlcgen.Queries, like
// BudgetRepo and GoalRepo: Save replaces a whole document and must begin its
// own transaction, which a *sqlcgen.Queries built once at construction time
// cannot do.
type VisionRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewVisionRepo(db *DB) *VisionRepo {
	return &VisionRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

func (r *VisionRepo) Get(ctx context.Context, householdID string, year int) (domain.Vision, error) {
	row, err := r.q.GetVision(ctx, sqlcgen.GetVisionParams{
		HouseholdID: uuid(householdID),
		Year:        int16(year),
	})
	if err != nil {
		return domain.Vision{}, translate(err, "get vision")
	}

	pillarRows, err := r.q.ListVisionPillars(ctx, row.ID)
	if err != nil {
		return domain.Vision{}, translate(err, "list vision pillars")
	}
	measureRows, err := r.q.ListVisionMeasures(ctx, row.ID)
	if err != nil {
		return domain.Vision{}, translate(err, "list vision measures")
	}
	milestoneRows, err := r.q.ListVisionMilestones(ctx, row.ID)
	if err != nil {
		return domain.Vision{}, translate(err, "list vision milestones")
	}

	// One pass over the measures, grouped by pillar id, rather than a query
	// per pillar: ListVisionMeasures already returns them in (pillar
	// position, measure position) order, so appending in encounter order
	// preserves both orderings without a second sort.
	byPillar := make(map[string][]domain.Measure, len(pillarRows))
	for _, m := range measureRows {
		byPillar[uuidString(m.PillarID)] = append(byPillar[uuidString(m.PillarID)], toMeasure(m))
	}

	pillars := make([]domain.Pillar, 0, len(pillarRows))
	for _, p := range pillarRows {
		pillars = append(pillars, domain.Pillar{
			ID:          uuidString(p.ID),
			Name:        p.Name,
			Description: p.Description,
			Measures:    byPillar[uuidString(p.ID)],
		})
	}

	milestones := make([]domain.Milestone, 0, len(milestoneRows))
	for _, m := range milestoneRows {
		milestones = append(milestones, domain.Milestone{
			ID:    uuidString(m.ID),
			Year:  int(m.Year),
			Title: m.Title,
			Note:  m.Note,
		})
	}

	return domain.Vision{
		ID:          uuidString(row.ID),
		HouseholdID: householdID,
		Year:        int(row.Year),
		Theme:       row.Theme,
		Description: row.Description,
		Version:     int(row.Version),
		Pillars:     pillars,
		Milestones:  milestones,
	}, nil
}

// toMeasure decides which of the three kinds a stored row is. The broken case
// is not defensive programming -- vision_measures' own CHECK permits it
// because ON DELETE SET NULL produces it, so a measure whose goal was deleted
// arrives here with all three value columns null and must be reported as
// MeasureBroken rather than silently read as a typed measure of 0 of 0.
func toMeasure(m sqlcgen.ListVisionMeasuresRow) domain.Measure {
	measure := domain.Measure{
		ID:    uuidString(m.ID),
		Label: m.Label,
	}
	switch {
	case m.GoalID != nil:
		measure.Kind = domain.MeasureLinked
		measure.GoalID = uuidString(*m.GoalID)
	case m.TargetValue != nil && m.CurrentValue != nil:
		measure.Kind = domain.MeasureTyped
		measure.Current = int(*m.CurrentValue)
		measure.Target = int(*m.TargetValue)
	default:
		measure.Kind = domain.MeasureBroken
	}
	return measure
}
```

If `uuidString` does not already exist in `convert.go`, add it there beside `uuid` — check first; the package already converts both directions for other repositories, and this must reuse whatever that helper is actually called.

- [ ] **Step 6: Run the tests to verify they pass**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestVisionRepoGet -v
```
Expected: PASS, all three.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/postgres/queries/vision.sql api/internal/adapter/postgres/vision_repo.go api/internal/adapter/postgres/sqlcgen api/internal/adapter/postgres/vision_repo_test.go
git commit -m "feat: vision queries and VisionRepo.Get"
```

---

### Task 5: `VisionRepo.Save` — the whole-document replace

**Files:**
- Modify: `api/internal/adapter/postgres/vision_repo.go`, `api/internal/adapter/postgres/vision_repo_test.go`

**Interfaces:**
- Consumes: Task 4's queries.
- Produces: `(*VisionRepo).Save(ctx, domain.Vision) (domain.Vision, error)`.

- [ ] **Step 1: Write the failing tests**

Append to `vision_repo_test.go`. The atomicity test's fault is deliberate and named — an out-of-range milestone year submitted *behind* valid pillars and measures, so the failure lands after several successful inserts. Without an injected fault the test cannot fail and therefore proves nothing.

```go
func TestVisionSaveCreatesAtVersionZeroAndRefusesASecondCreate(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)

	draft := domain.Vision{HouseholdID: householdID, Year: 2026, Theme: "Slow down together", Version: 0}
	saved, err := repo.Save(ctx, draft)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if saved.Version != 1 {
		t.Fatalf("a create must land at version 1, got %d", saved.Version)
	}

	// The first-save race: both partners read the empty vision, both hold 0.
	_, err = repo.Save(ctx, draft)
	if !errors.Is(err, domain.ErrVisionChanged) {
		t.Fatalf("want ErrVisionChanged for a second version-0 save, got %v", err)
	}
}

func TestVisionSaveRefusesAStaleVersion(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)

	first, err := repo.Save(ctx, domain.Vision{HouseholdID: householdID, Year: 2026, Theme: "A"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	second, err := repo.Save(ctx, domain.Vision{HouseholdID: householdID, Year: 2026, Theme: "B", Version: first.Version})
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	// The returned version must be the POST-write one, because it is the token
	// the next save sends. A query whose RETURNING gave back the version it
	// read would make every subsequent save conflict against itself.
	if second.Version != first.Version+1 {
		t.Fatalf("want the incremented version back, got %d after %d", second.Version, first.Version)
	}
	// first.Version is now stale.
	_, err = repo.Save(ctx, domain.Vision{HouseholdID: householdID, Year: 2026, Theme: "C", Version: first.Version})
	if !errors.Is(err, domain.ErrVisionChanged) {
		t.Fatalf("want ErrVisionChanged, got %v", err)
	}
}

func TestVisionSaveIsOneTransaction(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	householdID := insertTestHousehold(t, db)

	good := domain.Vision{
		HouseholdID: householdID, Year: 2026, Theme: "Slow down together",
		Pillars: []domain.Pillar{{Name: "Us before logistics", Measures: []domain.Measure{
			{Label: "Date nights / month", Kind: domain.MeasureTyped, Current: 2, Target: 2},
		}}},
	}
	saved, err := repo.Save(ctx, good)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	// The injected fault: a milestone year outside vision_milestones' own
	// CHECK range, submitted AFTER a valid pillar and measure have already
	// been inserted in this transaction.
	bad := good
	bad.Version = saved.Version
	bad.Theme = "Overwritten"
	bad.Pillars = []domain.Pillar{{Name: "Replaced", Measures: nil}}
	bad.Milestones = []domain.Milestone{{Year: 9999, Title: "Out of range"}}
	if _, err := repo.Save(ctx, bad); err == nil {
		t.Fatal("expected the out-of-range milestone year to fail the save")
	}

	after, err := repo.Get(ctx, householdID, 2026)
	if err != nil {
		t.Fatalf("get after the failed save: %v", err)
	}
	if after.Theme != "Slow down together" {
		t.Fatalf("the parent row was not rolled back: theme is %q", after.Theme)
	}
	if len(after.Pillars) != 1 || after.Pillars[0].Name != "Us before logistics" {
		t.Fatalf("children were not rolled back: %+v", after.Pillars)
	}
	if len(after.Pillars[0].Measures) != 1 {
		t.Fatalf("measures were not rolled back: %+v", after.Pillars[0].Measures)
	}
}

func TestVisionSaveRefusesAGoalFromAnotherHousehold(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewVisionRepo(db)
	mine := insertTestHousehold(t, db)
	theirs := insertTestHousehold(t, db)
	theirGoal := insertTestGoal(t, db, theirs)

	_, err := repo.Save(ctx, domain.Vision{
		HouseholdID: mine, Year: 2026, Theme: "T",
		Pillars: []domain.Pillar{{Name: "P", Measures: []domain.Measure{
			{Label: "Emergency fund", Kind: domain.MeasureLinked, GoalID: theirGoal},
		}}},
	})
	if !errors.Is(err, domain.ErrVisionGoalUnknown) {
		t.Fatalf("want ErrVisionGoalUnknown, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestVisionSave -v
```
Expected: FAIL — `repo.Save undefined`.

- [ ] **Step 3: Write `Save`**

Append to `vision_repo.go`:

```go
// Save replaces one household-year's vision wholesale, inside one
// transaction: create or version-check the parent, verify every linked goal
// belongs to this household, delete every child, then insert the submitted
// ones. Any failure rolls the whole thing back via pgx.BeginFunc, so a bad
// milestone can never leave the parent updated with its pillars half
// replaced (TestVisionSaveIsOneTransaction). BudgetRepo.Upsert is the model.
func (r *VisionRepo) Save(ctx context.Context, v domain.Vision) (domain.Vision, error) {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		row, err := r.upsertParent(ctx, q, v)
		if err != nil {
			return err
		}

		if err := validateMeasureGoals(ctx, q, v); err != nil {
			return err
		}

		// Full replace, never merge (the port's own doc comment). Deleting
		// the pillars cascades to their measures, so there is no third
		// delete: vision_measures.pillar_id is ON DELETE CASCADE.
		if err := q.DeleteVisionPillars(ctx, row.ID); err != nil {
			return translate(err, "delete vision pillars")
		}
		if err := q.DeleteVisionMilestones(ctx, row.ID); err != nil {
			return translate(err, "delete vision milestones")
		}

		for i, p := range v.Pillars {
			pillarID, err := q.InsertVisionPillar(ctx, sqlcgen.InsertVisionPillarParams{
				VisionID:    row.ID,
				Position:    int16(i),
				Name:        p.Name,
				Description: p.Description,
			})
			if err != nil {
				return translate(err, "insert vision pillar")
			}
			for j, m := range p.Measures {
				params := sqlcgen.InsertVisionMeasureParams{
					PillarID: pillarID,
					Position: int16(j),
					Label:    m.Label,
				}
				// Fail closed: Kind reached here through the domain's own
				// Validate, but this switch still refuses anything it does
				// not recognise rather than writing a row that satisfies no
				// branch of measure_is_typed_or_linked.
				switch m.Kind {
				case domain.MeasureTyped:
					current, target := int32(m.Current), int32(m.Target)
					params.CurrentValue, params.TargetValue = &current, &target
				case domain.MeasureLinked:
					goalID := uuid(m.GoalID)
					params.GoalID = &goalID
				default:
					return domain.ErrVisionMeasureAmbiguous
				}
				if err := q.InsertVisionMeasure(ctx, params); err != nil {
					return translate(err, "insert vision measure")
				}
			}
		}

		for i, m := range v.Milestones {
			if err := q.InsertVisionMilestone(ctx, sqlcgen.InsertVisionMilestoneParams{
				VisionID: row.ID,
				Position: int16(i),
				Year:     int16(m.Year),
				Title:    m.Title,
				Note:     m.Note,
			}); err != nil {
				return translate(err, "insert vision milestone")
			}
		}
		return nil
	})
	if err != nil {
		return domain.Vision{}, err
	}

	// Read back rather than returning the draft. The replace above DELETED and
	// reinserted every pillar, measure and milestone, so the ids the caller
	// sent name rows that no longer exist -- returning the draft would hand
	// back a document whose child ids are all stale. Nothing reads them today
	// (MeasureView carries no id), which is exactly why this would sit
	// unnoticed until the change the spec's decision 5 anticipates: the day
	// something references a measure, stable ids arrive, and a Save that had
	// been quietly lying about them would spring on that change rather than
	// on this one.
	return r.Get(ctx, v.HouseholdID, v.Year)
}

// upsertParent is the whole of the concurrency contract, and the two branches
// are genuinely different operations rather than one upsert with a flag.
func (r *VisionRepo) upsertParent(ctx context.Context, q *sqlcgen.Queries, v domain.Vision) (sqlcgen.Vision, error) {
	if v.Version == 0 {
		// A create. CreateVision is ON CONFLICT DO NOTHING, so pgx.ErrNoRows
		// here means the row appeared while this editor was typing -- the
		// first-save race two owners hit in January, when both read the empty
		// vision and both hold version 0.
		row, err := q.CreateVision(ctx, sqlcgen.CreateVisionParams{
			HouseholdID: uuid(v.HouseholdID),
			Year:        int16(v.Year),
			Theme:       v.Theme,
			Description: v.Description,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlcgen.Vision{}, domain.ErrVisionChanged
		}
		if err != nil {
			return sqlcgen.Vision{}, translate(err, "create vision")
		}
		return sqlcgen.Vision(row), nil
	}

	version, ok := versionParam(v.Version)
	if !ok {
		// A version outside int32's range can never be the stored one -- the
		// column is a Postgres integer, so every real value already fits.
		// Refusing here is what stops a silent truncation matching some other
		// row's version.
		return sqlcgen.Vision{}, domain.ErrVisionChanged
	}
	row, err := q.UpdateVision(ctx, sqlcgen.UpdateVisionParams{
		HouseholdID: uuid(v.HouseholdID),
		Year:        int16(v.Year),
		Theme:       v.Theme,
		Description: v.Description,
		Version:     version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Zero rows is ambiguous: deleted, or the other partner saved first.
		// One cheap read tells them apart, because a deleted vision must read
		// back as ErrNotFound and never as "reload and try again" --
		// RetroRepo.Update's own comment.
		if _, getErr := r.Get(ctx, v.HouseholdID, v.Year); errors.Is(getErr, domain.ErrNotFound) {
			return sqlcgen.Vision{}, domain.ErrNotFound
		}
		return sqlcgen.Vision{}, domain.ErrVisionChanged
	}
	if err != nil {
		return sqlcgen.Vision{}, translate(err, "update vision")
	}
	return sqlcgen.Vision(row), nil
}

// validateMeasureGoals refuses a measure naming a goal outside this
// household, inside the same transaction as the write. vision_measures' FK
// only proves a goal exists somewhere -- the identical hole
// validateLineCategories closes for budget lines.
func validateMeasureGoals(ctx context.Context, q *sqlcgen.Queries, v domain.Vision) error {
	seen := map[string]struct{}{}
	var ids []pgtype.UUID
	for _, p := range v.Pillars {
		for _, m := range p.Measures {
			if m.Kind != domain.MeasureLinked || m.GoalID == "" {
				continue
			}
			if _, dup := seen[m.GoalID]; dup {
				continue
			}
			seen[m.GoalID] = struct{}{}
			ids = append(ids, uuid(m.GoalID))
		}
	}
	if len(ids) == 0 {
		return nil
	}
	count, err := q.CountGoalsInHousehold(ctx, sqlcgen.CountGoalsInHouseholdParams{
		HouseholdID: uuid(v.HouseholdID),
		GoalIds:     ids,
	})
	if err != nil {
		return translate(err, "count goals in household")
	}
	if int(count) != len(ids) {
		return domain.ErrVisionGoalUnknown
	}
	return nil
}
```

Add the imports this needs: `errors`, `github.com/jackc/pgx/v5`, `github.com/jackc/pgx/v5/pgtype`. Reuse `versionParam` from `retro_repo.go` — if it is unexported and lives in that file, it is already in the same package, so call it directly. The `sqlcgen.Vision(row)` conversions assume `CreateVision`/`UpdateVision` return identically-shaped structs; if sqlc names them `CreateVisionRow`/`UpdateVisionRow` with different field sets, convert field by field instead of with a type conversion.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestVisionSave -v
```
Expected: PASS, all four.

- [ ] **Step 5: Confirm the one assumption the concurrency story rests on**

`CreateVision` is a sqlc `:one` over `ON CONFLICT DO NOTHING ... RETURNING`, and `upsertParent` relies on the zero-row conflict surfacing as `pgx.ErrNoRows`. That is what a sqlc `:one` does over pgx's `QueryRow`, but it is the single load-bearing assumption in the whole guard: if it comes back as some other error, a second version-0 save falls through to `translate` and the first partner's January is overwritten.

`TestVisionSaveCreatesAtVersionZeroAndRefusesASecondCreate` already exercises the path. If it fails with an unexpected error rather than `ErrVisionChanged`, **widen the branch to match the real error — do not weaken the test.**

- [ ] **Step 6: Mutation-check the transaction**

Replace `pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {` with a plain non-transactional path (use `r.q` instead of `q := r.q.WithTx(tx)`), re-run, and confirm `TestVisionSaveIsOneTransaction` fails — the theme should read "Overwritten" and the pillars should be gone. Restore. This is the one test whose entire value is the failure path.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/postgres/vision_repo.go api/internal/adapter/postgres/vision_repo_test.go
git commit -m "feat: VisionRepo.Save, one transaction and a version guard"
```

---

### Task 6: `GoalRepo.ProgressByIDs`

**Files:**
- Modify: `api/internal/adapter/postgres/queries/goal.sql`, `api/internal/adapter/postgres/goal_repo.go`, `api/internal/adapter/postgres/goal_repo_test.go`

**Interfaces:**
- Consumes: `usecase.GoalProgressReader` (Task 3).
- Produces: `(*GoalRepo).ProgressByIDs(ctx, householdID string, goalIDs []string) (map[string]usecase.GoalProgress, error)`.

- [ ] **Step 1: Write the failing test**

Append to `api/internal/adapter/postgres/goal_repo_test.go`:

```go
func TestGoalProgressByIDsReturnsOnlyThisHouseholdsGoals(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := postgres.NewGoalRepo(db)
	mine := insertTestHousehold(t, db)
	theirs := insertTestHousehold(t, db)
	myGoal := insertTestGoal(t, db, mine)
	theirGoal := insertTestGoal(t, db, theirs)

	got, err := repo.ProgressByIDs(ctx, mine, []string{myGoal, theirGoal, myGoal})
	if err != nil {
		t.Fatalf("progress by ids: %v", err)
	}
	if _, ok := got[theirGoal]; ok {
		t.Fatal("another household's goal must not appear")
	}
	progress, ok := got[myGoal]
	if !ok {
		t.Fatalf("want this household's goal, got %v", got)
	}
	if progress.Name != "Emergency fund" {
		t.Fatalf("want the goal's name, got %q", progress.Name)
	}
	// insertTestGoal writes no contributions, so nothing has been saved yet.
	if progress.Percent != 0 {
		t.Fatalf("want 0%%, got %d", progress.Percent)
	}
}

func TestGoalProgressByIDsIsAMissNotAnErrorForAnUnknownID(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.NewGoalRepo(db)
	householdID := insertTestHousehold(t, db)

	got, err := repo.ProgressByIDs(context.Background(), householdID,
		[]string{"00000000-0000-0000-0000-000000000000"})
	if err != nil {
		t.Fatalf("an unknown id must be a miss, not an error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want an empty map, got %v", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestGoalProgressByIDs -v
```
Expected: FAIL — `repo.ProgressByIDs undefined`.

- [ ] **Step 3: Add the query**

Append to `api/internal/adapter/postgres/queries/goal.sql`. Read `ListGoalsWithTotals` in that file first and reuse its contributed-total expression verbatim — a second, subtly different total is exactly the drift this whole feature avoids elsewhere.

```sql
-- GoalProgressByIDs is Vision's one read of Goals (usecase.GoalProgressReader).
-- It returns a row only for an id that belongs to THIS household, so a miss is
-- a miss rather than a leak, and Vision renders a figureless measure for it.
-- The contributed total mirrors ListGoalsWithTotals' own -- there must not be
-- two different definitions of how far along a goal is.
-- name: GoalProgressByIDs :many
SELECT g.id, g.name, g.target_amount_minor,
       COALESCE(SUM(c.amount_minor), 0)::bigint AS contributed_minor
FROM goals g
LEFT JOIN goal_contributions c ON c.goal_id = g.id AND c.household_id = g.household_id
WHERE g.household_id = $1 AND g.id = ANY(sqlc.arg(goal_ids)::uuid[])
GROUP BY g.id, g.name, g.target_amount_minor;
```

- [ ] **Step 4: Regenerate sqlc and implement the method**

```bash
cd api && sqlc generate
```

Append to `goal_repo.go`:

```go
// ProgressByIDs implements usecase.GoalProgressReader -- the one thing Vision
// needs from Goals. It deliberately returns no entry for an id it did not
// find, rather than an error: a measure whose goal was deleted renders as a
// label with no figure, and making that an error would turn an ordinary page
// render into a failure. Percent is domain.GoalProgressPercent's own capped
// figure, the same one GoalService.List puts on a goal card.
func (r *GoalRepo) ProgressByIDs(ctx context.Context, householdID string, goalIDs []string) (map[string]usecase.GoalProgress, error) {
	if len(goalIDs) == 0 {
		return map[string]usecase.GoalProgress{}, nil
	}
	ids := make([]pgtype.UUID, 0, len(goalIDs))
	for _, id := range goalIDs {
		ids = append(ids, uuid(id))
	}
	rows, err := r.q.GoalProgressByIDs(ctx, sqlcgen.GoalProgressByIDsParams{
		HouseholdID: uuid(householdID),
		GoalIds:     ids,
	})
	if err != nil {
		return nil, translate(err, "goal progress by ids")
	}
	out := make(map[string]usecase.GoalProgress, len(rows))
	for _, row := range rows {
		id := uuidString(row.ID)
		out[id] = usecase.GoalProgress{
			GoalID:  id,
			Name:    row.Name,
			Percent: domain.GoalProgressPercent(row.ContributedMinor, row.TargetAmountMinor),
		}
	}
	return out, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestGoalProgress -v
```
Expected: PASS, both.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/postgres/queries/goal.sql api/internal/adapter/postgres/goal_repo.go api/internal/adapter/postgres/goal_repo_test.go api/internal/adapter/postgres/sqlcgen
git commit -m "feat: GoalRepo.ProgressByIDs, the one read Vision makes of Goals"
```

---

### Task 7: `VisionService.Get`

**Files:**
- Create: `api/internal/usecase/vision.go`, `api/internal/usecase/vision_test.go`

**Interfaces:**
- Consumes: `VisionRepository`, `GoalProgressReader`, `Clock` (Task 3).
- Produces: `usecase.VisionService`, `NewVisionService(visions VisionRepository, goals GoalProgressReader, clock Clock) *VisionService`, `(*VisionService).Get(ctx, householdID string, year int) (VisionView, error)`, `(*VisionService).CurrentYear() int`, and the view types `VisionView`, `PillarView`, `MeasureView`.

- [ ] **Step 1: Write the failing tests**

Create `api/internal/usecase/vision_test.go`:

```go
package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestVisionGetReturnsAnEmptyVisionForAYearNeverSet(t *testing.T) {
	svc := usecase.NewVisionService(&fakeVisionRepo{}, &fakeGoalProgress{}, fixedClock{at: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)})

	got, err := svc.Get(context.Background(), "h1", 2026)
	if err != nil {
		t.Fatalf("a year never set must not be an error: %v", err)
	}
	if got.Version != 0 {
		t.Fatalf("an unset year must carry version 0 -- it is what tells a save it is a create -- got %d", got.Version)
	}
	if got.Theme != "" || len(got.Pillars) != 0 || len(got.Milestones) != 0 {
		t.Fatalf("want a blank vision, got %+v", got)
	}
	if got.Year != 2026 {
		t.Fatalf("want the requested year echoed back, got %d", got.Year)
	}
}

func TestVisionGetResolvesALinkedMeasure(t *testing.T) {
	repo := &fakeVisionRepo{vision: domain.Vision{
		Year: 2026, Theme: "T", Version: 3,
		Pillars: []domain.Pillar{{Name: "Money without fear", Measures: []domain.Measure{
			{Label: "Emergency fund", Kind: domain.MeasureLinked, GoalID: "g1"},
		}}},
	}}
	goals := &fakeGoalProgress{progress: map[string]usecase.GoalProgress{
		"g1": {GoalID: "g1", Name: "Emergency fund", Percent: 62},
	}}
	svc := usecase.NewVisionService(repo, goals, fixedClock{at: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)})

	got, err := svc.Get(context.Background(), "h1", 2026)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	m := got.Pillars[0].Measures[0]
	if !m.HasFigure || m.Percent != 62 {
		t.Fatalf("want a resolved 62%% figure, got %+v", m)
	}
	if m.Met != (m.Percent >= 100) {
		t.Fatalf("Met must be percent >= 100 for a linked measure, got %+v", m)
	}
}

func TestVisionGetRendersNoFigureWhenTheLinkedGoalIsGone(t *testing.T) {
	repo := &fakeVisionRepo{vision: domain.Vision{
		Year: 2026, Theme: "T", Version: 3,
		Pillars: []domain.Pillar{{Name: "P", Measures: []domain.Measure{
			// A link the repository still has, whose goal the reader cannot find.
			{Label: "Emergency fund", Kind: domain.MeasureLinked, GoalID: "gone"},
			// And the broken shape ON DELETE SET NULL leaves behind.
			{Label: "Old target", Kind: domain.MeasureBroken},
		}}},
	}}
	svc := usecase.NewVisionService(repo, &fakeGoalProgress{}, fixedClock{at: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)})

	got, err := svc.Get(context.Background(), "h1", 2026)
	if err != nil {
		t.Fatalf("a missing goal must not fail the page: %v", err)
	}
	for i, m := range got.Pillars[0].Measures {
		if m.HasFigure {
			t.Fatalf("measure %d must render no figure, got %+v", i, m)
		}
		if m.Percent != 0 || m.Current != 0 || m.Target != 0 {
			t.Fatalf("measure %d must carry no numbers at all -- never a zero standing in for a figure: %+v", i, m)
		}
		if m.Label == "" {
			t.Fatalf("measure %d must keep its label", i)
		}
	}
}

func TestVisionGetMarksATypedMeasureMetAtTarget(t *testing.T) {
	repo := &fakeVisionRepo{vision: domain.Vision{
		Year: 2026, Theme: "T", Version: 1,
		Pillars: []domain.Pillar{{Name: "P", Measures: []domain.Measure{
			{Label: "Date nights / month", Kind: domain.MeasureTyped, Current: 2, Target: 2},
			{Label: "Phone-free dinners / week", Kind: domain.MeasureTyped, Current: 3, Target: 5},
		}}},
	}}
	svc := usecase.NewVisionService(repo, &fakeGoalProgress{}, fixedClock{at: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)})

	got, _ := svc.Get(context.Background(), "h1", 2026)
	if !got.Pillars[0].Measures[0].Met {
		t.Fatal("2 of 2 must be met")
	}
	if got.Pillars[0].Measures[1].Met {
		t.Fatal("3 of 5 must not be met")
	}
}
```

Add the doubles at the bottom of the same file — `testdouble_test.go` in this package is where the existing ones live, so check there first and follow its style rather than duplicating a clock:

```go
type fakeVisionRepo struct {
	vision domain.Vision
	found  bool
	saved  domain.Vision
	saveErr error
}

func (f *fakeVisionRepo) Get(_ context.Context, _ string, year int) (domain.Vision, error) {
	if f.vision.Theme == "" && !f.found {
		return domain.Vision{}, domain.ErrNotFound
	}
	v := f.vision
	v.Year = year
	return v, nil
}

func (f *fakeVisionRepo) Save(_ context.Context, v domain.Vision) (domain.Vision, error) {
	if f.saveErr != nil {
		return domain.Vision{}, f.saveErr
	}
	f.saved = v
	v.Version++
	return v, nil
}

type fakeGoalProgress struct {
	progress map[string]usecase.GoalProgress
}

func (f *fakeGoalProgress) ProgressByIDs(_ context.Context, _ string, _ []string) (map[string]usecase.GoalProgress, error) {
	if f.progress == nil {
		return map[string]usecase.GoalProgress{}, nil
	}
	return f.progress, nil
}
```

If `fixedClock` does not already exist in this package's test files, add one with a single `Now()` returning its `at`.

- [ ] **Step 2: Run to verify they fail**

```bash
cd api && go test ./internal/usecase/ -run TestVisionGet -v
```
Expected: FAIL — `undefined: usecase.NewVisionService`.

- [ ] **Step 3: Write the service and its view types**

Create `api/internal/usecase/vision.go`:

```go
package usecase

import (
	"context"
	"errors"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// MeasureView is one line under a pillar, with every figure the screen shows
// already decided. HasFigure false is the whole of the broken-link contract:
// the label renders with a short explanation and NO number, because a zero
// would be a claim -- the same rule Accounts applies when a primary-currency
// change leaves net worth uncomputable.
type MeasureView struct {
	Label     string
	Kind      domain.MeasureKind
	HasFigure bool
	Current   int // typed only
	Target    int // typed only
	Percent   int // linked only
	Met       bool
	GoalID    string
	GoalName  string
}

type PillarView struct {
	Name        string
	Description string
	Measures    []MeasureView
}

// VisionView is the whole screen in one response. Version travels with it
// because every save must send back the version it read -- 0 for a year that
// had none, which is what makes the first save a create rather than a blind
// overwrite.
type VisionView struct {
	Year        int
	Theme       string
	Description string
	Version     int
	Pillars     []PillarView
	Milestones  []domain.Milestone
}

// VisionService composes the Vision screen and its one write.
type VisionService struct {
	visions VisionRepository
	goals   GoalProgressReader
	clock   Clock
}

func NewVisionService(visions VisionRepository, goals GoalProgressReader, clock Clock) *VisionService {
	return &VisionService{visions: visions, goals: goals, clock: clock}
}

// CurrentYear is the default the handler uses when a request names no year.
// It lives here rather than in the handler so nothing in the HTTP layer
// computes a year of its own.
func (s *VisionService) CurrentYear() int {
	return s.clock.Now().Year()
}

func (s *VisionService) Get(ctx context.Context, householdID string, year int) (VisionView, error) {
	v, err := s.visions.Get(ctx, householdID, year)
	if errors.Is(err, domain.ErrNotFound) {
		// A year never set is not an error -- the page's empty state is a
		// render, not an error branch (spec decision 9). Version 0 is what
		// tells the next save it is a create.
		return VisionView{Year: year, Version: 0, Pillars: []PillarView{}, Milestones: []domain.Milestone{}}, nil
	}
	if err != nil {
		return VisionView{}, err
	}
	return s.compose(ctx, householdID, v)
}

func (s *VisionService) compose(ctx context.Context, householdID string, v domain.Vision) (VisionView, error) {
	progress, err := s.goals.ProgressByIDs(ctx, householdID, linkedGoalIDs(v))
	if err != nil {
		return VisionView{}, err
	}

	pillars := make([]PillarView, 0, len(v.Pillars))
	for _, p := range v.Pillars {
		measures := make([]MeasureView, 0, len(p.Measures))
		for _, m := range p.Measures {
			measures = append(measures, toMeasureView(m, progress))
		}
		pillars = append(pillars, PillarView{Name: p.Name, Description: p.Description, Measures: measures})
	}

	milestones := v.Milestones
	if milestones == nil {
		milestones = []domain.Milestone{}
	}
	return VisionView{
		Year: v.Year, Theme: v.Theme, Description: v.Description,
		Version: v.Version, Pillars: pillars, Milestones: milestones,
	}, nil
}

func linkedGoalIDs(v domain.Vision) []string {
	var ids []string
	for _, p := range v.Pillars {
		for _, m := range p.Measures {
			if m.Kind == domain.MeasureLinked && m.GoalID != "" {
				ids = append(ids, m.GoalID)
			}
		}
	}
	return ids
}

func toMeasureView(m domain.Measure, progress map[string]GoalProgress) MeasureView {
	view := MeasureView{Label: m.Label, Kind: m.Kind}
	// Fail closed: Kind arrives from a database column, so an unrecognised
	// one renders as a figureless measure rather than as a typed 0 of 0.
	switch m.Kind {
	case domain.MeasureTyped:
		view.HasFigure = true
		view.Current, view.Target = m.Current, m.Target
		view.Met = m.Current >= m.Target
	case domain.MeasureLinked:
		view.GoalID = m.GoalID
		found, ok := progress[m.GoalID]
		if !ok {
			// The goal was deleted between the vision being saved and this
			// read, or the link was broken. Label only.
			return view
		}
		view.HasFigure = true
		view.Percent = found.Percent
		view.GoalName = found.Name
		view.Met = found.Percent >= 100
	}
	return view
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd api && go test ./internal/usecase/ -run TestVisionGet -v
```
Expected: PASS, all four.

- [ ] **Step 5: Mutation-check the broken-link path**

In `toMeasureView`'s `MeasureLinked` branch, change the `if !ok` early return to fall through and set `view.HasFigure = true`. Re-run: `TestVisionGetRendersNoFigureWhenTheLinkedGoalIsGone` must fail. Restore.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/vision.go api/internal/usecase/vision_test.go
git commit -m "feat: VisionService.Get, resolving linked measures and failing closed on a missing goal"
```

---

### Task 8: `VisionService.Save`

**Files:**
- Modify: `api/internal/usecase/vision.go`, `api/internal/usecase/vision_test.go`

**Interfaces:**
- Consumes: Task 7's service.
- Produces: `(*VisionService).Save(ctx, householdID string, year int, draft domain.Vision) (VisionView, error)`.

- [ ] **Step 1: Write the failing tests**

Append to `api/internal/usecase/vision_test.go`:

```go
func TestVisionSaveValidatesBeforeTouchingTheRepository(t *testing.T) {
	repo := &fakeVisionRepo{}
	svc := usecase.NewVisionService(repo, &fakeGoalProgress{}, fixedClock{at: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)})

	_, err := svc.Save(context.Background(), "h1", 2026, domain.Vision{Theme: "   "})
	if !errors.Is(err, domain.ErrVisionThemeRequired) {
		t.Fatalf("want ErrVisionThemeRequired, got %v", err)
	}
	if repo.saved.Theme != "" {
		t.Fatal("an invalid draft must never reach the repository")
	}
}

func TestVisionSaveOverwritesHouseholdAndYearFromTheRoute(t *testing.T) {
	repo := &fakeVisionRepo{found: true}
	svc := usecase.NewVisionService(repo, &fakeGoalProgress{}, fixedClock{at: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)})

	// A body claiming a different household and year. The route's values win:
	// a request body must never be able to write into someone else's
	// household by naming it.
	_, err := svc.Save(context.Background(), "h1", 2026, domain.Vision{
		HouseholdID: "someone-else", Year: 1999, Theme: "Slow down together", Version: 2,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if repo.saved.HouseholdID != "h1" || repo.saved.Year != 2026 {
		t.Fatalf("route values must win, got household %q year %d", repo.saved.HouseholdID, repo.saved.Year)
	}
}

func TestVisionSaveReturnsTheComposedViewWithTheNewVersion(t *testing.T) {
	repo := &fakeVisionRepo{found: true}
	svc := usecase.NewVisionService(repo, &fakeGoalProgress{}, fixedClock{at: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)})

	got, err := svc.Save(context.Background(), "h1", 2026, domain.Vision{Theme: "Slow down together", Version: 4})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if got.Version != 5 {
		t.Fatalf("the response must carry the version the next save will send, got %d", got.Version)
	}
	if got.Theme != "Slow down together" {
		t.Fatalf("want the saved theme back, got %q", got.Theme)
	}
}

func TestVisionSavePassesAConflictThrough(t *testing.T) {
	repo := &fakeVisionRepo{found: true, saveErr: domain.ErrVisionChanged}
	svc := usecase.NewVisionService(repo, &fakeGoalProgress{}, fixedClock{at: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)})

	_, err := svc.Save(context.Background(), "h1", 2026, domain.Vision{Theme: "T", Version: 1})
	if !errors.Is(err, domain.ErrVisionChanged) {
		t.Fatalf("want ErrVisionChanged, got %v", err)
	}
}
```

Add `"errors"` to the imports.

- [ ] **Step 2: Run to verify they fail**

```bash
cd api && go test ./internal/usecase/ -run TestVisionSave -v
```
Expected: FAIL — `svc.Save undefined`.

- [ ] **Step 3: Write `Save`**

Append to `api/internal/usecase/vision.go`:

```go
// Save validates the draft, then replaces the whole document. The household
// and year come from the caller (the route), never from the body: a request
// that names another household must not be able to write into it, and the
// service is where that is settled rather than in each handler.
func (s *VisionService) Save(ctx context.Context, householdID string, year int, draft domain.Vision) (VisionView, error) {
	draft.HouseholdID = householdID
	draft.Year = year

	if err := draft.Validate(); err != nil {
		return VisionView{}, err
	}

	saved, err := s.visions.Save(ctx, draft)
	if err != nil {
		return VisionView{}, err
	}
	return s.compose(ctx, householdID, saved)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd api && go test ./internal/usecase/ -run TestVision -v
```
Expected: PASS, every Vision test.

- [ ] **Step 5: Mutation-check the route-wins rule**

Delete the two `draft.HouseholdID = householdID` / `draft.Year = year` lines. `TestVisionSaveOverwritesHouseholdAndYearFromTheRoute` must fail. Restore.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/vision.go api/internal/usecase/vision_test.go
git commit -m "feat: VisionService.Save"
```

---

### Task 9: HTTP handlers, routes and wiring

**Files:**
- Create: `api/internal/adapter/http/vision_handlers.go`, `api/internal/adapter/http/vision_api_test.go`
- Modify: `api/internal/adapter/http/errors.go`, `api/internal/adapter/http/router.go`, `api/cmd/api/main.go`

**Interfaces:**
- Consumes: `usecase.VisionService` (Tasks 7–8).
- Produces: `GET /api/v1/marriage/vision?year=YYYY`, `PUT /api/v1/marriage/vision/{year}`, and the JSON shape `visionSchemas.ts` mirrors in Task 10.

- [ ] **Step 1: Write the failing route tests**

Create `api/internal/adapter/http/vision_api_test.go`, following `marriage_api_test.go`'s existing harness (read it first — it already builds a router with a seeded household and a session cookie; reuse those helpers rather than inventing new ones):

```go
package httpadapter_test

import (
	"net/http"
	"testing"
)

func TestGetVisionReturnsAnEmptyDocumentForAYearNeverSet(t *testing.T) {
	env := newMarriageTestEnv(t)
	resp := env.ownerRequest(t, http.MethodGet, "/api/v1/marriage/vision?year=2026", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("want 200 for a year never set, never 404; got %d", resp.Code)
	}
	body := decodeVision(t, resp)
	if body.Vision.Version != 0 {
		t.Fatalf("an unset year must carry version 0, got %d", body.Vision.Version)
	}
}

func TestPutVisionWithoutACSRFTokenIsRefused(t *testing.T) {
	env := newMarriageTestEnv(t)
	resp := env.ownerRequestWithoutCSRF(t, http.MethodPut, "/api/v1/marriage/vision/2026",
		`{"version":0,"theme":"Slow down together","description":"","pillars":[],"milestones":[]}`)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("want 403 without a CSRF token, got %d", resp.Code)
	}
}

func TestPutVisionAsALimitedMemberIsRefused(t *testing.T) {
	env := newMarriageTestEnv(t)
	resp := env.limitedMemberRequest(t, http.MethodPut, "/api/v1/marriage/vision/2026",
		`{"version":0,"theme":"T","description":"","pillars":[],"milestones":[]}`)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("want 403 for a member without the marriage capability, got %d", resp.Code)
	}
}

func TestPutVisionWithAStaleVersionIsAConflict(t *testing.T) {
	env := newMarriageTestEnv(t)
	first := env.ownerRequest(t, http.MethodPut, "/api/v1/marriage/vision/2026",
		`{"version":0,"theme":"Slow down together","description":"","pillars":[],"milestones":[]}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first save: want 200, got %d", first.Code)
	}
	second := env.ownerRequest(t, http.MethodPut, "/api/v1/marriage/vision/2026",
		`{"version":0,"theme":"Overwritten","description":"","pillars":[],"milestones":[]}`)
	if second.Code != http.StatusConflict {
		t.Fatalf("a second version-0 save must be 409, got %d", second.Code)
	}
}

func TestPutVisionRoundTripsPillarsAndMilestones(t *testing.T) {
	env := newMarriageTestEnv(t)
	resp := env.ownerRequest(t, http.MethodPut, "/api/v1/marriage/vision/2026", `{
		"version":0,
		"theme":"Slow down together",
		"description":"Fewer commitments, more presence.",
		"pillars":[{"name":"Us before logistics","description":"Partners first.",
			"measures":[{"label":"Date nights / month","kind":"typed","current":2,"target":2}]}],
		"milestones":[{"year":2027,"title":"Sabbatical","note":"Indonesia"}]
	}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.Code, resp.Body.String())
	}
	body := decodeVision(t, resp)
	if body.Vision.Version != 1 {
		t.Fatalf("a create must come back at version 1, got %d", body.Vision.Version)
	}
	if len(body.Vision.Pillars) != 1 || len(body.Vision.Pillars[0].Measures) != 1 {
		t.Fatalf("pillars did not round-trip: %+v", body.Vision.Pillars)
	}
	if !body.Vision.Pillars[0].Measures[0].Met {
		t.Fatal("2 of 2 must come back met")
	}
	if len(body.Vision.Milestones) != 1 {
		t.Fatalf("milestones did not round-trip: %+v", body.Vision.Milestones)
	}
}
```

Write `decodeVision` beside these, decoding into a struct mirroring the DTOs below. If `marriage_api_test.go`'s env helpers are named differently, use its real names — do not add a parallel harness.

- [ ] **Step 2: Run to verify they fail**

```bash
cd api && go test ./internal/adapter/http/ -run TestGetVision -v
```
Expected: FAIL — 404, the route does not exist.

- [ ] **Step 3: Write the handlers**

Create `api/internal/adapter/http/vision_handlers.go`:

```go
package httpadapter

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// maxVisionRequestBodyBytes replaces the ordinary maxRequestBodyBytes for
// PUT /marriage/vision/{year}: the body is a whole document -- a theme, a
// description of up to 2000 characters, up to twelve pillars each with their
// own description and eight measures, and twenty-four milestones. 32 KiB
// clears that comfortably while still refusing anything absurd, the same
// reasoning maxRetroRequestBodyBytes gives for its own override.
const maxVisionRequestBodyBytes = 32 * 1024

// measureDTO is one line under a pillar. Kind is "typed", "linked" or
// "broken", and hasFigure is what the screen actually branches on: a broken
// link renders its label with no number at all, so current, target and
// percent are all 0 and must not be read. They are plain ints rather than
// pointers because hasFigure already carries the "there is no figure" state,
// and two ways to say the same thing is one too many.
type measureDTO struct {
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	HasFigure bool   `json:"hasFigure"`
	Current   int    `json:"current"`
	Target    int    `json:"target"`
	Percent   int    `json:"percent"`
	Met       bool   `json:"met"`
	GoalID    string `json:"goalId"`   // "" unless linked
	GoalName  string `json:"goalName"` // "" unless linked and resolved
}

type pillarDTO struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Measures    []measureDTO `json:"measures"`
}

type milestoneDTO struct {
	Year  int    `json:"year"`
	Title string `json:"title"`
	Note  string `json:"note"`
}

// visionDTO is one household-year. Pillars and Milestones are always arrays,
// never null, even when empty -- the "no null collections" convention every
// DTO in this package follows, so the frontend never distinguishes an absent
// key from an empty one. Version 0 means the year has no vision yet.
type visionDTO struct {
	Year        int            `json:"year"`
	Theme       string         `json:"theme"`
	Description string         `json:"description"`
	Version     int            `json:"version"`
	Pillars     []pillarDTO    `json:"pillars"`
	Milestones  []milestoneDTO `json:"milestones"`
}

type visionResponse struct {
	Vision visionDTO `json:"vision"`
}

// saveVisionRequest is the whole document. There is no per-field "unchanged"
// sentinel: the modal holds every field and sends all of them, which is what
// makes the save a replace rather than a merge.
type saveVisionRequest struct {
	Version     int `json:"version"`
	Theme       string `json:"theme"`
	Description string `json:"description"`
	Pillars     []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Measures    []struct {
			Label   string `json:"label"`
			Kind    string `json:"kind"`
			Current int    `json:"current"`
			Target  int    `json:"target"`
			GoalID  string `json:"goalId"`
		} `json:"measures"`
	} `json:"pillars"`
	Milestones []struct {
		Year  int    `json:"year"`
		Title string `json:"title"`
		Note  string `json:"note"`
	} `json:"milestones"`
}

func handleGetVision(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		householdID := householdIDFrom(r)
		year := deps.Visions.CurrentYear()
		if raw := r.URL.Query().Get("year"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				WriteError(w, http.StatusUnprocessableEntity, "INVALID_YEAR", "That is not a year.", nil)
				return
			}
			year = parsed
		}

		view, err := deps.Visions.Get(r.Context(), householdID, year)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, visionResponse{Vision: toVisionDTO(view)})
	}
}

func handleSaveVision(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		year, err := strconv.Atoi(chi.URLParam(r, "year"))
		if err != nil {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_YEAR", "That is not a year.", nil)
			return
		}

		var req saveVisionRequest
		if !decodeJSONBodyLimit(w, r, &req, maxVisionRequestBodyBytes) {
			return
		}

		view, err := deps.Visions.Save(r.Context(), householdIDFrom(r), year, toDomainVision(req))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, visionResponse{Vision: toVisionDTO(view)})
	}
}

// toDomainVision maps the request's kind strings onto domain.MeasureKind
// without interpreting them: an unrecognised string passes through as-is and
// is refused by domain.Measure.validate's own default branch. Guessing a kind
// here would move a fail-closed decision into the layer least able to explain
// it.
func toDomainVision(req saveVisionRequest) domain.Vision {
	v := domain.Vision{Theme: req.Theme, Description: req.Description, Version: req.Version}
	for _, p := range req.Pillars {
		pillar := domain.Pillar{Name: p.Name, Description: p.Description}
		for _, m := range p.Measures {
			pillar.Measures = append(pillar.Measures, domain.Measure{
				Label:   m.Label,
				Kind:    domain.MeasureKind(m.Kind),
				Current: m.Current,
				Target:  m.Target,
				GoalID:  m.GoalID,
			})
		}
		v.Pillars = append(v.Pillars, pillar)
	}
	for _, m := range req.Milestones {
		v.Milestones = append(v.Milestones, domain.Milestone{Year: m.Year, Title: m.Title, Note: m.Note})
	}
	return v
}

func toVisionDTO(view usecase.VisionView) visionDTO {
	pillars := make([]pillarDTO, 0, len(view.Pillars))
	for _, p := range view.Pillars {
		measures := make([]measureDTO, 0, len(p.Measures))
		for _, m := range p.Measures {
			measures = append(measures, measureDTO{
				Label: m.Label, Kind: string(m.Kind), HasFigure: m.HasFigure,
				Current: m.Current, Target: m.Target, Percent: m.Percent,
				Met: m.Met, GoalID: m.GoalID, GoalName: m.GoalName,
			})
		}
		pillars = append(pillars, pillarDTO{Name: p.Name, Description: p.Description, Measures: measures})
	}
	milestones := make([]milestoneDTO, 0, len(view.Milestones))
	for _, m := range view.Milestones {
		milestones = append(milestones, milestoneDTO{Year: m.Year, Title: m.Title, Note: m.Note})
	}
	return visionDTO{
		Year: view.Year, Theme: view.Theme, Description: view.Description,
		Version: view.Version, Pillars: pillars, Milestones: milestones,
	}
}
```

Use whatever this package's real helper for the session's household id is called (`marriage_api_test.go` and `retro_handlers.go` both show it) instead of the placeholder name `householdIDFrom` if it differs.

- [ ] **Step 4: Map the new domain errors**

In `api/internal/adapter/http/errors.go`, inside `MapDomainError`, add — placed with the other 409s and 422s:

```go
	case errors.Is(err, domain.ErrVisionChanged):
		WriteError(w, http.StatusConflict, "VISION_CHANGED",
			"This vision changed while you were editing it. Reload and try again.", nil)
	case errors.Is(err, domain.ErrVisionGoalUnknown):
		WriteError(w, http.StatusUnprocessableEntity, "VISION_GOAL_UNKNOWN",
			"That savings goal is not one of this household's.", nil)
	case errors.Is(err, domain.ErrVisionThemeRequired):
		WriteError(w, http.StatusUnprocessableEntity, "VISION_THEME_REQUIRED",
			"Give this year a theme.", nil)
	case errors.Is(err, domain.ErrVisionMeasureAmbiguous),
		errors.Is(err, domain.ErrVisionMeasureGoalRequired):
		WriteError(w, http.StatusUnprocessableEntity, "VISION_MEASURE_INVALID",
			"A measure is either a number you keep, or a savings goal — not both.", nil)
	case errors.Is(err, domain.ErrVisionThemeTooLong),
		errors.Is(err, domain.ErrVisionDescriptionTooLong),
		errors.Is(err, domain.ErrVisionYearOutOfRange),
		errors.Is(err, domain.ErrVisionPillarNameRequired),
		errors.Is(err, domain.ErrVisionMeasureLabelRequired),
		errors.Is(err, domain.ErrVisionMeasureTargetNotPositive),
		errors.Is(err, domain.ErrVisionMeasureCurrentNegative),
		errors.Is(err, domain.ErrVisionMilestoneTitleRequired),
		errors.Is(err, domain.ErrVisionTooManyPillars),
		errors.Is(err, domain.ErrVisionTooManyMeasures),
		errors.Is(err, domain.ErrVisionTooManyMilestones):
		WriteError(w, http.StatusUnprocessableEntity, "VISION_INVALID", err.Error(), nil)
```

- [ ] **Step 5: Register the routes and wire the service**

In `router.go`, add `Visions *usecase.VisionService` to `Deps`, then inside the existing marriage group (`router.go:301-302`), beside the retro routes:

```go
				m.Get("/marriage/vision", handleGetVision(deps))
```

and inside that group's CSRF sub-group:

```go
					w.Put("/marriage/vision/{year}", handleSaveVision(deps))
```

In `api/cmd/api/main.go`, beside `retroRepo`:

```go
	visionRepo := postgres.NewVisionRepo(db)
```

beside `retroSvc`:

```go
	// goalRepo doubles as the GoalProgressReader: Vision needs one
	// percentage from Goals and the narrow port is what keeps it from
	// depending on GoalRepository's whole surface.
	visionSvc := usecase.NewVisionService(visionRepo, goalRepo, clock)
```

and in the `Deps` literal: `Visions: visionSvc,`. Use whatever the clock variable is actually called in that file.

- [ ] **Step 6: Run the tests to verify they pass**

```bash
cd api && go test ./internal/adapter/http/ -run TestVision -v && go test ./internal/adapter/http/ -run TestPutVision -v
```
Expected: PASS.

- [ ] **Step 7: Mutation-check the guard**

Move `m.Get("/marriage/vision", …)` out of the marriage group into the plain `g` group. `TestPutVisionAsALimitedMemberIsRefused`'s read equivalent must fail — if no test covers the *read* guard, add one before moving on. Restore.

- [ ] **Step 8: Commit**

```bash
git add api/internal/adapter/http api/internal/usecase/ports.go api/cmd/api/main.go
git commit -m "feat: vision routes, guarded and wired"
```

---

### Task 10: Frontend schemas, query keys and `useVision`

**Files:**
- Create: `web/src/features/marriage/visionSchemas.ts`, `web/src/features/marriage/visionQueryKeys.ts`, `web/src/features/marriage/useVision.ts`, `web/src/features/marriage/useVision.test.ts`

**Interfaces:**
- Consumes: Task 9's JSON.
- Produces: `visionQueryKey(year)`, `useVision(year)` returning `{ vision, isLoading, error, saveVision }`, and the types `Vision`, `VisionPillar`, `VisionMeasure`, `VisionMilestone`, `SaveVisionBody`.

- [ ] **Step 1: Write the schemas and keys**

Create `web/src/features/marriage/visionQueryKeys.ts`:

```ts
// The Vision feature's TanStack Query key, in its own module for the reason
// retroQueryKeys.ts records: Overview's VisionCard reads the same year's
// vision as VisionPage does, and neither should import from the other just to
// invalidate a cache.
export function visionQueryKey(year: number) {
  return ["vision", year] as const;
}
```

Create `web/src/features/marriage/visionSchemas.ts`:

```ts
// Zod mirrors of the DTOs in api/internal/adapter/http/vision_handlers.go
// (measureDTO, pillarDTO, milestoneDTO, visionDTO, visionResponse). These
// follow the backend's structs rather than the design doc, the convention
// retroSchemas.ts and goalSchemas.ts already use -- the backend's comments
// say which fields can be absent and why.
import { z } from "zod";

// kind mirrors domain.MeasureKind. "broken" is a real state the server can
// send: ON DELETE SET NULL leaves a measure whose goal was deleted with no
// figure at all. Parsing it explicitly rather than falling back to "typed" is
// what stops such a measure rendering as a confident "0 of 0".
export const measureKindSchema = z.enum(["typed", "linked", "broken"]);

// hasFigure is the field the screen branches on. When it is false, current,
// target and percent are all 0 and mean nothing -- the server's own comment.
export const visionMeasureSchema = z.object({
  label: z.string(),
  kind: measureKindSchema,
  hasFigure: z.boolean(),
  current: z.number().int(),
  target: z.number().int(),
  percent: z.number().int(),
  met: z.boolean(),
  goalId: z.string(),
  goalName: z.string(),
});
export type VisionMeasure = z.infer<typeof visionMeasureSchema>;

export const visionPillarSchema = z.object({
  name: z.string(),
  description: z.string(),
  measures: z.array(visionMeasureSchema),
});
export type VisionPillar = z.infer<typeof visionPillarSchema>;

export const visionMilestoneSchema = z.object({
  year: z.number().int(),
  title: z.string(),
  note: z.string(),
});
export type VisionMilestone = z.infer<typeof visionMilestoneSchema>;

// version 0 means this year has no vision yet, and it is what a first save
// sends back so the server can tell a create from a blind overwrite.
export const visionSchema = z.object({
  year: z.number().int(),
  theme: z.string(),
  description: z.string(),
  version: z.number().int(),
  pillars: z.array(visionPillarSchema),
  milestones: z.array(visionMilestoneSchema),
});
export type Vision = z.infer<typeof visionSchema>;

export const visionResponseSchema = z.object({ vision: visionSchema });
```

- [ ] **Step 2: Write the failing hook test**

Create `web/src/features/marriage/useVision.test.ts` following `useRetros.test.ts`'s harness — read it first; it already builds a `QueryClient` and stubs `apiFetch`, and the names below (`renderHookWithClient`, `mockApiFetch`) must be replaced with whatever that file actually uses.

```ts
import { waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useVision } from "./useVision";

const emptyYear = {
  vision: { year: 2026, theme: "", description: "", version: 0, pillars: [], milestones: [] },
};

const setYear = {
  vision: {
    year: 2026,
    theme: "Slow down together",
    description: "Fewer commitments.",
    version: 4,
    pillars: [],
    milestones: [],
  },
};

describe("useVision", () => {
  it("treats a year that has no vision as version 0 rather than an error", async () => {
    mockApiFetch.mockResolvedValueOnce(emptyYear);
    const { result } = renderHookWithClient(() => useVision(2026));

    await waitFor(() => expect(result.current.vision).toBeDefined());
    expect(result.current.error).toBeNull();
    expect(result.current.vision?.version).toBe(0);
    expect(result.current.vision?.theme).toBe("");
  });

  // A hook that let a caller pass version could send a stale one by accident.
  // The version on the wire must always be the one this hook loaded -- the
  // same mutation check useRetros.test.ts makes of saveRetro.
  it("sends the version it loaded, never one the caller supplies", async () => {
    mockApiFetch.mockResolvedValueOnce(setYear);
    const { result } = renderHookWithClient(() => useVision(2026));
    await waitFor(() => expect(result.current.vision?.version).toBe(4));

    mockApiFetch.mockResolvedValueOnce({ vision: { ...setYear.vision, version: 5 } });
    await result.current.saveVision({
      theme: "Slow down together",
      description: "Fewer commitments.",
      pillars: [],
      milestones: [],
    });

    const [url, init] = mockApiFetch.mock.calls[1];
    expect(url).toBe("/api/v1/marriage/vision/2026");
    expect(init.method).toBe("PUT");
    expect(JSON.parse(init.body).version).toBe(4);
  });

  it("sends version 0 for a year that had none, so the save is a create", async () => {
    mockApiFetch.mockResolvedValueOnce(emptyYear);
    const { result } = renderHookWithClient(() => useVision(2026));
    await waitFor(() => expect(result.current.vision?.version).toBe(0));

    mockApiFetch.mockResolvedValueOnce({ vision: { ...emptyYear.vision, theme: "T", version: 1 } });
    await result.current.saveVision({ theme: "T", description: "", pillars: [], milestones: [] });

    expect(JSON.parse(mockApiFetch.mock.calls[1][1].body).version).toBe(0);
  });

  it("refuses a response whose measure kind it does not recognise", async () => {
    // The zod boundary is the frontend's own fail-closed rule: a server that
    // drifts must surface as an error, not render as a confident wrong number.
    mockApiFetch.mockResolvedValueOnce({
      vision: {
        ...setYear.vision,
        pillars: [{ name: "P", description: "", measures: [{
          label: "M", kind: "sideways", hasFigure: true, current: 0, target: 0,
          percent: 0, met: false, goalId: "", goalName: "",
        }] }],
      },
    });
    const { result } = renderHookWithClient(() => useVision(2026));
    await waitFor(() => expect(result.current.error).toBeTruthy());
  });
});
```

- [ ] **Step 3: Run to verify it fails**

```bash
cd web && npx vitest run src/features/marriage/useVision.test.ts
```
Expected: FAIL — the module does not exist.

- [ ] **Step 4: Write the hook**

Create `web/src/features/marriage/useVision.ts`:

```ts
// Fetch orchestration for one year's Vision. Year-keyed the way useRetro.ts
// keys its own single-month query: a vision is addressed by the year it
// belongs to.
//
// saveVision always attaches the version this hook loaded, and the caller
// never supplies one -- a caller that could pass version could pass a stale
// one, and the whole point of the guard is that a stale save is refused
// rather than silently overwriting a partner's work.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { visionQueryKey } from "./visionQueryKeys";
import { visionResponseSchema, type Vision } from "./visionSchemas";

export type SaveVisionBody = {
  theme: string;
  description: string;
  pillars: {
    name: string;
    description: string;
    measures: { label: string; kind: "typed" | "linked"; current: number; target: number; goalId: string }[];
  }[];
  milestones: { year: number; title: string; note: string }[];
};

async function fetchVision(year: number): Promise<Vision> {
  const body = await apiFetch<unknown>(`/api/v1/marriage/vision?year=${year}`);
  return visionResponseSchema.parse(body).vision;
}

export function useVision(year: number) {
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: visionQueryKey(year), queryFn: () => fetchVision(year) });

  const save = useMutation({
    mutationFn: async (body: SaveVisionBody) => {
      const response = await apiFetch<unknown>(`/api/v1/marriage/vision/${year}`, {
        method: "PUT",
        // version comes from the loaded query, never from the caller.
        body: JSON.stringify({ ...body, version: query.data?.version ?? 0 }),
      });
      return visionResponseSchema.parse(response).vision;
    },
    onSuccess: async (saved) => {
      queryClient.setQueryData(visionQueryKey(year), saved);
      await queryClient.invalidateQueries({ queryKey: visionQueryKey(year) });
    },
  });

  return {
    vision: query.data,
    isLoading: query.isLoading,
    error: query.error,
    saveVision: save.mutateAsync,
    isSaving: save.isPending,
    saveError: save.error,
  };
}
```

Match `apiFetch`'s real signature — check `web/src/api/client.ts` for whether it takes a body object or a string and whether it attaches the CSRF header itself.

- [ ] **Step 5: Run to verify it passes**

```bash
cd web && npx vitest run src/features/marriage/useVision.test.ts
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/marriage/vision*.ts web/src/features/marriage/useVision*
git commit -m "feat: vision schemas, query key and useVision"
```

---

### Task 11: `VisionPage`, its route and the `/marriage` index

**Files:**
- Create: `web/src/features/marriage/VisionPage.tsx`, `PillarCard.tsx`, `MilestoneGrid.tsx` and their tests
- Modify: `web/src/routes/router.tsx`, `web/src/features/shell/Sidebar.tsx`

**Interfaces:**
- Consumes: `useVision` (Task 10).
- Produces: the route `/marriage/vision`, the sidebar entry, and a `/marriage` index that redirects to Retros.

- [ ] **Step 1: Write the failing page tests**

Create `web/src/features/marriage/VisionPage.test.tsx`. Cover, each as its own `it`:

- renders the theme hero with the year label, the theme in quotes and the description
- renders one card per pillar, numbered `Pillar 1`, `Pillar 2`
- a typed measure renders `2 of 2` and shows the met marker; `3 of 5` does not
- a linked measure renders `62%`
- **a measure with `hasFigure: false` renders its label and no number at all** — assert the absence of `0`, not merely the presence of the label
- a year with `version: 0` renders the empty state and its call to action, not a grid of blank cards
- milestones render in order with their year, title and note

Use the same `renderWithProviders` helper `RetrosPage.test.tsx` uses.

- [ ] **Step 2: Run to verify they fail**

```bash
cd web && npx vitest run src/features/marriage/VisionPage.test.tsx
```
Expected: FAIL — module not found.

- [ ] **Step 3: Build the three components**

`PillarCard.tsx` renders one pillar: the `Pillar N` label, name, description, and its measures. The measure row is the piece that carries the decision:

```tsx
// A measure with no figure renders its label and a short explanation, never a
// number. `hasFigure: false` is what the server sends when a linked goal was
// deleted (vision_handlers.go's measureDTO comment) -- rendering `0 of 0`
// there would state something untrue about the household, the same way a zero
// net worth would after a primary-currency change.
function MeasureRow({ measure }: { measure: VisionMeasure }) {
  if (!measure.hasFigure) {
    return (
      <div className="flex justify-between gap-3 text-sm">
        <span>{measure.label}</span>
        <span className="text-muted">Goal removed</span>
      </div>
    );
  }
  return (
    <div className="flex justify-between gap-3 text-sm">
      <span>{measure.label}</span>
      <span className={measure.met ? "font-semibold text-accent" : "font-semibold"}>
        {measure.kind === "linked" ? `${measure.percent}%` : `${measure.current} of ${measure.target}`}
        {measure.met ? " ✓" : ""}
      </span>
    </div>
  );
}
```

`MilestoneGrid.tsx` renders the "Longer horizon" panel and the `+ Add milestone` affordance, which opens the modal (Task 12) — until then, wire it to the same handler the header's **Edit vision** button uses.

`VisionPage.tsx` composes the header, hero, pillar grid and milestone panel, and renders the empty state when `vision.version === 0`. Follow `RetrosPage.tsx` for the loading, error and non-owner branches — including the distinction `BillsPage.tsx` had to be taught: a routine 403 is not the same as a server failure and must not get the same red alert.

Use tabular figures for every number (`font-variant-numeric: tabular-nums`), the convention the UI-polish round established.

- [ ] **Step 4: Add the route, the index redirect and the sidebar entry**

In `web/src/routes/router.tsx`, beside `marriageRetrosRoute`:

```tsx
const marriageVisionRoute = createRoute({
  getParentRoute: () => marriageGuardRoute,
  path: "vision",
  component: VisionPage,
});

// Task 10 of the Retros plan left marriageGuardRoute with one child and no
// index, so bare /marriage rendered the shell with an empty content area.
// Marriage now has two pages, so the index goes to the first of them.
const marriageIndexRoute = createRoute({
  getParentRoute: () => marriageGuardRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/marriage/retros" });
  },
});
```

Add all three to `marriageGuardRoute.addChildren([...])` in the route tree, and import `redirect` from `@tanstack/react-router`.

In `Sidebar.tsx`:

```tsx
  marriage: [
    { label: "Retros", to: "/marriage/retros" },
    { label: "Vision & goals", to: "/marriage/vision" },
  ],
```

and update that block's comment: Retros is no longer "the first of Marriage's three pages" with the other two still ⬜ — Vision has landed, Agreements has not.

- [ ] **Step 5: Run the tests**

```bash
cd web && npx vitest run src/features/marriage/ && npx vitest run src/routes/
```
Expected: PASS, including the existing router test.

- [ ] **Step 6: Mutation-check the figureless measure**

Delete the `if (!measure.hasFigure)` branch from `MeasureRow`. The "renders its label and no number at all" test must fail. Restore.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/marriage web/src/routes/router.tsx web/src/features/shell/Sidebar.tsx
git commit -m "feat: the Vision page, its route, the sidebar entry and /marriage's index"
```

---

### Task 12: `VisionModal` — the whole-document editor

**Files:**
- Create: `web/src/features/marriage/VisionModal.tsx`, `VisionModal.test.tsx`
- Modify: `web/src/features/marriage/VisionPage.tsx` (mount it)

**Interfaces:**
- Consumes: `useVision().saveVision` (Task 10), `useGoals` for the goal picker.
- Produces: the modal, opened by **Edit vision** and by **+ Add milestone**.

- [ ] **Step 1: Write the failing tests**

`VisionModal.test.tsx` covers:

- opens prefilled from the loaded vision, including each pillar's description and measures — **the two fields the design's own modal omits** (spec decision 7)
- **+ Add pillar** appends an empty pillar; its ✕ removes it
- **+ Add measure** appends a measure to that pillar; switching it between "a number we keep" and "a savings goal" clears the other mode's inputs, so a measure can never be submitted as both
- **+ Add milestone** appends a row with year, title and note
- **Save vision** submits every field — assert the exact body handed to `saveVision`, including a pillar whose description was edited, since a modal that quietly dropped a field would still pass a shallower test
- a `409` from the save renders the reload message rather than a generic failure
- the year select offers exactly the previous, current and next year

- [ ] **Step 2: Run to verify they fail**

```bash
cd web && npx vitest run src/features/marriage/VisionModal.test.tsx
```
Expected: FAIL.

- [ ] **Step 3: Build the modal**

Follow `RetroModal.tsx` for structure, focus management and the in-page confirmation convention (never `window.confirm`). The measure editor is the new part, and its rule belongs in a comment at the line someone would change:

```tsx
// A measure is typed OR linked, never both -- the domain refuses the
// ambiguous shape and so does the database's own
// measure_is_typed_or_linked. Switching modes therefore CLEARS the other
// mode's inputs rather than leaving them populated and hidden: a hidden
// value that still submits is how a form sends a body its own UI never
// showed anyone.
function setMeasureMode(measure: DraftMeasure, mode: "typed" | "linked"): DraftMeasure {
  return mode === "typed"
    ? { ...measure, kind: "typed", goalId: "", current: 0, target: 1 }
    : { ...measure, kind: "linked", goalId: "", current: 0, target: 0 };
}
```

The year select offers `[currentYear - 1, currentYear, currentYear + 1]` — the spec's decision, with its reasoning as a comment: a household setting January's theme in December needs next year, one writing up a year they never recorded needs last year, and nothing in the design asks for 2019.

- [ ] **Step 4: Run the tests**

```bash
cd web && npx vitest run src/features/marriage/
```
Expected: PASS.

- [ ] **Step 5: Mutation-check the mode switch**

Make `setMeasureMode` preserve `goalId` when switching to `"typed"`. The "switching clears the other mode's inputs" test must fail. Restore.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/marriage
git commit -m "feat: the Edit-vision modal, with the pillar descriptions and measures the design's own modal omits"
```

---

### Task 13: Overview — the card and the check-in strip

**Files:**
- Create: `web/src/features/overview/VisionCard.tsx`, `VisionCard.test.tsx`
- Modify: `web/src/features/overview/OverviewPage.tsx`, `web/src/features/overview/NextRetroCard.tsx`

**Interfaces:**
- Consumes: `useVision(currentYear)` (Task 10).
- Produces: `VisionCard`, and the check-in strip inside `NextRetroCard`.

- [ ] **Step 1: Write the failing tests**

`VisionCard.test.tsx` pins decision 3 exactly:

- renders the theme, then **one line per pillar**, each showing that pillar's **first** measure with its live figures
- a pillar with **no** measures renders its own name on that line instead
- a vision with **no pillars** renders the theme alone
- **a vision with `version: 0` renders nothing at all** — assert the card is absent, not empty. An empty quotation would say the household has a vision and it is blank
- clicking the card navigates to `/marriage/vision`

Add one test to `NextRetroCard.test.tsx`: the strip renders `Vision check-in: 2026 theme — "…"` when a theme exists, and is absent when none does.

- [ ] **Step 2: Run to verify they fail**

```bash
cd web && npx vitest run src/features/overview/
```
Expected: FAIL.

- [ ] **Step 3: Build the card**

```tsx
// One line per pillar, each its FIRST measure -- not the design's own three
// flat commitment lines, which are a third shape the design never says how to
// store (spec decision 3). "First" is free: measures already carry a display
// order for the pillar cards, so nothing here needs a "show on overview" flag
// and there is no "which three of six" to answer.
function overviewLine(pillar: VisionPillar): { label: string; figure: string | null } {
  const measure = pillar.measures[0];
  if (!measure) return { label: pillar.name, figure: null };
  if (!measure.hasFigure) return { label: measure.label, figure: null };
  return {
    label: measure.label,
    figure: measure.kind === "linked" ? `${measure.percent}%` : `${measure.current} of ${measure.target}`,
  };
}
```

The card returns `null` when `vision.version === 0`, and both Overview surfaces render only for a member holding `marriage` — the rule Retros' decision 9 set for the Next-retro card; reuse whatever capability check `NextRetroCard.tsx` already applies rather than writing a second one.

- [ ] **Step 4: Run the tests**

```bash
cd web && npx vitest run src/features/overview/
```
Expected: PASS.

- [ ] **Step 5: Mutation-check the omission**

Make the card render its shell when `version === 0`. The "renders nothing at all" test must fail. Restore.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/overview
git commit -m "feat: Overview's Vision card and the retro card's check-in strip"
```

---

### Task 14: The documents

**Files:**
- Modify: `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`, `docs/SYSTEM_DESIGN.md`

**Interfaces:**
- Consumes: everything built in Tasks 1–13.
- Produces: the record. A feature nobody ticked off gets built twice.

- [ ] **Step 1: Update `docs/SYSTEM_DESIGN.md`**

Use the **`maintaining-system-design`** skill. This change adds four tables, two routes, a service and a port — all four are things that document draws. Change the prose under each diagram too; that is where the non-obvious reasoning lives.

- [ ] **Step 2: Update `docs/FEATURE_TRACKER.md`**

In the Marriage table: `Vision — yearly theme`, `Vision — pillars with measures`, `Vision — longer-horizon milestones` and `Edit vision (modal)` move ⬜ → ✅. The marriage-duration row **stays ⬜** and keeps the reason already recorded there.

Add any row for something built that the design never drew — the `/marriage` index redirect is a candidate, the way Accounts' archive/restore rows were added.

**Say plainly that per-year history is stored and not yet reachable.** The schema keeps one vision per year and decision 4 sells that as "2026 remains readable forever", but nothing built here displays a past year: the page always renders the current one, and the modal's year select is the only affordance that can even write to another. Whoever reads the tracker next will otherwise believe history is browsable. One line, at the `Vision — yearly theme` row or beneath the table.

In Overview: the `Vision check-in strip` row moves to ✅.

**Recount the summary table** by counting the first symbol in each row's own cell, section by section — never by adjusting the previous totals. This file records that adjusting by delta has produced wrong numbers before.

- [ ] **Step 3: Update `docs/LEARNING.md`**

One entry per defect worth remembering. At minimum, the one this plan was written around:

> **A referential `SET NULL` is an `UPDATE`, and CHECK constraints are enforced on `UPDATE`.** `vision_measures` began with a two-branch CHECK (typed or linked). `ON DELETE SET NULL` on `goal_id` would have produced an all-null row satisfying neither, so **deleting a savings goal would have failed with a constraint violation surfacing inside the Goals feature** — a Vision bug reported as a Goals bug. Caught in spec review, before any code. What would have caught it later: a repository test that deletes a linked goal, which is why one exists (`TestDeletingALinkedGoalUnlinksTheMeasureInsteadOfFailing`).

If a defect found during the build matches an existing pattern, add it there as evidence rather than starting a new section — the repetition is the point.

- [ ] **Step 4: Full verification**

```bash
make lint && make test
```
Expected: both green. Fix anything that is not before continuing — this is the bar in `CLAUDE.md`.

- [ ] **Step 5: Commit**

```bash
git add docs/
git commit -m "docs: vision in the tracker, the learning log and the system design"
```

---

### Task 15: The browser walk

**Files:**
- Create: `docs/superpowers/plans/2026-08-28-hearth-vision-verification.md`

**Interfaces:**
- Consumes: the running app.
- Produces: the verification record. A feature is not done because its tests pass.

- [ ] **Step 1: Start the app and seed it**

```bash
make dev    # http://localhost:5173
make seed   # prints sign-in details
```

- [ ] **Step 2: Walk all fifteen criteria in a real browser**

Drive it yourself with the browser tools. Record each criterion's result, and any criterion met by an interpreted rather than literal path, the way Bills' and Retros' own records do.

1. Signed in as an owner, the sidebar shows **Vision & goals** under Marriage, and it opens `/marriage/vision`.
2. A household with no vision for this year sees the **empty state** — an invitation to set the theme, not a grid of blank cards and not an error.
3. **Edit vision** opens the modal with the year select offering last year, this year and next year, and nothing else.
4. Saving a theme and description renders the hero, with the theme in quotes at full width and **no marriage-duration block** (spec decision 2).
5. Adding a pillar with a description and two typed measures renders `Pillar 1`, its description and both measures with their `N of M` figures.
6. A measure at its target shows the met marker; one below it does not.
7. Adding a measure linked to a savings goal renders that goal's **percentage**, and the percentage matches what `/money/goals` shows for the same goal.
8. **Deleting that goal from `/money/goals` succeeds** — the delete does not error — and the Vision page then shows that measure's label with **no number**. This is the constraint's third branch, exercised through the product rather than a test.
9. Adding three milestones renders them in order in **Longer horizon**, each with its year, title and note.
10. **+ Add milestone** opens the same modal.
11. Two browser windows, both signed in as the same owner, both with the modal open on the same year: the second save is refused with the reload message, and **the first partner's pillars are still there** after reloading.
12. The same, on a year that had **no** vision: both windows open the empty modal, the second save is refused rather than overwriting.
13. The Overview shows the **Vision 2026** card with one line per pillar, each its first measure, and the **Vision check-in** strip inside the Next-retro card. A household with no vision sees **neither**.
14. A limited member typing `/marriage/vision` is redirected to `/`, never reaching the page.
15. The page holds up at 305, 360, 768 and 1440 px with **no horizontal overflow**, screenshots at each width.

- [ ] **Step 3: Fix anything the walk finds, and sweep for the class**

If a defect appears, use the **`hunting-sibling-defects`** skill before closing it: in this codebase, fixing one instance has failed to fix the class five separate times. Bills' own walk found a defect that turned out to be sitting in two sibling pages as well.

- [ ] **Step 4: Write the record and commit**

```bash
git add docs/superpowers/plans/2026-08-28-hearth-vision-verification.md
git commit -m "docs: Vision's fifteen-criterion browser walk"
```

---

## Self-Review

**Spec coverage.** Every decision has a task: 1 → Tasks 2, 6, 7, 12; 2 → Task 14 (the tracker row, unchanged) and criterion 4; 3 → Task 13; 4 → Task 1; 5 → Tasks 1, 5; 6 → Tasks 1, 11; 7 → Task 12; 8 → Tasks 1, 4, 7, criterion 8; 9 → Tasks 7, 9; 10 → Tasks 1, 5, 9, criteria 11–12; 11 → Task 11; 12 → Tasks 3, 6; 13 → Task 11. The formulas table is implemented in Tasks 6 (percent), 7 (met, no-figure), 13 (overview lines), 9 (default year). Error handling is Task 9's step 4. Testing is distributed across every task, with the mutation checks named individually.

**Types.** `domain.MeasureKind` / `MeasureTyped` / `MeasureLinked` / `MeasureBroken` are used consistently in Tasks 2, 4, 5, 7, 9 and mirrored as the `"typed" | "linked" | "broken"` zod enum in Task 10. `usecase.GoalProgress{GoalID, Name, Percent}` is defined once (Task 3) and consumed in Tasks 6 and 7. `VisionView`/`PillarView`/`MeasureView` are defined in Task 7 and mapped to DTOs in Task 9 under the same field names. `version` means the same thing at every layer, and `0` means "unset" at every one of them.

**Known soft spots, called out rather than hidden.** Three names must be checked against the real code before use rather than trusted from this plan: `uuidString` (Task 4), `versionParam` (Task 5) and `householdIDFrom` (Task 9). Each exists in this codebase in some form; each step says to confirm the actual name. sqlc's generated struct names for `CreateVision`/`UpdateVision` may not permit the direct type conversion Task 5 uses — the step says what to do instead.
