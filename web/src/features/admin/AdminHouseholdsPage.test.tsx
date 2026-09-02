// Follows AdminFlagsPage.test.tsx: renderWithRouter plus stubFetchRoutes
// for every request, literal strings asserted.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AdminHouseholdsPage } from "./AdminHouseholdsPage";
import { adminHouseholdsPath } from "./useAdminDirectory";
import type {
  AdminHouseholdListing,
  AdminHouseholdsResponse,
} from "./adminDirectorySchemas";

function listing(
  overrides: Partial<AdminHouseholdListing> = {},
): AdminHouseholdListing {
  return {
    id: "h1",
    name: "Oentoro",
    familyName: "Oentoro",
    memberCount: 4,
    createdAt: "2026-08-15T02:11:09Z",
    lastActiveAt: "2026-09-02T07:40:12Z",
    primaryCurrency: "SGD",
    match: null,
    ...overrides,
  };
}

function response(
  households: AdminHouseholdListing[],
  truncated = false,
): AdminHouseholdsResponse {
  return {
    metrics: {
      households: households.length,
      activeHouseholds7d: 1,
      signups30d: { requested: 9, completed: 4 },
      pendingInvites: 2,
    },
    households,
    truncated,
  };
}

function renderPage(
  q = "",
  limit = 50,
  handlers: Partial<{
    onSearch: (q: string) => void;
    onShowMore: () => void;
  }> = {},
) {
  return renderWithRouter(
    <AdminHouseholdsPage
      q={q}
      limit={limit}
      onSearch={handlers.onSearch ?? vi.fn()}
      onShowMore={handlers.onShowMore ?? vi.fn()}
    />,
  );
}

describe("AdminHouseholdsPage", () => {
  it("renders the four tiles and a household row with a link to its drill-in", async () => {
    stubFetchRoutes({
      [`GET ${adminHouseholdsPath("", 50)}`]: {
        status: 200,
        body: response([listing()]),
      },
    });
    renderPage();

    expect(
      await screen.findByRole("heading", { name: "Households" }),
    ).toBeInTheDocument();
    // findByRole, not getByRole: the heading above renders unconditionally
    // (outside the isPending branch), so awaiting it only proves the router
    // has mounted, not that the fetch has resolved -- the tiles list is the
    // first element that actually gates on query.data.
    const tiles = await screen.findByRole("list", { name: "Install metrics" });
    expect(within(tiles).getByText("9 requested")).toBeInTheDocument();
    expect(within(tiles).getByText("4 completed")).toBeInTheDocument();
    expect(within(tiles).getByText("Invites pending")).toBeInTheDocument();

    const link = screen.getByRole("link", { name: "Oentoro" });
    expect(link.getAttribute("href")).toMatch(/\/admin\/households\/h1$/);
    expect(screen.getByText("Showing 1 of 1")).toBeInTheDocument();
  });

  it("names the matched member under a row that matched through a member", async () => {
    stubFetchRoutes({
      [`GET ${adminHouseholdsPath("christine@", 50)}`]: {
        status: 200,
        body: response([
          listing({
            match: { memberName: "Christine", memberEmail: "c@example.org" },
          }),
        ]),
      },
    });
    renderPage("christine@");

    expect(
      await screen.findByText("matched Christine · c@example.org"),
    ).toBeInTheDocument();
  });

  // Submitting is the only thing that searches: typing must not fetch.
  it("calls onSearch with the typed value on submit, and not before", async () => {
    stubFetchRoutes({
      [`GET ${adminHouseholdsPath("", 50)}`]: {
        status: 200,
        body: response([listing()]),
      },
    });
    const onSearch = vi.fn();
    renderPage("", 50, { onSearch });
    await screen.findByText("Showing 1 of 1");

    fireEvent.change(screen.getByLabelText("Search"), {
      target: { value: "wei" },
    });
    expect(onSearch).not.toHaveBeenCalled();
    fireEvent.submit(screen.getByRole("search"));
    expect(onSearch).toHaveBeenCalledWith("wei");
  });

  it("shows the empty-install message with the tiles at zero", async () => {
    stubFetchRoutes({
      [`GET ${adminHouseholdsPath("", 50)}`]: {
        status: 200,
        body: {
          metrics: {
            households: 0,
            activeHouseholds7d: 0,
            signups30d: { requested: 0, completed: 0 },
            pendingInvites: 0,
          },
          households: [],
          truncated: false,
        },
      },
    });
    renderPage();
    expect(await screen.findByText("No households yet.")).toBeInTheDocument();
  });

  it("shows the no-match message with the query and a Clear link", async () => {
    stubFetchRoutes({
      [`GET ${adminHouseholdsPath("zzz", 50)}`]: {
        status: 200,
        body: response([]),
      },
    });
    renderPage("zzz");
    const message = await screen.findByText("Nothing matches 'zzz'.");
    // Scoped to the message: the search form also shows its own "Clear"
    // once a query is active, so an unscoped query would match two buttons
    // with the identical accessible name -- this proves the message's own
    // Clear exists, which is the behaviour under test.
    expect(
      within(message).getByRole("button", { name: "Clear" }),
    ).toBeInTheDocument();
  });

  it("offers Show more when truncated under the cap, and says search to narrow at it", async () => {
    stubFetchRoutes({
      [`GET ${adminHouseholdsPath("", 50)}`]: {
        status: 200,
        body: response([listing()], true),
      },
      [`GET ${adminHouseholdsPath("", 200)}`]: {
        status: 200,
        body: response([listing()], true),
      },
    });
    const onShowMore = vi.fn();
    const { unmount } = renderPage("", 50, { onShowMore });
    fireEvent.click(await screen.findByRole("button", { name: "Show more" }));
    expect(onShowMore).toHaveBeenCalled();
    unmount();

    renderPage("", 200);
    expect(
      await screen.findByText("Showing the first 1 — search to narrow"),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Show more" }),
    ).not.toBeInTheDocument();
  });

  it("renders a skeleton, not a spinner, while loading", async () => {
    stubFetchRoutes({
      [`GET ${adminHouseholdsPath("", 50)}`]: {
        status: 200,
        body: response([listing()]),
      },
    });
    renderPage();
    // findByTestId, not getByTestId: renderWithRouter's RouterProvider does
    // not paint synchronously (its first match resolves on a later
    // microtask), so a bare assertion straight after render() sees an empty
    // body -- every other test in this file gates its first assertion on
    // the same await for that reason.
    expect(
      await screen.findByTestId("households-skeleton"),
    ).toBeInTheDocument();
    // Let the query settle before the test ends, so no update lands after
    // this test has finished and warns about a missing act().
    await screen.findByText("Showing 1 of 1");
  });

  it("shows an inline error for a failure the gate does not own", async () => {
    stubFetchRoutes({
      [`GET ${adminHouseholdsPath("", 50)}`]: {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Something broke." } },
      },
    });
    renderPage();
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("Something broke."),
    );
  });

  // A lapsed grant (or a revoked admin) is a gate-layer failure -- AdminShell's
  // own AdminGate is about to replace the whole surface for it, via
  // useCloseSurfaceOnReauth invalidating adminFlagsKey. This page must not
  // also render its own alert for the identical failure, or the operator
  // would see two different messages for one event.
  it("renders no inline alert for a gate-layer failure, leaving it to AdminGate", async () => {
    stubFetchRoutes({
      [`GET ${adminHouseholdsPath("", 50)}`]: {
        status: 401,
        body: {
          error: {
            code: "ADMIN_REAUTH_REQUIRED",
            message: "Confirm your password.",
          },
        },
      },
    });
    renderPage();
    // findByTestId first, then wait for it to go away: a bare
    // "not.toBeInTheDocument()" waitFor with nothing awaited first is
    // vacuously true before the router has even mounted (this file's
    // "renders a skeleton" test above hit the identical trap), so it would
    // pass on a broken page just as readily as a correct one -- confirming
    // the loading state actually appeared is what makes "then it went away"
    // mean the query actually settled.
    await screen.findByTestId("households-skeleton");
    await waitFor(() =>
      expect(
        screen.queryByTestId("households-skeleton"),
      ).not.toBeInTheDocument(),
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
