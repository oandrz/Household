// Zod mirrors of the DTOs in api/internal/adapter/http/holding_handlers.go
// (holdingDTO, portfolioResponse, holdingResponse, holdingEventDTO,
// holdingValuationDTO). These follow the backend's own structs rather than a
// design doc, the same convention goalSchemas.ts and budgetSchemas.ts use --
// the backend's comments are what say which fields can be null and why.
import { z } from "zod";

// Mirrors domain.InstrumentKind. "other" is a real kind, not a fallback: a
// holding the household cannot express as a quantity at a unit price is
// recorded as one of these with a quantity of 1, so there is a single
// arithmetic path rather than a second one for lump sums.
export const instrumentKindSchema = z.enum(["stock", "gold", "other"]);
export type InstrumentKind = z.infer<typeof instrumentKindSchema>;

// Mirrors domain.HoldingEventKind. Income (a dividend) is deliberately not
// among them -- it changes neither what is held nor what it cost, so folding
// it in would corrupt the average cost.
export const holdingEventKindSchema = z.enum(["acquisition", "disposal"]);
export type HoldingEventKind = z.infer<typeof holdingEventKindSchema>;

// holdingSchema mirrors holdingDTO.
//
// `held` is a STRING and `heldNano` is its integer. The string is what this
// app renders; nothing here ever divides heldNano by 1e9 to produce it. That
// division is float64 arithmetic on a figure a money screen shows, and
// docs/LEARNING.md records what it already cost this codebase once
// (333333 * 0.3 === 99999.90000000001 in JavaScript, a budget pool floored one
// unit low). The server formats it in integers and sends both.
//
// `hasMarketValue` false means there is NO figure -- not a figure of zero. A
// holding nobody has priced is unknowable, not worthless, and the page shows
// the reason instead of a number. `valuedAt` is null in exactly that case.
//
// `primaryMarketValueMinor` is present only when the holding is NOT already in
// the household's own currency; null means "already in your currency", so the
// page shows one figure rather than two identical ones.
export const holdingSchema = z.object({
  id: z.string(),
  accountId: z.string(),
  accountName: z.string(),
  name: z.string(),
  instrument: instrumentKindSchema,
  unit: z.string(),
  currency: z.string(),
  archivedAt: z.string().nullable(),

  // heldNano is the exact integer, for a caller doing exact arithmetic -- but
  // JavaScript numbers lose precision above 2^53 (9.007e15), which a holding
  // of more than ~9 million units would exceed (1e16 nano). Nothing renders
  // from it today and nothing should: `held` above is the string the server
  // formatted, and it is exact at any size. Read this one only for comparison,
  // never for display.
  heldNano: z.number(),
  held: z.string(),
  costMinor: z.number(),
  realisedMinor: z.number(),

  marketValueMinor: z.number(),
  hasMarketValue: z.boolean(),
  valuedAt: z.string().nullable(),
  primaryMarketValueMinor: z.number().nullable(),
  primaryCurrency: z.string().nullable(),
});
export type Holding = z.infer<typeof holdingSchema>;

// notInNetWorth is a wire-level fact the page reads rather than hardcodes.
// Milestone 1 deliberately keeps holdings out of net worth and the twelve-month
// trend, and the page says so on its face; when that changes it changes on the
// server and the banner follows without a frontend edit.
export const portfolioResponseSchema = z.object({
  holdings: z.array(holdingSchema),
  notInNetWorth: z.boolean(),
});
export type PortfolioResponse = z.infer<typeof portfolioResponseSchema>;


// quantity is a string here for holdingSchema's reason, restated: it crosses
// the wire as the person typed it.
const holdingEventSchema = z.object({
  id: z.string(),
  kind: holdingEventKindSchema,
  quantityNano: z.number(),
  quantity: z.string(),
  amountMinor: z.number(),
  currency: z.string(),
  primaryAmountMinor: z.number().nullable(),
  primaryCurrency: z.string().nullable(),
  occurredOn: z.string(),
  note: z.string(),
});

export const holdingEventsResponseSchema = z.object({ events: z.array(holdingEventSchema) });

const holdingValuationSchema = z.object({
  id: z.string(),
  unitPriceMinor: z.number(),
  currency: z.string(),
  primaryUnitPriceMinor: z.number().nullable(),
  primaryCurrency: z.string().nullable(),
  asOf: z.string(),
  note: z.string(),
});

export const holdingValuationsResponseSchema = z.object({
  valuations: z.array(holdingValuationSchema),
});

// --- income, and the period report -------------------------------------------

// Mirrors domain.IncomeKind. Both are stored POSITIVE and the report subtracts
// the fees -- a negative amount is refused everywhere in this product, and one
// exception is how a rule stops being a rule.
export const incomeKindSchema = z.enum(["income", "fee"]);
export type IncomeKind = z.infer<typeof incomeKindSchema>;

const holdingIncomeSchema = z.object({
  id: z.string(),
  kind: incomeKindSchema,
  amountMinor: z.number(),
  currency: z.string(),
  primaryAmountMinor: z.number().nullable(),
  primaryCurrency: z.string().nullable(),
  receivedOn: z.string(),
  note: z.string(),
});

export const holdingIncomeResponseSchema = z.object({ income: z.array(holdingIncomeSchema) });

// Mirrors domain.PeriodKind. "half" is a calendar half-year (H1, H2), not a
// rolling six months.
export const periodKindSchema = z.enum(["quarter", "half", "year"]);
export type PeriodKind = z.infer<typeof periodKindSchema>;

// Mirrors domain.BlankReason, with "" for a period that computed fine. The
// empty string is a real value on the wire rather than an absent field, so a
// screen switches on it instead of testing for undefined.
export const blankReasonSchema = z.enum(["", "no_opening_price", "no_closing_price"]);
export type BlankReason = z.infer<typeof blankReasonSchema>;

// One figure in both currencies. The primary one is the household's own and is
// what answers "did this make us richer"; the native one sits beside it so the
// owner can still tell whether the PICK was good and the exchange rate was the
// problem.
const returnComponentSchema = z.object({
  nativeMinor: z.number(),
  primaryMinor: z.number(),
});

// periodReturnSchema mirrors periodReturnDTO.
//
// `unrealised` and `total` are NULL rather than zero when they cannot be
// known, and `reason` says which price was missing. A screen must render the
// reason, never a zero: a quarter nobody priced is unknowable, not flat --
// the same rule the net worth card follows. `realised`, `income` and `fees`
// are always present, because no price is involved in computing them.
//
// The price dates are dates, not booleans, so the screen can say how OLD the
// figure is. Null means no price was consulted at that end, which is what
// holding nothing there means -- not a price that is missing.
export const periodReturnSchema = z.object({
  unrealised: returnComponentSchema.nullable(),
  realised: returnComponentSchema,
  income: returnComponentSchema,
  fees: returnComponentSchema,
  total: returnComponentSchema.nullable(),
  reason: blankReasonSchema,
  openingPriceAsOf: z.string().nullable(),
  closingPriceAsOf: z.string().nullable(),
});
export type PeriodReturn = z.infer<typeof periodReturnSchema>;

export const reportPeriodSchema = z.object({
  kind: periodKindSchema,
  year: z.number(),
  index: z.number(),
  label: z.string(),
  start: z.string(),
  end: z.string(),
  // The period the household is still living in, which the screen labels "to
  // date" rather than presenting as a closed result.
  current: z.boolean(),
});
export type ReportPeriod = z.infer<typeof reportPeriodSchema>;

// `returns` is index-aligned with the response's `periods`. The chart reads
// the two together by POSITION rather than matching labels, which is what
// stops a holding with a gap in its history shifting its own bars.
export const reportHoldingSchema = z.object({
  id: z.string(),
  name: z.string(),
  accountName: z.string(),
  instrument: instrumentKindSchema,
  unit: z.string(),
  currency: z.string(),
  archived: z.boolean(),
  returns: z.array(periodReturnSchema),
});
export type ReportHolding = z.infer<typeof reportHoldingSchema>;

export const portfolioReportSchema = z.object({
  kind: periodKindSchema,
  primaryCurrency: z.string(),
  periods: z.array(reportPeriodSchema),
  holdings: z.array(reportHoldingSchema),
});
export type PortfolioReport = z.infer<typeof portfolioReportSchema>;
