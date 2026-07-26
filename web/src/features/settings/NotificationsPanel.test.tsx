// Not part of the task-20 brief's enumerated MembersPanel behaviours, but
// added because the partial-PATCH semantics (one toggle, one field) and the
// owner-only gating are non-trivial and untested otherwise.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import type { Me } from "../auth/schemas";
import { NotificationsPanel } from "./NotificationsPanel";

const ME_URL = "/api/v1/auth/me";
const PREFS_URL = "/api/v1/notification-preferences";

function meFixture(role: "owner" | "limited" = "owner"): Me {
  return {
    user: { id: "u-1", email: "andreas@hearth.family", displayName: "Andreas", avatarInitial: "A" },
    household: {
      id: "h-1",
      name: "Andreas & Christine",
      familyName: "Oentoro",
      primaryCurrency: "SGD",
      showSecondaryCurrency: true,
      secondaryCurrency: "IDR",
      fxRateMode: "auto",
    },
    membership: {
      id: "mem-1",
      householdId: "h-1",
      userId: "u-1",
      role,
      capabilities: ["calendar", "chores", "money", "marriage"],
    },
    capabilities: ["calendar", "chores", "money", "marriage"],
    spaces: [],
  };
}

const preferences = {
  billReminders: true,
  overspendAlerts: true,
  retroReminder: true,
  weeklyDigest: false,
};

function renderPanel() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <NotificationsPanel />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("NotificationsPanel", () => {
  it("renders the design's four toggle labels with their current state", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${PREFS_URL}`]: { status: 200, body: preferences },
    });
    renderPanel();

    expect(
      await screen.findByText("Bill due reminders (3 days before)"),
    ).toBeInTheDocument();
    expect(screen.getByText("Budget over-spend alerts")).toBeInTheDocument();
    expect(screen.getByText("Monthly retro reminder")).toBeInTheDocument();
    expect(screen.getByText("Weekly family digest (Sun 8am)")).toBeInTheDocument();

    expect(
      screen.getByRole("switch", { name: "Weekly family digest (Sun 8am)" }),
    ).toHaveAttribute("aria-checked", "false");
    expect(
      screen.getByRole("switch", { name: "Monthly retro reminder" }),
    ).toHaveAttribute("aria-checked", "true");
  });

  it("issues a partial PATCH carrying only the toggled field", async () => {
    const fetchMock = stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${PREFS_URL}`]: { status: 200, body: preferences },
      [`PATCH ${PREFS_URL}`]: {
        status: 200,
        body: { ...preferences, weeklyDigest: true },
      },
    });
    renderPanel();

    await screen.findByText("Weekly family digest (Sun 8am)");
    fireEvent.click(screen.getByRole("switch", { name: "Weekly family digest (Sun 8am)" }));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input) === PREFS_URL && (init?.method ?? "").toUpperCase() === "PATCH",
      );
      expect(call).toBeDefined();
      expect(JSON.parse(call![1]!.body as string)).toEqual({ weeklyDigest: true });
    });
  });

  it("disables every toggle for a non-owner viewer", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("limited") },
      [`GET ${PREFS_URL}`]: { status: 200, body: preferences },
    });
    renderPanel();

    await screen.findByText("Weekly family digest (Sun 8am)");
    for (const name of [
      "Bill due reminders (3 days before)",
      "Budget over-spend alerts",
      "Monthly retro reminder",
      "Weekly family digest (Sun 8am)",
    ]) {
      expect(screen.getByRole("switch", { name })).toBeDisabled();
    }
  });
});
