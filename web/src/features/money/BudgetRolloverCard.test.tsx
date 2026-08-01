// Follows GoalContributionsPanel.test.tsx/GoalModal.test.tsx's conventions:
// renderWithRouter for a fresh QueryClient, stubFetchRoutes for every
// request (it throws on anything unregistered), fireEvent + findBy*/waitFor
// for the async gaps a real mount always has.
//
// `closed` is a plain prop this component trusts rather than re-deriving
// (BudgetRolloverCard.tsx's own header comment on why an earlier version
// that recomputed it from `month` against the real clock was wrong), so
// there is no fake-clock setup here at all -- every "is this month closed"
// case below is just a different `closed` value, not a different date.
//
// The two cross-query proofs the brief also names -- "a successful move
// refetches both the month and /goals" and "rollOver POSTs {goalId}" -- are
// pinned at useBudget.test.ts's own level instead ("rollOver POSTs {goalId}
// then re-GETs both the month and /goals"), where a real, *active*
// useBudget(month) observer actually exists to refetch through. This
// card's own `useBudget(month, { enabled: false })` instance never becomes
// active (react-query's `invalidateQueries` only auto-refetches watched
// queries), so no render here could prove that refetch actually reaches
// anything -- BudgetPage.test.tsx's own rollover describe block proves the
// full, real chain end to end instead.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { BudgetRolloverCard } from "./BudgetRolloverCard";
import type { Goal } from "./goalSchemas";

function goalFixture(overrides: Partial<Goal> = {}): Goal {
  return {
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
    ...overrides,
  };
}

function goalsBody(goals: Goal[]) {
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
    },
  };
}

function renderCard(
  props: Partial<{
    month: string;
    closed: boolean;
    remainingMinor: number;
    rolloverAmountMinor: number | null;
    currency: string;
    rolledOverTo: string | null;
    onRolledOver: () => void;
    symbol: string;
    excludedNoRate: number;
  }> = {},
  extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  const fetchMock = stubFetchRoutes({
    "GET /api/v1/goals": { status: 200, body: goalsBody([goalFixture()]) },
    ...extraRoutes,
  });
  const onRolledOver = props.onRolledOver ?? vi.fn();
  // Defaults to the "done" branch's own figure whenever a test names a
  // destination goal (rolledOverTo set) and leaves the amount unspecified --
  // most existing fixtures below only ever set rolledOverTo, and the two are
  // meant to move together. Tests that need to prove the amount and
  // remainingMinor can legitimately disagree (Finding 1's own regression)
  // pass rolloverAmountMinor explicitly instead.
  const stamped = props.rolledOverTo !== undefined && props.rolledOverTo !== null;
  const rolloverAmountMinor = props.rolloverAmountMinor ?? (stamped ? (props.remainingMinor ?? 178000) : null);
  return {
    fetchMock,
    onRolledOver,
    ...renderWithRouter(
      <BudgetRolloverCard
        month={props.month ?? "2026-07"}
        closed={props.closed ?? true}
        remainingMinor={props.remainingMinor ?? 178000}
        rolloverAmountMinor={rolloverAmountMinor}
        currency={props.currency ?? "SGD"}
        rolledOverTo={props.rolledOverTo ?? null}
        onRolledOver={onRolledOver}
        symbol={props.symbol ?? "S$"}
        excludedNoRate={props.excludedNoRate ?? 0}
      />,
    ),
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("BudgetRolloverCard", () => {
  it("renders nothing when the month is not closed, even with unspent budget and no stamp", () => {
    renderCard({ closed: false, remainingMinor: 178000 });

    expect(screen.queryByTestId("budget-rollover-offer")).not.toBeInTheDocument();
    expect(screen.queryByTestId("budget-rollover-done")).not.toBeInTheDocument();
  });

  it("renders nothing when the month is not closed, even once stamped", () => {
    // Not a real reachable state (the server refuses a rollover on an open
    // month -- BudgetService.RollOver's own doc comment), but this
    // component fails closed on it anyway rather than trusting a
    // `rolledOverTo` it has no other reason to believe.
    renderCard({ closed: false, rolledOverTo: "goal-1" });

    expect(screen.queryByTestId("budget-rollover-offer")).not.toBeInTheDocument();
    expect(screen.queryByTestId("budget-rollover-done")).not.toBeInTheDocument();
  });

  it("renders nothing on a closed month with nothing unspent and no stamp", () => {
    renderCard({ closed: true, remainingMinor: 0 });

    expect(screen.queryByTestId("budget-rollover-offer")).not.toBeInTheDocument();
    expect(screen.queryByTestId("budget-rollover-done")).not.toBeInTheDocument();
  });

  it("offers the move with the formatted amount and month name on a closed month with unspent budget and no stamp", async () => {
    renderCard({ closed: true, month: "2026-07", remainingMinor: 178000 });

    const offer = await screen.findByTestId("budget-rollover-offer");
    expect(offer).toHaveTextContent("S$1,780.00 unspent in July");
    expect(screen.getByTestId("budget-rollover-cta")).toHaveTextContent("Move it into a goal");
  });

  // Finding 4 of the goals-branch review: the " · " separator used to render
  // unconditionally while only the button after it was gated on
  // `!pickerOpen`, so opening the picker left the sentence ending in
  // "... unspent in July · " with nothing after it -- a dangling separator
  // pointing at nothing. The fix moved the separator inside the same guard
  // as the button, so opening the picker must remove both together.
  it("removes the trailing separator along with the button once the picker opens", async () => {
    renderCard({ closed: true, month: "2026-07", remainingMinor: 178000 });

    await screen.findByTestId("budget-rollover-cta");
    await waitFor(() => expect(screen.getByTestId("budget-rollover-cta")).not.toBeDisabled());

    const offerText = screen.getByTestId("budget-rollover-offer-text");
    expect(offerText.textContent).toBe("S$1,780.00 unspent in July · Move it into a goal");

    screen.getByTestId("budget-rollover-cta").click();

    await waitFor(() => expect(screen.queryByTestId("budget-rollover-cta")).not.toBeInTheDocument());
    expect(screen.getByTestId("budget-rollover-offer-text").textContent).toBe("S$1,780.00 unspent in July");
  });

  // Owner ruling, 2026-08-01: remainingMinor excludes any expense with no
  // available exchange rate the same way Spent does, so on a month with
  // exclusions the "unspent" figure can read higher than what was truly
  // left. The ruling is to name the count and what it means, right next to
  // the offer -- not to block the move.
  it("renders no exclusion note when excludedNoRate is 0", async () => {
    renderCard({ closed: true, excludedNoRate: 0 });

    await screen.findByTestId("budget-rollover-offer");
    expect(screen.queryByTestId("budget-rollover-excluded-note")).not.toBeInTheDocument();
  });

  it("names the excluded count and what it means for the unspent figure when excludedNoRate is positive, and the move still works", async () => {
    let postBody: unknown;
    renderCard(
      { closed: true, excludedNoRate: 3 },
      {
        "GET /api/v1/goals": { status: 200, body: goalsBody([goalFixture()]) },
        "POST /api/v1/budgets/2026-07/rollover": {
          status: 200,
          body: {
            contribution: {
              id: "contribution-1",
              amountMinor: 178000,
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
      },
    );

    const note = await screen.findByTestId("budget-rollover-excluded-note");
    expect(note).toHaveTextContent("3 transactions are not counted: no exchange rate.");
    expect(note).toHaveTextContent("The unspent amount above may be higher than what was actually left.");

    // The note is information, not a refusal -- the button stays enabled
    // and the move still works.
    await waitFor(() => expect(screen.getByTestId("budget-rollover-cta")).not.toBeDisabled());
    screen.getByTestId("budget-rollover-cta").click();
    const select = await screen.findByTestId("budget-rollover-select");
    fireEvent.change(select, { target: { value: "goal-1" } });
    screen.getByTestId("budget-rollover-confirm").click();

    await waitFor(() => expect(postBody).toEqual({ goalId: "goal-1" }));
  });

  it("shows the destination sentence, not the offer, once the month already carries a stamp", async () => {
    renderCard({ closed: true, remainingMinor: 178000, rolledOverTo: "goal-1" });

    // findByTestId resolves the instant the (already-mounted) element
    // exists -- true even in the instant before /goals has resolved, when
    // this same paragraph is still rendering the nameless fallback -- so
    // this waits for the real, resolved content specifically, the same
    // `waitFor`-around-content pattern BudgetPage.test.tsx's own month-switch
    // tests use for the identical reason.
    await waitFor(() =>
      expect(screen.getByTestId("budget-rollover-done")).toHaveTextContent("S$1,780.00 moved into Bali trip."),
    );
    expect(screen.queryByTestId("budget-rollover-offer")).not.toBeInTheDocument();
    expect(screen.queryByTestId("budget-rollover-cta")).not.toBeInTheDocument();
  });

  // Finding 1's own regression test at the component level: remainingMinor
  // is recomputed on every GET from whatever transactions exist in the
  // month right now, so a late transaction landing in an already-rolled-over
  // month can leave it disagreeing with what actually moved. The "done"
  // sentence must render rolloverAmountMinor -- the frozen figure -- even
  // when remainingMinor has since drifted to a different, and here even
  // negative, value.
  it("renders the frozen rolloverAmountMinor in the done sentence, never the live remainingMinor", async () => {
    renderCard({ closed: true, remainingMinor: -5000, rolloverAmountMinor: 178000, rolledOverTo: "goal-1" });

    await waitFor(() =>
      expect(screen.getByTestId("budget-rollover-done")).toHaveTextContent("S$1,780.00 moved into Bali trip."),
    );
    expect(screen.getByTestId("budget-rollover-done")).not.toHaveTextContent("S$50.00");
  });

  it("falls back to a nameless destination sentence when the stamped goal isn't in the fetched list", async () => {
    renderCard(
      { closed: true, remainingMinor: 178000, rolledOverTo: "goal-missing" },
      { "GET /api/v1/goals": { status: 200, body: goalsBody([goalFixture()]) } },
    );

    expect(await screen.findByTestId("budget-rollover-done")).toHaveTextContent("S$1,780.00 moved into a goal.");
  });

  it("excludes an archived goal from the picker", async () => {
    renderCard(
      { closed: true },
      {
        "GET /api/v1/goals": {
          status: 200,
          body: goalsBody([goalFixture({ id: "goal-1", name: "Bali trip" })]),
        },
      },
    );

    // findByTestId resolves the instant the button exists -- even while
    // it's still disabled, mid-`useGoals()` fetch -- so this waits for it to
    // actually become clickable before firing the click; a disabled button
    // silently swallows a native `.click()`, which otherwise leaves the
    // picker never opening and every assertion after this timing out on
    // "budget-rollover-select" for a reason that has nothing to do with it.
    await screen.findByTestId("budget-rollover-cta");
    await waitFor(() => expect(screen.getByTestId("budget-rollover-cta")).not.toBeDisabled());
    screen.getByTestId("budget-rollover-cta").click();
    const select = await screen.findByTestId("budget-rollover-select");
    // useGoals() (no include_archived) never returns an archived row at
    // all -- GET /goals' own contract, not a client-side filter -- so
    // there is nothing here to assert away beyond "only what was actually
    // returned is offered."
    expect(select).toHaveTextContent("Bali trip");
  });

  it("lists a non-primary-currency goal as an unavailable, disabled option naming the reason", async () => {
    renderCard(
      { closed: true, currency: "SGD" },
      {
        "GET /api/v1/goals": {
          status: 200,
          body: goalsBody([
            goalFixture({ id: "goal-1", name: "Bali trip", currency: "SGD" }),
            goalFixture({ id: "goal-2", name: "London flat", currency: "GBP" }),
          ]),
        },
      },
    );

    // findByTestId resolves the instant the button exists -- even while
    // it's still disabled, mid-`useGoals()` fetch -- so this waits for it to
    // actually become clickable before firing the click; a disabled button
    // silently swallows a native `.click()`, which otherwise leaves the
    // picker never opening and every assertion after this timing out on
    // "budget-rollover-select" for a reason that has nothing to do with it.
    await screen.findByTestId("budget-rollover-cta");
    await waitFor(() => expect(screen.getByTestId("budget-rollover-cta")).not.toBeDisabled());
    screen.getByTestId("budget-rollover-cta").click();
    const select = await screen.findByTestId("budget-rollover-select");
    const ineligibleOption = Array.from(select.querySelectorAll("option")).find((o) =>
      o.textContent?.includes("London flat"),
    );
    expect(ineligibleOption).toBeDefined();
    expect(ineligibleOption).toBeDisabled();
    expect(ineligibleOption?.textContent).toContain("GBP");
    expect(ineligibleOption?.textContent).toContain("only SGD goals can receive a rollover");

    // The eligible goal is still a real, selectable option.
    const eligibleOption = Array.from(select.querySelectorAll("option")).find((o) => o.textContent === "Bali trip");
    expect(eligibleOption).toBeDefined();
    expect(eligibleOption).not.toBeDisabled();
  });

  it("disables the button and explains why when the household has no goals at all", async () => {
    renderCard({ closed: true }, { "GET /api/v1/goals": { status: 200, body: goalsBody([]) } });

    const reason = await screen.findByTestId("budget-rollover-disabled-reason");
    expect(reason).toHaveTextContent("You don't have any savings goals yet.");
    expect(screen.getByTestId("budget-rollover-cta")).toBeDisabled();
    expect(screen.queryByTestId("budget-rollover-select")).not.toBeInTheDocument();
  });

  it("disables the button and explains why when every goal is in a different currency", async () => {
    renderCard(
      { closed: true, currency: "SGD" },
      {
        "GET /api/v1/goals": {
          status: 200,
          body: goalsBody([goalFixture({ id: "goal-2", name: "London flat", currency: "GBP" })]),
        },
      },
    );

    const reason = await screen.findByTestId("budget-rollover-disabled-reason");
    expect(reason).toHaveTextContent("None of your goals are in SGD");
    expect(screen.getByTestId("budget-rollover-cta")).toBeDisabled();
  });

  it("POSTs {goalId} to the month's rollover route on confirm, and fires onRolledOver", async () => {
    let postBody: unknown;
    const { onRolledOver } = renderCard(
      { closed: true },
      {
        "GET /api/v1/goals": { status: 200, body: goalsBody([goalFixture()]) },
        "POST /api/v1/budgets/2026-07/rollover": {
          status: 200,
          body: {
            contribution: {
              id: "contribution-1",
              amountMinor: 178000,
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
      },
    );

    // findByTestId resolves the instant the button exists -- even while
    // it's still disabled, mid-`useGoals()` fetch -- so this waits for it to
    // actually become clickable before firing the click; a disabled button
    // silently swallows a native `.click()`, which otherwise leaves the
    // picker never opening and every assertion after this timing out on
    // "budget-rollover-select" for a reason that has nothing to do with it.
    await screen.findByTestId("budget-rollover-cta");
    await waitFor(() => expect(screen.getByTestId("budget-rollover-cta")).not.toBeDisabled());
    screen.getByTestId("budget-rollover-cta").click();
    const select = await screen.findByTestId("budget-rollover-select");
    fireEvent.change(select, { target: { value: "goal-1" } });
    screen.getByTestId("budget-rollover-confirm").click();

    await waitFor(() => expect(postBody).toEqual({ goalId: "goal-1" }));
    await waitFor(() => expect(onRolledOver).toHaveBeenCalledTimes(1));
    // The picker closes once the move succeeds -- nothing left to confirm.
    expect(screen.queryByTestId("budget-rollover-select")).not.toBeInTheDocument();
  });

  it("shows a refusal inline rather than pretending the move succeeded", async () => {
    renderCard(
      { closed: true },
      {
        "GET /api/v1/goals": { status: 200, body: goalsBody([goalFixture()]) },
        "POST /api/v1/budgets/2026-07/rollover": {
          status: 409,
          body: { error: { code: "ROLLOVER_ALREADY_DONE", message: "That month has already been rolled over." } },
        },
      },
    );

    // findByTestId resolves the instant the button exists -- even while
    // it's still disabled, mid-`useGoals()` fetch -- so this waits for it to
    // actually become clickable before firing the click; a disabled button
    // silently swallows a native `.click()`, which otherwise leaves the
    // picker never opening and every assertion after this timing out on
    // "budget-rollover-select" for a reason that has nothing to do with it.
    await screen.findByTestId("budget-rollover-cta");
    await waitFor(() => expect(screen.getByTestId("budget-rollover-cta")).not.toBeDisabled());
    screen.getByTestId("budget-rollover-cta").click();
    const select = await screen.findByTestId("budget-rollover-select");
    fireEvent.change(select, { target: { value: "goal-1" } });
    screen.getByTestId("budget-rollover-confirm").click();

    expect(await screen.findByTestId("budget-rollover-error")).toHaveTextContent(
      "That month has already been rolled over.",
    );
    // Still the offer, not the destination sentence -- this card's own
    // props never changed (nothing here re-renders it with a new
    // `rolledOverTo`), so it must not claim the money moved anywhere.
    expect(screen.queryByTestId("budget-rollover-done")).not.toBeInTheDocument();
  });
});
