import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { accountsQueryKey } from "./useAccounts";
import {
  categoriesResponseSchema,
  transactionSchema,
  transactionsResponseSchema,
  type Category,
  type Transaction,
  type TransactionsResponse,
} from "./transactionSchemas";

export type TransactionFilters = {
  kind?: string;
  accountId?: string;
  categoryId?: string;
  paidBy?: string;
  month?: string;
  cursor?: string;
};

export function transactionsQueryKey(filters: TransactionFilters) {
  return ["transactions", filters] as const;
}

export function categoriesQueryKey() {
  return ["categories"] as const;
}

// Exported for its own test: it must omit an unset filter entirely rather
// than stringifying `undefined` into the query. parseTransactionFilter on the
// server treats an empty account_id/category_id/paid_by as absent (skipped
// before validation), but a non-empty, non-UUID value -- which is exactly
// what `String(undefined)` produces -- fails isValidUUID and answers 422. For
// `kind`, which the server never validates, the literal string "undefined"
// would instead become a real filter matching nothing: a silently empty
// ledger that reads exactly like end-of-history.
export function toQueryString(filters: TransactionFilters): string {
  const params = new URLSearchParams();
  if (filters.kind) params.set("kind", filters.kind);
  if (filters.accountId) params.set("account_id", filters.accountId);
  if (filters.categoryId) params.set("category_id", filters.categoryId);
  if (filters.paidBy) params.set("paid_by", filters.paidBy);
  // "" and undefined are different values here, deliberately. undefined is
  // "the page has not chosen a month yet", which sends no month at all and
  // lets parseTransactionFilter apply its default -- the current month, for
  // the list and the summary alike. "" is the user having deliberately asked
  // for every month, which must reach the server as the explicit `month=all`;
  // sent as an absent parameter it would be read as the default and silently
  // re-scope the ledger to this month, which is the widening quietly failing.
  if (filters.month) params.set("month", filters.month);
  else if (filters.month === "") params.set("month", "all");
  if (filters.cursor) params.set("cursor", filters.cursor);
  const query = params.toString();
  return query ? `?${query}` : "";
}

export function useTransactions(filters: TransactionFilters) {
  return useQuery({
    queryKey: transactionsQueryKey(filters),
    queryFn: async (): Promise<TransactionsResponse> => {
      const body = await apiFetch<unknown>(`/api/v1/transactions${toQueryString(filters)}`);
      return transactionsResponseSchema.parse(body);
    },
  });
}

export function useCategories() {
  return useQuery({
    queryKey: categoriesQueryKey(),
    queryFn: async (): Promise<Category[]> => {
      const body = await apiFetch<unknown>("/api/v1/categories");
      return categoriesResponseSchema.parse(body).categories;
    },
  });
}

// Every transaction mutation invalidates the accounts queries too: a
// transaction changes an account's balance and the net worth built from it, so
// leaving those cached shows a ledger that disagrees with the Finances page
// one click away.
//
// Returned, not fired-and-forgotten: TanStack Query awaits a mutation's
// onSuccess return value before treating it as settled, and settled is what
// isPending -- every submit button's disabled condition -- reflects. Skipping
// the return re-enables the button while the list is still serving stale data.
// That is the defect web/src/features/settings/CurrencyPanel.tsx:49 documents.
function invalidateLedger(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: ["transactions"] }),
    queryClient.invalidateQueries({ queryKey: accountsQueryKey(false) }),
    queryClient.invalidateQueries({ queryKey: accountsQueryKey(true) }),
  ]);
}

export function useCreateTransaction() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: unknown): Promise<Transaction> => {
      const raw = await apiFetch<unknown>("/api/v1/transactions", {
        method: "POST",
        body: JSON.stringify(body),
      });
      return transactionSchema.parse(raw);
    },
    onSuccess: () => invalidateLedger(queryClient),
  });
}

export function useUpdateTransaction() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, body }: { id: string; body: unknown }): Promise<Transaction> => {
      const raw = await apiFetch<unknown>(`/api/v1/transactions/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      });
      return transactionSchema.parse(raw);
    },
    onSuccess: () => invalidateLedger(queryClient),
  });
}

// DELETE answers 204 with no body, which apiFetch does not try to parse. Every
// other 2xx in this API carries JSON for exactly that reason.
export function useDeleteTransaction() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<void> => {
      await apiFetch<unknown>(`/api/v1/transactions/${encodeURIComponent(id)}`, { method: "DELETE" });
    },
    onSuccess: () => invalidateLedger(queryClient),
  });
}
