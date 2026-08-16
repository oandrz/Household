# Marriage Retros Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Marriage → Retros screen — a monthly shared retro with mood, went-well/was-hard, actions carried between months, a 12-month mood chart — and the Overview card that reads it.

**Architecture:** Three tables (`retros`, `retro_actions`, `retro_action_assignees`) behind two narrow ports, one service holding every derived figure, one HTTP group gated on `requireCapability(marriage)` stacked on `requireOwner`, and a React feature folder whose fetch orchestration lives in hooks from the first task. Concurrent editing is handled by a `version` column: a stale save is refused with `409`, never merged.

**Tech Stack:** Go 1.23 + chi + pgx/v5 + sqlc (queries in `api/internal/adapter/postgres/queries/`), goose migrations, Postgres 17, React 19 + TypeScript + TanStack Query + Tailwind, Vitest + Testing Library, testcontainers for the Go database tests.

**Spec:** `docs/superpowers/specs/2026-08-16-hearth-retros-design.md` — read it before Task 1. Every decision number referenced below (decision 1 … decision 12) is a section of that document.

## Global Constraints

- **Clean architecture, enforced by `make lint-arch`.** `internal/domain` imports the standard library only; `internal/usecase` may add `internal/domain`; everything else lives under `internal/adapter/**` or `cmd/**`. No pgx, chi or HTTP type crosses out of an adapter. A missing row becomes `domain.ErrNotFound` at the adapter boundary, never `pgx.ErrNoRows`.
- **No service takes an actor parameter.** Services enforce what is *valid*; middleware enforces who is *asking* (`CLAUDE.md`).
- **Every 2xx except 204 carries a JSON body**, because `apiFetch` throws on an ok response it cannot parse.
- **Fail closed on values you did not construct.** A `switch` over anything arriving from a database column or a request body needs a `default` that refuses. `mood` arrives from both.
- **No new dependencies.** The mood chart is inline SVG. Versions stay pinned; floating versions have broken this build twice.
- **`stubFetchRoutes` for every frontend test.** It matches on method *and* URL and throws on an unregistered request; a stub that ignores the URL has silently passed broken code twice here.
- **At least one mutation-checked test per task.** Break the implementation on purpose, watch the named test go red, restore. Record which mutation you used in the commit body.
- **Two breakpoints only, `sm` (640px) and `lg` (1024px)**, `dvh` not `vh` on full-height boxes, and a 44px touch-target floor on interactive controls. Any control that cannot meet the floor gets named in the tracker row, not shipped quietly small.
- **Test commands:** `cd api && go test ./... -count=1 -timeout=5m` (needs Docker; on the original machine export `DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock` and `TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`), `cd web && npx vitest run`, and `make lint` before any commit that closes a task.
- **Host edits to `web/src/**` do not reliably reach the running dev server.** If a change appears not to compile, `docker restart hearth-web-1` before debugging the code (`docs/HANDOVER.md` §2).

---

### Task 1: The migration

**Files:**
- Create: `api/migrations/00009_retros.sql`
- Test: `api/internal/adapter/postgres/schema_test.go` (add one case)

**Interfaces:**
- Consumes: nothing.
- Produces: tables `retros`, `retro_actions`, `retro_action_assignees` with the columns every later task reads by name.

- [ ] **Step 1: Write the failing test**

Add to `api/internal/adapter/postgres/schema_test.go`, following the file's existing shape:

```go
// TestRetroSchema pins the three columns later tasks depend on being NULLable
// or not. mood and completed_at must be nullable -- a draft with no emoji
// picked yet has no mood, and a draft has no completion time. version must be
// NOT NULL with a default, because every update path reads it.
func TestRetroSchema(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	var moodNullable, completedNullable, versionNullable string
	err := db.Pool().QueryRow(ctx, `
		SELECT
			max(CASE WHEN column_name = 'mood'         THEN is_nullable END),
			max(CASE WHEN column_name = 'completed_at' THEN is_nullable END),
			max(CASE WHEN column_name = 'version'      THEN is_nullable END)
		FROM information_schema.columns
		WHERE table_name = 'retros'
	`).Scan(&moodNullable, &completedNullable, &versionNullable)
	if err != nil {
		t.Fatalf("query columns: %v", err)
	}
	if moodNullable != "YES" || completedNullable != "YES" {
		t.Fatalf("mood=%s completed_at=%s, want both YES", moodNullable, completedNullable)
	}
	if versionNullable != "NO" {
		t.Fatalf("version nullable = %s, want NO", versionNullable)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd api && go test ./internal/adapter/postgres/ -run TestRetroSchema -count=1 -v`
Expected: FAIL — the `retros` table does not exist, so the scan returns no rows.

- [ ] **Step 3: Write the migration**

Create `api/migrations/00009_retros.sql`. The comments are part of the deliverable — this file is where a future editor decides whether to add a column, and every non-obvious choice must argue for itself there:

```sql
-- +goose Up

-- retros is one household-month's check-in. One row per calendar month
-- (UNIQUE below), month stored as the first of the month -- the same
-- convention budgets and TransactionRepository.MonthTotals already use.
--
-- A retro is ONE shared draft that both partners write into, and its lines
-- carry no author: the design renders no name against any bullet, and
-- attribution would need an author column, a second mood, and a rule for what
-- "4/5" means when two people disagree (spec decision 1).
CREATE TABLE retros (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    month        date        NOT NULL,
    -- NULL means nobody has picked an emoji yet. Never 0: zero is a claim,
    -- the same reasoning budgets.expected_income_minor carries.
    mood         smallint    CHECK (mood BETWEEN 1 AND 5),
    went_well    text        NOT NULL DEFAULT '',
    was_hard     text        NOT NULL DEFAULT '',
    notes        text        NOT NULL DEFAULT '',
    -- NULL is the whole draft concept (spec decision 2). No status column: one
    -- nullable timestamp answers "is it done" and "when", and a status enum
    -- would let a row claim finished with no completion time.
    completed_at timestamptz,
    -- Optimistic concurrency. Both partners can have the same draft open, and
    -- a whole-column save would otherwise silently eat whatever the other one
    -- just typed (spec decision 6). Bumped by the retro update path only --
    -- ticking an action writes a different table and must not collide.
    version      integer     NOT NULL DEFAULT 1,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, month)
);

-- retro_actions is what a retro decided to do next month. No position column
-- on purpose: an ordering integer needs a writer, the only safe writer is
-- max(position)+1 inside the insert, and two partners adding an action at the
-- same moment can still collide on it -- the version guard above covers the
-- retro's text, not its actions. The design draws no reordering control, so
-- the column would exist only to create that race. Insertion order is the
-- order; ORDER BY created_at, id.
CREATE TABLE retro_actions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    retro_id     uuid        NOT NULL REFERENCES retros(id) ON DELETE CASCADE,
    body         text        NOT NULL,
    -- NULL means open; this is the tick, and it is ticked all month long,
    -- which is why it lives on the page rather than inside the modal.
    done_at      timestamptz,
    -- Provenance for an action carried from last month (spec decision 4).
    -- ON DELETE SET NULL: deleting July's action must never delete August's
    -- copy of it.
    carried_from uuid        REFERENCES retro_actions(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX retro_actions_retro_id_idx ON retro_actions (retro_id);

-- An action is assigned to one owner or both -- the design's June retro shows
-- "A C" on one action and "A" on another. A join table rather than two boolean
-- columns: nothing in this product caps a household at two owners (the invite
-- modal offers "Parent" freely; last-owner protection only guarantees at
-- least one), so two columns would encode a limit that does not exist.
CREATE TABLE retro_action_assignees (
    action_id     uuid NOT NULL REFERENCES retro_actions(id) ON DELETE CASCADE,
    membership_id uuid NOT NULL REFERENCES memberships(id) ON DELETE CASCADE,
    PRIMARY KEY (action_id, membership_id)
);

-- +goose Down
DROP TABLE retro_action_assignees;
DROP TABLE retro_actions;
DROP TABLE retros;
```

- [ ] **Step 4: Run the test and watch it pass**

Run: `cd api && go test ./internal/adapter/postgres/ -run TestRetroSchema -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Mutation-check it**

Temporarily change `version integer NOT NULL DEFAULT 1` to `version integer DEFAULT 1` and rerun. Expected: FAIL on `version nullable = YES, want NO`. Restore.

- [ ] **Step 6: Commit**

```bash
git add api/migrations/00009_retros.sql api/internal/adapter/postgres/schema_test.go
git commit -m "feat(retros): three tables, and the two columns that are decisions"
```

---

### Task 2: Domain rules

**Files:**
- Create: `api/internal/domain/retro.go`, `api/internal/domain/retro_test.go`
- Modify: `api/internal/domain/errors.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Mood int` with `func ParseMood(n int) (Mood, error)` and `ErrInvalidMood`
  - `func StartableMonth(today time.Time, currentExists, previousExists bool) (time.Time, bool)`
  - `func FirstSentence(notes string) string`
  - `ErrRetroChanged`

- [ ] **Step 1: Write the failing tests**

Create `api/internal/domain/retro_test.go`:

```go
package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestParseMoodRefusesAnythingOutsideOneToFive(t *testing.T) {
	for _, n := range []int{-1, 0, 6, 99} {
		if _, err := domain.ParseMood(n); !errors.Is(err, domain.ErrInvalidMood) {
			t.Fatalf("ParseMood(%d) err = %v, want ErrInvalidMood", n, err)
		}
	}
	for _, n := range []int{1, 2, 3, 4, 5} {
		got, err := domain.ParseMood(n)
		if err != nil || int(got) != n {
			t.Fatalf("ParseMood(%d) = %v, %v; want %d, nil", n, got, err, n)
		}
	}
}

// The button starts the EARLIER of {previous month, current month} that has no
// retro row, so a couple doing July's retro on 2 August files it as July and
// August is still available afterwards (spec decision 5).
func TestStartableMonth(t *testing.T) {
	today := time.Date(2026, 8, 2, 21, 0, 0, 0, time.UTC)
	july := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	august := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name                          string
		currentExists, previousExists bool
		want                          time.Time
		wantOK                        bool
	}{
		{"neither exists: offers the missed month", false, false, july, true},
		{"previous exists: offers this month", false, true, august, true},
		{"only this month exists: offers the missed one", true, false, july, true},
		{"both exist: offers nothing", true, true, time.Time{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := domain.StartableMonth(today, c.currentExists, c.previousExists)
			if ok != c.wantOK || !got.Equal(c.want) {
				t.Fatalf("= %v, %v; want %v, %v", got, ok, c.want, c.wantOK)
			}
		})
	}
}

// January must walk back to the previous December, not to month zero.
func TestStartableMonthCrossesTheYear(t *testing.T) {
	today := time.Date(2027, 1, 4, 9, 0, 0, 0, time.UTC)
	want := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	got, ok := domain.StartableMonth(today, false, false)
	if !ok || !got.Equal(want) {
		t.Fatalf("= %v, %v; want %v, true", got, ok, want)
	}
}

func TestFirstSentence(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Best month this year. Agreed to keep the budget review.", "Best month this year."},
		{"Did we really do that?! Yes.", "Did we really do that?"},
		{"", ""},
		{"no terminator at all here", "no terminator at all here"},
		{"a very long note with no terminator that runs well past the sixty character budget we allow", "a very long note with no terminator that runs well past the s…"},
	}
	for _, c := range cases {
		if got := domain.FirstSentence(c.in); got != c.want {
			t.Fatalf("FirstSentence(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd api && go test ./internal/domain/ -run 'TestParseMood|TestStartableMonth|TestFirstSentence' -count=1 -v`
Expected: FAIL — undefined: `domain.ParseMood`.

- [ ] **Step 3: Write the implementation**

Create `api/internal/domain/retro.go`:

```go
package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Mood is how the month felt, 1 (worst) to 5 (best) -- the design's five
// emoji. It is a distinct type rather than an int so a caller cannot pass a
// count or an index where a mood belongs.
type Mood int

// ParseMood refuses anything outside 1..5. It fails closed because a mood
// arrives from two places we did not construct: a request body and a database
// column (CLAUDE.md, "Fail closed on values you did not construct").
func ParseMood(n int) (Mood, error) {
	if n < 1 || n > 5 {
		return 0, fmt.Errorf("%w: %d", ErrInvalidMood, n)
	}
	return Mood(n), nil
}

// StartableMonth answers which month the "Start retro" button begins: the
// EARLIER of {previous month, current month} that has no retro row yet.
//
// A couple doing July's retro on 2 August means July, not August -- the
// design's own example retro is dated Jun 28, near the edge of its month --
// and August stays available afterwards. When both months already have a
// retro there is nothing to start, and the page opens what exists instead.
//
// today is a parameter, never time.Now() reached for in here: every other
// date rule in this codebase takes its clock from the caller, which is what
// makes them testable without freezing time globally.
func StartableMonth(today time.Time, currentExists, previousExists bool) (time.Time, bool) {
	current := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	previous := current.AddDate(0, -1, 0)

	switch {
	case !previousExists:
		return previous, true
	case !currentExists:
		return current, true
	default:
		return time.Time{}, false
	}
}

// firstSentenceMax is the fallback budget for notes that never terminate a
// sentence. 60 characters is what fits the design's history row beside the
// mood and the action count without wrapping.
const firstSentenceMax = 60

// FirstSentence is the quoted line in a history row: the design renders
// `June 2026 · Mood 4/5 · 3 actions · "best month this year"`, and June's
// notes open with exactly that sentence. Derived rather than a second field
// nobody would fill twice (spec decision 7).
func FirstSentence(notes string) string {
	trimmed := strings.TrimSpace(notes)
	if trimmed == "" {
		return ""
	}
	if i := strings.IndexAny(trimmed, ".!?"); i >= 0 {
		return trimmed[:i+1]
	}
	if utf8.RuneCountInString(trimmed) <= firstSentenceMax {
		return trimmed
	}
	// Cut on a rune boundary: a note can hold any language, and slicing by
	// byte position would split a multi-byte character in half. This is the
	// same class of mistake domain.initialOf already exists to avoid.
	runes := []rune(trimmed)
	return string(runes[:firstSentenceMax-1]) + "…"
}
```

Add to `api/internal/domain/errors.go`, in the file's existing style (one sentinel, one comment saying when it is returned):

```go
	// ErrInvalidMood is returned when a mood outside 1..5 arrives from a
	// request body or a database column. Nothing defaults an invalid mood to
	// a valid one: a retro with no mood is a real state (NULL), and silently
	// rounding 7 to 5 would invent a feeling nobody recorded.
	ErrInvalidMood = errors.New("a mood must be between 1 and 5")

	// ErrRetroChanged is returned when a retro update carries a version older
	// than the stored one -- the other partner saved while this one was
	// typing. The write is refused, never merged: silently overwriting the
	// other person's paragraph is the failure this guard exists to prevent.
	ErrRetroChanged = errors.New("this retro changed while you were editing it")
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd api && go test ./internal/domain/ -run 'TestParseMood|TestStartableMonth|TestFirstSentence' -count=1 -v`
Expected: PASS, all four.

- [ ] **Step 5: Mutation-check `StartableMonth`**

Swap the two `case` arms so `!currentExists` is checked first. Expected: `TestStartableMonth/neither_exists:_offers_the_missed_month` goes red (it would return August, skipping the missed July). Restore, rerun, confirm green.

- [ ] **Step 6: Commit**

```bash
git add api/internal/domain/retro.go api/internal/domain/retro_test.go api/internal/domain/errors.go
git commit -m "feat(retros): the three rules that need no database"
```

---

### Task 3: Ports and the read side of the service

**Files:**
- Modify: `api/internal/usecase/ports.go`
- Create: `api/internal/usecase/retro.go`, `api/internal/usecase/retro_test.go`
- Modify: `api/internal/usecase/testdouble_test.go` (add the in-memory doubles)

**Interfaces:**
- Consumes: `domain.Mood`, `domain.StartableMonth`, `domain.FirstSentence` (Task 2).
- Produces:
  - `usecase.RetroRecord`, `usecase.RetroActionRecord`, `usecase.RetroSummary`
  - `usecase.RetroRepository`, `usecase.RetroActionRepository`
  - `usecase.RetroService` with `List(ctx, householdID string, today time.Time) (RetrosView, error)` and `Month(ctx, householdID string, month time.Time) (RetroView, error)`
  - `usecase.RetrosView{Summaries []RetroSummary, Mood []MoodPoint, DoneCount int, Since *time.Time, StartMonth *time.Time}`
  - `usecase.RetroView{Retro RetroRecord, Actions []RetroActionRecord, CarryOver []RetroActionRecord}`

- [ ] **Step 1: Write the ports**

Add to `api/internal/usecase/ports.go`. Doc comments here are load-bearing — they are the contract Task 5 implements against, and `ports.go` is the file `CLAUDE.md` names as the model for the rest:

```go
// RetroRepository stores one household's monthly retros. Every method is
// scoped by householdID and must filter on it in SQL: a retro that belongs to
// another household must be indistinguishable from one that does not exist.
type RetroRepository interface {
	// Create writes an empty draft for the month and returns it. A month that
	// already has a retro surfaces as domain.ErrAlreadyExists -- the UNIQUE
	// (household_id, month) constraint, translated, never a raw pgx error.
	// This is also what makes a double-clicked button harmless.
	Create(ctx context.Context, householdID string, month time.Time) (RetroRecord, error)
	// ByMonth reports domain.ErrNotFound when the month has no retro, which
	// the page reads as "not started" rather than as an error.
	ByMonth(ctx context.Context, householdID string, month time.Time) (RetroRecord, error)
	// List returns every retro, newest month first, each carrying its own
	// action count. Deliberately unbounded: a household writes twelve rows a
	// year, so a decade is 120 rows and one query, and the design's
	// "Show 2025 (7 more)" is a disclosure over data the page already holds,
	// not a second request. Do not add paging without a household the flat
	// list actually hurts.
	List(ctx context.Context, householdID string) ([]RetroSummary, error)
	// Update replaces mood and the three text columns, and bumps version, but
	// ONLY when the stored version equals u.Version. A mismatch returns
	// domain.ErrRetroChanged and writes nothing -- the other partner saved
	// while this one was typing, and merging the two would silently lose one
	// of them. The returned record carries the NEW version, so a caller never
	// has to guess what to send next.
	Update(ctx context.Context, u RetroUpdate) (RetroRecord, error)
	// Complete stamps completed_at with at. Idempotent: completing an already
	// finished retro leaves the original timestamp and is not an error, the
	// same shape GoalRepository.SetArchived takes.
	Complete(ctx context.Context, householdID, retroID string, at time.Time) (RetroRecord, error)
	// DeleteDraft removes a retro that has NOT been finished. The
	// completed_at IS NULL condition belongs in the WHERE clause, not in a
	// service if: a check-then-delete can race, and -- the reason that
	// matters here -- a zero-row match must report domain.ErrNotFound rather
	// than success. SetBillNextDue shipped the other way round and committed
	// two of three writes on a zero-row match (docs/LEARNING.md, database
	// catalogue).
	DeleteDraft(ctx context.Context, householdID, retroID string) error
}

// RetroActionRepository stores what a retro decided to do next month.
type RetroActionRepository interface {
	// Add writes the action AND its assignees inside one transaction: an
	// assignee that is not a membership of this household fails the whole
	// insert, so no orphan action survives a half-written assignment.
	Add(ctx context.Context, in RetroActionInput) (RetroActionRecord, error)
	// ForRetro returns a retro's actions in insertion order (created_at, id).
	// There is no position column to sort by -- see 00009_retros.sql for why.
	ForRetro(ctx context.Context, householdID, retroID string) ([]RetroActionRecord, error)
	// SetDone ticks or unticks. done=false clears done_at rather than
	// stamping a "not done" time. Reports domain.ErrNotFound on a zero-row
	// match, for the same reason DeleteDraft does.
	SetDone(ctx context.Context, householdID, actionID string, done bool, at time.Time) error
	// Remove hard-deletes an action. Nothing references an action except a
	// later action's carried_from, which is ON DELETE SET NULL, so removal
	// cannot orphan anything.
	Remove(ctx context.Context, householdID, actionID string) error
	// OpenInMonth returns that month's unticked actions -- the "Still open
	// from July" offer. The caller passes the immediately previous month
	// only: a household that skipped four months must not be handed an
	// unbounded backlog on the night it comes back (spec decision 4).
	OpenInMonth(ctx context.Context, householdID string, month time.Time) ([]RetroActionRecord, error)
}
```

And the records they exchange, in the same file, above the interfaces:

```go
// RetroRecord is one stored retro. Mood is a pointer because "nobody has
// picked an emoji yet" is a real state and 0 is not a mood; CompletedAt is a
// pointer for the same reason -- nil IS the draft concept.
type RetroRecord struct {
	ID          string
	Month       time.Time
	Mood        *int
	WentWell    string
	WasHard     string
	Notes       string
	CompletedAt *time.Time
	Version     int
}

// RetroSummary is one row of the history list: the stored retro plus the
// action count the row displays. The quoted line is derived from Notes by the
// service, not stored.
type RetroSummary struct {
	Retro       RetroRecord
	ActionCount int
}

// RetroActionInput is what Add receives. AssigneeMembershipIDs may be empty
// (an action nobody owns yet) or hold one or both owners; CarriedFrom is the
// id of last month's action when this one was carried, "" otherwise.
type RetroActionInput struct {
	HouseholdID           string
	RetroID               string
	Body                  string
	AssigneeMembershipIDs []string
	CarriedFrom           string
}

// RetroActionRecord is one action. DoneAt nil means open.
type RetroActionRecord struct {
	ID                    string
	RetroID               string
	Body                  string
	DoneAt                *time.Time
	CarriedFrom           string
	AssigneeMembershipIDs []string
}

// RetroUpdate is one save of the retro's own fields. Version is the version
// the editor loaded; the repository refuses the write when it no longer
// matches. Mood nil clears the mood, which a household can legitimately do.
type RetroUpdate struct {
	HouseholdID string
	RetroID     string
	Mood        *int
	WentWell    string
	WasHard     string
	Notes       string
	Version     int
}
```

- [ ] **Step 2: Write the failing service tests**

Create `api/internal/usecase/retro_test.go`. These run against in-memory doubles, the existing house pattern:

```go
package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func aug2026() time.Time { return time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) }
func jul2026() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }

// A draft is not a data point. It shows on the page as its own in-progress
// entry, and it is excluded from the finished count and the mood chart --
// a half-typed month must not become a point on a mood trend (decision 2).
func TestRetroListExcludesDraftsFromTheCountAndTheChart(t *testing.T) {
	retros := newRetroRepoDouble()
	finished := retros.seed(jul2026(), 4, "Best month this year. And more.", true)
	retros.seed(aug2026(), 5, "", false) // the draft

	svc := usecase.NewRetroService(retros, newRetroActionRepoDouble())
	view, err := svc.List(context.Background(), "hh", time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if view.DoneCount != 1 {
		t.Fatalf("DoneCount = %d, want 1 (the draft must not count)", view.DoneCount)
	}
	if len(view.Summaries) != 2 {
		t.Fatalf("len(Summaries) = %d, want 2 (the draft still shows on the page)", len(view.Summaries))
	}
	for _, p := range view.Mood {
		if p.Month.Equal(aug2026()) && p.HasMood {
			t.Fatal("the draft's mood reached the chart")
		}
	}
	if view.Since == nil || !view.Since.Equal(finished.Month) {
		t.Fatalf("Since = %v, want %v (the earliest FINISHED month)", view.Since, finished.Month)
	}
}

// Twelve points ending at the current month, gaps for months with no finished
// retro. A gap is never a zero -- zero is a claim, the same rule Budget
// applies to transactions it cannot convert.
func TestRetroMoodSeriesIsTwelveMonthsWithGaps(t *testing.T) {
	retros := newRetroRepoDouble()
	retros.seed(jul2026(), 3, "", true)

	svc := usecase.NewRetroService(retros, newRetroActionRepoDouble())
	view, err := svc.List(context.Background(), "hh", time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(view.Mood) != 12 {
		t.Fatalf("len(Mood) = %d, want 12", len(view.Mood))
	}
	if got := view.Mood[11].Month; !got.Equal(aug2026()) {
		t.Fatalf("last point = %v, want the current month %v", got, aug2026())
	}
	var withMood int
	for _, p := range view.Mood {
		if p.HasMood {
			withMood++
			if !p.Month.Equal(jul2026()) || p.Mood != 3 {
				t.Fatalf("unexpected point %+v", p)
			}
		}
	}
	if withMood != 1 {
		t.Fatalf("%d points carry a mood, want 1", withMood)
	}
}

// The quoted line in a history row is the first sentence of the notes, and a
// retro with no notes renders no quote at all -- not empty quotation marks.
func TestRetroSummaryQuoteIsDerivedFromNotes(t *testing.T) {
	retros := newRetroRepoDouble()
	retros.seed(jul2026(), 4, "Best month this year. Agreed to keep the budget review.", true)
	retros.seed(jun2026(), 2, "", true)

	svc := usecase.NewRetroService(retros, newRetroActionRepoDouble())
	view, err := svc.List(context.Background(), "hh", time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if got := view.Summaries[0].Quote; got != "Best month this year." {
		t.Fatalf("quote = %q", got)
	}
	if got := view.Summaries[1].Quote; got != "" {
		t.Fatalf("empty notes produced quote %q, want none", got)
	}
}

// The carry-over offer reads the IMMEDIATELY previous month only.
func TestRetroMonthOffersOnlyLastMonthsOpenActions(t *testing.T) {
	retros := newRetroRepoDouble()
	aug := retros.seed(aug2026(), 0, "", false)
	actions := newRetroActionRepoDouble()
	actions.seedOpen(jul2026(), "phone-free dinners")
	actions.seedOpen(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), "should not appear")

	svc := usecase.NewRetroService(retros, actions)
	view, err := svc.Month(context.Background(), "hh", aug.Month)
	if err != nil {
		t.Fatalf("Month: %v", err)
	}

	if len(view.CarryOver) != 1 || view.CarryOver[0].Body != "phone-free dinners" {
		t.Fatalf("CarryOver = %+v, want only July's open action", view.CarryOver)
	}
}
```

Add `func jun2026() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) }` beside the other month helpers.

- [ ] **Step 3: Run them and watch them fail**

Run: `cd api && go test ./internal/usecase/ -run TestRetro -count=1 -v`
Expected: FAIL — undefined: `usecase.NewRetroService`, `newRetroRepoDouble`.

- [ ] **Step 4: Write the in-memory doubles**

Add to `api/internal/usecase/testdouble_test.go`, matching the file's existing doubles (a struct holding a slice, methods satisfying the port, no database). The doubles must honour the contract, errors included — `Update` refuses a stale version and returns `domain.ErrRetroChanged`, `DeleteDraft` returns `domain.ErrNotFound` for a finished retro. A double that is more permissive than the real adapter makes every test that uses it a lie (Liskov, `CLAUDE.md`).

- [ ] **Step 5: Write the service read path**

Create `api/internal/usecase/retro.go` with `RetroService`, `NewRetroService(retros RetroRepository, actions RetroActionRepository) *RetroService`, and the two read methods. Every derived figure comes from the spec's pinned table and gets a comment saying so:

- `List` — summaries newest-first with `Quote: domain.FirstSentence(notes)` and the stored action count; `DoneCount` = finished retros; `Since` = earliest finished month, nil when none; `Mood` = twelve `MoodPoint{Month, Mood, HasMood}` ending at the current month, `HasMood` false for a month with no finished retro *or* a finished retro whose mood is nil; `StartMonth` from `domain.StartableMonth`, nil when both months exist.
- `Month` — the retro, its actions, and `CarryOver` from `OpenInMonth(previous month)`. When the month has no retro, return `domain.ErrNotFound` untouched.

- [ ] **Step 6: Run the tests and watch them pass**

Run: `cd api && go test ./internal/usecase/ -run TestRetro -count=1 -v`
Expected: PASS, all four.

- [ ] **Step 7: Mutation-check the draft exclusion**

Drop the `CompletedAt != nil` condition from the mood-series builder. Expected: `TestRetroListExcludesDraftsFromTheCountAndTheChart` goes red on "the draft's mood reached the chart". Restore.

- [ ] **Step 8: Commit**

```bash
git add api/internal/usecase/ports.go api/internal/usecase/retro.go api/internal/usecase/retro_test.go api/internal/usecase/testdouble_test.go
git commit -m "feat(retros): the ports and every figure the screen displays"
```

---

### Task 4: The write side of the service

**Files:**
- Modify: `api/internal/usecase/retro.go`, `api/internal/usecase/retro_test.go`

**Interfaces:**
- Consumes: everything Task 3 produced.
- Produces, on `*RetroService`:
  - `Start(ctx, householdID string, today time.Time) (RetroRecord, error)`
  - `Save(ctx, u RetroUpdate) (RetroRecord, error)`
  - `Finish(ctx, householdID, retroID string, at time.Time) (RetroRecord, error)`
  - `DiscardDraft(ctx, householdID, retroID string) error`
  - `AddAction(ctx, in RetroActionInput) (RetroActionRecord, error)`
  - `SetActionDone(ctx, householdID, actionID string, done bool, at time.Time) error`
  - `RemoveAction(ctx, householdID, actionID string) error`

- [ ] **Step 1: Write the failing tests**

Append to `api/internal/usecase/retro_test.go`:

```go
// Start files the retro against the month the button offered, not against
// today: a couple doing July's retro on 2 August means July (decision 5).
func TestRetroStartUsesTheStartableMonth(t *testing.T) {
	retros := newRetroRepoDouble()
	svc := usecase.NewRetroService(retros, newRetroActionRepoDouble())

	got, err := svc.Start(context.Background(), "hh", time.Date(2026, 8, 2, 21, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !got.Month.Equal(jul2026()) {
		t.Fatalf("month = %v, want July -- the missed month, not today's", got.Month)
	}
}

// Both months already have a retro: there is nothing to start, and the
// service says so rather than inventing a third month.
func TestRetroStartRefusesWhenBothMonthsExist(t *testing.T) {
	retros := newRetroRepoDouble()
	retros.seed(jul2026(), 4, "", true)
	retros.seed(aug2026(), 0, "", false)
	svc := usecase.NewRetroService(retros, newRetroActionRepoDouble())

	if _, err := svc.Start(context.Background(), "hh", time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("Start succeeded with both months already present")
	}
}

// A mood arriving from a request body is validated, not trusted.
func TestRetroSaveRefusesAnImpossibleMood(t *testing.T) {
	retros := newRetroRepoDouble()
	r := retros.seed(aug2026(), 0, "", false)
	svc := usecase.NewRetroService(retros, newRetroActionRepoDouble())

	seven := 7
	_, err := svc.Save(context.Background(), usecase.RetroUpdate{
		HouseholdID: "hh", RetroID: r.ID, Mood: &seven, Version: r.Version,
	})
	if !errors.Is(err, domain.ErrInvalidMood) {
		t.Fatalf("err = %v, want ErrInvalidMood", err)
	}
	if retros.writes != 0 {
		t.Fatalf("%d writes reached the repository, want 0", retros.writes)
	}
}

// The version guard: the other partner saved while this one was typing.
func TestRetroSaveRefusesAStaleVersion(t *testing.T) {
	retros := newRetroRepoDouble()
	r := retros.seed(aug2026(), 0, "", false)
	svc := usecase.NewRetroService(retros, newRetroActionRepoDouble())

	if _, err := svc.Save(context.Background(), usecase.RetroUpdate{
		HouseholdID: "hh", RetroID: r.ID, Notes: "mine", Version: r.Version,
	}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	_, err := svc.Save(context.Background(), usecase.RetroUpdate{
		HouseholdID: "hh", RetroID: r.ID, Notes: "theirs", Version: r.Version,
	})
	if !errors.Is(err, domain.ErrRetroChanged) {
		t.Fatalf("err = %v, want ErrRetroChanged", err)
	}
}

// Ticking an action must not bump the retro's version: one partner ticking
// all month cannot be allowed to invalidate the other's open editor.
func TestTickingAnActionLeavesTheRetroVersionAlone(t *testing.T) {
	retros := newRetroRepoDouble()
	r := retros.seed(aug2026(), 0, "", false)
	actions := newRetroActionRepoDouble()
	svc := usecase.NewRetroService(retros, actions)

	action, err := svc.AddAction(context.Background(), usecase.RetroActionInput{
		HouseholdID: "hh", RetroID: r.ID, Body: "book the getaway",
	})
	if err != nil {
		t.Fatalf("AddAction: %v", err)
	}
	if err := svc.SetActionDone(context.Background(), "hh", action.ID, true, time.Now()); err != nil {
		t.Fatalf("SetActionDone: %v", err)
	}

	after, err := retros.ByMonth(context.Background(), "hh", aug2026())
	if err != nil {
		t.Fatalf("ByMonth: %v", err)
	}
	if after.Version != r.Version {
		t.Fatalf("version moved from %d to %d", r.Version, after.Version)
	}
}

// An action with no body is refused: the design's own control is "+ Add an
// action & assign it to one of you", and a blank row on the detail view is
// indistinguishable from a rendering bug.
func TestAddActionRefusesAnEmptyBody(t *testing.T) {
	retros := newRetroRepoDouble()
	r := retros.seed(aug2026(), 0, "", false)
	svc := usecase.NewRetroService(retros, newRetroActionRepoDouble())

	if _, err := svc.AddAction(context.Background(), usecase.RetroActionInput{
		HouseholdID: "hh", RetroID: r.ID, Body: "   ",
	}); err == nil {
		t.Fatal("a whitespace-only action was accepted")
	}
}
```

Add `"errors"` and the `domain` import to the test file's import block.

- [ ] **Step 2: Run them and watch them fail**

Run: `cd api && go test ./internal/usecase/ -run TestRetro -count=1 -v`
Expected: FAIL — undefined methods on `*RetroService`.

- [ ] **Step 3: Implement the write path**

In `api/internal/usecase/retro.go`. Points that must not be lost:

- `Start` computes the month with `domain.StartableMonth` from the existing retros, and returns a plain error when there is none. It does not fall back to "today's month anyway".
- `Save` validates the mood with `domain.ParseMood` **before** touching the repository, trims the three text fields' trailing whitespace, and passes `Version` straight through — the refusal itself belongs to the repository, which is the only layer that can compare against the stored value atomically.
- `AddAction` refuses a blank body with a domain error and trims the body.
- `Finish` is idempotent; `DiscardDraft` passes through to `DeleteDraft` and lets `domain.ErrNotFound` surface for a finished retro.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd api && go test ./internal/usecase/ -run TestRetro -count=1 -v`
Expected: PASS, all ten in the file.

- [ ] **Step 5: Mutation-check the mood validation**

Delete the `domain.ParseMood` call in `Save`. Expected: `TestRetroSaveRefusesAnImpossibleMood` goes red — and note *which* assertion fires: if only the `errors.Is` line fails and the write count still reads 0, the test is weaker than it looks and the ordering assertion needs fixing before you restore.

- [ ] **Step 6: Commit**

```bash
git add api/internal/usecase/retro.go api/internal/usecase/retro_test.go
git commit -m "feat(retros): start, save, finish, and the guard that refuses a stale save"
```

---

### Task 5: The retro repository

**Files:**
- Create: `api/internal/adapter/postgres/queries/retro.sql`, `api/internal/adapter/postgres/retro_repo.go`, `api/internal/adapter/postgres/retro_repo_test.go`
- Modify: `api/internal/adapter/postgres/sqlcgen/**` (regenerated, never hand-edited)

**Interfaces:**
- Consumes: `usecase.RetroRepository` (Task 3), the tables from Task 1.
- Produces: `postgres.NewRetroRepo(db *DB) *RetroRepo`, satisfying `usecase.RetroRepository`.

- [ ] **Step 1: Write the failing tests**

Create `api/internal/adapter/postgres/retro_repo_test.go`, following `goal_repo_test.go`'s shape (testcontainers via the shared helper, one household seeded per test). These four are the things only a real database can prove:

```go
// Two editors, one draft. The second save carries the version the first one
// invalidated, and it must be refused outright -- not merged, not applied.
func TestRetroUpdateRefusesAStaleVersionAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	repo, householdID := newRetroRepo(t)

	draft, err := repo.Create(ctx, householdID, jul2026())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	first, err := repo.Update(ctx, usecase.RetroUpdate{
		HouseholdID: householdID, RetroID: draft.ID, Notes: "mine", Version: draft.Version,
	})
	if err != nil {
		t.Fatalf("first update: %v", err)
	}
	if first.Version != draft.Version+1 {
		t.Fatalf("version = %d, want %d", first.Version, draft.Version+1)
	}

	_, err = repo.Update(ctx, usecase.RetroUpdate{
		HouseholdID: householdID, RetroID: draft.ID, Notes: "theirs", Version: draft.Version,
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

// A zero-row match must be an error, not a silent success. This is the
// SetBillNextDue defect, written as a test before the code exists.
func TestDeleteDraftRefusesAFinishedRetro(t *testing.T) {
	ctx := context.Background()
	repo, householdID := newRetroRepo(t)

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
	repo, householdID := newRetroRepo(t)

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
	repo, householdID := newRetroRepo(t)
	other := seedSecondHousehold(t)

	if _, err := repo.Create(ctx, other, jul2026()); err != nil {
		t.Fatalf("create in other household: %v", err)
	}
	if _, err := repo.ByMonth(ctx, householdID, jul2026()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
```

Write `newRetroRepo(t)` and `seedSecondHousehold(t)` beside them using the helpers `goal_repo_test.go` already uses for the same job — do not invent a second seeding path.

- [ ] **Step 2: Run them and watch them fail**

Run: `cd api && go test ./internal/adapter/postgres/ -run TestRetro -count=1 -v`
Expected: FAIL — undefined: `postgres.NewRetroRepo`.

- [ ] **Step 3: Write the queries**

Create `api/internal/adapter/postgres/queries/retro.sql`. The update is where the guard lives, and it is one statement on purpose — a `SELECT version` followed by an `UPDATE` is two statements with a race between them:

```sql
-- name: CreateRetro :one
INSERT INTO retros (household_id, month)
VALUES ($1, $2)
RETURNING id, month, mood, went_well, was_hard, notes, completed_at, version;

-- name: GetRetroByMonth :one
SELECT id, month, mood, went_well, was_hard, notes, completed_at, version
FROM retros
WHERE household_id = $1 AND month = $2;

-- name: ListRetros :many
SELECT r.id, r.month, r.mood, r.went_well, r.was_hard, r.notes, r.completed_at, r.version,
       (SELECT count(*) FROM retro_actions a WHERE a.retro_id = r.id) AS action_count
FROM retros r
WHERE r.household_id = $1
ORDER BY r.month DESC;

-- name: UpdateRetro :one
-- The version comparison is IN the WHERE clause: a select-then-update pair
-- would leave a window where both partners read the same version and both
-- write. Zero rows updated means either the retro is gone or the version
-- moved, and the repository distinguishes the two before answering.
UPDATE retros
SET mood = $3, went_well = $4, was_hard = $5, notes = $6,
    version = version + 1, updated_at = now()
WHERE household_id = $1 AND id = $2 AND version = $7
RETURNING id, month, mood, went_well, was_hard, notes, completed_at, version;

-- name: CompleteRetro :one
-- coalesce keeps the first completion time: finishing twice is idempotent,
-- the shape SetArchived already uses.
UPDATE retros
SET completed_at = coalesce(completed_at, $3), updated_at = now()
WHERE household_id = $1 AND id = $2
RETURNING id, month, mood, went_well, was_hard, notes, completed_at, version;

-- name: DeleteDraftRetro :execrows
DELETE FROM retros
WHERE household_id = $1 AND id = $2 AND completed_at IS NULL;

-- name: RetroExistsForMonth :one
SELECT EXISTS (SELECT 1 FROM retros WHERE household_id = $1 AND month = $2);
```

- [ ] **Step 4: Regenerate sqlc and write the repository**

Run sqlc the way this repo already does (`make` lists the target; the generated package is `internal/adapter/postgres/sqlcgen` and is never hand-edited). Then write `retro_repo.go` modelled on `goal_repo.go`: a struct holding `*sqlcgen.Queries` and the pool, `translate(err, "…")` on every error, `uuid(...)` for ids, and a `toRetroRecord` converter.

The two error paths that need thought, not typing:

```go
func (r *RetroRepo) Update(ctx context.Context, u usecase.RetroUpdate) (usecase.RetroRecord, error) {
	row, err := r.q.UpdateRetro(ctx, sqlcgen.UpdateRetroParams{ /* … */ })
	if errors.Is(err, pgx.ErrNoRows) {
		// Zero rows means one of two things, and the caller needs to know
		// which: the retro is gone (404), or the other partner saved first
		// (409, with copy telling this one to reload). One more cheap read
		// separates them -- without it a deleted retro would report a
		// conflict, sending the editor to reload a page that no longer has
		// anything to show.
		if _, missing := r.ByMonth(ctx, u.HouseholdID, u.Month); errors.Is(missing, domain.ErrNotFound) {
			return usecase.RetroRecord{}, domain.ErrNotFound
		}
		return usecase.RetroRecord{}, domain.ErrRetroChanged
	}
	if err != nil {
		return usecase.RetroRecord{}, translate(err, "update retro")
	}
	return toRetroRecord(row.ID, row.Month, row.Mood, row.WentWell, row.WasHard, row.Notes, row.CompletedAt, row.Version)
}

func (r *RetroRepo) DeleteDraft(ctx context.Context, householdID, retroID string) error {
	n, err := r.q.DeleteDraftRetro(ctx, sqlcgen.DeleteDraftRetroParams{ /* … */ })
	if err != nil {
		return translate(err, "delete draft retro")
	}
	// A zero-row DELETE is not success. It means the retro is finished, or
	// belongs to another household, or never existed -- all of which the
	// caller must see as ErrNotFound rather than as "done".
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
```

`RetroUpdate` needs `Month` for that lookup — add the field in Task 3's struct if you are reading these out of order, and have the HTTP layer fill it from the URL.

- [ ] **Step 5: Run the tests and watch them pass**

Run: `cd api && go test ./internal/adapter/postgres/ -run TestRetro -count=1 -v`
Expected: PASS, all four.

- [ ] **Step 6: Mutation-check the version guard**

Remove `AND version = $7` from `UpdateRetro`, regenerate, rerun. Expected: `TestRetroUpdateRefusesAStaleVersionAndWritesNothing` goes red on the notes assertion — the refused write landed. Restore and regenerate.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/postgres/queries/retro.sql api/internal/adapter/postgres/retro_repo.go api/internal/adapter/postgres/retro_repo_test.go api/internal/adapter/postgres/sqlcgen
git commit -m "feat(retros): the repository, with the conflict decided in SQL"
```

---

### Task 6: The action repository

**Files:**
- Modify: `api/internal/adapter/postgres/queries/retro.sql`
- Create: `api/internal/adapter/postgres/retro_action_repo.go`, `api/internal/adapter/postgres/retro_action_repo_test.go`

**Interfaces:**
- Consumes: `usecase.RetroActionRepository` (Task 3).
- Produces: `postgres.NewRetroActionRepo(db *DB) *RetroActionRepo`.

- [ ] **Step 1: Write the failing tests**

```go
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
func TestAddActionKeepsBothAssignees(t *testing.T) { /* seed two memberships, assert both come back from ForRetro */ }

// OpenInMonth is the carry-over offer: that month, unticked only.
func TestOpenInMonthReturnsOnlyThatMonthsUntickedActions(t *testing.T) { /* seed July done + July open + June open; assert one row */ }

// Unticking clears the stamp rather than recording a "not done" time.
func TestSetDoneFalseClearsTheTimestamp(t *testing.T) { /* tick, untick, assert DoneAt == nil */ }
```

Write the three sketched bodies out in full when implementing — they are sketched here only because their assertions are one line each; a plan that says "similar to the above" is a plan failure, so copy the first test's structure rather than referring to it.

- [ ] **Step 2: Run them and watch them fail**

Run: `cd api && go test ./internal/adapter/postgres/ -run 'TestAddAction|TestOpenInMonth|TestSetDone' -count=1 -v`
Expected: FAIL — undefined: `postgres.NewRetroActionRepo`.

- [ ] **Step 3: Write the queries and the repository**

Append to `queries/retro.sql`: `AddRetroAction`, `AddRetroActionAssignee`, `ListRetroActions` (joined to assignees, `ORDER BY a.created_at, a.id`), `SetRetroActionDone`, `DeleteRetroAction`, `ListOpenActionsInMonth`. Every one is scoped by `household_id` through a join on `retros`, because an action carries no household of its own.

`Add` opens a transaction with `pgx.BeginFunc`, the same shape `GoalRepo.Create` uses, and writes the action then each assignee inside it.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd api && go test ./internal/adapter/postgres/ -run 'TestAddAction|TestOpenInMonth|TestSetDone' -count=1 -v`
Expected: PASS, all four.

- [ ] **Step 5: Mutation-check the transaction**

Move the assignee inserts outside `pgx.BeginFunc` (write the action, commit, then insert assignees). Expected: `TestAddActionWithABadAssigneeWritesNothingAtAll` goes red with one surviving action. Restore.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/postgres/queries/retro.sql api/internal/adapter/postgres/retro_action_repo.go api/internal/adapter/postgres/retro_action_repo_test.go api/internal/adapter/postgres/sqlcgen
git commit -m "feat(retros): actions and their assignees, written as one thing"
```

---

### Task 7: The HTTP read routes and the guard

**Files:**
- Create: `api/internal/adapter/http/retro_handlers.go`, `api/internal/adapter/http/marriage_api_test.go`
- Modify: `api/internal/adapter/http/router.go`, `api/internal/adapter/http/errors.go`, `api/cmd/api/main.go`

**Interfaces:**
- Consumes: `usecase.RetroService` (Tasks 3–4), `postgres.NewRetroRepo` / `NewRetroActionRepo` (Tasks 5–6).
- Produces: `GET /api/v1/retros`, `GET /api/v1/retros/{month}`, and the `retroDTO` / `retroSummaryDTO` / `moodPointDTO` JSON shapes every frontend task decodes.

- [ ] **Step 1: Write the failing route-walk matrix**

Create `api/internal/adapter/http/marriage_api_test.go`. It is its own file rather than a fifth block in `api_test.go`, which was already split by feature area for exactly this reason:

```go
// Every marriage route, against every member state. A limited member cannot
// hold the marriage capability at all (domain.ErrLimitedCannotHoldMarriage),
// so 403 is the only correct answer for one -- and the route must answer it
// from its own guard, not by leaning on that invariant.
func TestMarriageRoutesRequireMarriageAndOwner(t *testing.T) {
	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/retros"},
		{http.MethodGet, "/api/v1/retros/2026-07"},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			// no session at all
			assertStatus(t, requestAs(t, nil, c.method, c.path, nil), http.StatusUnauthorized)
			// a limited member
			assertStatus(t, requestAs(t, limitedSession(t), c.method, c.path, nil), http.StatusForbidden)
			// the owner
			assertStatusNot(t, requestAs(t, ownerSession(t), c.method, c.path, nil), http.StatusForbidden)
		})
	}
}

// The month in the URL is the budgets format, and a malformed one is refused
// before it reaches the service.
func TestGetRetroRejectsAMalformedMonth(t *testing.T) {
	assertStatus(t, requestAs(t, ownerSession(t), http.MethodGet, "/api/v1/retros/July", nil), http.StatusBadRequest)
}

// A month with no retro is "not started", not an error the page shouts about.
func TestGetRetroForAnEmptyMonthIs404(t *testing.T) {
	assertStatus(t, requestAs(t, ownerSession(t), http.MethodGet, "/api/v1/retros/2001-01", nil), http.StatusNotFound)
}
```

Use whatever the existing helpers in `api_test.go` are actually called — read them rather than assuming these names; the shapes above are the assertions that matter.

- [ ] **Step 2: Run it and watch it fail**

Run: `cd api && go test ./internal/adapter/http/ -run TestMarriage -count=1 -v`
Expected: FAIL — 404 on every route, because none exist.

- [ ] **Step 3: Write the DTOs and the read handlers**

`retro_handlers.go`, modelled on `goal_handlers.go`. The JSON shape, which every frontend task depends on:

```go
type retroDTO struct {
	ID          string     `json:"id"`
	Month       string     `json:"month"`       // "2026-07"
	Mood        *int       `json:"mood"`        // null when nobody has picked
	WentWell    string     `json:"wentWell"`
	WasHard     string     `json:"wasHard"`
	Notes       string     `json:"notes"`
	CompletedAt *time.Time `json:"completedAt"` // null means draft
	Version     int        `json:"version"`
	Actions     []retroActionDTO `json:"actions"`
}

type retroActionDTO struct {
	ID          string     `json:"id"`
	Body        string     `json:"body"`
	DoneAt      *time.Time `json:"doneAt"`
	CarriedFrom string     `json:"carriedFrom"` // "" when not carried
	AssigneeMembershipIDs []string `json:"assigneeMembershipIds"`
}

type retroSummaryDTO struct {
	ID          string `json:"id"`
	Month       string `json:"month"`
	Mood        *int   `json:"mood"`
	ActionCount int    `json:"actionCount"`
	Quote       string `json:"quote"`     // "" renders no quotation marks at all
	Finished    bool   `json:"finished"`
}

type moodPointDTO struct {
	Month string `json:"month"`
	Mood  *int   `json:"mood"` // null is a gap, never 0
}

type retrosResponse struct {
	Retros     []retroSummaryDTO `json:"retros"`
	Mood       []moodPointDTO    `json:"mood"`
	DoneCount  int               `json:"doneCount"`
	Since      *string           `json:"since"`      // "2025-08", or null
	StartMonth *string           `json:"startMonth"` // null when both months exist
}

type retroResponse struct {
	Retro     retroDTO         `json:"retro"`
	CarryOver []retroActionDTO `json:"carryOver"`
}
```

Parse `{month}` with **the function `handleGetBudgetMonth` already uses** — find it in `budget_handlers.go` and call it; do not write a second month parser two files away (`docs/LEARNING.md` pattern 1's most common shape here).

In `router.go`, add the group beside the money group, with a comment that says why the guard is stacked:

```go
// Marriage is parents-only, and its capability is refused to limited members
// in the domain (domain.ErrLimitedCannotHoldMarriage) -- so requireOwner is
// redundant here TODAY. It is stacked anyway, for the reason this file's
// money-group comment already gives: a route leaning on an invariant enforced
// in another layer for another reason opens silently the day that invariant
// is relaxed, with no failing test to catch it.
g.Group(func(m chi.Router) {
	m.Use(requireCapability(domain.CapMarriage))
	m.Use(requireOwner)

	m.Get("/retros", handleListRetros(deps))
	m.Get("/retros/{month}", handleGetRetro(deps))
})
```

Wire `RetroService` into `deps` in `cmd/api/main.go`, where every other service is chosen.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd api && go test ./internal/adapter/http/ -run TestMarriage -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Mutation-check the guard**

Remove `m.Use(requireOwner)`. Expected: nothing goes red today, because no limited member can hold marriage — **that is the finding, not a pass**. Add a test that constructs a membership holding `marriage` without owner role directly at the repository level (bypassing `ValidateMembershipChange`, the way a future relaxation would) and asserts 403. Then the mutation goes red. Restore.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/http/retro_handlers.go api/internal/adapter/http/marriage_api_test.go api/internal/adapter/http/router.go api/cmd/api/main.go
git commit -m "feat(retros): read routes, behind a guard that does not lean on another layer"
```

---

### Task 8: The HTTP write routes

**Files:**
- Modify: `api/internal/adapter/http/retro_handlers.go`, `api/internal/adapter/http/router.go`, `api/internal/adapter/http/errors.go`, `api/internal/adapter/http/marriage_api_test.go`

**Interfaces:**
- Consumes: Task 7's DTOs and group.
- Produces: `POST /retros`, `PATCH /retros/{month}`, `POST /retros/{month}/complete`, `DELETE /retros/{month}`, `POST /retros/{month}/actions`, `PATCH /retros/{month}/actions/{id}`, `DELETE /retros/{month}/actions/{id}`; error codes `RETRO_CHANGED`, `RETRO_EXISTS`.

- [ ] **Step 1: Write the failing tests**

Append to `marriage_api_test.go`:

```go
// Every write is behind CSRF, like every other write in this API.
func TestMarriageWritesRequireCSRF(t *testing.T) {
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/retros"},
		{http.MethodPatch, "/api/v1/retros/2026-07"},
		{http.MethodPost, "/api/v1/retros/2026-07/complete"},
		{http.MethodDelete, "/api/v1/retros/2026-07"},
		{http.MethodPost, "/api/v1/retros/2026-07/actions"},
	} {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			assertStatus(t, requestWithoutCSRF(t, ownerSession(t), c.method, c.path, nil), http.StatusForbidden)
		})
	}
}

// The conflict the whole version column exists for, answered as a 409 with a
// code the frontend can branch on -- not a 500, and not a silent merge.
func TestPatchRetroWithAStaleVersionIs409RetroChanged(t *testing.T) {
	s := ownerSession(t)
	created := mustPostRetro(t, s)

	firstBody := `{"mood":4,"wentWell":"mine","wasHard":"","notes":"","version":` + itoa(created.Version) + `}`
	assertStatus(t, requestAs(t, s, http.MethodPatch, "/api/v1/retros/"+created.Month, firstBody), http.StatusOK)

	res := requestAs(t, s, http.MethodPatch, "/api/v1/retros/"+created.Month, firstBody)
	assertStatus(t, res, http.StatusConflict)
	assertErrorCode(t, res, "RETRO_CHANGED")
}

// Starting the same month twice answers 409 rather than creating a second
// row -- which is also what makes a double-clicked button harmless.
func TestPostRetroTwiceIs409(t *testing.T) {
	s := ownerSession(t)
	mustPostRetro(t, s)
	res := requestAs(t, s, http.MethodPost, "/api/v1/retros", `{}`)
	assertStatus(t, res, http.StatusConflict)
	assertErrorCode(t, res, "RETRO_EXISTS")
}

// A finished retro cannot be deleted, and the refusal is the same 404 a
// missing one gets -- the state is "there is no draft here".
func TestDeleteAFinishedRetroIs404(t *testing.T) {
	s := ownerSession(t)
	created := mustPostRetro(t, s)
	assertStatus(t, requestAs(t, s, http.MethodPost, "/api/v1/retros/"+created.Month+"/complete", `{}`), http.StatusOK)
	assertStatus(t, requestAs(t, s, http.MethodDelete, "/api/v1/retros/"+created.Month, nil), http.StatusNotFound)
}

// 204 is the only 2xx allowed to carry no body, because apiFetch throws on an
// ok response it cannot parse.
func TestEveryRetroWriteAnswersJSONExceptDelete(t *testing.T) { /* POST, PATCH, complete return a parseable body; DELETE returns 204 with none */ }
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd api && go test ./internal/adapter/http/ -run 'TestMarriage|TestPatchRetro|TestPostRetro|TestDelete' -count=1 -v`
Expected: FAIL — 405/404 on the write routes.

- [ ] **Step 3: Write the handlers and the error mapping**

Add to `errors.go`, in its existing switch:

```go
	case errors.Is(err, domain.ErrRetroChanged):
		WriteError(w, http.StatusConflict, "RETRO_CHANGED",
			"Someone else saved this retro while you were editing it. Reload to see their changes.", nil)
	case errors.Is(err, domain.ErrInvalidMood):
		WriteError(w, http.StatusBadRequest, "INVALID_MOOD", "That is not a mood we can record.", nil)
```

`POST /retros` takes no body worth reading: the month comes from `RetroService.Start`, which computes it from the household's own state and the clock. A client-supplied month would let a stale tab file a retro against a month the button never offered.

`PATCH` decodes `{mood, wentWell, wasHard, notes, version}` — `mood` is `*int` so `null` clears it — and returns the retro **with its new version**.

The complete/actions routes join the same CSRF sub-group, in the shape `router.go` already uses for goals and bills.

- [ ] **Step 4: Run the tests and watch them pass**

Run the same command. Expected: PASS.

- [ ] **Step 5: Mutation-check the conflict mapping**

Map `domain.ErrRetroChanged` to `http.StatusInternalServerError`. Expected: `TestPatchRetroWithAStaleVersionIs409RetroChanged` goes red on the status. Restore.

- [ ] **Step 6: Commit**

```bash
git add api/internal/adapter/http/
git commit -m "feat(retros): write routes, and a 409 the screen can explain"
```

---

### Task 9: Frontend schemas and hooks

**Files:**
- Create: `web/src/features/marriage/retroSchemas.ts`, `web/src/features/marriage/useRetros.ts`, `web/src/features/marriage/useRetro.ts`, `web/src/features/marriage/useRetros.test.ts`

**Interfaces:**
- Consumes: the JSON from Tasks 7–8.
- Produces: `useRetros()`, `useRetro(month)`, and the mutations `startRetro`, `saveRetro`, `finishRetro`, `discardDraft`, `addAction`, `setActionDone`, `removeAction`; types `Retro`, `RetroSummary`, `RetroAction`, `MoodPoint`.

- [ ] **Step 1: Write the failing tests**

Create `useRetros.test.ts`, modelled on `useGoals.test.ts`, with `stubFetchRoutes` for every request:

```ts
it("saveRetro sends the version it loaded and stores the one it gets back", async () => {
  const stub = stubFetchRoutes({
    "GET /api/v1/retros/2026-07": { retro: retroFixture({ version: 3 }), carryOver: [] },
    "PATCH /api/v1/retros/2026-07": { retro: retroFixture({ version: 4, notes: "saved" }) },
  });

  const { result } = renderHook(() => useRetro("2026-07"), { wrapper });
  await waitFor(() => expect(result.current.data?.retro.version).toBe(3));

  await act(() => result.current.saveRetro({ notes: "saved" }));

  expect(stub.bodyOf("PATCH /api/v1/retros/2026-07")).toMatchObject({ version: 3 });
  await waitFor(() => expect(result.current.data?.retro.version).toBe(4));
});

it("surfaces a 409 as a conflict the page can branch on, not as a generic error", async () => {
  stubFetchRoutes({
    "GET /api/v1/retros/2026-07": { retro: retroFixture({ version: 3 }), carryOver: [] },
    "PATCH /api/v1/retros/2026-07": {
      status: 409,
      body: { error: { code: "RETRO_CHANGED", message: "Someone else saved this retro…" } },
    },
  });

  const { result } = renderHook(() => useRetro("2026-07"), { wrapper });
  await waitFor(() => expect(result.current.data).toBeDefined());

  await act(() => result.current.saveRetro({ notes: "mine" }).catch(() => {}));

  expect(result.current.conflict).toBe(true);
});

it("mood null round-trips as null, never as 0", async () => { /* fixture with mood: null; assert the parsed value is null and no 0 appears */ });
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd web && npx vitest run src/features/marriage/useRetros.test.ts`
Expected: FAIL — cannot resolve `./useRetro`.

- [ ] **Step 3: Write the schemas and hooks**

`retroSchemas.ts` mirrors the Go DTOs field for field, with `mood: z.number().int().min(1).max(5).nullable()` — the parse is the frontend's own fail-closed boundary, and a `0` from a buggy server must not render as a face.

`useRetros.ts` / `useRetro.ts` follow `useBudget.ts`'s month-keyed shape (`retroQueryKey(month)`), not `useGoals.ts`'s include-archived shape. Every mutation awaits its invalidation — `CurrencyPanel` and `NotificationsPanel` are on the follow-up list for exactly the non-awaited version of this.

`conflict` is derived state on the hook: true when the last mutation failed with code `RETRO_CHANGED`, cleared on a successful refetch.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd web && npx vitest run src/features/marriage/useRetros.test.ts`
Expected: PASS.

- [ ] **Step 5: Mutation-check the version round-trip**

Change `saveRetro` to send `version: 1` unconditionally. Expected: the first test goes red on `toMatchObject({ version: 3 })`. Restore.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/marriage/
git commit -m "feat(retros): hooks that carry the version, and a conflict the page can read"
```

---

### Task 10: The page, its five states, and Marriage's way back into the app

**Files:**
- Create: `web/src/features/marriage/RetrosPage.tsx`, `web/src/features/marriage/RetrosPage.test.tsx`
- Modify: `web/src/routes/router.tsx`, `web/src/features/shell/Sidebar.tsx`, `web/src/routes/router.test.tsx`, `web/src/features/shell/Sidebar.test.tsx`

**Interfaces:**
- Consumes: `useRetros` (Task 9).
- Produces: the route `/marriage/retros`, the sidebar's Marriage group, and the page shell every later component mounts into.

- [ ] **Step 1: Write the failing tests**

`RetrosPage.test.tsx` — the five states, with the owner-only pair copied from `GoalsPage.test.tsx`'s shape:

```ts
it("a household with no retros is invited to start its first", async () => { /* empty fixture; assert the first-run panel and the start button's label */ });

it("a draft shows as in progress and is not counted as done", async () => {
  renderPage(retrosFixture({ retros: [summaryFixture({ month: "2026-08", finished: false })], doneCount: 0 }));
  expect(await screen.findByTestId("retro-draft-row")).toHaveTextContent("In progress");
  expect(screen.queryByText(/1 done/)).not.toBeInTheDocument();
});

it("a limited member is told this is owner-only, not that something broke", async () => {
  stubFetchRoutes({
    "GET /api/v1/retros": { status: 403, body: { error: { code: "FORBIDDEN", message: "Owner only." } } },
  });
  renderWithRouter(<RetrosPage />);
  expect(await screen.findByTestId("retros-owner-only")).toHaveTextContent("Owner only");
  expect(screen.queryByTestId("retros-load-error")).not.toBeInTheDocument();
});

it("a non-403 failure renders the generic load error, not the owner-only explanation", async () => {
  stubFetchRoutes({
    "GET /api/v1/retros": { status: 500, body: { error: { code: "INTERNAL", message: "Something broke." } } },
  });
  renderWithRouter(<RetrosPage />);
  expect(await screen.findByTestId("retros-load-error")).toHaveTextContent("Couldn't load your retros.");
  expect(screen.queryByTestId("retros-owner-only")).not.toBeInTheDocument();
});

it("hides the start button when both months already have a retro", async () => { /* startMonth: null; assert no start button */ });
```

Plus, in `Sidebar.test.tsx`: a member holding `marriage` sees a Marriage group containing a Retros link; a member without it sees no Marriage entry at all.

- [ ] **Step 2: Run them and watch them fail**

Run: `cd web && npx vitest run src/features/marriage src/features/shell/Sidebar.test.tsx`
Expected: FAIL — `RetrosPage` does not exist; the sidebar renders no Marriage group.

- [ ] **Step 3: Build the page and the wiring, in one change**

All three of these land together — the route, the `SPACE_PAGES` entry and the guard — because the sidebar shows a builtin space only when `SPACE_PAGES` names at least one built page for it, so a route without the entry produces an invisible feature that looks like a bug (`docs/FEATURE_TRACKER.md` §6 says so at the row):

- `router.tsx`: `/marriage/retros` under the shell, wrapped in `RequireCapability cap="marriage"`.
- `Sidebar.tsx`: `marriage: [{ to: "/marriage/retros", label: "Retros" }]`.
- `RetrosPage.tsx`: header (`Marriage retros`, the "Monthly check-in, just the two of us" subtitle, the `🔒 Private — parents only` badge, the done count when there is one), the start button labelled from `startMonth`, and the five states above. Layout only — history, chart, detail and modal arrive in Tasks 11–13 and mount here.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd web && npx vitest run src/features/marriage src/features/shell src/routes`
Expected: PASS.

- [ ] **Step 5: Mutation-check the owner-only branch**

Collapse the 403 branch into the generic error branch (the exact bug `BudgetPage.tsx` shipped: `error || !data`). Expected: the limited-member test goes red. Restore.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/marriage/ web/src/features/shell/Sidebar.tsx web/src/routes/
git commit -m "feat(retros): the page, and Marriage's route, link and guard together"
```

---

### Task 11: History list and mood chart

**Files:**
- Create: `web/src/features/marriage/RetroHistoryList.tsx`, `web/src/features/marriage/MoodChart.tsx`, `web/src/features/marriage/RetroHistoryList.test.tsx`, `web/src/features/marriage/MoodChart.test.tsx`
- Modify: `web/src/features/marriage/RetrosPage.tsx`

**Interfaces:**
- Consumes: `RetroSummary[]` and `MoodPoint[]` from `useRetros` (Task 9).
- Produces: `<RetroHistoryList summaries onSelect selectedMonth />` and `<MoodChart points />`.

- [ ] **Step 1: Write the failing tests**

```ts
// The design's row: `June 2026 · Mood 4/5 · 3 actions · "best month this year"`.
// Each clause disappears when it has nothing to say -- never "0 actions", never
// empty quotation marks.
it("renders only the clauses a retro actually has", async () => {
  render(<RetroHistoryList summaries={[
    summaryFixture({ month: "2026-06", mood: 4, actionCount: 3, quote: "best month this year" }),
    summaryFixture({ month: "2026-05", mood: null, actionCount: 0, quote: "" }),
  ]} onSelect={() => {}} selectedMonth="2026-06" />);

  const rich = screen.getByTestId("retro-row-2026-06");
  expect(rich).toHaveTextContent("Mood 4/5");
  expect(rich).toHaveTextContent("3 actions");
  expect(rich).toHaveTextContent("best month this year");

  const bare = screen.getByTestId("retro-row-2026-05");
  expect(bare).not.toHaveTextContent("Mood");
  expect(bare).not.toHaveTextContent("actions");
  expect(bare.textContent).not.toContain('""');
});

// "Show 2025 (7 more) ↓" is a disclosure over data the page already holds --
// not a second request. The stub throws on any unregistered call, so a fetch
// here fails the test by itself.
it("collapses older years behind a disclosure and fetches nothing to expand them", async () => {
  render(<RetroHistoryList summaries={[...twelve2026(), ...seven2025()]} onSelect={() => {}} selectedMonth="2026-06" />);

  expect(screen.queryByTestId("retro-row-2025-12")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: /Show 2025 \(7 more\)/ }));
  expect(screen.getByTestId("retro-row-2025-12")).toBeInTheDocument();
});

// A gap is a gap. A month with no finished retro must not be drawn as a
// mood of zero -- zero is a claim, and on a line chart it is a dramatic one.
it("draws a gap for a month with no mood, never a zero", () => {
  const { container } = render(<MoodChart points={[
    { month: "2026-06", mood: 4 }, { month: "2026-07", mood: null }, { month: "2026-08", mood: 3 },
  ]} />);

  const polylines = container.querySelectorAll("polyline");
  expect(polylines.length).toBe(2); // one segment each side of the gap, not one line through it
  expect(container.querySelector("svg")?.textContent ?? "").not.toContain("0");
});

it("renders an empty chart without crashing when no month has a mood", () => { /* all null; assert the "not enough retros yet" copy */ });
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd web && npx vitest run src/features/marriage`
Expected: FAIL — modules not found.

- [ ] **Step 3: Build both components**

`MoodChart` is inline SVG: a fixed `viewBox`, one `<polyline>` per unbroken run of months that have a mood, a dot per point, month labels every third month so 320px does not overflow. No dependency, no canvas. Give it `role="img"` and an `aria-label` naming the range and the number of months with a mood, since a line drawing is invisible to a screen reader.

`RetroHistoryList` groups by year, renders the current year expanded, older years behind the design's disclosure button, and marks the selected month.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd web && npx vitest run src/features/marriage`
Expected: PASS.

- [ ] **Step 5: Mutation-check the gap**

Make `MoodChart` treat a null mood as `0` instead of breaking the line. Expected: the gap test goes red on the polyline count. Restore.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/marriage/
git commit -m "feat(retros): history rows that say only what they know, and a chart with real gaps"
```

---

### Task 12: The retro detail and its action rows

**Files:**
- Create: `web/src/features/marriage/RetroDetail.tsx`, `web/src/features/marriage/RetroActionRow.tsx`, `web/src/features/marriage/RetroDetail.test.tsx`
- Modify: `web/src/features/marriage/RetrosPage.tsx`

**Interfaces:**
- Consumes: `useRetro(month)` (Task 9), `useHouseholdMembers` (existing, for assignee initials).
- Produces: `<RetroDetail month />`, which owns the tick.

- [ ] **Step 1: Write the failing tests**

```ts
// The design renders "Phone-free dinners on weekdays  A C" -- one action, both
// owners. Initials come from the household's own members, not from parsing a
// name in the page.
it("shows an initial per assignee", async () => {
  renderDetail(retroFixture({ actions: [actionFixture({ body: "Phone-free dinners", assigneeMembershipIds: ["m-a", "m-c"] })] }));
  const row = await screen.findByTestId("retro-action-" + "a1");
  expect(within(row).getByTitle("Andreas")).toHaveTextContent("A");
  expect(within(row).getByTitle("Christine")).toHaveTextContent("C");
});

// Ticking is a PATCH to the action, and it must not send the retro's version:
// one partner ticking all month cannot be allowed to invalidate the other's
// open editor.
it("ticking an action sends no retro version", async () => {
  const stub = renderDetail(retroFixture({ version: 7, actions: [actionFixture({ id: "a1", doneAt: null })] }));
  await userEvent.click(await screen.findByRole("checkbox", { name: /Phone-free dinners/ }));

  await waitFor(() => expect(stub.bodyOf("PATCH /api/v1/retros/2026-07/actions/a1")).toMatchObject({ done: true }));
  expect(stub.bodyOf("PATCH /api/v1/retros/2026-07/actions/a1")).not.toHaveProperty("version");
});

it("renders went-well and was-hard as bullets, splitting on newlines", async () => { /* two lines in, two <li> out, blank lines dropped */ });

it("an action carried from last month says so", async () => { /* carriedFrom set; assert the "carried from June" label */ });
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd web && npx vitest run src/features/marriage/RetroDetail.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Build the components**

`RetroDetail` renders the header (`June 2026 retro`, the completion date, `mood 4/5`), the two bullet lists split on `\n` with blank lines dropped, the actions, and the notes. `RetroActionRow` is a real `<input type="checkbox">` with a visible label — **not** an `sr-only` input with a styled stand-in, which is the shape that shipped keyboard-invisible focus in Transactions.

Assignee initials come from the existing members hook; an action with no assignee renders no initial rather than a placeholder circle.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd web && npx vitest run src/features/marriage`
Expected: PASS.

- [ ] **Step 5: Mutation-check the tick's payload**

Make the tick send `{ done, version }`. Expected: the "sends no retro version" test goes red. Restore.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/marriage/
git commit -m "feat(retros): the detail view, and a tick that cannot invalidate an open editor"
```

---

### Task 13: The retro modal

**Files:**
- Create: `web/src/features/marriage/RetroModal.tsx`, `web/src/features/marriage/RetroModal.test.tsx`
- Modify: `web/src/features/marriage/RetrosPage.tsx`

**Interfaces:**
- Consumes: `useRetro(month)`, `components/Modal` (existing shared primitive).
- Produces: `<RetroModal month onClose />`, opened by the page's start button and by clicking a retro's Edit control.

- [ ] **Step 1: Write the failing tests**

```ts
// The design's five emoji. A real radio group -- one tab stop, arrow keys
// between options -- because that is what a single-choice control is.
it("the mood picker is a radio group with five options", async () => {
  renderModal(retroFixture({ mood: null }));
  const group = await screen.findByRole("radiogroup", { name: /How was this month/ });
  expect(within(group).getAllByRole("radio")).toHaveLength(5);
});

// Save draft writes without finishing.
it("Save draft sends the text and leaves the retro a draft", async () => {
  const stub = renderModal(retroFixture({ version: 2 }));
  await userEvent.type(await screen.findByLabelText(/What went well/), "Two date nights");
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));

  await waitFor(() => expect(stub.bodyOf("PATCH /api/v1/retros/2026-07")).toMatchObject({ wentWell: "Two date nights", version: 2 }));
  expect(stub.called("POST /api/v1/retros/2026-07/complete")).toBe(false);
});

// Finish saves first, then completes -- in that order, so a finish can never
// discard what is currently typed.
it("Finish retro saves before completing", async () => {
  const stub = renderModal(retroFixture({ version: 2 }));
  await userEvent.type(await screen.findByLabelText(/What was hard/), "Phones at dinner");
  await userEvent.click(screen.getByRole("button", { name: "Finish retro" }));

  await waitFor(() => expect(stub.called("POST /api/v1/retros/2026-07/complete")).toBe(true));
  expect(stub.orderOf("PATCH /api/v1/retros/2026-07")).toBeLessThan(stub.orderOf("POST /api/v1/retros/2026-07/complete"));
});

// The conflict, in the modal, with copy that tells the person what happened
// and what to do -- not a red "something went wrong".
it("a 409 explains that the other partner saved, and keeps what was typed", async () => {
  const stub = stubFetchRoutes({
    "GET /api/v1/retros/2026-07": { retro: retroFixture({ version: 2 }), carryOver: [] },
    "PATCH /api/v1/retros/2026-07": { status: 409, body: { error: { code: "RETRO_CHANGED", message: "…" } } },
  });
  renderModalWith(stub);

  await userEvent.type(await screen.findByLabelText(/Notes/), "mine");
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));

  expect(await screen.findByTestId("retro-conflict")).toHaveTextContent("saved this retro while you were editing");
  expect(screen.getByLabelText(/Notes/)).toHaveValue("mine"); // nothing typed is thrown away
});
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd web && npx vitest run src/features/marriage/RetroModal.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Build the modal**

Built on `components/Modal` (native `<dialog>`; do not reintroduce a declarative `open` attribute). Contents in the design's order: title with the private badge, the mood radio group, "What went well" and "What was hard" textareas with the design's placeholders, the money check-in panel (Task 14), the actions block (Task 14), then Save draft / Finish retro.

The conflict banner replaces neither the form nor its contents: what the person typed stays in the fields, because the whole point of refusing the write is that nothing gets lost.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd web && npx vitest run src/features/marriage`
Expected: PASS.

- [ ] **Step 5: Mutation-check the finish ordering**

Make Finish call complete first and save second. Expected: the ordering test goes red. Restore.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/marriage/
git commit -m "feat(retros): the modal, and a conflict that costs nobody their typing"
```

---

### Task 14: The carry-over offer and the money check-in

**Files:**
- Create: `web/src/features/marriage/MoneyCheckInPanel.tsx`, `web/src/features/marriage/MoneyCheckInPanel.test.tsx`
- Modify: `web/src/features/marriage/RetroModal.tsx`, `web/src/features/marriage/RetroModal.test.tsx`

**Interfaces:**
- Consumes: `useBudget(month)` and `useGoals()` (existing money hooks), `useRetro(month).carryOver`.
- Produces: `<MoneyCheckInPanel month />` and the "Still open from July" block inside the modal.

- [ ] **Step 1: Write the failing tests**

```ts
// Carrying is one click, and it creates a NEW action on this retro pointing
// back at last month's. July's own row is untouched (the API guarantees it;
// this asserts the request we send).
it("carrying an open action posts a new action with carriedFrom", async () => {
  const stub = renderModal(retroFixture({ month: "2026-08" }), {
    carryOver: [actionFixture({ id: "jul-1", body: "Phone-free dinners" })],
  });

  await userEvent.click(await screen.findByRole("button", { name: /Carry over Phone-free dinners/ }));

  await waitFor(() => expect(stub.bodyOf("POST /api/v1/retros/2026-08/actions")).toMatchObject({
    body: "Phone-free dinners",
    carriedFrom: "jul-1",
  }));
});

it("shows nothing at all when last month left nothing open", async () => { /* carryOver: []; assert no "Still open" heading */ });

// The panel is a prompt, not a record: budget figures are the retro's month,
// goals are today's standing, and the panel says which is which.
it("labels the goals line as today's standing, not the month's", async () => {
  renderPanel({ budget: budgetFixture({ percentUsed: 66 }), goals: goalsFixture({ onTrackCount: 4, datedCount: 4 }) });
  expect(await screen.findByTestId("checkin-budget")).toHaveTextContent("66% used");
  expect(screen.getByTestId("checkin-goals")).toHaveTextContent("4 of 4 on track");
  expect(screen.getByTestId("checkin-goals")).toHaveTextContent(/today/i);
});

// A month with no budget shows Budget's own empty copy, never zeros.
it("says there is no budget rather than showing 0% used", async () => {
  renderPanel({ budget: null, goals: goalsFixture({ onTrackCount: 0, datedCount: 0 }) });
  expect(await screen.findByTestId("checkin-budget")).not.toHaveTextContent("0%");
  expect(screen.getByTestId("checkin-budget")).toHaveTextContent(/No budget set/i);
});
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd web && npx vitest run src/features/marriage`
Expected: FAIL — `MoneyCheckInPanel` not found; no carry-over control.

- [ ] **Step 3: Build both**

The carry-over block renders only when `carryOver` is non-empty, one row per open action with a single "Carry over" control each. After a successful post the row leaves the offer list and appears in the retro's own actions.

`MoneyCheckInPanel` reads the existing hooks and renders three lines — budget percent used, on-pace-to-save, goals on track — with no arithmetic of its own. It computes nothing: every figure here is already pinned in Budget's and Goals' own specs, and recomputing one in a Marriage component is how two screens come to disagree.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd web && npx vitest run src/features/marriage`
Expected: PASS.

- [ ] **Step 5: Mutation-check the empty-budget case**

Make the panel render `0% used` when the month has no budget. Expected: the "says there is no budget" test goes red. Restore.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/marriage/
git commit -m "feat(retros): carry an action forward, and a money panel that computes nothing"
```

---

### Task 15: Overview's next-retro card

**Files:**
- Create: `web/src/features/overview/NextRetroCard.tsx`, `web/src/features/overview/NextRetroCard.test.tsx`
- Modify: `web/src/features/overview/OverviewPage.tsx` (the file that renders the existing cards — check its real name before editing), `web/src/features/overview/OverviewPage.test.tsx`

**Interfaces:**
- Consumes: `useRetros()` (Task 9) — the same hook and cache entry `/marriage/retros` uses, the way the next-bill card reuses `useBills`.
- Produces: `<NextRetroCard />`.

- [ ] **Step 1: Write the failing tests**

```ts
// Overview is the one page every member reaches, so the card's ABSENCE for a
// member without marriage needs a positive test -- an absence assertion holds
// perfectly over a blank page, which is exactly how the interim Overview
// shipped a limited member a page containing only the word "Overview"
// (docs/LEARNING.md pattern 2).
it("renders nothing for a member without the marriage capability, and the rest of the page still renders", async () => {
  renderOverview({ capabilities: ["money"] });

  expect(await screen.findByTestId("overview-net-worth")).toBeInTheDocument(); // the page is alive
  expect(screen.queryByTestId("next-retro-card")).not.toBeInTheDocument();
});

it("shows the current retro and its open actions", async () => {
  renderCard(retrosFixture({ retros: [summaryFixture({ month: "2026-08", finished: false, actionCount: 2 })] }));
  expect(await screen.findByTestId("next-retro-card")).toHaveTextContent("August retro");
  expect(screen.getByTestId("next-retro-card")).toHaveTextContent("in progress");
});

it("prompts to start when the month has no retro", async () => { /* startMonth: "2026-08"; assert the start prompt and its link to /marriage/retros */ });

it("makes no request for a member who cannot see retros", async () => { /* stub throws on unregistered calls; assert GET /api/v1/retros was never called */ });
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd web && npx vitest run src/features/overview`
Expected: FAIL — `NextRetroCard` not found.

- [ ] **Step 3: Build the card**

Gate the *hook call itself* on the capability, not just the render: a member without marriage would otherwise fire a request that answers 403 on every visit to the home page.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd web && npx vitest run src/features/overview`
Expected: PASS.

- [ ] **Step 5: Mutation-check the gate**

Render the card unconditionally. Expected: the first and last tests both go red. Restore.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/overview/
git commit -m "feat(retros): the Overview card, gated where the request is made"
```

---

### Task 16: The documents

**Files:**
- Modify: `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`, `docs/HANDOVER.md`

**Interfaces:**
- Consumes: everything built in Tasks 1–15.
- Produces: documentation that matches the code, which is the bar `CLAUDE.md` sets for "done".

- [ ] **Step 1: Update `docs/SYSTEM_DESIGN.md`**

Use the `maintaining-system-design` skill. A new Marriage section: the three tables and their relationships, the route group with both guards, and the retro flow (start → save with version → finish → tick actions all month → carry the open ones forward). Say in prose why the version guard exists and why ticking bypasses it — the diagram cannot show that, and it is the thing a future editor would break.

- [ ] **Step 2: Update `docs/FEATURE_TRACKER.md`**

Four Marriage rows move ⬜ → ✅: retro history with mood; mood chart over 12 months; single retro view (its own name includes the actions list, so the actions get no separate row); start retro modal — that last one carrying a note that the design's "45 min" duration is drawn and not built. Add two rows the mockup never draws, at ✅: carry an unfinished action into the next retro, and delete a draft retro. Overview's "Next retro card with carried-over actions" ⬜ → ✅.

**Recount the summary table by counting symbols in the tables** — the first symbol in each row's own cell, whether the cell is bare or has prose after it — never by adjusting the previous totals. This file records that adjusting by delta has produced wrong numbers here before, in both directions at once.

- [ ] **Step 3: Update `docs/LEARNING.md`**

One entry per defect this round actually produced, with what would have caught it sooner. If a defect matches an existing pattern, add it there as evidence rather than starting a new section — the repetition is the point. If the round produced no defect worth recording, say so explicitly in the commit rather than inventing one.

- [ ] **Step 4: Update `docs/HANDOVER.md`**

§1's slice table: Marriage moves from "Not started" to its real state (Retros done, Vision and Agreements not). §4: the next work is Vision, then Agreements, with Agreements' one-owner signing question named as the thing to settle before its spec.

- [ ] **Step 5: Commit**

```bash
git add docs/
git commit -m "docs: record Retros where each document is actually read"
```

---

### Task 17: The browser walk

**Files:**
- Create: `docs/superpowers/plans/2026-08-16-hearth-retros-verification.md`, `docs/superpowers/plans/2026-08-16-hearth-retros-screenshots/`

**Interfaces:**
- Consumes: the whole feature, running under `make dev` against a real database.
- Produces: the verification record, and any defect it finds — fixed in this task, not filed for later.

- [ ] **Step 1: Start clean**

`make down && make up` (this forces recreation, which is what makes the `migrate` service rerun — a stack left running across a new migration keeps its already-succeeded migrate container), then `make seed`. Check `docker ps` on **both** Docker engines if anything answers strangely: a Docker Desktop stack silently holds ports 5173/8080/8025 out from under colima's, and that cost an earlier walk most of an hour.

- [ ] **Step 2: Walk all fifteen criteria in a real browser**

1. A signed-in owner sees Marriage in the sidebar, expanding to a Retros link.
2. A limited member sees no Marriage entry at all, and typing `/marriage/retros` lands on the owner-only explanation — not a red error.
3. A household with no retros sees the first-run panel; the start button names the right month.
4. Starting a retro on a date where last month has none files it against **last** month.
5. The mood picker: click one, and — separately — reach it by keyboard, arrow through all five, and confirm the focused option is **visibly** distinct. Capture before/after screenshots and compare by `shasum -a 256`; two identical hashes mean the change did not land.
6. Type into both textareas, Save draft, reload — the text is still there and the retro still reads as in progress.
7. Add two actions, assign one to each partner and one to both; the initials render as the design's `A` / `C` / `A C`.
8. Finish the retro. It leaves the draft state, appears in the history, and the done count moves.
9. The money check-in shows the retro month's budget figures and today's goals standing, and says which is which.
10. On a month with no budget, the panel says so rather than showing `0%`.
11. Tick an action from the page (not the modal); reload; it stays ticked.
12. Start the next month's retro: last month's unticked action is offered, carrying it creates a new action here, and last month's row is unchanged.
13. Delete a draft — allowed. Try to delete a finished retro — refused, with copy that explains.
14. **Two browsers, one draft.** Both open it, both edit, the second save is refused with the reload copy and nothing typed is lost.
15. Every screen at 320 / 375 / 768 / 1440 with no horizontal overflow, including the modal — check the dialog's own `scrollWidth` vs `clientWidth`, because a native `<dialog>` paints in the top layer and a document-level check cannot see it.

Assert on numbers (`getBoundingClientRect()`, `innerText` read in page script), not on how a scaled screenshot looks. Sign-out via page script: `document.querySelector('button[aria-label="Sign out"]').click()`. React controlled inputs ignore synthetic typing — use the native setter plus a dispatched `input` event (`docs/HANDOVER.md` §2).

- [ ] **Step 3: Fix what the walk finds, in this task**

Every walk in this project has found something, and the ones that found it in *one* place have twice found the same shape in siblings. When you fix a defect here, grep for its shape before closing it.

- [ ] **Step 4: Write the record**

`2026-08-16-hearth-retros-verification.md`, criterion by criterion, in the shape `2026-08-09-hearth-bills-verification.md` uses: what was done, what was observed, and — for anything met by an interpreted rather than literal path — say so rather than passing over it quietly.

- [ ] **Step 5: Full suite and lint**

Run: `make lint && make test`
Expected: both green. Fix anything that is not before committing.

- [ ] **Step 6: Commit**

```bash
git add docs/superpowers/plans/
git commit -m "docs: the Retros walk, criterion by criterion"
```

---

## Self-review

**Spec coverage.** Every section of `2026-08-16-hearth-retros-design.md` maps to a task: decisions 1–2 → Tasks 1, 3, 4; decision 3 → Task 14; decision 4 → Tasks 3, 6, 14; decision 5 → Tasks 2, 4; decision 6 → Tasks 1, 3, 5, 9, 13; decision 7 → Task 2; decision 8 → Task 16 (the tracker note); decision 9 → Task 15; decisions 10–11 → Tasks 7, 10; decision 12 → nothing, deliberately — the scheduler is a separate spec, and Task 16 keeps that row 🟡. The pinned formula table is implemented in Task 3 and asserted there; the five screen states are Task 10; the error table is Tasks 7–8 and 13; the fifteen-criterion walk is Task 17.

**Type consistency.** `RetroRecord`, `RetroSummary`, `RetroActionRecord`, `RetroActionInput` and `RetroUpdate` are defined once in Task 3 and used by name in Tasks 4–8. `RetroUpdate.Month` is called out in Task 5 because the repository's conflict-versus-missing distinction needs it — add it in Task 3 if reading in order. The JSON field names in Task 7's DTOs are the ones Tasks 9–15 decode; `mood` is `*int` / `number | null` on both sides, never 0.

**One gap worth naming rather than hiding:** the helper names in the Go HTTP tests (`requestAs`, `ownerSession`, `assertStatus`) are the *shapes* the existing `api_test.go` harness provides, not necessarily its exact identifiers. Task 7 says to read them rather than assume — a plan that guessed and was wrong would cost an implementer more than one that says which part is a sketch.

