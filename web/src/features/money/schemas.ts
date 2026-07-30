// Zod schemas for GET /api/v1/accounts. These mirror the DTOs declared in
// api/internal/adapter/http/account_handlers.go (accountDTO, summaryDTO and
// their nested types) rather than being inferred from the design -- the
// backend's own comments on those structs are what a limited member's
// response omits and why.
import { z } from "zod";

export const accountTypeSchema = z.enum([
  "cash",
  "investment",
  "property",
  "loan",
  "credit_card",
]);
export type AccountType = z.infer<typeof accountTypeSchema>;

const moneySchema = z.object({
  amountMinor: z.number(),
  currency: z.string(),
});

// balance, openingBalance and balanceAsOf are optional because a limited
// member's response omits them entirely. Modelling them as `number | null`
// instead would let a component render a zero balance for someone who is not
// allowed to see one.
//
// balance and openingBalance are two different figures and stopped being the
// same number the day Transactions shipped. balance is what the account holds
// now -- the opening balance plus every transaction dated after balanceAsOf,
// summed in SQL (queries/account.sql). openingBalance is the figure someone
// asserted was true on balanceAsOf, and it is the only one of the two that
// may ever be written back as openingBalanceMinor. Anything editing the
// stored opening balance reads openingBalance; anything displaying what the
// account is worth today reads balance.
export const accountSchema = z.object({
  id: z.string(),
  nickname: z.string(),
  type: accountTypeSchema,
  ownerMembershipId: z.string().nullable(),
  ownerName: z.string().nullable(),
  balance: moneySchema.optional(),
  openingBalance: moneySchema.optional(),
  balanceAsOf: z.string().optional(),
  countTowardNetWorth: z.boolean(),
  visibleToLimitedMembers: z.boolean(),
  archivedAt: z.string().nullable(),
});
export type Account = z.infer<typeof accountSchema>;

const breakdownEntrySchema = z.object({
  type: accountTypeSchema,
  totalMinor: z.number(),
});

const excludedAccountSchema = z.object({
  accountId: z.string(),
  currency: z.string(),
});

// summaryDTO's own comment is why this is a discriminated union rather than
// three `.optional()` fields on one flat object: netWorthMinor, assetsMinor
// and liabilitiesMinor exist on the wire only when computable is true, and an
// incomputable summary must never be made to yield a figure by a caller that
// forgot to check first. Keying the union on `computable` (rather than
// modelling that as a plain `boolean` alongside independently-optional
// fields) means the compiler narrows the other three fields into existence
// the moment a branch checks `summary.computable` -- there is no `!`
// assertion for a component to reach for instead, because there is nothing
// left to assert past the branch.
const computableSummarySchema = z.object({
  currency: z.string(),
  computable: z.literal(true),
  netWorthMinor: z.number(),
  assetsMinor: z.number(),
  liabilitiesMinor: z.number(),
  breakdown: z.array(breakdownEntrySchema),
  excludedNoRate: z.array(excludedAccountSchema),
  excludedByChoice: z.number(),
});

const incomputableSummarySchema = z.object({
  currency: z.string(),
  computable: z.literal(false),
  breakdown: z.array(breakdownEntrySchema),
  excludedNoRate: z.array(excludedAccountSchema),
  excludedByChoice: z.number(),
});

export const summarySchema = z.discriminatedUnion("computable", [
  computableSummarySchema,
  incomputableSummarySchema,
]);
export type Summary = z.infer<typeof summarySchema>;

// summary is absent entirely for a limited member -- the server omits it
// rather than sending a zeroed one, and that absence is the one signal the
// frontend has for "this caller cannot see amounts." The page must never
// synthesise a summary to fill this gap.
export const accountsResponseSchema = z.object({
  accounts: z.array(accountSchema),
  summary: summarySchema.optional(),
});
export type AccountsResponse = z.infer<typeof accountsResponseSchema>;
