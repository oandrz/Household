// The Settings control an owner changes the household's time zone with.
// Changing it moves "today" for everyone in the household, so the tests are
// about who may do it, what is sent, and what happens when it is refused.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import type { Household, Me } from "../auth/schemas";
import { TimeZoneRow } from "./TimeZoneRow";

function householdFixture(timezone = "Asia/Singapore"): Household {
  return {
    id: "h-1",
    name: "Andreas & Christine",
    familyName: "Oentoro",
    primaryCurrency: "SGD",
    showSecondaryCurrency: true,
    secondaryCurrency: "IDR",
    fxRateMode: "auto",
    timezone,
  };
}

function meFixture(role: "owner" | "limited" = "owner"): Me {
  return {
    user: { id: "u-1", email: "andreas@hearth.family", displayName: "Andreas", avatarInitial: "A" },
    household: householdFixture(),
    membership: {
      id: "mem-1",
      householdId: "h-1",
      userId: "u-1",
      role,
      capabilities: ["calendar", "chores", "money", "marriage"],
    },
    capabilities: ["calendar", "chores", "money", "marriage"],
    spaces: [],
    isPlatformAdmin: false,
    features: {},
  };
}

function renderRow() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TimeZoneRow />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("TimeZoneRow", () => {
  it("shows an owner the household's zone, and saves the one they pick", async () => {
    let patched: unknown;
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/household": [
        { status: 200, body: householdFixture() },
        { status: 200, body: householdFixture("Pacific/Kiritimati") },
      ],
      "PATCH /api/v1/household": {
        status: 200,
        body: householdFixture("Pacific/Kiritimati"),
        capture: (body) => {
          patched = body;
        },
      },
    });
    renderRow();

    const select = await screen.findByLabelText("Time zone");
    expect(select).toHaveValue("Asia/Singapore");
    // Nothing to save until the choice differs from what is stored.
    expect(screen.getByRole("button", { name: "Save time zone" })).toBeDisabled();

    fireEvent.change(select, { target: { value: "Pacific/Kiritimati" } });
    fireEvent.click(screen.getByRole("button", { name: "Save time zone" }));

    // Only the zone is sent: PATCH /household leaves every omitted field as
    // it was.
    await waitFor(() => expect(patched).toEqual({ timezone: "Pacific/Kiritimati" }));
    await waitFor(() => expect(screen.getByLabelText("Time zone")).toHaveValue("Pacific/Kiritimati"));
  });

  // Changing the zone moves "today" for the whole household. The server
  // refuses it from a limited member; the screen does not offer it.
  it("shows a limited member the zone as text, with nothing to change", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture("limited") },
      "GET /api/v1/household": { status: 200, body: householdFixture() },
    });
    const { container } = renderRow();

    expect(await screen.findByText(/Asia\/Singapore/)).toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save time zone" })).not.toBeInTheDocument();
    // No label pointing at a control that is not there.
    expect(container.querySelector("label[for]")).toBeNull();
  });

  it("says so when the server refuses the zone, and keeps the choice on screen", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/household": { status: 200, body: householdFixture() },
      "PATCH /api/v1/household": {
        status: 422,
        body: { error: { code: "INVALID_TIMEZONE", message: "That time zone is not recognised." } },
      },
    });
    renderRow();

    const select = await screen.findByLabelText("Time zone");
    fireEvent.change(select, { target: { value: "Pacific/Pago_Pago" } });
    fireEvent.click(screen.getByRole("button", { name: "Save time zone" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("That time zone is not recognised.");
    expect(select).toHaveValue("Pacific/Pago_Pago");
  });

  // What the date is in the chosen zone, before saving: the one consequence
  // of this control a person can check against their own wall.
  it("says what today's date is in the zone being chosen", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-09-30T23:00:00Z"));
    try {
      stubFetchRoutes({
        "GET /api/v1/auth/me": { status: 200, body: meFixture() },
        "GET /api/v1/household": { status: 200, body: householdFixture() },
      });
      renderRow();

      const select = await screen.findByLabelText("Time zone");
      expect(screen.getByTestId("time-zone-today")).toHaveTextContent("Thu, 1 Oct 2026");

      fireEvent.change(select, { target: { value: "Pacific/Pago_Pago" } });
      expect(screen.getByTestId("time-zone-today")).toHaveTextContent("Wed, 30 Sep 2026");
    } finally {
      vi.useRealTimers();
    }
  });
});
