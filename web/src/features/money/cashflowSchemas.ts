// Shapes of GET /api/v1/cashflow and the confirmation write, mirroring the
// DTOs in cashflow_handlers.go. The fixed vocabularies are z.enum on purpose:
// a value the frontend has never heard of must fail parsing, not render as a
// wrong colour or a blank label.
import { z } from "zod";

export const cashflowColourSchema = z.enum(["grey", "red", "yellow", "green"]);
export type CashflowColour = z.infer<typeof cashflowColourSchema>;

const cashflowMonthStatusSchema = z.enum(["solid", "grey", "current", "future", "none"]);

export const cashflowGreyReasonSchema = z.enum(["no_income", "under_half_usual", "unconvertible"]);
export type CashflowGreyReason = z.infer<typeof cashflowGreyReasonSchema>;

export const cashflowReasonCodeSchema = z.enum([
  "no_finished_month",
  "last_month_not_enough_data",
  "no_months_to_average",
  "left_not_positive",
  "cushion_under_1",
  "kept_under_10",
  "last_month_negative",
  "cushion_under_3",
  "cushion_unknown",
  "bill_due_soon",
]);

// "" means the cushion is known; any other value says why it is not.
const cashflowCushionReasonSchema = z.enum([
  "",
  "no_cash_accounts",
  "unconvertible_account",
  "no_solid_months",
  "no_spending",
]);

const cashflowContributionKindSchema = z.enum(["spread_bill", "loan"]);

const cashflowReasonSchema = z.object({
  code: cashflowReasonCodeSchema,
  keptBasisPoints: z.number().nullable(),
  cushionTenths: z.number().nullable(),
  billName: z.string(),
});
export type CashflowReason = z.infer<typeof cashflowReasonSchema>;

const cashflowLightSchema = z.object({
  colour: cashflowColourSchema,
  reasons: z.array(cashflowReasonSchema),
});

const cashflowAverageSchema = z.object({
  months: z.number(),
  inMinor: z.number(),
  outMinor: z.number(),
  leftMinor: z.number(),
  keptBasisPoints: z.number().nullable(),
});

const cashflowCushionSchema = z.object({
  known: z.boolean(),
  tenths: z.number().nullable(),
  reason: cashflowCushionReasonSchema,
});

const cashflowConfirmationSchema = z.object({
  confirmedAt: z.string(),
  confirmedByName: z.string(),
});

const cashflowContributionSchema = z.object({
  kind: cashflowContributionKindSchema,
  name: z.string(),
  amountMinor: z.number(),
  paidOn: z.string(),
});

export const cashflowMonthSchema = z.object({
  month: z.string(),
  status: cashflowMonthStatusSchema,
  inMinor: z.number(),
  outMinor: z.number(),
  leftMinor: z.number(),
  keptBasisPoints: z.number().nullable(),
  spreadMinor: z.number(),
  loanMinor: z.number(),
  transactionCount: z.number(),
  grey: z.array(cashflowGreyReasonSchema),
  unconvertibleCurrencies: z.array(z.string()),
  projectedLeftMinor: z.number().nullable(),
  confirmation: cashflowConfirmationSchema.nullable(),
  contributions: z.array(cashflowContributionSchema),
});
export type CashflowMonth = z.infer<typeof cashflowMonthSchema>;

const cashflowBillToComeSchema = z.object({
  billId: z.string(),
  name: z.string(),
  amountMinor: z.number(),
  nextDue: z.string(),
  unconvertible: z.boolean(),
});

const cashflowYearSchema = z.object({
  year: z.number(),
  months: z.array(cashflowMonthSchema),
  keptSoFarMinor: z.number(),
  incomeMinor: z.number(),
  greyMonths: z.number(),
  keptBasisPoints: z.number().nullable(),
  sameMonthsLastYearMinor: z.number().nullable(),
  headingForMinor: z.number().nullable(),
  billsToCome: z.array(cashflowBillToComeSchema),
});

export const cashflowPageSchema = z.object({
  currency: z.string(),
  hasTransactions: z.boolean(),
  oldestYear: z.number(),
  light: cashflowLightSchema,
  average: cashflowAverageSchema,
  cushion: cashflowCushionSchema,
  lastFinished: cashflowMonthSchema.nullable(),
  current: cashflowMonthSchema,
  recent: z.array(cashflowMonthSchema),
  year: cashflowYearSchema,
});
export type CashflowPage = z.infer<typeof cashflowPageSchema>;

export const cashflowConfirmResponseSchema = z.object({
  month: z.string(),
  confirmedAt: z.string(),
  confirmedByName: z.string(),
});
export type CashflowConfirmResponse = z.infer<typeof cashflowConfirmResponseSchema>;
