// Zod mirrors of the DTOs in api/internal/adapter/http/goal_handlers.go
// (goalDTO, goalsResponse, nextGoalDTO, goalsSummaryDTO, contributionDTO,
// contributionsResponse, goalResponse). These follow the backend's own
// structs rather than the design doc, same convention as budgetSchemas.ts --
// the backend's comments are what say which fields can be null and why.
import { z } from "zod";

// goalStatusSchema mirrors domain.GoalStatus. "none" is a real status (a
// goal with no target date), not a missing one -- domain/goal.go's own
// comment on GoalStatus.
const goalStatusSchema = z.enum(["on_track", "behind", "achieved", "none"]);

// contributionSourceSchema mirrors domain.ContributionSource.
const contributionSourceSchema = z.enum(["manual", "starting_balance", "budget_rollover"]);

// goalSchema mirrors goalDTO -- one card on the Goals screen, and also the
// shape every write route answers inside {"goal": ...} (goalResponseSchema
// below). targetMonth and archivedAt are nullable, not optional: both are Go
// *T pointers with no `omitempty`, so they always serialise -- `null` when
// unset, never an absent key.
export const goalSchema = z.object({
  id: z.string(),
  name: z.string(),
  targetMinor: z.number(),
  currency: z.string(),
  targetMonth: z.string().nullable(),
  plannedMonthlyMinor: z.number(),
  contributedMinor: z.number(),
  percent: z.number(),
  status: goalStatusSchema,
  requiredMonthlyMinor: z.number(),
  requiredMonthlyOk: z.boolean(),
  archivedAt: z.string().nullable(),
});
export type Goal = z.infer<typeof goalSchema>;

// goalResponseSchema is the {"goal": {...}} shape every goal write route
// answers -- goal_handlers.go's own writeGoal. Only useGoals.ts's createGoal
// parses through this: update/archive/restore discard their write response
// and invalidate instead, the useBudget.ts rename/archive/restore
// convention (the month/list response is the one place every derived figure
// is computed together, so re-deriving them from a write response here would
// duplicate the service's own math on the client).
export const goalResponseSchema = z.object({
  goal: goalSchema,
});

// nextGoalSchema mirrors nextGoalDTO, the summary's "up next" pointer.
// Nested directly in goalsSummarySchema below rather than exported on its
// own -- nothing outside this file's own goalsResponseSchema needs it by
// name.
const nextGoalSchema = z.object({
  id: z.string(),
  name: z.string(),
  targetMonth: z.string().nullable(),
});

// goalsSummarySchema mirrors goalsSummaryDTO -- the page header and the
// Monthly contributions card. Every count here is live goals only, whether
// or not the `goals` array beside it also carries archived ones
// (handleListGoals's own comment: include_archived is a union, not a filter
// swap). nextGoal is nullable, not optional -- a household with no live
// goals yet still gets a `"nextGoal": null` key, never an absent one.
const goalsSummarySchema = z.object({
  plannedMonthlyTotalMinor: z.number(),
  actualThisMonthMinor: z.number(),
  onTrackCount: z.number(),
  datedCount: z.number(),
  noDateCount: z.number(),
  excludedNoRate: z.number(),
  nextGoal: nextGoalSchema.nullable(),
});

// goalsResponseSchema mirrors goalsResponse, GET /goals' whole body. The
// same schema serves both `GET /goals` and `GET /goals?include_archived=true`
// -- the query string changes which rows the server includes in `goals`, not
// the wire shape, so there is nothing here for the two calls to disagree on.
export const goalsResponseSchema = z.object({
  currency: z.string(),
  goals: z.array(goalSchema),
  summary: goalsSummarySchema,
});
export type GoalsResponse = z.infer<typeof goalsResponseSchema>;

// goalContributionSchema mirrors contributionDTO. sourceBudgetMonth is
// nullable and set only when source is "budget_rollover" --
// domain.GoalContribution's own comment.
export const goalContributionSchema = z.object({
  id: z.string(),
  amountMinor: z.number(),
  occurredOn: z.string(),
  note: z.string(),
  source: contributionSourceSchema,
  sourceBudgetMonth: z.string().nullable(),
});
export type GoalContribution = z.infer<typeof goalContributionSchema>;

// goalContributionsResponseSchema mirrors contributionsResponse, GET
// /goals/{id}/contributions' whole body. There is no schema here for POST's
// own {"contribution": ...} reply -- useGoals.ts's addContribution discards
// it and refetches instead, so nothing in this file ever parses it.
export const goalContributionsResponseSchema = z.object({
  contributions: z.array(goalContributionSchema),
});
export type GoalContributionsResponse = z.infer<typeof goalContributionsResponseSchema>;
