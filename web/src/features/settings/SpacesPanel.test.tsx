// Not part of the task-20 brief's enumerated MembersPanel behaviours, but
// added because SpacesPanel's audience-label derivation and NewSpaceModal's
// owner-only creation flow are non-trivial and untested otherwise.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import type { Me, Space } from "../auth/schemas";
import { SpacesPanel } from "./SpacesPanel";

const ME_URL = "/api/v1/auth/me";
const SPACES_URL = "/api/v1/spaces";

function meFixture(role: "owner" | "limited" = "owner"): Me {
  return {
    user: { id: "u-1", email: "andreas@hearth.family", displayName: "Andreas", avatarInitial: "A" },
    household: {
      id: "h-1",
      name: "Andreas & Christine",
      familyName: "Oentoro",
      primaryCurrency: "SGD",
      showSecondaryCurrency: true,
      secondaryCurrency: "IDR",
      fxRateMode: "auto",
    },
    membership: {
      id: "mem-1",
      householdId: "h-1",
      userId: "u-1",
      role,
      capabilities: ["calendar", "chores", "money", "marriage"],
    },
    capabilities: ["calendar", "chores", "money", "marriage"],
    spaces: [],
    isPlatformAdmin: false,
    features: {},
  };
}

function space(overrides: Partial<Space>): Space {
  return {
    id: "space-x",
    key: "x",
    name: "X",
    visibility: "everyone",
    position: 0,
    isBuiltin: true,
    ...overrides,
  };
}

const money = space({ id: "s-money", key: "money", name: "Money", position: 1, requiredCapability: "money" });
const marriage = space({
  id: "s-marriage",
  key: "marriage",
  name: "Marriage",
  visibility: "parents_only",
  position: 2,
  requiredCapability: "marriage",
});
const family = space({ id: "s-family", key: "family", name: "Family", position: 3 });

function renderPanel() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <SpacesPanel />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("SpacesPanel", () => {
  it("derives the design's three audience labels from visibility and requiredCapability", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${SPACES_URL}`]: { status: 200, body: [money, marriage, family] },
    });
    renderPanel();

    expect(await screen.findByText("Money")).toBeInTheDocument();
    expect(screen.getByText("Parents")).toBeInTheDocument();
    expect(screen.getByText("🔒 Parents only")).toBeInTheDocument();
    expect(screen.getByText("Everyone")).toBeInTheDocument();
  });

  it("hides + New space for a non-owner viewer", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("limited") },
      [`GET ${SPACES_URL}`]: { status: 200, body: [family] },
    });
    renderPanel();

    await screen.findByText("Family");
    expect(screen.queryByText(/New space/)).not.toBeInTheDocument();
  });

  it("opens the New space modal and disables Custom visibility", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${SPACES_URL}`]: { status: 200, body: [family] },
    });
    renderPanel();

    fireEvent.click(await screen.findByText(/New space/));

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Custom/ })).toBeDisabled();
    expect(screen.getByText("· not built")).toBeInTheDocument();
  });

  it("selecting a template prefills the Name field", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${SPACES_URL}`]: { status: 200, body: [family] },
    });
    renderPanel();

    fireEvent.click(await screen.findByText(/New space/));
    fireEvent.click(await screen.findByText("Travel"));

    expect(screen.getByLabelText("Name")).toHaveValue("Travel");
  });

  it("submits POST /api/v1/spaces with the chosen name and visibility", async () => {
    const fetchMock = stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${SPACES_URL}`]: { status: 200, body: [family] },
      [`POST ${SPACES_URL}`]: {
        status: 201,
        body: { id: "s-new", key: "kids", name: "Kids", visibility: "everyone", position: 4, isBuiltin: false },
      },
    });
    renderPanel();

    fireEvent.click(await screen.findByText(/New space/));
    fireEvent.click(screen.getByRole("button", { name: "Parents only" }));
    fireEvent.click(screen.getByRole("button", { name: "Create space" }));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input) === SPACES_URL && (init?.method ?? "").toUpperCase() === "POST",
      );
      expect(call).toBeDefined();
      const body = JSON.parse(call![1]!.body as string);
      expect(body.name).toBe("Kids");
      expect(body.visibility).toBe("parents_only");
    });
  });
});
