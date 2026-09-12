// Follows PortfolioPage.test.tsx's shape: renderWithRouter plus
// stubFetchRoutes for every request, and literal strings asserted rather than
// an imported copy module (which would make the assertion tautological against
// a typo in that same module).
//
// Most of these exist because the page must render what the SERVER said rather
// than what it could work out itself: the window length, which period is still
// running, and whether a figure is knowable at all are each a wire fact with a
// branch that is easy to reimplement locally and get subtly wrong.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { PortfolioReportPage } from "./PortfolioReportPage";
import type { PeriodReturn, ReportHolding, ReportPeriod } from "./holdingSchemas";

const zero = { nativeMinor: 0, primaryMinor: 0 };

function quarter(index: number, current = false): ReportPeriod {
  return {
    kind: "quarter",
    year: 2026,
    index,
    label: `Q${index} 2026`,
    start: "2026-04-01",
    end: "2026-06-30",
    current,
  };
}

const blankedQuarter: PeriodReturn = {
  unrealised: null,
  realised: { nativeMinor: 2000, primaryMinor: 2000 },
  income: zero,
  fees: zero,
  total: null,
  reason: "no_closing_price",
  openingPriceAsOf: null,
  closingPriceAsOf: null,
};

const earningQuarter: PeriodReturn = {
  unrealised: { nativeMinor: 20000, primaryMinor: 20000 },
  realised: zero,
  income: { nativeMinor: 4500, primaryMinor: 4500 },
  fees: { nativeMinor: 500, primaryMinor: 500 },
  total: { nativeMinor: 24000, primaryMinor: 24000 },
  reason: "",
  openingPriceAsOf: null,
  closingPriceAsOf: "2026-09-12",
};

function gold(overrides: Partial<ReportHolding> = {}): ReportHolding {
  return {
    id: "h1",
    name: "Gold bar",
    accountName: "Brokerage",
    instrument: "gold",
    unit: "gram",
    currency: "SGD",
    archived: false,
    returns: [blankedQuarter, earningQuarter],
    ...overrides,
  };
}

function reportBody(holdings: ReportHolding[] = [gold()], kind = "quarter") {
  return {
    status: 200,
    body: {
      kind,
      primaryCurrency: "SGD",
      periods: [quarter(2), quarter(3, true)],
      holdings,
    },
  };
}

// `name` is required by currencySchema -- omitting it makes the parse throw,
// the query fail, and every figure fall back to the bare code ("SGD 200.00").
// That is exactly the defect milestone 1's review found on the portfolio
// screen, so these tests assert the symbol rather than only the digits.
const currencies = {
  status: 200,
  body: {
    currencies: [
      { code: "SGD", symbol: "S$", name: "Singapore dollar" },
      { code: "USD", symbol: "US$", name: "United States dollar" },
    ],
  },
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe("PortfolioReportPage", () => {
  it("shows each period's three components and their total", async () => {
    stubFetchRoutes({
      "GET /api/v1/holdings/report?kind=quarter": reportBody(),
      "GET /api/v1/currencies": currencies,
    });
    renderWithRouter(<PortfolioReportPage />);

    const rows = await screen.findAllByTestId("report-row");
    expect(rows).toHaveLength(2);
    // Newest first in the table: the period the owner is in is what they came
    // for, and a table is read from the top.
    expect(rows[0]).toHaveTextContent("Q3 2026 to date");
    expect(rows[0]).toHaveTextContent("S$200.00");
    expect(rows[0]).toHaveTextContent("S$45.00");
    expect(rows[0]).toHaveTextContent("S$240.00");
  });

  // "No figure and a reason" is the PRD's rule. A dash on its own reads as a
  // defect, and a zero would be a lie about the household's money.
  it("explains a period it could not value rather than showing a zero", async () => {
    stubFetchRoutes({
      "GET /api/v1/holdings/report?kind=quarter": reportBody(),
      "GET /api/v1/currencies": currencies,
    });
    renderWithRouter(<PortfolioReportPage />);

    const reason = await screen.findByTestId("report-blank-reason");
    expect(reason).toHaveTextContent("No price recorded in this period");
    // ...while realised, which needs no price at all, is still reported.
    expect(screen.getAllByTestId("report-row")[1]).toHaveTextContent("S$20.00");
  });

  // The toggle asks for a different KIND. It never sends a count: the window
  // length is the server's, and a second copy of that rule here would be free
  // to drift from the one the chart's bar budget was chosen against.
  it("asks the server for the period kind the reader picked, and never for a count", async () => {
    const asked: string[] = [];
    stubFetchRoutes({
      "GET /api/v1/holdings/report?kind=quarter": {
        ...reportBody(),
        capture: () => asked.push("quarter"),
      },
      "GET /api/v1/holdings/report?kind=year": {
        ...reportBody([gold()], "year"),
        capture: () => asked.push("year"),
      },
      "GET /api/v1/currencies": currencies,
    });
    renderWithRouter(<PortfolioReportPage />);
    await screen.findAllByTestId("report-row");

    fireEvent.click(screen.getByRole("button", { name: "Year" }));

    await waitFor(() => expect(asked).toContain("year"));
  });

  // Milestone 2 still does not touch net worth, and the page says so rather
  // than leaving a reader to assume these figures are in the headline.
  it("says these figures are not in net worth", async () => {
    stubFetchRoutes({
      "GET /api/v1/holdings/report?kind=quarter": reportBody(),
      "GET /api/v1/currencies": currencies,
    });
    renderWithRouter(<PortfolioReportPage />);

    expect(await screen.findByTestId("report-not-in-net-worth")).toHaveTextContent(
      "not in your net worth",
    );
  });

  it("shows the household's own currency beside the instrument's when they differ", async () => {
    stubFetchRoutes({
      "GET /api/v1/holdings/report?kind=quarter": reportBody([
        gold({
          name: "VOO",
          currency: "USD",
          returns: [
            blankedQuarter,
            {
              ...earningQuarter,
              unrealised: { nativeMinor: 0, primaryMinor: -15000 },
              total: { nativeMinor: 0, primaryMinor: -15000 },
            },
          ],
        }),
      ]),
      "GET /api/v1/currencies": currencies,
    });
    renderWithRouter(<PortfolioReportPage />);

    const rows = await screen.findAllByTestId("report-row");
    // Flat in USD, down S$150 in SGD -- the PRD's own example, and the reason
    // both figures are shown rather than only the native one.
    expect(rows[0]).toHaveTextContent("\u2212S$150.00");
    expect(rows[0]).toHaveTextContent("US$0.00");
  });

  it("invites the household to add a holding when there is nothing to report", async () => {
    stubFetchRoutes({
      "GET /api/v1/holdings/report?kind=quarter": reportBody([]),
      "GET /api/v1/currencies": currencies,
    });
    renderWithRouter(<PortfolioReportPage />);

    expect(await screen.findByText("Nothing to report yet")).toBeInTheDocument();
  });
});
