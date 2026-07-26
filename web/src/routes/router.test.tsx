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
import { describe, expect, it } from "vitest";
import type { Me } from "../features/auth/schemas";
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

function renderApp(initialPath: string) {
  const testRouter = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return {
    ...render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={testRouter} />
      </QueryClientProvider>,
    ),
    router: testRouter,
  };
}

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
});
