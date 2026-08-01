// Follows BudgetPage.test.tsx's own shape: renderWithRouter (GoalsPage has
// no <Link> of its own today, but the same harness every money-feature test
// uses keeps this file consistent with its siblings) plus stubFetchRoutes
// for every request. Unlike BudgetPage, GoalsPage does not derive a month
// from the real calendar, so there is no Date faking here.
//
// Literal strings are asserted throughout, not GOAL_COPY's own exports --
// BudgetPage.test.tsx's own convention: importing the copy module here would
// make an assertion tautological against a typo in that same module.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { GoalsPage } from "./GoalsPage";
import type { Goal, GoalsResponse } from "./goalSchemas";

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

// The design's own Bali family trip figures (Household Dashboard.dc.html's
// is_goals screen), scaled into minor units: S$2,600 of S$4,000, 65%, on
// track, S$350/mo, target Dec 2026.
function goalFixture(overrides: Partial<Goal> = {}): Goal {
  return {
    id: "goal-1",
    name: "Bali family trip",
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

function goalsFixture(
  goals: Goal[],
  summaryOverrides: Partial<GoalsResponse["summary"]> = {},
): GoalsResponse {
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

function renderPage(
  response: GoalsResponse,
  extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  const fetchMock = stubFetchRoutes({
    "GET /api/v1/currencies": CURRENCIES,
    "GET /api/v1/goals": { status: 200, body: response },
    ...extraRoutes,
  });
  return { fetchMock, ...renderWithRouter(<GoalsPage />) };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("GoalsPage", () => {
  it("renders four goals as four cards with their formatted figures and percent labels", async () => {
    const goals = [
      goalFixture({ id: "g1", name: "Bali family trip", percent: 65 }),
      goalFixture({
        id: "g2",
        name: "Emergency fund",
        targetMinor: 3000000,
        contributedMinor: 1850000,
        percent: 62,
        plannedMonthlyMinor: 50000,
        targetMonth: "2027-06",
      }),
      goalFixture({
        id: "g3",
        name: "Kids' education",
        targetMinor: 12000000,
        contributedMinor: 4120000,
        percent: 34,
        plannedMonthlyMinor: 80000,
        targetMonth: "2032-01",
      }),
      goalFixture({
        id: "g4",
        name: "New family car",
        targetMinor: 3000000,
        contributedMinor: 360000,
        percent: 12,
        plannedMonthlyMinor: 40000,
        targetMonth: "2029-01",
      }),
    ];
    renderPage(goalsFixture(goals, { onTrackCount: 4, datedCount: 4 }));

    const cards = await screen.findAllByTestId("goal-card");
    expect(cards).toHaveLength(4);

    expect(cards[0]).toHaveTextContent("Bali family trip");
    expect(cards[0]).toHaveTextContent("65%");
    expect(cards[0]).toHaveTextContent("S$2,600.00 of S$4,000.00");
    expect(cards[0]).toHaveTextContent("by Dec 2026");
    expect(cards[0]).toHaveTextContent("S$350.00/mo");

    expect(cards[1]).toHaveTextContent("Emergency fund");
    expect(cards[1]).toHaveTextContent("62%");
    expect(cards[1]).toHaveTextContent("S$18,500.00 of S$30,000.00");
    expect(cards[1]).toHaveTextContent("S$500.00/mo");

    expect(cards[2]).toHaveTextContent("34%");
    expect(cards[3]).toHaveTextContent("12%");

    // No "from <account>" line anywhere -- spec decision 6, the design's own
    // "S$350/mo · from OCBC Joint" ships as "S$350/mo".
    expect(document.body.textContent).not.toMatch(/from OCBC/i);
    expect(document.body.textContent).not.toMatch(/\bfrom DBS\b/i);
  });

  it("a status: none goal renders no pill and no date clause", async () => {
    const goals = [
      goalFixture({
        status: "none",
        targetMonth: null,
        requiredMonthlyOk: false,
        requiredMonthlyMinor: 0,
      }),
    ];
    renderPage(goalsFixture(goals, { noDateCount: 1 }));

    const card = await screen.findByTestId("goal-card");
    expect(within(card).queryByTestId("goal-card-status")).not.toBeInTheDocument();
    expect(card).not.toHaveTextContent("by ");
    // Still shows progress, target and planned monthly (spec decision 3).
    expect(card).toHaveTextContent("S$2,600.00 of S$4,000.00");
    expect(card).toHaveTextContent("S$350.00/mo");
  });

  it("an achieved goal renders the achieved pill and a full ring", async () => {
    const goals = [
      goalFixture({
        status: "achieved",
        percent: 100,
        contributedMinor: 400000,
        requiredMonthlyOk: false,
        requiredMonthlyMinor: 0,
      }),
    ];
    renderPage(goalsFixture(goals));

    const card = await screen.findByTestId("goal-card");
    expect(within(card).getByTestId("goal-card-status")).toHaveTextContent("Achieved");
    expect(within(card).getByTestId("goal-card-ring")).toHaveTextContent("100%");
  });

  it("a behind goal names its required monthly figure", async () => {
    const goals = [
      goalFixture({ status: "behind", requiredMonthlyOk: true, requiredMonthlyMinor: 55000 }),
    ];
    renderPage(goalsFixture(goals, { onTrackCount: 0, datedCount: 1 }));

    const card = await screen.findByTestId("goal-card");
    expect(within(card).getByTestId("goal-card-status")).toHaveTextContent("Behind");
    expect(card).toHaveTextContent("Needs S$550.00/mo to catch up");
  });

  // A behind goal past its target month has requiredMonthlyOk: false (no
  // honest division exists -- domain.RequiredMonthlyMinor's own "ok" rule,
  // restated in usecase/goal.go's GoalView comment: false means no line, not
  // a zero standing in for a missing one). Pinned separately from the test
  // above so a card that renders the line unconditionally on status=="behind"
  // (ignoring requiredMonthlyOk) still fails somewhere.
  it("a behind goal past its target month names no required monthly figure", async () => {
    const goals = [
      goalFixture({ status: "behind", requiredMonthlyOk: false, requiredMonthlyMinor: 0 }),
    ];
    renderPage(goalsFixture(goals, { onTrackCount: 0, datedCount: 1 }));

    const card = await screen.findByTestId("goal-card");
    expect(within(card).getByTestId("goal-card-status")).toHaveTextContent("Behind");
    expect(within(card).queryByTestId("goal-card-required")).not.toBeInTheDocument();
  });

  it("renders the empty state with one Create your first goal action and no templates", async () => {
    renderPage(goalsFixture([]));

    const empty = await screen.findByTestId("goals-empty-state");
    expect(within(empty).getByTestId("goals-create-first")).toHaveTextContent("Create your first goal");
    // A goal has no category set to prefill (task brief) -- no template cards.
    expect(screen.queryByText(/template/i)).not.toBeInTheDocument();
    expect(screen.queryByTestId("goal-card")).not.toBeInTheDocument();
  });

  // FinancesPage.test.tsx's own precedent ("keeps the archived toggle
  // reachable after archiving a household's only account"): a household with
  // zero *live* goals but at least one archived one must still be able to
  // reach the archived view. The toggle must not be gated behind
  // `data.goals.length > 0`, or archiving a household's last goal would make
  // it permanently unrestorable through this screen.
  it("keeps the archived toggle reachable when there are no live goals to show", async () => {
    const archived = goalFixture({
      id: "g1",
      name: "Old family car",
      archivedAt: "2026-06-01T00:00:00Z",
      status: "none",
      targetMonth: null,
      requiredMonthlyOk: false,
      requiredMonthlyMinor: 0,
    });

    renderPage(goalsFixture([]), {
      "GET /api/v1/goals?include_archived=true": {
        status: 200,
        body: goalsFixture([archived]),
      },
    });

    // No live goals -- the empty state renders...
    expect(await screen.findByTestId("goals-empty-state")).toBeInTheDocument();
    // ...but the toggle is still there, not lost along with the list.
    const toggle = screen.getByRole("switch", { name: "Show archived" });

    fireEvent.click(toggle);

    expect(await screen.findByText("Old family car")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Restore Old family car" })).toBeInTheDocument();
  });

  // The dead end the Task 18 browser walk found at criterion 12: this page
  // shipped "Show archived" and a Restore button on every archived card,
  // useGoals.archiveGoal shipped with its own passing test -- and nothing on
  // any screen ever called it, so a household could never reach the archived
  // state these two tests above describe. Scoped with `within(card)` on
  // purpose: an unscoped getByRole would still pass if the button rendered
  // on the wrong card, or on the archived one that must offer Restore
  // instead.
  it("a live card offers Archive, which POSTs /goals/{id}/archive and refetches", async () => {
    const live = goalFixture({ id: "g1", name: "Bali family trip" });
    const archived = { ...live, archivedAt: "2026-08-01T00:00:00Z", status: "none" as const };
    let archiveCalled = false;

    renderPage(goalsFixture([live], { onTrackCount: 1, datedCount: 1 }), {
      // Two responses in order: the page's own initial GET, then the refetch
      // archiveGoal's onSuccess invalidation triggers. The second is what
      // proves the invalidation reaches this page's mounted query.
      "GET /api/v1/goals": [
        { status: 200, body: goalsFixture([live], { onTrackCount: 1, datedCount: 1 }) },
        { status: 200, body: goalsFixture([]) },
      ],
      "POST /api/v1/goals/g1/archive": {
        status: 200,
        body: { goal: archived },
        capture: () => {
          archiveCalled = true;
        },
      },
    });

    const card = await screen.findByTestId("goal-card");
    fireEvent.click(within(card).getByRole("button", { name: "Archive Bali family trip" }));

    await waitFor(() => expect(archiveCalled).toBe(true));
    // The goal left the default list, and the page says so itself rather
    // than leaving a stale card behind.
    expect(await screen.findByTestId("goals-empty-state")).toBeInTheDocument();
  });

  // An archived card offers Restore and NOT Archive -- the either/or
  // AccountRow keeps, so a card never states two contradictory affordances.
  it("an archived card offers Restore and no Archive", async () => {
    const archived = goalFixture({
      id: "g1",
      name: "Old family car",
      archivedAt: "2026-06-01T00:00:00Z",
      status: "none",
      targetMonth: null,
      requiredMonthlyOk: false,
      requiredMonthlyMinor: 0,
    });
    renderPage(goalsFixture([]), {
      "GET /api/v1/goals?include_archived=true": { status: 200, body: goalsFixture([archived]) },
    });

    await screen.findByTestId("goals-empty-state");
    fireEvent.click(screen.getByRole("switch", { name: "Show archived" }));

    const card = await screen.findByTestId("goal-card");
    expect(within(card).getByRole("button", { name: "Restore Old family car" })).toBeInTheDocument();
    expect(
      within(card).queryByRole("button", { name: "Archive Old family car" }),
    ).not.toBeInTheDocument();
  });

  // AccountsPanel.tsx's own `noneArchived` note, restated: switching the
  // toggle on with nothing archived behind it should say so, not silently
  // re-render the identical live list with no explanation for why nothing
  // changed.
  it("says so when the toggle is on and nothing is archived", async () => {
    const live = goalFixture({ id: "g1", name: "Bali family trip" });
    renderPage(goalsFixture([live], { onTrackCount: 1, datedCount: 1 }), {
      "GET /api/v1/goals?include_archived=true": {
        status: 200,
        body: goalsFixture([live], { onTrackCount: 1, datedCount: 1 }),
      },
    });

    expect(await screen.findByText("Bali family trip")).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("switch", { name: "Show archived" }));

    expect(await screen.findByTestId("goals-archived-empty")).toHaveTextContent("No archived goals.");
  });

  it('Show archived refetches with ?include_archived=true and renders archived goals alongside the live ones', async () => {
    const live = goalFixture({ id: "g1", name: "Bali family trip" });
    const archived = goalFixture({
      id: "g2",
      name: "New family car",
      archivedAt: "2026-06-01T00:00:00Z",
      status: "none",
      targetMonth: null,
      requiredMonthlyOk: false,
      requiredMonthlyMinor: 0,
    });
    // Both responses carry the identical summary -- goalsSummarySchema's own
    // comment: an archived goal is in no count, whether or not it is in the
    // `goals` array. This is what makes "the header counts did not change"
    // below a real assertion rather than a coincidence of the fixture.
    const summary = { onTrackCount: 1, datedCount: 1, noDateCount: 0 };

    renderPage(goalsFixture([live], summary), {
      "GET /api/v1/goals?include_archived=true": {
        status: 200,
        body: goalsFixture([live, archived], summary),
      },
    });

    expect(await screen.findByText("Bali family trip")).toBeInTheDocument();
    const subtitleBefore = (await screen.findByTestId("goals-subtitle")).textContent;

    fireEvent.click(await screen.findByRole("switch", { name: "Show archived" }));

    const cards = await waitFor(() => {
      const found = screen.getAllByTestId("goal-card");
      expect(found).toHaveLength(2);
      return found;
    });
    // The live goal is still on screen -- pins the union against a
    // filter-swap reading, which would show only the archived one here.
    expect(screen.getByText("Bali family trip")).toBeInTheDocument();

    const archivedCard = cards.find((card) => card.textContent?.includes("New family car"));
    expect(archivedCard).toBeDefined();
    expect(within(archivedCard!).getByText("(archived)")).toBeInTheDocument();
    expect(
      within(archivedCard!).getByRole("button", { name: "Restore New family car" }),
    ).toBeInTheDocument();

    // The header counts did not change.
    expect(screen.getByTestId("goals-subtitle").textContent).toBe(subtitleBefore);
  });

  it("excludedNoRate > 0 renders the exclusion note with its count, the ledger's copy shape", async () => {
    renderPage(
      goalsFixture([goalFixture()], { onTrackCount: 1, datedCount: 1, excludedNoRate: 2 }),
    );

    expect(await screen.findByTestId("goals-excluded-no-rate")).toHaveTextContent(
      "2 goals are not counted: no exchange rate.",
    );
  });

  // The interim Overview's one real defect was a page that rendered nothing
  // at all for exactly this member (docs/LEARNING.md pattern 2) -- every unit
  // test passed because each one asserted the absence of something, and
  // absence holds perfectly over a blank page. This asserts the explanation's
  // *presence*, not merely that the generic error is missing.
  it("a 403 from GET /goals renders the owner-only explanation, not the generic load error", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/goals": {
        status: 403,
        body: { error: { code: "FORBIDDEN", message: "Owners only." } },
      },
    });

    renderWithRouter(<GoalsPage />);

    const explanation = await screen.findByTestId("goals-owner-only");
    expect(explanation).toHaveTextContent("Owner only");
    expect(explanation).toHaveTextContent("Goals is visible to the household owner.");
    expect(screen.queryByTestId("goals-load-error")).not.toBeInTheDocument();
  });

  it("a non-403 failure renders the generic load error, not the owner-only explanation", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/goals": {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Something broke." } },
      },
    });

    renderWithRouter(<GoalsPage />);

    expect(await screen.findByTestId("goals-load-error")).toHaveTextContent("Couldn't load your goals.");
    expect(screen.queryByTestId("goals-owner-only")).not.toBeInTheDocument();
  });

  // Spec decision: the design's own automation lines in this slot --
  // "S$2,050 auto-saved on the 1st of each month" (header) and "next transfer
  // Aug 1" / "Auto-save each month" (elsewhere on the same screen) -- do not
  // ship. Pinned so a future copy-paste from the design cannot smuggle them
  // back in, the same guard BudgetPage.test.tsx uses for "rolls into".
  it("never renders automation copy", async () => {
    renderPage(goalsFixture([goalFixture()], { onTrackCount: 1, datedCount: 1 }));

    await screen.findByTestId("goals-page");
    expect(document.body.textContent).not.toContain("auto-saved");
    expect(document.body.textContent).not.toContain("next transfer");
    expect(document.body.textContent).not.toContain("Auto-save");
  });

  // Task 12's New/Edit modal (GoalModal.tsx) opens through state this page
  // owns (the task brief's own "Produces" line) -- three entry points, all
  // sharing the one `modalGoal` state and the one <GoalModal> render at the
  // bottom of the file. Task 12's own report flagged that no task in the
  // plan wired the modal in; these three tests (plus the mutation check
  // below) are that wiring's own coverage, replacing the two tests that used
  // to pin only the stub seam.
  it("clicking + New goal opens the create-goal modal, blank", async () => {
    renderPage(
      goalsFixture([goalFixture({ name: "Bali family trip" })], { onTrackCount: 1, datedCount: 1 }),
    );

    fireEvent.click(await screen.findByTestId("goals-new"));

    expect(await screen.findByLabelText("Goal name")).toHaveValue("");
    expect(screen.getByRole("button", { name: "Create goal" })).toBeInTheDocument();
    // Create mode only -- Starting balance exists exclusively at creation
    // (spec decision 8; GoalModal.tsx's own header comment on why).
    expect(screen.getByLabelText("Starting balance")).toBeInTheDocument();
  });

  it("clicking Create your first goal opens the create-goal modal, blank", async () => {
    renderPage(goalsFixture([]));

    fireEvent.click(await screen.findByTestId("goals-create-first"));

    expect(await screen.findByLabelText("Goal name")).toHaveValue("");
    expect(screen.getByRole("button", { name: "Create goal" })).toBeInTheDocument();
  });

  it("clicking a live card opens the edit modal, prefilled from that goal", async () => {
    renderPage(
      goalsFixture([goalFixture({ name: "Bali family trip" })], { onTrackCount: 1, datedCount: 1 }),
    );

    fireEvent.click(await screen.findByTestId("goal-card"));

    expect(await screen.findByLabelText("Goal name")).toHaveValue("Bali family trip");
    // Edit mode only -- Currency is locked once a goal exists (GoalModal.tsx
    // disables the control with its reason shown), and there is no
    // Starting balance field to prefill at all past creation.
    expect(screen.getByLabelText("Currency")).toBeDisabled();
    expect(screen.queryByLabelText("Starting balance")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
  });

  // An archived card opens nothing -- GoalCard.tsx's own
  // `clickable = !archived && Boolean(onEdit)` already refuses the click;
  // this pins that the page's wiring doesn't accidentally reopen the seam
  // for the archived case (e.g. by handing every card the same onEdit
  // regardless of archived state).
  it("clicking an archived card opens no modal", async () => {
    const archived = goalFixture({
      id: "g1",
      name: "Old family car",
      archivedAt: "2026-06-01T00:00:00Z",
      status: "none",
      targetMonth: null,
      requiredMonthlyOk: false,
      requiredMonthlyMinor: 0,
    });
    renderPage(goalsFixture([]), {
      "GET /api/v1/goals?include_archived=true": { status: 200, body: goalsFixture([archived]) },
    });

    await screen.findByTestId("goals-empty-state");
    fireEvent.click(screen.getByRole("switch", { name: "Show archived" }));
    fireEvent.click(await screen.findByText("Old family car"));

    expect(screen.queryByLabelText("Goal name")).not.toBeInTheDocument();
  });

  // The coordinator's own instruction: "verify rather than assume" that a
  // successful save closes the modal *and* the list reflects the change,
  // since useGoals.ts's createGoal/updateGoal invalidate on success rather
  // than patching local state -- this end-to-end loop is the only place
  // that invalidate-then-refetch behaviour is actually exercised through a
  // real page mount rather than asserted about in isolation.
  it("saving an edit closes the modal and the card shows the new figure, with no extra call this page had to make", async () => {
    const goal = goalFixture({ id: "goal-1", name: "Bali family trip", plannedMonthlyMinor: 35000 });
    const renamed = goalFixture({ id: "goal-1", name: "Bali family trip 2027", plannedMonthlyMinor: 40000 });
    let patchBody: unknown;

    renderPage(goalsFixture([goal], { onTrackCount: 1, datedCount: 1 }), {
      // Two responses, consumed in order: the page's own initial GET, then
      // the refetch createGoal/updateGoal's onSuccess invalidation triggers
      // -- this second entry is what proves the invalidation actually
      // reaches this page's mounted query, not just that useGoals.ts calls
      // invalidateQueries in isolation.
      "GET /api/v1/goals": [
        { status: 200, body: goalsFixture([goal], { onTrackCount: 1, datedCount: 1 }) },
        { status: 200, body: goalsFixture([renamed], { onTrackCount: 1, datedCount: 1 }) },
      ],
      [`PATCH /api/v1/goals/${goal.id}`]: {
        status: 200,
        body: { goal: renamed },
        capture: (body) => {
          patchBody = body;
        },
      },
    });

    fireEvent.click(await screen.findByTestId("goal-card"));
    fireEvent.change(await screen.findByLabelText("Goal name"), {
      target: { value: "Bali family trip 2027" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(patchBody).toBeDefined());
    // The modal is gone -- onSaved/onClose both fired.
    expect(screen.queryByLabelText("Goal name")).not.toBeInTheDocument();
    // And the card now shows what the refetch (not the PATCH response,
    // which this page never reads directly) came back with.
    expect(await screen.findByText("Bali family trip 2027")).toBeInTheDocument();
    expect(screen.getByText("S$400.00/mo")).toBeInTheDocument();
  });
});
