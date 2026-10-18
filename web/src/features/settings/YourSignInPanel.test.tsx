import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { meFixture } from "../marriage/agreementFixtures";
import { stubFetchRoutes } from "../../test/fetchStub";
import { renderWithRouter } from "../../test/renderWithRouter";
import { YourSignInPanel } from "./YourSignInPanel";

afterEach(() => vi.unstubAllGlobals());

function fillPasswords(current: string, next: string) {
  fireEvent.change(screen.getByLabelText("Current password"), { target: { value: current } });
  fireEvent.change(screen.getByLabelText("New password"), { target: { value: next } });
  fireEvent.click(screen.getByRole("button", { name: "Change password" }));
}

describe("YourSignInPanel", () => {
  it("changes the password and stays on the page", async () => {
    let posted: unknown;
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "POST /api/v1/auth/password": { status: 200, body: meFixture(), capture: (b) => (posted = b) },
    });
    const { router } = renderWithRouter(<YourSignInPanel />, "/settings");
    await screen.findByLabelText("Current password");
    fillPasswords("old-password-123", "new-password-456");

    expect(await screen.findByText(/Password changed/)).toBeInTheDocument();
    expect(posted).toEqual({ currentPassword: "old-password-123", newPassword: "new-password-456" });
    expect(router.state.location.pathname).toBe("/settings");
  });

  it("shows the server's words for a wrong current password and does not sign out", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "POST /api/v1/auth/password": {
        status: 403,
        body: { error: { code: "CURRENT_PASSWORD_WRONG", message: "That isn't your current password." } },
      },
    });
    const { router } = renderWithRouter(<YourSignInPanel />, "/settings");
    await screen.findByLabelText("Current password");
    fillPasswords("typo", "new-password-456");

    expect(await screen.findByRole("alert")).toHaveTextContent("That isn't your current password.");
    expect(router.state.location.pathname).toBe("/settings");
  });

  it("hides the password form for an account with no email", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture({ user: { ...meFixture().user, email: "" } }) },
    });
    renderWithRouter(<YourSignInPanel />, "/settings");
    await screen.findByRole("button", { name: "Sign out everywhere" });
    expect(screen.queryByLabelText("Current password")).not.toBeInTheDocument();
  });

  it("asks first, names what will happen, then signs out to the sign-in page", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "POST /api/v1/auth/sign-out-everywhere": { status: 204, body: null },
    });
    const { router } = renderWithRouter(<YourSignInPanel />, "/settings");
    fireEvent.click(await screen.findByRole("button", { name: "Sign out everywhere" }));

    expect(screen.getByText(/including this one\. Your API tokens are deleted and your Telegram chat is disconnected, so scripts/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Sign out everywhere now" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/sign-in"));
  });

  it("tells an account with no email that its chat stays connected", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture({ user: { ...meFixture().user, email: "" } }) },
    });
    renderWithRouter(<YourSignInPanel />, "/settings");
    fireEvent.click(await screen.findByRole("button", { name: "Sign out everywhere" }));
    expect(screen.getByText(/Your Telegram chat stays connected, because it is how you sign in\./)).toBeInTheDocument();
  });
});
