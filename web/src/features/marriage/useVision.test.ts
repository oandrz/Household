import { createElement } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { useVision } from "./useVision";
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
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.error).toBeNull();
    expect(result.current.vision?.version).toBe(0);
    expect(result.current.vision?.theme).toBe("");
  });

  // A hook that let a caller pass version could send a stale one by
  // accident -- the version on the wire must always be the one this hook
  // loaded, never one a caller supplies. Same mutation check
  // useRetros.test.ts makes of saveRetro's own version guard.
  it("sends the version it loaded, never one the caller supplies", async () => {
    let putBody: unknown;
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
    await waitFor(() => expect(result.current.vision?.version).toBe(4));

    await act(() =>
      result.current.saveVision({
        theme: "Slow down together",
        description: "Fewer commitments.",
        pillars: [],
        milestones: [],
      }),
    );

    expect(putBody).toMatchObject({ version: 4 });
    // Proves saveVision invalidates and awaits the refetch, not just fires
    // the PUT -- the same reason useRetro.ts's own writes await afterWrite.
    await waitFor(() => expect(result.current.vision?.version).toBe(5));
  });

  it("sends version 0 for a year that had none, so the save is a create", async () => {
    let putBody: unknown;
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
    await waitFor(() => expect(result.current.vision?.version).toBe(0));

    await act(() => result.current.saveVision({ theme: "T", description: "", pillars: [], milestones: [] }));

    expect(putBody).toMatchObject({ version: 0 });
    await waitFor(() => expect(result.current.vision?.version).toBe(1));
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
    expect(result.current.vision).toBeUndefined();
  });
});
