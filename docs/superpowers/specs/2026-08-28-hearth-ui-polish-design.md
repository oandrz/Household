# Hearth — the interaction skin, and seven defects

Written 2026-08-28, from a browser walk of the running app taken as a UI/UX
critique rather than a feature verification. Every page was opened in turn at
a desktop viewport of 2752×1054, then again at 390×844 through CDP viewport
emulation. Modals were opened and submitted empty; focus, validation and
computed styles were read out of the live DOM rather than inferred from the
source.

The mobile pass ran against the **seeded** household (`andreas@hearth.family`),
deliberately: the primary household on this machine holds one account, one
transaction and one achieved goal, which is too sparse to tell a layout problem
from an empty database. The seeded household has a populated ledger, and that
is what the mobile screenshots show. Both households are load-bearing here —
the achieved-goal defect (§1.1.2) only appears on the sparse one, and the
figure-alignment problem (§1.2) only appears on the populated one.

The raw findings are reproduced in §1 so this document stands on its own.

---

## 1 · What the walk found

### 1.1 Seven defects, each reproduced

#### 1.1.1 The Transactions header contradicts the list beneath it

The page reads **"0 in August 2026"** directly above ten visible July rows.

This is a backend contract violation, not a frontend mismatch.
`parseTransactionFilter` (`api/internal/adapter/http/transaction_handlers.go:228`)
sets the summary's month unconditionally:

```go
month := time.Now().UTC()
if raw := q.Get("month"); raw != "" {
        ...
        month = parsed
        filter.Month = parsed
}
```

`filter.Month` is set **only inside the branch**. So when no `?month=` arrives —
the default state of the screen — the list is filtered by a zero month (all
time) while the summary is computed for the current month. The two figures above
the ledger describe August; the ledger describes every month there has ever
been.

`handleListTransactions`'s own doc comment, thirteen lines above, states the
contract the code breaks:

> serves the ledger and the two figures above it together, because they are one
> screen and must describe the same month.

#### 1.1.2 The Overview's "Goals on track" card renders as a heading over nothing

`GoalsCard.tsx:83-99` has three conditional clauses — `trackClause`,
`summary.nextGoal`, `noDateClause` — and an `hasAnyGoals` guard above them
choosing between the empty state and those three.

When a household's only goal is **achieved**, all four conditions land on the
wrong side at once. `hasAnyGoals` is true, because `goals.goals` is non-empty.
But an achieved goal is counted in neither `datedCount` nor `noDateCount` (the
backend's `List` loop checks achieved before the dated/undated split), and it is
not `nextGoal`. Every clause is null, the empty state is skipped, and the card
paints its heading over blank space.

The component's own comments name this shape twice — "the state that would
otherwise render this card as a heading over nothing" — and guard `hasAnyGoals`
carefully against exactly this input. The guard is correct; there is simply no
fourth branch behind it.

#### 1.1.3 Form errors are Chrome's, not Hearth's

Submitting "Log a transaction" empty produces no inline message. The browser's
own `Please fill out this field.` bubble appears, and the field takes Chrome's
default focus ring, measured live at `rgb(0, 95, 204)` and `0.8px`.

The inline error machinery **already exists and already works**. Hearth renders
errors as `role="alert"` over `text-danger` in twelve forms, `TransactionModal`
among them — it holds `amountError` and `receivedAmountError` state, sets both
from `handleSubmit`, and renders both.

The empty field never reaches any of it. `noValidate` appears in **zero** of the
fifteen forms, so the browser's own constraint validation intercepts the submit
event before `handleSubmit` runs. `TransactionModal.tsx:236-241` says so in a
comment, about its own sibling field:

> a literally empty required field never reaches this far at all (native
> validation stops the submit event first, the same as any other required input
> on this form)

So the defect is not a missing error message. It is that native validation is
the app's first-line error surface, and Hearth's own messages only fire for
inputs that get past it — a bad number format, a zero amount.

Two censuses were run and one was wrong; the corrected one is below. An earlier
pass counted the *substring* `required`, which scored `GoalCard.tsx` at five —
those are all `requiredMonthlyMinor` and `requiredMonthlyOk`. **`GoalCard.tsx`
has no form fields at all** and is not part of this work. Counting the JSX
attribute:

| file | `required` attrs | `role="alert"` |
|---|---|---|
| `TransactionModal.tsx` | 7 | 3 |
| `GoalModal.tsx` | 4 | 5 |
| `BillModal.tsx` | 4 | 4 |
| `SignUpCompleteScreen.tsx` | 4 | 2 |
| `AccountModal.tsx` | 3 | 2 |

Every one of the fifteen forms has the same shape. `TransactionModal` is the
instance reproduced in the browser; the class is all fifteen.

One thing to know before touching them: `formState` appears in **zero** files.
`react-hook-form` is a dependency, but every form here is a hand-rolled
`handleSubmit`. The fix follows that pattern and does not introduce
react-hook-form's validation surface.

#### 1.1.4 The ⌘K chip does nothing

`Sidebar.tsx` renders a `⌘K` chip in the brand row and says so itself:

> The ⌘K chip is static — no command palette exists to open.

It also renders inside the mobile drawer, on devices with no ⌘ key.

#### 1.1.5 No skip link

`document.activeElement` traversal confirms the first tabbable element on every
page is the Overview nav link. A keyboard user passes eight navigation links
before reaching page content, on every navigation. There is no skip link
(`a[href="#main"]` and friends: none present).

The landmark structure is otherwise sound — `HEADER`, `NAV`, `MAIN` are all
present, every `<img>` has `alt`, and each page has exactly one `<h1>`.

#### 1.1.6 Modals open focused on ✕

`Modal.tsx` is built on native `<dialog>` and defers focus to `showModal()`,
which lands on the first focusable descendant. That descendant is the header's
close button, because the header precedes the form. Opening "Log a transaction"
from the keyboard puts the cursor on **Close**, not Amount.

The rest of the component is correct and should not be touched: it traps focus,
handles Escape through `oncancel`, restores focus to the trigger on close, and
carries a well-reasoned `supportsShowModal` guard.

#### 1.1.7 A cleared month filter reads as broken

The Month filter renders `--------- ----`.

**This was mis-diagnosed in the raw findings as an `<input type="date">`.** It
is not. `TransactionFilters.tsx:202` is already `type="month"`, and the file's
own comment says so. The dashes are Chrome's empty-state placeholder for a month
input.

The finding survives, smaller: the cleared state looks like a broken control
rather than like "any month", and there is no affordance saying which it is.

### 1.2 The app has no interaction states

Counted across roughly ninety components:

| | count | consequence |
|---|---|---|
| `focus-visible:` / `focus:` | **0** | the focus ring is Chrome's blue, inside a cream-and-forest palette |
| `active:` | **0** | nothing responds to being pressed |
| `hover:` | **4** | all four in `BudgetPage.tsx` and `BudgetHistoryModal.tsx`; rows, nav links and the Edit/Archive controls are inert |
| `tabular-nums` | **0** | see below |
| `animate-` | **0** | fifteen modals snap into place |

Real CSS transitions: **two** — `NavDrawer.tsx:101` (the drawer's transform) and
`ToggleSwitch.tsx:31` (the toggle's colours).

Nothing anywhere sets `outline-none`. A focus ring is therefore a **substitution**
for Chrome's, not an addition on top of nothing — which is why this costs a rule
rather than an audit.

The missing tabular figures are visible in the populated ledger, where amounts
are right-aligned but the digits wander down the column: `−€20.00`,
`−Rp 150,000`, `−S$10,000.00`, `−S$400.00`.

`formatMoney.ts` shows the intent already exists. It spends a comment on using
U+2212 MINUS rather than a hyphen —

> it aligns with digits at the same width, which a hyphen does not, and every
> negative figure in this app is in a column of numbers

— which is the same argument for tabular figures, made about the sign and not
yet about the digits.

### 1.3 Numbers that read wrong

**"0% used" for a household that has spent.** `domain.PercentUsed` rounds to the
nearest whole percent, so S$2.00 against S$800.00 renders `0% used` on the
Overview card and again in the Budget subtitle.

The rounding is correct and should not change. The display is the lie.

The copy that renders it is **duplicated**: `features/overview/copy.ts:34` and
`features/money/budgetCopy.ts:22` are both `` `${percent}% used` ``, and there
are three call sites — the Overview's `BudgetCard`, `BudgetPage`, and
`MoneyCheckInPanel.tsx:70`, which reads `budgetCopy`'s. Fixing either definition
alone leaves the other standing. This is the sibling-defect shape
`docs/LEARNING.md` already records five times.

### 1.4 Three font families are downloaded and never used

`index.html` loads five families from Google Fonts. Two are tokens nothing
references, and one is not even a token:

| family | token | components using it |
|---|---|---|
| Schibsted Grotesk | `--font-sans` | 6 |
| Newsreader | `--font-serif` | 8 |
| Karla | `--font-alt` | **0** |
| IBM Plex Mono | `--font-mono` | **0** |
| IBM Plex Sans | *(none)* | **0** |

### 1.5 What already works, and is the model to copy

Named so the work below does not "fix" it:

- **The empty states are good.** "No goals yet / Create a goal", "No budget set
  yet / Set a budget", and the Net-worth card's "Not enough history yet — the
  chart starts once there are two months to compare" are all composed, actionable
  and correctly scoped. §1.1.2 is a missing branch, not a missing empty state.
- **The mobile shell is sound.** At 390px the cards stack, the drawer opens over
  an `inert` main, the active link takes the accent, and Settings plus the
  account row stay pinned to the bottom.
- **The sign-in screen is the design bar** the rest of the app should meet.
- **`Modal.tsx` is correct** apart from where it lands focus.
- **The whitespace on the primary household is an empty database**, not a layout
  fault. It is not a finding.

---

## 2 · Decisions taken before designing

1. **The ⌘K chip is deleted, not implemented.** A command palette is a feature
   and deserves its own spec, its own tracker row and its own slice. Shipping
   one inside a defect-fix batch would smuggle a feature past the tracker; the
   chip meanwhile promises something the product cannot do. Two lines out.
2. **Of seven taste findings, three are taken and four deferred.** The three
   taken (§5) are places the app is inconsistent *with itself* — arguable
   against the codebase rather than against taste. The deferred four (all-caps
   micro-labels, a dismissible Transactions banner, hiding zero-budget
   categories, a mobile filter disclosure) are judgment calls and belong in a
   pass of their own.
3. **Motion goes no further than state transitions.** The hover, active and
   focus changes of §3 get a duration and a `prefers-reduced-motion` guard.
   Modal and drawer entrance animation is out: `<dialog>` entrance needs
   `@starting-style` or an open-state class, which is a real addition to a
   component that is otherwise correct.
4. **`domain.PercentUsed` is not changed.** Its rounding is right. The fix is
   in the copy layer, where the display decision belongs, and both call sites
   already carry `spentMinor` alongside `percentUsed` — so no new field crosses
   the wire.
5. **`BudgetByPerson`'s bar scale is left alone.** Scaling to household spend
   rather than to the budget is documented intent matching the design's
   three-member mockup. At one member it always paints a full bar, which is a
   real problem — and a design decision, not a defect. It gets its own look.
6. **Three milestones, three pull requests.** M1 changes how every page looks,
   so it lands and is walked first; every later walk then happens against the
   final skin instead of against one that is about to change. A visual
   regression in M1 must not be able to hide inside a bugfix diff.

---

## 3 · M1 — The interaction skin

One file carries most of this. `src/index.css` is the repo's designated single
source of tokens, and CLAUDE.md's rule is that nothing hard-codes a hex.

### 3.1 Tokens

Four additions to `@theme`, all expressed as tokens so call sites stay free of
raw values:

- a focus-ring colour derived from `--color-accent`, plus its width and offset;
- `--transition-state`, roughly 160ms, for hover/active/focus changes;
- nothing else. No new colour family, no new radius.

### 3.2 The focus ring

A global `:focus-visible` rule replacing Chrome's `rgb(0, 95, 204)` 0.8px
outline with the accent ring. `:focus-visible`, not `:focus`, so a mouse click
does not paint a ring the pointer user did not ask for.

Because nothing sets `outline-none`, this is a substitution — every focusable
element in the app inherits it without being touched individually.

### 3.3 Tabular figures

A single utility class in `index.css` carrying
`font-variant-numeric: tabular-nums`, applied at the elements that render
`formatMoney`'s output — the ledger's amount column, the stat cards, the
breakdown rows, the goal figures — and **not** on `body`: a proportional heading
with a year in it should stay proportional.

Schibsted Grotesk ships tabular figures, so this needs no font change. That is
worth confirming in the browser before the class is spread, because a face
without them silently ignores the property.

### 3.4 Hover and active

Applied to the shared interactive surfaces, not sprayed per component:
`NAV_ITEM_CLASS` in `Sidebar.tsx`, the primary and secondary button patterns,
and the list-row patterns in the money features. A row that responds to the
cursor and a button that moves when pressed are the two changes that make the
largest difference per line.

### 3.5 Reduced motion

A `prefers-reduced-motion: reduce` block zeroing `--transition-state`. Added
with the transitions, in the same change, rather than as a follow-up.

### 3.6 The skip link

`AppShell.tsx` gains a visually-hidden-until-focused skip link before
`MobileTopBar`, targeting `<main>`. `<main>` takes the matching id.

### 3.7 The unused fonts

Karla, IBM Plex Mono and IBM Plex Sans leave `index.html`'s Google Fonts
request. `--font-alt` and `--font-mono` leave `index.css`, since a token
nothing references is a token that will be referenced by accident later.

If a monospace face is wanted for figures, that is a decision to take
deliberately, with tabular figures already in hand — not by leaving an unused
token lying where it can be picked up.

### 3.8 M1 definition of done

`make lint && make test` green. One new test mutation-checked. A browser walk of
every page at desktop and at 390px, comparing against the screenshots in this
document's own walk. `docs/LEARNING.md` updated.

---

## 4 · M2 — The seven defects

### 4.1 The Transactions month contract

`month` and `filter.Month` must always describe the same set. There are two
coherent ways to make them, and the difference is user-visible:

- **Scope the list to the defaulted month.** Cheapest — one line moved out of
  the `if`. But the page is titled *All transactions*, and this quietly removes
  all-time browsing from it.
- **Widen the summary when the list is unfiltered.** Preserves the page's stated
  purpose, but "Spent this month" then has to stop saying "this month", and the
  Budget link beside it does speak in months.

**Taken: neither half alone — make the default explicit instead.** The screen
opens on the current month, with that month shown in the Month filter rather
than left blank, and clearing the filter widens the list *and* the summary
together to all time. The header is then true in both states, the control always
reflects what is on screen, and nothing is taken away.

Concretely: `filter.Month` is set from the same defaulted `month` when no
`?month=` arrives; an explicit `month=all` (or equivalent) clears both. The
frontend sends the effective month rather than an empty string on first load.

Two handler tests, both of which must fail against today's code: with no month
filter, every returned transaction falls inside `summary.month`; with the filter
cleared, the summary covers the same range the list does.

This changes a documented route behaviour, so `docs/SYSTEM_DESIGN.md` is checked
in the same change.

### 4.2 The achieved-goal branch

`GoalsCard.tsx` gains the fourth branch: goals exist, but none is dated, undated
or next. It renders the achieved state — the goal's name and that it is funded —
rather than a heading over nothing.

The test states the input plainly: a household whose only goal is achieved.

### 4.3 Inline validation

`TransactionModal.tsx`'s `<form>` takes `noValidate`, so `handleSubmit` runs on
an empty field instead of being intercepted. The existing machinery then does
the work unchanged: `toMinorUnits("")` already returns `null`, which already
feeds `describeAmountError`, which already renders through `amountError`. Each
remaining `required` attribute on that form gets a matching JS check, so nothing
that native validation used to catch becomes silently submittable.

No new library, no `formState`, no new error component.

**The class, named rather than assumed.** All fifteen forms share this shape and
none carries `noValidate`. This milestone fixes the one instance reproduced in
the browser and records the other fourteen — per `hunting-sibling-defects`,
which exists in this repo because fixing one instance has failed to fix the
class five times. Converting the remaining fourteen is a follow-up of its own,
because each needs its own per-field JS checks and its own tests; doing them
inside this milestone would triple its diff and bury the seven defects.

### 4.4 The ⌘K chip

Deleted from `Sidebar.tsx`, with the comment that describes it.

### 4.5 The month filter's empty state

Falls out of §4.1. The input stays `type="month"`, but is populated with the
month actually in effect — read from `summary.month` — instead of opening blank.
The placeholder dashes then appear only in the deliberately-cleared all-time
state, where they are accompanied by a label saying so rather than standing
alone.

### 4.6 Modal focus

`Modal.tsx` focuses the first form field in the panel rather than accepting
`showModal()`'s first-focusable-descendant, which is the close button. Falls
back to the existing behaviour when the panel has no field.

Nothing else in the component changes.

### 4.7 "0% used"

One shared helper, consumed by all three call sites, so the duplicated
`` `${percent}% used` `` in `overview/copy.ts` and `budgetCopy.ts` collapses to
one definition. Nonzero spend that rounds to zero renders `<1% used`.

Both call sites already hold `spentMinor`; `MoneyCheckInPanel` reads
`budgetCopy`'s helper and picks the fix up with it. Per
`hunting-sibling-defects`, the third call site is checked explicitly rather than
assumed.

### 4.8 M2 definition of done

`make lint && make test` green. The Go month test and the achieved-goal branch
test both mutation-checked. A browser walk of Transactions, Overview, Goals and
the transaction modal. `FEATURE_TRACKER.md` and `LEARNING.md` updated;
`SYSTEM_DESIGN.md` checked for §4.1.

---

## 5 · M3 — Three consistency fixes

Each is a place the app disagrees with itself.

1. **Bills' "All caught up" panel** (`billCopy.ts:162`, rendered in
   `BillsPage.tsx`) sits on the bare canvas while every sibling panel on the
   page has a card surface. It gets one.
2. **Finances weights "Net" identically to "Cash & savings"** in
   `BreakdownCard.tsx`. Net is the conclusion the rows above add up to and
   should read as one.
3. **The Retros detail panel** renders `retroCopy.ts:71`'s
   "Select a retro to see its detail." as a single line in a 440px empty card.
   It gets a composed empty state, in the shape the Overview cards already use.

### 5.1 M3 definition of done

`make lint && make test` green. A browser walk of Bills, Finances and Retros.
`LEARNING.md` updated.

---

## 6 · Out of scope

- **A command palette.** §2.1.
- **The four deferred taste findings.** §2.2.
- **`BudgetByPerson`'s bar scale at one member.** §2.5.
- **Modal and drawer entrance animation.** §2.3.
- **`domain.PercentUsed`'s rounding.** §2.4.
- **The other fourteen forms' `noValidate` conversion.** §4.3 — named as a
  class, fixed at one instance, deliberately not swept here.
- **Anything about the sparse primary household's whitespace.** §1.5.
