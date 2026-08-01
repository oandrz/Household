// contributionSourceLabel's three real cases (manual, starting_balance,
// budget_rollover) are already pinned end to end through the panel itself
// (GoalContributionsPanel.test.tsx's own "labels each row by its source"
// test) -- that is the layer actually reachable through a real GET response,
// since goalContributionSchema's z.enum refuses an unrecognised `source`
// before this function ever runs. This file covers only the belt-and-
// suspenders `default` branch directly, the one case that test cannot reach
// (contributionSourceLabel's own header comment explains why).
import { describe, expect, it } from "vitest";
import { contributionSourceLabel } from "./goalCopy";
import type { GoalContribution } from "./goalSchemas";

describe("contributionSourceLabel", () => {
  // Finding 3 of the goals-branch review: the default branch used to return
  // GOAL_COPY.manualContributionFallback ("Manual contribution"), asserting
  // something this function does not actually know about the row. A cast
  // stands in for the "future looser schema" the function's own comment
  // names -- z.enum makes this unreachable through a real response, but the
  // `default` case must still not claim "manual" about a source it refused.
  it("labels an unrecognised source neutrally, never as a manual contribution", () => {
    const unknownSource = "some_future_source" as GoalContribution["source"];
    expect(contributionSourceLabel(unknownSource, null, "")).toBe("Contribution");
  });
});
