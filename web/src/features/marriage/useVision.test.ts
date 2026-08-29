import { createElement } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { useVision, type SaveVisionBody } from "./useVision";
import type { Vision } from "./visionSchemas";

// Defaults follow retroFixture's own convention in useRetros.test.ts: every
// field named explicitly, with overrides applied last, so a test that wants
// only one field different says so at the call site rather than duplicating
// the whole shape. Field names and their meanings are taken directly from
// api/internal/adapter/http/vision_handlers.go, read for this task.
function visionFixture(overrides: Partial<Vision> = {}): Vision {
  return {
    year: 2026,
    theme: "",
    description: "",
    version: 0,
    pillars: [],
    milestones: [],
    ...overrides,
  };
}

function renderUseVision(year: number) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderHook(() => useVision(year), {
    wrapper: ({ children }) => createElement(QueryClientProvider, { client: queryClient }, children),
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useVision", () => {
  it("treats a year that has no vision as version 0 rather than an error", async () => {
    stubFetchRoutes({
      "GET /api/v1/marriage/vision?year=2026": { status: 200, body: { vision: visionFixture() } },
    });

    const { result } = renderUseVision(2026);
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.error).toBeNull();
    expect(result.current.data?.version).toBe(0);
    expect(result.current.data?.theme).toBe("");
  });

  // parses every field on a populated document, not just the empty-pillars
  // shape every other fixture in this file uses -- a typed measure, a linked
  // measure, a pillar with its own description, and a milestone with a note.
  // Without this, the schemas' field-for-field agreement with the Go DTOs
  // rested on manual comparison rather than the suite, and a typo here would
  // only ever surface against a real server.
  it("parses a populated vision -- a typed measure, a linked measure, a pillar description and a milestone note", async () => {
    stubFetchRoutes({
      "GET /api/v1/marriage/vision?year=2026": {
        status: 200,
        body: {
          vision: visionFixture({
            theme: "Slow down together",
            description: "Fewer commitments, more presence.",
            version: 7,
            pillars: [
              {
                name: "Us",
                description: "Time set aside for each other",
                measures: [
                  {
                    label: "Date nights",
                    kind: "typed",
                    hasFigure: true,
                    current: 2,
                    target: 2,
                    percent: 0,
                    met: true,
                    goalId: "",
                    goalName: "",
                  },
                  {
                    label: "House fund",
                    kind: "linked",
                    hasFigure: true,
                    current: 6000,
                    target: 10000,
                    percent: 60,
                    met: false,
                    goalId: "goal-1",
                    goalName: "House deposit",
                  },
                ],
              },
            ],
            milestones: [{ year: 2030, title: "Buy a house", note: "Save the deposit first" }],
          }),
        },
      },
    });

    const { result } = renderUseVision(2026);
    await waitFor(() => expect(result.current.data).toBeDefined());

    const pillar = result.current.data?.pillars[0];
    expect(pillar?.name).toBe("Us");
    expect(pillar?.description).toBe("Time set aside for each other");

    const [typedMeasure, linkedMeasure] = pillar?.measures ?? [];
    expect(typedMeasure).toMatchObject({ label: "Date nights", kind: "typed", current: 2, target: 2, met: true });
    expect(linkedMeasure).toMatchObject({
      label: "House fund",
      kind: "linked",
      percent: 60,
      goalId: "goal-1",
      goalName: "House deposit",
    });

    const milestone = result.current.data?.milestones[0];
    expect(milestone).toEqual({ year: 2030, title: "Buy a house", note: "Save the deposit first" });
  });

  // A hook that let a caller pass version could send a stale one by
  // accident -- the version on the wire must always be the one this hook
  // loaded, never one a caller supplies. Same mutation check
  // useRetros.test.ts makes of saveRetro's own version guard.
  //
  // Asserted with toEqual against the WHOLE body, not toMatchObject against
  // version alone: toMatchObject ignores keys it isn't told to check, so a
  // save that silently dropped theme/description/pillars/milestones would
  // still have passed a version-only assertion. This feature has exactly one
  // write path, and this is what actually proves it sends the household's
  // real content.
  it("sends the version it loaded, never one the caller supplies", async () => {
    let putBody: unknown;
    const saveBody: SaveVisionBody = {
      theme: "Slow down together",
      description: "Fewer commitments.",
      pillars: [
        {
          name: "Us",
          description: "Time set aside for each other",
          measures: [{ label: "Date nights", kind: "typed", current: 1, target: 2, goalId: "" }],
        },
      ],
      milestones: [{ year: 2028, title: "Buy a house", note: "Save the deposit first" }],
    };
    stubFetchRoutes({
      "GET /api/v1/marriage/vision?year=2026": [
        {
          status: 200,
          body: {
            vision: visionFixture({
              theme: "Slow down together",
              description: "Fewer commitments.",
              version: 4,
            }),
          },
        },
        {
          status: 200,
          body: {
            vision: visionFixture({
              theme: "Slow down together",
              description: "Fewer commitments.",
              version: 5,
            }),
          },
        },
      ],
      "PUT /api/v1/marriage/vision/2026": {
        status: 200,
        body: {
          vision: visionFixture({
            theme: "Slow down together",
            description: "Fewer commitments.",
            version: 5,
          }),
        },
        capture: (body) => {
          putBody = body;
        },
      },
    });

    const { result } = renderUseVision(2026);
    await waitFor(() => expect(result.current.data?.version).toBe(4));

    await act(() => result.current.saveVision(saveBody));

    expect(putBody).toEqual({ ...saveBody, version: 4 });
    // Proves saveVision invalidates and awaits the refetch, not just fires
    // the PUT -- the same reason useRetro.ts's own writes await afterWrite.
    await waitFor(() => expect(result.current.data?.version).toBe(5));
  });

  it("sends version 0 for a year that had none, so the save is a create", async () => {
    let putBody: unknown;
    const saveBody: SaveVisionBody = {
      theme: "T",
      description: "",
      pillars: [
        {
          name: "P",
          description: "",
          measures: [{ label: "M", kind: "linked", current: 0, target: 0, goalId: "goal-1" }],
        },
      ],
      milestones: [{ year: 2030, title: "M", note: "" }],
    };
    stubFetchRoutes({
      "GET /api/v1/marriage/vision?year=2026": [
        { status: 200, body: { vision: visionFixture() } },
        { status: 200, body: { vision: visionFixture({ theme: "T", version: 1 }) } },
      ],
      "PUT /api/v1/marriage/vision/2026": {
        status: 200,
        body: { vision: visionFixture({ theme: "T", version: 1 }) },
        capture: (body) => {
          putBody = body;
        },
      },
    });

    const { result } = renderUseVision(2026);
    await waitFor(() => expect(result.current.data?.version).toBe(0));

    await act(() => result.current.saveVision(saveBody));

    expect(putBody).toEqual({ ...saveBody, version: 0 });
    await waitFor(() => expect(result.current.data?.version).toBe(1));
  });

  it("refuses a response whose measure kind it does not recognise", async () => {
    // The zod boundary is the frontend's own fail-closed rule: a server that
    // drifts must surface as an error, not render as a confident wrong number.
    // Built as `unknown`, not through visionFixture: this is deliberately not
    // a valid Vision (that's the point of the test), so it must not be typed
    // as one.
    const driftedResponse: unknown = {
      vision: {
        ...visionFixture({ version: 4 }),
        pillars: [
          {
            name: "P",
            description: "",
            measures: [
              {
                label: "M",
                kind: "sideways",
                hasFigure: true,
                current: 0,
                target: 0,
                percent: 0,
                met: false,
                goalId: "",
                goalName: "",
              },
            ],
          },
        ],
      },
    };
    stubFetchRoutes({
      "GET /api/v1/marriage/vision?year=2026": { status: 200, body: driftedResponse },
    });

    const { result } = renderUseVision(2026);
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(result.current.data).toBeUndefined();
  });

  // The 409 VISION_CHANGED / everything-else distinction is the whole point
  // of `conflict`: it must mean specifically "another save already landed",
  // not "some save request failed" -- the same reason useRetro.ts checks
  // `err.code === "RETRO_CHANGED"` rather than treating any mutation error
  // as a conflict.
  it("sets conflict when a save fails with 409 VISION_CHANGED", async () => {
    stubFetchRoutes({
      "GET /api/v1/marriage/vision?year=2026": {
        status: 200,
        body: { vision: visionFixture({ version: 3 }) },
      },
      "PUT /api/v1/marriage/vision/2026": {
        status: 409,
        body: {
          error: { code: "VISION_CHANGED", message: "This vision changed while you were editing it." },
        },
      },
    });

    const { result } = renderUseVision(2026);
    await waitFor(() => expect(result.current.data).toBeDefined());
    expect(result.current.conflict).toBe(false);

    await act(() =>
      result.current
        .saveVision({ theme: "mine", description: "", pillars: [], milestones: [] })
        .catch(() => {}),
    );

    expect(result.current.conflict).toBe(true);
  });

  it("does not set conflict when a save fails for another reason", async () => {
    stubFetchRoutes({
      "GET /api/v1/marriage/vision?year=2026": {
        status: 200,
        body: { vision: visionFixture({ version: 3 }) },
      },
      "PUT /api/v1/marriage/vision/2026": {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Something broke." } },
      },
    });

    const { result } = renderUseVision(2026);
    await waitFor(() => expect(result.current.data).toBeDefined());

    await act(() =>
      result.current
        .saveVision({ theme: "mine", description: "", pillars: [], milestones: [] })
        .catch(() => {}),
    );

    expect(result.current.conflict).toBe(false);
  });

  // This pins the hook's own behaviour only: conflict clears once a refetch
  // THIS HOOK triggers actually succeeds. It says nothing about what a
  // screen does with `conflict`/`reload` -- useRetro.ts's own header comment
  // records that the identical claim, written for retros ("the modal needs
  // this to clear its banner"), turned out to be false: RetroModal decides
  // conflict itself from `err.code` and never reads `conflict` or calls
  // `reload()`. Task 12 owns whether, and how, VisionModal uses these.
  it("reload clears conflict once the refetch resolves successfully", async () => {
    stubFetchRoutes({
      "GET /api/v1/marriage/vision?year=2026": [
        { status: 200, body: { vision: visionFixture({ version: 3 }) } },
        { status: 200, body: { vision: visionFixture({ version: 3 }) } },
      ],
      "PUT /api/v1/marriage/vision/2026": {
        status: 409,
        body: {
          error: { code: "VISION_CHANGED", message: "This vision changed while you were editing it." },
        },
      },
    });

    const { result } = renderUseVision(2026);
    await waitFor(() => expect(result.current.data).toBeDefined());

    await act(() =>
      result.current
        .saveVision({ theme: "mine", description: "", pillars: [], milestones: [] })
        .catch(() => {}),
    );
    expect(result.current.conflict).toBe(true);

    await act(async () => {
      await result.current.reload();
    });

    expect(result.current.conflict).toBe(false);
  });

  // A reload() that itself fails (offline, a 500) has shown the caller
  // nothing current, so there is nothing yet to call resolved -- conflict
  // must stay set rather than being cleared unconditionally the moment
  // reload() is merely called.
  it("reload leaves conflict set when the refetch itself fails", async () => {
    stubFetchRoutes({
      "GET /api/v1/marriage/vision?year=2026": [
        { status: 200, body: { vision: visionFixture({ version: 3 }) } },
        { status: 500, body: { error: { code: "INTERNAL", message: "Something broke." } } },
      ],
      "PUT /api/v1/marriage/vision/2026": {
        status: 409,
        body: {
          error: { code: "VISION_CHANGED", message: "This vision changed while you were editing it." },
        },
      },
    });

    const { result } = renderUseVision(2026);
    await waitFor(() => expect(result.current.data).toBeDefined());

    await act(() =>
      result.current
        .saveVision({ theme: "mine", description: "", pillars: [], milestones: [] })
        .catch(() => {}),
    );
    expect(result.current.conflict).toBe(true);

    await act(async () => {
      await result.current.reload();
    });

    expect(result.current.conflict).toBe(true);
  });

  // VisionPage owns `year` as its own state, and the Edit-vision modal's
  // year select changes it on this SAME mounted useVision instance rather
  // than remounting it -- a 409 while saving 2026, then switching to 2025,
  // must not leave 2025 looking conflicted when nothing happened there.
  it("resets conflict when year changes", async () => {
    stubFetchRoutes({
      "GET /api/v1/marriage/vision?year=2026": {
        status: 200,
        body: { vision: visionFixture({ year: 2026, version: 3 }) },
      },
      "GET /api/v1/marriage/vision?year=2025": {
        status: 200,
        body: { vision: visionFixture({ year: 2025, version: 1 }) },
      },
      "PUT /api/v1/marriage/vision/2026": {
        status: 409,
        body: {
          error: { code: "VISION_CHANGED", message: "This vision changed while you were editing it." },
        },
      },
    });

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result, rerender } = renderHook(({ year }: { year: number }) => useVision(year), {
      initialProps: { year: 2026 },
      wrapper: ({ children }) => createElement(QueryClientProvider, { client: queryClient }, children),
    });
    await waitFor(() => expect(result.current.data?.year).toBe(2026));

    await act(() =>
      result.current
        .saveVision({ theme: "mine", description: "", pillars: [], milestones: [] })
        .catch(() => {}),
    );
    expect(result.current.conflict).toBe(true);

    // Checked immediately after the rerender, BEFORE 2025's own fetch has
    // resolved -- not after waiting for `data` to settle on 2025. Once 2025's
    // fetch does resolve, its query.dataUpdatedAt is a later timestamp than
    // 2026's conflictAt by simple virtue of the clock moving forward, which
    // would clear the derived `conflict` flag on its own and hide a missing
    // reset. The real bug is the window right after the switch, while the
    // new year's query has no data of its own yet (dataUpdatedAt still 0) --
    // that is exactly where a stale conflictAt from 2026 would otherwise read
    // as true for 2025.
    act(() => {
      rerender({ year: 2025 });
    });
    expect(result.current.conflict).toBe(false);

    await waitFor(() => expect(result.current.data?.year).toBe(2025));
    expect(result.current.conflict).toBe(false);
  });
});
