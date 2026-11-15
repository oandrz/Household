import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { CashflowChart } from "./CashflowChart";
import { cashflowCopy } from "./cashflowCopy";
import type { CashflowMonth } from "./cashflowSchemas";

function monthFixture(overrides: Partial<CashflowMonth> = {}): CashflowMonth {
  return {
    month: "2026-07",
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

// Prints like the page's formatter: a real minus sign for negatives.
function formatAmount(minor: number): string {
  const text = `S$${(Math.abs(minor) / 100).toLocaleString("en-US", { minimumFractionDigits: 2 })}`;
  return minor < 0 ? `−${text}` : text;
}

function renderChart(months: CashflowMonth[]) {
  render(<CashflowChart months={months} currency="SGD" formatAmount={formatAmount} />);
  return screen.getByTestId("cashflow-chart");
}

function bar(chart: HTMLElement, month: string): Element {
  return within(chart)
    .getAllByTestId("cashflow-bar")
    .find((el) => el.getAttribute("data-month") === month)!;
}

describe("CashflowChart", () => {
  it("titles each kind of bar with its month and amount", () => {
    const chart = renderChart([
      monthFixture({ month: "2026-05", leftMinor: 100000 }),
      monthFixture({ month: "2026-06", leftMinor: -12000 }),
      monthFixture({ month: "2026-07", leftMinor: 0 }),
      monthFixture({ month: "2026-08", status: "grey", grey: ["no_income"], leftMinor: 5000 }),
      monthFixture({ month: "2026-10", status: "current", leftMinor: 50000 }),
      monthFixture({ month: "2026-12", status: "future", leftMinor: 0, projectedLeftMinor: 148350 }),
    ]);
    const title = (month: string) => bar(chart, month).querySelector("title")!.textContent;
    expect(title("2026-05")).toBe("May 2026: S$1,000.00 left over");
    expect(title("2026-06")).toBe("June 2026: nothing left over (−S$120.00)");
    expect(title("2026-07")).toBe("July 2026: nothing left over (S$0.00)");
    expect(title("2026-08")).toBe("August 2026: not enough data");
    expect(title("2026-10")).toBe("October 2026 so far: S$500.00 left over");
    expect(title("2026-12")).toBe("December 2026, projected: S$1,483.50 left over");
  });

  it("words a running or projected month at or below zero like a finished one", () => {
    const chart = renderChart([
      monthFixture({ month: "2026-09", leftMinor: 500000 }),
      monthFixture({ month: "2026-10", status: "current", leftMinor: -300000 }),
      monthFixture({ month: "2026-12", status: "future", leftMinor: 0, projectedLeftMinor: -120000 }),
    ]);
    const title = (month: string) => bar(chart, month).querySelector("title")!.textContent;
    expect(title("2026-10")).toBe("October 2026 so far: nothing left over (−S$3,000.00)");
    expect(title("2026-12")).toBe("December 2026, projected: nothing left over (−S$1,200.00)");
  });

  it("marks only a clipped bar as cut off", () => {
    const chart = renderChart([
      monthFixture({ month: "2026-08", leftMinor: 100000 }),
      monthFixture({ month: "2026-09", leftMinor: 200000 }),
      monthFixture({ month: "2026-10", status: "current", leftMinor: 4728250 }),
    ]);
    const current = bar(chart, "2026-10");
    expect(current.getAttribute("data-clipped")).toBe("true");
    expect(current.querySelector("title")!.textContent).toBe(
      "October 2026 so far: S$47,282.50 left over (bar cut off)",
    );
    expect(within(chart).getAllByTestId("cashflow-bar-cut")).toHaveLength(1);
    expect(bar(chart, "2026-09").hasAttribute("data-clipped")).toBe(false);
    expect(bar(chart, "2026-09").querySelector("title")!.textContent).not.toContain("cut off");
  });

  it("holds the same height at every width", () => {
    const chart = renderChart([
      monthFixture({ month: "2026-08", leftMinor: 100000 }),
      monthFixture({ month: "2026-09", status: "grey", grey: ["no_income"] }),
      monthFixture({ month: "2026-10", status: "future", projectedLeftMinor: 5000 }),
    ]);
    expect(chart.getAttribute("class")).toContain("h-[200px]");
    expect(chart.getAttribute("preserveAspectRatio")).toBe("none");
    const stroked = Array.from(chart.querySelectorAll("[stroke]")).filter(
      (el) => el.getAttribute("stroke") !== "none" && !el.closest("defs"),
    );
    expect(stroked.length).toBeGreaterThan(0);
    for (const el of stroked) expect(el.getAttribute("vector-effect")).toBe("non-scaling-stroke");
  });

  it("draws the legend in the table's words and the bars' colours", () => {
    renderChart([monthFixture()]);
    const legend = screen.getByTestId("cashflow-legend");
    const swatches = within(legend).getAllByTestId("cashflow-legend-swatch");
    expect(swatches.map((swatch) => swatch.getAttribute("data-style"))).toEqual([
      "solid-positive",
      "solid-negative",
      "grey",
      "current",
      "projected",
    ]);
    const labels = Array.from(legend.querySelectorAll("li")).map((li) => li.textContent);
    expect(labels).toEqual([
      cashflowCopy.chartLegend.left,
      cashflowCopy.chartLegend.nothing,
      cashflowCopy.chartLegend.grey,
      cashflowCopy.chartLegend.current,
      cashflowCopy.chartLegend.projected,
    ]);
    expect(swatches[0].getAttribute("class")).toContain("text-accent");
    expect(swatches[1].getAttribute("class")).toContain("text-danger");
    expect(swatches[4].getAttribute("class")).toContain("text-accent");
  });

  it("colours a projected shortfall red and a projected gain green", () => {
    const chart = renderChart([
      monthFixture({ month: "2026-11", status: "future", projectedLeftMinor: -5000 }),
      monthFixture({ month: "2026-12", status: "future", projectedLeftMinor: 5000 }),
    ]);
    expect(bar(chart, "2026-11").getAttribute("class")).toContain("text-danger");
    expect(bar(chart, "2026-12").getAttribute("class")).toContain("text-accent");
  });

  it("keeps the cut-off slash the same width however wide the bars are", () => {
    const run = (count: number) => {
      const months = Array.from({ length: count }, (_, i) =>
        monthFixture({ month: `2026-${String(i + 1).padStart(2, "0")}`, leftMinor: 100000 }),
      );
      months.push(monthFixture({ month: "2027-01", status: "current", leftMinor: 9000000 }));
      const { unmount } = render(
        <CashflowChart months={months} currency="SGD" formatAmount={formatAmount} />,
      );
      const lines = Array.from(screen.getByTestId("cashflow-bar-cut").querySelectorAll("line"));
      const result = lines.map((l) => Number(l.getAttribute("x2")) - Number(l.getAttribute("x1")));
      const leans = lines.map((l) => Number(l.getAttribute("y1")) > Number(l.getAttribute("y2")));
      const passes = screen.getByTestId("cashflow-bar-cut").getAttribute("pointer-events");
      unmount();
      return { result, leans, passes };
    };
    const few = run(2);
    const many = run(10);
    expect(few.result).toEqual(many.result);
    expect(few.leans).toEqual([true, true]);
    expect(few.passes).toBe("none");
  });
});
