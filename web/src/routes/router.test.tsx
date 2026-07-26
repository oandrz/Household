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
import { afterEach, describe, expect, it } from "vitest";
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
