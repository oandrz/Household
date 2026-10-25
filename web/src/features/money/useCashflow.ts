// Fetch orchestration for Money > Cashflow: the page read plus the two
// month-confirmation writes. Every write that changes what the page adds up
// (a transaction, a bill, an account, the primary currency) invalidates
// CASHFLOW_ROOT_KEY from its own hook, because the page is built from all of
// them and a stale one would disagree with the screen the user just edited.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch, fetchAndParse } from "../../api/client";
import {
  cashflowConfirmResponseSchema,
  cashflowPageSchema,
  type CashflowConfirmResponse,
  type CashflowPage,
} from "./cashflowSchemas";

// The prefix every cashflow query shares. Invalidating it refreshes every
// year the user has opened, not only the one on screen.
export const cashflowRootKey = ["cashflow"] as const;

// year undefined means "the current year", which the server resolves in the
// household's own time zone.
export function cashflowQueryKey(year: number | undefined) {
  return ["cashflow", year] as const;
}

export function useCashflow(year: number | undefined) {
  return useQuery({
    queryKey: cashflowQueryKey(year),
    queryFn: async (): Promise<CashflowPage> => {
      const suffix = year === undefined ? "" : `?year=${encodeURIComponent(String(year))}`;
      return fetchAndParse(cashflowPageSchema, `/api/v1/cashflow${suffix}`);
    },
  });
}

// Returned, not fired-and-forgotten: the mutation counts as settled only
// once the page has refetched, so a button disabled by isPending never
// re-enables over stale data.
function refreshCashflow(queryClient: ReturnType<typeof useQueryClient>) {
  return queryClient.invalidateQueries({ queryKey: cashflowRootKey });
}

// month is "YYYY-MM".
export function useConfirmCashflowMonth() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (month: string): Promise<CashflowConfirmResponse> => {
      return fetchAndParse(
        cashflowConfirmResponseSchema,
        `/api/v1/cashflow/confirmations/${encodeURIComponent(month)}`,
        { method: "PUT" },
      );
    },
    onSuccess: () => refreshCashflow(queryClient),
  });
}

// DELETE answers 204 with no body, so there is nothing to parse.
export function useUnconfirmCashflowMonth() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (month: string): Promise<void> => {
      await apiFetch<unknown>(`/api/v1/cashflow/confirmations/${encodeURIComponent(month)}`, {
        method: "DELETE",
      });
    },
    onSuccess: () => refreshCashflow(queryClient),
  });
}
