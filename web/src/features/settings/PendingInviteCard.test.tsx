// The card's four states. Each is what the owner is looking at while they
// wait for a person in the same room to pick up their phone, so the words
// matter as much as the buttons. Task 11's own brief lists the exact cases
// this file pins.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import type { Me } from "../auth/schemas";
import { PendingInviteCard } from "./PendingInviteCard";
import type { PendingInvite } from "./schemas";

const INVITES_URL = "/api/v1/household/invites";
const MEMBERS_URL = "/api/v1/household/members";
const ME_URL = "/api/v1/auth/me";

const waitingInvite: PendingInvite = {
  id: "1",
  name: "Christine",
  email: "",
  role: "owner",
  capabilities: ["money"],
  channel: "telegram",
  knock: null,
  expiresAt: "2026-09-27T00:00:00Z",
};

// id "2" matters: it is what the admit tests' stub URL is built from below.
const knockedInvite: PendingInvite = {
  ...waitingInvite,
  id: "2",
  knock: { username: "christine_t", code: "4812", knockedAt: "2026-09-20T10:00:00Z" },
};

// Only used by the admit tests, which invalidate /me, /household/invites
// and /household/members on settle (useAdmitInvite's own comment). Nothing
// in this file mounts a component that observes those queries, so
// TanStack's default `refetchType: "active"` never actually issues these
// requests -- registered anyway so a change to that default doesn't turn
// into an opaque "no stub registered" failure here.
const meBody: Me = {
  user: { id: "u-1", email: "andreas@hearth.family", displayName: "Andreas", avatarInitial: "A" },
  household: {
    id: "h-1", name: "Andreas & Christine", familyName: "Oentoro",
    primaryCurrency: "SGD", showSecondaryCurrency: false, secondaryCurrency: "", fxRateMode: "auto",
  },
  membership: { id: "mem-1", householdId: "h-1", userId: "u-1", role: "owner", capabilities: ["money"] },
  capabilities: ["money"],
  spaces: [],
  isPlatformAdmin: false,
  features: {},
};

function renderCard(props: {
  invite: PendingInvite;
  link?: string;
  onNewLink?: (link: string) => void;
}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <ul>
        <PendingInviteCard {...props} />
      </ul>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("PendingInviteCard", () => {
  it("shows the link and the shown-once warning while nobody has knocked", () => {
    renderCard({ invite: waitingInvite, link: "https://t.me/HearthBot?start=inv_abc" });

    expect(screen.getByRole("button", { name: /copy/i })).toBeInTheDocument();
    expect(screen.getByText(/shown once/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /let in/i })).not.toBeInTheDocument();
  });

  it("offers a new link, not the old one, when this session no longer holds it", () => {
    renderCard({ invite: waitingInvite });

    expect(screen.queryByRole("button", { name: /copy/i })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /get a new link/i })).toBeInTheDocument();
  });

  it("asks the owner to compare the code once somebody has knocked", () => {
    renderCard({ invite: knockedInvite });

    expect(screen.getByText(/@christine_t tapped the link/i)).toBeInTheDocument();
    expect(screen.getByText("4812")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /let in/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /not them/i })).toBeInTheDocument();
  });

  // A @username is optional on Telegram, and the codebase never falls back
  // to a first name (adapter/telegram/update.go, senderName). The card says
  // so plainly instead of rendering an empty "@".
  it("says so plainly when the knocker has no Telegram username", () => {
    renderCard({ invite: { ...knockedInvite, knock: { ...knockedInvite.knock!, username: null } } });

    expect(screen.getByText(/someone with no telegram username tapped the link/i)).toBeInTheDocument();
    expect(screen.getByText("4812")).toBeInTheDocument();
  });

  it("names a sign-in link that could not be sent", async () => {
    stubFetchRoutes({
      "POST /api/v1/household/invites/2/admit": {
        status: 200,
        body: { member: { id: "m1", name: "Christine", role: "owner", capabilities: ["money"] }, signInSent: false },
      },
      [`GET ${INVITES_URL}`]: { status: 200, body: [] },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [] },
      [`GET ${ME_URL}`]: { status: 200, body: meBody },
    });
    renderCard({ invite: knockedInvite });

    fireEvent.click(screen.getByRole("button", { name: /let in/i }));

    expect(
      await screen.findByText(/if no message arrived, ask them to send \/start to the bot/i),
    ).toBeInTheDocument();
  });

  // The other half of "names a sign-in link that could not be sent" above:
  // proves the recovery sentence is additive, not a swap -- a successful
  // send still shows "Let in." and nothing more.
  it("shows only 'Let in.' when the sign-in link did send", async () => {
    stubFetchRoutes({
      "POST /api/v1/household/invites/2/admit": {
        status: 200,
        body: { member: { id: "m1", name: "Christine", role: "owner", capabilities: ["money"] }, signInSent: true },
      },
      [`GET ${INVITES_URL}`]: { status: 200, body: [] },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [] },
      [`GET ${ME_URL}`]: { status: 200, body: meBody },
    });
    renderCard({ invite: knockedInvite });

    fireEvent.click(screen.getByRole("button", { name: /let in/i }));

    expect(await screen.findByText("Let in.")).toBeInTheDocument();
    expect(screen.queryByText(/ask them to send/i)).not.toBeInTheDocument();
  });

  // The four digits are compared by eye; no endpoint accepts them back
  // (spec decision 3). A body here would be the first step toward a typed
  // code, which this design rejects -- so this asserts the request carries
  // none, via the stub's own capture hook rather than reaching into
  // fetchMock.mock.calls (fetchStub.ts's own stated reason for that hook).
  it("sends no body when admitting -- the code is compared by eye, never posted back", async () => {
    let capturedBody: unknown = "capture never ran";
    stubFetchRoutes({
      "POST /api/v1/household/invites/2/admit": {
        status: 200,
        body: { member: { id: "m1", name: "Christine", role: "owner", capabilities: ["money"] }, signInSent: true },
        capture: (body) => {
          capturedBody = body;
        },
      },
      [`GET ${INVITES_URL}`]: { status: 200, body: [] },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [] },
      [`GET ${ME_URL}`]: { status: 200, body: meBody },
    });
    renderCard({ invite: knockedInvite });

    fireEvent.click(screen.getByRole("button", { name: /let in/i }));

    await screen.findByText("Let in.");
    expect(capturedBody).toBeUndefined();
  });

  it("shows the server's message inline when admitting fails, without losing the knock", async () => {
    stubFetchRoutes({
      "POST /api/v1/household/invites/2/admit": {
        status: 409,
        body: { error: { code: "CONFLICT", message: "That Telegram account joined another household." } },
      },
    });
    renderCard({ invite: knockedInvite });

    fireEvent.click(screen.getByRole("button", { name: /let in/i }));

    expect(
      await screen.findByText("That Telegram account joined another household."),
    ).toBeInTheDocument();
    // Still knocked, not admitted: the mutation failed, so admit.data never
    // populated, and the code is still there to compare.
    expect(screen.getByText("4812")).toBeInTheDocument();
  });

  // "Not them" and "Get a new link" are the same mutation (useNewInviteLink)
  // -- this proves the knocked state's button actually reaches it and hands
  // the fresh link to the caller, not just that the button exists.
  it("asks before ending the tapped link, then hands the caller the fresh one", async () => {
    const onNewLink = vi.fn();
    stubFetchRoutes({
      "POST /api/v1/household/invites/2/link": {
        status: 200,
        body: { link: "https://t.me/HearthBot?start=inv_new", expiresAt: "2026-09-28T00:00:00Z" },
      },
    });
    renderCard({ invite: knockedInvite, onNewLink });

    fireEvent.click(screen.getByRole("button", { name: /not them/i }));
    // The in-page confirm step (never window.confirm -- this codebase's own
    // house rule, DiscardDraftControl.tsx among others): the request must
    // not fire until the confirm button is clicked too.
    expect(screen.getByText(/ends the tapped link/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /not them/i }));

    await waitFor(() => expect(onNewLink).toHaveBeenCalledWith("https://t.me/HearthBot?start=inv_new"));
  });

  // The bug this pins: a link this card mints itself has nowhere else to
  // live. PendingInvitesList.tsx's call site passes no `onNewLink` at all
  // (its own comment says why -- the list has no further use for one), so
  // if the card didn't hold what it mints, "Get a new link" would fetch a
  // fresh link and then throw it away, leaving the owner with no way to
  // share the very link they just asked for.
  it("holds and shows the link it mints itself, even with nobody listening for it", async () => {
    stubFetchRoutes({
      "POST /api/v1/household/invites/1/link": {
        status: 200,
        body: { link: "https://t.me/HearthBot?start=inv_fresh", expiresAt: "2026-09-28T00:00:00Z" },
      },
    });
    renderCard({ invite: waitingInvite }); // no link, no onNewLink

    fireEvent.click(screen.getByRole("button", { name: /get a new link/i }));
    fireEvent.click(screen.getByRole("button", { name: /get a new link/i }));

    expect(await screen.findByRole("button", { name: /copy/i })).toBeInTheDocument();
    expect(screen.getByText("https://t.me/HearthBot?start=inv_fresh")).toBeInTheDocument();
  });

  it("cancels out of the confirm step without sending a request", () => {
    const fetchMock = stubFetchRoutes({});
    renderCard({ invite: waitingInvite });

    fireEvent.click(screen.getByRole("button", { name: /get a new link/i }));
    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));

    expect(screen.getByRole("button", { name: /get a new link/i })).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("withdraws a knocked invite, after a confirm", async () => {
    const fetchMock = stubFetchRoutes({
      "DELETE /api/v1/household/invites/2": { status: 204, body: undefined },
      [`GET ${INVITES_URL}`]: { status: 200, body: [] },
    });
    renderCard({ invite: knockedInvite });

    fireEvent.click(screen.getByRole("button", { name: "Withdraw the invite to Christine" }));
    // The in-page confirm step (never window.confirm -- ApiTokenList's
    // Revoke and TelegramConnection's Disconnect give the reason): the
    // DELETE must not fire until "Yes, withdraw" is clicked too.
    expect(screen.getByText("Withdraw this invite? The link stops working.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Yes, withdraw" }));

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([input, init]) =>
            String(input) === `${INVITES_URL}/2` && init?.method === "DELETE",
        ),
      ).toBe(true),
    );
  });

  // Pins the one place WithdrawControl deliberately differs from its own
  // sibling NewLinkControl just above it in the source file: the error is
  // read outside the `isConfirming()` branch (see WithdrawControl's own
  // comment), so it survives useConfirmAction's collapse back to the
  // trigger. Reading it from inside that branch instead -- the shape
  // NewLinkControl actually uses -- would make this message never render,
  // because `isConfirming()` is already false again by the time the error
  // is set.
  it("shows the server's message when withdrawing fails, without losing the knock", async () => {
    stubFetchRoutes({
      "DELETE /api/v1/household/invites/2": {
        status: 409,
        body: { error: { code: "INVITE_ALREADY_ACCEPTED", message: "This invite has already been accepted." } },
      },
      [`GET ${INVITES_URL}`]: { status: 200, body: [knockedInvite] },
    });
    renderCard({ invite: knockedInvite });

    fireEvent.click(screen.getByRole("button", { name: "Withdraw the invite to Christine" }));
    fireEvent.click(screen.getByRole("button", { name: "Yes, withdraw" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("This invite has already been accepted.");
    // Still knocked: the failed withdraw didn't touch the invite, so the
    // code stays there to compare, and Withdraw is back for another try.
    expect(screen.getByText("4812")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Withdraw the invite to Christine" })).toBeInTheDocument();
  });

  it("sends no DELETE and returns to Withdraw when the confirm is cancelled", () => {
    const fetchMock = stubFetchRoutes({});
    renderCard({ invite: knockedInvite });

    fireEvent.click(screen.getByRole("button", { name: "Withdraw the invite to Christine" }));
    expect(screen.getByText("Withdraw this invite? The link stops working.")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Keep" }));

    expect(screen.getByRole("button", { name: "Withdraw the invite to Christine" })).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("renders nothing for a channel it does not own, so a caller cannot render Telegram-only controls over an email invite", () => {
    const { container } = renderCard({ invite: { ...waitingInvite, channel: "email" } });
    expect(container.querySelector("li")).toBeNull();
  });
});
