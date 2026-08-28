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
import type { Goal, GoalsResponse } from "../money/goalSchemas";

// A minimal, valid live goal -- fields beyond `status`/`archivedAt` don't
// matter to GoalsCard (it never reads an individual goal, only
// `goals.goals.length`), but the array has to hold *something* shaped like a
// real Goal now that the empty-state check counts it rather than deriving
// from summary (see the fix note on `hasAnyGoals` in GoalsCard.tsx: an
// achieved goal is in neither `datedCount` nor `noDateCount`, so a fixture
// with real goals but all-zero summary counts is exactly the case this file
// has to be able to represent).
function goalFixture(overrides: Partial<Goal> = {}): Goal {
  return {
    id: "goal-1",
    name: "Bali trip",
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

function goalsFixture(
  summaryOverrides: Partial<GoalsResponse["summary"]> = {},
  goals: Goal[] = [],
): GoalsResponse {
  return {
    currency: "SGD",
    goals,
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
        goals={goalsFixture(
          {
            onTrackCount: 3,
            datedCount: 4,
            nextGoal: { id: "g1", name: "Bali", targetMonth: "2026-12" },
          },
          [goalFixture()],
        )}
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
    renderWithRouter(
      <GoalsCard
        goals={goalsFixture({ datedCount: 0, noDateCount: 2 }, [
          goalFixture({ id: "g1", targetMonth: null }),
          goalFixture({ id: "g2", targetMonth: null }),
        ])}
      />,
    );

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
        goals={goalsFixture(
          {
            onTrackCount: 2,
            datedCount: 3,
            noDateCount: 1,
            nextGoal: { id: "g1", name: "Emergency fund", targetMonth: "2027-01" },
          },
          [goalFixture({ id: "g1" }), goalFixture({ id: "g2" }), goalFixture({ id: "g3" }), goalFixture({ id: "g4", targetMonth: null })],
        )}
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

  // The exact hole a review round found: a household that fully funds its
  // one and only goal, and hasn't archived it -- an ordinary state, since
  // nothing archives a goal automatically on reaching its target. The
  // backend's own summary counts (api/internal/usecase/goal.go's List loop)
  // put an achieved goal in neither `datedCount` nor `noDateCount` -- it
  // checks `status == domain.GoalAchieved` before the dated/undated split,
  // for a dated or an undated goal alike -- so a household in exactly this
  // state has `datedCount: 0, noDateCount: 0` despite a real, live,
  // unarchived goal sitting in `goals.goals`. The empty state must not
  // trigger here: this household is not goal-less, it is done.
  it("does not show the empty state for a household whose only goal is achieved", async () => {
    renderWithRouter(
      <GoalsCard
        goals={goalsFixture({ onTrackCount: 0, datedCount: 0, noDateCount: 0, nextGoal: null }, [
          goalFixture({ id: "g1", status: "achieved", percent: 100 }),
        ])}
      />,
    );

    // The heading proves the card actually rendered (not a blank div) before
    // the negative assertions below are asked to mean anything.
    expect(await screen.findByText("Goals on track")).toBeInTheDocument();
    expect(screen.queryByText("No goals yet")).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Create a goal" })).not.toBeInTheDocument();
    // None of the dated/next/no-date lines apply to an achieved goal, so none
    // of them render. What fills the card instead is asserted in its own test
    // below -- an earlier round of this fix stopped here, at a card that
    // correctly skipped the empty state and then had nothing left to draw.
    expect(screen.queryByText(/^\d+ of \d+$/)).not.toBeInTheDocument();
    expect(screen.queryByText(/^next:/)).not.toBeInTheDocument();
    expect(screen.queryByText(/with no date/)).not.toBeInTheDocument();
  });

  // The state a household reaches by succeeding: its only goal is funded and
  // not yet archived. Every clause below `hasAnyGoals` is null at once -- an
  // achieved goal is counted in neither `datedCount` nor `noDateCount` and is
  // never `nextGoal` -- so the guard above correctly refused the empty state
  // and the card then painted its heading over blank space. The guard was
  // right; there was simply no fourth branch behind it.
  it("says something true when every goal is already achieved", async () => {
    renderWithRouter(
      <GoalsCard
        goals={goalsFixture({ datedCount: 0, noDateCount: 0, nextGoal: null }, [
          goalFixture({ id: "g1", name: "Japan 2027", status: "achieved", percent: 100 }),
        ])}
      />,
    );

    expect(await screen.findByText("Goals on track")).toBeInTheDocument();
    expect(screen.getByText("All goals reached")).toBeInTheDocument();

    // Not the never-had-a-goal empty state: this household has one, and it won.
    expect(screen.queryByText("No goals yet")).not.toBeInTheDocument();
  });

  // The fourth branch must stay closed for every household that still has
  // something left to do, or it would sit above a real "2 of 3" and tell a
  // household mid-way that it was finished.
  it("does not claim every goal is reached while any clause still applies", async () => {
    renderWithRouter(
      <GoalsCard
        goals={goalsFixture(
          { onTrackCount: 2, datedCount: 3, noDateCount: 1, nextGoal: null },
          [goalFixture({ id: "g1" }), goalFixture({ id: "g2" })],
        )}
      />,
    );

    expect(await screen.findByText("2 of 3")).toBeInTheDocument();
    expect(screen.queryByText("All goals reached")).not.toBeInTheDocument();
  });
});
