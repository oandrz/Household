// Fix round 1, Finding 4: every other test in this codebase mounts a
// throwaway single-route harness (renderWithRouter), which exercises
// individual components but never the real route tree -- RequireAuth's
// redirect, RequireCapability's redirect, the splat routes, and the
// /sign-in -> / redirect (the actual navigation bug this task found and
// fixed) all shipped with no automated coverage over the real routing
// wiring, only a manual browser walkthrough. This mounts `routeTree` itself
// (exported from ./router alongside the `router` singleton, specifically so
// a test can build its own instance with a fresh memory history and
// QueryClient rather than sharing the app's one registered router).
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { setUnauthorizedHandler } from "../api/client";
import { createUnauthorizedHandler } from "../api/unauthorizedRedirect";
import type { InvitePreview, Me } from "../features/auth/schemas";
import { stubFetchRoutes } from "../test/fetchStub";
import { routeTree } from "./router";

function meFixture(overrides: Partial<Me> = {}): Me {
  return {
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
      capabilities: ["calendar", "chores", "money", "marriage"],
    },
    capabilities: ["calendar", "chores", "money", "marriage"],
    spaces: [],
    isPlatformAdmin: false,
    features: {},
    ...overrides,
  };
}

const NO_SESSION = {
  status: 401,
  body: { error: { code: "UNAUTHENTICATED", message: "Sign in required." } },
};

function invitePreviewFixture(overrides: Partial<InvitePreview> = {}): InvitePreview {
  return {
    householdName: "Andreas & Christine",
    inviterName: "Andreas",
    name: "Christine",
    role: "owner",
    capabilities: ["calendar", "chores", "money", "marriage"],
    ...overrides,
  };
}

// renderApp wires setUnauthorizedHandler with the *real* factory
// (createUnauthorizedHandler), against this test's own router and
// QueryClient, exactly as main.tsx wires it against the app's singletons.
// Fix round 4 wired apiFetch's 401 reaction, but every test that exercised
// it installed a stub handler instead of this one, and every test in this
// file predates the handler entirely -- the one seam that regression
// introduced was the only seam nothing here exercised. Wiring the real
// handler into this harness is what makes
// "does not bounce the invite screen off an expected GET /auth/me 401"
// below an actual regression test, not a hand check of the underlying
// logic in isolation.
function renderApp(initialPath: string) {
  const testRouter = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  setUnauthorizedHandler(createUnauthorizedHandler(testRouter, queryClient));
  return {
    ...render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={testRouter} />
      </QueryClientProvider>,
    ),
    router: testRouter,
  };
}

afterEach(() => {
  // setUnauthorizedHandler is module-level state in client.ts -- a handler
  // built against one test's throwaway router/QueryClient must not survive
  // into the next test, where it would hold a reference to an unmounted
  // router.
  setUnauthorizedHandler(null);
  // Real timers regardless of how the Budget test's own vi.useFakeTimers()
  // above finished -- a thrown assertion partway through would otherwise
  // leave every later test in this file running against a frozen clock.
  vi.useRealTimers();
});

describe("the real route tree", () => {
  it("redirects an unauthenticated visit to / to /sign-in", async () => {
    stubFetchRoutes({ "GET /api/v1/auth/me": NO_SESSION });

    const { router } = renderApp("/");

    await waitFor(() => expect(router.state.location.pathname).toBe("/sign-in"));
    expect(await screen.findByText("Welcome back.")).toBeInTheDocument();
  });

  it("redirects a signed-in visit to /sign-in to /", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
    });

    const { router } = renderApp("/sign-in");

    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    // The <h1>, not a bare text match: the sidebar carries its own
    // "Overview" link, so an unscoped query for that word matches two
    // elements and throws. What this pins is unchanged -- that the
    // redirect landed on / and / rendered its own page.
    expect(
      await screen.findByRole("heading", { level: 1, name: "Overview" }),
    ).toBeInTheDocument();
  });

  // Pins router.tsx's header comment: rootRoute's notFoundComponent sits
  // above authenticatedRoute/shellRoute in this tree, so a deleted space's
  // URL with genuinely no route anywhere in the tree falls through to it
  // rather than redirecting anywhere -- unlike every route still in the
  // tree, which either mounts or bounces to /sign-in or /. Task 10 gave
  // Marriage's own "/marriage" a real matching route (marriageGuardRoute),
  // so it no longer demonstrates this -- /family/calendar does instead:
  // Family's own placeholder was deleted in the same commit as Marriage's
  // (110ab0a) and nothing has rebuilt it. No session is stubbed at all
  // here, deliberately: if RequireAuth ran for this path the way it does
  // for every real route, an unauthenticated visit would redirect to
  // /sign-in and this test would time out waiting for a pathname that never
  // arrives. That it doesn't redirect is exactly the second consequence the
  // comment calls out, and the one most likely to surprise someone who
  // moves notFoundComponent under the shell later.
  it("leaves a deleted space's URL on the not-found page instead of redirecting", async () => {
    stubFetchRoutes({ "GET /api/v1/auth/me": NO_SESSION });

    const { router } = renderApp("/family/calendar");

    expect(await screen.findByText("Page not found.")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/family/calendar");
  });

  // Task 17: /money/transactions has to sit under moneyGuardRoute, not hung
  // off the shell beside it -- otherwise a member without the money
  // capability reaches a ledger because the guard that would have refused
  // them never ran. If the route were hung directly off shellRoute instead
  // (a literal path always wins over moneySplatRoute's "$" catch-all, so
  // that wrong wiring would still be the one TanStack Router picks),
  // RequireCapability would never run at all: TransactionsPage would mount
  // unconditionally, hit this test's deliberately-thin stub set (no
  // /api/v1/transactions, /accounts, etc.), its queries would error, and
  // router.state.location.pathname would still read "/money/transactions"
  // when the waitFor below times out -- a real failure, not a passthrough.
  //
  // On its own, though, this test is not proof the dedicated route exists at
  // all: today, before Step 3 adds it, "/money/transactions" already falls
  // through to moneySplatRoute (also a child of moneyGuardRoute), which is
  // gated by the exact same RequireCapability parent and therefore redirects
  // identically. That is what the next test, the one asserting a caller *with*
  // money capability actually reaches TransactionsPage rather than the Money
  // placeholder, is for -- it is the one that genuinely fails until Step 3.
  it("redirects a member without the money capability away from /money/transactions", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meFixture({
          membership: {
            id: "membership-2",
            householdId: "household-1",
            userId: "user-2",
            role: "limited",
            capabilities: ["calendar", "chores"],
          },
          capabilities: ["calendar", "chores"],
        }),
      },
    });

    const { router } = renderApp("/money/transactions");

    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    // The <h1>, not a bare text match: the sidebar carries its own
    // "Overview" link, so an unscoped query for that word matches two
    // elements and throws. What this pins is unchanged -- that the
    // redirect landed on / and / rendered its own page.
    expect(
      await screen.findByRole("heading", { level: 1, name: "Overview" }),
    ).toBeInTheDocument();
  });

  // The positive counterpart the test above needs: a caller who *does* carry
  // the money capability must actually land on TransactionsPage at
  // /money/transactions, not the Money slice's own placeholder -- today (and
  // until Step 3 adds moneyTransactionsRoute) that path falls through to
  // moneySplatRoute's "Arriving in slice 2" placeholder instead, so this is
  // the one of the pair that genuinely fails before the route exists.
  it("mounts the Transactions page at /money/transactions for a caller who has the money capability", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": {
        status: 200,
        body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
      },
      "GET /api/v1/categories": { status: 200, body: { categories: [] } },
      "GET /api/v1/household/members": { status: 200, body: [] },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [] } },
      "GET /api/v1/transactions": {
        status: 200,
        body: {
          transactions: [],
          nextCursor: null,
          summary: { currency: "SGD", month: "2026-07", count: 0, spentMinor: 0, excludedNoRate: [] },
        },
      },
    });

    const { router } = renderApp("/money/transactions");

    expect(await screen.findByText("All transactions")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/money/transactions");
  });

  // Task 11's analogue of the transactions redirect above: moneyBudgetRoute
  // has to sit under moneyGuardRoute too, for the same reason -- a member
  // without money must never reach the Budget screen.
  it("redirects a member without the money capability away from /money/budget", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meFixture({
          membership: {
            id: "membership-2",
            householdId: "household-1",
            userId: "user-2",
            role: "limited",
            capabilities: ["calendar", "chores"],
          },
          capabilities: ["calendar", "chores"],
        }),
      },
    });

    const { router } = renderApp("/money/budget");

    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    // The <h1>, not a bare text match: the sidebar carries its own
    // "Overview" link, so an unscoped query for that word matches two
    // elements and throws. What this pins is unchanged -- that the
    // redirect landed on / and / rendered its own page.
    expect(
      await screen.findByRole("heading", { level: 1, name: "Overview" }),
    ).toBeInTheDocument();
  });

  // The positive counterpart the redirect test above needs -- and the one
  // that actually proves moneyBudgetRoute exists. Without it, the redirect
  // test alone would still pass with no dedicated route at all: /money/budget
  // falls through to moneySplatRoute, a sibling under the exact same
  // RequireCapability parent, which redirects identically. This is the test
  // that fails until moneyBudgetRoute is added to addChildren.
  it("mounts the Budget page at /money/budget for a caller who has the money capability", async () => {
    // Task 12 wired BudgetPage to useBudget/useCurrencies, so this now needs
    // both stubbed -- BudgetPage.tsx's own currentMonth() reads the real
    // calendar, so `Date` is faked to a fixed July 2026 day first (the same
    // `toFake: ["Date"]` convention AccountModal.test.tsx's own today()
    // tests use) to pin which month it requests.
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-07-15T12:00:00Z"));
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": {
        status: 200,
        body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
      },
      "GET /api/v1/budgets/2026-07": {
        status: 200,
        body: {
          currency: "SGD",
          month: "2026-07",
          budget: null,
          categories: [],
          budgetedMinor: 0,
          spentMinor: 0,
          remainingMinor: 0,
          percentUsed: 0,
          percentOk: false,
          daysLeft: 13,
          dailyPaceMinor: 0,
          dailyPaceOk: false,
          byPerson: [],
          excludedNoRate: 0,
          overCount: 0,
          rolledOverAt: null,
          rolloverGoalId: null,
          rolloverAmountMinor: null,
        },
      },
    });

    const { router } = renderApp("/money/budget");

    expect(await screen.findByTestId("budget-page")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/money/budget");
    // Real timers restored in this file's shared afterEach below.
  });

  // Task 11's own analogue of the transactions/budget redirect tests above:
  // moneyGoalsRoute has to sit under moneyGuardRoute too, for the same
  // reason -- a member without money must never reach the Goals screen. On
  // its own this test cannot prove the dedicated route exists (before
  // moneyGoalsRoute existed at all, "/money/goals" already fell through to
  // moneySplatRoute -- a sibling gated by the exact same RequireCapability
  // parent, which redirects identically) -- the positive counterpart right
  // below is what actually proves it, the same pairing moneyBudgetRoute's
  // two tests use.
  it("redirects a member without the money capability away from /money/goals", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meFixture({
          membership: {
            id: "membership-2",
            householdId: "household-1",
            userId: "user-2",
            role: "limited",
            capabilities: ["calendar", "chores"],
          },
          capabilities: ["calendar", "chores"],
        }),
      },
    });

    const { router } = renderApp("/money/goals");

    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    // The <h1>, not a bare text match: the sidebar carries its own
    // "Overview" link, so an unscoped query for that word matches two
    // elements and throws. What this pins is unchanged -- that the
    // redirect landed on / and / rendered its own page.
    expect(
      await screen.findByRole("heading", { level: 1, name: "Overview" }),
    ).toBeInTheDocument();
  });

  // The positive counterpart the redirect test above needs -- and the one
  // that actually proves moneyGoalsRoute exists and mounts the real
  // GoalsPage, not moneySplatRoute's placeholder. This is the test Task 10's
  // own comment named as the one nothing would catch the placeholder swap
  // being forgotten without (task-11-brief.md's carry-forward).
  it("mounts the Goals page at /money/goals for a caller who has the money capability", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/currencies": {
        status: 200,
        body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
      },
      "GET /api/v1/goals": {
        status: 200,
        body: {
          currency: "SGD",
          goals: [],
          summary: {
            plannedMonthlyTotalMinor: 0,
            actualThisMonthMinor: 0,
            onTrackCount: 0,
            datedCount: 0,
            noDateCount: 0,
            excludedNoRate: 0,
            nextGoal: null,
          },
        },
      },
    });

    const { router } = renderApp("/money/goals");

    expect(await screen.findByTestId("goals-page")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/money/goals");
  });

  // Bills' own Task 11: moneyGuardRoute has to sit above /money/bills too,
  // the same reason every other /money/* route above pins it -- a member
  // without money must never reach the Bills screen.
  it("redirects a member without the money capability away from /money/bills", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meFixture({
          membership: {
            id: "membership-2",
            householdId: "household-1",
            userId: "user-2",
            role: "limited",
            capabilities: ["calendar", "chores"],
          },
          capabilities: ["calendar", "chores"],
        }),
      },
    });

    const { router } = renderApp("/money/bills");

    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    // The <h1>, not a bare text match: the sidebar carries its own
    // "Overview" link, so an unscoped query for that word matches two
    // elements and throws. What this pins is unchanged -- that the
    // redirect landed on / and / rendered its own page.
    expect(
      await screen.findByRole("heading", { level: 1, name: "Overview" }),
    ).toBeInTheDocument();
  });

  // The positive counterpart the redirect test above needs -- and the one
  // that actually proves moneyBillsRoute exists and mounts the real
  // BillsPage, not moneySplatRoute's placeholder (deleted in this same
  // task -- router.tsx's own header comment named Bills as the splat's last
  // remaining reason to exist).
  it("mounts the Bills page at /money/bills for a caller who has the money capability", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      // BillsPage fetches accounts of its own: a bill is paid FROM one, so
      // with none both Add-bill entry points are disabled (BillsPage.tsx's
      // own comment). Registered here so this mount exercises the ordinary
      // path -- stubFetchRoutes throws on an unregistered request, and
      // react-query would swallow that throw into `accounts.isError`,
      // leaving the test silently green against a state it is not about.
      "GET /api/v1/accounts": { status: 200, body: { accounts: [] } },
      "GET /api/v1/bills": {
        status: 200,
        body: {
          bills: [],
          paidThisMonth: [],
          summary: {
            currency: "SGD",
            dueThisMonthMinor: 0,
            paidSoFarMinor: 0,
            nextDue: null,
            autopayCount: 0,
            billCount: 0,
            subscriptionsMonthlyMinor: 0,
            subscriptionsAnnualMinor: 0,
            excludedNoRate: 0,
          },
        },
      },
    });

    const { router } = renderApp("/money/bills");

    expect(await screen.findByTestId("bills-page")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/money/bills");
  });

  // Task 10: Marriage's own route came back in the same change as its
  // SPACE_PAGES entry and RequireCapability guard (router.tsx's own header
  // comment on why splitting them across tasks isn't safe) -- a member
  // without the marriage capability must never reach the Retros screen, the
  // same reasoning every /money/* redirect test above pins for money.
  it("redirects a member without the marriage capability away from /marriage/retros", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meFixture({
          membership: {
            id: "membership-2",
            householdId: "household-1",
            userId: "user-2",
            role: "limited",
            capabilities: ["calendar", "chores"],
          },
          capabilities: ["calendar", "chores"],
        }),
      },
    });

    const { router } = renderApp("/marriage/retros");

    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(
      await screen.findByRole("heading", { level: 1, name: "Overview" }),
    ).toBeInTheDocument();
  });

  // The positive counterpart the redirect test above needs -- and the one
  // that actually proves marriageRetrosRoute exists and mounts the real
  // RetrosPage, not a 404: before Step 3 of task-10-brief.md added it,
  // "/marriage/retros" fell straight through to rootRoute's
  // notFoundComponent (router.tsx's own header comment, pre-task-10 version).
  it("mounts the Retros page at /marriage/retros for a caller who has the marriage capability", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/retros": {
        status: 200,
        body: { retros: [], mood: [], doneCount: 0, since: null, startMonth: "2026-08" },
      },
    });

    const { router } = renderApp("/marriage/retros");

    expect(await screen.findByTestId("retros-page")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/marriage/retros");
  });

  // Task 11's own analogue of the retros redirect test above: marriageVisionRoute
  // sits under marriageGuardRoute too, so a member without the marriage
  // capability must never reach the Vision screen either.
  it("redirects a member without the marriage capability away from /marriage/vision", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meFixture({
          membership: {
            id: "membership-2",
            householdId: "household-1",
            userId: "user-2",
            role: "limited",
            capabilities: ["calendar", "chores"],
          },
          capabilities: ["calendar", "chores"],
        }),
      },
    });

    const { router } = renderApp("/marriage/vision");

    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(
      await screen.findByRole("heading", { level: 1, name: "Overview" }),
    ).toBeInTheDocument();
  });

  // The positive counterpart the redirect test above needs -- and the one
  // that actually proves marriageVisionRoute exists and mounts the real
  // VisionPage, not a 404.
  it("mounts the Vision page at /marriage/vision for a caller who has the marriage capability", async () => {
    // VisionPage.tsx's own currentVisionYear() reads the real calendar, so
    // `Date` is faked first (the Budget test's own `toFake: ["Date"]`
    // convention above) to pin which year it requests.
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-08-15T12:00:00Z"));
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/marriage/vision?year=2026": {
        status: 200,
        body: {
          vision: { year: 2026, theme: "", description: "", version: 0, pillars: [], milestones: [] },
        },
      },
    });

    const { router } = renderApp("/marriage/vision");

    expect(await screen.findByTestId("vision-page")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/marriage/vision");
    // Real timers restored in this file's shared afterEach below.
  });

  // Task 11 gave marriageGuardRoute a real index route -- before this, bare
  // "/marriage" matched marriageGuardRoute itself (a real route, not an
  // absence), ran RequireAuth and RequireCapability as normal, and then had
  // no child route left to render: AppShell's sidebar showed, but the
  // content area was blank rather than a page or a 404 (router.tsx's own
  // header comment has the full history, confirmed empirically with a
  // throwaway probe before task 10's own version of this test was written).
  // Marriage now has two pages, so the index redirects to the first --
  // moneyIndexRoute's own "first page wins" choice, restated here as an
  // actual redirect (Money's own index route IS its first page rather than
  // redirecting to a sibling, since Finances has no separate URL of its own
  // the way Retros does).
  it("redirects bare /marriage to /marriage/retros, its first page", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/retros": {
        status: 200,
        body: { retros: [], mood: [], doneCount: 0, since: null, startMonth: "2026-08" },
      },
    });

    const { router } = renderApp("/marriage");

    await waitFor(() => expect(router.state.location.pathname).toBe("/marriage/retros"));
    expect(await screen.findByTestId("retros-page")).toBeInTheDocument();
    expect(screen.queryByText("Page not found.")).not.toBeInTheDocument();
  });

  // marriageIndexRoute's own beforeLoad is unconditional -- it does not
  // check capability itself, RequireCapability.tsx's own comment on why
  // client-side gating is presentation only. So a member without marriage
  // transits /marriage/retros on the way through (beforeLoad resolves the
  // redirect before marriageGuardRoute's own component -- RequireCapability
  // -- ever mounts to block it) rather than being stopped at bare
  // "/marriage" itself. The destination is still correct: RequireCapability
  // runs against /marriage/retros exactly as the redirect test above pins
  // for a member who holds the capability, and bounces this one to / the
  // same as if they had typed /marriage/retros directly. Worth pinning on
  // its own -- this route shape (an index route's beforeLoad sitting inside
  // a capability-gated parent) is the same kind of ordering LEARNING.md
  // already found empirically surprising once for this exact route group
  // (the blank-content-area entry) rather than assumed from reading the
  // tree.
  it("still ends a marriage-less member at / when they type bare /marriage, via /marriage/retros", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meFixture({
          membership: {
            id: "membership-2",
            householdId: "household-1",
            userId: "user-2",
            role: "limited",
            capabilities: ["calendar", "chores"],
          },
          capabilities: ["calendar", "chores"],
        }),
      },
    });

    const { router } = renderApp("/marriage");

    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(
      await screen.findByRole("heading", { level: 1, name: "Overview" }),
    ).toBeInTheDocument();
  });

  // Fix round 5, Finding 1 (critical): this is the exact reproduction the
  // coordinator's reviewer used -- /invite/tok with GET /api/v1/auth/me
  // returning 401 (a genuine, signed-out invitee -- Christine from `make
  // seed` included) and the invite preview returning 200. Before the fix,
  // the 401 from useMe() inside InviteScreen fired the real
  // unauthorizedHandler, which cleared the cache and navigated to
  // /sign-in -- the acceptance form never rendered, for every invitee. This
  // asserts both halves of that: the form renders, and the path is still
  // the invite link, not /sign-in.
  it("does not bounce the invite screen off an expected GET /auth/me 401", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": NO_SESSION,
      "GET /api/v1/invites/some-invite-token": {
        status: 200,
        body: invitePreviewFixture(),
      },
    });

    const { router } = renderApp("/invite/some-invite-token");

    expect(
      await screen.findByText("Andreas invited you in."),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Accept & join household" }),
    ).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/invite/some-invite-token");
  });

  // The sign-in screen has the identical shape (useMe() checking "is
  // someone already signed in", reachable with no session at all) --
  // covered here too so the fix is pinned for both screens the finding
  // named, not just the invite one.
  it("does not bounce the sign-in screen off its own expected GET /auth/me 401", async () => {
    stubFetchRoutes({ "GET /api/v1/auth/me": NO_SESSION });

    const { router } = renderApp("/sign-in");

    expect(await screen.findByText("Welcome back.")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/sign-in");
  });

  // Task 11: /admin sits outside shellRoute (AppShell's sidebar must not
  // apply -- the admin surface has its own chrome), but still inside
  // authenticatedRoute -- moneyIndexRoute and marriageIndexRoute's own
  // "first page wins" redirect, restated for the admin subtree's one page.
  // AdminShell and AdminFlagsPage are both behind React.lazy
  // (adminBundleSplit.test.ts pins that this stays true at the source
  // level), so this is also the one test in this file that exercises their
  // Suspense boundaries -- a component test mounting AdminFlagsPage
  // directly never would.
  it("redirects a platform admin's bare /admin to /admin/flags", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture({ isPlatformAdmin: true }) },
      "GET /api/v1/admin/flags": { status: 200, body: { flags: [] } },
    });

    const { router } = renderApp("/admin");

    await waitFor(() => expect(router.state.location.pathname).toBe("/admin/flags"));
    expect(await screen.findByText("Hearth · Operator")).toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { level: 1, name: "Feature flags" }),
    ).toBeInTheDocument();
  });

  // Task 8/9: both new admin routes sit two pathless routes deep
  // (authenticatedRoute, then adminRoute's own path segment), the first
  // routes in this file to call useSearch/useParams with a `from` string
  // from that depth. tsc alone cannot prove the string used at runtime
  // actually resolves -- TanStack Router looks the id up in its live match
  // array too, and throws if it can't find it -- so only mounting the real
  // routeTree (not AdminHouseholdsPage/AdminHouseholdPage directly, which
  // every component test in AdminHouseholdsPage.test.tsx and
  // AdminHouseholdPage.test.tsx does, bypassing the route entirely) can
  // catch a mismatch between the two id spaces.
  it("mounts the households list at /admin/households, resolving useSearch's route id", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture({ isPlatformAdmin: true }) },
      "GET /api/v1/admin/flags": { status: 200, body: { flags: [] } },
      "GET /api/v1/admin/households?limit=50": {
        status: 200,
        body: {
          metrics: { households: 0, activeHouseholds7d: 0, signups30d: { requested: 0, completed: 0 }, pendingInvites: 0 },
          households: [],
          truncated: false,
        },
      },
    });

    renderApp("/admin/households");

    expect(await screen.findByRole("heading", { name: "Households" })).toBeInTheDocument();
  });

  // Show more's own doubling (AdminHouseholdsPage.tsx's onShowMore prop,
  // wired here in adminHouseholdsRoute) lives entirely in this file's
  // navigate() calls -- AdminHouseholdsPage itself only ever renders
  // whatever `limit` it is handed as a prop, so only a test that drives the
  // real route (not AdminHouseholdsPage.test.tsx's direct-prop rendering)
  // can see the URL actually change on each click.
  it("doubles the URL's limit search param on each Show more click, up to the 200 cap", async () => {
    const household = {
      id: "h1",
      name: "Oentoro",
      familyName: "Oentoro",
      memberCount: 4,
      createdAt: "2026-08-15T02:11:09Z",
      lastActiveAt: "2026-09-02T07:40:12Z",
      primaryCurrency: "SGD",
      match: null,
    };
    const metrics = {
      households: 1,
      activeHouseholds7d: 1,
      signups30d: { requested: 0, completed: 0 },
      pendingInvites: 0,
    };
    // truncated: true at every limit -- the fixture never claims to have
    // caught up with however many households actually exist, so the cap
    // (not "ran out of results") is the only reason Show more disappears at
    // 200.
    stubFetchRoutes({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meFixture({ isPlatformAdmin: true }),
      },
      "GET /api/v1/admin/flags": { status: 200, body: { flags: [] } },
      "GET /api/v1/admin/households?limit=50": {
        status: 200,
        body: { metrics, households: [household], truncated: true },
      },
      "GET /api/v1/admin/households?limit=100": {
        status: 200,
        body: { metrics, households: [household], truncated: true },
      },
      "GET /api/v1/admin/households?limit=200": {
        status: 200,
        body: { metrics, households: [household], truncated: true },
      },
    });

    const { router } = renderApp("/admin/households");

    fireEvent.click(await screen.findByRole("button", { name: "Show more" }));
    await waitFor(() =>
      expect((router.state.location.search as { limit: number }).limit).toBe(
        100,
      ),
    );

    fireEvent.click(await screen.findByRole("button", { name: "Show more" }));
    await waitFor(() =>
      expect((router.state.location.search as { limit: number }).limit).toBe(
        200,
      ),
    );

    expect(
      await screen.findByText("Showing the first 1 — search to narrow"),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Show more" }),
    ).not.toBeInTheDocument();
  });

  it("mounts the household drill-in at /admin/households/$householdId, resolving useParams's route id", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture({ isPlatformAdmin: true }) },
      "GET /api/v1/admin/flags": { status: 200, body: { flags: [] } },
      "GET /api/v1/admin/households/h1": {
        status: 200,
        body: {
          household: { id: "h1", name: "Oentoro", familyName: "Oentoro", createdAt: "2026-08-15T02:11:09Z", primaryCurrency: "SGD" },
          members: [],
          pendingInvites: [],
          lockout: null,
        },
      },
    });

    renderApp("/admin/households/h1");

    expect(await screen.findByRole("heading", { name: "Oentoro" })).toBeInTheDocument();
  });

  // requirePlatformAdmin answers 404, not 403, to an authenticated non-admin
  // (middleware_admin.go's own doc comment: "to everyone else the whole
  // surface must look like a typo") -- AdminGate's fail-closed default is
  // what turns that into the identical screen a genuinely unrouted URL
  // gets, chrome included: "Hearth · Operator" must not be on screen
  // alongside it, or the surface would be answering "yes, but not for you"
  // rather than looking like nothing is there at all.
  it("shows the app's ordinary not-found page to a non-admin visiting /admin/flags", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture({ isPlatformAdmin: false }) },
      "GET /api/v1/admin/flags": {
        status: 404,
        body: { error: { code: "NOT_FOUND", message: "That endpoint does not exist." } },
      },
    });

    const { router } = renderApp("/admin/flags");

    expect(await screen.findByText("Page not found.")).toBeInTheDocument();
    expect(screen.queryByText("Hearth · Operator")).not.toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/admin/flags");
  });
});
