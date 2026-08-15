# Hearth — mobile responsive

Hearth has never been usable on a phone. Not "cramped" — unusable. This is the
design for making every existing screen work down to a 375px-wide device
without redesigning any of them.

The constraint the product owner set on 2026-08-15 is the shape of this whole
document: **same UI, same layout, same structure — invent only where a phone
forces a choice.** Mobile here is a reflow of the screens that exist, not a
second product. Where invention was unavoidable, this document names it.

## What is actually broken

Measured in a real browser at a 375px viewport (360px client area after the
scrollbar), signed in as the seeded household, on 2026-08-15:

| Screen | Measurement |
|---|---|
| `/sign-in` | `document.scrollWidth` **452px** against a 360px client — the page scrolls sideways |
| Any signed-in page | Sidebar **236px**, `<main>` **124px** |
| Any signed-in page | Content after `px-9` gutters: **52px** |

At 52px the Overview renders roughly one word per line, its heading is clipped
mid-word, and the net-worth figure reads `S$0.` with the value cut off.

The second measurement is the one that shaped the plan. With the sidebar
hidden by hand and `<main>` given the full 375px, **overflow on the
Transactions page drops to zero**. The ledger's rows are already
`flex`-with-two-children (a label block and an amount), not a table, so they
wrap on their own. The shell is not merely the worst defect; it is most of the
defect.

## Decisions

**1. The sidebar becomes a drawer below `lg` (1024px), and nothing else about
navigation changes.** `AppShell.tsx` goes from `grid-cols-[236px_1fr]` to a
single column below `lg` and the existing two-column grid at `lg` and above. A
new `MobileTopBar` (brand, hamburger) appears below `lg`; a new `NavDrawer`
positions the sidebar.

Three things about the shell's structure that an implementer would otherwise
have to guess, and that a wrong guess makes visible immediately:

- **`MobileTopBar` is sticky**, not static. It carries the only route back to
  navigation on a phone, and a ledger scrolled two screens down must not strip
  it away.
- **`<main>` keeps exactly one scroll container.** Today `<main>` carries
  `overflow-y-auto` inside a `min-h-screen` grid and `<nav>` carries its own.
  Below `lg` the drawer's scroll and the page's scroll must not both be live,
  or a phone gets two nested scrollbars fighting one drag.
- **The drawer overlay sits above `z-10`.** `QuickAddMenu.tsx:63` is `z-10`,
  so an open drawer with a lower stacking order renders *underneath* the
  Overview's quick-add menu.

A fixed bottom tab bar — the other obvious phone pattern — was rejected on
evidence in the code, not taste. `Sidebar.tsx` renders from `me.spaces`, and
"+ New space" lets a household add spaces without limit. A five-slot tab bar
contradicts the data model the sidebar was deliberately built against. The
drawer also satisfies the "same structure" constraint literally: same
component, same links, same order, same grouping.

**2. `Sidebar.tsx` renders exactly once, and its own file does not change.**
The tempting shape — one copy inside the drawer for mobile, another in the
desktop column — would put two elements carrying `data-testid="sidebar-space"`
in the DOM at once. `Sidebar.test.tsx` queries that testid, so every such query
becomes ambiguous and the existing suite breaks on a change that was supposed
to be invisible to it. `NavDrawer` wraps the single instance and varies only
its positioning.

**3. The drawer is a CSS transform, not a native `<dialog>`, and we hand-write
what that costs us.** `Modal.tsx` documents at length why it is built on
`<dialog>`: the platform supplies focus trapping and Escape handling, and no
hand-rolled key handler has to be trusted. The drawer cannot take that deal,
because the same element must also be the *static desktop column*, and a
`<dialog>` cannot be one. So the drawer is `fixed`/`-translate-x-full` when
closed, `lg:static lg:translate-x-0` always.

That means writing three things `<dialog>` would have given free:

- Escape closes the drawer.
- Focus moves into the drawer when it opens, and returns to the hamburger when
  it closes.
- `<main>` gets the `inert` attribute while the drawer is open, so a phone
  user cannot Tab into content sitting underneath the overlay.

This trade-off gets a comment at the seam, because the next person to read
`Modal.tsx` will reasonably ask why the two disagree.

**4. Two components get extracted, and only two.** Both already exist as
copy-paste at a scale that makes naming them the smaller change:

- **`PageContainer`** — `flex flex-col gap-5 px-9 py-8`, repeated verbatim at
  **8 sites**: `SettingsPage.tsx:13`, `FinancesPage.tsx:109/132/144`,
  `TransactionsPage.tsx:393`, `BudgetPage.tsx:238`, `BillsPage.tsx:295`,
  `GoalsPage.tsx:129`. Becomes `px-4 py-6 sm:px-9 sm:py-8`.
  `OverviewPage.tsx:43` uses `p-10` (40px) instead and joins them, which moves
  that one page's desktop gutters by 4px horizontally and 8px vertically —
  called out here because it is the single place layout work moves desktop
  pixels, and the alternative is a second container component for one caller.
- **`FieldPair`** — `grid grid-cols-2 gap-4`, unconditional, at **13 sites
  across 8 files**: `TransactionModal.tsx:333/385/424`,
  `BillModal.tsx:341/379/418`, `AccountModal.tsx:260/301`, `GoalModal.tsx:336`,
  `GoalContributionsPanel.tsx:231`, `MarkPaidModal.tsx:102`,
  `InviteMemberModal.tsx:91`, `NewSpaceModal.tsx:98`. Becomes
  `grid-cols-1 sm:grid-cols-2`.

Nothing else is extracted. CLAUDE.md's rule that a port with one implementation
is the wrong shape cuts both ways: 8 and 13 callers mean these are already
duplicated, and a component with 13 callers is earned rather than speculative.

**5. Two breakpoints, both Tailwind defaults, each with one job.**

- **`sm` (640px)** — content reflow. Field pairs stack, page gutters shrink,
  multi-column card grids go one-up.
- **`lg` (1024px)** — the shell navigation switch, and nothing else.

Mobile-first throughout: the unprefixed classes describe a phone, and prefixes
add width. A third breakpoint needs its reason written beside it.

At `lg` the sidebar leaves **788px** of content — measured, at a 1024px
viewport. Whether that is enough for `lg:grid-cols-[1.7fr_1fr]` on the Budget
and Bills pages is arithmetic and not yet measurement: 788 less the 72px
gutters and a 16px gap splits into roughly 441px and 259px, and the seeded
household has no budget, so the grid does not render to be measured. Slice 4
populates one and checks it; if 259px is too narrow for the side column, that
grid's breakpoint moves up rather than the shell's moving down.

**6. The floor is 375px designed, 320px tolerated.** 375px is the width every
screen is checked and tuned at. At 320px the requirement is weaker but
absolute: no horizontal scrolling anywhere. Below 320px is not a device.

**7. Every full-height rule moves from `screen` to `dvh`.** `100vh` on iOS
Safari is the *large* viewport — the height with the URL bar hidden — so a box
sized to it is taller than what the user can actually see, and its bottom edge
sits under the browser toolbar. Four places size to viewport height today:
`Modal.tsx:116` (`h-screen w-screen`), `AppShell.tsx:21` (`min-h-screen`), the
auth screens' `min-h-screen` wrapper, and the drawer this design adds. Tailwind
v4 ships `h-dvh`/`min-h-dvh`; all four take them.

The drawer is the worst case and the reason this is a decision rather than a
detail: it is `fixed` and full-height, and **sign-out lives at its bottom
edge** — precisely the part iOS would clip.

**8. Touch targets reach 44px, in their own slice.** Today the sidebar's
sign-out button is `h-6 w-6` (24px), `Modal`'s close is `h-7 w-7` (28px), and
roughly a dozen controls sit at `py-1.5`. These are separated from the layout
work because they are the one part of this design that *does* change desktop
rendering, so they need their own before-and-after comparison rather than being
buried in a reflow diff.

## What is deliberately not changing

Recorded here because each was checked, and each is the kind of thing a later
reader "fixes" by mistake.

**`Modal.tsx`'s container is already correct.** It carries
`max-w-[calc(100vw-32px)]`, and the panel measures **343px** at a 375px
viewport. Only the field grids inside it break.

**Modals need no vertical *scrolling* work — on a browser where `100vh` is
honest.** Measured at a 450px-tall viewport in desktop Chrome: the dialog's
computed `overflow-y` is `auto`, `scrollHeight` (667) exceeds `clientHeight`
(450), and the panel's title is reachable at `scrollTop 0`. Tall modals already
scroll and do not strand their heading off-screen.

That claim does **not** extend to iOS Safari, and the measurement could not
reach it: every number in this document comes from headless Chrome resized
narrow, which cannot reproduce a URL bar. AccountModal's content measures 665px
— on an iPhone with roughly 650px actually visible, its "Add account" button
would sit under the toolbar. That is what decision 7 fixes, and it is a
`dvh` change, not a scrolling one.

**The ledger and bill rows keep their shape.** They are flex rows with a label
block and an amount, and they wrap. No card-collapse redesign, no per-row
mobile variant.

**Desktop at 1440px does not move**, with the single exception of
`OverviewPage`'s gutter in decision 4.

## Testing, and what cannot be tested

jsdom performs no layout. CSS breakpoints are invisible to the existing Vitest
suite, and any plan promising unit tests for them is promising something that
cannot exist. The split is therefore explicit.

**Testable in jsdom, and where the new mutation-checked tests go:**

- The drawer opens when the hamburger is pressed.
- The drawer closes on navigation, so a phone user does not land on a new page
  behind an open overlay.
- Escape closes the drawer.
- The hamburger has an accessible name.
- `<main>` carries `inert` while the drawer is open and loses it on close.
- Exactly one element with `data-testid="sidebar-space"` per space exists at
  any viewport (the decision-2 guard, as a test rather than a hope).

**Browser-only, walked and recorded in the plan:** 320, 375, 414, 768, 1024 and
1440px, across sign-in, sign-up, Overview, Finances, Transactions, Budget,
Goals, Bills, Settings, and one modal from each of the three modal families.

**Desktop-parity guard:** 1440px screenshots before and after each slice. Where
they differ, either the slice is wrong or the difference is one this document
named. There are exactly two named ones: `OverviewPage`'s gutter, and the touch
targets in slice 5.

## Slices

1. **Shell and drawer.** `AppShell`, new `MobileTopBar`, new `NavDrawer`,
   `Sidebar` untouched. Carries decision 7's four `screen`→`dvh` swaps too —
   they are one-word changes, they share this slice's desktop-parity
   screenshots, and splitting them across slices means the drawer ships with a
   clipped sign-out. Unblocks every signed-in page at once.
2. **Auth screens.** `w-[428px]` becomes `w-full max-w-[428px]` at 7 sites
   across 6 files: `SignInScreen:204`, `SignUpScreen:137`,
   `SignUpCompleteScreen:57` and `:321`, `InviteScreen:42`,
   `MagicLinkConsumeScreen:45`, `CheckYourEmailPanel:39`. No wrapper padding is
   needed — every one of these sits inside a `min-h-screen grid place-items-center
   … p-6` shell, so the card lands 24px from each edge and never goes
   edge-to-edge. Second, not last: Hearth is sold self-serve, and sign-up is
   the first screen a stranger sees.
3. **`FieldPair` across the 13 modal sites**, plus `PageContainer` across the
   8 identical page sites and `OverviewPage`, which is 9 pages in total.
   Mechanical, and the largest file count of any slice.
4. **Data-dense screens.** The transaction filter bar, the bill row's action
   cluster (date badge, name, "Paid", amount and Undo in one flex row), and the
   budget category grid. `BudgetHistoryModal:192`'s `grid-cols-3` is checked
   here and changed only if it fails at 343px.
5. **Touch targets** to a 44px floor.
6. **Documentation.** `SYSTEM_DESIGN.md` gains the shell's responsive layout
   and the two-breakpoint convention; `FEATURE_TRACKER.md` gains a row for
   mobile responsiveness **and its summary table is recounted**, since
   CLAUDE.md requires the columns to sum to the stated totals rather than be
   guessed at; `LEARNING.md` gains what this taught — including that
   a fixed-width shell made every downstream page look broken, and that the
   fastest way to size the work was hiding the sidebar in the live DOM and
   re-measuring.

## Risks

**A mobile-first rewrite silently changes desktop.** This is the real risk of
the approach and the reason the parity screenshots are a gate rather than a
courtesy.

**Duplicate testids break the suite** — decision 2, guarded by a test.

**The `⌘K` chip** in the sidebar's brand row opens nothing; no command palette
exists. It stays out of the mobile top bar rather than consuming width a
hamburger and a product name need. It remains visible in the drawer, where the
existing sidebar renders unchanged.

**No mobile design exists to build against.** `design/Household Dashboard.dc.html`
is a 1440px canvas with zero `@media` queries. Every mobile layout in this
document is invention, authorised on 2026-08-15 under the constraint at the top
of this file. The inventions are: the drawer and its top bar, the `sm` stacking
points, and the 44px touch-target floor.
