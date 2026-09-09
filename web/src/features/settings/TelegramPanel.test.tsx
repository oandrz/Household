import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { telegramPollInterval } from "./copy";
import { TelegramPanel } from "./TelegramPanel";

const BINDING_URL = "/api/v1/auth/telegram";
const LINK_START_URL = "/api/v1/auth/telegram/link";
function linkStatusUrl(id: string) {
  return `/api/v1/auth/telegram/link/${id}`;
}
function linkConfirmUrl(id: string) {
  return `/api/v1/auth/telegram/link/${id}/confirm`;
}

function renderPanel() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TelegramPanel />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

// Unlike NotificationsPanel, this panel never calls useMe() -- spec decision
// 10 makes it available to any member, not owners only, so there is no
// GET /api/v1/auth/me stub to register anywhere below.
describe("TelegramPanel", () => {
  it("shows the connected chat and offers Disconnect", async () => {
    const fetchMock = stubFetchRoutes({
      [`GET ${BINDING_URL}`]: [
        {
          status: 200,
          body: { connected: true, chatUsername: "andreas_o", linkedAt: "2026-09-01T10:00:00Z" },
        },
        { status: 200, body: { connected: false } },
      ],
      [`DELETE ${BINDING_URL}`]: { status: 200, body: { connected: false } },
    });
    renderPanel();

    expect(await screen.findByText("@andreas_o")).toBeInTheDocument();
    const disconnectButton = screen.getByRole("button", { name: "Disconnect" });
    expect(disconnectButton).toBeInTheDocument();

    fireEvent.click(disconnectButton);

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input) === BINDING_URL && (init?.method ?? "").toUpperCase() === "DELETE",
      );
      expect(call).toBeDefined();
    });

    expect(await screen.findByRole("button", { name: "Connect Telegram" })).toBeInTheDocument();
  });

  it("names the chat that opened the link before asking to confirm", async () => {
    const openMock = vi.fn();
    vi.stubGlobal("open", openMock);
    stubFetchRoutes({
      [`GET ${BINDING_URL}`]: [
        { status: 200, body: { connected: false } },
        { status: 200, body: { connected: true, chatUsername: "andreas_o", linkedAt: "2026-09-01T10:00:00Z" } },
      ],
      [`POST ${LINK_START_URL}`]: {
        status: 200,
        body: { id: "link-1", url: "https://t.me/HearthBot?start=abc123", expiresAt: "2026-09-09T10:10:00Z" },
      },
      [`GET ${linkStatusUrl("link-1")}`]: {
        status: 200,
        body: { status: "pending", chatUsername: "andreas_o" },
      },
      [`POST ${linkConfirmUrl("link-1")}`]: {
        status: 200,
        body: { connected: true, chatUsername: "andreas_o", linkedAt: "2026-09-01T10:00:00Z" },
      },
    });
    renderPanel();

    fireEvent.click(await screen.findByRole("button", { name: "Connect Telegram" }));

    // The deep link opens with the exact args the brief resolves on --
    // "_blank" and "noopener", not the popup-then-navigate trick SignInScreen
    // uses. handleConnect's own comment covers the trade-off this simpler
    // shape accepts (a WebKit popup block) and the fallback link the waiting
    // view renders because of it.
    await waitFor(() =>
      expect(openMock).toHaveBeenCalledWith(
        "https://t.me/HearthBot?start=abc123",
        "_blank",
        "noopener",
      ),
    );

    // The chat that redeemed the link must be visible -- a confirm the
    // person cannot check against their own chat is a confirm that will
    // always be clicked (decision 7).
    expect(await screen.findByText(/@andreas_o/)).toBeInTheDocument();
    const confirmButton = screen.getByRole("button", { name: "Confirm" });
    expect(confirmButton).toBeInTheDocument();

    fireEvent.click(confirmButton);

    expect(await screen.findByRole("button", { name: "Disconnect" })).toBeInTheDocument();
  });

  it("stops polling and offers a fresh start when the link expires", async () => {
    vi.stubGlobal("open", vi.fn());
    stubFetchRoutes({
      [`GET ${BINDING_URL}`]: { status: 200, body: { connected: false } },
      [`POST ${LINK_START_URL}`]: {
        status: 200,
        body: { id: "link-2", url: "https://t.me/HearthBot?start=xyz", expiresAt: "2026-09-09T10:10:00Z" },
      },
      [`GET ${linkStatusUrl("link-2")}`]: { status: 200, body: { status: "expired" } },
    });
    renderPanel();

    fireEvent.click(await screen.findByRole("button", { name: "Connect Telegram" }));

    expect(await screen.findByText(/expired/i)).toBeInTheDocument();
    const startOverButton = screen.getByRole("button", { name: "Start over" });
    expect(startOverButton).toBeInTheDocument();

    // The rule under test: an expired (or refused, or connected) link stops
    // TanStack Query's refetchInterval rather than continuing to poll a nonce
    // that can never change again. Asserted directly against the exported
    // interval function -- proving this over real elapsed time would mean
    // waiting out several 3-second polls in the test itself.
    expect(telegramPollInterval("expired")).toBe(false);
    expect(telegramPollInterval("connected")).toBe(false);
    expect(telegramPollInterval("refused")).toBe(false);
    expect(telegramPollInterval("waiting")).toBe(3000);
    expect(telegramPollInterval("pending")).toBe(3000);

    // Offers a fresh start: it abandons the dead link and returns to the
    // same control a first-time connect uses, rather than silently
    // re-minting one on the person's behalf.
    fireEvent.click(startOverButton);
    expect(await screen.findByRole("button", { name: "Connect Telegram" })).toBeInTheDocument();
  });

  it("still offers a way out when the status poll itself fails", async () => {
    // Before this fix, a failed poll rendered the error line and stopped
    // there: none of the waiting/pending/refused/expired blocks match
    // linkStatus.isError, so there was no button at all and the only way
    // out was a page reload. "Start over" has to render here too.
    vi.stubGlobal("open", vi.fn());
    stubFetchRoutes({
      [`GET ${BINDING_URL}`]: { status: 200, body: { connected: false } },
      [`POST ${LINK_START_URL}`]: {
        status: 200,
        body: { id: "link-3", url: "https://t.me/HearthBot?start=broken", expiresAt: "2026-09-09T10:10:00Z" },
      },
      [`GET ${linkStatusUrl("link-3")}`]: {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Something broke." } },
      },
    });
    renderPanel();

    fireEvent.click(await screen.findByRole("button", { name: "Connect Telegram" }));

    expect(
      await screen.findByText("Couldn't check that link's status. Please try again."),
    ).toBeInTheDocument();
    const startOverButton = screen.getByRole("button", { name: "Start over" });
    expect(startOverButton).toBeInTheDocument();

    fireEvent.click(startOverButton);
    expect(await screen.findByRole("button", { name: "Connect Telegram" })).toBeInTheDocument();
  });

  it("shows the server's reason when the chat belongs to someone else", async () => {
    vi.stubGlobal("open", vi.fn());
    stubFetchRoutes({
      [`GET ${BINDING_URL}`]: { status: 200, body: { connected: false } },
      [`POST ${LINK_START_URL}`]: {
        status: 200,
        body: { id: "link-4", url: "https://t.me/HearthBot?start=refused", expiresAt: "2026-09-09T10:10:00Z" },
      },
      [`GET ${linkStatusUrl("link-4")}`]: {
        status: 200,
        body: {
          status: "refused",
          chatUsername: "stranger123",
          reason: "That Telegram chat is already connected to another Hearth account.",
        },
      },
    });
    renderPanel();

    fireEvent.click(await screen.findByRole("button", { name: "Connect Telegram" }));

    expect(
      await screen.findByText("That Telegram chat is already connected to another Hearth account."),
    ).toBeInTheDocument();
    // The bland-to-the-chat, specific-to-the-session asymmetry (decision 2)
    // means no Confirm control belongs on a refused link.
    expect(screen.queryByRole("button", { name: "Confirm" })).not.toBeInTheDocument();
  });

  it("renders nothing when the install has no Telegram bot configured", async () => {
    stubFetchRoutes({
      [`GET ${BINDING_URL}`]: {
        status: 404,
        body: { error: { code: "NOT_FOUND", message: "That endpoint does not exist." } },
      },
    });
    const { container } = renderPanel();

    await waitFor(() => expect(container).toBeEmptyDOMElement());
    expect(screen.queryByText("Telegram")).not.toBeInTheDocument();
  });

  it("names the chat with no Telegram username honestly instead of a bare @", async () => {
    // Decision 7's whole point is giving the person one piece of evidence to
    // check a confirm against -- when Telegram sent no @username
    // (update.go's senderName, "" in that case -- never a first name, which
    // is attacker-chosen), there is no evidence to give, and a bare "@"
    // would hide that gap rather than admit it.
    vi.stubGlobal("open", vi.fn());
    stubFetchRoutes({
      [`GET ${BINDING_URL}`]: { status: 200, body: { connected: false } },
      [`POST ${LINK_START_URL}`]: {
        status: 200,
        body: { id: "link-5", url: "https://t.me/HearthBot?start=noname", expiresAt: "2026-09-09T10:10:00Z" },
      },
      [`GET ${linkStatusUrl("link-5")}`]: {
        status: 200,
        body: { status: "pending" },
      },
    });
    renderPanel();

    fireEvent.click(await screen.findByRole("button", { name: "Connect Telegram" }));

    expect(await screen.findByText("a Telegram chat with no username")).toBeInTheDocument();
    expect(screen.queryByText("@undefined")).not.toBeInTheDocument();
  });

  it("offers the deep link as a plain fallback while waiting, for a blocked popup", async () => {
    // handleConnect's own comment: this panel's window.open runs after the
    // mutation resolves, which WebKit's popup gate can block (unlike
    // SignInScreen's pre-opened-tab trick). The waiting view's <a> is the
    // mitigation -- a blocked popup still leaves a real link to click.
    vi.stubGlobal("open", vi.fn());
    stubFetchRoutes({
      [`GET ${BINDING_URL}`]: { status: 200, body: { connected: false } },
      [`POST ${LINK_START_URL}`]: {
        status: 200,
        body: { id: "link-6", url: "https://t.me/HearthBot?start=waiting", expiresAt: "2026-09-09T10:10:00Z" },
      },
      [`GET ${linkStatusUrl("link-6")}`]: { status: 200, body: { status: "waiting" } },
    });
    renderPanel();

    fireEvent.click(await screen.findByRole("button", { name: "Connect Telegram" }));

    const fallbackLink = await screen.findByRole("link", { name: "Open Telegram" });
    expect(fallbackLink).toHaveAttribute("href", "https://t.me/HearthBot?start=waiting");
  });
});
