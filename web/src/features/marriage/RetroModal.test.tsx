// Follows GoalModal.test.tsx's shape: renderWithRouter for a fresh
// QueryClient, stubFetchRoutes for every request (it throws on anything
// unregistered).
//
// Two deviations from the brief's own Step 1 sketch, both already found by
// earlier tasks in this same plan rather than invented here:
//
// 1. `@testing-library/user-event` is not a dependency anywhere in this
//    codebase (SignUpScreen.test.tsx's own comment, restated by
//    task-12-report.md's deviation #1) -- every fireEvent.change/
//    fireEvent.click below stands in for the sketch's userEvent.type/
//    userEvent.click.
// 2. `stub.bodyOf(...)`/`.called(...)`/`.orderOf(...)` don't exist on
//    stubFetchRoutes' own return value (task-9-report.md's own finding
//    against an earlier brief's identical assumption, "stub.bodyOf(...)
//    does not exist"). `instrument` below is a thin local wrapper around
//    the stub's existing per-route `capture` callback that gives those
//    three exact methods -- the same "wrap capture, don't grow the shared
//    stub" choice useRetros.test.ts made for the narrower `bodyOf`-only
//    case.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { RetroModal } from "./RetroModal";
import type { Retro, RetroAction } from "./retroSchemas";
import type { BudgetMonthResponse } from "../money/budgetSchemas";
import type { GoalsResponse } from "../money/goalSchemas";

// Same real, verified-against-real-data sample useRetros.test.ts/
// RetroDetail.test.tsx both already use (task-7-report.md), with
// completedAt: null/version: 1 -- this modal's most common caller is an
// in-progress draft, not a finished retro.
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

// RetroDetail.test.tsx's own actionFixture shape.
function actionFixture(overrides: Partial<RetroAction> = {}): RetroAction {
  return {
    id: "a1",
    body: "Phone-free dinners on weekdays",
    doneAt: null,
    carriedFrom: "",
    assigneeMembershipIds: [],
    ...overrides,
  };
}

// Task 14's own default for renderModal's money mount below: a month with no
// budget and no goals, so every test that doesn't care what
// MoneyCheckInPanel shows still gets a real, schema-valid response rather
// than an unregistered-route throw. Deliberately the EMPTY state, not a
// populated one -- a test that wants real figures on screen (this file's own
// wiring-pin test) overrides both routes through renderModal's extraRoutes.
const NO_BUDGET: BudgetMonthResponse = {
  currency: "SGD",
  month: "2026-07",
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
};

const EMPTY_GOALS: GoalsResponse = {
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

type RouteEntry = RouteResponse | RouteResponse[];

// Wraps stubFetchRoutes so a test can ask, after the render, whether a
// route was ever called and in what order relative to another -- see this
// file's header comment for why these three methods don't already exist.
// Every route's own `capture` (if any) still runs first; this only adds
// bookkeeping around it.
function instrument(routes: Record<string, RouteEntry>) {
  const order: string[] = [];
  const bodies = new Map<string, unknown>();

  function wrap(key: string, entry: RouteResponse): RouteResponse {
    return {
      ...entry,
      capture: (body) => {
        order.push(key);
        bodies.set(key, body);
        entry.capture?.(body);
      },
    };
  }

  const wrapped: Record<string, RouteEntry> = {};
  for (const [key, value] of Object.entries(routes)) {
    wrapped[key] = Array.isArray(value) ? value.map((entry) => wrap(key, entry)) : wrap(key, value);
  }

  const fetchMock = stubFetchRoutes(wrapped);

  return {
    fetchMock,
    bodyOf: (key: string) => bodies.get(key),
    called: (key: string) => order.includes(key),
    orderOf: (key: string) => order.indexOf(key),
  };
}

// `carryOver` is a third, separate parameter rather than a key smuggled
// inside `extraRoutes` -- that object's keys are "METHOD url" route strings
// (RouteEntry's own shape), and every one of this file's four pre-existing
// calls already passes route entries straight in as extraRoutes' top-level
// keys, so folding carryOver into that same object would mean touching every
// one of them for a field only the two new carry-over tests below need.
function renderModal(retro: Retro, extraRoutes: Record<string, RouteEntry> = {}, carryOver: RetroAction[] = []) {
  const stub = instrument({
    [`GET /api/v1/retros/${retro.month}`]: { status: 200, body: { retro, carryOver } },
    // MoneyCheckInPanel (Task 14) fetches both of these unconditionally the
    // moment it mounts, which is the moment this modal's own GET resolves --
    // every test that renders this modal needs both registered, or
    // stubFetchRoutes throws before ANY capture runs (Task 13's own finding,
    // restated in this task's brief). Defaulted to the empty state so a test
    // that doesn't care what the money panel shows still gets a real,
    // schema-valid response; `extraRoutes` below can override either key for
    // a test that does care (this file's own wiring-pin test does).
    [`GET /api/v1/budgets/${retro.month}`]: { status: 200, body: NO_BUDGET },
    "GET /api/v1/goals": { status: 200, body: EMPTY_GOALS },
    ...extraRoutes,
  });
  const onClose = vi.fn();
  renderWithRouter(<RetroModal month={retro.month} onClose={onClose} />);
  return { ...stub, onClose };
}

// The conflict test needs a 409 on the very first save, which `renderModal`
// above has no hook for (its GET default and a caller's PATCH override are
// the only two routes it composes) -- this variant takes an already-built
// stub instead, the same two-helper split GoalModal.test.tsx's own
// renderModal/fillCreateBasics pair uses for "compose vs. hand-roll".
function renderModalWith(fetchMock: ReturnType<typeof stubFetchRoutes>, month = "2026-07") {
  const onClose = vi.fn();
  renderWithRouter(<RetroModal month={month} onClose={onClose} />);
  return { fetchMock, onClose };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("RetroModal", () => {
  // The design's five emoji, a real radio group -- one tab stop, arrow keys
  // between options, both free once every input shares one `name` -- because
  // that is what a single-choice control is. See RetroModal.tsx's own
  // header comment on the mood picker for the exact shape this must NOT be
  // (sr-only inputs behind a styled stand-in), and why: that shape shipped
  // keyboard-invisible focus in TransactionsPage's Kind filter with every
  // unit test green, because fireEvent.click never presses a key.
  it("the mood picker is a radio group with five options", async () => {
    renderModal(retroFixture({ mood: null }));

    const group = await screen.findByRole("radiogroup", { name: /How was this month/ });
    expect(within(group).getAllByRole("radio")).toHaveLength(5);
  });

  // Save draft writes without finishing, and closes the modal.
  //
  // The complete route is registered here, not omitted -- an earlier
  // version of this test left it unregistered on the theory that a
  // wrongful call would throw and fail loudly. That theory was wrong:
  // stubFetchRoutes throws inside its own fetch mock BEFORE any route's
  // `capture` ever runs (fetchStub.ts's own "no stub registered" guard),
  // and that throw lands in handleSaveDraft's own try/catch
  // (RetroModal.tsx), which sets `saveError` and swallows it -- a
  // handleSaveDraft that wrongly called finishRetro() too would still pass
  // both the PATCH assertion and a `.called(...)` read off an unregistered
  // route, because an unregistered route's key can never reach `instrument`'s
  // own `order` array at all, whether or not the component tried to call
  // it. Registering the route with a normal 200 means a wrongful call is
  // actually recorded, which is what makes `.called(...)` below a real
  // assertion instead of one that can never fail. See this task's own
  // report for the mutation that proves it.
  it("Save draft sends the text, leaves the retro a draft, and closes the modal", async () => {
    const stub = renderModal(retroFixture({ version: 2 }), {
      "PATCH /api/v1/retros/2026-07": {
        status: 200,
        body: { retro: retroFixture({ version: 3, wentWell: "Two date nights" }) },
      },
      "POST /api/v1/retros/2026-07/complete": {
        status: 200,
        body: {
          retro: retroFixture({
            version: 3,
            wentWell: "Two date nights",
            completedAt: "2026-07-28T21:18:52+08:00",
          }),
        },
      },
    });

    const field = await screen.findByLabelText(/What went well/);
    fireEvent.change(field, { target: { value: "Two date nights" } });
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));

    await waitFor(() =>
      expect(stub.bodyOf("PATCH /api/v1/retros/2026-07")).toMatchObject({
        wentWell: "Two date nights",
        version: 2,
      }),
    );
    expect(stub.called("POST /api/v1/retros/2026-07/complete")).toBe(false);
    expect(stub.onClose).toHaveBeenCalled();
  });

  // Finish saves first, then completes -- in that order, so a finish can
  // never discard what is currently typed. This is the ordering test the
  // brief's own Step 5 mutation check targets; see this task's report for
  // the mutation and its result.
  it("Finish retro saves before completing", async () => {
    const stub = renderModal(retroFixture({ version: 2 }), {
      "PATCH /api/v1/retros/2026-07": {
        status: 200,
        body: { retro: retroFixture({ version: 3, wasHard: "Phones at dinner" }) },
      },
      "POST /api/v1/retros/2026-07/complete": {
        status: 200,
        body: {
          retro: retroFixture({
            version: 3,
            wasHard: "Phones at dinner",
            completedAt: "2026-07-28T21:18:52+08:00",
          }),
        },
      },
    });

    const field = await screen.findByLabelText(/What was hard/);
    fireEvent.change(field, { target: { value: "Phones at dinner" } });
    fireEvent.click(screen.getByRole("button", { name: "Finish retro" }));

    await waitFor(() => expect(stub.called("POST /api/v1/retros/2026-07/complete")).toBe(true));
    expect(stub.orderOf("PATCH /api/v1/retros/2026-07")).toBeLessThan(
      stub.orderOf("POST /api/v1/retros/2026-07/complete"),
    );
    // Same gap the Save draft test above just closed for its own success
    // path -- nothing previously checked that a successful Finish actually
    // closes the modal, only that the two network calls happened in order.
    expect(stub.onClose).toHaveBeenCalled();
  });

  // The conflict, in the modal, with copy that tells the person what
  // happened and what to do -- not a red "something went wrong" -- and
  // without throwing away what they typed. The whole point of refusing the
  // write is that nothing is lost; a banner that also wiped the field would
  // be worse than the overwrite it prevents.
  it("a 409 explains that the other partner saved, and keeps what was typed", async () => {
    const stub = stubFetchRoutes({
      "GET /api/v1/retros/2026-07": { status: 200, body: { retro: retroFixture({ version: 2 }), carryOver: [] } },
      "GET /api/v1/budgets/2026-07": { status: 200, body: NO_BUDGET },
      "GET /api/v1/goals": { status: 200, body: EMPTY_GOALS },
      "PATCH /api/v1/retros/2026-07": {
        status: 409,
        body: { error: { code: "RETRO_CHANGED", message: "Someone else saved this retro." } },
      },
    });
    renderModalWith(stub);

    const notesField = await screen.findByLabelText(/Notes/);
    fireEvent.change(notesField, { target: { value: "mine" } });
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));

    expect(await screen.findByTestId("retro-conflict")).toHaveTextContent(
      "saved this retro while you were editing",
    );
    // Nothing typed is thrown away -- the field still holds it after the
    // refused save, not just before.
    expect(screen.getByLabelText(/Notes/)).toHaveValue("mine");
    // A real browser walk against a real 409 found the gap this asserts:
    // once a conflict has been seen, re-enabling Save/Finish on the
    // strength of useRetro's own `conflict` flag alone (which clears on any
    // later successful refetch, including a background one this modal never
    // asked for) lets the very next Save silently overwrite whatever the
    // other partner just wrote, with the new version attached and no error
    // -- last write wins, the shape the design spec's decision 6 rejects by
    // name. Both actions must stay off for the rest of this mount.
    expect(screen.getByRole("button", { name: "Save draft" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Finish retro" })).toBeDisabled();
  });

  // The mood picker is this task's headline control -- this is the one test
  // that proves a selection actually reaches the write, not just that the
  // radiogroup has the right shape (the first test above) or that the two
  // textareas do (Save draft/Finish above). Clicking a radio directly (not
  // Tab+arrow) is deliberate: `fireEvent.click` on the real, visible input
  // is exactly what a mouse/touch user does, distinct from the keyboard path
  // the radiogroup test's own comment already covers.
  it("selecting a mood sends it on the next save", async () => {
    const stub = renderModal(retroFixture({ version: 2, mood: null }), {
      "PATCH /api/v1/retros/2026-07": {
        status: 200,
        body: { retro: retroFixture({ version: 3, mood: 2 }) },
      },
    });

    fireEvent.click(await screen.findByRole("radio", { name: "Not great" }));
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));

    await waitFor(() =>
      expect(stub.bodyOf("PATCH /api/v1/retros/2026-07")).toMatchObject({ mood: 2, version: 2 }),
    );
  });

  // Carrying is one click, and it creates a NEW action on THIS retro
  // pointing back at last month's -- July's own row is untouched (the API
  // guarantees it; this asserts the request sent, not the July row itself,
  // which this modal never touches). `offered.id` is a deliberately
  // distinctive UUID-shaped string, never a hardcoded literal the assertion
  // could accidentally match against something the component invented --
  // asserting equality against the fixture's own id is what would catch a
  // component that carries the wrong id (the retro's own id, an empty
  // string, anything else "plausible") while still posting *something*.
  it("carrying an open action posts a new action with the offered id, not a freehand one", async () => {
    const offered = actionFixture({ id: "b6a1f9e0-carry-source", body: "Phone-free dinners" });
    const stub = renderModal(
      retroFixture({ month: "2026-08" }),
      {
        "POST /api/v1/retros/2026-08/actions": {
          status: 201,
          body: { action: actionFixture({ id: "new-action-1", body: offered.body, carriedFrom: offered.id }) },
        },
      },
      [offered],
    );

    fireEvent.click(await screen.findByRole("button", { name: `Carry over ${offered.body}` }));

    await waitFor(() =>
      expect(stub.bodyOf("POST /api/v1/retros/2026-08/actions")).toMatchObject({
        body: offered.body,
        carriedFrom: offered.id,
      }),
    );
  });

  // The offer only shows when there is something to offer -- an empty
  // `carryOver` renders no "Still open" heading at all, not an empty one.
  // Asserts a positive control first (docs/LEARNING.md pattern 2: an absence
  // assertion holds vacuously over a dialog that never finished rendering),
  // so this proves the modal actually mounted before checking the heading
  // isn't in it.
  it("shows nothing at all when last month left nothing open", async () => {
    renderModal(retroFixture());

    expect(await screen.findByLabelText(/What went well/)).toBeInTheDocument();
    expect(screen.queryByText(/Still open from/)).not.toBeInTheDocument();
    expect(screen.getByTestId("retro-modal-actions-mount")).toBeEmptyDOMElement();
  });

  // Pins MoneyCheckInPanel's own wiring inside this modal (docs/LEARNING.md
  // pattern 15, the same reasoning RetrosPage.test.tsx's Start/Edit tests
  // already carry for RetroModal itself): this asserts real figures the
  // panel renders, not merely that the modal opened. Deleting
  // `<MoneyCheckInPanel month={month} />` from RetroModal.tsx must turn this
  // red, not just MoneyCheckInPanel.test.tsx's own suite (which never mounts
  // RetroModal at all).
  it("renders the money check-in panel with real figures", async () => {
    renderModal(retroFixture(), {
      "GET /api/v1/budgets/2026-07": {
        status: 200,
        body: { ...NO_BUDGET, budget: { expectedIncomeMinor: 500000, lines: [] }, percentOk: true, percentUsed: 66 },
      },
      "GET /api/v1/goals": {
        status: 200,
        body: { ...EMPTY_GOALS, summary: { ...EMPTY_GOALS.summary, onTrackCount: 4, datedCount: 4 } },
      },
    });

    expect(await screen.findByTestId("checkin-budget")).toHaveTextContent("66% used");
    expect(screen.getByTestId("checkin-goals")).toHaveTextContent("4 of 4 on track");
  });
});
