// Fetch orchestration for the History modal (Task 15), kept out of
// useBudget.ts on purpose: useBudget has a second caller (BudgetModal.tsx
// calls `useBudget(month)` itself, per that file's own header comment) that
// has no business carrying a history query along for the ride every time it
// mounts. This hook exists only for BudgetPage.tsx and the modal it opens.
//
// Built on the same TanStack Query house pattern useBudget.ts/useTransactions.ts
// use, gated by an `enabled` flag the caller controls -- BudgetPage passes
// `open` (whether the History modal is currently mounted), the same shape
// useBudget.ts's own `prevMonthQuery` gates on `query.data?.budget === null`
// (Task 13's comment: "no point spending a request... to find out"). A
// household that never opens History never spends a request finding out
// what's in it, and reopening it within the query's staleness window reads
// the cache instead of refetching.
import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { budgetHistoryResponseSchema, type BudgetHistoryResponse } from "./budgetSchemas";

// Matches api/internal/adapter/http/budget_handlers.go's own
// `defaultHistoryMonths` -- the History modal always asks for the same
// window the design's subtitle names ("last 6 months"), so this is not a
// prop a caller can vary today. Kept as a named constant rather than a
// magic `6` at the call site, and exported so the modal's own subtitle copy
// (`historyModalSubtitle`) reads it instead of a second hard-coded `6`.
export const HISTORY_MONTHS = 6;

export function budgetHistoryQueryKey(months: number) {
  return ["budget-history", months] as const;
}

async function fetchBudgetHistory(months: number): Promise<BudgetHistoryResponse> {
  const body = await apiFetch<unknown>(`/api/v1/budgets/history?months=${months}`);
  return budgetHistoryResponseSchema.parse(body);
}

export function useBudgetHistory(enabled: boolean, months: number = HISTORY_MONTHS) {
  const query = useQuery({
    queryKey: budgetHistoryQueryKey(months),
    queryFn: () => fetchBudgetHistory(months),
    enabled,
  });

  return {
    data: query.data,
    loading: query.isLoading,
    error: query.error,
  };
}
