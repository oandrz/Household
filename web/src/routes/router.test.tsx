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
import { render, screen, waitFor } from "@testing-library/react";
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
    expect(await screen.findByText("Arriving in slice 5.")).toBeInTheDocument();
  });

  it("redirects a member without the marriage capability away from /marriage", async () => {
    // A limited member with calendar/chores/money but not marriage --
    // domain.NewMembership never grants a limited member CapMarriage, so this
    // is the real shape a "Kid" membership takes, not a contrived one.
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
    expect(await screen.findByText("Arriving in slice 5.")).toBeInTheDocument();
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
    expect(await screen.findByText("Arriving in slice 5.")).toBeInTheDocument();
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
    expect(await screen.findByText("Arriving in slice 5.")).toBeInTheDocument();
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
        },
      },
    });

    const { router } = renderApp("/money/budget");

    expect(await screen.findByTestId("budget-page")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/money/budget");
    // Real timers restored in this file's shared afterEach below.
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
});
