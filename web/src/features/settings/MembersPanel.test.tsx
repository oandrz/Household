// Behaviours from the task-20 brief:
// 1. Renders each member with their role label -- "Parent · full access" /
//    "Owner" for adults, "Kid · calendar & chores only" / "Limited" for
//    Kayla, "Kid · calendar only" / "Limited" for Ethan.
// 2. Toggling a capability for a kid issues
//    PATCH /api/v1/household/members/:id with the new capability array.
// 3. A 409 LAST_OWNER response renders the message inline and leaves the
//    toggle in its previous position.
// 4. The marriage capability is not offered for a limited member.
//
// Plus, since a role change and a capability change interact through
// domain rules (an owner must hold every capability, a limited member may
// never hold "marriage"): a role toggle must fix up the capability array in
// the same request, so two more behaviours are pinned directly rather than
// left to be inferred from #2/#3 alone -- promoting sends every capability
// (domain.ErrOwnerMustHoldAllCapabilities), demoting drops "marriage"
// (domain.ErrLimitedCannotHoldMarriage) -- and the
// 200-with-`warning` path (usecase.ErrSessionRevocationFailed) is checked
// explicitly rather than silently dropped.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import type { Me } from "../auth/schemas";
import { MembersPanel } from "./MembersPanel";
import type { MemberView } from "./schemas";

const ME_URL = "/api/v1/auth/me";
const MEMBERS_URL = "/api/v1/household/members";

function meFixture(role: "owner" | "limited" = "owner"): Me {
  return {
    user: {
      id: "u-andreas",
      email: "andreas@hearth.family",
      displayName: "Andreas",
      avatarInitial: "A",
    },
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
      id: "mem-andreas",
      householdId: "h-1",
      userId: "u-andreas",
      role,
      capabilities: role === "owner" ? ["calendar", "chores", "money", "marriage"] : ["calendar"],
    },
    capabilities: role === "owner" ? ["calendar", "chores", "money", "marriage"] : ["calendar"],
    spaces: [],
  };
}

function member(overrides: Partial<MemberView>): MemberView {
  return {
    id: "mem-x",
    user: { id: "u-x", email: "", displayName: "Name", avatarInitial: "N" },
    role: "limited",
    capabilities: [],
    ...overrides,
  };
}

const andreas = member({
  id: "mem-andreas",
  user: { id: "u-andreas", email: "andreas@hearth.family", displayName: "Andreas", avatarInitial: "A" },
  role: "owner",
  capabilities: ["calendar", "chores", "money", "marriage"],
});
const kayla = member({
  id: "mem-kayla",
  user: { id: "u-kayla", email: "", displayName: "Kayla", avatarInitial: "K" },
  role: "limited",
  capabilities: ["calendar", "chores"],
});
const ethan = member({
  id: "mem-ethan",
  user: { id: "u-ethan", email: "", displayName: "Ethan", avatarInitial: "E" },
  role: "limited",
  capabilities: ["calendar"],
});

function renderPanel() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MembersPanel />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("MembersPanel", () => {
  it("renders each member's role label exactly as the design writes it", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas, kayla, ethan] },
    });
    renderPanel();

    expect(await screen.findByText("Parent · full access")).toBeInTheDocument();
    expect(screen.getByText("Kid · calendar & chores only")).toBeInTheDocument();
    expect(screen.getByText("Kid · calendar only")).toBeInTheDocument();

    expect(screen.getByRole("switch", { name: "Andreas's role" })).toHaveTextContent("Owner");
    expect(screen.getByRole("switch", { name: "Kayla's role" })).toHaveTextContent("Limited");
    expect(screen.getByRole("switch", { name: "Ethan's role" })).toHaveTextContent("Limited");
  });

  it("issues PATCH /api/v1/household/members/:id with only the new capability array when a kid's capability is toggled", async () => {
    // Since the coordinator's fix (api/internal/adapter/http/
    // member_handlers.go's updateMemberRequest fields are now pointers,
    // matching /household and /notification-preferences), a plain
    // capability toggle sends capabilities alone -- role isn't changing,
    // and the server resolves the absent field against the membership's
    // current role rather than requiring it repeated on every request.
    const fetchMock = stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas, kayla, ethan] },
      "PATCH /api/v1/household/members/mem-kayla": {
        status: 200,
        body: { id: "mem-kayla", role: "limited", capabilities: ["calendar", "chores", "money"] },
      },
    });
    renderPanel();

    await screen.findByText("Kid · calendar & chores only");
    fireEvent.click(screen.getByRole("switch", { name: "Kayla Money access" }));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input) === "/api/v1/household/members/mem-kayla" &&
          (init?.method ?? "").toUpperCase() === "PATCH",
      );
      expect(call).toBeDefined();
      expect(JSON.parse(call![1]!.body as string)).toEqual({
        capabilities: ["calendar", "chores", "money"],
      });
    });
  });

  it("renders a 409 LAST_OWNER response inline and leaves the role toggle in its previous position", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas, kayla, ethan] },
      "PATCH /api/v1/household/members/mem-andreas": {
        status: 409,
        body: { error: { code: "LAST_OWNER", message: "A household must keep at least one owner." } },
      },
    });
    renderPanel();

    await screen.findByText("Parent · full access");
    const roleToggle = screen.getByRole("switch", { name: "Andreas's role" });
    expect(roleToggle).toHaveAttribute("aria-checked", "true");

    fireEvent.click(roleToggle);

    expect(
      await screen.findByText("A household must keep at least one owner."),
    ).toBeInTheDocument();
    expect(roleToggle).toHaveAttribute("aria-checked", "true");
    expect(roleToggle).toHaveTextContent("Owner");
  });

  it("does not offer the marriage capability for a limited member", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas, kayla, ethan] },
    });
    renderPanel();

    await screen.findByText("Kid · calendar & chores only");
    expect(screen.queryByRole("switch", { name: /marriage/i })).not.toBeInTheDocument();
  });

  it("grants every capability when promoting a limited member to owner", async () => {
    const fetchMock = stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas, kayla, ethan] },
      "PATCH /api/v1/household/members/mem-kayla": {
        status: 200,
        body: { id: "mem-kayla", role: "owner", capabilities: ["calendar", "chores", "money", "marriage"] },
      },
    });
    renderPanel();

    await screen.findByText("Kid · calendar & chores only");
    fireEvent.click(screen.getByRole("switch", { name: "Kayla's role" }));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input) === "/api/v1/household/members/mem-kayla" &&
          (init?.method ?? "").toUpperCase() === "PATCH",
      );
      expect(call).toBeDefined();
      expect(JSON.parse(call![1]!.body as string)).toEqual({
        role: "owner",
        capabilities: ["calendar", "chores", "money", "marriage"],
      });
    });
  });

  it("drops the marriage capability when demoting an owner to limited", async () => {
    const fetchMock = stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas, kayla, ethan] },
      "PATCH /api/v1/household/members/mem-andreas": {
        status: 200,
        body: { id: "mem-andreas", role: "limited", capabilities: ["calendar", "chores", "money"] },
      },
    });
    renderPanel();

    await screen.findByText("Parent · full access");
    fireEvent.click(screen.getByRole("switch", { name: "Andreas's role" }));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input) === "/api/v1/household/members/mem-andreas" &&
          (init?.method ?? "").toUpperCase() === "PATCH",
      );
      expect(call).toBeDefined();
      expect(JSON.parse(call![1]!.body as string)).toEqual({
        role: "limited",
        capabilities: ["calendar", "chores", "money"],
      });
    });
  });

  it("surfaces the session-revocation warning when the server returns one", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas, kayla, ethan] },
      "PATCH /api/v1/household/members/mem-kayla": {
        status: 200,
        body: {
          id: "mem-kayla",
          role: "limited",
          capabilities: ["calendar", "chores", "money"],
          warning:
            "The change was saved, but we couldn't sign the member out of their other sessions. They may still be able to use an old session until it expires.",
        },
      },
    });
    renderPanel();

    await screen.findByText("Kid · calendar & chores only");
    fireEvent.click(screen.getByRole("switch", { name: "Kayla Money access" }));

    expect(
      await screen.findByText(
        "The change was saved, but we couldn't sign the member out of their other sessions. They may still be able to use an old session until it expires.",
      ),
    ).toBeInTheDocument();
  });

  it("hides the invite control and disables editing for a non-owner viewer", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("limited") },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas, kayla, ethan] },
    });
    renderPanel();

    await screen.findByText("Parent · full access");
    expect(screen.queryByText("+ Invite")).not.toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Andreas's role" })).toBeDisabled();
    // Capability toggles are only ever rendered for the owner viewer.
    expect(screen.queryByRole("switch", { name: "Kayla Money access" })).not.toBeInTheDocument();
  });
});
