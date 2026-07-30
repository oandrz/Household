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
    // (Finances, Transactions), so the flat "Family, Money, Marriage" this
    // test used to assert no longer matches -- the grouped shape adds two
    // rows for Money alone.
    const elements = await screen.findAllByTestId("sidebar-space");
    expect(elements.map((el) => el.textContent)).toEqual([
      "Family",
      "Money",
      "Finances",
      "Transactions",
      "Marriage",
    ]);
  });

  it("groups Money into a label with Finances and Transactions links", async () => {
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
    // before Family.
    const labels = screen.getAllByTestId("sidebar-space").map((el) => el.textContent);
    expect(labels).toEqual(["Money", "Finances", "Transactions", "Marriage", "Family"]);
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
