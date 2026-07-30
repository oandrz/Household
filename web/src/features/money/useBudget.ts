// Fetch orchestration for the Budget screen, kept in a hook from day one
// (spec decision 11) rather than inline in BudgetPage.tsx -- the same shape
// warning TransactionsPage.tsx's 500 lines left behind. Deliberately not
// built on TanStack Query like useTransactions.ts/useAccounts.ts: those
// hooks are read caches shared across the app (categories, accounts) that
// several screens need kept in sync; a month's budget is read and written by
// exactly one screen, so a local load/reload pair is the smaller shape for
// the same behaviour -- see BudgetPage.tsx (Task 12) for the only caller.
import { useCallback, useEffect, useState } from "react";
import { apiFetch } from "../../api/client";
import { budgetMonthResponseSchema, type BudgetMonthResponse } from "./budgetSchemas";

export type SaveBudgetBody = {
  expectedIncomeMinor: number | null;
  lines: { categoryId: string; capMinor: number }[];
};

async function fetchBudgetMonth(month: string): Promise<BudgetMonthResponse> {
  const body = await apiFetch<unknown>(`/api/v1/budgets/${encodeURIComponent(month)}`);
  return budgetMonthResponseSchema.parse(body);
}

// useBudget(month) loads the whole Budget screen for one month and exposes
// every write the modal needs. Every write below reloads the month on
// success rather than patching local state from its own response -- the
// month response is the one place every derived figure (spent, remaining,
// percent used, over count...) is computed together, so re-deriving them
// from a create/rename/archive response here would duplicate
// BudgetService.Month's own math on the client.
export function useBudget(month: string) {
  const [data, setData] = useState<BudgetMonthResponse | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setData(await fetchBudgetMonth(month));
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  }, [month]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const save = useCallback(
    async (body: SaveBudgetBody) => {
      await apiFetch<unknown>(`/api/v1/budgets/${encodeURIComponent(month)}`, {
        method: "PUT",
        body: JSON.stringify(body),
      });
      await reload();
    },
    [month, reload],
  );

  // Category writes below hit /api/v1/categories, not /api/v1/budgets --
  // they reload the budget month (so the modal's cap rows reflect the new
  // name/archived state immediately) but do not touch
  // useTransactions.ts's `categoriesQueryKey()` react-query cache. The
  // ledger's category dropdown (useCategories()) is a separate cache with no
  // subscriber here to invalidate it; Task 14, which builds the modal that
  // calls these, needs to either invalidate that query key too or accept
  // that a category created from Budget shows up in the ledger only after
  // its next natural refetch.
  const createCategory = useCallback(
    async (name: string) => {
      await apiFetch<unknown>("/api/v1/categories", {
        method: "POST",
        body: JSON.stringify({ name }),
      });
      await reload();
    },
    [reload],
  );

  const renameCategory = useCallback(
    async (id: string, name: string) => {
      await apiFetch<unknown>(`/api/v1/categories/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify({ name }),
      });
      await reload();
    },
    [reload],
  );

  const archiveCategory = useCallback(
    async (id: string) => {
      await apiFetch<unknown>(`/api/v1/categories/${encodeURIComponent(id)}/archive`, {
        method: "POST",
      });
      await reload();
    },
    [reload],
  );

  const restoreCategory = useCallback(
    async (id: string) => {
      await apiFetch<unknown>(`/api/v1/categories/${encodeURIComponent(id)}/restore`, {
        method: "POST",
      });
      await reload();
    },
    [reload],
  );

  return { data, loading, error, reload, save, createCategory, renameCategory, archiveCategory, restoreCategory };
}
