# Hearth — Vision & goals

Spec for slice 3's second feature: the Marriage → Vision & goals screen, its
Edit-vision modal, and the two places Vision appears on the Overview. Written
2026-08-28 after a brainstorming session; the decisions below record what was
chosen and why, in the order they were made.

The design source is the Vision section of `design/Household Dashboard.dc.html`
(`:590-616`, the `is_vision` screen), the `modalVision` Edit-vision modal
(`:927-950`), the Overview's Vision check-in strip (`:322`) and its
`Vision 2026` card (`:336-338`), plus the flow map's entry
`4c Marriage · Vision & goals` — "Yearly theme hero, three pillars with
check-ins, long-horizon milestones".

**Vision is the second of three Marriage specs.** The order set in
`2026-08-16-hearth-retros-design.md` was Retros → Vision → Agreements, and
nothing has changed it: Retros shipped and passed its browser walk 15 of 15 on
2026-08-18, and Agreements still carries the product question — what "both
sign" means in a household with one owner — that its own conversation has to
settle first.

Vision also unblocks one of the two Overview cards still missing, which is the
second reason it comes before Agreements rather than after.

## What Vision inherits, and what it does not

**It inherits the discipline, not the data.** Retros' spec pinned every number
its screen displayed before any of it was built, because Money's five specs
established that an implementer handed an undefined figure will invent one.
Vision displays six measures in the mockup and defines none of them, so the
formulas table below exists for the same reason.

**It inherits the guards exactly.** Retros' decision 11 stacked
`requireCapability(domain.CapMarriage)` on `requireOwner`
(`api/internal/adapter/http/router.go:301-302`), redundant today because
`limited_members_have_no_marriage` — a database `CHECK` in
`api/migrations/00002_identity.sql:38` — already refuses `marriage` to a
limited member. Vision's routes join that same group and inherit both.

**One consequence worth stating, because it removes a whole branch:** everyone
who can reach Vision is an owner. Accounts and Goals each carry a redaction
path for a limited member reading a monetary figure; Vision needs none, and a
goal-linked measure can render its percentage with no conditional at all.

**It does not inherit a monetary path.** Nothing in Vision is money. A measure
that links to a savings goal renders that goal's own percentage — an integer
already computed by `domain.GoalProgressPercent` — and never a currency
amount. `int64` minor units do not appear in this feature.

## Decisions

**1 · A measure is typed, with an optional link to a savings goal.**

The design draws six measures across three pillars. Two are countable from data
Hearth already holds — `Monthly budget reviews 7 of 7` is roughly "finished
retros this year", `Emergency fund 62%` is a savings goal's own progress. The
other four — date nights, weekends away, phone-free dinners, one-on-one kid
dates — nothing in this product can count, and nothing should try.

So every measure carries a label and either a typed `current`/`target` pair or
a `goal_id`, never both. A linked measure reads its figure live; a typed one is
a number the couple maintains at their retro, which is what "checked in at each
retro" already asks of them.

The rejected third option was a named enum of derived sources, so that
`Monthly budget reviews` could count finished retros automatically. It was
rejected because such an enum grows with every future request and each entry is
a formula that has to be pinned — the exact trap Money's derived figures were.
The cost of not having it is that a household types a number this product could
have counted; that cost was accepted deliberately.

**2 · The marriage-duration block is not built.**

The design's theme hero carries `Married · 14 years · Feb 14, 2012` on its
right-hand side. Nothing in this product stores a wedding date, no feature
would read one, and the only figure derived from it is today minus the date.

Building it costs a nullable column, a field the design's own modal does not
draw, a null state for a household that never set one, a visibility decision
(`GET /household` sits behind `requireSession` alone, so a column on
`households` is readable by every member including a limited one), and a
years computation with a leap-day edge — five small things for no behaviour.

The theme hero therefore renders theme and description at full width. The
tracker row stays ⬜ with this decision named, the same treatment the design's
drawn-but-unbuilt "45 min" retro duration got in Retros' decision 8. Adding it
later reshapes nothing.

**3 · Overview's `Vision 2026` card renders one line per pillar — that
pillar's first measure.**

The design draws two incompatible shapes for the same data. The Vision page
shows three pillars, each with a name, a description and two measures. The
Overview card shows the theme plus three flat lines — `1 weekend away per
quarter`, `Date night twice a month`, `Screens off by 9:30pm` — which are
neither pillar names nor measure labels. It is a third shape the design never
says how to store.

The card renders one line per pillar in `position` order, showing that pillar's
`position`-0 measure with its live figures; a pillar with no measures falls
back to its own name. Deterministic, because measures already need a display
order for the pillar cards, so "first" is free — no `show_on_overview` flag is
invented, and there is no "which three of six" question to answer.

**This is a deliberate divergence from the design's own copy**, recorded here
rather than discovered later. The rejected alternative was a fourth kind of
child — free-text commitment lines whose only consumer is this card — which
would have reproduced the mockup exactly at the price of guaranteed
duplication: someone types `Date night twice a month` there and a
`Date nights / month, target 2` measure on the pillar, and nothing keeps the
two honest.

**4 · One vision row per `(household_id, year)`.**

The modal's own Year dropdown implies a vision per year, and the page's subtitle
says "Set every January". A row per year gives history with no versioning
machinery: 2027 is a new row, not an overwrite, and 2026 remains readable
forever.

**5 · Relational tables, one whole-document save.**

The modal has a single **Save vision** button that edits theme, year,
description, every pillar and every milestone at once. That is a whole-document
edit, so there is one write endpoint — `PUT /marriage/vision/{year}` — which
replaces the document in a single transaction.

Storage stays relational anyway (four tables, below) so that `goal_id` is a real
foreign key rather than an unenforced number, and so a future question like
"which visions link this goal" is a join rather than a JSON scan.

The rejected alternatives were granular per-child endpoints (roughly a dozen
routes and their tests to serve one modal that saves everything at once — most
with no caller on the day they ship) and a single JSONB document (one table, but
`goal_id` dangles unenforced and every read validates JSON it did not construct
at the adapter boundary, which is the fail-closed rule applied by hand instead
of by the database).

**A save deletes and reinserts child rows, so a measure's id does not survive
an edit.** Nothing references a measure from outside its own vision, so nothing
breaks. If anything ever does — a comment on a measure, a history of its value —
that is the change that introduces stable ids, with a rule for preserving them.

**Because a save rewrites every child, the collections are capped in the
domain**: at most 12 pillars per vision, 8 measures per pillar and 24
milestones. The numbers are generous against a design that draws three pillars
and three milestones; they exist so that the cost of one save is bounded rather
than whatever a request body happens to contain.

**6 · Milestones are free text. No goal link.**

The design's own milestone note reads `Tied to car + housing goals`. That stays
prose. A milestone is a year, a title and a note; linking it to goals would need
a join table and an ordering rule to serve a card that shows three lines of
text.

**7 · The modal gains the two fields the design's own modal omits.**

The Edit-vision modal as drawn cannot produce the screen as drawn. It edits
theme, year, description, pillar **names** and milestones. The screen renders
pillar **descriptions** and two **measures** per pillar, and the modal has no
field for either.

The pillar description is a plain oversight — a textarea under each pillar name
closes it. The measures are the substance of decision 1, and the modal is the
only place they can be edited, so each pillar's row list gains a measure editor:
a label, then either a goal picker or a typed current/target pair.

**8 · A linked goal that vanishes renders no figure.**

`goal_id` is `ON DELETE SET NULL`, so deleting a goal unlinks the measure rather
than taking the vision's row with it. An archived goal keeps its link and its
figure — archiving is not deletion anywhere else in this product either.

A measure whose link has been cleared, or whose goal cannot be read, renders its
label with **no figure and a named explanation** — never a zero, never a stale
number. That is the precedent Accounts set when the primary currency changes and
net worth cannot be computed: blank the figure and say why.

**9 · `GET` returns an empty vision, never 404.**

A year that was never set returns `200` with a vision carrying an empty theme,
no pillars and no milestones. The page's empty state is then a render rather
than an error branch, and the modal opens on a blank document by the same path
it opens on a full one.

**10 · A `version` guard, as Retros has.**

Two owners can edit the same vision, and a whole-document replace makes a lost
update worse than a field-level one: a partner's entire set of pillars can be
silently overwritten by a stale editor. `visions.version` increments on every
save, `PUT` carries the version it read, and a mismatch is refused with `409`.

Retros' decision 6 made the same call for the same reason and is the pattern to
copy. Note the difference in scope: Retros deliberately exempted the
action-tick path from its version check so that one partner ticking an action
could never collide with the other's open editor. Vision has no equivalent
lightweight write — every write is the whole document — so there is nothing to
exempt.

**`version: 0` means "I read a year that had no vision".** The empty document
`GET` returns for a never-set year carries version 0, never 1, and a `PUT`
carrying 0 means *create*: it succeeds only if that household-year still has no
row, and answers `409` if one appeared in the meantime. The created row lands at
version 1.

Without this, the first save is the one place the guard cannot work. Both
owners open the modal on a blank January, both hold the same version, and the
second save overwrites a whole year of pillars the first partner just typed —
the precise loss decision 10 exists to refuse, on the save where parallel
editing is most likely.

**11 · `/marriage` gains an index route redirecting to Retros.**

Task 10 of Retros left `marriageGuardRoute` with one child and no index, so
typing bare `/marriage` renders the sidebar shell with a blank content area.
Retros' own tracker entry named this and said the next task to give Marriage a
second page should fix it. This is that task.

**12 · A narrow `GoalProgressReader` port, not `GoalRepository`.**

`VisionService` needs one thing from Goals: the progress of a handful of goal
ids. Handing it `GoalRepository` — a port whose doc comment runs to forty lines
about contribution scoping — would violate interface segregation for the sake of
one percentage. A new port with a single method, implemented by the existing
goals repository, keeps the dependency the size of the need.

**13 · The route and the sidebar entry land in the same change as the page.**

Retros' `110ab0a` established this: never a route with no way to reach it, and
never a sidebar link to a route that 404s. Vision's route, its `SPACE_PAGES`
entry and `VisionPage.tsx` ship together.

## Data model

Migration `00010_vision.sql`, following the conventions in `00009_retros.sql`.

```sql
CREATE TABLE visions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    year         smallint    NOT NULL CHECK (year BETWEEN 1900 AND 2200),
    theme        text        NOT NULL DEFAULT '',
    description  text        NOT NULL DEFAULT '',
    version      integer     NOT NULL DEFAULT 1,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, year)
);

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
    goal_id       uuid     REFERENCES goals(id) ON DELETE SET NULL,
    UNIQUE (pillar_id, position),
    CONSTRAINT measure_is_typed_or_linked CHECK (
        -- typed
        (goal_id IS NULL     AND target_value IS NOT NULL AND target_value > 0
                             AND current_value IS NOT NULL AND current_value >= 0)
        -- linked
     OR (goal_id IS NOT NULL AND target_value IS NULL AND current_value IS NULL)
        -- link broken by a deleted goal (see below -- this branch is not
        -- optional, and the domain still refuses to CREATE this state)
     OR (goal_id IS NULL     AND target_value IS NULL AND current_value IS NULL)
    )
);

CREATE TABLE vision_milestones (
    id        uuid     PRIMARY KEY DEFAULT gen_random_uuid(),
    vision_id uuid     NOT NULL REFERENCES visions(id) ON DELETE CASCADE,
    year      smallint NOT NULL CHECK (year BETWEEN 1900 AND 2200),
    title     text     NOT NULL,
    note      text     NOT NULL DEFAULT '',
    position  smallint NOT NULL,
    UNIQUE (vision_id, position)
);
```

The parts that are decisions rather than typing, each of which belongs as a
comment at the line a future editor would change:

- **`year` is a `smallint`, not a `date`.** A vision is a calendar year, not a
  month or a day. Retros' `month` is a `date` because a retro happens on one;
  a vision does not.
- **`theme` defaults to empty rather than being `NOT NULL` without a default**,
  because decision 9 returns an empty vision for a year never set, and the
  simplest implementation of that is a row that can exist while still blank.
- **`version` is the concurrency guard** (decision 10), incremented by the save
  path only.
- **`measure_is_typed_or_linked` enforces decision 1 in the database**, not only
  in the domain. A measure with both a `goal_id` and a typed target would render
  two different answers to the same question, and neither layer alone should be
  the only thing standing between the product and that state.
- **`goal_id` is `ON DELETE SET NULL`** (decision 8) — deleting a goal must never
  delete a pillar's measure, let alone cascade into the vision.
- **The CHECK's third branch exists because of that `SET NULL`, and removing it
  breaks the Goals feature.** A referential `SET NULL` is an `UPDATE` on the
  measure row, and Postgres enforces CHECK constraints on `UPDATE`. With only
  the typed and linked branches, unlinking leaves `goal_id`, `target_value` and
  `current_value` all null — which satisfies neither — so **deleting a goal
  would fail with a constraint violation**, surfacing inside Goals where nobody
  debugging it would think to look at Vision. The third branch is the
  broken-link state decision 8 already describes how to render. **The database
  permits that state only because `SET NULL` produces it; the domain still
  refuses to create one**, so a `PUT` carrying a measure with a label and
  neither a goal nor a target is `422`. Read tolerantly, write strictly.
- **Children carry an explicit `position`, unlike `retro_actions`.** Retros
  deliberately omitted one because its only safe writer, `max(position) + 1`,
  raced when two partners added an action at once. Vision has no such race: a
  save writes every child of a document in one transaction, so `position` is
  assigned from the submitted array's own index by a single writer. The design
  also numbers its pillars visibly ("Pillar 1", "Pillar 2"), so the order is
  something the household sees rather than an accident of insertion.
- **`UNIQUE (vision_id, position)`** makes a duplicate position impossible even
  if a future writer gets the indexing wrong.

## The formulas, pinned

Every figure the Vision screen and the two Overview surfaces display, defined
here so no implementer invents one.

| Figure | Definition |
|---|---|
| Linked measure percentage | `domain.GoalProgressPercent(contributedMinor, target.Amount)` — the function Goals already uses (`api/internal/usecase/goal.go:182`), already capped to 0–100. No second percent formula enters this codebase |
| Typed measure `N of M` | The stored `current_value` and `target_value`, rendered verbatim. No rounding, no derivation |
| Measure met (the `✓`) | Typed: `current_value >= target_value`. Linked: `percent >= 100`. The design colours a met measure green and an unmet one plain, with the third colour (`#b4552e`) used where the mockup shows one behind pace — that colour is decoration in the mockup and is **not** built as a third state; a measure is met or it is not |
| Measure with a broken link | A measure whose `goal_id` is `NULL` after a goal deletion, or whose goal cannot be read, renders its label and **no figure**, with a short named explanation. Never `0`, never a stale number (decision 8) |
| Pillar heading | `Pillar {position + 1}`, matching the design's visible numbering |
| Overview `Vision 2026` card | The current year's theme, then one line per pillar in `position` order, each rendering that pillar's `position`-0 measure exactly as the pillar card renders it. A pillar with no measures renders its name alone. A vision with no pillars renders the theme alone (decision 3) |
| Overview `Vision check-in` strip | The current year's theme, one line, inside the Next-retro card as the design draws it (`:322`). A year with no theme omits the strip entirely rather than rendering an empty quotation |
| Either Overview surface, with no vision set | **Both are omitted entirely** when the current year has no theme — the card does not render, the strip does not render, and neither leaves a gap behind. Overview's setup checklist is the surface that tells a household something is missing; a Vision card that renders an empty quotation would be saying the household has a vision and it is blank. Both surfaces render only for a member holding `marriage`, the rule Retros' decision 9 set for the Next-retro card |
| Default year | The calendar year of `Clock.Now()`. The service normalises; no handler computes a year of its own |

## Service and ports

`usecase/vision.go` holds `VisionService` with exactly two methods, matching the
two routes:

- **`Get(ctx, householdID, year) (VisionView, error)`** — reads the document,
  resolves every linked measure, and returns an empty vision for a year never
  set (decision 9).
- **`Save(ctx, householdID, year, draft) (VisionView, error)`** — validates the
  draft in the domain, then replaces the whole document in one transaction and
  returns what was written. Partial success is impossible by construction; a
  failure anywhere leaves the previous document intact.

`usecase/ports.go` gains two entries:

- **`VisionRepository`** — `Get`, and a `Save` that performs the replace
  transactionally. Scoped by `householdID` in SQL like every other repository
  here: a vision belonging to another household must be indistinguishable from
  one that does not exist.
- **`GoalProgressReader`** — one method,
  `ProgressByIDs(ctx, householdID, goalIDs []string) (map[string]GoalProgress, error)`,
  implemented by the existing goals repository (decision 12). It returns only
  the ids it found; a missing id is a measure whose figure is blank, not an
  error, which is what makes decision 8's fail-closed rendering a lookup miss
  rather than an exception path.

No service takes an actor parameter. Authorisation is the HTTP layer's, as it is
everywhere in this product.

## API

Two routes, joining the existing marriage group —
`requireCapability(domain.CapMarriage)` stacked on `requireOwner`, with the
write additionally behind `requireCSRF`.

```
GET /marriage/vision?year=2026   200 VisionView
                                 (an empty vision for a year never set —
                                  decision 9; never 404)
                                 year omitted → the current year (Clock)

PUT /marriage/vision/2026        200 VisionView
                                 body: { version, theme, description,
                                         pillars: [ { name, description,
                                                      measures: [ { label,
                                                        current, target,
                                                        goalId } ] } ],
                                         milestones: [ { year, title, note } ] }
```

`PUT` replaces the whole document in one transaction, **creating the vision row
if that year has none** — it is the only write path, so a household's first-ever
save and its tenth edit take the same route. A create sends `version: 0`, the
value `GET` returns with an empty vision, and is refused with `409` if a row has
appeared since (decision 10). The created row lands at version 1.

Positions are assigned from array order — the client does not send them, so it
cannot send a conflicting set.

Both responses carry a JSON body; the house rule is that every 2xx except 204
does, because the frontend's `apiFetch` throws on an ok response it cannot
parse.

## Screens and states

**`/marriage/vision` — `VisionPage.tsx`**

- Header: "Vision & goals", the subtitle "Set every January, checked in at each
  retro", and the **Edit vision** button.
- Theme hero: the year label, the theme in quotes, the description. Full width
  (decision 2).
- Pillar grid: one card per pillar — `Pillar N`, name, description, then its
  measures with their figures.
- Longer horizon: milestone cards in `position` order, each with its year, title
  and note, followed by the **+ Add milestone** affordance which opens the same
  modal.
- **Empty state**, for a year never set: the page renders its header and a
  single composed panel inviting the household to set this year's theme, opening
  the modal. Not an error, not a spinner, and not a grid of empty cards.

**`VisionModal.tsx`** — the whole-document editor

- Theme, year, description. **The year select offers the previous, current and
  next calendar year only** — three entries, computed from `Clock.Now()`. A
  household setting January's theme in December needs next year; one writing up
  a year they never recorded needs last year; nothing in the design asks for
  2019. The server still accepts any year in the `CHECK` range, so the narrow
  select is a UI choice rather than a rule the API depends on.
- Pillars: name, description, and a measure editor per pillar — label, then a
  choice between a goal picker and a typed current/target pair (decision 7).
  Add and remove a pillar; add and remove a measure.
- Milestones: year, title, note. Add and remove.
- **Save vision** submits the whole document with the `version` it read.
- Focus management and the validation-message conventions follow the modals the
  UI-polish round settled (`2026-08-28-hearth-ui-polish-design.md`).

**Overview** — the two surfaces of decision 3 and the formulas table.

**Sidebar** — `SPACE_PAGES` gains Vision under Marriage, beside Retros
(decision 13).

## Error handling

| Case | Answer |
|---|---|
| Not an owner, or without `marriage` | The group's own guards answer before any handler runs. A limited member never reaches the page: `RequireCapability.tsx` redirects to `/` |
| `PUT` without a CSRF token | `403`, by the middleware. A test drives this route without one |
| Stale `version` | `409` with a message telling the editor the vision changed, and the page refetches. Never a silent overwrite (decision 10) |
| Theme too long, empty pillar name, milestone without a title | `422` with the field named. The domain refuses before the repository is reached |
| A measure with both `goalId` and a typed target | `422`. Refused in the domain and, independently, by `measure_is_typed_or_linked` |
| `goalId` naming a goal in another household | `422`, indistinguishable from a goal that does not exist — the scoping rule every repository in this product already follows |
| A year outside 1900–2200 | `422`. Fail closed on a value we did not construct |
| More than 12 pillars, 8 measures on one pillar, or 24 milestones | `422` naming the collection. The caps are the domain's (decision 5), refused before a transaction is opened |

## Testing

- **Domain** — table tests over `Vision` validation: theme length, the
  typed-or-linked rule in both directions, target and current bounds, milestone
  year range, the collection caps.
- **Service** — against in-memory doubles: a save replaces the whole document; a
  stale version is refused; a linked measure resolves its percentage; a measure
  whose goal has been deleted renders no figure; a year never set returns an
  empty vision.
- **Repository** — against a real Postgres via testcontainers:
  - **The replace is atomic**, and the test needs a named lever or it asserts
    nothing: save a document whose *last* child trips a database constraint —
    a milestone whose year is outside the `CHECK` range, submitted behind
    valid pillars and measures — so the failure lands after several successful
    inserts. The previous document must be intact afterwards, pillars and
    measures included. A test with no injected fault cannot fail and therefore
    proves nothing.
  - **Deleting a linked goal succeeds and nulls the link**, leaving the
    measure's row present. This is the CHECK's third branch; without it the
    delete raises a constraint violation, so this test is what stops Vision
    breaking Goals.
  - `UNIQUE (household_id, year)` holds.
  - **A `version: 0` save against a year that already has a row is refused**,
    which is the first-save race of decision 10.
- **HTTP** — `PUT` without a CSRF token; `PUT` as a non-owner; the `409` path;
  a `GET` for a year never set returning `200`.
- **Frontend** — `VisionPage` renders pillars and both measure kinds;
  the empty state; `VisionModal` adds and removes a pillar, a measure and a
  milestone, and submits the document it was given; the Overview card's
  first-measure-per-pillar rule including the no-measures fallback.
- **At least one test mutation-checked** (`proving-tests-can-fail`) — break the
  code it covers on purpose and watch it go red before believing it.

## Out of scope

- **The marriage-duration block** (decision 2) — drawn, not built.
- **Milestone-to-goal links** (decision 6) — the design's own note stays prose.
- **A derived-source enum for measures** (decision 1) — rejected, with the cost
  of rejecting it stated.
- **A third "behind pace" measure colour** — decoration in the mockup; a measure
  is met or it is not.
- **Reordering by drag** — `position` exists and is written from array order, so
  adding reordering later is a UI change, not a schema one.
- **Agreements** — the third Marriage spec, and it needs its own conversation
  about what "both sign" means in a one-owner household before it can be
  written.

## Definition of done

`make lint && make test` green, at least one new test mutation-checked,
`docs/FEATURE_TRACKER.md` and `docs/LEARNING.md` updated, and
`docs/SYSTEM_DESIGN.md` kept true — this feature adds four tables, two routes
and a port, every one of which that document draws.

Then **the fifteen-criterion browser walk**, the same bar every Money feature
and Retros was held to, recorded in its own verification file. A feature is not
done because its tests pass.
