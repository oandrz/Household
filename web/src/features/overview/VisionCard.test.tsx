// VisionCard.tsx owns its own useVision call, the same NextRetroCard.tsx/
// NextBillCard.tsx shape -- see VisionCard.tsx's own header comment for why
// (useVision takes no `enabled` option, so the gate has to live where the
// component is mounted or not, not inside the hook).
import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { currentVisionYear } from "../marriage/visionQueryKeys";
import type { Vision, VisionMeasure, VisionPillar } from "../marriage/visionSchemas";
import { VisionCard } from "./VisionCard";

const YEAR = currentVisionYear();

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
    description: "",
    measures: [],
    ...overrides,
  };
}

function visionFixture(overrides: Partial<Vision> = {}): Vision {
  return {
    year: YEAR,
    theme: "Slow down together",
    description: "",
    version: 1,
    pillars: [],
    milestones: [],
    ...overrides,
  };
}

// `capture` lets a test prove the request actually resolved (the version-0
// test below needs this -- an absence assertion holds just as well while
// the query is still in flight, which would pass for the wrong reason).
function renderCard(vision: Vision, capture?: () => void) {
  const fetchMock = stubFetchRoutes({
    [`GET /api/v1/marriage/vision?year=${YEAR}`]: { status: 200, body: { vision }, capture },
  });
  return { fetchMock, ...renderWithRouter(<VisionCard />) };
}

describe("VisionCard", () => {
  it("renders the theme, then one line per pillar showing its FIRST measure with live figures", async () => {
    renderCard(
      visionFixture({
        pillars: [
          pillarFixture({
            name: "Us before logistics",
            measures: [
              measureFixture({ label: "Date nights / month", current: 2, target: 2 }),
              measureFixture({ label: "Phone-free dinners / week", current: 3, target: 5 }),
            ],
          }),
          pillarFixture({
            name: "Faith and family",
            measures: [
              measureFixture({ label: "Emergency fund", kind: "linked", percent: 62, current: 0, target: 0 }),
            ],
          }),
        ],
      }),
    );

    const card = await screen.findByTestId("vision-card");
    expect(card).toHaveTextContent('"Slow down together"');

    const lines = screen.getAllByTestId("vision-overview-line");
    expect(lines).toHaveLength(2);
    expect(lines[0]).toHaveTextContent("Date nights / month");
    expect(lines[0]).toHaveTextContent("2 of 2");
    // The FIRST measure only -- the pillar's second measure (a different
    // label AND a different figure) must never surface here. Asserting
    // both, not just the label, is what actually pins "first," not merely
    // "a measure that happens to share a label with the first."
    expect(lines[0]).not.toHaveTextContent("Phone-free dinners / week");
    expect(lines[0]).not.toHaveTextContent("3 of 5");
    expect(lines[1]).toHaveTextContent("Emergency fund");
    expect(lines[1]).toHaveTextContent("62%");
  });

  // The same measure read "2 of 2 ✓" on /marriage/vision and "2 of 2" here,
  // so the Overview could not tell a target the household had hit from one
  // they had not. Both fixtures are `2 of 2`-shaped on purpose: only `met`
  // differs, so the assertion pins the marker rather than the arithmetic.
  it("marks a met measure the way the pillar card does, and leaves an unmet one plain", async () => {
    renderCard(
      visionFixture({
        pillars: [
          pillarFixture({
            name: "Us before logistics",
            measures: [measureFixture({ label: "Date nights / month", current: 2, target: 2, met: true })],
          }),
          pillarFixture({
            name: "Faith and family",
            measures: [measureFixture({ label: "Weekends away", current: 2, target: 4, met: false })],
          }),
        ],
      }),
    );

    const lines = await screen.findAllByTestId("vision-overview-line");
    expect(lines[0]).toHaveTextContent("2 of 2 ✓");
    expect(lines[1]).toHaveTextContent("2 of 4");
    expect(lines[1]).not.toHaveTextContent("✓");
  });

  it("falls back to the pillar's own name when it has no measures", async () => {
    renderCard(
      visionFixture({ pillars: [pillarFixture({ name: "Us before logistics", measures: [] })] }),
    );

    const line = await screen.findByTestId("vision-overview-line");
    expect(line).toHaveTextContent("Us before logistics");
  });

  // MeasureRow's own rule (PillarCard.tsx), duplicated here because
  // VisionCard's line is its own render path, not a reuse of that
  // component -- never "0 of 0", never "0%".
  it("shows a figureless first measure's label with no number at all", async () => {
    renderCard(
      visionFixture({
        pillars: [
          pillarFixture({
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
          }),
        ],
      }),
    );

    const line = await screen.findByTestId("vision-overview-line");
    expect(line).toHaveTextContent("Weekends away this year");
    expect(line.textContent).not.toMatch(/\d/);
  });

  it("renders the theme alone when the vision has no pillars", async () => {
    renderCard(visionFixture({ pillars: [] }));

    const card = await screen.findByTestId("vision-card");
    expect(card).toHaveTextContent('"Slow down together"');
    expect(screen.queryByTestId("vision-overview-line")).not.toBeInTheDocument();
  });

  // Ruling 2: a year with no vision (version 0) shows neither surface at
  // all -- an empty quotation would claim this household has a vision and
  // it is blank. Deliberately built from otherwise real-looking data (a
  // real theme, a pillar with a real measure) rather than an empty vision,
  // so this test can only pass because of the version guard, not because
  // there was nothing to render either way -- Step 5's mutation check
  // depends on that.
  it("renders nothing at all for a year with no vision, even with otherwise real-looking data", async () => {
    let visionRequested = false;
    renderCard(
      visionFixture({
        version: 0,
        theme: "Slow down together",
        pillars: [pillarFixture({ measures: [measureFixture()] })],
      }),
      () => {
        visionRequested = true;
      },
    );

    // Proof the request actually resolved, not merely that the card has not
    // appeared yet -- an absence assertion holds just as well while the
    // query is still in flight.
    await waitFor(() => expect(visionRequested).toBe(true));
    expect(screen.queryByTestId("vision-card")).not.toBeInTheDocument();
  });

  it("links the whole card to /marriage/vision", async () => {
    renderCard(visionFixture());

    const card = await screen.findByTestId("vision-card");
    expect(card).toHaveAttribute("href", "/marriage/vision");
  });
});
