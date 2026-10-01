// NextRetroCard.tsx owns its own useRetros call, the same reason
// NextBillCard.test.tsx tests that component the "stub the route, mount the
// real component, wait for the request to settle" way rather than the
// GoalsCard.test.tsx way (an already-typed prop). It also owns its own
// useVision call now, for the Vision check-in strip -- VisionCard.tsx's own
// header comment explains why that hook is mounted per-component rather than
// lifted to OverviewPage.
import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { meRoute } from "../../test/meFixture";
import { monthIn, yearIn } from "../../lib/householdDate";
import { monthNameOnly, nextMonthName } from "../marriage/retroCopy";
import type { RetroSummary, RetrosResponse } from "../marriage/retroSchemas";
import type { Vision } from "../marriage/visionSchemas";
import { OVERVIEW_COPY } from "./copy";
import { NextRetroCard } from "./NextRetroCard";

// The household these tests sign in as keeps its calendar in this zone, and
// the component reads "this month" and "this year" in it. The expected month
// and year are worked out in the same zone, so the two cannot disagree on the
// day the suite happens to run.
const ZONE = "Asia/Singapore";
const MONTH = monthIn(ZONE);
const YEAR = yearIn(ZONE);

// A minimal, schema-valid RetroSummary (retroSummarySchema's own required
// fields) -- every field present rather than optional, matching
// retro_handlers.go's toRetroSummaryDTO, which never omits one.
function summaryFixture(overrides: Partial<RetroSummary> = {}): RetroSummary {
  return {
    id: "retro-1",
    month: MONTH,
    mood: null,
    actionCount: 0,
    openActionCount: 0,
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

// theme: "" by default -- the same wire shape a never-set year sends
// (visionSchema's own version: 0 comment) -- so every test in this file that
// doesn't care about the strip keeps seeing it absent, the same
// empty-default convention retrosFixture/summaryFixture already follow.
function visionFixture(overrides: Partial<Vision> = {}): Vision {
  return {
    year: YEAR,
    theme: "",
    description: "",
    version: 0,
    pillars: [],
    milestones: [],
    ...overrides,
  };
}

function renderCard(retrosResponse: RetrosResponse, extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {}) {
  const fetchMock = stubFetchRoutes({
    ...meRoute(ZONE),
    "GET /api/v1/retros": { status: 200, body: retrosResponse },
    [`GET /api/v1/marriage/vision?year=${YEAR}`]: { status: 200, body: { vision: visionFixture() } },
    ...extraRoutes,
  });
  return { fetchMock, ...renderWithRouter(<NextRetroCard />) };
}

const ORIGINAL_TZ = process.env.TZ;

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  if (ORIGINAL_TZ === undefined) delete process.env.TZ;
  else process.env.TZ = ORIGINAL_TZ;
});

describe("NextRetroCard", () => {
  // actionCount (3) and openActionCount (2) are deliberately DIFFERENT
  // numbers here -- a fixture where they agree would pass just as happily
  // against an implementation that reads the wrong field. Asserting on "2",
  // and refusing to also match "3 actions", is what actually pins
  // openActionCount as the one rendered.
  it("shows the current month's draft retro and its OPEN action count, not the total", async () => {
    renderCard(
      retrosFixture({ retros: [summaryFixture({ month: MONTH, finished: false, actionCount: 3, openActionCount: 2 })] }),
    );

    const card = await screen.findByTestId("next-retro-card");
    expect(card).toHaveTextContent(`${monthNameOnly(MONTH)} retro`);
    expect(card).toHaveTextContent(OVERVIEW_COPY.nextRetroInProgress);
    expect(card).toHaveTextContent(OVERVIEW_COPY.nextRetroActions(2, nextMonthName(MONTH)));
    expect(card).not.toHaveTextContent(OVERVIEW_COPY.nextRetroActions(3, nextMonthName(MONTH)));
  });

  // Distinct from the draft above: a finished retro has nothing left to
  // flag as unfinished, so "In progress" must not linger once Finish has
  // been clicked -- the same "the label answers a real question, not just
  // 'is there a row'" rule RetroHistoryList.tsx's own draftInProgress
  // guards.
  it("does not call a finished current-month retro in progress", async () => {
    renderCard(
      retrosFixture({ retros: [summaryFixture({ month: MONTH, finished: true, actionCount: 1, openActionCount: 1 })] }),
    );

    const card = await screen.findByTestId("next-retro-card");
    expect(card).toHaveTextContent(OVERVIEW_COPY.nextRetroActions(1, nextMonthName(MONTH)));
    expect(card).not.toHaveTextContent(OVERVIEW_COPY.nextRetroInProgress);
  });

  // A finished retro nobody added an action to -- task-11-report.md's own
  // fix for the identical "0 actions" defect in RetroHistoryList.tsx is the
  // precedent this card follows too: omit the clause outright, never print
  // a zero.
  it("never prints a zero action count", async () => {
    renderCard(
      retrosFixture({ retros: [summaryFixture({ month: MONTH, finished: true, actionCount: 0, openActionCount: 0 })] }),
    );

    const card = await screen.findByTestId("next-retro-card");
    // Word-boundary before the digit, not a bare substring match -- "10
    // actions" contains "0 action" as a plain substring, which would make
    // this assertion fail for the wrong reason the day this fixture's
    // actionCount is ever bumped to a real two-digit count.
    expect(card).not.toHaveTextContent(/\b0 actions?\b/);
  });

  // The case a `> 0` guard reading the WRONG field would miss: every action
  // is ticked (openActionCount 0) but the retro still has a nonzero total.
  // A guard on actionCount would show "3 actions" here; the card must show
  // nothing at all, the same way a genuinely empty retro does above.
  it("prints nothing when every action is done, even though the total is nonzero", async () => {
    renderCard(
      retrosFixture({ retros: [summaryFixture({ month: MONTH, finished: false, actionCount: 3, openActionCount: 0 })] }),
    );

    const card = await screen.findByTestId("next-retro-card");
    expect(card).not.toHaveTextContent(/\bactions?\b/);
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
    stubFetchRoutes({
      "GET /api/v1/retros": { status: 200, body: retrosFixture() },
      [`GET /api/v1/marriage/vision?year=${YEAR}`]: { status: 200, body: { vision: visionFixture() } },
    });
    renderWithRouter(
      <div data-testid="next-retro-card-wrapper">
        <NextRetroCard />
      </div>,
    );

    const wrapper = await screen.findByTestId("next-retro-card-wrapper");
    expect(wrapper).toBeEmptyDOMElement();
  });

  // The design's own strip (dc.html: "Vision check-in: 2026 theme -- 'Slow
  // down together'", inside this same card). Its own describe block, not
  // folded into the tests above: this is Vision's data, not Retros', so
  // these two are the only tests in this file that touch the vision route
  // for a reason beyond satisfying the stub.
  describe("Vision check-in strip", () => {
    it('shows "Vision check-in: <year> theme — <theme>" when this year has a vision', async () => {
      renderCard(retrosFixture(), {
        [`GET /api/v1/marriage/vision?year=${YEAR}`]: {
          status: 200,
          body: { vision: visionFixture({ theme: "Slow down together", version: 1 }) },
        },
      });

      const strip = await screen.findByTestId("vision-checkin-strip");
      expect(strip).toHaveTextContent(`Vision check-in: ${YEAR} theme — “Slow down together”`);
    });

    // version 0's own theme is always "" on the wire (visionSchema's own
    // comment), so the default `visionFixture()` this file's `renderCard`
    // already stubs with is exactly the "no vision yet" case -- no override
    // needed to reach it.
    it("omits the strip when this year has no vision yet", async () => {
      let visionRequested = false;
      renderCard(retrosFixture(), {
        [`GET /api/v1/marriage/vision?year=${YEAR}`]: {
          status: 200,
          body: { vision: visionFixture() },
          capture: () => {
            visionRequested = true;
          },
        },
      });

      await screen.findByTestId("next-retro-card");
      // Proof the vision request itself resolved, not merely that the retro
      // card rendered -- the two queries are independent, and this card's
      // own top-level guard only waits on retros.
      await waitFor(() => expect(visionRequested).toBe(true));
      expect(screen.queryByTestId("vision-checkin-strip")).not.toBeInTheDocument();
    });
  });

  // The card answers "what is there to do about retros right now". With no
  // retro yet this month and last month's still a draft, that is: finish
  // last month's. A prompt to start the new month would step over the one
  // still open. Checked on the household's 1 October, the morning the QA
  // walk found the card saying "No retro yet this month" over an unfinished
  // September.
  describe("when this month has no retro and last month's is still a draft", () => {
    function renderOnTheFirstOfOctober(retros: RetroSummary[], startMonth: string | null) {
      process.env.TZ = "America/Los_Angeles";
      vi.useFakeTimers({ toFake: ["Date"] });
      vi.setSystemTime(new Date("2026-09-30T23:00:00Z"));
      stubFetchRoutes({
        ...meRoute("Asia/Singapore"),
        "GET /api/v1/retros": { status: 200, body: retrosFixture({ retros, startMonth }) },
        "GET /api/v1/marriage/vision?year=2026": { status: 200, body: { vision: visionFixture({ year: 2026 }) } },
      });
      renderWithRouter(<NextRetroCard />);
    }

    it("shows last month's open draft and a way to it, not a prompt for the new month", async () => {
      renderOnTheFirstOfOctober(
        [summaryFixture({ month: "2026-09", finished: false, actionCount: 3, openActionCount: 2 })],
        "2026-10",
      );

      const card = await screen.findByTestId("next-retro-card");
      await waitFor(() => expect(card).toHaveTextContent("September retro"));
      expect(card).toHaveTextContent(OVERVIEW_COPY.nextRetroInProgress);
      expect(card).toHaveTextContent(OVERVIEW_COPY.nextRetroActions(2, "October"));
      expect(card).not.toHaveTextContent(OVERVIEW_COPY.nextRetroNone);
      expect(screen.queryByRole("link", { name: OVERVIEW_COPY.nextRetroStart("October") })).not.toBeInTheDocument();

      const link = screen.getByRole("link", { name: OVERVIEW_COPY.nextRetroContinue("September") });
      expect(link).toHaveAttribute("href", "/marriage/retros");
    });

    // Last month's retro is finished, so there is nothing open to go back
    // to: the card prompts for this month, as it always did.
    it("prompts for this month when last month's retro is finished", async () => {
      renderOnTheFirstOfOctober([summaryFixture({ month: "2026-09", finished: true })], "2026-10");

      expect(await screen.findByText(OVERVIEW_COPY.nextRetroNone)).toBeInTheDocument();
      expect(screen.getByRole("link", { name: OVERVIEW_COPY.nextRetroStart("October") })).toBeInTheDocument();
      expect(screen.getByTestId("next-retro-card")).not.toHaveTextContent("September retro");
    });

    // This month's retro always wins: once October is started, the card is
    // about October, even with September still open beside it.
    it("shows this month's retro once it exists, even with last month's still open", async () => {
      renderOnTheFirstOfOctober(
        [
          summaryFixture({ id: "retro-oct", month: "2026-10", finished: false }),
          summaryFixture({ id: "retro-sep", month: "2026-09", finished: false }),
        ],
        null,
      );

      const card = await screen.findByTestId("next-retro-card");
      await waitFor(() => expect(card).toHaveTextContent("October retro"));
      expect(card).not.toHaveTextContent("September retro");
      expect(screen.getByRole("link", { name: OVERVIEW_COPY.nextRetroContinue("October") })).toHaveAttribute(
        "href",
        "/marriage/retros",
      );
    });

    // Only last month's draft is brought forward. An older one is not: the
    // Retros page offers last month to start, and the card says the same.
    it("does not bring forward a draft older than last month", async () => {
      renderOnTheFirstOfOctober([summaryFixture({ month: "2026-08", finished: false })], "2026-09");

      expect(await screen.findByText(OVERVIEW_COPY.nextRetroNone)).toBeInTheDocument();
      expect(screen.getByRole("link", { name: OVERVIEW_COPY.nextRetroStart("September") })).toBeInTheDocument();
      expect(screen.getByTestId("next-retro-card")).not.toHaveTextContent("August retro");
    });
  });

  // A finished retro has nothing to continue, so it gets no link.
  it("offers no way to continue a finished retro", async () => {
    renderCard(retrosFixture({ retros: [summaryFixture({ month: MONTH, finished: true })] }));

    await screen.findByTestId("next-retro-card");
    expect(screen.queryByRole("link", { name: OVERVIEW_COPY.nextRetroContinue(monthNameOnly(MONTH)) })).not.toBeInTheDocument();
  });

  // "This month's retro" is the household's month. On the household's 1
  // October, October's draft is the current one, though the browser in Los
  // Angeles, and the server's UTC clock, are still in September.
  it("treats the household's month as the current one, whatever zone the browser is in", async () => {
    process.env.TZ = "America/Los_Angeles";
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-09-30T23:00:00Z"));
    stubFetchRoutes({
      ...meRoute("Asia/Singapore"),
      "GET /api/v1/retros": {
        status: 200,
        body: retrosFixture({
          retros: [summaryFixture({ month: "2026-10", finished: false, actionCount: 1, openActionCount: 1 })],
        }),
      },
      "GET /api/v1/marriage/vision?year=2026": { status: 200, body: { vision: visionFixture({ year: 2026 }) } },
    });
    renderWithRouter(<NextRetroCard />);

    const card = await screen.findByTestId("next-retro-card");
    await waitFor(() => expect(card).toHaveTextContent("October retro"));
    expect(card).toHaveTextContent(OVERVIEW_COPY.nextRetroInProgress);
  });
});
