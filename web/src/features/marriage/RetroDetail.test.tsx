// Follows RetrosPage.test.tsx/useRetros.test.ts's own conventions:
// renderWithRouter for a fresh QueryClient, stubFetchRoutes for every
// request (it throws on anything unregistered), fireEvent (not
// @testing-library/user-event -- SignUpScreen.test.tsx's own comment: that
// package is not a dependency anywhere in this codebase, so the brief's own
// Step 1 sketch, which calls `userEvent.click`, is adapted to fireEvent
// here rather than adding a new dependency for one file), and a route's
// `capture` callback for reading back a posted body (task-9-report.md's own
// deviation note: the brief's `stub.bodyOf(...)` does not exist in
// fetchStub.ts).
//
// RetroDetail owns two things a recorded defect each demand real proof of:
// 1. the tick is a real, keyboard-reachable `<input type="checkbox">`
//    (docs/LEARNING.md pattern 3 -- an `sr-only` input behind a styled
//    stand-in shipped keyboard-invisible focus in TransactionsPage's Kind
//    filter, and no unit test caught it because fireEvent.click never
//    presses a key). `getByRole("checkbox", { name })` only resolves at all
//    if the input is real and properly labelled, so the tests below prove
//    that shape by construction, not by a separate assertion.
// 2. the tick's own PATCH body never carries the retro's `version` (useRetro.ts's
//    own header comment: a tick writes a different table precisely so it
//    cannot collide with the other partner's open editor) -- the "sends no
//    retro version" test's own capture asserts this directly.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { RetroDetail } from "./RetroDetail";
import type { Retro, RetroAction } from "./retroSchemas";

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

// Verified-shape defaults, the same task-7-report.md sample RetrosPage.test.tsx
// and useRetros.test.ts already draw their own retroFixtures from.
function retroFixture(overrides: Partial<Retro> = {}): Retro {
  return {
    id: "4844a918-fa2f-446c-b43b-deddecb49889",
    month: "2026-07",
    mood: 4,
    wentWell: "We stuck to the grocery budget.",
    wasHard: "Christine's parents visiting threw off the schedule.",
    notes: "We finally fixed the grocery budget. Next month: try meal prep.",
    completedAt: "2026-07-28T21:18:52+08:00",
    version: 7,
    actions: [],
    ...overrides,
  };
}

const MEMBERS = {
  status: 200,
  body: [
    {
      id: "m-a",
      user: { id: "u-a", email: "andreas@hearth.family", displayName: "Andreas", avatarInitial: "A" },
      role: "owner",
      capabilities: ["marriage"],
    },
    {
      id: "m-c",
      user: { id: "u-c", email: "christine@hearth.family", displayName: "Christine", avatarInitial: "C" },
      role: "owner",
      capabilities: ["marriage"],
    },
  ],
};

function renderDetail(record: Retro, extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {}) {
  const fetchMock = stubFetchRoutes({
    [`GET /api/v1/retros/${record.month}`]: { status: 200, body: { retro: record, carryOver: [] } },
    "GET /api/v1/household/members": MEMBERS,
    ...extraRoutes,
  });
  return { fetchMock, ...renderWithRouter(<RetroDetail month={record.month} />) };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("RetroDetail", () => {
  it("renders the month heading, completion date and mood", async () => {
    renderDetail(retroFixture());

    expect(await screen.findByTestId("retro-detail-heading")).toHaveTextContent("July 2026 retro");
    // "Jul 28" from the fixture's completedAt, "mood 4/5" from its mood --
    // both real fields this test's fixture actually carries, not guessed
    // defaults RetroDetail happens to render regardless.
    expect(screen.getByText(/Jul 28/)).toBeInTheDocument();
    expect(screen.getByText(/mood 4\/5/)).toBeInTheDocument();
  });

  // The design's own row: "Phone-free dinners on weekdays  A C" -- one
  // action, both owners. Initials come from the household's own members
  // (useHouseholdMembers), never from parsing a name in this component.
  it("shows an initial per assignee", async () => {
    renderDetail(
      retroFixture({
        actions: [actionFixture({ body: "Phone-free dinners", assigneeMembershipIds: ["m-a", "m-c"] })],
      }),
    );

    const row = await screen.findByTestId("retro-action-a1");
    expect(within(row).getByTitle("Andreas")).toHaveTextContent("A");
    expect(within(row).getByTitle("Christine")).toHaveTextContent("C");
    // Exactly two children in the assignee container -- not merely "two
    // elements with a title", the same distinction the empty case below
    // exists to make.
    expect(screen.getByTestId("retro-action-assignees-a1").children).toHaveLength(2);
  });

  // task-12-brief.md's own design-details paragraph: "none at all when
  // there are none (not a placeholder circle)". Asserted against the
  // assignee container's own child count, not against `title` presence --
  // a dashed placeholder circle with no `title` attribute would pass a
  // `queryAllByTitle` check while still being exactly the thing the brief
  // forbids. `toBeEmptyDOMElement()` catches ANY rendered content here,
  // titled or not.
  it("renders no initial at all for an action nobody is assigned to", async () => {
    renderDetail(retroFixture({ actions: [actionFixture({ assigneeMembershipIds: [] })] }));

    await screen.findByTestId("retro-action-a1");
    expect(screen.getByTestId("retro-action-assignees-a1")).toBeEmptyDOMElement();
  });

  // A membership id on the action that no longer matches a real member (the
  // household removed that person after the action was assigned) fails
  // closed -- it is skipped, not rendered as a blank or broken circle. The
  // container's own child count (not a title-based count) is what proves
  // "skipped" rather than "rendered untitled" for the unmatched id.
  it("skips an assignee id no current member matches, rather than rendering a broken circle", async () => {
    renderDetail(
      retroFixture({ actions: [actionFixture({ assigneeMembershipIds: ["m-a", "m-gone"] })] }),
    );

    const row = await screen.findByTestId("retro-action-a1");
    expect(within(row).getByTitle("Andreas")).toBeInTheDocument();
    expect(screen.getByTestId("retro-action-assignees-a1").children).toHaveLength(1);
  });

  // Ticking is a PATCH to the action, and it must not send the retro's
  // version: one partner ticking all month cannot be allowed to invalidate
  // the other's open editor (useRetro.ts's own header comment).
  it("ticking an action sends no retro version", async () => {
    let patchBody: unknown;
    const before = retroFixture({ version: 7, actions: [actionFixture({ id: "a1", doneAt: null })] });
    const after = retroFixture({ version: 7, actions: [actionFixture({ id: "a1", doneAt: "2026-08-16T21:18:52Z" })] });
    renderDetail(before, {
      "PATCH /api/v1/retros/2026-07/actions/a1": {
        status: 200,
        body: { id: "a1", doneAt: "2026-08-16T21:18:52Z" },
        capture: (body) => {
          patchBody = body;
        },
      },
      // setActionDone's own afterWrite refetches both this query and the
      // history list's -- both need a second GET queued (the second entry,
      // reflecting the row now ticked) or stubFetchRoutes throws on the
      // unregistered second call.
      "GET /api/v1/retros/2026-07": [
        { status: 200, body: { retro: before, carryOver: [] } },
        { status: 200, body: { retro: after, carryOver: [] } },
      ],
      "GET /api/v1/retros": { status: 200, body: { retros: [], mood: [], doneCount: 0, since: null, startMonth: null } },
    });

    fireEvent.click(await screen.findByRole("checkbox", { name: /Phone-free dinners/ }));

    await waitFor(() => expect(patchBody).toMatchObject({ done: true }));
    expect(patchBody).not.toHaveProperty("version");
  });

  it("renders went-well and was-hard as bullets, splitting on newlines and dropping blank lines", async () => {
    renderDetail(
      retroFixture({
        wentWell: "Two date nights happened\n\nZero money fights",
        wasHard: "Phones crept back in\nNo time alone\n",
      }),
    );

    const wentWell = await screen.findByTestId("retro-went-well");
    expect(within(wentWell).getAllByRole("listitem")).toHaveLength(2);
    expect(wentWell).toHaveTextContent("Two date nights happened");
    expect(wentWell).toHaveTextContent("Zero money fights");

    const wasHard = screen.getByTestId("retro-was-hard");
    expect(within(wasHard).getAllByRole("listitem")).toHaveLength(2);
    expect(wasHard).toHaveTextContent("Phones crept back in");
    expect(wasHard).toHaveTextContent("No time alone");
  });

  // A went-well/was-hard field that is blank end to end (an empty string,
  // or nothing but newlines) renders no card at all -- never an empty box
  // with a heading and nothing under it.
  it("renders no went-well card when the field is blank", async () => {
    renderDetail(retroFixture({ wentWell: "\n\n", wasHard: "Something hard" }));

    await screen.findByTestId("retro-was-hard");
    expect(screen.queryByTestId("retro-went-well")).not.toBeInTheDocument();
  });

  // An action carried from last month says so -- the design's own
  // carried_from provenance (spec: "say 'carried from July' rather than
  // showing a bare duplicate"), resolved from the retro's own month, not
  // from the opaque source-action id on the wire.
  it("an action carried from last month says so", async () => {
    renderDetail(
      retroFixture({ month: "2026-07", actions: [actionFixture({ carriedFrom: "prev-action-id" })] }),
    );

    const row = await screen.findByTestId("retro-action-a1");
    expect(row).toHaveTextContent("Carried from June");
  });

  // An action nobody carried (carriedFrom: "") shows no provenance line at
  // all -- the guard's other branch, proven directly rather than only by
  // the positive case above happening to pass.
  it("shows no carried-from line for an action nobody carried", async () => {
    renderDetail(retroFixture({ actions: [actionFixture({ carriedFrom: "" })] }));

    const row = await screen.findByTestId("retro-action-a1");
    expect(row).not.toHaveTextContent(/Carried from/);
  });

  // Both halves assert the SAME element (retro-notes) so deleting either the
  // heading or the body text alone still turns one of these red -- the
  // review's own finding: asserting the heading in one test and the body in
  // the other left each free to delete without the suite noticing.
  it("shows the notes card only when notes are present", async () => {
    renderDetail(retroFixture({ notes: "Best month this year." }));
    // Literal strings, not RETRO_COPY's own exports -- RetrosPage.test.tsx's
    // own header comment: importing the copy module here would make this
    // assertion tautological against a typo in that same module.
    const notes = await screen.findByTestId("retro-notes");
    expect(notes).toHaveTextContent("Notes");
    expect(notes).toHaveTextContent("Best month this year.");
  });

  it("renders no notes card when notes are blank", async () => {
    renderDetail(retroFixture({ notes: "" }));
    await screen.findByTestId("retro-detail-heading");
    expect(screen.queryByTestId("retro-notes")).not.toBeInTheDocument();
  });

  // A fresh draft (no completion date, no mood yet) renders no meta line at
  // all -- never an empty `<p>` sitting next to the heading.
  it("renders no meta line for a draft with neither a completion date nor a mood", async () => {
    renderDetail(retroFixture({ completedAt: null, mood: null }));

    await screen.findByTestId("retro-detail-heading");
    expect(screen.queryByTestId("retro-detail-meta")).not.toBeInTheDocument();
  });

  it("a failed load shows the detail-specific error, not the history list's own copy", async () => {
    stubFetchRoutes({
      "GET /api/v1/retros/2026-07": { status: 500, body: { error: { code: "INTERNAL", message: "broke" } } },
      "GET /api/v1/household/members": MEMBERS,
    });
    renderWithRouter(<RetroDetail month="2026-07" />);

    const error = await screen.findByTestId("retro-detail-load-error");
    expect(error).toHaveTextContent("Couldn't load this retro.");
  });
});
