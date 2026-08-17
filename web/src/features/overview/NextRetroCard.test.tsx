// NextRetroCard.tsx owns its own useRetros call, the same reason
// NextBillCard.test.tsx tests that component the "stub the route, mount the
// real component, wait for the request to settle" way rather than the
// GoalsCard.test.tsx way (an already-typed prop).
import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { currentMonth } from "../money/month";
import { monthNameOnly } from "../marriage/retroCopy";
import type { RetroSummary, RetrosResponse } from "../marriage/retroSchemas";
import { OVERVIEW_COPY } from "./copy";
import { NextRetroCard } from "./NextRetroCard";

const MONTH = currentMonth();

// A minimal, schema-valid RetroSummary (retroSummarySchema's own required
// fields) -- every field present rather than optional, matching
// retro_handlers.go's toRetroSummaryDTO, which never omits one.
function summaryFixture(overrides: Partial<RetroSummary> = {}): RetroSummary {
  return {
    id: "retro-1",
    month: MONTH,
    mood: null,
    actionCount: 0,
    quote: "",
    finished: false,
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

function renderCard(retrosResponse: RetrosResponse, extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {}) {
  const fetchMock = stubFetchRoutes({
    "GET /api/v1/retros": { status: 200, body: retrosResponse },
    ...extraRoutes,
  });
  return { fetchMock, ...renderWithRouter(<NextRetroCard />) };
}

describe("NextRetroCard", () => {
  it("shows the current month's draft retro and its own action count", async () => {
    renderCard(retrosFixture({ retros: [summaryFixture({ month: MONTH, finished: false, actionCount: 2 })] }));

    const card = await screen.findByTestId("next-retro-card");
    expect(card).toHaveTextContent(`${monthNameOnly(MONTH)} retro`);
    expect(card).toHaveTextContent(OVERVIEW_COPY.nextRetroInProgress);
    expect(card).toHaveTextContent(OVERVIEW_COPY.nextRetroActions(2));
  });

  // Distinct from the draft above: a finished retro has nothing left to
  // flag as unfinished, so "In progress" must not linger once Finish has
  // been clicked -- the same "the label answers a real question, not just
  // 'is there a row'" rule RetroHistoryList.tsx's own draftInProgress
  // guards.
  it("does not call a finished current-month retro in progress", async () => {
    renderCard(retrosFixture({ retros: [summaryFixture({ month: MONTH, finished: true, actionCount: 1 })] }));

    const card = await screen.findByTestId("next-retro-card");
    expect(card).toHaveTextContent(OVERVIEW_COPY.nextRetroActions(1));
    expect(card).not.toHaveTextContent(OVERVIEW_COPY.nextRetroInProgress);
  });

  // A finished retro nobody added an action to -- task-11-report.md's own
  // fix for the identical "0 actions" defect in RetroHistoryList.tsx is the
  // precedent this card follows too: omit the clause outright, never print
  // a zero.
  it("never prints a zero action count", async () => {
    renderCard(retrosFixture({ retros: [summaryFixture({ month: MONTH, finished: true, actionCount: 0 })] }));

    const card = await screen.findByTestId("next-retro-card");
    expect(card).not.toHaveTextContent(/0 action/);
  });

  it("prompts to start when the current month has no retro yet, linking to the Retros screen", async () => {
    renderCard(retrosFixture({ retros: [], startMonth: "2025-07" }));

    expect(await screen.findByText(OVERVIEW_COPY.nextRetroNone)).toBeInTheDocument();
    const link = await screen.findByRole("link", { name: OVERVIEW_COPY.nextRetroStart(monthNameOnly("2025-07")) });
    expect(link).toHaveAttribute("href", "/marriage/retros");
  });

  // retrosResponseSchema's own nullability allows startMonth to be null
  // with no current-month retro either -- a shape RetroService.List should
  // never actually produce (both candidates already have a retro is the
  // only real reason it's null), but this component fails closed against
  // it rather than calling nextRetroStart with a month that was not there.
  it("falls back to a plain link when nothing is startable either", async () => {
    renderCard(retrosFixture({ retros: [], startMonth: null }));

    expect(await screen.findByText(OVERVIEW_COPY.nextRetroNone)).toBeInTheDocument();
    const link = screen.getByRole("link", { name: OVERVIEW_COPY.nextRetroGo });
    expect(link).toHaveAttribute("href", "/marriage/retros");
  });

  // Mirrors NextBillCard.test.tsx's identical state: still loading, still
  // errored, or (this card's own case) simply never mounted for a member
  // without marriage -- none of the three has a figure to show, so this
  // proves the query's OWN in-flight state renders nothing rather than a
  // heading with nothing beneath it. The wrapper div is unconditional
  // static markup, so waiting for IT (not racing NextRetroCard's own render)
  // accounts for the router's own async initial transition
  // (NextBillCard.test.tsx's own comment on why the first query there is
  // `find`, not `get`, states the identical reason).
  it("renders nothing before its query has resolved", async () => {
    stubFetchRoutes({ "GET /api/v1/retros": { status: 200, body: retrosFixture() } });
    renderWithRouter(
      <div data-testid="next-retro-card-wrapper">
        <NextRetroCard />
      </div>,
    );

    const wrapper = await screen.findByTestId("next-retro-card-wrapper");
    expect(wrapper).toBeEmptyDOMElement();
  });
});
