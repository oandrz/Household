import { render, screen } from "@testing-library/react";
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

describe("CashflowChart zero line", () => {
  it("is drawn even when every month is above it, so the bars stand on something", () => {
    render(
      <CashflowChart
        months={[monthFixture({ month: "2026-08" }), monthFixture({ month: "2026-09", leftMinor: 245000 })]}
        currency="SGD"
        formatAmount={(minor) => String(minor)}
      />,
    );
    const chart = screen.getByTestId("cashflow-chart");
    const line = screen.getByTestId("cashflow-zero-line");
    expect(Number(line.getAttribute("y1"))).toBeCloseTo(Number(chart.getAttribute("data-baseline")));
  });
});
