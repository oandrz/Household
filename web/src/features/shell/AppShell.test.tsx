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
    // AppShell's `!me.data` loading branch also renders a <main>. Waiting on
    // this button first guarantees the query has resolved and the <main> we
    // then grab is the loaded one, not a detached node the loading branch
    // already unmounted.
    await screen.findByRole("button", { name: "Open navigation" });
    const main = screen.getByRole("main");

    expect(main.hasAttribute("inert")).toBe(false);

    openNav();
    await waitFor(() => expect(main.hasAttribute("inert")).toBe(true));

    // The skip link targets <main>, which is inert while the drawer covers
    // it -- rendering the link here would point a keyboard user at content
    // they cannot reach, a link that visibly does nothing. So it is removed
    // for exactly as long as <main> is inert, not merely hidden.
    expect(screen.queryByRole("link", { name: /skip to content/i })).toBeNull();

    fireEvent.click(screen.getByTestId("nav-drawer-backdrop"));
    await waitFor(() => expect(main.hasAttribute("inert")).toBe(false));
    expect(screen.getByRole("link", { name: /skip to content/i })).toBeInTheDocument();
  });

  it("closes the drawer on navigation", async () => {
    const { router } = renderShell();
    await screen.findByRole("button", { name: "Open navigation" });

    openNav();
    await waitFor(() => expect(screen.getByTestId("nav-drawer-backdrop")).toBeInTheDocument());

    // Navigated programmatically, not by clicking the link in <main>: with
    // the drawer open <main> is inert, so in a real browser that link cannot
    // be reached at all. jsdom does not implement inert, so clicking it would
    // pass on an interaction that cannot happen. The effect is keyed on the
    // pathname precisely so a route change from anywhere closes the drawer,
    // and this is that claim.
    //
    // `router.navigate`'s `to` param is typed against the app's globally
    // `Register`ed router (src/routes/router.tsx), not against this test's
    // own local route tree -- a router-core limitation: `Router.navigate`'s
    // field type defaults its generic to `RegisteredRouter` regardless of
    // instance, so "/elsewhere" (real on *this* router, at runtime) would
    // not typecheck via `.navigate()`. `history.push` takes a plain
    // `string`, so it drives the same route change without that fight.
    router.history.push("/elsewhere");

    await waitFor(() => expect(screen.queryByTestId("nav-drawer-backdrop")).toBeNull());
  });

  it("renders exactly one sidebar", async () => {
    renderShell();
    await screen.findByRole("button", { name: "Open navigation" });

    // Money expands to five links and no more; a second Sidebar instance
    // would double every one of them.
    const links = await screen.findAllByTestId("sidebar-space");
    expect(links).toHaveLength(5);
  });

  it("puts a skip link ahead of the navigation, pointing at the page content", async () => {
    renderShell();

    const skip = await screen.findByRole("link", { name: /skip to content/i });

    // Ahead of the nav in DOM order, which is what makes it the first thing a
    // keyboard reaches -- a skip link rendered after the nav it skips is
    // decoration.
    const nav = screen.getByRole("navigation");
    expect(skip.compareDocumentPosition(nav) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    expect(skip).toHaveAttribute("href", "#main-content");
    const main = document.querySelector("#main-content");
    expect(main?.tagName).toBe("MAIN");

    // <main> is not natively focusable, so a hash jump alone moves focus
    // there only in browsers that opt to (Chrome does; Safari and Firefox
    // have long histories of just scrolling). tabindex="-1" makes it a
    // valid focus target without adding it to the tab order -- "-1", not
    // "0", because "0" would add a stop to every page instead of removing
    // the eight this link exists to skip.
    expect(main).toHaveAttribute("tabindex", "-1");
  });
});
