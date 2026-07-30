import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { useBudget } from "./useBudget";

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
};

// A second, distinguishable body -- proving `reload`/`save` actually re-GET
// rather than a naive test passing against stubFetchRoutes' "same response
// forever" default for a route registered once.
const julyResponseAfterSave = {
  ...julyResponse,
  budget: { expectedIncomeMinor: 600000, lines: [{ categoryId: "cat-1", capMinor: 25000 }] },
  budgetedMinor: 25000,
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useBudget", () => {
  it("GETs the given month on mount", async () => {
    stubFetchRoutes({
      "GET /api/v1/budgets/2026-07": { status: 200, body: julyResponse },
    });

    const { result } = renderHook(() => useBudget("2026-07"));

    expect(result.current.loading).toBe(true);
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.error).toBeNull();
    expect(result.current.data?.month).toBe("2026-07");
    expect(result.current.data?.budgetedMinor).toBe(20000);
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

    const { result } = renderHook(() => useBudget("2026-07"));
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
    // reloaded rather than the hook still showing whatever GET returned on
    // mount.
    expect(result.current.data?.budgetedMinor).toBe(25000);
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

    const { result } = renderHook(() => useBudget("2026-07"));
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.createCategory("Rent");
    });

    expect(postBody).toEqual({ name: "Rent" });
    expect(result.current.data?.budgetedMinor).toBe(25000);
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

    const { result } = renderHook(() => useBudget("2026-07"));
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.renameCategory("cat-1", "Food");
    });

    expect(patchBody).toEqual({ name: "Food" });
    expect(result.current.data?.budgetedMinor).toBe(25000);
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

    const { result } = renderHook(() => useBudget("2026-07"));
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.archiveCategory("cat-1");
    });

    expect(result.current.data?.budgetedMinor).toBe(25000);
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

    const { result } = renderHook(() => useBudget("2026-07"));
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.restoreCategory("cat-1");
    });

    expect(result.current.data?.budgetedMinor).toBe(25000);
  });

  it("surfaces a fetch failure as `error`, not a thrown exception", async () => {
    stubFetchRoutes({
      "GET /api/v1/budgets/2026-07": {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Something broke." } },
      },
    });

    const { result } = renderHook(() => useBudget("2026-07"));
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.error).not.toBeNull();
    expect(result.current.data).toBeUndefined();
  });
});
