// NextBillCard.tsx owns its own useBills call, unlike GoalsCard.tsx/
// BudgetCard.tsx's pure-presentation shape -- see that file's own header
// comment for why. That means this suite tests it the BillModal.test.tsx
// way (stub the route, mount the real component, wait for the request to
// settle), not the GoalsCard.test.tsx way (hand it an already-typed prop).
import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import type { BillsResponse } from "../money/billSchemas";
import { OVERVIEW_COPY } from "./copy";
import { NextBillCard } from "./NextBillCard";

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

function billsResponse(summaryOverrides: Partial<BillsResponse["summary"]> = {}): BillsResponse {
  return {
    bills: [],
    paidThisMonth: [],
    summary: {
      currency: "SGD",
      dueThisMonthMinor: 0,
      paidSoFarMinor: 0,
      nextDue: null,
      autopayCount: 0,
      billCount: 0,
      subscriptionsMonthlyMinor: 0,
      subscriptionsAnnualMinor: 0,
      excludedNoRate: 0,
      ...summaryOverrides,
    },
  };
}

function renderCard(enabled: boolean, extraRoutes: Record<string, RouteResponse | RouteResponse[]> = {}) {
  const fetchMock = stubFetchRoutes({ "GET /api/v1/currencies": CURRENCIES, ...extraRoutes });
  return { fetchMock, ...renderWithRouter(<NextBillCard enabled={enabled} />) };
}

describe("NextBillCard", () => {
  // The design's own line (design/Household Dashboard.dc.html's go_bills
  // tile): the bill's own amount as the headline figure, its name and due
  // date as the subtitle -- "S$142.30" over "SP utilities · Jul 8".
  it("shows the design's line: the bill's amount over its name and due date", async () => {
    renderCard(true, {
      "GET /api/v1/bills": {
        status: 200,
        body: billsResponse({
          billCount: 1,
          nextDue: {
            billId: "bill-1",
            billName: "SP utilities",
            dueOn: "2026-07-08",
            amountMinor: 14230,
            currency: "SGD",
            overdue: false,
            autopay: true,
          },
        }),
      },
    });

    expect(await screen.findByText("Next bill")).toBeInTheDocument();
    expect(await screen.findByText("S$142.30")).toBeInTheDocument();
    expect(screen.getByText("SP utilities · Jul 8")).toBeInTheDocument();
  });

  // State 5's own contract (billCopy.ts's own comment on
  // nextDueOverdueValue, restated for this card): names the bill as overdue
  // rather than printing the past date as though it were still upcoming.
  it("names an overdue next bill as overdue rather than printing a past date as though upcoming", async () => {
    renderCard(true, {
      "GET /api/v1/bills": {
        status: 200,
        body: billsResponse({
          billCount: 1,
          nextDue: {
            billId: "bill-1",
            billName: "Tax GIRO",
            dueOn: "2026-06-01",
            amountMinor: 50000,
            currency: "SGD",
            overdue: true,
            autopay: false,
          },
        }),
      },
    });

    expect(await screen.findByText("S$500.00")).toBeInTheDocument();
    expect(screen.getByText("Tax GIRO · Overdue")).toBeInTheDocument();
    expect(screen.queryByText(/Jun 1/)).not.toBeInTheDocument();
  });

  // A household that has never added a live bill -- distinct from the
  // caught-up state below, which still has bills, just none due next.
  it("renders its own empty line, never a zero, for a household with no bills", async () => {
    renderCard(true, {
      "GET /api/v1/bills": { status: 200, body: billsResponse() },
    });

    expect(await screen.findByText(OVERVIEW_COPY.nextBillNone)).toBeInTheDocument();
    expect(screen.queryByText(/^S\$/)).not.toBeInTheDocument();
    const link = screen.getByRole("link", { name: OVERVIEW_COPY.nextBillAdd });
    expect(link).toHaveAttribute("href", "/money/bills");
  });

  // The state GoalsCard.tsx's own achieved-goal fix is the precedent for:
  // billCount > 0 but nextDue === null happens when every live bill is a
  // settled one-off (paid, no next occurrence) -- an ordinary state, not the
  // same as never having added a bill. "No bills yet" would be false here,
  // and there is no zero to print in its place either.
  it("tells a household with only settled bills that nothing is due, not that it has no bills", async () => {
    renderCard(true, {
      "GET /api/v1/bills": { status: 200, body: billsResponse({ billCount: 2, nextDue: null }) },
    });

    expect(await screen.findByText(OVERVIEW_COPY.nextBillCaughtUp)).toBeInTheDocument();
    expect(screen.queryByText(OVERVIEW_COPY.nextBillNone)).not.toBeInTheDocument();
  });

  // Task 11's `enabled` option means a limited member's browser never sends
  // this request at all -- the earlier wording here said "on a 403," which
  // this task's own brief retracts as a state that cannot occur. No
  // "GET /api/v1/bills" route is registered below, so a query that fires
  // anyway fails this test loudly (stubFetchRoutes' own throw-on-unregistered
  // behaviour), rather than this test merely finding no heading and calling
  // that proof. The wrapper div is unconditional static markup, so waiting
  // for it accounts for the router's own async initial transition
  // (GoalsCard.test.tsx's own comment on why the first query there is
  // `find`, not `get`) without racing NextBillCard's own render.
  it("renders nothing while its query is disabled, not an error region or a bare heading", async () => {
    const fetchMock = stubFetchRoutes({ "GET /api/v1/currencies": CURRENCIES });
    renderWithRouter(
      <div data-testid="next-bill-card-wrapper">
        <NextBillCard enabled={false} />
      </div>,
    );
    const wrapper = await screen.findByTestId("next-bill-card-wrapper");

    expect(wrapper).toBeEmptyDOMElement();
    expect(fetchMock.mock.calls.some(([url]) => String(url).includes("/api/v1/bills"))).toBe(false);
  });
});
