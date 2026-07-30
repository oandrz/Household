# Hearth UX Repair (M1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Repair the four defects a browser walk found in the running app — an uncontained layout, placeholder destinations in the navigation, a dead end on Transactions, and sign-up copy that describes a different product from the domain — without building any new screen.

**Architecture:** Frontend only. No new endpoint, no migration, no domain change. One container element in `AppShell`, one rule in `Sidebar`, four routes deleted, one branch added to the Transactions empty state, and nine copy strings. Milestone 2 (`2026-07-31-hearth-interim-overview.md`) builds the Overview page on top of what this leaves behind.

**Tech Stack:** React 19, TypeScript, TanStack Router 1.170, TanStack Query 5.101, Tailwind, Vitest + @testing-library/react.

**Spec:** `docs/superpowers/specs/2026-07-31-hearth-ux-repair-design.md`

## Global Constraints

- Content container width is **1204px** — the design canvas is 1440px with a 236px sidebar (`design/Household Dashboard.dc.html`, screen 5a: `width:1440px; grid-template-columns:236px 1fr`). Do not round it to a Tailwind preset.
- **Comments say why, never what.** `usecase/ports.go` is the model. Every non-obvious decision gets written down at the point someone would try to change it.
- **Every new test is mutation-checked**: break the code on purpose, watch the test go red, restore. A green test proves nothing until you have seen it fail (`proving-tests-can-fail` skill).
- **Copy lives where that feature already keeps it.** `features/money/transactionCopy.ts` for the ledger. The auth screens hold their strings inline in JSX today — follow that, do not introduce a copy module for them in this milestone.
- Test runner is `vitest`, driven with `fireEvent` — `@testing-library/user-event` is not a dependency of this project.
- Tests read as documentation: the name states the behaviour, the body shows it.
- Definition of done for the milestone: `make lint && make test` green, at least one new test mutation-checked, `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md` and `docs/SYSTEM_DESIGN.md` updated, and a browser walk recorded. The full checklist is at the end of `docs/LEARNING.md`.

## File Structure

| file | change | responsibility after the change |
|---|---|---|
| `web/src/features/shell/AppShell.tsx` | modify | authenticated layout: sidebar, and an outlet bounded to the design's content width |
| `web/src/features/shell/Sidebar.tsx` | modify | navigation rendered from `me.spaces`, skipping builtin spaces whose pages are not built |
| `web/src/features/shell/Sidebar.test.tsx` | modify | adds the two cases pinning that rule |
| `web/src/routes/router.tsx` | modify | route tree, minus the four placeholder routes |
| `web/src/routes/router.test.tsx` | modify | loses the two tests guarding routes that no longer exist |
| `web/src/features/money/transactionCopy.ts` | modify | ledger copy, plus the account-first empty state's three strings |
| `web/src/features/money/TransactionsPage.tsx` | modify | ledger screen; its empty state now branches on whether an account exists |
| `web/src/features/money/TransactionsPage.test.tsx` | modify | adds the account-first case; one existing assertion is narrowed |
| `web/src/features/auth/SignUpScreen.tsx` | modify | sign-up step 1 |
| `web/src/features/auth/SignUpScreen.test.tsx` | modify | its copy expectation follows the string |
| `web/src/features/auth/SignUpCompleteScreen.tsx` | modify | sign-up step 2: helper text, currency grouping, audience copy |
| `web/src/features/auth/SignInScreen.tsx` | modify | one footnote string |

`web/src/features/placeholder/PlaceholderPage.tsx` survives this milestone as `/`'s component only. Milestone 2 deletes it.

---

## Task 1: Bound the content width

The single change that recovers the most screen. Nothing else in this milestone depends on it, but doing it first means every later task is verified inside the layout it will ship in.

**Files:**
- Modify: `web/src/features/shell/AppShell.tsx:21-25`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: nothing other tasks import. Later tasks assume page content is bounded to 1204px when they check anything visually.

**Why there is no unit test here.** A test asserting `max-w-[1204px]` is present pins the implementation, not the behaviour — it would stay green against a layout broken some other way, and it would go red on a rename that changed nothing. The honest check is a measurement in a real browser, which is Step 3. This is the `verifying-in-the-real-environment` skill's case exactly.

- [ ] **Step 1: Measure the defect before changing anything**

Start the app if it is not running (`make dev`), sign in, and open `http://localhost:5173/money/transactions` in a real browser. In the console:

```js
const h = [...document.querySelectorAll('h1')].find(e => /All transactions/.test(e.innerText))
const b = [...document.querySelectorAll('button')].find(e => /Add transaction/.test(e.innerText))
JSON.stringify({ heading: Math.round(h.getBoundingClientRect().x), button: Math.round(b.getBoundingClientRect().x), main: Math.round(document.querySelector('main').getBoundingClientRect().width) })
```

Write the three numbers down. On a 2752px-wide viewport the walk recorded `{heading: 170, button: 2577, main: 2516}`. Yours will differ with your monitor; what matters is that `button - heading` is far larger than 1204.

- [ ] **Step 2: Add the container**

`web/src/features/shell/AppShell.tsx`, replacing the `<main>` element in the returned tree:

```tsx
  return (
    <div className="grid min-h-screen grid-cols-[236px_1fr] bg-surface">
      <Sidebar me={me.data} />
      <main className="overflow-y-auto">
        {/* 1204px is the design's own content width, not a taste: its canvas
            is 1440px wide with a 236px sidebar (design/Household
            Dashboard.dc.html, screen 5a). Without this, `1fr` gives every
            page the whole monitor -- on a 2752px display the ledger's heading
            and its "+ Add transaction" button ended up 2400px apart, and a
            Settings toggle sat 1200px from the label naming it. Pages keep
            their own padding; this element only bounds them. */}
        <div className="mx-auto w-full max-w-[1204px]">
          <Outlet />
        </div>
      </main>
    </div>
  );
```

- [ ] **Step 3: Measure again, in the browser**

Reload `/money/transactions` and run the same console snippet. Expected: `main` is unchanged (it still fills the grid column) but `button - heading` is now under 1204. Repeat on `/money`, `/money/budget` and `/settings`, checking each renders inside one readable column with no element stranded at the right edge.

On `/settings` specifically, check the Notifications card — before this change its right-hand column's toggles measured at x=2653 against labels at x≈1500:

```js
JSON.stringify([...document.querySelectorAll('button[role="switch"]')].map(e => ({ label: e.getAttribute('aria-label'), x: Math.round(e.getBoundingClientRect().x) })))
```

Expected: every toggle's x is inside the container, and each sits beside its own label.

- [ ] **Step 4: Check no page assumed full bleed**

Read the top-level wrapper of each page component and confirm none of them sets its own centring or width that now double-applies:

```bash
head -40 web/src/features/money/FinancesPage.tsx web/src/features/money/TransactionsPage.tsx web/src/features/money/BudgetPage.tsx web/src/features/settings/SettingsPage.tsx | grep -n "mx-auto\|max-w-\[\|w-screen"
```

Any `mx-auto` on a page's own root is now redundant — remove it. A `max-w-sm` on an empty-state paragraph is not; leave those alone.

- [ ] **Step 5: Run the suite**

Run: `make test-web`
Expected: PASS. No test asserts on layout, so this task should not move any of them. If something fails, it found a real coupling — fix it here rather than in a later task.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/shell/AppShell.tsx
git commit -m "fix(shell): bound page content to the design's 1204px column

Nothing in the authenticated shell set a maximum width, so `1fr` handed
every page the whole monitor. On a 2752px display the ledger's heading and
its primary action measured 2400px apart, and a Settings toggle sat 1200px
from the label naming it -- a label and its control that far apart stop
reading as one control."
```

---

## Task 2: Unbuilt spaces leave the navigation

Marriage and Family are nav rows whose only content is "Arriving in slice N". `Sidebar.tsx` already states the rule this task applies: *"a permanent grey 'soon' row reads as broken."*

**Files:**
- Modify: `web/src/features/shell/Sidebar.tsx:29-38` (`SPACE_PAGES`) and `:52-58` (`SpaceLink`'s no-entry branch)
- Modify: `web/src/features/shell/Sidebar.test.tsx`
- Modify: `web/src/routes/router.tsx`
- Modify: `web/src/routes/router.test.tsx`

**Interfaces:**
- Consumes: `Space` from `features/auth/schemas` — `{ id, key, name, visibility, position, isBuiltin, requiredCapability? }`. `isBuiltin` is already on the wire (`api/internal/adapter/http/auth_handlers.go:74`); no backend change is needed to read it.
- Produces: `SPACE_PAGES` keeps its shape `Record<string, { label: string; to: string }[]>` and keeps only its `money` entry. Milestone 2 does not touch it.

**What must not break.** `SettingsPage`'s Spaces panel reads `GET /api/v1/spaces` under its own `['spaces']` query key (`features/settings/SpacesPanel.tsx:20-25`) and renders plain text rows, not links. Marriage and Family keep appearing there. That is deliberate: the household still has those spaces, they just have nowhere to go yet.

- [ ] **Step 1: Write the failing tests**

Add to `web/src/features/shell/Sidebar.test.tsx`. The file already has a `space()` factory and `meFixture()`; add these fixtures next to the existing `familySpace`:

```tsx
const moneySpace = space({
  id: "space-money",
  key: "money",
  name: "Money",
  position: 1,
  requiredCapability: "money",
});

const marriageSpace = space({
  id: "space-marriage",
  key: "marriage",
  name: "Marriage",
  visibility: "parents_only",
  position: 2,
  requiredCapability: "marriage",
});

// A space a household made for itself with "+ New space". It has no
// SPACE_PAGES entry either, which is exactly why it is here: the rule below
// must key off isBuiltin, not off the absence of an entry.
const travelSpace = space({
  id: "space-travel",
  key: "travel",
  name: "Travel",
  position: 4,
  isBuiltin: false,
});
```

and these two tests inside the existing `describe("Sidebar", ...)`:

```tsx
  it("renders no row for a builtin space whose pages are not built yet", async () => {
    renderWithRouter(
      <Sidebar me={meFixture([moneySpace, marriageSpace, familySpace])} />,
    );

    // Money's own group renders, so the sidebar did mount -- without this the
    // assertions below would pass on an empty render.
    expect(await screen.findByText("Finances")).toBeInTheDocument();
    expect(screen.queryByText("Marriage")).toBeNull();
    expect(screen.queryByText("Family")).toBeNull();
  });

  it("still renders a space the household created, which has no built pages either", async () => {
    renderWithRouter(<Sidebar me={meFixture([moneySpace, travelSpace])} />);

    expect(await screen.findByText("Travel")).toBeInTheDocument();
  });
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/features/shell/Sidebar.test.tsx -t "builtin space whose pages"`
Expected: FAIL — "Marriage" is still in the document, because `SPACE_PAGES` still has an entry for it.

Run: `cd web && npx vitest run src/features/shell/Sidebar.test.tsx -t "space the household created"`
Expected: PASS already — the muted-text branch handles it today. That is fine; this test exists to stay green through the change, and Step 6 proves it can fail.

- [ ] **Step 3: Shrink SPACE_PAGES and add the rule**

`web/src/features/shell/Sidebar.tsx`. Replace the `SPACE_PAGES` literal with:

```tsx
const SPACE_PAGES: Record<string, { label: string; to: string }[]> = {
  money: [
    { label: "Finances", to: "/money" },
    { label: "Transactions", to: "/money/transactions" },
    { label: "Budget", to: "/money/budget" },
  ],
};
```

and update the comment above it, which still promises Marriage and Family rows:

```tsx
// One entry per built page of each space, in the design's order. A space
// with one entry renders as a single link named after the space; a space
// with several renders as the design's uppercase group label plus a link
// per page (the 5a sidebar).
//
// A *builtin* space missing from this map has no built pages at all and
// renders nothing -- the same rule this map already applies one level down
// ("Goals and Bills join it once their pages exist, not before, because a
// permanent grey 'soon' row reads as broken"). Marriage and Family were both
// rows whose only content was the sentence "Arriving in slice N"; they come
// back here when their pages do. A *custom* space -- one a household made
// with "+ New space" -- is missing from this map permanently and by design,
// so it still renders, as plain text.
```

Then the no-entry branch of `SpaceLink`:

```tsx
function SpaceLink({ space }: { space: Space }) {
  const matchRoute = useMatchRoute();
  const pages = SPACE_PAGES[space.key];
  if (!pages) {
    // Builtin and unbuilt: no row. Custom: a row the household named, with
    // no route yet -- see SPACE_PAGES's comment for why the two differ.
    if (space.isBuiltin) return null;
    return (
      <span data-testid="sidebar-space" className={`${NAV_ITEM_CLASS} text-muted`}>
        {space.name}
      </span>
    );
  }
```

The rest of the function is unchanged.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/shell/Sidebar.test.tsx`
Expected: PASS, all cases including the file's existing four.

If the existing test "renders only the spaces present in me.spaces" fails, read it first: it renders `meFixture([familySpace])` and asserts Family appears. Family is now hidden, so that test's fixture must change to a space that still renders — use `moneySpace` and assert "Finances" appears while a space absent from the payload does not. Keep the test's original point (the sidebar renders from the payload, not a hard-coded list); only its fixture changes.

- [ ] **Step 5: Delete the four placeholder routes**

`web/src/routes/router.tsx`:

- Delete the `marriageGuardRoute`, `marriageIndexRoute`, `marriageSplatRoute` and `familyCalendarRoute` declarations.
- Remove them from `routeTree`'s `addChildren` list, leaving `indexRoute`, the `moneyGuardRoute` subtree and `settingsRoute`.
- Update the file's header comment, which lists the route shape — delete the `/marriage`, `/marriage/$` and `/family/calendar` lines and add one sentence saying why:

```tsx
//       /            Overview      (slice 5 placeholder)
//       /money       RequireCapability("money") -> Finances
//       /money/transactions RequireCapability("money") -> Transactions
//       /money/budget RequireCapability("money") -> Budget
//       /money/$     RequireCapability("money") -> Money    (slice 2 placeholder; Goals/Bills remain)
//       /settings                                   -- the real Settings screen (Task 20)
//
// Marriage and Family have no routes: both were placeholders reading
// "Arriving in slice N", and the sidebar no longer offers them (see
// Sidebar.tsx's SPACE_PAGES). Their URLs fall through to rootRoute's
// notFoundComponent. Add the route back in the same change that builds the
// page, alongside its SPACE_PAGES entry -- and re-add RequireCapability for
// Marriage, which is capability-gated.
```

- `RequireCapability` stays imported and used — Money still needs it.

- [ ] **Step 6: Delete the tests that guarded the deleted routes, then mutation-check**

In `web/src/routes/router.test.tsx`, delete the two tests naming `/marriage` (around line 134) — a test for a route that no longer exists cannot fail meaningfully. If a test asserts `/family/calendar`, delete it too.

Now prove the new rule's second test can fail. Temporarily change the branch to hide every space with no entry:

```tsx
  if (!pages) {
    return null;   // MUTATION -- do not keep
  }
```

Run: `cd web && npx vitest run src/features/shell/Sidebar.test.tsx`
Expected: FAIL on "still renders a space the household created". Restore the `space.isBuiltin` check and confirm it passes again.

- [ ] **Step 7: Run the whole frontend suite and the type-check**

Run: `make test-web && make typecheck`
Expected: PASS. The type-check matters here — deleting routes narrows TanStack Router's registered path union, so any surviving `to="/marriage"` becomes a type error rather than a silent dead link. If one appears, it is a real broken link; remove it.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/shell/Sidebar.tsx web/src/features/shell/Sidebar.test.tsx web/src/routes/router.tsx web/src/routes/router.test.tsx
git commit -m "fix(nav): drop Marriage and Family until their pages exist

Both were navigation rows whose only content was the sentence 'Arriving in
slice N' -- a word from the plan documents, shown to users, on two of six
destinations. Sidebar.tsx already applied the rule one level down: a
permanent grey 'soon' row reads as broken. This applies it to whole spaces,
and deletes the four routes behind them.

Settings still lists all three spaces: SpacesPanel reads /spaces under its
own query key and renders text, not links. A custom space from '+ New
space' still renders in the sidebar too -- the rule keys off isBuiltin, not
off the absence of a pages entry."
```

---

## Task 3: The Transactions dead end

On a household with no accounts, the centre of the ledger invites you to add an expense, the button is disabled, and the reason renders at the far right of the header where nobody is looking. The disabling is correct and `TransactionsPage.tsx` says why in a comment; only the placement fails.

**Files:**
- Modify: `web/src/features/money/transactionCopy.ts`
- Modify: `web/src/features/money/TransactionsPage.tsx:443-462` (the `rows.length === 0` branch)
- Modify: `web/src/features/money/TransactionsPage.test.tsx` (add one test; narrow one existing assertion)

**Interfaces:**
- Consumes: `TRANSACTIONS_COPY` from `./transactionCopy`, `accounts: Account[]` already in scope in `TransactionsPage`, and `Link` from `@tanstack/react-router` — already imported by this file for its `‹ Finances` breadcrumb.
- Produces: three new `TRANSACTIONS_COPY` keys — `noAccountsTitle`, `noAccountsBody`, `noAccountsAction`. Nothing else consumes them.

**A collision you must handle.** The existing test at `TransactionsPage.test.tsx:322` asserts `screen.getByText(/add an account first/i)` against the header hint `noAccountsYet` ("Add an account first, and transactions can attach to it."). The new empty-state title is "Add an account first" — the same regex then matches two elements and `getByText` throws. Step 5 narrows that assertion.

- [ ] **Step 1: Add the copy**

`web/src/features/money/transactionCopy.ts`, beside the existing `emptyTitle`/`emptyBody`:

```ts
  // The first-run state above assumes there is somewhere to log *to*. With no
  // accounts there is not, and the header's "+ Add transaction" is disabled --
  // so the middle of the screen has to carry the way out, not just the header's
  // hint, which sits at the far right of a wide row.
  noAccountsTitle: "Add an account first",
  noAccountsBody:
    "Transactions attach to an account, so Hearth needs one before you can log anything.",
  noAccountsAction: "Add an account",
```

- [ ] **Step 2: Write the failing test**

Add to `web/src/features/money/TransactionsPage.test.tsx`, next to the existing "disables Add transaction" case:

```tsx
  it("sends a household with no accounts to Finances from the empty state", async () => {
    renderPage({
      transactions: [],
      summary: { count: 0, spentMinor: 0 },
      accounts: [],
    });

    const link = await screen.findByRole("link", { name: /add an account/i });
    expect(link).toHaveAttribute("href", "/money");
    // The generic first-run copy would be a lie here: there is nothing to add
    // an expense *to* yet.
    expect(screen.queryByText(/nothing logged yet/i)).toBeNull();
  });

  it("keeps the generic first-run copy once an account exists", async () => {
    renderPage({ transactions: [], summary: { count: 0, spentMinor: 0 } });

    expect(await screen.findByText(/nothing logged yet/i)).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /add an account/i })).toBeNull();
  });
```

`renderPage`'s default fixture already includes one account, so the second test needs no `accounts` override.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/features/money/TransactionsPage.test.tsx -t "no accounts to Finances"`
Expected: FAIL — no link with that name; the page renders `emptyTitle` instead.

- [ ] **Step 4: Branch the empty state**

`web/src/features/money/TransactionsPage.tsx`, in the `rows.length === 0` / `!filtersActive` branch, replace the single empty panel with:

```tsx
          ) : accounts.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-14 text-center">
              <p className="text-sm font-semibold text-ink">
                {TRANSACTIONS_COPY.noAccountsTitle}
              </p>
              <p className="max-w-sm text-[13px] text-muted">
                {TRANSACTIONS_COPY.noAccountsBody}
              </p>
              {/* The way out lives here, in the middle of the screen, not only
                  in the header's hint beside the disabled button. */}
              <Link
                to="/money"
                className="rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white"
              >
                {TRANSACTIONS_COPY.noAccountsAction}
              </Link>
            </div>
          ) : (
            <div className="flex flex-col items-center gap-3 py-14 text-center">
              <p className="text-sm font-semibold text-ink">{TRANSACTIONS_COPY.emptyTitle}</p>
              <p className="max-w-sm text-[13px] text-muted">{TRANSACTIONS_COPY.emptyBody}</p>
            </div>
          )
```

Leave the header's disabled button and its `noAccountsYet` hint exactly as they are. Inside Task 1's container they now sit about 1000px from the heading, which is an ordinary header-right position.

- [ ] **Step 5: Narrow the existing assertion**

In the existing test "disables Add transaction when the household has no accounts", `getByText(/add an account first/i)` now matches two elements. Replace that line so it asserts the header hint specifically:

```tsx
    expect(
      screen.getByText(/add an account first, and transactions can attach to it/i),
    ).toBeInTheDocument();
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/money/TransactionsPage.test.tsx`
Expected: PASS, every case.

- [ ] **Step 7: Mutation-check**

Delete the `accounts.length === 0` condition so the account-first panel always renders:

```tsx
          ) : true ? (      // MUTATION -- do not keep
```

Run: `cd web && npx vitest run src/features/money/TransactionsPage.test.tsx -t "keeps the generic first-run copy"`
Expected: FAIL — "Nothing logged yet." is gone for a household that does have an account. Restore the condition and confirm green.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/money/transactionCopy.ts web/src/features/money/TransactionsPage.tsx web/src/features/money/TransactionsPage.test.tsx
git commit -m "fix(transactions): offer the way out from the middle of the empty ledger

A household with no accounts read 'Add an expense, some income or a
transfer, and it will show up here' in the centre of the screen while the
button that would do it was disabled and its explanation rendered at the
far right of the header. The centre now says what is actually missing and
links to Finances; the header's hint stays."
```

---

## Task 4: Say who Hearth is for

Six strings claim a two-person product. `domain.Role` is `owner` | `limited`, Settings renders them as "Parent" and "Kid", `VisibilityParentsOnly` exists, and there is no member cap anywhere — the seeded household has four. The copy is what is wrong.

**Files:**
- Modify: `web/src/features/auth/SignUpScreen.tsx:142` and `:195`
- Modify: `web/src/features/auth/SignUpCompleteScreen.tsx:151` and `:310`
- Modify: `web/src/features/auth/SignInScreen.tsx:314`
- Modify: `web/src/features/auth/SignUpScreen.test.tsx:30`

**Interfaces:**
- Consumes: nothing. Produces: nothing. Pure copy.

- [ ] **Step 1: Update the test's expectation first**

`web/src/features/auth/SignUpScreen.test.tsx:30` currently expects:

```tsx
        "One household, two owners. Set it up once and invite your partner in.",
```

Change it to:

```tsx
        "One household for the whole family. Set it up once, invite your partner, add the kids later.",
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/features/auth/SignUpScreen.test.tsx`
Expected: FAIL — the screen still renders the old sentence.

- [ ] **Step 3: Change the six strings**

Both `SignUpScreen.tsx:142` and `SignUpCompleteScreen.tsx:151`:

```tsx
            One household for the whole family. Set it up once, invite your partner, add the kids later.
```

`SignInScreen.tsx:314`, `SignUpScreen.tsx:195` and `SignUpCompleteScreen.tsx:310`:

```tsx
            Your household data stays inside your household.
```

Leave "You can invite your partner right after — nothing is shared until they accept." alone in both sign-up screens. It is still true, and it is the right promise at that moment.

- [ ] **Step 4: Run the auth tests to verify they pass**

Run: `cd web && npx vitest run src/features/auth`
Expected: PASS. If another test asserts one of the two changed sentences, update it the same way — the string moved, the behaviour did not.

- [ ] **Step 5: Confirm nothing else claims two members**

```bash
grep -rn "two owners\|two of you\|both of you" web/src api
```

Expected: no matches. If one appears in a comment, fix the comment too — a comment that contradicts the product is the next person's wrong turn.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/auth
git commit -m "fix(auth): describe the household the domain actually models

Sign-up and sign-in claimed 'One household, two owners' and 'stays between
the two of you'. The domain has owners and limited members, renders them as
Parent and Kid, carries a parents-only visibility, and caps membership at
nothing -- the seeded household has four. The front door was the only place
saying otherwise."
```

---

## Task 5: Two things sign-up says that are not true

**Files:**
- Modify: `web/src/features/auth/SignUpScreen.tsx:177` (the submit button's label)
- Modify: `web/src/features/auth/SignUpScreen.test.tsx` (one new test)
- Modify: `web/src/features/auth/SignUpCompleteScreen.tsx:167-169` (the household-name helper)
- Modify: `web/src/features/auth/SignUpCompleteScreen.test.tsx:46` (the helper text is asserted verbatim)

**Interfaces:**
- Consumes: nothing. Produces: nothing.

`SignUpCompleteScreen.test.tsx:46` already pins the helper text exactly:

```tsx
      screen.getByText("Shown at the top of the sidebar. Change it any time."),
```

so Step 5 is test-driven like the button: change the expectation, watch it fail, then change the screen.

- [ ] **Step 1: Write the failing test**

Add to `web/src/features/auth/SignUpScreen.test.tsx`, inside its existing `describe`:

```tsx
  it("does not promise to create a household from the address step", async () => {
    // This button emails a set-up link. The household is created by the second
    // screen's button, which is the one that may carry this name.
    renderWithRouter(<SignUpScreen />);

    expect(
      await screen.findByRole("button", { name: "Send me a set-up link" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create household" })).toBeNull();
  });
```

If that file's existing tests render the screen through a helper rather than `renderWithRouter` directly, use the helper — match the file.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/features/auth/SignUpScreen.test.tsx -t "does not promise to create"`
Expected: FAIL — the button is still named "Create household".

- [ ] **Step 3: Rename the button**

`web/src/features/auth/SignUpScreen.tsx`, the submit button's text:

```tsx
              Send me a set-up link
```

Leave `SignUpCompleteScreen`'s "Create household" alone. That one does create a household.

- [ ] **Step 4: Run it to verify it passes**

Run: `cd web && npx vitest run src/features/auth/SignUpScreen.test.tsx`
Expected: PASS.

- [ ] **Step 5: Fix the household-name helper, test first**

`web/src/features/auth/SignUpCompleteScreen.test.tsx:46` — change the expected string:

```tsx
      screen.getByText("Shown at the bottom of the sidebar, beside your name. Change it any time."),
```

Run: `cd web && npx vitest run src/features/auth/SignUpCompleteScreen.test.tsx -t "helper text"`
Expected: FAIL — the screen still says "top".

Then `web/src/features/auth/SignUpCompleteScreen.tsx`, under the Household name input:

```tsx
          <p className="text-[11px] text-muted">
            Shown at the bottom of the sidebar, beside your name. Change it any time.
          </p>
```

Run the same command again. Expected: PASS.

`Sidebar.tsx` renders `me.household.name` in the footer, beside the avatar and "Free plan", which is where design 5a puts it. The text was wrong, not the layout — do not move the name.

- [ ] **Step 6: Mutation-check the button test**

Rename the button back to "Create household" temporarily.

Run: `cd web && npx vitest run src/features/auth/SignUpScreen.test.tsx -t "does not promise to create"`
Expected: FAIL. Restore "Send me a set-up link" and confirm green.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/auth/SignUpScreen.tsx web/src/features/auth/SignUpScreen.test.tsx web/src/features/auth/SignUpCompleteScreen.tsx
git commit -m "fix(sign-up): stop step one promising what step two does

The address form's button read 'Create household' but sends a verification
mail; the household is created by the second screen's button of the same
name. The household-name helper said the name is shown at the top of the
sidebar -- it renders in the footer, beside the avatar, which is where the
design puts it."
```

---

## Task 6: Group the currency list

~140 options, no default, on the second screen of sign-up. The wire already carries the signal needed to split it.

**Files:**
- Modify: `web/src/features/auth/SignUpCompleteScreen.tsx:171-192` (the `<select>`)

**Interfaces:**
- Consumes: `currencies.data.currencies` — `{ code: string; symbol: string; name: string }[]` from `useCurrencies()`. `symbol` is `""` for a currency with no symbol in `api/internal/adapter/http/currency_handlers.go`'s `currencySymbols` map.
- Produces: nothing.

**Why `symbol`, not a second list.** `currencySymbols` is the server's own judgement about which currencies are worth surfacing, and its comment already says it is "the few symbols worth showing". Splitting on `symbol !== ""` means adding a symbol there promotes that currency with no frontend change. Today that is 17 currencies — the map holds 18 entries, but VND is not in `domain.SelectableCurrencies` (only two-minor-unit currencies are selectable), so its symbol is never reached.

- [ ] **Step 1: Write the failing test**

Add to `web/src/features/auth/SignUpCompleteScreen.test.tsx`. That file has no render helper — each test calls `stubFetchRoutes(...)` then `renderWithRouter(<SignUpCompleteScreen token="tok" />)` — and its shared `preview` fixture gives **both** its currencies a symbol, so this test needs its own stub carrying a symbol-less one:

```tsx
  it("puts the currencies with a symbol in their own group", async () => {
    // Split on the wire's own `symbol`, so adding one server-side promotes a
    // currency here with no frontend change. ALL has none, which is the whole
    // point of the fixture -- `preview`'s two both do.
    stubFetchRoutes({
      ...preview,
      "GET /api/v1/currencies": {
        status: 200,
        body: {
          currencies: [
            { code: "SGD", symbol: "S$", name: "Singapore dollar" },
            { code: "ALL", symbol: "", name: "Albanian lek" },
          ],
        },
      },
    });
    renderWithRouter(<SignUpCompleteScreen token="tok" />);

    const common = await screen.findByRole("group", { name: "Common" });
    expect(within(common).getByRole("option", { name: /SGD/ })).toBeInTheDocument();
    expect(within(common).queryByRole("option", { name: /ALL/ })).toBeNull();

    const all = screen.getByRole("group", { name: "All currencies" });
    expect(within(all).getByRole("option", { name: /ALL/ })).toBeInTheDocument();
  });
```

Import `within` from `@testing-library/react` alongside `screen`. An `<optgroup>` exposes the ARIA `group` role with its `label` as the accessible name, so this asserts the grouping a user actually gets rather than the markup.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/features/auth/SignUpCompleteScreen.test.tsx -t "own group"`
Expected: FAIL — no element with role `group`; the options are flat.

- [ ] **Step 3: Group the options**

`web/src/features/auth/SignUpCompleteScreen.tsx`. Above the component (module scope, so it is not rebuilt per render):

```tsx
type CurrencyOption = { code: string; symbol: string; name: string };

function currencyLabel(c: CurrencyOption) {
  return c.symbol ? `${c.code} (${c.symbol}) — ${c.name}` : `${c.code} — ${c.name}`;
}
```

Inside the component, before `return`:

```tsx
  const allCurrencies = currencies.data?.currencies ?? [];
  // The split is the wire's own `symbol`, not a list kept here: currencySymbols
  // on the server is already the judgement about which currencies are worth
  // surfacing, and duplicating it would let the two drift. Order inside each
  // group is the server's.
  const common = allCurrencies.filter((c) => c.symbol);
  const rest = allCurrencies.filter((c) => !c.symbol);
```

and the `<select>`'s children:

```tsx
            {/* No pre-selection. Defaulting to SGD would ship a
                wrong-currency first impression to everyone who did not
                notice the field, which is the reason the field exists. */}
            <option value="">Choose a currency</option>
            {common.length > 0 && (
              <optgroup label="Common">
                {common.map((c) => (
                  <option key={c.code} value={c.code}>
                    {currencyLabel(c)}
                  </option>
                ))}
              </optgroup>
            )}
            {rest.length > 0 && (
              <optgroup label="All currencies">
                {rest.map((c) => (
                  <option key={c.code} value={c.code}>
                    {currencyLabel(c)}
                  </option>
                ))}
              </optgroup>
            )}
```

Every currency the server sent is still selectable; none is removed.

- [ ] **Step 4: Run it to verify it passes**

Run: `cd web && npx vitest run src/features/auth/SignUpCompleteScreen.test.tsx`
Expected: PASS, including the file's existing cases — the option values are unchanged, only their grouping.

- [ ] **Step 5: Mutation-check**

Invert the filter so everything lands in "Common":

```tsx
  const common = allCurrencies;           // MUTATION -- do not keep
  const rest: CurrencyOption[] = [];      // MUTATION -- do not keep
```

Run: `cd web && npx vitest run src/features/auth/SignUpCompleteScreen.test.tsx -t "own group"`
Expected: FAIL — "ALL" is inside Common. Restore and confirm green.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/auth/SignUpCompleteScreen.tsx web/src/features/auth/SignUpCompleteScreen.test.tsx
git commit -m "feat(sign-up): group the common currencies above the rest

A required field with ~140 flat options and no default, on the second
screen of sign-up. The split is the wire's own `symbol` -- currencySymbols
on the server is already the judgement about which are worth surfacing, so
adding one there promotes it here with no frontend change. Nothing is
removed from the list and nothing is pre-selected."
```

---

## Task 7: Verify in a real browser, then update the documents

The bar this project sets: tests passing is not the claim that it works (`verifying-in-the-real-environment` skill). The product owner asked for this explicitly on 2026-07-30, after a feature that verified 15 of 15 still surprised them in first-run use.

**Files:**
- Create: `docs/superpowers/plans/2026-07-31-hearth-ux-repair-verification.md`
- Create: `docs/superpowers/plans/2026-07-31-hearth-ux-repair-screenshots/` (before/after pairs)
- Modify: `docs/FEATURE_TRACKER.md`
- Modify: `docs/LEARNING.md`
- Modify: `docs/SYSTEM_DESIGN.md`

- [ ] **Step 1: Run the full gate**

Run: `make lint && make test`
Expected: PASS. The Go suite needs a Docker socket; on the original machine that is colima:

```bash
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
```

- [ ] **Step 2: Walk a fresh household**

`make dev`, then in a real browser at `http://localhost:5173`:

1. Sign out if a session exists. Sign up with a new address; collect the mail at `http://localhost:8025`.
2. Complete step 2. Check: the button on step 1 read "Send me a set-up link"; the currency select has **Common** and **All currencies** groups; the household-name helper says "bottom of the sidebar".
3. Land on `/`. It is still the Overview placeholder — milestone 2 replaces it. Check the sidebar shows only Overview, MONEY (Finances / Transactions / Budget) and Settings.
4. Open `/money/transactions`. Check the centre reads "Add an account first" with a working link to Finances, and that the link lands on `/money`.
5. Add an account, return to Transactions, and check the centre reverts to "Nothing logged yet." and "+ Add transaction" is enabled.
6. Visit `http://localhost:5173/marriage` directly. Check it renders "Page not found." and not "Arriving in slice 3."
7. Open `/settings`. Check Spaces still lists Money, Marriage and Family, and that every Notifications toggle sits beside the label naming it.

- [ ] **Step 3: Walk the seeded household**

`make seed`, sign in as the seeded owner, and repeat steps 3, 6 and 7 above with real data present. This is the pass the critique never reached — a populated Finances, Transactions and Budget inside the new container. Check no card is clipped and no table scrolls the page body sideways.

- [ ] **Step 4: Capture the evidence**

For `/money/transactions` and `/settings`, save a before and an after screenshot into `docs/superpowers/plans/2026-07-31-hearth-ux-repair-screenshots/`. Two screenshots that come back byte-identical mean the change did not land — the finance-fixes branch hit exactly that and it is recorded in `docs/LEARNING.md`.

- [ ] **Step 5: Write the verification file**

`docs/superpowers/plans/2026-07-31-hearth-ux-repair-verification.md`, in the shape `2026-07-30-hearth-budget-verification.md` uses: one numbered criterion per check above, each marked pass or fail, with a note wherever a criterion was met by an interpreted rather than literal path. A note is not a failure; a silent pass over an interpreted path is.

- [ ] **Step 6: Update the three documents**

`docs/FEATURE_TRACKER.md` — **no row changes symbol.** This milestone ships no feature; Marriage and Family stay ⬜, because removing a placeholder from the navigation does not make them less unbuilt. Add a note in each of those sections saying their routes were deleted with the placeholders, so whoever builds them adds the route and the `SPACE_PAGES` entry back rather than debugging the sidebar. Recount the summary table anyway and confirm it still sums.

`docs/LEARNING.md` — one entry per defect worth remembering:
- A placeholder page shipped as a live navigation destination, in the team's own planning vocabulary ("slice N"), on three of six destinations. What would have caught it sooner: walking the app as a new user rather than walking each feature.
- A layout with no maximum width put a heading and its primary action 2400px apart on a wide monitor. Every unit test passed. Caught only by measuring in the browser.
- A disabled control explained itself where nobody was looking. The decision to disable was right and documented; the placement was never checked against the empty state it belongs to.
- Sign-up copy described a two-person product for as long as the domain modelled a family. What would have caught it: reading the front door and Settings in the same sitting.

If any of these matches an existing pattern section, add it there as evidence rather than starting a new section — the repetition is the point.

`docs/SYSTEM_DESIGN.md` — use the `maintaining-system-design` skill. The route tree lost four routes; find every diagram and paragraph naming `/marriage` or `/family/calendar` and correct both the diagram and the prose beneath it.

- [ ] **Step 7: Commit**

```bash
git add docs/
git commit -m "docs: record the UX repair walk and what it taught"
```

---

## Self-review

**Spec coverage.** §3.1 container → Task 1. §3.2 spaces and routes → Task 2. §3.3 Transactions → Task 3. §3.4 audience copy → Task 4. §3.5a button and §3.5b helper → Task 5. §3.5c currencies → Task 6. §3.6 definition of done → Task 7. No spec section is unclaimed.

**Placeholders.** None. Every code step carries the code; every test step carries the test; every run step carries the command and the expected result.

**Type consistency.** `SPACE_PAGES` keeps `Record<string, { label: string; to: string }[]>` in Task 2. `TRANSACTIONS_COPY`'s three new keys are named identically in Task 3's copy step and its JSX. `CurrencyOption` in Task 6 matches the wire's `{ code, symbol, name }`, which is what `currencyLabel` in the existing file already destructures.

**Known interaction between tasks.** Task 2 changes what the existing Sidebar test's fixture may assert (Family no longer renders), and Task 3 changes what an existing Transactions assertion may match. Both are called out inside the task that causes them, with the fix, rather than left for the suite to discover.
