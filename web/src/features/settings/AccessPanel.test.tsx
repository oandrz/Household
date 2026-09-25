import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { meFixture } from "../marriage/agreementFixtures";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AccessPanel } from "./AccessPanel";

const ACCESS_URL = "/api/v1/household/access";
const EMPTY_ACCESS = { telegramEnabled: false, tokens: [], chats: [] };

function renderPanel() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <AccessPanel />
    </QueryClientProvider>,
  );
}

// meFixture's user id is "u-andreas".
const partnerChat = { memberId: "u-christine", memberName: "Christine", chatUsername: "chris_o", linkedAt: "2026-09-01T10:00:00Z" };
const myChat = { memberId: "u-andreas", memberName: "Andreas", chatUsername: "andreas_o", linkedAt: "2026-09-01T10:00:00Z" };

function pendingInvite(id: string, name: string) {
  return { id, name, email: "", role: "owner", capabilities: [], channel: "telegram", knock: null, expiresAt: "2026-12-01T00:00:00Z" };
}

afterEach(() => {
  vi.unstubAllGlobals();
  // jsdom has no scrollIntoView implementation at all -- the one test below
  // that clicks the pointer stubs it in inline so that click doesn't throw;
  // undo it here so it doesn't leak into a test that isn't expecting it.
  delete (Element.prototype as unknown as { scrollIntoView?: unknown }).scrollIntoView;
});

describe("AccessPanel", () => {
  it("lists the partner's chat, and mine only once, through my own connection", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      [`GET ${ACCESS_URL}`]: { status: 200, body: { telegramEnabled: true, tokens: [], chats: [partnerChat, myChat] } },
      "GET /api/v1/auth/telegram": {
        status: 200,
        body: { connected: true, chatUsername: "andreas_o", linkedAt: "2026-09-01T10:00:00Z" },
      },
      "GET /api/v1/household/invites": { status: 200, body: [] },
    });
    renderPanel();

    expect(await screen.findByText("@chris_o")).toBeInTheDocument();
    // TelegramConnection's own binding query and the access-list query settle
    // independently; "Disconnect" only renders once the binding query has
    // resolved and drawn my own connected row. Without this anchor,
    // findAllByText below can return as soon as it sees the access list's
    // row alone -- before TelegramConnection's row has appeared -- which
    // would let a de-dup regression (my own chat also drawn as a read-only
    // row) through undetected.
    await screen.findByRole("button", { name: "Disconnect" });
    expect(screen.getAllByText("@andreas_o")).toHaveLength(1);
  });

  it("hides the Linked chats group when Telegram is off", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      [`GET ${ACCESS_URL}`]: { status: 200, body: EMPTY_ACCESS },
      "GET /api/v1/household/invites": { status: 200, body: [] },
    });
    renderPanel();

    expect(await screen.findByText("No API tokens.")).toBeInTheDocument();
    expect(screen.queryByText("Linked chats")).not.toBeInTheDocument();
  });

  it("points an owner at pending invites in Members", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      [`GET ${ACCESS_URL}`]: { status: 200, body: EMPTY_ACCESS },
      "GET /api/v1/household/invites": { status: 200, body: [pendingInvite("i-1", "Jane"), pendingInvite("i-2", "Kid")] },
    });
    renderPanel();

    expect(await screen.findByRole("button", { name: "2 pending invites — in Members" })).toBeInTheDocument();
  });

  it("moves keyboard focus to the Members heading after the pointer scrolls there", async () => {
    // jsdom has no scrollIntoView -- without this stub the pointer's click
    // handler throws, which would fail the test for the wrong reason.
    Element.prototype.scrollIntoView = vi.fn();
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      [`GET ${ACCESS_URL}`]: { status: 200, body: EMPTY_ACCESS },
      "GET /api/v1/household/invites": { status: 200, body: [pendingInvite("i-1", "Jane")] },
    });
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <AccessPanel />
        {/* Stands in for MembersPanel's real <section id="members"> heading
            (MembersPanel.tsx) -- same id and tabIndex, so the pointer's
            scroll-and-focus targets exactly what the real page renders. */}
        <section id="members">
          <h2 id="members-heading" tabIndex={-1}>
            Members
          </h2>
        </section>
      </QueryClientProvider>,
    );

    const pointer = await screen.findByRole("button", { name: "1 pending invite — in Members" });
    pointer.focus();
    expect(document.activeElement).toBe(pointer);

    pointer.click();

    // Scroll target and focus target are deliberately different elements:
    // the whole #members card scrolls into view (so its own padding and
    // border are visible, not just the heading's text), while keyboard
    // focus moves to its heading specifically.
    expect(Element.prototype.scrollIntoView).toHaveBeenCalledOnce();
    expect((Element.prototype.scrollIntoView as ReturnType<typeof vi.fn>).mock.contexts[0]).toBe(
      document.getElementById("members"),
    );

    await waitFor(() => expect(document.activeElement).toBe(screen.getByText("Members")));
  });

  it("never asks a limited member's browser for the owner-only invite list", async () => {
    const owner = meFixture();
    const fetchMock = stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture({ membership: { ...owner.membership, role: "limited" } }) },
      [`GET ${ACCESS_URL}`]: { status: 200, body: EMPTY_ACCESS },
    });
    renderPanel();

    await screen.findByText("No API tokens.");
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes("/household/invites"))).toBe(false);
  });

  it("offers New token", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      [`GET ${ACCESS_URL}`]: { status: 200, body: EMPTY_ACCESS },
      "GET /api/v1/household/invites": { status: 200, body: [] },
    });
    renderPanel();
    expect(await screen.findByRole("button", { name: "New token" })).toBeInTheDocument();
  });

  // isOwner is computed from me.data, which is undefined while /auth/me is
  // still pending -- `undefined?.membership.role === "owner"` is false, the
  // same shape a real limited member's isOwner takes. Gating the subtitle on
  // me.isSuccess is what stops an owner's screen from reading, even for one
  // render, "Your own ways in besides a password."
  it("does not flash the limited-member subtitle for an owner while /auth/me is still loading", () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      [`GET ${ACCESS_URL}`]: { status: 200, body: EMPTY_ACCESS },
      "GET /api/v1/household/invites": { status: 200, body: [] },
    });
    renderPanel();

    // Asserted synchronously, before either query's stubbed fetch promise
    // has had a chance to resolve -- this is the exact frame the flash
    // happens in.
    expect(screen.queryByText("Your own ways in besides a password.")).not.toBeInTheDocument();
    expect(screen.queryByText("Every way into your household besides a password.")).not.toBeInTheDocument();
  });
});
