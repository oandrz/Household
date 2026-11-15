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
  render(<CashflowChart months={months} currency="SGD" formatAmount={(minor) => String(minor)} />);
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

describe("CashflowChart scale below zero", () => {
  const finished = [
    monthFixture({ month: "2026-08", leftMinor: 100000 }),
    monthFixture({ month: "2026-09", leftMinor: 245000 }),
  ];

  function render3(currentLeft: number) {
    render(
      <CashflowChart
        months={[...finished, monthFixture({ month: "2026-10", status: "current", leftMinor: currentLeft })]}
        currency="SGD"
        formatAmount={(minor) => String(minor)}
      />,
    );
    const chart = screen.getByTestId("cashflow-chart");
    const bar = (month: string) =>
      within(chart)
        .getAllByTestId("cashflow-bar")
        .find((el) => el.getAttribute("data-month") === month)!;
    return { chart, bar };
  }
  const num = (el: Element, attr: string) => Number(el.getAttribute(attr));

  it("draws a negative running month under the zero line with a real height", () => {
    const { chart, bar } = render3(-100000);
    const october = bar("2026-10");
    const baseline = num(chart, "data-baseline");
    expect(num(october, "y")).toBeCloseTo(baseline);
    expect(num(october, "height")).toBeGreaterThan(num(bar("2026-08"), "height") * 0.9);
    expect(october.hasAttribute("data-clipped")).toBe(false);
    expect(chart.querySelector("line")).not.toBeNull();
  });

  it("cuts a very negative running month off at the bottom, inside its own bar, and keeps finished bars tall", () => {
    const { chart, bar } = render3(-5000000);
    const october = bar("2026-10");
    expect(october.getAttribute("data-clipped")).toBe("true");
    expect(october.querySelector("title")!.textContent).toBe("October 2026 so far: nothing left over (-5000000) (bar cut off)");
    expect(num(bar("2026-09"), "height")).toBeGreaterThan(40);

    const top = num(october, "y");
    const bottom = top + num(october, "height");
    const ys = [...within(chart).getByTestId("cashflow-bar-cut").querySelectorAll("line")].flatMap((l) => [
      num(l, "y1"),
      num(l, "y2"),
    ]);
    for (const y of ys) {
      expect(y).toBeGreaterThanOrEqual(top);
      expect(y).toBeLessThanOrEqual(bottom);
    }
  });
});
