// The Cashflow tab and year live in the address, and the tabs are linked for
// screen readers. Mounts the real route tree so the address is the real one.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { routeTree } from "../../routes/router";
import { cashflowCopy } from "./cashflowCopy";
import type { CashflowMonth, CashflowPage as CashflowData } from "./cashflowSchemas";

const ME = {
  user: { id: "u1", email: "a@hearth.family", displayName: "Andreas", avatarInitial: "A" },
  household: {
    id: "h1", name: "Home", familyName: "Oentoro", primaryCurrency: "SGD",
    showSecondaryCurrency: false, secondaryCurrency: "", fxRateMode: "static", timezone: "Asia/Singapore",
  },
  membership: { id: "m1", householdId: "h1", userId: "u1", role: "owner", capabilities: ["money"] },
  capabilities: ["money"],
  spaces: [],
  isPlatformAdmin: false,
  features: {},
};

function month(m: string): CashflowMonth {
  return {
    month: m, status: "solid", inMinor: 500000, outMinor: 400000, leftMinor: 100000, keptBasisPoints: 2000,
    spreadMinor: 0, loanMinor: 0, transactionCount: 12, grey: [], unconvertibleCurrencies: [],
    projectedLeftMinor: null, confirmation: null, contributions: [], reconciliation: null,
  };
}

function page(year: number): CashflowData {
  return {
    currency: "SGD", hasTransactions: true, oldestYear: 2024,
    light: { colour: "green", reasons: [] },
    average: { months: 3, inMinor: 500000, outMinor: 450000, leftMinor: 50000, keptBasisPoints: 1000 },
    cushion: { known: true, tenths: 42, reason: "" },
    lastFinished: month("2026-09"),
    current: { ...month("2026-10"), status: "current" },
    recent: [month("2026-08"), month("2026-09")],
    year: {
      year, months: [month("2026-09")], keptSoFarMinor: 900000, incomeMinor: 4500000, greyMonths: 0,
      keptBasisPoints: 2000, sameMonthsLastYearMinor: 700000, headingForMinor: 1100000, billsToCome: [],
    },
  };
}

const CURRENCIES = { status: 200, body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] } };

function open(path: string, extra: Record<string, { status: number; body: unknown }> = {}) {
  const fetchMock = stubFetchRoutes({
    "GET /api/v1/auth/me": { status: 200, body: ME },
    "GET /api/v1/currencies": CURRENCIES,
    "GET /api/v1/cashflow": { status: 200, body: page(2026) },
    "GET /api/v1/cashflow?year=2025": { status: 200, body: page(2025) },
    ...extra,
  });
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }) });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { router, fetchMock };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Cashflow tabs and year in the address", () => {
  it("opens on Yearly when the address says so", async () => {
    open("/money/cashflow?view=yearly");
    expect(await screen.findByRole("tab", { name: "Yearly", selected: true })).toBeInTheDocument();
    expect(screen.getByTestId("cashflow-year")).toHaveTextContent("2026");
  });

  it("switching tab writes view=yearly to the address and back to nothing for Monthly", async () => {
    const { router } = open("/money/cashflow");
    fireEvent.click(await screen.findByRole("tab", { name: "Yearly" }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ view: "yearly" }));
    fireEvent.click(screen.getByRole("tab", { name: "Monthly" }));
    await waitFor(() => expect(router.state.location.search).not.toHaveProperty("view"));
  });

  it("picking a year writes year=YYYY and the current year removes it", async () => {
    const { router } = open("/money/cashflow?view=yearly");
    fireEvent.click(await screen.findByRole("button", { name: cashflowCopy.previousYear }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ view: "yearly", year: 2025 }));
    expect(await screen.findByText("2025")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: cashflowCopy.nextYear }));
    await waitFor(() => expect(router.state.location.search).not.toHaveProperty("year"));
    expect(router.state.location.search).toMatchObject({ view: "yearly" });
  });

  it("falls back to this year when the server refuses the year", async () => {
    const { router } = open("/money/cashflow?view=yearly&year=2099", {
      "GET /api/v1/cashflow?year=2099": {
        status: 422,
        body: { error: { code: "CASHFLOW_YEAR_OUT_OF_RANGE", message: "Year out of range." } },
      },
    });
    expect(await screen.findByTestId("cashflow-year")).toHaveTextContent("2026");
    await waitFor(() => expect(router.state.location.search).not.toHaveProperty("year"));
    expect(screen.queryByTestId("cashflow-load-error")).not.toBeInTheDocument();
  });

  it("arrow keys move between tabs", async () => {
    open("/money/cashflow");
    const monthly = await screen.findByRole("tab", { name: "Monthly" });
    monthly.focus();
    fireEvent.keyDown(monthly, { key: "ArrowRight" });
    const yearly = await screen.findByRole("tab", { name: "Yearly", selected: true });
    expect(yearly).toHaveFocus();
    fireEvent.keyDown(yearly, { key: "ArrowLeft" });
    const back = await screen.findByRole("tab", { name: "Monthly", selected: true });
    expect(back).toHaveFocus();
  });

  it("tabs and panel are linked", async () => {
    open("/money/cashflow");
    const monthly = await screen.findByRole("tab", { name: "Monthly" });
    const yearly = screen.getByRole("tab", { name: "Yearly" });
    expect(monthly).toHaveAttribute("aria-controls", "cashflow-panel");
    expect(yearly).toHaveAttribute("aria-controls", "cashflow-panel");
    expect(monthly).toHaveAttribute("id", "cashflow-tab-monthly");
    expect(yearly).toHaveAttribute("tabindex", "-1");
    expect(screen.getByRole("tabpanel")).toHaveAttribute("aria-labelledby", "cashflow-tab-monthly");
    expect(screen.getByRole("tabpanel")).toHaveAttribute("id", "cashflow-panel");
  });

  it("the month table has the heading Month by month", async () => {
    open("/money/cashflow");
    expect(await screen.findByRole("heading", { name: "Month by month" })).toBeInTheDocument();
  });
});
