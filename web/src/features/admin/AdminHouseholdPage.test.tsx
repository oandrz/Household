import { screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AdminHouseholdPage } from "./AdminHouseholdPage";
import type { AdminHouseholdPage as PageData } from "./adminDirectorySchemas";

function page(overrides: Partial<PageData> = {}): PageData {
  return {
    household: {
      id: "h1",
      name: "Oentoro",
      familyName: "Oentoro",
      createdAt: "2026-08-15T02:11:09Z",
      primaryCurrency: "SGD",
    },
    members: [
      {
        userId: "u1",
        name: "Andreas",
        email: "andreas@example.org",
        channel: "email",
        role: "owner",
        capabilities: ["calendar", "chores", "money", "marriage"],
        lastActiveAt: "2026-09-02T07:40:12Z",
      },
      {
        userId: "u2",
        name: "Kid",
        email: null,
        channel: "telegram",
        role: "limited",
        capabilities: ["calendar"],
        lastActiveAt: null,
      },
    ],
    pendingInvites: [
      {
        name: "Christine",
        email: "c@example.org",
        role: "owner",
        invitedByName: "Andreas",
        expiresAt: "2026-09-05T02:11:09Z",
      },
    ],
    lockout: null,
    ...overrides,
  };
}

const route = "GET /api/v1/admin/households/h1";

describe("AdminHouseholdPage", () => {
  it("renders the header, members with channel and role, and the pending invite", async () => {
    stubFetchRoutes({ [route]: { status: 200, body: page() } });
    renderWithRouter(<AdminHouseholdPage householdId="h1" />);

    expect(
      await screen.findByRole("heading", { name: "Oentoro" }),
    ).toBeInTheDocument();
    const members = within(screen.getByRole("table", { name: "Members" }));
    expect(members.getByText("andreas@example.org")).toBeInTheDocument();
    expect(members.getByText("Telegram")).toBeInTheDocument();
    // getAllByText, not getByText: MemberRow renders lastActive twice for
    // the responsive collapse (once in the md:hidden cell, once in the
    // dedicated column), and u2's lastActiveAt is null, so both copies read
    // "never" -- the adjacent Owner assertion below already uses the plural
    // form for the identical reason.
    expect(members.getAllByText("never").length).toBeGreaterThan(0);
    expect(members.getAllByText("Owner").length).toBeGreaterThan(0);

    const invites = within(
      screen.getByRole("region", { name: "Pending invites" }),
    );
    expect(invites.getByText("c@example.org")).toBeInTheDocument();
    expect(invites.getByText(/invited by Andreas/)).toBeInTheDocument();
  });

  it("shows the lockout callout only when the household is locked", async () => {
    stubFetchRoutes({ [route]: { status: 200, body: page() } });
    const { unmount } = renderWithRouter(
      <AdminHouseholdPage householdId="h1" />,
    );
    await screen.findByRole("heading", { name: "Oentoro" });
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    unmount();

    stubFetchRoutes({
      [route]: {
        status: 200,
        body: page({
          lockout: {
            lockedUntil: new Date(Date.now() + 14 * 60_000).toISOString(),
          },
        }),
      },
    });
    renderWithRouter(<AdminHouseholdPage householdId="h1" />);
    const callout = await screen.findByRole("status");
    expect(callout).toHaveTextContent(/Sign-in is locked until/);
    expect(callout).toHaveTextContent("adminctl unlock-household");
  });

  it("says none pending when there are no invites", async () => {
    stubFetchRoutes({
      [route]: { status: 200, body: page({ pendingInvites: [] }) },
    });
    renderWithRouter(<AdminHouseholdPage householdId="h1" />);
    expect(await screen.findByText("None pending.")).toBeInTheDocument();
  });

  // Reconciled against the tree: NotFoundScreen renders a <p>, not a
  // heading (features/shell/NotFoundScreen.tsx), and router.test.tsx's own
  // "shows the app's ordinary not-found page to a non-admin visiting
  // /admin/flags" asserts this literal text and this literal envelope
  // shape -- copied verbatim rather than the brief's getByRole("heading").
  it("renders the not-found screen for a 404, the page a non-admin would see", async () => {
    stubFetchRoutes({
      [route]: {
        status: 404,
        body: {
          error: {
            code: "NOT_FOUND",
            message: "That endpoint does not exist.",
          },
        },
      },
    });
    renderWithRouter(<AdminHouseholdPage householdId="h1" />);
    await waitFor(() =>
      expect(screen.getByText("Page not found.")).toBeInTheDocument(),
    );
  });
});
