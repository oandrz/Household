// Every sentence the period report can say, in one module so a test can assert
// the wording and a screen never spells a rule out twice -- the goalCopy.ts
// pattern.
import type { BlankReason } from "./holdingSchemas";

export const HOLDING_REPORT_COPY = {
  title: "How each investment did",
  empty: "Nothing is held yet. Add a holding and record what it cost, and this page fills in.",
  // The chart draws nothing when no period has a figure. Saying so beats an
  // empty axis, which reads as a bug.
  chartEmpty: "No period can be valued yet. Record a price and this chart fills in.",
  unrealised: "Unrealised",
  realised: "Realised",
  income: "Income",
  fees: "Fees",
  total: "Total",
  // Fees are stored positive and already taken out of the total. Saying it
  // here stops a reader adding them a second time in their head.
  feesNote: "Fees are already taken out of the total.",
  // Milestone 2 still does not touch net worth; the portfolio page says so and
  // this page must not imply otherwise.
  notInNetWorth: "These figures are not in your net worth yet.",
  periodKinds: {
    quarter: "Quarter",
    half: "Half-year",
    year: "Year",
  },
} as const;

// The reason a period could not be valued. Each one names the price that is
// missing and which end of the period it belongs to, because those are two
// different pieces of work for the owner: one is backfilling history, the
// other is recording today's price.
const REASONS: Record<Exclude<BlankReason, "">, string> = {
  no_closing_price: "No price recorded in this period, so what it was worth at the end is unknown.",
  no_opening_price: "No price recorded in the period before this one, so what it was worth at the start is unknown.",
};

export function blankReasonCopy(reason: BlankReason): string {
  if (reason === "") return "";
  return REASONS[reason];
}

export function periodHeading(period: { label: string; current: boolean }): string {
  return period.current ? `${period.label} to date` : period.label;
}

// priceAgeLabel says how old the price that measured a period is. The age
// rather than only the date, because "2026-06-30" does not tell an owner at a
// glance that they have not typed a price in three months -- and valuations
// going quietly stale is this feature's top product risk.
//
// today is passed in rather than read here so that a test can pin it, the same
// reason every service takes its clock as a parameter.
export function priceAgeLabel(asOf: string | null, today: string): string {
  if (asOf === null) return "";

  const priced = Date.parse(`${asOf}T00:00:00Z`);
  const now = Date.parse(`${today}T00:00:00Z`);
  // A date this cannot parse is shown as it arrived. Arithmetic on NaN would
  // put "priced NaN days ago" on a money screen.
  if (Number.isNaN(priced) || Number.isNaN(now)) return `priced ${asOf}`;

  const days = Math.round((now - priced) / 86_400_000);
  if (days <= 0) return "priced today";
  if (days === 1) return "priced yesterday";
  return `priced ${days} days ago`;
}
