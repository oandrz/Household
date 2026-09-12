import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PeriodReturnChart } from "./PeriodReturnChart";
import type { PeriodReturn, ReportHolding, ReportPeriod } from "./holdingSchemas";

function period(index: number, current = false): ReportPeriod {
  return {
    kind: "quarter",
    year: 2026,
    index,
    label: `Q${index} 2026`,
    start: "2026-01-01",
    end: "2026-03-31",
    current,
  };
}

function figure(totalMinor: number | null, reason: PeriodReturn["reason"] = ""): PeriodReturn {
  const zero = { nativeMinor: 0, primaryMinor: 0 };
  return {
    unrealised: totalMinor === null ? null : zero,
    realised: zero,
    income: zero,
    fees: zero,
    total: totalMinor === null ? null : { nativeMinor: totalMinor, primaryMinor: totalMinor },
    reason,
    openingPriceAsOf: null,
    closingPriceAsOf: null,
  };
}

function holding(name: string, totals: (number | null)[]): ReportHolding {
  return {
    id: name,
    name,
    accountName: "Brokerage",
    instrument: "stock",
    unit: "share",
    currency: "SGD",
    archived: false,
    returns: totals.map((t) => figure(t)),
  };
}

function bars() {
  return Array.from(document.querySelectorAll<SVGRectElement>("[data-testid='return-bar']"));
}

describe("PeriodReturnChart", () => {
  it("draws one bar per holding per period", () => {
    render(
      <PeriodReturnChart
        periods={[period(1), period(2)]}
        holdings={[holding("Gold", [1000, 2000]), holding("D05", [500, 700])]}
        primaryCurrency="SGD"
      />,
    );
    expect(bars()).toHaveLength(4);
  });

  // A figure that cannot be known draws NOTHING. A zero-height bar sitting on
  // the axis reads as "this earned nothing", which is a different claim -- the
  // same reason the net worth chart skips a month it cannot value.
  it("draws no bar at all for a period it cannot value", () => {
    render(
      <PeriodReturnChart
        periods={[period(1), period(2)]}
        holdings={[holding("Gold", [null, 2000])]}
        primaryCurrency="SGD"
      />,
    );
    const drawn = bars();
    expect(drawn).toHaveLength(1);
    expect(drawn[0].getAttribute("data-period")).toBe("Q2 2026");
  });

  // A loss is drawn below the zero line, not as a short bar above it. Reading
  // a losing quarter as a small win is the worst thing this chart could do.
  it("draws a loss below the baseline and a gain above it", () => {
    render(
      <PeriodReturnChart
        periods={[period(1), period(2)]}
        holdings={[holding("Gold", [2000, -2000])]}
        primaryCurrency="SGD"
      />,
    );
    const svg = document.querySelector<SVGSVGElement>("[data-testid='period-return-chart']");
    const baseline = Number(svg?.getAttribute("data-baseline"));
    expect(Number.isNaN(baseline)).toBe(false);

    const [gain, loss] = bars();
    // SVG y grows downwards: a gain ends at the baseline, a loss starts there.
    expect(Number(gain.getAttribute("y")) + Number(gain.getAttribute("height"))).toBeCloseTo(baseline, 5);
    expect(Number(loss.getAttribute("y"))).toBeCloseTo(baseline, 5);
  });

  it("says so rather than drawing an empty axis when nothing can be valued", () => {
    render(
      <PeriodReturnChart
        periods={[period(1), period(2)]}
        holdings={[holding("Gold", [null, null])]}
        primaryCurrency="SGD"
      />,
    );
    expect(bars()).toHaveLength(0);
    expect(screen.getByTestId("period-return-chart-empty")).toBeInTheDocument();
  });

  // The bar budget. Twelve periods against six holdings is seventy-two bars in
  // 320 pixels, which is a smear -- so the oldest periods are dropped and the
  // caption says what is actually on screen.
  it("drops the oldest periods rather than drawing bars nobody can see", () => {
    const periods = Array.from({ length: 12 }, (_, i) => period(i + 1));
    const totals = Array.from({ length: 12 }, () => 1000);
    const holdings = ["A", "B", "C", "D", "E", "F"].map((name) => holding(name, totals));

    render(<PeriodReturnChart periods={periods} holdings={holdings} primaryCurrency="SGD" />);

    expect(bars().length).toBeLessThanOrEqual(40);
    expect(bars().length).toBeGreaterThan(0);
    // The newest period survives; the oldest is what goes.
    const drawnPeriods = new Set(bars().map((b) => b.getAttribute("data-period")));
    expect(drawnPeriods.has("Q12 2026")).toBe(true);
    expect(drawnPeriods.has("Q1 2026")).toBe(false);
    expect(screen.getByTestId("period-return-chart-window")).toHaveTextContent(/6 of 12/);
  });

  it("names every holding it drew so a colour means something", () => {
    render(
      <PeriodReturnChart
        periods={[period(1)]}
        holdings={[holding("Gold", [1000]), holding("D05", [500])]}
        primaryCurrency="SGD"
      />,
    );
    expect(screen.getByTestId("period-return-chart-legend")).toHaveTextContent("Gold");
    expect(screen.getByTestId("period-return-chart-legend")).toHaveTextContent("D05");
  });
});

// The zero baseline again, from the other side. With only gains, zero IS the
// floor: the axis sits at the bottom of the plot and every bar grows up from
// it. A baseline measured from the smallest figure instead would push the axis
// below the chart and draw the bars straight off the end of it -- proportions
// alone cannot catch that, because they stay correct while the scale runs away.
describe("PeriodReturnChart, measured from zero", () => {
  it("keeps the axis and every bar inside the plot when nothing lost money", () => {
    render(
      <PeriodReturnChart
        periods={[period(1), period(2)]}
        holdings={[holding("Gold", [1000, 3000])]}
        primaryCurrency="SGD"
      />,
    );
    const svg = document.querySelector<SVGSVGElement>("[data-testid='period-return-chart']");
    const baseline = Number(svg?.getAttribute("data-baseline"));
    const floor = Number(svg?.getAttribute("data-plot-bottom"));

    expect(baseline).toBeCloseTo(floor, 5);
    for (const bar of bars()) {
      const bottom = Number(bar.getAttribute("y")) + Number(bar.getAttribute("height"));
      expect(bottom).toBeLessThanOrEqual(floor + 0.001);
      expect(Number(bar.getAttribute("y"))).toBeGreaterThanOrEqual(0);
    }

    // ...and they are still in proportion to each other.
    const [smaller, larger] = bars().map((b) => Number(b.getAttribute("height")));
    expect(larger / smaller).toBeCloseTo(3, 1);
  });
});
