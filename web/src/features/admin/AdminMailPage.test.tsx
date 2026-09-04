// Follows AdminHouseholdsPage.test.tsx (the list) and AdminHouseholdPage.test.tsx
// (the drill-in): renderWithRouter plus stubFetchRoutes for every request,
// literal strings asserted.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AdminMailMessagePage, AdminMailPage } from "./AdminMailPage";
import { adminMailPath, OUTBOX_DEFAULT_LIMIT } from "./useAdminOutbox";
import type { AdminMailList, AdminMailMessage } from "./adminOutboxSchemas";

const MESSAGE_ID = "0OQ1sV2mB7hN4kR8xT3wZq";

function list(overrides: Partial<AdminMailList> = {}): AdminMailList {
  return {
    messages: [
      {
        id: MESSAGE_ID,
        to: "chris@example.com",
        subject: "Your Hearth sign-in link",
        sentAt: "2026-09-04T09:12:33Z",
      },
    ],
    total: 1,
    truncated: false,
    ...overrides,
  };
}

function message(overrides: Partial<AdminMailMessage> = {}): AdminMailMessage {
  return {
    id: MESSAGE_ID,
    to: "chris@example.com",
    subject: "Your Hearth sign-in link",
    sentAt: "2026-09-04T09:12:33Z",
    links: ["https://oink.mywire.org/sign-in/magic?token=abc123"],
    text: "Open https://oink.mywire.org/sign-in/magic?token=abc123 to sign in.",
    ...overrides,
  };
}

describe("AdminMailPage", () => {
  it("lists what the outbox holds, with no body text on a row", async () => {
    stubFetchRoutes({
      [`GET ${adminMailPath(OUTBOX_DEFAULT_LIMIT)}`]: {
        status: 200,
        body: list(),
      },
    });
    renderWithRouter(<AdminMailPage />);

    expect(await screen.findByText("chris@example.com")).toBeInTheDocument();
    expect(
      screen.getByText("Your Hearth sign-in link"),
    ).toBeInTheDocument();
  });

  it("says the inspector is not configured, and names the variable", async () => {
    stubFetchRoutes({
      [`GET ${adminMailPath(OUTBOX_DEFAULT_LIMIT)}`]: {
        status: 503,
        body: {
          error: {
            code: "MAIL_INSPECTOR_NOT_CONFIGURED",
            message: "The message inspector is not configured.",
          },
        },
      },
    });
    renderWithRouter(<AdminMailPage />);

    expect(await screen.findByText(/MAILPIT_API_URL/)).toBeInTheDocument();
  });

  it("says Mailpit is not answering, which is a different message", async () => {
    stubFetchRoutes({
      [`GET ${adminMailPath(OUTBOX_DEFAULT_LIMIT)}`]: {
        status: 502,
        body: {
          error: {
            code: "MAIL_UPSTREAM_UNAVAILABLE",
            message: "Mailpit did not answer.",
          },
        },
      },
    });
    renderWithRouter(<AdminMailPage />);

    expect(await screen.findByText(/not answering/i)).toBeInTheDocument();
    expect(screen.queryByText(/MAILPIT_API_URL/)).not.toBeInTheDocument();
  });

  it("says the store is not durable, so a missing message is not a bug", async () => {
    stubFetchRoutes({
      [`GET ${adminMailPath(OUTBOX_DEFAULT_LIMIT)}`]: {
        status: 200,
        body: list(),
      },
    });
    renderWithRouter(<AdminMailPage />);

    expect(
      await screen.findByText(/Mailpit keeps these only until it restarts/i),
    ).toBeInTheDocument();
  });

  // Decision 10's third row. A 404 on this route is Mailpit having dropped
  // the message, not the admin surface disappearing -- if this rendered
  // nothing, the operator would meet a blank screen for an ordinary event.
  it("says a dropped message is gone rather than rendering nothing", async () => {
    stubFetchRoutes({
      [`GET /api/v1/admin/mail/${MESSAGE_ID}`]: {
        status: 404,
        body: {
          error: { code: "NOT_FOUND", message: "That could not be found." },
        },
      },
    });
    renderWithRouter(<AdminMailMessagePage messageId={MESSAGE_ID} />);

    expect(
      await screen.findByText(/no longer holds this message/i),
    ).toBeInTheDocument();
  });

  it("shows a message's links with a copy control, and never renders html", async () => {
    stubFetchRoutes({
      [`GET /api/v1/admin/mail/${MESSAGE_ID}`]: {
        status: 200,
        // A literal tag in the body: if this were ever handed to
        // dangerouslySetInnerHTML, "never rendered" would show as bold text
        // with no visible angle brackets. Asserting the brackets are still
        // there is what actually pins "never renders html" -- a body with no
        // markup in it would pass this test either way.
        body: message({
          text: "Open https://oink.mywire.org/sign-in/magic?token=abc123 to sign in. <b>never rendered</b>",
        }),
      },
    });
    renderWithRouter(<AdminMailMessagePage messageId={MESSAGE_ID} />);

    expect(
      await screen.findByText(
        "https://oink.mywire.org/sign-in/magic?token=abc123",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /copy link/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/<b>never rendered<\/b>/),
    ).toBeInTheDocument();
  });

  it("copies the link to the clipboard, guarding a clipboard that may not exist", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });

    stubFetchRoutes({
      [`GET /api/v1/admin/mail/${MESSAGE_ID}`]: {
        status: 200,
        body: message(),
      },
    });
    renderWithRouter(<AdminMailMessagePage messageId={MESSAGE_ID} />);

    fireEvent.click(
      await screen.findByRole("button", { name: /copy link/i }),
    );
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith(
        "https://oink.mywire.org/sign-in/magic?token=abc123",
      ),
    );

    // @ts-expect-error -- deleting a property that TS believes is required,
    // to restore jsdom's default (no clipboard) for every test after this one.
    delete navigator.clipboard;
  });
});
