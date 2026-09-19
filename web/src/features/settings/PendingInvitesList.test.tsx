import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { PendingInvitesList } from "./PendingInvitesList";
import type { PendingInvite } from "./schemas";

const INVITES_URL = "/api/v1/household/invites";

const jane: PendingInvite = {
  id: "inv-jane",
  name: "Jane",
  email: "jane@example.com",
  role: "owner",
  capabilities: ["calendar", "chores", "money", "marriage"],
  expiresAt: "2026-09-26T09:00:00Z",
};

function renderList() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <PendingInvitesList />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("PendingInvitesList", () => {
  it("lists each pending invite with its role, address and expiry", async () => {
    stubFetchRoutes({ [`GET ${INVITES_URL}`]: { status: 200, body: [jane] } });
    renderList();

    expect(await screen.findByText("Jane")).toBeInTheDocument();
    expect(screen.getByText(/^Owner · jane@example\.com · Expires /)).toBeInTheDocument();
  });

  it("renders nothing when no invite is pending", async () => {
    const fetchMock = stubFetchRoutes({ [`GET ${INVITES_URL}`]: { status: 200, body: [] } });
    const { container } = renderList();

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    await waitFor(() => expect(container).toBeEmptyDOMElement());
  });

  it("withdraws an invite and drops it from the list", async () => {
    const fetchMock = stubFetchRoutes({
      [`GET ${INVITES_URL}`]: [
        { status: 200, body: [jane] },
        { status: 200, body: [] },
      ],
      [`DELETE ${INVITES_URL}/inv-jane`]: { status: 204, body: undefined },
    });
    renderList();

    fireEvent.click(await screen.findByRole("button", { name: "Withdraw the invite to Jane" }));

    await waitFor(() => expect(screen.queryByText("Jane")).toBeNull());
    expect(
      fetchMock.mock.calls.some(
        ([input, init]) => String(input) === `${INVITES_URL}/inv-jane` && init?.method === "DELETE",
      ),
    ).toBe(true);
  });

  it("disables Withdraw while its request runs, so a double click sends one DELETE", async () => {
    let release: () => void = () => {};
    const routed = stubFetchRoutes({
      [`GET ${INVITES_URL}`]: { status: 200, body: [jane] },
      [`DELETE ${INVITES_URL}/inv-jane`]: { status: 204, body: undefined },
    });
    const gated = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "DELETE") await new Promise<void>((r) => (release = r));
      return routed(input, init);
    });
    vi.stubGlobal("fetch", gated);
    renderList();

    const button = await screen.findByRole("button", { name: "Withdraw the invite to Jane" });
    fireEvent.click(button);
    fireEvent.click(button);
    await waitFor(() => expect(button).toBeDisabled());
    release();

    expect(gated.mock.calls.filter(([, init]) => init?.method === "DELETE")).toHaveLength(1);
  });

  it("shows the server's message when a withdraw fails", async () => {
    stubFetchRoutes({
      [`GET ${INVITES_URL}`]: { status: 200, body: [jane] },
      [`DELETE ${INVITES_URL}/inv-jane`]: {
        status: 409,
        body: { error: { code: "INVITE_ALREADY_ACCEPTED", message: "This invite has already been accepted." } },
      },
    });
    renderList();

    fireEvent.click(await screen.findByRole("button", { name: "Withdraw the invite to Jane" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("This invite has already been accepted.");
  });
});
