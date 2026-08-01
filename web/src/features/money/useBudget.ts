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
import { goalsQueryKey } from "./useGoals";
import {
  budgetMonthResponseSchema,
  categoryResponseSchema,
  putBudgetResponseSchema,
  type BudgetMonthResponse,
  type Category,
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

// Shifts a "YYYY-MM" string back one whole month, built the same way
// BudgetPage.tsx's own shiftMonth is -- through Date's (year, monthIndex, 1)
// constructor, so day-31-doesn't-exist-next-month is never an edge this has
// to handle. Kept private to this file rather than importing BudgetPage.tsx's
// copy: that function lives in the component, which this hook must not
// depend on.
function previousMonth(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  const shifted = new Date(year, monthNum - 2, 1);
  return `${shifted.getFullYear()}-${String(shifted.getMonth() + 1).padStart(2, "0")}`;
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
// `enabled` exists for Overview, which renders for every member but may only
// read a budget for an owner (GET /budgets/{month} is requireCapability(money)
// AND requireOwner). A caller cannot skip the hook -- that breaks the rules of
// hooks -- and passing a fake month would both fire a doomed request and cache
// a failure under a key nobody meant to write. Defaults to true, so BudgetPage
// is unaffected.
export function useBudget(month: string, options: { enabled?: boolean } = {}) {
  const queryClient = useQueryClient();
  const enabled = options.enabled ?? true;

  const query = useQuery({
    queryKey: budgetQueryKey(month),
    queryFn: () => fetchBudgetMonth(month),
    enabled,
  });

  // Backs the empty state's "Import last month" card (Task 13), which only
  // renders when the previous month actually has a budget to copy. Keyed
  // through the same `budgetQueryKey` this hook already uses for `month`
  // itself -- Task 15's month picker will land on this exact key when the
  // household clicks back a month, so this fetch's cache entry is already
  // warm by the time that happens, not a second query keyed differently
  // for the same data.
  //
  // Gated on the current month being unbudgeted (`enabled` below) rather
  // than firing unconditionally: a month that already has its own budget
  // never shows the Import card, so there is nothing for this to answer
  // yet -- no point spending a request every month switch to find out.
  const prevMonth = previousMonth(month);
  const prevMonthQuery = useQuery({
    queryKey: budgetQueryKey(prevMonth),
    queryFn: () => fetchBudgetMonth(prevMonth),
    enabled: enabled && query.data?.budget === null,
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
    // Parsed and returned (unlike rename/archive/restore below, which stay
    // fire-and-refetch) because BudgetModal.tsx's queued creates need the
    // real id the server assigned *before* it can build the PUT's line set
    // -- a category that doesn't exist yet has no id to put a cap against,
    // and the alternative (invalidate, then re-GET the list and match the
    // new row back by name) would insert an extra request between every
    // queued create and the final PUT, which is exactly the call sequence
    // the save-order test pins.
    mutationFn: async (name: string): Promise<Category> => {
      const raw = await apiFetch<unknown>("/api/v1/categories", {
        method: "POST",
        body: JSON.stringify({ name }),
      });
      return categoryResponseSchema.parse(raw).category;
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

  // Task 15: moves a closed month's unspent budget into a goal, as one
  // contribution -- the manual half of the design's "Roll unspent into
  // savings" toggle (BudgetService.RollOver's own doc comment: nothing here
  // runs on a clock). The response carries the written contribution, but
  // this discards it and refetches instead, the same
  // rename/archive/restoreCategory convention above: the month's own
  // rolledOverAt/rolloverGoalId stamp and the target goal's own
  // contributedMinor/percent/status both live in GETs this hook and
  // useGoals.ts already own, not in this response.
  const rollOverMutation = useMutation({
    mutationFn: async (goalId: string) => {
      await apiFetch<unknown>(`/api/v1/budgets/${encodeURIComponent(month)}/rollover`, {
        method: "POST",
        body: JSON.stringify({ goalId }),
      });
    },
    // A successful move changes two screens at once: this month's own stamp
    // (so BudgetRolloverCard.tsx swaps from the offer to the destination
    // sentence) and the target goal's own figures on the Goals screen --
    // both goalsQueryKey variants, the same useGoals.ts invalidateGoals
    // shape, so the write is visible whether or not "Show archived" happens
    // to be on there.
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: budgetQueryKey(month) }),
        queryClient.invalidateQueries({ queryKey: goalsQueryKey(false) }),
        queryClient.invalidateQueries({ queryKey: goalsQueryKey(true) }),
      ]),
    // A refusal can still mean the server's own truth moved out from under
    // this tab: ROLLOVER_ALREADY_DONE (409) is exactly "another tab already
    // rolled this month over," which this tab's own cached month (still
    // showing no stamp, still offering the button) does not yet know.
    // Invalidating here is what lets BudgetRolloverCard.tsx swap to the real
    // destination sentence once this resolves, instead of leaving a stale,
    // still-clickable button sitting on top of a month that is already done
    // -- the caller's own catch block is what shows the refusal's message
    // inline in the meantime.
    onError: () => queryClient.invalidateQueries({ queryKey: budgetQueryKey(month) }),
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
    // The month string BudgetPage.tsx's "Import last month" card needs for
    // its own label ("Copy June's caps") -- computed once here rather than
    // twice, since this hook already has to know it to build the query key
    // above.
    prevMonth,
    // `undefined` while the query hasn't resolved (or was never enabled --
    // the current month already has a budget, so nothing to answer);
    // `true`/`false` once it has. BudgetPage.tsx only renders the Import
    // card once this is exactly `true`, never on the `undefined`
    // "don't know yet" state, so the card can't flash in and then vanish.
    prevMonthHasBudget: prevMonthQuery.data ? prevMonthQuery.data.budget !== null : undefined,
    // The previous month's actual saved budget, for the Import card's
    // prefill -- distinct from `prevMonthHasBudget` (a plain boolean the
    // card's own visibility check reads) because building that prefill
    // needs the real lines and income, not just whether they exist.
    prevMonthBudget: prevMonthQuery.data?.budget ?? null,
    reload: async () => {
      await query.refetch();
    },
    save: async (body: SaveBudgetBody) => {
      await saveMutation.mutateAsync(body);
    },
    createCategory: async (name: string): Promise<Category> => {
      return createCategoryMutation.mutateAsync(name);
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
    rollOver: async (goalId: string) => {
      await rollOverMutation.mutateAsync(goalId);
    },
  };
}
