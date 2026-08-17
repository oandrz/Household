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
import type { Retro } from "./retroSchemas";

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

function renderModal(retro: Retro, extraRoutes: Record<string, RouteEntry> = {}) {
  const stub = instrument({
    [`GET /api/v1/retros/${retro.month}`]: { status: 200, body: { retro, carryOver: [] } },
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

  // Save draft writes without finishing -- the complete endpoint is never
  // even registered here, so a component that called it anyway would throw
  // inside stubFetchRoutes' own "no stub registered" guard, not just fail
  // this assertion quietly.
  it("Save draft sends the text and leaves the retro a draft", async () => {
    const stub = renderModal(retroFixture({ version: 2 }), {
      "PATCH /api/v1/retros/2026-07": {
        status: 200,
        body: { retro: retroFixture({ version: 3, wentWell: "Two date nights" }) },
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
  });

  // The conflict, in the modal, with copy that tells the person what
  // happened and what to do -- not a red "something went wrong" -- and
  // without throwing away what they typed. The whole point of refusing the
  // write is that nothing is lost; a banner that also wiped the field would
  // be worse than the overwrite it prevents.
  it("a 409 explains that the other partner saved, and keeps what was typed", async () => {
    const stub = stubFetchRoutes({
      "GET /api/v1/retros/2026-07": { status: 200, body: { retro: retroFixture({ version: 2 }), carryOver: [] } },
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
});
