import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { CashflowChart } from "./CashflowChart";
import type { CashflowMonth } from "./cashflowSchemas";

function monthFixture(overrides: Partial<CashflowMonth> = {}): CashflowMonth {
  return {
    month: "2026-09",
    status: "solid",
    inMinor: 500000,
    outMinor: 400000,
    leftMinor: 100000,
    keptBasisPoints: 2000,
    spreadMinor: 0,
    loanMinor: 0,
    transactionCount: 12,
    grey: [],
    unconvertibleCurrencies: [],
    projectedLeftMinor: null,
    confirmation: null,
    contributions: [],
    reconciliation: null,
    ...overrides,
  };
}

const formatAmount = (minor: number) => `S$${(minor / 100).toFixed(2)}`;

function renderChart(months: CashflowMonth[]) {
  render(<CashflowChart months={months} currency="SGD" formatAmount={formatAmount} />);
}

describe("CashflowChart without a mouse", () => {
  it("names its months in words, not as keys", () => {
    renderChart([monthFixture({ month: "2026-08" }), monthFixture({ month: "2026-09" })]);
    expect(screen.getByRole("img", { name: "Left over each month, August 2026 to September 2026" })).toBeInTheDocument();
  });

  it("gives every drawn bar's month and amount as text a screen reader can reach", () => {
    renderChart([
      monthFixture({ month: "2026-08", status: "none" }),
      monthFixture({ month: "2026-09", leftMinor: 100000 }),
      monthFixture({ month: "2026-10", status: "current", leftMinor: 4728250 }),
    ]);
    const values = screen.getByTestId("cashflow-chart-values");
    expect(within(values).getAllByRole("listitem").map((item) => item.textContent)).toEqual([
      "September 2026: S$1000.00 left over",
      "October 2026 so far: S$47282.50 left over (bar cut off)",
    ]);
  });

  it("keeps the short month labels under the bars out of the spoken text", () => {
    renderChart([monthFixture({ month: "2026-09" })]);
    expect(screen.getByText("Sep")).toHaveAttribute("aria-hidden", "true");
  });
});
