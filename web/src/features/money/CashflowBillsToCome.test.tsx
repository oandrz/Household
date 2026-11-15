// The Yearly view's list of bills still to come this year.
import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { CashflowPage } from "./CashflowPage";
import type { CashflowMonth, CashflowPage as CashflowData } from "./cashflowSchemas";

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

function monthFixture(overrides: Partial<CashflowMonth> = {}): CashflowMonth {
  return {
    month: "2026-10", status: "solid", inMinor: 500000, outMinor: 400000, leftMinor: 100000,
    keptBasisPoints: 2000, spreadMinor: 0, loanMinor: 0, transactionCount: 12, grey: [],
    unconvertibleCurrencies: [], projectedLeftMinor: null, confirmation: null,
    contributions: [], reconciliation: null,
    ...overrides,
  };
}

function renderYearly(billsToCome: CashflowData["year"]["billsToCome"]) {
  const october = monthFixture();
  const page = {
    currency: "SGD",
    hasTransactions: true,
    oldestYear: 2026,
    light: { colour: "green", reasons: [] },
    average: { months: 3, inMinor: 500000, outMinor: 450000, leftMinor: 50000, keptBasisPoints: 1000 },
    cushion: { known: true, tenths: 42, reason: "" },
    lastFinished: october,
    current: monthFixture({ month: "2026-11", status: "current" }),
    recent: [october],
    year: {
      year: 2026, months: [october], keptSoFarMinor: 900000, incomeMinor: 4500000, greyMonths: 0,
      keptBasisPoints: 2000, sameMonthsLastYearMinor: null, headingForMinor: 1100000, billsToCome,
    },
  } as CashflowData;
  stubFetchRoutes({
    "GET /api/v1/currencies": CURRENCIES,
    "GET /api/v1/cashflow": { status: 200, body: page },
  });
  renderWithRouter(<CashflowPage />);
}

afterEach(() => vi.unstubAllGlobals());

describe("bills still to come", () => {
  it("names a bill past its due date as overdue rather than printing the date as if it were coming", async () => {
    renderYearly([
      { billId: "gift", name: "Gift fund", amountMinor: 5000, nextDue: "2026-11-10", unconvertible: false, overdue: true },
      { billId: "water", name: "Water", amountMinor: 9000, nextDue: "2026-12-01", unconvertible: false, overdue: false },
    ]);
    fireEvent.click(await screen.findByRole("tab", { name: "Yearly" }));

    const overdue = await screen.findByText("Overdue since 10 Nov 2026");
    expect(overdue).toHaveClass("text-danger");
    expect(screen.queryByText("10 Nov 2026")).toBeNull();

    const upcoming = screen.getByText("1 Dec 2026");
    expect(upcoming).toHaveClass("text-muted");
    expect(upcoming).not.toHaveClass("text-danger");
  });
});
