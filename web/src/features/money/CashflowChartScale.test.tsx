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

function barHeights(months: CashflowMonth[]) {
  render(<CashflowChart months={months} currency="SGD" />);
  const bars = within(screen.getByTestId("cashflow-chart")).getAllByTestId("cashflow-bar");
  const height = (month: string) =>
    Number(bars.find((bar) => bar.getAttribute("data-month") === month)!.getAttribute("height"));
  return height;
}

describe("CashflowChart scale", () => {
  it("is set by finished months, so a big month still running cannot flatten them", () => {
    const height = barHeights([
      monthFixture({ month: "2026-08", leftMinor: 100000 }),
      monthFixture({ month: "2026-09", leftMinor: 200000 }),
      monthFixture({ month: "2026-10", status: "current", leftMinor: 4728250 }),
    ]);
    // The tallest finished month fills the plot; the running month is cut off
    // at the same height rather than shrinking September to a sliver.
    expect(height("2026-09")).toBeCloseTo(height("2026-10"));
    expect(height("2026-08")).toBeCloseTo(height("2026-09") / 2);
  });

  it("still draws the running month to scale when nothing has finished yet", () => {
    const height = barHeights([monthFixture({ month: "2026-10", status: "current", leftMinor: 50000 })]);
    expect(height("2026-10")).toBeGreaterThan(1);
  });
});
