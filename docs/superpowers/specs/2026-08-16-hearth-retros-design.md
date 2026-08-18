# Hearth — Retros

Spec for slice 3's first feature: the Marriage → Retros screen, its Start-retro
modal, the actions a retro leaves behind, and Overview's "Next retro" card.
Written 2026-08-16 after a brainstorming session; the decisions below record
what was chosen and why, in the order they were made.

The design source is the Retros section of `design/Household Dashboard.dc.html`
(the "Marriage retros" screen and the `modalRetro` Start-retro modal), plus the
flow map's entry `4b Marriage · Retros` — "Retro history timeline + latest retro
detail (went well / was hard / actions / notes / mood trend)".

**Retros is the first of three Marriage specs**, not the whole slice. Marriage
is thirteen features across three pages, which is the size Money was, and Money
took five specs. The order is Retros → Vision → Agreements:

- **Retros first** because it is the smallest complete loop, because it exercises
  the route-and-guard restoration on the simplest possible surface, and because
  its mood chart draws twelve months of history the household does not have yet.
  Every month spent building Vision and Agreements first is a month that chart
  stays empty on the day it ships.
- **Agreements last** because propose → both sign, with preserved history, is
  the genuinely hard design in this space, and it carries a product question —
  what "both" means in a household with one owner — that deserves its own
  conversation rather than a corner of this one.

## What Marriage inherits, and what it does not

`docs/HANDOVER.md` §4 records that Money closed three standing items across five
features. Two of them do not transfer: Marriage has no monetary path to keep
`int64`-honest, and its capability gating is a different shape from Money's. The
third transfers completely — **no implementer should be inventing a derived
figure that no decision recorded first** — which is why this document pins every
number the Retros screen displays before any of it is built.

One thing Marriage genuinely does not inherit: **its routes were deleted.**
`110ab0a` removed `/marriage`, `/marriage/$`, their placeholder page, the
`SPACE_PAGES` entry and the `RequireCapability cap="marriage"` guard together,
because a navigation row whose only content was "Arriving in slice 3" reads as a
broken product. All of it comes back in this feature's first task, in one change
— see decision 10.

## Decisions

1. **A retro is one shared draft, and its lines carry no author.** One `retros`
   row per household per calendar month. Either owner opens it and adds lines to
   "What went well" and "What was hard"; one mood is recorded for the month.

   The design's modal says "each of you adds their own", and the design's own
   rendering draws no name against any line — the June retro's nine bullets are
   an unattributed list. Attribution would mean an author column, a second mood,
   and a decision about what "mood 4/5" means when two people disagree, none of
   which the design draws anywhere. "Each of you adds their own" is therefore a
   convention the couple follows, not a thing the app enforces or displays.

   The accepted cost, stated plainly: a household cannot later ask "whose month
   was hard?" of its own history. Returned cheaply if wanted — an author column
   on a line table — but only against a real request, not a guess.

2. **`completed_at` is the whole draft concept, and finishing does not lock
   anything.** A retro with `completed_at IS NULL` is a draft: shown on the page
   as its own "in progress" entry, and excluded from the finished count and the
   mood chart — a half-typed month must not become a point on a mood trend.
   Finishing stamps the timestamp. Everything stays editable afterwards — the
   text, the mood, the actions.

   This is a private journal for two people, not a record anyone audits.
   Freezing it would mean a typo is permanent and a line remembered the next
   morning has nowhere to go. **Agreements is where this product needs
   immutability and signatures**, and it will get them there; Retros must not
   inherit that weight because it sits in the same space.

   No status enum: one nullable timestamp answers "is it done", "when was it
   done", and orders the history, and a `status` column would let a row claim
   `finished` with no completion time.

3. **The 10-minute money check-in is read live and stored nowhere.** The modal
   calls the existing `GET /budgets/{month}` for the retro's own month, and `GET
   /goals`, and renders what they say. No figure is copied into the retro.

   **The two are scoped differently, and the panel must say so rather than imply
   otherwise.** The budget figures are that month's; goals' "X of Y on track" is
   a live figure with no month dimension at all — a goal's progress is a
   contributions ledger against a target date, not a monthly bucket. So the
   panel labels the goals line as today's standing, not July's. Inventing a
   month-scoped goals figure to make the two match would mean a new endpoint and
   a new derived number, for a panel whose whole job is to start a
   ten-minute conversation.

   This matches the design exactly: it draws budget and goals numbers inside the
   modal and shows **no money section at all** on the saved June retro detail.
   Storing them would add columns, invent a snapshot semantics for a ledger that
   can be corrected afterwards, and render a section the design never draws.

   The accepted cost: reopening June's retro in December shows December's reading
   of June, and a month whose budget was deleted shows Budget's own "no budget
   set" copy rather than history. Both are honest — the panel is a prompt for a
   ten-minute conversation, not a record of one.

4. **An unfinished action is offered to the next retro, and carried with one
   click.** Opening a new retro lists the previous month's unticked actions under
   "Still open from July". Clicking one creates a fresh action on the new retro,
   with `carried_from` pointing at the original. July's own row is untouched and
   stays unticked.

   Nothing moves by itself. Budget decision 1, Goals decision 4 and Bills
   decision 3 each already refused to make something happen on a clock or on a
   read, and automatic carry-over is the same shape: a couple that deliberately
   abandoned an action would have to delete it every month, and "3 actions" would
   stop meaning anything — three new, or one new and two inherited?

   **Only the immediately previous month is offered.** A household that skipped
   four months should not be handed an unbounded backlog on the night they come
   back.

5. **A retro belongs to a calendar month; the button starts the current month
   unless the previous one was missed.** `UNIQUE (household_id, month)`, month
   stored as the first of the month — the convention `budgets` already uses.

   The startable month is the **earlier** of {previous month, current month} that
   has no retro row at all. If both have one, there is no start button and the
   page opens what exists. So a couple doing July's retro on 2 August files it as
   July, which is what they mean, and August is still available afterwards.

   Rejected: current-month-only, which files a 2 August retro as August and then
   leaves August without one — the design's own example retro is dated Jun 28,
   near the edge of its month. Also rejected: a free month picker, which is more
   UI than a rare need justifies and lets the mood chart change shape
   retroactively.

6. **Both partners can type into one draft, so a stale save is refused, never
   merged.** `retros` carries a `version` integer. Every text-or-mood save sends
   the version it loaded; a mismatch returns `409` and the page says "Christine
   changed this while you were typing — reload to see it."

   The alternative shapes were considered and rejected. **One row per line**
   (`retro_lines`) makes appends collision-free structurally, but turns the
   modal's two textareas into add-a-line lists with per-row edit and delete
   controls — more UI than the design draws, for a couple typing three bullets
   each. **Last write wins** is cheapest and loses a partner's paragraph with no
   error and no trace, which is the silent-failure shape this codebase refuses
   everywhere else.

   **Ticking an action does not touch `version`**, because it writes a different
   table. One partner ticking an action all month can never collide with the
   other editing the notes.

7. **The history row's quoted line is derived from the notes, not a second
   field.** The design renders `June 2026 · Mood 4/5 · 3 actions · "best month
   this year"`, and June's notes open with the sentence "Best month this year."
   It is the first sentence, not a summary somebody types twice.

8. **The "45 min" duration on the retro header is not built.** It needs either a
   timer this product does not have or a field nobody fills honestly. The feature
   tracker records that the design draws it, the same way it records every other
   drawn-but-unbuilt thing, rather than leaving a silent gap.

9. **Overview's "Next retro" card ships in this feature.** Goals and Bills each
   built their own Overview card in their own round rather than deferring it, and
   the card needs nothing this feature does not already produce.

   It carries a gating point that must not be got wrong: **Overview is the only
   page every member reaches.** The card renders only for a member holding
   `marriage`, and its *absence* for everyone else needs a positive test. An
   absence assertion holds perfectly over a blank page — that is exactly how the
   interim Overview shipped a limited member a page containing only the word
   "Overview" with every unit test green (`docs/LEARNING.md` pattern 2).

10. **The route, the sidebar entry and the guard come back in one change.**
    `router.tsx` gets `/marriage/retros` wrapped in `RequireCapability
    cap="marriage"`; `Sidebar.tsx` gets a `SPACE_PAGES` entry for marriage. The
    sidebar renders a builtin space only when that map names at least one built
    page, so without the entry the space stays invisible however many routes
    exist. Splitting these across tasks produces a "missing sidebar link" that
    looks like a defect and is not.

11. **The guard is `requireCapability(marriage)` stacked on `requireOwner`,
    even though the pair is redundant today.** `domain.ValidateMembershipChange`
    already refuses a limited member holding marriage
    (`ErrLimitedCannotHoldMarriage`), so every marriage-holder is an owner. The
    stack goes in anyway, for the reason `router.go` already gives itself about
    the money group: a route that leans on an invariant enforced in another layer
    for another reason opens silently if that invariant is ever relaxed, with no
    failing test to catch it.

12. **The monthly retro reminder is not sent, and this feature does not change
    that.** `Notifications — monthly retro reminder` is 🟡 in the tracker as of
    2026-08-16: the preference is stored, served and editable, and nothing sends
    it, because nothing in this codebase runs on a clock. Building this product's
    first scheduler inside its first Marriage feature is the trade Budget, Goals
    and Bills each already refused. It stays 🟡, and the scheduler gets its own
    spec covering all four notification rows plus automatic contributions and
    rollover.

## Data model

Migration `00009_retros.sql`, following the conventions in `00006_budgets.sql`
and `00007_goals.sql`.

```sql
CREATE TABLE retros (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    month        date        NOT NULL,
    mood         smallint    CHECK (mood BETWEEN 1 AND 5),
    went_well    text        NOT NULL DEFAULT '',
    was_hard     text        NOT NULL DEFAULT '',
    notes        text        NOT NULL DEFAULT '',
    completed_at timestamptz,
    version      integer     NOT NULL DEFAULT 1,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, month)
);

CREATE TABLE retro_actions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    retro_id     uuid        NOT NULL REFERENCES retros(id) ON DELETE CASCADE,
    body         text        NOT NULL,
    done_at      timestamptz,
    carried_from uuid        REFERENCES retro_actions(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE retro_action_assignees (
    action_id     uuid NOT NULL REFERENCES retro_actions(id) ON DELETE CASCADE,
    membership_id uuid NOT NULL REFERENCES memberships(id) ON DELETE CASCADE,
    PRIMARY KEY (action_id, membership_id)
);
```

The parts that are decisions rather than typing, each of which belongs as a
comment at the line a future editor would change:

- **`month` is the first of the month**, the same convention
  `TransactionRepository.MonthTotals` and `budgets` already take. One retro per
  household-month, enforced in the schema rather than in a service.
- **`mood` is nullable and never 0.** A draft where nobody has picked an emoji
  has no mood; zero would be a claim. Same reasoning as
  `budgets.expected_income_minor`.
- **`completed_at IS NULL` means draft** (decision 2). No status column.
- **`version` is the concurrency guard** (decision 6), incremented by the retro
  update path only.
- **`carried_from` is provenance, `ON DELETE SET NULL`** — deleting July's action
  must never delete August's copy of it, and the nullable reference lets the UI
  say "carried from July" rather than showing a bare duplicate.
- **Assignees are a join table, not two boolean columns.** The design draws
  exactly `A` and `C`, but nothing in this product caps a household at two
  owners: the invite modal offers "Parent" freely, and last-owner protection only
  guarantees at least one. Two columns would encode a limit that does not exist.
- **Assignee is a `membership_id`**, matching `transactions.paid_by_membership_id`,
  so a removed member behaves the way removed members already behave everywhere.
- **A draft can be deleted; a finished retro cannot.** The delete is scoped
  `WHERE completed_at IS NULL` in SQL — see the API section for why that is not a
  service-level `if`.
- **Actions carry no `position` column, and order by `created_at, id`.** An
  explicit ordering integer needs a writer, and the only safe writer is
  `max(position) + 1` computed inside the insert — which two partners adding an
  action at the same moment can still collide on, since nothing else in this
  feature serialises that path (the `version` guard covers the retro's text, not
  its actions). Since the design draws no reordering control anywhere, the column
  would exist only to create that race. Insertion order is the order, with `id`
  as a stable tiebreak for two inserts landing in the same microsecond. **Adding
  drag-to-reorder later means adding the column then**, with a rule for writing
  it — not inheriting one that was never assigned a writer.

## The formulas, pinned

Every number the Retros screen displays, defined here so no implementer invents
one. This is the discipline `docs/HANDOVER.md` §4 records as having held across
five Money features in a row.

| Figure | Definition |
|---|---|
| `12 done since Aug 2025` | `count(*)` over retros with `completed_at IS NOT NULL`; "since" is `min(month)` of those rows. With no finished retros the header omits the phrase entirely rather than saying "0 done since —" |
| Mood over 12 months | The twelve calendar months ending at the current month. Each point is that month's **finished** retro's mood; a month with no finished retro, or a finished retro with no mood, is a **gap**. Never zero — zero is a claim, the same rule Budget applies to transactions it cannot convert |
| History row | `Mood N/5 · K actions · "<first sentence of notes>"`. `K` counts all of that retro's actions, ticked or not. A retro with no mood omits the mood clause; with no actions, omits the action clause; with no notes, omits the quote — never renders `0 actions` or empty quotation marks |
| First sentence | Up to and including the first `.`, `!` or `?`; failing that, the first 60 characters with an ellipsis; failing that (empty notes), nothing |
| Startable month | The earlier of {previous month, current month} with no retro row. Both present → no start button (decision 5) |
| `Still open from July` | Actions on the **immediately previous** month's retro with `done_at IS NULL`. Previous month only (decision 4) |
| Money check-in | Not ours. `GET /budgets/{month}` for the retro's own month plus `GET /goals`, rendered as they answer; a month with no budget shows Budget's own empty copy. The goals line is today's standing, not the month's, and is labelled that way — goals have no month dimension (decision 3) |
| Overview "Next retro" card | The current month's retro if one exists (draft or finished), else the startable month as a prompt to begin. Beneath it, that retro's open actions — the design's "carried-over actions". Renders only for a member holding `marriage` (decision 9) |

## API

A new group under the authenticated router:
`requireCapability(domain.CapMarriage)` stacked on `requireOwner` (decision 11),
with every write additionally behind `requireCSRF`, the house shape.

```
GET    /api/v1/retros                          list, counts, mood series, startable month
GET    /api/v1/retros/{month}                  one retro, its actions, the carry-over offer
POST   /api/v1/retros                          create the draft                      (CSRF)
PATCH  /api/v1/retros/{month}                  mood + text + version                 (CSRF)
POST   /api/v1/retros/{month}/complete         finish                                (CSRF)
DELETE /api/v1/retros/{month}                  draft only                            (CSRF)
POST   /api/v1/retros/{month}/actions          body, assignees, carriedFrom          (CSRF)
PATCH  /api/v1/retros/{month}/actions/{id}     the tick                              (CSRF)
DELETE /api/v1/retros/{month}/actions/{id}                                           (CSRF)
```

**Addressed by month, not by id**, the way `/budgets/{month}` already is: month
is the natural key, the URL reads like the screen, and the page never has to hold
an id to open the thing it is already looking at. The month parser is **the one
Budget's handlers already use** — not a second copy two files away, which is
pattern 1's most common shape in this codebase.

**Complete is its own route, not a field on PATCH**, the same reasoning archive
already carries on accounts, categories, goals and bills: if finishing were
patchable, saving a typo could finish the retro as a side effect.

Ports, narrow, declared in `usecase/ports.go`:

```go
type RetroRepository interface {
    Create(ctx, householdID, month) (Retro, error)       // ErrAlreadyExists on the unique clash
    ByMonth(ctx, householdID, month) (Retro, error)      // ErrNotFound
    List(ctx, householdID) ([]RetroSummary, error)       // newest first, action counts included
    Update(ctx, RetroUpdate) (Retro, error)              // ErrRetroChanged when version is stale
    Complete(ctx, householdID, retroID, at) (Retro, error)
    DeleteDraft(ctx, householdID, retroID) error         // WHERE completed_at IS NULL
}

type RetroActionRepository interface {
    Add(ctx, RetroActionInput) (RetroAction, error)      // action + assignees in one transaction
    SetDone(ctx, householdID, actionID, done bool) error
    Remove(ctx, householdID, actionID) error
    OpenInMonth(ctx, householdID, month) ([]RetroAction, error)
}
```

**`List` is deliberately unbounded, and that is a decision rather than an
oversight.** Every other list surface here that could grow was given a bound —
transactions took keyset paging, budget history takes `?months=6`. Retros grows
twelve rows a year, so a decade of use is 120 rows and one query. The design's
"Show 2025 (7 more) ↓" is a **disclosure over data the page already has**, not a
second request: the history renders the current year expanded and older years
collapsed behind that control. Nobody should add paging to this endpoint without
first finding a household the flat list actually hurts.

**`DeleteDraft` puts `completed_at IS NULL` in the `WHERE` clause, not in a
service `if`.** A check-then-delete can race, and — more importantly here — a
zero-row match must return `ErrNotFound` rather than reporting success. That
exact defect shipped in Bills (`SetBillNextDue` committing two of three writes on
a zero-row match) and is in `docs/LEARNING.md`'s database catalogue. `Add`
writes the action and its assignees in one transaction for the same reason: a bad
assignee must leave no orphan action behind.

The service (`usecase/retro.go`) takes **no actor parameter**, per the project
rule: services enforce what is valid, middleware enforces who is asking.

The domain layer (`internal/domain/retro.go`) holds what needs no database: mood
parsing that **fails closed** on anything outside 1–5 (it arrives from both a
request body and a database column), the startable-month rule as a pure function
of today plus which months exist, and first-sentence extraction. A new
`ErrRetroChanged` joins `errors.go`.

## Screens and states

`/marriage/retros`, built as `web/src/features/marriage/`:

| File | Job |
|---|---|
| `useRetros.ts` | List, counts, mood series, startable month |
| `useRetro.ts` | One month's detail, its actions, the carry-over offer |
| `RetrosPage.tsx` | Layout and screen states only |
| `RetroHistoryList.tsx` | Month rows, with the design's "Show 2025 (7 more)" |
| `MoodChart.tsx` | Twelve months, inline SVG |
| `RetroDetail.tsx` | Went well / was hard / actions / notes |
| `RetroActionRow.tsx` | One action: body, assignee initials, the tick |
| `RetroModal.tsx` | The start/edit modal |
| `MoneyCheckInPanel.tsx` | Budget + goals read, inside the modal |

**Fetch orchestration lives in the hooks from the first task.** Budget's spec
decision 11 did this deliberately after `TransactionsPage.tsx` grew past 500
lines doing fetching, pagination, body translation and rendering together — still
unsplit, still on the follow-up list. Retros copies Budget's shape.

**No chart library.** Twelve points as an inline SVG polyline with gaps for
missing months is less code than a dependency, and floating dependency versions
have broken this build twice.

The five states, all of which need a test, because **an absence assertion holds
perfectly over a blank page**:

1. **No retros ever** — an honest first-run panel and "Start your first retro".
2. **A draft exists** — the history shows it as in progress, and it is excluded
   from the chart and the finished count.
3. **Normal** — history, mood chart, the latest finished retro's detail.
4. **Not an owner** — the `goals-owner-only` copy shape, explaining the state.
5. **Load failure** — the red alert, kept genuinely distinct from state 4.

States 4 and 5 are built together from day one rather than found later. Bills
shipped without that distinction and its browser walk found the identical gap
sitting in `BudgetPage.tsx` and `TransactionsPage.tsx` as well
(`docs/LEARNING.md` pattern 1). A Marriage page is more exposed than any money
page: every household has children, and `/marriage/retros` is reachable by
anyone who types it.

**Where each thing is edited.** The modal owns writing the retro — mood, the two
textareas, adding actions, the carry-over offer. The page's detail view owns the
tick, because actions get ticked all month, long after the retro night, and
reopening a modal to tick a box is the wrong shape.

**Mobile from the start**, not retrofitted: the `sm`/`lg` two-breakpoint
convention, `dvh` on the modal, and the 44px touch floor. The mood picker is five
targets in a row and is the tightest thing here at 320px; if they cannot all
clear the floor, the gap is named in the tracker row the way the mobile round
named its five, not shipped quietly small.

## Error handling

| Case | Answer | What the screen says |
|---|---|---|
| Stale `version` on PATCH | `409` | "Christine changed this while you were typing — reload to see it." Never a merge, never a red failure alert |
| `POST /retros` for a month that already has one | `409` | Nothing — the page opens the existing retro. This is also what makes a double-clicked button harmless |
| `GET /retros/{month}` with no retro | `404` | Read as "not started", not as an error |
| `DELETE` a finished retro | `404` from the zero-row match | "That retro is finished and cannot be deleted" |
| Mood outside 1–5, from anywhere | Refused | Fails closed at the domain boundary |
| Not an owner | `403` from the guard | State 4's explanation, not state 5's alert |

Every 2xx except the `204` on delete carries a JSON body, because `apiFetch`
throws on an ok response it cannot parse. PATCH returns the retro **including its
new `version`**, so the client never has to guess what to send next.

## Testing

**Go.** Domain table tests: mood parsing against 0, 6 and garbage; the
startable-month rule across all four states; first-sentence extraction including
no-notes and no-terminator. Usecase tests against in-memory doubles, the existing
pattern. Postgres tests via testcontainers for the four things only a real
database proves — a stale-version update is refused rather than merged;
`DeleteDraft` against a finished retro matches zero rows and returns
`ErrNotFound` instead of reporting success; the unique-month clash surfaces as
`ErrAlreadyExists` rather than a raw pgx error; an action and its assignees are
written in one transaction.

A new `api/internal/adapter/http/marriage_api_test.go` carries the route-walk
matrix — every route against no session, a limited member, an owner without CSRF,
and an owner with it. It joins as its own file rather than growing `api_test.go`,
which was already split by feature area for this reason.

**Frontend.** `stubFetchRoutes` for every request — a stub that ignores the URL
has silently passed broken code twice in this project. All five screen states.
The 403-versus-server-error pair in the `GoalsPage.test.tsx` shape. The version
conflict's copy.

**At least one mutation-checked test per task**: break the code deliberately,
watch the test go red, restore. Five tests in this project have passed against
deliberately broken code.

**The browser walk, fifteen criteria**, run against a real database before
anything is called done. Two criteria exist because of specific past defects and
must not be dropped:

- **Keyboard focus on the mood picker.** Five emoji over radio inputs is exactly
  the shape that shipped broken in Transactions: `sr-only` radios whose visible
  labels never reacted to the hidden input's focus, so Tab and the arrow keys
  moved real focus with nothing visible on screen. `fireEvent.click` never
  presses a key, so no unit test can find it. Walk it with the keyboard, and
  compare screenshots by hash rather than by eye — that round's own fix was
  caught half-wrong by two byte-identical screenshots.
- **The concurrency refusal, in two real browsers.** Both sessions open the same
  draft, both edit, the second save is refused with the reload copy and nothing
  is lost. jsdom cannot express two sessions.

## Out of scope

- **The "45 min" retro duration** (decision 8). Drawn by the design, not built;
  recorded as such in the tracker.
- **Per-line attribution and per-partner moods** (decision 1). Returned cheaply
  if a real request appears.
- **Vision and Agreements** — their own specs, in that order. Agreements'
  propose → both-sign flow carries an unanswered product question (what "both"
  means with one owner) that belongs in its own conversation.
- **Sending the monthly retro reminder** (decision 12). Needs this product's
  first scheduler; that is its own spec, covering all four notification rows plus
  automatic goal contributions and budget rollover, which are the same missing
  piece rather than three independent gaps.
- **Editing a finished retro's month.** A retro filed against the wrong month is
  fixed by deleting it while it is a draft, or living with it. A month-move would
  need to defend the unique constraint and the mood chart's shape for a mistake
  that takes one click to avoid.

## Definition of done

`make lint && make test` green on the integrated tree, at least one
mutation-checked test, the fifteen-criterion browser walk passed and recorded in
`docs/superpowers/plans/`, and the three tracking documents updated **in the same
round**:

- **`docs/SYSTEM_DESIGN.md`** — a new Marriage section: the tables, the route
  group and its guards, and the retro flow. Use the `maintaining-system-design`
  skill.
- **`docs/FEATURE_TRACKER.md`** — checked against the section 6 table as it
  stands, which holds thirteen ⬜ rows, four of them this feature's:

  | Row | After |
  |---|---|
  | Retro history with mood | ✅ |
  | Mood chart over 12 months | ✅ |
  | Single retro view — went well, was hard, actions, notes | ✅ — the actions list belongs to this row by its own name, so it gets no separate row |
  | Start retro (modal) with mood, money check-in and actions | ✅, with a note that the design's "45 min" duration is drawn and not built (decision 8) |

  Plus two new rows the design's own mockup never draws, the shape Goals'
  contributions and Accounts' archive/restore rows already take — **carry an
  unfinished action into the next retro** and **delete a draft retro** — and
  Overview's "Next retro card with carried-over actions" ⬜ → ✅. The nine
  remaining Marriage rows stay ⬜; they are Vision's and Agreements'.

  Recount the summary table by counting symbols, never by adjusting the previous
  totals — that has produced wrong numbers in this file before, in both
  directions at once.
- **`docs/LEARNING.md`** — what this round taught, added to an existing pattern
  where one fits rather than starting a new section.
