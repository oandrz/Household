// Fetch orchestration for the Goals screen, built on TanStack Query -- the
// same house pattern useAccounts.ts/useBudget.ts use, not a hand-rolled
// useState/useEffect pair (see useBudget.ts's own header comment for why
// that shape was rejected once already). Goals share useAccounts.ts's
// include_archived union shape (a live-and-archived list from one endpoint,
// toggled by a query parameter) rather than useBudget.ts's month-keying, so
// `goalsQueryKey` is modelled on `accountsQueryKey`, not `budgetQueryKey`.
import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import {
  goalContributionsResponseSchema,
  goalResponseSchema,
  goalsResponseSchema,
  type Goal,
  type GoalContributionsResponse,
  type GoalsResponse,
} from "./goalSchemas";

// CreateGoalBody mirrors createGoalRequest (goal_handlers.go). Every field is
// required and sent as typed -- targetMonth is `string | null` because the
// create form always has an explicit "No target date" choice (Task 12), not
// an unset one; there is no "leave unchanged" concept on a create.
export type CreateGoalBody = {
  name: string;
  targetMinor: number;
  currency: string;
  targetMonth: string | null;
  plannedMonthlyMinor: number;
  startingBalanceMinor: number;
};

// UpdateGoalBody mirrors updateGoalRequest. Every field but
// clearTargetMonth is optional so an absent key round-trips as "unchanged"
// (usecase.GoalUpdate's own convention, per updateGoalRequest's comment) --
// JSON.stringify drops an `undefined` value's key entirely, which is what
// makes a plain TypeScript `?:` field the right mirror of a Go pointer field
// read this way, unlike the `| null` fields elsewhere in this file that mean
// "always present, sometimes empty."
export type UpdateGoalBody = {
  name?: string;
  targetMinor?: number;
  currency?: string;
  targetMonth?: string | null;
  clearTargetMonth?: boolean;
  plannedMonthlyMinor?: number;
};

// AddContributionBody mirrors addContributionRequest. currency is optional
// for the reason its Go field is a pointer at all -- addContributionRequest's
// own comment: it exists only to be checked against the goal's stored
// currency, never carried into the write itself.
export type AddContributionBody = {
  amountMinor: number;
  occurredOn: string;
  note: string;
  currency?: string;
};

export function goalsQueryKey(includeArchived: boolean) {
  return ["goals", { includeArchived }] as const;
}

export function goalContributionsQueryKey(goalId: string) {
  return ["goal-contributions", goalId] as const;
}

async function fetchGoals(includeArchived: boolean): Promise<GoalsResponse> {
  const suffix = includeArchived ? "?include_archived=true" : "";
  const body = await apiFetch<unknown>(`/api/v1/goals${suffix}`);
  return goalsResponseSchema.parse(body);
}

async function fetchGoalContributions(goalId: string): Promise<GoalContributionsResponse> {
  const body = await apiFetch<unknown>(`/api/v1/goals/${encodeURIComponent(goalId)}/contributions`);
  return goalContributionsResponseSchema.parse(body);
}

// Both goalsQueryKey variants, the same useAccounts.ts invalidateAccounts
// shape and for the same reason: a write performed while the screen shows
// one variant (live only, or live-and-archived) must not leave the other
// stale for the next time "Show archived" is toggled.
function invalidateGoals(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: goalsQueryKey(false) }),
    queryClient.invalidateQueries({ queryKey: goalsQueryKey(true) }),
  ]);
}

// A contribution write invalidates the goals list (both variants -- it moves
// that goal's contributed/percent/status and the page summary's totals) and
// that one goal's own contributions key (the panel's own recent-contributions
// list) together, per the brief: "a contribution changes the card's
// progress, the summary totals and the list at once."
function invalidateAfterContributionWrite(queryClient: QueryClient, goalId: string) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: goalsQueryKey(false) }),
    queryClient.invalidateQueries({ queryKey: goalsQueryKey(true) }),
    queryClient.invalidateQueries({ queryKey: goalContributionsQueryKey(goalId) }),
  ]);
}

// useGoals(options) loads the whole Goals screen (or, with `enabled: false`,
// stays idle -- the useBudget.ts precedent for a caller, such as Overview,
// that cannot always issue this request) and exposes every write the screen
// and its modals need. Every write below invalidates and refetches on
// success rather than patching local state from its own response -- the
// goals list is the one place every derived figure (contributed, percent,
// status, required monthly, the page summary) is computed together, so
// re-deriving them from a write response here would duplicate
// GoalService.List's own math on the client, the same reasoning useBudget.ts
// gives for its own category writes.
export function useGoals(options: { includeArchived?: boolean; enabled?: boolean } = {}) {
  const includeArchived = options.includeArchived ?? false;
  const enabled = options.enabled ?? true;
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: goalsQueryKey(includeArchived),
    queryFn: () => fetchGoals(includeArchived),
    enabled,
  });

  const createGoalMutation = useMutation({
    // Parsed and returned (unlike update/archive/restore below, which stay
    // fire-and-refetch): a caller that just created a goal may need its real
    // id immediately (Task 12's modal opening the new card, or a contribution
    // flow chained onto it), the same reason useBudget.ts's createCategory
    // returns rather than discards.
    mutationFn: async (body: CreateGoalBody): Promise<Goal> => {
      const raw = await apiFetch<unknown>("/api/v1/goals", {
        method: "POST",
        body: JSON.stringify(body),
      });
      return goalResponseSchema.parse(raw).goal;
    },
    onSuccess: () => invalidateGoals(queryClient),
  });

  const updateGoalMutation = useMutation({
    mutationFn: async (vars: { id: string; body: UpdateGoalBody }) => {
      await apiFetch<unknown>(`/api/v1/goals/${encodeURIComponent(vars.id)}`, {
        method: "PATCH",
        body: JSON.stringify(vars.body),
      });
    },
    onSuccess: () => invalidateGoals(queryClient),
  });

  const archiveGoalMutation = useMutation({
    mutationFn: async (id: string) => {
      await apiFetch<unknown>(`/api/v1/goals/${encodeURIComponent(id)}/archive`, {
        method: "POST",
      });
    },
    onSuccess: () => invalidateGoals(queryClient),
  });

  const restoreGoalMutation = useMutation({
    mutationFn: async (id: string) => {
      await apiFetch<unknown>(`/api/v1/goals/${encodeURIComponent(id)}/restore`, {
        method: "POST",
      });
    },
    onSuccess: () => invalidateGoals(queryClient),
  });

  const addContributionMutation = useMutation({
    mutationFn: async (vars: { goalId: string; body: AddContributionBody }) => {
      await apiFetch<unknown>(
        `/api/v1/goals/${encodeURIComponent(vars.goalId)}/contributions`,
        { method: "POST", body: JSON.stringify(vars.body) },
      );
    },
    onSuccess: (_data, vars) => invalidateAfterContributionWrite(queryClient, vars.goalId),
  });

  // 204 with no body -- the one status apiFetch does not try to parse
  // (handleDeleteGoalContribution's own comment). Nothing here reads a
  // response for that reason: the mutationFn's only job is the DELETE call
  // itself.
  const deleteContributionMutation = useMutation({
    mutationFn: async (vars: { goalId: string; contributionId: string }) => {
      await apiFetch<unknown>(
        `/api/v1/goals/${encodeURIComponent(vars.goalId)}/contributions/${encodeURIComponent(vars.contributionId)}`,
        { method: "DELETE" },
      );
    },
    onSuccess: (_data, vars) => invalidateAfterContributionWrite(queryClient, vars.goalId),
  });

  return {
    data: query.data,
    // v5's `isLoading` is `isPending && isFetching` -- true only while the
    // first fetch for this queryKey is in flight and no cached value exists
    // yet. A background refetch after invalidation does not flip this back
    // to true, so the screen doesn't blank out on every write.
    loading: query.isLoading,
    error: query.error,
    createGoal: (body: CreateGoalBody): Promise<Goal> => createGoalMutation.mutateAsync(body),
    updateGoal: async (id: string, body: UpdateGoalBody): Promise<void> => {
      await updateGoalMutation.mutateAsync({ id, body });
    },
    archiveGoal: async (id: string): Promise<void> => {
      await archiveGoalMutation.mutateAsync(id);
    },
    restoreGoal: async (id: string): Promise<void> => {
      await restoreGoalMutation.mutateAsync(id);
    },
    addContribution: async (goalId: string, body: AddContributionBody): Promise<void> => {
      await addContributionMutation.mutateAsync({ goalId, body });
    },
    deleteContribution: async (goalId: string, contributionId: string): Promise<void> => {
      await deleteContributionMutation.mutateAsync({ goalId, contributionId });
    },
  };
}

// useGoalContributions backs the contributions panel (Task 13), fetched only
// while it is open -- the same `enabled`-gated shape useBudgetHistory.ts
// uses for the History modal, so a goal whose panel is never opened never
// spends a request on its own history.
export function useGoalContributions(goalId: string, enabled: boolean) {
  const query = useQuery({
    queryKey: goalContributionsQueryKey(goalId),
    queryFn: () => fetchGoalContributions(goalId),
    enabled,
  });

  return {
    data: query.data,
    loading: query.isLoading,
    error: query.error,
  };
}
