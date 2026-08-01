// Follows BudgetModal.test.tsx/AccountModal.test.tsx's conventions:
// renderWithRouter for a fresh QueryClient, stubFetchRoutes for every
// request (it throws on anything unregistered), fireEvent + findBy*/waitFor
// for the async gaps a real mount always has, and every input driven through
// fireEvent.change -- @testing-library/dom's own implementation of
// fireEvent.change sets a controlled input's value through its native
// setter before dispatching the event, the same reason
// docs/HANDOVER.md §2's browser-walk note gives for why a plain
// `el.value = x` assignment does not reach React's own state (a real
// browser has no fireEvent to do this for it, so that note spells the fix
// out by hand; RTL's fireEvent.change already does it here).
//
// GoalModal calls `useGoals({ enabled: false })` itself (see GoalModal.tsx's
// own header comment on why) rather than taking a mutation as a prop, so
// this file only ever stubs the specific write each test fires -- there is
// no baseline GET this modal makes on its own to stub everywhere, unlike
// BudgetModal.test.tsx's own GET /budgets/<month>.
//
// "Today" is faked to 2026-08-15 (leaving every other timer real, so
// findBy*/waitFor's own polling still works) -- BudgetPage.test.tsx's own
// convention -- so the live suggestion's monthsLeft arithmetic is
// deterministic: Aug 2026 -> Dec 2026 is 5 months inclusive.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { GoalModal } from "./GoalModal";
import type { Goal } from "./goalSchemas";

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-08-15T12:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const CURRENCIES = [
  { code: "SGD", symbol: "S$", name: "Singapore dollar" },
  { code: "IDR", symbol: "Rp", name: "Indonesian rupiah" },
];

function goalFixture(overrides: Partial<Goal> = {}): Goal {
  return {
    id: "goal-1",
    name: "Bali family trip",
    targetMinor: 1000000, // S$10,000.00
    currency: "SGD",
    targetMonth: "2026-12",
    plannedMonthlyMinor: 200000, // S$2,000.00/mo
    contributedMinor: 500000, // S$5,000.00
    percent: 50,
    status: "on_track",
    requiredMonthlyMinor: 200000,
    requiredMonthlyOk: true,
    archivedAt: null,
    ...overrides,
  };
}

function renderModal(
  props: Partial<Parameters<typeof GoalModal>[0]> = {},
  extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  const onClose = vi.fn();
  const onSaved = vi.fn();
  const fetchMock = stubFetchRoutes(extraRoutes);

  renderWithRouter(
    <GoalModal
      mode="create"
      currencies={CURRENCIES}
      primaryCurrency="SGD"
      onClose={onClose}
      onSaved={onSaved}
      {...props}
    />,
  );

  return { fetchMock, onClose, onSaved };
}

// Only the writes matter for these assertions -- BudgetModal.test.tsx's own
// mutatingCalls helper, restated here since GoalModal.tsx fires no GET of
// its own to filter out (a request appearing at all beyond what a test
// registered would already throw via stubFetchRoutes).
function mutatingCalls(fetchMock: ReturnType<typeof stubFetchRoutes>): string[] {
  return fetchMock.mock.calls
    .filter(([, init]) => (init?.method ?? "GET").toUpperCase() !== "GET")
    .map(([input, init]) => `${(init?.method ?? "GET").toUpperCase()} ${String(input)}`);
}

async function fillCreateBasics() {
  await screen.findByLabelText("Goal name");
  fireEvent.change(screen.getByLabelText("Goal name"), { target: { value: "Japan 2027" } });
  fireEvent.change(screen.getByLabelText("Target amount"), { target: { value: "10000.00" } });
  fireEvent.change(screen.getByLabelText("Target month"), { target: { value: "2026-12" } });
  fireEvent.change(screen.getByLabelText("Starting balance"), { target: { value: "0" } });
  fireEvent.change(screen.getByLabelText("Planned each month"), { target: { value: "2000.00" } });
}

describe("GoalModal", () => {
  it("create posts exactly the typed values in minor units, including startingBalanceMinor", async () => {
    let postBody: unknown;
    const { fetchMock, onSaved, onClose } = renderModal(undefined, {
      "POST /api/v1/goals": {
        status: 201,
        body: { goal: goalFixture() },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    await screen.findByLabelText("Goal name");
    fireEvent.change(screen.getByLabelText("Goal name"), { target: { value: "Japan 2027" } });
    // Currency switches first: toMinorUnits reads whatever currency is
    // selected at the moment each field is typed, and IDR is a no-decimal
    // currency for display/input purposes only -- it is still stored in
    // hundredths (formatMoney.ts's own NO_DECIMAL_CURRENCIES comment), so
    // the whole numbers below parse to the same minor units a two-decimal
    // currency's "10000.00"/"500.00"/"150.00" would.
    fireEvent.change(screen.getByLabelText("Currency"), { target: { value: "IDR" } });
    fireEvent.change(screen.getByLabelText("Target amount"), { target: { value: "10000" } });
    fireEvent.change(screen.getByLabelText("Target month"), { target: { value: "2026-12" } });
    fireEvent.change(screen.getByLabelText("Starting balance"), { target: { value: "500" } });
    fireEvent.change(screen.getByLabelText("Planned each month"), { target: { value: "150" } });

    fireEvent.click(screen.getByRole("button", { name: "Create goal" }));

    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(onClose).toHaveBeenCalled();
    expect(postBody).toEqual({
      name: "Japan 2027",
      targetMinor: 1000000,
      currency: "IDR",
      targetMonth: "2026-12",
      plannedMonthlyMinor: 15000,
      startingBalanceMinor: 50000,
    });
    expect(mutatingCalls(fetchMock)).toEqual(["POST /api/v1/goals"]);
  });

  it("the suggestion recomputes as the target and date change, and is absent while either is blank", async () => {
    renderModal();

    await screen.findByLabelText("Goal name");
    expect(screen.queryByTestId("goal-modal-suggestion")).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Target amount"), { target: { value: "10000.00" } });
    expect(screen.queryByTestId("goal-modal-suggestion")).not.toBeInTheDocument();

    // Aug 2026 -> Dec 2026 inclusive is 5 months; remaining is the full
    // 10,000 (starting balance defaults to "0"), so 10,000 / 5 = 2,000 exactly.
    fireEvent.change(screen.getByLabelText("Target month"), { target: { value: "2026-12" } });
    expect(screen.getByTestId("goal-modal-suggestion")).toHaveTextContent(
      "To hit S$10,000.00 by Dec 2026, save ~S$2,000.00/mo",
    );

    // Recomputes as the target changes: 5,000 / 5 = 1,000 exactly.
    fireEvent.change(screen.getByLabelText("Target amount"), { target: { value: "5000.00" } });
    expect(screen.getByTestId("goal-modal-suggestion")).toHaveTextContent("save ~S$1,000.00/mo");

    // The inclusive-both-ends boundary, pinned with a number: a target in
    // the *current* month (system time is faked to 2026-08-15) is 1 month
    // left, not 0 -- the whole remaining figure, not an absent panel.
    // domain.MonthsLeftInclusive's own comment states this explicitly, and
    // it is the one boundary a fencepost error in monthsLeftInclusive
    // (`months <= 0` instead of `months < 0`) would silently break.
    fireEvent.change(screen.getByLabelText("Target month"), { target: { value: "2026-08" } });
    expect(screen.getByTestId("goal-modal-suggestion")).toHaveTextContent("save ~S$5,000.00/mo");

    // Starting balance lowers `remaining` in create mode -- the decision
    // this component's own comment makes (contributed = the starting
    // balance currently typed, since nothing else has been contributed
    // yet). Back to the 5-month target date first, then a starting balance
    // of 2,000 against a 10,000 target leaves 8,000 remaining: 8,000 / 5 =
    // 1,600 exactly.
    fireEvent.change(screen.getByLabelText("Target amount"), { target: { value: "10000.00" } });
    fireEvent.change(screen.getByLabelText("Target month"), { target: { value: "2026-12" } });
    fireEvent.change(screen.getByLabelText("Starting balance"), { target: { value: "2000.00" } });
    expect(screen.getByTestId("goal-modal-suggestion")).toHaveTextContent("save ~S$1,600.00/mo");

    // And is absent again once the date is blanked, not just the target.
    fireEvent.change(screen.getByLabelText("Target month"), { target: { value: "" } });
    expect(screen.queryByTestId("goal-modal-suggestion")).not.toBeInTheDocument();
  });

  // A past target month must never produce a figure -- domain.
  // MonthsLeftInclusive's own contract returns 0, never negative, once the
  // target has passed, and RequiredMonthlyMinor refuses to divide by that.
  // Rendering a number here anyway would disagree with GoalCard, which shows
  // no required-monthly line for a goal that far behind.
  it("the suggestion stays absent for a target month already in the past", async () => {
    renderModal();

    await screen.findByLabelText("Goal name");
    fireEvent.change(screen.getByLabelText("Target amount"), { target: { value: "10000.00" } });
    fireEvent.change(screen.getByLabelText("Target month"), { target: { value: "2026-01" } });

    expect(screen.queryByTestId("goal-modal-suggestion")).not.toBeInTheDocument();
  });

  it('choosing "No target date" posts targetMonth: null and hides the suggestion panel entirely', async () => {
    let postBody: unknown;
    const { fetchMock } = renderModal(undefined, {
      "POST /api/v1/goals": {
        status: 201,
        body: { goal: goalFixture({ targetMonth: null }) },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    await screen.findByLabelText("Goal name");
    fireEvent.change(screen.getByLabelText("Goal name"), { target: { value: "Rainy day fund" } });
    fireEvent.change(screen.getByLabelText("Target amount"), { target: { value: "10000.00" } });
    fireEvent.change(screen.getByLabelText("Target month"), { target: { value: "2026-12" } });
    expect(screen.getByTestId("goal-modal-suggestion")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("switch", { name: "No target date" }));
    expect(screen.queryByTestId("goal-modal-suggestion")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Target month")).toBeDisabled();

    fireEvent.change(screen.getByLabelText("Starting balance"), { target: { value: "0" } });
    fireEvent.change(screen.getByLabelText("Planned each month"), { target: { value: "100.00" } });
    fireEvent.click(screen.getByRole("button", { name: "Create goal" }));

    await waitFor(() => expect(postBody).toBeDefined());
    expect(postBody).toMatchObject({ targetMonth: null });
    expect(mutatingCalls(fetchMock)).toEqual(["POST /api/v1/goals"]);
  });

  it("edit mode prefills from the goal, omits starting balance, and disables the currency select with its reason visible", async () => {
    renderModal({ mode: "edit", goal: goalFixture() });

    await screen.findByLabelText("Goal name");
    expect(screen.getByLabelText("Goal name")).toHaveValue("Bali family trip");
    expect(screen.getByLabelText("Target amount")).toHaveValue("10000.00");
    expect(screen.getByLabelText("Target month")).toHaveValue("2026-12");
    expect(screen.getByLabelText("Planned each month")).toHaveValue("2000.00");

    expect(screen.queryByLabelText("Starting balance")).not.toBeInTheDocument();

    const currencySelect = screen.getByLabelText("Currency");
    expect(currencySelect).toBeDisabled();
    expect(currencySelect).toHaveValue("SGD");
    expect(screen.getByText(/Currency can't change after a goal is created/)).toBeInTheDocument();
  });

  it("edit posts a PATCH; switching a dated goal to \"No target date\" sends clearTargetMonth: true", async () => {
    let patchBody: unknown;
    const goal = goalFixture();
    const { fetchMock, onSaved, onClose } = renderModal(
      { mode: "edit", goal },
      {
        [`PATCH /api/v1/goals/${goal.id}`]: {
          status: 200,
          body: { goal: goalFixture({ targetMonth: null }) },
          capture: (body) => {
            patchBody = body;
          },
        },
      },
    );

    await screen.findByLabelText("Goal name");
    fireEvent.click(screen.getByRole("switch", { name: "No target date" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(onClose).toHaveBeenCalled();
    expect(patchBody).toEqual({
      name: "Bali family trip",
      targetMinor: 1000000,
      plannedMonthlyMinor: 200000,
      clearTargetMonth: true,
    });
    expect(patchBody).not.toHaveProperty("targetMonth");
    expect(mutatingCalls(fetchMock)).toEqual([`PATCH /api/v1/goals/${goal.id}`]);
  });

  it("a 409 GOAL_NAME_TAKEN against a live goal keeps the modal open with the taken name in the message", async () => {
    const { fetchMock, onClose, onSaved } = renderModal(undefined, {
      "POST /api/v1/goals": {
        status: 409,
        body: { error: { code: "GOAL_NAME_TAKEN", message: "A goal with that name already exists." } },
      },
    });

    await fillCreateBasics();
    fireEvent.click(screen.getByRole("button", { name: "Create goal" }));

    expect(await screen.findByRole("alert")).toHaveTextContent('"Japan 2027" is already the name of a goal');
    expect(screen.queryByRole("button", { name: "Restore" })).not.toBeInTheDocument();
    expect(mutatingCalls(fetchMock)).toEqual(["POST /api/v1/goals"]);
    expect(onClose).not.toHaveBeenCalled();
    expect(onSaved).not.toHaveBeenCalled();
  });

  it("a 409 naming an archived goal offers Restore instead of a dead end", async () => {
    const { fetchMock, onClose, onSaved } = renderModal(undefined, {
      "POST /api/v1/goals": {
        status: 409,
        body: {
          error: {
            code: "GOAL_NAME_TAKEN",
            message: '"Japan 2027" is the name of an archived goal. Restore it, or choose a different name.',
            details: { archivedGoalId: "goal-archived-1" },
          },
        },
      },
      "POST /api/v1/goals/goal-archived-1/restore": { status: 200, body: { goal: goalFixture() } },
    });

    await fillCreateBasics();
    fireEvent.click(screen.getByRole("button", { name: "Create goal" }));

    const restoreButton = await screen.findByRole("button", { name: "Restore" });
    expect(screen.getByRole("alert")).toHaveTextContent("is the name of an archived goal");
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.click(restoreButton);

    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(onClose).toHaveBeenCalled();
    expect(mutatingCalls(fetchMock)).toEqual([
      "POST /api/v1/goals",
      "POST /api/v1/goals/goal-archived-1/restore",
    ]);
  });

  it("a zero or negative target shows the inline error and fires no request", async () => {
    const { fetchMock } = renderModal();

    await screen.findByLabelText("Goal name");
    fireEvent.change(screen.getByLabelText("Goal name"), { target: { value: "Japan 2027" } });
    fireEvent.change(screen.getByLabelText("Target amount"), { target: { value: "0" } });
    fireEvent.change(screen.getByLabelText("Starting balance"), { target: { value: "0" } });
    fireEvent.change(screen.getByLabelText("Planned each month"), { target: { value: "100.00" } });
    fireEvent.click(screen.getByRole("switch", { name: "No target date" }));
    fireEvent.click(screen.getByRole("button", { name: "Create goal" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Enter a target greater than zero.");
    expect(mutatingCalls(fetchMock)).toEqual([]);

    fireEvent.change(screen.getByLabelText("Target amount"), { target: { value: "-500.00" } });
    fireEvent.click(screen.getByRole("button", { name: "Create goal" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Enter a target greater than zero.");
    expect(mutatingCalls(fetchMock)).toEqual([]);
  });
});
