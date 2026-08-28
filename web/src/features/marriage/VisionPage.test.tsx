// Follows RetrosPage.test.tsx's own shape: renderWithRouter plus
// stubFetchRoutes for every request, literal strings asserted throughout
// (not VISION_COPY's own exports -- importing the copy module here would
// make an assertion tautological against a typo in that same module).
//
// "Today" is faked to 2026-08-15 (GoalModal.test.tsx's own convention,
// `toFake: ["Date"]` only, leaving every other timer real so
// findBy*/waitFor's own polling still works) so currentVisionYear() -- and
// therefore which GET this page fires and what year it renders in its own
// hero -- is deterministic regardless of the real machine clock.
//
// Two of the states below -- owner-only and load-error -- are the pair that
// has shipped wrong three times already in this codebase (Bills, Budget,
// Transactions; docs/LEARNING.md pattern 1's own entry), which is why both
// get their own two-test pair here exactly as RetrosPage.test.tsx does, plus
// the mutation check in this task's own report.
import { screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { VisionPage } from "./VisionPage";
import type { Vision, VisionMeasure, VisionPillar } from "./visionSchemas";

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-08-15T12:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

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

function visionFixture(overrides: Partial<Vision> = {}): Vision {
  return {
    year: 2026,
    theme: "Slow down together",
    description: "Fewer commitments, more presence.",
    version: 3,
    pillars: [],
    milestones: [],
    ...overrides,
  };
}

function renderPage(vision: Vision, extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {}) {
  const fetchMock = stubFetchRoutes({
    "GET /api/v1/marriage/vision?year=2026": { status: 200, body: { vision } },
    ...extraRoutes,
  });
  return { fetchMock, ...renderWithRouter(<VisionPage />) };
}

describe("VisionPage", () => {
  it("renders the page title, subtitle and the Edit vision button", async () => {
    renderPage(visionFixture());

    expect(await screen.findByRole("heading", { level: 1, name: "Vision & goals" })).toBeInTheDocument();
    expect(screen.getByText("Set every January, checked in at each retro")).toBeInTheDocument();
    expect(screen.getByTestId("vision-edit")).toHaveTextContent("Edit vision");
  });

  it("renders the theme hero with the year label, the theme in quotes and the description", async () => {
    renderPage(visionFixture({ theme: "Slow down together", description: "Fewer commitments, more presence." }));

    const hero = await screen.findByTestId("vision-hero");
    expect(hero).toHaveTextContent("2026 theme");
    expect(hero).toHaveTextContent('"Slow down together"');
    expect(hero).toHaveTextContent("Fewer commitments, more presence.");
  });

  // The empty-vision response carries description: "" on the wire
  // (visionSchema's own comment), and a household with a theme but no
  // written-out description can hit this too, not only version 0
  // (task-11's own ruling 4).
  it("renders no description block in the hero when the vision's description is empty", async () => {
    renderPage(visionFixture({ description: "" }));

    const hero = await screen.findByTestId("vision-hero");
    expect(hero).toHaveTextContent('"Slow down together"');
    expect(within(hero).queryByTestId("vision-hero-description")).not.toBeInTheDocument();
  });

  it("renders one card per pillar, numbered Pillar 1, Pillar 2", async () => {
    renderPage(
      visionFixture({
        pillars: [
          pillarFixture({ name: "Us before logistics" }),
          pillarFixture({ name: "Money without fear" }),
        ],
      }),
    );

    const cards = await screen.findAllByTestId("vision-pillar-card");
    expect(cards).toHaveLength(2);
    expect(cards[0]).toHaveTextContent("Pillar 1");
    expect(cards[0]).toHaveTextContent("Us before logistics");
    expect(cards[1]).toHaveTextContent("Pillar 2");
    expect(cards[1]).toHaveTextContent("Money without fear");
  });

  it("a typed measure renders 2 of 2 and shows the met marker; 3 of 5 does not", async () => {
    renderPage(
      visionFixture({
        pillars: [
          pillarFixture({
            measures: [
              measureFixture({ label: "Date nights / month", current: 2, target: 2, met: true }),
              measureFixture({
                label: "Phone-free dinners / week",
                current: 3,
                target: 5,
                met: false,
                percent: 60,
              }),
            ],
          }),
        ],
      }),
    );

    const rows = await screen.findAllByTestId("vision-measure-row");
    expect(rows[0]).toHaveTextContent("2 of 2 ✓");
    expect(rows[1]).toHaveTextContent("3 of 5");
    expect(rows[1]).not.toHaveTextContent("✓");
  });

  it("a linked measure renders 62%", async () => {
    renderPage(
      visionFixture({
        pillars: [
          pillarFixture({
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
          }),
        ],
      }),
    );

    expect(await screen.findByTestId("vision-measure-row")).toHaveTextContent("62%");
  });

  // A measure with no figure renders its label and no number at all -- not
  // "0 of 0", not "0%". Asserts the ABSENCE of any digit in the row, not
  // merely the presence of the label (task-11's own ruling 3): a regression
  // that kept the label but let a stray "0" back in would pass a test that
  // only checked for the label's own text.
  it("a measure with hasFigure false renders its label and no number at all", async () => {
    renderPage(
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

    const row = await screen.findByTestId("vision-measure-row");
    expect(within(row).getByText("Weekends away this year")).toBeInTheDocument();
    expect(row).toHaveTextContent("Goal removed");
    expect(row.textContent).not.toMatch(/\d/);
  });

  it("a year with version 0 renders the empty state and its call to action, not a grid of blank cards", async () => {
    renderPage(visionFixture({ version: 0, theme: "", description: "", pillars: [], milestones: [] }));

    const empty = await screen.findByTestId("vision-empty-state");
    expect(empty).toHaveTextContent("No vision set for 2026");
    expect(screen.getByTestId("vision-empty-cta")).toBeInTheDocument();
    expect(screen.queryByTestId("vision-hero")).not.toBeInTheDocument();
    expect(screen.queryByTestId("vision-pillar-card")).not.toBeInTheDocument();
    expect(screen.queryByTestId("vision-milestone-card")).not.toBeInTheDocument();
  });

  it("milestones render in order with their year, title and note", async () => {
    renderPage(
      visionFixture({
        milestones: [
          { year: 2027, title: "Sabbatical month in Indonesia", note: "Kids meet Christine's side properly" },
          { year: 2029, title: "Upgrade to a bigger place", note: "Tied to car + housing goals" },
        ],
      }),
    );

    const cards = await screen.findAllByTestId("vision-milestone-card");
    expect(cards).toHaveLength(2);
    expect(cards[0]).toHaveTextContent("2027");
    expect(cards[0]).toHaveTextContent("Sabbatical month in Indonesia");
    expect(cards[0]).toHaveTextContent("Kids meet Christine's side properly");
    expect(cards[1]).toHaveTextContent("2029");
    expect(cards[1]).toHaveTextContent("Upgrade to a bigger place");
    expect(cards[1]).toHaveTextContent("Tied to car + housing goals");
  });

  // A limited member. GET /marriage/vision is marriage-AND-owner gated
  // (router.go's own comment on the group) -- the identical shape GET
  // /retros carries. This asserts the explanation's *presence*, not merely
  // that the generic error is missing -- docs/LEARNING.md pattern 2's own
  // reasoning (an absence assertion holds perfectly over a blank page).
  it("a limited member is told this is owner-only, not that something broke", async () => {
    stubFetchRoutes({
      "GET /api/v1/marriage/vision?year=2026": {
        status: 403,
        body: { error: { code: "FORBIDDEN", message: "Owner only." } },
      },
    });
    renderWithRouter(<VisionPage />);

    const explanation = await screen.findByTestId("vision-owner-only");
    expect(explanation).toHaveTextContent("Owner only");
    expect(explanation).toHaveTextContent("Vision & goals is visible to the household owner.");
    expect(screen.queryByTestId("vision-load-error")).not.toBeInTheDocument();
  });

  // Kept genuinely distinct from the 403 test above by asserting each
  // one's absence in the other's test, not merely its own presence.
  it("a non-403 failure renders the generic load error, not the owner-only explanation", async () => {
    stubFetchRoutes({
      "GET /api/v1/marriage/vision?year=2026": {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Something broke." } },
      },
    });
    renderWithRouter(<VisionPage />);

    expect(await screen.findByTestId("vision-load-error")).toHaveTextContent("Couldn't load this year's vision.");
    expect(screen.queryByTestId("vision-owner-only")).not.toBeInTheDocument();
  });
});
