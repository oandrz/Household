import { createElement } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { budgetHistoryResponseSchema, categoryResponseSchema } from "./budgetSchemas";
import { useBudget } from "./useBudget";
import { useGoals } from "./useGoals";

const julyResponse = {
  currency: "SGD",
  month: "2026-07",
  budget: { expectedIncomeMinor: 500000, lines: [{ categoryId: "cat-1", capMinor: 20000 }] },
  categories: [
    {
      categoryId: "cat-1",
      name: "Groceries",
      archived: false,
      capMinor: 20000,
      spentMinor: 15000,
      over: false,
    },
  ],
  budgetedMinor: 20000,
  spentMinor: 15000,
  remainingMinor: 5000,
  percentUsed: 75,
  percentOk: true,
  daysLeft: 5,
  dailyPaceMinor: 1000,
  dailyPaceOk: true,
  byPerson: [{ membershipId: "member-1", name: "Andreas", spentMinor: 15000 }],
  excludedNoRate: 0,
  overCount: 0,
  rolledOverAt: null,
  rolloverGoalId: null,
};

// A second, distinguishable body -- proving `reload`/`save` actually re-GET
// rather than a naive test passing against stubFetchRoutes' "same response
// forever" default for a route registered once.
const julyResponseAfterSave = {
  ...julyResponse,
  budget: { expectedIncomeMinor: 600000, lines: [{ categoryId: "cat-1", capMinor: 25000 }] },
  budgetedMinor: 25000,
};

// A fresh QueryClient per test, `retry: false` so a stubbed 500 surfaces on
// `.error` immediately instead of the hook waiting out react-query's
// default retry backoff -- the same convention router.test.tsx's
// `renderApp` and useTransactions.ts's callers already use. useBudget is
// built on `useQuery`/`useMutation` (Task 11 fix round: it started as a
// hand-rolled `useState`/`useEffect` pair, converted to match the house
// precedent useTransactions.ts's own month-parameterized `useQuery` sets),
// so every render here needs a real QueryClientProvider in scope, not just
// the fetch stub.
function renderUseBudget(month: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderHook(() => useBudget(month), {
    wrapper: ({ children }) => createElement(QueryClientProvider, { client: queryClient }, children),
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useBudget", () => {
  it("GETs the given month on mount", async () => {
    stubFetchRoutes({
      "GET /api/v1/budgets/2026-07": { status: 200, body: julyResponse },
    });

    const { result } = renderUseBudget("2026-07");

    expect(result.current.loading).toBe(true);
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.error).toBeNull();
    expect(result.current.data?.month).toBe("2026-07");
    expect(result.current.data?.budgetedMinor).toBe(20000);
  });

  // Task 9's empty state: no budget row for the month, but real spend
  // figures still computed. `data.budget` has to come through as the literal
  // `null` zod parsed it as, not `undefined` (which `?.` alone wouldn't
  // distinguish from "hasn't loaded yet") and not coerced into an empty
  // object.
  it("exposes `budget: null` for the empty state without losing the real spend figures", async () => {
    stubFetchRoutes({
      "GET /api/v1/budgets/2026-08": {
        status: 200,
        body: {
          ...julyResponse,
          month: "2026-08",
          budget: null,
        },
      },
      // The empty-state month is unbudgeted, so this hook also fetches
      // July (the previous month) for the "Import last month" card --
      // see the two prevMonthHasBudget tests below.
      "GET /api/v1/budgets/2026-07": { status: 200, body: julyResponse },
    });

    const { result } = renderUseBudget("2026-08");
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.error).toBeNull();
    expect(result.current.data?.budget).toBeNull();
    // The empty state still carries real spend -- BudgetService.Month
    // computes these regardless of whether a budget row exists.
    expect(result.current.data?.spentMinor).toBe(15000);
    expect(result.current.data?.budgetedMinor).toBe(20000);
  });

  // The Task 13 "Import last month" card: absent when the previous month
  // itself has no budget, present when it does. Both pinned at the hook
  // level (BudgetPage.test.tsx pins the DOM consequence) so a future change
  // to the enabled/gating logic breaks the test closest to the mistake.
  it("exposes prevMonthHasBudget: false when the previous month is also unbudgeted", async () => {
    stubFetchRoutes({
      "GET /api/v1/budgets/2026-08": { status: 200, body: { ...julyResponse, month: "2026-08", budget: null } },
      "GET /api/v1/budgets/2026-07": { status: 200, body: { ...julyResponse, budget: null } },
    });

    const { result } = renderUseBudget("2026-08");
    await waitFor(() => expect(result.current.loading).toBe(false));

    await waitFor(() => expect(result.current.prevMonthHasBudget).toBe(false));
    expect(result.current.prevMonth).toBe("2026-07");
  });

  it("exposes prevMonthHasBudget: true when the previous month has a real budget", async () => {
    stubFetchRoutes({
      "GET /api/v1/budgets/2026-08": { status: 200, body: { ...julyResponse, month: "2026-08", budget: null } },
      "GET /api/v1/budgets/2026-07": { status: 200, body: julyResponse },
    });

    const { result } = renderUseBudget("2026-08");
    await waitFor(() => expect(result.current.loading).toBe(false));

    await waitFor(() => expect(result.current.prevMonthHasBudget).toBe(true));
  });

  // The gating this hook depends on: no request to the previous month at
  // all when the current month already has a budget -- there is nothing
  // for the Import card to answer, so no reason to spend the request.
  it("never fetches the previous month when the current month already has a budget", async () => {
    const fetchMock = stubFetchRoutes({
      "GET /api/v1/budgets/2026-07": { status: 200, body: julyResponse },
    });

    const { result } = renderUseBudget("2026-07");
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.prevMonthHasBudget).toBeUndefined();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("save PUTs the exact body then re-GETs the month", async () => {
    let putBody: unknown;
    stubFetchRoutes({
      "GET /api/v1/budgets/2026-07": [
        { status: 200, body: julyResponse },
        { status: 200, body: julyResponseAfterSave },
      ],
      "PUT /api/v1/budgets/2026-07": {
        status: 200,
        body: { budget: julyResponseAfterSave.budget },
        capture: (body) => {
          putBody = body;
        },
      },
    });

    const { result } = renderUseBudget("2026-07");
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.save({
        expectedIncomeMinor: 600000,
        lines: [{ categoryId: "cat-1", capMinor: 25000 }],
      });
    });

    expect(putBody).toEqual({
      expectedIncomeMinor: 600000,
      lines: [{ categoryId: "cat-1", capMinor: 25000 }],
    });
    // The re-GET's second response, not the first -- proves save() actually
    // invalidated and refetched rather than the hook still showing whatever
    // GET returned on mount. A `waitFor`, not a synchronous assertion right
    // after `act`: `mutateAsync` resolving only guarantees
    // `invalidateQueries`'s refetch has been *dispatched*, not that the
    // query observer's `useSyncExternalStore` re-render has been flushed to
    // this render's `result.current` yet (react-query's notifyManager
    // batches that notification on its own microtask, one hop later than
    // `act`'s single flush pass covers) -- the same reason this codebase's
    // other invalidateQueries-driven UI tests (FinancesPage.test.tsx's
    // archive-button ones) assert through `waitFor` rather than immediately
    // after the triggering event.
    await waitFor(() => expect(result.current.data?.budgetedMinor).toBe(25000));
  });

  it("createCategory POSTs to /api/v1/categories then reloads the month", async () => {
    let postBody: unknown;
    stubFetchRoutes({
      "GET /api/v1/budgets/2026-07": [
        { status: 200, body: julyResponse },
        { status: 200, body: julyResponseAfterSave },
      ],
      "POST /api/v1/categories": {
        status: 201,
        body: { category: { id: "cat-2", name: "Rent", kind: "expense", archived: false } },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    const { result } = renderUseBudget("2026-07");
    await waitFor(() => expect(result.current.loading).toBe(false));

    let created: unknown;
    await act(async () => {
      created = await result.current.createCategory("Rent");
    });

    expect(postBody).toEqual({ name: "Rent" });
    // Pinned for BudgetModal.tsx: it has no other way to learn the new
    // category's real id before building the PUT's line set, since the
    // queued-create-then-PUT sequence must not interleave a second GET to
    // look the id back up by name.
    expect(created).toEqual({ id: "cat-2", name: "Rent", kind: "expense", archived: false });
    await waitFor(() => expect(result.current.data?.budgetedMinor).toBe(25000));
  });

  it("renameCategory PATCHes /api/v1/categories/{id} then reloads", async () => {
    let patchBody: unknown;
    stubFetchRoutes({
      "GET /api/v1/budgets/2026-07": [
        { status: 200, body: julyResponse },
        { status: 200, body: julyResponseAfterSave },
      ],
      "PATCH /api/v1/categories/cat-1": {
        status: 200,
        body: { category: { id: "cat-1", name: "Food", kind: "expense", archived: false } },
        capture: (body) => {
          patchBody = body;
        },
      },
    });

    const { result } = renderUseBudget("2026-07");
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.renameCategory("cat-1", "Food");
    });

    expect(patchBody).toEqual({ name: "Food" });
    await waitFor(() => expect(result.current.data?.budgetedMinor).toBe(25000));
  });

  it("archiveCategory POSTs /api/v1/categories/{id}/archive then reloads", async () => {
    stubFetchRoutes({
      "GET /api/v1/budgets/2026-07": [
        { status: 200, body: julyResponse },
        { status: 200, body: julyResponseAfterSave },
      ],
      "POST /api/v1/categories/cat-1/archive": {
        status: 200,
        body: { category: { id: "cat-1", name: "Groceries", kind: "expense", archived: true } },
      },
    });

    const { result } = renderUseBudget("2026-07");
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.archiveCategory("cat-1");
    });

    await waitFor(() => expect(result.current.data?.budgetedMinor).toBe(25000));
  });

  it("restoreCategory POSTs /api/v1/categories/{id}/restore then reloads", async () => {
    stubFetchRoutes({
      "GET /api/v1/budgets/2026-07": [
        { status: 200, body: julyResponse },
        { status: 200, body: julyResponseAfterSave },
      ],
      "POST /api/v1/categories/cat-1/restore": {
        status: 200,
        body: { category: { id: "cat-1", name: "Groceries", kind: "expense", archived: false } },
      },
    });

    const { result } = renderUseBudget("2026-07");
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.restoreCategory("cat-1");
    });

    await waitFor(() => expect(result.current.data?.budgetedMinor).toBe(25000));
  });

  it("surfaces a fetch failure as `error`, not a thrown exception", async () => {
    stubFetchRoutes({
      "GET /api/v1/budgets/2026-07": {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Something broke." } },
      },
    });

    const { result } = renderUseBudget("2026-07");
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.error).not.toBeNull();
    expect(result.current.data).toBeUndefined();
  });

  // Task 15: rollOver POSTs {goalId} to the month's own rollover route, then
  // re-GETs both this month (so BudgetRolloverCard.tsx can see the stamp)
  // and /goals (so the target goal's own contributedMinor/percent/status
  // move on the Goals screen) -- proven the same way "save PUTs the exact
  // body then re-GETs the month" is: a second, distinguishable body for
  // each re-GET. useGoals() is mounted alongside useBudget() in the same
  // QueryClient here specifically so there is an active observer for
  // /goals to refetch through -- react-query's default
  // `refetchType: "active"` only refetches a query invalidateQueries finds
  // someone is actually watching, and nothing else in this test file would
  // be.
  it("rollOver POSTs {goalId} then re-GETs both the month and /goals", async () => {
    let postBody: unknown;
    const julyResponseRolledOver = {
      ...julyResponse,
      rolledOverAt: "2026-07-31T00:00:00Z",
      rolloverGoalId: "goal-1",
    };
    const emptySummary = {
      plannedMonthlyTotalMinor: 0,
      actualThisMonthMinor: 0,
      onTrackCount: 0,
      datedCount: 0,
      noDateCount: 0,
      excludedNoRate: 0,
      nextGoal: null,
    };
    const goalsBefore = { currency: "SGD", goals: [], summary: emptySummary };
    const goalsAfter = {
      currency: "SGD",
      goals: [
        {
          id: "goal-1",
          name: "Bali trip",
          targetMinor: 400000,
          currency: "SGD",
          targetMonth: null,
          plannedMonthlyMinor: 0,
          contributedMinor: 5000,
          percent: 1,
          status: "on_track",
          requiredMonthlyMinor: 0,
          requiredMonthlyOk: false,
          archivedAt: null,
        },
      ],
      summary: emptySummary,
    };
    stubFetchRoutes({
      "GET /api/v1/budgets/2026-07": [
        { status: 200, body: julyResponse },
        { status: 200, body: julyResponseRolledOver },
      ],
      "GET /api/v1/goals": [
        { status: 200, body: goalsBefore },
        { status: 200, body: goalsAfter },
      ],
      "POST /api/v1/budgets/2026-07/rollover": {
        status: 200,
        body: {
          contribution: {
            id: "contribution-1",
            amountMinor: 5000,
            occurredOn: "2026-07-31",
            note: "",
            source: "budget_rollover",
            sourceBudgetMonth: "2026-07",
          },
        },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result } = renderHook(
      () => ({ budget: useBudget("2026-07"), goals: useGoals() }),
      { wrapper: ({ children }) => createElement(QueryClientProvider, { client: queryClient }, children) },
    );
    await waitFor(() => expect(result.current.budget.loading).toBe(false));
    await waitFor(() => expect(result.current.goals.loading).toBe(false));

    await act(async () => {
      await result.current.budget.rollOver("goal-1");
    });

    expect(postBody).toEqual({ goalId: "goal-1" });
    // The month re-GET's second, stamped response -- proves rollOver
    // actually invalidated and refetched this month rather than the hook
    // still showing whatever GET returned on mount.
    await waitFor(() => expect(result.current.budget.data?.rolloverGoalId).toBe("goal-1"));
    // /goals re-GET's second, populated response -- proves the same write
    // reaches the Goals screen's own query, not just this month's.
    await waitFor(() => expect(result.current.goals.data?.goals).toHaveLength(1));
  });
});

// budgetHistoryResponseSchema and categoryResponseSchema aren't parsed by
// useBudget itself (the History modal and the category-write responses are
// Task 15/Task 14's job respectively, and useBudget's category mutations
// deliberately discard their response body and invalidate instead -- see
// useBudget.ts's own comments). Parsing a realistic wire body here pins the
// two schemas against Task 9/10's actual DTOs now, rather than leaving the
// first real drift to surface as an unexplained parse throw two tasks from
// now.
describe("budgetSchemas", () => {
  it("parses a GET /budgets/history month row", () => {
    const parsed = budgetHistoryResponseSchema.parse({
      months: [
        { month: "2026-07", budgetedMinor: 50000, spentMinor: 42000, closed: false },
        { month: "2026-06", budgetedMinor: 50000, spentMinor: 61000, closed: true },
      ],
    });
    expect(parsed.months).toHaveLength(2);
    expect(parsed.months[1]).toEqual({
      month: "2026-06",
      budgetedMinor: 50000,
      spentMinor: 61000,
      closed: true,
    });
  });

  it("parses a category write's {category} response", () => {
    const parsed = categoryResponseSchema.parse({
      category: { id: "cat-1", name: "Groceries", kind: "expense", archived: false },
    });
    expect(parsed.category).toEqual({
      id: "cat-1",
      name: "Groceries",
      kind: "expense",
      archived: false,
    });
  });
});
