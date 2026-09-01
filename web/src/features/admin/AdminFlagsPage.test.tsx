// Follows GoalsPage.test.tsx's own shape: renderWithRouter plus
// stubFetchRoutes for every request, literal strings asserted rather than
// any copy this file might one day extract into its own module.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AdminFlagsPage } from "./AdminFlagsPage";
import type { AdminFlag } from "./adminSchemas";

function flagFixture(overrides: Partial<AdminFlag> = {}): AdminFlag {
  return {
    key: "family_calendar",
    description: "The shared family calendar space.",
    default: true,
    globalSet: false,
    globalEnabled: false,
    effective: true,
    orphaned: false,
    overrides: [],
    ...overrides,
  };
}

function flagsResponse(flags: AdminFlag[]) {
  return { status: 200, body: { flags } };
}

describe("AdminFlagsPage", () => {
  // The whole point of the screen: "no global opinion", "explicitly on" and
  // "explicitly off" are three states, not a boolean plus a maybe.
  it("renders a flag with no global opinion, one set on, and one set off distinguishably", async () => {
    const flags = [
      flagFixture({ key: "family_calendar", globalSet: false }),
      flagFixture({ key: "signups_open", globalSet: true, globalEnabled: true, effective: true }),
      flagFixture({ key: "telegram_signin", globalSet: true, globalEnabled: false, effective: false }),
    ];
    stubFetchRoutes({ "GET /api/v1/admin/flags": flagsResponse(flags) });

    renderWithRouter(<AdminFlagsPage />);

    const noOpinionRow = within(await screen.findByTestId("flag-row-family_calendar"));
    expect(noOpinionRow.getByText("Default")).toHaveClass("bg-canvas");
    expect(noOpinionRow.getByRole("button", { name: "On" })).toHaveAttribute("aria-pressed", "false");
    expect(noOpinionRow.getByRole("button", { name: "Off" })).toHaveAttribute("aria-pressed", "false");

    const onRow = within(screen.getByTestId("flag-row-signups_open"));
    expect(onRow.getByRole("button", { name: "On" })).toHaveAttribute("aria-pressed", "true");

    const offRow = within(screen.getByTestId("flag-row-telegram_signin"));
    expect(offRow.getByRole("button", { name: "Off" })).toHaveAttribute("aria-pressed", "true");
  });

  it("shows a flag's household overrides only once the disclosure is opened", async () => {
    const flags = [
      flagFixture({
        key: "signups_open",
        overrides: [{ householdId: "h1", householdName: "Oentoro household", enabled: true }],
      }),
    ];
    stubFetchRoutes({ "GET /api/v1/admin/flags": flagsResponse(flags) });

    renderWithRouter(<AdminFlagsPage />);

    const row = within(await screen.findByTestId("flag-row-signups_open"));
    expect(row.queryByText("Oentoro household")).not.toBeInTheDocument();

    fireEvent.click(row.getByRole("button", { name: "1 household override" }));

    expect(row.getByText("Oentoro household")).toBeInTheDocument();
  });

  // Clicking a global toggle is a PUT to the flag's own route, carrying the
  // clicked state -- useSetGlobalFlag's own contract in useAdmin.ts.
  it("issues a PUT to the flag's route when its global toggle is clicked", async () => {
    let putBody: unknown;
    const flags = [flagFixture({ key: "family_calendar", globalSet: false })];
    stubFetchRoutes({
      "GET /api/v1/admin/flags": flagsResponse(flags),
      "PUT /api/v1/admin/flags/family_calendar": {
        status: 200,
        body: { flags: [flagFixture({ key: "family_calendar", globalSet: true, globalEnabled: true, effective: true })] },
        capture: (body) => { putBody = body; },
      },
    });

    renderWithRouter(<AdminFlagsPage />);

    const row = within(await screen.findByTestId("flag-row-family_calendar"));
    fireEvent.click(row.getByRole("button", { name: "On" }));

    await waitFor(() => expect(putBody).toEqual({ enabled: true }));
    // The refreshed list this route answers with replaces the cache, so the
    // segmented control now reads "On" as the current state without a
    // second GET.
    await waitFor(() => expect(row.getByRole("button", { name: "On" })).toHaveAttribute("aria-pressed", "true"));
  });

  // Deleting an override is not the same request as setting it false --
  // handleClearHouseholdFlag's own comment on why the two are different
  // states -- so the disclosure's "Remove" control must reach the DELETE
  // route, not the PUT one every toggle above uses.
  it("issues a DELETE, not a PUT, when a household override is removed", async () => {
    let deleteCalled = false;
    const flags = [
      flagFixture({
        key: "signups_open",
        overrides: [{ householdId: "h1", householdName: "Oentoro household", enabled: true }],
      }),
    ];
    stubFetchRoutes({
      "GET /api/v1/admin/flags": flagsResponse(flags),
      "DELETE /api/v1/admin/flags/signups_open/households/h1": {
        status: 200,
        body: { flags: [flagFixture({ key: "signups_open", overrides: [] })] },
        capture: () => { deleteCalled = true; },
      },
    });

    renderWithRouter(<AdminFlagsPage />);

    const row = within(await screen.findByTestId("flag-row-signups_open"));
    fireEvent.click(row.getByRole("button", { name: "1 household override" }));
    fireEvent.click(row.getByRole("button", { name: "Remove signups_open override for Oentoro household" }));

    await waitFor(() => expect(deleteCalled).toBe(true));
  });

  // An orphaned override -- one naming a flag this build's registry no
  // longer defines -- is safe to delete and enables nothing; it must not be
  // mistaken for one of the real, currently-registered flags above it.
  it("lists an orphaned flag's overrides in their own group, with only a delete control", async () => {
    const flags = [
      flagFixture({ key: "family_calendar" }),
      flagFixture({
        key: "old_retired_flag",
        orphaned: true,
        overrides: [{ householdId: "h1", householdName: "Oentoro household", enabled: true }],
      }),
    ];
    stubFetchRoutes({ "GET /api/v1/admin/flags": flagsResponse(flags) });

    renderWithRouter(<AdminFlagsPage />);

    await screen.findByTestId("flag-row-family_calendar");
    expect(screen.queryByTestId("flag-row-old_retired_flag")).not.toBeInTheDocument();

    expect(screen.getByText("Orphaned — safe to delete")).toBeInTheDocument();
    const orphanedRow = within(screen.getByTestId("orphaned-flag-row-old_retired_flag"));
    expect(orphanedRow.getByText("Oentoro household")).toBeInTheDocument();
    expect(
      orphanedRow.getByRole("button", { name: "Remove old_retired_flag override for Oentoro household" }),
    ).toBeInTheDocument();
  });
});
