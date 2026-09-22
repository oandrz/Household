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
  channel: "email",
  expiresAt: "2026-09-26T09:00:00Z",
};

// A Telegram row, waiting on a tap with nothing minted this session --
// PendingInviteCard's state 3, the one every fixture above never reaches.
const christine: PendingInvite = {
  id: "inv-christine",
  name: "Christine",
  email: "",
  role: "owner",
  capabilities: ["money"],
  channel: "telegram",
  knock: null,
  expiresAt: "2026-09-27T00:00:00Z",
};

// Same invite, knocked -- used by the admitted-notice test below, which
// needs Let in to be clickable.
const knockedChristine: PendingInvite = {
  ...christine,
  knock: { username: "christine_t", code: "4812", knockedAt: "2026-09-20T10:00:00Z" },
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
    expect(screen.getByText("Owner")).toBeInTheDocument();
    expect(screen.getByText("jane@example.com")).toBeInTheDocument();
    expect(screen.getByText(/^Expires /)).toBeInTheDocument();
  });

  // Pins the ternary this list adds in Task 11: a Telegram row must reach
  // PendingInviteCard, not the milestone-1 PendingInviteRow beside it --
  // every other fixture in this file is `channel: "email"`, so without this
  // test, reverting that ternary back to always rendering PendingInviteRow
  // would leave every test here green.
  it("renders a Telegram invite as the waiting card, not the milestone-1 row", async () => {
    stubFetchRoutes({ [`GET ${INVITES_URL}`]: { status: 200, body: [jane, christine] } });
    renderList();

    expect(await screen.findByText("Christine")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /get a new link/i })).toBeInTheDocument();
    // The email row beside it is untouched: still Jane's plain row, no QR
    // controls or knock copy bleeding across.
    expect(screen.getByText("jane@example.com")).toBeInTheDocument();
  });

  // The bug a review caught: PendingInviteCard.test.tsx's own isolated
  // QueryClient has no active observer on pendingInvitesQueryKey, so
  // invalidateQueries there resolves instantly and the card never actually
  // races its own row leaving the list -- every test in that file passed
  // while this was broken. This list's own usePendingInvites *is* a live
  // observer, so admitting for real here triggers the same refetch a real
  // Settings tab would, and the row (and the card showing "Let in.") can
  // genuinely disappear before this assertion runs.
  it("keeps the sign-in-link warning on screen after the admitted row leaves the list", async () => {
    stubFetchRoutes({
      [`GET ${INVITES_URL}`]: [
        { status: 200, body: [knockedChristine] },
        { status: 200, body: [] }, // the refetch useAdmitInvite's onSettled kicks off
      ],
      [`POST ${INVITES_URL}/inv-christine/admit`]: {
        status: 200,
        body: {
          member: { id: "m1", name: "Christine", role: "owner", capabilities: ["money"] },
          signInSent: false,
        },
      },
    });
    renderList();

    fireEvent.click(await screen.findByRole("button", { name: /let in/i }));

    expect(
      await screen.findByText(/if no message arrived, ask them to send \/start to the bot/i),
    ).toBeInTheDocument();
    // The row itself is gone -- proving the notice survived independently
    // of the card that first showed it, not that the refetch just never ran.
    await waitFor(() => expect(screen.queryByRole("button", { name: /let in/i })).not.toBeInTheDocument());
    expect(
      screen.getByText(/if no message arrived, ask them to send \/start to the bot/i),
    ).toBeInTheDocument();
  });

  // Isolates the other half of the fix, separately from the test above:
  // useAdmitInvite's onSettled must not block the mutation's own success
  // dispatch on the pending-invites refetch it kicks off. Gates that
  // refetch open only at the end, so if onSettled still awaited it
  // (reverting fix #1 alone, with the onAdmitted hoist still in place),
  // this assertion would hang until the gate is released instead of
  // passing immediately.
  it("shows Let in immediately, without waiting for the pending-list refetch it triggers", async () => {
    let releaseRefetch: () => void = () => {};
    let getCount = 0;
    const routed = stubFetchRoutes({
      [`GET ${INVITES_URL}`]: [
        { status: 200, body: [knockedChristine] },
        { status: 200, body: [] },
      ],
      [`POST ${INVITES_URL}/inv-christine/admit`]: {
        status: 200,
        body: {
          member: { id: "m1", name: "Christine", role: "owner", capabilities: ["money"] },
          signInSent: true,
        },
      },
    });
    const gated = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "GET" && String(input) === INVITES_URL) {
        getCount += 1;
        if (getCount === 2) await new Promise<void>((r) => (releaseRefetch = r));
      }
      return routed(input, init);
    });
    vi.stubGlobal("fetch", gated);
    renderList();

    fireEvent.click(await screen.findByRole("button", { name: /let in/i }));

    expect(await screen.findByText("Let in.")).toBeInTheDocument();
    releaseRefetch();
  });

  // At 360px the detail line is about 214px wide, so even
  // "Owner · jane@example.com · Expires Sep 26" does not fit. When the whole
  // line truncated, the expiry was what got clipped (final review,
  // 2026-09-19). Only the address may give way. jsdom has no layout, so this
  // pins the structure; the browser walk measured the real row.
  it("clips a long address rather than the role or the expiry", async () => {
    stubFetchRoutes({ [`GET ${INVITES_URL}`]: { status: 200, body: [jane] } });
    renderList();

    const expiry = await screen.findByText(/^Expires /);
    expect(expiry.closest(".truncate")).toBeNull();
    expect(screen.getByText("Owner").closest(".truncate")).toBeNull();
    expect(screen.getByText("jane@example.com")).toHaveClass("truncate");
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

  // A failed withdraw is news too: a 404 means another owner already
  // withdrew it. Refreshing only on success left the dead row on screen
  // beside its error (final review, 2026-09-19).
  it("refreshes the list after a failed withdraw, so an invite already gone leaves the screen", async () => {
    stubFetchRoutes({
      [`GET ${INVITES_URL}`]: [
        { status: 200, body: [jane] },
        { status: 200, body: [] },
      ],
      [`DELETE ${INVITES_URL}/inv-jane`]: {
        status: 404,
        body: { error: { code: "NOT_FOUND", message: "That could not be found." } },
      },
    });
    renderList();

    fireEvent.click(await screen.findByRole("button", { name: "Withdraw the invite to Jane" }));

    await waitFor(() => expect(screen.queryByText("Jane")).not.toBeInTheDocument());
  });
});
