import { createElement } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { useRetro } from "./useRetro";
import { useRetros } from "./useRetros";
import type { Retro } from "./retroSchemas";

// Defaults taken from the real, verified-against-real-data sample in
// .superpowers/sdd/2026-08-16-hearth-retros/task-7-report.md -- captured
// from TestRetroWireShapeWithRealDataMatchesTheBrief's own `-v` output, not
// hand-invented, per that task's own finding that a hand-transcribed sample
// had shipped wrong once already.
function retroFixture(overrides: Partial<Retro> = {}): Retro {
  return {
    id: "4844a918-fa2f-446c-b43b-deddecb49889",
    month: "2026-07",
    mood: 4,
    wentWell: "We stuck to the grocery budget.",
    wasHard: "Christine's parents visiting threw off the schedule.",
    notes: "We finally fixed the grocery budget. Next month: try meal prep.",
    completedAt: null,
    version: 1,
    actions: [],
    ...overrides,
  };
}

const retrosListResponse = {
  retros: [
    {
      id: "4844a918-fa2f-446c-b43b-deddecb49889",
      month: "2026-07",
      mood: 4,
      actionCount: 1,
      quote: "We finally fixed the grocery budget.",
      finished: true,
    },
  ],
  mood: [
    { month: "2026-06", mood: null },
    { month: "2026-07", mood: 4 },
  ],
  doneCount: 1,
  since: "2026-07",
  startMonth: "2026-08",
};

function renderUseRetro(month: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderHook(() => useRetro(month), {
    wrapper: ({ children }) => createElement(QueryClientProvider, { client: queryClient }, children),
  });
}

function renderUseRetros() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderHook(() => useRetros(), {
    wrapper: ({ children }) => createElement(QueryClientProvider, { client: queryClient }, children),
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useRetros", () => {
  it("GETs /api/v1/retros on mount", async () => {
    stubFetchRoutes({
      "GET /api/v1/retros": { status: 200, body: retrosListResponse },
    });

    const { result } = renderUseRetros();
    expect(result.current.loading).toBe(true);
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.error).toBeNull();
    expect(result.current.data?.doneCount).toBe(1);
    expect(result.current.data?.startMonth).toBe("2026-08");
    expect(result.current.data?.retros).toHaveLength(1);
    // Pins the gap-vs-picked distinction on the mood chart itself, not just
    // on a single retro's own mood field.
    expect(result.current.data?.mood[0].mood).toBeNull();
    expect(result.current.data?.mood[1].mood).toBe(4);
  });

  // POST /retros reads no body -- the server, not the caller, picks the
  // month (handleStartRetro's own comment). sawBody stays undefined the
  // whole test if the hook ever attaches one.
  it("startRetro POSTs /api/v1/retros with no body, returns the created retro and reloads the list", async () => {
    let sawBody: unknown = "not called";
    stubFetchRoutes({
      "GET /api/v1/retros": [
        { status: 200, body: { ...retrosListResponse, retros: [] } },
        { status: 200, body: retrosListResponse },
      ],
      "POST /api/v1/retros": {
        status: 201,
        body: { retro: retroFixture({ month: "2026-08", version: 1 }) },
        capture: (body) => {
          sawBody = body;
        },
      },
    });

    const { result } = renderUseRetros();
    await waitFor(() => expect(result.current.loading).toBe(false));

    let created: Retro | undefined;
    await act(async () => {
      created = await result.current.startRetro();
    });

    expect(sawBody).toBeUndefined();
    expect(created?.month).toBe("2026-08");
    // Proves startRetro invalidates and awaits the list's own refetch, not
    // just fires the POST -- the same reason every mutation here awaits its
    // invalidation.
    await waitFor(() => expect(result.current.data?.retros).toHaveLength(1));
  });
});

describe("useRetro", () => {
  it("GETs /api/v1/retros/{month} on mount", async () => {
    stubFetchRoutes({
      "GET /api/v1/retros/2026-07": {
        status: 200,
        body: { retro: retroFixture(), carryOver: [] },
      },
    });

    const { result } = renderUseRetro("2026-07");
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.error).toBeNull();
    expect(result.current.data?.retro.month).toBe("2026-07");
    expect(result.current.data?.carryOver).toEqual([]);
  });

  // task-9-brief.md's own Step 1 sketch, adapted two ways to this
  // codebase's real conventions: the brief's `stub.bodyOf(...)` does not
  // exist here (fetchStub.ts's own `capture` callback is how a registered
  // route hands a request body back to a test, the same convention
  // useGoals.test.ts already uses), and the GET route needs a second queued
  // response -- this hook invalidates-and-refetches after a write rather
  // than trusting the write response directly (the useBudget.ts/useGoals.ts
  // house convention: "the month response is the one place every derived
  // figure is computed together"), so version 4 is what the refetch after
  // the PATCH actually returns, not the PATCH response itself.
  it("saveRetro sends the version it loaded and stores the one it gets back", async () => {
    let patchBody: unknown;
    stubFetchRoutes({
      "GET /api/v1/retros/2026-07": [
        { status: 200, body: { retro: retroFixture({ version: 3 }), carryOver: [] } },
        { status: 200, body: { retro: retroFixture({ version: 4, notes: "saved" }), carryOver: [] } },
      ],
      "PATCH /api/v1/retros/2026-07": {
        status: 200,
        body: { retro: retroFixture({ version: 4, notes: "saved" }) },
        capture: (body) => {
          patchBody = body;
        },
      },
    });

    const { result } = renderUseRetro("2026-07");
    await waitFor(() => expect(result.current.data?.retro.version).toBe(3));

    await act(() => result.current.saveRetro({ notes: "saved" }));

    expect(patchBody).toMatchObject({ version: 3 });
    await waitFor(() => expect(result.current.data?.retro.version).toBe(4));
  });

  it("surfaces a 409 as a conflict the page can branch on, not as a generic error", async () => {
    stubFetchRoutes({
      "GET /api/v1/retros/2026-07": {
        status: 200,
        body: { retro: retroFixture({ version: 3 }), carryOver: [] },
      },
      "PATCH /api/v1/retros/2026-07": {
        status: 409,
        body: {
          error: { code: "RETRO_CHANGED", message: "Someone else saved this retro while you were editing." },
        },
      },
    });

    const { result } = renderUseRetro("2026-07");
    await waitFor(() => expect(result.current.data).toBeDefined());
    expect(result.current.conflict).toBe(false);

    await act(() => result.current.saveRetro({ notes: "mine" }).catch(() => {}));

    expect(result.current.conflict).toBe(true);
  });

  // conflict is cleared "on a successful refetch," not left true forever
  // once tripped -- Task 13's modal needs its banner to go away once the
  // page shows current data again. This has to hold for reload() itself...
  it("clears conflict once an explicit reload() resolves successfully", async () => {
    stubFetchRoutes({
      "GET /api/v1/retros/2026-07": [
        { status: 200, body: { retro: retroFixture({ version: 3 }), carryOver: [] } },
        { status: 200, body: { retro: retroFixture({ version: 3 }), carryOver: [] } },
      ],
      "PATCH /api/v1/retros/2026-07": {
        status: 409,
        body: { error: { code: "RETRO_CHANGED", message: "Someone else saved this retro." } },
      },
    });

    const { result } = renderUseRetro("2026-07");
    await waitFor(() => expect(result.current.data).toBeDefined());

    await act(() => result.current.saveRetro({ notes: "mine" }).catch(() => {}));
    expect(result.current.conflict).toBe(true);

    await act(async () => {
      await result.current.reload();
    });

    expect(result.current.conflict).toBe(false);
  });

  // ...and, distinctly, for a completely unrelated write's own refetch too --
  // conflict is derived from the query's own dataUpdatedAt moving past the
  // failed save's own mark, not from a hard-coded list of which specific
  // call site is allowed to clear it. A caller that never calls reload()
  // itself (Task 13's modal, say, if the household ticks an action off
  // while the conflict banner is still showing) must still see it go away
  // once ANY later fetch for this month succeeds.
  it("clears conflict once a different, unrelated write's own refetch resolves successfully", async () => {
    const action = {
      id: "action-1",
      body: "Set up a shared grocery list",
      doneAt: null,
      carriedFrom: "",
      assigneeMembershipIds: [],
    };
    stubFetchRoutes({
      "GET /api/v1/retros/2026-07": [
        { status: 200, body: { retro: retroFixture({ version: 3, actions: [action] }), carryOver: [] } },
        {
          status: 200,
          body: {
            retro: retroFixture({ version: 3, actions: [{ ...action, doneAt: "2026-08-16T21:18:52Z" }] }),
            carryOver: [],
          },
        },
      ],
      "PATCH /api/v1/retros/2026-07": {
        status: 409,
        body: { error: { code: "RETRO_CHANGED", message: "Someone else saved this retro." } },
      },
      "PATCH /api/v1/retros/2026-07/actions/action-1": {
        status: 200,
        body: { id: "action-1", doneAt: "2026-08-16T21:18:52Z" },
      },
    });

    const { result } = renderUseRetro("2026-07");
    await waitFor(() => expect(result.current.data).toBeDefined());

    await act(() => result.current.saveRetro({ notes: "mine" }).catch(() => {}));
    expect(result.current.conflict).toBe(true);

    await act(async () => {
      await result.current.setActionDone("action-1", true);
    });

    expect(result.current.conflict).toBe(false);
  });

  it("mood null round-trips as null, never as 0", async () => {
    stubFetchRoutes({
      "GET /api/v1/retros/2026-07": {
        status: 200,
        body: { retro: retroFixture({ mood: null }), carryOver: [] },
      },
    });

    const { result } = renderUseRetro("2026-07");
    await waitFor(() => expect(result.current.data).toBeDefined());

    expect(result.current.data?.retro.mood).toBeNull();
    expect(result.current.data?.retro.mood).not.toBe(0);
  });

  it("finishRetro POSTs .../complete then reloads", async () => {
    stubFetchRoutes({
      "GET /api/v1/retros/2026-07": [
        { status: 200, body: { retro: retroFixture({ completedAt: null }), carryOver: [] } },
        {
          status: 200,
          body: { retro: retroFixture({ completedAt: "2026-08-16T21:18:52.716044+08:00" }), carryOver: [] },
        },
      ],
      "POST /api/v1/retros/2026-07/complete": {
        status: 200,
        body: { retro: retroFixture({ completedAt: "2026-08-16T21:18:52.716044+08:00" }) },
      },
    });

    const { result } = renderUseRetro("2026-07");
    await waitFor(() => expect(result.current.data?.retro.completedAt).toBeNull());

    await act(async () => {
      await result.current.finishRetro();
    });

    await waitFor(() => expect(result.current.data?.retro.completedAt).not.toBeNull());
  });

  // DELETE answers 204 with no body -- the one status apiFetch does not try
  // to parse (useGoals.ts's deleteContribution precedent). The refetch this
  // triggers now 404s (the draft is gone), which the hook surfaces as
  // `error` the same as any other failed GET -- discardDraft itself does
  // not throw for that.
  it("discardDraft DELETEs and tolerates the bodyless 204", async () => {
    stubFetchRoutes({
      "GET /api/v1/retros/2026-07": [
        { status: 200, body: { retro: retroFixture(), carryOver: [] } },
        { status: 404, body: { error: { code: "NOT_FOUND", message: "That could not be found." } } },
      ],
      "DELETE /api/v1/retros/2026-07": { status: 204, body: undefined },
    });

    const { result } = renderUseRetro("2026-07");
    await waitFor(() => expect(result.current.data).toBeDefined());

    await act(async () => {
      await result.current.discardDraft();
    });

    await waitFor(() => expect(result.current.error).not.toBeNull());
  });

  it("addAction POSTs .../actions with the optional fields filled in, then reloads", async () => {
    let postBody: unknown;
    const action = {
      id: "4625a31b-c9c1-4427-b68b-5edeaf980e71",
      body: "Set up a shared grocery list",
      doneAt: null,
      carriedFrom: "",
      assigneeMembershipIds: [],
    };
    stubFetchRoutes({
      "GET /api/v1/retros/2026-07": [
        { status: 200, body: { retro: retroFixture({ actions: [] }), carryOver: [] } },
        { status: 200, body: { retro: retroFixture({ actions: [action] }), carryOver: [] } },
      ],
      "POST /api/v1/retros/2026-07/actions": {
        status: 201,
        body: { action },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    const { result } = renderUseRetro("2026-07");
    await waitFor(() => expect(result.current.data?.retro.actions).toHaveLength(0));

    await act(async () => {
      await result.current.addAction({ body: "Set up a shared grocery list" });
    });

    expect(postBody).toEqual({
      body: "Set up a shared grocery list",
      assigneeMembershipIds: [],
      carriedFrom: "",
    });
    await waitFor(() => expect(result.current.data?.retro.actions).toHaveLength(1));
  });

  it("setActionDone PATCHes .../actions/{id} then reloads", async () => {
    const undone = {
      id: "action-1",
      body: "Set up a shared grocery list",
      doneAt: null,
      carriedFrom: "",
      assigneeMembershipIds: [],
    };
    const done = { ...undone, doneAt: "2026-08-16T21:18:52Z" };
    stubFetchRoutes({
      "GET /api/v1/retros/2026-07": [
        { status: 200, body: { retro: retroFixture({ actions: [undone] }), carryOver: [] } },
        { status: 200, body: { retro: retroFixture({ actions: [done] }), carryOver: [] } },
      ],
      "PATCH /api/v1/retros/2026-07/actions/action-1": {
        status: 200,
        body: { id: "action-1", doneAt: "2026-08-16T21:18:52Z" },
      },
    });

    const { result } = renderUseRetro("2026-07");
    await waitFor(() => expect(result.current.data?.retro.actions[0]?.doneAt).toBeNull());

    await act(async () => {
      await result.current.setActionDone("action-1", true);
    });

    await waitFor(() => expect(result.current.data?.retro.actions[0]?.doneAt).not.toBeNull());
  });

  it("removeAction DELETEs and tolerates the bodyless 204", async () => {
    const action = {
      id: "action-1",
      body: "Set up a shared grocery list",
      doneAt: null,
      carriedFrom: "",
      assigneeMembershipIds: [],
    };
    stubFetchRoutes({
      "GET /api/v1/retros/2026-07": [
        { status: 200, body: { retro: retroFixture({ actions: [action] }), carryOver: [] } },
        { status: 200, body: { retro: retroFixture({ actions: [] }), carryOver: [] } },
      ],
      "DELETE /api/v1/retros/2026-07/actions/action-1": { status: 204, body: undefined },
    });

    const { result } = renderUseRetro("2026-07");
    await waitFor(() => expect(result.current.data?.retro.actions).toHaveLength(1));

    await act(async () => {
      await result.current.removeAction("action-1");
    });

    await waitFor(() => expect(result.current.data?.retro.actions).toHaveLength(0));
  });

  it("surfaces a fetch failure as `error`, not a thrown exception", async () => {
    stubFetchRoutes({
      "GET /api/v1/retros/2026-07": {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Something broke." } },
      },
    });

    const { result } = renderUseRetro("2026-07");
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.error).not.toBeNull();
    expect(result.current.data).toBeUndefined();
  });
});
