# Mobile Responsive Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every existing Hearth screen usable on a 375px-wide phone without redesigning any of them.

**Architecture:** The fixed 236px sidebar becomes an off-canvas drawer below `lg`, driven by a new sticky mobile top bar; `Sidebar.tsx` itself is reused verbatim rather than duplicated. Everything below the shell is a mobile-first sweep of existing Tailwind classes, plus two components extracted because they already exist as copy-paste at 8 and 13 call sites.

**Tech Stack:** React 19, TypeScript, TanStack Router + Query, Tailwind CSS v4, Vitest + Testing Library (jsdom), Playwright MCP for browser verification.

**Spec:** `docs/superpowers/specs/2026-08-15-hearth-mobile-responsive-design.md`

## Global Constraints

Every task's requirements implicitly include these. Values copied verbatim from the spec.

- **Two breakpoints only:** `sm` (640px) for content reflow, `lg` (1024px) for the shell navigation switch. A third needs its reason written beside it.
- **Mobile-first:** unprefixed classes describe a phone; prefixes add width.
- **Floor:** 375px is designed and tuned; 320px must not scroll horizontally.
- **Full-height rules use `dvh`, never `screen`.** `100vh` on iOS Safari is the large viewport, so a `h-screen` box hides its bottom edge under the toolbar.
- **Desktop at 1440px does not move**, with exactly two named exceptions: `OverviewPage`'s gutter (Task 6) and touch targets (Task 9).
- **Exactly one `Sidebar` instance** renders at any viewport. Two would put two elements carrying `data-testid="sidebar-space"` in the DOM and make every existing `Sidebar.test.tsx` query ambiguous.
- **Drawer overlay sits above `z-10`** — `QuickAddMenu.tsx:63` is `z-10`.
- **Touch targets reach 44px** (Task 9 only).
- Comments say **why**, never what the line already says (CLAUDE.md).

**Commands used throughout:**

```bash
cd web && npx vitest run src/features/shell/NavDrawer.test.tsx   # one test file
make test-web                                                    # all frontend tests
make lint                                                        # arch lint, tsc, eslint, go vet
```

**The stack is already running** (`make dev`; ports 5173 and 8080 answering). If it is not:

```bash
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make dev    # then, in another shell, make seed
```

Seeded sign-in: `andreas@hearth.family` / `hearth-dev-password`.

## Honest note on what can be tested

jsdom performs no layout, so **no Vitest test in this repo can observe a CSS breakpoint.** Tasks 1–3 carry real behaviour tests and satisfy CLAUDE.md's "at least one new test, mutation-checked". Tasks 4–9 are CSS sweeps whose verification is `make lint` plus a browser walk plus a desktop-parity screenshot — the plan says so rather than inventing tests that assert class strings, which test the implementation and not the behaviour.

## File Structure

**Created:**

| File | Responsibility |
|---|---|
| `web/src/features/shell/MobileTopBar.tsx` | The sticky brand + hamburger row shown below `lg`. Nothing else. |
| `web/src/features/shell/MobileTopBar.test.tsx` | Its accessible name and its callback. |
| `web/src/features/shell/NavDrawer.tsx` | Off-canvas positioning, backdrop, Escape, focus in/out. Holds no navigation content. |
| `web/src/features/shell/NavDrawer.test.tsx` | Escape, backdrop, focus restoration. |
| `web/src/features/shell/AppShell.test.tsx` | `inert` on `<main>`, close-on-navigate, one `Sidebar` instance. |
| `web/src/components/PageContainer.tsx` | The page-level flex column and its responsive gutters. 9 callers. |
| `web/src/components/FieldPair.tsx` | The two-up field grid that stacks below `sm`. 13 callers. |

**Modified:** `web/src/features/shell/AppShell.tsx`, `web/src/components/Modal.tsx`, six auth screens, nine pages, eight modal files, `TransactionFilters.tsx`, `BillRow.tsx`, `BudgetCategoryGrid.tsx`, `Sidebar.tsx` (Task 9 only, touch target), plus `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`.

**Not modified, deliberately:** `Modal.tsx`'s `max-w-[calc(100vw-32px)]` container width, ledger and bill row structure, `Sidebar.tsx`'s content and testids.

---

### Task 1: MobileTopBar

The only route back to navigation on a phone. Sticky, because a ledger scrolled two screens down must not strip it away.

**Files:**
- Create: `web/src/features/shell/MobileTopBar.tsx`
- Test: `web/src/features/shell/MobileTopBar.test.tsx`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `export function MobileTopBar({ onOpenNav }: { onOpenNav: () => void }): JSX.Element` — Task 3 renders it.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/shell/MobileTopBar.test.tsx`:

```tsx
// The hamburger is the only way back to navigation below lg, so its
// accessible name is load-bearing: a screen-reader user with no visible
// sidebar has nothing else to go on.
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MobileTopBar } from "./MobileTopBar";

describe("MobileTopBar", () => {
  it("exposes the nav trigger by an accessible name", () => {
    render(<MobileTopBar onOpenNav={() => {}} />);

    expect(screen.getByRole("button", { name: "Open navigation" })).toBeInTheDocument();
  });

  it("calls onOpenNav when the trigger is pressed", () => {
    const onOpenNav = vi.fn();
    render(<MobileTopBar onOpenNav={onOpenNav} />);

    fireEvent.click(screen.getByRole("button", { name: "Open navigation" }));

    expect(onOpenNav).toHaveBeenCalledTimes(1);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/shell/MobileTopBar.test.tsx`
Expected: FAIL — `Failed to resolve import "./MobileTopBar"`.

- [ ] **Step 3: Write the component**

Create `web/src/features/shell/MobileTopBar.tsx`:

```tsx
// The mobile counterpart to Sidebar's brand row. Below `lg` the sidebar sits
// off-canvas (see NavDrawer), so this row holds the only control that can
// bring it back -- which is why it is sticky rather than static: a ledger
// scrolled two screens down would otherwise leave a phone with no way to
// navigate at all.
//
// The design has no mobile artwork to follow (design/Household Dashboard.dc.html
// is a 1440px canvas with zero media queries), so this row is invention,
// authorised by the product owner on 2026-08-15 under "same UI, layout and
// structure -- invent where a phone forces a choice". It repeats the
// sidebar's own brand mark rather than introducing a second one.
//
// The sidebar's ⌘K chip is deliberately absent: it opens nothing (no command
// palette exists), and on 375px it would cost width that a product name and a
// 44px touch target both need.
export function MobileTopBar({ onOpenNav }: { onOpenNav: () => void }) {
  return (
    <header className="sticky top-0 z-30 flex items-center gap-2.5 border-b border-hairline bg-card px-4 py-2.5 lg:hidden">
      <div className="h-7 w-7 rounded-lg bg-accent" />
      <div className="text-[15px] font-semibold tracking-[-0.01em]">Hearth</div>
      <button
        type="button"
        onClick={onOpenNav}
        aria-label="Open navigation"
        className="ml-auto grid h-11 w-11 place-items-center rounded-lg border border-hairline text-[15px] text-ink"
      >
        ☰
      </button>
    </header>
  );
}
```

`z-30` is chosen against `QuickAddMenu.tsx:63`'s `z-10`, so the Overview's quick-add menu cannot render on top of the bar. `h-11 w-11` is 44px, this file's share of the touch-target floor.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/features/shell/MobileTopBar.test.tsx`
Expected: PASS, 2 tests.

- [ ] **Step 5: Mutation-check the accessible-name test**

Temporarily change `aria-label="Open navigation"` to `aria-label="Menu"`. Re-run. The first test MUST fail. Restore the original.

This is the check CLAUDE.md's definition of done asks for: a test that passes whatever the code says is not a test.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/shell/MobileTopBar.tsx web/src/features/shell/MobileTopBar.test.tsx
git commit -m "feat(web): add the mobile top bar that opens navigation"
```

---

### Task 2: NavDrawer

Positioning, backdrop, Escape and focus. It holds no navigation content of its own — that stays in `Sidebar.tsx`.

**Files:**
- Create: `web/src/features/shell/NavDrawer.tsx`
- Test: `web/src/features/shell/NavDrawer.test.tsx`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `export function NavDrawer({ open, onClose, children }: { open: boolean; onClose: () => void; children: ReactNode }): JSX.Element` — Task 3 wraps `<Sidebar>` in it.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/shell/NavDrawer.test.tsx`:

```tsx
// The drawer is deliberately NOT built on <dialog>, unlike components/Modal
// -- the same element has to be the static desktop column, and a <dialog>
// cannot be one (see NavDrawer.tsx's header comment). That trade means the
// three things <dialog> would have supplied for free are hand-written here,
// so all three are tested: Escape closes, a backdrop press closes, and focus
// goes into the drawer on open and back to the trigger on close.
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NavDrawer } from "./NavDrawer";

function Harness({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <>
      <button type="button">Open navigation</button>
      <NavDrawer open={open} onClose={onClose}>
        <nav>
          <a href="/">Overview</a>
        </nav>
      </NavDrawer>
    </>
  );
}

describe("NavDrawer", () => {
  it("renders its children", () => {
    render(<Harness open={false} onClose={() => {}} />);

    expect(screen.getByText("Overview")).toBeInTheDocument();
  });

  it("closes on Escape while open", () => {
    const onClose = vi.fn();
    render(<Harness open onClose={onClose} />);

    fireEvent.keyDown(document, { key: "Escape" });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("ignores Escape while closed, so it cannot close a drawer nobody opened", () => {
    const onClose = vi.fn();
    render(<Harness open={false} onClose={onClose} />);

    fireEvent.keyDown(document, { key: "Escape" });

    expect(onClose).not.toHaveBeenCalled();
  });

  it("closes when the backdrop is pressed", () => {
    const onClose = vi.fn();
    render(<Harness open onClose={onClose} />);

    fireEvent.click(screen.getByTestId("nav-drawer-backdrop"));

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("has no backdrop while closed", () => {
    render(<Harness open={false} onClose={() => {}} />);

    expect(screen.queryByTestId("nav-drawer-backdrop")).toBeNull();
  });

  it("moves focus into the drawer on open and returns it on close", () => {
    const trigger = () => screen.getByRole("button", { name: "Open navigation" });
    const { rerender } = render(<Harness open={false} onClose={() => {}} />);

    trigger().focus();
    expect(document.activeElement).toBe(trigger());

    rerender(<Harness open onClose={() => {}} />);
    expect(screen.getByTestId("nav-drawer").contains(document.activeElement)).toBe(true);

    rerender(<Harness open={false} onClose={() => {}} />);
    expect(document.activeElement).toBe(trigger());
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/shell/NavDrawer.test.tsx`
Expected: FAIL — `Failed to resolve import "./NavDrawer"`.

- [ ] **Step 3: Write the component**

Create `web/src/features/shell/NavDrawer.tsx`:

```tsx
// Positions the sidebar: off-canvas below `lg`, and out of the way entirely
// at `lg` and above. It holds no navigation content -- Sidebar.tsx renders
// that, exactly once, and this wraps that single instance rather than a
// second copy (two copies would put two data-testid="sidebar-space" elements
// in the DOM and make every query in Sidebar.test.tsx ambiguous).
//
// Why not a native <dialog>, when components/Modal makes such a point of
// using one for free focus trapping and Escape handling: the same element
// here must also be the *static desktop column*, and a <dialog> cannot be
// one. So the three things <dialog> would have given are written by hand
// below -- Escape, focus in, focus back -- and NavDrawer.test.tsx covers all
// three because hand-written versions are exactly what regresses silently.
//
// `lg:contents` is what guarantees the desktop layout is untouched: at `lg`
// this wrapper stops generating a box at all, so Sidebar becomes the grid
// child of AppShell's two-column grid again, precisely as it was before this
// component existed. `position` and `transform` have no effect on an element
// with `display: contents`, so neither needs an `lg:` counterpart.
//
// `visibility` is the exception, and it is the one that bites: it is an
// *inherited* property, and `display: contents` suppresses the box, not the
// inheritance. Without the `lg:visible` below, the desktop sidebar would
// inherit `visibility: hidden` from this element in its normal state --
// closed -- and render as an invisible 236px column. Hence `lg:visible` on
// the closed branch specifically.
//
// Visibility, not aria-hidden, is what hides the closed drawer: `invisible`
// removes it from the tab order and the accessibility tree together, and it
// is a CSS rule, so it responds to the viewport without any JavaScript
// needing to know the width. An aria-hidden attribute would have to be
// driven from JS and would wrongly hide the visible desktop sidebar.
import { type ReactNode, useEffect, useRef } from "react";

export function NavDrawer({
  open,
  onClose,
  children,
}: {
  open: boolean;
  onClose: () => void;
  children: ReactNode;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const previouslyFocusedRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!open) return;

    // The same save-and-restore components/Modal performs, and for the same
    // reason: a keyboard user who opens the drawer and closes it again must
    // land back on the control they pressed, not at the top of the document.
    previouslyFocusedRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;
    panelRef.current?.focus();

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    document.addEventListener("keydown", handleKeyDown);

    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      const previouslyFocused = previouslyFocusedRef.current;
      if (previouslyFocused && document.contains(previouslyFocused)) {
        previouslyFocused.focus();
      }
    };
  }, [open, onClose]);

  return (
    <>
      {open && (
        <div
          data-testid="nav-drawer-backdrop"
          onClick={onClose}
          className="fixed inset-0 z-30 bg-black/40 lg:hidden"
        />
      )}
      <div
        ref={panelRef}
        tabIndex={-1}
        data-testid="nav-drawer"
        className={`fixed left-0 top-0 z-40 flex h-dvh w-[236px] flex-col overflow-y-auto transition-transform lg:contents ${
          open ? "visible translate-x-0" : "invisible -translate-x-full lg:visible"
        }`}
      >
        {children}
      </div>
    </>
  );
}
```

`h-dvh`, not `h-screen`: the drawer is full-height and **sign-out sits at its bottom edge**, which is exactly what iOS Safari's toolbar would cover if this were sized to `100vh`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/features/shell/NavDrawer.test.tsx`
Expected: PASS, 6 tests.

- [ ] **Step 5: Mutation-check the Escape test**

Temporarily change `if (event.key === "Escape")` to `if (event.key === "Esc")`. Re-run. "closes on Escape while open" MUST fail. Restore.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/shell/NavDrawer.tsx web/src/features/shell/NavDrawer.test.tsx
git commit -m "feat(web): add the off-canvas nav drawer, with the focus handling <dialog> would have given"
```

---

### Task 3: Wire the shell

`AppShell` goes single-column below `lg`, owns the open state, and marks `<main>` inert while the drawer covers it.

**Files:**
- Modify: `web/src/features/shell/AppShell.tsx` (whole component body, lines 8-38)
- Test: `web/src/features/shell/AppShell.test.tsx` (create)

**Interfaces:**
- Consumes: `MobileTopBar({ onOpenNav })` from Task 1, `NavDrawer({ open, onClose, children })` from Task 2.
- Produces: the shell every signed-in page renders inside. No exported API changes.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/shell/AppShell.test.tsx`. It builds its own two-route router rather than using `test/renderWithRouter.tsx`, because the close-on-navigate behaviour needs somewhere to navigate *to* and that helper mounts a single root route:

```tsx
// Three behaviours the shell owns and no other file can be asked about:
// 1. While the drawer covers the page, <main> is inert -- otherwise a phone
//    user can Tab into content sitting underneath an opaque overlay.
// 2. Navigating closes the drawer, so a tapped link lands on the new page
//    rather than behind the overlay that is still covering it.
// 3. Exactly one Sidebar renders. Two would make every
//    data-testid="sidebar-space" query in Sidebar.test.tsx ambiguous.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AppShell } from "./AppShell";

const me = {
  user: {
    id: "user-1",
    email: "andreas@hearth.family",
    displayName: "Andreas",
    avatarInitial: "A",
  },
  household: {
    id: "household-1",
    name: "Andreas & Christine",
    familyName: "Oentoro",
    primaryCurrency: "SGD",
    showSecondaryCurrency: false,
    secondaryCurrency: "",
    fxRateMode: "static",
  },
  membership: {
    id: "membership-1",
    householdId: "household-1",
    userId: "user-1",
    role: "owner",
    capabilities: ["money"],
  },
  capabilities: ["money"],
  spaces: [
    {
      id: "space-money",
      key: "money",
      name: "Money",
      visibility: "everyone",
      position: 1,
      isBuiltin: true,
      requiredCapability: "money",
    },
  ],
};

function renderShell() {
  stubFetchRoutes({ "GET /api/v1/auth/me": { status: 200, body: me } });

  const rootRoute = createRootRoute({ component: AppShell });
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/",
    component: () => <p>Home</p>,
  });
  const elsewhereRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/elsewhere",
    component: () => <p>Elsewhere</p>,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute, elsewhereRoute]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return {
    ...render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
    router,
  };
}

function openNav() {
  fireEvent.click(screen.getByRole("button", { name: "Open navigation" }));
}

describe("AppShell", () => {
  it("marks the page inert while the drawer covers it, and clears it on close", async () => {
    renderShell();
    const main = await screen.findByRole("main");

    expect(main.hasAttribute("inert")).toBe(false);

    openNav();
    await waitFor(() => expect(main.hasAttribute("inert")).toBe(true));

    fireEvent.click(screen.getByTestId("nav-drawer-backdrop"));
    await waitFor(() => expect(main.hasAttribute("inert")).toBe(false));
  });

  it("closes the drawer on navigation", async () => {
    const { router } = renderShell();
    await screen.findByRole("main");

    openNav();
    await waitFor(() => expect(screen.getByTestId("nav-drawer-backdrop")).toBeInTheDocument());

    // Navigated programmatically, not by clicking the link in <main>: with
    // the drawer open <main> is inert, so in a real browser that link cannot
    // be reached at all. jsdom does not implement inert, so clicking it would
    // pass on an interaction that cannot happen. The effect is keyed on the
    // pathname precisely so a route change from anywhere closes the drawer,
    // and this is that claim.
    await router.navigate({ to: "/elsewhere" });

    await waitFor(() => expect(screen.queryByTestId("nav-drawer-backdrop")).toBeNull());
  });

  it("renders exactly one sidebar", async () => {
    renderShell();
    await screen.findByRole("main");

    // Money expands to five links and no more; a second Sidebar instance
    // would double every one of them.
    const links = await screen.findAllByTestId("sidebar-space");
    expect(links).toHaveLength(5);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/shell/AppShell.test.tsx`
Expected: FAIL — no "Open navigation" button exists yet.

- [ ] **Step 3: Rewrite AppShell**

Replace the body of `web/src/features/shell/AppShell.tsx` with:

```tsx
// The authenticated layout: navigation plus the current page's outlet.
// Mounted only inside RequireAuth's <Outlet />, so `useMe()` here reads the
// same already-resolved ['me'] cache entry RequireAuth just confirmed
// succeeded -- this does not perform a second, independent fetch.
//
// Below `lg` the navigation is off-canvas (NavDrawer) and reached through
// MobileTopBar; at `lg` and above the grid below is exactly the two-column
// layout this file has always had, because NavDrawer resolves to
// `display: contents` at that width and stops generating a box.
import { useEffect, useState } from "react";
import { Outlet, useRouterState } from "@tanstack/react-router";
import { useMe } from "../auth/useAuth";
import { MobileTopBar } from "./MobileTopBar";
import { NavDrawer } from "./NavDrawer";
import { Sidebar } from "./Sidebar";

export function AppShell() {
  const me = useMe();
  const [navOpen, setNavOpen] = useState(false);
  const pathname = useRouterState({ select: (state) => state.location.pathname });

  // A tapped link must land the household on the new page, not behind an
  // overlay still covering it. Keyed on the pathname rather than on the link
  // press, so a route change from anywhere -- a redirect, a programmatic
  // navigate -- closes it too.
  useEffect(() => {
    setNavOpen(false);
  }, [pathname]);

  if (!me.data) {
    return (
      <main className="grid min-h-dvh place-items-center">
        <p className="text-sm text-muted">Loading…</p>
      </main>
    );
  }

  return (
    <div className="min-h-dvh bg-surface lg:grid lg:grid-cols-[236px_1fr]">
      <MobileTopBar onOpenNav={() => setNavOpen(true)} />
      <NavDrawer open={navOpen} onClose={() => setNavOpen(false)}>
        <Sidebar me={me.data} />
      </NavDrawer>
      {/* `inert` keeps Tab out of the page while the drawer's opaque backdrop
          covers it -- the one part of a <dialog>'s behaviour that cannot be
          expressed in CSS. React 19 forwards it as a real attribute. */}
      <main inert={navOpen || undefined} className="lg:overflow-y-auto">
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
}
```

Two deliberate changes beyond the drawer wiring:

- `min-h-screen` → `min-h-dvh` (global constraint).
- `overflow-y-auto` on `<main>` → `lg:overflow-y-auto`. Below `lg` the document scrolls, which is what makes `MobileTopBar`'s `sticky` work and what prevents a second nested scroll container from fighting the drag.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/features/shell/AppShell.test.tsx`
Expected: PASS, 3 tests.

If `inert` arrives as a string rather than an attribute, check React is 19.x — `inert` support landed there. Do not fall back to `aria-hidden`; it does not stop Tab.

- [ ] **Step 5: Run the whole frontend suite**

Run: `make test-web`
Expected: PASS. `Sidebar.test.tsx` in particular must be untouched and green — that is the one-instance guarantee holding.

- [ ] **Step 6: Verify in the browser at 375px**

With the app running, drive `http://localhost:5173` at a 375×812 viewport, signed in:

1. The sidebar is gone; the top bar shows brand and hamburger.
2. `document.documentElement.scrollWidth` equals the client width — no sideways scroll.
3. Press the hamburger: the drawer slides in over a dimmed page.
4. Press a nav link: the drawer closes and the new page is in front.
5. Press Escape with the drawer open: it closes.

Then at 1440×900, confirm the shell renders as it always did — and check the
inherited-visibility trap explicitly, because a parity screenshot only catches
it if someone looks at the sidebar rather than the content column:

```js
getComputedStyle(document.querySelector("nav")).visibility  // must be "visible"
```

- [ ] **Step 7: Capture the desktop-parity baseline**

At 1440×900, screenshot Overview, Transactions and Settings. Keep them; every later task compares against these. A difference that is not `OverviewPage`'s gutter (Task 6) or a touch target (Task 9) means that task is wrong.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/shell/AppShell.tsx web/src/features/shell/AppShell.test.tsx
git commit -m "feat(web): put navigation in a drawer below lg, and inert the page beneath it"
```

---

### Task 4: Move every full-height rule to dvh

Four rules size to viewport height. `100vh` on iOS Safari is the height with the URL bar hidden, so each hides its bottom edge under the toolbar.

**Files:**
- Modify: `web/src/components/Modal.tsx:116`
- Modify: `web/src/features/auth/SignInScreen.tsx:196`, `SignUpScreen.tsx`, `SignUpCompleteScreen.tsx`, `InviteScreen.tsx` (`AuthShell`), `MagicLinkConsumeScreen.tsx`, `CheckYourEmailPanel.tsx`
- Modify: `web/src/features/shell/RequireAuth.tsx:11,29`

`AppShell.tsx` and `NavDrawer.tsx` already took `dvh` in Tasks 2–3.

- [ ] **Step 1: Find every site**

```bash
cd web/src && grep -rn 'h-screen\|min-h-screen' --include='*.tsx' .
```

Expected: `Modal.tsx` (one, `h-screen w-screen`), `RequireAuth.tsx` (two), and one per auth screen. Record the count before changing anything.

- [ ] **Step 2: Replace them**

`h-screen` → `h-dvh`, `min-h-screen` → `min-h-dvh`. In `Modal.tsx:116` only the height token changes; `w-screen` stays, because there is no horizontal equivalent problem and `dvw` would be a change with no reason behind it.

Add this comment above `Modal.tsx`'s className, where someone would try to change it back:

```tsx
      // h-dvh, not h-screen: on iOS Safari `100vh` is the *large* viewport --
      // the height with the URL bar hidden -- so a dialog sized to it puts its
      // bottom edge under the browser toolbar. AccountModal's content is 665px
      // against roughly 650px of visible height on an iPhone, which is its
      // submit button sitting exactly where the user cannot reach it.
```

- [ ] **Step 3: Verify nothing was missed**

```bash
cd web/src && grep -rn 'h-screen' --include='*.tsx' . | grep -v 'w-screen'
```

Expected: no output.

- [ ] **Step 4: Lint and test**

Run: `make lint && make test-web`
Expected: PASS.

- [ ] **Step 5: Browser check**

At 375×812, open the Add-account modal from Finances and confirm its submit button is reachable without the page scrolling under a toolbar. Compare Overview at 1440 against the Task 3 baseline: identical.

- [ ] **Step 6: Commit**

```bash
git add web/src
git commit -m "fix(web): size full-height boxes to dvh, so iOS does not hide their bottom edge"
```

---

### Task 5: Auth screens stop being 428px wide

`/sign-in` scrolls sideways at 375px: the card is `w-[428px]` with no max-width. This is the first screen a stranger sees, and Hearth is sold self-serve.

**Files:**
- Modify: `web/src/features/auth/SignInScreen.tsx:204`, `SignUpScreen.tsx:137`, `SignUpCompleteScreen.tsx:57` and `:321`, `InviteScreen.tsx:42`, `MagicLinkConsumeScreen.tsx:45`, `CheckYourEmailPanel.tsx:39`

- [ ] **Step 1: Confirm the site list**

```bash
cd web/src && grep -rn 'w-\[428px\]' --include='*.tsx' . | grep -v 'max-w'
```

Expected: 7 lines across 6 files. The two `max-w-[428px]` lines (`SignUpScreen.tsx:191`, `SignUpCompleteScreen.tsx:332`) are already correct — leave them.

- [ ] **Step 2: Replace**

`w-[428px]` → `w-full max-w-[428px]` at all 7 sites.

No wrapper padding is needed: each of these sits inside `min-h-dvh grid place-items-center bg-canvas p-6 …`, so the card lands 24px from each edge and never goes edge-to-edge.

- [ ] **Step 3: Lint and test**

Run: `make lint && make test-web`
Expected: PASS. `SignInScreen.test.tsx`, `SignUpScreen.test.tsx`, `InviteScreen.test.tsx` and `MagicLinkConsumeScreen.test.tsx` all still pass — they assert behaviour, not width.

- [ ] **Step 4: Browser check**

At 375×812, load `/sign-in` and confirm `document.documentElement.scrollWidth` equals the client width (it was 452 against 360). Do the same for `/sign-up`. At 1440, both cards are still 428px.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/auth
git commit -m "fix(web): let auth cards shrink below 428px instead of scrolling the page sideways"
```

---

### Task 6: PageContainer

`flex flex-col gap-5 px-9 py-8` is copy-pasted at 8 sites. At 375px its 36px gutters eat 19% of the screen.

**Files:**
- Create: `web/src/components/PageContainer.tsx`
- Modify: `SettingsPage.tsx:13`, `FinancesPage.tsx:109,132,144`, `TransactionsPage.tsx:393`, `BudgetPage.tsx:238`, `BillsPage.tsx:295`, `GoalsPage.tsx:129`, `OverviewPage.tsx:43`

**Interfaces:**
- Produces: `export function PageContainer({ children, ...rest }: { children: ReactNode } & HTMLAttributes<HTMLDivElement>): JSX.Element` — it must forward `data-testid`, because `BudgetPage`, `BillsPage` and `GoalsPage` set one on this exact element and their tests query it.

- [ ] **Step 1: Create the component**

```tsx
// The outer box every page shares: a vertical stack with the design's own
// gutters at `sm` and above, and tighter ones below it -- 36px of padding
// either side costs 19% of a 375px screen, which is the difference between a
// readable ledger row and a wrapped one.
//
// Extracted rather than repeated because the class string below already
// existed verbatim at eight call sites. It forwards the rest of its props so
// pages can keep the `data-testid` their own tests query.
import type { HTMLAttributes, ReactNode } from "react";

export function PageContainer({
  children,
  className = "",
  ...rest
}: { children: ReactNode } & HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={`flex flex-col gap-5 px-4 py-6 sm:px-9 sm:py-8 ${className}`} {...rest}>
      {children}
    </div>
  );
}
```

- [ ] **Step 2: Apply it to the eight identical sites**

Replace `<div className="flex flex-col gap-5 px-9 py-8">` with `<PageContainer>` (and the matching `</div>` with `</PageContainer>`), keeping any `data-testid` as a prop: `<PageContainer data-testid="budget-page">`. Add the import to each file.

- [ ] **Step 3: Apply it to OverviewPage**

`OverviewPage.tsx:43` is `flex flex-col gap-5 p-10` — the odd one out. It joins, which moves that page's desktop gutters by 4px horizontally and 8px vertically. This is one of the two places in the whole plan where desktop pixels legitimately move; the alternative is a second container component with one caller.

- [ ] **Step 4: Lint and test**

Run: `make lint && make test-web`
Expected: PASS. If a page test fails on a missing testid, the `{...rest}` forwarding is not reaching the element.

- [ ] **Step 5: Browser check**

At 375, walk Overview, Finances, Transactions, Budget, Goals, Bills and Settings: gutters are 16px, no horizontal scroll on any. At 1440, compare against the Task 3 baseline — identical except Overview's gutter.

- [ ] **Step 6: Commit**

```bash
git add web/src
git commit -m "refactor(web): extract PageContainer, and shrink page gutters on small screens"
```

---

### Task 7: FieldPair

`grid grid-cols-2 gap-4`, unconditional, at 13 sites. Two fields side by side inside a 343px modal panel gives each about 155px.

**Files:**
- Create: `web/src/components/FieldPair.tsx`
- Modify: `TransactionModal.tsx:333,385,424`, `BillModal.tsx:341,379,418`, `AccountModal.tsx:260,301`, `GoalModal.tsx:336`, `GoalContributionsPanel.tsx:231`, `MarkPaidModal.tsx:102`, `InviteMemberModal.tsx:91`, `NewSpaceModal.tsx:98`

**Interfaces:**
- Produces: `export function FieldPair({ children }: { children: ReactNode }): JSX.Element`

- [ ] **Step 1: Confirm the site list**

```bash
cd web/src && grep -rn 'grid grid-cols-2 gap-4' --include='*.tsx' . | grep -v test
```

Expected: 13 lines across 8 files. A site with different gap or extra classes is not one of these — leave it and note it.

- [ ] **Step 2: Create the component**

```tsx
// Two form fields side by side, stacking below `sm`. Inside a modal panel
// that measures 343px on a 375px phone, two columns leave each field about
// 155px -- narrower than the date and amount inputs they usually hold.
//
// Extracted because this exact grid already appeared at thirteen call sites
// across eight modals; a fourteenth would have been a fourteenth place to
// forget the breakpoint.
import type { ReactNode } from "react";

export function FieldPair({ children }: { children: ReactNode }) {
  return <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">{children}</div>;
}
```

- [ ] **Step 3: Apply it at all 13 sites**

Replace `<div className="grid grid-cols-2 gap-4">` with `<FieldPair>` and its closing tag, adding the import per file.

- [ ] **Step 4: Lint and test**

Run: `make lint && make test-web`
Expected: PASS. The modal test files (`TransactionModal.test.tsx`, `BillModal.test.tsx`, `AccountModal.test.tsx`, `GoalModal.test.tsx`, `MarkPaidModal.test.tsx`, `InviteMemberModal.test.tsx`) all query by label, so none should notice.

- [ ] **Step 5: Browser check**

At 375, open one modal from each family — a Transaction modal, a Bill modal, and Settings' Invite-member modal — and confirm the paired fields stack and every input is reachable. At 1440, all three still show two columns.

- [ ] **Step 6: Commit**

```bash
git add web/src
git commit -m "refactor(web): extract FieldPair, and stack modal field pairs on small screens"
```

---

### Task 8: The data-dense screens

The three places where content, not the shell, is the constraint. This task needs real data, so it starts by creating some.

**Files:**
- Modify: `web/src/features/money/TransactionFilters.tsx` (the filter row), `web/src/features/money/BillRow.tsx` (the action cluster), `web/src/features/money/BudgetCategoryGrid.tsx:74`
- Check, change only if it fails: `web/src/features/money/BudgetHistoryModal.tsx:192` (`grid-cols-3`), `BudgetPage.tsx:405` and `BillsPage.tsx:399` (`lg:grid-cols-[1.7fr_1fr]`)

- [ ] **Step 1: Create data to measure against**

Through the running app at 1440: add one account, three transactions in the current month, one budget with at least four categories, and two bills (one paid this month, one upcoming). The seed creates only the household and its users, so none of this exists yet, and every empty state hides the layout this task is about.

- [ ] **Step 2: Measure at 375 before changing anything**

For each of `/money/transactions`, `/money/budget` and `/money/bills`, record which elements overflow:

```js
[...document.querySelectorAll('main *')]
  .filter((el) => el.scrollWidth > el.clientWidth + 2)
  .map((el) => ({ cls: el.className.toString().slice(0, 70), sw: el.scrollWidth, cw: el.clientWidth }))
```

Write the list down. It decides how much of the rest of this task is needed — the spec's expectation is the filter row and the bill action cluster, and anything else found here is new information worth recording in `LEARNING.md`.

- [ ] **Step 3: Fix the filter row**

`TransactionFilters.tsx:143` already wraps the row (`flex flex-wrap items-end gap-2.5`). What does not fit is each control: the four filters are each a bare `<div className="flex flex-col gap-1">` holding a label and a `SELECT_CLASS` select, and neither has any width bound — a long account nickname widens its select until the row overflows.

Add a field-wrapper constant beside the two that already exist at the top of the file, and give the selects a width that follows it:

```tsx
const SELECT_CLASS =
  "w-full rounded-lg border border-hairline bg-card px-3 py-1.5 text-[12.5px] text-label sm:w-auto";
// Each filter shares the row's width on a phone rather than sizing to its
// widest option -- an account nicknamed "Joint everyday account" would
// otherwise widen its select until the whole row overflowed 375px. `min-w-0`
// is what actually permits the shrink: a flex item's default `min-width:auto`
// refuses to go below its content.
const FIELD_CLASS = "flex min-w-0 flex-1 flex-col gap-1 sm:flex-none";
```

Then replace each of the four `<div className="flex flex-col gap-1">` wrappers (Account, Category, Paid by, Month) with `<div className={FIELD_CLASS}>`.

Leave the `<fieldset>`/`<legend>` Kind control exactly as it is — three short pills that already fit. It is also the control the product owner restored in review after a first draft made it a `<select>` (see the file's header comment); a mobile change must not quietly undo that.

- [ ] **Step 4: Fix the bill rows' action clusters**

`BillRow.tsx` has **two** rows, and both end in the same `<div className="flex items-center gap-3">`:

- `PaymentRow` (a paid bill): "Paid" label, a `w-20` amount, an Undo button.
- `BillRow`'s own upcoming row: an Overdue or Autopay pill, a `w-20` amount, and the actions after it.

Give that cluster — in both places — `flex flex-col items-end gap-1 sm:flex-row sm:items-center sm:gap-3`. The right-hand items then stack under one another on a phone instead of competing with the name block for a 343px row.

Both rows keep their shape: date badge and name on the left, amounts and actions on the right. This wraps a cluster; it does not restructure either row into a card, which the spec rules out.

- [ ] **Step 5: Check the grids**

`BudgetCategoryGrid.tsx:74` is already `grid-cols-1 sm:grid-cols-2` and should need nothing. `BudgetHistoryModal.tsx:192`'s `grid-cols-3` holds three small stat tiles inside a 343px panel — measure before touching it; three tiles of ~105px may well be fine, and changing it would be a change with no defect behind it.

`lg:grid-cols-[1.7fr_1fr]` on `BudgetPage.tsx:405` and `BillsPage.tsx:399`: at 1024 the content column is 788px, which arithmetic splits into roughly 441px and 259px. Now that a budget exists, measure it. If the 259px column is too narrow, raise **that grid's** breakpoint to `xl` — do not move the shell's `lg`, which is load-bearing for navigation.

- [ ] **Step 6: Lint and test**

Run: `make lint && make test-web`
Expected: PASS. `TransactionsPage.test.tsx` and `BillsPage.test.tsx` query by role and label and should not notice.

- [ ] **Step 7: Browser check**

Re-run Step 2's overflow query at 375 on all three pages. Expected: empty arrays. At 1440, compare against the Task 3 baseline.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/money
git commit -m "fix(web): wrap the filter row and the bill action cluster on small screens"
```

---

### Task 9: Touch targets

The one task that changes desktop rendering on purpose, which is why it is separated from the layout work.

**Files:**
- Modify: `web/src/features/shell/Sidebar.tsx:172` (sign-out, 24px), `web/src/components/Modal.tsx:146` (close, 28px), `web/src/features/money/BudgetModal.tsx:594` (28px), and the `py-1.5` controls listed below

- [ ] **Step 1: Inventory**

```bash
cd web/src && grep -rEn 'h-6 w-6|h-7 w-7|py-1\.5' --include='*.tsx' . | grep -v test
```

Expected around 18 lines. Not every one is a control — `Sidebar.tsx:132` and `:156` are a logo square and an avatar, and neither is pressable. Only interactive elements get the floor.

- [ ] **Step 2: Raise the icon buttons**

All three go to `h-11 w-11` on phones and keep their current size at the width where a pointer takes over — **but not all at the same breakpoint**:

- `Sidebar.tsx:172` (sign-out) → `h-11 w-11 lg:h-6 lg:w-6`. **`lg`, not `sm`**, because this button lives inside the drawer and the drawer switches at `lg`. Restoring it at `sm` would give a 768px tablet — still driving the touch drawer — a 24px target.
- `Modal.tsx:146` → `h-11 w-11 sm:h-7 sm:w-7`.
- `BudgetModal.tsx:594` → `h-11 w-11 sm:h-7 sm:w-7`.

The two modal buttons are defensible at `sm` because modals are not tied to the shell switch; the sidebar's is tied to it by construction.

This is the only task that edits `Sidebar.tsx`, and it is worth saying why that does not contradict the spec's decision 2: that decision forbids a *second instance* and any change to the testids its existing tests query. A size class on the sign-out button is neither.

This is the honest reading of "desktop does not move": the floor is a phone requirement, and applying it at every width would resize three desktop controls for no reason.

- [ ] **Step 3: Raise the padded controls**

For each interactive `py-1.5` from Step 1 — `TransactionFilters.tsx:33` and `:73`, `CurrencyPanel.tsx:148,153,159`, `NewSpaceModal.tsx:139,149,165`, `BudgetRolloverCard.tsx:265,274`, `GoalContributionsPanel.tsx:114,122`, `FinancesPage.tsx:69`, `TransactionsPage.tsx:459`, `GoalModal.tsx:484`, `BillModal.tsx:525` — use `py-2.5 sm:py-1.5`, which clears 44px with the existing text size on a phone and leaves desktop as it was.

- [ ] **Step 4: Lint and test**

Run: `make lint && make test-web`
Expected: PASS.

- [ ] **Step 5: Browser check**

At 375, measure a sample with `getBoundingClientRect()`: the sidebar's sign-out inside the open drawer, a modal's close button, and one filter select. All at least 44px tall. At 1440, compare against the Task 3 baseline — identical.

- [ ] **Step 6: Commit**

```bash
git add web/src
git commit -m "fix(web): give phone-sized touch targets a 44px floor"
```

---

### Task 10: The width matrix, and the documents

CLAUDE.md's definition of done. A defect nobody wrote down gets rebuilt.

**Files:**
- Modify: `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`

- [ ] **Step 1: Walk the width matrix**

At 320, 375, 414, 768, 1024 and 1440, walk sign-in, sign-up, Overview, Finances, Transactions, Budget, Goals, Bills, Settings, and one modal from each family. At every width, on every screen, check `document.documentElement.scrollWidth <= document.documentElement.clientWidth`.

Record the result as a table. A failure here is a bug to fix, not a note to write.

- [ ] **Step 2: Update SYSTEM_DESIGN.md**

Add the shell's responsive layout to the component diagram: `MobileTopBar` and `NavDrawer` alongside `AppShell` and `Sidebar`, with the `lg` switch marked. In the prose beneath it, record the two-breakpoint convention (`sm` reflow, `lg` shell) and the reason `NavDrawer` uses `lg:contents` — that is exactly the non-obvious reasoning the prose is for.

- [ ] **Step 3: Update FEATURE_TRACKER.md**

Add a row for mobile responsiveness under the appropriate section, marked ✅ (or 🟡 with the gap named, if the matrix turned up something deferred). **Then recount the summary table at the top** — CLAUDE.md requires its columns to sum to the stated totals, and adding a row without recounting leaves them wrong.

- [ ] **Step 4: Update LEARNING.md**

At minimum, three entries:

1. **A fixed-width shell made every page look broken.** The symptom was "the UI is not mobile friendly"; the cause was one class, `grid-cols-[236px_1fr]`, leaving `<main>` 124px wide on a 375px screen. What would have caught it sooner: opening the app at a phone width once, at any point in the previous nine slices.
2. **jsdom cannot see a breakpoint.** 60 frontend test files, all green, on a product that could not be used on a phone. Layout claims need a browser; the test suite is not evidence about layout.
3. **`100vh` is a lie on iOS Safari.** Every measurement in the design came from headless Chrome resized narrow, which has no URL bar to get wrong. Found by review, not by testing — worth remembering as a class of defect the tooling in use cannot reach.

If any of these matches an existing pattern section, add it there as evidence rather than opening a new section — the repetition is the point.

- [ ] **Step 5: Full green**

Run: `make lint && make test`
Expected: PASS, including the Go suite. Needs the Docker socket exports at the top of this plan.

- [ ] **Step 6: Commit**

```bash
git add docs
git commit -m "docs: record the mobile work, and what the fixed-width shell taught"
```

---

## Self-review

**Spec coverage.** Decision 1 → Tasks 1–3. Decision 2 (one `Sidebar`) → Task 3, Step 1's third test. Decision 3 (CSS drawer, hand-written Escape/focus/inert) → Tasks 2–3. Decision 4 (`PageContainer`, `FieldPair`) → Tasks 6–7. Decision 5 (two breakpoints) → Global Constraints. Decision 6 (375/320) → Task 10's matrix. Decision 7 (`dvh`) → Tasks 2, 3 and 4. Decision 8 (44px) → Task 9. "What is not changing" → stated in File Structure and re-stated in Tasks 7 and 8. Testing section → Tasks 1–3 plus every task's browser step. Slice 4's dense screens → Task 8. Documentation → Task 10.

**Type consistency.** `MobileTopBar({ onOpenNav })` is defined in Task 1 and consumed under that exact name in Task 3. `NavDrawer({ open, onClose, children })` likewise. `data-testid="nav-drawer"` and `data-testid="nav-drawer-backdrop"` are introduced in Task 2 and queried in Tasks 2 and 3 under those spellings. `PageContainer` forwards `...rest` because Task 6 depends on `data-testid` reaching the DOM.

**Corrected in review, worth knowing about before you read the code.** The first draft of Task 2 hid the closed drawer with `invisible` and asserted that `lg:contents` made an `lg:` counterpart unnecessary. It does not: `visibility` is an inherited property, `display: contents` suppresses the box rather than the inheritance, and the desktop sidebar — whose normal state is *closed* — would have inherited `visibility: hidden` and rendered as an invisible 236px column. Hence `lg:visible` on the closed branch, and the explicit `getComputedStyle` check in Task 3.

**Known open item, deliberately left to the executor.** `lg:contents` on `NavDrawer` is what guarantees desktop parity, and it is the one construct here that a browser could surprise us on. Task 3's Step 6 is where that surfaces. If it misbehaves, the fallback is a `lg:h-full` on the drawer panel plus `flex-1` on `Sidebar`'s `<nav>` — a change the spec's decision 2 permits (no second instance, no testid change) but which must be recorded in `SYSTEM_DESIGN.md` rather than made silently.
