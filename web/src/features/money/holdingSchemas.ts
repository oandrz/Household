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

export const holdingResponseSchema = z.object({ holding: holdingSchema });

// quantity is a string here for holdingSchema's reason, restated: it crosses
// the wire as the person typed it.
export const holdingEventSchema = z.object({
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
export type HoldingEvent = z.infer<typeof holdingEventSchema>;

export const holdingEventsResponseSchema = z.object({ events: z.array(holdingEventSchema) });

export const holdingValuationSchema = z.object({
  id: z.string(),
  unitPriceMinor: z.number(),
  currency: z.string(),
  primaryUnitPriceMinor: z.number().nullable(),
  primaryCurrency: z.string().nullable(),
  asOf: z.string(),
  note: z.string(),
});
export type HoldingValuation = z.infer<typeof holdingValuationSchema>;

export const holdingValuationsResponseSchema = z.object({
  valuations: z.array(holdingValuationSchema),
});
