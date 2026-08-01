// Zod mirrors of the DTOs in api/internal/adapter/http/budget_handlers.go
// (budgetMonthResponse, putBudgetResponse, budgetHistoryResponse) and
// category_handlers.go (categoryDTO, categoryResponse). These follow the
// backend's own structs rather than the design doc, same convention as
// transactionSchemas.ts -- the backend's comments are what say which fields
// can be absent and why.
import { z } from "zod";

// categorySchema mirrors category_handlers.go's categoryDTO, which grew
// `archived` in Task 10. transactionSchemas.ts already exports its own
// `categorySchema` without that field; redefining rather than editing it
// here is deliberate -- touching that file (and the ledger dropdown it
// feeds) is out of this task's scope, so the two stay two names until a
// later task reconciles them on purpose rather than by accident. The failure
// mode of picking the wrong one is silent, not a thrown error: zod strips
// unknown keys by default rather than rejecting them, so parsing a
// `{..., archived: true}` payload against transactionSchemas.ts's
// archived-less sibling would not throw -- it would quietly drop `archived`
// off the parsed object, and every "is this category archived" check
// downstream would see `undefined` and read as false.
export const categorySchema = z.object({
  id: z.string(),
  name: z.string(),
  kind: z.enum(["expense", "income"]),
  archived: z.boolean(),
});
export type Category = z.infer<typeof categorySchema>;

// categoryResponse is the {"category": {...}} shape every category write
// route answers (category_handlers.go's own comment: "matched by
// budget_api_test.go's categoryBody").
export const categoryResponseSchema = z.object({
  category: categorySchema,
});

// budgetLineSchema is both a GET line and a PUT line -- {categoryId,
// capMinor} reads and writes identically on the wire, mirroring
// budgetLineDTO's own comment for why one Go type serves both directions.
const budgetLineSchema = z.object({
  categoryId: z.string(),
  capMinor: z.number(),
});

// budgetSchema mirrors budgetDTO. expectedIncomeMinor is nullable, not
// optional: the field is always present on the wire (a Go *int64 pointer
// serialises to `null`, never an absent key), and "not provided" is a real
// value this screen has to render, not a missing one.
const budgetSchema = z.object({
  expectedIncomeMinor: z.number().nullable(),
  lines: z.array(budgetLineSchema),
});

// budgetCategoryLineSchema mirrors budgetCategoryDTO -- the Budget screen's
// per-category row, distinct from `categorySchema` above (that one is the
// household's whole category list; this one is one line of one month's
// budget, carrying spend and over-cap state the plain category list never
// has).
const budgetCategoryLineSchema = z.object({
  categoryId: z.string(),
  name: z.string(),
  archived: z.boolean(),
  capMinor: z.number(),
  spentMinor: z.number(),
  over: z.boolean(),
});

const budgetPersonSchema = z.object({
  membershipId: z.string(),
  name: z.string(),
  spentMinor: z.number(),
});

// budgetMonthResponseSchema mirrors budgetMonthResponse, GET
// /budgets/{month}'s whole body. `budget` is nullable (the never-budgeted
// empty state); every figure below it is still real even then, per
// BudgetService.Month's own doc comment.
//
// `excludedNoRate` here is a **count** (`int` on the wire), unlike
// transactionSchemas.ts's `monthSummarySchema.excludedNoRate`, which is a
// list of excluded transactions -- budget_handlers.go's own comment says
// why: this screen has no ledger rows to list them against, only the "N
// transactions excluded" note. Do not copy the array shape here.
export const budgetMonthResponseSchema = z.object({
  currency: z.string(),
  month: z.string(),
  budget: budgetSchema.nullable(),
  categories: z.array(budgetCategoryLineSchema),
  budgetedMinor: z.number(),
  spentMinor: z.number(),
  remainingMinor: z.number(),
  percentUsed: z.number(),
  percentOk: z.boolean(),
  daysLeft: z.number(),
  dailyPaceMinor: z.number(),
  dailyPaceOk: z.boolean(),
  byPerson: z.array(budgetPersonSchema),
  excludedNoRate: z.number(),
  overCount: z.number(),
  // Task 9's rollover stamp: nullable, not optional -- both are Go pointer
  // fields with no `omitempty` (budget_handlers.go's own comment), so they
  // always serialise, `null` until POST .../rollover succeeds for this
  // month and both populated together afterward, never one without the
  // other (the database's own rollover_stamp_is_whole CHECK constraint).
  rolledOverAt: z.string().nullable(),
  rolloverGoalId: z.string().nullable(),
  // rolloverAmountMinor is the fix for the finding this closes: the amount a
  // rollover actually moved, read off the goal_contributions row it wrote --
  // never `remainingMinor` above, which is recomputed on every GET from
  // whatever transactions exist in this month right now. It moves in
  // lockstep with `rolledOverAt`/`rolloverGoalId` (null until a rollover
  // happens, populated and then fixed from then on), but is not covered by
  // the database's rollover_stamp_is_whole CHECK -- it comes from a
  // different table, joined in only by BudgetRepository.Get.
  rolloverAmountMinor: z.number().nullable(),
});
export type BudgetMonthResponse = z.infer<typeof budgetMonthResponseSchema>;

// putBudgetResponseSchema mirrors putBudgetResponse -- PUT's own reply.
// useBudget's `save` parses PUT's response with this schema (pinning it
// against wire drift) but still invalidates and refetches the month
// afterward rather than writing this value straight into the query cache --
// the month response is the one place every derived figure (spent,
// remaining, percent used, over count...) is computed together, and this
// response carries none of that, only the saved budget itself.
export const putBudgetResponseSchema = z.object({
  budget: budgetSchema,
});

// budgetHistoryMonthSchema mirrors budgetHistoryMonthDTO. There is no
// `result` field on the wire -- the spec's "Result" column (Spent minus
// Budgeted, signed) is computed client-side from budgetedMinor/spentMinor,
// not sent as its own number.
const budgetHistoryMonthSchema = z.object({
  month: z.string(),
  budgetedMinor: z.number(),
  spentMinor: z.number(),
  closed: z.boolean(),
});

export const budgetHistoryResponseSchema = z.object({
  months: z.array(budgetHistoryMonthSchema),
});
export type BudgetHistoryResponse = z.infer<typeof budgetHistoryResponseSchema>;
