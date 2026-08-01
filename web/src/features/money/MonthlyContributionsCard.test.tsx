// Unit tests for the Monthly contributions card's own bar-width and
// planned/actual arithmetic -- this component takes `goals` and `summary`
// as props and does no fetching of its own (GoalsPage.tsx passes its own
// already-parsed `data.goals` and `data.summary` straight through), so a
// plain `render` is enough here, BudgetHistoryModal.test.tsx's own
// precedent for a props-only card: no router, no QueryClientProvider, no
// stubbed fetch.
//
// Literal strings are asserted throughout, not GOAL_COPY's own exports --
// GoalsPage.test.tsx's own convention: importing the copy module here would
// make an assertion tautological against a typo in that same module.
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MonthlyContributionsCard } from "./MonthlyContributionsCard";
import type { Goal, GoalsResponse } from "./goalSchemas";

// The design's own Bali family trip figures scaled to minor units, same
// base fixture GoalsPage.test.tsx uses -- kept independent rather than
// imported so this file's fixtures never drift with that one's unrelated
// edits.
function goalFixture(overrides: Partial<Goal> = {}): Goal {
  return {
    id: "goal-1",
    name: "Bali family trip",
    targetMinor: 400000,
    currency: "SGD",
    targetMonth: "2026-12",
    plannedMonthlyMinor: 35000,
    contributedMinor: 260000,
    percent: 65,
    status: "on_track",
    requiredMonthlyMinor: 28000,
    requiredMonthlyOk: true,
    archivedAt: null,
    ...overrides,
  };
}

function summaryFixture(overrides: Partial<GoalsResponse["summary"]> = {}): GoalsResponse["summary"] {
  return {
    plannedMonthlyTotalMinor: 0,
    actualThisMonthMinor: 0,
    onTrackCount: 0,
    datedCount: 0,
    noDateCount: 0,
    excludedNoRate: 0,
    nextGoal: null,
    ...overrides,
  };
}

const SYMBOLS: Record<string, string> = { SGD: "S$", USD: "US$" };
const symbolFor = (currency: string) => SYMBOLS[currency];

function renderCard(goals: Goal[], summary: GoalsResponse["summary"], currency = "SGD") {
  render(<MonthlyContributionsCard goals={goals} summary={summary} currency={currency} symbolFor={symbolFor} />);
}

describe("MonthlyContributionsCard", () => {
  it("sizes each bar segment proportional to its own goal's planned monthly, and names each in the legend with its own figure", () => {
    // The design's own four goals and figures (Household Dashboard.dc.html's
    // Monthly contributions bar: 800/500/400/350 -> 39%/24%/20%/17%).
    const goals = [
      goalFixture({ id: "g1", name: "Kids' education", plannedMonthlyMinor: 80000 }),
      goalFixture({ id: "g2", name: "Emergency fund", plannedMonthlyMinor: 50000 }),
      goalFixture({ id: "g3", name: "New family car", plannedMonthlyMinor: 40000 }),
      goalFixture({ id: "g4", name: "Bali family trip", plannedMonthlyMinor: 35000 }),
    ];
    renderCard(goals, summaryFixture({ plannedMonthlyTotalMinor: 205000, actualThisMonthMinor: 180000 }));

    const segments = screen.getAllByTestId("monthly-contributions-segment");
    expect(segments).toHaveLength(4);
    expect(Number.parseFloat(segments[0].style.width)).toBeCloseTo((80000 / 205000) * 100, 5);
    expect(Number.parseFloat(segments[1].style.width)).toBeCloseTo((50000 / 205000) * 100, 5);
    expect(Number.parseFloat(segments[2].style.width)).toBeCloseTo((40000 / 205000) * 100, 5);
    expect(Number.parseFloat(segments[3].style.width)).toBeCloseTo((35000 / 205000) * 100, 5);

    const legendItems = screen.getAllByTestId("monthly-contributions-legend-item");
    expect(legendItems).toHaveLength(4);
    expect(legendItems[0]).toHaveTextContent("Kids' education · S$800.00");
    expect(legendItems[3]).toHaveTextContent("Bali family trip · S$350.00");
  });

  it("renders the planned total and the actual figure both formatted, under distinct labels", () => {
    renderCard(
      [goalFixture({ plannedMonthlyMinor: 205000 })],
      summaryFixture({ plannedMonthlyTotalMinor: 205000, actualThisMonthMinor: 180000 }),
    );

    expect(screen.getByText("Planned monthly total")).toBeInTheDocument();
    expect(screen.getByTestId("monthly-contributions-planned")).toHaveTextContent("S$2,050.00");
    expect(screen.getByText("Actual this month")).toBeInTheDocument();
    expect(screen.getByTestId("monthly-contributions-actual")).toHaveTextContent("S$1,800.00");
  });

  it("states in words when nothing has been logged this month, rather than hiding the actual figure", () => {
    renderCard(
      [goalFixture({ plannedMonthlyMinor: 205000 })],
      summaryFixture({ plannedMonthlyTotalMinor: 205000, actualThisMonthMinor: 0 }),
    );

    // The figure still renders -- "S$0.00", never omitted.
    expect(screen.getByTestId("monthly-contributions-actual")).toHaveTextContent("S$0.00");
    expect(screen.getByTestId("monthly-contributions-diff")).toHaveTextContent("Nothing logged yet this month.");
  });

  it("says the household put in more than planned when actual exceeds it, not the reverse", () => {
    renderCard(
      [goalFixture({ plannedMonthlyMinor: 205000 })],
      summaryFixture({ plannedMonthlyTotalMinor: 205000, actualThisMonthMinor: 250000 }),
    );

    const sentence = screen.getByTestId("monthly-contributions-diff");
    expect(sentence).toHaveTextContent("S$450.00 more than planned this month.");
    expect(sentence.textContent).not.toMatch(/short/);
  });

  it("says the household is short of plan when actual falls under it", () => {
    renderCard(
      [goalFixture({ plannedMonthlyMinor: 205000 })],
      summaryFixture({ plannedMonthlyTotalMinor: 205000, actualThisMonthMinor: 180000 }),
    );

    const sentence = screen.getByTestId("monthly-contributions-diff");
    expect(sentence).toHaveTextContent("S$250.00 short of plan this month.");
    expect(sentence.textContent).not.toMatch(/more than/);
  });

  it("renders no differ sentence when actual matches planned exactly", () => {
    renderCard(
      [goalFixture({ plannedMonthlyMinor: 205000 })],
      summaryFixture({ plannedMonthlyTotalMinor: 205000, actualThisMonthMinor: 205000 }),
    );

    expect(screen.queryByTestId("monthly-contributions-diff")).not.toBeInTheDocument();
  });

  it("renders the no-exchange-rate exclusion note when excludedNoRate is greater than zero", () => {
    renderCard([goalFixture()], summaryFixture({ excludedNoRate: 2 }));

    expect(screen.getByTestId("goals-excluded-no-rate")).toHaveTextContent(
      "2 goals are not counted: no exchange rate.",
    );
  });

  it("omits the exclusion note when excludedNoRate is zero", () => {
    renderCard([goalFixture()], summaryFixture({ excludedNoRate: 0 }));

    expect(screen.queryByTestId("goals-excluded-no-rate")).not.toBeInTheDocument();
  });

  it("an archived goal contributes nothing to the bar, the legend, or either total", () => {
    const goals = [
      goalFixture({ id: "g1", name: "Live goal", plannedMonthlyMinor: 50000, archivedAt: null }),
      goalFixture({
        id: "g2",
        name: "Archived car",
        plannedMonthlyMinor: 999999,
        archivedAt: "2026-06-01T00:00:00Z",
      }),
    ];
    // The two totals below come straight from summary, never re-derived
    // from `goals` -- if this component silently summed goals itself
    // instead, the archived goal's huge figure would leak into a total
    // that never came from the server (the task brief's own "the card does
    // no arithmetic on minor units beyond bar segment widths" rule).
    renderCard(goals, summaryFixture({ plannedMonthlyTotalMinor: 50000, actualThisMonthMinor: 40000 }));

    const segments = screen.getAllByTestId("monthly-contributions-segment");
    expect(segments).toHaveLength(1);
    // The one live segment fills the whole bar -- the archived goal drops
    // out of the local denominator too, not just the numerator.
    expect(Number.parseFloat(segments[0].style.width)).toBeCloseTo(100, 5);

    const legendItems = screen.getAllByTestId("monthly-contributions-legend-item");
    expect(legendItems).toHaveLength(1);
    expect(legendItems[0]).toHaveTextContent("Live goal");
    expect(screen.queryByText(/Archived car/)).not.toBeInTheDocument();

    expect(screen.getByTestId("monthly-contributions-planned")).toHaveTextContent("S$500.00");
  });
});
