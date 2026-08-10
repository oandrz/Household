// Zod mirrors of the DTOs in api/internal/adapter/http/bill_handlers.go
// (billDTO, billResponse, nextDueBillDTO, billsSummaryDTO, billPaymentDTO,
// billsResponse, billPaymentResponse). Same convention as goalSchemas.ts:
// these follow the backend's own structs, not the design doc -- the
// backend's comments are what say which fields can be null and why, and
// task-9-report.md/task-10-report.md (the two prior tasks in this series)
// recorded the field lists this file was checked against line by line.
import { z } from "zod";

// cadenceSchema mirrors domain.Cadence. Kept private to this file -- Bill's
// own inferred `cadence` field already carries this union everywhere a
// caller needs it, the same reason goalSchemas.ts never exports
// goalStatusSchema either.
const cadenceSchema = z.enum(["one_off", "monthly", "quarterly", "yearly"]);

// billSchema mirrors billDTO -- one row on the Bills screen, and also the
// shape every write route answers inside {"bill": ...} (billResponseSchema
// below). nextDue and archivedAt are nullable, not optional: both are Go
// *T pointers with no `omitempty`, so they always serialise -- `null` when
// unset, never an absent key.
//
// `settled` must stay in this schema. A settled one-off (paid, no next
// occurrence) has nextDue: null and satisfies neither Due soon's nor
// Later's own 30-day/overdue definition -- both require a non-null nextDue
// -- yet the header's "N of M on autopay" count still includes it
// (billDTO's own comment: Task 7's review found this exact gap in an
// earlier sketch of the DTO, a bill counted in a figure and visible
// nowhere on the page).
export const billSchema = z.object({
  id: z.string(),
  name: z.string(),
  amountMinor: z.number(),
  currency: z.string(),
  cadence: cadenceSchema,
  nextDue: z.string().nullable(),
  categoryId: z.string(),
  categoryName: z.string(),
  payFromAccountId: z.string(),
  accountName: z.string(),
  paidByMembershipId: z.string(),
  autopay: z.boolean(),
  isSubscription: z.boolean(),
  overdue: z.boolean(),
  dueSoon: z.boolean(),
  settled: z.boolean(),
  archivedAt: z.string().nullable(),
});
export type Bill = z.infer<typeof billSchema>;

// billResponseSchema is the {"bill": {...}} shape every bill write route
// answers -- Create, Update, Archive and Restore alike (writeBill's own
// comment) -- mirroring goalResponseSchema's identical wrapper convention.
export const billResponseSchema = z.object({
  bill: billSchema,
});

// nextDueBillSchema mirrors nextDueBillDTO, billsSummarySchema's "what's
// coming up next" card. Nested directly below rather than exported on its
// own -- nothing outside this file's own billsResponseSchema needs it by
// name, the same choice goalSchemas.ts makes for nextGoalSchema.
// amountMinor/currency are deliberately the BILL's own, never converted to
// the household's primary -- nextDueBillDTO's own comment: converting only
// this figure would pair an amount with a currency symbol that disagrees
// with it.
const nextDueBillSchema = z.object({
  billId: z.string(),
  billName: z.string(),
  dueOn: z.string(),
  amountMinor: z.number(),
  currency: z.string(),
  overdue: z.boolean(),
  autopay: z.boolean(),
});

// billsSummarySchema mirrors billsSummaryDTO -- the page header and the
// three stat cards. Currency, dueThisMonthMinor, paidSoFarMinor,
// subscriptionsMonthlyMinor and subscriptionsAnnualMinor are all in the
// household's primary currency (billsSummaryDTO's own comment) -- unlike
// nextDue's own figures above. nextDue is nullable, not optional -- a
// household with no live bill carrying a next_due still gets a
// `"nextDue": null` key, never an absent one (nextDueBillDTO's own comment:
// nil is the field BillsSummary.NextDueOn itself gates on).
export const billsSummarySchema = z.object({
  currency: z.string(),
  dueThisMonthMinor: z.number(),
  paidSoFarMinor: z.number(),
  nextDue: nextDueBillSchema.nullable(),
  autopayCount: z.number(),
  billCount: z.number(),
  subscriptionsMonthlyMinor: z.number(),
  subscriptionsAnnualMinor: z.number(),
  excludedNoRate: z.number(),
});
export type BillsSummary = z.infer<typeof billsSummarySchema>;

// billPaymentSchema mirrors billPaymentDTO -- one row of "Paid this month",
// and also payBillRequest's response half (billPaymentResponseSchema
// below). billId (alongside id, the payment's own) is what the undo route
// (DELETE /bills/{id}/payments/{paymentId}) needs to build its URL.
export const billPaymentSchema = z.object({
  id: z.string(),
  billId: z.string(),
  billName: z.string(),
  dueOn: z.string(),
  paidOn: z.string(),
  amountMinor: z.number(),
  currency: z.string(),
  autopay: z.boolean(),
});
export type BillPayment = z.infer<typeof billPaymentSchema>;

// billsResponseSchema mirrors billsResponse, GET /bills' whole body. The
// same schema serves both `GET /bills` and `GET /bills?include_archived=true`
// -- the query string changes which rows the server includes in `bills`, not
// the wire shape (billsResponse's own comment). No top-level `currency`
// field, unlike goalsResponseSchema -- billsResponse names exactly three
// fields (task-9-report.md's own note on this deliberate difference).
export const billsResponseSchema = z.object({
  bills: z.array(billSchema),
  paidThisMonth: z.array(billPaymentSchema),
  summary: billsSummarySchema,
});
export type BillsResponse = z.infer<typeof billsResponseSchema>;

// billPaymentResponseSchema is POST /bills/{id}/pay's whole body: the
// payment just recorded plus the bill as it now stands (nextDue advanced,
// or settled) -- billPaymentResponse's own comment on why the caller never
// needs a second GET to see what paying just changed.
export const billPaymentResponseSchema = z.object({
  payment: billPaymentSchema,
  bill: billSchema,
});
