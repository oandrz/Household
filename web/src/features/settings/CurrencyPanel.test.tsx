// Not part of the task-20 brief's enumerated MembersPanel behaviours, but
// added because the owner-only PATCH gating and the currency-label
// derivation are non-trivial and untested otherwise.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import type { Household, Me } from "../auth/schemas";
import { CurrencyPanel } from "./CurrencyPanel";

const ME_URL = "/api/v1/auth/me";
const HOUSEHOLD_URL = "/api/v1/household";

function householdFixture(overrides: Partial<Household> = {}): Household {
  return {
    id: "h-1",
    name: "Andreas & Christine",
    familyName: "Oentoro",
    primaryCurrency: "SGD",
    showSecondaryCurrency: true,
    secondaryCurrency: "IDR",
    fxRateMode: "auto",
    ...overrides,
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
  };
}

function renderPanel() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <CurrencyPanel />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("CurrencyPanel", () => {
  it("renders the primary currency with its symbol", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${HOUSEHOLD_URL}`]: { status: 200, body: householdFixture() },
    });
    renderPanel();

    expect(await screen.findByText("SGD (S$)")).toBeInTheDocument();
    expect(screen.getByText("Show IDR equivalents")).toBeInTheDocument();
  });

  it("issues a PATCH toggling showSecondaryCurrency for an owner", async () => {
    const fetchMock = stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("owner") },
      [`GET ${HOUSEHOLD_URL}`]: { status: 200, body: householdFixture({ showSecondaryCurrency: true }) },
      [`PATCH ${HOUSEHOLD_URL}`]: {
        status: 200,
        body: householdFixture({ showSecondaryCurrency: false }),
      },
    });
    renderPanel();

    await screen.findByText("SGD (S$)");
    fireEvent.click(screen.getByRole("switch", { name: /show secondary currency/i }));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input) === HOUSEHOLD_URL && (init?.method ?? "").toUpperCase() === "PATCH",
      );
      expect(call).toBeDefined();
      expect(JSON.parse(call![1]!.body as string)).toEqual({ showSecondaryCurrency: false });
    });
  });

  it("disables the toggle for a non-owner viewer", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("limited") },
      [`GET ${HOUSEHOLD_URL}`]: { status: 200, body: householdFixture() },
    });
    renderPanel();

    await screen.findByText("SGD (S$)");
    expect(screen.getByRole("switch", { name: /show secondary currency/i })).toBeDisabled();
  });
});
