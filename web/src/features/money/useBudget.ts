// Fetch orchestration for the Budget screen, kept in a hook from day one
// (spec decision 11) rather than inline in BudgetPage.tsx -- the same shape
// warning TransactionsPage.tsx's 500 lines left behind. Built on TanStack
// Query, the same house pattern useTransactions.ts/useAccounts.ts use --
// useTransactions.ts's own `useTransactions(filters)` is already a
// month-parameterized, single-consumer `useQuery` (its `filters.month`
// plays the exact role this hook's `month` argument does), so there was no
// real precedent for a hand-rolled `useState`/`useEffect` pair here. Using
// `useQuery` instead of one is what gives this hook two things a manual
// fetch/reload loop had to hand-wave: `["budget", month]` as the cache key
// makes it structurally impossible for a month switch to show one month's
// figures under another month's label (the very race a bare `data` slot
// shared across months invites), and mutation `onSuccess` can invalidate
// `useTransactions.ts`'s `categoriesQueryKey()` alongside this hook's own
// key, so a category written from Budget doesn't leave the ledger's
// category dropdown stale the way `CurrencyPanel.tsx:49`'s defect class
// warns about.
import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { categoriesQueryKey } from "./useTransactions";
import {
  budgetMonthResponseSchema,
  putBudgetResponseSchema,
  type BudgetMonthResponse,
} from "./budgetSchemas";

export type SaveBudgetBody = {
  expectedIncomeMinor: number | null;
  lines: { categoryId: string; capMinor: number }[];
};

export function budgetQueryKey(month: string) {
  return ["budget", month] as const;
}

async function fetchBudgetMonth(month: string): Promise<BudgetMonthResponse> {
  const body = await apiFetch<unknown>(`/api/v1/budgets/${encodeURIComponent(month)}`);
  return budgetMonthResponseSchema.parse(body);
}

// Every category write below (create/rename/archive/restore) invalidates
// both this month's budget key and the ledger's `categoriesQueryKey()` --
// not just its own screen's cache. A category created, renamed or archived
// from Budget has to show up correctly the next time the ledger's own
// category dropdown reads it, not only in the cap rows here. Returned (not
// fired-and-forgotten) from each mutation's `onSuccess`, the same
// useAccounts.ts/useTransactions.ts convention: TanStack Query awaits the
// callback's return value before treating the mutation as settled, and
// settled is what a caller's `await save(...)`/`await createCategory(...)`
// actually waits on.
function invalidateAfterCategoryWrite(queryClient: QueryClient, month: string) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: budgetQueryKey(month) }),
    queryClient.invalidateQueries({ queryKey: categoriesQueryKey() }),
  ]);
}

// useBudget(month) loads the whole Budget screen for one month and exposes
// every write the modal needs. Every write below invalidates the month's
// query on success rather than patching local state from its own response
// -- the month response is the one place every derived figure (spent,
// remaining, percent used, over count...) is computed together, so
// re-deriving them from a create/rename/archive response here would
// duplicate BudgetService.Month's own math on the client.
export function useBudget(month: string) {
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: budgetQueryKey(month),
    queryFn: () => fetchBudgetMonth(month),
  });

  const saveMutation = useMutation({
    mutationFn: async (body: SaveBudgetBody) => {
      const raw = await apiFetch<unknown>(`/api/v1/budgets/${encodeURIComponent(month)}`, {
        method: "PUT",
        body: JSON.stringify(body),
      });
      // Parsed even though `save` reloads the month rather than reading
      // this value itself: a wire drift on PUT's own response shape would
      // otherwise never be caught by anything, since nothing else in this
      // hook touches putBudgetResponseSchema.
      return putBudgetResponseSchema.parse(raw);
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: budgetQueryKey(month) }),
  });

  const createCategoryMutation = useMutation({
    mutationFn: async (name: string) => {
      await apiFetch<unknown>("/api/v1/categories", {
        method: "POST",
        body: JSON.stringify({ name }),
      });
    },
    onSuccess: () => invalidateAfterCategoryWrite(queryClient, month),
  });

  const renameCategoryMutation = useMutation({
    mutationFn: async ({ id, name }: { id: string; name: string }) => {
      await apiFetch<unknown>(`/api/v1/categories/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify({ name }),
      });
    },
    onSuccess: () => invalidateAfterCategoryWrite(queryClient, month),
  });

  const archiveCategoryMutation = useMutation({
    mutationFn: async (id: string) => {
      await apiFetch<unknown>(`/api/v1/categories/${encodeURIComponent(id)}/archive`, {
        method: "POST",
      });
    },
    onSuccess: () => invalidateAfterCategoryWrite(queryClient, month),
  });

  const restoreCategoryMutation = useMutation({
    mutationFn: async (id: string) => {
      await apiFetch<unknown>(`/api/v1/categories/${encodeURIComponent(id)}/restore`, {
        method: "POST",
      });
    },
    onSuccess: () => invalidateAfterCategoryWrite(queryClient, month),
  });

  return {
    data: query.data,
    // v5's `isLoading` is `isPending && isFetching` -- true only while the
    // first fetch for this `queryKey` is in flight and no cached value
    // exists yet, the same "nothing to show yet" moment the old manual
    // `loading` state tracked. A background refetch after invalidation
    // does not flip this back to true (`isFetching` would, `isLoading`
    // deliberately doesn't), so the screen doesn't blank out on every save.
    loading: query.isLoading,
    error: query.error,
    reload: async () => {
      await query.refetch();
    },
    save: async (body: SaveBudgetBody) => {
      await saveMutation.mutateAsync(body);
    },
    createCategory: async (name: string) => {
      await createCategoryMutation.mutateAsync(name);
    },
    renameCategory: async (id: string, name: string) => {
      await renameCategoryMutation.mutateAsync({ id, name });
    },
    archiveCategory: async (id: string) => {
      await archiveCategoryMutation.mutateAsync(id);
    },
    restoreCategory: async (id: string) => {
      await restoreCategoryMutation.mutateAsync(id);
    },
  };
}
