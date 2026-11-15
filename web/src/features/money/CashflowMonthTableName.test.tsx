// The month-by-month table carries the name its visible heading shows.
import { screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { CashflowPage } from "./CashflowPage";
import type { CashflowMonth, CashflowPage as CashflowData } from "./cashflowSchemas";

function monthFixture(overrides: Partial<CashflowMonth> = {}): CashflowMonth {
  return {
    month: "2026-10", status: "solid", inMinor: 500000, outMinor: 400000, leftMinor: 100000,
    keptBasisPoints: 2000, spreadMinor: 0, loanMinor: 0, transactionCount: 12, grey: [],
    unconvertibleCurrencies: [], projectedLeftMinor: null, confirmation: null,
    contributions: [], reconciliation: null,
    ...overrides,
  };
}

afterEach(() => vi.unstubAllGlobals());

it("names the month table so a screen reader announces what it lists", async () => {
  const october = monthFixture();
  stubFetchRoutes({
    "GET /api/v1/currencies": {
      status: 200,
      body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
    },
    "GET /api/v1/cashflow": {
      status: 200,
      body: {
        currency: "SGD", hasTransactions: true, oldestYear: 2026,
        light: { colour: "green", reasons: [] },
        average: { months: 3, inMinor: 500000, outMinor: 450000, leftMinor: 50000, keptBasisPoints: 1000 },
        cushion: { known: true, tenths: 42, reason: "" },
        lastFinished: october,
        current: monthFixture({ month: "2026-11", status: "current" }),
        recent: [october],
        year: {
          year: 2026, months: [october], keptSoFarMinor: 0, incomeMinor: 0, greyMonths: 0,
          keptBasisPoints: null, sameMonthsLastYearMinor: null, headingForMinor: 0, billsToCome: [],
        },
      } as CashflowData,
    },
  });
  renderWithRouter(<CashflowPage />);

  expect(await screen.findByRole("table", { name: "Month by month" })).toBeInTheDocument();
});
