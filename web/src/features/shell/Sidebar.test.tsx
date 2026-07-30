// Behaviours from the task-19 brief:
// 1. Given only Family in `me.spaces`, Family renders and Money/Marriage do
//    not -- the sidebar must render from the payload, not a hard-coded list.
// 2. Given all three spaces, all three render, in the order the payload
//    gives them (VisibleSpaces on the server already sorted by position;
//    the sidebar must not re-sort).
// 3. The footer shows the household name and a "Sign out" control.
// 4. Clicking "Sign out" calls POST /api/v1/auth/sign-out.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Me, Space } from "../auth/schemas";
import { stubFetchRoutes } from "../../test/fetchStub";
import { renderWithRouter } from "../../test/renderWithRouter";
import { Sidebar } from "./Sidebar";

function space(overrides: Partial<Space>): Space {
  return {
    id: "space-id",
    key: "key",
    name: "Name",
    visibility: "everyone",
    position: 0,
    isBuiltin: true,
    ...overrides,
  };
}

const familySpace = space({
  id: "space-family",
  key: "family",
  name: "Family",
  position: 3,
});

function meFixture(spaces: Space[]): Me {
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
    spaces,
  };
}

describe("Sidebar", () => {
  it("renders only the spaces present in me.spaces", async () => {
    renderWithRouter(<Sidebar me={meFixture([familySpace])} />);

    // TanStack Router resolves its initial match asynchronously even with a
    // memory history and a single route, so the first render commit is
    // empty; findBy* awaits that before asserting.
    expect(await screen.findByText("Family")).toBeInTheDocument();
    expect(screen.queryByText("Money")).not.toBeInTheDocument();
    expect(screen.queryByText("Marriage")).not.toBeInTheDocument();
  });

  it("renders spaces in the order given, not re-sorted by position", async () => {
    // Fix round 1, Finding 2: the previous version of this test supplied
    // Money/Marriage/Family both in the array in that order AND with
    // ascending positions (1, 2, 3) -- a component that re-sorted by
    // `position` would have produced an identical rendered order, so the
    // test could not have caught that regression. This supplies them in the
    // array as Family, Money, Marriage while their `position` fields are 3,
    // 1, 2 respectively (still distinct and still meaningful data, just not
    // ascending in array order), and asserts the *array* order is what
    // renders -- not the position-sorted order (which would be Money,
    // Marriage, Family). Per domain.VisibleSpaces's own documented
    // invariant, the server has already ordered by position and the sidebar
    // must render exactly what it's given, never re-sort.
    const outOfOrderFamily = space({
      id: "space-family",
      key: "family",
      name: "Family",
      position: 3,
    });
    const outOfOrderMoney = space({
      id: "space-money",
      key: "money",
      name: "Money",
      position: 1,
      requiredCapability: "money",
    });
    const outOfOrderMarriage = space({
      id: "space-marriage",
      key: "marriage",
      name: "Marriage",
      visibility: "parents_only",
      position: 2,
      requiredCapability: "marriage",
    });

    renderWithRouter(
      <Sidebar
        me={meFixture([outOfOrderFamily, outOfOrderMoney, outOfOrderMarriage])}
      />,
    );

    // Money now expands into a group label plus one row per built page
    // (Finances, Transactions, Budget), so the flat "Family, Money, Marriage"
    // this test used to assert no longer matches -- the grouped shape adds a
    // label plus three link rows for Money alone. "sidebar-space" now tags
    // only links (and the unknown-key fallback span); the group label has
    // its own "sidebar-space-label" testid (finding 5) -- asserted here as
    // link order and label presence separately, so this test's actual
    // purpose (payload order, not position-sorted) isn't entangled with
    // where a non-link label happens to land in a flattened query.
    const links = await screen.findAllByTestId("sidebar-space");
    expect(links.map((el) => el.textContent)).toEqual([
      "Family",
      "Finances",
      "Transactions",
      "Budget",
      "Marriage",
    ]);
    expect(screen.getByTestId("sidebar-space-label")).toHaveTextContent("Money");
  });

  it("groups Money into a label with Finances, Transactions and Budget links", async () => {
    stubFetchRoutes({});
    renderWithRouter(
      <Sidebar
        me={meFixture([
          space({ id: "space-money", key: "money", name: "Money", position: 1 }),
        ])}
      />,
    );
    // The group label is not a link.
    expect(await screen.findByText("Money")).toBeInTheDocument();
    expect(screen.getByText("Money").closest("a")).toBeNull();
    expect(screen.getByRole("link", { name: "Finances" })).toHaveAttribute("href", "/money");
    expect(screen.getByRole("link", { name: "Transactions" })).toHaveAttribute(
      "href",
      "/money/transactions",
    );
    expect(screen.getByRole("link", { name: "Budget" })).toHaveAttribute(
      "href",
      "/money/budget",
    );
  });

  it("keeps single-page spaces as one link, in payload order", async () => {
    stubFetchRoutes({});
    renderWithRouter(
      <Sidebar
        me={meFixture([
          space({ id: "space-money", key: "money", name: "Money", position: 1 }),
          space({ id: "space-marriage", key: "marriage", name: "Marriage", position: 2 }),
          space({ id: "space-family", key: "family", name: "Family", position: 3 }),
        ])}
      />,
    );
    expect(await screen.findByRole("link", { name: "Marriage" })).toHaveAttribute(
      "href",
      "/marriage",
    );
    expect(screen.getByRole("link", { name: "Family" })).toHaveAttribute(
      "href",
      "/family/calendar",
    );
    // Order: the payload's, not alphabetical -- Finances before Marriage
    // before Family. Money's own name only ever shows as its group label
    // (finding 5), not a link, so it isn't part of this link-order list.
    const links = screen.getAllByTestId("sidebar-space").map((el) => el.textContent);
    expect(links).toEqual(["Finances", "Transactions", "Budget", "Marriage", "Family"]);
    expect(screen.getByTestId("sidebar-space-label")).toHaveTextContent("Money");
  });

  it("renders an unrecognised space key as plain text, not a link", async () => {
    stubFetchRoutes({});
    renderWithRouter(
      <Sidebar
        me={meFixture([
          // "+ New space" (Task 20) can let a household create a space
          // whose key SPACE_PAGES doesn't recognise yet -- that must render
          // as inert text rather than a Link to a route that doesn't exist.
          space({ id: "space-budget", key: "budget", name: "Budget", position: 1 }),
        ])}
      />,
    );

    const budget = await screen.findByTestId("sidebar-space");
    expect(budget).toHaveTextContent("Budget");
    expect(budget.tagName).not.toBe("A");
    expect(screen.queryByRole("link", { name: "Budget" })).not.toBeInTheDocument();
  });

  // The jsdom-visible half of the cascade bug fixed in e860d89: an active
  // link must carry `text-accent` and *not* `text-ink` at the same time --
  // `toHaveClass("text-accent")` alone would have stayed green throughout
  // that whole defect, since the broken markup carried both tokens on every
  // link and the class-string assertion never noticed which one a real
  // cascade actually painted. Asserting the absence of the other color
  // class is what makes this test able to fail again.
  it("accents only the active Money link", async () => {
    renderWithRouter(
      <Sidebar
        me={meFixture([
          space({ id: "space-money", key: "money", name: "Money", position: 1 }),
        ])}
      />,
      "/money",
    );

    const finances = await screen.findByRole("link", { name: "Finances" });
    const transactions = screen.getByRole("link", { name: "Transactions" });
    const budget = screen.getByRole("link", { name: "Budget" });
    expect(finances).toHaveClass("text-accent");
    expect(finances).not.toHaveClass("text-ink");
    expect(transactions).toHaveClass("text-ink");
    expect(transactions).not.toHaveClass("text-accent");
    expect(budget).toHaveClass("text-ink");
    expect(budget).not.toHaveClass("text-accent");
  });

  // Task 11's own regression test for the activeProps cascade defect
  // (LEARNING pattern 3): Budget's active state has to come from the same
  // route-driven `useMatchRoute` computation as its siblings, not from
  // `Link`'s `activeProps` (which merges rather than replaces className and
  // shipped the bug the comment above describes). Asserts both halves --
  // Budget carries the accent and not the ink class while its siblings carry
  // the reverse -- the same shape as "accents only the active Money link".
  it("accents only the active Budget link, on /money/budget", async () => {
    renderWithRouter(
      <Sidebar
        me={meFixture([
          space({ id: "space-money", key: "money", name: "Money", position: 1 }),
        ])}
      />,
      "/money/budget",
    );

    const finances = await screen.findByRole("link", { name: "Finances" });
    const transactions = screen.getByRole("link", { name: "Transactions" });
    const budget = screen.getByRole("link", { name: "Budget" });
    expect(budget).toHaveClass("text-accent");
    expect(budget).not.toHaveClass("text-ink");
    expect(finances).toHaveClass("text-ink");
    expect(finances).not.toHaveClass("text-accent");
    expect(transactions).toHaveClass("text-ink");
    expect(transactions).not.toHaveClass("text-accent");
  });

  // Overview used to be unconditionally text-accent (finding 3): it stayed
  // green even on /settings, where nothing about it was actually active.
  it("accents Overview only on /, not on another route", async () => {
    renderWithRouter(<Sidebar me={meFixture([familySpace])} />, "/settings");

    const overview = await screen.findByRole("link", { name: "Overview" });
    expect(overview).toHaveClass("text-ink");
    expect(overview).not.toHaveClass("text-accent");
  });

  // Settings used to be unconditionally text-ink (finding 3): it never
  // accented even while on /settings itself.
  it("accents Settings only on /settings", async () => {
    renderWithRouter(<Sidebar me={meFixture([familySpace])} />, "/settings");

    const settings = await screen.findByRole("link", { name: "Settings" });
    expect(settings).toHaveClass("text-accent");
    expect(settings).not.toHaveClass("text-ink");
  });

  // Single-page spaces (Marriage, Family) used to be unconditionally
  // text-ink, same defect class as Overview/Settings above.
  it("accents a single-page space's link only on its own route", async () => {
    renderWithRouter(
      <Sidebar
        me={meFixture([
          space({ id: "space-marriage", key: "marriage", name: "Marriage", position: 1 }),
        ])}
      />,
      "/marriage",
    );

    const marriage = await screen.findByRole("link", { name: "Marriage" });
    expect(marriage).toHaveClass("text-accent");
    expect(marriage).not.toHaveClass("text-ink");
  });

  it("shows the household name and a Sign out control in the footer", async () => {
    renderWithRouter(<Sidebar me={meFixture([familySpace])} />);

    expect(await screen.findByText("Andreas & Christine")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /sign out/i })).toBeInTheDocument();
  });

  it("calls POST /api/v1/auth/sign-out when Sign out is clicked", async () => {
    const fetchMock = stubFetchRoutes({
      "POST /api/v1/auth/sign-out": { status: 204, body: undefined },
      "GET /api/v1/auth/me": {
        status: 401,
        body: { error: { code: "UNAUTHENTICATED", message: "Not signed in." } },
      },
    });

    renderWithRouter(<Sidebar me={meFixture([familySpace])} />);
    fireEvent.click(await screen.findByRole("button", { name: /sign out/i }));

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(
          ([input, init]) =>
            String(input) === "/api/v1/auth/sign-out" &&
            (init?.method ?? "GET").toUpperCase() === "POST",
        ),
      ).toBe(true);
    });
  });
});
