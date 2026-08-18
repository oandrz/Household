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
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
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
    // Not asserted on anywhere in this file -- present only because
    // retroSummarySchema requires it on the wire (RetroHistoryList.test.tsx's
    // own fixture comment has the reason this component reads actionCount,
    // not this field).
    openActionCount: 1,
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

// RetroModal.tsx (Task 13) mounts MoneyCheckInPanel (Task 14) the moment it
// opens, which fetches both of these unconditionally -- both tests below
// that open the modal need them registered, or stubFetchRoutes throws
// before any capture runs (Task 13's own finding, restated in Task 14's
// brief). Neither test cares what the money panel shows, so both bodies are
// the empty state -- RetroModal.test.tsx/MoneyCheckInPanel.test.tsx own the
// populated-figures assertions.
const NO_BUDGET_FOR = (month: string) => ({
  currency: "SGD",
  month,
  budget: null,
  categories: [],
  budgetedMinor: 0,
  spentMinor: 0,
  remainingMinor: 0,
  percentUsed: 0,
  percentOk: false,
  daysLeft: 0,
  dailyPaceMinor: 0,
  dailyPaceOk: false,
  byPerson: [],
  excludedNoRate: 0,
  overCount: 0,
  rolledOverAt: null,
  rolloverGoalId: null,
  rolloverAmountMinor: null,
});

// useBudget.ts's own second, previous-month query: it fires whenever the
// CURRENT month's `budget` resolves to null, which NO_BUDGET_FOR always
// does -- both tests below need this month's route registered too, or
// stubFetchRoutes throws before any capture runs. Mirrors useBudget.ts's
// own `previousMonth` helper exactly.
function previousMonth(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  const shifted = new Date(year, monthNum - 2, 1);
  return `${shifted.getFullYear()}-${String(shifted.getMonth() + 1).padStart(2, "0")}`;
}

const EMPTY_GOALS = {
  currency: "SGD",
  goals: [],
  summary: {
    plannedMonthlyTotalMinor: 0,
    actualThisMonthMinor: 0,
    onTrackCount: 0,
    datedCount: 0,
    noDateCount: 0,
    excludedNoRate: 0,
    nextGoal: null,
  },
};

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

    // retro-row-2026-08: RetroHistoryList.tsx's own per-month testid (Task
    // 11) -- the generic retro-draft-row/retro-summary-row pair Task 10's
    // stand-in used couldn't stay unique once a real list can hold many rows
    // for the same year (RetroHistoryList.test.tsx's own disclosure test).
    const draftRow = await screen.findByTestId("retro-row-2026-08");
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
    // A draft's row never shows the finished clauses -- there is no separate
    // "summary row" element to tell it apart from any more; the row's own
    // content is the only signal.
    expect(draftRow).not.toHaveTextContent("Mood");
    expect(draftRow).not.toHaveTextContent("action");
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

  // doneSinceClause (retroCopy.ts) checks `doneCount <= 0` and `!since`
  // separately -- every other fixture in this file that carries
  // `doneCount: 0` also carries `since: null`, which means the `!since` half
  // alone would keep every other test green even with the `doneCount <= 0`
  // half deleted. retroSummarySchema/retrosResponseSchema don't tie the two
  // fields together (a schema-legal response can carry doneCount: 0 with a
  // real since, if the service ever miscounts), so this fixture supplies
  // exactly that pair -- the one case that discriminates the two halves of
  // the guard. The design is explicit that the phrase is omitted entirely
  // rather than shown as "0 done" (task-10-brief.md), so this asserts the
  // subtitle's exact text, not merely that a substring is present.
  it("omits the done-count clause when doneCount is 0, even if since is present", async () => {
    renderPage(
      retrosFixture({
        retros: [],
        doneCount: 0,
        since: "2026-06",
        startMonth: "2026-08",
      }),
    );

    const subtitle = await screen.findByTestId("retros-subtitle");
    expect(subtitle.textContent).toBe("Monthly check-in, just the two of us");
  });

  // The header renders on every state but owner-only/load-error, and
  // nothing above asserts either the title or the privacy badge -- both
  // could be deleted and every other test in this file would stay green.
  // The badge in particular carries the page's only statement that this
  // space is private; it is the one element on the page that had no
  // data-testid until this test needed one.
  it("renders the page title and the privacy badge", async () => {
    renderPage(retrosFixture({ retros: [], doneCount: 0, since: null, startMonth: "2026-08" }));

    expect(await screen.findByRole("heading", { level: 1, name: "Marriage retros" })).toBeInTheDocument();
    expect(screen.getByTestId("retros-privacy-badge")).toHaveTextContent("Private — parents only");
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

    await screen.findByTestId("retro-row-2026-06");
    expect(screen.queryByTestId("retros-start")).not.toBeInTheDocument();
    expect(screen.queryByTestId("retros-create-first")).not.toBeInTheDocument();
  });

  // State 3: normal -- history, chart and detail mount points. History and
  // the mood chart are real components as of task 11 (RetroHistoryList,
  // MoodChart); selecting a row's own RetroDetail render is proven by the
  // separate test below, once a month is actually clicked.
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

    const row = await screen.findByTestId("retro-row-2026-06");
    // The design's own row label carries the year (`dc.html`: "June 2026",
    // "May 2026", ...) -- 13+ months of history repeats month names across
    // years, and this row is real behaviour this task ships, not a Task-11
    // placeholder standing in for it.
    expect(row).toHaveTextContent("June 2026");
    expect(row).toHaveTextContent("Mood 4/5");
    expect(row).toHaveTextContent("3 actions");
    expect(row).toHaveTextContent("best month this year");
    // Asserts something MoodChart itself renders *inside* the mount point,
    // not merely that the (Task-10-era) wrapper div is present -- a wrapper
    // div survives with or without a <MoodChart .../> line inside it, which
    // is exactly the Goals-archive shape (docs/LEARNING.md pattern 15):
    // every layer built, a passing component test, and nothing here proving
    // the page actually calls it. The fixture's own `mood` entry above has a
    // real point, so the chart renders its `role="img"` svg, not the empty
    // state.
    const chartMount = screen.getByTestId("retro-mood-chart-mount");
    expect(within(chartMount).getByRole("img")).toBeInTheDocument();
    expect(screen.getByTestId("retro-detail-mount")).toBeInTheDocument();
  });

  // Pins the wiring itself, not just RetroDetail.tsx's own component test
  // (RetroDetail.test.tsx). docs/LEARNING.md pattern 15 -- MoodChart's own
  // <MoodChart .../> render line could have been deleted with the whole
  // suite staying green, because nothing asserted content MoodChart itself
  // produces from *inside* RetrosPage's mount point. This clicks a real row
  // and asserts something RetroDetail itself renders (its own heading)
  // inside retro-detail-mount, so deleting `<RetroDetail month=.../>` from
  // RetrosPage.tsx turns this test red, not just RetroDetail.test.tsx's own
  // (which never mounts RetrosPage at all).
  it("selecting a history row renders that month's real detail in the mount point", async () => {
    renderPage(
      retrosFixture({
        retros: [summaryFixture({ id: "retro-june", month: "2026-06", finished: true })],
        mood: [],
        doneCount: 1,
        since: "2026-06",
        startMonth: "2026-07",
      }),
      {
        "GET /api/v1/retros/2026-06": {
          status: 200,
          body: {
            retro: {
              id: "retro-june",
              month: "2026-06",
              mood: 4,
              wentWell: "",
              wasHard: "",
              notes: "",
              completedAt: null,
              version: 1,
              actions: [],
            },
            carryOver: [],
          },
        },
        "GET /api/v1/household/members": { status: 200, body: [] },
      },
    );

    fireEvent.click(await screen.findByTestId("retro-row-2026-06"));

    const mount = screen.getByTestId("retro-detail-mount");
    expect(await within(mount).findByTestId("retro-detail-heading")).toHaveTextContent("June 2026 retro");
    // The Task-10-era placeholder must be gone once a real detail is
    // showing -- both on screen at once would mean the mount point renders
    // both branches rather than choosing between them.
    expect(within(mount).queryByText("Select a retro to see its detail.")).not.toBeInTheDocument();
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
      // handleStart's own onSuccess opens RetroModal(created.month)
      // immediately (Task 13) -- this is that modal's own GET, not a second
      // fetch of the row above.
      "GET /api/v1/retros/2026-08": { status: 200, body: { retro: createdRetroFixture, carryOver: [] } },
      "GET /api/v1/budgets/2026-08": { status: 200, body: NO_BUDGET_FOR("2026-08") },
      // NO_BUDGET_FOR's own `budget: null` enables useBudget.ts's second,
      // previous-month query -- see `previousMonth`'s own comment above.
      [`GET /api/v1/budgets/${previousMonth("2026-08")}`]: {
        status: 200,
        body: NO_BUDGET_FOR(previousMonth("2026-08")),
      },
      "GET /api/v1/goals": { status: 200, body: EMPTY_GOALS },
      // RetroModal's add-action composer (fix round) calls
      // useHouseholdMembers() unconditionally on mount too.
      "GET /api/v1/household/members": { status: 200, body: [] },
    });

    fireEvent.click(await screen.findByTestId("retros-start"));

    await waitFor(() => expect(posted).toBe(true));
    expect(await screen.findByTestId("retro-row-2026-08")).toHaveTextContent("In progress");
    // Pins RetroModal's own wiring (docs/LEARNING.md pattern 15, the same
    // reasoning the row assertion above already carries for the list): this
    // asserts something the modal itself renders once open, not merely that
    // startRetro's own network call happened. Deleting the
    // `<RetroModal .../>` render line from RetrosPage.tsx must turn this
    // red, not just RetroModal.test.tsx's own suite (which never mounts
    // RetrosPage at all). Scoped inside the dialog: handleStart also selects
    // the new month, so RetroDetail behind the modal renders its own
    // "August 2026 retro" heading at the same time -- an unscoped query
    // would match both and throw on the ambiguity, which is not what this
    // assertion is about.
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading", { level: 2, name: "August 2026 retro" })).toBeInTheDocument();
  });

  // Edit reopens the same modal against whichever month is currently
  // selected -- the other entry point RetroModal.tsx's own header comment
  // names (Start's own onSuccess is the first, covered above).
  it("clicking Edit on a selected retro opens its modal", async () => {
    renderPage(
      retrosFixture({
        retros: [summaryFixture({ id: "retro-june", month: "2026-06", finished: true })],
        mood: [],
        doneCount: 1,
        since: "2026-06",
        startMonth: "2026-07",
      }),
      {
        "GET /api/v1/retros/2026-06": {
          status: 200,
          body: {
            retro: {
              id: "retro-june",
              month: "2026-06",
              mood: 4,
              wentWell: "",
              wasHard: "",
              notes: "",
              completedAt: null,
              version: 1,
              actions: [],
            },
            carryOver: [],
          },
        },
        "GET /api/v1/household/members": { status: 200, body: [] },
        "GET /api/v1/budgets/2026-06": { status: 200, body: NO_BUDGET_FOR("2026-06") },
        // NO_BUDGET_FOR's own `budget: null` enables useBudget.ts's second,
        // previous-month query -- see `previousMonth`'s own comment above.
        [`GET /api/v1/budgets/${previousMonth("2026-06")}`]: {
          status: 200,
          body: NO_BUDGET_FOR(previousMonth("2026-06")),
        },
        "GET /api/v1/goals": { status: 200, body: EMPTY_GOALS },
      },
    );

    fireEvent.click(await screen.findByTestId("retro-row-2026-06"));
    fireEvent.click(await screen.findByTestId("retro-edit"));

    // Scoped inside the dialog for the same reason as the Start test above
    // -- RetroDetail's own "June 2026 retro" heading is already on screen
    // behind the modal.
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading", { level: 2, name: "June 2026 retro" })).toBeInTheDocument();
  });

  // Discard draft (design decision 2, docs/LEARNING.md pattern 15's fourth
  // instance in this feature): DeleteDraft/DELETE /retros/{month}/
  // useRetro.discardDraft were all real and tested with no component
  // anywhere calling any of it. This is the page-level half of closing that
  // gap -- RetroModal.test.tsx's own suite proves the modal posts the
  // DELETE and closes itself; this proves the REST of the page reacts to
  // it too: the draft row leaves the history list and the Start button
  // (which only renders when `startMonth` is non-null) comes back, the same
  // "clicking Start" test's own before/after GET /retros refetch shape.
  it("discarding a draft removes it from the history and restores the Start button", async () => {
    const draftRetro: Retro = {
      id: "retro-draft",
      month: "2026-08",
      mood: null,
      wentWell: "",
      wasHard: "",
      notes: "",
      completedAt: null,
      version: 1,
      actions: [],
    };
    // A second, unrelated finished retro stays present in both `before` and
    // `after` -- discarding August must not empty the list entirely, or the
    // page renders its own "no retros yet" branch instead of the two-column
    // grid RetroDetail lives in, which is exactly the branch this test needs
    // to stay in to prove `selectedMonth` actually cleared.
    const july = summaryFixture({ id: "retro-july", month: "2026-07", finished: true });
    // August already has a draft, so no Start button shows yet (startMonth
    // null, the same guard "hides the start button when both months already
    // have a retro" pins) -- discarding it is what frees the month back up.
    const before = retrosFixture({
      retros: [summaryFixture({ id: "retro-draft", month: "2026-08", finished: false, mood: null, quote: "" }), july],
      doneCount: 1,
      since: "2026-07",
      startMonth: null,
    });
    const after = retrosFixture({ retros: [july], doneCount: 1, since: "2026-07", startMonth: "2026-08" });

    renderPage(before, {
      "GET /api/v1/retros": [
        { status: 200, body: before },
        { status: 200, body: after },
      ],
      "GET /api/v1/retros/2026-08": { status: 200, body: { retro: draftRetro, carryOver: [] } },
      "GET /api/v1/budgets/2026-08": { status: 200, body: NO_BUDGET_FOR("2026-08") },
      [`GET /api/v1/budgets/${previousMonth("2026-08")}`]: {
        status: 200,
        body: NO_BUDGET_FOR(previousMonth("2026-08")),
      },
      "GET /api/v1/goals": { status: 200, body: EMPTY_GOALS },
      "GET /api/v1/household/members": { status: 200, body: [] },
      "DELETE /api/v1/retros/2026-08": { status: 204, body: undefined },
    });

    fireEvent.click(await screen.findByTestId("retro-row-2026-08"));
    fireEvent.click(await screen.findByTestId("retro-edit"));

    fireEvent.click(await screen.findByRole("button", { name: "Discard draft" }));
    fireEvent.click(await screen.findByRole("button", { name: "Yes, discard it" }));

    // The modal closes on success (RetroModal.tsx's own handleDiscardDraft),
    // the list refetches (afterWrite's own retroListQueryKey invalidation),
    // and both halves of "the page reflects the retro being gone" hold: the
    // row is gone, and the Start button -- absent before, since August
    // already had this draft -- is back.
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(screen.queryByTestId("retro-row-2026-08")).not.toBeInTheDocument();
    expect(await screen.findByTestId("retros-start")).toBeInTheDocument();
    // A real browser walk against this exact flow found the gap this pins:
    // closing the modal alone left `selectedMonth` still pointed at August,
    // and RetroDetail.tsx (still mounted in the detail panel) refetched a
    // month that no longer exists, rendering "Couldn't load this retro."
    // right after a delete that had actually succeeded. `onDiscarded`
    // (RetroModal.tsx) clears the selection too, so the panel falls back to
    // its own placeholder instead.
    expect(screen.queryByTestId("retro-detail-load-error")).not.toBeInTheDocument();
    expect(screen.getByText("Select a retro to see its detail.")).toBeInTheDocument();
  });
});
