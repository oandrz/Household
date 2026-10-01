// Not part of the task-20 brief's enumerated MembersPanel behaviours, but
// added because SpacesPanel's audience-label derivation and NewSpaceModal's
// owner-only creation flow are non-trivial and untested otherwise.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import type { Me, Space } from "../auth/schemas";
import type { MemberView } from "./schemas";
import { SpacesPanel } from "./SpacesPanel";

const ME_URL = "/api/v1/auth/me";
const SPACES_URL = "/api/v1/spaces";
const MEMBERS_URL = "/api/v1/household/members";

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
      timezone: "Asia/Singapore",
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

function member(name: string, role: "owner" | "limited", capabilities: string[]): MemberView {
  return {
    id: `mem-${name}`,
    user: { id: `u-${name}`, email: "", displayName: name, avatarInitial: name[0] },
    role,
    capabilities,
  };
}

const andreas = member("Andreas", "owner", ["calendar", "chores", "money", "marriage"]);

// The row a space's name and audience sit in together, so an assertion reads
// "Money's audience is X" rather than "X is somewhere on the page".
function audienceOf(spaceName: string): string {
  const row = screen.getByText(spaceName).parentElement!;
  return row.lastElementChild!.textContent ?? "";
}

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
  it("labels a capability-gated space Parents while no kid holds its capability", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${SPACES_URL}`]: { status: 200, body: [money, marriage, family] },
      [`GET ${MEMBERS_URL}`]: {
        status: 200,
        body: [andreas, member("Kayla", "limited", ["calendar", "chores"])],
      },
    });
    renderPanel();

    await waitFor(() => expect(audienceOf("Money")).toBe("Parents"));
    expect(audienceOf("Marriage")).toBe("🔒 Parents only");
    expect(audienceOf("Family")).toBe("Everyone");
  });

  it("counts the kids who hold a space's capability into its audience", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${SPACES_URL}`]: { status: 200, body: [money, marriage, family] },
      [`GET ${MEMBERS_URL}`]: {
        status: 200,
        // Ethan holds no money capability, so he must not be counted: a count
        // of every limited member would read "3 kids" here.
        body: [
          andreas,
          member("Kayla", "limited", ["calendar", "money"]),
          member("Ethan", "limited", ["calendar"]),
          member("Maya", "limited", ["money"]),
        ],
      },
    });
    renderPanel();

    await waitFor(() => expect(audienceOf("Money")).toBe("Parents and 2 kids"));
    // A kid holding money changes Money's audience only.
    expect(audienceOf("Marriage")).toBe("🔒 Parents only");
    expect(audienceOf("Family")).toBe("Everyone");
  });

  it("says 1 kid, not 1 kids, when a single kid holds the capability", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${SPACES_URL}`]: { status: 200, body: [money] },
      [`GET ${MEMBERS_URL}`]: {
        status: 200,
        body: [andreas, member("Kayla", "limited", ["money"])],
      },
    });
    renderPanel();

    await waitFor(() => expect(audienceOf("Money")).toBe("Parents and 1 kid"));
  });

  it("says nothing about a capability-gated space's audience when the member list cannot be read", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${SPACES_URL}`]: { status: 200, body: [money, family] },
      [`GET ${MEMBERS_URL}`]: { status: 500, body: { error: { code: "INTERNAL", message: "boom" } } },
    });
    renderPanel();

    // Family needs no member list, so it is the signal the panel has rendered.
    await waitFor(() => expect(audienceOf("Family")).toBe("Everyone"));
    expect(audienceOf("Money")).toBe("");
  });

  it("labels a visibility it does not recognise parents-only, as the server treats it", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${SPACES_URL}`]: {
        status: 200,
        body: [space({ id: "s-custom", key: "club", name: "Club", visibility: "custom", isBuiltin: false })],
      },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas] },
    });
    renderPanel();

    await waitFor(() => expect(audienceOf("Club")).toBe("🔒 Parents only"));
  });

  it("hides + New space for a non-owner viewer", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("limited") },
      [`GET ${SPACES_URL}`]: { status: 200, body: [family] },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas] },
    });
    renderPanel();

    await screen.findByText("Family");
    expect(screen.queryByText(/New space/)).not.toBeInTheDocument();
  });

  it("opens the New space modal and disables Custom visibility", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${SPACES_URL}`]: { status: 200, body: [family] },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas] },
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
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas] },
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
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas] },
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
