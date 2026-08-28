// No fetch stub needed -- PillarCard takes a `pillar` as a prop, the same
// data VisionPage.tsx's own useVision(year) already fetched (MoodChart.test.tsx's
// own precedent for a pure-presentation component: plain render/screen, no
// router, no network).
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PillarCard } from "./PillarCard";
import type { VisionMeasure, VisionPillar } from "./visionSchemas";

function measureFixture(overrides: Partial<VisionMeasure> = {}): VisionMeasure {
  return {
    label: "Date nights / month",
    kind: "typed",
    hasFigure: true,
    current: 2,
    target: 2,
    percent: 100,
    met: true,
    goalId: "",
    goalName: "",
    ...overrides,
  };
}

function pillarFixture(overrides: Partial<VisionPillar> = {}): VisionPillar {
  return {
    name: "Us before logistics",
    description: "We're partners first, co-managers of a household second.",
    measures: [],
    ...overrides,
  };
}

describe("PillarCard", () => {
  it("renders its ordinal label, name and description", () => {
    render(<PillarCard pillar={pillarFixture()} index={0} />);

    expect(screen.getByText("Pillar 1")).toBeInTheDocument();
    expect(screen.getByText("Us before logistics")).toBeInTheDocument();
    expect(
      screen.getByText("We're partners first, co-managers of a household second."),
    ).toBeInTheDocument();
  });

  // index is 0-based (array position); the label is 1-based -- this is the
  // one place that translation happens, so a caller passing the raw array
  // index straight through gets the design's own "Pillar 1", "Pillar 2"
  // numbering without having to +1 itself.
  it("numbers the third pillar as Pillar 3, not Pillar 2", () => {
    render(<PillarCard pillar={pillarFixture()} index={2} />);

    expect(screen.getByText("Pillar 3")).toBeInTheDocument();
  });

  // An empty pillar description renders nothing, not an empty block --
  // toDomainVision (vision_handlers.go) always sends "" rather than
  // omitting the field, so this checks truthiness rather than trusting a
  // pillar always has one worth a line (task-11's own ruling 4).
  it("renders no description block when the pillar's description is empty", () => {
    render(<PillarCard pillar={pillarFixture({ description: "" })} index={0} />);

    expect(screen.queryByTestId("vision-pillar-description")).not.toBeInTheDocument();
  });

  it("a typed measure renders 2 of 2 and shows the met marker", () => {
    render(
      <PillarCard
        pillar={pillarFixture({
          measures: [measureFixture({ label: "Date nights / month", current: 2, target: 2, met: true })],
        })}
        index={0}
      />,
    );

    expect(screen.getByText(/2 of 2/)).toHaveTextContent("2 of 2 ✓");
  });

  it("a typed measure that has not hit its target renders the figure without a met marker", () => {
    render(
      <PillarCard
        pillar={pillarFixture({
          measures: [
            measureFixture({
              label: "Phone-free dinners / week",
              current: 3,
              target: 5,
              met: false,
              percent: 60,
            }),
          ],
        })}
        index={0}
      />,
    );

    const row = screen.getByTestId("vision-measure-row");
    expect(row).toHaveTextContent("3 of 5");
    expect(row).not.toHaveTextContent("✓");
  });

  // A real browser walk at 305px found this: a long measure label crowds
  // the figure for width in the row's flex layout, and without
  // whitespace-nowrap the figure itself wraps mid-number ("2 of" / "4") --
  // jsdom lays nothing out, so this pins the mechanism (the class that
  // stops it) rather than the pixels, the same idiom RetrosPage.test.tsx's
  // own centring comment uses for a property jsdom cannot see directly.
  it("renders the figure with the no-wrap class", () => {
    render(
      <PillarCard
        pillar={pillarFixture({
          measures: [
            measureFixture({
              label: "Weekends away together this year, just the two of us",
              current: 2,
              target: 4,
              met: false,
            }),
          ],
        })}
        index={0}
      />,
    );

    const figure = screen.getByText("2 of 4");
    expect(figure.className).toContain("whitespace-nowrap");
  });

  it("a linked measure renders its percent, not a count", () => {
    render(
      <PillarCard
        pillar={pillarFixture({
          measures: [
            measureFixture({
              label: "Emergency fund",
              kind: "linked",
              percent: 62,
              met: false,
              current: 0,
              target: 0,
              goalId: "goal-1",
              goalName: "Emergency fund",
            }),
          ],
        })}
        index={0}
      />,
    );

    expect(screen.getByTestId("vision-measure-row")).toHaveTextContent("62%");
  });

  // A measure with no figure renders its label and a short explanation,
  // never a number -- not "0 of 0", not "0%". This asserts the ABSENCE of
  // any digit in the row, not merely the presence of the label: a
  // regression that kept the label but slipped a stray "0" back in would
  // pass a test that only checked for the label's own text.
  it("a measure with hasFigure false renders its label and no number at all", () => {
    render(
      <PillarCard
        pillar={pillarFixture({
          measures: [
            measureFixture({
              label: "Weekends away this year",
              kind: "broken",
              hasFigure: false,
              current: 0,
              target: 0,
              percent: 0,
              met: false,
            }),
          ],
        })}
        index={0}
      />,
    );

    const row = screen.getByTestId("vision-measure-row");
    expect(within(row).getByText("Weekends away this year")).toBeInTheDocument();
    expect(row).toHaveTextContent("Goal removed");
    expect(row.textContent).not.toMatch(/\d/);
  });

  it("renders every measure of a pillar, in order", () => {
    render(
      <PillarCard
        pillar={pillarFixture({
          measures: [
            measureFixture({ label: "Date nights / month" }),
            measureFixture({ label: "Weekends away this year", current: 2, target: 4, met: false }),
          ],
        })}
        index={0}
      />,
    );

    const rows = screen.getAllByTestId("vision-measure-row");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("Date nights / month");
    expect(rows[1]).toHaveTextContent("Weekends away this year");
  });
});
