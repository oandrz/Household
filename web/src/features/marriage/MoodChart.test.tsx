// No fetch stub needed -- MoodChart takes `points` as a prop, the same data
// useRetros() already fetched (RetrosPage.tsx's own mount-point comment).
// No @testing-library/user-event either: this file has nothing to type or
// click, only markup to inspect.
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MoodChart } from "./MoodChart";
import type { MoodPoint } from "./retroSchemas";

function twelvePoints(overrides: Partial<Record<string, number | null>> = {}): MoodPoint[] {
  const months = [
    "2025-09", "2025-10", "2025-11", "2025-12",
    "2026-01", "2026-02", "2026-03", "2026-04",
    "2026-05", "2026-06", "2026-07", "2026-08",
  ];
  // `month in overrides`, not `?? 3` -- an override of `null` (a deliberate
  // gap) must not be treated the same as "not specified", which `??` would
  // do (it replaces both `null` and `undefined`).
  return months.map((month) => ({ month, mood: month in overrides ? (overrides[month] ?? null) : 3 }));
}

describe("MoodChart", () => {
  // A gap is a gap. A month with no finished retro must not be drawn as a
  // mood of zero -- zero is a claim, and on a line chart it is a dramatic
  // one (task-11-brief.md).
  it("draws a gap for a month with no mood, never a zero", () => {
    const { container } = render(
      <MoodChart
        points={[
          { month: "2026-06", mood: 4 },
          { month: "2026-07", mood: null },
          { month: "2026-08", mood: 3 },
        ]}
      />,
    );

    const polylines = container.querySelectorAll("polyline");
    // One segment each side of the gap, not one line running through it.
    expect(polylines.length).toBe(2);
    expect(container.querySelector("svg")?.textContent ?? "").not.toContain("0");
  });

  // Two gaps either side of a single tracked month still produce three
  // one-point "runs" -- proves the rule is "a run is a polyline", not
  // "a run of two or more points is a polyline".
  it("still gives an isolated tracked month its own polyline", () => {
    const { container } = render(
      <MoodChart
        points={[
          { month: "2026-05", mood: null },
          { month: "2026-06", mood: 4 },
          { month: "2026-07", mood: null },
        ]}
      />,
    );

    expect(container.querySelectorAll("polyline").length).toBe(1);
    expect(container.querySelectorAll("circle").length).toBe(1);
  });

  it("renders an empty chart without crashing when no month has a mood", () => {
    const allGaps: MoodPoint[] = twelvePoints().map((point) => ({ ...point, mood: null }));
    render(<MoodChart points={allGaps} />);

    expect(screen.getByTestId("mood-chart-empty")).toHaveTextContent("Not enough retros yet");
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
  });

  // role="img" + aria-label: a line drawing is invisible to a screen reader
  // otherwise. The label names the range and how many months actually carry
  // a mood -- never claims a gap is "0" (moodPointDTO's own comment).
  it("names the range and the tracked-month count for a screen reader", () => {
    const points = twelvePoints({ "2026-07": null });
    render(<MoodChart points={points} />);

    const chart = screen.getByRole("img");
    expect(chart).toHaveAccessibleName(/September 2025 to August 2026/);
    expect(chart).toHaveAccessibleName(/11 of 12 months tracked/);
  });

  it("draws one continuous polyline when every month has a mood", () => {
    const { container } = render(<MoodChart points={twelvePoints()} />);

    expect(container.querySelectorAll("polyline").length).toBe(1);
    expect(container.querySelectorAll("circle").length).toBe(12);
  });
});
