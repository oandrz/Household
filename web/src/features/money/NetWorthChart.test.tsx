// No fetch stub: NetWorthChart takes its points as a prop, the same data
// useAccounts() already fetched. MoodChart.test.tsx is the pattern.
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { NetWorthChart } from "./NetWorthChart";
import type { TrendPoint } from "./schemas";

const MONTHS = [
  "2025-08", "2025-09", "2025-10", "2025-11",
  "2025-12", "2026-01", "2026-02", "2026-03",
  "2026-04", "2026-05", "2026-06", "2026-07",
];

function twelvePoints(overrides: Record<string, Partial<TrendPoint>> = {}): TrendPoint[] {
  return MONTHS.map((month, index) => ({
    month,
    netWorthMinor: 1_000_000 + index * 10_000,
    complete: true,
    ...(overrides[month] ?? {}),
  }));
}

describe("NetWorthChart", () => {
  it("draws one bar per month that has a figure", () => {
    const { container } = render(<NetWorthChart points={twelvePoints()} />);
    expect(container.querySelectorAll("[data-testid='net-worth-bar']").length).toBe(12);
  });

  // A gap is a gap. Zero is a claim about the household's money, and on a bar
  // chart a zero-height bar reads as "they had nothing".
  it("draws no bar at all for a month with no figure", () => {
    const points = twelvePoints({
      "2025-08": { netWorthMinor: null, complete: false },
      "2025-09": { netWorthMinor: null, complete: false },
    });
    const { container } = render(<NetWorthChart points={points} />);

    const bars = container.querySelectorAll("[data-testid='net-worth-bar']");
    expect(bars.length).toBe(10);
    expect([...bars].map((b) => b.getAttribute("data-month"))).not.toContain("2025-08");
  });

  // Fewer than two known months is not a trend. This is the state every new
  // household is in on their first day -- every account opened today -- and a
  // single bar pinned to the right-hand edge is worse than saying so.
  it("says there is not enough history when only one month is known", () => {
    const points = twelvePoints(
      Object.fromEntries(
        MONTHS.slice(0, 11).map((month) => [month, { netWorthMinor: null, complete: false }]),
      ),
    );
    const { container } = render(<NetWorthChart points={points} />);

    expect(container.querySelector("svg")).toBeNull();
    expect(screen.getByTestId("net-worth-chart-empty")).toBeInTheDocument();
  });

  it("marks the months that are missing an account added later", () => {
    const points = twelvePoints({
      "2025-08": { complete: false },
      "2025-09": { complete: false },
    });
    const { container } = render(<NetWorthChart points={points} />);

    const incomplete = container.querySelectorAll("[data-complete='false']");
    expect(incomplete.length).toBe(2);
    // Pins the first complete month specifically (monthTickLabel's "Aug '25"
    // axis format -- see copy.ts) rather than a bare "2025", which any month
    // in the year would have satisfied without proving which one was named.
    expect(screen.getByTestId("net-worth-chart-note")).toHaveTextContent("Oct '25");
  });

  it("draws negative net worth below the baseline, not off the chart", () => {
    const points = twelvePoints({ "2025-08": { netWorthMinor: -500_000 } });
    const { container } = render(<NetWorthChart points={points} />);

    const bars = [...container.querySelectorAll("[data-testid='net-worth-bar']")];
    const negative = bars.find((b) => b.getAttribute("data-month") === "2025-08");
    const positive = bars.find((b) => b.getAttribute("data-month") === "2026-07");
    // The negative bar starts at the baseline and runs down, so its top edge
    // is BELOW the positive bar's top edge (y grows downward in SVG).
    expect(Number(negative?.getAttribute("y"))).toBeGreaterThan(
      Number(positive?.getAttribute("y")),
    );
  });

  // Tick count alone would stay green if TICKS changed to an evenly-spaced
  // rule (dropping the newest month) or monthTickLabel broke -- asserting
  // the visible text is what actually pins the design's own axis
  // (Aug '25 · Nov '25 · Feb '26 · Jul '26).
  it("labels the axis at the four design positions, including the newest month", () => {
    render(<NetWorthChart points={twelvePoints()} />);

    expect(screen.getByText("Aug '25")).toBeInTheDocument();
    expect(screen.getByText("Nov '25")).toBeInTheDocument();
    expect(screen.getByText("Feb '26")).toBeInTheDocument();
    expect(screen.getByText("Jul '26")).toBeInTheDocument();
  });

  // The wire contract is exactly twelve points (Task 5), but trendSchema
  // (schemas.ts) is a plain array with no length check -- a truncated or
  // malformed response that still parses must degrade to fewer ticks, not
  // crash on points[11]. This exercises the TICKS.filter guard in
  // NetWorthChart.tsx directly, at a length short enough (5) that two of the
  // four design tick positions (6 and 11) do not exist.
  it("degrades to fewer ticks instead of crashing when given fewer than twelve points", () => {
    const points = twelvePoints().slice(0, 5);
    expect(() => render(<NetWorthChart points={points} />)).not.toThrow();
  });
});
