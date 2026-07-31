# Hearth — UX repair and an interim Overview

Written 2026-07-31, from a browser walk of the running app taken as a UI/UX
critique rather than a feature verification: sign out, sign up fresh
(`sam@newhouse.test` → "Rivera household", USD), then every navigation
destination in turn. The raw findings are reproduced in §1 so this document
stands on its own.

The walk covered the cold path end to end. It did not cover the populated
household — browser tooling dropped before that pass — so every conclusion
below rests on the fresh-signup walk plus one screenshot of the seeded
household's landing page taken before signing out. That one screenshot is
load-bearing: it is what proves the front-door problem is not a first-run
artefact.

---

## 1 · What the walk found

### 1.1 There is no front door

`routes/router.tsx` maps `/` to `<PlaceholderPage page="Overview" slice={5} />`.
The entire content of the page is a heading and the sentence "Arriving in
slice 5."

A brand-new household lands there immediately after sign-up. So does the
pre-existing seeded household (`andreas@hearth.family`) — the first screenshot
of the walk, taken before signing out, is the identical page. This is not a
cold-start artefact: an established household with accounts, transactions and
a budget opens the app on the same dead page every day.

Sign-up itself promises "You can invite your partner right after — nothing is
shared until they accept." The page it delivers you to mentions no such thing.

### 1.2 Half the navigation is scaffolding, in the team's own vocabulary

Six top-level destinations. Three of them — Overview, Marriage, Family — render
"Arriving in slice N." *Slice* is a word from `docs/superpowers/plans/`. It has
no meaning to anyone who did not write the plan.

`Sidebar.tsx` already contains the argument against this, applied one level
down:

> SPACE_PAGES grows a row per shipped page — Goals and Bills join it once their
> pages exist, not before, because a permanent grey "soon" row reads as broken.

The same reasoning was never applied to whole spaces.

### 1.3 The layout is not contained

`AppShell.tsx` renders `grid grid-cols-[236px_1fr]` with a bare
`<main className="overflow-y-auto">`. Nothing sets a maximum width. Measured in
the browser at the viewport the product owner actually runs (2752 CSS px):

| element | x |
|---|---|
| "All transactions" heading | ~170 |
| "+ Add transaction" button | 2577 |
| "Budget over-spend alerts" label (Settings) | ~1500 |
| that label's toggle | 2653 |

`main` measured 2516px wide. The only `max-w-*` in the money and settings
features are on individual empty-state paragraphs (`max-w-sm`,
`max-w-[420px]`), never on a page container.

A label and the control it names sitting 1200px apart is not a small polish
issue; it is why the screen does not read as a screen.

### 1.4 The Transactions dead end

On a household with no accounts, the centre of the ledger says:

> Nothing logged yet.
> Add an expense, some income or a transfer, and it will show up here.

The "+ Add transaction" button is `disabled` (`accounts.length === 0`). Its
explanation — "Add an account first, and transactions can attach to it." —
renders at x=2496, roughly 2400px to the right of where the reader is looking.
The centre empty state offers no route to Finances.

Disabling the button was a deliberate, correct decision; `TransactionsPage.tsx`
says so in a comment ("not a modal whose account dropdown is empty — that is a
dead end reached after four clicks"). The placement is what fails.

### 1.5 The copy describes a different product from the domain

| where | says |
|---|---|
| `SignUpScreen.tsx:142`, `SignUpCompleteScreen.tsx:151` | "One household, two owners. Set it up once and invite your partner in." |
| `SignInScreen.tsx:314`, `SignUpScreen.tsx:195`, `SignUpCompleteScreen.tsx:310` | "Your household data stays between the two of you." |
| `settings/copy.ts:13` | role `owner` renders as "Parent" |
| `InviteMemberModal.tsx:188` | "Parents only" |
| `SettingsPage` Spaces panel | "+ New space (Kids, Home, Travel…)" |

`domain.Role` is `owner` | `limited`. There is no two-member cap anywhere —
the seeded household has four. `domain.VisibilityParentsOnly` exists. The
front door is the only place claiming this is a two-person product.

### 1.6 Smaller

- Sign-up step 1's submit button reads "Create household" but sends a
  verification email; the household is created by step 2's button of the same
  name.
- The household-name field's helper reads "Shown at the top of the sidebar." It
  renders at the *bottom*, beside the avatar and "Free plan" — which is where
  design 5a puts it. The text is wrong, not the layout.
- The currency select offers ~140 options with no default and no grouping, on
  the second screen of sign-up.

### 1.7 What already works, and is the model to copy

Budget's empty state — "A budget gives every dollar a job. Set a monthly cap
per category and Hearth will track spending against it automatically." — plus
two starter templates. Finances' empty state — "Add what your household owns
and owes, and Hearth will keep the total." — plus one button.

Both name the concept and offer one obvious action. The problem is not that
nobody here knows how to write an empty state. It is that Overview and the two
placeholder spaces never got one.

---

## 2 · Decisions taken before designing

Four questions were settled with the product owner on 2026-07-31.

**The front door is an interim Overview built only on Money.** The designed
Overview (`FEATURE_TRACKER.md` §4) is eight cards aggregating Money, Marriage
and Family, and the tracker's own suggested order puts it last for that
reason. Two of the eight are buildable today. Rather than redirect `/` to
Finances and leave the app with no home, `/` becomes a real page carrying those
two cards plus a setup checklist — the same route, the same component, grown
rather than replaced when the remaining areas land.

**Unbuilt spaces leave the navigation.** Marriage and Family are removed from
the sidebar and their routes deleted, on exactly the rule `Sidebar.tsx` already
states for sub-pages. They return when their pages exist.

**Hearth is a family product: parents plus kids.** The domain already says so
(`Role`, `VisibilityParentsOnly`, the Kids space suggestion). The six sign-up
and sign-in strings are what get rewritten, not the domain.

**Two milestones, one spec.** M1 is repair with no new screen and ships on its
own. M2 builds the interim Overview on top of an already-fixed shell. Two
branches, two reviews, two browser walks. The alternative — one branch — puts a
cross-cutting layout change and a new feature in a single diff, which is the
shape that hid five defects (one Critical) in the last whole-branch review.

---

## 3 · M1 — UX repair

No new screen. No new endpoint. No migration.

### 3.1 Content container

`web/src/features/shell/AppShell.tsx`:

```tsx
<main className="overflow-y-auto">
  <div className="mx-auto w-full max-w-[1204px]">
    <Outlet />
  </div>
</main>
```

**Why 1204.** The design's canvas is 1440px wide with a 236px sidebar
(`design/Household Dashboard.dc.html`, screen 5a: `width:1440px;
grid-template-columns:236px 1fr`). 1440 − 236 = 1204. The number reproduces the
design rather than inventing a taste.

Pages keep their own padding; nothing else changes. Every existing page is
checked for a full-bleed assumption — Finances, Transactions, Budget, Settings.

**Verification.** No unit test. A test asserting a Tailwind class pins the
implementation rather than the behaviour, and would stay green against a layout
that had broken in some other way. This is verified in the browser: screenshots
of Transactions and Settings at 2752px before and after, plus a DOM measurement
showing the heading and its primary action within one container width.

### 3.2 Unbuilt spaces leave the navigation

Frontend only. `isBuiltin` is already on the wire
(`auth_handlers.go:74`, `schemas.ts`), and `SpacesPanel` fetches `/spaces`
under its own `['spaces']` query key, so Settings keeps listing all three
spaces regardless of what the sidebar renders. Its rows are plain text, not
links, so nothing there leads to a deleted route.

`web/src/features/shell/Sidebar.tsx`:

- `SPACE_PAGES` loses its `marriage` and `family` entries.
- `SpaceLink` gains one rule: a **builtin** space with no `SPACE_PAGES` entry
  renders nothing. A **non-builtin** space with no entry keeps rendering as
  muted text — that is a household's own space from "+ New space", and hiding
  it would make the feature look broken.

`web/src/routes/router.tsx` deletes `marriageGuardRoute`, `marriageIndexRoute`,
`marriageSplatRoute` and `familyCalendarRoute`. `/marriage` and
`/family/calendar` fall through to the root route's existing
`notFoundComponent`. `PlaceholderPage` survives M1 as `/`'s component only, and
is deleted in M2.

`router.test.tsx` loses the two tests that redirect a member without the
`marriage` capability, because the routes they guard no longer exist.

**New test, mutation-checked.** In `Sidebar.test.tsx`: given `me.spaces`
containing a builtin space whose key has no `SPACE_PAGES` entry and a
non-builtin space likewise, the builtin one renders no nav row and the custom
one still renders its muted label. Mutation: make the rule unconditional (hide
both) and watch the custom-space assertion go red.

### 3.3 The Transactions dead end

`web/src/features/money/TransactionsPage.tsx`, the `rows.length === 0` /
`!filtersActive` branch. When `accounts.length === 0` it renders an
account-first state instead of the generic one:

- title — "Add an account first"
- body — "Transactions attach to an account, so Hearth needs one before you can
  log anything."
- a `<Link to="/money">` styled as the primary action — "Add an account"

New keys in the feature's `copy.ts` alongside `emptyTitle`/`emptyBody`; the
existing pair stays for the has-accounts-but-no-transactions case.

The header's disabled button and its `noAccountsYet` hint stay as they are.
Inside a 1204px container they sit about 1000px from the heading rather than
2400, which is a normal header-right position.

**New test, mutation-checked.** With zero accounts the link to `/money` is in
the document; with one account it is not, and the original empty copy is.
Mutation: drop the `accounts.length === 0` condition so the account-first state
always renders, and watch the has-accounts assertion go red.

### 3.4 Audience copy

Three files, six strings, one test expectation
(`SignUpScreen.test.tsx:30`).

| old | new |
|---|---|
| One household, two owners. Set it up once and invite your partner in. | One household for the whole family. Set it up once, invite your partner, add the kids later. |
| Your household data stays between the two of you. | Your household data stays inside your household. |

"You can invite your partner right after — nothing is shared until they
accept." is unchanged: it is still true and still the right thing to promise at
that moment.

Settings keeps "Parent" and "Parents only" — after this change they agree with
the front door instead of contradicting it.

### 3.5 Three smaller fixes

**a. Sign-up step 1's button.** `SignUpScreen.tsx` — "Create household" becomes
"Send me a set-up link". Step 2's button in `SignUpCompleteScreen.tsx` keeps
"Create household"; that one does create a household.

**b. The household-name helper.** "Shown at the top of the sidebar. Change it
any time." becomes "Shown at the bottom of the sidebar, beside your name.
Change it any time." Design 5a puts the household name in the sidebar footer
next to the avatar and plan, and `Sidebar.tsx` renders it there.

**c. Currency grouping.** `GET /api/v1/currencies` already returns a `symbol`
per currency (`currency_handlers.go:40`), and the sign-up screens already
render "AUD (A$)" when one is present. The split is driven by that wire field —
`symbol !== ""` — not by a second list hardcoded in the frontend, so adding a
symbol in `currencySymbols` promotes that currency with no frontend change.
Those go under `<optgroup label="Common">`, the rest under `<optgroup
label="All currencies">`, both in the order the server sent.

Today that is 17 currencies: `currencySymbols` holds 18 entries but VND is not
in `domain.SelectableCurrencies` (only two-minor-unit currencies are
selectable), so its symbol is never reached. No backend change, no default
guessed from locale, no currency removed from the list.

### 3.6 M1 definition of done

`make lint && make test` green. At least one new test mutation-checked (§3.2
and §3.3 each specify theirs). A browser walk on **both** a fresh household and
the seeded one — the fresh path is what found these, the seeded path is what
this spec never covered. `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md` and
`docs/SYSTEM_DESIGN.md` updated in the same change; SYSTEM_DESIGN because the
route tree loses four routes.

M1 ships no feature, so no tracker row changes symbol. Marriage and Family stay
`⬜` — removing a placeholder from the navigation does not make them less
unbuilt, and the tracker records what exists, not what is reachable. What
Marriage's and Family's sections gain is a note that their routes were removed
along with the placeholders, so the next person to open that area knows to add
the route back rather than wondering why the sidebar ignores their new page.
`LEARNING.md` gains the entries this walk earned: a placeholder in production
navigation, a primary action separated from its heading by an uncontained
layout, and a disabled control whose reason renders where nobody is looking.

---

## 4 · M2 — Interim Overview

Built on the shell M1 leaves behind.

### 4.1 Route and files

`/` stops rendering `PlaceholderPage`, which is then deleted — Overview was its
last remaining user.

New feature directory `web/src/features/overview/`, one job per file:

| file | job |
|---|---|
| `OverviewPage.tsx` | layout and data fetching |
| `NetWorthCard.tsx` | the net-worth figure and account count |
| `BudgetCard.tsx` | this month's budget percentage used |
| `SetupChecklist.tsx` | the four setup steps |
| `QuickAddMenu.tsx` | the "+ Add" menu |
| `copy.ts` | every user-visible string |

### 4.2 No new endpoints

Everything the page needs already exists: `GET /api/v1/accounts`,
`GET /api/v1/budgets/{month}`, `GET /api/v1/household/members`. This is
composition, not new capability.

### 4.3 The two cards

**Net worth** — the same computation Finances already performs, extracted so
both callers share it rather than each summing accounts their own way.

**This month's budget** — percentage used, plus the two money figures behind it,
from `/budgets/{month}` for the current month.

Both are `⬜ → 🟡` in `FEATURE_TRACKER.md` §4, not `✅`. The designed cards
carry detail this version will not, and a `🟡` names the gap where a `✅` would
hide it.

The remaining six designed cards — next bill, goals on track, next retro,
vision check-in, "this week" agenda — stay `⬜`. They read from features that do
not exist.

### 4.4 The setup checklist

Not in the design; it gets its own tracker row. Four steps, every one derived
from data the page already fetched:

| step | complete when | links to |
|---|---|---|
| Create your household | always | — |
| Add an account | `accounts.length > 0` | `/money` |
| Invite your partner | a second member exists, or an invite is pending | Settings → Invite |
| Set a budget for July | a budget exists for the current month | `/money/budget` |

The whole block disappears at four of four, so an established household does
not carry a permanent chore list. The month name in the fourth step comes from
the current month, not a literal.

### 4.5 "+ Add" quick-create

The design's menu offers Transaction, Account, Bill, Savings goal, Calendar
event and Marriage retro. Two of those exist. The menu offers those two and
nothing else, on the rule `Sidebar.tsx` already states — a permanent greyed
row reads as broken, not as a roadmap. `🟡` with the four missing entries
named.

### 4.6 A limited member without Money

Overview is the only page in the app every member reaches; Money's pages are
behind `RequireCapability`. A `limited` member without `CapMoney` gets 403 from
both `/accounts` and `/budgets/{month}`.

- Both cards render a plain "You don't have access to Money" state. Not an
  error, not a spinner, not a crash.
- The checklist renders only for an owner. A limited member can neither invite
  a member nor write a budget, so offering the steps would be offering work
  they cannot do.

This is the case most likely to be missed, because the seeded owner account
never hits it. It is named here so the plan carries a task for it and the walk
carries a criterion.

### 4.7 M2 definition of done

`make lint && make test` green, at least one new test mutation-checked, and a
browser walk covering three states: a fresh household (checklist at 1 of 4), an
established household (no checklist, both cards populated), and a limited
member without Money (no-access cards, no checklist). Tracker, LEARNING and
SYSTEM_DESIGN updated — SYSTEM_DESIGN because `/` gains a real component and a
data flow.

---

## 5 · Out of scope

- The designed Overview's other six cards. They need Bills, Goals, Marriage and
  Family.
- Anything responsive or mobile. The container in §3.1 is a maximum width, not
  a breakpoint system. A kitchen-tablet layout is its own piece of work.
- A command palette. The `⌘K` chip in the sidebar remains decorative, as its
  comment already says.
- Billing. "Free plan" in the sidebar footer stays static.
