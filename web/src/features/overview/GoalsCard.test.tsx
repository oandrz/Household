// GoalsCard.tsx is pure presentation over an already-fetched GoalsResponse
// (no fetch of its own), so these tests build the response by hand rather
// than stubbing a route -- the same shape BudgetCard's own figures would be
// tested at, had it a standalone file. renderWithRouter is still needed: the
// empty state's "Create a goal" is a real <Link>, which throws outside a
// router context.
import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { GoalsCard } from "./GoalsCard";
import type { GoalsResponse } from "../money/goalSchemas";

// summary is the only part of GoalsResponse this card reads -- goals stays
// an empty array in every fixture below, the same "composed from summary,
// never by filtering the goals array" contract GoalsPage.tsx's own
// trackClause comment states, restated here by construction rather than by
// comment: nothing in this file could accidentally start reading `.goals`.
function goalsFixture(summaryOverrides: Partial<GoalsResponse["summary"]> = {}): GoalsResponse {
  return {
    currency: "SGD",
    goals: [],
    summary: {
      plannedMonthlyTotalMinor: 0,
      actualThisMonthMinor: 0,
      onTrackCount: 0,
      datedCount: 0,
      noDateCount: 0,
      excludedNoRate: 0,
      nextGoal: null,
      ...summaryOverrides,
    },
  };
}

describe("GoalsCard", () => {
  // The first query in every test here is `find`, not `get` -- renderWithRouter
  // mounts a real TanStack `RouterProvider`, whose own initial transition
  // resolves asynchronously even with no fetch of its own in play. Asserting
  // synchronously before that settles would find nothing at all, the same
  // gap GoalModal.test.tsx's own fillCreateBasics helper already opens with
  // an initial `await screen.findByLabelText(...)` for the identical reason.
  it("shows the on-track count and the next dated goal", async () => {
    renderWithRouter(
      <GoalsCard
        goals={goalsFixture({
          onTrackCount: 3,
          datedCount: 4,
          nextGoal: { id: "g1", name: "Bali", targetMonth: "2026-12" },
        })}
      />,
    );

    expect(await screen.findByText("Goals on track")).toBeInTheDocument();
    expect(screen.getByText("3 of 4")).toBeInTheDocument();
    expect(screen.getByText("next: Bali · Dec 2026")).toBeInTheDocument();
  });

  // The formulas table's own rule: datedCount === 0 means every live goal is
  // dateless, so there is nothing to be "on track" against. "0 of 0" would
  // read as failure for a household that has simply not set a date yet.
  it("hides the on-track figure rather than showing 0 of 0 when no goal has a date", async () => {
    renderWithRouter(<GoalsCard goals={goalsFixture({ datedCount: 0, noDateCount: 2 })} />);

    // The card still says something true about its own goals, rather than
    // rendering a heading over nothing -- the same failure mode
    // OverviewPage's own limited-member panel exists to avoid.
    expect(await screen.findByText("2 with no date")).toBeInTheDocument();
    expect(screen.queryByText(/^\d+ of \d+$/)).not.toBeInTheDocument();
    expect(screen.queryByText(/^next:/)).not.toBeInTheDocument();
  });

  // Both clauses can be true at once: some goals dated, some not. The
  // denominator that shrank (datedCount excludes the dateless ones) is named
  // beside the figure it shrank, not left for the reader to notice on their
  // own -- the same honesty rule GoalsPage.tsx's own subtitle follows.
  it("names how many goals were excluded for having no date, alongside the ones that are dated", async () => {
    renderWithRouter(
      <GoalsCard
        goals={goalsFixture({
          onTrackCount: 2,
          datedCount: 3,
          noDateCount: 1,
          nextGoal: { id: "g1", name: "Emergency fund", targetMonth: "2027-01" },
        })}
      />,
    );

    expect(await screen.findByText("2 of 3")).toBeInTheDocument();
    expect(screen.getByText("1 with no date")).toBeInTheDocument();
  });

  // The state a brand-new household is in on day one: no goals at all yet,
  // not merely none with a date. Distinct from the case above -- a bare
  // heading with nothing beneath it is exactly the blank-card shape the
  // interim Overview's own defect took, so this offers a way in instead.
  it("offers a way to create the household's first goal when it has none yet", async () => {
    renderWithRouter(<GoalsCard goals={goalsFixture()} />);

    expect(await screen.findByText("Goals on track")).toBeInTheDocument();
    expect(screen.getByText("No goals yet")).toBeInTheDocument();
    expect(screen.queryByText(/^\d+ of \d+$/)).not.toBeInTheDocument();

    const link = screen.getByRole("link", { name: "Create a goal" });
    expect(link).toHaveAttribute("href", "/money/goals");
  });
});
