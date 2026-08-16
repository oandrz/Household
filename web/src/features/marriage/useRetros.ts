// Fetch orchestration for the Retros history screen -- the same house
// pattern useBudget.ts/useGoals.ts use (a hook from day one, spec decision
// 11's own reasoning), not inline in the page. This file owns the list
// screen (GET /retros) and the one write that has no month of its own yet:
// POST /retros, which the server -- not the caller -- picks the month for
// (handleStartRetro's own comment: a client-supplied month would let a
// stale tab file a retro against a month the "Start retro" button never
// actually offered it). Every other write is month-scoped and lives in
// useRetro.ts instead, which is why this file's own query key
// (retroListQueryKey) has no parameter, unlike useBudget.ts's
// budgetQueryKey(month).
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import {
  retroWriteResponseSchema,
  retrosResponseSchema,
  type Retro,
  type RetrosResponse,
} from "./retroSchemas";

export function retroListQueryKey() {
  return ["retros"] as const;
}

async function fetchRetros(): Promise<RetrosResponse> {
  const body = await apiFetch<unknown>("/api/v1/retros");
  return retrosResponseSchema.parse(body);
}

// useRetros() loads the whole Retros history screen: every summary row, the
// twelve-month mood chart, the finished count and its "since" month, and
// which month (if any) is startable. RetroService.List computes every
// derived figure server-side; this hook only fetches and parses.
export function useRetros() {
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: retroListQueryKey(),
    queryFn: fetchRetros,
  });

  // POST /retros reads no request body -- the server picks the month (see
  // this file's header comment). Returns the created retro (its month, in
  // particular) so a caller can navigate straight to the new detail screen
  // without a second read -- the same reason useBudget.ts's createCategory
  // and useGoals.ts's createGoal return rather than discard. Awaited before
  // the mutation resolves, the same useBudget.ts/useGoals.ts convention
  // that CurrencyPanel and NotificationsPanel are on the follow-up list for
  // skipping.
  const startRetroMutation = useMutation({
    mutationFn: async (): Promise<Retro> => {
      const raw = await apiFetch<unknown>("/api/v1/retros", { method: "POST" });
      return retroWriteResponseSchema.parse(raw).retro;
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: retroListQueryKey() }),
  });

  return {
    data: query.data,
    // v5's `isLoading` is `isPending && isFetching` -- true only while the
    // first fetch for this queryKey is in flight and no cached value exists
    // yet. A background refetch after invalidation does not flip this back
    // to true, so the screen doesn't blank out on every write.
    loading: query.isLoading,
    error: query.error,
    reload: async () => {
      await query.refetch();
    },
    startRetro: (): Promise<Retro> => startRetroMutation.mutateAsync(),
  };
}
