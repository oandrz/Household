// Follows GoalsPage.test.tsx's own shape: renderWithRouter plus
// stubFetchRoutes for every request, literal strings asserted throughout
// (not RETRO_COPY's own exports -- importing the copy module here would make
// an assertion tautological against a typo in that same module).
//
// The five states pinned here are the task-10 brief's own list, and two of
// them -- owner-only (4) and load-error (5) -- are the pair that has shipped
// wrong three times already in this codebase (Bills, Budget, Transactions;
// docs/LEARNING.md pattern 1's own entry), which is why both get their own
// two-test pair here exactly as GoalsPage.test.tsx does, plus the mutation
// check in this task's own report.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { RetrosPage } from "./RetrosPage";
import type { Retro, RetroSummary, RetrosResponse } from "./retroSchemas";

function summaryFixture(overrides: Partial<RetroSummary> = {}): RetroSummary {
  return {
    id: "retro-1",
    month: "2026-06",
    mood: 4,
    actionCount: 3,
    quote: "best month this year",
    finished: true,
    ...overrides,
  };
}

function retrosFixture(overrides: Partial<RetrosResponse> = {}): RetrosResponse {
  return {
    retros: [],
    mood: [],
    doneCount: 0,
    since: null,
    startMonth: null,
    ...overrides,
  };
}

function renderPage(
  response: RetrosResponse,
  extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  const fetchMock = stubFetchRoutes({
    "GET /api/v1/retros": { status: 200, body: response },
    ...extraRoutes,
  });
  return { fetchMock, ...renderWithRouter(<RetrosPage />) };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("RetrosPage", () => {
  // State 1: no retros ever.
  it("a household with no retros is invited to start its first", async () => {
    renderPage(retrosFixture({ retros: [], doneCount: 0, since: null, startMonth: "2026-08" }));

    const empty = await screen.findByTestId("retros-empty-state");
    expect(empty).toHaveTextContent("No retros yet");
    expect(screen.getByTestId("retros-create-first")).toHaveTextContent("Start your first retro");
    // The header's own button, distinct copy from the empty state's --
    // BillsPage.tsx's "+ Add bill"/"Create your first bill" precedent for
    // why both render at once with different labels rather than one being
    // suppressed. Its label comes from startMonth ("2026-08" -> "August").
    expect(screen.getByTestId("retros-start")).toHaveTextContent("Start August retro");
  });

  // State 2: a draft exists.
  it("a draft shows as in progress and is not counted as done", async () => {
    renderPage(
      retrosFixture({
        retros: [summaryFixture({ id: "retro-draft", month: "2026-08", finished: false })],
        doneCount: 0,
      }),
    );

    const draftRow = await screen.findByTestId("retro-draft-row");
    expect(draftRow).toHaveTextContent("In progress");
    // The design's own row label carries the year ("August 2026", not
    // "August") -- 13+ months of history repeats month names across years,
    // and this row is real behaviour this task ships, not a Task-11
    // placeholder.
    expect(draftRow).toHaveTextContent("August 2026");
    // Guards against computing the done count from data.retros.length (1
    // here) instead of the server's own doneCount (0) -- a draft must never
    // read as done just because it is the only row in the array.
    expect(screen.queryByText(/1 done/)).not.toBeInTheDocument();
    expect(screen.queryByTestId("retro-summary-row")).not.toBeInTheDocument();
  });

  // The done-count clause's own literal text, from a fixture where it
  // should actually render -- without this, a subtitle condition that never
  // fires (state 1 and state 2's fixtures both carry doneCount: 0) would
  // leave every other test in this file green while the clause never once
  // appeared on screen (docs/LEARNING.md pattern 2: an absence assertion
  // holds perfectly over a blank page).
  it("renders the done-count clause once there are finished retros", async () => {
    renderPage(
      retrosFixture({
        retros: [summaryFixture({ finished: true })],
        doneCount: 12,
        since: "2025-08",
        startMonth: "2026-08",
      }),
    );

    expect(await screen.findByTestId("retros-subtitle")).toHaveTextContent(
      "Monthly check-in, just the two of us · 12 done since Aug 2025",
    );
  });

  // State 4: not an owner. The interim Overview's one real defect was a page
  // that rendered nothing at all for exactly this member (docs/LEARNING.md
  // pattern 2) -- every unit test passed because each one asserted the
  // absence of something, and absence holds perfectly over a blank page.
  // This asserts the explanation's *presence*, not merely that the generic
  // error is missing.
  it("a limited member is told this is owner-only, not that something broke", async () => {
    stubFetchRoutes({
      "GET /api/v1/retros": { status: 403, body: { error: { code: "FORBIDDEN", message: "Owner only." } } },
    });
    renderWithRouter(<RetrosPage />);

    const explanation = await screen.findByTestId("retros-owner-only");
    expect(explanation).toHaveTextContent("Owner only");
    expect(explanation).toHaveTextContent("Retros are visible to the household owner.");
    expect(screen.queryByTestId("retros-load-error")).not.toBeInTheDocument();
  });

  // State 5: load failure -- kept genuinely distinct from state 4 above by
  // asserting each one's absence in the other's test, not merely its own
  // presence.
  it("a non-403 failure renders the generic load error, not the owner-only explanation", async () => {
    stubFetchRoutes({
      "GET /api/v1/retros": { status: 500, body: { error: { code: "INTERNAL", message: "Something broke." } } },
    });
    renderWithRouter(<RetrosPage />);

    expect(await screen.findByTestId("retros-load-error")).toHaveTextContent("Couldn't load your retros.");
    expect(screen.queryByTestId("retros-owner-only")).not.toBeInTheDocument();
  });

  it("hides the start button when both months already have a retro", async () => {
    renderPage(
      retrosFixture({
        retros: [summaryFixture({ finished: true })],
        doneCount: 1,
        since: "2026-06",
        startMonth: null,
      }),
    );

    await screen.findByTestId("retro-summary-row");
    expect(screen.queryByTestId("retros-start")).not.toBeInTheDocument();
    expect(screen.queryByTestId("retros-create-first")).not.toBeInTheDocument();
  });

  // State 3: normal -- history, chart and detail mount points. The
  // components themselves are Tasks 11-12; this pins that this task's own
  // stand-ins for them are actually present, at the testids those tasks will
  // mount against.
  it("renders history, mood-chart and detail mount points for a household with real history", async () => {
    renderPage(
      retrosFixture({
        retros: [
          summaryFixture({ id: "retro-june", month: "2026-06", finished: true, quote: "best month this year" }),
        ],
        mood: [{ month: "2026-06", mood: 4 }],
        doneCount: 1,
        since: "2026-06",
        startMonth: "2026-07",
      }),
    );

    const row = await screen.findByTestId("retro-summary-row");
    // The design's own row label carries the year (`dc.html`: "June 2026",
    // "May 2026", ...) -- 13+ months of history repeats month names across
    // years, and this row is real behaviour this task ships, not a Task-11
    // placeholder standing in for it.
    expect(row).toHaveTextContent("June 2026");
    expect(row).toHaveTextContent("Mood 4/5");
    expect(row).toHaveTextContent("3 actions");
    expect(row).toHaveTextContent("best month this year");
    expect(screen.getByTestId("retro-mood-chart-mount")).toBeInTheDocument();
    expect(screen.getByTestId("retro-detail-mount")).toBeInTheDocument();
  });

  // The Goals-archive dead end (docs/LEARNING.md pattern 15): a button
  // labelled from real data that does nothing on click is the same shape of
  // gap, just for a Start button instead of an Archive one. This proves
  // clicking it actually reaches the network and the new draft lands back on
  // screen, not merely that the handler exists.
  it("clicking Start posts a new retro and shows it as a draft once the list refetches", async () => {
    let posted = false;
    const before = retrosFixture({ retros: [], doneCount: 0, since: null, startMonth: "2026-08" });
    const after = retrosFixture({
      retros: [summaryFixture({ id: "retro-new", month: "2026-08", finished: false })],
      doneCount: 0,
      since: null,
      startMonth: null,
    });
    // POST /retros' own response shape (retroWriteResponseSchema): the whole
    // retro, not the summary row -- useRetros.ts's startRetro parses this
    // through retroWriteResponseSchema, distinct from the retros[] array's
    // own retroSummarySchema shape used above.
    const createdRetroFixture: Retro = {
      id: "retro-new",
      month: "2026-08",
      mood: null,
      wentWell: "",
      wasHard: "",
      notes: "",
      completedAt: null,
      version: 1,
      actions: [],
    };

    renderPage(before, {
      "GET /api/v1/retros": [
        { status: 200, body: before },
        { status: 200, body: after },
      ],
      "POST /api/v1/retros": {
        status: 201,
        body: { retro: createdRetroFixture },
        capture: () => {
          posted = true;
        },
      },
    });

    fireEvent.click(await screen.findByTestId("retros-start"));

    await waitFor(() => expect(posted).toBe(true));
    expect(await screen.findByTestId("retro-draft-row")).toHaveTextContent("In progress");
  });
});
