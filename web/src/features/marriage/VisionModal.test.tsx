// Follows RetroModal.test.tsx's own shape: renderWithRouter for a fresh
// QueryClient, stubFetchRoutes for every request (it throws on anything
// unregistered), `fireEvent` rather than `@testing-library/user-event`
// (SignUpScreen.test.tsx's own comment: that package is not a dependency
// anywhere in this codebase), and a local `instrument` wrapper around
// stubFetchRoutes' own per-route `capture` for the two tests that need to
// read what was posted (RetroModal.test.tsx's own header comment records
// why: `stub.bodyOf(...)`/`.called(...)` don't exist on stubFetchRoutes'
// own return value).
//
// VisionModal takes `year`/`onYearChange`/the pieces of `useVision(year)`
// it needs as props rather than calling the hook itself (VisionModal.tsx's
// own header comment explains why) -- `Harness` below is what VisionPage.tsx
// actually is from this modal's point of view: it owns `year` and the one
// `useVision(year)` call, and re-renders the modal with fresh props when
// either changes. Every test renders `Harness`, never `VisionModal` bare,
// so the year select's own effect on the underlying fetch is exercised for
// real rather than asserted against a callback spy.
import { useState } from "react";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { VisionModal } from "./VisionModal";
import { useVision } from "./useVision";
import type { Vision, VisionMeasure, VisionPillar } from "./visionSchemas";
import type { GoalsResponse } from "../money/goalSchemas";

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-08-15T12:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function measureFixture(overrides: Partial<VisionMeasure> = {}): VisionMeasure {
  return {
    label: "Date nights / month",
    kind: "typed",
    hasFigure: true,
    current: 2,
    target: 2,
    percent: 100,
    met: true,
    goalId: "",
    goalName: "",
    ...overrides,
  };
}

function pillarFixture(overrides: Partial<VisionPillar> = {}): VisionPillar {
  return {
    name: "Us before logistics",
    description: "We're partners first, co-managers of a household second.",
    measures: [],
    ...overrides,
  };
}

function visionFixture(overrides: Partial<Vision> = {}): Vision {
  return {
    year: 2026,
    theme: "Slow down together",
    description: "Fewer commitments, more presence.",
    version: 3,
    pillars: [],
    milestones: [],
    ...overrides,
  };
}

// One live goal (Task 12's own picker needs at least one to pick), used as
// every test's default -- includeArchived: true (VisionModal.tsx's own
// comment on why) means the request always carries the query string below,
// regardless of whether a given test ever touches the linked-measure path.
const GOALS_RESPONSE: GoalsResponse = {
  currency: "SGD",
  goals: [
    {
      id: "goal-1",
      name: "Emergency fund",
      targetMinor: 500000,
      currency: "SGD",
      targetMonth: null,
      plannedMonthlyMinor: 0,
      contributedMinor: 100000,
      percent: 20,
      status: "on_track",
      requiredMonthlyMinor: 0,
      requiredMonthlyOk: true,
      archivedAt: null,
    },
  ],
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

// VisionPage.tsx's own shape, reduced to the one thing this file's tests
// need from it: `year` state, the single `useVision(year)` call, and the
// props VisionModal.tsx declares. Real re-renders on a real year change,
// not a callback spy standing in for one -- see this file's own header
// comment.
function Harness({ initialYear, onClose }: { initialYear: number; onClose: () => void }) {
  const [year, setYear] = useState(initialYear);
  const vision = useVision(year);
  return (
    <VisionModal
      year={year}
      onYearChange={setYear}
      data={vision.data}
      loading={vision.loading}
      error={vision.error}
      saveVision={vision.saveVision}
      isSaving={vision.isSaving}
      reload={vision.reload}
      onClose={onClose}
    />
  );
}

type RouteEntry = RouteResponse | RouteResponse[];

// RetroModal.test.tsx's own instrument, verbatim shape: wraps stubFetchRoutes
// so a test can read what a route was posted and in what order, without
// growing stubFetchRoutes itself for two call sites.
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
    countOf: (key: string) => order.filter((entry) => entry === key).length,
  };
}

function renderModal(vision: Vision, extraRoutes: Record<string, RouteEntry> = {}, initialYear = 2026) {
  const onClose = vi.fn();
  const stub = instrument({
    [`GET /api/v1/marriage/vision?year=${initialYear}`]: { status: 200, body: { vision } },
    "GET /api/v1/goals?include_archived=true": { status: 200, body: GOALS_RESPONSE },
    ...extraRoutes,
  });
  renderWithRouter(<Harness initialYear={initialYear} onClose={onClose} />);
  return { ...stub, onClose };
}

describe("VisionModal", () => {
  it("opens prefilled from the loaded vision, including each pillar's description and measures -- the two fields the design's own modal omits", async () => {
    renderModal(
      visionFixture({
        theme: "Slow down together",
        description: "Fewer commitments, more presence.",
        pillars: [
          pillarFixture({
            name: "Us before logistics",
            description: "We're partners first, co-managers of a household second.",
            measures: [measureFixture({ label: "Date nights / month", current: 2, target: 2 })],
          }),
        ],
        milestones: [{ year: 2027, title: "Sabbatical month in Indonesia", note: "Kids meet properly" }],
      }),
    );

    expect(await screen.findByTestId("vision-modal-theme")).toHaveValue("Slow down together");
    expect(screen.getByTestId("vision-modal-description")).toHaveValue("Fewer commitments, more presence.");

    // The theme comes before the year, as the design's own row does
    // (dc.html:932, theme 1fr / year 150px). The theme is what the modal is
    // for; the year only says which one is being set -- so it leads, in
    // reading order and in tab order. DOCUMENT_POSITION_FOLLOWING asserts
    // the relationship rather than either element's coordinates, which
    // jsdom cannot lay out.
    const themeField = screen.getByTestId("vision-modal-theme");
    const yearField = screen.getByTestId("vision-modal-year");
    expect(
      themeField.compareDocumentPosition(yearField) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();

    const pillar = screen.getByTestId("vision-modal-pillar");
    expect(within(pillar).getByTestId("vision-modal-pillar-name")).toHaveValue("Us before logistics");
    // The field the design's own modal draws no box for at all (spec
    // decision 7) -- if this is missing, the pillar's description is
    // permanently unreachable through the UI even though PillarCard.tsx
    // already renders it on the page behind this modal.
    expect(within(pillar).getByTestId("vision-modal-pillar-description")).toHaveValue(
      "We're partners first, co-managers of a household second.",
    );

    // The other field decision 7 names: a measure editor per pillar.
    const measure = within(pillar).getByTestId("vision-modal-measure");
    expect(within(measure).getByTestId("vision-modal-measure-label")).toHaveValue("Date nights / month");
    expect(within(measure).getByTestId("vision-modal-measure-current")).toHaveValue("2");
    expect(within(measure).getByTestId("vision-modal-measure-target")).toHaveValue("2");

    const milestone = screen.getByTestId("vision-modal-milestone");
    expect(within(milestone).getByTestId("vision-modal-milestone-year")).toHaveValue("2027");
    expect(within(milestone).getByTestId("vision-modal-milestone-title")).toHaveValue(
      "Sabbatical month in Indonesia",
    );
    expect(within(milestone).getByTestId("vision-modal-milestone-note")).toHaveValue("Kids meet properly");
  });

  it("+ Add pillar appends an empty pillar; its ✕ removes it", async () => {
    renderModal(visionFixture({ pillars: [] }));
    await screen.findByTestId("vision-modal-theme");
    expect(screen.queryAllByTestId("vision-modal-pillar")).toHaveLength(0);

    fireEvent.click(screen.getByTestId("vision-modal-add-pillar"));

    const pillars = screen.getAllByTestId("vision-modal-pillar");
    expect(pillars).toHaveLength(1);
    expect(within(pillars[0]).getByTestId("vision-modal-pillar-name")).toHaveValue("");
    expect(within(pillars[0]).getByTestId("vision-modal-pillar-description")).toHaveValue("");

    fireEvent.click(within(pillars[0]).getByTestId("vision-modal-remove-pillar"));
    expect(screen.queryAllByTestId("vision-modal-pillar")).toHaveLength(0);
  });

  // The measure editor is the new part decision 7 asks for, and the rule
  // that governs it (setMeasureMode's own comment): a measure is typed OR
  // linked, never both, so switching modes must clear the other mode's
  // inputs rather than merely hide them. `instrument`, not the plain
  // `renderModal`, because the load-bearing assertion here is what actually
  // gets POSTed once a measure has visited linked mode and come back to
  // typed -- a hidden `goalId` that survives the round trip is invisible to
  // any assertion that only reads the (typed-mode) DOM, since typed mode
  // renders no goal field to read it back from at all. That is exactly the
  // gap this task's own Step 5 mutation check (preserving `goalId` when
  // switching to typed) exposed against an earlier version of this test
  // that only checked the visible fields: switching to typed rendered
  // current/target back at 0/1 either way, and switching to linked a
  // second time always clears `goalId` unconditionally regardless of what
  // the typed branch did -- so neither visible-only assertion could ever
  // fail, only a look at the submitted body can.
  it("+ Add measure appends a measure to the pillar; switching between typed and linked clears the other mode's inputs", async () => {
    const stub = instrument({
      "GET /api/v1/marriage/vision?year=2026": {
        status: 200,
        body: { vision: visionFixture({ version: 3, pillars: [pillarFixture({ measures: [] })] }) },
      },
      "GET /api/v1/goals?include_archived=true": { status: 200, body: GOALS_RESPONSE },
      "PUT /api/v1/marriage/vision/2026": { status: 200, body: { vision: visionFixture({ version: 4 }) } },
    });
    const onClose = vi.fn();
    renderWithRouter(<Harness initialYear={2026} onClose={onClose} />);

    const pillar = await screen.findByTestId("vision-modal-pillar");
    fireEvent.click(within(pillar).getByTestId("vision-modal-add-measure"));
    const measure = within(pillar).getByTestId("vision-modal-measure");
    expect(within(measure).getByTestId("vision-modal-measure-current")).toHaveValue("0");
    expect(within(measure).getByTestId("vision-modal-measure-target")).toHaveValue("1");

    fireEvent.change(within(measure).getByTestId("vision-modal-measure-current"), { target: { value: "5" } });
    fireEvent.change(within(measure).getByTestId("vision-modal-measure-target"), { target: { value: "10" } });
    fireEvent.change(within(measure).getByTestId("vision-modal-measure-mode"), { target: { value: "linked" } });

    // The typed fields are gone, not merely hidden with old values intact.
    expect(within(measure).queryByTestId("vision-modal-measure-current")).not.toBeInTheDocument();
    expect(within(measure).queryByTestId("vision-modal-measure-target")).not.toBeInTheDocument();
    const goalSelect = within(measure).getByTestId("vision-modal-measure-goal");
    expect(goalSelect).toHaveValue("");

    fireEvent.change(goalSelect, { target: { value: "goal-1" } });
    expect(goalSelect).toHaveValue("goal-1");

    fireEvent.change(within(measure).getByTestId("vision-modal-measure-mode"), { target: { value: "typed" } });
    expect(within(measure).getByTestId("vision-modal-measure-current")).toHaveValue("0");
    expect(within(measure).getByTestId("vision-modal-measure-target")).toHaveValue("1");
    // No goal field renders in typed mode at all -- if `goalId` survived
    // the switch, this is the only place left to catch it.
    expect(within(measure).queryByTestId("vision-modal-measure-goal")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Save vision" }));
    await waitFor(() => expect(stub.called("PUT /api/v1/marriage/vision/2026")).toBe(true));
    // current/target are back at the typed defaults (0/1), not the 5/10
    // typed before the round trip through linked mode -- that value is
    // correctly gone too, the same clearing rule in the other direction.
    // goalId: "" is the assertion this test exists for.
    expect(stub.bodyOf("PUT /api/v1/marriage/vision/2026")).toMatchObject({
      pillars: [{ measures: [{ kind: "typed", current: 0, target: 1, goalId: "" }] }],
    });
  });

  // A household that switches a measure to "A savings goal" and saves
  // before picking one has picked NEITHER -- the bug this test guards
  // against is the server's old, shared VISION_MEASURE_INVALID message
  // telling them they'd picked BOTH. Checked client-side, before the round
  // trip: the PUT must never fire at all.
  it("Save vision refuses a measure switched to a goal with none picked, without reaching the server", async () => {
    const stub = instrument({
      "GET /api/v1/marriage/vision?year=2026": {
        status: 200,
        body: { vision: visionFixture({ version: 3, pillars: [pillarFixture({ measures: [] })] }) },
      },
      "GET /api/v1/goals?include_archived=true": { status: 200, body: GOALS_RESPONSE },
      "PUT /api/v1/marriage/vision/2026": { status: 200, body: { vision: visionFixture({ version: 4 }) } },
    });
    renderWithRouter(<Harness initialYear={2026} onClose={vi.fn()} />);

    const pillar = await screen.findByTestId("vision-modal-pillar");
    fireEvent.click(within(pillar).getByTestId("vision-modal-add-measure"));
    const measure = within(pillar).getByTestId("vision-modal-measure");
    fireEvent.change(within(measure).getByTestId("vision-modal-measure-mode"), { target: { value: "linked" } });
    expect(within(measure).getByTestId("vision-modal-measure-goal")).toHaveValue("");

    fireEvent.click(screen.getByRole("button", { name: "Save vision" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Pick a savings goal for that measure, or switch it back to a number you keep.",
    );
    expect(stub.called("PUT /api/v1/marriage/vision/2026")).toBe(false);
  });

  it("+ Add milestone appends a row with year, title and note", async () => {
    renderModal(visionFixture({ milestones: [] }));
    await screen.findByTestId("vision-modal-theme");
    expect(screen.queryByTestId("vision-modal-milestone")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("vision-modal-add-milestone"));

    const row = screen.getByTestId("vision-modal-milestone");
    // Seeded with the vision's own year (2026), not left blank.
    expect(within(row).getByTestId("vision-modal-milestone-year")).toHaveValue("2026");
    expect(within(row).getByTestId("vision-modal-milestone-title")).toHaveValue("");
    expect(within(row).getByTestId("vision-modal-milestone-note")).toHaveValue("");

    fireEvent.change(within(row).getByTestId("vision-modal-milestone-title"), {
      target: { value: "Sabbatical month in Indonesia" },
    });
    fireEvent.change(within(row).getByTestId("vision-modal-milestone-note"), {
      target: { value: "Tied to car + housing goals" },
    });
    expect(within(row).getByTestId("vision-modal-milestone-title")).toHaveValue("Sabbatical month in Indonesia");
    expect(within(row).getByTestId("vision-modal-milestone-note")).toHaveValue("Tied to car + housing goals");

    fireEvent.click(within(row).getByTestId("vision-modal-remove-milestone"));
    expect(screen.queryByTestId("vision-modal-milestone")).not.toBeInTheDocument();
  });

  // The body-shape test: every field the modal holds, including one edited
  // live rather than merely carried through from the seed -- a modal that
  // quietly dropped a field on save would still pass a shallower assertion
  // that only checked, say, the theme. `toEqual`, not `toMatchObject`: an
  // extra draft-only key leaking into the PUT body must fail this test too.
  it("Save vision submits every field, including a pillar whose description was edited", async () => {
    const stub = instrument({
      "GET /api/v1/marriage/vision?year=2026": {
        status: 200,
        body: {
          vision: visionFixture({
            version: 3,
            pillars: [
              pillarFixture({
                name: "Us before logistics",
                description: "Old description.",
                measures: [measureFixture({ label: "Date nights / month", current: 2, target: 2 })],
              }),
            ],
            milestones: [
              { year: 2027, title: "Sabbatical month in Indonesia", note: "Kids meet Christine's side properly" },
            ],
          }),
        },
      },
      "GET /api/v1/goals?include_archived=true": { status: 200, body: GOALS_RESPONSE },
      "PUT /api/v1/marriage/vision/2026": {
        status: 200,
        body: { vision: visionFixture({ version: 4 }) },
      },
    });
    const onClose = vi.fn();
    renderWithRouter(<Harness initialYear={2026} onClose={onClose} />);

    const pillar = await screen.findByTestId("vision-modal-pillar");
    fireEvent.change(within(pillar).getByTestId("vision-modal-pillar-description"), {
      target: { value: "New description, written together this January." },
    });

    fireEvent.click(screen.getByRole("button", { name: "Save vision" }));

    await waitFor(() => expect(stub.called("PUT /api/v1/marriage/vision/2026")).toBe(true));
    expect(stub.bodyOf("PUT /api/v1/marriage/vision/2026")).toEqual({
      version: 3,
      theme: "Slow down together",
      description: "Fewer commitments, more presence.",
      pillars: [
        {
          name: "Us before logistics",
          description: "New description, written together this January.",
          measures: [{ label: "Date nights / month", kind: "typed", current: 2, target: 2, goalId: "" }],
        },
      ],
      milestones: [
        { year: 2027, title: "Sabbatical month in Indonesia", note: "Kids meet Christine's side properly" },
      ],
    });
    expect(onClose).toHaveBeenCalled();
  });

  it("the year select offers exactly the previous, current and next year", async () => {
    renderModal(visionFixture());

    const select = await screen.findByTestId("vision-modal-year");
    const values = within(select)
      .getAllByRole("option")
      .map((option) => (option as HTMLOptionElement).value);
    expect(values).toEqual(["2025", "2026", "2027"]);
  });

  // Ruling 1's own second half: the select that just caused a load must not
  // itself disappear while that load is in flight, or the household could
  // never change their mind mid-fetch. A hand-rolled fetch stub, not
  // stubFetchRoutes, because this needs a GET this test controls the
  // resolution of -- stubFetchRoutes always resolves synchronously.
  it("stays interactive and shows a loading state while a newly chosen year's document is still fetching", async () => {
    let resolve2025: (response: Response) => void = () => {};
    const pending2025 = new Promise<Response>((resolve) => {
      resolve2025 = resolve;
    });

    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      const key = `${method} ${String(input)}`;
      if (key === "GET /api/v1/marriage/vision?year=2026") {
        return new Response(JSON.stringify({ vision: visionFixture({ year: 2026, theme: "2026 theme" }) }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (key === "GET /api/v1/marriage/vision?year=2025") {
        return pending2025;
      }
      if (key === "GET /api/v1/goals?include_archived=true") {
        return new Response(JSON.stringify(GOALS_RESPONSE), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      throw new Error(`unstubbed request: ${key}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const onClose = vi.fn();
    renderWithRouter(<Harness initialYear={2026} onClose={onClose} />);
    expect(await screen.findByTestId("vision-modal-theme")).toHaveValue("2026 theme");

    fireEvent.change(screen.getByTestId("vision-modal-year"), { target: { value: "2025" } });

    // The select's own value updates immediately and stays on screen --
    // it is not gated behind the fetch it just started.
    expect(screen.getByTestId("vision-modal-year")).toHaveValue("2025");
    expect(screen.queryByTestId("vision-modal-theme")).not.toBeInTheDocument();
    expect(await screen.findByText("Loading…")).toBeInTheDocument();

    resolve2025(
      new Response(JSON.stringify({ vision: visionFixture({ year: 2025, theme: "2025 theme" }) }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    expect(await screen.findByTestId("vision-modal-theme")).toHaveValue("2025 theme");
  });

  // The conflict, with copy that tells the household what happened and what
  // to do, not a generic "couldn't save" -- and without silently discarding
  // what they typed before they choose to. See VisionModal.tsx's own header
  // comment for the full reasoning behind detecting this from `err.code`
  // (not the hook's own `conflict`) while still using `reload()` as the
  // banner's own action.
  it("a 409 renders the reload message, not a generic failure, and Reload discards the draft and closes", async () => {
    const stub = instrument({
      "GET /api/v1/marriage/vision?year=2026": [
        { status: 200, body: { vision: visionFixture({ version: 2 }) } },
        { status: 200, body: { vision: visionFixture({ version: 5, theme: "Their update" }) } },
      ],
      "GET /api/v1/goals?include_archived=true": { status: 200, body: GOALS_RESPONSE },
      "PUT /api/v1/marriage/vision/2026": {
        status: 409,
        body: { error: { code: "VISION_CHANGED", message: "This vision changed while you were editing it." } },
      },
    });
    const onClose = vi.fn();
    renderWithRouter(<Harness initialYear={2026} onClose={onClose} />);

    const themeField = await screen.findByTestId("vision-modal-theme");
    fireEvent.change(themeField, { target: { value: "My own edit" } });
    fireEvent.click(screen.getByRole("button", { name: "Save vision" }));

    const banner = await screen.findByTestId("vision-conflict");
    expect(banner).toHaveTextContent(
      "Someone else saved this year's vision while you were editing",
    );
    // Not the generic fallback -- the two are mutually exclusive on screen.
    expect(screen.queryByText("Couldn't save this year's vision. Try again.")).not.toBeInTheDocument();
    // Refused, not silently merged or wiped: what was typed is still there.
    expect(screen.getByTestId("vision-modal-theme")).toHaveValue("My own edit");

    fireEvent.click(within(banner).getByTestId("vision-conflict-reload"));

    await waitFor(() => expect(stub.countOf("GET /api/v1/marriage/vision?year=2026")).toBe(2));
    expect(onClose).toHaveBeenCalled();
  });
});
