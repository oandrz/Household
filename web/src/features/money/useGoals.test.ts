import { createElement } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { useGoalContributions, useGoals } from "./useGoals";

const goalsResponse = {
  currency: "SGD",
  goals: [
    {
      id: "goal-1",
      name: "Bali trip",
      targetMinor: 400000,
      currency: "SGD",
      targetMonth: "2026-12",
      plannedMonthlyMinor: 35000,
      contributedMinor: 260000,
      percent: 65,
      status: "on_track",
      requiredMonthlyMinor: 28000,
      requiredMonthlyOk: true,
      archivedAt: null,
    },
  ],
  summary: {
    plannedMonthlyTotalMinor: 35000,
    actualThisMonthMinor: 35000,
    onTrackCount: 1,
    datedCount: 1,
    noDateCount: 0,
    excludedNoRate: 0,
    nextGoal: { id: "goal-1", name: "Bali trip", targetMonth: "2026-12" },
  },
};

// A second, distinguishable body -- proves a mutation's onSuccess actually
// invalidates and refetches rather than the hook still showing whatever the
// first GET returned (the same reason useBudget.test.ts's
// julyResponseAfterSave exists).
const goalsResponseAfterWrite = {
  ...goalsResponse,
  goals: [{ ...goalsResponse.goals[0], contributedMinor: 300000, percent: 75 }],
};

function renderUseGoals(options?: { includeArchived?: boolean; enabled?: boolean }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderHook(() => useGoals(options), {
    wrapper: ({ children }) => createElement(QueryClientProvider, { client: queryClient }, children),
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useGoals", () => {
  it("GETs /api/v1/goals on mount", async () => {
    stubFetchRoutes({
      "GET /api/v1/goals": { status: 200, body: goalsResponse },
    });

    const { result } = renderUseGoals();

    expect(result.current.loading).toBe(true);
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.error).toBeNull();
    expect(result.current.data?.currency).toBe("SGD");
    expect(result.current.data?.goals).toHaveLength(1);
    expect(result.current.data?.goals[0].name).toBe("Bali trip");
  });

  // include_archived is spelled the way account_handlers.go already spells
  // it on GET /accounts (goal_handlers.go's own comment on handleListGoals),
  // so this pins that this hook spells it identically rather than inventing
  // a second convention.
  it("GETs with ?include_archived=true when includeArchived is set", async () => {
    stubFetchRoutes({
      "GET /api/v1/goals?include_archived=true": { status: 200, body: goalsResponse },
    });

    const { result } = renderUseGoals({ includeArchived: true });
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.data?.goals).toHaveLength(1);
  });

  it("createGoal POSTs the exact body and returns the parsed goal", async () => {
    let postBody: unknown;
    stubFetchRoutes({
      "GET /api/v1/goals": [
        { status: 200, body: { ...goalsResponse, goals: [] } },
        { status: 200, body: goalsResponse },
      ],
      "POST /api/v1/goals": {
        status: 201,
        body: { goal: goalsResponse.goals[0] },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    const { result } = renderUseGoals();
    await waitFor(() => expect(result.current.loading).toBe(false));

    const body = {
      name: "Bali trip",
      targetMinor: 400000,
      currency: "SGD",
      targetMonth: "2026-12",
      plannedMonthlyMinor: 35000,
      startingBalanceMinor: 0,
    };
    let created: unknown;
    await act(async () => {
      created = await result.current.createGoal(body);
    });

    expect(postBody).toEqual(body);
    expect(created).toEqual(goalsResponse.goals[0]);
    // Also proves createGoal invalidates the goals list, the same
    // fire-a-mutation-then-see-the-list-move shape every write below pins.
    await waitFor(() => expect(result.current.data?.goals).toHaveLength(1));
  });

  it("updateGoal PATCHes /api/v1/goals/{id} then reloads", async () => {
    let patchBody: unknown;
    stubFetchRoutes({
      "GET /api/v1/goals": [
        { status: 200, body: goalsResponse },
        { status: 200, body: goalsResponseAfterWrite },
      ],
      "PATCH /api/v1/goals/goal-1": {
        status: 200,
        body: { goal: goalsResponseAfterWrite.goals[0] },
        capture: (body) => {
          patchBody = body;
        },
      },
    });

    const { result } = renderUseGoals();
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.updateGoal("goal-1", { plannedMonthlyMinor: 40000 });
    });

    expect(patchBody).toEqual({ plannedMonthlyMinor: 40000 });
    await waitFor(() => expect(result.current.data?.goals[0].contributedMinor).toBe(300000));
  });

  it("archiveGoal POSTs /api/v1/goals/{id}/archive then reloads", async () => {
    stubFetchRoutes({
      "GET /api/v1/goals": [
        { status: 200, body: goalsResponse },
        { status: 200, body: { ...goalsResponse, goals: [] } },
      ],
      "POST /api/v1/goals/goal-1/archive": {
        status: 200,
        body: { goal: { ...goalsResponse.goals[0], archivedAt: "2026-08-01T00:00:00Z" } },
      },
    });

    const { result } = renderUseGoals();
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.archiveGoal("goal-1");
    });

    await waitFor(() => expect(result.current.data?.goals).toHaveLength(0));
  });

  it("restoreGoal POSTs /api/v1/goals/{id}/restore then reloads", async () => {
    stubFetchRoutes({
      "GET /api/v1/goals": [
        { status: 200, body: { ...goalsResponse, goals: [] } },
        { status: 200, body: goalsResponse },
      ],
      "POST /api/v1/goals/goal-1/restore": {
        status: 200,
        body: { goal: goalsResponse.goals[0] },
      },
    });

    const { result } = renderUseGoals();
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.restoreGoal("goal-1");
    });

    await waitFor(() => expect(result.current.data?.goals).toHaveLength(1));
  });

  // The brief's own pin: adding a contribution POSTs then re-fetches /goals
  // (not just its own contributions list) -- a contribution changes the
  // card's progress and the page summary at once.
  it("addContribution POSTs then re-fetches /goals", async () => {
    let postBody: unknown;
    stubFetchRoutes({
      "GET /api/v1/goals": [
        { status: 200, body: goalsResponse },
        { status: 200, body: goalsResponseAfterWrite },
      ],
      "POST /api/v1/goals/goal-1/contributions": {
        status: 201,
        body: {
          contribution: {
            id: "contrib-1",
            amountMinor: 40000,
            occurredOn: "2026-08-01",
            note: "Bonus",
            source: "manual",
            sourceBudgetMonth: null,
          },
        },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    const { result } = renderUseGoals();
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.addContribution("goal-1", {
        amountMinor: 40000,
        occurredOn: "2026-08-01",
        note: "Bonus",
      });
    });

    expect(postBody).toEqual({ amountMinor: 40000, occurredOn: "2026-08-01", note: "Bonus" });
    await waitFor(() => expect(result.current.data?.goals[0].contributedMinor).toBe(300000));
  });

  // DELETE answers 204 with no body -- the one status apiFetch does not try
  // to parse. A schema-parse or a `.json()` read here would throw against an
  // empty body, so this pins that deleteContribution tolerates it.
  it("deleteContribution DELETEs and tolerates the bodyless 204", async () => {
    stubFetchRoutes({
      "GET /api/v1/goals": [
        { status: 200, body: goalsResponse },
        { status: 200, body: goalsResponseAfterWrite },
      ],
      "DELETE /api/v1/goals/goal-1/contributions/contrib-1": { status: 204, body: undefined },
    });

    const { result } = renderUseGoals();
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.deleteContribution("goal-1", "contrib-1");
    });

    await waitFor(() => expect(result.current.data?.goals[0].contributedMinor).toBe(300000));
  });

  it("surfaces a fetch failure as `error`, not a thrown exception", async () => {
    stubFetchRoutes({
      "GET /api/v1/goals": {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Something broke." } },
      },
    });

    const { result } = renderUseGoals();
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.error).not.toBeNull();
    expect(result.current.data).toBeUndefined();
  });
});

describe("useGoalContributions", () => {
  it("GETs /api/v1/goals/{id}/contributions only while enabled", async () => {
    const fetchMock = stubFetchRoutes({
      "GET /api/v1/goals/goal-1/contributions": {
        status: 200,
        body: {
          contributions: [
            {
              id: "contrib-1",
              amountMinor: 40000,
              occurredOn: "2026-08-01",
              note: "Bonus",
              source: "manual",
              sourceBudgetMonth: null,
            },
          ],
        },
      },
    });

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { result, rerender } = renderHook(
      ({ enabled }: { enabled: boolean }) => useGoalContributions("goal-1", enabled),
      {
        initialProps: { enabled: false },
        wrapper: ({ children }) =>
          createElement(QueryClientProvider, { client: queryClient }, children),
      },
    );

    expect(result.current.loading).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();

    rerender({ enabled: true });
    await waitFor(() => expect(result.current.data?.contributions).toHaveLength(1));
    expect(result.current.data?.contributions[0].note).toBe("Bonus");
  });
});
