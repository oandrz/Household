// The operator header's nav, specifically: the shape that lets it carry a
// growing number of links without ever forcing the page wider than the
// viewport. jsdom does not run a real layout engine, so this file cannot
// assert "no horizontal overflow at 305px" the way the browser walk did
// (docs/superpowers/plans/2026-09-04-hearth-outbound-inspector-verification.md,
// criterion 14) -- what it can assert, honestly, is the mechanism that walk
// found missing: the nav must be allowed to wrap onto a second line rather
// than being a fixed-width, single-line row. Losing `flex-wrap` here is
// exactly the change that reintroduces the overflow a real browser at 305px
// would show and this test cannot.
import { render, screen } from "@testing-library/react";
import { QueryClientProvider, QueryClient } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { describe, expect, it } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AdminShell } from "./AdminShell";

function renderShell() {
  stubFetchRoutes({
    "GET /api/v1/admin/flags": { status: 200, body: { flags: [] } },
  });

  const rootRoute = createRootRoute({ component: AdminShell });
  const flagsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/admin/flags",
    component: () => <p>Flags</p>,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([flagsRoute]),
    history: createMemoryHistory({ initialEntries: ["/admin/flags"] }),
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

describe("AdminShell's operator nav", () => {
  it("renders Flags, Mail and Households, in that order, plus Back to Hearth", async () => {
    renderShell();

    const nav = await screen.findByRole("navigation", { name: "Operator" });
    const labels = [...nav.querySelectorAll("a")].map((a) => a.textContent);

    // Source order in AdminShell.tsx's own `items` array -- this is the
    // order the browser walk's criterion 2 confirmed the running app
    // actually shows, settling the one place the task's own framing
    // disagreed with itself about which order was correct.
    expect(labels).toEqual(["Flags", "Mail", "Households", "Back to Hearth"]);
  });

  it("lets the nav wrap onto a second line instead of forcing the page wider than the viewport", async () => {
    renderShell();

    const nav = await screen.findByRole("navigation", { name: "Operator" });

    // The fix for criterion 14: before it, this nav had only `flex` with a
    // fixed `gap-4`, which fit at every width the admin-households walk
    // tried (320px) but not the 305px this task's own walk was asked to
    // check -- adding a third link (`Mail`) was what pushed it over. This
    // assertion is the tripwire for that regression coming back: with
    // `flex-wrap` in place, a narrow viewport wraps the row instead of
    // overflowing it, and this class is what makes that possible.
    expect(nav.className).toContain("flex-wrap");
  });
});
