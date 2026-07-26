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

const moneySpace = space({
  id: "space-money",
  key: "money",
  name: "Money",
  position: 1,
  requiredCapability: "money",
});
const marriageSpace = space({
  id: "space-marriage",
  key: "marriage",
  name: "Marriage",
  visibility: "parents_only",
  position: 2,
  requiredCapability: "marriage",
});
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

  it("renders all three spaces in position order when all are visible", async () => {
    // Deliberately out of position order, matching the domain's own
    // documented invariant that VisibleSpaces does not sort -- callers must
    // already supply spaces in position order, so the sidebar must render
    // in the order given, not re-sort by `position` itself.
    renderWithRouter(
      <Sidebar me={meFixture([moneySpace, marriageSpace, familySpace])} />,
    );

    const elements = await screen.findAllByTestId("sidebar-space");
    expect(elements.map((el) => el.textContent)).toEqual([
      "Money",
      "Marriage",
      "Family",
    ]);
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
