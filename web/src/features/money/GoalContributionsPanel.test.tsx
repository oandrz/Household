// Follows GoalModal.test.tsx's conventions: renderWithRouter for a fresh
// QueryClient, stubFetchRoutes for every request (it throws on anything
// unregistered), fireEvent + findBy*/waitFor for the async gaps a real mount
// always has.
//
// GoalContributionsPanel calls `useGoals({ enabled: false })` for its two
// mutations only -- the same reason GoalModal.tsx's own header comment
// gives, and the same reason most tests below can stub the panel on its own
// with no baseline `GET /api/v1/goals` to register. The one exception is
// "moves the card's progress...": the panel only ever receives `goal` as a
// snapshot prop and renders nothing derived from `GET /goals` itself, so no
// panel-only render can prove a refetch actually reaches the card -- that
// test mounts `GoalsPage` for real, the same way GoalsPage.test.tsx's own
// "saving an edit closes the modal and the card shows the new figure" proves
// updateGoal's invalidation reaches a real mounted page rather than just
// asserting about `useGoals.ts` in isolation.
//
// "Today" is faked to 2026-08-15, GoalModal.test.tsx's own convention, so
// the date field's default is deterministic.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { GoalContributionsPanel } from "./GoalContributionsPanel";
import { GoalsPage } from "./GoalsPage";
import type { Goal, GoalContribution, GoalsResponse } from "./goalSchemas";

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-08-15T12:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

function goalFixture(overrides: Partial<Goal> = {}): Goal {
  return {
    id: "goal-1",
    name: "Bali family trip",
    targetMinor: 1000000,
    currency: "SGD",
    targetMonth: "2026-12",
    plannedMonthlyMinor: 200000,
    contributedMinor: 260000,
    percent: 65,
    status: "on_track",
    requiredMonthlyMinor: 200000,
    requiredMonthlyOk: true,
    archivedAt: null,
    ...overrides,
  };
}

function contributionFixture(overrides: Partial<GoalContribution> = {}): GoalContribution {
  return {
    id: "contrib-1",
    amountMinor: 40000,
    occurredOn: "2026-08-01",
    note: "Bonus",
    source: "manual",
    sourceBudgetMonth: null,
    ...overrides,
  };
}

function goalsResponse(goals: Goal[], summaryOverrides: Partial<GoalsResponse["summary"]> = {}): GoalsResponse {
  return {
    currency: "SGD",
    goals,
    summary: {
      plannedMonthlyTotalMinor: 0,
      actualThisMonthMinor: 0,
      onTrackCount: 0,
      datedCount: 0,
      noDateCount: 0,
      excludedNoRate: 0,
      nextGoal: null,
      ...summaryOverrides,
    },
  };
}

// GoalModal.test.tsx's own mutatingCalls, restated here for the same reason
// that file gives: only the writes matter for these assertions, and a
// request appearing at all beyond what a test registered would already
// throw via stubFetchRoutes.
function mutatingCalls(fetchMock: ReturnType<typeof stubFetchRoutes>): string[] {
  return fetchMock.mock.calls
    .filter(([, init]) => (init?.method ?? "GET").toUpperCase() !== "GET")
    .map(([input, init]) => `${(init?.method ?? "GET").toUpperCase()} ${String(input)}`);
}

function renderPanel(
  props: Partial<Parameters<typeof GoalContributionsPanel>[0]> = {},
  extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  const onClose = vi.fn();
  const fetchMock = stubFetchRoutes(extraRoutes);
  renderWithRouter(<GoalContributionsPanel goal={goalFixture()} onClose={onClose} {...props} />);
  return { fetchMock, onClose };
}

describe("GoalContributionsPanel", () => {
  it("defaults the date field to today", async () => {
    renderPanel(undefined, {
      "GET /api/v1/goals/goal-1/contributions": { status: 200, body: { contributions: [] } },
    });

    expect(await screen.findByLabelText("Date")).toHaveValue("2026-08-15");
  });

  it("posts the amount in minor units and the date as YYYY-MM-DD, then clears the form", async () => {
    let postBody: unknown;
    const { fetchMock } = renderPanel(undefined, {
      "GET /api/v1/goals/goal-1/contributions": [
        { status: 200, body: { contributions: [] } },
        {
          status: 200,
          body: {
            contributions: [contributionFixture({ id: "contrib-new", amountMinor: 40000, occurredOn: "2026-08-20" })],
          },
        },
      ],
      "POST /api/v1/goals/goal-1/contributions": {
        status: 201,
        body: { contribution: contributionFixture({ id: "contrib-new", amountMinor: 40000, occurredOn: "2026-08-20" }) },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    await screen.findByTestId("goal-contributions-empty");
    fireEvent.change(screen.getByLabelText("Amount"), { target: { value: "400.00" } });
    fireEvent.change(screen.getByLabelText("Date"), { target: { value: "2026-08-20" } });
    fireEvent.change(screen.getByLabelText("Note"), { target: { value: "Bonus" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    await waitFor(() => expect(postBody).toBeDefined());
    expect(postBody).toEqual({ amountMinor: 40000, occurredOn: "2026-08-20", note: "Bonus" });
    expect(mutatingCalls(fetchMock)).toEqual(["POST /api/v1/goals/goal-1/contributions"]);

    // The panel stays open (the brief's own "adding must move the card's
    // progress in the same interaction" only makes sense if this component
    // is still mounted to see its own list refetch) and the form clears,
    // ready for another entry -- not left holding the just-submitted values.
    expect(await screen.findByText("Bonus")).toBeInTheDocument();
    expect(screen.getByLabelText("Amount")).toHaveValue("");
    expect(screen.getByLabelText("Note")).toHaveValue("");
    expect(screen.getByLabelText("Date")).toHaveValue("2026-08-15");
  });

  // The spec's own decision: `amount_minor <> 0`, not `> 0` -- a mistyped
  // contribution is corrected by a negative row, so this must refuse exactly
  // zero and nothing else. No POST route is registered at all: if the
  // zero-guard failed to short-circuit, stubFetchRoutes' own
  // throw-on-unregistered-route would fail this test for us.
  it("a zero amount shows the inline error and fires no request", async () => {
    const { fetchMock } = renderPanel(undefined, {
      "GET /api/v1/goals/goal-1/contributions": { status: 200, body: { contributions: [] } },
    });

    await screen.findByTestId("goal-contributions-empty");
    fireEvent.change(screen.getByLabelText("Amount"), { target: { value: "0.00" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Enter an amount other than zero.");
    expect(mutatingCalls(fetchMock)).toEqual([]);
  });

  // The other half of the same spec decision: a negative amount is a
  // legitimate correction row, not a second thing this form refuses --
  // guarding against `amountMinor <= 0` here (the wrong copy-paste from
  // GoalModal.tsx's own target-amount check, which really does mean
  // "positive only") would silently block every correction the API accepts.
  it("accepts a negative amount as a correction, posting it unchanged", async () => {
    let postBody: unknown;
    const { fetchMock } = renderPanel(undefined, {
      "GET /api/v1/goals/goal-1/contributions": { status: 200, body: { contributions: [] } },
      "POST /api/v1/goals/goal-1/contributions": {
        status: 201,
        body: { contribution: contributionFixture({ amountMinor: -5000 }) },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    await screen.findByTestId("goal-contributions-empty");
    fireEvent.change(screen.getByLabelText("Amount"), { target: { value: "-50.00" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    await waitFor(() => expect(postBody).toBeDefined());
    expect(postBody).toMatchObject({ amountMinor: -5000 });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(mutatingCalls(fetchMock)).toEqual(["POST /api/v1/goals/goal-1/contributions"]);
  });

  // The three source labels, composed in goalCopy.ts's own
  // contributionSourceLabel: a manual row's own note (falling back to a
  // generic label when that note is blank, since the field is optional and
  // an empty line would read as a rendering bug), the fixed "Starting
  // balance" phrase, and the rollover sentence naming the month from
  // sourceBudgetMonth -- never parsed out of a note, which the server
  // deliberately leaves empty on that row (deviation 3).
  it("labels each row by its source: the note, 'Starting balance', or the rollover's month", async () => {
    const contributions = [
      contributionFixture({ id: "c1", source: "manual", note: "Grocery savings" }),
      contributionFixture({ id: "c2", source: "manual", note: "" }),
      contributionFixture({ id: "c3", source: "starting_balance", note: "" }),
      contributionFixture({ id: "c4", source: "budget_rollover", note: "", sourceBudgetMonth: "2026-07" }),
    ];
    renderPanel(undefined, {
      "GET /api/v1/goals/goal-1/contributions": { status: 200, body: { contributions } },
    });

    expect(await screen.findByText("Grocery savings")).toBeInTheDocument();
    expect(screen.getByText("Manual contribution")).toBeInTheDocument();
    expect(screen.getByText("Starting balance")).toBeInTheDocument();
    expect(screen.getByText("From July's unspent budget")).toBeInTheDocument();
  });

  // goalContributionSchema's z.enum refuses an unrecognised `source` one
  // layer up -- the whole GET fails to parse, and this is the resulting
  // state the panel actually renders for it: an inline error, never a
  // crash. contributionSourceLabel's own default case (goalCopy.ts) is a
  // second, belt-and-suspenders fallback below this one, for a source value
  // that reaches it some other way (a looser schema later, a cast); this
  // test pins the layer that is actually reachable through a real response.
  it("shows an inline error rather than crashing when a contribution's source fails to parse", async () => {
    renderPanel(undefined, {
      "GET /api/v1/goals/goal-1/contributions": {
        status: 200,
        body: { contributions: [{ ...contributionFixture(), source: "unknown_source" }] },
      },
    });

    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't load contributions.");
  });

  // Delete asks first (in-page, never window.confirm -- there is nothing
  // here for a test to drive if it were a real browser dialog), then
  // DELETEs, then refetches -- and cancelling never fires a request at all.
  it("delete asks first, then DELETEs, then refetches the list", async () => {
    const { fetchMock } = renderPanel(undefined, {
      "GET /api/v1/goals/goal-1/contributions": [
        { status: 200, body: { contributions: [contributionFixture({ id: "contrib-1", note: "Bonus" })] } },
        { status: 200, body: { contributions: [] } },
      ],
      "DELETE /api/v1/goals/goal-1/contributions/contrib-1": { status: 204, body: undefined },
    });

    const row = await screen.findByTestId("goal-contribution-row");
    fireEvent.click(within(row).getByRole("button", { name: "Delete" }));

    expect(screen.getByText("Delete this contribution? This can't be undone.")).toBeInTheDocument();
    expect(mutatingCalls(fetchMock)).toEqual([]);

    fireEvent.click(within(row).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByText("Delete this contribution? This can't be undone.")).not.toBeInTheDocument();
    expect(mutatingCalls(fetchMock)).toEqual([]);

    fireEvent.click(within(row).getByRole("button", { name: "Delete" }));
    fireEvent.click(within(row).getByRole("button", { name: "Yes, delete" }));

    await waitFor(() =>
      expect(mutatingCalls(fetchMock)).toEqual(["DELETE /api/v1/goals/goal-1/contributions/contrib-1"]),
    );
    await screen.findByTestId("goal-contributions-empty");
  });

  // A 404 -- the row already gone in another tab -- must surface inline, not
  // read as a quiet success: no optimistic removal happens anywhere in this
  // component, so the only way this could look like it "worked" is if the
  // error were silently swallowed.
  it("a delete that 404s surfaces the error inline rather than silently succeeding", async () => {
    const { fetchMock } = renderPanel(undefined, {
      "GET /api/v1/goals/goal-1/contributions": {
        status: 200,
        body: { contributions: [contributionFixture({ id: "contrib-1", note: "Bonus" })] },
      },
      "DELETE /api/v1/goals/goal-1/contributions/contrib-1": {
        status: 404,
        body: { error: { code: "NOT_FOUND", message: "That contribution could not be found." } },
      },
    });

    const row = await screen.findByTestId("goal-contribution-row");
    fireEvent.click(within(row).getByRole("button", { name: "Delete" }));
    fireEvent.click(within(row).getByRole("button", { name: "Yes, delete" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("That contribution could not be found.");
    // Still on screen -- a 404 never quietly removes the row from view.
    expect(screen.getByText("Bonus")).toBeInTheDocument();
    expect(mutatingCalls(fetchMock)).toEqual(["DELETE /api/v1/goals/goal-1/contributions/contrib-1"]);
  });
});

describe("GoalCard's Add contribution control", () => {
  it("opens the contributions panel, not the edit modal, and stops the click from bubbling to the card", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/goals": { status: 200, body: goalsResponse([goalFixture()], { onTrackCount: 1, datedCount: 1 }) },
      "GET /api/v1/goals/goal-1/contributions": { status: 200, body: { contributions: [] } },
    });
    renderWithRouter(<GoalsPage />);

    const card = await screen.findByTestId("goal-card");
    fireEvent.click(within(card).getByRole("button", { name: "Add contribution" }));

    expect(await screen.findByLabelText("Amount")).toBeInTheDocument();
    // The edit modal (GoalModal, Task 12's own click-to-edit seam) never
    // opened -- if the click had bubbled to the card's own onClick, this
    // would be present too.
    expect(screen.queryByLabelText("Goal name")).not.toBeInTheDocument();
  });

  // GoalCard's container also has an onKeyDown (Enter/Space -> onEdit).
  // Keydown is a distinct event from click and bubbles independently of it
  // -- stopping only the button's own onClick would leave this path open, a
  // keyboard user's Enter on this control silently opening the edit modal
  // behind it instead of the contributions panel.
  it("does not let a keydown on the control bubble into the card's own onKeyDown", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/goals": { status: 200, body: goalsResponse([goalFixture()], { onTrackCount: 1, datedCount: 1 }) },
    });
    renderWithRouter(<GoalsPage />);

    const card = await screen.findByTestId("goal-card");
    const addButton = within(card).getByRole("button", { name: "Add contribution" });
    fireEvent.keyDown(addButton, { key: "Enter", code: "Enter" });

    expect(screen.queryByLabelText("Goal name")).not.toBeInTheDocument();
  });

  it("is absent from an archived card", async () => {
    const archived = goalFixture({
      archivedAt: "2026-06-01T00:00:00Z",
      status: "none",
      targetMonth: null,
      requiredMonthlyOk: false,
      requiredMonthlyMinor: 0,
    });
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/goals": { status: 200, body: goalsResponse([]) },
      "GET /api/v1/goals?include_archived=true": { status: 200, body: goalsResponse([archived]) },
    });
    renderWithRouter(<GoalsPage />);

    await screen.findByTestId("goals-empty-state");
    fireEvent.click(screen.getByRole("switch", { name: "Show archived" }));

    const card = await screen.findByTestId("goal-card");
    expect(within(card).queryByRole("button", { name: "Add contribution" })).not.toBeInTheDocument();
  });

  // The coordinator's own instruction, restated for this task: verify rather
  // than assume that adding a contribution moves the card's own figures.
  // Submits an amount that is deliberately NOT the delta between the two
  // stubbed /goals responses (65% -> 75%, a 40,000-minor-unit change) --
  // asserting against a different, mismatched submitted amount is what rules
  // out the card echoing what was typed instead of trusting the refetch.
  it("moves the card's progress via the /goals refetch, not the amount just typed", async () => {
    const before = goalFixture({ contributedMinor: 260000, percent: 65 });
    const after = goalFixture({ contributedMinor: 300000, percent: 75 });

    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/goals": [
        { status: 200, body: goalsResponse([before], { onTrackCount: 1, datedCount: 1 }) },
        { status: 200, body: goalsResponse([after], { onTrackCount: 1, datedCount: 1 }) },
      ],
      "GET /api/v1/goals/goal-1/contributions": { status: 200, body: { contributions: [] } },
      "POST /api/v1/goals/goal-1/contributions": {
        status: 201,
        // 123.45 -- unrelated to the 400.00 delta the two /goals responses
        // above actually differ by.
        body: { contribution: contributionFixture({ amountMinor: 12345 }) },
      },
    });
    renderWithRouter(<GoalsPage />);

    const card = await screen.findByTestId("goal-card");
    expect(card).toHaveTextContent("65%");

    fireEvent.click(within(card).getByRole("button", { name: "Add contribution" }));
    fireEvent.change(await screen.findByLabelText("Amount"), { target: { value: "123.45" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    await waitFor(() => expect(screen.getByTestId("goal-card")).toHaveTextContent("75%"));
    expect(screen.getByTestId("goal-card")).toHaveTextContent("S$3,000.00 of S$10,000.00");
  });
});
