// Zod mirrors of the DTOs in api/internal/adapter/http/transaction_handlers.go
// (transactionDTO, monthSummaryDTO) and category_handlers.go. These follow the
// backend's own structs rather than the design, because the backend's comments
// are what say which fields can be absent and why.
import { z } from "zod";

export const transactionKindSchema = z.enum(["expense", "income", "transfer"]);
export type TransactionKind = z.infer<typeof transactionKindSchema>;

const moneySchema = z.object({
  amountMinor: z.number(),
  currency: z.string(),
});

// The two before-opening flags are nullable rather than boolean: null means
// there is no account on that side at all, which is different from "there is
// one and this transaction does not predate it". A transfer can predate one
// side and not the other, which is why there are two.
export const transactionSchema = z.object({
  id: z.string(),
  kind: transactionKindSchema,
  occurredOn: z.string(),
  description: z.string(),
  categoryId: z.string().nullable(),
  categoryName: z.string().nullable(),
  paidByMembershipId: z.string().nullable(),
  paidByName: z.string().nullable(),
  fromAccountId: z.string().nullable(),
  fromAccountName: z.string().nullable(),
  toAccountId: z.string().nullable(),
  toAccountName: z.string().nullable(),
  amount: moneySchema,
  receivedAmount: moneySchema.nullable(),
  beforeFromAccountOpeningBalance: z.boolean().nullable(),
  beforeToAccountOpeningBalance: z.boolean().nullable(),
});
export type Transaction = z.infer<typeof transactionSchema>;

const excludedTransactionSchema = z.object({
  transactionId: z.string(),
  currency: z.string(),
});

export const monthSummarySchema = z.object({
  currency: z.string(),
  month: z.string(),
  count: z.number(),
  spentMinor: z.number(),
  excludedNoRate: z.array(excludedTransactionSchema),
});
export type MonthSummary = z.infer<typeof monthSummarySchema>;

export const transactionsResponseSchema = z.object({
  transactions: z.array(transactionSchema),
  // null on the last page. The "Load older transactions" link keys off this
  // and not off a row count, which would be wrong on a page that happens to be
  // exactly full.
  nextCursor: z.string().nullable(),
  summary: monthSummarySchema,
});
export type TransactionsResponse = z.infer<typeof transactionsResponseSchema>;

export const categorySchema = z.object({
  id: z.string(),
  name: z.string(),
  kind: z.enum(["expense", "income"]),
});
export type Category = z.infer<typeof categorySchema>;

export const categoriesResponseSchema = z.object({
  categories: z.array(categorySchema),
});
export type CategoriesResponse = z.infer<typeof categoriesResponseSchema>;
