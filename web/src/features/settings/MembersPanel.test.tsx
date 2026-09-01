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
    isPlatformAdmin: false,
    features: {},
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

  // Fix round 2 (spec review), Finding 1. toggleCapability computes its
  // next array from `member.capabilities`, which is only as fresh as the
  // last completed fetch. Clicking Money, then clicking Chores before the
  // first PATCH resolves and the members query refetches, would compute
  // the second request from the same pre-first-click array the first
  // click already read -- silently reverting the money grant with no error
  // and no visible reason. The fix disables a member's role switch and
  // every capability toggle while a mutation for that specific member (not
  // the whole list) is in flight.
  it("disables a member's own controls while its mutation is in flight, so a second click cannot revert the first", async () => {
    const kaylaAfterMoney = { ...kayla, capabilities: ["calendar", "chores", "money"] };
    const fetchMock = stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${MEMBERS_URL}`]: [
        { status: 200, body: [andreas, kayla, ethan] },
        // What a refetch (triggered once the first PATCH's onSuccess
        // invalidates the query) sees -- money now granted.
        { status: 200, body: [andreas, kaylaAfterMoney, ethan] },
      ],
      "PATCH /api/v1/household/members/mem-kayla": {
        status: 200,
        body: { id: "mem-kayla", role: "limited", capabilities: ["calendar", "chores", "money"] },
      },
    });
    renderPanel();

    await screen.findByText("Kid · calendar & chores only");

    const moneyToggle = screen.getByRole("switch", { name: "Kayla Money access" });
    const choresToggle = screen.getByRole("switch", { name: "Kayla Chores access" });
    const kaylaRoleToggle = screen.getByRole("switch", { name: "Kayla's role" });
    const ethanChoresToggle = screen.getByRole("switch", { name: "Ethan Chores access" });

    fireEvent.click(moneyToggle);

    // Immediately -- before the PATCH resolves -- every other control on
    // Kayla's own row is disabled too, and Ethan's row is untouched: the
    // guard is scoped per member, not to the whole list.
    expect(choresToggle).toBeDisabled();
    expect(kaylaRoleToggle).toBeDisabled();
    expect(ethanChoresToggle).not.toBeDisabled();

    // A click on a disabled control fires no request at all.
    fireEvent.click(choresToggle);

    function kaylaPatchCalls() {
      return fetchMock.mock.calls.filter(
        ([input, init]) =>
          String(input) === "/api/v1/household/members/mem-kayla" &&
          (init?.method ?? "").toUpperCase() === "PATCH",
      );
    }

    await waitFor(() => expect(kaylaPatchCalls()).toHaveLength(1));
    expect(JSON.parse(kaylaPatchCalls()[0][1]!.body as string)).toEqual({
      capabilities: ["calendar", "chores", "money"],
    });

    // Once the mutation settles and the query refetches, the row re-enables
    // -- and a genuine second click, now computed from the up-to-date
    // array, carries both changes: money (already persisted) stays, chores
    // is what actually changes.
    await waitFor(() => expect(choresToggle).not.toBeDisabled());
    await screen.findByText("Kid · calendar & chores & money only");

    fireEvent.click(choresToggle);

    await waitFor(() => expect(kaylaPatchCalls()).toHaveLength(2));
    expect(JSON.parse(kaylaPatchCalls()[1][1]!.body as string)).toEqual({
      capabilities: ["calendar", "money"],
    });
  });

  // Fix round 3 (spec review), Finding 1 -- still open after round 2. The
  // test above waits for `screen.findByText("Kid · calendar & chores &
  // money only")` before its second click, i.e. it waits for the refetch to
  // visibly land -- it proves "disabled while the PATCH is in flight" but
  // steps around "settled but not yet refetched," the narrower window that
  // was actually still open: useUpdateMember's onSuccess fired three
  // invalidateQueries calls without returning/awaiting them, so TanStack
  // Query treated the mutation as settled -- and pendingIds cleared -- the
  // instant the PATCH response arrived, before the invalidated members
  // query had refetched. A click in exactly that gap would still compute
  // from the stale array.
  //
  // This test manufactures that gap directly with a fetch mock (not
  // stubFetchRoutes, which resolves every response on the same microtask
  // and cannot hold one open) whose second GET /household/members call --
  // the refetch -- is deliberately left pending until the test resolves it
  // by hand.
  it("keeps a member's controls disabled until the refetch after its mutation actually lands, not just until the PATCH resolves", async () => {
    let resolveRefetch!: (response: Response) => void;
    const refetchResponse = new Promise<Response>((resolve) => {
      resolveRefetch = resolve;
    });
    let membersGetCount = 0;

    function jsonResponse(body: unknown, status = 200): Response {
      return new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      });
    }

    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
        const method = (init?.method ?? "GET").toUpperCase();
        const url = String(input);
        const key = `${method} ${url}`;

        if (key === `GET ${ME_URL}`) return jsonResponse(meFixture("owner"));
        if (key === `GET ${MEMBERS_URL}`) {
          membersGetCount += 1;
          if (membersGetCount === 1) {
            return jsonResponse([andreas, kayla, ethan]);
          }
          // The refetch triggered by the first mutation's invalidateQueries
          // -- held open to simulate a slow network landing well after the
          // PATCH itself has already resolved.
          return refetchResponse;
        }
        if (key === "PATCH /api/v1/household/members/mem-kayla") {
          return jsonResponse({
            id: "mem-kayla",
            role: "limited",
            capabilities: ["calendar", "chores", "money"],
          });
        }
        throw new Error(`unstubbed request in this test: ${key}`);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderPanel();
    await screen.findByText("Kid · calendar & chores only");

    const choresToggle = screen.getByRole("switch", { name: "Kayla Chores access" });

    function kaylaPatchCalls() {
      return fetchMock.mock.calls.filter(
        ([input, init]) =>
          String(input) === "/api/v1/household/members/mem-kayla" &&
          ((init as RequestInit | undefined)?.method ?? "").toUpperCase() === "PATCH",
      );
    }

    fireEvent.click(screen.getByRole("switch", { name: "Kayla Money access" }));

    // Let the PATCH itself resolve -- it is not held open, only the
    // refetch is.
    await waitFor(() => expect(kaylaPatchCalls()).toHaveLength(1));

    // The PATCH has resolved, but the members refetch it triggered is
    // still pending (deliberately held open). This is exactly the gap the
    // non-awaited-onSuccess bug left uncovered: the row must still be
    // disabled here, not just during the PATCH itself.
    expect(choresToggle).toBeDisabled();

    // A click in this gap must be a genuine no-op.
    fireEvent.click(choresToggle);
    expect(kaylaPatchCalls()).toHaveLength(1);

    // Let the refetch land.
    resolveRefetch(
      jsonResponse([andreas, { ...kayla, capabilities: ["calendar", "chores", "money"] }, ethan]),
    );

    await waitFor(() => expect(choresToggle).not.toBeDisabled());
    await screen.findByText("Kid · calendar & chores & money only");

    // Now the real second click: computed from the up-to-date array, it
    // must carry both changes -- money (already persisted) stays, chores
    // is what actually changes -- never a request that lost money because
    // it was computed from the pre-refetch array.
    fireEvent.click(choresToggle);

    await waitFor(() => expect(kaylaPatchCalls()).toHaveLength(2));
    expect(JSON.parse(kaylaPatchCalls()[1][1]!.body as string)).toEqual({
      capabilities: ["calendar", "money"],
    });
  });

  // Fix round 2 (spec review), Finding 2. Every promotion this UI can
  // trigger sends the full, hardcoded capability set (via the shared
  // ALL_CAPABILITIES constant), so 422 INVALID_CAPABILITIES is prevented by
  // construction rather than ever actually reaching this code today. That
  // is exactly why it needs a test that drives the error directly rather
  // than through the UI: the path must stay correct even though nothing in
  // this screen can currently produce it, so it is still safe on the day a
  // future change (a fifth capability, a relaxed client-side guard) makes
  // it reachable.
  it("renders a 422 INVALID_CAPABILITIES response inline, the same way the 409 LAST_OWNER case does", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas, kayla, ethan] },
      "PATCH /api/v1/household/members/mem-kayla": {
        status: 422,
        body: {
          error: {
            code: "INVALID_CAPABILITIES",
            message: "That capability set is not valid for this role.",
          },
        },
      },
    });
    renderPanel();

    await screen.findByText("Kid · calendar & chores only");
    fireEvent.click(screen.getByRole("switch", { name: "Kayla's role" }));

    expect(
      await screen.findByText("That capability set is not valid for this role."),
    ).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Kayla's role" })).toHaveTextContent("Limited");
  });
});
